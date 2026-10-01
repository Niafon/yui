package inference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/provider"
)

func TestJevNativeChoiceAndConfidence(t *testing.T) {
	t.Setenv("YUI_TEST_JEV_KEY", "test")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["questions"] == nil || body["messages"] != nil {
			t.Error("not native Decisions protocol")
		}
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing auth")
		}
		w.Write([]byte(`{"id":"test-decision","answers":{"model":{"type":"choice","choice":"light","confidence":0.91,"probabilities":{"light":0.91,"heavy":0.09}}}}`))
	}))
	defer s.Close()
	j := NewJevRouter(config.JevConfig{Endpoint: s.URL, APIKeyEnv: "YUI_TEST_JEV_KEY", TimeoutMS: 1000, MinConfidence: 0.8})
	got := j.Choose(context.Background(), "привет", []model.ProviderConfig{{ID: "light"}, {ID: "heavy"}})
	if got.Status != "selected" || got.DecisionID != "test-decision" || got.Selected != "light" {
		t.Fatalf("%+v", got)
	}
	j.cfg.MinConfidence = 0.95
	if got := j.Choose(context.Background(), "привет", []model.ProviderConfig{{ID: "light"}, {ID: "heavy"}}); got.Status != "low_confidence" {
		t.Fatal(got)
	}
}

func TestJevRejectsMalformedAndUnknownChoice(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"answers":{"model":{"type":"choice","choice":"shell","probabilities":{"shell":1}}}}`,
		`{"answers":{"model":{"type":"choice","choice":"light","confidence":2,"probabilities":{"light":1}}}}`,
	} {
		if _, err := parseRoute([]byte(raw), map[string]string{"light": ""}); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestLocalRolesAndMissingKey(t *testing.T) {
	cfg := testInferenceConfig()
	cfg.Jev = config.JevConfig{Enabled: true, APIKeyEnv: "YUI_MISSING_JEV_KEY", TimeoutMS: 100, MinConfidence: 0.8}
	t.Setenv("YUI_MISSING_JEV_KEY", "")
	reg := provider.NewRegistry()
	reg.Build([]model.ProviderConfig{
		{ID: "light", Kind: model.KindLLM, Driver: "mock", Local: true, AutoSelect: true, Quality: 72, Tags: []string{"lightweight"}},
		{ID: "heavy", Kind: model.KindLLM, Driver: "mock", Local: true, AutoSelect: true, Quality: 94, Tags: []string{"reasoning"}},
	}, map[string]string{"llm": "light"})
	m := New(cfg, reg, nil, nil, context.Background())
	d, err := m.Select(context.Background(), Task{Kind: model.KindLLM, Text: "Привет", RoutingAllowed: true})
	if err != nil || d.ProviderID != "light" || d.Routing.Status != "missing_key" {
		t.Fatalf("%+v %v", d, err)
	}
	d, err = m.Select(context.Background(), Task{Kind: model.KindLLM, Text: "Проанализируй архитектуру, сравни код и объясни почему ошибка", RoutingAllowed: true})
	if err != nil || d.ProviderID != "heavy" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestAutoNeverResurrectsBlockedDefault(t *testing.T) {
	cfg := testInferenceConfig()
	reg := provider.NewRegistry()
	reg.Build([]model.ProviderConfig{{ID: "gpu", Kind: model.KindLLM, Driver: "mock", Local: true, AutoSelect: true, Quality: 90, ExecutionBackend: "gpu", EstimatedVRAMMB: 7000}}, map[string]string{"llm": "gpu"})
	mon := NewMonitor(cfg)
	mon.current = Snapshot{VRAMUsedMB: 10000, VRAMFreeMB: 1}
	m := New(cfg, reg, mon, nil, context.Background())
	if _, err := m.Select(context.Background(), Task{Kind: model.KindLLM}); err == nil || !strings.Contains(err.Error(), "no eligible") {
		t.Fatalf("%v", err)
	}
}
