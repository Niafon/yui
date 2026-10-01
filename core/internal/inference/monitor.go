package inference

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yui-companion/core/internal/config"
)

type Snapshot struct {
	CPUPercent        float64   `json:"cpu_percent"`
	GPUPercent        float64   `json:"gpu_percent"`
	RAMUsedMB         int       `json:"ram_used_mb"`
	RAMFreeMB         int       `json:"ram_free_mb"`
	VRAMUsedMB        int       `json:"vram_used_mb"`
	VRAMFreeMB        int       `json:"vram_free_mb"`
	ForegroundProcess string    `json:"foreground_process,omitempty"`
	GameActive        bool      `json:"game_active"`
	FPS               float64   `json:"fps,omitempty"`
	BaselineFPS       float64   `json:"baseline_fps,omitempty"`
	FrameTimeMS       float64   `json:"frame_time_ms,omitempty"`
	Source            string    `json:"source"`
	At                time.Time `json:"at"`
}

type Monitor struct {
	mu       sync.RWMutex
	cfg      config.InferenceConfig
	games    map[string]struct{}
	current  Snapshot
	external Snapshot
	platform platformSampler
}

func NewMonitor(cfg config.InferenceConfig) *Monitor {
	m := &Monitor{cfg: cfg, games: map[string]struct{}{}, platform: newPlatformSampler()}
	for _, name := range cfg.GamingProcesses {
		m.games[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	m.current = Snapshot{At: time.Now().UTC(), Source: "startup"}
	return m
}

func (m *Monitor) Start(ctx context.Context) {
	interval := time.Duration(m.cfg.PollIntervalMS) * time.Millisecond
	if interval < time.Second {
		interval = 3 * time.Second
	}
	m.sample()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.sample()
			}
		}
	}()
}

func (m *Monitor) UpdateExternal(s Snapshot) {
	m.mu.Lock()
	s.At = time.Now().UTC()
	s.Source = "client"
	m.external = s
	m.mu.Unlock()
}

func (m *Monitor) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.current
	// A desktop/game overlay can provide data the portable core cannot observe
	// reliably (notably FPS/frametime). Fresh client values override those fields.
	if !m.external.At.IsZero() && time.Since(m.external.At) < 10*time.Second {
		if m.external.FPS > 0 {
			s.FPS = m.external.FPS
		}
		if m.external.BaselineFPS > 0 {
			s.BaselineFPS = m.external.BaselineFPS
		}
		if m.external.FrameTimeMS > 0 {
			s.FrameTimeMS = m.external.FrameTimeMS
		}
		if m.external.ForegroundProcess != "" {
			s.ForegroundProcess = m.external.ForegroundProcess
		}
		if m.external.GameActive {
			s.GameActive = true
		}
	}
	return s
}

func (m *Monitor) sample() {
	s := m.platform.sample()
	gpu, used, free := nvidiaSnapshot()
	if gpu >= 0 {
		s.GPUPercent = gpu
		s.VRAMUsedMB = used
		s.VRAMFreeMB = free
	}
	s.ForegroundProcess = strings.TrimSpace(s.ForegroundProcess)
	_, s.GameActive = m.games[strings.ToLower(baseProcessName(s.ForegroundProcess))]
	s.At = time.Now().UTC()
	s.Source = "core"
	m.mu.Lock()
	m.current = s
	m.mu.Unlock()
}

func nvidiaSnapshot() (float64, int, int) {
	cmd := exec.Command("nvidia-smi", "--query-gpu=utilization.gpu,memory.used,memory.free", "--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return -1, 0, 0
	}
	line := strings.Split(strings.TrimSpace(string(out)), "\n")[0]
	parts := strings.Split(line, ",")
	if len(parts) < 3 {
		return -1, 0, 0
	}
	gpu, _ := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	used, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	free, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
	return gpu, used, free
}

func baseProcessName(v string) string {
	v = strings.ReplaceAll(v, "\\", "/")
	if i := strings.LastIndex(v, "/"); i >= 0 {
		v = v[i+1:]
	}
	return v
}
