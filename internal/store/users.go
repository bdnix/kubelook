package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bdnix/kubelook/internal/clock"
)

// NewID returns a random identifier: 16 random bytes as 32 lowercase hex characters.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("reading random bytes: %v", err)) // crypto/rand does not fail on supported platforms
	}
	return hex.EncodeToString(b[:])
}

// User is a signed-in person, identified by provider and stable subject (never by email).
type User struct {
	ID, Provider, Subject, Email, Name string
	CreatedAt, LastLoginAt             time.Time
}

// FilterSet is a saved field-filter query.
type FilterSet struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Query string `json:"query"`
}

// Settings is the one JSON document of preferences kept per user.
type Settings struct {
	FavouriteClusters []string            `json:"favouriteClusters"`
	NamespaceFilters  map[string][]string `json:"namespaceFilters"` // cluster ID → namespaces
	ColumnPresets     map[string][]string `json:"columnPresets"`    // "<group>/<kind>" → column IDs
	FilterSets        []FilterSet         `json:"filterSets"`
	WatchIdleSeconds  int                 `json:"watchIdleSeconds"` // 30..300
	Theme             string              `json:"theme"`            // light | dark | system
}

const (
	defaultWatchIdle = 30
	maxWatchIdle     = 300
)

// Users stores users and their settings.
type Users struct {
	db    *DB
	clock clock.Clock
	newID func() string
}

// NewUsers returns a Users store. newID supplies identifiers for new users.
func NewUsers(db *DB, c clock.Clock, newID func() string) *Users {
	return &Users{db: db, clock: c, newID: newID}
}

// Upsert creates the user or refreshes email, name and last login of the existing one.
func (u *Users) Upsert(ctx context.Context, provider, subject, email, name string) (User, error) {
	now := ms(u.clock.Now())
	row := u.db.QueryRow(ctx, `
INSERT INTO users (id, provider, subject, email, name, created_at, last_login_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (provider, subject) DO UPDATE SET email = excluded.email, name = excluded.name, last_login_at = excluded.last_login_at
RETURNING id, provider, subject, email, name, created_at, last_login_at`,
		u.newID(), provider, subject, email, name, now, now)
	user, err := scanUser(row)
	if err != nil {
		return User{}, fmt.Errorf("upserting user: %w", err)
	}
	return user, nil
}

// Get returns the user or ErrNotFound.
func (u *Users) Get(ctx context.Context, id string) (User, error) {
	user, err := scanUser(u.db.QueryRow(ctx,
		"SELECT id, provider, subject, email, name, created_at, last_login_at FROM users WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("user %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("reading user: %w", err)
	}
	return user, nil
}

func scanUser(row *sql.Row) (User, error) {
	var user User
	var created, last int64
	if err := row.Scan(&user.ID, &user.Provider, &user.Subject, &user.Email, &user.Name, &created, &last); err != nil {
		return User{}, err
	}
	user.CreatedAt, user.LastLoginAt = fromMS(created), fromMS(last)
	return user, nil
}

// GetSettings returns the user's settings, or the defaults when none were saved.
func (u *Users) GetSettings(ctx context.Context, userID string) (Settings, error) {
	var data string
	err := u.db.QueryRow(ctx, "SELECT data FROM user_settings WHERE user_id = ?", userID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{WatchIdleSeconds: defaultWatchIdle}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("reading settings: %w", err)
	}
	var s Settings
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		return Settings{}, fmt.Errorf("decoding settings: %w", err)
	}
	return s, nil
}

// PutSettings validates and saves the user's settings.
func (u *Users) PutSettings(ctx context.Context, userID string, s Settings) error {
	if s.WatchIdleSeconds < defaultWatchIdle || s.WatchIdleSeconds > maxWatchIdle {
		return fmt.Errorf("watchIdleSeconds must be between %d and %d: %w", defaultWatchIdle, maxWatchIdle, ErrInvalid)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encoding settings: %w", err)
	}
	_, err = u.db.Exec(ctx, `
INSERT INTO user_settings (user_id, data, updated_at) VALUES (?, ?, ?)
ON CONFLICT (user_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
		userID, string(data), ms(u.clock.Now()))
	if err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	return nil
}
