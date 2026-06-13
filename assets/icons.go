// Package assets embeds the system-tray state icons. The .ico files are
// produced by genicons.go (run: go run assets/genicons.go).
package assets

import _ "embed"

//go:embed icon_active.ico
var Active []byte

//go:embed icon_paused.ico
var Paused []byte

//go:embed icon_error.ico
var Error []byte
