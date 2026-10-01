//go:build !yuisecret

package crypto

// Scrubbed runs fn normally.
//
// The default build has no dependency on runtime/secret, so a fresh checkout
// compiles on any Go 1.27 toolchain without GOEXPERIMENT flags. Key material
// is still zeroed explicitly (see zero); what is missing is the runtime's
// erasure of stack and register copies.
func Scrubbed(fn func()) { fn() }
