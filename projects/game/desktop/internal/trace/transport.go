// Package trace provides lightweight W3C trace context injection for desktop
// HTTP/WebSocket requests. It wraps the existing common/gopkg/otel/tracecontext
// package to ensure every outgoing request carries a traceparent header.
//
// This is intentionally minimal — no full OTel SDK, just W3C trace context
// propagation so the server can correlate requests from the desktop client.
package trace

import (
	"context"
	"net/http"

	"dominion/common/gopkg/otel/tracecontext"
)

// NewHTTPTransport returns an http.RoundTripper that wraps http.DefaultTransport
// and injects a W3C traceparent header into every outgoing request.
//
// The caller MUST ensure the request context carries a valid trace context
// (e.g. via tracecontext.Ensure) before calling client methods. The traceparent
// header format is:
//
//	00-{32-char-trace-id}-{16-char-span-id}-01
func NewHTTPTransport() http.RoundTripper {
	return tracecontext.NewHTTPTransport(http.DefaultTransport)
}

// TraceIDFromContext extracts the W3C trace ID from ctx for logging purposes.
// Returns an empty string if ctx carries no valid trace context.
func TraceIDFromContext(ctx context.Context) string {
	return tracecontext.ID(ctx)
}
