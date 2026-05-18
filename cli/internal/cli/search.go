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

const defaultSearchLimit = 50

func newSearchCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var formatFlag string
	var jsonFlag bool
	var limitFlag int
	var offsetFlag int
	var projectFlag string
	var domainFlag string
	var deletedFlag bool
	var includeToolOutputs bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search records and chats",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format := formatFlag
			if jsonFlag {
				if cmd.Flags().Changed("format") && formatFlag != "json" {
					return fmt.Errorf("--json cannot be combined with --format %s", formatFlag)
				}
				format = "json"
			}
			return runSearch(cmd.Context(), stdout, stderr, args[0], searchOptions{
				Format: format, Limit: limitFlag, Offset: offsetFlag, ProjectID: projectFlag,
				Domain: domainFlag, IncludeDeleted: deletedFlag, IncludeToolOutputs: includeToolOutputs,
			})
		},
	}

	cmd.Flags().StringVar(&formatFlag, "format", "table", "Output format (table|ids|json)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output JSON")
	cmd.Flags().IntVar(&limitFlag, "limit", defaultSearchLimit, "Maximum number of results (default 50; pass 0 for unlimited)")
	cmd.Flags().IntVar(&offsetFlag, "offset", 0, "Offset for pagination")
	cmd.Flags().StringVar(&projectFlag, "project", "", "Filter by project ID")
	cmd.Flags().StringVar(&domainFlag, "domain", "", "Filter by domain (records|chats)")
	cmd.Flags().BoolVar(&deletedFlag, "deleted", false, "Include soft-deleted records")
	cmd.Flags().BoolVar(&includeToolOutputs, "include-tool-outputs", false, "Include chat tool outputs")

	return cmd
}

type searchOptions struct {
	Format             string
	Limit              int
	Offset             int
	ProjectID          string
	Domain             string
	IncludeDeleted     bool
	IncludeToolOutputs bool
}

// searchResultJSON is the JSON representation for pc search --format json.
type searchResultJSON struct {
	Domain         string  `json:"domain"`
	ID             string  `json:"id"`
	ChatSessionID  string  `json:"chat_session_id,omitempty"`
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
	Source         string  `json:"source,omitempty"`
	SourceSession  string  `json:"source_session_id,omitempty"`
	Ordinal        *int    `json:"ordinal,omitempty"`
	Role           string  `json:"role,omitempty"`
	Snippet        string  `json:"snippet,omitempty"`
}

func runSearch(ctx context.Context, stdout io.Writer, stderr io.Writer, query string, opts searchOptions) error {
	query = strings.TrimSpace(query)

	switch opts.Format {
	case "table", "ids", "json":
		// valid
	default:
		return fmt.Errorf("unknown format %q: expected table, ids, or json", opts.Format)
	}
	if query == "" {
		return fmt.Errorf("query must not be empty")
	}
	if opts.Limit < 0 {
		return fmt.Errorf("limit must be >= 0")
	}
	if opts.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	domain := strings.TrimSpace(opts.Domain)
	if domain != "" && domain != "records" && domain != "chats" {
		return fmt.Errorf("unknown domain %q: expected records or chats", domain)
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

	filter := repository.UnifiedSearchFilter{
		Query:              query,
		Limit:              opts.Limit,
		Offset:             opts.Offset,
		IncludeDeleted:     opts.IncludeDeleted,
		IncludeToolOutputs: opts.IncludeToolOutputs,
	}
	if opts.ProjectID != "" {
		filter.ProjectID = &opts.ProjectID
	}
	if domain != "" {
		filter.Domain = &domain
	}
	// Fetch one extra row beyond the requested page so we can detect
	// truncation without a second COUNT query that would pull every match.
	if opts.Limit > 0 {
		filter.Limit = opts.Limit + 1
	}
	results, err := stack.Repo.SearchAll(ctx, filter)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	truncated := false
	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
		truncated = true
	}
	truncationMessage := fmt.Sprintf("Showing first %d results (use --limit 0 to see all, or --offset for the next page)", len(results))

	switch opts.Format {
	case "table":
		if err := searchTable(stdout, results); err != nil {
			return err
		}
		if truncated {
			_, _ = fmt.Fprintln(stdout, truncationMessage)
		}
		return nil
	case "ids":
		if err := searchIDs(stdout, results); err != nil {
			return err
		}
		if truncated {
			_, _ = fmt.Fprintln(stderr, truncationMessage)
		}
		return nil
	case "json":
		childCounts := map[string]repository.ChildCounts{}
		recordIDs := make([]string, 0)
		for _, result := range results {
			if result.Record != nil {
				recordIDs = append(recordIDs, result.Record.ID)
			}
		}
		if len(recordIDs) > 0 {
			childCounts, err = stack.Repo.CountRecordChildren(ctx, recordIDs)
			if err != nil {
				return fmt.Errorf("count record children: %w", err)
			}
		}
		return searchJSON(stdout, results, childCounts)
	default:
		return fmt.Errorf("unknown format %q: expected table, ids, or json", opts.Format)
	}
}

func searchTable(w io.Writer, results []repository.DomainSearchResult) error {
	if len(results) == 0 {
		_, _ = fmt.Fprintln(w, "No matching records or chats found.")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "DOMAIN\tID\tDate\tPROJECT\tSUMMARY")
	for _, result := range results {
		switch {
		case result.Record != nil:
			_, _ = fmt.Fprintf(tw, "records\t%s\t%s\t%s\t%s\n", result.Record.ID, result.Record.Date, result.Record.ProjectID, "")
		case result.Chat != nil:
			project := ""
			if result.Chat.Session.ProjectID != nil {
				project = *result.Chat.Session.ProjectID
			}
			_, _ = fmt.Fprintf(tw, "chats\t%s\t%s\t%s\t%s\n", result.Chat.Session.ID, result.Chat.Session.LastActivityAt.Format("2006-01-02"), project, truncate(result.Chat.Snippet, 80))
		}
	}
	return tw.Flush()
}

func searchIDs(w io.Writer, results []repository.DomainSearchResult) error {
	for _, result := range results {
		if result.Record != nil {
			_, _ = fmt.Fprintln(w, result.Record.ID)
		} else if result.Chat != nil {
			_, _ = fmt.Fprintln(w, result.Chat.Session.ID)
		}
	}
	return nil
}

func searchJSON(w io.Writer, domainResults []repository.DomainSearchResult, childCounts map[string]repository.ChildCounts) error {
	results := make([]searchResultJSON, 0, len(domainResults))
	for _, result := range domainResults {
		if result.Record != nil {
			s := result.Record
			counts := childCounts[s.ID]
			results = append(results, searchResultJSON{
				Domain:         "records",
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
			})
			continue
		}
		if result.Chat != nil {
			ordinal := result.Chat.Item.Ordinal
			project := ""
			if result.Chat.Session.ProjectID != nil {
				project = *result.Chat.Session.ProjectID
			}
			results = append(results, searchResultJSON{
				Domain:         "chats",
				ID:             result.Chat.Session.ID,
				ChatSessionID:  result.Chat.Session.ID,
				ProjectID:      project,
				SourceDeviceID: result.Chat.Session.SourceDeviceID,
				CreatedAt:      formatCLITime(result.Chat.Session.CreatedAt),
				UpdatedAt:      formatCLITime(result.Chat.Session.UpdatedAt),
				DeletedAt:      formatCLITimePtr(result.Chat.Session.DeletedAt),
				Source:         result.Chat.Session.Source,
				SourceSession:  result.Chat.Session.SourceSessionID,
				Ordinal:        &ordinal,
				Role:           result.Chat.Item.Role,
				Snippet:        result.Chat.Snippet,
			})
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}
