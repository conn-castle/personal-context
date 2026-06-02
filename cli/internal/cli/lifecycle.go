package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	pcs3 "github.com/conn-castle/personal-context/cli/internal/s3client"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

// trashedItem is one row in the unified trash listing produced by per-domain
// adapters. rawSourceKey is empty for records and set to the PC-owned managed
// raw chat source key for chats.
type trashedItem struct {
	ID           string
	Domain       string
	Date         string
	DeletedAt    *time.Time
	rawSourceKey string
}

// trashedFetcher returns soft-deleted items for one domain.
type trashedFetcher func(ctx context.Context, repo repository.Repository) ([]trashedItem, error)

// hardDeleteLocalFn purges one item from local DB + filesystem. Callers must
// remove the matching cloud DB row (and PC-owned cloud objects) first when
// cloud is configured.
type hardDeleteLocalFn func(ctx context.Context, stack *localStack, item trashedItem) error

// hardDeleteCloudFn removes the cloud DB row and any PC-owned cloud objects
// for one trashed item. ErrNotFound (or the equivalent) signals "already
// gone": callers should continue with local cleanup. Any other error skips
// local cleanup for that item.
type hardDeleteCloudFn func(ctx context.Context, cloud *cloudStack, item trashedItem) error

// trashDomain bundles per-domain hooks used by the shared trash/gc loop.
type trashDomain struct {
	Name      string
	List      trashedFetcher
	HardLocal hardDeleteLocalFn
	HardCloud hardDeleteCloudFn
}

// recordTrashDomain is the adapter for record trash/gc behavior preserved
// from cli/internal/cli/{trash,gc}.go before the lifecycle refactor.
func recordTrashDomain() trashDomain {
	return trashDomain{
		Name: "record",
		List: func(ctx context.Context, repo repository.Repository) ([]trashedItem, error) {
			records, err := repo.ListRecords(ctx, repository.ListRecordsFilter{OnlyDeleted: true})
			if err != nil {
				return nil, fmt.Errorf("list deleted records: %w", err)
			}
			out := make([]trashedItem, 0, len(records))
			for _, r := range records {
				out = append(out, trashedItem{
					ID:        r.ID,
					Domain:    "record",
					Date:      r.Date,
					DeletedAt: r.DeletedAt,
				})
			}
			return out, nil
		},
		HardLocal: func(ctx context.Context, stack *localStack, item trashedItem) error {
			if err := stack.Repo.DeleteRecord(ctx, item.ID); err != nil {
				return &lifecycleDBError{op: "hard delete record " + item.ID, err: err}
			}
			if err := stack.FS.DeleteRecordDir(item.ID); err != nil {
				return &lifecycleFSError{err: err, dbDeleted: true}
			}
			return nil
		},
		HardCloud: func(ctx context.Context, cloud *cloudStack, item trashedItem) error {
			return cloud.Repo.DeleteRecord(ctx, item.ID)
		},
	}
}

// chatTrashDomain is the adapter for chat trash/gc parity per plan: soft
// delete preserves chats/raw/{id}/, hard delete/gc removes it; cloud gc
// deletes the PC-owned cloud raw object before deleting the DB row.
func chatTrashDomain() trashDomain {
	return trashDomain{
		Name: "chat",
		List: func(ctx context.Context, repo repository.Repository) ([]trashedItem, error) {
			sessions, err := repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{OnlyDeleted: true})
			if err != nil {
				return nil, fmt.Errorf("list deleted chats: %w", err)
			}
			out := make([]trashedItem, 0, len(sessions))
			for _, s := range sessions {
				item := trashedItem{
					ID:        s.ID,
					Domain:    "chat",
					Date:      s.LastActivityAt.UTC().Format("2006-01-02"),
					DeletedAt: s.DeletedAt,
				}
				if s.RawSourceKey != nil {
					item.rawSourceKey = *s.RawSourceKey
				}
				out = append(out, item)
			}
			return out, nil
		},
		HardLocal: func(ctx context.Context, stack *localStack, item trashedItem) error {
			// Delete the DB row first so a filesystem cleanup failure leaves
			// no metadata pointing at a now-missing managed raw source. This
			// mirrors recordTrashDomain.HardLocal and lets the caller treat
			// FS failures as warn-and-continue via lifecycleFSError{dbDeleted: true}.
			if err := stack.Repo.DeleteChatSession(ctx, item.ID); err != nil {
				return &lifecycleDBError{op: "hard delete chat " + item.ID, err: err}
			}
			if err := stack.FS.DeleteChatSource(item.ID); err != nil {
				return &lifecycleFSError{err: err, dbDeleted: true}
			}
			return nil
		},
		HardCloud: func(ctx context.Context, cloud *cloudStack, item trashedItem) error {
			if item.rawSourceKey != "" {
				if err := deleteCloudObjectTolerant(ctx, cloud.S3, item.rawSourceKey); err != nil {
					return fmt.Errorf("delete cloud raw chat object %s: %w", item.ID, err)
				}
			}
			return cloud.Repo.DeleteChatSession(ctx, item.ID)
		},
	}
}

// lifecycleDBError wraps a DB-side failure inside HardLocal so the
// orchestrator can distinguish DB failures (propagate) from filesystem
// cleanup failures (warn-and-continue).
type lifecycleDBError struct {
	op  string
	err error
}

func (e *lifecycleDBError) Error() string { return e.op + ": " + e.err.Error() }
func (e *lifecycleDBError) Unwrap() error { return e.err }

// lifecycleFSError wraps filesystem cleanup failure so it can be downgraded
// to a warning by the orchestrator.
type lifecycleFSError struct {
	err       error
	dbDeleted bool
}

func (e *lifecycleFSError) Error() string { return e.err.Error() }
func (e *lifecycleFSError) Unwrap() error { return e.err }

// deleteCloudObjectTolerant deletes an object and treats "object already
// missing" as success. Detection delegates to s3client.IsNotFound so missing
// buckets, auth failures, and other errors whose messages happen to contain
// "not found" are not silently swallowed as successful deletes.
func deleteCloudObjectTolerant(ctx context.Context, s3 *pcs3.Client, key string) error {
	if s3 == nil {
		return errors.New("cloud S3 client is required")
	}
	if err := s3.Delete(ctx, key); err != nil {
		if isCloudObjectNotFoundString(err) {
			return nil
		}
		return err
	}
	return nil
}

func isCloudObjectNotFoundString(err error) bool {
	return pcs3.IsNotFound(err)
}

// runTrashAll lists soft-deleted items across all domains in one table.
func runTrashAll(ctx context.Context, stdout io.Writer, _ io.Writer, domains []trashDomain) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}
	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	var all []trashedItem
	for _, d := range domains {
		items, err := d.List(ctx, stack.Repo)
		if err != nil {
			return err
		}
		all = append(all, items...)
	}
	if len(all) == 0 {
		_, _ = fmt.Fprintln(stdout, "Trash is empty.")
		return nil
	}
	return writeTrashTable(stdout, all)
}

func writeTrashTable(w io.Writer, items []trashedItem) error {
	if _, err := fmt.Fprintln(w, "ID\tTYPE\tDATE\tDELETED AT"); err != nil {
		return fmt.Errorf("write trash header: %w", err)
	}
	for _, it := range items {
		deletedAt := ""
		if it.DeletedAt != nil {
			deletedAt = it.DeletedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", it.ID, it.Domain, it.Date, deletedAt); err != nil {
			return fmt.Errorf("write trash row %s: %w", it.ID, err)
		}
	}
	return nil
}

// runGCAll hard-deletes expired trash across all domains, cloud-first, with
// per-item warn-and-skip on non-not-found cloud failures.
func runGCAll(ctx context.Context, stdout io.Writer, stderr io.Writer, domains []trashDomain) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}
	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	gcThreshold := stack.Config.GCRetention()
	now := time.Now()

	var expired []trashedItem
	for _, d := range domains {
		items, err := d.List(ctx, stack.Repo)
		if err != nil {
			return err
		}
		for _, it := range items {
			if it.DeletedAt != nil && now.Sub(*it.DeletedAt) > gcThreshold {
				expired = append(expired, it)
			}
		}
	}
	if len(expired) == 0 {
		_, _ = fmt.Fprintln(stdout, "No expired trash to clean up.")
		return nil
	}

	cloud, cloudErr := openCloudStackFn(ctx, homeDir, "")
	hasCloud := false
	switch {
	case cloudErr == nil:
		hasCloud = true
		defer func() { _ = cloud.Close() }()
	case errors.Is(cloudErr, errCloudNotConfigured):
	default:
		_, _ = fmt.Fprintf(stderr, "warning: cloud unreachable, locally deleted items may reappear after sync: %v\n", cloudErr)
	}

	byDomain := make(map[string]trashDomain, len(domains))
	for _, d := range domains {
		byDomain[d.Name] = d
	}

	removed := 0
	for _, item := range expired {
		domain, ok := byDomain[item.Domain]
		if !ok {
			return fmt.Errorf("no trash domain registered for %q", item.Domain)
		}
		if hasCloud {
			if err := domain.HardCloud(ctx, cloud, item); err != nil {
				if !errors.Is(err, repository.ErrNotFound) {
					_, _ = fmt.Fprintf(stderr, "Warning: failed to delete %s %s from cloud, skipping: %v\n", item.Domain, item.ID, err)
					continue
				}
			}
		}
		if err := domain.HardLocal(ctx, stack, item); err != nil {
			var dbErr *lifecycleDBError
			if errors.As(err, &dbErr) {
				return err
			}
			var fsErr *lifecycleFSError
			if errors.As(err, &fsErr) {
				_, _ = fmt.Fprintf(stderr, "Warning: failed to remove files for %s %s: %v\n", item.Domain, item.ID, err)
				if !fsErr.dbDeleted {
					continue
				}
			} else {
				return err
			}
		}
		removed++
		_, _ = fmt.Fprintf(stdout, "Deleted %s\n", item.ID)
	}

	_ = runAutoSyncFn(ctx, stderr)

	_, _ = fmt.Fprintf(stdout, "Removed %d item(s).\n", removed)
	return nil
}

// allTrashDomains returns the canonical set used by pc trash and pc gc, in a
// stable order (records first, then chats).
func allTrashDomains() []trashDomain {
	return []trashDomain{recordTrashDomain(), chatTrashDomain()}
}
