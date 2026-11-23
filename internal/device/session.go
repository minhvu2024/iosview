package device

// session is one live control connection to a phone. Two kinds exist:
// *rfb.Client (TrollVNC on jailbroken / TrollStore phones) and *cdClient
// (iOS 17.4+ without jailbreak, through Apple's CoreDevice services via the
// iosview-cd sidecar). The writer, typing and buttons only use this interface.
type session interface {
	Size() (int, int)
	Name() string
	Done() <-chan struct{}
	Err() error
	Close() error
	PointerEvent(mask uint8, x, y int) error
	WritePointerBatch(ev [][3]int) error
	KeyEvent(keysym uint32, down bool) error
	SetClipboard(text string) error
}

// paster is a session that can paste any text (accents, emoji) natively:
// set the phone clipboard, then ⌘V. TrollVNC on iOS 15 cannot (iOS ignores
// ⌘ from its injected keyboard), CoreDevice's virtual keyboard can.
type paster interface {
	Paste(text string) error
}

// screenSizer reports the phone's screen in pixels when Size() is in another
// unit (CoreDevice touches use 0..65535 on both axes).
type screenSizer interface {
	ScreenSize() (int, int)
}

// sessionSize is the screen size shown in the UI for s.
func sessionSize(s session) (int, int) {
	if ss, ok := s.(screenSizer); ok {
		return ss.ScreenSize()
	}
	return s.Size()
}
