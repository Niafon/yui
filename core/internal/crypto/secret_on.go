//go:build yuisecret

package crypto

import "runtime/secret"

// Scrubbed runs fn in secret mode: the runtime erases stack temporaries that
// held key material, including inside goroutines started by fn (Go 1.27).
//
// Enabled with `GOEXPERIMENT=runtimesecret go build -tags yuisecret`.
// Runtime erasure is currently supported on Linux amd64/arm64 only.
// It is opt-in because the package
// arrived as an experiment in Go 1.26 and its availability depends on the
// toolchain; a checkout must build without it (ADR-032).
func Scrubbed(fn func()) { secret.Do(fn) }
