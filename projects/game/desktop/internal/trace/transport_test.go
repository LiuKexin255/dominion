package trace_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"dominion/common/gopkg/otel/tracecontext"
	desktoptrace "dominion/projects/game/desktop/internal/trace"
)

func TestHTTPTransport_InjectTraceparent(t *testing.T) {
	// given: an httptest server that captures the traceparent header
	var traceparent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
	}))
	defer server.Close()

	// when: a request is sent via the trace transport
	client := &http.Client{
		Transport: desktoptrace.NewHTTPTransport(),
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error: %v", err)
	}
	resp.Body.Close()

	// then: the server received a traceparent header in W3C format
	if traceparent == "" {
		t.Fatal("server received no traceparent header")
	}

	// Verify W3C format: 00-{32hex}-{16hex}-01
	pattern := `^00-[0-9a-f]{32}-[0-9a-f]{16}-01$`
	matched, err := regexp.MatchString(pattern, traceparent)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Fatalf("traceparent header = %q, want format 00-{32hex}-{16hex}-01", traceparent)
	}
}

func TestTraceIDFromContext(t *testing.T) {
	// given: a context with a valid trace context
	ctx := tracecontext.Ensure(context.Background())

	// when: extracting the trace ID
	traceID := desktoptrace.TraceIDFromContext(ctx)

	// then: the trace ID is a 32-character hex string
	if traceID == "" {
		t.Fatal("TraceIDFromContext() returned empty string")
	}
	if len(traceID) != 32 {
		t.Fatalf("TraceIDFromContext() = %q (len=%d), want 32 hex chars", traceID, len(traceID))
	}

	// Verify it contains only hex characters
	matched, err := regexp.MatchString(`^[0-9a-f]{32}$`, traceID)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Fatalf("TraceIDFromContext() = %q, want 32 hex chars", traceID)
	}
}

func TestTraceIDFromContext_Empty(t *testing.T) {
	// given: a context without a valid trace context
	ctx := context.Background()

	// when: extracting the trace ID
	traceID := desktoptrace.TraceIDFromContext(ctx)

	// then: the trace ID is empty
	if traceID != "" {
		t.Fatalf("TraceIDFromContext() = %q, want empty string", traceID)
	}
}

func TestHTTPTransport_PreservesExistingTrace(t *testing.T) {
	// given: a context with a known trace ID
	ctx := tracecontext.Ensure(context.Background())
	expectedTraceID := tracecontext.ID(ctx)

	var traceparent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
	}))
	defer server.Close()

	// when: a request with the trace context is sent
	client := &http.Client{
		Transport: desktoptrace.NewHTTPTransport(),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error: %v", err)
	}
	resp.Body.Close()

	// then: the traceparent header contains the expected trace ID
	if !regexp.MustCompile(expectedTraceID).MatchString(traceparent) {
		t.Fatalf("traceparent header = %q, want to contain trace ID %q", traceparent, expectedTraceID)
	}
}
