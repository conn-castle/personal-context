package notes

import "testing"

func TestFromStringReturnsCopiedPointer(t *testing.T) {
	value := "hello"
	got := FromString(value)
	if got == nil {
		t.Fatal("expected non-nil pointer")
	}
	if *got != value {
		t.Fatalf("expected %q, got %q", value, *got)
	}
}
