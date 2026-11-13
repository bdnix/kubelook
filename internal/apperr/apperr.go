// Package apperr holds the sentinel errors shared by the store, the services and the HTTP layer.
package apperr

import "errors"

// Sentinels. Handlers map them to HTTP statuses with a single function.
var (
	ErrNotFound        = errors.New("not found")
	ErrInvalid         = errors.New("invalid input")
	ErrConflict        = errors.New("conflict")
	ErrForbidden       = errors.New("forbidden")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrBusy            = errors.New("server busy")
	ErrTooLarge        = errors.New("too large")
)
