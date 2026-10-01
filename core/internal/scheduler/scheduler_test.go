package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/yui-companion/core/internal/config"
)

// These tests run inside synctest bubbles, where time is virtual: a job with a
// fifteen minute interval is exercised in microseconds, and there is no sleep
// to tune. Before Go 1.25 the only way to test this scheduler was to shrink
// the intervals and sleep, which made the suite both slow and flaky — the
// behaviour under test is timing, so timing could not be approximated.

func testConfig() config.SchedulerConfig {
	return config.SchedulerConfig{
		Enabled: true, TickSeconds: 1, MaxParallelJobs: 1, PauseOnActiveTurn: true,
	}
}

func TestJobRunsOnItsInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32
		s := New(testConfig(), func() bool { return false })
		s.Add(Job{
			Name:     "counter",
			Interval: 10 * time.Second,
			Run: func(context.Context) error {
				runs.Add(1)
				return nil
			},
		})
		s.Start(t.Context())

		// The first tick runs the job because it has never run.
		synctest.Sleep(2 * time.Second)
		if got := runs.Load(); got != 1 {
			t.Fatalf("runs after 2s = %d, want 1", got)
		}

		// It must not run again until its interval elapses.
		synctest.Sleep(5 * time.Second)
		if got := runs.Load(); got != 1 {
			t.Fatalf("runs after 7s = %d, want 1 — the interval was ignored", got)
		}

		synctest.Sleep(6 * time.Second)
		if got := runs.Load(); got != 2 {
			t.Fatalf("runs after 13s = %d, want 2", got)
		}
		s.Stop()
	})
}

// This is the rule that matters most: background reflection must never steal
// time from a live conversation (NFR-006, AC-14).
func TestBusySessionSuppressesBackgroundWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var busy atomic.Bool
		var runs atomic.Int32
		busy.Store(true)

		s := New(testConfig(), busy.Load)
		s.Add(Job{
			Name:     "reflection",
			Interval: 5 * time.Second,
			Run: func(context.Context) error {
				runs.Add(1)
				return nil
			},
		})
		s.Start(t.Context())

		synctest.Sleep(20 * time.Second)
		if got := runs.Load(); got != 0 {
			t.Fatalf("job ran %d times while a turn was in flight; it must yield", got)
		}
		status := s.Status()
		if len(status) != 1 || status[0].Skipped == 0 {
			t.Fatalf("skips were not recorded: %+v", status)
		}

		// Once the conversation ends, the job catches up.
		busy.Store(false)
		synctest.Sleep(3 * time.Second)
		if got := runs.Load(); got == 0 {
			t.Fatal("job never resumed after the conversation ended")
		}
		s.Stop()
	})
}

// A job already running when the owner starts speaking is cancelled, not
// allowed to finish. Reflection is worth less than a responsive answer.
func TestRunningJobIsCancelledWhenTheOwnerSpeaks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var busy atomic.Bool
		cancelled := make(chan struct{}, 1)

		s := New(testConfig(), busy.Load)
		s.Add(Job{
			Name:     "long-reflection",
			Interval: time.Hour,
			Run: func(ctx context.Context) error {
				// Simulates an LLM extraction pass over the transcript.
				select {
				case <-ctx.Done():
					cancelled <- struct{}{}
					return ctx.Err()
				case <-time.After(30 * time.Second):
					return nil
				}
			},
		})
		s.Start(t.Context())

		synctest.Sleep(2 * time.Second) // job is now running
		busy.Store(true)                // owner starts talking
		synctest.Sleep(3 * time.Second) // watchdog polls twice a second

		select {
		case <-cancelled:
		default:
			t.Fatal("a running job was not cancelled when the conversation started")
		}
		if st := s.Status()[0]; st.Canceled == 0 {
			t.Fatalf("cancellation was not recorded: %+v", st)
		}
		s.Stop()
	})
}

func TestFailingJobDoesNotStopTheScheduler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var good atomic.Int32
		s := New(testConfig(), func() bool { return false })
		s.Add(Job{
			Name:     "broken",
			Interval: 5 * time.Second,
			Run:      func(context.Context) error { return errors.New("worker unreachable") },
		})
		s.Add(Job{
			Name:     "healthy",
			Interval: 5 * time.Second,
			Run: func(context.Context) error {
				good.Add(1)
				return nil
			},
		})
		s.Start(t.Context())

		synctest.Sleep(12 * time.Second)
		if good.Load() < 2 {
			t.Fatalf("healthy job ran %d times; a failing neighbour must not block it", good.Load())
		}
		for _, st := range s.Status() {
			if st.Name == "broken" && st.LastErr == "" {
				t.Fatal("the failure was not recorded — a silent failure is the one bug we refuse")
			}
		}
		s.Stop()
	})
}

func TestDisabledSchedulerRunsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := testConfig()
		cfg.Enabled = false
		var runs atomic.Int32
		s := New(cfg, func() bool { return false })
		s.Add(Job{
			Name:     "counter",
			Interval: time.Second,
			Run:      func(context.Context) error { runs.Add(1); return nil },
		})
		s.Start(t.Context())
		synctest.Sleep(10 * time.Second)
		if runs.Load() != 0 {
			t.Fatal("a disabled scheduler must not run jobs")
		}
	})
}
