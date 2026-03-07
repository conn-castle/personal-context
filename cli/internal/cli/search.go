package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newSearchCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var formatFlag string
	var limitFlag int
	var projectFlag string
	var deletedFlag bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search slides by content, notes, or project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd.Context(), stdout, stderr, args[0], formatFlag, limitFlag, projectFlag, deletedFlag)
		},
	}

	cmd.Flags().StringVar(&formatFlag, "format", "table", "Output format (table|ids|json)")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results (0 = no limit)")
	cmd.Flags().StringVar(&projectFlag, "project", "", "Filter by project ID")
	cmd.Flags().BoolVar(&deletedFlag, "deleted", false, "Include soft-deleted slides")

	return cmd
}

// searchResultJSON is the JSON representation for pc search --format json.
type searchResultJSON struct {
	ID        string  `json:"id"`
	Date      string  `json:"date"`
	DayOrder  string  `json:"day_order"`
	ProjectID *string `json:"project_id"`
	DeletedAt *string `json:"deleted_at"`
}

func runSearch(ctx context.Context, stdout io.Writer, _ io.Writer, query string, format string, limit int, project string, deleted bool) error {
	query = strings.TrimSpace(query)

	switch format {
	case "table", "ids", "json":
		// valid
	default:
		return fmt.Errorf("unknown format %q: expected table, ids, or json", format)
	}
	if query == "" {
		return fmt.Errorf("query must not be empty")
	}
	if limit < 0 {
		return fmt.Errorf("limit must be >= 0")
	}

	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	filter := repository.ListSlidesFilter{
		Query:          &query,
		Limit:          limit,
		IncludeDeleted: deleted,
	}
	if project != "" {
		filter.ProjectID = &project
	}

	slides, err := stack.Repo.ListSlides(ctx, filter)
	if err != nil {
		return fmt.Errorf("search slides: %w", err)
	}

	switch format {
	case "table":
		return searchTable(stdout, slides)
	case "ids":
		return searchIDs(stdout, slides)
	case "json":
		return searchJSON(stdout, slides)
	default:
		return fmt.Errorf("unknown format %q: expected table, ids, or json", format)
	}
}

func searchTable(w io.Writer, slides []repository.Slide) error {
	if len(slides) == 0 {
		_, _ = fmt.Fprintln(w, "No matching slides found.")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tDate\tProject")
	for _, s := range slides {
		project := ""
		if s.ProjectID != nil {
			project = *s.ProjectID
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", s.ID, s.Date, project)
	}
	return tw.Flush()
}

func searchIDs(w io.Writer, slides []repository.Slide) error {
	for _, s := range slides {
		_, _ = fmt.Fprintln(w, s.ID)
	}
	return nil
}

func searchJSON(w io.Writer, slides []repository.Slide) error {
	results := make([]searchResultJSON, 0, len(slides))
	for _, s := range slides {
		r := searchResultJSON{
			ID:        s.ID,
			Date:      s.Date,
			DayOrder:  s.DayOrder,
			ProjectID: s.ProjectID,
		}
		if s.DeletedAt != nil {
			ts := s.DeletedAt.UTC().Format("2006-01-02T15:04:05.000Z")
			r.DeletedAt = &ts
		}
		results = append(results, r)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}
