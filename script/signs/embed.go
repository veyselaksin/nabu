// Package signs embeds the sign lists and transliteration conventions shipped
// with Nabu, so that binaries (the API, a WASM build) work without data files
// on disk. See README.md in this directory for provenance and licenses.
package signs

import "embed"

// FS holds every *.tsv sign list and *.yaml conventions file in this directory.
//
//go:embed *.tsv *.yaml
var FS embed.FS

// Files embedded for Middle Egyptian.
const (
	EgyptianSignList    = "egy-unikemet-uniliterals.tsv"
	EgyptianConventions = "egy-conventions.yaml"
)
