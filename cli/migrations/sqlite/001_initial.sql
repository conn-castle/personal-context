CREATE TABLE IF NOT EXISTS slides (
    id TEXT PRIMARY KEY
        CHECK (
            id GLOB '[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]'
        ),
    date TEXT NOT NULL
        CHECK (date GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
    day_order TEXT NOT NULL DEFAULT 'n',
    html_content TEXT NOT NULL,
    notes TEXT,
    project_id TEXT,
    git_remote_url TEXT,
    git_hash TEXT
        CHECK (git_hash IS NULL OR (length(git_hash) = 40 AND git_hash NOT GLOB '*[^0-9a-f]*')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_slides_date ON slides (date, day_order, id);
CREATE INDEX IF NOT EXISTS idx_slides_project ON slides (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_slides_updated ON slides (updated_at);
CREATE INDEX IF NOT EXISTS idx_slides_deleted ON slides (deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS slide_figures (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slide_id TEXT NOT NULL REFERENCES slides(id) ON DELETE CASCADE,
    filename TEXT NOT NULL CHECK (length(filename) > 0 AND instr(filename, '/') = 0),
    s3_key TEXT NOT NULL CHECK (substr(s3_key, 1, 8) = 'figures/'),
    alt_text TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (slide_id, filename)
);

CREATE INDEX IF NOT EXISTS idx_figures_slide ON slide_figures (slide_id);

CREATE TABLE IF NOT EXISTS slide_data_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slide_id TEXT NOT NULL REFERENCES slides(id) ON DELETE CASCADE,
    filename TEXT NOT NULL CHECK (length(filename) > 0 AND instr(filename, '/') = 0),
    s3_key TEXT NOT NULL CHECK (substr(s3_key, 1, 5) = 'data/'),
    size INTEGER NOT NULL CHECK (size >= 0),
    hash TEXT NOT NULL CHECK (length(hash) = 64 AND hash NOT GLOB '*[^0-9a-f]*'),
    description TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (slide_id, filename)
);

CREATE INDEX IF NOT EXISTS idx_data_files_slide ON slide_data_files (slide_id);

CREATE TABLE IF NOT EXISTS templates (
    name TEXT PRIMARY KEY,
    html_content TEXT NOT NULL,
    description TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS sync_version (
    id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    version INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO sync_version (id, version)
SELECT 1, 0
WHERE NOT EXISTS (SELECT 1 FROM sync_version WHERE id = 1);

CREATE TRIGGER IF NOT EXISTS slides_sync_bump_after_insert
AFTER INSERT ON slides
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS slides_sync_bump_after_update
AFTER UPDATE ON slides
FOR EACH ROW
WHEN
    OLD.id != NEW.id OR
    OLD.date != NEW.date OR
    OLD.day_order != NEW.day_order OR
    OLD.html_content != NEW.html_content OR
    COALESCE(OLD.notes, '') != COALESCE(NEW.notes, '') OR
    COALESCE(OLD.project_id, '') != COALESCE(NEW.project_id, '') OR
    COALESCE(OLD.git_remote_url, '') != COALESCE(NEW.git_remote_url, '') OR
    COALESCE(OLD.git_hash, '') != COALESCE(NEW.git_hash, '') OR
    COALESCE(OLD.deleted_at, '') != COALESCE(NEW.deleted_at, '')
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS slides_sync_bump_after_delete
AFTER DELETE ON slides
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS figures_sync_bump_after_insert
AFTER INSERT ON slide_figures
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS figures_sync_bump_after_update
AFTER UPDATE ON slide_figures
FOR EACH ROW
WHEN
    OLD.slide_id != NEW.slide_id OR
    OLD.filename != NEW.filename OR
    OLD.s3_key != NEW.s3_key OR
    COALESCE(OLD.alt_text, '') != COALESCE(NEW.alt_text, '')
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS figures_sync_bump_after_delete
AFTER DELETE ON slide_figures
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS data_files_sync_bump_after_insert
AFTER INSERT ON slide_data_files
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS data_files_sync_bump_after_update
AFTER UPDATE ON slide_data_files
FOR EACH ROW
WHEN
    OLD.slide_id != NEW.slide_id OR
    OLD.filename != NEW.filename OR
    OLD.s3_key != NEW.s3_key OR
    OLD.size != NEW.size OR
    OLD.hash != NEW.hash OR
    COALESCE(OLD.description, '') != COALESCE(NEW.description, '')
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS data_files_sync_bump_after_delete
AFTER DELETE ON slide_data_files
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS templates_sync_bump_after_insert
AFTER INSERT ON templates
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS templates_sync_bump_after_update
AFTER UPDATE ON templates
FOR EACH ROW
WHEN
    OLD.name != NEW.name OR
    OLD.html_content != NEW.html_content OR
    COALESCE(OLD.description, '') != COALESCE(NEW.description, '')
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS templates_sync_bump_after_delete
AFTER DELETE ON templates
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS slides_auto_update_updated_at
AFTER UPDATE ON slides
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE slides
    SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.id;
END;

CREATE TRIGGER IF NOT EXISTS templates_auto_update_updated_at
AFTER UPDATE ON templates
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE templates
    SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE name = OLD.name;
END;
