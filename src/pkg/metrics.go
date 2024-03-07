package pkg

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

var (
	metricNamespace      = "opslevel_runner"
	MetricJobsStarted    prometheus.Counter
	MetricJobsDuration   prometheus.Histogram
	MetricJobsFinished   *prometheus.CounterVec
	MetricJobsProcessing prometheus.Gauge
)

func initMetrics(id string) {
	MetricJobsStarted = promauto.NewCounter(prometheus.CounterOpts{
		Namespace:   metricNamespace,
		Name:        "jobs_started",
		Help:        "The count of jobs that started processing.",
		ConstLabels: prometheus.Labels{"runner": id},
	})
	MetricJobsDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace:   metricNamespace,
		Name:        "jobs_duration",
		Help:        "The duration of jobs in seconds.",
		ConstLabels: prometheus.Labels{"runner": id},
		Buckets:     []float64{5, 30, 60, 120, 300, 600, 900, 1200},
	})
	MetricJobsFinished = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace:   metricNamespace,
		Name:        "jobs_finished",
		Help:        "The count of jobs that finished processing by outcome status.",
		ConstLabels: prometheus.Labels{"runner": id},
	},
		[]string{"outcome"})
	MetricJobsProcessing = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace:   metricNamespace,
		Name:        "jobs_processing",
		Help:        "The current number of active jobs being processed.",
		ConstLabels: prometheus.Labels{"runner": id},
	})
}

func StartMetricsServer(id string, port int) {
	initMetrics(id)
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{})) // Uses a clean instrumentation free handler
		prometheus.Unregister(collectors.NewGoCollector())                                              // Unregisters the go metrics
		prometheusAddress := fmt.Sprintf(":%d", port)
		log.Info().Msgf("Starting metrics service on '%s/metrics'", prometheusAddress)
		err := http.ListenAndServe(prometheusAddress, mux)
		if err != nil {
			log.Error().Err(err).Msgf("metrics service returned error")
		}
	}()
}
