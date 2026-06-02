package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdkconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"

	pcconfig "github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	postgresrepo "github.com/conn-castle/personal-context/cli/internal/repository/postgres"
)

const awsProfileName = "personal-context"

var (
	// validateNeonConnectivityFn opens a temporary connection pool and pings Postgres.
	validateNeonConnectivityFn = func(ctx context.Context, neonURL string) error {
		pool, err := newPGXPoolFn(ctx, neonURL)
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		defer closePGXPool(pool)
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("ping: %w", err)
		}
		return nil
	}
	// validateS3AccessFn creates temporary AWS credentials and verifies bucket access.
	validateS3AccessFn = func(ctx context.Context, bucket string, region string, accessKey string, secretKey string, endpoint string, forcePathStyle bool) error {
		cfg, err := awssdkconfig.LoadDefaultConfig(ctx,
			awssdkconfig.WithRegion(region),
			awssdkconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
			),
		)
		if err != nil {
			return fmt.Errorf("load aws config: %w", err)
		}
		var opts []func(*awss3.Options)
		if endpoint != "" {
			opts = append(opts, func(o *awss3.Options) {
				o.BaseEndpoint = aws.String(endpoint)
			})
		}
		if forcePathStyle {
			opts = append(opts, func(o *awss3.Options) {
				o.UsePathStyle = true
			})
		}
		s3Client := awss3.NewFromConfig(cfg, opts...)
		_, err = s3Client.HeadBucket(ctx, &awss3.HeadBucketInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			return fmt.Errorf("check bucket %q: %w", bucket, err)
		}
		return nil
	}
	applyPostgresSchemaFn = func(ctx context.Context, pool *pgxpool.Pool) error {
		return postgresrepo.ApplySchema(ctx, pool)
	}
	writeAWSProfileFn  = writeAWSSharedCredentialsProfile
	removeAWSProfileFn = removeAWSSharedCredentialsProfile
	promptLineFn       = promptLine
	promptConfirmFn    = promptConfirm
)

// runSetupRemoveCloud clears cloud configuration and removes the AWS credentials profile.
func runSetupRemoveCloud(stdout io.Writer, homeDir string, store pcconfig.Store) error {
	cfg, err := store.Read()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintln(stdout, "Cloud is not configured. Nothing to remove.")
			return nil
		}
		return fmt.Errorf("read config: %w", err)
	}

	// Mode() is guaranteed to succeed after a successful Read() (Read validates Mode).
	mode, _ := cfg.Mode()
	if mode == pcconfig.ModeLocalOnly {
		_, _ = fmt.Fprintln(stdout, "Cloud is not configured. Nothing to remove.")
		return nil
	}

	localOnly := pcconfig.Config{
		ActiveProject:   cfg.ActiveProject,
		GCRetentionDays: cfg.GCRetentionDays,
	}
	if err := store.Write(localOnly); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	userHome, err := resolveUserHomeDir()
	if err != nil {
		return err
	}
	if err := removeAWSProfileFn(userHome, cfg.AWSProfile); err != nil {
		return fmt.Errorf("remove AWS credentials profile: %w", err)
	}

	_, _ = fmt.Fprintln(stdout, "Cloud configuration removed.")
	return nil
}

func runSetupInitCloudSchema(ctx context.Context, stdout io.Writer, neonURL string) error {
	if err := pcconfig.ValidateNeonURL(neonURL); err != nil {
		return fmt.Errorf("invalid neon URL: %w", err)
	}
	if err := validateNeonConnectivityFn(ctx, neonURL); err != nil {
		return fmt.Errorf("neon connectivity check failed: %w", err)
	}

	pool, err := newPGXPoolFn(ctx, neonURL)
	if err != nil {
		return fmt.Errorf("connect to postgres for schema: %w", err)
	}
	defer closePGXPool(pool)

	if err := applyPostgresSchemaFn(ctx, pool); err != nil {
		return fmt.Errorf("apply postgres schema: %w", err)
	}

	_, _ = fmt.Fprintln(stdout, "Cloud Postgres schema initialized successfully.")
	return nil
}

// runSetupCloud validates cloud credentials, applies the Postgres schema, and writes
// cloud configuration. When interactive is true, a merge preview is shown and
// confirmation is required before writing.
//
// Args: localRepo is required when interactive is true (for merge preview record count).
func runSetupCloud(
	ctx context.Context,
	stdout io.Writer,
	_ io.Writer,
	stdin io.Reader,
	homeDir string,
	store pcconfig.Store,
	neonURL string, s3Bucket string, s3Region string,
	awsKey string, awsSecret string,
	s3Endpoint string, s3ForcePathStyle bool,
	apiKey string,
	localRepo repository.Repository,
	interactive bool,
) error {
	if interactive && localRepo == nil {
		return errors.New("localRepo is required for interactive cloud setup")
	}

	// 1. Validate formats.
	if err := pcconfig.ValidateNeonURL(neonURL); err != nil {
		return fmt.Errorf("invalid neon URL: %w", err)
	}
	if err := pcconfig.ValidateS3Bucket(s3Bucket); err != nil {
		return fmt.Errorf("invalid S3 bucket: %w", err)
	}
	if err := pcconfig.ValidateS3Region(s3Region); err != nil {
		return fmt.Errorf("invalid S3 region: %w", err)
	}
	if strings.TrimSpace(awsKey) == "" {
		return fmt.Errorf("AWS access key is required")
	}
	if strings.TrimSpace(awsSecret) == "" {
		return fmt.Errorf("AWS secret key is required")
	}
	trimmedAPIKey := strings.TrimSpace(apiKey)
	if trimmedAPIKey == "" {
		return fmt.Errorf("API key is required")
	}

	// 2. Validate Neon connectivity.
	if err := validateNeonConnectivityFn(ctx, neonURL); err != nil {
		return fmt.Errorf("neon connectivity check failed: %w", err)
	}

	// 3. Validate S3 access.
	if err := validateS3AccessFn(ctx, s3Bucket, s3Region, awsKey, awsSecret, s3Endpoint, s3ForcePathStyle); err != nil {
		return fmt.Errorf("S3 access check failed: %w", err)
	}

	// 4. Apply Postgres schema (creates tables if they don't exist).
	pool, err := newPGXPoolFn(ctx, neonURL)
	if err != nil {
		return fmt.Errorf("connect to postgres for schema: %w", err)
	}
	defer closePGXPool(pool)

	if err := applyPostgresSchemaFn(ctx, pool); err != nil {
		return fmt.Errorf("apply postgres schema: %w", err)
	}

	// 5. Validate API key and resolve user scope for cloud operations.
	validatedUserID, err := resolveUserIDFn(ctx, pool, trimmedAPIKey)
	if err != nil {
		return fmt.Errorf("validate API key: %w", err)
	}
	if strings.TrimSpace(validatedUserID) == "" {
		return fmt.Errorf("validate API key: resolved user ID is empty")
	}

	// 6. Merge preview (interactive only).
	if interactive {
		cloudRepo, err := newPostgresRepoFn(pool, validatedUserID)
		if err != nil {
			return fmt.Errorf("create temporary postgres repository: %w", err)
		}

		localRecords, err := localRepo.ListRecords(ctx, repository.ListRecordsFilter{})
		if err != nil {
			return fmt.Errorf("count local records: %w", err)
		}
		cloudRecords, err := cloudRepo.ListRecords(ctx, repository.ListRecordsFilter{})
		if err != nil {
			return fmt.Errorf("count cloud records: %w", err)
		}

		_, _ = fmt.Fprintf(stdout, "\nMerge preview:\n")
		_, _ = fmt.Fprintf(stdout, "  Local:  %d record(s)\n", len(localRecords))
		_, _ = fmt.Fprintf(stdout, "  Cloud:  %d record(s)\n", len(cloudRecords))
		_, _ = fmt.Fprintf(stdout, "  After sync, all records will be merged.\n\n")

		proceed, err := promptConfirmFn(stdin, stdout, "Proceed?")
		if err != nil {
			return fmt.Errorf("read confirmation: %w", err)
		}
		if !proceed {
			_, _ = fmt.Fprintln(stdout, "Cloud setup cancelled.")
			return nil
		}
	}

	existing := pcconfig.Config{}
	if current, readErr := store.Read(); readErr == nil {
		existing = current
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read config: %w", readErr)
	}

	// 7. Write AWS credentials profile (snapshot existing file for rollback).
	userHome, err := resolveUserHomeDir()
	if err != nil {
		return err
	}
	credsPath := awsCredentialsPath(userHome)
	prevCreds, prevErr := os.ReadFile(credsPath)
	hadPrevCreds := prevErr == nil

	if err := writeAWSProfileFn(userHome, awsProfileName, awsKey, awsSecret); err != nil {
		return fmt.Errorf("write AWS credentials profile: %w", err)
	}

	// 8. Write cloud config (preserve ActiveProject and GCRetentionDays from existing config).
	cfg := pcconfig.Config{
		NeonURL:          neonURL,
		S3Bucket:         s3Bucket,
		S3Region:         s3Region,
		AWSProfile:       awsProfileName,
		ActiveProject:    existing.ActiveProject,
		S3Endpoint:       s3Endpoint,
		S3ForcePathStyle: s3ForcePathStyle,
		APIKey:           trimmedAPIKey,
		GCRetentionDays:  existing.GCRetentionDays,
	}
	if err := store.Write(cfg); err != nil {
		// Rollback AWS credentials to previous state on config write failure.
		if hadPrevCreds {
			_ = os.WriteFile(credsPath, prevCreds, awsProfileFilePermission)
		} else {
			_ = removeAWSProfileFn(userHome, awsProfileName)
		}
		return fmt.Errorf("write config: %w", err)
	}

	_, _ = fmt.Fprintln(stdout, "Cloud sync configured successfully.")
	return nil
}

// ensureBufioReader wraps r in a bufio.Reader if it isn't one already.
// This lets multiple prompt calls share the same buffered reader.
func ensureBufioReader(r io.Reader) *bufio.Reader {
	if br, ok := r.(*bufio.Reader); ok {
		return br
	}
	return bufio.NewReader(r)
}

// promptLine prints a prompt and reads one trimmed line from stdin.
func promptLine(stdin io.Reader, stdout io.Writer, prompt string) (string, error) {
	_, _ = fmt.Fprint(stdout, prompt)
	br := ensureBufioReader(stdin)
	line, err := br.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("no input received")
	}
	return strings.TrimSpace(line), nil
}

// promptConfirm prints a yes/no prompt and returns true for "y" or "Y".
// EOF or empty input returns false (the default for [y/N]).
func promptConfirm(stdin io.Reader, stdout io.Writer, prompt string) (bool, error) {
	_, _ = fmt.Fprint(stdout, prompt+" [y/N]: ")
	br := ensureBufioReader(stdin)
	line, err := br.ReadString('\n')
	if err != nil && line == "" {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "y"), nil
}
