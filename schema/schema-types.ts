// =============================================================================
// Personal Context — Schema Types (v8)
// =============================================================================
//
// TIMEZONE RULE: All timestamps are UTC (ISO 8601 with Z suffix).
// The `date` field is a local calendar date (YYYY-MM-DD) with no time component.
// "Today" is based on the user's local timezone at creation time.
// All timestamp reads should be converted to local timezone for display.
//
// TIMESTAMP MANAGEMENT: created_at and updated_at are DB-managed (defaults +
// triggers). Sync/import bypasses the trigger by providing explicit values.
// Record ID format: {YYYYMMDD}-{8-random-hex} (e.g., 20250304-a3f2b7e1).
//
// NO title. NO tags. Records may be notes/data-first with no HTML.
// Project uses slash convention for hierarchy: "org/project".

// -----------------------------------------------------------------------------
// Authentication types (Postgres only — no SQLite equivalent)
// -----------------------------------------------------------------------------

interface User {
  id: string;                // UUID as text
  email: string;
  name: string | null;
  password_hash: string;
  created_at: string;        // ISO 8601 UTC
  updated_at: string;        // ISO 8601 UTC
}

interface ApiKey {
  id: string;                // UUID as text
  user_id: string;           // FK to users.id
  key_hash: string;          // SHA-256 hash of the raw key
  label: string;             // user-provided description
  created_at: string;        // ISO 8601 UTC
  last_used_at: string | null;
  revoked_at: string | null;
}

// -----------------------------------------------------------------------------
// Database row types (shared schema — Postgres adds user_id on records/sync_version)
// -----------------------------------------------------------------------------

interface Record {
  id: string;                // "20250304-a3f2b7e1"
  user_id?: string;          // Postgres only; absent in SQLite
  date: string;              // "2025-03-04" (local calendar date, no timezone)
  day_order: string;         // fractional index, lexicographic sort within date
  html_content: string | null; // raw HTML; null when record.html is absent
  notes: string | null;      // full markdown, null if no notes
  project_id: string;        // FK to projects.id
  source_device_id: string;  // FK to devices.id
  source_ref: string | null; // opaque source/provenance string
  git_remote_url: string | null; // "https://github.com/org/repo" (optional)
  git_hash: string | null;   // full SHA-1 commit hash, 40 hex chars (optional)
  created_at: string;        // ISO 8601 UTC (e.g. "2025-03-04T14:32:00Z")
  updated_at: string;        // ISO 8601 UTC
  deleted_at: string | null; // ISO 8601 UTC if soft-deleted, null if active
}

interface Project {
  id: string;
  created_at: string;        // ISO 8601 UTC
  updated_at: string;        // ISO 8601 UTC
  archived_at: string | null;
}

interface Device {
  id: string;
  created_at: string;        // ISO 8601 UTC
  updated_at: string;        // ISO 8601 UTC
  archived_at: string | null;
}

interface ProjectPath {
  id: number;
  user_id?: string;          // Postgres only; absent in SQLite
  project_id: string;
  path: string;              // absolute, normalized project path
  device_id: string;
  created_at: string;        // ISO 8601 UTC
  updated_at: string;        // ISO 8601 UTC
}

interface ChatSession {
  id: string;                // "20250304-a3f2b7e1"
  user_id?: string;          // Postgres only; absent in SQLite
  source: "codex" | "claude_code" | "gemini" | string;
  source_session_id: string;
  source_device_id: string;
  project_id: string | null;
  cwd: string | null;
  title: string | null;
  started_at: string;        // ISO 8601 UTC
  last_activity_at: string;  // ISO 8601 UTC
  original_source_path: string | null;
  raw_source_key: string | null;  // chats/raw/{id}/source.{json|jsonl|ndjson}
  created_at: string;        // ISO 8601 UTC
  updated_at: string;        // ISO 8601 UTC
  deleted_at: string | null;
}

interface ChatItem {
  id: number;
  session_id: string;
  ordinal: number;
  role: string;
  item_type: string;
  text: string | null;
  search_text: string;
  raw_json: string | null;
  created_at: string;        // ISO 8601 UTC
}

// Sort key: ORDER BY (date, day_order, id)

interface RecordFigure {
  id: number;                // auto-increment PK
  record_id: string;          // FK to records.id, CASCADE delete
  filename: string;          // "loss-curve.png"
  s3_key: string;            // "figures/20250304-a3f2b7e1/loss-curve.png"
  alt_text: string | null;
  created_at: string;        // ISO 8601 UTC
}

interface RecordDataFile {
  id: number;                // auto-increment PK
  record_id: string;          // FK to records.id, CASCADE delete
  filename: string;          // "training-log.csv"
  s3_key: string;            // "data/20250304-a3f2b7e1/training-log.csv"
  size: number;              // bytes
  hash: string;              // SHA-256
  description: string | null;
  created_at: string;        // ISO 8601 UTC
}

interface Template {
  name: string;
  html_content: string;
  description: string | null;
  created_at: string;        // ISO 8601 UTC
  updated_at: string;        // ISO 8601 UTC
}

interface SyncVersion {
  id?: 1;                    // SQLite only: always 1 (singleton).
  user_id?: string;          // Postgres only: per-user PK (FK to users.id)
  version: number;
  updated_at: string;        // ISO 8601 UTC
}

// -----------------------------------------------------------------------------
// JSON export: metadata.json (per record)
// -----------------------------------------------------------------------------
//
// Wire format convention: This interface represents the deserialized type.
// In the JSON file, nullable fields are OMITTED when null (Go: `omitempty`,
// TS: conditional serialization). Consumers must treat missing keys as null.

interface RecordExport {
  format_version: 1;         // always 1; bump on breaking format changes for pc import compat
  id: string;
  date: string;
  day_order: string;
  project_id: string;
  source_device_id: string;
  source_ref?: string;       // omitted when null
  git_remote_url?: string;   // omitted when null
  git_hash?: string;         // omitted when null
  has_notes: boolean;
  figures: FigureExport[];
  data_files: DataFileExport[];
  created_at: string;        // ISO 8601 UTC
  updated_at: string;        // ISO 8601 UTC
}

// Note: html_content → optional record.html file, notes → notes.md file
// deleted_at never exported (soft-deleted records excluded)

interface FigureExport {
  filename: string;
  s3_key: string;
  alt_text: string | null;
}

interface DataFileExport {
  filename: string;
  s3_key: string;
  size: number;
  hash: string;
  description: string | null;
}

interface ChatExport {
  format_version: 2;  // v2 renamed source_path→original_source_path, added raw_source_key.
  id: string;
  source: string;
  source_session_id: string;
  source_device_id: string;
  project_id?: string;
  cwd?: string;
  title?: string;
  started_at: string;
  last_activity_at: string;
  original_source_path?: string;
  raw_source_key?: string;
  created_at: string;
  updated_at: string;
}

interface ChatItemExport {
  ordinal: number;
  role: string;
  item_type: string;
  text?: string;
  search_text: string;
  raw_json?: string;
  created_at: string;
}

// -----------------------------------------------------------------------------
// Git export folder structure
// -----------------------------------------------------------------------------
//
// personal-context-data/
// ├── projects.json
// ├── devices.json
// ├── templates/
// │   ├── text-only.html
// │   └── single-image.html
// └── records/
//     ├── 20250304-a3f2b7e1/
//     │   ├── metadata.json      ← RecordExport
//     │   ├── record.html         ← html_content (omitted when null)
//     │   ├── notes.md           ← notes (only if has_notes)
//     │   └── figures/           ← Git LFS
//     │       └── loss-curve.png
//     └── 20250304-b7e1c9d3/   ← html_content omitted; only metadata.json
//         └── metadata.json
//
// Local filesystem structure:
//
// ~/personal-context/
// ├── .pc/
// │   ├── config.json
// │   ├── pc.db
// │   └── last_sync
// ├── figures/{record_id}/{filename}
// └── data/{record_id}/{filename}
//
// S3 bucket structure (user-scoped in cloud mode):
//
// s3://personal-context-prod/
// └── users/{user_id}/
//     ├── figures/{record_id}/{filename}
//     ├── data/{record_id}/{filename}
//     └── _version

// -----------------------------------------------------------------------------
// Example: records/20250304-a3f2b7e1/metadata.json
// -----------------------------------------------------------------------------
//
// {
//   "format_version": 1,
//   "id": "20250304-a3f2b7e1",
//   "date": "2025-03-04",
//   "day_order": "a",
//   "project_id": "happy-ai/sleep-staging",
//   "source_device_id": "nicholas-macbook",
//   "source_ref": "file:///opaque/source/path",
//   "git_remote_url": "https://github.com/happy-ai/sleep-staging",
//   "git_hash": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
//   "has_notes": true,
//   "figures": [
//     {
//       "filename": "loss-curve.png",
//       "s3_key": "figures/20250304-a3f2b7e1/loss-curve.png",
//       "alt_text": "Training loss over 50 epochs"
//     }
//   ],
//   "data_files": [
//     {
//       "filename": "training-log.csv",
//       "s3_key": "data/20250304-a3f2b7e1/training-log.csv",
//       "size": 2048000,
//       "hash": "ab3f2c8d...",
//       "description": "Epoch-level training metrics"
//     }
//   ],
//   "created_at": "2025-03-04T14:32:00Z",
//   "updated_at": "2025-03-04T16:10:00Z"
// }
