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
real organisation's Vault. That gate is open ([`docs/security-review.md`](docs/security-review.md)), so only release candidates are
published. Nothing has had an external security review.

## Install

Kubelook runs as one container. The supported way to run it is the Helm chart, on Kubernetes 1.35 or newer. You need:

- a Vault with one Kubernetes secrets engine mount per cluster ([Vault policy](docs/vault-policy.md)), or only kubeconfig upload;
- an OAuth application for Google or GitHub, with the redirect address `<publicURL>/auth/google/callback` or
  `<publicURL>/auth/github/callback`;
- an `https://` address for the console, with WebSocket support.

```bash
kubectl create namespace kubelook
kubectl -n kubelook create secret generic kubelook-key --from-literal=key="$(head -c 32 /dev/urandom | base64)"
kubectl -n kubelook create secret generic kubelook-oauth --from-literal=google=<client secret>

cat > values.yaml <<'YAML'
encryption: {keySecret: kubelook-key}
server: {publicURL: https://kubelook.example.org}
auth:
  google:
    clientId: <client id>
    clientSecretRef: {name: kubelook-oauth, key: google}
    allowedDomains: [example.org]
vault:
  address: https://vault.example.org
  candidateRoles: [devops-role]
ingress:
  enabled: true
  hosts: [{host: kubelook.example.org, paths: [{path: /, pathType: Prefix}]}]
YAML

helm install kubelook oci://ghcr.io/bdnix/charts/kubelook --version <version> -n kubelook -f values.yaml
```

This runs one replica with SQLite. For several replicas, PostgreSQL, upgrades, outbound traffic rules and the full list of settings,
read [Deployment](docs/deployment.md), [PostgreSQL](docs/postgres.md) and [Configuration](docs/configuration.md). Releases and how to
verify them are in [Releasing](docs/releasing.md).

## Using Kubelook

1. **Sign in** with Google or GitHub. Only accounts of your allow lists (`auth.google.allowedDomains`, `auth.github.allowedOrgs`) are
   admitted.
2. **Add a credential.** Paste a Vault token: Kubelook lists exactly the clusters its policies allow. Or upload a kubeconfig. The
   browser keeps an encrypted copy that only the server can read, so you do not paste it again at your next sign-in in that browser.
3. **Open a cluster** from the list and pick a role if the token allows more than one. Every request to the cluster uses your own
   credential: the console has no cluster rights of its own.
4. **Work.** Resource lists update live. Open a row for its details (overview, YAML, events, logs, files, related objects). Edit YAML
   with a preview of the change before you save. Scale, restart and roll back workloads, cordon and drain nodes, stream logs, open a
   shell or a debug container, browse a pod's files, and manage Helm releases and Flux resources. Ecosystem modules (Karpenter, KEDA,
   External Secrets, cert-manager, Gateway API, Trivy Operator, permissions) appear when their CRDs exist.
5. **Sign out.** Saved credentials stay in your browser; the session's decrypted credentials are deleted from the server.

Every action that changes something is written to the audit log with your account and the Vault identity behind it
([Operations](docs/operations.md)).

## Try it on your machine

The quickest way to see it: the [local demo](docs/demo.md) runs the console, Vault, PostgreSQL and a kind cluster with
[devbox](https://github.com/pilat/devbox).

```bash
devbox init https://github.com/bdnix/kubelook.git --name kubelook-demo
devbox --name kubelook-demo up   # then open http://localhost:8080
```

The integration environment needs Docker on a cgroup v2 host, and Go.

```bash
make it-up        # two kind clusters, Vault, PostgreSQL, a sign-in stand-in and the console at http://localhost:18080
make it           # the integration tests
make it-down
```

## Documentation

- [Deployment](docs/deployment.md): prerequisites, Secrets, install with SQLite or PostgreSQL, ingress, outbound traffic, upgrades
- [Configuration](docs/configuration.md): every setting, every environment variable, and the chart values
- [Vault policy](docs/vault-policy.md): what users' tokens and Vault roles need
- [PostgreSQL](docs/postgres.md): roles, migrations and the audit tamper resistance
- [Operations](docs/operations.md): key rotation, restarts, metrics, logs, the audit record and known limits
- [Local demo](docs/demo.md): try it on your machine with devbox
- [Running with `docker run`](docs/docker-run.md) (documented, not supported)
- [Development](docs/development.md)
- [Security review](docs/security-review.md), [CI](docs/ci.md), [releasing](docs/releasing.md), [spike findings](docs/spike-findings.md)
- [Implementation plan](PLAN.md)

## Development

```bash
make tools && (cd web && npm ci)
make verify       # lint, unit tests with coverage, build: ends with VERIFY OK
```

[Development](docs/development.md) has the commands, the layout, the conventions and how to run the whole stack.

## License

[MIT](LICENSE) © 2026 bdnix
