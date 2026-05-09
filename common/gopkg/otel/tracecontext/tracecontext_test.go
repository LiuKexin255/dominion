package tracecontext

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestEnsure(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		wantNew bool
	}{
		{
			name:    "empty context generates new trace",
			ctx:     context.Background(),
			wantNew: true,
		},
		{
			name: "existing valid span context is preserved",
			ctx: func() context.Context {
				spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
					TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
					SpanID:     [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
					TraceFlags: trace.FlagsSampled,
				})
				return trace.ContextWithSpanContext(context.Background(), spanCtx)
			}(),
			wantNew: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalID := ID(tt.ctx)
			got := Ensure(tt.ctx)
			gotID := ID(got)

			if gotID == "" {
				t.Fatal("Ensure() produced empty trace ID")
			}
			if len(gotID) != 32 {
				t.Fatalf("trace ID length = %d, want 32", len(gotID))
			}
			if tt.wantNew && gotID == originalID {
				t.Fatal("Ensure() should have generated a new trace ID")
			}
			if !tt.wantNew && gotID != originalID {
				t.Fatalf("Ensure() should have preserved existing trace ID: got %q, want %q", gotID, originalID)
			}
		})
	}
}

func TestEnsureGeneratesUniqueIDs(t *testing.T) {
	ids := make(map[string]struct{})
	for range 100 {
		ctx := Ensure(context.Background())
		id := ID(ctx)
		if _, exists := ids[id]; exists {
			t.Fatalf("duplicate trace ID generated: %s", id)
		}
		ids[id] = struct{}{}
	}
}

func TestID(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{
			name: "empty context returns empty",
			ctx:  context.Background(),
			want: "",
		},
		{
			name: "valid span context returns hex",
			ctx: func() context.Context {
				spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
					TraceID:    [16]byte{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
					SpanID:     [8]byte{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
					TraceFlags: trace.FlagsSampled,
				})
				return trace.ContextWithSpanContext(context.Background(), spanCtx)
			}(),
			want: "4bf92f3577b34da6a3ce929d0e0e4736",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ID(tt.ctx)
			if got != tt.want {
				t.Fatalf("ID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnviron(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want int // number of env vars expected
	}{
		{
			name: "empty context returns nil",
			ctx:  context.Background(),
			want: 0,
		},
		{
			name: "ensured context returns traceparent",
			ctx:  Ensure(context.Background()),
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Environ(tt.ctx)
			if len(got) != tt.want {
				t.Fatalf("Environ() returned %d entries, want %d", len(got), tt.want)
			}
			if tt.want > 0 {
				if !strings.HasPrefix(got[0], EnvKey+"=") {
					t.Fatalf("Environ() = %q, want prefix %q", got[0], EnvKey+"=")
				}
				parts := strings.SplitN(got[0], "=", 2)
				traceparent := parts[1]
				if !strings.HasPrefix(traceparent, "00-") {
					t.Fatalf("traceparent = %q, want W3C format starting with '00-'", traceparent)
				}
			}
		})
	}
}

func TestEnviron_RoundTrip(t *testing.T) {
	ctx := Ensure(context.Background())
	envVars := Environ(ctx)
	if len(envVars) != 1 {
		t.Fatalf("Environ() = %d entries, want 1", len(envVars))
	}

	parts := strings.SplitN(envVars[0], "=", 2)
	traceparent := parts[1]

	parsedCtx := propagation.TraceContext{}.Extract(context.Background(), &envGetter{val: traceparent})
	parsedID := ID(parsedCtx)
	originalID := ID(ctx)

	if parsedID != originalID {
		t.Fatalf("round-trip: parsed trace ID %q != original %q", parsedID, originalID)
	}
}

func TestFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		wantID  string
		wantNew bool
	}{
		{
			name:    "absent env generates new trace",
			envVal:  "",
			wantNew: true,
		},
		{
			name:   "valid traceparent is parsed",
			envVal: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			wantID: "4bf92f3577b34da6a3ce929d0e0e4736",
		},
		{
			name:    "invalid traceparent generates new trace",
			envVal:  "invalid",
			wantNew: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvKey, tt.envVal)

			got := FromEnv(context.Background())
			gotID := ID(got)

			if gotID == "" {
				t.Fatal("FromEnv() produced empty trace ID")
			}
			if tt.wantNew {
				if gotID == tt.wantID {
					t.Fatal("FromEnv() should have generated a new trace ID")
				}
			} else {
				if gotID != tt.wantID {
					t.Fatalf("trace ID = %q, want %q", gotID, tt.wantID)
				}
			}
		})
	}
}

func TestFromEnv_EnsureRoundTrip(t *testing.T) {
	originalCtx := Ensure(context.Background())
	originalID := ID(originalCtx)

	envVars := Environ(originalCtx)
	parts := strings.SplitN(envVars[0], "=", 2)
	t.Setenv(EnvKey, parts[1])

	parsedCtx := FromEnv(context.Background())
	parsedID := ID(parsedCtx)

	if parsedID != originalID {
		t.Fatalf("FromEnv round-trip: %q != original %q", parsedID, originalID)
	}
}

func TestHTTPTransport(t *testing.T) {
	ctx := Ensure(context.Background())
	traceID := ID(ctx)

	var receivedHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("traceparent")
	}))
	defer server.Close()

	client := &http.Client{
		Transport: NewHTTPTransport(http.DefaultTransport),
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

	if receivedHeader == "" {
		t.Fatal("server received no traceparent header")
	}
	if !strings.Contains(receivedHeader, traceID) {
		t.Fatalf("traceparent header = %q, want to contain trace ID %q", receivedHeader, traceID)
	}
}

func TestHTTPTransport_NilBase(t *testing.T) {
	transport := NewHTTPTransport(nil)
	if transport.base != http.DefaultTransport {
		t.Fatal("NewHTTPTransport(nil) should use http.DefaultTransport")
	}
}

func TestHTTPTransport_NoTraceInCtx(t *testing.T) {
	var receivedHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("traceparent")
	}))
	defer server.Close()

	client := &http.Client{
		Transport: NewHTTPTransport(http.DefaultTransport),
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

	// No trace in ctx → no traceparent header injected
	if receivedHeader != "" {
		t.Fatalf("expected no traceparent header, got %q", receivedHeader)
	}
}

func TestFullChain(t *testing.T) {
	// Simulate the full guitar → deploy → HTTP chain.
	// Step 1: guitar generates trace
	guitarCtx := Ensure(context.Background())
	guitarTraceID := ID(guitarCtx)

	// Step 2: guitar propagates via env
	envVars := Environ(guitarCtx)
	if len(envVars) != 1 {
		t.Fatalf("Environ() = %d entries, want 1", len(envVars))
	}
	parts := strings.SplitN(envVars[0], "=", 2)

	// Step 3: deploy picks up from env (simulated)
	os.Setenv(EnvKey, parts[1])
	defer os.Unsetenv(EnvKey)

	deployCtx := FromEnv(context.Background())
	deployTraceID := ID(deployCtx)

	if deployTraceID != guitarTraceID {
		t.Fatalf("deploy trace ID %q != guitar trace ID %q", deployTraceID, guitarTraceID)
	}

	// Step 4: deploy makes HTTP request
	var httpHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpHeader = r.Header.Get("traceparent")
	}))
	defer server.Close()

	client := &http.Client{Transport: NewHTTPTransport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(deployCtx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error: %v", err)
	}
	resp.Body.Close()

	if !strings.Contains(httpHeader, guitarTraceID) {
		t.Fatalf("HTTP traceparent = %q, want to contain trace ID %q", httpHeader, guitarTraceID)
	}
}
