//go:build e2e

package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	ns := os.Getenv("CHUR_E2E_NAMESPACE")
	if ns == "" {
		t.Skip("CHUR_E2E_NAMESPACE not set — use 'make e2e' to run integration tests")
	}

	secretName := "my-e2e-secret"
	secretKey := "token"
	testValue := "e2e-test-secret-value-12345"

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

	createK8sSecret(t, clientset, ns, secretName, secretKey, testValue)
	defer deleteK8sSecret(t, clientset, ns, secretName)

	podName := "test-pod"
	createTestPod(t, clientset, ns, podName, secretName, secretKey)
	defer deletePod(t, clientset, ns, podName)

	waitForPodReady(t, clientset, ns, podName, 2*time.Minute)

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
