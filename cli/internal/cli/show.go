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
		Short: "Display slide details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(cmd.Context(), stdout, stderr, args[0], formatFlag)
		},
	}

	cmd.Flags().StringVar(&formatFlag, "format", "text", "Output format (text|json)")

	return cmd
}

// slideJSON is the JSON representation for pc show --format json.
type slideJSON struct {
	ID           string         `json:"id"`
	Date         string         `json:"date"`
	DayOrder     string         `json:"day_order"`
	HTMLContent  string         `json:"html_content"`
	Notes        *string        `json:"notes"`
	ProjectID    *string        `json:"project_id"`
	GitRemoteURL *string        `json:"git_remote_url"`
	GitHash      *string        `json:"git_hash"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
	DeletedAt    *string        `json:"deleted_at"`
	Figures      []figureJSON   `json:"figures"`
	DataFiles    []dataFileJSON `json:"data_files"`
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

	slide, err := stack.Repo.GetSlideByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("slide %q not found", id)
		}
		return fmt.Errorf("get slide: %w", err)
	}

	figures, err := stack.Repo.ListSlideFiguresBySlideID(ctx, id)
	if err != nil {
		return fmt.Errorf("list figures: %w", err)
	}

	dataFiles, err := stack.Repo.ListSlideDataFilesBySlideID(ctx, id)
	if err != nil {
		return fmt.Errorf("list data files: %w", err)
	}

	switch format {
	case "json":
		return showJSON(stdout, slide, figures, dataFiles)
	case "text":
		return showText(stdout, slide, figures, dataFiles)
	default:
		return fmt.Errorf("unknown format %q: expected text or json", format)
	}
}

func showJSON(w io.Writer, slide repository.Slide, figures []repository.SlideFigure, dataFiles []repository.SlideDataFile) error {
	out := slideJSON{
		ID:           slide.ID,
		Date:         slide.Date,
		DayOrder:     slide.DayOrder,
		HTMLContent:  slide.HTMLContent,
		Notes:        slide.Notes,
		ProjectID:    slide.ProjectID,
		GitRemoteURL: slide.GitRemoteURL,
		GitHash:      slide.GitHash,
		CreatedAt:    slide.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		UpdatedAt:    slide.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		Figures:      make([]figureJSON, 0, len(figures)),
		DataFiles:    make([]dataFileJSON, 0, len(dataFiles)),
	}

	if slide.DeletedAt != nil {
		s := slide.DeletedAt.UTC().Format("2006-01-02T15:04:05.000Z")
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

func showText(w io.Writer, slide repository.Slide, figures []repository.SlideFigure, dataFiles []repository.SlideDataFile) error {
	_, _ = fmt.Fprintf(w, "ID:         %s\n", slide.ID)
	_, _ = fmt.Fprintf(w, "Date:       %s\n", slide.Date)
	_, _ = fmt.Fprintf(w, "DayOrder:   %s\n", slide.DayOrder)

	if slide.ProjectID != nil {
		_, _ = fmt.Fprintf(w, "Project:    %s\n", *slide.ProjectID)
	}

	if slide.DeletedAt != nil {
		_, _ = fmt.Fprintf(w, "Status:     deleted (%s)\n", slide.DeletedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}

	_, _ = fmt.Fprintf(w, "Created:    %s\n", slide.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"))
	_, _ = fmt.Fprintf(w, "Updated:    %s\n", slide.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"))

	if slide.GitRemoteURL != nil {
		_, _ = fmt.Fprintf(w, "Git Remote: %s\n", *slide.GitRemoteURL)
	}
	if slide.GitHash != nil {
		_, _ = fmt.Fprintf(w, "Git Hash:   %s\n", *slide.GitHash)
	}

	if slide.Notes != nil {
		_, _ = fmt.Fprintf(w, "Notes:      %s\n", truncate(*slide.Notes, 80))
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
