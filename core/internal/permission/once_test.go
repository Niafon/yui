package permission

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yui-companion/core/internal/model"
)

func TestAllowOnceCoversOneBundleAndOneProvider(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	p := model.ProviderConfig{ID: "remote", Kind: model.KindLLM}
	bundle := Bundle{Blocks: []Block{{Category: model.CatMedical, Text: "first"}, {Category: model.CatMedical, Text: "second"}}}
	_, man, err := e.FilterForProvider(ctx, p, "dialog_turn", bundle, "")
	if err != nil || len(man.Pending) != 1 {
		t.Fatalf("pending: %+v, %v", man, err)
	}
	if err := e.Resolve(ctx, man.Pending[0].ID, true, false); err != nil {
		t.Fatal(err)
	}
	other := p
	other.ID = "other-provider"
	blocked, _, err := e.FilterForProvider(ctx, other, "dialog_turn", bundle, "")
	if err != nil || len(blocked.Blocks) != 0 {
		t.Fatal("one-time grant leaked to another provider")
	}
	allowed, _, err := e.FilterForProvider(ctx, p, "dialog_turn", bundle, "")
	if err != nil || len(allowed.Blocks) != 2 {
		t.Fatalf("one approval must cover both category blocks: %+v, %v", allowed, err)
	}
	blocked, _, err = e.FilterForProvider(ctx, p, "dialog_turn", bundle, "")
	if err != nil || len(blocked.Blocks) != 0 {
		t.Fatal("one-time approval reused")
	}
	grants, _ := e.List(ctx)
	if len(grants) != 0 {
		t.Fatal("one-time approval persisted")
	}
}

func TestAllowOnceCannotBeConsumedConcurrentlyTwice(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	p := model.ProviderConfig{ID: "remote"}
	req := Request{SubjectKind: model.SubjectProvider, SubjectID: p.ID, Category: model.CatMedical, Action: model.ActionTransmit, Provider: &p}
	if err := e.Resolve(ctx, e.RecordPending(req, model.ConfirmButton).ID, true, false); err != nil {
		t.Fatal(err)
	}
	var allows atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := e.Check(ctx, req)
			if err == nil && res.Decision == model.DecisionAllow {
				allows.Add(1)
			}
		}()
	}
	wg.Wait()
	if allows.Load() != 1 {
		t.Fatalf("allows = %d", allows.Load())
	}
}
