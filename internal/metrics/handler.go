package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns an http.Handler that serves Prometheus metrics from the
// custom chur registry on the /metrics endpoint.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
}
