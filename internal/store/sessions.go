package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Credential kinds stored with a session.
const (
	CredVaultToken  = "vault_token"  // ref ""
	CredKubeconfig  = "kubeconfig"   // ref = file label
	CredClusterCred = "cluster_cred" // ref = cluster ID
	CredClusterRole = "cluster_role" // ref = cluster ID; the chosen Vault role (kept together, not secret)
	CredHelmRepo    = "helm_repo"    // ref = repository name
)

// SessionRow is a stored session. The session ID itself is never stored, only its hash.
type SessionRow struct {
	Hash, UserID, CSRFToken          string
	CreatedAt, LastSeenAt, ExpiresAt time.Time
}

// CredentialRow is one sealed credential. The store never sees plaintext.
type CredentialRow struct {
	Kind, Ref, Sealed string
	ExpiresAt         time.Time // zero when the credential does not expire
}

// Sessions stores sessions and their credentials.
type Sessions struct{ db *DB }

// NewSessions returns a Sessions store.
func NewSessions(db *DB) *Sessions { return &Sessions{db: db} }

// Create inserts a session.
func (s *Sessions) Create(ctx context.Context, hash, userID, csrf string, now, expiresAt time.Time) error {
	_, err := s.db.Exec(ctx,
		"INSERT INTO sessions (id_hash, user_id, csrf_token, created_at, last_seen_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
		hash, userID, csrf, ms(now), ms(now), ms(expiresAt))
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	return nil
}

// Get returns the session or ErrNotFound.
func (s *Sessions) Get(ctx context.Context, hash string) (SessionRow, error) {
	var r SessionRow
	var created, seen, expires int64
	err := s.db.QueryRow(ctx,
		"SELECT id_hash, user_id, csrf_token, created_at, last_seen_at, expires_at FROM sessions WHERE id_hash = ?", hash).
		Scan(&r.Hash, &r.UserID, &r.CSRFToken, &created, &seen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRow{}, fmt.Errorf("session: %w", ErrNotFound)
	}
	if err != nil {
		return SessionRow{}, fmt.Errorf("reading session: %w", err)
	}
	r.CreatedAt, r.LastSeenAt, r.ExpiresAt = fromMS(created), fromMS(seen), fromMS(expires)
	return r, nil
}

// Touch records activity. It returns ErrNotFound when the session is gone.
func (s *Sessions) Touch(ctx context.Context, hash string, now time.Time) error {
	res, err := s.db.Exec(ctx, "UPDATE sessions SET last_seen_at = ? WHERE id_hash = ?", ms(now), hash)
	if err != nil {
		return fmt.Errorf("touching session: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("touching session: %w", err)
	} else if n == 0 {
		return fmt.Errorf("session: %w", ErrNotFound)
	}
	return nil
}

// Delete removes the session and, by cascade, its credentials. Deleting a missing session is not an error.
func (s *Sessions) Delete(ctx context.Context, hash string) error {
	if _, err := s.db.Exec(ctx, "DELETE FROM sessions WHERE id_hash = ?", hash); err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	return nil
}

// DeleteExpired removes sessions past their absolute expiry or idle for idleTimeout,
// and returns their hashes sorted.
func (s *Sessions) DeleteExpired(ctx context.Context, now time.Time, idleTimeout time.Duration) ([]string, error) {
	var hashes []string
	err := s.db.InTx(ctx, func(tx *Tx) error {
		rows, err := tx.Query(ctx, "SELECT id_hash FROM sessions WHERE expires_at <= ? OR last_seen_at <= ? ORDER BY id_hash",
			ms(now), ms(now.Add(-idleTimeout)))
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				return err
			}
			hashes = append(hashes, h)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, h := range hashes {
			if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE id_hash = ?", h); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("deleting expired sessions: %w", err)
	}
	return hashes, nil
}

// Exists returns the subset of hashes that still exist, sorted.
func (s *Sessions) Exists(ctx context.Context, hashes []string) ([]string, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	args := make([]any, len(hashes))
	for i, h := range hashes {
		args[i] = h
	}
	q := "SELECT id_hash FROM sessions WHERE id_hash IN (?" + strings.Repeat(", ?", len(hashes)-1) + ")"
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("checking sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, fmt.Errorf("checking sessions: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("checking sessions: %w", err)
	}
	sort.Strings(out)
	return out, nil
}

// PutCredential inserts or replaces the credential identified by (session, kind, ref).
func (s *Sessions) PutCredential(ctx context.Context, hash string, c CredentialRow, now time.Time) error {
	_, err := s.db.Exec(ctx, `
INSERT INTO session_credentials (session_hash, kind, ref, sealed, expires_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (session_hash, kind, ref) DO UPDATE SET sealed = excluded.sealed, expires_at = excluded.expires_at, updated_at = excluded.updated_at`,
		hash, c.Kind, c.Ref, c.Sealed, optMS(c.ExpiresAt), ms(now))
	if err != nil {
		return fmt.Errorf("saving credential: %w", err)
	}
	return nil
}

// GetCredential returns one credential or ErrNotFound.
func (s *Sessions) GetCredential(ctx context.Context, hash, kind, ref string) (CredentialRow, error) {
	row := s.db.QueryRow(ctx,
		"SELECT kind, ref, sealed, expires_at FROM session_credentials WHERE session_hash = ? AND kind = ? AND ref = ?", hash, kind, ref)
	c, err := scanCredential(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CredentialRow{}, fmt.Errorf("credential %s: %w", kind, ErrNotFound)
	}
	if err != nil {
		return CredentialRow{}, fmt.Errorf("reading credential: %w", err)
	}
	return c, nil
}

// ListCredentials returns the session's credentials of one kind, sorted by ref.
func (s *Sessions) ListCredentials(ctx context.Context, hash, kind string) ([]CredentialRow, error) {
	rows, err := s.db.Query(ctx,
		"SELECT kind, ref, sealed, expires_at FROM session_credentials WHERE session_hash = ? AND kind = ? ORDER BY ref", hash, kind)
	if err != nil {
		return nil, fmt.Errorf("listing credentials: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CredentialRow
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, fmt.Errorf("listing credentials: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing credentials: %w", err)
	}
	return out, nil
}

// DeleteCredential removes one credential. A missing credential is not an error.
func (s *Sessions) DeleteCredential(ctx context.Context, hash, kind, ref string) error {
	_, err := s.db.Exec(ctx, "DELETE FROM session_credentials WHERE session_hash = ? AND kind = ? AND ref = ?", hash, kind, ref)
	if err != nil {
		return fmt.Errorf("deleting credential: %w", err)
	}
	return nil
}

type scanner interface{ Scan(dest ...any) error }

func scanCredential(r scanner) (CredentialRow, error) {
	var c CredentialRow
	var exp int64
	if err := r.Scan(&c.Kind, &c.Ref, &c.Sealed, &exp); err != nil {
		return CredentialRow{}, err
	}
	if exp != 0 {
		c.ExpiresAt = fromMS(exp)
	}
	return c, nil
}

// optMS stores the zero time as 0 ("no expiry").
func optMS(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return ms(t)
}
