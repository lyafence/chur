#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-}"

# GITHUB_REPOSITORY_OWNER is set by GitHub Actions.
# Auto-detect from git remote when running locally.
if [[ -z "${GITHUB_REPOSITORY_OWNER:-}" ]]; then
	REPO_OWNER=$(git remote get-url origin 2>/dev/null | sed -n 's|.*[:/]\([^/]\+\)/chur.*|\1|p')
	REPO_OWNER="${REPO_OWNER:-lyafence}"
else
	REPO_OWNER="${GITHUB_REPOSITORY_OWNER}"
fi
CHART="oci://ghcr.io/${REPO_OWNER}/charts/chur"

[ -z "$VERSION" ] && {
	echo "Usage: $0 <version>"
	echo ""
	echo "Example: $0 0.7.1"
	echo ""
	echo "OCI chart source: $CHART"
	echo "Prerequisites: kind cluster 'chur-validate' must exist."
	echo "Use 'kind create cluster --name chur-validate' first."
	exit 1
}

CLUSTER="${CHUR_VALIDATE_CLUSTER:-chur-validate}"
NS="chur-val-$(date +%s)"
DEBUG="${CHUR_DEBUG:-}"

[[ -n "$DEBUG" ]] && set -x

# ── helpers ────────────────────────────────────────────────────────────────

# GitHub Actions workflow commands. In CI these create collapsible sections.
# Locally they render as plain text — zero side effects.
_section() { echo -e "\n::group::=== $* ==="; }
_endsection() { echo "::endgroup::"; }
_error()   { echo "::error::$*"; }
_warn()    { echo "::warning::$*"; }

# Cluster diagnostics dump. Best-effort — does not fail.
_dump_state() {
	echo ""
	echo "══════════════════════════════════════════════════════════"
	echo "   D I A G N O S T I C S   $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	echo "══════════════════════════════════════════════════════════"
	echo ""
	echo "━━━ Pods in namespace $NS ━━━"
	kubectl get pods -n "$NS" --show-labels 2>/dev/null || true
	echo ""
	echo "━━━ chur pods (all namespaces) ━━━"
	kubectl get pods -A -l 'app.kubernetes.io/part-of in (chur,chur-keeper)' 2>/dev/null || true
	echo ""
	echo "━━━ Events in namespace $NS ━━━"
	kubectl get events -n "$NS" --sort-by=.lastTimestamp 2>/dev/null | tail -20 || true
	echo ""
	echo "━━━ Webhook logs ━━━"
	kubectl logs deployment/chur-webhook -n default --all-containers --tail=50 2>/dev/null || true
	echo ""
	echo "━━━ Keeper logs ━━━"
	kubectl logs deployment/chur-keeper -n default --all-containers --tail=50 2>/dev/null || true
	echo ""
	echo "══════════════════════════════════════════════════════════"
}

# ERR trap — fires only when a command fails (set -e).
# Dumps cluster diagnostics before exit.
_err_report() {
	local line=$1
	_error "validate-release.sh FATAL at line $line"
	_dump_state
	exit 1
}
trap '_err_report $LINENO' ERR

# kubectl wait with progress output and pod status on timeout.
_wait_for() {
	local target="$1" condition="$2" ns="${3:-default}" timeout_sec="${4:-60}"
	local start=$SECONDS
	while true; do
		if kubectl -n "$ns" wait --for=condition="$condition" "$target" --timeout=10s 2>/dev/null; then
			local took=$((SECONDS - start))
			echo "  $target $condition → OK (${took}s)"
			return 0
		fi
		local elapsed=$((SECONDS - start))
		if [[ $elapsed -ge $timeout_sec ]]; then
			_error "Timed out waiting for $target $condition in $ns after ${elapsed}s"
			echo "  Pod status in $ns:"
			kubectl -n "$ns" get pods -o wide 2>&1 || true
			return 1
		fi
		echo "  Waiting for $target... (${elapsed}s/${timeout_sec}s)"
	done
}

# ── prerequisite checks ────────────────────────────────────────────────────

_section "Prerequisites"
for cmd in kind kubectl helm; do
	command -v "$cmd" >/dev/null || { _error "$cmd is required but not found in PATH"; exit 1; }
done
echo "  kind:    $(kind version -q 2>/dev/null || echo 'ok')"
echo "  kubectl: $(kubectl version --client --output=json 2>/dev/null | grep -o '"gitVersion":"[^"]*"' | cut -d'"' -f4 || echo 'ok')"
echo "  helm:    $(helm version --short 2>/dev/null || echo 'ok')"
echo "  cluster: $CLUSTER"
_endsection

# ── cleanup ─────────────────────────────────────────────────────────────────

cleanup() {
	local exit_code=$?
	_section "Cleanup"
	kubectl delete namespace "$NS" --ignore-not-found --timeout=60s >/dev/null 2>&1 || {
		_warn "Namespace $NS did not clean up cleanly, forcing..."
		kubectl delete namespace "$NS" --ignore-not-found --timeout=5s --force --grace-period=0 >/dev/null 2>&1 || true
	}
	if [[ $exit_code -ne 0 ]]; then
		_error "Script exited with code $exit_code"
	fi
	_endsection
	exit "$exit_code"
}
trap cleanup EXIT

# ── [1/6] Prepare fixtures ─────────────────────────────────────────────────

_section "[1/6] Preparing test fixtures"
NODE=$(kind get nodes --name "$CLUSTER" 2>/dev/null | head -1)
if [[ -z "$NODE" ]]; then
	_error "Kind cluster '$CLUSTER' has no nodes — is the cluster running?"
	echo "  Available clusters: $(kind get clusters 2>/dev/null | tr '\n' ' ')"
	exit 1
fi
kubectl debug "node/$NODE" --image=busybox --quiet -- chroot /host sh -c '
  mkdir -p /etc/chur/secrets /var/lib/chur-keeper/secrets/prod
  echo -n "hello-local" > /etc/chur/secrets/local-ref
  echo -n "hello-keeper" > /var/lib/chur-keeper/secrets/prod/password
' 2>&1
echo "  Fixtures prepared on node $NODE"
_endsection

# ── [2/6] Install chur ─────────────────────────────────────────────────────

_section "[2/6] Installing chur $VERSION"

kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -
echo "  Namespace $NS created"

# Idempotent: apply the test secret (survives re-runs).
kubectl -n "$NS" apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: test-secret
stringData:
  token: hello
EOF
echo "  Test secret 'test-secret/token=hello' created"

HELM_COMMON=(
	--set tls.provider=helmGenerated
	--set webhook.allowKeeperSkipVerify=true
	--set-json 'rbac.namespaces=["'"$NS"'"]'
)

helm install chur "$CHART" --version "$VERSION" \
	"${HELM_COMMON[@]}" \
	--wait --timeout 180s || {
	rc=$?
	_error "Helm install failed (exit $rc)"
	echo ""
	echo "━━━ Helm status ━━━"
	helm status chur 2>&1 || true
	echo ""
	echo "━━━ Webhook pod describe ━━━"
	kubectl describe pod -l app.kubernetes.io/name=chur-webhook -n default 2>&1 | tail -50 || true
	echo ""
	echo "━━━ Webhook logs ━━━"
	kubectl logs deployment/chur-webhook -n default --all-containers --tail=50 2>&1 || true
	exit $rc
}
echo "  Helm install OK"

_wait_for "deployment/chur-webhook" "Available" "default" 60
_endsection

# ── [3/6] Test k8s + env + local providers ─────────────────────────────────

_section "[3/6] Testing providers: k8s, env, local"

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
		if [[ $i -lt $retries ]]; then
			echo "  kubectl apply attempt $i/$retries failed, retrying in 3s..."
			sleep 3
		fi
	done

	_wait_for "pod/test-$name" "Ready" "$NS" 60 || {
		_error "Pod test-$name did not become Ready"
		echo ""
		echo "━━━ Pod describe (test-$name) ━━━"
		kubectl describe pod "test-$name" -n "$NS" 2>&1 || true
		echo ""
		echo "━━━ Init container logs (chur-init) ━━━"
		kubectl logs "test-$name" -n "$NS" -c chur-init --tail=50 2>&1 || true
		echo ""
		echo "━━━ chur-webhook logs ━━━"
		kubectl logs deployment/chur-webhook -n default --all-containers --tail=30 2>&1 || true
		exit 1
	}

	result=$(kubectl exec "test-$name" -n "$NS" -- cat "$path")
	if [[ "$result" != "$expected" ]]; then
		_error "FAIL $name: expected '$expected', got '$result'"
		echo ""
		echo "━━━ Init container logs (chur-init) ━━━"
		kubectl logs "test-$name" -n "$NS" -c chur-init --tail=50 2>&1 || true
		exit 1
	fi
	echo "  PASS $name ($expected)"
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

_endsection

# ── [4/6] Enable keeper ─────────────────────────────────────────────────────

_section "[4/6] Enabling keeper"

HELM_KEEPER_EXTRA=(
	--set keeper.enabled=true
	--set keeper.backend=filesystem
	--set-json 'keeper.extraVolumes=[{"name":"tmp","emptyDir":{}}]'
	--set-json 'keeper.extraVolumeMounts=[{"name":"tmp","mountPath":"/tmp"}]'
)

helm upgrade chur "$CHART" --version "$VERSION" \
	"${HELM_COMMON[@]}" \
	"${HELM_KEEPER_EXTRA[@]}" \
	--wait --timeout 180s || {
	rc=$?
	_error "Keeper upgrade failed (exit $rc)"
	echo ""
	echo "━━━ Helm status ━━━"
	helm status chur 2>&1 || true
	echo ""
	echo "━━━ Keeper pod describe ━━━"
	kubectl describe pod -l app.kubernetes.io/name=chur-keeper -n default 2>&1 | tail -50 || true
	echo ""
	echo "━━━ Keeper logs ━━━"
	kubectl logs deployment/chur-keeper -n default --all-containers --tail=50 2>&1 || true
	echo ""
	echo "━━━ Webhook logs ━━━"
	kubectl logs deployment/chur-webhook -n default --all-containers --tail=30 2>&1 || true
	exit $rc
}
echo "  Helm upgrade OK"

_wait_for "deployment/chur-keeper" "Available" "default" 60

kubectl rollout restart deployment/chur-webhook -n default 2>&1
_wait_for "deployment/chur-webhook" "Available" "default" 60

_endsection

# ── [5/6] Test keeper provider ──────────────────────────────────────────────

_section "[5/6] Testing provider: keeper"

test_pod keeper /secrets/prod/password    hello-keeper \
    "    chur.io/provider: keeper
    chur.io/secret-ref: prod/password
    chur.io/keeper-skip-verify: \"true\""

_endsection

# ── [6/6] Done ──────────────────────────────────────────────────────────────

echo ""
echo "╔══════════════════════════════════════════╗"
echo "║   All 4 providers PASSED                 ║"
echo "║   Chart version: $VERSION                ║"
echo "╚══════════════════════════════════════════╝"
echo ""
