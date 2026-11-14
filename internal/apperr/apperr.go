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
	// ErrUnprocessable is a request the API server understood but refused to apply (validation, admission).
	ErrUnprocessable = errors.New("unprocessable")
	// ErrDryRunUnsupported is returned when the object's kind cannot be previewed with a dry run.
	ErrDryRunUnsupported = errors.New("dry run unsupported")
)

// APIError is an error that carries its own HTTP status, machine code and user-facing message.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// ErrClusterUnavailable is answered for any cluster ID that is not in the caller's session: a
// cluster of another user's kubeconfig and one that does not exist look the same. It matches
// ErrNotFound with errors.Is, and the HTTP layer answers it with its own code.
var ErrClusterUnavailable error = clusterUnavailable{}

type clusterUnavailable struct{}

func (clusterUnavailable) Error() string {
	return "This cluster isn't available in your session. If it comes from a kubeconfig, upload that kubeconfig first."
}

func (clusterUnavailable) Is(target error) bool { return target == ErrNotFound }

// ErrNotRecorded is the answer when the audit record of an action could not be written: no record, no action.
var ErrNotRecorded = &APIError{
	Status: 500, Code: "internal",
	Message: "The action was refused because it could not be recorded.",
}
