// Package model contains the domain entities described in SRS Appendix B.
// It has no dependencies on other internal packages so that every service can
// share the same vocabulary without import cycles.
package model

import "time"

// ---------------------------------------------------------------------------
// Categories, sensitivity and provenance (SRS 10.5, 10.6, 16.3)
// ---------------------------------------------------------------------------

// Category is a data category. Permissions and provider data policies are
// expressed in terms of categories, never in terms of physical tables.
type Category string

const (
	CatCurrentText    Category = "current_text"
	CatSessionSummary Category = "session_summary"
	CatConversation   Category = "conversation"
	CatProfile        Category = "profile"
	CatPreferences    Category = "preferences"
	CatProjects       Category = "projects"
	CatCalendar       Category = "calendar"
	CatContacts       Category = "contacts"
	CatPlaces         Category = "places"
	CatMedical        Category = "medical"
	CatBiometric      Category = "biometric"
	CatFiles          Category = "files"
	CatVision         Category = "vision"
	CatRawAudio       Category = "raw_audio"
	CatEmotions       Category = "emotions"
	CatPersonality    Category = "personality"
	CatActions        Category = "actions"
	CatAudit          Category = "audit"
)

// AllCategories is used by the permission UI and by policy defaults.
func AllCategories() []Category {
	return []Category{
		CatCurrentText, CatSessionSummary, CatConversation, CatProfile,
		CatPreferences, CatProjects, CatCalendar, CatContacts, CatPlaces,
		CatMedical, CatBiometric, CatFiles, CatVision, CatRawAudio,
		CatEmotions, CatPersonality, CatActions, CatAudit,
	}
}

// Sensitivity drives retention, encryption and outbound policy (SRS 16.8).
type Sensitivity string

const (
	SensNormal    Sensitivity = "normal"
	SensPrivate   Sensitivity = "private"
	SensSensitive Sensitivity = "sensitive"
	SensMedical   Sensitivity = "medical"
	SensBiometric Sensitivity = "biometric"
)

// Provenance records where a memory came from (SRS 10.6).
type Provenance struct {
	Kind     string    `json:"kind"` // conversation|vision|external|inference|user
	Ref      string    `json:"ref"`  // event id, turn id, media id
	DeviceID string    `json:"device_id,omitempty"`
	At       time.Time `json:"at"`
}

// ---------------------------------------------------------------------------
// Memory (SRS 10)
// ---------------------------------------------------------------------------

type MemoryType string

const (
	MemRawTranscript MemoryType = "raw_transcript"
	MemEpisodic      MemoryType = "episodic"
	MemSemanticFact  MemoryType = "semantic_fact"
	MemProcedure     MemoryType = "procedure"
	MemSummary       MemoryType = "summary"
	MemFrame         MemoryType = "frame"
	MemVideoSegment  MemoryType = "video_segment"
	MemArchive       MemoryType = "archive"
)

// MemoryStatus is the truthfulness state of a record (SRS 10.6).
type MemoryStatus string

const (
	StatusConfirmed         MemoryStatus = "confirmed"
	StatusExtracted         MemoryStatus = "extracted"
	StatusObserved          MemoryStatus = "observed"
	StatusExternal          MemoryStatus = "external"
	StatusInferred          MemoryStatus = "inferred"
	StatusAssumption        MemoryStatus = "assumption"
	StatusNeedsConfirmation MemoryStatus = "needs_confirmation"
	StatusOutdated          MemoryStatus = "outdated"
	StatusDisputed          MemoryStatus = "disputed"
	StatusSuperseded        MemoryStatus = "superseded"
)

// Active reports whether the status may be used as a current fact.
func (s MemoryStatus) Active() bool {
	switch s {
	case StatusOutdated, StatusSuperseded, StatusDisputed:
		return false
	}
	return true
}

// MemorySpace separates shared user stores from per-identity stores (SRS 10.1).
type MemorySpace struct {
	ID          string      `json:"id"`
	OwnerType   string      `json:"owner_type"` // user|identity
	OwnerID     string      `json:"owner_id"`
	Name        string      `json:"name"`
	Category    Category    `json:"category"`
	Sensitivity Sensitivity `json:"sensitivity"`
}

// MemoryItem is one structured memory (SRS 10.5).
type MemoryItem struct {
	ID           string       `json:"id"`
	SpaceID      string       `json:"space_id"`
	IdentityID   string       `json:"identity_id,omitempty"`
	Version      int          `json:"version"`
	Type         MemoryType   `json:"type"`
	Category     Category     `json:"category"`
	Subject      string       `json:"subject,omitempty"` // normalized key, e.g. "user.name"
	Content      string       `json:"content"`
	Confidence   float64      `json:"confidence"`
	Importance   float64      `json:"importance"`
	Sensitivity  Sensitivity  `json:"sensitivity"`
	Status       MemoryStatus `json:"status"`
	OccurredAt   time.Time    `json:"occurred_at"`
	RecordedAt   time.Time    `json:"recorded_at"`
	ExpiresAt    *time.Time   `json:"expires_at,omitempty"`
	Pinned       bool         `json:"pinned"`
	Provenance   []Provenance `json:"provenance,omitempty"`
	Links        []string     `json:"links,omitempty"`
	Embedding    []float32    `json:"-"`
	SupersededBy string       `json:"superseded_by,omitempty"`
	DeletedAt    *time.Time   `json:"deleted_at,omitempty"`
}

// MemoryVersion keeps the change history required by MEM-009.
type MemoryVersion struct {
	MemoryID   string       `json:"memory_id"`
	Version    int          `json:"version"`
	Content    string       `json:"content"`
	Status     MemoryStatus `json:"status"`
	ReplacedBy string       `json:"replaced_by,omitempty"`
	ValidFrom  time.Time    `json:"valid_from"`
	ValidTo    *time.Time   `json:"valid_to,omitempty"`
}

// MemoryCandidate is produced by the extractor before policy is applied
// (contract C.3).
type MemoryCandidate struct {
	IdentityID           string       `json:"identity_id"`
	SpaceID              string       `json:"memory_space_id"`
	Type                 MemoryType   `json:"type"`
	Category             Category     `json:"category"`
	Subject              string       `json:"subject"`
	NormalizedContent    string       `json:"normalized_content"`
	Confidence           float64      `json:"confidence"`
	Importance           float64      `json:"importance"`
	Sensitivity          Sensitivity  `json:"sensitivity"`
	Provenance           []Provenance `json:"provenance"`
	RequiresConfirmation bool         `json:"requires_confirmation"`
}

// ---------------------------------------------------------------------------
// Identity, presentation and emotion (SRS 11)
// ---------------------------------------------------------------------------

type DevelopmentMode string

const (
	DevFixed   DevelopmentMode = "fixed"
	DevLimited DevelopmentMode = "limited"
	DevFree    DevelopmentMode = "free"
	DevFrozen  DevelopmentMode = "frozen"
)

// ConversationMode is the behavioural preset of a turn (SRS 11.8).
type ConversationMode string

const (
	ModeNormal   ConversationMode = "normal"
	ModeWork     ConversationMode = "work"
	ModeLearning ConversationMode = "learning"
	ModeBrief    ConversationMode = "brief"
	ModeSupport  ConversationMode = "support"
	ModePlayful  ConversationMode = "playful"
	ModePublic   ConversationMode = "public"
	ModeNight    ConversationMode = "night"
	ModeSilent   ConversationMode = "silent"
)

// Traits are bounded 0..1 character dimensions.
type Traits map[string]float64

// Identity is a persistent companion instance (PER-001).
type Identity struct {
	ID              string           `json:"id"`
	UserID          string           `json:"user_id"`
	Name            string           `json:"name"`
	Pronouns        string           `json:"pronouns,omitempty"`
	AgeImage        int              `json:"age_image"` // presented age, 18+ enforced
	StyleImage      string           `json:"style_image,omitempty"`
	SpeechStyle     string           `json:"speech_style,omitempty"`
	Relationship    string           `json:"relationship,omitempty"`
	Traits          Traits           `json:"traits"`
	Initiative      float64          `json:"initiative"`
	Autonomy        float64          `json:"autonomy"`
	DevelopmentMode DevelopmentMode  `json:"development_mode"`
	Mode            ConversationMode `json:"mode"`
	Presentation    Presentation     `json:"presentation"`
	StateVersion    int              `json:"state_version"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// Presentation is the replaceable "body": model, voice, expressions (SRS 11.4).
type Presentation struct {
	Live2DPackage string            `json:"live2d_package,omitempty"`
	VoiceProfile  string            `json:"voice_profile,omitempty"`
	ExpressionMap map[string]string `json:"expression_map,omitempty"`
}

// EmotionalState uses valence/arousal/dominance plus a display label.
type EmotionalState struct {
	IdentityID string    `json:"identity_id"`
	Valence    float64   `json:"valence"`   // -1..1
	Arousal    float64   `json:"arousal"`   // 0..1
	Dominance  float64   `json:"dominance"` // 0..1
	Label      string    `json:"label"`
	Cause      string    `json:"cause,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	Version    int       `json:"version"`
}

// ---------------------------------------------------------------------------
// Sessions and turns (SRS 3.1, CORE-003)
// ---------------------------------------------------------------------------

type SessionState string

const (
	SessionIdle      SessionState = "idle"
	SessionListening SessionState = "listening"
	SessionThinking  SessionState = "thinking"
	SessionSpeaking  SessionState = "speaking"
	SessionActing    SessionState = "acting"
	SessionError     SessionState = "error"
	SessionClosed    SessionState = "closed"
)

type Session struct {
	ID           string            `json:"id"`
	IdentityID   string            `json:"identity_id"`
	UserID       string            `json:"user_id"`
	InputDevice  string            `json:"input_device,omitempty"`
	OutputDevice string            `json:"output_device,omitempty"`
	Mode         ConversationMode  `json:"mode"`
	State        SessionState      `json:"state"`
	Providers    map[string]string `json:"providers,omitempty"` // llm|stt|tts|vision|embeddings -> provider id
	Summary      string            `json:"summary,omitempty"`
	StartedAt    time.Time         `json:"started_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	ClosedAt     *time.Time        `json:"closed_at,omitempty"`
}

type Turn struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"session_id"`
	Seq         int       `json:"seq"`
	Role        string    `json:"role"` // user|assistant|system|tool
	Text        string    `json:"text"`
	Provider    string    `json:"provider,omitempty"`
	TraceID     string    `json:"trace_id,omitempty"`
	DeviceID    string    `json:"device_id,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

// ---------------------------------------------------------------------------
// Devices, permissions, audit, events (SRS 16, 18.4)
// ---------------------------------------------------------------------------

type Device struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind"` // android|desktop|bridge
	TokenHash    string     `json:"-"`
	Capabilities []string   `json:"capabilities,omitempty"`
	PairedAt     time.Time  `json:"paired_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
}

// SubjectKind identifies who is asking for access (SEC-003).
type SubjectKind string

const (
	SubjectIdentity SubjectKind = "identity"
	SubjectProvider SubjectKind = "provider"
	SubjectPlugin   SubjectKind = "plugin"
	SubjectDevice   SubjectKind = "device"
)

type Action string

const (
	ActionRead     Action = "read"
	ActionAppend   Action = "append"
	ActionUpdate   Action = "update"
	ActionDelete   Action = "delete"
	ActionExport   Action = "export"
	ActionTransmit Action = "transmit" // send outside the machine
	ActionInvoke   Action = "invoke"   // call a tool
)

type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
	DecisionAsk   Decision = "ask"
)

// Grant is a stored permission record (SEC-003).
type Grant struct {
	ID          string      `json:"id"`
	SubjectKind SubjectKind `json:"subject_kind"`
	SubjectID   string      `json:"subject_id"`
	Category    Category    `json:"category"`
	Action      Action      `json:"action"`
	Decision    Decision    `json:"decision"`
	Scope       string      `json:"scope,omitempty"`
	ExpiresAt   *time.Time  `json:"expires_at,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}

type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// ConfirmationMethod maps to SRS 16.5.
type ConfirmationMethod string

const (
	ConfirmNone      ConfirmationMethod = "none"
	ConfirmVoice     ConfirmationMethod = "voice"
	ConfirmButton    ConfirmationMethod = "button"
	ConfirmBiometric ConfirmationMethod = "biometric"
	ConfirmPIN       ConfirmationMethod = "pin"
)

// AuditRecord is append-only (SEC-007).
type AuditRecord struct {
	ID           string             `json:"id"`
	At           time.Time          `json:"at"`
	ActorKind    SubjectKind        `json:"actor_kind"`
	ActorID      string             `json:"actor_id"`
	IdentityID   string             `json:"identity_id,omitempty"`
	DeviceID     string             `json:"device_id,omitempty"`
	Action       string             `json:"action"`
	Reason       string             `json:"reason,omitempty"`
	Categories   []Category         `json:"categories,omitempty"`
	Provider     string             `json:"provider,omitempty"`
	Tool         string             `json:"tool,omitempty"`
	Permission   Decision           `json:"permission,omitempty"`
	Confirmation ConfirmationMethod `json:"confirmation,omitempty"`
	Result       string             `json:"result"`
	Error        string             `json:"error,omitempty"`
	TraceID      string             `json:"trace_id,omitempty"`
}

// Event is a raw append-only observation (SRS 5.3, 17.3).
type Event struct {
	ID            string      `json:"id"`
	At            time.Time   `json:"at"`
	Type          string      `json:"type"`
	Source        string      `json:"source"`
	SessionID     string      `json:"session_id,omitempty"`
	IdentityID    string      `json:"identity_id,omitempty"`
	DeviceID      string      `json:"device_id,omitempty"`
	CorrelationID string      `json:"correlation_id,omitempty"`
	Sensitivity   Sensitivity `json:"sensitivity,omitempty"`
	Payload       string      `json:"payload,omitempty"`
}

// ---------------------------------------------------------------------------
// Providers (SRS 9.2, AI-005)
// ---------------------------------------------------------------------------

// ProviderKind is the role a provider fills; each role is configured
// independently (AI-002).
type ProviderKind string

const (
	KindLLM        ProviderKind = "llm"
	KindComputer   ProviderKind = "computer"
	KindSTT        ProviderKind = "stt"
	KindTTS        ProviderKind = "tts"
	KindVision     ProviderKind = "vision"
	KindEmbeddings ProviderKind = "embeddings"
)

// ProviderConfig describes one configured model endpoint.
type ProviderConfig struct {
	ID        string       `json:"id"`
	Kind      ProviderKind `json:"kind"`
	Driver    string       `json:"driver"`            // openai|worker|mock
	Service   string       `json:"service,omitempty"` // ollama|lm-studio|openrouter|openai|custom
	Endpoint  string       `json:"endpoint,omitempty"`
	Model     string       `json:"model,omitempty"`
	APIKeyEnv string       `json:"api_key_env,omitempty"`
	Local     bool         `json:"local"`

	// Inference metadata is deliberately descriptive: the scheduler never
	// guesses VRAM use or whether a provider is CPU/GPU backed. Multiple
	// providers may share ModelFamily, which lets a manual model lock keep the
	// exact model while the runtime moves it between CPU/GPU/hybrid backends.
	ModelFamily      string   `json:"model_family,omitempty"`
	ExecutionBackend string   `json:"execution_backend,omitempty"` // auto|cpu|gpu|hybrid
	Quality          int      `json:"quality,omitempty"`           // relative 1..100
	EstimatedRAMMB   int      `json:"estimated_ram_mb,omitempty"`
	EstimatedVRAMMB  int      `json:"estimated_vram_mb,omitempty"`
	GamingSafe       bool     `json:"gaming_safe,omitempty"`
	AutoSelect       bool     `json:"auto_select,omitempty"`
	KeepWarm         bool     `json:"keep_warm,omitempty"`
	WorkerID         string   `json:"worker_id,omitempty"` // managed process in Supervisor
	Tags             []string `json:"tags,omitempty"`

	// Allowed lists categories this provider may ever receive. Empty means
	// "local defaults" for local providers and "nothing" for remote ones.
	Allowed []Category `json:"allowed_categories,omitempty"`
}
