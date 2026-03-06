package slideid

import (
	"crypto/rand"
	"errors"
	"io"
	"regexp"
	"testing"
	"time"
)

type errReader struct{}

func (errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("boom")
}

type fixedReader struct {
	bytes []byte
	off   int
}

func (r *fixedReader) Read(p []byte) (int, error) {
	if r.off >= len(r.bytes) {
		return 0, io.EOF
	}
	n := copy(p, r.bytes[r.off:])
	r.off += n
	return n, nil
}

func TestGenerateForDateFormat(t *testing.T) {
	id, err := GenerateForDate(time.Date(2026, time.March, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateForDate() error = %v", err)
	}

	if ok, _ := regexp.MatchString(`^20260305-[0-9a-f]{8}$`, id); !ok {
		t.Fatalf("unexpected ID format: %q", id)
	}
}

func TestGenerateForDateUniqueness(t *testing.T) {
	seen := map[string]struct{}{}
	date := time.Date(2026, time.March, 5, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 500; i++ {
		id, err := GenerateForDate(date)
		if err != nil {
			t.Fatalf("GenerateForDate() error = %v", err)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate ID generated: %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestExtractDate(t *testing.T) {
	parsed, err := ExtractDate("20260305-a1b2c3d4")
	if err != nil {
		t.Fatalf("ExtractDate() error = %v", err)
	}

	expected := time.Date(2026, time.March, 5, 0, 0, 0, 0, time.UTC)
	if !parsed.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, parsed)
	}
}

func TestExtractDateRejectsInvalidID(t *testing.T) {
	if _, err := ExtractDate("invalid"); err == nil {
		t.Fatal("expected format validation error")
	}
	if _, err := ExtractDate("20260230-a1b2c3d4"); err == nil {
		t.Fatal("expected parse error for invalid calendar date")
	}
}

func TestGenerateForDateWithReaderUsesProvidedEntropy(t *testing.T) {
	reader := &fixedReader{bytes: []byte{0xde, 0xad, 0xbe, 0xef}}
	id, err := GenerateForDateWithReader(time.Date(2026, time.March, 5, 0, 0, 0, 0, time.UTC), reader)
	if err != nil {
		t.Fatalf("GenerateForDateWithReader() error = %v", err)
	}

	if id != "20260305-deadbeef" {
		t.Fatalf("expected deterministic suffix, got %q", id)
	}
}

func TestGenerateForDateWithReaderFailsLoudly(t *testing.T) {
	if _, err := GenerateForDateWithReader(time.Now(), nil); err == nil {
		t.Fatal("expected error for nil entropy reader")
	}
	if _, err := GenerateForDateWithReader(time.Now(), errReader{}); err == nil {
		t.Fatal("expected error for entropy read failure")
	}
}

func TestDefaultEntropySourceIsCryptoRand(t *testing.T) {
	if DefaultEntropySource() != rand.Reader {
		t.Fatal("default entropy source must be crypto/rand.Reader")
	}
}

func TestGenerateForDateNormalizesToUTC(t *testing.T) {
	// 2026-03-05T23:00:00 in EST (UTC-5) is 2026-03-06T04:00:00 in UTC.
	// Local date is March 5, UTC date is March 6. The ID must use the UTC date.
	est := time.FixedZone("EST", -5*60*60)
	localTime := time.Date(2026, time.March, 5, 23, 0, 0, 0, est)

	reader := &fixedReader{bytes: []byte{0xaa, 0xbb, 0xcc, 0xdd}}
	id, err := GenerateForDateWithReader(localTime, reader)
	if err != nil {
		t.Fatalf("GenerateForDateWithReader() error = %v", err)
	}

	// The UTC date is 2026-03-06, so the date prefix must be 20260306.
	if id != "20260306-aabbccdd" {
		t.Fatalf("expected UTC-normalized date prefix 20260306, got %q", id)
	}
}

func TestValidate(t *testing.T) {
	if !Validate("20260305-a1b2c3d4") {
		t.Fatal("expected valid ID to pass validation")
	}
	if Validate("20260305-zzzzzzzz") {
		t.Fatal("expected invalid ID to fail validation")
	}
}
