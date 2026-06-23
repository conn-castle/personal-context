package recordio

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/notes"
)

// RecordInput holds parsed data from an input folder for pc add / pc edit.
type RecordInput struct {
	HTMLContent    *string
	Notes          *string
	ProjectID      *string
	SourceDeviceID *string
	SourceRef      *string
	GitRemoteURL   *string
	GitHash        *string
	Figures        []string // absolute paths to figure files
	DataFiles      []string // absolute paths to data files
}

// metadata is the JSON structure for metadata.json.
type metadata struct {
	ProjectID      *string `json:"project_id"`
	SourceDeviceID *string `json:"source_device_id"`
	SourceRef      *string `json:"source_ref"`
	GitRemoteURL   *string `json:"git_remote_url"`
	GitHash        *string `json:"git_hash"`
}

// figSrcPattern matches HTML figure references of the form
// src="figures/<name>" (single or double quoted). An optional query string or
// hash fragment after the name is consumed but excluded from the captured
// group, so the name matches the on-disk filename. This mirrors the web
// renderer's figure-source pattern (web/lib/record-utils.ts), which strips
// "?"/"#" suffixes before resolving the filename; keeping the two in sync
// prevents `pc add`/`pc edit` from rejecting a record whose HTML the web UI
// would render correctly.
var figSrcPattern = regexp.MustCompile(`(?i)src\s*=\s*["']figures/([^"'?#]+)(?:[?#][^"']*)?["']`)

// ParseInputFolder reads and validates a record input folder.
// metadata.json, record.html, notes.md, figures/, data/ are optional.
// Args: dir is the path to the input folder (absolute or relative).
// Returns: parsed record input or an error for missing/invalid content.
func ParseInputFolder(dir string) (RecordInput, error) {
	if strings.TrimSpace(dir) == "" {
		return RecordInput{}, fmt.Errorf("input directory is required")
	}

	info, err := os.Stat(dir)
	if err != nil {
		return RecordInput{}, fmt.Errorf("stat input directory: %w", err)
	}
	if !info.IsDir() {
		return RecordInput{}, fmt.Errorf("input path is not a directory: %s", dir)
	}

	var input RecordInput
	htmlBytes, err := os.ReadFile(filepath.Join(dir, "record.html"))
	if err == nil {
		htmlContent := string(htmlBytes)
		input.HTMLContent = &htmlContent
	} else if !errors.Is(err, os.ErrNotExist) {
		return RecordInput{}, fmt.Errorf("read record.html: %w", err)
	}

	// Parse notes.md
	notesPath := filepath.Join(dir, "notes.md")
	if notesBytes, err := os.ReadFile(notesPath); err == nil {
		input.Notes = notes.NormalizeString(string(notesBytes))
	} else if !errors.Is(err, os.ErrNotExist) {
		return RecordInput{}, fmt.Errorf("read notes.md: %w", err)
	}

	// Parse metadata.json
	metadataPath := filepath.Join(dir, "metadata.json")
	if metaBytes, err := os.ReadFile(metadataPath); err == nil {
		var meta metadata
		if err := json.Unmarshal(metaBytes, &meta); err != nil {
			return RecordInput{}, fmt.Errorf("parse metadata.json: %w", err)
		}
		input.ProjectID = meta.ProjectID
		input.SourceDeviceID = meta.SourceDeviceID
		input.SourceRef = meta.SourceRef
		input.GitRemoteURL = meta.GitRemoteURL
		input.GitHash = meta.GitHash
	} else if !errors.Is(err, os.ErrNotExist) {
		return RecordInput{}, fmt.Errorf("read metadata.json: %w", err)
	}

	// Enumerate figures
	figuresDir := filepath.Join(dir, "figures")
	if entries, err := os.ReadDir(figuresDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			input.Figures = append(input.Figures, filepath.Join(figuresDir, entry.Name()))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return RecordInput{}, fmt.Errorf("read figures directory: %w", err)
	}

	// Enumerate data files
	dataDir := filepath.Join(dir, "data")
	if entries, err := os.ReadDir(dataDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			input.DataFiles = append(input.DataFiles, filepath.Join(dataDir, entry.Name()))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return RecordInput{}, fmt.Errorf("read data directory: %w", err)
	}

	// Validate figure references in HTML
	if input.HTMLContent != nil {
		if err := validateFigureRefs(*input.HTMLContent, input.Figures); err != nil {
			return RecordInput{}, err
		}
	}

	return input, nil
}

// validateFigureRefs checks that every figures/ src in HTML has a matching file.
func validateFigureRefs(htmlContent string, figurePaths []string) error {
	figureNames := make(map[string]bool, len(figurePaths))
	for _, p := range figurePaths {
		figureNames[filepath.Base(p)] = true
	}

	matches := figSrcPattern.FindAllStringSubmatch(htmlContent, -1)
	var missing []string
	for _, match := range matches {
		name := match[1]
		if !figureNames[name] {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("HTML references missing figure files: %s", strings.Join(missing, ", "))
	}
	return nil
}

// HashFile computes the SHA-256 hex digest of a file.
// Args: path is the absolute file path.
// Returns: 64-character lowercase hex string.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for hashing: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
