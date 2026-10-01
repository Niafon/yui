package memory

import (
	"strings"
	"testing"
	"time"

	"github.com/yui-companion/core/internal/model"
)

func TestContextBudgetDeduplicatesAndKeepsWholeFacts(t *testing.T) {
	now := time.Now()
	a := item("a", "Любит крепкий чай", model.StatusConfirmed, .8, time.Hour)
	b := item("b", "Любит крепкий чай", model.StatusConfirmed, .7, time.Hour)
	huge := item("huge", strings.Repeat("чай ", 1000), model.StatusConfirmed, 1, time.Hour)
	c := item("c", "Зелёный чай без сахара", model.StatusConfirmed, .5, time.Hour)
	got := SelectContext(Rank("чай", nil, []*model.MemoryItem{a, b, huge, c}, now, 0), 4, 100)
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	for _, sc := range got {
		if sc.Item.ID == "huge" {
			t.Fatal("oversized fact must not crowd out smaller records")
		}
	}
}

func TestRankRejectsExpiredDeletedAndUnrelated(t *testing.T) {
	now := time.Now()
	old := item("old", "чай", model.StatusConfirmed, 1, time.Hour)
	past := now.Add(-time.Second)
	old.ExpiresAt = &past
	deleted := item("deleted", "чай", model.StatusConfirmed, 1, time.Hour)
	deleted.DeletedAt = &past
	unrelated := item("noise", "игра в шахматы", model.StatusConfirmed, 1, 0)
	if got := Rank("чай", nil, []*model.MemoryItem{old, deleted, unrelated}, now, 4); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}
