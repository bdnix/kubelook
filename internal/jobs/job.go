// Package jobs runs long actions as durable jobs: the job is recorded before it starts, its owner keeps
// a short lease on it, and another replica classifies it if the owner dies. A job is never re-run
// automatically.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/bdnix/kubelook/internal/session"
)

// Status is where a job is in its life.
type Status string

// The statuses of a job. Queued and running jobs are "active".
const (
	Queued      Status = "queued"
	Running     Status = "running"
	Succeeded   Status = "succeeded"
	Failed      Status = "failed"
	Interrupted Status = "interrupted"
)

// Offers a finished job may carry; the UI shows one button per offer.
const (
	OfferRetry     = "retry"
	OfferRollback  = "rollback"
	OfferUninstall = "uninstall"
)

// ItemResult is the outcome for one item of a job that works through a list.
type ItemResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // done | failed | skipped
	Message string `json:"message,omitempty"`
}

// Progress is what a running job reports.
type Progress struct {
	Done  int          `json:"done"`
	Total int          `json:"total"`
	Text  string       `json:"text"`
	Items []ItemResult `json:"items,omitempty"`
}

// Job is a job as handlers and the API see it. Params are the opened parameters; they never reach the API.
type Job struct {
	ID          string          `json:"id"`
	UserID      string          `json:"-"`
	SessionHash string          `json:"-"`
	Cluster     string          `json:"cluster"`
	Type        string          `json:"type"`
	Target      json.RawMessage `json:"target"`
	Params      json.RawMessage `json:"-"`
	Status      Status          `json:"status"`
	Progress    Progress        `json:"progress"`
	Message     string          `json:"message"`
	Offers      []string        `json:"offers"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	FinishedAt  time.Time       `json:"finishedAt,omitzero"`
}

// Session is the session the job runs under: its hash finds the user's cluster credentials.
func (j *Job) Session() session.Session {
	return session.Session{Hash: j.SessionHash, UserID: j.UserID}
}

// Handler runs one type of job.
type Handler interface {
	Type() string
	// Run does the work. It must stop when ctx ends; its error becomes the job's message.
	Run(ctx context.Context, j *Job, report func(Progress)) error
	// Recover decides, after the owning replica died, how a running job ended. It returns the status
	// (succeeded, failed or interrupted), a message and the offers. It must not redo the work.
	Recover(ctx context.Context, j *Job) (Status, string, []string)
}

type offerError struct {
	err    error
	offers []string
}

func (e *offerError) Error() string { return e.err.Error() }
func (e *offerError) Unwrap() error { return e.err }

// WithOffers makes a failed job offer these buttons instead of the default (retry).
func WithOffers(err error, offers ...string) error {
	if err == nil {
		return nil
	}
	return &offerError{err: err, offers: offers}
}

func offersOf(err error) []string {
	var oe *offerError
	if errors.As(err, &oe) {
		return oe.offers
	}
	return []string{OfferRetry}
}
