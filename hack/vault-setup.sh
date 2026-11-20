#!/usr/bin/env bash
# Vault mounts, roles, policies and tokens, and the Kubernetes fixtures of both clusters.
# Idempotent. See PLAN.md Step 7.2. Writes .it/tokens.env (never print it in CI logs).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
cd "$ROOT"
V() { docker exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=root -i kubelook-it-vault vault "$@"; }

for c in alpha beta; do
  K() { kubectl --kubeconfig ".it/kubeconfig-$c-host.yaml" "$@"; }
  K create namespace itops --dry-run=client -o yaml | K apply -f - >/dev/null
  K create namespace vault-system --dry-run=client -o yaml | K apply -f - >/dev/null
  K -n vault-system create serviceaccount vault --dry-run=client -o yaml | K apply -f - >/dev/null
  K create clusterrolebinding vault-admin --clusterrole=cluster-admin --serviceaccount=vault-system:vault \
    --dry-run=client -o yaml | K apply -f - >/dev/null
  JWT="$(K -n vault-system create token vault --duration=24h)"
  CA="$(K -n kube-system get configmap kube-root-ca.crt -o jsonpath='{.data.ca\.crt}')"
  V secrets enable -path="kubernetes/$c" kubernetes >/dev/null 2>&1 || true
  V write "kubernetes/$c/config" kubernetes_host="https://kubelook-it-$c-control-plane:6443" \
    kubernetes_ca_cert="$CA" service_account_jwt="$JWT" >/dev/null
  V write "kubernetes/$c/roles/devops-role" allowed_kubernetes_namespaces="*" \
    kubernetes_role_type=ClusterRole kubernetes_role_name=cluster-admin token_default_ttl=10m token_max_ttl=1h >/dev/null
  V write "kubernetes/$c/roles/viewer-role" allowed_kubernetes_namespaces="*" \
    kubernetes_role_type=ClusterRole kubernetes_role_name=view token_default_ttl=10m token_max_ttl=1h >/dev/null
  K apply -f test/integration/testdata/manifests/base.yaml >/dev/null
  K apply -f test/integration/testdata/manifests/pdb.yaml >/dev/null
  for d in web logger shell; do K -n demo rollout status "deployment/$d" --timeout=180s >/dev/null; done
done
# Fixture CRDs go to alpha only, so beta proves that modules hide themselves.
kubectl --kubeconfig .it/kubeconfig-alpha-host.yaml apply -f test/integration/testdata/crds/ >/dev/null

V secrets enable -path=kubernetes/empty kubernetes >/dev/null 2>&1 || true
V secrets enable -path=kubernetes/notk8s kv >/dev/null 2>&1 || true
V secrets enable -path=other/gamma kubernetes >/dev/null 2>&1 || true

for p in alice bob carol dave viewer; do
  docker cp "test/integration/testdata/policies/it-$p.hcl" "kubelook-it-vault:/tmp/it-$p.hcl"
  V policy write "it-$p" "/tmp/it-$p.hcl" >/dev/null
done

token() { V token create "$@" -field=token; }
{
  echo "ALICE_TOKEN=$(token -policy=it-alice -policy=default -ttl=4h)"
  echo "BOB_TOKEN=$(token -policy=it-bob -policy=default -ttl=4h)"
  echo "CAROL_TOKEN=$(token -policy=it-carol -policy=default -ttl=4h)"
  echo "DAVE_TOKEN=$(token -policy=it-dave -policy=default -ttl=4h)"
  echo "VIEWER_TOKEN=$(token -policy=it-viewer -policy=default -ttl=4h)"
  echo "SHORT_TOKEN=$(token -policy=it-alice -ttl=20m -explicit-max-ttl=25m)"
  echo "BATCH_TOKEN=$(token -type=batch -policy=it-alice -ttl=1h)"
  REVOKED="$(token -policy=it-alice -ttl=1h)"
  V token revoke "$REVOKED" >/dev/null
  echo "REVOKED_TOKEN=$REVOKED"
} > .it/tokens.env
chmod 600 .it/tokens.env
echo "VAULT SETUP OK"
