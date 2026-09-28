//go:build !windows && !darwin

package audio

// On platforms with no cgo-free audio API, the WAV file sink is the only
// backend. It is what keeps `go build ./...` and `go test ./...` honest on the
// Linux third of the CI matrix.

func newSink() (sink, error) { return &fileSink{}, nil }
