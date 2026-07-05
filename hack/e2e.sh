#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-dev}"
E2E_CLUSTER="${2:-chur-e2e}"
SKIP_CLEANUP="${3:-false}"
E2E_NAMESPACE="chur-e2e-tests-$(date +%s)"
APP_NAME="chur"

DOCKER=$(command -v podman 2>/dev/null || echo docker)
[ "$DOCKER" = "podman" ] && export KIND_EXPERIMENTAL_PROVIDER=podman

TMPDIR=$(mktemp -d)

cleanup() {
	local exit_code=$?
	echo "Cleaning up test namespace $E2E_NAMESPACE..."
	kubectl delete namespace "$E2E_NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
	if [ "$SKIP_CLEANUP" != "true" ]; then
		echo "Cleaning up Kind cluster..."
		kind delete cluster --name "$E2E_CLUSTER" >/dev/null 2>&1 || true
	fi
	rm -rf "$TMPDIR"
	exit "$exit_code"
}
trap cleanup EXIT

command -v kind >/dev/null || { echo "ERROR: kind is required"; exit 1; }
command -v kubectl >/dev/null || { echo "ERROR: kubectl is required"; exit 1; }
command -v helm >/dev/null || { echo "ERROR: helm is required"; exit 1; }

if kind get clusters | grep -q "^$E2E_CLUSTER$"; then
	echo "Kind cluster $E2E_CLUSTER already exists, reusing it..."
	kubectl config use-context "kind-$E2E_CLUSTER"
else
	echo "Creating Kind cluster..."
	systemd-run --user --scope -q -p Delegate=yes -- \
		kind create cluster --name "$E2E_CLUSTER" --wait 60s
fi

echo "Loading images into Kind..."
WEBHOOK_IMAGE="$APP_NAME-webhook:dev"
INIT_IMAGE="$APP_NAME-init:dev"

$DOCKER tag "$APP_NAME-webhook:$VERSION" "$WEBHOOK_IMAGE"
$DOCKER tag "$APP_NAME-init:$VERSION" "$INIT_IMAGE"

if [ "$DOCKER" = "docker" ]; then
	kind load docker-image "$WEBHOOK_IMAGE" --name "$E2E_CLUSTER"
	kind load docker-image "$INIT_IMAGE" --name "$E2E_CLUSTER"
else
	$DOCKER tag "$WEBHOOK_IMAGE" "docker.io/library/$WEBHOOK_IMAGE"
	$DOCKER tag "$INIT_IMAGE" "docker.io/library/$INIT_IMAGE"

	$DOCKER save -o "$TMPDIR/webhook.tar" "docker.io/library/$WEBHOOK_IMAGE"
	$DOCKER save -o "$TMPDIR/init.tar" "docker.io/library/$INIT_IMAGE"

	kind load image-archive "$TMPDIR/webhook.tar" --name "$E2E_CLUSTER"
	kind load image-archive "$TMPDIR/init.tar" --name "$E2E_CLUSTER"
fi

echo "Creating test namespace $E2E_NAMESPACE..."
kubectl create namespace "$E2E_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

echo "Installing chur via Helm..."
helm uninstall chur --ignore-not-found >/dev/null 2>&1 || true
helm install chur ./charts/chur \
  --set image.repository=chur-webhook \
  --set image.tag=dev \
  --set initImage.repository=chur-init \
  --set initImage.tag=dev \
  --set tls.provider=helmGenerated \
  --set-json 'rbac.namespaces=["'"$E2E_NAMESPACE"'"]' \
  --wait --timeout 120s

echo "Running E2E tests..."
CHUR_E2E_NAMESPACE="$E2E_NAMESPACE" go test -tags e2e -count=1 -timeout=300s -v ./test/e2e/
