# Kubelook

A browser-based Kubernetes console. Sign in with Google or GitHub, add a Vault token or upload a kubeconfig, and operate every
cluster that credential reaches, with no desktop install.

- Live resource lists, details, YAML editing with dry run and diff, workload and node actions, logs, shells, a debug toolkit and a pod
  file browser.
- Helm releases, Flux, a resource map, and modules for Karpenter, KEDA, External Secrets, cert-manager, Gateway API, Trivy Operator
  and permissions.
- Every Kubernetes call uses the user's own credential; the console has no cluster rights of its own. Every action is audited.

The spec ([`docs/spec.md`](docs/spec.md)) has the goals, features, architecture and security model.

## Status

The plan is implemented and tested against a real Vault and two real Kubernetes clusters, but the product has not been used against a
real organisation's Vault and has not had an external security review. Those two gates are open
([`docs/security-review.md`](docs/security-review.md)), so only release candidates are published.

## Quick start

You need Docker (on a cgroup v2 host) and Go.

```bash
make it-up        # two kind clusters, Vault, PostgreSQL, a sign-in stand-in and the console at http://localhost:18080
make it           # the integration tests
make it-down
```

To install it into a cluster, use the Helm chart:

```bash
helm install kubelook oci://ghcr.io/bdnix/charts/kubelook --version <version> -n kubelook --create-namespace -f values.yaml
```

## Documentation

- [Deployment](docs/deployment.md): prerequisites, Secrets, install with SQLite or PostgreSQL, ingress, outbound traffic, upgrades
- [Configuration](docs/configuration.md): every setting, every environment variable, and the chart values
- [Vault policy](docs/vault-policy.md): what users' tokens and Vault roles need
- [PostgreSQL](docs/postgres.md): roles, migrations and the audit tamper resistance
- [Operations](docs/operations.md): key rotation, restarts, metrics, logs, the audit record and known limits
- [Running with `docker run`](docs/docker-run.md) (documented, not supported)
- [Security review](docs/security-review.md), [CI](docs/ci.md), [releasing](docs/releasing.md), [spike findings](docs/spike-findings.md), [load test](docs/load-test.md)
- [Implementation plan](PLAN.md)

## Development

```bash
make tools        # pinned tools into bin/
make verify       # lint, unit tests with coverage, build: ends with VERIFY OK
make test-ui      # browser tests of the web app (needs Chromium)
make it-up && make it && make e2e   # the real-system tests
make chart-test   # the Helm chart
```

## License

[MIT](LICENSE) © 2026 bdnix
