CREATE TABLE sessions (
  id_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL, created_at BIGINT NOT NULL, last_seen_at BIGINT NOT NULL, expires_at BIGINT NOT NULL
);
-- statement-break
CREATE INDEX sessions_user ON sessions(user_id);
-- statement-break
CREATE TABLE session_credentials (
  session_hash TEXT NOT NULL REFERENCES sessions(id_hash) ON DELETE CASCADE,
  kind TEXT NOT NULL, ref TEXT NOT NULL, sealed TEXT NOT NULL, expires_at BIGINT NOT NULL DEFAULT 0, updated_at BIGINT NOT NULL,
  PRIMARY KEY (session_hash, kind, ref)
);
