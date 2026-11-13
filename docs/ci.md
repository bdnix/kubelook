# Continuous integration

The `ci` workflow (`.github/workflows/ci.yml`) runs on every pull request and on pushes to `main`. Every action is pinned to a commit SHA with the version in a trailing comment, the workflow token is read-only, and only `image-push` has write permissions.

## Jobs and what each proves

| Job | Proves |
| --- | --- |
| `lint-go` | `golangci-lint` is clean (includes formatting, the import rules and the ban on direct `time.Now`/`fmt.Print*`) |
| `lint-web` | ESLint, Prettier and the TypeScript type check pass |
| `test-go` | Go unit tests pass with `-race` and shuffled order |
| `test-web` | Web unit tests pass |
| `coverage (unit tests, >= 90%)` | Go statement coverage is at least 90%; web lines, statements and functions are at least 90% and branches at least 80%. The job summary shows the numbers |
| `build` | The web app builds, is embedded and the Go binary builds and runs `version` |
| `vuln` | `govulncheck` finds no reachable Go vulnerability and `npm audit` finds no high or critical production dependency advisory |
| `image-scan` | The container image builds, runs, and has no fixable HIGH or CRITICAL vulnerability (Trivy) |
| `dependency-review` | Pull requests add no dependency with a high-severity advisory (pull requests only) |
| `ci-ok` | Every job above except `dependency-review` succeeded. This is the single required check |
| `image-push` | On `main` only: publishes `ghcr.io/bdnix/kubelook` for `linux/amd64` and `linux/arm64` with SBOM and provenance |

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
- Require the status check `ci-ok` (and `integration-ok` once it exists, Step 7.11).
- Require branches to be up to date before merging.
- Disallow force pushes to `main`.
- Enable the dependency graph and Dependabot alerts (`dependency-review` fails without the dependency graph).
- Set the default workflow permissions of GitHub Actions to read-only.
