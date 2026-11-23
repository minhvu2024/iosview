package rfb

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestReadClipboard(t *testing.T) {
	for _, command := range []bool{false, true} {
		a, b := net.Pipe()
		c := &Client{conn: a, r: bufio.NewReader(a), closed: make(chan struct{}), extClip: true}
		ready := make(chan struct{})
		go c.readLoop(time.Hour)
		go func() {
			defer b.Close()
			io.ReadFull(b, make([]byte, 10)) // initial framebuffer request
			close(ready)
			if command {
				keys := make([]byte, 32)
				io.ReadFull(b, keys)
				if keys[0] != 4 || keys[1] != 1 || keys[25] != 0 {
					t.Error("bad Command key sequence")
				}
			}
			msg := make([]byte, 12)
			io.ReadFull(b, msg)
			if binary.BigEndian.Uint32(msg[8:]) != extClipRequest|extClipText {
				t.Error("missing text request")
			}
			var z bytes.Buffer
			zw := zlib.NewWriter(&z)
			text := []byte("Tiếng Việt\nline 2\n\x00")
			binary.Write(zw, binary.BigEndian, uint32(len(text)))
			zw.Write(text)
			zw.Close()
			h := make([]byte, 12)
			h[0] = 3
			binary.BigEndian.PutUint32(h[4:], negLen(4+z.Len()))
			binary.BigEndian.PutUint32(h[8:], extClipProvide|extClipText)
			b.Write(append(h, z.Bytes()...))
			<-c.closed
		}()
		<-ready
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		got, err := c.ReadClipboard(ctx, command)
		cancel()
		c.Close()
		if err != nil || got != "Tiếng Việt\nline 2\n" {
			t.Fatalf("command=%v: %q, %v", command, got, err)
		}
	}
}

func TestClipboardTimeoutAndEcho(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := &Client{conn: a, closed: make(chan struct{})}
	c.deliverClip("echo")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if got, err := c.ReadClipboard(ctx, false); err != nil || got != "echo" {
		t.Fatalf("echo: %q %v", got, err)
	}
	c.deliverClip("")
	if got, err := c.ReadClipboard(ctx, false); err != nil || got != "" {
		t.Fatalf("empty: %q %v", got, err)
	}
	c.extClip = true
	c.clipKnown = false
	// A blocked write must respect the timeout too.
	if _, err := c.ReadClipboard(ctx, false); err == nil {
		t.Fatal("expected timeout")
	}
	if len(c.clipWait) != 0 {
		t.Fatal("leaked waiter")
	}
}

func TestReadKnownExtendedClipboard(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := &Client{conn: a, closed: make(chan struct{}), extClip: true}
	c.deliverClip("đã copy\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	got, err := c.ReadClipboard(ctx, false)
	if err != nil || got != "đã copy\n" {
		t.Fatalf("cached extended: %q %v", got, err)
	}
}
