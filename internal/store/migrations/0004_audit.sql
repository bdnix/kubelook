CREATE TABLE audit_events (
  id TEXT PRIMARY KEY, time BIGINT NOT NULL,
  account_provider TEXT NOT NULL, account_subject TEXT NOT NULL, account_email TEXT NOT NULL,
  vault_entity_id TEXT NOT NULL, vault_display_name TEXT NOT NULL,
  kube_context TEXT NOT NULL, kube_user TEXT NOT NULL,
  cluster TEXT NOT NULL, namespace TEXT NOT NULL, verb TEXT NOT NULL,
  resource_kind TEXT NOT NULL, resource_name TEXT NOT NULL, outcome TEXT NOT NULL, details TEXT NOT NULL
);
-- statement-break
CREATE INDEX audit_events_time ON audit_events(time);
