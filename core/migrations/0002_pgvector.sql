-- Semantic index on pgvector with an HNSW index (ADR-018, closes TBD-06).
--
-- The vector is a derived index, never the source of truth: dropping this
-- column loses no memory, only search quality until it is rebuilt (MEM-004).
--
-- Dimension 1024 matches bge-m3 (ADR-017). Changing the embedding model means
-- re-running this migration with the new dimension and re-indexing; the
-- memories themselves are untouched.

BEGIN;

CREATE EXTENSION IF NOT EXISTS vector;

ALTER TABLE memory_items ADD COLUMN IF NOT EXISTS embedding_vec vector(1024);

-- HNSW beats IVFFlat for our access pattern: small dataset, high recall,
-- no bulk reindexing. m and ef_construction are the pgvector defaults, raised
-- slightly because build time is irrelevant at personal scale.
CREATE INDEX IF NOT EXISTS memory_items_embedding_idx
    ON memory_items USING hnsw (embedding_vec vector_cosine_ops)
    WITH (m = 16, ef_construction = 96);

-- Retrieval always filters by space and liveness before ranking, so these
-- indexes carry the filter while HNSW carries the ordering.
CREATE INDEX IF NOT EXISTS memory_items_live_idx
    ON memory_items (space_id)
    WHERE deleted_at IS NULL AND status NOT IN ('superseded', 'outdated', 'disputed');

-- Full text search in Russian, used together with vector distance for hybrid
-- retrieval: exact names and numbers are exactly what embeddings lose.
CREATE INDEX IF NOT EXISTS memory_items_fts_idx
    ON memory_items USING gin (to_tsvector('russian', content));

INSERT INTO schema_migrations (version) VALUES (2) ON CONFLICT DO NOTHING;

COMMIT;

-- Escape hatch, documented in ADR-018: when vectors exceed ~5M or p95 search
-- passes 50 ms, add pgvectorscale's StreamingDiskANN index instead of moving
-- to a separate vector service. The SQL below is the whole migration.
--
--   CREATE EXTENSION IF NOT EXISTS vectorscale CASCADE;
--   DROP INDEX memory_items_embedding_idx;
--   CREATE INDEX ON memory_items USING diskann (embedding_vec vector_cosine_ops);
