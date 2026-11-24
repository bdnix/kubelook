# Operations

What an operator needs to run the console. Statements here are proven by a test of this repository, or are marked "not verified".

## Changing the encryption key

The console holds one key at a time (`KUBELOOK_ENCRYPTION_KEY`); there are no old keys and no re-encryption. Every encrypted value
is AES-256-GCM with a random nonce and records the ID of the key that encrypted it. To rotate:

1. Create a new key (`head -c 32 /dev/urandom | base64`) and put it in the Secret named by `encryption.keySecret`.
2. Restart **all replicas together** (`kubectl -n kubelook rollout restart deployment/kubelook`). While replicas with the old and the
   new key run side by side, each discards what the other encrypted.

What users see afterwards:

- Every signed-in user is signed out, because the session credentials in the database cannot be decrypted any more.
- The encrypted copies in a browser carry an unknown key ID and are discarded. At the next sign-in the user sees: "Kubelook's
  encryption key was changed, so your saved Vault token and kubeconfigs can't be used anymore. Please paste your token and upload
  your kubeconfigs again."
- The change is recorded in the audit log by key ID, never the key.

`TestPostgres_ChangingTheEncryptionKey_SignsUsersOutAndDiscardsSavedCopiesWithoutLeakingAKey` proves this on a real PostgreSQL and
two replicas.

If the key leaked, replacing it is not the whole response. Whoever held the key together with a copy of the database or of a
browser's saved copies may already have plaintext credentials, which work directly against Vault and the clusters until they expire
or are revoked. Also revoke the affected Vault tokens and rotate the credentials in uploaded kubeconfigs.

## Restarts and the maintenance banner

Set `maintenance.banner` before a planned restart, for example "Kubelook restarts at 14:00 UTC; open shells will end.", and clear it
afterwards. Every signed-in user sees it on every page. There is no drain period: a stopping replica closes its connections at once.

What happens when a replica stops (PostgreSQL deployments; the tests are in `test/integration/postgres_test.go`):

- Sessions and credentials are in the database, so the browser reconnects to another replica and the user stays signed in
  (`TestPostgres_StoppingAReplica_TheSessionContinuesOnTheOtherAndAWatchDeliversASnapshot`).
- Watches reopen on the new replica, which lists each open view again and then watches. Log streams restart from the last line's
  timestamp; a few lines around the gap may repeat or be missing.
- **Shells end** with the replica that carries them, and so does anything running in them; the terminal says so and offers a new
  shell. File transfers in progress fail and must be started again.
- Long actions (Helm installs, upgrades, rollbacks and uninstalls, node drains, bulk actions) are durable jobs. If the replica that
  runs one dies, another replica checks the cluster against the target state and marks the job succeeded or **interrupted**. An
  interrupted job is never retried automatically: the user sees what is left and clicks Retry, or Roll back
  (`TestPostgres_ADrainJobOfAKilledReplica_IsInterruptedWithARetryOfferSeenFromTheOther`).

With SQLite there is one replica: a restart ends every connection, and the browsers reconnect when it is back.

## When Vault is unreachable

Clusters that are already open keep working until their current Kubernetes credentials expire; they cannot be re-minted until Vault is
back. Opening a new Vault cluster fails with "Vault is unreachable, so this cluster can't be opened right now". Kubeconfig clusters keep
working. A banner says that Vault is unreachable, and the console retries in the background (the integration test
`TestKubeconfig_KeepsWorkingWhileVaultIsDown_AndRestoresFromTheSealedCopy` covers the kubeconfig part).

## Metrics

`GET /metrics` (Prometheus format) needs no session; keep it off the public address and scrape it from inside the cluster.

| Metric | Meaning |
| --- | --- |
| `kubelook_build_info` | Build information; the value is always 1 |
| `kubelook_sessions_active` | Signed-in sessions |
| `kubelook_ws_connections` | Open WebSocket connections |
| `kubelook_ws_dropped_messages_total` | WebSocket messages dropped for slow clients, by channel kind |
| `kubelook_watches_active` | Upstream Kubernetes watches held by this replica |
| `kubelook_watch_subscribers` | Views attached to those watches |
| `kubelook_watch_cached_objects` | Objects held in the watch caches |
| `kubelook_watches_per_user` | Upstream watches per user that has any |
| `kubelook_log_streams_active` | Open log streams |
| `kubelook_log_lines_dropped_total` | Log lines dropped because a client could not keep up |
| `kubelook_exec_sessions_active` | Open shells and debug sessions |
| `kubelook_refused_requests_total` | Requests refused because the server was busy, by kind |
| `kubelook_jobs` | Jobs by type and status |
| `kubelook_http_requests_total`, `kubelook_http_request_duration_seconds` | HTTP requests and durations, by route pattern, method and status |
| `kubelook_vault_request_duration_seconds` | Duration of requests to Vault |
| `kubelook_kube_request_duration_seconds` | Time until the Kubernetes API answered |

The endpoint also serves the Go runtime and process collectors. Traces are exported to
`telemetry.otlpEndpoint` when it is set.

## Logs and the audit record

The console writes one JSON line per event to standard output (`log/slog`). A request line has `msg: "request"`, `method`, `route`
(the matched pattern, never the path), `status`, `durationMs` and `requestId`; the same request ID is in the `X-Request-Id` response
header. Secrets, tokens, Secret values, file contents and log lines are never logged.

Every audit record is also one JSON line, with `"audit": true`, and a row of the table `audit_events`. The fields are: `time`,
`recorded` (false when the database insert failed), `account` (provider, subject, email of the Google or GitHub account), `vault` (the
identity behind the Vault token), `kube` (the kubeconfig context and user, for kubeconfig clusters), `cluster`, `namespace`, `verb`,
`resource` (kind and name), `outcome` and `details`. An action that changes something has two records with the same
`details.requestId`: one before it with outcome `requested` (if it cannot be written, the action is refused) and one after with
`success` or `failure`. Details never carry a token, a password, a secret, content or a value. `TestAudit_EveryMutationHasARequestedAndAnOutcomeRecordWithTheAccountAndTheVaultIdentity`
proves this against a real cluster.

### Reading the audit log

The console has no audit viewer, and its own database role cannot read `audit_events`. Read the records with SQL, as the database
owner or through a read-only role you create, or from the JSON lines in your log pipeline. No console user can read another user's
records through the console. For example, as the database owner:

```sql
SELECT to_timestamp(time / 1000.0), account_email, verb, cluster, namespace, resource_kind, resource_name, outcome
FROM audit_events WHERE time > (extract(epoch FROM now() - interval '1 day') * 1000)::bigint ORDER BY time;
```

(`time` is Unix milliseconds.) The cleanup command `kubelook audit-cleanup` deletes records older than `audit.retentionDays`; see
[postgres.md](postgres.md).

## Known limits

- **Resource map.** It draws only links the objects state: `envFrom`, `env[].valueFrom.secretKeyRef` and volumes of Pods and
  workloads, owner references, Service selectors, Ingress backends and TLS Secrets, route `backendRefs`, and a workload's Helm
  release. Other references (for example `configMapKeyRef`, projected volumes, an Ingress `defaultBackend`) are not drawn.
- **Watch budget.** One view holds at most 24 watches. The resource map watches each kind per namespace while kinds times selected
  namespaces is at most 24, and otherwise watches each kind across all namespaces and filters on the server. A kind the user may not
  list across all namespaces falls back to the first namespaces that fit, and the map names the namespaces it left out. A user holds
  at most `watch.maxPerUser` (50) watches overall.
- **Helm storage.** Releases are read and written in Kubernetes Secrets (the Helm default). A cluster that stores releases in
  ConfigMaps can be read but is not supported for writes in this version.
- **OCI installs.** The mapping of install options for charts from an OCI registry is covered by unit tests only. No registry runs in
  the integration suite, so a pull from a real OCI registry is not proven ([spike-findings.md](spike-findings.md)).
- **Events.** The console stores no history. Views of events (for example Karpenter's scaling decisions) show only what the cluster
  still holds: one hour with the default `--event-ttl` of kube-apiserver.
- **Helm 3.** Kubelook ships only the Helm 4 SDK. A release made by the Helm 3 CLI is listed, upgraded and rolled back through
  Kubelook; after that the Helm 3 CLI could upgrade and roll back the same release on both test clusters, in one run each
  (`TestHelm_ReleasesMadeByHelm3_AreManagedThroughKubelookAndStayManageableByHelm3`). One run per cluster is not a rate; see
  [spike-findings.md](spike-findings.md).
- **Saved credentials stay in the browser that saved them.** On another machine, or after the user clears site data, they paste them
  again. Safari also removes script-writable storage, IndexedDB included, after 7 days of browser use without interaction with the
  site, so Safari users who stay away that long paste their token and kubeconfigs again.
- **Credentials are not revoked at sign-out.** Cluster credentials minted by Vault run to their TTL (`vault.tokenTTL`, default 30
  minutes); see [vault-policy.md](vault-policy.md).
- **Capacity.** The spec proposes 200 active sessions per replica. It is not measured: how many sessions a replica carries
  depends mostly on the clusters and Vault behind it. Watch the Prometheus metrics (sessions, watches, memory, refused requests).
