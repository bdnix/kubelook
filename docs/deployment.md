# Deploying Kubelook

Kubelook is deployed as one container image. The supported way is the Helm chart on Kubernetes; [docker-run.md](docker-run.md)
documents a `docker run` alternative that is not a supported deployment.

## Prerequisites

- **Kubernetes 1.35 or newer** for the cluster the console runs in and for the clusters it manages (the integration suite runs against
  1.35 and 1.37).
- **Vault** with one Kubernetes secrets engine mount per cluster at `kubernetes/<cluster>/`, each with `kubernetes_host` and
  `kubernetes_ca_cert` set. See [vault-policy.md](vault-policy.md). Kubelook can also run without Vault, with kubeconfig upload only.
- **OAuth applications** for the providers you allow, with these redirect addresses (`<publicURL>` is `server.publicURL`):
  - Google: `<publicURL>/auth/google/callback`
  - GitHub: `<publicURL>/auth/github/callback`

  For GitHub, an organisation that restricts third-party applications must approve the application before its members can sign in.
- **A database**: SQLite on a volume (one replica) or PostgreSQL (several replicas; see [postgres.md](postgres.md)).
- **HTTPS** in front of the console. `server.publicURL` must be an `https://` address in production: the session cookie gets the `Secure`
  attribute only then.

## Create the Secrets

Secrets are referenced by name; the chart never accepts secret material as a value.

```bash
kubectl create namespace kubelook

# The encryption key: 32 random bytes, base64.
kubectl -n kubelook create secret generic kubelook-key --from-literal=key="$(head -c 32 /dev/urandom | base64)"

# The OAuth client secrets.
kubectl -n kubelook create secret generic kubelook-oauth \
  --from-literal=google=<google client secret> --from-literal=github=<github client secret>
```

Keep a copy of the encryption key where you keep your other deployment secrets. Replacing it signs every user out
([operations.md](operations.md)).

## Install with SQLite (one replica)

```yaml
# values.yaml
encryption:
  keySecret: kubelook-key
server:
  publicURL: https://kubelook.example.org
auth:
  google:
    clientId: <google client id>
    clientSecretRef: {name: kubelook-oauth, key: google}
    allowedDomains: [example.org]
vault:
  address: https://vault.example.org
  candidateRoles: [devops-role, viewer-role]
```

```bash
helm install kubelook oci://ghcr.io/bdnix/charts/kubelook --version <version> -n kubelook -f values.yaml
```

The chart runs one replica with the strategy `Recreate` and a PersistentVolumeClaim (`persistence.size`, `persistence.storageClass`)
for the SQLite file. Releases of the chart are listed in the GitHub releases of the repository; see [releasing.md](releasing.md) for how
to verify an image and a chart.

## Install with PostgreSQL (several replicas)

Prepare the database and its three Secrets first ([postgres.md](postgres.md)), then:

```yaml
replicaCount: 2
database:
  driver: postgres
  existingSecret: kubelook-db            # key url: the application role
  migrationSecret: kubelook-db-migrate   # key url: the role that changes the schema
  rolesSecret: kubelook-db-roles         # keys appPassword, cleanupPassword (optional, see postgres.md)
audit:
  retentionDays: 90
  cleanup:
    credentialsSecret: kubelook-db-cleanup   # key url: the cleanup role
service:
  sessionAffinity: ClientIP
```

On every install and upgrade a hook Job runs `kubelook migrate` (and, when `database.rolesSecret` is set, applies
`deploy/postgres/roles.sql`) before the new pods start. A CronJob runs `kubelook audit-cleanup` on `audit.cleanup.schedule` when
`audit.retentionDays` is above 0. A PodDisruptionBudget keeps one replica available.

## Ingress, WebSockets and idle timeouts

Live lists, logs and shells use WebSockets. The ingress controller and any load balancer in front must pass WebSocket upgrades and
must not close an idle connection in under 60 seconds. A WebSocket stays on the replica that accepted it; `service.sessionAffinity:
ClientIP` and your controller's cookie or hash affinity make REST calls prefer the same replica. Example for ingress-nginx:

```yaml
ingress:
  enabled: true
  className: nginx
  annotations:
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
  hosts:
    - host: kubelook.example.org
      paths: [{path: /, pathType: Prefix}]
  tls:
    - secretName: kubelook-tls
      hosts: [kubelook.example.org]
```

`/metrics` needs no session. Do not expose it on the public address; scrape it from inside the cluster.

## Outbound traffic

The console has no egress allowlist of its own. It refuses loopback and link-local addresses for addresses users supply
(`egress.blockLocalAddresses`), but private ranges stay reachable because clusters live there. Restrict what the pod can reach with a
firewall or a NetworkPolicy. It must reach:

- the API servers of every cluster (the `kubernetes_host` of each Vault mount, and the servers of uploaded kubeconfigs),
- Vault (`vault.address`),
- the database (PostgreSQL),
- Google and GitHub (`accounts.google.com`, `github.com`, `api.github.com`, or your GitHub Enterprise Server),
- the OIDC issuers named in uploaded kubeconfigs,
- the Helm chart repositories and OCI registries your users add or you list in `helm.repositories`,
- the OpenTelemetry endpoint (`telemetry.otlpEndpoint`) and the per-cluster Prometheus addresses, when configured,
- and DNS.

The chart can render a NetworkPolicy. DNS is always allowed; you add the rest:

```yaml
networkPolicy:
  enabled: true
  egress:
    - to: [{ipBlock: {cidr: 10.20.0.0/16}}]        # the clusters' API servers
      ports: [{port: 6443}]
    - to: [{ipBlock: {cidr: 10.30.0.10/32}}]       # Vault
      ports: [{port: 8200}]
    - to: [{ipBlock: {cidr: 10.40.0.5/32}}]        # PostgreSQL
      ports: [{port: 5432}]
    - to: [{ipBlock: {cidr: 0.0.0.0/0, except: [10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16]}}]   # the identity providers and registries
      ports: [{port: 443}]
```

A NetworkPolicy filters by IP address, not by name, so the addresses of the identity providers and registries change under it, and a
policy can only approximate "Google and GitHub". Where you need names, use a proxy or a firewall that filters by name. Whether your
network plugin enforces NetworkPolicy is a property of the cluster; the chart tests do not check it.

## Upgrade and rollback

```bash
helm upgrade kubelook oci://ghcr.io/bdnix/charts/kubelook --version <new version> -n kubelook -f values.yaml
```

Set `maintenance.banner` first when users have shells open: a stopping replica closes its connections at once, and shells end with
it ([operations.md](operations.md)). The chart upgrade test (`TestChart_InstalledConsoleWorks_UpgradesAndUninstalls`) proves a
`helm upgrade` that changes a setting rolls the console out with the new configuration.

`helm rollback kubelook <revision> -n kubelook` returns to an earlier chart revision. Database schema migrations are applied by
`kubelook migrate` and the repository has no down migrations; rolling back to a version older than the schema in the database is not
verified. Read the release notes of the version you move to, and back up the database before an upgrade.
