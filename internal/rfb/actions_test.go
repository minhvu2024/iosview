package rfb

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"
)

func TestPhoneActionRoundTrip(t *testing.T) {
	agent, phone := net.Pipe()
	defer agent.Close()
	defer phone.Close()
	c := &Client{conn: agent, r: bufio.NewReader(agent), name: "6s" + actionTag, closed: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		apps, err := c.PhoneAction(ctx, "apps", "")
		if err == nil && (len(apps) != 1 || apps[0].ID != "com.apple.mobilesafari") {
			t.Error("missing Safari")
		}
		result <- err
	}()
	phone.SetDeadline(time.Now().Add(time.Second))
	var hdr [8]byte
	if _, err := io.ReadFull(phone, hdr[:]); err != nil || hdr[0] != 11 {
		t.Fatal("action header", err)
	}
	raw := make([]byte, binary.BigEndian.Uint32(hdr[4:]))
	if _, err := io.ReadFull(phone, raw); err != nil {
		t.Fatal(err)
	}
	var req struct {
		Seq    uint64
		Action string
	}
	if err := json.Unmarshal(raw, &req); err != nil || req.Action != "apps" {
		t.Fatal("action request", err)
	}
	reply, _ := json.Marshal(actionReply{Seq: req.Seq, Apps: []PhoneApp{{ID: "com.apple.mobilesafari", Name: "Safari"}}})
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(reply)))
	go phone.Write(append(hdr[1:], reply...))
	if err := c.readAction(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if c.Name() != "6s" {
		t.Fatal(c.Name())
	}
	old := &Client{name: "6s"}
	if _, err := old.PhoneAction(ctx, "open", "com.apple.Preferences"); err == nil {
		t.Fatal("legacy server accepted an app command")
	}
}
