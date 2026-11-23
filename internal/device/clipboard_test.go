package device

import (
	"context"
	"errors"
	"testing"
	"time"
)

type clipboardSession struct {
	session
	text    string
	started chan bool
	block   bool
	err     error
}

func (c *clipboardSession) ReadClipboard(ctx context.Context, command bool) (string, error) {
	c.started <- command
	if c.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return c.text, c.err
}

func TestCopyClipboards(t *testing.T) {
	started := make(chan bool, 3)
	m := &Manager{devices: map[string]*Device{
		"a": {ID: "a", No: 7, Name: "Tên đã đặt", Status: "online", InSync: true, cur: &clipboardSession{text: "a\nb\n", started: started}},
		"b": {ID: "b", No: 2, Name: "Lỗi", Status: "online", InSync: true, cur: &clipboardSession{text: "discard", err: errors.New("failed"), started: started}},
		"c": {ID: "c", No: 5, Name: "Chậm", Status: "online", InSync: true, cur: &clipboardSession{block: true, started: started}},
		"d": {ID: "d", No: 9, Name: "Ngoài nhóm", Status: "online"},
	}}
	done := make(chan string, 1)
	start := time.Now()
	go func() { done <- m.CopyClipboards(context.Background(), true, false) }()
	for range 3 {
		select {
		case cmd := <-started:
			if !cmd {
				t.Fatal("flag lost")
			}
		case <-time.After(time.Second):
			t.Fatal("reads not parallel")
		}
	}
	got := <-done
	want := "\n\na\nb\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if time.Since(start) > 2500*time.Millisecond {
		t.Fatal("timeout exceeded")
	}
	for _, d := range m.devices {
		d.InSync = false
		d.cur = nil
	}
	m.devices["b"].Status = "offline"
	got = m.CopyClipboards(context.Background(), false, false)
	if got != "\n\n" {
		t.Fatalf("all online: %q", got)
	}
}

func TestNoSelectionControlsOnlySource(t *testing.T) {
	m := &Manager{syncOn: true, devices: map[string]*Device{
		"a": {ID: "a", Status: "online"}, "b": {ID: "b", Status: "online"}, "c": {ID: "c", Status: "offline"},
	}}
	if got := len(m.targets("a")); got != 1 {
		t.Fatalf("no selection targets=%d", got)
	}
	m.devices["a"].InSync = true
	if got := len(m.targets("a")); got != 1 {
		t.Fatalf("selected targets=%d", got)
	}
	m.syncOn = false
	if got := len(m.targets("b")); got != 1 {
		t.Fatalf("sync off targets=%d", got)
	}
}

func TestClipboardDataSeparators(t *testing.T) {
	started := make(chan bool, 4)
	m := &Manager{devices: map[string]*Device{
		"b": {ID: "b", No: 2, Name: "Không đưa tên vào dữ liệu", Status: "online", cur: &clipboardSession{text: "B", started: started}},
		"a": {ID: "a", No: 1, Status: "online", cur: &clipboardSession{text: "A\nA2", started: started}},
	}}
	for _, tc := range []struct {
		blank bool
		want  string
	}{{false, "A\nA2\nB"}, {true, "A\nA2\n\nB"}} {
		if got := m.CopyClipboards(context.Background(), false, tc.blank); got != tc.want {
			t.Fatalf("blank=%v: %q", tc.blank, got)
		}
	}
}
