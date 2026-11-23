//go:build windows && wails

package main

import (
	"io/fs"
	"log"
	"log/slog"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"

	"iosview/web"
)

// runWindow (Wails build, 0.11.0 PoC) shows the UI in a frameless Wails v3
// window and blocks until it is closed.
//
// The page is served from Wails' own origin (http://wails.localhost) by the
// agent's HTTP handler, so relative fetches (/api/...) stay same-origin and
// /wails/runtime.js (window dragging, min/max/close) is available. The
// WebSockets (control, H.264) do not go through the asset server: the UI
// connects straight to the agent's TCP port, passed in as ?ws=host:port.
func runWindow(o winOpts) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Wails lỗi: %v", r)
			ok = false
		}
	}()

	h := o.Handler
	if h == nil {
		// Second window on an agent that is already running: proxy to it.
		u, err := url.Parse(o.URL)
		if err != nil {
			return false
		}
		h = httputil.NewSingleHostReverseProxy(u)
	}
	wsHost := strings.TrimPrefix(o.URL, "http://")

	// GPU flags for the browser process (same as the WebView2 build): raster on
	// the GPU and share the buffers with the compositor. Override with the
	// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS variable (" " to disable).
	args := []string{"--enable-gpu-rasterization", "--enable-zero-copy"}
	if v := os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"); v != "" {
		args = strings.Fields(v)
	}
	data := filepath.Join(os.Getenv("LocalAppData"), "iOSView", "webview2")
	icon, _ := fs.ReadFile(web.Dist, "dist/icon.png")

	app := application.New(application.Options{
		Name:        "iOSView",
		Description: "Xem và điều khiển nhiều iPhone cùng lúc",
		Icon:        icon,
		Assets:      application.AssetOptions{Handler: h, DisableLogging: true},
		Windows: application.WindowsOptions{
			WebviewUserDataPath:   data,
			AdditionalBrowserArgs: args,
			// moved to a screen with another scale: reload the icons at the
			// sizes Windows wants there (never consumes the message)
			WndProcInterceptor: func(hwnd uintptr, msg uint32, _, _ uintptr) (uintptr, bool) {
				if msg == wmDpiChanged {
					setWindowIcons(hwnd)
				}
				return 0, false
			},
		},
		Logger: slog.New(slog.NewTextHandler(log.Writer(), &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	// Mica needs Windows 11 22H2+; older Windows gets a solid canvas (the UI
	// paints the same light-grey background itself, so it looks the same
	// minus the translucency).
	mica := o.Mica && w32.IsWindowsVersionAtLeast(10, 0, 22621)
	wo := application.WebviewWindowOptions{
		Title:            "iOSView " + o.Version,
		URL:              "/?ws=" + url.QueryEscape(wsHost),
		Width:            1560,
		Height:           940,
		MinWidth:         1100,
		MinHeight:        640,
		InitialPosition:  application.WindowCentered,
		Frameless:        true,
		BackgroundType:   application.BackgroundTypeSolid,
		BackgroundColour: application.NewRGB(0xE9, 0xEE, 0xEB), // --canvas
		Windows: application.WindowsWindow{
			BackdropType: application.Mica,
			Theme:        application.Light,
		},
	}
	if mica {
		wo.BackgroundType = application.BackgroundTypeTranslucent
		wo.URL += "&mica=1"
	}
	win := app.Window.NewWithOptions(wo)
	// Same icon as the exe (taskbar, Alt+Tab, Explorer) — see setWindowIcons.
	for _, ev := range []events.WindowEventType{events.Common.WindowShow, events.Common.WindowRuntimeReady} {
		win.OnWindowEvent(ev, func(*application.WindowEvent) { setWindowIcons(uintptr(win.NativeWindow())) })
	}
	log.Printf("Wails window: mica=%v args=%q", mica, args)
	if err := app.Run(); err != nil {
		log.Printf("Wails: %v", err)
		return false
	}
	return true
}

// Window icons. Wails looks for the app icon at resource ID 3, but our .syso
// (assets/iosview.ico) has it at ID 1, so without this the taskbar showed the
// 256 px PNG scaled down and the small icon was Windows' generic one. Load
// the .ico frames Windows wants at this window's DPI — the .ico has 16/20/24/
// 32 px hand-drawn frames and 40+ px frames, so taskbar, title bar and
// Explorer all show the same artwork for the same size.
const (
	wmSetIcon    = 0x0080
	wmDpiChanged = 0x02E0
	iconSmall    = 0
	iconBig      = 1
	smCxIcon     = 11
	smCxSmIcon   = 49
	imageIcon    = 1
	appIconID    = 1 // RT_GROUP_ICON in rsrc_windows_amd64.syso
)

var (
	u32                  = windows.NewLazySystemDLL("user32.dll")
	procLoadImage        = u32.NewProc("LoadImageW")
	procSendMessage      = u32.NewProc("SendMessageW")
	procGetDpiForWindow  = u32.NewProc("GetDpiForWindow")
	procMetricsForDpi    = u32.NewProc("GetSystemMetricsForDpi")
	procGetSystemMetrics = u32.NewProc("GetSystemMetrics")
)

func setWindowIcons(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dpi := uintptr(96)
	if procGetDpiForWindow.Find() == nil {
		if d, _, _ := procGetDpiForWindow.Call(hwnd); d >= 96 {
			dpi = d
		}
	}
	metric := func(i uintptr) uintptr {
		if procMetricsForDpi.Find() == nil {
			if v, _, _ := procMetricsForDpi.Call(i, dpi); v > 0 {
				return v
			}
		}
		v, _, _ := procGetSystemMetrics.Call(i)
		return v * dpi / 96
	}
	var inst windows.Handle
	if windows.GetModuleHandleEx(0, nil, &inst) != nil {
		return
	}
	for _, ic := range []struct{ which, size uintptr }{{iconBig, metric(smCxIcon)}, {iconSmall, metric(smCxSmIcon)}} {
		h, _, _ := procLoadImage.Call(uintptr(inst), appIconID, imageIcon, ic.size, ic.size, 0)
		if h != 0 {
			procSendMessage.Call(hwnd, wmSetIcon, ic.which, h)
		}
	}
}
