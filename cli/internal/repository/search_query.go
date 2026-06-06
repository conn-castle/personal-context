package repository

import (
	"fmt"
	"strings"
)

// ValidateSearchQuery rejects standalone all-uppercase boolean-style operator
// tokens (OR, AND, NOT, NEAR). Lowercase and mixed-case words such as `and`,
// `or`, `not`, and `near` remain literal search terms, so natural-language
// queries like `research and development`, `not found`, and `near miss`
// continue to work. It returns an error wrapping ErrUnsupportedSearchOperator
// that points the user to `pc docs search-syntax`; nil means the query is a
// valid implicit-AND query.
func ValidateSearchQuery(query string) error {
	for _, token := range strings.Fields(query) {
		switch token {
		case "OR", "AND", "NOT", "NEAR":
			return fmt.Errorf("%w: %q is not supported; Personal Context search is implicit-AND only (whitespace-separated terms are combined with AND). See `pc docs search-syntax`", ErrUnsupportedSearchOperator, token)
		}
	}
	return nil
}
