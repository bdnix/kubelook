# Continuous integration

The `CI` workflow (`.github/workflows/ci.yml`) runs on every pull request, in the merge queue (`merge_group`) and on every push to
`main`. Every action is pinned to a commit SHA with its version in a trailing comment, the workflow token is read-only, and only the
release and publishing jobs have write permissions.

## How the pipeline is built

```
pull request / merge queue / main
├── lint-actions ────┐
├── test-go ─────────┤
├── test-ui ─────────┤
├── test-ui-browser ─┤
├── lint-chart ──────┤
├── review-dependencies (pull requests and merge queue)
└── build-image (amd64 + arm64, native runners) ──► test-e2e (SQLite; also PostgreSQL on main and the release PR)
                                                        │
main only, after every check above passed:              ▼
release ──► publish-image ──► publish-chart
```

- **Build once.** The image is built once per architecture, on a native runner (`ubuntu-24.04` and `ubuntu-24.04-arm`), with
  BuildKit's layer cache. Each build is smoke-tested and scanned on its own architecture and kept as an artifact. The integration
  tests load the amd64 build, and on `main` the publishing job pushes both builds. Nothing is built twice, and what is published is
  what was tested.
- **One real-system environment, in two sizes.** `test-e2e` is one job with a matrix over the database. Each entry starts two kind clusters under test (Kubernetes 1.35 and 1.37), a third
  kind cluster that runs Kubelook from its Helm chart, Vault and the sign-in stand-in. The clusters start in parallel. The browser
  suite, the Go integration suite and the chart's life cycle (upgrade, uninstall) all run against that release. The `sqlite` entry
  runs one replica on SQLite (`IT_DATABASE=sqlite`) and is what every pull request runs. The `postgres` entry runs two replicas on
  PostgreSQL (`IT_DATABASE=postgres`) and adds the tests of what the replicas share (the Go tests with the build tag
  `itpostgres`: sessions and opened clusters across replicas, a replica that stops or dies, the audit roles, the key change).
- **When the PostgreSQL entry runs.** On pushes to `main` and on the release pull request (a branch named `release-please--…`, so a
  release candidate is tested on it). Other pull requests and the merge queue run the SQLite entry only.
- **Verified publishing.** After pushing, the publishing jobs verify the signature and the attestations from the registry, run the
  published image, and pull the published chart back and render it.

## Jobs and what each proves

| Job ID | Name in the checks list | Proves |
| --- | --- | --- |
| `lint-actions` | Lint GitHub Actions workflows | actionlint finds no error in the workflows; zizmor finds no security problem (template injection, excessive permissions, unpinned actions) |
| `test-go` | Lint & test Go code | `golangci-lint` is clean; Go unit tests pass with `-race` and shuffled order; total statement coverage meets the Go threshold; `govulncheck` finds no reachable vulnerability |
| `test-ui` | Lint & test UI code | ESLint, Prettier and the TypeScript checks pass; web unit tests pass; coverage meets the web thresholds; `npm audit` finds no high or critical production advisory |
| `test-ui-browser` | UI tests in Chrome | The built web app works in a real browser, with the API answered by the tests (`web/ui`) |
| `lint-chart` | Lint & render Helm chart | The chart lints and renders with every values file in `deploy/helm/kubelook/ci`; the rendered objects have the required properties; the rendered `config.yaml` loads with the application's strict loader; invalid combinations fail; `ct lint` is clean |
| `build-image` | Build & scan image (amd64), Build & scan image (arm64) | The image builds for the architecture, runs `version`, runs as `65532:65532`, has no shell, and has no fixable HIGH or CRITICAL vulnerability (Trivy) |
| `test-e2e` | Integration & e2e tests; on main and the release pull request also Integration & e2e tests (PostgreSQL, two replicas) | Kubelook (one replica on SQLite) installed from its chart works against real Vault and two real Kubernetes clusters: sign-in, the Vault path, kubeconfig clusters, live data and day-2 operations, the browser suite, and the chart upgrade and uninstall. The PostgreSQL entry repeats it with two replicas on real PostgreSQL and adds: sessions and opened clusters shared by both replicas, a replica that stops or dies, the audit roles, the key change |
| `review-dependencies` | Review dependency changes | The change adds no dependency with a high-severity advisory (pull requests and the merge queue; not on pushes to `main`) |
| `release` | Prepare release | On `main` only, after every check passed: keeps the release pull request up to date, or creates the tag and the GitHub release when that pull request was merged ([releasing.md](releasing.md)) |
| `publish-image` | Publish container image | On `main` only: pushes the tested amd64 and arm64 builds as one image (`main`, `sha-<commit>`, and the version tags on a release), signs it, attests provenance and an SBOM per architecture, and verifies all of it from the registry |
| `publish-chart` | Publish Helm chart | On `main` only: publishes the chart pinned to that image's digest, pulls it back and renders it, and on a release attaches it to the GitHub release |

There is no aggregate gate job: branch protection requires the check jobs directly, and `release` lists them in `needs`.
Adding or renaming a required job means updating both.

Locally: `make it-up && make e2e && make it` runs the real-system tests on SQLite with one replica (needs Docker with cgroup v2;
`make it-down` removes the environment); `IT_DATABASE=postgres make it-up e2e it` runs them on PostgreSQL with two replicas and adds the
replica tests, and `make verify` runs lint, unit tests with coverage and the build.

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
- Require these checks: `Lint GitHub Actions workflows`, `Lint & test Go code`, `Lint & test UI code`, `UI tests in Chrome`,
  `Lint & render Helm chart`, `Build & scan image (amd64)`, `Build & scan image (arm64)`, `Integration & e2e tests` and `Review
  dependency changes`.
- Keep the secret `GH_PAT` (the token release-please uses) as an environment secret of `production` ([releasing.md](releasing.md)).
- Keep the environment `production` restricted to `main`; the release and publishing jobs and the `netshoot` workflow use it.
- Restrict who can push to `main`, and disallow force pushes to it.
- Enable the dependency graph and Dependabot alerts (`review-dependencies` fails without the dependency graph).
- Set the default workflow permissions of GitHub Actions to read-only.
