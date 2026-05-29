package repositorytest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

// RepositoryFactory creates a fresh repository per test case.
type RepositoryFactory func(t *testing.T) repository.Repository

// RunContractSuite executes backend-agnostic repository contract tests.
// Args: t is the parent testing context; factory returns a fresh repository.
// Returns: none. The test fails on contract violations.
func RunContractSuite(t *testing.T, factory RepositoryFactory) {
	t.Helper()

	t.Run("records CRUD and sort order", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		recordA := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250304-a3f2b7e1",
			Date:        "2025-03-04",
			DayOrder:    "b",
			HTMLContent: strPtr("<h1>A</h1>"),
		})

		notes := "updated notes"
		projectID := "org/project"
		sourceDeviceID := "contract-device"
		if _, err := repo.CreateProject(ctx, repository.CreateRegistryInput{ID: projectID}); err != nil && !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("CreateProject(update target) error = %v", err)
		}
		updated, err := repo.UpdateRecord(ctx, repository.UpdateRecordInput{
			ID:             recordA.ID,
			Date:           "2025-03-04",
			DayOrder:       "c",
			HTMLContent:    strPtr("<h1>A2</h1>"),
			Notes:          &notes,
			ProjectID:      projectID,
			SourceDeviceID: sourceDeviceID,
		})
		if err != nil {
			t.Fatalf("UpdateRecord() error = %v", err)
		}
		if updated.DayOrder != "c" || updated.HTMLContent == nil || *updated.HTMLContent != "<h1>A2</h1>" {
			t.Fatalf("unexpected updated record DayOrder/HTMLContent: %+v", updated)
		}
		if updated.Notes == nil || *updated.Notes != "updated notes" {
			t.Fatalf("expected Notes=%q after update, got %v", "updated notes", updated.Notes)
		}
		if updated.ProjectID != "org/project" {
			t.Fatalf("expected ProjectID=%q after update, got %v", "org/project", updated.ProjectID)
		}
		if updated.UpdatedAt.IsZero() {
			t.Fatal("expected non-zero UpdatedAt after update")
		}
		if updated.CreatedAt != recordA.CreatedAt {
			t.Fatalf("expected CreatedAt preserved after update: got %v, want %v", updated.CreatedAt, recordA.CreatedAt)
		}

		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250304-b7e1c9d3",
			Date:        "2025-03-04",
			DayOrder:    "a",
			HTMLContent: strPtr("<h1>B</h1>"),
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250303-c0ffee01",
			Date:        "2025-03-03",
			DayOrder:    "z",
			HTMLContent: strPtr("<h1>C</h1>"),
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250304-a3f2b700",
			Date:        "2025-03-04",
			DayOrder:    "c",
			HTMLContent: strPtr("<h1>D</h1>"),
		})

		records, err := repo.ListRecords(ctx, repository.ListRecordsFilter{})
		if err != nil {
			t.Fatalf("ListRecords() error = %v", err)
		}
		ids := recordIDs(records)
		expected := []string{
			"20250303-c0ffee01",
			"20250304-b7e1c9d3",
			"20250304-a3f2b700",
			"20250304-a3f2b7e1",
		}
		assertExactOrder(t, ids, expected)
	})

	t.Run("records soft delete and restore", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		record := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250305-deadbeef",
			Date:        "2025-03-05",
			DayOrder:    "n",
			HTMLContent: strPtr("<h1>Trash me</h1>"),
		})

		if err := repo.SoftDeleteRecord(ctx, record.ID); err != nil {
			t.Fatalf("SoftDeleteRecord() error = %v", err)
		}
		active, err := repo.ListRecords(ctx, repository.ListRecordsFilter{})
		if err != nil {
			t.Fatalf("ListRecords(active) error = %v", err)
		}
		if len(active) != 0 {
			t.Fatalf("expected active list to be empty, got %d rows", len(active))
		}

		deleted, err := repo.ListRecords(ctx, repository.ListRecordsFilter{IncludeDeleted: true})
		if err != nil {
			t.Fatalf("ListRecords(includeDeleted) error = %v", err)
		}
		if len(deleted) != 1 || deleted[0].DeletedAt == nil {
			t.Fatalf("expected one deleted record with deleted_at, got %+v", deleted)
		}

		if err := repo.RestoreRecord(ctx, record.ID); err != nil {
			t.Fatalf("RestoreRecord() error = %v", err)
		}
		restored, err := repo.ListRecords(ctx, repository.ListRecordsFilter{})
		if err != nil {
			t.Fatalf("ListRecords(after restore) error = %v", err)
		}
		if len(restored) != 1 || restored[0].DeletedAt != nil {
			t.Fatalf("expected restored active record, got %+v", restored)
		}
	})

	t.Run("record figures and data files unique constraints", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		record := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250306-1111aaaa",
			Date:        "2025-03-06",
			DayOrder:    "n",
			HTMLContent: strPtr("<h1>Assets</h1>"),
		})

		figure, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
			RecordID: record.ID,
			Filename: "plot.png",
			S3Key:    "figures/20250306-1111aaaa/plot.png",
		})
		if err != nil {
			t.Fatalf("CreateRecordFigure() error = %v", err)
		}
		_, err = repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
			RecordID: record.ID,
			Filename: "plot.png",
			S3Key:    "figures/20250306-1111aaaa/plot.png",
		})
		if !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("expected ErrConflict for duplicate figure filename, got %v", err)
		}

		dataFile, err := repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
			RecordID: record.ID,
			Filename: "metrics.csv",
			S3Key:    "data/20250306-1111aaaa/metrics.csv",
			Size:     12,
			Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		})
		if err != nil {
			t.Fatalf("CreateRecordDataFile() error = %v", err)
		}
		_, err = repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
			RecordID: record.ID,
			Filename: "metrics.csv",
			S3Key:    "data/20250306-1111aaaa/metrics.csv",
			Size:     12,
			Hash:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		})
		if !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("expected ErrConflict for duplicate data filename, got %v", err)
		}

		if err := repo.DeleteRecordFigure(ctx, figure.ID); err != nil {
			t.Fatalf("DeleteRecordFigure() error = %v", err)
		}
		if err := repo.DeleteRecordDataFile(ctx, dataFile.ID); err != nil {
			t.Fatalf("DeleteRecordDataFile() error = %v", err)
		}
	})

	t.Run("project paths and chats", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()
		now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

		if _, err := repo.CreateProject(ctx, repository.CreateRegistryInput{ID: "chat/project", CreatedAt: &now, UpdatedAt: &now}); err != nil && !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("CreateProject() error = %v", err)
		}
		if _, err := repo.CreateProject(ctx, repository.CreateRegistryInput{ID: "chat/project-child", CreatedAt: &now, UpdatedAt: &now}); err != nil && !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("CreateProject(child) error = %v", err)
		}
		if _, err := repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: "chat-device", CreatedAt: &now, UpdatedAt: &now}); err != nil && !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("CreateDevice() error = %v", err)
		}
		path, created, err := repo.UpsertProjectPath(ctx, repository.CreateProjectPathInput{ProjectID: "chat/project", Path: "/tmp/chat-project", DeviceID: "chat-device", CreatedAt: &now, UpdatedAt: &now})
		if err != nil {
			t.Fatalf("UpsertProjectPath() error = %v", err)
		}
		if !created || path.ProjectID != "chat/project" || path.Path != "/tmp/chat-project" {
			t.Fatalf("unexpected project path: created=%v path=%+v", created, path)
		}
		_, createdAgain, err := repo.UpsertProjectPath(ctx, repository.CreateProjectPathInput{ProjectID: "chat/project", Path: "/tmp/chat-project", DeviceID: "chat-device"})
		if err != nil {
			t.Fatalf("UpsertProjectPath(again) error = %v", err)
		}
		if createdAgain {
			t.Fatal("expected duplicate project path to report created=false")
		}
		if _, _, err := repo.UpsertProjectPath(ctx, repository.CreateProjectPathInput{ProjectID: "chat/project-child", Path: "/tmp/chat-project/nested", DeviceID: "chat-device", CreatedAt: &now, UpdatedAt: &now}); err != nil {
			t.Fatalf("UpsertProjectPath(child) error = %v", err)
		}

		cwd := "/tmp/chat-project/nested/deeper"
		session, created, err := repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              "20260514-abcdef12",
				Source:          "codex",
				SourceSessionID: "source-1",
				SourceDeviceID:  "chat-device",
				CWD:             &cwd,
				StartedAt:       now,
				LastActivityAt:  now,
				CreatedAt:       &now,
				UpdatedAt:       &now,
			},
			ClearDeleted: true,
		})
		if err != nil {
			t.Fatalf("UpsertChatSession() error = %v", err)
		}
		if !created || session.ID != "20260514-abcdef12" {
			t.Fatalf("unexpected chat session: created=%v session=%+v", created, session)
		}
		if backfilled, err := repo.BackfillChatProjects(ctx); err != nil {
			t.Fatalf("BackfillChatProjects() error = %v", err)
		} else if backfilled != 1 {
			t.Fatalf("expected one chat backfill, got %d", backfilled)
		}
		session, err = repo.GetChatSessionByID(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetChatSessionByID() error = %v", err)
		}
		if session.ProjectID == nil || *session.ProjectID != "chat/project-child" {
			t.Fatalf("expected chat project backfilled, got %+v", session.ProjectID)
		}
		itemText := "needle chat transcript text"
		if _, err := repo.CreateChatItem(ctx, repository.CreateChatItemInput{SessionID: session.ID, Ordinal: 0, Role: "user", ItemType: "message", Text: &itemText, SearchText: itemText, CreatedAt: &now}); err != nil {
			t.Fatalf("CreateChatItem() error = %v", err)
		}
		replacementText := "needle replacement chat transcript text"
		if err := repo.ReplaceChatItems(ctx, session.ID, []repository.CreateChatItemInput{{SessionID: session.ID, Ordinal: 0, Role: "assistant", ItemType: "message", Text: &replacementText, SearchText: replacementText, CreatedAt: &now}}); err != nil {
			t.Fatalf("ReplaceChatItems() error = %v", err)
		}
		items, err := repo.ListChatItems(ctx, session.ID)
		if err != nil {
			t.Fatalf("ListChatItems() error = %v", err)
		}
		if len(items) != 1 || items[0].SearchText != replacementText {
			t.Fatalf("unexpected replaced chat items: %+v", items)
		}
		appendedText := "contract appended chat transcript text"
		if err := repo.AppendChatItems(ctx, session.ID, []repository.CreateChatItemInput{{SessionID: session.ID, Ordinal: 1, Role: "user", ItemType: "message", Text: &appendedText, SearchText: appendedText, CreatedAt: &now}}); err != nil {
			t.Fatalf("AppendChatItems() error = %v", err)
		}
		items, err = repo.ListChatItems(ctx, session.ID)
		if err != nil {
			t.Fatalf("ListChatItems(appended) error = %v", err)
		}
		if len(items) != 2 || items[1].SearchText != appendedText {
			t.Fatalf("unexpected appended chat items: %+v", items)
		}
		results, err := repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "needle"})
		if err != nil {
			t.Fatalf("SearchChatItems() error = %v", err)
		}
		if len(results) != 1 || results[0].Session.ID != session.ID || results[0].Item.Ordinal != 0 {
			t.Fatalf("unexpected chat search results: %+v", results)
		}
		if _, err := repo.CreateRecord(ctx, repository.CreateRecordInput{
			ID:             session.ID,
			Date:           "2026-05-14",
			DayOrder:       "z",
			HTMLContent:    strPtr("<p>collision</p>"),
			ProjectID:      "chat/project",
			SourceDeviceID: "chat-device",
		}); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("expected ErrConflict for record id colliding with chat session, got %v", err)
		}
		record := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:             "20260514-fedcba98",
			Date:           "2026-05-14",
			DayOrder:       "y",
			HTMLContent:    strPtr("<p>record before chat collision</p>"),
			ProjectID:      "chat/project",
			SourceDeviceID: "chat-device",
		})
		if _, _, err := repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              record.ID,
				Source:          "codex",
				SourceSessionID: "source-collision",
				SourceDeviceID:  "chat-device",
				StartedAt:       now,
				LastActivityAt:  now,
			},
			ClearDeleted: true,
		}); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("expected ErrConflict for chat id colliding with record, got %v", err)
		}
		if err := repo.SoftDeleteChatSession(ctx, session.ID); err != nil {
			t.Fatalf("SoftDeleteChatSession() error = %v", err)
		}
		deleted, err := repo.GetChatSessionByID(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetChatSessionByID(deleted) error = %v", err)
		}
		if deleted.DeletedAt == nil {
			t.Fatal("expected deleted_at after chat soft delete")
		}
		if err := repo.RestoreChatSession(ctx, session.ID); err != nil {
			t.Fatalf("RestoreChatSession() error = %v", err)
		}
		restored, err := repo.GetChatSessionByID(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetChatSessionByID(restored) error = %v", err)
		}
		if restored.DeletedAt != nil {
			t.Fatalf("expected deleted_at to be cleared after restore, got %v", restored.DeletedAt)
		}
		if err := repo.RestoreChatSession(ctx, "20250101-nonexist"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for missing chat restore, got %v", err)
		}
	})

	t.Run("chat parent metadata, filters, and item counts", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()
		now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

		if _, err := repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: "parent-device", CreatedAt: &now, UpdatedAt: &now}); err != nil && !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("CreateDevice() error = %v", err)
		}

		// Parent transcript: no parent_source_session_id.
		parent, _, err := repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID: "20260528-aaaa0001", Source: "claude_code", SourceSessionID: "parent-sid",
				SourceDeviceID: "parent-device", StartedAt: now, LastActivityAt: now, CreatedAt: &now, UpdatedAt: &now,
			},
			ClearDeleted: true,
		})
		if err != nil {
			t.Fatalf("UpsertChatSession(parent) error = %v", err)
		}
		if parent.ParentSourceSessionID != nil {
			t.Fatalf("expected nil ParentSourceSessionID for parent, got %v", parent.ParentSourceSessionID)
		}

		// Two subagent transcripts referencing the parent source session id.
		parentSID := "parent-sid"
		for _, sub := range []struct {
			id, sourceSessionID, text string
		}{
			{"20260528-bbbb0001", "parent-sid:agent-aaa", "alpha subagent needle"},
			{"20260528-bbbb0002", "parent-sid:agent-bbb", "beta subagent needle"},
		} {
			child, created, err := repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
				CreateChatSessionInput: repository.CreateChatSessionInput{
					ID: sub.id, Source: "claude_code", SourceSessionID: sub.sourceSessionID,
					ParentSourceSessionID: &parentSID,
					SourceDeviceID:        "parent-device", StartedAt: now, LastActivityAt: now, CreatedAt: &now, UpdatedAt: &now,
				},
				ClearDeleted: true,
			})
			if err != nil {
				t.Fatalf("UpsertChatSession(%s) error = %v", sub.id, err)
			}
			if !created {
				t.Fatalf("expected subagent %s created", sub.id)
			}
			if child.ParentSourceSessionID == nil || *child.ParentSourceSessionID != parentSID {
				t.Fatalf("expected ParentSourceSessionID=%q for %s, got %v", parentSID, sub.id, child.ParentSourceSessionID)
			}
			text := sub.text
			if err := repo.ReplaceChatItems(ctx, sub.id, []repository.CreateChatItemInput{
				{SessionID: sub.id, Ordinal: 0, Role: "assistant", ItemType: "message", Text: &text, SearchText: text, CreatedAt: &now},
			}); err != nil {
				t.Fatalf("ReplaceChatItems(%s) error = %v", sub.id, err)
			}
		}

		// Persisted parent metadata survives a re-fetch.
		refetched, err := repo.GetChatSessionByID(ctx, "20260528-bbbb0001")
		if err != nil {
			t.Fatalf("GetChatSessionByID() error = %v", err)
		}
		if refetched.ParentSourceSessionID == nil || *refetched.ParentSourceSessionID != parentSID {
			t.Fatalf("expected persisted ParentSourceSessionID=%q, got %v", parentSID, refetched.ParentSourceSessionID)
		}

		// ListChatSessions parent filter returns exactly the two subagents.
		children, err := repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{ParentSourceSessionID: &parentSID})
		if err != nil {
			t.Fatalf("ListChatSessions(parent filter) error = %v", err)
		}
		if len(children) != 2 {
			t.Fatalf("expected 2 subagents for parent filter, got %d (%+v)", len(children), children)
		}
		for _, c := range children {
			if c.ParentSourceSessionID == nil || *c.ParentSourceSessionID != parentSID {
				t.Fatalf("ListChatSessions returned non-child %+v", c)
			}
		}

		// SearchChatItems parent filter restricts hits to the parent's subagents.
		hits, err := repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "needle", ParentSourceSessionID: &parentSID})
		if err != nil {
			t.Fatalf("SearchChatItems(parent filter) error = %v", err)
		}
		if len(hits) != 2 {
			t.Fatalf("expected 2 subagent search hits, got %d (%+v)", len(hits), hits)
		}
		for _, h := range hits {
			if h.Session.ParentSourceSessionID == nil || *h.Session.ParentSourceSessionID != parentSID {
				t.Fatalf("SearchChatItems returned non-child session %+v", h.Session)
			}
		}

		// CountChatItems is the authoritative item count: two subagent items so far.
		count, err := repo.CountChatItems(ctx, repository.CountChatItemsFilter{})
		if err != nil {
			t.Fatalf("CountChatItems() error = %v", err)
		}
		if count != 2 {
			t.Fatalf("expected CountChatItems=2, got %d", count)
		}

		// Soft-deleting one subagent removes its item from the default count but
		// keeps it when IncludeDeleted is set.
		if err := repo.SoftDeleteChatSession(ctx, "20260528-bbbb0001"); err != nil {
			t.Fatalf("SoftDeleteChatSession() error = %v", err)
		}
		visible, err := repo.CountChatItems(ctx, repository.CountChatItemsFilter{})
		if err != nil {
			t.Fatalf("CountChatItems(visible) error = %v", err)
		}
		if visible != 1 {
			t.Fatalf("expected CountChatItems=1 after soft delete, got %d", visible)
		}
		all, err := repo.CountChatItems(ctx, repository.CountChatItemsFilter{IncludeDeleted: true})
		if err != nil {
			t.Fatalf("CountChatItems(IncludeDeleted) error = %v", err)
		}
		if all != 2 {
			t.Fatalf("expected CountChatItems(IncludeDeleted)=2, got %d", all)
		}
	})

	t.Run("figure and data-file get/list/update", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		record := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250306-2222bbbb",
			Date:        "2025-03-06",
			DayOrder:    "n",
			HTMLContent: strPtr("<h1>Asset updates</h1>"),
		})

		figure, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
			RecordID: record.ID,
			Filename: "before.png",
			S3Key:    "figures/20250306-2222bbbb/before.png",
			AltText:  strPtr("before"),
		})
		if err != nil {
			t.Fatalf("CreateRecordFigure() error = %v", err)
		}

		figures, err := repo.ListRecordFiguresByRecordID(ctx, record.ID)
		if err != nil {
			t.Fatalf("ListRecordFiguresByRecordID() error = %v", err)
		}
		if len(figures) != 1 || figures[0].ID != figure.ID {
			t.Fatalf("unexpected figures list: %+v", figures)
		}

		updatedFigure, err := repo.UpdateRecordFigure(ctx, repository.UpdateRecordFigureInput{
			ID:       figure.ID,
			Filename: "after.png",
			S3Key:    "figures/20250306-2222bbbb/after.png",
			AltText:  strPtr("after"),
		})
		if err != nil {
			t.Fatalf("UpdateRecordFigure() error = %v", err)
		}
		if updatedFigure.Filename != "after.png" || updatedFigure.S3Key != "figures/20250306-2222bbbb/after.png" {
			t.Fatalf("unexpected updated figure Filename/S3Key: %+v", updatedFigure)
		}
		if updatedFigure.AltText == nil || *updatedFigure.AltText != "after" {
			t.Fatalf("expected AltText=%q after figure update, got %v", "after", updatedFigure.AltText)
		}

		// Verify nil AltText preserves existing value (patch semantics).
		patchedFigure, err := repo.UpdateRecordFigure(ctx, repository.UpdateRecordFigureInput{
			ID:      figure.ID,
			AltText: nil,
		})
		if err != nil {
			t.Fatalf("UpdateRecordFigure(nil AltText) error = %v", err)
		}
		if patchedFigure.AltText == nil || *patchedFigure.AltText != "after" {
			t.Fatalf("expected AltText preserved as %q when nil, got %v", "after", patchedFigure.AltText)
		}

		dataFile, err := repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
			RecordID:    record.ID,
			Filename:    "before.csv",
			S3Key:       "data/20250306-2222bbbb/before.csv",
			Size:        7,
			Hash:        "abababababababababababababababababababababababababababababababab",
			Description: strPtr("before"),
		})
		if err != nil {
			t.Fatalf("CreateRecordDataFile() error = %v", err)
		}

		files, err := repo.ListRecordDataFilesByRecordID(ctx, record.ID)
		if err != nil {
			t.Fatalf("ListRecordDataFilesByRecordID() error = %v", err)
		}
		if len(files) != 1 || files[0].ID != dataFile.ID {
			t.Fatalf("unexpected data-file list: %+v", files)
		}

		updatedDataFile, err := repo.UpdateRecordDataFile(ctx, repository.UpdateRecordDataFileInput{
			ID:          dataFile.ID,
			Filename:    "after.csv",
			S3Key:       "data/20250306-2222bbbb/after.csv",
			Size:        int64Ptr(11),
			Hash:        strPtr("cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"),
			Description: strPtr("after"),
		})
		if err != nil {
			t.Fatalf("UpdateRecordDataFile() error = %v", err)
		}
		if updatedDataFile.Filename != "after.csv" ||
			updatedDataFile.S3Key != "data/20250306-2222bbbb/after.csv" ||
			updatedDataFile.Size != 11 ||
			updatedDataFile.Hash != "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd" {
			t.Fatalf("unexpected updated data file Filename/S3Key/Size/Hash: %+v", updatedDataFile)
		}
		if updatedDataFile.Description == nil || *updatedDataFile.Description != "after" {
			t.Fatalf("expected Description=%q after data file update, got %v", "after", updatedDataFile.Description)
		}

		// Verify nil Description preserves existing value (patch semantics).
		patchedDataFile, err := repo.UpdateRecordDataFile(ctx, repository.UpdateRecordDataFileInput{
			ID:          dataFile.ID,
			Description: nil,
		})
		if err != nil {
			t.Fatalf("UpdateRecordDataFile(nil Description) error = %v", err)
		}
		if patchedDataFile.Description == nil || *patchedDataFile.Description != "after" {
			t.Fatalf("expected Description preserved as %q when nil, got %v", "after", patchedDataFile.Description)
		}
	})

	t.Run("list filters and invalid arguments", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250301-aa11aa11",
			Date:        "2025-03-01",
			DayOrder:    "a",
			HTMLContent: strPtr("<h1>1</h1>"),
			ProjectID:   "org/p1",
		})
		second := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250302-bb22bb22",
			Date:        "2025-03-02",
			DayOrder:    "a",
			HTMLContent: strPtr("<h1>2</h1>"),
			ProjectID:   "org/p2",
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250303-cc33cc33",
			Date:        "2025-03-03",
			DayOrder:    "a",
			HTMLContent: strPtr("<h1>3</h1>"),
			ProjectID:   "org/p2",
		})

		if err := repo.SoftDeleteRecord(ctx, second.ID); err != nil {
			t.Fatalf("SoftDeleteRecord() error = %v", err)
		}

		projectID := "org/p2"
		dateFrom := "2025-03-01"
		dateTo := "2025-03-03"
		filtered, err := repo.ListRecords(ctx, repository.ListRecordsFilter{
			IncludeDeleted: false,
			ProjectID:      &projectID,
			DateFrom:       &dateFrom,
			DateTo:         &dateTo,
			Limit:          1,
		})
		if err != nil {
			t.Fatalf("ListRecords(filter) error = %v", err)
		}
		if len(filtered) != 1 || filtered[0].ID != "20250303-cc33cc33" {
			t.Fatalf("unexpected filtered result: %+v", filtered)
		}

		if _, err := repo.GetRecordByID(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty record id, got %v", err)
		}
		if err := repo.DeleteRecord(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty delete id, got %v", err)
		}
		if err := repo.RestoreRecord(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty restore id, got %v", err)
		}
		if _, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid figure input, got %v", err)
		}
		if _, err := repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid data-file input, got %v", err)
		}
		if _, err := repo.CreateTemplate(ctx, repository.CreateTemplateInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid template input, got %v", err)
		}
		if _, err := repo.UpdateTemplate(ctx, repository.UpdateTemplateInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid template update, got %v", err)
		}
		if _, err := repo.GetTemplateByName(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty template name, got %v", err)
		}
		if err := repo.DeleteTemplate(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty template delete, got %v", err)
		}
		if _, err := repo.GetRecordFigureByID(ctx, 0); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid figure id, got %v", err)
		}
		if _, err := repo.GetRecordDataFileByID(ctx, 0); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid data-file id, got %v", err)
		}
		if _, err := repo.UpdateRecordFigure(ctx, repository.UpdateRecordFigureInput{ID: 0}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid figure update id, got %v", err)
		}
		if _, err := repo.UpdateRecordDataFile(ctx, repository.UpdateRecordDataFileInput{ID: 0}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid data-file update id, got %v", err)
		}
		if err := repo.DeleteRecordFigure(ctx, 0); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid figure delete id, got %v", err)
		}
		if err := repo.DeleteRecordDataFile(ctx, 0); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid data-file delete id, got %v", err)
		}
		if err := repo.SoftDeleteRecord(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty soft-delete id, got %v", err)
		}

		if err := repo.DeleteRecord(ctx, "20259999-missing00"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for missing record delete, got %v", err)
		}
	})

	t.Run("updated_at window filters are inclusive", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		firstUpdatedAt := time.Date(2025, 3, 4, 10, 0, 0, 0, time.UTC)
		middleUpdatedAt := time.Date(2025, 3, 4, 11, 0, 0, 0, time.UTC)
		lastUpdatedAt := time.Date(2025, 3, 4, 12, 0, 0, 0, time.UTC)

		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250304-a1b2c3d4",
			Date:        "2025-03-04",
			DayOrder:    "a",
			HTMLContent: strPtr("<h1>First</h1>"),
			UpdatedAt:   &firstUpdatedAt,
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250304-b2c3d4e5",
			Date:        "2025-03-04",
			DayOrder:    "b",
			HTMLContent: strPtr("<h1>Middle</h1>"),
			UpdatedAt:   &middleUpdatedAt,
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250304-c3d4e5f6",
			Date:        "2025-03-04",
			DayOrder:    "c",
			HTMLContent: strPtr("<h1>Last</h1>"),
			UpdatedAt:   &lastUpdatedAt,
		})

		afterResults, err := repo.ListRecords(ctx, repository.ListRecordsFilter{
			UpdatedAfter: &middleUpdatedAt,
		})
		if err != nil {
			t.Fatalf("ListRecords(UpdatedAfter) error = %v", err)
		}
		assertExactOrder(t, recordIDs(afterResults), []string{
			"20250304-b2c3d4e5",
			"20250304-c3d4e5f6",
		})

		beforeResults, err := repo.ListRecords(ctx, repository.ListRecordsFilter{
			UpdatedBefore: &middleUpdatedAt,
		})
		if err != nil {
			t.Fatalf("ListRecords(UpdatedBefore) error = %v", err)
		}
		assertExactOrder(t, recordIDs(beforeResults), []string{
			"20250304-a1b2c3d4",
			"20250304-b2c3d4e5",
		})

		windowResults, err := repo.ListRecords(ctx, repository.ListRecordsFilter{
			UpdatedAfter:  &middleUpdatedAt,
			UpdatedBefore: &middleUpdatedAt,
		})
		if err != nil {
			t.Fatalf("ListRecords(UpdatedAfter+UpdatedBefore) error = %v", err)
		}
		assertExactOrder(t, recordIDs(windowResults), []string{
			"20250304-b2c3d4e5",
		})
	})

	t.Run("templates CRUD", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		template, err := repo.CreateTemplate(ctx, repository.CreateTemplateInput{
			Name:        "text-only",
			HTMLContent: "<main></main>",
		})
		if err != nil {
			t.Fatalf("CreateTemplate() error = %v", err)
		}

		updated, err := repo.UpdateTemplate(ctx, repository.UpdateTemplateInput{
			Name:        template.Name,
			HTMLContent: "<section></section>",
		})
		if err != nil {
			t.Fatalf("UpdateTemplate() error = %v", err)
		}
		if updated.HTMLContent != "<section></section>" {
			t.Fatalf("unexpected updated template: %+v", updated)
		}

		listed, err := repo.ListTemplates(ctx)
		if err != nil {
			t.Fatalf("ListTemplates() error = %v", err)
		}
		if len(listed) != 1 {
			t.Fatalf("expected one template, got %d", len(listed))
		}

		if err := repo.DeleteTemplate(ctx, template.Name); err != nil {
			t.Fatalf("DeleteTemplate() error = %v", err)
		}
		_, err = repo.GetTemplateByName(ctx, template.Name)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected ErrNotFound after template delete, got %v", err)
		}
	})

	t.Run("foreign key rejection and cascading delete", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		_, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
			RecordID: "20250309-missing0",
			Filename: "orphan.png",
			S3Key:    "figures/20250309-missing0/orphan.png",
		})
		if !errors.Is(err, repository.ErrForeignKeyViolation) {
			t.Fatalf("expected ErrForeignKeyViolation for orphan figure, got %v", err)
		}

		record := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250309-ca5cad01",
			Date:        "2025-03-09",
			DayOrder:    "n",
			HTMLContent: strPtr("<h1>Cascade</h1>"),
		})
		figure, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
			RecordID: record.ID,
			Filename: "f.png",
			S3Key:    "figures/20250309-ca5cad01/f.png",
		})
		if err != nil {
			t.Fatalf("CreateRecordFigure() error = %v", err)
		}
		dataFile, err := repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
			RecordID: record.ID,
			Filename: "d.csv",
			S3Key:    "data/20250309-ca5cad01/d.csv",
			Size:     4,
			Hash:     "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		})
		if err != nil {
			t.Fatalf("CreateRecordDataFile() error = %v", err)
		}

		if err := repo.DeleteRecord(ctx, record.ID); err != nil {
			t.Fatalf("DeleteRecord() error = %v", err)
		}
		_, err = repo.GetRecordByID(ctx, record.ID)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for deleted record, got %v", err)
		}
		_, err = repo.GetRecordFigureByID(ctx, figure.ID)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for cascaded figure, got %v", err)
		}
		_, err = repo.GetRecordDataFileByID(ctx, dataFile.ID)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for cascaded data file, got %v", err)
		}
	})

	t.Run("sync version changes", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		before, err := repo.GetSyncVersion(ctx)
		if err != nil {
			t.Fatalf("GetSyncVersion(before) error = %v", err)
		}

		_, err = repo.CreateTemplate(ctx, repository.CreateTemplateInput{
			Name:        "sync-version",
			HTMLContent: "<main>sync</main>",
		})
		if err != nil {
			t.Fatalf("CreateTemplate() error = %v", err)
		}

		after, err := repo.GetSyncVersion(ctx)
		if err != nil {
			t.Fatalf("GetSyncVersion(after) error = %v", err)
		}
		if after.Version <= before.Version {
			t.Fatalf("expected sync version to increase, before=%d after=%d", before.Version, after.Version)
		}
	})

	t.Run("OnlyDeleted filter", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250401-aa010101",
			Date:        "2025-04-01",
			DayOrder:    "a",
			HTMLContent: strPtr("<h1>Active 1</h1>"),
		})
		deletedRecord := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250401-bb020202",
			Date:        "2025-04-01",
			DayOrder:    "b",
			HTMLContent: strPtr("<h1>Will be deleted</h1>"),
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250401-cc030303",
			Date:        "2025-04-01",
			DayOrder:    "c",
			HTMLContent: strPtr("<h1>Active 2</h1>"),
		})

		if err := repo.SoftDeleteRecord(ctx, deletedRecord.ID); err != nil {
			t.Fatalf("SoftDeleteRecord() error = %v", err)
		}

		onlyDeleted, err := repo.ListRecords(ctx, repository.ListRecordsFilter{OnlyDeleted: true})
		if err != nil {
			t.Fatalf("ListRecords(OnlyDeleted) error = %v", err)
		}
		if len(onlyDeleted) != 1 {
			t.Fatalf("expected 1 deleted record, got %d", len(onlyDeleted))
		}
		if onlyDeleted[0].ID != deletedRecord.ID {
			t.Fatalf("expected deleted record %s, got %s", deletedRecord.ID, onlyDeleted[0].ID)
		}
		if onlyDeleted[0].DeletedAt == nil {
			t.Fatal("expected deleted_at to be set on OnlyDeleted result")
		}
	})

	t.Run("Query filter search", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250402-a0a0a0a1",
			Date:        "2025-04-02",
			DayOrder:    "a",
			HTMLContent: strPtr("<p>Advances in machine learning</p>"),
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250402-b0b0b0b2",
			Date:        "2025-04-02",
			DayOrder:    "b",
			HTMLContent: strPtr("<p>Unrelated content</p>"),
			Notes:       strPtr("learning about rust"),
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250402-c0c0c0c3",
			Date:        "2025-04-02",
			DayOrder:    "c",
			HTMLContent: strPtr("<p>Some other topic</p>"),
			ProjectID:   "org/learning-project",
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250402-d0d0d0d4",
			Date:        "2025-04-02",
			DayOrder:    "d",
			HTMLContent: strPtr("<p>unrelated content only</p>"),
		})

		query := "learning"
		results, err := repo.ListRecords(ctx, repository.ListRecordsFilter{Query: &query})
		if err != nil {
			t.Fatalf("ListRecords(Query=learning) error = %v", err)
		}
		ids := recordIDs(results)
		expected := []string{
			"20250402-a0a0a0a1",
			"20250402-b0b0b0b2",
		}
		assertExactOrder(t, ids, expected)

		// Case-insensitive search returns the same results.
		upperQuery := "LEARNING"
		upperResults, err := repo.ListRecords(ctx, repository.ListRecordsFilter{Query: &upperQuery})
		if err != nil {
			t.Fatalf("ListRecords(Query=LEARNING) error = %v", err)
		}
		assertExactOrder(t, recordIDs(upperResults), expected)
	})

	t.Run("Query with project filter", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250403-a1a1a1a1",
			Date:        "2025-04-03",
			DayOrder:    "a",
			HTMLContent: strPtr("<p>golang concurrency patterns</p>"),
			ProjectID:   "org/backend",
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250403-b2b2b2b2",
			Date:        "2025-04-03",
			DayOrder:    "b",
			HTMLContent: strPtr("<p>golang generics tutorial</p>"),
			ProjectID:   "org/frontend",
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250403-c3c3c3c3",
			Date:        "2025-04-03",
			DayOrder:    "c",
			HTMLContent: strPtr("<p>python asyncio</p>"),
			ProjectID:   "org/backend",
		})

		query := "golang"
		projectID := "org/backend"
		results, err := repo.ListRecords(ctx, repository.ListRecordsFilter{
			Query:     &query,
			ProjectID: &projectID,
		})
		if err != nil {
			t.Fatalf("ListRecords(Query+ProjectID) error = %v", err)
		}
		if len(results) != 1 || results[0].ID != "20250403-a1a1a1a1" {
			t.Fatalf("expected only backend golang record, got %v", recordIDs(results))
		}
	})

	t.Run("Query with deleted flag", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		record := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250404-de1e1e01",
			Date:        "2025-04-04",
			DayOrder:    "a",
			HTMLContent: strPtr("<p>searchable content</p>"),
		})
		if err := repo.SoftDeleteRecord(ctx, record.ID); err != nil {
			t.Fatalf("SoftDeleteRecord() error = %v", err)
		}

		query := "searchable"

		// Default search excludes deleted records.
		defaultResults, err := repo.ListRecords(ctx, repository.ListRecordsFilter{Query: &query})
		if err != nil {
			t.Fatalf("ListRecords(Query, default) error = %v", err)
		}
		if len(defaultResults) != 0 {
			t.Fatalf("expected no results for deleted record in default search, got %d", len(defaultResults))
		}

		// IncludeDeleted=true includes the deleted record.
		includeResults, err := repo.ListRecords(ctx, repository.ListRecordsFilter{Query: &query, IncludeDeleted: true})
		if err != nil {
			t.Fatalf("ListRecords(Query, IncludeDeleted) error = %v", err)
		}
		if len(includeResults) != 1 || includeResults[0].ID != record.ID {
			t.Fatalf("expected deleted record with IncludeDeleted, got %v", recordIDs(includeResults))
		}
	})

	t.Run("LIKE wildcard escaping in Query", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250405-e5c01aa1",
			Date:        "2025-04-05",
			DayOrder:    "a",
			HTMLContent: strPtr("<p>100% complete</p>"),
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250405-e5c02bb2",
			Date:        "2025-04-05",
			DayOrder:    "b",
			HTMLContent: strPtr("<p>1000 items</p>"),
		})

		// "100%" should only match the record with the literal percent sign,
		// not "1000" (which would match if % were treated as a wildcard).
		query := "100%"
		results, err := repo.ListRecords(ctx, repository.ListRecordsFilter{Query: &query})
		if err != nil {
			t.Fatalf("ListRecords(Query=100%%) error = %v", err)
		}
		if len(results) != 1 || results[0].ID != "20250405-e5c01aa1" {
			t.Fatalf("expected only the record with literal '100%%', got %v", recordIDs(results))
		}

		// Also test underscore escaping: "_" should not match arbitrary single chars.
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250405-e5c03cc3",
			Date:        "2025-04-05",
			DayOrder:    "c",
			HTMLContent: strPtr("<p>item_count is 5</p>"),
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250405-e5c04dd4",
			Date:        "2025-04-05",
			DayOrder:    "d",
			HTMLContent: strPtr("<p>itemXcount is 9</p>"),
		})

		underscoreQuery := "item_count"
		underscoreResults, err := repo.ListRecords(ctx, repository.ListRecordsFilter{Query: &underscoreQuery})
		if err != nil {
			t.Fatalf("ListRecords(Query=item_count) error = %v", err)
		}
		if len(underscoreResults) != 1 || underscoreResults[0].ID != "20250405-e5c03cc3" {
			t.Fatalf("expected only the record with literal 'item_count', got %v", recordIDs(underscoreResults))
		}
	})

	t.Run("LIKE backslash escaping in Query", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250405-e5c05ee5",
			Date:        "2025-04-05",
			DayOrder:    "e",
			HTMLContent: strPtr(`<p>path is C:\Users\docs</p>`),
		})
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20250405-e5c06ff6",
			Date:        "2025-04-05",
			DayOrder:    "f",
			HTMLContent: strPtr("<p>path is C:Usersdocs</p>"),
		})

		// A query containing backslashes should only match the record with
		// literal backslashes, not the one without them.
		bsQuery := `C:\Users`
		bsResults, err := repo.ListRecords(ctx, repository.ListRecordsFilter{Query: &bsQuery})
		if err != nil {
			t.Fatalf("ListRecords(Query with backslash) error = %v", err)
		}
		if len(bsResults) != 1 || bsResults[0].ID != "20250405-e5c05ee5" {
			t.Fatalf("expected only the record with literal backslashes, got %v", recordIDs(bsResults))
		}
	})

	t.Run("Whitespace-only Query rejected", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		query := "   "
		_, err := repo.ListRecords(ctx, repository.ListRecordsFilter{Query: &query})
		if !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for whitespace-only query, got %v", err)
		}
	})

	t.Run("Negative Limit rejected", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		_, err := repo.ListRecords(ctx, repository.ListRecordsFilter{Limit: -1})
		if !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for negative limit, got %v", err)
		}
	})

	t.Run("CountRecords honors filters and ignores limit", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
		alpha := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:             "20260508-a1a1a1a1",
			Date:           "2026-05-08",
			DayOrder:       "a",
			HTMLContent:    strPtr("<p>alpha html</p>"),
			Notes:          strPtr("alpha notes"),
			ProjectID:      "count/alpha",
			SourceDeviceID: "source-alpha-device",
			SourceRef:      strPtr("source-alpha-ref"),
			UpdatedAt:      &now,
		})
		beta := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:             "20260509-b2b2b2b2",
			Date:           "2026-05-09",
			DayOrder:       "b",
			ProjectID:      "count/beta",
			SourceDeviceID: "source-beta-device",
			UpdatedAt:      ptrTime(now.Add(time.Hour)),
		})
		deleted := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20260510-c3c3c3c3",
			Date:        "2026-05-10",
			DayOrder:    "c",
			HTMLContent: strPtr("<p>deleted html</p>"),
			ProjectID:   "count/alpha",
			UpdatedAt:   ptrTime(now.Add(2 * time.Hour)),
		})
		if _, err := repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
			RecordID: alpha.ID,
			Filename: "alpha.json",
			S3Key:    "data/alpha.json",
			Size:     2,
			Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}); err != nil {
			t.Fatalf("CreateRecordDataFile(alpha) error = %v", err)
		}
		if err := repo.SoftDeleteRecord(ctx, deleted.ID); err != nil {
			t.Fatalf("SoftDeleteRecord() error = %v", err)
		}

		count, err := repo.CountRecords(ctx, repository.ListRecordsFilter{})
		if err != nil {
			t.Fatalf("CountRecords(default) error = %v", err)
		}
		if count != 2 {
			t.Fatalf("default count = %d, want 2", count)
		}

		count, err = repo.CountRecords(ctx, repository.ListRecordsFilter{Limit: 1})
		if err != nil {
			t.Fatalf("CountRecords(limit ignored) error = %v", err)
		}
		if count != 2 {
			t.Fatalf("limit-ignored count = %d, want 2", count)
		}

		count, err = repo.CountRecords(ctx, repository.ListRecordsFilter{ProjectID: strPtr("count/alpha")})
		if err != nil {
			t.Fatalf("CountRecords(project) error = %v", err)
		}
		if count != 1 {
			t.Fatalf("project count = %d, want 1", count)
		}

		count, err = repo.CountRecords(ctx, repository.ListRecordsFilter{DateFrom: strPtr("2026-05-09"), DateTo: strPtr("2026-05-09")})
		if err != nil {
			t.Fatalf("CountRecords(date) error = %v", err)
		}
		if count != 1 {
			t.Fatalf("date count = %d, want 1", count)
		}

		updatedAfter := now.Add(30 * time.Minute)
		count, err = repo.CountRecords(ctx, repository.ListRecordsFilter{UpdatedAfter: &updatedAfter})
		if err != nil {
			t.Fatalf("CountRecords(updated_after) error = %v", err)
		}
		if count != 1 {
			t.Fatalf("updated_after count = %d, want 1", count)
		}

		count, err = repo.CountRecords(ctx, repository.ListRecordsFilter{IncludeDeleted: true})
		if err != nil {
			t.Fatalf("CountRecords(include deleted) error = %v", err)
		}
		if count != 3 {
			t.Fatalf("include-deleted count = %d, want 3", count)
		}

		count, err = repo.CountRecords(ctx, repository.ListRecordsFilter{OnlyDeleted: true})
		if err != nil {
			t.Fatalf("CountRecords(only deleted) error = %v", err)
		}
		if count != 1 {
			t.Fatalf("only-deleted count = %d, want 1", count)
		}

		count, err = repo.CountRecords(ctx, repository.ListRecordsFilter{HasHTML: true})
		if err != nil {
			t.Fatalf("CountRecords(has html) error = %v", err)
		}
		if count != 1 {
			t.Fatalf("has-html count = %d, want 1", count)
		}

		count, err = repo.CountRecords(ctx, repository.ListRecordsFilter{HasData: true})
		if err != nil {
			t.Fatalf("CountRecords(has data) error = %v", err)
		}
		if count != 1 {
			t.Fatalf("has-data count = %d, want 1", count)
		}

		count, err = repo.CountRecords(ctx, repository.ListRecordsFilter{ProjectID: strPtr("count/missing")})
		if err != nil {
			t.Fatalf("CountRecords(empty) error = %v", err)
		}
		if count != 0 {
			t.Fatalf("empty count = %d, want 0", count)
		}

		notesQuery := "alpha notes"
		count, err = repo.CountRecords(ctx, repository.ListRecordsFilter{Query: &notesQuery})
		if err != nil {
			t.Fatalf("CountRecords(notes query) error = %v", err)
		}
		if count != 1 {
			t.Fatalf("notes query count = %d, want 1", count)
		}

		htmlQuery := "alpha html"
		count, err = repo.CountRecords(ctx, repository.ListRecordsFilter{Query: &htmlQuery})
		if err != nil {
			t.Fatalf("CountRecords(html query) error = %v", err)
		}
		if count != 1 {
			t.Fatalf("html query count = %d, want 1", count)
		}

		_ = beta
	})

	t.Run("CountRecordChildren aggregates child rows", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		withChildren := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20260511-a1a1a1a1",
			Date:        "2026-05-11",
			DayOrder:    "a",
			HTMLContent: strPtr("<p>with children</p>"),
		})
		zeroChildren := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20260511-b2b2b2b2",
			Date:        "2026-05-11",
			DayOrder:    "b",
			HTMLContent: strPtr("<p>zero children</p>"),
		})
		for _, name := range []string{"one.png", "two.png"} {
			if _, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
				RecordID: withChildren.ID,
				Filename: name,
				S3Key:    "figures/" + name,
			}); err != nil {
				t.Fatalf("CreateRecordFigure(%s) error = %v", name, err)
			}
		}
		if _, err := repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
			RecordID: withChildren.ID,
			Filename: "metrics.json",
			S3Key:    "data/metrics.json",
			Size:     2,
			Hash:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}); err != nil {
			t.Fatalf("CreateRecordDataFile() error = %v", err)
		}

		counts, err := repo.CountRecordChildren(ctx, nil)
		if err != nil {
			t.Fatalf("CountRecordChildren(nil) error = %v", err)
		}
		if len(counts) != 0 {
			t.Fatalf("nil input counts = %+v, want empty map", counts)
		}

		counts, err = repo.CountRecordChildren(ctx, []string{})
		if err != nil {
			t.Fatalf("CountRecordChildren(empty) error = %v", err)
		}
		if len(counts) != 0 {
			t.Fatalf("empty input counts = %+v, want empty map", counts)
		}

		counts, err = repo.CountRecordChildren(ctx, []string{withChildren.ID, zeroChildren.ID, "20260511-missing"})
		if err != nil {
			t.Fatalf("CountRecordChildren(mixed) error = %v", err)
		}
		if counts[withChildren.ID].Figures != 2 || counts[withChildren.ID].DataFiles != 1 {
			t.Fatalf("with-children counts = %+v, want figures=2 data_files=1", counts[withChildren.ID])
		}
		if counts[zeroChildren.ID] != (repository.ChildCounts{}) {
			t.Fatalf("zero-child counts = %+v, want zero value", counts[zeroChildren.ID])
		}
		if counts["20260511-missing"] != (repository.ChildCounts{}) {
			t.Fatalf("missing-id counts = %+v, want zero value", counts["20260511-missing"])
		}
	})

	t.Run("CountRecordChildren handles large ID sets without exceeding parameter limits", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		// 1500 IDs spans multiple SQLite chunks (500/chunk) and exercises the
		// Postgres array-binding path. Only the first record actually exists in
		// the DB; the rest are "missing" sentinels that must round-trip safely.
		populated := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID:          "20260512-aaaaaaaa",
			Date:        "2026-05-12",
			DayOrder:    "a",
			HTMLContent: strPtr("<p>scaled</p>"),
		})
		if _, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
			RecordID: populated.ID,
			Filename: "scale.png",
			S3Key:    "figures/scale.png",
		}); err != nil {
			t.Fatalf("CreateRecordFigure() error = %v", err)
		}

		ids := make([]string, 0, 1500)
		ids = append(ids, populated.ID)
		for i := 1; i < 1500; i++ {
			ids = append(ids, fmt.Sprintf("20260512-%08x", i))
		}

		counts, err := repo.CountRecordChildren(ctx, ids)
		if err != nil {
			t.Fatalf("CountRecordChildren(large) error = %v", err)
		}
		if counts[populated.ID].Figures != 1 || counts[populated.ID].DataFiles != 0 {
			t.Fatalf("large-input counts[%s] = %+v, want figures=1 data_files=0", populated.ID, counts[populated.ID])
		}
		if got := len(counts); got != 1 {
			t.Fatalf("large-input total counts = %d, want 1 (only populated record has children)", got)
		}
	})

	t.Run("CountActiveRecords and CountTrashedRecords", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		// Empty DB: both counts are zero.
		active, err := repo.CountActiveRecords(ctx)
		if err != nil {
			t.Fatalf("CountActiveRecords(empty) error = %v", err)
		}
		if active != 0 {
			t.Fatalf("expected 0 active records, got %d", active)
		}
		trashed, err := repo.CountTrashedRecords(ctx)
		if err != nil {
			t.Fatalf("CountTrashedRecords(empty) error = %v", err)
		}
		if trashed != 0 {
			t.Fatalf("expected 0 trashed records, got %d", trashed)
		}

		// Create two records, trash one.
		mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID: "20250410-c0a1b2c3", Date: "2025-04-10", DayOrder: "a", HTMLContent: strPtr("<h1>A</h1>"),
		})
		toTrash := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID: "20250410-c0d4e5f6", Date: "2025-04-10", DayOrder: "b", HTMLContent: strPtr("<h1>B</h1>"),
		})
		if err := repo.SoftDeleteRecord(ctx, toTrash.ID); err != nil {
			t.Fatalf("SoftDeleteRecord() error = %v", err)
		}

		active, err = repo.CountActiveRecords(ctx)
		if err != nil {
			t.Fatalf("CountActiveRecords error = %v", err)
		}
		if active != 1 {
			t.Fatalf("expected 1 active record, got %d", active)
		}
		trashed, err = repo.CountTrashedRecords(ctx)
		if err != nil {
			t.Fatalf("CountTrashedRecords error = %v", err)
		}
		if trashed != 1 {
			t.Fatalf("expected 1 trashed record, got %d", trashed)
		}
	})

	t.Run("PurgeDeletedRecords", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		// Purge on empty DB returns empty slice.
		ids, err := repo.PurgeDeletedRecords(ctx)
		if err != nil {
			t.Fatalf("PurgeDeletedRecords(empty) error = %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("expected 0 purged IDs, got %d", len(ids))
		}

		// Create 3 records, trash 2, purge.
		activeRecord := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID: "20250411-a0a1a1a1", Date: "2025-04-11", DayOrder: "a", HTMLContent: strPtr("<h1>Active</h1>"),
		})
		trash1 := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID: "20250411-b0b2b2b2", Date: "2025-04-11", DayOrder: "b", HTMLContent: strPtr("<h1>Trash1</h1>"),
		})
		trash2 := mustCreateRecord(t, ctx, repo, repository.CreateRecordInput{
			ID: "20250411-c0c3c3c3", Date: "2025-04-11", DayOrder: "c", HTMLContent: strPtr("<h1>Trash2</h1>"),
		})

		if err := repo.SoftDeleteRecord(ctx, trash1.ID); err != nil {
			t.Fatalf("SoftDeleteRecord(1) error = %v", err)
		}
		if err := repo.SoftDeleteRecord(ctx, trash2.ID); err != nil {
			t.Fatalf("SoftDeleteRecord(2) error = %v", err)
		}

		ids, err = repo.PurgeDeletedRecords(ctx)
		if err != nil {
			t.Fatalf("PurgeDeletedRecords error = %v", err)
		}
		if len(ids) != 2 {
			t.Fatalf("expected 2 purged IDs, got %d: %v", len(ids), ids)
		}

		// Active record should still exist.
		_, err = repo.GetRecordByID(ctx, activeRecord.ID)
		if err != nil {
			t.Fatalf("active record should still exist: %v", err)
		}

		// Trashed records should be hard-deleted.
		for _, trashID := range []string{trash1.ID, trash2.ID} {
			_, err = repo.GetRecordByID(ctx, trashID)
			if !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("expected ErrNotFound for purged record %s, got %v", trashID, err)
			}
		}

		// Count should reflect the purge.
		trashed, err := repo.CountTrashedRecords(ctx)
		if err != nil {
			t.Fatalf("CountTrashedRecords after purge error = %v", err)
		}
		if trashed != 0 {
			t.Fatalf("expected 0 trashed after purge, got %d", trashed)
		}
	})

	t.Run("registry CRUD archive restore and import upsert", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		projects, err := repo.ListProjects(ctx, false)
		if err != nil {
			t.Fatalf("ListProjects(empty) error = %v", err)
		}
		if len(projects) != 0 {
			t.Fatalf("expected empty project slice, got %v", projects)
		}

		project, err := repo.CreateProject(ctx, repository.CreateRegistryInput{ID: "org/alpha"})
		if err != nil {
			t.Fatalf("CreateProject() error = %v", err)
		}
		archivedProject, err := repo.ArchiveProject(ctx, project.ID)
		if err != nil {
			t.Fatalf("ArchiveProject() error = %v", err)
		}
		if archivedProject.ArchivedAt == nil {
			t.Fatal("expected archived project timestamp")
		}
		activeProjects, err := repo.ListProjects(ctx, false)
		if err != nil {
			t.Fatalf("ListProjects(active) error = %v", err)
		}
		if len(activeProjects) != 0 {
			t.Fatalf("expected archived project excluded from active list, got %+v", activeProjects)
		}
		restoredProject, err := repo.RestoreProject(ctx, project.ID)
		if err != nil {
			t.Fatalf("RestoreProject() error = %v", err)
		}
		if restoredProject.ArchivedAt != nil {
			t.Fatalf("expected restored project archived_at nil, got %v", restoredProject.ArchivedAt)
		}

		device, err := repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: "device/a"})
		if err != nil {
			t.Fatalf("CreateDevice() error = %v", err)
		}
		archivedDevice, err := repo.ArchiveDevice(ctx, device.ID)
		if err != nil {
			t.Fatalf("ArchiveDevice() error = %v", err)
		}
		if archivedDevice.ArchivedAt == nil {
			t.Fatal("expected archived device timestamp")
		}
		if _, err := repo.RestoreDevice(ctx, device.ID); err != nil {
			t.Fatalf("RestoreDevice() error = %v", err)
		}

		olderUpdatedAt := project.UpdatedAt.Add(-time.Hour)
		changed, err := repo.UpsertProjectForImport(ctx, repository.Project{
			ID:        project.ID,
			CreatedAt: project.CreatedAt,
			UpdatedAt: olderUpdatedAt,
		})
		if err != nil {
			t.Fatalf("UpsertProjectForImport(older) error = %v", err)
		}
		if changed {
			t.Fatal("expected older imported project to be skipped")
		}

		newerUpdatedAt := project.UpdatedAt.Add(time.Hour)
		importArchivedAt := newerUpdatedAt.Add(time.Minute)
		changed, err = repo.UpsertProjectForImport(ctx, repository.Project{
			ID:         project.ID,
			CreatedAt:  project.CreatedAt,
			UpdatedAt:  newerUpdatedAt,
			ArchivedAt: &importArchivedAt,
		})
		if err != nil {
			t.Fatalf("UpsertProjectForImport(newer) error = %v", err)
		}
		if !changed {
			t.Fatal("expected newer imported project to replace existing")
		}
		importedProject, err := repo.GetProjectByID(ctx, project.ID)
		if err != nil {
			t.Fatalf("GetProjectByID(imported) error = %v", err)
		}
		if importedProject.ArchivedAt == nil || !importedProject.UpdatedAt.Equal(newerUpdatedAt.UTC()) {
			t.Fatalf("unexpected imported project row: %+v", importedProject)
		}
	})

}

func mustCreateRecord(t *testing.T, ctx context.Context, repo repository.Repository, input repository.CreateRecordInput) repository.Record {
	t.Helper()

	if input.ProjectID == "" {
		input.ProjectID = "contract/default-project"
	}
	if input.SourceDeviceID == "" {
		input.SourceDeviceID = "contract-device"
	}
	_, err := repo.GetProjectByID(ctx, input.ProjectID)
	if errors.Is(err, repository.ErrNotFound) {
		_, err = repo.CreateProject(ctx, repository.CreateRegistryInput{ID: input.ProjectID})
	}
	if err != nil {
		t.Fatalf("ensure project registry row failed: %v", err)
	}
	_, err = repo.GetDeviceByID(ctx, input.SourceDeviceID)
	if errors.Is(err, repository.ErrNotFound) {
		_, err = repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: input.SourceDeviceID})
	}
	if err != nil {
		t.Fatalf("ensure device registry row failed: %v", err)
	}

	record, err := repo.CreateRecord(ctx, input)
	if err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}
	if record.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt after CreateRecord")
	}
	if record.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero UpdatedAt after CreateRecord")
	}
	return record
}

func recordIDs(records []repository.Record) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids
}

func assertExactOrder(t *testing.T, got []string, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d (%v)", len(want), len(got), got)
	}
	for idx := range got {
		if got[idx] != want[idx] {
			t.Fatalf("unexpected ordering at %d: got=%v want=%v", idx, got, want)
		}
	}
}

func strPtr(value string) *string {
	return &value
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}
