package store

import "errors"

var (
	ErrInvalidPath   = errors.New("invalid path")
	ErrNotFound      = errors.New("not found")
	ErrExists        = errors.New("already exists")
	ErrNotDir        = errors.New("not a directory")
	ErrIsDir         = errors.New("is a directory")
	ErrRootOperation = errors.New("operation not allowed on the share root")
	ErrQuotaExceeded = errors.New("storage quota exceeded")
	ErrDiskLow       = errors.New("server storage is low")
	ErrTooLarge      = errors.New("file is too large")
)
