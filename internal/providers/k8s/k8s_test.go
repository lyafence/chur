package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestK8sProvider_GetSecret_SingleKey(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		Data:       map[string][]byte{"token": []byte("abc123")},
	})

	p := &K8sProvider{client: client, namespace: "default"}
	secret, err := p.GetSecret(context.Background(), "my-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(secret) != "abc123" {
		t.Fatalf("expected %q, got %q", "abc123", string(secret))
	}
}

func TestK8sProvider_GetSecret_ExplicitKey(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		Data: map[string][]byte{
			"token": []byte("abc123"),
			"cert":  []byte("pem"),
		},
	})

	p := &K8sProvider{client: client, namespace: "default", secretKey: "cert"}
	secret, err := p.GetSecret(context.Background(), "my-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(secret) != "pem" {
		t.Fatalf("expected %q, got %q", "pem", string(secret))
	}
}

func TestK8sProvider_GetSecret_MultipleKeysNoSelection(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		Data: map[string][]byte{
			"a": []byte("1"),
			"b": []byte("2"),
		},
	})

	p := &K8sProvider{client: client, namespace: "default"}
	_, err := p.GetSecret(context.Background(), "my-secret")
	if err == nil {
		t.Fatal("expected error when multiple keys exist and none selected")
	}
}

func TestK8sProvider_GetSecret_MissingKey(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		Data:       map[string][]byte{"token": []byte("abc123")},
	})

	p := &K8sProvider{client: client, namespace: "default", secretKey: "missing"}
	_, err := p.GetSecret(context.Background(), "my-secret")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestK8sProvider_GetSecret_EmptySecret(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		Data:       map[string][]byte{},
	})

	p := &K8sProvider{client: client, namespace: "default"}
	_, err := p.GetSecret(context.Background(), "my-secret")
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}
