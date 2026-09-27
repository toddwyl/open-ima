package document

import "errors"

var (
	ErrNotFound  = errors.New("document not found")
	ErrNotFailed = errors.New("document is not in failed status")
	ErrDeleting  = errors.New("document is already deleting")
)
