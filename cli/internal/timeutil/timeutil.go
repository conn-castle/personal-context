package timeutil

import (
	"fmt"
	"time"
)

// UTCMillisFormat is the canonical wire format for sync-bearing timestamps:
// ISO 8601 UTC with millisecond precision and a `Z` suffix
// (e.g., "2026-05-10T20:01:39.000Z"). All sync cursors, API payloads, and
// SQLite timestamp columns share this format so cross-dialect comparisons
// stay byte-identical. Postgres microsecond timestamps truncate to
// millisecond when serialized through this format.
const UTCMillisFormat = "2006-01-02T15:04:05.000Z"

// FormatUTCMillis renders an instant in canonical UTC millisecond ISO 8601.
// Args: t is any instant.
// Returns: the formatted string with `Z` suffix.
func FormatUTCMillis(t time.Time) string {
	return t.UTC().Format(UTCMillisFormat)
}

// FormatUTCMillisPtr renders a nullable instant, returning nil for nil input.
// Args: t is a nullable instant.
// Returns: a pointer to the formatted string, or nil.
func FormatUTCMillisPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := FormatUTCMillis(*t)
	return &formatted
}

// LocalToUTC converts a local timestamp to UTC.
// Args: local is a timestamp with any timezone location.
// Returns: the equivalent UTC instant.
func LocalToUTC(local time.Time) time.Time {
	return local.UTC()
}

// UTCToLocal converts a UTC timestamp to the requested location.
// Args: utc is an instant; location is the target timezone.
// Returns: the equivalent local time or an error when location is nil.
func UTCToLocal(utc time.Time, location *time.Location) (time.Time, error) {
	if location == nil {
		return time.Time{}, fmt.Errorf("location is required")
	}
	return utc.In(location), nil
}

// TodayInLocation returns midnight for the current local day in a location.
// Args: referenceUTC is a UTC instant used to resolve "today"; location is the target timezone.
// Returns: local midnight for the day containing referenceUTC in location.
func TodayInLocation(referenceUTC time.Time, location *time.Location) (time.Time, error) {
	if location == nil {
		return time.Time{}, fmt.Errorf("location is required")
	}

	local := referenceUTC.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location), nil
}
