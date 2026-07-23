// Package otelsetup installs the process-wide OpenTelemetry TracerProvider
// that exports spans over OTLP/HTTP (Better Stack). ogen handlers already call
// otel.GetTracerProvider(); until Setup runs they are no-ops.
package otelsetup

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"

	"xolo/backend/internal/config"
)

// Setup installs a TracerProvider that exports to cfg.Endpoint when
// cfg.Enabled is true. Returns a shutdown func (no-op when disabled or when
// exporter construction fails — observability must not take the service down).
func Setup(ctx context.Context, cfg config.OTelConfig) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if !cfg.Enabled {
		return noop, nil
	}

	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if endpoint == "" || cfg.Token == "" {
		// validate() should have caught this; belt-and-suspenders.
		return noop, fmt.Errorf("otel: enabled but endpoint/token missing")
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"),
		otlptracehttp.WithHeaders(map[string]string{
			"Authorization": "Bearer " + cfg.Token,
		}),
		otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
	)
	if err != nil {
		// Match Axiom: log and continue without shipping rather than crash.
		slog.Error("otel trace export disabled", "error", err)
		return noop, nil
	}

	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = "notifbuddy-backend"
	}
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		_ = exporter.Shutdown(ctx)
		slog.Error("otel resource build failed; trace export disabled", "error", err)
		return noop, nil
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	slog.Info("otel trace export enabled", "endpoint", endpoint, "service", serviceName)

	return tp.Shutdown, nil
}
