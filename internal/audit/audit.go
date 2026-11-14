// Package audit records who did what. Every record goes to the database and to
// standard output as one JSON line; the recorder only ever inserts.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/store"
)

// Errors returned by the recorder.
var (
	ErrNotRecorded     = errors.New("audit: record could not be stored")
	ErrForbiddenDetail = errors.New("audit: details must not carry secrets or content")
)

// forbiddenDetailKeys guard against accidentally logging secrets (compared case-insensitively).
var forbiddenDetailKeys = []string{"token", "password", "secret", "content", "value"}

// Identity says who acts. Fields are empty before sign-in completes.
type Identity struct {
	AccountProvider, AccountSubject, AccountEmail string
	VaultEntityID, VaultDisplayName               string
	KubeContext, KubeUser                         string
}

// Event is what happened.
type Event struct {
	Cluster, Namespace, Verb, ResourceKind, ResourceName, Outcome string
	Details                                                       map[string]any
}

type identityKey struct{}

// WithIdentity attaches the acting identity to ctx.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFrom returns the identity attached by WithIdentity, or the zero Identity.
func IdentityFrom(ctx context.Context) Identity {
	id, _ := ctx.Value(identityKey{}).(Identity)
	return id
}

// UserIdentity is the identity of a signed-in user before any Vault identity is known.
func UserIdentity(u store.User) Identity {
	return Identity{AccountProvider: u.Provider, AccountSubject: u.Subject, AccountEmail: u.Email}
}

// line is the JSON written to standard output for every record.
type line struct {
	Time      string         `json:"time"`
	Audit     bool           `json:"audit"`
	Recorded  bool           `json:"recorded"`
	Account   map[string]any `json:"account"`
	Vault     map[string]any `json:"vault"`
	Kube      map[string]any `json:"kube"`
	Cluster   string         `json:"cluster"`
	Namespace string         `json:"namespace"`
	Verb      string         `json:"verb"`
	Resource  map[string]any `json:"resource"`
	Outcome   string         `json:"outcome"`
	Details   map[string]any `json:"details"`
}

// Recorder writes audit records.
type Recorder struct {
	db    *store.DB
	clock clock.Clock
	newID func() string

	mu  sync.Mutex // keeps stdout lines from interleaving
	out io.Writer
}

// NewRecorder returns a Recorder that stores to db and writes JSON lines to out.
func NewRecorder(db *store.DB, c clock.Clock, out io.Writer, newID func() string) *Recorder {
	return &Recorder{db: db, clock: c, out: out, newID: newID}
}

// Record stores one event. If the database insert fails the line is still written
// (with "recorded": false) and the error wraps ErrNotRecorded.
func (r *Recorder) Record(ctx context.Context, ev Event) error {
	for k := range ev.Details {
		if slicesContainsFold(forbiddenDetailKeys, k) {
			return fmt.Errorf("%w: key %q", ErrForbiddenDetail, k)
		}
	}
	details := ev.Details
	if details == nil {
		details = map[string]any{}
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encoding audit details: %w", err)
	}
	id, now := IdentityFrom(ctx), r.clock.Now()

	_, dbErr := r.db.Exec(ctx, `INSERT INTO audit_events (id, time, account_provider, account_subject, account_email,
vault_entity_id, vault_display_name, kube_context, kube_user, cluster, namespace, verb, resource_kind, resource_name, outcome, details)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.newID(), now.UnixMilli(), id.AccountProvider, id.AccountSubject, id.AccountEmail,
		id.VaultEntityID, id.VaultDisplayName, id.KubeContext, id.KubeUser,
		ev.Cluster, ev.Namespace, ev.Verb, ev.ResourceKind, ev.ResourceName, ev.Outcome, string(detailsJSON))

	r.write(line{
		Time: now.UTC().Format(time.RFC3339Nano), Audit: true, Recorded: dbErr == nil,
		Account:   map[string]any{"provider": id.AccountProvider, "subject": id.AccountSubject, "email": id.AccountEmail},
		Vault:     map[string]any{"entityId": id.VaultEntityID, "displayName": id.VaultDisplayName},
		Kube:      map[string]any{"context": id.KubeContext, "user": id.KubeUser},
		Cluster:   ev.Cluster,
		Namespace: ev.Namespace,
		Verb:      ev.Verb,
		Resource:  map[string]any{"kind": ev.ResourceKind, "name": ev.ResourceName},
		Outcome:   ev.Outcome,
		Details:   details,
	})
	if dbErr != nil {
		return fmt.Errorf("%w: %w", ErrNotRecorded, dbErr)
	}
	return nil
}

func (r *Recorder) write(l line) {
	b, err := json.Marshal(l)
	if err != nil {
		return // cannot happen: every field is a string, bool or map of encodable details
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.out.Write(append(b, '\n'))
}

// Pending is an action that has been recorded as requested but not yet finished.
type Pending struct {
	r         *Recorder
	ev        Event
	requestID string
}

// Begin records the event with outcome "requested" and a fresh details.requestId.
// If it returns an error the caller must not perform the action.
func (r *Recorder) Begin(ctx context.Context, ev Event) (*Pending, error) {
	requestID := r.newID()
	ev.Details = withRequestID(ev.Details, requestID)
	ev.Outcome = "requested"
	if err := r.Record(ctx, ev); err != nil {
		return nil, err
	}
	return &Pending{r: r, ev: ev, requestID: requestID}, nil
}

// Annotate adds a detail that is known only after the action ran, such as a size. It is recorded with the
// final outcome.
func (p *Pending) Annotate(key string, value any) {
	p.ev.Details = maps.Clone(p.ev.Details)
	p.ev.Details[key] = value
}

// Done records the final outcome with the same requestId. A failure to store it is
// already visible as a stdout line with "recorded": false.
func (p *Pending) Done(ctx context.Context, actionErr error) {
	ev := p.ev
	ev.Details = maps.Clone(p.ev.Details)
	ev.Outcome = "success"
	if actionErr != nil {
		ev.Outcome = "failure"
		ev.Details["error"] = actionErr.Error()
	}
	_ = p.r.Record(ctx, ev)
}

func withRequestID(details map[string]any, id string) map[string]any {
	out := maps.Clone(details)
	if out == nil {
		out = map[string]any{}
	}
	out["requestId"] = id
	return out
}

func slicesContainsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
