# Kubelook spec review

Reviewed: `docs/spec.md` as of commit `66940dd` (spec dated 2026-11-09). Review date: 2026-11-10.

Conventions:

- `L123` means line 123 of `docs/spec.md`.
- **[V]** means I checked the claim against a primary source on 2026-11-10. The source is in the verification log (section 10).
- **[U]** means unverified. It comes from my memory or from reasoning, and I did not check it. Treat it as a question, not a finding.
- **[E]** means extrapolation: a conclusion I draw from verified facts, not something a source states.
- I don't know the team size, the timeline, or how the author's Vault is configured. The spec does not say. Where a verdict depends on those, I say so.

---

## 1. Summary verdict

The spec is detailed and mostly coherent at the feature level, but its core security claim (browser-held encrypted copies are "useless without the server's key", L366) is not shown to be true, because the server decrypts any copy a signed-in browser presents (L103) and the spec never binds a copy to a user. Several technical statements are contradicted by primary sources: the Vault creds path needs `update`, not `create` (L62, L401); `kubectl edit` sends a PATCH, not a PUT (L238); Karpenter 1.0 is not supported on Kubernetes 1.35 (L295 vs L415); Helm 3's security support ends on 2026-11-11, two days after the spec date (L244). It is feasible as a product only if scope is cut hard: the delivery plan (L424–429) contains no sizes, no team, and leaves out infrastructure the features depend on (durable jobs, audit, the stream gateway, multi-replica support). The plan also tests the riskiest architectural assumption (per-user watches at 200 sessions per replica) last, in Phase 3, and builds the audit log after the features that claim to be audited. The biggest risks are: the credential custody model, stored XSS from cluster-supplied content in a tool that holds every user's cluster access, and scope.

---

## 2. Decision review

Ordered by cost to reverse later, most costly first.

### D1. Saved credentials live as server-encrypted copies in the browser; active-session credentials sit encrypted in the database (L103, L109)

1. **Decision and reason.** Saved Vault tokens and kubeconfigs are stored only in the browser's IndexedDB, encrypted by the server. Reason: "A database leak alone therefore exposes no saved credentials" (L103).
2. **Strongest case against.** The server is a decryption oracle. L103 says "the browser sends the copies back, and the server decrypts them into the session." The spec never says a copy is bound to the user record, so as written a stolen copy works in any signed-in session. With `auth.restrictSignIn: false` (L41) "any signed-in session" means any Google or GitHub account. The claim "A stolen copy is useless without the server's key" (L366) is then false. Other costs: there is no way to revoke one copy (removing it in settings only deletes the local copy; a stolen one stays valid until the key changes); every sign-in decrypts every saved kubeconfig into the database (L103, L109), so the database does hold long-lived kubeconfig credentials for every signed-in user; Safari deletes script-writable storage, including IndexedDB, after 7 days without user interaction with the site **[V, via search summary of webkit.org]**, so saved credentials silently vanish there; the client-go OIDC provider persists a rotated refresh token after each refresh **[V]**, so the browser copy of an OIDC kubeconfig goes stale after the first refresh unless the server re-issues the copy.
3. **Alternatives.**
   - *Save nothing.* The user pastes the Vault token at each sign-in. Vault tokens are short-lived anyway. Cost: more pasting; kubeconfig users suffer most. Benefit: no ciphertext-at-rest design to get wrong.
   - *Server-side storage with per-user envelope encryption*, bound to the user ID, with a revocation list. Cost: the database now holds saved credentials (encrypted). Benefit: works across browsers, per-item revocation, no IndexedDB eviction problem.
   - *Keep the browser copies but bind them*: user ID and a server-side copy ID as GCM additional data, plus a server-side table of valid copy IDs. Cost: a small table. Benefit: a stolen copy needs the victim's sign-in, and the user can revoke a copy.
4. **What must be true.** (a) A copy is cryptographically bound to one account. (b) A copy can be revoked. (c) Browser storage is durable enough. The spec shows none of these.
5. **Verdict: change.** At minimum bind and make revocable. This is the most expensive decision to reverse because the storage format ships to every browser.

### D2. Sign-in with Google or GitHub, then paste a Vault token or upload a kubeconfig; Vault OIDC dropped (L36–43, L437)

1. **Decision and reason.** "Vault itself has no SSO integration, so the console needs no changes to Vault or Duo configuration" (L437).
2. **Strongest case against.** The identity provider account authorizes nothing (L20). It exists to key settings and to name a person in the audit log. The spec itself admits the two identities are unlinked: "a user could paste someone else's Vault token" (L376). The user must get a Vault token somewhere else first, usually with the Vault CLI or UI. That undercuts the target user at L32 ("fast access from any machine without setting up a kubeconfig"). Pasting a bearer token into a web form is a habit that phishing exploits. A token restricted to the user's source address would fail when used from the console's address **[U: Vault token CIDR binding is from memory]**.
3. **Alternatives.**
   - *The console performs the Vault login* by calling an auth method the org already has (for example LDAP or userpass with Duo through Vault). No token is pasted and no Vault configuration changes. Cost: the console handles the user's Vault password and the MFA step.
   - *Vault token only, no identity provider.* The Vault token's identity keys the user record. Cost: tokens without an identity entity have no stable identity; kubeconfig-only users have none. Benefit: one identity, one step.
   - *Vault OIDC or JWT auth using the same Google identity.* Cost: the Vault configuration change the author wants to avoid. Benefit: identities are linked and nothing is pasted.
4. **What must be true.** Users can already get a Vault token easily on any machine, and Vault admins will not accept any change. The spec asserts the second and does not address the first.
5. **Verdict: needs evidence.** Evidence: how real users obtain a token today, step by step, on a machine without the Vault CLI; and whether the Vault admins would accept the console calling the existing login method.

### D3. Built from scratch, with Headlamp as a reference, not a fork (L448)

1. **Decision and reason.** Kubelook's core differs (Vault sign-in, per-user discovery, Google and GitHub login, a database, the audit log), "so a fork would soon stop taking upstream fixes."
2. **Strongest case against.** Every difference named is in the backend's auth and credential layer. Most of the spec's volume is frontend: lists, details, editors, terminals, logs, CRD views, a map, and eight modules (L181–328). Headlamp already has a backend OIDC flow and a "stateless" mode in which the kubeconfig is kept by the browser, not the server **[V: `backend/cmd/stateless.go`, `frontend/src/stateless/`, `backend/cmd/oidc_handlers_test.go` exist; I did not read their behaviour in depth]**. L448 also says Kubelook has "a database for saved credentials", which contradicts L103, so the stated difference is partly wrong.
3. **Alternatives.**
   - *Headlamp frontend with a Kubelook backend* that speaks the API the Headlamp frontend expects. Cost: coupling to an API that upstream can change.
   - *Fork Headlamp and replace only the credential source*, shipping Kubelook features as Headlamp plugins. Cost: plugin API limits; merge work.
   - *From scratch with a much smaller v1* (see section 7).
4. **What must be true.** Reusing Headlamp's frontend must cost more than rebuilding it. The spec gives no measurement and no team size.
5. **Verdict: needs evidence.** Evidence: a time-boxed spike that puts Vault-minted credentials behind an unmodified Headlamp frontend, and a written list of what broke.

### D4. No authorization inside the console (L18–20)

1. **Decision and reason.** Vault and cluster RBAC stay the only authorities. No reason beyond that is given; it is a stated goal.
2. **Strongest case against.** "No admin role" (L20) leaves operational questions with no owner inside the product: nobody can read the audit log through the console, nobody can end another user's session, and features that are risky for some groups (node shell L210, file browser L223, kubeconfig upload L93) can only be switched for everyone. L148 says the watch timeout "is an app setting, changed on the settings page". With no admin role, either every user can change a global setting, or it is per-user and a user can raise it without limit.
3. **Alternatives.**
   - *Feature flags mapped to identity-provider groups* (not Kubernetes permissions). Cost: a small policy surface.
   - *Operator-only actions through a CLI or the deployment config* (session kill, audit export). Cost: slower incident response.
4. **What must be true.** Nobody ever needs per-group feature limits, a session kill switch, or in-product audit reading. The spec doesn't show this.
5. **Verdict: keep, with gaps closed.** Define who reads the audit log and how, and how a user's sessions are ended.

### D5. Per-user watches, closed 30 seconds after the tab is hidden (L148, L171, L412)

1. **Decision and reason.** One upstream watch per user, cluster, kind and namespace. Implied reason: results are exactly what the user's RBAC allows, and memory is bounded by "the views they are actually looking at" (L171).
2. **Strongest case against.** Nothing is shared between users, so API server load grows with users times kinds times namespaces. Requests from service accounts fall in the `workload-low` priority level by default **[V]**, so Vault-minted identities compete with every in-cluster controller that runs as a service account. A 30-second close means each return to a tab is a full list. If the user was away longer than the API server keeps history, a resume from the old `resourceVersion` returns `410 Gone` and the client must list again **[V]**. When a replica with 200 sessions dies, every watch relists at once **[E]**. "No separate per-user watch cap" (L171) lets one user with many visible windows exhaust a shared replica.
3. **Alternatives.**
   - *Shared informer cache per cluster with per-user access checks.* Cost: the console needs its own cluster credentials, which breaks L368 ("no cluster rights of its own").
   - *Per-user watches with a longer idle time and bookmark-based resume.* Cost: more memory per user.
   - *Per-user watches plus a hard per-user cap and one cluster-wide watch instead of one per namespace when RBAC allows.* Cost: more logic.
4. **What must be true.** 200 sessions per replica (L411) fit in memory and the API servers tolerate the list rate. L175 defers this to "the load test", which is in Phase 3 (L429).
5. **Verdict: keep per-user; the 30-second number and the missing cap need evidence.** Evidence: a load test before Phase 1 ends.

### D6. Audit log in the application database, with an insert-only role on PostgreSQL (L378)

1. **Decision and reason.** Records go to `audit_events`. "Not S3" (L438); no reason is given for rejecting S3.
2. **Strongest case against.** The audit trail shares fate with the system it audits. The spec never says what happens to an action when the audit write fails. The role that runs schema migrations is not mentioned; if the console migrates its own schema at startup, that role can alter `audit_events`. A compromised console cannot change old records but can stop writing or insert false ones, and nothing (sequence numbers, a hash chain) detects a gap. A plain PostgreSQL `GRANT DELETE` cannot express "only records older than N days"; that needs row-level security or a function, and the spec names neither **[U]**. On SQLite there is no protection at all, which the spec admits. Default retention is forever (L382).
3. **Alternatives.**
   - *Also emit each record as a structured log line on stdout.* Cost: almost none. Benefit: the cluster's log pipeline keeps an off-box copy.
   - *Object storage with object lock.* Cost: a dependency. Benefit: real immutability.
   - *Hash-chained records in the database.* Cost: serialized writes. Benefit: gaps and edits are detectable.
4. **What must be true.** Deployers who need a trustworthy trail run PostgreSQL with correctly separated roles, including a separate migration role. The spec states the first and omits the second.
5. **Verdict: change.** Add an off-box sink in v1 and state the fail-closed or fail-open rule.

### D7. SQLite for one replica, PostgreSQL for several; no Redis or Valkey (L102, L151)

1. **Decision and reason.** Reason given to me: Redis licence concerns. The spec text gives no reason.
2. **Strongest case against.** The licence reason does not apply to Valkey, which is BSD 3-Clause **[V]**. Redis 8 itself is offered under RSALv2, SSPLv1 or AGPLv3 **[V]**. Separately, two database engines double the migration and test matrix. SQLite conflicts with the stream gateway: L158 makes the gateway "a separate stream gateway deployment", and a second pod cannot safely share a SQLite file on a volume **[E]**. Logout must close connections held by other replicas and by the gateway (L69); the spec names no cross-replica signal.
3. **Alternatives.**
   - *PostgreSQL only.* Cost: a heavier minimum install. Benefit: one engine, `LISTEN/NOTIFY` for cross-replica signals, real audit roles.
   - *SQLite only, single replica, no separate gateway.* Cost: no high availability. Benefit: simplest install.
   - *Keep both, but in SQLite mode run console and gateway in one process.* Cost: two run modes.
4. **What must be true.** The absence of a cache or message bus must not block any feature. Cross-replica logout and job notifications need some channel; PostgreSQL can provide one, SQLite cannot across pods.
5. **Verdict: keep "no Redis or Valkey", but for the right reason (not needed), and change the SQLite story.** Decide whether SQLite mode has a separate gateway.

### D8. One encryption key from an environment variable; rotation signs everyone out (L104–108)

1. **Decision and reason.** "The console holds one key at a time; there are no old keys and no re-encryption." Reason given to me: simplest rotation.
2. **Strongest case against.** During a rolling restart, replicas with the old key and replicas with the new key run together. Each discards the other's data, so sessions flap until the rollout ends **[E]**. Rotation also ends every shell and editor, which discourages rotation, so the key will in practice never rotate. L108 says "If the key leaked, replacing it at once is the whole response: nothing encrypted with the old key can be used afterwards." That is wrong as incident response: whoever had the key and the ciphertext already has the plaintext credentials, and those stay valid until Vault or the cluster expires or revokes them. L367 admits this for kubeconfigs, contradicting L108.
3. **Alternatives.**
   - *Current key plus one previous key for decryption only*, for a bounded time. Cost: a second variable. Benefit: no sign-out, safe rolling restarts.
   - *Envelope encryption with a key service* (a cloud KMS, or Vault transit). Cost: the console needs its own identity in that service.
   - *Key from a mounted file, reloaded on change.* Cost: small. Benefit: no restart.
4. **What must be true.** Rotation is rare, and a full sign-out with lost shells is acceptable each time. The spec does not say how often rotation is expected.
5. **Verdict: change.** Accept a previous key for decryption. Rewrite L108.

### D9. A separate stream gateway for shells, logs and transfers (L158)

1. **Decision and reason.** "The main backend can be redeployed without ending anyone's shell."
2. **Strongest case against.** L149 says one WebSocket per tab carries "watch events, log lines, exec frames and file-transfer chunks". If that socket ends at the main backend, a backend restart still ends the shell. If it ends at the gateway, watches go through the gateway and the gateway is no longer something that "changes rarely". The gateway is "from the same image", so every security patch and every key rotation restarts it too. It needs the encryption key and the database, so it adds no isolation. A 30-minute drain (L159) needs a pod termination grace period of at least 30 minutes and does not survive node loss.
3. **Alternatives.**
   - *One process with graceful drain.* Cost: deploys end shells after the drain window.
   - *A gateway that owns the exec session and lets a client reattach.* Cost: output buffering and a reattach protocol. Benefit: this is also what "reconnect after a network blip" (L208) and the pop-out dock (L354) need.
   - *Two sockets per tab*, one to each deployment. Cost: two connections and two auth paths.
4. **What must be true.** Backend deploys are frequent and shells are long-lived enough for this to matter. Not shown.
5. **Verdict: needs evidence; defer from v1.** Resolve the L149 and L158 conflict first.

### D10. Long actions are durable jobs, never retried automatically (L160–165)

1. **Decision and reason.** Jobs are recorded before they start; a dead replica's job is marked from cluster state; the user retries. No reason is given; it is conservative on purpose.
2. **Strongest case against.** The job record stores a "values hash" (L160), not the values, so a Retry after a crash has nothing to retry with. A replica cut off from the database but not from the cluster keeps running Helm while another replica marks the job interrupted; the spec has no fencing rule. Queued jobs (L173) run later "under the user's credentials", which may have expired; the spec does not say whether a job survives logout. Rollback "to the starting revision" (L163) does not exist for a first install stuck in `pending-install`. A retry re-runs Helm hooks. A Helm wait longer than the minted token's lifetime fails unless the Helm client refreshes its token **[E]**.
3. **Alternatives.**
   - *No durable jobs: run in the request, report over the socket, lose the action on a crash.* Cost: the user checks cluster state by hand. Benefit: far less machinery.
   - *Durable jobs with stored (encrypted) inputs and fencing tokens.* Cost: more secrets in the database.
4. **What must be true.** Reconciling from cluster state is always possible and safe. It is for Helm's `deployed` state; it is not defined for `failed`, `uninstalling`, or a half-finished uninstall.
5. **Verdict: keep, with the gaps closed.** "Never retried automatically" is right.

### D11. Helm 3 SDK; Karpenter 1.0 and newer; Flux 2.2 and newer (L244, L264, L295, L440, L444)

1. **Decision and reason.** Helm 3 "matches the Helm CLI in use today" (L244). Flux 2.2.2 is the oldest cluster (L440). Karpenter: the `karpenter.sh/v1` API (L444).
2. **Strongest case against.**
   - Helm: the Helm project's Helm 4 announcement says Helm 3 gets "Bug fixes until July 8th 2026" and "Security fixes until November 11th 2026" **[V]**. The spec is dated 2026-11-09. v1 would ship on an SDK with no security support.
   - Karpenter: the Karpenter compatibility matrix lists Kubernetes 1.35 as needing Karpenter `>= 1.9` **[V, AWS provider docs]**. The spec's floor is Kubernetes 1.35 (L415) and it promises testing "against 1.0" (L295). That combination is not supported upstream.
   - Flux: Flux supports "the last three minor releases" **[V]**. Flux 2.2 ships helm-controller v0.37.2 **[V]**, whose HelmRelease has no `.status.inventory`; that field arrived in helm-controller 1.5.0 **[V]**. L267 promises the inventory of "a Kustomization or HelmRelease".
3. **Alternatives.**
   - *Helm 4 SDK now.* Cost: must confirm the Helm 3 CLI in use can still manage releases written by Helm 4 **[U]**.
   - *Shell out to a pinned Helm binary.* Cost: process management, parsing. Benefit: the SDK version follows the CLI exactly.
   - *Floors derived from one rule*: "the oldest version of X that upstream supports on our Kubernetes floor". Cost: none.
4. **What must be true.** Helm 3 must stay safe to embed without upstream security fixes; the fleet must really run Karpenter 1.0 and Flux 2.2 on Kubernetes 1.35.
5. **Verdict: change (Helm, Karpenter); keep with documented degradation (Flux).** The Helm choice needs an owner decision and the Helm 4 interchange evidence.

### D12. YAML edits saved with a full-object update, not server-side apply (L238)

1. **Decision and reason.** "Saving replaces the whole object with an update (`PUT`, as `kubectl edit` does) ... so the saved object matches the editor exactly, including deleted fields."
2. **Strongest case against.** The parenthesis is wrong. `kubectl edit` computes a patch between the original and the edited object and sends a PATCH (strategic merge, or JSON merge for custom resources) **[V]**. A PUT carrying the loaded `resourceVersion` fails on any change to the object, including status updates **[U: that status writes bump `resourceVersion` is from memory]**. Editing a Deployment during a rollout would then conflict repeatedly. A PUT also needs the complete object, while L171 says the backend drops fields, and Secrets arrive masked (L337), so the server must rebuild the full object before every PUT.
3. **Alternatives.**
   - *Patch computed from original to edited*, as `kubectl edit` does. Deleted fields become explicit nulls. Unrelated concurrent changes don't conflict.
   - *PUT with automatic rebase* when the only changes since load are in `status` or `metadata.managedFields`.
   - *Server-side apply with force.* Poor fit for whole-object editing: deleting a field another manager owns does not remove it.
4. **What must be true.** Conflicts on a stale `resourceVersion` must be rare for the objects users edit. Not shown.
5. **Verdict: change, or needs evidence.** Evidence: the conflict rate when saving an edit to a Deployment during a rollout on a real cluster.

### D13. Cluster credentials are not revoked on logout (L69)

1. **Decision and reason.** "It does not revoke the lease: the credentials stay valid until their TTL runs out." No reason is given.
2. **Strongest case against.** `vault.tokenTTL` is unset by default (L88). The plugin then uses the role's default, and if the role has none, Vault's default lease TTL **[V]**. With a role that sets no default, a ClusterRoleBinding created for a signed-out user can live for that whole period. Each re-mint (L112) also leaves the previous service account and binding in the cluster until its lease ends, so objects pile up **[E]**. A reason the spec could have given but didn't: Vault's `default` policy does not grant `sys/leases/revoke` **[V]**, so revoking needs an extra policy line.
3. **Alternatives.**
   - *Revoke on logout when the token's policy allows it; fall back to expiry.* Cost: one optional policy line.
   - *Send a short explicit TTL by default* (for example 15 to 30 minutes), so revocation matters little. Cost: more re-mints.
4. **What must be true.** Role TTLs are short everywhere. The spec does not require or check this.
5. **Verdict: change.** Set a short default `vault.tokenTTL`. Note for the spec: Vault revokes a token's leases when the token expires or is revoked **[V]**, so minted credentials also end when the user's Vault token ends, not only "at their TTL" (L367).

### D14. Vault tokens renewed only when the user clicks Renew (L111)

1. **Decision and reason.** "The console never renews a Vault token on its own." Reason given to me: security risk of automatic renewal.
2. **Strongest case against.** The console already uses the token unattended to re-mint cluster credentials for as long as the session is active (L112). Renewing during an active session adds little risk on top of that, and Vault still enforces the maximum TTL. The cost of manual renewal lands on the on-call user: when the token expires, Vault revokes its leases **[V]** and every Vault cluster drops at once (L114). An open log stream keeps a session non-idle (L115), so "unattended" sessions already exist.
3. **Alternatives.**
   - *Renew automatically only while the user is interacting* (not merely while a stream is open). Cost: a definition of "interacting".
   - *A deployment setting*, default manual.
4. **What must be true.** Token TTLs are long compared with a working session, so Renew is rare. The spec does not state the org's TTLs.
5. **Verdict: needs evidence.** Evidence: the actual TTL and maximum TTL of the tokens users will paste.

### D15. No egress filter in the app; outbound restriction is the deployer's job (L97, L369)

1. **Decision and reason.** Reason given to me: a firewall can do it.
2. **Strongest case against.** User-controlled destinations are wider than L369's "user-supplied server URLs": Helm HTTP repositories and OCI registries "added per session" (L247–248); the OIDC issuer URL in a kubeconfig, which client-go contacts to refresh **[V]**; a per-cluster Prometheus source (L193); and whatever `kubernetes_host` Vault returns (L66). L97 suggests an allowlist of "the cluster API servers, Vault and the database", which makes Helm installs from arbitrary repositories impossible, so the two requirements conflict. The console sits "inside the same network as the clusters" (L150), where internal services and cloud metadata endpoints live. The API proxy returns response bodies to the user, so this is a read-capable SSRF, not a blind one **[E]**.
3. **Alternatives.**
   - *Built-in deny list by default* (loopback, link-local, metadata addresses) plus an optional allowlist of address ranges, with the resolved address pinned for the connection. Cost: small.
   - *A setting to turn kubeconfig upload off.* None exists today.
4. **What must be true.** Every deployer configures egress correctly, including for Helm sources. A product default cannot assume that.
5. **Verdict: change.** Keep the firewall as the main control; add the cheap app-level controls and the off switch.

### D16. Supported deployment is the Helm chart on Kubernetes; `docker run` is only documented (L151)

1. **Decision and reason.** None given beyond "one image and a Helm chart".
2. **Strongest case against.** The tool for fixing clusters runs on a cluster. If that cluster is the broken one, on-call users (L32) lose the console.
3. **Alternatives.** *Support a single-node container deployment* for a break-glass instance. *Document a second instance on another cluster.*
4. **What must be true.** The hosting cluster is more available than the clusters being operated. Not discussed.
5. **Verdict: keep.** Add the break-glass question to the spec.

### D17. Exclusions: no port-forwarding, no cross-cluster search, no keystroke recording, no AI assistant (L25–26, L211, L332, L439–442)

1. **Decision and reason.** Port-forward: no TCP tunnels (L372). Recording: "the audit log holds actions only" (L211). Search and AI: no reason given.
2. **Strongest case against.** Some compliance regimes expect privileged session recording; the spec doesn't say which regime applies, so I can't judge. Cross-cluster search is the feature that most helps "many clusters" users (L31).
3. **Alternatives.** *Optional recording set by the deployer, off by default.* *Cross-cluster search limited to names, on demand, with no standing watches.*
4. **What must be true.** No compliance requirement for session content. Not stated.
5. **Verdict: keep all four for v1.** Record the compliance question as open.

### D18. Other decisions the spec makes

- **Vault mounts are the cluster registry, found through an internal endpoint (L55, L60, L436).** The behaviour is as described **[V, source]** and Vault's docs say there is "no guarantee on backwards compatibility" **[V]**. The spec already flags this. Alternative: a configured cluster list checked with `sys/capabilities-self`, which is a public endpoint and accepts many paths in one call **[V]**. Verdict: keep, add the configured list as a fallback.
- **Kubernetes 1.35 floor (L415).** 1.35 is the oldest release still supported upstream today; its end of life is 2027-02-28 **[V]**. The spec has no rule for moving the floor. Verdict: keep, add the rule.
- **`vault.clusterRoleBinding: true` by default (L86).** Every minted credential is cluster-wide. A Vault role of type `Role` fails with "a ClusterRoleBinding cannot ref a Role" **[V]**, and the setting is global, so one install cannot mix role types. Verdict: change to a per-role setting; the default is an owner decision.
- **Own netshoot-based debug image, digest-pinned (L218).** Pinning is right. Missing: signing, vulnerability scanning, and what happens in clusters that cannot pull from `ghcr.io`. Verdict: keep.
- **Secret values stripped server-side on every route (L335).** Right approach; the route list is incomplete (section 5, Security).

---

## 3. Pros

- **Kubernetes calls use the user's own credential; the console has no cluster rights (L18, L368).** Strong because a console bug cannot grant more than the user's RBAC. Stops being strong if a shared cache or a console service account is added later for performance.
- **`exec` credential plugins and file-path references are rejected (L96).** Strong because it removes server-side command execution and local file reads from kubeconfig parsing. Stops being strong if any other kubeconfig field that makes the server read a file or call a URL is accepted; the OIDC provider's `idp-certificate-authority` (a file path) and `idp-issuer-url` exist **[V]** and L96 does not mention them.
- **File browser commands pass arguments separately, never through a shell line (L231).** Strong because file names cannot inject commands. Stops being strong the moment a fallback needs redirection to write a file (see C16).
- **Interrupted jobs are never retried automatically (L165).** Strong because Helm hooks and drains are not idempotent. Stops being strong if "Retry" hides what will be re-run.
- **Debug image pinned by digest per release (L218).** Strong because the image cannot change under a running install. Stops being strong if deployers override `debug.image` with a tag.
- **The spec names its own uncertainty.** The internal Vault endpoint is flagged (L60), numbers are marked "proposed", and a gate tests discovery first (L431). Stops being strong if the gate passes on one policy shape only.
- **Drains use the eviction API (L344).** Strong because PodDisruptionBudgets are respected. Stops being strong without a timeout for evictions that are refused indefinitely.
- **Log history limits are stated honestly (L201) and are correct.** Only the latest log file is served and the default rotation size is 10 Mi **[V]**.
- **Users are keyed by provider and stable ID, not email (L42); `hd` is checked from the ID token.** Matches Google's guidance **[V, via search summary]**. Stops being strong if the email is used anywhere as a key.
- **CSV exports neutralise formula cells everywhere (L341).** Strong because resource names are attacker-controlled.

---

## 4. Cons

| # | Severity | Spec line | Weakness | Concrete failure scenario |
|---|---|---|---|---|
| C1 | Critical | L103, L366 | The server decrypts any saved copy a signed-in browser sends; no binding to a user is specified | An attacker copies a victim's IndexedDB blob (XSS, shared machine, disk image). They sign in with their own account, load the blob, and the server decrypts the victim's kubeconfig into the attacker's session. With `restrictSignIn: false` any Google account will do. |
| C2 | Critical | L366, L200, L246, L333 | Cluster-supplied content is rendered widely; the only XSS defence named is "a strict CSP", undefined | A user with write access to one namespace sets an annotation, log line, Helm note or CRD field that triggers a rendering bug. A platform engineer views it. The script calls the reveal, exec and Helm APIs in the victim's session on every cluster the victim can reach. |
| C3 | High | L62, L401 | Discovery treats `create` as enough on the creds path. The path only handles update, and with no existence check Vault treats the write as update | A policy grants `create` only on `kubernetes/prod/creds/devops-role`. The cluster shows in the sidebar. Opening it returns permission denied from Vault. |
| C4 | High | L244 | Helm 3 SDK; upstream security fixes end 2026-11-11 | A Helm 3 template or archive vulnerability is published in 2027. No fixed Helm 3 release exists. Kubelook renders user-chosen charts in a process that holds every session's credentials. |
| C5 | High | L429 vs L211, L186, L232 | The audit log is built in Phase 3; features that "are audited" ship in Phases 1 and 2; the security review gate comes before it | Phase 1 is deployed for a pilot. Secrets are revealed and shells opened with no record. The penetration test at Gate 2 cannot test audit completeness because the audit log does not exist yet. |
| C6 | High | L108 | Says key replacement is "the whole response" to a leaked key | The key and a database dump leak. The operator rotates the key and stops. The attacker already decrypted Vault tokens and kubeconfig certificates, and uses them directly against Vault and the clusters until they expire. |
| C7 | High | L335–340 | Secret masking list misses routes | (a) A Secret created with client-side `kubectl apply` carries its full content in the `kubectl.kubernetes.io/last-applied-configuration` annotation **[U]**; the YAML view shows annotations. (b) Helm can store releases in ConfigMaps **[V]**; the ConfigMap view then shows release data that L339 hides only for Secrets. (c) HelmRelease `spec.values` is shown as an ordinary custom resource. |
| C8 | High | L171, L174 | No per-user cap on watches; one shared replica | One user opens all-namespace lists of several large kinds on many clusters in side-by-side windows. The replica passes 80% memory and refuses new work for all 200 users. Sticky routing sends their retries to the same replica. |
| C9 | High | L115 | Session idle needs "no interaction and no open stream"; there is no absolute session lifetime | A user leaves a following log stream open in a forgotten tab. The session never idles. The backend re-mints cluster credentials for days, until the Vault token itself expires. |
| C10 | High | L295 vs L415 | Karpenter 1.0 is promised on a Kubernetes 1.35 floor | Release testing "against 1.0" cannot be run on any supported cluster: Kubernetes 1.35 needs Karpenter 1.9 or newer **[V]**. The test is skipped or run on an unsupported pair. |
| C11 | High | L238 | Full-object PUT with the loaded `resourceVersion` | A user edits a Deployment's resources during a rollout. Each status write changes the `resourceVersion` **[U]**. Every save returns a conflict; the banner at L239 loops. |
| C12 | High | L149 vs L158 | One WebSocket per tab carries exec frames, yet shells run in a separate gateway | The main backend is redeployed. The tab's single socket ends. The shell ends, although L158 says it would not. |
| C13 | Medium | L69, L88 | No revoke on logout and no default TTL sent | A Vault role has no `token_default_ttl`. A user signs out. The service account and ClusterRoleBinding stay for Vault's default lease TTL. |
| C14 | Medium | L55, L66 | Assumes `config` returns a usable URL and CA | Vault runs inside cluster A and the mount was configured without `kubernetes_host`. The read returns only what the admin set **[V]**, so the host is empty and the cluster cannot be opened. L66 assigns this to the deployer but L55 states the return as fact. |
| C15 | Medium | L60 | Vault namespaces are not mentioned | The cluster mounts live in a Vault Enterprise child namespace. The endpoint returns only mounts in the request's namespace **[V]**. There is no namespace setting, so discovery returns nothing. |
| C16 | Medium | L228, L231 | The single-file fallback lists `ls`, `cat`, `stat`, `head`, `rm` and forbids shell command lines | A container has no `tar`. The user uploads a file. None of the listed tools can write a file without shell redirection, so upload cannot work as written. |
| C17 | Medium | L225 | Container picker "includes init containers" | The user picks a finished init container. `exec` needs a running container **[U]**, so the Files tab errors for almost every init container. |
| C18 | Medium | L209, L217 | Each debug action adds an ephemeral container | Ephemeral containers cannot be changed or removed **[V]**. If the image cannot be pulled (private cluster, no route to `ghcr.io`), the failed container stays in the pod until the pod is deleted. Ten clicks leave ten entries. |
| C19 | Medium | L267, L269, L266 | Flux claims that do not hold for every kind or version | (a) HelmRelease inventory on Flux 2.2: the field does not exist **[V]**. (b) Suspend by `spec.suspend` on ResourceSet or FluxInstance: I found no such field in the flux-operator API docs; they use the `fluxcd.controlplane.io/reconcile` annotation **[V]**. (c) "Ready, failed" counts for Alerts: the v1beta3 Alert type has no status **[V]**. |
| C20 | Medium | L343 | UI visibility driven by `SelfSubjectRulesReview` | The API's own documentation says the result "may be incomplete depending on the server's authorization mode" **[V]**. On a cluster with a webhook authorizer, a kubeconfig user is allowed to delete pods but the button is hidden. |
| C21 | Medium | L378 | The delete-only cleanup role is described as able to delete only old records | PostgreSQL table privileges alone cannot limit deletes by age **[U]**. If implemented with a plain grant, a stolen cleanup credential deletes the whole audit table. |
| C22 | Medium | L160, L165 | Jobs store a values hash, not the values | A replica dies during a Helm upgrade. The user clicks Retry on another replica. The edited values exist nowhere, so the retry cannot run or runs with wrong values. |
| C23 | Medium | L355 | "This cluster was uploaded by another user" | Kubeconfigs are not stored server-side (L103). The server cannot tell another user's upload from a name that never existed, so this message cannot be produced as written. |
| C24 | Medium | L306 | "While it is active its kinds are not repeated under Custom Resources" | The KEDA module shows ScaledObjects only (L312). If "its kinds" means the API group, ScaledJobs and TriggerAuthentications become unreachable anywhere in the UI. |
| C25 | Medium | L97 vs L247 | Recommended egress allowlist conflicts with Helm installs from arbitrary repositories | The deployer follows L97. A user adds a public Helm repository. The request is blocked; the feature at L247–248 is dead. |
| C26 | Medium | L86, L85 | Global `clusterRoleBinding` and a site-specific default namespace `itops` | A new install keeps defaults. Unless the Vault roles allow a namespace named `itops`, every mint fails with "kubernetes_namespace 'itops' is not present in role's allowed_kubernetes_namespaces" **[V, error text from source]**. |
| C27 | Medium | L181–191 | Built-in kinds outside the table have no view | ValidatingAdmissionPolicy, RuntimeClass, CSIDriver, CertificateSigningRequest and APIService are not CRDs and are not in the table. The "Custom resources" browser covers "every served CRD" only. |
| C28 | Medium | L187 | Endpoints listed as a first-class kind | The v1 Endpoints API has been deprecated since Kubernetes 1.33 **[V]**, below the spec's own 1.35 floor. Every list returns a deprecation warning. |
| C29 | Low | L179 vs L193 | "Every list and detail view is backed by a live watch" | metrics-server implements get and list only **[V]**. Metrics must be polled. |
| C30 | Low | L376 | "This is the only place the person, the cluster and the action appear together" | The generated service account name contains the first 8 characters of the Vault token's display name and of the role name **[V]**, so the cluster's own audit log carries part of the identity. The claim is overstated. |
| C31 | Low | L96 | "This rules out the kubeconfigs that EKS, GKE and AKS generate" | Not checked for AKS; AKS clusters with local accounts may produce certificate-based kubeconfigs **[U]**. |
| C32 | Low | L316 | Gatekeeper "violations" | Gatekeeper stores at most 20 violations per constraint by default **[V]**. The view shows a truncated list unless it says so. |

---

## 5. Deep gap analysis

### 5.1 Authentication and sessions

Missing:

- OAuth `state` and nonce handling, and where the PKCE verifier is kept. PKCE is named (L40) and GitHub's OAuth flow does support it with `S256` **[V]**.
- Session ID rotation after sign-in and after a credential is added.
- An absolute session lifetime. Only an idle timeout exists (L115).
- Re-checking allowlist membership. A user removed from the GitHub organization keeps their session until it idles. With an open stream it never idles (C9).
- How the CSRF token reaches the browser, and whether the WebSocket channel has any per-message protection beyond the Origin check at the upgrade.
- The external URL setting. The Origin check (L373) and the OAuth redirect both need it. None is defined.
- Behaviour when the identity provider is down (existing sessions? new sign-ins?).
- Behaviour when the database is down. Sessions and audit are in it, so I expect every request to fail **[E]**; the spec has a section for Vault being down (L71–76) and none for the database.
- Multi-tab rules: sign-out in one tab; pasting a new token in one tab while another has open shells; two browsers for one user (two sessions, each minting its own credentials?).
- GitHub organizations that restrict third-party OAuth apps or require SAML: whether membership is then visible to `read:org` **[U]**.

Ambiguous:

- "No interaction" (L115): does typing in an editor that has not saved count?
- "Sticky" WebSockets (L120): sticky by cookie, or just a long-lived connection? If a REST call creates a watch that is delivered on the socket, the REST call must reach the same replica.

Contradictory: see section 6.

### 5.2 Vault integration

Verified as written:

- `sys/internal/ui/mounts` is in the system backend's unauthenticated path list. With a token it returns mounts where the token has any non-deny capability on a path under the mount **[V]**.
- The `default` policy grants `auth/token/lookup-self` (read), `auth/token/renew-self` (update) and `sys/capabilities-self` (update) **[V]**.
- The sample creds response shows `renewable: false` **[V]**.
- `cluster_role_binding` requires `kubernetes_role_type=ClusterRole` **[V]**.
- The maximum TTL "cannot be displayed when a token's information is looked up", and a client should watch for a TTL that stops extending **[V]**.

Contradicted:

- L62 and L401: `create` is not sufficient on the creds path (C3). Vault's own policy examples use `update` **[V]**.

Missing:

- Vault versions and editions supported. None is stated. The API link at L67 is pinned to v1.16.x.
- Vault namespaces (C15), a CA bundle setting for Vault's TLS certificate, and request timeouts and retry limits.
- Token types. Batch tokens cannot be renewed **[V]**, so the Renew button never applies. A TTL of 0 means the token never expires **[V]**; the expiry banner logic must handle it. Use-limited tokens would be consumed by discovery **[U]**. Root tokens: should the console refuse them?
- The role's kind. With `service_account_name` roles, Vault creates a token for an existing service account and creates no objects **[V]**. L69 ("Vault then removes the service account objects it created") does not apply, and the cluster audit log shows a shared account, not a temporary one (L376).
- The number of Vault calls at sign-in for "100+" clusters (L414). `sys/capabilities-self` accepts a list of paths **[V]**; the spec describes a call per mount (L61).
- What the console does when the minted token's real lifetime is shorter than the lease. The plugin caps the lease to the token's lifetime and adds a warning **[V]**.
- The effect of Vault token expiry on minted credentials (leases are revoked with the token **[V]**).
- The console holds a token that can do everything the user's policies allow, not only Kubernetes paths. L445 is a promise about console behaviour, not a bound. Nothing narrows the token.
- Rate of object creation in clusters: each mint with a role-based Vault role creates a service account and a binding **[V]**; each re-mint adds another until the old lease ends.

Ambiguous:

- "Vault identity" (L376, L378): display name, entity ID, or accessor? Tokens without an entity exist.
- `vault.candidateRoles` is global. Do role names repeat across every cluster?

### 5.3 Credential storage and cryptography

Missing:

- Binding of ciphertext to a user and purpose (additional authenticated data). See C1.
- Revocation of a single saved copy.
- Key format and length checks at startup, and how the key ID is derived (L105).
- Mixed-key behaviour during a rolling restart (D8).
- Whether saved kubeconfigs are decrypted at sign-in (L103 says yes) or when a cluster is opened. Decrypting at sign-in puts every saved long-lived credential in the database for every active session.
- Zeroing or lifetime of plaintext in replica memory; scrubbing of tokens from logs and OpenTelemetry traces (L417).
- Nonce policy beyond "random" (L105). Random 96-bit nonces under one long-lived key have a usage limit **[U: the NIST figure is from memory]**. At this system's volume I expect it not to be reached **[E]**, but the spec should say it was considered.

What each compromise exposes, as the spec is written **[E]**:

- Database only: ciphertext of active sessions. Nothing readable.
- Database plus key: every active session's Vault token, kubeconfig credentials and minted credentials.
- One browser's storage: that user's saved copies, usable through the server if copies are not bound (C1).
- A replica or the gateway: everything above, in memory, for the sessions it serves. The spec says so (L362).
- An XSS bug: it cannot read plaintext saved credentials, but it can read and exfiltrate the encrypted copies, and act as the user through the API: reveal Secrets, open shells, run Helm. The CSP is the only named control and is not specified (C2).

### 5.4 Kubernetes behaviour

- **Watches and `resourceVersion`.** L148 says both "resumed from the last `resourceVersion`" and "the list reloads". `410 Gone` is not mentioned; the documented client duty is to clear the cache and list again **[V]**. The API server's default compaction interval is 5 minutes **[V]**. Bookmarks, list pagination and streaming lists are not mentioned. Where the last `resourceVersion` lives during failover (L156) is not said: the new replica has no cache.
- **Events and logs.** The 1-hour event TTL (L299) and log rotation facts (L201) are correct **[V]**. Missing: a cap on the number of pods in a merged log view (L198), and what "server-side filter" (L199) means, since the filtering can only happen in Kubelook's backend after it has pulled the lines **[E]**.
- **Update versus apply.** See D12. Also missing: a warning when the edited object is managed by Flux or Helm and will be reverted; how "multi-document apply" (L240) orders documents and reports partial failure, and whether it is an apply at all.
- **Dry run.** A webhook that does not declare `sideEffects` as `None` or `NoneOnDryRun` makes the dry-run request fail **[V]**. L237 puts a dry run before every save and does not say whether the user can still save.
- **Eviction and drain.** Missing: DaemonSet pods, pods with `emptyDir` data, pods without a controller, grace period, timeout, and the end state of an interrupted drain (the node stays cordoned **[E]**).
- **Ephemeral containers.** They cannot be removed, cannot set resources or ports, and are not supported on static pods **[V]**. L209 gates on `pods/ephemeralcontainers` only; opening the shell also needs exec or attach rights **[U]**. `kubectl debug` profiles are `legacy`, `general`, `baseline`, `restricted`, `netadmin`, `sysadmin` **[V]**; Pod Security admission may reject the stronger ones **[U]**.
- **Node shell (L210).** Missing: the namespace of the privileged pod, its image, and cleanup when the session or replica dies.
- **`exec` limits.** From Kubernetes 1.35, WebSocket `exec`, `attach` and `portforward` require `create`, where they used to require `get` **[V]**. The UI's permission check for shells must test `create`. Missing: a per-user shell cap; what "reconnect after a network blip" (L208) means, since the spec says a shell "can't survive the connection that carries" it (L157).
- **API deprecations.** Endpoints (C28). No process for handling API removals in "copy as kubectl" output.
- **Version floors.** D11 and C10. No rule for moving any floor. No stated skew policy between the client-go version built in and the clusters served.
- **Discovery.** Module detection should not need permission to list CRDs. Users with namespaced rights often cannot list namespaces; the namespace picker has no manual entry described.

### 5.5 Concurrency, scale and failure

Numbers marked "proposed", and whether each can be set without measurement:

- Policy choices, can be set by decision: `vault.expiryWarning` 30m (L89); session idle 30 min (L115); shell idle 15 min (L370); `streams.drainTimeout` 30m (L159); `fileBrowser.maxTransferBytes` 25 MiB (L397); `audit.retentionDays` (L382).
- Cannot be justified without measurement: 200 sessions per replica (L411); `memory.refuseNewWorkPercent` 80 (L174); first render under 2 s for 1,000 pods (L409); event to UI under 1 s (L410); `helm.maxConcurrentJobs` 5 (L173); `helm.maxChartBytes` 10 MiB as a memory bound, because rendered size is not chart size (L173); 10,000 live log lines and `logs.historyMaxLines` 200,000 as browser memory bounds (L203, L398); 99.9% availability (L413).
- Not marked "proposed" but equally unmeasured: the 30-second watch idle time (L148, L412).

Missing:

- A definition of "available" for the 99.9% target. The console depends on Vault, the identity provider, the database and each API server.
- A readiness signal when a replica refuses new work (L174). The load balancer keeps sending traffic to it.
- Autoscaling, or any statement that replicas are sized by hand.
- Lease length, clock source and fencing for jobs (D10).
- Helm states beyond the three listed (L161–163): `failed`, `uninstalling` **[V: both exist]**, half-finished uninstall, first-install `pending-install` with no revision to roll back to.
- Memory cost of listing Helm releases across namespaces (L245), which means reading every release object **[E]**.
- `GOMEMLIMIT` covers memory managed by the Go runtime and excludes memory "managed in other languages" **[V]**. If SQLite is linked through C, its memory is outside the limit **[E]**.
- Thundering herd on replica loss (D5).

### 5.6 Security

- **Threat table (L364–374).** Missing rows: malicious content from a cluster (C2); a malicious or oversized API server response from a user-supplied kubeconfig (memory exhaustion of a shared replica); chart rendering as untrusted computation; identity provider account takeover; the operator as an insider (the operator can read the key and the database); denial of service by one user; supply chain of the console image itself; terminal escape sequences from container output.
- **Secret masking.** See C7. Also not covered: the dry-run diff of a Secret (L237); a Secret reveal followed by "copy as kubectl" (L240); whether values are stripped before objects enter the replica's memory or only before they leave it. L335 says the reveal is "the only way to see a value", which L232 contradicts for Secret volumes.
- **File browser.** C16 and C17. Also: parsing `ls` output is unreliable for unusual file names **[U]**; the "file changed since opened" check (L227) is a check-then-write across separate `exec` calls and can race; every directory view is an audit record (L232), which will dominate audit volume.
- **Debug image.** Pinned by digest, good. Missing: signature, SBOM, scan policy, and behaviour when the image cannot be pulled (C18). The pipeline rebuilds "whenever a new netshoot release appears" (L218) with no human review step stated. netshoot's latest tag is v0.16 and it is Apache 2.0, as the spec says **[V]**.
- **SSRF.** See D15. No setting disables kubeconfig upload. The settings tables list `fileBrowser.enabled` and `modules.*.enabled` only.
- **Audit.** See 5.7.
- **Privilege escalation paths.** Cluster-wide binding by default (L86); pasting another person's token (L376); the console's token can reach all of the user's Vault paths (5.2); the stream gateway holds the same key and database access as the backend (D9); orphaned privileged node-shell pods; `restrictSignIn: false` combined with unbound copies (C1).
- **Console pod hardening.** Not mentioned: whether the pod mounts a service account token, runs as non-root, or has a read-only root filesystem. L68 says "Nothing is written to disk"; whether the Helm SDK writes chart or repository caches to disk is unchecked **[U]**.

### 5.7 Data and audit model

Attribution:

- Vault path: the record has the identity provider account and "the Vault identity" (undefined, 5.2).
- Kubeconfig path: there is no Vault identity. The spec does not say what is recorded. The kubeconfig's user name is chosen by the uploader.
- With `restrictSignIn: false`, the "person" is any Google or GitHub account.

Missing from the audit list (L376):

- Failed sign-ins and allowlist refusals.
- Adding, removing and renewing credentials (token paste, kubeconfig upload, Renew, removal of a saved copy).
- Opening a kubeconfig cluster (Vault clusters are covered by "every Vault credential issued").
- Role selection (L62).
- Reads: viewing YAML, listing Secret keys, viewing ConfigMaps. Only reveals, certificate views and logs are covered.
- Exports and downloads of resource lists (L333, L341).
- Settings changes, including the "app setting" at L148.
- Job retries and rollbacks are said to be audited (L165); job state changes made by another replica are not mentioned.
- Source address, user agent, session ID and a request ID. The column list (L378) has none.
- Denied actions (the "outcome" field covers mutating calls only).

Other gaps:

- No sequence number or chain to detect missing records.
- No way to read the audit log is described. No export exists until the "planned" Elasticsearch option.
- Retention default is forever (L382); on SQLite the volume fills. The behaviour when an audit write fails is not defined.
- The audit log holds personal data (account identifiers, activity). No privacy or access rule is given.
- No correlation with the cluster's own audit log is proposed, although the client's user agent could carry a session reference **[E]**.

### 5.8 UX and product

- **Deep links (L355).** The form `/c/<cluster>/ns/<ns>/<group>/<kind>/<name>` has no defined value for the core group (empty group name), covers objects only although "every view has a stable URL", and does not say how the link survives the sign-in redirect. Kubeconfig cluster names include a user-chosen file label, so links are personal. C23.
- **Preferences.** "App setting" versus "user preference" versus "deployment setting" is never defined. L148 needs an owner.
- **Session or token expiry.** No rule for unsaved editor content when the session idles out. L113 keeps terminals open when a new token is pasted; nothing is said about a session that ends.
- **Error messages.** A handful are written out (L74, L106, L355). Missing: Vault permission denied at mint, the role-type mismatch, the namespace mismatch, an unreachable API server, an untrusted certificate.
- **Accessibility (L356).** No target standard. The force-directed map (L289) and the treemap (L298) have no non-visual alternative. Pod state is shown by colour (L289).
- **On-call.** L32 says "from any machine"; L357 excludes phones.
- **Features that cannot be built as written:** C12 (shell survives backend redeploy over one socket), C16 (upload fallback), C17 (init containers), C19 (three Flux claims), C23 (the "another user" message), C29 (every view watched), L208 with L157 (shell reconnect), L354 pop-out dock keeping a live shell in a new window when a shell is bound to one tab's socket **[E]**, C10 (Karpenter 1.0 test), C21 (age-limited delete by role alone **[U]**).

### 5.9 Operations

Missing:

- Schema migrations: who runs them, with which role, on two engines, during a rolling upgrade, and how to roll back.
- Version skew between the console and the gateway during an upgrade (same image, separate rollouts).
- Backup and restore. A restored database contains old sessions; whether they come back to life is not said.
- Configuration validation at startup: a missing key, a missing allowlist, an unreachable Vault.
- Health and readiness endpoints.
- A complete configuration reference. The app "reads its config from environment variables and a YAML file" (L418); two variables are named.
- Alerts. Metrics are listed (L417); no alert or service-level indicator is.
- Ingress requirements: WebSocket timeouts, body size for uploads, TLS.

Unsafe or surprising defaults:

- `vault.clusterRoleBinding: true` (L86).
- `vault.credsNamespace: itops` (L85), a site-specific value as a product default.
- `vault.tokenTTL` unset (L88), see C13.
- `fileBrowser.enabled: true` (L396).
- `audit.retentionDays: 0` (L382).
- Kubeconfig upload always on, no setting.
- `debug.image` pulled from a public registry by every cluster (L393).

### 5.10 Ecosystem modules

- **Karpenter (L295–302).** `expireAfter` is a NodePool field in the v1 API **[V]**. NodeOverlay is `karpenter.sh/v1alpha1`, alpha, behind a feature gate **[V]**; the module must tolerate its absence. `EC2NodeClass` on AWS: not separately checked. `AKSNodeClass` exists in the Azure provider **[V]**. `GCENodeClass` and `GCECustomMachineType` exist in the cloudpilot-ai GCP provider **[V]**; that provider is a third-party project, which the spec does not say. The floor conflict is C10.
- **KEDA (L312).** The pause annotations are `autoscaling.keda.sh/paused` and `autoscaling.keda.sh/paused-replicas`, with different effects **[V]**. The spec does not say which one "Pause" uses. ScaledJobs and TriggerAuthentications have no view (C24).
- **External Secrets (L313).** Force sync by changing an annotation is documented (`force-sync`) **[V]**. The repository has both `v1` and `v1beta1` API packages **[V]**; the spec's "reads the versions each cluster serves" rule is stated only for Flux (L264).
- **cert-manager (L314).** Not checked. The module offers edit and delete only; no renew action.
- **Gateway API (L310).** GRPCRoute is a `v1` storage version **[V]**. "Every kind served by the groups" is generic and safe.
- **Trivy Operator (L315).** The seven report types named exist, plus cluster-scoped variants (twelve CRDs in the repository) **[V]**. "Rescan (deletes the report so the operator scans again)": I could not confirm this behaviour from the operator's docs.
- **Kyverno.** Not checked. The spec does not say where violations are read from.
- **Gatekeeper.** Violations on a constraint are capped at 20 by default **[V]** (C32).
- **ricoberger Vault Secrets Operator (L311, L321–326).** Confirmed from source: the `SecretCreated` condition, `observedGeneration` on it, the default `Replace` strategy, engines `kv` and `pki`, a Secret with the same name as the VaultSecret, and the `created-by: vault-secrets-operator` label (README) **[V]**. The API group string `ricoberger.de` appears in the CRD file name **[V]**.
- **Flux (L250–270).** `reconcile.fluxcd.io/requestedAt` is the documented trigger **[V]**. ArtifactGenerator suspends with `source.extensions.fluxcd.io/reconcile: Disabled` **[V]**. The kind list matches the Freelens extension README, which also states "Flux >= 2.7" for ExternalArtifact and ArtifactGenerator **[V; this is the spec's own secondary source, not Flux's docs]**. Forced HelmRelease reconciles use `forceAt` and `resetAt` as well **[V]**; the spec offers only `requestedAt`. Problems: C19.
- **Permissions (L316).** "Ranked by risk level" has no definition. The module needs no CRDs, so the rule at L306 does not fit it.

### 5.11 Missing spec sections

- Success criteria and how they are measured.
- Test strategy, including the matrix: Kubernetes versions, Vault versions, Flux and Karpenter versions, two database engines, four browsers.
- A risk register and an open-questions list.
- Team, estimates and timeline.
- Supported Vault versions and editions.
- A threat model with trust boundaries and data flows (the table at L364 is a list of mitigations).
- The API and WebSocket protocol between browser and backend.
- The data model (tables, session lifecycle, job states).
- A full configuration reference.
- Upgrade, compatibility and deprecation policy.
- Privacy and data retention.
- A pilot or rollout plan and a definition of done per phase.
- A glossary ("app setting", "Vault identity", "module", "source").

---

## 6. Internal consistency check

Places where two parts of the spec disagree:

1. L148 "resumed from the last `resourceVersion` on reconnect" vs L148 "the list reloads and goes live again" (and L156 "Watches reopen ... from the last `resourceVersion`").
2. L149 one WebSocket per tab carries exec frames, logs and file chunks vs L158 shells, logs and transfers run in a separate deployment so a backend redeploy does not end them.
3. L102 and L151 SQLite for a single replica vs L158 a separate stream gateway deployment that needs the same sessions.
4. L103 saved credentials "are not stored in the database" vs L448 "a database for saved credentials and settings".
5. L103 kubeconfigs are not stored server-side vs L355 "This cluster was uploaded by another user".
6. L108 replacing the key "is the whole response" vs L367 a stolen copy with the key "exposes them until they are rotated".
7. L366 "A stolen copy is useless without the server's key" vs L103 the server decrypts copies the browser sends.
8. L20 "no admin role or permission settings" vs L148 the watch timeout "is an app setting, changed on the settings page, not a deployment setting".
9. L97 restrict egress to "the cluster API servers, Vault and the database" vs L247–248 install from any HTTP repository or OCI registry added per session.
10. L179 "Every list and detail view is backed by a live watch" vs L193 metrics from metrics-server and L287 "A Helm release is not a cluster object".
11. L443 "The ephemeral debug container is the only debug tool" vs L210 node shell "through a privileged debug pod" vs L185 the action "debug pods".
12. L335 the audited reveal is "the only way to see a value" vs L232 "Files that come from Secret volumes can be read like any other file".
13. L211, L186, L232 actions "are audited" in Phase 1 features vs L429 the audit log is Phase 3.
14. L428 and L432 a penetration test at Gate 2 "with every finding fixed before Phase 3" vs L429 "penetration tests" in Phase 3.
15. L426 Phase 0 scope "Vault login" vs L437 "Vault itself has no SSO integration" (the user pastes a token; nothing logs in to Vault).
16. L120 "the only state a replica holds itself is its open connections" vs L171 objects "kept in memory" and L173 a per-replica Helm queue.
17. L160 the job stores a "values hash" vs L165 the user "clicks Retry".
18. L295 "Releases are tested against 1.0" vs L415 "Kubernetes 1.35 and newer" (external matrix, C10).
19. L264 "Supported Flux: v2.2 and newer" vs L267 HelmRelease inventory (C19).
20. L306 each module "appears only when the cluster serves the CRDs" vs L316 the permissions view of ServiceAccounts, which needs no CRDs.
21. L351 a sidebar entry "Security" vs L315–316 "Trivy Operator" and "Permissions and policy" vs L399 setting names `trivy` and `permissions`.
22. L17 lists the modules without Trivy and policy by name ("security reports"); L399 lists `karpenter` as a module name although Karpenter is specified outside the Ecosystem modules section (L293) whose rules L306 defines.
23. `README.md` line 3 "Sign in with a Vault token or an uploaded kubeconfig" vs L36 sign-in with Google or GitHub first.
24. L83 `encryption.keySecret` sits in the table headed "Helm chart settings for Vault".

Used but never defined:

- The YAML configuration file (L151, L418): name, path, schema.
- The database connection setting. Only the cleanup job's `KUBELOOK_AUDIT_CLEANUP_DATABASE_URL` is named (L378).
- The external URL or OAuth redirect setting.
- Setting names for: session idle timeout (L115), shell idle timeout (L370), watch idle timeout (L148), per-stream log rate cap (L172, L203), stream buffer size (L172), node shell switch (L210), org-wide Helm repositories (L248), per-cluster Prometheus source (L193), job lease length (L160).
- `auth.*.clientSecretRef` format (L48, L50).
- `debug.profiles` schema (L218, L395).
- "App setting" (L148, L412), "deployment settings", "Vault identity" (L376), "key ID" (L105), "file label" (L94), "risk level" (L316), "strict CSP" (L366), "sticky" (L120), "interaction" (L115), "server-side filter" (L199), "the load test" (L175), "the spike" as a deliverable (L405).
- "Duo" (L437), mentioned once with no context.
- `itops` (L85), unexplained.

---

## 7. Feasibility assessment

I don't know the team size or the time available, so I can't say whether any phase fits a calendar. What follows is about scope shape and risk.

- **Phase 0, Spike (L426).** Buildable. Riskiest item: discovery against real policies, including policies that use wildcards or templates, Vault namespaces, and `create`-only grants. The gate (L431) tests discovery, re-mint and logout only. It does not test the kubeconfig path, the saved-copy model, failover, or any load. I would add a watch-hub load test and the copy-binding design here, because both are expensive to change later.
- **Phase 1, Core (L427).** Very large and has no gate. It includes bulk node actions, which are durable jobs (L160), but no phase lists the job system. Riskiest item: the watch hub (per-user, bounded memory, resume, failover). Second: the file browser's write paths. I would defer: file browser edit, upload and delete (keep browse and download); node shell; kubeconfig OIDC refresh; the field-filter query bar; structured-log extras.
- **Phase 2, Platform (L428).** Helm, Flux, CRD forms, the map, Karpenter and seven modules in one phase. Riskiest item: Helm in a long-lived multi-user server (credentials that expire mid-operation, memory, job recovery, Secret handling), on an SDK that is out of security support (C4). Second: schema-driven forms from OpenAPI, which have many awkward cases **[U]**. I would cut from v1: schema forms (raw YAML only), the resource map, the Karpenter treemap, and the Trivy, permissions, KEDA, External Secrets and ricoberger modules, since the generic browser already lists those objects (L320). Keep Flux and Helm, read-only first.
- **Phase 3, Harden (L429).** The order is wrong. The audit log must exist before audited features ship (C5). The load test sets every "proposed" number (L175) and validates D5, so it belongs before Phase 1 ends. Metrics are needed to run that load test. Riskiest item: finding in Phase 3 that 200 sessions per replica does not hold.

Not in any phase: the stream gateway, PostgreSQL and multi-replica support, durable jobs, saved-credential encryption and browser storage, settings and preferences, deep links, the command palette, RBAC-aware UI, exports, metrics display, the Helm chart for Kubelook itself, and the audit cleanup command.

---

## 8. What the spec doesn't ask

Questions:

1. How does a user get a Vault token on a machine with nothing installed?
2. What does the Google or GitHub step buy, given it authorizes nothing?
3. Who reads the audit log, with what tool?
4. How is one user's access cut off within minutes?
5. Is the console reachable from the internet? "From any machine" (L32) suggests yes. Then the identity provider is the only control in front of a token-paste box.
6. Is one instance shared by teams that do not trust each other? If so, C2 crosses team boundaries.
7. What happens when the cluster hosting Kubelook is the one that is down?
8. Which Vault versions and editions are in use? Is a Vault namespace involved?
9. Is cluster-wide binding for every user intended?
10. How many service accounts and bindings will the console create per day in each cluster, and does anything limit Vault's lease count?
11. Does any compliance rule require session recording or audit immutability?
12. Should editing an object managed by Flux or Helm warn the user that it will be reverted?
13. Should the console refuse root tokens and never-expiring tokens?
14. Is there a read-only or change-freeze mode?
15. Why not Headlamp with a credential plugin, or an existing access broker? Only the fork option is discussed (L448).
16. Which browser storage behaviours break saved copies (Safari's 7-day rule, private windows, managed browsers that clear site data)?
17. What is the rule for raising the Kubernetes, Flux, Karpenter and Helm floors?

Assumptions made without saying so:

- The operator of the console is trusted with every user's credentials.
- The console's network can reach Vault and every API server, which makes the console a single point that bridges to every cluster.
- Mount name equals cluster name, and role names are the same on every cluster.
- There is one Vault and one organization.
- Vault roles set short TTLs.
- Users' Vault tokens are service tokens with the `default` policy attached.
- Every admission webhook supports dry run.
- Containers targeted by the file browser have common Unix tools with compatible options.
- The identity provider account and the Vault token belong to the same person.

---

## 9. Prioritized action list

1. **Bind saved copies to a user and make them revocable, or drop saved copies.** Sections: Sessions (L103), Security model (L366–367). Why: C1. *Needs an owner decision.*
2. **Move the audit log to Phase 0 or 1 and define behaviour when an audit write fails.** Sections: Delivery plan (L424–432), Audit storage (L378). Why: C5. *Needs an owner decision.*
3. **Decide the Helm SDK version.** Section: Helm (L244). Why: C4. *Needs an owner decision.*
4. **Fix the Vault capability rule to `update`.** Sections: L62, L401. Why: C3. *Can be fixed without asking.*
5. **Add a threat-table row and a concrete control set for cluster-supplied content** (CSP text, rendering rules for logs, notes, annotations, links, terminal output). Section: Security model (L364). Why: C2. *Needs an owner decision* on the CSP and rendering policy.
6. **Rewrite the key-leak response and allow a previous key for decryption.** Section: Sessions (L105–108). Why: C6, D8. *Needs an owner decision* on the second key; the L108 wording *can be fixed without asking*.
7. **Resolve one WebSocket versus the separate gateway, and SQLite versus the gateway; consider deferring the gateway.** Sections: Architecture (L149, L151, L158). Why: C12, D7, D9. *Needs an owner decision.*
8. **Move the watch-hub load test before the end of Phase 1; add a per-user watch cap and an absolute session lifetime.** Sections: Replica memory (L171), Sessions (L115), Delivery plan. Why: C8, C9, D5. *Needs an owner decision.*
9. **Correct the `kubectl edit` statement and choose the save mechanism after a conflict-rate test.** Section: Real-time manifest editing (L238). Why: C11, D12. The factual correction *can be fixed without asking*; the mechanism *needs an owner decision*.
10. **Make version floors consistent.** Sections: Karpenter (L295), FluxCD (L264–269), NFR (L415), Decisions (L440, L444). Why: C10, C19. *Needs an owner decision* on the floors; the Flux suspend and inventory text *can be fixed without asking*.
11. **Complete the Secret-masking route list.** Section: Cross-cutting (L335–340). Why: C7. *Can be fixed without asking.*
12. **Change risky defaults and add missing switches**: short default `vault.tokenTTL`, per-role cluster binding, no `itops` default, a switch for kubeconfig upload, app-level egress deny list. Sections: Vault settings (L80–89), Path B (L97), Other deployment settings (L385–399). Why: C13, C26, D15. *Needs an owner decision.*
13. **Specify the missing Vault behaviours**: namespaces, token types, empty `kubernetes_host`, lease revocation on token expiry, supported Vault versions. Section: Path A (L53–76), L401. Why: C14, C15, 5.2. Mostly *can be fixed without asking*; supported versions *need an owner decision*.
14. **Fix the unbuildable items**: upload fallback tools, init containers, the "another user" message, metrics polling, job inputs for Retry. Sections: Pod file browser (L225–228), Deep links (L355), L179, L160. Why: C16, C17, C22, C23, C29. *Can be fixed without asking.*
15. **Add the missing sections and cut scope**: test strategy, success criteria, risks, open questions, configuration reference; reduce Phase 2 as in section 7. Section: whole document, Delivery plan. Why: 5.11, section 7. *Needs an owner decision.*

---

## 10. Verification log

All sources were read on 2026-11-10. Repository files were read from the default branch on that date unless a tag is given. "Confirmed" means the source supports the spec's claim. "Contradicted" means the source disagrees with the spec. Rows that support a finding of mine but are not a spec claim are marked "Finding".

| # | Claim (spec line) | Source | Result |
|---|---|---|---|
| 1 | `sys/internal/ui/mounts` is internal with no backwards-compatibility guarantee; unauthenticated; with a token lists mounts the token has capabilities on (L60, L401) | developer.hashicorp.com/vault/api-docs/system/internal-ui-mounts; `hashicorp/vault` `vault/logical_system.go` (`pathInternalUIMountsRead`, `hasMountAccess`, unauthenticated path list) | Confirmed |
| 2 | Finding: the endpoint returns only mounts in the request's namespace | `hashicorp/vault` `vault/logical_system.go` (`ns.ID == entry.NamespaceID`) | Finding |
| 3 | Vault `default` policy grants `lookup-self`, `renew-self`, `capabilities-self` (L401) | `hashicorp/vault` `vault/policy_store.go` | Confirmed |
| 4 | Finding: `default` policy has no `sys/leases/revoke`; it has `auth/token/revoke-self`, `sys/leases/renew`, `sys/leases/lookup` | same file | Finding |
| 5 | `create` or `update` on the creds path is enough (L62, L401) | `hashicorp/vault-plugin-secrets-kubernetes` `path_creds.go` (only `logical.UpdateOperation`, no existence check); `hashicorp/vault` `vault/request_handling.go` (`existenceCheck`: no check means update); policy examples at developer.hashicorp.com/vault/docs/secrets/kubernetes use `update` | Contradicted (`create` alone is not enough) |
| 6 | `GET .../config` returns `kubernetes_host` and `kubernetes_ca_cert` (L55) | `vault-plugin-secrets-kubernetes` `path_config.go` | Confirmed, with the limit that it returns only values the admin set; the host can be empty |
| 7 | Creds request fields and response fields; `cluster_role_binding` needs a ClusterRole-type role (L67) | `path_creds.go` | Confirmed |
| 8 | Sample creds response shows `renewable: false` (L69) | developer.hashicorp.com/vault/api-docs/secret/kubernetes | Confirmed |
| 9 | Vault removes created objects when the lease expires (L69) | developer.hashicorp.com/vault/docs/secrets/kubernetes; `path_creds.go` (three role kinds; nothing created for `service_account_name`) | Confirmed for roles where Vault creates objects |
| 10 | Finding: TTL falls back to role default, then the mount or system default lease TTL | `path_creds.go` | Finding |
| 11 | Finding: default service account name template includes token display name and role name | `vault-plugin-secrets-kubernetes` `path_roles.go` | Finding |
| 12 | Max TTL can't be reported in advance; decided at renewal (L111) | developer.hashicorp.com/vault/docs/concepts/tokens | Confirmed |
| 13 | An expired token is revoked (L111) | same page | Confirmed |
| 14 | Finding: a token's leases are revoked when the token is revoked or expires | developer.hashicorp.com/vault/docs/concepts/lease; tokens page | Finding |
| 15 | Finding: batch tokens are not renewable; TTL 0 means never expires | tokens page | Finding |
| 16 | `sys/capabilities-self` exists and takes paths (L61) | developer.hashicorp.com/vault/api-docs/system/capabilities-self | Confirmed (takes a list of paths) |
| 17 | `kubectl edit` uses PUT (L238) | `kubernetes/kubernetes` `staging/src/k8s.io/kubectl/pkg/cmd/util/editor/editoptions.go` (`visitToPatch`) | Contradicted (it sends a PATCH) |
| 18 | Only the current log file is served; rotation default 10 Mi (L201) | `kubernetes/website` `content/en/docs/concepts/cluster-administration/logging.md` | Confirmed |
| 19 | Events kept 1 hour by default (L299) | `kubernetes/website` kube-apiserver reference (`--event-ttl` default `1h0m0s`) | Confirmed |
| 20 | Finding: `410 Gone` requires a relist; compaction interval default 5m | `kubernetes/website` `api-concepts.md`; kube-apiserver reference | Finding |
| 21 | Finding: Endpoints API deprecated in 1.33 | `kubernetes/kubernetes` `CHANGELOG/CHANGELOG-1.33.md` | Finding |
| 22 | Finding: WebSocket exec needs `create` from 1.35 | `CHANGELOG/CHANGELOG-1.35.md` | Finding |
| 23 | Finding: `SelfSubjectRulesReview` may be incomplete | `staging/src/k8s.io/api/authorization/v1/types.go` | Finding |
| 24 | Finding: service account requests use `workload-low` | `kubernetes/website` `flow-control.md` | Finding |
| 25 | Finding: dry run fails for webhooks with side effects | `kubernetes/website` `extensible-admission-controllers.md` | Finding |
| 26 | Finding: ephemeral containers can't be removed, no resources or ports, not on static pods | `kubernetes/website` `ephemeral-containers.md` | Finding |
| 27 | Kubernetes v1.35.6 exists; 1.35 is a current release (L415, L440) | `CHANGELOG-1.35.md`; `kubernetes/website` `data/releases/schedule.yaml` (1.35 end of life 2027-02-28; 1.34 end of life 2026-10-27) | Confirmed |
| 28 | client-go has an OIDC auth provider with refresh tokens (L95) | `staging/src/k8s.io/client-go/plugin/pkg/client/auth/oidc/oidc.go` | Confirmed; it also persists rotated tokens and reads `idp-issuer-url` and a CA file path |
| 29 | Finding: `kubectl debug` profile names | `staging/src/k8s.io/kubectl/pkg/cmd/debug/profiles.go` | Finding |
| 30 | Finding: metrics-server has no watch | `kubernetes-sigs/metrics-server` `pkg/api/pod.go` | Finding |
| 31 | Helm releases are stored as `sh.helm.release.v1.*` (L244) | `helm/helm` v3.19.0 `pkg/storage/storage.go`; pkg.go.dev page reachable | Confirmed |
| 32 | Helm `pending-*` states exist (L163) | `helm/helm` v3.19.0 `pkg/release/status.go` | Confirmed |
| 33 | Finding: Helm 3 security fixes end 2026-11-11 | `helm/helm-www` `blog/2025-11-17-helm-4-released/index.md` | Finding |
| 34 | Finding: Helm storage drivers include ConfigMaps and SQL | `helm/helm` v3.19.0 `pkg/action/action.go` | Finding |
| 35 | Finding: Helm 3.20.x supports Kubernetes 1.35 to 1.32 | `helm/helm-www` `versioned_docs/version-3/topics/version_skew.md` | Finding |
| 36 | Flux kinds list; ExternalArtifact and ArtifactGenerator need Flux 2.7 (L252–264) | `freelensapp/freelens-fluxcd-extension` `README.md` | Confirmed against the spec's cited source only |
| 37 | Reconcile by `reconcile.fluxcd.io/requestedAt` (L268) | `fluxcd/helm-controller` `docs/spec/v2/helmreleases.md` | Confirmed |
| 38 | ArtifactGenerator suspends by annotation (L269) | `fluxcd/source-watcher` `docs/spec/v1beta1/artifactgenerators.md` | Confirmed |
| 39 | Suspend by `spec.suspend` for the listed kinds (L269) | `controlplaneio-fluxcd/flux-operator` `docs/api/v1/resourceset.md`, `fluxinstance.md`, `resourcesetinputprovider.md` | Contradicted for flux-operator kinds (annotation; no `spec.suspend` found) |
| 40 | HelmRelease inventory available (L267) | `fluxcd/helm-controller` `CHANGELOG.md` (added in 1.5.0); `fluxcd/flux2` v2.2.2 manifests (helm-controller v0.37.2); `helm-controller` v0.37.2 `docs/spec/v2beta2/helmreleases.md` (no inventory) | Contradicted for Flux 2.2 |
| 41 | Ready and failed counts per kind (L266) | `fluxcd/notification-controller` `api/v1beta3/alert_types.go` (no status) | Contradicted for v1beta3 Alerts |
| 42 | Finding: Flux supports the last three minor releases | `fluxcd/website` `content/en/flux/releases/_index.md` | Finding |
| 43 | Karpenter v1 API; `expireAfter` on NodePool (L297) | `kubernetes-sigs/karpenter` `pkg/apis/v1/nodepool.go` | Confirmed |
| 44 | NodeOverlays exist (L297, L300) | `kubernetes-sigs/karpenter` `pkg/apis/v1alpha1/nodeoverlay.go`; `aws/karpenter-provider-aws` `website/content/en/docs/concepts/nodeoverlays.md` | Confirmed (alpha, feature-gated) |
| 45 | Karpenter 1.0 usable on the Kubernetes floor (L295 with L415) | `aws/karpenter-provider-aws` `website/content/en/docs/upgrading/compatibility.md` | Contradicted (1.35 needs `>= 1.9`) |
| 46 | AKSNodeClass, GCENodeClass, GCECustomMachineType exist (L301) | file listings of `Azure/karpenter-provider-azure` and `cloudpilot-ai/karpenter-provider-gcp` | Confirmed |
| 47 | KEDA pause and resume (L312) | `kedacore/keda-docs` `content/docs/2.22/concepts/scaling-deployments.md` | Confirmed |
| 48 | External Secrets force sync (L313) | `external-secrets/external-secrets` `docs/introduction/faq.md` | Confirmed |
| 49 | Trivy report kinds (L315) | file listing of `aquasecurity/trivy-operator` (`deploy/` CRDs) | Confirmed |
| 50 | Gatekeeper constraint violations (L316) | `open-policy-agent/gatekeeper` `website/docs/audit.md` | Confirmed, capped at 20 by default |
| 51 | ricoberger operator details (L311, L321–326) | `ricoberger/vault-secrets-operator` `api/v1alpha1/vaultsecret_types.go`, `internal/controller/vaultsecret_controller.go`, `README.md` | Confirmed |
| 52 | GRPCRoute served by Gateway API (L284) | `kubernetes-sigs/gateway-api` `apis/v1/grpcroute_types.go` | Confirmed |
| 53 | netshoot is Apache 2.0, v0.16 (L218) | `nicolaka/netshoot` `LICENSE`; repository tags | Confirmed |
| 54 | Headlamp is Apache 2.0 with stateless kubeconfig code (L448, L463) | `kubernetes-sigs/headlamp` `LICENSE`, `backend/cmd/stateless.go`, file listing | Confirmed |
| 55 | Finding: Redis 8 licence; Valkey licence | `redis/redis` `LICENSE.txt`; `valkey-io/valkey` `COPYING` | Finding |
| 56 | PKCE with GitHub (L40) | `github/docs` `content/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps.md` | Confirmed |
| 57 | Google `hd` claim, `sub` as key (L41–42) | Web search summary of [Google OpenID Connect guide](https://developers.google.com/identity/openid-connect/openid-connect) and [ID token verification guide](https://developers.google.com/identity/gsi/web/guides/verify-google-id-token); pages not read directly | Confirmed by summary only |
| 58 | Finding: Safari removes script-writable storage after 7 days without interaction | Web search summary of [WebKit tracking prevention](https://webkit.org/tracking-prevention/) and [WebKit blog](https://webkit.org/blog/10218/full-third-party-cookie-blocking-and-more/); pages not read directly; the pages are several years old | Finding, by summary only |
| 59 | `GOMEMLIMIT` behaviour (L174) | `golang/go` `src/runtime/extern.go` | Confirmed (soft limit; excludes non-Go memory) |
| 60 | Trivy rescan by deleting the report (L315) | `aquasecurity/trivy-operator` `docs/faq.md` | Couldn't verify |

Hosts I could not reach from this environment: kubernetes.io, helm.sh, fluxcd.io, karpenter.sh, webkit.org, the GitHub API. For those I read the documentation sources in the projects' GitHub repositories instead.

Claims left unchecked:

- Whether a long-running watch or `exec` stays authenticated after its token expires or its service account is deleted (L69).
- Whether status updates change `metadata.resourceVersion` (used in C11).
- Whether the `last-applied-configuration` annotation holds Secret data (used in C7).
- Whether the Helm SDK writes caches to disk (L68).
- Whether Helm 4 and Helm 3 can manage each other's releases (L244).
- Whether Flux 2.2.2 runs correctly on Kubernetes 1.35 (L440).
- Which Flux release ships helm-controller 1.5.0.
- cert-manager and Kyverno behaviour (L314, L316).
- `EC2NodeClass` details (L301).
- Which kubeconfigs EKS, GKE and AKS generate (L96).
- Vault token CIDR binding, use-limited tokens, and what `lookup-self` returns for identity.
- PostgreSQL privilege granularity for the cleanup role (L378).
- The NIST limit for random GCM nonces.
- Whether NetworkPolicy can filter by host name.
- Monaco's and xterm.js's CSP needs and escape-sequence handling.
- GitHub `read:org` visibility under organization OAuth restrictions.
- The Freelens extension repositories in the Sources list other than the FluxCD one (L452–462).
- The Rancher pages (L465–466).
- Page Visibility API behaviour and browser tab freezing (L148).
- Kubelet and load balancer idle timeouts for `exec`.
- Pod Security admission behaviour for debug profiles.
- Whether `exec` works in a terminated init container (used in C17).
