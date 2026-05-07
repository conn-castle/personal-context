package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestNewSeedCommand(t *testing.T) {
	cmd := newSeedCommand(os.Stdout, os.Stderr)
	if cmd.Use != "seed" {
		t.Errorf("unexpected Use: %s", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected non-empty Short description")
	}
	if cmd.Args == nil {
		t.Error("expected Args to be set")
	}
}

func TestSeedCreatesSlides(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &stdout, Stderr: &stderr})
	cmd.SetArgs([]string{"seed"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("seed: %v (stderr: %s)", err, stderr.String())
	}

	output := stdout.String()

	// Should have created 6 slides.
	if !strings.Contains(output, "Created 6 tutorial slides") {
		t.Errorf("expected 'Created 6 tutorial slides' in output, got: %s", output)
	}

	// Should mention the project.
	if !strings.Contains(output, "personal-context/tutorial") {
		t.Errorf("expected project name in output, got: %s", output)
	}

	// Should list each slide title.
	for _, title := range []string{
		"Welcome to Personal Context",
		"Adding Slides",
		"Managing Slides",
		"Projects",
		"Web UI",
		"Cloud Sync & Backup",
	} {
		if !strings.Contains(output, title) {
			t.Errorf("expected title %q in output, got: %s", title, output)
		}
	}

	// Verify slides are in the database by listing them.
	stdout.Reset()
	stderr.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: &stdout, Stderr: &stderr})
	cmd.SetArgs([]string{"search", "Personal Context", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search: %v", err)
	}

	// All seed slides have the project set.
	if !strings.Contains(stdout.String(), "personal-context/tutorial") {
		t.Errorf("expected search results to contain project, got: %s", stdout.String())
	}
}

func TestSeedIdempotent(t *testing.T) {
	setupEnv(t)

	// First seed.
	var stdout1, stderr1 bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &stdout1, Stderr: &stderr1})
	cmd.SetArgs([]string{"seed"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if !strings.Contains(stdout1.String(), "Created 6 tutorial slides") {
		t.Fatalf("first seed should create slides, got: %s", stdout1.String())
	}

	// Second seed — should skip.
	var stdout2, stderr2 bytes.Buffer
	cmd = NewRootCommand(RootCommandOptions{Stdout: &stdout2, Stderr: &stderr2})
	cmd.SetArgs([]string{"seed"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if !strings.Contains(stdout2.String(), "already exist") {
		t.Errorf("expected 'already exist' message on second seed, got: %s", stdout2.String())
	}

}

func TestSeedRepairsPartialSeed(t *testing.T) {
	homeDir := setupEnv(t)

	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &stdout, Stderr: &stderr})
	cmd.SetArgs([]string{"seed"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first seed: %v", err)
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open local stack: %v", err)
	}
	ctx := context.Background()
	slides, err := stack.Repo.ListSlides(ctx, repository.ListSlidesFilter{
		ProjectID: strPtr("personal-context/tutorial"),
	})
	if err != nil {
		_ = stack.Close()
		t.Fatalf("list seeded slides: %v", err)
	}
	if len(slides) != 6 {
		_ = stack.Close()
		t.Fatalf("expected 6 tutorial slides after first seed, got %d", len(slides))
	}
	if err := stack.Repo.DeleteSlide(ctx, slides[0].ID); err != nil {
		_ = stack.Close()
		t.Fatalf("delete one tutorial slide: %v", err)
	}
	if err := stack.Close(); err != nil {
		t.Fatalf("close local stack: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: &stdout, Stderr: &stderr})
	cmd.SetArgs([]string{"seed"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("repair seed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Created 1 tutorial slides") {
		t.Fatalf("expected one missing tutorial slide to be recreated, got: %s", stdout.String())
	}

	stack, err = openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("re-open local stack: %v", err)
	}
	defer func() { _ = stack.Close() }()

	slides, err = stack.Repo.ListSlides(ctx, repository.ListSlidesFilter{
		ProjectID: strPtr("personal-context/tutorial"),
	})
	if err != nil {
		t.Fatalf("list repaired tutorial slides: %v", err)
	}
	if len(slides) != 6 {
		t.Fatalf("expected repaired tutorial deck to contain 6 slides, got %d", len(slides))
	}
}

func TestSeedDBCorrupted(t *testing.T) {
	// Covers the ListSlides error path in runSeed.
	homeDir := setupEnv(t)
	corruptTable(t, homeDir, "slides")

	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &stdout, Stderr: &stderr})
	cmd.SetArgs([]string{"seed"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when slides table is corrupted")
	}
}

func TestSeedOpenStackError(t *testing.T) {
	// PC_HOME points to a dir that exists but has no DB setup.
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)
	pcDir := filepath.Join(homeDir, "personal-context", ".pc")
	if err := os.MkdirAll(pcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &stdout, Stderr: &stderr})
	cmd.SetArgs([]string{"seed"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when stack can't open")
	}
}

func TestSeedHomeDirError(t *testing.T) {
	withBrokenHomeDir(t, func() {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"seed"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error for broken home dir")
		}
	})
}

func TestSeedCreateSlideError(t *testing.T) {
	// Covers the CreateSlide error path in runSeed by dropping sync_version
	// so the INSERT trigger fails.
	homeDir := setupEnv(t)
	corruptTable(t, homeDir, "sync_version")

	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &stdout, Stderr: &stderr})
	cmd.SetArgs([]string{"seed"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when sync_version table is missing (trigger fails)")
	}
}

func TestSeedProjectLookupError(t *testing.T) {
	homeDir := setupEnv(t)
	corruptTable(t, homeDir, "projects")

	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &stdout, Stderr: &stderr})
	cmd.SetArgs([]string{"seed"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when projects table is corrupted")
	}
	if !strings.Contains(err.Error(), "get seed project") {
		t.Fatalf("seed error = %v, want seed project lookup context", err)
	}
}

func TestSeedDeviceLookupError(t *testing.T) {
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open stack: %v", err)
	}
	if _, err := stack.Repo.CreateProject(context.Background(), repository.CreateRegistryInput{ID: "personal-context/tutorial"}); err != nil {
		t.Fatalf("create tutorial project: %v", err)
	}
	if err := stack.Close(); err != nil {
		t.Fatalf("close stack: %v", err)
	}
	corruptTable(t, homeDir, "devices")

	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &stdout, Stderr: &stderr})
	cmd.SetArgs([]string{"seed"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected error when devices table is corrupted")
	}
	if !strings.Contains(err.Error(), "get seed device") {
		t.Fatalf("seed error = %v, want seed device lookup context", err)
	}
}

func TestSeedRegistryCreateErrors(t *testing.T) {
	setupEnv(t)

	t.Run("project", func(t *testing.T) {
		createErr := errors.New("create project failed")
		origNewSQLiteRepoFn := newSQLiteRepoFn
		newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
			return &mockRepo{
				createProjectFn: func(context.Context, repository.CreateRegistryInput) (repository.Project, error) {
					return repository.Project{}, createErr
				},
			}, nil
		}
		t.Cleanup(func() { newSQLiteRepoFn = origNewSQLiteRepoFn })

		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"seed"})
		err := cmd.Execute()
		if !errors.Is(err, createErr) || !strings.Contains(err.Error(), "create seed project") {
			t.Fatalf("seed error = %v, want create seed project", err)
		}
	})

	t.Run("device", func(t *testing.T) {
		createErr := errors.New("create device failed")
		origNewSQLiteRepoFn := newSQLiteRepoFn
		newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
			return &mockRepo{
				getProjectByIDFn: func(context.Context, string) (repository.Project, error) {
					return repository.Project{ID: "personal-context/tutorial"}, nil
				},
				createDeviceFn: func(context.Context, repository.CreateRegistryInput) (repository.Device, error) {
					return repository.Device{}, createErr
				},
			}, nil
		}
		t.Cleanup(func() { newSQLiteRepoFn = origNewSQLiteRepoFn })

		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"seed"})
		err := cmd.Execute()
		if !errors.Is(err, createErr) || !strings.Contains(err.Error(), "create seed device") {
			t.Fatalf("seed error = %v, want create seed device", err)
		}
	})
}

func TestSeedTitle(t *testing.T) {
	tests := []struct {
		index    int
		expected string
	}{
		{0, "Welcome to Personal Context"},
		{1, "Adding Slides"},
		{2, "Managing Slides"},
		{3, "Projects"},
		{4, "Web UI"},
		{5, "Cloud Sync & Backup"},
		{6, "Slide 7"},
		{99, "Slide 100"},
	}
	for _, tt := range tests {
		got := seedTitle(tt.index)
		if got != tt.expected {
			t.Errorf("seedTitle(%d) = %q, want %q", tt.index, got, tt.expected)
		}
	}
}

func TestBuiltinSeeds(t *testing.T) {
	seeds := builtinSeeds()

	if len(seeds) != 6 {
		t.Fatalf("expected 6 seeds, got %d", len(seeds))
	}

	for i, seed := range seeds {
		if seed.ProjectID != "personal-context/tutorial" {
			t.Errorf("seed %d: expected project 'personal-context/tutorial', got %q", i, seed.ProjectID)
		}
		if seed.HTMLContent == "" {
			t.Errorf("seed %d: empty HTMLContent", i)
		}
		if seed.Notes == "" {
			t.Errorf("seed %d: empty Notes", i)
		}
		// All seed slides should be full HTML documents (for 1920x1080 rendering).
		lower := strings.ToLower(seed.HTMLContent)
		if !strings.Contains(lower, "<!doctype html>") && !strings.Contains(lower, "<html") {
			t.Errorf("seed %d: HTMLContent should be a full document", i)
		}
		// All seed slides should specify 1920x1080 dimensions.
		if !strings.Contains(seed.HTMLContent, "1920px") || !strings.Contains(seed.HTMLContent, "1080px") {
			t.Errorf("seed %d: HTMLContent should contain 1920x1080 dimensions", i)
		}
	}
}
