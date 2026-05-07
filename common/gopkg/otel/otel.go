// Package otel provides OpenTelemetry provider initialization for dominion services.
package otel

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	logglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Shutdown flushes and shuts down all OpenTelemetry providers.
type Shutdown func(context.Context) error

const (
	defaultCollectorEndpoint = "dominion-opentelemetry-collector.kube-public.svc.cluster.local:4317"

	envServiceApp   = "SERVICE_APP"
	envDominionEnv  = "DOMINION_ENVIRONMENT"
	envPodNamespace = "POD_NAMESPACE"
)

var (
	// lookupEnv reads process environment variables and allows tests to stub them.
	lookupEnv = os.LookupEnv

	// newTraceExporter creates an OTLP trace exporter via gRPC.
	// Overridable for testing.
	newTraceExporter = func(ctx context.Context, opts ...otlptracegrpc.Option) (sdktrace.SpanExporter, error) {
		return otlptracegrpc.New(ctx, opts...)
	}

	// newMetricExporter creates an OTLP metric exporter via gRPC.
	// Overridable for testing.
	newMetricExporter = func(ctx context.Context, opts ...otlpmetricgrpc.Option) (sdkmetric.Exporter, error) {
		return otlpmetricgrpc.New(ctx, opts...)
	}

	// newLogExporter creates an OTLP log exporter via gRPC.
	// Overridable for testing.
	newLogExporter = func(ctx context.Context, opts ...otlploggrpc.Option) (log.Exporter, error) {
		return otlploggrpc.New(ctx, opts...)
	}
)

var (
	initOnce     = &sync.Once{}
	initErr      error
	initShutdown Shutdown
)

// LoggerProviderSet indicates whether the global LoggerProvider has been
// initialized by initDeploy().  Non-deploy initialization does not create
// a LoggerProvider, leaving this false.
var LoggerProviderSet bool

// IsLoggerProviderSet reports whether initDeploy() has set a global
// LoggerProvider.
func IsLoggerProviderSet() bool {
	return LoggerProviderSet
}

// isDeploy returns true when SERVICE_APP, DOMINION_ENVIRONMENT, and POD_NAMESPACE
// are all present and non-empty.
func isDeploy() bool {
	for _, key := range []string{envServiceApp, envDominionEnv, envPodNamespace} {
		val, ok := lookupEnv(key)
		if !ok || val == "" {
			return false
		}
	}
	return true
}

// Init initializes OpenTelemetry providers based on the deployment environment.
// It is idempotent; subsequent calls return the same Shutdown and error.
func Init(ctx context.Context, opts ...Option) (Shutdown, error) {
	initOnce.Do(func() {
		initShutdown, initErr = initProviders(ctx, opts...)
	})
	return initShutdown, initErr
}

func initProviders(ctx context.Context, opts ...Option) (Shutdown, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if isDeploy() {
		return initDeploy(ctx, cfg)
	}
	return initNonDeploy()
}

func initNonDeploy() (Shutdown, error) {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	mp := sdkmetric.NewMeterProvider()
	otel.SetMeterProvider(mp)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(shutdownCtx context.Context) error {
		var firstErr error
		if err := tp.Shutdown(shutdownCtx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("trace provider shutdown: %w", err)
		}
		if err := mp.Shutdown(shutdownCtx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("metric provider shutdown: %w", err)
		}
		return firstErr
	}, nil
}

func initDeploy(ctx context.Context, cfg *config) (Shutdown, error) {
	endpoint := cfg.collectorEndpoint

	// Create trace exporter.
	traceExp, err := newTraceExporter(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("create otel trace exporter: %w", err)
	}

	// Create metric exporter.
	metricExp, err := newMetricExporter(ctx,
		otlpmetricgrpc.WithEndpoint(endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		_ = traceExp.Shutdown(ctx)
		return nil, fmt.Errorf("create otel metric exporter: %w", err)
	}

	// Create log exporter.
	logExp, err := newLogExporter(ctx,
		otlploggrpc.WithEndpoint(endpoint),
		otlploggrpc.WithInsecure(),
	)
	if err != nil {
		_ = metricExp.Shutdown(ctx)
		_ = traceExp.Shutdown(ctx)
		return nil, fmt.Errorf("create otel log exporter: %w", err)
	}

	// Build resource from environment variables.
	res, err := buildResource()
	if err != nil {
		_ = logExp.Shutdown(ctx)
		_ = metricExp.Shutdown(ctx)
		_ = traceExp.Shutdown(ctx)
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	// Create TracerProvider.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	tpForCleanup := tp

	// Create MeterProvider with 30-second periodic reader.
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(30*time.Second))),
		sdkmetric.WithResource(res),
	)
	mpForCleanup := mp

	// Create LoggerProvider.
	lp := log.NewLoggerProvider(
		log.WithProcessor(log.NewBatchProcessor(logExp)),
		log.WithResource(res),
	)
	lpForCleanup := lp

	// Set global providers.
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	logglobal.SetLoggerProvider(lp)
	LoggerProviderSet = true

	// Set global propagator.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(shutdownCtx context.Context) error {
		var firstErr error
		if err := tpForCleanup.Shutdown(shutdownCtx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("trace provider shutdown: %w", err)
		}
		if err := mpForCleanup.Shutdown(shutdownCtx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("metric provider shutdown: %w", err)
		}
		if err := lpForCleanup.Shutdown(shutdownCtx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("log provider shutdown: %w", err)
		}
		return firstErr
	}, nil
}

func buildResource() (*resource.Resource, error) {
	return resource.Default(), nil
}

// Tracer returns a Tracer from the global TracerProvider.
func Tracer() trace.Tracer {
	return otel.Tracer("dominion/common/gopkg/otel")
}

// Meter returns a Meter from the global MeterProvider.
func Meter() metric.Meter {
	return otel.Meter("dominion/common/gopkg/otel")
}

// TraceID extracts the trace ID from the context's span and returns it as a hex string.
// Returns an empty string if the context contains no valid span.
func TraceID(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return ""
	}
	return spanCtx.TraceID().String()
}
