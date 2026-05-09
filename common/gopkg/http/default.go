// Package http provides HTTP server instrumentation for dominion services.
package http

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Handler wraps the given http.Handler with OpenTelemetry HTTP instrumentation.
// It creates spans for each incoming request, propagates trace context, and
// records metrics such as request duration and error counts.
// The name parameter is used as the span name for the root HTTP span.
func Handler(h http.Handler, name string) http.Handler {
	return otelhttp.NewHandler(h, name)
}
