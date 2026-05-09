// API response types — derived from CONTEXT.md payload shapes.
// DB row types are defined in schema/schema-types.ts (canonical, not imported here).

/** Summary representation of a record for list endpoints. */
export type RecordSummary = {
  id: string;
  date: string;
  day_order: string;
  html_content: string | null;
  project_id: string;
  source_device_id: string;
  source_ref: string | null;
  updated_at: string;
  deleted_at: string | null;
  figure_count: number;
  data_file_count: number;
};

/** A figure or data file attachment. */
export type RecordFile = {
  filename: string;
  s3_key: string;
  size?: number;
  hash?: string;
  alt_text?: string | null;
  description?: string | null;
};

/** Full record detail including child rows. */
export type RecordDetail = {
  id: string;
  date: string;
  day_order: string;
  html_content: string | null;
  notes: string | null;
  project_id: string;
  source_device_id: string;
  source_ref: string | null;
  git_remote_url: string | null;
  git_hash: string | null;
  created_at: string;
  updated_at: string;
  deleted_at: string | null;
  figures: RecordFile[];
  data_files: RecordFile[];
};

/** Request body for PATCH /api/records/[id]. */
export type RecordUpdateInput = {
  project_id?: string;
  notes?: string | null;
  git_remote_url?: string | null;
  git_hash?: string | null;
};

/** Request body for PATCH /api/records/[id]/order. */
export type ReorderInput = {
  date?: string;
  position:
    | { kind: "first" | "last" }
    | { kind: "before" | "after"; reference_id: string };
};

/** Query params for GET /api/records. */
export type RecordListParams = {
  limit?: number;
  cursor?: string;
  project?: string;
  deleted?: boolean;
  updated_after?: string;
};

/** Paginated response wrapper. */
export type PaginatedResponse<T> = {
  items: T[];
  total: number;
  next_cursor: string | null;
};

/** GET /api/sync/version response. */
export type SyncVersionResponse = {
  version: number;
  updated_at: string;
};

/** GET /api/sync/changes response. */
export type SyncChangesResponse = {
  items: RecordSummary[];
  server_now: string;
};

/** GET /api/files/.../... response. */
export type FileUrlResponse = {
  url: string;
  expires_at: string;
};

/** GET /api/projects response. */
export type ProjectsResponse = {
  projects: string[];
};

/** Response for DELETE /api/records/[id]. */
export type DeleteResponse = {
  id: string;
  deleted_at: string;
  updated_at: string;
  sync_version: number;
};

/** Response for POST /api/records/[id]/restore. */
export type RestoreResponse = {
  id: string;
  deleted_at: null;
  updated_at: string;
  sync_version: number;
};

/** Response for PATCH /api/records/[id]/order. */
export type ReorderResponse = {
  id: string;
  date: string;
  day_order: string;
  updated_at: string;
  sync_version: number;
};

/** A group of record summaries sharing the same date. */
export type RecordGroup = {
  date: string;
  records: RecordSummary[];
};

/** A virtual element representing a date boundary between record groups. */
export type VirtualDateRecord = {
  type: "date-marker";
  date: string;
};

/** GET /api/info response. */
export type AppInfoResponse = {
  mode: "local" | "cloud";
  version: string;
};

/** GET /api/stats response. */
export type StatsResponse = {
  total_records: number;
  total_projects: number;
  trashed_records: number;
};

/** DELETE /api/records/trash response. */
export type PurgeTrashResponse = {
  purged_count: number;
  sync_version: number;
};

// ---------- UI-specific types (v0.dev design) ----------

/** Navigation panel view mode. */
export type ViewMode = "strip" | "grid";

/** Visibility state of the three resizable panels + metadata bar. */
export type PanelVisibility = {
  navigation: boolean;
  details: boolean;
  metadata: boolean;
};
