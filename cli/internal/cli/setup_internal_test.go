package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/repository"

	_ "modernc.org/sqlite"
)

// mockRepo is a minimal repository mock for unit-testing command logic.
type mockRepo struct {
	getTemplateByNameFn func(ctx context.Context, name string) (repository.Template, error)
	createTemplateFn    func(ctx context.Context, input repository.CreateTemplateInput) (repository.Template, error)
	createRecordFn      func(ctx context.Context, input repository.CreateRecordInput) (repository.Record, error)
	getRecordByIDFn     func(ctx context.Context, id string) (repository.Record, error)
	listRecordsFn       func(ctx context.Context, filter repository.ListRecordsFilter) ([]repository.Record, error)
	countRecordsFn      func(ctx context.Context, filter repository.ListRecordsFilter) (int, error)
	countChildrenFn     func(ctx context.Context, recordIDs []string) (map[string]repository.ChildCounts, error)
	listFiguresFn       func(ctx context.Context, recordID string) ([]repository.RecordFigure, error)
	createFigureFn      func(ctx context.Context, input repository.CreateRecordFigureInput) (repository.RecordFigure, error)
	updateFigureFn      func(ctx context.Context, input repository.UpdateRecordFigureInput) (repository.RecordFigure, error)
	deleteFigureFn      func(ctx context.Context, id int64) error
	listDataFilesFn     func(ctx context.Context, recordID string) ([]repository.RecordDataFile, error)
	createDataFileFn    func(ctx context.Context, input repository.CreateRecordDataFileInput) (repository.RecordDataFile, error)
	updateDataFileFn    func(ctx context.Context, input repository.UpdateRecordDataFileInput) (repository.RecordDataFile, error)
	deleteDataFileFn    func(ctx context.Context, id int64) error
	listTemplatesFn     func(ctx context.Context) ([]repository.Template, error)
	createProjectFn     func(ctx context.Context, input repository.CreateRegistryInput) (repository.Project, error)
	getProjectByIDFn    func(ctx context.Context, id string) (repository.Project, error)
	listProjectsFn      func(ctx context.Context, includeArchived bool) ([]repository.Project, error)
	upsertProjectFn     func(ctx context.Context, project repository.Project) (bool, error)
	listProjectPathsFn  func(ctx context.Context, projectID *string) ([]repository.ProjectPath, error)
	createDeviceFn      func(ctx context.Context, input repository.CreateRegistryInput) (repository.Device, error)
	getDeviceByIDFn     func(ctx context.Context, id string) (repository.Device, error)
	listDevicesFn       func(ctx context.Context, includeArchived bool) ([]repository.Device, error)
	upsertDeviceFn      func(ctx context.Context, device repository.Device) (bool, error)
	upsertChatSessionFn func(ctx context.Context, input repository.UpsertChatSessionInput) (repository.ChatSession, bool, error)
	getChatByIDFn       func(ctx context.Context, id string) (repository.ChatSession, error)
	getChatBySourceFn   func(ctx context.Context, source string, sourceSessionID string) (repository.ChatSession, error)
	listChatSessionsFn  func(ctx context.Context, filter repository.ListChatSessionsFilter) ([]repository.ChatSession, error)
	countChatSessionsFn func(ctx context.Context, filter repository.ListChatSessionsFilter) (int, error)
	listChatItemsFn     func(ctx context.Context, sessionID string) ([]repository.ChatItem, error)
	searchChatItemsFn   func(ctx context.Context, filter repository.SearchChatItemsFilter) ([]repository.ChatSearchResult, error)
	maxChatOrdinalFn    func(ctx context.Context, sessionID string) (int, error)
	createChatItemFn    func(ctx context.Context, input repository.CreateChatItemInput) (repository.ChatItem, error)
	appendChatItemsFn   func(ctx context.Context, sessionID string, items []repository.CreateChatItemInput) error
	replaceChatItemsFn  func(ctx context.Context, sessionID string, items []repository.CreateChatItemInput) error
	writeChatBatchFn    func(ctx context.Context, ops []repository.ChatImportOp) ([]repository.ChatImportResult, error)
}

func (m *mockRepo) GetTemplateByName(ctx context.Context, name string) (repository.Template, error) {
	if m.getTemplateByNameFn != nil {
		return m.getTemplateByNameFn(ctx, name)
	}
	return repository.Template{}, repository.ErrNotFound
}

func (m *mockRepo) CreateTemplate(ctx context.Context, input repository.CreateTemplateInput) (repository.Template, error) {
	if m.createTemplateFn != nil {
		return m.createTemplateFn(ctx, input)
	}
	return repository.Template{}, nil
}

// Stubs for remaining Repository interface methods.
func (m *mockRepo) CreateRecord(ctx context.Context, input repository.CreateRecordInput) (repository.Record, error) {
	if m.createRecordFn != nil {
		return m.createRecordFn(ctx, input)
	}
	return repository.Record{}, nil
}
func (m *mockRepo) GetRecordByID(ctx context.Context, id string) (repository.Record, error) {
	if m.getRecordByIDFn != nil {
		return m.getRecordByIDFn(ctx, id)
	}
	return repository.Record{}, nil
}
func (m *mockRepo) UpdateRecord(context.Context, repository.UpdateRecordInput) (repository.Record, error) {
	return repository.Record{}, nil
}
func (m *mockRepo) ListRecords(ctx context.Context, filter repository.ListRecordsFilter) ([]repository.Record, error) {
	if m.listRecordsFn != nil {
		return m.listRecordsFn(ctx, filter)
	}
	return nil, nil
}
func (m *mockRepo) CountRecords(ctx context.Context, filter repository.ListRecordsFilter) (int, error) {
	if m.countRecordsFn != nil {
		return m.countRecordsFn(ctx, filter)
	}
	filter.Limit = 0
	records, err := m.ListRecords(ctx, filter)
	if err != nil {
		return 0, err
	}
	return len(records), nil
}
func (m *mockRepo) CountRecordChildren(ctx context.Context, recordIDs []string) (map[string]repository.ChildCounts, error) {
	if m.countChildrenFn != nil {
		return m.countChildrenFn(ctx, recordIDs)
	}
	counts := make(map[string]repository.ChildCounts)
	for _, id := range recordIDs {
		figures, err := m.ListRecordFiguresByRecordID(ctx, id)
		if err != nil {
			return nil, err
		}
		dataFiles, err := m.ListRecordDataFilesByRecordID(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(figures) > 0 || len(dataFiles) > 0 {
			counts[id] = repository.ChildCounts{Figures: len(figures), DataFiles: len(dataFiles)}
		}
	}
	return counts, nil
}
func (m *mockRepo) SoftDeleteRecord(context.Context, string) error { return nil }
func (m *mockRepo) RestoreRecord(context.Context, string) error    { return nil }
func (m *mockRepo) DeleteRecord(context.Context, string) error     { return nil }
func (m *mockRepo) CreateRecordFigure(ctx context.Context, input repository.CreateRecordFigureInput) (repository.RecordFigure, error) {
	if m.createFigureFn != nil {
		return m.createFigureFn(ctx, input)
	}
	return repository.RecordFigure{}, nil
}
func (m *mockRepo) GetRecordFigureByID(context.Context, int64) (repository.RecordFigure, error) {
	return repository.RecordFigure{}, nil
}
func (m *mockRepo) UpdateRecordFigure(ctx context.Context, input repository.UpdateRecordFigureInput) (repository.RecordFigure, error) {
	if m.updateFigureFn != nil {
		return m.updateFigureFn(ctx, input)
	}
	return repository.RecordFigure{}, nil
}
func (m *mockRepo) ListRecordFiguresByRecordID(ctx context.Context, recordID string) ([]repository.RecordFigure, error) {
	if m.listFiguresFn != nil {
		return m.listFiguresFn(ctx, recordID)
	}
	return nil, nil
}
func (m *mockRepo) DeleteRecordFigure(ctx context.Context, id int64) error {
	if m.deleteFigureFn != nil {
		return m.deleteFigureFn(ctx, id)
	}
	return nil
}
func (m *mockRepo) CreateRecordDataFile(ctx context.Context, input repository.CreateRecordDataFileInput) (repository.RecordDataFile, error) {
	if m.createDataFileFn != nil {
		return m.createDataFileFn(ctx, input)
	}
	return repository.RecordDataFile{}, nil
}
func (m *mockRepo) GetRecordDataFileByID(context.Context, int64) (repository.RecordDataFile, error) {
	return repository.RecordDataFile{}, nil
}
func (m *mockRepo) UpdateRecordDataFile(ctx context.Context, input repository.UpdateRecordDataFileInput) (repository.RecordDataFile, error) {
	if m.updateDataFileFn != nil {
		return m.updateDataFileFn(ctx, input)
	}
	return repository.RecordDataFile{}, nil
}
func (m *mockRepo) ListRecordDataFilesByRecordID(ctx context.Context, recordID string) ([]repository.RecordDataFile, error) {
	if m.listDataFilesFn != nil {
		return m.listDataFilesFn(ctx, recordID)
	}
	return nil, nil
}
func (m *mockRepo) DeleteRecordDataFile(ctx context.Context, id int64) error {
	if m.deleteDataFileFn != nil {
		return m.deleteDataFileFn(ctx, id)
	}
	return nil
}
func (m *mockRepo) UpdateTemplate(context.Context, repository.UpdateTemplateInput) (repository.Template, error) {
	return repository.Template{}, nil
}
func (m *mockRepo) ListTemplates(ctx context.Context) ([]repository.Template, error) {
	if m.listTemplatesFn != nil {
		return m.listTemplatesFn(ctx)
	}
	return nil, nil
}
func (m *mockRepo) DeleteTemplate(context.Context, string) error { return nil }
func (m *mockRepo) GetSyncVersion(context.Context) (repository.SyncVersion, error) {
	return repository.SyncVersion{}, nil
}
func (m *mockRepo) CreateProject(ctx context.Context, input repository.CreateRegistryInput) (repository.Project, error) {
	if m.createProjectFn != nil {
		return m.createProjectFn(ctx, input)
	}
	return repository.Project{}, nil
}
func (m *mockRepo) GetProjectByID(ctx context.Context, id string) (repository.Project, error) {
	if m.getProjectByIDFn != nil {
		return m.getProjectByIDFn(ctx, id)
	}
	return repository.Project{}, repository.ErrNotFound
}
func (m *mockRepo) ListProjects(ctx context.Context, includeArchived bool) ([]repository.Project, error) {
	if m.listProjectsFn != nil {
		return m.listProjectsFn(ctx, includeArchived)
	}
	return nil, nil
}
func (m *mockRepo) ArchiveProject(context.Context, string) (repository.Project, error) {
	return repository.Project{}, nil
}
func (m *mockRepo) RestoreProject(context.Context, string) (repository.Project, error) {
	return repository.Project{}, nil
}
func (m *mockRepo) UpsertProjectForImport(ctx context.Context, project repository.Project) (bool, error) {
	if m.upsertProjectFn != nil {
		return m.upsertProjectFn(ctx, project)
	}
	return true, nil
}
func (m *mockRepo) UpsertProjectPath(context.Context, repository.CreateProjectPathInput) (repository.ProjectPath, bool, error) {
	return repository.ProjectPath{}, true, nil
}
func (m *mockRepo) ListProjectPaths(ctx context.Context, projectID *string) ([]repository.ProjectPath, error) {
	if m.listProjectPathsFn != nil {
		return m.listProjectPathsFn(ctx, projectID)
	}
	return nil, nil
}
func (m *mockRepo) BackfillChatProjects(context.Context) (int, error) { return 0, nil }
func (m *mockRepo) CreateDevice(ctx context.Context, input repository.CreateRegistryInput) (repository.Device, error) {
	if m.createDeviceFn != nil {
		return m.createDeviceFn(ctx, input)
	}
	return repository.Device{}, nil
}
func (m *mockRepo) GetDeviceByID(ctx context.Context, id string) (repository.Device, error) {
	if m.getDeviceByIDFn != nil {
		return m.getDeviceByIDFn(ctx, id)
	}
	return repository.Device{}, repository.ErrNotFound
}
func (m *mockRepo) ListDevices(ctx context.Context, includeArchived bool) ([]repository.Device, error) {
	if m.listDevicesFn != nil {
		return m.listDevicesFn(ctx, includeArchived)
	}
	return nil, nil
}
func (m *mockRepo) ArchiveDevice(context.Context, string) (repository.Device, error) {
	return repository.Device{}, nil
}
func (m *mockRepo) RestoreDevice(context.Context, string) (repository.Device, error) {
	return repository.Device{}, nil
}
func (m *mockRepo) UpsertDeviceForImport(ctx context.Context, device repository.Device) (bool, error) {
	if m.upsertDeviceFn != nil {
		return m.upsertDeviceFn(ctx, device)
	}
	return true, nil
}
func (m *mockRepo) UpsertChatSession(ctx context.Context, input repository.UpsertChatSessionInput) (repository.ChatSession, bool, error) {
	if m.upsertChatSessionFn != nil {
		return m.upsertChatSessionFn(ctx, input)
	}
	return repository.ChatSession{}, true, nil
}
func (m *mockRepo) GetChatSessionByID(ctx context.Context, id string) (repository.ChatSession, error) {
	if m.getChatByIDFn != nil {
		return m.getChatByIDFn(ctx, id)
	}
	return repository.ChatSession{}, repository.ErrNotFound
}
func (m *mockRepo) GetChatSessionBySource(ctx context.Context, source string, sourceSessionID string) (repository.ChatSession, error) {
	if m.getChatBySourceFn != nil {
		return m.getChatBySourceFn(ctx, source, sourceSessionID)
	}
	return repository.ChatSession{}, repository.ErrNotFound
}
func (m *mockRepo) ListChatSessions(ctx context.Context, filter repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
	if m.listChatSessionsFn != nil {
		return m.listChatSessionsFn(ctx, filter)
	}
	return nil, nil
}
func (m *mockRepo) CountChatSessions(ctx context.Context, filter repository.ListChatSessionsFilter) (int, error) {
	if m.countChatSessionsFn != nil {
		return m.countChatSessionsFn(ctx, filter)
	}
	return 0, nil
}
func (m *mockRepo) SoftDeleteChatSession(context.Context, string) error { return nil }
func (m *mockRepo) RestoreChatSession(context.Context, string) error    { return nil }
func (m *mockRepo) DeleteChatSession(context.Context, string) error     { return nil }
func (m *mockRepo) MaxChatItemOrdinal(ctx context.Context, sessionID string) (int, error) {
	if m.maxChatOrdinalFn != nil {
		return m.maxChatOrdinalFn(ctx, sessionID)
	}
	return -1, nil
}
func (m *mockRepo) CreateChatItem(ctx context.Context, input repository.CreateChatItemInput) (repository.ChatItem, error) {
	if m.createChatItemFn != nil {
		return m.createChatItemFn(ctx, input)
	}
	return repository.ChatItem{}, nil
}
func (m *mockRepo) AppendChatItems(ctx context.Context, sessionID string, items []repository.CreateChatItemInput) error {
	if m.appendChatItemsFn != nil {
		return m.appendChatItemsFn(ctx, sessionID, items)
	}
	return nil
}
func (m *mockRepo) ReplaceChatItems(ctx context.Context, sessionID string, items []repository.CreateChatItemInput) error {
	if m.replaceChatItemsFn != nil {
		return m.replaceChatItemsFn(ctx, sessionID, items)
	}
	return nil
}
func (m *mockRepo) WriteChatImportBatch(ctx context.Context, ops []repository.ChatImportOp) ([]repository.ChatImportResult, error) {
	if m.writeChatBatchFn != nil {
		return m.writeChatBatchFn(ctx, ops)
	}
	results := make([]repository.ChatImportResult, 0, len(ops))
	for _, op := range ops {
		input := op.Session.CreateChatSessionInput
		results = append(results, repository.ChatImportResult{
			Session: repository.ChatSession{
				ID:                 input.ID,
				Source:             input.Source,
				SourceSessionID:    input.SourceSessionID,
				SourceDeviceID:     input.SourceDeviceID,
				ProjectID:          input.ProjectID,
				CWD:                input.CWD,
				Title:              input.Title,
				StartedAt:          input.StartedAt,
				LastActivityAt:     input.LastActivityAt,
				OriginalSourcePath: input.OriginalSourcePath,
				RawSourceKey:       input.RawSourceKey,
				DeletedAt:          input.DeletedAt,
			},
			Created: true,
		})
	}
	return results, nil
}
func (m *mockRepo) RunChatImportBulkMode(ctx context.Context, fn func(context.Context) (bool, error)) error {
	_, err := fn(ctx)
	return err
}
func (m *mockRepo) ListChatItems(ctx context.Context, sessionID string) ([]repository.ChatItem, error) {
	if m.listChatItemsFn != nil {
		return m.listChatItemsFn(ctx, sessionID)
	}
	return nil, nil
}
func (m *mockRepo) SearchChatItems(ctx context.Context, filter repository.SearchChatItemsFilter) ([]repository.ChatSearchResult, error) {
	if m.searchChatItemsFn != nil {
		return m.searchChatItemsFn(ctx, filter)
	}
	return nil, nil
}
func (m *mockRepo) SearchAll(context.Context, repository.UnifiedSearchFilter) ([]repository.DomainSearchResult, error) {
	return nil, nil
}
func (m *mockRepo) CountActiveRecords(context.Context) (int, error)       { return 0, nil }
func (m *mockRepo) CountTrashedRecords(context.Context) (int, error)      { return 0, nil }
func (m *mockRepo) PurgeDeletedRecords(context.Context) ([]string, error) { return nil, nil }

func TestSeedTemplatesAllNew(t *testing.T) {
	created := make(map[string]bool)
	repo := &mockRepo{
		getTemplateByNameFn: func(_ context.Context, _ string) (repository.Template, error) {
			return repository.Template{}, repository.ErrNotFound
		},
		createTemplateFn: func(_ context.Context, input repository.CreateTemplateInput) (repository.Template, error) {
			created[input.Name] = true
			return repository.Template{Name: input.Name}, nil
		},
	}

	if err := seedTemplates(context.Background(), repo); err != nil {
		t.Fatalf("seedTemplates() error = %v", err)
	}

	for _, tmpl := range builtinTemplates {
		if !created[tmpl.Name] {
			t.Fatalf("template %q was not created", tmpl.Name)
		}
	}
}

func TestSeedTemplatesSkipsExisting(t *testing.T) {
	repo := &mockRepo{
		getTemplateByNameFn: func(_ context.Context, _ string) (repository.Template, error) {
			return repository.Template{}, nil // already exists
		},
		createTemplateFn: func(_ context.Context, _ repository.CreateTemplateInput) (repository.Template, error) {
			t.Fatal("CreateTemplate should not be called for existing templates")
			return repository.Template{}, nil
		},
	}

	if err := seedTemplates(context.Background(), repo); err != nil {
		t.Fatalf("seedTemplates() error = %v", err)
	}
}

func TestSeedTemplatesFailsOnCheckError(t *testing.T) {
	repo := &mockRepo{
		getTemplateByNameFn: func(_ context.Context, _ string) (repository.Template, error) {
			return repository.Template{}, errors.New("db connection lost")
		},
	}

	err := seedTemplates(context.Background(), repo)
	if err == nil {
		t.Fatal("expected error when GetTemplateByName fails with non-ErrNotFound")
	}
}

func TestSeedTemplatesFailsOnCreateError(t *testing.T) {
	repo := &mockRepo{
		getTemplateByNameFn: func(_ context.Context, _ string) (repository.Template, error) {
			return repository.Template{}, repository.ErrNotFound
		},
		createTemplateFn: func(_ context.Context, _ repository.CreateTemplateInput) (repository.Template, error) {
			return repository.Template{}, errors.New("insert failed")
		},
	}

	err := seedTemplates(context.Background(), repo)
	if err == nil {
		t.Fatal("expected error when CreateTemplate fails")
	}
}

func TestEnsureConfigSkipsExistingValidConfig(t *testing.T) {
	homeDir := t.TempDir()
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	// Write a valid config first
	if err := store.Write(config.Config{}); err != nil {
		t.Fatalf("Write error = %v", err)
	}

	// ensureConfig should skip
	if err := ensureConfig(store); err != nil {
		t.Fatalf("ensureConfig() error = %v", err)
	}
}

func TestEnsureConfigCreatesWhenMissing(t *testing.T) {
	homeDir := t.TempDir()
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	// No config file exists
	if err := ensureConfig(store); err != nil {
		t.Fatalf("ensureConfig() error = %v", err)
	}

	// Verify it was created
	cfg, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	mode, err := cfg.Mode()
	if err != nil {
		t.Fatalf("Mode() error = %v", err)
	}
	if mode != config.ModeLocalOnly {
		t.Fatalf("expected local-only, got %q", mode)
	}
}

func TestEnsureConfigFailsOnInvalidExistingConfig(t *testing.T) {
	homeDir := t.TempDir()
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	configPath := store.Path()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	if err := ensureConfig(store); err == nil {
		t.Fatal("expected ensureConfig to fail for invalid existing config")
	}
}

func TestRunSetupWithBlockedDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Create a file where the .pc dir should be to block MkdirAll
	blocker := filepath.Join(homeDir, "personal-context", ".pc")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	if err := os.WriteFile(blocker, []byte("block"), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runSetup(context.Background(), stdout, stderr, strings.NewReader("n\n"), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected error when directory creation blocked")
	}
}

func TestOpenLocalStackFailsOnBadDBPath(t *testing.T) {
	homeDir := t.TempDir()
	// Write a valid config so config read passes
	store, _ := config.NewStore(homeDir)
	_ = store.Write(config.Config{})

	// Block the DB path by creating a directory named "pc.db" so Open fails
	dbFile := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	if err := os.MkdirAll(dbFile, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	_, err := openLocalStack(homeDir)
	if err == nil {
		t.Fatal("expected error when DB path is a directory")
	}
}

func TestRunSetupDatabaseOpenFails(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Create .pc dir to pass MkdirAll, but block pc.db
	pcDir := filepath.Join(homeDir, "personal-context", ".pc")
	if err := os.MkdirAll(pcDir, 0o700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	// Create a directory named "pc.db" to make sqlite.Open fail
	if err := os.MkdirAll(filepath.Join(pcDir, "pc.db"), 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected error when database open fails")
	}
}

func TestRunSetupSeedTemplatesError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Do a clean setup first
	stdout := &bytes.Buffer{}
	err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts())
	if err != nil {
		t.Fatalf("initial setup failed: %v", err)
	}

	// Corrupt the templates table to make seed fail
	db := openTestDBInternal(t, homeDir)
	if _, err := db.Exec("DROP TABLE templates"); err != nil {
		t.Fatalf("drop templates: %v", err)
	}
	_ = db.Close()

	// Re-run setup — should fail on seed
	stdout.Reset()
	err = runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected error when templates table is missing")
	}
}

// TestRunSetupFailsLoudlyAgainstPreChatTableStore asserts that
// when `pc setup` runs against an existing v0.1.1-vintage store whose
// `schema_migrations` row marks `001_initial.sql` as already applied, the
// migration runner skips re-application and leaves `chat_session` /
// `chat_item` missing. The previous behavior was to print "Personal Context
// initialized at ..." and exit 0, silently leaving the user on an
// incompatible schema. Setup must now fail loudly with recovery guidance.
func TestRunSetupFailsLoudlyAgainstPreChatTableStore(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Clean setup so schema_migrations records 001_initial.sql.
	stdout := &bytes.Buffer{}
	if err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts()); err != nil {
		t.Fatalf("initial setup failed: %v", err)
	}

	// Simulate a pre-chat-table store: drop chat_session while the
	// schema_migrations row stays in place. Subsequent setup runs will
	// see the version as applied and skip schema re-application.
	db := openTestDBInternal(t, homeDir)
	if _, err := db.Exec("DROP TABLE chat_item"); err != nil {
		t.Fatalf("drop chat_item: %v", err)
	}
	if _, err := db.Exec("DROP TABLE chat_session"); err != nil {
		t.Fatalf("drop chat_session: %v", err)
	}
	_ = db.Close()

	stdout.Reset()
	err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected setup to fail when chat_session is missing, got nil")
	}
	if !strings.Contains(err.Error(), "chat_session") {
		t.Fatalf("expected error to name the missing table, got: %v", err)
	}
	if !strings.Contains(err.Error(), "predates") {
		t.Fatalf("expected error to mention that the store predates the current schema, got: %v", err)
	}
	expectedBase := basePath(homeDir)
	if !strings.Contains(err.Error(), expectedBase) {
		t.Fatalf("expected error to name the actual store path %q (honoring PC_HOME), got: %v", expectedBase, err)
	}
	// The success message must not have been printed.
	if strings.Contains(stdout.String(), "Personal Context initialized at") {
		t.Fatalf("setup printed success message despite schema mismatch: %q", stdout.String())
	}
}

func TestRunSetupFailsLoudlyAgainstStandaloneChatFTSStore(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	stdout := &bytes.Buffer{}
	if err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts()); err != nil {
		t.Fatalf("initial setup failed: %v", err)
	}

	db := openTestDBInternal(t, homeDir)
	if _, err := db.Exec(`
DROP TRIGGER IF EXISTS chat_item_fts_after_insert;
DROP TRIGGER IF EXISTS chat_item_fts_after_update;
DROP TRIGGER IF EXISTS chat_item_fts_after_delete;
DROP TABLE chat_item_fts;
CREATE VIRTUAL TABLE chat_item_fts USING fts5(
    session_id UNINDEXED,
    ordinal UNINDEXED,
    search_text
);
`); err != nil {
		t.Fatalf("replace chat_item_fts with old shape: %v", err)
	}
	_ = db.Close()

	stdout.Reset()
	err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected setup to fail when chat_item_fts has the old standalone shape, got nil")
	}
	if !strings.Contains(err.Error(), "chat_item_fts") {
		t.Fatalf("expected error to name chat_item_fts, got: %v", err)
	}
	if !strings.Contains(err.Error(), "predates") {
		t.Fatalf("expected error to mention that the store predates the current schema, got: %v", err)
	}
	expectedBase := basePath(homeDir)
	if !strings.Contains(err.Error(), expectedBase) {
		t.Fatalf("expected error to name the actual store path %q (honoring PC_HOME), got: %v", expectedBase, err)
	}
	if strings.Contains(stdout.String(), "Personal Context initialized at") {
		t.Fatalf("setup printed success message despite schema mismatch: %q", stdout.String())
	}
}

func TestOpenLocalStackFailsLoudlyAgainstStandaloneChatFTSStore(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	if err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts()); err != nil {
		t.Fatalf("initial setup failed: %v", err)
	}

	db := openTestDBInternal(t, homeDir)
	if _, err := db.Exec(`
DROP TRIGGER IF EXISTS chat_item_fts_after_insert;
DROP TRIGGER IF EXISTS chat_item_fts_after_update;
DROP TRIGGER IF EXISTS chat_item_fts_after_delete;
DROP TABLE chat_item_fts;
CREATE VIRTUAL TABLE chat_item_fts USING fts5(
    session_id UNINDEXED,
    ordinal UNINDEXED,
    search_text
);
`); err != nil {
		t.Fatalf("replace chat_item_fts with old shape: %v", err)
	}
	_ = db.Close()

	stack, err := openLocalStack(homeDir)
	if err == nil {
		_ = stack.Close()
		t.Fatal("expected openLocalStack to fail when chat_item_fts has the old standalone shape")
	}
	if !strings.Contains(err.Error(), "chat_item_fts") {
		t.Fatalf("expected error to name chat_item_fts, got: %v", err)
	}
	if !strings.Contains(err.Error(), "predates") {
		t.Fatalf("expected error to mention that the store predates the current schema, got: %v", err)
	}
}

func TestOpenLocalStackFailsLoudlyWhenChatFTSTableMissing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	if err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts()); err != nil {
		t.Fatalf("initial setup failed: %v", err)
	}

	db := openTestDBInternal(t, homeDir)
	if _, err := db.Exec(`
DROP TRIGGER IF EXISTS chat_item_fts_after_insert;
DROP TRIGGER IF EXISTS chat_item_fts_after_update;
DROP TRIGGER IF EXISTS chat_item_fts_after_delete;
DROP TABLE chat_item_fts;
`); err != nil {
		t.Fatalf("drop chat_item_fts table: %v", err)
	}
	_ = db.Close()

	stack, err := openLocalStack(homeDir)
	if err == nil {
		_ = stack.Close()
		t.Fatal("expected openLocalStack to fail when chat_item_fts is missing")
	}
	if !strings.Contains(err.Error(), "chat_item_fts") {
		t.Fatalf("expected error to name chat_item_fts, got: %v", err)
	}
	if !strings.Contains(err.Error(), "predates") {
		t.Fatalf("expected error to mention stale schema, got: %v", err)
	}
}

func TestVerifyChatItemFTSShapeReportsTableQueryError(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	err = verifyChatItemFTSShape(context.Background(), db, t.TempDir())
	if err == nil {
		t.Fatal("expected closed database query to fail")
	}
	if !strings.Contains(err.Error(), `verify schema table "chat_item_fts"`) {
		t.Fatalf("expected table verification context, got %v", err)
	}
}

func TestVerifyCanonicalSchemaTablesReportsTableQueryError(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	err = verifyCanonicalSchemaTables(context.Background(), db, t.TempDir())
	if err == nil {
		t.Fatal("expected closed database query to fail")
	}
	if !strings.Contains(err.Error(), `verify schema table`) {
		t.Fatalf("expected schema table verification context, got %v", err)
	}
}

func TestOpenLocalStackFailsLoudlyWhenChatFTSTriggerMissing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	if err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts()); err != nil {
		t.Fatalf("initial setup failed: %v", err)
	}

	db := openTestDBInternal(t, homeDir)
	if _, err := db.Exec(`DROP TRIGGER chat_item_fts_after_insert;`); err != nil {
		t.Fatalf("drop chat_item_fts_after_insert: %v", err)
	}
	_ = db.Close()

	stack, err := openLocalStack(homeDir)
	if err == nil {
		_ = stack.Close()
		t.Fatal("expected openLocalStack to fail when chat_item_fts trigger is missing")
	}
	if !strings.Contains(err.Error(), "chat_item_fts_after_insert") {
		t.Fatalf("expected error to name missing trigger, got: %v", err)
	}
	if !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("expected error to mention interrupted schema state, got: %v", err)
	}
}

func TestRunSetupConfigStoreWriteCoversPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Do a full setup — this covers the config creation path
	stdout := &bytes.Buffer{}
	if err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts()); err != nil {
		t.Fatalf("runSetup() error = %v", err)
	}

	// Verify config was written
	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config.json not created: %v", err)
	}

	// Run again — config exists, should skip write
	stdout.Reset()
	if err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts()); err != nil {
		t.Fatalf("re-runSetup() error = %v", err)
	}
}

func TestOpenLocalStackEmptyHomeDir(t *testing.T) {
	_, err := openLocalStack("")
	if err == nil {
		t.Fatal("expected error for empty homeDir")
	}
}

// openTestDBInternal opens a raw SQLite connection for test verification.
func openTestDBInternal(t *testing.T, homeDir string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	return db
}
