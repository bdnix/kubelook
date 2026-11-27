#!/usr/bin/env bash
# Pins the default debug image of the Helm chart (debug.image in deploy/helm/kubelook/values.yaml) to the digest
# of ghcr.io/bdnix/kubelook-netshoot:<tag>, where <tag> is the tag of the FROM line of build/netshoot/Dockerfile.
# Run it after the `netshoot` workflow has published a new tag (a Dependabot pull request changed the FROM line).
#
#   hack/pin-netshoot.sh                          resolves the digest with `docker buildx imagetools inspect`
#   NETSHOOT_DIGEST=sha256:<64 hex> hack/pin-netshoot.sh   writes that digest without contacting the registry
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VALUES="$ROOT/deploy/helm/kubelook/values.yaml"
TAG="$(sed -n 's/^FROM nicolaka\/netshoot:\([^@ ]*\).*/\1/p' "$ROOT/build/netshoot/Dockerfile")"
[ -n "$TAG" ] || { echo "pin-netshoot: no FROM nicolaka/netshoot:<tag> line in build/netshoot/Dockerfile" >&2; exit 1; }

if [ -n "${NETSHOOT_DIGEST:-}" ]; then
  DIGEST="$NETSHOOT_DIGEST"
else
  DIGEST="$(docker buildx imagetools inspect "ghcr.io/bdnix/kubelook-netshoot:$TAG" --format '{{json .Manifest.Digest}}' | tr -d '"')"
fi
[[ "$DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "pin-netshoot: not a digest: $DIGEST" >&2; exit 1; }

tmp="$(mktemp)"
sed -E "s#^(  image: )ghcr\.io/bdnix/kubelook-netshoot[@:][^ ]+\$#\1ghcr.io/bdnix/kubelook-netshoot@$DIGEST#" "$VALUES" >"$tmp"
cat "$tmp" >"$VALUES"   # not mv: keeps the mode and any link of the file
rm -f "$tmp"
grep -qx "  image: ghcr.io/bdnix/kubelook-netshoot@$DIGEST" "$VALUES" || { echo "pin-netshoot: debug.image not found in $VALUES" >&2; exit 1; }
echo "debug.image is ghcr.io/bdnix/kubelook-netshoot@$DIGEST (from tag $TAG)"
