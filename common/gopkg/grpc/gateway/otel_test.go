package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestWithOTelTracing_SetsSpanNameAndRoute(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	defer tp.Shutdown(context.Background())

	otelTracer := tp.Tracer("test")

	// given
	ctx, span := otelTracer.Start(context.Background(), "http")
	defer span.End()

	// Simulate grpc-gateway setting pattern and rpc method in context.
	// In real grpc-gateway, annotateContext does:
	//   ctx = withRPCMethod(ctx, rpcMethodName)
	//   ctx = WithHTTPPathPattern(pattern)(ctx)
	//   ... then calls WithMetadata callback
	// We replicate by passing AnnotateContextOption to simulate.
	ctx = context.WithValue(ctx, struct{}{}, nil) // dummy to get a mutable context

	// Use the actual grpc-gateway internal context helpers by wrapping through AnnotateContext
	// Since we can't access internal funcs, simulate with runtime exported helpers
	// via a fake ServeMux creation with the option, then call the metadata annotator.
	opt := WithOTelTracing()
	mux := runtime.NewServeMux(opt)

	// Simulate what grpc-gateway does: set pattern + rpc method, then call metadata annotator
	// Use runtime.AnnotateContext to set up the context properly
	req := httptest.NewRequest(http.MethodGet, "/v1/hello/world", nil)

	// AnnotateContext sets RPCMethod and HTTPPathPattern before calling WithMetadata
	annotatedCtx, err := runtime.AnnotateContext(
		ctx, mux, req, "/example.Greeter/GetHello",
		runtime.WithHTTPPathPattern("/v1/hello/{name}"),
	)
	if err != nil {
		t.Fatalf("AnnotateContext() error: %v", err)
	}

	// Verify the span was enriched
	span = trace.SpanFromContext(annotatedCtx)
	if span == nil {
		t.Fatal("expected a span in annotated context")
	}

	// End the span to flush it
	span.End()

	// Read the exported span
	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one exported span")
	}

	// The last span should be the one we enriched
	got := spans[len(spans)-1]

	// Verify span name was set to "METHOD /pattern"
	if got.Name != "GET /v1/hello/{name}" {
		t.Fatalf("span name = %q, want %q", got.Name, "GET /v1/hello/{name}")
	}

	// Verify http.route attribute
	var routeAttr attribute.KeyValue
	for _, a := range got.Attributes {
		if string(a.Key) == "http.route" {
			routeAttr = a
		}
	}
	if routeAttr.Key == "" {
		t.Fatal("expected http.route attribute to be set")
	}
	if routeAttr.Value.AsString() != "/v1/hello/{name}" {
		t.Fatalf("http.route = %q, want %q", routeAttr.Value.AsString(), "/v1/hello/{name}")
	}

	// Verify rpc.method_full attribute
	var methodAttr attribute.KeyValue
	for _, a := range got.Attributes {
		if string(a.Key) == "rpc.method_full" {
			methodAttr = a
		}
	}
	if methodAttr.Key == "" {
		t.Fatal("expected rpc.method_full attribute to be set")
	}
	if methodAttr.Value.AsString() != "/example.Greeter/GetHello" {
		t.Fatalf("rpc.method_full = %q, want %q", methodAttr.Value.AsString(), "/example.Greeter/GetHello")
	}
}

func TestWithOTelTracing_NoPattern_NoRename(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	defer tp.Shutdown(context.Background())

	otelTracer := tp.Tracer("test")

	// given
	ctx, span := otelTracer.Start(context.Background(), "original-name")
	defer span.End()

	opt := WithOTelTracing()
	mux := runtime.NewServeMux(opt)

	req := httptest.NewRequest(http.MethodPost, "/unknown", nil)

	// AnnotateContext WITHOUT WithHTTPPathPattern — simulates unmatched route
	annotatedCtx, err := runtime.AnnotateContext(ctx, mux, req, "/example.Unknown/Method")
	if err != nil {
		t.Fatalf("AnnotateContext() error: %v", err)
	}

	span = trace.SpanFromContext(annotatedCtx)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one exported span")
	}

	got := spans[len(spans)-1]

	// Without a pattern, span name should remain unchanged
	if got.Name != "original-name" {
		t.Fatalf("span name = %q, want %q (unchanged)", got.Name, "original-name")
	}

	// rpc.method_full should still be set
	var methodAttr attribute.KeyValue
	for _, a := range got.Attributes {
		if string(a.Key) == "rpc.method_full" {
			methodAttr = a
		}
	}
	if methodAttr.Key == "" {
		t.Fatal("expected rpc.method_full attribute to be set")
	}
}
