// Package session creates, loads and ends browser sessions and protects requests with them.
package session

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/store"
)

const (
	cookieName      = "kubelook_session"
	touchInterval   = time.Minute
	watchdogPeriod  = 5 * time.Second
	sweepPeriod     = time.Minute
	secretByteCount = 32
)

// Session is the part of a stored session that request handlers see.
type Session struct {
	Hash, UserID, CSRFToken string
	CreatedAt, ExpiresAt    time.Time
}

// Deps are the Manager's dependencies.
type Deps struct {
	Store                    *store.Sessions
	Clock                    clock.Clock
	Rand                     io.Reader
	Log                      *slog.Logger
	IdleTimeout, MaxLifetime time.Duration
	CookieSecure             bool
}

// Manager owns the session lifecycle.
type Manager struct {
	d        Deps
	registry *Registry
}

// NewManager returns a Manager with an empty registry.
func NewManager(d Deps) *Manager { return &Manager{d: d, registry: NewRegistry()} }

// Registry returns the per-replica registry of things to close when a session ends.
func (m *Manager) Registry() *Registry { return m.registry }

// Hash is the form in which a session ID is stored: hex(sha256(id)).
func Hash(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func (m *Manager) secret() (string, error) {
	b := make([]byte, secretByteCount)
	if _, err := io.ReadFull(m.d.Rand, b); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Create starts a session for the user and returns the plain session ID (for the cookie).
func (m *Manager) Create(ctx context.Context, userID string) (id string, s Session, err error) {
	if id, err = m.secret(); err != nil {
		return "", Session{}, err
	}
	csrf, err := m.secret()
	if err != nil {
		return "", Session{}, err
	}
	now := m.d.Clock.Now()
	s = Session{Hash: Hash(id), UserID: userID, CSRFToken: csrf, CreatedAt: now, ExpiresAt: now.Add(m.d.MaxLifetime)}
	if err := m.d.Store.Create(ctx, s.Hash, userID, csrf, now, s.ExpiresAt); err != nil {
		return "", Session{}, err
	}
	return id, s, nil
}

// Load returns the session for the request's cookie. Expired or idle sessions are ended.
// last_seen_at is refreshed at most once a minute.
func (m *Manager) Load(ctx context.Context, r *http.Request) (Session, error) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return Session{}, apperr.ErrUnauthenticated
	}
	hash := Hash(c.Value)
	row, err := m.d.Store.Get(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		return Session{}, apperr.ErrUnauthenticated
	}
	if err != nil {
		return Session{}, err
	}
	now := m.d.Clock.Now()
	if !now.Before(row.ExpiresAt) || !now.Before(row.LastSeenAt.Add(m.d.IdleTimeout)) {
		if err := m.End(ctx, hash); err != nil {
			m.d.Log.Warn("ending expired session", "error", err.Error())
		}
		return Session{}, apperr.ErrUnauthenticated
	}
	if now.Sub(row.LastSeenAt) >= touchInterval {
		if err := m.d.Store.Touch(ctx, hash, now); err != nil && !errors.Is(err, store.ErrNotFound) {
			return Session{}, err
		}
	}
	return Session{Hash: hash, UserID: row.UserID, CSRFToken: row.CSRFToken, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}, nil
}

// KeepAlive counts an open stream as activity (A12). Failures are logged, not returned.
func (m *Manager) KeepAlive(ctx context.Context, hash string) {
	if err := m.d.Store.Touch(ctx, hash, m.d.Clock.Now()); err != nil && !errors.Is(err, store.ErrNotFound) {
		m.d.Log.Warn("keeping session alive", "error", err.Error())
	}
}

// End deletes the session and its credentials and closes everything registered for it on
// this replica. The closers run even if the delete fails; all errors are joined.
func (m *Manager) End(ctx context.Context, hash string) error {
	delErr := m.d.Store.Delete(ctx, hash)
	closeErr := m.registry.Close(hash)
	return errors.Join(delErr, closeErr)
}

// SetCookie writes the session cookie: a browser-session cookie, HttpOnly, SameSite=Lax.
func (m *Manager) SetCookie(w http.ResponseWriter, id string) {
	//nolint:gosec // Secure follows server.publicURL on purpose (A8): plain HTTP is only for local development
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: m.d.CookieSecure,
	})
}

// ClearCookie tells the browser to drop the session cookie.
func (m *Manager) ClearCookie(w http.ResponseWriter) {
	//nolint:gosec // see SetCookie
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: m.d.CookieSecure,
	})
}

// RunWatchdog closes registered streams of sessions that were ended on another replica (A13).
// It blocks until ctx is done.
func (m *Manager) RunWatchdog(ctx context.Context) {
	m.run(ctx, watchdogPeriod, func() {
		hashes := m.registry.Hashes()
		if len(hashes) == 0 {
			return
		}
		alive, err := m.d.Store.Exists(ctx, hashes)
		if err != nil {
			m.d.Log.Warn("session watchdog", "error", err.Error())
			return
		}
		still := make(map[string]bool, len(alive))
		for _, h := range alive {
			still[h] = true
		}
		for _, h := range hashes {
			if !still[h] {
				m.closeQuietly(h)
			}
		}
	})
}

// RunSweep deletes expired sessions every minute and closes their registered streams.
// It blocks until ctx is done.
func (m *Manager) RunSweep(ctx context.Context) {
	m.run(ctx, sweepPeriod, func() {
		gone, err := m.d.Store.DeleteExpired(ctx, m.d.Clock.Now(), m.d.IdleTimeout)
		if err != nil {
			m.d.Log.Warn("session sweep", "error", err.Error())
			return
		}
		for _, h := range gone {
			m.closeQuietly(h)
		}
	})
}

func (m *Manager) closeQuietly(hash string) {
	if err := m.registry.Close(hash); err != nil {
		m.d.Log.Warn("closing session resources", "error", err.Error())
	}
}

// run calls fn every interval until ctx is done.
func (m *Manager) run(ctx context.Context, interval time.Duration, fn func()) {
	t := m.d.Clock.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C():
			fn()
		}
	}
}
