package device

import (
	"io"
	"net"

	"iosview/internal/usbmux/fakemux"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// oneDevice starts a manager with a single fake phone (no usbmuxd).
func oneDevice(t *testing.T) (*Manager, *fakeVNC, *Device) {
	t.Helper()
	t.Setenv("USBMUXD_SOCKET_ADDRESS", "127.0.0.1:1")
	f := newFakeVNC(t, 1000, 2000)
	cfg, _ := LoadConfig(filepath.Join(t.TempDir(), "c.json"))
	cfg.WifiDevices = []string{f.ln.Addr().String()}
	m := NewManager(cfg)
	waitOnline(t, m, 1)
	return m, f, m.Get("wifi:" + f.ln.Addr().String())
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func lastPtr(f *fakeVNC) ([3]int, int) {
	p, _, _ := f.snapshot()
	if len(p) == 0 {
		return [3]int{-1, -1, -1}, 0
	}
	return p[len(p)-1], len(p)
}

// A drag that queues up behind slow work (typing) must still start where the
// finger went down: coalescing may merge moves, never move the press.
func TestCoalesceKeepsPressPosition(t *testing.T) {
	m, f, d := oneDevice(t)
	m.Dispatch(d.ID, Text("abc")) // keeps the writer busy ~250 ms
	m.Dispatch(d.ID, Pointer(0, 0.5, 1))
	for i := 1; i <= 20; i++ {
		m.Dispatch(d.ID, Pointer(float64(i)*0.02, 0.5, 1))
	}
	m.Dispatch(d.ID, Pointer(0.4, 0.5, 0))
	waitFor(t, "release", func() bool { p, _ := lastPtr(f); return p[0] == 0 })
	p, _, _ := f.snapshot()
	if p[0] != [3]int{1, 0, 1000} {
		t.Fatalf("drag must start at the press point, got %v", p)
	}
	if len(p) > 4 {
		t.Errorf("moves were not coalesced: %d events", len(p))
	}
}

// When a phone stops consuming input and its queue overflows, no release may
// be lost: the session is ended, the stale backlog is never replayed, and the
// phone works again after the reconnect.
func TestOverflowNeverLeavesTouchDown(t *testing.T) {
	m, f, d := oneDevice(t)
	m.Dispatch(d.ID, Text("abcdefghijklmnopqrstuvwxyz")) // writer busy ~2 s
	m.Dispatch(d.ID, Pointer(0.1, 0.1, 1))
	for i := 0; i < queueSize+600; i++ {
		m.Dispatch(d.ID, Pointer(0.2, 0.2+float64(i%100)/1000, 1))
	}
	m.Dispatch(d.ID, Pointer(0.3, 0.3, 0)) // this release used to be dropped
	time.Sleep(3 * time.Second)            // long enough for the typing and any backlog
	if p, _ := lastPtr(f); p[0] > 0 {
		t.Fatalf("touch left down after overflow: %v", p)
	}
	waitOnline(t, m, 1)
	_, n0 := lastPtr(f)
	m.Dispatch(d.ID, Pointer(0.5, 0.5, 1))
	m.Dispatch(d.ID, Pointer(0.5, 0.5, 0))
	waitFor(t, "new input", func() bool { _, n := lastPtr(f); return n >= n0+2 })
	p, _, _ := f.snapshot()
	if len(p) > n0+2 {
		t.Errorf("stale input replayed after reconnect: %v", p[n0:])
	}
}

// Switching path (USB <-> Wi-Fi) while a finger is down: the new session lifts
// it, and only one writer ever drains the queue.
func TestRerouteLiftsHeldTouch(t *testing.T) {
	m, f, d := oneDevice(t)
	m.Dispatch(d.ID, Pointer(0.25, 0.75, 1))
	waitFor(t, "press", func() bool { p, _ := lastPtr(f); return p[0] == 1 })
	for i := 0; i < 5; i++ {
		m.mu.Lock()
		m.rerouteLocked(d)
		m.mu.Unlock()
		time.Sleep(30 * time.Millisecond)
	}
	waitOnline(t, m, 1)
	waitFor(t, "release on new session", func() bool { p, _ := lastPtr(f); return p == [3]int{0, 250, 1499} })
	_, n0 := lastPtr(f)
	m.Dispatch(d.ID, Pointer(0.5, 0.5, 1))
	m.Dispatch(d.ID, Pointer(0.5, 0.5, 0))
	waitFor(t, "input after reroute", func() bool { _, n := lastPtr(f); return n == n0+2 })
	time.Sleep(100 * time.Millisecond)
	if _, n := lastPtr(f); n != n0+2 {
		t.Fatalf("events duplicated or lost after reroute: %d new", n-n0)
	}
}

// Alt (individual control): in sync mode, DispatchOne reaches only that phone.
func TestDispatchOneIgnoresSyncGroup(t *testing.T) {
	t.Setenv("USBMUXD_SOCKET_ADDRESS", "127.0.0.1:1")
	a, b := newFakeVNC(t, 100, 200), newFakeVNC(t, 100, 200)
	cfg, _ := LoadConfig(filepath.Join(t.TempDir(), "c.json"))
	cfg.WifiDevices = []string{a.ln.Addr().String(), b.ln.Addr().String()}
	m := NewManager(cfg)
	waitOnline(t, m, 2)
	idA := "wifi:" + a.ln.Addr().String()
	m.SetSync(true)
	if n := m.DispatchOne(idA, Pointer(0.5, 0.5, 1)); n != 1 {
		t.Fatalf("DispatchOne targets %d devices", n)
	}
	m.DispatchOne(idA, Pointer(0.5, 0.5, 0))
	waitFor(t, "A input", func() bool { _, n := lastPtr(a); return n == 2 })
	time.Sleep(100 * time.Millisecond)
	if _, n := lastPtr(b); n != 0 {
		t.Fatalf("B got input meant for A only")
	}
	if n := m.Dispatch(idA, Pointer(0.5, 0.5, 0)); n != 2 {
		t.Fatalf("normal dispatch should still reach the group, got %d", n)
	}
}

// Force paste types the text (accents stripped) even when the setting says
// "put it on the clipboard".
func TestForceTextTypes(t *testing.T) {
	m, f, d := oneDevice(t)
	m.mu.Lock()
	m.cfg.StripAccents = false
	m.mu.Unlock()
	m.Dispatch(d.ID, Text("Việt"))
	m.Dispatch(d.ID, ForceText("Đi"))
	waitFor(t, "typing", func() bool { _, k, _ := f.snapshot(); return len(k) >= 3 })
	_, keys, clip := f.snapshot()
	if len(clip) != 1 || clip[0] != "Việt" {
		t.Errorf("normal text should go to the clipboard: %q", clip)
	}
	// "Đi" → "Di": Shift, d, i
	if keys[0] != ksShiftL || keys[1] != 'd' || keys[2] != 'i' {
		t.Errorf("force text not typed: %v", keys)
	}
}

func TestUIPrefsRoundTrip(t *testing.T) {
	m, path := newTestManager(t)
	if string(m.UIPrefs()) != "{}" {
		t.Fatalf("empty prefs = %s", m.UIPrefs())
	}
	if err := m.SetUIPrefs([]byte(`[1]`)); err == nil {
		t.Fatal("non-object accepted")
	}
	if err := m.SetUIPrefs([]byte(`{"notes":"ghi chú","phrases":["a"]}`)); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg.UI), "ghi chú") {
		t.Fatalf("not saved: %s", cfg.UI)
	}
}

// A cable phone is being dialed over its Wi-Fi address (cable not seen yet),
// then the cable shows up and the session moves to USB. When the abandoned
// Wi-Fi dial times out later it must not mark the working USB session failed.
func TestStaleWifiDialDoesNotOverrideUSB(t *testing.T) {
	vnc := newFakeVNC(t, 100, 200)
	mux, _ := net.Listen("tcp", "127.0.0.1:0")
	defer mux.Close()
	go fakemux.Serve(mux, []fakemux.Phone{{ID: 7, UDID: "U1", Target: map[int]string{5901: vnc.ln.Addr().String()}}})
	// The cable is reported only while the first Wi-Fi dial is in flight
	// (it starts after usbGrace and lasts lanTimeout): the watcher's first
	// connection to usbmuxd is held back until then.
	front, _ := net.Listen("tcp", "127.0.0.1:0")
	defer front.Close()
	go func() {
		first := true
		for {
			c, err := front.Accept()
			if err != nil {
				return
			}
			delay := time.Duration(0)
			if first {
				delay, first = usbGrace+lanTimeout/2, false
			}
			go func() {
				time.Sleep(delay)
				up, err := net.Dial("tcp", mux.Addr().String())
				if err != nil {
					c.Close()
					return
				}
				go func() { io.Copy(up, c); up.Close() }()
				io.Copy(c, up)
				c.Close()
			}()
		}
	}()
	t.Setenv("USBMUXD_SOCKET_ADDRESS", front.Addr().String())
	// 192.0.2.x never answers (see dial_test.go), lanTimeout is 1.5 s in tests
	cfg, _ := LoadConfig(filepath.Join(t.TempDir(), "c.json"))
	cfg.WifiDevices = []string{"192.0.2.1:5901"}
	cfg.WifiLinks = map[string]string{"192.0.2.1:5901": "U1"}
	m := NewManager(cfg)
	deadline := time.Now().Add(10 * time.Second)
	for {
		d := m.Snapshot().Devices
		if len(d) == 1 && d[0].Status == "online" && d[0].Transport == "usb" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never online over USB: %+v", d)
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(lanTimeout + time.Second) // any abandoned Wi-Fi dial has timed out by now
	d := m.Snapshot().Devices
	if len(d) != 1 || d[0].Status != "online" || d[0].Transport != "usb" || d[0].Error != "" {
		t.Fatalf("USB session overridden by stale Wi-Fi dial: %+v", d)
	}
	if n := m.Dispatch("usb:U1", Pointer(0.5, 0.5, 1)); n != 1 {
		t.Fatalf("phone not targeted: %d", n)
	}
}

// At start, a phone with a cable connects over USB without ever trying its
// saved Wi-Fi address.
func TestStartupPrefersCable(t *testing.T) {
	vnc := newFakeVNC(t, 100, 200)
	mux, _ := net.Listen("tcp", "127.0.0.1:0")
	defer mux.Close()
	go fakemux.Serve(mux, []fakemux.Phone{{ID: 9, UDID: "U9", Target: map[int]string{5901: vnc.ln.Addr().String()}}})
	t.Setenv("USBMUXD_SOCKET_ADDRESS", mux.Addr().String())
	cfg, _ := LoadConfig(filepath.Join(t.TempDir(), "c.json"))
	cfg.WifiDevices = []string{"192.0.2.9:5901"}
	cfg.WifiLinks = map[string]string{"192.0.2.9:5901": "U9"}
	m := NewManager(cfg)
	waitOnline(t, m, 1)
	time.Sleep(usbGrace + 300*time.Millisecond)
	if d := m.Snapshot().Devices; len(d) != 1 || d[0].Transport != "usb" || d[0].Status != "online" {
		t.Fatalf("got %+v", d)
	}
	if n := attemptsTo("192.0.2.9:5901"); n != 0 {
		t.Fatalf("Wi-Fi dialed %d times although the cable is there", n)
	}
}
