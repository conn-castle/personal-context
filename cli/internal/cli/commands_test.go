package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Edit tests
// ---------------------------------------------------------------------------

func TestEditCommandSuccess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Add a record
	inputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte("<html>original</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir)

	addOut := &bytes.Buffer{}
	addCmd := NewRootCommand(RootCommandOptions{Stdout: addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", inputDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	// Edit with new content
	editDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(editDir, "record.html"), []byte("<html>updated</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, editDir)

	editOut := &bytes.Buffer{}
	editCmd := NewRootCommand(RootCommandOptions{Stdout: editOut, Stderr: &bytes.Buffer{}})
	editCmd.SetArgs([]string{"records", "edit", recordID, editDir})
	if err := editCmd.Execute(); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if !strings.Contains(editOut.String(), "updated") {
		t.Fatalf("expected stdout to contain 'updated', got %q", editOut.String())
	}
}

func TestEditCommandNotFound(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	editDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(editDir, "record.html"), []byte("<html>x</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, editDir)

	editCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	editCmd.SetArgs([]string{"records", "edit", "nonexistent-id", editDir})
	if err := editCmd.Execute(); err == nil {
		t.Fatal("expected error for nonexistent record ID")
	}
}

func TestEditCommandNoArgs(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "edit"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no args provided")
	}
}

func TestEditCommandWithFigures(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Add a record with a figure
	inputDir := t.TempDir()
	figDir := filepath.Join(inputDir, "figures")
	if err := os.MkdirAll(figDir, 0o755); err != nil {
		t.Fatalf("mkdir figures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(figDir, "original.png"), []byte("png-data"), 0o644); err != nil {
		t.Fatalf("write original.png: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte(`<html><img src="figures/original.png"></html>`), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir)

	addOut := &bytes.Buffer{}
	addCmd := NewRootCommand(RootCommandOptions{Stdout: addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", inputDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	// Edit with different figures
	editDir := t.TempDir()
	editFigDir := filepath.Join(editDir, "figures")
	if err := os.MkdirAll(editFigDir, 0o755); err != nil {
		t.Fatalf("mkdir figures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(editFigDir, "new.png"), []byte("new-png-data"), 0o644); err != nil {
		t.Fatalf("write new.png: %v", err)
	}
	if err := os.WriteFile(filepath.Join(editDir, "record.html"), []byte(`<html><img src="figures/new.png"></html>`), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, editDir)

	editOut := &bytes.Buffer{}
	editCmd := NewRootCommand(RootCommandOptions{Stdout: editOut, Stderr: &bytes.Buffer{}})
	editCmd.SetArgs([]string{"records", "edit", recordID, editDir})
	if err := editCmd.Execute(); err != nil {
		t.Fatalf("edit with figures: %v", err)
	}

	if !strings.Contains(editOut.String(), "updated") {
		t.Fatalf("expected stdout to contain 'updated', got %q", editOut.String())
	}
}

// ---------------------------------------------------------------------------
// Delete tests
// ---------------------------------------------------------------------------

func TestDeleteCommandSuccess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	inputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte("<html>test</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir)

	addOut := &bytes.Buffer{}
	addCmd := NewRootCommand(RootCommandOptions{Stdout: addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", inputDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	delOut := &bytes.Buffer{}
	delCmd := NewRootCommand(RootCommandOptions{Stdout: delOut, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", recordID})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if !strings.Contains(delOut.String(), "deleted") {
		t.Fatalf("expected stdout to contain 'deleted', got %q", delOut.String())
	}
}

func TestDeleteCommandNotFound(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", "nonexistent-id"})
	if err := delCmd.Execute(); err == nil {
		t.Fatal("expected error for nonexistent record ID")
	}
}

func TestDeleteCommandNoArgs(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "delete"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no args provided")
	}
}

// ---------------------------------------------------------------------------
// Restore tests
// ---------------------------------------------------------------------------

func TestRestoreCommandSuccess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	inputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte("<html>test</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir)

	addOut := &bytes.Buffer{}
	addCmd := NewRootCommand(RootCommandOptions{Stdout: addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", inputDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	// Delete the record first
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", recordID})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Restore it
	restoreOut := &bytes.Buffer{}
	restoreCmd := NewRootCommand(RootCommandOptions{Stdout: restoreOut, Stderr: &bytes.Buffer{}})
	restoreCmd.SetArgs([]string{"records", "restore", recordID})
	if err := restoreCmd.Execute(); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if !strings.Contains(restoreOut.String(), "restored") {
		t.Fatalf("expected stdout to contain 'restored', got %q", restoreOut.String())
	}
}

func TestRestoreCommandNotFound(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	restoreCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	restoreCmd.SetArgs([]string{"records", "restore", "nonexistent-id"})
	if err := restoreCmd.Execute(); err == nil {
		t.Fatal("expected error for nonexistent record ID")
	}
}

func TestRestoreCommandNoArgs(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "restore"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no args provided")
	}
}

// ---------------------------------------------------------------------------
// Move tests
// ---------------------------------------------------------------------------

func TestMoveCommandChangesDate(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	inputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte("<html>test</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir)

	addOut := &bytes.Buffer{}
	addCmd := NewRootCommand(RootCommandOptions{Stdout: addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", inputDir, "--date", "2025-06-01"})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	moveOut := &bytes.Buffer{}
	moveCmd := NewRootCommand(RootCommandOptions{Stdout: moveOut, Stderr: &bytes.Buffer{}})
	moveCmd.SetArgs([]string{"records", "move", recordID, "--date", "2025-07-15"})
	if err := moveCmd.Execute(); err != nil {
		t.Fatalf("move: %v", err)
	}

	if !strings.Contains(moveOut.String(), "moved") {
		t.Fatalf("expected stdout to contain 'moved', got %q", moveOut.String())
	}
}

func TestMoveCommandPositionFirst(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Add two records on the same date
	inputDir1 := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir1, "record.html"), []byte("<html>first</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir1)

	addOut1 := &bytes.Buffer{}
	addCmd1 := NewRootCommand(RootCommandOptions{Stdout: addOut1, Stderr: &bytes.Buffer{}})
	addCmd1.SetArgs([]string{"records", "add", inputDir1, "--date", "2025-06-01"})
	if err := addCmd1.Execute(); err != nil {
		t.Fatalf("add first: %v", err)
	}

	inputDir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir2, "record.html"), []byte("<html>second</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir2)

	addOut2 := &bytes.Buffer{}
	addCmd2 := NewRootCommand(RootCommandOptions{Stdout: addOut2, Stderr: &bytes.Buffer{}})
	addCmd2.SetArgs([]string{"records", "add", inputDir2, "--date", "2025-06-01"})
	if err := addCmd2.Execute(); err != nil {
		t.Fatalf("add second: %v", err)
	}
	recordID2 := strings.TrimSpace(addOut2.String())

	// Move second record to --first
	moveOut := &bytes.Buffer{}
	moveCmd := NewRootCommand(RootCommandOptions{Stdout: moveOut, Stderr: &bytes.Buffer{}})
	moveCmd.SetArgs([]string{"records", "move", recordID2, "--first"})
	if err := moveCmd.Execute(); err != nil {
		t.Fatalf("move --first: %v", err)
	}

	if !strings.Contains(moveOut.String(), "moved") {
		t.Fatalf("expected stdout to contain 'moved', got %q", moveOut.String())
	}
}

func TestMoveCommandNoFlags(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	inputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte("<html>test</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir)

	addOut := &bytes.Buffer{}
	addCmd := NewRootCommand(RootCommandOptions{Stdout: addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", inputDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	moveCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	moveCmd.SetArgs([]string{"records", "move", recordID})
	err := moveCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no flags provided")
	}
	if !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("expected error to contain 'at least one', got %q", err.Error())
	}
}

func TestMoveCommandNotFound(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	moveCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	moveCmd.SetArgs([]string{"records", "move", "nonexistent-id", "--date", "2025-07-15"})
	if err := moveCmd.Execute(); err == nil {
		t.Fatal("expected error for nonexistent record ID")
	}
}

func TestMoveCommandNoArgs(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "move"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no args provided")
	}
}

func TestMoveCommandInvalidDate(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	inputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte("<html>test</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	writeDefaultProvenanceMetadata(t, inputDir)

	addOut := &bytes.Buffer{}
	addCmd := NewRootCommand(RootCommandOptions{Stdout: addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", inputDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	moveCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	moveCmd.SetArgs([]string{"records", "move", recordID, "--date", "bad"})
	err := moveCmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid date")
	}
	if !strings.Contains(err.Error(), "invalid date") {
		t.Fatalf("expected error to contain 'invalid date', got %q", err.Error())
	}
}

// --- Edit coverage: data files path, old file cleanup ---

func TestEditWithDataFiles(t *testing.T) {
	setupEnv(t)
	// Add a record with data files
	id := addRecordWithContent(t,
		"<html>original</html>", "", "",
		nil,
		map[string][]byte{"old.csv": []byte("old data")},
	)

	// Edit with different data files
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(newDir, "record.html"), []byte("<html>edited</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDefaultProvenanceMetadata(t, newDir)
	dataDir := filepath.Join(newDir, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "new.csv"), []byte("new data"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "edit", id, newDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if !strings.Contains(stdout.String(), "updated") {
		t.Fatalf("expected 'updated' in output: %s", stdout.String())
	}
}

func TestEditReplacesFiguresAndDataFiles(t *testing.T) {
	setupEnv(t)
	// Add with both figures and data files
	id := addRecordWithContent(t,
		`<html><img src="figures/old.png">body</html>`,
		"notes",
		"",
		map[string][]byte{"old.png": []byte("old-fig")},
		map[string][]byte{"old-data.csv": []byte("x\n1\n")},
	)

	// Edit with new figures and data
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(newDir, "record.html"), []byte(`<html><img src="figures/new.png">new</html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDefaultProvenanceMetadata(t, newDir)
	if err := os.WriteFile(filepath.Join(newDir, "notes.md"), []byte("new notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	figDir := filepath.Join(newDir, "figures")
	if err := os.MkdirAll(figDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(figDir, "new.png"), []byte("new-fig"), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(newDir, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "new-data.csv"), []byte("y\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "edit", id, newDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if !strings.Contains(stdout.String(), "updated") {
		t.Fatalf("expected 'updated': %s", stdout.String())
	}
}

// --- Move coverage: after/before positions ---

func TestMovePositionAfter(t *testing.T) {
	setupEnv(t)
	id1 := addRecord(t, "--date", "2025-06-01")
	id2 := addRecord(t, "--date", "2025-06-01")
	id3 := addRecord(t, "--date", "2025-06-01")

	// Move id3 after id1
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "move", id3, "--after", id1})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("move --after: %v", err)
	}
	if !strings.Contains(stdout.String(), "moved") {
		t.Fatalf("expected 'moved': %s", stdout.String())
	}
	_ = id2
}

func TestMovePositionBefore(t *testing.T) {
	setupEnv(t)
	id1 := addRecord(t, "--date", "2025-06-02")
	_ = addRecord(t, "--date", "2025-06-02")
	id3 := addRecord(t, "--date", "2025-06-02")

	// Move id1 before id3
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "move", id1, "--before", id3})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("move --before: %v", err)
	}
	if !strings.Contains(stdout.String(), "moved") {
		t.Fatalf("expected 'moved': %s", stdout.String())
	}
}

func TestMovePositionLast(t *testing.T) {
	setupEnv(t)
	id1 := addRecord(t, "--date", "2025-06-03")
	_ = addRecord(t, "--date", "2025-06-03")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "move", id1, "--last"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("move --last: %v", err)
	}
}

func TestMoveDateAndPosition(t *testing.T) {
	setupEnv(t)
	id := addRecord(t, "--date", "2025-06-04")
	// Add some records on target date
	_ = addRecord(t, "--date", "2025-06-05")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "move", id, "--date", "2025-06-05", "--first"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("move --date --first: %v", err)
	}
}
