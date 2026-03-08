package syncengine

import "errors"

var (
	// ErrSyncLocked indicates another sync is already holding the lock file.
	ErrSyncLocked = errors.New("syncengine: sync already in progress")
)
