package gitsnapshot

import (
	"bufio"
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

// FormatVersion is the record/global snapshot format version.
const FormatVersion = 1

// ChatFormatVersion is the chat metadata format version. v2 renamed source_path
// to original_source_path and added raw_source_key; v1 chat metadata is
// intentionally rejected on import (clean-cut pre-release schema change). The
// optional parent_source_session_id field was later added within v2 (additive,
// omitted when null), so existing v2 snapshots remain compatible.
const ChatFormatVersion = 2

// Snapshot is the deterministic git-export representation of Personal Context data.
type Snapshot struct {
	Templates []Template
	Projects  []RegistryEntry
	Devices   []RegistryEntry
	Records   []Record
	Chats     []ChatSession
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

// ChatSession is an exported chat directory with metadata and normalized items.
type ChatSession struct {
	ID                    string
	Source                string
	SourceSessionID       string
	ParentSourceSessionID *string
	SourceDeviceID        string
	ProjectID             *string
	CWD                   *string
	Title                 *string
	StartedAt             time.Time
	LastActivityAt        time.Time
	OriginalSourcePath    *string
	RawSourceKey          *string
	RawSourceContent      []byte
	CreatedAt             time.Time
	UpdatedAt             time.Time
	Items                 []ChatItem
}

// ChatItem is a normalized exported chat item.
type ChatItem struct {
	Ordinal    int
	Role       string
	ItemType   string
	Text       *string
	SearchText string
	RawJSON    *string
	CreatedAt  time.Time
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

type chatMetadataFile struct {
	FormatVersion         int     `json:"format_version"`
	ID                    string  `json:"id"`
	Source                string  `json:"source"`
	SourceSessionID       string  `json:"source_session_id"`
	ParentSourceSessionID *string `json:"parent_source_session_id,omitempty"`
	SourceDeviceID        string  `json:"source_device_id"`
	ProjectID             *string `json:"project_id,omitempty"`
	CWD                   *string `json:"cwd,omitempty"`
	Title                 *string `json:"title,omitempty"`
	StartedAt             string  `json:"started_at"`
	LastActivityAt        string  `json:"last_activity_at"`
	OriginalSourcePath    *string `json:"original_source_path,omitempty"`
	RawSourceKey          *string `json:"raw_source_key,omitempty"`
	// SourcePathLegacy is populated when decoding a v1 snapshot; it triggers an
	// import-time rejection so legacy snapshots are not silently re-imported.
	SourcePathLegacy *string `json:"source_path,omitempty"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type chatItemFile struct {
	Ordinal    int     `json:"ordinal"`
	Role       string  `json:"role"`
	ItemType   string  `json:"item_type"`
	Text       *string `json:"text,omitempty"`
	SearchText string  `json:"search_text"`
	RawJSON    *string `json:"raw_json,omitempty"`
	CreatedAt  string  `json:"created_at"`
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
	syncDirFn    = syncDir
)

// syncableDir is the subset of *os.File used to durably flush a directory.
type syncableDir interface {
	Sync() error
	Close() error
}

// openDirFn opens a directory for fsync. It is a seam (mirroring createTempFileFn)
// so the Sync failure path in syncDir can be exercised in tests.
var openDirFn = func(dir string) (syncableDir, error) {
	return os.Open(dir)
}

// syncDir fsyncs a directory so that prior renames into or out of it survive a
// crash. On the POSIX platforms this CLI targets (darwin, linux) a rename is
// only durably recorded once the containing directory's own metadata is
// flushed; without this the directory entry can be lost even after the renamed
// file's contents were fsynced.
func syncDir(dir string) error {
	d, err := openDirFn(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

var managedSnapshotEntries = []string{"projects.json", "devices.json", "templates", "records", "chats"}

// Write stages a deterministic snapshot, then replaces the managed export entries under root.
func Write(root string, snapshot Snapshot) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("root path is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create export root: %w", err)
	}
	stagingDir, err := os.MkdirTemp(root, ".snapshot-staging-*")
	if err != nil {
		return fmt.Errorf("create staging snapshot dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	if err := writeSnapshotContents(stagingDir, snapshot); err != nil {
		return err
	}
	if err := replaceSnapshotContents(root, stagingDir); err != nil {
		return err
	}

	return nil
}

func writeSnapshotContents(root string, snapshot Snapshot) error {
	templatesDir := filepath.Join(root, "templates")
	recordsDir := filepath.Join(root, "records")
	chatsDir := filepath.Join(root, "chats")
	projectsPath := filepath.Join(root, "projects.json")
	devicesPath := filepath.Join(root, "devices.json")
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

	chats := append([]ChatSession(nil), snapshot.Chats...)
	sort.Slice(chats, func(i, j int) bool {
		if !chats[i].LastActivityAt.Equal(chats[j].LastActivityAt) {
			return chats[i].LastActivityAt.Before(chats[j].LastActivityAt)
		}
		return chats[i].ID < chats[j].ID
	})
	if len(chats) > 0 {
		if err := os.MkdirAll(chatsDir, 0o755); err != nil {
			return fmt.Errorf("create chats dir: %w", err)
		}
	}
	for _, chat := range chats {
		if err := validatePathSegment("chat id", chat.ID); err != nil {
			return err
		}
		chatDir := filepath.Join(chatsDir, chat.ID)
		if err := os.MkdirAll(chatDir, 0o755); err != nil {
			return fmt.Errorf("create chat dir %s: %w", chat.ID, err)
		}
		metadataBytes, err := json.MarshalIndent(chatMetadataFile{
			FormatVersion:         ChatFormatVersion,
			ID:                    chat.ID,
			Source:                chat.Source,
			SourceSessionID:       chat.SourceSessionID,
			ParentSourceSessionID: chat.ParentSourceSessionID,
			SourceDeviceID:        chat.SourceDeviceID,
			ProjectID:             chat.ProjectID,
			CWD:                   chat.CWD,
			Title:                 chat.Title,
			StartedAt:             chat.StartedAt.UTC().Format(time.RFC3339Nano),
			LastActivityAt:        chat.LastActivityAt.UTC().Format(time.RFC3339Nano),
			OriginalSourcePath:    chat.OriginalSourcePath,
			RawSourceKey:          chat.RawSourceKey,
			CreatedAt:             chat.CreatedAt.UTC().Format(time.RFC3339Nano),
			UpdatedAt:             chat.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal chat metadata for %s: %w", chat.ID, err)
		}
		metadataBytes = append(metadataBytes, '\n')
		if err := writeFile(filepath.Join(chatDir, "metadata.json"), metadataBytes); err != nil {
			return fmt.Errorf("write chat metadata for %s: %w", chat.ID, err)
		}
		items := append([]ChatItem(nil), chat.Items...)
		sort.Slice(items, func(i, j int) bool { return items[i].Ordinal < items[j].Ordinal })
		var itemBytes []byte
		for _, item := range items {
			line, err := json.Marshal(chatItemFile{
				Ordinal:    item.Ordinal,
				Role:       item.Role,
				ItemType:   item.ItemType,
				Text:       item.Text,
				SearchText: item.SearchText,
				RawJSON:    item.RawJSON,
				CreatedAt:  item.CreatedAt.UTC().Format(time.RFC3339Nano),
			})
			if err != nil {
				return fmt.Errorf("marshal chat item %s/%d: %w", chat.ID, item.Ordinal, err)
			}
			itemBytes = append(itemBytes, line...)
			itemBytes = append(itemBytes, '\n')
		}
		if err := writeFile(filepath.Join(chatDir, "items.jsonl"), itemBytes); err != nil {
			return fmt.Errorf("write chat items for %s: %w", chat.ID, err)
		}
		if chat.RawSourceKey != nil {
			if len(chat.RawSourceContent) == 0 {
				return fmt.Errorf("chat %s raw_source_key is set but raw source content is empty", chat.ID)
			}
			rawName, err := rawSourceExportFilename(chat.ID, *chat.RawSourceKey)
			if err != nil {
				return fmt.Errorf("chat %s raw source key: %w", chat.ID, err)
			}
			if err := writeFile(filepath.Join(chatDir, rawName), chat.RawSourceContent); err != nil {
				return fmt.Errorf("write chat raw source for %s: %w", chat.ID, err)
			}
		}
	}

	if err := writeRegistryFile(projectsPath, snapshot.Projects); err != nil {
		return fmt.Errorf("write projects.json: %w", err)
	}
	if err := writeRegistryFile(devicesPath, snapshot.Devices); err != nil {
		return fmt.Errorf("write devices.json: %w", err)
	}

	// Flush the managed container directories so their child entries (record/chat
	// id subdirectories, template files) are durable before these inodes are
	// renamed into the export root. writeFile already fsynced each file's
	// immediate parent (e.g. records/<id>), but never the records/chats/templates
	// directories themselves, so without this their entries could be lost on a
	// crash even though the renamed-in directory itself is present. templates/ and
	// records/ are always created above; chats/ only when chats were staged.
	containerDirs := []string{templatesDir, recordsDir}
	if len(chats) > 0 {
		containerDirs = append(containerDirs, chatsDir)
	}
	for _, dir := range containerDirs {
		if err := syncDirFn(dir); err != nil {
			return fmt.Errorf("sync staged %s: %w", filepath.Base(dir), err)
		}
	}

	return nil
}

func replaceSnapshotContents(root string, stagingRoot string) error {
	backupDir, err := os.MkdirTemp(root, ".snapshot-backup-*")
	if err != nil {
		return fmt.Errorf("create snapshot backup dir: %w", err)
	}
	cleanupBackup := true
	defer func() {
		if cleanupBackup {
			_ = os.RemoveAll(backupDir)
		}
	}()

	movedExisting := make([]string, 0, len(managedSnapshotEntries))
	promoted := make([]string, 0, len(managedSnapshotEntries))
	for _, name := range managedSnapshotEntries {
		target := filepath.Join(root, name)
		if _, err := os.Lstat(target); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return restoreSnapshotContents(root, backupDir, movedExisting, promoted, fmt.Errorf("inspect existing %s: %w", name, err), &cleanupBackup)
		}
		if err := renameFileFn(target, filepath.Join(backupDir, name)); err != nil {
			return restoreSnapshotContents(root, backupDir, movedExisting, promoted, fmt.Errorf("backup existing %s: %w", name, err), &cleanupBackup)
		}
		movedExisting = append(movedExisting, name)
	}

	for _, name := range managedSnapshotEntries {
		staged := filepath.Join(stagingRoot, name)
		if _, err := os.Lstat(staged); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return restoreSnapshotContents(root, backupDir, movedExisting, promoted, fmt.Errorf("inspect staged %s: %w", name, err), &cleanupBackup)
		}
		if err := renameFileFn(staged, filepath.Join(root, name)); err != nil {
			return restoreSnapshotContents(root, backupDir, movedExisting, promoted, fmt.Errorf("promote staged %s: %w", name, err), &cleanupBackup)
		}
		promoted = append(promoted, name)
	}

	// Flush the export root so the promote renames are durably recorded. Without
	// this, a crash after a successful return could lose the directory entries
	// even though each staged file was fsynced, leaving a partial snapshot.
	if err := syncDirFn(root); err != nil {
		return restoreSnapshotContents(root, backupDir, movedExisting, promoted, fmt.Errorf("sync export root: %w", err), &cleanupBackup)
	}

	// The snapshot is now committed. Remove the backup directory and flush root
	// again so the removal is durable before we return success. Without this
	// second sync, a crash between return and the OS persisting the directory
	// entry deletion could resurrect the backup directory, leaving stale data
	// that could confuse future reads. Backup-removal errors are non-fatal
	// (the snapshot write succeeded) but are propagated so callers know cleanup
	// is needed.
	cleanupBackup = false
	if err := os.RemoveAll(backupDir); err != nil {
		return fmt.Errorf("remove snapshot backup dir: %w", err)
	}
	if err := syncDirFn(root); err != nil {
		return fmt.Errorf("sync export root after backup removal: %w", err)
	}

	return nil
}

func restoreSnapshotContents(root string, backupDir string, movedExisting []string, promoted []string, cause error, cleanupBackup *bool) error {
	restoreErrors := make([]string, 0)
	for i := len(promoted) - 1; i >= 0; i-- {
		name := promoted[i]
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			restoreErrors = append(restoreErrors, fmt.Sprintf("remove promoted %s: %v", name, err))
		}
	}
	for i := len(movedExisting) - 1; i >= 0; i-- {
		name := movedExisting[i]
		target := filepath.Join(root, name)
		if err := os.RemoveAll(target); err != nil {
			restoreErrors = append(restoreErrors, fmt.Sprintf("clear target %s: %v", name, err))
			continue
		}
		if err := renameFileFn(filepath.Join(backupDir, name), target); err != nil {
			restoreErrors = append(restoreErrors, fmt.Sprintf("restore %s: %v", name, err))
		}
	}
	// Flush the export root so the restore renames are durably recorded before we
	// report the original failure and (on success) delete the backup.
	if err := syncDirFn(root); err != nil {
		restoreErrors = append(restoreErrors, fmt.Sprintf("sync export root: %v", err))
	}
	if len(restoreErrors) > 0 {
		*cleanupBackup = false
		return fmt.Errorf("%w; failed to restore previous snapshot state from %s: %s", cause, backupDir, strings.Join(restoreErrors, "; "))
	}
	return cause
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
	chats, err := readChats(filepath.Join(root, "chats"))
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Templates: templates, Projects: projects, Devices: devices, Records: records, Chats: chats}, nil
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

func readChats(dir string) ([]ChatSession, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read chats dir: %w", err)
	}
	chats := make([]ChatSession, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("unexpected file in chats export: %s", entry.Name())
		}
		chat, err := readChat(filepath.Join(dir, entry.Name()), entry.Name())
		if err != nil {
			return nil, err
		}
		chats = append(chats, chat)
	}
	sort.Slice(chats, func(i, j int) bool {
		if !chats[i].LastActivityAt.Equal(chats[j].LastActivityAt) {
			return chats[i].LastActivityAt.Before(chats[j].LastActivityAt)
		}
		return chats[i].ID < chats[j].ID
	})
	return chats, nil
}

func readChat(dir string, chatID string) (ChatSession, error) {
	if err := validatePathSegment("chat id", chatID); err != nil {
		return ChatSession{}, err
	}
	metadataBytes, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return ChatSession{}, fmt.Errorf("read chat metadata for %s: %w", chatID, err)
	}
	var metadata chatMetadataFile
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return ChatSession{}, fmt.Errorf("parse chat metadata for %s: %w", chatID, err)
	}
	if metadata.FormatVersion != ChatFormatVersion {
		return ChatSession{}, fmt.Errorf("unsupported chat format_version %d for %s (expected %d)", metadata.FormatVersion, chatID, ChatFormatVersion)
	}
	if metadata.SourcePathLegacy != nil {
		return ChatSession{}, fmt.Errorf("chat %s metadata contains legacy source_path; re-export with current format", chatID)
	}
	if metadata.ID != chatID {
		return ChatSession{}, fmt.Errorf("chat metadata id %s does not match dir %s", metadata.ID, chatID)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, metadata.StartedAt)
	if err != nil {
		return ChatSession{}, fmt.Errorf("parse started_at for chat %s: %w", chatID, err)
	}
	lastActivityAt, err := time.Parse(time.RFC3339Nano, metadata.LastActivityAt)
	if err != nil {
		return ChatSession{}, fmt.Errorf("parse last_activity_at for chat %s: %w", chatID, err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, metadata.CreatedAt)
	if err != nil {
		return ChatSession{}, fmt.Errorf("parse created_at for chat %s: %w", chatID, err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, metadata.UpdatedAt)
	if err != nil {
		return ChatSession{}, fmt.Errorf("parse updated_at for chat %s: %w", chatID, err)
	}
	items, err := readChatItems(filepath.Join(dir, "items.jsonl"), chatID)
	if err != nil {
		return ChatSession{}, err
	}
	var rawSourceContent []byte
	if metadata.RawSourceKey != nil {
		rawName, err := rawSourceExportFilename(chatID, *metadata.RawSourceKey)
		if err != nil {
			return ChatSession{}, fmt.Errorf("chat %s raw source key: %w", chatID, err)
		}
		rawSourceContent, err = os.ReadFile(filepath.Join(dir, rawName))
		if err != nil {
			return ChatSession{}, fmt.Errorf("read chat raw source for %s: %w", chatID, err)
		}
	}
	return ChatSession{
		ID:                    chatID,
		Source:                metadata.Source,
		SourceSessionID:       metadata.SourceSessionID,
		ParentSourceSessionID: metadata.ParentSourceSessionID,
		SourceDeviceID:        metadata.SourceDeviceID,
		ProjectID:             metadata.ProjectID,
		CWD:                   metadata.CWD,
		Title:                 metadata.Title,
		StartedAt:             startedAt.UTC(),
		LastActivityAt:        lastActivityAt.UTC(),
		OriginalSourcePath:    metadata.OriginalSourcePath,
		RawSourceKey:          metadata.RawSourceKey,
		RawSourceContent:      rawSourceContent,
		CreatedAt:             createdAt.UTC(),
		UpdatedAt:             updatedAt.UTC(),
		Items:                 items,
	}, nil
}

func rawSourceExportFilename(chatID string, rawSourceKey string) (string, error) {
	parts := strings.Split(rawSourceKey, "/")
	if len(parts) != 4 || parts[0] != "chats" || parts[1] != "raw" {
		return "", fmt.Errorf("expected chats/raw/{chat_session_id}/source.{json|jsonl|ndjson}, got %q", rawSourceKey)
	}
	if parts[2] != chatID {
		return "", fmt.Errorf("raw source key chat id %q does not match snapshot chat %q", parts[2], chatID)
	}
	name := parts[3]
	switch name {
	case "source.json", "source.jsonl", "source.ndjson":
		return name, nil
	default:
		return "", fmt.Errorf("expected source.{json|jsonl|ndjson}, got %q", name)
	}
}

func readChatItems(path string, chatID string) ([]ChatItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read chat items for %s: %w", chatID, err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	// 256 MiB matches the chatimport JSONL parser so any item Write produced
	// can be Read back; the old 10 MiB cap could make a successfully-exported
	// chat unreadable on import if any item carried a large raw_json/text.
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024*1024)
	var items []ChatItem
	seen := map[int]struct{}{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw chatItemFile
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse chat item for %s: %w", chatID, err)
		}
		if _, ok := seen[raw.Ordinal]; ok {
			return nil, fmt.Errorf("duplicate chat item ordinal %d for %s", raw.Ordinal, chatID)
		}
		seen[raw.Ordinal] = struct{}{}
		createdAt, err := time.Parse(time.RFC3339Nano, raw.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse chat item created_at for %s/%d: %w", chatID, raw.Ordinal, err)
		}
		items = append(items, ChatItem{
			Ordinal:    raw.Ordinal,
			Role:       raw.Role,
			ItemType:   raw.ItemType,
			Text:       raw.Text,
			SearchText: raw.SearchText,
			RawJSON:    raw.RawJSON,
			CreatedAt:  createdAt.UTC(),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan chat items for %s: %w", chatID, err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Ordinal < items[j].Ordinal })
	return items, nil
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
	// Flush the parent directory so the rename of the staged file is durably
	// recorded; an fsync of the file alone does not guarantee its directory entry
	// survives a crash.
	if err := syncDirFn(filepath.Dir(path)); err != nil {
		return err
	}
	return nil
}
