package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yui-companion/core/internal/model"
)

func TestVisionLocalQwenDisablesReasoningAndRejectsEmptyAnswer(t *testing.T) {
	for _, content := range []string{"Красный квадрат.", ""} {
		t.Run(content, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Options map[string]bool `json:"chat_template_kwargs"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if enabled, ok := body.Options["enable_thinking"]; !ok || enabled {
					t.Error("local Qwen must reserve its budget for the visible description")
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
			}))
			defer server.Close()
			p := NewOpenAIVision(model.ProviderConfig{ID: "test", Endpoint: server.URL, Model: "qwen3.5-9b", Local: true}, "")
			result, err := p.Analyze(context.Background(), VisionRequest{Image: []byte{1}, MIME: "image/png"})
			if content == "" && err == nil {
				t.Fatal("empty visible answer must not be reported as success")
			}
			if content != "" && (err != nil || result.Description != content) {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
		})
	}
}
