// Package update keeps an internal build current: it compares the running
// exe with the one published on Cloudflare Pages (by SHA-256, so no version
// bookkeeping is needed) and, when the user asks, downloads the new exe,
// swaps it in place and restarts.
//
// Published files (see iphone/deploy-repo.bat):
//
//	<base>/latest.json  {"version":"0.10.12","sha256":"…","size":123,"url":"iosview.exe"}
//	<base>/iosview.exe
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultManifest is where internal builds are published.
const DefaultManifest = "https://iosview.pages.dev/pc/latest.json"

// CheckEvery is how often a running program looks for a new build.
const CheckEvery = 30 * time.Minute

// Manifest describes the published build.
type Manifest struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	URL     string `json:"url"` // relative to the manifest, or absolute
}

// Status is what the UI shows.
type Status struct {
	Current     string `json:"current"`
	Latest      string `json:"latest,omitempty"`
	Available   bool   `json:"available"`
	Downloading bool   `json:"downloading"`
	Progress    int    `json:"progress"` // 0..100 while downloading
	Error       string `json:"error,omitempty"`
	CheckedAt   int64  `json:"checkedAt,omitempty"` // unix ms of the last successful check
}

// Updater checks for and applies updates. Safe for concurrent use.
type Updater struct {
	Manifest string // manifest URL
	Exe      string // path of the running exe
	Version  string // version of the running build
	Client   *http.Client
	// Restart runs the new exe and ends this process (replaced in tests).
	Restart func(exe string) error

	mu       sync.Mutex
	selfHash string
	latest   *Manifest
	st       Status
}

// New returns an updater for the running exe.
func New(exe, version string) *Updater {
	u := &Updater{
		Manifest: DefaultManifest, Exe: exe, Version: version,
		Client:  &http.Client{Timeout: 2 * time.Minute},
		Restart: restart,
	}
	if v := os.Getenv("IOSVIEW_UPDATE_URL"); v != "" { // testing / another host
		u.Manifest = v
	}
	u.st.Current = version
	return u
}

// Run checks shortly after start and then every CheckEvery.
func (u *Updater) Run(ctx context.Context) {
	cleanupOld(u.Exe)
	t := time.NewTimer(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := u.Check(ctx); err != nil {
				log.Printf("kiểm tra cập nhật: %v", err)
			}
			t.Reset(CheckEvery)
		}
	}
}

// Status returns the current state.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.st
}

func (u *Updater) setErr(err error) {
	u.mu.Lock()
	u.st.Error = ""
	if err != nil {
		u.st.Error = err.Error()
	}
	u.mu.Unlock()
}

// Check reads the manifest and compares it with the running exe.
func (u *Updater) Check(ctx context.Context) error {
	err := u.check(ctx)
	u.setErr(err)
	return err
}

func (u *Updater) check(ctx context.Context) error {
	self, err := u.hash()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.Manifest, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Cache-Control", "no-cache")
	res, err := u.Client.Do(req)
	if err != nil {
		return errors.New("không kết nối được máy chủ cập nhật")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("máy chủ cập nhật trả về %d", res.StatusCode)
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&m); err != nil {
		return errors.New("latest.json không hợp lệ")
	}
	m.SHA256 = strings.ToLower(strings.TrimSpace(m.SHA256))
	if len(m.SHA256) != 64 || m.URL == "" {
		return errors.New("latest.json thiếu sha256 / url")
	}
	u.mu.Lock()
	u.latest = &m
	u.st.Latest = m.Version
	// A different build is an update only if it is not older than this one:
	// a PC running a build newer than the published one (e.g. the developer's
	// machine, or the repo not yet redeployed) must not be offered a downgrade.
	// Same version with a different hash (rebuild) still counts.
	u.st.Available = m.SHA256 != self && cmpVersion(m.Version, u.Version) >= 0
	u.st.CheckedAt = time.Now().UnixMilli()
	u.mu.Unlock()
	return nil
}

// cmpVersion compares dotted numeric versions ("0.10.15"): -1, 0 or 1.
// Non-numeric parts (e.g. "dev") count as 0, so an unknown version never
// blocks an update.
func cmpVersion(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(strings.TrimSpace(pa[i]))
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(strings.TrimSpace(pb[i]))
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// hash is the SHA-256 of the running exe (computed once).
func (u *Updater) hash() (string, error) {
	u.mu.Lock()
	h := u.selfHash
	u.mu.Unlock()
	if h != "" {
		return h, nil
	}
	h, err := fileHash(u.Exe)
	if err != nil {
		return "", fmt.Errorf("không đọc được file chương trình: %w", err)
	}
	u.mu.Lock()
	u.selfHash = h
	u.mu.Unlock()
	return h, nil
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	s := sha256.New()
	if _, err := io.Copy(s, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(s.Sum(nil)), nil
}

// Start downloads the new build in the background; when it is complete and
// verified, the exe is swapped and the program restarts. Returns an error if
// there is nothing to install or a download is already running.
func (u *Updater) Start() error {
	u.mu.Lock()
	m := u.latest
	switch {
	case u.st.Downloading:
		u.mu.Unlock()
		return errors.New("đang tải bản cập nhật")
	case m == nil || !u.st.Available:
		u.mu.Unlock()
		return errors.New("không có bản cập nhật")
	}
	u.st.Downloading, u.st.Progress, u.st.Error = true, 0, ""
	u.mu.Unlock()
	go func() {
		err := u.install(m)
		u.mu.Lock()
		u.st.Downloading = false
		u.mu.Unlock()
		if err != nil {
			log.Printf("cập nhật lỗi: %v", err)
			u.setErr(err)
		}
	}()
	return nil
}

func (u *Updater) install(m *Manifest) error {
	src, err := url.Parse(u.Manifest)
	if err != nil {
		return err
	}
	ref, err := url.Parse(m.URL)
	if err != nil {
		return err
	}
	newPath := u.Exe + ".new"
	if err := u.download(src.ResolveReference(ref).String(), newPath, m); err != nil {
		os.Remove(newPath)
		return err
	}
	if err := swap(u.Exe, newPath); err != nil {
		os.Remove(newPath)
		return err
	}
	log.Printf("đã cập nhật lên %s — khởi động lại", m.Version)
	return u.Restart(u.Exe)
}

func (u *Updater) download(src, dst string, m *Manifest) error {
	res, err := u.Client.Get(src)
	if err != nil {
		return errors.New("tải bản cập nhật thất bại (mạng)")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("tải bản cập nhật thất bại (%d)", res.StatusCode)
	}
	total := m.Size
	if total <= 0 {
		total = res.ContentLength
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return errors.New("không ghi được file vào thư mục chương trình")
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var n int64
	for {
		k, rerr := res.Body.Read(buf)
		if k > 0 {
			if _, werr := f.Write(buf[:k]); werr != nil {
				f.Close()
				return errors.New("không ghi được file vào thư mục chương trình")
			}
			h.Write(buf[:k])
			n += int64(k)
			if total > 0 {
				u.mu.Lock()
				u.st.Progress = int(min(99, n*100/total))
				u.mu.Unlock()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return errors.New("tải bản cập nhật bị ngắt")
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != m.SHA256 || (m.Size > 0 && n != m.Size) {
		return errors.New("file tải về bị lỗi (sai mã kiểm tra) — thử lại")
	}
	u.mu.Lock()
	u.st.Progress = 100
	u.mu.Unlock()
	return nil
}

// swap replaces exe with newPath. Windows lets a running exe be renamed (not
// deleted), so the old one moves aside to exe.old and is removed on the next
// start. If anything fails the original is put back.
func swap(exe, newPath string) error {
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return errors.New("không thay được file chương trình (đang bị khoá?)")
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(old, exe)
		return errors.New("không thay được file chương trình")
	}
	return nil
}

// cleanupOld removes the exe left by the previous update (it may still be
// running for a moment right after the restart, so retry briefly).
func cleanupOld(exe string) {
	old := exe + ".old"
	if _, err := os.Stat(old); err != nil {
		return
	}
	go func() {
		for i := 0; i < 20; i++ {
			if os.Remove(old) == nil || os.IsNotExist(errOf(old)) {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

func errOf(p string) error { _, err := os.Stat(p); return err }

// AfterUpdateFlag tells the new process it was started by an update, so it
// waits for the old one to release the UI port instead of exiting.
const AfterUpdateFlag = "-after-update"

func restart(exe string) error {
	args := []string{AfterUpdateFlag}
	for _, a := range os.Args[1:] {
		if a != AfterUpdateFlag {
			args = append(args, a)
		}
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("không mở lại được chương trình: %w", err)
	}
	os.Exit(0)
	return nil
}
