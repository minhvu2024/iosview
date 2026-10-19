// iosview — view and control many jailbroken iPhones (TrollVNC) at once,
// with synchronised input across a group of phones.
//
// On Windows it is built as a GUI app (no console window): the UI runs in its
// own native window (WebView2, the engine built into Windows 10/11) and the
// program exits when that window is closed. Without WebView2 it falls back to
// an Edge/Chrome app window. Logs go to iosview.log next to the executable.
package main

import (
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"iosview/internal/device"
	"iosview/internal/server"
	"iosview/web"
)

var version = "dev"

func main() {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	cfgPath := flag.String("config", filepath.Join(dir, "iosview.json"), "đường dẫn file cấu hình")
	listen := flag.String("listen", "", "địa chỉ web UI (mặc định 127.0.0.1:7788)")
	noBrowser := flag.Bool("no-browser", false, "không tự mở cửa sổ giao diện")
	keepAlive := flag.Bool("keep-alive", false, "không tự thoát khi đóng cửa sổ giao diện")
	flag.Parse()

	if lf, err := os.OpenFile(filepath.Join(dir, "iosview.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(lf)
		defer lf.Close()
	}
	log.Printf("iOSView %s starting", version)

	cfg, err := device.LoadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("đọc cấu hình: %v", err)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:7788"
	}
	url := "http://" + cfg.Listen

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		// Already running? Just bring up another window on it.
		if c, e := http.Get(url + "/api/state"); e == nil {
			c.Body.Close()
			if !runWindow(url, version) {
				openBrowser(url)
			}
			return
		}
		log.Fatalf("không mở được %s: %v", cfg.Listen, err)
	}

	m := device.NewManager(cfg)
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		log.Fatal(err)
	}
	srv := &server.Server{M: m, Web: dist, ShotDir: filepath.Join(dir, "screenshots"), Version: version}
	if *noBrowser {
		log.Fatal(http.Serve(ln, srv.Handler()))
	}
	go func() { log.Fatal(http.Serve(ln, srv.Handler())) }()

	// Native window first: it blocks until the user closes it.
	if runWindow(url, version) {
		if *keepAlive {
			select {}
		}
		log.Printf("window closed — exiting")
		return
	}
	// No WebView2 runtime: browser app window, exit when it disconnects.
	time.Sleep(200 * time.Millisecond)
	openBrowser(url)
	if *keepAlive {
		select {}
	}
	exitWhenWindowClosed(m)
}

// exitWhenWindowClosed stops the program once no UI window has been connected
// for a few seconds (a page reload reconnects well within that time).
func exitWhenWindowClosed(m *device.Manager) {
	seen := false
	idleSince := time.Now()
	for range time.Tick(time.Second) {
		if m.Clients() > 0 {
			seen = true
			idleSince = time.Now()
			continue
		}
		idle := time.Since(idleSince)
		if (seen && idle > 8*time.Second) || (!seen && idle > 3*time.Minute) {
			log.Printf("UI closed — exiting")
			os.Exit(0)
		}
	}
}

// openBrowser opens the UI as a standalone app window (Edge/Chrome --app with
// its own profile so it gets its own taskbar entry and icon).
func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		profile := filepath.Join(os.Getenv("LocalAppData"), "iOSView", "browser")
		for _, p := range []string{
			os.Getenv("ProgramFiles(x86)") + `\Microsoft\Edge\Application\msedge.exe`,
			os.Getenv("ProgramFiles") + `\Microsoft\Edge\Application\msedge.exe`,
			os.Getenv("ProgramFiles") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("LocalAppData") + `\Google\Chrome\Application\chrome.exe`,
		} {
			if _, err := os.Stat(p); err == nil {
				cmd := exec.Command(p, "--app="+url, "--user-data-dir="+profile,
					"--no-first-run", "--no-default-browser-check", "--window-size=1500,950")
				if cmd.Start() == nil {
					return
				}
			}
		}
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	default:
		_ = exec.Command("xdg-open", url).Start()
	}
}
