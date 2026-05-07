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
    key_hash        TEXT NOT NULL UNIQUE,
    label           TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at    TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys (user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys (key_hash) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS slides (
    id              TEXT PRIMARY KEY CHECK (id ~ '^\d{8}-[0-9a-f]{8}$'),
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    date            DATE NOT NULL,
    day_order       TEXT NOT NULL DEFAULT 'n',
    html_content    TEXT NOT NULL,
    notes           TEXT,
    project_id      TEXT,
    git_remote_url  TEXT,
    git_hash        TEXT CHECK (git_hash ~ '^[0-9a-f]{40}$'),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_slides_user ON slides (user_id);
CREATE INDEX IF NOT EXISTS idx_slides_date ON slides (date, day_order, id);
CREATE INDEX IF NOT EXISTS idx_slides_project ON slides (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_slides_updated ON slides (updated_at);
CREATE INDEX IF NOT EXISTS idx_slides_deleted ON slides (deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS slide_figures (
    id              SERIAL PRIMARY KEY,
    slide_id        TEXT NOT NULL REFERENCES slides(id) ON DELETE CASCADE,
    filename        TEXT NOT NULL CHECK (length(filename) > 0 AND position('/' in filename) = 0),
    s3_key          TEXT NOT NULL CHECK (s3_key ~ '^figures/'),
    alt_text        TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (slide_id, filename)
);

CREATE INDEX IF NOT EXISTS idx_figures_slide ON slide_figures (slide_id);

CREATE TABLE IF NOT EXISTS slide_data_files (
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

CREATE INDEX IF NOT EXISTS idx_data_files_slide ON slide_data_files (slide_id);

CREATE TABLE IF NOT EXISTS templates (
    name            TEXT PRIMARY KEY,
    html_content    TEXT NOT NULL,
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sync_version (
    user_id         TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    version         BIGINT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE FUNCTION bump_sync_version()
RETURNS TRIGGER AS $$
DECLARE
    _user_id TEXT;
BEGIN
    IF TG_TABLE_NAME = 'slides' THEN
        _user_id := COALESCE(NEW.user_id, OLD.user_id);
    ELSIF TG_TABLE_NAME IN ('slide_figures', 'slide_data_files') THEN
        SELECT user_id INTO _user_id FROM slides WHERE id = COALESCE(NEW.slide_id, OLD.slide_id);
    ELSIF TG_TABLE_NAME = 'templates' THEN
        UPDATE sync_version SET version = version + 1, updated_at = NOW();
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

DROP TRIGGER IF EXISTS slides_sync_bump_after_insert ON slides;
CREATE TRIGGER slides_sync_bump_after_insert
    AFTER INSERT ON slides
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS slides_sync_bump_after_update ON slides;
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

DROP TRIGGER IF EXISTS slides_sync_bump_after_delete ON slides;
CREATE TRIGGER slides_sync_bump_after_delete
    AFTER DELETE ON slides
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS figures_sync_bump_after_insert ON slide_figures;
CREATE TRIGGER figures_sync_bump_after_insert
    AFTER INSERT ON slide_figures
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS figures_sync_bump_after_update ON slide_figures;
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

DROP TRIGGER IF EXISTS figures_sync_bump_after_delete ON slide_figures;
CREATE TRIGGER figures_sync_bump_after_delete
    AFTER DELETE ON slide_figures
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS data_files_sync_bump_after_insert ON slide_data_files;
CREATE TRIGGER data_files_sync_bump_after_insert
    AFTER INSERT ON slide_data_files
    FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

DROP TRIGGER IF EXISTS data_files_sync_bump_after_update ON slide_data_files;
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

DROP TRIGGER IF EXISTS data_files_sync_bump_after_delete ON slide_data_files;
CREATE TRIGGER data_files_sync_bump_after_delete
    AFTER DELETE ON slide_data_files
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

DROP TRIGGER IF EXISTS slides_auto_updated_at ON slides;
CREATE TRIGGER slides_auto_updated_at
    BEFORE UPDATE ON slides
    FOR EACH ROW EXECUTE FUNCTION auto_update_updated_at();

DROP TRIGGER IF EXISTS templates_auto_updated_at ON templates;
CREATE TRIGGER templates_auto_updated_at
    BEFORE UPDATE ON templates
    FOR EACH ROW EXECUTE FUNCTION auto_update_updated_at();
