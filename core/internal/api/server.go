// Package api exposes the core over HTTP and WebSocket. Clients never touch
// the database: this is the only surface (SRS 6.1, 18.1).
package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yui-companion/core/internal/agent"
	"github.com/yui-companion/core/internal/audit"
	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/eventbus"
	"github.com/yui-companion/core/internal/identity"
	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/inference"
	"github.com/yui-companion/core/internal/logging"
	"github.com/yui-companion/core/internal/memory"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/modelsettings"
	"github.com/yui-companion/core/internal/permission"
	"github.com/yui-companion/core/internal/provider"
	"github.com/yui-companion/core/internal/scheduler"
	"github.com/yui-companion/core/internal/session"
	"github.com/yui-companion/core/internal/store"
	"github.com/yui-companion/core/internal/supervisor"
	"github.com/yui-companion/core/internal/tools"
	"github.com/yui-companion/core/internal/ws"
)

// Deps is everything the HTTP layer needs. It is passed explicitly so the
// handlers stay testable.
type Deps struct {
	Cfg           *config.Config
	Store         store.Store
	Bus           *eventbus.Bus
	Sessions      *session.Manager
	Agent         *agent.Runtime
	Memory        *memory.Service
	Identities    *identity.Service
	Perms         *permission.Engine
	Audit         *audit.Service
	Providers     *provider.Registry
	Tools         *tools.Registry
	Scheduler     *scheduler.Scheduler
	Supervisor    *supervisor.Supervisor
	Inference     *inference.Manager
	ModelSettings *modelsettings.Store
	UserID        string
}

type Server struct {
	deps Deps
	mux  *http.ServeMux
	// pairing codes are short lived and single use (CL-004)
	pairing    *pairingStore
	socketsMu  sync.Mutex
	sockets    map[*ws.Conn]socketPeer
	settingsMu sync.Mutex
}

// caller identifies an authenticated client.
type caller struct {
	DeviceID string
	Kind     string // android|desktop|loopback
	Owner    bool
	Loopback bool
}

type ctxKey string

const callerKey ctxKey = "caller"

func New(deps Deps) *Server {
	s := &Server{deps: deps, mux: http.NewServeMux(), pairing: newPairingStore(), sockets: map[*ws.Conn]socketPeer{}}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.cors(s.logging(s.mux)) }

func (s *Server) routes() {
	m := s.mux

	// Unauthenticated: liveness and the pairing claim (protected by a
	// single-use code shown on the PC).
	m.HandleFunc("GET /healthz", s.handleHealth)
	m.HandleFunc("POST /v1/pair/claim", s.handlePairClaim)

	auth := s.authenticated

	m.Handle("GET /v1/status", auth(s.handleStatus))
	m.Handle("POST /v1/pair/start", auth(s.handlePairStart))
	m.Handle("GET /v1/devices", auth(s.handleDevices))
	m.Handle("POST /v1/devices/{id}/revoke", auth(s.handleDeviceRevoke))

	m.Handle("GET /v1/identities", auth(s.handleIdentityList))
	m.Handle("GET /v1/identities/{id}", auth(s.handleIdentityGet))
	m.Handle("PATCH /v1/identities/{id}", auth(s.handleIdentityUpdate))
	m.Handle("POST /v1/identities/{id}/checkpoint", auth(s.handleIdentityCheckpoint))
	m.Handle("GET /v1/identities/{id}/emotion", auth(s.handleEmotion))

	m.Handle("POST /v1/sessions", auth(s.handleSessionCreate))
	m.Handle("GET /v1/sessions", auth(s.handleSessionList))
	m.Handle("GET /v1/sessions/{id}", auth(s.handleSessionGet))
	m.Handle("GET /v1/sessions/{id}/turns", auth(s.handleSessionTurns))
	m.Handle("POST /v1/sessions/{id}/message", auth(s.handleSessionMessage))
	m.Handle("POST /v1/sessions/{id}/vision", auth(s.handleSessionVision))
	m.Handle("POST /v1/sessions/{id}/provider", auth(s.handleSessionProvider))
	m.Handle("POST /v1/sessions/{id}/output-device", auth(s.handleSessionOutput))
	m.Handle("POST /v1/sessions/{id}/cancel", auth(s.handleSessionCancel))
	m.Handle("POST /v1/sessions/{id}/close", auth(s.handleSessionClose))

	m.Handle("GET /v1/memory", auth(s.handleMemorySearch))
	m.Handle("POST /v1/memory", auth(s.handleMemoryCreate))
	m.Handle("GET /v1/memory/{id}", auth(s.handleMemoryGet))
	m.Handle("GET /v1/memory/{id}/versions", auth(s.handleMemoryVersions))
	m.Handle("POST /v1/memory/{id}/confirm", auth(s.handleMemoryConfirm))
	m.Handle("POST /v1/memory/{id}/pin", auth(s.handleMemoryPin))
	m.Handle("POST /v1/memory/{id}/restore", auth(s.handleMemoryRestore))
	m.Handle("DELETE /v1/memory/{id}", auth(s.handleMemoryDelete))

	m.Handle("GET /v1/permissions", auth(s.handlePermissionList))
	m.Handle("POST /v1/permissions", auth(s.handlePermissionGrant))
	m.Handle("DELETE /v1/permissions/{id}", auth(s.handlePermissionRevoke))
	m.Handle("GET /v1/permissions/pending", auth(s.handlePermissionPending))
	m.Handle("POST /v1/permissions/pending/{id}", auth(s.handlePermissionResolve))

	m.Handle("GET /v1/tools", auth(s.handleToolList))
	m.Handle("GET /v1/tools/pending", auth(s.handleToolPending))
	m.Handle("POST /v1/tools/pending/{id}", auth(s.handleToolConfirm))

	m.Handle("GET /v1/providers", auth(s.handleProviders))
	m.Handle("POST /v1/providers/default", auth(s.handleProviderDefault))
	m.Handle("GET /v1/models", auth(s.handleModels))
	m.Handle("POST /v1/models", auth(s.handleModelAdd))

	m.Handle("GET /v1/inference", auth(s.handleInferenceStatus))
	m.Handle("PATCH /v1/inference/preferences", auth(s.handleInferencePreferences))
	m.Handle("POST /v1/inference/telemetry", auth(s.handleInferenceTelemetry))

	m.Handle("GET /v1/audit", auth(s.handleAudit))

	m.Handle("GET /v1/control", auth(s.handleControlSocket))
	m.Handle("GET /v1/data", auth(s.handleDataSocket))

	s.routesDebug()
	if s.deps.Cfg.Server.StageDir != "" {
		m.Handle("GET /", http.FileServer(http.Dir(s.deps.Cfg.Server.StageDir)))
	}
}

// authenticated accepts either a paired device token or the loopback token
// used by the desktop stage. Remote plaintext is refused (SEC-005).
func (s *Server) authenticated(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local := isLoopback(r.RemoteAddr)
		if !local && s.deps.Cfg.Server.RequireTLSForRemote && r.TLS == nil {
			writeError(w, http.StatusUpgradeRequired, "remote connections must use TLS")
			return
		}
		token := bearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing token")
			return
		}
		if lt := s.deps.Cfg.Server.LoopbackToken; lt != "" && local &&
			subtle.ConstantTimeCompare([]byte(token), []byte(lt)) == 1 {
			ctx := context.WithValue(r.Context(), callerKey, caller{DeviceID: "desktop", Kind: "desktop", Owner: true, Loopback: true})
			next(w, r.WithContext(ctx))
			return
		}
		// Serialize the last-seen update with revocation so a stale device copy
		// cannot write RevokedAt=nil back after the owner revokes it.
		s.socketsMu.Lock()
		dev, err := s.deps.Store.Devices().FindByTokenHash(r.Context(), HashToken(token))
		if err != nil || dev.RevokedAt != nil {
			s.socketsMu.Unlock()
			writeError(w, http.StatusUnauthorized, "unknown or revoked device")
			return
		}
		dev.LastSeenAt = time.Now().UTC()
		_ = s.deps.Store.Devices().Upsert(r.Context(), dev)
		s.socketsMu.Unlock()
		ctx := context.WithValue(r.Context(), callerKey, caller{DeviceID: dev.ID, Kind: dev.Kind, Owner: true})
		next(w, r.WithContext(ctx))
	})
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := r.Header.Get("X-Trace-Id")
		if traceID == "" {
			traceID = ids.Trace()
		}
		ctx := logging.WithTrace(r.Context(), traceID)
		w.Header().Set("X-Trace-Id", traceID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func callerFrom(ctx context.Context) caller {
	c, _ := ctx.Value(callerKey).(caller)
	return c
}

func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	// Browsers cannot set headers on a WebSocket handshake.
	return r.URL.Query().Get("token")
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// HashToken stores only the hash of a device token (SEC-006).
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// health and status
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	dbErr := s.deps.Store.Ping(r.Context())
	status := "ok"
	code := http.StatusOK
	if dbErr != nil {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status":  status,
		"version": Version,
		"store":   s.deps.Cfg.Database.Driver,
		"time":    time.Now().UTC(),
	})
}

// Version is stamped at build time via -ldflags.
var Version = "0.3.1-dev"

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	sessions, _ := s.deps.Sessions.ListActive(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"version":             Version,
		"providers":           s.deps.Providers.Status(),
		"workers":             s.deps.Supervisor.Status(),
		"jobs":                s.deps.Scheduler.Status(),
		"active_sessions":     len(sessions),
		"dropped_events":      s.deps.Bus.Dropped(),
		"pending_permissions": len(s.deps.Perms.Pending()),
		"adaptive_inference":  s.deps.Inference != nil,
	})
}

func (s *Server) handleInferenceStatus(w http.ResponseWriter, r *http.Request) {
	if s.deps.Inference == nil {
		writeError(w, http.StatusNotImplemented, "adaptive inference is disabled")
		return
	}
	writeJSON(w, http.StatusOK, s.deps.Inference.Status())
}

func (s *Server) handleInferencePreferences(w http.ResponseWriter, r *http.Request) {
	if s.deps.Inference == nil {
		writeError(w, http.StatusNotImplemented, "adaptive inference is disabled")
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	p := s.deps.Inference.Preferences()
	previous := p
	if err := decode(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.deps.Inference.UpdatePreferences(p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.deps.ModelSettings != nil {
		if err := s.deps.ModelSettings.SetPreferences(p); err != nil {
			_ = s.deps.Inference.UpdatePreferences(previous)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, s.deps.Inference.Status())
}

func (s *Server) handleInferenceTelemetry(w http.ResponseWriter, r *http.Request) {
	if s.deps.Inference == nil {
		writeError(w, http.StatusNotImplemented, "adaptive inference is disabled")
		return
	}
	var snap inference.Snapshot
	if err := decode(r, &snap); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.deps.Inference.UpdateTelemetry(snap)
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func decode(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<20))
	return dec.Decode(dst)
}

func storeStatus(err error) int {
	switch err {
	case nil:
		return http.StatusOK
	case store.ErrNotFound:
		return http.StatusNotFound
	case store.ErrConflict:
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func mustCategory(v string) model.Category {
	c := model.Category(v)
	for _, known := range model.AllCategories() {
		if known == c {
			return c
		}
	}
	return model.CatPreferences
}
