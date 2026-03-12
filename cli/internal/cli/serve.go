package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/serve"
	"github.com/spf13/cobra"
)

func newServeCommand(stdout io.Writer, stderr io.Writer, version string) *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local API server for web UI development",
		Long: `Start an HTTP server that implements the same REST API as the Next.js
cloud backend, backed by the local SQLite database and filesystem.

Use this to run the web UI locally without cloud credentials (Neon/S3).
Set LOCAL_BACKEND_URL=http://127.0.0.1:<port> when running 'next dev'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), stdout, stderr, port, version)
		},
	}

	cmd.Flags().IntVar(&port, "port", 9876, "Port to listen on")

	return cmd
}

func runServe(ctx context.Context, _ io.Writer, stderr io.Writer, port int, version string) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	dataDir := basePath(homeDir)

	srv, err := serve.NewServer(stack.Repo, dataDir, port, version)
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	shutdown := func(reason string) error {
		if reason != "" {
			_, _ = fmt.Fprintln(stderr, reason)
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}

		err := <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}

	select {
	case sig := <-sigCh:
		return shutdown(fmt.Sprintf("\nReceived %s, shutting down...", sig))
	case <-ctx.Done():
		return shutdown("")
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
