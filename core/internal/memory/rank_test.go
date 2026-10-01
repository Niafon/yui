package memory

import (
	"testing"
	"time"

	"github.com/yui-companion/core/internal/model"
)

func item(id, content string, status model.MemoryStatus, importance float64, age time.Duration) *model.MemoryItem {
	now := time.Now().UTC()
	return &model.MemoryItem{
		ID: id, Content: content, Status: status, Importance: importance,
		Confidence: 0.9, OccurredAt: now.Add(-age), RecordedAt: now.Add(-age),
	}
}

func TestRankPrefersLexicalMatch(t *testing.T) {
	items := []*model.MemoryItem{
		item("a", "Пользователь живёт в Хельсинки", model.StatusConfirmed, 0.5, time.Hour),
		item("b", "Любимый напиток — чай", model.StatusConfirmed, 0.5, time.Hour),
	}
	got := Rank("где живёт пользователь", nil, items, time.Now().UTC(), 5)
	if len(got) == 0 || got[0].Item.ID != "a" {
		t.Fatalf("top result = %+v, want a", got)
	}
}

func TestRankDropsSupersededMemories(t *testing.T) {
	items := []*model.MemoryItem{
		item("old", "Любимый напиток — кофе", model.StatusSuperseded, 0.9, time.Hour),
		item("new", "Любимый напиток — чай", model.StatusConfirmed, 0.5, time.Hour),
	}
	got := Rank("любимый напиток", nil, items, time.Now().UTC(), 5)
	for _, s := range got {
		if s.Item.ID == "old" {
			t.Fatal("superseded memory must not be recalled (MEM-008)")
		}
	}
	if len(got) != 1 {
		t.Fatalf("results = %d, want 1", len(got))
	}
}

func TestPinnedMemoryOutranksNoise(t *testing.T) {
	pinned := item("pin", "Аллергия на орехи", model.StatusConfirmed, 0.2, 400*24*time.Hour)
	pinned.Pinned = true
	items := []*model.MemoryItem{
		item("fresh", "Сегодня был дождь", model.StatusExtracted, 0.4, time.Minute),
		pinned,
	}
	got := Rank("что важно помнить", nil, items, time.Now().UTC(), 5)
	if got[0].Item.ID != "pin" {
		t.Fatalf("top = %s, want pinned item (MEM-011)", got[0].Item.ID)
	}
}

func TestNeedsConfirmationRanksBelowConfirmed(t *testing.T) {
	items := []*model.MemoryItem{
		item("guess", "Пользователь любит чай", model.StatusNeedsConfirmation, 0.6, time.Hour),
		item("known", "Пользователь любит чай", model.StatusConfirmed, 0.6, time.Hour),
	}
	got := Rank("чай", nil, items, time.Now().UTC(), 5)
	if got[0].Item.ID != "known" {
		t.Fatalf("top = %s, want the confirmed fact (MEM-007)", got[0].Item.ID)
	}
}

func TestDecayedImportanceFallsWithAge(t *testing.T) {
	fresh := item("f", "x", model.StatusExtracted, 1.0, time.Hour)
	old := item("o", "x", model.StatusExtracted, 1.0, 200*24*time.Hour)
	now := time.Now().UTC()
	if DecayedImportance(old, now) >= DecayedImportance(fresh, now) {
		t.Fatal("old memories must decay (SRS 10.9)")
	}
	pinned := item("p", "x", model.StatusExtracted, 1.0, 200*24*time.Hour)
	pinned.Pinned = true
	if DecayedImportance(pinned, now) != 1.0 {
		t.Fatal("pinned memories must not decay")
	}
}
