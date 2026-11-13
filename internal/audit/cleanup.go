package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/store"
)

// Cleanup deletes audit records older than retentionDays and returns how many it deleted.
// Zero retention keeps everything.
func Cleanup(ctx context.Context, db *store.DB, c clock.Clock, retentionDays int) (int64, error) {
	if retentionDays < 0 {
		return 0, fmt.Errorf("retention days must not be negative, got %d", retentionDays)
	}
	if retentionDays == 0 {
		return 0, nil
	}
	cutoff := c.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	res, err := db.Exec(ctx, "DELETE FROM audit_events WHERE time < ?", cutoff.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("deleting old audit records: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("counting deleted audit records: %w", err)
	}
	return n, nil
}
