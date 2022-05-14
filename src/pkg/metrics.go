package pkg

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

var (
	metricNamespace   = "opslevel_runner"
	MetricJobsStarted  prometheus.Counter
	MetricJobsDuration   prometheus.Histogram
	MetricJobsFinished   *prometheus.CounterVec
	MetricJobsProcessing prometheus.Gauge
)

func initMetrics() {
	runner := uuid.New().String()
	log.Info().Msgf("Starting runner for id '%s'", runner)
	MetricJobsStarted = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricNamespace,
		Name: "jobs_started",
		Help: "The count of jobs that started processing.",
		ConstLabels: prometheus.Labels{"runner": runner},
	})
	MetricJobsDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Name: "jobs_duration",
		Help: "The duration of jobs in seconds.",
		ConstLabels: prometheus.Labels{"runner": runner},
	})
	MetricJobsFinished = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricNamespace,
		Name: "jobs_finished",
		Help: "The count of jobs that finished processing by outcome status.",
		ConstLabels: prometheus.Labels{"runner": runner},
	},
	[]string{"outcome"})
	MetricJobsProcessing = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Name:      "jobs_processing",
		Help:      "The current number of active jobs being processed.",
		ConstLabels: prometheus.Labels{"runner": runner},
	})
}

func StartMetricsServer(port int) {
	initMetrics()
	go func() {
		http.Handle("/metrics", promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{})) // Uses a clean instrumentation free handler
		prometheus.Unregister(collectors.NewGoCollector())                                               // Unregisters the go metrics
		prometheusAddress := fmt.Sprintf(":%d", port)
		log.Info().Msgf("Starting promethus metrics service on '%s/metrics'", prometheusAddress)
		http.ListenAndServe(prometheusAddress, nil)
	}()
}
