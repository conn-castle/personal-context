package fractionalindex

import (
	"sort"
	"testing"
)

func TestGenerateKnownCases(t *testing.T) {
	testCases := []struct {
		before string
		after  string
		want   string
	}{
		{"", "", "a0"},
		{"", "a0", "Zz"},
		{"", "Zz", "Zy"},
		{"a0", "", "a1"},
		{"a1", "", "a2"},
		{"a0", "a1", "a0V"},
		{"a1", "a2", "a1V"},
		{"a0V", "a1", "a0l"},
		{"Zz", "a0", "ZzV"},
		{"Zz", "a1", "a0"},
		{"", "Y00", "Xzzz"},
		{"bzz", "", "c000"},
		{"a0", "a0V", "a0G"},
		{"a0", "a0G", "a08"},
		{"b125", "b129", "b127"},
		{"a0", "a1V", "a1"},
		{"Zz", "a01", "a0"},
		{"", "a0V", "a0"},
		{"", "b999", "b99"},
		{"aV", "aV0V", "aV0G"},
	}

	for _, tc := range testCases {
		t.Run(tc.before+"|"+tc.after, func(t *testing.T) {
			got, err := GenerateBetween(tc.before, tc.after)
			if err != nil {
				t.Fatalf("GenerateBetween(%q, %q) error = %v", tc.before, tc.after, err)
			}
			if got != tc.want {
				t.Fatalf("GenerateBetween(%q, %q) = %q, want %q", tc.before, tc.after, got, tc.want)
			}
		})
	}
}

func TestGenerateRejectsInvalidInputs(t *testing.T) {
	testCases := []struct {
		before string
		after  string
	}{
		{"a00", ""},
		{"a00", "a1"},
		{"0", "1"},
		{"a1", "a0"},
		{"", "A00000000000000000000000000"},
	}

	for _, tc := range testCases {
		t.Run(tc.before+"|"+tc.after, func(t *testing.T) {
			if _, err := GenerateBetween(tc.before, tc.after); err == nil {
				t.Fatalf("expected error for bounds before=%q after=%q", tc.before, tc.after)
			}
		})
	}
}

func TestGenerateAtStart(t *testing.T) {
	first := "a0"
	candidate, err := GenerateAtStart(first)
	if err != nil {
		t.Fatalf("GenerateAtStart() error = %v", err)
	}
	if candidate >= first {
		t.Fatalf("expected %q < %q", candidate, first)
	}
}

func TestGenerateAtEnd(t *testing.T) {
	last := "a0"
	candidate, err := GenerateAtEnd(last)
	if err != nil {
		t.Fatalf("GenerateAtEnd() error = %v", err)
	}
	if candidate <= last {
		t.Fatalf("expected %q > %q", candidate, last)
	}
}

func TestGenerateBetweenMaintainsLexicographicOrdering(t *testing.T) {
	keys := []string{}
	for i := 0; i < 25; i++ {
		var before, after string
		if len(keys) > 0 {
			before = keys[len(keys)-1]
		}
		candidate, err := GenerateBetween(before, after)
		if err != nil {
			t.Fatalf("GenerateBetween() error = %v", err)
		}
		keys = append(keys, candidate)
	}

	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	for i := range sorted {
		if keys[i] != sorted[i] {
			t.Fatalf("keys not lexicographically sorted at index %d: got %q, want %q", i, keys[i], sorted[i])
		}
	}
}

func TestRepeatedInsertionsBetweenSamePositions(t *testing.T) {
	left := "a0"
	right := "a1"

	for i := 0; i < 50; i++ {
		mid, err := GenerateBetween(left, right)
		if err != nil {
			t.Fatalf("GenerateBetween() iteration %d error = %v", i, err)
		}
		if left >= mid || mid >= right {
			t.Fatalf("iteration %d expected %q < %q < %q", i, left, mid, right)
		}
		right = mid
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("a0"); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}
	if err := Validate("a00"); err == nil {
		t.Fatal("expected Validate(invalid) to fail")
	}
}

func TestGenerateBetweenCoversIntegerEdgeBranches(t *testing.T) {
	candidate, err := GenerateBetween("a9", "")
	if err != nil {
		t.Fatalf("GenerateBetween(a9, \"\") error = %v", err)
	}
	if candidate != "aA" {
		t.Fatalf("expected deterministic successor for a9, got %q", candidate)
	}

	candidate, err = GenerateBetween("a0", "a0V")
	if err != nil {
		t.Fatalf("GenerateBetween(a0, a0V) error = %v", err)
	}
	if !(candidate > "a0" && candidate < "a0V") {
		t.Fatalf("expected bounded candidate between a0 and a0V, got %q", candidate)
	}
}
