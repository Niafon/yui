-- pgvector 0.8 tuning (ADR-033).
--
-- Two things changed upstream since the schema was written, and both matter
-- for exactly the query shape we use.
--
-- 1. Iterative index scans. Our retrieval always filters first — by memory
--    space, by liveness, sometimes by category — and only then orders by
--    distance. That is the classic "overfiltering" case: HNSW returns its
--    ef_search candidates, the WHERE clause discards most of them, and the
--    query comes back with too few rows or none at all. pgvector 0.8 added
--    hnsw.iterative_scan, which keeps pulling candidates from the graph until
--    the filter is satisfied. Without it, a rarely used memory space could
--    silently return nothing.
--
-- 2. halfvec (float16). Halves index size and resident memory at negligible
--    recall cost for 1024 dimensions. On a machine where the LLM already owns
--    most of the RAM budget, that is the difference between an index that
--    stays hot and one that does not.

BEGIN;

-- Store the vector at half precision. The full-precision column stays for one
-- release so a rollback is possible; migration 0005 will drop it.
ALTER TABLE memory_items ADD COLUMN IF NOT EXISTS embedding_half halfvec(1024);

UPDATE memory_items
   SET embedding_half = embedding_vec::halfvec(1024)
 WHERE embedding_vec IS NOT NULL
   AND embedding_half IS NULL;

DROP INDEX IF EXISTS memory_items_embedding_idx;

CREATE INDEX IF NOT EXISTS memory_items_embedding_half_idx
    ON memory_items USING hnsw (embedding_half halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 96);

-- Database-wide default. The core also sets this per transaction with
-- SET LOCAL, because a session-level change would leak into every other query
-- on the same pooled connection.
DO $$
BEGIN
    EXECUTE format(
        'ALTER DATABASE %I SET hnsw.iterative_scan = %L',
        current_database(), 'relaxed_order');
EXCEPTION WHEN insufficient_privilege THEN
    -- Not fatal: the per-transaction SET LOCAL in the core covers the same
    -- ground. Only the convenience default is lost.
    RAISE NOTICE 'hnsw.iterative_scan not set database-wide: insufficient privilege';
END $$;

INSERT INTO schema_migrations (version) VALUES (4) ON CONFLICT DO NOTHING;

COMMIT;
