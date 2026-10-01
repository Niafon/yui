package api

import (
	"context"
	"net/http"
	"net/http/pprof"
	"runtime"

	"github.com/yui-companion/core/internal/logging"
)

// Diagnostics for a process that is expected to run for weeks (NFR-002).
//
// The failure mode of a long-lived companion is not a crash — it is a slow
// leak: a websocket goroutine that never returns because its session closed
// while it was blocked on a channel. Go 1.27 made the `goroutineleak` profile
// generally available, and it detects exactly that class: a goroutine blocked
// on a primitive that nothing reachable can ever signal.
//
// The whole surface is bound to loopback. Diagnostics of a system holding
// someone's private conversations are themselves sensitive: goroutine dumps
// contain function names and stack shapes that describe the owner's activity.

// leakThreshold is the number of leaked goroutines that turns the periodic
// check from a debug line into a warning.
const leakThreshold = 25

func (s *Server) routesDebug() {
	if !s.deps.Cfg.Diagnostics.Enabled {
		return
	}
	loopbackOnly := func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isLoopback(r.RemoteAddr) {
				// Not 403: a remote caller should not learn the endpoint exists.
				http.NotFound(w, r)
				return
			}
			h(w, r)
		})
	}
	s.mux.Handle("GET /debug/pprof/", loopbackOnly(pprof.Index))
	s.mux.Handle("GET /debug/pprof/profile", loopbackOnly(pprof.Profile))
	s.mux.Handle("GET /debug/pprof/trace", loopbackOnly(pprof.Trace))
	s.mux.Handle("GET /debug/runtime", loopbackOnly(s.handleRuntimeStats))
}

// handleRuntimeStats reports what actually matters for a resident process:
// goroutine count, leaked goroutines, heap size and GC pressure.
func (s *Server) handleRuntimeStats(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	writeJSON(w, http.StatusOK, map[string]any{
		"goroutines":      runtime.NumGoroutine(),
		"leaked":          leakedGoroutines(),
		"heap_mb":         mem.HeapAlloc / (1 << 20),
		"gc_cycles":       mem.NumGC,
		"gc_pause_avg_us": gcPauseAverageMicros(&mem),
		"dropped_events":  s.deps.Bus.Dropped(),
		"go_version":      runtime.Version(),
	})
}

func gcPauseAverageMicros(mem *runtime.MemStats) uint64 {
	if mem.NumGC == 0 {
		return 0
	}
	return mem.PauseTotalNs / uint64(mem.NumGC) / 1000
}

// WatchGoroutineLeaks logs when leaked goroutines accumulate. It runs from the
// scheduler like any other background job, so it yields to conversation.
func WatchGoroutineLeaks(ctx context.Context) error {
	n := leakedGoroutines()
	log := logging.From(ctx)
	if n >= leakThreshold {
		// Actionable, not decorative: the profile at /debug/pprof/goroutineleak
		// names the leaking call sites.
		log.Warn("leaked goroutines accumulating",
			"leaked", n, "total", runtime.NumGoroutine(),
			"profile", "/debug/pprof/goroutineleak")
		return nil
	}
	log.Debug("goroutine health", "leaked", n, "total", runtime.NumGoroutine())
	return nil
}
