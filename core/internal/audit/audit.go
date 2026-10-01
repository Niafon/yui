// Package audit writes the append-only local trail required by SEC-007.
// Secrets are never passed in: callers reference providers and tools by id.
package audit

import (
	"context"
	"time"

	"github.com/yui-companion/core/internal/eventbus"
	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/logging"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

const TopicAudit = "audit"

type Service struct {
	repo store.AuditRepo
	bus  *eventbus.Bus
}

func New(repo store.AuditRepo, bus *eventbus.Bus) *Service {
	return &Service{repo: repo, bus: bus}
}

// Record persists one audit entry. Failures are logged, never propagated into
// the voice path: losing an audit write must not break a conversation, but it
// must be visible.
func (s *Service) Record(ctx context.Context, rec model.AuditRecord) {
	if rec.ID == "" {
		rec.ID = ids.New("aud")
	}
	if rec.At.IsZero() {
		rec.At = time.Now().UTC()
	}
	if rec.TraceID == "" {
		rec.TraceID = logging.TraceID(ctx)
	}
	if err := s.repo.Append(ctx, &rec); err != nil {
		logging.From(ctx).Error("audit write failed", "action", rec.Action, "error", err)
		return
	}
	if s.bus != nil {
		s.bus.Publish(TopicAudit, model.Event{
			ID: rec.ID, At: rec.At, Type: "audit." + rec.Action,
			Source: string(rec.ActorKind), IdentityID: rec.IdentityID,
			DeviceID: rec.DeviceID, CorrelationID: rec.TraceID,
		})
	}
}

// ProviderCall records an outbound model call and the categories it carried
// (AI-012). The manifest, not the payload, is what gets stored.
func (s *Service) ProviderCall(ctx context.Context, identityID, provider, purpose string, cats []model.Category, decision model.Decision, result string, err error) {
	rec := model.AuditRecord{
		ActorKind:  model.SubjectProvider,
		ActorID:    provider,
		IdentityID: identityID,
		Action:     "provider.call",
		Reason:     purpose,
		Categories: cats,
		Provider:   provider,
		Permission: decision,
		Result:     result,
	}
	if err != nil {
		rec.Error = err.Error()
		rec.Result = "error"
	}
	s.Record(ctx, rec)
}

// ToolCall records a tool invocation with its confirmation method (AI-009/010).
func (s *Service) ToolCall(ctx context.Context, identityID, tool string, decision model.Decision, method model.ConfirmationMethod, result string, err error) {
	rec := model.AuditRecord{
		ActorKind:    model.SubjectIdentity,
		ActorID:      identityID,
		IdentityID:   identityID,
		Action:       "tool.invoke",
		Tool:         tool,
		Permission:   decision,
		Confirmation: method,
		Result:       result,
	}
	if err != nil {
		rec.Error = err.Error()
		rec.Result = "error"
	}
	s.Record(ctx, rec)
}

// MemoryChange records creation, correction and deletion of memories.
func (s *Service) MemoryChange(ctx context.Context, identityID, action, memoryID string, cat model.Category) {
	s.Record(ctx, model.AuditRecord{
		ActorKind:  model.SubjectIdentity,
		ActorID:    identityID,
		IdentityID: identityID,
		Action:     action,
		Reason:     memoryID,
		Categories: []model.Category{cat},
		Result:     "ok",
	})
}

func (s *Service) List(ctx context.Context, q store.AuditQuery) ([]*model.AuditRecord, error) {
	return s.repo.List(ctx, q)
}
