package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/lyafence/chur/internal/validate"
)

// validProviders lists all provider names that the webhook can pass to chur-init.
// This is the webhook's own list — it does not depend on the provider registry
// (which may have build-tag-gated entries like k8s).
// Admission-time provider validation does NOT guarantee runtime availability —
// chur-init may be built with a different build tag set.
var validProviders = map[string]bool{
	providerEnv:    true,
	providerLocal:  true,
	providerK8s:    true,
	providerKeeper: true,
}

// ErrValidation indicates that the pod annotations failed validation.
// The webhook should respond with HTTP 400 BadRequest for these errors.
var ErrValidation = errors.New("validation error")

const (
	providerEnv    = "env"
	providerLocal  = "local"
	providerK8s    = "k8s"
	providerKeeper = "keeper"

	annotationProvider         = "chur.io/provider"
	annotationSecret           = "chur.io/secret-ref" //nolint:gosec // annotation key, not a credential
	annotationSecretKey        = "chur.io/secret-key" //nolint:gosec // annotation key, not a credential
	annotationMount            = "chur.io/mount-path"
	annotationKeeperSkipVerify = "chur.io/keeper-skip-verify"
	annotationProviderEnv      = "chur.io/provider-env"

	opAdd = "add"

	volNameLocal  = "chur-local-base"
	volNameKeeper = "chur-keeper-client-tls"
)

// AuditInfo contains structured metadata for audit logging.
type AuditInfo struct {
	RequestUID types.UID
	Namespace  string
	Pod        string
	Provider   string
	DurationMs int64
	Result     string
}

// PatchOperation represents a single JSON Patch operation.
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// Config holds the mutable configuration for the webhook mutator.
type Config struct {
	VolumeSizeLimit            resource.Quantity
	AllowedNamespaces          []string
	InitImage                  string
	InitImagePullPolicy        corev1.PullPolicy
	MaxSecretSize              string
	LocalBasePath              string
	MaxConcurrent              int
	RunAsUser                  int64
	RunAsGroup                 *int64
	FSGroup                    int64
	KeeperServiceName          string
	KeeperServiceNamespace     string
	KeeperServicePort          string
	KeeperTLSCertPath          string
	KeeperTLSKeyPath           string
	KeeperServerCA             string
	KeeperClientCertSecretName string
	KeeperClientMaxSecretSize  string
	AllowKeeperSkipVerify      bool
}

// DefaultConfig returns a Config with safe defaults.
func DefaultConfig() *Config {
	return &Config{
		VolumeSizeLimit:        resource.MustParse("10Mi"),
		InitImage:              "ghcr.io/lyafence/chur-init:latest",
		InitImagePullPolicy:    corev1.PullIfNotPresent,
		MaxSecretSize:          "1Mi",
		LocalBasePath:          "/etc/chur/secrets",
		MaxConcurrent:          100,
		RunAsUser:              1001,
		FSGroup:                1001,
		KeeperServiceNamespace: "chur-system",
		KeeperServicePort:      "9443",
		AllowKeeperSkipVerify:  false,
	}
}

// reservedInitEnv lists keys that the webhook manages itself and that must
// not be overridden via chur.io/provider-env.
var reservedInitEnv = map[string]bool{
	"CHUR_PROVIDER":                      true,
	"CHUR_SECRET_REF":                    true,
	"CHUR_SECRET_KEY":                    true,
	"CHUR_MOUNT_PATH":                    true,
	"CHUR_MAX_SECRET_SIZE":               true,
	"CHUR_LOCAL_BASE_PATH":               true,
	"CHUR_KEEPER_URL":                    true,
	"CHUR_KEEPER_TLS_CERT_PATH":          true,
	"CHUR_KEEPER_TLS_KEY_PATH":           true,
	"CHUR_KEEPER_SERVER_CA":              true,
	"CHUR_KEEPER_INSECURE_SKIP_VERIFY":   true,
	"CHUR_KEEPER_CLIENT_MAX_SECRET_SIZE": true,
}

func validProviderEnvKey(k string) bool {
	if len(k) == 0 || len(k) > 128 {
		return false
	}
	for _, r := range k {
		if r != '_' && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return strings.HasPrefix(k, "CHUR_")
}

// parseProviderEnv parses the chur.io/provider-env annotation. It returns
// sorted env vars so patch output is deterministic.
func parseProviderEnv(annotation string) ([]corev1.EnvVar, error) {
	if annotation == "" {
		return nil, nil
	}
	var extra map[string]string
	if err := json.Unmarshal([]byte(annotation), &extra); err != nil {
		return nil, fmt.Errorf("%w: invalid %s: %w", ErrValidation, annotationProviderEnv, err)
	}

	var envs []corev1.EnvVar
	for k, v := range extra {
		if !validProviderEnvKey(k) {
			return nil, fmt.Errorf("%w: invalid key %q in %s", ErrValidation, k, annotationProviderEnv)
		}
		if reservedInitEnv[k] {
			return nil, fmt.Errorf("%w: reserved key %q in %s", ErrValidation, k, annotationProviderEnv)
		}
		if len(v) > 4096 {
			return nil, fmt.Errorf("%w: value for %q exceeds 4096 bytes in %s", ErrValidation, k, annotationProviderEnv)
		}
		envs = append(envs, corev1.EnvVar{Name: k, Value: v})
	}
	sort.Slice(envs, func(i, j int) bool { return envs[i].Name < envs[j].Name })
	return envs, nil
}

// MutatePod adds a tmpfs volume and init container to the pod spec when the
// chur annotations are present. It returns nil, nil, nil when no mutation is
// required. All user-controlled values are strictly validated before use.
// The returned AuditInfo is non-nil when chur annotations are present.
func MutatePod(pod *corev1.Pod, cfg *Config) ([]PatchOperation, *AuditInfo, error) {
	if pod == nil || pod.Annotations == nil {
		return nil, nil, nil
	}
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if len(cfg.AllowedNamespaces) > 0 {
		allowed := false
		for _, ns := range cfg.AllowedNamespaces {
			if pod.Namespace == ns {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, nil, nil
		}
	}

	providerName, ok := pod.Annotations[annotationProvider]
	if !ok {
		return nil, nil, nil
	}

	ai := &AuditInfo{
		Namespace: pod.Namespace,
		Pod:       pod.Name,
		Provider:  providerName,
	}

	if providerName == "" {
		return nil, ai, fmt.Errorf("%w: %s annotation must not be empty", ErrValidation, annotationProvider)
	}
	if !validProviders[providerName] {
		return nil, ai, fmt.Errorf("%w: unknown provider %q", ErrValidation, providerName)
	}

	secretRef := pod.Annotations[annotationSecret]
	validator := validate.ValidateSecretRef
	if providerName == providerKeeper {
		validator = validate.ValidateKeeperRef
	}
	if err := validator(secretRef); err != nil {
		return nil, ai, fmt.Errorf("%w: invalid %s: %w", ErrValidation, annotationSecret, err)
	}

	secretKey := pod.Annotations[annotationSecretKey]
	if err := validate.ValidateSecretKey(secretKey); err != nil {
		return nil, ai, fmt.Errorf("%w: invalid %s: %w", ErrValidation, annotationSecretKey, err)
	}

	mountPath := pod.Annotations[annotationMount]
	if mountPath == "" {
		mountPath = "/secrets"
	}
	if err := validate.ValidateMountPath(mountPath); err != nil {
		return nil, ai, fmt.Errorf("%w: invalid %s: %w", ErrValidation, annotationMount, err)
	}

	fsGroup := cfg.FSGroup
	if pod.Spec.SecurityContext != nil && pod.Spec.SecurityContext.FSGroup != nil {
		fsGroup = *pod.Spec.SecurityContext.FSGroup
	}

	volName := "chur-secrets"
	patches := []PatchOperation{}

	patches = append(patches, patchSecurityContext(pod, fsGroup)...)
	patches = append(patches, patchVolumes(pod, cfg, providerName, volName)...)

	initEnv, err := buildInitEnv(cfg, providerName, secretRef, secretKey, mountPath, pod)
	if err != nil {
		return nil, ai, err
	}

	initContainer := buildInitContainer(cfg, initEnv, volName, mountPath, providerName, fsGroup)
	if !initContainerExists(pod.Spec.InitContainers, initContainer.Name) {
		if len(pod.Spec.InitContainers) == 0 {
			patches = append(patches, PatchOperation{
				Op:    opAdd,
				Path:  "/spec/initContainers",
				Value: []corev1.Container{initContainer},
			})
		} else {
			patches = append(patches, PatchOperation{
				Op:    opAdd,
				Path:  "/spec/initContainers/-",
				Value: initContainer,
			})
		}
	}

	patches = append(patches, mountToAppContainers(pod, volName, mountPath)...)

	return patches, ai, nil
}

func patchSecurityContext(pod *corev1.Pod, fsGroup int64) []PatchOperation {
	if pod.Spec.SecurityContext == nil {
		return []PatchOperation{{
			Op:   opAdd,
			Path: "/spec/securityContext",
			Value: &corev1.PodSecurityContext{
				FSGroup: ptr.To(fsGroup),
			},
		}}
	}
	if pod.Spec.SecurityContext.FSGroup == nil {
		return []PatchOperation{{
			Op:    opAdd,
			Path:  "/spec/securityContext/fsGroup",
			Value: fsGroup,
		}}
	}
	return nil
}

func patchVolumes(pod *corev1.Pod, cfg *Config, providerName, volName string) []PatchOperation {
	var patches []PatchOperation

	if !volumeExists(pod.Spec.Volumes, volName) {
		v := corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    corev1.StorageMediumMemory,
					SizeLimit: &cfg.VolumeSizeLimit,
				},
			},
		}
		if len(pod.Spec.Volumes) == 0 {
			patches = append(patches, PatchOperation{
				Op: opAdd, Path: "/spec/volumes", Value: []corev1.Volume{v},
			})
		} else {
			patches = append(patches, PatchOperation{
				Op: opAdd, Path: "/spec/volumes/-", Value: v,
			})
		}
	}

	if providerName == providerLocal && !volumeExists(pod.Spec.Volumes, volNameLocal) {
		patches = append(patches, PatchOperation{
			Op:   opAdd,
			Path: "/spec/volumes/-",
			Value: corev1.Volume{
				Name: volNameLocal,
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: cfg.LocalBasePath,
						Type: ptr.To(corev1.HostPathDirectoryOrCreate),
					},
				},
			},
		})
	}

	if providerName == providerKeeper && cfg.KeeperClientCertSecretName != "" {
		if !volumeExists(pod.Spec.Volumes, volNameKeeper) {
			v := corev1.Volume{
				Name: volNameKeeper,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  cfg.KeeperClientCertSecretName,
						DefaultMode: ptr.To[int32](0444),
					},
				},
			}
			if len(pod.Spec.Volumes) == 0 {
				patches = append(patches, PatchOperation{
					Op: opAdd, Path: "/spec/volumes", Value: []corev1.Volume{v},
				})
			} else {
				patches = append(patches, PatchOperation{
					Op: opAdd, Path: "/spec/volumes/-", Value: v,
				})
			}
		}
	}

	return patches
}

func buildInitEnv(cfg *Config, providerName, secretRef, secretKey, mountPath string, pod *corev1.Pod) ([]corev1.EnvVar, error) {
	env := []corev1.EnvVar{
		{Name: "CHUR_PROVIDER", Value: providerName},
		{Name: "CHUR_SECRET_REF", Value: secretRef},
		{Name: "CHUR_MOUNT_PATH", Value: mountPath},
		{Name: "CHUR_MAX_SECRET_SIZE", Value: cfg.MaxSecretSize},
		{Name: "CHUR_LOCAL_BASE_PATH", Value: cfg.LocalBasePath},
	}
	if secretKey != "" {
		env = append(env, corev1.EnvVar{Name: "CHUR_SECRET_KEY", Value: secretKey})
	}

	if providerName == providerKeeper {
		if cfg.KeeperServiceName != "" {
			host := cfg.KeeperServiceName + "." + cfg.KeeperServiceNamespace + ".svc"
			u := url.URL{
				Scheme: "https",
				Host:   net.JoinHostPort(host, cfg.KeeperServicePort),
			}
			env = append(env, corev1.EnvVar{Name: "CHUR_KEEPER_URL", Value: u.String()})
		}
		if cfg.KeeperTLSCertPath != "" {
			env = append(env, corev1.EnvVar{Name: "CHUR_KEEPER_TLS_CERT_PATH", Value: cfg.KeeperTLSCertPath})
		}
		if cfg.KeeperTLSKeyPath != "" {
			env = append(env, corev1.EnvVar{Name: "CHUR_KEEPER_TLS_KEY_PATH", Value: cfg.KeeperTLSKeyPath})
		}
		if cfg.KeeperServerCA != "" {
			env = append(env, corev1.EnvVar{Name: "CHUR_KEEPER_SERVER_CA", Value: cfg.KeeperServerCA})
		}
		if cfg.KeeperClientMaxSecretSize != "" {
			env = append(env, corev1.EnvVar{Name: "CHUR_KEEPER_CLIENT_MAX_SECRET_SIZE", Value: cfg.KeeperClientMaxSecretSize})
		}
		if pod.Annotations[annotationKeeperSkipVerify] == "1" || pod.Annotations[annotationKeeperSkipVerify] == "true" {
			if !cfg.AllowKeeperSkipVerify {
				return nil, fmt.Errorf("%w: %s is set but webhook is not configured to allow it", ErrValidation, annotationKeeperSkipVerify)
			}
			env = append(env, corev1.EnvVar{Name: "CHUR_KEEPER_INSECURE_SKIP_VERIFY", Value: "1"})
		}
	}

	extraEnv, err := parseProviderEnv(pod.Annotations[annotationProviderEnv])
	if err != nil {
		return nil, err
	}
	env = append(env, extraEnv...)

	return env, nil
}

func buildInitContainer(cfg *Config, initEnv []corev1.EnvVar, volName, mountPath, providerName string, fsGroup int64) corev1.Container {
	runAsGroup := cfg.RunAsGroup
	if runAsGroup == nil {
		runAsGroup = ptr.To(fsGroup)
	}
	c := corev1.Container{
		Name:            "chur-init",
		Image:           cfg.InitImage,
		ImagePullPolicy: cfg.InitImagePullPolicy,
		Command:         []string{"/chur-init"},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			RunAsUser:                ptr.To(cfg.RunAsUser),
			RunAsGroup:               runAsGroup,
			ReadOnlyRootFilesystem:   ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Env: initEnv,
		VolumeMounts: []corev1.VolumeMount{
			{Name: volName, MountPath: mountPath},
		},
	}

	if providerName == providerLocal {
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
			Name:      volNameLocal,
			MountPath: cfg.LocalBasePath,
			ReadOnly:  true,
		})
	}

	if providerName == providerKeeper && cfg.KeeperClientCertSecretName != "" {
		mountDir := cfg.KeeperTLSCertPath
		if mountDir == "" {
			mountDir = "/etc/chur-keeper/client-tls"
		}
		if idx := strings.LastIndex(mountDir, "/"); idx >= 0 {
			mountDir = mountDir[:idx]
		}
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
			Name:      volNameKeeper,
			MountPath: mountDir,
			ReadOnly:  true,
		})
	}

	return c
}

func mountToAppContainers(pod *corev1.Pod, volName, mountPath string) []PatchOperation {
	var patches []PatchOperation
	for i := range pod.Spec.Containers {
		if volumeMountExists(pod.Spec.Containers[i].VolumeMounts, volName, mountPath) {
			continue
		}
		if len(pod.Spec.Containers[i].VolumeMounts) == 0 {
			patches = append(patches, PatchOperation{
				Op:   opAdd,
				Path: fmt.Sprintf("/spec/containers/%d/volumeMounts", i),
				Value: []corev1.VolumeMount{{
					Name:      volName,
					MountPath: mountPath,
					ReadOnly:  true,
				}},
			})
		} else {
			patches = append(patches, PatchOperation{
				Op:   opAdd,
				Path: fmt.Sprintf("/spec/containers/%d/volumeMounts/-", i),
				Value: corev1.VolumeMount{
					Name:      volName,
					MountPath: mountPath,
					ReadOnly:  true,
				},
			})
		}
	}
	return patches
}

func volumeExists(volumes []corev1.Volume, name string) bool {
	for _, v := range volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

func initContainerExists(containers []corev1.Container, name string) bool {
	for _, c := range containers {
		if c.Name == name {
			return true
		}
	}
	return false
}

func volumeMountExists(mounts []corev1.VolumeMount, name, mountPath string) bool {
	for _, m := range mounts {
		if m.Name == name && m.MountPath == mountPath {
			return true
		}
	}
	return false
}
