package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"iosview/internal/device"
)

// phone is a minimal RFB 3.8 server (security None) recording pointer masks.
type phone struct {
	ln   net.Listener
	mu   sync.Mutex
	mask []int
}

func newPhone(t *testing.T) *phone {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	p := &phone{ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.serve(c)
		}
	}()
	return p
}

func (p *phone) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	c.Write([]byte("RFB 003.008\n"))
	io.ReadFull(r, make([]byte, 12))
	c.Write([]byte{1, 1})
	r.ReadByte()
	c.Write([]byte{0, 0, 0, 0})
	r.ReadByte()
	si := make([]byte, 24)
	binary.BigEndian.PutUint16(si[0:], 100)
	binary.BigEndian.PutUint16(si[2:], 200)
	c.Write(si)
	sizes := map[byte]int{0: 19, 3: 9, 4: 7, 5: 5}
	for {
		t, err := r.ReadByte()
		if err != nil {
			return
		}
		switch t {
		case 2:
			h := make([]byte, 3)
			io.ReadFull(r, h)
			io.ReadFull(r, make([]byte, 4*int(binary.BigEndian.Uint16(h[1:]))))
		case 6:
			h := make([]byte, 7)
			io.ReadFull(r, h)
			n := int32(binary.BigEndian.Uint32(h[3:]))
			if n < 0 {
				n = -n
			}
			io.ReadFull(r, make([]byte, n))
		default:
			b := make([]byte, sizes[t])
			io.ReadFull(r, b)
			if t == 5 {
				p.mu.Lock()
				p.mask = append(p.mask, int(b[0]))
				p.mu.Unlock()
			}
		}
	}
}

func (p *phone) last() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.mask) == 0 {
		return -1, 0
	}
	return p.mask[len(p.mask)-1], len(p.mask)
}

// A UI window that disappears mid-drag (reload, crash) must not leave the
// phone with a finger down.
func TestControlCloseLiftsTouch(t *testing.T) {
	t.Setenv("USBMUXD_SOCKET_ADDRESS", "127.0.0.1:1")
	ph := newPhone(t)
	cfg, _ := device.LoadConfig(filepath.Join(t.TempDir(), "c.json"))
	cfg.WifiDevices = []string{ph.ln.Addr().String()}
	m := device.NewManager(cfg)
	id := "wifi:" + ph.ln.Addr().String()
	deadline := time.Now().Add(5 * time.Second)
	for m.Get(id) == nil || m.Snapshot().Devices[0].Status != "online" {
		if time.Now().After(deadline) {
			t.Fatal("device not online")
		}
		time.Sleep(20 * time.Millisecond)
	}

	srv := httptest.NewServer((&Server{M: m}).Handler())
	defer srv.Close()
	ctx := context.Background()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/control", nil)
	if err != nil {
		t.Fatal(err)
	}
	send := func(s string) {
		if err := ws.Write(ctx, websocket.MessageText, []byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	send(`{"t":"p","d":"` + id + `","x":0.5,"y":0.5,"m":1}`)
	send(`{"t":"p","d":"` + id + `","x":0.6,"y":0.6,"m":1}`)
	for {
		if mk, _ := ph.last(); mk == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("press not delivered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ws.CloseNow() // window gone before the release
	for {
		if mk, _ := ph.last(); mk == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("touch left down after the UI disconnected")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestControlBackpressure(t *testing.T) {
	states := make(chan []byte, 1)
	offerControlState(states, []byte("old"))
	offerControlState(states, []byte("latest"))
	if got := string(<-states); got != "latest" {
		t.Fatalf("pending snapshot = %q, want latest", got)
	}

	for _, kind := range []string{"toast", "textAck", "wifi", "clipboardCopy"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out := make(chan []byte, 1)
			offerControlReply(out, map[string]any{"t": kind}, cancel)
			if ctx.Err() != nil || len(out) != 1 {
				t.Fatal("reply should fit without closing the connection")
			}
			offerControlReply(out, map[string]any{"t": kind}, cancel)
			if (ctx.Err() != nil) != (kind != "toast") {
				t.Fatalf("unexpected cancellation state for %s: %v", kind, ctx.Err())
			}
		})
	}
}

func TestUIPrefsEndpoint(t *testing.T) {
	t.Setenv("USBMUXD_SOCKET_ADDRESS", "127.0.0.1:1")
	cfg, _ := device.LoadConfig(filepath.Join(t.TempDir(), "c.json"))
	srv := httptest.NewServer((&Server{M: device.NewManager(cfg)}).Handler())
	defer srv.Close()
	put := func(body, ctype, origin string) int {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/ui", strings.NewReader(body))
		req.Header.Set("Content-Type", ctype)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if c := put(`{"notes":"x"}`, "application/json", "http://evil.example"); c != http.StatusForbidden {
		t.Errorf("cross-origin PUT = %d", c)
	}
	if c := put(`{"notes":"x"}`, "text/plain", ""); c != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain PUT = %d", c)
	}
	if c := put(`{"notes":"hello"}`, "application/json", srv.URL); c != http.StatusNoContent {
		t.Fatalf("PUT = %d", c)
	}
	res, err := http.Get(srv.URL + "/api/ui")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(b) != `{"notes":"hello"}` {
		t.Fatalf("GET = %s", b)
	}
}
