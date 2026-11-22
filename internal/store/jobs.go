package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Job statuses. A job is "active" while it is queued or running.
const (
	JobQueued      = "queued"
	JobRunning     = "running"
	JobSucceeded   = "succeeded"
	JobFailed      = "failed"
	JobInterrupted = "interrupted"
)

// JobRow is one stored job. SealedParams is opaque to the store.
type JobRow struct {
	ID, UserID, SessionHash, Cluster, Type, Target, SealedParams string
	Status, Progress, Message, Offers, LeaseOwner                string
	LeaseExpiresAt, CreatedAt, UpdatedAt, FinishedAt             time.Time
}

// Jobs stores durable jobs. Every state change is one conditional UPDATE whose
// affected-row count says whether this caller won: a job is never read and then written.
type Jobs struct{ db *DB }

// NewJobs returns a Jobs store.
func NewJobs(db *DB) *Jobs { return &Jobs{db: db} }

const jobColumns = `id, user_id, session_hash, cluster, type, target, sealed_params, status, progress, message, offers,
lease_owner, lease_expires_at, created_at, updated_at, finished_at`

// Insert stores a new job.
func (j *Jobs) Insert(ctx context.Context, r JobRow) error {
	_, err := j.db.Exec(ctx, `INSERT INTO jobs (`+jobColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.UserID, r.SessionHash, r.Cluster, r.Type, r.Target, r.SealedParams, r.Status, r.Progress, r.Message, r.Offers,
		r.LeaseOwner, ms(r.LeaseExpiresAt), ms(r.CreatedAt), ms(r.UpdatedAt), optMS(r.FinishedAt))
	if err != nil {
		return fmt.Errorf("inserting job: %w", err)
	}
	return nil
}

// Get returns the job or ErrNotFound.
func (j *Jobs) Get(ctx context.Context, id string) (JobRow, error) {
	r, err := scanJob(j.db.QueryRow(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return JobRow{}, fmt.Errorf("job: %w", ErrNotFound)
	}
	if err != nil {
		return JobRow{}, fmt.Errorf("reading job: %w", err)
	}
	return r, nil
}

// List returns the user's jobs, newest first: all of them (active nil), the queued and running ones
// (true) or the finished ones (false).
func (j *Jobs) List(ctx context.Context, userID string, active *bool, limit int) ([]JobRow, error) {
	q := "SELECT " + jobColumns + " FROM jobs WHERE user_id = ?"
	switch {
	case active == nil:
	case *active:
		q += " AND status IN ('queued', 'running')"
	default:
		q += " AND status NOT IN ('queued', 'running')"
	}
	q += " ORDER BY created_at DESC, id DESC LIMIT ?"
	return j.query(ctx, q, userID, limit)
}

// Expired returns the queued and running jobs whose lease ended at or before now, oldest lease first.
func (j *Jobs) Expired(ctx context.Context, now time.Time) ([]JobRow, error) {
	return j.query(ctx, "SELECT "+jobColumns+" FROM jobs WHERE status IN ('queued', 'running') AND lease_expires_at <= ? ORDER BY lease_expires_at, id", ms(now))
}

func (j *Jobs) query(ctx context.Context, q string, args ...any) ([]JobRow, error) {
	rows, err := j.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []JobRow
	for rows.Next() {
		r, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("listing jobs: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}
	return out, nil
}

// won reports whether an UPDATE changed exactly the row the caller meant.
func won(res sql.Result, err error, what string) (bool, error) {
	if err != nil {
		return false, fmt.Errorf("%s: %w", what, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("%s: %w", what, err)
	}
	return n == 1, nil
}

// Start moves a queued job owned by owner to running and sets its lease.
func (j *Jobs) Start(ctx context.Context, id, owner string, leaseUntil, now time.Time) (bool, error) {
	res, err := j.db.Exec(ctx, "UPDATE jobs SET status = 'running', lease_expires_at = ?, updated_at = ? WHERE id = ? AND status = 'queued' AND lease_owner = ?",
		ms(leaseUntil), ms(now), id, owner)
	return won(res, err, "starting job")
}

// Renew extends the lease of a queued or running job owned by owner.
func (j *Jobs) Renew(ctx context.Context, id, owner string, leaseUntil, now time.Time) (bool, error) {
	res, err := j.db.Exec(ctx, "UPDATE jobs SET lease_expires_at = ?, updated_at = ? WHERE id = ? AND status IN ('queued', 'running') AND lease_owner = ?",
		ms(leaseUntil), ms(now), id, owner)
	return won(res, err, "renewing job lease")
}

// SetProgress stores the progress document of a running job owned by owner.
func (j *Jobs) SetProgress(ctx context.Context, id, owner, progress string, now time.Time) (bool, error) {
	res, err := j.db.Exec(ctx, "UPDATE jobs SET progress = ?, updated_at = ? WHERE id = ? AND status = 'running' AND lease_owner = ?",
		progress, ms(now), id, owner)
	return won(res, err, "saving job progress")
}

// Finish records the outcome of a running job owned by owner.
func (j *Jobs) Finish(ctx context.Context, id, owner, status, message, offers string, now time.Time) (bool, error) {
	res, err := j.db.Exec(ctx, "UPDATE jobs SET status = ?, message = ?, offers = ?, updated_at = ?, finished_at = ? WHERE id = ? AND status = 'running' AND lease_owner = ?",
		status, message, offers, ms(now), ms(now), id, owner)
	return won(res, err, "finishing job")
}

// Steal takes over a job whose lease still ends at expiredAt: only one of several replicas
// scanning at once sees the lease unchanged and wins.
func (j *Jobs) Steal(ctx context.Context, id string, expiredAt time.Time, owner string, leaseUntil, now time.Time) (bool, error) {
	res, err := j.db.Exec(ctx, "UPDATE jobs SET lease_owner = ?, lease_expires_at = ?, updated_at = ? WHERE id = ? AND status IN ('queued', 'running') AND lease_expires_at = ?",
		owner, ms(leaseUntil), ms(now), id, ms(expiredAt))
	return won(res, err, "taking over job")
}

// Classify records the outcome decided by recovery for a job this replica holds the claim on.
func (j *Jobs) Classify(ctx context.Context, id, owner string, claimedUntil time.Time, status, message, offers string, now time.Time) (bool, error) {
	res, err := j.db.Exec(ctx, "UPDATE jobs SET status = ?, message = ?, offers = ?, updated_at = ?, finished_at = ? WHERE id = ? AND status IN ('queued', 'running') AND lease_owner = ? AND lease_expires_at = ?",
		status, message, offers, ms(now), ms(now), id, owner, ms(claimedUntil))
	return won(res, err, "classifying job")
}

func scanJob(r scanner) (JobRow, error) {
	var j JobRow
	var lease, created, updated, finished int64
	if err := r.Scan(&j.ID, &j.UserID, &j.SessionHash, &j.Cluster, &j.Type, &j.Target, &j.SealedParams, &j.Status, &j.Progress,
		&j.Message, &j.Offers, &j.LeaseOwner, &lease, &created, &updated, &finished); err != nil {
		return JobRow{}, err
	}
	j.LeaseExpiresAt, j.CreatedAt, j.UpdatedAt = fromMS(lease), fromMS(created), fromMS(updated)
	if finished != 0 {
		j.FinishedAt = fromMS(finished)
	}
	return j, nil
}

// JobCount is the number of jobs of one type in one status.
type JobCount struct {
	Type, Status string
	Count        int
}

// CountByTypeAndStatus counts every user's jobs, ordered by type and status.
func (j *Jobs) CountByTypeAndStatus(ctx context.Context) ([]JobCount, error) {
	rows, err := j.db.Query(ctx, "SELECT type, status, COUNT(*) FROM jobs GROUP BY type, status ORDER BY type, status")
	if err != nil {
		return nil, fmt.Errorf("counting jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []JobCount
	for rows.Next() {
		var c JobCount
		if err := rows.Scan(&c.Type, &c.Status, &c.Count); err != nil {
			return nil, fmt.Errorf("reading job counts: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading job counts: %w", err)
	}
	return out, nil
}
