#!/usr/bin/env bash
# The toolbox of the local demo (docker-compose.yaml).
#
#   toolbox.sh                     create the kind cluster, configure it, Vault and the sign-in stand-in, then keep them
#                                  configured (Vault runs in dev mode and forgets everything when it restarts)
#   toolbox.sh token [user]        a Vault token of a demo user (alice: every role, bob: read only), by userpass login
#   toolbox.sh kubeconfig [--host] the cluster's kubeconfig: for uploading to Kubelook, or (--host) for kubectl on this machine
#   toolbox.sh signin-as <user>    the account the next "Sign in with GitHub" signs in as (alice or bob)
#   toolbox.sh destroy             delete the kind cluster (run before removing the demo for good)
set -euo pipefail

CLUSTER=kubelook-demo
NODE="$CLUSTER-control-plane"
NETWORK="${DEMO_NETWORK:?}"
SIGNIN="${DEMO_SIGNIN_URL:?}"
export KUBECONFIG=/root/.kube/config VAULT_ADDR="${VAULT_ADDR:?}" VAULT_TOKEN="${VAULT_TOKEN:?}"

log() { echo "$(date -u +%T) $*"; }

# retry <seconds> <command...>: retries the command once a second until it succeeds or the time is up.
retry() {
  local n="$1"; shift
  for _ in $(seq 1 "$n"); do "$@" >/dev/null 2>&1 && return 0; sleep 1; done
  "$@"
}

cluster() {
  if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
    log "creating the kind cluster $CLUSTER (takes a minute or two the first time)"
    kind create cluster --name "$CLUSTER" --config /demo/kind.yaml --wait 180s
  elif [ "$(docker inspect -f '{{.State.Running}}' "$NODE")" != "true" ]; then
    log "starting the stopped kind node"
    docker start "$NODE" >/dev/null
  fi
  # The node joins the demo network, so Kubelook and Vault reach the API server by the node's name.
  docker network connect "$NETWORK" "$NODE" 2>/dev/null || true
  mkdir -p /root/.kube
  kind get kubeconfig --internal --name "$CLUSTER" > "$KUBECONFIG"
  retry 120 kubectl get --raw /readyz
}

workloads() {
  log "applying the demo workloads"
  kubectl create namespace itops --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl create namespace vault-system --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n vault-system create serviceaccount vault --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl create clusterrolebinding vault-admin --clusterrole=cluster-admin --serviceaccount=vault-system:vault \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  # A token Secret does not expire, so Vault keeps working however long the demo runs.
  kubectl apply -f - >/dev/null <<'YAML'
apiVersion: v1
kind: Secret
metadata:
  name: vault-token
  namespace: vault-system
  annotations: {kubernetes.io/service-account.name: vault}
type: kubernetes.io/service-account-token
YAML
  kubectl apply -f /demo/manifests.yaml >/dev/null
  # metrics-server, so the console shows CPU and memory; kind's kubelets have self-signed certificates.
  if ! kubectl -n kube-system get deployment metrics-server >/dev/null 2>&1; then
    if kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/download/v0.8.0/components.yaml >/dev/null; then
      kubectl -n kube-system patch deployment metrics-server --type=json \
        -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]' >/dev/null
    else
      log "metrics-server could not be installed; the console works without it, but shows no CPU and memory"
    fi
  fi
}

vault_configured() { vault secrets list -format=json 2>/dev/null | jq -e 'has("kubernetes/demo/")' >/dev/null; }

vault_setup() {
  log "configuring Vault"
  retry 60 vault status
  local jwt ca
  retry 60 kubectl -n vault-system get secret vault-token -o jsonpath='{.data.token}'
  jwt="$(kubectl -n vault-system get secret vault-token -o jsonpath='{.data.token}' | base64 -d)"
  ca="$(kubectl -n kube-system get configmap kube-root-ca.crt -o jsonpath='{.data.ca\.crt}')"
  vault secrets enable -path=kubernetes/demo kubernetes >/dev/null 2>&1 || true
  vault write kubernetes/demo/config kubernetes_host="https://$NODE:6443" \
    kubernetes_ca_cert="$ca" service_account_jwt="$jwt" >/dev/null
  vault write kubernetes/demo/roles/devops-role allowed_kubernetes_namespaces="*" \
    kubernetes_role_type=ClusterRole kubernetes_role_name=cluster-admin token_default_ttl=30m token_max_ttl=8h >/dev/null
  vault write kubernetes/demo/roles/viewer-role allowed_kubernetes_namespaces="*" \
    kubernetes_role_type=ClusterRole kubernetes_role_name=view token_default_ttl=30m token_max_ttl=8h >/dev/null
  vault policy write demo-admin - >/dev/null <<'HCL'
path "kubernetes/demo/config" { capabilities = ["read"] }
path "kubernetes/demo/creds/devops-role" { capabilities = ["update"] }
path "kubernetes/demo/creds/viewer-role" { capabilities = ["update"] }
HCL
  vault policy write demo-viewer - >/dev/null <<'HCL'
path "kubernetes/demo/config" { capabilities = ["read"] }
path "kubernetes/demo/creds/viewer-role" { capabilities = ["update"] }
HCL
  vault auth enable userpass >/dev/null 2>&1 || true
  vault write auth/userpass/users/alice password=alice token_policies=demo-admin token_ttl=8h token_max_ttl=24h >/dev/null
  vault write auth/userpass/users/bob password=bob token_policies=demo-viewer token_ttl=8h token_max_ttl=24h >/dev/null
}

# account <user>: the JSON the sign-in stand-in signs in as.
account() {
  case "$1" in
    alice) echo '{"sub":"demo-alice","email":"alice@demo.example","name":"Alice (demo)","hd":"demo.example","email_verified":true,"github_id":1001,"github_orgs":["demo"]}' ;;
    bob) echo '{"sub":"demo-bob","email":"bob@demo.example","name":"Bob (demo)","hd":"demo.example","email_verified":true,"github_id":1002,"github_orgs":["demo"]}' ;;
    *) echo "unknown demo user $1 (alice or bob)" >&2; return 2 ;;
  esac
}

announce() { curl -fsS --max-time 5 -X POST -H 'Content-Type: application/json' -d "$(account "$1")" "$SIGNIN/admin/next" >/dev/null; }

token() { vault login -method=userpass -token-only -no-store username="$1" password="$1"; }

banner() {
  cat <<EOF

==========================================================================================
 Kubelook demo is ready:  http://localhost:8080

 1. Click "Sign in with GitHub". The sign-in is a local stand-in: you are
    signed in as alice@demo.example at once, no password.
 2. Add a Vault token. A token of alice (every role):

      $(token alice)

    More tokens:  devbox --name kubelook-demo run token [alice|bob]
    Or log in yourself:  vault login -address=http://127.0.0.1:8200 -method=userpass username=alice password=alice
 3. Or upload a kubeconfig instead:  devbox --name kubelook-demo run kubeconfig > demo-kubeconfig.yaml
==========================================================================================
EOF
}

setup() {
  cluster
  workloads
  vault_setup
  retry 60 announce alice
}

main() {
  local current=alice
  setup
  banner
  touch /tmp/ready
  # On stop, take the kind node off the demo network, so "devbox down" can remove the network.
  trap 'docker network disconnect "$NETWORK" "$NODE" >/dev/null 2>&1 || true; exit 0' TERM INT
  while true; do
    sleep 15 & wait $!
    if ! vault_configured; then
      log "Vault was restarted and lost its configuration; configuring it again"
      vault_setup || log "configuring Vault failed; trying again shortly"
    fi
    [ -f /tmp/signin-as ] && current="$(cat /tmp/signin-as)"
    announce "$current" 2>/dev/null || true   # the stand-in forgets the account when it restarts
  done
}

case "${1:-}" in
  "") main ;;
  token) token "${2:-alice}" ;;
  kubeconfig)
    if [ "${2:-}" = "--host" ]; then kind get kubeconfig --name "$CLUSTER"; else cat "$KUBECONFIG"; fi ;;
  signin-as)
    account "${2:?alice or bob}" >/dev/null
    announce "$2"
    echo "$2" > /tmp/signin-as
    echo "The next sign-in signs in as $2. Sign out in the console first if you are signed in." ;;
  destroy) kind delete cluster --name "$CLUSTER" ;;
  *) echo "usage: toolbox.sh [token [user]|kubeconfig [--host]|signin-as <user>|destroy]" >&2; exit 2 ;;
esac
