CREATE TABLE users (
  id TEXT PRIMARY KEY, provider TEXT NOT NULL, subject TEXT NOT NULL,
  email TEXT NOT NULL, name TEXT NOT NULL, created_at BIGINT NOT NULL, last_login_at BIGINT NOT NULL,
  UNIQUE (provider, subject)
);
-- statement-break
CREATE TABLE user_settings (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  data TEXT NOT NULL, updated_at BIGINT NOT NULL
);
