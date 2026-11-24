# Continuous integration

The `ci` workflow (`.github/workflows/ci.yml`) runs on every pull request and on pushes to `main`. Every action is pinned to a commit SHA with the version in a trailing comment, the workflow token is read-only, and only the `publish-*` jobs and `create-release` have write permissions.

## Jobs and what each proves

| Job ID | Name in the checks list | Proves |
| --- | --- | --- |
| `backend-tests` | Backend: Lint, Unit Tests & Coverage | `golangci-lint` is clean (formatting, import rules, ban on direct `time.Now`/`fmt.Print*`); Go unit tests pass with `-race` and shuffled order; total statement coverage meets the Go threshold (shown in the job summary); `govulncheck` finds no reachable vulnerability |
| `frontend-tests` | Frontend: Lint, Unit Tests & Coverage | ESLint, Prettier and the TypeScript checks (app and UI tests) pass; web unit tests pass; coverage meets the web thresholds; `npm audit` finds no high or critical production advisory |
| `ui-tests` | UI Tests (Playwright) | The built web app works in a real browser: user journeys with the API answered by the tests (`web/ui`). Failures upload the report and traces |
| `integration-tests` | Integration & End-to-End Tests (kind, Vault, PostgreSQL) | The real Kubelook image works against real Vault and two real Kubernetes clusters (1.35 and 1.37): sign-in, the Vault path (the spec's Gate 1), kubeconfig clusters, live data and day-2 operations, PostgreSQL with two replicas and the audit roles, and the browser end-to-end suite (`test/e2e`, real Chromium against the real stack). Failures upload the container and kind logs and the browser traces |
| `chart-tests` | Helm Chart: Lint & Template Tests | The Helm chart lints and renders with every values file in `deploy/helm/kubelook/ci`; the rendered objects have the properties the plan lists (strategy, volume, hook, cleanup job, hardened pods); the rendered `config.yaml` loads with the application's own strict loader; the five invalid combinations fail the install; `ct lint` is clean |
| `chart-install-tests` | Helm Chart: Install Tests (kind) | The chart installs a working console into a real kind cluster (`hack/it-chart.sh`): sign-in, Vault discovery, opening a cluster, listing pods, streaming logs, `helm upgrade` with a new setting, `helm uninstall` leaving no pods |
| `image-scan` | Container Image: Build & Vulnerability Scan | The whole product builds (web app, embedded Go binary, image), runs `version`, and has no fixable HIGH or CRITICAL vulnerability (Trivy) |
| `dependency-review` | Dependency Review | Pull requests add no dependency with a high-severity advisory, including GitHub Actions and npm dev dependencies (pull requests only) |
| `publish-image` | Publish Container Image | On pushes to `main` only, after the seven required jobs pass: publishes `ghcr.io/bdnix/kubelook` for `linux/amd64` and `linux/arm64` with SBOM, provenance and a cosign signature; on a release build also the version tags and a Trivy scan of the pushed digest |
| `publish-chart` | Publish Helm Chart | On pushes to `main` only, after `publish-image`: publishes the chart to `oci://ghcr.io/bdnix/charts/kubelook` with the digest of the image just built |
| `create-release` | Create Release | On release builds only: the tag `v<VERSION>` on the tested commit and the GitHub release with the chart archive |

There is no aggregate gate job: branch protection requires the test jobs directly, and `publish-image` lists them in `needs`. Adding or renaming a required job means updating both. Lint and tests of one side share a job (one checkout, one toolchain setup, one cache), and the coverage run is the unit-test run, so no test executes twice.

Locally: `make it-up && make it` runs the integration suite (needs Docker and a cgroup v2 host; `make it-down` removes it), `make lint`, `make test-backend`, `make test-frontend`, `make coverage`, `make test-ui` (needs Chromium: `cd web && npx playwright install --with-deps chromium`), and `make verify` for lint, coverage and build together.

## Other workflows

- `netshoot` (`.github/workflows/netshoot.yml`): on a push to `main` that changes `build/netshoot/**`, publishes `ghcr.io/bdnix/kubelook-netshoot:<tag>` for `linux/amd64` and `linux/arm64`, with a report-only Trivy scan and a provenance attestation. A new netshoot release arrives as a Dependabot pull request on the `FROM` line.

## Publishing

Publishing happens only from builds of `main`; pull requests and other branches never publish. Every build publishes the `main` and `sha-` image tags, the chart `<version>-main.<short-sha>`, a signature and provenance. A build whose `VERSION` has no tag yet is a release build: it also publishes the version tags and the chart `<version>`, and `create-release` creates the tag and the GitHub release. How to cut a release, verify one and roll back is in [releasing.md](releasing.md).

## Coverage rules

Coverage is enforced by the configuration, which is the only source of truth: `.testcoverage.yml` for Go (total statement coverage, and the excluded paths) and `web/vite.config.ts` for the web app (thresholds and excluded paths). Each exclusion has its reason in a comment there; a new exclusion needs one too.

## Updating a pinned action

Resolve the commit of a tag, using the `^{}` line when it exists (annotated tags):

```bash
git ls-remote https://github.com/<owner>/<repo> 'refs/tags/<tag>^{}' 'refs/tags/<tag>'
```

Then write `uses: <owner>/<repo>@<sha> # <tag>`. Dependabot rewrites both the SHA and the comment; keep that format exactly.

## Repository-owner settings

These cannot be applied from the repository and must be set by an owner:

- Protect `main`; require pull requests.
- Require the status checks of the jobs `backend-tests`, `frontend-tests`, `ui-tests`, `integration-tests` and `image-scan`. GitHub lists a job with a `name:` under that name, so select `Backend: Lint, Unit Tests & Coverage`, `Frontend: Lint, Unit Tests & Coverage`, `UI Tests (Playwright)`, `Integration & End-to-End Tests (kind, Vault, PostgreSQL)` and `Container Image: Build & Vulnerability Scan`. Also require `Helm Chart: Lint & Template Tests` (`chart-tests`) and `Helm Chart: Install Tests (kind)` (`chart-install-tests`).
- Create the environment `production` (*Settings → Environments*), with *Deployment branches and tags* set to `main` only, and optionally required reviewers. The publishing jobs and the `netshoot` workflow use it, so a workflow edited on another branch cannot publish ([releasing.md](releasing.md)).
- Restrict who can push to `main`: a push to `main` publishes images and, when `VERSION` changes, a release.
- Require branches to be up to date before merging.
- Disallow force pushes to `main`.
- Enable the dependency graph and Dependabot alerts (`dependency-review` fails without the dependency graph).
- Set the default workflow permissions of GitHub Actions to read-only.
