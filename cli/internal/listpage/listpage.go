// Package listpage provides the canonical Go JSON envelope and cursor
// encoding shared by `pc list`, `pc search`, `pc serve` REST handlers, and the
// Next.js record-list route. Keeping the shape in one place is what lets the
// contract-parity tests succeed: a cursor minted by `pc serve` must decode
// the same way in `pc list`, and vice versa.
package listpage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Response is the shared JSON shape for paginated list and search responses.
type Response[T any] struct {
	Items      []T     `json:"items"`
	Total      int     `json:"total"`
	NextCursor *string `json:"next_cursor"`
}

// Cursor is the canonical record-list cursor payload.
// Sort order is date DESC, day_order ASC, id ASC.
type Cursor struct {
	Date     string `json:"date"`
	DayOrder string `json:"day_order"`
	ID       string `json:"id"`
}

// ErrCursorEncoding is returned when a cursor string is not valid base64.
var ErrCursorEncoding = errors.New("invalid cursor encoding")

// ErrCursorFormat is returned when a cursor decodes but is not valid JSON or
// is missing required fields.
var ErrCursorFormat = errors.New("invalid cursor format")

// EncodeCursor renders a cursor as base64-encoded compact JSON.
func EncodeCursor(c Cursor) string {
	data, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeCursor parses a base64-encoded cursor string. Empty input returns a
// nil cursor with no error so callers can pass the raw query parameter
// directly without checking emptiness themselves.
func DecodeCursor(raw string) (*Cursor, error) {
	if raw == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, ErrCursorEncoding
	}
	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, ErrCursorFormat
	}
	if c.Date == "" || c.DayOrder == "" || c.ID == "" {
		return nil, fmt.Errorf("%w: incomplete cursor", ErrCursorFormat)
	}
	return &c, nil
}

// IsAfterCursor reports whether (date, dayOrder, id) sorts strictly after the
// cursor under the canonical (date DESC, day_order ASC, id ASC) order.
// Useful for in-memory pagination when the underlying store doesn't have
// native cursor support.
func IsAfterCursor(date string, dayOrder string, id string, cursor Cursor) bool {
	if date != cursor.Date {
		return date < cursor.Date
	}
	if dayOrder != cursor.DayOrder {
		return dayOrder > cursor.DayOrder
	}
	return id > cursor.ID
}

// WriteJSON writes resp as indented JSON.
func WriteJSON[T any](w io.Writer, resp Response[T]) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

// MarshalIndent returns resp encoded as indented JSON bytes.
func MarshalIndent[T any](resp Response[T]) ([]byte, error) {
	return json.MarshalIndent(resp, "", "  ")
}
