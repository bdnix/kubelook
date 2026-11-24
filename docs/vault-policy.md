# Vault policy and roles

Kubelook has no cluster registry of its own: each cluster is a Kubernetes secrets engine mount in Vault, and what a user sees is what
their Vault token may use.

## Layout of the mounts

One mount per cluster at `kubernetes/<cluster>/` (the prefix is `vault.kubernetesMountPrefix`). Each mount's configuration must set
`kubernetes_host` and `kubernetes_ca_cert` to an address and a CA that Kubelook can reach. Kubelook uses them as Vault returns them. A
mount that relies on Vault's in-cluster defaults, or was never configured, has no API server address or CA, and opening that cluster
fails with a message that says so. Kubelook has no per-cluster override.

## What a user's token needs

For each cluster the user may use:

| Path | Capability |
| --- | --- |
| `kubernetes/<cluster>/config` | `read` |
| `kubernetes/<cluster>/creds/<role>` | `update` |

`create` on the creds path is not enough: the path handles only the update operation, so Vault treats every write to it as an update
and Kubelook counts only `update`. The integration test `TestVault_Discovery_ListsExactlyTheClustersThePoliciesAllow` checks this on a
real Vault: a token with `create` only sees no cluster.

Discovery also calls `sys/capabilities-self`, and the console calls `auth/token/lookup-self` and `auth/token/renew-self`. Vault's
built-in `default` policy grants all three. Confirm this against your own policies.

A sample policy for the clusters `alpha` and `beta` with the role `devops-role`:

```hcl
path "kubernetes/alpha/config"            { capabilities = ["read"] }
path "kubernetes/alpha/creds/devops-role" { capabilities = ["update"] }
path "kubernetes/beta/config"             { capabilities = ["read"] }
path "kubernetes/beta/creds/devops-role"  { capabilities = ["update"] }
```

## What a Vault role needs

- With `vault.clusterRoleBinding: true` (the default) the Vault role must use `kubernetes_role_type=ClusterRole`. With a role of type
  `Role`, Vault answers "a ClusterRoleBinding cannot ref a Role". Set `vault.clusterRoleBinding: false` when your roles are namespaced.
- `allowed_kubernetes_namespaces` must include `vault.credsNamespace` (default `itops`), and that namespace must exist in the cluster.
  Both settings are global, so every role on every cluster must fit them.
- Kubelook asks for `ttl` equal to `vault.tokenTTL`. Vault caps it at the role's `token_max_ttl`. Kubernetes refuses service account
  tokens shorter than 10 minutes.
- The roles to try are listed in `vault.candidateRoles`, in order of preference.

The integration environment sets up exactly this on Vault 2.2.1 (`hack/vault-setup.sh`).

## The internal endpoint

Discovery calls `sys/internal/ui/mounts` with the user's token. Vault documents it as an internal endpoint with no
backwards-compatibility guarantee, and it lists only the mounts of the Vault namespace the request is made in. Kubelook has no Vault
namespace setting; whether the cluster mounts of an organisation live in a child namespace is an open question recorded in
[spike-findings.md](spike-findings.md). Test every Vault upgrade against the endpoint before the rollout: `make it-up && make it` runs
the discovery tests against a Vault of the version pinned in `hack/it-up.sh`.

## Revocation

Kubelook does not revoke the credentials it minted when a user signs out: that needs `update` on `sys/leases/revoke`, which the
`default` policy does not grant. They run to their TTL and Vault then removes the service account it created (for roles that use
`kubernetes_role_name` or `generated_role_rules`). Keep `vault.tokenTTL` short.
