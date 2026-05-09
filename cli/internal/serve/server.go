package serve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/fractionalindex"
	"github.com/conn-castle/personal-context/cli/internal/listpage"
	"github.com/conn-castle/personal-context/cli/internal/repository"
)

// Server implements the local REST API backed by SQLite + local files.
type Server struct {
	repo    repository.Repository
	dataDir string
	port    int
	version string
	server  *http.Server
	writeMu sync.Mutex // serializes read-modify-write cycles (PATCH, reorder)
}

// NewServer creates a local API server.
// Args: repo is the SQLite repository; dataDir is the base data directory (e.g., ~/personal-context);
// port is the port to listen on; version is the CLI version exposed by `/api/info`.
func NewServer(repo repository.Repository, dataDir string, port int, version string) (*Server, error) {
	if repo == nil {
		return nil, fmt.Errorf("repository is required")
	}
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("data directory is required")
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535, got %d", port)
	}
	if strings.TrimSpace(version) == "" {
		return nil, fmt.Errorf("version is required")
	}
	return &Server{repo: repo, dataDir: dataDir, port: port, version: version}, nil
}

// Start begins listening on 127.0.0.1:<port>. Blocks until the server shuts down.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.server = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler:      corsMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ln, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.server.Addr, err)
	}

	log.Printf("Local API server listening on http://%s", s.server.Addr)
	log.Printf("Set LOCAL_BACKEND_URL=http://%s when running next dev", s.server.Addr)

	return s.server.Serve(ln)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/projects", s.handleListProjects)
	mux.HandleFunc("GET /api/records", s.handleListRecords)
	mux.HandleFunc("DELETE /api/records/trash", s.handlePurgeTrash)
	mux.HandleFunc("GET /api/records/{id}", s.handleGetRecord)
	mux.HandleFunc("PATCH /api/records/{id}", s.handlePatchRecord)
	mux.HandleFunc("DELETE /api/records/{id}", s.handleDeleteRecord)
	mux.HandleFunc("POST /api/records/{id}/restore", s.handleRestoreRecord)
	mux.HandleFunc("PATCH /api/records/{id}/order", s.handleReorderRecord)
	mux.HandleFunc("GET /api/sync/version", s.handleSyncVersion)
	mux.HandleFunc("GET /api/sync/changes", s.handleSyncChanges)
	mux.HandleFunc("GET /api/files/{recordId}/{fileType}/{filename}", s.handleGetFile)
	mux.HandleFunc("GET /local-files/{recordId}/{fileType}/{filename}", s.handleServeFile)
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeError(w http.ResponseWriter, status int, message string, code string) {
	writeJSON(w, status, errorResponse{Error: message, Code: code})
}

func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return fmt.Errorf("request body is empty")
	}
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(r.Body)
	return decoder.Decode(v)
}

func mapRepoError(w http.ResponseWriter, err error, entity string) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("%s not found", entity), "NOT_FOUND")
	case errors.Is(err, repository.ErrConflict):
		writeError(w, http.StatusConflict, fmt.Sprintf("%s conflict", entity), "CONFLICT")
	case errors.Is(err, repository.ErrInvalidArgument):
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid %s argument", entity), "BAD_REQUEST")
	case errors.Is(err, repository.ErrForeignKeyViolation):
		writeError(w, http.StatusConflict, fmt.Sprintf("%s foreign key violation", entity), "FK_VIOLATION")
	default:
		log.Printf("internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
	}
}

// --- CORS middleware ---

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Cursor helpers ---

type cursorPayload struct {
	Date     string `json:"date"`
	DayOrder string `json:"day_order"`
	ID       string `json:"id"`
}

func decodeCursor(raw string) (*cursorPayload, error) {
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor encoding")
	}
	var c cursorPayload
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("invalid cursor format")
	}
	if c.Date == "" || c.DayOrder == "" || c.ID == "" {
		return nil, fmt.Errorf("incomplete cursor")
	}
	return &c, nil
}

func encodeCursor(c cursorPayload) string {
	data, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(data)
}

// --- Time helpers ---

func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := formatTime(*t)
	return &s
}

// --- Record ID validation ---

func isValidRecordID(id string) bool {
	if len(id) != 17 {
		return false
	}
	if id[8] != '-' {
		return false
	}
	for i := 0; i < 8; i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	for i := 9; i < 17; i++ {
		if (id[i] < '0' || id[i] > '9') && (id[i] < 'a' || id[i] > 'f') {
			return false
		}
	}
	return true
}

// --- File path safety ---

func isValidFilename(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	if strings.Contains(name, "\x00") {
		return false
	}
	return true
}

func isValidDate(date string) bool {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false
	}
	return parsed.UTC().Format("2006-01-02") == date
}

func isValidGitHash(hash string) bool {
	if len(hash) != 40 {
		return false
	}
	for i := 0; i < len(hash); i++ {
		c := hash[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

type listRecordsQuery struct {
	limit  int
	cursor *cursorPayload
	filter repository.ListRecordsFilter
}

// parseListRecordsQuery normalizes the list-records query parameters into one struct.
func parseListRecordsQuery(query url.Values) (listRecordsQuery, string) {
	parsed := listRecordsQuery{
		limit:  20,
		filter: repository.ListRecordsFilter{},
	}

	if raw := query.Get("limit"); raw != "" {
		var value int
		if _, err := fmt.Sscan(raw, &value); err == nil {
			if value < 1 {
				value = 1
			}
			if value > 100 {
				value = 100
			}
			parsed.limit = value
		}
	}

	if raw := query.Get("cursor"); raw != "" {
		cursor, err := decodeCursor(raw)
		if err != nil {
			return listRecordsQuery{}, "Invalid cursor format"
		}
		parsed.cursor = cursor
	}

	if project := query.Get("project"); project != "" {
		parsed.filter.ProjectID = &project
	}

	if query.Get("deleted") == "true" {
		parsed.filter.OnlyDeleted = true
		parsed.filter.IncludeDeleted = true
	}

	if updatedAfter := query.Get("updated_after"); updatedAfter != "" {
		timestamp, err := time.Parse(time.RFC3339Nano, updatedAfter)
		if err != nil {
			return listRecordsQuery{}, "Invalid updated_after timestamp"
		}
		parsed.filter.UpdatedAfter = &timestamp
	}

	return parsed, ""
}

type recordSummary struct {
	ID             string  `json:"id"`
	Date           string  `json:"date"`
	DayOrder       string  `json:"day_order"`
	HTMLContent    *string `json:"html_content"`
	ProjectID      string  `json:"project_id"`
	SourceDeviceID string  `json:"source_device_id"`
	SourceRef      *string `json:"source_ref"`
	UpdatedAt      string  `json:"updated_at"`
	DeletedAt      *string `json:"deleted_at"`
	FigureCount    int     `json:"figure_count"`
	DataFileCount  int     `json:"data_file_count"`
}

type recordFile struct {
	Filename    string  `json:"filename"`
	S3Key       string  `json:"s3_key"`
	Size        *int64  `json:"size,omitempty"`
	Hash        *string `json:"hash,omitempty"`
	AltText     *string `json:"alt_text"`
	Description *string `json:"description"`
}

type recordDetail struct {
	ID             string       `json:"id"`
	Date           string       `json:"date"`
	DayOrder       string       `json:"day_order"`
	HTMLContent    *string      `json:"html_content"`
	Notes          *string      `json:"notes"`
	ProjectID      string       `json:"project_id"`
	SourceDeviceID string       `json:"source_device_id"`
	SourceRef      *string      `json:"source_ref"`
	GitRemoteURL   *string      `json:"git_remote_url"`
	GitHash        *string      `json:"git_hash"`
	CreatedAt      string       `json:"created_at"`
	UpdatedAt      string       `json:"updated_at"`
	DeletedAt      *string      `json:"deleted_at"`
	Figures        []recordFile `json:"figures"`
	DataFiles      []recordFile `json:"data_files"`
}

func decodeJSONObject(r *http.Request) (map[string]any, string) {
	var body any
	if err := decodeJSON(r, &body); err != nil {
		return nil, "Invalid JSON body"
	}
	objectBody, ok := body.(map[string]any)
	if !ok || objectBody == nil {
		return nil, "Request body must be a JSON object"
	}
	return objectBody, ""
}

func validatePatchBody(body map[string]any) (map[string]any, string) {
	allowed := map[string]struct{}{
		"project_id":     {},
		"notes":          {},
		"git_remote_url": {},
		"git_hash":       {},
	}
	if len(body) == 0 {
		return nil, "Request body must include at least one field to update"
	}

	unknownKeys := make([]string, 0)
	for key := range body {
		if _, ok := allowed[key]; !ok {
			unknownKeys = append(unknownKeys, key)
		}
	}
	if len(unknownKeys) > 0 {
		return nil, fmt.Sprintf("Unknown fields: %s", strings.Join(unknownKeys, ", "))
	}

	normalized := make(map[string]any, len(body))

	if value, ok := body["project_id"]; ok {
		projectID, ok := value.(string)
		if !ok || projectID == "" || projectID != strings.TrimSpace(projectID) {
			return nil, "project_id must be a non-empty string with no leading or trailing whitespace"
		}
		normalized["project_id"] = projectID
	}

	if value, ok := body["notes"]; ok {
		if value == nil {
			normalized["notes"] = nil
		} else {
			notes, ok := value.(string)
			if !ok {
				return nil, "notes must be a string or null"
			}
			if notes == "" {
				normalized["notes"] = nil
			} else {
				normalized["notes"] = notes
			}
		}
	}

	if value, ok := body["git_remote_url"]; ok {
		if value == nil {
			normalized["git_remote_url"] = nil
		} else {
			gitRemoteURL, ok := value.(string)
			if !ok || gitRemoteURL == "" {
				return nil, "git_remote_url must be a non-empty string or null"
			}
			allowedSchemes := []string{"https://", "http://", "git://", "ssh://"}
			validScheme := false
			for _, scheme := range allowedSchemes {
				if strings.HasPrefix(gitRemoteURL, scheme) {
					validScheme = true
					break
				}
			}
			if !validScheme {
				return nil, "git_remote_url must start with https://, http://, git://, or ssh://"
			}
			normalized["git_remote_url"] = gitRemoteURL
		}
	}

	if value, ok := body["git_hash"]; ok {
		if value == nil {
			normalized["git_hash"] = nil
		} else {
			gitHash, ok := value.(string)
			if !ok || !isValidGitHash(gitHash) {
				return nil, "git_hash must be a 40-character hex string or null"
			}
			normalized["git_hash"] = gitHash
		}
	}

	return normalized, ""
}

type reorderInput struct {
	Date        *string
	Kind        string
	ReferenceID *string
}

func validateReorderBody(body map[string]any, recordID string) (reorderInput, string) {
	positionRaw, ok := body["position"]
	if !ok || positionRaw == nil {
		return reorderInput{}, "position is required"
	}

	position, ok := positionRaw.(map[string]any)
	if !ok {
		return reorderInput{}, "position is required"
	}

	kind, ok := position["kind"].(string)
	if !ok || (kind != "first" && kind != "last" && kind != "before" && kind != "after") {
		return reorderInput{}, `position.kind must be "first", "last", "before", or "after"`
	}

	var referenceID *string
	if kind == "before" || kind == "after" {
		referenceRaw, ok := position["reference_id"]
		if !ok || referenceRaw == nil {
			return reorderInput{}, fmt.Sprintf("position.reference_id is required for kind %q", kind)
		}
		referenceString, ok := referenceRaw.(string)
		if !ok {
			return reorderInput{}, "position.reference_id must be a string"
		}
		if referenceString == "" {
			return reorderInput{}, fmt.Sprintf("position.reference_id is required for kind %q", kind)
		}
		if !isValidRecordID(referenceString) {
			return reorderInput{}, "Invalid reference_id format"
		}
		if referenceString == recordID {
			return reorderInput{}, "Cannot reorder a record relative to itself"
		}
		referenceID = &referenceString
	}

	var date *string
	if dateRaw, ok := body["date"]; ok {
		dateString, ok := dateRaw.(string)
		if !ok || !isValidDate(dateString) {
			return reorderInput{}, "Invalid date format"
		}
		date = &dateString
	}

	return reorderInput{
		Date:        date,
		Kind:        kind,
		ReferenceID: referenceID,
	}, ""
}

func buildRecordSummary(record repository.Record, childCounts repository.ChildCounts) recordSummary {
	return recordSummary{
		ID:             record.ID,
		Date:           record.Date,
		DayOrder:       record.DayOrder,
		HTMLContent:    record.HTMLContent,
		ProjectID:      record.ProjectID,
		SourceDeviceID: record.SourceDeviceID,
		SourceRef:      record.SourceRef,
		UpdatedAt:      formatTime(record.UpdatedAt),
		DeletedAt:      formatTimePtr(record.DeletedAt),
		FigureCount:    childCounts.Figures,
		DataFileCount:  childCounts.DataFiles,
	}
}

func (s *Server) buildRecordFiles(ctx context.Context, recordID string) ([]recordFile, []recordFile, error) {
	figures, err := s.repo.ListRecordFiguresByRecordID(ctx, recordID)
	if err != nil {
		return nil, nil, err
	}
	dataFiles, err := s.repo.ListRecordDataFilesByRecordID(ctx, recordID)
	if err != nil {
		return nil, nil, err
	}

	figureFiles := make([]recordFile, len(figures))
	for i, figure := range figures {
		figureFiles[i] = recordFile{
			Filename: figure.Filename,
			S3Key:    figure.S3Key,
			AltText:  figure.AltText,
		}
	}

	dataFileList := make([]recordFile, len(dataFiles))
	for i, dataFile := range dataFiles {
		size := dataFile.Size
		hash := dataFile.Hash
		dataFileList[i] = recordFile{
			Filename:    dataFile.Filename,
			S3Key:       dataFile.S3Key,
			Size:        &size,
			Hash:        &hash,
			Description: dataFile.Description,
		}
	}

	return figureFiles, dataFileList, nil
}

func (s *Server) buildRecordDetail(ctx context.Context, record repository.Record) (recordDetail, error) {
	figureFiles, dataFileList, err := s.buildRecordFiles(ctx, record.ID)
	if err != nil {
		return recordDetail{}, err
	}

	return recordDetail{
		ID:             record.ID,
		Date:           record.Date,
		DayOrder:       record.DayOrder,
		HTMLContent:    record.HTMLContent,
		Notes:          record.Notes,
		ProjectID:      record.ProjectID,
		SourceDeviceID: record.SourceDeviceID,
		SourceRef:      record.SourceRef,
		GitRemoteURL:   record.GitRemoteURL,
		GitHash:        record.GitHash,
		CreatedAt:      formatTime(record.CreatedAt),
		UpdatedAt:      formatTime(record.UpdatedAt),
		DeletedAt:      formatTimePtr(record.DeletedAt),
		Figures:        figureFiles,
		DataFiles:      dataFileList,
	}, nil
}

// --- Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleInfo returns server mode and version information.
func (s *Server) handleInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"mode":    "local",
		"version": s.version,
	})
}

// handleStats returns aggregate counts: total records, total projects, and trashed records.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	totalRecords, err := s.repo.CountActiveRecords(ctx)
	if err != nil {
		mapRepoError(w, err, "records")
		return
	}

	trashedRecords, err := s.repo.CountTrashedRecords(ctx)
	if err != nil {
		mapRepoError(w, err, "records")
		return
	}

	projects, err := s.repo.ListProjects(ctx, false)
	if err != nil {
		mapRepoError(w, err, "projects")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int{
		"total_records":   totalRecords,
		"total_projects":  len(projects),
		"trashed_records": trashedRecords,
	})
}

// handlePurgeTrash hard-deletes all soft-deleted (trashed) records and removes their
// filesystem directories for figures and data.
func (s *Server) handlePurgeTrash(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Bulk delete all soft-deleted records and get their IDs for filesystem cleanup.
	purgedIDs, err := s.repo.PurgeDeletedRecords(ctx)
	if err != nil {
		mapRepoError(w, err, "records")
		return
	}

	// Best-effort removal of local filesystem dirs for figures and data.
	for _, id := range purgedIDs {
		for _, subdir := range []string{"figures", "data"} {
			dirPath := filepath.Join(s.dataDir, subdir, id)
			if rmErr := os.RemoveAll(dirPath); rmErr != nil {
				log.Printf("warning: failed to remove %s: %v", dirPath, rmErr)
			}
		}
	}

	syncVersion, err := s.repo.GetSyncVersion(ctx)
	if err != nil {
		mapRepoError(w, err, "sync version")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"purged_count": len(purgedIDs),
		"sync_version": syncVersion.Version,
	})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.repo.ListProjects(r.Context(), false)
	if err != nil {
		mapRepoError(w, err, "projects")
		return
	}
	projectIDs := make([]string, 0, len(projects))
	for _, project := range projects {
		projectIDs = append(projectIDs, project.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projectIDs})
}

func (s *Server) handleListRecords(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	parsedQuery, queryErr := parseListRecordsQuery(r.URL.Query())
	if queryErr != "" {
		writeError(w, http.StatusBadRequest, queryErr, "BAD_REQUEST")
		return
	}

	total, err := s.repo.CountRecords(ctx, parsedQuery.filter)
	if err != nil {
		mapRepoError(w, err, "records")
		return
	}

	// Fetch the full matching set, then sort and paginate in API order.
	// The repository contract sorts ascending and applies LIMIT before returning,
	// which would otherwise truncate the wrong end of the result set.
	records, err := s.repo.ListRecords(ctx, parsedQuery.filter)
	if err != nil {
		mapRepoError(w, err, "records")
		return
	}

	sortRecordsForAPI(records)

	// Apply cursor-based pagination manually since repo doesn't support cursors
	if parsedQuery.cursor != nil {
		startIdx := -1
		for i, record := range records {
			if isAfterCursor(record, parsedQuery.cursor) {
				startIdx = i
				break
			}
		}
		if startIdx == -1 {
			records = nil
		} else {
			records = records[startIdx:]
		}
	}

	// Truncate to limit+1 for pagination after applying the final API sort.
	if len(records) > parsedQuery.limit+1 {
		records = records[:parsedQuery.limit+1]
	}

	items := make([]recordSummary, 0, len(records))
	hasNextPage := len(records) > parsedQuery.limit
	resultRecords := records
	if hasNextPage {
		resultRecords = records[:parsedQuery.limit]
	}

	childCounts, err := s.repo.CountRecordChildren(ctx, serveRecordIDs(resultRecords))
	if err != nil {
		mapRepoError(w, err, "record files")
		return
	}

	for _, record := range resultRecords {
		items = append(items, buildRecordSummary(record, childCounts[record.ID]))
	}

	var nextCursor *string
	if hasNextPage && len(items) > 0 {
		last := items[len(items)-1]
		c := encodeCursor(cursorPayload{Date: last.Date, DayOrder: last.DayOrder, ID: last.ID})
		nextCursor = &c
	}

	resp := listpage.Response[recordSummary]{Items: items, Total: total, NextCursor: nextCursor}
	writeJSON(w, http.StatusOK, resp)
}

func serveRecordIDs(records []repository.Record) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids
}

// sortRecordsForAPI sorts records in date DESC, day_order ASC, id ASC order.
func sortRecordsForAPI(records []repository.Record) {
	slices.SortFunc(records, compareRecordsForAPI)
}

func compareRecordsForAPI(a, b repository.Record) int {
	// Date DESC
	if a.Date > b.Date {
		return -1
	}
	if a.Date < b.Date {
		return 1
	}
	// Day order ASC
	if a.DayOrder < b.DayOrder {
		return -1
	}
	if a.DayOrder > b.DayOrder {
		return 1
	}
	// ID ASC
	if a.ID < b.ID {
		return -1
	}
	if a.ID > b.ID {
		return 1
	}
	return 0
}

// isAfterCursor returns true if the record should appear after the cursor position.
func isAfterCursor(record repository.Record, cursor *cursorPayload) bool {
	// Sort is date DESC, day_order ASC, id ASC
	// So "after cursor" means:
	// (date < cursor.date) OR
	// (date == cursor.date AND day_order > cursor.day_order) OR
	// (date == cursor.date AND day_order == cursor.day_order AND id > cursor.id)
	if record.Date < cursor.Date {
		return true
	}
	if record.Date == cursor.Date && record.DayOrder > cursor.DayOrder {
		return true
	}
	if record.Date == cursor.Date && record.DayOrder == cursor.DayOrder && record.ID > cursor.ID {
		return true
	}
	return false
}

// compareRecordsByDayOrder sorts siblings within a single date by day_order then ID.
func compareRecordsByDayOrder(left, right repository.Record) int {
	if left.DayOrder < right.DayOrder {
		return -1
	}
	if left.DayOrder > right.DayOrder {
		return 1
	}
	if left.ID < right.ID {
		return -1
	}
	if left.ID > right.ID {
		return 1
	}
	return 0
}

// findRecordIndexByID returns the index of a record with the given ID or -1.
func findRecordIndexByID(records []repository.Record, recordID string) int {
	for index, record := range records {
		if record.ID == recordID {
			return index
		}
	}
	return -1
}

func (s *Server) handleGetRecord(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isValidRecordID(id) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid record ID: %s", id), "INVALID_ID")
		return
	}

	ctx := r.Context()
	record, err := s.repo.GetRecordByID(ctx, id)
	if err != nil {
		mapRepoError(w, err, "Record")
		return
	}
	detail, err := s.buildRecordDetail(ctx, record)
	if err != nil {
		mapRepoError(w, err, "record files")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"record": detail})
}

func (s *Server) handlePatchRecord(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isValidRecordID(id) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid record ID: %s", id), "INVALID_ID")
		return
	}

	ctx := r.Context()

	body, bodyErr := decodeJSONObject(r)
	if bodyErr != "" {
		writeError(w, http.StatusBadRequest, bodyErr, "BAD_REQUEST")
		return
	}

	normalizedBody, validationErr := validatePatchBody(body)
	if validationErr != "" {
		writeError(w, http.StatusBadRequest, validationErr, "BAD_REQUEST")
		return
	}

	// Serialize read-modify-write to prevent concurrent PATCH requests from
	// clobbering each other's changes (lost-update problem).
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	existing, err := s.repo.GetRecordByID(ctx, id)
	if err != nil {
		mapRepoError(w, err, "Record")
		return
	}
	if existing.DeletedAt != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Record not found: %s", id), "NOT_FOUND")
		return
	}

	input := repository.UpdateRecordInput{
		ID:             existing.ID,
		Date:           existing.Date,
		DayOrder:       existing.DayOrder,
		HTMLContent:    existing.HTMLContent,
		Notes:          existing.Notes,
		ProjectID:      existing.ProjectID,
		SourceDeviceID: existing.SourceDeviceID,
		SourceRef:      existing.SourceRef,
		GitRemoteURL:   existing.GitRemoteURL,
		GitHash:        existing.GitHash,
		DeletedAt:      existing.DeletedAt,
	}

	// Apply PATCH fields
	if value, ok := normalizedBody["project_id"]; ok {
		if projectID, ok := value.(string); ok && strings.TrimSpace(projectID) != "" {
			input.ProjectID = projectID
		}
	}
	if value, ok := normalizedBody["notes"]; ok {
		if value == nil {
			input.Notes = nil
		} else if notes, ok := value.(string); ok {
			input.Notes = &notes
		}
	}
	if value, ok := normalizedBody["git_remote_url"]; ok {
		if value == nil {
			input.GitRemoteURL = nil
		} else if gitRemoteURL, ok := value.(string); ok {
			input.GitRemoteURL = &gitRemoteURL
		}
	}
	if value, ok := normalizedBody["git_hash"]; ok {
		if value == nil {
			input.GitHash = nil
		} else if gitHash, ok := value.(string); ok {
			input.GitHash = &gitHash
		}
	}

	updated, err := s.repo.UpdateRecord(ctx, input)
	if err != nil {
		mapRepoError(w, err, "Record")
		return
	}

	detail, err := s.buildRecordDetail(ctx, updated)
	if err != nil {
		mapRepoError(w, err, "record files")
		return
	}

	syncVersion, err := s.repo.GetSyncVersion(ctx)
	if err != nil {
		mapRepoError(w, err, "sync version")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"record":       detail,
		"sync_version": syncVersion.Version,
	})
}

func (s *Server) handleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isValidRecordID(id) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid record ID: %s", id), "INVALID_ID")
		return
	}

	ctx := r.Context()
	current, err := s.repo.GetRecordByID(ctx, id)
	if err != nil {
		mapRepoError(w, err, "Record")
		return
	}
	if current.DeletedAt != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Record not found or already deleted: %s", id), "NOT_FOUND")
		return
	}

	if err := s.repo.SoftDeleteRecord(ctx, id); err != nil {
		mapRepoError(w, err, "Record")
		return
	}

	record, err := s.repo.GetRecordByID(ctx, id)
	if err != nil {
		mapRepoError(w, err, "Record")
		return
	}

	syncVersion, err := s.repo.GetSyncVersion(ctx)
	if err != nil {
		mapRepoError(w, err, "sync version")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":           record.ID,
		"deleted_at":   formatTimePtr(record.DeletedAt),
		"updated_at":   formatTime(record.UpdatedAt),
		"sync_version": syncVersion.Version,
	})
}

func (s *Server) handleRestoreRecord(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isValidRecordID(id) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid record ID: %s", id), "INVALID_ID")
		return
	}

	ctx := r.Context()
	current, err := s.repo.GetRecordByID(ctx, id)
	if err != nil {
		mapRepoError(w, err, "Record")
		return
	}
	if current.DeletedAt == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Record not found or not deleted: %s", id), "NOT_FOUND")
		return
	}

	if err := s.repo.RestoreRecord(ctx, id); err != nil {
		mapRepoError(w, err, "Record")
		return
	}

	record, err := s.repo.GetRecordByID(ctx, id)
	if err != nil {
		mapRepoError(w, err, "Record")
		return
	}

	syncVersion, err := s.repo.GetSyncVersion(ctx)
	if err != nil {
		mapRepoError(w, err, "sync version")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":           record.ID,
		"deleted_at":   nil,
		"updated_at":   formatTime(record.UpdatedAt),
		"sync_version": syncVersion.Version,
	})
}

func (s *Server) handleReorderRecord(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isValidRecordID(id) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid record ID: %s", id), "INVALID_ID")
		return
	}

	ctx := r.Context()

	body, bodyErr := decodeJSONObject(r)
	if bodyErr != "" {
		writeError(w, http.StatusBadRequest, bodyErr, "BAD_REQUEST")
		return
	}

	input, validationErr := validateReorderBody(body, id)
	if validationErr != "" {
		writeError(w, http.StatusBadRequest, validationErr, "BAD_REQUEST")
		return
	}

	// Serialize read-modify-write to prevent concurrent reorder requests from
	// overwriting unrelated metadata changes (lost-update problem).
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	existing, err := s.repo.GetRecordByID(ctx, id)
	if err != nil {
		mapRepoError(w, err, "Record")
		return
	}
	if existing.DeletedAt != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Record not found: %s", id), "NOT_FOUND")
		return
	}

	targetDate := existing.Date
	if input.Date != nil {
		targetDate = *input.Date
	}

	// Get siblings for the target date (exclude the moving record)
	siblings, err := s.repo.ListRecords(ctx, repository.ListRecordsFilter{
		DateFrom: &targetDate,
		DateTo:   &targetDate,
	})
	if err != nil {
		mapRepoError(w, err, "records")
		return
	}

	var dateSiblings []repository.Record
	for _, sl := range siblings {
		if sl.Date == targetDate && sl.ID != id && sl.DeletedAt == nil {
			dateSiblings = append(dateSiblings, sl)
		}
	}
	slices.SortFunc(dateSiblings, compareRecordsByDayOrder)

	newOrder, err := computeFractionalIndex(dateSiblings, input.Kind, input.ReferenceID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error(), "NOT_FOUND")
		return
	}

	// Update via read-then-merge
	updateInput := repository.UpdateRecordInput{
		ID:             existing.ID,
		Date:           targetDate,
		DayOrder:       newOrder,
		HTMLContent:    existing.HTMLContent,
		Notes:          existing.Notes,
		ProjectID:      existing.ProjectID,
		SourceDeviceID: existing.SourceDeviceID,
		SourceRef:      existing.SourceRef,
		GitRemoteURL:   existing.GitRemoteURL,
		GitHash:        existing.GitHash,
		DeletedAt:      existing.DeletedAt,
	}

	updated, err := s.repo.UpdateRecord(ctx, updateInput)
	if err != nil {
		mapRepoError(w, err, "Record")
		return
	}

	syncVersion, err := s.repo.GetSyncVersion(ctx)
	if err != nil {
		mapRepoError(w, err, "sync version")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":           updated.ID,
		"date":         updated.Date,
		"day_order":    updated.DayOrder,
		"updated_at":   formatTime(updated.UpdatedAt),
		"sync_version": syncVersion.Version,
	})
}

// computeFractionalIndex uses the Go fracdex library to compute a new day_order.
func computeFractionalIndex(siblings []repository.Record, kind string, refID *string) (string, error) {
	// Import the fractional index library
	switch kind {
	case "first":
		if len(siblings) == 0 {
			return generateKeyBetween("", ""), nil
		}
		return generateKeyBetween("", siblings[0].DayOrder), nil
	case "last":
		if len(siblings) == 0 {
			return generateKeyBetween("", ""), nil
		}
		return generateKeyBetween(siblings[len(siblings)-1].DayOrder, ""), nil
	case "before":
		refIdx := findRecordIndexByID(siblings, *refID)
		if refIdx == -1 {
			return "", fmt.Errorf("reference record not found: %s", *refID)
		}
		prevOrder := ""
		if refIdx > 0 {
			prevOrder = siblings[refIdx-1].DayOrder
		}
		return generateKeyBetween(prevOrder, siblings[refIdx].DayOrder), nil
	case "after":
		refIdx := findRecordIndexByID(siblings, *refID)
		if refIdx == -1 {
			return "", fmt.Errorf("reference record not found: %s", *refID)
		}
		nextOrder := ""
		if refIdx < len(siblings)-1 {
			nextOrder = siblings[refIdx+1].DayOrder
		}
		return generateKeyBetween(siblings[refIdx].DayOrder, nextOrder), nil
	}
	return "", fmt.Errorf("invalid position kind: %s", kind)
}

// generateKeyBetween wraps the Go fracdex library to match the JS fractional-indexing library.
// The error fallback branches are defense-in-depth: GenerateBetween only returns errors for
// invalid inputs (a >= b), which cannot happen with well-ordered data from the database.
func generateKeyBetween(a, b string) string {
	result, err := fractionalindex.GenerateBetween(a, b)
	if err != nil {
		return generateKeyFallback(a, b)
	}
	return result
}

// generateKeyFallback provides deterministic keys when GenerateBetween fails (defense-in-depth).
func generateKeyFallback(a, b string) string {
	if a == "" {
		return "a0"
	}
	if b == "" {
		return a + "V"
	}
	return a + "V"
}

func (s *Server) handleSyncVersion(w http.ResponseWriter, r *http.Request) {
	sv, err := s.repo.GetSyncVersion(r.Context())
	if err != nil {
		mapRepoError(w, err, "sync version")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"version":    sv.Version,
		"updated_at": formatTime(sv.UpdatedAt),
	})
}

func (s *Server) handleSyncChanges(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	if since == "" {
		writeError(w, http.StatusBadRequest, "Missing required query parameter: since", "BAD_REQUEST")
		return
	}

	sinceTime, err := time.Parse(time.RFC3339Nano, since)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid since parameter: must be a valid ISO 8601 timestamp", "BAD_REQUEST")
		return
	}

	ctx := r.Context()

	// Snapshot window: capture server_now BEFORE querying
	serverNow := time.Now().UTC()

	filter := repository.ListRecordsFilter{
		IncludeDeleted: true,
		UpdatedAfter:   &sinceTime,
		UpdatedBefore:  &serverNow,
	}

	records, err := s.repo.ListRecords(ctx, filter)
	if err != nil {
		mapRepoError(w, err, "records")
		return
	}

	// Sort for API: date DESC, day_order ASC, id ASC
	sortRecordsForAPI(records)

	childCounts, err := s.repo.CountRecordChildren(ctx, serveRecordIDs(records))
	if err != nil {
		mapRepoError(w, err, "record files")
		return
	}

	items := make([]recordSummary, 0, len(records))
	for _, record := range records {
		items = append(items, buildRecordSummary(record, childCounts[record.ID]))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"server_now": formatTime(serverNow),
	})
}

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	recordID := r.PathValue("recordId")
	fileType := r.PathValue("fileType")
	filename := r.PathValue("filename")

	if !isValidRecordID(recordID) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid record ID: %s", recordID), "INVALID_ID")
		return
	}

	if fileType != "figures" && fileType != "data" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid file type: %s. Must be \"figures\" or \"data\"", fileType), "BAD_REQUEST")
		return
	}

	if !isValidFilename(filename) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid filename: %s", filename), "BAD_REQUEST")
		return
	}

	ctx := r.Context()
	fileExists, err := s.recordFileExists(ctx, recordID, fileType, filename)
	if err != nil {
		mapRepoError(w, err, "record files")
		return
	}

	if !fileExists {
		writeError(w, http.StatusNotFound, fmt.Sprintf("File not found: %s/%s", fileType, filename), "NOT_FOUND")
		return
	}

	// Return JSON {url, expires_at} pointing to our local-files endpoint
	scheme := "http"
	host := r.Host
	if host == "" {
		host = fmt.Sprintf("127.0.0.1:%d", s.port)
	}
	fileURL := fmt.Sprintf(
		"%s://%s/local-files/%s/%s/%s",
		scheme,
		host,
		url.PathEscape(recordID),
		url.PathEscape(fileType),
		url.PathEscape(filename),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"url":        fileURL,
		"expires_at": "2099-01-01T00:00:00Z",
	})
}

// recordFileExists reports whether the requested record file is present in repository metadata.
func (s *Server) recordFileExists(
	ctx context.Context,
	recordID string,
	fileType string,
	filename string,
) (bool, error) {
	switch fileType {
	case "figures":
		figures, err := s.repo.ListRecordFiguresByRecordID(ctx, recordID)
		if err != nil {
			return false, err
		}
		for _, figure := range figures {
			if figure.Filename == filename {
				return true, nil
			}
		}
		return false, nil
	case "data":
		dataFiles, err := s.repo.ListRecordDataFilesByRecordID(ctx, recordID)
		if err != nil {
			return false, err
		}
		for _, dataFile := range dataFiles {
			if dataFile.Filename == filename {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("unknown file type %q", fileType)
	}
}

func (s *Server) handleServeFile(w http.ResponseWriter, r *http.Request) {
	recordID := r.PathValue("recordId")
	fileType := r.PathValue("fileType")
	filename := r.PathValue("filename")

	if !isValidRecordID(recordID) || !isValidFilename(filename) {
		http.NotFound(w, r)
		return
	}

	if fileType != "figures" && fileType != "data" {
		http.NotFound(w, r)
		return
	}

	filePath := filepath.Join(s.dataDir, fileType, recordID, filename)

	// Verify the resolved path is within the data directory
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	absDataDir, _ := filepath.Abs(s.dataDir)
	if !strings.HasPrefix(absPath, absDataDir+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, filePath)
}
