package usbmux

import "testing"

func TestSockaddr(t *testing.T) {
	if ip := ipFromSockaddr([]byte{16, 2, 0, 0, 192, 168, 1, 7, 0, 0, 0, 0, 0, 0, 0, 0}); ip != "192.168.1.7" {
		t.Fatal(ip)
	}
	if ip := ipFromSockaddr([]byte{2, 0, 0, 0, 10, 0, 0, 9}); ip != "10.0.0.9" {
		t.Fatal(ip)
	}
	if ip := ipFromSockaddr([]byte{28, 30, 0, 0}); ip != "" {
		t.Fatal(ip)
	}
}
