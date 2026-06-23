package s3client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const versionKey = "_version"

// Client wraps the AWS S3 SDK for figure and data file operations.
type Client struct {
	s3        *s3.Client
	bucket    string
	keyPrefix string // prepended to every S3 key (e.g. "users/{userId}/")
}

// New creates an S3 client for the given bucket with an optional key prefix.
// Args: s3Client is an initialized AWS S3 client; bucket is the S3 bucket name;
// keyPrefix is prepended to every key (use "" for no prefix).
// Returns: a configured client or an error when arguments are invalid.
func New(s3Client *s3.Client, bucket string, keyPrefix string) (*Client, error) {
	if s3Client == nil {
		return nil, fmt.Errorf("s3 client is required")
	}
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("bucket name is required")
	}
	return &Client{s3: s3Client, bucket: bucket, keyPrefix: keyPrefix}, nil
}

// prefixedKey returns the full S3 key with the client's prefix prepended.
func (c *Client) prefixedKey(key string) string {
	return c.keyPrefix + key
}

// Upload writes an object to S3.
// Args: key is the full S3 key; body provides the object content.
// Returns: nil on success or a descriptive error.
func (c *Client) Upload(ctx context.Context, key string, body io.Reader) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("key is required")
	}
	if body == nil {
		return fmt.Errorf("body is required")
	}

	fullKey := c.prefixedKey(key)
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(fullKey),
		Body:   body,
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, mapS3Error(err))
	}
	return nil
}

// Download retrieves an object from S3.
// Args: key is the full S3 key.
// Returns: the object body (caller must close) or an error.
func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("key is required")
	}

	fullKey := c.prefixedKey(key)
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(fullKey),
	})
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", key, mapS3Error(err))
	}
	return out.Body, nil
}

// Delete removes an object from S3.
// Args: key is the full S3 key.
// Returns: nil on success or a descriptive error.
func (c *Client) Delete(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("key is required")
	}

	fullKey := c.prefixedKey(key)
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(fullKey),
	})
	if err != nil {
		return fmt.Errorf("delete %s: %w", key, mapS3Error(err))
	}
	return nil
}

// Exists checks whether an object exists in S3.
// Args: key is the full S3 key.
// Returns: true if the object exists, false if not, or an error for other failures.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	if strings.TrimSpace(key) == "" {
		return false, fmt.Errorf("key is required")
	}

	fullKey := c.prefixedKey(key)
	_, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(fullKey),
	})
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("exists %s: %w", key, mapS3Error(err))
	}
	return true, nil
}

// versionPayload is the JSON structure stored in the _version S3 object.
type versionPayload struct {
	Version   int64  `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// versionReadPayload uses pointer fields to detect missing JSON keys during deserialization.
type versionReadPayload struct {
	Version   *int64  `json:"version"`
	UpdatedAt *string `json:"updated_at"`
}

// HeadVersion reads the _version object as JSON {"version": N, "updated_at": "ISO"}.
// Returns (0, "", nil) when the _version key does not exist (new/empty bucket).
// Backward-compatible: if JSON parse fails, tries plain text int64 (migration path).
func (c *Client) HeadVersion(ctx context.Context) (int64, string, error) {
	fullKey := c.prefixedKey(versionKey)
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(fullKey),
	})
	if err != nil {
		if isNotFoundError(err) {
			return 0, "", nil
		}
		return 0, "", fmt.Errorf("head version: %w", mapS3Error(err))
	}
	defer func() { _ = out.Body.Close() }()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return 0, "", fmt.Errorf("read version body: %w", err)
	}

	return parseVersionPayload(data)
}

// parseVersionPayload decodes the raw _version object body.
// It accepts the JSON form {"version": N, "updated_at": "ISO"} and a
// backward-compatible plain-text int64 (migration path), and rejects negative
// versions so the read path can never return a value UpdateVersion would refuse
// to write. Returns (version, updatedAt, error); updatedAt is "" for the
// plain-text form.
func parseVersionPayload(data []byte) (int64, string, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		// Detect and parse off the same trimmed body so branch selection and
		// decoding share one source of truth.
		var payload versionReadPayload
		if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
			return 0, "", fmt.Errorf("parse version object: %w", err)
		}
		if payload.Version == nil || payload.UpdatedAt == nil {
			return 0, "", fmt.Errorf("parse version object %q: version and updated_at are required", string(data))
		}
		if *payload.Version < 0 {
			return 0, "", fmt.Errorf("parse version object %q: version must be non-negative", string(data))
		}
		return *payload.Version, *payload.UpdatedAt, nil
	}

	// Backward-compatible fallback: plain text int64.
	v, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("parse version %q: not valid JSON or integer", string(data))
	}
	if v < 0 {
		return 0, "", fmt.Errorf("parse version %q: version must be non-negative", string(data))
	}
	return v, "", nil
}

// UpdateVersion writes the version as JSON {"version": N, "updated_at": "ISO"} to the _version object.
// Args: version must be non-negative; updatedAt is an ISO 8601 timestamp string.
// Returns: nil on success or a descriptive error.
func (c *Client) UpdateVersion(ctx context.Context, version int64, updatedAt string) error {
	if version < 0 {
		return fmt.Errorf("version must be non-negative, got %d", version)
	}
	if updatedAt == "" {
		return fmt.Errorf("updatedAt must not be empty")
	}

	payload := versionPayload{Version: version, UpdatedAt: updatedAt}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal version payload: %w", err)
	}

	fullKey := c.prefixedKey(versionKey)
	_, err = c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(fullKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("update version: %w", mapS3Error(err))
	}
	return nil
}

// mapS3Error converts AWS S3 errors to more descriptive wrapped errors.
func mapS3Error(err error) error {
	if err == nil {
		return nil
	}
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return fmt.Errorf("not found: %w", err)
	}
	var nsb *types.NoSuchBucket
	if errors.As(err, &nsb) {
		return fmt.Errorf("bucket not found: %w", err)
	}
	return err
}

// IsNotFound reports whether the error indicates a missing S3 object (key).
// It returns false for NoSuchBucket so that misconfigured buckets propagate
// as real errors instead of being silently treated as "key not found".
func IsNotFound(err error) bool { return isNotFoundError(err) }

// isNotFoundError checks if the error indicates the object does not exist.
// It returns false for NoSuchBucket errors so that misconfigured buckets
// propagate as real errors instead of being silently treated as "key not found".
func isNotFoundError(err error) bool {
	// Reject bucket-level 404s first — a missing bucket is not "key not found".
	var nsb *types.NoSuchBucket
	if errors.As(err, &nsb) {
		return false
	}

	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	// HeadObject returns a generic NotFound (HTTP 404) rather than NoSuchKey.
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	// Some S3-compatible services return a generic API error with 404 status.
	var re interface{ HTTPStatusCode() int }
	if errors.As(err, &re) && re.HTTPStatusCode() == 404 {
		return true
	}
	return false
}
