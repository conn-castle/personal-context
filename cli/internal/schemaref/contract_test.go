package schemaref

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func cliModuleRoot(t *testing.T) string {
	t.Helper()

	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("failed to resolve module root: %v", err)
	}

	return moduleRoot
}

func TestCanonicalRelativePaths(t *testing.T) {
	expected := []string{
		CanonicalSchemaSQLRelativePath,
		CanonicalSchemaTypesRelativePath,
	}

	if !reflect.DeepEqual(CanonicalRelativePaths(), expected) {
		t.Fatalf("unexpected canonical paths: %v", CanonicalRelativePaths())
	}
}

func TestResolveCanonicalPathsRequiresModuleRoot(t *testing.T) {
	_, err := ResolveCanonicalPaths("")
	if err == nil {
		t.Fatal("expected error for empty module root, got nil")
	}
	if !strings.Contains(err.Error(), "moduleRoot is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveCanonicalPaths(t *testing.T) {
	moduleRoot := cliModuleRoot(t)

	resolved, err := ResolveCanonicalPaths(moduleRoot)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	expected := []string{
		filepath.Clean(filepath.Join(moduleRoot, CanonicalSchemaSQLRelativePath)),
		filepath.Clean(filepath.Join(moduleRoot, CanonicalSchemaTypesRelativePath)),
	}
	if !reflect.DeepEqual(resolved, expected) {
		t.Fatalf("expected %v, got %v", expected, resolved)
	}
}

func TestValidateCanonicalPaths(t *testing.T) {
	if err := ValidateCanonicalPaths(cliModuleRoot(t)); err != nil {
		t.Fatalf("expected valid schema contract, got %v", err)
	}
}

func TestValidateCanonicalPathsFailsWhenFilesMissing(t *testing.T) {
	tempRoot := t.TempDir()
	moduleRoot := filepath.Join(tempRoot, "cli")
	if err := os.MkdirAll(moduleRoot, 0o755); err != nil {
		t.Fatalf("failed to create module root: %v", err)
	}

	err := ValidateCanonicalPaths(moduleRoot)
	if err == nil {
		t.Fatal("expected error for missing schema files, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, CanonicalSchemaSQLRelativePath) {
		t.Fatalf("expected schema.sql in error, got %v", err)
	}
	if !strings.Contains(errMsg, CanonicalSchemaTypesRelativePath) {
		t.Fatalf("expected schema-types.ts in error, got %v", err)
	}
}

func TestValidateCanonicalPathsFailsWhenSchemaPathIsDirectory(t *testing.T) {
	tempRoot := t.TempDir()
	moduleRoot := filepath.Join(tempRoot, "cli")
	if err := os.MkdirAll(moduleRoot, 0o755); err != nil {
		t.Fatalf("failed to create module root: %v", err)
	}

	schemaAsDirectory := filepath.Clean(filepath.Join(moduleRoot, CanonicalSchemaSQLRelativePath))
	if err := os.MkdirAll(schemaAsDirectory, 0o755); err != nil {
		t.Fatalf("failed to create schema directory placeholder: %v", err)
	}

	err := ValidateCanonicalPaths(moduleRoot)
	if err == nil {
		t.Fatal("expected error for schema directory placeholder, got nil")
	}
	if !strings.Contains(err.Error(), "expected file, found directory") {
		t.Fatalf("expected directory validation error, got %v", err)
	}
}
