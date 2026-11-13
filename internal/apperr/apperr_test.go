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
		apperr.ErrUnauthenticated, apperr.ErrBusy, apperr.ErrTooLarge,
	}
	for i, e := range all {
		wrapped := fmt.Errorf("doing x: %w", e)
		for j, other := range all {
			assert.Equal(t, i == j, errors.Is(wrapped, other), "%v vs %v", e, other)
		}
	}
}
