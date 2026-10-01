// Package provider defines the stable contracts every model backend must
// satisfy (SRS 2.2 "Replaceable providers", AI-001). The core never imports a
// model library: it talks to these interfaces only.
package provider

import (
	"context"
	"errors"

	"github.com/yui-companion/core/internal/model"
)

var (
	ErrNotConfigured = errors.New("provider: not configured")
	ErrUnsupported   = errors.New("provider: capability not supported")
	ErrDuplicate     = errors.New("provider: duplicate id")
)

// Info is what the core knows about a provider without calling it (AI-006).
type Info struct {
	ID            string             `json:"id"`
	Kind          model.ProviderKind `json:"kind"`
	Driver        string             `json:"driver"`
	Model         string             `json:"model,omitempty"`
	Local         bool               `json:"local"`
	Streaming     bool               `json:"streaming"`
	Tools         bool               `json:"tools"`
	Vision        bool               `json:"vision"`
	ContextTokens int                `json:"context_tokens,omitempty"`
}

type Message struct {
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Role       string     `json:"role"` // system|user|assistant|tool
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
}

// ToolSpec is a JSON-schema described tool the model may request (AI-008).
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
}

type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"arguments"`
}

type ChatRequest struct {
	Messages    []Message
	Tools       []ToolSpec
	Temperature float64
	MaxTokens   int
	TraceID     string
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Delta is one streamed piece of a reply. Streaming is mandatory so that TTS
// can start before the text is complete (VOICE-007).
type Delta struct {
	Text     string
	ToolCall *ToolCall
	Usage    *Usage
	Done     bool
	Err      error
}

type LLM interface {
	Info() Info
	Chat(ctx context.Context, req ChatRequest) (<-chan Delta, error)
}

type TranscribeRequest struct {
	SessionID  string
	Audio      []byte
	SampleRate int
	Channels   int
	Format     string // pcm16|wav|opus
	Language   string
	Final      bool
}

type Transcript struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	Final      bool    `json:"final"`
	Language   string  `json:"language,omitempty"`
	// SpeakerIsOwner is reported when the worker performs owner verification
	// (VOICE-005). Nil means "unknown".
	SpeakerIsOwner *bool `json:"speaker_is_owner,omitempty"`
}

type STT interface {
	Info() Info
	Transcribe(ctx context.Context, req TranscribeRequest) (Transcript, error)
}

type SpeakRequest struct {
	Text    string
	Voice   string
	Emotion string
	Speed   float64
	Format  string
}

type AudioChunk struct {
	Data       []byte
	SampleRate int
	Seq        int
	Final      bool
	Err        error
}

type TTS interface {
	Info() Info
	// Synthesize streams audio; the returned channel closes when finished.
	// Cancelling ctx must stop synthesis (VOICE-011, the barge-in seam).
	Synthesize(ctx context.Context, req SpeakRequest) (<-chan AudioChunk, error)
}

type VisionRequest struct {
	Image    []byte
	MIME     string
	Question string
	WantOCR  bool
}

type VisionResult struct {
	Description string   `json:"description"`
	Objects     []string `json:"objects,omitempty"`
	Text        string   `json:"ocr_text,omitempty"`
	Confidence  float64  `json:"confidence"`
}

type Vision interface {
	Info() Info
	Analyze(ctx context.Context, req VisionRequest) (VisionResult, error)
}

type Embeddings interface {
	Info() Info
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
}

// TaskEmbeddings is optional. Instruction-aware models such as Qwen3 can use
// a retrieval prompt for queries while storing documents without that prompt.
type TaskEmbeddings interface {
	EmbedFor(ctx context.Context, texts []string, purpose string) ([][]float32, error)
}

const (
	EmbeddingQuery    = "query"
	EmbeddingDocument = "document"
)
