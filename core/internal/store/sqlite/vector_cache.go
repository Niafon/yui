package sqlite

import (
	"context"
	"database/sql"
	"math"
	"sync"
	"time"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/store"
)

// vectorCache is a derived, rebuildable exact index. Vectors are normalized
// once on load/write, so cosine search becomes a single dot product per row.
// This deliberately avoids ANN while a personal-memory corpus is small enough
// that exact scan is cheap: no graph maintenance, no recall loss, no daemon.
type vectorCache struct {
	mu      sync.RWMutex
	entries []vectorEntry
	byID    map[string]int
}

type vectorEntry struct {
	id         string
	spaceID    string
	identityID string
	category   model.Category
	typ        model.MemoryType
	subject    string
	status     model.MemoryStatus
	occurredAt time.Time
	deletedAt  *time.Time
	vector     []float32 // unit-normalized
}

func newVectorCache() *vectorCache { return &vectorCache{byID: make(map[string]int)} }

func (c *vectorCache) load(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id, space_id, identity_id, category, type, subject,
        status, occurred_at, deleted_at, embedding FROM memory_items WHERE embedding IS NOT NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()

	next := make([]vectorEntry, 0)
	byID := make(map[string]int)
	for rows.Next() {
		var e vectorEntry
		var deleted sql.NullTime
		var raw []byte
		if err := rows.Scan(&e.id, &e.spaceID, &e.identityID, &e.category, &e.typ, &e.subject,
			&e.status, &e.occurredAt, &deleted, &raw); err != nil {
			return err
		}
		if deleted.Valid {
			t := deleted.Time
			e.deletedAt = &t
		}
		v, err := decodeEmbedding(raw)
		if err != nil {
			return err
		}
		e.vector = normalizeVector(v)
		if len(e.vector) > 0 {
			byID[e.id] = len(next)
			next = append(next, e)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.entries = next
	c.byID = byID
	c.mu.Unlock()
	return nil
}

func (c *vectorCache) upsert(it *model.MemoryItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(it.Embedding) == 0 {
		c.removeLocked(it.ID)
		return
	}
	e := vectorEntry{
		id: it.ID, spaceID: it.SpaceID, identityID: it.IdentityID,
		category: it.Category, typ: it.Type, subject: it.Subject, status: it.Status,
		occurredAt: it.OccurredAt, deletedAt: cloneTimePtr(it.DeletedAt),
		vector: normalizeVector(it.Embedding),
	}
	if len(e.vector) == 0 {
		c.removeLocked(it.ID)
		return
	}
	if pos, ok := c.byID[it.ID]; ok {
		c.entries[pos] = e
		return
	}
	c.byID[it.ID] = len(c.entries)
	c.entries = append(c.entries, e)
}

func (c *vectorCache) mutate(id string, fn func(*vectorEntry)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pos, ok := c.byID[id]
	if !ok {
		return
	}
	fn(&c.entries[pos])
}

func (c *vectorCache) remove(id string) {
	c.mu.Lock()
	c.removeLocked(id)
	c.mu.Unlock()
}

func (c *vectorCache) removeLocked(id string) {
	pos, ok := c.byID[id]
	if !ok {
		return
	}
	last := len(c.entries) - 1
	if pos != last {
		c.entries[pos] = c.entries[last]
		c.byID[c.entries[pos].id] = pos
	}
	c.entries[last] = vectorEntry{}
	c.entries = c.entries[:last]
	delete(c.byID, id)
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

func normalizeVector(v []float32) []float32 {
	if len(v) == 0 {
		return nil
	}
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return nil
	}
	inv := float32(1 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}

type vectorHit struct {
	id    string
	score float32
}
type hitHeap []vectorHit

func (h *hitHeap) push(x vectorHit) {
	*h = append(*h, x)
	i := len(*h) - 1
	for i > 0 {
		p := (i - 1) / 2
		if (*h)[p].score <= (*h)[i].score {
			break
		}
		(*h)[p], (*h)[i] = (*h)[i], (*h)[p]
		i = p
	}
}

func (h *hitHeap) replaceMin(x vectorHit) {
	(*h)[0] = x
	h.siftDown(0)
}

func (h *hitHeap) popMin() vectorHit {
	n := len(*h)
	x := (*h)[0]
	last := (*h)[n-1]
	*h = (*h)[:n-1]
	if n > 1 {
		(*h)[0] = last
		h.siftDown(0)
	}
	return x
}

func (h *hitHeap) siftDown(i int) {
	n := len(*h)
	for {
		l := 2*i + 1
		if l >= n {
			return
		}
		r := l + 1
		smallest := l
		if r < n && (*h)[r].score < (*h)[l].score {
			smallest = r
		}
		if (*h)[i].score <= (*h)[smallest].score {
			return
		}
		(*h)[i], (*h)[smallest] = (*h)[smallest], (*h)[i]
		i = smallest
	}
}

func (c *vectorCache) search(query []float32, q store.MemoryQuery) []string {
	if len(query) == 0 {
		return nil
	}
	// Stored vectors are unit-normalized. Dividing every dot product by the
	// same query norm cannot change top-k ordering, so avoid allocating and
	// normalizing a copy of the query on every retrieval.
	nonZero := false
	for _, x := range query {
		if x != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		return nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	capHint := limit
	if capHint > len(c.entries) {
		capHint = len(c.entries)
	}
	h := make(hitHeap, 0, capHint)
	for _, e := range c.entries {
		if len(e.vector) != len(query) || !vectorEntryMatches(e, q) {
			continue
		}
		var dot float32
		for i, x := range query {
			dot += x * e.vector[i]
		}
		hit := vectorHit{id: e.id, score: dot}
		if len(h) < limit {
			h.push(hit)
			continue
		}
		if dot > h[0].score {
			h.replaceMin(hit)
		}
	}
	hits := make([]vectorHit, len(h))
	// A min-heap pops worst→best; filling backwards yields best→worst with
	// no second sort pass.
	for i := len(hits) - 1; i >= 0; i-- {
		hits[i] = h.popMin()
	}
	ids := make([]string, len(hits))
	for i := range hits {
		ids[i] = hits[i].id
	}
	return ids
}

func vectorEntryMatches(e vectorEntry, q store.MemoryQuery) bool {
	if e.deletedAt != nil && !q.IncludeDead {
		return false
	}
	if q.ActiveOnly && !e.status.Active() {
		return false
	}
	if q.IdentityID != "" && e.identityID != "" && e.identityID != q.IdentityID {
		return false
	}
	if q.Subject != "" && e.subject != q.Subject {
		return false
	}
	if len(q.SpaceIDs) > 0 && !containsString(q.SpaceIDs, e.spaceID) {
		return false
	}
	if len(q.Categories) > 0 {
		ok := false
		for _, v := range q.Categories {
			if v == e.category {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(q.Types) > 0 {
		ok := false
		for _, v := range q.Types {
			if v == e.typ {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if q.From != nil && e.occurredAt.Before(*q.From) {
		return false
	}
	if q.To != nil && e.occurredAt.After(*q.To) {
		return false
	}
	return true
}
