# Security review

A written self-review, kept next to the code so that the claims of the spec's security model can be checked. It is **not** the external
review: that is an open human gate (see the end). Each row names where the mitigation is implemented and which test proves it. A row
without a test says so.

## Threats and mitigations

| Threat (spec, "Security model") | Where it is implemented | What proves it |
| --- | --- | --- |
| Credential theft from the browser | The session cookie is `HttpOnly`, `SameSite` and `Secure` for an `https` public URL (`internal/session/manager.go`). Saved credentials are encrypted copies bound to the user by the authenticated data of AES-256-GCM (`internal/crypto`, `internal/auth/handlers.go`). A strict CSP is set on every response (`internal/httpapi/middleware.go`) | `TestSetCookie_Attributes` (session); `TestOpen_WrongAAD_ErrDecrypt` (crypto); `TestSecurityHeaders_OnEveryResponse` (CSP); `TestSignIn_GoogleAllowedDomain_SignsInWithASecureCookieAndAStableUserID` (integration) |
| Backend or database compromise | Session credentials are stored encrypted with `KUBELOOK_ENCRYPTION_KEY` (`internal/crypto`, `internal/session`); saved Vault tokens and kubeconfigs are kept only as encrypted copies in the browser; a changed key makes both unreadable | `TestPostgres_ChangingTheEncryptionKey_SignsUsersOutAndDiscardsSavedCopiesWithoutLeakingAKey` (integration). Saved kubeconfigs are a known exposure: their certificates and tokens can be long-lived (spec) |
| Privilege escalation through the console | Every Kubernetes call uses the user's own credential (`internal/cluster`, `internal/kube`); the chart gives the pod no service account token (`automountServiceAccountToken: false`) | `TestKubeconfig_BearerTokenOfAViewer_ReadsButCannotWrite`, `TestVault_ViewerRole_…` (integration); `hack/chart-test.sh` checks the token is not mounted |
| Malicious content from a cluster | Cluster text is rendered as React text children; `dangerouslySetInnerHTML` is forbidden by an ESLint rule (`web/eslint.config.js`); links go through `SafeLink`, which accepts only `http` and `https`; the CSP forbids inline script | `SafeLink.test.tsx`; the lint step of `make verify`; `TestSecurityHeaders_OnEveryResponse` |
| SSRF through user-supplied addresses | The egress guard refuses loopback, link-local and unspecified addresses on the resolved IP at connect time (`internal/egress`), for kubeconfig servers, OIDC issuers, and session Helm repositories and OCI registries | `TestControl_RefusesLocalAndLinkLocalAddresses`, `TestTransport_Blocked_RefusesALoopbackServer`, `TestTransport_ANameThatResolvesToALoopbackAddress_IsRefused`, `TestProxy_Blocked_RefusesATunnelToALoopbackAddress`; integration `TestKubeconfig_ServerOnALoopbackAddress_UploadsButOpeningIsRefused` |
| Abuse of shells | Exec goes through the Kubernetes API (`internal/execs`); idle shells time out; start and stop are audited | `TestShell_IdleClosesTheChannelOnTheFakeClock`; integration `TestExec_Audit_RecordsStartAndStopButNeverTheOutput` |
| Data theft or tampering through the file browser | Uses the `exec` subresource under the user's own RBAC (`internal/files`); size limit `fileBrowser.maxTransferBytes`; every action audited with path and size, never content; can be switched off | Integration `TestFiles_AnUploadOverTheLimitIsTooLarge`, `TestFiles_ANameWithShellSyntaxIsCreatedLiterally` |
| Tunnels into the cluster network | Not offered: the product has no port-forward or TCP tunnel feature | No test; the absence of a feature cannot be tested. A search for port-forward in `internal/` finds none |
| Cross-site request forgery | `SameSite` cookie, a CSRF token on every request that is not `GET`, `HEAD` or `OPTIONS`, and an `Origin` check on WebSocket upgrades (`internal/session/middleware.go`, `internal/ws/mux.go`) | `TestWS_UpgradeNeedsASessionAndAllowedOrigin`; integration `TestSignIn_MutatingCallsNeedTheCSRFToken` |
| Secret exposure in the UI | The server masks Secret values in every response and sends them only through the audited reveal (`internal/redact`) | `TestApply_Property_NoOriginalValueSurvives`; integration `TestRedaction_SecretsAreMaskedEverywhereButTheAuditedReveal` |
| Audit tampering | The audit table accepts only inserts from the console's PostgreSQL role; a cleanup role deletes only old rows; every record is also on standard output | Integration `TestPostgres_AuditRecords_CannotBeReadChangedOrDeletedByTheApplicationRole`, `TestPostgres_AuditCleanup_RoleDeletesOnlyRecordsOlderThanTheRetentionPeriod`. SQLite cannot provide this |

## The content security policy

```
default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'
```

`style-src` allows inline styles; `script-src` is `'self'`, so scripts are never inline. The other response
headers are `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, `Cache-Control: no-store` on
`/api/`, and `Strict-Transport-Security` when the public URL is `https`.

## What the egress guard does not cover

The guard checks addresses that a user supplies. It does not apply to the addresses of the deployment's own settings (Vault, Google,
GitHub, the database, per-cluster Prometheus, organisation-wide Helm repositories) or to the API servers of Vault clusters, which Vault
administrators set. Private ranges stay reachable on purpose, because clusters live there, so the console has no egress allowlist of its
own: whoever deploys it restricts its outbound traffic with a firewall or a NetworkPolicy ([deployment.md](deployment.md)). A
NetworkPolicy filters by IP address and cannot express "Google and GitHub" by name.

## Open human gates

The plan covers these with the integration suite, but a person must do them. The sign-off lines are filled in by the repository owner.
Cutting `0.1.0` (a version without `-rc`) needs both ([releasing.md](releasing.md)).

| Gate | What it is | Passed (date, who, link) |
| --- | --- | --- |
| Gate 1 | A real user's Vault token in a real organisation's Vault: discovery lists exactly the clusters the policies allow, opening works, and the open questions of [spike-findings.md](spike-findings.md) (for example Vault namespaces) are answered | _not yet_ |
| Gate 2 | An external security review and penetration test of the console, including this document | _not yet_ |

Items of [spike-findings.md](spike-findings.md) that are still open: the Vault namespace question, Kyverno's report location, and the
behaviour of open connections when a credential expires (the nightly run records it).
