package gitsnapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const FormatVersion = 1

// Snapshot is the deterministic git-export representation of Personal Context data.
type Snapshot struct {
	Templates []Template
	Slides    []Slide
}

// Template is an exported HTML template file.
type Template struct {
	Name        string
	HTMLContent string
}

// Slide is an exported slide directory with metadata, content, and figures.
type Slide struct {
	ID           string
	Date         string
	DayOrder     string
	ProjectID    *string
	GitRemoteURL *string
	GitHash      *string
	HTMLContent  string
	Notes        *string
	Figures      []Figure
	DataFiles    []DataFile
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Figure is a metadata row plus exported file bytes.
type Figure struct {
	Filename string
	S3Key    string
	AltText  *string
	Content  []byte
}

// DataFile is exported as metadata-only; binaries stay outside git export.
type DataFile struct {
	Filename    string
	S3Key       string
	Size        int64
	Hash        string
	Description *string
}

type metadataFile struct {
	FormatVersion int          `json:"format_version"`
	ID            string       `json:"id"`
	Date          string       `json:"date"`
	DayOrder      string       `json:"day_order"`
	ProjectID     *string      `json:"project_id,omitempty"`
	GitRemoteURL  *string      `json:"git_remote_url,omitempty"`
	GitHash       *string      `json:"git_hash,omitempty"`
	HasNotes      bool         `json:"has_notes"`
	Figures       []figureFile `json:"figures"`
	DataFiles     []dataFile   `json:"data_files"`
	CreatedAt     string       `json:"created_at"`
	UpdatedAt     string       `json:"updated_at"`
}

type figureFile struct {
	Filename string  `json:"filename"`
	S3Key    string  `json:"s3_key"`
	AltText  *string `json:"alt_text"`
}

type dataFile struct {
	Filename    string  `json:"filename"`
	S3Key       string  `json:"s3_key"`
	Size        int64   `json:"size"`
	Hash        string  `json:"hash"`
	Description *string `json:"description"`
}

var lfsPointerPattern = regexp.MustCompile(`(?s)\Aversion https://git-lfs\.github\.com/spec/v1\r?\noid sha256:[0-9a-fA-F]{64}\r?\nsize [0-9]+\r?\n?\z`)

type tempFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
	Name() string
}

var (
	mkdirAllFn       = os.MkdirAll
	createTempFileFn = func(dir string, pattern string) (tempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
	removeFileFn = os.Remove
	renameFileFn = os.Rename
	chmodFileFn  = os.Chmod
)

// Write replaces the export subdirectories under root with a deterministic snapshot.
func Write(root string, snapshot Snapshot) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("root path is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create export root: %w", err)
	}
	templatesDir := filepath.Join(root, "templates")
	slidesDir := filepath.Join(root, "slides")
	if err := os.RemoveAll(templatesDir); err != nil {
		return fmt.Errorf("reset templates dir: %w", err)
	}
	if err := os.RemoveAll(slidesDir); err != nil {
		return fmt.Errorf("reset slides dir: %w", err)
	}
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		return fmt.Errorf("create templates dir: %w", err)
	}
	if err := os.MkdirAll(slidesDir, 0o755); err != nil {
		return fmt.Errorf("create slides dir: %w", err)
	}

	templates := append([]Template(nil), snapshot.Templates...)
	sort.Slice(templates, func(i, j int) bool {
		return templates[i].Name < templates[j].Name
	})
	for _, tmpl := range templates {
		if err := validatePathSegment("template name", tmpl.Name); err != nil {
			return err
		}
		path := filepath.Join(templatesDir, tmpl.Name+".html")
		if err := writeFile(path, []byte(tmpl.HTMLContent)); err != nil {
			return fmt.Errorf("write template %s: %w", tmpl.Name, err)
		}
	}

	slides := append([]Slide(nil), snapshot.Slides...)
	sort.Slice(slides, func(i, j int) bool {
		if slides[i].Date != slides[j].Date {
			return slides[i].Date < slides[j].Date
		}
		if slides[i].DayOrder != slides[j].DayOrder {
			return slides[i].DayOrder < slides[j].DayOrder
		}
		return slides[i].ID < slides[j].ID
	})
	for _, slide := range slides {
		if err := validatePathSegment("slide id", slide.ID); err != nil {
			return err
		}
		slideDir := filepath.Join(slidesDir, slide.ID)
		if err := os.MkdirAll(slideDir, 0o755); err != nil {
			return fmt.Errorf("create slide dir %s: %w", slide.ID, err)
		}
		if err := writeFile(filepath.Join(slideDir, "slide.html"), []byte(slide.HTMLContent)); err != nil {
			return fmt.Errorf("write slide.html for %s: %w", slide.ID, err)
		}
		if slide.Notes != nil {
			if err := writeFile(filepath.Join(slideDir, "notes.md"), []byte(*slide.Notes)); err != nil {
				return fmt.Errorf("write notes.md for %s: %w", slide.ID, err)
			}
		}

		figures := append([]Figure(nil), slide.Figures...)
		sort.Slice(figures, func(i, j int) bool {
			return figures[i].Filename < figures[j].Filename
		})
		figureDir := filepath.Join(slideDir, "figures")
		if len(figures) > 0 {
			if err := os.MkdirAll(figureDir, 0o755); err != nil {
				return fmt.Errorf("create figures dir for %s: %w", slide.ID, err)
			}
		}
		metadataFigures := make([]figureFile, 0, len(figures))
		for _, figure := range figures {
			if err := validatePathSegment("figure filename", figure.Filename); err != nil {
				return err
			}
			if err := writeFile(filepath.Join(figureDir, figure.Filename), figure.Content); err != nil {
				return fmt.Errorf("write figure %s/%s: %w", slide.ID, figure.Filename, err)
			}
			metadataFigures = append(metadataFigures, figureFile{
				Filename: figure.Filename,
				S3Key:    figure.S3Key,
				AltText:  figure.AltText,
			})
		}

		dataFiles := append([]DataFile(nil), slide.DataFiles...)
		sort.Slice(dataFiles, func(i, j int) bool {
			return dataFiles[i].Filename < dataFiles[j].Filename
		})
		metadataDataFiles := make([]dataFile, 0, len(dataFiles))
		for _, file := range dataFiles {
			if err := validatePathSegment("data file filename", file.Filename); err != nil {
				return err
			}
			metadataDataFiles = append(metadataDataFiles, dataFile(file))
		}

		metadataBytes, err := json.MarshalIndent(metadataFile{
			FormatVersion: FormatVersion,
			ID:            slide.ID,
			Date:          slide.Date,
			DayOrder:      slide.DayOrder,
			ProjectID:     slide.ProjectID,
			GitRemoteURL:  slide.GitRemoteURL,
			GitHash:       slide.GitHash,
			HasNotes:      slide.Notes != nil,
			Figures:       metadataFigures,
			DataFiles:     metadataDataFiles,
			CreatedAt:     slide.CreatedAt.UTC().Format(time.RFC3339Nano),
			UpdatedAt:     slide.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal metadata for %s: %w", slide.ID, err)
		}
		metadataBytes = append(metadataBytes, '\n')
		if err := writeFile(filepath.Join(slideDir, "metadata.json"), metadataBytes); err != nil {
			return fmt.Errorf("write metadata.json for %s: %w", slide.ID, err)
		}
	}

	return nil
}

// Read loads and validates a snapshot from disk.
func Read(root string) (Snapshot, error) {
	if strings.TrimSpace(root) == "" {
		return Snapshot{}, fmt.Errorf("root path is required")
	}
	templates, err := readTemplates(filepath.Join(root, "templates"))
	if err != nil {
		return Snapshot{}, err
	}
	slides, err := readSlides(filepath.Join(root, "slides"))
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Templates: templates, Slides: slides}, nil
}

// Manifest returns a deterministic listing of the snapshot tree for byte-for-byte comparisons.
func Manifest(root string) ([]string, error) {
	entries := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			entries = append(entries, "dir:"+rel)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		entries = append(entries, "file:"+rel+":"+hex.EncodeToString(sum[:])+":"+strconv.Itoa(len(data)))
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(entries)
	return entries, nil
}

func readTemplates(dir string) ([]Template, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read templates dir: %w", err)
	}
	templates := make([]Template, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			return nil, fmt.Errorf("unexpected directory in templates export: %s", entry.Name())
		}
		if filepath.Ext(entry.Name()) != ".html" {
			return nil, fmt.Errorf("template file must end with .html: %s", entry.Name())
		}
		name := strings.TrimSuffix(entry.Name(), ".html")
		if err := validatePathSegment("template name", name); err != nil {
			return nil, err
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", entry.Name(), err)
		}
		templates = append(templates, Template{
			Name:        name,
			HTMLContent: string(content),
		})
	}
	sort.Slice(templates, func(i, j int) bool {
		return templates[i].Name < templates[j].Name
	})
	return templates, nil
}

func readSlides(dir string) ([]Slide, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read slides dir: %w", err)
	}
	slides := make([]Slide, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("unexpected file in slides export: %s", entry.Name())
		}
		slide, err := readSlide(filepath.Join(dir, entry.Name()), entry.Name())
		if err != nil {
			return nil, err
		}
		slides = append(slides, slide)
	}
	sort.Slice(slides, func(i, j int) bool {
		if slides[i].Date != slides[j].Date {
			return slides[i].Date < slides[j].Date
		}
		if slides[i].DayOrder != slides[j].DayOrder {
			return slides[i].DayOrder < slides[j].DayOrder
		}
		return slides[i].ID < slides[j].ID
	})
	return slides, nil
}

func readSlide(dir string, slideID string) (Slide, error) {
	if err := validatePathSegment("slide id", slideID); err != nil {
		return Slide{}, err
	}
	metadataBytes, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return Slide{}, fmt.Errorf("read metadata for %s: %w", slideID, err)
	}
	var metadata metadataFile
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return Slide{}, fmt.Errorf("parse metadata for %s: %w", slideID, err)
	}
	if metadata.FormatVersion != FormatVersion {
		return Slide{}, fmt.Errorf("unsupported format_version %d for %s", metadata.FormatVersion, slideID)
	}
	if metadata.ID != slideID {
		return Slide{}, fmt.Errorf("metadata id %s does not match slide dir %s", metadata.ID, slideID)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, metadata.CreatedAt)
	if err != nil {
		return Slide{}, fmt.Errorf("parse created_at for %s: %w", slideID, err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, metadata.UpdatedAt)
	if err != nil {
		return Slide{}, fmt.Errorf("parse updated_at for %s: %w", slideID, err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(dir, "slide.html"))
	if err != nil {
		return Slide{}, fmt.Errorf("read slide.html for %s: %w", slideID, err)
	}
	var notes *string
	if metadata.HasNotes {
		notesBytes, err := os.ReadFile(filepath.Join(dir, "notes.md"))
		if err != nil {
			return Slide{}, fmt.Errorf("read notes.md for %s: %w", slideID, err)
		}
		value := string(notesBytes)
		notes = &value
	} else if _, err := os.Stat(filepath.Join(dir, "notes.md")); err == nil {
		return Slide{}, fmt.Errorf("notes.md present for %s despite has_notes=false", slideID)
	}

	figures, err := readFigures(dir, slideID, metadata.Figures)
	if err != nil {
		return Slide{}, err
	}
	dataFiles := make([]DataFile, 0, len(metadata.DataFiles))
	for _, file := range metadata.DataFiles {
		if err := validatePathSegment("data file filename", file.Filename); err != nil {
			return Slide{}, err
		}
		dataFiles = append(dataFiles, DataFile(file))
	}

	return Slide{
		ID:           slideID,
		Date:         metadata.Date,
		DayOrder:     metadata.DayOrder,
		ProjectID:    metadata.ProjectID,
		GitRemoteURL: metadata.GitRemoteURL,
		GitHash:      metadata.GitHash,
		HTMLContent:  string(htmlBytes),
		Notes:        notes,
		Figures:      figures,
		DataFiles:    dataFiles,
		CreatedAt:    createdAt.UTC(),
		UpdatedAt:    updatedAt.UTC(),
	}, nil
}

func readFigures(dir string, slideID string, metadata []figureFile) ([]Figure, error) {
	figures := make([]Figure, 0, len(metadata))
	figureDir := filepath.Join(dir, "figures")
	expected := make(map[string]struct{}, len(metadata))
	for _, figure := range metadata {
		if err := validatePathSegment("figure filename", figure.Filename); err != nil {
			return nil, err
		}
		expected[figure.Filename] = struct{}{}
		content, err := os.ReadFile(filepath.Join(figureDir, figure.Filename))
		if err != nil {
			return nil, fmt.Errorf("read figure %s/%s: %w", slideID, figure.Filename, err)
		}
		if isLFSPointer(content) {
			return nil, fmt.Errorf("figure %s/%s is a Git LFS pointer, not real content", slideID, figure.Filename)
		}
		figures = append(figures, Figure{
			Filename: figure.Filename,
			S3Key:    figure.S3Key,
			AltText:  figure.AltText,
			Content:  content,
		})
	}
	if len(metadata) == 0 {
		if _, err := os.Stat(figureDir); err == nil {
			entries, err := os.ReadDir(figureDir)
			if err != nil {
				return nil, fmt.Errorf("read figures dir for %s: %w", slideID, err)
			}
			if len(entries) > 0 {
				return nil, fmt.Errorf("figures dir for %s contains files not referenced by metadata", slideID)
			}
		}
		return figures, nil
	}
	entries, err := os.ReadDir(figureDir)
	if err != nil {
		return nil, fmt.Errorf("read figures dir for %s: %w", slideID, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return nil, fmt.Errorf("unexpected nested dir in figures for %s: %s", slideID, entry.Name())
		}
		if _, ok := expected[entry.Name()]; !ok {
			return nil, fmt.Errorf("figure file %s/%s not referenced by metadata", slideID, entry.Name())
		}
	}
	return figures, nil
}

func isLFSPointer(data []byte) bool {
	if len(data) > 512 {
		return false
	}
	return lfsPointerPattern.Match(data)
}

func validatePathSegment(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if value == "." || value == ".." {
		return fmt.Errorf("%s must not be %q", field, value)
	}
	if strings.ContainsAny(value, "/\\") {
		return fmt.Errorf("%s must not include path separators: %q", field, value)
	}
	if value != filepath.Base(value) {
		return fmt.Errorf("%s must not include path separators: %q", field, value)
	}
	return nil
}

func writeFile(path string, content []byte) error {
	if err := mkdirAllFn(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tempFile, err := createTempFileFn(filepath.Dir(path), ".snapshot-*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = removeFileFn(tempPath)
		}
	}()
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := renameFileFn(tempPath, path); err != nil {
		return err
	}
	cleanupTemp = false
	if err := chmodFileFn(path, 0o644); err != nil {
		return err
	}
	return nil
}
