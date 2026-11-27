package device

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"iosview/internal/rfb"
)

var bundleID = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)

// PhoneAction uses the same selection as touch, and only lists apps on the source phone.
func (m *Manager) PhoneAction(ctx context.Context, from, command, bundle string, one bool) ([]rfb.PhoneApp, string) {
	if command != "cc" && command != "apps" && (command != "open" || len(bundle) > 255 || !bundleID.MatchString(bundle)) {
		return nil, "Lệnh mở ứng dụng không hợp lệ"
	}
	if from == "" {
		return nil, "Chọn iPhone trước"
	}
	var devices []*Device
	if one || command == "apps" {
		if d := m.Get(from); d != nil {
			devices = []*Device{d}
		}
	} else {
		devices = m.targets(from)
	}
	if len(devices) == 0 {
		return nil, "Chưa có iPhone đang kết nối"
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var apps []rfb.PhoneApp
	var failures []string
	for _, d := range devices {
		m.mu.Lock()
		rc, _ := d.cur.(*rfb.Client)
		online := !d.Parked && d.Status == "online" && d.cur != nil
		no := d.No
		m.mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			var msg string
			switch {
			case !online:
				msg = "chưa kết nối"
			case rc != nil && rc.HasActions():
				list, err := rc.PhoneAction(ctx, command, bundle)
				if err != nil {
					msg = err.Error()
				} else if command == "apps" {
					mu.Lock()
					apps = list
					mu.Unlock()
				}
			case command == "cc":
				if m.deliver(d.ID, []*Device{d}, Button("cc")) == 0 {
					msg = "chưa gửi được cử chỉ"
				} else if rc != nil {
					msg = "đã gửi cử chỉ vuốt. Mở trực tiếp cần TrollVNC hỗ trợ phím tắt"
				}
			default:
				msg = "máy chưa hỗ trợ mở ứng dụng trực tiếp"
			}
			if msg != "" {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("Máy %d · %s", no, msg))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Strings(failures)
	return apps, strings.Join(failures, " · ")
}
