// Package server exposes the web UI, a control WebSocket (input + state) and
// a per-device WebSocket→VNC proxy used by the in-browser noVNC viewers.
package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"iosview/internal/device"
	"iosview/internal/update"
)

// Server wires HTTP handlers to the device manager.
type Server struct {
	M   *device.Manager
	Web fs.FS
	// Version is shown in the UI so the running build is easy to tell apart.
	Version string
	// Up checks for / installs internal updates (nil = updates off, e.g. dev build).
	Up          *update.Updater
	CopyCommand bool
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// The UI is embedded in the exe: never let the window reuse files cached
	// from an older build.
	static := http.FileServer(http.FS(s.Web))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		static.ServeHTTP(w, r)
	}))
	mux.HandleFunc("POST /api/copy", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		result, err := s.M.CopyClipboards(r.Context(), s.CopyCommand, r.URL.Query().Get("blank") == "1")
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]string{"version": s.Version})
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.M.Snapshot())
	})
	mux.HandleFunc("/api/shot", s.shot)
	mux.HandleFunc("/api/update", s.update)
	mux.HandleFunc("/api/ui", s.uiPrefs)
	mux.HandleFunc("/ws/control", s.control)
	mux.HandleFunc("/ws/vnc/", s.vncProxy)
	mux.HandleFunc("/ws/h264/", s.h264Proxy)
	return mux
}

func accept(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	// Only the local UI may connect (blocks drive-by pages on other origins).
	// The Wails build serves the page from http://wails.localhost, so that
	// origin is allowed too (the request host is always allowed).
	return websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:       []string{"binary"},
		CompressionMode:    websocket.CompressionDisabled,
		InsecureSkipVerify: false,
		OriginPatterns:     []string{"wails.localhost"},
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
	if d.IsCD() {
		ws.Close(websocket.StatusTryAgainLater, "máy CoreDevice chỉ có luồng HEVC")
		return
	}
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
	if d.IsCD() { // iOS 17+ without jailbreak: HEVC from the iosview-cd sidecar
		base := d.CDBase()
		if base == "" {
			ws.Close(websocket.StatusTryAgainLater, "chưa kết nối")
			return
		}
		if err := s.cdVideo(r.Context(), ws, base); err != nil {
			ws.Close(websocket.StatusTryAgainLater, trim(err.Error()))
			return
		}
		ws.Close(websocket.StatusNormalClosure, "")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	trace := os.Getenv("IOSVIEW_H264_TIMING") == "1"
	opened := time.Now()
	conn, route, err := d.DialVideo(ctx)
	if trace {
		log.Printf("H264 %s %s: mở đường %v, lỗi=%v", id, route, time.Since(opened).Round(time.Millisecond), err)
	}
	if err != nil {
		ws.Close(websocket.StatusTryAgainLater, trim(err.Error()))
		return
	}
	defer conn.Close()
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(512 * 1024)
	}
	if r.URL.Query().Get("ready") == "1" {
		if err := ws.Write(ctx, websocket.MessageText, []byte("ready")); err != nil {
			return
		}
	}

	go func() {
		defer cancel()
		var lastRefresh time.Time
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if len(data) == 1 && data[0] == 'R' {
				// TrollVNC forces a new screen capture when a viewer joins.
				// K alone may only re-encode its cached pixel buffer.
				if time.Since(lastRefresh) >= 2*time.Second {
					lastRefresh = time.Now()
					go func() {
						start := time.Now()
						refreshCtx, done := context.WithTimeout(ctx, time.Second)
						defer done()
						peer, err := d.DialH264(refreshCtx)
						if err == nil {
							peer.Close()
						}
						if trace {
							log.Printf("H264 %s: mở kết nối làm mới %v, lỗi=%v", id, time.Since(start).Round(time.Millisecond), err)
						}
					}()
				}
				data[0] = 'K'
			}
			_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
			if _, err := conn.Write(data); err != nil {
				return
			}
		}
	}()
	go func() { <-ctx.Done(); conn.Close() }()

	br := bufio.NewReaderSize(conn, 256*1024)
	// One buffer reused for every frame: ws.Write is synchronous and does not
	// keep it, so there is no per-frame allocation (less GC with many phones).
	var buf []byte
	lastRot := byte(0xff)
	var lastFrame, windowStart time.Time
	var maxGap, maxWrite time.Duration
	var frames int
	var lastWrite time.Duration
	var firstKey bool
	if trace {
		defer func() {
			log.Printf("H264 %s %s: đóng, khung còn trong kỳ=%d, khoảng nhận max=%v, ghi max=%v, lỗi=%v",
				id, route, frames, maxGap, maxWrite, err)
		}()
	}
	for {
		var hdr [12]byte
		if _, err = io.ReadFull(br, hdr[:]); err != nil {
			ws.Close(websocket.StatusNormalClosure, "")
			return
		}
		n := binary.BigEndian.Uint32(hdr[:4])
		if n == 0 || n > 16<<20 {
			err = errors.New("khung sai kích thước")
			ws.Close(websocket.StatusProtocolError, "bad frame")
			return
		}
		if hdr[8]&14 == 0 {
			if rot := hdr[9] & 3; rot != lastRot {
				if lastRot != 0xff {
					go d.NudgeSize()
				}
				lastRot = rot
			}
		}
		if cap(buf) < 12+int(n) {
			buf = make([]byte, 12+int(n)+int(n)/2) // headroom: keyframes vary in size
		}
		msg := buf[:12+int(n)]
		copy(msg, hdr[:])
		if _, err = io.ReadFull(br, msg[12:]); err != nil {
			ws.Close(websocket.StatusNormalClosure, "")
			return
		}
		var received time.Time
		if trace {
			received = time.Now()
			if !firstKey && hdr[8]&1 != 0 {
				firstKey = true
				log.Printf("H264 %s %s: khung khóa đầu sau %v", id, route, received.Sub(opened).Round(time.Millisecond))
			}
			if !lastFrame.IsZero() {
				gap := received.Sub(lastFrame)
				if gap >= 500*time.Millisecond {
					log.Printf("H264 %s %s: gián đoạn %v, ghi khung trước %v, còn lại %v", id, route, gap, lastWrite, gap-lastWrite)
				}
				maxGap = max(maxGap, gap)
			} else {
				windowStart = received
			}
			lastFrame = received
		}
		if err = ws.Write(ctx, websocket.MessageBinary, msg); err != nil {
			return
		}
		if trace {
			lastWrite = time.Since(received)
			maxWrite = max(maxWrite, lastWrite)
			frames++
			if received.Sub(windowStart) >= 5*time.Second {
				log.Printf("H264 %s: %d khung/%v, khoảng nhận lớn nhất=%v, ghi WebSocket lâu nhất=%v", id, frames,
					received.Sub(windowStart).Round(time.Millisecond), maxGap.Round(time.Millisecond), maxWrite.Round(time.Millisecond))
				windowStart, frames, maxGap, maxWrite = received, 0, 0, 0
			}
		}
	}
}

// update: GET = status (?check=1 checks now), POST = download, swap the exe
// and restart (the window closes and the new build opens).
func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if s.Up == nil {
		_ = json.NewEncoder(w).Encode(update.Status{Current: s.Version, Error: "bản dev không tự cập nhật"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("check") == "1" {
			_ = s.Up.Check(r.Context())
		}
	case http.MethodPost:
		// Same-origin UI only.
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := s.Up.Start(); err != nil {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
	default:
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	_ = json.NewEncoder(w).Encode(s.Up.Status())
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
	Old   string   `json:"old,omitempty"`
	No    int      `json:"no,omitempty"`
	Pw    string   `json:"pw,omitempty"`
	Q     int      `json:"q,omitempty"`
	FQ    int      `json:"fq,omitempty"`
	Port  int      `json:"port,omitempty"`
	Strip bool     `json:"strip"`
	Seq   int      `json:"seq,omitempty"`
	One   bool     `json:"one,omitempty"`   // only device D, ignore the sync group (Alt held)
	F     bool     `json:"f,omitempty"`     // text: force typing (dán cưỡng bức)
	Quiet bool     `json:"quiet,omitempty"` // text: the UI shows its own result
}

// Only the state publisher calls this. The writer may drain the slot at any time.
func offerControlState(slot chan []byte, snapshot []byte) {
	select {
	case <-slot:
	default:
	}
	slot <- snapshot
}

func offerControlReply(out chan<- []byte, reply map[string]any, cancel context.CancelFunc) {
	b, _ := json.Marshal(reply)
	select {
	case out <- b:
	default:
		// End only this control connection if a command result cannot be sent.
		switch reply["t"] {
		case "textAck", "wifi", "clipboardCopy":
			cancel()
		}
	}
}

func (s *Server) control(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled, OriginPatterns: []string{"wails.localhost"}})
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer ws.CloseNow()

	out := make(chan []byte, 256)
	states := make(chan []byte, 1)
	push := func(v map[string]any) { offerControlReply(out, v, cancel) }
	sendState := func() {
		b, _ := json.Marshal(map[string]any{"t": "state", "state": s.M.Snapshot()})
		offerControlState(states, b)
	}

	// Run config-mutating commands (which persist iosview.json) on an ordered
	// background worker so disk/antivirus latency never stalls realtime touch input.
	cfgOps := make(chan func(), 64)
	runCfg := func(fn func()) bool {
		select {
		case cfgOps <- fn:
			return true
		default:
			push(map[string]any{"t": "toast", "s": "Đang lưu nhiều thay đổi, vui lòng thử lại"})
			return false
		}
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case fn := <-cfgOps:
				fn()
			}
		}
	}()

	go func() {
		for {
			var b []byte
			select {
			case <-ctx.Done():
				return
			case b = <-out:
			case b = <-states:
			}
			wctx, c := context.WithTimeout(ctx, 5*time.Second)
			err := ws.Write(wctx, websocket.MessageText, b)
			c()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	copies, unsubscribeCopies := s.M.SubscribeClipboardCopies()
	defer unsubscribeCopies()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case copy := <-copies:
				push(map[string]any{"t": "clipboardCopy", "copy": copy})
			}
		}
	}()

	sub, unsub := s.M.Subscribe()
	defer unsub()
	go func() {
		sendState()
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

	// Devices this window is currently pressing on. If the window goes away
	// mid-drag (reload, crash, closed), the release would never come: lift
	// them so no phone is left with a finger down.
	type press struct {
		x, y    float64
		targets []device.InputTarget
	}
	held := map[string]press{}
	defer func() {
		for _, p := range held {
			s.M.ContinueTouch(p.targets, device.Pointer(p.x, p.y, 0))
		}
	}()
	// dispatch honours Alt (individual control): only the device acted on.
	dispatch := func(m *msg, ev device.Event) int {
		if m.One {
			return s.M.DispatchOne(m.D, ev)
		}
		return s.M.Dispatch(m.D, ev)
	}

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
			ev := device.Pointer(m.X, m.Y, m.M)
			p, active := held[m.D]
			if active {
				s.M.ContinueTouch(p.targets, ev)
			} else if m.M != 0 {
				p.targets = s.M.StartTouch(m.D, m.One, ev)
			}
			if m.M != 0 {
				p.x, p.y = m.X, m.Y
				held[m.D] = p
			} else {
				delete(held, m.D)
			}
		case "w":
			dispatch(&m, device.Wheel(m.X, m.Y, m.M, m.Down))
		case "k":
			dispatch(&m, device.Key(m.K, m.Down))
		case "text":
			ev := device.Text(m.S)
			if m.F {
				ev = device.ForceText(m.S)
			}
			n := dispatch(&m, ev)
			if m.Seq != 0 {
				push(map[string]any{"t": "textAck", "seq": m.Seq, "count": n})
			} else if !m.Quiet {
				push(map[string]any{"t": "toast", "s": textToast(n)})
			}
		case "phoneAction", "btn":
			if m.T == "phoneAction" || m.B == "cc" {
				req := m
				runCfg(func() {
					actionCtx, done := context.WithTimeout(ctx, 4*time.Second)
					defer done()
					apps, msg := s.M.PhoneAction(actionCtx, req.D, req.B, req.S, req.One)
					if req.B == "apps" {
						push(map[string]any{"t": "phoneApps", "seq": req.Seq, "apps": apps, "error": msg})
					} else if msg != "" {
						push(map[string]any{"t": "toast", "s": msg})
					}
				})
				continue
			}
			dispatch(&m, device.Button(m.B))
		case "sync":
			s.M.ApplySync(m.On)
		case "h264":
			on := m.On
			runCfg(func() { s.M.SetH264(on) })
		case "copyblank":
			s.M.SetCopyBlank(m.On)
		case "group":
			s.M.ApplyInSync(m.IDs, m.On)
		case "park":
			if len(m.IDs) > 0 {
				s.M.ApplyParkedGroup(m.IDs, m.On)
			} else if m.D != "" {
				s.M.ApplyParked(m.D, m.On)
			}
		case "rename":
			d, name := m.D, m.Name
			runCfg(func() { s.M.Rename(d, name) })
		case "number":
			d, no := m.D, m.No
			runCfg(func() {
				if !s.M.SetNumber(d, no) {
					push(map[string]any{"t": "toast", "s": "Số máy phải từ 1 đến 999"})
				}
			})
		case "tag":
			name, ids, old := m.Name, m.IDs, m.Old
			runCfg(func() { s.M.SetTag(name, ids, old) })
		case "tagdel":
			name := m.Name
			runCfg(func() { s.M.DeleteTag(name) })
		case "addwifi":
			addr := m.S
			runCfg(func() { s.M.AddWifi(addr) })
		case "setwifi":
			ids, seq := m.IDs, m.Seq
			if !runCfg(func() {
				reply := map[string]any{"t": "wifi", "seq": seq}
				if err := s.M.SetWifiList(ids); err != nil {
					reply["error"] = "Không lưu được danh sách Wi-Fi: " + err.Error()
				}
				push(reply)
			}) {
				push(map[string]any{"t": "wifi", "seq": seq, "error": "Danh sách Wi-Fi chưa lưu"})
			}
		case "remove":
			d := m.D
			runCfg(func() { s.M.Remove(d) })
		case "link":
			d, target := m.D, m.S
			runCfg(func() { s.M.Link(d, target) })
		case "settings":
			pw, q, fq, port, strip := m.Pw, m.Q, m.FQ, m.Port, m.Strip
			runCfg(func() { s.M.SetSettings(pw, q, fq, port, strip) })
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
	return "Đã xếp lệnh gửi văn bản cho " + itoa(n) + " máy"
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// uiPrefs stores the window's own data (shortcuts, quick phrases, notes) in
// iosview.json. GET returns it, PUT replaces it (JSON object, max 1 MiB).
func (s *Server) uiPrefs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s.M.UIPrefs())
	case http.MethodPut:
		// Same-origin UI only (blocks other pages from overwriting notes).
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "need application/json", http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
		if err != nil || len(body) > 1<<20 {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err := s.M.SetUIPrefs(body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "GET or PUT", http.StatusMethodNotAllowed)
	}
}
