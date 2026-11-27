// Package rfb is a lightweight RFB (VNC) *control* client. It never decodes
// pixels: it only authenticates, tracks the framebuffer size (rotation) and
// sends pointer / key / clipboard events with as little latency as possible.
// Video is rendered by the browser (noVNC) over a separate connection.
package rfb

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/des"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	encRaw             int32 = 0
	encDesktopSize     int32 = -223
	encExtDesktopSize  int32 = -308
	encExtendedClipbrd int32 = -1063131698 // 0xC0A1E5CE

	extClipText    = 1 << 0
	extClipCaps    = 1 << 24
	extClipRequest = 1 << 25
	extClipPeek    = 1 << 26
	extClipNotify  = 1 << 27
	extClipProvide = 1 << 28
)

// Client is a connected RFB control session.
type Client struct {
	conn net.Conn
	r    *bufio.Reader

	wmu sync.Mutex // serialises writes

	mu        sync.Mutex
	width     int
	height    int
	name      string
	extClip   bool
	onResize  func(w, h int)
	onClip    func(text string)
	clipText  string
	clipKnown bool
	clipWait  map[chan string]struct{}
	closed    chan struct{}
	actionSeq uint64
	actions   map[uint64]chan actionReply
	closeOnce sync.Once
	err       error
}

// Options for Dial.
type Options struct {
	Password string
	OnResize func(w, h int)
	// OnClipboard is called when the phone's clipboard changes (text).
	OnClipboard func(text string)
	// SizePoll is how often to ask the server for (1x1) updates so we learn
	// about rotation / framebuffer size changes. Default 2.5s (the H.264
	// stream also nudges PollSize immediately when rotation changes).
	SizePoll time.Duration
}

// Handshake runs the RFB handshake on an existing connection.
func Handshake(conn net.Conn, opt Options) (*Client, error) {
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	c := &Client{conn: conn, r: bufio.NewReaderSize(conn, 64*1024), onResize: opt.OnResize, onClip: opt.OnClipboard, closed: make(chan struct{})}
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	if err := c.handshake(opt.Password); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	poll := opt.SizePoll
	if poll <= 0 {
		poll = 2500 * time.Millisecond
	}
	go c.readLoop(poll)
	return c, nil
}

func (c *Client) handshake(password string) error {
	ver := make([]byte, 12)
	if _, err := io.ReadFull(c.r, ver); err != nil {
		return fmt.Errorf("đọc phiên bản RFB: %w", err)
	}
	var maj, min int
	if _, err := fmt.Sscanf(string(ver), "RFB %03d.%03d\n", &maj, &min); err != nil {
		return fmt.Errorf("không phải VNC server: %q", ver)
	}
	if maj != 3 {
		return fmt.Errorf("RFB version không hỗ trợ: %q", ver)
	}
	if min >= 8 {
		min = 8
	} else if min >= 7 {
		min = 7
	} else {
		min = 3
	}
	if _, err := fmt.Fprintf(c.conn, "RFB 003.%03d\n", min); err != nil {
		return err
	}

	var secType uint32
	if min == 3 {
		if err := binary.Read(c.r, binary.BigEndian, &secType); err != nil {
			return err
		}
		if secType == 0 {
			return c.readReason()
		}
	} else {
		n, err := c.r.ReadByte()
		if err != nil {
			return err
		}
		if n == 0 {
			return c.readReason()
		}
		types := make([]byte, n)
		if _, err := io.ReadFull(c.r, types); err != nil {
			return err
		}
		has := func(t byte) bool { return bytes.IndexByte(types, t) >= 0 }
		switch {
		case password != "" && has(2):
			secType = 2
		case has(1):
			secType = 1
		case has(2):
			secType = 2
		default:
			return fmt.Errorf("server yêu cầu kiểu bảo mật không hỗ trợ: %v", types)
		}
		if _, err := c.conn.Write([]byte{byte(secType)}); err != nil {
			return err
		}
	}

	if secType == 2 {
		if password == "" {
			return errors.New("TrollVNC yêu cầu mật khẩu — điền vào Cài đặt")
		}
		challenge := make([]byte, 16)
		if _, err := io.ReadFull(c.r, challenge); err != nil {
			return err
		}
		resp, err := vncAuth(password, challenge)
		if err != nil {
			return err
		}
		if _, err := c.conn.Write(resp); err != nil {
			return err
		}
	}
	if secType == 2 || min == 8 {
		var result uint32
		if err := binary.Read(c.r, binary.BigEndian, &result); err != nil {
			return err
		}
		if result != 0 {
			if min == 8 {
				if err := c.readReason(); err != nil {
					return err
				}
			}
			return errors.New("sai mật khẩu VNC")
		}
	}

	// ClientInit: shared = 1 so we never kick the browser viewer off.
	if _, err := c.conn.Write([]byte{1}); err != nil {
		return err
	}
	var si struct {
		W, H uint16
		PF   [16]byte
		NLen uint32
	}
	if err := binary.Read(c.r, binary.BigEndian, &si); err != nil {
		return fmt.Errorf("ServerInit: %w", err)
	}
	if si.NLen > 4096 {
		return errors.New("ServerInit name quá dài")
	}
	name := make([]byte, si.NLen)
	if _, err := io.ReadFull(c.r, name); err != nil {
		return err
	}
	c.width, c.height, c.name = int(si.W), int(si.H), string(name)

	// SetPixelFormat: 32bpp true colour, little endian -> raw pixel = 4 bytes.
	pf := []byte{0, 0, 0, 0, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0}
	// SetEncodings
	encs := []int32{encRaw, encDesktopSize, encExtDesktopSize, encExtendedClipbrd}
	se := make([]byte, 4+4*len(encs))
	se[0] = 2
	binary.BigEndian.PutUint16(se[2:], uint16(len(encs)))
	for i, e := range encs {
		binary.BigEndian.PutUint32(se[4+4*i:], uint32(e))
	}
	_, err := c.conn.Write(append(pf, se...))
	return err
}

func (c *Client) readReason() error {
	var n uint32
	if err := binary.Read(c.r, binary.BigEndian, &n); err != nil {
		return err
	}
	if n > 4096 {
		n = 4096
	}
	b := make([]byte, n)
	_, _ = io.ReadFull(c.r, b)
	return fmt.Errorf("server từ chối: %s", b)
}

func reverseBits(b byte) byte {
	var r byte
	for i := 0; i < 8; i++ {
		r = r<<1 | b&1
		b >>= 1
	}
	return r
}

func vncAuth(password string, challenge []byte) ([]byte, error) {
	key := make([]byte, 8)
	copy(key, password) // only the first 8 chars are used
	for i := range key {
		key[i] = reverseBits(key[i])
	}
	blk, err := des.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 16)
	blk.Encrypt(out[0:8], challenge[0:8])
	blk.Encrypt(out[8:16], challenge[8:16])
	return out, nil
}

// Size returns the current framebuffer size.
func (c *Client) Size() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.width, c.height
}

// Name is the desktop name advertised by the server.
func (c *Client) Name() string { return strings.TrimSuffix(c.name, actionTag) }

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.closed }

// Err returns why the connection ended.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close terminates the session.
func (c *Client) Close() error {
	c.fail(errors.New("closed"))
	return nil
}

func (c *Client) fail(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		c.conn.Close()
		close(c.closed)
	})
}

func (c *Client) write(b []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err := c.conn.Write(b)
	if err != nil {
		c.fail(err)
	}
	return err
}

// PointerEvent sends a pointer event in framebuffer pixel coordinates.
// mask bit0=touch, bit1=Power (TrollVNC), bit2=Home, bit3/4=wheel up/down.
func (c *Client) PointerEvent(mask uint8, x, y int) error {
	return c.write(pointerMsg(mask, x, y))
}

func pointerMsg(mask uint8, x, y int) []byte {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	b := []byte{5, mask, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(b[2:], uint16(x))
	binary.BigEndian.PutUint16(b[4:], uint16(y))
	return b
}

// WritePointerBatch writes several pointer events in a single syscall.
func (c *Client) WritePointerBatch(ev [][3]int) error {
	buf := make([]byte, 0, 6*len(ev))
	for _, e := range ev {
		buf = append(buf, pointerMsg(uint8(e[0]), e[1], e[2])...)
	}
	return c.write(buf)
}

// KeyEvent sends an X11 keysym press/release.
func (c *Client) KeyEvent(keysym uint32, down bool) error {
	b := make([]byte, 8)
	b[0] = 4
	if down {
		b[1] = 1
	}
	binary.BigEndian.PutUint32(b[4:], keysym)
	return c.write(b)
}

// SetClipboard puts text on the device clipboard. Non-Latin-1 text (e.g.
// Vietnamese) needs the Extended Clipboard extension, which TrollVNC
// (LibVNCServer) supports.
func (c *Client) SetClipboard(text string) error {
	latin1 := true
	for _, r := range text {
		if r > 0xff {
			latin1 = false
			break
		}
	}
	c.mu.Lock()
	ext := c.extClip
	c.mu.Unlock()
	if latin1 && !ext {
		lb := make([]byte, 0, len(text))
		for _, r := range text {
			lb = append(lb, byte(r))
		}
		b := make([]byte, 8, 8+len(lb))
		b[0] = 6
		binary.BigEndian.PutUint32(b[4:], uint32(len(lb)))
		return c.writeClipboard(append(b, lb...), text)
	}
	if !ext {
		return errors.New("server không hỗ trợ clipboard UTF-8 (bật -C on trong TrollVNC)")
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	payload := append([]byte(text), 0)
	var sz [4]byte
	binary.BigEndian.PutUint32(sz[:], uint32(len(payload)))
	_, _ = zw.Write(sz[:])
	_, _ = zw.Write(payload)
	// LibVNCServer inflates with Z_SYNC_FLUSH and rejects a terminated
	// stream (Z_STREAM_END), so sync-flush instead of Close.
	_ = zw.Flush()
	data := make([]byte, 4, 4+z.Len())
	binary.BigEndian.PutUint32(data, extClipProvide|extClipText)
	data = append(data, z.Bytes()...)
	b := make([]byte, 8, 8+len(data))
	b[0] = 6
	binary.BigEndian.PutUint32(b[4:], uint32(-int32(len(data))))
	return c.writeClipboard(append(b, data...), text)
}

func (c *Client) writeClipboard(packet []byte, text string) error {
	if err := c.write(packet); err != nil {
		return err
	}
	c.mu.Lock()
	c.clipText, c.clipKnown = text, true
	c.mu.Unlock()
	return nil
}

func (c *Client) requestUpdate() error {
	// Incremental 1x1 update request: almost free, but lets the server tell
	// us about DesktopSize changes (rotation).
	return c.write([]byte{3, 1, 0, 0, 0, 0, 0, 1, 0, 1})
}

// PollSize requests an immediate 1x1 update so a DesktopSize change (rotation)
// is picked up without waiting for the periodic poll timer.
func (c *Client) PollSize() {
	select {
	case <-c.closed:
	default:
		_ = c.requestUpdate()
	}
}

func (c *Client) setSize(w, h int) {
	c.mu.Lock()
	changed := w != c.width || h != c.height
	c.width, c.height = w, h
	cb := c.onResize
	c.mu.Unlock()
	if changed && cb != nil {
		cb(w, h)
	}
}

func (c *Client) readLoop(poll time.Duration) {
	var timerMu sync.Mutex
	pending := false
	schedule := func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if pending {
			return
		}
		pending = true
		time.AfterFunc(poll, func() {
			timerMu.Lock()
			pending = false
			timerMu.Unlock()
			select {
			case <-c.closed:
			default:
				_ = c.requestUpdate()
			}
		})
	}
	if err := c.requestUpdate(); err != nil {
		return
	}
	for {
		t, err := c.r.ReadByte()
		if err != nil {
			c.fail(err)
			return
		}
		switch t {
		case 0: // FramebufferUpdate
			if err := c.readFBUpdate(); err != nil {
				c.fail(err)
				return
			}
			schedule()
		case 1: // SetColourMapEntries
			var h struct {
				Pad   uint8
				First uint16
				N     uint16
			}
			if err := binary.Read(c.r, binary.BigEndian, &h); err != nil {
				c.fail(err)
				return
			}
			if _, err := c.r.Discard(int(h.N) * 6); err != nil {
				c.fail(err)
				return
			}
		case 11:
			if err := c.readAction(); err != nil {
				c.fail(err)
				return
			}
		case 2: // Bell
		case 3: // ServerCutText
			var h struct {
				Pad [3]byte
				Len int32
			}
			if err := binary.Read(c.r, binary.BigEndian, &h); err != nil {
				c.fail(err)
				return
			}
			n := int(h.Len)
			if n > 10<<20 || n < -(10<<20) {
				c.fail(errors.New("clipboard quá dài"))
				return
			}
			if n < 0 {
				n = -n
				buf := make([]byte, n)
				if _, err := io.ReadFull(c.r, buf); err != nil {
					c.fail(err)
					return
				}
				c.handleExtClipboard(buf)
			} else {
				buf := make([]byte, n)
				if _, err := io.ReadFull(c.r, buf); err != nil {
					c.fail(err)
					return
				}
				// legacy clipboard is Latin-1
				rs := make([]rune, len(buf))
				for i, b := range buf {
					rs[i] = rune(b)
				}
				c.deliverClip(string(rs))
			}
		default:
			c.fail(fmt.Errorf("RFB message không hỗ trợ: %d", t))
			return
		}
	}
}

func (c *Client) readFBUpdate() error {
	var h struct {
		Pad uint8
		N   uint16
	}
	if err := binary.Read(c.r, binary.BigEndian, &h); err != nil {
		return err
	}
	for i := 0; i < int(h.N); i++ {
		var r struct {
			X, Y, W, H uint16
			Enc        int32
		}
		if err := binary.Read(c.r, binary.BigEndian, &r); err != nil {
			return err
		}
		switch r.Enc {
		case encRaw:
			if _, err := c.r.Discard(int(r.W) * int(r.H) * 4); err != nil {
				return err
			}
		case encDesktopSize:
			c.setSize(int(r.W), int(r.H))
		case encExtDesktopSize:
			var sh struct {
				N   uint8
				Pad [3]byte
			}
			if err := binary.Read(c.r, binary.BigEndian, &sh); err != nil {
				return err
			}
			if _, err := c.r.Discard(int(sh.N) * 16); err != nil {
				return err
			}
			if r.Y == 0 { // y = status code; 0 = success
				c.setSize(int(r.W), int(r.H))
			}
		default:
			return fmt.Errorf("encoding không mong đợi %d", r.Enc)
		}
	}
	return nil
}

func (c *Client) deliverClip(text string) {
	c.mu.Lock()
	c.clipText, c.clipKnown = text, true
	for ch := range c.clipWait {
		select {
		case ch <- text:
		default:
		}
	}
	cb := c.onClip
	requested := len(c.clipWait) > 0
	c.mu.Unlock()
	// TrollVNC suppresses remote-set echoes; equal local copies are still events.
	if cb != nil && !requested {
		cb(text)
	}
}

// handleExtClipboard processes an Extended Clipboard message from the server.
func (c *Client) handleExtClipboard(buf []byte) {
	if len(buf) < 4 {
		return
	}
	flags := binary.BigEndian.Uint32(buf)
	switch {
	case flags&extClipCaps != 0:
		c.mu.Lock()
		c.extClip = true
		c.mu.Unlock()
		// Answer with our own capabilities: text, all actions, 10 MB max.
		msg := make([]byte, 16)
		msg[0] = 6
		binary.BigEndian.PutUint32(msg[4:], negLen(8))
		binary.BigEndian.PutUint32(msg[8:], extClipCaps|extClipRequest|extClipPeek|extClipNotify|extClipProvide|extClipText)
		binary.BigEndian.PutUint32(msg[12:], 10<<20)
		_ = c.write(msg)
	case flags&extClipNotify != 0:
		if flags&extClipText != 0 {
			msg := make([]byte, 12)
			msg[0] = 6
			binary.BigEndian.PutUint32(msg[4:], negLen(4))
			binary.BigEndian.PutUint32(msg[8:], extClipRequest|extClipText)
			_ = c.write(msg)
		}
	case flags&extClipProvide != 0:
		if flags&extClipText == 0 {
			return
		}
		zr, err := zlib.NewReader(bytes.NewReader(buf[4:]))
		if err != nil {
			return
		}
		var sz [4]byte
		if _, err := io.ReadFull(zr, sz[:]); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(sz[:])
		if n > 10<<20 {
			return
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(zr, data); err != nil {
			return
		}
		c.deliverClip(string(bytes.TrimSuffix(data, []byte{0})))
	}
}

// negLen encodes a negative ClientCutText length (Extended Clipboard marker).
func negLen(n int) uint32 { return uint32(int32(-n)) }
