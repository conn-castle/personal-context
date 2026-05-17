//go:build integration

package cloude2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchSuccessPathFromRealCloudState(t *testing.T) {
	cloud := newCloudTestEnv(t)

	// Set up homeA to produce records, homeB to fetch them.
	// First home applies the Postgres schema; second home reuses it.
	homeA, userHomeA := setupCloudHome(t, cloud)
	homeB, userHomeB := setupCloudHomeNoSchema(t, cloud)

	// --- Phase 1: Create 3 records on homeA — 2 in project "alpha", 1 in "beta" ---
	// Alpha record 1 with data file.
	inputAlpha1 := createInputFolder(t,
		"<html>Alpha 1</html>",
		"",
		nil,
		map[string][]byte{"dataset1.csv": []byte("x,y\n1,2\n3,4\n")},
	)
	alpha1ID := strings.TrimSpace(runPCSuccessNoStderr(t, homeA, userHomeA,
		"records", "add", "--project", "alpha", inputAlpha1))

	// Alpha record 2 with data file.
	inputAlpha2 := createInputFolder(t,
		"<html>Alpha 2</html>",
		"",
		nil,
		map[string][]byte{"results.json": []byte(`{"accuracy": 0.95}`)},
	)
	alpha2ID := strings.TrimSpace(runPCSuccessNoStderr(t, homeA, userHomeA,
		"records", "add", "--project", "alpha", inputAlpha2))

	// Beta record with data file.
	inputBeta := createInputFolder(t,
		"<html>Beta</html>",
		"",
		nil,
		map[string][]byte{"report.txt": []byte("Beta report content")},
	)
	betaID := strings.TrimSpace(runPCSuccessNoStderr(t, homeA, userHomeA,
		"records", "add", "--project", "beta", inputBeta))

	// Sync homeA → cloud.
	syncOut := runPCSuccessNoStderr(t, homeA, userHomeA, "sync")
	if !strings.Contains(syncOut, "Sync complete") {
		t.Fatalf("expected sync success output, got:\n%s", syncOut)
	}

	// Sync homeB ← cloud (metadata arrives, data files remain cloud-only).
	syncOut = runPCSuccessNoStderr(t, homeB, userHomeB, "sync")
	if !strings.Contains(syncOut, "Sync complete") {
		t.Fatalf("expected sync success output, got:\n%s", syncOut)
	}

	// Sync should not download data-file bytes into the canonical local data path.
	alpha1DefaultPath := filepath.Join(homeB, "personal-context", "data", alpha1ID, "dataset1.csv")
	alpha2DefaultPath := filepath.Join(homeB, "personal-context", "data", alpha2ID, "results.json")
	betaDefaultPath := filepath.Join(homeB, "personal-context", "data", betaID, "report.txt")
	if _, err := os.Stat(alpha1DefaultPath); !os.IsNotExist(err) {
		t.Fatalf("expected alpha1 default data path to be empty before fetch, stat err = %v", err)
	}
	if _, err := os.Stat(alpha2DefaultPath); !os.IsNotExist(err) {
		t.Fatalf("expected alpha2 default data path to be empty before fetch, stat err = %v", err)
	}

	// --- Phase 2: Fetch alpha project data files to a custom output directory ---
	customOutput := filepath.Join(t.TempDir(), "fetched")
	fetchOut := runPCSuccessNoStderr(t, homeB, userHomeB,
		"fetch", "--project", "alpha", "--output", customOutput)

	expectedFetchLine := "Downloaded 2 file(s) to " + customOutput + "\n"
	if fetchOut != expectedFetchLine {
		t.Fatalf("expected exact fetch output %q, got %q", expectedFetchLine, fetchOut)
	}

	// Verify files are written under <customOutput>/<recordID>/<filename>.
	alpha1Path := filepath.Join(customOutput, alpha1ID, "dataset1.csv")
	alpha2Path := filepath.Join(customOutput, alpha2ID, "results.json")

	alpha1Content, err := os.ReadFile(alpha1Path)
	if err != nil {
		t.Fatalf("read alpha1 data file: %v", err)
	}
	if string(alpha1Content) != "x,y\n1,2\n3,4\n" {
		t.Fatalf("alpha1 content = %q, want original", string(alpha1Content))
	}

	alpha2Content, err := os.ReadFile(alpha2Path)
	if err != nil {
		t.Fatalf("read alpha2 data file: %v", err)
	}
	if string(alpha2Content) != `{"accuracy": 0.95}` {
		t.Fatalf("alpha2 content = %q, want original", string(alpha2Content))
	}

	// Verify beta files are NOT downloaded.
	betaEntries, _ := filepath.Glob(filepath.Join(customOutput, "*", "report.txt"))
	if len(betaEntries) > 0 {
		t.Fatalf("beta data file should not be fetched with --project alpha, found: %v", betaEntries)
	}
	if _, err := os.Stat(filepath.Join(customOutput, betaID, "report.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected beta output path to remain absent, stat err = %v", err)
	}
	if _, err := os.Stat(alpha1DefaultPath); !os.IsNotExist(err) {
		t.Fatalf("expected fetch --output to leave canonical data path untouched for alpha1, stat err = %v", err)
	}
	if _, err := os.Stat(alpha2DefaultPath); !os.IsNotExist(err) {
		t.Fatalf("expected fetch --output to leave canonical data path untouched for alpha2, stat err = %v", err)
	}

	// --- Phase 3: Fetch all data files into the canonical local data path ---
	fetchAllOut := runPCSuccessNoStderr(t, homeB, userHomeB, "fetch", "--all")
	for _, want := range []string{
		"Records scanned: 3\n",
		"Files already present: 0\n",
		"Files downloaded: 3\n",
		"Missing/failed files: 0\n",
	} {
		if !strings.Contains(fetchAllOut, want) {
			t.Fatalf("fetch --all output missing %q, got:\n%s", want, fetchAllOut)
		}
	}

	for _, item := range []struct {
		path string
		want string
	}{
		{alpha1DefaultPath, "x,y\n1,2\n3,4\n"},
		{alpha2DefaultPath, `{"accuracy": 0.95}`},
		{betaDefaultPath, "Beta report content"},
	} {
		content, err := os.ReadFile(item.path)
		if err != nil {
			t.Fatalf("read canonical fetched file %s: %v", item.path, err)
		}
		if string(content) != item.want {
			t.Fatalf("canonical file %s content = %q, want %q", item.path, string(content), item.want)
		}
	}

	// --- Phase 4: Re-run all-mode fetch to verify idempotent skip behavior ---
	fetchAllOut2 := runPCSuccessNoStderr(t, homeB, userHomeB, "fetch", "--all")
	for _, want := range []string{
		"Records scanned: 3\n",
		"Files already present: 3\n",
		"Files downloaded: 0\n",
		"Bytes downloaded: 0\n",
		"Missing/failed files: 0\n",
	} {
		if !strings.Contains(fetchAllOut2, want) {
			t.Fatalf("second fetch --all output missing %q, got:\n%s", want, fetchAllOut2)
		}
	}

	// --- Phase 5: Verify no temp files remain ---
	tempFiles, _ := filepath.Glob(filepath.Join(customOutput, "*", ".pc-fetch-*.tmp"))
	if len(tempFiles) > 0 {
		t.Fatalf("temp files left behind: %v", tempFiles)
	}
	canonicalTempFiles, _ := filepath.Glob(filepath.Join(homeB, "personal-context", "data", "*", ".pc-fetch-*.tmp"))
	if len(canonicalTempFiles) > 0 {
		t.Fatalf("canonical temp files left behind: %v", canonicalTempFiles)
	}

	// --- Phase 6: Re-run project fetch to verify idempotent overwrite ---
	fetchOut2 := runPCSuccessNoStderr(t, homeB, userHomeB,
		"fetch", "--project", "alpha", "--output", customOutput)
	if fetchOut2 != expectedFetchLine {
		t.Fatalf("expected exact re-fetch output %q, got %q", expectedFetchLine, fetchOut2)
	}

	// Content should still be correct after overwrite.
	alpha1Again, _ := os.ReadFile(alpha1Path)
	if string(alpha1Again) != "x,y\n1,2\n3,4\n" {
		t.Fatalf("alpha1 content after re-fetch = %q", string(alpha1Again))
	}

	tempFiles2, _ := filepath.Glob(filepath.Join(customOutput, "*", ".pc-fetch-*.tmp"))
	if len(tempFiles2) > 0 {
		t.Fatalf("temp files left behind after re-fetch: %v", tempFiles2)
	}
}
