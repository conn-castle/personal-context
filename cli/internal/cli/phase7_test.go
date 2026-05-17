package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/gitsnapshot"
	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestExportCommandWritesSnapshot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)
	runSetupCommandForTest(t, homeDir)

	inputDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(inputDir, "figures"), 0o755); err != nil {
		t.Fatalf("mkdir figures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte(`<html><img src="figures/plot.png"></html>`), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir)
	if err := os.WriteFile(filepath.Join(inputDir, "figures", "plot.png"), []byte("plot-bytes"), 0o644); err != nil {
		t.Fatalf("write figure: %v", err)
	}

	addCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", inputDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	exportDir := t.TempDir()
	gitInit := exec.Command("git", "-C", exportDir, "init")
	if output, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("git init exportDir: %v\n%s", err, string(output))
	}
	gitRemote := exec.Command("git", "-C", exportDir, "remote", "add", "origin", "https://github.com/org/repo.git")
	if output, err := gitRemote.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, string(output))
	}
	exportStdout := &bytes.Buffer{}
	exportCmd := NewRootCommand(RootCommandOptions{Stdout: exportStdout, Stderr: &bytes.Buffer{}})
	exportCmd.SetArgs([]string{"export", "--path", exportDir, "--github-remote", "origin"})
	if err := exportCmd.Execute(); err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(exportStdout.String(), exportDir) {
		t.Fatalf("expected export stdout to mention %s, got %q", exportDir, exportStdout.String())
	}
	if _, err := os.Stat(filepath.Join(exportDir, "templates", "text-only.html")); err != nil {
		t.Fatalf("expected exported template: %v", err)
	}
}

func TestImportCommandAppliesSnapshot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)
	runSetupCommandForTest(t, homeDir)

	exportDir := t.TempDir()
	writeSnapshotForCLITest(t, exportDir, gitsnapshot.Snapshot{
		Templates: []gitsnapshot.Template{{Name: "text-only", HTMLContent: "<html>template</html>"}},
		Records: []gitsnapshot.Record{{
			ID:          "20260309-aaaabbbb",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: strPtr("<html><body>imported</body></html>"),
			CreatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		}},
	})

	importCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	importCmd.SetArgs([]string{"import", exportDir})
	if err := importCmd.Execute(); err != nil {
		t.Fatalf("import: %v", err)
	}

	showStdout := &bytes.Buffer{}
	showCmd := NewRootCommand(RootCommandOptions{Stdout: showStdout, Stderr: &bytes.Buffer{}})
	showCmd.SetArgs([]string{"show", "20260309-aaaabbbb"})
	if err := showCmd.Execute(); err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(showStdout.String(), "20260309-aaaabbbb") {
		t.Fatalf("expected imported record in show output, got %q", showStdout.String())
	}
}

func TestRestoreDBCommandReportsBackupPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)
	runSetupCommandForTest(t, homeDir)

	inputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte("<html>before restore</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir)
	addOut := &bytes.Buffer{}
	addCmd := NewRootCommand(RootCommandOptions{Stdout: addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", inputDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	exportDir := t.TempDir()
	writeSnapshotForCLITest(t, exportDir, gitsnapshot.Snapshot{
		Templates: []gitsnapshot.Template{{Name: "text-only", HTMLContent: "<html>template</html>"}},
		Records: []gitsnapshot.Record{{
			ID:          "20260309-ccccdddd",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: strPtr("<html><body>restored</body></html>"),
			CreatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		}},
	})

	restoreStdout := &bytes.Buffer{}
	restoreCmd := NewRootCommand(RootCommandOptions{Stdout: restoreStdout, Stderr: &bytes.Buffer{}})
	restoreCmd.SetArgs([]string{"restore-db", exportDir})
	if err := restoreCmd.Execute(); err != nil {
		t.Fatalf("restore-db: %v", err)
	}
	if !strings.Contains(restoreStdout.String(), "Backup created at ") {
		t.Fatalf("expected restore-db to report backup path, got %q", restoreStdout.String())
	}
}

func TestRestoreDBReportsBackupPathWhenImportFails(t *testing.T) {
	homeDir := setupEnv(t)
	t.Setenv(pcHomeEnvVar, homeDir)

	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	snapshotDir := t.TempDir()
	writeSnapshotForCLITest(t, snapshotDir, gitsnapshot.Snapshot{
		Chats: []gitsnapshot.ChatSession{{
			ID:              "20260309-cafe0001",
			Source:          "codex",
			SourceSessionID: "duplicate-source",
			SourceDeviceID:  "device/unit",
			StartedAt:       now,
			LastActivityAt:  now,
			CreatedAt:       now,
			UpdatedAt:       now,
		}, {
			ID:              "20260309-cafe0002",
			Source:          "codex",
			SourceSessionID: "duplicate-source",
			SourceDeviceID:  "device/unit",
			StartedAt:       now,
			LastActivityAt:  now,
			CreatedAt:       now,
			UpdatedAt:       now,
		}},
	})

	stdout := &bytes.Buffer{}
	err := runRestoreDB(context.Background(), stdout, &bytes.Buffer{}, snapshotDir)
	if err == nil || !strings.Contains(err.Error(), "appears multiple times in snapshot") {
		t.Fatalf("runRestoreDB() error = %v, want duplicate source identity error", err)
	}
	if !strings.Contains(stdout.String(), "Backup created at ") {
		t.Fatalf("restore-db should report backup path before import failure, got %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "Restore complete") {
		t.Fatalf("restore-db should not report completion on import failure, got %q", stdout.String())
	}
}

func TestVerifyCommandLocalRoundTrip(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)
	runSetupCommandForTest(t, homeDir)

	inputDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(inputDir, "figures"), 0o755); err != nil {
		t.Fatalf("mkdir figures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte(`<html><img src="figures/verify.png"></html>`), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir)
	if err := os.WriteFile(filepath.Join(inputDir, "notes.md"), []byte("verify notes"), 0o644); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "figures", "verify.png"), []byte("verify-figure"), 0o644); err != nil {
		t.Fatalf("write figure: %v", err)
	}

	addCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", inputDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	verifyCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	verifyCmd.SetArgs([]string{"verify"})
	if err := verifyCmd.Execute(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyFromCloudNotConfigured(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)
	runSetupCommandForTest(t, homeDir)

	stdout := &bytes.Buffer{}
	err := runVerify(context.Background(), stdout, &bytes.Buffer{}, true)
	if err == nil || !strings.Contains(err.Error(), "cloud is not configured") {
		t.Fatalf("runVerify(fromCloud) error = %v", err)
	}
}

func TestVerifyErrorBranches(t *testing.T) {
	originalResolve := resolveHomeDirFn
	resolveHomeDirFn = func() (string, error) {
		return "", errors.New("home failed")
	}
	if err := runVerify(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, false); err == nil || !strings.Contains(err.Error(), "home failed") {
		t.Fatalf("expected home resolution error, got %v", err)
	}
	resolveHomeDirFn = originalResolve
	t.Cleanup(func() { resolveHomeDirFn = originalResolve })

	homeDir := setupEnv(t)
	origOpenCloud := openCloudStackFn
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errors.New("cloud failed")
	}
	t.Cleanup(func() { openCloudStackFn = origOpenCloud })
	t.Setenv(pcHomeEnvVar, homeDir)
	if err := runVerify(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, true); err == nil || !strings.Contains(err.Error(), "open cloud") {
		t.Fatalf("expected cloud open error, got %v", err)
	}
}

func TestRestoreDBRejectsInvalidSnapshotBeforeHomeResolution(t *testing.T) {
	original := resolveHomeDirFn
	t.Cleanup(func() { resolveHomeDirFn = original })
	resolveHomeDirFn = func() (string, error) {
		return "", errors.New("home should not be resolved")
	}
	err := runRestoreDB(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "read restore snapshot") {
		t.Fatalf("runRestoreDB() error = %v", err)
	}
}

func TestRestoreDBSurfacesEnvironmentCreationFailure(t *testing.T) {
	snapshotDir := t.TempDir()
	writeSnapshotForCLITest(t, snapshotDir, gitsnapshot.Snapshot{})

	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write home blocker: %v", err)
	}
	t.Setenv(pcHomeEnvVar, homeFile)

	err := runRestoreDB(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir)
	if err == nil {
		t.Fatal("expected environment creation error")
	}
	if !strings.Contains(err.Error(), "create .pc directory") {
		t.Fatalf("runRestoreDB() error = %v, want .pc directory context", err)
	}
}

func TestRestoreDBSurfacesHomeResolutionFailure(t *testing.T) {
	snapshotDir := t.TempDir()
	writeSnapshotForCLITest(t, snapshotDir, gitsnapshot.Snapshot{})

	original := resolveHomeDirFn
	t.Cleanup(func() { resolveHomeDirFn = original })
	resolveHomeDirFn = func() (string, error) {
		return "", errors.New("home unavailable")
	}

	err := runRestoreDB(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir)
	if err == nil || !strings.Contains(err.Error(), "home unavailable") {
		t.Fatalf("runRestoreDB() error = %v, want home resolution failure", err)
	}
}

func TestRestoreDBSurfacesCurrentSnapshotFailure(t *testing.T) {
	homeDir := setupEnv(t)
	recordID := addRecordWithContent(
		t,
		`<html><body><img src="figures/missing.png"></body></html>`,
		"",
		"",
		map[string][]byte{"missing.png": []byte("figure")},
		nil,
	)
	if err := os.Remove(filepath.Join(basePath(homeDir), "figures", recordID, "missing.png")); err != nil {
		t.Fatalf("remove local figure: %v", err)
	}
	snapshotDir := t.TempDir()
	writeSnapshotForCLITest(t, snapshotDir, gitsnapshot.Snapshot{})

	err := runRestoreDB(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir)
	if err == nil || !strings.Contains(err.Error(), "read local figure") {
		t.Fatalf("runRestoreDB() error = %v, want current snapshot failure", err)
	}
}

func TestImportSnapshotIntoStackCreatesAndUpdatesRecords(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)
	runSetupCommandForTest(t, homeDir)

	ctx := context.Background()
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack: %v", err)
	}
	defer func() { _ = stack.Close() }()

	notes := "created notes"
	templateHTML := "<html>overridden template</html>"
	createdAt := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	createSnapshot := gitsnapshot.Snapshot{
		Templates: []gitsnapshot.Template{{Name: "text-only", HTMLContent: templateHTML}},
		Records: []gitsnapshot.Record{{
			ID:          "20260309-aaaabbbb",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: strPtr("<html><body>created</body></html>"),
			Notes:       &notes,
			Figures: []gitsnapshot.Figure{{
				Filename: "plot.png",
				S3Key:    "figures/20260309-aaaabbbb/plot.png",
				Content:  []byte("plot-bytes"),
			}},
			DataFiles: []gitsnapshot.DataFile{{
				Filename: "metrics.csv",
				S3Key:    "data/20260309-aaaabbbb/metrics.csv",
				Size:     7,
				Hash:     strings.Repeat("a", 64),
			}},
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}},
	}

	stats, err := importSnapshotIntoStack(ctx, stack, withCLISnapshotDefaults(createSnapshot))
	if err != nil {
		t.Fatalf("importSnapshotIntoStack(create): %v", err)
	}
	if stats.Created != 1 || stats.Updated != 0 || stats.Skipped != 0 {
		t.Fatalf("create stats = %+v", stats)
	}

	template, err := stack.Repo.GetTemplateByName(ctx, "text-only")
	if err != nil {
		t.Fatalf("GetTemplateByName(text-only): %v", err)
	}
	if template.HTMLContent != templateHTML {
		t.Fatalf("template html = %q", template.HTMLContent)
	}

	newNotes := "updated notes"
	updateSnapshot := gitsnapshot.Snapshot{
		Records: []gitsnapshot.Record{{
			ID:          "20260309-aaaabbbb",
			Date:        "2026-03-10",
			DayOrder:    "b0",
			HTMLContent: strPtr("<html><body>updated</body></html>"),
			Notes:       &newNotes,
			Figures: []gitsnapshot.Figure{{
				Filename: "fresh.png",
				S3Key:    "figures/20260309-aaaabbbb/fresh.png",
				Content:  []byte("fresh-bytes"),
			}},
			DataFiles: []gitsnapshot.DataFile{{
				Filename: "fresh.csv",
				S3Key:    "data/20260309-aaaabbbb/fresh.csv",
				Size:     9,
				Hash:     strings.Repeat("d", 64),
			}},
			CreatedAt: createdAt,
			UpdatedAt: updatedAt.Add(time.Minute),
		}},
	}

	stats, err = importSnapshotIntoStack(ctx, stack, withCLISnapshotDefaults(updateSnapshot))
	if err != nil {
		t.Fatalf("importSnapshotIntoStack(update): %v", err)
	}
	if stats.Created != 0 || stats.Updated != 1 || stats.Skipped != 0 {
		t.Fatalf("update stats = %+v", stats)
	}

	record, err := stack.Repo.GetRecordByID(ctx, "20260309-aaaabbbb")
	if err != nil {
		t.Fatalf("GetRecordByID: %v", err)
	}
	if record.HTMLContent == nil || *record.HTMLContent != "<html><body>updated</body></html>" {
		t.Fatalf("record html = %v", record.HTMLContent)
	}
	figures, err := stack.Repo.ListRecordFiguresByRecordID(ctx, record.ID)
	if err != nil {
		t.Fatalf("ListRecordFiguresByRecordID: %v", err)
	}
	if len(figures) != 1 || figures[0].Filename != "fresh.png" {
		t.Fatalf("figures = %#v", figures)
	}
	if _, err := os.Stat(filepath.Join(homeDir, "personal-context", "figures", record.ID, "plot.png")); !os.IsNotExist(err) {
		t.Fatalf("expected old figure to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(homeDir, "personal-context", "figures", record.ID, "fresh.png")); err != nil {
		t.Fatalf("expected new figure to exist: %v", err)
	}
}

func TestEnsureLocalEnvironmentAndWipeLocalState(t *testing.T) {
	homeDir := t.TempDir()

	if err := ensureLocalEnvironment(context.Background(), homeDir); err != nil {
		t.Fatalf("ensureLocalEnvironment: %v", err)
	}
	if _, err := os.Stat(dbPath(homeDir)); err != nil {
		t.Fatalf("expected database after ensureLocalEnvironment: %v", err)
	}
	if _, err := os.Stat(filepath.Join(basePath(homeDir), ".pc", "config.json")); err != nil {
		t.Fatalf("expected config after ensureLocalEnvironment: %v", err)
	}

	for _, path := range []string{
		filepath.Join(basePath(homeDir), "figures", "record-1", "plot.png"),
		filepath.Join(basePath(homeDir), "data", "record-1", "metrics.csv"),
		dbPath(homeDir) + "-wal",
		dbPath(homeDir) + "-shm",
		filepath.Join(basePath(homeDir), ".pc", "last_sync"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	if err := wipeLocalState(homeDir); err != nil {
		t.Fatalf("wipeLocalState: %v", err)
	}
	for _, path := range []string{
		dbPath(homeDir),
		dbPath(homeDir) + "-wal",
		dbPath(homeDir) + "-shm",
		filepath.Join(basePath(homeDir), "figures"),
		filepath.Join(basePath(homeDir), "data"),
		filepath.Join(basePath(homeDir), ".pc", "last_sync"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(basePath(homeDir), ".pc", "config.json")); err != nil {
		t.Fatalf("expected config to survive wipe: %v", err)
	}
}

func TestCompareSnapshotDirsAndValidateGitRemote(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	snapshot := gitsnapshot.Snapshot{
		Templates: []gitsnapshot.Template{{Name: "text-only", HTMLContent: "<html>template</html>"}},
	}
	writeSnapshotForCLITest(t, left, snapshot)
	writeSnapshotForCLITest(t, right, snapshot)

	if err := compareSnapshotDirs(left, right); err != nil {
		t.Fatalf("compareSnapshotDirs(equal): %v", err)
	}
	if err := os.WriteFile(filepath.Join(right, "templates", "text-only.html"), []byte("<html>different</html>"), 0o644); err != nil {
		t.Fatalf("overwrite template: %v", err)
	}
	if err := compareSnapshotDirs(left, right); err == nil {
		t.Fatal("expected compareSnapshotDirs to detect differences")
	}
	if err := validateGitRemote(left, ""); err != nil {
		t.Fatalf("validateGitRemote(empty): %v", err)
	}
	if err := validateGitRemote(left, "origin"); err == nil {
		t.Fatal("expected validateGitRemote to fail for non-git directory")
	}
}

func TestBuildCloudSnapshotAndCloudPhase7Commands(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)
	runSetupCommandForTest(t, homeDir)

	inputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte("<html><body>cloud-backed</body></html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir)
	addCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", inputDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack: %v", err)
	}
	defer func() { _ = stack.Close() }()

	snapshot, err := buildCloudSnapshot(context.Background(), homeDir, &cloudStack{Repo: stack.Repo}, repository.ListRecordsFilter{})
	if err != nil {
		t.Fatalf("buildCloudSnapshot: %v", err)
	}
	if len(snapshot.Templates) == 0 {
		t.Fatal("expected buildCloudSnapshot to include local seeded templates")
	}
	if len(snapshot.Records) != 1 {
		t.Fatalf("expected 1 cloud-backed record, got %d", len(snapshot.Records))
	}

	previousOpenCloudStackFn := openCloudStackFn
	openCloudStackFn = func(_ context.Context, _, _ string) (*cloudStack, error) {
		return &cloudStack{Repo: stack.Repo}, nil
	}
	t.Cleanup(func() { openCloudStackFn = previousOpenCloudStackFn })

	exportDir := t.TempDir()
	if err := runExport(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{
		Path:      exportDir,
		FromCloud: true,
	}); err != nil {
		t.Fatalf("runExport(from-cloud): %v", err)
	}
	if _, err := os.Stat(filepath.Join(exportDir, "records")); err != nil {
		t.Fatalf("expected from-cloud export records dir: %v", err)
	}
	if err := runVerify(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, true); err != nil {
		t.Fatalf("runVerify(from-cloud): %v", err)
	}
}

func runSetupCommandForTest(t *testing.T, homeDir string) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  strings.NewReader("n\n"),
	})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
	}
	ensureRegisteredProjectAndDevice(t, "test/default-project", "test-device")
}

func writeSnapshotForCLITest(t *testing.T, root string, snapshot gitsnapshot.Snapshot) {
	t.Helper()
	snapshot = withCLISnapshotDefaults(snapshot)
	if err := gitsnapshot.Write(root, snapshot); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
}

func withCLISnapshotDefaults(snapshot gitsnapshot.Snapshot) gitsnapshot.Snapshot {
	defaultCreatedAt := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	if len(snapshot.Projects) == 0 {
		snapshot.Projects = []gitsnapshot.RegistryEntry{{
			ID:        "test/default-project",
			CreatedAt: defaultCreatedAt,
			UpdatedAt: defaultCreatedAt,
		}}
	}
	if len(snapshot.Devices) == 0 {
		snapshot.Devices = []gitsnapshot.RegistryEntry{{
			ID:        "test-device",
			CreatedAt: defaultCreatedAt,
			UpdatedAt: defaultCreatedAt,
		}}
	}
	for i := range snapshot.Records {
		if snapshot.Records[i].ProjectID == "" {
			snapshot.Records[i].ProjectID = "test/default-project"
		}
		if snapshot.Records[i].SourceDeviceID == "" {
			snapshot.Records[i].SourceDeviceID = "test-device"
		}
	}
	return snapshot
}
