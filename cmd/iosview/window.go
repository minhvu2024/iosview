package main

import "net/http"

// winOpts describes the UI window to open (see runWindow in the per-platform
// files).
type winOpts struct {
	URL     string       // where the Go agent serves the UI, e.g. http://127.0.0.1:7788
	Version string       // shown in the title
	Handler http.Handler // the agent's HTTP handler; nil when another instance owns the port
	Mica    bool         // translucent Mica background (Windows 11 only; ignored elsewhere)
}
