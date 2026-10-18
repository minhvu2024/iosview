package device

import (
	"math"
	"time"

	"iosview/internal/rfb"
)

// Input event kinds sent to a device.
const (
	evPointer = iota
	evKey
	evText
	evButton
)

// Event is one input action, with coordinates normalised to 0..1 so the same
// event maps correctly onto phones with different resolutions.
type Event struct {
	Kind   int
	X, Y   float64
	Mask   uint8
	Keysym uint32
	Down   bool
	Text   string
	Button string
}

// Hardware button keysyms / masks understood by TrollVNC.
const (
	ksVolUp   = 0x1008FF13
	ksVolDown = 0x1008FF11
	ksMute    = 0x1008FF12
	ksSuperL  = 0xFFEB // Command
	ksV       = 0x0076
	maskTouch = 1
	maskPower = 2
	maskHome  = 4
)

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func toPx(n float64, size int) int {
	if size <= 1 {
		return 0
	}
	return int(math.Round(clamp01(n) * float64(size-1)))
}

// runWriter drains the event queue for one device. Consecutive pointer moves
// with the same button mask are coalesced into the newest one when the
// device falls behind, so latency never accumulates; presses, releases and
// keys are never dropped.
func (d *Device) runWriter(c *rfb.Client) {
	for {
		select {
		case <-c.Done():
			return
		case ev := <-d.queue:
			batch := []Event{ev}
		drain:
			for len(batch) < 256 {
				select {
				case e := <-d.queue:
					batch = append(batch, e)
				default:
					break drain
				}
			}
			d.flush(c, batch)
		}
	}
}

func (d *Device) flush(c *rfb.Client, batch []Event) {
	w, h := c.Size()
	var ptr [][3]int
	flushPtr := func() {
		if len(ptr) > 0 {
			_ = c.WritePointerBatch(ptr)
			ptr = ptr[:0]
		}
	}
	for i, ev := range batch {
		switch ev.Kind {
		case evPointer:
			// Skip a move if the next event is also a move with the same mask.
			if i+1 < len(batch) {
				n := batch[i+1]
				if n.Kind == evPointer && n.Mask == ev.Mask {
					continue
				}
			}
			ptr = append(ptr, [3]int{int(ev.Mask), toPx(ev.X, w), toPx(ev.Y, h)})
		case evKey:
			flushPtr()
			_ = c.KeyEvent(ev.Keysym, ev.Down)
			// Give iOS a real press duration; back-to-back down/up is dropped.
			if ev.Down {
				time.Sleep(keyHold)
			} else {
				time.Sleep(modGap)
			}
		case evText:
			flushPtr()
			d.typeText(c, ev.Text)
		case evButton:
			flushPtr()
			d.pressButton(c, ev.Button, w, h)
		}
	}
	flushPtr()
}

func (d *Device) pressButton(c *rfb.Client, b string, w, h int) {
	cx, cy := w/2, h/2
	tap := func(mask uint8, hold time.Duration) {
		_ = c.PointerEvent(mask, cx, cy)
		time.Sleep(hold)
		_ = c.PointerEvent(0, cx, cy)
	}
	key := func(ks uint32) {
		_ = c.KeyEvent(ks, true)
		time.Sleep(30 * time.Millisecond)
		_ = c.KeyEvent(ks, false)
	}
	if len(b) == 5 && b[:4] == "cmd:" { // ⌘ + letter, e.g. "cmd:c"
		commandKey(c, uint32(b[4]))
		return
	}
	switch b {
	case "selectall":
		selectAll(c)
		return
	case "clear":
		clearField(c)
		return
	case "home":
		tap(maskHome, 60*time.Millisecond)
	case "switcher":
		tap(maskHome, 40*time.Millisecond)
		time.Sleep(90 * time.Millisecond)
		tap(maskHome, 40*time.Millisecond)
	case "power":
		tap(maskPower, 60*time.Millisecond)
	case "back":
		// iOS has no Back key: emulate the system edge-swipe from the left
		// edge (works in apps using standard navigation, Safari, Settings…).
		y := h * 45 / 100
		steps := []float64{0, 0.08, 0.18, 0.30, 0.44, 0.58, 0.70}
		_ = c.PointerEvent(maskTouch, 0, y)
		for _, f := range steps[1:] {
			time.Sleep(16 * time.Millisecond)
			_ = c.PointerEvent(maskTouch, int(f*float64(w)), y)
		}
		time.Sleep(16 * time.Millisecond)
		_ = c.PointerEvent(0, int(0.70*float64(w)), y)
	case "siri":
		tap(maskHome, 1200*time.Millisecond)
	case "volup":
		key(ksVolUp)
	case "voldown":
		key(ksVolDown)
	case "mute":
		key(ksMute)
	}
}

// commandKey presses ⌘+key with small gaps so iOS registers the shortcut
// reliably (TrollVNC maps Super to Command).
func commandKey(c *rfb.Client, ks uint32) {
	_ = c.KeyEvent(ksSuperL, true)
	time.Sleep(20 * time.Millisecond)
	_ = c.KeyEvent(ks, true)
	time.Sleep(30 * time.Millisecond)
	_ = c.KeyEvent(ks, false)
	time.Sleep(20 * time.Millisecond)
	_ = c.KeyEvent(ksSuperL, false)
}
