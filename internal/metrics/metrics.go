package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bulwarkai_requests_total",
		Help: "Total number of requests processed",
	}, []string{"action", "model"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "bulwarkai_request_duration_seconds",
		Help:    "Request duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"action"})

	InspectorDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "bulwarkai_inspector_duration_seconds",
		Help:    "Inspector call duration in seconds",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"inspector", "direction"})

	ActiveRequests = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bulwarkai_active_requests",
		Help: "Number of requests currently being processed",
	})

	RequestBodySize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "bulwarkai_request_body_bytes",
		Help:    "Request body size in bytes",
		Buckets: []float64{100, 1000, 10000, 100000, 500000, 1e6, 5e6, 10e6},
	})
)
