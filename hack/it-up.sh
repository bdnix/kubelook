#!/usr/bin/env bash
# Creates the integration environment idempotently:
#   - alpha (Kubernetes 1.35) and beta (1.37): the clusters Kubelook manages in the tests;
#   - host: a third kind cluster that runs Kubelook from its Helm chart: one replica on SQLite, or with IT_DATABASE=postgres
#     two replicas on PostgreSQL (hack/it-lib.sh, it_database);
#   - Vault, PostgreSQL (only with IT_DATABASE=postgres) and the identity stand-in (which also serves the test chart
#     repository) as containers.
# The image under test is the archive in KUBELOOK_IMAGE_ARCHIVE (CI passes the one its build job made), or else the
# local kubelook:dev, built when missing.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; export PATH="$ROOT/bin:$PATH"
source "$ROOT/hack/it-lib.sh"
DATABASE="$(it_database)"
mkdir -p "$ROOT/.it"

# Asked of the Docker daemon, whose host runs the kind nodes (on macOS and Windows that is a VM, not this machine).
if [ "$(docker info --format '{{.CgroupVersion}}' 2>/dev/null || true)" != "2" ]; then
  echo "The Docker host does not use cgroup v2. The Kubernetes 1.35 node of kind never becomes ready on cgroup v1." >&2
  echo "Use a Docker host with cgroup v2 (check: docker info --format '{{.CgroupVersion}}' prints 2)." >&2
  exit 1
fi

# 1. The image under test.
if [ -n "${KUBELOOK_IMAGE_ARCHIVE:-}" ]; then
  docker load -q -i "$KUBELOOK_IMAGE_ARCHIVE" >/dev/null
elif ! docker image inspect kubelook:dev >/dev/null 2>&1; then
  make -C "$ROOT" image
fi
docker image inspect kubelook:dev >/dev/null || { echo "no image kubelook:dev" >&2; exit 1; }

# 2. The three clusters, in parallel. kind creates the Docker network "kind" with the first cluster; the others
#    start once it exists, so two creations never race for it.
create_cluster() {
  local c="$1"
  kind get clusters | grep -qx "kubelook-it-$c" || \
    kind create cluster --name "kubelook-it-$c" --config "$ROOT/test/integration/kind/$c.yaml" --wait 180s >"$ROOT/.it/kind-$c.log" 2>&1
}
pids=()
create_cluster host & pids+=($!)
for _ in $(seq 1 120); do docker network inspect kind >/dev/null 2>&1 && break; sleep 1; done
docker network inspect kind >/dev/null 2>&1 || { cat "$ROOT/.it/kind-host.log" >&2; exit 1; }
create_cluster alpha & pids+=($!)
create_cluster beta & pids+=($!)

# 3. Vault, PostgreSQL and the identity stand-in, while the clusters start.
container_running kubelook-it-vault || { remove_container kubelook-it-vault; docker run -d --name kubelook-it-vault --network kind --cap-add IPC_LOCK \
  -p 127.0.0.1:18200:8200 -e VAULT_DEV_ROOT_TOKEN_ID=root -e VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200 \
  hashicorp/vault:2.2.2@sha256:dfa48c2fa2b0099ccb47681ec5c4804bf5aec9d6879ecfee07ad4b92a5427ad6 >/dev/null; }
if [ "$DATABASE" = postgres ]; then
  container_running kubelook-it-postgres || { remove_container kubelook-it-postgres; docker run -d --name kubelook-it-postgres --network kind \
    -p 127.0.0.1:15432:5432 -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=kubelook postgres:18.7-trixie >/dev/null; }
fi
"$ROOT/hack/it-chartrepo.sh"
docker build -q -t kubelook-fakeidp:dev -f "$ROOT/test/integration/fakeidp/Dockerfile" "$ROOT" >/dev/null
remove_container kubelook-it-fakeidp   # always recreated: the charts mount and the image may have changed
docker run -d --name kubelook-it-fakeidp --network kind -v "$ROOT/.it/charts:/charts:ro" \
  -p 127.0.0.1:8300:8300 kubelook-fakeidp:dev >/dev/null
for img in busybox:1.37 registry.k8s.io/pause:3.10; do
  docker image inspect "$img" >/dev/null 2>&1 || docker pull -q "$img" >/dev/null
done
wait_http http://127.0.0.1:18200/v1/sys/health 60
if [ "$DATABASE" = postgres ]; then wait_pg kubelook-it-postgres 60; fi
wait_http http://127.0.0.1:8300/healthz 30

for p in "${pids[@]}"; do
  if ! wait "$p"; then cat "$ROOT"/.it/kind-*.log >&2; exit 1; fi
done
for c in alpha beta host; do
  kind get kubeconfig --name "kubelook-it-$c" > "$ROOT/.it/kubeconfig-$c-host.yaml"                  # kubectl on the host
done
for c in alpha beta; do
  kind get kubeconfig --name "kubelook-it-$c" --internal > "$ROOT/.it/kubeconfig-$c-internal.yaml"   # uploaded to Kubelook in tests
  for img in busybox:1.37 registry.k8s.io/pause:3.10; do kind load docker-image "$img" --name "kubelook-it-$c" >/dev/null; done
done

# 4. Vault content and fixtures (the clusters under test), then Kubelook in the host cluster.
"$ROOT/hack/vault-setup.sh"
"$ROOT/hack/it-kubelook.sh" install
echo "IT UP"
