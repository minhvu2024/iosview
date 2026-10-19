package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// shot saves a PNG screenshot posted by the UI into ShotDir and answers with
// the file path. Body: raw PNG. Query: d = device id.
func (s *Server) shot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// Same-origin UI only: browsers always send Origin on cross-site POSTs.
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.ShotDir == "" {
		http.Error(w, "screenshots disabled", http.StatusNotFound)
		return
	}
	d := s.M.Get(r.URL.Query().Get("d"))
	if d == nil {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil || !bytes.HasPrefix(body, pngMagic) {
		http.Error(w, "need a PNG body", http.StatusBadRequest)
		return
	}
	snap := s.M.Snapshot()
	label := ""
	for _, x := range snap.Devices {
		if x.ID == d.ID {
			label = fmt.Sprintf("%02d", x.No)
			if n := x.Name; n != "" {
				label += "-" + n
			}
		}
	}
	name := safeFile(label) + "-" + time.Now().Format("20060102-150405.000") + ".png"
	if err := os.MkdirAll(s.ShotDir, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	path := filepath.Join(s.ShotDir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"path": path})
}

// safeFile keeps a name usable as a Windows file name.
func safeFile(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.Trim(s, " .")
	if s == "" {
		s = "iphone"
	}
	return s
}
