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

create_kind_cluster() {
	if command -v systemd-run >/dev/null 2>&1 && \
	   systemctl --user is-system-running >/dev/null 2>&1; then
		systemd-run --user --scope -q -p Delegate=yes -- \
			kind create cluster --name "$E2E_CLUSTER" --wait 60s
	else
		kind create cluster --name "$E2E_CLUSTER" --wait 60s
	fi
}

if kind get clusters | grep -q "^$E2E_CLUSTER$"; then
	echo "Kind cluster $E2E_CLUSTER already exists, reusing it..."
	kubectl config use-context "kind-$E2E_CLUSTER"
else
	echo "Creating Kind cluster..."
	create_kind_cluster
fi

echo "Loading images into Kind..."
WEBHOOK_IMAGE="$APP_NAME-webhook:dev"
INIT_IMAGE="$APP_NAME-init:dev"
KEEPER_IMAGE="$APP_NAME-keeper:dev"

$DOCKER tag "$APP_NAME-webhook:$VERSION" "$WEBHOOK_IMAGE"
$DOCKER tag "$APP_NAME-init:$VERSION" "$INIT_IMAGE"
$DOCKER tag "$APP_NAME-keeper:$VERSION" "$KEEPER_IMAGE"

if [ "$DOCKER" = "docker" ]; then
	kind load docker-image "$WEBHOOK_IMAGE" --name "$E2E_CLUSTER"
	kind load docker-image "$INIT_IMAGE" --name "$E2E_CLUSTER"
	kind load docker-image "$KEEPER_IMAGE" --name "$E2E_CLUSTER"
else
	$DOCKER tag "$WEBHOOK_IMAGE" "docker.io/library/$WEBHOOK_IMAGE"
	$DOCKER tag "$INIT_IMAGE" "docker.io/library/$INIT_IMAGE"
	$DOCKER tag "$KEEPER_IMAGE" "docker.io/library/$KEEPER_IMAGE"

	$DOCKER save -o "$TMPDIR/webhook.tar" "docker.io/library/$WEBHOOK_IMAGE"
	$DOCKER save -o "$TMPDIR/init.tar" "docker.io/library/$INIT_IMAGE"
	$DOCKER save -o "$TMPDIR/keeper.tar" "docker.io/library/$KEEPER_IMAGE"

	kind load image-archive "$TMPDIR/webhook.tar" --name "$E2E_CLUSTER"
	kind load image-archive "$TMPDIR/init.tar" --name "$E2E_CLUSTER"
	kind load image-archive "$TMPDIR/keeper.tar" --name "$E2E_CLUSTER"
fi

echo "Preparing local provider test files on Kind node..."
NODE_NAME=$(kind get nodes --name "$E2E_CLUSTER" | head -n 1)
$DOCKER exec "$NODE_NAME" mkdir -p /etc/chur/secrets
$DOCKER exec "$NODE_NAME" sh -c 'echo -n "e2e-local-secret-value-12345" > /etc/chur/secrets/e2e-local-secret'
$DOCKER exec "$NODE_NAME" dd if=/dev/zero of=/etc/chur/secrets/e2e-large-secret bs=1024 count=1100 status=none

echo "Preparing keeper secret files on Kind node..."
$DOCKER exec "$NODE_NAME" sh -c 'mkdir -p /var/lib/chur-keeper/secrets/prod/db && echo -n "e2e-keeper-secret-value" > /var/lib/chur-keeper/secrets/prod/db/password'

echo "Creating test namespace $E2E_NAMESPACE..."
kubectl create namespace "$E2E_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

echo "Installing chur via Helm..."
helm uninstall chur --ignore-not-found >/dev/null 2>&1 || true
helm install chur ./charts/chur \
  --set image.repository=chur-webhook \
  --set image.tag=dev \
  --set initImage.repository=chur-init \
  --set initImage.tag=dev \
  --set keeper.image.repository=chur-keeper \
  --set keeper.image.tag=dev \
  --set keeper.enabled=true \
  --set keeper.backend=filesystem \
  --set-json 'keeper.extraVolumes=[{"name":"tmp","emptyDir":{}}]' \
  --set-json 'keeper.extraVolumeMounts=[{"name":"tmp","mountPath":"/tmp"}]' \
  --set tls.provider=helmGenerated \
  --set-json 'rbac.namespaces=["'"$E2E_NAMESPACE"'"]' \
  --wait --timeout 120s

echo "Running E2E tests..."
CHUR_E2E_NAMESPACE="$E2E_NAMESPACE" go test -tags e2e -count=1 -timeout=300s -v ./test/e2e/
