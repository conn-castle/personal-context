package s3client

import (
	"errors"
	"testing"

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

func (e *mockHTTP404Error) Error() string        { return e.msg }
func (e *mockHTTP404Error) HTTPStatusCode() int   { return e.code }

func strPtr(s string) *string { return &s }

func TestNewRejectsNilClient(t *testing.T) {
	_, err := New(nil, "bucket", "")
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestNewRejectsEmptyBucket(t *testing.T) {
	// Cannot easily construct a real *s3.Client without aws config,
	// but we can test nil + empty bucket separately.
	_, err := New(nil, "", "")
	if err == nil {
		t.Fatal("expected error for nil client + empty bucket")
	}
	// The nil check runs first, so error is about the client.
	if err.Error() != "s3 client is required" {
		t.Fatalf("unexpected error message: %v", err)
	}
}
