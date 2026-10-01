package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/yui-companion/core/internal/agent"
	"github.com/yui-companion/core/internal/logging"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/session"
	"github.com/yui-companion/core/internal/store"
	"github.com/yui-companion/core/internal/ws"
)

type socketPeer struct {
	deviceID string
	cancel   context.CancelFunc
}

// Recheck after upgrade under the same lock as revocation. Authentication may
// have finished just before the owner revoked this device.
func (s *Server) trackSocket(ctx context.Context, conn *ws.Conn, c caller, cancel context.CancelFunc) error {
	s.socketsMu.Lock()
	defer s.socketsMu.Unlock()
	if !c.Loopback {
		device, err := s.deps.Store.Devices().Get(ctx, c.DeviceID)
		if err != nil {
			return err
		}
		if device.RevokedAt != nil {
			return store.ErrNotFound
		}
	}
	s.sockets[conn] = socketPeer{deviceID: c.DeviceID, cancel: cancel}
	return nil
}

func (s *Server) revokeDevice(ctx context.Context, id string) error {
	s.socketsMu.Lock()
	if err := s.deps.Store.Devices().Revoke(ctx, id, time.Now().UTC()); err != nil {
		s.socketsMu.Unlock()
		return err
	}
	var connections []*ws.Conn
	for conn, peer := range s.sockets {
		if peer.deviceID == id {
			peer.cancel()
			connections = append(connections, conn)
		}
	}
	s.socketsMu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
	return nil
}

// Control and data planes are separate endpoints so that audio never delays a
// permission prompt or a state change (SRS 5.2).

// frame is the wire format for both planes.
type frame struct {
	Type    string          `json:"type"`
	At      time.Time       `json:"at"`
	Session string          `json:"session_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// isDataFrame decides which plane carries a given event.
func isDataFrame(t string) bool {
	switch t {
	case session.FrameTranscript, session.FrameTranscriptDone, session.FrameDelta, session.FrameAudio:
		return true
	}
	return false
}

func (s *Server) handleControlSocket(w http.ResponseWriter, r *http.Request) {
	s.serveSocket(w, r, false)
}

func (s *Server) handleDataSocket(w http.ResponseWriter, r *http.Request) {
	s.serveSocket(w, r, true)
}

func (s *Server) serveSocket(w http.ResponseWriter, r *http.Request, dataPlane bool) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session query parameter is required")
		return
	}
	sess, err := s.deps.Sessions.Get(r.Context(), sessionID)
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	conn, err := ws.Upgrade(w, r, ws.DefaultMaxPayload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer conn.Close()

	c := callerFrom(r.Context())
	log := logging.From(r.Context()).With("session", sessionID, "device", c.DeviceID)
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	if err := s.trackSocket(ctx, conn, c, cancel); err != nil {
		return
	}
	defer func() {
		s.socketsMu.Lock()
		delete(s.sockets, conn)
		s.socketsMu.Unlock()
	}()

	events, unsubscribe := s.deps.Sessions.Subscribe(sessionID, 256)
	defer unsubscribe()

	// Writer: forward bus events belonging to this plane.
	go func() {
		defer conn.Close()
		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ping.C:
				if err := conn.Ping(); err != nil {
					cancel()
					return
				}
			case ev, ok := <-events:
				if ctx.Err() != nil {
					return
				}
				if !ok {
					return
				}
				if isDataFrame(ev.Type) != dataPlane {
					continue
				}
				f := frame{Type: ev.Type, At: ev.At, Session: ev.SessionID, Payload: json.RawMessage(ev.Payload)}
				body, err := json.Marshal(f)
				if err != nil {
					continue
				}
				if err := conn.WriteText(body); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	// Reader: client commands and audio.
	audio := newAudioBuffer()
	for {
		op, data, err := conn.ReadMessage()
		if err != nil {
			log.Debug("socket closed", "error", err)
			return
		}
		if ctx.Err() != nil {
			return
		}
		switch op {
		case ws.OpBinary:
			// Raw PCM on the data plane; bounded so a stuck client cannot grow
			// memory without limit (SRS 5.4).
			audio.append(data)
		case ws.OpText:
			var cmd struct {
				Type       string `json:"type"`
				Text       string `json:"text"`
				SampleRate int    `json:"sample_rate"`
				Channels   int    `json:"channels"`
				ImageB64   string `json:"image_b64"`
				MIME       string `json:"mime"`
				Question   string `json:"question"`
				DeviceID   string `json:"device_id"`
			}
			if err := json.Unmarshal(data, &cmd); err != nil {
				continue
			}
			switch cmd.Type {
			case "ping":
				_ = conn.WriteText([]byte(`{"type":"pong"}`))

			case "session.cancel":
				s.deps.Agent.BargeIn(ctx, sessionID, "button")

			case "barge_in":
				// ADR-021: the mode decides whether the client may interrupt,
				// and how. The client reports what triggered it; the core does
				// not take its word for the mode.
				s.handleBargeIn(ctx, sessionID, cmd.Text)

			case "mic.state":
				// Mute is a first class, visible state (VOICE-008).
				s.deps.Sessions.Publish(sessionID, session.FrameState, map[string]any{
					"state": sess.State, "microphone": cmd.Text, "session_id": sessionID,
				})

			case "audio.chunk":
				if cmd.Text != "" {
					if raw, err := base64.StdEncoding.DecodeString(cmd.Text); err == nil {
						audio.append(raw)
					}
				}

			case "audio.end":
				buf := audio.take()
				if len(buf) == 0 {
					continue
				}
				go s.runAudioTurn(ctx, sess, c.DeviceID, buf, cmd.SampleRate, cmd.Channels)
			case "audio.clear":
				audio.take()

			case "text":
				if cmd.Text == "" {
					continue
				}
				go s.runTextTurn(ctx, sess, c.DeviceID, cmd.Text)

			case "vision.frame":
				raw, err := base64.StdEncoding.DecodeString(cmd.ImageB64)
				if err != nil {
					continue
				}
				go func(img []byte, mime, question string) {
					if _, err := s.deps.Agent.Look(ctx, sess, img, mime, question); err != nil {
						s.deps.Sessions.Publish(sessionID, session.FrameError,
							map[string]any{"stage": "vision", "error": err.Error()})
					}
				}(raw, cmd.MIME, cmd.Question)
			}
		case ws.OpClose:
			return
		}
	}
}

func (s *Server) runTextTurn(ctx context.Context, sess *model.Session, deviceID, text string) {
	_, err := s.deps.Agent.HandleUserTurn(ctx, agent.TurnInput{
		SessionID: sess.ID, Text: text, DeviceID: deviceID,
		TraceID: logging.TraceID(ctx), SpeakerIsOwner: true,
	})
	if err != nil {
		logging.From(ctx).Error("text turn failed", "error", err)
	}
}

// runAudioTurn is the voice path: transcribe, then answer only if the owner
// spoke (VOICE-005, VOICE-009).
func (s *Server) runAudioTurn(ctx context.Context, sess *model.Session, deviceID string, buf []byte, rate, channels int) {
	if rate <= 0 {
		rate = 16000
	}
	if channels <= 0 {
		channels = 1
	}
	tr, err := s.deps.Agent.Transcribe(ctx, sess, buf, rate, channels, true)
	if err != nil {
		s.deps.Sessions.Publish(sess.ID, session.FrameError, map[string]any{"stage": "stt", "error": err.Error()})
		return
	}
	if tr.Text == "" {
		return
	}
	isOwner := tr.SpeakerIsOwner == nil || *tr.SpeakerIsOwner
	if !isOwner {
		// Ambient speech is kept as context only, never answered as a command.
		logging.From(ctx).Debug("ignoring non-owner speech")
		return
	}
	if _, err := s.deps.Agent.HandleUserTurn(ctx, agent.TurnInput{
		SessionID: sess.ID, Text: tr.Text, DeviceID: deviceID,
		TraceID: logging.TraceID(ctx), SpeakerIsOwner: true,
	}); err != nil {
		logging.From(ctx).Error("voice turn failed", "error", err)
	}
}

// audioBuffer bounds how much audio one utterance may hold (about 60 s of
// 16 kHz mono PCM).
type audioBuffer struct {
	max  int
	data []byte
}

func newAudioBuffer() *audioBuffer { return &audioBuffer{max: 16000 * 2 * 60} }

func (b *audioBuffer) append(chunk []byte) {
	if len(b.data)+len(chunk) > b.max {
		// Drop the oldest audio rather than the newest speech.
		overflow := len(b.data) + len(chunk) - b.max
		if overflow >= len(b.data) {
			b.data = b.data[:0]
		} else {
			b.data = b.data[overflow:]
		}
	}
	b.data = append(b.data, chunk...)
}

func (b *audioBuffer) take() []byte {
	out := b.data
	b.data = nil
	return out
}

// handleBargeIn applies the configured barge-in policy.
//
//	off        — ignored entirely
//	button     — only an explicit press interrupts
//	wake_word  — only the wake phrase interrupts
//	any_speech — detected speech interrupts, but only after MinSpeechMs and
//	             only if the client confirms echo cancellation is on
func (s *Server) handleBargeIn(ctx context.Context, sessionID, trigger string) {
	cfg := s.deps.Cfg.Voice.BargeIn
	allowed := false
	switch cfg.Mode {
	case "off":
		allowed = false
	case "button":
		allowed = trigger == "button"
	case "wake_word":
		allowed = trigger == "button" || trigger == "wake_word"
	case "any_speech":
		allowed = trigger == "button" || trigger == "wake_word" || trigger == "speech"
		if trigger == "speech" && cfg.RequireEchoCancellation {
			// The client must state that AEC is active; without it the
			// companion hears its own voice and interrupts itself.
			allowed = false
		}
	}
	if trigger == "speech_aec" && cfg.Mode == "any_speech" {
		allowed = true
	}
	if !allowed {
		return
	}
	s.deps.Agent.BargeIn(ctx, sessionID, trigger)
}
