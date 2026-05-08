package gitsnapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const FormatVersion = 1

// Snapshot is the deterministic git-export representation of Personal Context data.
type Snapshot struct {
	Templates []Template
	Projects  []RegistryEntry
	Devices   []RegistryEntry
	Records    []Record
}

// Template is an exported HTML template file.
type Template struct {
	Name        string
	HTMLContent string
}

// Record is an exported record directory with metadata, content, and figures.
type Record struct {
	ID             string
	Date           string
	DayOrder       string
	ProjectID      string
	SourceDeviceID string
	SourceRef      *string
	GitRemoteURL   *string
	GitHash        *string
	HTMLContent    *string
	Notes          *string
	Figures        []Figure
	DataFiles      []DataFile
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// RegistryEntry is an exported project/device registry row.
type RegistryEntry struct {
	ID         string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt *time.Time
}

// Figure is a metadata row plus exported file bytes.
type Figure struct {
	Filename string
	S3Key    string
	AltText  *string
	Content  []byte
}

// DataFile is exported as metadata-only; binaries stay outside git export.
type DataFile struct {
	Filename    string
	S3Key       string
	Size        int64
	Hash        string
	Description *string
}

type metadataFile struct {
	FormatVersion  int          `json:"format_version"`
	ID             string       `json:"id"`
	Date           string       `json:"date"`
	DayOrder       string       `json:"day_order"`
	ProjectID      string       `json:"project_id"`
	SourceDeviceID string       `json:"source_device_id"`
	SourceRef      *string      `json:"source_ref,omitempty"`
	GitRemoteURL   *string      `json:"git_remote_url,omitempty"`
	GitHash        *string      `json:"git_hash,omitempty"`
	HasNotes       bool         `json:"has_notes"`
	Figures        []figureFile `json:"figures"`
	DataFiles      []dataFile   `json:"data_files"`
	CreatedAt      string       `json:"created_at"`
	UpdatedAt      string       `json:"updated_at"`
}

type registryFileEntry struct {
	ID         string  `json:"id"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	ArchivedAt *string `json:"archived_at,omitempty"`
}

type figureFile struct {
	Filename string  `json:"filename"`
	S3Key    string  `json:"s3_key"`
	AltText  *string `json:"alt_text"`
}

type dataFile struct {
	Filename    string  `json:"filename"`
	S3Key       string  `json:"s3_key"`
	Size        int64   `json:"size"`
	Hash        string  `json:"hash"`
	Description *string `json:"description"`
}

var lfsPointerPattern = regexp.MustCompile(`(?s)\Aversion https://git-lfs\.github\.com/spec/v1\r?\noid sha256:[0-9a-fA-F]{64}\r?\nsize [0-9]+\r?\n?\z`)

type tempFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
	Name() string
}

var (
	mkdirAllFn       = os.MkdirAll
	createTempFileFn = func(dir string, pattern string) (tempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
	removeFileFn = os.Remove
	renameFileFn = os.Rename
	chmodFileFn  = os.Chmod
)

// Write replaces the export subdirectories under root with a deterministic snapshot.
func Write(root string, snapshot Snapshot) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("root path is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create export root: %w", err)
	}
	templatesDir := filepath.Join(root, "templates")
	recordsDir := filepath.Join(root, "records")
	projectsPath := filepath.Join(root, "projects.json")
	devicesPath := filepath.Join(root, "devices.json")
	if err := os.Remove(projectsPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset projects.json: %w", err)
	}
	if err := os.Remove(devicesPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset devices.json: %w", err)
	}
	if err := os.RemoveAll(templatesDir); err != nil {
		return fmt.Errorf("reset templates dir: %w", err)
	}
	if err := os.RemoveAll(recordsDir); err != nil {
		return fmt.Errorf("reset records dir: %w", err)
	}
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		return fmt.Errorf("create templates dir: %w", err)
	}
	if err := os.MkdirAll(recordsDir, 0o755); err != nil {
		return fmt.Errorf("create records dir: %w", err)
	}
	templates := append([]Template(nil), snapshot.Templates...)
	sort.Slice(templates, func(i, j int) bool {
		return templates[i].Name < templates[j].Name
	})
	for _, tmpl := range templates {
		if err := validatePathSegment("template name", tmpl.Name); err != nil {
			return err
		}
		path := filepath.Join(templatesDir, tmpl.Name+".html")
		if err := writeFile(path, []byte(tmpl.HTMLContent)); err != nil {
			return fmt.Errorf("write template %s: %w", tmpl.Name, err)
		}
	}

	records := append([]Record(nil), snapshot.Records...)
	sort.Slice(records, func(i, j int) bool {
		if records[i].Date != records[j].Date {
			return records[i].Date < records[j].Date
		}
		if records[i].DayOrder != records[j].DayOrder {
			return records[i].DayOrder < records[j].DayOrder
		}
		return records[i].ID < records[j].ID
	})
	for _, record := range records {
		if err := validatePathSegment("record id", record.ID); err != nil {
			return err
		}
		recordDir := filepath.Join(recordsDir, record.ID)
		if err := os.MkdirAll(recordDir, 0o755); err != nil {
			return fmt.Errorf("create record dir %s: %w", record.ID, err)
		}
		if record.HTMLContent != nil {
			if err := writeFile(filepath.Join(recordDir, "record.html"), []byte(*record.HTMLContent)); err != nil {
				return fmt.Errorf("write record.html for %s: %w", record.ID, err)
			}
		}
		if record.Notes != nil {
			if err := writeFile(filepath.Join(recordDir, "notes.md"), []byte(*record.Notes)); err != nil {
				return fmt.Errorf("write notes.md for %s: %w", record.ID, err)
			}
		}

		figures := append([]Figure(nil), record.Figures...)
		sort.Slice(figures, func(i, j int) bool {
			return figures[i].Filename < figures[j].Filename
		})
		figureDir := filepath.Join(recordDir, "figures")
		if len(figures) > 0 {
			if err := os.MkdirAll(figureDir, 0o755); err != nil {
				return fmt.Errorf("create figures dir for %s: %w", record.ID, err)
			}
		}
		metadataFigures := make([]figureFile, 0, len(figures))
		for _, figure := range figures {
			if err := validatePathSegment("figure filename", figure.Filename); err != nil {
				return err
			}
			if err := writeFile(filepath.Join(figureDir, figure.Filename), figure.Content); err != nil {
				return fmt.Errorf("write figure %s/%s: %w", record.ID, figure.Filename, err)
			}
			metadataFigures = append(metadataFigures, figureFile{
				Filename: figure.Filename,
				S3Key:    figure.S3Key,
				AltText:  figure.AltText,
			})
		}

		dataFiles := append([]DataFile(nil), record.DataFiles...)
		sort.Slice(dataFiles, func(i, j int) bool {
			return dataFiles[i].Filename < dataFiles[j].Filename
		})
		metadataDataFiles := make([]dataFile, 0, len(dataFiles))
		for _, file := range dataFiles {
			if err := validatePathSegment("data file filename", file.Filename); err != nil {
				return err
			}
			metadataDataFiles = append(metadataDataFiles, dataFile(file))
		}

		metadataBytes, err := json.MarshalIndent(metadataFile{
			FormatVersion:  FormatVersion,
			ID:             record.ID,
			Date:           record.Date,
			DayOrder:       record.DayOrder,
			ProjectID:      record.ProjectID,
			SourceDeviceID: record.SourceDeviceID,
			SourceRef:      record.SourceRef,
			GitRemoteURL:   record.GitRemoteURL,
			GitHash:        record.GitHash,
			HasNotes:       record.Notes != nil,
			Figures:        metadataFigures,
			DataFiles:      metadataDataFiles,
			CreatedAt:      record.CreatedAt.UTC().Format(time.RFC3339Nano),
			UpdatedAt:      record.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal metadata for %s: %w", record.ID, err)
		}
		metadataBytes = append(metadataBytes, '\n')
		if err := writeFile(filepath.Join(recordDir, "metadata.json"), metadataBytes); err != nil {
			return fmt.Errorf("write metadata.json for %s: %w", record.ID, err)
		}
	}

	if err := writeRegistryFile(projectsPath, snapshot.Projects); err != nil {
		return fmt.Errorf("write projects.json: %w", err)
	}
	if err := writeRegistryFile(devicesPath, snapshot.Devices); err != nil {
		return fmt.Errorf("write devices.json: %w", err)
	}

	return nil
}

// Read loads and validates a snapshot from disk.
func Read(root string) (Snapshot, error) {
	if strings.TrimSpace(root) == "" {
		return Snapshot{}, fmt.Errorf("root path is required")
	}
	templates, err := readTemplates(filepath.Join(root, "templates"))
	if err != nil {
		return Snapshot{}, err
	}
	records, err := readRecords(filepath.Join(root, "records"))
	if err != nil {
		return Snapshot{}, err
	}
	projects, err := readRegistryFile(filepath.Join(root, "projects.json"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read projects.json: %w", err)
	}
	devices, err := readRegistryFile(filepath.Join(root, "devices.json"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read devices.json: %w", err)
	}
	return Snapshot{Templates: templates, Projects: projects, Devices: devices, Records: records}, nil
}

// Manifest returns a deterministic listing of the snapshot tree for byte-for-byte comparisons.
func Manifest(root string) ([]string, error) {
	entries := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			entries = append(entries, "dir:"+rel)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		entries = append(entries, "file:"+rel+":"+hex.EncodeToString(sum[:])+":"+strconv.Itoa(len(data)))
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(entries)
	return entries, nil
}

func writeRegistryFile(path string, entries []RegistryEntry) error {
	sorted := append([]RegistryEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})
	fileEntries := make([]registryFileEntry, 0, len(sorted))
	for _, entry := range sorted {
		if strings.TrimSpace(entry.ID) == "" {
			return fmt.Errorf("registry id is required")
		}
		if entry.CreatedAt.IsZero() || entry.UpdatedAt.IsZero() {
			return fmt.Errorf("registry %s must have created_at and updated_at", entry.ID)
		}
		var archivedAt *string
		if entry.ArchivedAt != nil {
			value := entry.ArchivedAt.UTC().Format(time.RFC3339Nano)
			archivedAt = &value
		}
		fileEntries = append(fileEntries, registryFileEntry{
			ID:         entry.ID,
			CreatedAt:  entry.CreatedAt.UTC().Format(time.RFC3339Nano),
			UpdatedAt:  entry.UpdatedAt.UTC().Format(time.RFC3339Nano),
			ArchivedAt: archivedAt,
		})
	}
	content, err := json.MarshalIndent(fileEntries, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return writeFile(path, content)
}

func readRegistryFile(path string) ([]RegistryEntry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fileEntries []registryFileEntry
	if err := json.Unmarshal(content, &fileEntries); err != nil {
		return nil, err
	}
	entries := make([]RegistryEntry, 0, len(fileEntries))
	seen := make(map[string]struct{}, len(fileEntries))
	for _, fileEntry := range fileEntries {
		if strings.TrimSpace(fileEntry.ID) == "" {
			return nil, fmt.Errorf("registry id is required")
		}
		if _, exists := seen[fileEntry.ID]; exists {
			return nil, fmt.Errorf("duplicate registry id %s", fileEntry.ID)
		}
		seen[fileEntry.ID] = struct{}{}
		createdAt, err := time.Parse(time.RFC3339Nano, fileEntry.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse created_at for registry %s: %w", fileEntry.ID, err)
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, fileEntry.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse updated_at for registry %s: %w", fileEntry.ID, err)
		}
		var archivedAt *time.Time
		if fileEntry.ArchivedAt != nil {
			parsedArchivedAt, err := time.Parse(time.RFC3339Nano, *fileEntry.ArchivedAt)
			if err != nil {
				return nil, fmt.Errorf("parse archived_at for registry %s: %w", fileEntry.ID, err)
			}
			utcArchivedAt := parsedArchivedAt.UTC()
			archivedAt = &utcArchivedAt
		}
		entries = append(entries, RegistryEntry{
			ID:         fileEntry.ID,
			CreatedAt:  createdAt.UTC(),
			UpdatedAt:  updatedAt.UTC(),
			ArchivedAt: archivedAt,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

func readTemplates(dir string) ([]Template, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read templates dir: %w", err)
	}
	templates := make([]Template, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			return nil, fmt.Errorf("unexpected directory in templates export: %s", entry.Name())
		}
		if filepath.Ext(entry.Name()) != ".html" {
			return nil, fmt.Errorf("template file must end with .html: %s", entry.Name())
		}
		name := strings.TrimSuffix(entry.Name(), ".html")
		if err := validatePathSegment("template name", name); err != nil {
			return nil, err
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", entry.Name(), err)
		}
		templates = append(templates, Template{
			Name:        name,
			HTMLContent: string(content),
		})
	}
	sort.Slice(templates, func(i, j int) bool {
		return templates[i].Name < templates[j].Name
	})
	return templates, nil
}

func readRecords(dir string) ([]Record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read records dir: %w", err)
	}
	records := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("unexpected file in records export: %s", entry.Name())
		}
		record, err := readRecord(filepath.Join(dir, entry.Name()), entry.Name())
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Date != records[j].Date {
			return records[i].Date < records[j].Date
		}
		if records[i].DayOrder != records[j].DayOrder {
			return records[i].DayOrder < records[j].DayOrder
		}
		return records[i].ID < records[j].ID
	})
	return records, nil
}

func readRecord(dir string, recordID string) (Record, error) {
	if err := validatePathSegment("record id", recordID); err != nil {
		return Record{}, err
	}
	metadataBytes, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return Record{}, fmt.Errorf("read metadata for %s: %w", recordID, err)
	}
	var metadata metadataFile
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return Record{}, fmt.Errorf("parse metadata for %s: %w", recordID, err)
	}
	if metadata.FormatVersion != FormatVersion {
		return Record{}, fmt.Errorf("unsupported format_version %d for %s", metadata.FormatVersion, recordID)
	}
	if metadata.ID != recordID {
		return Record{}, fmt.Errorf("metadata id %s does not match record dir %s", metadata.ID, recordID)
	}
	if strings.TrimSpace(metadata.ProjectID) == "" {
		return Record{}, fmt.Errorf("project_id is required for %s", recordID)
	}
	if strings.TrimSpace(metadata.SourceDeviceID) == "" {
		return Record{}, fmt.Errorf("source_device_id is required for %s", recordID)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, metadata.CreatedAt)
	if err != nil {
		return Record{}, fmt.Errorf("parse created_at for %s: %w", recordID, err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, metadata.UpdatedAt)
	if err != nil {
		return Record{}, fmt.Errorf("parse updated_at for %s: %w", recordID, err)
	}
	var htmlContent *string
	htmlBytes, err := os.ReadFile(filepath.Join(dir, "record.html"))
	if err == nil {
		value := string(htmlBytes)
		htmlContent = &value
	} else if !os.IsNotExist(err) {
		return Record{}, fmt.Errorf("read record.html for %s: %w", recordID, err)
	}
	var notes *string
	if metadata.HasNotes {
		notesBytes, err := os.ReadFile(filepath.Join(dir, "notes.md"))
		if err != nil {
			return Record{}, fmt.Errorf("read notes.md for %s: %w", recordID, err)
		}
		value := string(notesBytes)
		notes = &value
	} else if _, err := os.Stat(filepath.Join(dir, "notes.md")); err == nil {
		return Record{}, fmt.Errorf("notes.md present for %s despite has_notes=false", recordID)
	}

	figures, err := readFigures(dir, recordID, metadata.Figures)
	if err != nil {
		return Record{}, err
	}
	dataFiles := make([]DataFile, 0, len(metadata.DataFiles))
	for _, file := range metadata.DataFiles {
		if err := validatePathSegment("data file filename", file.Filename); err != nil {
			return Record{}, err
		}
		dataFiles = append(dataFiles, DataFile(file))
	}

	return Record{
		ID:             recordID,
		Date:           metadata.Date,
		DayOrder:       metadata.DayOrder,
		ProjectID:      metadata.ProjectID,
		SourceDeviceID: metadata.SourceDeviceID,
		SourceRef:      metadata.SourceRef,
		GitRemoteURL:   metadata.GitRemoteURL,
		GitHash:        metadata.GitHash,
		HTMLContent:    htmlContent,
		Notes:          notes,
		Figures:        figures,
		DataFiles:      dataFiles,
		CreatedAt:      createdAt.UTC(),
		UpdatedAt:      updatedAt.UTC(),
	}, nil
}

func readFigures(dir string, recordID string, metadata []figureFile) ([]Figure, error) {
	figures := make([]Figure, 0, len(metadata))
	figureDir := filepath.Join(dir, "figures")
	expected := make(map[string]struct{}, len(metadata))
	for _, figure := range metadata {
		if err := validatePathSegment("figure filename", figure.Filename); err != nil {
			return nil, err
		}
		expected[figure.Filename] = struct{}{}
		content, err := os.ReadFile(filepath.Join(figureDir, figure.Filename))
		if err != nil {
			return nil, fmt.Errorf("read figure %s/%s: %w", recordID, figure.Filename, err)
		}
		if isLFSPointer(content) {
			return nil, fmt.Errorf("figure %s/%s is a Git LFS pointer, not real content", recordID, figure.Filename)
		}
		figures = append(figures, Figure{
			Filename: figure.Filename,
			S3Key:    figure.S3Key,
			AltText:  figure.AltText,
			Content:  content,
		})
	}
	if len(metadata) == 0 {
		if _, err := os.Stat(figureDir); err == nil {
			entries, err := os.ReadDir(figureDir)
			if err != nil {
				return nil, fmt.Errorf("read figures dir for %s: %w", recordID, err)
			}
			if len(entries) > 0 {
				return nil, fmt.Errorf("figures dir for %s contains files not referenced by metadata", recordID)
			}
		}
		return figures, nil
	}
	entries, err := os.ReadDir(figureDir)
	if err != nil {
		return nil, fmt.Errorf("read figures dir for %s: %w", recordID, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return nil, fmt.Errorf("unexpected nested dir in figures for %s: %s", recordID, entry.Name())
		}
		if _, ok := expected[entry.Name()]; !ok {
			return nil, fmt.Errorf("figure file %s/%s not referenced by metadata", recordID, entry.Name())
		}
	}
	return figures, nil
}

func isLFSPointer(data []byte) bool {
	if len(data) > 512 {
		return false
	}
	return lfsPointerPattern.Match(data)
}

func validatePathSegment(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if value == "." || value == ".." {
		return fmt.Errorf("%s must not be %q", field, value)
	}
	if strings.ContainsAny(value, "/\\") {
		return fmt.Errorf("%s must not include path separators: %q", field, value)
	}
	if value != filepath.Base(value) {
		return fmt.Errorf("%s must not include path separators: %q", field, value)
	}
	return nil
}

func writeFile(path string, content []byte) error {
	if err := mkdirAllFn(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tempFile, err := createTempFileFn(filepath.Dir(path), ".snapshot-*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = removeFileFn(tempPath)
		}
	}()
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := renameFileFn(tempPath, path); err != nil {
		return err
	}
	cleanupTemp = false
	if err := chmodFileFn(path, 0o644); err != nil {
		return err
	}
	return nil
}
