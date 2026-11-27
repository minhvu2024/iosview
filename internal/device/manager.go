// Package device keeps track of every phone (USB via usbmuxd, or Wi-Fi by
// IP), keeps one persistent low-latency RFB control connection per phone and
// fans input out to the sync group.
package device

import (
	"context"
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
	Parked    bool   `json:"parked,omitempty"`
	ScreenOff bool   `json:"screenOff,omitempty"` // last screen command sent by iOSView
	UDID      string `json:"udid,omitempty"`      // known once the phone is identified
	Wifi      string `json:"wifi,omitempty"`      // Wi-Fi address of the same phone (fallback path)
	WifiSync  bool   `json:"wifiSync,omitempty"`  // active path is Apple's wireless tunnel
	// Kind of control session: "" = TrollVNC, "cd" = CoreDevice (iOS 17.4+
	// without jailbreak, through the iosview-cd sidecar; cable only).
	Kind string `json:"kind,omitempty"`
	IOS  string `json:"ios,omitempty"` // iOS version (cable phones)

	m             *Manager
	usbID         int64
	usbOK         bool  // physical cable attached
	wifiMuxID     int64 // independent Network attachment, used only without a LAN address
	skipCable     bool  // active cable could not open a control tunnel
	preferMuxWiFi bool  // direct LAN could not open a control connection
	wifiAddr      string
	present       bool
	queue         chan Event
	stop          chan struct{}
	attempts      int
	owner         chan struct{} // held by the one writer that may drain queue
	cur           session       // live control session (guarded by m.mu)
	failedInput   session
	held          *ptrState // last pointer state sent to the phone (writer-owned)
}

// ptrState is the button mask, pixel position and timing the phone last received.
type ptrState struct {
	mask   uint8
	x, y   int
	at     time.Time
	moved  bool    // whether this touch gesture has sent at least one move
	edge   bool    // whether the press began on a screen edge
	jx, jy float64 // per-gesture micro-offset for sync followers
	rng    uint64  // writer-owned xorshift state
}

// DialH264 opens the phone's hardware H.264 stream (patched TrollVNC).
func (d *Device) DialH264(ctx context.Context) (net.Conn, error) {
	conn, _, err := d.DialVideo(ctx)
	return conn, err
}

// DialVideo also returns the route selected under the same lock as the address.
func (d *Device) DialVideo(ctx context.Context) (net.Conn, string, error) {
	d.m.mu.Lock()
	muxID, addr, vport, hport := d.activeMuxID(), d.wifiAddr, d.m.cfg.VNCPort, d.m.cfg.H264Port
	route := d.Transport
	if d.WifiSync {
		route = "wifi-apple"
	}
	d.m.mu.Unlock()
	if muxID != 0 {
		if hport <= 0 {
			hport = vport + 10
		}
		conn, err := usbmux.ConnectContext(ctx, muxID, uint16(hport), true)
		return conn, route, err
	}
	host, ps, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, route, err
	}
	if hport <= 0 {
		p, _ := strconv.Atoi(ps)
		hport = p + 10
	}
	conn, err := dialLANContext(ctx, net.JoinHostPort(host, strconv.Itoa(hport)), lanTimeout)
	return conn, route, err
}

// Dial opens a fresh TCP stream to the phone's VNC port (used both by the
// control connection and by the browser viewer proxy).
func (d *Device) Dial() (net.Conn, error) { return d.dial(context.Background()) }

func (d *Device) dial(ctx context.Context) (net.Conn, error) {
	d.m.mu.Lock()
	muxID, addr, port := d.activeMuxID(), d.wifiAddr, d.m.cfg.VNCPort
	d.m.mu.Unlock()
	if muxID != 0 {
		return usbmux.ConnectContext(ctx, muxID, uint16(port), false)
	}
	return dialLANContext(ctx, addr, lanTimeout)
}

// connect opens a control session over the best path. A cable phone without
// TrollVNC (port closed) on iOS 17.4+ switches to the CoreDevice driver.
// conn is the raw stream when a TrollVNC handshake failed (closed by caller).
func (d *Device) connect(ctx context.Context, pw string, mayProbe bool) (session, net.Conn, error) {
	m := d.m
	m.mu.Lock()
	kind, usb, usbID, udid, ios := d.Kind, d.Transport == "usb", d.usbID, d.UDID, d.IOS
	m.mu.Unlock()
	if kind == "cd" {
		if !usb {
			return nil, nil, errors.New("iPhone iOS 17+ không jailbreak: cần cắm cáp USB")
		}
		cl, err := startCD(udid, d.setErr, func(text string) { m.publishClip(d.ID, text) })
		if err != nil {
			return nil, nil, err
		}
		if cl.info.IOS != "" && ios == "" {
			m.mu.Lock()
			d.IOS = cl.info.IOS
			m.mu.Unlock()
		}
		return cl, nil, nil
	}
	conn, err := d.dial(ctx)
	if err != nil {
		if usb && errors.Is(err, ErrPortClosed) {
			if ios == "" && mayProbe {
				if v, pErr := usbmux.ProductVersion(usbID); pErr == nil {
					ios = v
					m.mu.Lock()
					d.IOS = v
					m.mu.Unlock()
					m.notify()
				}
			}
			if iosAtLeast(ios, 17, 4) {
				m.mu.Lock()
				d.Kind = "cd"
				m.mu.Unlock()
				log.Printf("%s: iOS %s không có TrollVNC — dùng CoreDevice", d.ID, ios)
				return d.connect(ctx, pw, false)
			}
		}
		if !errors.Is(err, ErrPortClosed) && ctx.Err() == nil {
			return nil, nil, &routeDialError{err}
		}
		return nil, nil, err
	}
	rc, err := rfb.Handshake(conn, rfb.Options{
		Password:    pw,
		OnClipboard: func(text string) { m.publishClip(d.ID, text) },
		OnResize: func(w, h int) {
			m.mu.Lock()
			if d.Width == w && d.Height == h {
				m.mu.Unlock()
				return
			}
			d.Width, d.Height = w, h
			m.mu.Unlock()
			m.notify()
		},
	})
	if err != nil {
		return nil, conn, err
	}
	return rc, conn, nil
}

// CDBase is the sidecar URL of a CoreDevice phone's live session ("" if the
// phone is not a CoreDevice phone or not connected).
func (d *Device) CDBase() string {
	d.m.mu.Lock()
	defer d.m.mu.Unlock()
	if c, ok := d.cur.(*cdClient); ok {
		return c.Base()
	}
	return ""
}

// NudgeSize asks a connected RFB control client for an immediate 1x1 update so
// framebuffer size changes (rotation) are picked up as soon as the H.264 stream
// reports a new orientation.
func (d *Device) NudgeSize() {
	d.m.mu.Lock()
	c, _ := d.cur.(*rfb.Client)
	d.m.mu.Unlock()
	if c != nil {
		c.PollSize()
	}
}

// IsCD reports whether the phone is driven through CoreDevice.
func (d *Device) IsCD() bool {
	d.m.mu.Lock()
	defer d.m.mu.Unlock()
	return d.Kind == "cd"
}

// Only errors opening a control connection are eligible for route failover.
type routeDialError struct{ error }

func (e *routeDialError) Unwrap() error { return e.error }

// Try the remaining advertised paths, then start the next round at the cable.
func (d *Device) nextControlRoute() {
	switch {
	case d.Transport == "usb":
		d.skipCable = d.wifiAddr != "" || d.wifiMuxID != 0
	case !d.WifiSync && d.wifiMuxID != 0:
		d.preferMuxWiFi = true
	default:
		d.skipCable, d.preferMuxWiFi = false, false
	}
	d.refresh()
}

func (d *Device) activeMuxID() int64 {
	if d.usbOK && (!d.skipCable || (d.wifiAddr == "" && d.wifiMuxID == 0)) {
		return d.usbID
	}
	if d.wifiAddr == "" || (d.preferMuxWiFi && d.wifiMuxID != 0) {
		return d.wifiMuxID
	}
	return 0
}

func (d *Device) refresh() {
	muxID := d.activeMuxID()
	d.WifiSync = muxID != 0 && muxID == d.wifiMuxID
	if d.usbOK && muxID == d.usbID {
		d.Transport, d.Addr = "usb", d.UDID
	} else {
		d.Transport, d.Addr = "wifi", d.wifiAddr
	}
	d.Wifi = d.wifiAddr
	d.present = d.usbOK || d.wifiAddr != "" || d.WifiSync
}

func (d *Device) setErr(msg string) {
	d.m.mu.Lock()
	if d.Error == msg {
		d.m.mu.Unlock()
		return
	}
	d.Error = msg
	d.m.mu.Unlock()
	d.m.notify()
}

// Manager owns all devices.
type Manager struct {
	mu           sync.Mutex
	cfg          *Config
	devices      map[string]*Device
	syncOn       bool
	copyBlank    bool
	writeClip    func(string) error
	clipWriteMu  sync.Mutex
	clipSequence uint64
	clipCopySubs map[chan ClipboardCopy]struct{}
	clipWritten  uint64
	lastSoloID   string
	lastSoloAt   time.Time
	clipBatch    *clipCollector
	subs         map[chan struct{}]struct{}
	clipSubs     map[chan ClipEvent]struct{}
	USBError     string
	started      time.Time
	saveSeq      uint64
	savePending  bool
	saveRunning  bool
}

// NewManager creates the manager and starts discovery.
func NewManager(cfg *Config) *Manager {
	m := &Manager{cfg: cfg, devices: map[string]*Device{}, subs: map[chan struct{}]struct{}{}, clipSubs: map[chan ClipEvent]struct{}{}, syncOn: cfg.SyncOn, writeClip: sysWriteClipboard, started: time.Now()}
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
	Devices     []Device `json:"devices"`
	WifiDevices []string `json:"wifiDevices"`
	SyncOn      bool     `json:"syncOn"`
	USBError    string   `json:"usbError,omitempty"`
	Password    string   `json:"password"`
	Quality     int      `json:"quality"`
	FocusQ      int      `json:"focusQuality"`
	H264        bool     `json:"h264"`
	VNCPort     int      `json:"vncPort"`
	Strip       bool     `json:"stripAccents"`
	Tags        []Tag    `json:"tags"`
}

// Snapshot returns the current state.
func (m *Manager) Snapshot() State {
	m.mu.Lock()
	st := State{
		WifiDevices: append([]string(nil), m.cfg.WifiDevices...),
		SyncOn:      m.syncOn,
		USBError:    m.USBError,
		Password:    m.cfg.Password,
		Quality:     m.cfg.Quality,
		FocusQ:      m.cfg.FocusQuality,
		H264:        !m.cfg.NoH264,
		VNCPort:     m.cfg.VNCPort,
		Strip:       m.cfg.StripAccents,
	}
	for _, d := range m.devices {
		cp := *d
		cp.m, cp.queue, cp.stop, cp.owner, cp.cur, cp.held, cp.failedInput = nil, nil, nil, nil, nil, nil, nil
		st.Devices = append(st.Devices, cp)
	}
	for id, slot := range m.cfg.Parked {
		if m.devices[id] == nil {
			transport, _, _ := strings.Cut(id, ":")
			st.Devices = append(st.Devices, Device{ID: id, No: m.cfg.Numbers[id], Name: m.cfg.Names[id],
				Transport: transport, Status: "offline", Parked: true, Width: slot.Width, Height: slot.Height})
		}
	}
	st.Tags = make([]Tag, 0, len(m.cfg.Tags))
	for _, t := range m.cfg.Tags {
		st.Tags = append(st.Tags, Tag{Name: t.Name, IDs: append([]string(nil), t.IDs...)})
	}
	m.mu.Unlock()
	sort.Slice(st.Devices, func(i, j int) bool {
		a, b := st.Devices[i], st.Devices[j]
		if a.No != b.No {
			return a.No < b.No
		}
		return a.ID < b.ID
	})
	return st
}

// Pipeline reports whether the viewer proxy should pipeline frame requests.
func (m *Manager) Pipeline() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.cfg.NoPipeline
}

// Get returns a device available for viewing or control.
func (m *Manager) Get(id string) *Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.devices[id]; d != nil && !d.Parked {
		return d
	}
	return nil
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
	if slot, parked := m.cfg.Parked[id]; parked {
		d.Parked, d.InSync = true, false
		d.Width, d.Height = slot.Width, slot.Height
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
		if slot, parked := m.cfg.Parked[wid]; parked {
			if !d.Parked {
				m.cfg.Parked[id] = slot
				d.Parked, d.InSync, d.Status = true, false, "offline"
				d.Width, d.Height = slot.Width, slot.Height
				m.rerouteLocked(d)
			}
			delete(m.cfg.Parked, wid)
		}
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
	previous := d.activeMuxID()
	if changed {
		d.preferMuxWiFi = false
	}
	d.wifiAddr = addr
	d.refresh()
	if d.stop == nil {
		m.startLocked(d)
	} else if changed && (previous != d.activeMuxID() || d.activeMuxID() == 0) {
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

// SetWifiList replaces the saved Wi-Fi IP list with addrs (one IP[:port] per
// entry), removes Wi-Fi devices omitted from the list, and assigns display
// numbers in the exact line order provided so employees can control ordering.
func (m *Manager) SetWifiList(addrs []string) error {
	m.mu.Lock()
	port := m.cfg.VNCPort
	if port <= 0 {
		port = 5901
	}
	seen := map[string]bool{}
	want := make([]string, 0, len(addrs))
	for _, raw := range addrs {
		a := strings.TrimSpace(raw)
		if a == "" {
			continue
		}
		host, ps, err := net.SplitHostPort(a)
		if err != nil {
			if net.ParseIP(a) == nil {
				m.mu.Unlock()
				return errors.New("Địa chỉ Wi-Fi không hợp lệ: " + a)
			}
			host, ps = a, strconv.Itoa(port)
		}
		p, err := strconv.Atoi(ps)
		if err != nil || p < 1 || p > 65535 || host == "" || strings.ContainsAny(host, " /\\\t\n") {
			m.mu.Unlock()
			return errors.New("Địa chỉ hoặc cổng Wi-Fi không hợp lệ: " + a)
		}
		if ip := net.ParseIP(host); ip != nil {
			host = ip.String()
		} else if strings.Contains(host, ":") || strings.Trim(host, "0123456789.") == "" {
			m.mu.Unlock()
			return errors.New("Địa chỉ Wi-Fi không hợp lệ: " + a)
		}
		a = net.JoinHostPort(host, strconv.Itoa(p))
		if seen[a] {
			continue
		}
		seen[a] = true
		want = append(want, a)
	}

	// Disconnect and forget Wi-Fi addresses no longer in the list.
	for id, d := range m.devices {
		if d.wifiAddr == "" || seen[d.wifiAddr] {
			continue
		}
		delete(m.cfg.WifiLinks, d.wifiAddr)
		previous := d.activeMuxID()
		d.wifiAddr = ""
		d.refresh()
		if d.present && d.activeMuxID() != previous {
			m.rerouteLocked(d)
		}
		if !d.present {
			if d.stop != nil {
				close(d.stop)
				d.stop = nil
			}
			delete(m.devices, id)
			if !d.Parked {
				delete(m.cfg.Numbers, id)
				m.retagLocked(id, "")
			}
		}
	}

	// Ensure every requested address exists in exact input order.
	m.cfg.WifiDevices = want
	ordered := make([]*Device, 0, len(want))
	usedID := map[string]bool{}
	for _, a := range want {
		d := m.addWifiLocked(a)
		if d != nil && !usedID[d.ID] {
			usedID[d.ID] = true
			ordered = append(ordered, d)
		}
	}

	// Reserve numbers for all other known devices, including disconnected USB phones.
	reserved := make(map[int]bool, len(m.cfg.Numbers))
	for id, no := range m.cfg.Numbers {
		if _, parked := m.cfg.Parked[id]; !usedID[id] || parked {
			reserved[no] = true
		}
	}
	no := 1
	for _, d := range ordered {
		if d.Parked {
			continue
		}
		for reserved[no] {
			no++
		}
		d.No = no
		m.cfg.Numbers[d.ID] = no
		no++
	}
	m.mu.Unlock()
	err := m.save()
	m.notify()
	return err
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
		previous := d.activeMuxID()
		d.wifiAddr = ""
		d.refresh()
		if d.present && d.activeMuxID() != previous {
			m.rerouteLocked(d)
		}
		if !d.present {
			if d.stop != nil {
				close(d.stop)
				d.stop = nil
			}
			delete(m.devices, id)
			if !d.Parked {
				delete(m.cfg.Numbers, id) // number becomes free for the next phone
				m.retagLocked(id, "")
			}
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

func (m *Manager) save() error {
	m.mu.Lock()
	m.cfg.SyncOn = m.syncOn
	m.cfg.SyncGroup = map[string]struct{}{}
	for id, d := range m.devices {
		if d.InSync {
			m.cfg.SyncGroup[id] = struct{}{}
		}
	}
	// Marshal under m.mu while cfg maps are consistent, then release m.mu
	// before touching the disk so Dispatch never stalls on file/AV I/O.
	b, err := json.MarshalIndent(m.cfg, "", "  ")
	m.saveSeq++
	seq := m.saveSeq
	m.mu.Unlock()
	if err == nil {
		err = m.cfg.WriteSeq(seq, b)
	}
	if err != nil {
		log.Printf("lưu cấu hình lỗi: %v", err)
	}
	return err
}

// saveLater coalesces state changes without making input wait for disk.
func (m *Manager) saveLater() {
	m.mu.Lock()
	m.savePending = true
	if m.saveRunning {
		m.mu.Unlock()
		return
	}
	m.saveRunning = true
	m.mu.Unlock()
	go func() {
		for {
			m.mu.Lock()
			if !m.savePending {
				m.saveRunning = false
				m.mu.Unlock()
				return
			}
			m.savePending = false
			m.mu.Unlock()
			m.save()
		}
	}()
}

// usbGrace: at start, how long a cable phone waits for usbmuxd before its
// Wi-Fi address is tried.
const usbGrace = 1500 * time.Millisecond

// startLocked launches the control-connection loop for d.
func (m *Manager) startLocked(d *Device) {
	if d.stop != nil || d.Parked {
		return
	}
	d.stop = make(chan struct{})
	go d.loop(d.stop)
}

func (d *Device) loop(stop chan struct{}) {
	m := d.m
	dialFailures := 0
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

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			select {
			case <-stop:
				cancel()
			case <-ctx.Done():
			}
		}()
		c, conn, err := d.connect(ctx, pw, true)
		cancel()
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
			var dialErr *routeDialError
			if errors.As(err, &dialErr) && d.Kind != "cd" {
				dialFailures++
				if dialFailures >= 2 {
					d.nextControlRoute()
					dialFailures = 0
				}
			} else {
				dialFailures = 0
			}
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
		w, h := sessionSize(c)
		m.mu.Lock()
		if d.stop != stop { // switched path while connecting
			m.mu.Unlock()
			<-d.owner
			c.Close()
			return
		}
		d.Status, d.Error, d.attempts = "online", "", 0
		dialFailures = 0
		d.Width, d.Height, d.VNCName = w, h, c.Name()
		d.cur = c
		d.failedInput = nil
		needIOS := d.usbOK && d.IOS == ""
		usbID := d.usbID
		m.mu.Unlock()
		m.notify()
		if needIOS {
			go func(id int64) {
				select {
				case <-stop:
					return
				case <-time.After(1500 * time.Millisecond):
				}
				if v, pErr := usbmux.ProductVersion(id); pErr == nil && v != "" {
					m.mu.Lock()
					if d.IOS == "" {
						d.IOS = v
						m.mu.Unlock()
						m.notify()
						return
					}
					m.mu.Unlock()
				}
			}(usbID)
		}
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
		if d.stop != stop {
			if d.cur == c {
				d.cur = nil
			}
			m.mu.Unlock()
			return
		}
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
	for {
		err := usbmux.Listen(m.onUSB)
		m.mu.Lock()
		if err != nil {
			m.USBError = err.Error()
		}
		// usbmuxd went away: every USB path is gone (Wi-Fi paths stay).
		for _, d := range m.devices {
			if !d.usbOK && d.wifiMuxID == 0 {
				continue
			}
			active := d.activeMuxID()
			d.usbOK, d.usbID, d.wifiMuxID = false, 0, 0
			d.skipCable, d.preferMuxWiFi = false, false
			d.refresh()
			if active != 0 {
				m.rerouteLocked(d)
			}
		}
		m.mu.Unlock()
		m.notify()
		time.Sleep(3 * time.Second)
	}
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
			if !cable {
				// Ignore unplugged iTunes Wi-Fi Sync announcements for phones not
				// already added by USB or explicit IP entry.
				m.mu.Unlock()
				return
			}
			d = m.newDevice(id, "usb", udid)
		}
		d.UDID = udid
		previous := d.activeMuxID()
		if cable {
			if !d.usbOK || d.usbID != ev.Device.DeviceID {
				d.skipCable, d.preferMuxWiFi = false, false
			}
			d.usbID, d.usbOK = ev.Device.DeviceID, true
		} else {
			d.wifiMuxID = ev.Device.DeviceID
		}
		d.refresh()
		if d.stop == nil {
			m.startLocked(d)
		} else if previous != d.activeMuxID() {
			m.rerouteLocked(d) // move from Wi-Fi (or an old cable id) to this path
		}
	} else {
		for _, d := range m.devices {
			previous := d.activeMuxID()
			if d.usbOK && d.usbID == ev.Device.DeviceID {
				d.usbOK, d.usbID = false, 0
				d.skipCable = false
			} else if d.wifiMuxID == ev.Device.DeviceID {
				d.wifiMuxID = 0
				d.preferMuxWiFi = false
			} else {
				continue
			}
			d.refresh()
			if previous != d.activeMuxID() {
				m.rerouteLocked(d)
			}
		}
	}
	m.mu.Unlock()
	m.notify()
}

// SetSync toggles sync mode.
func (m *Manager) SetSync(on bool) {
	m.applySync(on)
	m.save()
}

// ApplySync updates input routing immediately and persists it in the background.
func (m *Manager) ApplySync(on bool) {
	m.applySync(on)
	m.saveLater()
}

func (m *Manager) applySync(on bool) {
	m.mu.Lock()
	m.syncOn = on
	m.mu.Unlock()
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
	m.applyInSync(ids, in)
	m.save()
}

// ApplyInSync updates membership before the next input from the same socket.
func (m *Manager) ApplyInSync(ids []string, in bool) {
	m.applyInSync(ids, in)
	m.saveLater()
}

func (m *Manager) applyInSync(ids []string, in bool) {
	m.mu.Lock()
	for _, id := range ids {
		if d, ok := m.devices[id]; ok && !d.Parked {
			d.InSync = in
		}
	}
	m.mu.Unlock()
	m.notify()
}

// ApplyParked reserves a slot while stopping that phone's control connection.
func (m *Manager) ApplyParked(id string, parked bool) {
	m.ApplyParkedGroup([]string{id}, parked)
}

func (m *Manager) ApplyParkedGroup(ids []string, parked bool) {
	m.mu.Lock()
	changed := false
	for _, id := range ids {
		if m.setParkedLocked(id, parked) {
			changed = true
		}
	}
	m.mu.Unlock()
	if changed {
		m.notify()
		m.saveLater()
	}
}

func (m *Manager) setParkedLocked(id string, parked bool) bool {
	d := m.devices[id]
	if parked {
		if d == nil || d.Parked {
			return false
		}
		if m.cfg.Parked == nil {
			m.cfg.Parked = make(map[string]ParkedSlot)
		}
		m.cfg.Parked[id] = ParkedSlot{Width: d.Width, Height: d.Height}
		d.Parked, d.InSync, d.Status = true, false, "offline"
		m.rerouteLocked(d)
		if m.lastSoloID == id {
			m.lastSoloID = ""
		}
	} else {
		if _, exists := m.cfg.Parked[id]; !exists && (d == nil || !d.Parked) {
			return false
		}
		if slot, exists := m.cfg.Parked[id]; exists && d == nil {
			transport, addr, _ := strings.Cut(id, ":")
			d = m.newDevice(id, transport, addr)
			d.Width, d.Height = slot.Width, slot.Height
			if transport == "usb" {
				d.UDID = addr
			}
		}
		delete(m.cfg.Parked, id)
		if d != nil && d.Parked {
			d.Parked = false
			m.rerouteLocked(d)
		}
	}
	return true
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
	if ok && src.Parked {
		return nil
	}
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
	return m.deliver(from, m.targets(from), ev)
}

// DispatchOne sends an event to device id only, ignoring the sync group
// (individual control while Alt is held).
func (m *Manager) DispatchOne(id string, ev Event) int {
	m.mu.Lock()
	d := m.devices[id]
	if d != nil && !d.Parked {
		m.lastSoloID = id
		m.lastSoloAt = time.Now()
	}
	m.mu.Unlock()
	if d == nil {
		return 0
	}
	return m.deliver(id, []*Device{d}, ev)
}

func (m *Manager) deliver(from string, ts []*Device, ev Event) int {
	if len(ts) > 1 {
		m.mu.Lock()
		m.lastSoloID = ""
		m.mu.Unlock()
	}
	accepted := 0
	for _, d := range ts {
		devEv := ev
		if len(ts) > 1 && from != "" && d.ID != from {
			devEv.Follower = true
		}
		m.mu.Lock()
		if (ev.Kind != evText || (d.Status == "online" && d.cur != nil && m.devices[d.ID] == d)) && m.enqueueInputLocked(d, devEv) {
			accepted++
		}
		m.mu.Unlock()
	}
	return accepted
}

// InputTarget records the device and the connection that received a press.
type InputTarget struct {
	device *Device
	conn   session
}

func (m *Manager) StartTouch(from string, one bool, ev Event) []InputTarget {
	var devices []*Device
	if one {
		m.mu.Lock()
		if d := m.devices[from]; d != nil && !d.Parked {
			devices = append(devices, d)
			m.lastSoloID, m.lastSoloAt = from, time.Now()
		}
		m.mu.Unlock()
	} else {
		devices = m.targets(from)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(devices) > 1 {
		m.lastSoloID = ""
	}
	var targets []InputTarget
	for _, d := range devices {
		if d.Status == "online" && d.cur != nil && m.enqueueInputLocked(d, ev) {
			targets = append(targets, InputTarget{d, d.cur})
		}
	}
	return targets
}

func (m *Manager) ContinueTouch(targets []InputTarget, ev Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, target := range targets {
		d := target.device
		if d.cur == target.conn && d.Status == "online" && m.devices[d.ID] == d {
			m.enqueueInputLocked(d, ev)
		}
	}
}

func (m *Manager) enqueueInputLocked(d *Device, ev Event) bool {
	if d.Parked {
		return false
	}
	select {
	case d.queue <- ev:
		return true
	default:
		// Close only this session, outside the group dispatch path.
		if c := d.cur; c != nil && d.failedInput != c {
			d.failedInput = c
			go func() {
				log.Printf("%s: hàng đợi input đầy, kết nối lại", d.ID)
				c.Close()
			}()
		}
		return false
	}
}

// Pointer helpers used by the HTTP layer.
func Pointer(x, y float64, mask uint8) Event { return Event{Kind: evPointer, X: x, Y: y, Mask: mask} }
func Wheel(x, y float64, mask uint8, up bool) Event {
	return Event{Kind: evWheel, X: x, Y: y, Mask: mask, Down: up}
}
func Key(ks uint32, down bool) Event { return Event{Kind: evKey, Keysym: ks, Down: down} }
func Text(s string) Event            { return Event{Kind: evText, Text: s} }
func ForceText(s string) Event       { return Event{Kind: evText, Text: s, Force: true} }
func Button(b string) Event          { return Event{Kind: evButton, Button: b} }

// ClipEvent is a clipboard change reported by a phone.
type ClipEvent struct {
	Device string
	Text   string
}

// SubscribeClipboard streams phone clipboard changes.
func (m *Manager) SubscribeClipboard() (chan ClipEvent, func()) {
	ch := make(chan ClipEvent, 256)
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
	m.onPhoneClip(id, text)
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
	return m.save()
}

// moveWifiLocked replaces the Wi-Fi fallback address of a linked phone (its IP
// changed), so it is not dialed at a dead address forever.
func (m *Manager) moveWifiLocked(d *Device, addr string) {
	previous := d.activeMuxID()
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
	d.preferMuxWiFi = false
	d.refresh()
	if previous != d.activeMuxID() || d.activeMuxID() == 0 {
		m.rerouteLocked(d)
	}
	go m.save()
}
