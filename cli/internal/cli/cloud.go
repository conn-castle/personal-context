package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdkconfig "github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"

	pcconfig "github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	postgresrepo "github.com/conn-castle/personal-context/cli/internal/repository/postgres"
	pcs3 "github.com/conn-castle/personal-context/cli/internal/s3client"
)

var (
	errCloudNotConfigured = errors.New("cloud is not configured")
	openCloudStackFn      = openCloudStack
)

type cloudStack struct {
	Config pcconfig.Config
	Store  pcconfig.Store
	Repo   repository.Repository
	S3     *pcs3.Client
	pool   *pgxpool.Pool
}

var (
	validateCloudConfigFn = pcconfig.ValidateCloudConfig
	loadAWSConfigFn       = loadAWSConfig
	newPGXPoolFn          = func(ctx context.Context, connectionString string) (*pgxpool.Pool, error) {
		return pgxpool.New(ctx, connectionString)
	}
	newPostgresRepoFn = func(pool *pgxpool.Pool, userID string) (repository.Repository, error) {
		return postgresrepo.New(pool, userID)
	}
	newAWSSDKS3ClientFn = func(cfg aws.Config, endpoint string, forcePathStyle bool) *awss3.Client {
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
		return awss3.NewFromConfig(cfg, opts...)
	}
	newCloudS3ClientFn = func(client *awss3.Client, bucket string, keyPrefix string) (*pcs3.Client, error) {
		return pcs3.New(client, bucket, keyPrefix)
	}
	closePGXPoolFn = func(pool *pgxpool.Pool) {
		if pool != nil {
			pool.Close()
		}
	}
)

// Close releases any resources held by the cloud stack.
func (s *cloudStack) Close() error {
	closePGXPool(s.pool)
	return nil
}

// openCloudStack initializes the Postgres repository and S3 client for cloud mode.
// userID scopes all slide and sync queries to the authenticated user.
func openCloudStack(ctx context.Context, homeDir string, userID string) (*cloudStack, error) {
	if strings.TrimSpace(homeDir) == "" {
		return nil, fmt.Errorf("home directory is required")
	}

	store, err := newConfigStoreFn(homeDir)
	if err != nil {
		return nil, fmt.Errorf("create config store: %w", err)
	}
	cfg, err := store.Read()
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	mode, err := cfg.Mode()
	if err != nil {
		return nil, err
	}
	if mode != pcconfig.ModeCloud {
		return nil, errCloudNotConfigured
	}
	if err := validateCloudConfigFn(cfg); err != nil {
		return nil, fmt.Errorf("validate cloud config: %w", err)
	}

	awsCfg, err := loadAWSConfigFn(ctx, cfg.AWSProfile)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	awsCfg.Region = cfg.S3Region

	pool, err := newPGXPoolFn(ctx, cfg.NeonURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}

	// If userID is empty, resolve it from the config's API key.
	if userID == "" {
		userID, err = resolveCloudUserID(ctx, pool, cfg)
		if err != nil {
			closePGXPool(pool)
			return nil, err
		}
	}

	repo, err := newPostgresRepoFn(pool, userID)
	if err != nil {
		closePGXPool(pool)
		return nil, fmt.Errorf("create postgres repository: %w", err)
	}

	// Per-user S3 key namespace: "users/{userID}/" prefix on all keys.
	var s3KeyPrefix string
	if userID != "" {
		s3KeyPrefix = "users/" + userID + "/"
	}
	s3Client, err := newCloudS3ClientFn(newAWSSDKS3ClientFn(awsCfg, cfg.S3Endpoint, cfg.S3ForcePathStyle), cfg.S3Bucket, s3KeyPrefix)
	if err != nil {
		closePGXPool(pool)
		return nil, fmt.Errorf("create s3 client: %w", err)
	}

	return &cloudStack{
		Config: cfg,
		Store:  store,
		Repo:   repo,
		S3:     s3Client,
		pool:   pool,
	}, nil
}

var resolveUserIDFn = resolveUserIDFromAPIKey

// queryAPIKeyUserIDFn executes the API key lookup query against Postgres.
// Extracted as a function variable for unit-test injection (avoids requiring a live pool).
var queryAPIKeyUserIDFn = func(ctx context.Context, pool *pgxpool.Pool, keyHash string) (string, error) {
	var userID string
	err := pool.QueryRow(ctx,
		`UPDATE api_keys SET last_used_at = NOW()
		 WHERE key_hash = $1 AND revoked_at IS NULL
		 RETURNING user_id`,
		keyHash,
	).Scan(&userID)
	return userID, err
}

// resolveUserIDFromAPIKey hashes the raw API key and looks up the user_id in Postgres.
// Returns an error if the key is missing, invalid, or revoked.
func resolveUserIDFromAPIKey(ctx context.Context, pool *pgxpool.Pool, rawKey string) (string, error) {
	if strings.TrimSpace(rawKey) == "" {
		return "", fmt.Errorf("API key is required for cloud mode — generate one from the authenticated web app and run 'pc setup --api-key=<key>'")
	}

	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	userID, err := queryAPIKeyUserIDFn(ctx, pool, keyHash)
	if err != nil {
		return "", fmt.Errorf("API key validation failed (key may be invalid or revoked): %w", err)
	}
	return userID, nil
}

// resolveCloudUserID reads the API key from config and resolves the user_id via Postgres.
func resolveCloudUserID(ctx context.Context, pool *pgxpool.Pool, cfg pcconfig.Config) (string, error) {
	return resolveUserIDFn(ctx, pool, cfg.APIKey)
}

// loadAWSConfig loads AWS SDK configuration for the named shared-credentials profile.
func loadAWSConfig(ctx context.Context, profile string) (aws.Config, error) {
	if strings.TrimSpace(profile) == "" {
		return aws.Config{}, fmt.Errorf("profile is required")
	}
	credentialsPath, err := currentUserAWSCredentialsPath()
	if err != nil {
		return aws.Config{}, err
	}

	return awssdkconfig.LoadDefaultConfig(
		ctx,
		awssdkconfig.WithSharedConfigProfile(profile),
		awssdkconfig.WithSharedCredentialsFiles([]string{credentialsPath}),
	)
}

func closePGXPool(pool *pgxpool.Pool) {
	closePGXPoolFn(pool)
}
