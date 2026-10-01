package provider

import (
	"testing"

	"github.com/yui-companion/core/internal/model"
)

func TestRegisterComputerCapabilityDoesNotRequireProviderInterface(t *testing.T) {
	for _, driver := range []string{"openai", "worker", "mock"} {
		t.Run(driver, func(t *testing.T) {
			r := NewRegistry()
			cfg := model.ProviderConfig{
				ID: "fara", Kind: model.KindComputer, Driver: driver,
				Endpoint: "http://127.0.0.1:8810", Local: true,
			}
			if err := r.Register(cfg); err != nil {
				t.Fatalf("computer capability should be accepted: %v", err)
			}
			got, ok := r.Config(cfg.ID)
			if !ok || got.Kind != model.KindComputer {
				t.Fatalf("computer configuration was not retained: %+v, %v", got, ok)
			}
			if _, _, err := r.LLM(cfg.ID); err != ErrNotConfigured {
				t.Fatalf("computer capability must not masquerade as an LLM: %v", err)
			}
		})
	}
}
