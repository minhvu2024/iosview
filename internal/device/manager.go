// Package device keeps track of every phone (USB via usbmuxd, or Wi-Fi by
// IP), keeps one persistent low-latency RFB control connection per phone and
// fans input out to the sync group.
package device

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"iosview/internal/rfb"
	"iosview/internal/usbmux"
)

// Device is one phone.
type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	No        int    `json:"no"` // stable display number (1-based)
	VNCName   string `json:"vncName"`
	Transport string `json:"transport"` // usb | wifi
	Addr      string `json:"addr"`      // host:port for wifi, UDID for usb
	Status    string `json:"status"`    // connecting | online | offline | error
	Error     string `json:"error,omitempty"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	InSync    bool   `json:"inSync"`
	UDID      string `json:"udid,omitempty"` // known once the phone is identified
	Wifi      string `json:"wifi,omitempty"` // Wi-Fi address of the same phone (fallback path)

	m        *Manager
	usbID    int64
	usbOK    bool // a usbmuxd path exists (cable preferred over Wi-Fi sync)
	usbCable bool // that path is a real cable, not usbmuxd Wi-Fi sync
	wifiAddr string
	present  bool
	queue    chan Event
	stop     chan struct{}
	attempts int
	owner    chan struct{} // held by the one writer that may drain queue
	cur      *rfb.Client   // live control session (guarded by m.mu)
	held     *ptrState     // last pointer state sent to the phone (writer-owned)
}

// ptrState is the button mask and pixel position the phone last received.
type ptrState struct {
	mask uint8
	x, y int
}

// DialH264 opens the phone's hardware H.264 stream (patched TrollVNC).
func (d *Device) DialH264() (net.Conn, error) {
	d.m.mu.Lock()
	viaUSB, addr, usbID, vport, hport := d.usbOK, d.wifiAddr, d.usbID, d.m.cfg.VNCPort, d.m.cfg.H264Port
	d.m.mu.Unlock()
	if viaUSB {
		if hport <= 0 {
			hport = vport + 10
		}
		return usbmux.Connect(usbID, uint16(hport))
	}
	host, ps, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if hport <= 0 {
		p, _ := strconv.Atoi(ps)
		hport = p + 10
	}
	return dialLAN(net.JoinHostPort(host, strconv.Itoa(hport)), lanTimeout)
}

// Dial opens a fresh TCP stream to the phone's VNC port (used both by the
// control connection and by the browser viewer proxy).
func (d *Device) Dial() (net.Conn, error) {
	d.m.mu.Lock()
	viaUSB, addr, usbID, port := d.usbOK, d.wifiAddr, d.usbID, d.m.cfg.VNCPort
	d.m.mu.Unlock()
	if viaUSB {
		return usbmux.Connect(usbID, uint16(port))
	}
	return dialLAN(addr, lanTimeout)
}

// refresh recomputes the user-visible path. USB always wins over Wi-Fi.
func (d *Device) refresh() {
	if d.usbOK {
		d.Transport, d.Addr = "usb", d.UDID
	} else {
		d.Transport, d.Addr = "wifi", d.wifiAddr
	}
	d.Wifi = d.wifiAddr
	d.present = d.usbOK || d.wifiAddr != ""
}

func (d *Device) setErr(msg string) {
	d.m.mu.Lock()
	d.Error = msg
	d.m.mu.Unlock()
	d.m.notify()
}

// Manager owns all devices.
type Manager struct {
	mu       sync.Mutex
	cfg      *Config
	devices  map[string]*Device
	syncOn   bool
	subs     map[chan struct{}]struct{}
	clipSubs map[chan ClipEvent]struct{}
	USBError string
	started  time.Time
}

// NewManager creates the manager and starts discovery.
func NewManager(cfg *Config) *Manager {
	m := &Manager{cfg: cfg, devices: map[string]*Device{}, subs: map[chan struct{}]struct{}{}, clipSubs: map[chan ClipEvent]struct{}{}, syncOn: cfg.SyncOn, started: time.Now()}
	// Hold the lock: newDevice may start a background save that reads cfg.
	m.mu.Lock()
	for _, a := range cfg.WifiDevices {
		m.addWifiLocked(a)
	}
	m.mu.Unlock()
	go m.watchUSB()
	return m
}

// Subscribe returns a channel that receives a tick whenever state changes.
func (m *Manager) Subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}
}

func (m *Manager) notify() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// State is the snapshot sent to the UI.
type State struct {
	Devices  []Device `json:"devices"`
	SyncOn   bool     `json:"syncOn"`
	USBError string   `json:"usbError,omitempty"`
	Password string   `json:"password"`
	Quality  int      `json:"quality"`
	FocusQ   int      `json:"focusQuality"`
	H264     bool     `json:"h264"`
	VNCPort  int      `json:"vncPort"`
	Strip    bool     `json:"stripAccents"`
	Tags     []Tag    `json:"tags"`
}

// Snapshot returns the current state.
func (m *Manager) Snapshot() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := State{SyncOn: m.syncOn, USBError: m.USBError, Password: m.cfg.Password, Quality: m.cfg.Quality, FocusQ: m.cfg.FocusQuality, H264: !m.cfg.NoH264, VNCPort: m.cfg.VNCPort, Strip: m.cfg.StripAccents}
	for _, d := range m.devices {
		cp := *d
		cp.m, cp.queue, cp.stop, cp.owner, cp.cur, cp.held = nil, nil, nil, nil, nil, nil
		st.Devices = append(st.Devices, cp)
	}
	sort.Slice(st.Devices, func(i, j int) bool {
		a, b := st.Devices[i], st.Devices[j]
		if a.No != b.No {
			return a.No < b.No
		}
		return a.ID < b.ID
	})
	st.Tags = make([]Tag, 0, len(m.cfg.Tags))
	for _, t := range m.cfg.Tags {
		st.Tags = append(st.Tags, Tag{Name: t.Name, IDs: append([]string(nil), t.IDs...)})
	}
	return st
}

// Pipeline reports whether the viewer proxy should pipeline frame requests.
func (m *Manager) Pipeline() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.cfg.NoPipeline
}

// Get returns a device by id.
func (m *Manager) Get(id string) *Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.devices[id]
}

func (m *Manager) newDevice(id, transport, addr string) *Device {
	d := &Device{
		ID: id, Transport: transport, Addr: addr, Status: "offline",
		m: m, queue: make(chan Event, queueSize), owner: make(chan struct{}, 1),
		held: &ptrState{},
	}
	d.Name = m.cfg.Names[id]
	d.No = m.numberLocked(id)
	_, d.InSync = m.cfg.SyncGroup[id]
	if !m.cfg.Known[id] {
		d.InSync = true // never seen before: join the sync group by default
		m.cfg.Known[id] = true
		go m.save()
	}
	m.devices[id] = d
	return d
}

func (m *Manager) addWifiLocked(addr string) *Device {
	if !strings.Contains(addr, ":") {
		addr = fmt.Sprintf("%s:%d", addr, m.cfg.VNCPort)
	}
	// Already known to be the same phone as a cable device: one device only.
	if udid := m.cfg.WifiLinks[addr]; udid != "" {
		return m.linkLocked(addr, udid)
	}
	id := "wifi:" + addr
	if d, ok := m.devices[id]; ok {
		return d
	}
	d := m.newDevice(id, "wifi", addr)
	d.wifiAddr = addr
	d.refresh()
	m.startLocked(d)
	go m.identify(addr)
	return d
}

// linkLocked makes Wi-Fi address addr a fallback path of the phone with this
// UDID (device id "usb:<udid>"), merging any separate "wifi:<addr>" entry into
// it. The cable stays the preferred path.
func (m *Manager) linkLocked(addr, udid string) *Device {
	id, wid := "usb:"+udid, "wifi:"+addr
	w := m.devices[wid]
	known := m.cfg.Known[id]
	d := m.devices[id]
	if d == nil {
		d = m.newDevice(id, "usb", udid)
		d.UDID = udid
		if w != nil && !known { // first sighting: inherit what the user set on the Wi-Fi entry
			d.InSync = w.InSync
			if d.Name == "" && w.Name != "" {
				d.Name = w.Name
				m.cfg.Names[id] = w.Name
			}
			// keep the number the user already knows this phone by
			if w.No > 0 {
				d.No = w.No
				m.cfg.Numbers[id] = w.No
			}
		}
	}
	if w != nil {
		delete(m.cfg.Numbers, wid)
		delete(m.cfg.Names, wid)
		m.retagLocked(wid, id)
		w.present = false
		if w.stop != nil {
			close(w.stop)
			w.stop = nil
		}
		delete(m.devices, wid)
	}
	m.cfg.WifiLinks[addr] = udid
	changed := d.wifiAddr != addr
	d.wifiAddr = addr
	d.refresh()
	if d.stop == nil {
		m.startLocked(d)
	} else if changed && !d.usbOK {
		m.rerouteLocked(d) // was connected through a different Wi-Fi path
	}
	return d
}

// rerouteLocked drops the current connection and reconnects over the best path.
func (m *Manager) rerouteLocked(d *Device) {
	if d.stop != nil {
		close(d.stop)
		d.stop = nil
	}
	d.attempts = 0
	if d.present {
		m.startLocked(d)
	} else {
		d.Status = "offline"
	}
}

// AddWifi adds a phone reachable at host[:port] over the LAN.
func (m *Manager) AddWifi(addr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	m.mu.Lock()
	d := m.addWifiLocked(addr)
	addr = d.wifiAddr
	found := false
	for _, a := range m.cfg.WifiDevices {
		if a == addr {
			found = true
		}
	}
	if !found {
		m.cfg.WifiDevices = append(m.cfg.WifiDevices, addr)
	}
	m.mu.Unlock()
	m.save()
	m.notify()
}

// Link tells the manager that the Wi-Fi entry wifiID is the same phone as the
// cable device usbID (manual fallback when automatic identification is not
// available, e.g. phones still running an older TrollVNC build).
func (m *Manager) Link(wifiID, usbID string) {
	m.mu.Lock()
	w, d := m.devices[wifiID], m.devices[usbID]
	if w == nil || d == nil || !strings.HasPrefix(wifiID, "wifi:") || d.UDID == "" || d.wifiAddr != "" {
		m.mu.Unlock()
		return
	}
	m.linkLocked(w.wifiAddr, d.UDID)
	m.mu.Unlock()
	m.save()
	m.notify()
}

// Remove forgets a Wi-Fi address (a cable device stays; it returns on re-plug).
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	d, ok := m.devices[id]
	if ok && d.wifiAddr != "" {
		addr := d.wifiAddr
		out := m.cfg.WifiDevices[:0]
		for _, a := range m.cfg.WifiDevices {
			if a != addr {
				out = append(out, a)
			}
		}
		m.cfg.WifiDevices = out
		delete(m.cfg.WifiLinks, addr)
		d.wifiAddr = ""
		d.refresh()
		if !d.present {
			if d.stop != nil {
				close(d.stop)
				d.stop = nil
			}
			delete(m.devices, id)
			delete(m.cfg.Numbers, id) // number becomes free for the next phone
			m.retagLocked(id, "")
		}
	}
	m.mu.Unlock()
	m.save()
	m.notify()
}

// identify asks a not-yet-identified Wi-Fi phone for its UDID (patched
// TrollVNC answers on the stream port) and merges it with its cable entry.
// Phones that cannot answer stay separate; the user can link them by hand.
func (m *Manager) identify(addr string) {
	delays := []time.Duration{500 * time.Millisecond, 4 * time.Second, 15 * time.Second, 45 * time.Second}
	for i := 0; ; i++ {
		d := delays[min(i, len(delays)-1)]
		if i >= len(delays) {
			d = 3 * time.Minute
		}
		time.Sleep(d)
		m.mu.Lock()
		_, alive := m.devices["wifi:"+addr]
		vport, hport := m.cfg.VNCPort, m.cfg.H264Port
		m.mu.Unlock()
		if !alive {
			return
		}
		udid, err := probeIdent(addr, vport, hport)
		if err != nil || udid == "" {
			continue
		}
		m.mu.Lock()
		if _, ok := m.devices["wifi:"+addr]; ok {
			m.linkLocked(addr, udid)
		}
		m.mu.Unlock()
		m.save()
		m.notify()
		return
	}
}

// probeIdent connects to the phone's stream port, sends 'I' and waits for the
// identity message (flag bit 1) among the framed messages.
func probeIdent(addr string, vport, hport int) (string, error) {
	host, ps, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if hport <= 0 {
		p, _ := strconv.Atoi(ps)
		if p == 0 {
			p = vport
		}
		hport = p + 10
	}
	c, err := dialLAN(net.JoinHostPort(host, strconv.Itoa(hport)), 3*time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err := c.Write([]byte{'I'}); err != nil {
		return "", err
	}
	hdr := make([]byte, 12)
	for n := 0; n < 64; n++ {
		if _, err := io.ReadFull(c, hdr); err != nil {
			return "", err
		}
		size := int(binary.BigEndian.Uint32(hdr))
		if size < 0 || size > 8<<20 {
			return "", errors.New("bad frame")
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(c, buf); err != nil {
			return "", err
		}
		if hdr[8]&2 != 0 {
			return strings.TrimSpace(string(buf)), nil
		}
	}
	return "", errors.New("no identity")
}

func (m *Manager) save() {
	m.mu.Lock()
	m.cfg.SyncOn = m.syncOn
	m.cfg.SyncGroup = map[string]struct{}{}
	for id, d := range m.devices {
		if d.InSync {
			m.cfg.SyncGroup[id] = struct{}{}
		}
	}
	// Marshal while still holding m.mu: other goroutines mutate cfg maps
	// (names, links...) under it.
	err := m.cfg.Save()
	m.mu.Unlock()
	if err != nil {
		log.Printf("lưu cấu hình lỗi: %v", err)
	}
}

// usbGrace: at start, how long a cable phone waits for usbmuxd before its
// Wi-Fi address is tried.
const usbGrace = 1500 * time.Millisecond

// startLocked launches the control-connection loop for d.
func (m *Manager) startLocked(d *Device) {
	if d.stop != nil {
		return
	}
	d.stop = make(chan struct{})
	go d.loop(d.stop)
}

func (d *Device) loop(stop chan struct{}) {
	m := d.m
	// A loop replaced by a path switch (Wi-Fi -> USB) may still be inside a
	// slow Wi-Fi dial. When that returns it must not touch the device any
	// more: it used to overwrite the live USB session's "online" with a Wi-Fi
	// timeout error, which blanked the picture and dropped the phone from sync.
	stopped := func() bool {
		select {
		case <-stop:
			return true
		default:
			return false
		}
	}
	for {
		select {
		case <-stop:
			return
		default:
		}
		m.mu.Lock()
		if !d.present {
			if d.stop == stop {
				d.stop = nil
			}
			m.mu.Unlock()
			return
		}
		// Right after start a phone known to have a cable is only in the
		// list through its saved Wi-Fi address: give usbmuxd a moment to
		// report the cable, so USB is used from the first connection.
		if wait := usbGrace - time.Since(m.started); wait > 0 && d.UDID != "" && !d.usbOK {
			m.mu.Unlock()
			select {
			case <-stop:
				return
			case <-time.After(wait):
			}
			continue
		}
		d.Status = "connecting"
		pw := m.cfg.Password
		m.mu.Unlock()
		m.notify()

		var c *rfb.Client
		conn, err := d.Dial()
		if err == nil {
			c, err = rfb.Handshake(conn, rfb.Options{
				Password:    pw,
				OnClipboard: func(text string) { m.publishClip(d.ID, text) },
				OnResize: func(w, h int) {
					m.mu.Lock()
					d.Width, d.Height = w, h
					m.mu.Unlock()
					m.notify()
				},
			})
		}
		if stopped() {
			if c != nil {
				c.Close()
			} else if conn != nil {
				conn.Close()
			}
			return
		}
		if err != nil {
			m.mu.Lock()
			if d.stop != stop { // replaced meanwhile (stop is closed just after)
				m.mu.Unlock()
				return
			}
			d.Status = "error"
			d.Error = err.Error()
			d.attempts++
			wait := time.Duration(min(d.attempts, 5)) * time.Second
			m.mu.Unlock()
			m.notify()
			select {
			case <-stop:
				return
			case <-time.After(wait):
			}
			continue
		}
		// Exactly one writer owns d.queue: wait until the previous session's
		// writer (e.g. before a USB <-> Wi-Fi switch) has fully exited, so it
		// can never steal events meant for this session.
		select {
		case d.owner <- struct{}{}:
		case <-stop:
			c.Close()
			return
		}
		if stopped() {
			<-d.owner
			c.Close()
			return
		}
		// Input queued for the old session is stale: never replay it.
	flushQ:
		for {
			select {
			case <-d.queue:
			default:
				break flushQ
			}
		}
		w, h := c.Size()
		m.mu.Lock()
		if d.stop != stop { // switched path while connecting
			m.mu.Unlock()
			<-d.owner
			c.Close()
			return
		}
		d.Status, d.Error, d.attempts = "online", "", 0
		d.Width, d.Height, d.VNCName = w, h, c.Name()
		d.cur = c
		m.mu.Unlock()
		m.notify()
		go func() {
			defer func() { <-d.owner }()
			d.runWriter(c)
		}()
		select {
		case <-stop:
			c.Close()
			m.mu.Lock()
			if d.cur == c {
				d.cur = nil
			}
			m.mu.Unlock()
			return
		case <-c.Done():
		}
		m.mu.Lock()
		if d.cur == c {
			d.cur = nil
		}
		d.Status = "offline"
		if e := c.Err(); e != nil {
			d.Error = e.Error()
		}
		m.mu.Unlock()
		m.notify()
		select {
		case <-stop:
			return
		case <-time.After(700 * time.Millisecond):
		}
	}
}

func (m *Manager) watchUSB() {
	events := make(chan usbmux.Event, 64)
	go func() {
		for ev := range events {
			m.onUSB(ev)
		}
	}()
	for {
		err := usbmux.Listen(events)
		m.mu.Lock()
		if err != nil {
			m.USBError = err.Error()
		}
		// usbmuxd went away: every USB path is gone (Wi-Fi paths stay).
		for _, d := range m.devices {
			if d.usbOK {
				m.usbGoneLocked(d)
			}
		}
		m.mu.Unlock()
		m.notify()
		time.Sleep(3 * time.Second)
	}
}

// usbGoneLocked removes d's usbmuxd path; a Wi-Fi fallback takes over if known.
func (m *Manager) usbGoneLocked(d *Device) {
	d.usbOK, d.usbCable = false, false
	d.refresh()
	m.rerouteLocked(d)
}

func (m *Manager) onUSB(ev usbmux.Event) {
	m.mu.Lock()
	m.USBError = ""
	if ev.Attached {
		udid := ev.Device.SerialNumber
		if udid == "" {
			m.mu.Unlock()
			return
		}
		cable := ev.Device.ConnectionType == "USB"
		// usbmuxd lists a Wi-Fi-sync phone as "Network"; it also tells us the
		// phone's IP, which links a LAN entry to this phone for free.
		if !cable && ev.Device.NetAddr != "" {
			addr := fmt.Sprintf("%s:%d", ev.Device.NetAddr, m.cfg.VNCPort)
			if _, ok := m.devices["wifi:"+addr]; ok {
				m.linkLocked(addr, udid)
			} else if d := m.devices["usb:"+udid]; d != nil && d.wifiAddr != "" && d.wifiAddr != addr {
				m.moveWifiLocked(d, addr) // DHCP gave the phone a new IP
			}
		}
		id := "usb:" + udid
		d, ok := m.devices[id]
		if !ok {
			d = m.newDevice(id, "usb", udid)
		}
		d.UDID = udid
		if d.usbOK && d.usbCable && !cable {
			m.mu.Unlock() // cable already in use: ignore the Wi-Fi-sync entry
			m.notify()
			return
		}
		switched := !d.usbOK || d.usbID != ev.Device.DeviceID
		d.usbID, d.usbOK, d.usbCable = ev.Device.DeviceID, true, cable
		d.refresh()
		if d.stop == nil {
			m.startLocked(d)
		} else if switched {
			m.rerouteLocked(d) // move from Wi-Fi (or an old cable id) to this path
		}
	} else {
		for _, d := range m.devices {
			if d.usbOK && d.usbID == ev.Device.DeviceID {
				m.usbGoneLocked(d)
			}
		}
	}
	m.mu.Unlock()
	m.notify()
}

// SetSync toggles sync mode.
func (m *Manager) SetSync(on bool) {
	m.mu.Lock()
	m.syncOn = on
	m.mu.Unlock()
	m.save()
	m.notify()
}

// SetH264 enables/disables the hardware H.264 stream.
func (m *Manager) SetH264(on bool) {
	m.mu.Lock()
	m.cfg.NoH264 = !on
	m.mu.Unlock()
	m.save()
	m.notify()
}

// SetInSync sets which devices belong to the sync group.
func (m *Manager) SetInSync(ids []string, in bool) {
	m.mu.Lock()
	for _, id := range ids {
		if d, ok := m.devices[id]; ok {
			d.InSync = in
		}
	}
	m.mu.Unlock()
	m.save()
	m.notify()
}

// Rename sets a friendly name.
func (m *Manager) Rename(id, name string) {
	name = CleanName(name)
	m.mu.Lock()
	d, ok := m.devices[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	d.Name = name
	if name == "" {
		delete(m.cfg.Names, id) // back to the default label
	} else {
		m.cfg.Names[id] = name
	}
	m.mu.Unlock()
	m.save()
	m.notify()
}

// SetSettings updates password / quality / port.
func (m *Manager) SetSettings(password string, quality, focusQ, port int, strip bool) {
	m.mu.Lock()
	m.cfg.StripAccents = strip
	changed := password != m.cfg.Password || (port > 0 && port != m.cfg.VNCPort)
	m.cfg.Password = password
	if quality >= 0 && quality <= 9 {
		m.cfg.Quality = quality
	}
	if focusQ >= 1 && focusQ <= 9 {
		m.cfg.FocusQuality = focusQ
	}
	if port > 0 && port < 65536 {
		m.cfg.VNCPort = port
	}
	var restart []*Device
	if changed {
		for _, d := range m.devices {
			if d.stop != nil {
				close(d.stop)
				d.stop = nil
			}
			restart = append(restart, d)
		}
	}
	for _, d := range restart {
		if d.present {
			d.attempts = 0
			m.startLocked(d)
		}
	}
	m.mu.Unlock()
	m.save()
	m.notify()
}

// targets resolves who receives input originating on device `from`.
// In sync mode, acting on any device that is in the group drives the whole
// group; otherwise only that device is driven.
func (m *Manager) targets(from string) []*Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Device
	src, ok := m.devices[from]
	if m.syncOn && (from == "" || (ok && src.InSync)) {
		for _, d := range m.devices {
			if d.InSync && d.Status == "online" {
				out = append(out, d)
			}
		}
		return out
	}
	if ok {
		out = append(out, src)
	}
	return out
}

// queueSize bounds the input backlog per phone. The writer coalesces moves, so
// a healthy phone never gets near it; reaching it means the phone stopped
// consuming input.
const queueSize = 1024

// Dispatch fans an event out. `from` is the device the user acted on. In
// sync mode an empty `from` (toolbar action) targets the whole group.
// It never blocks: one stuck phone cannot slow down the others.
func (m *Manager) Dispatch(from string, ev Event) int {
	return m.deliver(m.targets(from), ev)
}

// DispatchOne sends an event to device id only, ignoring the sync group
// (individual control while Alt is held).
func (m *Manager) DispatchOne(id string, ev Event) int {
	m.mu.Lock()
	d := m.devices[id]
	m.mu.Unlock()
	if d == nil {
		return 0
	}
	return m.deliver([]*Device{d}, ev)
}

func (m *Manager) deliver(ts []*Device, ev Event) int {
	for _, d := range ts {
		select {
		case d.queue <- ev:
		default:
			// Dropping just this event could lose a release and leave a
			// finger down on the phone. End the session instead: the
			// reconnect discards the stale queue and lifts anything still
			// pressed (see runWriter).
			m.mu.Lock()
			c := d.cur
			m.mu.Unlock()
			if c != nil {
				select {
				case <-c.Done():
				default:
					log.Printf("%s: hàng đợi input đầy, kết nối lại", d.ID)
					c.Close()
				}
			}
		}
	}
	return len(ts)
}

// Pointer helpers used by the HTTP layer.
func Pointer(x, y float64, mask uint8) Event { return Event{Kind: evPointer, X: x, Y: y, Mask: mask} }
func Key(ks uint32, down bool) Event         { return Event{Kind: evKey, Keysym: ks, Down: down} }
func Text(s string) Event                    { return Event{Kind: evText, Text: s} }
func ForceText(s string) Event               { return Event{Kind: evText, Text: s, Force: true} }
func Button(b string) Event                  { return Event{Kind: evButton, Button: b} }

// ClipEvent is a clipboard change reported by a phone.
type ClipEvent struct {
	Device string
	Text   string
}

// SubscribeClipboard streams phone clipboard changes.
func (m *Manager) SubscribeClipboard() (chan ClipEvent, func()) {
	ch := make(chan ClipEvent, 16)
	m.mu.Lock()
	m.clipSubs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.clipSubs, ch)
		m.mu.Unlock()
	}
}

func (m *Manager) publishClip(id, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.clipSubs {
		select {
		case ch <- ClipEvent{Device: id, Text: text}:
		default:
		}
	}
}

// Clients reports how many UI windows are connected (used for auto-exit).
func (m *Manager) Clients() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.clipSubs)
}

// UIPrefs returns the UI's stored data (JSON object, "{}" when empty).
func (m *Manager) UIPrefs() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cfg.UI) == 0 {
		return []byte("{}")
	}
	return append([]byte(nil), m.cfg.UI...)
}

// SetUIPrefs replaces the UI's stored data with a JSON object.
func (m *Manager) SetUIPrefs(b []byte) error {
	var v map[string]json.RawMessage
	if err := json.Unmarshal(b, &v); err != nil {
		return errors.New("cần một JSON object")
	}
	m.mu.Lock()
	m.cfg.UI = append(json.RawMessage(nil), b...)
	m.mu.Unlock()
	m.save()
	return nil
}

// moveWifiLocked replaces the Wi-Fi fallback address of a linked phone (its IP
// changed), so it is not dialed at a dead address forever.
func (m *Manager) moveWifiLocked(d *Device, addr string) {
	old := d.wifiAddr
	log.Printf("%s: IP Wi-Fi đổi %s -> %s", d.ID, old, addr)
	for i, a := range m.cfg.WifiDevices {
		if a == old {
			m.cfg.WifiDevices[i] = addr
		}
	}
	delete(m.cfg.WifiLinks, old)
	m.cfg.WifiLinks[addr] = d.UDID
	d.wifiAddr = addr
	d.refresh()
	if !d.usbOK {
		m.rerouteLocked(d)
	}
	go m.save()
}
