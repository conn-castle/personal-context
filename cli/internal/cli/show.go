package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newShowCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var formatFlag string

	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Display record details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(cmd.Context(), stdout, stderr, args[0], formatFlag)
		},
	}

	cmd.Flags().StringVar(&formatFlag, "format", "text", "Output format (text|json)")

	return cmd
}

// recordJSON is the JSON representation for pc show --format json.
type recordJSON struct {
	ID             string         `json:"id"`
	Date           string         `json:"date"`
	DayOrder       string         `json:"day_order"`
	HTMLContent    *string        `json:"html_content"`
	Notes          *string        `json:"notes"`
	ProjectID      string         `json:"project_id"`
	SourceDeviceID string         `json:"source_device_id"`
	SourceRef      *string        `json:"source_ref"`
	GitRemoteURL   *string        `json:"git_remote_url"`
	GitHash        *string        `json:"git_hash"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	DeletedAt      *string        `json:"deleted_at"`
	Figures        []figureJSON   `json:"figures"`
	DataFiles      []dataFileJSON `json:"data_files"`
}

type figureJSON struct {
	Filename string  `json:"filename"`
	S3Key    string  `json:"s3_key"`
	AltText  *string `json:"alt_text"`
}

type dataFileJSON struct {
	Filename    string  `json:"filename"`
	S3Key       string  `json:"s3_key"`
	Size        int64   `json:"size"`
	Hash        string  `json:"hash"`
	Description *string `json:"description"`
}

func runShow(ctx context.Context, stdout io.Writer, _ io.Writer, id string, format string) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	record, err := stack.Repo.GetRecordByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("record %q not found", id)
		}
		return fmt.Errorf("get record: %w", err)
	}

	figures, err := stack.Repo.ListRecordFiguresByRecordID(ctx, id)
	if err != nil {
		return fmt.Errorf("list figures: %w", err)
	}

	dataFiles, err := stack.Repo.ListRecordDataFilesByRecordID(ctx, id)
	if err != nil {
		return fmt.Errorf("list data files: %w", err)
	}

	switch format {
	case "json":
		return showJSON(stdout, record, figures, dataFiles)
	case "text":
		return showText(stdout, record, figures, dataFiles)
	default:
		return fmt.Errorf("unknown format %q: expected text or json", format)
	}
}

func showJSON(w io.Writer, record repository.Record, figures []repository.RecordFigure, dataFiles []repository.RecordDataFile) error {
	out := recordJSON{
		ID:             record.ID,
		Date:           record.Date,
		DayOrder:       record.DayOrder,
		HTMLContent:    record.HTMLContent,
		Notes:          record.Notes,
		ProjectID:      record.ProjectID,
		SourceDeviceID: record.SourceDeviceID,
		SourceRef:      record.SourceRef,
		GitRemoteURL:   record.GitRemoteURL,
		GitHash:        record.GitHash,
		CreatedAt:      record.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		UpdatedAt:      record.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		Figures:        make([]figureJSON, 0, len(figures)),
		DataFiles:      make([]dataFileJSON, 0, len(dataFiles)),
	}

	if record.DeletedAt != nil {
		s := record.DeletedAt.UTC().Format("2006-01-02T15:04:05.000Z")
		out.DeletedAt = &s
	}

	for _, f := range figures {
		out.Figures = append(out.Figures, figureJSON{
			Filename: f.Filename,
			S3Key:    f.S3Key,
			AltText:  f.AltText,
		})
	}

	for _, d := range dataFiles {
		out.DataFiles = append(out.DataFiles, dataFileJSON{
			Filename:    d.Filename,
			S3Key:       d.S3Key,
			Size:        d.Size,
			Hash:        d.Hash,
			Description: d.Description,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func showText(w io.Writer, record repository.Record, figures []repository.RecordFigure, dataFiles []repository.RecordDataFile) error {
	_, _ = fmt.Fprintf(w, "ID:         %s\n", record.ID)
	_, _ = fmt.Fprintf(w, "Date:       %s\n", record.Date)
	_, _ = fmt.Fprintf(w, "DayOrder:   %s\n", record.DayOrder)

	_, _ = fmt.Fprintf(w, "Project:    %s\n", record.ProjectID)
	_, _ = fmt.Fprintf(w, "Device:     %s\n", record.SourceDeviceID)
	if record.SourceRef != nil {
		_, _ = fmt.Fprintf(w, "Source Ref: %s\n", *record.SourceRef)
	}

	if record.DeletedAt != nil {
		_, _ = fmt.Fprintf(w, "Status:     deleted (%s)\n", record.DeletedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}

	_, _ = fmt.Fprintf(w, "Created:    %s\n", record.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"))
	_, _ = fmt.Fprintf(w, "Updated:    %s\n", record.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"))

	if record.GitRemoteURL != nil {
		_, _ = fmt.Fprintf(w, "Git Remote: %s\n", *record.GitRemoteURL)
	}
	if record.GitHash != nil {
		_, _ = fmt.Fprintf(w, "Git Hash:   %s\n", *record.GitHash)
	}

	if record.Notes != nil {
		_, _ = fmt.Fprintf(w, "Notes:      %s\n", truncate(*record.Notes, 80))
	} else {
		_, _ = fmt.Fprintln(w, "Notes:      (none)")
	}

	_, _ = fmt.Fprintf(w, "Figures:    %d\n", len(figures))
	for _, f := range figures {
		_, _ = fmt.Fprintf(w, "  - %s\n", f.Filename)
	}

	_, _ = fmt.Fprintf(w, "Data files: %d\n", len(dataFiles))
	for _, d := range dataFiles {
		_, _ = fmt.Fprintf(w, "  - %s (%d bytes)\n", d.Filename, d.Size)
	}

	return nil
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
