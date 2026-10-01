PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA temp_store = MEMORY;
PRAGMA secure_delete = FAST;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS identities (
    id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL,
    pronouns TEXT NOT NULL DEFAULT '', age_image INTEGER NOT NULL DEFAULT 18 CHECK(age_image >= 18),
    style_image TEXT NOT NULL DEFAULT '', speech_style TEXT NOT NULL DEFAULT '', relationship TEXT NOT NULL DEFAULT '',
    traits BLOB NOT NULL DEFAULT '{}', initiative REAL NOT NULL DEFAULT 0.3, autonomy REAL NOT NULL DEFAULT 0.3,
    development_mode TEXT NOT NULL DEFAULT 'limited', mode TEXT NOT NULL DEFAULT 'normal', presentation BLOB NOT NULL DEFAULT '{}',
    state_version INTEGER NOT NULL DEFAULT 1, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS emotional_states (
    identity_id TEXT PRIMARY KEY REFERENCES identities(id) ON DELETE CASCADE,
    valence REAL NOT NULL DEFAULT 0, arousal REAL NOT NULL DEFAULT 0.3, dominance REAL NOT NULL DEFAULT 0.5,
    label TEXT NOT NULL DEFAULT 'neutral', cause TEXT NOT NULL DEFAULT '', updated_at DATETIME NOT NULL, version INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS memory_spaces (
    id TEXT PRIMARY KEY, owner_type TEXT NOT NULL CHECK(owner_type IN ('user','identity')),
    owner_id TEXT NOT NULL, name TEXT NOT NULL, category TEXT NOT NULL, sensitivity TEXT NOT NULL DEFAULT 'normal'
);
CREATE TABLE IF NOT EXISTS memory_items (
    id TEXT PRIMARY KEY, space_id TEXT NOT NULL REFERENCES memory_spaces(id) ON DELETE CASCADE,
    identity_id TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL DEFAULT 1, type TEXT NOT NULL, category TEXT NOT NULL,
    subject TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, confidence REAL NOT NULL DEFAULT 0.5,
    importance REAL NOT NULL DEFAULT 0.5, sensitivity TEXT NOT NULL DEFAULT 'normal', status TEXT NOT NULL,
    occurred_at DATETIME NOT NULL, recorded_at DATETIME NOT NULL, expires_at DATETIME,
    pinned INTEGER NOT NULL DEFAULT 0, provenance BLOB NOT NULL DEFAULT '[]', links BLOB NOT NULL DEFAULT '[]',
    embedding BLOB, embedding_dim INTEGER NOT NULL DEFAULT 0,
    superseded_by TEXT NOT NULL DEFAULT '', deleted_at DATETIME,
    content_cipher BLOB, key_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS memory_items_space_idx ON memory_items(space_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS memory_items_subject_idx ON memory_items(space_id, subject) WHERE subject <> '';
CREATE INDEX IF NOT EXISTS memory_items_status_idx ON memory_items(status);
CREATE INDEX IF NOT EXISTS memory_items_expiry_idx ON memory_items(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS memory_items_category_idx ON memory_items(category, recorded_at DESC);

-- FTS is external-content: text lives only once in memory_items. The index is
-- derived and can be rebuilt from the durable rows at any time.
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
    subject, content, content='memory_items', content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER IF NOT EXISTS memory_fts_ai AFTER INSERT ON memory_items BEGIN
    INSERT INTO memory_fts(rowid, subject, content) VALUES (new.rowid, new.subject, new.content);
END;
CREATE TRIGGER IF NOT EXISTS memory_fts_ad AFTER DELETE ON memory_items BEGIN
    INSERT INTO memory_fts(memory_fts, rowid, subject, content) VALUES('delete', old.rowid, old.subject, old.content);
END;
CREATE TRIGGER IF NOT EXISTS memory_fts_au AFTER UPDATE OF subject, content ON memory_items BEGIN
    INSERT INTO memory_fts(memory_fts, rowid, subject, content) VALUES('delete', old.rowid, old.subject, old.content);
    INSERT INTO memory_fts(rowid, subject, content) VALUES(new.rowid, new.subject, new.content);
END;

CREATE TABLE IF NOT EXISTS memory_versions (
    memory_id TEXT NOT NULL, version INTEGER NOT NULL, content TEXT NOT NULL, status TEXT NOT NULL,
    replaced_by TEXT NOT NULL DEFAULT '', valid_from DATETIME NOT NULL, valid_to DATETIME,
    PRIMARY KEY(memory_id, version)
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY, identity_id TEXT NOT NULL, user_id TEXT NOT NULL,
    input_device TEXT NOT NULL DEFAULT '', output_device TEXT NOT NULL DEFAULT '', mode TEXT NOT NULL DEFAULT 'normal',
    state TEXT NOT NULL DEFAULT 'idle', providers BLOB NOT NULL DEFAULT '{}', summary TEXT NOT NULL DEFAULT '',
    started_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, closed_at DATETIME
);
CREATE INDEX IF NOT EXISTS sessions_open_idx ON sessions(started_at) WHERE closed_at IS NULL;
CREATE TABLE IF NOT EXISTS turns (
    id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, seq INTEGER NOT NULL,
    role TEXT NOT NULL, text TEXT NOT NULL, provider TEXT NOT NULL DEFAULT '', trace_id TEXT NOT NULL DEFAULT '',
    device_id TEXT NOT NULL DEFAULT '', started_at DATETIME NOT NULL, completed_at DATETIME NOT NULL,
    UNIQUE(session_id, seq)
);

CREATE TABLE IF NOT EXISTS devices (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, kind TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE,
    capabilities BLOB NOT NULL DEFAULT '[]', paired_at DATETIME NOT NULL, last_seen_at DATETIME NOT NULL, revoked_at DATETIME
);
CREATE TABLE IF NOT EXISTS permission_grants (
    id TEXT PRIMARY KEY, subject_kind TEXT NOT NULL, subject_id TEXT NOT NULL, category TEXT NOT NULL,
    action TEXT NOT NULL, decision TEXT NOT NULL, scope TEXT NOT NULL DEFAULT '', expires_at DATETIME, created_at DATETIME NOT NULL,
    UNIQUE(subject_kind, subject_id, category, action)
);
CREATE TABLE IF NOT EXISTS audit_records (
    id TEXT PRIMARY KEY, at DATETIME NOT NULL, actor_kind TEXT NOT NULL, actor_id TEXT NOT NULL DEFAULT '',
    identity_id TEXT NOT NULL DEFAULT '', device_id TEXT NOT NULL DEFAULT '', action TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '',
    categories BLOB NOT NULL DEFAULT '[]', provider TEXT NOT NULL DEFAULT '', tool TEXT NOT NULL DEFAULT '',
    permission TEXT NOT NULL DEFAULT '', confirmation TEXT NOT NULL DEFAULT '', result TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '', trace_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS audit_records_at_idx ON audit_records(at DESC);
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY, at DATETIME NOT NULL, type TEXT NOT NULL, source TEXT NOT NULL, session_id TEXT NOT NULL DEFAULT '',
    identity_id TEXT NOT NULL DEFAULT '', device_id TEXT NOT NULL DEFAULT '', correlation_id TEXT NOT NULL DEFAULT '',
    sensitivity TEXT NOT NULL DEFAULT 'normal', payload TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS events_session_idx ON events(session_id, at DESC);

CREATE TABLE IF NOT EXISTS media_objects (
    id TEXT PRIMARY KEY, uri TEXT NOT NULL, hash TEXT NOT NULL DEFAULT '', mime TEXT NOT NULL DEFAULT '', bytes INTEGER NOT NULL DEFAULT 0,
    encrypted INTEGER NOT NULL DEFAULT 1, source TEXT NOT NULL DEFAULT '', memory_id TEXT, created_at DATETIME NOT NULL,
    expires_at DATETIME, key_id TEXT NOT NULL DEFAULT '', nonce BLOB
);
CREATE INDEX IF NOT EXISTS media_expiry_idx ON media_objects(expires_at) WHERE expires_at IS NOT NULL;
CREATE TABLE IF NOT EXISTS plugins (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, version TEXT NOT NULL, manifest BLOB NOT NULL,
    requested_capabilities BLOB NOT NULL DEFAULT '[]', granted_capabilities BLOB NOT NULL DEFAULT '[]',
    installed_at DATETIME NOT NULL, enabled INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS provider_secrets (
    provider_id TEXT PRIMARY KEY, cipher BLOB NOT NULL, key_id TEXT NOT NULL, created_at DATETIME NOT NULL, rotated_at DATETIME
);
CREATE TABLE IF NOT EXISTS encryption_keys (
    key_id TEXT PRIMARY KEY, algorithm TEXT NOT NULL DEFAULT 'aes-256-gcm', created_at DATETIME NOT NULL,
    retired_at DATETIME, operations INTEGER NOT NULL DEFAULT 0
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (1), (3);
