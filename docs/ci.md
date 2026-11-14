# Continuous integration

The `ci` workflow (`.github/workflows/ci.yml`) runs on every pull request and on pushes to `main`. Every action is pinned to a commit SHA with the version in a trailing comment, the workflow token is read-only, and only `publish-image` has write permissions.

## Jobs and what each proves

| Job ID | Name in the checks list | Proves |
| --- | --- | --- |
| `backend-tests` | Backend: Lint, Unit Tests & Coverage | `golangci-lint` is clean (formatting, import rules, ban on direct `time.Now`/`fmt.Print*`); Go unit tests pass with `-race` and shuffled order; total statement coverage is at least 90% (shown in the job summary); `govulncheck` finds no reachable vulnerability |
| `frontend-tests` | Frontend: Lint, Unit Tests & Coverage | ESLint, Prettier and the TypeScript checks (app and UI tests) pass; web unit tests pass; lines, statements and functions are at least 90% and branches at least 80%; `npm audit` finds no high or critical production advisory |
| `ui-tests` | UI Tests (Playwright) | The built web app works in a real browser: user journeys with the API answered by the tests (`web/ui`). Failures upload the report and traces |
| `image-scan` | Container Image: Build & Vulnerability Scan | The whole product builds (web app, embedded Go binary, image), runs `version`, and has no fixable HIGH or CRITICAL vulnerability (Trivy) |
| `dependency-review` | Dependency Review | Pull requests add no dependency with a high-severity advisory, including GitHub Actions and npm dev dependencies (pull requests only) |
| `publish-image` | Publish Container Image | On pushes to `main` only, after the four required jobs pass: publishes `ghcr.io/bdnix/kubelook` for `linux/amd64` and `linux/arm64` with SBOM and provenance |

There is no aggregate gate job: branch protection requires the four test jobs directly, and `publish-image` lists them in `needs`. Adding or renaming a required job means updating both. Lint and tests of one side share a job (one checkout, one toolchain setup, one cache), and the coverage run is the unit-test run, so no test executes twice.

Locally: `make lint`, `make test-backend`, `make test-frontend`, `make coverage`, `make test-ui` (needs Chromium: `cd web && npx playwright install --with-deps chromium`), and `make verify` for lint, coverage and build together.

## Publishing

Publishing happens only from builds of `main`; pull requests and other branches never publish. Versioned releases are cut by a reviewed pull request that changes the `VERSION` file; the build of that merge creates the tag and the GitHub release. This arrives in Step 8.4 (`docs/releasing.md`); until then a `main` build publishes only the `main` and `sha-` image tags.

## Coverage rules

Go total statement coverage must be at least 90%, and the web thresholds above apply. Only these paths are excluded, and every new exclusion needs a row here and in `PLAN.md` §6 with a reason:

| Path | Reason |
| --- | --- |
| `cmd/kubelook/main.go` | Three lines that call `app.Main` and `os.Exit`; cannot be run in-process |
| `internal/testutil/**` | Test helpers, not product code |
| `test/**`, `hack/**` | Integration, load and tooling code; not part of the product binary (also behind build tags) |
| `web/src/main.tsx` | Browser bootstrap (`createRoot`) |
| `web/src/adapters/monaco/**`, `web/src/adapters/xterm/**` | Thin wrappers around libraries that need a real browser (canvas, workers, layout); exercised by Playwright in Step 7.10 |
| `web/src/**/*.test.*`, `web/src/test/**`, `web/src/**/*.d.ts` | Tests and type declarations |

## Updating a pinned action

Resolve the commit of a tag, using the `^{}` line when it exists (annotated tags):

```bash
git ls-remote https://github.com/<owner>/<repo> 'refs/tags/<tag>^{}' 'refs/tags/<tag>'
```

Then write `uses: <owner>/<repo>@<sha> # <tag>`. Dependabot rewrites both the SHA and the comment; keep that format exactly.

## Repository-owner settings

These cannot be applied from the repository and must be set by an owner:

- Protect `main`; require pull requests.
- Require the status checks of the jobs `backend-tests`, `frontend-tests`, `ui-tests` and `image-scan`. GitHub lists a job with a `name:` under that name, so select `Backend: Lint, Unit Tests & Coverage`, `Frontend: Lint, Unit Tests & Coverage`, `UI Tests (Playwright)` and `Container Image: Build & Vulnerability Scan`. Add `integration-tests` (Step 7.11), `chart-tests` (Step 8.2) and `chart-install-tests` (Step 8.3) when they exist.
- Restrict who can push to `main`: a push to `main` publishes images and, when `VERSION` changes, a release.
- Require branches to be up to date before merging.
- Disallow force pushes to `main`.
- Enable the dependency graph and Dependabot alerts (`dependency-review` fails without the dependency graph).
- Set the default workflow permissions of GitHub Actions to read-only.
