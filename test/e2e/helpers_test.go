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

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

func createNamespace(t *testing.T, cs kubernetes.Interface, name string) func() {
	t.Helper()
	ctx := context.Background()

	if _, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app": "chur-e2e"}},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	return func() {
		if err := cs.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			t.Logf("cleanup: delete namespace: %v", err)
		}
	}
}

func createTLSSecret(t *testing.T, cs kubernetes.Interface, ns, name string, certPEM, keyPEM []byte) {
	t.Helper()
	_, err := cs.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create tls secret: %v", err)
	}
}

func createSecretRBAC(t *testing.T, cs kubernetes.Interface, ns string) {
	t.Helper()
	_, err := cs.RbacV1().Roles(ns).Create(context.Background(), &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "chur-secret-reader"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     []string{"get"},
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	_, err = cs.RbacV1().RoleBindings(ns).Create(context.Background(), &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "chur-secret-reader"},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "chur-secret-reader",
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      "default",
			Namespace: ns,
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create rolebinding: %v", err)
	}
}

func createWebhookDeployment(t *testing.T, cs kubernetes.Interface, ns, name string) {
	t.Helper()
	replicas := int32(1)
	_, err := cs.AppsV1().Deployments(ns).Create(context.Background(), &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app": name}},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  name,
						Image: "chur-webhook:dev",
						Ports: []corev1.ContainerPort{{ContainerPort: 8443}},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "tls",
							MountPath: "/etc/chur/tls",
							ReadOnly:  true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "tls",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "chur-webhook-tls",
							},
						},
					}},
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}
}

func createWebhookService(t *testing.T, cs kubernetes.Interface, ns, name string) {
	t.Helper()
	_, err := cs.CoreV1().Services(ns).Create(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app": name}},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports: []corev1.ServicePort{{
				Port:       443,
				TargetPort: intstr.FromInt32(8443),
			}},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
}

func createMutatingWebhook(t *testing.T, cs kubernetes.Interface, name, ns string, caCertPEM []byte) func() {
	t.Helper()
	sideEffects := admissionv1.SideEffectClassNone
	fail := admissionv1.Fail
	path := "/"
	port := int32(443)
	scope := admissionv1.AllScopes

	_, err := cs.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(context.Background(), &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Webhooks: []admissionv1.MutatingWebhook{{
			Name: fmt.Sprintf("%s.%s.svc", name, ns),
			ClientConfig: admissionv1.WebhookClientConfig{
				CABundle: caCertPEM,
				Service: &admissionv1.ServiceReference{
					Name:      name,
					Namespace: ns,
					Path:      &path,
					Port:      &port,
				},
			},
			Rules: []admissionv1.RuleWithOperations{{
				Operations: []admissionv1.OperationType{admissionv1.Create},
				Rule: admissionv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
					Scope:       &scope,
				},
			}},
			FailurePolicy:           &fail,
			SideEffects:             &sideEffects,
			AdmissionReviewVersions: []string{"v1"},
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create mutating webhook: %v", err)
	}

	return func() {
		if err := cs.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil {
			t.Logf("cleanup: delete mutating webhook: %v", err)
		}
	}
}

func createK8sSecret(t *testing.T, cs kubernetes.Interface, ns, name, key, value string) {
	t.Helper()
	_, err := cs.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Data:       map[string][]byte{key: []byte(value)},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create test secret: %v", err)
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
			Containers: []corev1.Container{{
				Name:    "app",
				Image:   "busybox",
				Command: []string{"sleep", "9999"},
			}},
		},
	}
	if _, err := cs.CoreV1().Pods(ns).Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create test pod: %v", err)
	}
}

func deletePod(t *testing.T, cs kubernetes.Interface, ns, name string) {
	t.Helper()
	if err := cs.CoreV1().Pods(ns).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil {
		t.Logf("cleanup: delete pod: %v", err)
		}
}

func waitForDeploymentReady(t *testing.T, cs kubernetes.Interface, ns, name string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := wait.PollUntilContextTimeout(ctx, 1*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		dep, err := cs.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return dep.Status.ReadyReplicas >= 1, nil
	})
	if err != nil {
		t.Fatalf("deployment %s not ready within %v: %v", name, timeout, err)
	}
	t.Logf("deployment %s ready", name)
}

func waitForPodReady(t *testing.T, cs kubernetes.Interface, ns, name string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := wait.PollUntilContextTimeout(ctx, 1*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
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
			if init.State.Waiting != nil && init.State.Waiting.Reason == "CrashLoopBackOff" {
				return false, fmt.Errorf("init container %s: CrashLoopBackOff: %s", init.Name, init.State.Waiting.Message)
			}
		}
		return false, nil
	})
	if err != nil {
		logs, logErr := cs.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{}).DoRaw(ctx)
		if logErr == nil {
			t.Logf("pod %s logs:\n%s", name, string(logs))
		}
		initLogs, logErr := cs.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{
			Container: "chur-init",
		}).DoRaw(ctx)
		if logErr == nil {
			t.Logf("init container logs:\n%s", string(initLogs))
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
