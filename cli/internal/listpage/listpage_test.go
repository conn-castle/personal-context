package listpage

import (
	"bytes"
	"errors"
	"testing"
)

type testItem struct {
	ID string `json:"id"`
}

func TestWriteJSONEmptyItemsNullCursor(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSON(&buf, Response[testItem]{
		Items:      []testItem{},
		Total:      0,
		NextCursor: nil,
	})
	if err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	want := "{\n  \"items\": [],\n  \"total\": 0,\n  \"next_cursor\": null\n}\n"
	if got := buf.String(); got != want {
		t.Fatalf("WriteJSON() = %q, want %q", got, want)
	}
}

func TestMarshalIndentPopulatedItemsWithCursor(t *testing.T) {
	cursor := "next-page"
	got, err := MarshalIndent(Response[testItem]{
		Items:      []testItem{{ID: "a"}, {ID: "b"}},
		Total:      10,
		NextCursor: &cursor,
	})
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}

	want := "{\n  \"items\": [\n    {\n      \"id\": \"a\"\n    },\n    {\n      \"id\": \"b\"\n    }\n  ],\n  \"total\": 10,\n  \"next_cursor\": \"next-page\"\n}"
	if string(got) != want {
		t.Fatalf("MarshalIndent() = %q, want %q", string(got), want)
	}
}

func TestMarshalIndentPopulatedItemsNullCursor(t *testing.T) {
	got, err := MarshalIndent(Response[testItem]{
		Items:      []testItem{{ID: "a"}},
		Total:      1,
		NextCursor: nil,
	})
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}

	want := "{\n  \"items\": [\n    {\n      \"id\": \"a\"\n    }\n  ],\n  \"total\": 1,\n  \"next_cursor\": null\n}"
	if string(got) != want {
		t.Fatalf("MarshalIndent() = %q, want %q", string(got), want)
	}
}

func TestEncodeDecodeCursorRoundTrip(t *testing.T) {
	original := Cursor{Date: "2026-05-10", DayOrder: "a0", ID: "20260510-a3f2b7e1"}
	encoded := EncodeCursor(original)
	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if decoded == nil || *decoded != original {
		t.Fatalf("DecodeCursor() = %v, want %v", decoded, original)
	}
}

func TestDecodeCursorEmptyReturnsNil(t *testing.T) {
	c, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("DecodeCursor(\"\") error = %v", err)
	}
	if c != nil {
		t.Fatalf("DecodeCursor(\"\") = %+v, want nil", c)
	}
}

func TestDecodeCursorInvalidBase64(t *testing.T) {
	_, err := DecodeCursor("not!valid!base64")
	if !errors.Is(err, ErrCursorEncoding) {
		t.Fatalf("DecodeCursor() error = %v, want ErrCursorEncoding", err)
	}
}

func TestDecodeCursorInvalidJSON(t *testing.T) {
	_, err := DecodeCursor("bm90LWpzb24=") // base64("not-json")
	if !errors.Is(err, ErrCursorFormat) {
		t.Fatalf("DecodeCursor() error = %v, want ErrCursorFormat", err)
	}
}

func TestDecodeCursorIncompleteFields(t *testing.T) {
	encoded := EncodeCursor(Cursor{Date: "2026-05-10", DayOrder: "", ID: ""})
	_, err := DecodeCursor(encoded)
	if !errors.Is(err, ErrCursorFormat) {
		t.Fatalf("DecodeCursor() error = %v, want ErrCursorFormat", err)
	}
}

func TestIsAfterCursor(t *testing.T) {
	cursor := Cursor{Date: "2026-05-10", DayOrder: "a0", ID: "id-2"}

	tests := []struct {
		name     string
		date     string
		dayOrder string
		id       string
		want     bool
	}{
		{"earlier date is after under DESC", "2026-05-09", "a0", "id-2", true},
		{"later date is before under DESC", "2026-05-11", "a0", "id-2", false},
		{"same date larger day_order is after", "2026-05-10", "a1", "id-2", true},
		{"same date smaller day_order is before", "2026-05-10", "9z", "id-2", false},
		{"same date+order larger id is after", "2026-05-10", "a0", "id-3", true},
		{"same date+order smaller id is before", "2026-05-10", "a0", "id-1", false},
		{"identical is not after", "2026-05-10", "a0", "id-2", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAfterCursor(tt.date, tt.dayOrder, tt.id, cursor); got != tt.want {
				t.Fatalf("IsAfterCursor() = %v, want %v", got, tt.want)
			}
		})
	}
}
