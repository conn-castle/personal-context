package schemaref

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	CanonicalSchemaSQLRelativePath   = "../schema/schema.sql"
	CanonicalSchemaTypesRelativePath = "../schema/schema-types.ts"
)

// CanonicalRelativePaths returns canonical schema paths relative to the `cli/` module root.
// Returns: [schema.sql path, schema-types.ts path] in fixed order.
func CanonicalRelativePaths() []string {
	return []string{
		CanonicalSchemaSQLRelativePath,
		CanonicalSchemaTypesRelativePath,
	}
}

// ResolveCanonicalPaths resolves canonical schema paths against the provided module root.
// Args: moduleRoot is the path to the `cli/` module directory.
// Returns: absolute filesystem paths to canonical schema files.
func ResolveCanonicalPaths(moduleRoot string) ([]string, error) {
	if moduleRoot == "" {
		return nil, errors.New("moduleRoot is required")
	}

	canonical := CanonicalRelativePaths()
	resolved := make([]string, 0, len(canonical))
	for _, relativePath := range canonical {
		resolved = append(resolved, filepath.Clean(filepath.Join(moduleRoot, relativePath)))
	}

	return resolved, nil
}

// ValidateCanonicalPaths verifies that canonical schema files exist and are regular files.
// Args: moduleRoot is the path to the `cli/` module directory.
// Returns: nil when the schema contract is satisfied; otherwise a descriptive error listing all violations.
func ValidateCanonicalPaths(moduleRoot string) error {
	canonical := CanonicalRelativePaths()
	resolved, err := ResolveCanonicalPaths(moduleRoot)
	if err != nil {
		return err
	}

	var violations []error
	for index, absolutePath := range resolved {
		info, statErr := os.Stat(absolutePath)
		if statErr != nil {
			violations = append(violations, fmt.Errorf("schema contract violation (%s): %w", canonical[index], statErr))
			continue
		}
		if info.IsDir() {
			violations = append(violations, fmt.Errorf("schema contract violation (%s): expected file, found directory", canonical[index]))
		}
	}

	return errors.Join(violations...)
}
