//go:build provider_k8s

package k8s

import (
	"context"
	"fmt"
	"os"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/lyafence/chur/internal/provider"
	"github.com/lyafence/chur/internal/validate"
)

type K8sProvider struct {
	client    kubernetes.Interface
	namespace string
	secretKey string
}

func (p *K8sProvider) Name() string { return "k8s" }

func (p *K8sProvider) GetSecret(ctx context.Context, ref string) ([]byte, error) {
	sec, err := p.client.CoreV1().Secrets(p.namespace).Get(ctx, ref, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("k8s: get secret %s/%s: %w", p.namespace, ref, err)
	}

	if len(sec.Data) == 0 {
		return nil, fmt.Errorf("k8s: secret %s/%s has no data", p.namespace, ref)
	}

	if p.secretKey != "" {
		value, ok := sec.Data[p.secretKey]
		if !ok {
			return nil, fmt.Errorf("k8s: secret %s/%s does not contain key %q", p.namespace, ref, p.secretKey)
		}
		return value, nil
	}

	if len(sec.Data) == 1 {
		for _, v := range sec.Data {
			return v, nil
		}
	}

	// Multiple keys and no explicit key selected. Return a deterministic error.
	keys := make([]string, 0, len(sec.Data))
	for k := range sec.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return nil, fmt.Errorf("k8s: secret %s/%s contains multiple keys (%v); specify CHUR_SECRET_KEY", p.namespace, ref, keys)
}

func init() {
	provider.Register("k8s", func(ctx context.Context) (provider.SecretProvider, error) {
		config, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("k8s: in-cluster config: %w", err)
		}
		client, err := kubernetes.NewForConfig(config)
		if err != nil {
			return nil, fmt.Errorf("k8s: create client: %w", err)
		}
		ns := "default"
		if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
			ns = string(data)
		}

		secretKey := os.Getenv("CHUR_SECRET_KEY")
		if err := validate.ValidateSecretKey(secretKey); err != nil {
			return nil, fmt.Errorf("k8s: invalid CHUR_SECRET_KEY: %w", err)
		}

		return &K8sProvider{client: client, namespace: ns, secretKey: secretKey}, nil
	})
}
