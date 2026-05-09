package otel

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	logglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// mockSpanExporter implements sdktrace.SpanExporter for testing.
type mockSpanExporter struct {
	shutdownCalled bool
	exportCalled   bool
}

func (m *mockSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	m.exportCalled = true
	return nil
}

func (m *mockSpanExporter) Shutdown(ctx context.Context) error {
	m.shutdownCalled = true
	return nil
}

// mockMetricExporter implements sdkmetric.Exporter for testing.
type mockMetricExporter struct {
	shutdownCalled bool
	exportCalled   bool
}

func (m *mockMetricExporter) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return metricdata.CumulativeTemporality
}

func (m *mockMetricExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return nil
}

func (m *mockMetricExporter) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	m.exportCalled = true
	return nil
}

func (m *mockMetricExporter) ForceFlush(ctx context.Context) error {
	return nil
}

func (m *mockMetricExporter) Shutdown(ctx context.Context) error {
	m.shutdownCalled = true
	return nil
}

// mockLogExporter implements log.Exporter for testing.
type mockLogExporter struct {
	shutdownCalled bool
	exportCalled   bool
}

func (m *mockLogExporter) Export(ctx context.Context, records []log.Record) error {
	m.exportCalled = true
	return nil
}

func (m *mockLogExporter) ForceFlush(ctx context.Context) error {
	return nil
}

func (m *mockLogExporter) Shutdown(ctx context.Context) error {
	m.shutdownCalled = true
	return nil
}

// resetInitState resets the singleton init state for a clean test.
func resetInitState(t *testing.T) {
	t.Helper()

	originalOnce := initOnce
	originalErr := initErr
	originalShutdown := initShutdown

	t.Cleanup(func() {
		initOnce = originalOnce
		initErr = originalErr
		initShutdown = originalShutdown
	})

	initOnce = &sync.Once{}
	initErr = nil
	initShutdown = nil
}

// stubEnv sets lookupEnv to return values from the given map.
func stubEnv(t *testing.T, env map[string]string) {
	t.Helper()

	original := lookupEnv
	t.Cleanup(func() {
		lookupEnv = original
	})

	lookupEnv = func(key string) (string, bool) {
		val, ok := env[key]
		return val, ok
	}
}

// restoreGlobalProviders resets global OTel providers to their current values after the test.
func restoreGlobalProviders(t *testing.T) {
	originalTP := otel.GetTracerProvider()
	originalMP := otel.GetMeterProvider()
	originalLP := logglobal.GetLoggerProvider()
	originalProp := otel.GetTextMapPropagator()

	t.Cleanup(func() {
		otel.SetTracerProvider(originalTP)
		otel.SetMeterProvider(originalMP)
		logglobal.SetLoggerProvider(originalLP)
		otel.SetTextMapPropagator(originalProp)
	})
}

func Test_isDeploy(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		wantDeploy bool
	}{
		{name: "all set", env: map[string]string{
			"SERVICE_APP":          "myapp",
			"DOMINION_ENVIRONMENT": "prod",
			"POD_NAMESPACE":        "default",
		}, wantDeploy: true},
		{name: "none set"},
		{name: "missing SERVICE_APP", env: map[string]string{
			"DOMINION_ENVIRONMENT": "prod",
			"POD_NAMESPACE":        "default",
		}},
		{name: "missing DOMINION_ENVIRONMENT", env: map[string]string{
			"SERVICE_APP":   "myapp",
			"POD_NAMESPACE": "default",
		}},
		{name: "missing POD_NAMESPACE", env: map[string]string{
			"SERVICE_APP":          "myapp",
			"DOMINION_ENVIRONMENT": "prod",
		}},
		{name: "SERVICE_APP empty", env: map[string]string{
			"SERVICE_APP":          "",
			"DOMINION_ENVIRONMENT": "prod",
			"POD_NAMESPACE":        "default",
		}},
		{name: "DOMINION_ENVIRONMENT empty", env: map[string]string{
			"SERVICE_APP":          "myapp",
			"DOMINION_ENVIRONMENT": "",
			"POD_NAMESPACE":        "default",
		}},
		{name: "POD_NAMESPACE empty", env: map[string]string{
			"SERVICE_APP":          "myapp",
			"DOMINION_ENVIRONMENT": "prod",
			"POD_NAMESPACE":        "",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubEnv(t, tt.env)

			got := isDeploy()
			if got != tt.wantDeploy {
				t.Fatalf("isDeploy() = %v, want %v", got, tt.wantDeploy)
			}
		})
	}
}

func TestInit_NonDeploy(t *testing.T) {
	resetInitState(t)
	restoreGlobalProviders(t)
	loggerProviderSet.Store(false)
	t.Cleanup(func() { loggerProviderSet.Store(false) })

	stubEnv(t, nil)

	shutdown, err := Init(context.Background())
	if err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init() shutdown = nil")
	}

	// Verify Tracer returns a non-nil tracer.
	tracer := Tracer()
	if tracer == nil {
		t.Fatal("Tracer() = nil")
	}

	// Verify Meter returns a non-nil meter.
	meter := Meter()
	if meter == nil {
		t.Fatal("Meter() = nil")
	}

	// Verify TraceID with a span returns a non-empty trace ID.
	ctx, span := tracer.Start(context.Background(), "test")
	defer span.End()
	traceID := TraceID(ctx)
	if traceID == "" {
		t.Fatal("TraceID(ctx) = \"\", want non-empty trace ID")
	}
	if len(traceID) != 32 {
		t.Fatalf("TraceID(ctx) = %q, want 32-char hex string", traceID)
	}

	// Verify cleanup.
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() unexpected error: %v", err)
	}

	// Non-deploy path should NOT set LoggerProviderSet.
	if IsLoggerProviderSet() {
		t.Fatal("LoggerProviderSet = true after non-deploy init, want false")
	}
}

func TestInit_Deploy(t *testing.T) {
	resetInitState(t)
	restoreGlobalProviders(t)
	loggerProviderSet.Store(false)
	t.Cleanup(func() { loggerProviderSet.Store(false) })

	stubEnv(t, map[string]string{
		"SERVICE_APP":          "myapp",
		"DOMINION_ENVIRONMENT": "prod",
		"POD_NAMESPACE":        "default",
	})

	var (
		traceExp  = &mockSpanExporter{}
		metricExp = &mockMetricExporter{}
		logExp    = &mockLogExporter{}
	)

	originalTraceExporter := newTraceExporter
	originalMetricExporter := newMetricExporter
	originalLogExporter := newLogExporter
	t.Cleanup(func() {
		newTraceExporter = originalTraceExporter
		newMetricExporter = originalMetricExporter
		newLogExporter = originalLogExporter
	})

	newTraceExporter = func(ctx context.Context, opts ...otlptracegrpc.Option) (sdktrace.SpanExporter, error) {
		return traceExp, nil
	}
	newMetricExporter = func(ctx context.Context, opts ...otlpmetricgrpc.Option) (sdkmetric.Exporter, error) {
		return metricExp, nil
	}
	newLogExporter = func(ctx context.Context, opts ...otlploggrpc.Option) (log.Exporter, error) {
		return logExp, nil
	}

	shutdown, err := Init(context.Background())
	if err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init() shutdown = nil")
	}

	// Verify providers are set globally (not nil).
	if tp := otel.GetTracerProvider(); tp == nil {
		t.Fatal("global TracerProvider is nil")
	}
	if mp := otel.GetMeterProvider(); mp == nil {
		t.Fatal("global MeterProvider is nil")
	}
	if lp := logglobal.GetLoggerProvider(); lp == nil {
		t.Fatal("global LoggerProvider is nil")
	}

	// Verify shutdown doesn't error.
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() unexpected error: %v", err)
	}

	// Deploy path should set LoggerProviderSet.
	if !IsLoggerProviderSet() {
		t.Fatal("LoggerProviderSet = false after deploy init, want true")
	}
}

func TestTraceID_NoSpan(t *testing.T) {
	ctx := context.Background()
	got := TraceID(ctx)
	if got != "" {
		t.Fatalf("TraceID(ctx) = %q, want \"\"", got)
	}
}

func TestTraceID_WithSpan(t *testing.T) {
	restoreGlobalProviders(t)

	// Set up a TracerProvider that always samples.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := otel.Tracer("dominion/common/gopkg/otel")
	ctx, span := tracer.Start(context.Background(), "test")
	defer span.End()

	got := TraceID(ctx)
	if got == "" {
		t.Fatal("TraceID(ctx) = \"\", want non-empty trace ID")
	}
	if len(got) != 32 {
		t.Fatalf("TraceID(ctx) = %q, want 32-char hex string", got)
	}

	// Verify it only contains hex characters.
	for _, c := range got {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("TraceID(ctx) = %q contains non-hex character %c", got, c)
		}
	}
}

func TestInit_PartialFailure(t *testing.T) {
	resetInitState(t)
	restoreGlobalProviders(t)
	loggerProviderSet.Store(false)
	t.Cleanup(func() { loggerProviderSet.Store(false) })

	stubEnv(t, map[string]string{
		"SERVICE_APP":          "myapp",
		"DOMINION_ENVIRONMENT": "prod",
		"POD_NAMESPACE":        "default",
	})

	var (
		traceExp = &mockSpanExporter{}
		logExp   = &mockLogExporter{}
	)

	originalTraceExporter := newTraceExporter
	originalMetricExporter := newMetricExporter
	originalLogExporter := newLogExporter
	t.Cleanup(func() {
		newTraceExporter = originalTraceExporter
		newMetricExporter = originalMetricExporter
		newLogExporter = originalLogExporter
	})

	newTraceExporter = func(ctx context.Context, opts ...otlptracegrpc.Option) (sdktrace.SpanExporter, error) {
		return traceExp, nil
	}
	// Metric exporter fails.
	newMetricExporter = func(ctx context.Context, opts ...otlpmetricgrpc.Option) (sdkmetric.Exporter, error) {
		return nil, fmt.Errorf("simulated metric exporter failure")
	}
	newLogExporter = func(ctx context.Context, opts ...otlploggrpc.Option) (log.Exporter, error) {
		return logExp, nil
	}

	_, err := Init(context.Background())
	if err == nil {
		t.Fatal("Init() expected error")
	}

	// Verify the trace exporter was cleaned up (Shutdown called).
	if !traceExp.shutdownCalled {
		t.Fatal("traceExporter.Shutdown() was not called during cleanup")
	}

	// LoggerProviderSet should remain false after partial failure.
	if IsLoggerProviderSet() {
		t.Fatal("LoggerProviderSet = true after partial failure, want false")
	}
}

func TestOptions(t *testing.T) {
	tests := []struct {
		name         string
		opts         []Option
		wantEndpoint string
	}{
		{name: "defaults", wantEndpoint: defaultCollectorEndpoint},
		{
			name:         "custom endpoint",
			opts:         []Option{WithCollectorEndpoint("custom:4317")},
			wantEndpoint: "custom:4317",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			for _, opt := range tt.opts {
				opt(cfg)
			}

			if cfg.collectorEndpoint != tt.wantEndpoint {
				t.Fatalf("config.collectorEndpoint = %q, want %q", cfg.collectorEndpoint, tt.wantEndpoint)
			}
		})
	}
}

func TestInit_Idempotent(t *testing.T) {
	resetInitState(t)
	restoreGlobalProviders(t)
	loggerProviderSet.Store(false)
	t.Cleanup(func() { loggerProviderSet.Store(false) })

	stubEnv(t, nil)

	// First call.
	shutdown1, err1 := Init(context.Background())
	if err1 != nil {
		t.Fatalf("Init() first call unexpected error: %v", err1)
	}
	if shutdown1 == nil {
		t.Fatal("Init() first call shutdown = nil")
	}

	// Second call should return the same result.
	shutdown2, err2 := Init(context.Background())
	if err2 != err1 {
		t.Fatalf("Init() second call error = %v, want %v", err2, err1)
	}
	if shutdown2 == nil {
		t.Fatal("Init() second call shutdown = nil")
	}

	if shutdown1 == nil || shutdown2 == nil {
		t.Fatal("Init() shutdown function is nil")
	}
	if err := shutdown1(context.Background()); err != nil {
		t.Fatalf("shutdown1() unexpected error: %v", err)
	}

	// Non-deploy path should NOT set LoggerProviderSet.
	if IsLoggerProviderSet() {
		t.Fatal("LoggerProviderSet = true after non-deploy init, want false")
	}
}
