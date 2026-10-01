package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/model"
)

// Pairing follows CL-004: the PC shows a one-time code (rendered as a QR),
// the phone claims it once, and the resulting token is stored only as a hash.

const pairingTTL = 24 * time.Hour

type pendingPair struct {
	Code      string
	ExpiresAt time.Time
	Used      bool
}

type pairingStore struct {
	mu    sync.Mutex
	items map[string]*pendingPair
}

func newPairingStore() *pairingStore {
	return &pairingStore{items: map[string]*pendingPair{}}
}

func (p *pairingStore) create() *pendingPair {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gcLocked()
	item := &pendingPair{Code: ids.ShortCode(), ExpiresAt: time.Now().Add(pairingTTL)}
	p.items[item.Code] = item
	return item
}

func (p *pairingStore) claim(code string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gcLocked()
	item, ok := p.items[normalizePairingCode(code)]
	if !ok || item.Used || time.Now().After(item.ExpiresAt) {
		return false
	}
	item.Used = true
	delete(p.items, item.Code)
	return true
}

// Mobile keyboards and copied text may substitute typographic dashes or spaces.
// Normalize separators only; never guess ambiguous letters or accept partial codes.
func normalizePairingCode(code string) string {
	code = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("-–—−", r) {
			return -1
		}
		return unicode.ToUpper(r)
	}, code)
	if len(code) != 8 {
		return ""
	}
	return code[:4] + "-" + code[4:]
}

func (p *pairingStore) gcLocked() {
	now := time.Now()
	for code, item := range p.items {
		if now.After(item.ExpiresAt) {
			delete(p.items, code)
		}
	}
}

// QRPayload is what the desktop stage encodes into the QR image.
type QRPayload struct {
	Version int    `json:"v"`
	Host    string `json:"host"`
	Code    string `json:"code"`
	Name    string `json:"name"`
}

func (s *Server) handlePairStart(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r.RemoteAddr) {
		writeError(w, http.StatusForbidden, "start pairing on the command center PC")
		return
	}
	item := s.pairing.create()
	payload := QRPayload{Version: 1, Host: s.deps.Cfg.Server.Addr, Code: item.Code, Name: "Yui Core"}
	raw, _ := json.Marshal(payload)
	s.deps.Audit.Record(r.Context(), model.AuditRecord{
		ActorKind: model.SubjectDevice, ActorID: callerFrom(r.Context()).DeviceID,
		Action: "pairing.start", Result: "ok",
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"code":       item.Code,
		"expires_at": item.ExpiresAt,
		"qr_payload": string(raw),
	})
}

func (s *Server) handlePairClaim(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r.RemoteAddr) && s.deps.Cfg.Server.RequireTLSForRemote && r.TLS == nil {
		writeError(w, http.StatusUpgradeRequired, "remote connections must use TLS")
		return
	}
	var body struct {
		Code         string   `json:"code"`
		Name         string   `json:"name"`
		Kind         string   `json:"kind"`
		Capabilities []string `json:"capabilities"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.pairing.claim(body.Code) {
		// A used or expired code must fail even if replayed immediately.
		writeError(w, http.StatusForbidden, "pairing code is invalid, used or expired")
		return
	}
	token := ids.Token()
	dev := &model.Device{
		ID:           ids.New("dev"),
		Name:         firstNonEmpty(body.Name, "device"),
		Kind:         firstNonEmpty(body.Kind, "android"),
		TokenHash:    HashToken(token),
		Capabilities: body.Capabilities,
		PairedAt:     time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	}
	if err := s.deps.Store.Devices().Upsert(r.Context(), dev); err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	s.deps.Audit.Record(r.Context(), model.AuditRecord{
		ActorKind: model.SubjectDevice, ActorID: dev.ID,
		Action: "pairing.claim", Reason: dev.Kind, Result: "ok",
	})
	// The token is returned exactly once.
	writeJSON(w, http.StatusCreated, map[string]any{
		"device_id": dev.ID,
		"token":     token,
		"name":      dev.Name,
	})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
