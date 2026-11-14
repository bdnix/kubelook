package httpapi_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/httpapi"
)

func TestErrorWriter_WritesTheStandardErrorBody(t *testing.T) {
	rec := httptest.NewRecorder()
	httpapi.ErrorWriter(slog.New(slog.DiscardHandler))(rec, httptest.NewRequest(http.MethodGet, "/x", nil), httpapi.ErrNotFound)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "not_found", errorCode(t, rec))
}
