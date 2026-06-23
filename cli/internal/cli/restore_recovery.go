package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/gitsnapshot"
)

const (
	restoreMarkerFileName       = "restore.marker"
	restorePayloadBackupDirName = ".restore-payload"

	restorePhaseStaging    = "staging"
	restorePhaseCommitting = "committing"
	restorePhaseDone       = "done"
)

var restorePayloadEntries = []string{
	filepath.Join(".pc", "pc.db"),
	filepath.Join(".pc", "pc.db-wal"),
	filepath.Join(".pc", "pc.db-shm"),
	filepath.Join(".pc", "pc.db-journal"),
	filepath.Join(".pc", "last_sync"),
	"figures",
	"data",
	filepath.Join("chats", "raw"),
}

var syncFilePathFn = syncFilePath

type restoreMarker struct {
	Phase           string   `json:"phase"`
	StagingDir      string   `json:"staging_dir"`
	BackupDir       string   `json:"backup_dir"`
	Timestamp       string   `json:"timestamp"`
	StagedEntries   []string `json:"staged_entries"`
	OriginalEntries []string `json:"original_entries"`
	RollbackOnly    bool     `json:"rollback_only,omitempty"`
}

func restoreMarkerTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func newRestoreMarker(phase string, stagingDir string, backupDir string) restoreMarker {
	return restoreMarker{
		Phase:      phase,
		StagingDir: stagingDir,
		BackupDir:  backupDir,
		Timestamp:  restoreMarkerTimestamp(),
	}
}

// recoverInterruptedRestore repairs or clears a marker-recorded restore before
// live SQLite is opened. It leaves the live store in either the old backup state
// or the fully promoted staged state.
func recoverInterruptedRestore(homeDir string) error {
	marker, err := readRestoreMarker(homeDir)
	if err != nil {
		return err
	}
	if marker == nil {
		return nil
	}

	switch marker.Phase {
	case restorePhaseStaging:
		if err := cleanupRestoreStaging(*marker); err != nil {
			return err
		}
		return removeRestoreMarker(homeDir)
	case restorePhaseCommitting:
		if err := recoverCommittingRestore(homeDir, *marker); err != nil {
			return err
		}
		if err := cleanupCompletedRestore(*marker); err != nil {
			return err
		}
		return removeRestoreMarker(homeDir)
	case restorePhaseDone:
		if err := cleanupCompletedRestore(*marker); err != nil {
			return err
		}
		return removeRestoreMarker(homeDir)
	default:
		return fmt.Errorf("restore marker has unknown phase %q", marker.Phase)
	}
}

func readRestoreMarker(homeDir string) (*restoreMarker, error) {
	content, err := os.ReadFile(restoreMarkerPath(homeDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return nil, nil
		}
		return nil, fmt.Errorf("read restore marker: %w", err)
	}
	var marker restoreMarker
	if err := json.Unmarshal(content, &marker); err != nil {
		return nil, fmt.Errorf("parse restore marker: %w", err)
	}
	if marker.Phase == "" || marker.StagingDir == "" || marker.BackupDir == "" || marker.Timestamp == "" {
		return nil, fmt.Errorf("restore marker is missing required fields")
	}
	return &marker, nil
}

func writeRestoreMarker(homeDir string, marker restoreMarker) error {
	if marker.Phase == "" || marker.StagingDir == "" || marker.BackupDir == "" || marker.Timestamp == "" {
		return fmt.Errorf("restore marker is missing required fields")
	}
	content, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("encode restore marker: %w", err)
	}
	content = append(content, '\n')
	if err := writeTextFileAtomically(restoreMarkerPath(homeDir), content, 0o700, 0o600); err != nil {
		return fmt.Errorf("write restore marker: %w", err)
	}
	return nil
}

func removeRestoreMarker(homeDir string) error {
	path := restoreMarkerPath(homeDir)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove restore marker: %w", err)
	}
	if err := syncDirFn(filepath.Dir(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("sync restore marker removal: %w", err)
	}
	return nil
}

func recoverCommittingRestore(homeDir string, marker restoreMarker) error {
	base := basePath(homeDir)
	payloadBackupDir := restorePayloadBackupDir(marker.BackupDir)
	if marker.StagedEntries == nil {
		return fmt.Errorf("restore marker committing phase is missing staged_entries")
	}
	if marker.OriginalEntries == nil {
		return fmt.Errorf("restore marker committing phase is missing original_entries")
	}
	if marker.RollbackOnly {
		if err := gitsnapshot.RestoreReplacementBackup(base, payloadBackupDir, restorePayloadEntries, marker.OriginalEntries); err != nil {
			return fmt.Errorf("roll back interrupted restore: %w", err)
		}
		if !liveDatabaseExists(base) {
			return fmt.Errorf("roll back interrupted restore: restored database is missing")
		}
		return nil
	}
	stagingInfo, stagingErr := os.Stat(marker.StagingDir)
	stagingDirExists := stagingErr == nil && stagingInfo.IsDir()
	stagingDirMissing := errors.Is(stagingErr, os.ErrNotExist)
	if stagingErr == nil && !stagingInfo.IsDir() {
		return fmt.Errorf("restore staging path is not a directory: %s", marker.StagingDir)
	}
	if !restorePathExists(payloadBackupDir) && stagingDirMissing && liveDatabaseExists(base) {
		return nil
	}
	if stagingDirExists {
		if err := gitsnapshot.CompleteReplacement(base, marker.StagingDir, payloadBackupDir, restorePayloadEntries, marker.StagedEntries); err != nil {
			return fmt.Errorf("roll forward interrupted restore: %w", err)
		}
		if liveDatabaseExists(base) {
			return nil
		}
	}
	if err := gitsnapshot.RestoreReplacementBackup(base, payloadBackupDir, restorePayloadEntries, marker.OriginalEntries); err != nil {
		return fmt.Errorf("roll back interrupted restore: %w", err)
	}
	if !liveDatabaseExists(base) {
		return fmt.Errorf("roll back interrupted restore: restored database is missing")
	}
	return nil
}

func restorePathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func liveDatabaseExists(base string) bool {
	info, err := os.Stat(filepath.Join(base, ".pc", "pc.db"))
	return err == nil && !info.IsDir()
}

func cleanupCompletedRestore(marker restoreMarker) error {
	if err := cleanupRestoreStaging(marker); err != nil {
		return err
	}
	payloadBackupDir := restorePayloadBackupDir(marker.BackupDir)
	if err := os.RemoveAll(payloadBackupDir); err != nil {
		return fmt.Errorf("remove restore payload backup: %w", err)
	}
	if err := syncDirFn(marker.BackupDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("sync restore payload backup cleanup: %w", err)
	}
	return nil
}

func cleanupRestoreStaging(marker restoreMarker) error {
	if err := os.RemoveAll(marker.StagingDir); err != nil {
		return fmt.Errorf("remove restore staging dir: %w", err)
	}
	parent := filepath.Dir(marker.StagingDir)
	if err := syncDirFn(parent); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("sync restore staging cleanup: %w", err)
	}
	return nil
}

func restoreMarkerPath(homeDir string) string {
	return filepath.Join(basePath(homeDir), ".pc", restoreMarkerFileName)
}

func restorePayloadBackupDir(backupDir string) string {
	return filepath.Join(backupDir, restorePayloadBackupDirName)
}

func createRestoreStagingDir(homeDir string) (string, error) {
	pcDir := filepath.Join(basePath(homeDir), ".pc")
	if err := ensureDirDurably(pcDir, 0o700); err != nil {
		return "", fmt.Errorf("create restore staging parent: %w", err)
	}
	stagingDir, err := os.MkdirTemp(pcDir, "restore-staging-*")
	if err != nil {
		return "", fmt.Errorf("create restore staging dir: %w", err)
	}
	if err := syncDirFn(pcDir); err != nil {
		return "", fmt.Errorf("sync restore staging parent: %w", err)
	}
	return stagingDir, nil
}

func listExistingRestorePayloadEntries(base string) ([]string, error) {
	entries := make([]string, 0, len(restorePayloadEntries))
	for _, entry := range restorePayloadEntries {
		if _, err := os.Lstat(filepath.Join(base, entry)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("inspect restore payload entry %s: %w", entry, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func syncRestorePayload(base string) error {
	dirs := map[string]struct{}{}
	for _, entry := range restorePayloadEntries {
		path := filepath.Join(base, entry)
		if err := syncRestorePath(path, dirs); err != nil {
			return err
		}
	}
	dirs[filepath.Clean(base)] = struct{}{}
	sortedDirs := make([]string, 0, len(dirs))
	for dir := range dirs {
		sortedDirs = append(sortedDirs, dir)
	}
	sort.Slice(sortedDirs, func(i, j int) bool {
		return len(sortedDirs[i]) > len(sortedDirs[j])
	})
	for _, dir := range sortedDirs {
		if err := syncDirFn(dir); err != nil {
			return fmt.Errorf("sync staged restore directory %s: %w", dir, err)
		}
	}
	return nil
}

func syncRestorePath(path string, dirs map[string]struct{}) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect staged restore path %s: %w", path, err)
	}
	if !info.IsDir() {
		if err := syncFilePathFn(path); err != nil {
			return fmt.Errorf("sync staged restore file %s: %w", path, err)
		}
		dirs[filepath.Clean(filepath.Dir(path))] = struct{}{}
		return nil
	}
	return filepath.WalkDir(path, func(child string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			dirs[filepath.Clean(child)] = struct{}{}
			return nil
		}
		if err := syncFilePathFn(child); err != nil {
			return fmt.Errorf("sync staged restore file %s: %w", child, err)
		}
		dirs[filepath.Clean(filepath.Dir(child))] = struct{}{}
		return nil
	})
}

func syncFilePath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
