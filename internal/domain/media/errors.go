package media

import "errors"

var (
	ErrNotFound  = errors.New("media not found")
	ErrNotFailed = errors.New("media is not in failed status")
	ErrDeleting  = errors.New("media is already deleting")
)
