//go:build go1.27

package api

import "runtime/pprof"

// leakedGoroutines counts goroutines the runtime can prove will never wake up:
// blocked on a channel, mutex or condition variable that is unreachable from
// anything runnable (Go 1.27, generally available).
//
// It is a lower bound. Leaks reachable through package-level variables are
// invisible to this technique, so a zero here is not proof of health — but a
// number that climbs is proof of a bug.
func leakedGoroutines() int {
	p := pprof.Lookup("goroutineleak")
	if p == nil {
		return 0
	}
	return p.Count()
}
