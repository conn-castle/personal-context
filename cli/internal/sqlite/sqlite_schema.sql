CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY CHECK (length(id) > 0),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    archived_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_projects_archived ON projects (archived_at) WHERE archived_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS devices (
    id TEXT PRIMARY KEY CHECK (length(id) > 0),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    archived_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_devices_archived ON devices (archived_at) WHERE archived_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS project_paths (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path TEXT NOT NULL CHECK (length(path) > 0),
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, path, device_id)
);

CREATE INDEX IF NOT EXISTS idx_project_paths_project ON project_paths (project_id);
CREATE INDEX IF NOT EXISTS idx_project_paths_device ON project_paths (device_id);

CREATE TABLE IF NOT EXISTS records (
    id TEXT PRIMARY KEY
        CHECK (
            id GLOB '[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]'
        ),
    date TEXT NOT NULL
        CHECK (date GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
    day_order TEXT NOT NULL DEFAULT 'n',
    html_content TEXT,
    notes TEXT,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    source_device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
    source_ref TEXT,
    git_remote_url TEXT,
    git_hash TEXT
        CHECK (git_hash IS NULL OR (length(git_hash) = 40 AND git_hash NOT GLOB '*[^0-9a-f]*')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_records_date ON records (date, day_order, id);
CREATE INDEX IF NOT EXISTS idx_records_project ON records (project_id);
CREATE INDEX IF NOT EXISTS idx_records_source_device ON records (source_device_id);
CREATE INDEX IF NOT EXISTS idx_records_updated ON records (updated_at);
CREATE INDEX IF NOT EXISTS idx_records_deleted ON records (deleted_at) WHERE deleted_at IS NOT NULL;

CREATE VIRTUAL TABLE IF NOT EXISTS records_fts USING fts5(
    id UNINDEXED,
    search_text
);

CREATE TABLE IF NOT EXISTS chat_session (
    id TEXT PRIMARY KEY
        CHECK (
            id GLOB '[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]'
        ),
    source TEXT NOT NULL CHECK (length(source) > 0),
    source_session_id TEXT NOT NULL CHECK (length(source_session_id) > 0),
    parent_source_session_id TEXT,
    source_device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
    project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
    cwd TEXT,
    title TEXT,
    started_at TEXT NOT NULL,
    last_activity_at TEXT NOT NULL,
    original_source_path TEXT,
    raw_source_key TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    deleted_at TEXT,
    UNIQUE (source, source_session_id),
    CHECK (
        raw_source_key IS NULL
        OR raw_source_key = 'chats/raw/' || id || '/source.json'
        OR raw_source_key = 'chats/raw/' || id || '/source.jsonl'
        OR raw_source_key = 'chats/raw/' || id || '/source.ndjson'
    )
);

CREATE INDEX IF NOT EXISTS idx_chat_session_project ON chat_session (project_id);
CREATE INDEX IF NOT EXISTS idx_chat_session_source ON chat_session (source, source_session_id);
CREATE INDEX IF NOT EXISTS idx_chat_session_parent ON chat_session (parent_source_session_id) WHERE parent_source_session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_chat_session_device ON chat_session (source_device_id);
CREATE INDEX IF NOT EXISTS idx_chat_session_activity ON chat_session (last_activity_at);
CREATE INDEX IF NOT EXISTS idx_chat_session_deleted ON chat_session (deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS chat_item (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    role TEXT NOT NULL CHECK (length(role) > 0),
    item_type TEXT NOT NULL CHECK (length(item_type) > 0),
    text TEXT,
    search_text TEXT NOT NULL DEFAULT '',
    raw_json TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (session_id, ordinal)
);

CREATE INDEX IF NOT EXISTS idx_chat_item_session ON chat_item (session_id, ordinal);

-- chat_item remains the content table for chat_item_fts and supports bulk rebuilds.
CREATE VIRTUAL TABLE IF NOT EXISTS chat_item_fts USING fts5(
    search_text,
    content='chat_item',
    content_rowid='id'
);


CREATE TABLE IF NOT EXISTS record_figures (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    record_id TEXT NOT NULL REFERENCES records(id) ON DELETE CASCADE,
    filename TEXT NOT NULL CHECK (length(filename) > 0 AND instr(filename, '/') = 0),
    s3_key TEXT NOT NULL CHECK (substr(s3_key, 1, 8) = 'figures/'),
    alt_text TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (record_id, filename)
);

CREATE INDEX IF NOT EXISTS idx_figures_record ON record_figures (record_id);

CREATE TABLE IF NOT EXISTS record_data_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    record_id TEXT NOT NULL REFERENCES records(id) ON DELETE CASCADE,
    filename TEXT NOT NULL CHECK (length(filename) > 0 AND instr(filename, '/') = 0),
    s3_key TEXT NOT NULL CHECK (substr(s3_key, 1, 5) = 'data/'),
    size INTEGER NOT NULL CHECK (size >= 0),
    hash TEXT NOT NULL CHECK (length(hash) = 64 AND hash NOT GLOB '*[^0-9a-f]*'),
    description TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (record_id, filename)
);

CREATE INDEX IF NOT EXISTS idx_data_files_record ON record_data_files (record_id);

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

CREATE TRIGGER IF NOT EXISTS records_sync_bump_after_insert
AFTER INSERT ON records
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS records_sync_bump_after_update
AFTER UPDATE ON records
FOR EACH ROW
WHEN
    OLD.id != NEW.id OR
    OLD.date != NEW.date OR
    OLD.day_order != NEW.day_order OR
    OLD.html_content IS NOT NEW.html_content OR
    OLD.notes IS NOT NEW.notes OR
    OLD.project_id != NEW.project_id OR
    OLD.source_device_id != NEW.source_device_id OR
    OLD.source_ref IS NOT NEW.source_ref OR
    OLD.git_remote_url IS NOT NEW.git_remote_url OR
    OLD.git_hash IS NOT NEW.git_hash OR
    OLD.deleted_at IS NOT NEW.deleted_at
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS records_sync_bump_after_delete
AFTER DELETE ON records
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS records_fts_after_insert
AFTER INSERT ON records
BEGIN
    INSERT INTO records_fts (id, search_text)
    VALUES (NEW.id, COALESCE(NEW.notes, '') || ' ' || COALESCE(NEW.html_content, ''));
END;

CREATE TRIGGER IF NOT EXISTS records_fts_after_update
AFTER UPDATE ON records
FOR EACH ROW
WHEN
    OLD.id != NEW.id OR
    OLD.html_content IS NOT NEW.html_content OR
    OLD.notes IS NOT NEW.notes
BEGIN
    DELETE FROM records_fts WHERE id = OLD.id;
    INSERT INTO records_fts (id, search_text)
    VALUES (NEW.id, COALESCE(NEW.notes, '') || ' ' || COALESCE(NEW.html_content, ''));
END;

CREATE TRIGGER IF NOT EXISTS records_fts_after_delete
AFTER DELETE ON records
BEGIN
    DELETE FROM records_fts WHERE id = OLD.id;
END;

CREATE TRIGGER IF NOT EXISTS figures_sync_bump_after_insert
AFTER INSERT ON record_figures
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS figures_sync_bump_after_update
AFTER UPDATE ON record_figures
FOR EACH ROW
WHEN
    OLD.record_id != NEW.record_id OR
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
AFTER DELETE ON record_figures
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS data_files_sync_bump_after_insert
AFTER INSERT ON record_data_files
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS data_files_sync_bump_after_update
AFTER UPDATE ON record_data_files
FOR EACH ROW
WHEN
    OLD.record_id != NEW.record_id OR
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
AFTER DELETE ON record_data_files
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

CREATE TRIGGER IF NOT EXISTS projects_sync_bump_after_insert
AFTER INSERT ON projects
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS projects_sync_bump_after_update
AFTER UPDATE ON projects
FOR EACH ROW
WHEN
    OLD.id != NEW.id OR
    OLD.created_at != NEW.created_at OR
    OLD.updated_at != NEW.updated_at OR
    COALESCE(OLD.archived_at, '') != COALESCE(NEW.archived_at, '')
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS projects_sync_bump_after_delete
AFTER DELETE ON projects
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS devices_sync_bump_after_insert
AFTER INSERT ON devices
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS devices_sync_bump_after_update
AFTER UPDATE ON devices
FOR EACH ROW
WHEN
    OLD.id != NEW.id OR
    OLD.created_at != NEW.created_at OR
    OLD.updated_at != NEW.updated_at OR
    COALESCE(OLD.archived_at, '') != COALESCE(NEW.archived_at, '')
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS devices_sync_bump_after_delete
AFTER DELETE ON devices
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS project_paths_sync_bump_after_insert
AFTER INSERT ON project_paths
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS project_paths_sync_bump_after_update
AFTER UPDATE ON project_paths
FOR EACH ROW
WHEN
    OLD.project_id != NEW.project_id OR
    OLD.path != NEW.path OR
    OLD.device_id != NEW.device_id OR
    OLD.created_at != NEW.created_at OR
    OLD.updated_at != NEW.updated_at
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS project_paths_sync_bump_after_delete
AFTER DELETE ON project_paths
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS chat_session_sync_bump_after_insert
AFTER INSERT ON chat_session
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS chat_session_sync_bump_after_update
AFTER UPDATE ON chat_session
FOR EACH ROW
WHEN
    OLD.id != NEW.id OR
    OLD.source != NEW.source OR
    OLD.source_session_id != NEW.source_session_id OR
    OLD.parent_source_session_id IS NOT NEW.parent_source_session_id OR
    OLD.source_device_id != NEW.source_device_id OR
    OLD.project_id IS NOT NEW.project_id OR
    OLD.cwd IS NOT NEW.cwd OR
    OLD.title IS NOT NEW.title OR
    OLD.started_at != NEW.started_at OR
    OLD.last_activity_at != NEW.last_activity_at OR
    OLD.original_source_path IS NOT NEW.original_source_path OR
    OLD.raw_source_key IS NOT NEW.raw_source_key OR
    OLD.deleted_at IS NOT NEW.deleted_at
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS chat_session_sync_bump_after_delete
AFTER DELETE ON chat_session
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS chat_item_sync_bump_after_insert
AFTER INSERT ON chat_item
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS chat_item_sync_bump_after_update
AFTER UPDATE ON chat_item
FOR EACH ROW
WHEN
    OLD.session_id != NEW.session_id OR
    OLD.ordinal != NEW.ordinal OR
    OLD.role != NEW.role OR
    OLD.item_type != NEW.item_type OR
    OLD.text IS NOT NEW.text OR
    OLD.search_text != NEW.search_text OR
    OLD.raw_json IS NOT NEW.raw_json
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS chat_item_sync_bump_after_delete
AFTER DELETE ON chat_item
BEGIN
    UPDATE sync_version
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = 1;
END;

-- Maintain chat_item_fts for single-row writes; bulk imports drop these triggers and rebuild once.
CREATE TRIGGER IF NOT EXISTS chat_item_fts_after_insert
AFTER INSERT ON chat_item
BEGIN
    INSERT INTO chat_item_fts(rowid, search_text) VALUES (NEW.id, NEW.search_text);
END;

CREATE TRIGGER IF NOT EXISTS chat_item_fts_after_update
AFTER UPDATE ON chat_item
FOR EACH ROW
WHEN OLD.search_text != NEW.search_text
BEGIN
    INSERT INTO chat_item_fts(chat_item_fts, rowid, search_text) VALUES('delete', OLD.id, OLD.search_text);
    INSERT INTO chat_item_fts(rowid, search_text) VALUES (NEW.id, NEW.search_text);
END;

CREATE TRIGGER IF NOT EXISTS chat_item_fts_after_delete
AFTER DELETE ON chat_item
BEGIN
    INSERT INTO chat_item_fts(chat_item_fts, rowid, search_text) VALUES('delete', OLD.id, OLD.search_text);
END;

CREATE TRIGGER IF NOT EXISTS records_auto_update_updated_at
AFTER UPDATE ON records
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE records
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

CREATE TRIGGER IF NOT EXISTS projects_auto_update_updated_at
AFTER UPDATE ON projects
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE projects
    SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.id;
END;

CREATE TRIGGER IF NOT EXISTS devices_auto_update_updated_at
AFTER UPDATE ON devices
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE devices
    SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.id;
END;

CREATE TRIGGER IF NOT EXISTS project_paths_auto_update_updated_at
AFTER UPDATE ON project_paths
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE project_paths
    SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.id;
END;

CREATE TRIGGER IF NOT EXISTS chat_session_auto_update_updated_at
AFTER UPDATE ON chat_session
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE chat_session
    SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.id;
END;
