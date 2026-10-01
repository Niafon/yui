package identity

import (
	"context"
	"testing"

	"github.com/yui-companion/core/internal/audit"
	"github.com/yui-companion/core/internal/eventbus"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store/memstore"
)

func newService(t *testing.T) *Service {
	t.Helper()
	st, err := memstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	return New(st.Identities(), audit.New(st.Audit(), eventbus.New()))
}

func TestDefaultPresetIsAdultAndPersisted(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	it, err := svc.EnsureDefault(ctx, "user_owner")
	if err != nil {
		t.Fatal(err)
	}
	if it.AgeImage < 18 {
		t.Fatal("preset must present as 18+ (PER-003)")
	}
	again, err := svc.EnsureDefault(ctx, "user_owner")
	if err != nil || again.ID != it.ID {
		t.Fatal("EnsureDefault must not create a second identity")
	}
}

func TestEvolveIsRateLimited(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	it, _ := svc.EnsureDefault(ctx, "user_owner")
	before := it.Traits["warmth"]
	after, err := svc.Evolve(ctx, it.ID, model.Traits{"warmth": 0.9}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if delta := after.Traits["warmth"] - before; delta > maxTraitDelta+1e-9 {
		t.Fatalf("trait moved %.3f in one step, limit is %.3f (R-08)", delta, maxTraitDelta)
	}
}

func TestFrozenIdentityDoesNotDrift(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	it, _ := svc.EnsureDefault(ctx, "user_owner")
	it.DevelopmentMode = model.DevFrozen
	if err := svc.Update(ctx, it); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Evolve(ctx, it.ID, model.Traits{"warmth": 0.5}, "test"); err != ErrFrozen {
		t.Fatalf("error = %v, want ErrFrozen (PER-012)", err)
	}
}

func TestEmotionLabelsFollowValence(t *testing.T) {
	if Label(0.8, 0.8) != "joy" {
		t.Fatal("high valence and arousal should read as joy")
	}
	if Label(-0.8, 0.8) != "concern" {
		t.Fatal("negative valence with high arousal should read as concern")
	}
}

func TestSnapshotStatesTheImitationBoundary(t *testing.T) {
	it := DefaultPreset("u")
	text := Snapshot(it, NeutralState(it.ID))
	if !contains(text, "не выдавай себя за человека") {
		t.Fatal("the personality snapshot must keep the imitation boundary (SRS 11.7)")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
