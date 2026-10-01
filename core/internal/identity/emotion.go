package identity

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

// Emotional state is per identity instance and survives restarts and device
// changes (PER-004, AC-12). It is a small vector plus a label, not free text,
// so the avatar, the voice and the ranking can all read it.

func NeutralState(identityID string) *model.EmotionalState {
	return &model.EmotionalState{
		IdentityID: identityID, Valence: 0.1, Arousal: 0.3, Dominance: 0.5,
		Label: "neutral", UpdatedAt: time.Now().UTC(), Version: 1,
	}
}

func (s *Service) Emotion(ctx context.Context, identityID string) (*model.EmotionalState, error) {
	st, err := s.repo.GetEmotion(ctx, identityID)
	if err == store.ErrNotFound {
		st = NeutralState(identityID)
		if err := s.repo.PutEmotion(ctx, st); err != nil {
			return nil, err
		}
		return st, nil
	}
	return st, err
}

// Delta is a bounded change applied to the emotional state.
type Delta struct {
	Valence   float64
	Arousal   float64
	Dominance float64
	Cause     string
}

// Apply moves the state towards the delta and decays it back to baseline over
// time, so a single event does not pin the mood forever.
func (s *Service) Apply(ctx context.Context, identityID string, d Delta) (*model.EmotionalState, error) {
	st, err := s.Emotion(ctx, identityID)
	if err != nil {
		return nil, err
	}
	elapsed := time.Since(st.UpdatedAt).Hours()
	decay := math.Pow(0.5, elapsed/2.0) // 2 hour half-life towards baseline
	st.Valence = clamp(-1, 1, baseline(st.Valence, 0.1, decay)+d.Valence)
	st.Arousal = clamp(0, 1, baseline(st.Arousal, 0.3, decay)+d.Arousal)
	st.Dominance = clamp(0, 1, baseline(st.Dominance, 0.5, decay)+d.Dominance)
	st.Label = Label(st.Valence, st.Arousal)
	st.Cause = d.Cause
	st.UpdatedAt = time.Now().UTC()
	st.Version++
	if err := s.repo.PutEmotion(ctx, st); err != nil {
		return nil, err
	}
	return st, nil
}

func baseline(current, base, decay float64) float64 {
	return base + (current-base)*decay
}

func clamp(min, max, v float64) float64 {
	return math.Max(min, math.Min(max, v))
}

// Label maps the vector to the expression names the avatar understands.
func Label(valence, arousal float64) string {
	switch {
	case valence > 0.45 && arousal > 0.55:
		return "joy"
	case valence > 0.3:
		return "warm"
	case valence < -0.45 && arousal > 0.55:
		return "concern"
	case valence < -0.3:
		return "sad"
	case arousal > 0.7:
		return "alert"
	case arousal < 0.2:
		return "calm"
	}
	return "neutral"
}

// Appraise derives a small emotional delta from a user message. It is a cheap
// lexicon pass; a model-based appraisal can replace it behind the same call.
func Appraise(text string) Delta {
	lower := strings.ToLower(text)
	d := Delta{Cause: "dialogue"}
	positive := []string{"спасибо", "отлично", "рад", "люблю", "круто", "получилось", "thanks", "great"}
	negative := []string{"плохо", "устал", "злюсь", "болит", "ошибка", "не работает", "грустно", "tired", "sad"}
	urgent := []string{"срочно", "быстрее", "опасно", "помоги", "urgent", "help"}
	for _, w := range positive {
		if strings.Contains(lower, w) {
			d.Valence += 0.12
			d.Arousal += 0.04
		}
	}
	for _, w := range negative {
		if strings.Contains(lower, w) {
			d.Valence -= 0.14
			d.Arousal += 0.06
		}
	}
	for _, w := range urgent {
		if strings.Contains(lower, w) {
			d.Arousal += 0.12
		}
	}
	return d
}

// ExpressionFor resolves the avatar expression id for the current state.
func ExpressionFor(it *model.Identity, st *model.EmotionalState) string {
	if st == nil || it == nil {
		return "exp_neutral"
	}
	if id, ok := it.Presentation.ExpressionMap[st.Label]; ok {
		return id
	}
	// Existing identities may predate new labels in the default presentation.
	switch st.Label {
	case "warm":
		return "exp_smile"
	case "sad", "angry":
		return "exp_worry"
	case "alert":
		return "exp_surprise"
	case "think":
		return "exp_think"
	}
	return "exp_neutral"
}
