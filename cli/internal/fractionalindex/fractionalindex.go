package fractionalindex

import "roci.dev/fracdex"

// GenerateBetween returns a key that sorts lexicographically between before and after.
// Args: before or after may be empty to indicate unbounded lower/upper range.
// Returns: a valid fractional index key or an error on invalid input/range.
func GenerateBetween(before string, after string) (string, error) {
	return fracdex.KeyBetween(before, after)
}

// GenerateAtStart returns a key that sorts before the provided first key.
// Args: first is the current first key; empty means no upper bound.
// Returns: a new key that sorts first.
func GenerateAtStart(first string) (string, error) {
	return fracdex.KeyBetween("", first)
}

// GenerateAtEnd returns a key that sorts after the provided last key.
// Args: last is the current last key; empty means no lower bound.
// Returns: a new key that sorts last.
func GenerateAtEnd(last string) (string, error) {
	return fracdex.KeyBetween(last, "")
}

// Validate reports whether key is a valid fractional index key.
// Args: key is the candidate key.
// Returns: nil when the key is valid.
func Validate(key string) error {
	_, err := fracdex.Float64Approx(key)
	return err
}
