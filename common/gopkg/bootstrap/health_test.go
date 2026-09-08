package bootstrap

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// startTestHealthServer starts a real health server on an OS-assigned port so
// concurrent test targets cannot collide on the fixed probe port, and returns
// it with Stop already registered as cleanup (Stop is idempotent, so the
// cleanup cannot fail a test that stopped the server itself).
func startTestHealthServer(t *testing.T) *healthServer {
	t.Helper()
	h := &healthServer{server: &http.Server{Addr: ":0", Handler: newHealthMux()}}
	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { _ = h.Stop(context.Background()) })
	return h
}

// boundPort returns the port the server actually bound.
func boundPort(t *testing.T, h *healthServer) string {
	t.Helper()
	_, port, err := net.SplitHostPort(h.ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort(%s): %v", h.ln.Addr().String(), err)
	}
	return port
}

// Test_healthServer_Endpoint verifies the probe endpoint contract: GET
// /healthz returns 200 with the fixed body and every other path returns 404
// (specs/052-deploy-health-probe/contracts/bootstrap-health.md §1).
func Test_healthServer_Endpoint(t *testing.T) {
	// given: a started health server on an OS-assigned port.
	h := startTestHealthServer(t)
	base := "http://127.0.0.1:" + boundPort(t, h)

	tests := []struct {
		name     string
		path     string
		wantCode int
		wantBody string
	}{
		{name: "healthz returns ok", path: "/healthz", wantCode: http.StatusOK, wantBody: "ok\n"},
		{name: "root is not found", path: "/", wantCode: http.StatusNotFound},
		{name: "unknown path is not found", path: "/unknown", wantCode: http.StatusNotFound},
		{name: "healthz subpath is not found", path: "/healthz/extra", wantCode: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when: a GET request hits the endpoint.
			resp, err := http.Get(base + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close()

			// then: the status code matches the contract.
			if resp.StatusCode != tt.wantCode {
				t.Fatalf("GET %s status = %d, want %d", tt.path, resp.StatusCode, tt.wantCode)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("GET %s read body: %v", tt.path, err)
			}
			if tt.wantBody != "" && string(body) != tt.wantBody {
				t.Fatalf("GET %s body = %q, want %q", tt.path, string(body), tt.wantBody)
			}
		})
	}
}

// Test_healthServer_StopReleasesPort verifies that Stop releases the bound
// port so it can be bound again — the rollback path must leave no listener
// behind (specs/052-deploy-health-probe/spec.md FR-010).
func Test_healthServer_StopReleasesPort(t *testing.T) {
	// given: a started health server.
	h := startTestHealthServer(t)
	port := boundPort(t, h)

	// when: the server is stopped.
	if err := h.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// then: the same port is free again.
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		t.Fatalf("expected port %s to be free after Stop, got: %v", port, err)
	}
	_ = ln.Close()
}

// Test_healthServer_StartOnHeldPortReturnsError verifies that a Start against
// a port already held by a listener fails with an error mentioning the
// address: a bind failure must surface as a Start error
// (specs/052-deploy-health-probe/spec.md FR-010).
func Test_healthServer_StartOnHeldPortReturnsError(t *testing.T) {
	// given: a started health server holding its OS-assigned port.
	h := startTestHealthServer(t)
	heldAddr := ":" + boundPort(t, h)

	// when: another Start targets the still-bound port.
	held := &healthServer{server: &http.Server{Addr: heldAddr, Handler: newHealthMux()}}
	err := held.Start(context.Background())

	// then: an error mentioning the address is returned.
	if err == nil {
		t.Fatal("expected error for Start on a held port, got nil")
	}
	if !strings.Contains(err.Error(), heldAddr) {
		t.Fatalf("expected error to mention %s, got: %v", heldAddr, err)
	}
}
