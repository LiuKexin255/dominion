package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	game "dominion/projects/game"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// ---------------------------------------------------------------------------
// Tests: root handler routing (/api/v2 agent surface, /api/v1 regression)
// ---------------------------------------------------------------------------

// newRoutingTestMux builds the HTTP handler the way main() assembles it: the
// SendFrame body guard wrapping the grpc-gateway mux, whose handlers are
// registered on unreachable backends — the proxy connection carrying the v2
// AgentService/DesktopBridgeService handlers and the direct agent_v2
// connection carrying the PresetService handler. Routing assertions can then
// distinguish "reached grpc-gateway and proxied" (503, backend unavailable)
// from "no route" (404); the backend connection choice does not affect the
// route-presence assertions.
func newRoutingTestMux(t *testing.T) http.Handler {
	t.Helper()

	gwmux := newGatewayMux()
	proxyConn, err := grpc.NewClient(
		"unreachable-proxy.invalid:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	t.Cleanup(func() { proxyConn.Close() })

	presetConn, err := grpc.NewClient(
		"unreachable-agent-v2.invalid:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial agent-v2: %v", err)
	}
	t.Cleanup(func() { presetConn.Close() })

	if err := game.RegisterSessionServiceHandler(context.Background(), gwmux, proxyConn); err != nil {
		t.Fatalf("register session handler: %v", err)
	}
	if err := game.RegisterMemoryServiceHandler(context.Background(), gwmux, proxyConn); err != nil {
		t.Fatalf("register memory handler: %v", err)
	}
	if err := game.RegisterAgentServiceHandler(context.Background(), gwmux, proxyConn); err != nil {
		t.Fatalf("register agent handler: %v", err)
	}
	if err := game.RegisterDesktopBridgeServiceHandler(context.Background(), gwmux, proxyConn); err != nil {
		t.Fatalf("register desktop bridge handler: %v", err)
	}
	if err := game.RegisterPresetServiceHandler(context.Background(), gwmux, presetConn); err != nil {
		t.Fatalf("register preset handler: %v", err)
	}

	return sendFrameBodyLimit(gwmux)
}

// TestRootMuxAPIv2SendProxiesToGrpcGateway verifies the /api/v2/ subtree is
// bound to grpc-gateway: a :send request reaches the AgentService
// route and fails proxying to the unreachable backend with 503 (grpc code
// Unavailable) instead of a routing 404 (spec 049-agent-v2-dsh-init FR-013).
func TestRootMuxAPIv2SendProxiesToGrpcGateway(t *testing.T) {
	httpSrv := httptest.NewServer(newRoutingTestMux(t))
	defer httpSrv.Close()

	resp, err := http.Post(
		httpSrv.URL+"/api/v2/templates/saolei/sessions/route-test:send",
		"application/json",
		strings.NewReader(`{"text":"hi"}`),
	)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (grpc-gateway proxy failure, not a routing 404)", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// TestRootMuxAPIv2UnknownPathStillReachesGrpcGateway verifies an /api/v2/
// path that matches no AgentService HTTP rule is answered by
// grpc-gateway's 404 (the subtree routes there), not by the root mux's own
// 404 handler.
func TestRootMuxAPIv2UnknownPathStillReachesGrpcGateway(t *testing.T) {
	httpSrv := httptest.NewServer(newRoutingTestMux(t))
	defer httpSrv.Close()

	resp, err := http.Get(httpSrv.URL + "/api/v2/not-a-resource")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	// grpc-gateway answers unmatched routes with a JSON status body — the
	// root mux's http.NotFound would be plain text.
	body, _ := io.ReadAll(resp.Body)
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q, want grpc-gateway's application/json error body (got body %q)", ct, string(body))
	}
}

// TestRootMuxAPIv2PresetPathsRouteToDirectHandler verifies the /api/v2
// preset CRUD + ListModels paths are bound to the PresetService handler —
// the direct agent_v2 connection, not the proxy. Every path of the six-RPC
// face reaches grpc-gateway and fails proxying to the unreachable backend
// with 503 (grpc code Unavailable — the one-hop configuration-face failure
// table, contracts/agent-api.md §3) instead of a routing 404
// (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §3.4).
func TestRootMuxAPIv2PresetPathsRouteToDirectHandler(t *testing.T) {
	httpSrv := httptest.NewServer(newRoutingTestMux(t))
	defer httpSrv.Close()

	tests := []struct {
		name string
		req  func(url string) (*http.Response, error)
	}{
		{
			name: "ListModels",
			req:  func(url string) (*http.Response, error) { return http.Get(url + "/api/v2/models") },
		},
		{
			name: "ListPresets",
			req:  func(url string) (*http.Response, error) { return http.Get(url + "/api/v2/templates/saolei/presets") },
		},
		{
			name: "CreatePreset",
			req: func(url string) (*http.Response, error) {
				return http.Post(
					url+"/api/v2/templates/saolei/presets?preset_id=base",
					"application/json",
					strings.NewReader(`{"preset":{"name":"templates/saolei/presets/base"}}`),
				)
			},
		},
		{
			name: "GetPreset",
			req: func(url string) (*http.Response, error) {
				return http.Get(url + "/api/v2/templates/saolei/presets/base")
			},
		},
		{
			name: "UpdatePreset",
			req: func(url string) (*http.Response, error) {
				req, err := http.NewRequest(
					http.MethodPatch,
					url+"/api/v2/templates/saolei/presets/base",
					// body: "preset" — the HTTP body maps to the
					// UpdatePresetRequest.preset field itself; the
					// {preset.name} path variable fills the name.
					strings.NewReader(`{"persona":"hi"}`),
				)
				if err != nil {
					return nil, err
				}
				req.Header.Set("Content-Type", "application/json")
				return http.DefaultClient.Do(req)
			},
		},
		{
			name: "DeletePreset",
			req: func(url string) (*http.Response, error) {
				req, err := http.NewRequest(http.MethodDelete, url+"/api/v2/templates/saolei/presets/base", nil)
				if err != nil {
					return nil, err
				}
				return http.DefaultClient.Do(req)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.req(httpSrv.URL)
			if err != nil {
				t.Fatalf("%s: request: %v", tt.name, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("%s: status = %d, want %d (grpc-gateway proxy failure on the direct handler, not a routing 404)",
					tt.name, resp.StatusCode, http.StatusServiceUnavailable)
			}
		})
	}
}

// TestRootMuxAPIv1SessionAndPresetMemoryFallback verifies the /api/v1/
// subtree still routes to grpc-gateway for the services that remain on it:
// session and preset-scoped memory requests reach their handler routes and
// fail proxying to the unreachable backend with 503 (grpc code Unavailable),
// while the removed v1 team face and the superseded session-scoped memory
// path answer a routing 404 (FR-019 — the team/prompt faces are gone;
// memory/session stay, specs/051-agent-v2-dsh-migration/spec.md FR-019;
// memory is preset-scoped since specs/071-memory-preset-scope/contracts/
// memory-service.md §5).
func TestRootMuxAPIv1SessionAndPresetMemoryFallback(t *testing.T) {
	httpSrv := httptest.NewServer(newRoutingTestMux(t))
	defer httpSrv.Close()

	// CreateSession rule (game.proto: post /api/v1/{parent=templates/*}/sessions).
	resp, err := http.Post(httpSrv.URL+"/api/v1/templates/saolei/sessions", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("api/v1 session fallback status = %d, want %d (grpc-gateway proxy failure)", resp.StatusCode, http.StatusServiceUnavailable)
	}

	// ListMemories rule (game.proto: get /api/v1/{parent=templates/*/presets/*}/memories).
	memResp, err := http.Get(httpSrv.URL + "/api/v1/templates/saolei/presets/route-test/memories")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer memResp.Body.Close()
	if memResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("api/v1 memory fallback status = %d, want %d (grpc-gateway proxy failure)", memResp.StatusCode, http.StatusServiceUnavailable)
	}

	// The superseded session-scoped memory path has no registered rule
	// (specs/071-memory-preset-scope/contracts/memory-service.md §5), so
	// grpc-gateway answers with its own 404 JSON body.
	sessionMemResp, err := http.Get(httpSrv.URL + "/api/v1/templates/saolei/sessions/route-test/memories")
	if err != nil {
		t.Fatalf("get session-scoped memory: %v", err)
	}
	defer sessionMemResp.Body.Close()
	if sessionMemResp.StatusCode != http.StatusNotFound {
		t.Fatalf("api/v1 session-scoped memory status = %d, want %d (superseded route)", sessionMemResp.StatusCode, http.StatusNotFound)
	}

	// The v1 team:refresh rule no longer exists (the v1 team face is
	// removed), so grpc-gateway answers with its own 404 JSON body.
	teamResp, err := http.Post(httpSrv.URL+"/api/v1/templates/saolei/sessions/route-test/team:refresh", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post team:refresh: %v", err)
	}
	defer teamResp.Body.Close()
	if teamResp.StatusCode != http.StatusNotFound {
		t.Fatalf("api/v1 team:refresh status = %d, want %d (v1 face removed)", teamResp.StatusCode, http.StatusNotFound)
	}

	missing, err := http.Get(httpSrv.URL + "/api/v3/anything")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("api/v3 status = %d, want %d", missing.StatusCode, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// Tests: SendFrame body limit middleware
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §3.3)
// ---------------------------------------------------------------------------

const sendFramePath = "/api/v2/templates/saolei/sessions/s1:sendFrame"

// bodyProbeHandler reports the number of bytes it can read from the request
// body, or the read error. It stands in for grpc-gateway's request decoder
// (which reads the whole body before invoking the backend).
func bodyProbeHandler(w http.ResponseWriter, r *http.Request) {
	n, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fmt.Fprintf(w, "%d", n)
}

// zeroReader yields zeros without bound, standing in for an attacker-supplied
// chunked upload whose length is unknown to the server.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestSendFrameBodyLimit_ContentLengthOverLimitReturns413(t *testing.T) {
	// given: a declared length above the cap — the guard rejects before any read
	handler := sendFrameBodyLimit(http.HandlerFunc(bodyProbeHandler))
	req := httptest.NewRequest(http.MethodPost, sendFramePath, http.NoBody)
	req.ContentLength = maxSendFrameBodyBytes + 1
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if !strings.Contains(rec.Body.String(), "8MiB") {
		t.Fatalf("body = %q, want a message naming the 8MiB limit", rec.Body.String())
	}
}

func TestSendFrameBodyLimit_OverLimitReadReportsBodyTooLarge(t *testing.T) {
	// given: an unknown-length body (chunked upload) that runs past the cap —
	// the read fails with a clear error instead of truncating silently
	handler := sendFrameBodyLimit(http.HandlerFunc(bodyProbeHandler))
	req := httptest.NewRequest(http.MethodPost, sendFramePath, io.LimitReader(zeroReader{}, maxSendFrameBodyBytes+1024))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (the read error surfaces to the decoder)", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("body = %q, want the MaxBytesReader \"request body too large\" attribution", rec.Body.String())
	}
}

func TestSendFrameBodyLimit_WithinLimitPassesThroughUnmodified(t *testing.T) {
	// given: a 5MiB screenshot payload well under the 8MiB cap — the guard
	// must not interfere with normal uplinks
	handler := sendFrameBodyLimit(http.HandlerFunc(bodyProbeHandler))
	const bodyBytes = 5 * 1024 * 1024
	req := httptest.NewRequest(http.MethodPost, sendFramePath, bytes.NewReader(make([]byte, bodyBytes)))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != fmt.Sprint(bodyBytes) {
		t.Fatalf("read bytes = %s, want %d (no truncation)", got, bodyBytes)
	}
}

func TestSendFrameBodyLimit_OtherRoutesUnaffected(t *testing.T) {
	// given: an oversized declared length on routes that are not the
	// sendFrame POST — the guard must leave them untouched
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "stream GET", method: http.MethodGet, path: "/api/v2/templates/saolei/sessions/s1:stream"},
		{name: "team send POST", method: http.MethodPost, path: "/api/v2/templates/saolei/sessions/s1:send"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := sendFrameBodyLimit(http.HandlerFunc(bodyProbeHandler))
			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			req.ContentLength = maxSendFrameBodyBytes + 1
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (route not guarded)", rec.Code, http.StatusOK)
			}
		})
	}
}

func TestGatewayMuxOctetStreamAndJSONMarshalersCoexist(t *testing.T) {
	// given: the production mux (octet-stream registered) routing to an
	// unreachable backend — a well-formed request of either content type
	// reaches the backend and fails with 503 (rather than a decoding 500 or a
	// routing 404), proving per-MIME marshaler selection
	httpSrv := httptest.NewServer(newRoutingTestMux(t))
	defer httpSrv.Close()

	t.Run("application/json still routes", func(t *testing.T) {
		resp, err := http.Post(
			httpSrv.URL+sendFramePath,
			"application/json",
			strings.NewReader(`{"frame":{"sessionId":"s1"}}`),
		)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d (JSON marshaler unaffected by the octet-stream registration)", resp.StatusCode, http.StatusServiceUnavailable)
		}
	})

	t.Run("application/octet-stream decodes binary proto", func(t *testing.T) {
		payload, err := proto.Marshal(&game.SendFrameRequest{Frame: &game.UserFrame{SessionId: "s1"}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		resp, err := http.Post(httpSrv.URL+sendFramePath, "application/octet-stream", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d (binary proto decoded, then proxied to the unreachable backend)", resp.StatusCode, http.StatusServiceUnavailable)
		}
	})
}
