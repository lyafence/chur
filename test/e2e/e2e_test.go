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

	podName := "test-pod-k8s"
	createK8sTestPod(t, clientset, ns, podName, secretName, secretKey)
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

func TestE2E_LocalProvider(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	ns := os.Getenv("CHUR_E2E_NAMESPACE")
	if ns == "" {
		t.Skip("CHUR_E2E_NAMESPACE not set — use 'make e2e' to run integration tests")
	}

	secretRef := "e2e-local-secret"
	testValue := "e2e-local-secret-value-12345"

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

	podName := "test-pod-local"
	createLocalTestPod(t, clientset, ns, podName, secretRef)
	defer deletePod(t, clientset, ns, podName)

	waitForPodReady(t, clientset, ns, podName, 2*time.Minute)

	stdout, stderr, err := execInPod("", ns, podName, "app", "cat", "/secrets/"+secretRef)
	if err != nil {
		t.Fatalf("exec failed: %v\nstderr: %s", err, stderr)
	}
	result := strings.TrimSpace(stdout)
	if result != testValue {
		t.Fatalf("secret mismatch: expected %q, got %q", testValue, result)
	}
	t.Logf("local secret verified: %s = %s", secretRef, result)
}

func TestE2E_UnknownProvider(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	ns := os.Getenv("CHUR_E2E_NAMESPACE")
	if ns == "" {
		t.Skip("CHUR_E2E_NAMESPACE not set — use 'make e2e' to run integration tests")
	}

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

	podName := "test-pod-unknown"
	annotations := map[string]string{
		"chur.io/provider":   "unknown-provider",
		"chur.io/secret-ref": "anything",
	}

	err = createTestPodExpectError(clientset, ns, podName, annotations)
	if err == nil {
		t.Fatalf("expected admission denial for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("expected 'unknown provider' in error, got: %v", err)
	}
	t.Logf("admission denied as expected: %v", err)
}

func TestE2E_SecretTooLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	ns := os.Getenv("CHUR_E2E_NAMESPACE")
	if ns == "" {
		t.Skip("CHUR_E2E_NAMESPACE not set — use 'make e2e' to run integration tests")
	}

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

	secretRef := "e2e-large-secret"
	podName := "test-pod-large"
	createLocalTestPod(t, clientset, ns, podName, secretRef)
	defer deletePod(t, clientset, ns, podName)

	if err := waitForInitContainerError(t, clientset, ns, podName, 60*time.Second, "secret exceeds max size"); err != nil {
		t.Fatalf("expected init container failure: %v", err)
	}
}
