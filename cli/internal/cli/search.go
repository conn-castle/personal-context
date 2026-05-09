package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/conn-castle/personal-context/cli/internal/listpage"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

const defaultSearchLimit = 50

func newSearchCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var formatFlag string
	var limitFlag int
	var projectFlag string
	var deletedFlag bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search records by content, notes, or project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd.Context(), stdout, stderr, args[0], formatFlag, limitFlag, projectFlag, deletedFlag)
		},
	}

	cmd.Flags().StringVar(&formatFlag, "format", "table", "Output format (table|ids|json)")
	cmd.Flags().IntVar(&limitFlag, "limit", defaultSearchLimit, "Maximum number of results (default 50; pass 0 for unlimited)")
	cmd.Flags().StringVar(&projectFlag, "project", "", "Filter by project ID")
	cmd.Flags().BoolVar(&deletedFlag, "deleted", false, "Include soft-deleted records")

	return cmd
}

// searchResultJSON is the JSON representation for pc search --format json.
type searchResultJSON struct {
	ID             string  `json:"id"`
	Date           string  `json:"date"`
	DayOrder       string  `json:"day_order"`
	ProjectID      string  `json:"project_id"`
	SourceDeviceID string  `json:"source_device_id"`
	SourceRef      *string `json:"source_ref"`
	GitRemoteURL   *string `json:"git_remote_url"`
	GitHash        *string `json:"git_hash"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	DeletedAt      *string `json:"deleted_at"`
	HasHTML        bool    `json:"has_html"`
	HasNotes       bool    `json:"has_notes"`
	FigureCount    int     `json:"figure_count"`
	DataFileCount  int     `json:"data_file_count"`
}

func runSearch(ctx context.Context, stdout io.Writer, stderr io.Writer, query string, format string, limit int, project string, deleted bool) error {
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

	filter := repository.ListRecordsFilter{
		Query:          &query,
		Limit:          limit,
		IncludeDeleted: deleted,
	}
	if project != "" {
		filter.ProjectID = &project
	}

	total, err := stack.Repo.CountRecords(ctx, filter)
	if err != nil {
		return fmt.Errorf("count search records: %w", err)
	}

	records, err := stack.Repo.ListRecords(ctx, filter)
	if err != nil {
		return fmt.Errorf("search records: %w", err)
	}
	truncated := total > len(records)
	truncationMessage := fmt.Sprintf("Showing %d of %d results (use --limit 0 to see all)", len(records), total)

	switch format {
	case "table":
		if err := searchTable(stdout, records); err != nil {
			return err
		}
		if truncated {
			_, _ = fmt.Fprintln(stdout, truncationMessage)
		}
		return nil
	case "ids":
		if err := searchIDs(stdout, records); err != nil {
			return err
		}
		if truncated {
			_, _ = fmt.Fprintln(stderr, truncationMessage)
		}
		return nil
	case "json":
		childCounts, err := stack.Repo.CountRecordChildren(ctx, recordIDs(records))
		if err != nil {
			return fmt.Errorf("count record children: %w", err)
		}
		return searchJSON(stdout, records, childCounts, total)
	default:
		return fmt.Errorf("unknown format %q: expected table, ids, or json", format)
	}
}

func searchTable(w io.Writer, records []repository.Record) error {
	if len(records) == 0 {
		_, _ = fmt.Fprintln(w, "No matching records found.")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tDate\tProject")
	for _, s := range records {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", s.ID, s.Date, s.ProjectID)
	}
	return tw.Flush()
}

func searchIDs(w io.Writer, records []repository.Record) error {
	for _, s := range records {
		_, _ = fmt.Fprintln(w, s.ID)
	}
	return nil
}

func searchJSON(w io.Writer, records []repository.Record, childCounts map[string]repository.ChildCounts, total int) error {
	results := make([]searchResultJSON, 0, len(records))
	for _, s := range records {
		counts := childCounts[s.ID]
		r := searchResultJSON{
			ID:             s.ID,
			Date:           s.Date,
			DayOrder:       s.DayOrder,
			ProjectID:      s.ProjectID,
			SourceDeviceID: s.SourceDeviceID,
			SourceRef:      s.SourceRef,
			GitRemoteURL:   s.GitRemoteURL,
			GitHash:        s.GitHash,
			CreatedAt:      formatCLITime(s.CreatedAt),
			UpdatedAt:      formatCLITime(s.UpdatedAt),
			DeletedAt:      formatCLITimePtr(s.DeletedAt),
			HasHTML:        s.HTMLContent != nil,
			HasNotes:       s.Notes != nil,
			FigureCount:    counts.Figures,
			DataFileCount:  counts.DataFiles,
		}
		results = append(results, r)
	}

	return listpage.WriteJSON(w, listpage.Response[searchResultJSON]{Items: results, Total: total, NextCursor: nil})
}
