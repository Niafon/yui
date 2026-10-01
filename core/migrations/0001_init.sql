-- Yui Core schema, version 1 (SRS 17.2 logical schemas).
-- Applied by: psql -f migrations/0001_init.sql  or  make migrate
-- Every migration is forward-only and versioned (NFR-003).

BEGIN;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version     integer PRIMARY KEY,
    applied_at  timestamptz NOT NULL DEFAULT now()
);

-- identity -------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS identities (
    id               text PRIMARY KEY,
    user_id          text NOT NULL,
    name             text NOT NULL,
    pronouns         text NOT NULL DEFAULT '',
    age_image        integer NOT NULL DEFAULT 18 CHECK (age_image >= 18),
    style_image      text NOT NULL DEFAULT '',
    speech_style     text NOT NULL DEFAULT '',
    relationship     text NOT NULL DEFAULT '',
    traits           jsonb NOT NULL DEFAULT '{}'::jsonb,
    initiative       double precision NOT NULL DEFAULT 0.3,
    autonomy         double precision NOT NULL DEFAULT 0.3,
    development_mode text NOT NULL DEFAULT 'limited',
    mode             text NOT NULL DEFAULT 'normal',
    presentation     jsonb NOT NULL DEFAULT '{}'::jsonb,
    state_version    integer NOT NULL DEFAULT 1,
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS emotional_states (
    identity_id text PRIMARY KEY REFERENCES identities(id) ON DELETE CASCADE,
    valence     double precision NOT NULL DEFAULT 0,
    arousal     double precision NOT NULL DEFAULT 0.3,
    dominance   double precision NOT NULL DEFAULT 0.5,
    label       text NOT NULL DEFAULT 'neutral',
    cause       text NOT NULL DEFAULT '',
    updated_at  timestamptz NOT NULL,
    version     integer NOT NULL DEFAULT 1
);

-- memory ---------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS memory_spaces (
    id          text PRIMARY KEY,
    owner_type  text NOT NULL CHECK (owner_type IN ('user','identity')),
    owner_id    text NOT NULL,
    name        text NOT NULL,
    category    text NOT NULL,
    sensitivity text NOT NULL DEFAULT 'normal'
);

CREATE TABLE IF NOT EXISTS memory_items (
    id            text PRIMARY KEY,
    space_id      text NOT NULL REFERENCES memory_spaces(id) ON DELETE CASCADE,
    identity_id   text NOT NULL DEFAULT '',
    version       integer NOT NULL DEFAULT 1,
    type          text NOT NULL,
    category      text NOT NULL,
    subject       text NOT NULL DEFAULT '',
    content       text NOT NULL,
    confidence    double precision NOT NULL DEFAULT 0.5,
    importance    double precision NOT NULL DEFAULT 0.5,
    sensitivity   text NOT NULL DEFAULT 'normal',
    status        text NOT NULL,
    occurred_at   timestamptz NOT NULL,
    recorded_at   timestamptz NOT NULL,
    expires_at    timestamptz,
    pinned        boolean NOT NULL DEFAULT false,
    provenance    jsonb NOT NULL DEFAULT '[]'::jsonb,
    links         jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- The vector is a derived index, never the source of truth (MEM-004).
    -- TBD-06 decides between this column and pgvector.
    embedding     jsonb,
    superseded_by text NOT NULL DEFAULT '',
    deleted_at    timestamptz
);

CREATE INDEX IF NOT EXISTS memory_items_space_idx    ON memory_items (space_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS memory_items_subject_idx  ON memory_items (space_id, subject) WHERE subject <> '';
CREATE INDEX IF NOT EXISTS memory_items_status_idx   ON memory_items (status);
CREATE INDEX IF NOT EXISTS memory_items_expiry_idx   ON memory_items (expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS memory_versions (
    memory_id   text NOT NULL,
    version     integer NOT NULL,
    content     text NOT NULL,
    status      text NOT NULL,
    replaced_by text NOT NULL DEFAULT '',
    valid_from  timestamptz NOT NULL,
    valid_to    timestamptz,
    PRIMARY KEY (memory_id, version)
);

-- conversation ---------------------------------------------------------------

CREATE TABLE IF NOT EXISTS sessions (
    id            text PRIMARY KEY,
    identity_id   text NOT NULL,
    user_id       text NOT NULL,
    input_device  text NOT NULL DEFAULT '',
    output_device text NOT NULL DEFAULT '',
    mode          text NOT NULL DEFAULT 'normal',
    state         text NOT NULL DEFAULT 'idle',
    providers     jsonb NOT NULL DEFAULT '{}'::jsonb,
    summary       text NOT NULL DEFAULT '',
    started_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    closed_at     timestamptz
);

CREATE INDEX IF NOT EXISTS sessions_open_idx ON sessions (started_at) WHERE closed_at IS NULL;

CREATE TABLE IF NOT EXISTS turns (
    id           text PRIMARY KEY,
    session_id   text NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq          integer NOT NULL,
    role         text NOT NULL,
    text         text NOT NULL,
    provider     text NOT NULL DEFAULT '',
    trace_id     text NOT NULL DEFAULT '',
    device_id    text NOT NULL DEFAULT '',
    started_at   timestamptz NOT NULL,
    completed_at timestamptz NOT NULL,
    UNIQUE (session_id, seq)
);

-- devices, permissions, audit, events ----------------------------------------

CREATE TABLE IF NOT EXISTS devices (
    id           text PRIMARY KEY,
    name         text NOT NULL,
    kind         text NOT NULL,
    -- Only the hash is stored; the token is shown once at pairing (SEC-006).
    token_hash   text NOT NULL UNIQUE,
    capabilities jsonb NOT NULL DEFAULT '[]'::jsonb,
    paired_at    timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    revoked_at   timestamptz
);

CREATE TABLE IF NOT EXISTS permission_grants (
    id           text PRIMARY KEY,
    subject_kind text NOT NULL,
    subject_id   text NOT NULL,
    category     text NOT NULL,
    action       text NOT NULL,
    decision     text NOT NULL,
    scope        text NOT NULL DEFAULT '',
    expires_at   timestamptz,
    created_at   timestamptz NOT NULL,
    UNIQUE (subject_kind, subject_id, category, action)
);

CREATE TABLE IF NOT EXISTS audit_records (
    id           text PRIMARY KEY,
    at           timestamptz NOT NULL,
    actor_kind   text NOT NULL,
    actor_id     text NOT NULL DEFAULT '',
    identity_id  text NOT NULL DEFAULT '',
    device_id    text NOT NULL DEFAULT '',
    action       text NOT NULL,
    reason       text NOT NULL DEFAULT '',
    categories   jsonb NOT NULL DEFAULT '[]'::jsonb,
    provider     text NOT NULL DEFAULT '',
    tool         text NOT NULL DEFAULT '',
    permission   text NOT NULL DEFAULT '',
    confirmation text NOT NULL DEFAULT '',
    result       text NOT NULL DEFAULT '',
    error        text NOT NULL DEFAULT '',
    trace_id     text NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS audit_records_at_idx ON audit_records (at DESC);

CREATE TABLE IF NOT EXISTS events (
    id             text PRIMARY KEY,
    at             timestamptz NOT NULL,
    type           text NOT NULL,
    source         text NOT NULL,
    session_id     text NOT NULL DEFAULT '',
    identity_id    text NOT NULL DEFAULT '',
    device_id      text NOT NULL DEFAULT '',
    correlation_id text NOT NULL DEFAULT '',
    sensitivity    text NOT NULL DEFAULT 'normal',
    payload        text NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS events_session_idx ON events (session_id, at DESC);

-- media ----------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS media_objects (
    id          text PRIMARY KEY,
    uri         text NOT NULL,
    hash        text NOT NULL DEFAULT '',
    mime        text NOT NULL DEFAULT '',
    bytes       bigint NOT NULL DEFAULT 0,
    encrypted   boolean NOT NULL DEFAULT true,
    source      text NOT NULL DEFAULT '',
    memory_id   text,
    created_at  timestamptz NOT NULL,
    expires_at  timestamptz
);

CREATE INDEX IF NOT EXISTS media_expiry_idx ON media_objects (expires_at) WHERE expires_at IS NOT NULL;

-- plugins --------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS plugins (
    id                   text PRIMARY KEY,
    name                 text NOT NULL,
    version              text NOT NULL,
    manifest             jsonb NOT NULL,
    requested_capabilities jsonb NOT NULL DEFAULT '[]'::jsonb,
    granted_capabilities   jsonb NOT NULL DEFAULT '[]'::jsonb,
    installed_at         timestamptz NOT NULL,
    enabled              boolean NOT NULL DEFAULT false
);

INSERT INTO schema_migrations (version) VALUES (1) ON CONFLICT DO NOTHING;

COMMIT;
