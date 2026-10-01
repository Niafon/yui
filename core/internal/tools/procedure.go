package tools

import (
	"context"

	"github.com/yui-companion/core/internal/model"
)

type ProcedureLearner interface {
	LearnProcedure(context.Context, string, string, string) (string, error)
}

func RegisterProcedureLearning(r *Registry, mem ProcedureLearner) error {
	str := map[string]any{"type": "string"}
	return r.Register(Tool{
		Name: "memory.learn_procedure", Description: "Сохранить согласованный с владельцем порядок действий для похожих задач. Не сохраняй пароли и ключи; правило не заменяет разрешения на действия.",
		Schema:   map[string]any{"type": "object", "properties": map[string]any{"trigger": str, "steps": str}},
		Required: []string{"trigger", "steps"}, Risk: model.RiskMedium, Category: model.CatPreferences,
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return mem.LearnProcedure(ctx, identityFrom(ctx), asString(args["trigger"]), asString(args["steps"]))
		},
	})
}
