package server

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestCDVideo: the sidecar's /codec + /stream.bin become the UI's framing
// (config message, then key/delta/reset frames); 'K' from the UI hits /pli.
func TestCDVideo(t *testing.T) {
	var plis atomic.Int32
	side := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/codec":
			w.Write([]byte(`{"codec":"hvc1.1.6.L153.B0","description":"AAEC"}`))
		case "/pli":
			plis.Add(1)
		case "/stream.bin":
			fl := w.(http.Flusher)
			for _, f := range []struct {
				typ  byte
				data string
			}{{0, "KEY"}, {1, "delta"}, {2, "RESET"}} {
				var h [5]byte
				binary.BigEndian.PutUint32(h[:4], uint32(1+len(f.data)))
				h[4] = f.typ
				w.Write(h[:])
				w.Write([]byte(f.data))
				fl.Flush()
			}
			<-r.Context().Done()
		}
	}))
	defer side.Close()

	s := &Server{}
	ui := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		_ = s.cdVideo(r.Context(), ws, side.URL)
	}))
	defer ui.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ui.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()

	want := []struct {
		flags byte
		data  string
	}{{8, `{"codec":"hvc1.1.6.L153.B0","description":"AAEC"}`}, {1, "KEY"}, {0, "delta"}, {5, "RESET"}}
	for i, w := range want {
		_, b, err := ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		n := binary.BigEndian.Uint32(b[:4])
		if b[8] != w.flags || int(n) != len(b)-12 || string(b[12:]) != w.data {
			t.Fatalf("msg %d: flags %d len %d data %q, want flags %d data %q", i, b[8], n, b[12:], w.flags, w.data)
		}
	}
	ws.Write(ctx, websocket.MessageBinary, []byte{'K'})
	ws.Write(ctx, websocket.MessageBinary, []byte{'K'}) // throttled
	time.Sleep(300 * time.Millisecond)
	if got := plis.Load(); got != 1 {
		t.Fatalf("want 1 PLI, got %d", got)
	}
}
