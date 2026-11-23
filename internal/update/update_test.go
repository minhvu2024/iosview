package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// server publishes exe bytes; hash lets a test publish a wrong checksum.
func server(t *testing.T, exe []byte, hash string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/pc/latest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"version":"9.9.9","sha256":"%s","size":%d,"url":"iosview.exe"}`, hash, len(exe))
	})
	mux.HandleFunc("/pc/iosview.exe", func(w http.ResponseWriter, r *http.Request) { w.Write(exe) })
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func setup(t *testing.T, running []byte, srv *httptest.Server) (*Updater, string, chan string) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "iosview.exe")
	if err := os.WriteFile(exe, running, 0o755); err != nil {
		t.Fatal(err)
	}
	u := New(exe, "0.10.12")
	u.Manifest = srv.URL + "/pc/latest.json"
	restarted := make(chan string, 1)
	u.Restart = func(p string) error { restarted <- p; return nil }
	return u, exe, restarted
}

func TestSameBuildIsNotAnUpdate(t *testing.T) {
	cur := []byte("build A")
	u, _, _ := setup(t, cur, server(t, cur, sum(cur)))
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := u.Status(); st.Available || st.Latest != "9.9.9" {
		t.Fatalf("status %+v", st)
	}
	if u.Start() == nil {
		t.Fatal("Start must refuse when there is nothing new")
	}
}

func TestOlderPublishedBuildIsNotAnUpdate(t *testing.T) {
	cur, old := []byte("build B — newer"), []byte("build A")
	u, _, _ := setup(t, cur, server(t, old, sum(old)))
	u.Version = "10.0.0" // running build is newer than the published 9.9.9
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := u.Status(); st.Available || st.Latest != "9.9.9" {
		t.Fatalf("a downgrade was offered: %+v", st)
	}
	if u.Start() == nil {
		t.Fatal("Start must refuse a downgrade")
	}
}

func TestCmpVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"0.10.15", "0.10.13", 1}, {"0.10.13", "0.10.15", -1}, {"0.10.15", "0.10.15", 0}, {"0.9.9", "0.10.0", -1}, {"1.0", "1.0.0", 0}, {"dev", "0.10.1", -1}} {
		if got := cmpVersion(c.a, c.b); got != c.want {
			t.Errorf("cmpVersion(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestUpdateSwapsAndRestarts(t *testing.T) {
	cur, next := []byte("build A"), []byte("build B — newer")
	u, exe, restarted := setup(t, cur, server(t, next, sum(next)))
	if err := u.Check(context.Background()); err != nil || !u.Status().Available {
		t.Fatalf("check: %v %+v", err, u.Status())
	}
	if err := u.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-restarted:
		if p != exe {
			t.Fatalf("restarted %q", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no restart, status %+v", u.Status())
	}
	if b, _ := os.ReadFile(exe); string(b) != string(next) {
		t.Fatalf("exe not replaced: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != string(cur) {
		t.Fatalf("old exe not kept aside: %q", b)
	}
	cleanupOld(exe)
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal(".old not cleaned up")
	}
}

func TestBadDownloadKeepsCurrentExe(t *testing.T) {
	cur, next := []byte("build A"), []byte("build B")
	// Manifest promises a different file than the one served.
	u, exe, restarted := setup(t, cur, server(t, next, sum([]byte("something else"))))
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := u.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for u.Status().Downloading || u.Status().Error == "" {
		if time.Now().After(deadline) {
			t.Fatalf("no error, status %+v", u.Status())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(restarted) != 0 {
		t.Fatal("must not restart after a bad download")
	}
	if b, _ := os.ReadFile(exe); string(b) != string(cur) {
		t.Fatalf("current exe changed: %q", b)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatal(".new left behind")
	}
}

func TestOfflineIsAnErrorNotAPanic(t *testing.T) {
	srv := server(t, nil, "")
	u, _, _ := setup(t, []byte("x"), srv)
	srv.Close()
	if err := u.Check(context.Background()); err == nil || u.Status().Available {
		t.Fatalf("want error, got %v %+v", err, u.Status())
	}
}
