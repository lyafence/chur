package webhook

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/lyafence/chur/internal/provider"
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
	t.Parallel()
	pod := podWithAnnotations(nil)
	patch, _, err := MutatePod(pod, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patch != nil {
		t.Fatalf("expected nil patch, got %v", patch)
	}
}

func TestMutatePod_InvalidSecretRef(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "foo/bar",
	})
	_, _, err := MutatePod(pod, DefaultConfig())
	if err == nil {
		t.Fatal("expected error for invalid secret-ref")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestMutatePod_InvalidSecretKey(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider:  "env",
		annotationSecret:    "my-secret",
		annotationSecretKey: "bad/key",
	})
	_, _, err := MutatePod(pod, DefaultConfig())
	if err == nil {
		t.Fatal("expected error for invalid secret-key")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestMutatePod_UnknownProvider(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "vault",
		annotationSecret:   "my-secret",
	})
	_, _, err := MutatePod(pod, DefaultConfig())
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestMutatePod_InvalidMountPath(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
		annotationMount:    "../../etc",
	})
	_, _, err := MutatePod(pod, DefaultConfig())
	if err == nil {
		t.Fatal("expected error for invalid mount-path")
	}
}

func TestMutatePod_CreatesArrays(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	patch, _, err := MutatePod(pod, DefaultConfig())
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

	// Verify patch content: securityContext fsGroup, volumes, initContainers, container volumeMounts.
	if len(ops) != 4 {
		t.Fatalf("expected 4 patch operations, got %d", len(ops))
	}

	var (
		hasFSGroup       bool
		hasVolumes       bool
		hasInitContainer bool
		hasVolumeMount   bool
	)
	for _, op := range ops {
		switch op["path"] {
		case "/spec/securityContext":
			hasFSGroup = true
		case "/spec/volumes":
			hasVolumes = true
			val, ok := op["value"].([]any)
			if !ok || len(val) != 1 {
				t.Error("expected one volume in /spec/volumes")
				continue
			}
			vol, ok := val[0].(map[string]any)
			if !ok {
				t.Error("volume value is not a map")
				continue
			}
			emptyDir, ok := vol["emptyDir"].(map[string]any)
			if !ok {
				t.Error("emptyDir missing in volume")
				continue
			}
			if emptyDir["medium"] != "Memory" {
				t.Errorf("expected emptyDir medium=Memory, got %v", emptyDir["medium"])
			}
		case "/spec/initContainers":
			hasInitContainer = true
			val, ok := op["value"].([]any)
			if !ok || len(val) != 1 {
				t.Error("expected one init container")
				continue
			}
			ic, ok := val[0].(map[string]any)
			if !ok {
				t.Error("init container value is not a map")
				continue
			}
			if ic["image"] == nil || ic["image"] == "" {
				t.Error("init container has no image")
			}
			if mounts, ok := ic["volumeMounts"].([]any); !ok || len(mounts) == 0 {
				t.Error("init container has no volume mounts")
			}
		case "/spec/containers/0/volumeMounts":
			hasVolumeMount = true
		}
	}
	if !hasFSGroup {
		t.Error("missing /spec/securityContext operation")
	}
	if !hasVolumes {
		t.Error("missing /spec/volumes operation")
	}
	if !hasInitContainer {
		t.Error("missing /spec/initContainers operation")
	}
	if !hasVolumeMount {
		t.Error("missing container volumeMounts operation")
	}
}

func TestMutatePod_AppendsToExistingArrays(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})
	pod.Spec.Volumes = []corev1.Volume{{Name: "existing"}}
	pod.Spec.InitContainers = []corev1.Container{{Name: "existing-init", Image: "init"}}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "existing-mount"}}

	patch, _, err := MutatePod(pod, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, op := range patch {
		if op.Path != "/spec/containers/0/volumeMounts/-" &&
			op.Path != "/spec/volumes/-" &&
			op.Path != "/spec/initContainers/-" &&
			op.Path != "/spec/securityContext" &&
			op.Path != "/spec/securityContext/fsGroup" {
			t.Fatalf("expected append path, got %q", op.Path)
		}
	}
}

func TestMutatePod_AddsFSGroup(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})
	patch, _, err := MutatePod(pod, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasFSGroup := false
	for _, op := range patch {
		if op.Path == "/spec/securityContext" || op.Path == "/spec/securityContext/fsGroup" {
			hasFSGroup = true
		}
	}
	if !hasFSGroup {
		t.Fatal("expected fsGroup patch")
	}
}

func TestMutatePod_RespectsExistingFSGroup(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})
	pod.Spec.SecurityContext = &corev1.PodSecurityContext{
		FSGroup: ptr.To[int64](2000),
	}
	patch, _, err := MutatePod(pod, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range patch {
		if op.Path == "/spec/securityContext" || op.Path == "/spec/securityContext/fsGroup" {
			t.Fatal("expected no fsGroup patch when already set")
		}
	}
}

func TestMutatePod_PassesSecretKey(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider:  "k8s",
		annotationSecret:    "my-secret",
		annotationSecretKey: "token",
	})

	cfg := DefaultConfig()
	patch, _, err := MutatePod(pod, cfg)
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

func TestMutatePod_PassesInitConfig(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	cfg := DefaultConfig()
	cfg.MaxSecretSize = "2Mi"
	cfg.LocalBasePath = "/custom/secrets"

	patch, _, err := MutatePod(pod, cfg)
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

	want := map[string]string{
		"CHUR_MAX_SECRET_SIZE": "2Mi",
		"CHUR_LOCAL_BASE_PATH": "/custom/secrets",
	}
	got := map[string]string{}
	for _, env := range initContainer.Env {
		got[env.Name] = env.Value
	}
	for name, value := range want {
		if got[name] != value {
			t.Fatalf("expected env %s=%q, got %q", name, value, got[name])
		}
	}
}

func TestMutatePod_SecurityContext(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	patch, _, err := MutatePod(pod, DefaultConfig())
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
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	patch, _, err := MutatePod(pod, DefaultConfig())
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
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	cfg := DefaultConfig()
	hundredMi := resource.MustParse("100Mi")
	cfg.VolumeSizeLimit = hundredMi

	patch, _, err := MutatePod(pod, cfg)
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
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})
	pod.Spec.Volumes = []corev1.Volume{{Name: "existing-vol"}}

	cfg := DefaultConfig()
	cfg.VolumeSizeLimit = resource.MustParse("10Mi")

	patch, _, err := MutatePod(pod, cfg)
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
	t.Parallel()
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

			patch, _, err := MutatePod(pod, cfg)
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

func toJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMutatePodKeeperEnvInjection(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		VolumeSizeLimit:        resource.MustParse("10Mi"),
		InitImage:              "chur-init:latest",
		MaxSecretSize:          "1Mi",
		LocalBasePath:          "/etc/chur/secrets",
		KeeperServiceName:      "chur-keeper",
		KeeperServiceNamespace: "chur-system",
		KeeperServicePort:      "9443",
		KeeperServerCA:         "/etc/chur-keeper/ca.crt",
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Annotations: map[string]string{
				annotationProvider:         "keeper",
				annotationSecret:           "prod/db/password",
				annotationKeeperSkipVerify: "true",
			},
		},
	}

	patches, _, err := MutatePod(pod, cfg)
	if err != nil {
		t.Fatalf("mutate: %v", err)
	}

	var found bool
	for _, p := range patches {
		if p.Path == "/spec/initContainers" {
			found = true
			var containers []corev1.Container
			if err := json.Unmarshal(toJSON(t, p.Value), &containers); err != nil {
				t.Fatal(err)
			}
			if len(containers) != 1 {
				t.Fatalf("expected 1 init container, got %d", len(containers))
			}
			env := map[string]string{}
			for _, e := range containers[0].Env {
				env[e.Name] = e.Value
			}
			if got, want := env["CHUR_KEEPER_URL"], "https://chur-keeper.chur-system.svc:9443"; got != want {
				t.Errorf("CHUR_KEEPER_URL = %q, want %q", got, want)
			}
			if got, want := env["CHUR_KEEPER_SKIP_VERIFY"], "1"; got != want {
				t.Errorf("CHUR_KEEPER_SKIP_VERIFY = %q, want %q", got, want)
			}
			if got, want := env["CHUR_KEEPER_SERVER_CA"], "/etc/chur-keeper/ca.crt"; got != want {
				t.Errorf("CHUR_KEEPER_SERVER_CA = %q, want %q", got, want)
			}
		}
	}
	if !found {
		t.Error("expected initContainers patch")
	}
}

func TestMutatePodProviderEnvInvalidKey(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		VolumeSizeLimit: resource.MustParse("10Mi"),
		InitImage:       "chur-init:latest",
		MaxSecretSize:   "1Mi",
		LocalBasePath:   "/etc/chur/secrets",
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Annotations: map[string]string{
				annotationProvider:    "keeper",
				annotationSecret:      "prod/db/password",
				annotationProviderEnv: `{"BAD_KEY":"value"}`,
			},
		},
	}
	if _, _, err := MutatePod(pod, cfg); err == nil {
		t.Error("expected error for invalid provider-env key")
	}
}

func TestMutatePod_CustomInitImage(t *testing.T) {
	t.Parallel()
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})

	cfg := DefaultConfig()
	cfg.InitImage = "my-registry/chur-init:v1.0.0"

	patch, _, err := MutatePod(pod, cfg)
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

func TestMutatePod_LocalProvider(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.LocalBasePath = "/host/secrets"
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "local",
		annotationSecret:   "my-secret",
	})
	patch, _, err := MutatePod(pod, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var foundHostPath bool
	var foundReadOnly bool
	for _, op := range patch {
		if op.Path == "/spec/volumes/-" {
			if v, ok := op.Value.(corev1.Volume); ok {
				if v.Name == "chur-local-base" && v.HostPath != nil {
					foundHostPath = true
				}
			}
		}
		if op.Path == "/spec/initContainers" {
			if arr, ok := op.Value.([]corev1.Container); ok && len(arr) > 0 {
				for _, vm := range arr[0].VolumeMounts {
					if vm.Name == "chur-local-base" && vm.ReadOnly {
						foundReadOnly = true
					}
				}
			}
		}
	}
	if !foundHostPath {
		t.Error("expected hostPath volume for local provider")
	}
	if !foundReadOnly {
		t.Error("expected read-only mount for local provider")
	}
}

func TestMutatePod_KeeperSkipVerifyValues(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		VolumeSizeLimit:        resource.MustParse("10Mi"),
		InitImage:              "chur-init:latest",
		MaxSecretSize:          "1Mi",
		LocalBasePath:          "/etc/chur/secrets",
		KeeperServiceName:      "chur-keeper",
		KeeperServiceNamespace: "chur-system",
		KeeperServicePort:      "9443",
	}
	tests := []struct {
		name    string
		annoVal string
		wantEnv bool
	}{
		{"true sets env", "true", true},
		{"1 sets env", "1", true},
		{"false does not set env", "false", false},
		{"empty does not set env", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annos := map[string]string{
				annotationProvider: "keeper",
				annotationSecret:   "ref",
			}
			if tt.annoVal != "" {
				annos[annotationKeeperSkipVerify] = tt.annoVal
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test", Namespace: "default", Annotations: annos,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "app"}},
				},
			}
			patches, _, err := MutatePod(pod, cfg)
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, p := range patches {
				if p.Path == "/spec/initContainers" || p.Path == "/spec/initContainers/-" {
					switch v := p.Value.(type) {
					case corev1.Container:
						for _, e := range v.Env {
							if e.Name == "CHUR_KEEPER_SKIP_VERIFY" {
								found = true
							}
						}
					case []corev1.Container:
						for _, c := range v {
							for _, e := range c.Env {
								if e.Name == "CHUR_KEEPER_SKIP_VERIFY" {
									found = true
								}
							}
						}
					}
				}
			}
			if tt.wantEnv && !found {
				t.Error("expected CHUR_KEEPER_SKIP_VERIFY env var")
			}
			if !tt.wantEnv && found {
				t.Error("unexpected CHUR_KEEPER_SKIP_VERIFY env var")
			}
		})
	}
}

func TestValidProviderEnvKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key   string
		valid bool
	}{
		{"", false},
		{"CHUR_FOO", true},
		{"CHUR_123", true},
		{"CHUR_", true},
		{"chur_foo", false},
		{"BAD_KEY", false},
		{"CHUR_" + strings.Repeat("A", 129), false},
		{"CHUR_HELLO_WORLD", true},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			got := validProviderEnvKey(tc.key)
			if got != tc.valid {
				t.Errorf("validProviderEnvKey(%q) = %v, want %v", tc.key, got, tc.valid)
			}
		})
	}
}

func TestMutatePod_KeeperClientCertMount(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		VolumeSizeLimit:            resource.MustParse("10Mi"),
		InitImage:                  "chur-init:latest",
		MaxSecretSize:              "1Mi",
		LocalBasePath:              "/etc/chur/secrets",
		KeeperServiceName:          "chur-keeper",
		KeeperServiceNamespace:     "chur-system",
		KeeperServicePort:          "9443",
		KeeperTLSCertPath:          "/etc/chur-keeper/client-tls/tls.crt",
		KeeperClientCertSecretName: "keeper-client-tls",
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Annotations: map[string]string{
				annotationProvider: "keeper",
				annotationSecret:   "prod/db/password",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "app"}},
		},
	}

	patches, _, err := MutatePod(pod, cfg)
	if err != nil {
		t.Fatalf("mutate: %v", err)
	}

	var foundVolume bool
	var foundMount bool
	for _, p := range patches {
		// Check volume creation.
		if p.Path == "/spec/volumes" {
			if arr, ok := p.Value.([]corev1.Volume); ok {
				for _, vol := range arr {
					if vol.Name == "chur-keeper-client-tls" &&
						vol.Secret != nil &&
						vol.Secret.SecretName == "keeper-client-tls" &&
						*vol.Secret.DefaultMode == 0444 {
						foundVolume = true
					}
				}
			}
		}
		if p.Path == "/spec/volumes/-" {
			if vol, ok := p.Value.(corev1.Volume); ok {
				if vol.Name == "chur-keeper-client-tls" &&
					vol.Secret != nil &&
					vol.Secret.SecretName == "keeper-client-tls" &&
					*vol.Secret.DefaultMode == 0444 {
					foundVolume = true
				}
			}
		}
		// Check init container volume mount.
		if p.Path == "/spec/initContainers" || p.Path == "/spec/initContainers/-" {
			switch v := p.Value.(type) {
			case corev1.Container:
				for _, vm := range v.VolumeMounts {
					if vm.Name == "chur-keeper-client-tls" && vm.ReadOnly {
						foundMount = true
					}
				}
			case []corev1.Container:
				for _, c := range v {
					for _, vm := range c.VolumeMounts {
						if vm.Name == "chur-keeper-client-tls" && vm.ReadOnly {
							foundMount = true
						}
					}
				}
			}
		}
	}
	if !foundVolume {
		t.Error("expected chur-keeper-client-tls volume with secret keeper-client-tls")
	}
	if !foundMount {
		t.Error("expected chur-keeper-client-tls volume mount in init container")
	}
}

func TestParseProviderEnvReservedEnv(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		VolumeSizeLimit:        resource.MustParse("10Mi"),
		InitImage:              "chur-init:latest",
		MaxSecretSize:          "1Mi",
		LocalBasePath:          "/etc/chur/secrets",
		KeeperServiceName:      "chur-keeper",
		KeeperServiceNamespace: "chur-system",
		KeeperServicePort:      "9443",
	}
	for _, reserved := range []string{
		"CHUR_PROVIDER",
		"CHUR_SECRET_REF",
		"CHUR_SECRET_KEY",
		"CHUR_MOUNT_PATH",
		"CHUR_MAX_SECRET_SIZE",
		"CHUR_LOCAL_BASE_PATH",
		"CHUR_KEEPER_URL",
		"CHUR_KEEPER_TLS_CERT_PATH",
		"CHUR_KEEPER_TLS_KEY_PATH",
		"CHUR_KEEPER_SERVER_CA",
	} {
		t.Run(reserved, func(t *testing.T) {
			anno := `{"` + reserved + `":"value"}`
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Annotations: map[string]string{
						annotationProvider:    "keeper",
						annotationSecret:      "ref",
						annotationProviderEnv: anno,
					},
				},
			}
			if _, _, err := MutatePod(pod, cfg); err == nil {
				t.Errorf("expected error for reserved key %q", reserved)
			}
		})
	}
}

func TestValidProvidersMatchRegistry(t *testing.T) {
	t.Parallel()
	registered := provider.Names()
	for _, name := range registered {
		if !validProviders[name] {
			t.Errorf("registry has %q but validProviders does not", name)
		}
	}
}

func BenchmarkMutatePod_EmptyPod(b *testing.B) {
	pod := podWithAnnotations(nil)
	cfg := DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := MutatePod(pod, cfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMutatePod_AnnotatedPod(b *testing.B) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})
	cfg := DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := MutatePod(pod, cfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMutatePod_ExistingVolumes(b *testing.B) {
	pod := podWithAnnotations(map[string]string{
		annotationProvider: "env",
		annotationSecret:   "my-secret",
	})
	pod.Spec.Volumes = []corev1.Volume{{Name: "existing"}}
	pod.Spec.InitContainers = []corev1.Container{{Name: "existing-init", Image: "init"}}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "existing-mount"}}
	cfg := DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := MutatePod(pod, cfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMutatePod_KeeperWithClientCert(b *testing.B) {
	cfg := &Config{
		VolumeSizeLimit:            resource.MustParse("10Mi"),
		InitImage:                  "chur-init:latest",
		MaxSecretSize:              "1Mi",
		LocalBasePath:              "/etc/chur/secrets",
		KeeperServiceName:          "chur-keeper",
		KeeperServiceNamespace:     "chur-system",
		KeeperServicePort:          "9443",
		KeeperTLSCertPath:          "/etc/chur-keeper/client-tls/tls.crt",
		KeeperTLSKeyPath:           "/etc/chur-keeper/client-tls/tls.key",
		KeeperServerCA:             "/etc/chur-keeper/ca.crt",
		KeeperClientCertSecretName: "keeper-client-tls",
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Annotations: map[string]string{
				annotationProvider: "keeper",
				annotationSecret:   "prod/db/password",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "app"}},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := MutatePod(pod, cfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}
