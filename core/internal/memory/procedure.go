package memory

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/yui-companion/core/internal/model"
)

// LearnProcedure is called only after the owner confirms a proposed workflow.
// An LLM's successful-looking output alone cannot establish a verified rule.
func (s *Service) LearnProcedure(ctx context.Context, identityID, trigger, steps string) (string, error) {
	trigger, steps = strings.TrimSpace(trigger), strings.TrimSpace(steps)
	if trigger == "" || steps == "" || len([]rune(trigger)) > 300 || len([]rune(steps)) > 1600 {
		return "", fmt.Errorf("memory: procedure requires a short trigger and steps")
	}
	sum := sha256.Sum256([]byte(strings.ToLower(trigger)))
	it, err := s.Remember(ctx, model.MemoryCandidate{
		IdentityID: identityID, SpaceID: EpisodicSpace(identityID), Type: model.MemProcedure,
		Category: model.CatPreferences, Subject: fmt.Sprintf("procedure.%x", sum[:12]),
		NormalizedContent: "Когда: " + trigger + ". Согласованный порядок: " + steps,
		Confidence:        1, Importance: .8, Sensitivity: model.SensPrivate,
		Provenance: []model.Provenance{{Kind: "user", Ref: "confirmed:memory.learn_procedure", At: time.Now().UTC()}},
	})
	if err != nil {
		return "", err
	}
	return "Сохранён порядок действий: " + it.ID, nil
}
