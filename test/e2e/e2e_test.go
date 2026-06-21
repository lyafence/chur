//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lyafence/chur/internal/webhook"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	ns := fmt.Sprintf("chur-e2e-tests-%d", time.Now().Unix())
	t.Logf("namespace: %s", ns)
	hookName := "chur-webhook"
	secretName := "my-e2e-secret"
	secretKey := "token"
	testValue := "e2e-test-secret-value-12345"

	// --- client-go setup ---------------------------------------------------

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	cfg, err := kubeconfig.ClientConfig()
	if err != nil {
		t.Fatalf("kubeconfig: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("clientset: %v", err)
	}

	// --- TLS cert generation -----------------------------------------------

	certPEM, keyPEM, err := webhook.GenerateCertMemory(fmt.Sprintf("%s.%s.svc", hookName, ns))
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	// --- namespace ---------------------------------------------------------

	cleanupNamespace := createNamespace(t, clientset, ns)
	defer cleanupNamespace()

	// --- TLS Secret --------------------------------------------------------

	createTLSSecret(t, clientset, ns, "chur-webhook-tls", certPEM, keyPEM)

	// --- RBAC: grant the test pod's SA permission to read Secrets ----------

	createSecretRBAC(t, clientset, ns)

	// --- Webhook Deployment ------------------------------------------------

	createWebhookDeployment(t, clientset, ns, hookName)

	// --- Webhook Service ---------------------------------------------------

	createWebhookService(t, clientset, ns, hookName)

	// --- Wait for webhook to be ready BEFORE creating MWC ------------------
	// The MWC intercepts ALL pod creation including the webhook's own pod.
	// Creating it before the webhook is ready creates a chicken-and-egg loop.

	waitForDeploymentReady(t, clientset, ns, hookName, 2*time.Minute)

	// --- MutatingWebhookConfiguration --------------------------------------

	cleanupMWC := createMutatingWebhook(t, clientset, hookName, ns, certPEM)
	defer cleanupMWC()

	// --- Test Secret (k8s provider reads this) -----------------------------

	createK8sSecret(t, clientset, ns, secretName, secretKey, testValue)

	// --- Test Pod ----------------------------------------------------------

	podName := "test-pod"
	createTestPod(t, clientset, ns, podName, secretName, secretKey)
	defer deletePod(t, clientset, ns, podName)

	// --- Wait for pod to be ready ------------------------------------------

	waitForPodReady(t, clientset, ns, podName, 2*time.Minute)

	// --- Verify secret in tmpfs --------------------------------------------

	stdout, stderr, err := execInPod("", ns, podName, "app", "cat", "/secrets/"+secretName)
	if err != nil {
		t.Fatalf("exec failed: %v\nstderr: %s", err, stderr)
	}
	result := strings.TrimSpace(stdout)
	if result != testValue {
		t.Fatalf("secret mismatch: expected %q, got %q", testValue, result)
	}
	t.Logf("secret verified: %s = %s", secretName, result)
}
