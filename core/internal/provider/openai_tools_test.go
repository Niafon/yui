package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yui-companion/core/internal/model"
)

func TestOpenAIAssemblesFragmentedToolCall(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"memory.search\",\"arguments\":\"{\\\"que\"}}]}}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"ry\\\":\\\"чай\\\"}\"}}]}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer s.Close()
	p := NewOpenAILLM(model.ProviderConfig{Endpoint: s.URL}, "")
	stream, err := p.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for d := range stream {
		if d.ToolCall != nil {
			n++
			if d.ToolCall.Args != `{"query":"чай"}` || d.ToolCall.Name != "memory.search" || d.ToolCall.ID != "c1" {
				t.Fatal(d.ToolCall)
			}
		}
	}
	if n != 1 {
		t.Fatalf("got %d calls", n)
	}
}

func TestOpenAIFollowupCarriesToolProtocol(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatRequestJSON
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Messages) != 2 || body.Messages[1].ToolCallID != "c1" || len(body.Messages[0].ToolCalls) != 1 {
			t.Errorf("%+v", body.Messages)
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer s.Close()
	p := NewOpenAILLM(model.ProviderConfig{Endpoint: s.URL}, "")
	stream, err := p.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "time.now", Args: "{}"}}}, {Role: "tool", ToolCallID: "c1", Content: "today"}}})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
}
