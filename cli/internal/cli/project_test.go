package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSetSuccess(t *testing.T) {
	homeDir := setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "set", "my-project"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}
	if !strings.Contains(stdout.String(), `"my-project"`) {
		t.Fatalf("expected project name in output, got %q", stdout.String())
	}

	// Verify config was updated
	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg["active_project"] != "my-project" {
		t.Fatalf("expected active_project=my-project in config, got %v", cfg["active_project"])
	}
}

func TestProjectSetOverwrites(t *testing.T) {
	homeDir := setupEnv(t)

	cmd1 := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd1.SetArgs([]string{"project", "set", "first"})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("project set first: %v", err)
	}

	cmd2 := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd2.SetArgs([]string{"project", "set", "second"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("project set second: %v", err)
	}

	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg["active_project"] != "second" {
		t.Fatalf("expected active_project=second, got %v", cfg["active_project"])
	}
}

func TestProjectSetEmptyName(t *testing.T) {
	setupEnv(t)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "set", ""})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for empty project name")
	}
}

func TestProjectSetWhitespaceName(t *testing.T) {
	setupEnv(t)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "set", "   "})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for whitespace-only project name")
	}
}

func TestProjectClearSuccess(t *testing.T) {
	setupEnv(t)

	// Set first
	cmd1 := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd1.SetArgs([]string{"project", "set", "to-clear"})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}

	// Clear
	stdout := &bytes.Buffer{}
	cmd2 := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd2.SetArgs([]string{"project", "clear"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("project clear: %v", err)
	}
	if !strings.Contains(stdout.String(), "Active project cleared.") {
		t.Fatalf("expected clear message, got %q", stdout.String())
	}
}

func TestProjectClearIdempotent(t *testing.T) {
	setupEnv(t)

	// Clear without setting first
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "clear"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project clear: %v", err)
	}
	if !strings.Contains(stdout.String(), "Active project cleared.") {
		t.Fatalf("expected clear message, got %q", stdout.String())
	}
}

func TestProjectListEmpty(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project list: %v", err)
	}
	if !strings.Contains(stdout.String(), "No projects found.") {
		t.Fatalf("expected 'No projects found.', got %q", stdout.String())
	}
}

func TestProjectListWithProjects(t *testing.T) {
	setupEnv(t)

	// Add slides with projects
	addSlideWithContent(t, "<html>A</html>", "", `{"project_id":"alpha"}`, nil, nil)
	addSlideWithContent(t, "<html>B</html>", "", `{"project_id":"beta"}`, nil, nil)
	addSlideWithContent(t, "<html>C</html>", "", `{"project_id":"alpha"}`, nil, nil) // duplicate

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project list: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "alpha") {
		t.Fatalf("expected alpha in output, got %q", out)
	}
	if !strings.Contains(out, "beta") {
		t.Fatalf("expected beta in output, got %q", out)
	}
}

func TestProjectListMarksActive(t *testing.T) {
	setupEnv(t)

	addSlideWithContent(t, "<html>A</html>", "", `{"project_id":"alpha"}`, nil, nil)
	addSlideWithContent(t, "<html>B</html>", "", `{"project_id":"beta"}`, nil, nil)

	// Set active project
	setCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	setCmd.SetArgs([]string{"project", "set", "beta"})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project list: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "beta (active)") {
		t.Fatalf("expected 'beta (active)' in output, got %q", out)
	}
	// alpha should not have (active)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "alpha") && strings.Contains(line, "(active)") {
			t.Fatalf("alpha should not be marked active: %q", line)
		}
	}
}

func TestProjectCommandShowsHelp(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project help: %v", err)
	}
	if !strings.Contains(stdout.String(), "Manage active project") {
		t.Fatalf("expected help text, got %q", stdout.String())
	}
}

func TestAddUsesActiveProject(t *testing.T) {
	homeDir := setupEnv(t)

	// Set active project
	setCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	setCmd.SetArgs([]string{"project", "set", "active-proj"})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}

	// Add slide without --project flag
	id := addSlide(t)

	// Verify project from DB
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open stack: %v", err)
	}
	defer func() { _ = stack.Close() }()

	slide, err := stack.Repo.GetSlideByID(t.Context(), id)
	if err != nil {
		t.Fatalf("get slide: %v", err)
	}
	if slide.ProjectID == nil {
		t.Fatal("expected project_id to be set from active project")
	}
	if *slide.ProjectID != "active-proj" {
		t.Fatalf("expected project_id=active-proj, got %q", *slide.ProjectID)
	}
}

func TestAddProjectFlagOverridesActiveProject(t *testing.T) {
	homeDir := setupEnv(t)

	// Set active project
	setCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	setCmd.SetArgs([]string{"project", "set", "active-proj"})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}

	// Add slide with --project flag
	id := addSlide(t, "--project", "flag-proj")

	// Verify flag wins
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open stack: %v", err)
	}
	defer func() { _ = stack.Close() }()

	slide, err := stack.Repo.GetSlideByID(t.Context(), id)
	if err != nil {
		t.Fatalf("get slide: %v", err)
	}
	if slide.ProjectID == nil || *slide.ProjectID != "flag-proj" {
		t.Fatalf("expected project_id=flag-proj, got %v", slide.ProjectID)
	}
}

func TestAddMetadataOverridesActiveProject(t *testing.T) {
	homeDir := setupEnv(t)

	// Set active project
	setCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	setCmd.SetArgs([]string{"project", "set", "active-proj"})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}

	// Add slide with metadata.json project_id (no --project flag)
	id := addSlideWithContent(t, "<html>X</html>", "", `{"project_id":"metadata-proj"}`, nil, nil)

	// Verify metadata wins over active project
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open stack: %v", err)
	}
	defer func() { _ = stack.Close() }()

	slide, err := stack.Repo.GetSlideByID(t.Context(), id)
	if err != nil {
		t.Fatalf("get slide: %v", err)
	}
	if slide.ProjectID == nil || *slide.ProjectID != "metadata-proj" {
		t.Fatalf("expected project_id=metadata-proj, got %v", slide.ProjectID)
	}
}
