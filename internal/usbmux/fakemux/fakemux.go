// Package fakemux is a minimal usbmuxd emulator used for tests: it reports
// fake attached iPhones and tunnels Connect requests to local TCP ports.
package fakemux

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"iosview/internal/plist"
)

// Phone is one emulated device; Connect to device port P goes to Target[P].
type Phone struct {
	ID     int64
	UDID   string
	Target map[int]string
}

// Serve accepts usbmuxd clients on ln until it is closed.
func Serve(ln net.Listener, phones []Phone) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go handle(c, phones)
	}
}

func write(c net.Conn, m map[string]any) {
	body, _ := plist.Marshal(m)
	h := make([]byte, 16)
	binary.LittleEndian.PutUint32(h, uint32(16+len(body)))
	binary.LittleEndian.PutUint32(h[4:], 1)
	binary.LittleEndian.PutUint32(h[8:], 8)
	c.Write(append(h, body...))
}

func attached(p Phone) map[string]any {
	return map[string]any{
		"MessageType": "Attached", "DeviceID": p.ID,
		"Properties": map[string]any{"ConnectionType": "USB", "DeviceID": p.ID, "SerialNumber": p.UDID, "ProductID": int64(4776)},
	}
}

func handle(c net.Conn, phones []Phone) {
	h := make([]byte, 16)
	if _, err := io.ReadFull(c, h); err != nil {
		c.Close()
		return
	}
	body := make([]byte, binary.LittleEndian.Uint32(h)-16)
	io.ReadFull(c, body)
	v, err := plist.Unmarshal(body)
	if err != nil {
		c.Close()
		return
	}
	m := v.(map[string]any)
	switch m["MessageType"] {
	case "ListDevices":
		var list []any
		for _, p := range phones {
			list = append(list, attached(p))
		}
		write(c, map[string]any{"DeviceList": list})
		c.Close()
	case "Listen":
		write(c, map[string]any{"MessageType": "Result", "Number": int64(0)})
		for _, p := range phones {
			write(c, attached(p))
		}
		io.Copy(io.Discard, c) // keep open
	case "Connect":
		id, _ := plist.Int(m["DeviceID"])
		pn, _ := plist.Int(m["PortNumber"])
		port := int((pn&0xff)<<8 | (pn>>8)&0xff)
		for _, p := range phones {
			if p.ID == id {
				if t, ok := p.Target[port]; ok {
					up, err := net.Dial("tcp", t)
					if err == nil {
						write(c, map[string]any{"MessageType": "Result", "Number": int64(0)})
						go func() { io.Copy(up, c); up.Close() }()
						io.Copy(c, up)
						c.Close()
						return
					}
				}
			}
		}
		write(c, map[string]any{"MessageType": "Result", "Number": int64(3)})
		c.Close()
	default:
		fmt.Println("fakemux: unknown", m["MessageType"])
		c.Close()
	}
}
