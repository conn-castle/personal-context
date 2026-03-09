package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/conn-castle/personal-context/cli/internal/gitsnapshot"
	"github.com/spf13/cobra"
)

func newVerifyCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var fromCloud bool

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Run a full Phase 7 round-trip verification against local or cloud state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runVerify(cmd.Context(), stdout, stderr, fromCloud)
		},
	}

	cmd.Flags().BoolVar(&fromCloud, "from-cloud", false, "Verify against configured cloud Postgres/S3 instead of local SQLite/files")

	return cmd
}

func runVerify(ctx context.Context, stdout io.Writer, _ io.Writer, fromCloud bool) error {
	tempRoot, err := os.MkdirTemp("", "pc-verify-*")
	if err != nil {
		return fmt.Errorf("create verify temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempRoot) }()

	sourceSnapshotDir := filepath.Join(tempRoot, "snapshot-a")
	roundTripSnapshotDir := filepath.Join(tempRoot, "snapshot-b")

	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	var snapshot gitsnapshot.Snapshot
	if fromCloud {
		cloud, err := openCloudStackFn(ctx, homeDir)
		if err != nil {
			if errors.Is(err, errCloudNotConfigured) {
				return fmt.Errorf("cloud is not configured; run 'pc setup' first")
			}
			return fmt.Errorf("open cloud: %w", err)
		}
		defer func() { _ = cloud.Close() }()
		snapshot, err = buildCloudSnapshot(ctx, homeDir, cloud)
		if err != nil {
			return err
		}
	} else {
		stack, err := openLocalStack(homeDir)
		if err != nil {
			return err
		}
		defer func() { _ = stack.Close() }()
		snapshot, err = buildLocalSnapshot(ctx, stack)
		if err != nil {
			return err
		}
	}

	if err := gitsnapshot.Write(sourceSnapshotDir, snapshot); err != nil {
		return fmt.Errorf("write source snapshot: %w", err)
	}

	restoreHome := filepath.Join(tempRoot, "restore-home")
	if err := ensureLocalEnvironment(ctx, restoreHome); err != nil {
		return err
	}
	restoreStack, err := openLocalStack(restoreHome)
	if err != nil {
		return err
	}
	defer func() { _ = restoreStack.Close() }()
	if _, err := importSnapshotIntoStack(ctx, restoreStack, snapshot); err != nil {
		return err
	}
	roundTripSnapshot, err := buildLocalSnapshot(ctx, restoreStack)
	if err != nil {
		return err
	}
	if err := gitsnapshot.Write(roundTripSnapshotDir, roundTripSnapshot); err != nil {
		return fmt.Errorf("write round-trip snapshot: %w", err)
	}
	if err := compareSnapshotDirs(sourceSnapshotDir, roundTripSnapshotDir); err != nil {
		return fmt.Errorf("round-trip verification failed: %w", err)
	}

	if fromCloud {
		_, _ = fmt.Fprintln(stdout, "Cloud round-trip verification passed")
	} else {
		_, _ = fmt.Fprintln(stdout, "Local round-trip verification passed")
	}
	return nil
}
