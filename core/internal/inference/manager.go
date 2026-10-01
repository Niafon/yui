// Package inference chooses the cheapest model/provider that satisfies the
// owner's quality and resource policy. The user's explicit choice always wins.
package inference

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/provider"
	"github.com/yui-companion/core/internal/supervisor"
)

const (
	ModeAuto       = "auto"
	ModeMaxQuality = "max_quality"
	ModeBalanced   = "balanced"
	ModeGaming     = "gaming"
	ModeManual     = "manual"
)

type Preferences struct {
	Mode               string `json:"mode"`
	PreferredProvider  string `json:"preferred_provider,omitempty"`
	LockedModel        string `json:"locked_model,omitempty"`
	AllowAutoDowngrade bool   `json:"allow_auto_downgrade"`
	MinimumQuality     int    `json:"minimum_quality"`
}

type Task struct {
	RoutingAllowed bool               `json:"-"`
	Kind           model.ProviderKind `json:"kind"`
	Text           string             `json:"text,omitempty"`
	HasVisual      bool               `json:"has_visual,omitempty"`
	LatencyLow     bool               `json:"latency_low,omitempty"`
}

type Decision struct {
	Routing     *RouteResult       `json:"routing,omitempty"`
	ProviderID  string             `json:"provider_id"`
	Model       string             `json:"model,omitempty"`
	ModelFamily string             `json:"model_family,omitempty"`
	Kind        model.ProviderKind `json:"kind"`
	Backend     string             `json:"backend,omitempty"`
	Quality     int                `json:"quality"`
	Mode        string             `json:"mode"`
	Reason      string             `json:"reason"`
	Score       float64            `json:"score"`
	At          time.Time          `json:"at"`
	Snapshot    Snapshot           `json:"snapshot"`
	Explicit    bool               `json:"explicit"`
}

type ModelStatus struct {
	Local       bool               `json:"local"`
	ProviderID  string             `json:"provider_id"`
	Kind        model.ProviderKind `json:"kind"`
	Model       string             `json:"model,omitempty"`
	ModelFamily string             `json:"model_family,omitempty"`
	Backend     string             `json:"backend,omitempty"`
	Quality     int                `json:"quality"`
	RAMMB       int                `json:"estimated_ram_mb,omitempty"`
	VRAMMB      int                `json:"estimated_vram_mb,omitempty"`
	GamingSafe  bool               `json:"gaming_safe"`
	AutoSelect  bool               `json:"auto_select"`
	KeepWarm    bool               `json:"keep_warm"`
	WorkerID    string             `json:"worker_id,omitempty"`
}

type Status struct {
	Preferences Preferences   `json:"preferences"`
	Telemetry   Snapshot      `json:"telemetry"`
	Last        *Decision     `json:"last_decision,omitempty"`
	LastLLM     *Decision     `json:"last_llm_decision,omitempty"`
	Models      []ModelStatus `json:"models"`
}

type Manager struct {
	mu               sync.RWMutex
	cfg              config.InferenceConfig
	prefs            Preferences
	reg              *provider.Registry
	monitor          *Monitor
	sup              *supervisor.Supervisor
	rootCtx          context.Context
	last             *Decision
	lastLLM          *Decision
	router           *JevRouter
	lease            chan struct{}
	activeByKind     map[model.ProviderKind]string
	explicitDefaults map[model.ProviderKind]string
}

func New(cfg config.InferenceConfig, reg *provider.Registry, mon *Monitor, sup *supervisor.Supervisor, root context.Context) *Manager {
	if root == nil {
		root = context.Background()
	}
	p := Preferences{
		Mode:               normalizeMode(cfg.Mode),
		PreferredProvider:  cfg.PreferredProvider,
		LockedModel:        cfg.LockedModel,
		AllowAutoDowngrade: cfg.AllowAutoDowngrade,
		MinimumQuality:     cfg.MinimumQuality,
	}
	if p.MinimumQuality <= 0 {
		p.MinimumQuality = 1
	}
	return &Manager{cfg: cfg, prefs: p, reg: reg, monitor: mon, sup: sup, rootCtx: root, router: NewJevRouter(cfg.Jev), lease: make(chan struct{}, 1), activeByKind: map[model.ProviderKind]string{}, explicitDefaults: map[model.ProviderKind]string{}}
}

// SetExplicitDefault makes an owner-selected non-dialogue model take precedence
// over automatic scoring. A configured installation default remains a fallback.
func (m *Manager) SetExplicitDefault(kind model.ProviderKind, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == "" {
		delete(m.explicitDefaults, kind)
	} else {
		m.explicitDefaults[kind] = id
	}
}

// Acquire serializes model-using turns, vision and confirmed computer tasks.
// Keep the lease until inference finishes, including streamed generation.
func (m *Manager) Acquire(ctx context.Context) (func(), error) {
	select {
	case m.lease <- struct{}{}:
		return func() { <-m.lease }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *Manager) PrepareWorker(ctx context.Context, id string) error {
	if m.sup == nil {
		return errors.New("inference: supervisor unavailable")
	}
	m.sup.Start(m.rootCtx, id)
	return m.sup.WaitHealthy(ctx, id, time.Duration(m.cfg.WarmupTimeoutSeconds)*time.Second)
}

func normalizeMode(mode string) string {
	switch mode {
	case ModeAuto, ModeMaxQuality, ModeBalanced, ModeGaming, ModeManual:
		return mode
	default:
		return ModeAuto
	}
}

func (m *Manager) Preferences() Preferences {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.prefs
}

func (m *Manager) UpdatePreferences(p Preferences) error {
	p.Mode = normalizeMode(p.Mode)
	if p.MinimumQuality < 1 || p.MinimumQuality > 100 {
		return errors.New("inference: minimum_quality must be between 1 and 100")
	}
	if p.PreferredProvider != "" {
		if _, ok := m.reg.Config(p.PreferredProvider); !ok {
			return fmt.Errorf("inference: unknown preferred provider %q", p.PreferredProvider)
		}
	}
	if p.Mode == ModeManual && strings.TrimSpace(p.LockedModel) == "" && strings.TrimSpace(p.PreferredProvider) == "" {
		return errors.New("inference: manual mode requires locked_model or preferred_provider")
	}
	m.mu.Lock()
	m.prefs = p
	m.mu.Unlock()
	return nil
}

func (m *Manager) UpdateTelemetry(s Snapshot) {
	if m.monitor != nil {
		m.monitor.UpdateExternal(s)
	}
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	p := m.prefs
	var last *Decision
	if m.last != nil {
		cp := *m.last
		last = &cp
	}
	var lastLLM *Decision
	if m.lastLLM != nil {
		cp := *m.lastLLM
		lastLLM = &cp
	}
	m.mu.RUnlock()

	models := make([]ModelStatus, 0)
	for _, c := range m.reg.Configs() {
		models = append(models, ModelStatus{
			Local:      c.Local,
			ProviderID: c.ID, Kind: c.Kind, Model: c.Model, ModelFamily: family(c),
			Backend: backend(c), Quality: quality(c), RAMMB: c.EstimatedRAMMB,
			VRAMMB: c.EstimatedVRAMMB, GamingSafe: c.GamingSafe,
			AutoSelect: c.AutoSelect, KeepWarm: c.KeepWarm, WorkerID: c.WorkerID,
		})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Kind != models[j].Kind {
			return models[i].Kind < models[j].Kind
		}
		if models[i].Quality != models[j].Quality {
			return models[i].Quality > models[j].Quality
		}
		return models[i].ProviderID < models[j].ProviderID
	})
	return Status{Preferences: p, Telemetry: m.snapshot(), Last: last, LastLLM: lastLLM, Models: models}
}

func (m *Manager) snapshot() Snapshot {
	if m.monitor == nil {
		return Snapshot{At: time.Now().UTC(), Source: "unavailable"}
	}
	return m.monitor.Snapshot()
}

// Select chooses and activates a provider. A provider explicitly chosen by a
// session bypasses scoring but still uses the managed-worker lifecycle.
func (m *Manager) Select(ctx context.Context, task Task) (Decision, error) {
	p := m.Preferences()
	// The UI's global model/provider lock selects the dialogue model. Other
	// modalities keep resource limits but use their own session/default choice.
	if task.Kind != model.KindLLM {
		p.LockedModel = ""
		p.PreferredProvider = ""
		m.mu.RLock()
		id := m.explicitDefaults[task.Kind]
		m.mu.RUnlock()
		if id != "" {
			return m.ActivateExplicit(ctx, id, task.Kind, "owner-selected default")
		}
	}
	snap := m.snapshot()
	candidates := make([]model.ProviderConfig, 0)
	for _, c := range m.reg.Configs() {
		if c.Kind != task.Kind || !c.AutoSelect || !c.Local {
			continue
		}
		if quality(c) < p.MinimumQuality {
			continue
		}
		if p.LockedModel != "" && family(c) != p.LockedModel && c.Model != p.LockedModel {
			continue
		}
		candidates = append(candidates, c)
	}

	if p.Mode == ModeManual && p.PreferredProvider != "" {
		return m.ActivateExplicit(ctx, p.PreferredProvider, task.Kind, "manual provider lock")
	}
	if len(candidates) == 0 {
		if p.LockedModel != "" {
			return Decision{}, errors.New("inference: no eligible provider for locked model")
		}
		fallback := m.reg.Default(task.Kind)
		if fallback == "" {
			return Decision{}, provider.ErrNotConfigured
		}
		c, _ := m.reg.Config(fallback)
		if !c.Local {
			return Decision{}, errors.New("inference: remote default requires explicit selection")
		}
		if _, _, usable := scoreCandidate(c, p, snap, taskComplexity(task), m.cfg, m.resident(c)); !usable {
			return Decision{}, errors.New("inference: default blocked by resource limits")
		}
		return m.ActivateExplicit(ctx, fallback, task.Kind, "no auto-select candidate; using configured default")
	}

	preferredQuality := 0
	if p.PreferredProvider != "" {
		if c, ok := m.reg.Config(p.PreferredProvider); ok {
			preferredQuality = quality(c)
		}
	}
	if !p.AllowAutoDowngrade && preferredQuality > 0 {
		filtered := candidates[:0]
		for _, c := range candidates {
			if quality(c) >= preferredQuality {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) > 0 {
			candidates = filtered
		}
	}

	complexity := taskComplexity(task)
	if p.Mode == ModeMaxQuality {
		complexity = 100
	}
	type ranked struct {
		c      model.ProviderConfig
		score  float64
		reason string
	}
	rankedCandidates := make([]ranked, 0, len(candidates))
	for _, c := range candidates {
		score, reason, usable := scoreCandidate(c, p, m.afterEviction(c, snap), complexity, m.cfg, m.resident(c))
		if usable {
			rankedCandidates = append(rankedCandidates, ranked{c: c, score: score, reason: reason})
		}
	}
	sort.Slice(rankedCandidates, func(i, j int) bool {
		if rankedCandidates[i].score != rankedCandidates[j].score {
			return rankedCandidates[i].score > rankedCandidates[j].score
		}
		return rankedCandidates[i].c.ID < rankedCandidates[j].c.ID
	})
	var routing *RouteResult
	if m.cfg.Jev.Enabled && task.Kind == model.KindLLM && p.Mode != ModeManual && p.LockedModel == "" && p.PreferredProvider == "" {
		r := RouteResult{Status: "policy_blocked"}
		if task.RoutingAllowed && len(rankedCandidates) > 1 {
			eligible := make([]model.ProviderConfig, len(rankedCandidates))
			for i, rc := range rankedCandidates {
				eligible[i] = rc.c
			}
			r = m.router.Choose(ctx, task.Text, eligible)
			if r.Status == "selected" {
				for i := range rankedCandidates {
					if rankedCandidates[i].c.ID == r.Selected {
						chosen := rankedCandidates[i]
						copy(rankedCandidates[1:i+1], rankedCandidates[:i])
						rankedCandidates[0] = chosen
						break
					}
				}
			}
		} else if len(rankedCandidates) <= 1 {
			r.Status = "single_candidate"
		}
		routing = &r
	}
	var activationErrors []string
	for _, rc := range rankedCandidates {
		if err := ctx.Err(); err != nil {
			return Decision{}, err
		}
		if err := m.activate(ctx, rc.c); err != nil {
			activationErrors = append(activationErrors, rc.c.ID+": "+err.Error())
			continue
		}
		d := Decision{
			Routing:    routing,
			ProviderID: rc.c.ID, Model: rc.c.Model, ModelFamily: family(rc.c), Kind: rc.c.Kind,
			Backend: backend(rc.c), Quality: quality(rc.c), Mode: p.Mode, Reason: rc.reason,
			Score: rc.score, At: time.Now().UTC(), Snapshot: snap,
		}
		if len(activationErrors) > 0 {
			d.Reason += "; higher-ranked provider unavailable: " + strings.Join(activationErrors, " | ")
		}
		m.record(d)
		return d, nil
	}

	return Decision{}, fmt.Errorf("inference: no eligible local provider available: %s", strings.Join(activationErrors, " | "))
}

func (m *Manager) ActivateExplicit(ctx context.Context, providerID string, kind model.ProviderKind, reason string) (Decision, error) {
	c, ok := m.reg.Config(providerID)
	if !ok || c.Kind != kind {
		return Decision{}, provider.ErrNotConfigured
	}
	if err := m.activate(ctx, c); err != nil {
		return Decision{}, err
	}
	d := Decision{ProviderID: c.ID, Model: c.Model, ModelFamily: family(c), Kind: c.Kind,
		Backend: backend(c), Quality: quality(c), Mode: m.Preferences().Mode,
		Reason: reason, At: time.Now().UTC(), Snapshot: m.snapshot(), Explicit: true}
	m.record(d)
	return d, nil
}

func (m *Manager) record(d Decision) {
	m.mu.Lock()
	m.last = &d
	if d.Kind == model.KindLLM {
		m.lastLLM = &d
	}
	m.activeByKind[d.Kind] = d.ProviderID
	m.mu.Unlock()
}

func (m *Manager) activate(ctx context.Context, c model.ProviderConfig) error {
	if c.WorkerID == "" || m.sup == nil {
		return nil
	}
	// Grouped GPU workers are mutually exclusive even across provider kinds.
	if hasTag(c, "exclusive-gpu") {
		for _, other := range m.reg.Configs() {
			if other.WorkerID != "" && other.WorkerID != c.WorkerID && hasTag(other, "exclusive-gpu") {
				if err := m.sup.StopAndWait(ctx, other.WorkerID); err != nil {
					return err
				}
			}
		}
	}
	m.mu.RLock()
	previousID := m.activeByKind[c.Kind]
	m.mu.RUnlock()
	if previousID != "" && previousID != c.ID {
		if prev, ok := m.reg.Config(previousID); ok && prev.WorkerID != "" && !prev.KeepWarm {
			if err := m.sup.StopAndWait(ctx, prev.WorkerID); err != nil {
				return err
			}
		}
	}
	m.sup.Start(m.rootCtx, c.WorkerID)
	if err := m.sup.WaitHealthy(ctx, c.WorkerID, time.Duration(m.cfg.WarmupTimeoutSeconds)*time.Second); err != nil {
		m.sup.Stop(c.WorkerID)
		return err
	}
	return nil
}

func hasTag(c model.ProviderConfig, tag string) bool {
	for _, value := range c.Tags {
		if value == tag {
			return true
		}
	}
	return false
}

// Only managed resident workers in the same exclusive group are reclaimable;
// game/desktop memory is never counted as available for a model switch.
func (m *Manager) afterEviction(c model.ProviderConfig, snap Snapshot) Snapshot {
	if !hasTag(c, "exclusive-gpu") || snap.VRAMUsedMB+snap.VRAMFreeMB == 0 {
		return snap
	}
	seen := map[string]bool{}
	for _, other := range m.reg.Configs() {
		if other.WorkerID == c.WorkerID || seen[other.WorkerID] || !hasTag(other, "exclusive-gpu") || !m.resident(other) {
			continue
		}
		seen[other.WorkerID] = true
		reclaim := other.EstimatedVRAMMB
		if reclaim > snap.VRAMUsedMB {
			reclaim = snap.VRAMUsedMB
		}
		snap.VRAMUsedMB -= reclaim
		snap.VRAMFreeMB += reclaim
	}
	return snap
}

// A healthy model already in VRAM needs only the configured free reserve.
// Charging its full weights again made every second GPU turn switch to CPU.
func (m *Manager) resident(c model.ProviderConfig) bool {
	if c.WorkerID == "" || m.sup == nil {
		return false
	}
	m.mu.RLock()
	known := false
	for _, id := range m.activeByKind {
		if active, ok := m.reg.Config(id); ok && active.WorkerID == c.WorkerID {
			known = true
			break
		}
	}
	m.mu.RUnlock()
	if !known {
		return false
	}
	for _, status := range m.sup.Status() {
		if status.ID == c.WorkerID {
			return status.State == supervisor.StateRunning
		}
	}
	return false
}

func family(c model.ProviderConfig) string {
	if strings.TrimSpace(c.ModelFamily) != "" {
		return c.ModelFamily
	}
	return c.Model
}
func backend(c model.ProviderConfig) string {
	if c.ExecutionBackend == "" {
		return "auto"
	}
	return c.ExecutionBackend
}
func quality(c model.ProviderConfig) int {
	if c.Quality <= 0 {
		return 50
	}
	if c.Quality > 100 {
		return 100
	}
	return c.Quality
}

func taskComplexity(t Task) int {
	n := len([]rune(t.Text))
	complexity := 20
	if n > 120 {
		complexity += 10
	}
	if n > 400 {
		complexity += 15
	}
	if t.HasVisual {
		complexity += 20
	}
	low := strings.ToLower(t.Text)
	for _, marker := range []string{"проанализ", "сравн", "архитект", "спроект", "почему", "объясн", "код", "debug", "reason", "analy", "compare"} {
		if strings.Contains(low, marker) {
			complexity += 7
		}
	}
	if t.LatencyLow {
		complexity -= 10
	}
	if complexity < 1 {
		return 1
	}
	if complexity > 100 {
		return 100
	}
	return complexity
}

func scoreCandidate(c model.ProviderConfig, p Preferences, s Snapshot, complexity int, cfg config.InferenceConfig, resident ...bool) (float64, string, bool) {
	q := float64(quality(c))
	b := backend(c)
	gaming := s.GameActive || p.Mode == ModeGaming
	maxGPU := cfg.MaxGPUUtilPercent
	if maxGPU <= 0 {
		maxGPU = 82
	}
	maxCPU := cfg.MaxCPUUtilPercent
	if maxCPU <= 0 {
		maxCPU = 88
	}
	reserve := cfg.VRAMReserveMB
	if reserve < 0 {
		reserve = 0
	}

	if b == "gpu" || b == "hybrid" {
		if s.GPUPercent > 0 && s.GPUPercent >= maxGPU {
			return 0, "GPU is above the configured utilization limit", false
		}
		additionalVRAM := c.EstimatedVRAMMB
		if len(resident) > 0 && resident[0] {
			additionalVRAM = 0
		}
		if s.VRAMUsedMB+s.VRAMFreeMB > 0 && s.VRAMFreeMB < additionalVRAM+reserve {
			return 0, "not enough free VRAM after reserve", false
		}
		if gaming && s.BaselineFPS > 0 && s.FPS > 0 && cfg.MaxFPSImpactPercent > 0 {
			drop := 100 * (s.BaselineFPS - s.FPS) / s.BaselineFPS
			if drop > cfg.MaxFPSImpactPercent {
				return 0, fmt.Sprintf("game FPS is %.1f%% below baseline; GPU inference blocked", drop), false
			}
		}
	}
	if s.RAMUsedMB+s.RAMFreeMB > 0 && c.EstimatedRAMMB > s.RAMFreeMB && !(len(resident) > 0 && resident[0]) {
		return 0, "not enough free RAM", false
	}
	if b == "cpu" && s.CPUPercent > 0 && s.CPUPercent >= maxCPU && !gaming {
		// Keep it as a weak candidate only when no lower-load profile exists.
		q -= 20
	}

	weight := 1.0 + float64(complexity)/100.0
	score := q * weight
	if p.Mode != ModeMaxQuality && p.Mode != ModeManual && p.PreferredProvider == "" {
		if role(c) == "lightweight" && complexity < 40 {
			score += 100
		}
		if role(c) == "reasoning" && complexity >= 40 && !gaming {
			score += 65
		}
	}
	reasons := []string{fmt.Sprintf("quality %d, task complexity %d", quality(c), complexity)}
	if c.ID == p.PreferredProvider {
		score += 22
		reasons = append(reasons, "preferred by user")
	}
	if p.Mode == ModeMaxQuality {
		score += q * 0.65
	}
	if p.Mode == ModeBalanced || p.Mode == ModeAuto {
		score += q * 0.18
	}

	if gaming {
		// In a game the scheduler optimizes not only compute contention but also
		// memory pressure/cache churn. This is what lets a 4B CPU profile beat a
		// 9B CPU profile for a trivial command while preserving 9B for hard tasks.
		if c.EstimatedRAMMB > 0 {
			score -= float64(c.EstimatedRAMMB) / 180.0
		}
		if c.EstimatedVRAMMB > 0 {
			score -= float64(c.EstimatedVRAMMB) / 120.0
		}
		if c.GamingSafe {
			score += 40
			reasons = append(reasons, "gaming-safe profile")
		}
		if b == "gpu" {
			score -= 55 + s.GPUPercent*0.35
			reasons = append(reasons, "GPU penalized while a game is active")
		} else if b == "cpu" {
			score += 28
			reasons = append(reasons, "CPU keeps the GPU available for the game")
		}
	}
	if !gaming && b == "gpu" {
		score += 25
		reasons = append(reasons, "GPU preferred while no game is active")
	}
	if b == "gpu" && s.GPUPercent > 0 {
		score -= s.GPUPercent * 0.22
	}
	if b == "cpu" && s.CPUPercent > 0 {
		score -= s.CPUPercent * 0.12
	}
	if c.EstimatedVRAMMB > 0 {
		score -= float64(c.EstimatedVRAMMB) / 750.0
	}
	if c.EstimatedRAMMB > 0 {
		score -= float64(c.EstimatedRAMMB) / 2000.0
	}
	return score, strings.Join(reasons, "; "), true
}
