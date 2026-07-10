package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/lyafence/chur/internal/provider"
	"github.com/lyafence/chur/internal/validate"
)

// ErrValidation indicates that the pod annotations failed validation.
// The webhook should respond with HTTP 400 BadRequest for these errors.
var ErrValidation = errors.New("validation error")

const (
	annotationProvider         = "chur.io/provider"
	annotationSecret           = "chur.io/secret-ref" //nolint:gosec // annotation key, not a credential
	annotationSecretKey        = "chur.io/secret-key" //nolint:gosec // annotation key, not a credential
	annotationMount            = "chur.io/mount-path"
	annotationKeeperSkipVerify = "chur.io/keeper-skip-verify"
	annotationProviderEnv      = "chur.io/provider-env"

	opAdd                    = "add"
	defaultChurFSGroup int64 = 1001
)

// PatchOperation represents a single JSON Patch operation.
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// Config holds the mutable configuration for the webhook mutator.
type Config struct {
	VolumeSizeLimit        resource.Quantity
	AllowedNamespaces      []string
	InitImage              string
	MaxSecretSize          string
	LocalBasePath          string
	MaxConcurrent          int
	KeeperServiceName      string
	KeeperServiceNamespace string
	KeeperServicePort      string
}

// DefaultConfig returns a Config with safe defaults.
func DefaultConfig() *Config {
	return &Config{
		VolumeSizeLimit:        resource.MustParse("10Mi"),
		InitImage:              "ghcr.io/lyafence/chur-init:latest",
		MaxSecretSize:          "1Mi",
		LocalBasePath:          "/etc/chur/secrets",
		MaxConcurrent:          100,
		KeeperServiceNamespace: "chur",
		KeeperServicePort:      "9443",
	}
}

// reservedInitEnv lists keys that the webhook manages itself and that must
// not be overridden via chur.io/provider-env.
var reservedInitEnv = map[string]bool{
	"CHUR_PROVIDER":        true,
	"CHUR_SECRET_REF":      true,
	"CHUR_SECRET_KEY":      true,
	"CHUR_MOUNT_PATH":      true,
	"CHUR_MAX_SECRET_SIZE": true,
	"CHUR_LOCAL_BASE_PATH": true,
	"CHUR_KEEPER_URL":      true,
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
		envs = append(envs, corev1.EnvVar{Name: k, Value: v})
	}
	sort.Slice(envs, func(i, j int) bool { return envs[i].Name < envs[j].Name })
	return envs, nil
}

// MutatePod adds a tmpfs volume and init container to the pod spec when the
// chur annotations are present. It returns nil, nil when no mutation is
// required. All user-controlled values are strictly validated before use.
func MutatePod(pod *corev1.Pod, cfg *Config) ([]PatchOperation, error) {
	if pod == nil || pod.Annotations == nil {
		return nil, nil
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
			return nil, nil
		}
	}

	providerName, ok := pod.Annotations[annotationProvider]
	if !ok {
		return nil, nil
	}
	if providerName == "" {
		return nil, fmt.Errorf("%w: %s annotation must not be empty", ErrValidation, annotationProvider)
	}
	if !provider.IsValidName(providerName) {
		return nil, fmt.Errorf("%w: unknown provider %q", ErrValidation, providerName)
	}

	secretRef := pod.Annotations[annotationSecret]
	validator := validate.ValidateSecretRef
	if providerName == "keeper" {
		validator = validate.ValidateKeeperRef
	}
	if err := validator(secretRef); err != nil {
		return nil, fmt.Errorf("%w: invalid %s: %w", ErrValidation, annotationSecret, err)
	}

	secretKey := pod.Annotations[annotationSecretKey]
	if err := validate.ValidateSecretKey(secretKey); err != nil {
		return nil, fmt.Errorf("%w: invalid %s: %w", ErrValidation, annotationSecretKey, err)
	}

	mountPath := pod.Annotations[annotationMount]
	if mountPath == "" {
		mountPath = "/secrets"
	}
	if err := validate.ValidateMountPath(mountPath); err != nil {
		return nil, fmt.Errorf("%w: invalid %s: %w", ErrValidation, annotationMount, err)
	}

	// Determine the group that will own the shared tmpfs volume.
	// If the pod already specifies fsGroup, respect it; otherwise inject one.
	fsGroup := defaultChurFSGroup
	if pod.Spec.SecurityContext != nil && pod.Spec.SecurityContext.FSGroup != nil {
		fsGroup = *pod.Spec.SecurityContext.FSGroup
	}

	volName := "chur-secrets"
	patches := []PatchOperation{}

	// Ensure the pod-level securityContext has an fsGroup so that all
	// containers share a supplementary group for the tmpfs volume.
	if pod.Spec.SecurityContext == nil {
		patches = append(patches, PatchOperation{
			Op:   opAdd,
			Path: "/spec/securityContext",
			Value: &corev1.PodSecurityContext{
				FSGroup: ptr.To(fsGroup),
			},
		})
	} else if pod.Spec.SecurityContext.FSGroup == nil {
		patches = append(patches, PatchOperation{
			Op:    opAdd,
			Path:  "/spec/securityContext/fsGroup",
			Value: fsGroup,
		})
	}

	// Add the tmpfs volume, creating the array if necessary.
	if len(pod.Spec.Volumes) == 0 {
		patches = append(patches, PatchOperation{
			Op:   opAdd,
			Path: "/spec/volumes",
			Value: []corev1.Volume{{
				Name: volName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						Medium:    corev1.StorageMediumMemory,
						SizeLimit: &cfg.VolumeSizeLimit,
					},
				},
			}},
		})
	} else {
		patches = append(patches, PatchOperation{
			Op:   opAdd,
			Path: "/spec/volumes/-",
			Value: corev1.Volume{
				Name: volName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						Medium:    corev1.StorageMediumMemory,
						SizeLimit: &cfg.VolumeSizeLimit,
					},
				},
			},
		})
	}

	initEnv := []corev1.EnvVar{
		{Name: "CHUR_PROVIDER", Value: providerName},
		{Name: "CHUR_SECRET_REF", Value: secretRef},
		{Name: "CHUR_MOUNT_PATH", Value: mountPath},
		{Name: "CHUR_MAX_SECRET_SIZE", Value: cfg.MaxSecretSize},
		{Name: "CHUR_LOCAL_BASE_PATH", Value: cfg.LocalBasePath},
	}
	if secretKey != "" {
		initEnv = append(initEnv, corev1.EnvVar{Name: "CHUR_SECRET_KEY", Value: secretKey})
	}

	if providerName == "keeper" {
		if cfg.KeeperServiceName != "" {
			url := fmt.Sprintf("https://%s.%s.svc:%s", cfg.KeeperServiceName, cfg.KeeperServiceNamespace, cfg.KeeperServicePort)
			initEnv = append(initEnv, corev1.EnvVar{Name: "CHUR_KEEPER_URL", Value: url})
		}
		if pod.Annotations[annotationKeeperSkipVerify] == "true" {
			initEnv = append(initEnv, corev1.EnvVar{Name: "CHUR_KEEPER_SKIP_VERIFY", Value: "1"})
		}
		extraEnv, err := parseProviderEnv(pod.Annotations[annotationProviderEnv])
		if err != nil {
			return nil, err
		}
		initEnv = append(initEnv, extraEnv...)
	}

	// The local provider reads files from the node filesystem. Mount the base
	// directory as a read-only hostPath volume into the init container only.
	localVolName := "chur-local-base"
	if providerName == "local" {
		patches = append(patches, PatchOperation{
			Op:   opAdd,
			Path: "/spec/volumes/-",
			Value: corev1.Volume{
				Name: localVolName,
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: cfg.LocalBasePath,
						Type: ptr.To(corev1.HostPathDirectoryOrCreate),
					},
				},
			},
		})
	}

	// Add the chur-init init container, creating the array if necessary.
	initContainer := corev1.Container{
		Name:            "chur-init",
		Image:           cfg.InitImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/chur-init"},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			RunAsUser:                ptr.To[int64](1001),
			RunAsGroup:               ptr.To(fsGroup),
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
	if providerName == "local" {
		initContainer.VolumeMounts = append(initContainer.VolumeMounts, corev1.VolumeMount{
			Name:      localVolName,
			MountPath: cfg.LocalBasePath,
			ReadOnly:  true,
		})
	}
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

	// Mount the tmpfs volume to every app container.
	for i := range pod.Spec.Containers {
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

	return patches, nil
}
