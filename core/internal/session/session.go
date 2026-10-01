// Package session owns conversation lifecycle. A session outlives any single
// client: closing the phone app or restarting the desktop stage must not end
// the conversation (CORE-003, CORE-004, NFR-002).
package session

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yui-companion/core/internal/eventbus"
	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

// Topic returns the bus topic carrying one session's stream.
func Topic(sessionID string) string { return "session." + sessionID }

// Frame types published to clients.
const (
	FrameState          = "session.state"
	FrameTranscript     = "transcript.partial"
	FrameTranscriptDone = "transcript.final"
	FrameDelta          = "turn.delta"
	FrameTurnDone       = "turn.done"
	FrameExpression     = "avatar.expression"
	FrameAudio          = "tts.chunk"
	FrameMemory         = "memory.indicator"
	FrameManifest       = "data.manifest"
	FrameError          = "error"
	FramePermission     = "permission.request"
	FrameConfirm        = "tool.confirm"
	FrameToolResult     = "tool.result"
	FrameBargeIn        = "barge_in"
)

type Manager struct {
	repo store.SessionRepo
	bus  *eventbus.Bus

	mu       sync.RWMutex
	seqMu    sync.Mutex
	seq      map[string]int
	cancels  map[string]*activeTurn
	activeIn int64 // number of turns currently being processed
}

type activeTurn struct {
	cancel context.CancelFunc
}

func NewManager(repo store.SessionRepo, bus *eventbus.Bus) *Manager {
	return &Manager{repo: repo, bus: bus, seq: map[string]int{}, cancels: map[string]*activeTurn{}}
}

func (m *Manager) Start(ctx context.Context, identityID, userID, deviceID string, mode model.ConversationMode, providers map[string]string) (*model.Session, error) {
	now := time.Now().UTC()
	s := &model.Session{
		ID:           ids.New("ses"),
		IdentityID:   identityID,
		UserID:       userID,
		InputDevice:  deviceID,
		OutputDevice: deviceID,
		Mode:         mode,
		State:        model.SessionIdle,
		Providers:    providers,
		StartedAt:    now,
		UpdatedAt:    now,
	}
	if err := m.repo.Create(ctx, s); err != nil {
		return nil, err
	}
	m.Publish(s.ID, FrameState, map[string]any{"state": s.State, "session_id": s.ID})
	return s, nil
}

func (m *Manager) Get(ctx context.Context, id string) (*model.Session, error) {
	return m.repo.Get(ctx, id)
}

func (m *Manager) ListActive(ctx context.Context) ([]*model.Session, error) {
	return m.repo.ListActive(ctx)
}

func (m *Manager) SetState(ctx context.Context, id string, st model.SessionState) error {
	s, err := m.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	s.State = st
	s.UpdatedAt = time.Now().UTC()
	if err := m.repo.Update(ctx, s); err != nil {
		return err
	}
	m.Publish(id, FrameState, map[string]any{"state": st, "session_id": id})
	return nil
}

// SetOutputDevice moves the voice to one device only, to avoid the same reply
// playing twice (SRS 7.4, VOICE-010).
func (m *Manager) SetOutputDevice(ctx context.Context, id, deviceID string) error {
	s, err := m.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	s.OutputDevice = deviceID
	s.UpdatedAt = time.Now().UTC()
	if err := m.repo.Update(ctx, s); err != nil {
		return err
	}
	m.Publish(id, FrameState, map[string]any{"state": s.State, "output_device": deviceID, "session_id": id})
	return nil
}

// SetProvider switches one provider kind mid-session without touching the
// identity or the history (AC-08).
func (m *Manager) SetProvider(ctx context.Context, id, kind, providerID string) error {
	s, err := m.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if s.Providers == nil {
		s.Providers = map[string]string{}
	}
	s.Providers[kind] = providerID
	s.UpdatedAt = time.Now().UTC()
	return m.repo.Update(ctx, s)
}

func (m *Manager) Close(ctx context.Context, id string) error {
	s, err := m.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	s.State = model.SessionClosed
	s.ClosedAt = &now
	s.UpdatedAt = now
	if err := m.repo.Update(ctx, s); err != nil {
		return err
	}
	m.CancelTurn(id)
	m.Publish(id, FrameState, map[string]any{"state": model.SessionClosed, "session_id": id})
	return nil
}

func (m *Manager) AppendTurn(ctx context.Context, sessionID, role, text, providerID, deviceID, traceID string, started time.Time) (*model.Turn, error) {
	m.seqMu.Lock()
	defer m.seqMu.Unlock()
	if _, ok := m.seq[sessionID]; !ok {
		turns, err := m.repo.Turns(ctx, sessionID, 1)
		if err != nil {
			return nil, err
		}
		if len(turns) > 0 {
			m.seq[sessionID] = turns[len(turns)-1].Seq
		}
	}
	seq := m.seq[sessionID] + 1
	t := &model.Turn{
		ID: ids.New("turn"), SessionID: sessionID, Seq: seq, Role: role, Text: text,
		Provider: providerID, DeviceID: deviceID, TraceID: traceID,
		StartedAt: started, CompletedAt: time.Now().UTC(),
	}
	if err := m.repo.AppendTurn(ctx, t); err != nil {
		return nil, err
	}
	m.seq[sessionID] = seq
	return t, nil
}

func (m *Manager) Turns(ctx context.Context, sessionID string, limit int) ([]*model.Turn, error) {
	return m.repo.Turns(ctx, sessionID, limit)
}

// Publish sends a frame to every client attached to the session.
func (m *Manager) Publish(sessionID, frameType string, payload any) {
	if m.bus == nil {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	m.bus.Publish(Topic(sessionID), model.Event{
		ID:        ids.New("fr"),
		At:        time.Now().UTC(),
		Type:      frameType,
		Source:    "core",
		SessionID: sessionID,
		Payload:   string(body),
	})
}

// Subscribe attaches a client to the session stream.
func (m *Manager) Subscribe(sessionID string, buffer int) (<-chan model.Event, func()) {
	return m.bus.Subscribe(Topic(sessionID), buffer)
}

// BeginTurn registers the cancel function for the running turn, which is the
// seam barge-in will use later (VOICE-011).
func (m *Manager) BeginTurn(sessionID string, cancel context.CancelFunc) func() {
	m.mu.Lock()
	if prev, ok := m.cancels[sessionID]; ok {
		prev.cancel()
	}
	turn := &activeTurn{cancel: cancel}
	m.cancels[sessionID] = turn
	atomic.AddInt64(&m.activeIn, 1)
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			if m.cancels[sessionID] == turn {
				delete(m.cancels, sessionID)
			}
			atomic.AddInt64(&m.activeIn, -1)
			m.mu.Unlock()
		})
	}
}

// CancelTurn stops generation and synthesis for a session.
func (m *Manager) CancelTurn(sessionID string) {
	m.mu.Lock()
	cancel, ok := m.cancels[sessionID]
	delete(m.cancels, sessionID)
	m.mu.Unlock()
	if ok {
		cancel.cancel()
	}
}

// Busy reports whether any turn is in flight. The scheduler uses it to pause
// background work during a conversation (NFR-006, AC-14).
func (m *Manager) Busy() bool { return atomic.LoadInt64(&m.activeIn) > 0 }
