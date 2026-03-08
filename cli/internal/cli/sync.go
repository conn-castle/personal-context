package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	pcsync "github.com/conn-castle/personal-context/cli/internal/sync"
	"github.com/conn-castle/personal-context/cli/internal/syncengine"
	"github.com/spf13/cobra"
)

type syncRunner interface {
	Sync(ctx context.Context) error
}

var (
	newSyncSessionManagerFn = func(homeDir string) (pcsync.SessionManager, error) {
		return syncengine.NewManager(filepath.Join(basePath(homeDir), ".pc"))
	}
	newSyncServiceFn = func(
		local *localStack,
		cloud *cloudStack,
		session pcsync.SessionManager,
	) (syncRunner, error) {
		return pcsync.NewService(local.Repo, cloud.Repo, local.FS, cloud.S3, session)
	}
	runAutoSyncFn = runAutoSync
)

func newSyncCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Run a bidirectional cloud sync",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd.Context(), stdout, stderr)
		},
	}
	return cmd
}

func runSync(ctx context.Context, stdout io.Writer, _ io.Writer) (returnErr error) {
	runner, cleanup, err := openSyncRunner(ctx)
	if err != nil {
		if errors.Is(err, errCloudNotConfigured) {
			return fmt.Errorf("cloud is not configured")
		}
		return err
	}
	defer func() {
		if cleanupErr := cleanup(); cleanupErr != nil {
			if returnErr == nil {
				returnErr = cleanupErr
			} else {
				returnErr = errors.Join(returnErr, cleanupErr)
			}
		}
	}()

	if err := runner.Sync(ctx); err != nil {
		return fmt.Errorf("run sync: %w", err)
	}

	_, _ = fmt.Fprintln(stdout, "Sync complete")
	return nil
}

func runAutoSync(ctx context.Context, stderr io.Writer) error {
	runner, cleanup, err := openSyncRunner(ctx)
	if err != nil {
		if errors.Is(err, errCloudNotConfigured) {
			return nil
		}
		warnAutoSync(stderr, err)
		return nil
	}
	defer func() {
		if cleanupErr := cleanup(); cleanupErr != nil {
			warnAutoSync(stderr, cleanupErr)
		}
	}()

	if err := runner.Sync(ctx); err != nil {
		warnAutoSync(stderr, err)
	}
	return nil
}

func openSyncRunner(ctx context.Context) (syncRunner, func() error, error) {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return nil, nil, err
	}

	localStack, err := openLocalStack(homeDir)
	if err != nil {
		return nil, nil, err
	}

	cloudStack, err := openCloudStackFn(ctx, homeDir)
	if err != nil {
		_ = localStack.Close()
		return nil, nil, err
	}

	session, err := newSyncSessionManagerFn(homeDir)
	if err != nil {
		_ = cloudStack.Close()
		_ = localStack.Close()
		return nil, nil, fmt.Errorf("create sync session manager: %w", err)
	}

	service, err := newSyncServiceFn(localStack, cloudStack, session)
	if err != nil {
		_ = cloudStack.Close()
		_ = localStack.Close()
		return nil, nil, fmt.Errorf("create sync service: %w", err)
	}

	cleanup := func() error {
		if err := cloudStack.Close(); err != nil {
			_ = localStack.Close()
			return err
		}
		return localStack.Close()
	}
	return service, cleanup, nil
}

func warnAutoSync(stderr io.Writer, err error) {
	if stderr == nil || err == nil {
		return
	}
	_, _ = fmt.Fprintf(stderr, "warning: auto-sync failed: %v\n", err)
}
