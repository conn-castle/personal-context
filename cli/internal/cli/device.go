package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newDeviceCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device",
		Short: "Manage source device registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newDeviceListCommand(stdout, stderr))
	cmd.AddCommand(newDeviceRegisterCommand(stdout, stderr))
	cmd.AddCommand(newDeviceArchiveCommand(stdout, stderr))
	cmd.AddCommand(newDeviceRestoreCommand(stdout, stderr))
	return cmd
}

func newDeviceListCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var includeArchived bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered devices",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeviceList(cmd.Context(), stdout, stderr, includeArchived)
		},
	}
	cmd.Flags().BoolVar(&includeArchived, "all", false, "Include archived devices")
	return cmd
}

func runDeviceList(ctx context.Context, stdout io.Writer, _ io.Writer, includeArchived bool) error {
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	devices, err := stack.Repo.ListDevices(ctx, includeArchived)
	if err != nil {
		return fmt.Errorf("list devices: %w", err)
	}
	if len(devices) == 0 {
		_, _ = fmt.Fprintln(stdout, "No devices registered.")
		return nil
	}
	for _, device := range devices {
		if device.ArchivedAt != nil {
			_, _ = fmt.Fprintf(stdout, "%s (archived)\n", device.ID)
		} else {
			_, _ = fmt.Fprintln(stdout, device.ID)
		}
	}
	return nil
}

func newDeviceRegisterCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "register <id>",
		Short: "Register a source device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceRegister(cmd.Context(), stdout, stderr, args[0])
		},
	}
}

func runDeviceRegister(ctx context.Context, stdout io.Writer, _ io.Writer, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("device id must not be empty")
	}
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	device, err := stack.Repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: strings.TrimSpace(id)})
	if err != nil {
		return fmt.Errorf("register device: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "%s\n", device.ID)
	return nil
}

func newDeviceArchiveCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <id>",
		Short: "Archive a source device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceArchive(cmd.Context(), stdout, stderr, args[0])
		},
	}
}

func runDeviceArchive(ctx context.Context, stdout io.Writer, _ io.Writer, id string) error {
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	device, err := stack.Repo.ArchiveDevice(ctx, id)
	if err != nil {
		return fmt.Errorf("archive device: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "%s archived\n", device.ID)
	return nil
}

func newDeviceRestoreCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <id>",
		Short: "Restore an archived source device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceRestore(cmd.Context(), stdout, stderr, args[0])
		},
	}
}

func runDeviceRestore(ctx context.Context, stdout io.Writer, _ io.Writer, id string) error {
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	device, err := stack.Repo.RestoreDevice(ctx, id)
	if err != nil {
		return fmt.Errorf("restore device: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "%s restored\n", device.ID)
	return nil
}
