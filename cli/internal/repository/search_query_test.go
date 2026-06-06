package repository

import (
	"errors"
	"testing"
)

func TestValidateSearchQueryRejectsOnlyUppercaseOperatorTokens(t *testing.T) {
	if err := ValidateSearchQuery("alpha OR beta"); !errors.Is(err, ErrUnsupportedSearchOperator) {
		t.Fatalf("expected ErrUnsupportedSearchOperator for uppercase OR, got %v", err)
	}

	for _, query := range []string{
		"research and development",
		"not found",
		"near miss",
		"ordinary words",
	} {
		if err := ValidateSearchQuery(query); err != nil {
			t.Fatalf("ValidateSearchQuery(%q) error = %v", query, err)
		}
	}
}
