package sqlite

import (
	"fmt"
	"testing"
	"time"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

// BenchmarkVectorCacheExact256 is intentionally opt-in (go test -bench ...).
// It measures the point where an ANN backend becomes cheaper on the actual
// target CPU instead of hard-coding a threshold from somebody else's dataset.
func BenchmarkVectorCacheExact256(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 50_000, 100_000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			cache := newVectorCache()
			now := time.Unix(0, 0).UTC()
			for i := 0; i < n; i++ {
				v := make([]float32, 256)
				for j := range v {
					// Deterministic, non-zero vectors without benchmark setup RNG cost.
					v[j] = float32(((i+1)*(j+3))%251+1) / 251
				}
				cache.upsert(&model.MemoryItem{
					ID: fmt.Sprintf("m-%d", i), SpaceID: "user.general",
					Type: model.MemSemanticFact, Category: model.CatPreferences,
					Status: model.StatusConfirmed, OccurredAt: now, Embedding: v,
				})
			}
			query := make([]float32, 256)
			for i := range query {
				query[i] = float32((i*7)%251+1) / 251
			}
			q := store.MemoryQuery{ActiveOnly: true, Limit: 32}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = cache.search(query, q)
			}
		})
	}
}
