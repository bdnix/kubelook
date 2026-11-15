package jobs

import (
	"context"
	"time"
)

// Timing of the lease protocol. A replica that stops renewing loses its jobs to recovery within
// leaseDuration plus one scan period.
const (
	leaseDuration = 30 * time.Second
	renewEvery    = 10 * time.Second
	scanEvery     = 15 * time.Second
)

// renewAll extends the lease of every job this replica holds. A job whose lease cannot be extended (the row
// is gone, someone else took it, the store failed) is cancelled and nothing more is written for it.
func (r *Runner) renewAll(ctx context.Context) {
	now := r.d.Clock.Now()
	for _, h := range r.heldJobs() {
		ok, err := r.d.Store.Renew(ctx, h.id, r.d.ReplicaID, now.Add(leaseDuration), now)
		if err != nil || !ok {
			if err != nil {
				r.d.Log.Warn("renewing job lease", "job", h.id, "error", err.Error())
			}
			r.lose(h)
		}
	}
}

// steal takes over an expired job. Of several replicas scanning at once exactly one wins.
func (r *Runner) steal(ctx context.Context, id string, expiredAt time.Time) (claimedUntil time.Time, won bool) {
	now := r.d.Clock.Now()
	until := now.Add(leaseDuration)
	ok, err := r.d.Store.Steal(ctx, id, expiredAt, r.d.ReplicaID, until, now)
	if err != nil {
		r.d.Log.Warn("taking over job", "job", id, "error", err.Error())
		return time.Time{}, false
	}
	return until, ok
}
