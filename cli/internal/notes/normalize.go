package notes

import "strings"

// Normalize canonicalizes note values: converts CRLF to LF, trims trailing
// whitespace from each line, and returns nil for empty/whitespace-only inputs.
// Args: value is an optional markdown string.
// Returns: nil when value has no meaningful content; otherwise a pointer to the normalized string.
func Normalize(value *string) *string {
	if value == nil {
		return nil
	}

	// Normalize line endings: CRLF → LF.
	normalized := strings.ReplaceAll(*value, "\r\n", "\n")
	// Remove stray CR.
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	// Trim trailing whitespace from each line.
	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	normalized = strings.Join(lines, "\n")

	// Trim leading/trailing blank lines.
	normalized = strings.TrimSpace(normalized)

	if normalized == "" {
		return nil
	}

	return &normalized
}

// NormalizeString is a convenience wrapper for callers that hold plain strings.
// Args: value is a markdown string that may be empty or whitespace.
// Returns: nil when value has no meaningful content; otherwise a pointer to value.
func NormalizeString(value string) *string {
	return Normalize(&value)
}
