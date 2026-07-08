package webhook

import (
	"errors"
	"fmt"

	"github.com/lyafence/chur/internal/provider"
	"github.com/lyafence/chur/internal/validate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

// ErrValidation indicates that the pod annotations failed validation.
// The webhook should respond with HTTP 400 BadRequest for these errors.
var ErrValidation = errors.New("validation error")

const (
	annotationProvider  = "chur.io/provider"
	annotationSecret    = "chur.io/secret-ref"
	annotationSecretKey = "chur.io/secret-key"
	annotationMount     = "chur.io/mount-path"

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
	VolumeSizeLimit   resource.Quantity
	AllowedNamespaces []string
	InitImage         string
	MaxSecretSize     string
	LocalBasePath     string
	MaxConcurrent     int
}

// DefaultConfig returns a Config with safe defaults.
func DefaultConfig() *Config {
	return &Config{
		VolumeSizeLimit: resource.MustParse("10Mi"),
		InitImage:       "ghcr.io/lyafence/chur-init:latest",
		MaxSecretSize:   "1Mi",
		LocalBasePath:   "/etc/chur/secrets",
		MaxConcurrent:   100,
	}
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
	if err := validate.ValidateSecretRef(secretRef); err != nil {
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
			Op:   "add",
			Path: "/spec/securityContext",
			Value: &corev1.PodSecurityContext{
				FSGroup: ptr.To(fsGroup),
			},
		})
	} else if pod.Spec.SecurityContext.FSGroup == nil {
		patches = append(patches, PatchOperation{
			Op:    "add",
			Path:  "/spec/securityContext/fsGroup",
			Value: fsGroup,
		})
	}

	// Add the tmpfs volume, creating the array if necessary.
	if len(pod.Spec.Volumes) == 0 {
		patches = append(patches, PatchOperation{
			Op:   "add",
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
			Op:   "add",
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
		},
		Env: initEnv,
		VolumeMounts: []corev1.VolumeMount{
			{Name: volName, MountPath: mountPath},
		},
	}
	if len(pod.Spec.InitContainers) == 0 {
		patches = append(patches, PatchOperation{
			Op:    "add",
			Path:  "/spec/initContainers",
			Value: []corev1.Container{initContainer},
		})
	} else {
		patches = append(patches, PatchOperation{
			Op:    "add",
			Path:  "/spec/initContainers/-",
			Value: initContainer,
		})
	}

	// Mount the tmpfs volume to every app container.
	for i := range pod.Spec.Containers {
		if len(pod.Spec.Containers[i].VolumeMounts) == 0 {
			patches = append(patches, PatchOperation{
				Op:   "add",
				Path: fmt.Sprintf("/spec/containers/%d/volumeMounts", i),
				Value: []corev1.VolumeMount{{
					Name:      volName,
					MountPath: mountPath,
					ReadOnly:  true,
				}},
			})
		} else {
			patches = append(patches, PatchOperation{
				Op:   "add",
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
