// fakemux runs a usbmuxd emulator for local testing without an iPhone:
//
//	go run ./tools/fakemux -listen 127.0.0.1:27100 -phone UDID1=127.0.0.1:5913
//
// then start iosview with USBMUXD_SOCKET_ADDRESS=127.0.0.1:27100.
package main

import (
	"flag"
	"log"
	"net"
	"strings"

	"iosview/internal/usbmux/fakemux"
)

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(s string) error { *l = append(*l, s); return nil }

func main() {
	listen := flag.String("listen", "127.0.0.1:27100", "")
	port := flag.Int("port", 5901, "device-side VNC port")
	var phones list
	flag.Var(&phones, "phone", "UDID=host:port (repeatable)")
	flag.Parse()
	var ps []fakemux.Phone
	for i, p := range phones {
		kv := strings.SplitN(p, "=", 2)
		ps = append(ps, fakemux.Phone{ID: int64(i + 1), UDID: kv[0], Target: map[int]string{*port: kv[1]}})
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("fake usbmuxd on %s with %d phones", *listen, len(ps))
	fakemux.Serve(ln, ps)
}
