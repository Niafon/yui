package permission

import (
	"context"
	"testing"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store/memstore"
)

func newEngine(t *testing.T) *Engine {
	t.Helper()
	st, err := memstore.Open("")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return New(st.Permissions(), []model.Category{
		model.CatCurrentText, model.CatConversation, model.CatPreferences, model.CatProfile,
	})
}

func TestRemoteProviderIsAskedBeforeFirstTransfer(t *testing.T) {
	e := newEngine(t)
	remote := model.ProviderConfig{ID: "openrouter", Kind: model.KindLLM, Local: false}
	res, err := e.Check(context.Background(), Request{
		SubjectKind: model.SubjectProvider, SubjectID: remote.ID,
		Category: model.CatPreferences, Action: model.ActionTransmit, Provider: &remote,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != model.DecisionAsk {
		t.Fatalf("decision = %v, want ask (SEC-004)", res.Decision)
	}
}

func TestLocalProviderMayUseDefaultCategories(t *testing.T) {
	e := newEngine(t)
	local := model.ProviderConfig{ID: "llama", Kind: model.KindLLM, Local: true}
	res, _ := e.Check(context.Background(), Request{
		SubjectKind: model.SubjectProvider, SubjectID: local.ID,
		Category: model.CatConversation, Action: model.ActionTransmit, Provider: &local,
	})
	if res.Decision != model.DecisionAllow {
		t.Fatalf("decision = %v, want allow", res.Decision)
	}
}

func TestSensitiveCategoryIsNeverAllowedByProviderDefaults(t *testing.T) {
	e := newEngine(t)
	local := model.ProviderConfig{ID: "llama", Kind: model.KindLLM, Local: true}
	res, _ := e.Check(context.Background(), Request{
		SubjectKind: model.SubjectProvider, SubjectID: local.ID,
		Category: model.CatMedical, Action: model.ActionTransmit, Provider: &local,
	})
	if res.Decision == model.DecisionAllow {
		t.Fatal("medical data must not be allowed without an explicit grant")
	}
}

func TestFilterForProviderDropsForbiddenBlocks(t *testing.T) {
	e := newEngine(t)
	remote := model.ProviderConfig{
		ID: "openrouter", Kind: model.KindLLM, Local: false,
		Allowed: []model.Category{model.CatCurrentText, model.CatConversation},
	}
	in := Bundle{Blocks: []Block{
		{Category: model.CatCurrentText, Label: "current_text", Text: "какая у меня аллергия?", Tokens: 6},
		{Category: model.CatMedical, Label: "memory:fact", Text: "диагноз пациента", Tokens: 4},
		{Category: model.CatConversation, Label: "recent_turns", Text: "владелец: привет", Tokens: 4},
	}}
	out, manifest, err := e.FilterForProvider(context.Background(), remote, "dialog_turn", in, "tr_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(out.Blocks))
	}
	for _, b := range out.Blocks {
		if b.Category == model.CatMedical {
			t.Fatal("medical block reached the provider (R-05)")
		}
	}
	if len(manifest.Excluded) != 1 || manifest.Excluded[0] != model.CatMedical {
		t.Fatalf("manifest excluded = %v, want [medical]", manifest.Excluded)
	}
	if len(e.Pending()) == 0 {
		t.Fatal("a blocked category must raise a pending consent request")
	}
}

func TestExplicitGrantOverridesDefaults(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	remote := model.ProviderConfig{ID: "openrouter", Kind: model.KindLLM}
	if err := e.Grant(ctx, &model.Grant{
		SubjectKind: model.SubjectProvider, SubjectID: remote.ID,
		Category: model.CatPreferences, Action: model.ActionTransmit, Decision: model.DecisionAllow,
	}); err != nil {
		t.Fatal(err)
	}
	res, _ := e.Check(ctx, Request{
		SubjectKind: model.SubjectProvider, SubjectID: remote.ID,
		Category: model.CatPreferences, Action: model.ActionTransmit, Provider: &remote,
	})
	if res.Decision != model.DecisionAllow {
		t.Fatalf("decision = %v, want allow after grant", res.Decision)
	}
}

func TestFitKeepsCurrentTextWithinBudget(t *testing.T) {
	in := Bundle{Blocks: []Block{
		{Category: model.CatConversation, Text: "long history", Tokens: 100},
		{Category: model.CatCurrentText, Text: "вопрос", Tokens: 10},
	}}
	out := Fit(in, 20)
	if len(out.Blocks) != 1 || out.Blocks[0].Category != model.CatCurrentText {
		t.Fatalf("Fit kept %+v, want only current_text", out.Blocks)
	}
}
