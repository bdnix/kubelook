# Spike findings

Written answers to the spec's "To confirm in the spike" list. Each section has the result, how it was checked, the date and the consequence for the plan. A row marked **open** is answered by the step named in it.

## Helm 3 release managed by Kubelook (Helm 4)

- **Result:** works on both kind clusters (Kubernetes 1.35 and 1.37). A release installed with the Helm 3.22.1 CLI is listed by Kubelook with its notes, masked manifest and values; an upgrade through Kubelook (revision 2) stays on client-side apply (no field moved to a server-side-apply manager); a rollback through Kubelook to revision 1 gives revision 3; the Helm 3 CLI then shows revision 3 `deployed`, three history entries and `greeting: one`; an uninstall through Kubelook removes it.
- **How it was checked:** `TestHelm_ReleasesMadeByHelm3_AreManagedThroughKubelookAndStayManageableByHelm3` in the `integration-tests` job (Helm 4.3.1 SDK, Helm 3.22.1 CLI).
- **Date:** 2026-11-20
- **Consequence for the plan:** none; this was required (A31) and it holds. The same run found that the Helm 4 SDK refuses to run an action without a wait strategy; the adapter now sets the CLI default (hooks only).

## Release touched by Helm 4 managed again by the Helm 3 CLI

- **Result:** favourable in one run on each cluster. After the Kubelook changes, `helm3 upgrade` and `helm3 rollback` of the same release succeeded, and the same two commands succeeded on a release first installed through Kubelook (Helm 4); `helm3 list` worked on both. One run per cluster, not a rate.
- **How it was checked:** the recorded part of the same test; it writes `.it/findings/helm-cross-version.txt`, which the job prints.
- **Date:** 2026-11-20
- **Consequence for the plan:** nothing to add to `docs/operations.md`.

## YAML save conflict rate on a busy object

- **Result:** one run, recorded and not asserted: 20 saves of a Deployment during a rollout, 19 saved and 1 refused with 409. One run is a sample, not a rate; the number varies with how fast the rollout moves.
- **How it was checked:** `TestEdit_ConflictRateOnABusyObject_IsRecordedNotAsserted` in the `integration-tests` job; it writes `.it/findings/yaml-save-conflicts.txt`, which the job prints.
- **Date:** 2026-11-20
- **Consequence for the plan:** a conflict is rare enough that a full PUT with a conflict banner is workable; the owner may still decide on patch-on-save. Behaviour is not changed.

## Trivy rescans after report deletion

- **Result:** confirmed in the source of the Trivy Operator (`main` branch, not a pinned release) for eight kinds: `VulnerabilityReport` and `ExposedSecretReport` (the workload controller owns them, checks for reports of the current pod-spec hash and images, and submits a scan job when they are missing and no scan job is active; the exposed-secret check runs only when the secret scanner is enabled), and `ConfigAuditReport`, `RbacAssessmentReport`, `InfraAssessmentReport`, `ClusterConfigAuditReport`, `ClusterRbacAssessmentReport` and `ClusterInfraAssessmentReport` (the resource and node controllers own them and check for a report before scanning). Not confirmed: `SbomReport`, `ClusterSbomReport`, `ClusterVulnerabilityReport` and `ClusterComplianceReport`, whose controllers were not read. `ClusterExposedSecretReport` is not a kind of the operator (not in its registered types, no CRD), so it is not offered.
- **How it was checked:** read from `aquasecurity/trivy-operator` (`pkg/vulnerabilityreport/controller/workload.go`, `pkg/configauditreport/controller/{resource,node}.go`, the registered types and the CRDs). Not run against a cluster.
- **Date:** 2026-11-20
- **Consequence for the plan:** the Rescan action works for every report kind; the dialog says "read in the operator source" for the eight kinds and "Not confirmed for this kind" for the other four.

## Where Kyverno reports violations

- **Result:** unconfirmed (A24).
- **How it was checked:** not checkable.
- **Date:** 2026-11-11
- **Consequence for the plan:** stays unconfirmed; UI note in Step 5.14.

## Open watch or shell after credential expiry

- **Result:** open — answered in Step 7.11 part B (`slow_test.go`, nightly).
- **How it was checked:** `slow_test.go`.
- **Date:** —
- **Consequence for the plan:** if connections drop, nothing to change (the hub resumes watches; shells end with the standard message).

## Cluster mounts in a Vault namespace

- **Result:** open — the owner must answer; not applicable to the test Vault.
- **How it was checked:** not checkable here.
- **Date:** —
- **Consequence for the plan:** if yes, add `vault.namespace` (sent as `X-Vault-Namespace`) in a follow-up step using the same TDD pattern as Step 2.5.

## U1: minting credentials on Vault 2.2.1

- **Result:** works. Vault 2.2.1 mints service account tokens on both kind clusters (Kubernetes 1.35 and 1.37) through `kubernetes/<cluster>/creds/<role>`, with a role bound to `cluster-admin` and one bound to `view`. The two policy results of plan §2 held too: a token lists only the mounts its policies allow, and a `create`-only grant does not count as `update`.
- **How it was checked:** `hack/vault-setup.sh` (it runs on every `make it-up`) and `TestVault_Discovery_…`, `TestVault_Open_…`, `TestVault_ViewerRole_…` and `TestVault_Remint_…` in the `integration-tests` job.
- **Date:** 2026-11-20
- **Consequence for the plan:** none. Mounts need the `itops` namespace to exist in the cluster, or minting fails.

## OCI install

- **Result:** OCI install: unit-tested option mapping only.
- **How it was checked:** unit tests of the option mapping in `internal/helm/helm4`. No registry container is started in the integration suite, so a pull from a real OCI registry is not exercised.
- **Date:** 2026-11-20
- **Consequence for the plan:** none; an install from an OCI reference is not proven on a real cluster.
