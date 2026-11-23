package rfb

import (
	"context"
	"encoding/binary"
	"errors"
	"time"
)

// ReadClipboard requests text without changing the phone clipboard.
func (c *Client) ReadClipboard(ctx context.Context, command bool) (string, error) {
	ch := make(chan string, 1)
	c.mu.Lock()
	if c.clipWait == nil {
		c.clipWait = make(map[chan string]struct{})
	}
	c.clipWait[ch] = struct{}{}
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.clipWait, ch); c.mu.Unlock() }()
	if command {
		// One packet keeps Command press/release together, even during typing.
		var keys []byte
		for _, k := range []struct {
			key  uint32
			down byte
		}{{0xffeb, 1}, {'c', 1}, {'c', 0}, {0xffeb, 0}} {
			b := make([]byte, 8)
			b[0], b[1] = 4, k.down
			binary.BigEndian.PutUint32(b[4:], k.key)
			keys = append(keys, b...)
		}
		if err := c.clipWrite(ctx, keys); err != nil {
			return "", err
		}
		timer := time.NewTimer(150 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	c.mu.Lock()
	ext, known, cached := c.extClip, c.clipKnown, c.clipText
	c.mu.Unlock()
	if known && !command {
		return cached, nil
	}
	if ext {
		msg := make([]byte, 12)
		msg[0] = 6
		binary.BigEndian.PutUint32(msg[4:], negLen(4))
		binary.BigEndian.PutUint32(msg[8:], extClipRequest|extClipText)
		if err := c.clipWrite(ctx, msg); err != nil {
			return "", err
		}
	}
	select {
	case text := <-ch:
		return text, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-c.closed:
		return "", errors.New("VNC đã ngắt")
	}
}

// Bound both waiting for the writer and the socket write by the caller's timeout.
func (c *Client) clipWrite(ctx context.Context, b []byte) error {
	for !c.wmu.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	defer c.wmu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	_ = c.conn.SetWriteDeadline(deadline)
	_, err := c.conn.Write(b)
	if err != nil {
		c.fail(err)
	}
	return err
}
