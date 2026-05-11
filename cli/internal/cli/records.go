package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/listpage"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/timeutil"
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

	filter, err := buildListRecordsFilter(opts.recordFilterOptions)
	if err != nil {
		return err
	}
	filter.HasHTML = opts.HasHTML
	filter.HasData = opts.HasData
	cursor, err := listpage.DecodeCursor(strings.TrimSpace(opts.Cursor))
	if err != nil {
		return err
	}

	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	total, err := stack.Repo.CountRecords(ctx, filter)
	if err != nil {
		return fmt.Errorf("count records: %w", err)
	}

	records, err := stack.Repo.ListRecords(ctx, filter)
	if err != nil {
		return fmt.Errorf("list records: %w", err)
	}
	sortRecordsForDiscovery(records)
	records = applyRecordCursor(records, cursor)

	itemCap := opts.Limit + 1
	if opts.All || len(records) < itemCap {
		itemCap = len(records)
	}
	pageRecords := records
	if !opts.All && len(pageRecords) > opts.Limit+1 {
		pageRecords = pageRecords[:opts.Limit+1]
	}

	childCounts, err := stack.Repo.CountRecordChildren(ctx, recordIDs(pageRecords))
	if err != nil {
		return fmt.Errorf("count record children: %w", err)
	}

	items := make([]recordListItem, 0, itemCap)
	for _, record := range pageRecords {
		items = append(items, buildRecordListItem(record, childCounts[record.ID]))
	}

	var nextCursor *string
	if !opts.All && len(items) > opts.Limit {
		pageItems := items[:opts.Limit]
		last := pageItems[len(pageItems)-1]
		cursor := listpage.EncodeCursor(listpage.Cursor{Date: last.Date, DayOrder: last.DayOrder, ID: last.ID})
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
		return listpage.WriteJSON(stdout, listpage.Response[recordListItem]{Items: items, Total: total, NextCursor: nextCursor})
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
	activeRecords, err := stack.Repo.ListRecords(ctx, activeFilter)
	if err != nil {
		return fmt.Errorf("list active records: %w", err)
	}
	deletedFilter := baseFilter
	deletedFilter.IncludeDeleted = true
	deletedFilter.OnlyDeleted = true
	deletedRecords, err := stack.Repo.ListRecords(ctx, deletedFilter)
	if err != nil {
		return fmt.Errorf("list deleted records: %w", err)
	}

	selectedRecords := activeRecords
	if opts.Deleted {
		selectedRecords = deletedRecords
	}
	stats, err := buildRecordStats(ctx, homeDir, stack, selectedRecords)
	if err != nil {
		return err
	}
	stats.ActiveRecordCount = len(activeRecords)
	stats.DeletedRecordCount = len(deletedRecords)
	stats.SelectedRecordCount = len(selectedRecords)

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
	filter, err := buildListRecordsFilter(opts.recordFilterOptions)
	if err != nil {
		return err
	}
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	recordID := strings.TrimSpace(opts.RecordID)
	var records []repository.Record
	if recordID != "" {
		record, err := stack.Repo.GetRecordByID(ctx, recordID)
		if err != nil {
			return fmt.Errorf("record %q not found", recordID)
		}
		if !recordMatchesFilter(record, filter) {
			return fmt.Errorf("record %q does not match the requested filters", recordID)
		}
		records = []repository.Record{record}
	} else {
		records, err = stack.Repo.ListRecords(ctx, filter)
		if err != nil {
			return fmt.Errorf("list records: %w", err)
		}
		sortRecordsForDiscovery(records)
	}

	items := make([]fileInventoryItem, 0)
	for _, record := range records {
		recordItems, err := buildFileInventoryItems(ctx, stack, record)
		if err != nil {
			return err
		}
		items = append(items, recordItems...)
	}

	if opts.Format == "json" {
		return writeIndentedJSON(stdout, items)
	}
	return writeFilesListTable(stdout, items)
}

func recordMatchesFilter(record repository.Record, filter repository.ListRecordsFilter) bool {
	if filter.ProjectID != nil && record.ProjectID != *filter.ProjectID {
		return false
	}
	if filter.DateFrom != nil && record.Date < *filter.DateFrom {
		return false
	}
	if filter.DateTo != nil && record.Date > *filter.DateTo {
		return false
	}
	deleted := record.DeletedAt != nil
	if filter.OnlyDeleted && !deleted {
		return false
	}
	if !filter.IncludeDeleted && deleted {
		return false
	}
	return true
}

func addRecordFilterFlags(cmd *cobra.Command, opts *recordFilterOptions) {
	cmd.Flags().StringVar(&opts.ProjectID, "project", "", "Filter by project ID")
	cmd.Flags().StringVar(&opts.DateFrom, "from", "", "Filter records on or after date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&opts.DateTo, "to", "", "Filter records on or before date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&opts.Deleted, "deleted", false, "Show only soft-deleted records")
}

func buildListRecordsFilter(opts recordFilterOptions) (repository.ListRecordsFilter, error) {
	filter, err := buildBaseRecordFilter(opts)
	if err != nil {
		return repository.ListRecordsFilter{}, err
	}
	if opts.Deleted {
		filter.IncludeDeleted = true
		filter.OnlyDeleted = true
	}
	return filter, nil
}

func buildBaseRecordFilter(opts recordFilterOptions) (repository.ListRecordsFilter, error) {
	from := strings.TrimSpace(opts.DateFrom)
	to := strings.TrimSpace(opts.DateTo)
	if from != "" && !isValidRecordDate(from) {
		return repository.ListRecordsFilter{}, fmt.Errorf("invalid --from date %q: expected YYYY-MM-DD", from)
	}
	if to != "" && !isValidRecordDate(to) {
		return repository.ListRecordsFilter{}, fmt.Errorf("invalid --to date %q: expected YYYY-MM-DD", to)
	}
	if from != "" && to != "" && from > to {
		return repository.ListRecordsFilter{}, fmt.Errorf("--from must be on or before --to")
	}

	filter := repository.ListRecordsFilter{}
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

// sortRecordsForDiscovery orders records date-DESC, day_order-ASC, id-ASC —
// the canonical "newest day first; oldest position first within a day" order
// used by `pc list` and `pc files list`.
func sortRecordsForDiscovery(records []repository.Record) {
	slices.SortFunc(records, compareRecordsForDiscovery)
}

func compareRecordsForDiscovery(a, b repository.Record) int {
	if cmp := strings.Compare(b.Date, a.Date); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(a.DayOrder, b.DayOrder); cmp != 0 {
		return cmp
	}
	return strings.Compare(a.ID, b.ID)
}

// applyRecordCursor advances past records that sort before the cursor under
// the canonical (date DESC, day_order ASC, id ASC) order, returning the
// remaining suffix.
func applyRecordCursor(records []repository.Record, cursor *listpage.Cursor) []repository.Record {
	if cursor == nil {
		return records
	}
	for i, record := range records {
		if listpage.IsAfterCursor(record.Date, record.DayOrder, record.ID, *cursor) {
			return records[i:]
		}
	}
	return nil
}

func recordIDs(records []repository.Record) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids
}

func buildRecordListItem(record repository.Record, childCounts repository.ChildCounts) recordListItem {
	return recordListItem{
		ID:             record.ID,
		Date:           record.Date,
		DayOrder:       record.DayOrder,
		ProjectID:      record.ProjectID,
		SourceDeviceID: record.SourceDeviceID,
		UpdatedAt:      formatCLITime(record.UpdatedAt),
		DeletedAt:      formatCLITimePtr(record.DeletedAt),
		HasHTML:        record.HTMLContent != nil,
		HasNotes:       record.Notes != nil,
		FigureCount:    childCounts.Figures,
		DataFileCount:  childCounts.DataFiles,
	}
}

func buildRecordStats(ctx context.Context, homeDir string, stack *localStack, records []repository.Record) (recordStatsJSON, error) {
	stats := recordStatsJSON{}
	for _, record := range records {
		if stats.OldestRecordDate == nil || record.Date < *stats.OldestRecordDate {
			date := record.Date
			stats.OldestRecordDate = &date
		}
		if stats.NewestRecordDate == nil || record.Date > *stats.NewestRecordDate {
			date := record.Date
			stats.NewestRecordDate = &date
		}
		if record.HTMLContent != nil {
			stats.HTMLRecordCount++
		}
		if record.Notes != nil {
			stats.NotesRecordCount++
		}

		figures, err := stack.Repo.ListRecordFiguresByRecordID(ctx, record.ID)
		if err != nil {
			return recordStatsJSON{}, fmt.Errorf("list figures for %s: %w", record.ID, err)
		}
		dataFiles, err := stack.Repo.ListRecordDataFilesByRecordID(ctx, record.ID)
		if err != nil {
			return recordStatsJSON{}, fmt.Errorf("list data files for %s: %w", record.ID, err)
		}
		stats.FigureCount += len(figures)
		stats.DataFileCount += len(dataFiles)
		for _, figure := range figures {
			size, ok, err := statLocalAttachment(stack.FS.ResolveFigurePath, record.ID, figure.Filename)
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
			size, ok, err := statLocalAttachment(stack.FS.ResolveDataFilePath, record.ID, dataFile.Filename)
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

func buildFileInventoryItems(ctx context.Context, stack *localStack, record repository.Record) ([]fileInventoryItem, error) {
	figures, err := stack.Repo.ListRecordFiguresByRecordID(ctx, record.ID)
	if err != nil {
		return nil, fmt.Errorf("list figures for %s: %w", record.ID, err)
	}
	dataFiles, err := stack.Repo.ListRecordDataFilesByRecordID(ctx, record.ID)
	if err != nil {
		return nil, fmt.Errorf("list data files for %s: %w", record.ID, err)
	}
	items := make([]fileInventoryItem, 0, len(figures)+len(dataFiles))
	for _, figure := range figures {
		path, size, status, err := resolveLocalAttachment(stack.FS.ResolveFigurePath, record.ID, figure.Filename)
		if err != nil {
			return nil, err
		}
		items = append(items, fileInventoryItem{
			RecordID:  record.ID,
			Date:      record.Date,
			ProjectID: record.ProjectID,
			Kind:      "figure",
			Filename:  figure.Filename,
			S3Key:     figure.S3Key,
			LocalSize: size,
			LocalPath: path,
			Status:    status,
		})
	}
	for _, dataFile := range dataFiles {
		path, size, status, err := resolveLocalAttachment(stack.FS.ResolveDataFilePath, record.ID, dataFile.Filename)
		if err != nil {
			return nil, err
		}
		recordedSize := dataFile.Size
		hash := dataFile.Hash
		items = append(items, fileInventoryItem{
			RecordID:     record.ID,
			Date:         record.Date,
			ProjectID:    record.ProjectID,
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

func statLocalAttachment(resolve func(string, string) (string, error), recordID string, filename string) (int64, bool, error) {
	path, err := resolve(recordID, filename)
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

func resolveLocalAttachment(resolve func(string, string) (string, error), recordID string, filename string) (string, *int64, string, error) {
	path, err := resolve(recordID, filename)
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
	return timeutil.FormatUTCMillis(t)
}

func formatCLITimePtr(t *time.Time) *string {
	return timeutil.FormatUTCMillisPtr(t)
}
