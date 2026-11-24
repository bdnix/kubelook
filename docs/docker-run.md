# Running with `docker run`

**Documented, not a supported deployment.** The supported deployment is the Helm chart ([deployment.md](deployment.md)). The commands
below show how the image is configured: it reads a YAML file and environment variables, however they are provided. Nothing here is
covered by a test of this repository except that `docker run` of the image runs `version` (the build job) and `audit-cleanup` (the
integration tests, see `hack/it-pg.sh`).

The image runs as `65532:65532` on a distroless base and has no shell.

## The console (SQLite)

```bash
docker volume create kubelook-data
docker run -d --name kubelook -p 8080:8080 \
  -v "$PWD/config.yaml:/etc/kubelook/config.yaml:ro" \
  -v kubelook-data:/data \
  --tmpfs /tmp \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  -e KUBELOOK_ENCRYPTION_KEY="$(cat kubelook.key)" \
  -e KUBELOOK_GOOGLE_CLIENT_SECRET=... \
  ghcr.io/bdnix/kubelook:<version> serve
```

The volume must be writable by the user `65532`. `config.yaml` is the file described in [configuration.md](configuration.md); its
`database.sqlitePath` is `/data/kubelook.db` by default. Put an HTTPS reverse proxy in front.

## Migrations (PostgreSQL)

```bash
docker run --rm -v "$PWD/config.yaml:/etc/kubelook/config.yaml:ro" \
  -e KUBELOOK_ENCRYPTION_KEY="$(cat kubelook.key)" \
  -e KUBELOOK_DATABASE_URL=postgres://kubelook_app:...@db/kubelook \
  -e KUBELOOK_MIGRATE_DATABASE_URL=postgres://owner:...@db/kubelook \
  ghcr.io/bdnix/kubelook:<version> migrate
```

Then apply `deploy/postgres/roles.sql` as described in [postgres.md](postgres.md).

## Audit cleanup with cron

```bash
docker run --rm -v "$PWD/config.yaml:/etc/kubelook/config.yaml:ro" \
  -e KUBELOOK_ENCRYPTION_KEY="$(cat kubelook.key)" \
  -e KUBELOOK_DATABASE_URL=postgres://kubelook_app:...@db/kubelook \
  -e KUBELOOK_AUDIT_CLEANUP_DATABASE_URL=postgres://kubelook_cleanup:...@db/kubelook \
  ghcr.io/bdnix/kubelook:<version> audit-cleanup
```

A crontab line that runs it every night at 03:17 (put the command in a script, `cleanup.sh`, so the quoting stays simple):

```
17 3 * * * /usr/local/bin/cleanup.sh >> /var/log/kubelook-cleanup.log 2>&1
```

With SQLite, mount the same volume instead of passing the cleanup URL; the command takes
`audit.retentionDays` from the configuration.
