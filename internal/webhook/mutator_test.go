package webhook

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
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
	patch, err := MutatePod(pod)
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
	_, err := MutatePod(pod)
	if err == nil {
		t.Fatal("expected error for invalid secret-ref")
	}
}

func TestMutatePod_CreatesArrays(t *testing.T) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	patch, err := MutatePod(pod)
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

	patch, err := MutatePod(pod)
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

	patch, err := MutatePod(pod)
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
