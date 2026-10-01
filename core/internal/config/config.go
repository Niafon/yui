// Package config loads core configuration. A missing file is not an error:
// the defaults start a fully local, mock-provider system so a fresh checkout
// runs without any model installed (NFR-007).
package config

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yui-companion/core/internal/model"
)

// WorkerSpec describes an external process the supervisor owns (CORE-007).
type WorkerSpec struct {
	ID        string            `json:"id"`
	Command   string            `json:"command"`
	Args      []string          `json:"args,omitempty"`
	Dir       string            `json:"dir,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	HealthURL string            `json:"health_url,omitempty"`
	Autostart bool              `json:"autostart"`
	Restart   bool              `json:"restart"`
}

type ServerConfig struct {
	Addr      string `json:"addr"`
	StageDir  string `json:"stage_dir,omitempty"`
	LocalAddr string `json:"local_addr,omitempty"`
	// LoopbackToken authorises the desktop stage on 127.0.0.1. Devices use
	// pairing tokens instead (CL-004).
	LoopbackToken string `json:"loopback_token,omitempty"`
	// RequireTLSForRemote rejects non-loopback plaintext clients (SEC-005).
	RequireTLSForRemote bool   `json:"require_tls_for_remote"`
	TLSCertFile         string `json:"tls_cert_file,omitempty"`
	TLSKeyFile          string `json:"tls_key_file,omitempty"`
	// AllowedOrigins adds browser origins (beyond the Tauri shell and the
	// Vite dev server) that may read core responses, e.g. a custom stage host.
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
}

type DatabaseConfig struct {
	// SQLite is the default production store: one local file, no daemon.
	// PostgreSQL remains readable as a legacy/large-deployment backend.
	Driver string `json:"driver"`
	Path   string `json:"path,omitempty"`
	DSN    string `json:"dsn,omitempty"`
}

type MemoryConfig struct {
	ContextTokens        int     `json:"context_tokens"`
	SmallContextTokens   int     `json:"small_context_tokens"`
	SmallRetrievalLimit  int     `json:"small_retrieval_limit"`
	RawTranscriptTTLDays int     `json:"raw_transcript_ttl_days"`
	FrameTTLDays         int     `json:"frame_ttl_days"`
	TrashRetentionDays   int     `json:"trash_retention_days"`
	MediaQuotaMB         int     `json:"media_quota_mb"`
	RetrievalLimit       int     `json:"retrieval_limit"`
	MinConfirmConfidence float64 `json:"min_confirm_confidence"`
}

type PrivacyConfig struct {
	// AllowRemoteByDefault must stay false: a new category is never sent to a
	// remote provider before the owner decides (SEC-004).
	AllowRemoteByDefault bool `json:"allow_remote_by_default"`
	// LocalAllowedCategories are the categories local providers may read.
	LocalAllowedCategories []model.Category `json:"local_allowed_categories,omitempty"`
}

type SchedulerConfig struct {
	Enabled           bool `json:"enabled"`
	TickSeconds       int  `json:"tick_seconds"`
	MaxParallelJobs   int  `json:"max_parallel_jobs"`
	PauseOnActiveTurn bool `json:"pause_on_active_turn"`
}

// WakeWordConfig implements ADR-020: the owner picks where detection runs.
//
//	off    — button or an open session only
//	device — detector on the phone; only speech after activation reaches the PC
//	core   — the phone streams continuously and the PC detects
type WakeWordConfig struct {
	Mode        string  `json:"mode"`
	Phrase      string  `json:"phrase"`
	Sensitivity float64 `json:"sensitivity"`
}

// BargeInConfig implements ADR-021.
//
//	off | button | wake_word | any_speech
//
// any_speech requires echo cancellation on the client, otherwise the companion
// interrupts itself. MinSpeechMs additionally ignores triggers fired right
// after synthesis starts.
type BargeInConfig struct {
	Mode                    string `json:"mode"`
	MinSpeechMs             int    `json:"min_speech_ms"`
	RequireEchoCancellation bool   `json:"require_echo_cancellation"`
}

type VoiceConfig struct {
	WakeWord WakeWordConfig `json:"wake_word"`
	BargeIn  BargeInConfig  `json:"barge_in"`
	// SpeakerVerification stays off until a model exists (ADR-022).
	SpeakerVerification bool `json:"speaker_verification"`
}

// VisionConfig implements ADR-028: several event modes, because the right
// threshold is a measurement, not a guess.
//
//	off | manual | motion | periodic | smart
type VisionConfig struct {
	EventMode          string  `json:"event_mode"`
	MinIntervalMs      int     `json:"min_interval_ms"`
	MotionThreshold    float64 `json:"motion_threshold"`
	UnloadAfterSeconds int     `json:"unload_after_seconds"`
}

// RemoteConfig implements ADR-029: lan | vpn | relay.
// relay is declared but refused at startup until a zero-knowledge design
// exists — a visible refusal beats the appearance of protection.
type RemoteConfig struct {
	Mode     string `json:"mode"`
	RelayURL string `json:"relay_url,omitempty"`
}

// CryptoConfig implements ADR-024/ADR-025. Argon2id parameters live in the key
// file header too, so they can be raised later without breaking old files.
type CryptoConfig struct {
	Enabled        bool   `json:"enabled"`
	KeyFile        string `json:"key_file"`
	Argon2Time     uint32 `json:"argon2_time"`
	Argon2MemoryMB uint32 `json:"argon2_memory_mb"`
	Argon2Threads  uint8  `json:"argon2_threads"`
	// EncryptSensitiveOnly limits field encryption to sensitive, medical and
	// biometric records; media is always encrypted.
	EncryptSensitiveOnly bool `json:"encrypt_sensitive_only"`
}

// DiagnosticsConfig controls the loopback-only debug surface (ADR-034).
// Off by default: profiles of a personal assistant describe the owner's
// activity, so they are opt-in even on localhost.

type InferenceConfig struct {
	Jev                  JevConfig `json:"jev"`
	Enabled              bool      `json:"enabled"`
	Mode                 string    `json:"mode"` // auto|max_quality|balanced|gaming|manual
	PreferredProvider    string    `json:"preferred_provider,omitempty"`
	LockedModel          string    `json:"locked_model,omitempty"`
	AllowAutoDowngrade   bool      `json:"allow_auto_downgrade"`
	MinimumQuality       int       `json:"minimum_quality"`
	MaxGPUUtilPercent    float64   `json:"max_gpu_util_percent"`
	MaxCPUUtilPercent    float64   `json:"max_cpu_util_percent"`
	VRAMReserveMB        int       `json:"vram_reserve_mb"`
	MaxFPSImpactPercent  float64   `json:"max_fps_impact_percent"`
	PollIntervalMS       int       `json:"poll_interval_ms"`
	WarmupTimeoutSeconds int       `json:"warmup_timeout_seconds"`
	GamingProcesses      []string  `json:"gaming_processes,omitempty"`
}

// Jev only chooses among eligible local models. It never executes tools.
type JevConfig struct {
	Enabled          bool    `json:"enabled"`
	Endpoint         string  `json:"endpoint"`
	Model            string  `json:"model"`
	APIKeyEnv        string  `json:"api_key_env"`
	TimeoutMS        int     `json:"timeout_ms"`
	MinConfidence    float64 `json:"min_confidence"`
	AllowCurrentText bool    `json:"allow_current_text"`
}

type ComputerConfig struct {
	Enabled       bool   `json:"enabled"`
	Endpoint      string `json:"endpoint"`
	WorkerID      string `json:"worker_id"`
	ModelWorkerID string `json:"model_worker_id"`
}

type WebSearchConfig struct {
	Enabled bool `json:"enabled"`
}

type DiagnosticsConfig struct {
	Enabled bool `json:"enabled"`
	// LeakCheckMinutes is how often the goroutine leak profile is sampled.
	LeakCheckMinutes int `json:"leak_check_minutes"`
}

type Config struct {
	Computer  ComputerConfig  `json:"computer"`
	WebSearch WebSearchConfig `json:"web_search"`
	DataDir   string          `json:"data_dir"`
	Server    ServerConfig    `json:"server"`
	Database  DatabaseConfig  `json:"database"`
	Logging   struct {
		Level string `json:"level"`
		JSON  bool   `json:"json"`
	} `json:"logging"`
	Providers   []model.ProviderConfig `json:"providers"`
	Defaults    map[string]string      `json:"default_providers"`
	Workers     []WorkerSpec           `json:"workers"`
	Memory      MemoryConfig           `json:"memory"`
	Privacy     PrivacyConfig          `json:"privacy"`
	Scheduler   SchedulerConfig        `json:"scheduler"`
	Voice       VoiceConfig            `json:"voice"`
	Vision      VisionConfig           `json:"vision"`
	Remote      RemoteConfig           `json:"remote"`
	Crypto      CryptoConfig           `json:"crypto"`
	Diagnostics DiagnosticsConfig      `json:"diagnostics"`
	Inference   InferenceConfig        `json:"inference"`
}

// Default returns a runnable offline configuration.
func Default() *Config {
	c := &Config{
		DataDir: "./data",
		Server: ServerConfig{
			Addr:                "127.0.0.1:8765",
			RequireTLSForRemote: true,
		},
		// SQLite is embedded in yui-core: no database service, no socket hop and
		// no ANN index to maintain for a personal-sized memory corpus (ADR-037).
		Database: DatabaseConfig{
			Driver: "sqlite",
			Path:   "./data/yui.db",
		},
		Providers: []model.ProviderConfig{
			{ID: "mock-llm", Kind: model.KindLLM, Driver: "mock", Local: true},
			{ID: "mock-stt", Kind: model.KindSTT, Driver: "mock", Local: true},
			{ID: "mock-tts", Kind: model.KindTTS, Driver: "mock", Local: true},
			{ID: "mock-vision", Kind: model.KindVision, Driver: "mock", Local: true},
			{ID: "mock-embeddings", Kind: model.KindEmbeddings, Driver: "mock", Local: true},
		},
		Defaults: map[string]string{
			"llm":        "mock-llm",
			"stt":        "mock-stt",
			"tts":        "mock-tts",
			"vision":     "mock-vision",
			"embeddings": "mock-embeddings",
		},
		Memory: MemoryConfig{
			ContextTokens: 1200, SmallContextTokens: 600, SmallRetrievalLimit: 4,
			RawTranscriptTTLDays: 14,
			FrameTTLDays:         7,
			TrashRetentionDays:   30,
			MediaQuotaMB:         20480,
			RetrievalLimit:       12,
			MinConfirmConfidence: 0.55,
		},
		Privacy: PrivacyConfig{
			AllowRemoteByDefault: false,
			LocalAllowedCategories: []model.Category{
				model.CatCurrentText, model.CatSessionSummary, model.CatConversation,
				model.CatProfile, model.CatPreferences, model.CatProjects,
				model.CatCalendar, model.CatPlaces, model.CatVision,
				model.CatEmotions, model.CatPersonality,
			},
		},
		Scheduler: SchedulerConfig{
			Enabled: true, TickSeconds: 20, MaxParallelJobs: 1, PauseOnActiveTurn: true,
		},
		Voice: VoiceConfig{
			// Phone-side detection by default: battery costs more than the
			// hundred milliseconds it saves (ADR-020).
			WakeWord: WakeWordConfig{Mode: "device", Phrase: "юи", Sensitivity: 0.5},
			// Button by default: it is the only mode that works correctly
			// without echo cancellation (ADR-021).
			BargeIn:             BargeInConfig{Mode: "button", MinSpeechMs: 350, RequireEchoCancellation: true},
			SpeakerVerification: false,
		},
		Vision: VisionConfig{
			EventMode: "manual", MinIntervalMs: 4000, MotionThreshold: 0.12,
			// Vision and the LLM do not fit in 10 GB together (ADR-016).
			UnloadAfterSeconds: 120,
		},
		Remote:      RemoteConfig{Mode: "lan"},
		Diagnostics: DiagnosticsConfig{Enabled: false, LeakCheckMinutes: 30},
		Inference: InferenceConfig{
			Jev:     JevConfig{Endpoint: "https://openrouter.ai/api/alpha/decisions", Model: "~typesafe/jev-latest", APIKeyEnv: "OPENROUTER_API_KEY", TimeoutMS: 2500, MinConfidence: 0.8},
			Enabled: true, Mode: "auto", AllowAutoDowngrade: true, MinimumQuality: 55,
			MaxGPUUtilPercent: 82, MaxCPUUtilPercent: 88, VRAMReserveMB: 3072,
			MaxFPSImpactPercent: 2, PollIntervalMS: 3000, WarmupTimeoutSeconds: 45,
			GamingProcesses: []string{"dota2.exe", "cs2.exe", "csgo.exe", "robloxplayerbeta.exe", "deadlock.exe"},
		},
		Crypto: CryptoConfig{
			Enabled: true, KeyFile: "./data/yui.key",
			Argon2Time: 3, Argon2MemoryMB: 64, Argon2Threads: 4,
			EncryptSensitiveOnly: true,
		},
	}
	c.Logging.Level = "info"
	return c
}

// Load reads a JSON config, applies environment overrides and validates.
// Relative durable-data paths are resolved from the config file directory,
// not from the caller's current working directory. This keeps scripts, Make
// targets and a packaged desktop binary pointed at the same yui.db.
func Load(path string) (*Config, error) {
	cfg := Default()
	baseDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if path != "" {
		absConfig, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		baseDir = filepath.Dir(absConfig)
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := json.Unmarshal(b, cfg); err != nil {
				return nil, err
			}
		case errors.Is(err, os.ErrNotExist):
			// keep defaults
		default:
			return nil, err
		}
	}
	applyEnv(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.DataDir = resolvePath(baseDir, cfg.DataDir)
	if cfg.Server.StageDir != "" {
		cfg.Server.StageDir = resolvePath(baseDir, cfg.Server.StageDir)
	}
	if cfg.Server.TLSCertFile != "" {
		cfg.Server.TLSCertFile = resolvePath(baseDir, cfg.Server.TLSCertFile)
	}
	if cfg.Server.TLSKeyFile != "" {
		cfg.Server.TLSKeyFile = resolvePath(baseDir, cfg.Server.TLSKeyFile)
	}
	if cfg.Database.Driver == "sqlite" {
		cfg.Database.Path = resolvePath(baseDir, cfg.Database.Path)
	}
	if cfg.Crypto.KeyFile != "" {
		cfg.Crypto.KeyFile = resolvePath(baseDir, cfg.Crypto.KeyFile)
	}
	return cfg, nil
}

func resolvePath(baseDir, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

func applyEnv(c *Config) {
	if v := os.Getenv("YUI_ADDR"); v != "" {
		c.Server.Addr = v
	}
	if v := os.Getenv("YUI_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("YUI_DB_DRIVER"); v != "" {
		c.Database.Driver = v
	}
	if v := os.Getenv("YUI_DB_PATH"); v != "" {
		c.Database.Path = v
	}
	if v := os.Getenv("YUI_DB_DSN"); v != "" {
		c.Database.DSN = v
	}
	if v := os.Getenv("YUI_LOG_LEVEL"); v != "" {
		c.Logging.Level = v
	}
	if v := os.Getenv("YUI_LOOPBACK_TOKEN"); v != "" {
		c.Server.LoopbackToken = v
	}
	if v := os.Getenv("YUI_SCHEDULER"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Scheduler.Enabled = b
		}
	}
}

func (c *Config) Validate() error {
	if c.Computer.Enabled {
		u, err := url.Parse(c.Computer.Endpoint)
		if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") || u.User != nil || !c.Inference.Enabled || c.Computer.WorkerID == "" || c.Computer.ModelWorkerID == "" {
			return errors.New("config: computer requires a loopback HTTP worker, worker_id and inference.enabled")
		}
	}
	if c.Inference.Jev.Enabled {
		j := c.Inference.Jev
		if j.TimeoutMS < 100 || j.TimeoutMS > 30000 || j.MinConfidence < 0.5 || j.MinConfidence > 1 || j.APIKeyEnv == "" || j.Model == "" {
			return errors.New("config: invalid Jev timeout, confidence, model or API key environment variable")
		}
		if !strings.HasPrefix(j.Endpoint, "https://") {
			return errors.New("config: Jev endpoint must use HTTPS")
		}
	}
	if c.Server.LocalAddr != "" {
		host, _, err := net.SplitHostPort(c.Server.LocalAddr)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return errors.New("config: server.local_addr must use a loopback IP address")
		}
	}
	if strings.TrimSpace(c.Server.Addr) == "" {
		return errors.New("config: server.addr is empty")
	}
	if c.Inference.Enabled {
		switch c.Inference.Mode {
		case "auto", "max_quality", "balanced", "gaming", "manual":
		default:
			return errors.New("config: inference.mode must be auto|max_quality|balanced|gaming|manual")
		}
		if c.Inference.MinimumQuality < 1 || c.Inference.MinimumQuality > 100 {
			return errors.New("config: inference.minimum_quality must be between 1 and 100")
		}
	}
	switch c.Database.Driver {
	case "memory", "sqlite", "postgres":
	default:
		return errors.New("config: database.driver must be sqlite, memory or postgres")
	}
	if c.Database.Driver == "sqlite" && strings.TrimSpace(c.Database.Path) == "" {
		c.Database.Path = filepath.Join(c.DataDir, "yui.db")
	}
	if c.Database.Driver == "postgres" && c.Database.DSN == "" {
		return errors.New("config: database.dsn required for postgres")
	}
	seen := map[string]bool{}
	for _, p := range c.Providers {
		if p.ID == "" {
			return errors.New("config: provider without id")
		}
		if seen[p.ID] {
			return errors.New("config: duplicate provider id " + p.ID)
		}
		seen[p.ID] = true
	}
	for kind, id := range c.Defaults {
		if id != "" && !seen[id] {
			return errors.New("config: default provider for " + kind + " not declared: " + id)
		}
	}
	if c.Memory.RetrievalLimit <= 0 {
		c.Memory.RetrievalLimit = 12
	}
	if err := oneOf("voice.wake_word.mode", c.Voice.WakeWord.Mode, "off", "device", "core"); err != nil {
		return err
	}
	if err := oneOf("voice.barge_in.mode", c.Voice.BargeIn.Mode, "off", "button", "wake_word", "any_speech"); err != nil {
		return err
	}
	if err := oneOf("vision.event_mode", c.Vision.EventMode, "off", "manual", "motion", "periodic", "smart"); err != nil {
		return err
	}
	if err := oneOf("remote.mode", c.Remote.Mode, "lan", "vpn", "relay"); err != nil {
		return err
	}
	if c.Remote.Mode == "relay" {
		// ADR-029: refuse rather than pretend. A relay needs a zero-knowledge
		// design and its own threat model before it can carry a voice session.
		return errors.New("config: remote.mode=relay is not implemented yet; use lan or vpn")
	}
	if c.Crypto.Enabled {
		if c.Crypto.KeyFile == "" {
			return errors.New("config: crypto.key_file is required when encryption is enabled")
		}
		if c.Crypto.Argon2MemoryMB < 32 {
			return errors.New("config: crypto.argon2_memory_mb must be at least 32")
		}
	}
	return nil
}

func oneOf(field, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return errors.New("config: " + field + " must be one of " + strings.Join(allowed, ", ") + ", got " + strconv.Quote(value))
}

// ProviderByID finds a configured provider.
func (c *Config) ProviderByID(id string) (model.ProviderConfig, bool) {
	for _, p := range c.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return model.ProviderConfig{}, false
}
