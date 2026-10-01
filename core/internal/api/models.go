package api

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/provider"
)

type modelEntry struct {
	ID              string             `json:"id"`
	Kind            model.ProviderKind `json:"kind"`
	Service         string             `json:"service"`
	Driver          string             `json:"driver"`
	Endpoint        string             `json:"endpoint,omitempty"`
	Model           string             `json:"model,omitempty"`
	ModelFamily     string             `json:"model_family,omitempty"`
	Local           bool               `json:"local"`
	Backend         string             `json:"backend,omitempty"`
	Quality         int                `json:"quality,omitempty"`
	RAMMB           int                `json:"estimated_ram_mb,omitempty"`
	VRAMMB          int                `json:"estimated_vram_mb,omitempty"`
	AutoSelect      bool               `json:"auto_select"`
	Tags            []string           `json:"tags,omitempty"`
	IsDefault       bool               `json:"is_default"`
	CredentialReady bool               `json:"credential_ready"`
	APIKeyEnv       string             `json:"api_key_env,omitempty"`
	UserAdded       bool               `json:"user_added"`
	Voice           string             `json:"voice,omitempty"`
}

func (s *Server) modelEntries() []modelEntry {
	added := map[string]bool{}
	if s.deps.ModelSettings != nil {
		for _, c := range s.deps.ModelSettings.Snapshot().Providers {
			added[c.ID] = true
		}
	}
	out := make([]modelEntry, 0)
	for _, c := range s.deps.Providers.Configs() {
		family := c.ModelFamily
		if family == "" {
			family = c.Model
		}
		out = append(out, modelEntry{
			ID: c.ID, Kind: c.Kind, Service: modelService(c), Driver: c.Driver,
			Endpoint: c.Endpoint, Model: c.Model, ModelFamily: family, Local: c.Local,
			Backend: c.ExecutionBackend, Quality: c.Quality,
			RAMMB: c.EstimatedRAMMB, VRAMMB: c.EstimatedVRAMMB,
			AutoSelect: c.AutoSelect, Tags: c.Tags, IsDefault: s.deps.Providers.Default(c.Kind) == c.ID,
			CredentialReady: s.deps.Providers.CredentialReady(c.ID),
			APIKeyEnv:       c.APIKeyEnv, UserAdded: added[c.ID], Voice: c.Voice,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Local != out[j].Local {
			return out[i].Local
		}
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		return out[i].Model < out[j].Model
	})
	return out
}

func modelService(c model.ProviderConfig) string {
	if c.Service != "" {
		return c.Service
	}
	u, err := url.Parse(c.Endpoint)
	if err == nil {
		host := strings.ToLower(u.Hostname())
		switch {
		case host == "openrouter.ai":
			return "openrouter"
		case host == "api.openai.com":
			return "openai"
		case host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback():
			if u.Port() == "11434" {
				return "ollama"
			}
			return "local"
		case host != "":
			return host
		}
	}
	if c.Driver == "worker" {
		return "yui-worker"
	}
	return c.Driver
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.modelEntries())
}

type addModelRequest struct {
	Kind      model.ProviderKind `json:"kind"`
	Service   string             `json:"service"`
	Source    string             `json:"source"`
	Endpoint  string             `json:"endpoint"`
	Model     string             `json:"model"`
	APIKeyEnv string             `json:"api_key_env"`
	// Voice is used by speech models only.
	Voice string `json:"voice"`
}

// Default model ids for speech services when the owner leaves the field empty.
var defaultSpeechModels = map[string]string{
	"openai": "gpt-4o-mini-tts", "elevenlabs": "eleven_multilingual_v2", "azure": "azure-neural",
}

var envName = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]{0,79}$")

func (b addModelRequest) config() (model.ProviderConfig, error) {
	b.Service = strings.TrimSpace(b.Service)
	b.Source = strings.TrimSpace(b.Source)
	b.Endpoint = strings.TrimSpace(b.Endpoint)
	b.Model = strings.TrimSpace(b.Model)
	b.APIKeyEnv = strings.TrimSpace(b.APIKeyEnv)
	b.Voice = strings.TrimSpace(b.Voice)
	if b.Kind != model.KindLLM && b.Kind != model.KindVision && b.Kind != model.KindEmbeddings && b.Kind != model.KindTTS {
		return model.ProviderConfig{}, errors.New("supported kinds: llm, vision, embeddings, tts")
	}
	speechOnly := b.Service == "elevenlabs" || b.Service == "azure"
	if speechOnly && b.Kind != model.KindTTS {
		return model.ProviderConfig{}, errors.New(b.Service + " provides speech synthesis only")
	}
	if len(b.Voice) > 100 || strings.ContainsAny(b.Voice, "\r\n<>\"&") {
		return model.ProviderConfig{}, errors.New("voice must be a plain id up to 100 characters")
	}
	if b.Kind == model.KindTTS && b.Model == "" {
		b.Model = defaultSpeechModels[b.Service]
		if b.Model == "" {
			b.Model = "tts-1"
		}
	}
	if len(b.Model) == 0 || len(b.Model) > 200 || strings.ContainsAny(b.Model, "\r\n") {
		return model.ProviderConfig{}, errors.New("model id must contain 1-200 characters")
	}
	if len(b.Endpoint) == 0 || len(b.Endpoint) > 400 {
		return model.ProviderConfig{}, errors.New("endpoint is required")
	}
	u, err := url.Parse(b.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return model.ProviderConfig{}, errors.New("endpoint must be an HTTP(S) base URL without credentials or query")
	}
	local := b.Source == "local"
	if !local && b.Source != "provider" {
		return model.ProviderConfig{}, errors.New("source must be local or provider")
	}
	if local {
		ip := net.ParseIP(u.Hostname())
		if (u.Scheme != "http" && u.Scheme != "https") || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return model.ProviderConfig{}, errors.New("local model endpoint must use loopback HTTP(S)")
		}
		switch b.Service {
		case "ollama", "lm-studio", "llama.cpp", "kokoro", "custom":
		default:
			return model.ProviderConfig{}, errors.New("unsupported local service")
		}
	} else {
		if u.Scheme != "https" || u.Hostname() == "" {
			return model.ProviderConfig{}, errors.New("provider endpoint must use HTTPS")
		}
		switch b.Service {
		case "openrouter":
			if !strings.EqualFold(u.Hostname(), "openrouter.ai") {
				return model.ProviderConfig{}, errors.New("OpenRouter endpoint must use openrouter.ai")
			}
		case "openai":
			if !strings.EqualFold(u.Hostname(), "api.openai.com") {
				return model.ProviderConfig{}, errors.New("OpenAI endpoint must use api.openai.com")
			}
		case "elevenlabs":
			if !strings.EqualFold(u.Hostname(), "api.elevenlabs.io") {
				return model.ProviderConfig{}, errors.New("ElevenLabs endpoint must use api.elevenlabs.io")
			}
		case "azure":
			if !strings.HasSuffix(strings.ToLower(u.Hostname()), ".tts.speech.microsoft.com") {
				return model.ProviderConfig{}, errors.New("Azure endpoint must be https://<region>.tts.speech.microsoft.com/cognitiveservices/v1")
			}
		case "custom":
		default:
			return model.ProviderConfig{}, errors.New("unsupported hosted service")
		}
		if b.APIKeyEnv == "" {
			return model.ProviderConfig{}, errors.New("hosted provider requires an API key environment variable")
		}
	}
	if b.APIKeyEnv != "" && !envName.MatchString(b.APIKeyEnv) {
		return model.ProviderConfig{}, errors.New("invalid API key environment variable name")
	}
	backend := "auto"
	if !local {
		backend = "remote"
	}
	driver := "openai"
	if speechOnly {
		driver = b.Service
	}
	voice := ""
	if b.Kind == model.KindTTS {
		voice = b.Voice
	}
	return model.ProviderConfig{
		ID: ids.New("mdl"), Kind: b.Kind, Driver: driver, Service: b.Service,
		Endpoint: strings.TrimRight(b.Endpoint, "/"), Model: b.Model, Voice: voice,
		ModelFamily: b.Model, APIKeyEnv: b.APIKeyEnv, Local: local,
		ExecutionBackend: backend, AutoSelect: false,
	}, nil
}

func (s *Server) handleModelAdd(w http.ResponseWriter, r *http.Request) {
	if !callerFrom(r.Context()).Loopback {
		writeError(w, http.StatusForbidden, "models can be added only from the desktop")
		return
	}
	if s.deps.ModelSettings == nil {
		writeError(w, http.StatusNotImplemented, "model settings storage unavailable")
		return
	}
	var body addModelRequest
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := body.config()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	for _, existing := range s.deps.Providers.Configs() {
		if existing.Kind == c.Kind && existing.Endpoint == c.Endpoint && existing.Model == c.Model && existing.Voice == c.Voice {
			writeError(w, http.StatusConflict, "this model is already configured")
			return
		}
	}
	if err := s.deps.ModelSettings.AddProvider(c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.deps.Providers.Register(c); err != nil {
		_ = s.deps.ModelSettings.RemoveProvider(c.ID)
		code := http.StatusInternalServerError
		if errors.Is(err, provider.ErrDuplicate) || errors.Is(err, provider.ErrUnsupported) {
			code = http.StatusConflict
		}
		writeError(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": c.ID, "models": s.modelEntries()})
}
