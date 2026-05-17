package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestProjectRegistryCommands(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "add", "alpha"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project add: %v", err)
	}
	if !strings.Contains(stdout.String(), "alpha") {
		t.Fatalf("expected alpha in add output, got %q", stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project list: %v", err)
	}
	if !strings.Contains(stdout.String(), "alpha") {
		t.Fatalf("expected alpha in list output, got %q", stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "archive", "alpha"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project archive: %v", err)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project list active: %v", err)
	}
	if strings.Contains(stdout.String(), "alpha") {
		t.Fatalf("expected archived project excluded, got %q", stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "restore", "alpha"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project restore: %v", err)
	}
}

func TestProjectAddPathRegistersAndBackfillsChat(t *testing.T) {
	projectPath := t.TempDir()
	normalizedProjectPath, err := normalizeProjectPath(projectPath)
	if err != nil {
		t.Fatalf("normalize project path: %v", err)
	}
	setupEnv(t)
	root := t.TempDir()
	transcript := `{
  "id": "project-path-session",
  "cwd": "` + filepath.ToSlash(filepath.Join(normalizedProjectPath, "nested", "child")) + `",
  "title": "Path chat",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [{"role": "user", "content": "path backfill needle"}]
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
	cmd.SetArgs([]string{"project", "add", "path/project", projectPath, "--device", "test-device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project add path: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"path/project registered", "path registered", "Backfilled 1 chat session"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in project add output, got %q", want, out)
		}
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "add", "path/project", projectPath, "--device", "test-device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project add path idempotent: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "already registered") || !strings.Contains(out, "path already registered") {
		t.Fatalf("expected idempotent output, got %q", out)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "list", "--format", "json", "--project", "path/project"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat list by project: %v", err)
	}
	if !strings.Contains(stdout.String(), `"project_id": "path/project"`) {
		t.Fatalf("expected backfilled project in chat list, got %q", stdout.String())
	}
}

func TestProjectAddPathValidationBranches(t *testing.T) {
	setupEnv(t)
	projectPath := t.TempDir()
	cases := [][]string{
		{"project", "add", "missing-device", projectPath},
		{"project", "add", "unknown-device", projectPath, "--device", "does-not-exist"},
		{"project", "add", "bad-path", filepath.Join(projectPath, "missing"), "--device", "test-device"},
	}
	for _, args := range cases {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected %v to fail", args)
		}
	}
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project list after failed path registration: %v", err)
	}
	for _, unexpected := range []string{"missing-device", "unknown-device", "bad-path"} {
		if strings.Contains(stdout.String(), unexpected) {
			t.Fatalf("failed path registration created project %q: %q", unexpected, stdout.String())
		}
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"device", "archive", "test-device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("archive device: %v", err)
	}
	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "add", "archived-device", projectPath, "--device", "test-device"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected archived device path registration to fail")
	}
}

func TestProjectPathAndDeviceValidationHelpers(t *testing.T) {
	if _, err := normalizeProjectPath("   "); err == nil {
		t.Fatal("expected empty project path to fail")
	}
	if err := validateActiveDevice(context.Background(), &mockRepo{
		getDeviceByIDFn: func(context.Context, string) (repository.Device, error) {
			return repository.Device{}, errors.New("device lookup failed")
		},
	}, "device-x"); err == nil || !strings.Contains(err.Error(), "get device") {
		t.Fatalf("expected device lookup error, got %v", err)
	}
	archived := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	if err := validateActiveDevice(context.Background(), &mockRepo{
		getDeviceByIDFn: func(context.Context, string) (repository.Device, error) {
			return repository.Device{ID: "device-x", ArchivedAt: &archived}, nil
		},
	}, "device-x"); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("expected archived device error, got %v", err)
	}
}

func TestDeviceRegistryCommands(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"device", "register", "laptop"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("device register: %v", err)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"device", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("device list: %v", err)
	}
	if !strings.Contains(stdout.String(), "laptop") {
		t.Fatalf("expected laptop in list output, got %q", stdout.String())
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"device", "archive", "laptop"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("device archive: %v", err)
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"device", "restore", "laptop"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("device restore: %v", err)
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
	if !strings.Contains(stdout.String(), "Manage project registry") {
		t.Fatalf("expected help text, got %q", stdout.String())
	}
}

func TestRegistryCommandsEmptyListsAndDeviceHelp(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	stdout := &bytes.Buffer{}
	if err := runProjectList(context.Background(), stdout, &bytes.Buffer{}, false); err != nil {
		t.Fatalf("runProjectList(empty): %v", err)
	}
	if !strings.Contains(stdout.String(), "No projects registered.") {
		t.Fatalf("expected empty project list message, got %q", stdout.String())
	}
	stdout.Reset()
	if err := runDeviceList(context.Background(), stdout, &bytes.Buffer{}, false); err != nil {
		t.Fatalf("runDeviceList(empty): %v", err)
	}
	if !strings.Contains(stdout.String(), "No devices registered.") {
		t.Fatalf("expected empty device list message, got %q", stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("device help: %v", err)
	}
	if !strings.Contains(stdout.String(), "Manage source device registry") {
		t.Fatalf("expected device help text, got %q", stdout.String())
	}
}

func TestRegistryCommandsHomeResolutionErrors(t *testing.T) {
	original := resolveHomeDirFn
	t.Cleanup(func() { resolveHomeDirFn = original })
	resolveHomeDirFn = func() (string, error) {
		return "", errors.New("home unavailable")
	}

	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{name: "project list", run: func() error { return runProjectList(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, false) }},
		{name: "project add", run: func() error {
			return runProjectAdd(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "p", "", "")
		}},
		{name: "project archive", run: func() error { _, err := runArchiveProjectForTest("p"); return err }},
		{name: "project restore", run: func() error { return runProjectRestore(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "p") }},
		{name: "device list", run: func() error { return runDeviceList(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, false) }},
		{name: "device register", run: func() error { return runDeviceRegister(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "d") }},
		{name: "device archive", run: func() error { return runDeviceArchive(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "d") }},
		{name: "device restore", run: func() error { return runDeviceRestore(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "d") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("expected home resolution error")
			}
		})
	}
}

func TestProjectAndDeviceRegistryCommandBranches(t *testing.T) {
	setupEnv(t)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "project empty", args: []string{"project", "add", ""}, want: "project id must not be empty"},
		{name: "device empty", args: []string{"device", "register", ""}, want: "device id must not be empty"},
		{name: "project missing archive", args: []string{"project", "archive", "missing"}, want: "archive project"},
		{name: "project missing restore", args: []string{"project", "restore", "missing"}, want: "restore project"},
		{name: "device missing archive", args: []string{"device", "archive", "missing"}, want: "archive device"},
		{name: "device missing restore", args: []string{"device", "restore", "missing"}, want: "restore device"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}

	if err := runProjectAdd(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "archived-project", "", ""); err != nil {
		t.Fatalf("runProjectAdd: %v", err)
	}
	if _, err := runArchiveProjectForTest("archived-project"); err != nil {
		t.Fatalf("archive project: %v", err)
	}
	stdout := &bytes.Buffer{}
	if err := runProjectList(context.Background(), stdout, &bytes.Buffer{}, true); err != nil {
		t.Fatalf("runProjectList(all): %v", err)
	}
	if !strings.Contains(stdout.String(), "archived-project (archived)") {
		t.Fatalf("expected archived project marker, got %q", stdout.String())
	}

	if err := runDeviceRegister(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "archived-device"); err != nil {
		t.Fatalf("runDeviceRegister: %v", err)
	}
	if _, err := runArchiveDeviceForTest("archived-device"); err != nil {
		t.Fatalf("archive device: %v", err)
	}
	stdout.Reset()
	if err := runDeviceList(context.Background(), stdout, &bytes.Buffer{}, true); err != nil {
		t.Fatalf("runDeviceList(all): %v", err)
	}
	if !strings.Contains(stdout.String(), "archived-device (archived)") {
		t.Fatalf("expected archived device marker, got %q", stdout.String())
	}
}

func TestProvenanceResolutionAndValidationBranches(t *testing.T) {
	setupEnv(t)
	project := "project/a"
	device := "device/a"
	sourceRef := "src"
	resolvedProject, resolvedDevice, resolvedSourceRef, err := resolveRecordProvenance(&project, &device, &sourceRef, "project/a", "device/a", "src")
	if err != nil {
		t.Fatalf("resolveRecordProvenance: %v", err)
	}
	if resolvedProject != project || resolvedDevice != device || resolvedSourceRef == nil || *resolvedSourceRef != sourceRef {
		t.Fatalf("resolved provenance = %q %q %v", resolvedProject, resolvedDevice, resolvedSourceRef)
	}

	empty := " "
	if _, _, _, err := resolveRecordProvenance(&empty, &device, nil, "", "", ""); err == nil || !strings.Contains(err.Error(), "project_id") {
		t.Fatalf("expected empty project metadata error, got %v", err)
	}
	if _, _, _, err := resolveRecordProvenance(&project, &device, nil, "other", "", ""); err == nil || !strings.Contains(err.Error(), "project_id conflict") {
		t.Fatalf("expected project conflict, got %v", err)
	}
	if _, _, _, err := resolveRecordProvenance(&project, &device, &sourceRef, "", "", "other"); err == nil || !strings.Contains(err.Error(), "source_ref conflict") {
		t.Fatalf("expected source_ref conflict, got %v", err)
	}
	if provenanceFlagName("other_field") != "other-field" {
		t.Fatalf("unexpected fallback flag name")
	}
	if provenanceFlagName("source_device_id") != "device" {
		t.Fatalf("unexpected source device flag name")
	}
	if emptySource, err := resolveOptionalProvenanceValue("source_ref", &empty, ""); err != nil || emptySource != nil {
		t.Fatalf("empty optional provenance = %v, %v; want nil, nil", emptySource, err)
	}

	homeDir, err := resolveHomeDir()
	if err != nil {
		t.Fatalf("resolve home: %v", err)
	}
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open stack: %v", err)
	}
	defer func() { _ = stack.Close() }()
	ctx := context.Background()
	if err := validateActiveProjectAndDevice(ctx, stack.Repo, "missing", "test-device"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected missing project validation error, got %v", err)
	}
	if err := validateActiveProjectAndDevice(ctx, stack.Repo, "test/default-project", "missing"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected missing device validation error, got %v", err)
	}
	if _, err := stack.Repo.CreateProject(ctx, repository.CreateRegistryInput{ID: "archived"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := stack.Repo.ArchiveProject(ctx, "archived"); err != nil {
		t.Fatalf("archive project: %v", err)
	}
	if err := validateActiveProjectAndDevice(ctx, stack.Repo, "archived", "test-device"); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("expected archived project validation error, got %v", err)
	}
	if _, err := stack.Repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: "archived-device"}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if _, err := stack.Repo.ArchiveDevice(ctx, "archived-device"); err != nil {
		t.Fatalf("archive device: %v", err)
	}
	if err := validateActiveProjectAndDevice(ctx, stack.Repo, "test/default-project", "archived-device"); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("expected archived device validation error, got %v", err)
	}
	projectErr := errors.New("project lookup failed")
	if err := validateActiveProjectAndDevice(ctx, &mockRepo{
		getProjectByIDFn: func(context.Context, string) (repository.Project, error) {
			return repository.Project{}, projectErr
		},
	}, "project/a", "device/a"); !errors.Is(err, projectErr) {
		t.Fatalf("expected wrapped project lookup error, got %v", err)
	}
	deviceErr := errors.New("device lookup failed")
	if err := validateActiveProjectAndDevice(ctx, &mockRepo{
		getProjectByIDFn: func(context.Context, string) (repository.Project, error) {
			return repository.Project{ID: "project/a"}, nil
		},
		getDeviceByIDFn: func(context.Context, string) (repository.Device, error) {
			return repository.Device{}, deviceErr
		},
	}, "project/a", "device/a"); !errors.Is(err, deviceErr) {
		t.Fatalf("expected wrapped device lookup error, got %v", err)
	}
}

func runArchiveProjectForTest(id string) (repository.Project, error) {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return repository.Project{}, err
	}
	stack, err := openLocalStack(homeDir)
	if err != nil {
		return repository.Project{}, err
	}
	defer func() { _ = stack.Close() }()
	return stack.Repo.ArchiveProject(context.Background(), id)
}

func runArchiveDeviceForTest(id string) (repository.Device, error) {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return repository.Device{}, err
	}
	stack, err := openLocalStack(homeDir)
	if err != nil {
		return repository.Device{}, err
	}
	defer func() { _ = stack.Close() }()
	return stack.Repo.ArchiveDevice(context.Background(), id)
}
