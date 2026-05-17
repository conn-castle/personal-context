//go:build integration

package cloude2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTwoHomeRoundTripWithAutoSyncConflictResolution(t *testing.T) {
	cloud := newCloudTestEnv(t)

	// Set up two independent homes against the same cloud.
	// First home applies the Postgres schema; second home reuses it.
	homeA, userHomeA := setupCloudHome(t, cloud)
	homeB, userHomeB := setupCloudHomeNoSchema(t, cloud)

	const (
		originalHTML   = "<html>HomeA original</html>"
		originalNotes  = "Original notes from A"
		originalFigure = "FIGURE_CONTENT_A"
		originalData   = "a,b,c\n1,2,3\n"
		homeAHTML      = "<html>HomeA edit</html>"
		homeANotes     = "Edited by A"
		homeAFigure    = "FIGURE_CONTENT_A_EDITED"
		homeAData      = "edited,by\nA,1\n"
		homeBHTML      = "<html>HomeB edit (later)</html>"
		homeBNotes     = "Edited by B"
		homeBFigure    = "FIGURE_CONTENT_B_LATER"
		homeBData      = "edited,by\nB,2\n"
	)

	// --- Phase 1: Create a record with a figure and a data file on homeA ---
	inputDir := createInputFolder(t,
		originalHTML,
		originalNotes,
		map[string][]byte{"plot.png": []byte(originalFigure)},
		map[string][]byte{"metrics.csv": []byte(originalData)},
	)
	recordID := strings.TrimSpace(runPCSuccessNoStderr(t, homeA, userHomeA, "records", "add", inputDir))
	if recordID == "" {
		t.Fatal("expected record ID from add")
	}

	// HomeA add auto-syncs to cloud; homeB only needs an explicit sync to pull it.
	syncOut := runPCSuccessNoStderr(t, homeB, userHomeB, "sync")
	if !strings.Contains(syncOut, "Sync complete") {
		t.Fatalf("expected sync success output, got:\n%s", syncOut)
	}

	recordB := getRecordJSON(t, homeB, userHomeB, recordID)
	if recordB.HTMLContent != originalHTML {
		t.Fatalf("homeB record content = %q, want HomeA original", recordB.HTMLContent)
	}
	if recordB.Notes != originalNotes {
		t.Fatalf("homeB notes = %q, want %q", recordB.Notes, originalNotes)
	}

	// Figure should be present locally on homeB after sync.
	figurePath := filepath.Join(homeB, "personal-context", "figures", recordID, "plot.png")
	figureContent, err := os.ReadFile(figurePath)
	if err != nil {
		t.Fatalf("read figure on homeB: %v", err)
	}
	if string(figureContent) != originalFigure {
		t.Fatalf("homeB figure = %q, want %q", string(figureContent), originalFigure)
	}

	// Data file should remain cloud-only (not downloaded by sync).
	dataPath := filepath.Join(homeB, "personal-context", "data", recordID, "metrics.csv")
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected data file to be cloud-only on homeB, stat err = %v", err)
	}

	// --- Phase 3: Edit on homeA. Auto-sync should push without an explicit sync command. ---
	editDirA := createInputFolder(t, homeAHTML, homeANotes,
		map[string][]byte{"plot.png": []byte(homeAFigure)},
		map[string][]byte{"metrics.csv": []byte(homeAData)},
	)
	editOutA := runPCSuccessNoStderr(t, homeA, userHomeA, "records", "edit", recordID, editDirA)
	if !strings.Contains(editOutA, "Record "+recordID+" updated") {
		t.Fatalf("expected homeA edit success output, got:\n%s", editOutA)
	}

	// --- Phase 4: Edit the same stale record on homeB. Its auto-sync should win. ---
	// Backdate homeA's cloud timestamp to ensure homeB's edit is deterministically later.
	backdateRecordCloud(t, cloud.NeonURL, recordID)
	editDirB := createInputFolder(t, homeBHTML, homeBNotes,
		map[string][]byte{"plot.png": []byte(homeBFigure)},
		map[string][]byte{"metrics.csv": []byte(homeBData)},
	)
	editOutB := runPCSuccessNoStderr(t, homeB, userHomeB, "records", "edit", recordID, editDirB)
	if !strings.Contains(editOutB, "Record "+recordID+" updated") {
		t.Fatalf("expected homeB edit success output, got:\n%s", editOutB)
	}

	// --- Phase 5: Sync homeA once to converge on homeB's later cloud state. ---
	syncOut = runPCSuccessNoStderr(t, homeA, userHomeA, "sync")
	if !strings.Contains(syncOut, "Sync complete") {
		t.Fatalf("expected sync success output after conflict, got:\n%s", syncOut)
	}

	// --- Phase 6: Verify both homes converge and assets reflect current sync semantics. ---
	recordA := getRecordJSON(t, homeA, userHomeA, recordID)
	recordBFinal := getRecordJSON(t, homeB, userHomeB, recordID)

	// The later edit (homeB) should win.
	if recordA.HTMLContent != homeBHTML {
		t.Fatalf("homeA final content = %q, want HomeB edit", recordA.HTMLContent)
	}
	if recordA.Notes != homeBNotes {
		t.Fatalf("homeA final notes = %q, want %q", recordA.Notes, homeBNotes)
	}
	if recordBFinal.HTMLContent != homeBHTML {
		t.Fatalf("homeB final content = %q, want HomeB edit", recordBFinal.HTMLContent)
	}
	if recordBFinal.Notes != homeBNotes {
		t.Fatalf("homeB final notes = %q, want %q", recordBFinal.Notes, homeBNotes)
	}
	if recordA.ID != recordBFinal.ID {
		t.Fatalf("record IDs diverged: homeA=%s, homeB=%s", recordA.ID, recordBFinal.ID)
	}

	figureAPath := filepath.Join(homeA, "personal-context", "figures", recordID, "plot.png")
	figureBPath := filepath.Join(homeB, "personal-context", "figures", recordID, "plot.png")
	figureAContent, err := os.ReadFile(figureAPath)
	if err != nil {
		t.Fatalf("read converged figure on homeA: %v", err)
	}
	figureBContent, err := os.ReadFile(figureBPath)
	if err != nil {
		t.Fatalf("read converged figure on homeB: %v", err)
	}
	if string(figureAContent) != homeBFigure {
		t.Fatalf("homeA converged figure = %q, want %q", string(figureAContent), homeBFigure)
	}
	if string(figureBContent) != homeBFigure {
		t.Fatalf("homeB converged figure = %q, want %q", string(figureBContent), homeBFigure)
	}

	// Winner home keeps its local data file; the other home keeps only cloud metadata after sync.
	homeBDataPath := filepath.Join(homeB, "personal-context", "data", recordID, "metrics.csv")
	homeBDataContent, err := os.ReadFile(homeBDataPath)
	if err != nil {
		t.Fatalf("read data on winning homeB: %v", err)
	}
	if string(homeBDataContent) != homeBData {
		t.Fatalf("homeB data = %q, want %q", string(homeBDataContent), homeBData)
	}
	homeADataPath := filepath.Join(homeA, "personal-context", "data", recordID, "metrics.csv")
	if _, err := os.Stat(homeADataPath); !os.IsNotExist(err) {
		t.Fatalf("expected homeA data file to be removed during sync reconciliation, stat err = %v", err)
	}
}
