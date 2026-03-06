package notes

// FromString converts a string to a pointer value.
// Args: value is a source string.
// Returns: a pointer to the provided string.
func FromString(value string) *string {
	copied := value
	return &copied
}
