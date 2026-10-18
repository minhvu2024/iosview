// Package web embeds the built UI (web/dist) into the binary.
package web

import "embed"

//go:embed dist
var Dist embed.FS
