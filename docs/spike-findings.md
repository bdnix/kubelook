# Spike findings

Written answers to the spec's "To confirm in the spike" list. Each section has the result, how it was checked, the date and the consequence for the plan. A row marked **open** is answered by the step named in it.

## Helm 3 release managed by Kubelook (Helm 4)

- **Result:** open — answered in Step 7.8.
- **How it was checked:** Step 7.8, required assertions.
- **Date:** —
- **Consequence for the plan:** not allowed to be unfavourable (A31): the test fails and the pull request does not merge.

## Release touched by Helm 4 managed again by the Helm 3 CLI

- **Result:** open — answered in Step 7.8.
- **How it was checked:** Step 7.8, recorded.
- **Date:** —
- **Consequence for the plan:** if unfavourable, document in `docs/operations.md`: one Helm major version at a time, move the CLI to Helm 4; no code change.

## YAML save conflict rate on a busy object

- **Result:** one run, recorded and not asserted: 20 saves of a Deployment during a rollout, 19 saved and 1 refused with 409. One run is a sample, not a rate; the number varies with how fast the rollout moves.
- **How it was checked:** `TestEdit_ConflictRateOnABusyObject_IsRecordedNotAsserted` in the `integration-tests` job; it writes `.it/findings/yaml-save-conflicts.txt`, which the job prints.
- **Date:** 2026-11-20
- **Consequence for the plan:** a conflict is rare enough that a full PUT with a conflict banner is workable; the owner may still decide on patch-on-save. Behaviour is not changed.

## Trivy rescans after report deletion

- **Result:** unconfirmed (A24).
- **How it was checked:** not checkable.
- **Date:** 2026-11-11
- **Consequence for the plan:** stays unconfirmed; the UI text already says so.

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
