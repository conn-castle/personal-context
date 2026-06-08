package repository

import (
	"fmt"
	"strings"
)

// ValidateRecordAssetKey verifies that a child asset key is the canonical
// relative path derived from its owning record and filename.
func ValidateRecordAssetKey(kind string, recordID string, filename string, s3Key string) error {
	expected, err := CanonicalRecordAssetKey(kind, recordID, filename)
	if err != nil {
		return err
	}
	if strings.TrimSpace(s3Key) == "" {
		return fmt.Errorf("%w: s3_key is required", ErrInvalidArgument)
	}
	if s3Key != strings.TrimSpace(s3Key) {
		return fmt.Errorf("%w: s3_key must not contain surrounding whitespace", ErrInvalidArgument)
	}
	if strings.HasPrefix(s3Key, "/") || strings.Contains(s3Key, "\\") {
		return fmt.Errorf("%w: s3_key must be a forward-slash relative path", ErrInvalidArgument)
	}
	parts := strings.Split(s3Key, "/")
	if len(parts) != 3 {
		return fmt.Errorf("%w: s3_key must be %s/{record_id}/{filename}", ErrInvalidArgument, kind)
	}
	if s3Key != expected {
		return fmt.Errorf("%w: s3_key %q must equal %q", ErrInvalidArgument, s3Key, expected)
	}
	return nil
}

// CanonicalRecordAssetKey returns the canonical relative key for a child asset.
func CanonicalRecordAssetKey(kind string, recordID string, filename string) (string, error) {
	if kind != "figures" && kind != "data" {
		return "", fmt.Errorf("%w: unsupported asset key kind %q", ErrInvalidArgument, kind)
	}
	if err := validateAssetPathSegment("record id", recordID); err != nil {
		return "", err
	}
	if err := validateAssetPathSegment("filename", filename); err != nil {
		return "", err
	}
	return kind + "/" + recordID + "/" + filename, nil
}

func validateAssetPathSegment(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidArgument, field)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%w: %s must not contain surrounding whitespace", ErrInvalidArgument, field)
	}
	if value == "." || value == ".." || strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return fmt.Errorf("%w: %s must be one path segment", ErrInvalidArgument, field)
	}
	return nil
}
