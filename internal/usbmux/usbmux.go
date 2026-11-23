// Package usbmux is a minimal client for usbmuxd (Apple Mobile Device Service
// on Windows, usbmuxd on macOS/Linux). It lists attached iPhones, watches
// attach/detach events and opens raw TCP tunnels to a port on the device —
// the same thing `iproxy` does.
package usbmux

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"iosview/internal/plist"
)

const (
	msgPlist     = 8
	protoVersion = 1
)

// Device is one entry reported by usbmuxd.
type Device struct {
	DeviceID       int64
	SerialNumber   string // UDID
	ConnectionType string // "USB" or "Network"
	ProductID      int64
	NetAddr        string // phone's IPv4 for "Network" (Wi-Fi sync) entries, if reported
}

// ipFromSockaddr extracts an IPv4 address from the sockaddr blob usbmuxd
// reports in NetworkAddress (BSD or Linux layout).
func ipFromSockaddr(b []byte) string {
	if len(b) < 8 || !(b[1] == 2 || (b[0] == 2 && b[1] == 0)) {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d", b[4], b[5], b[6], b[7])
}

var tagCounter uint32

// ErrPortClosed: the phone is reachable but nothing listens on that port.
var ErrPortClosed = errors.New("port closed")

// Address returns the usbmuxd endpoint as (network, address).
// USBMUXD_SOCKET_ADDRESS can override it ("UNIX:/path" or "host:port").
func Address() (string, string) {
	if a := os.Getenv("USBMUXD_SOCKET_ADDRESS"); a != "" {
		if strings.HasPrefix(strings.ToUpper(a), "UNIX:") {
			return "unix", a[5:]
		}
		return "tcp", a
	}
	if runtime.GOOS == "windows" {
		return "tcp", "127.0.0.1:27015"
	}
	return "unix", "/var/run/usbmuxd"
}

func dial() (net.Conn, error) {
	n, a := Address()
	c, err := net.DialTimeout(n, a, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("không kết nối được usbmuxd (%s %s) — đã cài Apple Devices/iTunes chưa? %w", n, a, err)
	}
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	return c, nil
}

func baseMsg(t string) map[string]any {
	return map[string]any{
		"MessageType":         t,
		"ClientVersionString": "iosview-1.0",
		"ProgName":            "iosview",
		"kLibUSBMuxVersion":   int64(3),
	}
}

func send(c net.Conn, m map[string]any) error {
	body, err := plist.Marshal(m)
	if err != nil {
		return err
	}
	hdr := make([]byte, 16)
	binary.LittleEndian.PutUint32(hdr[0:], uint32(16+len(body)))
	binary.LittleEndian.PutUint32(hdr[4:], protoVersion)
	binary.LittleEndian.PutUint32(hdr[8:], msgPlist)
	binary.LittleEndian.PutUint32(hdr[12:], atomic.AddUint32(&tagCounter, 1))
	_, err = c.Write(append(hdr, body...))
	return err
}

func recv(c net.Conn) (map[string]any, error) {
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[0:])
	if n < 16 || n > 4<<20 {
		return nil, fmt.Errorf("usbmux: bad packet length %d", n)
	}
	body := make([]byte, n-16)
	if _, err := io.ReadFull(c, body); err != nil {
		return nil, err
	}
	v, err := plist.Unmarshal(body)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("usbmux: response is not a dict")
	}
	return m, nil
}

func parseDevice(m map[string]any) Device {
	var d Device
	if id, ok := plist.Int(m["DeviceID"]); ok {
		d.DeviceID = id
	}
	props, _ := m["Properties"].(map[string]any)
	if props != nil {
		if id, ok := plist.Int(props["DeviceID"]); ok && d.DeviceID == 0 {
			d.DeviceID = id
		}
		d.SerialNumber, _ = props["SerialNumber"].(string)
		d.ConnectionType, _ = props["ConnectionType"].(string)
		d.ProductID, _ = plist.Int(props["ProductID"])
		if b, ok := props["NetworkAddress"].([]byte); ok {
			d.NetAddr = ipFromSockaddr(b)
		}
	}
	return d
}

// ListDevices returns the devices usbmuxd currently knows about.
func ListDevices() ([]Device, error) {
	c, err := dial()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if err := send(c, baseMsg("ListDevices")); err != nil {
		return nil, err
	}
	resp, err := recv(c)
	if err != nil {
		return nil, err
	}
	list, _ := resp["DeviceList"].([]any)
	out := make([]Device, 0, len(list))
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			out = append(out, parseDevice(m))
		}
	}
	return out, nil
}

// Event is an attach/detach notification.
type Event struct {
	Attached bool
	Device   Device
}

// Listen streams attach/detach events until the connection drops.
// The current device list is delivered as Attached events first.
func Listen(events chan<- Event) error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	if err := send(c, baseMsg("Listen")); err != nil {
		return err
	}
	for {
		m, err := recv(c)
		if err != nil {
			return err
		}
		switch m["MessageType"] {
		case "Result":
			if n, _ := plist.Int(m["Number"]); n != 0 {
				return fmt.Errorf("usbmux Listen failed: %d", n)
			}
		case "Attached":
			events <- Event{Attached: true, Device: parseDevice(m)}
		case "Detached":
			id, _ := plist.Int(m["DeviceID"])
			events <- Event{Attached: false, Device: Device{DeviceID: id}}
		}
	}
}

// Connect opens a raw TCP tunnel to port on the device. The returned conn
// carries the device's TCP stream directly.
func Connect(deviceID int64, port uint16) (net.Conn, error) {
	c, err := dial()
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	m := baseMsg("Connect")
	m["DeviceID"] = deviceID
	// usbmuxd expects the port in network byte order.
	m["PortNumber"] = int64((port&0xff)<<8 | port>>8)
	if err := send(c, m); err != nil {
		c.Close()
		return nil, err
	}
	resp, err := recv(c)
	if err != nil {
		c.Close()
		return nil, err
	}
	if n, _ := plist.Int(resp["Number"]); n != 0 {
		c.Close()
		switch n {
		case 2:
			return nil, fmt.Errorf("thiết bị chưa sẵn sàng/không tìm thấy (usbmux=%d)", n)
		case 3:
			return nil, fmt.Errorf("cổng %d trên iPhone đang đóng — TrollVNC đã chạy chưa? (usbmux=%d): %w", port, n, ErrPortClosed)
		}
		return nil, fmt.Errorf("usbmux Connect lỗi %d", n)
	}
	_ = c.SetDeadline(time.Time{})
	return c, nil
}

// lockdownPort is lockdownd on the phone (always reachable through usbmuxd).
const lockdownPort = 62078

// ProductVersion asks lockdownd for the iOS version ("17.5.1"). This key is
// answered without pairing, so it works on a phone that was never trusted.
func ProductVersion(deviceID int64) (string, error) {
	c, err := Connect(deviceID, lockdownPort)
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(4 * time.Second))
	body, err := plist.Marshal(map[string]any{"Label": "iosview", "Request": "GetValue", "Key": "ProductVersion"})
	if err != nil {
		return "", err
	}
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, uint32(len(body)))
	if _, err := c.Write(append(hdr, body...)); err != nil {
		return "", err
	}
	if _, err := io.ReadFull(c, hdr); err != nil {
		return "", err
	}
	n := binary.BigEndian.Uint32(hdr)
	if n == 0 || n > 1<<20 {
		return "", fmt.Errorf("lockdown: bad reply length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		return "", err
	}
	v, err := plist.Unmarshal(buf)
	if err != nil {
		return "", err
	}
	m, _ := v.(map[string]any)
	if s, _ := m["Value"].(string); s != "" {
		return s, nil
	}
	if e, _ := m["Error"].(string); e != "" {
		return "", fmt.Errorf("lockdown: %s", e)
	}
	return "", errors.New("lockdown: no ProductVersion")
}
