// Package scheduler runs background reflection and maintenance. Every job
// yields to an active conversation: reflection must never degrade a live turn
// (SRS 12.5, NFR-006, AC-14).
package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/logging"
)

// Job is a periodic background task.
type Job struct {
	Name     string
	Interval time.Duration
	Priority int // lower runs first
	// Run must respect ctx cancellation: the scheduler cancels running jobs
	// as soon as the owner starts speaking.
	Run func(ctx context.Context) error
}

type Status struct {
	Name     string    `json:"name"`
	LastRun  time.Time `json:"last_run"`
	LastErr  string    `json:"last_error,omitempty"`
	Runs     int       `json:"runs"`
	Skipped  int       `json:"skipped"`
	Canceled int       `json:"canceled"`
}

type Scheduler struct {
	cfg  config.SchedulerConfig
	busy func() bool

	mu     sync.Mutex
	jobs   []Job
	status map[string]*Status
	cancel context.CancelFunc
}

func New(cfg config.SchedulerConfig, busy func() bool) *Scheduler {
	if busy == nil {
		busy = func() bool { return false }
	}
	return &Scheduler{cfg: cfg, busy: busy, status: map[string]*Status{}}
}

func (s *Scheduler) Add(j Job) {
	if j.Interval <= 0 {
		j.Interval = time.Minute
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, j)
	s.status[j.Name] = &Status{Name: j.Name}
}

// Start runs the loop until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	if !s.cfg.Enabled {
		logging.From(ctx).Info("scheduler disabled by configuration")
		return
	}
	tick := time.Duration(s.cfg.TickSeconds) * time.Second
	if tick <= 0 {
		tick = 20 * time.Second
	}
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	go func() {
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.runDue(ctx)
			}
		}
	}()
}

func (s *Scheduler) runDue(ctx context.Context) {
	now := time.Now()
	s.mu.Lock()
	jobs := append([]Job(nil), s.jobs...)
	s.mu.Unlock()

	for _, j := range jobs {
		s.mu.Lock()
		st := s.status[j.Name]
		due := st.LastRun.IsZero() || now.Sub(st.LastRun) >= j.Interval
		s.mu.Unlock()
		if !due {
			continue
		}
		if s.cfg.PauseOnActiveTurn && s.busy() {
			s.mu.Lock()
			st.Skipped++
			s.mu.Unlock()
			continue
		}
		s.runOne(ctx, j, st)
	}
}

func (s *Scheduler) runOne(ctx context.Context, j Job, st *Status) {
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Watch for a conversation starting and abort immediately.
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if s.cfg.PauseOnActiveTurn && s.busy() {
					cancel()
					return
				}
			}
		}
	}()

	err := j.Run(jobCtx)
	close(done)

	s.mu.Lock()
	st.LastRun = time.Now()
	st.Runs++
	if err != nil {
		if jobCtx.Err() != nil {
			st.Canceled++
		} else {
			st.LastErr = err.Error()
		}
	} else {
		st.LastErr = ""
	}
	s.mu.Unlock()

	if err != nil && jobCtx.Err() == nil {
		logging.From(ctx).Warn("background job failed", "job", j.Name, "error", err)
	}
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Scheduler) Status() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Status, 0, len(s.status))
	for _, st := range s.status {
		out = append(out, *st)
	}
	return out
}
