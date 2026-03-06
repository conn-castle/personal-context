package notes

import "strings"

// Normalize converts nil/empty/whitespace-only note values to nil.
// Args: value is an optional markdown string.
// Returns: nil when value has no meaningful content; otherwise the original pointer.
func Normalize(value *string) *string {
	if value == nil {
		return nil
	}

	if strings.TrimSpace(*value) == "" {
		return nil
	}

	return value
}

// NormalizeString is a convenience wrapper for callers that hold plain strings.
// Args: value is a markdown string that may be empty or whitespace.
// Returns: nil when value has no meaningful content; otherwise a pointer to value.
func NormalizeString(value string) *string {
	return Normalize(&value)
}
