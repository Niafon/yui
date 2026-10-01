// Package memstore is a file-backed implementation of store.Store. It exists
// so that a fresh checkout runs with no external service, and so that unit
// tests exercise the same contracts as the embedded SQLite store.
//
// It is not the production backend; it is a deterministic file snapshot for tests.
// See internal/store/sqlite.
package memstore

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

type dataset struct {
	Identities map[string]*model.Identity        `json:"identities"`
	Emotions   map[string]*model.EmotionalState  `json:"emotions"`
	Spaces     map[string]*model.MemorySpace     `json:"spaces"`
	Memories   map[string]*model.MemoryItem      `json:"memories"`
	Versions   map[string][]*model.MemoryVersion `json:"versions"`
	Sessions   map[string]*model.Session         `json:"sessions"`
	Turns      map[string][]*model.Turn          `json:"turns"`
	Devices    map[string]*model.Device          `json:"devices"`
	Grants     map[string]*model.Grant           `json:"grants"`
	Audit      []*model.AuditRecord              `json:"audit"`
	Events     []*model.Event                    `json:"events"`
}

func newDataset() *dataset {
	return &dataset{
		Identities: map[string]*model.Identity{},
		Emotions:   map[string]*model.EmotionalState{},
		Spaces:     map[string]*model.MemorySpace{},
		Memories:   map[string]*model.MemoryItem{},
		Versions:   map[string][]*model.MemoryVersion{},
		Sessions:   map[string]*model.Session{},
		Turns:      map[string][]*model.Turn{},
		Devices:    map[string]*model.Device{},
		Grants:     map[string]*model.Grant{},
	}
}

// Store keeps everything in memory and snapshots to disk after each write.
type Store struct {
	mu   sync.RWMutex
	file string
	d    *dataset
}

var _ store.Store = (*Store)(nil)

// Open loads the snapshot from dir (creating it when absent). An empty dir
// disables persistence, which is what tests use.
func Open(dir string) (*Store, error) {
	s := &Store{d: newDataset()}
	if dir == "" {
		return s, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s.file = filepath.Join(dir, "state.json")
	b, err := os.ReadFile(s.file)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	d := newDataset()
	if err := json.Unmarshal(b, d); err != nil {
		return nil, err
	}
	// Tolerate snapshots written by older versions.
	if d.Identities == nil {
		d.Identities = map[string]*model.Identity{}
	}
	if d.Emotions == nil {
		d.Emotions = map[string]*model.EmotionalState{}
	}
	if d.Spaces == nil {
		d.Spaces = map[string]*model.MemorySpace{}
	}
	if d.Memories == nil {
		d.Memories = map[string]*model.MemoryItem{}
	}
	if d.Versions == nil {
		d.Versions = map[string][]*model.MemoryVersion{}
	}
	if d.Sessions == nil {
		d.Sessions = map[string]*model.Session{}
	}
	if d.Turns == nil {
		d.Turns = map[string][]*model.Turn{}
	}
	if d.Devices == nil {
		d.Devices = map[string]*model.Device{}
	}
	if d.Grants == nil {
		d.Grants = map[string]*model.Grant{}
	}
	s.d = d
	return s, nil
}

// persist must be called with the write lock held.
func (s *Store) persist() error {
	if s.file == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.file)
}

func (s *Store) Identities() store.IdentityRepo    { return identityRepo{s} }
func (s *Store) Memory() store.MemoryRepo          { return memoryRepo{s} }
func (s *Store) Sessions() store.SessionRepo       { return sessionRepo{s} }
func (s *Store) Devices() store.DeviceRepo         { return deviceRepo{s} }
func (s *Store) Permissions() store.PermissionRepo { return permissionRepo{s} }
func (s *Store) Audit() store.AuditRepo            { return auditRepo{s} }
func (s *Store) Events() store.EventRepo           { return eventRepo{s} }
func (s *Store) Ping(context.Context) error        { return nil }

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persist()
}

// ---------------------------------------------------------------------------
// identities
// ---------------------------------------------------------------------------

type identityRepo struct{ s *Store }

func cloneIdentity(in *model.Identity) *model.Identity {
	out := *in
	out.Traits = model.Traits{}
	for k, v := range in.Traits {
		out.Traits[k] = v
	}
	if in.Presentation.ExpressionMap != nil {
		m := map[string]string{}
		for k, v := range in.Presentation.ExpressionMap {
			m[k] = v
		}
		out.Presentation.ExpressionMap = m
	}
	return &out
}

func (r identityRepo) Create(_ context.Context, it *model.Identity) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.d.Identities[it.ID]; ok {
		return store.ErrConflict
	}
	r.s.d.Identities[it.ID] = cloneIdentity(it)
	return r.s.persist()
}

func (r identityRepo) Get(_ context.Context, id string) (*model.Identity, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	it, ok := r.s.d.Identities[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneIdentity(it), nil
}

func (r identityRepo) List(_ context.Context) ([]*model.Identity, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := make([]*model.Identity, 0, len(r.s.d.Identities))
	for _, it := range r.s.d.Identities {
		out = append(out, cloneIdentity(it))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r identityRepo) Update(_ context.Context, it *model.Identity) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.d.Identities[it.ID]; !ok {
		return store.ErrNotFound
	}
	r.s.d.Identities[it.ID] = cloneIdentity(it)
	return r.s.persist()
}

func (r identityRepo) PutEmotion(_ context.Context, st *model.EmotionalState) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *st
	r.s.d.Emotions[st.IdentityID] = &cp
	return r.s.persist()
}

func (r identityRepo) GetEmotion(_ context.Context, identityID string) (*model.EmotionalState, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	st, ok := r.s.d.Emotions[identityID]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *st
	return &cp, nil
}

// ---------------------------------------------------------------------------
// memory
// ---------------------------------------------------------------------------

type memoryRepo struct{ s *Store }

func cloneMemory(in *model.MemoryItem) *model.MemoryItem {
	out := *in
	out.Provenance = append([]model.Provenance(nil), in.Provenance...)
	out.Links = append([]string(nil), in.Links...)
	out.Embedding = append([]float32(nil), in.Embedding...)
	return &out
}

func (r memoryRepo) CreateSpace(_ context.Context, sp *model.MemorySpace) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *sp
	r.s.d.Spaces[sp.ID] = &cp
	return r.s.persist()
}

func (r memoryRepo) Spaces(_ context.Context, ownerType, ownerID string) ([]*model.MemorySpace, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []*model.MemorySpace{}
	for _, sp := range r.s.d.Spaces {
		if ownerType != "" && sp.OwnerType != ownerType {
			continue
		}
		if ownerID != "" && sp.OwnerID != ownerID {
			continue
		}
		cp := *sp
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r memoryRepo) Put(_ context.Context, it *model.MemoryItem) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.d.Memories[it.ID] = cloneMemory(it)
	return r.s.persist()
}

func (r memoryRepo) Get(_ context.Context, id string) (*model.MemoryItem, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	it, ok := r.s.d.Memories[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneMemory(it), nil
}

func (r memoryRepo) Search(_ context.Context, q store.MemoryQuery) ([]*model.MemoryItem, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []*model.MemoryItem{}
	for _, it := range r.s.d.Memories {
		if !matches(it, q) {
			continue
		}
		out = append(out, cloneMemory(it))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RecordedAt.After(out[j].RecordedAt) })
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (r memoryRepo) LexicalSearch(ctx context.Context, text string, q store.MemoryQuery) ([]*model.MemoryItem, error) {
	q.Text = text
	return r.Search(ctx, q)
}

func matches(it *model.MemoryItem, q store.MemoryQuery) bool {
	if it.DeletedAt != nil && !q.IncludeDead {
		return false
	}
	if q.ActiveOnly && !it.Status.Active() {
		return false
	}
	if q.IdentityID != "" && it.IdentityID != "" && it.IdentityID != q.IdentityID {
		return false
	}
	if q.Subject != "" && it.Subject != q.Subject {
		return false
	}
	if len(q.SpaceIDs) > 0 && !containsString(q.SpaceIDs, it.SpaceID) {
		return false
	}
	if len(q.Categories) > 0 {
		found := false
		for _, c := range q.Categories {
			if c == it.Category {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(q.Types) > 0 {
		found := false
		for _, t := range q.Types {
			if t == it.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if q.Text != "" && !strings.Contains(strings.ToLower(it.Content), strings.ToLower(q.Text)) {
		return false
	}
	if q.From != nil && it.OccurredAt.Before(*q.From) {
		return false
	}
	if q.To != nil && it.OccurredAt.After(*q.To) {
		return false
	}
	return true
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// VectorSearch brute forces cosine distance. At dev-store scale that is
// faster than any index; production SQLite also stays exact at personal scale.
func (r memoryRepo) VectorSearch(_ context.Context, embedding []float32, q store.MemoryQuery) ([]*model.MemoryItem, error) {
	if len(embedding) == 0 {
		return nil, nil
	}
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()

	type scored struct {
		item *model.MemoryItem
		sim  float64
	}
	found := []scored{}
	for _, it := range r.s.d.Memories {
		if !matches(it, q) || len(it.Embedding) != len(embedding) {
			continue
		}
		found = append(found, scored{item: it, sim: cosine(embedding, it.Embedding)})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].sim > found[j].sim })
	limit := q.Limit
	if limit <= 0 || limit > len(found) {
		limit = len(found)
	}
	out := make([]*model.MemoryItem, 0, limit)
	for _, f := range found[:limit] {
		out = append(out, cloneMemory(f.item))
	}
	return out, nil
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

func (r memoryRepo) Supersede(_ context.Context, oldID, newID string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	old, ok := r.s.d.Memories[oldID]
	if !ok {
		return store.ErrNotFound
	}
	old.Status = model.StatusSuperseded
	old.SupersededBy = newID
	now := time.Now().UTC()
	vs := r.s.d.Versions[oldID]
	if len(vs) > 0 {
		last := vs[len(vs)-1]
		if last.ValidTo == nil {
			last.ValidTo = &now
			last.ReplacedBy = newID
		}
	}
	return r.s.persist()
}

func (r memoryRepo) AppendVersion(_ context.Context, v *model.MemoryVersion) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *v
	r.s.d.Versions[v.MemoryID] = append(r.s.d.Versions[v.MemoryID], &cp)
	return r.s.persist()
}

func (r memoryRepo) Versions(_ context.Context, memoryID string) ([]*model.MemoryVersion, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	src := r.s.d.Versions[memoryID]
	out := make([]*model.MemoryVersion, 0, len(src))
	for _, v := range src {
		cp := *v
		out = append(out, &cp)
	}
	return out, nil
}

func (r memoryRepo) SoftDelete(_ context.Context, id string, at time.Time) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	it, ok := r.s.d.Memories[id]
	if !ok {
		return store.ErrNotFound
	}
	t := at
	it.DeletedAt = &t
	return r.s.persist()
}

func (r memoryRepo) Restore(_ context.Context, id string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	it, ok := r.s.d.Memories[id]
	if !ok {
		return store.ErrNotFound
	}
	it.DeletedAt = nil
	return r.s.persist()
}

func (r memoryRepo) Purge(_ context.Context, id string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	delete(r.s.d.Memories, id)
	delete(r.s.d.Versions, id)
	return r.s.persist()
}

// ---------------------------------------------------------------------------
// sessions
// ---------------------------------------------------------------------------

type sessionRepo struct{ s *Store }

func cloneSession(in *model.Session) *model.Session {
	out := *in
	if in.Providers != nil {
		m := map[string]string{}
		for k, v := range in.Providers {
			m[k] = v
		}
		out.Providers = m
	}
	return &out
}

func (r sessionRepo) Create(_ context.Context, s *model.Session) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.d.Sessions[s.ID]; ok {
		return store.ErrConflict
	}
	r.s.d.Sessions[s.ID] = cloneSession(s)
	return r.s.persist()
}

func (r sessionRepo) Get(_ context.Context, id string) (*model.Session, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	s, ok := r.s.d.Sessions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneSession(s), nil
}

func (r sessionRepo) Update(_ context.Context, s *model.Session) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.d.Sessions[s.ID]; !ok {
		return store.ErrNotFound
	}
	r.s.d.Sessions[s.ID] = cloneSession(s)
	return r.s.persist()
}

func (r sessionRepo) ListActive(_ context.Context) ([]*model.Session, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []*model.Session{}
	for _, s := range r.s.d.Sessions {
		if s.ClosedAt == nil {
			out = append(out, cloneSession(s))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

func (r sessionRepo) AppendTurn(_ context.Context, t *model.Turn) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *t
	r.s.d.Turns[t.SessionID] = append(r.s.d.Turns[t.SessionID], &cp)
	return r.s.persist()
}

func (r sessionRepo) Turns(_ context.Context, sessionID string, limit int) ([]*model.Turn, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	src := r.s.d.Turns[sessionID]
	if limit > 0 && len(src) > limit {
		src = src[len(src)-limit:]
	}
	out := make([]*model.Turn, 0, len(src))
	for _, t := range src {
		cp := *t
		out = append(out, &cp)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// devices, permissions, audit, events
// ---------------------------------------------------------------------------

type deviceRepo struct{ s *Store }

func (r deviceRepo) Upsert(_ context.Context, d *model.Device) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *d
	cp.Capabilities = append([]string(nil), d.Capabilities...)
	r.s.d.Devices[d.ID] = &cp
	return r.s.persist()
}

func (r deviceRepo) Get(_ context.Context, id string) (*model.Device, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	d, ok := r.s.d.Devices[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *d
	return &cp, nil
}

func (r deviceRepo) List(_ context.Context) ([]*model.Device, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []*model.Device{}
	for _, d := range r.s.d.Devices {
		cp := *d
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PairedAt.Before(out[j].PairedAt) })
	return out, nil
}

func (r deviceRepo) FindByTokenHash(_ context.Context, hash string) (*model.Device, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, d := range r.s.d.Devices {
		if d.TokenHash == hash {
			cp := *d
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (r deviceRepo) Revoke(_ context.Context, id string, at time.Time) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	d, ok := r.s.d.Devices[id]
	if !ok {
		return store.ErrNotFound
	}
	t := at
	d.RevokedAt = &t
	return r.s.persist()
}

type permissionRepo struct{ s *Store }

func (r permissionRepo) Put(_ context.Context, g *model.Grant) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *g
	r.s.d.Grants[g.ID] = &cp
	return r.s.persist()
}

func (r permissionRepo) Delete(_ context.Context, id string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	delete(r.s.d.Grants, id)
	return r.s.persist()
}

func (r permissionRepo) List(_ context.Context, kind model.SubjectKind, subjectID string) ([]*model.Grant, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []*model.Grant{}
	for _, g := range r.s.d.Grants {
		if g.SubjectKind != kind || g.SubjectID != subjectID {
			continue
		}
		cp := *g
		out = append(out, &cp)
	}
	return out, nil
}

func (r permissionRepo) All(_ context.Context) ([]*model.Grant, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []*model.Grant{}
	for _, g := range r.s.d.Grants {
		cp := *g
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

type auditRepo struct{ s *Store }

func (r auditRepo) Append(_ context.Context, rec *model.AuditRecord) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *rec
	cp.Categories = append([]model.Category(nil), rec.Categories...)
	r.s.d.Audit = append(r.s.d.Audit, &cp)
	return r.s.persist()
}

func (r auditRepo) List(_ context.Context, q store.AuditQuery) ([]*model.AuditRecord, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []*model.AuditRecord{}
	for i := len(r.s.d.Audit) - 1; i >= 0; i-- {
		rec := r.s.d.Audit[i]
		if q.IdentityID != "" && rec.IdentityID != q.IdentityID {
			continue
		}
		if q.Provider != "" && rec.Provider != q.Provider {
			continue
		}
		if q.From != nil && rec.At.Before(*q.From) {
			continue
		}
		if q.To != nil && rec.At.After(*q.To) {
			continue
		}
		cp := *rec
		out = append(out, &cp)
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}
	return out, nil
}

type eventRepo struct{ s *Store }

func (r eventRepo) Append(_ context.Context, e *model.Event) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *e
	r.s.d.Events = append(r.s.d.Events, &cp)
	// The dev store is bounded; production SQLite keeps the full append-only log.
	if len(r.s.d.Events) > 20000 {
		r.s.d.Events = r.s.d.Events[len(r.s.d.Events)-20000:]
	}
	return r.s.persist()
}

func (r eventRepo) List(_ context.Context, q store.EventQuery) ([]*model.Event, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []*model.Event{}
	for i := len(r.s.d.Events) - 1; i >= 0; i-- {
		e := r.s.d.Events[i]
		if q.SessionID != "" && e.SessionID != q.SessionID {
			continue
		}
		if len(q.Types) > 0 && !containsString(q.Types, e.Type) {
			continue
		}
		if q.From != nil && e.At.Before(*q.From) {
			continue
		}
		cp := *e
		out = append(out, &cp)
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}
	return out, nil
}
