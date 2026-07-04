package webhook

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func podWithAnnotations(annos map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pod",
			Namespace:   "default",
			Annotations: annos,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "app:latest",
			}},
		},
	}
}

func TestMutatePod_NoAnnotations(t *testing.T) {
	pod := podWithAnnotations(nil)
	patch, err := MutatePod(pod, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patch != nil {
		t.Fatalf("expected nil patch, got %v", patch)
	}
}

func TestMutatePod_InvalidSecretRef(t *testing.T) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "foo/bar",
	})
	_, err := MutatePod(pod, DefaultConfig())
	if err == nil {
		t.Fatal("expected error for invalid secret-ref")
	}
}

func TestMutatePod_InvalidMountPath(t *testing.T) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
		annotationMount:    "../../etc",
	})
	_, err := MutatePod(pod, DefaultConfig())
	if err == nil {
		t.Fatal("expected error for invalid mount-path")
	}
}

func TestMutatePod_CreatesArrays(t *testing.T) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	patch, err := MutatePod(pod, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patch) == 0 {
		t.Fatal("expected non-empty patch")
	}

	// Verify patch is valid JSON.
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("failed to marshal patch: %v", err)
	}
	var ops []map[string]any
	if err := json.Unmarshal(patchBytes, &ops); err != nil {
		t.Fatalf("patch is not valid JSON: %v", err)
	}

	// We expect replace/add for volumes, initContainers, and one container volumeMounts.
	if len(ops) != 3 {
		t.Fatalf("expected 3 patch operations, got %d", len(ops))
	}
}

func TestMutatePod_AppendsToExistingArrays(t *testing.T) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})
	pod.Spec.Volumes = []corev1.Volume{{Name: "existing"}}
	pod.Spec.InitContainers = []corev1.Container{{Name: "existing-init", Image: "init"}}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "existing-mount"}}

	patch, err := MutatePod(pod, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, op := range patch {
		if op.Path != "/spec/containers/0/volumeMounts/-" && op.Path != "/spec/volumes/-" && op.Path != "/spec/initContainers/-" {
			t.Fatalf("expected append path, got %q", op.Path)
		}
	}
}

func TestMutatePod_PassesSecretKey(t *testing.T) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider:  "k8s",
		annotationSecret:    "my-secret",
		annotationSecretKey: "token",
	})

	cfg := DefaultConfig()
	patch, err := MutatePod(pod, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var initContainer *corev1.Container
	for _, op := range patch {
		if op.Path == "/spec/initContainers/-" || op.Path == "/spec/initContainers" {
			c, ok := op.Value.(corev1.Container)
			if ok {
				initContainer = &c
			} else if arr, ok := op.Value.([]corev1.Container); ok && len(arr) > 0 {
				initContainer = &arr[0]
			}
		}
	}
	if initContainer == nil {
		t.Fatal("init container patch not found")
	}

	found := false
	for _, env := range initContainer.Env {
		if env.Name == "CHUR_SECRET_KEY" && env.Value == "token" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected CHUR_SECRET_KEY env var in init container, got %+v", initContainer.Env)
	}
}

func TestMutatePod_SecurityContext(t *testing.T) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	patch, err := MutatePod(pod, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var initContainer *corev1.Container
	for _, op := range patch {
		if op.Path == "/spec/initContainers/-" || op.Path == "/spec/initContainers" {
			c, ok := op.Value.(corev1.Container)
			if ok {
				initContainer = &c
			} else if arr, ok := op.Value.([]corev1.Container); ok && len(arr) > 0 {
				initContainer = &arr[0]
			}
		}
	}
	if initContainer == nil {
		t.Fatal("init container patch not found")
	}

	sc := initContainer.SecurityContext
	if sc == nil {
		t.Fatal("expected security context")
	}
	if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
		t.Fatal("expected RunAsNonRoot = true")
	}
	if sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
		t.Fatal("expected ReadOnlyRootFilesystem = true")
	}
	if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		t.Fatal("expected AllowPrivilegeEscalation = false")
	}
	if sc.Capabilities == nil || len(sc.Capabilities.Drop) == 0 {
		t.Fatal("expected capabilities drop")
	}
}

func TestMutatePod_SizeLimitInEmptyDir(t *testing.T) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	patch, err := MutatePod(pod, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var vol *corev1.Volume
	for _, op := range patch {
		if op.Path == "/spec/volumes" {
			if arr, ok := op.Value.([]corev1.Volume); ok && len(arr) > 0 {
				vol = &arr[0]
			}
		}
	}
	if vol == nil {
		t.Fatal("volume patch not found")
	}
	if vol.EmptyDir == nil {
		t.Fatal("expected emptyDir volume")
	}
	if vol.EmptyDir.SizeLimit == nil {
		t.Fatal("expected sizeLimit")
	}
	tenMi := resource.MustParse("10Mi")
	if vol.EmptyDir.SizeLimit.Value() != tenMi.Value() {
		t.Fatalf("expected sizeLimit 10Mi, got %s", vol.EmptyDir.SizeLimit.String())
	}
}

func TestMutatePod_CustomSizeLimit(t *testing.T) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	cfg := DefaultConfig()
	hundredMi := resource.MustParse("100Mi")
	cfg.VolumeSizeLimit = hundredMi

	patch, err := MutatePod(pod, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var vol *corev1.Volume
	for _, op := range patch {
		if op.Path == "/spec/volumes" {
			if arr, ok := op.Value.([]corev1.Volume); ok && len(arr) > 0 {
				vol = &arr[0]
			}
		}
	}
	if vol == nil {
		t.Fatal("volume patch not found")
	}
	if vol.EmptyDir.SizeLimit.Value() != hundredMi.Value() {
		t.Fatalf("expected sizeLimit 100Mi, got %s", vol.EmptyDir.SizeLimit.String())
	}
}

func TestMutatePod_SizeLimitInAppendPath(t *testing.T) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})
	pod.Spec.Volumes = []corev1.Volume{{Name: "existing-vol"}}

	cfg := DefaultConfig()
	cfg.VolumeSizeLimit = resource.MustParse("10Mi")

	patch, err := MutatePod(pod, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var vol *corev1.Volume
	for _, op := range patch {
		if op.Path == "/spec/volumes/-" {
			if v, ok := op.Value.(corev1.Volume); ok {
				vol = &v
			}
		}
	}
	if vol == nil {
		t.Fatal("expected volume append patch at /spec/volumes/-")
	}
	if vol.EmptyDir == nil {
		t.Fatal("expected emptyDir volume")
	}
	if vol.EmptyDir.SizeLimit == nil {
		t.Fatal("expected sizeLimit")
	}
	tenMi := resource.MustParse("10Mi")
	if vol.EmptyDir.SizeLimit.Value() != tenMi.Value() {
		t.Fatalf("expected sizeLimit 10Mi on append path, got %s", vol.EmptyDir.SizeLimit.String())
	}
}

func TestMutatePod_AllowedNamespaces(t *testing.T) {
	tests := []struct {
		name         string
		podNamespace string
		allowed      []string
		expectPatch  bool
	}{
		{"in allowed list", "default", []string{"default"}, true},
		{"not in allowed list", "kube-system", []string{"default"}, false},
		{"empty allowed list (allow all)", "kube-system", nil, true},
		{"multiple allowed", "prod", []string{"default", "prod"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := podWithAnnotations(map[string]string{
				annotationProvider: "env",
				annotationSecret:   "my-secret",
			})
			pod.Namespace = tt.podNamespace

			cfg := DefaultConfig()
			cfg.AllowedNamespaces = tt.allowed

			patch, err := MutatePod(pod, cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.expectPatch && patch == nil {
				t.Fatal("expected patch, got nil")
			}
			if !tt.expectPatch && patch != nil {
				t.Fatal("expected nil patch, got non-nil")
			}
		})
	}
}

func TestMutatePod_CustomInitImage(t *testing.T) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	cfg := DefaultConfig()
	cfg.InitImage = "my-registry/chur-init:v1.0.0"

	patch, err := MutatePod(pod, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var initContainer *corev1.Container
	for _, op := range patch {
		if op.Path == "/spec/initContainers/-" || op.Path == "/spec/initContainers" {
			c, ok := op.Value.(corev1.Container)
			if ok {
				initContainer = &c
			} else if arr, ok := op.Value.([]corev1.Container); ok && len(arr) > 0 {
				initContainer = &arr[0]
			}
		}
	}
	if initContainer == nil {
		t.Fatal("init container patch not found")
	}
	if initContainer.Image != "my-registry/chur-init:v1.0.0" {
		t.Fatalf("expected image my-registry/chur-init:v1.0.0, got %s", initContainer.Image)
	}
}
