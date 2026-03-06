package repository

import "errors"

var (
	// ErrNotFound indicates the requested record does not exist.
	ErrNotFound = errors.New("repository: not found")
	// ErrConflict indicates a uniqueness or duplicate-key conflict.
	ErrConflict = errors.New("repository: conflict")
	// ErrForeignKeyViolation indicates a parent-child relationship constraint failure.
	ErrForeignKeyViolation = errors.New("repository: foreign key violation")
	// ErrInvalidArgument indicates caller-provided input is missing or malformed.
	ErrInvalidArgument = errors.New("repository: invalid argument")
)
