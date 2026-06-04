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

	"github.com/conn-castle/personal-context/cli/internal/filesystem"
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

func buildLocalSnapshot(ctx context.Context, stack *localStack, filter repository.ListRecordsFilter) (gitsnapshot.Snapshot, error) {
	return buildSnapshot(ctx, stack.Repo, stack.Repo, func(_ context.Context, figure repository.RecordFigure) ([]byte, error) {
		path, err := stack.FS.ResolveFigurePath(figure.RecordID, figure.Filename)
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read local figure %s: %w", path, err)
		}
		return content, nil
	}, func(_ context.Context, chat repository.ChatSession) ([]byte, error) {
		if chat.RawSourceKey == nil {
			return nil, nil
		}
		path, err := stack.FS.ResolveChatSourcePath(chat.ID, *chat.RawSourceKey)
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read local chat raw source %s: %w", path, err)
		}
		return content, nil
	}, filter)
}

func buildCloudSnapshot(ctx context.Context, homeDir string, cloud *cloudStack, filter repository.ListRecordsFilter) (gitsnapshot.Snapshot, error) {
	localStack, err := openLocalStack(homeDir)
	if err != nil {
		return gitsnapshot.Snapshot{}, err
	}
	defer func() { _ = localStack.Close() }()

	return buildSnapshot(ctx, localStack.Repo, cloud.Repo, func(ctx context.Context, figure repository.RecordFigure) ([]byte, error) {
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
	}, func(ctx context.Context, chat repository.ChatSession) ([]byte, error) {
		if chat.RawSourceKey == nil {
			return nil, nil
		}
		if cloud.S3 == nil {
			return nil, fmt.Errorf("cloud S3 client is required to export chat raw source %s", chat.ID)
		}
		body, err := cloud.S3.Download(ctx, *chat.RawSourceKey)
		if err != nil {
			return nil, err
		}
		defer func() { _ = body.Close() }()
		content, err := io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("read cloud chat raw source %s: %w", *chat.RawSourceKey, err)
		}
		return content, nil
	}, filter)
}

func buildSnapshot(
	ctx context.Context,
	templateRepo repository.Repository,
	recordRepo repository.Repository,
	readFigure func(context.Context, repository.RecordFigure) ([]byte, error),
	readChatRawSource func(context.Context, repository.ChatSession) ([]byte, error),
	filter repository.ListRecordsFilter,
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
	projects, err := recordRepo.ListProjects(ctx, true)
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
	devices, err := recordRepo.ListDevices(ctx, true)
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

	records, err := recordRepo.ListRecords(ctx, filter)
	if err != nil {
		return gitsnapshot.Snapshot{}, fmt.Errorf("list records: %w", err)
	}
	snapshot.Records = make([]gitsnapshot.Record, 0, len(records))
	for _, record := range records {
		figures, err := recordRepo.ListRecordFiguresByRecordID(ctx, record.ID)
		if err != nil {
			return gitsnapshot.Snapshot{}, fmt.Errorf("list figures for %s: %w", record.ID, err)
		}
		dataFiles, err := recordRepo.ListRecordDataFilesByRecordID(ctx, record.ID)
		if err != nil {
			return gitsnapshot.Snapshot{}, fmt.Errorf("list data files for %s: %w", record.ID, err)
		}

		exportFigures := make([]gitsnapshot.Figure, 0, len(figures))
		for _, figure := range figures {
			content, err := readFigure(ctx, figure)
			if err != nil {
				return gitsnapshot.Snapshot{}, fmt.Errorf("load figure %s/%s: %w", record.ID, figure.Filename, err)
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

		snapshot.Records = append(snapshot.Records, gitsnapshot.Record{
			ID:             record.ID,
			Date:           record.Date,
			DayOrder:       record.DayOrder,
			ProjectID:      record.ProjectID,
			SourceDeviceID: record.SourceDeviceID,
			SourceRef:      record.SourceRef,
			GitRemoteURL:   record.GitRemoteURL,
			GitHash:        record.GitHash,
			HTMLContent:    record.HTMLContent,
			Notes:          record.Notes,
			Figures:        exportFigures,
			DataFiles:      exportDataFiles,
			CreatedAt:      record.CreatedAt,
			UpdatedAt:      record.UpdatedAt,
		})
	}
	chats, err := recordRepo.ListChatSessions(ctx, repository.ListChatSessionsFilter{})
	if err != nil {
		return gitsnapshot.Snapshot{}, fmt.Errorf("list chats: %w", err)
	}
	snapshot.Chats = make([]gitsnapshot.ChatSession, 0, len(chats))
	for _, chat := range chats {
		items, err := recordRepo.ListChatItems(ctx, chat.ID)
		if err != nil {
			return gitsnapshot.Snapshot{}, fmt.Errorf("list chat items for %s: %w", chat.ID, err)
		}
		exportItems := make([]gitsnapshot.ChatItem, 0, len(items))
		for _, item := range items {
			exportItems = append(exportItems, gitsnapshot.ChatItem{
				Ordinal:    item.Ordinal,
				Role:       item.Role,
				ItemType:   item.ItemType,
				Text:       item.Text,
				SearchText: item.SearchText,
				RawJSON:    item.RawJSON,
				CreatedAt:  item.CreatedAt,
			})
		}
		var rawSourceContent []byte
		if chat.RawSourceKey != nil {
			rawSourceContent, err = readChatRawSource(ctx, chat)
			if err != nil {
				return gitsnapshot.Snapshot{}, fmt.Errorf("load chat raw source %s: %w", chat.ID, err)
			}
		}
		snapshot.Chats = append(snapshot.Chats, gitsnapshot.ChatSession{
			ID:                    chat.ID,
			Source:                chat.Source,
			SourceSessionID:       chat.SourceSessionID,
			ParentSourceSessionID: chat.ParentSourceSessionID,
			SourceDeviceID:        chat.SourceDeviceID,
			ProjectID:             chat.ProjectID,
			CWD:                   chat.CWD,
			Title:                 chat.Title,
			StartedAt:             chat.StartedAt,
			LastActivityAt:        chat.LastActivityAt,
			OriginalSourcePath:    chat.OriginalSourcePath,
			RawSourceKey:          chat.RawSourceKey,
			RawSourceContent:      rawSourceContent,
			CreatedAt:             chat.CreatedAt,
			UpdatedAt:             chat.UpdatedAt,
			Items:                 exportItems,
		})
	}
	return snapshot, nil
}

func importSnapshotIntoStack(ctx context.Context, stack *localStack, snapshot gitsnapshot.Snapshot) (importStats, error) {
	stats := importStats{}

	if err := validateSnapshotChatSourceIdentities(ctx, stack.Repo, snapshot.Chats); err != nil {
		return stats, err
	}

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

	for _, record := range snapshot.Records {
		existing, err := stack.Repo.GetRecordByID(ctx, record.ID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return stats, fmt.Errorf("get record %s: %w", record.ID, err)
		}
		if errors.Is(err, repository.ErrNotFound) {
			if err := createImportedRecord(ctx, stack, record); err != nil {
				return stats, err
			}
			stats.Created++
			continue
		}
		if !record.UpdatedAt.After(existing.UpdatedAt) {
			stats.Skipped++
			continue
		}
		if err := updateImportedRecord(ctx, stack, record); err != nil {
			return stats, err
		}
		stats.Updated++
	}
	for _, chat := range snapshot.Chats {
		if err := ensureSnapshotChatSourceIdentity(ctx, stack.Repo, chat); err != nil {
			return stats, err
		}
		rollback, err := captureSnapshotChatRollback(ctx, stack, chat.ID)
		if err != nil {
			return stats, err
		}
		if rollback.existed && !chat.UpdatedAt.After(rollback.session.UpdatedAt) {
			rollback.cleanup(stack)
			stats.Skipped++
			continue
		}
		rawStage, err := stageSnapshotChatRawSource(stack, chat)
		if err != nil {
			rollback.cleanup(stack)
			return stats, err
		}
		createdAt := chat.CreatedAt.UTC()
		updatedAt := chat.UpdatedAt.UTC()
		stored, _, err := stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:                    chat.ID,
				Source:                chat.Source,
				SourceSessionID:       chat.SourceSessionID,
				ParentSourceSessionID: chat.ParentSourceSessionID,
				SourceDeviceID:        chat.SourceDeviceID,
				ProjectID:             chat.ProjectID,
				CWD:                   chat.CWD,
				Title:                 chat.Title,
				StartedAt:             chat.StartedAt,
				LastActivityAt:        chat.LastActivityAt,
				OriginalSourcePath:    chat.OriginalSourcePath,
				RawSourceKey:          chat.RawSourceKey,
				CreatedAt:             &createdAt,
				UpdatedAt:             &updatedAt,
			},
			ClearDeleted: true,
		})
		if err != nil {
			deleteSnapshotChatRawStage(stack, rawStage)
			rollback.cleanup(stack)
			return stats, fmt.Errorf("upsert chat %s: %w", chat.ID, err)
		}
		itemInputs := make([]repository.CreateChatItemInput, 0, len(chat.Items))
		for _, item := range chat.Items {
			createdAt := item.CreatedAt.UTC()
			itemInputs = append(itemInputs, repository.CreateChatItemInput{
				SessionID:  stored.ID,
				Ordinal:    item.Ordinal,
				Role:       item.Role,
				ItemType:   item.ItemType,
				Text:       item.Text,
				SearchText: item.SearchText,
				RawJSON:    item.RawJSON,
				CreatedAt:  &createdAt,
			})
		}
		if err := replaceChatItems(ctx, stack.Repo, stored.ID, itemInputs); err != nil {
			deleteSnapshotChatRawStage(stack, rawStage)
			rollback.restore(ctx, stack)
			rollback.cleanup(stack)
			return stats, fmt.Errorf("replace chat items %s: %w", stored.ID, err)
		}
		if rawStage != nil {
			if _, err := stack.FS.PromoteChatSourceStage(*rawStage); err != nil {
				deleteSnapshotChatRawStage(stack, rawStage)
				rollback.restore(ctx, stack)
				rollback.cleanup(stack)
				return stats, fmt.Errorf("promote chat raw source %s: %w", chat.ID, err)
			}
		}
		if rollback.existed {
			stats.Updated++
		} else {
			stats.Created++
		}
		rollback.cleanup(stack)
	}

	return stats, nil
}

func validateSnapshotChatSourceIdentities(ctx context.Context, repo repository.Repository, chats []gitsnapshot.ChatSession) error {
	seen := make(map[string]string, len(chats))
	for _, chat := range chats {
		key := chat.Source + "\x00" + chat.SourceSessionID
		if existingID, ok := seen[key]; ok {
			// Even a same-id duplicate is rejected: directory iteration
			// order in gitsnapshot is not stable, so silently keeping the
			// first-seen entry would let items, metadata, or raw content
			// be picked non-deterministically from different snapshot dirs.
			return fmt.Errorf(
				"chat source %s/%s appears multiple times in snapshot (ids %s, %s); resolve the duplicate snapshot entries before import",
				chat.Source,
				chat.SourceSessionID,
				existingID,
				chat.ID,
			)
		}
		seen[key] = chat.ID
		if err := ensureSnapshotChatSourceIdentity(ctx, repo, chat); err != nil {
			return err
		}
	}
	return nil
}

func ensureSnapshotChatSourceIdentity(ctx context.Context, repo repository.Repository, chat gitsnapshot.ChatSession) error {
	existing, err := repo.GetChatSessionBySource(ctx, chat.Source, chat.SourceSessionID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load chat source %s/%s: %w", chat.Source, chat.SourceSessionID, err)
	}
	if existing.ID != chat.ID {
		return fmt.Errorf(
			"chat source %s/%s already exists with id %s; snapshot id %s cannot be imported without manual resolution",
			chat.Source,
			chat.SourceSessionID,
			existing.ID,
			chat.ID,
		)
	}
	return nil
}

func captureSnapshotChatRollback(ctx context.Context, stack *localStack, chatID string) (chatImportRollbackState, error) {
	existing, err := stack.Repo.GetChatSessionByID(ctx, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return chatImportRollbackState{session: repository.ChatSession{ID: chatID}}, nil
		}
		return chatImportRollbackState{}, fmt.Errorf("look up existing chat rollback state: %w", err)
	}
	items, err := stack.Repo.ListChatItems(ctx, existing.ID)
	if err != nil {
		return chatImportRollbackState{}, fmt.Errorf("list existing chat items for rollback: %w", err)
	}
	state := chatImportRollbackState{existed: true, session: existing, items: items}
	if existing.RawSourceKey != nil && stack.FS != nil {
		rawPath, err := stack.FS.ResolveChatSourcePath(existing.ID, *existing.RawSourceKey)
		if err != nil {
			return chatImportRollbackState{}, fmt.Errorf("resolve existing chat raw source for rollback: %w", err)
		}
		backup, err := stack.FS.CopyChatSourceToStage(existing.ID, rawPath)
		if err == nil {
			state.rawBackup = &backup
		} else if !errors.Is(err, os.ErrNotExist) {
			return chatImportRollbackState{}, fmt.Errorf("back up existing chat raw source for rollback: %w", err)
		}
	}
	return state, nil
}

func stageSnapshotChatRawSource(stack *localStack, chat gitsnapshot.ChatSession) (*filesystem.ChatSourceStage, error) {
	if chat.RawSourceKey == nil {
		return nil, nil
	}
	if len(chat.RawSourceContent) == 0 {
		return nil, fmt.Errorf("chat %s raw_source_key is set but raw source content is empty", chat.ID)
	}
	path, err := stack.FS.ResolveChatSourcePath(chat.ID, *chat.RawSourceKey)
	if err != nil {
		return nil, fmt.Errorf("resolve chat raw source %s: %w", chat.ID, err)
	}
	rawRoot := filepath.Dir(filepath.Dir(path))
	if err := os.MkdirAll(rawRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create chat raw staging root: %w", err)
	}
	stageDir, err := os.MkdirTemp(rawRoot, ".staging-"+chat.ID+"-")
	if err != nil {
		return nil, fmt.Errorf("create chat raw stage: %w", err)
	}
	stage := filesystem.ChatSourceStage{
		ChatSessionID: chat.ID,
		RawSourceKey:  *chat.RawSourceKey,
		StagedPath:    filepath.Join(stageDir, filepath.Base(path)),
		Size:          int64(len(chat.RawSourceContent)),
	}
	if err := writeTextFileAtomically(stage.StagedPath, chat.RawSourceContent, 0o700, 0o600); err != nil {
		_ = stack.FS.DeleteChatSourceStage(stage)
		return nil, fmt.Errorf("stage chat raw source %s: %w", chat.ID, err)
	}
	return &stage, nil
}

func deleteSnapshotChatRawStage(stack *localStack, stage *filesystem.ChatSourceStage) {
	if stack == nil || stack.FS == nil || stage == nil {
		return
	}
	_ = stack.FS.DeleteChatSourceStage(*stage)
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

func createImportedRecord(ctx context.Context, stack *localStack, record gitsnapshot.Record) error {
	createdAt := record.CreatedAt.UTC()
	updatedAt := record.UpdatedAt.UTC()
	if _, err := stack.Repo.CreateRecord(ctx, repository.CreateRecordInput{
		ID:             record.ID,
		Date:           record.Date,
		DayOrder:       record.DayOrder,
		HTMLContent:    record.HTMLContent,
		Notes:          record.Notes,
		ProjectID:      record.ProjectID,
		SourceDeviceID: record.SourceDeviceID,
		SourceRef:      record.SourceRef,
		GitRemoteURL:   record.GitRemoteURL,
		GitHash:        record.GitHash,
		CreatedAt:      &createdAt,
		UpdatedAt:      &updatedAt,
	}); err != nil {
		return fmt.Errorf("create record %s: %w", record.ID, err)
	}
	if err := replaceRecordChildren(ctx, stack, record); err != nil {
		return err
	}
	return nil
}

func updateImportedRecord(ctx context.Context, stack *localStack, record gitsnapshot.Record) error {
	updatedAt := record.UpdatedAt.UTC()
	if _, err := stack.Repo.UpdateRecord(ctx, repository.UpdateRecordInput{
		ID:             record.ID,
		Date:           record.Date,
		DayOrder:       record.DayOrder,
		HTMLContent:    record.HTMLContent,
		Notes:          record.Notes,
		ProjectID:      record.ProjectID,
		SourceDeviceID: record.SourceDeviceID,
		SourceRef:      record.SourceRef,
		GitRemoteURL:   record.GitRemoteURL,
		GitHash:        record.GitHash,
		UpdatedAt:      &updatedAt,
		SetDeletedAt:   true,
	}); err != nil {
		return fmt.Errorf("update record %s: %w", record.ID, err)
	}
	if err := replaceRecordChildren(ctx, stack, record); err != nil {
		return err
	}
	return nil
}

func replaceRecordChildren(ctx context.Context, stack *localStack, record gitsnapshot.Record) error {
	existingFigures, err := stack.Repo.ListRecordFiguresByRecordID(ctx, record.ID)
	if err != nil {
		return fmt.Errorf("list existing figures for %s: %w", record.ID, err)
	}
	for _, figure := range existingFigures {
		if err := stack.Repo.DeleteRecordFigure(ctx, figure.ID); err != nil {
			return fmt.Errorf("delete existing figure %s/%s: %w", record.ID, figure.Filename, err)
		}
	}
	existingDataFiles, err := stack.Repo.ListRecordDataFilesByRecordID(ctx, record.ID)
	if err != nil {
		return fmt.Errorf("list existing data files for %s: %w", record.ID, err)
	}
	for _, file := range existingDataFiles {
		if err := stack.Repo.DeleteRecordDataFile(ctx, file.ID); err != nil {
			return fmt.Errorf("delete existing data file %s/%s: %w", record.ID, file.Filename, err)
		}
	}
	if err := stack.FS.DeleteRecordDir(record.ID); err != nil {
		return fmt.Errorf("reset local files for %s: %w", record.ID, err)
	}
	for _, figure := range record.Figures {
		path, err := stack.FS.ResolveFigurePath(record.ID, figure.Filename)
		if err != nil {
			return err
		}
		if err := writeTextFileAtomically(path, figure.Content, 0o700, 0o644); err != nil {
			return fmt.Errorf("write local figure %s/%s: %w", record.ID, figure.Filename, err)
		}
		if _, err := stack.Repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
			RecordID: record.ID,
			Filename: figure.Filename,
			S3Key:    figure.S3Key,
			AltText:  figure.AltText,
		}); err != nil {
			return fmt.Errorf("create figure row %s/%s: %w", record.ID, figure.Filename, err)
		}
	}
	for _, file := range record.DataFiles {
		if _, err := stack.Repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
			RecordID:    record.ID,
			Filename:    file.Filename,
			S3Key:       file.S3Key,
			Size:        file.Size,
			Hash:        file.Hash,
			Description: file.Description,
		}); err != nil {
			return fmt.Errorf("create data file row %s/%s: %w", record.ID, file.Filename, err)
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
		filepath.Join(basePath(homeDir), "chats", "raw"),
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
