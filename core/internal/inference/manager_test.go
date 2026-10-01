package inference

import (
	"context"
	"testing"

	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/provider"
)

func testRegistry(t *testing.T) *provider.Registry {
	t.Helper()
	r := provider.NewRegistry()
	cfgs := []model.ProviderConfig{
		{ID: "q9-gpu", Kind: model.KindLLM, Driver: "mock", Local: true, Model: "qwen9", ModelFamily: "qwen9", ExecutionBackend: "gpu", Quality: 92, EstimatedVRAMMB: 7000, AutoSelect: true},
		{ID: "q9-cpu", Kind: model.KindLLM, Driver: "mock", Local: true, Model: "qwen9", ModelFamily: "qwen9", ExecutionBackend: "cpu", Quality: 92, EstimatedRAMMB: 7600, AutoSelect: true, GamingSafe: true},
		{ID: "q4-cpu", Kind: model.KindLLM, Driver: "mock", Local: true, Model: "qwen4", ModelFamily: "qwen4", ExecutionBackend: "cpu", Quality: 76, EstimatedRAMMB: 3900, AutoSelect: true, GamingSafe: true},
	}
	if err := r.Build(cfgs, map[string]string{"llm": "q9-gpu"}); err != nil {
		t.Fatal(err)
	}
	return r
}

func testInferenceConfig() config.InferenceConfig {
	return config.InferenceConfig{Enabled: true, Mode: ModeAuto, AllowAutoDowngrade: true, MinimumQuality: 1, MaxGPUUtilPercent: 82, MaxCPUUtilPercent: 88, VRAMReserveMB: 1024, MaxFPSImpactPercent: 2}
}

func TestResidentGPUDoesNotChargeWeightsTwice(t *testing.T) {
	c := model.ProviderConfig{ID: "gpu", ExecutionBackend: "gpu", EstimatedVRAMMB: 7000, Quality: 92}
	cfg := testInferenceConfig()
	p := Preferences{Mode: ModeAuto}
	snap := Snapshot{VRAMFreeMB: 3000}
	if _, _, ok := scoreCandidate(c, p, snap, 20, cfg); ok {
		t.Fatal("cold model should not fit")
	}
	if _, _, ok := scoreCandidate(c, p, snap, 20, cfg, true); !ok {
		t.Fatal("resident model should fit with free reserve")
	}
	snap.VRAMFreeMB = 512
	if _, _, ok := scoreCandidate(c, p, snap, 20, cfg, true); ok {
		t.Fatal("resident model must still respect reserve")
	}
	snap.VRAMFreeMB = 3000
	snap.GPUPercent = 99
	if _, _, ok := scoreCandidate(c, p, snap, 20, cfg, true); ok {
		t.Fatal("resident model must still respect GPU load")
	}
}

func TestGamingAutoPrefersGamingSafeCPU(t *testing.T) {
	cfg := testInferenceConfig()
	mon := NewMonitor(cfg)
	mon.UpdateExternal(Snapshot{GameActive: true, FPS: 144, BaselineFPS: 144})
	m := New(cfg, testRegistry(t), mon, nil, context.Background())
	d, err := m.Select(context.Background(), Task{Kind: model.KindLLM, Text: "что мне купить дальше"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ProviderID != "q4-cpu" {
		t.Fatalf("expected gaming-safe q4-cpu, got %s (%s)", d.ProviderID, d.Reason)
	}
}

func TestManualModelLockKeepsFamilyButMayMoveBackend(t *testing.T) {
	cfg := testInferenceConfig()
	cfg.Mode = ModeManual
	cfg.LockedModel = "qwen9"
	mon := NewMonitor(cfg)
	mon.UpdateExternal(Snapshot{GameActive: true, FPS: 144, BaselineFPS: 144})
	m := New(cfg, testRegistry(t), mon, nil, context.Background())
	d, err := m.Select(context.Background(), Task{Kind: model.KindLLM})
	if err != nil {
		t.Fatal(err)
	}
	if d.ModelFamily != "qwen9" {
		t.Fatalf("model lock changed family: %#v", d)
	}
	if d.Backend != "cpu" {
		t.Fatalf("expected locked model to move to CPU during game, got %s", d.Backend)
	}
}

func TestExplicitProviderOverridesAuto(t *testing.T) {
	cfg := testInferenceConfig()
	mon := NewMonitor(cfg)
	mon.UpdateExternal(Snapshot{GameActive: true, FPS: 100, BaselineFPS: 144})
	m := New(cfg, testRegistry(t), mon, nil, context.Background())
	d, err := m.ActivateExplicit(context.Background(), "q9-gpu", model.KindLLM, "session provider lock")
	if err != nil {
		t.Fatal(err)
	}
	if d.ProviderID != "q9-gpu" || !d.Explicit {
		t.Fatalf("explicit choice was not preserved: %#v", d)
	}
}

func TestDialogueLockDoesNotDisableSpeechProvider(t *testing.T) {
	reg := testRegistry(t)
	if err := reg.Register(model.ProviderConfig{ID: "speech", Kind: model.KindTTS, Driver: "mock", Local: true, Model: "voice", ExecutionBackend: "cpu"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetDefault(model.KindTTS, "speech"); err != nil {
		t.Fatal(err)
	}
	cfg := testInferenceConfig()
	cfg.Mode = ModeManual
	cfg.LockedModel = "qwen9"
	cfg.PreferredProvider = "q9-gpu"
	m := New(cfg, reg, NewMonitor(cfg), nil, context.Background())
	d, err := m.Select(context.Background(), Task{Kind: model.KindTTS})
	if err != nil || d.ProviderID != "speech" {
		t.Fatalf("dialogue lock disabled TTS: %+v, %v", d, err)
	}
}

func TestOwnerSelectedSpeechModelOverridesAutoSelection(t *testing.T) {
	reg := provider.NewRegistry()
	for _, c := range []model.ProviderConfig{
		{ID: "auto-voice", Kind: model.KindTTS, Driver: "mock", Local: true, Model: "voice-a", Quality: 90, AutoSelect: true},
		{ID: "chosen-voice", Kind: model.KindTTS, Driver: "mock", Local: true, Model: "voice-b", Quality: 50, AutoSelect: false},
	} {
		if err := reg.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	cfg := testInferenceConfig()
	m := New(cfg, reg, nil, nil, context.Background())
	m.SetExplicitDefault(model.KindTTS, "chosen-voice")
	d, err := m.Select(context.Background(), Task{Kind: model.KindTTS})
	if err != nil || d.ProviderID != "chosen-voice" || !d.Explicit {
		t.Fatalf("speech choice was not applied: %+v, %v", d, err)
	}
}

func TestStatusKeepsLastLLMWhenSpeechRuns(t *testing.T) {
	cfg := testInferenceConfig()
	m := New(cfg, testRegistry(t), NewMonitor(cfg), nil, context.Background())
	m.record(Decision{ProviderID: "q9-gpu", ModelFamily: "qwen9", Kind: model.KindLLM})
	m.record(Decision{ProviderID: "speech", ModelFamily: "silero-v5", Kind: model.KindTTS})
	status := m.Status()
	if status.Last == nil || status.Last.Kind != model.KindTTS {
		t.Fatalf("latest general decision = %#v, want TTS", status.Last)
	}
	if status.LastLLM == nil || status.LastLLM.ProviderID != "q9-gpu" {
		t.Fatalf("latest LLM decision = %#v, want q9-gpu", status.LastLLM)
	}
}

func TestFPSGuardrailBlocksGPU(t *testing.T) {
	cfg := testInferenceConfig()
	mon := NewMonitor(cfg)
	mon.UpdateExternal(Snapshot{GameActive: true, FPS: 135, BaselineFPS: 144})
	m := New(cfg, testRegistry(t), mon, nil, context.Background())
	d, err := m.Select(context.Background(), Task{Kind: model.KindLLM, Text: "сложный анализ архитектуры"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Backend == "gpu" {
		t.Fatalf("GPU should be blocked after FPS drop, got %#v", d)
	}
}

func TestIdleManualModelLockPrefersGPUBackend(t *testing.T) {
	cfg := testInferenceConfig()
	cfg.Mode = ModeManual
	cfg.LockedModel = "qwen9"
	mon := NewMonitor(cfg)
	m := New(cfg, testRegistry(t), mon, nil, context.Background())
	d, err := m.Select(context.Background(), Task{Kind: model.KindLLM})
	if err != nil {
		t.Fatal(err)
	}
	if d.ModelFamily != "qwen9" || d.Backend != "gpu" {
		t.Fatalf("idle locked qwen9 should use GPU, got %#v", d)
	}
}
