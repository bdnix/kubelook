# Kubelook Helm chart

Deploys the Kubelook console: one Deployment, its configuration, a Service, and optionally an Ingress, a
PodDisruptionBudget, a NetworkPolicy, a migrate hook (PostgreSQL) and an audit cleanup CronJob.

```bash
helm install kubelook oci://ghcr.io/bdnix/charts/kubelook --version <version> -n kubelook --create-namespace -f values.yaml
```

Read [docs/deployment.md](../../../docs/deployment.md) first: it lists the prerequisites and the Secrets to create.
Every setting is described in [docs/configuration.md](../../../docs/configuration.md).

## What you must set

| Value | Why |
| --- | --- |
| `encryption.keySecret` | Name of a Secret with the key `key` (32 random bytes, base64). The install fails without it |
| `server.publicURL` | The address users open; it is also the redirect address of the sign-in providers |
| `auth.google` / `auth.github` | `clientId`, `clientSecretRef` (`{name, key}` of an existing Secret) and the allow list (`allowedDomains` / `allowedOrgs`) |
| `vault.address` | The Vault that grants access to the clusters |

With PostgreSQL (`database.driver: postgres`) also `database.existingSecret` (key `url`), `database.migrationSecret`
(key `url`, a role that may change the schema) and, when `audit.retentionDays` is above 0,
`audit.cleanup.credentialsSecret` (key `url`). `database.rolesSecret` (keys `appPassword` and `cleanupPassword`) makes the
migrate hook also apply the hardened roles of `deploy/postgres/roles.sql`.

The chart refuses to install when the key Secret is not named, when SQLite is combined with more than one replica, when
PostgreSQL is chosen without `database.existingSecret`, when `helm.version` is not 4, and when audit retention is on with
PostgreSQL and no cleanup credentials. Secrets are never values of this chart: they are referenced by name.

## Notes

- SQLite runs one replica with the strategy `Recreate` and a PersistentVolumeClaim. PostgreSQL allows several replicas
  (RollingUpdate and a PodDisruptionBudget).
- A WebSocket stays on its replica. `service.sessionAffinity: ClientIP` and the ingress annotations of your controller
  make REST calls prefer the same replica. A load balancer in front must not time out idle WebSockets in under 60 seconds.
- `goMemLimit` is about 90% of the memory limit in `resources`. Change both together.
- The default `debug.image` is pinned to a digest by `hack/pin-netshoot.sh`.
