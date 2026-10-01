-- Application-level envelope encryption metadata (ADR-024, ADR-025).
--
-- PostgreSQL has no built-in TDE, so sensitive fields and all media are
-- encrypted by the core before they reach the database. The database stores
-- ciphertext plus the id of the key that wrapped it.

BEGIN;

-- Sensitive memory content is stored encrypted; `content` then holds a short
-- non-sensitive label and `content_cipher` holds the real value.
ALTER TABLE memory_items ADD COLUMN IF NOT EXISTS content_cipher bytea;
ALTER TABLE memory_items ADD COLUMN IF NOT EXISTS key_id text NOT NULL DEFAULT '';

ALTER TABLE media_objects ADD COLUMN IF NOT EXISTS key_id text NOT NULL DEFAULT '';
ALTER TABLE media_objects ADD COLUMN IF NOT EXISTS nonce bytea;

-- Provider secrets are never stored in plaintext and never in the config file
-- once the owner has entered them once.
CREATE TABLE IF NOT EXISTS provider_secrets (
    provider_id text PRIMARY KEY,
    cipher      bytea NOT NULL,
    key_id      text NOT NULL,
    created_at  timestamptz NOT NULL,
    rotated_at  timestamptz
);

-- Key metadata only: the wrapped DEK itself lives in the key file on disk,
-- so a database dump alone cannot be decrypted.
CREATE TABLE IF NOT EXISTS encryption_keys (
    key_id      text PRIMARY KEY,
    algorithm   text NOT NULL DEFAULT 'aes-256-gcm',
    created_at  timestamptz NOT NULL,
    retired_at  timestamptz,
    operations  bigint NOT NULL DEFAULT 0
);

INSERT INTO schema_migrations (version) VALUES (3) ON CONFLICT DO NOTHING;

COMMIT;
