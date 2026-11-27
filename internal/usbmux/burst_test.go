package usbmux_test

import (
	"context"
	"net"
	"testing"
	"time"

	"iosview/internal/usbmux"
	"iosview/internal/usbmux/fakemux"
)

func TestTwentyUSBViewersLeaveRoomForControl(t *testing.T) {
	listen := func() net.Listener {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		return ln
	}
	mux, target := listen(), listen()
	go func() {
		for {
			c, err := target.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	gate := make(chan struct{})
	defer close(gate)
	started := make(chan struct{}, 20)
	done := make(chan error, 20)
	phones := make([]fakemux.Phone, 20)
	for i := range phones {
		phones[i] = fakemux.Phone{ID: int64(i + 1), Target: map[int]string{5911: target.Addr().String(), 5901: target.Addr().String()}, BeforeConnect: func(port int) {
			if port == 5911 {
				started <- struct{}{}
				<-gate
			}
		}}
	}
	go fakemux.Serve(mux, phones)
	t.Setenv("USBMUXD_SOCKET_ADDRESS", mux.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for _, p := range phones {
		go func(id int64) {
			c, err := usbmux.ConnectContext(ctx, id, 5911, true)
			if c != nil {
				c.Close()
			}
			done <- err
		}(p.ID)
	}
	for range 6 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("background handshakes failed to start")
		}
	}
	control, err := usbmux.ConnectContext(ctx, 1, 5901, false)
	if err != nil {
		t.Fatal(err)
	}
	control.Close()
	for range phones {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			t.Fatal("queued viewer timed out")
		}
	}
	for range phones {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
