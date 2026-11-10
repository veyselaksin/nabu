module github.com/veyselaksin/nabu/tts

go 1.24.0

require (
	github.com/veyselaksin/nabu/g2p v0.0.0-00010101000000-000000000000
	github.com/yalue/onnxruntime_go v1.38.0
	golang.org/x/text v0.34.0
)

require go.yaml.in/yaml/v3 v3.0.6 // indirect

// Resolved by go.work during development; the replace keeps GOWORK=off builds
// and `go mod tidy` working too.
replace github.com/veyselaksin/nabu/g2p => ../g2p
