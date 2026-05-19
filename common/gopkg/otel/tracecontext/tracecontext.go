// Package tracecontext provides lightweight W3C trace context generation and propagation
// for CLI tools. It stores trace information in context.Context using the same mechanism
// as OpenTelemetry (trace.SpanContext), making it compatible with OTel propagation.
//
// CLI tools (guitar, deploy) use this package instead of the full OTel SDK.
// Services that already initialize OTel providers should use common/gopkg/otel directly.
package tracecontext

import (
	"context"
	"crypto/rand"
	"net/http"
	"os"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	// EnvKey is the environment variable key for W3C traceparent propagation.
	EnvKey = "TRACEPARENT"
)

// propagator is the W3C TraceContext propagator used for serialization.
var propagator = propagation.TraceContext{}

// Ensure ensures ctx carries a valid trace span context.
// If ctx already has a valid SpanContext (from OTel, a parent process, or a previous call),
// it returns ctx unchanged. Otherwise it generates a new trace ID and span ID, injects them
// into ctx, and returns the enriched context.
func Ensure(ctx context.Context) context.Context {
	if trace.SpanContextFromContext(ctx).IsValid() {
		return ctx
	}

	var traceID [16]byte
	var spanID [8]byte
	if _, err := rand.Read(traceID[:]); err != nil {
		return ctx
	}
	if _, err := rand.Read(spanID[:]); err != nil {
		return ctx
	}

	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID(traceID),
		SpanID:     trace.SpanID(spanID),
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(ctx, spanCtx)
}

// ID returns the hex trace ID from ctx, or an empty string if ctx carries no valid span.
func ID(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return ""
	}
	return spanCtx.TraceID().String()
}

// FromEnv reads the TRACEPARENT environment variable, parses it as a W3C traceparent,
// and injects the trace context into ctx. If TRACEPARENT is absent or invalid, a new
// trace context is generated via Ensure.
func FromEnv(ctx context.Context) context.Context {
	if v := os.Getenv(EnvKey); v != "" {
		ctx = propagator.Extract(ctx, &envGetter{val: v})
	}
	return Ensure(ctx)
}

// Environ returns environment variable strings derived from ctx's trace context,
// suitable for appending to exec.Cmd.Env. Returns nil if ctx has no valid trace.
//
// Example return value: []string{"TRACEPARENT=00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}
func Environ(ctx context.Context) []string {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return nil
	}

	carrier := new(envSetter)
	propagator.Inject(ctx, carrier)
	if carrier.val == "" {
		return nil
	}
	return []string{EnvKey + "=" + carrier.val}
}

// HTTPTransport wraps an http.RoundTripper to inject the W3C traceparent header
// from ctx into outgoing HTTP requests.
type HTTPTransport struct {
	base http.RoundTripper
}

// NewHTTPTransport creates an HTTPTransport that delegates to base.
// If base is nil, http.DefaultTransport is used.
func NewHTTPTransport(base http.RoundTripper) *HTTPTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &HTTPTransport{base: base}
}

// RoundTrip executes a single HTTP transaction, injecting traceparent header from
// the request context before forwarding to the base transport.
func (t *HTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	propagator.Inject(req.Context(), propagation.HeaderCarrier(req.Header))
	return t.base.RoundTrip(req)
}

// envGetter implements propagation.TextMapCarrier for reading a single env var.
type envGetter struct {
	val string
}

func (e *envGetter) Get(key string) string { return e.val }
func (e *envGetter) Set(string, string)    {}
func (e *envGetter) Keys() []string        { return nil }

// envSetter implements propagation.TextMapCarrier for capturing a propagated value.
type envSetter struct {
	val string
}

func (e *envSetter) Get(string) string   { return "" }
func (e *envSetter) Set(key, val string) { e.val = val }
func (e *envSetter) Keys() []string      { return nil }
