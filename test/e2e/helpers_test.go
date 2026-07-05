//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

func createK8sSecret(t *testing.T, cs kubernetes.Interface, ns, name, key, value string) {
	t.Helper()
	_, err := cs.CoreV1().Secrets(ns).Create(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Data:       map[string][]byte{key: []byte(value)},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create test secret: %v", err)
	}
}

func deleteK8sSecret(t *testing.T, cs kubernetes.Interface, ns, name string) {
	t.Helper()
	if err := cs.CoreV1().Secrets(ns).Delete(t.Context(), name, metav1.DeleteOptions{}); err != nil {
		t.Logf("cleanup: delete secret: %v", err)
	}
}

func createTestPod(t *testing.T, cs kubernetes.Interface, ns, name, secretRef, secretKey string) {
	t.Helper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"app": "chur-e2e"},
			Annotations: map[string]string{
				"chur.io/provider":   "k8s",
				"chur.io/secret-ref": secretRef,
				"chur.io/secret-key": secretKey,
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "chur-init",
			Containers: []corev1.Container{{
				Name:    "app",
				Image:   "busybox",
				Command: []string{"sleep", "9999"},
			}},
		},
	}
	if _, err := cs.CoreV1().Pods(ns).Create(t.Context(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create test pod: %v", err)
	}
}

func deletePod(t *testing.T, cs kubernetes.Interface, ns, name string) {
	t.Helper()
	if err := cs.CoreV1().Pods(ns).Delete(t.Context(), name, metav1.DeleteOptions{}); err != nil {
		t.Logf("cleanup: delete pod: %v", err)
	}
}

func waitForPodReady(t *testing.T, cs kubernetes.Interface, ns, name string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()

	var lastPod *corev1.Pod
	err := wait.PollUntilContextTimeout(ctx, 1*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		lastPod = pod
		if pod.Status.Phase == corev1.PodPending {
			return false, nil
		}
		if pod.Status.Phase == corev1.PodRunning {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					return true, nil
				}
			}
		}
		for _, init := range pod.Status.InitContainerStatuses {
			if init.State.Waiting != nil {
				reason := init.State.Waiting.Reason
				msg := init.State.Waiting.Message
				if reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" || reason == "ErrImagePull" {
					return false, fmt.Errorf("init container %s: %s: %s", init.Name, reason, msg)
				}
			}
		}
		return false, nil
	})
	if err != nil {
		logCtx := t.Context()
		logs, logErr := cs.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{}).DoRaw(logCtx)
		if logErr == nil {
			t.Logf("pod %s logs:\n%s", name, string(logs))
		}
		initLogs, logErr := cs.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{
			Container: "chur-init",
		}).DoRaw(logCtx)
		if logErr == nil {
			t.Logf("init container logs:\n%s", string(initLogs))
		}
		if lastPod != nil {
			t.Logf("pod status: phase=%s containers=%d init_containers=%d",
				lastPod.Status.Phase, len(lastPod.Status.ContainerStatuses), len(lastPod.Status.InitContainerStatuses))
			for _, init := range lastPod.Status.InitContainerStatuses {
				if init.State.Waiting != nil {
					t.Logf("init %s: waiting reason=%s message=%s", init.Name, init.State.Waiting.Reason, init.State.Waiting.Message)
				}
				if init.State.Terminated != nil {
					t.Logf("init %s: terminated reason=%s exit=%d message=%s", init.Name, init.State.Terminated.Reason, init.State.Terminated.ExitCode, init.State.Terminated.Message)
				}
			}
		}
		t.Fatalf("pod %s not ready within %v: %v", name, timeout, err)
	}
	t.Logf("pod %s ready", name)
}

func execInPod(kubeconfig, ns, pod, container string, cmd ...string) (string, string, error) {
	args := []string{"exec", "-n", ns, pod, "-c", container, "--"}
	args = append(args, cmd...)
	c := exec.Command("kubectl", args...)
	if kubeconfig != "" {
		c.Env = append(os.Environ(), "KUBECONFIG="+kubeconfig)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	return stdout.String(), stderr.String(), err
}
