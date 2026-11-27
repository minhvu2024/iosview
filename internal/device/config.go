package device

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Config is persisted next to the executable as iosview.json.
type Config struct {
	Listen       string `json:"listen"`
	VNCPort      int    `json:"vncPort"`
	Password     string `json:"password"`
	Quality      int    `json:"quality"`      // JPEG quality 0..9 for grid tiles
	FocusQuality int    `json:"focusQuality"` // JPEG quality 0..9 for the zoomed (focus) phone
	// NoH264 turns off the hardware H.264 stream (falls back to VNC JPEG).
	NoH264 bool `json:"noH264,omitempty"`
	// H264Port overrides the stream port (default: VNC port + 10).
	H264Port int `json:"h264Port,omitempty"`
	// NoPipeline turns off proxy-side frame request pipelining (debug only).
	NoPipeline  bool     `json:"noPipeline,omitempty"`
	StreamVer   int      `json:"streamVer"` // config migration marker for stream defaults
	WifiDevices []string `json:"wifiDevices"`
	// WifiLinks maps a Wi-Fi address to the UDID of the same phone, so one
	// phone seen over cable + Wi-Fi is a single device (cable preferred).
	WifiLinks map[string]string `json:"wifiLinks,omitempty"`
	Names     map[string]string `json:"names"`
	// Numbers: stable display number per device (01, 02 …), kept across runs.
	Numbers map[string]int        `json:"numbers,omitempty"`
	Parked  map[string]ParkedSlot `json:"parked,omitempty"`
	// Tags: named groups of devices for one-click selection.
	Tags      []Tag               `json:"tags,omitempty"`
	SyncOn    bool                `json:"syncOn"`
	SyncGroup map[string]struct{} `json:"syncGroup"`
	Known     map[string]bool     `json:"known"` // devices seen before (new ones auto-join the sync group)
	// StripAccents: type Vietnamese text without diacritics instead of using
	// the device clipboard (iOS ignores ⌘V from injected keyboards). Pointer so
	// an absent key in an old config defaults to true.
	StripAccents bool `json:"stripAccents"`
	// UI: the window's own data (custom shortcuts, quick phrases, notes),
	// stored as-is so it survives reinstalls of the window.
	UI json.RawMessage `json:"ui,omitempty"`

	path     string
	mu       sync.Mutex
	savedSeq uint64
}

// ParkedSlot preserves the last known screen shape while the phone is absent.
type ParkedSlot struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Tag is a named group of device IDs.
type Tag struct {
	Name string   `json:"name"`
	IDs  []string `json:"ids"`
}

// LoadConfig reads the config (or returns defaults if absent).
func LoadConfig(path string) (*Config, error) {
	c := &Config{Listen: "127.0.0.1:7788", VNCPort: 5901, Quality: 5, FocusQuality: 7, SyncOn: true, StripAccents: true, Names: map[string]string{}, SyncGroup: map[string]struct{}{}, Known: map[string]bool{}, path: path}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, c); err != nil {
			return nil, err
		}
	} else if os.IsNotExist(err) {
		c.StreamVer = 3 // fresh config already has the current defaults
	} else {
		return nil, err
	}
	// v2 stream defaults: grid tiles are small, so quality >6 only costs
	// bandwidth/CPU (phone encodes, PC decodes) without visible gain.
	if c.StreamVer < 2 {
		if c.Quality > 6 {
			c.Quality = 5
		}
		c.StreamVer = 2
	}
	// v3: 0.10.3 could drag the quality sliders down by accident (wheel over
	// the panel): bring very low values back to the defaults once.
	if c.StreamVer < 3 {
		if c.Quality < 3 {
			c.Quality = 5
		}
		if c.FocusQuality < 6 {
			c.FocusQuality = 7
		}
		c.StreamVer = 3
	}
	if c.FocusQuality <= 0 || c.FocusQuality > 9 {
		c.FocusQuality = 7
	}
	if c.VNCPort == 0 {
		c.VNCPort = 5901
	}
	if c.Names == nil {
		c.Names = map[string]string{}
	}
	if c.SyncGroup == nil {
		c.SyncGroup = map[string]struct{}{}
	}
	if c.Numbers == nil {
		c.Numbers = map[string]int{}
	}
	if c.WifiLinks == nil {
		c.WifiLinks = map[string]string{}
	}
	if c.Known == nil {
		c.Known = map[string]bool{}
	}
	return c, nil
}

// Save writes the config atomically.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return c.writeLocked(b)
}

// WriteSeq atomically writes pre-marshaled config bytes if no newer snapshot
// has already been written, so callers do not hold Manager.mu across disk I/O.
func (c *Config) WriteSeq(seq uint64, b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seq != 0 && seq < c.savedSeq {
		return nil
	}
	if err := c.writeLocked(b); err != nil {
		return err
	}
	c.savedSeq = seq
	return nil
}

func (c *Config) writeLocked(b []byte) error {
	tmp := c.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if err = os.Rename(tmp, c.path); err == nil {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return err
}
