package device

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// CopyClipboards snapshots selection and names before reading phones in parallel.
func (m *Manager) CopyClipboards(ctx context.Context, command bool, blankLine bool) string {
	type target struct {
		no int
		id string
		s  session
	}
	m.mu.Lock()
	selected := false
	for _, d := range m.devices {
		selected = selected || d.InSync
	}
	var list []target
	for _, d := range m.devices {
		if (selected && !d.InSync) || (!selected && d.Status != "online") {
			continue
		}
		cur := d.cur
		if d.Status != "online" {
			cur = nil
		}
		list = append(list, target{d.No, d.ID, cur})
	}
	m.mu.Unlock()
	sort.Slice(list, func(i, j int) bool {
		if list[i].no != list[j].no {
			return list[i].no < list[j].no
		}
		return list[i].id < list[j].id
	})
	blocks := make([]string, len(list))
	var wg sync.WaitGroup
	for i, d := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			text := ""
			// CoreDevice is deliberately left unsupported in this first step.
			if c, ok := d.s.(interface {
				ReadClipboard(context.Context, bool) (string, error)
			}); ok {
				if value, err := c.ReadClipboard(ctx, command); err == nil {
					text = value
				}
			}
			blocks[i] = text
		}()
	}
	wg.Wait()
	separator := "\n"
	if blankLine {
		separator = "\n\n"
	}
	return strings.Join(blocks, separator)
}
