package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/yui-companion/core/internal/model"
)

// The OpenAI-compatible driver covers llama.cpp, Ollama, LM Studio, OpenRouter
// and hosted APIs with one code path (AI-003, AI-004). Whether a call is local
// or remote is a configuration fact, and it is the permission engine — not
// this file — that decides what may be sent.

type openAIBase struct {
	cfg    model.ProviderConfig
	apiKey string
	http   *http.Client
}

func (b *openAIBase) endpoint(path string) string {
	base := strings.TrimRight(b.cfg.Endpoint, "/")
	if base == "" {
		base = "http://127.0.0.1:11434/v1"
	}
	return base + path
}

func (b *openAIBase) request(ctx context.Context, path string, body any) (*http.Request, error) {
	if b.cfg.APIKeyEnv != "" && strings.TrimSpace(b.apiKey) == "" {
		return nil, errors.New("provider " + b.cfg.ID + ": set " + b.cfg.APIKeyEnv + " and restart core")
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint(path), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if b.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.apiKey)
	}
	return req, nil
}

type OpenAILLM struct{ openAIBase }

func NewOpenAILLM(c model.ProviderConfig, apiKey string) *OpenAILLM {
	return &OpenAILLM{openAIBase{cfg: c, apiKey: apiKey, http: &http.Client{Timeout: 5 * time.Minute}}}
}

func (p *OpenAILLM) Info() Info {
	return Info{ID: p.cfg.ID, Kind: model.KindLLM, Driver: "openai", Model: p.cfg.Model,
		Local: p.cfg.Local, Streaming: true, Tools: true}
}

type chatMessageJSON struct {
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	Name       string         `json:"name,omitempty"`
}

type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatRequestJSON struct {
	Model       string            `json:"model"`
	Messages    []chatMessageJSON `json:"messages"`
	Stream      bool              `json:"stream"`
	Temperature float64           `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Tools       []toolJSON        `json:"tools,omitempty"`
}

type toolJSON struct {
	Type     string         `json:"type"`
	Function map[string]any `json:"function"`
}

type chatChunkJSON struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (p *OpenAILLM) Chat(ctx context.Context, req ChatRequest) (<-chan Delta, error) {
	body := chatRequestJSON{
		Model:       p.cfg.Model,
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	for _, m := range req.Messages {
		msg := chatMessageJSON{Role: m.Role, Content: m.Content, Name: m.Name, ToolCallID: m.ToolCallID}
		for _, call := range m.ToolCalls {
			tc := chatToolCall{ID: call.ID, Type: "function"}
			tc.Function.Name, tc.Function.Arguments = call.Name, call.Args
			msg.ToolCalls = append(msg.ToolCalls, tc)
		}
		body.Messages = append(body.Messages, msg)
	}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, toolJSON{
			Type: "function",
			Function: map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Schema,
			},
		})
	}
	httpReq, err := p.request(ctx, "/chat/completions", body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, errors.New("provider " + p.cfg.ID + ": " + resp.Status + ": " + string(snippet))
	}

	out := make(chan Delta, 32)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		calls := map[int]*ToolCall{}
		send := func(d Delta) bool {
			select {
			case out <- d:
				return true
			case <-ctx.Done():
				return false
			}
		}
		flush := func() {
			indices := make([]int, 0, len(calls))
			for index := range calls {
				indices = append(indices, index)
			}
			sort.Ints(indices)
			for _, index := range indices {
				if !send(Delta{ToolCall: calls[index]}) {
					return
				}
			}
			send(Delta{Done: true})
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				flush()
				return
			}
			var chunk chatChunkJSON
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				continue // tolerate keep-alive comments and vendor extensions
			}
			d := Delta{}
			if chunk.Error != nil {
				message := chunk.Error.Message
				if message == "" {
					message = "upstream streaming error"
				}
				send(Delta{Err: errors.New("provider " + p.cfg.ID + ": " + message), Done: true})
				return
			}
			if chunk.Usage != nil {
				d.Usage = &Usage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens}
			}
			for _, c := range chunk.Choices {
				if c.Delta.Content != "" {
					d.Text += c.Delta.Content
				}
				for _, tc := range c.Delta.ToolCalls {
					call := calls[tc.Index]
					if call == nil {
						call = &ToolCall{}
						calls[tc.Index] = call
					}
					call.ID += tc.ID
					call.Name += tc.Function.Name
					call.Args += tc.Function.Arguments
				}
			}
			if d.Text != "" || d.ToolCall != nil || d.Usage != nil {
				select {
				case out <- d:
				case <-ctx.Done():
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			send(Delta{Err: err, Done: true})
			return
		}
		flush()
	}()
	return out, nil
}

type OpenAIEmbeddings struct {
	openAIBase
	dim int
}

func NewOpenAIEmbeddings(c model.ProviderConfig, apiKey string) *OpenAIEmbeddings {
	return &OpenAIEmbeddings{openAIBase{cfg: c, apiKey: apiKey, http: &http.Client{Timeout: 2 * time.Minute}}, 0}
}

func (p *OpenAIEmbeddings) Info() Info {
	return Info{ID: p.cfg.ID, Kind: model.KindEmbeddings, Driver: "openai", Model: p.cfg.Model, Local: p.cfg.Local}
}

func (p *OpenAIEmbeddings) Dimensions() int { return p.dim }

func (p *OpenAIEmbeddings) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	req, err := p.request(ctx, "/embeddings", map[string]any{
		"model": p.cfg.Model,
		"input": texts,
	})
	if err != nil {
		return nil, err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, errors.New("provider " + p.cfg.ID + ": " + resp.Status + ": " + string(snippet))
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	out := make([][]float32, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		out = append(out, d.Embedding)
	}
	if len(out) > 0 {
		p.dim = len(out[0])
	}
	return out, nil
}
