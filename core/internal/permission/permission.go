// Package permission implements the policy engine described in SRS 16.3 and
// SEC-003. Every decision is made here, in code — never inside a system
// prompt — so it can be tested and audited (SRS 9.1).
package permission

import (
	"context"
	"sync"
	"time"

	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

// Request is one authorisation question.
type Request struct {
	SubjectKind model.SubjectKind
	SubjectID   string
	Category    model.Category
	Action      model.Action
	// Provider is set when Action is transmit, so the engine can tell a local
	// endpoint from a remote one.
	Provider *model.ProviderConfig
}

// Result carries the decision plus the reason, which goes into the audit log.
type Result struct {
	Decision model.Decision
	Reason   string
	GrantID  string
}

// Pending is an unresolved "ask" waiting for the owner (SEC-004).
type Pending struct {
	ID        string                   `json:"id"`
	Request   Request                  `json:"-"`
	Subject   string                   `json:"subject"`
	Category  model.Category           `json:"category"`
	Action    model.Action             `json:"action"`
	Provider  string                   `json:"provider,omitempty"`
	Method    model.ConfirmationMethod `json:"method"`
	CreatedAt time.Time                `json:"created_at"`
}

// alwaysSensitive never leaves the machine without an explicit owner grant,
// regardless of provider defaults (SRS 16.8, R-05).
var alwaysSensitive = map[model.Category]bool{
	model.CatMedical:   true,
	model.CatBiometric: true,
	model.CatContacts:  true,
	model.CatRawAudio:  true,
	model.CatFiles:     true,
	model.CatAudit:     true,
}

type Engine struct {
	repo store.PermissionRepo

	mu           sync.RWMutex
	localAllowed map[model.Category]bool
	pending      map[string]*Pending
	once         map[requestKey]time.Time
}

type requestKey struct {
	subjectKind model.SubjectKind
	subjectID   string
	category    model.Category
	action      model.Action
	providerID  string
}

func keyFor(req Request) requestKey {
	key := requestKey{subjectKind: req.SubjectKind, subjectID: req.SubjectID, category: req.Category, action: req.Action}
	if req.Provider != nil {
		key.providerID = req.Provider.ID
	}
	return key
}

func New(repo store.PermissionRepo, localAllowed []model.Category) *Engine {
	m := make(map[model.Category]bool, len(localAllowed))
	for _, c := range localAllowed {
		m[c] = true
	}
	return &Engine{repo: repo, localAllowed: m, pending: map[string]*Pending{}, once: map[requestKey]time.Time{}}
}

// Check answers a single request. Order: explicit grant, provider allow-list,
// built-in defaults. Absence of a rule is never "allow" for outbound data.
func (e *Engine) Check(ctx context.Context, req Request) (Result, error) {
	grants, err := e.repo.List(ctx, req.SubjectKind, req.SubjectID)
	if err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	for _, g := range grants {
		if g.Category != req.Category || g.Action != req.Action {
			continue
		}
		if g.ExpiresAt != nil && g.ExpiresAt.Before(now) {
			continue
		}
		return Result{Decision: g.Decision, Reason: "explicit grant", GrantID: g.ID}, nil
	}
	// A one-time approval is consumed atomically by one operation, never saved
	// as a permanent grant. Explicit persisted decisions above still win.
	e.mu.Lock()
	key := keyFor(req)
	expires, approved := e.once[key]
	delete(e.once, key)
	e.mu.Unlock()
	if approved && now.Before(expires) {
		return Result{Decision: model.DecisionAllow, Reason: "one-time owner approval"}, nil
	}
	return e.defaultDecision(req), nil
}

func (e *Engine) defaultDecision(req Request) Result {
	if req.Action == model.ActionTransmit {
		p := req.Provider
		if p == nil {
			return Result{Decision: model.DecisionDeny, Reason: "transmit without provider context"}
		}
		for _, c := range p.Allowed {
			if c == req.Category {
				return Result{Decision: model.DecisionAllow, Reason: "provider allow-list"}
			}
		}
		if alwaysSensitive[req.Category] {
			return Result{Decision: model.DecisionAsk, Reason: "sensitive category requires explicit consent"}
		}
		if p.Local {
			e.mu.RLock()
			ok := e.localAllowed[req.Category]
			e.mu.RUnlock()
			if ok {
				return Result{Decision: model.DecisionAllow, Reason: "local provider default"}
			}
			return Result{Decision: model.DecisionAsk, Reason: "category not in local defaults"}
		}
		// Remote provider, no rule: block and ask (SEC-004).
		return Result{Decision: model.DecisionAsk, Reason: "first transfer of this category to a remote provider"}
	}

	// Local reads and writes by an identity are permitted except for the
	// protected categories, which require a grant.
	if alwaysSensitive[req.Category] {
		return Result{Decision: model.DecisionAsk, Reason: "protected category"}
	}
	switch req.Action {
	case model.ActionRead, model.ActionAppend, model.ActionUpdate:
		return Result{Decision: model.DecisionAllow, Reason: "default local access"}
	case model.ActionDelete, model.ActionExport, model.ActionInvoke:
		return Result{Decision: model.DecisionAsk, Reason: "action requires confirmation"}
	}
	return Result{Decision: model.DecisionDeny, Reason: "no rule"}
}

// Grant stores an owner decision so it is not asked again (SRS 16.4).
func (e *Engine) Grant(ctx context.Context, g *model.Grant) error {
	if g.ID == "" {
		g.ID = ids.New("grant")
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now().UTC()
	}
	return e.repo.Put(ctx, g)
}

func (e *Engine) Revoke(ctx context.Context, id string) error { return e.repo.Delete(ctx, id) }

func (e *Engine) List(ctx context.Context) ([]*model.Grant, error) { return e.repo.All(ctx) }

// RecordPending registers an unresolved ask so the UI can surface it.
func (e *Engine) RecordPending(req Request, method model.ConfirmationMethod) *Pending {
	p := &Pending{
		ID:        ids.New("ask"),
		Request:   req,
		Subject:   string(req.SubjectKind) + ":" + req.SubjectID,
		Category:  req.Category,
		Action:    req.Action,
		Method:    method,
		CreatedAt: time.Now().UTC(),
	}
	if req.Provider != nil {
		p.Provider = req.Provider.ID
	}
	e.mu.Lock()
	e.pending[p.ID] = p
	e.mu.Unlock()
	return p
}

func (e *Engine) Pending() []*Pending {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*Pending, 0, len(e.pending))
	for _, p := range e.pending {
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// Resolve applies the owner's answer to a pending request and remembers it.
func (e *Engine) Resolve(ctx context.Context, pendingID string, allow bool, remember bool) error {
	e.mu.Lock()
	p, ok := e.pending[pendingID]
	if ok {
		delete(e.pending, pendingID)
	}
	e.mu.Unlock()
	if !ok {
		return store.ErrNotFound
	}
	if !remember {
		if allow {
			e.mu.Lock()
			for key, expires := range e.once {
				if time.Now().After(expires) {
					delete(e.once, key)
				}
			}
			e.once[keyFor(p.Request)] = time.Now().Add(5 * time.Minute)
			e.mu.Unlock()
		}
		return nil
	}
	decision := model.DecisionDeny
	if allow {
		decision = model.DecisionAllow
	}
	return e.Grant(ctx, &model.Grant{
		SubjectKind: p.Request.SubjectKind,
		SubjectID:   p.Request.SubjectID,
		Category:    p.Category,
		Action:      p.Action,
		Decision:    decision,
	})
}

// ConfirmationFor maps a risk level to the required factor (SRS 16.5).
// A voice match is never sufficient for high risk actions (SEC-002, R-07).
func ConfirmationFor(risk model.RiskLevel) model.ConfirmationMethod {
	switch risk {
	case model.RiskLow:
		return model.ConfirmVoice
	case model.RiskMedium:
		return model.ConfirmButton
	case model.RiskHigh:
		return model.ConfirmBiometric
	case model.RiskCritical:
		return model.ConfirmPIN
	}
	return model.ConfirmButton
}
