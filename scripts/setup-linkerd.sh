#!/usr/bin/env bash
# scripts/setup-linkerd.sh
#
# Installs Linkerd control plane into a local k3d cluster and deploys loom.
#
# Prerequisites:
#   - Docker running
#   - k3d v5+ installed  (brew install k3d  or  curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash)
#   - kubectl installed
#   - linkerd CLI installed (see below for auto-install)
#   - make build  run first so bin/loom exists
#
# Usage:
#   ./scripts/setup-linkerd.sh [--skip-cluster] [--skip-linkerd] [--skip-deploy]

set -euo pipefail

CLUSTER_NAME="loom-dev"
REGISTRY_NAME="k3d-registry.localhost"
REGISTRY_PORT="5001"
IMAGE_TAG="dev"
LINKERD_VERSION="stable-2.14.10"

info()  { echo "[INFO]  $*"; }
warn()  { echo "[WARN]  $*" >&2; }
die()   { echo "[ERROR] $*" >&2; exit 1; }

SKIP_CLUSTER=false
SKIP_LINKERD=false
SKIP_DEPLOY=false

for arg in "$@"; do
  case "$arg" in
    --skip-cluster) SKIP_CLUSTER=true ;;
    --skip-linkerd) SKIP_LINKERD=true ;;
    --skip-deploy)  SKIP_DEPLOY=true ;;
    *) die "Unknown argument: $arg" ;;
  esac
done

# ── 1. Install Linkerd CLI if missing ─────────────────────────────────────
install_linkerd_cli() {
  if command -v linkerd &>/dev/null; then
    info "linkerd CLI already installed: $(linkerd version --client --short 2>/dev/null || echo 'unknown')"
    return
  fi
  info "Installing Linkerd CLI ${LINKERD_VERSION}..."
  curl -fsL https://run.linkerd.io/install | LINKERD2_VERSION="${LINKERD_VERSION}" sh
  export PATH="${HOME}/.linkerd2/bin:${PATH}"
  info "Linkerd CLI installed: $(linkerd version --client --short)"
}

# ── 2. Create k3d cluster with local registry ─────────────────────────────
create_cluster() {
  if k3d cluster list | grep -q "^${CLUSTER_NAME}"; then
    info "Cluster '${CLUSTER_NAME}' already exists, skipping creation."
    return
  fi

  info "Creating k3d cluster '${CLUSTER_NAME}' with local registry..."
  k3d registry create "${REGISTRY_NAME}" --port "${REGISTRY_PORT}" 2>/dev/null || true

  k3d cluster create "${CLUSTER_NAME}" \
    --registry-use "k3d-${REGISTRY_NAME}:${REGISTRY_PORT}" \
    --port "30081:30081@loadbalancer" \
    --port "30090:30090@loadbalancer" \
    --agents 2 \
    --wait

  info "Cluster ready. Switching kubectl context..."
  k3d kubeconfig merge "${CLUSTER_NAME}" --kubeconfig-merge-default --switch-context
}

# ── 3. Install Linkerd control plane ──────────────────────────────────────
install_linkerd() {
  if kubectl get namespace linkerd &>/dev/null; then
    info "Linkerd namespace exists, skipping control plane install."
    return
  fi

  info "Pre-checking cluster readiness for Linkerd..."
  linkerd check --pre || die "Pre-check failed. Fix issues before continuing."

  info "Installing Linkerd CRDs..."
  linkerd install --crds | kubectl apply -f -

  info "Installing Linkerd control plane..."
  linkerd install \
    --set proxyInit.runAsRoot=true \
    | kubectl apply -f -

  info "Waiting for Linkerd control plane to be ready..."
  linkerd check

  info "Installing Linkerd viz extension (dashboard + metrics)..."
  linkerd viz install | kubectl apply -f -
  linkerd viz check

  info "Linkerd control plane installed successfully."
}

# ── 4. Build and push loom image ──────────────────────────────────────────
push_image() {
  info "Building loom Docker image..."
  docker build -t "loom:${IMAGE_TAG}" .

  info "Tagging and pushing to local registry..."
  docker tag "loom:${IMAGE_TAG}" "${REGISTRY_NAME}:${REGISTRY_PORT}/loom:${IMAGE_TAG}"
  docker push "${REGISTRY_NAME}:${REGISTRY_PORT}/loom:${IMAGE_TAG}"
  info "Image pushed: ${REGISTRY_NAME}:${REGISTRY_PORT}/loom:${IMAGE_TAG}"
}

# ── 5. Deploy loom to cluster ─────────────────────────────────────────────
deploy_loom() {
  info "Applying Kubernetes manifests (base + local overlay)..."
  kubectl apply -k deploy/k8s/overlays/local

  info "Waiting for loom deployment to roll out..."
  kubectl -n loom rollout status deployment/loom --timeout=300s

  info "Checking Linkerd mesh status for loom namespace..."
  linkerd -n loom check --proxy || warn "Some proxy checks failed (expected if pods are still starting)"

  info ""
  info "=== Deployment complete! ==="
  info "Loom HTTP API: http://localhost:30081"
  info "Loom gRPC:     localhost:30090"
  info ""
  info "Useful commands:"
  info "  kubectl -n loom get pods"
  info "  linkerd -n loom stat deploy"
  info "  linkerd viz dashboard &   (opens Linkerd dashboard in browser)"
  info "  linkerd -n loom tap deploy/loom   (live traffic tap)"
}

# ── Main ───────────────────────────────────────────────────────────────────
install_linkerd_cli

if [ "${SKIP_CLUSTER}" = "false" ]; then
  create_cluster
fi

if [ "${SKIP_LINKERD}" = "false" ]; then
  install_linkerd
fi

if [ "${SKIP_DEPLOY}" = "false" ]; then
  push_image
  deploy_loom
fi
