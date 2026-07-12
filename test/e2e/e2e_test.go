//go:build e2e

package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestE2E_KeeperProvider(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	ns := os.Getenv("CHUR_E2E_NAMESPACE")
	if ns == "" {
		t.Skip("CHUR_E2E_NAMESPACE not set — use 'make e2e' to run integration tests")
	}

	secretRef := "prod/db/password"
	testValue := "e2e-keeper-secret-value"

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

	podName := "test-pod-keeper"
	createKeeperTestPod(t, clientset, ns, podName, secretRef)
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
	t.Logf("keeper secret verified: %s = %s", secretRef, result)
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

func TestE2E_KeeperUnavailable(t *testing.T) {
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

	podName := "test-pod-keeper-unavailable"
	createTestPod(t, clientset, ns, podName, map[string]string{
		"chur.io/provider":           "keeper",
		"chur.io/secret-ref":         "test/secret",
		"chur.io/keeper-skip-verify": "true",
	})
	defer deletePod(t, clientset, ns, podName)

	if err := waitForInitContainerError(t, clientset, ns, podName, 90*time.Second, "failed to get secret"); err != nil {
		t.Fatalf("expected init container to fail without keeper: %v", err)
	}
}

func TestE2E_InvalidKeeperRef(t *testing.T) {
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

	podName := "test-pod-invalid-ref"
	annotations := map[string]string{
		"chur.io/provider":   "keeper",
		"chur.io/secret-ref": "../../../etc/passwd",
	}

	err = createTestPodExpectError(clientset, ns, podName, annotations)
	if err == nil {
		t.Fatalf("expected admission denial for invalid keeper ref")
	}
	if !strings.Contains(err.Error(), "validation error") {
		t.Fatalf("expected 'validation error', got: %v", err)
	}
	t.Logf("admission denied as expected: %v", err)
}

func TestE2E_MultipleContainers(t *testing.T) {
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

	secretName := "multi-test"
	createK8sSecret(t, clientset, ns, secretName, "key", "multi-container-value")
	defer deleteK8sSecret(t, clientset, ns, secretName)

	podName := "test-pod-multi"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
			Labels:    map[string]string{"app": "chur-e2e"},
			Annotations: map[string]string{
				"chur.io/provider":   "k8s",
				"chur.io/secret-ref": secretName,
				"chur.io/secret-key": "key",
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "chur-init",
			Containers: []corev1.Container{
				{Name: "app1", Image: "busybox", Command: []string{"sleep", "9999"}},
				{Name: "app2", Image: "busybox", Command: []string{"sleep", "9999"}},
			},
		},
	}
	_, err = clientset.CoreV1().Pods(ns).Create(t.Context(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer deletePod(t, clientset, ns, podName)

	waitForPodReady(t, clientset, ns, podName, 2*time.Minute)
	t.Logf("pod %s ready, verifying all containers can read the secret", podName)

	for _, c := range []string{"app1", "app2"} {
		stdout, stderr, err := execInPod("", ns, podName, c, "cat", "/secrets/"+secretName)
		if err != nil {
			t.Fatalf("container %s: exec failed: %v\nstderr: %s", c, err, stderr)
		}
		if strings.TrimSpace(stdout) != "multi-container-value" {
			t.Errorf("container %s: got %q, want %q", c, stdout, "multi-container-value")
		}
	}
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
