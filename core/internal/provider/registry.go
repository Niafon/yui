package provider

import (
	"os"
	"strings"
	"sync"

	"github.com/yui-companion/core/internal/model"
)

// Registry owns every configured provider and the current default per kind.
// Switching a default must not touch the identity or the session (AC-08).
type Registry struct {
	mu          sync.RWMutex
	configs     map[string]model.ProviderConfig
	llm         map[string]LLM
	stt         map[string]STT
	tts         map[string]TTS
	vision      map[string]Vision
	embed       map[string]Embeddings
	defaults    map[model.ProviderKind]string
	credentials map[string]bool
}

func NewRegistry() *Registry {
	return &Registry{
		configs:     map[string]model.ProviderConfig{},
		llm:         map[string]LLM{},
		stt:         map[string]STT{},
		tts:         map[string]TTS{},
		vision:      map[string]Vision{},
		embed:       map[string]Embeddings{},
		defaults:    map[model.ProviderKind]string{},
		credentials: map[string]bool{},
	}
}

// Build instantiates providers from configuration. Unknown drivers are an
// error at startup rather than a silent fallback (AI-007).
func (r *Registry) Build(cfgs []model.ProviderConfig, defaults map[string]string) error {
	for _, c := range cfgs {
		if err := r.Register(c); err != nil {
			return err
		}
	}
	for kind, id := range defaults {
		if id == "" {
			continue
		}
		if err := r.SetDefault(model.ProviderKind(kind), id); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) Register(c model.ProviderConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(c.ID) == "" {
		return ErrNotConfigured
	}
	if _, exists := r.configs[c.ID]; exists {
		return ErrDuplicate
	}
	// Computer is a tool capability owned by the dedicated worker bridge in
	// internal/tools, not an LLM/STT/TTS provider interface. Keep its metadata
	// available to inference and configuration validation, but do not try to
	// materialise it in one of the ordinary provider maps.
	if c.Kind == model.KindComputer {
		r.configs[c.ID] = c
		r.credentials[c.ID] = c.APIKeyEnv == "" || strings.TrimSpace(os.Getenv(c.APIKeyEnv)) != ""
		return nil
	}
	apiKey := ""
	if c.APIKeyEnv != "" {
		apiKey = os.Getenv(c.APIKeyEnv)
	}
	switch c.Driver {
	case "mock":
		switch c.Kind {
		case model.KindLLM:
			r.llm[c.ID] = NewMockLLM(c)
		case model.KindSTT:
			r.stt[c.ID] = NewMockSTT(c)
		case model.KindTTS:
			r.tts[c.ID] = NewMockTTS(c)
		case model.KindVision:
			r.vision[c.ID] = NewMockVision(c)
		case model.KindEmbeddings:
			r.embed[c.ID] = NewHashEmbeddings(c, 256)
		default:
			return ErrUnsupported
		}
	case "openai":
		switch c.Kind {
		case model.KindComputer:
			// The official Fara harness owns its multimodal request protocol.
		case model.KindLLM:
			r.llm[c.ID] = NewOpenAILLM(c, apiKey)
		case model.KindEmbeddings:
			r.embed[c.ID] = NewOpenAIEmbeddings(c, apiKey)
		case model.KindVision:
			// Vision through the multimodal chat endpoint — usually the same
			// model that is already resident (ADR-036).
			r.vision[c.ID] = NewOpenAIVision(c, apiKey)
		default:
			return ErrUnsupported
		}
	case "worker":
		w := NewWorkerClient(c)
		switch c.Kind {
		case model.KindSTT:
			r.stt[c.ID] = w
		case model.KindTTS:
			r.tts[c.ID] = w
		case model.KindVision:
			r.vision[c.ID] = w
		case model.KindEmbeddings:
			r.embed[c.ID] = w
		default:
			return ErrUnsupported
		}
	default:
		return ErrNotConfigured
	}
	r.configs[c.ID] = c
	r.credentials[c.ID] = c.APIKeyEnv == "" || strings.TrimSpace(apiKey) != ""
	return nil
}

func (r *Registry) CredentialReady(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.credentials[id]
}

func (r *Registry) SetDefault(kind model.ProviderKind, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.configs[id]
	if !ok || c.Kind != kind {
		return ErrNotConfigured
	}
	r.defaults[kind] = id
	return nil
}

func (r *Registry) Default(kind model.ProviderKind) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaults[kind]
}

// Config returns the configuration used by the permission engine to decide
// whether a call is local or remote.
func (r *Registry) Config(id string) (model.ProviderConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.configs[id]
	return c, ok
}

func (r *Registry) Configs() []model.ProviderConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.ProviderConfig, 0, len(r.configs))
	for _, c := range r.configs {
		out = append(out, c)
	}
	return out
}

func (r *Registry) LLM(id string) (LLM, model.ProviderConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaults[model.KindLLM]
	}
	p, ok := r.llm[id]
	if !ok {
		return nil, model.ProviderConfig{}, ErrNotConfigured
	}
	return p, r.configs[id], nil
}

func (r *Registry) STT(id string) (STT, model.ProviderConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaults[model.KindSTT]
	}
	p, ok := r.stt[id]
	if !ok {
		return nil, model.ProviderConfig{}, ErrNotConfigured
	}
	return p, r.configs[id], nil
}

func (r *Registry) TTS(id string) (TTS, model.ProviderConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaults[model.KindTTS]
	}
	p, ok := r.tts[id]
	if !ok {
		return nil, model.ProviderConfig{}, ErrNotConfigured
	}
	return p, r.configs[id], nil
}

func (r *Registry) Vision(id string) (Vision, model.ProviderConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaults[model.KindVision]
	}
	p, ok := r.vision[id]
	if !ok {
		return nil, model.ProviderConfig{}, ErrNotConfigured
	}
	return p, r.configs[id], nil
}

func (r *Registry) Embeddings(id string) (Embeddings, model.ProviderConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaults[model.KindEmbeddings]
	}
	p, ok := r.embed[id]
	if !ok {
		return nil, model.ProviderConfig{}, ErrNotConfigured
	}
	return p, r.configs[id], nil
}

// Status is reported to the UI so a failure is visible instead of silently
// falling back to the cloud (AI-007, NFR-010).
type Status struct {
	Info      Info   `json:"info"`
	IsDefault bool   `json:"is_default"`
	Healthy   bool   `json:"healthy"`
	Detail    string `json:"detail,omitempty"`
}

func (r *Registry) Status() []Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []Status{}
	add := func(info Info) {
		ready := r.credentials[info.ID]
		detail := ""
		if !ready {
			detail = "API key environment variable is not set"
		}
		out = append(out, Status{
			Info:      info,
			IsDefault: r.defaults[info.Kind] == info.ID,
			Healthy:   ready,
			Detail:    detail,
		})
	}
	for _, p := range r.llm {
		add(p.Info())
	}
	for _, p := range r.stt {
		add(p.Info())
	}
	for _, p := range r.tts {
		add(p.Info())
	}
	for _, p := range r.vision {
		add(p.Info())
	}
	for _, p := range r.embed {
		add(p.Info())
	}
	return out
}
