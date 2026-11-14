package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/crypto"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/vault"
	"github.com/bdnix/kubelook/internal/version"
)

// kindPendingCopy is a sealed browser copy that the browser has not collected yet (a rotated OIDC refresh token).
const kindPendingCopy = "pending_copy"

const refresherPeriod = 30 * time.Second

// StateOpen is the state of a cluster whose credentials are in use.
const StateOpen = "open"

// Errors of opening a cluster. Each carries the text the user sees.
var (
	errNoAPIConfig = &apperr.APIError{Status: http.StatusUnprocessableEntity, Code: "unprocessable",
		Message: "This cluster's Vault mount has no API server address or CA configured, so Kubelook can't open it."}
	errVaultDown = &apperr.APIError{Status: http.StatusServiceUnavailable, Code: "vault_unreachable",
		Message: "Vault is unreachable, so this cluster can't be opened right now"}
	errVaultTokenGone = &apperr.APIError{Status: http.StatusUnauthorized, Code: "unauthenticated",
		Message: "Vault no longer accepts your token. Paste a new Vault token in Settings to reconnect this cluster."}
)

// OpenState is what opening or inspecting a cluster reports.
type OpenState struct {
	State               string     `json:"state"`
	Server              string     `json:"server,omitempty"`
	Role                string     `json:"role,omitempty"`
	CredentialExpiresAt *time.Time `json:"credentialExpiresAt,omitempty"`
}

// PendingCopy is a sealed browser copy waiting to be collected.
type PendingCopy struct {
	Label  string `json:"label"`
	Sealed string `json:"sealed"`
}

// OpenerDeps are the dependencies of Opener.
type OpenerDeps struct {
	Registry    *Registry
	Credentials *Credentials
	Sessions    *store.Sessions
	Sealer      *crypto.Sealer
	Vault       vault.API
	Clock       clock.Clock
	Config      *config.Config
	Audit       *audit.Recorder
	Closers     *session.Registry
	Log         *slog.Logger
	HTTPClient  *http.Client // OIDC token endpoint calls; nil → a client with a 15 s timeout
}

// source is where the credentials of a cluster come from.
type source interface {
	// open establishes the cluster and returns its first state.
	open(ctx context.Context, sess session.Session, ref Ref, role string) (*entry, error)
	// token returns a valid bearer token ("" when the cluster authenticates some other way).
	token(ctx context.Context, e *entry) (string, error)
	// restConfig returns the connection settings, without the token.
	restConfig(e *entry) (*rest.Config, error)
	// due reports whether the credential should be renewed by the background refresher.
	due(e *entry) bool
}

// entry is one open cluster of one session on this replica. Fields are guarded by Opener.mu.
type entry struct {
	sess     session.Session
	ref      Ref
	src      source
	state    string
	role     string
	server   string
	expires  time.Time
	lastUsed time.Time
	vcred    vaultClusterCred // vault
	kube     *kubeState       // kubeconfig
}

// Opener turns a cluster ID into working Kubernetes credentials.
type Opener struct {
	d      OpenerDeps
	vault  source
	kube   source
	mu     sync.Mutex
	open   map[string]*entry
	flight flights
}

// NewOpener returns the opener.
func NewOpener(d OpenerDeps) *Opener {
	if d.HTTPClient == nil {
		d.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	o := &Opener{d: d, open: map[string]*entry{}}
	o.vault = &vaultSource{o: o}
	o.kube = &kubeSource{o: o}
	return o
}

func entryKey(hash, id string) string { return hash + "\x00" + id }

func (o *Opener) sourceFor(ref Ref) source {
	if ref.Source == SourceVault {
		return o.vault
	}
	return o.kube
}

// snapshot describes e. Callers hold o.mu.
func (o *Opener) snapshot(e *entry) OpenState {
	st := OpenState{State: e.state, Server: e.server, Role: e.role}
	if !e.expires.IsZero() {
		exp := e.expires
		st.CredentialExpiresAt = &exp
	}
	return st
}

// Open opens the cluster for the session. role matters for Vault clusters only: empty means the
// remembered role or the cluster's default.
func (o *Opener) Open(ctx context.Context, sess session.Session, clusterID, role string) (OpenState, error) {
	ref, err := o.d.Registry.Resolve(ctx, sess, clusterID)
	if err != nil {
		return OpenState{}, err
	}
	src := o.sourceFor(ref)
	e, err := src.open(ctx, sess, ref, role)
	if err != nil {
		return OpenState{}, err
	}
	e.src, e.sess, e.ref, e.state = src, sess, ref, StateOpen
	e.lastUsed = o.d.Clock.Now()

	key := entryKey(sess.Hash, clusterID)
	o.mu.Lock()
	_, existed := o.open[key]
	o.open[key] = e
	st := o.snapshot(e)
	o.mu.Unlock()
	if !existed {
		// The closer drops the in-memory state when the session ends or the credential is removed.
		o.d.Closers.Register(sess.Hash, clusterID, func() error { o.drop(key); return nil })
	}
	return st, nil
}

func (o *Opener) drop(key string) {
	o.mu.Lock()
	delete(o.open, key)
	o.mu.Unlock()
}

// lookup returns the open entry; ok is false when this replica has none.
func (o *Opener) lookup(hash, id string) (*entry, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.open[entryKey(hash, id)]
	return e, ok
}

// ensure returns the open entry, opening the cluster with the remembered role when needed.
func (o *Opener) ensure(ctx context.Context, sess session.Session, id string) (*entry, error) {
	if e, ok := o.lookup(sess.Hash, id); ok && o.stateOf(e) == StateOpen {
		return e, nil
	}
	if _, err := o.Open(ctx, sess, id, ""); err != nil {
		return nil, err
	}
	e, _ := o.lookup(sess.Hash, id)
	return e, nil
}

func (o *Opener) stateOf(e *entry) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return e.state
}

// Token returns a valid bearer token for the cluster, re-minting or refreshing it first when it is
// about to expire. Concurrent callers share one upstream call.
func (o *Opener) Token(ctx context.Context, sessHash, clusterID string) (string, error) {
	e, ok := o.lookup(sessHash, clusterID)
	if !ok {
		row, err := o.d.Sessions.Get(ctx, sessHash)
		if err != nil {
			return "", apperr.ErrClusterUnavailable
		}
		sess := session.Session{Hash: sessHash, UserID: row.UserID, CSRFToken: row.CSRFToken, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}
		if e, err = o.ensure(ctx, sess, clusterID); err != nil {
			return "", err
		}
	}
	o.mu.Lock()
	e.lastUsed = o.d.Clock.Now()
	o.mu.Unlock()
	return e.src.token(ctx, e)
}

// RESTConfig returns client settings for the cluster; the bearer token is attached per request.
func (o *Opener) RESTConfig(ctx context.Context, sess session.Session, clusterID string) (*rest.Config, error) {
	e, err := o.ensure(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	cfg, err := e.src.restConfig(e)
	if err != nil {
		return nil, err
	}
	cfg.QPS, cfg.Burst, cfg.Timeout = 50, 100, 0
	cfg.UserAgent = "kubelook/" + version.Version
	hash := sess.Hash
	cfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			tok, err := o.Token(req.Context(), hash, clusterID)
			if err != nil {
				return nil, err
			}
			if tok == "" {
				return rt.RoundTrip(req)
			}
			r := req.Clone(req.Context())
			r.Header.Set("Authorization", "Bearer "+tok)
			return rt.RoundTrip(r)
		})
	})
	return cfg, nil
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// State reports the state of the cluster for the session.
func (o *Opener) State(ctx context.Context, sess session.Session, clusterID string) OpenState {
	if e, ok := o.lookup(sess.Hash, clusterID); ok {
		o.mu.Lock()
		defer o.mu.Unlock()
		return o.snapshot(e)
	}
	if _, err := o.d.Sessions.GetCredential(ctx, sess.Hash, kindClusterCred, clusterID); err == nil {
		return OpenState{State: StateOpen}
	}
	return OpenState{State: StateClosed}
}

// StateOf is State reduced to the state name, for the cluster list.
func (o *Opener) StateOf(ctx context.Context, sess session.Session, clusterID string) string {
	return o.State(ctx, sess, clusterID).State
}

func (o *Opener) setDisconnected(e *entry) {
	o.mu.Lock()
	e.state = StateDisconnected
	o.mu.Unlock()
}

// RunRefresher renews, every 30 s, the credentials of the clusters this replica used within the
// session idle timeout and that are about to expire. It blocks until ctx is done.
func (o *Opener) RunRefresher(ctx context.Context) {
	t := o.d.Clock.NewTicker(refresherPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C():
			o.refreshDue(ctx)
		}
	}
}

func (o *Opener) refreshDue(ctx context.Context) {
	idle := o.d.Config.Session.IdleTimeout.D()
	now := o.d.Clock.Now()
	o.mu.Lock()
	all := make([]*entry, 0, len(o.open))
	for _, e := range o.open {
		if e.state == StateOpen && now.Sub(e.lastUsed) < idle {
			all = append(all, e)
		}
	}
	o.mu.Unlock()
	var due []*entry
	for _, e := range all {
		if e.src.due(e) { // takes o.mu itself
			due = append(due, e)
		}
	}
	slices.SortFunc(due, func(a, b *entry) int {
		return strings.Compare(entryKey(a.sess.Hash, a.ref.ID), entryKey(b.sess.Hash, b.ref.ID))
	})
	for _, e := range due {
		if _, err := e.src.token(ctx, e); err != nil {
			o.d.Log.Warn("renewing cluster credential", "cluster", e.ref.ID, "error", err.Error())
		}
	}
}

// Pending returns the sealed browser copies the browser has not collected yet.
func (o *Opener) Pending(ctx context.Context, sess session.Session) ([]PendingCopy, error) {
	rows, err := o.d.Sessions.ListCredentials(ctx, sess.Hash, kindPendingCopy)
	if err != nil {
		return nil, err
	}
	out := make([]PendingCopy, len(rows))
	for i, r := range rows {
		out[i] = PendingCopy{Label: r.Ref, Sealed: r.Sealed}
	}
	return out, nil
}

// AckPending forgets the pending copies with these labels.
func (o *Opener) AckPending(ctx context.Context, sess session.Session, labels []string) error {
	for _, l := range labels {
		if err := o.d.Sessions.DeleteCredential(ctx, sess.Hash, kindPendingCopy, l); err != nil {
			return err
		}
	}
	return nil
}

// flights is a minimal singleflight: callers with the same key share one call.
type flights struct {
	mu    sync.Mutex
	calls map[string]*call
}

type call struct {
	done chan struct{}
	err  error
}

func (f *flights) do(key string, fn func() error) error {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]*call{}
	}
	if c, ok := f.calls[key]; ok {
		f.mu.Unlock()
		<-c.done
		return c.err
	}
	c := &call{done: make(chan struct{})}
	f.calls[key] = c
	f.mu.Unlock()

	c.err = fn()
	f.mu.Lock()
	delete(f.calls, key)
	f.mu.Unlock()
	close(c.done)
	return c.err
}

func (o *Opener) auditIssued(ctx context.Context, id, mountPath, role, lease string) {
	err := o.d.Audit.Record(ctx, audit.Event{
		Cluster: id, Verb: audit.VerbVaultCredentialIssued, Outcome: "success",
		Details: map[string]any{"path": mountPath, "leaseId": lease, "role": role},
	})
	if err != nil {
		o.d.Log.Warn("recording credential issue", "error", err.Error())
	}
}

func (o *Opener) sealSession(sess session.Session, plaintext []byte) (string, error) {
	sealed, err := o.d.Sealer.Seal(plaintext, crypto.AAD(crypto.PurposeSessionCredential, sess.Hash))
	if err != nil {
		return "", fmt.Errorf("sealing cluster credential: %w", err)
	}
	return sealed, nil
}
