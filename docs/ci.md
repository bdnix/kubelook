# Continuous integration

The `ci` workflow (`.github/workflows/ci.yml`) runs on every pull request, in the merge queue (`merge_group`) and on every push to
`main`. Every action is pinned to a commit SHA with its version in a trailing comment, the workflow token is read-only, and only the
release and publishing jobs have write permissions.

## How the pipeline is built

```
pull request / merge queue / main
├── workflow-lint ─┐
├── backend-tests ─┤
├── frontend-tests ┤
├── ui-tests ──────┤
├── chart-tests ───┤
├── dependency-review (pull requests only)
└── build (amd64 + arm64, native runners) ──► integration-tests
                                                     │
main only, after every check above passed:           ▼
release-please ──► publish-image ──► publish-chart
```

- **Build once.** The image is built once per architecture, on a native runner (`ubuntu-24.04` and `ubuntu-24.04-arm`), with
  BuildKit's layer cache. Each build is smoke-tested and scanned on its own architecture and kept as an artifact. The integration
  tests load the amd64 build, and on `main` the publishing job pushes both builds. Nothing is built twice, and what is published is
  what was tested.
- **One real-system environment.** `integration-tests` starts two kind clusters under test (Kubernetes 1.35 and 1.37), a third kind
  cluster that runs Kubelook from its Helm chart (two replicas on PostgreSQL), Vault, PostgreSQL and the sign-in stand-in. The
  clusters start in parallel. The browser suite, the Go integration suite and the chart's life cycle (upgrade, uninstall) all run
  against that release.
- **Verified publishing.** After pushing, the publishing jobs verify the signature and the attestations from the registry, run the
  published image, and pull the published chart back and render it.

## Jobs and what each proves

| Job ID | Name in the checks list | Proves |
| --- | --- | --- |
| `workflow-lint` | Workflows: Lint & Security Audit | actionlint finds no error in the workflows; zizmor finds no security problem (template injection, excessive permissions, unpinned actions) |
| `backend-tests` | Backend: Lint, Unit Tests & Coverage | `golangci-lint` is clean; Go unit tests pass with `-race` and shuffled order; total statement coverage meets the Go threshold; `govulncheck` finds no reachable vulnerability |
| `frontend-tests` | Frontend: Lint, Unit Tests & Coverage | ESLint, Prettier and the TypeScript checks pass; web unit tests pass; coverage meets the web thresholds; `npm audit` finds no high or critical production advisory |
| `ui-tests` | UI Tests (Playwright) | The built web app works in a real browser, with the API answered by the tests (`web/ui`) |
| `chart-tests` | Helm Chart: Lint & Template Tests | The chart lints and renders with every values file in `deploy/helm/kubelook/ci`; the rendered objects have the required properties; the rendered `config.yaml` loads with the application's strict loader; invalid combinations fail; `ct lint` is clean |
| `build` | Image: Build, Smoke Test & Scan (amd64), (arm64) | The image builds for the architecture, runs `version`, runs as `65532:65532`, has no shell, and has no fixable HIGH or CRITICAL vulnerability (Trivy) |
| `integration-tests` | Integration & End-to-End Tests (Helm-installed, kind, Vault, PostgreSQL) | Kubelook installed from its chart works against real Vault, real PostgreSQL and two real Kubernetes clusters: sign-in, the Vault path, kubeconfig clusters, live data and day-2 operations, two replicas sharing sessions, a replica that stops or dies, the audit roles, the key change, the browser suite, and the chart upgrade and uninstall |
| `dependency-review` | Dependency Review | Pull requests add no dependency with a high-severity advisory (pull requests only) |
| `release-please` | Release | On `main` only, after every check passed: keeps the release pull request up to date, or creates the tag and the GitHub release when that pull request was merged ([releasing.md](releasing.md)) |
| `publish-image` | Publish Container Image | On `main` only: pushes the tested amd64 and arm64 builds as one image (`main`, `sha-<commit>`, and the version tags on a release), signs it, attests provenance and an SBOM per architecture, and verifies all of it from the registry |
| `publish-chart` | Publish Helm Chart | On `main` only: publishes the chart pinned to that image's digest, pulls it back and renders it, and on a release attaches it to the GitHub release |

There is no aggregate gate job: branch protection requires the check jobs directly, and `release-please` lists them in `needs`.
Adding or renaming a required job means updating both.

Locally: `make it-up && make e2e && make it` runs the real-system tests (needs Docker and a cgroup v2 host; `make it-down` removes
the environment), and `make verify` runs lint, unit tests with coverage and the build.

## Other workflows

- `netshoot` (`.github/workflows/netshoot.yml`): on a push to `main` that changes `build/netshoot/**`, publishes
  `ghcr.io/bdnix/kubelook-netshoot:<tag>` for `linux/amd64` and `linux/arm64`, with a report-only Trivy scan and a provenance
  attestation. A new netshoot release arrives as a Dependabot pull request on the `FROM` line.

## Coverage rules

Coverage is enforced by the configuration, which is the only source of truth: `.testcoverage.yml` for Go (total statement coverage,
and the excluded paths) and `web/vite.config.ts` for the web app (thresholds and excluded paths). Each exclusion has its reason in a
comment there; a new exclusion needs one too.

## Updating a pinned action

Resolve the commit of a tag, using the `^{}` line when it exists (annotated tags):

```bash
git ls-remote https://github.com/<owner>/<repo> 'refs/tags/<tag>^{}' 'refs/tags/<tag>'
```

Then write `uses: <owner>/<repo>@<sha> # <tag>`. Dependabot rewrites both the SHA and the comment; keep that format exactly.

## Repository-owner settings

These cannot be applied from the repository and must be set by an owner:

- Protect `main`; require pull requests, and turn on the merge queue (the workflow runs on `merge_group`).
- Require these checks: `Workflows: Lint & Security Audit`, `Backend: Lint, Unit Tests & Coverage`, `Frontend: Lint, Unit Tests &
  Coverage`, `UI Tests (Playwright)`, `Helm Chart: Lint & Template Tests`, `Image: Build, Smoke Test & Scan (amd64)`, `Image: Build,
  Smoke Test & Scan (arm64)` and `Integration & End-to-End Tests (Helm-installed, kind, Vault, PostgreSQL)`. Remove the checks that no
  longer exist: `Container Image: Build & Vulnerability Scan`, `Helm Chart: Install Tests (kind)` and the old name of the integration
  job.
- Keep the secret `GH_PAT` (the token release-please uses) as an environment secret of `production` ([releasing.md](releasing.md)).
- Keep the environment `production` restricted to `main`; the release and publishing jobs and the `netshoot` workflow use it.
- Restrict who can push to `main`, and disallow force pushes to it.
- Enable the dependency graph and Dependabot alerts (`dependency-review` fails without the dependency graph).
- Set the default workflow permissions of GitHub Actions to read-only.
