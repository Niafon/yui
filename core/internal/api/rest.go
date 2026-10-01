package api

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"time"

	"github.com/yui-companion/core/internal/agent"
	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/logging"
	"github.com/yui-companion/core/internal/memory"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/permission"
	"github.com/yui-companion/core/internal/store"
)

// ---------------------------------------------------------------------------
// devices
// ---------------------------------------------------------------------------

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Store.Devices().List(r.Context())
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDeviceRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.revokeDevice(r.Context(), id); err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	s.deps.Audit.Record(r.Context(), model.AuditRecord{
		ActorKind: model.SubjectDevice, ActorID: callerFrom(r.Context()).DeviceID,
		Action: "device.revoke", Reason: id, Result: "ok",
	})
	writeJSON(w, http.StatusOK, map[string]any{"revoked": id})
}

// ---------------------------------------------------------------------------
// identities
// ---------------------------------------------------------------------------

func (s *Server) handleIdentityList(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Identities.List(r.Context())
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleIdentityGet(w http.ResponseWriter, r *http.Request) {
	it, err := s.deps.Identities.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, it)
}

type identityPatch struct {
	Name            *string                 `json:"name"`
	SpeechStyle     *string                 `json:"speech_style"`
	Relationship    *string                 `json:"relationship"`
	AgeImage        *int                    `json:"age_image"`
	StyleImage      *string                 `json:"style_image"`
	Mode            *model.ConversationMode `json:"mode"`
	DevelopmentMode *model.DevelopmentMode  `json:"development_mode"`
	Initiative      *float64                `json:"initiative"`
	Traits          model.Traits            `json:"traits"`
	VoiceProfile    *string                 `json:"voice_profile"`
	Live2DPackage   *string                 `json:"live2d_package"`
}

func (s *Server) handleIdentityUpdate(w http.ResponseWriter, r *http.Request) {
	it, err := s.deps.Identities.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	var patch identityPatch
	if err := decode(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if patch.Name != nil {
		it.Name = *patch.Name
	}
	if patch.SpeechStyle != nil {
		it.SpeechStyle = *patch.SpeechStyle
	}
	if patch.Relationship != nil {
		it.Relationship = *patch.Relationship
	}
	if patch.AgeImage != nil {
		it.AgeImage = *patch.AgeImage
	}
	if patch.StyleImage != nil {
		it.StyleImage = *patch.StyleImage
	}
	if patch.Mode != nil {
		it.Mode = *patch.Mode
	}
	if patch.DevelopmentMode != nil {
		it.DevelopmentMode = *patch.DevelopmentMode
	}
	if patch.Initiative != nil {
		it.Initiative = *patch.Initiative
	}
	for k, v := range patch.Traits {
		if _, ok := it.Traits[k]; ok {
			it.Traits[k] = v
		}
	}
	if patch.VoiceProfile != nil {
		it.Presentation.VoiceProfile = *patch.VoiceProfile
	}
	if patch.Live2DPackage != nil {
		it.Presentation.Live2DPackage = *patch.Live2DPackage
	}
	if err := s.deps.Identities.Update(r.Context(), it); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) handleIdentityCheckpoint(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	_ = decode(r, &body)
	cp, err := s.deps.Identities.Checkpoint(r.Context(), r.PathValue("id"), body.Name)
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, cp)
}

func (s *Server) handleEmotion(w http.ResponseWriter, r *http.Request) {
	st, err := s.deps.Identities.Emotion(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// ---------------------------------------------------------------------------
// sessions
// ---------------------------------------------------------------------------

func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IdentityID string                 `json:"identity_id"`
		Mode       model.ConversationMode `json:"mode"`
		Providers  map[string]string      `json:"providers"`
	}
	_ = decode(r, &body)
	if body.IdentityID == "" {
		it, err := s.deps.Identities.EnsureDefault(r.Context(), s.deps.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		body.IdentityID = it.ID
	}
	if body.Mode == "" {
		body.Mode = model.ModeNormal
	}
	c := callerFrom(r.Context())
	sess, err := s.deps.Sessions.Start(r.Context(), body.IdentityID, s.deps.UserID, c.DeviceID, body.Mode, body.Providers)
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) handleSessionList(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Sessions.ListActive(r.Context())
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	sess, err := s.deps.Sessions.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleSessionTurns(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 50)
	turns, err := s.deps.Sessions.Turns(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, turns)
}

func (s *Server) handleSessionMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text   string `json:"text"`
		Visual string `json:"visual"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	c := callerFrom(r.Context())
	res, err := s.deps.Agent.HandleUserTurn(r.Context(), agent.TurnInput{
		SessionID: r.PathValue("id"), Text: body.Text, Visual: body.Visual,
		DeviceID: c.DeviceID, TraceID: logging.TraceID(r.Context()), SpeakerIsOwner: true,
	})
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSessionVision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ImageB64 string `json:"image_b64"`
		MIME     string `json:"mime"`
		Question string `json:"question"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := base64.StdEncoding.DecodeString(body.ImageB64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "image_b64 is not valid base64")
		return
	}
	sess, err := s.deps.Sessions.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	res, err := s.deps.Agent.Look(r.Context(), sess, raw, body.MIME, body.Question)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSessionProvider(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind       string `json:"kind"`
		ProviderID string `json:"provider_id"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, ok := s.deps.Providers.Config(body.ProviderID); !ok {
		writeError(w, http.StatusBadRequest, "unknown provider")
		return
	}
	if err := s.deps.Sessions.SetProvider(r.Context(), r.PathValue("id"), body.Kind, body.ProviderID); err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	s.deps.Audit.Record(r.Context(), model.AuditRecord{
		ActorKind: model.SubjectDevice, ActorID: callerFrom(r.Context()).DeviceID,
		Action: "session.provider.switch", Provider: body.ProviderID, Result: "ok",
	})
	writeJSON(w, http.StatusOK, map[string]any{"kind": body.Kind, "provider_id": body.ProviderID})
}

func (s *Server) handleSessionOutput(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceID string `json:"device_id"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.deps.Sessions.SetOutputDevice(r.Context(), r.PathValue("id"), body.DeviceID); err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output_device": body.DeviceID})
}

func (s *Server) handleSessionCancel(w http.ResponseWriter, r *http.Request) {
	s.deps.Agent.BargeIn(r.Context(), r.PathValue("id"), "button")
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": true})
}

func (s *Server) handleSessionClose(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Sessions.Close(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"closed": true})
}

// ---------------------------------------------------------------------------
// memory
// ---------------------------------------------------------------------------

func (s *Server) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	q := store.MemoryQuery{
		Text:        r.URL.Query().Get("q"),
		Subject:     r.URL.Query().Get("subject"),
		IdentityID:  r.URL.Query().Get("identity_id"),
		Limit:       intParam(r, "limit", 50),
		IncludeDead: r.URL.Query().Get("trash") == "true",
		ActiveOnly:  r.URL.Query().Get("active") == "true",
	}
	if cat := r.URL.Query().Get("category"); cat != "" {
		q.Categories = []model.Category{mustCategory(cat)}
	}
	items, err := s.deps.Memory.Search(r.Context(), q)
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleMemoryCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IdentityID string  `json:"identity_id"`
		SpaceID    string  `json:"space_id"`
		Category   string  `json:"category"`
		Subject    string  `json:"subject"`
		Content    string  `json:"content"`
		Importance float64 `json:"importance"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if body.SpaceID == "" {
		body.SpaceID = memory.SpaceUserGeneral
	}
	item, err := s.deps.Memory.Remember(r.Context(), model.MemoryCandidate{
		IdentityID:        body.IdentityID,
		SpaceID:           body.SpaceID,
		Type:              model.MemSemanticFact,
		Category:          mustCategory(body.Category),
		Subject:           body.Subject,
		NormalizedContent: body.Content,
		// Entered by the owner: confirmed by definition (SRS 10.6).
		Confidence:  1,
		Importance:  body.Importance,
		Sensitivity: model.SensNormal,
		Provenance: []model.Provenance{{
			Kind: "user", Ref: "manual", At: time.Now().UTC(),
		}},
	})
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleMemoryGet(w http.ResponseWriter, r *http.Request) {
	item, err := s.deps.Memory.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleMemoryVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.deps.Memory.Versions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) handleMemoryConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Correct bool `json:"correct"`
	}
	_ = decode(r, &body)
	item, err := s.deps.Memory.Confirm(r.Context(), r.PathValue("id"), body.Correct)
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleMemoryPin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pinned bool `json:"pinned"`
	}
	_ = decode(r, &body)
	if err := s.deps.Memory.Pin(r.Context(), r.PathValue("id"), body.Pinned); err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pinned": body.Pinned})
}

func (s *Server) handleMemoryRestore(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Memory.Restore(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"restored": true})
}

func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	hard := r.URL.Query().Get("purge") == "true"
	id := r.PathValue("id")
	var err error
	if hard {
		err = s.deps.Memory.Purge(r.Context(), id)
	} else {
		err = s.deps.Memory.Delete(r.Context(), id)
	}
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id, "purged": hard})
}

// ---------------------------------------------------------------------------
// permissions
// ---------------------------------------------------------------------------

func (s *Server) handlePermissionList(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Perms.List(r.Context())
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handlePermissionGrant(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SubjectKind string `json:"subject_kind"`
		SubjectID   string `json:"subject_id"`
		Category    string `json:"category"`
		Action      string `json:"action"`
		Decision    string `json:"decision"`
		TTLHours    int    `json:"ttl_hours"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	g := &model.Grant{
		ID:          ids.New("grant"),
		SubjectKind: model.SubjectKind(body.SubjectKind),
		SubjectID:   body.SubjectID,
		Category:    mustCategory(body.Category),
		Action:      model.Action(body.Action),
		Decision:    model.Decision(body.Decision),
		CreatedAt:   time.Now().UTC(),
	}
	if body.TTLHours > 0 {
		exp := time.Now().UTC().Add(time.Duration(body.TTLHours) * time.Hour)
		g.ExpiresAt = &exp
	}
	if err := s.deps.Perms.Grant(r.Context(), g); err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	s.deps.Audit.Record(r.Context(), model.AuditRecord{
		ActorKind: model.SubjectDevice, ActorID: callerFrom(r.Context()).DeviceID,
		Action: "permission.grant", Categories: []model.Category{g.Category},
		Permission: g.Decision, Result: "ok",
	})
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) handlePermissionRevoke(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Perms.Revoke(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": r.PathValue("id")})
}

func (s *Server) handlePermissionPending(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.deps.Perms.Pending())
}

func (s *Server) handlePermissionResolve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Allow    bool `json:"allow"`
		Remember bool `json:"remember"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.deps.Perms.Resolve(r.Context(), r.PathValue("id"), body.Allow, body.Remember); err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resolved": true, "allow": body.Allow})
}

// ---------------------------------------------------------------------------
// providers and audit
// ---------------------------------------------------------------------------

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.deps.Providers.Status())
}

func (s *Server) handleProviderDefault(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind       string `json:"kind"`
		ProviderID string `json:"provider_id"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	kind := model.ProviderKind(body.Kind)
	previous := s.deps.Providers.Default(kind)
	if err := s.deps.Providers.SetDefault(kind, body.ProviderID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.deps.ModelSettings != nil {
		if err := s.deps.ModelSettings.SetDefault(body.Kind, body.ProviderID); err != nil {
			if previous != "" {
				_ = s.deps.Providers.SetDefault(kind, previous)
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if s.deps.Inference != nil && kind != model.KindLLM {
		s.deps.Inference.SetExplicitDefault(kind, body.ProviderID)
	}
	s.deps.Audit.Record(r.Context(), model.AuditRecord{
		ActorKind: model.SubjectDevice, ActorID: callerFrom(r.Context()).DeviceID,
		Action: "provider.default", Provider: body.ProviderID, Result: "ok",
	})
	writeJSON(w, http.StatusOK, map[string]any{"kind": body.Kind, "provider_id": body.ProviderID})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Audit.List(r.Context(), store.AuditQuery{
		IdentityID: r.URL.Query().Get("identity_id"),
		Provider:   r.URL.Query().Get("provider"),
		Limit:      intParam(r, "limit", 100),
	})
	if err != nil {
		writeError(w, storeStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func intParam(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// ---------------------------------------------------------------------------
// tools
// ---------------------------------------------------------------------------

func (s *Server) handleToolList(w http.ResponseWriter, r *http.Request) {
	list := s.deps.Tools.List()
	out := make([]map[string]any, 0, len(list))
	for _, t := range list {
		out = append(out, map[string]any{
			"name": t.Name, "description": t.Description, "risk": t.Risk,
			"category": t.Category, "capability": t.Capability,
			"confirmation": permission.ConfirmationFor(t.Risk),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleToolPending(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.deps.Agent.PendingTools())
}

// handleToolConfirm is the owner answering a confirmation request. The method
// actually used is reported by the client and recorded as-is (SRS 16.7).
func (s *Server) handleToolConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Approved  bool   `json:"approved"`
		Method    string `json:"method"`
		SessionID string `json:"session_id"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := s.deps.Agent.ConfirmTool(r.Context(), body.SessionID, r.PathValue("id"),
		body.Approved, model.ConfirmationMethod(body.Method))
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": out, "approved": body.Approved})
}
