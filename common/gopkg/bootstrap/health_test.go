package bootstrap

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// Test_healthServer_Endpoint verifies the probe endpoint contract: GET
// /healthz returns 200 with the fixed body and every other path returns 404
// (specs/052-deploy-health-probe/contracts/bootstrap-health.md §1).
func Test_healthServer_Endpoint(t *testing.T) {
	// given: a started health server on the fixed address.
	h := newHealthServer()
	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { _ = h.Stop(context.Background()) })

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
			resp, err := http.Get("http://127.0.0.1" + healthAddr + tt.path)
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

// Test_healthServer_StopReleasesPort verifies that Stop releases the fixed
// address so it can be bound again — the rollback path must leave no
// listener behind (specs/052-deploy-health-probe/spec.md FR-010).
func Test_healthServer_StopReleasesPort(t *testing.T) {
	// given: a started health server.
	h := newHealthServer()
	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	// If the Stop under test fails, the cleanup still stops the server so a
	// leaked listener cannot hold :38080 against later tests in this package.
	t.Cleanup(func() { _ = h.Stop(context.Background()) })

	// when: the server is stopped.
	if err := h.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// then: the address is free again.
	ln, err := net.Listen("tcp", healthAddr)
	if err != nil {
		t.Fatalf("expected %s to be free after Stop, got: %v", healthAddr, err)
	}
	_ = ln.Close()
}

// Test_healthServer_DoubleStartReturnsError verifies that a second Start
// fails while the first listener still holds the port: a bind failure must
// surface as a Start error (specs/052-deploy-health-probe/spec.md FR-010).
func Test_healthServer_DoubleStartReturnsError(t *testing.T) {
	// given: a started health server holding the port.
	h := newHealthServer()
	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { _ = h.Stop(context.Background()) })

	// when: Start is called again while the port is still bound.
	err := h.Start(context.Background())

	// then: an error mentioning the address is returned.
	if err == nil {
		t.Fatal("expected error for double Start, got nil")
	}
	if !strings.Contains(err.Error(), healthAddr) {
		t.Fatalf("expected error to mention %s, got: %v", healthAddr, err)
	}
}
