// =============================================================================
// Personal Context — Schema Types (v7)
// =============================================================================
//
// TIMEZONE RULE: All timestamps are UTC (ISO 8601 with Z suffix).
// The `date` field is a local calendar date (YYYY-MM-DD) with no time component.
// "Today" is based on the user's local timezone at creation time.
// All timestamp reads should be converted to local timezone for display.
//
// TIMESTAMP MANAGEMENT: created_at and updated_at are DB-managed (defaults +
// triggers). Sync/import bypasses the trigger by providing explicit values.
// Slide ID format: {YYYYMMDD}-{8-random-hex} (e.g., 20250304-a3f2b7e1).
//
// NO title. NO tags. Slides are identified by their content (HTML).
// Project uses slash convention for hierarchy: "org/project".

// -----------------------------------------------------------------------------
// Database row types (same schema for Postgres and SQLite)
// -----------------------------------------------------------------------------

interface Slide {
  id: string;                // "20250304-a3f2b7e1"
  date: string;              // "2025-03-04" (local calendar date, no timezone)
  day_order: string;         // fractional index, lexicographic sort within date
  html_content: string;      // raw HTML
  notes: string | null;      // full markdown, null if no notes
  project_id: string | null; // "happy-ai/sleep-staging" (slash convention for org/project)
  git_remote_url: string | null; // "https://github.com/org/repo" (optional)
  git_hash: string | null;   // full SHA-1 commit hash, 40 hex chars (optional)
  created_at: string;        // ISO 8601 UTC (e.g. "2025-03-04T14:32:00Z")
  updated_at: string;        // ISO 8601 UTC
  deleted_at: string | null; // ISO 8601 UTC if soft-deleted, null if active
}

// Sort key: ORDER BY (date, day_order, id)

interface SlideFigure {
  id: number;                // auto-increment PK
  slide_id: string;          // FK to slides.id, CASCADE delete
  filename: string;          // "loss-curve.png"
  s3_key: string;            // "figures/20250304-a3f2b7e1/loss-curve.png"
  alt_text: string | null;
  created_at: string;        // ISO 8601 UTC
}

interface SlideDataFile {
  id: number;                // auto-increment PK
  slide_id: string;          // FK to slides.id, CASCADE delete
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
  id: 1;                     // always 1, single-row table
  version: number;
  updated_at: string;        // ISO 8601 UTC
}

// -----------------------------------------------------------------------------
// JSON export: metadata.json (per slide)
// -----------------------------------------------------------------------------
//
// Wire format convention: This interface represents the deserialized type.
// In the JSON file, nullable fields are OMITTED when null (Go: `omitempty`,
// TS: conditional serialization). Consumers must treat missing keys as null.

interface SlideExport {
  format_version: 1;         // always 1; bump on breaking format changes for pc import compat
  id: string;
  date: string;
  day_order: string;
  project_id?: string;       // omitted when null
  git_remote_url?: string;   // omitted when null
  git_hash?: string;         // omitted when null
  has_notes: boolean;
  figures: FigureExport[];
  data_files: DataFileExport[];
  created_at: string;        // ISO 8601 UTC
  updated_at: string;        // ISO 8601 UTC
}

// Note: html_content → slide.html file, notes → notes.md file
// deleted_at never exported (soft-deleted slides excluded)

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

// -----------------------------------------------------------------------------
// Git export folder structure
// -----------------------------------------------------------------------------
//
// personal-context-data/
// ├── templates/
// │   ├── text-only.html
// │   └── single-image.html
// └── slides/
//     ├── 20250304-a3f2b7e1/
//     │   ├── metadata.json      ← SlideExport
//     │   ├── slide.html         ← html_content
//     │   ├── notes.md           ← notes (only if has_notes)
//     │   └── figures/           ← Git LFS
//     │       └── loss-curve.png
//     └── 20250304-b7e1c9d3/
//         ├── metadata.json
//         └── slide.html
//
// Local filesystem structure:
//
// ~/personal-context/
// ├── .pc/
// │   ├── config.json
// │   ├── pc.db
// │   └── last_sync
// ├── figures/{slide_id}/{filename}
// └── data/{slide_id}/{filename}
//
// S3 bucket structure:
//
// s3://personal-context-prod/
// ├── figures/{slide_id}/{filename}
// ├── data/{slide_id}/{filename}
// └── _version

// -----------------------------------------------------------------------------
// Example: slides/20250304-a3f2b7e1/metadata.json
// -----------------------------------------------------------------------------
//
// {
//   "format_version": 1,
//   "id": "20250304-a3f2b7e1",
//   "date": "2025-03-04",
//   "day_order": "a",
//   "project_id": "happy-ai/sleep-staging",
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
