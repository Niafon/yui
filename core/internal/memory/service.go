// Package memory implements the long term memory described in SRS 10.
//
// Two rules shape everything here: raw events and verified facts are separate
// records, and the semantic index is never the source of truth (MEM-004).
package memory

import (
	"context"
	"strings"
	"time"

	"github.com/yui-companion/core/internal/audit"
	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/eventbus"
	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/logging"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/permission"
	"github.com/yui-companion/core/internal/provider"
	"github.com/yui-companion/core/internal/store"
)

const (
	// TopicMemory carries "a memory was created/changed" so clients can show
	// the unobtrusive indicator (UC-03, CL-011).
	TopicMemory = "memory"

	// candidatePool bounds how many rows enter ranking; ranking itself is
	// cheap, fetching is not.
	candidatePool = 300

	SpaceUserProfile = "space_user_profile"
	SpaceUserGeneral = "space_user_general"
)

type Service struct {
	repo  store.MemoryRepo
	perms *permission.Engine
	audit *audit.Service
	reg   *provider.Registry
	bus   *eventbus.Bus
	cfg   config.MemoryConfig
}

func New(repo store.MemoryRepo, perms *permission.Engine, aud *audit.Service, reg *provider.Registry, bus *eventbus.Bus, cfg config.MemoryConfig) *Service {
	return &Service{repo: repo, perms: perms, audit: aud, reg: reg, bus: bus, cfg: cfg}
}

// EnsureSpaces creates the default memory spaces for a user and an identity
// (SRS 10.2, 10.3). Spaces are the unit permissions apply to (MEM-005).
func (s *Service) EnsureSpaces(ctx context.Context, userID, identityID string) error {
	defaults := []model.MemorySpace{
		{ID: SpaceUserProfile, OwnerType: "user", OwnerID: userID, Name: "profile", Category: model.CatProfile, Sensitivity: model.SensPrivate},
		{ID: SpaceUserGeneral, OwnerType: "user", OwnerID: userID, Name: "general", Category: model.CatPreferences, Sensitivity: model.SensNormal},
	}
	if identityID != "" {
		defaults = append(defaults,
			model.MemorySpace{ID: identitySpace(identityID, "episodic"), OwnerType: "identity", OwnerID: identityID, Name: "episodic", Category: model.CatConversation, Sensitivity: model.SensPrivate},
			model.MemorySpace{ID: identitySpace(identityID, "relationship"), OwnerType: "identity", OwnerID: identityID, Name: "relationship", Category: model.CatEmotions, Sensitivity: model.SensPrivate},
		)
	}
	for i := range defaults {
		sp := defaults[i]
		if err := s.repo.CreateSpace(ctx, &sp); err != nil {
			return err
		}
	}
	return nil
}

func identitySpace(identityID, name string) string { return "space_" + identityID + "_" + name }

// EpisodicSpace is where a personality keeps its own conversation memories.
func EpisodicSpace(identityID string) string { return identitySpace(identityID, "episodic") }

// Remember turns a candidate into a stored memory, superseding the previous
// value of the same subject (MEM-006, MEM-008, MEM-009).
func (s *Service) Remember(ctx context.Context, cand model.MemoryCandidate) (*model.MemoryItem, error) {
	res, err := s.perms.Check(ctx, permission.Request{
		SubjectKind: model.SubjectIdentity,
		SubjectID:   cand.IdentityID,
		Category:    cand.Category,
		Action:      model.ActionAppend,
	})
	if err != nil {
		return nil, err
	}
	if res.Decision == model.DecisionDeny {
		return nil, permissionDenied(cand.Category)
	}

	now := time.Now().UTC()
	status := model.StatusExtracted
	switch {
	case cand.RequiresConfirmation || cand.Confidence < s.cfg.MinConfirmConfidence:
		status = model.StatusNeedsConfirmation
	case cand.Confidence >= 0.9:
		status = model.StatusConfirmed
	}

	item := &model.MemoryItem{
		ID:          ids.New("mem"),
		SpaceID:     cand.SpaceID,
		IdentityID:  cand.IdentityID,
		Version:     1,
		Type:        cand.Type,
		Category:    cand.Category,
		Subject:     cand.Subject,
		Content:     strings.TrimSpace(cand.NormalizedContent),
		Confidence:  cand.Confidence,
		Importance:  cand.Importance,
		Sensitivity: cand.Sensitivity,
		Status:      status,
		OccurredAt:  now,
		RecordedAt:  now,
		Provenance:  cand.Provenance,
	}
	if ttl := s.ttlFor(cand.Type); ttl > 0 {
		exp := now.Add(ttl)
		item.ExpiresAt = &exp
	}

	// Correction path: an existing active fact about the same subject.
	var previous *model.MemoryItem
	if cand.Subject != "" {
		found, err := s.repo.Search(ctx, store.MemoryQuery{
			SpaceIDs:   []string{cand.SpaceID},
			Subject:    cand.Subject,
			ActiveOnly: true,
			Limit:      1,
		})
		if err != nil {
			return nil, err
		}
		if len(found) > 0 {
			previous = found[0]
			if strings.EqualFold(strings.TrimSpace(previous.Content), item.Content) {
				// Same value repeated: raise confidence instead of duplicating.
				previous.Confidence = minFloat(1, previous.Confidence+0.1)
				previous.RecordedAt = now
				if err := s.repo.Put(ctx, previous); err != nil {
					return nil, err
				}
				return previous, nil
			}
			item.Version = previous.Version + 1
		}
	}

	if emb := s.embed(ctx, item.Content, provider.EmbeddingDocument); emb != nil {
		item.Embedding = emb
	}
	if err := s.repo.Put(ctx, item); err != nil {
		return nil, err
	}
	if err := s.repo.AppendVersion(ctx, &model.MemoryVersion{
		MemoryID: item.ID, Version: item.Version, Content: item.Content,
		Status: item.Status, ValidFrom: now,
	}); err != nil {
		return nil, err
	}
	if previous != nil {
		if err := s.repo.Supersede(ctx, previous.ID, item.ID); err != nil {
			return nil, err
		}
	}

	s.audit.MemoryChange(ctx, cand.IdentityID, "memory.create", item.ID, item.Category)
	s.publish(item, "memory.created")
	return item, nil
}

// RetrieveRequest describes what the context builder needs for one turn.
type RetrieveRequest struct {
	SmallModel bool
	IdentityID string
	Query      string
	SpaceIDs   []string
	Categories []model.Category
	Limit      int
}

// Retrieve returns ranked memories. Candidate selection is done in the store
// (source of truth) and only the ordering uses vectors (MEM-003, MEM-004).
func (s *Service) Retrieve(ctx context.Context, req RetrieveRequest) ([]Scored, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = s.cfg.RetrievalLimit
	}
	if req.SmallModel {
		smallLimit := s.cfg.SmallRetrievalLimit
		if smallLimit <= 0 {
			smallLimit = 4
		}
		if limit > smallLimit {
			limit = smallLimit
		}
	}
	filter := store.MemoryQuery{
		SpaceIDs:   req.SpaceIDs,
		Categories: req.Categories,
		ActiveOnly: true,
		Limit:      candidatePool,
	}

	// Hybrid retrieval (ADR-018): the vector index proposes candidates, the
	// FTS5/BM25 lexical pass catches what embeddings lose — names, numbers,
	// exact wording. Both candidate sets feed one ranking.
	qvec := s.embed(ctx, req.Query, provider.EmbeddingQuery)
	seen := map[string]bool{}
	items := []*model.MemoryItem{}

	if len(qvec) > 0 {
		near, err := s.repo.VectorSearch(ctx, qvec, filter)
		if err != nil {
			// A broken index must not break recall.
			logging.From(ctx).Warn("vector search unavailable, falling back to lexical", "error", err)
		}
		for _, it := range near {
			if !seen[it.ID] {
				seen[it.ID] = true
				items = append(items, it)
			}
		}
	}

	lexical, err := s.repo.LexicalSearch(ctx, req.Query, filter)
	if err != nil {
		// Lexical indexing is derived state too. A broken FTS index must not
		// make memory unavailable; fall back to ordinary filtered recency.
		logging.From(ctx).Warn("lexical search unavailable, falling back to filtered scan", "error", err)
		lexical, err = s.repo.Search(ctx, filter)
		if err != nil {
			return nil, err
		}
	}
	for _, it := range lexical {
		if !seen[it.ID] {
			seen[it.ID] = true
			items = append(items, it)
		}
	}

	budget := s.cfg.ContextTokens
	if budget <= 0 {
		budget = 1200
	}
	if req.SmallModel {
		budget = s.cfg.SmallContextTokens
		if budget <= 0 {
			budget = 600
		}
	}
	return SelectContext(Rank(req.Query, qvec, items, time.Now().UTC(), 0), limit, budget), nil
}

func (s *Service) Search(ctx context.Context, q store.MemoryQuery) ([]*model.MemoryItem, error) {
	return s.repo.Search(ctx, q)
}

func (s *Service) Get(ctx context.Context, id string) (*model.MemoryItem, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) Versions(ctx context.Context, id string) ([]*model.MemoryVersion, error) {
	return s.repo.Versions(ctx, id)
}

// Confirm resolves a needs-confirmation record after the owner answers.
func (s *Service) Confirm(ctx context.Context, id string, correct bool) (*model.MemoryItem, error) {
	it, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if correct {
		it.Status = model.StatusConfirmed
		it.Confidence = 1
	} else {
		it.Status = model.StatusDisputed
	}
	if err := s.repo.Put(ctx, it); err != nil {
		return nil, err
	}
	s.audit.MemoryChange(ctx, it.IdentityID, "memory.confirm", it.ID, it.Category)
	s.publish(it, "memory.updated")
	return it, nil
}

func (s *Service) Pin(ctx context.Context, id string, pinned bool) error {
	it, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	it.Pinned = pinned
	if err := s.repo.Put(ctx, it); err != nil {
		return err
	}
	s.audit.MemoryChange(ctx, it.IdentityID, "memory.pin", it.ID, it.Category)
	return nil
}

// Delete moves a memory to the trash; Purge removes it and its derived data
// (MEM-012, MEM-013, SEC-014).
func (s *Service) Delete(ctx context.Context, id string) error {
	it, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.SoftDelete(ctx, id, time.Now().UTC()); err != nil {
		return err
	}
	s.audit.MemoryChange(ctx, it.IdentityID, "memory.delete", id, it.Category)
	s.publish(it, "memory.deleted")
	return nil
}

func (s *Service) Restore(ctx context.Context, id string) error {
	if err := s.repo.Restore(ctx, id); err != nil {
		return err
	}
	s.audit.MemoryChange(ctx, "", "memory.restore", id, "")
	return nil
}

func (s *Service) Purge(ctx context.Context, id string) error {
	it, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	// Derived data (embedding, version rows) is removed with the item; media
	// links are cleaned by the retention job.
	if err := s.repo.Purge(ctx, id); err != nil {
		return err
	}
	s.audit.MemoryChange(ctx, it.IdentityID, "memory.purge", id, it.Category)
	return nil
}

// ApplyRetention enforces TTL and trash policy. Pinned records are never
// removed by policy (MEM-011, MEM-017).
func (s *Service) ApplyRetention(ctx context.Context) (int, error) {
	now := time.Now().UTC()
	items, err := s.repo.Search(ctx, store.MemoryQuery{IncludeDead: true, Limit: 10000})
	if err != nil {
		return 0, err
	}
	removed := 0
	trashCutoff := now.AddDate(0, 0, -maxInt(1, s.cfg.TrashRetentionDays))
	for _, it := range items {
		if it.Pinned {
			continue
		}
		switch {
		case it.DeletedAt != nil && it.DeletedAt.Before(trashCutoff):
			if err := s.repo.Purge(ctx, it.ID); err == nil {
				removed++
			}
		case it.ExpiresAt != nil && it.ExpiresAt.Before(now):
			if err := s.repo.SoftDelete(ctx, it.ID, now); err == nil {
				removed++
			}
		}
	}
	if removed > 0 {
		logging.From(ctx).Info("memory retention applied", "removed", removed)
	}
	return removed, nil
}

func (s *Service) ttlFor(t model.MemoryType) time.Duration {
	day := 24 * time.Hour
	switch t {
	case model.MemRawTranscript:
		return time.Duration(s.cfg.RawTranscriptTTLDays) * day
	case model.MemFrame, model.MemVideoSegment:
		return time.Duration(s.cfg.FrameTTLDays) * day
	}
	return 0
}

func (s *Service) embed(ctx context.Context, text, purpose string) []float32 {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	emb, _, err := s.reg.Embeddings("")
	if err != nil {
		return nil
	}
	var vecs [][]float32
	if taskEmb, ok := emb.(provider.TaskEmbeddings); ok {
		vecs, err = taskEmb.EmbedFor(ctx, []string{text}, purpose)
	} else {
		vecs, err = emb.Embed(ctx, []string{text})
	}
	if err != nil || len(vecs) == 0 {
		logging.From(ctx).Debug("embedding unavailable, lexical ranking only", "error", err)
		return nil
	}
	return vecs[0]
}

func (s *Service) publish(it *model.MemoryItem, kind string) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(TopicMemory, model.Event{
		ID: ids.New("ev"), At: time.Now().UTC(), Type: kind, Source: "memory",
		IdentityID: it.IdentityID, Sensitivity: it.Sensitivity, Payload: it.ID,
	})
}

type deniedError struct{ cat model.Category }

func (e deniedError) Error() string { return "memory: write denied for category " + string(e.cat) }

func permissionDenied(cat model.Category) error { return deniedError{cat} }

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
