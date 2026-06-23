package s3client

import (
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

func TestMapS3ErrorNil(t *testing.T) {
	if mapS3Error(nil) != nil {
		t.Fatal("expected nil for nil error")
	}
}

func TestMapS3ErrorNoSuchKey(t *testing.T) {
	nsk := &types.NoSuchKey{Message: strPtr("not found")}
	err := mapS3Error(nsk)
	if err == nil {
		t.Fatal("expected non-nil error for NoSuchKey")
	}
	if !errors.As(err, new(*types.NoSuchKey)) {
		t.Fatal("expected wrapped NoSuchKey")
	}
}

func TestMapS3ErrorNoSuchBucket(t *testing.T) {
	nsb := &types.NoSuchBucket{Message: strPtr("missing bucket")}
	err := mapS3Error(nsb)
	if err == nil {
		t.Fatal("expected non-nil error for NoSuchBucket")
	}
	if !errors.As(err, new(*types.NoSuchBucket)) {
		t.Fatal("expected wrapped NoSuchBucket")
	}
}

func TestMapS3ErrorPassthrough(t *testing.T) {
	orig := errors.New("some other error")
	err := mapS3Error(orig)
	if !errors.Is(err, orig) {
		t.Fatalf("expected passthrough, got %v", err)
	}
}

func TestIsNotFoundErrorNoSuchKey(t *testing.T) {
	nsk := &types.NoSuchKey{Message: strPtr("not found")}
	if !isNotFoundError(nsk) {
		t.Fatal("expected true for NoSuchKey")
	}
}

func TestIsNotFoundErrorNotFoundType(t *testing.T) {
	nf := &types.NotFound{Message: strPtr("not found")}
	if !isNotFoundError(nf) {
		t.Fatal("expected true for NotFound")
	}
}

func TestIsNotFoundErrorHTTP404(t *testing.T) {
	err := &smithy.OperationError{
		ServiceID:     "S3",
		OperationName: "HeadObject",
		Err: &mockHTTP404Error{
			msg:  "head object 404",
			code: 404,
		},
	}
	if !isNotFoundError(err) {
		t.Fatal("expected true for HTTP 404 error")
	}
}

func TestIsNotFoundErrorNoSuchBucket(t *testing.T) {
	nsb := &types.NoSuchBucket{Message: strPtr("bucket gone")}
	if isNotFoundError(nsb) {
		t.Fatal("expected false for NoSuchBucket — missing bucket is not 'key not found'")
	}
}

func TestIsNotFoundErrorOtherError(t *testing.T) {
	err := errors.New("some error")
	if isNotFoundError(err) {
		t.Fatal("expected false for generic error")
	}
}

func TestIsNotFoundErrorHTTP500(t *testing.T) {
	err := &mockHTTP404Error{msg: "server error", code: 500}
	if isNotFoundError(err) {
		t.Fatal("expected false for HTTP 500 error")
	}
}

// mockHTTP404Error simulates an API error with an HTTP status code.
type mockHTTP404Error struct {
	msg  string
	code int
}

func (e *mockHTTP404Error) Error() string       { return e.msg }
func (e *mockHTTP404Error) HTTPStatusCode() int { return e.code }

func strPtr(s string) *string { return &s }

func TestNewRejectsNilClient(t *testing.T) {
	_, err := New(nil, "bucket", "")
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestNewRejectsEmptyBucket(t *testing.T) {
	// A non-nil *s3.Client can be constructed offline (no AWS round-trip),
	// so the empty/whitespace-bucket branch is exercisable without Docker.
	s3Client := s3.New(s3.Options{Region: "us-east-1"})

	if _, err := New(s3Client, "", ""); err == nil {
		t.Fatal("expected error for empty bucket")
	}
	if _, err := New(s3Client, "   ", ""); err == nil {
		t.Fatal("expected error for whitespace bucket")
	}
}

func TestNewNilCheckPrecedesBucketCheck(t *testing.T) {
	// With both a nil client and an empty bucket, the nil-client check must win,
	// so callers get the more fundamental error first.
	_, err := New(nil, "", "")
	if err == nil {
		t.Fatal("expected error for nil client + empty bucket")
	}
	if err.Error() != "s3 client is required" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestPrefixedKey(t *testing.T) {
	tests := []struct {
		name      string
		keyPrefix string
		key       string
		want      string
	}{
		{name: "empty prefix passes key through", keyPrefix: "", key: "figures/r1/plot.png", want: "figures/r1/plot.png"},
		{name: "tenant prefix is prepended", keyPrefix: "users/u-123/", key: "data/r1/big.bin", want: "users/u-123/data/r1/big.bin"},
		{name: "version key is prefixed too", keyPrefix: "users/u-123/", key: versionKey, want: "users/u-123/_version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{keyPrefix: tt.keyPrefix}
			if got := c.prefixedKey(tt.key); got != tt.want {
				t.Fatalf("prefixedKey(%q) with prefix %q = %q, want %q", tt.key, tt.keyPrefix, got, tt.want)
			}
		})
	}
}

func TestParseVersionPayloadJSON(t *testing.T) {
	v, updatedAt, err := parseVersionPayload([]byte(`{"version": 42, "updated_at": "2026-03-10T12:00:00Z"}`))
	if err != nil {
		t.Fatalf("parseVersionPayload() error = %v", err)
	}
	if v != 42 {
		t.Fatalf("version = %d, want 42", v)
	}
	if updatedAt != "2026-03-10T12:00:00Z" {
		t.Fatalf("updatedAt = %q, want 2026-03-10T12:00:00Z", updatedAt)
	}
}

func TestParseVersionPayloadJSONWithSurroundingWhitespace(t *testing.T) {
	// The "{" routing check keys off the trimmed body, so a leading newline must
	// still route to the JSON branch rather than the plain-text fallback. If the
	// routing used the raw bytes, this body would miss the "{" prefix, fall into
	// strconv.ParseInt, and fail as "not valid JSON or integer".
	v, updatedAt, err := parseVersionPayload([]byte("\n  {\"version\": 7, \"updated_at\": \"2026-03-10T13:00:00Z\"}\n"))
	if err != nil {
		t.Fatalf("parseVersionPayload() error = %v", err)
	}
	if v != 7 || updatedAt != "2026-03-10T13:00:00Z" {
		t.Fatalf("got (%d, %q), want (7, 2026-03-10T13:00:00Z)", v, updatedAt)
	}
}

func TestParseVersionPayloadPlainTextFallback(t *testing.T) {
	v, updatedAt, err := parseVersionPayload([]byte("  99\n"))
	if err != nil {
		t.Fatalf("parseVersionPayload() error = %v", err)
	}
	if v != 99 {
		t.Fatalf("version = %d, want 99", v)
	}
	if updatedAt != "" {
		t.Fatalf("updatedAt = %q, want empty for plain-text form", updatedAt)
	}
}

func TestParseVersionPayloadRejectsMissingJSONFields(t *testing.T) {
	_, _, err := parseVersionPayload([]byte(`{"foo": "bar"}`))
	if err == nil {
		t.Fatal("expected error for JSON missing version/updated_at")
	}
	if !strings.Contains(err.Error(), "version and updated_at are required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseVersionPayloadRejectsNegativeJSONVersion(t *testing.T) {
	// UpdateVersion refuses version < 0, so the read path must never surface a
	// negative version from a corrupted/legacy JSON _version object.
	_, _, err := parseVersionPayload([]byte(`{"version": -5, "updated_at": "2026-03-10T12:00:00Z"}`))
	if err == nil {
		t.Fatal("expected error for negative JSON version")
	}
	if !strings.Contains(err.Error(), "version must be non-negative") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseVersionPayloadRejectsNegativePlainTextVersion(t *testing.T) {
	// Same invariant for the plain-text migration path: a stored "-5" must be
	// rejected, not returned as a usable version.
	_, _, err := parseVersionPayload([]byte("-5"))
	if err == nil {
		t.Fatal("expected error for negative plain-text version")
	}
	if !strings.Contains(err.Error(), "version must be non-negative") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseVersionPayloadRejectsNonNumericContent(t *testing.T) {
	_, _, err := parseVersionPayload([]byte("not-a-number"))
	if err == nil {
		t.Fatal("expected error for non-numeric, non-JSON content")
	}
	if !strings.Contains(err.Error(), "not valid JSON or integer") {
		t.Fatalf("unexpected error: %v", err)
	}
}
