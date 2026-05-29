package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type failingDocsWriter struct{}

func (failingDocsWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("docs write failed")
}

func runDocs(t *testing.T, args ...string) (string, error) {
	t.Helper()
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs(append([]string{"docs"}, args...))
	err := cmd.Execute()
	return stdout.String(), err
}

func TestDocsListsTopics(t *testing.T) {
	out, err := runDocs(t)
	if err != nil {
		t.Fatalf("docs: %v", err)
	}
	for _, topic := range []string{"chat-import", "item-types", "schema", "search-syntax", "project-device-registry"} {
		if !strings.Contains(out, topic) {
			t.Fatalf("docs list missing %q: %s", topic, out)
		}
	}
}

func TestDocsPrintsTopic(t *testing.T) {
	out, err := runDocs(t, "chat-import")
	if err != nil {
		t.Fatalf("docs chat-import: %v", err)
	}
	if !strings.Contains(out, "items_after_import") || !strings.Contains(out, "collisions_skipped") {
		t.Fatalf("expected chat-import content, got %s", out)
	}
}

func TestDocsUnknownTopicErrors(t *testing.T) {
	_, err := runDocs(t, "nope")
	if err == nil || !strings.Contains(err.Error(), "unknown docs topic") {
		t.Fatalf("expected unknown topic error, got %v", err)
	}
}

func TestDocsSearchHits(t *testing.T) {
	out, err := runDocs(t, "search", "parent_source_session_id")
	if err != nil {
		t.Fatalf("docs search: %v", err)
	}
	if !strings.Contains(out, "schema") {
		t.Fatalf("expected schema hit, got %s", out)
	}
}

func TestDocsSearchNoHits(t *testing.T) {
	out, err := runDocs(t, "search", "zzqqxx-not-present")
	if err != nil {
		t.Fatalf("docs search: %v", err)
	}
	if !strings.Contains(out, "No documentation matches") {
		t.Fatalf("expected no-hit message, got %s", out)
	}
}

func TestDocsPropagatesWriteErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "list"},
		{name: "topic", args: []string{"chat-import"}},
		{name: "search hit", args: []string{"search", "parent_source_session_id"}},
		{name: "search miss", args: []string{"search", "zzqqxx-not-present"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewRootCommand(RootCommandOptions{Stdout: failingDocsWriter{}, Stderr: &bytes.Buffer{}})
			cmd.SetArgs(append([]string{"docs"}, tc.args...))
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "docs write failed") {
				t.Fatalf("expected docs write failure, got %v", err)
			}
		})
	}
}
