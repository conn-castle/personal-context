-- =============================================================================
-- Personal Context — Database Schema (v8)
-- =============================================================================
--
-- Design-level source of truth (Postgres dialect). The executable SQLite schema
-- is embedded in cli/internal/sqlite/sqlite_schema.sql, and the executable
-- Postgres schema is embedded in cli/internal/repository/postgres/postgres_schema.sql.
-- This file documents the intended schema structure.
--
-- Sort key: (date, day_order, id) — id is the universal tiebreaker.
-- Record ID format: {YYYYMMDD}-{8-random-hex} (e.g., 20250304-a3f2b7e1).
-- Soft deletes: deleted_at column on records (for sync and trash/restore).
-- No title column. No tags column. Project is a plain string with slash convention.
--
-- TIMEZONE RULE: All timestamps stored as UTC (TIMESTAMPTZ in Postgres,
-- ISO 8601 with Z suffix in SQLite). Date fields (record date) are stored
-- as DATE (no time component). "Today" is determined by local time at the
-- point of creation, then stored as a date. All reads convert to local
-- timezone for display.
--
-- TIMESTAMP MANAGEMENT: created_at and updated_at are DB-managed via defaults
-- and triggers. Application code does NOT set these for normal operations.
-- The auto_update_updated_at trigger bumps updated_at on any UPDATE unless
-- the UPDATE explicitly sets updated_at (for sync/import to preserve original
-- timestamps). Sync/import bypasses the trigger by providing explicit values.
--
-- MULTI-USER: The users and api_keys tables, plus the user_id column on records
-- and the per-user sync_version table, are Postgres-only. SQLite remains
-- single-user (local mode). The schema equivalence guard has exceptions for
-- these Postgres-only structures.

-- =============================================================================
-- Authentication tables (Postgres only — no SQLite equivalent)
-- =============================================================================

CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    email           TEXT NOT NULL UNIQUE,
    name            TEXT,
    password_hash   TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_keys (
    id              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_hash        TEXT NOT NULL UNIQUE,    -- SHA-256 hash of the raw key
    label           TEXT NOT NULL,           -- user-provided description
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at    TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys (user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys (key_hash) WHERE revoked_at IS NULL;

-- =============================================================================
-- Application tables
-- =============================================================================

CREATE TABLE IF NOT EXISTS projects (
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,  -- Postgres only; absent in SQLite
    id              TEXT NOT NULL CHECK (length(id) > 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at     TIMESTAMPTZ,
    PRIMARY KEY (user_id, id)
);

CREATE INDEX IF NOT EXISTS idx_projects_user ON projects (user_id);
CREATE INDEX IF NOT EXISTS idx_projects_archived ON projects (archived_at) WHERE archived_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS devices (
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,  -- Postgres only; absent in SQLite
    id              TEXT NOT NULL CHECK (length(id) > 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at     TIMESTAMPTZ,
    PRIMARY KEY (user_id, id)
);

CREATE INDEX IF NOT EXISTS idx_devices_user ON devices (user_id);
CREATE INDEX IF NOT EXISTS idx_devices_archived ON devices (archived_at) WHERE archived_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS records (
    id              TEXT PRIMARY KEY CHECK (id ~ '^\d{8}-[0-9a-f]{8}$'),
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,  -- Postgres only; absent in SQLite
    date            DATE NOT NULL,                  -- local date when record was created/assigned
    day_order       TEXT NOT NULL DEFAULT 'n',
    html_content    TEXT,
    notes           TEXT,
    project_id      TEXT NOT NULL,                  -- e.g. 'happy-ai/sleep-staging'
    source_device_id TEXT NOT NULL,
    source_ref      TEXT,
    git_remote_url  TEXT,                           -- e.g. 'https://github.com/org/repo'
    git_hash        TEXT CHECK (git_hash ~ '^[0-9a-f]{40}$'),  -- full SHA-1 commit hash (40 hex chars)
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,                    -- NULL = active, non-NULL = soft deleted
    FOREIGN KEY (user_id, project_id) REFERENCES projects(user_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (user_id, source_device_id) REFERENCES devices(user_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_records_user ON records (user_id);
CREATE INDEX IF NOT EXISTS idx_records_date ON records (date, day_order, id);
CREATE INDEX IF NOT EXISTS idx_records_project ON records (project_id);
CREATE INDEX IF NOT EXISTS idx_records_source_device ON records (source_device_id);
CREATE INDEX IF NOT EXISTS idx_records_updated ON records (updated_at);
CREATE INDEX IF NOT EXISTS idx_records_deleted ON records (deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS record_figures (
    id              SERIAL PRIMARY KEY,
    record_id        TEXT NOT NULL REFERENCES records(id) ON DELETE CASCADE,
    filename        TEXT NOT NULL CHECK (length(filename) > 0 AND position('/' in filename) = 0),
    s3_key          TEXT NOT NULL CHECK (s3_key ~ '^figures/'),
    alt_text        TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (record_id, filename)
);

CREATE INDEX IF NOT EXISTS idx_figures_record ON record_figures (record_id);

-- INVARIANT: Child rows (record_figures, record_data_files) are only modified as
-- part of a parent record operation (pc add, pc edit, sync). Never independently.
-- The parent record's updated_at is the authoritative change signal for sync.
-- The sync_version triggers on child tables may cause harmless false positives
-- (version bump without discoverable record changes) during sync operations.
-- If independent child modification commands are ever added, a cross-table
-- trigger to bump parent record updated_at should be added at that time.

CREATE TABLE IF NOT EXISTS record_data_files (
    id              SERIAL PRIMARY KEY,
    record_id        TEXT NOT NULL REFERENCES records(id) ON DELETE CASCADE,
    filename        TEXT NOT NULL CHECK (length(filename) > 0 AND position('/' in filename) = 0),
    s3_key          TEXT NOT NULL CHECK (s3_key ~ '^data/'),
    size            BIGINT NOT NULL CHECK (size >= 0),
    hash            TEXT NOT NULL CHECK (hash ~ '^[0-9a-f]{64}$'),
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (record_id, filename)
);

CREATE INDEX IF NOT EXISTS idx_data_files_record ON record_data_files (record_id);

CREATE TABLE IF NOT EXISTS templates (
    name            TEXT PRIMARY KEY,
    html_content    TEXT NOT NULL,
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sync_version (
    user_id         TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,  -- per-user; SQLite uses id=1 singleton
    version         BIGINT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE FUNCTION bump_sync_version()
RETURNS TRIGGER AS $$
DECLARE
    _user_id TEXT;
BEGIN
    -- Resolve the user_id from the triggering row.
    -- For records: directly on the row. For child tables: join via record_id.
    IF TG_TABLE_NAME = 'records' THEN
        _user_id := COALESCE(NEW.user_id, OLD.user_id);
    ELSIF TG_TABLE_NAME IN ('record_figures', 'record_data_files') THEN
        SELECT user_id INTO _user_id FROM records WHERE id = COALESCE(NEW.record_id, OLD.record_id);
    ELSIF TG_TABLE_NAME IN ('projects', 'devices') THEN
        _user_id := COALESCE(NEW.user_id, OLD.user_id);
    ELSIF TG_TABLE_NAME = 'templates' THEN
        -- Templates are shared; create or bump every user's sync_version.
        INSERT INTO sync_version (user_id, version, updated_at)
        SELECT u.id, 1, NOW()
        FROM users AS u
        ON CONFLICT (user_id) DO UPDATE
        SET version = sync_version.version + 1,
            updated_at = NOW();
        RETURN COALESCE(NEW, OLD);
    END IF;
    IF _user_id IS NOT NULL THEN
        INSERT INTO sync_version (user_id, version, updated_at)
        VALUES (_user_id, 1, NOW())
        ON CONFLICT (user_id) DO UPDATE SET version = sync_version.version + 1, updated_at = NOW();
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS records_sync_bump_after_insert ON records;
CREATE TRIGGER records_sync_bump_after_insert
    AFTER INSERT ON records
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS records_sync_bump_after_update ON records;
CREATE TRIGGER records_sync_bump_after_update
    AFTER UPDATE ON records
    FOR EACH ROW
    WHEN (
        OLD.id IS DISTINCT FROM NEW.id OR
        OLD.date IS DISTINCT FROM NEW.date OR
        OLD.day_order IS DISTINCT FROM NEW.day_order OR
        OLD.html_content IS DISTINCT FROM NEW.html_content OR
        OLD.notes IS DISTINCT FROM NEW.notes OR
        OLD.project_id IS DISTINCT FROM NEW.project_id OR
        OLD.source_device_id IS DISTINCT FROM NEW.source_device_id OR
        OLD.source_ref IS DISTINCT FROM NEW.source_ref OR
        OLD.git_remote_url IS DISTINCT FROM NEW.git_remote_url OR
        OLD.git_hash IS DISTINCT FROM NEW.git_hash OR
        OLD.deleted_at IS DISTINCT FROM NEW.deleted_at
    )
    EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS records_sync_bump_after_delete ON records;
CREATE TRIGGER records_sync_bump_after_delete
    AFTER DELETE ON records
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS figures_sync_bump_after_insert ON record_figures;
CREATE TRIGGER figures_sync_bump_after_insert
    AFTER INSERT ON record_figures
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS figures_sync_bump_after_update ON record_figures;
CREATE TRIGGER figures_sync_bump_after_update
    AFTER UPDATE ON record_figures
    FOR EACH ROW
    WHEN (
        OLD.record_id IS DISTINCT FROM NEW.record_id OR
        OLD.filename IS DISTINCT FROM NEW.filename OR
        OLD.s3_key IS DISTINCT FROM NEW.s3_key OR
        OLD.alt_text IS DISTINCT FROM NEW.alt_text
    )
    EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS figures_sync_bump_after_delete ON record_figures;
CREATE TRIGGER figures_sync_bump_after_delete
    AFTER DELETE ON record_figures
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS data_files_sync_bump_after_insert ON record_data_files;
CREATE TRIGGER data_files_sync_bump_after_insert
    AFTER INSERT ON record_data_files
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS data_files_sync_bump_after_update ON record_data_files;
CREATE TRIGGER data_files_sync_bump_after_update
    AFTER UPDATE ON record_data_files
    FOR EACH ROW
    WHEN (
        OLD.record_id IS DISTINCT FROM NEW.record_id OR
        OLD.filename IS DISTINCT FROM NEW.filename OR
        OLD.s3_key IS DISTINCT FROM NEW.s3_key OR
        OLD.size IS DISTINCT FROM NEW.size OR
        OLD.hash IS DISTINCT FROM NEW.hash OR
        OLD.description IS DISTINCT FROM NEW.description
    )
    EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS data_files_sync_bump_after_delete ON record_data_files;
CREATE TRIGGER data_files_sync_bump_after_delete
    AFTER DELETE ON record_data_files
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS templates_sync_bump_after_insert ON templates;
CREATE TRIGGER templates_sync_bump_after_insert
    AFTER INSERT ON templates
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS templates_sync_bump_after_update ON templates;
CREATE TRIGGER templates_sync_bump_after_update
    AFTER UPDATE ON templates
    FOR EACH ROW
    WHEN (
        OLD.name IS DISTINCT FROM NEW.name OR
        OLD.html_content IS DISTINCT FROM NEW.html_content OR
        OLD.description IS DISTINCT FROM NEW.description
    )
    EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS templates_sync_bump_after_delete ON templates;
CREATE TRIGGER templates_sync_bump_after_delete
    AFTER DELETE ON templates
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS projects_sync_bump_after_insert ON projects;
CREATE TRIGGER projects_sync_bump_after_insert
    AFTER INSERT ON projects
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS projects_sync_bump_after_update ON projects;
CREATE TRIGGER projects_sync_bump_after_update
    AFTER UPDATE ON projects
    FOR EACH ROW
    WHEN (
        OLD.id IS DISTINCT FROM NEW.id OR
        OLD.created_at IS DISTINCT FROM NEW.created_at OR
        OLD.updated_at IS DISTINCT FROM NEW.updated_at OR
        OLD.archived_at IS DISTINCT FROM NEW.archived_at
    )
    EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS projects_sync_bump_after_delete ON projects;
CREATE TRIGGER projects_sync_bump_after_delete
    AFTER DELETE ON projects
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS devices_sync_bump_after_insert ON devices;
CREATE TRIGGER devices_sync_bump_after_insert
    AFTER INSERT ON devices
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS devices_sync_bump_after_update ON devices;
CREATE TRIGGER devices_sync_bump_after_update
    AFTER UPDATE ON devices
    FOR EACH ROW
    WHEN (
        OLD.id IS DISTINCT FROM NEW.id OR
        OLD.created_at IS DISTINCT FROM NEW.created_at OR
        OLD.updated_at IS DISTINCT FROM NEW.updated_at OR
        OLD.archived_at IS DISTINCT FROM NEW.archived_at
    )
    EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS devices_sync_bump_after_delete ON devices;
CREATE TRIGGER devices_sync_bump_after_delete
    AFTER DELETE ON devices
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

-- ---------------------------------------------------------------------------
-- Auto-update updated_at on row modification.
-- When a normal UPDATE does not explicitly set updated_at, the trigger bumps
-- it to NOW(). When sync/import explicitly sets updated_at to a different
-- value (NEW.updated_at != OLD.updated_at), the trigger skips.
-- SQLite equivalent uses AFTER UPDATE trigger (see cli/internal/sqlite/sqlite_schema.sql).
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION auto_update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.updated_at = OLD.updated_at THEN
        NEW.updated_at = NOW();
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS users_auto_updated_at ON users;
CREATE TRIGGER users_auto_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION auto_update_updated_at();

DROP TRIGGER IF EXISTS records_auto_updated_at ON records;
CREATE TRIGGER records_auto_updated_at
    BEFORE UPDATE ON records
    FOR EACH ROW EXECUTE FUNCTION auto_update_updated_at();

DROP TRIGGER IF EXISTS templates_auto_updated_at ON templates;
CREATE TRIGGER templates_auto_updated_at
    BEFORE UPDATE ON templates
    FOR EACH ROW EXECUTE FUNCTION auto_update_updated_at();

DROP TRIGGER IF EXISTS projects_auto_updated_at ON projects;
CREATE TRIGGER projects_auto_updated_at
    BEFORE UPDATE ON projects
    FOR EACH ROW EXECUTE FUNCTION auto_update_updated_at();

DROP TRIGGER IF EXISTS devices_auto_updated_at ON devices;
CREATE TRIGGER devices_auto_updated_at
    BEFORE UPDATE ON devices
    FOR EACH ROW EXECUTE FUNCTION auto_update_updated_at();
