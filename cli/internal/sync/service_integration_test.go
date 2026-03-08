//go:build integration

package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	miniomodule "github.com/testcontainers/testcontainers-go/modules/minio"
	pgmodule "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	postgresrepo "github.com/conn-castle/personal-context/cli/internal/repository/postgres"
	sqliterepo "github.com/conn-castle/personal-context/cli/internal/repository/sqlite"
	"github.com/conn-castle/personal-context/cli/internal/s3client"
	"github.com/conn-castle/personal-context/cli/internal/sqlite"
	"github.com/conn-castle/personal-context/cli/internal/syncengine"
)

var integrationEnv struct {
	postgresConnString string
	minioEndpoint      string
	minioUsername      string
	minioPassword      string
}

func TestMain(m *testing.M) {
	ctx := context.Background()

	postgresContainer, err := pgmodule.Run(ctx,
		"postgres:16-alpine",
		pgmodule.WithDatabase("testdb"),
		pgmodule.WithUsername("test"),
		pgmodule.WithPassword("test"),
		pgmodule.WithSQLDriver("pgx"),
		pgmodule.BasicWaitStrategies(),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres container: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = postgresContainer.Terminate(ctx) }()

	connString, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get postgres connection string: %v\n", err)
		os.Exit(1)
	}
	integrationEnv.postgresConnString = connString

	minioContainer, err := miniomodule.Run(ctx,
		"minio/minio:RELEASE.2024-01-16T16-07-38Z",
		miniomodule.WithUsername("minioadmin"),
		miniomodule.WithPassword("minioadmin"),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/minio/health/live").
				WithPort("9000/tcp").
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start minio container: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = minioContainer.Terminate(ctx) }()

	endpoint, err := minioContainer.ConnectionString(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get minio endpoint: %v\n", err)
		os.Exit(1)
	}
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = "http://" + endpoint
	}
	integrationEnv.minioEndpoint = endpoint
	integrationEnv.minioUsername = minioContainer.Username
	integrationEnv.minioPassword = minioContainer.Password

	os.Exit(m.Run())
}

func TestServiceSyncIntegrationRoundTripBetweenLocals(t *testing.T) {
	ctx := context.Background()
	cloudRepo, cloudObjects := newCloudDependencies(t)
	localOneRepo, localOneFS, localOneSession := newLocalDependencies(t)
	localTwoRepo, localTwoFS, localTwoSession := newLocalDependencies(t)

	bundle := SlideBundle{
		Slide: repository.Slide{
			ID:          "20260308-a1b2c3d4",
			Date:        "2026-03-08",
			DayOrder:    "a0",
			HTMLContent: "<html>integration</html>",
			CreatedAt:   time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 3, 8, 10, 1, 0, 0, time.UTC),
		},
		Figures: []repository.SlideFigure{{
			SlideID:  "20260308-a1b2c3d4",
			Filename: "plot.png",
			S3Key:    "figures/20260308-a1b2c3d4/plot.png",
		}},
		DataFiles: []repository.SlideDataFile{{
			SlideID:  "20260308-a1b2c3d4",
			Filename: "metrics.csv",
			S3Key:    "data/20260308-a1b2c3d4/metrics.csv",
			Size:     8,
			Hash:     strings.Repeat("a", 64),
		}},
	}

	insertBundle(t, localOneRepo, bundle)
	writeLocalAsset(t, localOneFS, true, bundle.Slide.ID, "plot.png", "FIGURE")
	writeLocalAsset(t, localOneFS, false, bundle.Slide.ID, "metrics.csv", "1,2,3\n")

	serviceOne, err := NewService(localOneRepo, cloudRepo, localOneFS, cloudObjects, localOneSession)
	if err != nil {
		t.Fatalf("NewService(local one) error = %v", err)
	}
	if err := serviceOne.Sync(ctx); err != nil {
		t.Fatalf("serviceOne.Sync() error = %v", err)
	}

	serviceTwo, err := NewService(localTwoRepo, cloudRepo, localTwoFS, cloudObjects, localTwoSession)
	if err != nil {
		t.Fatalf("NewService(local two) error = %v", err)
	}
	if err := serviceTwo.Sync(ctx); err != nil {
		t.Fatalf("serviceTwo.Sync() error = %v", err)
	}

	got := loadBundleFromRepository(t, ctx, localTwoRepo, bundle.Slide.ID)
	assertBundleEqual(t, got, bundle)
	if got := readLocalAsset(t, localTwoFS, true, bundle.Slide.ID, "plot.png"); got != "FIGURE" {
		t.Fatalf("local two figure = %q, want %q", got, "FIGURE")
	}
	dataPath, err := localTwoFS.ResolveDataFilePath(bundle.Slide.ID, "metrics.csv")
	if err != nil {
		t.Fatalf("ResolveDataFilePath() error = %v", err)
	}
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected local two data file to remain undownloaded, stat error = %v", err)
	}
}

func TestServiceSyncIntegrationCloudLaterEditWins(t *testing.T) {
	ctx := context.Background()
	cloudRepo, cloudObjects := newCloudDependencies(t)
	localRepo, localFS, localSession := newLocalDependencies(t)

	bundle := SlideBundle{
		Slide: repository.Slide{
			ID:          "20260308-b1c2d3e4",
			Date:        "2026-03-08",
			DayOrder:    "a1",
			HTMLContent: "<html>original</html>",
			CreatedAt:   time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 3, 8, 11, 1, 0, 0, time.UTC),
		},
		Figures: []repository.SlideFigure{{
			SlideID:  "20260308-b1c2d3e4",
			Filename: "plot.png",
			S3Key:    "figures/20260308-b1c2d3e4/plot.png",
		}},
	}

	insertBundle(t, localRepo, bundle)
	writeLocalAsset(t, localFS, true, bundle.Slide.ID, "plot.png", "ORIGINAL")

	service, err := NewService(localRepo, cloudRepo, localFS, cloudObjects, localSession)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.Sync(ctx); err != nil {
		t.Fatalf("initial Sync() error = %v", err)
	}

	localUpdatedAt := time.Now().UTC().Add(1 * time.Minute)
	if _, err := localRepo.UpdateSlide(ctx, repository.UpdateSlideInput{
		ID:          bundle.Slide.ID,
		Date:        bundle.Slide.Date,
		DayOrder:    bundle.Slide.DayOrder,
		HTMLContent: "<html>local edit</html>",
		UpdatedAt:   &localUpdatedAt,
	}); err != nil {
		t.Fatalf("local UpdateSlide() error = %v", err)
	}
	writeLocalAsset(t, localFS, true, bundle.Slide.ID, "plot.png", "LOCAL")

	cloudUpdatedAt := localUpdatedAt.Add(1 * time.Minute)
	if _, err := cloudRepo.UpdateSlide(ctx, repository.UpdateSlideInput{
		ID:          bundle.Slide.ID,
		Date:        bundle.Slide.Date,
		DayOrder:    bundle.Slide.DayOrder,
		HTMLContent: "<html>cloud edit</html>",
		UpdatedAt:   &cloudUpdatedAt,
	}); err != nil {
		t.Fatalf("cloud UpdateSlide() error = %v", err)
	}
	if err := cloudObjects.Upload(ctx, "figures/20260308-b1c2d3e4/plot.png", strings.NewReader("CLOUD")); err != nil {
		t.Fatalf("cloud figure upload error = %v", err)
	}

	if err := service.Sync(ctx); err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}

	got := loadBundleFromRepository(t, ctx, localRepo, bundle.Slide.ID)
	if got.Slide.HTMLContent != "<html>cloud edit</html>" {
		t.Fatalf("local HTMLContent = %q, want cloud edit", got.Slide.HTMLContent)
	}
	if !got.Slide.UpdatedAt.Equal(cloudUpdatedAt) {
		t.Fatalf("local UpdatedAt = %v, want %v", got.Slide.UpdatedAt, cloudUpdatedAt)
	}
	if got := readLocalAsset(t, localFS, true, bundle.Slide.ID, "plot.png"); got != "CLOUD" {
		t.Fatalf("local figure = %q, want %q", got, "CLOUD")
	}
}

func newLocalDependencies(t *testing.T) (repository.Repository, *filesystem.Client, pcsyncSessionManager) {
	t.Helper()

	baseDir := t.TempDir()
	conn, err := sqlite.Open(filepath.Join(baseDir, ".pc", "pc.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := conn.ApplySchema(context.Background()); err != nil {
		t.Fatalf("ApplySchema() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	repo, err := sqliterepo.New(conn.DB())
	if err != nil {
		t.Fatalf("sqliterepo.New() error = %v", err)
	}
	localFS, err := filesystem.NewClient(baseDir)
	if err != nil {
		t.Fatalf("filesystem.NewClient() error = %v", err)
	}
	session, err := syncengine.NewManager(filepath.Join(baseDir, ".pc"))
	if err != nil {
		t.Fatalf("syncengine.NewManager() error = %v", err)
	}
	return repo, localFS, session
}

type pcsyncSessionManager = interface {
	Begin() (syncengine.SyncWindow, *syncengine.FileLock, error)
	Complete(syncengine.SyncWindow) error
}

var integrationSchemaCounter int
var integrationBucketCounter int

func newCloudDependencies(t *testing.T) (repository.Repository, *s3client.Client) {
	t.Helper()

	ctx := context.Background()
	integrationSchemaCounter++
	schemaName := fmt.Sprintf("sync_%d_%d", time.Now().UnixNano(), integrationSchemaCounter)

	pool, err := pgxpool.New(ctx, integrationEnv.postgresConnString)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		pool.Close()
		t.Fatalf("CREATE SCHEMA error = %v", err)
	}
	pool.Close()

	connString := integrationEnv.postgresConnString + fmt.Sprintf("&search_path=%s", schemaName)
	pool, err = pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("pgxpool.New(search_path) error = %v", err)
	}
	if err := postgresrepo.ApplySchema(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("ApplySchema() error = %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	repo, err := postgresrepo.New(pool)
	if err != nil {
		t.Fatalf("postgresrepo.New() error = %v", err)
	}

	integrationBucketCounter++
	bucketName := fmt.Sprintf("sync-%d-%d", time.Now().UnixNano(), integrationBucketCounter)
	s3Client := awss3.New(awss3.Options{
		BaseEndpoint: aws.String(integrationEnv.minioEndpoint),
		Credentials: credentials.NewStaticCredentialsProvider(
			integrationEnv.minioUsername,
			integrationEnv.minioPassword,
			"",
		),
		Region:       "us-east-1",
		UsePathStyle: true,
	})
	if _, err := s3Client.CreateBucket(ctx, &awss3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}); err != nil {
		t.Fatalf("CreateBucket(%s) error = %v", bucketName, err)
	}
	t.Cleanup(func() {
		listOut, _ := s3Client.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{
			Bucket: aws.String(bucketName),
		})
		if listOut != nil {
			for _, obj := range listOut.Contents {
				_, _ = s3Client.DeleteObject(ctx, &awss3.DeleteObjectInput{
					Bucket: aws.String(bucketName),
					Key:    obj.Key,
				})
			}
		}
		_, _ = s3Client.DeleteBucket(ctx, &awss3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
	})

	client, err := s3client.New(s3Client, bucketName)
	if err != nil {
		t.Fatalf("s3client.New() error = %v", err)
	}
	return repo, client
}

func insertBundle(t *testing.T, repo repository.Repository, bundle SlideBundle) {
	t.Helper()

	ctx := context.Background()
	createdAt := bundle.Slide.CreatedAt
	updatedAt := bundle.Slide.UpdatedAt
	if _, err := repo.CreateSlide(ctx, repository.CreateSlideInput{
		ID:           bundle.Slide.ID,
		Date:         bundle.Slide.Date,
		DayOrder:     bundle.Slide.DayOrder,
		HTMLContent:  bundle.Slide.HTMLContent,
		Notes:        bundle.Slide.Notes,
		ProjectID:    bundle.Slide.ProjectID,
		GitRemoteURL: bundle.Slide.GitRemoteURL,
		GitHash:      bundle.Slide.GitHash,
		CreatedAt:    &createdAt,
		UpdatedAt:    &updatedAt,
		DeletedAt:    bundle.Slide.DeletedAt,
	}); err != nil {
		t.Fatalf("CreateSlide() error = %v", err)
	}
	for _, figure := range bundle.Figures {
		if _, err := repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
			SlideID:  figure.SlideID,
			Filename: figure.Filename,
			S3Key:    figure.S3Key,
			AltText:  figure.AltText,
		}); err != nil {
			t.Fatalf("CreateSlideFigure() error = %v", err)
		}
	}
	for _, dataFile := range bundle.DataFiles {
		if _, err := repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
			SlideID:     dataFile.SlideID,
			Filename:    dataFile.Filename,
			S3Key:       dataFile.S3Key,
			Size:        dataFile.Size,
			Hash:        dataFile.Hash,
			Description: dataFile.Description,
		}); err != nil {
			t.Fatalf("CreateSlideDataFile() error = %v", err)
		}
	}
}

func loadBundleFromRepository(
	t *testing.T,
	ctx context.Context,
	repo repository.Repository,
	slideID string,
) SlideBundle {
	t.Helper()

	slide, err := repo.GetSlideByID(ctx, slideID)
	if err != nil {
		t.Fatalf("GetSlideByID() error = %v", err)
	}
	figures, err := repo.ListSlideFiguresBySlideID(ctx, slideID)
	if err != nil {
		t.Fatalf("ListSlideFiguresBySlideID() error = %v", err)
	}
	dataFiles, err := repo.ListSlideDataFilesBySlideID(ctx, slideID)
	if err != nil {
		t.Fatalf("ListSlideDataFilesBySlideID() error = %v", err)
	}
	return SlideBundle{Slide: slide, Figures: figures, DataFiles: dataFiles}
}
