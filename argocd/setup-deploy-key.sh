#!/usr/bin/env bash
set -euo pipefail

KEY_FILE="argocd-deploy-key"

echo "=== 1. Generate SSH key pair ==="
ssh-keygen -t ed25519 -C "argocd@goodtribes-org/deploy" -f "$KEY_FILE" -N ""

echo ""
echo "=== 2. Add the PUBLIC key as a read-only deploy key on GitHub ==="
echo "Go to: https://github.com/goodtribes-org/deploy/settings/keys/new"
echo "Title: argocd-read"
echo "Allow write access: NO"
echo ""
echo "Public key to paste:"
cat "${KEY_FILE}.pub"

echo ""
echo "=== 3. Create the ArgoCD repository secret in your cluster ==="
PRIVATE_KEY=$(cat "$KEY_FILE")
kubectl create secret generic goodtribes-deploy-repo \
  --namespace argocd \
  --from-literal=type=git \
  --from-literal=url=git@github.com:goodtribes-org/deploy.git \
  --from-literal=sshPrivateKey="$PRIVATE_KEY" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl label secret goodtribes-deploy-repo \
  --namespace argocd \
  argocd.argoproj.io/secret-type=repository \
  --overwrite

echo ""
echo "=== 4. Bootstrap ArgoCD ==="
echo "Copy argocd/ into the goodtribes-org/deploy repo root, then run:"
echo "  kubectl apply -f argocd/repo-secret.yaml  # or use step 3 above"
echo "  kubectl apply -f argocd/bootstrap.yaml"
echo ""
echo "ArgoCD will then pick up argocd/apps/*.yaml and create the three Application objects."
echo ""
echo "Done. You can delete the key files now:"
echo "  rm $KEY_FILE ${KEY_FILE}.pub"
