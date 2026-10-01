//go:build !go1.27

package api

// leakedGoroutines returns 0 on toolchains without the goroutineleak profile.
// Reporting an honest zero beats guessing from the total goroutine count,
// which says nothing about whether those goroutines can still make progress.
func leakedGoroutines() int { return 0 }
