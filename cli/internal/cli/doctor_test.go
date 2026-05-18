package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestDoctorResolveHomeDirError(t *testing.T) {
	original := resolveHomeDirFn
	t.Cleanup(func() { resolveHomeDirFn = original })
	resolveHomeDirFn = func() (string, error) {
		return "", errors.New("home dir error")
	}

	stdout := &bytes.Buffer{}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error from resolveHomeDirFn")
	}
}

func TestDoctorOpenLocalStackFail(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	// Write a config that is parseable but make DB path unusable by blocking it with a file
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	if err := store.Write(config.Config{}); err != nil {
		t.Fatalf("Write config error = %v", err)
	}

	// Block the DB path by creating a directory named "pc.db"
	dbFile := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	if err := os.MkdirAll(dbFile, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	stdout := &bytes.Buffer{}
	err = runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when openLocalStack fails")
	}
	if !strings.Contains(stdout.String(), "Database:           FAIL") {
		t.Fatalf("expected Database FAIL in output, got %q", stdout.String())
	}
}

func TestDoctorListRecordsFail(t *testing.T) {
	homeDir := setupEnv(t)

	// Drop the records table to make ListRecords fail
	corruptTable(t, homeDir, "records")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when ListRecords fails")
	}
}

func TestDoctorMissingFigureCheckFail(t *testing.T) {
	homeDir := setupEnv(t)
	addRecord(t)
	corruptTable(t, homeDir, "record_figures")

	err := runDoctor(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, doctorOptions{})
	if err == nil || !strings.Contains(err.Error(), "missing figures check failed") {
		t.Fatalf("runDoctor() error = %v, want missing figures failure", err)
	}
}

func TestDoctorMissingDataFileCheckFail(t *testing.T) {
	homeDir := setupEnv(t)
	addRecord(t)
	corruptTable(t, homeDir, "record_data_files")

	err := runDoctor(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, doctorOptions{})
	if err == nil || !strings.Contains(err.Error(), "missing data files check failed") {
		t.Fatalf("runDoctor() error = %v, want missing data files failure", err)
	}
}

func TestDoctorListChatSessionsFail(t *testing.T) {
	homeDir := setupEnv(t)
	corruptTable(t, homeDir, "chat_session")

	err := runDoctor(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, doctorOptions{})
	if err == nil || !strings.Contains(err.Error(), "list chat sessions failed") {
		t.Fatalf("runDoctor() error = %v, want chat session list failure", err)
	}
}

func TestDoctorCloudWarning(t *testing.T) {
	setupEnv(t)
	original := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = original })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errors.New("cloud unavailable")
	}

	stdout := &bytes.Buffer{}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil || !strings.Contains(err.Error(), "warnings found") {
		t.Fatalf("runDoctor() error = %v, want warnings found", err)
	}
	if !strings.Contains(stdout.String(), "Cloud:") || !strings.Contains(stdout.String(), "cloud unavailable") {
		t.Fatalf("expected cloud warning in output, got %q", stdout.String())
	}
}

// failAfterWriter fails after n successful writes.
type failAfterWriter struct {
	remaining int
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, errors.New("write limit reached")
	}
	w.remaining--
	return len(p), nil
}

func TestDoctorDatabaseSuccessWriteError(t *testing.T) {
	setupEnv(t)

	// Fail on the first write (Database: OK line).
	stdout := &failAfterWriter{remaining: 0}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails")
	}
}

func TestDoctorOrphanedFiguresSuccessWriteError(t *testing.T) {
	setupEnv(t)

	// "Database: OK" is the first write. Fail on the second write (Orphaned figures: OK).
	stdout := &failAfterWriter{remaining: 1}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails")
	}
}

func TestDoctorOrphanedDataSuccessWriteError(t *testing.T) {
	setupEnv(t)

	// "Database: OK", "Orphaned figures: OK". Fail on 3rd (Orphaned data: OK).
	stdout := &failAfterWriter{remaining: 2}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails")
	}
}

func TestDoctorMissingFiguresSuccessWriteError(t *testing.T) {
	setupEnv(t)

	// 4th write is "Missing figures: OK".
	stdout := &failAfterWriter{remaining: 3}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails")
	}
}

func TestDoctorMissingDataFilesSuccessWriteError(t *testing.T) {
	setupEnv(t)

	// 5th write is "Missing data files: OK".
	stdout := &failAfterWriter{remaining: 4}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails")
	}
}

func TestDoctorAllPassedWriteError(t *testing.T) {
	setupEnv(t)

	// 7th write is "All checks passed." after Database, Orphaned figures,
	// Orphaned data, Missing figures, Missing data files, Missing chat raw sources.
	stdout := &failAfterWriter{remaining: 6}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails")
	}
}

func TestDoctorDatabaseFailWriteError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	// Write config but block DB path so openLocalStack fails
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	if err := store.Write(config.Config{}); err != nil {
		t.Fatalf("Write config error = %v", err)
	}
	dbFile := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	if err := os.MkdirAll(dbFile, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	// Use a failing writer so writeDoctorf for the FAIL line also errors
	stdout := &failAfterWriter{remaining: 0}
	err = runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails on FAIL path")
	}
}

func TestDoctorDatabaseReadFailWriteError(t *testing.T) {
	homeDir := setupEnv(t)

	// Corrupt sync_version so GetSyncVersion fails
	corruptTable(t, homeDir, "sync_version")

	// Fail on the first write (Database: FAIL line)
	stdout := &failAfterWriter{remaining: 0}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails")
	}
}

func TestDoctorOrphanedFiguresWarnWriteError(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t,
		`<html><img src="figures/fig.png">body</html>`, "", "",
		map[string][]byte{"fig.png": []byte("data")}, nil,
	)

	// Hard-delete the record, leaving orphan figure dir
	db := openErrorPathsDB(t, homeDir)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := db.Exec("DELETE FROM records WHERE id = ?", id); err != nil {
		t.Fatalf("hard delete record: %v", err)
	}

	// Fail on 2nd write (Orphaned figures: WARN); 1st is "Database: OK"
	stdout := &failAfterWriter{remaining: 1}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails")
	}
}

func TestDoctorMissingFiguresWarnWriteError(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t,
		`<html><img src="figures/fig.png">body</html>`, "", "",
		map[string][]byte{"fig.png": []byte("data")}, nil,
	)

	// Remove figure file
	figurePath := filepath.Join(homeDir, "personal-context", "figures", id, "fig.png")
	if err := os.Remove(figurePath); err != nil {
		t.Fatalf("remove figure: %v", err)
	}

	// Writes: "Database: OK", "Orphaned figures: OK", "Orphaned data: OK", "Missing figures: WARN"
	// Fail on 4th write
	stdout := &failAfterWriter{remaining: 3}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails on Missing figures WARN")
	}
}

func TestDoctorMissingFiguresWarnPathWriteError(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t,
		`<html><img src="figures/fig.png">body</html>`, "", "",
		map[string][]byte{"fig.png": []byte("data")}, nil,
	)

	// Remove figure file
	figurePath := filepath.Join(homeDir, "personal-context", "figures", id, "fig.png")
	if err := os.Remove(figurePath); err != nil {
		t.Fatalf("remove figure: %v", err)
	}

	// Fail on 5th write (the "  recordID/fig.png" path line after "Missing figures: WARN")
	stdout := &failAfterWriter{remaining: 4}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails on missing figure path line")
	}
}

func TestDoctorHealthyNoRecords(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Database:           OK") {
		t.Fatalf("expected 'Database:           OK', got %q", out)
	}
	if !strings.Contains(out, "Orphaned figures:   OK") {
		t.Fatalf("expected 'Orphaned figures:   OK', got %q", out)
	}
	if !strings.Contains(out, "Orphaned data:      OK") {
		t.Fatalf("expected 'Orphaned data:      OK', got %q", out)
	}
	if !strings.Contains(out, "Missing figures:    OK") {
		t.Fatalf("expected 'Missing figures:    OK', got %q", out)
	}
	if !strings.Contains(out, "Missing data files: OK") {
		t.Fatalf("expected 'Missing data files: OK', got %q", out)
	}
	if !strings.Contains(out, "Missing chat raw sources:") {
		t.Fatalf("expected 'Missing chat raw sources:' line, got %q", out)
	}
	if !strings.Contains(out, "All checks passed.") {
		t.Fatalf("expected 'All checks passed.', got %q", out)
	}
}

func TestDoctorHealthyWithRecordAndFigure(t *testing.T) {
	setupEnv(t)

	addRecordWithContent(t,
		`<html><img src="figures/fig.png">body</html>`,
		"", "",
		map[string][]byte{"fig.png": []byte("data")},
		nil,
	)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "All checks passed.") {
		t.Fatalf("expected 'All checks passed.', got %q", out)
	}
}

func TestDoctorHealthyWithRecordAndDataFile(t *testing.T) {
	setupEnv(t)

	addRecordWithContent(t,
		"<html>body</html>",
		"", "",
		nil,
		map[string][]byte{"metrics.csv": []byte("col1,col2\n1,2\n")},
	)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "All checks passed.") {
		t.Fatalf("expected 'All checks passed.', got %q", out)
	}
}

func TestDoctorOrphanedFigureDirectory(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t,
		`<html><img src="figures/fig.png">body</html>`,
		"", "",
		map[string][]byte{"fig.png": []byte("data")},
		nil,
	)

	// Hard-delete the record via SQL, leaving figure directory on disk
	db := openErrorPathsDB(t, homeDir)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := db.Exec("DELETE FROM records WHERE id = ?", id); err != nil {
		t.Fatalf("hard delete record: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for orphaned figures")
	}

	out := stdout.String()
	if !strings.Contains(out, "Orphaned figures:   WARN") {
		t.Fatalf("expected orphaned figures WARN, got %q", out)
	}
	if !strings.Contains(out, id) {
		t.Fatalf("expected record ID %s in warning, got %q", id, out)
	}
}

func TestDoctorOrphanedDataDirectory(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t,
		"<html>body</html>",
		"", "",
		nil,
		map[string][]byte{"data.csv": []byte("a,b\n1,2\n")},
	)

	// Hard-delete the record via SQL, leaving data directory on disk
	db := openErrorPathsDB(t, homeDir)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := db.Exec("DELETE FROM records WHERE id = ?", id); err != nil {
		t.Fatalf("hard delete record: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for orphaned data")
	}

	out := stdout.String()
	if !strings.Contains(out, "Orphaned data:      WARN") {
		t.Fatalf("expected orphaned data WARN, got %q", out)
	}
	if !strings.Contains(out, id) {
		t.Fatalf("expected record ID %s in warning, got %q", id, out)
	}
}

func TestDoctorOrphanedChatRawDirectory(t *testing.T) {
	homeDir := setupEnv(t)
	chatID := "20260514-deadbeef"
	rawPath := filepath.Join(homeDir, "personal-context", "chats", "raw", chatID, "source.json")
	if err := os.MkdirAll(filepath.Dir(rawPath), 0o700); err != nil {
		t.Fatalf("mkdir raw dir: %v", err)
	}
	if err := os.WriteFile(rawPath, []byte(`{"id":"orphan"}`), 0o600); err != nil {
		t.Fatalf("write raw source: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for orphaned chat raw source")
	}
	out := stdout.String()
	if !strings.Contains(out, "Orphaned chat raws: WARN") {
		t.Fatalf("expected orphaned chat raws WARN, got %q", out)
	}
	if !strings.Contains(out, chatID) {
		t.Fatalf("expected chat ID %s in warning, got %q", chatID, out)
	}
}

func TestDoctorChatRawRootFile(t *testing.T) {
	homeDir := setupEnv(t)
	rawRoot := filepath.Join(homeDir, "personal-context", "chats", "raw")
	if err := os.MkdirAll(filepath.Dir(rawRoot), 0o700); err != nil {
		t.Fatalf("mkdir chats dir: %v", err)
	}
	if err := os.WriteFile(rawRoot, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("write raw root blocker: %v", err)
	}

	stdout := &bytes.Buffer{}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil || !strings.Contains(err.Error(), "list chat raw directories") {
		t.Fatalf("runDoctor() error = %v, want chat raw directory list error", err)
	}
}

func TestFindChatRawOrphansRepositoryError(t *testing.T) {
	repo := &mockRepo{
		getChatByIDFn: func(context.Context, string) (repository.ChatSession, error) {
			return repository.ChatSession{}, errors.New("lookup failed")
		},
	}

	_, err := findChatRawOrphans(context.Background(), repo, []string{"20260514-deadbeef"})
	if err == nil || !strings.Contains(err.Error(), "look up chat 20260514-deadbeef") {
		t.Fatalf("findChatRawOrphans() error = %v, want lookup error", err)
	}
}

func TestReportDoctorMissingPathsWritePathError(t *testing.T) {
	writer := &failAfterWriter{remaining: 1}
	_, err := reportDoctorMissingPaths(writer, "Missing figures", "figure files", []string{"20260514-deadbeef/plot.png"})
	if err == nil || !strings.Contains(err.Error(), "write missing figure path") {
		t.Fatalf("reportDoctorMissingPaths() error = %v, want path write error", err)
	}
}

func TestDoctorMissingFigureFile(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t,
		`<html><img src="figures/fig.png">body</html>`,
		"", "",
		map[string][]byte{"fig.png": []byte("data")},
		nil,
	)

	// Delete figure file from disk but leave DB record
	figurePath := filepath.Join(homeDir, "personal-context", "figures", id, "fig.png")
	if err := os.Remove(figurePath); err != nil {
		t.Fatalf("remove figure: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing figures")
	}

	out := stdout.String()
	if !strings.Contains(out, "Missing figures:    WARN") {
		t.Fatalf("expected missing figures WARN, got %q", out)
	}
	if !strings.Contains(out, id+"/fig.png") {
		t.Fatalf("expected figure path in warning, got %q", out)
	}
}

func TestDoctorMissingDataFile(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t,
		"<html>body</html>",
		"", "",
		nil,
		map[string][]byte{"data.csv": []byte("a,b\n1,2\n")},
	)

	// Delete data file from disk but leave DB record
	dataPath := filepath.Join(homeDir, "personal-context", "data", id, "data.csv")
	if err := os.Remove(dataPath); err != nil {
		t.Fatalf("remove data file: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing data files")
	}

	out := stdout.String()
	if !strings.Contains(out, "Missing data files: WARN") {
		t.Fatalf("expected missing data files WARN, got %q", out)
	}
	if !strings.Contains(out, id+"/data.csv") {
		t.Fatalf("expected data file path in warning, got %q", out)
	}
}

func TestDoctorMissingFigureFileInDeletedRecord(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t,
		`<html><img src="figures/fig.png">body</html>`,
		"", "",
		map[string][]byte{"fig.png": []byte("data")},
		nil,
	)

	deleteCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	deleteCmd.SetArgs([]string{"records", "delete", id})
	if err := deleteCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	figurePath := filepath.Join(homeDir, "personal-context", "figures", id, "fig.png")
	if err := os.Remove(figurePath); err != nil {
		t.Fatalf("remove figure: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing figures on deleted record")
	}

	out := stdout.String()
	if !strings.Contains(out, "Missing figures:    WARN") {
		t.Fatalf("expected missing figures WARN, got %q", out)
	}
	if !strings.Contains(out, id+"/fig.png") {
		t.Fatalf("expected figure path in warning, got %q", out)
	}
}

func TestDoctorDatabaseFail(t *testing.T) {
	homeDir := setupEnv(t)

	// Corrupt the sync_version table to make GetSyncVersion fail
	corruptTable(t, homeDir, "sync_version")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for database read failure")
	}

	out := stdout.String()
	if !strings.Contains(out, "Database:           FAIL") {
		t.Fatalf("expected database FAIL, got %q", out)
	}
}

func TestDoctorFigureMetadataReadFail(t *testing.T) {
	homeDir := setupEnv(t)

	addRecordWithContent(t,
		`<html><img src="figures/fig.png">body</html>`,
		"", "",
		map[string][]byte{"fig.png": []byte("data")},
		nil,
	)

	corruptTable(t, homeDir, "record_figures")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for figure metadata read failure")
	}

	out := stdout.String()
	if !strings.Contains(out, "Missing figures:    FAIL") {
		t.Fatalf("expected missing figures FAIL, got %q", out)
	}
}

type orphanRepoErrorStub struct {
	repository.Repository
	err error
}

func (s orphanRepoErrorStub) GetRecordByID(context.Context, string) (repository.Record, error) {
	return repository.Record{}, s.err
}

// --- errWriter for writeDoctorf/writeDoctorln error branches ---

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write error")
}

func TestWriteDoctorfErrorWriter(t *testing.T) {
	err := writeDoctorf(errWriter{}, "test context", "hello %s", "world")
	if err == nil {
		t.Fatal("expected error from writeDoctorf with errWriter")
	}
	if !strings.Contains(err.Error(), "test context") {
		t.Fatalf("expected context in error, got %v", err)
	}
}

func TestWriteDoctorlnErrorWriter(t *testing.T) {
	err := writeDoctorln(errWriter{}, "test context", "hello")
	if err == nil {
		t.Fatal("expected error from writeDoctorln with errWriter")
	}
	if !strings.Contains(err.Error(), "test context") {
		t.Fatalf("expected context in error, got %v", err)
	}
}

// --- Doctor: orphaned figures/data check returns non-ErrNotFound error ---

func TestDoctorOrphanedFiguresCheckFailure(t *testing.T) {
	homeDir := setupEnv(t)

	addRecordWithContent(t,
		`<html><img src="figures/fig.png">body</html>`, "", "",
		map[string][]byte{"fig.png": []byte("data")}, nil,
	)

	// Drop records table so GetRecordByID returns a non-ErrNotFound error
	corruptTable(t, homeDir, "records")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for orphaned figures check failure")
	}

	out := stdout.String()
	if !strings.Contains(out, "Orphaned figures:   FAIL") {
		t.Fatalf("expected 'Orphaned figures:   FAIL', got %q", out)
	}
}

func TestDoctorOrphanedDataCheckFailure(t *testing.T) {
	homeDir := setupEnv(t)

	// Add record with data file only (no figures → no figure dirs on disk)
	addRecordWithContent(t,
		"<html>body</html>", "", "",
		nil, map[string][]byte{"data.csv": []byte("a,b\n1,2\n")},
	)

	// Drop records table so GetRecordByID returns a non-ErrNotFound error.
	// No figure dirs exist, so orphaned figures check passes with empty list.
	corruptTable(t, homeDir, "records")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for orphaned data check failure")
	}

	out := stdout.String()
	if !strings.Contains(out, "Orphaned data:      FAIL") {
		t.Fatalf("expected 'Orphaned data:      FAIL', got %q", out)
	}
}

// --- Doctor: missing data file metadata read failure ---

func TestDoctorDataFileMetadataReadFail(t *testing.T) {
	homeDir := setupEnv(t)

	addRecordWithContent(t,
		"<html>body</html>", "", "",
		nil, map[string][]byte{"data.csv": []byte("a,b\n1,2\n")},
	)

	corruptTable(t, homeDir, "record_data_files")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for data file metadata read failure")
	}

	out := stdout.String()
	if !strings.Contains(out, "Missing data files: FAIL") {
		t.Fatalf("expected 'Missing data files: FAIL', got %q", out)
	}
}

// --- Doctor: figure/data file stat errors (not IsNotExist) ---

func TestDoctorMissingFigureStatError(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t,
		`<html><img src="figures/fig.png">body</html>`, "", "",
		map[string][]byte{"fig.png": []byte("data")}, nil,
	)

	// Remove execute permission so stat fails with EACCES
	figDir := filepath.Join(homeDir, "personal-context", "figures", id)
	if err := os.Chmod(figDir, 0o600); err != nil {
		t.Fatalf("chmod figure dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(figDir, 0o755) })

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for figure stat failure")
	}

	out := stdout.String()
	if !strings.Contains(out, "Missing figures:    FAIL") {
		t.Fatalf("expected 'Missing figures:    FAIL', got %q", out)
	}
}

func TestDoctorMissingDataFileStatError(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t,
		"<html>body</html>", "", "",
		nil, map[string][]byte{"data.csv": []byte("a,b\n1,2\n")},
	)

	// Remove execute permission so stat fails with EACCES
	dataDir := filepath.Join(homeDir, "personal-context", "data", id)
	if err := os.Chmod(dataDir, 0o600); err != nil {
		t.Fatalf("chmod data dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0o755) })

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for data file stat failure")
	}

	out := stdout.String()
	if !strings.Contains(out, "Missing data files: FAIL") {
		t.Fatalf("expected 'Missing data files: FAIL', got %q", out)
	}
}

// --- Doctor: ResolveFigurePath/ResolveDataFilePath errors via invalid filenames ---

func TestDoctorFigureResolvePathError(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t, "<html>body</html>", "", "", nil, nil)

	// Insert a figure record with an invalid filename directly via SQL
	db := openErrorPathsDB(t, homeDir)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO record_figures (record_id, filename, s3_key) VALUES (?, '..', ?)",
		id, "figures/"+id+"/bad",
	); err != nil {
		t.Fatalf("insert bad figure record: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for figure resolve path failure")
	}

	out := stdout.String()
	if !strings.Contains(out, "Missing figures:    FAIL") {
		t.Fatalf("expected 'Missing figures:    FAIL', got %q", out)
	}
}

func TestDoctorDataFileResolvePathError(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t, "<html>body</html>", "", "", nil, nil)

	// Insert a data file record with an invalid filename directly via SQL
	db := openErrorPathsDB(t, homeDir)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO record_data_files (record_id, filename, s3_key, size, hash) VALUES (?, '..', ?, 0, 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')",
		id, "data/"+id+"/bad",
	); err != nil {
		t.Fatalf("insert bad data file record: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for data file resolve path failure")
	}

	out := stdout.String()
	if !strings.Contains(out, "Missing data files: FAIL") {
		t.Fatalf("expected 'Missing data files: FAIL', got %q", out)
	}
}

func TestFindOrphansUnexpectedRepoError(t *testing.T) {
	_, err := findOrphans(context.Background(), orphanRepoErrorStub{err: errors.New("boom")}, []string{"20250307-deadbeef"})
	if err == nil {
		t.Fatal("expected error when GetRecordByID fails unexpectedly")
	}
	if !strings.Contains(err.Error(), "20250307-deadbeef") {
		t.Fatalf("expected record id in error, got %v", err)
	}
}

// cloudRepoStub is a minimal stub satisfying the cloud connectivity Ping in runDoctor.
type cloudRepoStub struct {
	repository.Repository
}

func (cloudRepoStub) GetSyncVersion(context.Context) (repository.SyncVersion, error) {
	return repository.SyncVersion{}, nil
}

// --- Cloud connectivity check tests ---

func TestDoctorCloudOK(t *testing.T) {
	setupEnv(t)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return &cloudStack{Repo: cloudRepoStub{}}, nil
	}

	stdout := &bytes.Buffer{}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Cloud:              OK") {
		t.Fatalf("expected 'Cloud:              OK', got %q", out)
	}
	if !strings.Contains(out, "All checks passed.") {
		t.Fatalf("expected all checks passed, got %q", out)
	}
}

func TestDoctorCloudNotConfiguredSkipsCheck(t *testing.T) {
	setupEnv(t)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errCloudNotConfigured
	}

	stdout := &bytes.Buffer{}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	out := stdout.String()
	if strings.Contains(out, "Cloud") {
		t.Fatalf("expected no Cloud line when not configured, got %q", out)
	}
	if !strings.Contains(out, "All checks passed.") {
		t.Fatalf("expected all checks passed, got %q", out)
	}
}

func TestDoctorCloudUnreachableShowsWarn(t *testing.T) {
	setupEnv(t)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errors.New("connection refused")
	}

	stdout := &bytes.Buffer{}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error for cloud WARN")
	}
	if !strings.Contains(err.Error(), "doctor: warnings found") {
		t.Fatalf("expected 'doctor: warnings found', got %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Cloud:              WARN") {
		t.Fatalf("expected 'Cloud:              WARN', got %q", out)
	}
	if !strings.Contains(out, "connection refused") {
		t.Fatalf("expected error detail in Cloud WARN, got %q", out)
	}
}

func TestDoctorCloudOKWriteError(t *testing.T) {
	setupEnv(t)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return &cloudStack{Repo: cloudRepoStub{}}, nil
	}

	// 6 local checks succeed (Database, Orphaned figures, Orphaned data,
	// Missing figures, Missing data files, Missing chat raw sources). 7th
	// write is Cloud: OK — fail there.
	stdout := &failAfterWriter{remaining: 6}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails on Cloud: OK")
	}
}

func TestDoctorCloudWarnWriteError(t *testing.T) {
	setupEnv(t)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errors.New("connection refused")
	}

	// 6 local checks succeed. 7th write is Cloud: WARN — fail there.
	stdout := &failAfterWriter{remaining: 6}
	err := runDoctor(context.Background(), stdout, &bytes.Buffer{}, doctorOptions{})
	if err == nil {
		t.Fatal("expected error when stdout write fails on Cloud: WARN")
	}
}

// --- Doctor: ListRecordIDsOnDisk error ---

func TestDoctorListRecordIDsOnDiskError(t *testing.T) {
	homeDir := setupEnv(t)

	// Replace figures directory with a file to make ListRecordIDsOnDisk fail
	figuresDir := filepath.Join(homeDir, "personal-context", "figures")
	if err := os.RemoveAll(figuresDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(figuresDir, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when figures dir is a file")
	}
}

func TestScanMissingAttachments_RejectsZeroValueScanner(t *testing.T) {
	_, err := scanMissingAttachments(context.Background(), nil, nil, missingAttachmentScanner{label: "figure"})
	if err == nil {
		t.Fatal("expected error for unset scanner fields, got nil")
	}
	if !strings.Contains(err.Error(), "listFilenames") || !strings.Contains(err.Error(), "resolvePath") {
		t.Fatalf("error should mention required fields, got %q", err)
	}
}

func TestDoctorReportsDirectoryWhereFigureExpected(t *testing.T) {
	homeDir := setupEnv(t)

	id := addRecordWithContent(t,
		`<html><img src="figures/fig.png">body</html>`,
		"", "",
		map[string][]byte{"fig.png": []byte("data")},
		nil,
	)

	figurePath := filepath.Join(homeDir, "personal-context", "figures", id, "fig.png")
	if err := os.Remove(figurePath); err != nil {
		t.Fatalf("remove figure: %v", err)
	}
	if err := os.Mkdir(figurePath, 0o755); err != nil {
		t.Fatalf("mkdir at figure path: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when figure path is a directory")
	}

	out := stdout.String()
	if !strings.Contains(out, "Missing figures:    WARN") {
		t.Fatalf("expected missing figures WARN, got %q", out)
	}
	if !strings.Contains(out, "(is a directory)") {
		t.Fatalf("expected directory annotation, got %q", out)
	}
}

// importChatForDoctor imports a single transcript and returns the new chat ID.
func importChatForDoctor(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	transcript := `{
  "id": "doctor-session",
  "cwd": "/tmp/doctor-chat",
  "title": "Doctor chat",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [{"role": "user", "content": "hi doctor"}]
}`
	if err := os.WriteFile(filepath.Join(root, "session.json"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import: %v", err)
	}
	stdout := &bytes.Buffer{}
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "list", "--format", "ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat list: %v", err)
	}
	chatID := strings.TrimSpace(stdout.String())
	if chatID == "" {
		t.Fatalf("expected at least one chat id, got %q", stdout.String())
	}
	return chatID
}

func TestDoctorChatRawSourcesHealthy(t *testing.T) {
	setupEnv(t)
	_ = importChatForDoctor(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Missing chat raw sources:") {
		t.Fatalf("expected Missing chat raw sources line, got %q", out)
	}
	if !strings.Contains(out, "All checks passed.") {
		t.Fatalf("expected all checks passed, got %q", out)
	}
}

func TestDoctorChatRawSourcesMissingLocal(t *testing.T) {
	homeDir := setupEnv(t)
	chatID := importChatForDoctor(t)

	rawPath := filepath.Join(homeDir, "personal-context", "chats", "raw", chatID, "source.json")
	if err := os.Remove(rawPath); err != nil {
		t.Fatalf("remove raw source: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected doctor to report warnings for missing chat raw source")
	}
	out := stdout.String()
	if !strings.Contains(out, "Missing chat raw sources:") || !strings.Contains(out, "WARN") {
		t.Fatalf("expected Missing chat raw sources WARN, got %q", out)
	}
	if strings.Contains(out, chatID) {
		t.Fatalf("normal mode should not list per-chat detail, got %q", out)
	}
}

func TestDoctorChatRawSourcesVerboseListsDetail(t *testing.T) {
	homeDir := setupEnv(t)
	chatID := importChatForDoctor(t)

	rawPath := filepath.Join(homeDir, "personal-context", "chats", "raw", chatID, "source.json")
	if err := os.Remove(rawPath); err != nil {
		t.Fatalf("remove raw source: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor", "--verbose"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected doctor to fail with verbose details")
	}
	out := stdout.String()
	if !strings.Contains(out, chatID) {
		t.Fatalf("verbose mode should list chat id, got %q", out)
	}
	if !strings.Contains(out, "[local]") {
		t.Fatalf("verbose mode should tag local origin, got %q", out)
	}
	if !strings.Contains(out, "chats/raw/"+chatID+"/source.json") {
		t.Fatalf("verbose mode should include raw_source_key, got %q", out)
	}
}

func TestDoctorChatRawSourcesDeletedChatStillChecked(t *testing.T) {
	homeDir := setupEnv(t)
	chatID := importChatForDoctor(t)

	// Soft-delete the chat — durability check should still report missing raw
	// source files because they are PC-owned content.
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "delete", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat delete: %v", err)
	}
	rawPath := filepath.Join(homeDir, "personal-context", "chats", "raw", chatID, "source.json")
	if err := os.Remove(rawPath); err != nil {
		t.Fatalf("remove raw source: %v", err)
	}

	stdout := &bytes.Buffer{}
	doctor := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	doctor.SetArgs([]string{"doctor", "--verbose"})
	if err := doctor.Execute(); err == nil {
		t.Fatal("expected doctor to flag deleted-chat raw source miss")
	}
	if !strings.Contains(stdout.String(), chatID) {
		t.Fatalf("expected deleted chat id in verbose output, got %q", stdout.String())
	}
}

func TestScanCloudChatRawMissesNilClient(t *testing.T) {
	misses, err := scanCloudChatRawMisses(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("expected nil error for nil client, got %v", err)
	}
	if len(misses) != 0 {
		t.Fatalf("expected no misses for nil client, got %d", len(misses))
	}
}

func TestScanCloudChatRawMissesReportsAbsentObjects(t *testing.T) {
	// HEAD returns 404 for every object, simulating "cloud is empty".
	s3 := newTestS3Client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	key := "chats/raw/20250101-deadbeef/source.json"
	misses, err := scanCloudChatRawMisses(context.Background(), s3, []repository.ChatSession{{ID: "20250101-deadbeef", RawSourceKey: &key}})
	if err != nil {
		t.Fatalf("scanCloudChatRawMisses: %v", err)
	}
	if len(misses) != 1 || misses[0].Origin != "cloud" || misses[0].ChatID != "20250101-deadbeef" {
		t.Fatalf("expected one cloud miss, got %v", misses)
	}
}

func TestScanCloudChatRawMissesPropagatesAuthError(t *testing.T) {
	// HEAD returns 500 — surfaces as a cloud-check error, not a miss.
	s3 := newTestS3Client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	key := "chats/raw/20250101-deadbeef/source.json"
	misses, err := scanCloudChatRawMisses(context.Background(), s3, []repository.ChatSession{{ID: "20250101-deadbeef", RawSourceKey: &key}})
	if err == nil {
		t.Fatal("expected cloud-check error to be surfaced separately")
	}
	if len(misses) != 0 {
		t.Fatalf("expected auth/network errors to not count as misses, got %v", misses)
	}
}

func TestScanLocalChatRawMissesSkipsEmptyKey(t *testing.T) {
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open local stack: %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })
	misses, err := scanLocalChatRawMisses(stack.FS, []repository.ChatSession{{ID: "20250101-deadbeef"}})
	if err != nil {
		t.Fatalf("expected nil scan error, got %v", err)
	}
	if len(misses) != 0 {
		t.Fatalf("expected nil-keyed session to be skipped, got %v", misses)
	}
}

func TestScanLocalChatRawMissesRejectsInvalidKey(t *testing.T) {
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open local stack: %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })
	badKey := "chats/raw/other-id/source.json"
	misses, err := scanLocalChatRawMisses(stack.FS, []repository.ChatSession{{ID: "20250101-deadbeef", RawSourceKey: &badKey}})
	if err != nil {
		t.Fatalf("expected nil scan error for invalid key, got %v", err)
	}
	if len(misses) != 1 {
		t.Fatalf("expected invalid key to be reported as miss, got %v", misses)
	}
	if misses[0].Origin != "local" {
		t.Fatalf("expected local origin, got %q", misses[0].Origin)
	}
}
