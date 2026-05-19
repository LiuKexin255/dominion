// Package gateway provides grpc-gateway instrumentation for dominion services.
package gateway

import (
	"context"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

// WithOTelTracing returns a runtime.ServeMuxOption that enriches the
// OpenTelemetry HTTP span created by the outer otelhttp handler.
//
// It reads the matched HTTP path pattern and RPC method from the
// grpc-gateway context (available after route matching) and sets the
// span name to "METHOD /pattern" and the "http.route" attribute, which
// satisfies the OpenTelemetry HTTP semantic conventions for low-cardinality
// route names.
//
// The outer handler should be wrapped with otelhttp for this to work:
//
//	mux := runtime.NewServeMux(gateway.WithOTelTracing())
//	handler := otelhttp.NewHandler(mux, "grpc-gateway")
func WithOTelTracing() runtime.ServeMuxOption {
	return runtime.WithMetadata(func(ctx context.Context, r *http.Request) metadata.MD {
		span := trace.SpanFromContext(ctx)

		if pattern, ok := runtime.HTTPPathPattern(ctx); ok {
			span.SetAttributes(attribute.String("http.route", pattern))
			span.SetName(r.Method + " " + pattern)
		}

		if method, ok := runtime.RPCMethod(ctx); ok {
			span.SetAttributes(attribute.String("rpc.method_full", method))
		}

		return nil
	})
}
