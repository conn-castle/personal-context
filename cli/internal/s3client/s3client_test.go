//go:build integration

package s3client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"
	"github.com/testcontainers/testcontainers-go/wait"
)

var sharedMinio struct {
	endpoint string
	username string
	password string
}

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, err := minio.Run(ctx,
		"minio/minio:RELEASE.2024-01-16T16-07-38Z",
		minio.WithUsername("minioadmin"),
		minio.WithPassword("minioadmin"),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/minio/health/live").
				WithPort("9000/tcp").
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		panic(fmt.Sprintf("start minio container: %v", err))
	}

	endpoint, err := container.ConnectionString(ctx)
	if err != nil {
		panic(fmt.Sprintf("get minio connection string: %v", err))
	}
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = "http://" + endpoint
	}
	sharedMinio.endpoint = endpoint
	sharedMinio.username = container.Username
	sharedMinio.password = container.Password

	exitCode := m.Run()

	_ = container.Terminate(ctx)
	if exitCode != 0 {
		panic("tests failed")
	}
}

var bucketCounter int

func newTestClient(t *testing.T) *Client {
	t.Helper()

	ctx := context.Background()
	bucketCounter++
	bucketName := fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), bucketCounter)

	s3Client := s3.New(s3.Options{
		BaseEndpoint: aws.String(sharedMinio.endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(sharedMinio.username, sharedMinio.password, ""),
		Region:       "us-east-1",
		UsePathStyle: true,
	})

	if _, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}); err != nil {
		t.Fatalf("CreateBucket(%s) error = %v", bucketName, err)
	}

	client, err := New(s3Client, bucketName)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Cleanup(func() {
		// Best-effort cleanup: list and delete all objects, then delete bucket.
		listOut, _ := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucketName),
		})
		if listOut != nil {
			for _, obj := range listOut.Contents {
				_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(bucketName),
					Key:    obj.Key,
				})
			}
		}
		_, _ = s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
	})

	return client
}

func TestUploadDownloadRoundTrip(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	payload := []byte("figure-bytes-for-round-trip-test")
	key := "figures/20260305-a1b2c3d4/plot.png"

	if err := client.Upload(ctx, key, bytes.NewReader(payload)); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	rc, err := client.Download(ctx, key)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Download() content mismatch: got %q, want %q", got, payload)
	}
}

func TestUploadDownloadLargePayload(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	// ~1MB payload
	payload := make([]byte, 1024*1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	key := "data/20260305-a1b2c3d4/large.bin"

	if err := client.Upload(ctx, key, bytes.NewReader(payload)); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	rc, err := client.Download(ctx, key)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Download() large payload mismatch: got %d bytes, want %d bytes", len(got), len(payload))
	}
}

func TestDeleteThenVerifyGone(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	key := "figures/20260305-a1b2c3d4/to-delete.png"
	if err := client.Upload(ctx, key, bytes.NewReader([]byte("delete-me"))); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	exists, err := client.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists() after upload error = %v", err)
	}
	if !exists {
		t.Fatal("Exists() should be true after upload")
	}

	if err := client.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	exists, err = client.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists() after delete error = %v", err)
	}
	if exists {
		t.Fatal("Exists() should be false after delete")
	}
}

func TestExistsReturnsTrueForExistingObject(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	key := "figures/20260305-a1b2c3d4/present.png"
	if err := client.Upload(ctx, key, bytes.NewReader([]byte("exists"))); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	exists, err := client.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !exists {
		t.Fatal("Exists() should be true for existing object")
	}
}

func TestExistsReturnsFalseForMissingObject(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	exists, err := client.Exists(ctx, "figures/20260305-a1b2c3d4/nonexistent.png")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if exists {
		t.Fatal("Exists() should be false for missing object")
	}
}

func TestHeadVersionUpdateVersionRoundTrip(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	// Fresh bucket should return 0.
	v, err := client.HeadVersion(ctx)
	if err != nil {
		t.Fatalf("HeadVersion() on fresh bucket error = %v", err)
	}
	if v != 0 {
		t.Fatalf("HeadVersion() on fresh bucket = %d, want 0", v)
	}

	if err := client.UpdateVersion(ctx, 42); err != nil {
		t.Fatalf("UpdateVersion(42) error = %v", err)
	}
	v, err = client.HeadVersion(ctx)
	if err != nil {
		t.Fatalf("HeadVersion() error = %v", err)
	}
	if v != 42 {
		t.Fatalf("HeadVersion() = %d, want 42", v)
	}

	if err := client.UpdateVersion(ctx, 43); err != nil {
		t.Fatalf("UpdateVersion(43) error = %v", err)
	}
	v, err = client.HeadVersion(ctx)
	if err != nil {
		t.Fatalf("HeadVersion() error = %v", err)
	}
	if v != 43 {
		t.Fatalf("HeadVersion() = %d, want 43", v)
	}
}

func TestUploadToNonexistentBucketFails(t *testing.T) {
	s3Client := s3.New(s3.Options{
		BaseEndpoint: aws.String(sharedMinio.endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(sharedMinio.username, sharedMinio.password, ""),
		Region:       "us-east-1",
		UsePathStyle: true,
	})

	client, err := New(s3Client, "nonexistent-bucket-12345")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = client.Upload(context.Background(), "test/key", bytes.NewReader([]byte("data")))
	if err == nil {
		t.Fatal("expected error for upload to nonexistent bucket")
	}
}

func TestDownloadNonexistentKeyFails(t *testing.T) {
	client := newTestClient(t)

	_, err := client.Download(context.Background(), "nonexistent/key")
	if err == nil {
		t.Fatal("expected error for download of nonexistent key")
	}
}

func TestNewClientRejectsInvalidArguments(t *testing.T) {
	s3Client := s3.New(s3.Options{Region: "us-east-1"})

	if _, err := New(nil, "bucket"); err == nil {
		t.Fatal("expected error for nil s3 client")
	}
	if _, err := New(s3Client, ""); err == nil {
		t.Fatal("expected error for empty bucket")
	}
	if _, err := New(s3Client, "  "); err == nil {
		t.Fatal("expected error for whitespace bucket")
	}
}

func TestUploadRejectsEmptyKey(t *testing.T) {
	client := newTestClient(t)

	if err := client.Upload(context.Background(), "", bytes.NewReader([]byte("data"))); err == nil {
		t.Fatal("expected error for empty key")
	}
	if err := client.Upload(context.Background(), "  ", bytes.NewReader([]byte("data"))); err == nil {
		t.Fatal("expected error for whitespace key")
	}
}

func TestDownloadRejectsEmptyKey(t *testing.T) {
	client := newTestClient(t)

	if _, err := client.Download(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestDeleteRejectsEmptyKey(t *testing.T) {
	client := newTestClient(t)

	if err := client.Delete(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestExistsRejectsEmptyKey(t *testing.T) {
	client := newTestClient(t)

	if _, err := client.Exists(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestUploadRejectsNilBody(t *testing.T) {
	client := newTestClient(t)

	if err := client.Upload(context.Background(), "some/key", nil); err == nil {
		t.Fatal("expected error for nil body")
	}
}

func TestUpdateVersionRejectsNegative(t *testing.T) {
	client := newTestClient(t)

	if err := client.UpdateVersion(context.Background(), -1); err == nil {
		t.Fatal("expected error for negative version")
	}
}

func TestHeadVersionInvalidContent(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	// Write non-numeric content to the _version key.
	if err := client.Upload(ctx, "_version", bytes.NewReader([]byte("not-a-number"))); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	_, err := client.HeadVersion(ctx)
	if err == nil {
		t.Fatal("expected error for non-numeric version content")
	}
}

func TestErrorPathsWithUnreachableEndpoint(t *testing.T) {
	// Create a client pointing to a non-existent endpoint to trigger error paths.
	s3Client := s3.New(s3.Options{
		BaseEndpoint: aws.String("http://localhost:1"),
		Credentials:  credentials.NewStaticCredentialsProvider("x", "x", ""),
		Region:       "us-east-1",
		UsePathStyle: true,
	})

	client, err := New(s3Client, "any-bucket")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	if err := client.Delete(ctx, "some/key"); err == nil {
		t.Fatal("expected error for Delete on unreachable endpoint")
	}
	if _, err := client.Exists(ctx, "some/key"); err == nil {
		t.Fatal("expected error for Exists on unreachable endpoint")
	}
	if _, err := client.HeadVersion(ctx); err == nil {
		t.Fatal("expected error for HeadVersion on unreachable endpoint")
	}
	if err := client.UpdateVersion(ctx, 1); err == nil {
		t.Fatal("expected error for UpdateVersion on unreachable endpoint")
	}
	if _, err := client.Download(ctx, "some/key"); err == nil {
		t.Fatal("expected error for Download on unreachable endpoint")
	}
}
