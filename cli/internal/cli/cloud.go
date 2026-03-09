package cli

import (
	"context"
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
	loadAWSConfigFn = loadAWSConfig
	newPGXPoolFn          = func(ctx context.Context, connectionString string) (*pgxpool.Pool, error) {
		return pgxpool.New(ctx, connectionString)
	}
	newPostgresRepoFn   = func(pool *pgxpool.Pool) (repository.Repository, error) { return postgresrepo.New(pool) }
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
	newCloudS3ClientFn = func(client *awss3.Client, bucket string) (*pcs3.Client, error) { return pcs3.New(client, bucket) }
	closePGXPoolFn      = func(pool *pgxpool.Pool) {
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
func openCloudStack(ctx context.Context, homeDir string) (*cloudStack, error) {
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

	repo, err := newPostgresRepoFn(pool)
	if err != nil {
		closePGXPool(pool)
		return nil, fmt.Errorf("create postgres repository: %w", err)
	}

	s3Client, err := newCloudS3ClientFn(newAWSSDKS3ClientFn(awsCfg, cfg.S3Endpoint, cfg.S3ForcePathStyle), cfg.S3Bucket)
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
