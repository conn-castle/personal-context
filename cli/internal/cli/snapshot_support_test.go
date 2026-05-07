package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/gitsnapshot"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/sqlite"
)

func TestSnapshotSupportRoundTripAndUpdatePaths(t *testing.T) {
	ctx := context.Background()
	sourceHome := setupEnv(t)

	slideID := addSlideWithContent(
		t,
		`<html><body><img src="figures/original.png">source</body></html>`,
		"source-notes",
		`{"project_id":"phase7/source","git_remote_url":"https://github.com/org/source","git_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
		map[string][]byte{"original.png": []byte("original-figure")},
		map[string][]byte{"original.csv": []byte("x,y\n1,2\n")},
	)

	sourceStack, err := openLocalStack(sourceHome)
	if err != nil {
		t.Fatalf("open source stack: %v", err)
	}
	defer func() { _ = sourceStack.Close() }()

	if _, err := sourceStack.Repo.CreateTemplate(ctx, repository.CreateTemplateInput{
		Name:        "phase7-custom",
		HTMLContent: "<html>custom</html>",
	}); err != nil {
		t.Fatalf("create custom template: %v", err)
	}

	snapshot, err := buildLocalSnapshot(ctx, sourceStack)
	if err != nil {
		t.Fatalf("buildLocalSnapshot(): %v", err)
	}
	if len(snapshot.Templates) < 3 {
		t.Fatalf("expected builtin templates plus custom template, got %d", len(snapshot.Templates))
	}
	if len(snapshot.Slides) != 1 {
		t.Fatalf("expected 1 slide in snapshot, got %d", len(snapshot.Slides))
	}

	targetHome := t.TempDir()
	if err := ensureLocalEnvironment(ctx, targetHome); err != nil {
		t.Fatalf("ensureLocalEnvironment(): %v", err)
	}
	targetStack, err := openLocalStack(targetHome)
	if err != nil {
		t.Fatalf("open target stack: %v", err)
	}
	defer func() { _ = targetStack.Close() }()

	stats, err := importSnapshotIntoStack(ctx, targetStack, snapshot)
	if err != nil {
		t.Fatalf("importSnapshotIntoStack(create): %v", err)
	}
	if stats.Created != 1 || stats.Updated != 0 || stats.Skipped != 0 {
		t.Fatalf("create stats = %+v", stats)
	}

	updatedSnapshot := snapshot
	updatedSnapshot.Templates = append([]gitsnapshot.Template(nil), snapshot.Templates...)
	updatedSnapshot.Slides = append([]gitsnapshot.Slide(nil), snapshot.Slides...)

	for i := range updatedSnapshot.Templates {
		if updatedSnapshot.Templates[i].Name == "text-only" {
			updatedSnapshot.Templates[i].HTMLContent = "<html>text-only-updated</html>"
		}
	}
	updatedSlide := updatedSnapshot.Slides[0]
	updatedSlide.HTMLContent = `<html><body><img src="figures/fresh.png">updated</body></html>`
	updatedSlide.UpdatedAt = updatedSlide.UpdatedAt.Add(time.Minute)
	updatedSlide.Notes = strPtr("updated-notes")
	updatedSlide.ProjectID = strPtr("phase7/updated")
	updatedSlide.GitRemoteURL = strPtr("https://github.com/org/updated")
	updatedSlide.GitHash = strPtr("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	updatedSlide.Figures = []gitsnapshot.Figure{{
		Filename: "fresh.png",
		S3Key:    filepath.ToSlash(filepath.Join("figures", slideID, "fresh.png")),
		Content:  []byte("fresh-figure"),
	}}
	updatedSlide.DataFiles = []gitsnapshot.DataFile{{
		Filename: "fresh.csv",
		S3Key:    filepath.ToSlash(filepath.Join("data", slideID, "fresh.csv")),
		Size:     int64(len("fresh,data\n")),
		Hash:     hashData("fresh,data\n"),
	}}
	updatedSnapshot.Slides[0] = updatedSlide

	stats, err = importSnapshotIntoStack(ctx, targetStack, updatedSnapshot)
	if err != nil {
		t.Fatalf("importSnapshotIntoStack(update): %v", err)
	}
	if stats.Created != 0 || stats.Updated != 1 || stats.Skipped != 0 {
		t.Fatalf("update stats = %+v", stats)
	}

	figures, err := targetStack.Repo.ListSlideFiguresBySlideID(ctx, slideID)
	if err != nil {
		t.Fatalf("ListSlideFiguresBySlideID(): %v", err)
	}
	if len(figures) != 1 || figures[0].Filename != "fresh.png" {
		t.Fatalf("figures after update = %+v", figures)
	}
	dataFiles, err := targetStack.Repo.ListSlideDataFilesBySlideID(ctx, slideID)
	if err != nil {
		t.Fatalf("ListSlideDataFilesBySlideID(): %v", err)
	}
	if len(dataFiles) != 1 || dataFiles[0].Filename != "fresh.csv" {
		t.Fatalf("data files after update = %+v", dataFiles)
	}
	if _, err := os.Stat(filepath.Join(basePath(targetHome), "figures", slideID, "original.png")); !os.IsNotExist(err) {
		t.Fatalf("original figure should be removed after update, stat err = %v", err)
	}

	stats, err = importSnapshotIntoStack(ctx, targetStack, updatedSnapshot)
	if err != nil {
		t.Fatalf("importSnapshotIntoStack(skip): %v", err)
	}
	if stats.Created != 0 || stats.Updated != 0 || stats.Skipped != 1 {
		t.Fatalf("skip stats = %+v", stats)
	}
}

func TestRunExportImportRestoreAndVerifyLocal(t *testing.T) {
	ctx := context.Background()
	sourceHome := setupEnv(t)
	t.Setenv(pcHomeEnvVar, sourceHome)
	withResolvedHomeDir(t, sourceHome)

	originalID := addSlideWithContent(
		t,
		`<html><body><img src="figures/export.png">export</body></html>`,
		"export-notes",
		`{"project_id":"phase7/export","git_remote_url":"https://github.com/org/export","git_hash":"cccccccccccccccccccccccccccccccccccccccc"}`,
		map[string][]byte{"export.png": []byte("export-figure")},
		map[string][]byte{"export.csv": []byte("col1,col2\n5,6\n")},
	)

	exportDir := t.TempDir()
	stdout := &bytes.Buffer{}
	if err := runExport(ctx, stdout, &bytes.Buffer{}, exportOptions{Path: exportDir}); err != nil {
		t.Fatalf("runExport(): %v", err)
	}
	if !strings.Contains(stdout.String(), "Exported snapshot to") {
		t.Fatalf("unexpected export output: %q", stdout.String())
	}

	targetHome := t.TempDir()
	if err := ensureLocalEnvironment(ctx, targetHome); err != nil {
		t.Fatalf("ensureLocalEnvironment(target): %v", err)
	}
	t.Setenv(pcHomeEnvVar, targetHome)
	withResolvedHomeDir(t, targetHome)

	stdout.Reset()
	if err := runImport(ctx, stdout, &bytes.Buffer{}, exportDir); err != nil {
		t.Fatalf("runImport(): %v", err)
	}
	if !strings.Contains(stdout.String(), "Import complete: created 1, updated 0, skipped 0") {
		t.Fatalf("unexpected import output: %q", stdout.String())
	}

	stdout.Reset()
	if err := runVerify(ctx, stdout, &bytes.Buffer{}, false); err != nil {
		t.Fatalf("runVerify(local): %v", err)
	}
	if !strings.Contains(stdout.String(), "Local round-trip verification passed") {
		t.Fatalf("unexpected verify output: %q", stdout.String())
	}

	extraID := addSlideWithContent(
		t,
		"<html><body>extra-slide</body></html>",
		"",
		"",
		nil,
		nil,
	)
	stdout.Reset()
	if err := runRestoreDB(ctx, stdout, &bytes.Buffer{}, exportDir); err != nil {
		t.Fatalf("runRestoreDB(): %v", err)
	}
	if !strings.Contains(stdout.String(), "Backup created at ") {
		t.Fatalf("restore-db output missing backup path: %q", stdout.String())
	}

	stack, err := openLocalStack(targetHome)
	if err != nil {
		t.Fatalf("openLocalStack(target after restore): %v", err)
	}
	defer func() { _ = stack.Close() }()
	if _, err := stack.Repo.GetSlideByID(ctx, originalID); err != nil {
		t.Fatalf("GetSlideByID(original): %v", err)
	}
	if _, err := stack.Repo.GetSlideByID(ctx, extraID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("extra slide should be removed by restore-db, err = %v", err)
	}
}

func TestSnapshotCommandErrorPathsAndHelpers(t *testing.T) {
	ctx := context.Background()
	homeDir := setupEnv(t)
	t.Setenv(pcHomeEnvVar, homeDir)
	withResolvedHomeDir(t, homeDir)

	if err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{}); err == nil {
		t.Fatal("expected runExport to reject empty --path")
	}
	if err := runImport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected runImport to reject missing snapshot path")
	}
	if err := runRestoreDB(ctx, &bytes.Buffer{}, &bytes.Buffer{}, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected runRestoreDB to reject missing snapshot path")
	}

	exportDir := t.TempDir()
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("mkdir export dir: %v", err)
	}
	if err := validateGitRemote(exportDir, "origin"); err == nil {
		t.Fatal("expected validateGitRemote to fail when remote is missing")
	}

	origOpenCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origOpenCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errCloudNotConfigured
	}
	if err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{Path: t.TempDir(), FromCloud: true}); err == nil {
		t.Fatal("expected runExport(fromCloud) to fail without cloud config")
	}
	if err := runVerify(ctx, &bytes.Buffer{}, &bytes.Buffer{}, true); err == nil {
		t.Fatal("expected runVerify(fromCloud) to fail without cloud config")
	}

	firstHome := t.TempDir()
	secondHome := t.TempDir()
	if err := ensureLocalEnvironment(ctx, firstHome); err != nil {
		t.Fatalf("ensureLocalEnvironment(first): %v", err)
	}
	if err := ensureLocalEnvironment(ctx, secondHome); err != nil {
		t.Fatalf("ensureLocalEnvironment(second): %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.WriteFile(dbPath(firstHome)+suffix, []byte("artifact"), 0o600); err != nil {
			t.Fatalf("write db artifact %s: %v", suffix, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(basePath(firstHome), "figures", "x"), 0o755); err != nil {
		t.Fatalf("mkdir figures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(basePath(firstHome), ".pc", "last_sync"), []byte("1"), 0o600); err != nil {
		t.Fatalf("write last_sync: %v", err)
	}
	if err := wipeLocalState(firstHome); err != nil {
		t.Fatalf("wipeLocalState(): %v", err)
	}
	for _, path := range []string{
		dbPath(firstHome),
		dbPath(firstHome) + "-wal",
		dbPath(firstHome) + "-shm",
		dbPath(firstHome) + "-journal",
		filepath.Join(basePath(firstHome), ".pc", "last_sync"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err = %v", path, err)
		}
	}

	snapshot := gitsnapshot.Snapshot{
		Templates: []gitsnapshot.Template{{Name: "text-only", HTMLContent: "<html>a</html>"}},
	}
	manifestA := t.TempDir()
	manifestB := t.TempDir()
	if err := gitsnapshot.Write(manifestA, snapshot); err != nil {
		t.Fatalf("gitsnapshot.Write(manifestA): %v", err)
	}
	if err := gitsnapshot.Write(manifestB, snapshot); err != nil {
		t.Fatalf("gitsnapshot.Write(manifestB): %v", err)
	}
	if err := compareSnapshotDirs(manifestA, manifestB); err != nil {
		t.Fatalf("compareSnapshotDirs(equal): %v", err)
	}
	if err := os.WriteFile(filepath.Join(manifestB, "templates", "text-only.html"), []byte("<html>changed</html>"), 0o644); err != nil {
		t.Fatalf("mutate manifestB: %v", err)
	}
	if err := compareSnapshotDirs(manifestA, manifestB); err == nil {
		t.Fatal("expected compareSnapshotDirs to detect manifest drift")
	}
}

func withResolvedHomeDir(t *testing.T, homeDir string) {
	t.Helper()

	origResolveHomeDirFn := resolveHomeDirFn
	resolveHomeDirFn = func() (string, error) {
		return homeDir, nil
	}
	t.Cleanup(func() {
		resolveHomeDirFn = origResolveHomeDirFn
	})
}

func hashData(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type snapshotRepoStub struct {
	mockRepo
	listTemplatesFn     func(context.Context) ([]repository.Template, error)
	listSlidesFn        func(context.Context, repository.ListSlidesFilter) ([]repository.Slide, error)
	listFiguresFn       func(context.Context, string) ([]repository.SlideFigure, error)
	listDataFilesFn     func(context.Context, string) ([]repository.SlideDataFile, error)
	getSlideByIDFn      func(context.Context, string) (repository.Slide, error)
	createSlideFn       func(context.Context, repository.CreateSlideInput) (repository.Slide, error)
	updateSlideFn       func(context.Context, repository.UpdateSlideInput) (repository.Slide, error)
	deleteSlideFigureFn func(context.Context, int64) error
	deleteSlideDataFn   func(context.Context, int64) error
	createSlideFigureFn func(context.Context, repository.CreateSlideFigureInput) (repository.SlideFigure, error)
	createSlideDataFn   func(context.Context, repository.CreateSlideDataFileInput) (repository.SlideDataFile, error)
}

func (s *snapshotRepoStub) ListTemplates(ctx context.Context) ([]repository.Template, error) {
	if s.listTemplatesFn != nil {
		return s.listTemplatesFn(ctx)
	}
	return s.mockRepo.ListTemplates(ctx)
}

func (s *snapshotRepoStub) ListSlides(ctx context.Context, filter repository.ListSlidesFilter) ([]repository.Slide, error) {
	if s.listSlidesFn != nil {
		return s.listSlidesFn(ctx, filter)
	}
	return s.mockRepo.ListSlides(ctx, filter)
}

func (s *snapshotRepoStub) ListSlideFiguresBySlideID(ctx context.Context, slideID string) ([]repository.SlideFigure, error) {
	if s.listFiguresFn != nil {
		return s.listFiguresFn(ctx, slideID)
	}
	return s.mockRepo.ListSlideFiguresBySlideID(ctx, slideID)
}

func (s *snapshotRepoStub) ListSlideDataFilesBySlideID(ctx context.Context, slideID string) ([]repository.SlideDataFile, error) {
	if s.listDataFilesFn != nil {
		return s.listDataFilesFn(ctx, slideID)
	}
	return s.mockRepo.ListSlideDataFilesBySlideID(ctx, slideID)
}

func (s *snapshotRepoStub) GetSlideByID(ctx context.Context, slideID string) (repository.Slide, error) {
	if s.getSlideByIDFn != nil {
		return s.getSlideByIDFn(ctx, slideID)
	}
	return s.mockRepo.GetSlideByID(ctx, slideID)
}

func (s *snapshotRepoStub) CreateSlide(ctx context.Context, input repository.CreateSlideInput) (repository.Slide, error) {
	if s.createSlideFn != nil {
		return s.createSlideFn(ctx, input)
	}
	return s.mockRepo.CreateSlide(ctx, input)
}

func (s *snapshotRepoStub) UpdateSlide(ctx context.Context, input repository.UpdateSlideInput) (repository.Slide, error) {
	if s.updateSlideFn != nil {
		return s.updateSlideFn(ctx, input)
	}
	return s.mockRepo.UpdateSlide(ctx, input)
}

func (s *snapshotRepoStub) DeleteSlideFigure(ctx context.Context, id int64) error {
	if s.deleteSlideFigureFn != nil {
		return s.deleteSlideFigureFn(ctx, id)
	}
	return s.mockRepo.DeleteSlideFigure(ctx, id)
}

func (s *snapshotRepoStub) DeleteSlideDataFile(ctx context.Context, id int64) error {
	if s.deleteSlideDataFn != nil {
		return s.deleteSlideDataFn(ctx, id)
	}
	return s.mockRepo.DeleteSlideDataFile(ctx, id)
}

func (s *snapshotRepoStub) CreateSlideFigure(ctx context.Context, input repository.CreateSlideFigureInput) (repository.SlideFigure, error) {
	if s.createSlideFigureFn != nil {
		return s.createSlideFigureFn(ctx, input)
	}
	return s.mockRepo.CreateSlideFigure(ctx, input)
}

func (s *snapshotRepoStub) CreateSlideDataFile(ctx context.Context, input repository.CreateSlideDataFileInput) (repository.SlideDataFile, error) {
	if s.createSlideDataFn != nil {
		return s.createSlideDataFn(ctx, input)
	}
	return s.mockRepo.CreateSlideDataFile(ctx, input)
}

type templateRepoStub struct {
	mockRepo
	updateTemplateFn func(context.Context, repository.UpdateTemplateInput) (repository.Template, error)
}

func (s *templateRepoStub) UpdateTemplate(ctx context.Context, input repository.UpdateTemplateInput) (repository.Template, error) {
	if s.updateTemplateFn != nil {
		return s.updateTemplateFn(ctx, input)
	}
	return s.mockRepo.UpdateTemplate(ctx, input)
}

func TestBuildSnapshotErrorPaths(t *testing.T) {
	ctx := context.Background()
	baseSlide := repository.Slide{ID: "20260309-deadbeef"}
	baseFigure := repository.SlideFigure{SlideID: baseSlide.ID, Filename: "plot.png"}

	tests := []struct {
		name          string
		templateRepo  repository.Repository
		slideRepo     repository.Repository
		readFigure    func(context.Context, repository.SlideFigure) ([]byte, error)
		wantSubstring string
	}{
		{
			name: "list templates",
			templateRepo: &snapshotRepoStub{
				listTemplatesFn: func(context.Context) ([]repository.Template, error) {
					return nil, errors.New("templates failed")
				},
			},
			slideRepo:     &snapshotRepoStub{},
			readFigure:    func(context.Context, repository.SlideFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list templates",
		},
		{
			name:         "list slides",
			templateRepo: &snapshotRepoStub{},
			slideRepo: &snapshotRepoStub{
				listSlidesFn: func(context.Context, repository.ListSlidesFilter) ([]repository.Slide, error) {
					return nil, errors.New("slides failed")
				},
			},
			readFigure:    func(context.Context, repository.SlideFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list slides",
		},
		{
			name:         "list figures",
			templateRepo: &snapshotRepoStub{},
			slideRepo: &snapshotRepoStub{
				listSlidesFn: func(context.Context, repository.ListSlidesFilter) ([]repository.Slide, error) {
					return []repository.Slide{baseSlide}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
					return nil, errors.New("figures failed")
				},
			},
			readFigure:    func(context.Context, repository.SlideFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list figures",
		},
		{
			name:         "list data files",
			templateRepo: &snapshotRepoStub{},
			slideRepo: &snapshotRepoStub{
				listSlidesFn: func(context.Context, repository.ListSlidesFilter) ([]repository.Slide, error) {
					return []repository.Slide{baseSlide}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
					return nil, nil
				},
				listDataFilesFn: func(context.Context, string) ([]repository.SlideDataFile, error) {
					return nil, errors.New("data files failed")
				},
			},
			readFigure:    func(context.Context, repository.SlideFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list data files",
		},
		{
			name:         "read figure",
			templateRepo: &snapshotRepoStub{},
			slideRepo: &snapshotRepoStub{
				listSlidesFn: func(context.Context, repository.ListSlidesFilter) ([]repository.Slide, error) {
					return []repository.Slide{baseSlide}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
					return []repository.SlideFigure{baseFigure}, nil
				},
				listDataFilesFn: func(context.Context, string) ([]repository.SlideDataFile, error) {
					return nil, nil
				},
			},
			readFigure: func(context.Context, repository.SlideFigure) ([]byte, error) {
				return nil, errors.New("figure download failed")
			},
			wantSubstring: "load figure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildSnapshot(ctx, tt.templateRepo, tt.slideRepo, tt.readFigure)
			if err == nil || !strings.Contains(err.Error(), tt.wantSubstring) {
				t.Fatalf("buildSnapshot() error = %v, want substring %q", err, tt.wantSubstring)
			}
		})
	}

	if _, err := buildCloudSnapshot(ctx, filepath.Join(t.TempDir(), "missing-home"), &cloudStack{Repo: &snapshotRepoStub{}}); err == nil {
		t.Fatal("expected buildCloudSnapshot to fail when local template home is missing")
	}
}

func TestImportSnapshotIntoStackErrorPaths(t *testing.T) {
	ctx := context.Background()
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack(): %v", err)
	}
	defer func() { _ = stack.Close() }()

	snapshot := gitsnapshot.Snapshot{
		Slides: []gitsnapshot.Slide{{
			ID:          "20260309-beadfeed",
			Date:        "2026-03-09",
			DayOrder:    "a",
			HTMLContent: "<html>snapshot</html>",
			Figures: []gitsnapshot.Figure{{
				Filename: "plot.png",
				S3Key:    "figures/20260309-beadfeed/plot.png",
				Content:  []byte("plot"),
			}},
			DataFiles: []gitsnapshot.DataFile{{
				Filename: "metrics.csv",
				S3Key:    "data/20260309-beadfeed/metrics.csv",
				Size:     7,
				Hash:     strings.Repeat("a", 64),
			}},
			CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		}},
	}

	tests := []struct {
		name          string
		repo          repository.Repository
		wantSubstring string
	}{
		{
			name: "get slide",
			repo: &snapshotRepoStub{
				getSlideByIDFn: func(context.Context, string) (repository.Slide, error) {
					return repository.Slide{}, errors.New("lookup failed")
				},
			},
			wantSubstring: "get slide",
		},
		{
			name: "create slide",
			repo: &snapshotRepoStub{
				getSlideByIDFn: func(context.Context, string) (repository.Slide, error) {
					return repository.Slide{}, repository.ErrNotFound
				},
				createSlideFn: func(context.Context, repository.CreateSlideInput) (repository.Slide, error) {
					return repository.Slide{}, errors.New("create failed")
				},
			},
			wantSubstring: "create slide",
		},
		{
			name: "update slide",
			repo: &snapshotRepoStub{
				getSlideByIDFn: func(context.Context, string) (repository.Slide, error) {
					return repository.Slide{ID: "20260309-beadfeed", UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC)}, nil
				},
				updateSlideFn: func(context.Context, repository.UpdateSlideInput) (repository.Slide, error) {
					return repository.Slide{}, errors.New("update failed")
				},
			},
			wantSubstring: "update slide",
		},
		{
			name: "delete figure",
			repo: &snapshotRepoStub{
				getSlideByIDFn: func(context.Context, string) (repository.Slide, error) {
					return repository.Slide{ID: "20260309-beadfeed", UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC)}, nil
				},
				updateSlideFn: func(context.Context, repository.UpdateSlideInput) (repository.Slide, error) {
					return repository.Slide{}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
					return []repository.SlideFigure{{ID: 1, SlideID: "20260309-beadfeed", Filename: "old.png"}}, nil
				},
				deleteSlideFigureFn: func(context.Context, int64) error {
					return errors.New("delete figure failed")
				},
			},
			wantSubstring: "delete existing figure",
		},
		{
			name: "delete data file",
			repo: &snapshotRepoStub{
				getSlideByIDFn: func(context.Context, string) (repository.Slide, error) {
					return repository.Slide{ID: "20260309-beadfeed", UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC)}, nil
				},
				updateSlideFn: func(context.Context, repository.UpdateSlideInput) (repository.Slide, error) {
					return repository.Slide{}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
					return nil, nil
				},
				listDataFilesFn: func(context.Context, string) ([]repository.SlideDataFile, error) {
					return []repository.SlideDataFile{{ID: 1, SlideID: "20260309-beadfeed", Filename: "old.csv"}}, nil
				},
				deleteSlideDataFn: func(context.Context, int64) error {
					return errors.New("delete data file failed")
				},
			},
			wantSubstring: "delete existing data file",
		},
		{
			name: "list existing data files",
			repo: &snapshotRepoStub{
				getSlideByIDFn: func(context.Context, string) (repository.Slide, error) {
					return repository.Slide{ID: "20260309-beadfeed", UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC)}, nil
				},
				updateSlideFn: func(context.Context, repository.UpdateSlideInput) (repository.Slide, error) {
					return repository.Slide{}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
					return nil, nil
				},
				listDataFilesFn: func(context.Context, string) ([]repository.SlideDataFile, error) {
					return nil, errors.New("list data files failed")
				},
			},
			wantSubstring: "list existing data files",
		},
		{
			name: "create figure row",
			repo: &snapshotRepoStub{
				getSlideByIDFn: func(context.Context, string) (repository.Slide, error) {
					return repository.Slide{}, repository.ErrNotFound
				},
				createSlideFn: func(context.Context, repository.CreateSlideInput) (repository.Slide, error) {
					return repository.Slide{}, nil
				},
				createSlideFigureFn: func(context.Context, repository.CreateSlideFigureInput) (repository.SlideFigure, error) {
					return repository.SlideFigure{}, errors.New("create figure row failed")
				},
			},
			wantSubstring: "create figure row",
		},
		{
			name: "create data file row",
			repo: &snapshotRepoStub{
				getSlideByIDFn: func(context.Context, string) (repository.Slide, error) {
					return repository.Slide{}, repository.ErrNotFound
				},
				createSlideFn: func(context.Context, repository.CreateSlideInput) (repository.Slide, error) {
					return repository.Slide{}, nil
				},
				createSlideDataFn: func(context.Context, repository.CreateSlideDataFileInput) (repository.SlideDataFile, error) {
					return repository.SlideDataFile{}, errors.New("create data file row failed")
				},
			},
			wantSubstring: "create data file row",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origRepo := stack.Repo
			stack.Repo = tt.repo
			defer func() { stack.Repo = origRepo }()

			_, err := importSnapshotIntoStack(ctx, stack, snapshot)
			if err == nil || !strings.Contains(err.Error(), tt.wantSubstring) {
				t.Fatalf("importSnapshotIntoStack() error = %v, want substring %q", err, tt.wantSubstring)
			}
		})
	}
}

func TestPhase7CommandAdditionalErrorPaths(t *testing.T) {
	ctx := context.Background()
	snapshotDir := t.TempDir()
	if err := gitsnapshot.Write(snapshotDir, gitsnapshot.Snapshot{}); err != nil {
		t.Fatalf("gitsnapshot.Write(snapshotDir): %v", err)
	}

	t.Run("resolveHomeDir failures", func(t *testing.T) {
		origResolveHomeDirFn := resolveHomeDirFn
		resolveHomeDirFn = func() (string, error) {
			return "", errors.New("resolve failed")
		}
		t.Cleanup(func() {
			resolveHomeDirFn = origResolveHomeDirFn
		})

		if err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{Path: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("runExport() error = %v, want resolve failure", err)
		}
		if err := runImport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir); err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("runImport() error = %v, want resolve failure", err)
		}
		if err := runRestoreDB(ctx, &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir); err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("runRestoreDB() error = %v, want resolve failure", err)
		}
		if err := runVerify(ctx, &bytes.Buffer{}, &bytes.Buffer{}, false); err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("runVerify() error = %v, want resolve failure", err)
		}
	})

	t.Run("cloud open failures are wrapped", func(t *testing.T) {
		origResolveHomeDirFn := resolveHomeDirFn
		origOpenCloudStackFn := openCloudStackFn
		resolveHomeDirFn = func() (string, error) {
			return t.TempDir(), nil
		}
		openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
			return nil, errors.New("cloud dial failed")
		}
		t.Cleanup(func() {
			resolveHomeDirFn = origResolveHomeDirFn
			openCloudStackFn = origOpenCloudStackFn
		})

		if err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{Path: t.TempDir(), FromCloud: true}); err == nil || !strings.Contains(err.Error(), "open cloud") {
			t.Fatalf("runExport(fromCloud) error = %v, want wrapped cloud failure", err)
		}
		if err := runVerify(ctx, &bytes.Buffer{}, &bytes.Buffer{}, true); err == nil || !strings.Contains(err.Error(), "open cloud") {
			t.Fatalf("runVerify(fromCloud) error = %v, want wrapped cloud failure", err)
		}
	})

	t.Run("local stack open failures are surfaced", func(t *testing.T) {
		origResolveHomeDirFn := resolveHomeDirFn
		resolveHomeDirFn = func() (string, error) {
			return t.TempDir(), nil
		}
		t.Cleanup(func() {
			resolveHomeDirFn = origResolveHomeDirFn
		})

		if err := runImport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir); err == nil || !strings.Contains(err.Error(), "read config") {
			t.Fatalf("runImport() error = %v, want local stack failure", err)
		}
		if err := runVerify(ctx, &bytes.Buffer{}, &bytes.Buffer{}, false); err == nil || !strings.Contains(err.Error(), "read config") {
			t.Fatalf("runVerify() error = %v, want local stack failure", err)
		}
	})

	t.Run("export write failures are wrapped", func(t *testing.T) {
		homeDir := setupEnv(t)
		t.Setenv(pcHomeEnvVar, homeDir)
		withResolvedHomeDir(t, homeDir)

		blockedPath := filepath.Join(t.TempDir(), "snapshot-root")
		if err := os.WriteFile(blockedPath, []byte("not-a-directory"), 0o644); err != nil {
			t.Fatalf("write blocked path: %v", err)
		}

		err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{Path: blockedPath})
		if err == nil || !strings.Contains(err.Error(), "write export snapshot") {
			t.Fatalf("runExport() error = %v, want wrapped write failure", err)
		}
	})

	t.Run("restore backup write failures are wrapped", func(t *testing.T) {
		homeDir := setupEnv(t)
		t.Setenv(pcHomeEnvVar, homeDir)
		withResolvedHomeDir(t, homeDir)

		if err := os.WriteFile(filepath.Join(basePath(homeDir), ".pc", "backups"), []byte("blocked"), 0o644); err != nil {
			t.Fatalf("write backup blocker: %v", err)
		}

		err := runRestoreDB(ctx, &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir)
		if err == nil || !strings.Contains(err.Error(), "write restore backup") {
			t.Fatalf("runRestoreDB() error = %v, want wrapped backup write failure", err)
		}
	})

	t.Run("cloud figure download failures bubble through commands", func(t *testing.T) {
		homeDir := setupEnv(t)
		t.Setenv(pcHomeEnvVar, homeDir)
		withResolvedHomeDir(t, homeDir)

		slide := repository.Slide{
			ID:          "20260309-clouddead",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: `<html><body><img src="figures/cloud.png"></body></html>`,
			CreatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		}
		figure := repository.SlideFigure{
			SlideID:  slide.ID,
			Filename: "cloud.png",
			S3Key:    "figures/20260309-clouddead/cloud.png",
		}
		repo := &snapshotRepoStub{
			listSlidesFn: func(context.Context, repository.ListSlidesFilter) ([]repository.Slide, error) {
				return []repository.Slide{slide}, nil
			},
			listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
				return []repository.SlideFigure{figure}, nil
			},
			listDataFilesFn: func(context.Context, string) ([]repository.SlideDataFile, error) {
				return nil, nil
			},
		}

		origOpenCloudStackFn := openCloudStackFn
		origDownloadCloudFigureFn := downloadCloudFigureFn
		openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
			return &cloudStack{Repo: repo}, nil
		}
		downloadCloudFigureFn = func(context.Context, *cloudStack, string) (io.ReadCloser, error) {
			return nil, errors.New("download failed")
		}
		t.Cleanup(func() {
			openCloudStackFn = origOpenCloudStackFn
			downloadCloudFigureFn = origDownloadCloudFigureFn
		})

		if err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{Path: t.TempDir(), FromCloud: true}); err == nil || !strings.Contains(err.Error(), "load figure") {
			t.Fatalf("runExport(fromCloud) error = %v, want figure download failure", err)
		}
		if err := runVerify(ctx, &bytes.Buffer{}, &bytes.Buffer{}, true); err == nil || !strings.Contains(err.Error(), "load figure") {
			t.Fatalf("runVerify(fromCloud) error = %v, want figure download failure", err)
		}
	})
}

func TestSnapshotSupportAdditionalHelperPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("buildLocalSnapshot missing figure content", func(t *testing.T) {
		homeDir := setupEnv(t)
		slideID := addSlideWithContent(
			t,
			`<html><body><img src="figures/missing.png">broken</body></html>`,
			"",
			"",
			map[string][]byte{"missing.png": []byte("figure")},
			nil,
		)
		if err := os.Remove(filepath.Join(basePath(homeDir), "figures", slideID, "missing.png")); err != nil {
			t.Fatalf("remove local figure: %v", err)
		}

		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		defer func() { _ = stack.Close() }()

		if _, err := buildLocalSnapshot(ctx, stack); err == nil || !strings.Contains(err.Error(), "read local figure") {
			t.Fatalf("buildLocalSnapshot() error = %v, want local figure read failure", err)
		}
	})

	t.Run("buildCloudSnapshot downloads figure content", func(t *testing.T) {
		homeDir := setupEnv(t)
		slide := repository.Slide{
			ID:          "20260309-cloudbeef",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: `<html><body><img src="figures/cloud.png"></body></html>`,
			CreatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		}
		figure := repository.SlideFigure{
			SlideID:  slide.ID,
			Filename: "cloud.png",
			S3Key:    "figures/20260309-cloudbeef/cloud.png",
		}

		origDownloadCloudFigureFn := downloadCloudFigureFn
		downloadCloudFigureFn = func(context.Context, *cloudStack, string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("cloud-bytes")), nil
		}
		t.Cleanup(func() { downloadCloudFigureFn = origDownloadCloudFigureFn })

		snapshot, err := buildCloudSnapshot(ctx, homeDir, &cloudStack{
			Repo: &snapshotRepoStub{
				listSlidesFn: func(context.Context, repository.ListSlidesFilter) ([]repository.Slide, error) {
					return []repository.Slide{slide}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
					return []repository.SlideFigure{figure}, nil
				},
				listDataFilesFn: func(context.Context, string) ([]repository.SlideDataFile, error) {
					return nil, nil
				},
			},
		})
		if err != nil {
			t.Fatalf("buildCloudSnapshot(): %v", err)
		}
		if got := string(snapshot.Slides[0].Figures[0].Content); got != "cloud-bytes" {
			t.Fatalf("cloud figure content = %q, want %q", got, "cloud-bytes")
		}
	})

	t.Run("upsertTemplate variants", func(t *testing.T) {
		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{}, errors.New("lookup failed")
				},
			},
		}, gitsnapshot.Template{Name: "x", HTMLContent: "<html>x</html>"}); err == nil || !strings.Contains(err.Error(), "get template") {
			t.Fatalf("upsertTemplate(get error) = %v, want get template failure", err)
		}

		created := false
		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{}, repository.ErrNotFound
				},
				createTemplateFn: func(context.Context, repository.CreateTemplateInput) (repository.Template, error) {
					created = true
					return repository.Template{Name: "new-template"}, nil
				},
			},
		}, gitsnapshot.Template{Name: "new-template", HTMLContent: "<html>new</html>"}); err != nil {
			t.Fatalf("upsertTemplate(create): %v", err)
		}
		if !created {
			t.Fatal("expected create template path to run")
		}

		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{}, repository.ErrNotFound
				},
				createTemplateFn: func(context.Context, repository.CreateTemplateInput) (repository.Template, error) {
					return repository.Template{}, errors.New("create failed")
				},
			},
		}, gitsnapshot.Template{Name: "create-fail", HTMLContent: "<html>new</html>"}); err == nil || !strings.Contains(err.Error(), "create template") {
			t.Fatalf("upsertTemplate(create error) = %v, want create failure", err)
		}

		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{Name: "same", HTMLContent: "<html>same</html>"}, nil
				},
			},
			updateTemplateFn: func(context.Context, repository.UpdateTemplateInput) (repository.Template, error) {
				t.Fatal("UpdateTemplate should not run for identical HTML")
				return repository.Template{}, nil
			},
		}, gitsnapshot.Template{Name: "same", HTMLContent: "<html>same</html>"}); err != nil {
			t.Fatalf("upsertTemplate(no-op): %v", err)
		}

		updated := false
		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{Name: "update", HTMLContent: "<html>old</html>"}, nil
				},
			},
			updateTemplateFn: func(_ context.Context, input repository.UpdateTemplateInput) (repository.Template, error) {
				updated = input.HTMLContent == "<html>new</html>"
				return repository.Template{Name: input.Name, HTMLContent: input.HTMLContent}, nil
			},
		}, gitsnapshot.Template{Name: "update", HTMLContent: "<html>new</html>"}); err != nil {
			t.Fatalf("upsertTemplate(update): %v", err)
		}
		if !updated {
			t.Fatal("expected update template path to run")
		}

		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{Name: "update-fail", HTMLContent: "<html>old</html>"}, nil
				},
			},
			updateTemplateFn: func(context.Context, repository.UpdateTemplateInput) (repository.Template, error) {
				return repository.Template{}, errors.New("update failed")
			},
		}, gitsnapshot.Template{Name: "update-fail", HTMLContent: "<html>new</html>"}); err == nil || !strings.Contains(err.Error(), "update template") {
			t.Fatalf("upsertTemplate(update error) = %v, want update failure", err)
		}
	})

	t.Run("ensureLocalEnvironment error paths", func(t *testing.T) {
		homeDir := t.TempDir()

		t.Run("config store", func(t *testing.T) {
			origNewConfigStoreFn := newConfigStoreFn
			newConfigStoreFn = func(string) (config.Store, error) {
				return config.Store{}, errors.New("store failed")
			}
			t.Cleanup(func() { newConfigStoreFn = origNewConfigStoreFn })

			if err := ensureLocalEnvironment(ctx, homeDir); err == nil || !strings.Contains(err.Error(), "create config store") {
				t.Fatalf("ensureLocalEnvironment() error = %v, want config store failure", err)
			}
		})

		t.Run("open sqlite", func(t *testing.T) {
			origOpenSQLiteFn := openSQLiteFn
			openSQLiteFn = func(string) (*sqlite.Connection, error) {
				return nil, errors.New("sqlite open failed")
			}
			t.Cleanup(func() { openSQLiteFn = origOpenSQLiteFn })

			if err := ensureLocalEnvironment(ctx, homeDir); err == nil || !strings.Contains(err.Error(), "open database") {
				t.Fatalf("ensureLocalEnvironment() error = %v, want sqlite open failure", err)
			}
		})

		t.Run("load migrations", func(t *testing.T) {
			origSQLiteMigrationsFSFn := sqliteMigrationsFSFn
			sqliteMigrationsFSFn = func() (fs.FS, error) {
				return nil, errors.New("migrations failed")
			}
			t.Cleanup(func() { sqliteMigrationsFSFn = origSQLiteMigrationsFSFn })

			if err := ensureLocalEnvironment(ctx, homeDir); err == nil || !strings.Contains(err.Error(), "load migrations") {
				t.Fatalf("ensureLocalEnvironment() error = %v, want migrations failure", err)
			}
		})

		t.Run("repo factory", func(t *testing.T) {
			origNewSQLiteRepoFn := newSQLiteRepoFn
			newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
				return nil, errors.New("repo failed")
			}
			t.Cleanup(func() { newSQLiteRepoFn = origNewSQLiteRepoFn })

			if err := ensureLocalEnvironment(ctx, homeDir); err == nil || !strings.Contains(err.Error(), "create repository") {
				t.Fatalf("ensureLocalEnvironment() error = %v, want repo failure", err)
			}
		})

		t.Run("seed templates", func(t *testing.T) {
			origNewSQLiteRepoFn := newSQLiteRepoFn
			newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
				return &templateRepoStub{
					mockRepo: mockRepo{
						getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
							return repository.Template{}, errors.New("template lookup failed")
						},
					},
				}, nil
			}
			t.Cleanup(func() { newSQLiteRepoFn = origNewSQLiteRepoFn })

			if err := ensureLocalEnvironment(ctx, homeDir); err == nil || !strings.Contains(err.Error(), "seed templates") {
				t.Fatalf("ensureLocalEnvironment() error = %v, want seed template failure", err)
			}
		})
	})

	t.Run("wipeLocalState and compareSnapshotDirs manifest failures", func(t *testing.T) {
		homeDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(basePath(homeDir), ".pc", "last_sync"), 0o755); err != nil {
			t.Fatalf("mkdir last_sync dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(basePath(homeDir), ".pc", "last_sync", "marker"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write last_sync marker: %v", err)
		}
		if err := wipeLocalState(homeDir); err == nil || !strings.Contains(err.Error(), "remove last_sync") {
			t.Fatalf("wipeLocalState() error = %v, want last_sync removal failure", err)
		}

		if err := compareSnapshotDirs(filepath.Join(t.TempDir(), "missing"), t.TempDir()); err == nil {
			t.Fatal("expected compareSnapshotDirs to surface manifest errors")
		}
	})

	t.Run("wipeLocalState surfaces database artifact removal failures", func(t *testing.T) {
		homeDir := t.TempDir()
		if err := os.MkdirAll(dbPath(homeDir), 0o755); err != nil {
			t.Fatalf("mkdir db dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dbPath(homeDir), "marker"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write db marker: %v", err)
		}

		if err := wipeLocalState(homeDir); err == nil || !strings.Contains(err.Error(), "remove database artifact") {
			t.Fatalf("wipeLocalState() error = %v, want database artifact failure", err)
		}
	})
}
