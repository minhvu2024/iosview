package rfb

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const actionTag = " [iosview-actions/1]"

type PhoneApp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type actionReply struct {
	Seq   uint64     `json:"seq"`
	Apps  []PhoneApp `json:"apps"`
	Error string     `json:"error"`
}

func (c *Client) HasActions() bool { return strings.HasSuffix(c.name, actionTag) }

func (c *Client) PhoneAction(ctx context.Context, command, bundle string) ([]PhoneApp, error) {
	if !c.HasActions() {
		return nil, errors.New("cần bản TrollVNC hỗ trợ phím tắt iPhone")
	}
	reply := make(chan actionReply, 1)
	c.mu.Lock()
	c.actionSeq++
	seq := c.actionSeq
	if c.actions == nil {
		c.actions = make(map[uint64]chan actionReply)
	}
	c.actions[seq] = reply
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.actions, seq); c.mu.Unlock() }()
	payload, err := json.Marshal(map[string]any{"seq": seq, "action": command, "bundle": bundle})
	if err != nil {
		return nil, err
	}
	hdr := []byte{11, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(payload)))
	if err := c.write(append(hdr, payload...)); err != nil {
		return nil, err
	}
	select {
	case r := <-reply:
		if r.Error != "" {
			return nil, errors.New(r.Error)
		}
		return r.Apps, nil
	case <-ctx.Done():
		return nil, errors.New("iPhone chưa phản hồi")
	case <-c.closed:
		return nil, errors.New("mất kết nối iPhone")
	}
}

func (c *Client) readAction() error {
	var hdr [7]byte
	if _, err := io.ReadFull(c.r, hdr[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr[3:])
	if n >= 0xfffffffd {
		return nil
	}
	if n > 512*1024 {
		return errors.New("danh sách ứng dụng quá lớn")
	}
	raw := make([]byte, n)
	if _, err := io.ReadFull(c.r, raw); err != nil {
		return err
	}
	var r actionReply
	if json.Unmarshal(raw, &r) == nil {
		c.mu.Lock()
		ch := c.actions[r.Seq]
		c.mu.Unlock()
		select {
		case ch <- r:
		default:
		}
	}
	return nil
}
