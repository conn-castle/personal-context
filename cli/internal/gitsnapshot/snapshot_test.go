package gitsnapshot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWriteReadAndManifestRoundTrip(t *testing.T) {
	notes := "snapshot notes"
	altText := "alt text"
	description := "data description"
	snapshot := Snapshot{
		Templates: []Template{
			{Name: "single-image", HTMLContent: "<html>single-image</html>"},
			{Name: "text-only", HTMLContent: "<html>text-only</html>"},
		},
		Projects: []RegistryEntry{{
			ID:        "phase7/unit",
			CreatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
		}},
		Devices: []RegistryEntry{{
			ID:        "device/unit",
			CreatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
		}},
		Slides: []Slide{
			{
				ID:             "20260309-aaaabbbb",
				Date:           "2026-03-09",
				DayOrder:       "a0",
				ProjectID:      "phase7/unit",
				SourceDeviceID: "device/unit",
				GitRemoteURL:   strPtrSnapshot("https://github.com/org/repo"),
				GitHash:        strPtrSnapshot(strings.Repeat("a", 40)),
				HTMLContent:    strPtrSnapshot("<html><body><img src=\"figures/plot.png\"></body></html>"),
				Notes:          &notes,
				Figures: []Figure{{
					Filename: "plot.png",
					S3Key:    "figures/20260309-aaaabbbb/plot.png",
					AltText:  &altText,
					Content:  []byte("plot-bytes"),
				}},
				DataFiles: []DataFile{{
					Filename:    "metrics.csv",
					S3Key:       "data/20260309-aaaabbbb/metrics.csv",
					Size:        11,
					Hash:        "abc123",
					Description: &description,
				}},
				CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 123000000, time.UTC),
				UpdatedAt: time.Date(2026, 3, 9, 12, 30, 0, 456000000, time.UTC),
			},
			{
				ID:             "20260309-ccccdddd",
				Date:           "2026-03-09",
				DayOrder:       "b0",
				ProjectID:      "phase7/unit",
				SourceDeviceID: "device/unit",
				HTMLContent:    strPtrSnapshot("<html><body>minimal</body></html>"),
				Figures:        []Figure{},
				DataFiles:      []DataFile{},
				CreatedAt:      time.Date(2026, 3, 9, 13, 0, 0, 0, time.UTC),
				UpdatedAt:      time.Date(2026, 3, 9, 13, 0, 0, 0, time.UTC),
			},
		},
	}

	firstDir := t.TempDir()
	secondDir := t.TempDir()
	if err := Write(firstDir, snapshot); err != nil {
		t.Fatalf("Write(firstDir) error = %v", err)
	}
	if err := Write(secondDir, snapshot); err != nil {
		t.Fatalf("Write(secondDir) error = %v", err)
	}

	loaded, err := Read(firstDir)
	if err != nil {
		t.Fatalf("Read(firstDir) error = %v", err)
	}
	if !reflect.DeepEqual(snapshot, loaded) {
		t.Fatalf("round-trip snapshot mismatch\nwant=%#v\ngot=%#v", snapshot, loaded)
	}

	firstManifest, err := Manifest(firstDir)
	if err != nil {
		t.Fatalf("Manifest(firstDir) error = %v", err)
	}
	secondManifest, err := Manifest(secondDir)
	if err != nil {
		t.Fatalf("Manifest(secondDir) error = %v", err)
	}
	if !reflect.DeepEqual(firstManifest, secondManifest) {
		t.Fatalf("deterministic manifest mismatch\nfirst=%v\nsecond=%v", firstManifest, secondManifest)
	}

	metadataBytes, err := os.ReadFile(filepath.Join(firstDir, "slides", "20260309-aaaabbbb", "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		t.Fatalf("parse metadata.json: %v", err)
	}
	if metadata["format_version"].(float64) != 1 {
		t.Fatalf("format_version = %v, want 1", metadata["format_version"])
	}
}

func TestReadRejectsGitLFSPointerFigures(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "slides", "20260309-aaaabbbb", "figures"), 0o755); err != nil {
		t.Fatalf("mkdir slide dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "templates", "text-only.html"), []byte("<html>template</html>"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	writeDefaultSnapshotRegistries(t, root)
	metadata := `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "project_id": "phase7/unit",
  "source_device_id": "device/unit",
  "has_notes": false,
  "figures": [
    {
      "filename": "plot.png",
      "s3_key": "figures/20260309-aaaabbbb/plot.png",
      "alt_text": null
    }
  ],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`
	if err := os.WriteFile(filepath.Join(root, "slides", "20260309-aaaabbbb", "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "slides", "20260309-aaaabbbb", "slide.html"), []byte("<html><img src=\"figures/plot.png\"></html>"), 0o644); err != nil {
		t.Fatalf("write slide.html: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "slides", "20260309-aaaabbbb", "figures", "plot.png"),
		[]byte("version https://git-lfs.github.com/spec/v1\noid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\nsize 42\n"),
		0o644,
	); err != nil {
		t.Fatalf("write figure: %v", err)
	}

	_, err := Read(root)
	if err == nil {
		t.Fatal("expected Read() to reject Git LFS pointer figure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "git lfs") {
		t.Fatalf("expected git lfs error, got %v", err)
	}
}

func TestReadRejectsGitLFSPointerWithCRLF(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "slides", "20260309-aaaabbbb", "figures"), 0o755); err != nil {
		t.Fatalf("mkdir slide dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "templates", "text-only.html"), []byte("<html>template</html>"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	writeDefaultSnapshotRegistries(t, root)
	metadata := `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "project_id": "phase7/unit",
  "source_device_id": "device/unit",
  "has_notes": false,
  "figures": [
    {
      "filename": "plot.png",
      "s3_key": "figures/20260309-aaaabbbb/plot.png",
      "alt_text": null
    }
  ],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`
	if err := os.WriteFile(filepath.Join(root, "slides", "20260309-aaaabbbb", "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "slides", "20260309-aaaabbbb", "slide.html"), []byte("<html>slide</html>"), 0o644); err != nil {
		t.Fatalf("write slide.html: %v", err)
	}
	// CRLF line endings
	if err := os.WriteFile(
		filepath.Join(root, "slides", "20260309-aaaabbbb", "figures", "plot.png"),
		[]byte("version https://git-lfs.github.com/spec/v1\r\noid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\r\nsize 42\r\n"),
		0o644,
	); err != nil {
		t.Fatalf("write figure: %v", err)
	}

	_, err := Read(root)
	if err == nil {
		t.Fatal("expected Read() to reject Git LFS pointer with CRLF line endings")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "git lfs") {
		t.Fatalf("expected git lfs error, got %v", err)
	}
}

func TestWriteRejectsInvalidPathSegments(t *testing.T) {
	err := Write(t.TempDir(), Snapshot{
		Templates: []Template{{Name: "bad/name", HTMLContent: "x"}},
	})
	if err == nil {
		t.Fatal("expected invalid template name to fail")
	}

	err = Write(t.TempDir(), Snapshot{
		Slides: []Slide{{
			ID:          "bad/name",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: strPtrSnapshot("x"),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}},
	})
	if err == nil {
		t.Fatal("expected invalid slide id to fail")
	}

	// data file filename with path separator
	err = Write(t.TempDir(), Snapshot{
		Slides: []Slide{{
			ID:          "20260309-aaaabbbb",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: strPtrSnapshot("x"),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
			DataFiles: []DataFile{{
				Filename: "../escape.csv",
				S3Key:    "data/escape.csv",
				Size:     10,
				Hash:     "abcd",
			}},
		}},
	})
	if err == nil {
		t.Fatal("expected invalid data file filename to fail")
	}
	if !strings.Contains(err.Error(), "data file filename") {
		t.Fatalf("expected data file filename error, got: %v", err)
	}

	// backslash path separator (portable validation)
	err = Write(t.TempDir(), Snapshot{
		Slides: []Slide{{
			ID:          "20260309-aaaabbbb",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: strPtrSnapshot("x"),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
			DataFiles: []DataFile{{
				Filename: "dir\\file.csv",
				S3Key:    "data/file.csv",
				Size:     10,
				Hash:     "abcd",
			}},
		}},
	})
	if err == nil {
		t.Fatal("expected backslash in data file filename to fail")
	}
}

func TestReadRejectsStructuralMismatches(t *testing.T) {
	t.Run("template directory entry", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "templates", "nested"), 0o755); err != nil {
			t.Fatalf("mkdir nested: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "slides"), 0o755); err != nil {
			t.Fatalf("mkdir slides: %v", err)
		}
		if _, err := Read(root); err == nil {
			t.Fatal("expected nested template directory to fail")
		}
	})

	t.Run("unsupported format version", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 2,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if _, err := Read(root); err == nil {
			t.Fatal("expected unsupported format_version to fail")
		}
	})

	t.Run("notes present despite has_notes false", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if err := os.WriteFile(filepath.Join(root, "slides", "20260309-aaaabbbb", "notes.md"), []byte("unexpected"), 0o644); err != nil {
			t.Fatalf("write notes.md: %v", err)
		}
		if _, err := Read(root); err == nil {
			t.Fatal("expected notes mismatch to fail")
		}
	})

	t.Run("unexpected figure file", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if err := os.MkdirAll(filepath.Join(root, "slides", "20260309-aaaabbbb", "figures"), 0o755); err != nil {
			t.Fatalf("mkdir figures: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "slides", "20260309-aaaabbbb", "figures", "extra.png"), []byte("extra"), 0o644); err != nil {
			t.Fatalf("write extra figure: %v", err)
		}
		if _, err := Read(root); err == nil {
			t.Fatal("expected unexpected figure file to fail")
		}
	})

	t.Run("invalid data file filename on read", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [
    {"filename": "../escape.csv", "s3_key": "data/escape.csv", "size": 10, "hash": "abcd"}
  ],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		_, err := Read(root)
		if err == nil {
			t.Fatal("expected invalid data file filename to fail on read")
		}
		if !strings.Contains(err.Error(), "data file filename") {
			t.Fatalf("expected data file filename error, got: %v", err)
		}
	})

	t.Run("metadata id mismatch", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-wrongid",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if _, err := Read(root); err == nil {
			t.Fatal("expected metadata id mismatch to fail")
		}
	})

	t.Run("missing figure file", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [{"filename":"plot.png","s3_key":"figures/20260309-aaaabbbb/plot.png","alt_text":null}],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if err := os.MkdirAll(filepath.Join(root, "slides", "20260309-aaaabbbb", "figures"), 0o755); err != nil {
			t.Fatalf("mkdir figures: %v", err)
		}
		if _, err := Read(root); err == nil {
			t.Fatal("expected missing figure file to fail")
		}
	})
}

func TestWriteFileErrorWhenParentIsFile(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "blocked")
	if err := os.WriteFile(parent, []byte("file"), 0o644); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	if err := writeFile(filepath.Join(parent, "child.txt"), []byte("x")); err == nil {
		t.Fatal("expected writeFile to fail when parent path is a file")
	}
}

func TestInternalReadersErrorPaths(t *testing.T) {
	t.Run("readTemplates bad extension", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bad.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write bad template: %v", err)
		}
		if _, err := readTemplates(dir); err == nil {
			t.Fatal("expected readTemplates to reject non-html file")
		}
	})

	t.Run("readSlides unexpected file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "not-a-dir"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write unexpected file: %v", err)
		}
		if _, err := readSlides(dir); err == nil {
			t.Fatal("expected readSlides to reject plain files")
		}
	})

	t.Run("readSlide bad timestamp", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [],
  "created_at": "bad",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if _, err := readSlide(filepath.Join(root, "slides", "20260309-aaaabbbb"), "20260309-aaaabbbb"); err == nil {
			t.Fatal("expected readSlide to reject bad created_at")
		}
	})

	t.Run("readFigures nested dir", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [{"filename":"plot.png","s3_key":"figures/20260309-aaaabbbb/plot.png","alt_text":null}],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		figuresDir := filepath.Join(root, "slides", "20260309-aaaabbbb", "figures")
		if err := os.MkdirAll(filepath.Join(figuresDir, "nested"), 0o755); err != nil {
			t.Fatalf("mkdir nested: %v", err)
		}
		if err := os.WriteFile(filepath.Join(figuresDir, "plot.png"), []byte("plot"), 0o644); err != nil {
			t.Fatalf("write plot.png: %v", err)
		}
		if _, err := readFigures(filepath.Join(root, "slides", "20260309-aaaabbbb"), "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}}); err == nil {
			t.Fatal("expected readFigures to reject nested directories")
		}
	})
}

func strPtrSnapshot(value string) *string {
	return &value
}

func writeDefaultSnapshotRegistries(t *testing.T, root string) {
	t.Helper()
	ts := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	if err := writeRegistryFile(filepath.Join(root, "projects.json"), []RegistryEntry{{
		ID:        "phase7/unit",
		CreatedAt: ts,
		UpdatedAt: ts,
	}}); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}
	if err := writeRegistryFile(filepath.Join(root, "devices.json"), []RegistryEntry{{
		ID:        "device/unit",
		CreatedAt: ts,
		UpdatedAt: ts,
	}}); err != nil {
		t.Fatalf("write devices.json: %v", err)
	}
}

func writeMinimalSnapshotTree(t *testing.T, root string, metadata string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "slides", "20260309-aaaabbbb"), 0o755); err != nil {
		t.Fatalf("mkdir slide dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "templates", "text-only.html"), []byte("<html>template</html>"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	var metadataMap map[string]any
	if err := json.Unmarshal([]byte(metadata), &metadataMap); err != nil {
		t.Fatalf("parse metadata fixture: %v", err)
	}
	if _, ok := metadataMap["project_id"]; !ok {
		metadataMap["project_id"] = "phase7/unit"
	}
	if _, ok := metadataMap["source_device_id"]; !ok {
		metadataMap["source_device_id"] = "device/unit"
	}
	metadataBytes, err := json.MarshalIndent(metadataMap, "", "  ")
	if err != nil {
		t.Fatalf("marshal metadata fixture: %v", err)
	}
	metadataBytes = append(metadataBytes, '\n')
	if err := os.WriteFile(filepath.Join(root, "slides", "20260309-aaaabbbb", "metadata.json"), metadataBytes, 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	if err := writeRegistryFile(filepath.Join(root, "projects.json"), []RegistryEntry{{
		ID:        "phase7/unit",
		CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}
	if err := writeRegistryFile(filepath.Join(root, "devices.json"), []RegistryEntry{{
		ID:        "device/unit",
		CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("write devices.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "slides", "20260309-aaaabbbb", "slide.html"), []byte("<html>slide</html>"), 0o644); err != nil {
		t.Fatalf("write slide.html: %v", err)
	}
}

func TestWriteRejectsInvalidInputs(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)

	if err := Write("   ", Snapshot{}); err == nil {
		t.Fatal("expected Write to reject an empty root")
	}

	rootFile := filepath.Join(t.TempDir(), "snapshot-root")
	if err := os.WriteFile(rootFile, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("write root file: %v", err)
	}
	if err := Write(rootFile, Snapshot{}); err == nil {
		t.Fatal("expected Write to reject a file path as the root")
	}

	cases := []struct {
		name     string
		snapshot Snapshot
		want     string
	}{
		{
			name: "invalid template name",
			snapshot: Snapshot{
				Templates: []Template{{Name: "bad/name", HTMLContent: "<html>bad</html>"}},
			},
			want: "template name",
		},
		{
			name: "invalid slide id",
			snapshot: Snapshot{
				Slides: []Slide{{
					ID:          "bad/id",
					Date:        "2026-03-09",
					DayOrder:    "a0",
					HTMLContent: strPtrSnapshot("<html>bad</html>"),
					CreatedAt:   now,
					UpdatedAt:   now,
				}},
			},
			want: "slide id",
		},
		{
			name: "invalid figure filename",
			snapshot: Snapshot{
				Slides: []Slide{{
					ID:          "20260309-aaaabbbb",
					Date:        "2026-03-09",
					DayOrder:    "a0",
					HTMLContent: strPtrSnapshot("<html>bad</html>"),
					Figures: []Figure{{
						Filename: "bad/name.png",
						S3Key:    "figures/20260309-aaaabbbb/bad/name.png",
						Content:  []byte("bad"),
					}},
					CreatedAt: now,
					UpdatedAt: now,
				}},
			},
			want: "figure filename",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Write(t.TempDir(), tc.snapshot)
			if err == nil {
				t.Fatalf("expected Write to fail for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Write error = %v, want substring %q", err, tc.want)
			}
		})
	}

	parentFile := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parentFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocking parent file: %v", err)
	}
	if err := writeFile(filepath.Join(parentFile, "child.txt"), []byte("content")); err == nil {
		t.Fatal("expected writeFile to fail when the parent path is a file")
	}
}

func TestWriteSortsSlidesFiguresAndDataFiles(t *testing.T) {
	root := t.TempDir()
	snapshot := Snapshot{
		Slides: []Slide{
			{
				ID:             "20260310-ccccdddd",
				Date:           "2026-03-10",
				DayOrder:       "a0",
				ProjectID:      "phase7/unit",
				SourceDeviceID: "device/unit",
				HTMLContent:    strPtrSnapshot("<html>later</html>"),
				CreatedAt:      time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
				UpdatedAt:      time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
			},
			{
				ID:             "20260309-bbbbbbbb",
				Date:           "2026-03-09",
				DayOrder:       "b0",
				ProjectID:      "phase7/unit",
				SourceDeviceID: "device/unit",
				HTMLContent:    strPtrSnapshot("<html>second</html>"),
				CreatedAt:      time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
				UpdatedAt:      time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			},
			{
				ID:             "20260309-aaaaaaaa",
				Date:           "2026-03-09",
				DayOrder:       "b0",
				ProjectID:      "phase7/unit",
				SourceDeviceID: "device/unit",
				HTMLContent:    strPtrSnapshot("<html>first</html>"),
				Figures: []Figure{
					{Filename: "zeta.png", S3Key: "figures/20260309-aaaaaaaa/zeta.png", Content: []byte("zeta")},
					{Filename: "alpha.png", S3Key: "figures/20260309-aaaaaaaa/alpha.png", Content: []byte("alpha")},
				},
				DataFiles: []DataFile{
					{Filename: "zeta.csv", S3Key: "data/20260309-aaaaaaaa/zeta.csv", Size: 4, Hash: "hash-zeta"},
					{Filename: "alpha.csv", S3Key: "data/20260309-aaaaaaaa/alpha.csv", Size: 5, Hash: "hash-alpha"},
				},
				CreatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
			},
		},
	}

	if err := Write(root, snapshot); err != nil {
		t.Fatalf("Write(root) error = %v", err)
	}

	slides, err := readSlides(filepath.Join(root, "slides"))
	if err != nil {
		t.Fatalf("readSlides(slides) error = %v", err)
	}
	gotIDs := []string{slides[0].ID, slides[1].ID, slides[2].ID}
	wantIDs := []string{"20260309-aaaaaaaa", "20260309-bbbbbbbb", "20260310-ccccdddd"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("slide order = %v, want %v", gotIDs, wantIDs)
	}
	if len(slides[0].Figures) != 2 || slides[0].Figures[0].Filename != "alpha.png" || slides[0].Figures[1].Filename != "zeta.png" {
		t.Fatalf("figure order = %#v, want alpha.png then zeta.png", slides[0].Figures)
	}
	if len(slides[0].DataFiles) != 2 || slides[0].DataFiles[0].Filename != "alpha.csv" || slides[0].DataFiles[1].Filename != "zeta.csv" {
		t.Fatalf("data file order = %#v, want alpha.csv then zeta.csv", slides[0].DataFiles)
	}

	metadataBytes, err := os.ReadFile(filepath.Join(root, "slides", "20260309-aaaaaaaa", "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var metadata metadataFile
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		t.Fatalf("parse metadata.json: %v", err)
	}
	if len(metadata.Figures) != 2 || metadata.Figures[0].Filename != "alpha.png" || metadata.Figures[1].Filename != "zeta.png" {
		t.Fatalf("metadata figures = %#v, want sorted by filename", metadata.Figures)
	}
	if len(metadata.DataFiles) != 2 || metadata.DataFiles[0].Filename != "alpha.csv" || metadata.DataFiles[1].Filename != "zeta.csv" {
		t.Fatalf("metadata data_files = %#v, want sorted by filename", metadata.DataFiles)
	}
}

func TestRegistryFilesReadWriteAndValidation(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	archived := now.Add(time.Hour)
	entries := []RegistryEntry{
		{ID: "zeta", CreatedAt: now, UpdatedAt: archived, ArchivedAt: &archived},
		{ID: "alpha", CreatedAt: now, UpdatedAt: now},
	}
	path := filepath.Join(root, "projects.json")
	if err := writeRegistryFile(path, entries); err != nil {
		t.Fatalf("writeRegistryFile() error = %v", err)
	}
	loaded, err := readRegistryFile(path)
	if err != nil {
		t.Fatalf("readRegistryFile() error = %v", err)
	}
	if len(loaded) != 2 || loaded[0].ID != "alpha" || loaded[1].ArchivedAt == nil {
		t.Fatalf("loaded registry entries = %#v", loaded)
	}

	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{name: "malformed", content: "{not-json}\n", want: "invalid character"},
		{name: "empty id", content: `[{"id":"","created_at":"2026-03-09T12:00:00Z","updated_at":"2026-03-09T12:00:00Z"}]`, want: "id is required"},
		{name: "bad created", content: `[{"id":"x","created_at":"bad","updated_at":"2026-03-09T12:00:00Z"}]`, want: "parse created_at"},
		{name: "bad updated", content: `[{"id":"x","created_at":"2026-03-09T12:00:00Z","updated_at":"bad"}]`, want: "parse updated_at"},
		{name: "bad archived", content: `[{"id":"x","created_at":"2026-03-09T12:00:00Z","updated_at":"2026-03-09T12:00:00Z","archived_at":"bad"}]`, want: "parse archived_at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testPath := filepath.Join(t.TempDir(), "devices.json")
			if err := os.WriteFile(testPath, []byte(tc.content), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			_, err := readRegistryFile(testPath)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("readRegistryFile() error = %v, want %q", err, tc.want)
			}
		})
	}

	if err := writeRegistryFile(filepath.Join(t.TempDir(), "bad.json"), []RegistryEntry{{CreatedAt: now, UpdatedAt: now}}); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("expected writeRegistryFile to reject empty ID, got %v", err)
	}
	if err := writeRegistryFile(filepath.Join(t.TempDir(), "bad.json"), []RegistryEntry{{ID: "bad", CreatedAt: time.Time{}, UpdatedAt: now}}); err == nil || !strings.Contains(err.Error(), "must have created_at and updated_at") {
		t.Fatalf("expected writeRegistryFile to reject missing timestamp, got %v", err)
	}
	missingRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(missingRoot, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(missingRoot, "slides"), 0o755); err != nil {
		t.Fatalf("mkdir slides: %v", err)
	}
	if _, err := Read(missingRoot); err == nil || !strings.Contains(err.Error(), "read projects.json") {
		t.Fatalf("expected missing projects.json read error, got %v", err)
	}
	if err := writeRegistryFile(filepath.Join(missingRoot, "projects.json"), []RegistryEntry{{ID: "phase7/unit", CreatedAt: now, UpdatedAt: now}}); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}
	if _, err := Read(missingRoot); err == nil || !strings.Contains(err.Error(), "read devices.json") {
		t.Fatalf("expected missing devices.json read error, got %v", err)
	}
}

func TestWriteRejectsFilesystemFailures(t *testing.T) {
	t.Run("reset templates dir", func(t *testing.T) {
		root := t.TempDir()
		mustWriteFileSnapshot(t, filepath.Join(root, "templates", "old.html"), "<html>old</html>")
		if err := os.Chmod(root, 0o555); err != nil {
			t.Fatalf("chmod root: %v", err)
		}
		defer func() {
			if err := os.Chmod(root, 0o755); err != nil {
				t.Fatalf("restore root permissions: %v", err)
			}
		}()
		if err := Write(root, Snapshot{}); err == nil || !strings.Contains(err.Error(), "reset templates dir") {
			t.Fatalf("Write(root) error = %v, want reset templates dir failure", err)
		}
	})

	t.Run("reset slides dir", func(t *testing.T) {
		root := t.TempDir()
		mustWriteFileSnapshot(t, filepath.Join(root, "slides", "existing", "metadata.json"), "{}\n")
		if err := os.Chmod(root, 0o555); err != nil {
			t.Fatalf("chmod root: %v", err)
		}
		defer func() {
			if err := os.Chmod(root, 0o755); err != nil {
				t.Fatalf("restore root permissions: %v", err)
			}
		}()
		if err := Write(root, Snapshot{}); err == nil || !strings.Contains(err.Error(), "reset slides dir") {
			t.Fatalf("Write(root) error = %v, want reset slides dir failure", err)
		}
	})

	t.Run("create templates dir", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o555); err != nil {
			t.Fatalf("chmod root: %v", err)
		}
		defer func() {
			if err := os.Chmod(root, 0o755); err != nil {
				t.Fatalf("restore root permissions: %v", err)
			}
		}()
		if err := Write(root, Snapshot{}); err == nil || !strings.Contains(err.Error(), "create templates dir") {
			t.Fatalf("Write(root) error = %v, want create templates dir failure", err)
		}
	})

	t.Run("write template", func(t *testing.T) {
		root := t.TempDir()
		templateName := strings.Repeat("t", 251)
		err := Write(root, Snapshot{
			Templates: []Template{{Name: templateName, HTMLContent: "<html>too long</html>"}},
		})
		if err == nil || !strings.Contains(err.Error(), "write template") {
			t.Fatalf("Write(root) error = %v, want write template failure", err)
		}
	})

	t.Run("create slide dir", func(t *testing.T) {
		root := t.TempDir()
		err := Write(root, Snapshot{
			Slides: []Slide{{
				ID:          strings.Repeat("s", 256),
				Date:        "2026-03-09",
				DayOrder:    "a0",
				HTMLContent: strPtrSnapshot("<html>too long</html>"),
				CreatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
				UpdatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "create slide dir") {
			t.Fatalf("Write(root) error = %v, want create slide dir failure", err)
		}
	})

	t.Run("write figure", func(t *testing.T) {
		root := t.TempDir()
		err := Write(root, Snapshot{
			Slides: []Slide{{
				ID:          "20260309-aaaabbbb",
				Date:        "2026-03-09",
				DayOrder:    "a0",
				HTMLContent: strPtrSnapshot("<html>figure</html>"),
				Figures: []Figure{{
					Filename: strings.Repeat("f", 256),
					S3Key:    "figures/20260309-aaaabbbb/too-long",
					Content:  []byte("figure-bytes"),
				}},
				CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "write figure") {
			t.Fatalf("Write(root) error = %v, want write figure failure", err)
		}
	})
}

func TestReadRejectsTemplateDirectoryProblems(t *testing.T) {
	if _, err := Read(" "); err == nil {
		t.Fatal("expected Read to reject an empty root")
	}

	if _, err := readTemplates(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected readTemplates to fail for a missing directory")
	}

	t.Run("rejects nested directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "templates")
		if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
			t.Fatalf("mkdir nested dir: %v", err)
		}
		if _, err := readTemplates(dir); err == nil || !strings.Contains(err.Error(), "unexpected directory") {
			t.Fatalf("readTemplates error = %v, want unexpected directory", err)
		}
	})

	t.Run("rejects non-html template file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "templates")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir templates dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "text-only.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write bad template file: %v", err)
		}
		if _, err := readTemplates(dir); err == nil || !strings.Contains(err.Error(), ".html") {
			t.Fatalf("readTemplates error = %v, want .html failure", err)
		}
	})

	t.Run("rejects invalid template path segment", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "templates")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir templates dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "..html"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write invalid template file: %v", err)
		}
		if _, err := readTemplates(dir); err == nil || !strings.Contains(err.Error(), "template name") {
			t.Fatalf("readTemplates error = %v, want template name validation failure", err)
		}
	})

	t.Run("rejects unreadable template target", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "templates")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir templates dir: %v", err)
		}
		if err := os.Symlink(filepath.Join(dir, "missing-target.html"), filepath.Join(dir, "broken.html")); err != nil {
			t.Fatalf("create broken template symlink: %v", err)
		}
		if _, err := readTemplates(dir); err == nil || !strings.Contains(err.Error(), "read template broken.html") {
			t.Fatalf("readTemplates error = %v, want read failure for broken.html", err)
		}
	})
}

func TestReadSlidesRejectsUnexpectedFile(t *testing.T) {
	t.Run("missing slides dir", func(t *testing.T) {
		if _, err := readSlides(filepath.Join(t.TempDir(), "missing")); err == nil || !strings.Contains(err.Error(), "read slides dir") {
			t.Fatalf("readSlides error = %v, want missing dir failure", err)
		}
	})

	t.Run("unexpected file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "slides")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir slides dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "not-a-slide.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write unexpected slide file: %v", err)
		}
		if _, err := readSlides(dir); err == nil || !strings.Contains(err.Error(), "unexpected file in slides export") {
			t.Fatalf("readSlides error = %v, want unexpected file failure", err)
		}
	})
}

func TestReadSlideRejectsMetadataAndNotesProblems(t *testing.T) {
	t.Run("invalid slide id", func(t *testing.T) {
		_, slideDir := writeSnapshotFixture(t)
		if _, err := readSlide(slideDir, "bad/id"); err == nil || !strings.Contains(err.Error(), "slide id") {
			t.Fatalf("readSlide error = %v, want slide id validation failure", err)
		}
	})

	t.Run("missing metadata file", func(t *testing.T) {
		_, slideDir := writeSnapshotFixture(t)
		if err := os.Remove(filepath.Join(slideDir, "metadata.json")); err != nil {
			t.Fatalf("remove metadata.json: %v", err)
		}
		if _, err := readSlide(slideDir, filepath.Base(slideDir)); err == nil || !strings.Contains(err.Error(), "read metadata") {
			t.Fatalf("readSlide error = %v, want metadata read failure", err)
		}
	})

	t.Run("malformed metadata json", func(t *testing.T) {
		_, slideDir := writeSnapshotFixture(t)
		mustWriteFileSnapshot(t, filepath.Join(slideDir, "metadata.json"), "{not-json}\n")
		if _, err := readSlide(slideDir, filepath.Base(slideDir)); err == nil || !strings.Contains(err.Error(), "parse metadata") {
			t.Fatalf("readSlide error = %v, want metadata parse failure", err)
		}
	})

	t.Run("unsupported format version", func(t *testing.T) {
		root, slideDir := writeSnapshotFixture(t)
		writeMetadataJSON(t, slideDir, map[string]any{
			"format_version": 2,
			"id":             "20260309-aaaabbbb",
			"date":           "2026-03-09",
			"day_order":      "a0",
			"has_notes":      true,
			"figures":        []map[string]any{},
			"data_files":     []map[string]any{},
			"created_at":     "2026-03-09T12:00:00Z",
			"updated_at":     "2026-03-09T12:00:00Z",
		})
		_, err := readSlide(slideDir, filepath.Base(slideDir))
		if err == nil || !strings.Contains(err.Error(), "unsupported format_version") {
			t.Fatalf("readSlide(%s) error = %v", root, err)
		}
	})

	t.Run("metadata id mismatch", func(t *testing.T) {
		_, slideDir := writeSnapshotFixture(t)
		writeMetadataJSON(t, slideDir, map[string]any{
			"format_version": 1,
			"id":             "20260309-ccccdddd",
			"date":           "2026-03-09",
			"day_order":      "a0",
			"has_notes":      true,
			"figures":        []map[string]any{},
			"data_files":     []map[string]any{},
			"created_at":     "2026-03-09T12:00:00Z",
			"updated_at":     "2026-03-09T12:00:00Z",
		})
		_, err := readSlide(slideDir, filepath.Base(slideDir))
		if err == nil || !strings.Contains(err.Error(), "does not match slide dir") {
			t.Fatalf("readSlide error = %v, want metadata id mismatch", err)
		}
	})

	t.Run("invalid timestamps", func(t *testing.T) {
		_, slideDir := writeSnapshotFixture(t)
		writeMetadataJSON(t, slideDir, map[string]any{
			"format_version": 1,
			"id":             "20260309-aaaabbbb",
			"date":           "2026-03-09",
			"day_order":      "a0",
			"has_notes":      true,
			"figures": []map[string]any{{
				"filename": "plot.png",
				"s3_key":   "figures/20260309-aaaabbbb/plot.png",
				"alt_text": nil,
			}},
			"data_files": []map[string]any{},
			"created_at": "not-a-time",
			"updated_at": "still-not-a-time",
		})
		_, err := readSlide(slideDir, filepath.Base(slideDir))
		if err == nil || !strings.Contains(err.Error(), "parse created_at") {
			t.Fatalf("readSlide error = %v, want created_at parse failure", err)
		}
	})

	t.Run("invalid updated_at", func(t *testing.T) {
		_, slideDir := writeSnapshotFixture(t)
		writeMetadataJSON(t, slideDir, map[string]any{
			"format_version": 1,
			"id":             "20260309-aaaabbbb",
			"date":           "2026-03-09",
			"day_order":      "a0",
			"has_notes":      true,
			"figures": []map[string]any{{
				"filename": "plot.png",
				"s3_key":   "figures/20260309-aaaabbbb/plot.png",
				"alt_text": nil,
			}},
			"data_files": []map[string]any{},
			"created_at": "2026-03-09T12:00:00Z",
			"updated_at": "not-a-time",
		})
		if _, err := readSlide(slideDir, filepath.Base(slideDir)); err == nil || !strings.Contains(err.Error(), "parse updated_at") {
			t.Fatalf("readSlide error = %v, want updated_at parse failure", err)
		}
	})

	t.Run("missing slide html is null", func(t *testing.T) {
		_, slideDir := writeSnapshotFixture(t)
		if err := os.Remove(filepath.Join(slideDir, "slide.html")); err != nil {
			t.Fatalf("remove slide.html: %v", err)
		}
		slide, err := readSlide(slideDir, filepath.Base(slideDir))
		if err != nil {
			t.Fatalf("readSlide error = %v", err)
		}
		if slide.HTMLContent != nil {
			t.Fatalf("HTMLContent = %q, want nil", *slide.HTMLContent)
		}
	})

	t.Run("notes required when has_notes is true", func(t *testing.T) {
		_, slideDir := writeSnapshotFixture(t)
		if err := os.Remove(filepath.Join(slideDir, "notes.md")); err != nil {
			t.Fatalf("remove notes.md: %v", err)
		}
		_, err := readSlide(slideDir, filepath.Base(slideDir))
		if err == nil || !strings.Contains(err.Error(), "read notes.md") {
			t.Fatalf("readSlide error = %v, want missing notes failure", err)
		}
	})

	t.Run("notes forbidden when has_notes is false", func(t *testing.T) {
		_, slideDir := writeSnapshotFixture(t)
		writeMetadataJSON(t, slideDir, map[string]any{
			"format_version": 1,
			"id":             "20260309-aaaabbbb",
			"date":           "2026-03-09",
			"day_order":      "a0",
			"has_notes":      false,
			"figures": []map[string]any{{
				"filename": "plot.png",
				"s3_key":   "figures/20260309-aaaabbbb/plot.png",
				"alt_text": nil,
			}},
			"data_files": []map[string]any{},
			"created_at": "2026-03-09T12:00:00Z",
			"updated_at": "2026-03-09T12:00:00Z",
		})
		_, err := readSlide(slideDir, filepath.Base(slideDir))
		if err == nil || !strings.Contains(err.Error(), "notes.md present") {
			t.Fatalf("readSlide error = %v, want forbidden notes failure", err)
		}
	})
}

func TestReadFiguresRejectsDirectoryAndPointerProblems(t *testing.T) {
	t.Run("invalid figure filename", func(t *testing.T) {
		root := t.TempDir()
		if _, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "bad/name.png",
			S3Key:    "figures/20260309-aaaabbbb/bad/name.png",
		}}); err == nil || !strings.Contains(err.Error(), "figure filename") {
			t.Fatalf("readFigures error = %v, want figure filename validation failure", err)
		}
	})

	t.Run("missing figure file", func(t *testing.T) {
		root := t.TempDir()
		if _, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}}); err == nil || !strings.Contains(err.Error(), "read figure") {
			t.Fatalf("readFigures error = %v, want missing figure failure", err)
		}
	})

	t.Run("extra figure file", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "slide")
		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(figuresDir, 0o755); err != nil {
			t.Fatalf("mkdir figures dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "plot.png"), "plot")
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "extra.png"), "extra")
		_, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}})
		if err == nil || !strings.Contains(err.Error(), "not referenced by metadata") {
			t.Fatalf("readFigures error = %v, want unreferenced file failure", err)
		}
	})

	t.Run("nested figure directory", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "slide")
		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(filepath.Join(figuresDir, "nested"), 0o755); err != nil {
			t.Fatalf("mkdir nested figures dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "plot.png"), "plot")
		_, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}})
		if err == nil || !strings.Contains(err.Error(), "unexpected nested dir") {
			t.Fatalf("readFigures error = %v, want nested dir failure", err)
		}
	})

	t.Run("unexpected figures when metadata is empty", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "slide")
		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(figuresDir, 0o755); err != nil {
			t.Fatalf("mkdir figures dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "extra.png"), "extra")
		if _, err := readFigures(root, "20260309-aaaabbbb", nil); err == nil || !strings.Contains(err.Error(), "contains files not referenced") {
			t.Fatalf("readFigures error = %v, want unexpected figures failure", err)
		}
	})

	t.Run("missing figures dir is allowed when metadata is empty", func(t *testing.T) {
		root := t.TempDir()
		figures, err := readFigures(root, "20260309-aaaabbbb", nil)
		if err != nil {
			t.Fatalf("readFigures error = %v, want nil", err)
		}
		if len(figures) != 0 {
			t.Fatalf("readFigures returned %d figures, want 0", len(figures))
		}
	})

	t.Run("metadata empty but figures path is not a directory", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "slide")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("mkdir slide dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(root, "figures"), "not-a-directory")
		if _, err := readFigures(root, "20260309-aaaabbbb", nil); err == nil || !strings.Contains(err.Error(), "read figures dir") {
			t.Fatalf("readFigures error = %v, want read figures dir failure", err)
		}
	})

	t.Run("metadata present but figure dir cannot be listed", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "slide")
		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(figuresDir, 0o755); err != nil {
			t.Fatalf("mkdir figures dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "plot.png"), "plot")
		if err := os.Chmod(figuresDir, 0o111); err != nil {
			t.Fatalf("chmod figures dir: %v", err)
		}
		defer func() {
			if err := os.Chmod(figuresDir, 0o755); err != nil {
				t.Fatalf("restore figures dir permissions: %v", err)
			}
		}()
		if _, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}}); err == nil || !strings.Contains(err.Error(), "read figures dir") {
			t.Fatalf("readFigures error = %v, want read figures dir failure", err)
		}
	})

	t.Run("rejects lfs pointer", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "slide")
		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(figuresDir, 0o755); err != nil {
			t.Fatalf("mkdir figures dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "plot.png"), "version https://git-lfs.github.com/spec/v1\noid sha256:DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF\nsize 42\n")
		_, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "git lfs") {
			t.Fatalf("readFigures error = %v, want git lfs failure", err)
		}
	})
}

func TestReadPropagatesSlidesDirFailureAfterTemplatesLoad(t *testing.T) {
	root := t.TempDir()
	templatesDir := filepath.Join(root, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("mkdir templates dir: %v", err)
	}
	mustWriteFileSnapshot(t, filepath.Join(templatesDir, "text-only.html"), "<html>template</html>")

	if _, err := Read(root); err == nil || !strings.Contains(err.Error(), "read slides dir") {
		t.Fatalf("Read(root) error = %v, want slides dir failure", err)
	}
}

func TestHelperFunctionsRejectInvalidInputs(t *testing.T) {
	t.Run("manifest rejects unreadable file target", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Symlink(filepath.Join(root, "missing.txt"), filepath.Join(root, "broken.txt")); err != nil {
			t.Fatalf("create broken manifest symlink: %v", err)
		}
		if _, err := Manifest(root); err == nil {
			t.Fatal("expected Manifest to fail for a broken symlink")
		}
	})

	t.Run("manifest missing root", func(t *testing.T) {
		if _, err := Manifest(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("expected Manifest to fail for a missing root")
		}
	})

	t.Run("validatePathSegment", func(t *testing.T) {
		cases := []struct {
			value string
			want  string
		}{
			{value: "", want: "required"},
			{value: ".", want: "must not be"},
			{value: "..", want: "must not be"},
			{value: "nested/name", want: "path separators"},
		}
		for _, tc := range cases {
			if err := validatePathSegment("field", tc.value); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validatePathSegment(%q) error = %v, want %q", tc.value, err, tc.want)
			}
		}
		if err := validatePathSegment("field", "valid-name"); err != nil {
			t.Fatalf("validatePathSegment(valid-name) error = %v", err)
		}
	})

	t.Run("isLFSPointer", func(t *testing.T) {
		if !isLFSPointer([]byte("version https://git-lfs.github.com/spec/v1\noid sha256:ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789\nsize 42\n")) {
			t.Fatal("expected uppercase Git LFS pointer to be detected")
		}
		if isLFSPointer([]byte(strings.Repeat("x", 513))) {
			t.Fatal("expected oversized content not to be treated as a Git LFS pointer")
		}
	})

	t.Run("writeFile create temp failure", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "blocked")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir blocked dir: %v", err)
		}
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatalf("chmod blocked dir: %v", err)
		}
		defer func() {
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatalf("restore blocked dir permissions: %v", err)
			}
		}()
		if err := writeFile(filepath.Join(dir, "child.txt"), []byte("content")); err == nil {
			t.Fatal("expected writeFile to fail when CreateTemp cannot write")
		}
	})

	t.Run("writeFile cleans up temp file after rename failure", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		if err := os.MkdirAll(filepath.Join(target, "nested"), 0o755); err != nil {
			t.Fatalf("mkdir target dir: %v", err)
		}
		if err := writeFile(target, []byte("content")); err == nil {
			t.Fatal("expected writeFile to fail when rename target is a directory")
		}
		entries, err := filepath.Glob(filepath.Join(root, ".snapshot-*.tmp"))
		if err != nil {
			t.Fatalf("glob temp files: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("temp files remaining after rename failure = %v, want none", entries)
		}
	})
}

func TestWriteFaultInjectionErrorPaths(t *testing.T) {
	origCreateTemp := createTempFileFn
	origRename := renameFileFn
	origRemove := removeFileFn
	origChmod := chmodFileFn
	t.Cleanup(func() {
		createTempFileFn = origCreateTemp
		renameFileFn = origRename
		removeFileFn = origRemove
		chmodFileFn = origChmod
	})

	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	baseSnapshot := Snapshot{
		Templates: []Template{{Name: "text-only", HTMLContent: "<html>template</html>"}},
		Slides: []Slide{{
			ID:          "20260309-aaaabbbb",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: strPtrSnapshot("<html>slide</html>"),
			CreatedAt:   now,
			UpdatedAt:   now,
		}},
	}

	t.Run("template write failure", func(t *testing.T) {
		createTempFileFn = func(string, string) (tempFile, error) {
			return &stubTempFile{name: filepath.Join(t.TempDir(), "template.tmp"), writeErr: errors.New("write failed")}, nil
		}

		err := Write(t.TempDir(), baseSnapshot)
		if err == nil || !strings.Contains(err.Error(), "write template text-only") {
			t.Fatalf("Write() error = %v, want wrapped template write failure", err)
		}
	})

	t.Run("metadata sync failure", func(t *testing.T) {
		createTempFileFn = func(string, string) (tempFile, error) {
			return &stubTempFile{name: filepath.Join(t.TempDir(), "metadata.tmp"), syncErr: errors.New("sync failed")}, nil
		}

		err := Write(t.TempDir(), baseSnapshot)
		if err == nil || !strings.Contains(err.Error(), "write template text-only") {
			t.Fatalf("Write() error = %v, want wrapped sync failure", err)
		}
	})

	t.Run("slide close failure", func(t *testing.T) {
		createTempFileFn = func(_ string, _ string) (tempFile, error) {
			return &stubTempFile{name: filepath.Join(t.TempDir(), "slide.tmp"), closeErr: errors.New("close failed")}, nil
		}

		err := Write(t.TempDir(), Snapshot{
			Slides: []Slide{{
				ID:          "20260309-beadfeed",
				Date:        "2026-03-09",
				DayOrder:    "a0",
				HTMLContent: strPtrSnapshot("<html>slide</html>"),
				CreatedAt:   now,
				UpdatedAt:   now,
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "write slide.html for 20260309-beadfeed") {
			t.Fatalf("Write() error = %v, want wrapped slide close failure", err)
		}
	})

	t.Run("rename failure removes temp file", func(t *testing.T) {
		tempDir := t.TempDir()
		tempPath := filepath.Join(tempDir, ".snapshot-rename.tmp")
		if err := os.WriteFile(tempPath, []byte("stale"), 0o644); err != nil {
			t.Fatalf("write temp path: %v", err)
		}

		createTempFileFn = func(string, string) (tempFile, error) {
			file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				t.Fatalf("open temp path: %v", err)
			}
			return file, nil
		}
		renameFileFn = func(string, string) error {
			return errors.New("rename failed")
		}

		err := Write(tempDir, Snapshot{
			Slides: []Slide{{
				ID:          "20260309-feedface",
				Date:        "2026-03-09",
				DayOrder:    "a0",
				HTMLContent: strPtrSnapshot("<html>slide</html>"),
				CreatedAt:   now,
				UpdatedAt:   now,
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "write slide.html for 20260309-feedface") {
			t.Fatalf("Write() error = %v, want wrapped rename failure", err)
		}
		if _, statErr := os.Stat(tempPath); !os.IsNotExist(statErr) {
			t.Fatalf("expected temp file cleanup after rename failure, stat err = %v", statErr)
		}
	})

	t.Run("chmod failure after rename", func(t *testing.T) {
		createTempFileFn = origCreateTemp
		renameFileFn = origRename
		chmodFileFn = func(string, os.FileMode) error {
			return errors.New("chmod failed")
		}

		err := Write(t.TempDir(), baseSnapshot)
		if err == nil || !strings.Contains(err.Error(), "chmod failed") {
			t.Fatalf("Write() error = %v, want chmod failure", err)
		}
	})
}

type stubTempFile struct {
	name     string
	writeErr error
	syncErr  error
	closeErr error
}

func (s *stubTempFile) Write(p []byte) (int, error) {
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return len(p), nil
}

func (s *stubTempFile) Sync() error {
	return s.syncErr
}

func (s *stubTempFile) Close() error {
	return s.closeErr
}

func (s *stubTempFile) Name() string {
	return s.name
}

func writeSnapshotFixture(t *testing.T) (root string, slideDir string) {
	t.Helper()

	root = t.TempDir()
	notes := "fixture notes"
	snapshot := Snapshot{
		Templates: []Template{{Name: "text-only", HTMLContent: "<html>template</html>"}},
		Slides: []Slide{{
			ID:             "20260309-aaaabbbb",
			Date:           "2026-03-09",
			DayOrder:       "a0",
			ProjectID:      "phase7/unit",
			SourceDeviceID: "device/unit",
			HTMLContent:    strPtrSnapshot("<html><body><img src=\"figures/plot.png\"></body></html>"),
			Notes:          &notes,
			Figures: []Figure{{
				Filename: "plot.png",
				S3Key:    "figures/20260309-aaaabbbb/plot.png",
				Content:  []byte("plot-bytes"),
			}},
			CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		}},
	}
	if err := Write(root, snapshot); err != nil {
		t.Fatalf("Write(root) error = %v", err)
	}
	return root, filepath.Join(root, "slides", "20260309-aaaabbbb")
}

func writeMetadataJSON(t *testing.T, slideDir string, metadata map[string]any) {
	t.Helper()
	if _, ok := metadata["project_id"]; !ok {
		metadata["project_id"] = "phase7/unit"
	}
	if _, ok := metadata["source_device_id"]; !ok {
		metadata["source_device_id"] = "device/unit"
	}

	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(slideDir, "metadata.json"), raw, 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
}

func mustWriteFileSnapshot(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
