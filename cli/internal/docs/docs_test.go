package docs

import (
	"strings"
	"testing"
)

func TestTopicsAndGet(t *testing.T) {
	topics := Topics()
	if len(topics) == 0 {
		t.Fatal("expected embedded topics")
	}
	names := map[string]bool{}
	for _, topic := range topics {
		names[topic.Name] = true
		if topic.Title == "" {
			t.Fatalf("topic %q missing title", topic.Name)
		}
		if !strings.Contains(topic.Content, "#") {
			t.Fatalf("topic %q missing markdown headings", topic.Name)
		}
	}
	for _, want := range []string{"chat-import", "item-types", "schema", "search-syntax", "project-device-registry"} {
		if !names[want] {
			t.Fatalf("missing expected topic %q", want)
		}
	}
	content, err := Get("schema")
	if err != nil {
		t.Fatalf("Get(schema): %v", err)
	}
	if !strings.Contains(content, "parent_source_session_id") {
		t.Fatal("schema topic should document parent_source_session_id")
	}
}

func TestGetUnknownTopicListsAvailable(t *testing.T) {
	_, err := Get("does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown topic")
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Fatalf("error should list available topics, got %v", err)
	}
}

func TestSearch(t *testing.T) {
	if Search("   ") != nil {
		t.Fatal("blank query should return nil")
	}
	if got := Search("zzqqxx-not-present"); got != nil {
		t.Fatalf("no-hit query should return nil, got %+v", got)
	}

	hits := Search("parent_source_session_id")
	if len(hits) == 0 {
		t.Fatal("expected hits for parent_source_session_id")
	}
	foundSchema := false
	for _, h := range hits {
		if h.Topic == "schema" {
			foundSchema = true
			if h.Heading == "" || h.Excerpt == "" {
				t.Fatalf("hit missing heading/excerpt: %+v", h)
			}
		}
	}
	if !foundSchema {
		t.Fatalf("expected a schema hit, got %+v", hits)
	}

	// Multi-term AND: both terms must appear in the same section.
	if len(Search("items_after_import items_delta")) == 0 {
		t.Fatal("expected AND hits in chat-import")
	}
	// A term present only in different sections must not produce a false hit.
	if got := Search("parent_source_session_id include-tool-outputs"); got != nil {
		t.Fatalf("cross-section terms should not match a single section, got %+v", got)
	}

	// Deterministic ordering.
	first := Search("Personal Context")
	second := Search("Personal Context")
	if len(first) != len(second) {
		t.Fatal("search not deterministic in length")
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("search order not deterministic at %d", i)
		}
	}
}
