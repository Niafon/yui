package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yui-companion/core/internal/audit"
	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/eventbus"
	"github.com/yui-companion/core/internal/identity"
	"github.com/yui-companion/core/internal/inference"
	"github.com/yui-companion/core/internal/memory"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/permission"
	"github.com/yui-companion/core/internal/provider"
	"github.com/yui-companion/core/internal/session"
	"github.com/yui-companion/core/internal/store/memstore"
	"github.com/yui-companion/core/internal/tools"
)

func regressionRuntime(t *testing.T) (*Runtime, *model.Session, *model.Identity, <-chan model.Event) {
	t.Helper()
	st, err := memstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.Default()
	reg := provider.NewRegistry()
	if err := reg.Build(cfg.Providers, cfg.Defaults); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	aud := audit.New(st.Audit(), bus)
	perms := permission.New(st.Permissions(), cfg.Privacy.LocalAllowedCategories)
	mem := memory.New(st.Memory(), perms, aud, reg, bus, cfg.Memory)
	ident := identity.New(st.Identities(), aud)
	self, err := ident.EnsureDefault(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.NewManager(st.Sessions(), bus)
	sess, err := sessions.Start(context.Background(), self.ID, "owner", "desktop", model.ModeNormal, nil)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := sessions.Subscribe(sess.ID, 256)
	t.Cleanup(unsubscribe)
	return NewRuntime(reg, perms, aud, mem, memory.NewExtractor(reg), ident, sessions, tools.NewRegistry()), sess, self, events
}

func TestBargeInDoesNotBecomeErrorOrCompletedTurn(t *testing.T) {
	r, sess, _, events := regressionRuntime(t)
	r.Speak = false
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"unfinished\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-req.Context().Done()
	}))
	defer llm.Close()
	if err := r.reg.Register(model.ProviderConfig{ID: "stream", Kind: model.KindLLM, Driver: "openai", Endpoint: llm.URL, Local: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.reg.SetDefault(model.KindLLM, "stream"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := r.HandleUserTurn(ctx, TurnInput{SessionID: sess.ID, Text: "hello"}); done <- err }()
	for {
		select {
		case event := <-events:
			if event.Type == session.FrameDelta {
				goto interrupt
			}
		case err := <-done:
			t.Fatalf("turn ended before interruption: %v", err)
		case <-ctx.Done():
			t.Fatal("no stream delta")
		}
	}
interrupt:
	r.BargeIn(ctx, sess.ID, "button")
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("turn did not stop")
	}
	state, err := r.sessions.Get(ctx, sess.ID)
	if err != nil || state.State != model.SessionListening {
		t.Fatalf("state = %+v, %v", state, err)
	}
	for {
		select {
		case event := <-events:
			if event.Type == session.FrameError || event.Type == session.FrameTurnDone {
				t.Fatalf("unexpected %s after cancel: %s", event.Type, event.Payload)
			}
		default:
			return
		}
	}
}

func TestSpeechDoesNotUseResourceBlockedDefault(t *testing.T) {
	r, sess, self, events := regressionRuntime(t)
	r.reg = provider.NewRegistry()
	var calls atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"sample_rate":16000,"chunks":[]}`)
	}))
	defer worker.Close()
	if err := r.reg.Register(model.ProviderConfig{ID: "gpu-tts", Kind: model.KindTTS, Driver: "worker", Endpoint: worker.URL, Local: true, ExecutionBackend: "gpu", EstimatedVRAMMB: 9000}); err != nil {
		t.Fatal(err)
	}
	if err := r.reg.SetDefault(model.KindTTS, "gpu-tts"); err != nil {
		t.Fatal(err)
	}
	cfg := config.InferenceConfig{Enabled: true, Mode: inference.ModeAuto, MinimumQuality: 1, VRAMReserveMB: 1024, MaxGPUUtilPercent: 80, MaxFPSImpactPercent: 2}
	monitor := inference.NewMonitor(cfg)
	monitor.UpdateExternal(inference.Snapshot{GameActive: true, FPS: 30, BaselineFPS: 120})
	r.SetInferenceManager(inference.New(cfg, r.reg, monitor, nil, context.Background()))
	if _, err := r.inference.Select(context.Background(), inference.Task{Kind: model.KindTTS}); err == nil {
		t.Fatal("test setup must reject default TTS")
	}
	sentences := make(chan string, 1)
	sentences <- "This must not bypass the resource limit."
	close(sentences)
	r.speak(context.Background(), sess, self, nil, sentences)
	if calls.Load() != 0 {
		t.Fatalf("blocked worker received %d requests", calls.Load())
	}
	select {
	case event := <-events:
		if event.Type != session.FrameError {
			t.Fatalf("selection failure hidden: %s", event.Type)
		}
	default:
		t.Fatal("selection failure was not reported")
	}
}
