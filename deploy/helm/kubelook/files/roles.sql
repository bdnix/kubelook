-- Roles of the Kubelook database. Run by the database owner after EVERY `kubelook migrate`, not only the
-- first: it is idempotent, and running it again gives the application role its rights on tables that a
-- migration added and changes the retention period.
--
--   psql -v app_password=... -v cleanup_password=... -v retention_days=90 -f roles.sql
--
-- retention_days is the floor the database enforces for `kubelook audit-cleanup`; 0 means "keep forever".
-- It should equal audit.retentionDays in the Kubelook configuration.
DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'kubelook_app') THEN CREATE ROLE kubelook_app LOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'kubelook_cleanup') THEN CREATE ROLE kubelook_cleanup LOGIN; END IF;
END $$;
ALTER ROLE kubelook_app PASSWORD :'app_password';
ALTER ROLE kubelook_cleanup PASSWORD :'cleanup_password';

-- Every table of the schema, including ones added by later migrations, except the audit table.
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM kubelook_app, kubelook_cleanup;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO kubelook_app;
REVOKE ALL ON audit_events, schema_migrations FROM kubelook_app;
GRANT SELECT ON schema_migrations TO kubelook_app;
GRANT INSERT ON audit_events TO kubelook_app;                      -- insert only: no SELECT, UPDATE, DELETE
GRANT SELECT (time), DELETE ON audit_events TO kubelook_cleanup;

ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS audit_insert ON audit_events;
DROP POLICY IF EXISTS audit_cleanup_select ON audit_events;
DROP POLICY IF EXISTS audit_cleanup_delete ON audit_events;
CREATE POLICY audit_insert ON audit_events FOR INSERT TO kubelook_app WITH CHECK (true);
-- retention_days = 0 means "keep forever": the cleanup role then sees and deletes nothing.
CREATE POLICY audit_cleanup_select ON audit_events FOR SELECT TO kubelook_cleanup
  USING ((:retention_days)::bigint > 0
     AND time < (extract(epoch FROM now()) * 1000)::bigint - (:retention_days)::bigint * 86400000);
CREATE POLICY audit_cleanup_delete ON audit_events FOR DELETE TO kubelook_cleanup
  USING ((:retention_days)::bigint > 0
     AND time < (extract(epoch FROM now()) * 1000)::bigint - (:retention_days)::bigint * 86400000);
