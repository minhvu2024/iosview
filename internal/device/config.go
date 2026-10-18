package device

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
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
	WifiLinks map[string]string   `json:"wifiLinks,omitempty"`
	Names     map[string]string   `json:"names"`
	SyncOn    bool                `json:"syncOn"`
	SyncGroup map[string]struct{} `json:"syncGroup"`
	Known     map[string]bool     `json:"known"` // devices seen before (new ones auto-join the sync group)
	// StripAccents: type Vietnamese text without diacritics instead of using
	// the device clipboard (iOS ignores ⌘V from injected keyboards). Pointer so
	// an absent key in an old config defaults to true.
	StripAccents bool `json:"stripAccents"`

	path string
	mu   sync.Mutex
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
		c.StreamVer = 2 // fresh config already has the v2 defaults
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
	tmp := c.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}
