package timeutil

import (
	"testing"
	"time"
)

func TestLocalToUTC(t *testing.T) {
	loc := time.FixedZone("UTC-5", -5*60*60)
	local := time.Date(2026, time.March, 5, 14, 30, 45, 123456000, loc)

	utc := LocalToUTC(local)
	expected := time.Date(2026, time.March, 5, 19, 30, 45, 123456000, time.UTC)
	if !utc.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, utc)
	}
	if utc.Location() != time.UTC {
		t.Fatalf("expected UTC location, got %v", utc.Location())
	}
}

func TestUTCToLocal(t *testing.T) {
	loc := time.FixedZone("UTC+9", 9*60*60)
	utc := time.Date(2026, time.March, 5, 1, 2, 3, 654321000, time.UTC)

	local, err := UTCToLocal(utc, loc)
	if err != nil {
		t.Fatalf("UTCToLocal() error = %v", err)
	}

	expected := time.Date(2026, time.March, 5, 10, 2, 3, 654321000, loc)
	if !local.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, local)
	}
}

func TestUTCToLocalFailsWithoutLocation(t *testing.T) {
	if _, err := UTCToLocal(time.Now().UTC(), nil); err == nil {
		t.Fatal("expected error for nil location")
	}
}

func TestTodayInLocationAcrossTimezones(t *testing.T) {
	reference := time.Date(2026, time.March, 5, 1, 30, 0, 0, time.UTC)

	ny := time.FixedZone("UTC-5", -5*60*60)
	tokyo := time.FixedZone("UTC+9", 9*60*60)

	nyToday, err := TodayInLocation(reference, ny)
	if err != nil {
		t.Fatalf("TodayInLocation(ny) error = %v", err)
	}
	if nyToday.Day() != 4 {
		t.Fatalf("expected NY local day to be 4, got %d", nyToday.Day())
	}

	tokyoToday, err := TodayInLocation(reference, tokyo)
	if err != nil {
		t.Fatalf("TodayInLocation(tokyo) error = %v", err)
	}
	if tokyoToday.Day() != 5 {
		t.Fatalf("expected Tokyo local day to be 5, got %d", tokyoToday.Day())
	}
}

func TestTodayInLocationFailsWithoutLocation(t *testing.T) {
	if _, err := TodayInLocation(time.Now().UTC(), nil); err == nil {
		t.Fatal("expected error for nil location")
	}
}

func TestMicrosecondRoundTrip(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	local := time.Date(2026, time.March, 5, 8, 9, 10, 987654000, loc)

	utc := LocalToUTC(local)
	roundTripped, err := UTCToLocal(utc, loc)
	if err != nil {
		t.Fatalf("UTCToLocal() error = %v", err)
	}

	if !roundTripped.Equal(local) {
		t.Fatalf("expected %v, got %v", local, roundTripped)
	}
	if roundTripped.Nanosecond()/1000 != local.Nanosecond()/1000 {
		t.Fatalf("microseconds changed: expected %d, got %d", local.Nanosecond()/1000, roundTripped.Nanosecond()/1000)
	}
}
