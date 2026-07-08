package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := NewServer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func mustEncodePod(t *testing.T, pod *corev1.Pod) []byte {
	t.Helper()
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		t.Fatalf("to unstructured: %v", err)
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestServer_Mutate_AllowsPod(t *testing.T) {
	t.Parallel()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				annotationProvider: "env",
				annotationSecret:   "my-secret",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "app"}},
		},
	}

	review := admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Object: runtime.RawExtension{Raw: mustEncodePod(t, pod)},
		},
	}

	body, _ := json.Marshal(review)
	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv := newTestServer(t)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp admissionv1.AdmissionReview
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Response == nil {
		t.Fatal("expected response")
	}
	if !resp.Response.Allowed {
		t.Fatalf("expected allowed, got %v", resp.Response.Result)
	}
	if resp.Response.UID != "test-uid" {
		t.Fatalf("expected uid test-uid, got %s", resp.Response.UID)
	}
	if resp.Response.PatchType == nil || *resp.Response.PatchType != admissionv1.PatchTypeJSONPatch {
		t.Fatal("expected JSONPatch")
	}
}

func TestServer_Mutate_DeniesNonPod(t *testing.T) {
	t.Parallel()
	review := admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Kind: metav1.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			Object: runtime.RawExtension{Raw: []byte(`{"apiVersion":"apps/v1","kind":"Deployment"}`)},
		},
	}

	body, _ := json.Marshal(review)
	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv := newTestServer(t)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp admissionv1.AdmissionReview
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Response.Allowed {
		t.Fatal("expected denied for non-Pod request")
	}
}

func TestServer_Mutate_DeniesInvalidPodObject(t *testing.T) {
	t.Parallel()
	review := admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Object: runtime.RawExtension{Raw: []byte(`{"kind":"Pod","apiVersion":"v1","spec":{"containers":"not-an-array"}}`)},
		},
	}

	body, _ := json.Marshal(review)
	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv := newTestServer(t)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp admissionv1.AdmissionReview
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Response.Allowed {
		t.Fatal("expected denied for invalid pod object")
	}
}

func TestServer_RejectMalformedBody(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader([]byte(`{not json`)))
	rec := httptest.NewRecorder()

	srv := newTestServer(t)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestServer_Mutate_DeniesInvalidSecretRef(t *testing.T) {
	t.Parallel()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				annotationProvider: "env",
				annotationSecret:   "foo/bar",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "app"}},
		},
	}

	review := admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Object: runtime.RawExtension{Raw: mustEncodePod(t, pod)},
		},
	}

	body, _ := json.Marshal(review)
	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv := newTestServer(t)
	srv.ServeHTTP(rec, req)

	var resp admissionv1.AdmissionReview
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Response.Allowed {
		t.Fatal("expected denied for invalid secret-ref")
	}
	if resp.Response.Result == nil || resp.Response.Result.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 BadRequest for validation error, got %v", resp.Response.Result)
	}
}

func TestServer_RejectMethod(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/mutate", nil)
	rec := httptest.NewRecorder()

	srv := newTestServer(t)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestServer_Mutate_DryRun_NoPatch(t *testing.T) {
	t.Parallel()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				annotationProvider: "env",
				annotationSecret:   "my-secret",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "app"}},
		},
	}
	dryRun := true
	review := admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID:    "test-uid",
			DryRun: &dryRun,
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Object: runtime.RawExtension{Raw: mustEncodePod(t, pod)},
		},
	}

	body, _ := json.Marshal(review)
	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv := newTestServer(t)
	srv.ServeHTTP(rec, req)

	var resp admissionv1.AdmissionReview
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Response == nil {
		t.Fatal("expected response")
	}
	if !resp.Response.Allowed {
		t.Fatalf("expected allowed during dry-run, got %v", resp.Response.Result)
	}
	if resp.Response.Patch != nil {
		t.Fatal("expected no patch during dry-run")
	}
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	h := HealthHandler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestHealthz_WrongMethod(t *testing.T) {
	t.Parallel()
	h := HealthHandler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/healthz", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestReadyz(t *testing.T) {
	t.Parallel()
	h := HealthHandler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestHealthHandler_UnknownPath(t *testing.T) {
	t.Parallel()
	h := HealthHandler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/foo")
	if err != nil {
		t.Fatalf("GET /foo: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServer_ConcurrencyLimit(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.MaxConcurrent = 1
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	blocked := make(chan struct{})
	release := make(chan struct{})

	srv.mutateFn = func(review *admissionv1.AdmissionReview) *admissionv1.AdmissionReview {
		close(blocked)
		<-release
		return &admissionv1.AdmissionReview{
			Response: &admissionv1.AdmissionResponse{Allowed: true},
		}
	}

	// First request holds the only concurrency slot.
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader([]byte("{}")))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
	}()

	<-blocked

	// Second request should be rejected because the slot is taken.
	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader([]byte("{}")))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when busy, got %d", rec.Code)
	}
	close(release)
}
