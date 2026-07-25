package health

import (
	"context"
	"log/slog"
	"net/http"
)

func HealthzHandler(component string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			slog.WarnContext(r.Context(), component+": health: failed to write response", "error", err)
		}
	}
}

// RegisterHealthEndpoints registers health check and metrics endpoints on the
// given mux. Standard endpoints are /healthz, /readyz, and /metrics. Pass a
// subset to register only specific endpoints (e.g. keeper: /healthz, /metrics).
func RegisterHealthEndpoints(mux *http.ServeMux, component string, metricsHandler http.Handler, endpoints ...string) {
	if len(endpoints) == 0 {
		endpoints = []string{"/healthz", "/readyz", "/metrics"}
	}
	for _, ep := range endpoints {
		switch ep {
		case "/healthz", "/readyz":
			mux.Handle(ep, HealthzHandler(component))
		case "/metrics":
			if metricsHandler != nil {
				mux.Handle(ep, metricsHandler)
			}
		default:
			slog.WarnContext(context.Background(), "health: unknown endpoint registered", "endpoint", ep)
		}
	}
}
