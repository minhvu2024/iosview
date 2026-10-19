package device

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"iosview/internal/usbmux"
)

// Wi-Fi dialing.
//
// Windows retransmits a connection's first SYN only after 3 s. With the old
// 3 s timeout a single lost SYN — common while an iPhone's Wi-Fi is in power
// save, or before ARP has resolved — ended in "dial tcp …: i/o timeout".
// dialLAN instead starts a fresh attempt (its own SYN) every lanRetry and the
// first one that connects wins, until lanTimeout.
var (
	lanTimeout = 5 * time.Second // var: shortened in tests
	lanRetry   = 700 * time.Millisecond
)

// ErrPortClosed: the phone answered but nothing listens on that port (e.g. a
// TrollVNC without the iOSView H.264 patch). Not worth retrying soon.
var ErrPortClosed = usbmux.ErrPortClosed

// dialAttempt is one connection attempt (replaced in tests).
var dialAttempt = func(ctx context.Context, addr string) (net.Conn, error) {
	var dl net.Dialer
	return dl.DialContext(ctx, "tcp", addr)
}

func dialLAN(addr string, total time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), total)
	defer cancel()
	type result struct {
		c   net.Conn
		err error
	}
	out := make(chan result)
	attempt := func() {
		c, err := dialAttempt(ctx, addr)
		select {
		case out <- result{c, err}:
		case <-ctx.Done():
			if c != nil {
				c.Close() // another attempt already won
			}
		}
	}
	go attempt()
	running := 1
	tick := time.NewTicker(lanRetry)
	defer tick.Stop()
	for {
		select {
		case r := <-out:
			running--
			if r.err == nil {
				if tc, ok := r.c.(*net.TCPConn); ok {
					_ = tc.SetNoDelay(true)
				}
				return r.c, nil
			}
			if ctx.Err() == nil && !isTimeout(r.err) {
				return nil, lanError(addr, r.err) // refused / unreachable: no point retrying now
			}
			if running == 0 && ctx.Err() != nil {
				return nil, lanError(addr, context.DeadlineExceeded)
			}
		case <-tick.C:
			if ctx.Err() == nil {
				running++
				go attempt()
			}
		case <-ctx.Done():
			return nil, lanError(addr, context.DeadlineExceeded)
		}
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}

// lanError turns a dial error into something the user can act on.
func lanError(addr string, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "refused"):
		return fmt.Errorf("iPhone %s từ chối kết nối — TrollVNC chưa chạy hoặc sai cổng: %w", addr, ErrPortClosed)
	case isTimeout(err):
		return fmt.Errorf("không tới được iPhone %s qua Wi‑Fi sau %d giây — iPhone đang khoá màn hình / tắt Wi‑Fi, hoặc đã đổi IP", addr, int(lanTimeout/time.Second))
	case strings.Contains(msg, "unreachable") || strings.Contains(msg, "no route"):
		return fmt.Errorf("không có đường tới %s — PC và iPhone không cùng mạng Wi‑Fi? (%v)", addr, err)
	}
	return fmt.Errorf("Wi‑Fi %s: %w", addr, err)
}
