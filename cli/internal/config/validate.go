package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	// s3BucketRegexp matches valid S3 bucket names per AWS naming rules.
	s3BucketRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9.\-]{1,61}[a-z0-9]$`)
	// s3RegionRegexp matches AWS region identifiers (e.g., us-east-1, us-gov-west-1).
	s3RegionRegexp = regexp.MustCompile(`^[a-z]{2}(?:-[a-z0-9]+)+-\d+$`)
	// ipv4Regexp detects IP-address-style bucket names.
	ipv4Regexp = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)
)

// ValidateNeonURL checks that url is a valid postgres:// or postgresql:// connection string.
// Args: neonURL is the Neon database URL.
// Returns: nil when valid, or a descriptive error.
func ValidateNeonURL(neonURL string) error {
	if strings.TrimSpace(neonURL) == "" {
		return fmt.Errorf("neon URL is required")
	}

	u, err := url.Parse(neonURL)
	if err != nil {
		return fmt.Errorf("invalid neon URL: %w", err)
	}

	switch u.Scheme {
	case "postgres", "postgresql":
		// valid
	default:
		return fmt.Errorf("neon URL must use postgres:// or postgresql:// scheme, got %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("neon URL must include a host")
	}

	return nil
}

// ValidateS3Bucket checks that the bucket name follows S3 naming rules.
// Args: bucket is the S3 bucket name.
// Returns: nil when valid, or a descriptive error.
func ValidateS3Bucket(bucket string) error {
	if strings.TrimSpace(bucket) == "" {
		return fmt.Errorf("S3 bucket name is required")
	}
	if len(bucket) < 3 || len(bucket) > 63 {
		return fmt.Errorf("S3 bucket name must be 3–63 characters, got %d", len(bucket))
	}
	if !s3BucketRegexp.MatchString(bucket) {
		return fmt.Errorf("S3 bucket name %q contains invalid characters (lowercase letters, numbers, hyphens, and dots only)", bucket)
	}
	if strings.Contains(bucket, "..") {
		return fmt.Errorf("S3 bucket name must not contain consecutive dots")
	}
	// AWS rejects any name in IPv4 dotted-quad format regardless of octet
	// validity (e.g. 192.168.5.4 and 256.256.256.256 are both invalid), so the
	// format match alone must reject — do not narrow it with net.ParseIP.
	if ipv4Regexp.MatchString(bucket) {
		return fmt.Errorf("S3 bucket name must not be formatted as an IP address")
	}
	return nil
}

// ValidateS3Region checks that the region matches the AWS region format.
// Args: region is the AWS region identifier.
// Returns: nil when valid, or a descriptive error.
func ValidateS3Region(region string) error {
	if strings.TrimSpace(region) == "" {
		return fmt.Errorf("S3 region is required")
	}
	if !s3RegionRegexp.MatchString(region) {
		return fmt.Errorf("S3 region %q does not match expected format (e.g., us-east-1)", region)
	}
	return nil
}

// MaxGCRetentionDays bounds the retention window. The upper limit keeps the
// derived nanosecond duration (days * 24h) well within int64 range so it can
// never overflow into a negative threshold and trigger unintended deletion.
const MaxGCRetentionDays = 36500 // 100 years

// ValidateGCRetentionDays checks that a configured gc retention window is usable.
// Args: days is the configured retention in days, or nil when unset.
// Returns: nil when unset or a positive integer within MaxGCRetentionDays; an error otherwise.
func ValidateGCRetentionDays(days *int) error {
	if days == nil {
		return nil
	}
	if *days < 1 {
		return fmt.Errorf("gc retention days must be a positive integer, got %d", *days)
	}
	if *days > MaxGCRetentionDays {
		return fmt.Errorf("gc retention days must be at most %d, got %d", MaxGCRetentionDays, *days)
	}
	return nil
}

// ValidateCloudConfig validates all cloud configuration fields when cloud mode is active.
// For local-only mode (no cloud fields set), returns nil.
// Args: cfg is the configuration to validate.
// Returns: nil when valid, or the first validation error found.
func ValidateCloudConfig(cfg Config) error {
	mode, err := cfg.Mode()
	if err != nil {
		return err
	}
	if mode == ModeLocalOnly {
		return nil
	}

	if err := ValidateNeonURL(cfg.NeonURL); err != nil {
		return fmt.Errorf("neon_url: %w", err)
	}
	if err := ValidateS3Bucket(cfg.S3Bucket); err != nil {
		return fmt.Errorf("s3_bucket: %w", err)
	}
	if err := ValidateS3Region(cfg.S3Region); err != nil {
		return fmt.Errorf("s3_region: %w", err)
	}
	if strings.TrimSpace(cfg.AWSProfile) == "" {
		return fmt.Errorf("aws_profile is required for cloud mode")
	}
	// api_key is validated at use-time by resolveCloudUserID, not here.
	// Legacy configs may omit api_key until the user runs 'pc setup --api-key=...'.
	return nil
}
