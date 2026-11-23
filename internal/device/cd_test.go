package device

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"iosview/internal/plist"
	"iosview/internal/usbmux/fakemux"
)

func TestIOSAtLeast(t *testing.T) {
	for v, want := range map[string]bool{"17.4": true, "17.4.1": true, "17.3.1": false, "18.0": true, "16.7": false, "": false, "26.1": true} {
		if got := iosAtLeast(v, 17, 4); got != want {
			t.Errorf("iosAtLeast(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestHIDUsage(t *testing.T) {
	cases := map[uint32][2]int{'a': {0x04, 0}, 'Z': {0x1d, 1}, '1': {0x1e, 0}, '0': {0x27, 0}, '!': {0x1e, 1}, '?': {0x38, 1}, ' ': {0x2c, 0}, 0xff0d: {0x28, 0}, 0xff08: {0x2a, 0}, 0xffeb: {0xe3, 0}}
	for ks, want := range cases {
		u, sh := hidUsage(ks)
		if int(u) != want[0] || sh != (want[1] == 1) {
			t.Errorf("hidUsage(%#x) = %#x,%v want %#x,%v", ks, u, sh, want[0], want[1] == 1)
		}
	}
}

// fakeLockdown answers GetValue ProductVersion like lockdownd.
func fakeLockdown(t *testing.T, version string) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var h [4]byte
				if _, err := io.ReadFull(c, h[:]); err != nil {
					return
				}
				io.ReadFull(c, make([]byte, binary.BigEndian.Uint32(h[:])))
				body, _ := plist.Marshal(map[string]any{"Key": "ProductVersion", "Value": version})
				binary.BigEndian.PutUint32(h[:], uint32(len(body)))
				c.Write(append(h[:], body...))
			}(c)
		}
	}()
	return ln.Addr().String()
}

// TestCoreDevicePhone: a cable phone on iOS 17.5 without TrollVNC is driven
// through the sidecar — touches, buttons, native paste with accents, and the
// phone's own copies coming back.
func TestCoreDevicePhone(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}
	src, _ := filepath.Abs("testdata/fake_cd.py")
	t.Setenv("IOSVIEW_CD", src)
	logPath := filepath.Join(t.TempDir(), "req.log")
	t.Setenv("FAKE_CD_LOG", logPath)

	mux, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer mux.Close()
	go fakemux.Serve(mux, []fakemux.Phone{{ID: 7, UDID: "00008110-CD", Target: map[int]string{62078: fakeLockdown(t, "17.5")}}})
	t.Setenv("USBMUXD_SOCKET_ADDRESS", mux.Addr().String())

	dir, _ := os.MkdirTemp("", "iosview-cd")
	defer func() { time.Sleep(50 * time.Millisecond); os.RemoveAll(dir) }()
	cfg, err := LoadConfig(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(cfg)
	clips, unclip := m.SubscribeClipboard()
	defer unclip()

	var d Device
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if got := ids(m)["usb:00008110-CD"]; got.Status == "online" {
			d = got
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if d.Status != "online" {
		t.Fatalf("phone not online: %+v", ids(m)["usb:00008110-CD"])
	}
	if d.Kind != "cd" || d.IOS != "17.5" || d.Width != 1179 || d.Height != 2556 || d.VNCName != "Fake 15" {
		t.Fatalf("unexpected device %+v", d)
	}
	if m.Get(d.ID).CDBase() == "" {
		t.Fatal("no sidecar URL")
	}

	m.Dispatch(d.ID, Pointer(0.5, 0.25, 1))
	m.Dispatch(d.ID, Pointer(0.5, 0.30, 0))
	m.Dispatch(d.ID, Button("home"))
	m.Dispatch(d.ID, Text("Nguyễn Văn A 😀"))
	m.Dispatch(d.ID, Key('A', true))
	m.Dispatch(d.ID, Key('A', false))

	select {
	case c := <-clips:
		if c.Text != "từ iPhone" || c.Device != d.ID {
			t.Fatalf("clipboard event %+v", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no clipboard event from the phone")
	}

	want := []string{
		`/touch {"type":"contact","x":32768,"y":16384}`,
		`/touch {"type":"release","x":32768,"y":19661}`,
		`/button {"name":"home","state":"down"}`,
		`/button {"name":"home","state":"up"}`,
		`/clipboard {"text":"Nguyễn Văn A 😀"}`,
		`/key {"usages":[227]}`,
		`/key {"usages":[227,25]}`,
		`/key {"usages":[227]}`,
		`/key {"usages":[]}`,
		`/key {"usages":[225,4]}`,
		`/key {"usages":[]}`,
	}
	var got []string
	for time.Now().Before(deadline) {
		got = readReqs(t, logPath)
		if len(got) >= len(want) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func readReqs(t *testing.T, path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r struct{ Path, Body string }
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r.Path+" "+r.Body)
		}
	}
	return out
}

// An iOS 15 phone without TrollVNC stays a TrollVNC phone (port-closed error),
// and an iOS 17 phone without the sidecar says what is missing.
func TestCoreDeviceOnlyForIOS17(t *testing.T) {
	for _, tc := range []struct{ ios, kind, errHas string }{
		{"15.8", "", "TrollVNC"},
		{"17.5", "cd", "iosview-cd"},
	} {
		t.Run(tc.ios, func(t *testing.T) {
			t.Setenv("IOSVIEW_CD", filepath.Join(t.TempDir(), "missing.exe"))
			mux, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer mux.Close()
			go fakemux.Serve(mux, []fakemux.Phone{{ID: 3, UDID: "U-" + tc.ios, Target: map[int]string{62078: fakeLockdown(t, tc.ios)}}})
			t.Setenv("USBMUXD_SOCKET_ADDRESS", mux.Addr().String())
			dir, _ := os.MkdirTemp("", "iosview-cd")
			defer func() { time.Sleep(50 * time.Millisecond); os.RemoveAll(dir) }()
			cfg, _ := LoadConfig(filepath.Join(dir, "c.json"))
			m := NewManager(cfg)
			var d Device
			for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
				if d = ids(m)["usb:U-"+tc.ios]; d.Status == "error" {
					break
				}
			}
			if d.Status != "error" || d.Kind != tc.kind || d.IOS != tc.ios || !strings.Contains(d.Error, tc.errHas) {
				t.Fatalf("got %+v", d)
			}
		})
	}
}
