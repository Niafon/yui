package agent

import (
	"context"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/permission"
	"github.com/yui-companion/core/internal/provider"
	"github.com/yui-companion/core/internal/session"
	"github.com/yui-companion/core/internal/tools"
)

// The gate every tool call passes (SRS 9.5, AI-008…AI-010):
//
//	schema → capability → permission → risk → confirmation → execute → audit
//
// The model never reaches a handler directly, and a refusal is returned to the
// model as text so it can explain itself instead of silently doing nothing.

// ToolSpecs renders the registry for the provider request.
func (r *Runtime) ToolSpecs() []provider.ToolSpec {
	if r.tools == nil {
		return nil
	}
	list := r.tools.List()
	out := make([]provider.ToolSpec, 0, len(list))
	for _, t := range list {
		schema := t.Schema
		if len(t.Required) > 0 {
			// Copy so the registry entry is not mutated.
			merged := map[string]any{}
			for k, v := range schema {
				merged[k] = v
			}
			merged["required"] = t.Required
			schema = merged
		}
		out = append(out, provider.ToolSpec{Name: t.Name, Description: t.Description, Schema: schema})
	}
	return out
}

// runToolCall validates, authorises and either executes or parks the call.
// The returned string is what the model sees next.
func (r *Runtime) runToolCall(ctx context.Context, sess *model.Session, identityID string, call *provider.ToolCall) string {
	if r.tools == nil {
		return "Инструменты не подключены."
	}
	tool, ok := r.tools.Get(call.Name)
	if !ok {
		r.audit.ToolCall(ctx, identityID, call.Name, model.DecisionDeny, model.ConfirmNone, "unknown", tools.ErrUnknownTool)
		return "Такого инструмента нет."
	}

	args, err := tool.Validate(call.Args)
	if err != nil {
		r.audit.ToolCall(ctx, identityID, tool.Name, model.DecisionDeny, model.ConfirmNone, "invalid_args", err)
		return "Аргументы не прошли проверку: " + err.Error()
	}

	res, err := r.perms.Check(ctx, permission.Request{
		SubjectKind: model.SubjectIdentity,
		SubjectID:   identityID,
		Category:    tool.Category,
		Action:      model.ActionInvoke,
	})
	if err != nil {
		return "Не удалось проверить права: " + err.Error()
	}
	if res.Decision == model.DecisionDeny {
		r.audit.ToolCall(ctx, identityID, tool.Name, model.DecisionDeny, model.ConfirmNone, "denied", nil)
		return "Это действие запрещено настройками владельца."
	}

	method := permission.ConfirmationFor(tool.Risk)
	// A voice match never authorises anything above low risk (SEC-002, R-07).
	needsOwner := tool.Risk != model.RiskLow || res.Decision == model.DecisionAsk

	if needsOwner {
		inv := tools.NewInvocation(tool, args, identityID, method)
		r.pending.Add(inv)
		r.sessions.Publish(sess.ID, session.FrameConfirm, inv)
		r.audit.ToolCall(ctx, identityID, tool.Name, res.Decision, method, "awaiting_confirmation", nil)
		return "Нужно подтверждение владельца: " + inv.Description
	}

	out, err := tool.Handler(tools.WithIdentity(ctx, identityID), args)
	if err != nil {
		r.audit.ToolCall(ctx, identityID, tool.Name, res.Decision, model.ConfirmNone, "error", err)
		return "Инструмент вернул ошибку: " + err.Error()
	}
	r.audit.ToolCall(ctx, identityID, tool.Name, res.Decision, model.ConfirmNone, "ok", nil)
	return out
}

// ConfirmTool executes a parked invocation after the owner approves it. The
// confirmation method actually used is recorded, not the one requested.
func (r *Runtime) ConfirmTool(ctx context.Context, sessionID, invocationID string, approved bool, used model.ConfirmationMethod) (string, error) {
	if r.inference != nil {
		release, err := r.inference.Acquire(ctx)
		if err != nil {
			return "", err
		}
		defer release()
	}
	inv, ok := r.pending.Take(invocationID)
	if !ok {
		return "", tools.ErrUnknownTool
	}
	if !approved {
		r.audit.ToolCall(ctx, inv.IdentityID, inv.Tool, model.DecisionDeny, used, "rejected", nil)
		return "Отменено владельцем.", nil
	}
	// A weaker factor than the risk level requires is a rejection, not a
	// warning: this is the line SEC-002 draws.
	if weaker(used, inv.Method) {
		r.audit.ToolCall(ctx, inv.IdentityID, inv.Tool, model.DecisionDeny, used, "insufficient_factor", nil)
		return "", tools.ErrNeedsConfirm
	}
	tool, ok := r.tools.Get(inv.Tool)
	if !ok {
		return "", tools.ErrUnknownTool
	}
	out, err := tool.Handler(tools.WithIdentity(ctx, inv.IdentityID), inv.Args)
	if err != nil {
		r.audit.ToolCall(ctx, inv.IdentityID, inv.Tool, model.DecisionAllow, used, "error", err)
		return "", err
	}
	r.audit.ToolCall(ctx, inv.IdentityID, inv.Tool, model.DecisionAllow, used, "ok", nil)
	if sessionID != "" {
		r.sessions.Publish(sessionID, session.FrameToolResult, map[string]any{
			"invocation_id": inv.ID, "tool": inv.Tool, "result": out,
		})
	}
	return out, nil
}

// PendingTools exposes parked invocations to the UI.
func (r *Runtime) PendingTools() []*tools.Invocation { return r.pending.List() }

// factorStrength orders confirmation methods; higher is stronger.
func factorStrength(m model.ConfirmationMethod) int {
	switch m {
	case model.ConfirmNone:
		return 0
	case model.ConfirmVoice:
		return 1
	case model.ConfirmButton:
		return 2
	case model.ConfirmPIN:
		return 3
	case model.ConfirmBiometric:
		return 4
	}
	return 0
}

func weaker(used, required model.ConfirmationMethod) bool {
	// PIN and biometric are treated as interchangeable strong factors, which
	// is what SRS 16.5 asks for; anything below them is not.
	if factorStrength(required) >= 3 {
		return factorStrength(used) < 3
	}
	return factorStrength(used) < factorStrength(required)
}
