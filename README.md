# Kubelook

A browser-based Kubernetes console. Sign in with a Vault token or an uploaded kubeconfig and operate every cluster that credential reaches, with no desktop install.

> **Status: design stage.** This repository currently contains the product and technical spec ([`docs/spec.md`](docs/spec.md)) and the license. No implementation has been committed yet, and everything below describes the planned v1 as written in the spec. Targets marked "proposed" in the spec are starting points, not commitments.

## Why

Engineers need to investigate and operate many Kubernetes clusters whose access is brokered through Vault. Kubelook gives them one place in the browser where a Vault token (or a kubeconfig) opens exactly the clusters they are allowed to see, without storing credentials on their own machines.

## Planned features

- **Two sign-in paths**
  - *Vault token:* loads every cluster the token's policies can get credentials for (one Vault Kubernetes secrets engine mount per cluster, `kubernetes/<cluster>/`).
  - *kubeconfig upload:* loads every context in the file. Supports embedded client certificates, bearer tokens and OIDC refresh tokens. File-path references and `exec` credential plugins are rejected.
- **Day-2 operations**
  - Live lists and details for workloads, config, networking, storage, cluster, access-control objects and CRDs
  - Log streaming (follow, search, multi-container / multi-pod merge)
  - In-browser container shells, with optional ephemeral debug containers
  - Live YAML editing with OpenAPI-based validation, server-side dry run and server-side apply
  - Helm releases (list, install, upgrade with diff, rollback, uninstall)
  - FluxCD objects (dashboard, reconcile, suspend/resume, dependency graph)
- **Your identity, your RBAC.** All Kubernetes calls run with the user's own credentials, so cluster RBAC remains the only authority. The console has no users, roles or permission settings of its own, and never changes Vault policies, roles or secrets engines.
- **Audit log** of sign-ins, credential issuance, mutating calls, Helm/Flux actions, Secret reveals and shell sessions, written to S3 (or an S3-compatible store). Shell keystroke recording is optional and off by default.

## Architecture (planned)

- **Backend:** stateless Go service (client-go, Helm SDK) that holds credentials, proxies API calls under the user's identity, and multiplexes watches and streams over one WebSocket per browser tab.
- **Frontend:** React + TypeScript, Monaco (YAML), xterm.js (terminals).
- **Sessions:** HttpOnly cookie in the browser; short-lived Kubernetes credentials stored encrypted server-side (Redis, key from Vault Transit or a KMS) and revoked on logout.
- **Deployment:** one container image plus a Helm chart, two or more replicas, running in the same network as the clusters (no agent or tunnel).

See the [spec](docs/spec.md) for the full discovery flow, security model, Helm settings and requirements.

## Roadmap

| Phase | Scope |
| --- | --- |
| 0. Spike | Vault login and cluster discovery; pods list and logs on one cluster |
| 1. Core | Core kinds with live watches; YAML edit with dry run; exec shells; kubeconfig upload |
| 2. Platform | Helm; Flux; CRD browser and forms |
| 3. Harden | Audit log to S3; metrics and tracing; load and penetration tests |

Phases are not dated yet.

## Non-goals for v1

A plugin marketplace. The architecture leaves room for plugins, but v1 ships the features above as built-ins. Phone layouts are also not supported.

## Contributing

There is no code to build or run yet. The most useful contributions right now are feedback on the spec in [`docs/spec.md`](docs/spec.md).

## License

[MIT](LICENSE) © 2026 bdnix
