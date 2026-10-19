package device

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"iosview/internal/usbmux"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	// A usbmuxd that accepts and stays silent: Listen blocks, so the watcher
	// does not report "usbmuxd gone" in the middle of a test.
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
			go func() { io.Copy(io.Discard, c) }()
		}
	}()
	t.Setenv("USBMUXD_SOCKET_ADDRESS", ln.Addr().String())
	// not t.TempDir(): background saves may still be writing at cleanup time
	dir, _ := os.MkdirTemp("", "iosview-test")
	t.Cleanup(func() { time.Sleep(50 * time.Millisecond); os.RemoveAll(dir) })
	path := filepath.Join(dir, "c.json")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.VNCPort = 1 // nothing listens: connections just fail
	return NewManager(cfg), path
}

func links(m *Manager) (map[string]string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for k, v := range m.cfg.WifiLinks {
		out[k] = v
	}
	return out, len(m.cfg.WifiDevices)
}

func ids(m *Manager) map[string]Device {
	out := map[string]Device{}
	for _, d := range m.Snapshot().Devices {
		out[d.ID] = d
	}
	return out
}

func TestUSBAndWifiAreOneDevice(t *testing.T) {
	m, path := newTestManager(t)
	m.AddWifi("127.0.0.1:1")
	if got := ids(m); len(got) != 1 || got["wifi:127.0.0.1:1"].Transport != "wifi" {
		t.Fatalf("want one wifi device, got %v", got)
	}
	// cable shows up: separate until the phone is identified
	m.onUSB(usbmux.Event{Attached: true, Device: usbmux.Device{DeviceID: 5, SerialNumber: "U1", ConnectionType: "USB"}})
	if got := ids(m); len(got) != 2 {
		t.Fatalf("want 2 devices before identification, got %d", len(got))
	}
	m.Link("wifi:127.0.0.1:1", "usb:U1")
	got := ids(m)
	if len(got) != 1 {
		t.Fatalf("want 1 device after link, got %v", got)
	}
	d := got["usb:U1"]
	if l, _ := links(m); d.Transport != "usb" || d.Wifi != "127.0.0.1:1" || l["127.0.0.1:1"] != "U1" {
		t.Fatalf("bad merged device: %+v links=%v", d, l)
	}
	// a Wi-Fi-sync entry must not displace the cable
	m.onUSB(usbmux.Event{Attached: true, Device: usbmux.Device{DeviceID: 9, SerialNumber: "U1", ConnectionType: "Network"}})
	m.mu.Lock()
	if m.devices["usb:U1"].usbID != 5 {
		t.Fatal("network entry replaced the cable")
	}
	m.mu.Unlock()
	// unplug: Wi-Fi takes over, still one device
	m.onUSB(usbmux.Event{Attached: false, Device: usbmux.Device{DeviceID: 5}})
	d = ids(m)["usb:U1"]
	if d.Transport != "wifi" || d.Addr != "127.0.0.1:1" || len(ids(m)) != 1 {
		t.Fatalf("want wifi fallback, got %+v", d)
	}
	// plug again: cable wins again
	m.onUSB(usbmux.Event{Attached: true, Device: usbmux.Device{DeviceID: 6, SerialNumber: "U1", ConnectionType: "USB"}})
	if d = ids(m)["usb:U1"]; d.Transport != "usb" || len(ids(m)) != 1 {
		t.Fatalf("want usb again, got %+v", d)
	}
	// restart: the saved link keeps it a single device before any USB event
	m.save()
	cfg2, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg2.VNCPort = 1
	m2 := NewManager(cfg2)
	if got := ids(m2); len(got) != 1 || got["usb:U1"].Transport != "wifi" {
		t.Fatalf("after restart: %v", got)
	}
	// forgetting the Wi-Fi address: device vanishes when no cable
	m2.Remove("usb:U1")
	if l, n := links(m2); len(ids(m2)) != 0 || len(l) != 0 || n != 0 {
		t.Fatalf("after remove: %v %v %v", ids(m2), l, n)
	}
}

func TestNetworkEntryLinksLAN(t *testing.T) {
	m, _ := newTestManager(t)
	m.AddWifi("10.1.2.3:1")
	m.onUSB(usbmux.Event{Attached: true, Device: usbmux.Device{DeviceID: 3, SerialNumber: "U2", ConnectionType: "Network", NetAddr: "10.1.2.3"}})
	// VNCPort is 1 in tests, so the address matches
	if got := ids(m); len(got) != 1 || got["usb:U2"].Wifi != "10.1.2.3:1" {
		t.Fatalf("got %v", got)
	}
}

func TestProbeIdent(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		buf := make([]byte, 1)
		c.Read(buf)
		if buf[0] != 'I' {
			return
		}
		frame := func(flags byte, p string) []byte {
			h := make([]byte, 12)
			binary.BigEndian.PutUint32(h, uint32(len(p)))
			h[8] = flags
			return append(h, p...)
		}
		c.Write(frame(1, "videoframe"))
		c.Write(frame(2, "UDID-XYZ\n"))
		c.Close()
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	got, err := probeIdent("127.0.0.1:"+strconv.Itoa(port-10), 0, 0)
	if err != nil || got != "UDID-XYZ" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// Quality values dragged to the floor by 0.10.3 are brought back once.
func TestConfigV3RaisesLowQuality(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(path, []byte(`{"quality":0,"focusQuality":4,"streamVer":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Quality != 5 || c.FocusQuality != 7 || c.StreamVer != 3 {
		t.Fatalf("got q=%d fq=%d v=%d", c.Quality, c.FocusQuality, c.StreamVer)
	}
	// already migrated: the user's own low choice stays
	os.WriteFile(path, []byte(`{"quality":1,"focusQuality":4,"streamVer":3}`), 0o644)
	c, _ = LoadConfig(path)
	if c.Quality != 1 || c.FocusQuality != 4 {
		t.Fatalf("v3 config changed: q=%d fq=%d", c.Quality, c.FocusQuality)
	}
}

// A linked phone that got a new IP (seen through usbmuxd Wi-Fi sync) is
// dialed at the new address from then on.
func TestNetworkEntryUpdatesChangedIP(t *testing.T) {
	m, path := newTestManager(t)
	m.AddWifi("10.1.2.3:1")
	m.onUSB(usbmux.Event{Attached: true, Device: usbmux.Device{DeviceID: 3, SerialNumber: "U2", ConnectionType: "Network", NetAddr: "10.1.2.3"}})
	m.onUSB(usbmux.Event{Attached: false, Device: usbmux.Device{DeviceID: 3}})
	m.onUSB(usbmux.Event{Attached: true, Device: usbmux.Device{DeviceID: 4, SerialNumber: "U2", ConnectionType: "Network", NetAddr: "10.1.2.99"}})
	if got := ids(m); len(got) != 1 || got["usb:U2"].Wifi != "10.1.2.99:1" {
		t.Fatalf("got %v", got)
	}
	m.save()
	cfg, _ := LoadConfig(path)
	if len(cfg.WifiDevices) != 1 || cfg.WifiDevices[0] != "10.1.2.99:1" || cfg.WifiLinks["10.1.2.99:1"] != "U2" || cfg.WifiLinks["10.1.2.3:1"] != "" {
		t.Fatalf("config not updated: %v %v", cfg.WifiDevices, cfg.WifiLinks)
	}
}
