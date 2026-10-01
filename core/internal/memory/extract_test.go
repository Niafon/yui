package memory

import (
	"testing"
	"time"

	"github.com/yui-companion/core/internal/model"
)

func TestExtractorFindsName(t *testing.T) {
	e := NewExtractor(nil)
	got := e.FromTurn("id_1", SpaceUserGeneral, "Привет, меня зовут Антон, и я живу в Хельсинки",
		model.Provenance{Kind: "conversation", At: time.Now()})
	if len(got) < 2 {
		t.Fatalf("candidates = %d, want at least 2: %+v", len(got), got)
	}
	var foundName bool
	for _, c := range got {
		if c.Subject == "user.name" {
			foundName = true
			if c.Confidence < 0.9 {
				t.Fatalf("name confidence = %v, want >= 0.9", c.Confidence)
			}
		}
	}
	if !foundName {
		t.Fatal("the name fact was not extracted")
	}
}

func TestExplicitRememberIsHighConfidence(t *testing.T) {
	e := NewExtractor(nil)
	got := e.FromTurn("id_1", SpaceUserGeneral, "Запомни, что дедлайн проекта в пятницу",
		model.Provenance{Kind: "conversation", At: time.Now()})
	if len(got) == 0 {
		t.Fatal("explicit instruction produced no candidate")
	}
	if got[0].RequiresConfirmation {
		t.Fatal("an explicit instruction should not need confirmation")
	}
}

func TestLLMCandidatesAlwaysNeedConfirmation(t *testing.T) {
	raw := `Вот факты: [{"content":"Пользователь работает архитектором","category":"profile","subject":"user.occupation","confidence":0.9,"importance":0.7}]`
	got, err := parseCandidates(raw, "id_1", SpaceUserGeneral, model.Provenance{Kind: "conversation"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	if !got[0].RequiresConfirmation {
		t.Fatal("model-derived facts must be marked for confirmation (R-04)")
	}
}
