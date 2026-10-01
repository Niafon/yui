// Package store defines the persistence contracts. Clients never touch the
// database directly (SRS 6.1); every read and write goes through these
// interfaces so the backing engine can be swapped without touching services.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/yui-companion/core/internal/model"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
)

// MemoryQuery drives search, timeline and retrieval (MEM-003, MEM-012, MEM-015).
type MemoryQuery struct {
	SpaceIDs    []string
	IdentityID  string
	Categories  []model.Category
	Types       []model.MemoryType
	Subject     string
	Text        string
	ActiveOnly  bool
	IncludeDead bool // include soft-deleted (trash view)
	From        *time.Time
	To          *time.Time
	Limit       int
}

type AuditQuery struct {
	IdentityID string
	Provider   string
	From       *time.Time
	To         *time.Time
	Limit      int
}

type EventQuery struct {
	SessionID string
	Types     []string
	From      *time.Time
	Limit     int
}

type IdentityRepo interface {
	Create(ctx context.Context, it *model.Identity) error
	Get(ctx context.Context, id string) (*model.Identity, error)
	List(ctx context.Context) ([]*model.Identity, error)
	Update(ctx context.Context, it *model.Identity) error
	PutEmotion(ctx context.Context, st *model.EmotionalState) error
	GetEmotion(ctx context.Context, identityID string) (*model.EmotionalState, error)
}

type MemoryRepo interface {
	CreateSpace(ctx context.Context, sp *model.MemorySpace) error
	Spaces(ctx context.Context, ownerType, ownerID string) ([]*model.MemorySpace, error)

	Put(ctx context.Context, it *model.MemoryItem) error
	Get(ctx context.Context, id string) (*model.MemoryItem, error)
	Search(ctx context.Context, q MemoryQuery) ([]*model.MemoryItem, error)
	// LexicalSearch returns text-ranked candidates. Production SQLite uses
	// FTS5/BM25; simpler backends may delegate to Search.
	LexicalSearch(ctx context.Context, text string, q MemoryQuery) ([]*model.MemoryItem, error)
	// VectorSearch returns the nearest candidates by embedding distance,
	// after applying the same filters as Search. The vector is an index, not
	// the source of truth: an empty result means "no index", not "no memory",
	// and callers fall back to Search (MEM-004, ADR-018).
	VectorSearch(ctx context.Context, embedding []float32, q MemoryQuery) ([]*model.MemoryItem, error)
	// Supersede marks old as superseded by new and closes its version window.
	Supersede(ctx context.Context, oldID, newID string) error
	AppendVersion(ctx context.Context, v *model.MemoryVersion) error
	Versions(ctx context.Context, memoryID string) ([]*model.MemoryVersion, error)
	// SoftDelete moves to trash; Purge removes the row and its derived data.
	SoftDelete(ctx context.Context, id string, at time.Time) error
	Restore(ctx context.Context, id string) error
	Purge(ctx context.Context, id string) error
}

type SessionRepo interface {
	Create(ctx context.Context, s *model.Session) error
	Get(ctx context.Context, id string) (*model.Session, error)
	Update(ctx context.Context, s *model.Session) error
	ListActive(ctx context.Context) ([]*model.Session, error)
	AppendTurn(ctx context.Context, t *model.Turn) error
	Turns(ctx context.Context, sessionID string, limit int) ([]*model.Turn, error)
}

type DeviceRepo interface {
	Upsert(ctx context.Context, d *model.Device) error
	Get(ctx context.Context, id string) (*model.Device, error)
	List(ctx context.Context) ([]*model.Device, error)
	FindByTokenHash(ctx context.Context, hash string) (*model.Device, error)
	Revoke(ctx context.Context, id string, at time.Time) error
}

type PermissionRepo interface {
	Put(ctx context.Context, g *model.Grant) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, kind model.SubjectKind, subjectID string) ([]*model.Grant, error)
	All(ctx context.Context) ([]*model.Grant, error)
}

type AuditRepo interface {
	Append(ctx context.Context, r *model.AuditRecord) error
	List(ctx context.Context, q AuditQuery) ([]*model.AuditRecord, error)
}

type EventRepo interface {
	Append(ctx context.Context, e *model.Event) error
	List(ctx context.Context, q EventQuery) ([]*model.Event, error)
}

// Store aggregates the repositories and owns the connection lifecycle.
type Store interface {
	Identities() IdentityRepo
	Memory() MemoryRepo
	Sessions() SessionRepo
	Devices() DeviceRepo
	Permissions() PermissionRepo
	Audit() AuditRepo
	Events() EventRepo
	Ping(ctx context.Context) error
	Close() error
}
