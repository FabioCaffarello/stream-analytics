package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var registry = prometheus.NewRegistry()

// Registry returns the shared Prometheus registry.
func Registry() *prometheus.Registry {
	return registry
}

// Handler returns the Prometheus exposition handler for /metrics.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry(), promhttp.HandlerOpts{})
}
