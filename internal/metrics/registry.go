package metrics

import "github.com/prometheus/client_golang/prometheus"

// Registry is the custom prometheus registry for chur metrics.
// A custom registry is used instead of the default registry to isolate
// chur metrics from Go runtime metrics and to enable unit testing.
var Registry = prometheus.NewRegistry()
