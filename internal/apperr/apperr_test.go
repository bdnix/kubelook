package apperr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/apperr"
)

func TestSentinels_MatchThroughWrapping_AndAreDistinct(t *testing.T) {
	all := []error{
		apperr.ErrNotFound, apperr.ErrInvalid, apperr.ErrConflict, apperr.ErrForbidden,
		apperr.ErrUnauthenticated, apperr.ErrBusy, apperr.ErrTooLarge, apperr.ErrUnprocessable, apperr.ErrDryRunUnsupported,
	}
	for i, e := range all {
		wrapped := fmt.Errorf("doing x: %w", e)
		for j, other := range all {
			assert.Equal(t, i == j, errors.Is(wrapped, other), "%v vs %v", e, other)
		}
	}
}

func TestAPIError_ErrorText(t *testing.T) {
	err := &apperr.APIError{Status: 403, Code: "csrf", Message: "Missing CSRF token."}
	assert.Equal(t, "csrf: Missing CSRF token.", err.Error())
	var target *apperr.APIError
	assert.True(t, errors.As(fmt.Errorf("x: %w", err), &target))
}

func TestErrClusterUnavailable_SaysWhatToDoAndMatchesNotFound(t *testing.T) {
	assert.Contains(t, apperr.ErrClusterUnavailable.Error(), "upload that kubeconfig first")
	assert.ErrorIs(t, apperr.ErrClusterUnavailable, apperr.ErrNotFound)
	assert.NotErrorIs(t, apperr.ErrClusterUnavailable, apperr.ErrConflict)
}

func TestErrServerBusy_SaysTryAgainShortly_AndIsErrBusy(t *testing.T) {
	assert.Equal(t, "The server is busy. Try again shortly.", apperr.ErrServerBusy.Error())
	assert.ErrorIs(t, apperr.ErrServerBusy, apperr.ErrBusy)
	assert.ErrorIs(t, fmt.Errorf("opening: %w", apperr.ErrServerBusy), apperr.ErrBusy)
}
