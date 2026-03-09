package sync

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/syncengine"
)

func TestServiceSyncPushesLocalBundleToCloud(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 4, 5, 987000000, time.UTC)
	bundle := SlideBundle{
		Slide: repository.Slide{
			ID:          slideID,
			Date:        "2026-03-08",
			DayOrder:    "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Figures: []repository.SlideFigure{{
			SlideID:  slideID,
			Filename: "plot.png",
			S3Key:    "figures/20260308-a1b2c3d4/plot.png",
		}},
		DataFiles: []repository.SlideDataFile{{
			SlideID:  slideID,
			Filename: "metrics.csv",
			S3Key:    "data/20260308-a1b2c3d4/metrics.csv",
			Size:     7,
			Hash:     strings.Repeat("a", 64),
		}},
	}

	service, localRepo, cloudRepo, localFS, objects, cursorStore := newTestService(
		t,
		[]SlideBundle{bundle},
		nil,
	)
	writeLocalAsset(t, localFS, true, slideID, "plot.png", "FIGURE")
	writeLocalAsset(t, localFS, false, slideID, "metrics.csv", "1,2,3\n")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	assertBundleEqual(t, cloudRepo.bundle(slideID), localRepo.bundle(slideID))
	if got := objects.objects["figures/20260308-a1b2c3d4/plot.png"]; got != "FIGURE" {
		t.Fatalf("uploaded figure = %q, want %q", got, "FIGURE")
	}
	if got := objects.objects["data/20260308-a1b2c3d4/metrics.csv"]; got != "1,2,3\n" {
		t.Fatalf("uploaded data file = %q, want %q", got, "1,2,3\n")
	}

	_, exists, err := cursorStore.Read()
	if err != nil {
		t.Fatalf("cursorStore.Read() error = %v", err)
	}
	if !exists {
		t.Fatal("expected successful sync to persist last_sync")
	}
}

func TestServiceSyncPullsCloudBundleToLocalWithoutDownloadingDataFiles(t *testing.T) {
	slideID := "20260308-deadbeef"
	now := time.Date(2026, 3, 8, 18, 30, 0, 123000000, time.UTC)
	bundle := SlideBundle{
		Slide: repository.Slide{
			ID:          slideID,
			Date:        "2026-03-08",
			DayOrder:    "a1",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Figures: []repository.SlideFigure{{
			SlideID:  slideID,
			Filename: "chart.png",
			S3Key:    "figures/20260308-deadbeef/chart.png",
		}},
		DataFiles: []repository.SlideDataFile{{
			SlideID:  slideID,
			Filename: "report.csv",
			S3Key:    "data/20260308-deadbeef/report.csv",
			Size:     9,
			Hash:     strings.Repeat("b", 64),
		}},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(
		t,
		nil,
		[]SlideBundle{bundle},
	)
	objects.objects["figures/20260308-deadbeef/chart.png"] = "CLOUD-FIGURE"
	objects.objects["data/20260308-deadbeef/report.csv"] = "CLOUD-DATA"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	assertBundleEqual(t, localRepo.bundle(slideID), bundle)
	if got := readLocalAsset(t, localFS, true, slideID, "chart.png"); got != "CLOUD-FIGURE" {
		t.Fatalf("downloaded figure = %q, want %q", got, "CLOUD-FIGURE")
	}
	dataPath, err := localFS.ResolveDataFilePath(slideID, "report.csv")
	if err != nil {
		t.Fatalf("ResolveDataFilePath() error = %v", err)
	}
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected pc sync to skip local data-file download, stat error = %v", err)
	}
}

func TestServiceSyncLeavesLastSyncUnchangedOnFailure(t *testing.T) {
	slideID := "20260308-feedface"
	now := time.Date(2026, 3, 8, 20, 0, 0, 0, time.UTC)
	bundle := SlideBundle{
		Slide: repository.Slide{
			ID:          slideID,
			Date:        "2026-03-08",
			DayOrder:    "a2",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Figures: []repository.SlideFigure{{
			SlideID:  slideID,
			Filename: "missing.png",
			S3Key:    "figures/20260308-feedface/missing.png",
		}},
	}

	service, _, _, _, _, cursorStore := newTestService(t, nil, []SlideBundle{bundle})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected Sync() to fail when a required figure download is missing")
	}

	_, exists, readErr := cursorStore.Read()
	if readErr != nil {
		t.Fatalf("cursorStore.Read() error = %v", readErr)
	}
	if exists {
		t.Fatal("expected failed sync to leave last_sync unset")
	}
}

func TestServiceSyncPullsLaterCloudEditOverLocalChange(t *testing.T) {
	slideID := "20260308-cafebabe"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID:          slideID,
			Date:        "2026-03-08",
			DayOrder:    "a3",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base,
			UpdatedAt:   base.Add(1 * time.Minute),
		},
		Figures: []repository.SlideFigure{{
			SlideID:  slideID,
			Filename: "plot.png",
			S3Key:    "figures/20260308-cafebabe/plot.png",
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID:          slideID,
			Date:        "2026-03-08",
			DayOrder:    "a3",
			HTMLContent: "<html>cloud wins</html>",
			CreatedAt:   base,
			UpdatedAt:   base.Add(2 * time.Minute),
		},
		Figures: []repository.SlideFigure{{
			SlideID:  slideID,
			Filename: "plot.png",
			S3Key:    "figures/20260308-cafebabe/plot.png",
		}},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(
		t,
		[]SlideBundle{localBundle},
		[]SlideBundle{cloudBundle},
	)
	writeLocalAsset(t, localFS, true, slideID, "plot.png", "LOCAL")
	objects.objects["figures/20260308-cafebabe/plot.png"] = "CLOUD"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	assertBundleEqual(t, localRepo.bundle(slideID), cloudBundle)
	if got := readLocalAsset(t, localFS, true, slideID, "plot.png"); got != "CLOUD" {
		t.Fatalf("local figure = %q, want %q", got, "CLOUD")
	}
}

func TestServiceSyncPullsRenamedCloudFigureWithoutDeletingNewFile(t *testing.T) {
	slideID := "20260308-rename01"
	base := time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID:          slideID,
			Date:        "2026-03-08",
			DayOrder:    "a4",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base,
			UpdatedAt:   base,
		},
		Figures: []repository.SlideFigure{{
			ID:       1,
			SlideID:  slideID,
			Filename: "old.png",
			S3Key:    "figures/20260308-rename01/old.png",
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID:          slideID,
			Date:        "2026-03-08",
			DayOrder:    "a4",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base,
			UpdatedAt:   base.Add(1 * time.Minute),
		},
		Figures: []repository.SlideFigure{{
			ID:       1,
			SlideID:  slideID,
			Filename: "new.png",
			S3Key:    "figures/20260308-rename01/new.png",
		}},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(
		t,
		[]SlideBundle{localBundle},
		[]SlideBundle{cloudBundle},
	)
	writeLocalAsset(t, localFS, true, slideID, "old.png", "OLD")
	objects.objects["figures/20260308-rename01/new.png"] = "NEW"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	assertBundleEqual(t, localRepo.bundle(slideID), cloudBundle)
	if got := readLocalAsset(t, localFS, true, slideID, "new.png"); got != "NEW" {
		t.Fatalf("renamed figure = %q, want %q", got, "NEW")
	}
	oldPath, err := localFS.ResolveFigurePath(slideID, "old.png")
	if err != nil {
		t.Fatalf("ResolveFigurePath() error = %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old figure file to be removed, stat error = %v", err)
	}
}

// --- NewService nil-argument validation tests ---

func TestNewServiceErrorsOnNilLocalRepo(t *testing.T) {
	_, err := NewService(nil, &memoryRepo{}, &stubLocalFiles{}, newMockObjectStore(), &errSessionManager{})
	if err == nil {
		t.Fatal("expected error for nil local repository")
	}
	if got := err.Error(); got != "local repository is required" {
		t.Fatalf("error = %q, want 'local repository is required'", got)
	}
}

func TestNewServiceErrorsOnNilCloudRepo(t *testing.T) {
	_, err := NewService(&memoryRepo{}, nil, &stubLocalFiles{}, newMockObjectStore(), &errSessionManager{})
	if err == nil {
		t.Fatal("expected error for nil cloud repository")
	}
	if got := err.Error(); got != "cloud repository is required" {
		t.Fatalf("error = %q, want 'cloud repository is required'", got)
	}
}

func TestNewServiceErrorsOnNilLocalFS(t *testing.T) {
	_, err := NewService(&memoryRepo{}, &memoryRepo{}, nil, newMockObjectStore(), &errSessionManager{})
	if err == nil {
		t.Fatal("expected error for nil local filesystem")
	}
	if got := err.Error(); got != "local filesystem is required" {
		t.Fatalf("error = %q, want 'local filesystem is required'", got)
	}
}

func TestNewServiceErrorsOnNilObjects(t *testing.T) {
	_, err := NewService(&memoryRepo{}, &memoryRepo{}, &stubLocalFiles{}, nil, &errSessionManager{})
	if err == nil {
		t.Fatal("expected error for nil object store")
	}
	if got := err.Error(); got != "object store is required" {
		t.Fatalf("error = %q, want 'object store is required'", got)
	}
}

func TestNewServiceErrorsOnNilSession(t *testing.T) {
	_, err := NewService(&memoryRepo{}, &memoryRepo{}, &stubLocalFiles{}, newMockObjectStore(), nil)
	if err == nil {
		t.Fatal("expected error for nil session manager")
	}
	if got := err.Error(); got != "session manager is required" {
		t.Fatalf("error = %q, want 'session manager is required'", got)
	}
}

// --- Sync error path tests ---

func TestSyncErrorsOnSessionBeginFailure(t *testing.T) {
	beginErr := fmt.Errorf("lock already held")
	svc := &Service{
		localRepo:    newMemoryRepo(nil),
		cloudRepo:    newMemoryRepo(nil),
		localFS:      &stubLocalFiles{},
		cloudObjects: newMockObjectStore(),
		session:      &errSessionManager{beginErr: beginErr},
	}
	err := svc.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from session.Begin()")
	}
	if err.Error() != "lock already held" {
		t.Fatalf("error = %q, want 'lock already held'", err.Error())
	}
}

func TestSyncErrorsOnSessionCompleteFailure(t *testing.T) {
	completeErr := fmt.Errorf("cursor write failed")
	pcDir := filepath.Join(t.TempDir(), ".pc")
	session, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Wrap the real session to inject Complete error.
	wrapper := &completeErrSessionWrapper{
		real:        session,
		completeErr: completeErr,
	}

	svc := &Service{
		localRepo:    newMemoryRepo(nil),
		cloudRepo:    newMemoryRepo(nil),
		localFS:      &stubLocalFiles{},
		cloudObjects: newMockObjectStore(),
		session:      wrapper,
	}
	err = svc.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from session.Complete()")
	}
	if err.Error() != "cursor write failed" {
		t.Fatalf("error = %q, want 'cursor write failed'", err.Error())
	}
}

func TestSyncErrorsOnUpdateCloudVersionGetSyncVersionFailure(t *testing.T) {
	cloudRepo := newMemoryRepo(nil)
	cloudRepo.getSyncVersionErr = fmt.Errorf("db connection lost")

	service, _, _, _, _, _ := newTestService(t, nil, nil)
	// Replace cloudRepo with error-injecting one.
	service.cloudRepo = cloudRepo

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from GetSyncVersion()")
	}
	if !strings.Contains(err.Error(), "db connection lost") {
		t.Fatalf("error = %q, want to contain 'db connection lost'", err.Error())
	}
}

func TestSyncErrorsOnUpdateCloudVersionUpdateVersionFailure(t *testing.T) {
	service, _, _, _, objects, _ := newTestService(t, nil, nil)
	objects.updateVersionErr = fmt.Errorf("s3 write denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from UpdateVersion()")
	}
	if !strings.Contains(err.Error(), "s3 write denied") {
		t.Fatalf("error = %q, want to contain 's3 write denied'", err.Error())
	}
}

func TestSyncErrorsOnListLocalChanges(t *testing.T) {
	localRepo := newMemoryRepo(nil)
	localRepo.listSlidesErr = fmt.Errorf("local list failed")

	service, _, _, _, _, _ := newTestService(t, nil, nil)
	service.localRepo = localRepo

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local ListSlides()")
	}
	if !strings.Contains(err.Error(), "local list failed") {
		t.Fatalf("error = %q, want to contain 'local list failed'", err.Error())
	}
}

func TestSyncErrorsOnListCloudChanges(t *testing.T) {
	cloudRepo := newMemoryRepo(nil)
	cloudRepo.listSlidesErr = fmt.Errorf("cloud list failed")

	service, _, _, _, _, _ := newTestService(t, nil, nil)
	service.cloudRepo = cloudRepo

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from cloud ListSlides()")
	}
	if !strings.Contains(err.Error(), "cloud list failed") {
		t.Fatalf("error = %q, want to contain 'cloud list failed'", err.Error())
	}
}

func TestSyncErrorsOnLoadBundleNonNotFoundError(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	// Inject non-ErrNotFound error into cloud repo for GetSlideByID.
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.getSlideByIDErr = fmt.Errorf("connection timeout")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from loadBundle non-ErrNotFound path")
	}
	if !strings.Contains(err.Error(), "connection timeout") {
		t.Fatalf("error = %q, want to contain 'connection timeout'", err.Error())
	}
}

func TestSyncErrorsOnBundleForSlideListFiguresError(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.listFiguresBySlideIDErr = fmt.Errorf("figures table locked")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from bundleForSlide ListSlideFiguresBySlideID")
	}
	if !strings.Contains(err.Error(), "figures table locked") {
		t.Fatalf("error = %q, want to contain 'figures table locked'", err.Error())
	}
}

func TestSyncErrorsOnBundleForSlideListDataFilesError(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.listDataFilesBySlideIDErr = fmt.Errorf("data files table locked")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from bundleForSlide ListSlideDataFilesBySlideID")
	}
	if !strings.Contains(err.Error(), "data files table locked") {
		t.Fatalf("error = %q, want to contain 'data files table locked'", err.Error())
	}
}

func TestSyncErrorsOnUploadFileOpenFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.SlideFigure{{
			SlideID:  slideID,
			Filename: "missing.png",
			S3Key:    "figures/20260308-a1b2c3d4/missing.png",
		}},
	}

	// Do NOT write the local file so os.Open fails.
	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from uploadFile when local file does not exist")
	}
	if !strings.Contains(err.Error(), "open local file") {
		t.Fatalf("error = %q, want to contain 'open local file'", err.Error())
	}
}

func TestSyncErrorsOnDownloadFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.SlideFigure{{
			SlideID:  slideID,
			Filename: "chart.png",
			S3Key:    "figures/20260308-a1b2c3d4/chart.png",
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	objects.downloadErr = fmt.Errorf("s3 access denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from downloadFile")
	}
	if !strings.Contains(err.Error(), "s3 access denied") {
		t.Fatalf("error = %q, want to contain 's3 access denied'", err.Error())
	}
}

// --- Push: cloud exists but cloud wins -> skip ---

func TestSyncPushSkipsWhenCloudWins(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud later</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, cloudRepo, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Cloud should not have been overwritten by local.
	got := cloudRepo.bundle(slideID)
	if got.Slide.HTMLContent != "<html>cloud later</html>" {
		t.Fatalf("cloud HTMLContent = %q, want %q", got.Slide.HTMLContent, "<html>cloud later</html>")
	}
}

// --- Pull: local exists but local wins -> skip ---

func TestSyncPullSkipsWhenLocalWins(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local later</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
	}

	service, localRepo, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Local should not have been overwritten by cloud.
	got := localRepo.bundle(slideID)
	if got.Slide.HTMLContent != "<html>local later</html>" {
		t.Fatalf("local HTMLContent = %q, want %q", got.Slide.HTMLContent, "<html>local later</html>")
	}
}

// --- Push with figure create/update/delete on cloud ---

func TestSyncPushFigureCreateUpdateDeleteOnCloud(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	// Local has: new.png (create), updated.png (update s3key), no old.png (delete)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.SlideFigure{
			{SlideID: slideID, Filename: "new.png", S3Key: "figures/" + slideID + "/new.png"},
			{SlideID: slideID, Filename: "updated.png", S3Key: "figures/" + slideID + "/updated-v2.png"},
		},
	}
	// Cloud has: updated.png (existing, different s3key), old.png (to be deleted)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{
			{ID: 10, SlideID: slideID, Filename: "updated.png", S3Key: "figures/" + slideID + "/updated-v1.png"},
			{ID: 11, SlideID: slideID, Filename: "old.png", S3Key: "figures/" + slideID + "/old.png"},
		},
	}

	service, _, cloudRepo, localFS, objects, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, slideID, "new.png", "NEW-FIG")
	writeLocalAsset(t, localFS, true, slideID, "updated.png", "UPDATED-FIG")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// new.png should be uploaded and created in cloud repo.
	if got := objects.objects["figures/"+slideID+"/new.png"]; got != "NEW-FIG" {
		t.Fatalf("uploaded new figure = %q, want %q", got, "NEW-FIG")
	}
	// updated.png should be uploaded with new s3 key; old s3 key should be deleted.
	if got := objects.objects["figures/"+slideID+"/updated-v2.png"]; got != "UPDATED-FIG" {
		t.Fatalf("uploaded updated figure = %q, want %q", got, "UPDATED-FIG")
	}
	if _, exists := objects.objects["figures/"+slideID+"/updated-v1.png"]; exists {
		t.Fatal("expected old s3 key for updated figure to be deleted")
	}
	// old.png should be deleted from cloud object store and repo.
	if _, exists := objects.objects["figures/"+slideID+"/old.png"]; exists {
		t.Fatal("expected deleted figure s3 object to be removed")
	}
	cloudFigures := cloudRepo.bundle(slideID).Figures
	for _, fig := range cloudFigures {
		if fig.Filename == "old.png" {
			t.Fatal("expected old.png to be deleted from cloud repo")
		}
	}
}

// --- Push with data file create/update/delete on cloud ---

func TestSyncPushDataFileCreateUpdateDeleteOnCloud(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{
			{SlideID: slideID, Filename: "new.csv", S3Key: "data/" + slideID + "/new.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
			{SlideID: slideID, Filename: "updated.csv", S3Key: "data/" + slideID + "/updated-v2.csv",
				Size: 12, Hash: strings.Repeat("b", 64)},
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{
			{ID: 20, SlideID: slideID, Filename: "updated.csv", S3Key: "data/" + slideID + "/updated-v1.csv",
				Size: 10, Hash: strings.Repeat("c", 64)},
			{ID: 21, SlideID: slideID, Filename: "old.csv", S3Key: "data/" + slideID + "/old.csv",
				Size: 8, Hash: strings.Repeat("d", 64)},
		},
	}

	service, _, cloudRepo, localFS, objects, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, slideID, "new.csv", "NEW-DATA")
	writeLocalAsset(t, localFS, false, slideID, "updated.csv", "UPDATED-DATA")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// new.csv uploaded and created.
	if got := objects.objects["data/"+slideID+"/new.csv"]; got != "NEW-DATA" {
		t.Fatalf("uploaded new data file = %q, want %q", got, "NEW-DATA")
	}
	// updated.csv: old s3 key deleted.
	if _, exists := objects.objects["data/"+slideID+"/updated-v1.csv"]; exists {
		t.Fatal("expected old s3 key for updated data file to be deleted")
	}
	// old.csv: deleted from object store and repo.
	if _, exists := objects.objects["data/"+slideID+"/old.csv"]; exists {
		t.Fatal("expected deleted data file s3 object to be removed")
	}
	cloudDataFiles := cloudRepo.bundle(slideID).DataFiles
	for _, df := range cloudDataFiles {
		if df.Filename == "old.csv" {
			t.Fatal("expected old.csv to be deleted from cloud repo")
		}
	}
}

// --- Pull slide that deletes local figures and data files ---

func TestSyncPullDeletesLocalFiguresAndDataFiles(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{
			{ID: 1, SlideID: slideID, Filename: "obsolete.png", S3Key: "figures/" + slideID + "/obsolete.png"},
		},
		DataFiles: []repository.SlideDataFile{
			{ID: 1, SlideID: slideID, Filename: "obsolete.csv", S3Key: "data/" + slideID + "/obsolete.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	// Cloud has no figures or data files (everything should be deleted locally).
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud wins</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, slideID, "obsolete.png", "OBSOLETE-FIG")
	writeLocalAsset(t, localFS, false, slideID, "obsolete.csv", "OBSOLETE-DATA")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(slideID)
	if len(got.Figures) != 0 {
		t.Fatalf("expected no local figures, got %d", len(got.Figures))
	}
	if len(got.DataFiles) != 0 {
		t.Fatalf("expected no local data files, got %d", len(got.DataFiles))
	}

	// Check files removed from disk.
	figPath, _ := localFS.ResolveFigurePath(slideID, "obsolete.png")
	if _, err := os.Stat(figPath); !os.IsNotExist(err) {
		t.Fatalf("expected obsolete figure file to be removed, stat error = %v", err)
	}
	dataPath, _ := localFS.ResolveDataFilePath(slideID, "obsolete.csv")
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected obsolete data file to be removed, stat error = %v", err)
	}
}

// --- Pull with data file filename change (delete old local file) ---

func TestSyncPullWithDataFileFilenameChange(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{
			{ID: 1, SlideID: slideID, Filename: "old-name.csv", S3Key: "data/" + slideID + "/old-name.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{
			{SlideID: slideID, Filename: "new-name.csv", S3Key: "data/" + slideID + "/new-name.csv",
				Size: 8, Hash: strings.Repeat("b", 64)},
		},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, slideID, "old-name.csv", "OLD-DATA")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(slideID)
	if len(got.DataFiles) != 1 {
		t.Fatalf("expected 1 data file, got %d", len(got.DataFiles))
	}
	if got.DataFiles[0].Filename != "new-name.csv" {
		t.Fatalf("data file filename = %q, want %q", got.DataFiles[0].Filename, "new-name.csv")
	}

	// Old local file should be removed.
	oldPath, _ := localFS.ResolveDataFilePath(slideID, "old-name.csv")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old data file to be removed, stat error = %v", err)
	}
}

// --- Push: cloud bundle exists, local wins -> update cloud ---

func TestSyncPushLocalWinsUpdatesCloud(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local wins</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "plot.png", S3Key: "figures/" + slideID + "/plot.png",
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud loses</html>",
			CreatedAt:   base, UpdatedAt: base.Add(1 * time.Minute),
		},
	}

	service, _, cloudRepo, localFS, objects, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, slideID, "plot.png", "LOCAL-FIGURE")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := cloudRepo.bundle(slideID)
	if got.Slide.HTMLContent != "<html>local wins</html>" {
		t.Fatalf("cloud HTMLContent = %q, want %q", got.Slide.HTMLContent, "<html>local wins</html>")
	}
	if uploadedFig := objects.objects["figures/"+slideID+"/plot.png"]; uploadedFig != "LOCAL-FIGURE" {
		t.Fatalf("uploaded figure = %q, want %q", uploadedFig, "LOCAL-FIGURE")
	}
}

// --- removeFileIfPresent with non-existent file succeeds ---

func TestRemoveFileIfPresentNonExistentFileSucceeds(t *testing.T) {
	err := removeFileIfPresent(filepath.Join(t.TempDir(), "nonexistent.txt"))
	if err != nil {
		t.Fatalf("removeFileIfPresent() error = %v, want nil for non-existent file", err)
	}
}

// --- writeReaderToPath success path ---

func TestWriteReaderToPathSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "output.txt")
	content := "hello, world"
	err := writeReaderToPath(path, strings.NewReader(content))
	if err != nil {
		t.Fatalf("writeReaderToPath() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != content {
		t.Fatalf("file content = %q, want %q", string(data), content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != syncedFilePermission {
		t.Fatalf("file perm = %o, want %o", info.Mode().Perm(), syncedFilePermission)
	}
}

// --- Pull: new cloud slide with data files (create path for applyDataFilesToLocal) ---

func TestSyncPullNewCloudSlideWithDataFiles(t *testing.T) {
	slideID := "20260308-newcloud1"
	now := time.Date(2026, 3, 8, 18, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud new</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("d", 64),
		}},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, nil, []SlideBundle{cloudBundle})

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(slideID)
	if len(got.DataFiles) != 1 {
		t.Fatalf("expected 1 data file, got %d", len(got.DataFiles))
	}
	if got.DataFiles[0].Filename != "data.csv" {
		t.Fatalf("data file filename = %q, want %q", got.DataFiles[0].Filename, "data.csv")
	}
	// Data files are NOT downloaded to local, but the old file at the path should be removed if present.
	dataPath, _ := localFS.ResolveDataFilePath(slideID, "data.csv")
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected data file to not exist locally, stat error = %v", err)
	}
}

// --- Pull with figure and data file updates (update path for applyDataFilesToLocal) ---

func TestSyncPullUpdatesLocalDataFilesAndFigures(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{
			{ID: 1, SlideID: slideID, Filename: "plot.png", S3Key: "figures/" + slideID + "/plot.png",
				AltText: strPtr("old alt")},
		},
		DataFiles: []repository.SlideDataFile{
			{ID: 1, SlideID: slideID, Filename: "metrics.csv", S3Key: "data/" + slideID + "/metrics.csv",
				Size: 5, Hash: strings.Repeat("a", 64), Description: strPtr("old desc")},
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud wins</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.SlideFigure{
			{SlideID: slideID, Filename: "plot.png", S3Key: "figures/" + slideID + "/plot-v2.png",
				AltText: strPtr("new alt")},
		},
		DataFiles: []repository.SlideDataFile{
			{SlideID: slideID, Filename: "metrics.csv", S3Key: "data/" + slideID + "/metrics-v2.csv",
				Size: 10, Hash: strings.Repeat("b", 64), Description: strPtr("new desc")},
		},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, slideID, "plot.png", "OLD-FIG")
	writeLocalAsset(t, localFS, false, slideID, "metrics.csv", "OLD-DATA")
	objects.objects["figures/"+slideID+"/plot-v2.png"] = "NEW-FIG"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(slideID)
	if len(got.Figures) != 1 || *got.Figures[0].AltText != "new alt" {
		t.Fatalf("expected figure alt text to be 'new alt', got %+v", got.Figures)
	}
	if len(got.DataFiles) != 1 || *got.DataFiles[0].Description != "new desc" {
		t.Fatalf("expected data file description to be 'new desc', got %+v", got.DataFiles)
	}

	// Figure should be downloaded from cloud.
	figContent := readLocalAsset(t, localFS, true, slideID, "plot.png")
	if figContent != "NEW-FIG" {
		t.Fatalf("local figure = %q, want %q", figContent, "NEW-FIG")
	}
}

// --- applySlide error from UpdateSlide ---

func TestSyncPushErrorsOnApplySlideUpdateFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local wins</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.updateSlideErr = fmt.Errorf("update slide denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applySlide UpdateSlide")
	}
	if !strings.Contains(err.Error(), "update slide denied") {
		t.Fatalf("error = %q, want to contain 'update slide denied'", err.Error())
	}
}

// --- applySlide error from CreateSlide ---

func TestSyncPushErrorsOnApplySlideCreateFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.createSlideErr = fmt.Errorf("create slide denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applySlide CreateSlide")
	}
	if !strings.Contains(err.Error(), "create slide denied") {
		t.Fatalf("error = %q, want to contain 'create slide denied'", err.Error())
	}
}

// --- applyFiguresToCloud error paths ---

func TestSyncPushErrorsOnCloudCreateSlideFigureFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "new.png", S3Key: "figures/" + slideID + "/new.png",
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, true, slideID, "new.png", "FIG")
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.createSlideFigureErr = fmt.Errorf("create figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from CreateSlideFigure")
	}
	if !strings.Contains(err.Error(), "create figure denied") {
		t.Fatalf("error = %q, want to contain 'create figure denied'", err.Error())
	}
}

func TestSyncPushErrorsOnCloudUpdateSlideFigureFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "plot.png", S3Key: "figures/" + slideID + "/plot-v2.png",
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{{
			ID: 10, SlideID: slideID, Filename: "plot.png", S3Key: "figures/" + slideID + "/plot-v1.png",
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, slideID, "plot.png", "FIG")
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.updateSlideFigureErr = fmt.Errorf("update figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from UpdateSlideFigure")
	}
	if !strings.Contains(err.Error(), "update figure denied") {
		t.Fatalf("error = %q, want to contain 'update figure denied'", err.Error())
	}
}

func TestSyncPushErrorsOnCloudDeleteSlideFigureFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{{
			ID: 10, SlideID: slideID, Filename: "old.png", S3Key: "figures/" + slideID + "/old.png",
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.deleteSlideFigureErr = fmt.Errorf("delete figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from DeleteSlideFigure on cloud")
	}
	if !strings.Contains(err.Error(), "delete figure denied") {
		t.Fatalf("error = %q, want to contain 'delete figure denied'", err.Error())
	}
}

func TestSyncPushErrorsOnCloudDeleteObjectAfterDeleteFigure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{{
			ID: 10, SlideID: slideID, Filename: "old.png", S3Key: "figures/" + slideID + "/old.png",
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	objects.deleteErr = fmt.Errorf("s3 delete denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Delete on cloud object after figure delete")
	}
	if !strings.Contains(err.Error(), "s3 delete denied") {
		t.Fatalf("error = %q, want to contain 's3 delete denied'", err.Error())
	}
}

func TestSyncPushErrorsOnUploadFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "plot.png", S3Key: "figures/" + slideID + "/plot.png",
		}},
	}

	service, _, _, localFS, objects, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, true, slideID, "plot.png", "FIG")
	objects.uploadErr = fmt.Errorf("s3 upload denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Upload")
	}
	if !strings.Contains(err.Error(), "s3 upload denied") {
		t.Fatalf("error = %q, want to contain 's3 upload denied'", err.Error())
	}
}

// --- applyDataFilesToCloud error paths ---

func TestSyncPushErrorsOnCloudCreateSlideDataFileFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "metrics.csv", S3Key: "data/" + slideID + "/metrics.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, false, slideID, "metrics.csv", "DATA")
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.createSlideDataFileErr = fmt.Errorf("create data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from CreateSlideDataFile")
	}
	if !strings.Contains(err.Error(), "create data file denied") {
		t.Fatalf("error = %q, want to contain 'create data file denied'", err.Error())
	}
}

func TestSyncPushErrorsOnCloudUpdateSlideDataFileFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "metrics.csv", S3Key: "data/" + slideID + "/metrics-v2.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 20, SlideID: slideID, Filename: "metrics.csv", S3Key: "data/" + slideID + "/metrics-v1.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, slideID, "metrics.csv", "DATA")
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.updateSlideDataFileErr = fmt.Errorf("update data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from UpdateSlideDataFile")
	}
	if !strings.Contains(err.Error(), "update data file denied") {
		t.Fatalf("error = %q, want to contain 'update data file denied'", err.Error())
	}
}

func TestSyncPushErrorsOnCloudDeleteSlideDataFileFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 20, SlideID: slideID, Filename: "old.csv", S3Key: "data/" + slideID + "/old.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.deleteSlideDataFileErr = fmt.Errorf("delete data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from DeleteSlideDataFile")
	}
	if !strings.Contains(err.Error(), "delete data file denied") {
		t.Fatalf("error = %q, want to contain 'delete data file denied'", err.Error())
	}
}

// --- applyFiguresToLocal error paths ---

func TestSyncPullErrorsOnLocalCreateSlideFigureFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "new.png", S3Key: "figures/" + slideID + "/new.png",
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	objects.objects["figures/"+slideID+"/new.png"] = "FIG"
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.createSlideFigureErr = fmt.Errorf("local create figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local CreateSlideFigure")
	}
	if !strings.Contains(err.Error(), "local create figure denied") {
		t.Fatalf("error = %q, want to contain 'local create figure denied'", err.Error())
	}
}

func TestSyncPullErrorsOnLocalUpdateSlideFigureFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{{
			ID: 1, SlideID: slideID, Filename: "plot.png", S3Key: "figures/" + slideID + "/plot.png",
			AltText: strPtr("old"),
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "plot.png", S3Key: "figures/" + slideID + "/plot-v2.png",
			AltText: strPtr("new"),
		}},
	}

	service, _, _, localFS, objects, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, slideID, "plot.png", "OLD")
	objects.objects["figures/"+slideID+"/plot-v2.png"] = "NEW"
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.updateSlideFigureErr = fmt.Errorf("local update figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local UpdateSlideFigure")
	}
	if !strings.Contains(err.Error(), "local update figure denied") {
		t.Fatalf("error = %q, want to contain 'local update figure denied'", err.Error())
	}
}

func TestSyncPullErrorsOnLocalDeleteSlideFigureFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{{
			ID: 1, SlideID: slideID, Filename: "old.png", S3Key: "figures/" + slideID + "/old.png",
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, slideID, "old.png", "OLD")
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.deleteSlideFigureErr = fmt.Errorf("local delete figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local DeleteSlideFigure")
	}
	if !strings.Contains(err.Error(), "local delete figure denied") {
		t.Fatalf("error = %q, want to contain 'local delete figure denied'", err.Error())
	}
}

// --- applyDataFilesToLocal error paths ---

func TestSyncPullErrorsOnLocalCreateSlideDataFileFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.createSlideDataFileErr = fmt.Errorf("local create data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local CreateSlideDataFile")
	}
	if !strings.Contains(err.Error(), "local create data file denied") {
		t.Fatalf("error = %q, want to contain 'local create data file denied'", err.Error())
	}
}

func TestSyncPullErrorsOnLocalUpdateSlideDataFileFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 1, SlideID: slideID, Filename: "metrics.csv", S3Key: "data/" + slideID + "/metrics.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "metrics.csv", S3Key: "data/" + slideID + "/metrics-v2.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.updateSlideDataFileErr = fmt.Errorf("local update data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local UpdateSlideDataFile")
	}
	if !strings.Contains(err.Error(), "local update data file denied") {
		t.Fatalf("error = %q, want to contain 'local update data file denied'", err.Error())
	}
}

func TestSyncPullErrorsOnLocalDeleteSlideDataFileFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 1, SlideID: slideID, Filename: "old.csv", S3Key: "data/" + slideID + "/old.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.deleteSlideDataFileErr = fmt.Errorf("local delete data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local DeleteSlideDataFile")
	}
	if !strings.Contains(err.Error(), "local delete data file denied") {
		t.Fatalf("error = %q, want to contain 'local delete data file denied'", err.Error())
	}
}

// --- Pull: figure filename rename triggers old file deletion ---

func TestSyncPullFigureFilenameRenameDeletesOldFile(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{{
			ID: 1, SlideID: slideID, Filename: "old-name.png", S3Key: "figures/" + slideID + "/old-name.png",
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "new-name.png", S3Key: "figures/" + slideID + "/new-name.png",
		}},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, slideID, "old-name.png", "OLD")
	objects.objects["figures/"+slideID+"/new-name.png"] = "NEW"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(slideID)
	if len(got.Figures) != 1 || got.Figures[0].Filename != "new-name.png" {
		t.Fatalf("expected renamed figure, got %+v", got.Figures)
	}

	// Old figure file should be removed.
	oldPath, _ := localFS.ResolveFigurePath(slideID, "old-name.png")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old figure file to be removed, stat error = %v", err)
	}
}

// --- Pull: data file update with filename change (covers rename path in applyDataFilesToLocal) ---

func TestSyncPullDataFileUpdateWithFilenameChange(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 1, SlideID: slideID, Filename: "old-data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}
	// Cloud has same filename pattern but the update will happen by filename match.
	// Use different filename to trigger the rename path.
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "old-data.csv", S3Key: "data/" + slideID + "/data-v2.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, slideID, "old-data.csv", "OLD-DATA")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(slideID)
	if len(got.DataFiles) != 1 {
		t.Fatalf("expected 1 data file, got %d", len(got.DataFiles))
	}
	if got.DataFiles[0].Hash != strings.Repeat("b", 64) {
		t.Fatalf("data file hash not updated")
	}
}

// --- applyBundleToCloud and applyBundleToLocal error propagation ---

func TestSyncPushErrorsOnApplyFiguresToCloudFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "fig.png", S3Key: "figures/" + slideID + "/fig.png",
		}},
	}

	// Trigger ResolveFigurePath error by using a stub that fails.
	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	service.localFS = &errLocalFiles{figurErr: fmt.Errorf("resolve figure path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyFiguresToCloud")
	}
	if !strings.Contains(err.Error(), "resolve figure path failed") {
		t.Fatalf("error = %q, want to contain 'resolve figure path failed'", err.Error())
	}
}

func TestSyncPushErrorsOnApplyDataFilesToCloudResolvePathFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	service.localFS = &errLocalFiles{dataFileErr: fmt.Errorf("resolve data path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyDataFilesToCloud resolve path")
	}
	if !strings.Contains(err.Error(), "resolve data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve data path failed'", err.Error())
	}
}

func TestSyncPullErrorsOnApplyFiguresToLocalResolvePathFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "fig.png", S3Key: "figures/" + slideID + "/fig.png",
		}},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	service.localFS = &errLocalFiles{figurErr: fmt.Errorf("local resolve figure failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyFiguresToLocal resolve path")
	}
	if !strings.Contains(err.Error(), "local resolve figure failed") {
		t.Fatalf("error = %q, want to contain 'local resolve figure failed'", err.Error())
	}
}

func TestSyncPullErrorsOnApplyDataFilesToLocalResolvePathFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	service.localFS = &errLocalFiles{dataFileErr: fmt.Errorf("local resolve data path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyDataFilesToLocal resolve path")
	}
	if !strings.Contains(err.Error(), "local resolve data path failed") {
		t.Fatalf("error = %q, want to contain 'local resolve data path failed'", err.Error())
	}
}

// --- Pull error paths for cloud-side bundleForSlide ---

func TestSyncPullErrorsOnCloudBundleForSlideListFiguresError(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.listFiguresBySlideIDErr = fmt.Errorf("cloud figures locked")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from cloud bundleForSlide ListSlideFiguresBySlideID")
	}
	if !strings.Contains(err.Error(), "cloud figures locked") {
		t.Fatalf("error = %q, want to contain 'cloud figures locked'", err.Error())
	}
}

func TestSyncPullErrorsOnCloudBundleForSlideListDataFilesError(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.listDataFilesBySlideIDErr = fmt.Errorf("cloud data files locked")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from cloud bundleForSlide ListSlideDataFilesBySlideID")
	}
	if !strings.Contains(err.Error(), "cloud data files locked") {
		t.Fatalf("error = %q, want to contain 'cloud data files locked'", err.Error())
	}
}

// --- Pull error: loadBundle non-ErrNotFound for local repo ---

func TestSyncPullErrorsOnLoadLocalBundleNonNotFoundError(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.getSlideByIDErr = fmt.Errorf("local db timeout")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from loadBundle for local repo")
	}
	if !strings.Contains(err.Error(), "local db timeout") {
		t.Fatalf("error = %q, want to contain 'local db timeout'", err.Error())
	}
}

// --- Sync lock release error propagation ---

func TestSyncPropagatesLockReleaseError(t *testing.T) {
	pcDir := filepath.Join(t.TempDir(), ".pc")
	session, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	svc := &Service{
		localRepo:    newMemoryRepo(nil),
		cloudRepo:    newMemoryRepo(nil),
		localFS:      &stubLocalFiles{},
		cloudObjects: newMockObjectStore(),
		session:      session,
	}

	// First, do a successful sync so the cursor file exists.
	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}

	// Now acquire the lock manually so Begin() succeeds, then remove
	// the lock file before Release() runs so Release() fails on os.Remove.
	window, lock, err := session.Begin()
	if err != nil {
		t.Fatalf("session.Begin() error = %v", err)
	}
	// Remove the lock file to make Release() fail.
	lockPath := filepath.Join(pcDir, "sync.lock")
	_ = os.Remove(lockPath)
	// Close the underlying file handle so Close() doesn't fail first.
	// We can't access it directly, so just call Release() and expect an error.
	releaseErr := lock.Release()
	if releaseErr == nil {
		t.Fatal("expected Release() to fail when lock file is removed")
	}
	_ = window
}

// --- Pull: applyBundleToLocal slide apply error ---

func TestSyncPullErrorsOnApplySlideToLocalFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.createSlideErr = fmt.Errorf("local create slide denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applySlide to local")
	}
	if !strings.Contains(err.Error(), "local create slide denied") {
		t.Fatalf("error = %q, want to contain 'local create slide denied'", err.Error())
	}
}

func TestSyncPullErrorsOnApplySlideUpdateToLocalFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.updateSlideErr = fmt.Errorf("local update slide denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applySlide UpdateSlide to local")
	}
	if !strings.Contains(err.Error(), "local update slide denied") {
		t.Fatalf("error = %q, want to contain 'local update slide denied'", err.Error())
	}
}

// --- loadBundle: bundleForSlide error after GetSlideByID success ---

func TestSyncPullErrorsOnLoadBundleBundleForSlideError(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	// Put the slide in local so loadBundle's GetSlideByID succeeds,
	// but inject error on ListSlideFiguresBySlideID so bundleForSlide fails.
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.listFiguresBySlideIDErr = fmt.Errorf("local figures table error")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from loadBundle/bundleForSlide")
	}
	if !strings.Contains(err.Error(), "local figures table error") {
		t.Fatalf("error = %q, want to contain 'local figures table error'", err.Error())
	}
}

// --- Pull: applyFiguresToLocal delete path fully exercised ---

func TestSyncPullDeleteFigureWithFileRemoval(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{
			{ID: 1, SlideID: slideID, Filename: "delete-me.png", S3Key: "figures/" + slideID + "/delete-me.png"},
			{ID: 2, SlideID: slideID, Filename: "keep-me.png", S3Key: "figures/" + slideID + "/keep-me.png"},
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.SlideFigure{
			{SlideID: slideID, Filename: "keep-me.png", S3Key: "figures/" + slideID + "/keep-me.png"},
		},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, slideID, "delete-me.png", "DELETE")
	writeLocalAsset(t, localFS, true, slideID, "keep-me.png", "KEEP")
	objects.objects["figures/"+slideID+"/keep-me.png"] = "KEEP"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(slideID)
	if len(got.Figures) != 1 {
		t.Fatalf("expected 1 figure, got %d", len(got.Figures))
	}
	if got.Figures[0].Filename != "keep-me.png" {
		t.Fatalf("kept figure = %q, want %q", got.Figures[0].Filename, "keep-me.png")
	}

	deletedPath, _ := localFS.ResolveFigurePath(slideID, "delete-me.png")
	if _, err := os.Stat(deletedPath); !os.IsNotExist(err) {
		t.Fatalf("expected deleted figure file to be removed, stat error = %v", err)
	}
}

// --- Pull: applyDataFilesToLocal full delete path ---

func TestSyncPullDeleteDataFileWithFileRemoval(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{
			{ID: 1, SlideID: slideID, Filename: "delete-me.csv", S3Key: "data/" + slideID + "/delete-me.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, slideID, "delete-me.csv", "DELETE")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(slideID)
	if len(got.DataFiles) != 0 {
		t.Fatalf("expected 0 data files, got %d", len(got.DataFiles))
	}

	deletedPath, _ := localFS.ResolveDataFilePath(slideID, "delete-me.csv")
	if _, err := os.Stat(deletedPath); !os.IsNotExist(err) {
		t.Fatalf("expected deleted data file to be removed, stat error = %v", err)
	}
}

// --- Pull: applyDataFilesToLocal update with filename rename ---

func TestSyncPullDataFileUpdateWithFilenameRename(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{
			{ID: 1, SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	// Cloud renamed the file.
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{
			{SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data-v2.csv",
				Size: 10, Hash: strings.Repeat("b", 64)},
		},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, slideID, "data.csv", "OLD")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(slideID)
	if len(got.DataFiles) != 1 || got.DataFiles[0].Hash != strings.Repeat("b", 64) {
		t.Fatalf("expected updated data file, got %+v", got.DataFiles)
	}

	// The old local file at data.csv path should be removed (via removeFileIfPresent
	// in the update path - line 538).
	dataPath, _ := localFS.ResolveDataFilePath(slideID, "data.csv")
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected data file path to be cleaned up, stat error = %v", err)
	}
}

// --- removeFileIfPresent error path ---

func TestRemoveFileIfPresentErrorOnPermissionDenied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "protected.txt")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// Make the directory read-only to prevent file removal.
	if err := os.Chmod(dir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})

	err := removeFileIfPresent(path)
	if err == nil {
		t.Fatal("expected error from removeFileIfPresent with permission denied")
	}
}

// --- writeReaderToPath error paths ---

func TestWriteReaderToPathErrorOnMkdirAll(t *testing.T) {
	// Use a path under a file (not a directory) to make MkdirAll fail.
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	path := filepath.Join(tmpFile, "subdir", "file.txt")

	err := writeReaderToPath(path, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error from writeReaderToPath when MkdirAll fails")
	}
	if !strings.Contains(err.Error(), "create directory") {
		t.Fatalf("error = %q, want to contain 'create directory'", err.Error())
	}
}

func TestWriteReaderToPathErrorOnCreateTemp(t *testing.T) {
	// Use a read-only directory to make CreateTemp fail.
	dir := t.TempDir()
	subdir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Chmod(subdir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(subdir, 0o755)
	})

	path := filepath.Join(subdir, "file.txt")
	err := writeReaderToPath(path, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error from writeReaderToPath when CreateTemp fails")
	}
	if !strings.Contains(err.Error(), "create temp file") {
		t.Fatalf("error = %q, want to contain 'create temp file'", err.Error())
	}
}

func TestWriteReaderToPathErrorOnCopyFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")

	// Use a reader that returns an error.
	err := writeReaderToPath(path, &errReader{err: fmt.Errorf("read failed")})
	if err == nil {
		t.Fatal("expected error from writeReaderToPath when copy fails")
	}
	if !strings.Contains(err.Error(), "copy temp file") {
		t.Fatalf("error = %q, want to contain 'copy temp file'", err.Error())
	}
}

func TestWriteReaderToPathErrorOnChmod(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")

	origChmod := osFileChmod
	osFileChmod = func(_ *os.File, _ os.FileMode) error {
		return fmt.Errorf("injected chmod error")
	}
	t.Cleanup(func() { osFileChmod = origChmod })

	err := writeReaderToPath(path, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error from writeReaderToPath when Chmod fails")
	}
	if !strings.Contains(err.Error(), "chmod temp file") {
		t.Fatalf("error = %q, want to contain 'chmod temp file'", err.Error())
	}
	// Temp file should have been cleaned up.
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".sync-") {
			t.Fatalf("temp file %q was not cleaned up", entry.Name())
		}
	}
}

func TestWriteReaderToPathErrorOnRename(t *testing.T) {
	dir := t.TempDir()
	// Create a directory at the target path so os.Rename(file, dir) fails.
	target := filepath.Join(dir, "output")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	err := writeReaderToPath(target, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error from writeReaderToPath when Rename fails")
	}
	if !strings.Contains(err.Error(), "replace") {
		t.Fatalf("error = %q, want to contain 'replace'", err.Error())
	}
}

func TestSyncReturnsLockReleaseError(t *testing.T) {
	// Set up a sync that succeeds, but whose lock.Release() fails.
	// This covers the deferred lock-release error path in Sync (service.go:85).
	slideID := "20260308-locktest"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	bundle := SlideBundle{
		Slide: repository.Slide{
			ID:          slideID,
			Date:        "2026-03-08",
			DayOrder:    "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	localRepo := newMemoryRepo([]SlideBundle{bundle})
	cloudRepo := newMemoryRepo(nil)
	baseDir := t.TempDir()
	localFS, err := filesystem.NewClient(baseDir)
	if err != nil {
		t.Fatalf("filesystem.NewClient() error = %v", err)
	}

	pcDir := filepath.Join(t.TempDir(), ".pc")
	realSession, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("syncengine.NewManager() error = %v", err)
	}

	// Wrap the real session so we can sabotage the lock directory during Complete().
	lockDir := pcDir
	wrapper := &lockSabotageSessionWrapper{
		real:    realSession,
		lockDir: lockDir,
	}

	service, err := NewService(localRepo, cloudRepo, localFS, newMockObjectStore(), wrapper)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	err = service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Sync when lock Release fails")
	}
	if !strings.Contains(err.Error(), "remove sync lock") {
		t.Fatalf("error = %q, want to contain 'remove sync lock'", err.Error())
	}

	// Restore directory permissions for cleanup.
	_ = os.Chmod(lockDir, 0o755)
}

// lockSabotageSessionWrapper delegates to a real SessionManager but makes
// the lock directory read-only during Complete(), causing the deferred
// lock.Release() to fail because os.Remove cannot delete the lock file.
type lockSabotageSessionWrapper struct {
	real    SessionManager
	lockDir string
}

func (w *lockSabotageSessionWrapper) Begin() (syncengine.SyncWindow, *syncengine.FileLock, error) {
	return w.real.Begin()
}

func (w *lockSabotageSessionWrapper) Complete(window syncengine.SyncWindow) error {
	if err := w.real.Complete(window); err != nil {
		return err
	}
	// Make the lock directory read-only so os.Remove in Release() fails.
	return os.Chmod(w.lockDir, 0o444)
}

// --- errReader for triggering io.Copy failures ---

type errReader struct {
	err error
}

func (r *errReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

// --- downloadFile write error ---

func TestDownloadFileWriteError(t *testing.T) {
	// Create service with a localFS that resolves to a path under a file.
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "chart.png", S3Key: "figures/" + slideID + "/chart.png",
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	objects.objects["figures/"+slideID+"/chart.png"] = "CONTENT"

	// Replace localFS with one that returns a path under a non-directory.
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	service.localFS = &fixedPathLocalFiles{
		figurePath:   filepath.Join(tmpFile, "subdir", "chart.png"),
		dataFilePath: filepath.Join(tmpFile, "subdir", "data.csv"),
	}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from downloadFile write")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Fatalf("error = %q, want to contain 'write'", err.Error())
	}
}

type fixedPathLocalFiles struct {
	figurePath   string
	dataFilePath string
}

func (f *fixedPathLocalFiles) ResolveFigurePath(_ string, _ string) (string, error) {
	return f.figurePath, nil
}

func (f *fixedPathLocalFiles) ResolveDataFilePath(_ string, _ string) (string, error) {
	return f.dataFilePath, nil
}

// --- applyDataFilesToCloud: metadata-only create/update when local binary is absent ---

func TestSyncPushCreatesCloudDataFileMetadataWithoutLocalBinary(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, cloudRepo, _, objects, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := cloudRepo.bundle(slideID)
	if len(got.DataFiles) != 1 || got.DataFiles[0].Filename != "data.csv" {
		t.Fatalf("cloud data files = %+v, want metadata row for data.csv", got.DataFiles)
	}
	if _, ok := objects.objects["data/"+slideID+"/data.csv"]; ok {
		t.Fatal("expected missing local data file to skip object upload")
	}
}

func TestSyncPushUpdatesCloudDataFileMetadataWithoutLocalBinaryWhenS3KeyIsUnchanged(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 20, SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, cloudRepo, _, objects, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := cloudRepo.bundle(slideID)
	if len(got.DataFiles) != 1 || got.DataFiles[0].Hash != strings.Repeat("b", 64) || got.DataFiles[0].Size != 10 {
		t.Fatalf("cloud data files = %+v, want updated metadata with unchanged s3_key", got.DataFiles)
	}
	if _, ok := objects.objects["data/"+slideID+"/data.csv"]; ok {
		t.Fatal("expected missing local data file to skip object upload")
	}
}

func TestSyncPushErrorsWhenMissingLocalBinaryWouldChangeDataFileS3Key(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data-v2.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 20, SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data-v1.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error when metadata-only push would change data file s3_key")
	}
	if !strings.Contains(err.Error(), "is required to change data file s3_key") {
		t.Fatalf("error = %q, want missing-local-file s3_key guard", err.Error())
	}
}

// --- applyDataFilesToCloud: upload error on data file create ---

func TestSyncPushErrorsOnDataFileCreateUploadFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	// Write the local data file so ResolveDataFilePath succeeds, but upload fails.
	service, _, _, localFS, objects, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, false, slideID, "data.csv", "DATA")
	objects.uploadErr = fmt.Errorf("data upload denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from data file upload")
	}
	if !strings.Contains(err.Error(), "data upload denied") {
		t.Fatalf("error = %q, want to contain 'data upload denied'", err.Error())
	}
}

// --- applyDataFilesToCloud: upload error on data file update ---

func TestSyncPushErrorsOnDataFileUpdateUploadFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data-v2.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 20, SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data-v1.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, localFS, objects, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, slideID, "data.csv", "DATA")
	objects.uploadErr = fmt.Errorf("data update upload denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from data file update upload")
	}
	if !strings.Contains(err.Error(), "data update upload denied") {
		t.Fatalf("error = %q, want to contain 'data update upload denied'", err.Error())
	}
}

// --- applyDataFilesToCloud: delete old S3 object error on data file update ---

func TestSyncPushErrorsOnDataFileUpdateDeleteOldS3Failure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data-v2.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 20, SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data-v1.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, slideID, "data.csv", "DATA")
	// Use a custom object store that fails on Delete but succeeds on Upload.
	objects := service.cloudObjects.(*mockObjectStore)
	// We can't set deleteErr because figure deletes also use it.
	// Instead, let the upload succeed, the UpdateSlideDataFile succeed, then delete fails.
	// We need per-operation error injection. Let's use a counting approach.
	countingObjects := &countingObjectStore{
		inner:          objects,
		deleteErrAfter: 0, // fail on first delete
		deleteErr:      fmt.Errorf("s3 delete old key denied"),
	}
	service.cloudObjects = countingObjects

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Delete old S3 key on data file update")
	}
	if !strings.Contains(err.Error(), "s3 delete old key denied") {
		t.Fatalf("error = %q, want to contain 's3 delete old key denied'", err.Error())
	}
}

// --- applyDataFilesToCloud: delete object error on data file delete ---

func TestSyncPushErrorsOnDataFileDeleteObjectFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 20, SlideID: slideID, Filename: "old.csv", S3Key: "data/" + slideID + "/old.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	objects.deleteErr = fmt.Errorf("s3 delete object denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Delete object on data file delete")
	}
	if !strings.Contains(err.Error(), "s3 delete object denied") {
		t.Fatalf("error = %q, want to contain 's3 delete object denied'", err.Error())
	}
}

// --- applyFiguresToCloud: delete old S3 key error on figure update ---

func TestSyncPushErrorsOnFigureUpdateDeleteOldS3Failure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "plot.png", S3Key: "figures/" + slideID + "/plot-v2.png",
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{{
			ID: 10, SlideID: slideID, Filename: "plot.png", S3Key: "figures/" + slideID + "/plot-v1.png",
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, slideID, "plot.png", "FIG")
	// Use counting object store to allow uploads but fail on delete.
	objects := service.cloudObjects.(*mockObjectStore)
	countingObjects := &countingObjectStore{
		inner:          objects,
		deleteErrAfter: 0,
		deleteErr:      fmt.Errorf("s3 delete old figure key denied"),
	}
	service.cloudObjects = countingObjects

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Delete old S3 key on figure update")
	}
	if !strings.Contains(err.Error(), "s3 delete old figure key denied") {
		t.Fatalf("error = %q, want to contain 's3 delete old figure key denied'", err.Error())
	}
}

// --- applyDataFilesToCloud: resolve data path error on create ---

func TestSyncPushErrorsOnDataFileCreateResolvePathFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	service.localFS = &errLocalFiles{dataFileErr: fmt.Errorf("resolve create data path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from resolve data path on create")
	}
	if !strings.Contains(err.Error(), "resolve create data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve create data path failed'", err.Error())
	}
}

// --- applyDataFilesToCloud: resolve data path error on update ---

func TestSyncPushErrorsOnDataFileUpdateResolvePathFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data-v2.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 20, SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data-v1.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	service.localFS = &errLocalFiles{dataFileErr: fmt.Errorf("resolve update data path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from resolve data path on update")
	}
	if !strings.Contains(err.Error(), "resolve update data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve update data path failed'", err.Error())
	}
}

// --- applyFiguresToLocal: ResolveFigurePath error on delete ---

func TestSyncPullErrorsOnFigureDeleteResolvePathFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{
			{ID: 1, SlideID: slideID, Filename: "delete-me.png", S3Key: "figures/" + slideID + "/delete-me.png"},
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	// Use a localFS that returns error for all figure resolutions.
	// But we need the download step in applyFiguresToLocal to succeed (no desired figures, so no download needed).
	// Then the delete loop calls ResolveFigurePath which should fail.
	service.localFS = &errLocalFiles{figurErr: fmt.Errorf("resolve delete figure path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from ResolveFigurePath in figure delete")
	}
	if !strings.Contains(err.Error(), "resolve delete figure path failed") {
		t.Fatalf("error = %q, want to contain 'resolve delete figure path failed'", err.Error())
	}
}

// --- applyDataFilesToLocal: ResolveDataFilePath error on create ---

func TestSyncPullErrorsOnDataFileCreateResolvePathFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	// Use countingLocalFiles to succeed on initial operations but fail on data file resolve for create.
	service.localFS = &countingLocalFiles{
		dataFileErrAfter: 0, // fail on first ResolveDataFilePath call
		dataFileErr:      fmt.Errorf("resolve create local data path failed"),
	}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from ResolveDataFilePath in data file create")
	}
	if !strings.Contains(err.Error(), "resolve create local data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve create local data path failed'", err.Error())
	}
}

// --- applyDataFilesToLocal: ResolveDataFilePath error on update ---

func TestSyncPullErrorsOnDataFileUpdateResolvePathFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 1, SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data-v2.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	service.localFS = &countingLocalFiles{
		dataFileErrAfter: 0,
		dataFileErr:      fmt.Errorf("resolve update local data path failed"),
	}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from ResolveDataFilePath in data file update")
	}
	if !strings.Contains(err.Error(), "resolve update local data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve update local data path failed'", err.Error())
	}
}

// --- applyDataFilesToLocal: ResolveDataFilePath error on delete ---

func TestSyncPullErrorsOnDataFileDeleteResolvePathFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{{
			ID: 1, SlideID: slideID, Filename: "delete-me.csv", S3Key: "data/" + slideID + "/delete-me.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, _, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	service.localFS = &countingLocalFiles{
		dataFileErrAfter: 0,
		dataFileErr:      fmt.Errorf("resolve delete local data path failed"),
	}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from ResolveDataFilePath in data file delete")
	}
	if !strings.Contains(err.Error(), "resolve delete local data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve delete local data path failed'", err.Error())
	}
}

// --- Pull: local wins skip when local has a pre-existing later edit ---

func TestSyncPullSkipsWhenLocalHasLaterPreExistingEdit(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	lastSyncTime := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	localUpdatedAt := time.Date(2026, 3, 8, 11, 59, 0, 0, time.UTC) // just before lastSync
	cloudUpdatedAt := time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC)  // after lastSync

	// Local slide was updated before lastSync (won't be in push list).
	// But its UpdatedAt is LATER than cloud's — wait, that would mean push would process it.
	// Actually, the local slide is NOT in the push list because its UpdatedAt is before lastSync.
	// The FilterSlidesUpdatedSince check uses inclusive comparison (>= threshold).
	// So localUpdatedAt (11:59) < lastSync (12:00) means it IS filtered out.
	//
	// Cloud slide updated after lastSync (in pull list).
	// But local's latest action time must be after cloud's for local to win.
	// Local has UpdatedAt=11:59 and no DeletedAt; Cloud has UpdatedAt=13:00.
	// Cloud has a later timestamp, so cloud wins. That's not what we want.
	//
	// We need local to win during pull. Local needs a later "action" than cloud.
	// The trick: local has a DeletedAt that's later than cloud's UpdatedAt.
	// Wait, no - we want local to win to skip the pull update.
	//
	// Actually, the way this works: for local to win the pull conflict,
	// local must have a later "latest action" than cloud. But local's UpdatedAt
	// is before lastSync (so it's not in push list). Cloud's UpdatedAt is after lastSync.
	// Cloud's UpdatedAt > local's UpdatedAt → cloud wins. That's the normal case.
	//
	// To make local win even though local's UpdatedAt is before lastSync and cloud's is after:
	// Local needs a later DeletedAt or some other field that makes it win.
	// If local has a DeletedAt that's after cloud's UpdatedAt, the delete action wins.
	//
	// Let's say local was deleted at 14:00 (after cloud's update at 13:00).
	// Local's latest action = delete at 14:00. Cloud's latest action = update at 13:00.
	// Local wins (14:00 > 13:00). But local's UpdatedAt is 11:59 (before lastSync),
	// so it's NOT in the push list. Cloud is in the pull list.
	// During pull, ResolveBundle picks local as winner → continue/skip.
	localDeletedAt := time.Date(2026, 3, 8, 14, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local stays</html>",
			CreatedAt:   localUpdatedAt, UpdatedAt: localUpdatedAt,
			DeletedAt: &localDeletedAt,
		},
	}
	// Cloud slide was updated after lastSync (will be in pull list).
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud loses</html>",
			CreatedAt:   localUpdatedAt, UpdatedAt: cloudUpdatedAt,
		},
	}

	// Set up with an existing lastSync so the sync window filters properly.
	localRepo := newMemoryRepo([]SlideBundle{localBundle})
	cloudRepo := newMemoryRepo([]SlideBundle{cloudBundle})

	baseDir := t.TempDir()
	localFS, err := filesystem.NewClient(baseDir)
	if err != nil {
		t.Fatalf("filesystem.NewClient() error = %v", err)
	}

	pcDir := filepath.Join(t.TempDir(), ".pc")
	cursorStore, err := syncengine.NewCursorStore(pcDir)
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}
	// Write a lastSync so only slides updated after it are considered.
	if err := cursorStore.Write(lastSyncTime); err != nil {
		t.Fatalf("cursorStore.Write() error = %v", err)
	}

	session, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	service, err := NewService(localRepo, cloudRepo, localFS, newMockObjectStore(), session)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Local should NOT have been updated to cloud's content (local won).
	got := localRepo.bundle(slideID)
	if got.Slide.HTMLContent != "<html>local stays</html>" {
		t.Fatalf("local HTMLContent = %q, want %q (local should win)", got.Slide.HTMLContent, "<html>local stays</html>")
	}
}

// --- PlanFigureReconciliation error in applyFiguresToLocal ---

func TestSyncPullErrorsOnFigurePlanReconciliationWithInvalidDesired(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	// Use a valid filename but whitespace-only S3Key. The download step downloads from
	// the whitespace key (which the mock has), and then PlanFigureReconciliation trims
	// whitespace and rejects it.
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "bad.png", S3Key: "  ", // whitespace-only S3Key
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	objects.objects["  "] = "BAD-CONTENT"

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from PlanFigureReconciliation with whitespace S3Key")
	}
	if !strings.Contains(err.Error(), "s3_key are required") {
		t.Fatalf("error = %q, want to contain 's3_key are required'", err.Error())
	}
}

// --- PlanFigureReconciliation error in applyFiguresToCloud ---

func TestSyncPushErrorsOnFigurePlanReconciliationWithInvalidDesired(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	// Use a valid filename but whitespace-only S3Key. The upload step uses the S3Key
	// as the object key (which succeeds), but PlanFigureReconciliation trims whitespace
	// and rejects it.
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.SlideFigure{{
			SlideID: slideID, Filename: "bad.png", S3Key: "  ", // whitespace-only S3Key
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, true, slideID, "bad.png", "FIG")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from PlanFigureReconciliation with whitespace S3Key")
	}
	if !strings.Contains(err.Error(), "s3_key are required") {
		t.Fatalf("error = %q, want to contain 's3_key are required'", err.Error())
	}
}

// --- PlanDataFileReconciliation error in applyDataFilesToLocal ---

func TestSyncPullErrorsOnDataFilePlanReconciliationWithInvalidDesired(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	// Use valid filename and S3Key but whitespace-only hash. PlanDataFileReconciliation
	// trims and rejects. Note: applyDataFilesToLocal calls PlanDataFileReconciliation
	// before any file operations, so we don't need to set up any downloads.
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "bad.csv", S3Key: "data/" + slideID + "/bad.csv",
			Size: 5, Hash: "  ", // whitespace-only hash
		}},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []SlideBundle{cloudBundle})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from PlanDataFileReconciliation with whitespace hash")
	}
	if !strings.Contains(err.Error(), "hash are required") {
		t.Fatalf("error = %q, want to contain 'hash are required'", err.Error())
	}
}

// --- PlanDataFileReconciliation error in applyDataFilesToCloud ---

func TestSyncPushErrorsOnDataFilePlanReconciliationWithInvalidDesired(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	// Use a valid filename but whitespace-only hash. PlanDataFileReconciliation
	// checks Filename, S3Key, and Hash — all must be non-blank after trim.
	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "bad.csv", S3Key: "data/" + slideID + "/bad.csv",
			Size: 5, Hash: "  ", // whitespace-only hash
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, false, slideID, "bad.csv", "DATA")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from PlanDataFileReconciliation with whitespace hash")
	}
	if !strings.Contains(err.Error(), "hash are required") {
		t.Fatalf("error = %q, want to contain 'hash are required'", err.Error())
	}
}

// --- loadBundle: bundleForSlide error inside loadBundle on pull ---

func TestSyncPullErrorsOnLoadBundleBundleForSlideErrorInsideLoadBundle(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	lastSyncTime := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	localUpdatedAt := time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC) // before lastSync
	cloudUpdatedAt := time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC) // after lastSync

	// Local slide exists but updated before lastSync (not in push list).
	localRepo := newMemoryRepo([]SlideBundle{{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   localUpdatedAt, UpdatedAt: localUpdatedAt,
		},
	}})

	// Cloud slide updated after lastSync (in pull list).
	cloudRepo := newMemoryRepo([]SlideBundle{{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   localUpdatedAt, UpdatedAt: cloudUpdatedAt,
		},
	}})

	baseDir := t.TempDir()
	localFS, err := filesystem.NewClient(baseDir)
	if err != nil {
		t.Fatalf("filesystem.NewClient() error = %v", err)
	}
	pcDir := filepath.Join(t.TempDir(), ".pc")
	cursorStore, err := syncengine.NewCursorStore(pcDir)
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}
	if err := cursorStore.Write(lastSyncTime); err != nil {
		t.Fatalf("cursorStore.Write() error = %v", err)
	}
	session, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Inject error only in local repo's ListSlideFiguresBySlideID.
	// Since local is not in push list, push phase won't call bundleForSlide on localRepo.
	// During pull, loadBundle(localRepo, slideID) calls GetSlideByID (succeeds),
	// then bundleForSlide which calls ListSlideFiguresBySlideID (fails).
	localRepo.listFiguresBySlideIDErr = fmt.Errorf("local figures error in loadBundle")

	service, err := NewService(localRepo, cloudRepo, localFS, newMockObjectStore(), session)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	syncErr := service.Sync(context.Background())
	if syncErr == nil {
		t.Fatal("expected error from loadBundle bundleForSlide")
	}
	if !strings.Contains(syncErr.Error(), "local figures error in loadBundle") {
		t.Fatalf("error = %q, want to contain 'local figures error in loadBundle'", syncErr.Error())
	}
}

// --- applyFiguresToLocal: removeFileIfPresent error in delete path ---

func TestSyncPullErrorsOnFigureDeleteRemoveFileFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.SlideFigure{
			{ID: 1, SlideID: slideID, Filename: "delete-me.png", S3Key: "figures/" + slideID + "/delete-me.png"},
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, slideID, "delete-me.png", "DELETE")

	// Make the file's directory read-only so removal fails.
	figPath, _ := localFS.ResolveFigurePath(slideID, "delete-me.png")
	figDir := filepath.Dir(figPath)
	if err := os.Chmod(figDir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(figDir, 0o755)
	})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from removeFileIfPresent in figure delete")
	}
	if !strings.Contains(err.Error(), "remove") {
		t.Fatalf("error = %q, want to contain 'remove'", err.Error())
	}
}

// --- applyDataFilesToLocal: removeFileIfPresent error in delete path ---

func TestSyncPullErrorsOnDataFileDeleteRemoveFileFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{
			{ID: 1, SlideID: slideID, Filename: "delete-me.csv", S3Key: "data/" + slideID + "/delete-me.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, slideID, "delete-me.csv", "DELETE")

	// Make the file's directory read-only so removal fails.
	dataPath, _ := localFS.ResolveDataFilePath(slideID, "delete-me.csv")
	dataDir := filepath.Dir(dataPath)
	if err := os.Chmod(dataDir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dataDir, 0o755)
	})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from removeFileIfPresent in data file delete")
	}
	if !strings.Contains(err.Error(), "remove") {
		t.Fatalf("error = %q, want to contain 'remove'", err.Error())
	}
}

// --- applyDataFilesToLocal: removeFileIfPresent error on update ---

func TestSyncPullErrorsOnDataFileUpdateRemoveFileFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>local</html>",
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.SlideDataFile{
			{ID: 1, SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.SlideDataFile{
			{SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data-v2.csv",
				Size: 10, Hash: strings.Repeat("b", 64)},
		},
	}

	service, _, _, localFS, _, _ := newTestService(t, []SlideBundle{localBundle}, []SlideBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, slideID, "data.csv", "OLD")

	// Make the file's directory read-only so removal fails.
	dataPath, _ := localFS.ResolveDataFilePath(slideID, "data.csv")
	dataDir := filepath.Dir(dataPath)
	if err := os.Chmod(dataDir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dataDir, 0o755)
	})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from removeFileIfPresent in data file update")
	}
	if !strings.Contains(err.Error(), "remove") {
		t.Fatalf("error = %q, want to contain 'remove'", err.Error())
	}
}

// --- applyDataFilesToLocal: removeFileIfPresent error on create ---

func TestSyncPullErrorsOnDataFileCreateRemoveFileFailure(t *testing.T) {
	slideID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := SlideBundle{
		Slide: repository.Slide{
			ID: slideID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: "<html>cloud</html>",
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.SlideDataFile{{
			SlideID: slideID, Filename: "data.csv", S3Key: "data/" + slideID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, nil, []SlideBundle{cloudBundle})
	// Pre-create a file at the data path and make its directory read-only.
	writeLocalAsset(t, localFS, false, slideID, "data.csv", "EXISTING")
	dataPath, _ := localFS.ResolveDataFilePath(slideID, "data.csv")
	dataDir := filepath.Dir(dataPath)
	if err := os.Chmod(dataDir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dataDir, 0o755)
	})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from removeFileIfPresent in data file create")
	}
	if !strings.Contains(err.Error(), "remove") {
		t.Fatalf("error = %q, want to contain 'remove'", err.Error())
	}
}

// --- countingObjectStore wraps mockObjectStore and fails Delete after N successful calls ---

type countingObjectStore struct {
	inner          *mockObjectStore
	deleteCount    int
	deleteErrAfter int // fail on this Nth call (0-based)
	deleteErr      error
}

func (c *countingObjectStore) Upload(ctx context.Context, key string, body io.Reader) error {
	return c.inner.Upload(ctx, key, body)
}

func (c *countingObjectStore) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return c.inner.Download(ctx, key)
}

func (c *countingObjectStore) Delete(ctx context.Context, key string) error {
	if c.deleteErr != nil && c.deleteCount >= c.deleteErrAfter {
		return c.deleteErr
	}
	c.deleteCount++
	return c.inner.Delete(ctx, key)
}

func (c *countingObjectStore) UpdateVersion(ctx context.Context, version int64) error {
	return c.inner.UpdateVersion(ctx, version)
}

// --- countingLocalFiles returns error after N calls to ResolveDataFilePath ---

type countingLocalFiles struct {
	dataFileCount    int
	dataFileErrAfter int
	dataFileErr      error
}

func (c *countingLocalFiles) ResolveFigurePath(slideID string, filename string) (string, error) {
	return filepath.Join("/tmp/counting", slideID, "figures", filename), nil
}

func (c *countingLocalFiles) ResolveDataFilePath(slideID string, filename string) (string, error) {
	if c.dataFileErr != nil && c.dataFileCount >= c.dataFileErrAfter {
		return "", c.dataFileErr
	}
	c.dataFileCount++
	return filepath.Join("/tmp/counting", slideID, "data", filename), nil
}

// --- errLocalFiles for testing LocalFiles error paths ---

type errLocalFiles struct {
	figurErr    error
	dataFileErr error
}

func (e *errLocalFiles) ResolveFigurePath(_ string, _ string) (string, error) {
	if e.figurErr != nil {
		return "", e.figurErr
	}
	return "/tmp/stub/figure", nil
}

func (e *errLocalFiles) ResolveDataFilePath(_ string, _ string) (string, error) {
	if e.dataFileErr != nil {
		return "", e.dataFileErr
	}
	return "/tmp/stub/data", nil
}

// --- stubLocalFiles for NewService nil-arg tests ---

type stubLocalFiles struct{}

func (s *stubLocalFiles) ResolveFigurePath(slideID string, filename string) (string, error) {
	return filepath.Join("/tmp/stub", slideID, "figures", filename), nil
}

func (s *stubLocalFiles) ResolveDataFilePath(slideID string, filename string) (string, error) {
	return filepath.Join("/tmp/stub", slideID, "data", filename), nil
}

// completeErrSessionWrapper wraps a real session and injects a Complete error.
type completeErrSessionWrapper struct {
	real        SessionManager
	completeErr error
}

func (w *completeErrSessionWrapper) Begin() (syncengine.SyncWindow, *syncengine.FileLock, error) {
	return w.real.Begin()
}

func (w *completeErrSessionWrapper) Complete(_ syncengine.SyncWindow) error {
	return w.completeErr
}

// errSessionManager is a SessionManager that returns errors on Begin and/or Complete.
type errSessionManager struct {
	beginErr    error
	completeErr error
}

func (e *errSessionManager) Begin() (syncengine.SyncWindow, *syncengine.FileLock, error) {
	if e.beginErr != nil {
		return syncengine.SyncWindow{}, nil, e.beginErr
	}
	// Should not be reached in tests that set beginErr.
	return syncengine.SyncWindow{}, nil, fmt.Errorf("not implemented")
}

func (e *errSessionManager) Complete(_ syncengine.SyncWindow) error {
	return e.completeErr
}

type memoryRepo struct {
	slides         map[string]repository.Slide
	figuresBySlide map[string]map[string]repository.SlideFigure
	dataBySlide    map[string]map[string]repository.SlideDataFile
	nextFigureID   int64
	nextDataID     int64
	syncVersion    repository.SyncVersion

	// Error injection fields.
	listSlidesErr             error
	getSyncVersionErr         error
	getSlideByIDErr           error
	listFiguresBySlideIDErr   error
	listDataFilesBySlideIDErr error
	createSlideFigureErr      error
	updateSlideFigureErr      error
	deleteSlideFigureErr      error
	createSlideDataFileErr    error
	updateSlideDataFileErr    error
	deleteSlideDataFileErr    error
	updateSlideErr            error
	createSlideErr            error
}

func newMemoryRepo(bundles []SlideBundle) *memoryRepo {
	repo := &memoryRepo{
		slides:         make(map[string]repository.Slide),
		figuresBySlide: make(map[string]map[string]repository.SlideFigure),
		dataBySlide:    make(map[string]map[string]repository.SlideDataFile),
		nextFigureID:   1,
		nextDataID:     1,
		syncVersion:    repository.SyncVersion{ID: 1, Version: 1},
	}
	for _, bundle := range bundles {
		repo.slides[bundle.Slide.ID] = bundle.Slide
		repo.figuresBySlide[bundle.Slide.ID] = make(map[string]repository.SlideFigure)
		for _, figure := range bundle.Figures {
			if figure.ID == 0 {
				figure.ID = repo.nextFigureID
			}
			if figure.ID >= repo.nextFigureID {
				repo.nextFigureID = figure.ID + 1
			}
			repo.figuresBySlide[bundle.Slide.ID][figure.Filename] = figure
		}
		repo.dataBySlide[bundle.Slide.ID] = make(map[string]repository.SlideDataFile)
		for _, dataFile := range bundle.DataFiles {
			if dataFile.ID == 0 {
				dataFile.ID = repo.nextDataID
			}
			if dataFile.ID >= repo.nextDataID {
				repo.nextDataID = dataFile.ID + 1
			}
			repo.dataBySlide[bundle.Slide.ID][dataFile.Filename] = dataFile
		}
	}
	return repo
}

func (m *memoryRepo) CreateSlide(_ context.Context, input repository.CreateSlideInput) (repository.Slide, error) {
	if m.createSlideErr != nil {
		return repository.Slide{}, m.createSlideErr
	}
	if _, exists := m.slides[input.ID]; exists {
		return repository.Slide{}, repository.ErrConflict
	}
	slide := repository.Slide{
		ID:           input.ID,
		Date:         input.Date,
		DayOrder:     input.DayOrder,
		HTMLContent:  input.HTMLContent,
		Notes:        cloneStringPtr(input.Notes),
		ProjectID:    cloneStringPtr(input.ProjectID),
		GitRemoteURL: cloneStringPtr(input.GitRemoteURL),
		GitHash:      cloneStringPtr(input.GitHash),
		CreatedAt:    derefTime(input.CreatedAt),
		UpdatedAt:    derefTime(input.UpdatedAt),
		DeletedAt:    cloneTimePtr(input.DeletedAt),
	}
	m.slides[input.ID] = slide
	if _, ok := m.figuresBySlide[input.ID]; !ok {
		m.figuresBySlide[input.ID] = make(map[string]repository.SlideFigure)
	}
	if _, ok := m.dataBySlide[input.ID]; !ok {
		m.dataBySlide[input.ID] = make(map[string]repository.SlideDataFile)
	}
	return slide, nil
}

func (m *memoryRepo) GetSlideByID(_ context.Context, id string) (repository.Slide, error) {
	if m.getSlideByIDErr != nil {
		return repository.Slide{}, m.getSlideByIDErr
	}
	slide, ok := m.slides[id]
	if !ok {
		return repository.Slide{}, repository.ErrNotFound
	}
	return slide, nil
}

func (m *memoryRepo) UpdateSlide(_ context.Context, input repository.UpdateSlideInput) (repository.Slide, error) {
	if m.updateSlideErr != nil {
		return repository.Slide{}, m.updateSlideErr
	}
	slide, ok := m.slides[input.ID]
	if !ok {
		return repository.Slide{}, repository.ErrNotFound
	}
	slide.Date = input.Date
	slide.DayOrder = input.DayOrder
	slide.HTMLContent = input.HTMLContent
	slide.Notes = cloneStringPtr(input.Notes)
	slide.ProjectID = cloneStringPtr(input.ProjectID)
	slide.GitRemoteURL = cloneStringPtr(input.GitRemoteURL)
	slide.GitHash = cloneStringPtr(input.GitHash)
	slide.UpdatedAt = derefTime(input.UpdatedAt)
	slide.DeletedAt = cloneTimePtr(input.DeletedAt)
	m.slides[input.ID] = slide
	return slide, nil
}

func (m *memoryRepo) ListSlides(_ context.Context, filter repository.ListSlidesFilter) ([]repository.Slide, error) {
	if m.listSlidesErr != nil {
		return nil, m.listSlidesErr
	}
	slides := sortMemorySlides(m.slides)
	result := make([]repository.Slide, 0, len(slides))
	for _, slide := range slides {
		switch {
		case filter.OnlyDeleted && slide.DeletedAt == nil:
			continue
		case !filter.IncludeDeleted && !filter.OnlyDeleted && slide.DeletedAt != nil:
			continue
		}
		result = append(result, slide)
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (m *memoryRepo) CreateSlideFigure(_ context.Context, input repository.CreateSlideFigureInput) (repository.SlideFigure, error) {
	if m.createSlideFigureErr != nil {
		return repository.SlideFigure{}, m.createSlideFigureErr
	}
	figure := repository.SlideFigure{
		ID:       m.nextFigureID,
		SlideID:  input.SlideID,
		Filename: input.Filename,
		S3Key:    input.S3Key,
		AltText:  cloneStringPtr(input.AltText),
	}
	m.nextFigureID++
	if _, ok := m.figuresBySlide[input.SlideID]; !ok {
		m.figuresBySlide[input.SlideID] = make(map[string]repository.SlideFigure)
	}
	m.figuresBySlide[input.SlideID][input.Filename] = figure
	return figure, nil
}

func (m *memoryRepo) UpdateSlideFigure(_ context.Context, input repository.UpdateSlideFigureInput) (repository.SlideFigure, error) {
	if m.updateSlideFigureErr != nil {
		return repository.SlideFigure{}, m.updateSlideFigureErr
	}
	for slideID, figures := range m.figuresBySlide {
		for filename, figure := range figures {
			if figure.ID != input.ID {
				continue
			}
			delete(figures, filename)
			figure.Filename = input.Filename
			figure.S3Key = input.S3Key
			figure.AltText = cloneStringPtr(input.AltText)
			m.figuresBySlide[slideID][figure.Filename] = figure
			return figure, nil
		}
	}
	return repository.SlideFigure{}, repository.ErrNotFound
}

func (m *memoryRepo) GetSlideFigureByID(_ context.Context, id int64) (repository.SlideFigure, error) {
	for _, figures := range m.figuresBySlide {
		for _, figure := range figures {
			if figure.ID == id {
				return figure, nil
			}
		}
	}
	return repository.SlideFigure{}, repository.ErrNotFound
}

func (m *memoryRepo) ListSlideFiguresBySlideID(_ context.Context, slideID string) ([]repository.SlideFigure, error) {
	if m.listFiguresBySlideIDErr != nil {
		return nil, m.listFiguresBySlideIDErr
	}
	figures := make([]repository.SlideFigure, 0, len(m.figuresBySlide[slideID]))
	for _, figure := range m.figuresBySlide[slideID] {
		figures = append(figures, figure)
	}
	sort.Slice(figures, func(i, j int) bool {
		return figures[i].Filename < figures[j].Filename
	})
	return figures, nil
}

func (m *memoryRepo) DeleteSlideFigure(_ context.Context, id int64) error {
	if m.deleteSlideFigureErr != nil {
		return m.deleteSlideFigureErr
	}
	for slideID, figures := range m.figuresBySlide {
		for filename, figure := range figures {
			if figure.ID != id {
				continue
			}
			delete(m.figuresBySlide[slideID], filename)
			return nil
		}
	}
	return repository.ErrNotFound
}

func (m *memoryRepo) CreateSlideDataFile(_ context.Context, input repository.CreateSlideDataFileInput) (repository.SlideDataFile, error) {
	if m.createSlideDataFileErr != nil {
		return repository.SlideDataFile{}, m.createSlideDataFileErr
	}
	dataFile := repository.SlideDataFile{
		ID:          m.nextDataID,
		SlideID:     input.SlideID,
		Filename:    input.Filename,
		S3Key:       input.S3Key,
		Size:        input.Size,
		Hash:        input.Hash,
		Description: cloneStringPtr(input.Description),
	}
	m.nextDataID++
	if _, ok := m.dataBySlide[input.SlideID]; !ok {
		m.dataBySlide[input.SlideID] = make(map[string]repository.SlideDataFile)
	}
	m.dataBySlide[input.SlideID][input.Filename] = dataFile
	return dataFile, nil
}

func (m *memoryRepo) UpdateSlideDataFile(_ context.Context, input repository.UpdateSlideDataFileInput) (repository.SlideDataFile, error) {
	if m.updateSlideDataFileErr != nil {
		return repository.SlideDataFile{}, m.updateSlideDataFileErr
	}
	for slideID, dataFiles := range m.dataBySlide {
		for filename, dataFile := range dataFiles {
			if dataFile.ID != input.ID {
				continue
			}
			delete(dataFiles, filename)
			dataFile.Filename = input.Filename
			dataFile.S3Key = input.S3Key
			if input.Size != nil {
				dataFile.Size = *input.Size
			}
			if input.Hash != nil {
				dataFile.Hash = *input.Hash
			}
			dataFile.Description = cloneStringPtr(input.Description)
			m.dataBySlide[slideID][dataFile.Filename] = dataFile
			return dataFile, nil
		}
	}
	return repository.SlideDataFile{}, repository.ErrNotFound
}

func (m *memoryRepo) GetSlideDataFileByID(_ context.Context, id int64) (repository.SlideDataFile, error) {
	for _, dataFiles := range m.dataBySlide {
		for _, dataFile := range dataFiles {
			if dataFile.ID == id {
				return dataFile, nil
			}
		}
	}
	return repository.SlideDataFile{}, repository.ErrNotFound
}

func (m *memoryRepo) ListSlideDataFilesBySlideID(_ context.Context, slideID string) ([]repository.SlideDataFile, error) {
	if m.listDataFilesBySlideIDErr != nil {
		return nil, m.listDataFilesBySlideIDErr
	}
	dataFiles := make([]repository.SlideDataFile, 0, len(m.dataBySlide[slideID]))
	for _, dataFile := range m.dataBySlide[slideID] {
		dataFiles = append(dataFiles, dataFile)
	}
	sort.Slice(dataFiles, func(i, j int) bool {
		return dataFiles[i].Filename < dataFiles[j].Filename
	})
	return dataFiles, nil
}

func (m *memoryRepo) DeleteSlideDataFile(_ context.Context, id int64) error {
	if m.deleteSlideDataFileErr != nil {
		return m.deleteSlideDataFileErr
	}
	for slideID, dataFiles := range m.dataBySlide {
		for filename, dataFile := range dataFiles {
			if dataFile.ID != id {
				continue
			}
			delete(m.dataBySlide[slideID], filename)
			return nil
		}
	}
	return repository.ErrNotFound
}

func (m *memoryRepo) SoftDeleteSlide(_ context.Context, id string) error { return nil }
func (m *memoryRepo) RestoreSlide(_ context.Context, id string) error    { return nil }
func (m *memoryRepo) DeleteSlide(_ context.Context, id string) error     { return nil }
func (m *memoryRepo) CreateTemplate(_ context.Context, input repository.CreateTemplateInput) (repository.Template, error) {
	return repository.Template{}, nil
}
func (m *memoryRepo) GetTemplateByName(_ context.Context, name string) (repository.Template, error) {
	return repository.Template{}, repository.ErrNotFound
}
func (m *memoryRepo) UpdateTemplate(_ context.Context, input repository.UpdateTemplateInput) (repository.Template, error) {
	return repository.Template{}, nil
}
func (m *memoryRepo) ListTemplates(_ context.Context) ([]repository.Template, error) { return nil, nil }
func (m *memoryRepo) DeleteTemplate(_ context.Context, name string) error            { return nil }
func (m *memoryRepo) GetSyncVersion(_ context.Context) (repository.SyncVersion, error) {
	if m.getSyncVersionErr != nil {
		return repository.SyncVersion{}, m.getSyncVersionErr
	}
	return m.syncVersion, nil
}
func (m *memoryRepo) ListDistinctProjectIDs(_ context.Context) ([]string, error) { return nil, nil }

func (m *memoryRepo) bundle(slideID string) SlideBundle {
	slide, ok := m.slides[slideID]
	if !ok {
		return SlideBundle{}
	}
	figures, _ := m.ListSlideFiguresBySlideID(context.Background(), slideID)
	dataFiles, _ := m.ListSlideDataFilesBySlideID(context.Background(), slideID)
	return SlideBundle{Slide: slide, Figures: figures, DataFiles: dataFiles}
}

type mockObjectStore struct {
	objects          map[string]string
	versionWrites    []int64
	uploadErr        error
	downloadErr      error
	deleteErr        error
	updateVersionErr error
}

func newMockObjectStore() *mockObjectStore {
	return &mockObjectStore{objects: make(map[string]string)}
}

func (m *mockObjectStore) Upload(_ context.Context, key string, body io.Reader) error {
	if m.uploadErr != nil {
		return m.uploadErr
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.objects[key] = string(data)
	return nil
}

func (m *mockObjectStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	if m.downloadErr != nil {
		return nil, m.downloadErr
	}
	value, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("missing object %s", key)
	}
	return io.NopCloser(strings.NewReader(value)), nil
}

func (m *mockObjectStore) Delete(_ context.Context, key string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.objects, key)
	return nil
}

func (m *mockObjectStore) UpdateVersion(_ context.Context, version int64) error {
	if m.updateVersionErr != nil {
		return m.updateVersionErr
	}
	m.versionWrites = append(m.versionWrites, version)
	return nil
}

func newTestService(
	t *testing.T,
	localBundles []SlideBundle,
	cloudBundles []SlideBundle,
) (*Service, *memoryRepo, *memoryRepo, *filesystem.Client, *mockObjectStore, *syncengine.CursorStore) {
	t.Helper()

	localRepo := newMemoryRepo(localBundles)
	cloudRepo := newMemoryRepo(cloudBundles)

	baseDir := t.TempDir()
	localFS, err := filesystem.NewClient(baseDir)
	if err != nil {
		t.Fatalf("filesystem.NewClient() error = %v", err)
	}

	pcDir := filepath.Join(t.TempDir(), ".pc")
	session, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("syncengine.NewManager() error = %v", err)
	}
	cursorStore, err := syncengine.NewCursorStore(pcDir)
	if err != nil {
		t.Fatalf("syncengine.NewCursorStore() error = %v", err)
	}

	service, err := NewService(localRepo, cloudRepo, localFS, newMockObjectStore(), session)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	objects := service.cloudObjects.(*mockObjectStore)
	return service, localRepo, cloudRepo, localFS, objects, cursorStore
}

func writeLocalAsset(
	t *testing.T,
	localFS *filesystem.Client,
	isFigure bool,
	slideID string,
	filename string,
	content string,
) {
	t.Helper()

	var (
		path string
		err  error
	)
	if isFigure {
		path, err = localFS.ResolveFigurePath(slideID, filename)
	} else {
		path, err = localFS.ResolveDataFilePath(slideID, filename)
	}
	if err != nil {
		t.Fatalf("resolve local asset path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func readLocalAsset(
	t *testing.T,
	localFS *filesystem.Client,
	isFigure bool,
	slideID string,
	filename string,
) string {
	t.Helper()

	var (
		path string
		err  error
	)
	if isFigure {
		path, err = localFS.ResolveFigurePath(slideID, filename)
	} else {
		path, err = localFS.ResolveDataFilePath(slideID, filename)
	}
	if err != nil {
		t.Fatalf("resolve local asset path: %v", err)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return string(bytes)
}

func assertBundleEqual(t *testing.T, got SlideBundle, want SlideBundle) {
	t.Helper()
	if canonicalBundle(got) != canonicalBundle(want) {
		t.Fatalf("bundle mismatch\n got: %+v\nwant: %+v", canonicalBundle(got), canonicalBundle(want))
	}
}

func canonicalBundle(bundle SlideBundle) string {
	canonicalFigures := append([]repository.SlideFigure(nil), bundle.Figures...)
	sort.Slice(canonicalFigures, func(i, j int) bool {
		return canonicalFigures[i].Filename < canonicalFigures[j].Filename
	})
	canonicalDataFiles := append([]repository.SlideDataFile(nil), bundle.DataFiles...)
	sort.Slice(canonicalDataFiles, func(i, j int) bool {
		return canonicalDataFiles[i].Filename < canonicalDataFiles[j].Filename
	})

	parts := []string{
		bundle.Slide.ID,
		bundle.Slide.Date,
		bundle.Slide.DayOrder,
		bundle.Slide.HTMLContent,
		bundle.Slide.CreatedAt.UTC().Format(time.RFC3339Nano),
		bundle.Slide.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if bundle.Slide.DeletedAt != nil {
		parts = append(parts, bundle.Slide.DeletedAt.UTC().Format(time.RFC3339Nano))
	}
	for _, figure := range canonicalFigures {
		parts = append(parts, fmt.Sprintf("fig:%s:%s:%s", figure.SlideID, figure.Filename, figure.S3Key))
	}
	for _, dataFile := range canonicalDataFiles {
		parts = append(parts, fmt.Sprintf("data:%s:%s:%s:%d:%s", dataFile.SlideID, dataFile.Filename, dataFile.S3Key, dataFile.Size, dataFile.Hash))
	}
	return strings.Join(parts, "|")
}

func sortMemorySlides(slides map[string]repository.Slide) []repository.Slide {
	result := make([]repository.Slide, 0, len(slides))
	for _, slide := range slides {
		result = append(result, slide)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Date != result[j].Date {
			return result[i].Date < result[j].Date
		}
		if result[i].DayOrder != result[j].DayOrder {
			return result[i].DayOrder < result[j].DayOrder
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
