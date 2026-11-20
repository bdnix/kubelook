#!/usr/bin/env bash
# Creates the integration environment idempotently: two kind clusters, Vault, PostgreSQL, the
# identity stand-in, the Vault content and the Kubelook containers. See PLAN.md Step 7.1.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
source "$ROOT/hack/it-lib.sh"
mkdir -p "$ROOT/.it"

if [ "$(stat -fc %T /sys/fs/cgroup 2>/dev/null || true)" != "cgroup2fs" ]; then
  echo "This host does not use cgroup v2. The Kubernetes 1.35 node of kind never becomes ready on cgroup v1." >&2
  echo "Run the integration environment on a cgroup v2 host (check: stat -fc %T /sys/fs/cgroup prints cgroup2fs)." >&2
  exit 1
fi

# 1. Clusters (kind creates the Docker network "kind", so they come first).
for c in alpha beta; do
  kind get clusters | grep -qx "kubelook-it-$c" || \
    kind create cluster --name "kubelook-it-$c" --config "$ROOT/test/integration/kind/$c.yaml" --wait 180s
  kind get kubeconfig --name "kubelook-it-$c" > "$ROOT/.it/kubeconfig-$c-host.yaml"                 # kubectl on the host
  kind get kubeconfig --name "kubelook-it-$c" --internal > "$ROOT/.it/kubeconfig-$c-internal.yaml"  # uploaded to Kubelook in tests
done

# 2. Vault (dev mode, root token "root")
container_running kubelook-it-vault || { remove_container kubelook-it-vault; docker run -d --name kubelook-it-vault --network kind --cap-add IPC_LOCK \
  -p 127.0.0.1:18200:8200 -e VAULT_DEV_ROOT_TOKEN_ID=root -e VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200 \
  hashicorp/vault:2.2.1@sha256:962a92876e168d68eaaa24b0b8b49bbce1cb63024060a67bba95cd158a366fc2 >/dev/null; }
wait_http http://127.0.0.1:18200/v1/sys/health 60

# 3. PostgreSQL
container_running kubelook-it-postgres || { remove_container kubelook-it-postgres; docker run -d --name kubelook-it-postgres --network kind \
  -p 127.0.0.1:15432:5432 -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=kubelook postgres:18.6-trixie >/dev/null; }
wait_pg kubelook-it-postgres 60

# 4. Identity stand-in
docker build -q -t kubelook-fakeidp:dev -f "$ROOT/test/integration/fakeidp/Dockerfile" "$ROOT" >/dev/null
container_running kubelook-it-fakeidp || { remove_container kubelook-it-fakeidp; docker run -d --name kubelook-it-fakeidp --network kind \
  -p 127.0.0.1:8300:8300 kubelook-fakeidp:dev >/dev/null; }
wait_http http://127.0.0.1:8300/healthz 30

# 4b. Pre-load the small workload images so tests never wait on a registry.
for img in busybox:1.37 registry.k8s.io/pause:3.10; do
  docker image inspect "$img" >/dev/null 2>&1 || docker pull -q "$img" >/dev/null
  for c in alpha beta; do kind load docker-image "$img" --name "kubelook-it-$c" >/dev/null; done
done

# 5. Vault content and test fixtures (Step 7.2), Kubelook containers (Step 7.3)
"$ROOT/hack/vault-setup.sh"
"$ROOT/hack/it-apps.sh"
echo "IT UP"
