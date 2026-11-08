// Package rules embeds the rule files shipped with Nabu so that binaries (the
// API, a future WASM build) work without a rules directory on disk.
package rules

import "embed"

// FS holds every *.yaml rule file in this directory.
//
//go:embed *.yaml
var FS embed.FS
