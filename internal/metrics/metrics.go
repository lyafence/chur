package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Webhook metrics.
var (
	WebhookAdmissionRequestsTotal = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "chur_webhook_admission_requests_total",
		Help: "Total number of admission review requests processed by the webhook.",
	}, []string{"allowed", "mutated"})

	WebhookAdmissionDurationSeconds = promauto.With(Registry).NewHistogram(prometheus.HistogramOpts{
		Name:    "chur_webhook_admission_duration_seconds",
		Help:    "Duration of admission review processing in seconds.",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10},
	})

	WebhookAdmissionErrorsTotal = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "chur_webhook_admission_errors_total",
		Help: "Total number of admission review errors by reason.",
	}, []string{"reason"})

	WebhookProviderInjectionsTotal = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "chur_webhook_provider_injections_total",
		Help: "Total number of provider injections by provider name.",
	}, []string{"provider"})

	WebhookConcurrentRequests = promauto.With(Registry).NewGauge(prometheus.GaugeOpts{
		Name: "chur_webhook_concurrent_requests",
		Help: "Current number of concurrent admission review requests being processed.",
	})

	WebhookAdmissionResultsTotal = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "chur_webhook_admission_results_total",
		Help: "Total number of admission review results by result type.",
	}, []string{"result"})
)

// Keeper metrics.
var (
	KeeperRequestsTotal = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "chur_keeper_requests_total",
		Help: "Total number of secret fetch requests processed by the keeper.",
	}, []string{"backend", "status"})

	KeeperRequestDurationSeconds = promauto.With(Registry).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "chur_keeper_request_duration_seconds",
		Help:    "Duration of keeper secret fetch requests in seconds.",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10},
	}, []string{"backend"})

	KeeperConcurrentRequests = promauto.With(Registry).NewGauge(prometheus.GaugeOpts{
		Name: "chur_keeper_concurrent_requests",
		Help: "Current number of concurrent secret fetch requests being processed.",
	})
)
