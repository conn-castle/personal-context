package listpage

import (
	"bytes"
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
