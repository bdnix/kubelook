package httpapi

import (
	"context"
	"net/http"

	"github.com/bdnix/kubelook/internal/audit"
)

// errNotRecorded is the answer when the audit record of an action could not be written.
var errNotRecorded = &APIError{
	Status: http.StatusInternalServerError, Code: "internal",
	Message: "The action was refused because it could not be recorded.",
}

// audited records ev as requested, runs fn, then records the outcome. When the first record cannot
// be written fn is not run: no record, no action. Every later mutating handler goes through it.
func (d Deps) audited(ctx context.Context, ev audit.Event, fn func() error) error {
	p, err := d.Audit.Begin(ctx, ev)
	if err != nil {
		d.Log.Error("audit record could not be written", "verb", ev.Verb, "error", err.Error())
		return errNotRecorded
	}
	err = fn()
	p.Done(context.WithoutCancel(ctx), err)
	return err
}
