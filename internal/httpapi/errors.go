package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/bdnix/kubelook/internal/apperr"
)

// Sentinel errors, shared with the other packages through apperr.
var (
	ErrNotFound        = apperr.ErrNotFound
	ErrInvalid         = apperr.ErrInvalid
	ErrConflict        = apperr.ErrConflict
	ErrForbidden       = apperr.ErrForbidden
	ErrUnauthenticated = apperr.ErrUnauthenticated
	ErrBusy            = apperr.ErrBusy
	ErrTooLarge        = apperr.ErrTooLarge
	ErrUnprocessable   = apperr.ErrUnprocessable
)

// maxBodyBytes bounds every JSON request body.
const maxBodyBytes = 1 << 20

// APIError is returned by handlers that need a specific status, code and user-facing message.
type APIError = apperr.APIError

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// classify maps an error to the response it produces. Unknown errors become a generic 500.
func classify(err error) (status int, code, message string, known bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status, apiErr.Code, apiErr.Message, true
	}
	table := []struct {
		target error
		status int
		code   string
	}{
		{apperr.ErrClusterUnavailable, http.StatusNotFound, "cluster_unavailable"}, // before ErrNotFound, which it also matches
		{apperr.ErrDryRunUnsupported, http.StatusUnprocessableEntity, "dry_run_unsupported"},
		{ErrNotFound, http.StatusNotFound, "not_found"},
		{ErrUnprocessable, http.StatusUnprocessableEntity, "unprocessable"},
		{ErrInvalid, http.StatusBadRequest, "bad_request"},
		{ErrConflict, http.StatusConflict, "conflict"},
		{ErrForbidden, http.StatusForbidden, "forbidden"},
		{ErrUnauthenticated, http.StatusUnauthorized, "unauthenticated"},
		{ErrBusy, http.StatusServiceUnavailable, "server_busy"},
		{ErrTooLarge, http.StatusRequestEntityTooLarge, "too_large"},
	}
	for _, e := range table {
		if errors.Is(err, e.target) {
			return e.status, e.code, err.Error(), true
		}
	}
	return http.StatusInternalServerError, "internal", "internal error", false
}

// writeError is the single place that turns an error into a response. The raw text of
// unknown errors is logged with the request ID and never sent to the client.
func writeError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	status, code, message, known := classify(err)
	if !known || status >= http.StatusInternalServerError {
		log.Error("request failed", "error", err.Error(), "requestId", w.Header().Get("X-Request-Id"), "method", r.Method)
	}
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal","message":"internal error"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// decodeJSON reads one JSON value of at most 1 MiB with unknown fields rejected.
// The returned error is ready for writeError.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return fmt.Errorf("request body exceeds %d bytes: %w", maxBodyBytes, ErrTooLarge)
		}
		return fmt.Errorf("invalid JSON body: %w: %w", ErrInvalid, err)
	}
	if dec.More() {
		return fmt.Errorf("request body holds more than one JSON value: %w", ErrInvalid)
	}
	return nil
}

// ErrorWriter returns a function that writes err as the standard JSON error response.
// Packages that cannot import httpapi (auth) receive it as a dependency.
func ErrorWriter(log *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) { writeError(w, r, log, err) }
}
