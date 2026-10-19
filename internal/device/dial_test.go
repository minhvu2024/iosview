package device

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// Test network: 192.0.2.0/24 (TEST-NET-1) behaves like an iPhone whose Wi-Fi
// does not answer; addresses in dropFirst lose their first SYN. Installed
// once for the whole package so background managers never race on it.
var (
	testNetMu sync.Mutex
	dropFirst = map[string]bool{}
	attempts  = map[string]int{}
)

func init() {
	lanTimeout = 1500 * time.Millisecond
	real := dialAttempt
	dialAttempt = func(ctx context.Context, addr string) (net.Conn, error) {
		testNetMu.Lock()
		attempts[addr]++
		lost := strings.HasPrefix(addr, "192.0.2.") || (dropFirst[addr] && attempts[addr] == 1)
		testNetMu.Unlock()
		if lost {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return real(ctx, addr)
	}
}

func attemptsTo(addr string) int {
	testNetMu.Lock()
	defer testNetMu.Unlock()
	return attempts[addr]
}

// The first SYN is lost: a second attempt must connect long before the old
// 3 s timeout.
func TestDialLANRetriesLostSYN(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	addr := ln.Addr().String()
	testNetMu.Lock()
	dropFirst[addr] = true
	testNetMu.Unlock()
	start := time.Now()
	c, err := dialLAN(addr, 8*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if d := time.Since(start); d > 1500*time.Millisecond || attemptsTo(addr) < 2 {
		t.Fatalf("took %v, %d attempts", d, attemptsTo(addr))
	}
}

func TestDialLANRefusedAndTimeout(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close() // nothing listens: refused
	start := time.Now()
	_, err := dialLAN(addr, 8*time.Second)
	if !errors.Is(err, ErrPortClosed) || time.Since(start) > time.Second {
		t.Fatalf("refused: %v after %v", err, time.Since(start))
	}
	start = time.Now()
	_, err = dialLAN("192.0.2.200:5901", 2*time.Second)
	if err == nil || errors.Is(err, ErrPortClosed) || !strings.Contains(err.Error(), "Wi‑Fi") {
		t.Fatalf("timeout error: %v", err)
	}
	if d := time.Since(start); d < 1900*time.Millisecond || d > 2500*time.Millisecond {
		t.Fatalf("timeout took %v", d)
	}
	if n := attemptsTo("192.0.2.200:5901"); n < 3 {
		t.Fatalf("only %d attempts", n)
	}
}
