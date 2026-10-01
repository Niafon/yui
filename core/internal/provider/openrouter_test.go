package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yui-companion/core/internal/model"
)

func TestOpenRouterRequestAndStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("incorrect request routing/auth")
		}
		var body chatRequestJSON
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "deepseek/deepseek-v4.1-flash" || !body.Stream {
			t.Errorf("incorrect model/stream: %+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"error\":{\"code\":429,\"message\":\"Rate limited\"}}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	p := NewOpenAILLM(model.ProviderConfig{ID: "openrouter", Endpoint: server.URL + "/api/v1", Model: "deepseek/deepseek-v4.1-flash", APIKeyEnv: "OPENROUTER_API_KEY"}, "test-key")
	stream, err := p.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	failures := 0
	for d := range stream {
		if d.Err != nil && strings.Contains(d.Err.Error(), "Rate limited") && d.Done {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("stream error was lost: %d", failures)
	}
}

func TestMissingConfiguredAPIKeyFailsBeforeNetwork(t *testing.T) {
	p := NewOpenAILLM(model.ProviderConfig{ID: "openrouter", APIKeyEnv: "OPENROUTER_API_KEY"}, "")
	_, err := p.Chat(context.Background(), ChatRequest{})
	if err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("expected actionable missing key error: %v", err)
	}
}
