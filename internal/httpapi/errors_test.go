package httpapi_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/httpapi"
)

func TestErrorWriter_WritesTheStandardErrorBody(t *testing.T) {
	rec := httptest.NewRecorder()
	httpapi.ErrorWriter(slog.New(slog.DiscardHandler))(rec, httptest.NewRequest(http.MethodGet, "/x", nil), httpapi.ErrNotFound)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "not_found", errorCode(t, rec))
}

func TestErrorWriter_ClusterUnavailable_404WithItsOwnCodeAndMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	httpapi.ErrorWriter(slog.New(slog.DiscardHandler))(rec, httptest.NewRequest(http.MethodGet, "/x", nil), apperr.ErrClusterUnavailable)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "cluster_unavailable", errorCode(t, rec))
	assert.Contains(t, rec.Body.String(), "This cluster isn't available in your session.")
}
