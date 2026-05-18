package cli

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/personal-context/cli/internal/repository"

	_ "modernc.org/sqlite"
)

func TestStageReplacementFileSuccess(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(sourcePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	finalPath := filepath.Join(dir, "nested", "dest.txt")
	tempPath, size, err := stageReplacementFile(finalPath, sourcePath)
	if err != nil {
		t.Fatalf("stageReplacementFile() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(tempPath) })

	if size != int64(len("hello")) {
		t.Fatalf("expected size %d, got %d", len("hello"), size)
	}
	content, err := os.ReadFile(tempPath)
	if err != nil {
		t.Fatalf("read staged file: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("unexpected staged content: %q", string(content))
	}
}

func TestRunEditRejectsArchivedProjectBeforeMutation(t *testing.T) {
	setupEnv(t)
	id := addRecordWithContent(t, "<html><body>before</body></html>", "", "", nil, nil)

	editDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(editDir, "record.html"), []byte("<html><body>after</body></html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(editDir, "metadata.json"), []byte(`{"project_id":"archived-edit-project","source_device_id":"test-device"}`), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	ensureRegisteredProjectAndDevice(t, "archived-edit-project", "test-device")

	homeDir, err := resolveHomeDir()
	if err != nil {
		t.Fatalf("resolve home: %v", err)
	}
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open stack: %v", err)
	}
	if _, err := stack.Repo.ArchiveProject(context.Background(), "archived-edit-project"); err != nil {
		t.Fatalf("archive project: %v", err)
	}
	if err := stack.Close(); err != nil {
		t.Fatalf("close stack: %v", err)
	}

	err = runEdit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, id, editDir)
	if err == nil {
		t.Fatal("expected archived project error")
	}
	if !strings.Contains(err.Error(), "archived") {
		t.Fatalf("runEdit() error = %v, want archived project", err)
	}
}

func TestEditSupportRepositoryErrorBranches(t *testing.T) {
	ctx := context.Background()

	listDataErr := fmt.Errorf("list data failed")
	if _, err := loadExistingEditAssets(ctx, &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
			return []repository.RecordFigure{{ID: 1, Filename: "fig.png"}}, nil
		},
		listDataFilesFn: func(context.Context, string) ([]repository.RecordDataFile, error) {
			return nil, listDataErr
		},
	}, "record-a"); err == nil || !strings.Contains(err.Error(), "list old data files") {
		t.Fatalf("loadExistingEditAssets() error = %v, want data file list context", err)
	}

	updateFigureErr := fmt.Errorf("update figure failed")
	if _, err := upsertEditFigureRecords(ctx, &mockRepo{
		updateFigureFn: func(context.Context, repository.UpdateRecordFigureInput) (repository.RecordFigure, error) {
			return repository.RecordFigure{}, updateFigureErr
		},
	}, []repository.CreateRecordFigureInput{{Filename: "fig.png"}}, map[string]repository.RecordFigure{
		"fig.png": {ID: 1, Filename: "fig.png"},
	}, &editMutationState{}); err == nil || !strings.Contains(err.Error(), "update figure record") {
		t.Fatalf("upsertEditFigureRecords(update) error = %v", err)
	}

	createFigureErr := fmt.Errorf("create figure failed")
	if _, err := upsertEditFigureRecords(ctx, &mockRepo{
		createFigureFn: func(context.Context, repository.CreateRecordFigureInput) (repository.RecordFigure, error) {
			return repository.RecordFigure{}, createFigureErr
		},
	}, []repository.CreateRecordFigureInput{{Filename: "new.png"}}, nil, &editMutationState{}); err == nil || !strings.Contains(err.Error(), "create figure record") {
		t.Fatalf("upsertEditFigureRecords(create) error = %v", err)
	}

	updateDataErr := fmt.Errorf("update data failed")
	if _, err := upsertEditDataFileRecords(ctx, &mockRepo{
		updateDataFileFn: func(context.Context, repository.UpdateRecordDataFileInput) (repository.RecordDataFile, error) {
			return repository.RecordDataFile{}, updateDataErr
		},
	}, []repository.CreateRecordDataFileInput{{Filename: "data.csv"}}, map[string]repository.RecordDataFile{
		"data.csv": {ID: 1, Filename: "data.csv"},
	}, &editMutationState{}); err == nil || !strings.Contains(err.Error(), "update data file record") {
		t.Fatalf("upsertEditDataFileRecords(update) error = %v", err)
	}

	createDataErr := fmt.Errorf("create data failed")
	if _, err := upsertEditDataFileRecords(ctx, &mockRepo{
		createDataFileFn: func(context.Context, repository.CreateRecordDataFileInput) (repository.RecordDataFile, error) {
			return repository.RecordDataFile{}, createDataErr
		},
	}, []repository.CreateRecordDataFileInput{{Filename: "data.csv"}}, nil, &editMutationState{}); err == nil || !strings.Contains(err.Error(), "create data file record") {
		t.Fatalf("upsertEditDataFileRecords(create) error = %v", err)
	}

	deleteFigureErr := fmt.Errorf("delete figure failed")
	if _, err := deleteRemovedEditFigures(ctx, &mockRepo{
		deleteFigureFn: func(context.Context, int64) error {
			return deleteFigureErr
		},
	}, []repository.RecordFigure{{ID: 1, Filename: "old.png"}}, nil, &editMutationState{}); err == nil || !strings.Contains(err.Error(), "delete old figure record") {
		t.Fatalf("deleteRemovedEditFigures() error = %v", err)
	}

	deleteDataErr := fmt.Errorf("delete data failed")
	if _, err := deleteRemovedEditDataFiles(ctx, &mockRepo{
		deleteDataFileFn: func(context.Context, int64) error {
			return deleteDataErr
		},
	}, []repository.RecordDataFile{{ID: 1, Filename: "old.csv"}}, nil, &editMutationState{}); err == nil || !strings.Contains(err.Error(), "delete old data file record") {
		t.Fatalf("deleteRemovedEditDataFiles() error = %v", err)
	}
}

func TestStageReplacementFileMissingSource(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "dest.txt")
	if _, _, err := stageReplacementFile(finalPath, filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatal("expected error for missing source file")
	}
}

func TestStageReplacementFileDestinationMkdirError(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(sourcePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	finalPath := filepath.Join(blocker, "dest.txt")
	if _, _, err := stageReplacementFile(finalPath, sourcePath); err == nil {
		t.Fatal("expected mkdir error when parent path is a file")
	}
}

func TestRunEditRollsBackRecordOnStageFailure(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecordWithContent(t,
		"<html><body>before</body></html>", "", "",
		nil,
		map[string][]byte{"x.csv": []byte("before,data\n1,2\n")},
	)

	editDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(editDir, "record.html"), []byte("<html><body>after</body></html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, editDir)
	dataDir := filepath.Join(editDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "x.csv"), []byte("new,data\n3,4\n"), 0o644); err != nil {
		t.Fatalf("write x.csv: %v", err)
	}
	yPath := filepath.Join(dataDir, "y.csv")
	if err := os.WriteFile(yPath, []byte("blocked,data\n5,6\n"), 0o644); err != nil {
		t.Fatalf("write y.csv: %v", err)
	}
	if err := os.Chmod(yPath, 0o000); err != nil {
		t.Fatalf("chmod y.csv: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(yPath, 0o644) })

	err := runEdit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, id, editDir)
	if err == nil {
		t.Fatal("expected edit failure when staging unreadable data file")
	}
	if !strings.Contains(err.Error(), "stage data file y.csv") {
		t.Fatalf("expected stage error, got %v", err)
	}

	db := openEditInternalDB(t, homeDir)
	var htmlContent string
	if err := db.QueryRow("SELECT html_content FROM records WHERE id = ?", id).Scan(&htmlContent); err != nil {
		t.Fatalf("query html_content: %v", err)
	}
	if htmlContent != "<html><body>before</body></html>" {
		t.Fatalf("expected html rollback to original value, got %q", htmlContent)
	}
}

func TestRunEditFailsWhenCommitRenameTargetIsDirectory(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecord(t)

	finalFigurePath := filepath.Join(homeDir, "personal-context", "figures", id, "bad.png")
	if err := os.MkdirAll(finalFigurePath, 0o755); err != nil {
		t.Fatalf("mkdir final figure path: %v", err)
	}

	editDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(editDir, "record.html"), []byte(`<html><img src="figures/bad.png"></html>`), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, editDir)
	figuresDir := filepath.Join(editDir, "figures")
	if err := os.MkdirAll(figuresDir, 0o755); err != nil {
		t.Fatalf("mkdir figures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(figuresDir, "bad.png"), []byte("new-figure"), 0o644); err != nil {
		t.Fatalf("write bad.png: %v", err)
	}

	err := runEdit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, id, editDir)
	if err == nil {
		t.Fatal("expected commit rename failure")
	}
	if !strings.Contains(err.Error(), "backup existing file bad.png") {
		t.Fatalf("expected staged commit error, got %v", err)
	}

	db := openEditInternalDB(t, homeDir)
	var figureRows int
	if err := db.QueryRow("SELECT COUNT(*) FROM record_figures WHERE record_id = ?", id).Scan(&figureRows); err != nil {
		t.Fatalf("count figure rows: %v", err)
	}
	if figureRows != 0 {
		t.Fatalf("expected no persisted figure rows after failed edit, got %d", figureRows)
	}
}

func TestRunEditRollsBackDataFileRowOnCommitFailure(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecordWithContent(t,
		"<html><body>before</body></html>",
		"", "",
		nil,
		map[string][]byte{"x.csv": []byte("before,data\n1,2\n")},
	)

	finalDataPath := filepath.Join(homeDir, "personal-context", "data", id, "x.csv")
	if err := os.Remove(finalDataPath); err != nil {
		t.Fatalf("remove old data file: %v", err)
	}
	if err := os.MkdirAll(finalDataPath, 0o755); err != nil {
		t.Fatalf("mkdir data path blocker: %v", err)
	}

	db := openEditInternalDB(t, homeDir)
	var beforeHash string
	if err := db.QueryRow("SELECT hash FROM record_data_files WHERE record_id = ? AND filename = ?", id, "x.csv").Scan(&beforeHash); err != nil {
		t.Fatalf("query old hash: %v", err)
	}

	editDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(editDir, "record.html"), []byte("<html><body>after</body></html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, editDir)
	dataDir := filepath.Join(editDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "x.csv"), []byte("new,data\n3,4\n"), 0o644); err != nil {
		t.Fatalf("write x.csv: %v", err)
	}

	err := runEdit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, id, editDir)
	if err == nil {
		t.Fatal("expected commit rename failure")
	}
	if !strings.Contains(err.Error(), "backup existing file x.csv") {
		t.Fatalf("expected staged commit error, got %v", err)
	}

	var afterHash string
	if err := db.QueryRow("SELECT hash FROM record_data_files WHERE record_id = ? AND filename = ?", id, "x.csv").Scan(&afterHash); err != nil {
		t.Fatalf("query hash after failed edit: %v", err)
	}
	if afterHash != beforeHash {
		t.Fatalf("expected data row hash rollback, before=%s after=%s", beforeHash, afterHash)
	}
}

func TestRunEditRestoresEarlierCommittedFilesWhenLaterCommitFails(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecordWithContent(t,
		`<html><img src="figures/a.png"></html>`,
		"", "",
		map[string][]byte{"a.png": []byte("old-a")},
		nil,
	)

	finalBPath := filepath.Join(homeDir, "personal-context", "figures", id, "b.png")
	if err := os.MkdirAll(finalBPath, 0o755); err != nil {
		t.Fatalf("mkdir final b path: %v", err)
	}

	editDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(editDir, "record.html"), []byte(`<html><img src="figures/a.png"><img src="figures/b.png"></html>`), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, editDir)
	figuresDir := filepath.Join(editDir, "figures")
	if err := os.MkdirAll(figuresDir, 0o755); err != nil {
		t.Fatalf("mkdir figures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(figuresDir, "a.png"), []byte("new-a"), 0o644); err != nil {
		t.Fatalf("write a.png: %v", err)
	}
	if err := os.WriteFile(filepath.Join(figuresDir, "b.png"), []byte("new-b"), 0o644); err != nil {
		t.Fatalf("write b.png: %v", err)
	}

	err := runEdit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, id, editDir)
	if err == nil {
		t.Fatal("expected commit failure for b.png")
	}
	if !strings.Contains(err.Error(), "backup existing file b.png") {
		t.Fatalf("expected backup failure for b.png, got %v", err)
	}

	aPath := filepath.Join(homeDir, "personal-context", "figures", id, "a.png")
	aContent, err := os.ReadFile(aPath)
	if err != nil {
		t.Fatalf("read a.png after failed edit: %v", err)
	}
	if string(aContent) != "old-a" {
		t.Fatalf("expected a.png rollback to old content, got %q", string(aContent))
	}

	db := openEditInternalDB(t, homeDir)
	var bCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM record_figures WHERE record_id = ? AND filename = ?", id, "b.png").Scan(&bCount); err != nil {
		t.Fatalf("count b.png rows: %v", err)
	}
	if bCount != 0 {
		t.Fatalf("expected b.png row rollback, got count %d", bCount)
	}
}

func TestRunEditRestoresFilesAndRowsWhenDeletePhaseFails(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecordWithContent(t,
		`<html><img src="figures/keep.png"><img src="figures/remove.png"></html>`,
		"", "",
		map[string][]byte{
			"keep.png":   []byte("old-keep"),
			"remove.png": []byte("old-remove"),
		},
		nil,
	)

	db := openEditInternalDB(t, homeDir)
	if _, err := db.Exec(`
CREATE TRIGGER block_remove_figure_delete
BEFORE DELETE ON record_figures
WHEN OLD.filename = 'remove.png'
BEGIN
  SELECT RAISE(FAIL, 'blocked figure delete');
END;
`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	editDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(editDir, "record.html"), []byte(`<html><img src="figures/keep.png"></html>`), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, editDir)
	figuresDir := filepath.Join(editDir, "figures")
	if err := os.MkdirAll(figuresDir, 0o755); err != nil {
		t.Fatalf("mkdir figures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(figuresDir, "keep.png"), []byte("new-keep"), 0o644); err != nil {
		t.Fatalf("write keep.png: %v", err)
	}

	err := runEdit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, id, editDir)
	if err == nil {
		t.Fatal("expected delete-phase failure")
	}
	if !strings.Contains(err.Error(), "delete old figure record remove.png") {
		t.Fatalf("expected delete-phase error, got %v", err)
	}

	keepPath := filepath.Join(homeDir, "personal-context", "figures", id, "keep.png")
	keepContent, err := os.ReadFile(keepPath)
	if err != nil {
		t.Fatalf("read keep.png after failed edit: %v", err)
	}
	if string(keepContent) != "old-keep" {
		t.Fatalf("expected keep.png rollback to old content, got %q", string(keepContent))
	}

	removePath := filepath.Join(homeDir, "personal-context", "figures", id, "remove.png")
	removeContent, err := os.ReadFile(removePath)
	if err != nil {
		t.Fatalf("read remove.png after failed edit: %v", err)
	}
	if string(removeContent) != "old-remove" {
		t.Fatalf("expected remove.png to remain unchanged, got %q", string(removeContent))
	}

	var figureRows int
	if err := db.QueryRow("SELECT COUNT(*) FROM record_figures WHERE record_id = ?", id).Scan(&figureRows); err != nil {
		t.Fatalf("count figure rows: %v", err)
	}
	if figureRows != 2 {
		t.Fatalf("expected figure rows rollback to original count, got %d", figureRows)
	}
}

func TestRunEditRestoresDataRowsWhenDeletePhaseFailsAfterPartialDeletes(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecordWithContent(t,
		`<html><body>data delete failure</body></html>`,
		"", "",
		nil,
		map[string][]byte{
			"keep.csv":  []byte("old,keep\n1,1\n"),
			"drop1.csv": []byte("old,drop1\n2,2\n"),
			"drop2.csv": []byte("old,drop2\n3,3\n"),
		},
	)

	db := openEditInternalDB(t, homeDir)
	rows, err := db.Query("SELECT filename FROM record_data_files WHERE record_id = ? AND filename <> ? ORDER BY id", id, "keep.csv")
	if err != nil {
		t.Fatalf("query removable filenames: %v", err)
	}
	t.Cleanup(func() {
		_ = rows.Close()
	})

	removable := make([]string, 0, 2)
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			t.Fatalf("scan removable filename: %v", err)
		}
		removable = append(removable, filename)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate removable filenames: %v", err)
	}
	if len(removable) != 2 {
		t.Fatalf("expected 2 removable data files, got %d", len(removable))
	}

	failFilename := removable[1]
	escapedFailFilename := strings.ReplaceAll(failFilename, "'", "''")
	triggerSQL := fmt.Sprintf(`
CREATE TRIGGER block_selected_data_delete
BEFORE DELETE ON record_data_files
WHEN OLD.filename = '%s'
BEGIN
  SELECT RAISE(FAIL, 'blocked data delete');
END;
`, escapedFailFilename)
	if _, err := db.Exec(triggerSQL); err != nil {
		t.Fatalf("create data delete trigger: %v", err)
	}

	editDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(editDir, "record.html"), []byte(`<html><body>updated</body></html>`), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, editDir)
	dataDir := filepath.Join(editDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "keep.csv"), []byte("new,keep\n9,9\n"), 0o644); err != nil {
		t.Fatalf("write keep.csv: %v", err)
	}

	err = runEdit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, id, editDir)
	if err == nil {
		t.Fatal("expected data delete-phase failure")
	}
	if !strings.Contains(err.Error(), "delete old data file record "+failFilename) {
		t.Fatalf("expected delete-phase failure for %s, got %v", failFilename, err)
	}

	keepPath := filepath.Join(homeDir, "personal-context", "data", id, "keep.csv")
	keepContent, err := os.ReadFile(keepPath)
	if err != nil {
		t.Fatalf("read keep.csv after failed edit: %v", err)
	}
	if string(keepContent) != "old,keep\n1,1\n" {
		t.Fatalf("expected keep.csv rollback to old content, got %q", string(keepContent))
	}

	var dataRows int
	if err := db.QueryRow("SELECT COUNT(*) FROM record_data_files WHERE record_id = ?", id).Scan(&dataRows); err != nil {
		t.Fatalf("count data rows: %v", err)
	}
	if dataRows != 3 {
		t.Fatalf("expected data rows rollback to original count, got %d", dataRows)
	}
}

func TestRunEditReconcilesCreateUpdateAndDeletePaths(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecordWithContent(t,
		`<html><img src="figures/keep.png"><img src="figures/remove.png"></html>`,
		"", "",
		map[string][]byte{
			"keep.png":   []byte("old-keep"),
			"remove.png": []byte("old-remove"),
		},
		map[string][]byte{
			"keep.csv":   []byte("old,keep\n1,1\n"),
			"remove.csv": []byte("old,remove\n2,2\n"),
		},
	)

	editDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(editDir, "record.html"), []byte(`<html><img src="figures/keep.png"><img src="figures/new.png"></html>`), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, editDir)

	figuresDir := filepath.Join(editDir, "figures")
	if err := os.MkdirAll(figuresDir, 0o755); err != nil {
		t.Fatalf("mkdir figures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(figuresDir, "keep.png"), []byte("new-keep"), 0o644); err != nil {
		t.Fatalf("write keep.png: %v", err)
	}
	if err := os.WriteFile(filepath.Join(figuresDir, "new.png"), []byte("brand-new"), 0o644); err != nil {
		t.Fatalf("write new.png: %v", err)
	}

	dataDir := filepath.Join(editDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "keep.csv"), []byte("new,keep\n3,3\n"), 0o644); err != nil {
		t.Fatalf("write keep.csv: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "new.csv"), []byte("brand,new\n4,4\n"), 0o644); err != nil {
		t.Fatalf("write new.csv: %v", err)
	}

	if err := runEdit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, id, editDir); err != nil {
		t.Fatalf("runEdit() error = %v", err)
	}

	db := openEditInternalDB(t, homeDir)
	var figureCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM record_figures WHERE record_id = ?", id).Scan(&figureCount); err != nil {
		t.Fatalf("count figures: %v", err)
	}
	if figureCount != 2 {
		t.Fatalf("expected 2 figure rows after edit, got %d", figureCount)
	}

	var dataCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM record_data_files WHERE record_id = ?", id).Scan(&dataCount); err != nil {
		t.Fatalf("count data files: %v", err)
	}
	if dataCount != 2 {
		t.Fatalf("expected 2 data-file rows after edit, got %d", dataCount)
	}

	if _, err := os.Stat(filepath.Join(homeDir, "personal-context", "figures", id, "remove.png")); !os.IsNotExist(err) {
		t.Fatalf("expected remove.png to be deleted, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(homeDir, "personal-context", "data", id, "remove.csv")); !os.IsNotExist(err) {
		t.Fatalf("expected remove.csv to be deleted, got err=%v", err)
	}
}

func openEditInternalDB(t *testing.T, homeDir string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestCommitStagedFilesRestoresBackupWhenRenameFails(t *testing.T) {
	dir := t.TempDir()

	// Create an existing file at the final path so backup succeeds.
	finalPath := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(finalPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Point the temp path at a file that does not exist so os.Rename fails.
	mutations := &editMutationState{
		stagedFiles: []stagedReplacementFile{{
			TempPath:  filepath.Join(dir, "nonexistent-temp"),
			FinalPath: finalPath,
		}},
	}

	err := mutations.commitStagedFiles()
	if err == nil {
		t.Fatal("expected error when temp file does not exist for rename")
	}
	if !strings.Contains(err.Error(), "commit staged file") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the backup was restored — the original content should be back.
	content, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("expected backup to restore original content, got %q", string(content))
	}
}

func TestBackupExistingFileForEditMissingDestination(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "missing.txt")

	backupPath, err := backupExistingFileForEdit(finalPath)
	if err != nil {
		t.Fatalf("backupExistingFileForEdit() error = %v", err)
	}
	if backupPath != "" {
		t.Fatalf("expected empty backup path for missing destination, got %q", backupPath)
	}
}

func TestStageReplacementFileErrorBranches(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := stageReplacementFile(filepath.Join(dir, "dest.txt"), filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatal("expected missing source file to fail")
	}

	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	source := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if _, _, err := stageReplacementFile(filepath.Join(blocker, "dest.txt"), source); err == nil {
		t.Fatal("expected destination parent creation to fail")
	}
}

func TestBackupExistingFileForEditMovesExistingFile(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "dest.txt")
	if err := os.WriteFile(finalPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write destination file: %v", err)
	}

	backupPath, err := backupExistingFileForEdit(finalPath)
	if err != nil {
		t.Fatalf("backupExistingFileForEdit() error = %v", err)
	}
	if backupPath == "" {
		t.Fatal("expected non-empty backup path")
	}

	if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
		t.Fatalf("expected destination to be moved away, got err=%v", err)
	}
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup file: %v", err)
	}
	if string(backupContent) != "old" {
		t.Fatalf("unexpected backup content %q", string(backupContent))
	}
}

func TestBackupExistingFileForEditRejectsDirectoryDestination(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "dest")
	if err := os.MkdirAll(finalPath, 0o755); err != nil {
		t.Fatalf("mkdir destination directory: %v", err)
	}

	if _, err := backupExistingFileForEdit(finalPath); err == nil {
		t.Fatal("expected error for directory destination path")
	}
}
