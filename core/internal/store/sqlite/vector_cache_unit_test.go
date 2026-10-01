package sqlite

import (
	"testing"
	"time"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

func TestVectorCacheExactOrderingAndDelete(t *testing.T) {
	c := newVectorCache()
	now := time.Unix(0, 0).UTC()
	for _, it := range []*model.MemoryItem{
		{ID: "x", SpaceID: "s", Type: model.MemSemanticFact, Category: model.CatPreferences, Status: model.StatusConfirmed, OccurredAt: now, Embedding: []float32{1, 0}},
		{ID: "y", SpaceID: "s", Type: model.MemSemanticFact, Category: model.CatPreferences, Status: model.StatusConfirmed, OccurredAt: now, Embedding: []float32{.8, .2}},
		{ID: "z", SpaceID: "s", Type: model.MemSemanticFact, Category: model.CatPreferences, Status: model.StatusConfirmed, OccurredAt: now, Embedding: []float32{0, 1}},
	} {
		c.upsert(it)
	}
	got := c.search([]float32{3, 0}, store.MemoryQuery{SpaceIDs: []string{"s"}, ActiveOnly: true, Limit: 3})
	if len(got) != 3 || got[0] != "x" || got[1] != "y" || got[2] != "z" {
		t.Fatalf("unexpected order: %v", got)
	}
	c.remove("x")
	got = c.search([]float32{1, 0}, store.MemoryQuery{SpaceIDs: []string{"s"}, ActiveOnly: true, Limit: 3})
	if len(got) != 2 || got[0] != "y" || got[1] != "z" {
		t.Fatalf("unexpected order after delete: %v", got)
	}
}
