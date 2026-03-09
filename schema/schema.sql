-- =============================================================================
-- Personal Context — Database Schema (v7)
-- =============================================================================
--
-- Design-level source of truth (Postgres dialect). The executable SQLite schema
-- is embedded in cli/internal/sqlite/sqlite_schema.sql, and the executable
-- Postgres schema is embedded in cli/internal/repository/postgres/postgres_schema.sql.
-- This file documents the intended schema structure.
--
-- Sort key: (date, day_order, id) — id is the universal tiebreaker.
-- Slide ID format: {YYYYMMDD}-{8-random-hex} (e.g., 20250304-a3f2b7e1).
-- Soft deletes: deleted_at column on slides (for sync and trash/restore).
-- No title column. No tags column. Project is a plain string with slash convention.
--
-- TIMEZONE RULE: All timestamps stored as UTC (TIMESTAMPTZ in Postgres,
-- ISO 8601 with Z suffix in SQLite). Date fields (slide date) are stored
-- as DATE (no time component). "Today" is determined by local time at the
-- point of creation, then stored as a date. All reads convert to local
-- timezone for display.
--
-- TIMESTAMP MANAGEMENT: created_at and updated_at are DB-managed via defaults
-- and triggers. Application code does NOT set these for normal operations.
-- The auto_update_updated_at trigger bumps updated_at on any UPDATE unless
-- the UPDATE explicitly sets updated_at (for sync/import to preserve original
-- timestamps). Sync/import bypasses the trigger by providing explicit values.

CREATE TABLE slides (
    id              TEXT PRIMARY KEY CHECK (id ~ '^\d{8}-[0-9a-f]{8}$'),
    date            DATE NOT NULL,                  -- local date when slide was created/assigned
    day_order       TEXT NOT NULL DEFAULT 'n',
    html_content    TEXT NOT NULL,
    notes           TEXT,
    project_id      TEXT,                           -- e.g. 'happy-ai/sleep-staging'
    git_remote_url  TEXT,                           -- e.g. 'https://github.com/org/repo'
    git_hash        TEXT CHECK (git_hash ~ '^[0-9a-f]{40}$'),  -- full SHA-1 commit hash (40 hex chars)
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ                     -- NULL = active, non-NULL = soft deleted
);

CREATE INDEX idx_slides_date ON slides (date, day_order, id);
CREATE INDEX idx_slides_project ON slides (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX idx_slides_updated ON slides (updated_at);
CREATE INDEX idx_slides_deleted ON slides (deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE slide_figures (
    id              SERIAL PRIMARY KEY,
    slide_id        TEXT NOT NULL REFERENCES slides(id) ON DELETE CASCADE,
    filename        TEXT NOT NULL CHECK (length(filename) > 0 AND position('/' in filename) = 0),
    s3_key          TEXT NOT NULL CHECK (s3_key ~ '^figures/'),
    alt_text        TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (slide_id, filename)
);

CREATE INDEX idx_figures_slide ON slide_figures (slide_id);

-- INVARIANT: Child rows (slide_figures, slide_data_files) are only modified as
-- part of a parent slide operation (pc add, pc edit, sync). Never independently.
-- The parent slide's updated_at is the authoritative change signal for sync.
-- The sync_version triggers on child tables may cause harmless false positives
-- (version bump without discoverable slide changes) during sync operations.
-- If independent child modification commands are ever added, a cross-table
-- trigger to bump parent slide updated_at should be added at that time.

CREATE TABLE slide_data_files (
    id              SERIAL PRIMARY KEY,
    slide_id        TEXT NOT NULL REFERENCES slides(id) ON DELETE CASCADE,
    filename        TEXT NOT NULL CHECK (length(filename) > 0 AND position('/' in filename) = 0),
    s3_key          TEXT NOT NULL CHECK (s3_key ~ '^data/'),
    size            BIGINT NOT NULL CHECK (size >= 0),
    hash            TEXT NOT NULL CHECK (hash ~ '^[0-9a-f]{64}$'),
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (slide_id, filename)
);

CREATE INDEX idx_data_files_slide ON slide_data_files (slide_id);

CREATE TABLE templates (
    name            TEXT PRIMARY KEY,
    html_content    TEXT NOT NULL,
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE sync_version (
    id              INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    version         BIGINT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO sync_version (version) VALUES (0);

CREATE OR REPLACE FUNCTION bump_sync_version()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE sync_version SET version = version + 1, updated_at = NOW() WHERE id = 1;
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER slides_sync_bump_after_insert
    AFTER INSERT ON slides
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER slides_sync_bump_after_update
    AFTER UPDATE ON slides
    FOR EACH ROW
    WHEN (
        OLD.id IS DISTINCT FROM NEW.id OR
        OLD.date IS DISTINCT FROM NEW.date OR
        OLD.day_order IS DISTINCT FROM NEW.day_order OR
        OLD.html_content IS DISTINCT FROM NEW.html_content OR
        OLD.notes IS DISTINCT FROM NEW.notes OR
        OLD.project_id IS DISTINCT FROM NEW.project_id OR
        OLD.git_remote_url IS DISTINCT FROM NEW.git_remote_url OR
        OLD.git_hash IS DISTINCT FROM NEW.git_hash OR
        OLD.deleted_at IS DISTINCT FROM NEW.deleted_at
    )
    EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER slides_sync_bump_after_delete
    AFTER DELETE ON slides
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER figures_sync_bump_after_insert
    AFTER INSERT ON slide_figures
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER figures_sync_bump_after_update
    AFTER UPDATE ON slide_figures
    FOR EACH ROW
    WHEN (
        OLD.slide_id IS DISTINCT FROM NEW.slide_id OR
        OLD.filename IS DISTINCT FROM NEW.filename OR
        OLD.s3_key IS DISTINCT FROM NEW.s3_key OR
        OLD.alt_text IS DISTINCT FROM NEW.alt_text
    )
    EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER figures_sync_bump_after_delete
    AFTER DELETE ON slide_figures
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER data_files_sync_bump_after_insert
    AFTER INSERT ON slide_data_files
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER data_files_sync_bump_after_update
    AFTER UPDATE ON slide_data_files
    FOR EACH ROW
    WHEN (
        OLD.slide_id IS DISTINCT FROM NEW.slide_id OR
        OLD.filename IS DISTINCT FROM NEW.filename OR
        OLD.s3_key IS DISTINCT FROM NEW.s3_key OR
        OLD.size IS DISTINCT FROM NEW.size OR
        OLD.hash IS DISTINCT FROM NEW.hash OR
        OLD.description IS DISTINCT FROM NEW.description
    )
    EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER data_files_sync_bump_after_delete
    AFTER DELETE ON slide_data_files
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER templates_sync_bump_after_insert
    AFTER INSERT ON templates
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER templates_sync_bump_after_update
    AFTER UPDATE ON templates
    FOR EACH ROW
    WHEN (
        OLD.name IS DISTINCT FROM NEW.name OR
        OLD.html_content IS DISTINCT FROM NEW.html_content OR
        OLD.description IS DISTINCT FROM NEW.description
    )
    EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER templates_sync_bump_after_delete
    AFTER DELETE ON templates
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

CREATE TRIGGER slides_auto_updated_at
    BEFORE UPDATE ON slides
    FOR EACH ROW EXECUTE FUNCTION auto_update_updated_at();

CREATE TRIGGER templates_auto_updated_at
    BEFORE UPDATE ON templates
    FOR EACH ROW EXECUTE FUNCTION auto_update_updated_at();
