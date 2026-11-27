package server

import (
	"context"
	"io"
	"net"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"iosview/internal/device"
	"iosview/internal/usbmux/fakemux"
)

// A stalled auxiliary USB handshake must not hold commands on the live stream.
func TestUSBRefreshDoesNotBlockStream(t *testing.T) {
	listen := func() net.Listener {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		return ln
	}
	mux, video := listen(), listen()
	const port = 5911
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	go fakemux.Serve(mux, []fakemux.Phone{{ID: 1, UDID: "USB-test", Target: map[int]string{port: video.Addr().String()}, BeforeConnect: func(p int) {
		if p == port && calls.Add(1) == 2 {
			close(started)
			<-release
		}
	}}})
	t.Setenv("USBMUXD_SOCKET_ADDRESS", mux.Addr().String())
	cfg, err := device.LoadConfig(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.H264Port = port
	m := device.NewManager(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for m.Get("usb:USB-test") == nil {
		if ctx.Err() != nil {
			t.Fatal("missing USB attachment")
		}
		time.Sleep(10 * time.Millisecond)
	}
	srv := httptest.NewServer((&Server{M: m}).Handler())
	defer srv.Close()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/h264/usb:USB-test?ready=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	video.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second))
	stream, err := video.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	kind, ready, err := ws.Read(ctx)
	if err != nil || kind != websocket.MessageText || string(ready) != "ready" {
		t.Fatalf("missing tunnel-ready notification: %q, %v", ready, err)
	}
	if err := ws.Write(ctx, websocket.MessageBinary, []byte{'F', 0, 1}); err != nil {
		t.Fatal(err)
	}
	var fresh [3]byte
	stream.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(stream, fresh[:]); err != nil || fresh != [3]byte{'F', 0, 1} || calls.Load() != 1 {
		t.Fatalf("in-band request: %v, calls=%d, err=%v", fresh, calls.Load(), err)
	}
	for _, command := range []string{"R", "B1234"} {
		if err := ws.Write(ctx, websocket.MessageBinary, []byte(command)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("refresh did not open auxiliary tunnel")
	}
	stream.SetReadDeadline(time.Now().Add(750 * time.Millisecond))
	var commands [6]byte
	if _, err := io.ReadFull(stream, commands[:]); err != nil || string(commands[:]) != "KB1234" {
		t.Fatalf("commands delayed by auxiliary handshake: %q, %v", commands, err)
	}
}
