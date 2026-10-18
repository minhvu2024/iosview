// Package server exposes the web UI, a control WebSocket (input + state) and
// a per-device WebSocket→VNC proxy used by the in-browser noVNC viewers.
package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"iosview/internal/device"
)

// Server wires HTTP handlers to the device manager.
type Server struct {
	M   *device.Manager
	Web fs.FS
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(s.Web)))
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.M.Snapshot())
	})
	mux.HandleFunc("/ws/control", s.control)
	mux.HandleFunc("/ws/vnc/", s.vncProxy)
	mux.HandleFunc("/ws/h264/", s.h264Proxy)
	return mux
}

func accept(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	// Only the local UI may connect (blocks drive-by pages on other origins).
	return websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:       []string{"binary"},
		CompressionMode:    websocket.CompressionDisabled,
		InsecureSkipVerify: false,
	})
}

// vncProxy pipes a browser WebSocket to the phone's VNC port.
//
// Pipelining: plain RFB is request/response — the viewer asks for the next
// frame only after it has received and parsed the previous one, so every
// frame waits one full round trip (painful on Wi-Fi). As soon as frame data
// starts arriving from the phone, the proxy re-sends the viewer's last
// incremental FramebufferUpdateRequest itself, so the phone already holds
// the request for the next frame while the current one is still in flight.
// Duplicate requests just merge on the server, so this is safe.
func (s *Server) vncProxy(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/ws/vnc/")
	d := s.M.Get(id)
	if d == nil {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	ws, err := accept(w, r)
	if err != nil {
		return
	}
	ws.SetReadLimit(-1)
	conn, err := d.Dial()
	if err != nil {
		ws.Close(websocket.StatusTryAgainLater, trim(err.Error()))
		return
	}
	defer conn.Close()
	defer ws.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	pipeline := s.M.Pipeline()
	var (
		wmu     sync.Mutex               // serialises writes to the phone
		lastReq atomic.Pointer[[10]byte] // viewer's last update request, as incremental
	)
	toPhone := func(b []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		_, err := conn.Write(b)
		return err
	}

	// browser -> phone (noVNC sends whole RFB messages per WebSocket message)
	go func() {
		defer cancel()
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			// FramebufferUpdateRequest: type 3, 10 bytes. Never ambiguous with
			// handshake messages (those are 1, 12 or 16 bytes).
			if pipeline && len(data) == 10 && data[0] == 3 {
				var q [10]byte
				copy(q[:], data)
				q[1] = 1 // incremental
				lastReq.Store(&q)
			}
			if toPhone(data) != nil {
				return
			}
		}
	}()

	// phone -> browser
	buf := make([]byte, 256*1024)
	var lastAsk time.Time
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if q := lastReq.Load(); q != nil && time.Since(lastAsk) > 4*time.Millisecond {
				lastAsk = time.Now()
				if toPhone(q[:]) != nil {
					return
				}
			}
			if ws.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
				return
			}
		}
		if err != nil {
			ws.Close(websocket.StatusNormalClosure, "")
			return
		}
	}
}

// h264Proxy relays the phone's H.264 stream: one WebSocket message per
// encoded frame (12-byte header + Annex-B data), so the browser can hand each
// message straight to WebCodecs. Browser→phone bytes are stream commands
// ('K' keyframe, 'B'+kbps bitrate).
func (s *Server) h264Proxy(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/ws/h264/")
	d := s.M.Get(id)
	if d == nil {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	ws, err := accept(w, r)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	conn, err := d.DialH264()
	if err != nil {
		ws.Close(websocket.StatusTryAgainLater, trim(err.Error()))
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		defer cancel()
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if _, err := conn.Write(data); err != nil {
				return
			}
		}
	}()
	go func() { <-ctx.Done(); conn.Close() }()

	br := bufio.NewReaderSize(conn, 256*1024)
	for {
		var hdr [12]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			ws.Close(websocket.StatusNormalClosure, "")
			return
		}
		n := binary.BigEndian.Uint32(hdr[:4])
		if n == 0 || n > 16<<20 {
			ws.Close(websocket.StatusProtocolError, "bad frame")
			return
		}
		msg := make([]byte, 12+int(n))
		copy(msg, hdr[:])
		if _, err := io.ReadFull(br, msg[12:]); err != nil {
			ws.Close(websocket.StatusNormalClosure, "")
			return
		}
		if err := ws.Write(ctx, websocket.MessageBinary, msg); err != nil {
			return
		}
	}
}

func trim(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}

// Control messages from the UI. Short keys keep pointer traffic tiny.
type msg struct {
	T     string   `json:"t"`
	D     string   `json:"d,omitempty"` // device the user acted on
	X     float64  `json:"x,omitempty"`
	Y     float64  `json:"y,omitempty"`
	M     uint8    `json:"m,omitempty"`
	K     uint32   `json:"k,omitempty"`
	Down  bool     `json:"down,omitempty"`
	S     string   `json:"s,omitempty"`
	B     string   `json:"b,omitempty"`
	On    bool     `json:"on,omitempty"`
	IDs   []string `json:"ids,omitempty"`
	Name  string   `json:"name,omitempty"`
	Pw    string   `json:"pw,omitempty"`
	Q     int      `json:"q,omitempty"`
	FQ    int      `json:"fq,omitempty"`
	Port  int      `json:"port,omitempty"`
	Strip bool     `json:"strip"`
	Seq   int      `json:"seq,omitempty"`
}

func (s *Server) control(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer ws.CloseNow()

	out := make(chan []byte, 64)
	push := func(v any) {
		b, _ := json.Marshal(v)
		select {
		case out <- b:
		default:
		}
	}
	sendState := func() { push(map[string]any{"t": "state", "state": s.M.Snapshot()}) }

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-out:
				wctx, c := context.WithTimeout(ctx, 5*time.Second)
				err := ws.Write(wctx, websocket.MessageText, b)
				c()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	sub, unsub := s.M.Subscribe()
	defer unsub()
	clips, unclip := s.M.SubscribeClipboard()
	defer unclip()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case c := <-clips:
				push(map[string]any{"t": "clip", "d": c.Device, "s": c.Text})
			}
		}
	}()
	go func() {
		// Coalesce state pushes to at most ~20/s.
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		dirty := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-sub:
				dirty = true
			case <-t.C:
				if dirty {
					dirty = false
					sendState()
				}
			}
		}
	}()
	sendState()

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var m msg
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		switch m.T {
		case "p":
			s.M.Dispatch(m.D, device.Pointer(m.X, m.Y, m.M))
		case "k":
			s.M.Dispatch(m.D, device.Key(m.K, m.Down))
		case "text":
			n := s.M.Dispatch(m.D, device.Text(m.S))
			push(map[string]any{"t": "toast", "s": textToast(n)})
		case "btn":
			s.M.Dispatch(m.D, device.Button(m.B))
		case "sync":
			s.M.SetSync(m.On)
		case "h264":
			s.M.SetH264(m.On)
		case "group":
			s.M.SetInSync(m.IDs, m.On)
		case "rename":
			s.M.Rename(m.D, m.Name)
		case "addwifi":
			s.M.AddWifi(m.S)
		case "remove":
			s.M.Remove(m.D)
		case "link":
			s.M.Link(m.D, m.S)
		case "settings":
			s.M.SetSettings(m.Pw, m.Q, m.FQ, m.Port, m.Strip)
		case "scan":
			go func() {
				found := s.M.ScanLAN()
				push(map[string]any{"t": "scan", "found": found})
			}()
		case "ping":
			push(map[string]any{"t": "pong", "seq": m.Seq})
		default:
			log.Printf("unknown control message %q", m.T)
		}
	}
}

func textToast(n int) string {
	if n == 0 {
		return "Không có máy nào để dán (máy chưa online?)"
	}
	return "Đã dán vào " + itoa(n) + " máy"
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
