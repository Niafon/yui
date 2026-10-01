package memstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

func TestMemoryCorrectionKeepsHistory(t *testing.T) {
	ctx := context.Background()
	st, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	repo := st.Memory()
	now := time.Now().UTC()
	if err := repo.CreateSpace(ctx, &model.MemorySpace{ID: "sp", OwnerType: "user", OwnerID: "u"}); err != nil {
		t.Fatal(err)
	}
	old := &model.MemoryItem{ID: "m1", SpaceID: "sp", Version: 1, Subject: "user.drink",
		Content: "кофе", Status: model.StatusConfirmed, OccurredAt: now, RecordedAt: now}
	if err := repo.Put(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendVersion(ctx, &model.MemoryVersion{MemoryID: "m1", Version: 1, Content: "кофе", ValidFrom: now}); err != nil {
		t.Fatal(err)
	}
	newer := &model.MemoryItem{ID: "m2", SpaceID: "sp", Version: 2, Subject: "user.drink",
		Content: "чай", Status: model.StatusConfirmed, OccurredAt: now, RecordedAt: now}
	if err := repo.Put(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := repo.Supersede(ctx, "m1", "m2"); err != nil {
		t.Fatal(err)
	}

	active, err := repo.Search(ctx, store.MemoryQuery{SpaceIDs: []string{"sp"}, Subject: "user.drink", ActiveOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != "m2" {
		t.Fatalf("active = %+v, want only m2 (MEM-008)", active)
	}
	versions, err := repo.Versions(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].ValidTo == nil || versions[0].ReplacedBy != "m2" {
		t.Fatalf("history not closed correctly: %+v", versions[0])
	}
}

func TestTrashAndRestore(t *testing.T) {
	ctx := context.Background()
	st, _ := Open("")
	repo := st.Memory()
	now := time.Now().UTC()
	_ = repo.CreateSpace(ctx, &model.MemorySpace{ID: "sp"})
	_ = repo.Put(ctx, &model.MemoryItem{ID: "m1", SpaceID: "sp", Content: "x",
		Status: model.StatusConfirmed, OccurredAt: now, RecordedAt: now})

	if err := repo.SoftDelete(ctx, "m1", now); err != nil {
		t.Fatal(err)
	}
	live, _ := repo.Search(ctx, store.MemoryQuery{SpaceIDs: []string{"sp"}})
	if len(live) != 0 {
		t.Fatal("deleted memory must not appear in normal search")
	}
	trash, _ := repo.Search(ctx, store.MemoryQuery{SpaceIDs: []string{"sp"}, IncludeDead: true})
	if len(trash) != 1 {
		t.Fatal("deleted memory must remain in the trash view (MEM-013)")
	}
	if err := repo.Restore(ctx, "m1"); err != nil {
		t.Fatal(err)
	}
	live, _ = repo.Search(ctx, store.MemoryQuery{SpaceIDs: []string{"sp"}})
	if len(live) != 1 {
		t.Fatal("restore failed")
	}
}

func TestSnapshotSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.Identities().Create(ctx, &model.Identity{ID: "id1", Name: "Юи",
		Traits: model.Traits{"warmth": 0.8}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := filepath.Abs(dir); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Identities().Get(ctx, "id1")
	if err != nil {
		t.Fatalf("identity lost after restart: %v", err)
	}
	if got.Name != "Юи" || got.Traits["warmth"] != 0.8 {
		t.Fatalf("identity = %+v, want the persisted values (PER-011/AC-11)", got)
	}
}

func TestDeviceRevocationIsVisible(t *testing.T) {
	ctx := context.Background()
	st, _ := Open("")
	repo := st.Devices()
	now := time.Now().UTC()
	_ = repo.Upsert(ctx, &model.Device{ID: "d1", Name: "phone", Kind: "android",
		TokenHash: "hash", PairedAt: now, LastSeenAt: now})
	if _, err := repo.FindByTokenHash(ctx, "hash"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Revoke(ctx, "d1", now); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByTokenHash(ctx, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if got.RevokedAt == nil {
		t.Fatal("revoked device must be marked (SEC-010)")
	}
}
