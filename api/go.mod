module github.com/veyselaksin/nabu/api

go 1.24.0

require (
	github.com/go-chi/chi/v5 v5.5.0
	github.com/veyselaksin/nabu/g2p v0.0.0
)

require (
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	golang.org/x/text v0.34.0 // indirect
)

// Resolved by go.work during development; the replace keeps GOWORK=off builds
// and `go mod tidy` working too.
replace github.com/veyselaksin/nabu/g2p => ../g2p
