// Package listpage provides the canonical Go JSON envelope for list/search pages.
package listpage

import (
	"encoding/json"
	"io"
)

// Response is the shared JSON shape for paginated list and search responses.
type Response[T any] struct {
	Items      []T     `json:"items"`
	Total      int     `json:"total"`
	NextCursor *string `json:"next_cursor"`
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
