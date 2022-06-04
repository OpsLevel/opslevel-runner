package pkg

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var _tracerProvider trace.TracerProvider

func GetTracer() trace.Tracer {
	if _tracerProvider == nil {
		_tracerProvider = otel.GetTracerProvider()
	}
	return _tracerProvider.Tracer("opslevel-runner")
}
// Seems like the opentelemetry metrics stuff is not fully baked yet
//
//var _meterProvider metric.MeterProvider
//var (
//	NewMetricJobsStarted    syncint64.Counter
//	NewMetricJobsDuration   syncfloat64.Histogram
//	NewMetricJobsFinished   syncint64.Counter
//	NewMetricJobsProcessing asyncint64.Gauge
//	NewRunnerAttribute      attribute.KeyValue
//)
//
//func GetMeter() metric.Meter {
//	if _meterProvider == nil {
//		_meterProvider = global.MeterProvider()
//	}
//	return _meterProvider.Meter("opslevel-runner")
//}
//
//func InitMetrics(id string) {
//	meter := GetMeter()
//	NewMetricJobsStarted, _ = meter.SyncInt64().Counter(
//		"jobs_started",
//		instrument.WithUnit(unit.Dimensionless),
//		instrument.WithDescription("The count of jobs that started processing."),
//	)
//	NewMetricJobsDuration, _ = meter.SyncFloat64().Histogram(
//		"jobs_duration",
//		instrument.WithUnit(unit.Milliseconds),
//		instrument.WithDescription("The duration of jobs in milliseconds."),
//	)
//	NewMetricJobsFinished, _ = meter.SyncInt64().Counter(
//		"jobs_finished",
//		instrument.WithUnit(unit.Dimensionless),
//		instrument.WithDescription("The count of jobs that finished processing by outcome status."),
//	)
//	NewMetricJobsProcessing, _ = meter.AsyncInt64().Gauge(
//		"jobs_processing",
//		instrument.WithUnit(unit.Dimensionless),
//		instrument.WithDescription("The current number of active jobs being processed."),
//	)
//	NewRunnerAttribute = attribute.String("runner", id)
//	NewMetricJobsStarted.Add(context.Background(), 1, attribute.String("foo", "bar"), NewRunnerAttribute)
//}
