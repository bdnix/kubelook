CREATE TABLE jobs (
  id TEXT PRIMARY KEY, user_id TEXT NOT NULL, session_hash TEXT NOT NULL, cluster TEXT NOT NULL,
  type TEXT NOT NULL, target TEXT NOT NULL, sealed_params TEXT NOT NULL,
  status TEXT NOT NULL, progress TEXT NOT NULL, message TEXT NOT NULL, offers TEXT NOT NULL,
  lease_owner TEXT NOT NULL, lease_expires_at BIGINT NOT NULL,
  created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL, finished_at BIGINT NOT NULL DEFAULT 0
);
-- statement-break
CREATE INDEX jobs_user ON jobs(user_id, created_at);
-- statement-break
CREATE INDEX jobs_lease ON jobs(status, lease_expires_at);
