package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bdnix/kubelook/internal/store"
)

const (
	msgNotStarted  = "The server stopped before this job started."
	msgSessionGone = "The job's session ended, so its result could not be checked."
	msgUnreadable  = "The job's parameters could not be read, so its result could not be checked."
	msgNoHandler   = "This server cannot check the result of this kind of job."
	msgRecoverFail = "The result of this job could not be checked."
)

// recover classifies the jobs whose owner stopped renewing. A job is classified once, by whichever
// replica wins the claim, and is never run again.
func (r *Runner) recover(ctx context.Context) {
	rows, err := r.d.Store.Expired(ctx, r.d.Clock.Now())
	if err != nil {
		r.d.Log.Warn("scanning for orphaned jobs", "error", err.Error())
		return
	}
	for _, row := range rows {
		if r.isHeld(row.ID) {
			continue // ours and still alive here: its renewal decides
		}
		claimed, won := r.steal(ctx, row.ID, row.LeaseExpiresAt)
		if !won {
			continue
		}
		status, message, offers := r.classify(ctx, row)
		if offers == nil {
			offers = []string{}
		}
		ob, _ := json.Marshal(offers)
		if len(message) > maxMessage {
			message = message[:maxMessage]
		}
		ok, err := r.d.Store.Classify(context.WithoutCancel(ctx), row.ID, r.d.ReplicaID, claimed, string(status), message, string(ob), r.d.Clock.Now())
		if err != nil || !ok {
			r.d.Log.Warn("classifying orphaned job", "job", row.ID, "stored", ok, "error", fmt.Sprint(err))
		}
	}
}

func (r *Runner) classify(ctx context.Context, row store.JobRow) (status Status, message string, offers []string) {
	if row.Status == store.JobQueued {
		return Interrupted, msgNotStarted, []string{OfferRetry}
	}
	live, err := r.d.Sessions.Exists(ctx, []string{row.SessionHash})
	if err == nil && len(live) == 0 {
		return Interrupted, msgSessionGone, []string{OfferRetry}
	}
	h, ok := r.handler(row.Type)
	if !ok {
		return Interrupted, msgNoHandler, nil
	}
	job, err := r.open(row)
	if err != nil {
		return Interrupted, msgUnreadable, nil
	}
	if r.d.Context != nil {
		ctx = r.d.Context(ctx, &job)
	}
	return r.callRecover(ctx, h, &job)
}

func (r *Runner) callRecover(ctx context.Context, h Handler, j *Job) (status Status, message string, offers []string) {
	defer func() {
		if p := recover(); p != nil {
			r.d.Log.Error("job recovery panicked", "job", j.ID, "type", j.Type, "panic", fmt.Sprint(p))
			status, message, offers = Interrupted, msgRecoverFail, nil
		}
	}()
	status, message, offers = h.Recover(ctx, j)
	switch status {
	case Succeeded, Failed, Interrupted:
	default:
		return Interrupted, msgRecoverFail, nil
	}
	return status, message, offers
}
