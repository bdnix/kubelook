package httpapi

import (
	"context"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
)

var errNotRecorded = apperr.ErrNotRecorded

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
