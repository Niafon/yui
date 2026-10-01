// Package tools implements the tool layer from SRS 9.5 and ADR-026.
//
// Every invocation passes the same gate, in this order: schema validation,
// capability, permission, risk assessment, confirmation, execution, audit.
// A tool that cannot state its risk level cannot be registered — there is no
// default of "low".
//
// What is deliberately absent is as important as what is here: no sending
// messages as the owner, no calls, no payments, no file deletion, no arbitrary
// OS commands, and no emergency services (SEC-013). The model cannot invoke
// what does not exist.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/model"
)

var (
	ErrUnknownTool     = errors.New("tools: unknown tool")
	ErrInvalidArgs     = errors.New("tools: arguments do not match the schema")
	ErrDenied          = errors.New("tools: denied by permission policy")
	ErrNeedsConfirm    = errors.New("tools: owner confirmation required")
	ErrNotImplemented  = errors.New("tools: capability not available on this system")
)

// Handler executes a validated invocation and returns a short, human readable
// result that goes back to the model.
type Handler func(ctx context.Context, args map[string]any) (string, error)

// Tool is one callable capability.
type Tool struct {
	Name        string
	Description string
	// Schema is a JSON Schema object describing the arguments.
	Schema map[string]any
	// Required argument names, checked before the handler runs.
	Required []string
	Risk     model.RiskLevel
	// Category is the data category the call touches, used by the permission
	// engine. A tool that reads the calendar is not the same subject as one
	// that reads contacts.
	Category model.Category
	// Capability is the plugin capability this tool needs; empty means the
	// tool is built into the core.
	Capability string
	Handler    Handler
}

// Registry holds the tools available to a personality.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

func (r *Registry) Register(t Tool) error {
	if t.Name == "" || t.Handler == nil {
		return errors.New("tools: name and handler are required")
	}
	if t.Risk == "" {
		return errors.New("tools: " + t.Name + " must declare a risk level")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name] = t
	return nil
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Validate checks the arguments against the declared schema. It is intentionally
// strict: unknown fields are rejected rather than ignored, because a model that
// invents an argument is a model that misunderstood the tool (AI-008).
func (t Tool) Validate(raw string) (map[string]any, error) {
	args := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return nil, ErrInvalidArgs
		}
	}
	props, _ := t.Schema["properties"].(map[string]any)
	for name := range args {
		if props != nil {
			if _, ok := props[name]; !ok {
				return nil, errors.New("tools: unexpected argument " + name)
			}
		}
	}
	for _, name := range t.Required {
		v, ok := args[name]
		if !ok {
			return nil, errors.New("tools: missing argument " + name)
		}
		if s, isString := v.(string); isString && strings.TrimSpace(s) == "" {
			return nil, errors.New("tools: argument " + name + " is empty")
		}
	}
	return args, nil
}

// Invocation is a pending or completed tool call, kept for audit and for the
// confirmation round trip (contract C.5).
type Invocation struct {
	ID           string                   `json:"invocation_id"`
	Tool         string                   `json:"tool"`
	Args         map[string]any           `json:"-"`
	Risk         model.RiskLevel          `json:"risk"`
	Description  string                   `json:"human_readable_action"`
	Categories   []model.Category         `json:"data_categories"`
	Method       model.ConfirmationMethod `json:"required_method"`
	IdentityID   string                   `json:"identity_id"`
	CreatedAt    time.Time                `json:"created_at"`
	ExpiresAt    time.Time                `json:"expires_at"`
}

// Pending holds invocations waiting for the owner. They expire: an unanswered
// confirmation must not stay executable an hour later.
type Pending struct {
	mu    sync.Mutex
	items map[string]*Invocation
	ttl   time.Duration
}

func NewPending() *Pending {
	return &Pending{items: map[string]*Invocation{}, ttl: 2 * time.Minute}
}

func (p *Pending) Add(inv *Invocation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expireLocked()
	inv.ExpiresAt = time.Now().Add(p.ttl)
	p.items[inv.ID] = inv
}

func (p *Pending) Take(id string) (*Invocation, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expireLocked()
	inv, ok := p.items[id]
	if ok {
		delete(p.items, id)
	}
	return inv, ok
}

func (p *Pending) List() []*Invocation {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expireLocked()
	out := make([]*Invocation, 0, len(p.items))
	for _, inv := range p.items {
		out = append(out, inv)
	}
	return out
}

func (p *Pending) expireLocked() {
	now := time.Now()
	for id, inv := range p.items {
		if now.After(inv.ExpiresAt) {
			delete(p.items, id)
		}
	}
}

// NewInvocation builds a pending invocation record.
func NewInvocation(t Tool, args map[string]any, identityID string, method model.ConfirmationMethod) *Invocation {
	return &Invocation{
		ID:          ids.New("inv"),
		Tool:        t.Name,
		Args:        args,
		Risk:        t.Risk,
		Description: Describe(t, args),
		Categories:  []model.Category{t.Category},
		Method:      method,
		IdentityID:  identityID,
		CreatedAt:   time.Now().UTC(),
	}
}

// Describe renders what the owner is being asked to approve. The confirmation
// prompt must describe the action, not the function name (SRS 16.5).
func Describe(t Tool, args map[string]any) string {
	b := &strings.Builder{}
	b.WriteString(t.Description)
	if len(args) == 0 {
		return b.String()
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString(" (")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(stringify(args[k]))
	}
	b.WriteString(")")
	return b.String()
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strings.TrimRight(strings.TrimRight(formatFloat(t), "0"), ".")
	case bool:
		if t {
			return "да"
		}
		return "нет"
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return "?"
		}
		return string(raw)
	}
}

func formatFloat(f float64) string {
	raw, err := json.Marshal(f)
	if err != nil {
		return "0"
	}
	return string(raw)
}
