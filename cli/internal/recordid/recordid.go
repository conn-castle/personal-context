package recordid

import (
	"crypto/rand"
	"fmt"
	"io"
	"regexp"
	"time"
)

const dateLayout = "20060102"

var (
	idPattern               = regexp.MustCompile(`^\d{8}-[0-9a-f]{8}$`)
	entropySource io.Reader = rand.Reader
)

// GenerateForDate returns a record ID in the format {YYYYMMDD}-{8-random-hex}.
// Args: date supplies the prefix; it is normalized to UTC before formatting, so
// the prefix is the UTC calendar day (which may differ from a local-time date by
// a day near midnight).
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
