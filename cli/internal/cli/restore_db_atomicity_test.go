package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/gitsnapshot"
	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestRestoreDBAtomicNoCrashPreservesBehavior(t *testing.T) {
	homeDir := setupEnv(t)
	oldID := addRestoreAtomicityOldRecord(t, homeDir)

	snapshotDir := t.TempDir()
	newSnapshot := restoreAtomicityNewSnapshot()
	writeSnapshotForCLITest(t, snapshotDir, newSnapshot)

	stdout := &bytes.Buffer{}
	if err := runRestoreDB(context.Background(), stdout, &bytes.Buffer{}, snapshotDir); err != nil {
		t.Fatalf("runRestoreDB() error = %v", err)
	}
	backupPath := restoreBackupPathFromOutput(t, stdout.String())
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("expected retained backup snapshot: %v", err)
	}
	backupSnapshot, err := gitsnapshot.Read(backupPath)
	if err != nil {
		t.Fatalf("retained backup should remain a readable git snapshot: %v", err)
	}
	if !snapshotHasRecord(backupSnapshot, oldID) {
		t.Fatalf("backup snapshot does not contain old record %s", oldID)
	}
	assertRestoreStoreHasOnlyRecord(t, homeDir, restoreAtomicityNewRecordID, "new.png", []byte("new-figure"), oldID)
	assertRestoreMarkerAndStagingCleaned(t, homeDir)
	if _, err := os.Stat(restorePayloadBackupDir(backupPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("internal restore payload backup should be cleaned up, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(basePath(homeDir), ".pc", "last_sync")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restore-db should remove last_sync like the previous wipe path, stat err=%v", err)
	}
}

func TestRecoverInterruptedRestoreStagingPhaseDiscardsStaging(t *testing.T) {
	homeDir := setupEnv(t)
	oldID := addRestoreAtomicityOldRecord(t, homeDir)
	stagingDir, err := createRestoreStagingDir(homeDir)
	if err != nil {
		t.Fatalf("createRestoreStagingDir() error = %v", err)
	}
	backupPath := filepath.Join(basePath(homeDir), ".pc", "backups", "restore-db-test")
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatalf("create backup path: %v", err)
	}
	marker := newRestoreMarker(restorePhaseStaging, stagingDir, backupPath)
	if err := writeRestoreMarker(homeDir, marker); err != nil {
		t.Fatalf("writeRestoreMarker() error = %v", err)
	}

	if err := recoverInterruptedRestore(homeDir); err != nil {
		t.Fatalf("recoverInterruptedRestore() error = %v", err)
	}
	assertRestoreStoreHasOnlyRecord(t, homeDir, oldID, "old.png", []byte("old-figure"), restoreAtomicityNewRecordID)
	assertRestoreMarkerAndStagingCleaned(t, homeDir)
}

func TestRecoverInterruptedRestoreCommittingRollsForwardAtEveryRenameBoundary(t *testing.T) {
	stepFixture := setupRestoreAtomicityFixture(t)
	totalSteps := countRestoreRenameSteps(stepFixture.homeDir, stepFixture.stagingDir)
	for steps := 0; steps <= totalSteps; steps++ {
		t.Run(fmt.Sprintf("steps_%02d", steps), func(t *testing.T) {
			fixture := setupRestoreAtomicityFixture(t)
			marker := committingRestoreMarkerForTest(t, fixture)
			if err := writeRestoreMarker(fixture.homeDir, marker); err != nil {
				t.Fatalf("writeRestoreMarker() error = %v", err)
			}
			simulateRestoreRenameSteps(t, fixture.homeDir, fixture.stagingDir, fixture.backupPath, steps)

			if err := recoverInterruptedRestore(fixture.homeDir); err != nil {
				t.Fatalf("recoverInterruptedRestore() error = %v", err)
			}
			assertRestoreStoreHasOnlyRecord(t, fixture.homeDir, restoreAtomicityNewRecordID, "new.png", []byte("new-figure"), fixture.oldID)
			assertRestoreMarkerAndStagingCleaned(t, fixture.homeDir)
		})
	}
}

func TestRecoverInterruptedRestoreCommittingRollsBackWhenStagingIsLost(t *testing.T) {
	fixture := setupRestoreAtomicityFixture(t)
	marker := committingRestoreMarkerForTest(t, fixture)
	if err := writeRestoreMarker(fixture.homeDir, marker); err != nil {
		t.Fatalf("writeRestoreMarker() error = %v", err)
	}
	stepsThroughFirstPromote := countRestoreBackupSteps(fixture.homeDir) + 1
	simulateRestoreRenameSteps(t, fixture.homeDir, fixture.stagingDir, fixture.backupPath, stepsThroughFirstPromote)
	if err := os.RemoveAll(fixture.stagingDir); err != nil {
		t.Fatalf("remove staging: %v", err)
	}

	if err := recoverInterruptedRestore(fixture.homeDir); err != nil {
		t.Fatalf("recoverInterruptedRestore() error = %v", err)
	}
	assertRestoreStoreHasOnlyRecord(t, fixture.homeDir, fixture.oldID, "old.png", []byte("old-figure"), restoreAtomicityNewRecordID)
	assertRestoreMarkerAndStagingCleaned(t, fixture.homeDir)
}

func TestRecoverInterruptedRestoreKeepsPromotedEntryThatHadNoBackup(t *testing.T) {
	fixture := setupRestoreAtomicityFixtureWithoutOldFigure(t)
	marker := committingRestoreMarkerForTest(t, fixture)
	if err := writeRestoreMarker(fixture.homeDir, marker); err != nil {
		t.Fatalf("writeRestoreMarker() error = %v", err)
	}
	simulateRestoreRenameSteps(t, fixture.homeDir, fixture.stagingDir, fixture.backupPath, countRestoreRenameSteps(fixture.homeDir, fixture.stagingDir))

	if err := recoverInterruptedRestore(fixture.homeDir); err != nil {
		t.Fatalf("recoverInterruptedRestore() error = %v", err)
	}
	assertRestoreStoreHasOnlyRecord(t, fixture.homeDir, restoreAtomicityNewRecordID, "new.png", []byte("new-figure"), fixture.oldID)
	assertRestoreMarkerAndStagingCleaned(t, fixture.homeDir)
}

func TestRecoverInterruptedRestoreDonePhaseCleansMarkerAndStaleDirs(t *testing.T) {
	fixture := setupRestoreAtomicityFixture(t)
	payloadBackupDir := restorePayloadBackupDir(fixture.backupPath)
	if err := os.MkdirAll(payloadBackupDir, 0o700); err != nil {
		t.Fatalf("create payload backup dir: %v", err)
	}
	marker := newRestoreMarker(restorePhaseDone, fixture.stagingDir, fixture.backupPath)
	if err := writeRestoreMarker(fixture.homeDir, marker); err != nil {
		t.Fatalf("writeRestoreMarker() error = %v", err)
	}

	if err := recoverInterruptedRestore(fixture.homeDir); err != nil {
		t.Fatalf("recoverInterruptedRestore() error = %v", err)
	}
	assertRestoreStoreHasOnlyRecord(t, fixture.homeDir, fixture.oldID, "old.png", []byte("old-figure"), restoreAtomicityNewRecordID)
	assertRestoreMarkerAndStagingCleaned(t, fixture.homeDir)
	if _, err := os.Stat(payloadBackupDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("payload backup should be removed, stat err=%v", err)
	}
}

func TestRestoreMarkerAndRecoveryErrorPaths(t *testing.T) {
	t.Run("invalid marker json", func(t *testing.T) {
		homeDir := t.TempDir()
		markerPath := restoreMarkerPath(homeDir)
		if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
			t.Fatalf("mkdir marker parent: %v", err)
		}
		if err := os.WriteFile(markerPath, []byte("{bad"), 0o600); err != nil {
			t.Fatalf("write bad marker: %v", err)
		}
		if err := recoverInterruptedRestore(homeDir); err == nil || !strings.Contains(err.Error(), "parse restore marker") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want parse failure", err)
		}
	})

	t.Run("marker missing required fields", func(t *testing.T) {
		homeDir := t.TempDir()
		markerPath := restoreMarkerPath(homeDir)
		if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
			t.Fatalf("mkdir marker parent: %v", err)
		}
		if err := os.WriteFile(markerPath, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		if err := recoverInterruptedRestore(homeDir); err == nil || !strings.Contains(err.Error(), "missing required fields") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want required-field failure", err)
		}
	})

	t.Run("read marker surfaces non-missing read error", func(t *testing.T) {
		homeDir := t.TempDir()
		markerPath := restoreMarkerPath(homeDir)
		if err := os.MkdirAll(markerPath, 0o700); err != nil {
			t.Fatalf("mkdir marker path: %v", err)
		}
		if _, err := readRestoreMarker(homeDir); err == nil || !strings.Contains(err.Error(), "read restore marker") {
			t.Fatalf("readRestoreMarker() error = %v, want read failure", err)
		}
	})

	t.Run("unknown marker phase", func(t *testing.T) {
		homeDir := t.TempDir()
		marker := newRestoreMarker("mystery", filepath.Join(t.TempDir(), "stage"), filepath.Join(t.TempDir(), "backup"))
		if err := writeRestoreMarker(homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		if err := recoverInterruptedRestore(homeDir); err == nil || !strings.Contains(err.Error(), "unknown phase") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want unknown phase", err)
		}
	})

	t.Run("write marker rejects missing fields", func(t *testing.T) {
		if err := writeRestoreMarker(t.TempDir(), restoreMarker{}); err == nil || !strings.Contains(err.Error(), "missing required fields") {
			t.Fatalf("writeRestoreMarker() error = %v, want missing fields", err)
		}
	})

	t.Run("committing marker preserves empty entry lists", func(t *testing.T) {
		homeDir := setupEnv(t)
		_ = addRestoreAtomicityOldRecord(t, homeDir)
		marker := newRestoreMarker(
			restorePhaseCommitting,
			filepath.Join(basePath(homeDir), ".pc", "missing-staging"),
			filepath.Join(basePath(homeDir), ".pc", "backups", "restore-db-empty-entries"),
		)
		marker.StagedEntries = []string{}
		marker.OriginalEntries = []string{}
		if err := writeRestoreMarker(homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}

		if err := recoverInterruptedRestore(homeDir); err != nil {
			t.Fatalf("recoverInterruptedRestore() error = %v, want empty entry lists accepted", err)
		}
		assertRestoreMarkerAndStagingCleaned(t, homeDir)
	})

	t.Run("write marker surfaces parent failure", func(t *testing.T) {
		homeDir := t.TempDir()
		pcDir := filepath.Join(basePath(homeDir), ".pc")
		if err := os.MkdirAll(filepath.Dir(pcDir), 0o700); err != nil {
			t.Fatalf("mkdir base: %v", err)
		}
		if err := os.WriteFile(pcDir, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write pc blocker: %v", err)
		}
		marker := newRestoreMarker(restorePhaseStaging, filepath.Join(t.TempDir(), "stage"), filepath.Join(t.TempDir(), "backup"))
		if err := writeRestoreMarker(homeDir, marker); err == nil || !strings.Contains(err.Error(), "write restore marker") {
			t.Fatalf("writeRestoreMarker() error = %v, want parent failure", err)
		}
	})

	t.Run("remove marker surfaces remove failure", func(t *testing.T) {
		homeDir := t.TempDir()
		markerPath := restoreMarkerPath(homeDir)
		if err := os.MkdirAll(filepath.Join(markerPath, "child"), 0o700); err != nil {
			t.Fatalf("mkdir marker dir: %v", err)
		}
		if err := removeRestoreMarker(homeDir); err == nil || !strings.Contains(err.Error(), "remove restore marker") {
			t.Fatalf("removeRestoreMarker() error = %v, want remove failure", err)
		}
	})

	t.Run("remove marker surfaces sync failure", func(t *testing.T) {
		homeDir := t.TempDir()
		marker := newRestoreMarker(restorePhaseStaging, filepath.Join(t.TempDir(), "stage"), filepath.Join(t.TempDir(), "backup"))
		if err := writeRestoreMarker(homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		origSync := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(filepath.Dir(restoreMarkerPath(homeDir))) {
				return errors.New("sync failed")
			}
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })
		if err := removeRestoreMarker(homeDir); err == nil || !strings.Contains(err.Error(), "sync restore marker removal") {
			t.Fatalf("removeRestoreMarker() error = %v, want sync failure", err)
		}
	})
}

func TestRestoreDBMarkerPhaseFailurePaths(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failWriteAt int
		want        string
		wantRecord  string
		wantFigure  string
		wantContent []byte
		absentID    string
	}{
		{name: "staging marker", failWriteAt: 1, want: "write restore marker", wantFigure: "old.png", wantContent: []byte("old-figure"), absentID: restoreAtomicityNewRecordID},
		{name: "committing marker", failWriteAt: 2, want: "write restore marker", wantFigure: "old.png", wantContent: []byte("old-figure"), absentID: restoreAtomicityNewRecordID},
		{name: "done marker", failWriteAt: 3, want: "write restore marker", wantRecord: restoreAtomicityNewRecordID, wantFigure: "new.png", wantContent: []byte("new-figure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := setupEnv(t)
			oldID := addRestoreAtomicityOldRecord(t, homeDir)
			wantRecord := tc.wantRecord
			absentID := tc.absentID
			if wantRecord == "" {
				wantRecord = oldID
			}
			if absentID == "" {
				absentID = oldID
			}
			snapshotDir := t.TempDir()
			writeSnapshotForCLITest(t, snapshotDir, restoreAtomicityNewSnapshot())
			origCreateTempFile := createTempFileFn
			writes := 0
			createTempFileFn = func(dir string, pattern string) (atomicTempFile, error) {
				if filepath.Clean(dir) == filepath.Clean(filepath.Join(basePath(homeDir), ".pc")) {
					writes++
					if writes == tc.failWriteAt {
						return nil, errors.New("marker temp failed")
					}
				}
				return origCreateTempFile(dir, pattern)
			}
			t.Cleanup(func() { createTempFileFn = origCreateTempFile })
			err := runRestoreDB(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runRestoreDB() error = %v, want %s", err, tc.want)
			}
			if err := recoverInterruptedRestore(homeDir); err != nil {
				t.Fatalf("recoverInterruptedRestore() error = %v", err)
			}
			assertRestoreStoreHasOnlyRecord(t, homeDir, wantRecord, tc.wantFigure, tc.wantContent, absentID)
			assertRestoreMarkerAndStagingCleaned(t, homeDir)
		})
	}

	t.Run("done cleanup failure", func(t *testing.T) {
		homeDir := setupEnv(t)
		oldID := addRestoreAtomicityOldRecord(t, homeDir)
		snapshotDir := t.TempDir()
		writeSnapshotForCLITest(t, snapshotDir, restoreAtomicityNewSnapshot())
		origSync := syncDirFn
		backupSyncs := 0
		syncDirFn = func(dir string) error {
			if strings.Contains(filepath.ToSlash(dir), "/.pc/backups/restore-db-") {
				backupSyncs++
				if backupSyncs == 1 {
					return errors.New("cleanup sync failed")
				}
			}
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })
		err := runRestoreDB(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir)
		if err == nil || !strings.Contains(err.Error(), "sync restore payload backup cleanup") {
			t.Fatalf("runRestoreDB() error = %v, want cleanup sync failure", err)
		}
		if err := recoverInterruptedRestore(homeDir); err != nil {
			t.Fatalf("cleanup recovery after injected failure: %v", err)
		}
		assertRestoreStoreHasOnlyRecord(t, homeDir, restoreAtomicityNewRecordID, "new.png", []byte("new-figure"), oldID)
		assertRestoreMarkerAndStagingCleaned(t, homeDir)
	})
}

func TestRestoreDBPromoteFailureDoesNotLaterRollForward(t *testing.T) {
	homeDir := setupEnv(t)
	oldID := addRestoreAtomicityOldRecord(t, homeDir)
	snapshotDir := t.TempDir()
	writeSnapshotForCLITest(t, snapshotDir, restoreAtomicityNewSnapshot())

	origReplace := replaceRestoreContentsFn
	replaceRestoreContentsFn = func(root string, stagingRoot string, options gitsnapshot.ReplacementOptions) error {
		payloadBackupDir := options.BackupDir
		for _, entry := range restorePayloadEntries {
			live := filepath.Join(root, entry)
			if !testPathExists(live) {
				continue
			}
			backup := filepath.Join(payloadBackupDir, entry)
			if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
				return err
			}
			if err := os.Rename(live, backup); err != nil {
				return err
			}
		}
		for _, entry := range restorePayloadEntries {
			staged := filepath.Join(stagingRoot, entry)
			if !testPathExists(staged) {
				continue
			}
			live := filepath.Join(root, entry)
			if err := os.MkdirAll(filepath.Dir(live), 0o700); err != nil {
				return err
			}
			if err := os.Rename(staged, live); err != nil {
				return err
			}
			break
		}
		if options.BeforeRollback != nil {
			if err := options.BeforeRollback(); err != nil {
				return err
			}
		}
		return errors.New("promote failed")
	}
	t.Cleanup(func() { replaceRestoreContentsFn = origReplace })
	err := runRestoreDB(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir)
	if err == nil || !strings.Contains(err.Error(), "promote restored store") {
		t.Fatalf("runRestoreDB() error = %v, want promote failure", err)
	}

	if err := recoverInterruptedRestore(homeDir); err != nil {
		t.Fatalf("recoverInterruptedRestore() after promote failure = %v", err)
	}
	assertRestoreStoreHasOnlyRecord(t, homeDir, oldID, "old.png", []byte("old-figure"), restoreAtomicityNewRecordID)
	assertRestoreMarkerAndStagingCleaned(t, homeDir)
}

func TestRestoreDBPromoteFailureCleanupFailureLeavesCleanupOnlyMarker(t *testing.T) {
	homeDir := setupEnv(t)
	oldID := addRestoreAtomicityOldRecord(t, homeDir)
	snapshotDir := t.TempDir()
	writeSnapshotForCLITest(t, snapshotDir, restoreAtomicityNewSnapshot())

	var capturedStaging string
	origReplace := replaceRestoreContentsFn
	replaceRestoreContentsFn = func(root string, stagingRoot string, options gitsnapshot.ReplacementOptions) error {
		capturedStaging = stagingRoot
		payloadBackupDir := options.BackupDir
		for _, entry := range restorePayloadEntries {
			live := filepath.Join(root, entry)
			if !testPathExists(live) {
				continue
			}
			backup := filepath.Join(payloadBackupDir, entry)
			if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
				return err
			}
			if err := os.Rename(live, backup); err != nil {
				return err
			}
		}
		if err := os.Chmod(filepath.Join(stagingRoot, ".pc"), 0o500); err != nil {
			return err
		}
		if options.BeforeRollback != nil {
			if err := options.BeforeRollback(); err != nil {
				return err
			}
		}
		return errors.New("promote failed")
	}
	t.Cleanup(func() { replaceRestoreContentsFn = origReplace })
	err := runRestoreDB(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir)
	if capturedStaging != "" {
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(capturedStaging, ".pc"), 0o700) })
	}
	if err == nil || !strings.Contains(err.Error(), "abort cleanup failed") {
		t.Fatalf("runRestoreDB() error = %v, want abort cleanup failure", err)
	}
	assertRestoreStoreHasOnlyRecordOnDisk(t, homeDir, oldID, "old.png", []byte("old-figure"), restoreAtomicityNewRecordID)
	marker, err := readRestoreMarker(homeDir)
	if err != nil {
		t.Fatalf("readRestoreMarker() error = %v", err)
	}
	if marker == nil || marker.Phase != restorePhaseDone {
		t.Fatalf("marker after abort cleanup failure = %+v, want done phase", marker)
	}

	if err := os.Chmod(filepath.Join(capturedStaging, ".pc"), 0o700); err != nil {
		t.Fatalf("restore staging permissions: %v", err)
	}
	if err := recoverInterruptedRestore(homeDir); err != nil {
		t.Fatalf("recoverInterruptedRestore() error = %v", err)
	}
	assertRestoreStoreHasOnlyRecord(t, homeDir, oldID, "old.png", []byte("old-figure"), restoreAtomicityNewRecordID)
	assertRestoreMarkerAndStagingCleaned(t, homeDir)
}

func TestRestoreDBCommittedCleanupFailureDoesNotRollback(t *testing.T) {
	homeDir := setupEnv(t)
	oldID := addRestoreAtomicityOldRecord(t, homeDir)
	snapshotDir := t.TempDir()
	writeSnapshotForCLITest(t, snapshotDir, restoreAtomicityNewSnapshot())

	origReplace := replaceRestoreContentsFn
	replaceRestoreContentsFn = func(root string, stagingRoot string, options gitsnapshot.ReplacementOptions) error {
		for _, entry := range restorePayloadEntries {
			live := filepath.Join(root, entry)
			if !testPathExists(live) {
				continue
			}
			backup := filepath.Join(options.BackupDir, entry)
			if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
				return err
			}
			if err := os.Rename(live, backup); err != nil {
				return err
			}
		}
		for _, entry := range restorePayloadEntries {
			staged := filepath.Join(stagingRoot, entry)
			if !testPathExists(staged) {
				continue
			}
			live := filepath.Join(root, entry)
			if err := os.MkdirAll(filepath.Dir(live), 0o700); err != nil {
				return err
			}
			if err := os.Rename(staged, live); err != nil {
				return err
			}
		}
		return &gitsnapshot.CommittedReplacementCleanupError{Err: errors.New("post-commit cleanup failed")}
	}
	t.Cleanup(func() { replaceRestoreContentsFn = origReplace })

	stdout := &bytes.Buffer{}
	err := runRestoreDB(context.Background(), stdout, &bytes.Buffer{}, snapshotDir)
	if err == nil || !strings.Contains(err.Error(), "promote restored store committed") {
		t.Fatalf("runRestoreDB() error = %v, want committed cleanup failure", err)
	}
	backupPath := restoreBackupPathFromOutput(t, stdout.String())
	assertRestoreStoreHasOnlyRecord(t, homeDir, restoreAtomicityNewRecordID, "new.png", []byte("new-figure"), oldID)
	assertRestoreMarkerAndStagingCleaned(t, homeDir)
	if _, err := os.Stat(restorePayloadBackupDir(backupPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("internal restore payload backup should be cleaned immediately after committed cleanup failure, stat err=%v", err)
	}
	if err := recoverInterruptedRestore(homeDir); err != nil {
		t.Fatalf("recoverInterruptedRestore() after committed cleanup failure = %v", err)
	}
	assertRestoreStoreHasOnlyRecord(t, homeDir, restoreAtomicityNewRecordID, "new.png", []byte("new-figure"), oldID)
	assertRestoreMarkerAndStagingCleaned(t, homeDir)
}

func TestRecoverInterruptedRestoreRollbackOnlyNeverRollsForward(t *testing.T) {
	fixture := setupRestoreAtomicityFixture(t)
	marker := committingRestoreMarkerForTest(t, fixture)
	marker.RollbackOnly = true
	if err := writeRestoreMarker(fixture.homeDir, marker); err != nil {
		t.Fatalf("writeRestoreMarker() error = %v", err)
	}
	base := basePath(fixture.homeDir)
	payloadBackupDir := restorePayloadBackupDir(fixture.backupPath)
	for _, entry := range restorePayloadEntries {
		live := filepath.Join(base, entry)
		if !testPathExists(live) {
			continue
		}
		backup := filepath.Join(payloadBackupDir, entry)
		if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
			t.Fatalf("create backup parent for %s: %v", entry, err)
		}
		if err := os.Rename(live, backup); err != nil {
			t.Fatalf("backup live %s: %v", entry, err)
		}
	}
	for _, entry := range restorePayloadEntries {
		staged := filepath.Join(fixture.stagingDir, entry)
		if !testPathExists(staged) {
			continue
		}
		live := filepath.Join(base, entry)
		if err := os.MkdirAll(filepath.Dir(live), 0o700); err != nil {
			t.Fatalf("create live parent for %s: %v", entry, err)
		}
		if err := os.Rename(staged, live); err != nil {
			t.Fatalf("promote staged %s: %v", entry, err)
		}
		break
	}

	if err := recoverInterruptedRestore(fixture.homeDir); err != nil {
		t.Fatalf("recoverInterruptedRestore() error = %v", err)
	}
	assertRestoreStoreHasOnlyRecord(t, fixture.homeDir, fixture.oldID, "old.png", []byte("old-figure"), restoreAtomicityNewRecordID)
	assertRestoreMarkerAndStagingCleaned(t, fixture.homeDir)
}

func TestAbortFailedRestorePromotionErrorPaths(t *testing.T) {
	t.Run("rollback failure", func(t *testing.T) {
		homeDir := setupEnv(t)
		marker := newRestoreMarker(restorePhaseCommitting, filepath.Join(t.TempDir(), "stage"), filepath.Join(basePath(homeDir), ".pc", "backups", "restore-db-test"))
		marker.OriginalEntries = []string{"/not-relative"}
		err := abortFailedRestorePromotion(homeDir, marker)
		if err == nil || !strings.Contains(err.Error(), "roll back failed restore promotion") {
			t.Fatalf("abortFailedRestorePromotion() error = %v, want rollback failure", err)
		}
	})

	t.Run("done marker write failure", func(t *testing.T) {
		homeDir := setupEnv(t)
		oldID := addRestoreAtomicityOldRecord(t, homeDir)
		marker := newRestoreMarker(restorePhaseCommitting, filepath.Join(t.TempDir(), "stage"), filepath.Join(basePath(homeDir), ".pc", "backups", "restore-db-test"))
		originalEntries, err := listExistingRestorePayloadEntries(basePath(homeDir))
		if err != nil {
			t.Fatalf("list original entries: %v", err)
		}
		marker.OriginalEntries = originalEntries
		origCreateTempFile := createTempFileFn
		createTempFileFn = func(dir string, pattern string) (atomicTempFile, error) {
			if filepath.Clean(dir) == filepath.Clean(filepath.Join(basePath(homeDir), ".pc")) {
				return nil, errors.New("marker temp failed")
			}
			return origCreateTempFile(dir, pattern)
		}
		t.Cleanup(func() { createTempFileFn = origCreateTempFile })
		err = abortFailedRestorePromotion(homeDir, marker)
		if err == nil || !strings.Contains(err.Error(), "write restore marker") {
			t.Fatalf("abortFailedRestorePromotion() error = %v, want marker write failure", err)
		}
		assertRestoreStoreHasOnlyRecordOnDisk(t, homeDir, oldID, "old.png", []byte("old-figure"), restoreAtomicityNewRecordID)
	})
}

func TestCreateRestoreStagingDirFailsWhenParentSyncFails(t *testing.T) {
	homeDir := setupEnv(t)
	pcDir := filepath.Join(basePath(homeDir), ".pc")
	origSync := syncDirFn
	syncDirFn = func(dir string) error {
		if filepath.Clean(dir) == filepath.Clean(pcDir) {
			return errors.New("sync failed")
		}
		return origSync(dir)
	}
	t.Cleanup(func() { syncDirFn = origSync })

	if _, err := createRestoreStagingDir(homeDir); err == nil || !strings.Contains(err.Error(), "sync restore staging parent") {
		t.Fatalf("createRestoreStagingDir() error = %v, want staging parent sync failure", err)
	}
}

func TestRestoreRecoverySupportErrorPaths(t *testing.T) {
	t.Run("staging cleanup sync failure", func(t *testing.T) {
		homeDir := setupEnv(t)
		stagingDir, err := createRestoreStagingDir(homeDir)
		if err != nil {
			t.Fatalf("createRestoreStagingDir() error = %v", err)
		}
		backupPath := filepath.Join(basePath(homeDir), ".pc", "backups", "restore-db-test")
		if err := os.MkdirAll(backupPath, 0o700); err != nil {
			t.Fatalf("mkdir backup: %v", err)
		}
		marker := newRestoreMarker(restorePhaseStaging, stagingDir, backupPath)
		if err := writeRestoreMarker(homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		origSync := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(filepath.Dir(stagingDir)) {
				return errors.New("sync failed")
			}
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })
		if err := recoverInterruptedRestore(homeDir); err == nil || !strings.Contains(err.Error(), "sync restore staging cleanup") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want staging cleanup sync failure", err)
		}
	})

	t.Run("done cleanup payload sync failure", func(t *testing.T) {
		fixture := setupRestoreAtomicityFixture(t)
		payloadBackupDir := restorePayloadBackupDir(fixture.backupPath)
		if err := os.MkdirAll(payloadBackupDir, 0o700); err != nil {
			t.Fatalf("mkdir payload backup: %v", err)
		}
		marker := newRestoreMarker(restorePhaseDone, fixture.stagingDir, fixture.backupPath)
		if err := writeRestoreMarker(fixture.homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		origSync := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(fixture.backupPath) {
				return errors.New("sync failed")
			}
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })
		if err := recoverInterruptedRestore(fixture.homeDir); err == nil || !strings.Contains(err.Error(), "sync restore payload backup cleanup") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want payload cleanup sync failure", err)
		}
	})

	t.Run("committing marker missing staged entries fails loud", func(t *testing.T) {
		fixture := setupRestoreAtomicityFixture(t)
		marker := newRestoreMarker(restorePhaseCommitting, fixture.stagingDir, fixture.backupPath)
		marker.OriginalEntries = []string{filepath.Join(".pc", "pc.db")}
		if err := writeRestoreMarker(fixture.homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		if err := recoverInterruptedRestore(fixture.homeDir); err == nil || !strings.Contains(err.Error(), "missing staged_entries") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want staged_entries failure", err)
		}
	})

	t.Run("committing marker missing original entries fails loud", func(t *testing.T) {
		fixture := setupRestoreAtomicityFixture(t)
		marker := newRestoreMarker(restorePhaseCommitting, fixture.stagingDir, fixture.backupPath)
		marker.StagedEntries = []string{filepath.Join(".pc", "pc.db")}
		if err := writeRestoreMarker(fixture.homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		if err := recoverInterruptedRestore(fixture.homeDir); err == nil || !strings.Contains(err.Error(), "missing original_entries") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want original_entries failure", err)
		}
	})

	t.Run("committing rollback detects missing restored database", func(t *testing.T) {
		homeDir := setupEnv(t)
		if err := os.Remove(dbPath(homeDir)); err != nil {
			t.Fatalf("remove live db: %v", err)
		}
		marker := newRestoreMarker(restorePhaseCommitting, filepath.Join(t.TempDir(), "missing-stage"), filepath.Join(basePath(homeDir), ".pc", "backups", "restore-db-test"))
		marker.StagedEntries = []string{"figures"}
		marker.OriginalEntries = []string{filepath.Join(".pc", "pc.db")}
		if err := writeRestoreMarker(homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		if err := recoverInterruptedRestore(homeDir); err == nil || !strings.Contains(err.Error(), "restored database is missing") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want missing restored database", err)
		}
	})

	t.Run("committing rollback-only rollback failure fails loud", func(t *testing.T) {
		fixture := setupRestoreAtomicityFixture(t)
		marker := committingRestoreMarkerForTest(t, fixture)
		marker.RollbackOnly = true
		marker.OriginalEntries = []string{"/not-relative"}
		if err := writeRestoreMarker(fixture.homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		if err := recoverInterruptedRestore(fixture.homeDir); err == nil || !strings.Contains(err.Error(), "roll back interrupted restore") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want rollback failure", err)
		}
	})

	t.Run("committing rollback surfaces backup inspection failure", func(t *testing.T) {
		homeDir := setupEnv(t)
		if err := os.Remove(dbPath(homeDir)); err != nil {
			t.Fatalf("remove live db: %v", err)
		}
		backupFile := filepath.Join(basePath(homeDir), ".pc", "backups", "restore-db-file")
		if err := os.MkdirAll(filepath.Dir(backupFile), 0o700); err != nil {
			t.Fatalf("mkdir backup parent: %v", err)
		}
		if err := os.WriteFile(backupFile, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write backup file: %v", err)
		}
		marker := newRestoreMarker(restorePhaseCommitting, filepath.Join(t.TempDir(), "missing-stage"), backupFile)
		marker.StagedEntries = []string{"figures"}
		marker.OriginalEntries = []string{filepath.Join(".pc", "pc.db")}
		if err := writeRestoreMarker(homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		if err := recoverInterruptedRestore(homeDir); err == nil || !strings.Contains(err.Error(), "roll back interrupted restore") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want rollback failure", err)
		}
	})

	t.Run("committing staging path file fails loud", func(t *testing.T) {
		fixture := setupRestoreAtomicityFixture(t)
		marker := committingRestoreMarkerForTest(t, fixture)
		stagingFile := filepath.Join(basePath(fixture.homeDir), ".pc", "restore-staging-file")
		if err := os.RemoveAll(fixture.stagingDir); err != nil {
			t.Fatalf("remove staging dir: %v", err)
		}
		if err := os.WriteFile(stagingFile, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write staging file: %v", err)
		}
		marker.StagingDir = stagingFile
		if err := writeRestoreMarker(fixture.homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		if err := recoverInterruptedRestore(fixture.homeDir); err == nil || !strings.Contains(err.Error(), "restore staging path is not a directory") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want staging file failure", err)
		}
	})

	t.Run("committing roll-forward error fails loud without rollback", func(t *testing.T) {
		homeDir := setupEnv(t)
		oldID := addRestoreAtomicityOldRecord(t, homeDir)
		stagingDir := filepath.Join(basePath(homeDir), ".pc", "restore-staging-bad")
		if err := os.MkdirAll(stagingDir, 0o700); err != nil {
			t.Fatalf("mkdir staging: %v", err)
		}
		if err := os.WriteFile(filepath.Join(stagingDir, ".pc"), []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write staging blocker: %v", err)
		}
		backupPath := filepath.Join(basePath(homeDir), ".pc", "backups", "restore-db-test")
		originalEntries, err := listExistingRestorePayloadEntries(basePath(homeDir))
		if err != nil {
			t.Fatalf("list live entries: %v", err)
		}
		marker := newRestoreMarker(restorePhaseCommitting, stagingDir, backupPath)
		marker.StagedEntries = []string{filepath.Join(".pc", "pc.db")}
		marker.OriginalEntries = originalEntries
		if err := writeRestoreMarker(homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}

		if err := recoverInterruptedRestore(homeDir); err == nil || !strings.Contains(err.Error(), "roll forward interrupted restore") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want roll-forward failure", err)
		}
		assertRestoreStoreHasOnlyRecordOnDisk(t, homeDir, oldID, "old.png", []byte("old-figure"), restoreAtomicityNewRecordID)
		if _, err := os.Stat(restoreMarkerPath(homeDir)); err != nil {
			t.Fatalf("marker should remain for retry after failed recovery, stat err=%v", err)
		}
	})

	t.Run("committing recognizes completed promote before done marker", func(t *testing.T) {
		homeDir := setupEnv(t)
		marker := newRestoreMarker(restorePhaseCommitting, filepath.Join(t.TempDir(), "stage-without-db"), filepath.Join(basePath(homeDir), ".pc", "backups", "restore-db-test"))
		marker.StagedEntries = []string{"figures"}
		marker.OriginalEntries = []string{filepath.Join(".pc", "pc.db")}
		if err := writeRestoreMarker(homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		if err := recoverInterruptedRestore(homeDir); err != nil {
			t.Fatalf("recoverInterruptedRestore() error = %v", err)
		}
		assertRestoreMarkerAndStagingCleaned(t, homeDir)
	})

	t.Run("committing cleanup failure leaves marker for retry", func(t *testing.T) {
		fixture := setupRestoreAtomicityFixture(t)
		marker := committingRestoreMarkerForTest(t, fixture)
		if err := writeRestoreMarker(fixture.homeDir, marker); err != nil {
			t.Fatalf("writeRestoreMarker() error = %v", err)
		}
		simulateRestoreRenameSteps(t, fixture.homeDir, fixture.stagingDir, fixture.backupPath, countRestoreRenameSteps(fixture.homeDir, fixture.stagingDir))
		origSync := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(fixture.backupPath) {
				return errors.New("sync failed")
			}
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })
		if err := recoverInterruptedRestore(fixture.homeDir); err == nil || !strings.Contains(err.Error(), "sync restore payload backup cleanup") {
			t.Fatalf("recoverInterruptedRestore() error = %v, want cleanup sync failure", err)
		}
		if _, err := os.Stat(restoreMarkerPath(fixture.homeDir)); err != nil {
			t.Fatalf("marker should remain for cleanup retry, stat err=%v", err)
		}
	})

	t.Run("create staging dir sync failure", func(t *testing.T) {
		homeDir := t.TempDir()
		origSync := syncDirFn
		syncDirFn = func(string) error {
			return errors.New("sync failed")
		}
		t.Cleanup(func() { syncDirFn = origSync })
		if _, err := createRestoreStagingDir(homeDir); err == nil || !strings.Contains(err.Error(), "sync") {
			t.Fatalf("createRestoreStagingDir() error = %v, want sync failure", err)
		}
	})

	t.Run("create staging dir parent failure", func(t *testing.T) {
		homeFile := filepath.Join(t.TempDir(), "home-file")
		if err := os.WriteFile(homeFile, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write home file: %v", err)
		}
		if _, err := createRestoreStagingDir(homeFile); err == nil || !strings.Contains(err.Error(), "create restore staging parent") {
			t.Fatalf("createRestoreStagingDir() error = %v, want parent failure", err)
		}
	})

	t.Run("list staged entries stat failure", func(t *testing.T) {
		base := t.TempDir()
		if err := os.WriteFile(filepath.Join(base, ".pc"), []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write .pc blocker: %v", err)
		}
		if _, err := listExistingRestorePayloadEntries(base); err == nil || !strings.Contains(err.Error(), "inspect restore payload entry") {
			t.Fatalf("listExistingRestorePayloadEntries() error = %v, want stat failure", err)
		}
	})

	t.Run("sync staged payload file failure", func(t *testing.T) {
		base := t.TempDir()
		if err := os.MkdirAll(filepath.Join(base, ".pc"), 0o700); err != nil {
			t.Fatalf("mkdir .pc: %v", err)
		}
		if err := os.WriteFile(filepath.Join(base, ".pc", "pc.db"), []byte("db"), 0o600); err != nil {
			t.Fatalf("write db: %v", err)
		}
		origSyncFile := syncFilePathFn
		syncFilePathFn = func(string) error {
			return errors.New("sync file failed")
		}
		t.Cleanup(func() { syncFilePathFn = origSyncFile })
		if err := syncRestorePayload(base); err == nil || !strings.Contains(err.Error(), "sync staged restore file") {
			t.Fatalf("syncRestorePayload() error = %v, want file sync failure", err)
		}
	})

	t.Run("sync staged payload directory failure", func(t *testing.T) {
		base := t.TempDir()
		if err := os.MkdirAll(filepath.Join(base, ".pc"), 0o700); err != nil {
			t.Fatalf("mkdir .pc: %v", err)
		}
		if err := os.WriteFile(filepath.Join(base, ".pc", "pc.db"), []byte("db"), 0o600); err != nil {
			t.Fatalf("write db: %v", err)
		}
		origSync := syncDirFn
		syncDirFn = func(string) error {
			return errors.New("sync dir failed")
		}
		t.Cleanup(func() { syncDirFn = origSync })
		if err := syncRestorePayload(base); err == nil || !strings.Contains(err.Error(), "sync staged restore directory") {
			t.Fatalf("syncRestorePayload() error = %v, want directory sync failure", err)
		}
	})

	t.Run("sync staged path inspect failure", func(t *testing.T) {
		base := t.TempDir()
		if err := os.WriteFile(filepath.Join(base, ".pc"), []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write .pc blocker: %v", err)
		}
		if err := syncRestorePath(filepath.Join(base, ".pc", "pc.db"), map[string]struct{}{}); err == nil || !strings.Contains(err.Error(), "inspect staged restore path") {
			t.Fatalf("syncRestorePath() error = %v, want inspect failure", err)
		}
	})

	t.Run("sync staged nested file failure", func(t *testing.T) {
		base := t.TempDir()
		figurePath := filepath.Join(base, "figures", "record", "plot.png")
		if err := os.MkdirAll(filepath.Dir(figurePath), 0o700); err != nil {
			t.Fatalf("mkdir figure dir: %v", err)
		}
		if err := os.WriteFile(figurePath, []byte("figure"), 0o600); err != nil {
			t.Fatalf("write figure: %v", err)
		}
		origSyncFile := syncFilePathFn
		syncFilePathFn = func(string) error {
			return errors.New("sync file failed")
		}
		t.Cleanup(func() { syncFilePathFn = origSyncFile })
		if err := syncRestorePath(filepath.Join(base, "figures"), map[string]struct{}{}); err == nil || !strings.Contains(err.Error(), "sync staged restore file") {
			t.Fatalf("syncRestorePath() error = %v, want nested file sync failure", err)
		}
	})

	t.Run("sync file missing path", func(t *testing.T) {
		if err := syncFilePath(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("expected syncFilePath missing path to fail")
		}
	})

	t.Run("cleanup completed restore surfaces staging failure", func(t *testing.T) {
		parentFile := filepath.Join(t.TempDir(), "parent-file")
		if err := os.WriteFile(parentFile, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write parent file: %v", err)
		}
		marker := newRestoreMarker(restorePhaseDone, filepath.Join(parentFile, "stage"), t.TempDir())
		if err := cleanupCompletedRestore(marker); err == nil || !strings.Contains(err.Error(), "remove restore staging dir") {
			t.Fatalf("cleanupCompletedRestore() error = %v, want staging failure", err)
		}
	})

	t.Run("cleanup completed restore surfaces payload removal failure", func(t *testing.T) {
		stagingDir := filepath.Join(t.TempDir(), "stage")
		if err := os.MkdirAll(stagingDir, 0o700); err != nil {
			t.Fatalf("mkdir staging: %v", err)
		}
		backupFile := filepath.Join(t.TempDir(), "backup-file")
		if err := os.WriteFile(backupFile, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write backup file: %v", err)
		}
		marker := newRestoreMarker(restorePhaseDone, stagingDir, backupFile)
		if err := cleanupCompletedRestore(marker); err == nil || !strings.Contains(err.Error(), "remove restore payload backup") {
			t.Fatalf("cleanupCompletedRestore() error = %v, want payload removal failure", err)
		}
	})

	t.Run("build staged store surfaces environment failure", func(t *testing.T) {
		homeDir := setupEnv(t)
		stagingFile := filepath.Join(t.TempDir(), "stage-file")
		if err := os.WriteFile(stagingFile, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write staging file: %v", err)
		}
		if _, err := buildRestoreStagedStore(context.Background(), homeDir, stagingFile, restoreAtomicityNewSnapshot()); err == nil {
			t.Fatal("expected buildRestoreStagedStore environment failure")
		}
	})

	t.Run("build staged store surfaces final payload sync failure", func(t *testing.T) {
		homeDir := setupEnv(t)
		stagingDir, err := createRestoreStagingDir(homeDir)
		if err != nil {
			t.Fatalf("createRestoreStagingDir() error = %v", err)
		}
		origSyncFile := syncFilePathFn
		syncFilePathFn = func(string) error {
			return errors.New("sync file failed")
		}
		t.Cleanup(func() { syncFilePathFn = origSyncFile })
		if _, err := buildRestoreStagedStore(context.Background(), homeDir, stagingDir, restoreAtomicityNewSnapshot()); err == nil || !strings.Contains(err.Error(), "sync staged restore file") {
			t.Fatalf("buildRestoreStagedStore() error = %v, want sync failure", err)
		}
	})
}

const restoreAtomicityNewRecordID = "20260623-aabbccdd"

type restoreAtomicityFixture struct {
	homeDir    string
	oldID      string
	stagingDir string
	backupPath string
}

func setupRestoreAtomicityFixture(t *testing.T) restoreAtomicityFixture {
	t.Helper()
	homeDir := setupEnv(t)
	oldID := addRestoreAtomicityOldRecord(t, homeDir)
	return setupRestoreAtomicityFixtureFromHome(t, homeDir, oldID)
}

func setupRestoreAtomicityFixtureWithoutOldFigure(t *testing.T) restoreAtomicityFixture {
	t.Helper()
	homeDir := setupEnv(t)
	oldID := addRecordWithContent(t, "<html><body>old without figures</body></html>", "old notes", "", nil, nil)
	return setupRestoreAtomicityFixtureFromHome(t, homeDir, oldID)
}

func setupRestoreAtomicityFixtureFromHome(t *testing.T, homeDir string, oldID string) restoreAtomicityFixture {
	t.Helper()
	backupPath := filepath.Join(basePath(homeDir), ".pc", "backups", "restore-db-test")
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack() error = %v", err)
	}
	oldSnapshot, err := buildLocalSnapshot(context.Background(), stack, repository.ListRecordsFilter{})
	closeErr := stack.Close()
	if err != nil {
		t.Fatalf("buildLocalSnapshot() error = %v", err)
	}
	if closeErr != nil {
		t.Fatalf("close stack: %v", closeErr)
	}
	if err := gitsnapshot.Write(backupPath, oldSnapshot); err != nil {
		t.Fatalf("write backup snapshot: %v", err)
	}
	stagingDir, err := createRestoreStagingDir(homeDir)
	if err != nil {
		t.Fatalf("createRestoreStagingDir() error = %v", err)
	}
	if _, err := buildRestoreStagedStore(context.Background(), homeDir, stagingDir, restoreAtomicityNewSnapshot()); err != nil {
		t.Fatalf("buildRestoreStagedStore() error = %v", err)
	}
	return restoreAtomicityFixture{homeDir: homeDir, oldID: oldID, stagingDir: stagingDir, backupPath: backupPath}
}

func committingRestoreMarkerForTest(t *testing.T, fixture restoreAtomicityFixture) restoreMarker {
	t.Helper()
	entries, err := listExistingRestorePayloadEntries(fixture.stagingDir)
	if err != nil {
		t.Fatalf("listExistingRestorePayloadEntries() error = %v", err)
	}
	originalEntries, err := listExistingRestorePayloadEntries(basePath(fixture.homeDir))
	if err != nil {
		t.Fatalf("listExistingRestorePayloadEntries(live) error = %v", err)
	}
	marker := newRestoreMarker(restorePhaseCommitting, fixture.stagingDir, fixture.backupPath)
	marker.StagedEntries = entries
	marker.OriginalEntries = originalEntries
	return marker
}

func addRestoreAtomicityOldRecord(t *testing.T, homeDir string) string {
	t.Helper()
	oldID := addRecordWithContent(
		t,
		`<html><body><img src="figures/old.png">old</body></html>`,
		"old notes",
		"",
		map[string][]byte{"old.png": []byte("old-figure")},
		map[string][]byte{"old.csv": []byte("a,b\n1,2\n")},
	)
	lastSyncPath := filepath.Join(basePath(homeDir), ".pc", "last_sync")
	if err := os.WriteFile(lastSyncPath, []byte("2026-06-23T00:00:00Z\n"), 0o600); err != nil {
		t.Fatalf("write last_sync: %v", err)
	}
	return oldID
}

func restoreAtomicityNewSnapshot() gitsnapshot.Snapshot {
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	return withCLISnapshotDefaults(gitsnapshot.Snapshot{
		Records: []gitsnapshot.Record{{
			ID:          restoreAtomicityNewRecordID,
			Date:        "2026-06-23",
			DayOrder:    "a0",
			HTMLContent: strPtr("<html><body><img src=\"figures/new.png\">new</body></html>"),
			Notes:       strPtr("new notes"),
			Figures: []gitsnapshot.Figure{{
				Filename: "new.png",
				S3Key:    "figures/" + restoreAtomicityNewRecordID + "/new.png",
				Content:  []byte("new-figure"),
			}},
			CreatedAt: now,
			UpdatedAt: now,
		}},
	})
}

func countRestoreRenameSteps(homeDir string, stagingDir string) int {
	base := basePath(homeDir)
	total := 0
	for _, entry := range restorePayloadEntries {
		if testPathExists(filepath.Join(base, entry)) {
			total++
		}
	}
	for _, entry := range restorePayloadEntries {
		if testPathExists(filepath.Join(stagingDir, entry)) {
			total++
		}
	}
	return total
}

func countRestoreBackupSteps(homeDir string) int {
	base := basePath(homeDir)
	total := 0
	for _, entry := range restorePayloadEntries {
		if testPathExists(filepath.Join(base, entry)) {
			total++
		}
	}
	return total
}

func simulateRestoreRenameSteps(t *testing.T, homeDir string, stagingDir string, backupPath string, maxSteps int) {
	t.Helper()
	if maxSteps == 0 {
		return
	}
	base := basePath(homeDir)
	payloadBackupDir := restorePayloadBackupDir(backupPath)
	steps := 0
	for _, entry := range restorePayloadEntries {
		live := filepath.Join(base, entry)
		if !testPathExists(live) {
			continue
		}
		backup := filepath.Join(payloadBackupDir, entry)
		if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
			t.Fatalf("create backup parent for %s: %v", entry, err)
		}
		if err := os.Rename(live, backup); err != nil {
			t.Fatalf("backup live %s: %v", entry, err)
		}
		steps++
		if steps == maxSteps {
			return
		}
	}
	for _, entry := range restorePayloadEntries {
		staged := filepath.Join(stagingDir, entry)
		if !testPathExists(staged) {
			continue
		}
		live := filepath.Join(base, entry)
		if err := os.MkdirAll(filepath.Dir(live), 0o700); err != nil {
			t.Fatalf("create live parent for %s: %v", entry, err)
		}
		if err := os.Rename(staged, live); err != nil {
			t.Fatalf("promote staged %s: %v", entry, err)
		}
		steps++
		if steps == maxSteps {
			return
		}
	}
}

func assertRestoreStoreHasOnlyRecord(t *testing.T, homeDir string, presentID string, figureName string, figureContent []byte, absentID string) {
	t.Helper()
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack() error = %v", err)
	}
	defer func() { _ = stack.Close() }()
	ctx := context.Background()
	if _, err := stack.Repo.GetRecordByID(ctx, presentID); err != nil {
		t.Fatalf("expected record %s after recovery: %v", presentID, err)
	}
	if _, err := stack.Repo.GetRecordByID(ctx, absentID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("record %s presence after recovery error = %v, want not found", absentID, err)
	}
	figures, err := stack.Repo.ListRecordFiguresByRecordID(ctx, presentID)
	if err != nil {
		t.Fatalf("ListRecordFiguresByRecordID(%s) error = %v", presentID, err)
	}
	if len(figures) != 1 || figures[0].Filename != figureName {
		t.Fatalf("figures for %s = %+v, want only %s", presentID, figures, figureName)
	}
	figurePath := filepath.Join(basePath(homeDir), "figures", presentID, figureName)
	gotFigure, err := os.ReadFile(figurePath)
	if err != nil {
		t.Fatalf("read recovered figure %s: %v", figurePath, err)
	}
	if !bytes.Equal(gotFigure, figureContent) {
		t.Fatalf("recovered figure content = %q, want %q", gotFigure, figureContent)
	}
	if _, err := os.Stat(filepath.Join(basePath(homeDir), "figures", absentID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent record figure dir still exists, stat err=%v", err)
	}
}

func assertRestoreStoreHasOnlyRecordOnDisk(t *testing.T, homeDir string, presentID string, figureName string, figureContent []byte, absentID string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath(homeDir))
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	defer func() { _ = db.Close() }()
	var presentCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM records WHERE id = ?`, presentID).Scan(&presentCount); err != nil {
		t.Fatalf("query present record %s: %v", presentID, err)
	}
	if presentCount != 1 {
		t.Fatalf("record %s count = %d, want 1", presentID, presentCount)
	}
	var absentCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM records WHERE id = ?`, absentID).Scan(&absentCount); err != nil {
		t.Fatalf("query absent record %s: %v", absentID, err)
	}
	if absentCount != 0 {
		t.Fatalf("record %s count = %d, want 0", absentID, absentCount)
	}
	var figureCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM record_figures WHERE record_id = ?`, presentID).Scan(&figureCount); err != nil {
		t.Fatalf("query figure count for %s: %v", presentID, err)
	}
	if figureCount != 1 {
		t.Fatalf("figure count for %s = %d, want 1", presentID, figureCount)
	}
	var gotFigure string
	if err := db.QueryRow(`SELECT filename FROM record_figures WHERE record_id = ?`, presentID).Scan(&gotFigure); err != nil {
		t.Fatalf("query figure for %s: %v", presentID, err)
	}
	if gotFigure != figureName {
		t.Fatalf("figure for %s = %s, want %s", presentID, gotFigure, figureName)
	}
	figurePath := filepath.Join(basePath(homeDir), "figures", presentID, figureName)
	gotFigureContent, err := os.ReadFile(figurePath)
	if err != nil {
		t.Fatalf("read figure %s: %v", figurePath, err)
	}
	if !bytes.Equal(gotFigureContent, figureContent) {
		t.Fatalf("figure content = %q, want %q", gotFigureContent, figureContent)
	}
	if _, err := os.Stat(filepath.Join(basePath(homeDir), "figures", absentID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent record figure dir still exists, stat err=%v", err)
	}
}

func assertRestoreMarkerAndStagingCleaned(t *testing.T, homeDir string) {
	t.Helper()
	if _, err := os.Stat(restoreMarkerPath(homeDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restore marker should be removed, stat err=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(basePath(homeDir), ".pc"))
	if err != nil {
		t.Fatalf("read .pc: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "restore-staging-") {
			t.Fatalf("restore staging dir %s was not cleaned", entry.Name())
		}
	}
}

func restoreBackupPathFromOutput(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Backup created at ") {
			return strings.TrimPrefix(line, "Backup created at ")
		}
	}
	t.Fatalf("restore output missing backup path: %q", output)
	return ""
}

func snapshotHasRecord(snapshot gitsnapshot.Snapshot, id string) bool {
	for _, record := range snapshot.Records {
		if record.ID == id {
			return true
		}
	}
	return false
}

func testPathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
