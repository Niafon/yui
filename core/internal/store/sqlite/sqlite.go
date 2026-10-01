// Package sqlite implements the production embedded store for Yui Core.
// SQLite is the durable source of truth; FTS5 provides exact lexical/BM25
// retrieval and the vector layer is an exact in-process derived index.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

const DriverName = "sqlite3"

type Store struct {
	db      *sql.DB
	vectors *vectorCache
}

var _ store.Store = (*Store)(nil)

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		path = "./data/yui.db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("sqlite: create data dir: %w", err)
	}
	// WAL keeps readers independent from short writes. NORMAL synchronous mode
	// is the recommended latency/durability trade-off for a local WAL database.
	dsn := "file:" + filepath.ToSlash(path) + "?_busy_timeout=5000&_foreign_keys=on&_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-8192"
	db, err := sql.Open(DriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	if err := applySchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	vc := newVectorCache()
	if err := vc.load(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: load vector cache: %w", err)
	}
	return &Store{db: db, vectors: vc}, nil
}

// New wraps an existing pool, used by tests and by tooling.
func New(db *sql.DB) *Store {
	vc := newVectorCache()
	_ = vc.load(context.Background(), db)
	return &Store{db: db, vectors: vc}
}

func (s *Store) Identities() store.IdentityRepo    { return identityRepo{s.db} }
func (s *Store) Memory() store.MemoryRepo          { return memoryRepo{db: s.db, vectors: s.vectors} }
func (s *Store) Sessions() store.SessionRepo       { return sessionRepo{s.db} }
func (s *Store) Devices() store.DeviceRepo         { return deviceRepo{s.db} }
func (s *Store) Permissions() store.PermissionRepo { return permissionRepo{s.db} }
func (s *Store) Audit() store.AuditRepo            { return auditRepo{s.db} }
func (s *Store) Events() store.EventRepo           { return eventRepo{s.db} }
func (s *Store) Ping(ctx context.Context) error    { return s.db.PingContext(ctx) }
func (s *Store) Close() error                      { return s.db.Close() }

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func encode(v any) ([]byte, error) { return json.Marshal(v) }

func decodeInto(raw []byte, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

func noRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	return err
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func timePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

// ---------------------------------------------------------------------------
// identities
// ---------------------------------------------------------------------------

type identityRepo struct{ db *sql.DB }

const identityColumns = `id, user_id, name, pronouns, age_image, style_image, speech_style,
	relationship, traits, initiative, autonomy, development_mode, mode, presentation,
	state_version, created_at, updated_at`

func (r identityRepo) Create(ctx context.Context, it *model.Identity) error {
	traits, err := encode(it.Traits)
	if err != nil {
		return err
	}
	pres, err := encode(it.Presentation)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO identities (`+identityColumns+`)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14,?15,?16,?17)`,
		it.ID, it.UserID, it.Name, it.Pronouns, it.AgeImage, it.StyleImage, it.SpeechStyle,
		it.Relationship, traits, it.Initiative, it.Autonomy, it.DevelopmentMode, it.Mode,
		pres, it.StateVersion, it.CreatedAt, it.UpdatedAt)
	return err
}

func scanIdentity(scan func(dest ...any) error) (*model.Identity, error) {
	var (
		it     model.Identity
		traits []byte
		pres   []byte
	)
	err := scan(&it.ID, &it.UserID, &it.Name, &it.Pronouns, &it.AgeImage, &it.StyleImage,
		&it.SpeechStyle, &it.Relationship, &traits, &it.Initiative, &it.Autonomy,
		&it.DevelopmentMode, &it.Mode, &pres, &it.StateVersion, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return nil, noRows(err)
	}
	if err := decodeInto(traits, &it.Traits); err != nil {
		return nil, err
	}
	if err := decodeInto(pres, &it.Presentation); err != nil {
		return nil, err
	}
	return &it, nil
}

func (r identityRepo) Get(ctx context.Context, id string) (*model.Identity, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+identityColumns+` FROM identities WHERE id=?1`, id)
	return scanIdentity(row.Scan)
}

func (r identityRepo) List(ctx context.Context) ([]*model.Identity, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+identityColumns+` FROM identities ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Identity{}
	for rows.Next() {
		it, err := scanIdentity(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r identityRepo) Update(ctx context.Context, it *model.Identity) error {
	traits, err := encode(it.Traits)
	if err != nil {
		return err
	}
	pres, err := encode(it.Presentation)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `UPDATE identities SET name=?2, pronouns=?3, age_image=?4,
		style_image=?5, speech_style=?6, relationship=?7, traits=?8, initiative=?9, autonomy=?10,
		development_mode=?11, mode=?12, presentation=?13, state_version=?14, updated_at=?15
		WHERE id=?1`,
		it.ID, it.Name, it.Pronouns, it.AgeImage, it.StyleImage, it.SpeechStyle, it.Relationship,
		traits, it.Initiative, it.Autonomy, it.DevelopmentMode, it.Mode, pres, it.StateVersion, it.UpdatedAt)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r identityRepo) PutEmotion(ctx context.Context, st *model.EmotionalState) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO emotional_states
		(identity_id, valence, arousal, dominance, label, cause, updated_at, version)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8)
		ON CONFLICT (identity_id) DO UPDATE SET valence=?2, arousal=?3, dominance=?4,
		label=?5, cause=?6, updated_at=?7, version=?8`,
		st.IdentityID, st.Valence, st.Arousal, st.Dominance, st.Label, st.Cause, st.UpdatedAt, st.Version)
	return err
}

func (r identityRepo) GetEmotion(ctx context.Context, identityID string) (*model.EmotionalState, error) {
	var st model.EmotionalState
	err := r.db.QueryRowContext(ctx, `SELECT identity_id, valence, arousal, dominance, label,
		cause, updated_at, version FROM emotional_states WHERE identity_id=?1`, identityID).
		Scan(&st.IdentityID, &st.Valence, &st.Arousal, &st.Dominance, &st.Label, &st.Cause,
			&st.UpdatedAt, &st.Version)
	if err != nil {
		return nil, noRows(err)
	}
	return &st, nil
}

// ---------------------------------------------------------------------------
// memory
// ---------------------------------------------------------------------------

type memoryRepo struct {
	db      *sql.DB
	vectors *vectorCache
}

const memoryColumns = `id, space_id, identity_id, version, type, category, subject, content,
	confidence, importance, sensitivity, status, occurred_at, recorded_at, expires_at, pinned,
	provenance, links, embedding, superseded_by, deleted_at`
const memoryColumnsQualified = `m.id, m.space_id, m.identity_id, m.version, m.type, m.category, m.subject, m.content,
	m.confidence, m.importance, m.sensitivity, m.status, m.occurred_at, m.recorded_at, m.expires_at, m.pinned,
	m.provenance, m.links, m.embedding, m.superseded_by, m.deleted_at`

func (r memoryRepo) CreateSpace(ctx context.Context, sp *model.MemorySpace) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO memory_spaces (id, owner_type, owner_id, name, category, sensitivity)
		VALUES (?1,?2,?3,?4,?5,?6) ON CONFLICT (id) DO NOTHING`,
		sp.ID, sp.OwnerType, sp.OwnerID, sp.Name, sp.Category, sp.Sensitivity)
	return err
}

func (r memoryRepo) Spaces(ctx context.Context, ownerType, ownerID string) ([]*model.MemorySpace, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, owner_type, owner_id, name, category, sensitivity
		FROM memory_spaces WHERE (?1='' OR owner_type=?1) AND (?2='' OR owner_id=?2) ORDER BY id`, ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.MemorySpace
	for rows.Next() {
		var sp model.MemorySpace
		if err := rows.Scan(&sp.ID, &sp.OwnerType, &sp.OwnerID, &sp.Name, &sp.Category, &sp.Sensitivity); err != nil {
			return nil, err
		}
		out = append(out, &sp)
	}
	return out, rows.Err()
}

func encodeEmbedding(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, len(v)*4)
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(x))
	}
	return b
}
func decodeEmbedding(b []byte) ([]float32, error) {
	if len(b) == 0 {
		return nil, nil
	}
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("sqlite: invalid embedding blob length %d", len(b))
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v, nil
}

func (r memoryRepo) Put(ctx context.Context, it *model.MemoryItem) error {
	prov, err := encode(it.Provenance)
	if err != nil {
		return err
	}
	links, err := encode(it.Links)
	if err != nil {
		return err
	}
	emb := encodeEmbedding(it.Embedding)
	_, err = r.db.ExecContext(ctx, `INSERT INTO memory_items (`+memoryColumns+`, embedding_dim)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14,?15,?16,?17,?18,?19,?20,?21,?22)
		ON CONFLICT (id) DO UPDATE SET space_id=?2, identity_id=?3, version=?4, type=?5, category=?6,
		subject=?7, content=?8, confidence=?9, importance=?10, sensitivity=?11, status=?12,
		occurred_at=?13, recorded_at=?14, expires_at=?15, pinned=?16, provenance=?17, links=?18,
		embedding=?19, superseded_by=?20, deleted_at=?21, embedding_dim=?22`,
		it.ID, it.SpaceID, it.IdentityID, it.Version, it.Type, it.Category, it.Subject, it.Content,
		it.Confidence, it.Importance, it.Sensitivity, it.Status, it.OccurredAt, it.RecordedAt,
		nullTime(it.ExpiresAt), it.Pinned, prov, links, emb, it.SupersededBy, nullTime(it.DeletedAt), len(it.Embedding))
	if err == nil && r.vectors != nil {
		r.vectors.upsert(it)
	}
	return err
}

func scanMemory(scan func(dest ...any) error) (*model.MemoryItem, error) {
	var it model.MemoryItem
	var prov, links, emb []byte
	var expires, deleted sql.NullTime
	if err := scan(&it.ID, &it.SpaceID, &it.IdentityID, &it.Version, &it.Type, &it.Category, &it.Subject, &it.Content,
		&it.Confidence, &it.Importance, &it.Sensitivity, &it.Status, &it.OccurredAt, &it.RecordedAt, &expires, &it.Pinned,
		&prov, &links, &emb, &it.SupersededBy, &deleted); err != nil {
		return nil, noRows(err)
	}
	it.ExpiresAt = timePtr(expires)
	it.DeletedAt = timePtr(deleted)
	if err := decodeInto(prov, &it.Provenance); err != nil {
		return nil, err
	}
	if err := decodeInto(links, &it.Links); err != nil {
		return nil, err
	}
	v, err := decodeEmbedding(emb)
	if err != nil {
		return nil, err
	}
	it.Embedding = v
	return &it, nil
}
func (r memoryRepo) Get(ctx context.Context, id string) (*model.MemoryItem, error) {
	return scanMemory(r.db.QueryRowContext(ctx, `SELECT `+memoryColumns+` FROM memory_items WHERE id=?1`, id).Scan)
}

func appendIn[T ~string](where *[]string, args *[]any, column string, values []T) {
	if len(values) == 0 {
		return
	}
	ph := make([]string, len(values))
	for i, v := range values {
		*args = append(*args, string(v))
		ph[i] = "?"
	}
	*where = append(*where, column+" IN ("+strings.Join(ph, ",")+")")
}
func memoryWhere(q store.MemoryQuery, alias string, includeText bool) ([]string, []any) {
	if alias != "" {
		alias += "."
	}
	var where []string
	var args []any
	add := func(clause string, v any) { where = append(where, clause); args = append(args, v) }
	if !q.IncludeDead {
		where = append(where, alias+"deleted_at IS NULL")
	}
	if q.ActiveOnly {
		where = append(where, alias+"status NOT IN ('superseded','outdated','disputed')")
	}
	if q.IdentityID != "" {
		add("("+alias+"identity_id = ? OR "+alias+"identity_id = '')", q.IdentityID)
	}
	if q.Subject != "" {
		add(alias+"subject = ?", q.Subject)
	}
	if includeText && q.Text != "" {
		add("lower("+alias+"content) LIKE lower(?)", "%"+q.Text+"%")
	}
	appendIn(&where, &args, alias+"space_id", q.SpaceIDs)
	appendIn(&where, &args, alias+"category", q.Categories)
	appendIn(&where, &args, alias+"type", q.Types)
	if q.From != nil {
		add(alias+"occurred_at >= ?", *q.From)
	}
	if q.To != nil {
		add(alias+"occurred_at <= ?", *q.To)
	}
	return where, args
}
func (r memoryRepo) Search(ctx context.Context, q store.MemoryQuery) ([]*model.MemoryItem, error) {
	where, args := memoryWhere(q, "", true)
	sqlText := `SELECT ` + memoryColumns + ` FROM memory_items`
	if len(where) > 0 {
		sqlText += " WHERE " + strings.Join(where, " AND ")
	}
	sqlText += " ORDER BY recorded_at DESC"
	if q.Limit > 0 {
		sqlText += " LIMIT ?"
		args = append(args, q.Limit)
	}
	return queryMemories(ctx, r.db, sqlText, args...)
}

// LexicalSearch uses SQLite FTS5/BM25. It complements exact semantic search
// for names, numbers and wording that embeddings can miss.
func (r memoryRepo) LexicalSearch(ctx context.Context, text string, q store.MemoryQuery) ([]*model.MemoryItem, error) {
	query := ftsQuery(text)
	if query == "" {
		return nil, nil
	}
	where, args := memoryWhere(q, "m", false)
	where = append([]string{"memory_fts MATCH ?"}, where...)
	args = append([]any{query}, args...)
	sqlText := `SELECT ` + memoryColumnsQualified + ` FROM memory_fts JOIN memory_items m ON m.rowid=memory_fts.rowid WHERE ` +
		strings.Join(where, " AND ") + ` ORDER BY memory_fts.rank`
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	sqlText += " LIMIT ?"
	args = append(args, limit)
	return queryMemories(ctx, r.db, sqlText, args...)
}
func ftsQuery(text string) string {
	var tokens []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			tok := b.String()
			tokens = append(tokens, `"`+strings.ReplaceAll(tok, `"`, `""`)+`"`)
			b.Reset()
		}
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return strings.Join(tokens, " OR ")
}
func queryMemories(ctx context.Context, db *sql.DB, sqlText string, args ...any) ([]*model.MemoryItem, error) {
	rows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.MemoryItem
	for rows.Next() {
		it, err := scanMemory(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r memoryRepo) VectorSearch(ctx context.Context, embedding []float32, q store.MemoryQuery) ([]*model.MemoryItem, error) {
	if len(embedding) == 0 || r.vectors == nil {
		return nil, nil
	}
	ids := r.vectors.search(embedding, q)
	if len(ids) == 0 {
		return nil, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	items, err := queryMemories(ctx, r.db, `SELECT `+memoryColumns+` FROM memory_items WHERE id IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*model.MemoryItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	out := make([]*model.MemoryItem, 0, len(ids))
	for _, id := range ids {
		if it := byID[id]; it != nil {
			out = append(out, it)
		}
	}
	return out, nil
}

func (r memoryRepo) Supersede(ctx context.Context, oldID, newID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE memory_items SET status='superseded', superseded_by=?2 WHERE id=?1`, oldID, newID)
	if err != nil {
		return err
	}
	if err := requireAffected(res); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE memory_versions SET valid_to=CURRENT_TIMESTAMP, replaced_by=?2 WHERE memory_id=?1 AND valid_to IS NULL`, oldID, newID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if r.vectors != nil {
		r.vectors.mutate(oldID, func(e *vectorEntry) { e.status = model.StatusSuperseded })
	}
	return nil
}
func (r memoryRepo) AppendVersion(ctx context.Context, v *model.MemoryVersion) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO memory_versions(memory_id,version,content,status,replaced_by,valid_from,valid_to)
	VALUES(?1,?2,?3,?4,?5,?6,?7) ON CONFLICT(memory_id,version) DO NOTHING`, v.MemoryID, v.Version, v.Content, v.Status, v.ReplacedBy, v.ValidFrom, nullTime(v.ValidTo))
	return err
}
func (r memoryRepo) Versions(ctx context.Context, memoryID string) ([]*model.MemoryVersion, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT memory_id,version,content,status,replaced_by,valid_from,valid_to FROM memory_versions WHERE memory_id=?1 ORDER BY version`, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.MemoryVersion
	for rows.Next() {
		var v model.MemoryVersion
		var vt sql.NullTime
		if err := rows.Scan(&v.MemoryID, &v.Version, &v.Content, &v.Status, &v.ReplacedBy, &v.ValidFrom, &vt); err != nil {
			return nil, err
		}
		v.ValidTo = timePtr(vt)
		out = append(out, &v)
	}
	return out, rows.Err()
}
func (r memoryRepo) SoftDelete(ctx context.Context, id string, at time.Time) error {
	res, err := r.db.ExecContext(ctx, `UPDATE memory_items SET deleted_at=?2 WHERE id=?1`, id, at)
	if err != nil {
		return err
	}
	if err = requireAffected(res); err == nil && r.vectors != nil {
		r.vectors.mutate(id, func(e *vectorEntry) { e.deletedAt = &at })
	}
	return err
}
func (r memoryRepo) Restore(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE memory_items SET deleted_at=NULL WHERE id=?1`, id)
	if err != nil {
		return err
	}
	if err = requireAffected(res); err == nil && r.vectors != nil {
		r.vectors.mutate(id, func(e *vectorEntry) { e.deletedAt = nil })
	}
	return err
}
func (r memoryRepo) Purge(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM memory_versions WHERE memory_id=?1`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM memory_items WHERE id=?1`, id)
	if err != nil {
		return err
	}
	if err = requireAffected(res); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if r.vectors != nil {
		r.vectors.remove(id)
	}
	return nil
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// sessions
// ---------------------------------------------------------------------------

type sessionRepo struct{ db *sql.DB }

const sessionColumns = `id, identity_id, user_id, input_device, output_device, mode, state,
	providers, summary, started_at, updated_at, closed_at`

func (r sessionRepo) Create(ctx context.Context, s *model.Session) error {
	providers, err := encode(s.Providers)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO sessions (`+sessionColumns+`)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12)`,
		s.ID, s.IdentityID, s.UserID, s.InputDevice, s.OutputDevice, s.Mode, s.State,
		providers, s.Summary, s.StartedAt, s.UpdatedAt, nullTime(s.ClosedAt))
	return err
}

func scanSession(scan func(dest ...any) error) (*model.Session, error) {
	var (
		s         model.Session
		providers []byte
		closed    sql.NullTime
	)
	err := scan(&s.ID, &s.IdentityID, &s.UserID, &s.InputDevice, &s.OutputDevice, &s.Mode,
		&s.State, &providers, &s.Summary, &s.StartedAt, &s.UpdatedAt, &closed)
	if err != nil {
		return nil, noRows(err)
	}
	s.ClosedAt = timePtr(closed)
	if err := decodeInto(providers, &s.Providers); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r sessionRepo) Get(ctx context.Context, id string) (*model.Session, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id=?1`, id)
	return scanSession(row.Scan)
}

func (r sessionRepo) Update(ctx context.Context, s *model.Session) error {
	providers, err := encode(s.Providers)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `UPDATE sessions SET input_device=?2, output_device=?3,
		mode=?4, state=?5, providers=?6, summary=?7, updated_at=?8, closed_at=?9 WHERE id=?1`,
		s.ID, s.InputDevice, s.OutputDevice, s.Mode, s.State, providers, s.Summary,
		s.UpdatedAt, nullTime(s.ClosedAt))
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (r sessionRepo) ListActive(ctx context.Context) ([]*model.Session, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions
		WHERE closed_at IS NULL ORDER BY started_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Session{}
	for rows.Next() {
		s, err := scanSession(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r sessionRepo) AppendTurn(ctx context.Context, t *model.Turn) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO turns
		(id, session_id, seq, role, text, provider, trace_id, device_id, started_at, completed_at)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10)`,
		t.ID, t.SessionID, t.Seq, t.Role, t.Text, t.Provider, t.TraceID, t.DeviceID,
		t.StartedAt, t.CompletedAt)
	return err
}

func (r sessionRepo) Turns(ctx context.Context, sessionID string, limit int) ([]*model.Turn, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, session_id, seq, role, text, provider,
		trace_id, device_id, started_at, completed_at FROM turns WHERE session_id=?1
		ORDER BY seq DESC LIMIT ?2`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Turn{}
	for rows.Next() {
		var t model.Turn
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Seq, &t.Role, &t.Text, &t.Provider,
			&t.TraceID, &t.DeviceID, &t.StartedAt, &t.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Callers expect chronological order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// devices, permissions, audit, events
// ---------------------------------------------------------------------------

type deviceRepo struct{ db *sql.DB }

const deviceColumns = `id, name, kind, token_hash, capabilities, paired_at, last_seen_at, revoked_at`

func (r deviceRepo) Upsert(ctx context.Context, d *model.Device) error {
	caps, err := encode(d.Capabilities)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO devices (`+deviceColumns+`)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8)
		ON CONFLICT (id) DO UPDATE SET name=?2, kind=?3, token_hash=?4, capabilities=?5,
		last_seen_at=?7, revoked_at=?8`,
		d.ID, d.Name, d.Kind, d.TokenHash, caps, d.PairedAt, d.LastSeenAt, nullTime(d.RevokedAt))
	return err
}

func scanDevice(scan func(dest ...any) error) (*model.Device, error) {
	var (
		d       model.Device
		caps    []byte
		revoked sql.NullTime
	)
	if err := scan(&d.ID, &d.Name, &d.Kind, &d.TokenHash, &caps, &d.PairedAt, &d.LastSeenAt, &revoked); err != nil {
		return nil, noRows(err)
	}
	d.RevokedAt = timePtr(revoked)
	if err := decodeInto(caps, &d.Capabilities); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r deviceRepo) Get(ctx context.Context, id string) (*model.Device, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+deviceColumns+` FROM devices WHERE id=?1`, id)
	return scanDevice(row.Scan)
}

func (r deviceRepo) List(ctx context.Context) ([]*model.Device, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+deviceColumns+` FROM devices ORDER BY paired_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Device{}
	for rows.Next() {
		d, err := scanDevice(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r deviceRepo) FindByTokenHash(ctx context.Context, hash string) (*model.Device, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+deviceColumns+` FROM devices WHERE token_hash=?1`, hash)
	return scanDevice(row.Scan)
}

func (r deviceRepo) Revoke(ctx context.Context, id string, at time.Time) error {
	res, err := r.db.ExecContext(ctx, `UPDATE devices SET revoked_at=?2 WHERE id=?1`, id, at)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

type permissionRepo struct{ db *sql.DB }

func (r permissionRepo) Put(ctx context.Context, g *model.Grant) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO permission_grants
		(id, subject_kind, subject_id, category, action, decision, scope, expires_at, created_at)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9)
		ON CONFLICT (id) DO UPDATE SET decision=?6, scope=?7, expires_at=?8`,
		g.ID, g.SubjectKind, g.SubjectID, g.Category, g.Action, g.Decision, g.Scope,
		nullTime(g.ExpiresAt), g.CreatedAt)
	return err
}

func (r permissionRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM permission_grants WHERE id=?1`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func scanGrants(rows *sql.Rows) ([]*model.Grant, error) {
	defer rows.Close()
	out := []*model.Grant{}
	for rows.Next() {
		var (
			g       model.Grant
			expires sql.NullTime
		)
		if err := rows.Scan(&g.ID, &g.SubjectKind, &g.SubjectID, &g.Category, &g.Action,
			&g.Decision, &g.Scope, &expires, &g.CreatedAt); err != nil {
			return nil, err
		}
		g.ExpiresAt = timePtr(expires)
		out = append(out, &g)
	}
	return out, rows.Err()
}

const grantColumns = `id, subject_kind, subject_id, category, action, decision, scope, expires_at, created_at`

func (r permissionRepo) List(ctx context.Context, kind model.SubjectKind, subjectID string) ([]*model.Grant, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+grantColumns+` FROM permission_grants
		WHERE subject_kind=?1 AND subject_id=?2`, kind, subjectID)
	if err != nil {
		return nil, err
	}
	return scanGrants(rows)
}

func (r permissionRepo) All(ctx context.Context) ([]*model.Grant, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+grantColumns+` FROM permission_grants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return scanGrants(rows)
}

type auditRepo struct{ db *sql.DB }

func (r auditRepo) Append(ctx context.Context, rec *model.AuditRecord) error {
	cats, err := encode(rec.Categories)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO audit_records
		(id, at, actor_kind, actor_id, identity_id, device_id, action, reason, categories,
		 provider, tool, permission, confirmation, result, error, trace_id)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14,?15,?16)`,
		rec.ID, rec.At, rec.ActorKind, rec.ActorID, rec.IdentityID, rec.DeviceID, rec.Action,
		rec.Reason, cats, rec.Provider, rec.Tool, rec.Permission, rec.Confirmation,
		rec.Result, rec.Error, rec.TraceID)
	return err
}

func (r auditRepo) List(ctx context.Context, q store.AuditQuery) ([]*model.AuditRecord, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, at, actor_kind, actor_id, identity_id,
		device_id, action, reason, categories, provider, tool, permission, confirmation,
		result, error, trace_id FROM audit_records
		WHERE (?1='' OR identity_id=?1) AND (?2='' OR provider=?2)
		ORDER BY at DESC LIMIT ?3`, q.IdentityID, q.Provider, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.AuditRecord{}
	for rows.Next() {
		var (
			rec  model.AuditRecord
			cats []byte
		)
		if err := rows.Scan(&rec.ID, &rec.At, &rec.ActorKind, &rec.ActorID, &rec.IdentityID,
			&rec.DeviceID, &rec.Action, &rec.Reason, &cats, &rec.Provider, &rec.Tool,
			&rec.Permission, &rec.Confirmation, &rec.Result, &rec.Error, &rec.TraceID); err != nil {
			return nil, err
		}
		if err := decodeInto(cats, &rec.Categories); err != nil {
			return nil, err
		}
		out = append(out, &rec)
	}
	return out, rows.Err()
}

type eventRepo struct{ db *sql.DB }

func (r eventRepo) Append(ctx context.Context, e *model.Event) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO events
		(id, at, type, source, session_id, identity_id, device_id, correlation_id, sensitivity, payload)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10)`,
		e.ID, e.At, e.Type, e.Source, e.SessionID, e.IdentityID, e.DeviceID,
		e.CorrelationID, e.Sensitivity, e.Payload)
	return err
}

func (r eventRepo) List(ctx context.Context, q store.EventQuery) ([]*model.Event, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, at, type, source, session_id, identity_id,
		device_id, correlation_id, sensitivity, payload FROM events
		WHERE (?1='' OR session_id=?1) ORDER BY at DESC LIMIT ?2`, q.SessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Event{}
	for rows.Next() {
		var e model.Event
		if err := rows.Scan(&e.ID, &e.At, &e.Type, &e.Source, &e.SessionID, &e.IdentityID,
			&e.DeviceID, &e.CorrelationID, &e.Sensitivity, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
