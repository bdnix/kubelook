#!/usr/bin/env bash
# Helpers for the integration environment scripts. Source this file; do not run it.

# wait_http <url> <seconds>: a bounded retry on a condition, not a fixed sleep. Each attempt is bounded too: without
# --connect-timeout, a connection to a pod that has just gone away waits for the kernel's SYN retries (about 127 s
# with Linux's default tcp_syn_retries=6) before curl retries.
wait_http() {
  curl -fsS --connect-timeout 2 --max-time 5 --retry "$2" --retry-delay 1 --retry-max-time "$2" \
    --retry-connrefused --retry-all-errors -o /dev/null "$1"
}

# container_running <name>: true when the container exists and runs.
container_running() {
  [ "$(docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null || true)" = "true" ]
}

# wait_pg <container> <seconds>: waits until PostgreSQL in the container accepts connections.
wait_pg() {
  local i
  for i in $(seq 1 "$2"); do
    if docker exec "$1" pg_isready -U postgres >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo "PostgreSQL in $1 did not become ready in $2 s" >&2
  return 1
}

# remove_container <name>: removes a container, ignoring "no such container".
remove_container() {
  docker rm -f "$1" >/dev/null 2>&1 || true
}
