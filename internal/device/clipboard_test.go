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
	go func() { result, _ := m.CopyClipboards(context.Background(), true, false); done <- result.Text }()
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
	empty, err := m.CopyClipboards(context.Background(), false, false)
	if empty.Text != "" || err == nil {
		t.Fatalf("all online: %q", empty.Text)
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
		if got, err := m.CopyClipboards(context.Background(), false, tc.blank); got.Text != tc.want || err != nil {
			t.Fatalf("blank=%v: %q", tc.blank, got.Text)
		}
	}
}

func TestPhoneClipRoutingAndAdaptiveBatch(t *testing.T) {
	oldMax := clipMaxWindow
	clipMaxWindow = 650 * time.Millisecond
	defer func() { clipMaxWindow = oldMax }()

	gotCh := make(chan string, 8)
	m := &Manager{
		syncOn: true,
		writeClip: func(s string) error {
			gotCh <- s
			return nil
		},
		devices: map[string]*Device{
			"a": {ID: "a", No: 1, Status: "online", InSync: true, queue: make(chan Event, 4)},
			"b": {ID: "b", No: 2, Status: "online", InSync: true, queue: make(chan Event, 4)},
			"c": {ID: "c", No: 3, Status: "online", InSync: false, queue: make(chan Event, 4)},
		},
	}

	// 1. Phone outside sync group writes immediately with no blank lines.
	m.publishClip("c", "solo-outside")
	select {
	case got := <-gotCh:
		if got != "solo-outside" {
			t.Fatalf("outside group: got %q", got)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("outside group clip delayed")
	}

	// 2. Solo interaction (Alt / DispatchOne) on a sync-group phone writes immediately.
	m.DispatchOne("a", Pointer(0.5, 0.5, 1))
	m.publishClip("a", "solo-alt")
	select {
	case got := <-gotCh:
		if got != "solo-alt" {
			t.Fatalf("solo alt: got %q", got)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("solo alt clip delayed")
	}

	// 3. Group interaction clears solo state; all online group phones flush immediately.
	m.Dispatch("a", Pointer(0.5, 0.5, 1))
	m.publishClip("b", "two")
	m.publishClip("a", "one")
	select {
	case got := <-gotCh:
		if got != "one\ntwo" {
			t.Fatalf("full group: got %q", got)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("full group did not flush immediately")
	}

	copies, unsubscribe := m.SubscribeClipboardCopies()
	defer unsubscribe()

	// 4. A late group member still belongs to the same copy.
	m.publishClip("a", "early")
	select {
	case <-gotCh:
		t.Fatal("group flushed before late member could reply")
	case <-time.After(450 * time.Millisecond):
	}
	m.publishClip("b", "late")
	select {
	case got := <-gotCh:
		if got != "early\nlate" {
			t.Fatalf("late member: got %q", got)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("late group not written")
	}

	select {
	case result := <-copies:
		if len(result.Parts) != 2 || result.Parts[0] != (ClipboardPart{ID: "a", Text: "early", Read: true}) {
			t.Fatalf("invalid copy metadata: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("missing copy metadata")
	}

	// 5. Missing group members leave placeholders at the collection deadline.
	start := time.Now()
	m.publishClip("a", "previous")
	m.publishClip("a", "only-a")
	select {
	case got := <-gotCh:
		if got != "only-a\n" {
			t.Fatalf("partial group: got %q", got)
		}
		if elapsed := time.Since(start); elapsed > 900*time.Millisecond {
			t.Fatalf("partial group took too long: %v", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("partial group did not flush")
	}
}
