package web

import "embed"

// FS holds the compiled React SPA assets (populated by `make web-build`).
//
//go:embed all:dist
var FS embed.FS
