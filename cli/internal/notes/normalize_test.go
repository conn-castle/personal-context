package notes

import "testing"

func TestNormalizeNilValue(t *testing.T) {
	if Normalize(nil) != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestNormalizeEmptyString(t *testing.T) {
	input := ""
	if Normalize(&input) != nil {
		t.Fatal("expected nil for empty input")
	}
}

func TestNormalizeWhitespaceOnlyString(t *testing.T) {
	input := " \n\t  "
	if Normalize(&input) != nil {
		t.Fatal("expected nil for whitespace-only input")
	}
}

func TestNormalizeValidMarkdownPreserved(t *testing.T) {
	input := "# Title\n\nSome *markdown* content."
	got := Normalize(&input)
	if got == nil {
		t.Fatal("expected non-nil for valid markdown")
	}
	if *got != input {
		t.Fatalf("expected markdown unchanged, got %q", *got)
	}
}

func TestNormalizeCRLFToLF(t *testing.T) {
	input := "line one\r\nline two\r\n"
	got := Normalize(&input)
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if *got != "line one\nline two" {
		t.Fatalf("expected CRLF converted to LF and trailing trimmed, got %q", *got)
	}
}

func TestNormalizeStrayCR(t *testing.T) {
	input := "line one\rline two"
	got := Normalize(&input)
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if *got != "line one\nline two" {
		t.Fatalf("expected stray CR converted to LF, got %q", *got)
	}
}

func TestNormalizeTrimsTrailingWhitespacePerLine(t *testing.T) {
	input := "hello   \nworld\t\t"
	got := Normalize(&input)
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if *got != "hello\nworld" {
		t.Fatalf("expected trailing whitespace trimmed per line, got %q", *got)
	}
}

func TestNormalizeStringConvenience(t *testing.T) {
	got := NormalizeString("hello")
	if got == nil || *got != "hello" {
		t.Fatalf("expected pointer to \"hello\", got %v", got)
	}
}
