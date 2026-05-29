// Package docs embeds Personal Context concept documentation into the binary so
// `pc docs` always serves reference material that matches the installed version.
package docs

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed *.md
var topicFS embed.FS

// Topic is one embedded documentation topic.
type Topic struct {
	// Name is the topic slug used on the command line (file name without .md).
	Name string
	// Title is the first level-1 heading, used as a one-line description.
	Title string
	// Content is the full markdown body.
	Content string
}

// Topics returns every embedded topic sorted by name.
// Returns: topics in deterministic name order.
func Topics() []Topic {
	// topicFS is populated by //go:embed, so listing and reading its files
	// cannot fail at runtime.
	entries, _ := fs.ReadDir(topicFS, ".")
	topics := make([]Topic, 0, len(entries))
	for _, entry := range entries {
		content, _ := topicFS.ReadFile(entry.Name())
		topics = append(topics, Topic{
			Name:    strings.TrimSuffix(entry.Name(), ".md"),
			Title:   firstHeading(string(content)),
			Content: string(content),
		})
	}
	sort.Slice(topics, func(i, j int) bool { return topics[i].Name < topics[j].Name })
	return topics
}

// Get returns the markdown content for one topic.
// Args: name is the topic slug.
// Returns: the topic content, or an error naming the available topics.
func Get(name string) (string, error) {
	content, err := topicFS.ReadFile(name + ".md")
	if err != nil {
		available := make([]string, 0)
		for _, topic := range Topics() {
			available = append(available, topic.Name)
		}
		return "", fmt.Errorf("unknown docs topic %q: available topics are %s", name, strings.Join(available, ", "))
	}
	return string(content), nil
}

// SearchHit is one section-level documentation match.
type SearchHit struct {
	Topic   string
	Heading string
	Excerpt string
}

// Search returns the documentation sections that contain every whitespace-
// separated term in query (case-insensitive). Results are deterministic:
// ordered by topic name, then by section order within the topic.
// Args: query is one or more search terms.
// Returns: matching sections; empty when query is blank or nothing matches.
func Search(query string) []SearchHit {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil
	}
	var hits []SearchHit
	for _, topic := range Topics() {
		for _, section := range splitSections(topic.Content) {
			lower := strings.ToLower(section.body)
			if !containsAll(lower, terms) {
				continue
			}
			hits = append(hits, SearchHit{Topic: topic.Name, Heading: section.heading, Excerpt: excerpt(section.body, terms)})
		}
	}
	return hits
}

type section struct {
	heading string
	body    string
}

// splitSections breaks markdown into sections keyed by their heading line. Text
// before the first heading is grouped under the topic's title (or "(intro)").
func splitSections(content string) []section {
	lines := strings.Split(content, "\n")
	var sections []section
	current := section{heading: "(intro)"}
	var body []string
	flush := func() {
		current.body = strings.TrimSpace(strings.Join(body, "\n"))
		if current.body != "" || current.heading != "(intro)" {
			sections = append(sections, current)
		}
		body = nil
	}
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			flush()
			current = section{heading: strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))}
		}
		body = append(body, line)
	}
	flush()
	return sections
}

func containsAll(haystack string, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}

// excerpt returns the first non-heading line that contains a search term, so a
// hit shows surrounding context rather than just the heading.
func excerpt(body string, terms []string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lower := strings.ToLower(trimmed)
		for _, term := range terms {
			if strings.Contains(lower, term) {
				return trimmed
			}
		}
	}
	return ""
}

func firstHeading(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			return strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		}
	}
	return ""
}
