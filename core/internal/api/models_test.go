package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/modelsettings"
	"github.com/yui-companion/core/internal/provider"
)

func TestAddModelRequestValidatesEndpointAndCredentials(t *testing.T) {
	base := addModelRequest{Kind: model.KindLLM, Source: "local", Service: "ollama", Endpoint: "http://127.0.0.1:11434/v1", Model: "qwen3:9b"}
	if cfg, err := base.config(); err != nil || cfg.Driver != "openai" || !cfg.Local || cfg.AutoSelect {
		t.Fatalf("valid local model rejected: %+v, %v", cfg, err)
	}
	for _, tc := range []struct {
		name string
		edit func(*addModelRequest)
	}{
		{"local network target", func(b *addModelRequest) { b.Endpoint = "http://192.168.1.20:11434/v1" }},
		{"localhost lookalike", func(b *addModelRequest) { b.Endpoint = "http://localhost.example.com/v1" }},
		{"URL credentials", func(b *addModelRequest) { b.Endpoint = "http://user:secret@127.0.0.1/v1" }},
		{"URL query", func(b *addModelRequest) { b.Endpoint += "?key=secret" }},
		{"hosted plaintext", func(b *addModelRequest) {
			b.Source = "provider"
			b.Service = "custom"
			b.Endpoint = "http://api.example.com/v1"
			b.APIKeyEnv = "KEY"
		}},
		{"hosted without key variable", func(b *addModelRequest) {
			b.Source = "provider"
			b.Service = "custom"
			b.Endpoint = "https://api.example.com/v1"
		}},
		{"invalid key variable", func(b *addModelRequest) { b.APIKeyEnv = "KEY;echo secret" }},
		{"provider lookalike", func(b *addModelRequest) {
			b.Source = "provider"
			b.Service = "openrouter"
			b.Endpoint = "https://openrouter.ai.example.com/v1"
			b.APIKeyEnv = "KEY"
		}},
		{"unsupported kind", func(b *addModelRequest) { b.Kind = model.KindTTS }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := base
			tc.edit(&request)
			if _, err := request.config(); err == nil {
				t.Fatal("unsafe or unsupported model was accepted")
			}
		})
	}
	remote := addModelRequest{Kind: model.KindLLM, Source: "provider", Service: "custom", Endpoint: "https://api.example.com/v1", Model: "model-x", APIKeyEnv: "MODEL_API_KEY"}
	if cfg, err := remote.config(); err != nil || cfg.Local || cfg.APIKeyEnv != remote.APIKeyEnv {
		t.Fatalf("valid hosted model rejected: %+v, %v", cfg, err)
	}
}

func TestModelAddRequiresDesktopAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	settings, err := modelsettings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	s := New(Deps{Cfg: config.Default(), Providers: reg, ModelSettings: settings})
	request := func(local bool) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/models", strings.NewReader(`{"kind":"llm","source":"local","service":"ollama","endpoint":"http://127.0.0.1:11434/v1","model":"qwen3:9b"}`))
		return r.WithContext(context.WithValue(r.Context(), callerKey, caller{Loopback: local}))
	}
	blocked := httptest.NewRecorder()
	s.handleModelAdd(blocked, request(false))
	if blocked.Code != http.StatusForbidden || len(settings.Snapshot().Providers) != 0 {
		t.Fatalf("remote caller changed model settings: %d", blocked.Code)
	}
	created := httptest.NewRecorder()
	s.handleModelAdd(created, request(true))
	if created.Code != http.StatusCreated || len(settings.Snapshot().Providers) != 1 {
		t.Fatalf("desktop model was not saved: %d %s", created.Code, created.Body.String())
	}
	if reg.Default(model.KindLLM) != "" {
		t.Fatal("adding a model silently changed the active default")
	}
	duplicate := httptest.NewRecorder()
	s.handleModelAdd(duplicate, request(true))
	if duplicate.Code != http.StatusConflict || len(settings.Snapshot().Providers) != 1 {
		t.Fatalf("duplicate model was accepted: %d", duplicate.Code)
	}
	reopened, err := modelsettings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Snapshot().Providers) != 1 {
		t.Fatal("added model did not survive reopen")
	}
}
