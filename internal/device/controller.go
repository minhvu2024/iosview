package device

import (
	"math"
	"time"
)

// Input event kinds sent to a device.
const (
	evPointer = iota
	evWheel
	evKey
	evText
	evButton
)

// Minimum gap enforced ONLY when a drag's final Move and Lift arrive in the
// same batch (0 ms apart), preventing UIScrollView/UIPickerView velocity spikes
// without adding latency to taps or live dragging.
const liftSettleGap = 8 * time.Millisecond

// Event is one input action, with coordinates normalised to 0..1 so the same
// event maps correctly onto phones with different resolutions.
type Event struct {
	Kind     int
	X, Y     float64
	Mask     uint8
	Keysym   uint32
	Down     bool
	Text     string
	Button   string
	Force    bool // evText: always type it (accents stripped), whatever the setting
	Follower bool // sync-group follower (not the phone directly under the cursor)
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

func isEdgeNorm(x, y float64) bool {
	return x <= 0.03 || x >= 0.97 || y <= 0.03 || y >= 0.97
}

// runWriter drains the event queue for one device. Consecutive pointer moves
// with the same button mask are coalesced into the newest one when the
// device falls behind, so latency never accumulates; presses, releases and
// keys are never dropped. Only one writer runs per device (see d.owner).
func (d *Device) runWriter(c session) {
	d.setScreenOff(false)
	// The previous session may have ended with a finger (or Home/Power) still
	// down: its release never reached the phone. Lift it before anything else.
	if d.held.mask != 0 && c.PointerEvent(0, d.held.x, d.held.y) == nil {
		d.held.mask = 0
		d.held.moved = false
		d.held.edge = false
	}
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

func (d *Device) flush(c session, batch []Event) {
	w, h := c.Size()
	var ptr [][3]int
	flushPtr := func() {
		if len(ptr) > 0 {
			_ = c.WritePointerBatch(ptr)
			ptr = ptr[:0]
		}
	}
	for i, ev := range batch {
		select {
		case <-c.Done():
			return // session gone: the rest is stale
		default:
		}
		switch ev.Kind {
		case evPointer:
			// Skip a pure move (mask unchanged) when the next event is another
			// move with the same mask, EXCEPT keep the very first move of an
			// edge gesture so Control Center / Back swipes preserve their
			// initial edge-exit trajectory.
			if ev.Mask != 0 && ev.Mask == d.held.mask && i+1 < len(batch) {
				if !(d.held.edge && !d.held.moved) {
					n := batch[i+1]
					if n.Kind == evPointer && n.Mask == ev.Mask {
						continue
					}
				}
			}

			nx, ny := clamp01(ev.X), clamp01(ev.Y)
			wasDown := d.held.mask != 0
			nowDown := ev.Mask != 0

			if nowDown && !wasDown {
				d.held.edge = isEdgeNorm(nx, ny)
				d.held.moved = false
			}

			x, y := toPx(nx, w), toPx(ny, h)
			if nowDown && wasDown && x == d.held.x && y == d.held.y {
				continue
			}

			// Only separate a drag's final Move and Lift when both landed in the
			// same batch (0 ms apart), so UIPickerView never sees infinite release
			// velocity while normal taps and live dragging pay zero sleep cost.
			if !nowDown && wasDown && d.held.moved && len(ptr) > 0 {
				flushPtr()
				time.Sleep(liftSettleGap)
			}

			if nowDown && wasDown {
				d.held.moved = true
			} else if !nowDown {
				d.held.moved = false
				d.held.edge = false
			}

			ptr = append(ptr, [3]int{int(ev.Mask), x, y})
			d.held.mask, d.held.x, d.held.y, d.held.at = ev.Mask, x, y, time.Now()
		case evWheel:
			x, y := toPx(ev.X, w), toPx(ev.Y, h)
			bit := 16
			if ev.Down {
				bit = 8
			}
			ptr = append(ptr, [3]int{int(ev.Mask) | bit, x, y}, [3]int{int(ev.Mask), x, y})
			d.held.mask, d.held.x, d.held.y, d.held.at = ev.Mask, x, y, time.Now()
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
			d.typeText(c, ev.Text, ev.Force)
		case evButton:
			flushPtr()
			d.pressButton(c, ev.Button, w, h)
		}
	}
	flushPtr()
}

func (d *Device) setScreenOff(off bool) {
	d.m.mu.Lock()
	changed := d.ScreenOff != off
	d.ScreenOff = off
	d.m.mu.Unlock()
	if changed {
		d.m.notify()
	}
}

func (d *Device) pressButton(c session, b string, w, h int) {
	cx, cy := w/2, h/2
	tap := func(mask uint8, hold time.Duration) bool {
		down := c.PointerEvent(mask, cx, cy)
		time.Sleep(hold)
		up := c.PointerEvent(0, cx, cy)
		return down == nil && up == nil
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
		if tap(maskHome, 60*time.Millisecond) {
			d.setScreenOff(false)
		}
	case "switcher":
		tap(maskHome, 40*time.Millisecond)
		time.Sleep(90 * time.Millisecond)
		tap(maskHome, 40*time.Millisecond)
	case "power":
		if !d.ScreenOff {
			if tap(maskPower, 60*time.Millisecond) {
				d.setScreenOff(true)
			}
			return
		}
		if tap(maskHome, 60*time.Millisecond) {
			// The first Home wakes the display. A separate Home press after
			// the lock screen appears asks iOS to open or show authentication.
			select {
			case <-c.Done():
				return
			case <-time.After(900 * time.Millisecond):
			}
			tap(maskHome, 60*time.Millisecond)
			d.setScreenOff(false)
		}
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
	case "cc":
		// Control Center: Home-button iPhones (~16:9) swipe up from the bottom
		// edge; FaceID/Notch iPhones (~19.5:9) swipe down from top-right.
		longSide, shortSide := h, w
		if w > h {
			longSide, shortSide = w, h
		}
		notch := shortSide > 0 && longSide*100/shortSide >= 195
		if notch || d.IsCD() {
			x := toPx(0.86, w)
			steps := []float64{0, 0.015, 0.045, 0.10, 0.18, 0.28, 0.38}
			_ = c.PointerEvent(maskTouch, x, toPx(steps[0], h))
			for _, f := range steps[1:] {
				time.Sleep(14 * time.Millisecond)
				_ = c.PointerEvent(maskTouch, x, toPx(f, h))
			}
			time.Sleep(35 * time.Millisecond)
			_ = c.PointerEvent(0, x, toPx(steps[len(steps)-1], h))
		} else {
			x := toPx(0.50, w)
			steps := []float64{1.0, 0.988, 0.96, 0.91, 0.82, 0.68, 0.52, 0.38}
			_ = c.PointerEvent(maskTouch, x, toPx(steps[0], h))
			for _, f := range steps[1:] {
				time.Sleep(14 * time.Millisecond)
				_ = c.PointerEvent(maskTouch, x, toPx(f, h))
			}
			time.Sleep(35 * time.Millisecond)
			_ = c.PointerEvent(0, x, toPx(steps[len(steps)-1], h))
		}
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
func commandKey(c session, ks uint32) {
	_ = c.KeyEvent(ksSuperL, true)
	time.Sleep(20 * time.Millisecond)
	_ = c.KeyEvent(ks, true)
	time.Sleep(30 * time.Millisecond)
	_ = c.KeyEvent(ks, false)
	time.Sleep(20 * time.Millisecond)
	_ = c.KeyEvent(ksSuperL, false)
}
