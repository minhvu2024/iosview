package device

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// ScanLAN probes every host of the PC's local /24 networks for a VNC server
// on the configured port and adds the ones that answer with an RFB banner.
// It returns the addresses found.
func (m *Manager) ScanLAN() []string {
	m.mu.Lock()
	port := m.cfg.VNCPort
	m.mu.Unlock()

	var hosts []string
	ifaces, _ := net.Interfaces()
	seen := map[string]bool{}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || ip4[0] == 169 {
				continue
			}
			prefix := fmt.Sprintf("%d.%d.%d.", ip4[0], ip4[1], ip4[2])
			if seen[prefix] {
				continue
			}
			seen[prefix] = true
			for i := 1; i < 255; i++ {
				if byte(i) == ip4[3] {
					continue
				}
				hosts = append(hosts, fmt.Sprintf("%s%d", prefix, i))
			}
		}
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		found []string
		sem   = make(chan struct{}, 128)
	)
	for _, h := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(h string) {
			defer wg.Done()
			defer func() { <-sem }()
			addr := fmt.Sprintf("%s:%d", h, port)
			c, err := net.DialTimeout("tcp", addr, 400*time.Millisecond)
			if err != nil {
				return
			}
			defer c.Close()
			_ = c.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
			b := make([]byte, 12)
			if _, err := io.ReadFull(c, b); err != nil || !strings.HasPrefix(string(b), "RFB ") {
				return
			}
			mu.Lock()
			found = append(found, addr)
			mu.Unlock()
		}(h)
	}
	wg.Wait()
	for _, a := range found {
		m.AddWifi(a)
	}
	return found
}
