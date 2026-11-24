# Configuration

Kubelook reads one YAML file and a fixed set of environment variables. The file is `/etc/kubelook/config.yaml`, or the path in
`KUBELOOK_CONFIG`. It is read strictly: an unknown key stops the start with a message that names it. Secrets are never in the file;
they come only from the environment variables below.

With the Helm chart you do not write the file. Every key below is a value of the chart with the same name (`vault.address` is
`vault.address`), and the chart renders them into the file. The few settings that exist only in the chart are listed at the end.
Where a key is not set, the default in the table applies. Durations are Go durations: `30m`, `12h`, `45s`.

## Environment variables

| Variable | Needed | Meaning |
| --- | --- | --- |
| `KUBELOOK_ENCRYPTION_KEY` | always | The key that encrypts what the console stores for a session: 32 random bytes, base64 (standard, padded). Create it with `head -c 32 /dev/urandom \| base64`. Changing it signs every user out; see [operations.md](operations.md) |
| `KUBELOOK_DATABASE_URL` | `database.driver: postgres` | The PostgreSQL URL the console connects with. The commands `migrate` and `audit-cleanup` load the whole configuration and need it too |
| `KUBELOOK_MIGRATE_DATABASE_URL` | `kubelook migrate` on PostgreSQL | A role that may change the schema (the database owner) |
| `KUBELOOK_AUDIT_CLEANUP_DATABASE_URL` | `kubelook audit-cleanup` on PostgreSQL | The cleanup role, which can only delete audit records older than the retention period |
| `KUBELOOK_GOOGLE_CLIENT_SECRET` | `auth.google.clientId` is set | OAuth client secret of the Google application |
| `KUBELOOK_GITHUB_CLIENT_SECRET` | `auth.github.clientId` is set | OAuth client secret of the GitHub application |
| `KUBELOOK_CONFIG` | no | Path of the configuration file; default `/etc/kubelook/config.yaml` |
| `KUBELOOK_LOG_LEVEL` | no | `debug`, `info` (default), `warn` or `error` |

In the chart these come from Secrets that you create and name in the values: `encryption.keySecret` (key `key`),
`database.existingSecret`, `database.migrationSecret` and `audit.cleanup.credentialsSecret` (key `url` in each) and
`auth.google.clientSecretRef` / `auth.github.clientSecretRef` (`{name, key}`).

## Keys of the file

### Server and database

| Key | Default | Meaning |
| --- | --- | --- |
| `server.listenAddress` | `:8080` | Address the server listens on. The chart derives the container port from it |
| `server.publicURL` | required | The address users open. An `http` or `https` URL. It is the base of the sign-in redirect addresses, and the session cookie gets the `Secure` attribute when it starts with `https://`. Production needs `https://` |
| `database.driver` | `sqlite` | `sqlite` (one replica) or `postgres` (several replicas) |
| `database.sqlitePath` | `/data/kubelook.db` | The SQLite file. The chart mounts its volume at the directory of this path |
| `database.migrateOnStart` | `true` for `sqlite`, `false` for `postgres` | Apply the schema migrations when the server starts. With PostgreSQL the chart runs `kubelook migrate` in a hook instead |
| `session.idleTimeout` | `30m` | A session ends after this long without interaction and without an open stream |
| `session.maxLifetime` | `12h` | The longest a session lives |

### Sign-in

| Key | Default | Meaning |
| --- | --- | --- |
| `auth.restrictSignIn` | `true` | Only accounts of the allow lists below can sign in. With `true` and an empty list, that provider refuses everyone (the server logs a warning) |
| `auth.google.clientId` | none | Client ID of the Google application. Setting it turns Google sign-in on and requires `KUBELOOK_GOOGLE_CLIENT_SECRET` |
| `auth.google.allowedDomains` | none | Google Workspace domains (the `hd` claim) that may sign in |
| `auth.google.issuerURL` | `https://accounts.google.com` | The OpenID Connect issuer. Change it only to point at a stand-in |
| `auth.github.clientId` | none | Client ID of the GitHub application. Setting it turns GitHub sign-in on and requires `KUBELOOK_GITHUB_CLIENT_SECRET` |
| `auth.github.allowedOrgs` | none | GitHub organisations whose active members may sign in |
| `auth.github.baseURL` | `https://github.com` | For GitHub Enterprise Server |
| `auth.github.apiBaseURL` | `https://api.github.com` | For GitHub Enterprise Server (`https://<host>/api/v3`) |

### Vault

| Key | Default | Meaning |
| --- | --- | --- |
| `vault.address` | none | Vault URL. When empty the Vault path is off |
| `vault.kubernetesMountPrefix` | `kubernetes/` | Where the cluster mounts live. A missing trailing slash is added |
| `vault.credsNamespace` | `itops` | `kubernetes_namespace` sent when minting a credential: where Vault creates the temporary service account. The namespace must exist in the cluster |
| `vault.clusterRoleBinding` | `true` | `cluster_role_binding` sent when minting. `true` needs Vault roles with `kubernetes_role_type=ClusterRole` |
| `vault.candidateRoles` | none | The Vault roles checked on every cluster, in order of preference; the first allowed one is the default |
| `vault.tokenTTL` | `30m` | `ttl` sent when minting. Credentials are not revoked at sign-out, so this is how long they outlive a session |
| `vault.expiryWarning` | `30m` | Time left on a saved Vault token at which the console starts to warn the user |
| `vault.remintBefore` | `5m` | A cluster credential with less than this left is replaced by a fresh one. `0s` is read as the default |

### Features

| Key | Default | Meaning |
| --- | --- | --- |
| `kubeconfig.enabled` | `true` | Kubeconfig upload (path B) on or off |
| `fileBrowser.enabled` | `true` | The pod file browser on or off |
| `fileBrowser.maxTransferBytes` | `26214400` | Largest single upload or download |
| `logs.historyMaxLines` | `200000` | Most lines the log viewer loads when scrolling back |
| `logs.liveBufferLines` | `10000` | Lines the browser keeps of a live stream |
| `logs.maxLinesPerSecond` | `2000` | Per-stream rate cap. Lines over the cap are dropped and counted |
| `shell.idleTimeout` | `15m` | An idle shell is closed after this long |
| `nodeShell.enabled` | `false` | Node shells (a privileged pod on the node) on or off |
| `nodeShell.image` | same as `debug.image` | Image of the node shell pod |
| `debug.image` | `ghcr.io/bdnix/kubelook-netshoot:v0.16` in the application; the chart pins a digest | Image of the ephemeral debug container |
| `debug.shell` | `/bin/bash` | Shell started in the debug container; it falls back to `/bin/sh` |
| `debug.profiles` | none | Named sets of debug options users can pick: a list of `{name, profile, shareProcessNamespace}` |
| `helm.version` | `4` | The Helm SDK. Only `4` is accepted; `3` stops the start. Releases made by Helm 3 stay manageable |
| `helm.maxConcurrentJobs` | `5` | Helm jobs one replica runs at once; more are queued |
| `helm.maxChartBytes` | `10485760` | Largest chart a replica accepts |
| `helm.repositories` | none | Organisation-wide chart repositories: a list of `{name, url}` |
| `modules.<name>.enabled` | `auto` | `auto` shows a module only when its custom resource definitions exist; `false` hides it. Names: `karpenter`, `keda`, `externalSecrets`, `vaultSecrets`, `certManager`, `gatewayApi`, `trivy`, `permissions` |
| `prometheus.clusters` | none | A map from cluster ID (`v:prod`) to `{url}`: the Prometheus the metrics views may use. The console queries it without credentials |

### Limits, audit and operation

| Key | Default | Meaning |
| --- | --- | --- |
| `watch.maxPerUser` | `50` | Watches one user holds at once |
| `memory.refuseNewWorkPercent` | `80` | Above this share of the memory limit, new watches, shells and Helm jobs are refused with "server busy" |
| `audit.retentionDays` | `0` | Days to keep audit records; `0` keeps them forever. On PostgreSQL it should equal the `retention_days` given to `deploy/postgres/roles.sql` |
| `maintenance.banner` | none | A message shown to every signed-in user on every page |
| `telemetry.otlpEndpoint` | none | Where traces are exported (OpenTelemetry). Empty: no export |
| `egress.blockLocalAddresses` | `true` | Refuse loopback, link-local (cloud metadata included) and unspecified addresses for addresses a user supplies |

## Settings that exist only in the chart

| Value | Default | Meaning |
| --- | --- | --- |
| `image.repository`, `image.tag`, `image.digest`, `image.pullPolicy` | `ghcr.io/bdnix/kubelook`, the chart's app version, none, `IfNotPresent` | The image. A digest replaces the tag |
| `replicaCount` | `1` | Replicas. More than 1 needs PostgreSQL |
| `encryption.keySecret` | required | Secret with the key `key` |
| `database.existingSecret`, `database.migrationSecret`, `database.rolesSecret` | none | Secrets for PostgreSQL; see [postgres.md](postgres.md) |
| `audit.cleanup.schedule`, `audit.cleanup.credentialsSecret` | `17 3 * * *`, none | Schedule and PostgreSQL credentials of the cleanup CronJob, which exists when `audit.retentionDays` is above 0 |
| `persistence.size`, `persistence.storageClass` | `1Gi`, cluster default | The SQLite volume |
| `service.type`, `service.port`, `service.sessionAffinity` | `ClusterIP`, `80`, `None` | The Service |
| `ingress.*` | off | `enabled`, `className`, `annotations`, `hosts`, `tls` |
| `resources` | memory limit `512Mi` | Container resources |
| `goMemLimit` | `460MiB` | `GOMEMLIMIT`, about 90% of the memory limit. Change both together |
| `hostAliases`, `podAnnotations`, `nodeSelector`, `tolerations`, `affinity` | empty | As in a pod spec |
| `networkPolicy.enabled`, `networkPolicy.egress` | `false`, none | A NetworkPolicy; see [deployment.md](deployment.md) |
| `imagePullSecrets` | none | Pull secrets of the pod |

`values.schema.json` makes the install fail when `encryption.keySecret` is empty, when `replicaCount` is above 1 with SQLite, when
PostgreSQL is chosen without `database.existingSecret`, when `helm.version` is not 4, and when `audit.retentionDays` is above 0 with
PostgreSQL and no `audit.cleanup.credentialsSecret`.
