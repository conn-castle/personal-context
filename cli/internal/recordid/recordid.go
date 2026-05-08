package recordid

import (
	"crypto/rand"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const dateLayout = "20060102"

var (
	idPattern               = regexp.MustCompile(`^\d{8}-[0-9a-f]{8}$`)
	entropySource io.Reader = rand.Reader
)

// GenerateForDate returns a record ID in the format {YYYYMMDD}-{8-random-hex}.
// Args: date is the local date component that must be embedded in the ID.
// Returns: a formatted record ID or an error when random bytes cannot be read.
func GenerateForDate(date time.Time) (string, error) {
	return GenerateForDateWithReader(date, entropySource)
}

// GenerateForDateWithReader returns a record ID using the provided entropy reader.
// Args: date is the date component; reader supplies 4 random bytes for the suffix.
// Returns: a formatted record ID or an error for invalid input or entropy read failure.
func GenerateForDateWithReader(date time.Time, reader io.Reader) (string, error) {
	if reader == nil {
		return "", fmt.Errorf("entropy reader is required")
	}

	var suffix [4]byte
	if _, err := io.ReadFull(reader, suffix[:]); err != nil {
		return "", fmt.Errorf("read entropy: %w", err)
	}

	return fmt.Sprintf("%s-%x", date.UTC().Format(dateLayout), suffix), nil
}

// ExtractDate parses the YYYYMMDD prefix from a record ID.
// Args: id is a record ID in canonical format.
// Returns: the parsed UTC date at midnight or an error when format is invalid.
func ExtractDate(id string) (time.Time, error) {
	if !idPattern.MatchString(id) {
		return time.Time{}, fmt.Errorf("invalid record ID: %q", id)
	}

	parts := strings.SplitN(id, "-", 2)
	parsed, err := time.ParseInLocation(dateLayout, parts[0], time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse record ID date: %w", err)
	}

	return parsed, nil
}

// Validate reports whether an ID matches the canonical record ID format.
// Args: id is a candidate record ID.
// Returns: true when the ID matches {YYYYMMDD}-{8-random-hex}.
func Validate(id string) bool {
	return idPattern.MatchString(id)
}

// DefaultEntropySource returns the configured default entropy source.
// Args: none.
// Returns: the reader used by GenerateForDate.
func DefaultEntropySource() io.Reader {
	return entropySource
}
