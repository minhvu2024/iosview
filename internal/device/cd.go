package device

// CoreDevice driver: iPhones on iOS 17.4+ without jailbreak or TrollStore.
//
// Apple's developer services (the ones Xcode uses for its device window) can
// stream the screen as HEVC, inject touches / buttons / a virtual keyboard and
// read/write the pasteboard. They are reached through an RSD tunnel that only
// pymobiledevice3 implements, so each such phone gets a small sidecar process
// (iosview-cd, Python) that opens the tunnel and serves those services on a
// local HTTP port. This file starts the sidecar and turns the device session
// interface into its HTTP calls:
//
//	POST /touch {type: contact|release, x, y}   x, y in 0..65535
//	POST /button {name, state: down|up|press}   home, lock, volume-up, ...
//	POST /key {usages: [...]}                   full set of held HID usages
//	POST /clipboard {text}  GET /clipboard/events?since=N (long poll)
//	GET /codec, GET /stream.bin, POST /pli      video (see server/cd.go)

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// cdHID is the touch coordinate range (both axes) of the CoreDevice digitizer.
const cdHID = 65536

// cdStartTimeout bounds the sidecar start. The first start on a phone may
// download and mount the Developer Disk Image, which takes a while.
const cdStartTimeout = 3 * time.Minute

// ErrNoSidecar: iosview-cd is not installed next to iosview.
var ErrNoSidecar = errors.New("thiếu iosview-cd.exe cạnh iosview (cần cho iPhone iOS 17+ không jailbreak)")

// iosAtLeast reports whether version v ("17.4.1") is >= major.minor.
func iosAtLeast(v string, major, minor int) bool {
	p := strings.Split(v, ".")
	if len(p) == 0 || p[0] == "" {
		return false
	}
	a, err := strconv.Atoi(p[0])
	if err != nil {
		return false
	}
	b := 0
	if len(p) > 1 {
		b, _ = strconv.Atoi(p[1])
	}
	return a > major || (a == major && b >= minor)
}

// cdCommand finds the sidecar: IOSVIEW_CD (path to an exe or a .py), else
// iosview-cd.exe (or the PyInstaller folder build) next to the executable,
// else iosview_cd.py next to it run with Python.
func cdCommand() (string, []string, error) {
	cands := []string{}
	if p := os.Getenv("IOSVIEW_CD"); p != "" {
		cands = append(cands, p)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		cands = append(cands,
			filepath.Join(dir, "iosview-cd.exe"),
			filepath.Join(dir, "iosview-cd", "iosview-cd.exe"),
			filepath.Join(dir, "iosview-cd"),
			filepath.Join(dir, "iosview_cd.py"),
		)
	}
	for _, p := range cands {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(p), ".py") {
			for _, py := range [][]string{{"py", "-3"}, {"python3"}, {"python"}} {
				if path, err := exec.LookPath(py[0]); err == nil {
					return path, append(py[1:], p), nil
				}
			}
			return "", nil, errors.New("có iosview_cd.py nhưng không tìm thấy Python")
		}
		return p, nil, nil
	}
	return "", nil, ErrNoSidecar
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// cdReady is the sidecar's "READY {json}" line.
type cdReady struct {
	Port   int    `json:"port"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Name   string `json:"name"`
	IOS    string `json:"ios"`
}

// cdClient is a live CoreDevice session (one sidecar process).
type cdClient struct {
	base   string
	cmd    *exec.Cmd
	stdin  io.Closer
	http   *http.Client
	poll   *http.Client
	info   cdReady
	onClip func(string)

	closed chan struct{}
	once   sync.Once
	mu     sync.Mutex
	err    error

	// writer-owned input state
	mask    uint8
	keys    []byte // held HID keyboard usages, in press order
	shifted map[byte]bool
}

// startCD launches the sidecar for the phone with this UDID and waits until
// it serves. status receives progress lines while it starts.
func startCD(udid string, status func(string), onClip func(string)) (*cdClient, error) {
	bin, args, err := cdCommand()
	if err != nil {
		return nil, err
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	args = append(args, "--udid", udid, "--port", strconv.Itoa(port))
	cmd := exec.Command(bin, args...)
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe() // the sidecar exits when this closes (iosview gone)
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = &logWriter{prefix: "cd " + short(udid) + ": "}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("không chạy được iosview-cd: %w", err)
	}
	c := &cdClient{
		base:  "http://127.0.0.1:" + strconv.Itoa(port),
		cmd:   cmd,
		stdin: stdin,
		http: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
			MaxIdleConnsPerHost: 4, DisableCompression: true, Proxy: nil,
		}},
		poll:    &http.Client{Transport: &http.Transport{DisableCompression: true, Proxy: nil}},
		onClip:  onClip,
		closed:  make(chan struct{}),
		shifted: map[byte]bool{},
	}
	ready := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		sent := false
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			kind, rest, _ := strings.Cut(line, " ")
			switch kind {
			case "READY":
				if !sent {
					var r cdReady
					if err := json.Unmarshal([]byte(rest), &r); err != nil {
						ready <- fmt.Errorf("iosview-cd: READY lạ: %v", err)
					} else {
						c.info = r
						ready <- nil
					}
					sent = true
				}
			case "STATUS":
				if !sent && status != nil {
					status(rest)
				}
			case "ERROR":
				if !sent {
					ready <- errors.New(rest)
					sent = true
				} else {
					c.fail(errors.New(rest))
				}
			}
		}
		if !sent {
			ready <- errors.New("iosview-cd đã thoát")
		}
	}()
	go func() {
		err := cmd.Wait()
		if err == nil {
			err = errors.New("iosview-cd đã dừng")
		}
		c.fail(err)
	}()
	select {
	case err := <-ready:
		if err != nil {
			c.Close()
			return nil, err
		}
	case <-c.closed:
		return nil, c.Err()
	case <-time.After(cdStartTimeout):
		c.Close()
		return nil, errors.New("iosview-cd khởi động quá lâu")
	}
	go c.watchClipboard()
	return c, nil
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// logWriter forwards the sidecar's stderr into iosview.log line by line.
type logWriter struct {
	prefix string
	buf    []byte
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		if line := strings.TrimSpace(string(w.buf[:i])); line != "" {
			log.Print(w.prefix + line)
		}
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > 8192 {
		w.buf = w.buf[:0]
	}
	return len(p), nil
}

func (c *cdClient) fail(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.closed)
		c.stdin.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	})
}

// session interface

func (c *cdClient) Size() (int, int)       { return cdHID, cdHID }
func (c *cdClient) ScreenSize() (int, int) { return c.info.Width, c.info.Height }
func (c *cdClient) Name() string           { return c.info.Name }
func (c *cdClient) Done() <-chan struct{}  { return c.closed }
func (c *cdClient) Close() error           { c.fail(errors.New("đã đóng")); return nil }
func (c *cdClient) Base() string           { return c.base }
func (c *cdClient) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *cdClient) post(path string, v any) error {
	select {
	case <-c.closed:
		return c.Err()
	default:
	}
	b, _ := json.Marshal(v)
	resp, err := c.http.Post(c.base+path, "application/json", bytes.NewReader(b))
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			c.fail(fmt.Errorf("iosview-cd không phản hồi: %w", err))
		}
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("iosview-cd %s: HTTP %d", path, resp.StatusCode)
	}
	return nil
}

func (c *cdClient) touch(op string, x, y int) error {
	return c.post("/touch", map[string]any{"type": op, "x": clampHID(x), "y": clampHID(y)})
}

func (c *cdClient) button(name, state string) error {
	return c.post("/button", map[string]string{"name": name, "state": state})
}

func clampHID(v int) int {
	if v < 0 {
		return 0
	}
	if v > cdHID-1 {
		return cdHID - 1
	}
	return v
}

// PointerEvent maps the RFB button mask: bit0 touch, bit1 Lock, bit2 Home,
// bit3/bit4 wheel up/down (a short swipe, iOS has no wheel).
func (c *cdClient) PointerEvent(mask uint8, x, y int) error {
	prev := c.mask
	c.mask = mask &^ (8 | 16)
	var err error
	switch {
	case mask&maskTouch != 0:
		err = c.touch("contact", x, y)
	case prev&maskTouch != 0:
		err = c.touch("release", x, y)
	}
	edge := func(bit uint8, name string) {
		if mask&bit != 0 && prev&bit == 0 {
			err = c.button(name, "down")
		} else if mask&bit == 0 && prev&bit != 0 {
			err = c.button(name, "up")
		}
	}
	edge(maskHome, "home")
	edge(maskPower, "lock")
	if mask&(8|16) != 0 && c.mask&maskTouch == 0 {
		err = c.scroll(x, y, mask&8 != 0)
	}
	return err
}

// scroll drags a finger a short way: wheel up shows content above, so the
// finger moves down.
func (c *cdClient) scroll(x, y int, up bool) error {
	dist := cdHID / 8
	if !up {
		dist = -dist
	}
	steps := 5
	for i := 0; i <= steps; i++ {
		if err := c.touch("contact", x, y+dist*i/steps); err != nil {
			return err
		}
		time.Sleep(12 * time.Millisecond)
	}
	return c.touch("release", x, y+dist)
}

func (c *cdClient) WritePointerBatch(ev [][3]int) error {
	for _, e := range ev {
		if err := c.PointerEvent(uint8(e[0]), e[1], e[2]); err != nil {
			return err
		}
	}
	return nil
}

// KeyEvent presses keysym on the virtual HID keyboard. Media keysyms become
// hardware buttons. Characters that need Shift on a US keyboard get it
// automatically, so a bare 'A' or '!' types correctly.
func (c *cdClient) KeyEvent(ks uint32, down bool) error {
	switch ks {
	case ksVolUp, ksVolDown, ksMute:
		if !down {
			return nil
		}
		return c.button(map[uint32]string{ksVolUp: "volume-up", ksVolDown: "volume-down", ksMute: "mute"}[ks], "press")
	}
	u, shift := hidUsage(ks)
	if u == 0 {
		return nil
	}
	i := bytes.IndexByte(c.keys, u)
	if down {
		if i < 0 {
			c.keys = append(c.keys, u)
		}
		c.shifted[u] = shift
	} else {
		if i < 0 {
			return nil
		}
		c.keys = append(c.keys[:i], c.keys[i+1:]...)
		delete(c.shifted, u)
	}
	return c.sendKeys()
}

func (c *cdClient) sendKeys() error {
	usages := make([]int, 0, len(c.keys)+1)
	needShift := false
	for _, u := range c.keys {
		if c.shifted[u] {
			needShift = true
		}
	}
	if needShift && bytes.IndexByte(c.keys, 0xe1) < 0 && bytes.IndexByte(c.keys, 0xe5) < 0 {
		usages = append(usages, 0xe1)
	}
	for _, u := range c.keys { // modifiers first, like a real keyboard report
		if u >= 0xe0 {
			usages = append(usages, int(u))
		}
	}
	for _, u := range c.keys {
		if u < 0xe0 {
			usages = append(usages, int(u))
		}
	}
	return c.post("/key", map[string][]int{"usages": usages})
}

func (c *cdClient) report(usages ...int) error {
	if usages == nil {
		usages = []int{}
	}
	return c.post("/key", map[string][]int{"usages": usages})
}

// SetClipboard puts text on the phone's pasteboard (any Unicode).
func (c *cdClient) SetClipboard(text string) error {
	return c.post("/clipboard", map[string]string{"text": text})
}

// Paste puts text on the pasteboard and presses ⌘V: accents and emoji arrive
// intact, with no typing delay.
func (c *cdClient) Paste(text string) error {
	if err := c.SetClipboard(text); err != nil {
		return err
	}
	time.Sleep(120 * time.Millisecond)
	steps := [][]int{{0xe3}, {0xe3, 0x19}, {0xe3}, {}}
	gaps := []time.Duration{20, 40, 20, 0}
	for i, s := range steps {
		if err := c.report(s...); err != nil {
			return err
		}
		time.Sleep(gaps[i] * time.Millisecond)
	}
	c.keys = c.keys[:0]
	return nil
}

// watchClipboard reports copies made on the phone (long poll on the sidecar).
func (c *cdClient) watchClipboard() {
	since := -1
	for {
		select {
		case <-c.closed:
			return
		default:
		}
		url := c.base + "/clipboard/events"
		if since >= 0 {
			url += "?since=" + strconv.Itoa(since)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		go func() {
			select {
			case <-c.closed:
				cancel()
			case <-ctx.Done():
			}
		}()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := c.poll.Do(req)
		if err != nil {
			cancel()
			select {
			case <-c.closed:
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		var ev struct {
			Seq  int     `json:"seq"`
			Text *string `json:"text"`
		}
		err = json.NewDecoder(resp.Body).Decode(&ev)
		resp.Body.Close()
		cancel()
		if err != nil || resp.StatusCode != http.StatusOK {
			select {
			case <-c.closed:
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		if since >= 0 && ev.Seq != since && ev.Text != nil && *ev.Text != "" && c.onClip != nil {
			c.onClip(*ev.Text)
		}
		since = ev.Seq
	}
}

// hidUsage maps an X11 keysym to a USB HID keyboard usage, and whether Shift
// is needed for it on a US layout.
func hidUsage(ks uint32) (byte, bool) {
	switch {
	case ks >= 'a' && ks <= 'z':
		return byte(0x04 + ks - 'a'), false
	case ks >= 'A' && ks <= 'Z':
		return byte(0x04 + ks - 'A'), true
	case ks >= '1' && ks <= '9':
		return byte(0x1e + ks - '1'), false
	case ks == '0':
		return 0x27, false
	}
	if ks < 0x80 {
		if b, ok := shiftBase[rune(ks)]; ok {
			u, _ := hidUsage(uint32(b))
			return u, true
		}
	}
	switch ks {
	case ' ':
		return 0x2c, false
	case '-':
		return 0x2d, false
	case '=':
		return 0x2e, false
	case '[':
		return 0x2f, false
	case ']':
		return 0x30, false
	case '\\':
		return 0x31, false
	case ';':
		return 0x33, false
	case '\'':
		return 0x34, false
	case '`':
		return 0x35, false
	case ',':
		return 0x36, false
	case '.':
		return 0x37, false
	case '/':
		return 0x38, false
	case 0xff0d, 0xff8d: // Return, KP_Enter
		return 0x28, false
	case 0xff1b:
		return 0x29, false
	case 0xff08:
		return 0x2a, false
	case 0xff09:
		return 0x2b, false
	case 0xffff:
		return 0x4c, false
	case 0xff50:
		return 0x4a, false
	case 0xff57:
		return 0x4d, false
	case 0xff55:
		return 0x4b, false
	case 0xff56:
		return 0x4e, false
	case 0xff53:
		return 0x4f, false
	case 0xff51:
		return 0x50, false
	case 0xff54:
		return 0x51, false
	case 0xff52:
		return 0x52, false
	case 0xffe5:
		return 0x39, false
	case 0xffe1:
		return 0xe1, false
	case 0xffe2:
		return 0xe5, false
	case 0xffe3:
		return 0xe0, false
	case 0xffe4:
		return 0xe4, false
	case 0xffe9:
		return 0xe2, false
	case 0xffea:
		return 0xe6, false
	case 0xffeb, 0xffe7: // Super_L / Meta_L → Command
		return 0xe3, false
	case 0xffec, 0xffe8:
		return 0xe7, false
	}
	return 0, false
}
