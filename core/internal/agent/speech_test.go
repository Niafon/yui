package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yui-companion/core/internal/audit"
	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/eventbus"
	"github.com/yui-companion/core/internal/identity"
	"github.com/yui-companion/core/internal/memory"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/permission"
	"github.com/yui-companion/core/internal/provider"
	"github.com/yui-companion/core/internal/session"
	"github.com/yui-companion/core/internal/store/memstore"
	"github.com/yui-companion/core/internal/tools"
)

func TestToolFollowupIsSpokenBeforeTurnDone(t *testing.T) {
	var calls atomic.Int32
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"time.now\",\"arguments\":\"{}\"}}]}}]}\n\ndata: [DONE]\n\n"))
		} else {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Результат готов и озвучен.\"}}]}\n\ndata: [DONE]\n\n"))
		}
	}))
	defer llm.Close()
	spoken := make(chan string, 1)
	tts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		spoken <- body.Text
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sample_rate":22050,"chunks":[{"seq":0,"audio_b64":"AQACAA=="}]}`))
	}))
	defer tts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := memstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	reg := provider.NewRegistry()
	if err := reg.Build(cfg.Providers, cfg.Defaults); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(model.ProviderConfig{ID: "tool-llm", Kind: model.KindLLM, Driver: "openai", Endpoint: llm.URL, Local: true}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(model.ProviderConfig{ID: "tool-tts", Kind: model.KindTTS, Driver: "worker", Endpoint: tts.URL, Local: true}); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetDefault(model.KindLLM, "tool-llm"); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetDefault(model.KindTTS, "tool-tts"); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	aud := audit.New(st.Audit(), bus)
	perms := permission.New(st.Permissions(), cfg.Privacy.LocalAllowedCategories)
	mem := memory.New(st.Memory(), perms, aud, reg, bus, cfg.Memory)
	ident := identity.New(st.Identities(), aud)
	self, err := ident.EnsureDefault(ctx, "test-owner")
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.NewManager(st.Sessions(), bus)
	sess, err := sessions.Start(ctx, self.ID, "test-owner", "desktop", model.ModeNormal, nil)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := sessions.Subscribe(sess.ID, 256)
	defer unsubscribe()
	runtime := NewRuntime(reg, perms, aud, mem, memory.NewExtractor(reg), ident, sessions, tools.NewRegistry())
	result, err := runtime.HandleUserTurn(ctx, TurnInput{SessionID: sess.ID, Text: "Установи таймер", DeviceID: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Reply, "Результат готов") {
		t.Fatalf("reply = %q", result.Reply)
	}
	select {
	case text := <-spoken:
		if text != "Результат готов и озвучен." {
			t.Fatalf("spoken = %q", text)
		}
	default:
		t.Fatal("tool follow-up was not sent to speech")
	}
	heard := false
	for {
		select {
		case ev := <-events:
			if ev.Type == session.FrameAudio {
				heard = true
			}
			if ev.Type == session.FrameTurnDone {
				if !heard {
					t.Fatal("turn.done arrived before tool follow-up audio")
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("missing turn.done")
		}
	}
}

func TestTurnWaitsForFinalSpeechChunk(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	tts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sample_rate":22050,"chunks":[{"seq":0,"audio_b64":"AQACAA=="}]}`))
	}))
	defer tts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := memstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	reg := provider.NewRegistry()
	if err := reg.Build(cfg.Providers, cfg.Defaults); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(model.ProviderConfig{ID: "delayed-tts", Kind: model.KindTTS, Driver: "worker", Endpoint: tts.URL, Local: true}); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetDefault(model.KindTTS, "delayed-tts"); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	aud := audit.New(st.Audit(), bus)
	perms := permission.New(st.Permissions(), cfg.Privacy.LocalAllowedCategories)
	mem := memory.New(st.Memory(), perms, aud, reg, bus, cfg.Memory)
	ident := identity.New(st.Identities(), aud)
	self, err := ident.EnsureDefault(ctx, "test-owner")
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.NewManager(st.Sessions(), bus)
	sess, err := sessions.Start(ctx, self.ID, "test-owner", "desktop", model.ModeNormal, nil)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := sessions.Subscribe(sess.ID, 256)
	defer unsubscribe()
	runtime := NewRuntime(reg, perms, aud, mem, memory.NewExtractor(reg), ident, sessions, tools.NewRegistry())
	done := make(chan error, 1)
	go func() {
		_, err := runtime.HandleUserTurn(ctx, TurnInput{SessionID: sess.ID, Text: "Привет", DeviceID: "desktop"})
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		close(release)
		t.Fatalf("turn ended before speech: %v", err)
	case <-ctx.Done():
		close(release)
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-done:
		close(release)
		t.Fatalf("turn canceled pending synthesis: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	heard := false
	for {
		select {
		case ev := <-events:
			if ev.Type == session.FrameAudio {
				heard = true
			}
			if ev.Type == session.FrameError {
				t.Fatalf("speech failed: %s", ev.Payload)
			}
			if ev.Type == session.FrameTurnDone {
				if !heard {
					t.Fatal("turn.done arrived before audio")
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("missing turn.done")
		}
	}
}

func TestCloudVoiceNeedsConsentForReplyText(t *testing.T) {
	var hits atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(bytes.Repeat([]byte{1, 0}, 2400))
	}))
	defer cloud.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := memstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	reg := provider.NewRegistry()
	if err := reg.Build(cfg.Providers, cfg.Defaults); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(model.ProviderConfig{ID: "cloud-voice", Kind: model.KindTTS, Driver: "openai", Endpoint: cloud.URL, Local: false}); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetDefault(model.KindTTS, "cloud-voice"); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	aud := audit.New(st.Audit(), bus)
	perms := permission.New(st.Permissions(), cfg.Privacy.LocalAllowedCategories)
	mem := memory.New(st.Memory(), perms, aud, reg, bus, cfg.Memory)
	ident := identity.New(st.Identities(), aud)
	self, err := ident.EnsureDefault(ctx, "test-owner")
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.NewManager(st.Sessions(), bus)
	sess, err := sessions.Start(ctx, self.ID, "test-owner", "desktop", model.ModeNormal, nil)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := sessions.Subscribe(sess.ID, 512)
	defer unsubscribe()
	runtime := NewRuntime(reg, perms, aud, mem, memory.NewExtractor(reg), ident, sessions, tools.NewRegistry())

	turn := func() (asked *permission.Pending, audio int) {
		if _, err := runtime.HandleUserTurn(ctx, TurnInput{SessionID: sess.ID, Text: "Расскажи что-нибудь", DeviceID: "desktop", SpeakerIsOwner: true}); err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case ev := <-events:
				switch ev.Type {
				case session.FramePermission:
					var p permission.Pending
					_ = json.Unmarshal([]byte(ev.Payload), &p)
					if p.Category == model.CatConversation && p.Provider == "cloud-voice" {
						asked = &p
					}
				case session.FrameAudio:
					audio++
				case session.FrameTurnDone:
					return asked, audio
				}
			case <-ctx.Done():
				t.Fatal("missing turn.done")
			}
		}
	}
	asked, audio := turn()
	if asked == nil || audio != 0 || hits.Load() != 0 {
		t.Fatalf("first turn: asked=%v audio=%d cloud hits=%d; reply text must not leave without consent", asked, audio, hits.Load())
	}
	if err := perms.Resolve(ctx, asked.ID, true, false); err != nil {
		t.Fatal(err)
	}
	if _, audio = turn(); audio == 0 || hits.Load() == 0 {
		t.Fatalf("after one-time consent: audio=%d cloud hits=%d", audio, hits.Load())
	}
}
