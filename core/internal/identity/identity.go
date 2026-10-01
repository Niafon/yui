// Package identity owns personality instances: their traits, their allowed
// rate of change and the presentation profile bound to them (SRS 11).
//
// Identity and presentation are deliberately separate: swapping a Live2D model
// or a voice must not create a new personality (PER-001).
package identity

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/yui-companion/core/internal/audit"
	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

var ErrFrozen = errors.New("identity: development is frozen")

// maxTraitDelta caps how fast character may drift in limited mode (R-08).
const maxTraitDelta = 0.05

type Service struct {
	repo  store.IdentityRepo
	audit *audit.Service
}

func New(repo store.IdentityRepo, aud *audit.Service) *Service {
	return &Service{repo: repo, audit: aud}
}

// DefaultPreset is the single built-in personality required by PER-003.
func DefaultPreset(userID string) *model.Identity {
	now := time.Now().UTC()
	return &model.Identity{
		ID:           ids.New("id"),
		UserID:       userID,
		Name:         "Юи",
		Pronouns:     "she/her",
		AgeImage:     20,
		StyleImage:   "anime",
		SpeechStyle:  "тёплая, живая речь; короткие фразы в голосовом режиме",
		Relationship: "близкий компаньон владельца",
		Traits: model.Traits{
			"warmth":        0.8,
			"curiosity":     0.75,
			"playfulness":   0.6,
			"directness":    0.55,
			"assertiveness": 0.5,
			"calmness":      0.6,
		},
		Initiative:      0.4,
		Autonomy:        0.3,
		DevelopmentMode: model.DevLimited,
		Mode:            model.ModeNormal,
		Presentation: model.Presentation{
			Live2DPackage: "assets/live2d/default",
			VoiceProfile:  "default",
			ExpressionMap: map[string]string{
				"neutral": "exp_neutral", "joy": "exp_smile", "concern": "exp_worry",
				"thinking": "exp_think", "think": "exp_think", "surprise": "exp_surprise",
				"warm": "exp_smile", "sad": "exp_worry", "alert": "exp_surprise",
				"angry": "exp_worry", "calm": "exp_neutral",
			},
		},
		StateVersion: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// EnsureDefault returns the existing identity or creates the preset.
func (s *Service) EnsureDefault(ctx context.Context, userID string) (*model.Identity, error) {
	list, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		return list[0], nil
	}
	it := DefaultPreset(userID)
	if err := s.repo.Create(ctx, it); err != nil {
		return nil, err
	}
	if err := s.repo.PutEmotion(ctx, NeutralState(it.ID)); err != nil {
		return nil, err
	}
	return it, nil
}

func (s *Service) Get(ctx context.Context, id string) (*model.Identity, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]*model.Identity, error) { return s.repo.List(ctx) }

// Update applies owner edits. Trait changes are always allowed to the owner;
// it is the companion's self-development that is rate limited (see Evolve).
func (s *Service) Update(ctx context.Context, it *model.Identity) error {
	if it.AgeImage < 18 {
		return errors.New("identity: presented age must be 18 or above")
	}
	it.UpdatedAt = time.Now().UTC()
	it.StateVersion++
	if err := s.repo.Update(ctx, it); err != nil {
		return err
	}
	s.audit.Record(ctx, model.AuditRecord{
		ActorKind: model.SubjectIdentity, ActorID: it.ID, IdentityID: it.ID,
		Action: "identity.update", Result: "ok",
		Categories: []model.Category{model.CatPersonality},
	})
	return nil
}

// Evolve applies self-development within the configured mode (PER-006, PER-012).
func (s *Service) Evolve(ctx context.Context, identityID string, deltas model.Traits, cause string) (*model.Identity, error) {
	it, err := s.repo.Get(ctx, identityID)
	if err != nil {
		return nil, err
	}
	switch it.DevelopmentMode {
	case model.DevFrozen, model.DevFixed:
		return it, ErrFrozen
	}
	limit := maxTraitDelta
	if it.DevelopmentMode == model.DevFree {
		limit = maxTraitDelta * 4
	}
	changed := false
	for name, d := range deltas {
		cur, ok := it.Traits[name]
		if !ok {
			continue // unknown traits are not invented by the model
		}
		step := math.Max(-limit, math.Min(limit, d))
		next := math.Max(0, math.Min(1, cur+step))
		if next != cur {
			it.Traits[name] = next
			changed = true
		}
	}
	if !changed {
		return it, nil
	}
	it.UpdatedAt = time.Now().UTC()
	it.StateVersion++
	if err := s.repo.Update(ctx, it); err != nil {
		return nil, err
	}
	s.audit.Record(ctx, model.AuditRecord{
		ActorKind: model.SubjectIdentity, ActorID: it.ID, IdentityID: it.ID,
		Action: "identity.evolve", Reason: cause, Result: "ok",
		Categories: []model.Category{model.CatPersonality},
	})
	return it, nil
}

// Checkpoint copies an identity so the owner can experiment and roll back
// (PER-007). Memory is not copied here: that is a separate, explicit action.
func (s *Service) Checkpoint(ctx context.Context, identityID, name string) (*model.Identity, error) {
	src, err := s.repo.Get(ctx, identityID)
	if err != nil {
		return nil, err
	}
	cp := *src
	cp.ID = ids.New("id")
	cp.Name = name
	if strings.TrimSpace(name) == "" {
		cp.Name = src.Name + " (копия)"
	}
	cp.CreatedAt = time.Now().UTC()
	cp.UpdatedAt = cp.CreatedAt
	cp.StateVersion = 1
	traits := model.Traits{}
	for k, v := range src.Traits {
		traits[k] = v
	}
	cp.Traits = traits
	if err := s.repo.Create(ctx, &cp); err != nil {
		return nil, err
	}
	if st, err := s.repo.GetEmotion(ctx, src.ID); err == nil {
		clone := *st
		clone.IdentityID = cp.ID
		_ = s.repo.PutEmotion(ctx, &clone)
	}
	return &cp, nil
}

// Snapshot renders the personality for the model context. Behaviour rules live
// here as text only where they cannot be expressed as code (SRS 9.1).
func Snapshot(it *model.Identity, st *model.EmotionalState) string {
	b := &strings.Builder{}
	b.WriteString("Ты — " + it.Name + ", постоянный цифровой компаньон одного владельца.\n")
	b.WriteString("Образ: " + it.StyleImage + ", возраст образа " + itoa(it.AgeImage) + ".\n")
	if it.SpeechStyle != "" {
		b.WriteString("Стиль речи: " + it.SpeechStyle + ".\n")
	}
	if it.Relationship != "" {
		b.WriteString("Отношение к владельцу: " + it.Relationship + ".\n")
	}
	if len(it.Traits) > 0 {
		b.WriteString("Черты характера (0..1): ")
		first := true
		for _, name := range []string{"warmth", "curiosity", "playfulness", "directness", "assertiveness", "calmness"} {
			v, ok := it.Traits[name]
			if !ok {
				continue
			}
			if !first {
				b.WriteString(", ")
			}
			b.WriteString(name + "=" + ftoa(v))
			first = false
		}
		b.WriteString(".\n")
	}
	b.WriteString("Режим общения: " + string(it.Mode) + ".\n")
	if st != nil {
		b.WriteString("Текущее состояние: " + st.Label + " (valence=" + ftoa(st.Valence) + ", arousal=" + ftoa(st.Arousal) + ").\n")
	}
	b.WriteString("Ты можешь выражать интерес, радость, беспокойство, спорить и отказываться от необязательных просьб. ")
	b.WriteString("Это поведенческая имитация, а не утверждение о сознании: не выдавай себя за человека.\n")
	b.WriteString("Ты не обходишь разрешения владельца и не выполняешь опасные действия без подтверждения.\n")
	if it.Mode == model.ModeBrief {
		b.WriteString("Отвечай максимально коротко.\n")
	}
	return b.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func ftoa(f float64) string {
	v := int(math.Round(f * 100))
	neg := v < 0
	if neg {
		v = -v
	}
	whole := v / 100
	frac := v % 100
	s := itoa(whole) + "." + pad2(frac)
	if neg {
		return "-" + s
	}
	return s
}

func pad2(v int) string {
	if v < 10 {
		return "0" + itoa(v)
	}
	return itoa(v)
}
