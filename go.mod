module iosview

go 1.26.0

require (
	github.com/coder/websocket v1.8.15
	github.com/jchv/go-webview2 v0.0.0-00010101000000-000000000000
	github.com/wailsapp/wails/v3 v3.0.0
	golang.org/x/sys v0.48.0
)

require (
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
)

replace (
	github.com/jchv/go-webview2 => ./third_party/go-webview2
	github.com/jchv/go-winloader => ./third_party/go-winloader
	golang.org/x/sys => ./third_party/sys
)
