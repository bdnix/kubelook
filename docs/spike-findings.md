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

- **Result:** open — answered in Step 7.7.
- **How it was checked:** Step 7.7 edit test variant: 20 saves during a rollout, count of 409 answers (recorded, not asserted).
- **Date:** —
- **Consequence for the plan:** open an issue proposing patch-on-save; do not change behaviour without the owner's decision.

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

- **Result:** open — answered by the first green run of the `integration-tests` job (Step 7.2 acceptance, `hack/vault-setup.sh`).
- **How it was checked:** Step 7.2 acceptance and `TestVault_*`.
- **Date:** —
- **Consequence for the plan:** if minting fails, fix the harness, not the product; if the discovery results differ from §2, stop and report to the repository owner.
