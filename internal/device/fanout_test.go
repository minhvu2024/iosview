package device

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeVNC is a tiny RFB 3.8 server (security None) that records input.
type fakeVNC struct {
	ln    net.Listener
	w, h  uint16
	mu    sync.Mutex
	ptr   [][3]int
	keys  []uint32
	clip  []string
	conns int
}

func newFakeVNC(t *testing.T, w, h uint16) *fakeVNC {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeVNC{ln: ln, w: w, h: h}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeVNC) serve(c net.Conn) {
	defer c.Close()
	f.mu.Lock()
	f.conns++
	f.mu.Unlock()
	r := bufio.NewReader(c)
	c.Write([]byte("RFB 003.008\n"))
	io.ReadFull(r, make([]byte, 12))
	c.Write([]byte{1, 1}) // one type: None
	r.ReadByte()
	c.Write([]byte{0, 0, 0, 0}) // ok
	r.ReadByte()                // shared flag
	si := make([]byte, 24)
	binary.BigEndian.PutUint16(si[0:], f.w)
	binary.BigEndian.PutUint16(si[2:], f.h)
	name := "FakePhone"
	binary.BigEndian.PutUint32(si[20:], uint32(len(name)))
	c.Write(append(si, name...))
	// advertise extended clipboard caps
	caps := make([]byte, 8)
	caps[0] = 3
	neg := int32(-8)
	binary.BigEndian.PutUint32(caps[4:], uint32(neg))
	capb := make([]byte, 8)
	binary.BigEndian.PutUint32(capb, 1<<24|1)
	binary.BigEndian.PutUint32(capb[4:], 1<<20)
	c.Write(append(caps, capb...))
	for {
		t, err := r.ReadByte()
		if err != nil {
			return
		}
		switch t {
		case 0:
			io.ReadFull(r, make([]byte, 19))
		case 2:
			hdr := make([]byte, 3)
			io.ReadFull(r, hdr)
			n := binary.BigEndian.Uint16(hdr[1:])
			io.ReadFull(r, make([]byte, 4*int(n)))
		case 3:
			io.ReadFull(r, make([]byte, 9))
		case 4:
			b := make([]byte, 7)
			io.ReadFull(r, b)
			f.mu.Lock()
			if b[0] == 1 {
				f.keys = append(f.keys, binary.BigEndian.Uint32(b[3:]))
			}
			f.mu.Unlock()
		case 5:
			b := make([]byte, 5)
			io.ReadFull(r, b)
			f.mu.Lock()
			f.ptr = append(f.ptr, [3]int{int(b[0]), int(binary.BigEndian.Uint16(b[1:])), int(binary.BigEndian.Uint16(b[3:]))})
			f.mu.Unlock()
		case 6:
			b := make([]byte, 7)
			io.ReadFull(r, b)
			n := int32(binary.BigEndian.Uint32(b[3:]))
			if n < 0 {
				data := make([]byte, -n)
				io.ReadFull(r, data)
				zr, err := zlib.NewReader(bytes.NewReader(data[4:]))
				if err == nil {
					raw, _ := io.ReadAll(zr)
					sz := binary.BigEndian.Uint32(raw)
					f.mu.Lock()
					f.clip = append(f.clip, string(raw[4:4+sz-1]))
					f.mu.Unlock()
				}
			} else {
				data := make([]byte, n)
				io.ReadFull(r, data)
				f.mu.Lock()
				f.clip = append(f.clip, string(data))
				f.mu.Unlock()
			}
		}
	}
}

func (f *fakeVNC) snapshot() ([][3]int, []uint32, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][3]int{}, f.ptr...), append([]uint32{}, f.keys...), append([]string{}, f.clip...)
}

func waitOnline(t *testing.T, m *Manager, n int) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c := 0
		for _, d := range m.Snapshot().Devices {
			if d.Status == "online" {
				c++
			}
		}
		if c == n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("devices not online: %+v", m.Snapshot())
}

func TestSyncFanout(t *testing.T) {
	t.Setenv("USBMUXD_SOCKET_ADDRESS", "127.0.0.1:1") // no usbmuxd in test
	a := newFakeVNC(t, 390, 844)
	b := newFakeVNC(t, 828, 1792)
	c := newFakeVNC(t, 750, 1334)
	cfg, _ := LoadConfig(filepath.Join(t.TempDir(), "c.json"))
	cfg.StripAccents = false // non-ASCII text goes to the device clipboard
	cfg.WifiDevices = []string{a.ln.Addr().String(), b.ln.Addr().String(), c.ln.Addr().String()}
	m := NewManager(cfg)
	waitOnline(t, m, 3)
	idA, idB, idC := "wifi:"+a.ln.Addr().String(), "wifi:"+b.ln.Addr().String(), "wifi:"+c.ln.Addr().String()

	// c is not in the group
	m.SetInSync([]string{idC}, false)
	m.SetSync(true)

	start := time.Now()
	if n := m.Dispatch(idA, Pointer(0.5, 0.25, 1)); n != 2 {
		t.Fatalf("expected 2 targets, got %d", n)
	}
	m.Dispatch(idA, Pointer(0.5, 0.25, 0))
	m.Dispatch(idB, Key('a', true))
	m.Dispatch(idA, Text("Xin chào Việt Nam"))
	m.Dispatch("", Button("home"))
	// non-sync device only receives its own input
	m.Dispatch(idC, Pointer(1, 1, 1))

	time.Sleep(400 * time.Millisecond)
	pa, ka, ca := a.snapshot()
	pb, _, cb := b.snapshot()
	pc, kc, _ := c.snapshot()
	t.Logf("fanout delivered in < %v", time.Since(start))
	t.Logf("A ptr=%v keys=%v clip=%q", pa, ka, ca)
	t.Logf("B ptr=%v clip=%q", pb, cb)
	t.Logf("C ptr=%v keys=%v", pc, kc)

	if len(pa) < 2 || pa[0] != [3]int{1, 195, 211} {
		t.Errorf("A pointer wrong: %v", pa)
	}
	if len(pb) < 2 || pb[0] != [3]int{1, 414, 448} {
		t.Errorf("B pointer scaled wrong: %v", pb)
	}
	if len(pc) != 1 || pc[0] != [3]int{1, 749, 1333} {
		t.Errorf("C should only get its own event: %v", pc)
	}
	if len(ka) == 0 || ka[0] != 'a' {
		t.Errorf("A should get key from B (sync): %v", ka)
	}
	if len(ca) != 1 || ca[0] != "Xin chào Việt Nam" || len(cb) != 1 {
		t.Errorf("clipboard not delivered: %q %q", ca, cb)
	}
	// home button = mask 4 press + release on both
	foundHome := false
	for _, p := range pa {
		if p[0] == 4 {
			foundHome = true
		}
	}
	if !foundHome {
		t.Errorf("home not delivered: %v", pa)
	}

	// sync off: input on A goes only to A
	m.SetSync(false)
	if n := m.Dispatch(idA, Pointer(0, 0, 1)); n != 1 {
		t.Fatalf("sync off should target 1, got %d", n)
	}
}
