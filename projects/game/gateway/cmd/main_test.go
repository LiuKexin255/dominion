package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pgrpc "dominion/common/gopkg/grpc"
	game "dominion/projects/game"

	"github.com/coder/websocket"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// ---------------------------------------------------------------------------
// Unit tests: path helpers
// ---------------------------------------------------------------------------

func TestIsWebSocketConnectPathV2(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/api/v2/templates/saolei/sessions/abc/connect", want: true},
		{path: "api/v2/templates/saolei/sessions/abc/connect", want: true},
		{path: "/api/v2/templates/saolei/sessions/abc/connect/", want: true},
		{path: "/api/v2/templates//sessions/abc/connect", want: false},
		{path: "/api/v2/templates/saolei/sessions//connect", want: false},
		{path: "/api/v2/templates/saolei/sessions/abc", want: false},
		{path: "/api/v2/templates/saolei/sessions/abc/agent", want: false},
		{path: "/api/v2/models", want: false},
		{path: "/api/v1/templates/saolei/sessions/abc/connect", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isWebSocketConnectPathV2(tt.path)
			if got != tt.want {
				t.Fatalf("isWebSocketConnectPathV2(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestExtractConnectIdentityV2(t *testing.T) {
	tests := []struct {
		path         string
		wantTemplate string
		wantSession  string
	}{
		{path: "/api/v2/templates/saolei/sessions/abc123/connect", wantTemplate: "saolei", wantSession: "abc123"},
		{path: "/api/v2/templates/saolei/sessions/x-y-z/connect", wantTemplate: "saolei", wantSession: "x-y-z"},
		{path: "/api/v2/templates/saolei/sessions//connect", wantTemplate: "", wantSession: ""},
		{path: "/api/v2/templates/saolei/sessions/abc/agent", wantTemplate: "", wantSession: ""},
		{path: "/api/v1/templates/saolei/sessions/abc/connect", wantTemplate: "", wantSession: ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			gotTemplate, gotSession := extractConnectIdentityV2(tt.path)
			if gotTemplate != tt.wantTemplate {
				t.Fatalf("extractConnectIdentityV2(%q) template = %q, want %q", tt.path, gotTemplate, tt.wantTemplate)
			}
			if gotSession != tt.wantSession {
				t.Fatalf("extractConnectIdentityV2(%q) session = %q, want %q", tt.path, gotSession, tt.wantSession)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Unit tests: isCleanClose
// ---------------------------------------------------------------------------

func TestIsCleanClose(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "io.EOF", err: io.EOF, want: true},
		{name: "context.Canceled", err: context.Canceled, want: true},
		{name: "nil error", err: nil, want: false},
		{name: "random error", err: errors.New("something"), want: false},
		{name: "websocket normal close", err: websocket.CloseError{Code: websocket.StatusNormalClosure, Reason: ""}, want: true},
		{name: "websocket going away", err: websocket.CloseError{Code: websocket.StatusGoingAway, Reason: "bye"}, want: true},
		{name: "websocket internal error", err: websocket.CloseError{Code: websocket.StatusInternalError, Reason: "oops"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCleanClose(tt.err)
			if got != tt.want {
				t.Fatalf("isCleanClose(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: proto.Unmarshal forward-compat with unknown fields
// ---------------------------------------------------------------------------

func TestProtoUnmarshalForwardCompat(t *testing.T) {
	// Verify that proto.Unmarshal tolerates unknown fields without error —
	// the forward-compatibility mechanism of the WebSocket adapter's Recv
	// (wsStream). Unknown fields are preserved per the proto spec, not
	// discarded. The inbound frame type is UserFrame (the WebSocket
	// adapter's Recv unmarshal target).
	want := &game.UserFrame{
		SessionId: "s1",
		Payload: &game.UserFrame_FlowParts{
			FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
				{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
			}},
		},
	}

	data, err := proto.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Append an unknown field (field 999, length-delimited wire type 2).
	// Tag varint for (999<<3)|2 = 7994: {0xBA, 0x3E}; length 6; payload "future".
	data = append(data, 0xBA, 0x3E, 0x06)
	data = append(data, []byte("future")...)

	frame := new(game.UserFrame)
	if err := proto.Unmarshal(data, frame); err != nil {
		t.Fatalf("Unmarshal with unknown field: %v", err)
	}

	if frame.GetSessionId() != "s1" {
		t.Fatalf("session_id = %q, want %q", frame.GetSessionId(), "s1")
	}
	fp := frame.GetFlowParts()
	if fp == nil {
		t.Fatal("payload oneof = nil, want flowParts")
	}
	if len(fp.GetParts()) != 1 {
		t.Fatalf("flowParts parts = %d, want 1", len(fp.GetParts()))
	}
	sf := fp.GetParts()[0].GetStatus()
	if sf == nil {
		t.Fatal("flowParts[0] kind = nil, want status")
	}
	if sf.GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE {
		t.Fatalf("status = %q, want %q", sf.GetStatus(), game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE)
	}

	// Note: we do NOT use proto.Equal here because proto.Unmarshal preserves
	// unknown fields (the appended 999:"future"), so the frame would differ
	// from a clean marshal. This is the correct forward-compat behavior —
	// the individual field assertions above confirm the known fields parse
	// correctly despite the unknown field.
}

// ---------------------------------------------------------------------------
// Integration helpers: real WebSocket + gRPC backend
// ---------------------------------------------------------------------------

// setupTestGRPCBridge starts a gRPC server with the given DesktopBridgeService
// mock and returns the client connection.
func setupTestGRPCBridge(t *testing.T, mock game.DesktopBridgeServiceServer) (*grpc.ClientConn, context.CancelFunc) {
	t.Helper()
	return startTestGRPC(t, func(srv *grpc.Server) {
		game.RegisterDesktopBridgeServiceServer(srv, mock)
	})
}

// startTestGRPC starts a gRPC server whose services the register callback
// installs, and returns the client connection. Resources are released on
// test cleanup.
func startTestGRPC(t *testing.T, register func(srv *grpc.Server)) (*grpc.ClientConn, context.CancelFunc) {
	t.Helper()

	srv := grpc.NewServer()
	register(srv)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go srv.Serve(lis)

	_, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		srv.Stop()
		lis.Close()
		cancel()
		t.Fatalf("dial: %v", err)
	}

	t.Cleanup(func() {
		conn.Close()
		srv.Stop()
		lis.Close()
		cancel()
	})

	return conn, cancel
}

// wsURL converts an http:// URL to ws:// for WebSocket dialing.
func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

// echoAsTeamFrame converts an inbound UserFrame into the outbound TeamFrame
// echo, preserving the routing pair and payload. Recv returns UserFrame while
// Send takes TeamFrame (specs/035-proto-contract-refine/contracts/
// frame-split.md §1), so a server-side echo must translate the types.
func echoAsTeamFrame(f *game.UserFrame) *game.TeamFrame {
	out := &game.TeamFrame{
		SessionId:  f.GetSessionId(),
		TemplateId: f.GetTemplateId(),
	}
	if mp := f.GetMessageParts(); mp != nil {
		out.Payload = &game.TeamFrame_MessageParts{MessageParts: mp}
	} else if fp := f.GetFlowParts(); fp != nil {
		out.Payload = &game.TeamFrame_FlowParts{FlowParts: fp}
	}
	return out
}

// ---------------------------------------------------------------------------
// Tests: root mux routing (/api/v2 agent surface, /api/v1 regression)
// ---------------------------------------------------------------------------

// newRoutingTestMux builds the root mux the way main() assembles it: the
// proxy connection (teamConn) carrying the v2 AgentService handler, the
// direct agent_v2 connection (presetConn) carrying the PresetService handler,
// and the session/memory handlers on their own connections — all pointed at
// unreachable backends. Routing assertions can then distinguish "reached
// grpc-gateway and proxied" (503, backend unavailable) from "no route"
// (404); the backend connection choice does not affect the route-presence
// assertions.
func newRoutingTestMux(t *testing.T) *http.ServeMux {
	t.Helper()

	gwmux := runtime.NewServeMux(pgrpc.GatewayDefault()...)
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
	if err := game.RegisterPresetServiceHandler(context.Background(), gwmux, presetConn); err != nil {
		t.Fatalf("register preset handler: %v", err)
	}

	return newRootMux(gwmux, proxyConn)
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
					strings.NewReader(`{"playerPrompt":"hi"}`),
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

// TestRootMuxAPIv1SessionMemoryFallback verifies the /api/v1/ subtree still
// routes to grpc-gateway for the services that remain on it: session and
// memory requests reach their handler routes and fail proxying to the
// unreachable backend with 503 (grpc code Unavailable), while the removed
// v1 TeamService face answers a routing 404 (FR-019 — the team/prompt faces
// are gone; memory/session stay, specs/051-agent-v2-dsh-migration/
// spec.md FR-019).
func TestRootMuxAPIv1SessionMemoryFallback(t *testing.T) {
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

	// ListMemories rule (game.proto: get /api/v1/{parent=templates/*/sessions/*}/memories).
	memResp, err := http.Get(httpSrv.URL + "/api/v1/templates/saolei/sessions/route-test/memories")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer memResp.Body.Close()
	if memResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("api/v1 memory fallback status = %d, want %d (grpc-gateway proxy failure)", memResp.StatusCode, http.StatusServiceUnavailable)
	}

	// RefreshTeam rule — the v1 TeamService face is removed, so the route no
	// longer exists and grpc-gateway answers with its own 404 JSON body.
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
// Tests: /api/v2 desktop-bridge WebSocket surface
// (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §3)
// ---------------------------------------------------------------------------

// mockDesktopBridgeServer implements game.DesktopBridgeServiceServer for
// testing.
type mockDesktopBridgeServer struct {
	game.UnimplementedDesktopBridgeServiceServer

	// onConnect receives the bidi stream (Recv returns UserFrame, Send takes
	// TeamFrame) and returns when done or on error.
	onConnect func(stream game.DesktopBridgeService_ConnectServer) error
}

func (m *mockDesktopBridgeServer) Connect(stream game.DesktopBridgeService_ConnectServer) error {
	if m.onConnect != nil {
		return m.onConnect(stream)
	}
	// Default: echo each received frame back as a TeamFrame (Recv returns
	// UserFrame, Send takes TeamFrame).
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		if err := stream.Send(echoAsTeamFrame(frame)); err != nil {
			return err
		}
	}
}

// TestHandleDesktopBridgeConnect_ProbeRoundtrip drives the v2 connect face
// end to end: a WS dial to /api/v2/.../connect reaches a real
// DesktopBridgeService gRPC server, the URL-derived identity overwrites the
// client-supplied frame identity, and the probe echoes back as a TeamFrame.
func TestHandleDesktopBridgeConnect_ProbeRoundtrip(t *testing.T) {
	bridgeConn, _ := setupTestGRPCBridge(t, &mockDesktopBridgeServer{})

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleDesktopBridgeConnect(w, r, bridgeConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v2/templates/saolei/sessions/probe-session/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// The probe frame carries a deliberately wrong session id — the gateway
	// must overwrite it with the URL path value
	// (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §1).
	sendFrame := &game.UserFrame{
		TemplateId: "wrong",
		SessionId:  "from-proto",
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE}}},
		}}},
	}
	msg, err := proto.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := conn.Write(ctx, websocket.MessageBinary, msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, resp, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	recvFrame := new(game.TeamFrame)
	if err := proto.Unmarshal(resp, recvFrame); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if recvFrame.GetTemplateId() != "saolei" {
		t.Fatalf("template_id = %q, want %q (from URL, not protobuf)", recvFrame.GetTemplateId(), "saolei")
	}
	if recvFrame.GetSessionId() != "probe-session" {
		t.Fatalf("session_id = %q, want %q (from URL, not protobuf)", recvFrame.GetSessionId(), "probe-session")
	}
	status := recvFrame.GetFlowParts().GetParts()[0].GetStatus()
	if status == nil {
		t.Fatal("response flowParts[0] kind = nil, want status")
	}
	if status.GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE {
		t.Fatalf("status = %q, want %q", status.GetStatus(), game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
	}
}

// TestHandleDesktopBridgeConnect_MissingSessionID verifies the routing
// guard: a connect path with an empty session segment is answered with HTTP
// 400 before any upgrade.
func TestHandleDesktopBridgeConnect_MissingSessionID(t *testing.T) {
	bridgeConn, _ := setupTestGRPCBridge(t, &mockDesktopBridgeServer{})

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleDesktopBridgeConnect(w, r, bridgeConn)
	}))
	defer httpSrv.Close()

	resp, err := http.Get(httpSrv.URL + "/api/v2/templates/saolei/sessions//connect")
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestRootMuxAPIv2WebSocketConnectRoutedToDesktopBridge verifies the
// /api/v2/ subtree diverts the connect path to the WebSocket pump before
// grpc-gateway: a WS dial through the full root mux reaches the desktop
// bridge backend and round-trips a probe (desktop-bridge.md §3: the
// /api/v2/ subtree checks the WS branch first, then falls through to
// gwmux).
func TestRootMuxAPIv2WebSocketConnectRoutedToDesktopBridge(t *testing.T) {
	bridgeConn, _ := setupTestGRPCBridge(t, &mockDesktopBridgeServer{})

	// The mux mirrors main(): one proxy connection carries the grpc-gateway
	// AgentService handler and the WebSocket pump. The gateway handler
	// registered on this conn is never exercised by the WS path under test.
	gwmux := runtime.NewServeMux(pgrpc.GatewayDefault()...)
	if err := game.RegisterAgentServiceHandler(context.Background(), gwmux, bridgeConn); err != nil {
		t.Fatalf("register agent handler: %v", err)
	}

	httpSrv := httptest.NewServer(newRootMux(gwmux, bridgeConn))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v2/templates/saolei/sessions/routed/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	sendFrame := probeUserFrame("saolei", "from-proto")
	msg, err := proto.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := conn.Write(ctx, websocket.MessageBinary, msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, resp, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	recvFrame := new(game.TeamFrame)
	if err := proto.Unmarshal(resp, recvFrame); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if recvFrame.GetSessionId() != "routed" {
		t.Fatalf("session_id = %q, want %q (identity injected from the routed URL)", recvFrame.GetSessionId(), "routed")
	}
}

// probeUserFrame builds the StatusSignal probe UserFrame with a
// client-supplied (overwritable) identity.
func probeUserFrame(templateID, sessionID string) *game.UserFrame {
	return &game.UserFrame{
		TemplateId: templateID,
		SessionId:  sessionID,
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE}}},
		}}},
	}
}
