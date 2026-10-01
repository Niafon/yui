package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

func TestMemoryExactVectorAndFTS(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "yui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	repo := st.Memory()
	if err := repo.CreateSpace(ctx, &model.MemorySpace{ID: "user.general", OwnerType: "user", OwnerID: "u", Name: "general", Category: model.CatPreferences, Sensitivity: model.SensNormal}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	items := []*model.MemoryItem{
		{ID: "a", SpaceID: "user.general", Type: model.MemSemanticFact, Category: model.CatPreferences, Content: "Любимый язык разработки — Go", Confidence: 1, Importance: .8, Sensitivity: model.SensNormal, Status: model.StatusConfirmed, OccurredAt: now, RecordedAt: now, Embedding: []float32{1, 0, 0}},
		{ID: "b", SpaceID: "user.general", Type: model.MemSemanticFact, Category: model.CatPreferences, Content: "Любимый редактор — VS Code", Confidence: 1, Importance: .5, Sensitivity: model.SensNormal, Status: model.StatusConfirmed, OccurredAt: now, RecordedAt: now, Embedding: []float32{0, 1, 0}},
	}
	for _, it := range items {
		if err := repo.Put(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	near, err := repo.VectorSearch(ctx, []float32{.9, .1, 0}, store.MemoryQuery{SpaceIDs: []string{"user.general"}, ActiveOnly: true, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(near) == 0 || near[0].ID != "a" {
		t.Fatalf("exact vector top=%v", idsOf(near))
	}

	lex, err := repo.LexicalSearch(ctx, "язык Go", store.MemoryQuery{SpaceIDs: []string{"user.general"}, ActiveOnly: true, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(lex) == 0 || lex[0].ID != "a" {
		t.Fatalf("fts top=%v", idsOf(lex))
	}
}

func idsOf(items []*model.MemoryItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}
