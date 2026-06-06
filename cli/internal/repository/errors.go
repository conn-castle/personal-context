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
	// ErrUnsupportedSearchOperator indicates the search query contains an
	// all-uppercase boolean-style operator token (OR, AND, NOT, NEAR) that
	// Personal Context's implicit-AND search does not support.
	ErrUnsupportedSearchOperator = errors.New("repository: unsupported search operator")
)
