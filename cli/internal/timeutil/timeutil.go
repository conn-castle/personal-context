package timeutil

import (
	"fmt"
	"time"
)

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
