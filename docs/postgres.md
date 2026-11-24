# PostgreSQL

Several replicas share one PostgreSQL database. The schema is applied by `kubelook migrate`, and a script gives the console a role
that can write audit records but cannot read, change or erase them.

## Three roles

| Role | Used by | Rights |
| --- | --- | --- |
| The owner (a role you already have, for example the one that owns the database) | `kubelook migrate` and `deploy/postgres/roles.sql` | Changes the schema and the grants. `KUBELOOK_MIGRATE_DATABASE_URL` |
| `kubelook_app` | the console | Select, insert, update and delete on the tables of the schema, **except** the audit table, where it can only insert. `KUBELOOK_DATABASE_URL` |
| `kubelook_cleanup` | `kubelook audit-cleanup` | Delete on the audit table, and only rows older than the retention period. `KUBELOOK_AUDIT_CLEANUP_DATABASE_URL` |

`roles.sql` creates `kubelook_app` and `kubelook_cleanup` when they do not exist, sets their passwords and grants, and enables a
row-level security policy on `audit_events`. It is idempotent: run it after **every** `kubelook migrate`, not only the first, so that
tables added by a later migration get their grants and the retention period is current.

## Run it by hand

```bash
export KUBELOOK_MIGRATE_DATABASE_URL=postgres://owner:...@db.example.org/kubelook
export KUBELOOK_ENCRYPTION_KEY=<base64 key>   # the commands load the whole configuration, which needs these
export KUBELOOK_DATABASE_URL=postgres://kubelook_app:...@db.example.org/kubelook
kubelook migrate

psql "$KUBELOOK_MIGRATE_DATABASE_URL" -v ON_ERROR_STOP=1 \
  -v app_password=<password of kubelook_app> -v cleanup_password=<password of kubelook_cleanup> \
  -v retention_days=90 -f deploy/postgres/roles.sql
```

`retention_days` is the floor the database enforces: the cleanup role sees and deletes only records older than that, and `0` means
keep forever. It should equal `audit.retentionDays`. `kubelook audit-cleanup` does not compare the two: if the database policy is
stricter than the setting, the command simply deletes fewer rows.

## With the Helm chart

The chart runs `kubelook migrate` in a hook before every install and upgrade, using the Secret named by `database.migrationSecret`
(key `url`). When `database.rolesSecret` is set (keys `appPassword` and `cleanupPassword`), the same hook then applies `roles.sql`
with `retention_days` set to `audit.retentionDays`, using the `postgres:18.6-trixie` image's `psql`; the two cannot drift in a chart
install. Create the Secrets before the first install:

```bash
kubectl -n kubelook create secret generic kubelook-db-migrate --from-literal=url='postgres://owner:...@db.example.org/kubelook'
kubectl -n kubelook create secret generic kubelook-db-roles   --from-literal=appPassword=... --from-literal=cleanupPassword=...
kubectl -n kubelook create secret generic kubelook-db          --from-literal=url='postgres://kubelook_app:<appPassword>@db.example.org/kubelook'
kubectl -n kubelook create secret generic kubelook-db-cleanup  --from-literal=url='postgres://kubelook_cleanup:<cleanupPassword>@db.example.org/kubelook'
```

Without `database.rolesSecret` you apply `roles.sql` yourself after each upgrade.

## What the tamper resistance covers

The integration tests `TestPostgres_AuditRecords_CannotBeReadChangedOrDeletedByTheApplicationRole`,
`TestPostgres_AuditCleanup_RoleDeletesOnlyRecordsOlderThanTheRetentionPeriod` and
`TestPostgres_RolesScript_CoversALaterMigrationAndChangesTheRetentionPeriod` prove that, on a real PostgreSQL: the console's role can
insert audit records and cannot select, update or delete them, the cleanup role deletes only records older than the retention period,
and the script gives the application role its rights on a table added by a later migration.

It covers a console that is taken over: its own database role cannot erase the trail. It does not cover someone who holds the owner's
credentials, or the database server itself. Every record is also written to the console's standard output as a JSON line, so a log
pipeline keeps a copy outside the database.

**SQLite cannot provide this.** The console owns the file and can change anything in it. Use PostgreSQL where audit tamper resistance
matters.
