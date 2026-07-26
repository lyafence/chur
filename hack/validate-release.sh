#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-}"
CHART="${2:-oci://ghcr.io/${GITHUB_REPOSITORY_OWNER:-lyafence}/charts/chur}"
[ -z "$VERSION" ] && { echo "Usage: $0 <version> [chart-source]"; exit 1; }

CLUSTER="chur-validate"
NS="chur-val-$(date +%s)"

cleanup() {
	kubectl delete namespace "$NS" --ignore-not-found >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "=== Preparing test fixtures on node ==="
NODE=$(kind get nodes --name "$CLUSTER" | head -1)
kubectl debug "node/$NODE" --image=busybox --quiet -- chroot /host sh -c '
  mkdir -p /etc/chur/secrets /var/lib/chur-keeper/secrets/prod
  echo -n "hello-local" > /etc/chur/secrets/local-ref
  echo -n "hello-keeper" > /var/lib/chur-keeper/secrets/prod/password
' 2>/dev/null || true

echo "=== Installing chur $VERSION ==="
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS" create secret generic test-secret --from-literal=token=hello

if [[ "$CHART" == oci://* ]]; then
	helm install chur "$CHART" --version "$VERSION" \
		--set tls.provider=helmGenerated \
		--set webhook.allowKeeperSkipVerify=true \
		--set-json 'rbac.namespaces=["'"$NS"'"]' \
		--wait --timeout 180s
else
	helm install chur "$CHART" \
		--set image.tag="$VERSION" \
		--set initImage.tag="$VERSION" \
		--set keeper.image.tag="$VERSION" \
		--set tls.provider=helmGenerated \
		--set webhook.allowKeeperSkipVerify=true \
		--set-json 'rbac.namespaces=["'"$NS"'"]' \
		--wait --timeout 180s
fi || {
		rc=$?
		echo "=== Helm install failed. Pod status: ==="
		kubectl describe pod -l app.kubernetes.io/name=chur-webhook -n default 2>&1 | tail -30
		exit $rc
}

echo "=== Waiting for webhook to be ready ==="
kubectl wait --for=condition=Available deployment/chur-webhook -n default --timeout=60s

test_pod() {
	local name="$1" path="$2" expected="$3" annotations="$4"
	local retries=10
	for i in $(seq 1 $retries); do
		if kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-$name
  namespace: $NS
  annotations:
$annotations
spec:
  serviceAccountName: chur-init
  containers:
  - name: app
    image: busybox
    command: ["sleep", "30"]
EOF
		then
			break
		fi
		[ $i -lt $retries ] && sleep 3
	done
	kubectl wait --for=condition=Ready "pod/test-$name" -n "$NS" --timeout=60s
	result=$(kubectl exec "test-$name" -n "$NS" -- cat "$path")
	[ "$result" = "$expected" ] || { echo "FAIL $name: $result"; exit 1; }
	echo "PASS $name"
}

test_pod k8s   /secrets/test-secret      hello \
    "    chur.io/provider: k8s
    chur.io/secret-ref: test-secret
    chur.io/secret-key: token"

test_pod env   /secrets/CHUR_TEST_ENV_VAL hello-env \
    "    chur.io/provider: env
    chur.io/secret-ref: CHUR_TEST_ENV_VAL
    chur.io/provider-env: '{\"CHUR_TEST_ENV_VAL\":\"hello-env\"}'"

test_pod local /secrets/local-ref         hello-local \
    "    chur.io/provider: local
    chur.io/secret-ref: local-ref"

echo "=== Enable keeper ==="
if [[ "$CHART" == oci://* ]]; then
	helm upgrade chur "$CHART" --version "$VERSION" \
		--set tls.provider=helmGenerated \
		--set webhook.allowKeeperSkipVerify=true \
		--set-json 'rbac.namespaces=["'"$NS"'"]' \
		--set keeper.enabled=true \
		--set keeper.backend=filesystem \
		--set-json 'keeper.extraVolumes=[{"name":"tmp","emptyDir":{}}]' \
		--set-json 'keeper.extraVolumeMounts=[{"name":"tmp","mountPath":"/tmp"}]' \
		--wait --timeout 180s
else
	helm upgrade chur "$CHART" \
		--set image.tag="$VERSION" \
		--set initImage.tag="$VERSION" \
		--set keeper.image.tag="$VERSION" \
		--set tls.provider=helmGenerated \
		--set webhook.allowKeeperSkipVerify=true \
		--set-json 'rbac.namespaces=["'"$NS"'"]' \
		--set keeper.enabled=true \
		--set keeper.backend=filesystem \
		--set-json 'keeper.extraVolumes=[{"name":"tmp","emptyDir":{}}]' \
		--set-json 'keeper.extraVolumeMounts=[{"name":"tmp","mountPath":"/tmp"}]' \
		--wait --timeout 180s
fi || {
	rc=$?
	echo "=== Keeper upgrade failed. Pod status: ==="
	kubectl describe pod -l app.kubernetes.io/name=chur-keeper -n default 2>&1 | tail -20
	exit $rc
}

kubectl wait --for=condition=Available deployment/chur-keeper -n default --timeout=60s

kubectl rollout restart deployment/chur-webhook -n default 2>/dev/null
kubectl wait --for=condition=Available deployment/chur-webhook -n default --timeout=60s

test_pod keeper /secrets/prod/password    hello-keeper \
    "    chur.io/provider: keeper
    chur.io/secret-ref: prod/password
    chur.io/keeper-skip-verify: \"true\""

echo "=== All 4 providers PASSED ==="
