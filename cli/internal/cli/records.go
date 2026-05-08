package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

const (
	defaultRecordListLimit = 50
	maxRecordListLimit     = 500
)

type recordFilterOptions struct {
	ProjectID string
	DateFrom  string
	DateTo    string
	Deleted   bool
}

type recordListOptions struct {
	recordFilterOptions
	Limit   int
	Cursor  string
	HasHTML bool
	HasData bool
	All     bool
	Format  string
}

type recordListItem struct {
	ID             string  `json:"id"`
	Date           string  `json:"date"`
	DayOrder       string  `json:"day_order"`
	ProjectID      string  `json:"project_id"`
	SourceDeviceID string  `json:"source_device_id"`
	UpdatedAt      string  `json:"updated_at"`
	DeletedAt      *string `json:"deleted_at"`
	HasHTML        bool    `json:"has_html"`
	HasNotes       bool    `json:"has_notes"`
	FigureCount    int     `json:"figure_count"`
	DataFileCount  int     `json:"data_file_count"`
}

type recordListJSON struct {
	Items      []recordListItem `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

type cliCursorPayload struct {
	Date     string `json:"date"`
	DayOrder string `json:"day_order"`
	ID       string `json:"id"`
}

func newListCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	opts := recordListOptions{Limit: defaultRecordListLimit, Format: "table"}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List record summaries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), stdout, stderr, opts)
		},
	}
	addRecordFilterFlags(cmd, &opts.recordFilterOptions)
	cmd.Flags().IntVar(&opts.Limit, "limit", defaultRecordListLimit, "Maximum records to return")
	cmd.Flags().StringVar(&opts.Cursor, "cursor", "", "Cursor from a previous page")
	cmd.Flags().BoolVar(&opts.HasHTML, "has-html", false, "Show only records with HTML content")
	cmd.Flags().BoolVar(&opts.HasData, "has-data", false, "Show only records with data files")
	cmd.Flags().BoolVar(&opts.All, "all", false, "Return all matching records")
	cmd.Flags().StringVar(&opts.Format, "format", "table", "Output format (table|ids|json)")
	return cmd
}

func runList(ctx context.Context, stdout io.Writer, stderr io.Writer, opts recordListOptions) error {
	switch opts.Format {
	case "table", "ids", "json":
	default:
		return fmt.Errorf("unknown format %q: expected table, ids, or json", opts.Format)
	}
	if opts.Limit < 1 {
		return fmt.Errorf("limit must be >= 1")
	}
	if opts.Limit > maxRecordListLimit {
		return fmt.Errorf("limit must be <= %d", maxRecordListLimit)
	}
	if opts.All && strings.TrimSpace(opts.Cursor) != "" {
		return fmt.Errorf("--all cannot be used with --cursor")
	}

	filter, err := buildListSlidesFilter(opts.recordFilterOptions)
	if err != nil {
		return err
	}
	cursor, err := decodeCLICursor(strings.TrimSpace(opts.Cursor))
	if err != nil {
		return err
	}

	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	slides, err := stack.Repo.ListSlides(ctx, filter)
	if err != nil {
		return fmt.Errorf("list records: %w", err)
	}
	sortRecordsForDiscovery(slides)
	slides = applyRecordCursor(slides, cursor)

	items := make([]recordListItem, 0, len(slides))
	for _, slide := range slides {
		item, err := buildRecordListItem(ctx, stack.Repo, slide)
		if err != nil {
			return err
		}
		if opts.HasHTML && !item.HasHTML {
			continue
		}
		if opts.HasData && item.DataFileCount == 0 {
			continue
		}
		items = append(items, item)
	}

	var nextCursor *string
	if !opts.All && len(items) > opts.Limit {
		pageItems := items[:opts.Limit]
		last := pageItems[len(pageItems)-1]
		cursor := encodeCLICursor(cliCursorPayload{Date: last.Date, DayOrder: last.DayOrder, ID: last.ID})
		nextCursor = &cursor
		items = pageItems
	}

	switch opts.Format {
	case "table":
		return writeRecordListTable(stdout, items, nextCursor)
	case "ids":
		if err := writeRecordListIDs(stdout, items); err != nil {
			return err
		}
		if nextCursor != nil {
			_, _ = fmt.Fprintf(stderr, "Next cursor: %s\n", *nextCursor)
		}
		return nil
	case "json":
		return writeIndentedJSON(stdout, recordListJSON{Items: items, NextCursor: nextCursor})
	default:
		return fmt.Errorf("unknown format %q: expected table, ids, or json", opts.Format)
	}
}

type recordStatsOptions struct {
	recordFilterOptions
	Format string
}

type recordStatsJSON struct {
	ActiveRecordCount      int     `json:"active_record_count"`
	DeletedRecordCount     int     `json:"deleted_record_count"`
	SelectedRecordCount    int     `json:"selected_record_count"`
	HTMLRecordCount        int     `json:"html_record_count"`
	NotesRecordCount       int     `json:"notes_record_count"`
	FigureCount            int     `json:"figure_count"`
	DataFileCount          int     `json:"data_file_count"`
	OldestRecordDate       *string `json:"oldest_record_date"`
	NewestRecordDate       *string `json:"newest_record_date"`
	RecordedDataFileBytes  int64   `json:"recorded_data_file_bytes"`
	LocalAttachmentBytes   int64   `json:"local_attachment_bytes"`
	StoreFileBytes         int64   `json:"store_file_bytes"`
	LocalTotalBytes        int64   `json:"local_total_bytes"`
	MissingAttachmentCount int     `json:"missing_attachment_count"`
}

func newStatsCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	opts := recordStatsOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show record store statistics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStats(cmd.Context(), stdout, stderr, opts)
		},
	}
	addRecordFilterFlags(cmd, &opts.recordFilterOptions)
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format (text|json)")
	return cmd
}

func runStats(ctx context.Context, stdout io.Writer, _ io.Writer, opts recordStatsOptions) error {
	switch opts.Format {
	case "text", "json":
	default:
		return fmt.Errorf("unknown format %q: expected text or json", opts.Format)
	}
	baseFilter, err := buildBaseRecordFilter(opts.recordFilterOptions)
	if err != nil {
		return err
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

	activeFilter := baseFilter
	activeFilter.IncludeDeleted = false
	activeFilter.OnlyDeleted = false
	activeSlides, err := stack.Repo.ListSlides(ctx, activeFilter)
	if err != nil {
		return fmt.Errorf("list active records: %w", err)
	}
	deletedFilter := baseFilter
	deletedFilter.IncludeDeleted = true
	deletedFilter.OnlyDeleted = true
	deletedSlides, err := stack.Repo.ListSlides(ctx, deletedFilter)
	if err != nil {
		return fmt.Errorf("list deleted records: %w", err)
	}

	selectedSlides := activeSlides
	if opts.Deleted {
		selectedSlides = deletedSlides
	}
	stats, err := buildRecordStats(ctx, homeDir, stack, selectedSlides)
	if err != nil {
		return err
	}
	stats.ActiveRecordCount = len(activeSlides)
	stats.DeletedRecordCount = len(deletedSlides)
	stats.SelectedRecordCount = len(selectedSlides)

	if opts.Format == "json" {
		return writeIndentedJSON(stdout, stats)
	}
	return writeRecordStatsText(stdout, stats, opts.Deleted)
}

type filesListOptions struct {
	recordFilterOptions
	RecordID string
	Format   string
}

type fileInventoryItem struct {
	RecordID     string  `json:"record_id"`
	Date         string  `json:"date"`
	ProjectID    string  `json:"project_id"`
	Kind         string  `json:"kind"`
	Filename     string  `json:"filename"`
	S3Key        string  `json:"s3_key"`
	RecordedSize *int64  `json:"recorded_size,omitempty"`
	LocalSize    *int64  `json:"local_size,omitempty"`
	LocalPath    string  `json:"local_path"`
	Status       string  `json:"status"`
	Hash         *string `json:"hash,omitempty"`
}

func newFilesCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "files",
		Short: "Inspect record attachment inventory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newFilesListCommand(stdout, stderr))
	return cmd
}

func newFilesListCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	opts := filesListOptions{Format: "table"}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List record attachment files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFilesList(cmd.Context(), stdout, stderr, opts)
		},
	}
	addRecordFilterFlags(cmd, &opts.recordFilterOptions)
	cmd.Flags().StringVar(&opts.RecordID, "record", "", "Filter by record ID")
	cmd.Flags().StringVar(&opts.Format, "format", "table", "Output format (table|json)")
	return cmd
}

func runFilesList(ctx context.Context, stdout io.Writer, _ io.Writer, opts filesListOptions) error {
	switch opts.Format {
	case "table", "json":
	default:
		return fmt.Errorf("unknown format %q: expected table or json", opts.Format)
	}
	filter, err := buildListSlidesFilter(opts.recordFilterOptions)
	if err != nil {
		return err
	}
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	slides, err := stack.Repo.ListSlides(ctx, filter)
	if err != nil {
		return fmt.Errorf("list records: %w", err)
	}
	sortRecordsForDiscovery(slides)
	recordID := strings.TrimSpace(opts.RecordID)
	items := make([]fileInventoryItem, 0)
	for _, slide := range slides {
		if recordID != "" && slide.ID != recordID {
			continue
		}
		slideItems, err := buildFileInventoryItems(ctx, stack, slide)
		if err != nil {
			return err
		}
		items = append(items, slideItems...)
	}
	if recordID != "" && len(items) == 0 {
		if _, err := stack.Repo.GetSlideByID(ctx, recordID); err != nil {
			return fmt.Errorf("record %q not found", recordID)
		}
	}

	if opts.Format == "json" {
		return writeIndentedJSON(stdout, items)
	}
	return writeFilesListTable(stdout, items)
}

func addRecordFilterFlags(cmd *cobra.Command, opts *recordFilterOptions) {
	cmd.Flags().StringVar(&opts.ProjectID, "project", "", "Filter by project ID")
	cmd.Flags().StringVar(&opts.DateFrom, "from", "", "Filter records on or after date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&opts.DateTo, "to", "", "Filter records on or before date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&opts.Deleted, "deleted", false, "Show only soft-deleted records")
}

func buildListSlidesFilter(opts recordFilterOptions) (repository.ListSlidesFilter, error) {
	filter, err := buildBaseRecordFilter(opts)
	if err != nil {
		return repository.ListSlidesFilter{}, err
	}
	if opts.Deleted {
		filter.IncludeDeleted = true
		filter.OnlyDeleted = true
	}
	return filter, nil
}

func buildBaseRecordFilter(opts recordFilterOptions) (repository.ListSlidesFilter, error) {
	from := strings.TrimSpace(opts.DateFrom)
	to := strings.TrimSpace(opts.DateTo)
	if from != "" && !isValidRecordDate(from) {
		return repository.ListSlidesFilter{}, fmt.Errorf("invalid --from date %q: expected YYYY-MM-DD", from)
	}
	if to != "" && !isValidRecordDate(to) {
		return repository.ListSlidesFilter{}, fmt.Errorf("invalid --to date %q: expected YYYY-MM-DD", to)
	}
	if from != "" && to != "" && from > to {
		return repository.ListSlidesFilter{}, fmt.Errorf("--from must be on or before --to")
	}

	filter := repository.ListSlidesFilter{}
	if project := strings.TrimSpace(opts.ProjectID); project != "" {
		filter.ProjectID = &project
	}
	if from != "" {
		filter.DateFrom = &from
	}
	if to != "" {
		filter.DateTo = &to
	}
	return filter, nil
}

func isValidRecordDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func sortRecordsForDiscovery(slides []repository.Slide) {
	slices.SortFunc(slides, func(a, b repository.Slide) int {
		if a.Date > b.Date {
			return -1
		}
		if a.Date < b.Date {
			return 1
		}
		if a.DayOrder < b.DayOrder {
			return -1
		}
		if a.DayOrder > b.DayOrder {
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
}

func applyRecordCursor(slides []repository.Slide, cursor *cliCursorPayload) []repository.Slide {
	if cursor == nil {
		return slides
	}
	for i, slide := range slides {
		if recordIsAfterCursor(slide, cursor) {
			return slides[i:]
		}
	}
	return nil
}

func recordIsAfterCursor(slide repository.Slide, cursor *cliCursorPayload) bool {
	if slide.Date != cursor.Date {
		return slide.Date < cursor.Date
	}
	if slide.DayOrder != cursor.DayOrder {
		return slide.DayOrder > cursor.DayOrder
	}
	return slide.ID > cursor.ID
}

func encodeCLICursor(cursor cliCursorPayload) string {
	data, _ := json.Marshal(cursor)
	return base64.StdEncoding.EncodeToString(data)
}

func decodeCLICursor(raw string) (*cliCursorPayload, error) {
	if raw == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor encoding")
	}
	var cursor cliCursorPayload
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fmt.Errorf("invalid cursor format")
	}
	if cursor.Date == "" || cursor.DayOrder == "" || cursor.ID == "" {
		return nil, fmt.Errorf("incomplete cursor")
	}
	return &cursor, nil
}

func buildRecordListItem(ctx context.Context, repo repository.Repository, slide repository.Slide) (recordListItem, error) {
	figures, err := repo.ListSlideFiguresBySlideID(ctx, slide.ID)
	if err != nil {
		return recordListItem{}, fmt.Errorf("list figures for %s: %w", slide.ID, err)
	}
	dataFiles, err := repo.ListSlideDataFilesBySlideID(ctx, slide.ID)
	if err != nil {
		return recordListItem{}, fmt.Errorf("list data files for %s: %w", slide.ID, err)
	}
	item := recordListItem{
		ID:             slide.ID,
		Date:           slide.Date,
		DayOrder:       slide.DayOrder,
		ProjectID:      slide.ProjectID,
		SourceDeviceID: slide.SourceDeviceID,
		UpdatedAt:      formatCLITime(slide.UpdatedAt),
		DeletedAt:      formatCLITimePtr(slide.DeletedAt),
		HasHTML:        slide.HTMLContent != nil,
		HasNotes:       slide.Notes != nil,
		FigureCount:    len(figures),
		DataFileCount:  len(dataFiles),
	}
	return item, nil
}

func buildRecordStats(ctx context.Context, homeDir string, stack *localStack, slides []repository.Slide) (recordStatsJSON, error) {
	stats := recordStatsJSON{}
	for _, slide := range slides {
		if stats.OldestRecordDate == nil || slide.Date < *stats.OldestRecordDate {
			date := slide.Date
			stats.OldestRecordDate = &date
		}
		if stats.NewestRecordDate == nil || slide.Date > *stats.NewestRecordDate {
			date := slide.Date
			stats.NewestRecordDate = &date
		}
		if slide.HTMLContent != nil {
			stats.HTMLRecordCount++
		}
		if slide.Notes != nil {
			stats.NotesRecordCount++
		}

		figures, err := stack.Repo.ListSlideFiguresBySlideID(ctx, slide.ID)
		if err != nil {
			return recordStatsJSON{}, fmt.Errorf("list figures for %s: %w", slide.ID, err)
		}
		dataFiles, err := stack.Repo.ListSlideDataFilesBySlideID(ctx, slide.ID)
		if err != nil {
			return recordStatsJSON{}, fmt.Errorf("list data files for %s: %w", slide.ID, err)
		}
		stats.FigureCount += len(figures)
		stats.DataFileCount += len(dataFiles)
		for _, figure := range figures {
			size, ok, err := statLocalAttachment(stack.FS.ResolveFigurePath, slide.ID, figure.Filename)
			if err != nil {
				return recordStatsJSON{}, err
			}
			if ok {
				stats.LocalAttachmentBytes += size
			} else {
				stats.MissingAttachmentCount++
			}
		}
		for _, dataFile := range dataFiles {
			stats.RecordedDataFileBytes += dataFile.Size
			size, ok, err := statLocalAttachment(stack.FS.ResolveDataFilePath, slide.ID, dataFile.Filename)
			if err != nil {
				return recordStatsJSON{}, err
			}
			if ok {
				stats.LocalAttachmentBytes += size
			} else {
				stats.MissingAttachmentCount++
			}
		}
	}
	storeBytes, err := localStoreFileBytes(homeDir)
	if err != nil {
		return recordStatsJSON{}, err
	}
	stats.StoreFileBytes = storeBytes
	stats.LocalTotalBytes = stats.LocalAttachmentBytes + stats.StoreFileBytes
	return stats, nil
}

func buildFileInventoryItems(ctx context.Context, stack *localStack, slide repository.Slide) ([]fileInventoryItem, error) {
	figures, err := stack.Repo.ListSlideFiguresBySlideID(ctx, slide.ID)
	if err != nil {
		return nil, fmt.Errorf("list figures for %s: %w", slide.ID, err)
	}
	dataFiles, err := stack.Repo.ListSlideDataFilesBySlideID(ctx, slide.ID)
	if err != nil {
		return nil, fmt.Errorf("list data files for %s: %w", slide.ID, err)
	}
	items := make([]fileInventoryItem, 0, len(figures)+len(dataFiles))
	for _, figure := range figures {
		path, size, status, err := resolveLocalAttachment(stack.FS.ResolveFigurePath, slide.ID, figure.Filename)
		if err != nil {
			return nil, err
		}
		items = append(items, fileInventoryItem{
			RecordID:  slide.ID,
			Date:      slide.Date,
			ProjectID: slide.ProjectID,
			Kind:      "figure",
			Filename:  figure.Filename,
			S3Key:     figure.S3Key,
			LocalSize: size,
			LocalPath: path,
			Status:    status,
		})
	}
	for _, dataFile := range dataFiles {
		path, size, status, err := resolveLocalAttachment(stack.FS.ResolveDataFilePath, slide.ID, dataFile.Filename)
		if err != nil {
			return nil, err
		}
		recordedSize := dataFile.Size
		hash := dataFile.Hash
		items = append(items, fileInventoryItem{
			RecordID:     slide.ID,
			Date:         slide.Date,
			ProjectID:    slide.ProjectID,
			Kind:         "data",
			Filename:     dataFile.Filename,
			S3Key:        dataFile.S3Key,
			RecordedSize: &recordedSize,
			LocalSize:    size,
			LocalPath:    path,
			Status:       status,
			Hash:         &hash,
		})
	}
	return items, nil
}

func statLocalAttachment(resolve func(string, string) (string, error), slideID string, filename string) (int64, bool, error) {
	path, err := resolve(slideID, filename)
	if err != nil {
		return 0, false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("stat local attachment %s: %w", path, err)
	}
	if info.IsDir() {
		return 0, false, fmt.Errorf("local attachment path is a directory: %s", path)
	}
	return info.Size(), true, nil
}

func resolveLocalAttachment(resolve func(string, string) (string, error), slideID string, filename string) (string, *int64, string, error) {
	path, err := resolve(slideID, filename)
	if err != nil {
		return "", nil, "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, nil, "missing", nil
		}
		return "", nil, "", fmt.Errorf("stat local attachment %s: %w", path, err)
	}
	if info.IsDir() {
		return "", nil, "", fmt.Errorf("local attachment path is a directory: %s", path)
	}
	size := info.Size()
	return path, &size, "present", nil
}

func localStoreFileBytes(homeDir string) (int64, error) {
	var total int64
	for _, path := range []string{
		dbPath(homeDir),
		dbPath(homeDir) + "-wal",
		dbPath(homeDir) + "-shm",
		dbPath(homeDir) + "-journal",
	} {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, fmt.Errorf("stat store file %s: %w", path, err)
		}
		if !info.IsDir() {
			total += info.Size()
		}
	}
	return total, nil
}

func writeRecordListTable(w io.Writer, items []recordListItem, nextCursor *string) error {
	if len(items) == 0 {
		_, _ = fmt.Fprintln(w, "No matching records found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tDate\tProject\tHTML\tNotes\tFigures\tData")
	for _, item := range items {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%t\t%d\t%d\n", item.ID, item.Date, item.ProjectID, item.HasHTML, item.HasNotes, item.FigureCount, item.DataFileCount)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if nextCursor != nil {
		_, _ = fmt.Fprintf(w, "Next cursor: %s\n", *nextCursor)
	}
	return nil
}

func writeRecordListIDs(w io.Writer, items []recordListItem) error {
	for _, item := range items {
		_, _ = fmt.Fprintln(w, item.ID)
	}
	return nil
}

func writeRecordStatsText(w io.Writer, stats recordStatsJSON, deleted bool) error {
	selection := "active"
	if deleted {
		selection = "deleted"
	}
	_, _ = fmt.Fprintf(w, "Selected records: %d (%s)\n", stats.SelectedRecordCount, selection)
	_, _ = fmt.Fprintf(w, "Active records: %d\n", stats.ActiveRecordCount)
	_, _ = fmt.Fprintf(w, "Deleted records: %d\n", stats.DeletedRecordCount)
	_, _ = fmt.Fprintf(w, "HTML records: %d\n", stats.HTMLRecordCount)
	_, _ = fmt.Fprintf(w, "Notes records: %d\n", stats.NotesRecordCount)
	_, _ = fmt.Fprintf(w, "Figures: %d\n", stats.FigureCount)
	_, _ = fmt.Fprintf(w, "Data files: %d\n", stats.DataFileCount)
	if stats.OldestRecordDate != nil {
		_, _ = fmt.Fprintf(w, "Oldest record date: %s\n", *stats.OldestRecordDate)
	} else {
		_, _ = fmt.Fprintln(w, "Oldest record date: (none)")
	}
	if stats.NewestRecordDate != nil {
		_, _ = fmt.Fprintf(w, "Newest record date: %s\n", *stats.NewestRecordDate)
	} else {
		_, _ = fmt.Fprintln(w, "Newest record date: (none)")
	}
	_, _ = fmt.Fprintf(w, "Recorded data-file bytes: %d\n", stats.RecordedDataFileBytes)
	_, _ = fmt.Fprintf(w, "Local attachment bytes: %d\n", stats.LocalAttachmentBytes)
	_, _ = fmt.Fprintf(w, "Store file bytes: %d\n", stats.StoreFileBytes)
	_, _ = fmt.Fprintf(w, "Local total bytes: %d\n", stats.LocalTotalBytes)
	_, _ = fmt.Fprintf(w, "Missing attachments: %d\n", stats.MissingAttachmentCount)
	return nil
}

func writeFilesListTable(w io.Writer, items []fileInventoryItem) error {
	if len(items) == 0 {
		_, _ = fmt.Fprintln(w, "No matching files found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "Record\tKind\tFilename\tRecorded\tLocal\tStatus\tPath")
	for _, item := range items {
		recorded := ""
		if item.RecordedSize != nil {
			recorded = fmt.Sprintf("%d", *item.RecordedSize)
		}
		local := ""
		if item.LocalSize != nil {
			local = fmt.Sprintf("%d", *item.LocalSize)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.RecordID, item.Kind, item.Filename, recorded, local, item.Status, item.LocalPath)
	}
	return tw.Flush()
}

func writeIndentedJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func formatCLITime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func formatCLITimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := formatCLITime(*t)
	return &formatted
}
