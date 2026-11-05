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
  - Log streaming (follow, search, multi-container / multi-pod merge, structured JSON logs with level colouring and field filters, NDJSON export)
  - In-browser container shells
  - Debug toolkit: ephemeral debug containers with a configurable image and shell
  - Pod file browser: browse, view, edit, upload, download and delete files in a running container
  - Live YAML editing with OpenAPI-based validation, server-side dry run and server-side apply
  - Helm releases (list, install, upgrade with diff, rollback, uninstall)
  - FluxCD objects (dashboard, reconcile, suspend/resume, dependency graph)
  - Live resource map linking pods, workloads, Services, Ingresses, routes, config, claims and Helm releases
  - Karpenter module: NodePools, NodeClaims, NodeOverlays, node classes, topology and scaling decisions
  - Ecosystem modules, shown only when a cluster has the CRDs: KEDA, External Secrets, Vault Secrets Operator, cert-manager, Gateway API, Trivy Operator reports, and ServiceAccount permissions and policy status
  - Field filter on any kind (CRDs included), bulk node cordon and drain, certificate details for TLS Secrets, and quick image-tag edits
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
| 1. Core | Core kinds with live watches; YAML edit with dry run; exec shells; kubeconfig upload; structured logs; debug toolkit; pod file browser; bulk node actions; field filter; certificate details; image-tag edit |
| 2. Platform | Helm; Flux; CRD browser and forms; resource map; Karpenter; KEDA, External Secrets, Vault Secrets Operator, cert-manager and Gateway API modules; Trivy and permissions views; cluster overview triage |
| 3. Harden | Audit log to S3; metrics and tracing; load and penetration tests |

Phases are not dated yet.

## Non-goals for v1

A plugin marketplace. The architecture leaves room for plugins, but v1 ships the features above as built-ins. Also out of scope for v1:

- Port-forwarding, and any feature that needs a TCP tunnel from the user's machine (such as database or message-broker consoles).
- An AI assistant, which is a possible future feature.
- Phone layouts.

## Contributing

There is no code to build or run yet. The most useful contributions right now are feedback on the spec in [`docs/spec.md`](docs/spec.md).

## License

[MIT](LICENSE) © 2026 bdnix
