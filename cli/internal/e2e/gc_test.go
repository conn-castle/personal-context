package e2e_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// backdateDeletedAt directly updates deleted_at in the DB to simulate aging.
// It computes the past timestamp in Go to avoid clock skew between SQLite's
// 'now' and Go's time.Now() which can cause boundary tests to be flaky.
func backdateDeletedAt(t *testing.T, db *sql.DB, slideID string, daysAgo int) {
	t.Helper()
	past := time.Now().UTC().Add(-time.Duration(daysAgo) * 24 * time.Hour)
	ts := past.Format("2006-01-02T15:04:05.000Z")
	_, err := db.Exec(
		`UPDATE slides SET deleted_at = ? WHERE id = ?`,
		ts,
		slideID,
	)
	if err != nil {
		t.Fatalf("backdate deleted_at for %s: %v", slideID, err)
	}
}

func TestGCDeletesOldTrash(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/fig.png">content</html>`,
		Figures:     map[string][]byte{"fig.png": []byte("image data")},
	})
	stdout := runPCSuccess(t, homeDir, "add", folder, "--date", "2025-01-15")
	slideID := strings.TrimSpace(stdout)

	// Soft-delete, then backdate to 31 days ago.
	runPCSuccess(t, homeDir, "delete", slideID)

	db := openTestDB(t, homeDir)
	backdateDeletedAt(t, db, slideID, 31)

	// Run gc.
	gcOut := runPCSuccess(t, homeDir, "gc")
	if !strings.Contains(gcOut, slideID) {
		t.Fatalf("expected gc output to mention deleted slide %s, got:\n%s", slideID, gcOut)
	}
	if !strings.Contains(gcOut, "Removed 1 slide(s).") {
		t.Fatalf("expected summary 'Removed 1 slide(s).', got:\n%s", gcOut)
	}

	// Verify slide is gone from DB.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM slides WHERE id = ?", slideID).Scan(&count); err != nil {
		t.Fatalf("query slide count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected slide to be hard-deleted from DB, got count=%d", count)
	}

	// Verify figure file is gone from disk.
	figurePath := filepath.Join(homeDir, "personal-context", "figures", slideID, "fig.png")
	if _, err := os.Stat(figurePath); !os.IsNotExist(err) {
		t.Fatalf("expected figure file to be removed, stat err=%v", err)
	}
}

func TestGCLeavesYoungTrash(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", folder)
	slideID := strings.TrimSpace(stdout)

	// Soft-delete (0 days old).
	runPCSuccess(t, homeDir, "delete", slideID)

	// Run gc.
	gcOut := runPCSuccess(t, homeDir, "gc")
	if !strings.Contains(gcOut, "No expired trash to clean up.") {
		t.Fatalf("expected 'No expired trash to clean up.', got:\n%s", gcOut)
	}

	// Verify slide still exists.
	db := openTestDB(t, homeDir)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM slides WHERE id = ?", slideID).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected slide to still exist, got count=%d", count)
	}
}

func TestGCMixedAges(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add two slides.
	folder1 := createInputFolder(t, inputFolderOpts{HTMLContent: "<html>Old</html>"})
	id1 := strings.TrimSpace(runPCSuccess(t, homeDir, "add", folder1))

	folder2 := createInputFolder(t, inputFolderOpts{HTMLContent: "<html>Young</html>"})
	id2 := strings.TrimSpace(runPCSuccess(t, homeDir, "add", folder2))

	// Soft-delete both.
	runPCSuccess(t, homeDir, "delete", id1)
	runPCSuccess(t, homeDir, "delete", id2)

	// Backdate only id1 to 31 days ago.
	db := openTestDB(t, homeDir)
	backdateDeletedAt(t, db, id1, 31)

	// Run gc.
	gcOut := runPCSuccess(t, homeDir, "gc")
	if !strings.Contains(gcOut, id1) {
		t.Fatalf("expected gc to delete old slide %s, got:\n%s", id1, gcOut)
	}
	if strings.Contains(gcOut, id2) {
		t.Fatalf("expected gc to NOT delete young slide %s, got:\n%s", id2, gcOut)
	}

	// Verify with pc trash: only id2 should remain.
	trashOut := runPCSuccess(t, homeDir, "trash")
	if !strings.Contains(trashOut, id2) {
		t.Fatalf("expected trash to still contain young slide %s, got:\n%s", id2, trashOut)
	}
	if strings.Contains(trashOut, id1) {
		t.Fatalf("expected trash to NOT contain old slide %s, got:\n%s", id1, trashOut)
	}
}

func TestGCCascadesChildRows(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/a.png"><img src="figures/b.png">content</html>`,
		Figures:     map[string][]byte{"a.png": []byte("fig-a"), "b.png": []byte("fig-b")},
		DataFiles:   map[string][]byte{"data.csv": []byte("a,b,c")},
	})
	slideID := strings.TrimSpace(runPCSuccess(t, homeDir, "add", folder))

	// Soft-delete and backdate.
	runPCSuccess(t, homeDir, "delete", slideID)
	db := openTestDB(t, homeDir)
	backdateDeletedAt(t, db, slideID, 31)

	// Run gc.
	runPCSuccess(t, homeDir, "gc")

	// Verify child rows are gone (CASCADE).
	var figCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM slide_figures WHERE slide_id = ?", slideID).Scan(&figCount); err != nil {
		t.Fatalf("count slide_figures: %v", err)
	}
	if figCount != 0 {
		t.Fatalf("expected 0 figure rows after gc, got %d", figCount)
	}

	var dataCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM slide_data_files WHERE slide_id = ?", slideID).Scan(&dataCount); err != nil {
		t.Fatalf("count slide_data_files: %v", err)
	}
	if dataCount != 0 {
		t.Fatalf("expected 0 data file rows after gc, got %d", dataCount)
	}
}

func TestGCRemovesFigureFilesFromDisk(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/fig.png">content</html>`,
		Figures:     map[string][]byte{"fig.png": []byte("image data")},
		DataFiles:   map[string][]byte{"data.csv": []byte("x,y,z")},
	})
	slideID := strings.TrimSpace(runPCSuccess(t, homeDir, "add", folder))

	// Verify files exist before gc.
	figurePath := filepath.Join(homeDir, "personal-context", "figures", slideID, "fig.png")
	if _, err := os.Stat(figurePath); err != nil {
		t.Fatalf("expected figure file to exist before gc: %v", err)
	}
	dataPath := filepath.Join(homeDir, "personal-context", "data", slideID, "data.csv")
	if _, err := os.Stat(dataPath); err != nil {
		t.Fatalf("expected data file to exist before gc: %v", err)
	}

	// Soft-delete and backdate.
	runPCSuccess(t, homeDir, "delete", slideID)
	db := openTestDB(t, homeDir)
	backdateDeletedAt(t, db, slideID, 31)

	// Run gc.
	runPCSuccess(t, homeDir, "gc")

	// Verify files are gone.
	if _, err := os.Stat(figurePath); !os.IsNotExist(err) {
		t.Fatalf("expected figure file to be removed after gc, stat err=%v", err)
	}
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected data file to be removed after gc, stat err=%v", err)
	}
}

func TestGCVerifyWithTrashAfter(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add two slides, soft-delete both.
	folder1 := createInputFolder(t, inputFolderOpts{HTMLContent: "<html>Old</html>"})
	idOld := strings.TrimSpace(runPCSuccess(t, homeDir, "add", folder1))

	folder2 := createInputFolder(t, inputFolderOpts{HTMLContent: "<html>Young</html>"})
	idYoung := strings.TrimSpace(runPCSuccess(t, homeDir, "add", folder2))

	runPCSuccess(t, homeDir, "delete", idOld)
	runPCSuccess(t, homeDir, "delete", idYoung)

	// Backdate only the old one.
	db := openTestDB(t, homeDir)
	backdateDeletedAt(t, db, idOld, 31)

	// Run gc.
	runPCSuccess(t, homeDir, "gc")

	// Verify with trash.
	trashOut := runPCSuccess(t, homeDir, "trash")
	if strings.Contains(trashOut, idOld) {
		t.Fatalf("expected old slide %s to be gone from trash, got:\n%s", idOld, trashOut)
	}
	if !strings.Contains(trashOut, idYoung) {
		t.Fatalf("expected young slide %s to remain in trash, got:\n%s", idYoung, trashOut)
	}
}

func TestGCOnEmptyTrash(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// No deleted slides at all.
	gcOut := runPCSuccess(t, homeDir, "gc")
	if !strings.Contains(gcOut, "No expired trash to clean up.") {
		t.Fatalf("expected 'No expired trash to clean up.', got:\n%s", gcOut)
	}
}

func TestGCBoundary30Days(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder := createInputFolder(t, inputFolderOpts{})
	slideID := strings.TrimSpace(runPCSuccess(t, homeDir, "add", folder))

	// Soft-delete and backdate to 29 days and 23 hours ago.
	// This is comfortably within the 30-day window, so gc must NOT delete it.
	// We avoid using exactly 30 days because test execution time could push
	// time.Since() past the boundary.
	runPCSuccess(t, homeDir, "delete", slideID)
	db := openTestDB(t, homeDir)
	past := time.Now().UTC().Add(-(29*24*time.Hour + 23*time.Hour))
	ts := past.Format("2006-01-02T15:04:05.000Z")
	if _, err := db.Exec(`UPDATE slides SET deleted_at = ? WHERE id = ?`, ts, slideID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// Run gc -- should NOT delete (boundary is exclusive: > 30 days).
	gcOut := runPCSuccess(t, homeDir, "gc")
	if !strings.Contains(gcOut, "No expired trash to clean up.") {
		t.Fatalf("expected slide at ~30 days to NOT be deleted, got:\n%s", gcOut)
	}

	// Verify slide still exists.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM slides WHERE id = ?", slideID).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected slide to still exist at 30-day boundary, got count=%d", count)
	}
}

func TestGCLeavesActiveSlides(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add an active slide and an old deleted slide.
	folderActive := createInputFolder(t, inputFolderOpts{HTMLContent: "<html>Active</html>"})
	idActive := strings.TrimSpace(runPCSuccess(t, homeDir, "add", folderActive))

	folderDeleted := createInputFolder(t, inputFolderOpts{HTMLContent: "<html>Deleted</html>"})
	idDeleted := strings.TrimSpace(runPCSuccess(t, homeDir, "add", folderDeleted))

	// Only delete the second one.
	runPCSuccess(t, homeDir, "delete", idDeleted)
	db := openTestDB(t, homeDir)
	backdateDeletedAt(t, db, idDeleted, 31)

	// Run gc.
	gcOut := runPCSuccess(t, homeDir, "gc")
	if !strings.Contains(gcOut, idDeleted) {
		t.Fatalf("expected gc to delete old trash slide %s, got:\n%s", idDeleted, gcOut)
	}

	// Verify the active slide is untouched.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM slides WHERE id = ?", idActive).Scan(&count); err != nil {
		t.Fatalf("query active slide: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected active slide to remain, got count=%d", count)
	}

	// Verify active slide is visible via show.
	showOut := runPCSuccess(t, homeDir, "show", idActive)
	if !strings.Contains(showOut, idActive) {
		t.Fatalf("expected show to display active slide, got:\n%s", showOut)
	}
}
