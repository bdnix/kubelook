package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
)

// Limits are the numbers of jobs of one type that run at once on one replica.
type Limits struct{ Helm, Other int }

const (
	defaultHelmLimit  = 5
	defaultOtherLimit = 2
	maxMessage        = 1000
)

// Deps are the Runner's dependencies.
type Deps struct {
	Store     *store.Jobs
	Clock     clock.Clock
	Sealer    *crypto.Sealer
	Log       *slog.Logger
	ReplicaID string
	NewID     func() string
	Limits    Limits // zero values → 5 for Helm job types, 2 for the others
	// Sessions tells which session hashes still exist (*store.Sessions).
	Sessions interface {
		Exists(ctx context.Context, hashes []string) ([]string, error)
	}
	// Context adds what handlers need to the job's context, such as the identity for audit records. Optional.
	Context func(ctx context.Context, j *Job) context.Context
	// OnPass is told after each renewal ("renew") and recovery ("scan") pass. Optional; tests use it to wait.
	OnPass func(pass string)
}

var (
	errLeaseLost = errors.New("jobs: lease lost")
	errClosed    = errors.New("jobs: runner closed")
	errPanic     = errors.New("jobs: handler panicked")
)

// held is a job this replica owns: waiting in its queue or running.
type held struct {
	id     string
	typ    string
	row    store.JobRow
	cancel context.CancelCauseFunc // nil until the job starts
	lost   bool
}

type typeState struct {
	running int
	queue   []*held
}

// Runner runs jobs, keeps their leases and classifies the jobs of dead replicas.
type Runner struct {
	d    Deps
	base context.Context
	stop context.CancelCauseFunc
	wg   sync.WaitGroup

	mu       sync.Mutex
	closed   bool
	handlers map[string]Handler
	types    map[string]*typeState
	held     map[string]*held
}

// NewRunner returns a Runner. Jobs start as soon as they are submitted; Run keeps leases and scans for orphans.
func NewRunner(d Deps) *Runner {
	if d.Limits.Helm <= 0 {
		d.Limits.Helm = defaultHelmLimit
	}
	if d.Limits.Other <= 0 {
		d.Limits.Other = defaultOtherLimit
	}
	base, stop := context.WithCancelCause(context.Background())
	return &Runner{d: d, base: base, stop: stop, handlers: map[string]Handler{}, types: map[string]*typeState{}, held: map[string]*held{}}
}

// Register adds the handler for its type.
func (r *Runner) Register(h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[h.Type()] = h
}

func (r *Runner) handler(typ string) (Handler, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.handlers[typ]
	return h, ok
}

func (r *Runner) limit(typ string) int {
	if strings.HasPrefix(typ, "helm") {
		return r.d.Limits.Helm
	}
	return r.d.Limits.Other
}

// Submit records the job, then queues it. The row exists before anything runs.
func (r *Runner) Submit(ctx context.Context, sess session.Session, cluster, typ string, target, params any) (string, error) {
	if _, ok := r.handler(typ); !ok {
		return "", fmt.Errorf("unknown job type %q: %w", typ, apperr.ErrInvalid)
	}
	tb, err := json.Marshal(target)
	if err != nil {
		return "", fmt.Errorf("encoding job target: %w", err)
	}
	pb, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("encoding job parameters: %w", err)
	}
	id := r.d.NewID()
	sealed, err := r.d.Sealer.Seal(pb, crypto.AAD(crypto.PurposeJob, id))
	if err != nil {
		return "", fmt.Errorf("sealing job parameters: %w", err)
	}
	now := r.d.Clock.Now()
	row := store.JobRow{
		ID: id, UserID: sess.UserID, SessionHash: sess.Hash, Cluster: cluster, Type: typ, Target: string(tb), SealedParams: sealed,
		Status: store.JobQueued, Progress: "{}", Offers: "[]", LeaseOwner: r.d.ReplicaID, LeaseExpiresAt: now.Add(leaseDuration),
		CreatedAt: now, UpdatedAt: now,
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", fmt.Errorf("the server is shutting down: %w", apperr.ErrBusy)
	}
	r.mu.Unlock()
	if err := r.d.Store.Insert(ctx, row); err != nil {
		return "", err
	}
	h := &held{id: id, typ: typ, row: row}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.held[id] = h
	ts := r.typeState(typ)
	ts.queue = append(ts.queue, h)
	r.dispatchLocked(typ)
	return id, nil
}

func (r *Runner) typeState(typ string) *typeState {
	ts := r.types[typ]
	if ts == nil {
		ts = &typeState{}
		r.types[typ] = ts
	}
	return ts
}

// dispatchLocked starts queued jobs of the type while the limit allows. Callers hold r.mu.
func (r *Runner) dispatchLocked(typ string) {
	ts := r.typeState(typ)
	for !r.closed && ts.running < r.limit(typ) && len(ts.queue) > 0 {
		h := ts.queue[0]
		ts.queue = ts.queue[1:]
		if h.lost {
			delete(r.held, h.id)
			continue
		}
		ts.running++
		r.wg.Add(1)
		go r.execute(h)
	}
}

func (r *Runner) release(h *held) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.held, h.id)
	r.typeState(h.typ).running--
	r.dispatchLocked(h.typ)
}

func (r *Runner) heldJobs() []*held {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*held, 0, len(r.held))
	for _, h := range r.held {
		out = append(out, h)
	}
	return out
}

func (r *Runner) isHeld(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.held[id]
	return ok
}

// lose marks a job as no longer ours: a running handler is cancelled, a queued job is skipped.
func (r *Runner) lose(h *held) {
	r.mu.Lock()
	h.lost = true
	cancel := h.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel(errLeaseLost)
	}
}

// execute runs one job to its end. It writes the outcome only if the job still belongs to this replica.
func (r *Runner) execute(h *held) {
	defer r.wg.Done()
	defer r.release(h)
	ctx, cancel := context.WithCancelCause(r.base)
	defer cancel(nil)
	r.mu.Lock()
	h.cancel = cancel
	lost := h.lost
	r.mu.Unlock()
	if lost {
		return
	}
	now := r.d.Clock.Now()
	started, err := r.d.Store.Start(ctx, h.id, r.d.ReplicaID, now.Add(leaseDuration), now)
	if err != nil || !started {
		if err != nil {
			r.d.Log.Warn("starting job", "job", h.id, "error", err.Error())
		}
		return
	}
	job, err := r.open(h.row)
	if err != nil {
		r.finish(ctx, h, Failed, "The job's parameters could not be read.", nil)
		return
	}
	job.Status = Running
	if r.d.Context != nil {
		ctx = r.d.Context(ctx, &job)
	}
	handler, _ := r.handler(h.typ)
	report := func(p Progress) {
		b, err := json.Marshal(p)
		if err != nil {
			return
		}
		if _, err := r.d.Store.SetProgress(ctx, h.id, r.d.ReplicaID, string(b), r.d.Clock.Now()); err != nil {
			r.d.Log.Warn("saving job progress", "job", h.id, "error", err.Error())
		}
	}
	runErr := r.call(ctx, handler, &job, report)
	if ctx.Err() != nil {
		return // lease lost or shutting down: recovery decides how this job ended
	}
	if runErr == nil {
		r.finish(ctx, h, Succeeded, "", nil)
		return
	}
	message := runErr.Error()
	if errors.Is(runErr, errPanic) {
		message = "The job crashed unexpectedly."
	}
	r.finish(ctx, h, Failed, message, offersOf(runErr))
}

// call runs the handler and turns a panic into a failure whose details are logged, not shown.
func (r *Runner) call(ctx context.Context, h Handler, j *Job, report func(Progress)) (err error) {
	defer func() {
		if p := recover(); p != nil {
			r.d.Log.Error("job handler panicked", "job", j.ID, "type", j.Type, "panic", fmt.Sprint(p))
			err = errPanic
		}
	}()
	return h.Run(ctx, j, report)
}

func (r *Runner) finish(ctx context.Context, h *held, status Status, message string, offers []string) {
	if offers == nil {
		offers = []string{}
	}
	ob, _ := json.Marshal(offers)
	if len(message) > maxMessage {
		message = message[:maxMessage]
	}
	ok, err := r.d.Store.Finish(context.WithoutCancel(ctx), h.id, r.d.ReplicaID, string(status), message, string(ob), r.d.Clock.Now())
	if err != nil {
		r.d.Log.Error("recording job outcome", "job", h.id, "error", err.Error())
	} else if !ok {
		r.d.Log.Warn("job outcome not recorded: the job no longer belongs to this replica", "job", h.id)
	}
}

// open turns a stored row into a Job with its parameters opened.
func (r *Runner) open(row store.JobRow) (Job, error) {
	j := view(row)
	pb, err := r.d.Sealer.Open(row.SealedParams, crypto.AAD(crypto.PurposeJob, row.ID))
	if err != nil {
		return j, fmt.Errorf("opening job parameters: %w", err)
	}
	j.Params = pb
	return j, nil
}

// view converts a stored row to a Job without parameters.
func view(row store.JobRow) Job {
	j := Job{
		ID: row.ID, UserID: row.UserID, SessionHash: row.SessionHash, Cluster: row.Cluster, Type: row.Type,
		Target: json.RawMessage(row.Target), Status: Status(row.Status), Message: row.Message,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, FinishedAt: row.FinishedAt, Offers: []string{},
	}
	_ = json.Unmarshal([]byte(row.Progress), &j.Progress)
	_ = json.Unmarshal([]byte(row.Offers), &j.Offers)
	if j.Offers == nil {
		j.Offers = []string{}
	}
	return j
}

// Wait returns when every job started by this runner has returned.
func (r *Runner) Wait() { r.wg.Wait() }

// Close cancels the running jobs and waits for their handlers. Their rows stay as they are: recovery
// on a running replica classifies them once their lease expires. Close may be called more than once.
func (r *Runner) Close() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.stop(errClosed)
	r.wg.Wait()
}

// Run keeps the leases of this replica's jobs and scans for the jobs of replicas that died, until ctx ends.
func (r *Runner) Run(ctx context.Context) {
	renew, scan := r.d.Clock.NewTicker(renewEvery), r.d.Clock.NewTicker(scanEvery)
	defer renew.Stop()
	defer scan.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-renew.C():
			r.renewAll(ctx)
			r.pass("renew")
		case <-scan.C():
			r.recover(ctx)
			r.pass("scan")
		}
	}
}

func (r *Runner) pass(name string) {
	if r.d.OnPass != nil {
		r.d.OnPass(name)
	}
}

// Get returns the user's job; other users' jobs do not exist for them.
func (r *Runner) Get(ctx context.Context, userID, id string) (Job, error) {
	row, err := r.d.Store.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && row.UserID != userID) {
		return Job{}, fmt.Errorf("no such job: %w", apperr.ErrNotFound)
	}
	if err != nil {
		return Job{}, err
	}
	return view(row), nil
}

// List returns the user's jobs, newest first, at most 100: all of them, or only the active (true) or finished (false) ones.
func (r *Runner) List(ctx context.Context, userID string, active *bool) ([]Job, error) {
	rows, err := r.d.Store.List(ctx, userID, active, 100)
	if err != nil {
		return nil, err
	}
	out := make([]Job, len(rows))
	for i, row := range rows {
		out[i] = view(row)
	}
	return out, nil
}

// Retry starts a new job with the type, target and parameters of a failed or interrupted job of the
// caller, under the caller's current session.
func (r *Runner) Retry(ctx context.Context, sess session.Session, id string) (string, error) {
	row, err := r.d.Store.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && row.UserID != sess.UserID) {
		return "", fmt.Errorf("no such job: %w", apperr.ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	if row.Status != store.JobFailed && row.Status != store.JobInterrupted {
		return "", fmt.Errorf("only a failed or interrupted job can be retried: %w", apperr.ErrConflict)
	}
	j, err := r.open(row)
	if err != nil {
		return "", fmt.Errorf("the job's parameters can no longer be read: %w", apperr.ErrConflict)
	}
	return r.Submit(ctx, sess, row.Cluster, row.Type, j.Target, j.Params)
}
