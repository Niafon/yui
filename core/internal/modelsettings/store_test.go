package modelsettings

import (
	"path/filepath"
	"testing"

	"github.com/yui-companion/core/internal/inference"
	"github.com/yui-companion/core/internal/model"
)

func TestSettingsSurviveRepeatedWritesAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model-settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c := model.ProviderConfig{ID: "custom-local", Kind: model.KindLLM, Driver: "openai", Model: "qwen3", Local: true}
	if err := s.AddProvider(c); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDefault("llm", c.ID); err != nil {
		t.Fatal(err)
	}
	p := inference.Preferences{Mode: inference.ModeManual, PreferredProvider: c.ID, LockedModel: c.Model, MinimumQuality: 55}
	if err := s.SetPreferences(p); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Snapshot()
	if len(got.Providers) != 1 || got.Providers[0].ID != c.ID || got.Defaults["llm"] != c.ID || got.Preferences == nil || *got.Preferences != p {
		t.Fatalf("settings were not restored: %+v", got)
	}
	if err := reopened.RemoveProvider(c.ID); err != nil {
		t.Fatal(err)
	}
	if len(reopened.Snapshot().Providers) != 0 {
		t.Fatal("provider was not removed")
	}
}
