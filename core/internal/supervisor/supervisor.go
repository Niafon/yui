// Package supervisor owns the external processes: Python AI workers and local
// inference servers. The owner starts one application, not a row of terminals
// (SRS 14.3, NFR-007). A worker crash restarts the worker and never the core
// (CORE-005, AC-13).
package supervisor

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/logging"
)

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateFailed   State = "failed"
)

type Status struct {
	ID       string    `json:"id"`
	State    State     `json:"state"`
	PID      int       `json:"pid,omitempty"`
	Restarts int       `json:"restarts"`
	LastErr  string    `json:"last_error,omitempty"`
	Since    time.Time `json:"since"`
}

type process struct {
	spec   config.WorkerSpec
	status Status
	cancel context.CancelFunc
}

type Supervisor struct {
	mu    sync.Mutex
	procs map[string]*process
}

func New() *Supervisor { return &Supervisor{procs: map[string]*process{}} }

func (s *Supervisor) Add(spec config.WorkerSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.procs[spec.ID] = &process{spec: spec, status: Status{ID: spec.ID, State: StateStopped}}
}

// StartAll launches every autostart worker.
func (s *Supervisor) StartAll(ctx context.Context) {
	s.mu.Lock()
	ids := make([]string, 0, len(s.procs))
	for id, p := range s.procs {
		if p.spec.Autostart {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.Start(ctx, id)
	}
}

// Start runs one worker with restart-on-exit and exponential backoff.
func (s *Supervisor) Start(ctx context.Context, id string) {
	s.mu.Lock()
	p, ok := s.procs[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	if p.status.State == StateRunning || p.status.State == StateStarting {
		s.mu.Unlock()
		return
	}
	procCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.status.State = StateStarting
	p.status.Since = time.Now()
	spec := p.spec
	s.mu.Unlock()

	go s.supervise(procCtx, spec)
}

func (s *Supervisor) supervise(ctx context.Context, spec config.WorkerSpec) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			s.setState(spec.ID, StateStopped, 0, "")
			return
		}
		cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
		cmd.Dir = spec.Dir
		cmd.Env = os.Environ()
		for k, v := range spec.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			s.setState(spec.ID, StateFailed, 0, err.Error())
			logging.From(ctx).Error("worker start failed", "worker", spec.ID, "error", err)
			if !spec.Restart {
				return
			}
			time.Sleep(backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		s.setState(spec.ID, StateRunning, cmd.Process.Pid, "")
		logging.From(ctx).Info("worker started", "worker", spec.ID, "pid", cmd.Process.Pid)

		err := cmd.Wait()
		if ctx.Err() != nil {
			s.setState(spec.ID, StateStopped, 0, "")
			return
		}
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		s.bumpRestart(spec.ID, msg)
		logging.From(ctx).Warn("worker exited", "worker", spec.ID, "error", msg)
		if !spec.Restart {
			s.setState(spec.ID, StateStopped, 0, msg)
			return
		}
		time.Sleep(backoff)
		backoff = nextBackoff(backoff)
	}
}

func nextBackoff(cur time.Duration) time.Duration {
	next := cur * 2
	if next > 30*time.Second {
		return 30 * time.Second
	}
	return next
}

func (s *Supervisor) setState(id string, st State, pid int, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.procs[id]
	if !ok {
		return
	}
	p.status.State = st
	p.status.PID = pid
	p.status.Since = time.Now()
	if errMsg != "" {
		p.status.LastErr = errMsg
	}
}

func (s *Supervisor) bumpRestart(id, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.procs[id]; ok {
		p.status.Restarts++
		p.status.State = StateStarting
		if errMsg != "" {
			p.status.LastErr = errMsg
		}
	}
}

// Stop terminates one worker; StopAll terminates every worker.
func (s *Supervisor) Stop(id string) {
	s.mu.Lock()
	p, ok := s.procs[id]
	var cancel context.CancelFunc
	if ok {
		cancel = p.cancel
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// StopAndWait observes process exit before another model claims its memory.
func (s *Supervisor) StopAndWait(ctx context.Context, id string) error {
	s.Stop(id)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		s.mu.Lock()
		p, ok := s.procs[id]
		done := !ok || p.status.State == StateStopped || p.status.State == StateFailed
		s.mu.Unlock()
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("supervisor: process did not stop: " + id)
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) StopAll() {
	s.mu.Lock()
	cancels := []context.CancelFunc{}
	for _, p := range s.procs {
		if p.cancel != nil {
			cancels = append(cancels, p.cancel)
		}
	}
	s.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

func (s *Supervisor) Status() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Status, 0, len(s.procs))
	for _, p := range s.procs {
		out = append(out, p.status)
	}
	return out
}

// WaitHealthy blocks until a managed process reports healthy or the timeout
// expires. It is used by the inference manager when a cold model is selected.
func (s *Supervisor) WaitHealthy(ctx context.Context, id string, timeout time.Duration) error {
	s.mu.Lock()
	p, ok := s.procs[id]
	if !ok {
		s.mu.Unlock()
		return errors.New("supervisor: unknown worker " + id)
	}
	healthURL := p.spec.HealthURL
	s.mu.Unlock()
	if healthURL == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		s.mu.Lock()
		state := s.procs[id].status.State
		lastErr := s.procs[id].status.LastErr
		s.mu.Unlock()
		if state == StateFailed {
			if lastErr == "" {
				lastErr = "process failed"
			}
			return errors.New("supervisor: worker " + id + " failed: " + lastErr)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err == nil {
			resp, callErr := client.Do(req)
			if callErr == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("supervisor: worker " + id + " did not become healthy before timeout")
		case <-ticker.C:
		}
	}
}
