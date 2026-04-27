#!/usr/bin/env bash
set -euo pipefail

: "${GHCR_USERNAME:?GHCR_USERNAME is required}"
: "${GHCR_TOKEN:?GHCR_TOKEN is required}"

SECRET_NAME="ghcr-pull-secret"
NAMESPACES=(kickfix asylguiden goodtribes)

echo "=== Creating GHCR pull secret in all namespaces ==="
for ns in "${NAMESPACES[@]}"; do
  echo "  -> $ns"
  kubectl create secret docker-registry "$SECRET_NAME" \
    --docker-server=ghcr.io \
    --docker-username="$GHCR_USERNAME" \
    --docker-password="$GHCR_TOKEN" \
    --namespace="$ns" \
    --dry-run=client -o yaml | kubectl apply -f -
done

echo ""
echo "Done. Verify with:"
echo "  kubectl get secret $SECRET_NAME -n kickfix"
