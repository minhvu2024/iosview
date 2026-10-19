package device

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxNameLen is the longest device / tag name kept (in characters).
const MaxNameLen = 32

// CleanName trims a user-entered name, drops control characters and limits it
// to MaxNameLen characters. "" means "use the default label".
func CleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > MaxNameLen {
		s = string([]rune(s)[:MaxNameLen])
		s = strings.TrimSpace(s)
	}
	return s
}

// numberLocked returns the stable display number of a device, assigning the
// lowest free one the first time a device is seen. Caller holds m.mu.
func (m *Manager) numberLocked(id string) int {
	if n := m.cfg.Numbers[id]; n > 0 {
		return n
	}
	used := map[int]bool{}
	for _, n := range m.cfg.Numbers {
		used[n] = true
	}
	n := 1
	for used[n] {
		n++
	}
	m.cfg.Numbers[id] = n
	go m.save()
	return n
}

// retagLocked replaces device ID from with to in every tag ("" removes it).
// Caller holds m.mu.
func (m *Manager) retagLocked(from, to string) {
	for i := range m.cfg.Tags {
		out := m.cfg.Tags[i].IDs[:0]
		seen := map[string]bool{}
		for _, id := range m.cfg.Tags[i].IDs {
			if id == from {
				id = to
			}
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
		m.cfg.Tags[i].IDs = out
	}
}

// SetTag creates or replaces the tag called name with the given devices.
// When rename is non-empty the tag currently called rename is renamed.
func (m *Manager) SetTag(name string, ids []string, rename string) {
	name = CleanName(name)
	if name == "" {
		return
	}
	m.mu.Lock()
	seen := map[string]bool{}
	clean := []string{}
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			clean = append(clean, id)
		}
	}
	key := name
	if rename != "" {
		key = rename
	}
	found := false
	for i := range m.cfg.Tags {
		if m.cfg.Tags[i].Name == key {
			m.cfg.Tags[i].Name = name
			if ids != nil {
				m.cfg.Tags[i].IDs = clean
			}
			found = true
			break
		}
	}
	if !found {
		m.cfg.Tags = append(m.cfg.Tags, Tag{Name: name, IDs: clean})
	}
	m.mu.Unlock()
	m.save()
	m.notify()
}

// DeleteTag removes a tag (the devices themselves are untouched).
func (m *Manager) DeleteTag(name string) {
	m.mu.Lock()
	out := m.cfg.Tags[:0]
	for _, t := range m.cfg.Tags {
		if t.Name != name {
			out = append(out, t)
		}
	}
	m.cfg.Tags = out
	m.mu.Unlock()
	m.save()
	m.notify()
}

// MaxNumber is the highest display number a device can be given.
const MaxNumber = 999

// SetNumber gives device id the display number n. If another phone (connected
// or not) already has n, the two swap numbers so numbers stay unique.
func (m *Manager) SetNumber(id string, n int) bool {
	if n < 1 || n > MaxNumber {
		return false
	}
	m.mu.Lock()
	d, ok := m.devices[id]
	if !ok {
		m.mu.Unlock()
		return false
	}
	old := d.No
	if old != n {
		for other, no := range m.cfg.Numbers {
			if no == n && other != id {
				m.cfg.Numbers[other] = old
				if od, ok := m.devices[other]; ok {
					od.No = old
				}
			}
		}
		m.cfg.Numbers[id] = n
		d.No = n
	}
	m.mu.Unlock()
	m.save()
	m.notify()
	return true
}
