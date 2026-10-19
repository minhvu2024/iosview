//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

// The window must be created and pumped on the process's main thread.
func init() { runtime.LockOSThread() }

// runWindow shows the UI in a native window backed by WebView2 and blocks until
// it is closed. It returns false (without blocking) when WebView2 is missing.
func runWindow(url, version string) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("WebView2 lỗi: %v", r)
			ok = false
		}
	}()
	w0, h0 := windowSize(1560, 940)
	data := filepath.Join(os.Getenv("LocalAppData"), "iOSView", "webview2")
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  data,
		WindowOptions: webview2.WindowOptions{
			Title:  "iOSView " + version,
			Width:  w0,
			Height: h0,
			IconId: 1, // RT_GROUP_ICON in rsrc_windows_amd64.syso
			Center: true,
		},
	})
	if w == nil {
		log.Printf("không tạo được cửa sổ WebView2 — dùng Edge/Chrome")
		return false
	}
	defer w.Destroy()
	mw, mh := windowSize(1100, 640)
	w.SetSize(int(mw), int(mh), webview2.HintMin)
	w.Navigate(url)
	w.Run()
	return true
}

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	shcore = windows.NewLazySystemDLL("shcore.dll")
)

// Sharp text on 125 % / 150 % displays: declare the process DPI-aware before
// any window exists (otherwise Windows stretches the window as a bitmap).
func init() {
	const perMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	if p := user32.NewProc("SetProcessDpiAwarenessContext"); p.Find() == nil {
		if r, _, _ := p.Call(perMonitorV2); r != 0 {
			return
		}
	}
	if p := shcore.NewProc("SetProcessDpiAwareness"); p.Find() == nil {
		if r, _, _ := p.Call(2); r == 0 { // PROCESS_PER_MONITOR_DPI_AWARE
			return
		}
	}
	if p := user32.NewProc("SetProcessDPIAware"); p.Find() == nil {
		p.Call()
	}
}

// windowSize scales a size in 96-dpi pixels to the screen's DPI and keeps it
// inside the screen.
func windowSize(w, h uint) (uint, uint) {
	dpi := uintptr(96)
	if p := user32.NewProc("GetDpiForSystem"); p.Find() == nil {
		if r, _, _ := p.Call(); r >= 96 {
			dpi = r
		}
	}
	w, h = w*uint(dpi)/96, h*uint(dpi)/96
	gsm := user32.NewProc("GetSystemMetrics")
	if gsm.Find() == nil {
		sw, _, _ := gsm.Call(16) // SM_CXFULLSCREEN (work area width)
		sh, _, _ := gsm.Call(17) // SM_CYFULLSCREEN
		if sw > 0 && w > uint(sw)*96/100 {
			w = uint(sw) * 96 / 100
		}
		if sh > 0 && h > uint(sh)*96/100 {
			h = uint(sh) * 96 / 100
		}
	}
	return w, h
}
