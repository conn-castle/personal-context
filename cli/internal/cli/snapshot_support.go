package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/gitsnapshot"
	"github.com/conn-castle/personal-context/cli/internal/repository"
)

type importStats struct {
	Created int
	Updated int
	Skipped int
}

var downloadCloudFigureFn = func(ctx context.Context, cloud *cloudStack, key string) (io.ReadCloser, error) {
	return cloud.S3.Download(ctx, key)
}

func buildLocalSnapshot(ctx context.Context, stack *localStack, filter repository.ListSlidesFilter) (gitsnapshot.Snapshot, error) {
	return buildSnapshot(ctx, stack.Repo, stack.Repo, func(_ context.Context, figure repository.SlideFigure) ([]byte, error) {
		path, err := stack.FS.ResolveFigurePath(figure.SlideID, figure.Filename)
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read local figure %s: %w", path, err)
		}
		return content, nil
	}, filter)
}

func buildCloudSnapshot(ctx context.Context, homeDir string, cloud *cloudStack, filter repository.ListSlidesFilter) (gitsnapshot.Snapshot, error) {
	localStack, err := openLocalStack(homeDir)
	if err != nil {
		return gitsnapshot.Snapshot{}, err
	}
	defer func() { _ = localStack.Close() }()

	return buildSnapshot(ctx, localStack.Repo, cloud.Repo, func(ctx context.Context, figure repository.SlideFigure) ([]byte, error) {
		body, err := downloadCloudFigureFn(ctx, cloud, figure.S3Key)
		if err != nil {
			return nil, err
		}
		defer func() { _ = body.Close() }()
		content, err := io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("read cloud figure %s: %w", figure.S3Key, err)
		}
		return content, nil
	}, filter)
}

func buildSnapshot(
	ctx context.Context,
	templateRepo repository.Repository,
	slideRepo repository.Repository,
	readFigure func(context.Context, repository.SlideFigure) ([]byte, error),
	filter repository.ListSlidesFilter,
) (gitsnapshot.Snapshot, error) {
	templates, err := templateRepo.ListTemplates(ctx)
	if err != nil {
		return gitsnapshot.Snapshot{}, fmt.Errorf("list templates: %w", err)
	}
	snapshot := gitsnapshot.Snapshot{
		Templates: make([]gitsnapshot.Template, 0, len(templates)),
	}
	for _, template := range templates {
		snapshot.Templates = append(snapshot.Templates, gitsnapshot.Template{
			Name:        template.Name,
			HTMLContent: template.HTMLContent,
		})
	}
	projects, err := slideRepo.ListProjects(ctx, true)
	if err != nil {
		return gitsnapshot.Snapshot{}, fmt.Errorf("list projects: %w", err)
	}
	snapshot.Projects = make([]gitsnapshot.RegistryEntry, 0, len(projects))
	for _, project := range projects {
		snapshot.Projects = append(snapshot.Projects, gitsnapshot.RegistryEntry{
			ID:         project.ID,
			CreatedAt:  project.CreatedAt,
			UpdatedAt:  project.UpdatedAt,
			ArchivedAt: project.ArchivedAt,
		})
	}
	devices, err := slideRepo.ListDevices(ctx, true)
	if err != nil {
		return gitsnapshot.Snapshot{}, fmt.Errorf("list devices: %w", err)
	}
	snapshot.Devices = make([]gitsnapshot.RegistryEntry, 0, len(devices))
	for _, device := range devices {
		snapshot.Devices = append(snapshot.Devices, gitsnapshot.RegistryEntry{
			ID:         device.ID,
			CreatedAt:  device.CreatedAt,
			UpdatedAt:  device.UpdatedAt,
			ArchivedAt: device.ArchivedAt,
		})
	}

	slides, err := slideRepo.ListSlides(ctx, filter)
	if err != nil {
		return gitsnapshot.Snapshot{}, fmt.Errorf("list slides: %w", err)
	}
	snapshot.Slides = make([]gitsnapshot.Slide, 0, len(slides))
	for _, slide := range slides {
		figures, err := slideRepo.ListSlideFiguresBySlideID(ctx, slide.ID)
		if err != nil {
			return gitsnapshot.Snapshot{}, fmt.Errorf("list figures for %s: %w", slide.ID, err)
		}
		dataFiles, err := slideRepo.ListSlideDataFilesBySlideID(ctx, slide.ID)
		if err != nil {
			return gitsnapshot.Snapshot{}, fmt.Errorf("list data files for %s: %w", slide.ID, err)
		}

		exportFigures := make([]gitsnapshot.Figure, 0, len(figures))
		for _, figure := range figures {
			content, err := readFigure(ctx, figure)
			if err != nil {
				return gitsnapshot.Snapshot{}, fmt.Errorf("load figure %s/%s: %w", slide.ID, figure.Filename, err)
			}
			exportFigures = append(exportFigures, gitsnapshot.Figure{
				Filename: figure.Filename,
				S3Key:    figure.S3Key,
				AltText:  figure.AltText,
				Content:  content,
			})
		}
		exportDataFiles := make([]gitsnapshot.DataFile, 0, len(dataFiles))
		for _, file := range dataFiles {
			exportDataFiles = append(exportDataFiles, gitsnapshot.DataFile{
				Filename:    file.Filename,
				S3Key:       file.S3Key,
				Size:        file.Size,
				Hash:        file.Hash,
				Description: file.Description,
			})
		}

		snapshot.Slides = append(snapshot.Slides, gitsnapshot.Slide{
			ID:             slide.ID,
			Date:           slide.Date,
			DayOrder:       slide.DayOrder,
			ProjectID:      slide.ProjectID,
			SourceDeviceID: slide.SourceDeviceID,
			SourceRef:      slide.SourceRef,
			GitRemoteURL:   slide.GitRemoteURL,
			GitHash:        slide.GitHash,
			HTMLContent:    slide.HTMLContent,
			Notes:          slide.Notes,
			Figures:        exportFigures,
			DataFiles:      exportDataFiles,
			CreatedAt:      slide.CreatedAt,
			UpdatedAt:      slide.UpdatedAt,
		})
	}
	return snapshot, nil
}

func importSnapshotIntoStack(ctx context.Context, stack *localStack, snapshot gitsnapshot.Snapshot) (importStats, error) {
	stats := importStats{}

	for _, template := range snapshot.Templates {
		if err := upsertTemplate(ctx, stack.Repo, template); err != nil {
			return stats, err
		}
	}
	for _, project := range snapshot.Projects {
		if _, err := stack.Repo.UpsertProjectForImport(ctx, repository.Project{
			ID:         project.ID,
			CreatedAt:  project.CreatedAt,
			UpdatedAt:  project.UpdatedAt,
			ArchivedAt: project.ArchivedAt,
		}); err != nil {
			return stats, fmt.Errorf("upsert project %s: %w", project.ID, err)
		}
	}
	for _, device := range snapshot.Devices {
		if _, err := stack.Repo.UpsertDeviceForImport(ctx, repository.Device{
			ID:         device.ID,
			CreatedAt:  device.CreatedAt,
			UpdatedAt:  device.UpdatedAt,
			ArchivedAt: device.ArchivedAt,
		}); err != nil {
			return stats, fmt.Errorf("upsert device %s: %w", device.ID, err)
		}
	}

	for _, slide := range snapshot.Slides {
		existing, err := stack.Repo.GetSlideByID(ctx, slide.ID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return stats, fmt.Errorf("get slide %s: %w", slide.ID, err)
		}
		if errors.Is(err, repository.ErrNotFound) {
			if err := createImportedSlide(ctx, stack, slide); err != nil {
				return stats, err
			}
			stats.Created++
			continue
		}
		if !slide.UpdatedAt.After(existing.UpdatedAt) {
			stats.Skipped++
			continue
		}
		if err := updateImportedSlide(ctx, stack, slide); err != nil {
			return stats, err
		}
		stats.Updated++
	}

	return stats, nil
}

func upsertTemplate(ctx context.Context, repo repository.Repository, tmpl gitsnapshot.Template) error {
	existing, err := repo.GetTemplateByName(ctx, tmpl.Name)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("get template %s: %w", tmpl.Name, err)
	}
	if errors.Is(err, repository.ErrNotFound) {
		if _, err := repo.CreateTemplate(ctx, repository.CreateTemplateInput{
			Name:        tmpl.Name,
			HTMLContent: tmpl.HTMLContent,
		}); err != nil {
			return fmt.Errorf("create template %s: %w", tmpl.Name, err)
		}
		return nil
	}
	if existing.HTMLContent == tmpl.HTMLContent {
		return nil
	}
	if _, err := repo.UpdateTemplate(ctx, repository.UpdateTemplateInput{
		Name:        tmpl.Name,
		HTMLContent: tmpl.HTMLContent,
	}); err != nil {
		return fmt.Errorf("update template %s: %w", tmpl.Name, err)
	}
	return nil
}

func createImportedSlide(ctx context.Context, stack *localStack, slide gitsnapshot.Slide) error {
	createdAt := slide.CreatedAt.UTC()
	updatedAt := slide.UpdatedAt.UTC()
	if _, err := stack.Repo.CreateSlide(ctx, repository.CreateSlideInput{
		ID:             slide.ID,
		Date:           slide.Date,
		DayOrder:       slide.DayOrder,
		HTMLContent:    slide.HTMLContent,
		Notes:          slide.Notes,
		ProjectID:      slide.ProjectID,
		SourceDeviceID: slide.SourceDeviceID,
		SourceRef:      slide.SourceRef,
		GitRemoteURL:   slide.GitRemoteURL,
		GitHash:        slide.GitHash,
		CreatedAt:      &createdAt,
		UpdatedAt:      &updatedAt,
	}); err != nil {
		return fmt.Errorf("create slide %s: %w", slide.ID, err)
	}
	if err := replaceSlideChildren(ctx, stack, slide); err != nil {
		return err
	}
	return nil
}

func updateImportedSlide(ctx context.Context, stack *localStack, slide gitsnapshot.Slide) error {
	updatedAt := slide.UpdatedAt.UTC()
	if _, err := stack.Repo.UpdateSlide(ctx, repository.UpdateSlideInput{
		ID:             slide.ID,
		Date:           slide.Date,
		DayOrder:       slide.DayOrder,
		HTMLContent:    slide.HTMLContent,
		Notes:          slide.Notes,
		ProjectID:      slide.ProjectID,
		SourceDeviceID: slide.SourceDeviceID,
		SourceRef:      slide.SourceRef,
		GitRemoteURL:   slide.GitRemoteURL,
		GitHash:        slide.GitHash,
		UpdatedAt:      &updatedAt,
	}); err != nil {
		return fmt.Errorf("update slide %s: %w", slide.ID, err)
	}
	if err := replaceSlideChildren(ctx, stack, slide); err != nil {
		return err
	}
	return nil
}

func replaceSlideChildren(ctx context.Context, stack *localStack, slide gitsnapshot.Slide) error {
	existingFigures, err := stack.Repo.ListSlideFiguresBySlideID(ctx, slide.ID)
	if err != nil {
		return fmt.Errorf("list existing figures for %s: %w", slide.ID, err)
	}
	for _, figure := range existingFigures {
		if err := stack.Repo.DeleteSlideFigure(ctx, figure.ID); err != nil {
			return fmt.Errorf("delete existing figure %s/%s: %w", slide.ID, figure.Filename, err)
		}
	}
	existingDataFiles, err := stack.Repo.ListSlideDataFilesBySlideID(ctx, slide.ID)
	if err != nil {
		return fmt.Errorf("list existing data files for %s: %w", slide.ID, err)
	}
	for _, file := range existingDataFiles {
		if err := stack.Repo.DeleteSlideDataFile(ctx, file.ID); err != nil {
			return fmt.Errorf("delete existing data file %s/%s: %w", slide.ID, file.Filename, err)
		}
	}
	if err := stack.FS.DeleteSlideDir(slide.ID); err != nil {
		return fmt.Errorf("reset local files for %s: %w", slide.ID, err)
	}
	for _, figure := range slide.Figures {
		path, err := stack.FS.ResolveFigurePath(slide.ID, figure.Filename)
		if err != nil {
			return err
		}
		if err := writeTextFileAtomically(path, figure.Content, 0o700, 0o644); err != nil {
			return fmt.Errorf("write local figure %s/%s: %w", slide.ID, figure.Filename, err)
		}
		if _, err := stack.Repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
			SlideID:  slide.ID,
			Filename: figure.Filename,
			S3Key:    figure.S3Key,
			AltText:  figure.AltText,
		}); err != nil {
			return fmt.Errorf("create figure row %s/%s: %w", slide.ID, figure.Filename, err)
		}
	}
	for _, file := range slide.DataFiles {
		if _, err := stack.Repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
			SlideID:     slide.ID,
			Filename:    file.Filename,
			S3Key:       file.S3Key,
			Size:        file.Size,
			Hash:        file.Hash,
			Description: file.Description,
		}); err != nil {
			return fmt.Errorf("create data file row %s/%s: %w", slide.ID, file.Filename, err)
		}
	}
	return nil
}

func ensureLocalEnvironment(ctx context.Context, homeDir string) error {
	store, err := newConfigStoreFn(homeDir)
	if err != nil {
		return fmt.Errorf("create config store: %w", err)
	}
	pcDir := filepath.Join(basePath(homeDir), ".pc")
	if err := os.MkdirAll(pcDir, 0o700); err != nil {
		return fmt.Errorf("create .pc directory: %w", err)
	}
	if err := ensureConfig(store); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	conn, err := openSQLiteFn(dbPath(homeDir))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = conn.Close() }()
	migrationsFS, err := sqliteMigrationsFSFn()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	if err := conn.ApplyMigrationsFS(ctx, migrationsFS); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	repo, err := newSQLiteRepoFn(conn.DB())
	if err != nil {
		return fmt.Errorf("create repository: %w", err)
	}
	if err := seedTemplates(ctx, repo); err != nil {
		return fmt.Errorf("seed templates: %w", err)
	}
	return nil
}

func wipeLocalState(homeDir string) error {
	for _, path := range []string{
		dbPath(homeDir),
		dbPath(homeDir) + "-wal",
		dbPath(homeDir) + "-shm",
		dbPath(homeDir) + "-journal",
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove database artifact %s: %w", path, err)
		}
	}
	for _, dir := range []string{
		filepath.Join(basePath(homeDir), "figures"),
		filepath.Join(basePath(homeDir), "data"),
	} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s: %w", dir, err)
		}
	}
	if err := os.Remove(filepath.Join(basePath(homeDir), ".pc", "last_sync")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove last_sync: %w", err)
	}
	return nil
}

func compareSnapshotDirs(pathA string, pathB string) error {
	manifestA, err := gitsnapshot.Manifest(pathA)
	if err != nil {
		return err
	}
	manifestB, err := gitsnapshot.Manifest(pathB)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(manifestA, manifestB) {
		return fmt.Errorf("snapshot manifests differ")
	}
	return nil
}

func createRestoreBackupPath(homeDir string) string {
	return filepath.Join(
		basePath(homeDir),
		".pc",
		"backups",
		"restore-db-"+time.Now().UTC().Format("20060102T150405.000000000Z0700"),
	)
}
