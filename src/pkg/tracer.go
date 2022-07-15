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
