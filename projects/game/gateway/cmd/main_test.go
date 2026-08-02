package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	game "dominion/projects/game"

	"github.com/coder/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// ---------------------------------------------------------------------------
// Unit tests: path helpers
// ---------------------------------------------------------------------------

func TestIsWebSocketConnectPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/api/v1/templates/saolei/sessions/abc/connect", want: true},
		{path: "api/v1/templates/saolei/sessions/abc/connect", want: true},
		{path: "/api/v1/templates/saolei/sessions/abc/connect/", want: true},
		{path: "/api/v1/templates//sessions/abc/connect", want: false},
		{path: "/api/v1/templates/saolei/sessions//connect", want: false},
		{path: "/api/v1/templates/saolei/sessions/abc", want: false},
		{path: "/api/v1/templates/saolei/sessions/connect", want: false},
		{path: "/api/v1/templates/saolei/sessions/abc/foo/bar", want: false},
		{path: "/api/v1/agents", want: false},
		// Top-level sessions paths no longer exist (FR-002 clean break).
		{path: "/api/v1/sessions/abc/connect", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isWebSocketConnectPath(tt.path)
			if got != tt.want {
				t.Fatalf("isWebSocketConnectPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestExtractConnectIdentity(t *testing.T) {
	tests := []struct {
		path         string
		wantTemplate string
		wantSession  string
	}{
		{path: "/api/v1/templates/saolei/sessions/abc123/connect", wantTemplate: "saolei", wantSession: "abc123"},
		{path: "/api/v1/templates/saolei/sessions/x-y-z/connect", wantTemplate: "saolei", wantSession: "x-y-z"},
		{path: "/api/v1/templates/saolei/sessions//connect", wantTemplate: "", wantSession: ""},
		{path: "/api/v1/agents", wantTemplate: "", wantSession: ""},
		{path: "/api/v1/sessions/abc/connect", wantTemplate: "", wantSession: ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			gotTemplate, gotSession := extractConnectIdentity(tt.path)
			if gotTemplate != tt.wantTemplate {
				t.Fatalf("extractConnectIdentity(%q) template = %q, want %q", tt.path, gotTemplate, tt.wantTemplate)
			}
			if gotSession != tt.wantSession {
				t.Fatalf("extractConnectIdentity(%q) session = %q, want %q", tt.path, gotSession, tt.wantSession)
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
// Integration tests: handleWebSocketConnect with real WebSocket + gRPC
// ---------------------------------------------------------------------------

// mockTeamServer implements game.TeamServiceServer for testing.
type mockTeamServer struct {
	game.UnimplementedTeamServiceServer

	// onConnect is called when Connect is invoked. It receives the
	// gRPC stream and can read/write AgentFrames. It should return when
	// done or on error.
	onConnect func(stream game.TeamService_ConnectServer) error
}

func (m *mockTeamServer) Connect(stream game.TeamService_ConnectServer) error {
	if m.onConnect != nil {
		return m.onConnect(stream)
	}
	// Default: echo received frames back.
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		if err := stream.Send(frame); err != nil {
			return err
		}
	}
}

// setupTestGRPC starts a gRPC server with the given mock and returns the
// client connection. Caller must call conn.Close() and cancel() when done.
func setupTestGRPC(t *testing.T, mock game.TeamServiceServer) (*grpc.ClientConn, context.CancelFunc) {
	t.Helper()

	srv := grpc.NewServer()
	game.RegisterTeamServiceServer(srv, mock)

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

// ---------------------------------------------------------------------------
// Test: invalid protobuf causes close, no echo
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_InvalidProtobufClosesConnection(t *testing.T) {
	mock := &mockTeamServer{
		onConnect: func(stream game.TeamService_ConnectServer) error {
			// The server should not receive any frames since the client
			// sends invalid protobuf.
			_, err := stream.Recv()
			return err
		},
	}

	teamConn, _ := setupTestGRPC(t, mock)

	// Create a test HTTP server that routes to handleWebSocketConnect.
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, teamConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/templates/saolei/sessions/test-session/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send invalid protobuf (0xFF bytes are an invalid wire format).
	err = conn.Write(ctx, websocket.MessageBinary, []byte{0xFF, 0xFF, 0xFF, 0xFF})
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read response — should get a close frame (error).
	_, _, readErr := conn.Read(ctx)
	if readErr == nil {
		t.Fatal("expected error reading after sending invalid protobuf, got nil")
	}

	closeCode := websocket.CloseStatus(readErr)
	if closeCode != websocket.StatusInvalidFramePayloadData {
		t.Fatalf("expected close status StatusInvalidFramePayloadData (%d), got %d (err: %v)", websocket.StatusInvalidFramePayloadData, closeCode, readErr)
	}
}

// ---------------------------------------------------------------------------
// Test: valid protobuf with unknown fields is accepted (forward compat)
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_ForwardCompatUnknownFields(t *testing.T) {
	received := make(chan *game.AgentFrame, 1)

	mock := &mockTeamServer{
		onConnect: func(stream game.TeamService_ConnectServer) error {
			frame, err := stream.Recv()
			if err != nil {
				return err
			}
			received <- frame
			return nil
		},
	}

	teamConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, teamConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/templates/saolei/sessions/test-session/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Build a valid AgentFrame (status is a FlowPart kind), marshal as binary
	// protobuf, then append an unknown field (field 999, length-delimited)
	// to verify proto.Unmarshal tolerates unknown fields — the forward-
	// compatibility mechanism that replaced protojson's DiscardUnknown
	// (specs/025-desktop-image-state-refine/contracts/image-transport-contract.md §2).
	sendFrame := &game.AgentFrame{
		Payload: &game.AgentFrame_FlowParts{
			FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
				{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
			}},
		},
	}
	data, err := proto.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Tag for field 999, wire type 2: varint((999<<3)|2) = varint(7994) = {0xBA, 0x3E}
	// Length 6, then payload "future".
	data = append(data, 0xBA, 0x3E, 0x06)
	data = append(data, []byte("future")...)

	err = conn.Write(ctx, websocket.MessageBinary, data)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Wait for the gRPC server to receive the frame.
	select {
	case f := <-received:
		if f.GetTemplateId() != "saolei" {
			t.Fatalf("template_id = %q, want %q", f.GetTemplateId(), "saolei")
		}
		if f.GetSessionId() != "test-session" {
			t.Fatalf("session_id = %q, want %q", f.GetSessionId(), "test-session")
		}
		fp := f.GetFlowParts()
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
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for frame on gRPC server")
	}
}

// ---------------------------------------------------------------------------
// Test: bidirectional frame forwarding
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_BidirectionalForward(t *testing.T) {
	mock := &mockTeamServer{
		onConnect: func(stream game.TeamService_ConnectServer) error {
			// Echo each received frame back.
			for {
				frame, err := stream.Recv()
				if err != nil {
					return err
				}
				if err := stream.Send(frame); err != nil {
					return err
				}
			}
		},
	}

	teamConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, teamConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/templates/saolei/sessions/echo-session/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a message_parts frame (MessageParts with a single TextPart) —
	// the display payload unit. The server echoes it back unmodified
	// (specs/023-saolei-mcp-refine/contracts/content-model-contract.md §3/§4).
	sendFrame := &game.AgentFrame{
		SessionId: "echo-session",
		Payload: &game.AgentFrame_MessageParts{
			MessageParts: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: "hello"}}},
				},
			},
		},
	}
	msg, err := proto.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	err = conn.Write(ctx, websocket.MessageBinary, msg)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read the echoed frame.
	_, resp, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	recvFrame := new(game.AgentFrame)
	if err := proto.Unmarshal(resp, recvFrame); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if recvFrame.GetTemplateId() != "saolei" {
		t.Fatalf("template_id = %q, want %q", recvFrame.GetTemplateId(), "saolei")
	}
	if recvFrame.GetSessionId() != "echo-session" {
		t.Fatalf("session_id = %q, want %q", recvFrame.GetSessionId(), "echo-session")
	}
	mp := recvFrame.GetMessageParts()
	if mp == nil {
		t.Fatal("payload oneof = nil, want messageParts")
	}
	if len(mp.GetParts()) != 1 {
		t.Fatalf("parts = %d, want 1", len(mp.GetParts()))
	}
	textPart := mp.GetParts()[0].GetText()
	if textPart == nil {
		t.Fatal("part[0].kind = nil, want text")
	}
	if textPart.GetContent() != "hello" {
		t.Fatalf("text content = %q, want %q", textPart.GetContent(), "hello")
	}
}

// ---------------------------------------------------------------------------
// Test: missing session ID returns HTTP 400
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_MissingSessionID(t *testing.T) {
	teamConn, _ := setupTestGRPC(t, &mockTeamServer{})

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, teamConn)
	}))
	defer httpSrv.Close()

	// Use path without a valid session ID.
	resp, err := http.Get(httpSrv.URL + "/api/v1/templates/saolei/sessions//connect")
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// Test: gRPC server error closes WebSocket cleanly
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_GRPCStreamError(t *testing.T) {
	mock := &mockTeamServer{
		onConnect: func(stream game.TeamService_ConnectServer) error {
			return status.Error(codes.Internal, "test internal error")
		},
	}

	teamConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, teamConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/templates/saolei/sessions/err-session/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a valid frame — the gRPC server will error on Recv or the
	// stream will error immediately. status is a FlowPart kind (spec 023).
	sendFrame := &game.AgentFrame{
		SessionId: "err-session",
		Payload: &game.AgentFrame_FlowParts{
			FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
				{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
			}},
		},
	}
	msg, err := proto.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	err = conn.Write(ctx, websocket.MessageBinary, msg)
	if err != nil {
		// Connection may already be closed by the server.
		return
	}

	// Try to read — expect error or close.
	_, _, readErr := conn.Read(ctx)
	if readErr == nil {
		t.Fatal("expected error from closed connection, got nil")
	}
}

// ---------------------------------------------------------------------------
// Test: session ID is overwritten from URL path
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_SessionIDFromPath(t *testing.T) {
	received := make(chan *game.AgentFrame, 1)

	mock := &mockTeamServer{
		onConnect: func(stream game.TeamService_ConnectServer) error {
			frame, err := stream.Recv()
			if err != nil {
				return err
			}
			received <- frame
			return nil
		},
	}

	teamConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, teamConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect with session "from-url" in the URL path.
	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/templates/saolei/sessions/from-url/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a frame with a DIFFERENT session_id in the protobuf — gateway
	// should overwrite it with the URL session_id. status is a FlowPart kind.
	sendFrame := &game.AgentFrame{
		SessionId: "from-proto",
		Payload: &game.AgentFrame_FlowParts{
			FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
				{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
			}},
		},
	}
	msg, err := proto.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	err = conn.Write(ctx, websocket.MessageBinary, msg)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case f := <-received:
		if f.GetTemplateId() != "saolei" {
			t.Fatalf("template_id = %q, want %q (should be from URL, not protobuf)", f.GetTemplateId(), "saolei")
		}
		if f.GetSessionId() != "from-url" {
			t.Fatalf("session_id = %q, want %q (should be from URL, not protobuf)", f.GetSessionId(), "from-url")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for frame on gRPC server")
	}
}

// ---------------------------------------------------------------------------
// Test: proto.Unmarshal forward-compat with unknown fields directly
// ---------------------------------------------------------------------------

func TestProtoUnmarshalForwardCompat(t *testing.T) {
	// Verify that proto.Unmarshal tolerates unknown fields without error —
	// the forward-compatibility mechanism that replaced protojson's
	// DiscardUnknown (specs/025-desktop-image-state-refine/contracts/
	// image-transport-contract.md §2). Unknown fields are preserved per
	// the proto spec, not discarded.
	want := &game.AgentFrame{
		SessionId: "s1",
		Payload: &game.AgentFrame_FlowParts{
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

	frame := new(game.AgentFrame)
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
// Test: client disconnect does not leak goroutines
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_ClientDisconnectNoLeak(t *testing.T) {
	// The gRPC server blocks on Recv until the test is done.
	serverRecv := make(chan struct{})
	serverDone := make(chan struct{})

	mock := &mockTeamServer{
		onConnect: func(stream game.TeamService_ConnectServer) error {
			defer close(serverDone)
			// Block until the test signals or the stream errors.
			_, err := stream.Recv()
			close(serverRecv)
			return err
		},
	}

	teamConn, _ := setupTestGRPC(t, mock)

	// Use a WaitGroup to detect when handleWebSocketConnect returns.
	var handlerDone sync.WaitGroup
	handlerDone.Add(1)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, teamConn)
		handlerDone.Done()
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/templates/saolei/sessions/leak-test/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}

	// Close the client WebSocket immediately — handler must detect via
	// request context cancellation or Read error and return.
	conn.Close(websocket.StatusNormalClosure, "bye")

	// Wait for the handler goroutine to finish.
	doneCh := make(chan struct{})
	go func() {
		handlerDone.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// Handler exited — no goroutine leak.
	case <-time.After(5 * time.Second):
		t.Fatal("handleWebSocketConnect did not return within 5s after client disconnect — goroutine leak")
	}

	// Also verify the gRPC server's Recv unblocked.
	select {
	case <-serverRecv:
	case <-time.After(3 * time.Second):
		t.Fatal("gRPC server Recv did not unblock after client disconnect")
	}
}

// ---------------------------------------------------------------------------
// Test: content frame with image roundtrips and server can reply with a status
// ---------------------------------------------------------------------------

func TestContentFrameWithImageRoundtrip(t *testing.T) {
	received := make(chan *game.AgentFrame, 1)
	statusSent := make(chan struct{})

	mock := &mockTeamServer{
		onConnect: func(stream game.TeamService_ConnectServer) error {
			frame, err := stream.Recv()
			if err != nil {
				return err
			}
			received <- frame
			// Reply with a StatusSignal FlowPart — the control-signal
			// payload used for connectivity / lifecycle confirmation.
			// status is a FlowPart kind (spec 023 C3 / FR-003).
			if err := stream.Send(&game.AgentFrame{
				SessionId: frame.GetSessionId(),
				Payload: &game.AgentFrame_FlowParts{
					FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
						{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
					}},
				},
			}); err != nil {
				return err
			}
			close(statusSent)
			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	teamConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, teamConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/templates/saolei/sessions/shot-session/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	pngData := make([]byte, 256)
	for i := range pngData {
		pngData[i] = byte(i)
	}

	// Send a message_parts frame carrying [TextPart, ImagePart] — the
	// display payload for a multimodal user turn
	// (specs/023-saolei-mcp-refine/contracts/content-model-contract.md §3).
	sendFrame := &game.AgentFrame{
		SessionId: "shot-session",
		FrameId:   "frame-1",
		Sender:    game.FrameSender_FRAME_SENDER_USER,
		Payload: &game.AgentFrame_MessageParts{
			MessageParts: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: "look"}}},
					{Kind: &game.MessagePart_Image{Image: &game.ImagePart{
						Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
						Data:        pngData,
						WidthPx:     800,
						HeightPx:    600,
						ScaleFactor: 1.5,
						WindowTitle: "Test Window",
					}}},
				},
			},
		},
	}
	msg, err := proto.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	err = conn.Write(ctx, websocket.MessageBinary, msg)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case f := <-received:
		if f.GetTemplateId() != "saolei" {
			t.Fatalf("template_id = %q, want %q", f.GetTemplateId(), "saolei")
		}
		if f.GetSessionId() != "shot-session" {
			t.Fatalf("session_id = %q, want %q", f.GetSessionId(), "shot-session")
		}
		mp := f.GetMessageParts()
		if mp == nil {
			t.Fatal("payload oneof = nil, want messageParts")
		}
		parts := mp.GetParts()
		if len(parts) != 2 {
			t.Fatalf("parts = %d, want 2", len(parts))
		}
		if parts[0].GetText().GetContent() != "look" {
			t.Fatalf("part[0].text.content = %q, want %q", parts[0].GetText().GetContent(), "look")
		}
		img := parts[1].GetImage()
		if img == nil {
			t.Fatal("part[1].image = nil")
		}
		if img.GetEncoding() != game.ImageEncoding_IMAGE_ENCODING_PNG {
			t.Fatalf("encoding = %v, want PNG", img.GetEncoding())
		}
		if string(img.GetData()) != string(pngData) {
			t.Fatalf("image data mismatch: got %d bytes, want %d bytes", len(img.GetData()), len(pngData))
		}
		if img.GetWidthPx() != 800 || img.GetHeightPx() != 600 {
			t.Fatalf("dimensions = %dx%d, want 800x600", img.GetWidthPx(), img.GetHeightPx())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for message_parts frame on gRPC server")
	}

	_, resp, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read status: %v", err)
	}

	recvFrame := new(game.AgentFrame)
	if err := proto.Unmarshal(resp, recvFrame); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}

	// status is a FlowPart kind carried inside flow_parts (spec 023).
	status := recvFrame.GetFlowParts().GetParts()[0].GetStatus()
	if status == nil {
		t.Fatal("response flowParts[0] kind = nil, want status")
	}
	if status.GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE {
		t.Fatalf("status = %q, want %q", status.GetStatus(), game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE)
	}
}

// ---------------------------------------------------------------------------
// Test: ReadLimit is configured (10MB for PNG screenshot support)
// ---------------------------------------------------------------------------

func TestReadLimitSet(t *testing.T) {
	// Send a frame slightly above the default 32KB limit to verify ReadLimit
	// has been raised. The default ReadLimit is 32768 bytes; we send 64KB.
	// If ReadLimit were not set, the read would be rejected.

	mock := &mockTeamServer{
		onConnect: func(stream game.TeamService_ConnectServer) error {
			frame, err := stream.Recv()
			if err != nil {
				return err
			}
			if err := stream.Send(frame); err != nil {
				return err
			}
			// Keep stream alive so the WS client can read the echoed frame.
			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	teamConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, teamConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/templates/saolei/sessions/limit-test/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Raise client-side ReadLimit too so we can read the large echoed response.
	conn.SetReadLimit(10 << 20)

	// Build a message_parts frame with a 64KB ImagePart — exceeds default
	// 32KB limit.
	largeData := make([]byte, 64*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	sendFrame := &game.AgentFrame{
		SessionId: "limit-test",
		Sender:    game.FrameSender_FRAME_SENDER_USER,
		Payload: &game.AgentFrame_MessageParts{
			MessageParts: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_Image{Image: &game.ImagePart{
						Encoding: game.ImageEncoding_IMAGE_ENCODING_PNG,
						Data:     largeData,
						WidthPx:  100,
						HeightPx: 100,
					}}},
				},
			},
		},
	}
	msg, err := proto.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	err = conn.Write(ctx, websocket.MessageBinary, msg)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read the echoed response — if ReadLimit were 32KB, this would fail.
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("read echoed frame (64KB payload): %v — ReadLimit may not be set to 10MB", err)
	}
}
