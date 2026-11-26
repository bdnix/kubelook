# Kubelook plan 2: from a working engine to a usable product

Written 2026-11-26. Source of truth for *what* the product is: [`docs/spec.md`](docs/spec.md). This file says what is
still to be done, in what order, and where each part stands. The first plan is archived in
[`docs/plans/20261126-021417-plan.md`](docs/plans/20261126-021417-plan.md); its conventions still apply (see §3).

## §1 Why this plan exists

All 11 pull requests of the first plan are merged. A review of the code and of the running console on 2026-11-26 found:

- **The engine is complete and sound.** Sign-in, Vault discovery and credential minting, kubeconfig upload, sessions on
  several replicas, the watch hub, logs, shells, the file browser, editing, actions, durable jobs, Helm, audit, redaction,
  telemetry and the Helm chart work, and the core of it is proven against real Vault and real kind clusters
  (63 integration tests, 9 browser tests of the whole stack).
- **There is no usable interface.** The spec describes the structure of the screen but not how it looks, and no step of
  the first plan designed it. The web app has 224 lines of CSS: a grid skeleton and the log viewer. Tables do not lay out
  as tables, the cluster rail is 4rem wide and overlaps the sidebar, the dock covers the sidebar, the home page is the
  placeholder "Choose a cluster", and Settings has no link. Every capability can be reached, but not comfortably.
- **One feature is only half wired.** After an OIDC kubeconfig's refresh token rotates, the server prepares a new sealed
  copy (`GET /api/v1/credentials/pending`, `POST /api/v1/credentials/pending/ack`), but the web app never fetches it, so the
  saved kubeconfig stops working at the next sign-in.
- **The platform features are proven only against fakes.** Resource map, cluster overview, metrics, Flux views, Karpenter,
  Gateway API, Vault Secrets Operator, cert-manager, KEDA and External Secrets views, Kyverno and Gatekeeper, taints,
  set image, CronJob trigger, PVC expand, bulk delete/restart/label, field filter, export, palette, node shell and Helm
  from OCI have unit and stand-in UI tests only.
- **Small leftovers:** code reachable only from tests, test fixtures inside `web/src`, one stale package name in
  `docs/development.md`, and a 404 on the StorageVersionMigration list seen in the demo.

The root cause of all of it is the same: every step was accepted on its own tests, and nothing checked the product as a
user meets it. This plan adds that check (§3.2) and fixes what it finds.

## §2 How to use this plan

1. Work through the stages in the order of the status board (§4). A stage is one pull request unless it says otherwise;
   a stage marked **gate** is a document the owner approves, not code.
2. **Status is kept in this file.** Each stage and each step has a `Status` field with one of these values:

   | Status | Meaning |
   | --- | --- |
   | `todo` | Not started |
   | `in progress` | Work has started on the stage's branch |
   | `in review` | The pull request is open and CI is green; waiting for the owner |
   | `blocked` | Cannot go on; the reason is written next to the status |
   | `done` | Merged to `main` (steps: committed and pushed with `make verify` green) |
   | `dropped` | The owner decided against it; the reason is written next to the status |

   Update the status in the same pull request as the work: set the stage to `in progress` in its first commit, update each
   step as it is finished, and set the stage to `in review` when the pull request is marked ready. The owner (or the
   agent, when told the pull request was merged) sets `done` on `main`. The status board in §4 and the `Status` line of
   the stage must always agree.
3. Never start a stage before the stage it depends on is `done`, except where §4 says they may run side by side.
4. Decisions that are the owner's are listed in §5. A step that needs an open decision is `blocked` until it is made;
   do not guess.
5. When the last stage is `done`, archive this file as `AGENTS.md` describes.

## §3 Conventions

### 3.1 Inherited

Everything in §5 (Conventions) and §6 (Test strategy) of the archived plan still applies: test first (Red, Green,
Refactor, each committed), `make verify` must print `VERIFY OK` after every step, push after every step, Conventional
Commits, no fixed waits in tests, no lowered thresholds, no skipped tests, dependencies only as §3.3 below allows.
Step IDs in this plan are `<stage>.<step>` with the prefix `U` (for example `U3.2`), so commits read `test(U3.2): …`.
Pull request titles are `<type>(stage <n>): <what it delivers>`.

### 3.2 New: every stage is checked as a user meets it

The first plan's tests proved each piece separately. From now on a stage that changes what a user sees is finished only
when all of these hold:

1. **Screenshot tests.** Each screen the stage touches has a Playwright screenshot test in `web/ui` (stand-in API, fixed
   data, fixed viewport 1440×900, both themes). Baselines are generated in CI's container image, never on a laptop, with
   `make test-ui-update` (Stage 3 adds it), and committed in the same pull request. A changed baseline is reviewed as an
   image in the pull request.
2. **The user walkthrough.** The walkthrough of §6 is done by hand in the local demo (`docs/demo.md`) on the pull
   request's branch, and the pull request body has a screenshot of every screen the stage changed.
3. **No dead ends.** Every new screen is reachable by clicking from the home page; the pull request names the click path.
4. **Wiring check.** Every API endpoint and WebSocket channel the stage adds is called by the web app, and a browser
   test exercises that call (no endpoint that only unit tests reach).
5. **Design match.** Each screen the stage touches is built from its frame in the Figma file (§3.4). The pull request
   body links the frame next to the screenshot and lists every deviation with its reason. A deviation that is better than
   the design changes the Figma frame first (in the same pull request, noted in the body), so the file never lags the code.

### 3.4 New: Figma is the source of the design

The interface is designed in Figma (D8), not in Markdown wireframes. The split of roles:

- **The Figma file** (location: D8a) holds the design: variables for the colour, spacing, radius and type tokens, a
  component library with the states of U2.3, one frame per screen and state of U2.4, and prototype flows for the click
  paths of U2.5. It is the source of truth for *how it looks*.
- **`docs/ui-spec.md`** is the written companion: the rules that do not show in a picture (density, keyboard behaviour,
  empty/loading/error text, what each screen reads from which API), and a table that links every frame (Figma URL and
  node ID). PNG exports of the frames live in `docs/ui/` so a pull request shows them and the spec reads offline.
  `docs/spec.md` stays the source of truth for *what the product does*.
- **Who works where.** The agent builds and edits the file through the Figma MCP tools (`use_figma`, `get_variable_defs`,
  `get_design_context`, `get_screenshot`) and reads frames back when it builds a screen; the owner reviews and comments in
  Figma. Both need edit access to the file (D8a).
- **Tokens.** The Figma variables are the source of the Mantine theme object in `web/src/theme/` (U3.2). A token changes
  in Figma first, then in the theme, in the same pull request.
- **Not a dependency.** Figma is a design tool, not a build input: nothing in `make verify` or CI calls Figma, and the
  repository does not need a Figma token. The screenshot baselines (§3.2.1) remain what CI enforces.

### 3.3 Dependencies

The dependency rule of the archived plan holds: add or change a dependency only where a step names it and the owner has
agreed in §5. Advisory-driven bumps stay allowed as before. Agreed so far: `@mantine/core`, `@mantine/hooks` (D1a) and
`@fontsource-variable/inter` (D2) in U3.2; the icon package once U2.1 confirms it. Figma (§3.4) adds no dependency;
Code Connect (D8c), if chosen, adds only Figma's own CLI as a developer tool, not a package of the web app, and U3.3
names it.

## §4 Status board

| Stage | Title | Kind | Depends on | Status | Pull request |
| --- | --- | --- | --- | --- | --- |
| 0 | Housekeeping: archive plan 1, the local demo, this plan | code + docs | – | in review | [#42](https://github.com/bdnix/kubelook/pull/42) |
| 1 | Finish what is half done, remove leftovers | code | 0 | todo | – |
| 2 | UI design in Figma and its specification | **gate** (design + document) | 0 | todo | – |
| 3 | UI foundation: tokens, base styles, the shell, screenshot tests | code | 2 | todo | – |
| 4 | Lists and tables | code | 3 | todo | – |
| 5 | Details drawer, dialogs and forms | code | 4 | todo | – |
| 6 | Home, navigation and first run | code | 5 | todo | – |
| 7 | Dock, logs, shell and files | code | 6 | todo | – |
| 8 | Platform pages: overview, Helm, Flux, map, Karpenter, modules | code | 7 | todo | – |
| 9 | Prove the platform features on real systems | code | 1 | todo | – |
| 10 | Demo and documentation refresh, owner acceptance | code + docs | 8, 9 | todo | – |

Stages 1 and 9 do not touch the interface and may run side by side with stages 2–8 (on their own branches); everything
else runs in the order of the table.

## §5 Owner decisions

| # | Decision | Needed by | Options | Status |
| --- | --- | --- | --- | --- |
| D1 | How the interface is styled | U2.1 | (a) plain CSS with custom properties, as the first plan chose (no new dependency); (b) a headless component library (for example Radix primitives) plus our CSS; (c) a full component library | **decided 2026-11-26: (c) a full component library**; which one is D1a |
| D1a | Which component library | U2.1 | Mantine, PatternFly, MUI, Chakra UI | **decided 2026-11-26: Mantine** (`@mantine/core`, `@mantine/hooks`, and further `@mantine/*` packages only where a step names them) |
| D2 | A self-hosted UI font for stable screenshots and a consistent look (for example Inter, OFL licence, served from the app because the CSP allows only `font-src 'self'`) | U3.1 | yes (one font package) / no (system fonts, screenshots tolerate font differences) | **decided 2026-11-26: yes, bundle Inter** (`@fontsource-variable/inter`) |
| D3 | Visual reference for the design | U2.1 | Freelens, Headlamp, the Kubernetes Dashboard, or "none, design from the spec" | **decided 2026-11-26: Headlamp.** Headlamp shows details as full pages, while the spec asks for a details drawer: U2.2 proposes how to reconcile the two (for example a drawer for quick looks and a full page per object) and the owner decides in U2.7 |
| D4 | When the user has exactly one cluster, open it directly instead of showing the home page | U6.1 | yes / no | **decided 2026-11-26: yes**, land on that cluster's overview; the home page stays reachable from the logo and breadcrumb |
| D5 | Add `deadcode` (golang.org/x/tools) to CI so code reachable only from tests fails the build | U1.5 | yes / no | **decided 2026-11-26: no**; U1.3 removes today's leftovers and code review watches for new ones |
| D6 | Keep the demo manifest as `docker-compose.yaml` at the repository root (pilat/devbox needs it there) | U0.2 | yes / move to a separate manifest repository | **decided 2026-11-26: yes**, at the root of this repository |
| D7 | Which ecosystem components the integration environment installs in Stage 9 (each adds minutes to CI) | U9.1 | CRDs + fixtures only; real where cheap; everything real; two tiers | **decided 2026-11-26: two tiers.** Every pull request: CRDs and fixture objects for every module, with metrics-server and Flux running for real. Before a release candidate: a separate job installs every component for real (Karpenter stays CRDs and fixtures) and runs the same tests |
| D8 | Where the interface is designed | U2.0 | in Figma (§3.4) / in Markdown wireframes under `docs/ui/` as the plan first said | **decided 2026-11-26: in Figma** |
| D8a | Where the Figma file lives: which Figma team or project and plan, who owns it, and that the agent's Figma account has edit access (the agent cannot write to a file it can only view) | U2.0 | the owner names the team or project, or asks the agent to create the file in their drafts and move it later | **open** (U2.0 is `blocked` until decided) |
| D8b | Whether to start the component library from a community Mantine kit for Figma or draw it from scratch against the Mantine theme of U2.1. U2.1 checks which kit exists, its licence and whether it is current for the Mantine version in use; nothing is assumed here | U2.1 | community kit adapted / from scratch | **open** (decided with the findings of U2.1) |
| D8c | Whether to map the shared React components to their Figma components with Figma Code Connect, so the design-to-code tools name our components. U3.3 checks that the owner's Figma plan supports it before the step starts | U3.3 | yes / no | **open** (default if the owner does not decide: no, and U3.3 skips the mapping) |

## §6 The user walkthrough

A person who has not used Kubelook before does these in the local demo, without help. Each step that needs a hint is a
defect of the stage that owns that screen.

1. Sign in.
2. Add a Vault token (from `devbox --name kubelook-demo run token`).
3. See which clusters the token gives, and open `demo`.
4. Find the pods of the namespace `shop` and tell which are healthy.
5. Read the logs of `orders`, show only the errors, and pause the stream.
6. Open a shell in `worker` and list `/data`.
7. Open `/data/hello.txt` in the file browser, change it and save it.
8. Edit the ConfigMap `shop-settings`, preview the change and save it.
9. Scale the Deployment `web` to 3, then restart it.
10. Reveal the value of the Secret `payment-gateway`.
11. Find the cluster overview and say whether anything is failing.
12. Find Settings, switch to the dark theme, and replace the Vault token.
13. Sign out.

---

# Stage 0 — Housekeeping

**Status:** in review · **Pull request:** [#42](https://github.com/bdnix/kubelook/pull/42) · **Depends on:** –

**Goal.** Close the first plan properly, commit the local demo, and put this plan in place.

| Step | What | Status |
| --- | --- | --- |
| U0.1 | Archive the first plan with `git mv` to `docs/plans/20261126-021417-plan.md`; repoint the comments that cite its steps (`deploy/helm/kubelook/Chart.yaml`, `hack/docs-check.sh`, `hack/vault-setup.sh`, `hack/it-chartrepo.sh`, `hack/chart-test.sh`) | done |
| U0.2 | The local demo: `docker-compose.yaml`, `hack/demo/` (toolbox image and script, kind config, workloads, Kubelook config), `docs/demo.md`, the README section. Needs D6 | done |
| U0.3 | This plan as `PLAN.md` | done |
| U0.4 | `docs/development.md`: remove the package `internal/overview`, which does not exist, list `certs`, `apperr` and `version`; describe `PLAN.md` and `docs/plans/` | done |
| U0.5 | `AGENTS.md`: "the next pull request" is the first stage of §4 whose status is not `done`; keep the status board up to date as §2 says | done |
| U0.6 | Commit on a branch, push, open the pull request, CI green | in review |

**Acceptance.** `make verify` → `VERIFY OK`; `hack/docs-check.sh` → `DOCS OK`; `devbox --name kubelook-demo up` on the
branch brings the demo up and the walkthrough steps 1–4 work (as far as the current interface allows); CI green.

---

# Stage 1 — Finish what is half done, remove leftovers

**Status:** todo · **Pull request:** – · **Depends on:** 0

**Goal.** No feature is half wired, and no production code exists only for tests.

| Step | What | Status |
| --- | --- | --- |
| U1.1 | **Re-sealed kubeconfig copies reach the browser.** Red: a unit test of `useRestore` (or a new `usePendingCredentials`) that, after `GET /api/v1/credentials/pending` returns `{items:[{label, sealed}]}`, replaces the stored copy of that label in IndexedDB and then calls `POST /api/v1/credentials/pending/ack` with the labels; a UI test in `web/ui/credentials.ui.ts` where the stand-in API returns a pending item after a cluster opens, a reload follows, and the restore sends the new sealed copy. Green: fetch pending items after a cluster open and after restore, store, acknowledge. Decide in the step's test which moment fetches (after every `open` of a kubeconfig cluster, and once after restore) | todo |
| U1.2 | **Integration proof of U1.1.** An OIDC kubeconfig against a kind cluster whose API server trusts the identity stand-in as an OIDC issuer (add `--oidc-issuer-url` and friends to the alpha cluster's kind config; the stand-in already issues ID tokens), with a refresh token the stand-in rotates. The test opens the cluster, forces a refresh, and checks that `GET /credentials/pending` returns a copy that restores in a new session. If the kind API server cannot reach the stand-in's issuer over HTTPS, mark this step `blocked` with the reason and keep U1.1's UI test as the proof | todo |
| U1.3 | **Remove code only tests reach.** For each, delete it and its tests, or show in the pull request that a production caller exists: `cluster.Credentials.VaultToken`, `kube.Access.CanExec`, `kube.Discovery.Has`, `helm4.Adapter.WorkDir`, the droppable queue of `internal/ws` (`Conn.SendDroppable`, `Conn.Dropped`, `queue.pushDroppable`, `queue.evict`, `queue.dropped`) if nothing uses it after review, web `lib/diff.ts` `hasChanges`, `state/csrf.ts` `useCsrf`, `state/theme.ts` `useTheme`, `lib/filter/evaluate.ts` `evaluate` (keep `compileFilter`). Move `web/src/features/helm/fixtures.ts` and `web/src/kinds/testing.ts` under `web/src/test/` | todo |
| U1.4 | **The StorageVersionMigration 404.** Reproduce on Kubernetes 1.37 (the demo cluster): the sidebar offers `storagemigration.k8s.io` StorageVersionMigration under Custom Resources and its list answers 404. Red: a unit test of discovery or the CRD section with the discovery document that cluster serves. Green: list only what the cluster serves at the version it prefers | todo |
| U1.5 | **Dead code check in CI** (needs D5). `hack/deadcode.sh` runs `deadcode` pinned in `hack/install-tools.sh` against `./cmd/...` and fails on any unreachable function outside `internal/testutil` and interface methods listed in an allowlist with a reason each; a `knip`-style check for `web/src` exports (only if D5 extends to web). Added to `make lint` | dropped (D5 = no) |

**Acceptance.** `make verify`, `make test-ui`, `make it` green; `hack/deadcode.sh` prints nothing (if D5 = yes); §3.2
wiring check holds for the pending-credentials endpoints.

---

# Stage 2 — UI design in Figma and its specification (gate)

**Status:** todo · **Pull request:** – · **Depends on:** 0 · **Needs:** D1, D3, D8, D8a

**Goal.** A design the owner approves in Figma before any styling is built, so the stages after it build the same thing
(§3.4). Output: the Figma file, `docs/ui-spec.md` (the written companion with the frame links), referenced from
`docs/spec.md` "UI and UX", and PNG exports of the frames under `docs/ui/`.

| Step | What | Status |
| --- | --- | --- |
| U2.0 | **The Figma file.** Needs D8a. Create or receive the file; name its pages (Foundations, Components, Shell, Screens, Flows); put its link in `docs/ui-spec.md`; check that the agent can write to it (a test frame, then removed). If the agent has no edit access, mark this step `blocked` with the reason | todo |
| U2.1 | **Design direction.** Mantine (D1a) themed after Headlamp's look (D3), with Inter (D2): in Figma the variables (primary colour, grey scale, radius, spacing, font sizes) and text styles; the density (rows per screen on 1440×900); the icon set (Tabler icons, which Mantine uses in its own docs, bundled as React components; CSP forbids remote images; owner confirms the dependency here); the colour tokens for both themes (Figma variable modes) including status colours (ok, warning, error, unknown, pending) with their contrast ratios (WCAG AA). Also decides D8b, and writes the Mantine theme object it implies into `docs/ui-spec.md` as a table of token to value, so U3.2 has one list to implement | todo |
| U2.2 | **The shell.** Frames and sizes: the cluster rail (cluster name or initials with a status dot and a tooltip, grouped by source, favourites first, collapsible to icons), the resource sidebar (collapsible sections, current item marked, module and CRD groups), a top bar (cluster name and role, breadcrumbs, namespace filter, command palette hint, user menu with Settings and Sign out), the banners (where they sit, how they stack), the drawer (resizable width), the dock (collapsible, resizable height, tab bar). Built with auto layout so the resize and collapse behaviour is visible in the file | todo |
| U2.3 | **Components.** Each shared component as a Figma component with variants for its states (default, hover, focus, disabled, loading, error, empty): button (primary, secondary, danger, icon), input, select, checkbox, tabs, table (sticky header, column alignment by type, truncation with tooltip, row selection, sort indicators, row hover, zebra or not), status badge, usage bar, dialog, confirm dialog, menu, toast or inline result, banner, empty state, skeleton loader. Every colour, size and radius uses a variable of U2.1, none a raw value | todo |
| U2.4 | **Page inventory.** One frame per screen and per state (empty, loading, error): sign-in, add credentials (first run), home, cluster overview, resource list, details drawer (each tab), create from YAML, settings, jobs panel, dock (logs, shell, editor), pod files page, Helm releases and release details, Flux, resource map, Karpenter, each module, who-can, permissions, not found, no access. Each frame is 1440×900 (the screenshot viewport of §3.2) and built from the components of U2.3, with realistic data, not lorem ipsum, taken from the demo (`docs/demo.md`). Both themes | todo |
| U2.5 | **Home and navigation.** What the home page shows (clusters by source with status and role, recent clusters, favourites; D4), how a user reaches Settings, jobs and help (keyboard shortcuts), what the browser title says, and the click path to every screen of U2.4, as prototype flows in Figma for the §6 walkthrough and as a table in `docs/ui-spec.md` | todo |
| U2.6 | **The written companion.** `docs/ui-spec.md` as §3.4 describes: link table (frame, URL, node ID), the rules a picture does not show, the token table of U2.1, and a PNG export of every frame in `docs/ui/` named after the frame. Reference it from `docs/spec.md` "UI and UX" | todo |
| U2.7 | **Owner review.** The owner reviews the Figma file (comments on frames) and approves `docs/ui-spec.md` in the pull request. Changes requested are made in the Figma file and re-exported in the same pull request. Open comments in Figma are resolved or answered before approval | todo |

**Acceptance.** The owner approves; every screen of U2.4 and every component of U2.3 has a frame in both themes;
`hack/docs-check.sh` → `DOCS OK`. No application code changes in this stage.

---

# Stage 3 — UI foundation

**Status:** todo · **Pull request:** – · **Depends on:** 2 · **Needs:** D1, D2

**Goal.** The design tokens, base styles, shared components and the layout shell of the Figma file and `docs/ui-spec.md`, and the
screenshot test machinery every later stage uses.

| Step | What | Status |
| --- | --- | --- |
| U3.1 | **Screenshot test machinery.** `web/ui/screens.ui.ts` with a helper `screen(page, name)` that waits for fonts and network idle, disables animations, and calls `toHaveScreenshot` in both themes; `make test-ui-update` regenerates baselines inside the CI Playwright image (`mcr.microsoft.com/playwright:v<pinned>`), so baselines match CI; the `ui-tests` job uploads the diff images when a screenshot differs. Font per D2 | todo |
| U3.2 | **Mantine and the theme.** Add `@mantine/core`, `@mantine/hooks`, `@fontsource-variable/inter` and the icon package U2.1 confirms (the only new dependencies of this stage). `MantineProvider` with the theme of U2.1 in `web/src/theme/` (its values read from the Figma variables with `get_variable_defs` and matched to the token table of `docs/ui-spec.md`; a difference is a defect of one of the two); the light/dark setting of the user drives Mantine's colour scheme (replacing `data-theme` in `theme.css`); jsdom tests render inside a test provider (`web/src/test/render.tsx`). Check that the production build still passes the CSP of the server (no inline scripts; inline styles are allowed) with a UI test under `guardPage`. Red: screenshot of a test page `web/ui/fixtures/kitchen-sink` that renders every component in both themes | todo |
| U3.3 | **Shared components on Mantine.** Re-implement `Button`, `Field`, `Banner`, `StatusBadge`, `FocusTrap` users, dialogs, menus and tabs with Mantine components, keeping their props where callers depend on them so feature code changes little; add `EmptyState` and skeleton loaders as U2.3 specifies. `Table` keeps its virtualised core (1,000 pods under 2 s) with Mantine styling (Stage 4 finishes it). Existing unit tests keep passing or are rewritten for the same behaviour (keyboard, focus, ARIA); screenshot tests for looks. Compare each component with its Figma frame side by side (§3.2.5). If D8c = yes and the owner's plan supports it, map each shared component to its Figma component with Code Connect (the mapping files live in `web/figma/`). Remove the plain CSS the components replace | todo |
| U3.4 | **The shell.** `Layout` on Mantine's `AppShell` with the rail, sidebar, top bar, banners, drawer and dock of U2.2: resizable drawer and dock (sizes saved per browser), collapsible rail and sidebar, the user menu (Settings, Sign out), breadcrumbs. Existing landmarks and accessible names stay, so the existing UI tests keep passing. Screenshot tests: empty shell, shell with a cluster open, with the drawer open, with the dock open | todo |
| U3.5 | **Walkthrough.** §6 steps 1–3 and 12–13 in the demo, with screenshots in the pull request | todo |

**Acceptance.** §3.2 holds for every screen touched; `make test-ui` green with the new screenshots; `make verify` green.

---

# Stage 4 — Lists and tables

**Status:** todo · **Pull request:** – · **Depends on:** 3

**Goal.** Every resource list reads like a table and is fast to scan.

| Step | What | Status |
| --- | --- | --- |
| U4.1 | **Table.** Real table layout from U2.3: fixed header, column widths by column type (name, namespace, status, number, age, usage), right-aligned numbers, truncation with a tooltip, sort indicator, row hover, selection column, keyboard row navigation. The virtualisation of the current `Table` stays (1,000 pods must still render in under 2 s, spec target) | todo |
| U4.2 | **List toolbar.** One row: title with count, namespace filter, quick search, field filter (collapsed into a button that opens the rows editor), saved filters, column presets, export and download in one menu, "Create from YAML". Bulk bar appears only with a selection | todo |
| U4.3 | **Status in lists.** Status badges with colour and text for every kind's status column; restarts and readiness as numbers with a warning colour above a threshold; usage bars for CPU and memory | todo |
| U4.4 | **States.** Loading skeleton, empty ("No pods in the selected namespaces" with the filter named), error (the API's message and a retry), paused watch (from the hidden-tab idle rule) | todo |
| U4.5 | **Screens.** Screenshot tests: pods, deployments, nodes, events, secrets, a custom resource list; each with data, empty and error | todo |
| U4.6 | **Walkthrough.** §6 steps 4 and 9 | todo |

**Acceptance.** As Stage 3, plus the 1,000-pod render test of the first plan still passes.

---

# Stage 5 — Details drawer, dialogs and forms

**Status:** todo · **Pull request:** – · **Depends on:** 4

| Step | What | Status |
| --- | --- | --- |
| U5.1 | **Drawer.** Header with kind, name, namespace, status badge and an actions menu (the current buttons and dialogs move into it; RBAC hiding and the missing-verb text stay); tabs from the Tabs component; resizable; closes with Escape and keeps focus rules | todo |
| U5.2 | **Overview tab.** Sections as cards: metadata (labels and annotations as chips, long values folded), conditions table, containers table (image, state, restarts, ports, usage), owner chain, kind-specific sections (node, workload, Karpenter, Trivy, Vault Secrets Operator) | todo |
| U5.3 | **YAML and editor.** The editor fills the tab; the save bar (dry-run preview, save, discard, "copy as kubectl") is fixed at the bottom; the conflict banner sits above the editor; the diff preview opens as a dialog | todo |
| U5.4 | **Dialogs and forms.** Scale, set image, rollback, taint, expand, confirm, bulk confirm, debug, Helm install and upgrade, create from YAML, credentials (Vault token, kubeconfig), settings: one dialog layout, labelled fields, inline validation, primary action on the right, danger actions in red | todo |
| U5.5 | **Secrets.** The reveal flow and the certificate table in the new layout | todo |
| U5.6 | **Screens and walkthrough.** Screenshot tests for the drawer of a pod, a deployment, a node and a secret (each tab), every dialog; §6 steps 8–10 | todo |

**Acceptance.** As Stage 3.

---

# Stage 6 — Home, navigation and first run

**Status:** todo · **Pull request:** – · **Depends on:** 5 · **Needs:** D4

| Step | What | Status |
| --- | --- | --- |
| U6.1 | **Home page.** Replace the "Choose a cluster" placeholder with U2.5's home: clusters by source with status, role and a direct "Open" (role picker for Vault clusters), favourites, recently opened; D4 | todo |
| U6.2 | **First run.** Signed in with no credentials: a page that explains the two ways (Vault token, kubeconfig) with the forms side by side and what happens to the credential (sealed copy in this browser) | todo |
| U6.3 | **Navigation.** Settings and Jobs in the user menu and the palette; the palette hint (Ctrl/Cmd+K) in the top bar; the keyboard help reachable from the menu; browser titles per page (`Pods · demo · Kubelook`) | todo |
| U6.4 | **Sign-in page, not found, no access, cluster unavailable** in the new layout | todo |
| U6.5 | **Screens and walkthrough.** Screenshot tests of each page; §6 steps 1–3, 11–13 done without hints | todo |

**Acceptance.** As Stage 3, and every screen of U2.4 is reachable by clicking from the home page (§3.2.3), checked by a
UI test that clicks the path to each.

---

# Stage 7 — Dock, logs, shell and files

**Status:** todo · **Pull request:** – · **Depends on:** 6

| Step | What | Status |
| --- | --- | --- |
| U7.1 | **Dock.** Tab bar with kind icons and close buttons, collapse and resize, "pop out" as an icon action; the dock never covers the sidebar | todo |
| U7.2 | **Log viewer.** Toolbar (follow, pause, timestamps, previous, since, tail, sources, level filter, search, download) in one row; source colours; structured-log fields; history loading indicator | todo |
| U7.3 | **Shell.** Terminal fills the tab; container and connection state in the tab; reconnect notice | todo |
| U7.4 | **Files.** The file browser in the drawer and on its page: breadcrumbs, table from Stage 4, actions in a row menu, upload drop zone, editor in a dialog or panel | todo |
| U7.5 | **Screens and walkthrough.** Screenshot tests of each; §6 steps 5–7 | todo |

**Acceptance.** As Stage 3.

---

# Stage 8 — Platform pages

**Status:** todo · **Pull request:** – · **Depends on:** 7

| Step | What | Status |
| --- | --- | --- |
| U8.1 | Cluster overview: cards for node health, failing, pending and not-ready pods, warning timeline, Helm and Flux failure counts, Karpenter summary | todo |
| U8.2 | Helm: releases table, release details tabs, repositories panel, jobs | todo |
| U8.3 | Flux dashboard, details and graph | todo |
| U8.4 | Resource map: legend, colours and icons per kind (U2.1), zoom controls, hover card | todo |
| U8.5 | Karpenter: overview cards, topology treemap, scaling decisions, lists and details | todo |
| U8.6 | Modules: Gateway API, Vault Secrets Operator, KEDA, External Secrets, cert-manager, Trivy, Permissions and policy, who-can | todo |
| U8.7 | Screenshot tests of each page with the stand-in data of `web/ui/fixtures.ts`; walkthrough step 11 | todo |

**Acceptance.** As Stage 3. If this stage grows past what one review can hold, split it at a step boundary into 8a
(U8.1–U8.4) and 8b (U8.5–U8.7), with the owner's agreement, and add the second row to §4.

---

# Stage 9 — Prove the platform features on real systems

**Status:** todo · **Pull request:** – · **Depends on:** 1 · **Needs:** D7

**Goal.** Every feature the spec lists has at least one test against a real system, not only against fakes.

| Step | What | Status |
| --- | --- | --- |
| U9.1 | **Per-pull-request tier (D7).** Into the alpha cluster in `hack/it-up.sh`, pinned by version and digest: **real** metrics-server and Flux (source and kustomize controllers, with a GitRepository or OCIRepository served by the identity stand-in); **CRDs and fixture objects** for cert-manager, KEDA, External Secrets, Gateway API, Kyverno, Gatekeeper, Trivy Operator, the ricoberger Vault Secrets Operator and Karpenter. Fixture objects are copied from what the real controllers write (status, conditions), kept under `test/integration/testdata/ecosystem/`, with the component version they were taken from in a comment. Beta keeps none of them, so the hiding rules stay tested. The integration job must stay within 5 minutes of its current time | todo |
| U9.1b | **Release tier (D7).** `hack/it-up.sh --ecosystem=real` installs every component for real instead (cert-manager with a self-signed Issuer, KEDA with a cron trigger, External Secrets with the fake provider, Kyverno and Gatekeeper with one violating policy each, Trivy Operator scanning one small image, the Vault Secrets Operator against the test Vault, Gateway API with a controller; Karpenter stays CRDs and fixtures because it needs a cloud). A CI job `integration-tests-ecosystem` runs it with the same tests when a release-please pull request is opened, and must pass before a release candidate is published (`docs/releasing.md`). When it shows a fixture no longer matches the real controller, the fixture is updated in the same pull request | todo |
| U9.2 | **API-level integration tests** for: resource map graph (pod → ConfigMap, Secret, PVC; Service → pods; Helm release grouping), cluster overview data, metrics (pod and node usage from metrics-server), Flux dashboard counts and inventory, each module's list and details data, Kyverno and Gatekeeper violations, taints add and remove, set image, CronJob trigger, PVC expand (local-path does not expand: use a StorageClass that allows it, or assert the refusal), bulk delete, restart and label, field filter download, export with formula neutralisation, Helm install from an OCI registry (an OCI chart served by the stand-in or a registry container), node shell (with `nodeShell.enabled`) | todo |
| U9.3 | **Browser tests of the whole stack** (`test/e2e`) for: cluster overview, resource map, Helm releases, Flux dashboard, one module page, the palette, field filter and export | todo |
| U9.4 | Update `docs/spike-findings.md` with what the new tests answer (for example the Trivy rescan behaviour for the four unconfirmed report kinds) | todo |

**Acceptance.** `make it-up`, `make it`, `make e2e` green locally and in CI; CI time of `integration-tests` recorded in
the pull request (and in `docs/ci.md`).

---

# Stage 10 — Demo and documentation refresh, owner acceptance

**Status:** todo · **Pull request:** – · **Depends on:** 8, 9

| Step | What | Status |
| --- | --- | --- |
| U10.1 | The demo gets an optional profile (`devbox up --profile ecosystem`) that installs the components of U9.1 into the demo cluster, so the platform pages have something to show | todo |
| U10.2 | README with screenshots of the main screens (taken from the screenshot baselines), `docs/demo.md` updated for the new interface | todo |
| U10.3 | **Owner acceptance.** The owner does the §6 walkthrough in the demo and lists anything that needs a hint; each item is fixed here or becomes a stage of a next plan | todo |
| U10.4 | Archive this plan as `AGENTS.md` describes | todo |

**Acceptance.** The owner accepts; `make verify`, `make test-ui`, `make it`, `make e2e` green; CI green.
