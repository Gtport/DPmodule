package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ServiceMetrics tracks operation and error counts per service.
// Each service creates its own instance via NewServiceMetrics.
type ServiceMetrics struct {
	operations *prometheus.CounterVec
	errors     *prometheus.CounterVec
	duration   *prometheus.HistogramVec
}

// NewServiceMetrics registers Prometheus metrics for the given service name.
// Call once per service on startup (e.g. NewServiceMetrics("item")).
func NewServiceMetrics(service string) *ServiceMetrics {
	labels := []string{"operation"}
	return &ServiceMetrics{
		operations: promauto.NewCounterVec(prometheus.CounterOpts{
			Name:        "service_operations_total",
			Help:        "Total number of service operations.",
			ConstLabels: prometheus.Labels{"service": service},
		}, labels),

		errors: promauto.NewCounterVec(prometheus.CounterOpts{
			Name:        "service_errors_total",
			Help:        "Total number of failed service operations.",
			ConstLabels: prometheus.Labels{"service": service},
		}, labels),

		duration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "service_operation_duration_seconds",
			Help:        "Service operation latency.",
			ConstLabels: prometheus.Labels{"service": service},
			Buckets:     prometheus.DefBuckets,
		}, labels),
	}
}

// IncOperation increments the operation counter for the given operation name.
func (m *ServiceMetrics) IncOperation(operation string) {
	m.operations.WithLabelValues(operation).Inc()
}

// IncError increments the error counter for the given operation name.
func (m *ServiceMetrics) IncError(operation string) {
	m.errors.WithLabelValues(operation).Inc()
}

// ObserveDuration records the duration of an operation in seconds.
func (m *ServiceMetrics) ObserveDuration(operation string, seconds float64) {
	m.duration.WithLabelValues(operation).Observe(seconds)
}
