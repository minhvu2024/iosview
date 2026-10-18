package device

import (
	"strings"
	"time"

	"iosview/internal/rfb"
)

// Keysyms for keys used while typing.
const (
	ksShiftL = 0xFFE1
	ksReturn = 0xFF0D
	ksTab    = 0xFF09
	ksSpace  = 0x0020
	ksUp     = 0xFF52
	ksDown   = 0xFF54
	ksBksp   = 0xFF08
)

// Pacing. Real key presses last 50–100 ms; iOS drops or merges HID key
// events that are much shorter, so we hold every key for a while.
const (
	keyHold  = 35 * time.Millisecond // down → up
	keyGap   = 25 * time.Millisecond // between keys
	modGap   = 15 * time.Millisecond // modifier down → key, key up → modifier up
	fastHold = 18 * time.Millisecond // for long arrow/backspace runs
)

// shiftBase maps a US-keyboard "shifted" character to the base key that must
// be pressed with Shift held. We only send base (unshifted) keysyms so we rely
// solely on TrollVNC mapping the base keys, which it always does, and let
// Shift produce the shifted glyph on the device — exactly like a real keyboard.
var shiftBase = map[rune]rune{
	'~': '`', '!': '1', '@': '2', '#': '3', '$': '4', '%': '5', '^': '6',
	'&': '7', '*': '8', '(': '9', ')': '0', '_': '-', '+': '=', '{': '[',
	'}': ']', '|': '\\', ':': ';', '"': '\'', '<': ',', '>': '.', '?': '/',
}

// typeableRune reports whether a rune can be typed directly via the US HID
// keyboard (printable ASCII, plus newline and tab).
func typeableRune(r rune) bool {
	return r == '\n' || r == '\t' || (r >= 0x20 && r <= 0x7e)
}

// allTypeable reports whether every rune of s can be typed directly.
func allTypeable(s string) bool {
	for _, r := range s {
		if !typeableRune(r) {
			return false
		}
	}
	return true
}

// viet maps Vietnamese letters (and a few common typographic characters) to
// plain ASCII, for "type without diacritics" mode.
var viet = func() map[rune]string {
	m := map[rune]string{}
	add := func(base string, chars string) {
		for _, r := range chars {
			m[r] = base
		}
	}
	add("a", "àáạảãâầấậẩẫăằắặẳẵ")
	add("A", "ÀÁẠẢÃÂẦẤẬẨẪĂẰẮẶẲẴ")
	add("e", "èéẹẻẽêềếệểễ")
	add("E", "ÈÉẸẺẼÊỀẾỆỂỄ")
	add("i", "ìíịỉĩ")
	add("I", "ÌÍỊỈĨ")
	add("o", "òóọỏõôồốộổỗơờớợởỡ")
	add("O", "ÒÓỌỎÕÔỒỐỘỔỖƠỜỚỢỞỠ")
	add("u", "ùúụủũưừứựửữ")
	add("U", "ÙÚỤỦŨƯỪỨỰỬỮ")
	add("y", "ỳýỵỷỹ")
	add("Y", "ỲÝỴỶỸ")
	add("d", "đ")
	add("D", "Đ")
	// typographic punctuation often found in copied text
	add("-", "–—‑")
	add("'", "‘’")
	add("\"", "“”")
	add("...", "…")
	add(" ", "   ")
	return m
}()

// stripAccents converts s to ASCII: Vietnamese diacritics removed, CRLF
// normalised; anything else non-ASCII is dropped. Returns the text and
// whether anything was changed.
func stripAccents(s string) (string, bool) {
	var b strings.Builder
	changed := false
	for _, r := range strings.ReplaceAll(s, "\r\n", "\n") {
		if typeableRune(r) {
			b.WriteRune(r)
			continue
		}
		changed = true
		if rep, ok := viet[r]; ok {
			b.WriteString(rep)
		}
	}
	return b.String(), changed
}

// typeText enters s into the focused field on the device by typing it key by
// key (no clipboard, no ⌘V — iOS ignores ⌘ shortcuts from injected keyboards).
// Non-ASCII text is either transliterated (StripAccents) or, if that option is
// off, put on the device clipboard for a manual tap on Paste.
func (d *Device) typeText(c *rfb.Client, s string) {
	if s == "" {
		return
	}
	if !allTypeable(s) {
		d.m.mu.Lock()
		strip := d.m.cfg.StripAccents
		d.m.mu.Unlock()
		if strip {
			s, _ = stripAccents(s)
		} else {
			if err := c.SetClipboard(s); err != nil {
				d.setErr(err.Error())
			}
			return
		}
	}
	for _, r := range s {
		typeRune(c, r)
		time.Sleep(keyGap)
	}
}

func tap(c *rfb.Client, ks uint32, hold time.Duration) {
	_ = c.KeyEvent(ks, true)
	time.Sleep(hold)
	_ = c.KeyEvent(ks, false)
}

func typeRune(c *rfb.Client, r rune) {
	switch r {
	case '\n':
		tap(c, ksReturn, keyHold)
		return
	case '\t':
		tap(c, ksTab, keyHold)
		return
	case ' ':
		tap(c, ksSpace, keyHold)
		return
	}
	shift := false
	base := r
	if r >= 'A' && r <= 'Z' {
		shift, base = true, r+('a'-'A')
	} else if b, ok := shiftBase[r]; ok {
		shift, base = true, b
	}
	if shift {
		_ = c.KeyEvent(ksShiftL, true)
		time.Sleep(modGap)
	}
	tap(c, uint32(base), keyHold)
	if shift {
		time.Sleep(modGap)
		_ = c.KeyEvent(ksShiftL, false)
	}
}

// selectAll selects the whole text of the focused field without ⌘A: move the
// caret to the very end (Down ×N), then extend the selection to the very
// start with Shift+Up ×N. Works in single- and multi-line fields.
func selectAll(c *rfb.Client) {
	const lines = 24
	for i := 0; i < lines; i++ {
		tap(c, ksDown, fastHold)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond)
	_ = c.KeyEvent(ksShiftL, true)
	time.Sleep(modGap)
	for i := 0; i < lines; i++ {
		tap(c, ksUp, fastHold)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(modGap)
	_ = c.KeyEvent(ksShiftL, false)
}

// clearField deletes the whole content of the focused field: select all,
// then Backspace.
func clearField(c *rfb.Client) {
	selectAll(c)
	time.Sleep(80 * time.Millisecond)
	tap(c, ksBksp, keyHold)
}
