package device

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParkGroupKeepsUnselectedPhoneOnline(t *testing.T) {
	t.Setenv("USBMUXD_SOCKET_ADDRESS", "127.0.0.1:1")
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		cfg.WifiDevices = append(cfg.WifiDevices, newFakeVNC(t, 375, 667).ln.Addr().String())
	}
	m := NewManager(cfg)
	waitOnline(t, m, 3)
	first, middle, last := "wifi:"+cfg.WifiDevices[0], "wifi:"+cfg.WifiDevices[1], "wifi:"+cfg.WifiDevices[2]
	m.ApplyParkedGroup([]string{first, last, last, "missing"}, true)
	view := ids(m)
	if !view[first].Parked || !view[last].Parked || view[first].InSync || view[last].InSync || view[middle].Parked || view[middle].Status != "online" {
		t.Fatalf("unexpected group state: %+v", view)
	}
	m.ApplyParked(first, false)
	if view = ids(m); view[first].Parked || !view[last].Parked {
		t.Fatalf("restoring one phone changed another slot: %+v", view)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		m.mu.Lock()
		saving := m.saveRunning
		m.mu.Unlock()
		if !saving {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("group state was not saved")
		}
		time.Sleep(time.Millisecond)
	}
	saved, err := LoadConfig(cfg.path)
	if err != nil || len(saved.Parked) != 1 {
		t.Fatalf("saved parked group: %+v, %v", saved, err)
	}
}

func TestParkedPhoneReservesSlotAndRejectsInput(t *testing.T) {
	t.Setenv("USBMUXD_SOCKET_ADDRESS", "127.0.0.1:1")
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	phone := newFakeVNC(t, 375, 667)
	other := newFakeVNC(t, 375, 667)
	cfg.WifiDevices = []string{phone.ln.Addr().String(), other.ln.Addr().String()}
	m := NewManager(cfg)
	waitOnline(t, m, 2)
	id := "wifi:" + phone.ln.Addr().String()
	before := m.Snapshot().Devices
	m.ApplyParked(id, true)
	m.ApplyInSync([]string{id, before[1].ID}, true)
	after := m.Snapshot().Devices
	if len(after) != 2 || after[0].ID != id || !after[0].Parked || after[0].InSync ||
		after[0].Width != before[0].Width || after[0].No != before[0].No || after[1].No != before[1].No {
		t.Fatalf("park changed the grid or group: %+v", after)
	}
	ev := Key('a', true)
	if m.Get(id) != nil || m.Dispatch(id, ev) != 0 || m.DispatchOne(id, ev) != 0 ||
		len(m.StartTouch(id, true, Pointer(.5, .5, 1))) != 0 || m.Dispatch("", ev) != 1 {
		t.Fatal("parked phone remained reachable, or stopped another phone")
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	// A parked phone may be absent during the next startup.
	restarted := &Manager{cfg: saved}
	slots := restarted.Snapshot().Devices
	if len(slots) != 1 || !slots[0].Parked || slots[0].No != before[0].No || slots[0].Height != before[0].Height {
		t.Fatalf("saved slot was not restored: %+v", slots)
	}
	m.ApplyParked(id, false)
	waitOnline(t, m, 2)
	restored := m.Snapshot().Devices[0]
	if restored.Parked || restored.No != before[0].No || restored.InSync {
		t.Fatalf("restored phone unexpectedly changed slot or rejoined sync: %+v", restored)
	}
	restarted.devices = make(map[string]*Device)
	restarted.cfg.path = filepath.Join(t.TempDir(), "restart.json")
	restarted.ApplyParked(id, false)
	offline := restarted.Snapshot().Devices
	if len(offline) != 1 || offline[0].Parked || offline[0].No != before[0].No || offline[0].Status != "offline" {
		t.Fatalf("restoring before reconnect should retain the offline slot: %+v", offline)
	}
	if err := restarted.save(); err != nil {
		t.Fatal(err)
	}
}
