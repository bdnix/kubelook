package store

import "github.com/bdnix/kubelook/internal/apperr"

// Errors returned by the stores; they match the shared sentinels with errors.Is.
var (
	ErrNotFound = apperr.ErrNotFound
	ErrInvalid  = apperr.ErrInvalid
)
