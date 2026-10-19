package device

import (
	"fmt"
	"strings"
	"testing"
)

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"  Kho A‑1  ":           "Kho A‑1",
		"a\tb\nc":               "a b c",
		"":                      "",
		strings.Repeat("x", 40): strings.Repeat("x", MaxNameLen),
		"Máy   bán   hàng":      "Máy bán hàng",
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNumbersRenameAndTags(t *testing.T) {
	m, path := newTestManager(t)
	m.AddWifi("10.9.9.1")
	m.AddWifi("10.9.9.2")
	a, b := "wifi:10.9.9.1:1", "wifi:10.9.9.2:1"
	d := ids(m)
	if d[a].No != 1 || d[b].No != 2 {
		t.Fatalf("numbers = %d, %d; want 1, 2", d[a].No, d[b].No)
	}

	m.Rename(a, "  Live   1 ")
	if got := ids(m)[a].Name; got != "Live 1" {
		t.Fatalf("name = %q", got)
	}
	m.Rename(a, "   ")
	if got := ids(m)[a].Name; got != "" {
		t.Fatalf("empty rename should restore default, got %q", got)
	}
	m.Rename(a, "Kho 1")

	m.SetTag("Kho", []string{a, b, a}, "")
	m.SetTag("Kho A", nil, "Kho") // rename only, keep members
	st := m.Snapshot()
	if len(st.Tags) != 1 || st.Tags[0].Name != "Kho A" || len(st.Tags[0].IDs) != 2 {
		t.Fatalf("tags = %+v", st.Tags)
	}

	// Everything survives a restart.
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Names[a] != "Kho 1" || cfg.Numbers[b] != 2 || len(cfg.Tags) != 1 {
		t.Fatalf("not persisted: names=%v numbers=%v tags=%v", cfg.Names, cfg.Numbers, cfg.Tags)
	}

	// Removing a phone frees its number and drops it from tags.
	m.Remove(a)
	m.AddWifi("10.9.9.3")
	if n := ids(m)["wifi:10.9.9.3:1"].No; n != 1 {
		t.Fatalf("new phone got number %d, want the freed 1", n)
	}
	if st := m.Snapshot(); len(st.Tags[0].IDs) != 1 || st.Tags[0].IDs[0] != b {
		t.Fatalf("tag after remove = %+v", st.Tags[0])
	}
	m.DeleteTag("Kho A")
	if st := m.Snapshot(); len(st.Tags) != 0 {
		t.Fatalf("tag not deleted: %+v", st.Tags)
	}
}

// A Wi-Fi entry merged into its cable device keeps its number, name and tags.
func TestLinkKeepsNumberNameTags(t *testing.T) {
	m, _ := newTestManager(t)
	m.AddWifi("10.9.9.7")
	w := "wifi:10.9.9.7:1"
	m.Rename(w, "Live 7")
	m.SetTag("Live", []string{w}, "")
	no := ids(m)[w].No

	m.mu.Lock()
	m.linkLocked("10.9.9.7:1", "abc123")
	m.mu.Unlock()

	d, ok := ids(m)["usb:abc123"]
	if !ok {
		t.Fatal("merged device missing")
	}
	if d.No != no || d.Name != "Live 7" {
		t.Fatalf("merged device No=%d Name=%q, want %d %q", d.No, d.Name, no, "Live 7")
	}
	if tg := m.Snapshot().Tags[0]; len(tg.IDs) != 1 || tg.IDs[0] != "usb:abc123" {
		t.Fatalf("tag not moved to the cable id: %+v", tg)
	}
}

func TestSetNumberSwapsAndSorts(t *testing.T) {
	m, path := newTestManager(t)
	for _, a := range []string{"10.9.8.1", "10.9.8.2", "10.9.8.3"} {
		m.AddWifi(a)
	}
	a, c := "wifi:10.9.8.1:1", "wifi:10.9.8.3:1"
	if !m.SetNumber(c, 1) { // 3 → 1: the phone that had 1 gets 3
		t.Fatal("SetNumber failed")
	}
	got := []string{}
	for _, d := range m.Snapshot().Devices {
		got = append(got, fmt.Sprintf("%d=%s", d.No, d.ID))
	}
	want := "[1=wifi:10.9.8.3:1 2=wifi:10.9.8.2:1 3=wifi:10.9.8.1:1]"
	if fmt.Sprint(got) != want {
		t.Fatalf("order = %v, want %s", got, want)
	}
	if m.SetNumber(a, 0) || m.SetNumber(a, 1000) {
		t.Fatal("out-of-range number accepted")
	}
	m.SetNumber(a, 42)
	if cfg, _ := LoadConfig(path); cfg.Numbers[a] != 42 || cfg.Numbers[c] != 1 {
		t.Fatalf("numbers not saved: %v", cfg.Numbers)
	}
}
