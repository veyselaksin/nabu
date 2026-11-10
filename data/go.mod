module github.com/veyselaksin/nabu/data

go 1.24.0

require (
	github.com/veyselaksin/nabu/script v0.0.0
	golang.org/x/text v0.34.0
)

require go.yaml.in/yaml/v3 v3.0.6 // indirect

// Resolved by go.work during development; the replace keeps GOWORK=off builds
// and `go mod tidy` working too.
replace github.com/veyselaksin/nabu/script => ../script
