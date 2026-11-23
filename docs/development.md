# Development

How to build, test and change Kubelook. [`ci.md`](ci.md) says what each CI job proves; [`spec.md`](spec.md) says what the product is.

## What you need

| Tool | Version | Why |
| --- | --- | --- |
| Go | the `go` line of `go.mod` (1.27.4) | the server |
| Node.js | `web/.nvmrc` (26.13.1) | the web app and its tests |
| Docker | any recent | the image, and the integration environment |
| A cgroup v2 host | `stat -fc %T /sys/fs/cgroup` prints `cgroup2fs` | only for the integration environment: the Kubernetes 1.35 node of kind does not start on cgroup v1 |
| Chromium | Playwright's, or any build | only for the browser tests |

`make tools` installs the pinned developer tools into `bin/` (golangci-lint, govulncheck, kind, kubectl, Helm 4, and Helm 3 for one
integration test), each checked against a published checksum where one exists. Then install the web dependencies once:

```bash
make tools
cd web && npm ci
```

## The commands

| Command | What it does | Passing output ends with |
| --- | --- | --- |
| `make verify` | `lint`, `coverage`, `build`: run it before every push | `VERIFY OK` |
| `make lint` | golangci-lint, ESLint, Prettier check, `tsc`, and the documentation check | `LINT OK` |
| `make test-backend PKG=./internal/x/...` | Go tests of one package with `-race -shuffle=on` | `ok` |
| `make test-frontend FILE=src/x.test.tsx` | Vitest, one file or all | `Test Files  N passed` |
| `make coverage` | all unit tests with both coverage gates | `COVERAGE OK` |
| `make build` | web build, embed in the Go binary, `bin/kubelook` | `BUILD OK` |
| `make test-ui` | browser tests of the built web app, with the API answered by the tests (`web/ui`) | `N passed` |
| `make image` | the container image `kubelook:dev` | |
| `make it-up` / `make it` / `make it-down` | the integration environment: two kind clusters, Vault, PostgreSQL, a sign-in stand-in, the real image; the Go tests against it; remove it | `IT UP` / `INTEGRATION OK` / `IT DOWN` |
| `make e2e` | the browser tests of the whole stack (`test/e2e`); needs `make it-up` | `E2E OK` |
| `make chart-test` | lint, render and assert the Helm chart; needs no cluster | `CHART OK` |
| `make test-release-scripts` | the tests of `hack/version.sh` | `RELEASE SCRIPTS OK` |
| `make load` | the watch-hub load test ([load-test.md](load-test.md)); a measurement, not a pass or fail | |

The web build rewrites `internal/webui/dist`, which the Go build embeds, so never run Make targets in parallel.

### Browser tests need a Chromium

`make test-ui` uses Playwright's Chromium: `cd web && npx playwright install --with-deps chromium`. Where a Chromium is already
installed elsewhere, set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` to it.

## Repository layout

```
cmd/kubelook/        the entry point; it only calls internal/app
internal/            all Go code; one package per concern
  app/               command dispatch (serve, migrate, audit-cleanup, version), wiring, shutdown
  config/ clock/ crypto/ store/ audit/ session/ auth/    foundations
  vault/ kubeconfig/ cluster/ kube/ redact/              credentials and cluster access
  ws/ watch/ logs/ execs/ files/ jobs/ actions/          live data and day-two operations
  helm/ flux/ modules/ resourcemap/ overview/ metrics/   platform features
  telemetry/ egress/ httpapi/ webui/ testutil/           hardening, the HTTP layer, the embedded web app
web/                 the React single-page app (src/), its unit tests beside the code, and web/ui (browser tests)
deploy/helm/         the Helm chart; deploy/postgres/roles.sql the hardened database roles
test/                integration (build tag integration), e2e (Playwright) and load (build tag load)
hack/                scripts: tools, the integration environment, chart tests, release scripts, docs check
docs/                the documentation; PLAN.md is the implementation plan
```

`internal/httpapi` may import any internal package. Handlers stay thin: decode the request, call a package, encode the answer.

## How to change code

- **Test first.** For each change write the failing test, see it fail for the right reason, write the smallest code that passes, then
  clean up. Commit those steps separately: `test(<id>): …`, `feat(<id>): …`, `refactor(<id>): …`, as Conventional Commits.
- **Go conventions.** Anything that depends on time, randomness, the network or another subsystem is an interface passed into a
  constructor (`New(deps Deps)`), and tests use hand-written fakes (`internal/testutil`), not mock frameworks. Time comes from
  `clock.Clock`: lint rejects `time.Now`, `time.Sleep` and tickers outside `internal/clock`. Wrap errors with `%w`, pass `ctx` first,
  and never log secrets, tokens, Secret values, file content or log lines. No package-level mutable state.
- **Tests are deterministic.** No sleeps and no fixed waits: use the fake clock and wait on channels. Maps never reach output in
  iteration order. Go tests run with `-race -shuffle=on`, and packages that start goroutines check for leaks (`goleak`). Integration
  tests wait with `it.Eventually`, never a bare sleep.
- **Web conventions.** Components are functions; shared state lives in `zustand` stores under `web/src/state`, server data goes
  through `@tanstack/react-query` hooks under `web/src/api`. Everything that touches `fetch`, `WebSocket`, `indexedDB`, `Date.now`
  or timers receives it through a parameter or a context, so tests pass fakes. Strings that come from a cluster are rendered as React
  text, never with `dangerouslySetInnerHTML` (an ESLint rule forbids it), and links are made only with `SafeLink`, which accepts
  only `http` and `https`. Monaco and xterm are used only inside `web/src/adapters`.
- **API conventions.** REST under `/api/v1`, JSON with `camelCase` fields, errors as `{"error":{"code","message"}}` through the one
  function `httpapi.writeError`, and an `X-CSRF-Token` on every request that is not `GET`, `HEAD` or `OPTIONS`.
- **Coverage.** Go total statement coverage must be at least 90% (`.testcoverage.yml`); the web app needs 90% of lines, statements
  and functions and 80% of branches (`web/vite.config.ts`). Every exclusion has a reason in a comment next to it, and a new one needs
  one. Never lower a threshold or skip a test to get green.
- **Dependencies.** Do not add or upgrade a dependency as part of a feature; Dependabot does upgrades. The exception is a published
  advisory that fails a required check: bump the affected dependency to the nearest fixed version in its own `fix(deps):` commit.

## Adding a configuration key

1. Add the field to `internal/config/config.go` (a `json` tag with the key name), its default in `applyDefaults`, its validation in
   `validate`, and the tests in `internal/config`.
2. `TestPrintKeys` counts the keys: update the number in `internal/config/keys_test.go`.
3. Document the key in `docs/configuration.md`. `hack/docs-check.sh` (part of `make lint`) fails when a key or an environment
   variable that the code reads is missing there.
4. If the Helm chart should expose it, add a default to `deploy/helm/kubelook/values.yaml` when it needs one: the chart writes every
   top-level key it knows into `config.yaml`, and the strict loader refuses unknown keys. `make chart-test` checks the rendered file.

## Running it

### The whole stack, on your machine

`make it-up` builds the image and starts everything the product talks to: two kind clusters (Kubernetes 1.35 and 1.37), Vault with
cluster mounts, tokens and policies, PostgreSQL, and a stand-in for Google and GitHub sign-in, then starts the Kubelook image on
`http://localhost:18080` (SQLite) and two replicas on PostgreSQL (ports 18081 and 18082). Vault tokens for the test users are in
`.it/tokens.env`; the Vault CLI runs inside the container `kubelook-it-vault`. `make it-down` removes it all. The console of
this environment admits the domain `corp.example` (Google) and the organisation `acme` (GitHub); the integration tests announce to the
stand-in which account signs in next (`test/integration/it`).

To try the Helm chart in the same environment: `hack/it-chart.sh` installs it into the alpha cluster and forwards it to
`http://localhost:18090`.

### Only the web app

```bash
cd web
npm run dev          # Vite on :5173, proxying /api and /auth to a Kubelook server on :8080
```

Start a server for it with `bin/kubelook serve` (`make build` first), a configuration file and the environment variables of
[configuration.md](configuration.md); `KUBELOOK_CONFIG` points at the file. Sign-in needs an OAuth application or the stand-in of the
integration environment. Most screens are tested without any backend: `web/ui` answers the API calls itself (see
`web/ui/fixtures.ts`), so a new screen starts as a browser test there.

## Tests, by layer

| Layer | Where | Runs in CI as |
| --- | --- | --- |
| Go unit tests | next to the code, `x_test.go` in package `x_test` | `backend-tests` |
| Web unit tests | next to the code, `X.test.tsx` | `frontend-tests` |
| Browser tests of the web app | `web/ui/*.ui.ts` | `ui-tests` |
| Integration: real Vault, real Kubernetes, PostgreSQL | `test/integration` (build tag `integration`) | `integration-tests` |
| Browser tests of the whole stack | `test/e2e` | `integration-tests` |
| Helm chart: render and assert | `hack/chart-test.sh` | `chart-tests` |
| Helm chart: install into kind | `hack/it-chart.sh`, `test/integration/zz_chart_test.go` | `chart-install-tests` |
| Slow checks (credential expiry), load test | `test/integration/slow_test.go`, `test/load` | `nightly` workflow |

A failing integration test is a product bug: the tests run against the real image, Vault and clusters, with one stand-in for the
identity providers.

## Pull requests

Branch from `main` and open a pull request; never push to `main`. A pull request title is `<type>(<scope>): <what it delivers>`,
and its body says what the change is and how it was verified. Do not put session or tool URLs in commits or pull requests. Every
required check must be green before a merge ([ci.md](ci.md) lists them). A merge to `main` publishes images and, when `VERSION`
changed, a release ([releasing.md](releasing.md)).

`AGENTS.md` has the instructions for coding agents that work in this repository.
