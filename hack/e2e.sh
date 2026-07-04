#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-dev}"
E2E_CLUSTER="${2:-chur-e2e}"
APP_NAME="chur"

DOCKER=$(command -v podman 2>/dev/null || echo docker)
[ "$DOCKER" = "podman" ] && export KIND_EXPERIMENTAL_PROVIDER=podman

TMPDIR=$(mktemp -d)

cleanup() {
	local exit_code=$?
	echo "Cleaning up Kind cluster..."
	kind delete cluster --name "$E2E_CLUSTER" >/dev/null 2>&1 || true
	rm -rf "$TMPDIR"
	exit "$exit_code"
}
trap cleanup EXIT

command -v kind >/dev/null || { echo "ERROR: kind is required"; exit 1; }
command -v kubectl >/dev/null || { echo "ERROR: kubectl is required"; exit 1; }

echo "Creating Kind cluster..."
systemd-run --user --scope -q -p Delegate=yes -- \
	kind create cluster --name "$E2E_CLUSTER" --wait 60s

echo "Loading images into Kind..."
WEBHOOK_IMAGE="$APP_NAME-webhook:dev"
INIT_IMAGE="$APP_NAME-init:dev"

$DOCKER tag "$APP_NAME-webhook:$VERSION" "$WEBHOOK_IMAGE"
$DOCKER tag "$APP_NAME-init:$VERSION" "$INIT_IMAGE"

if [ "$DOCKER" = "docker" ]; then
	kind load docker-image "$WEBHOOK_IMAGE" --name "$E2E_CLUSTER"
	kind load docker-image "$INIT_IMAGE" --name "$E2E_CLUSTER"
else
	# Podman stores short names as localhost/, but kubelet resolves them to
	# docker.io/library/. Tag with the full docker.io path before saving.
	$DOCKER tag "$WEBHOOK_IMAGE" "docker.io/library/$WEBHOOK_IMAGE"
	$DOCKER tag "$INIT_IMAGE" "docker.io/library/$INIT_IMAGE"

	$DOCKER save -o "$TMPDIR/webhook.tar" "docker.io/library/$WEBHOOK_IMAGE"
	$DOCKER save -o "$TMPDIR/init.tar" "docker.io/library/$INIT_IMAGE"

	kind load image-archive "$TMPDIR/webhook.tar" --name "$E2E_CLUSTER"
	kind load image-archive "$TMPDIR/init.tar" --name "$E2E_CLUSTER"
fi

echo "Running E2E tests..."
go test -tags e2e -count=1 -timeout=300s -v ./test/e2e/
