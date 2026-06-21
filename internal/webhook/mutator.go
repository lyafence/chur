package webhook

import (
	"fmt"

	"github.com/lyafence/chur/internal/validate"
	corev1 "k8s.io/api/core/v1"
)

const (
	annotationProvider  = "chur.io/provider"
	annotationSecret    = "chur.io/secret-ref"
	annotationSecretKey = "chur.io/secret-key"
	annotationMount     = "chur.io/mount-path"
)

// PatchOperation represents a single JSON Patch operation.
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// MutatePod adds a tmpfs volume and init container to the pod spec when the
// chur annotations are present. It returns nil, nil when no mutation is
// required. All user-controlled values are strictly validated before use.
func MutatePod(pod *corev1.Pod) ([]PatchOperation, error) {
	if pod == nil || pod.Annotations == nil {
		return nil, nil
	}

	provider, ok := pod.Annotations[annotationProvider]
	if !ok {
		return nil, nil
	}
	if provider == "" {
		return nil, fmt.Errorf("%s annotation must not be empty", annotationProvider)
	}

	secretRef := pod.Annotations[annotationSecret]
	if err := validate.ValidateSecretRef(secretRef); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", annotationSecret, err)
	}

	secretKey := pod.Annotations[annotationSecretKey]
	if err := validate.ValidateSecretKey(secretKey); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", annotationSecretKey, err)
	}

	mountPath := pod.Annotations[annotationMount]
	if mountPath == "" {
		mountPath = "/secrets"
	}

	volName := "chur-secrets"
	patches := []PatchOperation{}

	// Add the tmpfs volume, creating the array if necessary.
	if len(pod.Spec.Volumes) == 0 {
		patches = append(patches, PatchOperation{
			Op:   "add",
			Path: "/spec/volumes",
			Value: []corev1.Volume{{
				Name: volName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						Medium: corev1.StorageMediumMemory,
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
						Medium: corev1.StorageMediumMemory,
					},
				},
			},
		})
	}

	initEnv := []corev1.EnvVar{
		{Name: "CHUR_PROVIDER", Value: provider},
		{Name: "CHUR_SECRET_REF", Value: secretRef},
		{Name: "CHUR_MOUNT_PATH", Value: mountPath},
	}
	if secretKey != "" {
		initEnv = append(initEnv, corev1.EnvVar{Name: "CHUR_SECRET_KEY", Value: secretKey})
	}

	// Add the chur-init init container, creating the array if necessary.
	initContainer := corev1.Container{
		Name:            "chur-init",
		Image:           "ghcr.io/lyafence/chur-init:latest",
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/chur-init"},
		Env:             initEnv,
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
