# Kubelook implementation plan

Source of truth for *what* to build: [`docs/spec.md`](docs/spec.md) (dated 2026-11-10). This file says *how* and *in what order*.
Written 2026-11-11. Every version below was looked up on that date (see §3).

**How to use this plan (read first, executor).**

1. Work through the steps strictly in order. The 96 steps are grouped into **10 pull requests** (see "Pull request map" in §7). Never start a pull request before the previous one is merged with CI green.
2. One pull request = one branch `pr/<nn>-<slug>` (or the branch the repository owner designates) into `main`. A step is a group of commits inside its pull request, never a pull request of its own. Push after every step so CI runs step by step; open the pull request as a draft after the first step and mark it ready after the last.
3. Inside every step, follow the order **Red → Green → Refactor** exactly as written. Commit after each: `test(<id>): …` (failing tests), `feat(<id>): …` (implementation), `refactor(<id>): …` (only if something changed).
4. Do not make design decisions. If something is not specified, look in this order: the step → §5 Conventions → §1 Assumptions → `docs/spec.md`. If it is still not answered, stop and ask; do not guess.
5. Do not upgrade, swap or add dependencies beyond those in §3. Dependabot does upgrades after the project is done.
6. A step is finished only when its **Acceptance** commands print the expected output *and* `make verify` prints `VERIFY OK` (from Step 0.5 on) *and* CI is green on the pushed commit (from Step 0.7 on, when the pipeline exists). A pull request is finished when every step in it is finished; only then is it merged, as a squash merge whose message is the pull request title.

---

## Contents

- §1 Assumptions made where the spec is ambiguous
- §2 What was verified while writing this plan, and what was not
- §3 Technology choices and versions
- §4 Repository layout
- §5 Conventions (TDD procedure, commands, API, errors, determinism)
- §6 Test strategy and coverage rules
- §7 Step dependency map
- Phase 0 — Repository and CI/CD
- Phase 1 — Foundations
- Phase 2 — Sign-in and credentials
- Phase 3 — Cluster access and live data
- Phase 4 — Core features
- Phase 5 — Platform features
- Phase 6 — Hardening
- Phase 7 — Integration testing (kind + Vault)
- Phase 8 — Release packaging
- §8 Definition of done
- §9 Final verification checklist

---

## §1 Assumptions

The spec leaves these open. Each line is the decision this plan takes. Items marked **(spec: proposed)** use the value the spec itself proposes.

**Scope**

- A1. The two human gates in the spec's delivery plan (a real user's Vault token in a real organisation; an external security review and penetration test) cannot be executed by this plan. The plan covers them with the integration suite (Phase 7) and a written self-review checklist (`docs/security-review.md`, Step 8.5). They remain open items for the repository owner.
- A2. "To confirm in the spike" items are checked by integration tests in Step 7.11, which writes the answers to `docs/spike-findings.md`. Until then the plan builds what the spec currently says (for example: YAML save is a full `PUT`).
- A3. The spec's performance targets (2 s first render, 200 sessions per replica) are measured by the load test in Step 6.4 and reported, not enforced in CI.
- A4. Shipping audit records to Elasticsearch, a plugin system, port-forwarding and an AI assistant are out of scope (spec non-goals or "future").

**Configuration**

- A5. Configuration file keys are exactly the "Helm setting" names in the spec (`auth.restrictSignIn`, `vault.address`, …). The file path comes from `KUBELOOK_CONFIG` (default `/etc/kubelook/config.yaml`). Secrets are never in the file; they come only from these environment variables: `KUBELOOK_ENCRYPTION_KEY`, `KUBELOOK_DATABASE_URL`, `KUBELOOK_GOOGLE_CLIENT_SECRET`, `KUBELOOK_GITHUB_CLIENT_SECRET`, `KUBELOOK_AUDIT_CLEANUP_DATABASE_URL`, `KUBELOOK_MIGRATE_DATABASE_URL`. Two non-secret operational variables also exist: `KUBELOOK_CONFIG` and `KUBELOOK_LOG_LEVEL`. There is no generic "any key via env" mechanism.
- A6. Settings the spec does not name but the implementation needs (all listed in Step 1.1): `server.listenAddress` (`:8080`), `server.publicURL` (required), `database.driver` (`sqlite`|`postgres`), `database.sqlitePath`, `database.migrateOnStart`, `session.idleTimeout` (`30m`, spec: proposed), `session.maxLifetime` (`12h`, spec: proposed), `shell.idleTimeout` (`15m`, spec: proposed), `nodeShell.enabled` (`false`), `nodeShell.image`, `watch.maxPerUser` (`50`, spec: proposed), `logs.liveBufferLines` (`10000`, spec: proposed), `logs.maxLinesPerSecond` (`2000`), `vault.remintBefore` (`5m`), `helm.repositories` (org-wide repos), `prometheus.clusters` (per-cluster Prometheus URL), `telemetry.otlpEndpoint`, `auth.google.issuerURL`, `auth.github.baseURL`, `auth.github.apiBaseURL` (the last three exist so tests can point at a stand-in; defaults are the real services).
- A7. `KUBELOOK_ENCRYPTION_KEY` is 32 random bytes, base64 (standard, padded) encoded. The key ID is the first 16 hex characters of SHA-256 of the raw key.
- A8. The session cookie's `Secure` attribute is set when `server.publicURL` starts with `https://`. Production docs require HTTPS. Plain HTTP is accepted only so local development and the integration suite can run without TLS.

**Authentication and sessions**

- A9. Cluster credentials are re-minted when less than `vault.remintBefore` (default 5 minutes) of their lease is left, and at the latest on the next request that needs them. This is the plan's reading of "shortly before the current one expires".
- A10. Cluster IDs: `v:<mount name without prefix and trailing slash>` for Vault clusters, `k:<file label>/<context>` for kubeconfig clusters. Display names drop the `v:`/`k:` prefix. In URLs the ID is percent-encoded.
- A11. A kubeconfig's "file label" defaults to the uploaded file name without extension (or `pasted` for pasted text) and can be edited before upload. Labels are unique per session; uploading with an existing label replaces that kubeconfig.
- A12. "Interaction" for the idle timeout = any REST request or any client-originated WebSocket message other than heartbeat replies. An open log stream or shell also counts as activity (spec: "no interaction and no open stream").
- A13. Other replicas learn that a session ended by checking the session row every 5 seconds for each session that has open connections on that replica.
- A14. Audit of an action is two inserts: one before the action with `outcome="requested"` (if this insert fails, the action is refused), one after with `outcome="success"` or `"failure"`. Two inserts are needed because the console's PostgreSQL role may not update rows. Both carry the same `details.requestId`.
- A15. On PostgreSQL the hardened role layout (insert-only app role, cleanup role, row-level security) is created by `deploy/postgres/roles.sql`, which the deployer runs once as a database owner. Schema migrations are run by `kubelook migrate` using `KUBELOOK_MIGRATE_DATABASE_URL` (the Helm chart runs it as a pre-install/pre-upgrade Job). `database.migrateOnStart` defaults to `true` for SQLite and `false` for PostgreSQL.

**Features**

- A16. WebSocket frames are JSON text frames; binary payloads (shell I/O, file chunks) are base64 inside JSON. Simpler to implement and test; throughput is sufficient for terminals and for the 25 MiB transfer limit.
- A17. Durations in the field filter accept `s`, `m`, `h`, `d`, `w`. Timestamps are RFC 3339.
- A18. "Risk level" in the Permissions module is a fixed rule table defined in Step 5.14 (the spec does not define it).
- A19. A server-side log filter is a plain substring or RE2 regular expression applied to each line before it is sent.
- A20. "Repositories are added per session" means they are stored in the session's credentials table (encrypted, deleted at sign-out) because they may contain passwords.
- A21. The per-cluster Prometheus source is deployment configuration (`prometheus.clusters.<clusterID>.url`), queried from the backend without credentials.
- A22. Coverage: "90% unit-test coverage across the entire codebase" is enforced separately on the Go code (statement coverage, total ≥ 90%) and on the web code (lines ≥ 90%, statements ≥ 90%, functions ≥ 90%; branches ≥ 80%). Both must pass; this is stricter than one combined number.

- A26. "Reconnect after a network blip" for shells: a shell cannot survive its connection (spec, "Replica restarts and failures"), so after a dropped connection the terminal shows the spec's error and a one-click "Start a new shell". Watches and log streams do resume.
- A27. For merged logs (all pods of a workload or selector), the set of pods is fixed when the stream opens; a "Refresh sources" button re-resolves it. At most 20 streams per view.
- A28. Flux kinds that suspend by annotation use the value `disabled` to suspend and `enabled` to resume. The spec names the annotation keys but not the values; Step 5.7 requires confirming the values in the documentation the spec links before merging.
- A29. Karpenter's "scaling decisions" groups are derived from event reasons with the substring rules in Step 5.10, because the spec does not list reasons per group.
- A30. A Vault mount "without API server address or CA" shows up in two ways, both handled the same: the config read returns the fields empty, or (measured on Vault 2.2.1, see §2) the config read returns HTTP 404 because the mount was never configured.

**Integration testing**

- A23. Google and GitHub cannot be reached from CI (no external network, no real accounts). In the integration suite both are replaced by one stand-in container built from this repository (`test/integration/fakeidp`): a small OIDC issuer (with the `hd` claim) and the three GitHub endpoints Kubelook calls. This is the **only** stand-in in the integration suite. Vault, Kubernetes, PostgreSQL and Kubelook itself are real.
- A24. Ecosystem operators (Flux, Karpenter, KEDA, External Secrets, cert-manager, Trivy Operator, Kyverno, Gatekeeper, ricoberger Vault Secrets Operator, Gateway API) are not installed in the integration clusters. The suite installs hand-written minimal CRDs (kept in `test/integration/testdata/crds/`) on the real API server and creates custom resources with hand-set status. This tests Kubelook's discovery, listing, patching and module logic against a real API server without pulling third-party controllers. Behaviour that depends on a running operator (for example "Trivy rescans after a report is deleted") stays unconfirmed and is listed in `docs/spike-findings.md`.
- A25. Kubernetes refuses service-account tokens shorter than 10 minutes, so the "re-mint before expiry" test sets `vault.tokenTTL: 10m` and `vault.remintBefore: 9m50s` and waits (by polling a status field, with a deadline) for the re-mint. The "does a watch survive credential expiry" check needs ≥ 10 minutes of wall time and therefore runs only in the nightly workflow.

---

## §2 Verified and not verified

**Verified on 2026-11-11 while writing this plan**

- Every version in §3 exists in its registry (Go module proxy, npm registry, Docker Hub tag lists, upstream git tags).
- Every GitHub Action commit SHA in Step 0.7–0.9 is the commit that the named tag points to (annotated tags were peeled). The input names used in the workflows exist in each action's `action.yml` at that SHA.
- One Go module can import `helm.sh/helm/v3@v3.22.1`, `helm.sh/helm/v4@v4.3.1`, `k8s.io/client-go@v0.37.2`, `modernc.org/sqlite@v1.62.0` and `github.com/jackc/pgx/v5@v5.12.0` together and builds with Go 1.27.4 and `CGO_ENABLED=0`.
- `go-test-coverage` v2.23.0 with the `.testcoverage.yml` shown in Step 0.3 exits 1 below the threshold and 0 above it, and prints the lines quoted there.
- Go API facts quoted in the steps (`action.Configuration.Init` signatures for Helm 3 and 4, `kube/fake.PrintingKubeClient`, `storage.Init`, `driver.NewMemory`, `fake.NewClientset`, `remotecommand.NewWebSocketExecutor`, `clientcmd.Load`, `websocket.Accept`, `oauth2.S256ChallengeOption`, `oauth2.VerifierOption`).
- **Vault 2.2.1, run for real in dev mode** (image digest in §3), with the policies of Step 7.2:
  - `GET sys/internal/ui/mounts` with a policy-limited token lists only the Kubernetes mount that token has capabilities on (plus `cubbyhole/`, `identity/`, `sys/`); mounts of other users and other types are absent. Mount type is the string `kubernetes`.
  - `POST sys/capabilities-self` with several paths returns one entry per path under `data`; a path the token cannot use returns `["deny"]`; a root token returns `["root"]`; with a single path an extra `capabilities` key is present.
  - A token with only `create` on `kubernetes/<c>/creds/<role>` gets HTTP 403 when it posts to that path (the spec's "create alone is not enough").
  - `GET kubernetes/<c>/config` returns `kubernetes_host`, `kubernetes_ca_cert`, `disable_local_ca_jwt`. On a mount that was never configured it returns **HTTP 404** with `{"errors":[]}` (not empty fields), and writing a config without `kubernetes_host` outside a pod fails. See A30.
  - `auth/token/lookup-self` returns `ttl`, `renewable`, `type` (`service` or `batch`), `policies`, `entity_id` (empty for a token created directly with `vault token create`), `display_name`, `expire_time`; a batch token reports `renewable: false`; `auth/token/renew-self` returns the new TTL in `auth.lease_duration`; an invalid token gets 403.
  - A failing `creds` call (bad CA) returns **HTTP 500 with an `errors` list**, so 500 must not be treated as "Vault unreachable".
  - `GET sys/health?standbyok=true&perfstandbyok=true` returns 200 without a token; a paused Vault container makes requests time out.
- npm peer-dependency ranges: `typescript-eslint@8.72.2` requires `typescript <6.1.0` (hence TypeScript 6.0.3, not 7.x); `eslint-plugin-jsx-a11y@6.10.2` does not accept ESLint 10 (hence not used); `react-router@8.5.1` exports `BrowserRouter`, `MemoryRouter`, `Routes`, `Route`, `Link`, `NavLink`, `Outlet`, `useParams`, `useNavigate`, `useSearchParams`.

**Not verified**

- U1. Minting credentials (`POST kubernetes/<c>/creds/<role>`) against a real cluster, and everything else that needs Kubernetes. kind could not start in the planning sandbox: its host uses cgroup v1, and the kubelet in Kubernetes 1.35 refuses to run on cgroup v1 ("kubelet is configured to not run on a host using cgroup v1"). Step 7.2's acceptance checks minting first. **Consequence for the executor:** the machine that runs `make it-up` must use cgroup v2 (`stat -fc %T /sys/fs/cgroup` prints `cgroup2fs`). GitHub's `ubuntu-24.04` runners do.
- U2. The exact field names of Helm 4 SDK structs beyond those listed above. Steps 5.3–5.5 give `go doc` commands to run before writing adapter code.
- U3. Runtime behaviour of the frontend tool chain (Vite 8, Vitest 5, React Router 8). Step 0.4 proves the tool chain and the coverage gate before anything depends on them.
- U4. Image digests other than the ones printed in §3. Step 0.6 gives the command to resolve them.
- U5. Download hosts `get.helm.sh` (Helm CLI) and `go.dev` were blocked in the planning sandbox, so the Helm CLI download in `hack/install-tools.sh` was not run. The Helm version itself was verified from the upstream git tag.

**Known gaps in the Phase 0 tooling** (found in review; accepted for now, to be closed in the step named)

- G1. The ESLint rules of Step 0.4 do not catch `el.innerHTML = …`, a spread `dangerouslySetInnerHTML`, or `XMLHttpRequest`. Close in Step 1.11 (web shell, where `SafeLink` and the API client are added) with `no-restricted-syntax` selectors for `innerHTML` and `outerHTML` assignments and `no-restricted-globals` for `XMLHttpRequest`; prove each with a temporary offending line, as Step 0.4 does for coverage.
- G2. The `forbidigo` bans on `time.Now` and `fmt.Print*` in `.golangci.yml` match the written call, so an aliased import (`import t "time"`) bypasses them. Reviewers must reject aliased imports of `time` and `fmt`; revisit with `importas` or `depguard` if it ever happens.
- G3. `install-tools.sh` fetches the kubectl and Helm checksums from the same host as the binaries. The check catches corruption, not a compromised host. Pin the digests in the script if that matters.
- G4. The Vite dev proxy in `vite.config.ts` may need `ws: true` for the WebSocket path `/api/v1/ws`. Not verified for Vite 8; settle it in Step 3.8 when the first WebSocket client exists.
- G5. `make verify` runs `tsc` twice (see Step 0.4 pitfalls); accepted.

---

## §3 Technology choices and versions

Pin exactly these. Go modules are pinned in `go.mod`; npm packages are installed with `--save-exact`.

### Backend

| Choice | Version | Why |
| --- | --- | --- |
| Go | 1.27.4 | Required by the spec (client-go, Helm SDKs are Go); newest stable; dependencies need ≥ 1.26 |
| `k8s.io/client-go`, `k8s.io/api`, `k8s.io/apimachinery` | v0.37.2 | Newest; the line both Helm SDKs build against (they require v0.37.1). `k8s.io/kubectl` is deliberately not imported: drain is implemented in Step 4.12 so it can use the fake clock |
| `helm.sh/helm/v4` | v4.3.1 | Spec: Helm 4 is the default SDK |
| `helm.sh/helm/v3` | v3.22.1 | Spec: Helm 3 selectable with `helm.version: 3` |
| `net/http` `ServeMux` (standard library) | Go 1.27 | Method and wildcard patterns are built in; no router dependency |
| `github.com/coder/websocket` | v1.8.15 | Context-aware, maintained WebSocket library with origin checking built into `Accept` |
| `modernc.org/sqlite` | v1.62.0 | Pure-Go SQLite: static binary with `CGO_ENABLED=0`, runs in a distroless image |
| `github.com/jackc/pgx/v5` (`stdlib` adapter) | v5.12.0 | Standard PostgreSQL driver; used through `database/sql` so SQLite and PostgreSQL share code |
| Hand-written migrator (Step 1.4) | — | ~60 lines over `database/sql`; fully unit-testable; avoids a dependency for two dialects |
| Hand-written Vault HTTP client (Step 2.5) | — | Kubelook calls six endpoints with a per-request token; `net/http` + `httptest` is simpler and fully fakeable |
| `golang.org/x/oauth2` | v0.38.0 | Authorization-code flow with PKCE helpers |
| `github.com/coreos/go-oidc/v3` | v3.21.0 | ID-token verification and OIDC discovery (Google sign-in, kubeconfig OIDC refresh) |
| `sigs.k8s.io/yaml` | v1.6.0 | YAML↔JSON used across Kubernetes; one YAML library for config and manifests |
| `github.com/prometheus/client_golang` | v1.25.0 | Spec: Prometheus metrics |
| `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/sdk`, `…/exporters/otlp/otlptrace/otlptracegrpc` | v1.47.0 | Spec: OpenTelemetry traces |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | v0.72.0 | HTTP server and client spans |
| `log/slog` (standard library) | Go 1.27 | Structured JSON logs without a dependency |
| `github.com/stretchr/testify` | v1.12.1 | `require`/`assert` in tests |
| `go.uber.org/goleak` | v1.3.0 | Fails tests that leak goroutines (hub, streams) |

### Frontend

| Choice | Version | Why |
| --- | --- | --- |
| Node.js | 26.13.0 (LTS "Lithium") | Current LTS; satisfies every package's `engines` |
| npm | the one bundled with Node 26.13.0 | No extra package manager to install |
| TypeScript | 6.0.3 | Newest version `typescript-eslint` 8.72.2 accepts |
| React, React DOM | 19.3.1 | Spec: React |
| Vite, `@vitejs/plugin-react` | 8.3.7, 6.3.0 | Build and dev server |
| Vitest, `@vitest/coverage-v8` | 5.1.0, 5.1.0 | Unit tests and coverage thresholds in one tool |
| `@testing-library/react`, `@testing-library/dom`, `@testing-library/user-event`, `@testing-library/jest-dom`, `jsdom` | 16.3.3, `^10` (latest 10.x at install), 14.6.9, 7.0.1, 31.0.0 | Component tests by role and text |
| `fake-indexeddb` | 6.2.8 | In-memory IndexedDB for tests of the saved-credential store |
| React Router (`react-router`) | 8.5.1 | Deep links |
| `@tanstack/react-query` | 5.107.1 | Server-state cache for REST calls |
| `zustand` | 5.1.0 | Small client-state store (dock, live resource store) |
| `monaco-editor`, `@monaco-editor/react`, `monaco-yaml` | 0.57.0, 4.7.0, 5.5.1 | Spec: Monaco YAML editor with schema validation |
| `@xterm/xterm`, `@xterm/addon-fit` | 6.0.0, 0.11.0 | Spec: xterm.js terminals |
| `d3-force` | 3.0.1 | Force layout for the resource map and Flux graph |
| `yaml` | 2.9.1 | YAML parse/print in the browser |
| `idb` | 8.0.4 | Promise wrapper for IndexedDB |
| ESLint, `typescript-eslint`, `eslint-plugin-react-hooks` | 10.14.0, 8.72.2, 7.1.1 | Lint |
| Prettier | 3.9.10 | Formatting |
| `@playwright/test` | 1.65.0 | Browser UI tests of the web app with a mocked API (`web/ui`, from Phase 2, run in CI job `ui-tests`) and full-stack browser tests (`test/e2e`, Phase 7) |
| CSS | plain CSS files with custom properties | No UI framework: fewer dependencies, themes via variables |

### Tooling, images, CI

| Choice | Version | Why |
| --- | --- | --- |
| golangci-lint | v2.15.0 | Go lint and formatting |
| `github.com/vladopajic/go-test-coverage/v2` | v2.23.0 | Coverage threshold with path exclusions |
| `golang.org/x/vuln/cmd/govulncheck` | v1.9.0 | Go vulnerability check |
| kind | v0.33.0 | Spec'd by the task: Kubernetes clusters for integration tests |
| kind node images | `kindest/node:v1.35.8@sha256:07b2536e30b803ed61d1677a79df6115f798ce64c80f9e22f6ed45afd09323c0` and `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5` | Oldest supported Kubernetes (1.35) and newest |
| kubectl | v1.37.2 | Test setup |
| Helm CLI | v4.3.1 | Chart lint, template, install |
| Vault image | `hashicorp/vault:2.2.1@sha256:962a92876e168d68eaaa24b0b8b49bbce1cb63024060a67bba95cd158a366fc2` | Newest Vault (see U1) |
| PostgreSQL image | `postgres:18.6-trixie` | Newest stable PostgreSQL |
| Build images | `golang:1.27.4-trixie`, `node:26.13.0-trixie` | Match tool versions. Digests pinned in the `Dockerfile` |
| Runtime base image | `gcr.io/distroless/static-debian13:nonroot` | No shell, non-root, small attack surface. Digest pinned in the `Dockerfile` |
| Debug image base | `nicolaka/netshoot:v0.16` | Spec: default debug image is built from netshoot |
| Trivy | via `aquasecurity/trivy-action` v0.36.0 | Image vulnerability scan |
| chart-testing | via `helm/chart-testing-action` v2.9.0 | Chart lint and install test |
| CI | GitHub Actions on `ubuntu-24.04` | Required by the task |

---

## §4 Repository layout

```
.
├── .github/
│   ├── dependabot.yml
│   └── workflows/{ci.yml, nightly.yml, netshoot.yml}
├── .golangci.yml  .testcoverage.yml  .editorconfig  .gitignore  .dockerignore
├── Dockerfile  Makefile  go.mod  go.sum  PLAN.md  README.md  LICENSE
├── cmd/kubelook/main.go              # only calls app.Main; excluded from coverage
├── internal/
│   ├── app/          # command dispatch (serve, migrate, audit-cleanup, version), wiring, shutdown
│   ├── version/      # build version
│   ├── config/       # config file + env loading and validation
│   ├── clock/        # Clock interface, real and fake clocks
│   ├── crypto/       # AES-256-GCM envelopes with key ID
│   ├── store/        # database/sql access for SQLite and PostgreSQL, migrations (embedded .sql)
│   ├── audit/        # audit recorder (DB + stdout), cleanup
│   ├── session/      # sessions, cookie, CSRF, idle/absolute expiry, connection registry
│   ├── auth/         # OAuth: state cookie, Google, GitHub, allowlists
│   ├── vault/        # Vault HTTP client and cluster discovery
│   ├── kubeconfig/   # kubeconfig parsing and validation
│   ├── cluster/      # cluster registry per session, opener, credential broker, REST config
│   ├── kube/         # client factory, discovery cache, RBAC checks, generic resource operations
│   ├── redact/       # Secret masking and object pruning
│   ├── ws/           # WebSocket connection, channel multiplexing, heartbeat
│   ├── watch/        # watch hub
│   ├── logs/         # log streams, structured-log parsing, history
│   ├── execs/        # shells, debug containers, node shell
│   ├── files/        # pod file browser
│   ├── jobs/         # durable jobs: store, runner, lease, recovery
│   ├── actions/      # workload, node, flux and module actions
│   ├── certs/        # PEM certificate metadata
│   ├── helm/         # Driver interface; helm3/ and helm4/ adapters; repositories
│   ├── flux/         # Flux kinds, status summary, inventory
│   ├── modules/      # module registry: karpenter, keda, externalsecrets, vaultsecrets, certmanager, gatewayapi, trivy, permissions
│   ├── resourcemap/  # graph builder
│   ├── overview/     # cluster overview triage
│   ├── metrics/      # metrics-server and Prometheus pod/node metrics
│   ├── telemetry/    # Prometheus metrics registry, OpenTelemetry setup, memory guard
│   ├── httpapi/      # router, middleware, handlers (thin: decode → call package → encode)
│   ├── webui/        # embedded built SPA (dist/) and static handler
│   └── testutil/     # fakes shared by unit tests (fakevault, fakeoidc, fakekube, helmtest); excluded from coverage
├── web/              # React SPA (see Step 0.4 for its inner layout)
├── deploy/
│   ├── helm/kubelook/        # Helm chart
│   └── postgres/roles.sql    # hardened PostgreSQL roles and row-level security
├── build/netshoot/Dockerfile # Kubelook's debug image
├── hack/             # scripts: install-tools.sh, it-up.sh, it-down.sh, vault-setup.sh, …
├── test/
│   ├── integration/  # Go tests with build tag `integration`, fakeidp, testdata
│   ├── e2e/          # Playwright tests
│   └── load/         # watch-hub load test (build tag `load`)
└── docs/             # spec.md, deployment.md, configuration.md, operations.md, vault-policy.md, …
```

Package dependency rule (enforced by review, and by `depguard` in Step 0.3): `internal/httpapi` may import any internal package; no internal package may import `internal/httpapi` or `internal/app`; `internal/store`, `internal/crypto`, `internal/clock`, `internal/config` import no other internal package except `clock`.

---

## §5 Conventions

### 5.1 The TDD procedure for every step

1. **Red.** Create the test file(s) named in the step with the listed cases. Run the step's Red command. It must fail for the stated reason (for Go a compile error such as `undefined: crypto.Seal` counts as the expected failure; for web, `Failed to resolve import` or a failed assertion). If it passes, the test is wrong: fix the test. Commit.
2. **Green.** Write the smallest implementation that makes the Red command pass. Do not add behaviour that no test asks for. Run the Green command; it must print `ok` (Go) or `Test Files  N passed` (Vitest) with zero failures. Commit.
3. **Refactor.** Apply the cleanups the step lists (and only those plus removal of duplication you introduced). Re-run the same command; it must still pass. Commit if anything changed.
4. Run `make verify`. It must end with `VERIFY OK`. This includes lint, all unit tests, both coverage gates and the build.
5. Push. CI must be green on the step's last commit before the next step starts. After the last step of the pull request (§7), mark it ready; merge only when every CI job is green.

### 5.2 Commands (defined in the Makefile, Step 0.1 onwards)

| Command | What it runs | Passing output ends with |
| --- | --- | --- |
| `make test-backend PKG=./internal/x/...` | `go test -race -count=1 -shuffle=on $(PKG)` | `ok  	github.com/bdnix/kubelook/internal/x` |
| `make test-frontend FILE=src/x.test.tsx` | `cd web && npx vitest run $(FILE)` | `Test Files  1 passed` |
| `make test-ui` | `cd web && npx playwright test` (builds the app, serves it, drives Chromium) | `N passed` |
| `make lint` | golangci-lint, ESLint, Prettier check, `tsc --noEmit` | `LINT OK` |
| `make coverage` | Go and web unit tests with coverage and both gates | `COVERAGE OK` |
| `make build` | web build, embed, Go build to `bin/kubelook` | `BUILD OK` |
| `make verify` | `lint`, `coverage`, `build` | `VERIFY OK` |
| `make it` | integration suite (Phase 7) | `INTEGRATION OK` |

### 5.3 Go conventions

- Module path `github.com/bdnix/kubelook`. All code under `internal/`.
- Every dependency on time, randomness, the network or another subsystem is an **interface passed into a constructor**. Constructors are `New(deps Deps) *T` where `Deps` is a struct. No package-level mutable state, no `init()` side effects.
- Time: use `clock.Clock` (Step 1.2), never `time.Now()`, `time.Sleep`, `time.After`, `time.NewTimer` or `time.NewTicker` directly outside `internal/clock`. Lint enforces this.
- Errors: wrap with `fmt.Errorf("doing x: %w", err)`. Sentinel errors are package-level `var ErrX = errors.New(...)`. HTTP handlers map errors with the single function `httpapi.writeError` (Step 1.8).
- Context: every function that does I/O takes `ctx context.Context` first.
- Logging: `*slog.Logger` passed in `Deps`; never log secrets, tokens, Secret values, file content or log lines.
- Tests: table-driven where there are more than two cases; `require` for preconditions, `assert` for checks; test names `TestThing_Condition_Result`. Tests live next to the code as `x_test.go` in package `x_test` (black-box) unless the step says otherwise.
- Fakes, not mocks frameworks: hand-written fakes that implement the interface. Shared fakes live in `internal/testutil`.

### 5.4 Web conventions

- All code in `web/src`. Tests next to the code as `X.test.ts(x)`.
- Components are functions; state shared across views goes in `zustand` stores under `web/src/state/`; server data goes through `@tanstack/react-query` hooks under `web/src/api/`.
- Everything that touches `fetch`, `WebSocket`, `indexedDB`, `Date.now`, `setTimeout` or `document.visibilityState` receives it through a parameter or a React context with a default, so tests pass fakes. Tests use `vi.useFakeTimers()` for time.
- Cluster-supplied strings are rendered as React text children only. Never use `dangerouslySetInnerHTML`. Links are created only through `SafeLink` (Step 1.11), which accepts only `http:` and `https:` URLs.
- Monaco and xterm are used only inside `web/src/adapters/monaco/` and `web/src/adapters/xterm/`. Everything else talks to the adapter's props. Tests replace the adapters with `vi.mock`.

### 5.5 HTTP API conventions

- REST base path `/api/v1`. JSON bodies. Field names `camelCase`.
- Errors: `{"error":{"code":"<machine_code>","message":"<text for the user>"}}` with a matching HTTP status. Codes used across the plan: `bad_request` 400, `unauthenticated` 401, `forbidden` 403, `csrf` 403, `not_found` 404, `conflict` 409, `too_large` 413, `unprocessable` 422, `server_busy` 503, `vault_unreachable` 503, `cluster_unavailable` 404, `key_changed` 409, `internal` 500.
- Every request that is not `GET`, `HEAD` or `OPTIONS` must carry `X-CSRF-Token`.
- Cluster in a path is the percent-encoded cluster ID: `/api/v1/clusters/{cluster}/…`. The empty API group is written `core` in paths.

### 5.6 Determinism rules (tests)

- No `time.Sleep` and no fixed waits anywhere in unit tests. Use the fake clock and wait on channels. A `select` with `case <-time.After(5*time.Second): t.Fatal("timeout")` is allowed **only** as a failure bound, written through the helper `testutil.Recv(t, ch)`.
- No real network in unit tests except loopback `httptest` servers created by the test.
- Random values come from an injected `io.Reader` (`crypto/rand.Reader` in production; a fixed-seed reader in tests) or an injected ID function.
- Map iteration order must never reach output: sort before returning lists.
- Go tests run with `-race -shuffle=on`. Packages that start goroutines call `goleak.VerifyTestMain(m)` in `main_test.go`.
- Integration tests wait with `it.Eventually(t, deadline, func() bool)` (condition polling with a deadline) or `kubectl wait`; never a bare sleep.

### 5.7 Git and CI conventions

- Commits follow Conventional Commits. Do not put session URLs or tool names in commits or PR text.
- Pull request titles follow `<type>(pr<n>): <what the pull request delivers>`, and the body lists the steps it contains with their acceptance results. Reviewers read a pull request step by step, using the `test(<id>)` / `feat(<id>)` commits.
- Never push to `main` directly. Never merge with a red or skipped required check. Never disable a test or lower a threshold to get green.

---

## §6 Test strategy

### Unit tests (count toward the 90% gate)

- **What they cover:** all logic. Config parsing and validation, crypto envelopes, SQL repositories, session and CSRF rules, OAuth state handling and allowlists, Vault request/response handling and discovery filtering, kubeconfig validation, credential re-mint timing, redaction, the WebSocket protocol, watch-hub fan-out/resume/410 handling, log parsing and back-pressure, exec framing, file-browser command building and tar checks, job leases and recovery decisions, Helm adapters, Flux/module status logic, graph building, filters, exports, HTTP handlers, and every React component, hook and store.
- **How external systems are replaced, at these interfaces only:**

  | External system | Interface seam | Fake used in unit tests |
  | --- | --- | --- |
  | Time | `clock.Clock` | `clock.Fake` |
  | Database | `*sql.DB` | real SQLite on a file in `t.TempDir()` (in-process, no network) |
  | Vault | `vault.API` (and under it, HTTP) | `httptest` server in `internal/testutil/fakevault` |
  | Google / GitHub | provider base URLs | `httptest` servers in the auth tests |
  | Kubernetes API | `kube.ClientFactory` returning client-go interfaces | client-go `dynamic/fake`, `kubernetes/fake`, `discovery/fake`; `httptest` server for raw REST paths |
  | Kubernetes exec / logs streams | `execs.Executor`, `logs.Source` | in-memory pipes |
  | Helm storage and cluster | Helm `action.Configuration` | Helm's memory storage driver and `kube/fake.PrintingKubeClient` |
  | Browser APIs | parameters / React context | `vi.fn()` fetch, fake WebSocket class, `fake-indexeddb`, fake timers |

- **Tools and thresholds:** see Step 0.3 (Go) and Step 0.4 (web). Total Go statement coverage ≥ 90%; web lines/statements/functions ≥ 90%, branches ≥ 80%.
- **Excluded from coverage, with reasons (nothing else may be excluded):**

  | Path | Reason |
  | --- | --- |
  | `cmd/kubelook/main.go` | Three lines that call `app.Main` and `os.Exit`; cannot be run in-process |
  | `internal/testutil/**` | Test helpers, not product code |
  | `test/**`, `hack/**` | Integration, load and tooling code; not part of the product binary (also behind build tags) |
  | `web/src/main.tsx` | Browser bootstrap (`createRoot`) |
  | `web/src/adapters/monaco/**`, `web/src/adapters/xterm/**` | Thin wrappers around libraries that need a real browser (canvas, workers, layout); exercised by Playwright in Step 7.10 |
  | `web/src/**/*.test.*`, `web/src/test/**`, `web/src/**/*.d.ts` | Tests and type declarations |

  There is no generated code in this plan. If any is ever added, it must be listed here with a reason in the same pull request.

### UI tests (browser, run on every pull request)

- **What they cover:** what jsdom cannot prove: the built app in real Chromium, real IndexedDB (saved credential copies surviving a reload), real navigation and URL encoding, keyboard and focus behaviour, and the user journeys of a phase end to end through the screen. Every phase that adds screens adds UI tests for its main journeys in the same pull request (Phase 2: sign-in page and its error texts, adding a kubeconfig, the cluster rail, restore after reload, key-change notice).
- **How:** Playwright (`web/ui/*.ui.ts`, config `web/playwright.config.ts`) serves the production build with `vite preview` and answers `/api/v1/**` from the tests with `page.route`. No backend, Vault or cluster is involved, so they take seconds and never flake on infrastructure. Locators are role- and text-based (`getByRole`, `getByLabel`), assertions are auto-waiting, and there is no `waitForTimeout`.
- **Run:** `make test-ui` locally (set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` to reuse an installed Chromium), CI job `ui-tests`. Failures upload the Playwright report and traces. They are not coverage-counted. `make verify` does not run them because it must work without a browser.
- **Relation to the other layers:** unit tests (jsdom) prove component logic; UI tests prove the screens in a real browser against a stand-in API; the Phase 7 browser tests (`test/e2e`) prove the same screens against the real stack, including Monaco and xterm.

### Integration tests (separate CI job, never counted in coverage)

- **What they cover:** behaviour that only a real system can prove. Vault: token lookup, mount listing limited by policy, capability checks (`update` vs `create`), reading mount config, minting credentials, renewal, leases. Kubernetes (two kind clusters, 1.35 and 1.37): list/watch/resume, logs, exec over WebSocket, ephemeral containers, server-side dry run, optimistic-concurrency conflicts, eviction and drain with PodDisruptionBudgets, `SelfSubjectAccessReview`, CRD discovery, Helm 3 and Helm 4 installs. PostgreSQL: both dialects, the insert-only audit role and row-level security. Kubelook as a container image: sign-in flows, sessions across two replicas, logout closing every connection, the browser UI through Playwright (including Monaco and xterm).
- They are Go tests under `test/integration` with build tag `integration` (so `go test ./...` never compiles them) plus Playwright tests under `test/e2e`.
- They run with `make it` locally and in the CI job `integration-tests` (in `ci.yml`, so publishing waits for them). They must pass for a pull request to merge from pull request 9 (Phase 7) on.

---

## §7 Step dependency map

Steps are executed in numeric order; this map shows the real dependencies so a failure can be traced.

```
0.1 → 0.2 → 0.3 → 0.4 → 0.5 → 0.6 → 0.7 → 0.8 → 0.9
1.1 config ─┬→ 1.4 store ─→ 1.5 users ─→ 1.6 sessions ─→ 1.7 audit ─→ 1.9 session mgr ─→ 1.10 app wiring
1.2 clock ──┤                                              1.8 http ───┘                    1.11 web shell
1.3 crypto ─┘
2.1 oauth core → 2.2 google, 2.3 github → 2.4 sign-in/out
2.5 vault client → 2.6 discovery → 2.7 saved credentials (needs 1.3, 1.6)
2.8 kubeconfig → 2.9 cluster list (needs 2.6, 2.8) → 2.10 web sign-in + credentials
3.1 broker (needs 2.5–2.9) → 3.2 client factory → 3.4 resource read API (needs 3.3 redact) → 3.5 RBAC
3.6 ws → 3.7 watch hub (needs 3.2, 3.3) → 3.8 web live lists → 3.9 logs → 3.10 logs web → 3.11 vault outage
4.1 writes (needs 1.7, 3.4) → 4.2 … 4.19 (each needs 3.x; 4.12 needs 4.11; 4.8 needs 4.7; 4.10 needs 4.9)
5.1 CRDs → 5.2 module framework → 5.10 … 5.14;  5.3 → 5.4 → 5.5 (needs 4.11) → 5.6;  5.7 → 5.8;  5.9;  5.15
6.1 → 6.2 → 6.3 → 6.4
7.1 → 7.2 → 7.3 → 7.4 … 7.10 → 7.11
8.1 → 8.2 → 8.3 → 8.4 → 8.5 → 8.6
```

### Pull request map

The whole plan is delivered in 10 pull requests. Each ends in a state where `main` builds, passes `make verify` and is green in CI.

| PR | Steps | Delivers | Ends with |
| --- | --- | --- | --- |
| 1 | 0.1–0.9 | Scaffold, `version` command, web app, embed, Dockerfile, **the CI pipeline**, image scan and push, Dependabot | The `ci` workflow green on the pull request itself: lint, tests, both coverage gates, build, vulnerability check, image scan |
| 2 | 1.1–1.11 | Foundations: config, clock, crypto, store, users, sessions, audit, HTTP server, session manager, `serve`/`migrate`, web shell | A server that starts and serves the web shell |
| 3 | 2.1–2.10 | Sign-in (Google, GitHub), Vault client and discovery, saved credentials, kubeconfig upload, cluster list, sign-in UI | A user can sign in and see their clusters |
| 4 | 3.1–3.11 | Credential broker, Kubernetes clients, redaction, read API, RBAC checks, WebSocket, watch hub, live lists, logs, Vault outage handling | Live resource lists and log streaming |
| 5 | 4.1–4.10 | Writes with audit, kind registry, details drawer, YAML editor, workload actions, Secret reveal, shells, debug toolkit, pod file browser | Day-to-day operations on workloads |
| 6 | 4.11–4.19 | Durable jobs, node actions and drain, full logs, field filter, export, settings, navigation, metrics, "who can" | All core features of the spec |
| 7 | 5.1–5.15 | Custom resources, module framework, Helm 3 and 4, Flux, resource map, ecosystem modules, cluster overview | All platform features of the spec |
| 8 | 6.1–6.4 | Prometheus metrics, traces and logs, memory guard, load test | Hardening complete |
| 9 | 7.1–7.11, 8.1 part A | Integration and browser suites on kind + Vault as required checks; nightly run; spike findings; the debug image published | The CI job `integration-tests` green on the pull request itself |
| 10 | 8.1 part B, 8.2–8.6 | Debug image digest pin, Helm chart, chart test, publishing from `main` (versions, image, chart, releases), documentation, final verification | §8 Definition of done |

Rules for this grouping:

- Pull request 1 **must** contain the pipeline (Steps 0.7–0.9). Nothing is merged to `main` before CI exists, so every merge in the project's history was checked by CI.
- Phase 4 is split after Step 4.10 because 4.11 (durable jobs) starts a new dependency chain (4.12 and 5.5 need it); nothing in 4.1–4.10 depends on 4.11–4.19.
- Step 8.1 needs two merges (the image must be published from `main` before its digest can be pinned), so its part A rides in pull request 9 and part B in pull request 10.
- Do not split or merge these groups without the repository owner's agreement. If a pull request must be split because of size, split only at a step boundary.

---
# Phase 0 — Repository and CI/CD

**Delivered in pull request 1** (all of Phase 0; it is not merged until Steps 0.7–0.9 have added the pipeline and it is green).

Outcome: an empty-but-real application (a `kubelook version` command and a one-component web page) with the full pipeline around it: lint, unit tests, a 90% coverage gate that is proven to fail, build, container image with vulnerability scan, push to GHCR, Dependabot. Every later step lands inside this pipeline.

Prerequisites on the executor's machine: Git, Docker (with buildx), Go 1.27.4, Node.js 26.13.0, GNU Make, bash, curl.

### Step 0.1 — Scaffolding, Makefile, tool pins

**Goal.** Create the module, the Makefile that every later step uses, and the tool installer.

**Files.** Create `go.mod`, `Makefile`, `.gitignore`, `.editorconfig`, `hack/install-tools.sh`, `web/.nvmrc`.

**Commands.**

```bash
go mod init github.com/bdnix/kubelook
go mod edit -go=1.27.4
printf '26.13.0\n' > web/.nvmrc      # create web/ first: mkdir -p web
chmod +x hack/install-tools.sh
```

`Makefile` (complete; later steps only add targets where they say so). `.NOTPARALLEL:` is deliberate: the web build rewrites `internal/webui/dist`, which the Go build embeds, so targets must never run in parallel. `make clean` removes only `bin/kubelook`, never the tools that `make tools` installs into `bin/`:

```make
SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := verify

# The web build rewrites internal/webui/dist, which the Go build embeds: never run targets in parallel.
.NOTPARALLEL:

BIN := $(CURDIR)/bin
export PATH := $(BIN):$(PATH)
PKG ?= ./...
FILE ?=
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X github.com/bdnix/kubelook/internal/version.Version=$(VERSION) -X github.com/bdnix/kubelook/internal/version.Commit=$(COMMIT)
COVER_TOOL := github.com/vladopajic/go-test-coverage/v2@v2.23.0

.PHONY: tools fmt lint lint-backend lint-frontend test test-backend test-frontend test-ui coverage coverage-backend coverage-frontend build build-frontend build-backend image verify clean

tools:
	hack/install-tools.sh

fmt:
	golangci-lint fmt
	cd web && npx prettier --write .

lint-backend:
	golangci-lint run

lint-frontend:
	cd web && npm run lint && npm run format:check && npm run typecheck

lint: lint-backend lint-frontend
	@echo "LINT OK"

test-backend:
	go test -race -count=1 -shuffle=on $(PKG)

test-frontend:
	cd web && npx vitest run $(FILE)

test: test-backend test-frontend

# Browser UI tests (Playwright, Chromium): the built web app with the API answered by the tests.
# Needs a browser: `cd web && npx playwright install --with-deps chromium`, or set
# PLAYWRIGHT_CHROMIUM_EXECUTABLE to an existing Chromium. Not part of `verify` (it needs a browser).
test-ui:
	cd web && npx playwright test

coverage-backend:
	go test -race -count=1 -shuffle=on -covermode=atomic -coverprofile=coverage.out ./...
	go run $(COVER_TOOL) --config=.testcoverage.yml

coverage-frontend:
	cd web && npx vitest run --coverage

coverage: coverage-backend coverage-frontend
	@echo "COVERAGE OK"

build-frontend:
	cd web && npm run build
	mkdir -p internal/webui/dist
	find internal/webui/dist -mindepth 1 ! -name placeholder.txt -delete
	cp -R web/dist/. internal/webui/dist/

build-backend:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/kubelook ./cmd/kubelook

build: build-frontend build-backend
	@echo "BUILD OK"

image:
	@test -f Dockerfile || { echo "no Dockerfile yet (added in Step 0.6)" >&2; exit 1; }
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t kubelook:$(VERSION) .

verify: lint coverage build
	@echo "VERIFY OK"

clean:
	rm -rf $(BIN)/kubelook coverage.out web/dist web/coverage
	mkdir -p internal/webui/dist
	find internal/webui/dist -mindepth 1 ! -name placeholder.txt -delete
```

`hack/install-tools.sh` (installs into `./bin`; safe to re-run). It builds the Go tools with the Go version written in `go.mod` (`GOTOOLCHAIN`), because a lint binary built with an older Go cannot load a module that targets a newer one. Installed versions are compared exactly (`grep -Fqw`), downloads go to a temporary directory and are checksum-verified before they are moved into `bin/`, and `shasum` is used where `sha256sum` is missing (macOS). The checksums come from the same host as the binaries, so they catch corruption, not a compromised host:

```bash
#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export GOBIN="$ROOT/bin"
# Build the tools with the same Go as the module so lint can load its code.
export GOTOOLCHAIN="go$(sed -n 's/^go //p' "$ROOT/go.mod")"
mkdir -p "$GOBIN"
OS="$(go env GOOS)"; ARCH="$(go env GOARCH)"

# sha256 check that works with GNU coreutils and with stock macOS.
sha256_check() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum -c -; else shasum -a 256 -c -; fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

GOLANGCI_VERSION=v2.15.0
GOVULNCHECK_VERSION=v1.9.0
KIND_VERSION=v0.33.0
KUBECTL_VERSION=v1.37.2
HELM_VERSION=v4.3.1

go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
go install "sigs.k8s.io/kind@${KIND_VERSION}"

have_kubectl="$("$GOBIN/kubectl" version --client 2>/dev/null || true)"
if ! grep -Fqw "${KUBECTL_VERSION}" <<<"$have_kubectl"; then
  curl -fsSLo "$tmp/kubectl" "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH}/kubectl"
  echo "$(curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH}/kubectl.sha256")  $tmp/kubectl" | sha256_check
  chmod +x "$tmp/kubectl"
  mv "$tmp/kubectl" "$GOBIN/kubectl"
fi

have_helm="$("$GOBIN/helm" version --short 2>/dev/null || true)"
if ! grep -Fqw "${HELM_VERSION}" <<<"$have_helm"; then
  curl -fsSLo "$tmp/helm.tgz" "https://get.helm.sh/helm-${HELM_VERSION}-${OS}-${ARCH}.tar.gz"
  echo "$(curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-${OS}-${ARCH}.tar.gz.sha256sum" | awk '{print $1}')  $tmp/helm.tgz" | sha256_check
  tar -xzf "$tmp/helm.tgz" -C "$tmp"
  mv "$tmp/${OS}-${ARCH}/helm" "$GOBIN/helm"
fi
echo "TOOLS OK"
```

`.gitignore`:

```
/bin/
/coverage.out
/web/node_modules/
/web/dist/
/web/coverage/
/internal/webui/dist/*
!/internal/webui/dist/placeholder.txt
/test/e2e/node_modules/
/test/e2e/test-results/
/.it/
*.local
```

`.editorconfig`: `root = true`; `[*]` with `end_of_line = lf`, `insert_final_newline = true`, `charset = utf-8`, `indent_style = space`, `indent_size = 2`; `[*.go]` and `[Makefile]` with `indent_style = tab`.

**Red / Green / Refactor.** Not applicable: this step adds no product code (configuration only). The first test is written in Step 0.2.

**Acceptance.**

```bash
make tools            # last line: TOOLS OK
bin/golangci-lint version | grep -c '2.15.0'      # 1
bin/kind version | grep -c 'v0.33.0'              # 1
bin/kubectl version --client | grep -c 'v1.37.2'  # 1
bin/helm version --short | grep -c 'v4.3.1'       # 1
```

**Pitfalls.**
- Makefile recipe lines must start with a tab, not spaces.
- The macOS `shasum` fallback in `install-tools.sh` has not been run; CI and the reference environment are Linux.
- `make verify` does not work yet (no code, no web). It first works at the end of Step 0.5. Until then use the per-step acceptance commands.

### Step 0.2 — First unit, test-first: `version` and the command entry point

**Goal.** Establish the TDD pattern with the smallest real unit: `kubelook version` prints the build version. All command dispatch lives in `internal/app` so it is unit-testable; `main.go` is three lines.

**Files.** Create `internal/version/version.go`, `internal/version/version_test.go`, `internal/app/app.go`, `internal/app/app_test.go`, `cmd/kubelook/main.go`.

**Red.** `internal/version/version_test.go`:

```go
package version_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/version"
)

func TestString_DefaultValues_ReturnsDevUnknown(t *testing.T) {
	assert.Equal(t, "kubelook dev (commit unknown)", version.String())
}

func TestString_SetValues_ReturnsThem(t *testing.T) {
	oldV, oldC := version.Version, version.Commit
	t.Cleanup(func() { version.Version, version.Commit = oldV, oldC })
	version.Version, version.Commit = "1.2.3", "abc1234"
	assert.Equal(t, "kubelook 1.2.3 (commit abc1234)", version.String())
}
```

`internal/app/app_test.go`:

```go
package app_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/app"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = app.Main(args, &out, &errb, func(string) string { return "" })
	return code, out.String(), errb.String()
}

func TestMain_Version_PrintsVersionAndReturns0(t *testing.T) {
	code, out, _ := run("version")
	assert.Equal(t, 0, code)
	assert.Equal(t, "kubelook dev (commit unknown)\n", out)
}

func TestMain_NoArgs_PrintsUsageAndReturns2(t *testing.T) {
	code, _, errOut := run()
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "usage: kubelook <serve|migrate|audit-cleanup|version>")
}

func TestMain_UnknownCommand_PrintsUsageAndReturns2(t *testing.T) {
	code, _, errOut := run("bogus")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, `unknown command "bogus"`)
}
```

Run:

```bash
go get github.com/stretchr/testify@v1.12.1
make test-backend PKG="./internal/version/... ./internal/app/..."
```

Expected failure: build errors `undefined: version.String`, `undefined: app.Main` (packages do not exist yet).

**Green.**

```go
// internal/version/version.go

// Package version holds the build version.
package version

import "fmt"

// Version and Commit are set at build time with -ldflags -X.
var (
	Version = "dev"
	Commit  = "unknown"
)

// String returns the human-readable version line.
func String() string { return fmt.Sprintf("kubelook %s (commit %s)", Version, Commit) }
```

```go
// internal/app/app.go

// Package app dispatches the kubelook commands.
package app

import (
	"fmt"
	"io"

	"github.com/bdnix/kubelook/internal/version"
)

const usage = "usage: kubelook <serve|migrate|audit-cleanup|version>"

// Main is the whole program. args excludes the program name. It returns the exit code.
func Main(args []string, stdout, stderr io.Writer, _ func(string) string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		_, _ = fmt.Fprintln(stdout, version.String())
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n%s\n", args[0], usage)
		return 2
	}
}
```

```go
// cmd/kubelook/main.go

// Command kubelook is the Kubelook server and its maintenance commands.
package main

import (
	"os"

	"github.com/bdnix/kubelook/internal/app"
)

func main() { os.Exit(app.Main(os.Args[1:], os.Stdout, os.Stderr, os.Getenv)) }
```

The code above already satisfies the linters of Step 0.3 (package comments, checked-or-discarded write errors, no unused parameter name). The first Red run fails with `missing go.sum entry … go.yaml.in/yaml/v3` instead of `undefined: …` until `go mod tidy` has run; that still counts as the expected failure. Run `go mod tidy && make test-backend PKG="./internal/version/... ./internal/app/..."`. Expected: two `ok` lines.

**Refactor.** None needed. Re-run the command; still `ok`.

**Acceptance.**

```bash
make test-backend                                   # all packages ok
go run ./cmd/kubelook version                  # kubelook dev (commit unknown)
go run ./cmd/kubelook; echo "exit=$?"          # usage line, exit=2
```

**Pitfalls.** `serve`, `migrate` and `audit-cleanup` are in the usage text but not implemented until Steps 1.7 and 1.10; the default branch handles them until then. Do not add them early.

### Step 0.3 — Go lint and the Go coverage gate

**Goal.** Lint configuration, and a coverage gate at 90% that is proven to fail when coverage is low.

**Files.** Create `.golangci.yml`, `.testcoverage.yml`.

`.golangci.yml`:

```yaml
version: "2"
run:
  timeout: 5m
linters:
  default: standard
  enable:
    - bodyclose
    - depguard
    - errorlint
    - forbidigo
    - gocritic
    - gosec
    - misspell
    - noctx
    - revive
    - rowserrcheck
    - sqlclosecheck
    - unconvert
    - unparam
  settings:
    forbidigo:
      forbid:
        - pattern: '^time\.(Now|Since|Until|Sleep|After|AfterFunc|Tick|NewTimer|NewTicker)$'
          msg: use clock.Clock (internal/clock) so tests stay deterministic
        - pattern: '^(fmt\.Print(f|ln)?|print|println)$'
          msg: write to an injected io.Writer or slog.Logger
    depguard:
      rules:
        only-app-imports-httpapi:
          files:
            - "!**/internal/app/**"
            - "!**/internal/httpapi/**"
          deny:
            - pkg: github.com/bdnix/kubelook/internal/httpapi
              desc: only internal/app may import httpapi
        only-main-imports-app:
          files:
            - "!**/cmd/kubelook/**"
            - "!**/internal/app/**"
          deny:
            - pkg: github.com/bdnix/kubelook/internal/app
              desc: only cmd/kubelook may import app
  exclusions:
    rules:
      - path: internal/clock/
        linters: [forbidigo]
      - path: internal/testutil/
        linters: [forbidigo]
      - path: _test\.go
        linters: [gosec, noctx, unparam]
formatters:
  enable:
    - gofmt
    - goimports
  settings:
    goimports:
      local-prefixes:
        - github.com/bdnix/kubelook
```

`.testcoverage.yml`:

```yaml
# Unit-test coverage gate for Go. Total statement coverage must be >= 90%.
profile: coverage.out
threshold:
  total: 90
exclude:
  paths:
    - ^cmd/kubelook/main\.go$     # 3-line entry point, cannot run in-process
    - ^internal/testutil/         # test helpers, not product code
    - ^test/                      # integration/load tests (build-tagged)
    - ^hack/                      # tooling
```

**Red (prove the gate fails).**

```bash
bin/golangci-lint config verify      # must print nothing and exit 0; fix the YAML if it reports errors
# Temporarily add an untested function:
cat >> internal/version/version.go <<'EOF'

func untested(a int) int {
	if a > 0 {
		return a + 1
	}
	if a < -10 {
		return a - 1
	}
	return a * 2
}

var _ = untested
EOF
make coverage-backend; echo "exit=$?"
```

Expected: output contains `Total coverage threshold (90%) satisfied:	FAIL` and `exit=2` (make reports the tool's exit status 1 as its own failure). If it prints `PASS`, the gate is not working: stop and fix `.testcoverage.yml`.

**Green.** Remove the temporary function (`git checkout internal/version/version.go`). Run `make coverage-backend`. Expected output contains:

```
Total coverage threshold (90%) satisfied:	PASS
Total test coverage: 100% (…)
```

**Refactor.** None.

**Acceptance.**

```bash
make lint-backend      # 0 issues.
make coverage-backend     # … satisfied: PASS
git status --porcelain internal/     # empty: the temporary function is gone
```

**Pitfalls.**
- The exclude paths are regular expressions matched against paths relative to the module root. Do not add entries. Every new exclusion needs a row in §6 and a reason.
- Since Go 1.22, packages without test files are reported at 0% and count against the total. Every package therefore needs tests from the commit that creates it.
- `depguard` ignores blank (`_`) imports; the two rules therefore guard ordinary imports only. `only-app-imports-httpapi` and `only-main-imports-app` are separate rules so that `cmd/kubelook` may import `app` but not `httpapi`, and `httpapi` may not import `app`.
- Go tools must not see `web/node_modules`: Step 0.4 adds `web/go.mod` for that.
- If `golangci-lint config verify` rejects a key (the v2 schema changes between minor versions), run `bin/golangci-lint linters` and `bin/golangci-lint help linters` and correct only the key name; do not drop the rule.

### Step 0.4 — Web scaffold, test-first, with the web coverage gate

**Goal.** A Vite + React + TypeScript app with one tested component, lint, formatting, type check, and Vitest coverage thresholds that are proven to fail.

**Files.** Create under `web/`: `package.json`, `package-lock.json` (generated), `tsconfig.json`, `vite.config.ts`, `eslint.config.js`, `.prettierrc.json`, `.prettierignore`, `index.html`, `go.mod`, `src/main.tsx`, `src/vite-env.d.ts`, `src/App.tsx`, `src/App.test.tsx`, `src/test/setup.ts`, `src/styles/theme.css`.

Inner layout of `web/src` used by all later steps:

```
src/
  main.tsx            bootstrap (excluded from coverage)
  App.tsx             providers + routes
  api/                REST client and query hooks
  ws/                 WebSocket client
  state/              zustand stores
  kinds/              kind registry (columns, actions per resource kind)
  components/         shared UI (Table, Drawer, Banner, SafeLink, …)
  features/<name>/    one folder per feature (auth, clusters, resources, logs, shell, files, helm, flux, …)
  adapters/monaco/    Monaco wrapper (excluded from coverage)
  adapters/xterm/     xterm wrapper (excluded from coverage)
  lib/                pure helpers (formatting, filters, csv, …)
  styles/             CSS
  test/               test helpers (setup.ts, render helpers, fakes)
```

**Commands.**

```bash
cd web
npm init -y
npm pkg set name=kubelook-web type=module license=MIT
npm pkg set --json private=true
npm pkg delete main keywords author description
npm pkg set scripts.dev="vite" scripts.build="tsc --noEmit && vite build" scripts.test="vitest run" \
  scripts.lint="eslint . --max-warnings=0" scripts.format:check="prettier --check ." scripts.typecheck="tsc --noEmit"
npm install --save-exact react@19.3.1 react-dom@19.3.1
npm install --save-dev --save-exact typescript@6.0.3 vite@8.3.7 @vitejs/plugin-react@6.3.0 \
  vitest@5.1.0 @vitest/coverage-v8@5.1.0 jsdom@31.0.0 \
  @testing-library/react@16.3.3 @testing-library/user-event@14.6.9 @testing-library/jest-dom@7.0.1 \
  eslint@10.14.0 typescript-eslint@8.72.2 eslint-plugin-react-hooks@7.1.1 prettier@3.9.10 \
  @types/react@19.3.0
npm install --save-dev --save-exact @eslint/js@10.0.1 @testing-library/dom@10.4.2 @types/node@26.6.6 @types/react-dom@19.3.0
```

(The four packages in the last command have no version in §3; these are the versions that resolved on 2026-11-11, pinned exactly so that `package.json` has no `^` ranges. Commit `package-lock.json`.)

`vite.config.ts`:

```ts
/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist', sourcemap: false },
  server: { proxy: { '/api': 'http://localhost:8080', '/auth': 'http://localhost:8080' } },
  test: {
    environment: 'jsdom',
    setupFiles: ['src/test/setup.ts'],
    restoreMocks: true,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'text-summary', 'json-summary', 'lcov'],
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/main.tsx',
        'src/adapters/monaco/**',
        'src/adapters/xterm/**',
        'src/test/**',
        'src/**/*.test.{ts,tsx}',
        'src/**/*.d.ts',
      ],
      thresholds: { lines: 90, statements: 90, functions: 90, branches: 80 },
    },
  },
});
```

`tsconfig.json`: `"compilerOptions": { "target": "ES2022", "lib": ["ES2022", "DOM", "DOM.Iterable"], "module": "ESNext", "moduleResolution": "bundler", "jsx": "react-jsx", "strict": true, "noUncheckedIndexedAccess": true, "noEmit": true, "skipLibCheck": true, "isolatedModules": true, "types": ["@testing-library/jest-dom"] }, "include": ["src", "vite.config.ts"]`. Vitest globals are off at runtime and in the type check; tests import `describe`, `it`, `expect`, `vi` from `vitest` explicitly. A test that forgets the import therefore fails `tsc`.

`eslint.config.js`:

```js
import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import reactHooks from 'eslint-plugin-react-hooks';

export default [
  { ignores: ['dist/**', 'coverage/**'] },
  js.configs.recommended,
  ...tseslint.configs.strict,
  {
    files: ['src/**/*.{ts,tsx}'],
    plugins: { 'react-hooks': reactHooks },
    rules: {
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'error',
      'no-restricted-syntax': [
        'error',
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message: 'Cluster content must be rendered as text.',
        },
      ],
      'no-restricted-globals': ['error', { name: 'fetch', message: 'Use api/client.ts.' }],
      'no-restricted-properties': [
        'error',
        ...['window', 'globalThis', 'self'].map((object) => ({
          object,
          property: 'fetch',
          message: 'Use api/client.ts.',
        })),
      ],
    },
  },
  {
    // The one place that may call fetch.
    files: ['src/api/client.ts', 'src/api/client.test.ts'],
    rules: { 'no-restricted-globals': 'off', 'no-restricted-properties': 'off' },
  },
];
```

`.prettierrc.json`: `{ "singleQuote": true, "printWidth": 110 }`. `.prettierignore`: `dist`, `coverage`, `package-lock.json`.

`src/test/setup.ts`: registers the jest-dom matchers and `afterEach(cleanup)` (Testing Library only cleans up by itself when test globals are on):

```ts
import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

// Vitest globals are off, so Testing Library cannot register its own cleanup.
afterEach(cleanup);
```

`go.mod` (in `web/`): a stub module (`module example.invalid/kubelook-web-ignore`, `go 1.27.4`, no code) so that `go ./...`, `go test`, coverage and the Go linters skip the Go files some npm packages ship inside `web/node_modules`. `src/vite-env.d.ts`: `/// <reference types="vite/client" />` (TypeScript 6 needs a declaration for the CSS import).

`index.html`: standard Vite HTML with `<div id="root"></div>` and `<script type="module" src="/src/main.tsx"></script>`, `<title>Kubelook</title>`. No inline scripts or inline styles (the CSP in Step 1.8 forbids them).

**Red.** `src/App.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { App } from './App';

describe('App', () => {
  it('shows the product name as the main heading', () => {
    render(<App />);
    expect(screen.getByRole('heading', { level: 1, name: 'Kubelook' })).toBeInTheDocument();
  });
});
```

Run `make test-frontend FILE=src/App.test.tsx`. Expected failure: `Failed to resolve import "./App"`.

**Green.** `src/App.tsx`: `export function App() { return <main><h1>Kubelook</h1></main>; }`. `src/main.tsx`: import `./styles/theme.css`, look up `#root` (throw `missing #root element` if absent; the `!` assertion is banned by the strict lint rules), then `createRoot(root).render(<StrictMode><App /></StrictMode>)`. `src/styles/theme.css`: `:root` custom properties for a light theme and `[data-theme='dark']` overrides (`--bg`, `--fg`, `--muted`, `--border`, `--accent`, `--danger`, `--warn`, `--ok`), plus `body { margin:0; background:var(--bg); color:var(--fg); font-family:system-ui,sans-serif; }`.

Run the same command. Expected: `Test Files  1 passed (1)`.

**Prove the web gate fails.** Add `src/lib/tmp.ts` with a function that has three branches and no test. Run `make coverage-frontend; echo "exit=$?"`. Expected: an error line starting `ERROR: Coverage for` naming a threshold `(90%)`, and a non-zero exit. Delete `src/lib/tmp.ts`. Run `make coverage-frontend` again: it must pass and print `Statements   : 100%` in the coverage summary (Vitest 5.1.0 prints no table when every file is fully covered).

**Refactor.** None.

**Acceptance.**

```bash
make lint-frontend      # no output from eslint/prettier/tsc errors
make coverage-frontend     # table printed; no ERROR lines
cd web && npm run build && ls dist/index.html     # file exists
```

**Pitfalls.**
- TypeScript must stay at 6.0.3: `typescript-eslint` 8.72.2 rejects 6.1 and 7.x.
- If `tseslint.configs.strict` is not iterable in this version, run `node -e "import('typescript-eslint').then(m=>console.log(Object.keys(m.default.configs)))"` in `web/` and use the `strict` entry as that output shows. Do not remove the strict rules.
- If Vitest reports an unknown `coverage.thresholds` key, run `npx vitest --help` and `npx vitest run --coverage --coverage.thresholds.lines=90` to find the supported spelling. The gate must exist before continuing.
- Never import `monaco-editor` or `@xterm/xterm` outside `src/adapters/`.
- `npm run build` runs `tsc --noEmit` first on purpose: the Dockerfile of Step 0.6 runs only `npm run build`, and an image must never be built from code that fails type-checking. `make verify` therefore runs `tsc` twice (once in `lint`, once in `build`); that is accepted.

### Step 0.5 — Embed the web build in the Go binary

**Goal.** The Go binary serves the built SPA from an embedded file system, with correct fallback to `index.html` for client-side routes.

**Files.** Create `internal/webui/webui.go`, `internal/webui/webui_test.go`, `internal/webui/dist/placeholder.txt` (content: `web UI not built`).

**Red.** `internal/webui/webui_test.go` (package `webui_test`), using `testing/fstest.MapFS` and `httptest.NewRecorder`. Cases:

| Case | File system | Request | Expect |
| --- | --- | --- | --- |
| serves index at root | `index.html`=`<html>app</html>` | `GET /` | 200, body `<html>app</html>`, `Cache-Control: no-cache`, `Content-Type` starts `text/html` |
| serves an asset | plus `assets/a.js`=`x` | `GET /assets/a.js` | 200, body `x`, `Cache-Control: public, max-age=31536000, immutable` |
| client route falls back | as above | `GET /c/v%3Aprod/ns/default/core/pods/web-1` | 200, body of `index.html` |
| missing asset is 404 | as above | `GET /assets/missing.js` | 404 |
| API paths are never served | as above | `GET /api/v1/unknown` | 404 |
| not built | only `placeholder.txt` | `GET /` | 503, body contains `web UI not built` |
| wrong method | as above | `POST /` | 405 |
| embedded FS is usable | — | `webui.FS()` | non-nil, `fs.Stat(fsys, "placeholder.txt")` succeeds |

Run `make test-backend PKG=./internal/webui/...`. Expected failure: `undefined: webui.Handler`.

**Green.**

```go
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded web build (the contents of dist/).
func FS() fs.FS { sub, _ := fs.Sub(dist, "dist"); return sub }

// Handler serves the SPA from fsys.
//   - GET/HEAD only (405 otherwise)
//   - paths starting with /api/ or /auth/ → 404 (they belong to other handlers)
//   - /assets/* → file or 404, long cache
//   - any other path → the file if it exists, else index.html (no-cache)
//   - 503 "web UI not built" if index.html is missing
func Handler(fsys fs.FS) http.Handler
```

Implement with `fs.ReadFile` and `http.ServeContent` (pass a zero `time.Time` as modtime). Run the command: `ok`.

**Refactor.** Extract `isAsset(path)` and `serveFile(w, r, fsys, name, cacheControl)`. Re-run: `ok`.

**Acceptance.**

```bash
make verify        # ends with VERIFY OK  (first time the full gate works)
```

**Pitfalls.**
- `//go:embed all:dist` needs at least one file in `dist/` at compile time; that is why `placeholder.txt` is committed and `make build-frontend` and `make clean` never delete it.
- Clean the URL path with `path.Clean` and reject anything containing `..` before reading from the file system.

### Step 0.6 — Dockerfile

**Goal.** One multi-stage image: build web, build Go (cross-compiled, static), run on distroless as non-root.

**Files.** Create `Dockerfile`, `.dockerignore`.

`Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM node:26.13.0-trixie AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-trixie AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/web/dist/ internal/webui/dist/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/bdnix/kubelook/internal/version.Version=${VERSION} -X github.com/bdnix/kubelook/internal/version.Commit=${COMMIT}" \
    -o /out/kubelook ./cmd/kubelook

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/kubelook /kubelook
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/kubelook"]
CMD ["serve"]
```

Pin every `FROM` line to its digest (`@sha256:…`). The `Dockerfile` is the only place these digests are recorded; Dependabot keeps them current, so this plan does not repeat them. Resolve a digest with:

```bash
docker buildx imagetools inspect <image>:<tag> --format '{{json .Manifest.Digest}}'
```

`.dockerignore`: `.git`, `bin`, `web/node_modules`, `web/dist`, `web/coverage`, `coverage.out`, `test`, `docs`, `deploy`, `.github`, `.it`.

**Red / Green / Refactor.** No product code. Verification is the acceptance below.

**Acceptance.**

```bash
make image
docker run --rm kubelook:dev version          # kubelook dev (commit <short sha>)
docker run --rm --entrypoint="" kubelook:dev /bin/sh -c true; echo "exit=$?"   # non-zero: the image has no shell
docker inspect kubelook:dev --format '{{.Config.User}}'    # 65532:65532
```

**Pitfalls.**
- Both build stages run on `$BUILDPLATFORM`, and the final stage has no `RUN`, so multi-architecture builds need no QEMU.
- Keep `COPY` lists explicit. `COPY . .` would bust the cache on every docs change.

### Step 0.7 — CI workflow: lint, unit tests, coverage, vulnerability checks

**Goal.** `.github/workflows/ci.yml` with one test job per side (`backend-tests`, `frontend-tests`) plus `ui-tests`, least privilege, SHA-pinned actions, caching and concurrency control. Each test job runs lint, the unit tests with the coverage gate (fails below 90%, numbers in the job summary) and that side's vulnerability check. There is no aggregate gate job: branch protection requires the jobs directly. `ui-tests` arrives with the first UI tests (Step 2.10); until then the workflow has the two side jobs only.

**Files.** Create `.github/workflows/ci.yml`, `hack/coverage-summary.sh`.

Pinned actions (use exactly these `uses:` strings everywhere in the repository):

```
actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1                # v7.0.1
actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e                # v7.0.0
actions/setup-node@949feb2413d6458794dcd2491c4babbbce0c15c1              # v7.1.0
actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9                   # v6.1.0
actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9         # v7.0.2
actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333       # v8.0.2
actions/dependency-review-action@a1d282b36b6f3519aa1f3fc636f609c47dddb294 # v5.0.0
actions/attest-build-provenance@4d101475d8b20a2381f78447822ac1eab6504dd8 # v4.2.2
golangci/golangci-lint-action@ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a   # v9.3.0
docker/setup-buildx-action@f87e5991a6d7451dcb8d9637bfbc97413f497069      # v4.4.1
docker/login-action@8aa5831fb8d48d8f76594c7ddde61ae58019f550             # v4.8.0
docker/metadata-action@dc802804100637a589fabce1cb79ff13a1411302          # v6.2.0
docker/build-push-action@c3c9e263c25d99ce0380d002d59b67737d91b0dc        # v7.4.0
aquasecurity/trivy-action@ed142fd0673e97e23eac54620cfb913e5ce36c25       # v0.36.0
sigstore/cosign-installer@756fe2d284c7292c4e9b62c8a28f1e5629c59532       # v4.1.3
helm/chart-testing-action@720270857a28afd41e2426521a7801db09e9b510       # v2.9.0
```

`.github/workflows/ci.yml`:

```yaml
name: ci

on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read

concurrency:
  group: ci-${{ github.event.pull_request.number || github.ref }}
  cancel-in-progress: ${{ github.event_name == 'pull_request' }}

defaults:
  run:
    shell: bash

# Required checks (branch protection): backend-tests, frontend-tests, ui-tests, image-scan.
# Publishing happens only on pushes to main, after all four have passed.
jobs:
  # Lint, unit tests and the vulnerability check of one side share a job: one checkout, one
  # toolchain setup, one cache. The coverage run IS the unit-test run, so no test runs twice.
  backend-tests:
    name: "Backend: Lint, Unit Tests & Coverage"
    runs-on: ubuntu-24.04
    timeout-minutes: 25
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
      - name: Lint
        uses: golangci/golangci-lint-action@ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a # v9.3.0
        with:
          version: v2.15.0
      - name: Unit tests with coverage (>= 90%)
        run: make coverage-backend 2>&1 | tee go-coverage.txt
      - name: Vulnerability check (govulncheck)
        run: go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
      - name: Report coverage
        if: ${{ !cancelled() }}
        run: hack/coverage-summary.sh backend >> "$GITHUB_STEP_SUMMARY"
      - name: Upload coverage report
        if: ${{ !cancelled() }}
        uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        with:
          name: coverage-backend
          path: coverage.out
          if-no-files-found: warn
          retention-days: 14

  frontend-tests:
    name: "Frontend: Lint, Unit Tests & Coverage"
    runs-on: ubuntu-24.04
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-node@949feb2413d6458794dcd2491c4babbbce0c15c1 # v7.1.0
        with:
          node-version-file: web/.nvmrc
          cache: npm
          cache-dependency-path: web/package-lock.json
      - name: Install dependencies
        run: npm ci
        working-directory: web
      - name: Lint and type check
        run: make lint-frontend
      - name: Unit tests with coverage (>= 90%)
        run: make coverage-frontend 2>&1 | tee web-coverage.txt
      - name: Dependency audit (npm audit)
        run: npm audit --omit=dev --audit-level=high
        working-directory: web
      - name: Report coverage
        if: ${{ !cancelled() }}
        run: hack/coverage-summary.sh frontend >> "$GITHUB_STEP_SUMMARY"
      - name: Upload coverage report
        if: ${{ !cancelled() }}
        uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        with:
          name: coverage-frontend
          path: |
            web/coverage/lcov.info
            web/coverage/coverage-summary.json
          if-no-files-found: warn
          retention-days: 14

  # The built web app in real Chromium, with the API answered by the tests (web/ui).
  # The full-stack browser tests (real backend, Vault, clusters) are Step 7.10.
  ui-tests:
    name: UI Tests (Playwright)
    runs-on: ubuntu-24.04
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-node@949feb2413d6458794dcd2491c4babbbce0c15c1 # v7.1.0
        with:
          node-version-file: web/.nvmrc
          cache: npm
          cache-dependency-path: web/package-lock.json
      - name: Install dependencies
        run: npm ci
        working-directory: web
      - name: Cache browser
        id: browsers
        uses: actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
        with:
          path: ~/.cache/ms-playwright
          key: playwright-${{ runner.os }}-${{ hashFiles('web/package-lock.json') }}
      - name: Install browser
        if: steps.browsers.outputs.cache-hit != 'true'
        run: npx playwright install --with-deps chromium
        working-directory: web
      - name: Install browser system libraries
        if: steps.browsers.outputs.cache-hit == 'true'
        run: npx playwright install-deps chromium
        working-directory: web
      - name: Build application and run UI tests
        run: make test-ui
      - name: Upload test report
        if: ${{ failure() }}
        uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        with:
          name: playwright-report
          path: |
            web/playwright-report
            web/test-results
          if-no-files-found: ignore
          retention-days: 7
```

`hack/coverage-summary.sh` (prints Markdown; executable; `backend` or `frontend` as argument):

```bash
#!/usr/bin/env bash
# Usage: coverage-summary.sh backend|frontend — prints the coverage numbers as Markdown for the job summary.
set -euo pipefail
echo "## Unit-test coverage (gate: 90%)"
echo
case "${1:-}" in
backend)
  echo "### Backend"
  echo '```'
  grep -E 'Total (coverage threshold|test coverage)' go-coverage.txt || echo "no Go coverage output"
  echo '```'
  ;;
frontend)
  echo "### Frontend"
  echo '```'
  if [ -f web/coverage/coverage-summary.json ]; then
    node -e 'const t=require("./web/coverage/coverage-summary.json").total;for(const k of ["lines","statements","functions","branches"])console.log(k.padEnd(11),t[k].pct+"%")'
  else
    echo "no web coverage output"
  fi
  echo '```'
  ;;
*)
  echo "usage: $0 backend|frontend" >&2
  exit 2
  ;;
esac
```

**Red / Green.** Prove the pipeline rejects low coverage on pull request 1 itself (no extra pull request): commit the untested function from Step 0.3 as `test(0.7): prove the coverage gate fails in CI` and push. Expected: job `Backend: Lint, Unit Tests & Coverage` fails. Then `git revert` that commit and push. Expected: all jobs green, and the two test jobs' summaries show the Go total and the four web percentages.

**Acceptance.**
- Pull request 1 shows these jobs, all green: `Backend: Lint, Unit Tests & Coverage`, `Frontend: Lint, Unit Tests & Coverage`, `Container Image: Build & Vulnerability Scan`, `Dependency Review` (the last two from Steps 0.8 and 0.9). Lint, tests and the vulnerability check of one side share a job (one checkout, one toolchain setup, one cache), and the coverage run is the unit-test run, so no test executes twice.
- `grep -c 'uses: .*@v[0-9]' .github/workflows/*.yml` prints `0` (no tag-pinned actions).
- `grep -L '^permissions:' .github/workflows/*.yml` prints nothing.

**Pitfalls.**
- `actions/setup-go` caches modules and build output by `go.sum` by default; do not add a second cache step for Go.
- `tee` hides failures unless the shell uses `pipefail`. GitHub's `bash` shell sets it; that is why `defaults.run.shell: bash` is declared.
- `persist-credentials: false` stops the checkout token from staying in `.git/config`.
- Do not add `pull_request_target` triggers anywhere.

### Step 0.8 — Image build, vulnerability scan, push to GHCR

**Goal.** Every PR builds the image and scans it. This job builds the whole product (web app, embedded Go binary, image) and runs `version`, so there is no separate build job. Pushes to `main` also publish `ghcr.io/bdnix/kubelook` with provenance, and only after every required job has passed; nothing is ever published from a pull request or another branch.

**Files.** Modify `.github/workflows/ci.yml`: add the two jobs below. `publish-image.needs` lists every required job; when a required job is added later (`ui-tests`, `chart-tests`), add it there too.

```yaml
  # Builds the whole product (web app, Go binary, image), so there is no separate build job.
  image-scan:
    name: "Container Image: Build & Vulnerability Scan"
    runs-on: ubuntu-24.04
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: docker/setup-buildx-action@f87e5991a6d7451dcb8d9637bfbc97413f497069 # v4.4.1
      - name: Build image
        uses: docker/build-push-action@c3c9e263c25d99ce0380d002d59b67737d91b0dc # v7.4.0
        with:
          context: .
          load: true
          push: false
          tags: kubelook:scan
          build-args: |
            VERSION=${{ github.sha }}
            COMMIT=${{ github.sha }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
      - name: Scan image (Trivy; fails on fixable HIGH or CRITICAL)
        uses: aquasecurity/trivy-action@ed142fd0673e97e23eac54620cfb913e5ce36c25 # v0.36.0
        with:
          scan-type: image
          image-ref: kubelook:scan
          format: table
          severity: HIGH,CRITICAL
          ignore-unfixed: true
          exit-code: "1"
      - name: Smoke test
        run: docker run --rm kubelook:scan version

  publish-image:
    name: Publish Container Image
    if: ${{ github.event_name == 'push' && github.ref == 'refs/heads/main' }}
    needs: [backend-tests, frontend-tests, ui-tests, image-scan]
    runs-on: ubuntu-24.04
    timeout-minutes: 30
    permissions:
      contents: read
      packages: write
      id-token: write
      attestations: write
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: docker/setup-buildx-action@f87e5991a6d7451dcb8d9637bfbc97413f497069 # v4.4.1
      - name: Log in to GHCR
        uses: docker/login-action@8aa5831fb8d48d8f76594c7ddde61ae58019f550 # v4.8.0
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: Compute image tags
        id: meta
        uses: docker/metadata-action@dc802804100637a589fabce1cb79ff13a1411302 # v6.2.0
        with:
          images: ghcr.io/bdnix/kubelook
          tags: |
            type=raw,value=main
            type=sha,format=long
      - name: Build and push image
        id: push
        uses: docker/build-push-action@c3c9e263c25d99ce0380d002d59b67737d91b0dc # v7.4.0
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          build-args: |
            VERSION=main-${{ github.sha }}
            COMMIT=${{ github.sha }}
          cache-from: type=gha
          sbom: true
          provenance: mode=max
      - name: Attest build provenance
        uses: actions/attest-build-provenance@4d101475d8b20a2381f78447822ac1eab6504dd8 # v4.2.2
        with:
          subject-name: ghcr.io/bdnix/kubelook
          subject-digest: ${{ steps.push.outputs.digest }}
          push-to-registry: true
```

**Acceptance.**
- On the PR: `image-scan` is green and its log shows the Trivy table (or "no vulnerabilities").
- After merge to `main`: `publish-image` is green; `docker pull ghcr.io/bdnix/kubelook:main && docker run --rm ghcr.io/bdnix/kubelook:main version` prints `kubelook main-<sha> (commit <sha>)`.

**Pitfalls.**
- Write permissions (`packages`, `id-token`, `attestations`) are on `publish-image` only. Never move them to the workflow level.
- `publish-image` never runs for pull requests, so forks and Dependabot PRs never get a token that can push.
- If Trivy finds a fixable HIGH/CRITICAL in the base image, update the base image digest (Step 0.6 command); do not add ignores. An ignore needs a `.trivyignore` entry with a comment naming the CVE, why it does not apply, and an expiry date, in its own PR.
- SARIF upload to code scanning is deliberately not used: it needs `security-events: write` and a repository feature that may not be enabled. Results are in the job log.

### Step 0.9 — Dependabot, dependency review, repository settings

**Goal.** Automated dependency updates for every ecosystem, a dependency review on PRs, and documented branch protection.

**Files.** Create `.github/dependabot.yml`, `docs/ci.md`. Modify `.github/workflows/ci.yml` (add job `dependency-review`).

`.github/dependabot.yml`:

```yaml
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule: { interval: weekly }
    groups:
      actions: { patterns: ["*"] }
  - package-ecosystem: gomod
    directory: /
    schedule: { interval: weekly }
    open-pull-requests-limit: 10
    groups:
      kubernetes: { patterns: ["k8s.io/*", "sigs.k8s.io/*"] }
      helm: { patterns: ["helm.sh/helm/*"] }
      opentelemetry: { patterns: ["go.opentelemetry.io/*"] }
      go-minor: { patterns: ["*"], update-types: [minor, patch], exclude-patterns: ["k8s.io/*", "sigs.k8s.io/*", "helm.sh/helm/*", "go.opentelemetry.io/*"] }
  - package-ecosystem: npm
    directory: /web
    schedule: { interval: weekly }
    open-pull-requests-limit: 10
    ignore:
      - dependency-name: typescript
        versions: [">=6.1.0"]   # typescript-eslint 8.72.2 supports < 6.1.0; lift when it supports newer
    groups:
      web-minor: { patterns: ["*"], update-types: [minor, patch] }
  - package-ecosystem: docker
    directory: /
    schedule: { interval: weekly }
```

Job to add to `ci.yml` (not in `publish-image.needs`: it only runs on PRs, so it is skipped on the `main` builds that publish):

```yaml
  dependency-review:
    name: Dependency Review
    if: ${{ github.event_name == 'pull_request' }}
    runs-on: ubuntu-24.04
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - name: Review added and changed dependencies
        uses: actions/dependency-review-action@a1d282b36b6f3519aa1f3fc636f609c47dddb294 # v5.0.0
        with:
          fail-on-severity: high
```

`docs/ci.md` documents: the jobs and what each proves; the coverage rules (copy the exclusion table from §6); how to resolve a new action SHA (`git ls-remote https://github.com/<owner>/<repo> 'refs/tags/<tag>^{}' 'refs/tags/<tag>'` — use the `^{}` line if present); and these **repository-owner settings**, which the executor cannot apply and must list in the PR description: protect `main`; require pull requests; require the status checks of the four required jobs, by their display names (`Backend: Lint, Unit Tests & Coverage`, `Frontend: Lint, Unit Tests & Coverage`, `UI Tests (Playwright)`, `Container Image: Build & Vulnerability Scan`; later the integration jobs of Step 7.11 and `chart-tests` of Step 8.2); restrict pushes to `main`, because a push to `main` publishes; require branches up to date; disallow force pushes; enable the dependency graph and Dependabot alerts; set Actions default workflow permissions to read-only.

**Acceptance.**
- PR shows `dependency-review` green. If it fails with "Dependency review is not supported on this repository", the dependency graph is disabled: say so in the PR description and ask the owner to enable it; do not delete the job.
- `docs/ci.md` exists and lists the required checks.

**Pitfalls.** Dependabot rewrites both the SHA and the trailing `# vX.Y.Z` comment; keep that comment format exactly.

**Phase 0 exit check.** `make verify` → `VERIFY OK`; `main` has green `ci`; `ghcr.io/bdnix/kubelook:main` exists.

---
# Phase 1 — Foundations

**Delivered in pull request 2**.

Outcome: `kubelook serve` starts, reads config, opens the database, runs migrations, serves the SPA, health endpoints and an authenticated-session skeleton. No sign-in yet.

Step format from here on: **Goal · Files · Red · Green · Refactor · Acceptance · Pitfalls**. "Red command" and "Green command" are the same command; it must fail before Green and pass after. Every step ends with `make verify` → `VERIFY OK` (not repeated in each Acceptance).

### Step 1.1 — Configuration

**Goal.** Load and validate configuration from a YAML file and the fixed environment variables (A5, A6).

**Files.** Create `internal/config/config.go`, `internal/config/config_test.go`, `internal/config/testdata/{full.yaml,minimal.yaml}`.

**Red.** `go get sigs.k8s.io/yaml@v1.6.0`. Tests (package `config_test`) call `config.Load(path string, getenv func(string) string) (*config.Config, error)`:

| Case | Input | Expect |
| --- | --- | --- |
| defaults | `minimal.yaml` (only `server.publicURL`, `vault.address`) + valid key env | every default in the table below |
| full file | `full.yaml` setting every key to a non-default | every field equals the file value |
| missing file | path that does not exist | error contains `reading config` |
| unknown key | file with `vault.adress` | error contains `unknown field` (strict decoding) |
| key missing | no `KUBELOOK_ENCRYPTION_KEY` | error `KUBELOOK_ENCRYPTION_KEY is required` |
| key wrong length | base64 of 16 bytes | error contains `must decode to 32 bytes` |
| key not base64 | `!!!` | error contains `base64` |
| publicURL missing / not http(s) | — | error names `server.publicURL` |
| postgres without URL | `database.driver: postgres`, no `KUBELOOK_DATABASE_URL` | error names `KUBELOOK_DATABASE_URL` |
| bad driver | `database.driver: mysql` | error names `database.driver` |
| helm.version not 3 or 4 | `5` | error names `helm.version` |
| google id without secret | `auth.google.clientId` set, env secret empty | error names `KUBELOOK_GOOGLE_CLIENT_SECRET` |
| restrictSignIn without lists | `restrictSignIn: true`, google configured, no `allowedDomains` | loads (sign-in is refused at runtime per spec), `Warnings()` contains one entry naming `auth.google.allowedDomains` |
| durations | `vault.tokenTTL: 45m` | `45 * time.Minute`; `vault.tokenTTL: abc` → error |
| mount prefix normalised | `kubernetes` | stored as `kubernetes/` |
| modules | `modules.keda.enabled: false` | `cfg.Modules["keda"].Enabled == "false"`; unknown module name → error; value other than `auto`/`false` → error |
| `MigrateOnStart` default | sqlite → `true`; postgres → `false`; explicit value wins | |
| `CookieSecure()` | `https://…` → true; `http://…` → false | |

Command: `make test-backend PKG=./internal/config/...`. Expected failure: `undefined: config.Load`.

**Green.** Struct (JSON tags because `sigs.k8s.io/yaml` converts YAML to JSON; use `yaml.UnmarshalStrict`):

```go
type Config struct {
	Server      Server      `json:"server"`
	Database    Database    `json:"database"`
	Session     Session     `json:"session"`
	Auth        Auth        `json:"auth"`
	Vault       Vault       `json:"vault"`
	Kubeconfig  Toggle      `json:"kubeconfig"`
	FileBrowser FileBrowser `json:"fileBrowser"`
	Logs        Logs        `json:"logs"`
	Shell       Shell       `json:"shell"`
	NodeShell   NodeShell   `json:"nodeShell"`
	Debug       Debug       `json:"debug"`
	Helm        Helm        `json:"helm"`
	Watch       Watch       `json:"watch"`
	Memory      Memory      `json:"memory"`
	Audit       Audit       `json:"audit"`
	Maintenance Maintenance `json:"maintenance"`
	Modules     map[string]Module `json:"modules"`
	Prometheus  Prometheus  `json:"prometheus"`
	Telemetry   Telemetry   `json:"telemetry"`

	// From environment only; never serialised.
	EncryptionKey     []byte `json:"-"`
	DatabaseURL       string `json:"-"`
	GoogleSecret      string `json:"-"`
	GitHubSecret      string `json:"-"`
}
```

Keys and defaults (key → Go type → default):

| Key | Type | Default |
| --- | --- | --- |
| `server.listenAddress` | string | `:8080` |
| `server.publicURL` | string | required |
| `database.driver` | string | `sqlite` |
| `database.sqlitePath` | string | `/data/kubelook.db` |
| `database.migrateOnStart` | *bool | sqlite `true`, postgres `false` |
| `session.idleTimeout` / `session.maxLifetime` | Duration | `30m` / `12h` |
| `auth.restrictSignIn` | *bool | `true` |
| `auth.google.clientId`, `auth.google.allowedDomains`, `auth.google.issuerURL` | string, []string, string | `""`, nil, `https://accounts.google.com` |
| `auth.github.clientId`, `auth.github.allowedOrgs`, `auth.github.baseURL`, `auth.github.apiBaseURL` | string, []string, string, string | `""`, nil, `https://github.com`, `https://api.github.com` |
| `vault.address` | string | `""` (Vault path is disabled when empty) |
| `vault.kubernetesMountPrefix` | string | `kubernetes/` |
| `vault.credsNamespace` | string | `itops` |
| `vault.clusterRoleBinding` | *bool | `true` |
| `vault.candidateRoles` | []string | nil |
| `vault.tokenTTL` / `vault.expiryWarning` / `vault.remintBefore` | Duration | `30m` / `30m` / `5m` |
| `kubeconfig.enabled` | *bool | `true` |
| `fileBrowser.enabled` / `fileBrowser.maxTransferBytes` | *bool / int64 | `true` / `26214400` |
| `logs.historyMaxLines` / `logs.liveBufferLines` / `logs.maxLinesPerSecond` | int | `200000` / `10000` / `2000` |
| `shell.idleTimeout` | Duration | `15m` |
| `nodeShell.enabled` / `nodeShell.image` | *bool / string | `false` / same as `debug.image` |
| `debug.image` / `debug.shell` / `debug.profiles` | string / string / []DebugProfile{Name, Profile, ShareProcessNamespace} | `ghcr.io/bdnix/kubelook-netshoot:v0.16` (replaced by a digest in Step 8.1) / `/bin/bash` / nil |
| `helm.version` / `helm.maxConcurrentJobs` / `helm.maxChartBytes` / `helm.repositories` | int / int / int64 / []HelmRepo{Name, URL} | `4` / `5` / `10485760` / nil |
| `watch.maxPerUser` | int | `50` |
| `memory.refuseNewWorkPercent` | int | `80` |
| `audit.retentionDays` | int | `0` |
| `maintenance.banner` | string | `""` |
| `modules.<name>.enabled` | string | `auto`; names: `karpenter`, `keda`, `externalSecrets`, `vaultSecrets`, `certManager`, `gatewayApi`, `trivy`, `permissions` |
| `prometheus.clusters` | map[string]{URL string} | nil |
| `telemetry.otlpEndpoint` | string | `""` |

`Duration` is a named type wrapping `time.Duration` with `UnmarshalJSON` using `time.ParseDuration`. Booleans whose default is `true` are `*bool` so "absent" differs from `false`; expose accessor methods (`cfg.Auth.Restrict() bool`). `Load` order: read file → strict unmarshal → apply defaults → read env → validate → return. `(*Config).Warnings() []string`.

**Refactor.** Move validation into `func (c *Config) validate() error` that collects all problems with `errors.Join`. Re-run.

**Acceptance.** `make test-backend PKG=./internal/config/...` → `ok`. `make coverage-backend` → `PASS`.

**Pitfalls.**
- `sigs.k8s.io/yaml` uses `json` tags, not `yaml` tags.
- Do not log the config struct. Add `func (c *Config) LogValue() slog.Value` that returns only non-secret fields, and test that its output does not contain the key or secrets.

### Step 1.2 — Clock and test helpers

**Goal.** A clock abstraction with a fake that fires timers deterministically, and the shared test helpers the determinism rules require.

**Files.** Create `internal/clock/clock.go`, `internal/clock/fake.go`, `internal/clock/clock_test.go`, `internal/testutil/recv.go`, `internal/testutil/rand.go`.

**Red.** Tests:
- `Real().Now()` is within 1 s of `time.Now()` (the only place real time is read).
- `NewFake(t0).Now()` equals `t0`; after `Advance(90*time.Second)` it equals `t0+90s`.
- `fake.NewTimer(10s)`: channel does not fire after `Advance(9s)`, fires exactly once after `Advance(1s)` with value `t0+10s`. `Stop()` before firing returns `true` and the channel never fires. `Reset(d)` re-arms.
- `fake.NewTicker(5s)`: after `Advance(12s)` exactly two ticks are readable (non-blocking sends, buffer 1: a slow reader misses ticks, as with `time.Ticker`), so the test reads after each `Advance(5s)`.
- `fake.AfterFunc(3s, f)`: `f` runs synchronously inside the `Advance` call that crosses 3 s.
- `fake.BlockUntil(n)` returns once `n` timers/tickers are registered (lets a test wait until a goroutine has armed its timer, without sleeping).
- Concurrent `Advance` and `NewTimer` under `-race` do not race.

Command: `make test-backend PKG=./internal/clock/...`. Expected failure: `undefined: clock.NewFake`.

**Green.**

```go
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
	NewTicker(d time.Duration) Ticker
	AfterFunc(d time.Duration, f func()) Timer
}
type Timer interface { C() <-chan time.Time; Stop() bool; Reset(d time.Duration) bool }
type Ticker interface { C() <-chan time.Time; Stop() }
func Real() Clock
func NewFake(start time.Time) *Fake      // Fake has Advance(d), BlockUntil(n int)
```

`Fake` keeps a mutex-guarded list of pending timers sorted by fire time; `Advance` moves `now` forward step by step to each due timer in order, firing it, and signals a `sync.Cond` used by `BlockUntil`.

`internal/testutil`:

```go
// Recv returns the next value from ch or fails the test after 5 seconds (failure bound only).
func Recv[T any](t testing.TB, ch <-chan T) T
// NoRecv fails if ch has a value ready right now (non-blocking).
func NoRecv[T any](t testing.TB, ch <-chan T)
// Rand returns a deterministic io.Reader (SHA-256 counter stream seeded by seed).
func Rand(seed string) io.Reader
```

**Refactor.** None beyond naming. Re-run.

**Acceptance.** `make test-backend PKG=./internal/clock/...` → `ok` (run it with `-count=20` once: `go test -race -count=20 ./internal/clock/...`).

**Pitfalls.** `internal/clock` and `internal/testutil` are the only places allowed to use package `time` functions that read or wait on real time (lint exclusion from Step 0.3). `internal/testutil` is excluded from coverage but `internal/clock` is not.

### Step 1.3 — Encryption envelopes

**Goal.** AES-256-GCM sealing with a random nonce, a recorded key ID and bound associated data, as the spec's Sessions section requires.

**Files.** Create `internal/crypto/crypto.go`, `internal/crypto/crypto_test.go`.

**Red.** Cases:
- `NewSealer(key, rand)` rejects keys that are not 32 bytes.
- `KeyID()` is 16 lowercase hex characters, equal to `hex(sha256(key))[:16]`; fixed vector: key of 32 zero bytes → `66687aadf862bd77`.
- Round trip: `Open(Seal(pt, aad), aad) == pt`, for empty and 1 MiB plaintext.
- Envelope format: `v1.<keyID>.<nonce b64url>.<ciphertext b64url>`; exactly four dot-separated parts; nonce decodes to 12 bytes.
- Two seals of the same plaintext differ (random nonce).
- Wrong associated data → `ErrDecrypt`.
- Envelope from another key → `ErrUnknownKey` (decided by key ID, before decrypting).
- Tampered ciphertext → `ErrDecrypt`. Malformed envelope (3 parts, bad base64, version `v2`) → `ErrMalformed`.
- `AAD("saved-credential", "user-1")` returns `[]byte("saved-credential|user-1")`; a purpose containing `|` panics (programming error).

Command: `make test-backend PKG=./internal/crypto/...`. Expected failure: `undefined: crypto.NewSealer`.

**Green.**

```go
var (
	ErrUnknownKey = errors.New("crypto: sealed with a different key")
	ErrDecrypt    = errors.New("crypto: decryption failed")
	ErrMalformed  = errors.New("crypto: malformed envelope")
)
type Sealer struct{ /* aead cipher.AEAD; keyID string; rand io.Reader */ }
func NewSealer(key []byte, rand io.Reader) (*Sealer, error)
func (s *Sealer) KeyID() string
func (s *Sealer) Seal(plaintext, aad []byte) (string, error)
func (s *Sealer) Open(envelope string, aad []byte) ([]byte, error)
func AAD(purpose string, binding string) []byte
```

Purposes used later (constants in this package): `PurposeSavedCredential` (`saved-credential`, binding = user ID), `PurposeSessionCredential` (`session-credential`, binding = session hash), `PurposeJob` (`job`, binding = job ID), `PurposeOAuthState` (`oauth-state`, binding = provider name).

**Refactor.** Extract `parseEnvelope`. Re-run.

**Acceptance.** `make test-backend PKG=./internal/crypto/...` → `ok`. Compute the fixed vector independently: `head -c 32 /dev/zero | sha256sum | cut -c1-16` prints `66687aadf862bd77`.

**Pitfalls.** Use `crypto/aes` + `cipher.NewGCM` from the standard library only. Never reuse a nonce: always read 12 fresh bytes from the injected reader and return an error if the read fails.

### Step 1.4 — Database open, dialect and migrator

**Goal.** Open SQLite or PostgreSQL through `database/sql`, rebind placeholders, and run embedded SQL migrations exactly once each.

**Files.** Create `internal/store/db.go`, `internal/store/migrate.go`, `internal/store/migrations/0001_init.sql`, `internal/store/db_test.go`, `internal/store/migrate_test.go`, `internal/store/storetest/storetest.go`.

**Red.** `go get modernc.org/sqlite@v1.62.0 github.com/jackc/pgx/v5@v5.12.0`. Cases:
- `Open(ctx, Options{Driver:"sqlite", SQLitePath: <temp file>})` returns a `*DB` whose `Dialect()` is `SQLite`; `PRAGMA foreign_keys` reads `1`; `PRAGMA journal_mode` reads `wal`.
- `Open` with `Driver:"postgres", URL:"postgres://u:p@127.0.0.1:1/db"` returns without connecting (`sql.Open` is lazy) and `Dialect()` is `Postgres`. (No server needed.)
- `Open` with an unknown driver → error.
- `Rebind(SQLite, "a=? AND b=?")` unchanged; `Rebind(Postgres, …)` → `a=$1 AND b=$2`; question marks inside single-quoted strings are not touched.
- `Migrate(ctx, db, fsys)` with a `fstest.MapFS` holding `0001_a.sql` (creates table `a`) and `0002_b.sql`: both applied in name order; `schema_migrations` has two rows; running `Migrate` again applies nothing; a third file added later is applied alone.
- A migration with invalid SQL → error naming the file; nothing from that file is applied (transaction rolled back); earlier files stay applied.
- A file name not matching `^\d{4}_[a-z0-9_]+\.sql$` → error.
- The embedded migrations (`store.Migrations()`) apply cleanly to an empty SQLite database.
- `storetest.New(t)` returns a migrated SQLite `*DB` in `t.TempDir()` and closes it in `t.Cleanup`.

Command: `make test-backend PKG=./internal/store/...`. Expected failure: `undefined: store.Open`.

**Green.**

```go
type Dialect int
const ( SQLite Dialect = iota; Postgres )
type Options struct{ Driver, SQLitePath, URL string }
type DB struct{ *sql.DB; dialect Dialect }
func Open(ctx context.Context, o Options) (*DB, error)
func (d *DB) Dialect() Dialect
func (d *DB) Exec(ctx, query string, args ...any) (sql.Result, error)      // rebinds, then ExecContext
func (d *DB) Query(ctx, query string, args ...any) (*sql.Rows, error)
func (d *DB) QueryRow(ctx, query string, args ...any) *sql.Row
func (d *DB) InTx(ctx context.Context, fn func(tx *Tx) error) error        // Tx has the same three methods
func Rebind(d Dialect, query string) string
func Migrations() fs.FS                                                    // embedded migrations/*.sql
func Migrate(ctx context.Context, db *DB, fsys fs.FS) error
```

- SQLite DSN: `file:<path>?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`; `db.SetMaxOpenConns(1)` for SQLite (single writer; avoids `SQLITE_BUSY`).
- PostgreSQL: `sql.Open("pgx", url)`; `SetMaxOpenConns(20)`.
- Schema rules so one set of SQL files serves both dialects: column types only `TEXT`, `BIGINT`, `INTEGER`; all IDs are `TEXT`; all times are `BIGINT` Unix milliseconds (UTC); JSON is stored as `TEXT`; encrypted values are envelope strings in `TEXT`; booleans are `INTEGER` 0/1. No dialect-specific syntax (no `AUTOINCREMENT`, `SERIAL`, `JSONB`, `ON CONFLICT … DO UPDATE` is allowed: both support it).
- `0001_init.sql` creates only `schema_migrations` companions needed now: nothing else (tables are added by the steps that use them, each in a new numbered file).
- `Migrate`: create `schema_migrations(name TEXT PRIMARY KEY, applied_at BIGINT NOT NULL)` if missing; list `*.sql` sorted; for each unapplied file run it and the insert inside one transaction. Statements in a file are separated by a line containing only `-- statement-break` (do not try to parse SQL).

**Refactor.** Share the rebind-and-run code between `DB` and `Tx` through one unexported interface. Re-run.

**Acceptance.** `make test-backend PKG=./internal/store/...` → `ok`.

**Pitfalls.**
- Import the drivers for side effects only: `_ "modernc.org/sqlite"` (driver name `sqlite`) and `_ "github.com/jackc/pgx/v5/stdlib"` (driver name `pgx`).
- PostgreSQL-specific behaviour cannot be unit-tested without a server; everything dialect-specific must therefore stay inside `Rebind` and `Open`. The real PostgreSQL run is Step 7.9.
- Always `defer rows.Close()` and check `rows.Err()` (lint enforces).

### Step 1.5 — Users and user settings

**Goal.** User records keyed by provider and stable subject, and one JSON settings document per user.

**Files.** Create `internal/store/migrations/0002_users.sql`, `internal/store/users.go`, `internal/store/users_test.go`.

Schema:

```sql
CREATE TABLE users (
  id TEXT PRIMARY KEY, provider TEXT NOT NULL, subject TEXT NOT NULL,
  email TEXT NOT NULL, name TEXT NOT NULL, created_at BIGINT NOT NULL, last_login_at BIGINT NOT NULL,
  UNIQUE (provider, subject)
);
-- statement-break
CREATE TABLE user_settings (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  data TEXT NOT NULL, updated_at BIGINT NOT NULL
);
```

**Red.** Cases (all with `storetest.New(t)` and a fake clock):
- `UpsertUser(provider, subject, email, name)` creates a user with a new ID (from the injected `NewID func() string`), `created_at == last_login_at == now`.
- Second upsert with the same provider+subject but a new email returns the **same ID**, updates email, name and `last_login_at`, keeps `created_at`.
- Same subject under a different provider → a different user.
- `GetUser(id)` → the user; unknown ID → `store.ErrNotFound`.
- `GetSettings(userID)` for a user without a row → the zero `Settings` with defaults (`WatchIdleSeconds: 30`).
- `PutSettings` then `GetSettings` round-trips every field; `updated_at` = now.
- `PutSettings` rejects `WatchIdleSeconds` outside 30…300 with `store.ErrInvalid`.

Command: `make test-backend PKG=./internal/store/...`. Expected failure: `undefined: store.NewUsers`.

**Green.**

```go
type User struct{ ID, Provider, Subject, Email, Name string; CreatedAt, LastLoginAt time.Time }
type Settings struct {
	FavouriteClusters []string                       `json:"favouriteClusters"`
	NamespaceFilters  map[string][]string            `json:"namespaceFilters"`  // cluster ID → namespaces
	ColumnPresets     map[string][]string            `json:"columnPresets"`     // "<group>/<kind>" → column IDs
	FilterSets        []FilterSet                    `json:"filterSets"`        // {Name, Kind, Query string}
	WatchIdleSeconds  int                            `json:"watchIdleSeconds"`  // 30..300
	Theme             string                         `json:"theme"`             // "light" | "dark" | "system"
}
type Users struct{ /* db *DB; clock clock.Clock; newID func() string */ }
func NewUsers(db *DB, c clock.Clock, newID func() string) *Users
func (u *Users) Upsert(ctx, provider, subject, email, name string) (User, error)
func (u *Users) Get(ctx, id string) (User, error)
func (u *Users) GetSettings(ctx, userID string) (Settings, error)
func (u *Users) PutSettings(ctx, userID string, s Settings) error
```

Production `newID` is `store.NewID` = 16 random bytes, lowercase hex (add it here with a test that two calls differ and length is 32).

**Refactor.** Add helpers `ms(t time.Time) int64` and `fromMS(int64) time.Time` in `internal/store/time.go` with tests; use them everywhere. Re-run.

**Acceptance.** `make test-backend PKG=./internal/store/...` → `ok`.

**Pitfalls.** Use `INSERT … ON CONFLICT (provider, subject) DO UPDATE SET … RETURNING id` — supported by both SQLite ≥ 3.35 and PostgreSQL. Never key users by email (spec).

### Step 1.6 — Sessions and session credentials

**Goal.** Persist sessions (by hashed ID) and their encrypted credentials; deleting a session deletes its credentials.

**Files.** Create `internal/store/migrations/0003_sessions.sql`, `internal/store/sessions.go`, `internal/store/sessions_test.go`.

```sql
CREATE TABLE sessions (
  id_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL, created_at BIGINT NOT NULL, last_seen_at BIGINT NOT NULL, expires_at BIGINT NOT NULL
);
-- statement-break
CREATE INDEX sessions_user ON sessions(user_id);
-- statement-break
CREATE TABLE session_credentials (
  session_hash TEXT NOT NULL REFERENCES sessions(id_hash) ON DELETE CASCADE,
  kind TEXT NOT NULL, ref TEXT NOT NULL, sealed TEXT NOT NULL, expires_at BIGINT NOT NULL DEFAULT 0, updated_at BIGINT NOT NULL,
  PRIMARY KEY (session_hash, kind, ref)
);
```

`kind` values (constants): `vault_token` (ref `""`), `kubeconfig` (ref = label), `cluster_cred` (ref = cluster ID), `cluster_role` (ref = cluster ID; chosen Vault role, not secret but kept together), `helm_repo` (ref = repo name).

**Red.** Cases:
- `Create(hash, userID, csrf, now, expiresAt)` then `Get(hash)` returns it; unknown hash → `ErrNotFound`.
- `Touch(hash, now)` updates `last_seen_at`; touching an unknown hash → `ErrNotFound`.
- `Delete(hash)` removes the session **and** its credentials (assert by direct `SELECT COUNT(*)`).
- `DeleteExpired(now, idleTimeout)` deletes sessions with `expires_at <= now` or `last_seen_at <= now-idle`, returns their hashes sorted, leaves others.
- `Exists(hashes []string)` returns the subset that still exists (used by the watchdog, A13).
- `PutCredential` upserts by (session, kind, ref); `GetCredential` returns sealed text and expiry or `ErrNotFound`; `ListCredentials(hash, kind)` sorted by ref; `DeleteCredential`.
- Putting a credential for a missing session → error (foreign key).

Command: `make test-backend PKG=./internal/store/...`. Expected failure: `undefined: store.NewSessions`.

**Green.** `type Sessions struct{…}` with the methods named above; `type SessionRow struct{ Hash, UserID, CSRFToken string; CreatedAt, LastSeenAt, ExpiresAt time.Time }`; `type CredentialRow struct{ Kind, Ref, Sealed string; ExpiresAt time.Time }`.

**Refactor.** None. Re-run.

**Acceptance.** `make test-backend PKG=./internal/store/...` → `ok`.

**Pitfalls.** The store never sees plaintext: it stores the envelope string produced by `crypto.Sealer`. The session ID itself is never stored, only `hex(sha256(id))`.

### Step 1.7 — Audit recorder and cleanup command

**Goal.** Write each audit record to the database and to standard output as one JSON line; refuse when the database write fails; delete old records with `kubelook audit-cleanup`.

**Files.** Create `internal/store/migrations/0004_audit.sql`, `internal/audit/audit.go`, `internal/audit/cleanup.go`, `internal/audit/audit_test.go`, `internal/audit/cleanup_test.go`. Modify `internal/app/app.go`, `internal/app/app_test.go`.

```sql
CREATE TABLE audit_events (
  id TEXT PRIMARY KEY, time BIGINT NOT NULL,
  account_provider TEXT NOT NULL, account_subject TEXT NOT NULL, account_email TEXT NOT NULL,
  vault_entity_id TEXT NOT NULL, vault_display_name TEXT NOT NULL,
  kube_context TEXT NOT NULL, kube_user TEXT NOT NULL,
  cluster TEXT NOT NULL, namespace TEXT NOT NULL, verb TEXT NOT NULL,
  resource_kind TEXT NOT NULL, resource_name TEXT NOT NULL, outcome TEXT NOT NULL, details TEXT NOT NULL
);
-- statement-break
CREATE INDEX audit_events_time ON audit_events(time);
```

**Red.** Cases for `audit.Recorder`:
- `Record(ctx, ev)` inserts one row with every field, `time` from the clock, `id` from `newID`, and writes exactly one line to the injected `io.Writer` that parses as JSON with keys `time` (RFC 3339), `account`, `vault`, `kube`, `cluster`, `namespace`, `verb`, `resource`, `outcome`, `details`, and `"audit": true`.
- Database failure (close the DB before calling) → `Record` returns an error wrapping `audit.ErrNotRecorded`, and still writes the line to stdout with `"recorded": false`.
- `details` values are JSON-encoded; a nil map is stored as `{}`.
- `Begin(ctx, ev)` records `outcome="requested"` with a fresh `details.requestId` and returns a `*Pending`; `pending.Done(ctx, err)` records `success` (nil) or `failure` with `details.error = err.Error()`, same `requestId`. `Begin` returning an error means the caller must not perform the action.
- `Identity` is taken from the context: `audit.WithIdentity(ctx, Identity{…})`; `Record` without identity in context uses empty strings (sign-in failures have no account yet).
- Never recorded: a test passes `details` containing a key named `token`, `password`, `secret`, `content` or `value` → `Record` returns `audit.ErrForbiddenDetail` (guard against accidental secret logging).

Cases for cleanup: `Cleanup(ctx, db, clock, retentionDays)`: `0` → deletes nothing, returns 0; `7` → deletes rows with `time < now-7d`, returns the count; negative → error.

`app.Main("audit-cleanup")` cases: with `KUBELOOK_AUDIT_CLEANUP_DATABASE_URL` unset and driver postgres → exit 1 and message; with SQLite config → opens `database.sqlitePath`, runs cleanup with `audit.retentionDays`, prints `audit-cleanup: deleted N records`, exit 0.

Command: `make test-backend PKG="./internal/audit/... ./internal/app/..."`. Expected failure: `undefined: audit.NewRecorder`.

**Green.**

```go
type Identity struct {
	AccountProvider, AccountSubject, AccountEmail string
	VaultEntityID, VaultDisplayName               string
	KubeContext, KubeUser                         string
}
type Event struct {
	Cluster, Namespace, Verb, ResourceKind, ResourceName, Outcome string
	Details map[string]any
}
type Recorder struct{…}
func NewRecorder(db *store.DB, c clock.Clock, out io.Writer, newID func() string) *Recorder
func (r *Recorder) Record(ctx context.Context, ev Event) error
func (r *Recorder) Begin(ctx context.Context, ev Event) (*Pending, error)
func (p *Pending) Done(ctx context.Context, actionErr error)        // logs, never returns an error
func WithIdentity(ctx context.Context, id Identity) context.Context
```

Verbs used across the plan (constants): `sign-in`, `sign-out`, `vault-credential-issued`, `create`, `update`, `patch`, `delete`, `scale`, `restart`, `rollback`, `evict`, `cordon`, `uncordon`, `drain`, `taint`, `label`, `secret-reveal`, `certificate-view`, `exec-start`, `exec-stop`, `debug-container`, `log-view-open`, `log-view-close`, `log-download`, `file-view`, `file-save`, `file-upload`, `file-download`, `file-delete`, `helm-install`, `helm-upgrade`, `helm-rollback`, `helm-uninstall`, `helm-test`, `helm-values-reveal`, `flux-reconcile`, `flux-suspend`, `flux-resume`, `module-action`, `key-changed`.

In `app.Main`, add the `audit-cleanup` case: load config; choose DB options (`KUBELOOK_AUDIT_CLEANUP_DATABASE_URL` for postgres, the SQLite path otherwise); call `audit.Cleanup`.

**Refactor.** Put the stdout JSON shape in one struct `line` shared by success and failure paths. Re-run.

**Acceptance.** `make test-backend PKG="./internal/audit/... ./internal/app/..."` → `ok`.

**Pitfalls.** The recorder only inserts. It must never `UPDATE` or `DELETE` (the PostgreSQL role cannot, A14). Write to stdout under a mutex so lines never interleave.

### Step 1.8 — HTTP server skeleton

**Goal.** Router, middleware chain, error format, health endpoints, security headers and SPA serving.

**Files.** Create `internal/httpapi/server.go`, `internal/httpapi/errors.go`, `internal/httpapi/middleware.go`, `internal/httpapi/server_test.go`, `internal/httpapi/middleware_test.go`.

**Red.** Tests build the handler with `httpapi.New(httpapi.Deps{…})` and call it through `httptest.NewRecorder`:
- `GET /healthz` → 200 `{"status":"ok"}`.
- `GET /readyz` → 200 when `Deps.Ready(ctx)` returns nil; 503 `{"error":{"code":"internal",…}}` when it returns an error.
- Unknown `/api/v1/x` → 404 with JSON error `not_found`.
- `GET /` → served by the SPA handler (use a `fstest.MapFS`).
- Every response has: `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`; and `Strict-Transport-Security: max-age=31536000` only when the public URL is HTTPS.
- `/api/` responses also have `Cache-Control: no-store`.
- Request ID: response header `X-Request-Id` is set; an incoming valid ID (≤ 64 chars, `[A-Za-z0-9-]`) is reused, anything else replaced.
- A handler that panics → 500 JSON `internal`, the panic is logged once, the server keeps serving.
- `writeError` mapping table: `ErrNotFound`→404 `not_found`; `ErrInvalid`→400 `bad_request`; `ErrConflict`→409; `ErrForbidden`→403; `ErrUnauthenticated`→401; `ErrBusy`→503 `server_busy`; `ErrTooLarge`→413; unknown → 500 `internal` with message `internal error` (never the raw error text).
- Request body limit: `decodeJSON` rejects bodies over 1 MiB with 413 and unknown fields with 400.
- Access log: one `slog` record per request with method, route pattern, status, duration (from the fake clock), request ID; never the query string.

Command: `make test-backend PKG=./internal/httpapi/...`. Expected failure: `undefined: httpapi.New`.

**Green.**

```go
type Deps struct {
	Config *config.Config
	Clock  clock.Clock
	Log    *slog.Logger
	WebFS  fs.FS
	Ready  func(context.Context) error
	// later steps add fields here; nil fields mean "feature not wired yet"
}
func New(d Deps) http.Handler
// errors.go
var ( ErrNotFound, ErrInvalid, ErrConflict, ErrForbidden, ErrUnauthenticated, ErrBusy, ErrTooLarge error )
type APIError struct{ Status int; Code, Message string }       // returned by handlers for specific messages
func writeError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error)
func writeJSON(w http.ResponseWriter, status int, v any)
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error
```

Use `http.NewServeMux()` with method patterns (`mux.HandleFunc("GET /healthz", …)`). Middleware order, outermost first: recover → request ID → access log → security headers → (later: session, CSRF) → mux. The sentinel errors live in a new tiny package `internal/apperr` if another package needs them; `httpapi` re-exports. Create `internal/apperr/apperr.go` now with those sentinels and a one-case test each for `errors.Is`.

**Refactor.** Each middleware is `func(http.Handler) http.Handler`; compose with `chain(h, mws...)`. Re-run.

**Acceptance.** `make test-backend PKG="./internal/httpapi/... ./internal/apperr/..."` → `ok`.

**Pitfalls.**
- `style-src 'unsafe-inline'` is required by Monaco and xterm (they set inline styles); `script-src` stays strict. This is the plan's CSP; the spec defers the exact value to the security review.
- Register API routes before the SPA catch-all `mux.Handle("/", …)`.
- Do not call `http.Error` anywhere; always `writeError`.

### Step 1.9 — Session manager, cookie, CSRF

**Goal.** Create, load and end sessions; enforce idle and absolute lifetime; CSRF protection; a per-replica registry of things to close when a session ends.

**Files.** Create `internal/session/manager.go`, `internal/session/registry.go`, `internal/session/middleware.go`, `internal/session/*_test.go`. Modify `internal/httpapi/server.go` (wire middleware) and its tests.

**Red.** Cases (SQLite store, fake clock, `testutil.Rand`):
- `Create(ctx, userID)` returns a session with a 43-character base64url ID (32 random bytes) and a CSRF token of the same form; the store holds only `sha256(id)` in hex.
- `SetCookie(w, s)` writes `kubelook_session=<id>; Path=/; HttpOnly; SameSite=Lax` plus `Secure` when `CookieSecure` is true; no `Expires`/`Max-Age` (session cookie).
- `Load(ctx, r)` returns the session for a valid cookie and touches `last_seen_at` at most once per minute (two loads 10 s apart → one `Touch`).
- Idle: advance 31 minutes without loads → `Load` returns `ErrUnauthenticated` and the row is deleted. Absolute: touch every 10 minutes for 12 h 1 min → `ErrUnauthenticated`.
- `KeepAlive(hash)` (called by open streams, A12) touches the session like a request.
- `End(ctx, hash)` deletes the session (and credentials by cascade), runs every closer registered for that hash exactly once, in registration order, and returns their joined errors.
- Registry: `Register(hash, name, closeFn) (unregister func())`; `Count(hash)`; `Hashes()` sorted; closers registered after `End` for the same hash are closed immediately.
- Watchdog: `RunWatchdog(ctx)` on a 5-second fake ticker asks the store which registered hashes still exist and closes the registry entries of those that do not (simulates logout on another replica). Use `fake.BlockUntil(1)` then `Advance(5s)`; assert the closer ran via a channel and `testutil.Recv`.
- `Sweep`: on a 1-minute ticker deletes expired sessions and closes their registry entries.
- Middleware `Require`: no/invalid cookie on `/api/v1/…` → 401 `unauthenticated`; valid → handler sees `session.FromContext(ctx)`.
- Middleware `CSRF`: `POST` without `X-CSRF-Token` → 403 `csrf`; wrong token → 403; right token → passes; `GET` needs none. Comparison uses `subtle.ConstantTimeCompare`.
- Public routes (`/healthz`, `/readyz`, `/metrics`, `/auth/…`, `GET /api/v1/auth/providers`, static files) pass without a session.

Command: `make test-backend PKG="./internal/session/... ./internal/httpapi/..."`. Expected failure: `undefined: session.NewManager`.

**Green.**

```go
type Session struct{ Hash, UserID, CSRFToken string; CreatedAt, ExpiresAt time.Time }
type Manager struct{…}
type Deps struct {
	Store *store.Sessions; Clock clock.Clock; Rand io.Reader; Log *slog.Logger
	IdleTimeout, MaxLifetime time.Duration; CookieSecure bool
}
func NewManager(d Deps) *Manager
func (m *Manager) Create(ctx context.Context, userID string) (id string, s Session, err error)
func (m *Manager) Load(ctx context.Context, r *http.Request) (Session, error)
func (m *Manager) KeepAlive(ctx context.Context, hash string)
func (m *Manager) End(ctx context.Context, hash string) error
func (m *Manager) SetCookie(w http.ResponseWriter, id string)
func (m *Manager) ClearCookie(w http.ResponseWriter)
func (m *Manager) Registry() *Registry
func (m *Manager) RunWatchdog(ctx context.Context)   // blocks until ctx is done
func (m *Manager) RunSweep(ctx context.Context)
func Hash(id string) string
func FromContext(ctx context.Context) (Session, bool)
```

**Refactor.** Merge the watchdog and sweep loops into one `run(ctx, interval, fn)` helper. Add `goleak.VerifyTestMain` (`go get go.uber.org/goleak@v1.3.0`). Re-run.

**Acceptance.** `make test-backend PKG="./internal/session/... ./internal/httpapi/..."` → `ok`.

**Pitfalls.**
- Closers must not be called while holding the registry mutex (they may call `unregister`).
- Tests that start `RunWatchdog` must cancel the context and wait for the goroutine to return (goleak).

### Step 1.10 — `serve` and `migrate` commands, graceful shutdown

**Goal.** `kubelook serve` wires config → store → migrations → sessions → HTTP server and shuts down cleanly; `kubelook migrate` runs migrations and exits.

**Files.** Create `internal/app/serve.go`, `internal/app/serve_test.go`. Modify `internal/app/app.go`.

**Red.** Cases:
- `app.Serve(ctx, cfg, opts)` with `opts.Listener` = a `net.Listen("tcp","127.0.0.1:0")` listener: `GET http://<addr>/healthz` returns 200; `GET /readyz` returns 200 (database ping). Cancel `ctx`: `Serve` returns nil within the failure bound, and the listener is closed.
- With `migrateOnStart=true` and an empty SQLite path in `t.TempDir()`, tables exist afterwards. With `false` and an empty database, `Serve` returns an error containing `database schema is not up to date; run "kubelook migrate"`.
- On shutdown, every session-registry closer is called (register one before cancelling; assert it ran).
- `Main("serve")` with a missing config file → exit 1, stderr contains `reading config`.
- `Main("migrate")` with SQLite config → exit 0, stdout `migrate: schema is up to date`; with postgres and no `KUBELOOK_MIGRATE_DATABASE_URL` → exit 1 naming the variable.
- A `SIGTERM`-equivalent: `Main` receives its context from `opts`; test passes an already-cancelled context and expects exit 0.

Command: `make test-backend PKG=./internal/app/...`. Expected failure: `undefined: app.Serve`.

**Green.**

```go
type ServeOptions struct {
	Listener net.Listener     // nil → net.Listen on cfg.Server.ListenAddress
	Stdout   io.Writer        // audit lines and logs
	Clock    clock.Clock      // nil → clock.Real()
	Rand     io.Reader        // nil → crypto/rand.Reader
}
func Serve(ctx context.Context, cfg *config.Config, o ServeOptions) error
```

`Serve`: build logger (`slog.NewJSONHandler(o.Stdout, …)`), open DB, migrate or verify schema (`store.Pending(ctx, db, fsys) ([]string, error)` — add it to `migrate.go` with a test), build `crypto.Sealer`, stores, `audit.Recorder`, `session.Manager`, `httpapi.New`; start `RunWatchdog`/`RunSweep`; `http.Server{ReadHeaderTimeout: 10s}`; on `ctx.Done()`: close all registry entries (no drain period, spec), `server.Shutdown` with a 10-second bound, close DB. `Main` builds the context with `signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)` in a small function `signalContext()` that `main`-path code uses; tests inject their own context through an unexported `mainWithContext(ctx, …)`.

**Refactor.** Group constructed components in an unexported `type components struct` returned by `build(cfg, o)` so later steps add wiring in one place. Re-run.

**Acceptance.**

```bash
make build
export KUBELOOK_ENCRYPTION_KEY="$(head -c 32 /dev/urandom | base64)"
printf 'server:\n  publicURL: http://localhost:8080\ndatabase:\n  sqlitePath: /tmp/kubelook-dev.db\n' > /tmp/kubelook.yaml
KUBELOOK_CONFIG=/tmp/kubelook.yaml bin/kubelook serve &   # then, once the log shows "listening":
curl -s localhost:8080/healthz      # {"status":"ok"}
curl -sI localhost:8080/ | head -1  # HTTP/1.1 200 OK
kill %1                             # process exits 0
```

**Pitfalls.** `http.Server` timeouts: set only `ReadHeaderTimeout`; `ReadTimeout`/`WriteTimeout` would kill WebSockets and log streams. Never call `os.Exit` or `log.Fatal` outside `cmd/kubelook/main.go`.

### Step 1.11 — Web shell: API client, layout, routing, theme

**Goal.** The SPA skeleton every feature plugs into: a typed API client with CSRF, the `me` query, the four-area layout, routes and the safe-link component.

**Files.** Create `web/src/api/client.ts`, `web/src/api/client.test.ts`, `web/src/api/me.ts`, `web/src/api/me.test.tsx`, `web/src/components/SafeLink.tsx`(+test), `web/src/components/Banner.tsx`(+test), `web/src/features/shell/Layout.tsx`(+test), `web/src/features/shell/routes.tsx`(+test), `web/src/state/theme.ts`(+test), `web/src/test/render.tsx`. Modify `web/src/App.tsx`, `web/src/App.test.tsx`. Backend: add `GET /api/v1/me` returning 401 without a session (handler in `internal/httpapi/me.go` + test); its full payload arrives in Step 2.4.

**Commands.** `cd web && npm install --save-exact react-router@8.5.1 @tanstack/react-query@5.107.1 zustand@5.1.0`

**Red.** Cases:
- `createClient({ fetch, getCsrf })`: `get('/x')` calls `fetch('/api/v1/x', { credentials: 'same-origin', … })`; `post` sends `Content-Type: application/json` and `X-CSRF-Token`; a non-2xx JSON error becomes `ApiError { status, code, message }`; a non-JSON error body becomes `ApiError` with code `internal`; 204 returns `undefined`.
- `useMe()`: 200 → `{ state: 'signedIn', me }`; 401 → `{ state: 'signedOut' }`; other error → `{ state: 'error' }`.
- `SafeLink`: `href="https://a.b"` renders an `<a>` with `rel="noopener noreferrer"` and `target="_blank"`; `javascript:alert(1)`, `data:…`, relative and malformed URLs render plain text, no anchor.
- `Banner`: renders `role="status"` (info) or `role="alert"` (warning, error) with the text; optional action button calls its handler.
- `Layout`: landmarks exist — `navigation` named "Clusters", `navigation` named "Resources", `main`, `complementary` named "Details" only when a drawer is open, `region` named "Dock".
- Routes: `/` → cluster picker placeholder; `/settings` → settings placeholder; `/c/:cluster/*` → cluster placeholder showing the **decoded** cluster ID (`/c/k%3Aprod%2Fctx` shows `k:prod/ctx`); unknown → "Page not found". Signed-out state renders the sign-in placeholder on every route.
- Theme store: default `system`; `setTheme('dark')` sets `document.documentElement.dataset.theme = 'dark'` and persists to `localStorage` key `kubelook.theme`.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green.** Implement. `web/src/test/render.tsx` exports `renderApp(ui, { route, client })` wrapping `MemoryRouter`, a fresh `QueryClient` (`retry: false`) and an `ApiContext` provider. `App.tsx` composes `QueryClientProvider` → `ApiContext` → `BrowserRouter` → routes.

**Refactor.** Route path builders in `web/src/lib/paths.ts` (`clusterPath(id)`, `resourcePath({cluster, namespace, group, kind, name})`) with tests for encoding (cluster IDs containing `/` and `:`; empty group written as `core`). Re-run.

**Acceptance.** `make test-frontend` → all passed. `make coverage-frontend` → no threshold errors. `make build` → `BUILD OK`; opening `http://localhost:8080/` (server from Step 1.10) shows the sign-in placeholder.

**Pitfalls.** `fetch` is banned by lint outside `api/client.ts`; pass it in (`createClient({ fetch: window.fetch.bind(window) })` in `main.tsx`). Use `encodeURIComponent` for every path segment.

---

# Phase 2 — Sign-in and credentials

**Delivered in pull request 3**.

Outcome (the spec's spike, minus live data): a user signs in with Google or GitHub, pastes a Vault token or uploads a kubeconfig, and sees exactly the clusters that credential reaches. Saved credentials live encrypted in the browser.

### Step 2.1 — OAuth core: state, PKCE, provider interface

**Goal.** The provider-independent half of sign-in: start (`/auth/{provider}/login`) and the state handling for the callback.

**Files.** Create `internal/auth/auth.go`, `internal/auth/state.go`, `internal/auth/handlers.go`, `internal/auth/*_test.go`.

**Red.** `go get golang.org/x/oauth2@v0.38.0`. Cases (a fake `Provider` implementation in the test):
- `GET /api/v1/auth/providers` → `{"providers":["github","google"]}` listing only configured providers, sorted.
- `GET /auth/google/login` → 302 to the provider's authorize URL with `response_type=code`, `client_id`, `redirect_uri=<publicURL>/auth/google/callback`, `scope`, `state`, `code_challenge`, `code_challenge_method=S256`; sets cookie `kubelook_oauth` (HttpOnly, SameSite=Lax, Path=/auth, Max-Age=600) whose value is a sealed envelope (purpose `oauth-state`, binding = provider) of `{state, verifier, createdAt}`.
- `code_challenge` equals `base64url(sha256(verifier))` for the verifier inside the cookie.
- Unknown provider → 404.
- Callback validation (`parseCallback`): missing cookie → `ErrState`; `state` query ≠ cookie state → `ErrState`; cookie older than 10 minutes (fake clock) → `ErrState`; cookie sealed for another provider → `ErrState`; `error=access_denied` in the query → `ErrDenied`; valid → returns code and verifier, and the cookie is cleared.

Command: `make test-backend PKG=./internal/auth/...`. Expected failure: `undefined: auth.NewHandlers`.

**Green.**

```go
type Identity struct{ Provider, Subject, Email, Name string }
type Provider interface {
	Name() string                                   // "google" | "github"
	OAuth2(redirectURL string) *oauth2.Config
	// Identify exchanges nothing; it receives the token from the exchange and returns the identity
	// or ErrNotAllowed when the allowlist refuses it.
	Identify(ctx context.Context, tok *oauth2.Token) (Identity, error)
}
var ( ErrState, ErrDenied, ErrNotAllowed error )
```

Use `oauth2.GenerateVerifier`-equivalent built from the injected `io.Reader` (32 random bytes, base64url), `oauth2.S256ChallengeOption(verifier)` on `AuthCodeURL` and `oauth2.VerifierOption(verifier)` on `Exchange`.

**Refactor.** None. Re-run.

**Acceptance.** `make test-backend PKG=./internal/auth/...` → `ok`.

**Pitfalls.** The state cookie is sealed, not just signed, so the verifier is not readable by page scripts or proxies. Never accept a `redirect` parameter from the query (open redirect): after sign-in the user always goes to `/`.

### Step 2.2 — Google provider

**Goal.** Google sign-in with ID-token verification and the hosted-domain allowlist.

**Files.** Create `internal/auth/google.go`, `internal/auth/google_test.go`, `internal/testutil/fakeoidc/fakeoidc.go` (an `httptest` OIDC issuer: discovery document, JWKS, token endpoint that returns an RS256 ID token with configurable claims and checks the PKCE verifier).

**Red.** `go get github.com/coreos/go-oidc/v3@v3.21.0`. Cases, all against `fakeoidc`:
- Claims `sub=123`, `email=a@corp.example`, `email_verified=true`, `hd=corp.example`, allowlist `[corp.example]`, restrict on → identity `{google, 123, a@corp.example, name}`.
- `hd` missing (personal account), restrict on → `ErrNotAllowed`. `hd=other.example` → `ErrNotAllowed`. `email_verified=false` → `ErrNotAllowed`. Empty allowlist with restrict on → `ErrNotAllowed` ("no list is set").
- Restrict off → any account accepted, including without `hd`.
- ID token with wrong audience, wrong issuer, expired (`exp` in the past relative to the injected clock) or a bad signature → error (not `ErrNotAllowed`).
- Token response without `id_token` → error.
- The domain comparison is case-insensitive; the `hd` claim is used, never the email's domain part.

Command: `make test-backend PKG=./internal/auth/...`. Expected failure: `undefined: auth.NewGoogle`.

**Green.** `func NewGoogle(ctx context.Context, cfg GoogleConfig, c clock.Clock, hc *http.Client) (Provider, error)`; `oidc.NewProvider(oidc.ClientContext(ctx, hc), cfg.IssuerURL)`; verifier `provider.Verifier(&oidc.Config{ClientID: cfg.ClientID, Now: c.Now})`; scopes `openid email profile`; ID token from `tok.Extra("id_token")`.

**Refactor.** Extract `allowed(domains []string, hd string) bool`. Re-run.

**Acceptance.** `make test-backend PKG=./internal/auth/...` → `ok`.

**Pitfalls.** `oidc.NewProvider` performs discovery at construction: construct providers lazily on first use with a `sync.Once`-style retry, so the server starts even when Google is briefly unreachable (test: issuer down at construction, up later → login works).

### Step 2.3 — GitHub provider

**Goal.** GitHub sign-in with the organisation-membership allowlist.

**Files.** Create `internal/auth/github.go`, `internal/auth/github_test.go`.

**Red.** Against an `httptest` server implementing `GET /user` and `GET /user/memberships/orgs/{org}`:
- `/user` returns `{"id":42,"login":"octo","name":"Octo","email":"o@x.example"}`; membership for `acme` returns `{"state":"active"}`; allowlist `[acme]` → identity `{github, "42", o@x.example, Octo}` (subject is the numeric ID as a decimal string, never the login).
- Membership `{"state":"pending"}` → `ErrNotAllowed`; 404 for every listed org → `ErrNotAllowed`; member of the second of two orgs → allowed.
- 403 from the membership endpoint (organisation has OAuth app restrictions) → `ErrNotAllowed` whose message includes `an organization owner must approve this OAuth app`.
- Empty allowlist with restrict on → `ErrNotAllowed`. Restrict off → no membership calls are made (assert zero requests) and scope is only `read:user user:email`.
- Restrict on → scopes include `read:org`.
- `/user` non-200 → error. Null email → empty string (allowed).
- Requests carry `Authorization: Bearer <token>`, `Accept: application/vnd.github+json`, `X-GitHub-Api-Version: 2022-11-28`.

Command: `make test-backend PKG=./internal/auth/...`. Expected failure: `undefined: auth.NewGitHub`.

**Green.** `func NewGitHub(cfg GitHubConfig, hc *http.Client) Provider`; OAuth endpoints `<baseURL>/login/oauth/authorize` and `<baseURL>/login/oauth/access_token`; API calls to `<apiBaseURL>`.

**Refactor.** One `getJSON(ctx, url, token, &v) (status int, err error)` helper. Re-run.

**Acceptance.** `make test-backend PKG=./internal/auth/...` → `ok`.

**Pitfalls.** Limit response bodies with `io.LimitReader(resp.Body, 1<<20)`. Close bodies (lint `bodyclose`).

### Step 2.4 — Completing sign-in, `me`, sign-out

**Goal.** The callback creates the user and session; `GET /api/v1/me` describes the session; `POST /api/v1/auth/logout` ends it.

**Files.** Modify `internal/auth/handlers.go`, `internal/httpapi/me.go`, `internal/app/serve.go`; tests alongside.

**Red.** Cases (fake providers; real store; captured audit lines):
- Successful callback → user upserted; session created; `kubelook_session` cookie set; 302 to `/`; audit record `verb=sign-in outcome=success` with the account identity.
- `ErrNotAllowed` → 302 to `/?signin_error=not_allowed`; no session; audit `sign-in` / `failure` with `details.reason`. `ErrState` → `/?signin_error=state`; `ErrDenied` → `/?signin_error=denied`; any other error → `/?signin_error=failed` (details only in the log).
- Signing in while already signed in ends the old session first.
- `GET /api/v1/me` → `{ user:{id,provider,email,name}, csrfToken, settings, maintenanceBanner, features:{kubeconfig, fileBrowser, nodeShell, vault, helmVersion, modules:{…}}, limits:{fileTransferBytes, logsHistoryMaxLines, logsLiveBufferLines}, vault:null, keyId }`.
- Logout → 204; cookie cleared; session row gone; registry closers called; audit `sign-out`. Logout without CSRF token → 403.
- Audit identity middleware: every request with a session gets `audit.WithIdentity` filled from the user (Vault identity is added in Step 2.7).

Command: `make test-backend PKG="./internal/auth/... ./internal/httpapi/..."`. Expected failure: assertion failures / undefined handler fields.

**Green.** Implement; wire providers in `serve.go` from config (a provider exists only when its client ID is set).

**Refactor.** Move the redirect-with-error logic to `failSignIn(w, r, reason)`. Re-run.

**Acceptance.** Tests `ok`. Manual: not possible without an IdP; covered by Step 7.4.

**Pitfalls.** Redirect targets are fixed strings; never echo provider error text into the URL or page.

### Step 2.5 — Vault client

**Goal.** A small client for the six Vault endpoints Kubelook uses, with typed errors.

**Files.** Create `internal/vault/client.go`, `internal/vault/client_test.go`, `internal/testutil/fakevault/fakevault.go`.

`fakevault` is an `httptest`-based Vault double used by unit tests: configurable tokens (TTL, policies, renewable, type, entity ID, display name), mounts (path, type, config, roles), a capability table (token → path → capabilities), counters for calls, and switches to make it unreachable or return 5xx. It implements only the six endpoints below, with Vault's JSON shapes.

**Red.** Cases:

| Method | Request | Parsed result |
| --- | --- | --- |
| `LookupSelf` | `GET /v1/auth/token/lookup-self` | `TokenInfo{TTL, Renewable, Type ("service"/"batch"), Policies, EntityID, DisplayName, ExpireTime}` from `data.ttl`, `data.renewable`, `data.type`, `data.policies`, `data.entity_id`, `data.display_name`, `data.expire_time`; `ttl: 0` → `NeverExpires() == true` |
| `RenewSelf` | `POST /v1/auth/token/renew-self` body `{}` | new TTL from `auth.lease_duration` |
| `Mounts` | `GET /v1/sys/internal/ui/mounts` | map of secret mounts path → type, from `data.secret` |
| `Capabilities` | `POST /v1/sys/capabilities-self` body `{"paths":[…]}` | map path → []capability from `data.<path>`; ≤ 100 paths per call, the client splits larger lists and merges |
| `ClusterConfig` | `GET /v1/kubernetes/prod/config` | `{Host, CACert}` from `data.kubernetes_host`, `data.kubernetes_ca_cert`; HTTP 404 (mount never configured, A30) → zero-value `ClusterConfig{}` and a nil error |
| `Creds` | `POST /v1/kubernetes/prod/creds/devops-role` body `{"kubernetes_namespace":"itops","cluster_role_binding":true,"ttl":"30m0s"}` | `{Token, ServiceAccount, Namespace, LeaseID, LeaseDuration}` from `data.service_account_token`, `data.service_account_name`, `data.service_account_namespace`, `lease_id`, `lease_duration` |

Plus: every request sends `X-Vault-Token` and `X-Vault-Request: true`; 403 → `ErrPermissionDenied` (wrapping Vault's `errors[0]`); 404 → `ErrNotFound`; 400 **and 500** with an `errors` list → `*ResponseError{Status, Messages}` (Vault 2.2.1 answers 500 when a secrets engine fails, for example a bad CA on `creds`; that is not an outage); connection refused, timeout, or 502/503/504 → `ErrUnreachable`; a response over 4 MiB → error; context cancellation propagates; the token never appears in any returned error string (test with a token value and `assert.NotContains`).

Command: `make test-backend PKG=./internal/vault/...`. Expected failure: `undefined: vault.NewClient`.

**Green.**

```go
type API interface {
	LookupSelf(ctx context.Context, token string) (TokenInfo, error)
	RenewSelf(ctx context.Context, token string) (time.Duration, error)
	Mounts(ctx context.Context, token string) (map[string]string, error)
	Capabilities(ctx context.Context, token string, paths []string) (map[string][]string, error)
	ClusterConfig(ctx context.Context, token, mount string) (ClusterConfig, error)
	Creds(ctx context.Context, token, mount, role string, req CredsRequest) (Creds, error)
}
func NewClient(address string, hc *http.Client) *Client     // hc has a 15 s timeout in production
```

**Refactor.** One `do(ctx, method, path, token, in, out)` function for all six. Re-run.

**Acceptance.** `make test-backend PKG=./internal/vault/...` → `ok`.

**Pitfalls.** Mount paths in Vault's responses end with `/` (`kubernetes/prod/`). `mount` arguments in this API are always the full mount path without the trailing slash (`kubernetes/prod`). `ttl` is sent as a Go duration string.

### Step 2.6 — Vault cluster discovery

**Goal.** From a token, compute exactly the clusters it can open and the roles allowed on each (spec "Discovery").

**Files.** Create `internal/vault/discovery.go`, `internal/vault/discovery_test.go`.

**Red.** Using a fake `vault.API` (in-memory struct in the test), prefix `kubernetes/`, candidate roles `[devops-role, platform-hub-role]`:

| Case | Setup | Expected clusters |
| --- | --- | --- |
| read config + update on first role | caps: config `[read]`, creds/devops-role `[update]` | `prod` with roles `[devops-role]`, default `devops-role` |
| both roles allowed | both creds paths `[update]` | roles in configured order, default = first |
| only second role | — | roles `[platform-hub-role]` |
| `create` only on creds | creds `[create]` | cluster **absent** |
| no read on config | config `[deny]` or `[]`, creds `[update]` | absent |
| `root` capability | config `[root]`, creds `[root]` | present (root implies everything) |
| `deny` together with others | `[deny, update]` | absent (deny wins) |
| `sudo`, `list` only | — | absent |
| mount of another type under the prefix | `kubernetes/kv/` type `kv` | ignored |
| kubernetes mount outside the prefix | `k8s/other/` | ignored |
| nested name | `kubernetes/eu/prod/` | cluster name `eu/prod` |
| no candidate roles configured | — | `ErrNoCandidateRoles` |
| 250 mounts | — | exactly one `Mounts` call and `ceil(250*3/100)` capability calls; result sorted by name |
| Vault unreachable | — | error wrapping `ErrUnreachable` |

Command: `make test-backend PKG=./internal/vault/...`. Expected failure: `undefined: vault.Discover`.

**Green.**

```go
type Cluster struct{ Name, Mount string; Roles []string }       // Roles in preference order; Roles[0] is the default
func Discover(ctx context.Context, api API, token string, prefix string, candidateRoles []string) ([]Cluster, error)
func can(caps []string, want string) bool    // root → true; deny → false; else contains(want)
```

**Refactor.** None. Re-run.

**Acceptance.** `make test-backend PKG=./internal/vault/...` → `ok`.

**Pitfalls.** Capability paths are sent without a leading slash and without `v1/`: `kubernetes/prod/config`, `kubernetes/prod/creds/devops-role`.

### Step 2.7 — Vault token as a saved credential

**Goal.** Endpoints to add, restore, renew and remove the Vault token; the browser gets only a sealed copy bound to the user.

**Files.** Create `internal/cluster/credentials.go`, `internal/cluster/credentials_test.go`, `internal/httpapi/credentials.go`, `internal/httpapi/credentials_test.go`.

API:

| Endpoint | Body | Response |
| --- | --- | --- |
| `POST /api/v1/credentials/vault` | `{token}` | `{sealed, vault: VaultStatus}` |
| `POST /api/v1/credentials/restore` | `{items:[{sealed}]}` | `{results:[{status, kind, label, vault?, clusters?, message?}]}` with status `ok`, `key_changed`, `wrong_user`, `expired`, `invalid`, `vault_unreachable` |
| `POST /api/v1/credentials/vault/renew` | — | `{vault: VaultStatus}` |
| `DELETE /api/v1/credentials/vault` | — | 204 |

`VaultStatus = { displayName, entityId, expiresAt|null, ttlSeconds, renewable, canRenew, neverExpires, warning: bool, expired: bool }`.

**Red.** Cases (fakevault, real store, fake clock):
- Add: token validated with `LookupSelf`; invalid token (403) → 400 `bad_request` "Vault rejected this token"; valid → session credential `vault_token` stored sealed (purpose `session-credential`, binding = session hash); response `sealed` opens with purpose `saved-credential`, binding = **user ID**, to JSON `{kind:"vault_token", token, savedAt}`.
- The plaintext token appears nowhere in the response body, logs or audit lines.
- Restore with a copy sealed for another user → `wrong_user`, nothing stored. Copy from another key → `key_changed` with the spec's message (exact text: `Kubelook's encryption key was changed, so your saved Vault token and kubeconfigs can't be used anymore. Please paste your token and upload your kubeconfigs again.`) and one audit record `key-changed` carrying both key IDs. Token expired in Vault (lookup 403) → `expired`. Garbage → `invalid`. Vault down → `vault_unreachable` and the token is kept in the session so discovery can be retried.
- Renew: `renewable=true` → `RenewSelf` called; status reflects the new TTL. If the TTL after renewal is not greater than before (within 1 s), `canRenew` becomes false (max TTL reached). Batch token or `renewable=false` → 409 `conflict`, `canRenew=false`. The console never calls `RenewSelf` on its own (assert the fake's renew counter stays 0 across discovery and status calls).
- `warning` is true when `0 < ttl < vault.expiryWarning`; never for `neverExpires`.
- Delete: removes `vault_token`, all `cluster_cred`/`cluster_role` rows of Vault clusters, and closes that session's registry entries tagged with a `v:` cluster.
- Audit identity now includes the Vault entity ID and display name whenever a Vault token is in the session.
- `vault.address` empty → all four endpoints 404 and `me.features.vault=false`.

Command: `make test-backend PKG="./internal/cluster/... ./internal/httpapi/..."`. Expected failure: `undefined: cluster.NewCredentials`.

**Green.**

```go
type Credentials struct{…}   // deps: store.Sessions, crypto.Sealer, vault.API, clock, config, audit.Recorder
func (c *Credentials) AddVaultToken(ctx, sess session.Session, token string) (sealed string, st VaultStatus, err error)
func (c *Credentials) Restore(ctx, sess session.Session, sealed []string) []RestoreResult
func (c *Credentials) RenewVaultToken(ctx, sess session.Session) (VaultStatus, error)
func (c *Credentials) RemoveVaultToken(ctx, sess session.Session) error
func (c *Credentials) VaultToken(ctx, sess session.Session) (string, vault.TokenInfo, error)   // for later steps
func (c *Credentials) VaultStatus(ctx, sess session.Session) (*VaultStatus, error)
```

**Refactor.** `sealForBrowser(userID, payload)` / `openFromBrowser(userID, sealed)` helpers shared with Step 2.8. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** Session credentials become unreadable after a key change: on `crypto.ErrUnknownKey` when opening a **session** credential, end the session and answer 401 with code `key_changed` (test this). TTL shown to the user is computed from `ExpireTime` and the clock, not from the stale `ttl` number.

### Step 2.8 — Kubeconfig upload

**Goal.** Parse a kubeconfig server-side, reject unsupported auth with clear messages, keep it as a sealed browser copy.

**Files.** Create `internal/kubeconfig/kubeconfig.go`, `internal/kubeconfig/kubeconfig_test.go`, `internal/kubeconfig/testdata/*.yaml`. Modify `internal/cluster/credentials.go`, `internal/httpapi/credentials.go` (+tests).

API: `POST /api/v1/credentials/kubeconfig` `{label, content}` → `{sealed, label, clusters:[{id, name, context, user, server}]}`; `DELETE /api/v1/credentials/kubeconfig/{label}` → 204.

**Red.** `go get k8s.io/client-go@v0.37.2 k8s.io/api@v0.37.2 k8s.io/apimachinery@v0.37.2`. `kubeconfig.Parse(label string, content []byte) (*File, error)` cases (one testdata file each):

| Case | Expect |
| --- | --- |
| client certificate data + CA data | accepted; context listed |
| bearer token | accepted |
| OIDC `auth-provider` with `idp-issuer-url`, `client-id`, `refresh-token` | accepted; `Auth == OIDC` |
| `exec` plugin user (EKS-style) | `*UnsupportedError` naming the user entry; message contains `exec credential plugins are not supported` |
| `client-certificate: /path` | error contains `file path references are not supported (client-certificate)` |
| `certificate-authority: /path` | same for `certificate-authority` |
| OIDC `idp-certificate-authority: /path` | same for `idp-certificate-authority` |
| `tokenFile` | file-path error |
| auth-provider other than `oidc` | unsupported error |
| username/password | unsupported error |
| a context referring to a missing cluster or user | error naming the context |
| no contexts | error `kubeconfig has no contexts` |
| invalid YAML | error `not a valid kubeconfig` |
| over 1 MiB | `apperr.ErrTooLarge` |
| two contexts, one unsupported | the whole file is rejected, naming the offending entry (no partial load) |
| label with `/` or empty, or longer than 63 | `ErrInvalid` |

Cluster IDs are `k:<label>/<context>`. `File.RESTConfig(context string) (*rest.Config, error)` builds from in-memory data only (`clientcmd.NewNonInteractiveClientConfig(...).ClientConfig()`), and the test asserts `ExecProvider == nil`, `AuthProvider == nil`, all `*File` path fields empty.

HTTP cases: `kubeconfig.enabled=false` → 404; upload stores session credential `kubeconfig` (ref = label), returns a sealed copy (kind `kubeconfig`, label, content); restore of a kubeconfig copy re-parses it; delete removes the credential, its clusters' `cluster_cred` rows and closes their connections; uploading an existing label replaces it.

Command: `make test-backend PKG="./internal/kubeconfig/... ./internal/cluster/... ./internal/httpapi/..."`. Expected failure: `undefined: kubeconfig.Parse`.

**Green.** Use `clientcmd.Load(content)` then validate every `AuthInfo`, `Cluster` and context before building anything.

**Refactor.** Table of `(field name, value)` checks for file-path references. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** Never call any client-go function that reads the default kubeconfig locations or environment (`clientcmd.NewDefaultClientConfigLoadingRules`, `BuildConfigFromFlags("", path)`). OIDC token refresh is implemented in Step 3.1, not through client-go's auth-provider plugin (do not import `k8s.io/client-go/plugin/pkg/client/auth/...`).

### Step 2.9 — Cluster list

**Goal.** One endpoint that lists the session's clusters from both sources.

**Files.** Create `internal/cluster/registry.go`, `internal/cluster/registry_test.go`, `internal/httpapi/clusters.go`, `internal/httpapi/clusters_test.go`.

API: `GET /api/v1/clusters` → `{ sources:[{ type:"vault"|"kubeconfig", label, status:"ok"|"unreachable"|"expired", clusters:[{ id, name, roles?:[…], selectedRole?, state:"closed"|"open"|"disconnected", server? }] }] }`.

**Red.** Cases:
- No credentials → `{sources:[]}`.
- Vault token with two allowed mounts → one source `vault` with clusters `v:a`, `v:b`, sorted, with roles.
- Kubeconfig `prod` with two contexts → source `kubeconfig` label `prod`, clusters `k:prod/ctx1`, `k:prod/ctx2`.
- Both at once → Vault source first, then kubeconfig sources sorted by label.
- Discovery results are cached per session for 60 s (fake clock): two calls → one `Mounts` call; after 61 s → two; adding or renewing a token invalidates the cache.
- Vault unreachable → the Vault source has `status:"unreachable"` and lists the clusters from the last successful discovery (or none); kubeconfig sources unaffected.
- Vault token expired → `status:"expired"`, its clusters `state:"disconnected"`.
- `Registry.Resolve(ctx, sess, clusterID)` returns a `Ref{ID, Source, Mount, Roles, Label, Context}` or `apperr.ErrNotFound` for any ID not in the session (another user's kubeconfig cluster and a non-existent one are indistinguishable → HTTP 404 `cluster_unavailable` with the spec's message `This cluster isn't available in your session. If it comes from a kubeconfig, upload that kubeconfig first.`).

Command: `make test-backend PKG="./internal/cluster/... ./internal/httpapi/..."`. Expected failure: `undefined: cluster.NewRegistry`.

**Green.** Implement as specified; the cache is an in-memory map keyed by session hash with entries dropped by a registry closer when the session ends.

**Refactor.** None. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** Credentials are fetched lazily (spec: "100+ clusters … credentials fetched lazily"): listing must not call `ClusterConfig` or `Creds` (assert zero calls on the fake).

### Step 2.10 — Web: sign-in, saved credentials, cluster rail, banners

**Goal.** The complete browser side of Phase 2.

**Files.** Create under `web/src/features/auth/`: `SignInPage.tsx`, `signinErrors.ts`; under `web/src/features/credentials/`: `vaultStore.ts` (IndexedDB), `useRestore.ts`, `AddCredentials.tsx`, `VaultTokenForm.tsx`, `KubeconfigForm.tsx`, `SavedCredentials.tsx`; under `web/src/features/clusters/`: `ClusterRail.tsx`, `useClusters.ts`; `web/src/features/shell/Banners.tsx`; tests for each. `npm install --save-exact idb@8.0.4` and `npm install --save-dev --save-exact fake-indexeddb@6.2.8`.

**Red.** Cases:
- `SignInPage`: shows one button per provider from `/auth/providers` as links to `/auth/<provider>/login`; no providers → message "No sign-in provider is configured."; `?signin_error=not_allowed` → "This account isn't allowed to sign in."; `state`, `denied`, `failed` → their fixed texts.
- `vaultStore` (with `fake-indexeddb/auto`): `put({id, kind, label, sealed, savedAt})`, `list()`, `remove(id)`, `clear()`; database `kubelook`, object store `credentials`; items are scoped by `userId` (a second user's items are not listed).
- `useRestore`: after sign-in, sends all stored copies to `/credentials/restore`; `ok` items stay; `key_changed` → all copies removed and the spec's message shown once; `expired` vault token → copy removed and the "paste a new token" instruction shown; `wrong_user`/`invalid` → that copy removed silently; `vault_unreachable` → copy kept.
- `VaultTokenForm`: password-type input, submit posts the token, stores the returned sealed copy, clears the input; server error message is shown; the token is never written to `localStorage`, the URL or the query cache (assert).
- `KubeconfigForm`: file picker and paste box; label defaults to the file name without extension; shows the server's rejection message verbatim as text.
- `SavedCredentials` (settings page section): lists saved copies with kind, label, saved date; "Remove" deletes the browser copy and calls the matching DELETE endpoint; explanatory text that removal affects this browser only.
- `ClusterRail`: groups by source with headings "Vault" and each kubeconfig label; status dot with an accessible label ("connected", "disconnected", "not opened"); favourites pinned on top (from settings); clicking navigates to `/c/<encoded id>`; role picker shown when a Vault cluster has more than one role.
- `Banners`: maintenance banner from `me.maintenanceBanner`; Vault expiry warning with remaining time (fake timers; updates each minute) and a **Renew** button when `canRenew`; without `canRenew` the text tells the user to get a new token from Vault and paste it in settings; expired state text; "Vault is unreachable" banner when the Vault source status is `unreachable`.
- No credentials yet → `AddCredentials` is the landing page with both forms (kubeconfig form hidden when `features.kubeconfig` is false; Vault form hidden when `features.vault` is false).

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green.** Implement. Text is plain React children everywhere.

**Refactor.** Shared `Field`, `Button`, `ErrorText` components in `web/src/components/` with their own tests. Re-run.

**Acceptance.** `make test-frontend` passes; `make coverage-frontend` passes; `make verify` → `VERIFY OK`.

**Pitfalls.** IndexedDB access is asynchronous: in tests `await` the store calls, and use `findBy…` queries rather than timers. Safari may clear IndexedDB (spec); treat an empty store as normal.

**Phase 2 exit check.** `make verify` → `VERIFY OK`. The cluster list for a Vault token is computed and displayed; opening a cluster comes next.

---
# Phase 3 — Cluster access and live data

**Delivered in pull request 4**.

Outcome: opening a cluster mints credentials; lists are live through one WebSocket per tab; pod logs stream. This completes the spec's spike scope (sign-in, discovery, pods list, logs).

### Step 3.1 — Opening a cluster and the credential broker

**Goal.** Turn a cluster ID into working Kubernetes credentials: mint from Vault, re-mint before expiry, refresh kubeconfig OIDC tokens, never write to disk.

**Files.** Create `internal/cluster/opener.go`, `internal/cluster/oidc.go`, `internal/cluster/opener_test.go`, `internal/cluster/oidc_test.go`, `internal/httpapi/open.go`(+test).

API: `POST /api/v1/clusters/{cluster}/open` `{role?}` → `{state:"open", server, role?, credentialExpiresAt?}`. `GET /api/v1/credentials/pending` → `{items:[{label, sealed}]}`; `POST /api/v1/credentials/pending/ack` `{labels:[…]}` → 204.

**Red.** Cases (fakevault, fake clock, real store):
- Open `v:prod`: calls `ClusterConfig` then `Creds` with `kubernetes_namespace=<vault.credsNamespace>`, `cluster_role_binding=<vault.clusterRoleBinding>`, `ttl=<vault.tokenTTL>`; stores sealed `cluster_cred` `{host, caCert, token, serviceAccount, leaseId, role, mintedAt, expiresAt}` with `expires_at = now + lease_duration`; audit `vault-credential-issued` with `details.path="kubernetes/prod/creds/devops-role"` and `details.leaseId`, never the token.
- Role: omitted → default role; a role not in the cluster's allowed list → 403; chosen role is remembered (`cluster_role`).
- Empty `kubernetes_host` or `kubernetes_ca_cert`, or a mount whose config read returned 404 (A30) → 422 `unprocessable`, message `This cluster's Vault mount has no API server address or CA configured, so Kubelook can't open it.`; nothing stored.
- Vault unreachable → 503 `vault_unreachable`, message `Vault is unreachable, so this cluster can't be opened right now`.
- Vault refuses creds (a `*ResponseError` with status 400 or 500, for example "a ClusterRoleBinding cannot ref a Role") → 422 with Vault's message passed through as text.
- `Token(ctx, hash, id)`: more than `remintBefore` left → stored token, no Vault call; less → mints a new one, stores it, returns it, audits; concurrent callers during a re-mint cause exactly one `Creds` call (singleflight keyed by session+cluster).
- Re-mint fails with `ErrUnreachable` while the current token is still valid → returns the current token (spec: open clusters keep working during a Vault outage); after expiry → error `vault_unreachable`.
- Vault token gone (403 on creds) → cluster state `disconnected`, error `unauthenticated`-class with message telling the user to paste a new Vault token.
- `RunRefresher`: on a 30 s fake ticker, re-mints every credential of this replica's "active" clusters (used in the last `session.idleTimeout`) that is within `remintBefore`; stops for a session when it ends (assert no `Creds` call after `End`).
- `RESTConfig`: `Host`, `TLSClientConfig.CAData`, no `BearerToken`/`BearerTokenFile`; `WrapTransport` sets `Authorization: Bearer <Token()>` on every request (assert with an `httptest` TLS server acting as API server: first request carries token A, after advancing the clock past the threshold the next carries token B).
- Kubeconfig cluster: `Open` builds the config from the stored file; no Vault calls; state `open`.
- OIDC kubeconfig: ID token with `exp` in the past (fake clock) → refresh against a `fakeoidc` issuer using `refresh_token`, `client-id`, optional `client-secret`, optional `idp-certificate-authority-data`; new ID token used; when the issuer returns a new refresh token, the session's kubeconfig credential is updated **and** a new sealed browser copy is queued as a `pending_copy` credential, returned by `/credentials/pending` until acknowledged.
- Session end deletes `cluster_cred` rows (cascade) and drops in-memory caches; no lease revocation call exists (assert the fake has no revoke endpoint hit).

Command: `make test-backend PKG="./internal/cluster/... ./internal/httpapi/..."`. Expected failure: `undefined: cluster.NewOpener`.

**Green.**

```go
type Opener struct{…} // deps: Registry, Credentials, store.Sessions, crypto.Sealer, vault.API, clock, config, audit, session.Manager
func (o *Opener) Open(ctx context.Context, sess session.Session, clusterID, role string) (OpenState, error)
func (o *Opener) Token(ctx context.Context, sessHash, clusterID string) (string, error)
func (o *Opener) RESTConfig(ctx context.Context, sess session.Session, clusterID string) (*rest.Config, error) // opens lazily if needed
func (o *Opener) State(ctx context.Context, sess session.Session, clusterID string) OpenState
func (o *Opener) RunRefresher(ctx context.Context)
```

`rest.Config` extras: `QPS: 50`, `Burst: 100`, `UserAgent: "kubelook/<version>"`, `Timeout: 0` (streams).

**Refactor.** Split Vault and kubeconfig paths behind an unexported `source` interface with `open` and `token`. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.**
- Decode a JWT's `exp` without verifying it only to decide *when* to refresh; never trust it for anything else.
- Implement singleflight with a small mutex-guarded map of in-flight calls; do not add a dependency.
- The CA from Vault is PEM text; pass it as `CAData` bytes unchanged.

### Step 3.2 — Kubernetes client factory and discovery

**Goal.** Per-session, per-cluster client-go clients, and a cached description of what the cluster serves.

**Files.** Create `internal/kube/clients.go`, `internal/kube/discovery.go`, `internal/kube/*_test.go`, `internal/httpapi/discovery.go`(+test), `internal/testutil/fakekube/fakekube.go`.

`fakekube` provides: a `kube.Clients` implementation backed by client-go fakes (`dynamic/fake.NewSimpleDynamicClientWithCustomListKinds`, `kubernetes/fake.NewClientset`, `discovery/fake.FakeDiscovery`) and helper constructors for unstructured objects (`fakekube.Pod(ns, name, …)`).

API: `GET /api/v1/clusters/{cluster}/discovery` → `{ serverVersion, resources:[{group, version, resource, kind, namespaced, verbs:[…], preferred:bool, servedVersions:[…]}] }`.

**Red.** Cases:
- `Clients.Dynamic/Typed/Discovery/REST` build from `Opener.RESTConfig`; the same session+cluster returns the same cached client; a different session gets a different client; ending the session drops the cache entry (registry closer).
- Discovery result excludes subresources (names containing `/`) from the main list but records them (`Has("pods","exec")`).
- `Resolve(group, resource)` returns the preferred served version's GVR; `ResolveKind(group, kind)` likewise; unknown → `ErrNotFound`.
- Partial discovery failure (one API group unavailable, `discovery.ErrGroupDiscoveryFailed`) → the other groups are still returned.
- Cached 60 s per session+cluster (fake clock); `Invalidate` forces a refresh (used after CRD changes).
- HTTP: unknown cluster → 404 `cluster_unavailable`; result sorted by group, then resource.

Command: `make test-backend PKG="./internal/kube/... ./internal/httpapi/..."`. Expected failure: `undefined: kube.NewClients`.

**Green.**

```go
type Clients interface {
	Dynamic(ctx context.Context, sess session.Session, clusterID string) (dynamic.Interface, error)
	Typed(ctx context.Context, sess session.Session, clusterID string) (kubernetes.Interface, error)
	Discovery(ctx context.Context, sess session.Session, clusterID string) (*Discovery, error)
	REST(ctx context.Context, sess session.Session, clusterID string) (*rest.Config, error)
}
type Resource struct{ Group, Version, Resource, Kind string; Namespaced bool; Verbs []string; Preferred bool; ServedVersions []string }
```

**Refactor.** None. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** Use `ServerGroupsAndResources()` and tolerate its partial-failure error. Do not use informers or shared caches: each call runs under one user's credential.

### Step 3.3 — Redaction and pruning

**Goal.** One function every object passes through before it is cached or sent to a browser (spec "Secret handling").

**Files.** Create `internal/redact/redact.go`, `internal/redact/redact_test.go`.

**Red.** `redact.Apply(obj *unstructured.Unstructured) (keep bool)` cases:
- Any object: `metadata.managedFields` removed.
- `v1/Secret`: every value under `data` replaced by the constant `redact.Mask` (`"********"`), keys kept; `stringData` removed; annotation `kubectl.kubernetes.io/last-applied-configuration` removed; `type`, labels, other annotations kept.
- Secret with `type: helm.sh/release.v1` or name starting `sh.helm.release.v1.` → `keep == false`.
- ConfigMap with name starting `sh.helm.release.v1.` and label `owner=helm` → `keep == false`. Other ConfigMaps unchanged (their data is not secret).
- Non-Secret objects keep `last-applied-configuration`.
- `redact.List(items)` applies to each and drops `keep == false` items.
- `redact.Manifest(yamlText)` (multi-document, used for Helm manifests): Secret documents get masked values, other documents are byte-for-byte unchanged; a document that fails to parse is replaced by `# unparseable document removed`.
- `redact.Unmask(edited, live)` for saves: every `data` value equal to `Mask` is replaced by the live object's value for that key; a masked key that no longer exists live → `ErrConflict`; non-masked values pass through; non-Secrets returned unchanged.
- Property test: for 200 generated Secrets (deterministic generator from `testutil.Rand`), no original data value appears anywhere in the JSON of the redacted object.

Command: `make test-backend PKG=./internal/redact/...`. Expected failure: `undefined: redact.Apply`.

**Green.** Implement with `unstructured` helpers (`unstructured.NestedMap`, `SetNestedField`, `RemoveNestedField`).

**Refactor.** None. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** `Mask` contains `*`, which is not in the base64 alphabet, so it can never equal a real `data` value. Apply redaction **before** caching in the watch hub, so unredacted Secret values never sit in memory.

### Step 3.4 — Generic resource read API

**Goal.** List and get any served kind through one handler.

**Files.** Create `internal/kube/resources.go`(+test), `internal/httpapi/resources.go`(+test).

API:
- `GET /api/v1/clusters/{cluster}/resources/{group}/{version}/{resource}?namespace=<ns>&labelSelector=&fieldSelector=&limit=&continue=` → `{items:[…], resourceVersion, continue}`.
- `GET /api/v1/clusters/{cluster}/resources/{group}/{version}/{resource}/{name}?namespace=<ns>` → the object.

**Red.** Cases (fakekube): list namespaced with and without `namespace`; cluster-scoped; selectors passed through; `limit` default 500, max 5000; `core` maps to the empty group; unknown resource → 404; API `Forbidden` → 403 with the API server's message as text; `NotFound` → 404; every returned object passed through `redact.Apply` (a Secret's values are masked; a Helm release Secret is absent from lists and 404 on get); items keep API order.

Command: `make test-backend PKG="./internal/kube/... ./internal/httpapi/..."`. Expected failure: `undefined: kube.NewResources`.

**Green.**

```go
type Resources struct{ clients Clients }
func (r *Resources) List(ctx, sess, clusterID string, gvr schema.GroupVersionResource, ns string, o ListOptions) (*unstructured.UnstructuredList, error)
func (r *Resources) Get(ctx, sess, clusterID string, gvr schema.GroupVersionResource, ns, name string) (*unstructured.Unstructured, error)
func MapError(err error) error    // k8s apierrors → apperr sentinels, keeping the message
```

**Refactor.** `parseTarget(r *http.Request) (clusterID string, gvr, ns, name, err)` shared by later handlers. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** Path values arrive percent-decoded from `r.PathValue`; the cluster ID may contain `/` only when encoded as `%2F` — register routes with `{cluster}` and require clients to encode. Add a test with `k%3Aprod%2Fctx`.

### Step 3.5 — RBAC-aware access checks

**Goal.** Tell the UI what the user may do, using `SelfSubjectRulesReview` as a first pass and `SelfSubjectAccessReview` as the decider.

**Files.** Create `internal/kube/access.go`(+test), `internal/httpapi/access.go`(+test).

API: `POST /api/v1/clusters/{cluster}/access` `{checks:[{verb, group, resource, subresource?, namespace?, name?}]}` → `{results:[{allowed:bool, reason?}]}` in the same order (max 100 checks).

**Red.** Cases (typed fake with reactors for the two review APIs):
- Rules review allows the verb/resource in that namespace → `allowed:true` without an access review (assert 0 SSAR calls).
- Rules review does not allow it → SSAR is called; its answer decides (true → allowed; false → `allowed:false` with reason "missing permission: <verb> <resource>").
- Rules review marked `incomplete: true` → still only a first pass; SSAR decides negatives.
- Wildcards in rules (`*` verbs, resources, groups) and `resourceNames` handled; a rule limited by `resourceNames` does not allow a check with a different name.
- Cluster-scoped checks skip the rules review and use SSAR.
- Results cached per session+cluster+check for 60 s; errors are not cached.
- Shell check helper `CanExec(ns, pod)` = `create` on `pods/exec` (spec: Kubernetes 1.35 requires `create` for WebSocket exec).
- More than 100 checks → 400.

Command: `make test-backend PKG="./internal/kube/... ./internal/httpapi/..."`. Expected failure: `undefined: kube.NewAccess`.

**Green.** `func (a *Access) Can(ctx, sess, clusterID string, c Check) (bool, string, error)` and `CanAll`.

**Refactor.** Extract `ruleAllows(rule, check)`. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** An action is hidden or disabled only after SSAR denies it (spec). Server handlers for mutations do **not** pre-check: the API server is the authority; the check endpoint is for the UI only.

### Step 3.6 — WebSocket connection and channel multiplexing

**Goal.** One WebSocket per tab carrying tagged channels with a heartbeat, origin check and bounded queues.

**Files.** Create `internal/ws/protocol.go`, `internal/ws/conn.go`, `internal/ws/mux.go`, `internal/ws/*_test.go`, `internal/httpapi/ws.go`(+test).

Protocol (JSON text frames, A16):
- Client → server: `{"op":"<name>","ch":<int ≥ 0>,"d":{…}}`.
- Server → client: `{"ch":<int>,"t":"<type>","d":{…}}`.
- Channel 0 is control: server `ping` every 20 s, client answers `{"op":"pong","ch":0}`; client `{"op":"visibility","ch":0,"d":{"hidden":true|false}}`.
- A channel is opened by an op ending in `.open` with a client-chosen `ch` (1…2^31-1, unique per connection). Server answers `{t:"opened"}` or `{t:"error", d:{code,message}}`. Any later op with that `ch` goes to the channel. `<prefix>.close` or server `{t:"closed", d:{reason}}` ends it.
- Close codes: `4401` session ended, `4408` heartbeat timeout, `1001` server shutting down, `1009` message too big, `1008` protocol error.

**Red.** `go get github.com/coder/websocket@v1.8.15`. Tests use `httptest.NewServer` and `websocket.Dial` (loopback, deterministic by reading frames in order):
- Upgrade without a session → 401. With `Origin` not matching the public URL host → 403. Matching → 101.
- Unknown op → `error` frame code `bad_request`, connection stays open. Malformed JSON → close 1008.
- Registered factory `echo.open`: open on ch 5 → `opened`; `echo.say` → `said` frame on ch 5; `echo.close` → `closed`; reusing an open `ch` → `error`; more than 100 open channels → `error` code `server_busy`.
- Heartbeat with the fake clock: after 20 s a `ping` arrives; with no pong for 60 s the server closes with 4408; a pong resets the timer.
- `visibility` updates `conn.Hidden()` and notifies subscribers registered with `conn.OnVisibility(fn)`.
- Outbound queue (capacity 1024): `Send(msg)` never blocks; when full, a message sent with `SendDroppable` evicts the oldest droppable message of the same channel and increments that channel's drop counter; when full and nothing is droppable, `Send` returns `ErrSlowConsumer` and the channel that called it is closed with `{t:"closed", d:{reason:"slow_consumer"}}`.
- Client frames over 1 MiB → close 1009.
- Session end (registry closer) → close 4401 and every channel's `Close()` called once. Server shutdown → 1001.
- Any non-pong client frame calls `session.Manager.KeepAlive`.
- No goroutine leaks (`goleak`).

Command: `make test-backend PKG="./internal/ws/... ./internal/httpapi/..."`. Expected failure: `undefined: ws.NewMux`.

**Green.**

```go
type Channel interface {
	Handle(ctx context.Context, op string, d json.RawMessage) error
	Close()
}
type Factory func(ctx context.Context, c *Conn, ch int, d json.RawMessage) (Channel, error)
type Mux struct{…}
func NewMux(d Deps) *Mux                         // deps: clock, log, session.Manager, public origin host
func (m *Mux) Register(openOp string, f Factory) // e.g. "watch.open"
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request)
type Conn struct{…}
func (c *Conn) Session() session.Session
func (c *Conn) Send(ch int, typ string, d any) error
func (c *Conn) SendDroppable(ch int, typ string, d any)
func (c *Conn) Hidden() bool
func (c *Conn) OnVisibility(fn func(hidden bool)) (cancel func())
```

One reader goroutine and one writer goroutine per connection; `websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{host}})`; `conn.SetReadLimit(1<<20)`.

**Refactor.** Separate the queue into `queue.go` with its own unit tests (pure data structure). Re-run.

**Acceptance.** `go test -race -count=10 ./internal/ws/...` → `ok`.

**Pitfalls.**
- Only the writer goroutine writes to the socket. Never call `conn.Write` from a channel.
- Tests must read the `opened` frame before sending the next op; never assume ordering between two different connections.
- Browsers cannot set headers on WebSocket upgrades: authentication is the session cookie plus the Origin check; there is no CSRF header here (spec).

### Step 3.7 — Watch hub

**Goal.** One upstream watch per user, cluster, kind and namespace, fanned out to that user's views; resume, `410 Gone`, idle close, limits.

**Files.** Create `internal/watch/hub.go`, `internal/watch/upstream.go`, `internal/watch/channel.go`, `internal/watch/*_test.go`. Modify `internal/app/serve.go`.

Channel: `watch.open` `d = {cluster, group, version, resource, namespace}`. Server frames: `snapshot {items, resourceVersion}`, `event {type:"ADDED"|"MODIFIED"|"DELETED", object}`, `paused {}` (upstream closed for idleness), `closed {reason}`.

**Red.** Cases (fakekube dynamic client with `PrependWatchReactor` returning `watch.NewFake()` / `watch.NewRaceFreeFake()`; fake clock):
- First subscriber: one `List` (with `ResourceVersion` unset) then one `Watch` from the list's `resourceVersion` with `AllowWatchBookmarks: true`; subscriber receives `snapshot`.
- Second subscriber of the same user and key: **no** new List/Watch; it gets a `snapshot` from the hub's cache that equals the current state (after two events).
- Another user with the same key gets a separate upstream (assert two watches).
- `ADDED`/`MODIFIED`/`DELETED` reach all subscribers in order; cache updated; objects are redacted and pruned before caching (a Secret event carries masked values; a Helm release Secret event is not delivered).
- `BOOKMARK` updates the stored `resourceVersion` and is not forwarded.
- Upstream channel closes (network blip) → a new `Watch` starts from the last seen `resourceVersion`; no new snapshot is sent.
- Watch returns an `ERROR` event with status code 410 (or `Watch` returns `apierrors.IsGone`) → `List` again, send a fresh `snapshot` to all subscribers, then `Watch`.
- Repeated upstream failures back off 1 s, 2 s, 4 s … max 30 s on the fake clock; a `Forbidden` list → channel `error` with code `forbidden` and no retry.
- Idle: last active subscriber leaves (or all its connections become hidden) → after the user's `WatchIdleSeconds` (default 30) the upstream stops and remaining (hidden) subscribers get `paused`; a subscriber returning before the timeout cancels the stop.
- Limit: a user's 51st watch closes that user's oldest watch that has only hidden subscribers (they get `paused`); if none is hidden, the open fails with code `too_many_watches`.
- Session end closes all its upstreams. `Stats()` returns counts for metrics (watches, subscribers, cached objects).
- `goleak` clean after `hub.Close()`.

Command: `go test -race -count=5 ./internal/watch/...`. Expected failure: `undefined: watch.NewHub`.

**Green.**

```go
type Key struct{ SessionHash, UserID, Cluster string; GVR schema.GroupVersionResource; Namespace string }
type Subscriber interface {
	Snapshot(items []*unstructured.Unstructured, rv string)
	Event(typ string, obj *unstructured.Unstructured)
	Paused()
	Failed(code, message string)
	Active() bool      // false while the tab is hidden
}
type Hub struct{…}
func NewHub(d Deps) *Hub   // deps: kube.Clients, clock, log, settings lookup (idle seconds per user), maxPerUser
func (h *Hub) Subscribe(ctx context.Context, sess session.Session, k Key, s Subscriber) (unsubscribe func(), err error)
func (h *Hub) Factory() ws.Factory
```

Each upstream runs one goroutine owning its cache (`map[types.UID]*unstructured.Unstructured`) and subscriber list; all access goes through that goroutine's command channel (no shared locks).

**Refactor.** Move the list-then-watch loop into `upstream.run` with the back-off as a small type `backoff{clock, min, max}` tested alone. Re-run.

**Acceptance.** Tests `ok` with `-count=5`.

**Pitfalls.**
- Namespace `""` means all namespaces; it is a different key from each single namespace.
- Send snapshots as one frame; for very large lists split into frames of 500 items with `d.more=true` on all but the last (test with 1,200 items → 3 frames).
- The fake watcher's `Add` blocks until read: drive it from the test goroutine and read frames with `testutil.Recv`.

### Step 3.8 — Web: WebSocket client, live store, generic table, Pods

**Goal.** The first live view: the Pods list of an open cluster.

**Files.** Create `web/src/ws/socket.ts`, `web/src/ws/socket.test.ts`, `web/src/test/FakeWebSocket.ts`, `web/src/state/live.ts`(+test), `web/src/api/discovery.ts`(+test), `web/src/api/access.ts`(+test), `web/src/kinds/types.ts`, `web/src/kinds/registry.ts`(+test), `web/src/kinds/core/pods.ts`(+test), `web/src/lib/age.ts`(+test), `web/src/lib/window.ts`(+test), `web/src/components/Table.tsx`(+test), `web/src/components/StatusBadge.tsx`(+test), `web/src/features/resources/ResourceListPage.tsx`(+test), `web/src/features/clusters/ClusterPage.tsx`(+test).

**Red.** Cases:
- `socket.ts` (`createSocket({ url, WebSocketCtor, timers, visibility })`): connects; `open(op, d, handlers)` allocates increasing channel numbers and sends the open frame; frames are routed to the right handlers; `pong` answers `ping`; on close it reconnects with back-off 1 s, 2 s, 4 s … 30 s (fake timers) and re-opens every channel that was open; close code 4401 → no reconnect, `onSessionEnded` called; visibility change sends the `visibility` op; on becoming visible, channels that received `paused` are re-opened.
- `live.ts`: `subscribe(key)` opens `watch.open`; `snapshot` (including multi-frame `more`) replaces the list; events add, replace by `metadata.uid`, remove; two subscribers of the same key share one channel and it closes when the last unsubscribes; status `loading` → `live` → `paused`/`error`.
- `kinds/registry.ts`: `KindDescriptor = { group, kind, resource, namespaced, title, section, columns: Column[], status?: (obj) => Status, actions?: ActionId[] }`; `Column = { id, title, value: (obj) => string | number, sort?: 'text'|'number'|'age' }`; `getKind(group, kind)`; unknown kinds fall back to a generic descriptor with Name, Namespace, Age.
- `pods.ts` columns, each with a table-driven test over fixture pods: Name; Namespace; Ready (`2/3`); Status (same derivation as `kubectl get pods`: `Pending`, `Running`, `Succeeded`, `Failed`, container waiting reasons such as `CrashLoopBackOff`, `ImagePullBackOff`, `Init:1/2`, `Terminating` when `deletionTimestamp` is set); Restarts (sum over containers); Node; Age.
- `age.ts`: `45s`, `12m`, `3h`, `2d`, `5w`… (kubectl-style, from an injected `now`).
- `window.ts`: `visibleRange({ rowCount, rowHeight, viewportHeight, scrollTop, overscan })` returns `[start, end)`; edge cases (empty, scrolled past end).
- `Table`: renders only the windowed rows; sort by clicking a header (toggles asc/desc, `aria-sort` set); text filter; multi-select with checkboxes and shift-range; keyboard: arrow keys move the active row, Enter opens it; `aria-rowcount` equals the total.
- `StatusBadge`: text plus an icon with an accessible label; colour is never the only signal.
- `ResourceListPage`: given a cluster and kind, calls `/open` if the cluster is not open, subscribes, shows the table; shows "You don't have permission to list <kind>" on a `forbidden` error; shows "Live updates paused" on `paused`.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green.** Implement. The WebSocket URL is `(location.protocol === 'https:' ? 'wss' : 'ws') + '://' + location.host + '/api/v1/ws'`.

**Refactor.** Move column helpers (`get(obj, 'status.phase')`) to `web/src/lib/path.ts` with tests. Re-run.

**Acceptance.** `make test-frontend` and `make coverage-frontend` pass. Manual check is deferred to the integration phase; nothing else to run.

**Pitfalls.** jsdom has no layout: the table takes `viewportHeight` from a prop with a default measured via `ResizeObserver` (inject a fake in tests). `Table` must not re-render all rows on every event: key rows by `metadata.uid`.

### Step 3.9 — Log streaming (backend)

**Goal.** Follow pod logs through a WebSocket channel with batching, rate cap, bounded buffer and audit.

**Files.** Create `internal/logs/stream.go`, `internal/logs/source.go`, `internal/logs/channel.go`, `internal/logs/*_test.go`.

Channel: `logs.open` `d = {cluster, namespace, pod, container, follow, tailLines?, sinceSeconds?, sinceTime?, previous?}`. Server frames: `lines {lines:[{ts, text}]}`, `dropped {count}`, `eof {}`, `closed {reason}`.

**Red.** Cases (a fake `Source` returning an `io.Pipe`; fake clock):
- Lines written upstream arrive as `lines` frames with the RFC 3339 timestamp split from the text (the stream is always requested with `Timestamps: true`).
- Batching: lines are flushed when 500 are pending or on a 100 ms fake-clock tick, whichever comes first.
- Rate cap: with `maxLinesPerSecond=1000`, writing 3,000 lines within one fake second delivers 1,000 and a `dropped {count:2000}` frame; the next second delivers again.
- Slow browser: the per-stream buffer holds 2,000 lines; overflow drops the oldest and reports `dropped`.
- A line longer than 64 KiB is truncated with the suffix `… [truncated]`.
- Non-UTF-8 bytes are replaced with U+FFFD.
- Upstream EOF with `follow=false` → `eof`; with `follow=true` on a terminated container → `eof` then `closed`.
- Options map to `corev1.PodLogOptions` exactly (`TailLines`, `SinceSeconds`, `SinceTime`, `Previous`, `Follow`, `Container`).
- Audit: one `log-view-open` record at open (cluster, namespace, pod, container, tail/since) and one `log-view-close` at close; if the open record cannot be written the stream is refused; log text never appears in audit output.
- Closing the channel cancels the upstream request (the pipe's reader is closed).
- While open, `KeepAlive` is called on each tick (A12).
- API errors (pod not found, forbidden, container not ready) → `error` frame with the API message as text.

Command: `make test-backend PKG=./internal/logs/...`. Expected failure: `undefined: logs.NewFactory`.

**Green.**

```go
type Source interface {
	Stream(ctx context.Context, sess session.Session, clusterID, ns, pod string, o *corev1.PodLogOptions) (io.ReadCloser, error)
}
func NewKubeSource(c kube.Clients) Source   // Typed().CoreV1().Pods(ns).GetLogs(pod, o).Stream(ctx)
func NewFactory(d Deps) ws.Factory
```

Read with a `bufio.Reader` and `ReadSlice('\n')` handling `bufio.ErrBufferFull` for long lines.

**Refactor.** Token bucket as its own small type with tests. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** `NewKubeSource` is three lines over client-go; test it with the typed fake (`GetLogs` on the fake returns the string `fake logs`).

### Step 3.10 — Log viewer (web, basic)

**Goal.** A dock tab that follows one container's logs.

**Files.** Create `web/src/features/logs/useLogStream.ts`, `LogViewer.tsx`, `logBuffer.ts`, tests; `web/src/features/dock/Dock.tsx`, `web/src/state/dock.ts`, tests.

**Red.** Cases:
- `logBuffer`: append keeps at most `liveBufferLines` (from `me.limits`), dropping the oldest; `dropped` markers are inserted as a visible line "… N lines dropped …".
- `useLogStream`: opens `logs.open`; pause stops appending to the view but keeps buffering; resume shows buffered lines; on reconnect re-opens with `sinceTime` = last timestamp.
- `LogViewer`: container picker; follow toggle (auto-scroll only while at the bottom); timestamps toggle; tail size and "since" selectors; "previous instance" toggle; client-side search (plain or regex, invalid regex shows an inline error and filters nothing) with matches wrapped in `<mark>`; download of the current buffer as text (uses an injected `saveFile(name, blob)`); lines are rendered as text nodes.
- `Dock`: tabs for open items; closing a tab closes its channel; tabs survive route changes (state lives in `state/dock.ts`); the dock is a `region` named "Dock"; each tab is keyboard reachable.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green.** Implement; the log list uses the same windowing helper as `Table`.

**Refactor.** Extract `highlight(text, query)` to `web/src/lib/highlight.ts` with tests. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** Build regexes with `new RegExp(source, 'gi')` inside `try/catch`; cap highlighted matches per line at 100 to avoid pathological patterns freezing the tab.

### Step 3.11 — Vault outage handling

**Goal.** The spec's "When Vault is unreachable" behaviour, end to end.

**Files.** Create `internal/vault/health.go`(+test), `internal/httpapi/status.go`(+test). Modify `internal/vault/client.go` (add `Health`), `web/src/features/shell/Banners.tsx`(+test), `web/src/api/status.ts`(+test).

API: `GET /api/v1/status` → `{vault:{configured, reachable, since}}`.

**Red.** Cases:
- `Health(ctx)` calls `GET /v1/sys/health?standbyok=true&perfstandbyok=true` without a token: 200 → reachable; 429/472/473 (standby states) → reachable; 501/503 or network error → unreachable.
- `Monitor` on a 15 s fake ticker flips `reachable` after **two** consecutive failures and back after one success; any real API call that returns `ErrUnreachable` flips it immediately; a successful one flips it back.
- `/status` reflects the monitor. With `vault.address` empty → `configured:false`.
- Web: banner "Vault is unreachable. Open clusters keep working until their credentials expire; new Vault clusters can't be opened." appears when `reachable:false` and disappears when it returns (query polled every 15 s with fake timers); the open button for closed Vault clusters is disabled with that explanation; kubeconfig clusters unaffected.

Command: `make test-backend PKG="./internal/vault/... ./internal/httpapi/..."` and `make test-frontend`. Expected failure: `undefined: vault.NewMonitor`.

**Green.** Implement.

**Refactor.** None. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** The monitor uses no user token (it runs without a session).

**Phase 3 exit check.** `make verify` → `VERIFY OK`.

---
# Phase 4 — Core features

**Delivered in pull request 5 (Steps 4.1–4.10) and pull request 6 (Steps 4.11–4.19)**.

Outcome: the spec's "Phase 1. Core": all core kinds live, editing with dry run, actions, shells, debug containers, file browser, durable jobs and bulk actions, structured logs, field filter, exports, settings, navigation.

### Step 4.1 — Mutating resource API with audit

**Goal.** Create, replace, patch and delete any object; dry run; conflicts; every mutation audited (A14).

**Files.** Modify `internal/kube/resources.go`, `internal/httpapi/resources.go`; create `internal/httpapi/apply.go`; tests alongside.

API (same paths as Step 3.4):
- `POST …/{resource}?namespace=&dryRun=true|false` body = object → created object.
- `PUT …/{resource}/{name}?namespace=&dryRun=` body = whole object → updated object. With `dryRun=true` the response is `{live, result}` (both redacted).
- `PATCH …/{resource}/{name}` with `Content-Type: application/merge-patch+json` or `application/strategic-merge-patch+json`.
- `DELETE …/{resource}/{name}?propagationPolicy=Background|Foreground|Orphan` (default Background).
- `POST /api/v1/clusters/{cluster}/apply` `{documents:["<yaml>", …], dryRun}` → `{results:[{index, kind, namespace, name, action:"created"|"updated", error?}]}`.

**Red.** Cases:
- `PUT` sends an `Update` with `FieldManager: "kubelook"`; body without `metadata.resourceVersion` → 400 `bad_request` "resourceVersion is required"; API `Conflict` → 409 `conflict` "The object changed since you loaded it."
- `dryRun=true` adds `DryRun: ["All"]` and writes **no** audit record; an API error whose message contains `does not support dry run` → 422 with code `dry_run_unsupported`.
- Body name/namespace/kind must match the path → otherwise 400.
- Secrets: masked values are restored with `redact.Unmask` against a fresh live read before the update; the response is redacted; a Helm release Secret cannot be written (404).
- Every non-dry-run call: `audit.Begin` (verb `create`/`update`/`patch`/`delete`, cluster, namespace, kind, name) before the API call; if `Begin` fails the API is **not** called and the response is 500 `internal` "The action was refused because it could not be recorded."; `Done` afterwards with the API error if any.
- Admission denial (API `Invalid`/`Forbidden` with webhook message) → 422/403 with the message as text.
- `apply`: each YAML document is resolved through discovery (`apiVersion`+`kind`); existing object → update using the live `resourceVersion`; missing → create; results per document, continuing after a failed document; unknown kind → that document's `error`; more than 100 documents or over 1 MiB total → 413.
- Delete of a missing object → 404.

Command: `make test-backend PKG="./internal/kube/... ./internal/httpapi/..."`. Expected failure: new methods undefined.

**Green.** Add `Create`, `Update`, `Patch`, `Delete` to `kube.Resources`; handlers wrap them with a helper `audited(ctx, ev, func() error) error` (in `internal/httpapi/audited.go`) used by every later mutating handler.

**Refactor.** All mutation handlers share `mutate(w, r, verb, fn)`. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** Parse YAML documents with `k8s.io/apimachinery/pkg/util/yaml.NewYAMLOrJSONDecoder`; skip empty documents. Never log request bodies.

### Step 4.2 — Kind registry for all core kinds and the sidebar

**Goal.** Declarative descriptors (columns, status) for every core kind, and the resource sidebar built from discovery and permissions.

**Files.** Create one file per kind group under `web/src/kinds/core/` (`workloads.ts`, `config.ts`, `network.ts`, `storage.ts`, `cluster.ts`, `access.ts`) each with a test, `web/src/lib/quantity.ts`(+test), `web/src/features/shell/Sidebar.tsx`(+test), fixtures in `web/src/test/fixtures/*.json`.

Columns per kind (besides Name, Namespace where namespaced, and Age, which every kind has):

| Section | Kind (group) | Columns |
| --- | --- | --- |
| Workloads | Pod (core) | Step 3.8 |
| | Deployment (apps) | Ready `ready/desired`, Up-to-date, Available, Status (`Progressing`, `Available`, `Paused`, `Degraded` when `Available=False`) |
| | StatefulSet (apps) | Ready, Status |
| | DaemonSet (apps) | Desired, Current, Ready, Up-to-date, Available, Node selector |
| | ReplicaSet (apps) | Desired, Current, Ready, Owner |
| | Job (batch) | Completions `succeeded/completions`, Duration, Status (`Complete`, `Failed`, `Running`, `Suspended`) |
| | CronJob (batch) | Schedule, Suspend, Active, Last schedule |
| Config | ConfigMap (core) | Keys (count) |
| | Secret (core) | Type, Keys (count) |
| | ResourceQuota (core) | Used/hard summary |
| | LimitRange (core) | — |
| | HorizontalPodAutoscaler (autoscaling) | Reference, Min, Max, Replicas, Status |
| | PodDisruptionBudget (policy) | Min available, Max unavailable, Allowed disruptions |
| | PriorityClass (scheduling.k8s.io) | Value, Global default |
| Network | Service (core) | Type, Cluster IP, External IP, Ports |
| | Endpoints (core) | Endpoints; page note "Endpoints is deprecated since Kubernetes 1.33 in favour of EndpointSlices." |
| | EndpointSlice (discovery.k8s.io) | Address type, Ports, Endpoints, Service (label `kubernetes.io/service-name`) |
| | Ingress (networking.k8s.io) | Class, Hosts, Address, Ports |
| | IngressClass (networking.k8s.io) | Controller, Default |
| | NetworkPolicy (networking.k8s.io) | Pod selector, Policy types |
| Storage | PersistentVolumeClaim (core) | Status, Volume, Capacity, Access modes, Storage class |
| | PersistentVolume (core) | Capacity, Access modes, Reclaim policy, Status, Claim, Storage class |
| | StorageClass (storage.k8s.io) | Provisioner, Reclaim policy, Binding mode, Expansion |
| | VolumeSnapshot (snapshot.storage.k8s.io, when served) | Ready, Source PVC, Restore size, Snapshot class |
| Cluster | Node (core) | Status (`Ready`, `NotReady`, `SchedulingDisabled`), Roles, Version, Internal IP, CPU (allocatable), Memory (allocatable), Pressure (list of true pressure conditions) |
| | Namespace (core) | Status |
| | Event (events.k8s.io) | Type, Reason, Object, Message, Count, Last seen |
| | Lease (coordination.k8s.io) | Holder, Renew time |
| | MutatingWebhookConfiguration, ValidatingWebhookConfiguration (admissionregistration.k8s.io) | Webhooks (count) |
| Access control | ServiceAccount (core) | Secrets (count) |
| | Role, ClusterRole (rbac.authorization.k8s.io) | Rules (count) |
| | RoleBinding, ClusterRoleBinding (rbac.authorization.k8s.io) | Role, Subjects |

**Red.** For each descriptor: a table-driven test over two or three fixture objects asserting every column's value and the status. `quantity.ts`: parse Kubernetes quantities (`100m`, `1`, `512Mi`, `2Gi`, `1e3`) to numbers and format bytes/cores. `Sidebar`: sections in the spec's order (Overview, Workloads, Config, Network, Storage, Access Control, Helm, FluxCD, Resource Map, Pod Files, modules, Custom Resources, Events); a kind is hidden when discovery does not serve it; a kind is hidden when the access check says the user cannot `list` it (after the SSAR-backed answer arrives; while loading it is shown); an empty section is hidden; the current item has `aria-current="page"`.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green.** Implement descriptors as data plus small pure functions. Register them in `registry.ts`.

**Refactor.** Shared column builders (`ageColumn`, `conditionStatus(type)`, `countColumn(path)`) in `web/src/kinds/columns.ts` with tests. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** Column functions must tolerate missing fields (objects mid-creation): add one fixture with only `metadata` per kind.

### Step 4.3 — Details drawer

**Goal.** The right-hand drawer with tabs Overview, YAML (read-only here), Events, Logs (pods and workloads), Files (pods; placeholder until 4.10), Related.

**Files.** Create `web/src/features/details/{DetailsDrawer,OverviewTab,EventsTab,RelatedTab,Conditions,OwnerChain,ContainerList}.tsx` with tests, `web/src/lib/owners.ts`(+test), `web/src/lib/related.ts`(+test).

**Red.** Cases:
- Opening a row sets the URL to the deep link `/c/<cluster>/ns/<ns>/<group>/<kind>/<name>` (cluster-scoped: no `ns` part; empty group → `core`); loading that URL directly opens the drawer; closing returns to the list URL; `Escape` closes; focus moves into the drawer and returns to the row on close.
- Overview: labels, annotations (values truncated at 200 characters with "show more"), conditions table, kind-specific sections: pods → containers and init containers (image, state, restarts, ports, last termination reason), workloads → strategy, replicas, selector; nodes → capacity, allocatable, taints, pressure conditions; services → ports and matched pods (pods whose labels match the selector, from the live pod list); ingresses → rules linking to Services; PVCs → bound volume and the pods that mount the claim.
- Events tab: live events filtered by `regarding`/`involvedObject` UID, newest first.
- Owner chain: follows `ownerReferences` upward (Pod → ReplicaSet → Deployment) with links; stops after 5 levels or on a missing owner.
- Related: owners, children (objects whose `ownerReferences` point at this UID, from live lists of the common child kinds), Service ↔ pods, Ingress → Services.
- Object deleted while open → banner "This object was deleted." and actions disabled.
- All cluster text is rendered as text; URLs in annotations become `SafeLink`s.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement; move selector matching to `web/src/lib/selectors.ts` (`matchLabels`, `matchExpressions`) with tests. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** The drawer subscribes to a single-object watch by listing with `fieldSelector=metadata.name=<name>` through the same live store (key includes the selector) so it stays live even when the list view is filtered.

### Step 4.4 — YAML editor with dry run, diff and conflict handling

**Goal.** The spec's "Real-time manifest editing".

**Files.** Backend: `internal/httpapi/openapi.go`(+test) — `GET /api/v1/clusters/{cluster}/openapi/{group}/{version}` proxies the cluster's `/openapi/v3/apis/<group>/<version>` (or `/openapi/v3/api/v1` for `core`) through the user's REST client, returns JSON, cached 5 minutes per session+cluster. Web: `web/src/adapters/monaco/YamlEditor.tsx` and `DiffViewer.tsx` (excluded from coverage; no logic beyond wiring props), `web/src/features/editor/{EditorTab,useEditorState,SavePreview,ConflictBanner,CreateFromYaml,kubectlCommand}.ts(x)` with tests, `web/src/lib/yaml.ts`(+test), `web/src/lib/diff.ts`(+test). `npm install --save-exact monaco-editor@0.57.0 @monaco-editor/react@4.7.0 monaco-yaml@5.5.1 yaml@2.9.1`.

**Red.** Cases:
- `useEditorState`: loads the object (redacted) as YAML with `status` and `metadata.managedFields` omitted from the text but `metadata.resourceVersion` kept; tracks dirty state.
- Save flow: **Preview** calls `PUT …?dryRun=true`; shows a line diff of `live` vs `result` (`lib/diff.ts`, a plain LCS line diff with tests); admission errors are shown as text; `dry_run_unsupported` → message "A preview isn't available for this object." and a "Save without preview" button; **Save** calls `PUT` and on success reloads; 409 → conflict banner.
- While open, the object stays watched: when its `resourceVersion` changes and the editor is dirty → banner with "Reload" (discard mine) and "Diff against mine" (opens the diff viewer live vs. editor text); when not dirty → the text updates silently.
- Secrets: values show as `********`; the diff of a Secret shows masks only; a "Reveal" control per key (Step 4.6) replaces the mask with the value so it can be edited.
- Invalid YAML → inline error, Save disabled. Changing `metadata.name`/`namespace`/`kind` → inline error explaining that these can't be changed.
- Create from YAML: multi-document input → `POST /apply` with `dryRun` first, then for real; per-document results listed.
- `kubectlCommand(action)`: returns the equivalent command for each action type, e.g. `kubectl replace -f -` with the namespace flag, `kubectl delete pod web-1 -n default`, `kubectl scale deployment/web --replicas=3 -n default`; arguments are shell-quoted (test names with spaces and quotes).
- Schema: the editor component receives `schema` (fetched from the openapi endpoint for the object's group/version) and `schemaRef` for the kind; unit test only that the hook requests the right URL and passes the result on.

Command: `make test-backend PKG=./internal/httpapi/...` and `make test-frontend`. Expected failure: unresolved imports / undefined handler.

**Green.** Implement. In tests, `vi.mock('../../adapters/monaco/YamlEditor', …)` with a `<textarea>` stand-in that calls `onChange`.

**Refactor.** Move the save state machine (`idle → previewing → previewed → saving → saved|conflict|error`) into a reducer with exhaustive tests. Re-run.

**Acceptance.** Tests pass; `make build` → `BUILD OK` (Monaco bundles; check `web/dist` exists and `ls web/dist/assets | wc -l` is greater than 3).

**Pitfalls.**
- Configure Monaco workers through Vite `?worker` imports inside the adapter only; the CSP allows `worker-src 'self' blob:`.
- The adapter files stay under 80 lines each and contain no branching logic; anything testable belongs outside `adapters/`.
- The update is a full `PUT` (spec). Do not implement patch-on-save.

### Step 4.5 — Workload and storage actions

**Goal.** Scale, rollout restart, pause/resume, rollback, set image, CronJob trigger/suspend, evict, PVC expand.

**Files.** Create `internal/actions/actions.go`, `internal/actions/workloads.go`, `internal/actions/*_test.go`, `internal/httpapi/actions.go`(+test); web `web/src/features/actions/{ActionMenu,ConfirmDialog,ScaleDialog,SetImageDialog,RollbackDialog,useAction}.tsx` with tests, `web/src/kinds/actions.ts`(+test).

API: `POST /api/v1/clusters/{cluster}/actions` `{action, target:{group, version, resource, namespace, name}, params:{…}}` → `{ok:true}` or an error. Every action is audited with the verb in the table.

| `action` | Kinds | Kubernetes call | Audit verb |
| --- | --- | --- | --- |
| `scale` `{replicas}` | Deployment, StatefulSet, ReplicaSet | merge-patch `{"spec":{"replicas":N}}` on the `scale` subresource | `scale` |
| `restart` | Deployment, StatefulSet, DaemonSet | strategic-merge-patch setting `spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"]` to now (RFC 3339, from the clock) | `restart` |
| `pause` / `resume` | Deployment | merge-patch `spec.paused` true/false | `patch` |
| `rollback` `{revision}` | Deployment | find the owned ReplicaSet with annotation `deployment.kubernetes.io/revision == revision`; patch the Deployment's `spec.template` to that ReplicaSet's template without the `pod-template-hash` label | `rollback` |
| `setImage` `{container, image, init}` | Deployment, StatefulSet, DaemonSet | strategic-merge-patch `{"spec":{"template":{"spec":{"containers"|"initContainers":[{"name":C,"image":I}]}}}}` | `patch` |
| `cronjobTrigger` | CronJob | create a Job named `<cronjob>-manual-<unix seconds>` from `spec.jobTemplate`, owner reference to the CronJob, annotation `cronjob.kubernetes.io/instantiate: manual` | `create` |
| `cronjobSuspend` / `cronjobResume` | CronJob | merge-patch `spec.suspend` | `patch` |
| `evict` | Pod | `policy/v1` Eviction | `evict` |
| `expandPVC` `{storage}` | PersistentVolumeClaim | merge-patch `spec.resources.requests.storage`; refused with 422 when the StorageClass has no `allowVolumeExpansion: true` or the new size is not larger | `patch` |

**Red.** One test per row asserting the exact patch type and body (capture the fake client's actions), the audit records (requested + success), and kind validation (action on the wrong kind → 400). Extra cases: rollback to a missing revision → 404; `setImage` with an unknown container → 400; eviction blocked by a PodDisruptionBudget (API 429) → 409 with the message "Cannot evict: a PodDisruptionBudget would be violated."; `replicas` negative → 400; image string empty or containing whitespace → 400. Web: the action menu shows only actions listed for the kind; each is disabled with a tooltip "missing permission: <verb> <resource>" when the access check denies it; destructive actions (delete, evict, rollback, restart) require confirmation naming the object; dialogs validate input; errors are shown as text; "Copy as kubectl" is offered in every dialog.

Command: `make test-backend PKG="./internal/actions/... ./internal/httpapi/..."` and `make test-frontend`. Expected failure: `undefined: actions.New`.

**Green.**

```go
type Request struct{ Action string; Target Target; Params json.RawMessage }
type Service struct{…}                      // deps: kube.Clients, clock, audit
func (s *Service) Do(ctx context.Context, sess session.Session, clusterID string, r Request) error
// internal registry: map[action]handler{ kinds []schema.GroupKind; verb string; run func(...) error }
```

**Refactor.** Table-driven registration so later steps (nodes, Flux, modules) add rows. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** Use the typed client for built-in kinds where it gives a subresource helper (`UpdateScale` is not used: send the patch to subresource `scale` via the dynamic client so it works for all three kinds). The access-check mapping per action lives in `web/src/kinds/actions.ts` (for example `scale` → `patch` on `deployments/scale`).

### Step 4.6 — Secret reveal and certificate details

**Goal.** The only path by which Secret values leave the server, audited; certificate metadata without private keys.

**Files.** Create `internal/certs/certs.go`(+test), `internal/httpapi/secrets.go`(+test); web `web/src/features/secrets/{SecretData,RevealButton,CertificateDetails}.tsx` with tests.

API: `POST /api/v1/clusters/{cluster}/secrets/reveal` `{namespace, name, keys?:[…]}` → `{data:{key: "<base64>"}}`. `POST /api/v1/clusters/{cluster}/secrets/certificates` `{namespace, name}` → `{certificates:[{key, index, subject, issuer, serial, notBefore, notAfter, dnsNames, isCA, expiresInSeconds}]}`.

**Red.** Cases:
- Reveal: audit `secret-reveal` (namespace, name, `details.keys`) is written **before** the Secret is read; audit failure → no read, 500; missing key → 404; Helm release Secret → 404; response has `Cache-Control: no-store`; values never logged.
- Certificates: `certs.Parse(data map[string][]byte)` finds every PEM `CERTIFICATE` block in every key (chains included, in order), returns metadata only; `PRIVATE KEY` blocks are ignored and no part of them appears in the output (assert); non-PEM keys ignored; malformed certificate → entry with `error` text; `expiresInSeconds` from the clock (negative when expired); audit verb `certificate-view`.
- Web: values hidden until "Reveal" per key or "Reveal all"; revealed values shown as text with a copy button, and hidden again when the drawer closes; a Secret that has PEM certificates shows a certificate table with a warning style under 30 days to expiry; binary values (non-UTF-8 after base64 decoding) shown as "binary, N bytes" with a download button.

Command: `make test-backend PKG="./internal/certs/... ./internal/httpapi/..."` and `make test-frontend`. Expected failure: `undefined: certs.Parse`.

**Green / Refactor.** Implement with `encoding/pem` and `crypto/x509`. Generate test certificates in the test with `x509.CreateCertificate` and a fixed serial and validity. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** Reveal reads the Secret with the user's own client; a user without `get` on Secrets gets the API's 403.

### Step 4.7 — Shells

**Goal.** Browser terminal over the `exec` subresource: bash-then-sh, resize, idle timeout, audit of start and stop only.

**Files.** Create `internal/execs/executor.go`, `internal/execs/channel.go`, `internal/execs/*_test.go`, `internal/testutil/fakekube/execserver.go`; web `web/src/adapters/xterm/Terminal.tsx` (excluded from coverage), `web/src/features/shell/{ShellTab,useShell,ContainerPicker}.tsx` with tests. `npm install --save-exact @xterm/xterm@6.0.0 @xterm/addon-fit@0.11.0`.

Channel: `exec.open` `d = {cluster, namespace, pod, container, cols, rows}`; ops `exec.stdin {data:<base64>}`, `exec.resize {cols, rows}`, `exec.close`; frames `opened {shell}`, `out {data:<base64>}`, `exit {code, message?}`, `closed {reason}`.

**Red.** Cases with a fake `Executor` (in-memory pipes):
- Tries `["/bin/bash"]`; when the executor reports that the command could not start (error text contains `executable file not found`, `no such file or directory`, or exit code 126/127 before any stdin was sent) → retries `["/bin/sh"]`; both fail → `error` frame "This container has no shell (/bin/bash or /bin/sh)."; `opened.shell` names the shell that started.
- stdin bytes reach the process; stdout arrives as `out` frames (chunks ≤ 32 KiB); `resize` reaches the size queue; process exit → `exit {code}` then `closed`.
- Idle: no stdin and no stdout for `shell.idleTimeout` on the fake clock → the stream is cancelled, `closed {reason:"idle"}`.
- Audit: `exec-start` (namespace, pod, container, `details.shell`) before starting, refused if it cannot be written; `exec-stop` with `details.exitCode` and `details.durationSeconds` at the end. No stdin/stdout bytes in any audit or log output (assert by writing a marker string through the shell and searching captured output).
- Session end / connection close cancels the stream. While open, `KeepAlive` is called every minute (A12).
- With the memory guard (Step 6.3) reporting busy → `error` code `server_busy` (wire the dependency now as an interface with a default that never refuses).
- `KubeExecutor` against `fakekube.ExecServer` (an `httptest` TLS server that upgrades `/api/v1/namespaces/{ns}/pods/{pod}/exec` with subprotocol `v5.channel.k8s.io`, echoes channel-0 bytes to channel 1, records resize messages from channel 4, and finishes with a `Success` status on channel 3): the request URL has `container`, `command` (repeated), `stdin=true`, `stdout=true`, `tty=true`; echo works; cancelling the context ends the stream.

Web cases: `useShell` opens the channel, forwards terminal input as base64, writes output to the terminal handle, sends resize on size change, shows "The server connection was lost and the shell session ended." with a "Start a new shell" button when the socket drops or `closed` arrives (A26), container picker lists running containers including running init containers and ephemeral containers.

Command: `make test-backend PKG=./internal/execs/...` and `make test-frontend`. Expected failure: `undefined: execs.NewFactory`.

**Green.**

```go
type ExecRequest struct{ Namespace, Pod, Container string; Command []string; TTY, Stdin bool }
type StreamIO struct{ Stdin io.Reader; Stdout, Stderr io.Writer; Resize <-chan Size }
type Executor interface {
	Stream(ctx context.Context, sess session.Session, clusterID string, r ExecRequest, sio StreamIO) error
}
func NewKubeExecutor(c kube.Clients) Executor
```

`KubeExecutor`: build the URL with `Typed().CoreV1().RESTClient().Post().Resource("pods").Namespace(ns).Name(pod).SubResource("exec").VersionedParams(&corev1.PodExecOptions{…}, scheme.ParameterCodec).URL()`; `remotecommand.NewWebSocketExecutor(cfg, "GET", url.String())`; `StreamWithContext(ctx, remotecommand.StreamOptions{Stdin, Stdout, Stderr, Tty, TerminalSizeQueue})`. The size queue adapts the `Resize` channel to `remotecommand.TerminalSizeQueue` (`Next() *remotecommand.TerminalSize`, returning nil when the channel closes).

**Refactor.** Extract the bash/sh fallback into `startShell(ctx, exec, candidates)` reused by Step 4.8. Re-run.

**Acceptance.** Tests `ok` (`-count=5` for `internal/execs`).

**Pitfalls.**
- WebSocket exec only; do not add the SPDY fallback (supported clusters are 1.35+).
- xterm's `write` accepts `Uint8Array`: decode base64 to bytes, do not convert to a string first (multi-byte characters split across frames).
- The adapter `Terminal.tsx` exposes `{ write(bytes), onData(cb), onResize(cb), fit() }` and nothing else.

### Step 4.8 — Debug toolkit

**Goal.** Ephemeral debug containers with profiles, reuse of a running debug container, and the optional node shell.

**Files.** Create `internal/execs/debug.go`, `internal/execs/nodeshell.go`, tests; `internal/httpapi/debug.go`(+test); web `web/src/features/debug/{DebugDialog,NodeShellButton}.tsx` with tests.

API: `POST /api/v1/clusters/{cluster}/debug/pod` `{namespace, pod, target, profile?}` → `{container, reused:bool}` (the UI then opens a shell in that container). `POST /api/v1/clusters/{cluster}/debug/node` `{node}` → `{namespace, pod, container}`; `DELETE /api/v1/clusters/{cluster}/debug/node/{pod}` removes it.

Profiles (the names of `kubectl debug`; this table is the definition):

| Profile | Security context of the debug container |
| --- | --- |
| `general` (default) | none set |
| `baseline` | none set |
| `restricted` | `runAsNonRoot: true`, `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`, `seccompProfile.type: RuntimeDefault` |
| `netadmin` | `capabilities.add: [NET_ADMIN, NET_RAW]` |
| `sysadmin` | `privileged: true` |

**Red.** Cases:
- Adds an ephemeral container through `Typed().CoreV1().Pods(ns).UpdateEphemeralContainers` with name `kubelook-debug-<5 lowercase chars from injected rand>`, image `debug.image`, `stdin: true`, `tty: true`, `targetContainerName: target`, `command: [debug.shell]`… and the profile's security context; a named entry of `debug.profiles` selects the profile and whether `targetContainerName` is set (`shareProcessNamespace: false` → no target).
- Reuse: a pod that already has an ephemeral container named `kubelook-debug-*` whose status is `running` → no update call, `reused:true`.
- An existing debug container that is waiting with `ErrImagePull`/`ImagePullBackOff` is not reused; a new one is added (the old one stays, as Kubernetes does not allow removal).
- Audit `debug-container` (pod, container, image, profile).
- Waits for the container to run by watching the pod (no polling); 60 s (fake clock) without `running` → 504-class error "The debug container did not start: <waiting reason>".
- Node shell: disabled (`nodeShell.enabled=false`) → 404. Enabled: creates a pod `kubelook-node-shell-<5 chars>` in namespace `default` with `nodeName`, `hostPID`, `hostNetwork`, `hostIPC`, privileged container from `nodeShell.image` running `sleep infinity`, tolerations for all taints, `restartPolicy: Never`, label `app.kubernetes.io/managed-by=kubelook`; audited; the shell command for it is `nsenter -t 1 -m -u -i -n -p -- /bin/sh`; DELETE removes the pod; closing the shell tab calls DELETE.
- Web: the dialog states before adding: "A debug container stays in the pod until the pod is deleted. Kubernetes doesn't allow removing it." ; profile picker; target container picker for multi-container pods; the action is hidden without `patch` on `pods/ephemeralcontainers` and the shell without `create` on `pods/exec`.

Command: `make test-backend PKG="./internal/execs/... ./internal/httpapi/..."` and `make test-frontend`. Expected failure: `undefined: execs.NewDebugger`.

**Green / Refactor.** Implement; share "wait for container running via watch" as `waitContainerRunning(ctx, typed, ns, pod, container, clock, timeout)`. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** The fake clientset does not implement the `ephemeralcontainers` subresource specially: add a reactor in the test for `update` on `pods/ephemeralcontainers`.

### Step 4.9 — Pod file browser (backend)

**Goal.** List, read, save, upload, download and delete files in a container through `exec`, with argument-vector commands, tar validation, size limits and audit.

**Files.** Create `internal/files/{commands,list,tar,transfer,channel}.go` with tests.

Channel: `file.open` `d = {cluster, namespace, pod, container}`. Every later op carries a request ID `rid` echoed in replies.

| Op | Command(s) run in the container (argument vectors, never a shell line) | Reply frames |
| --- | --- | --- |
| `file.list {path}` | `["ls","-lA","--",path]` | `list {entries:[{name, type:"file"|"dir"|"link"|"other", mode, size, modified, target?}], skipped}` |
| `file.read {path}` | `["ls","-ld","--",path]` then `["head","-c","1048577","--",path]` | `content {data:<base64>, binary, truncated, stat:{size, modified}}` |
| `file.save {path, data, expect:{size, modified}}` | `["ls","-ld","--",path]` (compare with `expect`) then `["tee","--",path]` with stdin | `saved {stat}` or `error {code:"conflict"}` |
| `file.delete {path, recursive}` | `["rm","-f","--",path]` or `["rm","-rf","--",path]` | `deleted {}` |
| `file.download {path}` | file: `["cat","--",path]`; directory: `["tar","cf","-","-C",dir,"--",base]` | `chunk {data, seq}` … `done {bytes, tar:bool}` |
| `file.upload.begin {dir, name, tar:bool}`, `file.upload.chunk {data, seq}`, `file.upload.end {}` | tar available: `["tar","xf","-","-C",dir]`; otherwise single files only: `["tee","--",dir+"/"+name]` | `uploaded {bytes}` |

**Red.** Cases (fake `execs.Executor` scripted per command):
- Paths: must be absolute; cleaned with `path.Clean`; containing NUL → 400; `rm` on `/` refused; the path is always a separate argument after `--` (test with names `-rf`, `; rm -rf /`, `$(id)`, spaces and newlines: the argument vector contains the name verbatim).
- `ls` parsing: regular file, directory, symlink with ` -> target`, device, names with spaces, both date forms (`Jan  2 15:04`, `Jan  2  2006`); unparseable lines are counted in `skipped`; the `total N` line is ignored; BusyBox and GNU sample outputs from `testdata/`.
- Binary detection: a NUL byte in the first 8,000 bytes → `binary:true` and no content; over 1 MiB → `truncated:true`.
- Save conflict: `ls -ld` differs from `expect` → `conflict`, `tee` not run. `tee` writes stdin; its stdout is discarded.
- tar probing: `["tar","--version"]` exit 0 → tar available (cached per channel); otherwise directories cannot be downloaded/uploaded → error "This container has no tar; only single files can be transferred."
- Download validation: the tar stream is parsed with `archive/tar` **as it streams** and forwarded unchanged; an entry with an absolute name, a name containing `..` as a path element, or a symlink/hardlink whose target is absolute or escapes the requested directory → the transfer is aborted with `error {code:"unsafe_archive"}`; nothing is ever written to the server's disk (assert no file-system calls: the package imports neither `os.Create` nor `os.WriteFile`; enforce with a `depguard`/`forbidigo` rule for `os\.(Create|WriteFile|MkdirAll|OpenFile)` scoped to `internal/files/`).
- Limit: more than `fileBrowser.maxTransferBytes` streamed (upload or download) → abort with `too_large`, counting actual bytes, not the reported size.
- Upload builds a tar on the fly from chunks for a single file when tar is available (header from name and declared size), or forwards a client-built tar when `tar:true` after validating its entries the same way.
- Audit: `file-view`, `file-save`, `file-upload`, `file-download`, `file-delete` with `details.path` and `details.size`; never content; refused if the record cannot be written.
- `fileBrowser.enabled=false` → `file.open` answers `error` code `not_found`.
- Missing utilities (`ls` not found) → "This container has no shell utilities; the file browser can't work here."

Command: `make test-backend PKG=./internal/files/...`. Expected failure: `undefined: files.NewFactory`.

**Green.** Implement; non-TTY exec with separate stdout/stderr buffers (stderr capped at 4 KiB and returned as the error message text).

**Refactor.** `run(ctx, argv, stdin) (stdout, stderr, code, err)` helper; one `validateTarHeader(h, root)` function with its own table test. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** `--` is supported by GNU coreutils and BusyBox applets used here. Chunks are 48 KiB before base64 so a frame stays under the 1 MiB read limit.

### Step 4.10 — Pod file browser (web)

**Goal.** The Files tab and the Pod Files page.

**Files.** Create `web/src/features/files/{FilesPanel,useFiles,Breadcrumbs,FileList,FileEditor,UploadDrop,PodFilesPage,tarWriter}.ts(x)` with tests.

**Red.** Cases: breadcrumbs navigate; filter box filters names; list shows type icon with label, mode, size (human), modified, symlink target; opening a text file loads it into the editor adapter with language from the extension; Save sends `expect` from the opened stat and shows "The file changed in the container after you opened it." on conflict; binary files show "Binary file" with Download only; truncated files are read-only with a notice; upload by button and by drag-and-drop, multiple files and directories (directories are packed by `tarWriter.ts`, a minimal ustar writer with tests for header checksum, long names > 100 bytes rejected with a clear error, and padding); progress display; download saves through the injected `saveFile`; delete asks for confirmation naming the path; errors as text; the page has namespace, pod and container pickers (container picker includes running init containers only); panel hidden when `features.fileBrowser` is false or the user lacks `create` on `pods/exec`.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement; share chunking/base64 helpers in `web/src/lib/bytes.ts` with tests. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** Use `Uint8Array` end to end; build base64 in chunks (no `String.fromCharCode(...bigArray)` spread, which overflows the stack).

> **Pull request 6 starts here.** Pull request 5 (Steps 4.1–4.10) must be merged first.

### Step 4.11 — Durable jobs framework

**Goal.** Long actions recorded before they start, run under a renewed lease, and classified by another replica if their replica dies (spec "Long actions are durable jobs").

**Files.** Create `internal/store/migrations/0005_jobs.sql`, `internal/store/jobs.go`(+test), `internal/jobs/{runner,recovery}.go` with tests, `internal/httpapi/jobs.go`(+test); web `web/src/features/jobs/{JobsPanel,useJobs,JobRow}.tsx` with tests.

```sql
CREATE TABLE jobs (
  id TEXT PRIMARY KEY, user_id TEXT NOT NULL, session_hash TEXT NOT NULL, cluster TEXT NOT NULL,
  type TEXT NOT NULL, target TEXT NOT NULL, sealed_params TEXT NOT NULL,
  status TEXT NOT NULL, progress TEXT NOT NULL, message TEXT NOT NULL, offers TEXT NOT NULL,
  lease_owner TEXT NOT NULL, lease_expires_at BIGINT NOT NULL,
  created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL, finished_at BIGINT NOT NULL DEFAULT 0
);
-- statement-break
CREATE INDEX jobs_user ON jobs(user_id, created_at);
-- statement-break
CREATE INDEX jobs_lease ON jobs(status, lease_expires_at);
```

Statuses: `queued`, `running`, `succeeded`, `failed`, `interrupted`. `offers` is a JSON list of `retry`, `rollback`, `uninstall`. `sealed_params` uses purpose `job`, binding = job ID.

API: `GET /api/v1/jobs?active=true|false` (the user's jobs, newest first, max 100), `GET /api/v1/jobs/{id}`, `POST /api/v1/jobs/{id}/retry` → new job ID.

**Red.** Cases (real store, fake clock, fake handlers):
- `Submit` inserts the row (`queued`, lease owner = this replica's ID, lease 30 s) **before** anything runs, then enqueues; with the per-type limit (default 5 for Helm types, 2 for others) reached, extra jobs stay `queued` and start as others finish, in submission order.
- A running job's lease is renewed every 10 s on the fake clock; `report(progress)` persists progress.
- Renewal fails (row deleted, or store error) → the job's context is cancelled and the handler stops; status is not overwritten by this replica.
- Handler returns nil → `succeeded`; error → `failed` with message; panics are recovered → `failed`.
- Recovery scan every 15 s: a `running` row whose lease expired and whose owner is another replica → calls the handler's `Recover(ctx, job)` and stores the returned status/message/offers; a `queued` row with an expired lease → `interrupted`, message "The server stopped before this job started."; if the job's session no longer exists → `interrupted`, message "The job's session ended, so its result could not be checked."; two replicas scanning at once classify a job only once (compare-and-swap on `lease_expires_at`).
- Interrupted jobs are never re-run automatically (assert handler `Run` call count).
- `Retry` creates a **new** job with the same type/target/params under the caller's current session; only the owner user may read or retry a job (other users → 404).
- Shutdown (`Runner.Close`) cancels running jobs; they stay `running` until recovery classifies them.
- Web: jobs panel in the dock lists jobs with status, progress text, message; polls every 3 s only while any job is active (fake timers); `interrupted` rows show the offered buttons; visible in any tab.

Command: `make test-backend PKG="./internal/store/... ./internal/jobs/... ./internal/httpapi/..."` and `make test-frontend`. Expected failure: `undefined: jobs.NewRunner`.

**Green.**

```go
type Job struct{ ID, UserID, SessionHash, Cluster, Type string; Target, Params json.RawMessage; Status Status; … }
type Progress struct{ Done, Total int; Text string; Items []ItemResult }
type Handler interface {
	Type() string
	Run(ctx context.Context, j *Job, report func(Progress)) error
	Recover(ctx context.Context, j *Job) (Status, string, []string)   // status, message, offers
}
type Runner struct{…}
func NewRunner(d Deps) *Runner                         // deps: store.Jobs, clock, sealer, log, replicaID, limits, session lookup
func (r *Runner) Register(h Handler)
func (r *Runner) Submit(ctx context.Context, sess session.Session, cluster, typ string, target, params any) (string, error)
func (r *Runner) Run(ctx context.Context)              // workers + lease renewal + recovery scan
```

**Refactor.** Lease logic in `lease.go` (claim, renew, steal) with focused tests. Re-run.

**Acceptance.** Tests `ok` (`-count=5` for `internal/jobs`).

**Pitfalls.** All state transitions are single `UPDATE … WHERE id=? AND status=? AND lease_owner=?` statements checked by rows-affected; never read-then-write.

### Step 4.12 — Node actions, drain and bulk actions

**Goal.** Cordon, uncordon, taints; drain as a durable job using the eviction API; bulk delete/restart/label and bulk node actions.

**Files.** Create `internal/actions/nodes.go`, `internal/actions/drain.go`, `internal/actions/bulk.go`, tests; web `web/src/features/actions/{BulkBar,BulkConfirm,TaintDialog}.tsx` with tests.

API: actions `cordon`, `uncordon`, `taintAdd {key,value,effect}`, `taintRemove {key,effect}` via `/actions`. `POST /api/v1/clusters/{cluster}/bulk` `{action:"delete"|"restart"|"label"|"cordon"|"uncordon"|"drain", targets:[Target…], params}` → `{jobId}`.

**Red.** Cases:
- `cordon`/`uncordon`: merge-patch `spec.unschedulable`; audit verbs `cordon`/`uncordon`. Taints: read-modify-update of `spec.taints` with `resourceVersion`, retried on conflict up to 3 times; duplicates rejected; audit `taint`.
- Drain of one node (`drain.go`, own implementation over the typed client and the clock): cordon first; list pods with `fieldSelector=spec.nodeName=<node>`; skip DaemonSet-owned pods, mirror pods (annotation `kubernetes.io/config.mirror`) and pods in phase `Succeeded`/`Failed`; evict each remaining pod with `policy/v1` Eviction; a 429 answer (PodDisruptionBudget) is retried after 5 s on the fake clock, indefinitely until the job's context ends; wait for each evicted pod to disappear through a pod **watch** (not polling); progress reports `evicted/total`; pods with `emptyDir` volumes are evicted too (same as `kubectl drain --delete-emptydir-data`), stated in the confirmation text.
- Drain `Recover`: node still has evictable pods → `interrupted`, message "Drain stopped part-way: N pods remain on the node.", offers `retry`; none remain → `succeeded`.
- Bulk job: handles items one at a time in the given order; each item is audited individually; an item failure is recorded in `progress.items` and the job continues; final status `failed` if any item failed, with a summary; bulk drain processes one node at a time (spec); `label` params `{add:{k:v}, remove:[k]}` as a merge patch; max 500 targets.
- Bulk `Recover`: `interrupted` listing which items were done (from persisted progress), offers `retry` (the retry skips nothing: it is a new job with the remaining items, built by the UI).
- Web: selecting rows shows the bulk bar with the actions valid for the kind; the confirmation dialog **lists every object by name** and requires an explicit confirm; for drain it shows the emptyDir note; progress shown from the job.

Command: `make test-backend PKG="./internal/actions/... ./internal/httpapi/..."` and `make test-frontend`. Expected failure: `undefined: actions.NewDrainHandler`.

**Green / Refactor.** Implement; `podFilter(pod) (evict bool, reason string)` as a pure function with a table test. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** The fake clientset has no eviction logic: add a reactor on `create` `pods/eviction` that deletes the pod (and one that returns `apierrors.NewTooManyRequests` for the PDB case). Do not import `k8s.io/kubectl`.

### Step 4.13 — Logs: structured parsing, merged sources, history, server filter, export

**Goal.** The rest of the spec's Logs section.

**Files.** Modify `internal/logs/*` (multi-source, filter, history) with tests; create `internal/httpapi/logs.go`(+test); web `web/src/features/logs/{parse,tokens,levels,MultiSourcePicker,FieldFilter,exportLogs}.ts(x)` with tests; modify `LogViewer.tsx`.

**Red.** Backend cases:
- `logs.open` accepts `sources`: either one `{pod, container}`, or `{pod, allContainers:true}`, or `{selector:"app=web"}`, or `{workload:{group, kind, name}}` (resolved through the workload's `spec.selector`); at most 20 streams (more → error naming the limit); each line is tagged `{pod, container}`; the set of pods is fixed at open (A27).
- `filter:{text}` or `filter:{regex}` (RE2 via Go `regexp`; invalid → error; pattern length ≤ 256): only matching lines are sent; the dropped-by-filter count is not reported as `dropped`.
- `logs.history {source, before:<ts>, lines:<n>}`: runs a non-follow request with `tailLines = alreadyLoaded + n` and returns only lines older than `before`; total loaded per channel is capped at `logs.historyMaxLines` (reply `historyEnd` when the cap or the start of the log file is reached).
- `POST /api/v1/clusters/{cluster}/logs/downloads` `{namespace, pod, container, format:"text"|"ndjson", from, to, lines}` → 204 after writing audit `log-download` (the browser saves its buffer only after this succeeds).
Web cases:
- `parse.ts`: a line that is a JSON object → `{fields, level, message, time}` using keys `level|lvl|severity|loglevel`, `msg|message`, `time|ts|timestamp|@timestamp`; non-JSON → plain; level normalisation (`warning`→`WARN`, `err`→`ERROR`, numbers per pino/bunyan: 30→INFO, 40→WARN, 50→ERROR).
- `tokens.ts`: splits text into spans typed `timestamp`, `uuid`, `url`, `logger`, `text`; URLs become `SafeLink`s; everything else text with a class.
- Level colours with a text label; filter by level set and by `field = value`; plain lines always shown unless a field filter is active.
- Multi-source view: colour per source (stable hash of pod/container to a palette of 12), source label on each line, merged in arrival order.
- Scrolling to the top triggers `logs.history` once per step and prepends lines; shows "Beginning of the available log" at `historyEnd`.
- Export: text or NDJSON (`{"time","pod","container","message"}` per line) after the audit call succeeds; failure → nothing saved and an error shown.

Command: `make test-backend PKG="./internal/logs/... ./internal/httpapi/..."` and `make test-frontend`. Expected failure: assertion failures / unresolved imports.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** Tests pass.

**Pitfalls.** Kubernetes only serves the current log file: `historyEnd` is reached when a request returns fewer lines than asked. Never use the user's regex with a backtracking engine on the server; Go's `regexp` is linear-time.

### Step 4.14 — Field filter

**Goal.** Filter any list by field/operator/value rows or a query bar; save filter sets; download matches; copy as kubectl.

**Files.** Create `web/src/lib/filter/{parse,evaluate,paths,toJq,duration}.ts` with tests, `web/src/features/filter/{FilterBar,FilterRows,SavedFilters,DownloadMenu}.tsx` with tests.

Grammar of the query bar (A17): `expr := term (("AND"|"OR") term)*`, `AND` binds tighter than `OR`, no parentheses; `term := path op value?`; `path` is dotted with `[n]` or `[*]` for arrays (`spec.containers[*].image`); operators `=`, `!=`, `contains`, `regex`, `in (a, b, …)`, `exists`, `older than <duration|timestamp>`, `newer than <duration|timestamp>`; values are bare words or double-quoted strings.

**Red.** Cases:
- Parser: each operator; quoting; precedence (`a = 1 OR b = 2 AND c = 3` groups as `a=1 OR (b=2 AND c=3)`); errors carry a position and message; round trip rows → text → rows.
- Evaluator: comparisons on strings/numbers/booleans (numeric compare when both sides parse as numbers); `[*]` matches if any element matches; `exists` true for present non-null; `older than 7d` against a timestamp field using the injected `now`; `regex` invalid → the row is flagged, list unfiltered by it.
- `duration.ts`: `30s`, `15m`, `2h`, `7d`, `1w`; invalid → error.
- `paths.ts`: suggestions collected from the first 50 live objects, depth ≤ 6, arrays as `[*]`; for Secrets, paths under `data` and `stringData` are excluded and a filter on them is rejected with "Secret values can't be filtered."
- Saved filters: stored in user settings (`filterSets`), per kind; load, rename, delete.
- Download: the filtered objects as YAML (multi-document) or JSON (a `List`), from the already-redacted live store.
- `toJq.ts`: produces `kubectl get <resource> [-n ns | -A] -o json | jq '.items[] | select(<expr>)'` for every operator, with proper jq string escaping; and a `yq` variant.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Hand-written tokenizer and recursive-descent parser (no dependency). Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** Evaluate filters with `useMemo` keyed on the list version and the parsed filter, not per render.

### Step 4.15 — Export

**Goal.** Every list exports as CSV or JSON, without sensitive values, with spreadsheet-formula neutralisation.

**Files.** Create `web/src/lib/csv.ts`(+test), `web/src/features/export/ExportMenu.tsx`(+test).

**Red.** Cases: CSV per RFC 4180 (quotes doubled; fields with comma, quote or newline quoted; CRLF line ends; UTF-8 BOM); any cell whose first character is `=`, `+`, `-` or `@` (also after leading whitespace, tab or carriage return) is prefixed with a single quote `'`; CSV contains the visible columns of the current (filtered, sorted) list; JSON export contains the redacted objects; Secret values never appear (they are masked upstream; assert with a Secret fixture); file name `<cluster>-<kind>-<yyyyMMdd-HHmmss>.csv` from the injected clock.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** Tests pass.

**Pitfalls.** Neutralise after converting the value to a string and before quoting.

### Step 4.16 — User settings

**Goal.** Settings API and page: favourites, namespace filters, column presets, watch idle timeout, theme, saved credentials, new-token box.

**Files.** Create `internal/httpapi/settings.go`(+test); web `web/src/features/settings/{SettingsPage,WatchIdleSetting,ColumnPresets,NewTokenBox}.tsx` with tests, `web/src/api/settings.ts`(+test).

API: `GET /api/v1/settings` → `Settings`; `PUT /api/v1/settings` (whole document) → `Settings`.

**Red.** Cases: round trip; validation errors → 400 (`watchIdleSeconds` outside 30–300, more than 200 favourites, filter set name empty, document over 256 KiB); settings belong to the user, not the session (a second session of the same user sees them); the watch hub reads the new idle value on the next subscription. Web: favourites toggle from the cluster rail persists; idle timeout slider 30 s–5 min; column chooser per kind (order and visibility) used by `Table`; theme selector; "Saved credentials" section from Step 2.10; a "Paste a new Vault token" box that replaces the token **without** closing terminals, log streams or editors (assert no dock tab is closed and the socket is not reconnected); the page contains no admin or permission settings.

Command: `make test-backend PKG=./internal/httpapi/...` and `make test-frontend`. Expected failure: handler undefined.

**Green / Refactor.** Implement; optimistic updates with rollback on error in `api/settings.ts`. Re-run.

**Acceptance.** Tests pass.

**Pitfalls.** Replacing the Vault token keeps existing `cluster_cred` rows; they are re-minted with the new token at their next refresh.

### Step 4.17 — Navigation: command palette, namespace filter, deep links, dock pop-out, keyboard

**Goal.** The cross-cutting UI behaviour.

**Files.** Create `web/src/features/palette/{CommandPalette,usePaletteItems}.tsx`, `web/src/features/namespaces/NamespaceFilter.tsx`, `web/src/features/dock/popout.ts`, `web/src/lib/hotkeys.ts`, `web/src/features/shell/NoAccessPage.tsx`, tests for each.

**Red.** Cases:
- Palette (`Ctrl/Cmd+K`): lists views and resource kinds of the **open cluster only**; fuzzy match on title; Enter navigates; `Escape` closes; implemented as an ARIA combobox with listbox; no cross-cluster search exists.
- Namespace filter: multi-select per cluster, saved to settings (`namespaceFilters[clusterID]`); lists subscribe per selected namespace (one watch per namespace) or to all namespaces when none is selected; more than 10 selected → falls back to an all-namespaces watch filtered client-side.
- Deep links: every view's URL is stable and reload-safe; a link to a cluster not in the session shows the spec's message ("This cluster isn't available in your session. If it comes from a kubeconfig, upload that kubeconfig first."); a Vault cluster the user's token cannot reach shows "You don't have access to this cluster."
- Dock pop-out: opens `/dock` in a new window (`window.open` injected); state is shared through a `BroadcastChannel` named `kubelook-dock` (injected); closing the pop-out returns tabs to the main window.
- Hotkeys: `g` then `p` → Pods, `/` focuses the list filter, `?` opens the shortcut help; ignored while typing in inputs, the editor or a terminal.
- Accessibility: every interactive element reachable by Tab; visible focus style; status icons have text labels; dialogs trap focus and restore it.
- Screen size: below 1024 px width a banner says "Kubelook is designed for desktop browsers."

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** Each browser window has its own WebSocket; the pop-out re-opens channels for the tabs it takes over (shell tabs cannot move: moving one shows the "start a new shell" prompt).

### Step 4.18 — Metrics

**Goal.** CPU and memory per pod and node from metrics-server (polled), and the optional per-cluster Prometheus source.

**Files.** Create `internal/metrics/{metricsserver,prometheus}.go` with tests, `internal/httpapi/metrics.go`(+test); web `web/src/features/metrics/{usePodMetrics,useNodeMetrics,UsageBar,Sparkline}.tsx` with tests.

API: `GET /api/v1/clusters/{cluster}/metrics/pods?namespace=` and `/metrics/nodes` → `{available:bool, items:[{namespace?, name, cpuMillicores, memoryBytes, containers?:[…]}]}`. `GET /api/v1/clusters/{cluster}/metrics/history?kind=pod|node&namespace=&name=&minutes=60` → `{available, series:[{metric:"cpu"|"memory", points:[[unixSeconds, value]]}]}`.

**Red.** Cases: `metrics.k8s.io/v1beta1` not served → `{available:false}` (no error); quantities converted (`250m` → 250, `128Mi` → 134217728, nanocores `n` suffix); Prometheus not configured for the cluster → `{available:false}`; configured → queries `<url>/api/v1/query_range` with these fixed templates only: pod CPU `sum(rate(container_cpu_usage_seconds_total{namespace="<ns>",pod="<name>",container!=""}[5m]))`, pod memory `sum(container_memory_working_set_bytes{namespace="<ns>",pod="<name>",container!=""})`, node CPU `sum(rate(node_cpu_seconds_total{mode!="idle",instance=~"<name>.*"}[5m]))`, node memory `node_memory_MemTotal_bytes{instance=~"<name>.*"} - node_memory_MemAvailable_bytes{instance=~"<name>.*"}`; label values are escaped (test a name containing `"` and `\`); Prometheus errors → `available:false` with a message; `minutes` limited to 1–1440. Web: polls every 30 s while the view is visible (fake timers, paused when hidden); pods and nodes lists gain CPU and Memory columns when available; usage bars on node details against allocatable.

Command: `make test-backend PKG="./internal/metrics/... ./internal/httpapi/..."` and `make test-frontend`. Expected failure: `undefined: metrics.New`.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** Tests pass.

**Pitfalls.** Users never supply PromQL. The Prometheus URL is deployment configuration (A21); it is an SSRF surface the deployer controls.

### Step 4.19 — Access control "who can" view

**Goal.** A view answering "who can <verb> <resource>" from the bindings the user can list.

**Files.** Create `web/src/lib/rbac.ts`(+test), `web/src/features/rbac/WhoCanPage.tsx`(+test).

**Red.** Cases for `whoCan({verb, group, resource, namespace}, {roles, clusterRoles, roleBindings, clusterRoleBindings})`: ClusterRoleBinding to a ClusterRole with a matching rule → subject listed for all namespaces; RoleBinding in the namespace to a Role or to a ClusterRole → subject listed for that namespace only; wildcards `*`; `resourceNames`-restricted rules are listed with the note "only named objects"; aggregated ClusterRoles use their already-aggregated `rules`; subjects de-duplicated and sorted (kind, namespace, name); a binding whose role is missing is listed under "dangling bindings". Page: pickers for verb, resource (from discovery) and namespace; table of subjects with the binding and role linked; a notice when the user cannot list one of the four kinds ("Results may be incomplete: you can't list ClusterRoleBindings").

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement as a pure function plus a thin page. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** This view reflects RBAC objects only; say so on the page ("Other authorizers, if any, are not shown.").

**Phase 4 exit check.** `make verify` → `VERIFY OK`.

---
# Phase 5 — Platform features

**Delivered in pull request 7**.

Outcome: the spec's "Phase 2. Platform": CRD browser, Helm 3 and 4, Flux, resource map, Karpenter, the ecosystem modules, security views and the cluster overview.

### Step 5.1 — Custom resource browser and schema forms

**Goal.** Generic list, details, edit, delete and create for every served CRD.

**Files.** Create `web/src/lib/jsonpath.ts`(+test), `web/src/features/crds/{CrdSidebarSection,CustomResourceList,SchemaDetails,SchemaForm,useCrd}.tsx` with tests, `web/src/kinds/generic.ts`(+test).

**Red.** Cases:
- `jsonpath.ts` evaluates the subset used by `additionalPrinterColumns`: `.a.b`, `.a[0]`, `.a[*].b`, `.a[?(@.type=="Ready")].status`, `.metadata.labels['x\.y']`; unsupported syntax → empty string, never a throw; arrays joined with `,`.
- Sidebar section "Custom Resources": kinds from discovery whose group is not a built-in Kubernetes group and that no **active** module claims (Step 5.2), grouped by API group, sorted.
- List: columns Name, Namespace (if namespaced), then the CRD version's `additionalPrinterColumns` (types `date` → age, `integer`/`number` → numeric sort), then Age; the CRD is read from `apiextensions.k8s.io/v1` `customresourcedefinitions/<plural>.<group>`; if the user cannot read CRDs the list falls back to Name/Namespace/Age.
- Details: the standard drawer; the Overview tab renders `spec` and `status` as a field tree with descriptions from the CRD's `openAPIV3Schema`; conditions table when `status.conditions` exists.
- Create: `SchemaForm` generates inputs from the schema for `string` (with `enum` → select), `integer`, `number`, `boolean`, `object` (nested group), `array` of scalars (add/remove rows); marks `required`; any schema node it cannot render (`oneOf`, `x-kubernetes-preserve-unknown-fields`, `additionalProperties`, arrays of objects deeper than 2 levels) becomes an embedded YAML field for that node; the form's value and the raw YAML tab stay in sync both ways; submit goes through the dry-run preview of Step 4.4.
- Edit and delete use the standard editor and actions.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement; `schemaToFields(schema)` as a pure function with tests. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** CRD schemas can be recursive or huge: cap rendering depth at 8 and fall back to YAML below that.

### Step 5.2 — Module framework

**Goal.** Detect which ecosystem modules a cluster supports, honour `modules.<name>.enabled`, and keep module kinds out of the Custom Resources list.

**Files.** Create `internal/modules/registry.go`(+test), `internal/httpapi/modules.go`(+test); web `web/src/features/modules/{registry,useModules,ModuleGate}.ts(x)` with tests.

API: `GET /api/v1/clusters/{cluster}/modules` → `{modules:[{name, active, reason?, kinds:[{group, version, resource, kind, namespaced}]}]}`.

Module definitions (the single source of truth, in `registry.go`):

| Name (config key) | Active when the cluster serves | Kinds claimed (served version is taken from discovery) |
| --- | --- | --- |
| `flux` (always enabled; no config key) | any group in the spec's Flux table | the kinds in the spec's Flux table |
| `karpenter` | `karpenter.sh/v1` `NodePool` | `karpenter.sh`: NodePool, NodeClaim, NodeOverlay (`v1alpha1`, if served); in any group starting with `karpenter.`: EC2NodeClass, AKSNodeClass, GCENodeClass, GCECustomMachineType |
| `keda` | `keda.sh` `ScaledObject` | ScaledObject |
| `externalSecrets` | `external-secrets.io` `ExternalSecret` | ExternalSecret, SecretStore, ClusterSecretStore |
| `vaultSecrets` | `ricoberger.de` `VaultSecret` | VaultSecret |
| `certManager` | `cert-manager.io` `Certificate` | Certificate, Issuer, ClusterIssuer, CertificateRequest |
| `gatewayApi` | any kind in `gateway.networking.k8s.io` | every kind in `gateway.networking.k8s.io` and `gateway.networking.x-k8s.io` |
| `trivy` | any kind in `aquasecurity.github.io` | every kind in that group whose name ends in `Report` |
| `permissions` | always | `kyverno.io`: ClusterPolicy, Policy; `wgpolicyk8s.io`: PolicyReport, ClusterPolicyReport; every kind in `constraints.gatekeeper.sh` |

**Red.** Cases: each module active/inactive from a fake discovery; `modules.keda.enabled: false` → inactive with `reason:"disabled"` and its kinds **not** claimed (they reappear under Custom Resources); Karpenter 0.x (`karpenter.sh/v1beta1` only) → inactive, kinds unclaimed; `permissions` active without any CRDs but claims only served kinds; kinds of a claimed group that the module does not list (KEDA `ScaledJob`, `TriggerAuthentication`) stay unclaimed; result sorted by name. Web: `useModules(cluster)`; `ModuleGate name="keda"` renders children only when active; sidebar shows module entries in the spec's order with "Security" holding `trivy` and `permissions`.

Command: `make test-backend PKG="./internal/modules/... ./internal/httpapi/..."` and `make test-frontend`. Expected failure: `undefined: modules.Detect`.

**Green.** `func Detect(d *kube.Discovery, cfg map[string]config.Module) []Status`.

**Refactor.** None. Re-run.

**Acceptance.** Tests pass.

**Pitfalls.** Modules read and change objects only through the generic resource API and `/actions`; no module adds its own Kubernetes client.

### Step 5.3 — Helm: driver interface and Helm 4 adapter (read side)

**Goal.** List releases and read a release's details through the Helm 4 SDK under the user's credentials.

**Files.** Create `internal/helm/driver.go`, `internal/helm/getter.go`, `internal/helm/helm4/helm4.go`, tests for each; `internal/testutil/helmtest/chart.go` (builds a tiny in-memory chart for tests; it is under `internal/testutil`, which is already excluded from coverage — no new exclusion).

**Commands.**

```bash
go get helm.sh/helm/v4@v4.3.1
go doc helm.sh/helm/v4/pkg/action.Configuration
go doc helm.sh/helm/v4/pkg/action.List
go doc helm.sh/helm/v4/pkg/action.Get
go doc helm.sh/helm/v4/pkg/action.History
go doc helm.sh/helm/v4/pkg/action.GetValues
go doc -all helm.sh/helm/v4/pkg/release/v1 | head -150
go doc -all helm.sh/helm/v4/pkg/chart/v2 | head -120
```

Read that output before writing the adapter. Verified facts: `func (cfg *Configuration) Init(getter genericclioptions.RESTClientGetter, namespace, helmDriver string) error`; `action.Configuration` has exported fields `RESTClientGetter`, `Releases *storage.Storage`, `KubeClient kube.Interface`, `RegistryClient`, `Capabilities`; `storage.Init(driver.NewMemory())`; `helm.sh/helm/v4/pkg/kube/fake.PrintingKubeClient{Out io.Writer}`; `action.Install.Run` returns the interface type `ri.Releaser`, so use the accessor/conversion the `release` package documents to reach the concrete `v1.Release`.

**Red.** Cases (the adapter is constructed from an `*action.Configuration`; tests build one with memory storage and `PrintingKubeClient`, seeded with releases created through the SDK's own `Install` action using `helmtest.Chart()`):
- `List("")` returns releases of all namespaces sorted by namespace, name with `Name, Namespace, Revision, Status, Chart, ChartVersion, AppVersion, Updated`.
- `List("team-a")` only that namespace.
- `Get(ns, name, 0)` returns latest: user-supplied values (YAML), computed values (YAML), manifest, notes, hooks `[{name, kind, events}]`, chart metadata; `Get(…, 1)` an older revision; unknown → `apperr.ErrNotFound`.
- `History` newest first with revision, status, chart version, description, time.
- The manifest passes through `redact.Manifest` (a Secret in the test chart has masked values in the output).
- `getter.go`: `NewRESTClientGetter(cfg *rest.Config, namespace string)` implements `genericclioptions.RESTClientGetter` (`ToRESTConfig`, `ToDiscoveryClient` with an in-memory cache, `ToRESTMapper`, `ToRawKubeConfigLoader` returning the namespace); tested against `fakekube`'s `httptest` API server for discovery.

Command: `make test-backend PKG=./internal/helm/...`. Expected failure: `undefined: helm4.New`.

**Green.**

```go
// internal/helm/driver.go
type Release struct{ Name, Namespace, Status, Chart, ChartVersion, AppVersion string; Revision int; Updated time.Time }
type Detail struct{ Release; UserValues, ComputedValues, Manifest, Notes string; Hooks []Hook }
type Revision struct{ Revision int; Status, ChartVersion, Description string; Updated time.Time }
type Driver interface {
	List(ctx context.Context, namespace string) ([]Release, error)
	Get(ctx context.Context, namespace, name string, revision int) (*Detail, error)
	History(ctx context.Context, namespace, name string) ([]Revision, error)
	// write side added in Step 5.5:
	Install(ctx context.Context, o InstallOptions) (*Release, error)
	Upgrade(ctx context.Context, o UpgradeOptions) (*Release, error)
	Rollback(ctx context.Context, namespace, name string, revision int) error
	Uninstall(ctx context.Context, namespace, name string) error
	Test(ctx context.Context, namespace, name string) (TestResult, error)
	Render(ctx context.Context, o UpgradeOptions) (manifest string, err error)
	Status(ctx context.Context, namespace, name string) (*Release, error)   // ErrNotFound when absent
}
// NewConfig is what production uses: cfg.Init(getter, namespace, "secret")
```

In this step the write methods return `errors.New("not implemented")` and have one test each asserting that (replaced in Step 5.5).

**Refactor.** Conversion `toRelease(rel)` in one place. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** Helm storage driver for production is `"secret"` (the default; ConfigMap storage is read only when a deployer's cluster uses it — not supported for writes in v1; state this in `docs/operations.md`). Never return values from `Get` to HTTP callers without the reveal step (Step 5.6).

### Step 5.4 — Helm 3 adapter and the version switch

**Goal.** The same `Driver` over the Helm 3 SDK; `helm.version` picks the adapter.

**Files.** Create `internal/helm/helm3/helm3.go`(+test), `internal/helm/factory.go`(+test).

**Commands.** `go get helm.sh/helm/v3@v3.22.1`, then the same `go doc` commands with `helm.sh/helm/v3/pkg/...` (`pkg/release`, `pkg/chart`). Verified: `func (cfg *Configuration) Init(getter genericclioptions.RESTClientGetter, namespace, helmDriver string, log DebugLog) error` (note the extra `log` argument compared with Helm 4); `helm.sh/helm/v3/pkg/kube/fake.PrintingKubeClient`.

**Red.** The **same test table** as Step 5.3, run against the Helm 3 adapter: put the shared cases in `internal/testutil/helmtest/conformance.go` as `func RunReadConformance(t *testing.T, newDriver func(t *testing.T) helm.Driver, seed func(…))` and call it from both adapters' tests (refactor Step 5.3's tests to use it — still green). Factory cases: `helm.version: 4` → Helm 4 driver; `3` → Helm 3; anything else → error; each driver gets its own temporary working directory under the job's temp dir.

Command: `make test-backend PKG=./internal/helm/...`. Expected failure: `undefined: helm3.New`.

**Green.** Implement; `factory.go`:

```go
type Factory struct{ Version int; Clients kube.Clients }
func (f *Factory) Driver(ctx context.Context, sess session.Session, clusterID, namespace, workDir string) (Driver, error)
```

**Refactor.** Keep the two adapters as near-identical files; do **not** try to share code through generics across the two SDKs' types. Re-run.

**Acceptance.** Tests `ok`; `CGO_ENABLED=0 go build ./...` succeeds with both SDKs linked.

**Pitfalls.** The two SDKs have identically named packages: always import with aliases `actionv3`, `actionv4`, etc. They must never be mixed in one file.

### Step 5.5 — Helm write actions as durable jobs

**Goal.** Install (HTTP repo or OCI), upgrade with edited values, rollback, uninstall and test, as jobs with limits, temp directories and recovery.

**Files.** Modify both adapters (+ conformance tests `RunWriteConformance`); create `internal/helm/jobs.go`, `internal/helm/repos.go`, `internal/helm/chartfetch.go` with tests; `internal/httpapi/helm.go`(+test).

API (all under `/api/v1/clusters/{cluster}/helm`):
- `GET /releases?namespace=`; `GET /releases/{namespace}/{name}?revision=` → detail **without** values (`valuesHidden:true`).
- `POST /releases/{namespace}/{name}/values/reveal` `{revision}` → `{userValues, computedValues}`; audit `helm-values-reveal`.
- `POST /preview` `{action:"install"|"upgrade", namespace, name, chart:{repo?, oci?, name, version}, values}` → `{currentManifest, newManifest}` (both redacted; rendered with the SDK's client-side dry run).
- `POST /jobs` `{action:"install"|"upgrade"|"rollback"|"uninstall"|"test", namespace, name, chart?, values?, revision?}` → `{jobId}`.
- `GET /repositories` → org-wide (`helm.repositories`) plus session repositories (no passwords in the response); `POST /repositories` `{name, url, username?, password?}`; `DELETE /repositories/{name}` (session ones only).

**Red.** Cases:
- Write conformance for both adapters (memory storage + printing client): install creates revision 1 `deployed`; upgrade with new values → revision 2 and the values are stored; rollback to 1 → revision 3 whose values equal revision 1's; uninstall removes the release; test returns hook results; `Render` returns the manifest without storing a release; install of an existing name → `apperr.ErrConflict`.
- Chart fetch from an `httptest` repository serving `index.yaml` and a `.tgz` built by `helmtest`: downloads into the job's temp directory; a chart larger than `helm.maxChartBytes` → refused with `apperr.ErrTooLarge` and the file removed; basic-auth credentials from the repository entry are sent; the temp directory is removed after success **and** after failure (assert the directory is gone).
- OCI references (`oci://…`) are passed to the SDK's chart locator unchanged (unit test asserts only the option mapping; the real pull is Step 7.8).
- Job handler `Run`: records the starting revision in the job target before acting; audit verb per action (`helm-install`, …) with chart name and version, never values; values are stored only in `sealed_params`.
- Job handler `Recover` (spec table): release `deployed` at the target chart version and values → `succeeded`; release unchanged from the starting revision → `interrupted`, offers `[retry]`; status `pending-upgrade`/`pending-rollback` → `interrupted`, offers `[rollback]` (to the starting revision); `pending-install` with no earlier revision → `interrupted`, offers `[uninstall]`; uninstall job and release gone → `succeeded`.
- Concurrency: at most `helm.maxConcurrentJobs` Helm jobs run per replica (uses Step 4.11's per-type limit with all five Helm job types sharing one limit group).
- Session repositories are stored as `helm_repo` session credentials (sealed), gone at sign-out (A20).
- Memory guard busy → `POST /jobs` answers 503 `server_busy`.

Command: `make test-backend PKG="./internal/helm/... ./internal/httpapi/..."`. Expected failure: conformance failures (`not implemented`).

**Green.** Implement. Per job: `workDir := os.MkdirTemp("", "kubelook-helm-*")`, set the SDK's repository cache/config and registry config paths inside it (look up the exact option names with `go doc helm.sh/helm/v4/pkg/cli.EnvSettings` and `go doc helm.sh/helm/v4/pkg/action.ChartPathOptions`, and the v3 equivalents), `defer os.RemoveAll(workDir)`.

**Refactor.** `withWorkDir(fn)` helper; one `classify(startRev int, target, current *Release) (Status, []string)` pure function for `Recover` with a table test. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.**
- `internal/helm` is the only package allowed to write to disk, and only under its temp directory.
- Use the SDK's `RunWithContext` variants; on context cancellation Helm 4 documents that the install "proceeds in the background" — the job must still be classified by `Recover`, never assumed stopped.
- Do not pass `--wait`-style options longer than 10 minutes; timeout is fixed at 10 minutes for v1.

### Step 5.6 — Helm UI

**Goal.** Releases list and details, install/upgrade with diff, rollback, uninstall, test, repositories.

**Files.** Create `web/src/features/helm/{ReleasesPage,ReleaseDetails,ValuesPanel,InstallDialog,UpgradeDialog,RollbackDialog,RepositoriesPanel,useReleases}.tsx` with tests.

**Red.** Cases: the list polls every 15 s while visible (releases are not watchable; fake timers) and shows Name, Namespace, Status, Chart, App version, Revision, Updated; details tabs Values (hidden until "Reveal", which calls the audited endpoint; hidden again on close), Manifest (read-only, Secrets masked), Notes (text only), Hooks, History; upgrade dialog edits values YAML, then shows two diffs — values (old revealed vs new) and manifest (`/preview`) — before the Confirm button is enabled; install dialog picks a repository or OCI reference, chart, version, namespace, release name; rollback picks a revision; uninstall and test confirm first; each action submits a job and shows its progress row; `interrupted` jobs show Retry / Roll back / Uninstall as offered; actions disabled with a reason when the access check denies creating Secrets in the namespace; the page states which Helm major version the console uses (`me.features.helmVersion`).

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** Helm notes and hook output are cluster-supplied text: text nodes only.

### Step 5.7 — Flux: kinds, status, actions, inventory (backend)

**Goal.** Flux reconcile, suspend and resume for every Flux kind, plus inventory resolution for HelmReleases on older Flux.

**Files.** Create `internal/flux/kinds.go`, `internal/flux/actions.go`, `internal/flux/inventory.go`, tests; register actions in `internal/actions`; `internal/httpapi/flux.go`(+test).

Suspend mechanism per kind (from the spec):

| Kinds | Suspend / resume | Reconcile |
| --- | --- | --- |
| GitRepository, OCIRepository, HelmRepository, HelmChart, Bucket, ExternalArtifact, Kustomization, HelmRelease, ImageRepository, ImagePolicy, ImageUpdateAutomation, Provider, Alert, Receiver | merge-patch `spec.suspend: true|false` | merge-patch annotation `reconcile.fluxcd.io/requestedAt: <now RFC 3339 nano>` |
| ArtifactGenerator | annotation `source.extensions.fluxcd.io/reconcile: disabled|enabled` | same `requestedAt` annotation |
| FluxInstance, ResourceSet, ResourceSetInputProvider | annotation `fluxcd.controlplane.io/reconcile: disabled|enabled` | same `requestedAt` annotation |
| FluxReport | cannot be suspended | same `requestedAt` annotation |

(The annotation **values** `disabled`/`enabled` are assumption A28; confirm them in the flux-operator and source-watcher documentation linked from the spec's Sources before merging this step, and record the confirmation in the PR description.)

**Red.** Cases: actions `fluxReconcile`, `fluxSuspend`, `fluxResume` send the exact patch for each row (one test per kind family); the API version used is the one discovery reports as served (test a cluster serving `kustomize.toolkit.fluxcd.io/v1beta2` only and one serving `v1`); reconcile on a suspended object → 409 "Resume the object before reconciling."; suspend on FluxReport → 400; audit verbs `flux-reconcile`, `flux-suspend`, `flux-resume`. Inventory endpoint `GET /api/v1/clusters/{cluster}/flux/inventory?group=&kind=&namespace=&name=` → `{entries:[{group, version, kind, namespace, name}]}`: Kustomization → parse `.status.inventory.entries[].id` (`<namespace>_<name>_<group>_<kind>`, empty namespace for cluster-scoped, version from `.v`); HelmRelease with `.status.inventory` (Flux ≥ 2.8) → same parsing; HelmRelease without it → load the Helm release named by `.status.history[0].name`/`namespace` (fallback: `spec.releaseName` or `<targetNamespace>-<name>` rules of helm-controller: use `status.storageNamespace` and the release name from `status.history[0]`; when absent return an empty list with `note:"inventory unavailable"`) through the Helm driver and list the objects in its manifest.

Command: `make test-backend PKG="./internal/flux/... ./internal/actions/... ./internal/httpapi/..."`. Expected failure: `undefined: flux.Kinds`.

**Green / Refactor.** `flux.Kinds` is a table `[]Kind{Group, Kind, SuspendVia}`; `parseInventoryID(id, v string)`. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** Inventory IDs use `_` as a separator and names may not contain `_`, but groups are empty for core kinds (`default_web__Service`): split into exactly four parts.

### Step 5.8 — Flux UI

**Goal.** Dashboard, per-kind lists and details, and the source → Kustomization/HelmRelease → workload graph.

**Files.** Create `web/src/kinds/flux.ts`(+test), `web/src/features/flux/{FluxDashboard,FluxDetails,FluxGraph,summary,graph}.ts(x)` with tests, `web/src/components/Graph.tsx`(+test), `web/src/lib/forceLayout.ts`(+test). `npm install --save-exact d3-force@3.0.1` and `npm install --save-dev @types/d3-force@^3`.

**Red.** Cases:
- `summary.ts`: per object → `ready` (`Ready=True`), `failed` (`Ready=False`), `inProgress` (`Reconciling=True` or `Ready=Unknown`), `suspended` (per the Step 5.7 table); kinds without status (Alert, Provider on `v1beta3`) count `total` and `suspended` only.
- Dashboard: counts per kind; Flux version and controller health from Deployments labelled `app.kubernetes.io/part-of=flux` (version from label `app.kubernetes.io/version`; ready = available replicas ≥ desired); kinds the cluster does not serve are hidden.
- Details: conditions, last applied revision (`status.lastAppliedRevision` / `status.lastAttemptedRevision`), source artifact (`status.artifact.revision`), inventory (from the endpoint, each entry linking to its view), history (`status.history` for HelmRelease), events; Reconcile disabled while suspended, with the reason as tooltip.
- `graph.ts`: nodes for sources, Kustomizations, HelmReleases and inventory workloads; edges from `spec.sourceRef`, `spec.chart.spec.sourceRef`, `spec.chartRef`, `spec.dependsOn` and inventory; failed nodes flagged.
- `forceLayout.ts`: wraps `d3-force` with a fixed seed and a fixed number of ticks (300) run synchronously, so the layout is deterministic (test: same input → same coordinates).
- `Graph`: SVG with zoom (wheel), pan (drag background), node drag; nodes are focusable buttons with accessible names "<kind> <name>, <status>"; click opens details; hover shows facts.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** `d3-force` uses `Math.random` only through `simulation.randomSource`: set it to a seeded generator. Never run the simulation on a timer in tests.

### Step 5.9 — Resource map

**Goal.** A live graph of objects and links in the selected namespaces, built server-side so no Secret or ConfigMap data reaches the browser.

**Files.** Create `internal/resourcemap/build.go`, `internal/resourcemap/channel.go`, tests; web `web/src/features/resourcemap/{ResourceMapPage,RelatedMap,useResourceMap,legend}.tsx` with tests.

Channel: `map.open` `d = {cluster, namespaces:[…], focus?:{group, kind, namespace, name}}`. Frames: `graph {nodes:[{id, group, kind, namespace, name, status?}], edges:[{from, to, type}], omittedKinds:[…]}` sent on open and whenever the graph changes (debounced 1 s on the clock).

**Red.** `Build(objects)` cases, one per row of the spec's table: Pod → ConfigMaps/Secrets/PVCs via `envFrom`, `env[].valueFrom.secretKeyRef`, and volumes (`configMap`, `secret`, `persistentVolumeClaim`); Deployment/StatefulSet/DaemonSet → their pods (through owner references, via ReplicaSets for Deployments) and their Helm release (annotations `meta.helm.sh/release-name` + `meta.helm.sh/release-namespace`, label `app.kubernetes.io/managed-by=Helm`); Service → pods matched by selector; Ingress → backend Services and TLS Secrets; HTTPRoute/GRPCRoute → `backendRefs` Services; documented non-links produce no edge (`configMapKeyRef`, projected volumes, Ingress `defaultBackend`); node `status` for pods is the phase; nodes carry names only (assert the JSON of the graph contains no `data` field and no value from the ConfigMap/Secret fixtures); deterministic ordering of nodes and edges. Channel cases: subscribes to hub watches for each needed kind as the user; a kind the user cannot list is left out and named in `omittedKinds`; changes are debounced; `focus` restricts the graph to the focused object's connected component (for the "Resources" map in details). Web: page with namespace selection, legend (colour and icon per kind, pod colour by phase, failed pods marked with an icon and label), hover facts, click → details; the related-map panel for Deployment, DaemonSet, StatefulSet, Pod, Service, Ingress details.

Command: `make test-backend PKG=./internal/resourcemap/...` and `make test-frontend`. Expected failure: `undefined: resourcemap.Build`.

**Green / Refactor.** Implement; reuse `Graph` and `forceLayout` from Step 5.8. Re-run.

**Acceptance.** Tests pass.

**Pitfalls.** Helm release nodes come from workload annotations only (release Secrets are dropped by redaction before the hub caches them); their status is not shown on the map. Document this and the spec's known limits in `docs/operations.md` (Step 8.5).

### Step 5.10 — Karpenter module

**Goal.** Overview, topology treemap, scaling decisions, lists and details, node section.

**Files.** Create `web/src/kinds/karpenter.ts`(+test), `web/src/features/karpenter/{Overview,NodePoolCard,Topology,ScalingDecisions,NodeClaimDetails,NodeKarpenterSection,classify,treemap,usage}.ts(x)` with tests.

**Red.** Cases:
- Descriptors with the columns of `kubectl get -o wide`: NodePool (NodeClass, Nodes, Ready, Age, Weight, CPU, Memory), NodeClaim (Type, Capacity, Zone, Node, Ready, Age, ID, NodePool, NodeClass, Drifted), NodeOverlay (Ready, Weight, Age), node classes (Ready, Age).
- Overview: totals (all nodes; Karpenter-managed = nodes with label `karpenter.sh/nodepool`; other), NodePools ready/not, NodeClaims without `status.nodeName`; one card per NodePool: node class ref, readiness, instance types (from the pool's nodes' label `node.kubernetes.io/instance-type`), `spec.limits` vs current CPU/memory (sum of the pool's nodes' capacity) as bars, `spec.template.spec.expireAfter`, NodeOverlays whose requirements match the pool (and those with `Ready=False` listed as rejected), nodes, recent events; filter by name, node status, instance type, pool status.
- `treemap.ts`: squarified layout as a pure function `(items:[{id, value}], width, height) → rects`; areas proportional to values; deterministic. Grouping by NodePool, zone (`topology.kubernetes.io/zone`), instance type or node class; sizing by CPU, memory, pod count or equal; capacity type from label `karpenter.sh/capacity-type`.
- `classify.ts` (A29, heuristic on event reason, case-insensitive substring): contains `drift` → drift; event type `Warning` or reason contains `fail`, `error`, `insufficient` → errors; contains `disrupt`, `terminat`, `consolidat`, `evict`, `empty`, `expir` → scale down; contains `nominat`, `launch`, `register`, `initializ`, `provision` → scale up; otherwise other. A notice states that only events the cluster still holds are shown.
- NodeClaim details: launch/registration/initialization conditions, drift, instance type, capacity type, zone, provider ID, image ID, capacity, links to Node, NodePool, node class; "why not ready" = message of the first false condition.
- Provider node classes page shows only the classes the cluster serves.
- Node details gain a Karpenter section for managed nodes (pool and claim links, instance and capacity type, zone, conditions, `karpenter.sh/do-not-disrupt` annotation, disruption taint `karpenter.sh/disrupted`).
- NodeOverlay UI appears only when the kind is served.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** All data comes from live lists; no backend code is added in this step.

### Step 5.11 — KEDA, External Secrets, cert-manager and Gateway API modules

**Goal.** Four modules that are descriptors plus one or two actions each.

**Files.** Create `web/src/kinds/{keda,externalsecrets,certmanager,gatewayapi}.ts` with tests, `web/src/features/modules/{GatewayOverview,ModuleListPage}.tsx` with tests; backend actions in `internal/actions/modules.go`(+test).

**Red.** Cases:
- KEDA ScaledObject columns as `kubectl get so`: ScaleTargetKind, ScaleTargetName, Min, Max, Ready, Active, Fallback, Paused, Triggers, Authentications, Age; details list every trigger with its `authenticationRef`, conditions, events. Actions `kedaPause` (merge-patch annotation `autoscaling.keda.sh/paused: "true"`) and `kedaResume` (remove the annotation with a merge patch setting it to `null`); audit verb `module-action`.
- External Secrets: ExternalSecret (Store, Refresh interval, Status from `Ready` condition, Target Secret link `spec.target.name` or the object's name), SecretStore, ClusterSecretStore (Status, Provider = first key under `spec.provider`). Action `esoForceSync`: merge-patch annotation `force-sync: "<unix seconds from the clock>"`.
- cert-manager: Certificate (Ready, Secret, Issuer, Not after from `status.notAfter`, Renewal `status.renewalTime`), Issuer/ClusterIssuer (Ready), CertificateRequest (Approved, Denied, Ready, Issuer, Requestor). Edit and delete only.
- Gateway API: overview counting Ready / Not Ready / In Progress / Unknown per kind (from `Accepted`/`Programmed`/`Ready` conditions: any `False` → Not Ready; all `True` → Ready; any `Unknown` → In Progress; none → Unknown) and the events of those objects; one list per served kind (generic columns plus Hostnames, Parents, Class where the fields exist); details link `parentRefs`, `backendRefs` and `gatewayClassName` to their objects.
- Each module's entries disappear when the module is inactive.

Command: `make test-backend PKG=./internal/actions/...` and `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** Tests pass.

**Pitfalls.** Removing an annotation with a JSON merge patch requires an explicit `null` value.

### Step 5.12 — Vault Secrets Operator module

**Goal.** The `ricoberger.de` VaultSecret views, generated-Secret links and consumer restart.

**Files.** Create `web/src/kinds/vaultsecrets.ts`(+test), `web/src/features/vaultsecrets/{VaultSecretDetails,ConsumersPanel,consumers}.ts(x)` with tests; modify the Secret details (warning banner) with tests.

**Red.** Cases: columns Condition (`SecretCreated` status), Reason, Path(s) (`spec.path` or `spec.paths`), Engine (`spec.secretEngine`: KV or PKI), Secret, Age; details show the condition's status, reason, message, last transition; `stale` badge when the condition's `observedGeneration` < `metadata.generation`; link to the Secret of the same name (masked values, audited reveal, certificate details as usual); a Secret labelled `created-by: vault-secrets-operator` links back and warns "Edits will be overwritten by the operator."; `consumers.ts` finds Deployments, StatefulSets and DaemonSets whose pod template uses the Secret through `envFrom.secretRef`, `env[].valueFrom.secretKeyRef` or a `secret` volume; "Restart consumers" confirms, then calls the `restart` action per workload; the cluster overview count of VaultSecrets with `SecretCreated=False` is exposed as `useVaultSecretHealth()`; no Vault API is ever called by this module (no network calls other than the Kubernetes resource API in tests).

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement; `consumers.ts` shares the reference-walking code with the resource-map legend docs only by behaviour, not by import (server vs browser). Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** HashiCorp's own Vault Secrets Operator uses other CRDs and must stay in the generic browser.

### Step 5.13 — Trivy Operator module

**Goal.** Report lists, an overview of the worst findings, a workload section, rescan and export; never show matched secret text.

**Files.** Modify `internal/redact/redact.go`(+test); create `web/src/kinds/trivy.ts`(+test), `web/src/features/trivy/{TrivyOverview,ReportDetails,WorkloadTrivySection,exportFindings}.ts(x)` with tests; action `trivyRescan` in `internal/actions/modules.go`(+test).

**Red.** Cases:
- Redaction: for kinds `ExposedSecretReport` and `ClusterExposedSecretReport` (group `aquasecurity.github.io`), every `report.secrets[*].match` field is removed server-side (test that the match text is absent from list, get and watch output).
- Lists per report kind with severity counts from `report.summary` (`criticalCount`, `highCount`, `mediumCount`, `lowCount`); details list findings (vulnerability ID, package, installed/fixed version, severity, title; check ID and message for config/RBAC/infra; rule and category for exposed secrets; components for SBOM; control results for compliance).
- Overview: top 20 reports by critical then high count; totals per severity.
- Workload section: reports whose labels `trivy-operator.resource.kind/name/namespace` match the workload.
- `trivyRescan` deletes the report after a confirmation that says "The operator should scan again after the report is deleted (not confirmed for every operator version)."; audit `module-action`.
- Export CSV/JSON of findings through `lib/csv.ts` (formula neutralisation applies to titles).

Command: `make test-backend PKG="./internal/redact/... ./internal/actions/..."` and `make test-frontend`. Expected failure: assertion failures / unresolved imports.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** Tests pass.

**Pitfalls.** Reports can be large (SBOM): the list view requests them through the normal watch; the details view fetches the single object.

### Step 5.14 — Permissions and policy module

**Goal.** Effective permissions of every ServiceAccount ranked by risk; Kyverno and Gatekeeper status and violations. Read-only.

**Files.** Create `web/src/lib/risk.ts`(+test), `web/src/features/permissions/{ServiceAccountPermissions,KyvernoPolicies,GatekeeperConstraints}.tsx` with tests.

Risk rules (A18), highest matching level wins:

| Level | A ServiceAccount has a rule that grants… |
| --- | --- |
| critical | `*` verbs on `*` resources; or any of `escalate`, `bind`, `impersonate`; or `get`/`list`/`watch` on `secrets` cluster-wide; or `create` on `pods/exec` or `pods/attach` cluster-wide; or any verb on `nodes/proxy`; or write verbs on `clusterroles`/`clusterrolebindings` |
| high | `get`/`list`/`watch` on `secrets` in a namespace; write verbs (`create`, `update`, `patch`, `delete`, `deletecollection`) on workloads (`pods`, `deployments`, `statefulsets`, `daemonsets`, `jobs`, `cronjobs`) cluster-wide; write verbs on `roles`/`rolebindings`; `create` on `pods/exec` in a namespace; `create` on `serviceaccounts/token` |
| medium | write verbs on workloads, `configmaps`, `services` or `ingresses` in a namespace |
| low | anything else (read-only) |

**Red.** Cases: `effectiveRules(serviceAccount, bindings, roles)` resolves RoleBindings and ClusterRoleBindings (subjects of kind `ServiceAccount`, and groups `system:serviceaccounts` and `system:serviceaccounts:<ns>`); `riskLevel(rules, scope)` per table row (one test per condition, plus wildcards in resources and API groups); list sorted by level then name with the rules that caused the level; always available, even with no CRDs; Kyverno: policies (`kyverno.io` ClusterPolicy, Policy) with `Ready` condition and `spec.validationFailureAction`, violations from `wgpolicyk8s.io` PolicyReport/ClusterPolicyReport results with `result: fail` grouped by policy (a note states this source is unconfirmed until the spike finding is recorded); Gatekeeper: constraints (every kind in `constraints.gatekeeper.sh`) with `spec.enforcementAction`, `status.totalViolations`, and `status.violations`; when `totalViolations` exceeds the listed violations show "Showing N of M violations (Gatekeeper keeps a limited number per constraint)."; Kyverno and Gatekeeper sections hidden when their CRDs are not served; no action buttons anywhere.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement; reuse `web/src/lib/rbac.ts`. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass.

**Pitfalls.** Risk levels are an aid, not a verdict: the page says "Based on RBAC rules only."

### Step 5.15 — Cluster overview

**Goal.** The landing page of a cluster: health and triage.

**Files.** Create `web/src/features/overview/{OverviewPage,NodeHealth,FailingPods,PendingPods,NotReadyPods,WarningTimeline,FailureCounts,triage}.ts(x)` with tests.

**Red.** Cases for `triage.ts` (pure): failing pods (phase `Failed`, or a container waiting with `CrashLoopBackOff`/`ImagePullBackOff`/`ErrImagePull`/`CreateContainerConfigError`); pending pods joined with the message of their latest `FailedScheduling` event; running-but-not-ready pods with the failing condition (`Ready`/`ContainersReady` message) and failing readiness container names; node health counts (ready, not ready, cordoned, under pressure); warning events bucketed per 5 minutes for the timeline (from the injected `now`, over the events the cluster holds). Page: sections above plus Flux failure count (when Flux is served), Helm failure count (releases with status `failed` or `pending-*`), VaultSecret failures (module active), Karpenter summary (module active); each number links to the filtered list; sections the user cannot list are hidden.

Command: `make test-frontend`. Expected failure: unresolved imports.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** `make test-frontend`, `make coverage-frontend` pass; `make verify` → `VERIFY OK`.

**Pitfalls.** The overview opens several watches at once; it must unsubscribe all of them on leave (test with the fake socket: zero open channels after unmount).

---

# Phase 6 — Hardening

**Delivered in pull request 8**.

### Step 6.1 — Prometheus metrics

**Goal.** Export the metrics the spec lists at `GET /metrics`.

**Files.** Create `internal/telemetry/metrics.go`(+test); instrument `session`, `watch`, `ws`, `logs`, `execs`, `vault`, `kube`, `jobs` through small interfaces; `internal/httpapi/metrics_endpoint.go`(+test).

**Commands.** `go get github.com/prometheus/client_golang@v1.25.0`

**Red.** Cases (a fresh `prometheus.NewRegistry()` per test; assert with `testutil.ToFloat64` / `testutil.CollectAndCompare`): `kubelook_sessions_active` (gauge), `kubelook_watches_active`, `kubelook_watch_subscribers`, `kubelook_watch_cached_objects`, `kubelook_watches_per_user` (histogram), `kubelook_ws_connections`, `kubelook_ws_dropped_messages_total{channel}`, `kubelook_log_streams_active`, `kubelook_log_lines_dropped_total`, `kubelook_exec_sessions_active`, `kubelook_vault_request_duration_seconds{endpoint,outcome}`, `kubelook_kube_request_duration_seconds{verb,outcome}`, `kubelook_http_requests_total{route,method,status}`, `kubelook_http_request_duration_seconds{route}`, `kubelook_jobs{type,status}`, `kubelook_refused_requests_total{kind}`, `kubelook_build_info{version,commit}`; label values never contain user, cluster, namespace or object names (cardinality and privacy); `/metrics` is served without a session and contains `kubelook_build_info`.

Command: `make test-backend PKG="./internal/telemetry/... ./internal/httpapi/..."`. Expected failure: `undefined: telemetry.NewMetrics`.

**Green / Refactor.** Each package receives a narrow interface (for example `watch.Metrics{ SetWatches(n int); … }`) with a no-op default, so earlier tests keep passing unchanged. Kubernetes request latency is measured with a `rest.Config.WrapTransport` round-tripper. Re-run.

**Acceptance.** Tests `ok`; `curl -s localhost:8080/metrics | grep -c '^kubelook_'` prints a number ≥ 10 on a running server.

**Pitfalls.** Use the route **pattern** as the `route` label, never the raw path.

### Step 6.2 — OpenTelemetry traces and structured logs

**Goal.** Traces for HTTP requests and outbound Vault/Kubernetes calls; consistent JSON logs with trace IDs.

**Files.** Create `internal/telemetry/tracing.go`(+test), `internal/telemetry/logging.go`(+test); modify `internal/app/serve.go`.

**Commands.** `go get go.opentelemetry.io/otel@v1.47.0 go.opentelemetry.io/otel/sdk@v1.47.0 go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.47.0 go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.72.0`

**Red.** Cases (with the SDK's in-memory span exporter `tracetest.NewInMemoryExporter`): a request to `/healthz` through the instrumented handler produces one server span named by the route pattern; a Vault client call inside it produces a child client span; span attributes never include tokens, the query string or request bodies; `telemetry.otlpEndpoint` empty → a no-op tracer provider and no exporter goroutines (goleak); log records written inside a traced request carry `trace_id` and `span_id`; the logger's level comes from `KUBELOOK_LOG_LEVEL` (`debug|info|warn|error`, default `info`); shutdown flushes the provider.

Command: `make test-backend PKG=./internal/telemetry/...`. Expected failure: `undefined: telemetry.NewTracing`.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** Add `KUBELOOK_LOG_LEVEL` to the environment-variable list in Step 1.1's tests and in `docs/configuration.md` (it is not a secret but is env-only, like the other operational variables).

### Step 6.3 — Memory guard

**Goal.** Refuse new watches, shells and Helm jobs above `memory.refuseNewWorkPercent` of the memory limit, with a clear message.

**Files.** Create `internal/telemetry/memguard.go`(+test); wire into `watch`, `execs`, `helm` job submission.

**Red.** Cases (injected `usage func() uint64` and `limit func() uint64`): usage below the threshold → `Busy() == false`; at or above → true; limit 0 or unknown → never busy; the value is sampled at most once per second on the fake clock; when busy: a new `watch.open` gets `error` code `server_busy` with message "The server is busy. Try again shortly.", a new `exec.open` likewise, `POST /helm/jobs` → 503; existing watches and shells keep working (assert events still flow); each refusal increments `kubelook_refused_requests_total{kind}`. Production readers: `limit` reads `/sys/fs/cgroup/memory.max` (value `max` → fall back to `debug.SetMemoryLimit(-1)` unless it is `math.MaxInt64`); `usage` reads `runtime/metrics` `/memory/classes/total:bytes` minus `/memory/classes/heap/released:bytes`. Test the file parser with a temp file path injected.

Command: `make test-backend PKG="./internal/telemetry/... ./internal/watch/... ./internal/execs/..."`. Expected failure: `undefined: telemetry.NewMemGuard`.

**Green / Refactor.** Implement. Re-run.

**Acceptance.** Tests `ok`.

**Pitfalls.** `GOMEMLIMIT` is set by the Helm chart (Step 8.2) to 90% of the container memory limit; the guard itself never calls `debug.SetMemoryLimit` with a new value.

### Step 6.4 — Watch-hub load test

**Goal.** Measure memory per watch, event latency and behaviour at the spec's proposed scale; report the numbers.

**Files.** Create `test/load/load_test.go` (build tag `load`), `test/load/fakeapiserver.go`, `hack/load.sh`, `docs/load-test.md`; add Makefile target `load`.

**What it does.** Starts an in-process fake API server (an `httptest` TLS server that serves `GET /api/v1/pods` with N generated pods and a `watch=true` stream emitting M events per second) and a real Kubelook server (`app.Serve`) with a stand-in session source; opens S WebSocket clients, each subscribing to pods; records heap growth per watch (`runtime/metrics`), time from event emission to client receipt (p50/p95/p99), time to first snapshot, and refusals. Defaults: N=1000, S=200, M=50, duration 60 s. Output: a Markdown table appended to `docs/load-test.md` with the date and commit.

```make
load:
	go test -tags load -count=1 -timeout 15m -run TestLoad ./test/load/... -v
```

**Red / Green.** This is a measurement, not a unit test: no Red step. It is excluded from `make verify` and from coverage (path `test/`).

**Acceptance.** `make load` prints a results table and exits 0; `docs/load-test.md` contains the table and a comparison with the spec's proposed targets (first list render < 2 s for 1,000 pods; event to client < 1 s; 200 sessions). If a target is missed, record it under "Open findings" in that file; do not change thresholds.

**Pitfalls.** The load test uses real time by nature; it is the one place outside integration tests where that is allowed, and it never runs in the `ci` workflow (only in `nightly.yml`, Step 7.11).

**Phase 6 exit check.** `make verify` → `VERIFY OK`.

---
# Phase 7 — Integration testing (kind + Vault)

**Delivered in pull request 9** (together with Step 8.1 part A).

Outcome: a suite that runs the real Kubelook image against real Vault, two real Kubernetes clusters (kind) and real PostgreSQL, locally with `make it-up && make it` and in CI. No mocks of Vault or Kubernetes. The only stand-in is the identity provider (A23).

**Topology.** Everything is a container on Docker's `kind` network, so containers reach each other by name and the test process on the host reaches them through ports published on `127.0.0.1`.

| Container | Image | Name on the `kind` network | Host port |
| --- | --- | --- | --- |
| kind cluster "alpha" (Kubernetes 1.35) | `kindest/node:v1.35.8@sha256:07b2…23c0` (full digest in §3) | `kubelook-it-alpha-control-plane:6443` | chosen by kind |
| kind cluster "beta" (Kubernetes 1.37) | `kindest/node:v1.37.0@sha256:a1ed…0ae5` | `kubelook-it-beta-control-plane:6443` | chosen by kind |
| Vault (dev mode) | `hashicorp/vault:2.2.1@sha256:962a…6fc2` | `kubelook-it-vault:8200` | 18200 |
| PostgreSQL | `postgres:18.6-trixie` | `kubelook-it-postgres:5432` | 15432 |
| Identity stand-in | built from `test/integration/fakeidp` | `kubelook-it-fakeidp:8300` | 8300 (same number on purpose) |
| Kubelook, SQLite, 1 replica | `kubelook:dev` | `kubelook-it-app` | 18080 |
| Kubelook, PostgreSQL, 2 replicas | `kubelook:dev` | `kubelook-it-pg-1`, `kubelook-it-pg-2` | 18081, 18082 |

### Step 7.1 — Harness: clusters, Vault, PostgreSQL, identity stand-in

**Goal.** `make it-up` creates the whole environment idempotently; `make it-down` removes it.

**Files.** Create `hack/it-up.sh`, `hack/it-down.sh`, `hack/it-lib.sh`, `test/integration/kind/alpha.yaml`, `test/integration/kind/beta.yaml`, `test/integration/fakeidp/main.go`, `test/integration/fakeidp/main_test.go`, `test/integration/fakeidp/Dockerfile`. Modify `Makefile`.

`test/integration/kind/alpha.yaml` (beta is the same with its own image line):

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    image: kindest/node:v1.35.8@sha256:07b2536e30b803ed61d1677a79df6115f798ce64c80f9e22f6ed45afd09323c0
```

`hack/it-up.sh` (outline; write it exactly in this order, each block skipping work that is already done):

```bash
#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
source "$ROOT/hack/it-lib.sh"      # wait_http <url> <seconds>; container_running <name>
mkdir -p "$ROOT/.it"

# 1. Clusters
for c in alpha beta; do
  kind get clusters | grep -qx "kubelook-it-$c" || \
    kind create cluster --name "kubelook-it-$c" --config "$ROOT/test/integration/kind/$c.yaml" --wait 180s
  kind get kubeconfig --name "kubelook-it-$c" > "$ROOT/.it/kubeconfig-$c-host.yaml"                 # for kubectl on the host
  kind get kubeconfig --name "kubelook-it-$c" --internal > "$ROOT/.it/kubeconfig-$c-internal.yaml"  # uploaded to Kubelook in tests
done

# 2. Vault (dev mode, root token "root")
container_running kubelook-it-vault || docker run -d --name kubelook-it-vault --network kind --cap-add IPC_LOCK \
  -p 127.0.0.1:18200:8200 -e VAULT_DEV_ROOT_TOKEN_ID=root -e VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200 \
  hashicorp/vault:2.2.1@sha256:962a92876e168d68eaaa24b0b8b49bbce1cb63024060a67bba95cd158a366fc2
wait_http http://127.0.0.1:18200/v1/sys/health 60

# 3. PostgreSQL
container_running kubelook-it-postgres || docker run -d --name kubelook-it-postgres --network kind \
  -p 127.0.0.1:15432:5432 -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=kubelook postgres:18.6-trixie
wait_pg kubelook-it-postgres 60        # loops on: docker exec … pg_isready -U postgres

# 4. Identity stand-in
docker build -t kubelook-fakeidp:dev -f "$ROOT/test/integration/fakeidp/Dockerfile" "$ROOT"
container_running kubelook-it-fakeidp || docker run -d --name kubelook-it-fakeidp --network kind \
  -p 127.0.0.1:8300:8300 kubelook-fakeidp:dev
wait_http http://127.0.0.1:8300/healthz 30

# 5. Vault content and test fixtures (Step 7.2), Kubelook containers (Step 7.3)
"$ROOT/hack/vault-setup.sh"
"$ROOT/hack/it-apps.sh"
echo "IT UP"
```

`wait_http` is `curl -fsS --retry <n> --retry-delay 1 --retry-connrefused --retry-all-errors -o /dev/null <url>`: a bounded retry on a condition, not a fixed sleep.

`hack/it-down.sh`: `docker rm -f` the five non-kind containers (ignore "no such container"), `kind delete cluster --name kubelook-it-alpha` and `…-beta`, `rm -rf .it`. Ends with `echo "IT DOWN"`.

Makefile additions:

```make
.PHONY: it-up it-down it e2e
it-up: tools image
	hack/it-up.sh
it-down:
	hack/it-down.sh
it:
	go test -tags integration -count=1 -p 1 -timeout 30m ./test/integration/...
	@echo "INTEGRATION OK"
e2e:
	cd test/e2e && npm ci && npx playwright test
	@echo "E2E OK"
```

**Identity stand-in (`fakeidp`).** A single Go program (standard library only) listening on `:8300`. It has unit tests (it is under `test/`, so it does not count toward coverage, but it follows Red → Green like everything else).

| Endpoint | Behaviour |
| --- | --- |
| `GET /healthz` | 200 |
| `POST /admin/next` body `{sub, email, name, hd?, email_verified?, github_id?, github_orgs?:[…], org_403?:bool}` | sets the account the next authorization will sign in |
| OIDC: `GET /oidc/.well-known/openid-configuration`, `GET /oidc/keys`, `GET /oidc/authorize`, `POST /oidc/token` | issuer `http://kubelook-it-fakeidp:8300/oidc`; `authorize` immediately redirects to `redirect_uri` with `code` and `state` and remembers `code_challenge`; `token` checks `client_id`/`client_secret`, verifies `code_verifier` against the S256 challenge (400 if wrong), returns an RS256 `id_token` (key generated at start) with `iss`, `aud`, `sub`, `email`, `email_verified`, `hd` (only if set), `exp` = now + 5 min |
| GitHub: `GET /github/login/oauth/authorize`, `POST /github/login/oauth/access_token`, `GET /github/api/user`, `GET /github/api/user/memberships/orgs/{org}` | same auto-approve flow with PKCE check; `user` returns `{id, login, name, email}`; membership returns `{"state":"active"}` for orgs in `github_orgs`, 404 otherwise, 403 for every org when `org_403` is set |

**Red.** `test/integration/fakeidp/main_test.go` (plain unit tests with `httptest`): discovery document fields; a full code + PKCE exchange yields an ID token whose signature verifies against `/oidc/keys`; wrong verifier → 400; `hd` absent unless set; GitHub membership answers per the table. Command: `go test ./test/integration/fakeidp/...` — fails (`undefined`). **Green.** Implement. **Refactor.** None.

**Acceptance.**

```bash
make it-up                 # last line: IT UP   (steps 5's scripts are stubs printing "skip" until 7.2/7.3)
docker ps --format '{{.Names}}' | sort | grep -c kubelook-it      # 5 or more
bin/kubectl --kubeconfig .it/kubeconfig-alpha-host.yaml version | grep -c 'v1.35'   # 1 (server line)
bin/kubectl --kubeconfig .it/kubeconfig-beta-host.yaml  version | grep -c 'v1.37'   # at least 1
make it-up                 # second run succeeds quickly: idempotent
make it-down               # last line: IT DOWN
```

**Pitfalls.**
- kind names its Docker network `kind`; it exists only after the first cluster is created, so clusters come first.
- The fakeidp port is the same inside and outside (8300) because the browser on the host and Kubelook in a container must use the **same** issuer URL. Host-side clients map the name `kubelook-it-fakeidp` to `127.0.0.1` (Step 7.3 for Go, Step 7.10 for Chromium).
- Two kind clusters need roughly 4 GB of RAM; GitHub's standard runners have enough.
- The host must use cgroup v2 (`stat -fc %T /sys/fs/cgroup` → `cgroup2fs`). On a cgroup v1 host the 1.35 node never becomes ready and `kind create cluster` fails in `kubeadm init` (observed while planning). `it-up.sh` checks this first and exits with that explanation.

### Step 7.2 — Vault content and cluster fixtures

**Goal.** Mounts, roles, policies and tokens that let the tests prove "exactly the clusters the policies allow"; namespaces, workloads and fixture CRDs in both clusters. Its acceptance is the first real test of minting credentials (U1).

**Files.** Create `hack/vault-setup.sh`, `test/integration/testdata/manifests/{base.yaml,pdb.yaml}`, `test/integration/testdata/crds/*.yaml`, `test/integration/testdata/policies/*.hcl`.

`hack/vault-setup.sh` does, for `c` in `alpha`, `beta` (with `K="kubectl --kubeconfig .it/kubeconfig-$c-host.yaml"` and `V="docker exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=root kubelook-it-vault vault"`):

```bash
$K create namespace itops --dry-run=client -o yaml | $K apply -f -
$K create namespace vault-system --dry-run=client -o yaml | $K apply -f -
$K -n vault-system create serviceaccount vault --dry-run=client -o yaml | $K apply -f -
$K create clusterrolebinding vault-admin --clusterrole=cluster-admin --serviceaccount=vault-system:vault \
   --dry-run=client -o yaml | $K apply -f -
JWT="$($K -n vault-system create token vault --duration=24h)"
CA="$($K -n kube-system get configmap kube-root-ca.crt -o jsonpath='{.data.ca\.crt}')"
$V secrets enable -path="kubernetes/$c" kubernetes || true
$V write "kubernetes/$c/config" kubernetes_host="https://kubelook-it-$c-control-plane:6443" \
   kubernetes_ca_cert="$CA" service_account_jwt="$JWT"
$V write "kubernetes/$c/roles/devops-role" allowed_kubernetes_namespaces="*" \
   kubernetes_role_type=ClusterRole kubernetes_role_name=cluster-admin token_default_ttl=10m token_max_ttl=1h
$V write "kubernetes/$c/roles/viewer-role" allowed_kubernetes_namespaces="*" \
   kubernetes_role_type=ClusterRole kubernetes_role_name=view token_default_ttl=10m token_max_ttl=1h
$K apply -f test/integration/testdata/manifests/base.yaml
$K apply -f test/integration/testdata/crds/
```

Then once: `secrets enable -path=kubernetes/empty kubernetes` (no config written: empty host and CA); `secrets enable -path=kubernetes/notk8s kv`; `secrets enable -path=other/gamma kubernetes`. Policies (files in `testdata/policies/`, loaded with `vault policy write`):

| Policy | Grants | Expected clusters in Kubelook |
| --- | --- | --- |
| `it-alice` | `read` `kubernetes/alpha/config`; `update` `kubernetes/alpha/creds/devops-role` | `alpha` (role `devops-role`) |
| `it-bob` | alpha: config read, creds update on both roles; beta: config read, creds update on `devops-role` | `alpha` (two roles, default `devops-role`), `beta` |
| `it-carol` | `read` `kubernetes/beta/config`; **`create` only** on `kubernetes/beta/creds/devops-role`; `read` `kubernetes/alpha/config` | none |
| `it-dave` | read + update for `kubernetes/empty/*` and `other/gamma/*` | `empty` (opening it fails with the "no API server address or CA" message); `gamma` not listed |
| `it-viewer` | alpha: config read, creds update on `viewer-role` | `alpha` with read-only Kubernetes rights |

Tokens, written to `.it/tokens.env` as `ALICE_TOKEN=…` etc.: one per policy with `vault token create -policy=<p> -policy=default -ttl=4h -field=token`; plus `SHORT_TOKEN` (`-policy=it-alice -ttl=20m -explicit-max-ttl=25m`), `BATCH_TOKEN` (`-type=batch -policy=it-alice -ttl=1h`), `REVOKED_TOKEN` (created, then `vault token revoke`).

`base.yaml`: namespace `demo` with a Deployment `web` (2 replicas, image `registry.k8s.io/pause:3.10`), a Deployment `logger` (1 replica, `busybox:1.37` running `sh -c 'i=0; while true; do echo "{\"level\":\"info\",\"msg\":\"tick\",\"n\":$i}"; i=$((i+1)); sleep 1; done'`; the in-container `sleep` is the workload's own pacing, not test synchronisation), a `busybox:1.37` Deployment `shell` running `sleep 1000000` (for exec and the file browser; it has `sh`, `ls`, `tar`, `tee`), a ConfigMap `settings`, a Secret `credentials` with key `password`, a Service `web`, and a PodDisruptionBudget (in `pdb.yaml`) with `maxUnavailable: 0` for `web`. Wait with `$K -n demo rollout status deployment/<name> --timeout=180s`.

`testdata/crds/`: minimal `CustomResourceDefinition`s (schema `type: object` with `x-kubernetes-preserve-unknown-fields: true`, `status` subresource) for: `kustomizations.kustomize.toolkit.fluxcd.io` (v1), `gitrepositories.source.toolkit.fluxcd.io` (v1), `helmreleases.helm.toolkit.fluxcd.io` (v2), `scaledobjects.keda.sh` (v1alpha1), `externalsecrets.external-secrets.io` (v1), `certificates.cert-manager.io` (v1), `vaultsecrets.ricoberger.de` (v1alpha1), `nodepools.karpenter.sh` (v1), `vulnerabilityreports.aquasecurity.github.io` (v1alpha1), `exposedsecretreports.aquasecurity.github.io` (v1alpha1), and a made-up `widgets.example.kubelook.dev` (v1) with two `additionalPrinterColumns` and a structural schema (for the CRD browser). Applied to **alpha only**, so beta proves modules hide themselves.

**Acceptance (also the U1 check).** The first two results below were already observed on Vault 2.2.1 while planning (§2); the third (minting) is new.

```bash
make it-up
source .it/tokens.env
# Vault lists only what the policy allows:
curl -s -H "X-Vault-Token: $ALICE_TOKEN" http://127.0.0.1:18200/v1/sys/internal/ui/mounts | grep -o '"kubernetes/[a-z]*/"' | sort -u
#   expected: "kubernetes/alpha/"   (and nothing for beta, empty, notk8s)
curl -s -H "X-Vault-Token: $CAROL_TOKEN" -X POST -d '{"paths":["kubernetes/beta/creds/devops-role"]}' \
  http://127.0.0.1:18200/v1/sys/capabilities-self | grep -o '"capabilities":\[[^]]*\]'
#   expected: "capabilities":["create"]
curl -s -H "X-Vault-Token: $ALICE_TOKEN" -X POST -d '{"kubernetes_namespace":"itops","cluster_role_binding":true,"ttl":"10m"}' \
  http://127.0.0.1:18200/v1/kubernetes/alpha/creds/devops-role | grep -c service_account_token
#   expected: 1
```

If minting fails, read Vault's `errors` text: it names the cause (unreachable API server, wrong CA, missing `itops` namespace, missing RBAC for the `vault` service account). Fix the harness, not the product. If the first two results differ from §2, stop and report to the repository owner: the spec's discovery design would not hold on that Vault version.

**Pitfalls.**
- `kubectl create token --duration=24h` may be capped by the API server; the Vault mount only needs it during the test run.
- `itops` must exist in both clusters or `creds` fails.
- Never print `.it/tokens.env` in CI logs.

### Step 7.3 — Test framework and Kubelook containers

**Goal.** Start Kubelook from the image against the harness and give tests a small client library.

**Files.** Create `hack/it-apps.sh`, `test/integration/config/{sqlite.yaml,postgres.yaml}`, `test/integration/it/{env,client,ws,eventually,kube}.go`, `test/integration/smoke_test.go`. Every `.go` file under `test/integration` except `fakeidp/` starts with `//go:build integration`.

`config/sqlite.yaml`:

```yaml
server: { publicURL: "http://localhost:18080" }
database: { driver: sqlite, sqlitePath: /data/kubelook.db }
auth:
  restrictSignIn: true
  google: { clientId: kubelook-it, allowedDomains: [corp.example], issuerURL: "http://kubelook-it-fakeidp:8300/oidc" }
  github: { clientId: kubelook-it, allowedOrgs: [acme], baseURL: "http://kubelook-it-fakeidp:8300/github", apiBaseURL: "http://kubelook-it-fakeidp:8300/github/api" }
vault:
  address: "http://kubelook-it-vault:8200"
  candidateRoles: [devops-role, viewer-role]
  tokenTTL: 10m
  remintBefore: 9m50s
debug: { image: "busybox:1.37", shell: /bin/sh }
fileBrowser: { maxTransferBytes: 1048576 }
audit: { retentionDays: 1 }
maintenance: { banner: "integration test banner" }
```

`hack/it-apps.sh` runs (removing any previous container of the same name first):

```bash
KEY="$(head -c 32 /dev/urandom | base64)"; echo "KUBELOOK_ENCRYPTION_KEY=$KEY" > .it/app.env
docker run -d --name kubelook-it-app --network kind -p 127.0.0.1:18080:8080 \
  -v "$ROOT/test/integration/config/sqlite.yaml:/etc/kubelook/config.yaml:ro" --tmpfs /data:uid=65532,gid=65532 --tmpfs /tmp \
  -e KUBELOOK_ENCRYPTION_KEY="$KEY" -e KUBELOOK_GOOGLE_CLIENT_SECRET=it-secret -e KUBELOOK_GITHUB_CLIENT_SECRET=it-secret \
  kubelook:dev serve
wait_http http://127.0.0.1:18080/readyz 60
```

(The PostgreSQL pair is added in Step 7.9.)

`it` package:

```go
func BaseURL() string                       // KUBELOOK_IT_BASE_URL or http://localhost:18080
func Token(name string) string              // from .it/tokens.env
func Kubectl(t testing.TB, cluster string, args ...string) string     // runs bin/kubectl with the host kubeconfig
func Vault(t testing.TB, args ...string) string                       // docker exec … vault …
func Docker(t testing.TB, args ...string) string
func Eventually(t testing.TB, d time.Duration, cond func() bool, msg string)   // polls every 250 ms until d; fails with msg
type Client struct{…}                       // http.Client with cookie jar; Dial maps kubelook-it-fakeidp → 127.0.0.1
func NewClient(t testing.TB) *Client
func (c *Client) SignInGoogle(acct Account) // POST fakeidp /admin/next, GET /auth/google/login, follow redirects, load /me for CSRF
func (c *Client) SignInGitHub(acct Account)
func (c *Client) JSON(method, path string, in, out any) (status int)
func (c *Client) AddVaultToken(token string) (sealed string)
func (c *Client) UploadKubeconfig(label string, content []byte) (sealed string)
func (c *Client) WS() *WS                   // connects /api/v1/ws with the session cookie and Origin header
func (w *WS) Open(op string, d any) *Channel
func (ch *Channel) Next(typ string, within time.Duration) json.RawMessage     // waits for the next frame of that type
func (w *WS) Closed(within time.Duration) (code int)
```

**Red.** `smoke_test.go`: `TestSmoke_HealthAndSignIn` — `/healthz` is 200; `/api/v1/me` is 401; after `SignInGoogle({sub:"1", email:"a@corp.example", hd:"corp.example"})` it is 200 with that email and the maintenance banner text. Run `make it` before `it-apps.sh` exists → fails (connection refused). **Green.** Write `it-apps.sh` and the `it` package. **Refactor.** None.

**Acceptance.** `make it-up && make it` → `ok` and `INTEGRATION OK`. `go test ./...` (no tag) does not compile anything under `test/integration` except `fakeidp` (check: `go list ./... | grep -c test/integration` prints `1`).

**Pitfalls.**
- The Go client must send `Origin: http://localhost:18080` on the WebSocket handshake or the server rejects it.
- Tests share one environment and run with `-p 1`; every test uses its own account (`sub`) and its own Kubernetes namespace (`it.Namespace(t)` creates `it-<random>` and deletes it in cleanup) so order does not matter.

### Step 7.4 — Sign-in flows

**Goal.** End-to-end proof of Step 1 of the spec's authentication.

**Files.** Create `test/integration/signin_test.go`.

**Tests (write first; they fail only if the product is wrong — any failure is a product bug to fix with a unit test added in the owning package first).**
- Google allowed domain → signed in; cookie is `HttpOnly`, `SameSite=Lax`; the user ID is stable across two sign-ins with a changed email.
- Google account without `hd`, with another `hd`, or with `email_verified=false` → redirected to `/?signin_error=not_allowed`, `/me` stays 401.
- GitHub member of `acme` → signed in; not a member → refused; `org_403` → refused.
- A callback with a wrong `state` → `signin_error=state`.
- The PKCE verifier is checked by the stand-in: sign-in works, proving the verifier matches the challenge.
- Mutating call without the CSRF header → 403; with it → not 403.
- Logout → 204, then `/me` 401.
- Audit: `docker logs kubelook-it-app` contains JSON lines with `"audit":true` and verbs `sign-in` (success and failure) and `sign-out`, and no line contains the string `it-secret`.

**Acceptance.** `go test -tags integration -count=1 -run 'TestSignIn' ./test/integration/...` → `ok`.

**Pitfalls.** A second Kubelook container with `restrictSignIn: false` is not started; that branch is covered by unit tests (Steps 2.2, 2.3).

### Step 7.5 — Vault path (Gate 1 of the spec)

**Goal.** Prove: a Vault token lists exactly the clusters its policies allow; credentials are re-minted before expiry; logout closes every open connection.

**Files.** Create `test/integration/vault_test.go`.

**Tests.**
- Discovery exactness: for each token in the Step 7.2 table, `GET /clusters` returns exactly the expected cluster IDs and roles (alice → `[v:alpha]`; bob → `[v:alpha, v:beta]` with alpha roles `[devops-role, viewer-role]`; carol → none; dave → `[v:empty]`; viewer → `[v:alpha]` with `[viewer-role]`).
- Invalid and revoked tokens are rejected with a clear message; the batch token is accepted and reports `canRenew:false`.
- Open `v:alpha` as alice → `state:"open"`; listing pods in `demo` returns the fixture pods; `kubectl get serviceaccounts -n itops` in alpha shows a service account created by Vault; the audit log has `vault-credential-issued` with the creds path and a lease ID and no token.
- Open `v:empty` as dave → 422 with the "no API server address or CA" message.
- bob opens alpha with `viewer-role` → deleting a pod returns 403 from the cluster; the access-check endpoint reports `delete pods` as not allowed.
- Re-mint (A25): after opening, `credentialExpiresAt` from `GET /clusters` changes to a later time within 90 s (`it.Eventually`), and a pod list still works afterwards.
- Renew: with `SHORT_TOKEN`, `POST /credentials/vault/renew` extends `ttlSeconds`; repeating until the explicit max TTL is reached makes `canRenew` false.
- Saved copy: the `sealed` value returned for alice, sent to `/credentials/restore` in a **new** session of the same user → `ok`; in a session of another user → `wrong_user`.
- Logout closes everything: with a watch channel, a follow log channel and a shell open on one WebSocket, `POST /auth/logout` → the socket closes with code 4401 within 10 s; a second socket of the same session on the same replica closes too.
- Vault outage: `docker pause kubelook-it-vault` → `/status` reports unreachable (Eventually ≤ 60 s); an already-open cluster still lists pods; opening `v:beta` returns `vault_unreachable`; `docker unpause` → reachable again and the open succeeds. (Cleanup always unpauses.)

**Acceptance.** `go test -tags integration -count=1 -run 'TestVault' ./test/integration/...` → `ok`. This is the automated form of the spec's Gate 1.

**Pitfalls.** The outage test must run with `t.Cleanup` that unpauses Vault even on failure, or later tests break.

### Step 7.6 — Kubeconfig path

**Files.** Create `test/integration/kubeconfig_test.go`, `test/integration/testdata/kubeconfigs/{exec.yaml,filepath.yaml}`.

**Tests.** Uploading `.it/kubeconfig-alpha-internal.yaml` with label `alpha-kc` → cluster `k:alpha-kc/kind-kubelook-it-alpha`; open → pods listed; a bearer-token kubeconfig built in the test from `kubectl create token` for a service account bound to `view` → read works, write is 403; `exec.yaml` (an `exec` plugin user) and `filepath.yaml` (`client-certificate: /tmp/x`) → rejected with the spec's messages and nothing stored; removing the kubeconfig makes its cluster ID answer 404 `cluster_unavailable`; another user requesting that cluster ID gets the same 404 message; Vault paused → the kubeconfig cluster keeps working; restoring the sealed copy in a new session of the same user brings the cluster back.

**Acceptance.** `go test -tags integration -count=1 -run 'TestKubeconfig' ./test/integration/...` → `ok`.

**Pitfalls.** Use the `--internal` kubeconfig: its server address (`https://kubelook-it-alpha-control-plane:6443`) is reachable from the Kubelook container; the host one (`127.0.0.1:<port>`) is not.

### Step 7.7 — Live data and day-2 operations

**Files.** Create `test/integration/{watch,logs,exec,files,edit,actions,redaction,audit}_test.go`.

**Tests (each in its own namespace on alpha unless stated; all also run against beta through a table over `[]string{"v:alpha","v:beta"}` for watch, logs and exec, proving Kubernetes 1.35 and 1.37).**
- Watch: subscribe to ConfigMaps in the namespace → `snapshot`; `kubectl create configmap` → `ADDED` event within 10 s; patch → `MODIFIED`; delete → `DELETED`. A second channel with the same key gets a snapshot containing the current state. `managedFields` is absent from every object.
- Watch pause and resume: open the watch, send `visibility {hidden:true}`, receive `paused` within 45 s (the default 30 s idle timeout), send `visibility {hidden:false}`, re-open the channel → a fresh `snapshot`.
- Logs: follow `logger` → `lines` frames arrive; `tailLines: 5` on a non-follow request returns exactly 5 lines then `eof`; multi-source by selector `app=web` tags lines with pod names; audit has `log-view-open` and `log-view-close` and no log text.
- Exec: shell into `shell` pod → `opened.shell` is `/bin/sh` (busybox has no bash: proves the fallback); send `echo kubelook-$((6*7))\n` → output contains `kubelook-42`; resize is accepted; `exit\n` → `exit {code:0}`; audit has `exec-start`/`exec-stop` and does **not** contain `kubelook-42`.
- Debug container: `config/sqlite.yaml` sets `debug.image: busybox:1.37` and `debug.shell: /bin/sh`. On a `web` pod (`pause` image, no shell) add a debug container → a shell opens in it; a second request reuses it (`reused:true`).
- Files: list `/etc` in the `shell` pod → contains `passwd`; write `/tmp/a.txt` via `file.save`, read it back; save with a stale `expect` → `conflict`; upload a two-file tar to `/tmp/up`, download the directory → the tar contains both files; a name such as `$(touch pwned)` is created literally and no file `pwned` appears; a 2 MiB upload → `too_large` (`config/sqlite.yaml` sets `fileBrowser.maxTransferBytes: 1048576`).
- Edit: dry-run `PUT` of a ConfigMap returns `live` and `result` and does not change the object; real `PUT` changes it and sets field manager `kubelook` (`kubectl get --show-managed-fields`); a `PUT` with a stale `resourceVersion` → 409; multi-document apply creates two objects.
- Actions: scale `web` to 3 → `kubectl` shows 3 replicas (Eventually); restart sets the `restartedAt` annotation; rollback to revision 1 after an image change restores the image; CronJob trigger creates a Job; evicting a `web` pod with the `maxUnavailable: 0` PDB → 409 with the PDB message.
- Drain: on beta (single node), create a Deployment with a PDB allowing disruption, start a drain job for the node → job `succeeded`, node is cordoned, the pod was evicted; uncordon in cleanup.
- Redaction: the `credentials` Secret arrives masked in list, get, watch and the editor's dry run; reveal returns the real value and writes `secret-reveal` to the audit log; a TLS Secret created in the test yields certificate metadata and no key material; the response bodies of every call in this test file are scanned for the Secret's plaintext value and must not contain it except the reveal response.
- Audit completeness: after the tests above, the audit rows in the SQLite database (copied out with `docker cp` and read with `modernc.org/sqlite`) contain a `requested` and a `success`/`failure` row for each mutating action, each with the Google/GitHub account and the Vault entity fields filled.

**Acceptance.** `go test -tags integration -count=1 -run 'TestWatch|TestLogs|TestExec|TestFiles|TestEdit|TestActions|TestRedaction|TestAudit' ./test/integration/...` → `ok`.

**Pitfalls.** Image pulls inside kind need the network once; pre-pull and `kind load docker-image` the three small images in `it-up.sh` so tests never wait on a registry.

### Step 7.8 — Helm 3 and 4, modules, CRD browser

**Files.** Create `test/integration/{helm,modules}_test.go`, `test/integration/testdata/charts/demo/` (a minimal chart: one ConfigMap, one Secret, `NOTES.txt`, a test hook Pod using `busybox:1.37`), `hack/it-chartrepo.sh` (packages the chart with `helm package` into `.it/charts/`, generates `index.yaml` with `helm repo index --url http://kubelook-it-fakeidp:8300/charts`). Add `GET /charts/*` to `fakeidp` (test first) serving files from `/charts`, and mount `.it/charts` there in `it-up.sh`, so no extra image is needed. Also add to `hack/it-apps.sh` a container `kubelook-it-app-helm3` (port 18083) with `config/helm3.yaml` = `sqlite.yaml` plus `helm: {version: 3, maxChartBytes: 1048576}`.

**Tests.**
- With `helm.version: 4` (default container): install `demo` from the HTTP repository into a new namespace as a job → `succeeded`; the release appears in the list; details show notes and a manifest whose Secret values are masked; values are hidden until the reveal call; upgrade with changed values → revision 2; rollback → revision 3; test → hook result; uninstall → gone. `bin/helm list -n <ns>` (Helm 4 CLI) agrees at each step.
- With `helm.version: 3`: the `kubelook-it-app-helm3` container runs the same sequence.
- Cross-version (spec "to confirm"): a release installed through the Helm 4 container is listed, upgraded and rolled back through the Helm 3 container, and the other way round. The test **records** the outcome in `.it/findings/helm-cross-version.txt` and fails only if listing fails; write the result into `docs/spike-findings.md` in Step 7.11.
- Chart size limit: `it-chartrepo.sh` also packages `demo-big` (the demo chart plus a 2 MiB file under `files/`); installing it through the Helm 3 container (limit 1 MiB) fails with `too_large` and the job is `failed`.
- Modules: `GET /clusters/v:alpha/modules` reports `keda`, `externalSecrets`, `certManager`, `vaultSecrets`, `karpenter`, `trivy`, `flux` active and `gatewayApi` inactive; on `v:beta` all CRD-based modules are inactive and `permissions` is active.
- Flux actions on alpha: create a `Kustomization` custom resource; `fluxReconcile` sets `reconcile.fluxcd.io/requestedAt`; `fluxSuspend` sets `spec.suspend: true`; reconcile while suspended → 409.
- KEDA pause/resume toggles the annotation; External Secrets force-sync sets `force-sync`.
- Trivy: an `ExposedSecretReport` created with a `match` field is returned without it.
- CRD browser: `widgets.example.kubelook.dev` appears in discovery; a Widget created through `POST …/resources/…` is listed; the CRD's printer columns are readable through the CRD object.

**Acceptance.** `go test -tags integration -count=1 -run 'TestHelm|TestModules' ./test/integration/...` → `ok`.

**Pitfalls.** OCI install is exercised only if a registry is available offline; it is not in this suite (no registry container is started). State this in `docs/spike-findings.md` as "OCI install: unit-tested option mapping only".

### Step 7.9 — PostgreSQL, two replicas, audit hardening

**Files.** Create `deploy/postgres/roles.sql`, `test/integration/postgres_test.go`; extend `hack/it-apps.sh`.

`deploy/postgres/roles.sql` (psql variables `app_password`, `cleanup_password`, `retention_days`; run by a database owner **after** `kubelook migrate`):

```sql
CREATE ROLE kubelook_app LOGIN PASSWORD :'app_password';
CREATE ROLE kubelook_cleanup LOGIN PASSWORD :'cleanup_password';
GRANT SELECT, INSERT, UPDATE, DELETE ON users, user_settings, sessions, session_credentials, jobs TO kubelook_app;
GRANT SELECT ON schema_migrations TO kubelook_app;
GRANT INSERT ON audit_events TO kubelook_app;                      -- insert only: no SELECT, UPDATE, DELETE
GRANT SELECT (time), DELETE ON audit_events TO kubelook_cleanup;
ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY audit_insert ON audit_events FOR INSERT TO kubelook_app WITH CHECK (true);
CREATE POLICY audit_cleanup_select ON audit_events FOR SELECT TO kubelook_cleanup
  USING (time < (extract(epoch FROM now()) * 1000)::bigint - (:retention_days)::bigint * 86400000);
CREATE POLICY audit_cleanup_delete ON audit_events FOR DELETE TO kubelook_cleanup
  USING (time < (extract(epoch FROM now()) * 1000)::bigint - (:retention_days)::bigint * 86400000);
```

`it-apps.sh` additions: run `kubelook migrate` in a one-off container with `KUBELOOK_MIGRATE_DATABASE_URL=postgres://postgres:postgres@kubelook-it-postgres:5432/kubelook`; apply `roles.sql` with `docker exec -i kubelook-it-postgres psql -U postgres -d kubelook -v app_password=app -v cleanup_password=cleanup -v retention_days=1`; start `kubelook-it-pg-1` and `-pg-2` (ports 18081, 18082) with `config/postgres.yaml` (`database.driver: postgres`, `publicURL: http://localhost:18081` for both — they stand behind one name in production; the test client talks to each port directly and sets the cookie for both), `KUBELOOK_DATABASE_URL=postgres://kubelook_app:app@kubelook-it-postgres:5432/kubelook`, and the **same** encryption key.

**Tests.**
- A session created on replica 1 is valid on replica 2 (`/me` 200); a cluster opened on 1 lists pods through 2 without re-opening.
- Logout on replica 1 closes a WebSocket held on replica 2 within 15 s (the 5-second watchdog, A13).
- Stopping replica 1 (`docker stop`) → the session continues on replica 2; a watch re-opened there delivers a snapshot.
- Durable job recovery: start a drain job on replica 1 against a node whose pod is blocked by a `maxUnavailable: 0` PDB (so it cannot finish), `docker kill kubelook-it-pg-1`, then within 90 s the job is `interrupted` with an offer of `retry` as seen from replica 2. Restart replica 1 in cleanup.
- Audit tamper resistance: connecting as `kubelook_app`, `UPDATE audit_events …` and `DELETE FROM audit_events` fail with permission errors and `SELECT` fails too; as `kubelook_cleanup`, deleting a row newer than one day affects 0 rows, and a row inserted (by the owner) with a time two days old is deleted; `kubelook audit-cleanup` run as a one-off container with `KUBELOOK_AUDIT_CLEANUP_DATABASE_URL` for the cleanup role prints `deleted 1 records` for that old row.
- Key change: restart both replicas with a different `KUBELOOK_ENCRYPTION_KEY` → an existing session cookie gets 401 `key_changed`; restoring an old sealed copy returns `key_changed` with the spec's message; the audit output has a `key-changed` line with two key IDs and no key.

**Acceptance.** `go test -tags integration -count=1 -run 'TestPostgres' ./test/integration/...` → `ok`.

**Pitfalls.** PostgreSQL requires `SELECT` on the columns used in a `DELETE … WHERE`; that is why the cleanup role has `SELECT (time)` and a matching `SELECT` policy. Use `github.com/jackc/pgx/v5` directly in the test for role checks.

### Step 7.10 — Browser end-to-end tests (Playwright)

**Goal.** Prove the real UI, including the Monaco and xterm adapters that unit tests cannot cover.

**Files.** Create `test/e2e/package.json`, `test/e2e/playwright.config.ts`, `test/e2e/tests/*.spec.ts`, `test/e2e/helpers.ts`.

**Commands.** `cd test/e2e && npm init -y && npm install --save-dev --save-exact @playwright/test@1.65.0 && npx playwright install --with-deps chromium`

`playwright.config.ts`: `baseURL: 'http://localhost:18080'`, one worker, `retries: 0`, Chromium only with `launchOptions.args: ['--host-resolver-rules=MAP kubelook-it-fakeidp 127.0.0.1']`, `trace: 'retain-on-failure'`, `expect.timeout: 15000`.

**Tests.** (1) Sign in through the stand-in, see the add-credentials page, paste `ALICE_TOKEN`, see `alpha` in the cluster rail under "Vault" and no `beta`. (2) Open `alpha` → Pods: the fixture pods are listed; `kubectl run` a pod from the test → its row appears without reload; delete it → the row disappears. (3) Open the `logger` pod's Logs tab → lines appear; search highlights matches. (4) Open a shell in the `shell` pod, type `echo e2e-$((40+2))`, and expect `e2e-42` in the terminal (xterm adapter). (5) Open the `settings` ConfigMap's YAML tab (Monaco adapter), change a value, Preview shows a diff with the changed line, Save succeeds, and `kubectl get` shows the new value. (6) Open the `credentials` Secret: the value is masked; Reveal shows it. (7) Files tab on the `shell` pod lists `/`. (8) Reload the page: still signed in; the saved token copy is restored from IndexedDB after signing out and in again (clusters reappear without pasting). (9) The page has no CSP violations: the test listens for `console` errors containing `Content Security Policy` and fails on any. (10) Sign out → sign-in page.

**Acceptance.** `make it-up && make e2e` → all tests passed, `E2E OK`.

**Pitfalls.** Use role- and text-based locators (`getByRole`), never CSS classes. Use Playwright's auto-waiting assertions (`await expect(locator).toBeVisible()`); never `page.waitForTimeout`.

### Step 7.11 — CI jobs, nightly run, spike findings

**Goal.** The integration and browser suites gate every pull request; slow checks run nightly; the spec's "to confirm" list gets written answers.

**Files.** Modify `.github/workflows/ci.yml` (add job `integration-tests`, and add it to `publish-image.needs`). Create `.github/workflows/nightly.yml`, `docs/spike-findings.md`, `test/integration/slow_test.go`.

The integration job lives in `ci.yml`, not in a workflow of its own: `needs` works only inside one workflow, and `publish-image` must not publish a `main` build before the integration tests of that build have passed.

Job to add to `.github/workflows/ci.yml` (same pins as Step 0.7):

```yaml
  integration-tests:
    name: "Integration & End-to-End Tests (kind, Vault, PostgreSQL)"
    runs-on: ubuntu-24.04
    timeout-minutes: 60
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with: { persist-credentials: false }
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with: { go-version-file: go.mod }
      - uses: actions/setup-node@949feb2413d6458794dcd2491c4babbbce0c15c1 # v7.1.0
        with: { node-version-file: web/.nvmrc, cache: npm, cache-dependency-path: "web/package-lock.json\ntest/e2e/package-lock.json" }
      - uses: actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
        with:
          path: bin
          key: tools-${{ runner.os }}-${{ hashFiles('hack/install-tools.sh') }}
      - run: make it-up
      - run: make it
      - run: npx playwright install --with-deps chromium
        working-directory: test/e2e
      - run: make e2e
      - name: Collect logs
        if: ${{ failure() }}
        run: |
          mkdir -p it-logs
          for c in $(docker ps -a --format '{{.Names}}' | grep kubelook-it); do docker logs "$c" > "it-logs/$c.log" 2>&1 || true; done
          bin/kind export logs it-logs/kind-alpha --name kubelook-it-alpha || true
          bin/kind export logs it-logs/kind-beta --name kubelook-it-beta || true
      - uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        if: ${{ failure() }}
        with:
          name: integration-logs
          path: |
            it-logs
            test/e2e/test-results
          retention-days: 7
      - run: make it-down
        if: ${{ always() }}
```

Indent the job by two spaces under `jobs:` like the others, and give every step a `name:` in the style of Step 0.7.

`nightly.yml`: `on: schedule: - cron: "17 2 * * *"` and `workflow_dispatch`; `permissions: contents: read`; one job that runs `make it-up`, `KUBELOOK_IT_SLOW=1 make it`, `make load`, uploads `docs/load-test.md` output and `.it/findings/` as artifacts, then `make it-down`.

`slow_test.go` (skipped unless `KUBELOOK_IT_SLOW=1`): with a Kubelook container configured `vault.remintBefore: 0s` so no re-mint happens, open a watch and a shell, wait until the 10-minute credential has expired (`it.Eventually` with a 12-minute deadline on "a new list request returns 401"), then record whether the watch still delivers an event and whether the shell still echoes. Writes `.it/findings/credential-expiry.txt`.

`docs/spike-findings.md`: one section per item of the spec's "To confirm in the spike", each with **Result**, **How it was checked** (test name), **Date**, **Consequence for the plan**:

| Spec item | Checked by | If the answer is unfavourable |
| --- | --- | --- |
| Helm 4 release managed by Helm 3 and vice versa | Step 7.8 cross-version test | Document in `docs/operations.md` that teams must not mix; no code change |
| YAML save conflict rate on a busy object | Step 7.7 edit test variant: 20 saves during a rollout, count 409s (recorded, not asserted) | Open an issue proposing patch-on-save; do not change behaviour without the owner's decision |
| Trivy rescans after report deletion | Not checkable (A24) | Stays "unconfirmed"; the UI text already says so |
| Where Kyverno reports violations | Not checkable (A24) | Stays "unconfirmed"; UI note in Step 5.14 |
| Open watch or shell after credential expiry | `slow_test.go` (nightly) | If connections drop: nothing to change (the hub resumes watches; shells end with the standard message) |
| Cluster mounts in a Vault namespace | Not applicable to the test Vault | Owner must answer; if yes, add `vault.namespace` (sent as `X-Vault-Namespace`) in a follow-up step using the same TDD pattern as Step 2.5 |
| U1: minting credentials on Vault 2.2.1 | Step 7.2 acceptance | See Step 7.2 |

**Acceptance.** The pull request shows `Integration & End-to-End Tests (kind, Vault, PostgreSQL)` green; `publish-image.needs` includes `integration-tests`; `docs/ci.md` lists it as a required check; `docs/spike-findings.md` has a filled Result for every row that is checkable.

**Pitfalls.** The integration job has no write permissions and needs no secrets, so it is safe on fork pull requests.

**Phase 7 exit check.** `make verify` → `VERIFY OK`; `make it-up && make it && make e2e` → `INTEGRATION OK`, `E2E OK`.

---

# Phase 8 — Release packaging

**Delivered in pull request 10** (Step 8.1 part A is delivered earlier, in pull request 9).

Outcome: versioned multi-architecture images, the debug image, a Helm chart that installs a working console, and deployment documentation.

### Step 8.1 — Debug image and digest pin

**Goal.** Publish `ghcr.io/bdnix/kubelook-netshoot` and pin Kubelook's default `debug.image` to its digest.

**Files (part A, delivered in pull request 9).** Create `build/netshoot/Dockerfile` (`FROM nicolaka/netshoot:v0.16` plus OCI labels `org.opencontainers.image.source`, `…licenses=Apache-2.0`, `…description`), `.github/workflows/netshoot.yml`; add a `docker` entry for `/build/netshoot` to `.github/dependabot.yml`.

`netshoot.yml`: triggers `push` to `main` with `paths: [build/netshoot/**]` and `workflow_dispatch`; top-level `permissions: contents: read`; one job with `packages: write`, `id-token: write`, `attestations: write` that reads the tag from the `FROM` line (`TAG=$(sed -n 's/^FROM nicolaka\/netshoot:\([^@ ]*\).*/\1/p' build/netshoot/Dockerfile)`), builds for `linux/amd64,linux/arm64`, pushes `ghcr.io/bdnix/kubelook-netshoot:$TAG`, runs Trivy with `exit-code: "0"` (report only: it is a third-party toolbox image; deployers may replace it via `debug.image`), and attests provenance. A new netshoot release reaches the repository as a Dependabot PR on the `FROM` line; merging it publishes the new image (this is the spec's "rebuilds whenever a new netshoot release appears").

**Files (part B, in pull request 10, after pull request 9 is merged and its workflow has pushed the image).** Create `hack/pin-netshoot.sh` (resolves `docker buildx imagetools inspect ghcr.io/bdnix/kubelook-netshoot:<tag> --format '{{json .Manifest.Digest}}'` and rewrites the constant `DefaultDebugImage` in `internal/config/defaults.go`); modify `internal/config/config_test.go`.

**Red (part B).** Add the test: the default `debug.image` matches `^ghcr\.io/bdnix/kubelook-netshoot@sha256:[0-9a-f]{64}$`. `make test-backend PKG=./internal/config/...` fails (the default is still a tag). **Green.** Run `hack/pin-netshoot.sh`; test passes. **Refactor.** None.

**Acceptance.** `docker pull "$(grep -o 'ghcr.io/bdnix/kubelook-netshoot@sha256:[0-9a-f]*' internal/config/defaults.go)"` succeeds; `make verify` → `VERIFY OK`.

**Pitfalls.** Part B cannot start before pull request 9 is merged and its `netshoot` workflow run on `main` is green. The integration config keeps overriding `debug.image` with `busybox:1.37`.

### Step 8.2 — Helm chart

**Goal.** A chart that deploys the console with every setting from the spec.

**Files.** Create `deploy/helm/kubelook/{Chart.yaml,values.yaml,values.schema.json,README.md}`, `deploy/helm/kubelook/templates/{_helpers.tpl,configmap.yaml,deployment.yaml,service.yaml,serviceaccount.yaml,ingress.yaml,pvc.yaml,migrate-job.yaml,audit-cleanup-cronjob.yaml,pdb.yaml,networkpolicy.yaml,NOTES.txt}`, `deploy/helm/kubelook/ci/{sqlite-values.yaml,postgres-values.yaml,full-values.yaml}`, `hack/chart-test.sh`, `ct.yaml`. Modify `Makefile` (target `chart-test`), `.github/workflows/ci.yml` (job `chart-tests`, added to `publish-image.needs` and to the required checks in `docs/ci.md`).

Design rules:
- `values.yaml` top-level keys are **exactly** the spec's setting names (`auth`, `vault`, `encryption`, `maintenance`, `helm`, `memory`, `debug`, `kubeconfig`, `fileBrowser`, `logs`, `modules`, `audit`) plus the A6 keys, plus chart-only keys: `image.{repository,tag,digest,pullPolicy}`, `replicaCount` (default 1), `database.{driver, existingSecret}` (Secret key `url`), `database.migrationSecret` (Secret key `url`), `auth.google.clientSecretRef` / `auth.github.clientSecretRef` (`{name, key}`), `encryption.keySecret` (Secret key `key`), `audit.cleanup.{schedule, credentialsSecret}`, `persistence.{size, storageClass}` (SQLite), `service.{type, port, sessionAffinity}`, `ingress.*`, `resources` (default limits `memory: 512Mi`), `goMemLimit` (default `460MiB`, about 90% of the memory limit; documented: change both together), `hostAliases`, `podAnnotations`, `nodeSelector`, `tolerations`, `affinity`, `networkPolicy.{enabled, egress}`.
- The ConfigMap renders `config.yaml` from the spec-named values (only keys the app knows; test by loading it with `kubelook`'s own config loader in `hack/chart-test.sh`).
- Deployment: one container, `args: [serve]`, env from Secrets (`KUBELOOK_ENCRYPTION_KEY`, provider secrets, `KUBELOOK_DATABASE_URL`), `GOMEMLIMIT`, config mounted at `/etc/kubelook`, `emptyDir` at `/tmp`, PVC at `/data` when SQLite; `securityContext`: `runAsNonRoot: true`, `runAsUser: 65532`, `readOnlyRootFilesystem: true`, `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`, `seccompProfile.type: RuntimeDefault`; probes `/healthz` (liveness) and `/readyz` (readiness); checksum annotation of the ConfigMap; strategy `Recreate` when SQLite, `RollingUpdate` otherwise.
- ServiceAccount with `automountServiceAccountToken: false` (the console has no cluster rights of its own).
- `migrate-job.yaml`: `helm.sh/hook: pre-install,pre-upgrade` Job running `kubelook migrate` with `KUBELOOK_MIGRATE_DATABASE_URL` from `database.migrationSecret`; rendered only for PostgreSQL.
- `audit-cleanup-cronjob.yaml`: rendered when `audit.retentionDays > 0`; runs `kubelook audit-cleanup`; PostgreSQL: env `KUBELOOK_AUDIT_CLEANUP_DATABASE_URL` from `audit.cleanup.credentialsSecret` (required); SQLite: mounts the same PVC with pod affinity to the console pod.
- `values.schema.json` fails the install when: `encryption.keySecret` is empty; `replicaCount > 1` with `database.driver: sqlite`; `database.driver: postgres` without `database.existingSecret`; `helm.version` not 3 or 4; `audit.retentionDays > 0` with PostgreSQL and no `audit.cleanup.credentialsSecret`.

**Red.** Write `hack/chart-test.sh` first. It runs `helm lint` and `helm template` for each `ci/*.yaml` file and asserts with `grep`/`bin/kubectl apply --dry-run=client`:
- SQLite values: one Deployment with strategy `Recreate`, a PVC, no migrate Job, no CronJob (`retentionDays: 0`).
- PostgreSQL values (`replicaCount: 2`, `audit.retentionDays: 30`): two replicas, PDB present, migrate Job with both hook annotations, CronJob with the cleanup env var and **not** `KUBELOOK_DATABASE_URL`, no PVC.
- Full values: every spec setting set to a non-default; the rendered `config.yaml`, extracted and loaded by `bin/kubelook` through a hidden test of the config loader (`go test ./internal/config -run TestLoad_RenderedChartConfig` with env `KUBELOOK_TEST_CHART_CONFIG=<path>`; add this test in this step, skipped when the env var is unset), has every one of those values.
- Schema failures: each of the five invalid combinations makes `helm template` exit non-zero.
- No rendered manifest contains `automountServiceAccountToken: true`, a `hostPath`, or a privileged container.
- Ends with `echo "CHART OK"`.

`make chart-test` → fails (no chart). **Green.** Write the chart until `CHART OK`. **Refactor.** Move repeated labels/selectors into `_helpers.tpl`; re-run.

CI job `chart-tests` (in `ci.yml`, display name `Helm Chart: Lint & Template Tests`): checkout, setup-go, `make tools build`, `make chart-test`, then `helm/chart-testing-action` and `ct lint --config ct.yaml` (`ct.yaml`: `chart-dirs: [deploy/helm]`, `validate-maintainers: false`, `target-branch: main`).

**Acceptance.** `make chart-test` → `CHART OK`; CI job `chart-tests` green.

**Pitfalls.**
- Secrets are referenced, never templated from values: the chart must not accept secret material in `values.yaml`.
- Sticky WebSockets: a WebSocket stays on its replica by nature; `service.sessionAffinity: ClientIP` and the ingress annotations in the README make REST calls prefer the same replica. Document that any load balancer in front must not time out idle WebSockets under 60 s.

### Step 8.3 — Chart install test in kind

**Goal.** Prove the chart produces a working console in a real cluster.

**Files.** Create `hack/it-chart.sh`, `test/integration/chart_test.go` (reuses `TestSmoke` and a Vault round trip against `KUBELOOK_IT_BASE_URL`); add job `chart-install-tests` (display name `Helm Chart: Install Tests (kind)`) to `ci.yml`, to `publish-image.needs` and to the required checks in `docs/ci.md`.

`hack/it-chart.sh` (after `make it-up`): `kind load docker-image kubelook:dev --name kubelook-it-alpha`; create namespace `kubelook`; create Secrets (`kubelook-key` with key `key`, `kubelook-oauth` with the two client secrets); `helm install kubelook deploy/helm/kubelook -n kubelook --wait --timeout 180s` with values: `image.repository=kubelook`, `image.tag=dev`, `image.pullPolicy=Never`, the SQLite defaults, `server.publicURL=http://localhost:18090`, the `auth`/`vault` blocks from `config/sqlite.yaml`, and `hostAliases` mapping `kubelook-it-vault`, `kubelook-it-fakeidp`, `kubelook-it-alpha-control-plane` and `kubelook-it-beta-control-plane` to their container IPs (`docker inspect -f '{{.NetworkSettings.Networks.kind.IPAddress}}' <name>`); then `kubectl -n kubelook port-forward svc/kubelook 18090:80 &` (store the PID in `.it/pf.pid`; `it-down.sh` kills it) and `wait_http http://127.0.0.1:18090/readyz 60`.

**Test.** `KUBELOOK_IT_BASE_URL=http://localhost:18090 go test -tags integration -count=1 -run 'TestSmoke|TestChart' ./test/integration/...`: sign in, add `ALICE_TOKEN`, see `v:alpha`, open it, list pods in `demo`, open a log stream and receive a line; then `helm upgrade` with `maintenance.banner=upgraded` and expect `/me` to show it after the rollout; `helm uninstall` leaves no pods.

**Acceptance.** `hack/it-chart.sh && KUBELOOK_IT_BASE_URL=http://localhost:18090 go test -tags integration -count=1 -run 'TestSmoke|TestChart' ./test/integration/...` → `ok`; CI `chart-install-tests` green.

**Pitfalls.** `kubectl port-forward` here is test tooling on the developer's side; the product still has no port-forward feature.

### Step 8.4 — Publishing from `main`: versions, image, chart, releases

**Goal.** Every build of `main` publishes development artifacts, and a merge that changes the `VERSION` file also publishes a versioned release: signed multi-architecture images, the chart as an OCI artifact, a tag and a GitHub release. Kubelook is deployed as a container (image or Helm chart); standalone binaries are deliberately not published. Nothing is published from pull requests, other branches or tags: a release is a reviewed pull request, and the tag is an output of the build, never its trigger.

**Files.** Create `VERSION`, `hack/version.sh`, `docs/releasing.md`. Modify `.github/workflows/ci.yml` (job `publish-image`, new jobs `publish-chart` and `create-release`), `Makefile` (target `version`).

**Version source.** `VERSION` holds one line, a semantic version without `v` (first content: `0.1.0`). `hack/version.sh` prints it after checking it matches `^[0-9]+\.[0-9]+\.[0-9]+$` (exit 1 otherwise). The backend-tests job runs it, so a malformed `VERSION` fails the pull request that introduces it.

**Release detection.** A `main` build is a release build when `v$(cat VERSION)` does not exist as a tag yet. The first job that needs it (`publish-image`) computes `release=true|false` with `git ls-remote --tags origin "refs/tags/v$version"` and exposes `version` and `release` as job outputs. Checking the tag rather than the diff means a release whose build failed is published by the next green `main` build, and a revert never publishes the same version twice.

**Jobs added to `ci.yml`.** All run only on `push` to `main` (`if: github.event_name == 'push' && github.ref == 'refs/heads/main'`), and each declares only the permissions it needs.

| Job | Needs | Publishes on every `main` build | Also publishes on a release build |
| --- | --- | --- | --- |
| `publish-image` | every required job | `ghcr.io/bdnix/kubelook:main` and `:sha-<sha>`, SBOM, provenance, cosign signature | tags `<version>` and `<major>.<minor>`; Trivy scan of the pushed digest (`exit-code: "1"`) |
| `publish-chart` | `publish-image` | `oci://ghcr.io/bdnix/charts/kubelook` version `<version>-main.<short-sha>` | version `<version>` |
| `create-release` | `publish-image`, `publish-chart` | — (skipped) | tag `v<version>` on the built commit and a GitHub release with generated notes and the chart archive |

- `publish-image` (`packages: write`, `id-token: write`, `attestations: write`): as in Step 0.8, plus the version tags on release builds, `sigstore/cosign-installer` then `cosign sign --yes ghcr.io/bdnix/kubelook@<digest>`. Build argument `VERSION` is `<version>` on release builds and `<version>-main.<short-sha>` otherwise. Outputs: `version`, `release`, `digest`.
- `publish-chart` (`packages: write`): `make tools`; copy the chart, set `image.digest` in the copy's `values.yaml` to `publish-image`'s digest; `helm package` with `--version` and `--app-version` from the table; `helm push … oci://ghcr.io/bdnix/charts`. On release builds, upload the archive as an artifact for `create-release`.
- `create-release` (`contents: write`; runs only when `release == 'true'`): download the chart artifact; `gh release create "v$version" --target "$GITHUB_SHA" --generate-notes kubelook-*.tgz` with `GH_TOKEN: ${{ github.token }}`. `gh release create` creates the tag; no job pushes a tag by itself.

**Cutting a release** (documented in `docs/releasing.md`): open a pull request that changes `VERSION` (and nothing else is required); merge it once every required check is green; the `main` build publishes the release. Preconditions: `main` green including `integration-tests`; `docs/spike-findings.md` up to date; `hack/pin-netshoot.sh` run. Verification: `cosign verify ghcr.io/bdnix/kubelook:<version> --certificate-identity-regexp 'https://github.com/bdnix/kubelook/.*' --certificate-oidc-issuer https://token.actions.githubusercontent.com`, and `gh attestation verify`. Rollback: a released version is never re-published; fix forward with a pull request that bumps `VERSION` again.

**Red.** Tests for the version script, run by `make test-release-scripts` (part of `backend-tests`): `hack/version.sh` with `0.1.0` prints it; with `v0.1.0`, `0.1`, an empty file or two lines exits 1.

**Green.** Write the scripts and the jobs.

**Acceptance.** `make test-release-scripts` passes. `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/ci.yml` prints nothing. After the pull request merges, the `main` build shows `publish-image` and `publish-chart` green and `create-release` green for the first version (`0.1.0` has no tag yet, so that first merge is a release). The executor does not push tags or create releases by hand; say in the PR that the merge publishes `0.1.0`.

**Pitfalls.**
- The publish jobs never run on `pull_request` events, so fork and Dependabot pull requests never get a token that can push, sign or release.
- Restrict who can push to `main` (docs/ci.md): pushing to `main` now publishes.
- `concurrency` on the workflow cancels in-progress runs only for pull requests; `main` builds always finish, so two quick merges each publish.
- `create-release` creates the tag on `$GITHUB_SHA`, the commit that was tested, not on the current tip of `main`.

### Step 8.5 — Deployment documentation

**Goal.** Documentation sufficient to deploy and operate the console without reading the source.

**Files.** Create or complete: `docs/deployment.md`, `docs/configuration.md`, `docs/vault-policy.md`, `docs/postgres.md`, `docs/docker-run.md`, `docs/operations.md`, `docs/security-review.md`; rewrite `README.md`; create `hack/docs-check.sh`; add it to `make verify` as the last step of `lint`.

Required content:
- `deployment.md`: prerequisites (Kubernetes 1.35+, Vault mounts laid out as `kubernetes/<cluster>/` with `kubernetes_host` and `kubernetes_ca_cert` set, OAuth apps for Google/GitHub with redirect URIs `<publicURL>/auth/google/callback` and `<publicURL>/auth/github/callback`, GitHub org approval note), creating the Secrets (key generation: `head -c 32 /dev/urandom | base64`), `helm install` from `oci://ghcr.io/bdnix/charts/kubelook` for SQLite single replica and for PostgreSQL multi-replica (migrate hook, roles), ingress with WebSocket support and idle-timeout guidance, the egress allowlist the deployer must enforce (cluster API servers, Vault, database, Google/GitHub, kubeconfig OIDC issuers, Helm repositories and registries) with a sample NetworkPolicy and the note that NetworkPolicy filters by IP, upgrade and rollback.
- `configuration.md`: every key of Step 1.1's table with type, default and meaning, every environment variable, and the mapping to Helm values.
- `vault-policy.md`: the policy users need (spec "Vault policy users need"), a sample policy file, the role requirements (`kubernetes_role_type=ClusterRole` with `clusterRoleBinding: true`, allowed namespaces include `vault.credsNamespace`), and the note about the internal `sys/internal/ui/mounts` endpoint and re-testing on Vault upgrades (`make it`).
- `postgres.md`: owner vs app vs cleanup roles, running `kubelook migrate`, applying `deploy/postgres/roles.sql`, what tamper resistance covers and that SQLite cannot provide it.
- `docker-run.md`: `docker run` for the console (SQLite volume), for `migrate`, and for `audit-cleanup` with a crontab line; marked "documented, not a supported deployment" (spec).
- `operations.md`: encryption-key rotation procedure and its user-visible effects (spec text), maintenance banner use before restarts, what happens on replica restarts (shells end, watches resume, jobs become interrupted), Vault outage behaviour, metrics list, log format, audit record format, known limits (resource map non-links, Helm ConfigMap storage, OCI install test coverage, event retention), Safari storage note.
- `security-review.md`: the threat table from the spec with, per row, where it is implemented and which test proves it; the CSP value; the list of open human gates (A1).
- `README.md`: what Kubelook is, quick start (kind + Vault via `make it-up`), links to the docs, development commands (`make verify`, `make it`), licence.

`hack/docs-check.sh` (the "test" for this step, written first): fails if any key from `internal/config`'s table is missing in `docs/configuration.md` (it greps for each key printed by `go test ./internal/config -run TestPrintKeys -v`, a small test that prints the key list), if any relative Markdown link in `docs/` or `README.md` points to a missing file, or if `README.md` still contains "Under construction". Prints `DOCS OK`.

**Red.** Run `hack/docs-check.sh` → fails. **Green.** Write the documents until `DOCS OK`. **Refactor.** None.

**Acceptance.** `hack/docs-check.sh` → `DOCS OK`; `make verify` → `VERIFY OK`.

**Pitfalls.** Documentation states only behaviour that a test in this plan proves, or marks it "not verified" with a pointer to `docs/spike-findings.md`.

### Step 8.6 — Final verification

**Goal.** Run the whole checklist of §9 on a clean clone and record the result.

**Commands.**

```bash
git clone https://github.com/bdnix/kubelook.git /tmp/kubelook-final && cd /tmp/kubelook-final
make tools && (cd web && npm ci)
make verify
make it-up && make it && make e2e
hack/it-chart.sh && KUBELOOK_IT_BASE_URL=http://localhost:18090 go test -tags integration -count=1 -run 'TestSmoke|TestChart' ./test/integration/...
make it-down
```

**Acceptance.** Every box in §9 ticked in the description of pull request 10, with the command output for the coverage lines pasted in.

---

## §8 Definition of done

The project is done when all of the following are true on `main`:

1. Every step of Phases 0–8 is merged, in order, through the 10 pull requests of §7, each of which was green in CI when merged.
2. `make verify` prints `VERIFY OK` on a clean clone. In particular: Go unit-test statement coverage ≥ 90% (`Total coverage threshold (90%) satisfied: PASS`), and web unit-test coverage ≥ 90% lines, statements and functions and ≥ 80% branches, with only the exclusions listed in §6.
3. The `ci` workflow is green on pull requests: `backend-tests`, `frontend-tests`, `ui-tests`, `image-scan`, `dependency-review`, `chart-tests`, `integration-tests`, `chart-install-tests`; and on `main` additionally `publish-image`, `publish-chart` and `create-release`.
4. The integration jobs are green: the kind + Vault suite (`make it`), the browser suite (`make e2e`) and the chart install test.
5. Every feature in `docs/spec.md` "Functional requirements", "Authentication and cluster discovery", "Security model" and "Architecture" maps to a merged step (the mapping is the step titles of Phases 2–6), or is listed in §1 as out of scope with a reason.
6. Release artifacts can be produced: a `main` build whose `VERSION` has no tag publishes the image, chart and GitHub release (Step 8.4), the chart installs a working console in kind (Step 8.3), and the image runs as non-root on a distroless base with no fixable HIGH or CRITICAL vulnerabilities at release time.
7. Deployment documentation exists and passes `hack/docs-check.sh`.
8. `docs/spike-findings.md` records a result for every checkable "to confirm" item and names the ones still open for the repository owner (A1, A24).

## §9 Final verification checklist

Run on a clean clone at the final commit. Every line must be true.

**Build and unit tests**
- [ ] `make tools` → `TOOLS OK`
- [ ] `make lint` → `LINT OK` (golangci-lint 0 issues, ESLint 0 warnings, Prettier clean, `tsc` clean, `DOCS OK`)
- [ ] `make coverage-backend` → `Total coverage threshold (90%) satisfied:	PASS` and `Total test coverage:` ≥ 90%
- [ ] `make coverage-frontend` → no `ERROR: Coverage` lines; `All files` row shows lines, statements, functions ≥ 90 and branches ≥ 80
- [ ] `.testcoverage.yml` and `web/vite.config.ts` exclusions are exactly those of §6 (`git diff` against the lists in this plan shows no extra paths)
- [ ] `grep -rn "time\.Sleep" --include=*_test.go internal/ web/src 2>/dev/null | wc -l` → `0`
- [ ] `go test -race -count=3 -shuffle=on ./...` → all `ok` (no flaky tests)
- [ ] `make build` → `BUILD OK`; `bin/kubelook version` prints a version

**CI/CD**
- [ ] `grep -c 'uses: .*@v[0-9]' .github/workflows/*.yml` → `0` for every file (all actions pinned to commit SHAs)
- [ ] Every workflow has top-level `permissions: contents: read`; write permissions appear only on the `publish-*` jobs, `create-release` and `netshoot`
- [ ] Every workflow has a `concurrency` block; every job has `timeout-minutes`
- [ ] `.github/dependabot.yml` covers `github-actions`, `gomod`, `npm` (`/web`), `docker` (`/` and `/build/netshoot`)
- [ ] Latest `ci` run on `main` is green, including every required job and the `publish-*` jobs
- [ ] `ghcr.io/bdnix/kubelook:main` exists and `docker run --rm ghcr.io/bdnix/kubelook:main version` works

**Integration (real Vault, real Kubernetes)**
- [ ] `stat -fc %T /sys/fs/cgroup` → `cgroup2fs` on the machine running the suite (Kubernetes 1.35+ in kind needs cgroup v2)
- [ ] `make it-up` → `IT UP` (two kind clusters on Kubernetes 1.35 and 1.37, Vault, PostgreSQL)
- [ ] `make it` → `INTEGRATION OK` (sign-in flows, Vault discovery exactness, re-mint, logout closes connections, kubeconfig path, watch/logs/exec/files/edit/actions, redaction, audit, Helm 3 and 4, modules, PostgreSQL replicas and audit roles)
- [ ] `make e2e` → `E2E OK`
- [ ] Chart install test (Step 8.3) → `ok`
- [ ] `make it-down` → `IT DOWN`

**Release packaging**
- [ ] `make image` then `docker run --rm kubelook:dev version` works; image user is `65532:65532`; image has no shell
- [ ] `make chart-test` → `CHART OK`
- [ ] `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/*.yml` prints nothing; `make test-release-scripts` passes
- [ ] `internal/config/defaults.go` pins `debug.image` to a `ghcr.io/bdnix/kubelook-netshoot@sha256:…` digest
- [ ] `docs/deployment.md`, `configuration.md`, `vault-policy.md`, `postgres.md`, `docker-run.md`, `operations.md`, `security-review.md`, `releasing.md`, `ci.md`, `spike-findings.md`, `load-test.md` exist; `README.md` no longer says "Under construction"

**Open items handed to the repository owner (not blockers for "done", must be listed in the final PR)**
- [ ] Branch protection with the required checks listed in `docs/ci.md`, and pushes to `main` restricted
- [ ] First release (`VERSION` 0.1.0) published by a `main` build and verified with `cosign verify` and `gh attestation verify`
- [ ] Spec Gate 1 with a real organisation's Vault, and Gate 2 (external security review and penetration test)
- [ ] Unconfirmed spike items in `docs/spike-findings.md`
