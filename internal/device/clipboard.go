package device

import (
	"context"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// Tunables (vars so unit tests can run fast).
var (
	copyReadTimeout = 800 * time.Millisecond

	clipMaxWindow = 1200 * time.Millisecond
	soloClipGrace = 3 * time.Second
)

type clipMember struct {
	id     string
	no     int
	online bool
}

type clipCollector struct {
	seq       uint64
	devices   []clipMember
	texts     map[string]string
	blankLine bool

	maxTimer *time.Timer
	done     bool
}

// SetCopyBlank updates whether multi-phone clipboard copies separate entries with
// a blank line ("\n\n") instead of a single newline ("\n").
func (m *Manager) SetCopyBlank(on bool) {
	m.mu.Lock()
	m.copyBlank = on
	m.mu.Unlock()
}

// writeClipboard serializes Win32 writes and discards completions older than
// the last successful copy.
func (m *Manager) writeClipboard(copy ClipboardCopy) error {
	m.clipWriteMu.Lock()
	defer m.clipWriteMu.Unlock()
	if copy.Seq <= m.clipWritten || m.writeClip == nil {
		return nil
	}
	if err := m.writeClip(copy.Text); err != nil {
		return err
	}
	m.clipWritten = copy.Seq
	m.mu.Lock()
	for ch := range m.clipCopySubs {
		select {
		case <-ch:
		default:
		}
		ch <- copy
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) cancelClipBatchLocked() {
	if b := m.clipBatch; b != nil {
		b.done = true
		if b.maxTimer != nil {
			b.maxTimer.Stop()
		}
		m.clipBatch = nil
	}
}

func (m *Manager) flushClipBatch(b *clipCollector) {
	m.mu.Lock()
	if b == nil || b.done || m.clipBatch != b {
		m.mu.Unlock()
		return
	}
	b.done = true
	if b.maxTimer != nil {
		b.maxTimer.Stop()
	}
	m.clipBatch = nil
	seq := b.seq
	sep := "\n"
	if b.blankLine {
		sep = "\n\n"
	}
	parts := make([]ClipboardPart, len(b.devices))
	blocks := make([]string, len(parts))
	for i, d := range b.devices {
		text, ok := b.texts[d.id]
		parts[i] = ClipboardPart{ID: d.id, Text: text, Read: ok}
		blocks[i] = text
	}
	m.mu.Unlock()

	copy := ClipboardCopy{Seq: seq, Text: strings.Join(blocks, sep), Parts: parts}
	if len(b.texts) == 0 {
		return
	}
	go func() {
		if err := m.writeClipboard(copy); err != nil {
			log.Printf("ghi clipboard lỗi: %v", err)
		}
	}()
}

// onPhoneClip routes a clipboard update pushed by a phone to the PC clipboard.
// Solo copies write immediately; group copies wait for all online members
// or the bounded collection window.
func (m *Manager) onPhoneClip(id, text string) {
	m.mu.Lock()
	if m.writeClip == nil {
		m.mu.Unlock()
		return
	}
	d := m.devices[id]
	solo := !m.syncOn ||
		(id == m.lastSoloID && time.Since(m.lastSoloAt) <= soloClipGrace) ||
		d == nil || !d.InSync

	var group []clipMember
	onlineCount := 0
	if !solo {
		for _, dev := range m.devices {
			if !dev.InSync {
				continue
			}
			on := dev.Status == "online"
			if on {
				onlineCount++
			}
			group = append(group, clipMember{id: dev.ID, no: dev.No, online: on})
		}
		if onlineCount <= 1 {
			solo = true
		}
	}

	if solo {
		m.clipSequence++
		seq := m.clipSequence
		m.mu.Unlock()
		go func() {
			if err := m.writeClipboard(ClipboardCopy{Seq: seq, Text: text}); err != nil {
				log.Printf("ghi clipboard lỗi: %v", err)
			}
		}()
		return
	}

	b := m.clipBatch
	if b != nil {
		if _, already := b.texts[id]; already {
			m.cancelClipBatchLocked()
			b = nil
		}
	}
	if b == nil {
		sort.Slice(group, func(i, j int) bool {
			if group[i].no != group[j].no {
				return group[i].no < group[j].no
			}
			return group[i].id < group[j].id
		})
		m.clipSequence++
		b = &clipCollector{
			seq:       m.clipSequence,
			devices:   group,
			texts:     make(map[string]string, len(group)),
			blankLine: m.copyBlank,
		}
		m.clipBatch = b
		b.maxTimer = time.AfterFunc(clipMaxWindow, func() { m.flushClipBatch(b) })
	}
	b.texts[id] = text

	allOnlineReady := true
	for _, mem := range b.devices {
		if mem.online {
			if _, ok := b.texts[mem.id]; !ok {
				allOnlineReady = false
				break
			}
		}
	}
	if allOnlineReady {
		m.mu.Unlock()
		m.flushClipBatch(b)
		return
	}
	m.mu.Unlock()
}

type ClipboardPart struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Read bool   `json:"read"`
}

type ClipboardCopy struct {
	Seq   uint64          `json:"seq"`
	Text  string          `json:"text"`
	Parts []ClipboardPart `json:"parts"`
}

// SubscribeClipboardCopies delivers the latest committed PC clipboard copy.
func (m *Manager) SubscribeClipboardCopies() (<-chan ClipboardCopy, func()) {
	ch := make(chan ClipboardCopy, 1)
	m.mu.Lock()
	if m.clipCopySubs == nil {
		m.clipCopySubs = make(map[chan ClipboardCopy]struct{})
	}
	m.clipCopySubs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.clipCopySubs, ch)
		m.mu.Unlock()
	}
}

// CopyClipboards snapshots selection before reading phones in parallel.
func (m *Manager) CopyClipboards(ctx context.Context, command bool, blankLine bool) (ClipboardCopy, error) {
	type target struct {
		no int
		id string
		s  session
	}
	m.mu.Lock()
	m.cancelClipBatchLocked()
	m.clipSequence++
	seq := m.clipSequence
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
	parts := make([]ClipboardPart, len(list))
	var wg sync.WaitGroup
	for i, d := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rctx, cancel := context.WithTimeout(ctx, copyReadTimeout)
			defer cancel()
			part := ClipboardPart{ID: d.id}
			// CoreDevice is deliberately left unsupported in this first step.
			if c, ok := d.s.(interface {
				ReadClipboard(context.Context, bool) (string, error)
			}); ok {
				if value, err := c.ReadClipboard(rctx, command); err == nil {
					part.Text, part.Read = value, true
				}
			}
			parts[i] = part
			blocks[i] = part.Text
		}()
	}
	wg.Wait()
	separator := "\n"
	if blankLine {
		separator = "\n\n"
	}
	for _, part := range parts {
		if part.Read {
			out := strings.Join(blocks, separator)
			copy := ClipboardCopy{Seq: seq, Text: out, Parts: parts}
			return copy, m.writeClipboard(copy)
		}
	}
	return ClipboardCopy{}, errors.New("Không đọc được clipboard từ iPhone")
}
