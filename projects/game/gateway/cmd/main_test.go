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
	"google.golang.org/protobuf/encoding/protojson"
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
		{path: "/api/v1/sessions/abc/agent/connect", want: true},
		{path: "api/v1/sessions/abc/agent/connect", want: true},
		{path: "/api/v1/sessions/abc/agent/connect/", want: true},
		{path: "/api/v1/sessions//agent/connect", want: false},
		{path: "/api/v1/sessions/abc/agent", want: false},
		{path: "/api/v1/sessions/agent/connect", want: false},
		{path: "/api/v1/sessions/abc/foo/bar", want: false},
		{path: "/api/v1/agents", want: false},
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

func TestExtractSessionID(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/api/v1/sessions/abc123/agent/connect", want: "abc123"},
		{path: "/api/v1/sessions/x-y-z/agent/connect", want: "x-y-z"},
		{path: "/api/v1/sessions//agent/connect", want: ""},
		{path: "/api/v1/agents", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractSessionID(tt.path)
			if got != tt.want {
				t.Fatalf("extractSessionID(%q) = %q, want %q", tt.path, got, tt.want)
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

// mockProxyServer implements game.ProxyServiceServer for testing.
type mockProxyServer struct {
	game.UnimplementedProxyServiceServer

	// onConnect is called when ConnectAgent is invoked. It receives the
	// gRPC stream and can read/write AgentFrames. It should return when
	// done or on error.
	onConnect func(stream game.ProxyService_ConnectAgentServer) error
}

func (m *mockProxyServer) ConnectAgent(stream game.ProxyService_ConnectAgentServer) error {
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
func setupTestGRPC(t *testing.T, mock game.ProxyServiceServer) (*grpc.ClientConn, context.CancelFunc) {
	t.Helper()

	srv := grpc.NewServer()
	game.RegisterProxyServiceServer(srv, mock)

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
// Test: invalid JSON causes close, no echo
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_InvalidJSONClosesConnection(t *testing.T) {
	mock := &mockProxyServer{
		onConnect: func(stream game.ProxyService_ConnectAgentServer) error {
			// The server should not receive any frames since the client
			// sends invalid JSON.
			_, err := stream.Recv()
			return err
		},
	}

	proxyConn, _ := setupTestGRPC(t, mock)

	// Create a test HTTP server that routes to handleWebSocketConnect.
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, proxyConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/sessions/test-session/agent/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send invalid JSON.
	err = conn.Write(ctx, websocket.MessageText, []byte("not valid json at all"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read response — should get a close frame (error).
	_, _, readErr := conn.Read(ctx)
	if readErr == nil {
		t.Fatal("expected error reading after sending invalid JSON, got nil")
	}

	closeCode := websocket.CloseStatus(readErr)
	if closeCode != websocket.StatusInvalidFramePayloadData {
		t.Fatalf("expected close status StatusInvalidFramePayloadData (%d), got %d (err: %v)", websocket.StatusInvalidFramePayloadData, closeCode, readErr)
	}
}

// ---------------------------------------------------------------------------
// Test: valid JSON with unknown fields is accepted (DiscardUnknown)
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_DiscardUnknownFields(t *testing.T) {
	received := make(chan *game.AgentFrame, 1)

	mock := &mockProxyServer{
		onConnect: func(stream game.ProxyService_ConnectAgentServer) error {
			frame, err := stream.Recv()
			if err != nil {
				return err
			}
			received <- frame
			return nil
		},
	}

	proxyConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, proxyConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/sessions/test-session/agent/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send valid AgentFrame JSON with an extra unknown field.
	frameJSON := `{"sessionId":"","status":{"status":"ready"},"unknown_field":"should be discarded"}`
	err = conn.Write(ctx, websocket.MessageText, []byte(frameJSON))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Wait for the gRPC server to receive the frame.
	select {
	case f := <-received:
		if f.GetSessionId() != "test-session" {
			t.Fatalf("session_id = %q, want %q", f.GetSessionId(), "test-session")
		}
		sf := f.GetStatus()
		if sf == nil {
			t.Fatal("payload oneof = nil, want status")
		}
		if sf.GetStatus() != "ready" {
			t.Fatalf("status = %q, want %q", sf.GetStatus(), "ready")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for frame on gRPC server")
	}
}

// ---------------------------------------------------------------------------
// Test: bidirectional echo forwarding
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_BidirectionalEcho(t *testing.T) {
	mock := &mockProxyServer{
		onConnect: func(stream game.ProxyService_ConnectAgentServer) error {
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

	proxyConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, proxyConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/sessions/echo-session/agent/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a valid AgentFrame.
	sendFrame := &game.AgentFrame{
		SessionId: "echo-session",
		Payload: &game.AgentFrame_Echo{
			Echo: &game.AgentEchoFrame{Data: []byte("hello")},
		},
	}
	msg, err := protojson.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	err = conn.Write(ctx, websocket.MessageText, msg)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read the echoed frame.
	_, resp, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	recvFrame := new(game.AgentFrame)
	if err := protojson.Unmarshal(resp, recvFrame); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if recvFrame.GetSessionId() != "echo-session" {
		t.Fatalf("session_id = %q, want %q", recvFrame.GetSessionId(), "echo-session")
	}
	echoPayload := recvFrame.GetEcho()
	if echoPayload == nil {
		t.Fatal("payload oneof = nil, want echo")
	}
	if string(echoPayload.GetData()) != "hello" {
		t.Fatalf("echo data = %q, want %q", string(echoPayload.GetData()), "hello")
	}
}

// ---------------------------------------------------------------------------
// Test: missing session ID returns HTTP 400
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_MissingSessionID(t *testing.T) {
	proxyConn, _ := setupTestGRPC(t, &mockProxyServer{})

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, proxyConn)
	}))
	defer httpSrv.Close()

	// Use path without a valid session ID.
	resp, err := http.Get(httpSrv.URL + "/api/v1/sessions//agent/connect")
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
	mock := &mockProxyServer{
		onConnect: func(stream game.ProxyService_ConnectAgentServer) error {
			return status.Error(codes.Internal, "test internal error")
		},
	}

	proxyConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, proxyConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/sessions/err-session/agent/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a valid frame — the gRPC server will error on Recv or the
	// stream will error immediately.
	sendFrame := &game.AgentFrame{
		SessionId: "err-session",
		Payload: &game.AgentFrame_Status{
			Status: &game.AgentStatusFrame{Status: "ping"},
		},
	}
	msg, err := protojson.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	err = conn.Write(ctx, websocket.MessageText, msg)
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

	mock := &mockProxyServer{
		onConnect: func(stream game.ProxyService_ConnectAgentServer) error {
			frame, err := stream.Recv()
			if err != nil {
				return err
			}
			received <- frame
			return nil
		},
	}

	proxyConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, proxyConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect with session "from-url" in the URL path.
	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/sessions/from-url/agent/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a frame with a DIFFERENT session_id in JSON — gateway should
	// overwrite it with the URL session_id.
	frameJSON := `{"sessionId":"from-json","status":{"status":"ready"}}`
	err = conn.Write(ctx, websocket.MessageText, []byte(frameJSON))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case f := <-received:
		if f.GetSessionId() != "from-url" {
			t.Fatalf("session_id = %q, want %q (should be from URL, not JSON)", f.GetSessionId(), "from-url")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for frame on gRPC server")
	}
}

// ---------------------------------------------------------------------------
// Test: protojson DiscardUnknown directly
// ---------------------------------------------------------------------------

func TestProtojsonDiscardUnknown(t *testing.T) {
	// Verify that protojson.UnmarshalOptions{DiscardUnknown: true} actually
	// discards unknown fields without error.
	input := []byte(`{"sessionId":"s1","status":{"status":"running"},"future_field":"ignored"}`)

	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	frame := new(game.AgentFrame)
	if err := opts.Unmarshal(input, frame); err != nil {
		t.Fatalf("Unmarshal with DiscardUnknown: %v", err)
	}

	if frame.GetSessionId() != "s1" {
		t.Fatalf("session_id = %q, want %q", frame.GetSessionId(), "s1")
	}
	sf := frame.GetStatus()
	if sf == nil {
		t.Fatal("payload oneof = nil, want status")
	}
	if sf.GetStatus() != "running" {
		t.Fatalf("status = %q, want %q", sf.GetStatus(), "running")
	}

	// Verify the proto is valid.
	want := &game.AgentFrame{
		SessionId: "s1",
		Payload: &game.AgentFrame_Status{
			Status: &game.AgentStatusFrame{Status: "running"},
		},
	}
	if !proto.Equal(frame, want) {
		t.Fatalf("frame = %v, want %v", frame, want)
	}
}

// ---------------------------------------------------------------------------
// Test: client disconnect does not leak goroutines
// ---------------------------------------------------------------------------

func TestHandleWebSocketConnect_ClientDisconnectNoLeak(t *testing.T) {
	// The gRPC server blocks on Recv until the test is done.
	serverRecv := make(chan struct{})
	serverDone := make(chan struct{})

	mock := &mockProxyServer{
		onConnect: func(stream game.ProxyService_ConnectAgentServer) error {
			defer close(serverDone)
			// Block until the test signals or the stream errors.
			_, err := stream.Recv()
			close(serverRecv)
			return err
		},
	}

	proxyConn, _ := setupTestGRPC(t, mock)

	// Use a WaitGroup to detect when handleWebSocketConnect returns.
	var handlerDone sync.WaitGroup
	handlerDone.Add(1)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, proxyConn)
		handlerDone.Done()
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/sessions/leak-test/agent/connect", nil)
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
// Test: screenshot frame roundtrip
// ---------------------------------------------------------------------------

func TestScreenshotFrameRoundtrip(t *testing.T) {
	received := make(chan *game.AgentFrame, 1)
	ackSent := make(chan struct{})

	mock := &mockProxyServer{
		onConnect: func(stream game.ProxyService_ConnectAgentServer) error {
			frame, err := stream.Recv()
			if err != nil {
				return err
			}
			received <- frame
			if err := stream.Send(&game.AgentFrame{
				SessionId: frame.GetSessionId(),
				Payload: &game.AgentFrame_Ack{
					Ack: &game.AgentAckFrame{AckFrameId: frame.GetFrameId()},
				},
			}); err != nil {
				return err
			}
			close(ackSent)
			// Block until the test is done so the gRPC stream stays open
			// for the gateway to forward the ack to the WS client.
			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	proxyConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, proxyConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/sessions/shot-session/agent/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	pngData := make([]byte, 256)
	for i := range pngData {
		pngData[i] = byte(i)
	}

	sendFrame := &game.AgentFrame{
		SessionId: "shot-session",
		FrameId:   "frame-1",
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId:   "cap-1",
				Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:        pngData,
				WidthPx:     800,
				HeightPx:    600,
				ScaleFactor: 1.5,
				WindowTitle: "Test Window",
			},
		},
	}
	msg, err := protojson.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	err = conn.Write(ctx, websocket.MessageText, msg)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case f := <-received:
		if f.GetSessionId() != "shot-session" {
			t.Fatalf("session_id = %q, want %q", f.GetSessionId(), "shot-session")
		}
		sf := f.GetScreenshot()
		if sf == nil {
			t.Fatal("payload oneof = nil, want screenshot")
		}
		if sf.GetEncoding() != game.ImageEncoding_IMAGE_ENCODING_PNG {
			t.Fatalf("encoding = %v, want PNG", sf.GetEncoding())
		}
		if string(sf.GetData()) != string(pngData) {
			t.Fatalf("screenshot data mismatch: got %d bytes, want %d bytes", len(sf.GetData()), len(pngData))
		}
		if sf.GetWidthPx() != 800 || sf.GetHeightPx() != 600 {
			t.Fatalf("dimensions = %dx%d, want 800x600", sf.GetWidthPx(), sf.GetHeightPx())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for screenshot frame on gRPC server")
	}

	_, resp, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}

	recvFrame := new(game.AgentFrame)
	if err := protojson.Unmarshal(resp, recvFrame); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}

	ack := recvFrame.GetAck()
	if ack == nil {
		t.Fatal("response payload oneof = nil, want ack")
	}
	if ack.GetAckFrameId() != "frame-1" {
		t.Fatalf("ack_frame_id = %q, want %q", ack.GetAckFrameId(), "frame-1")
	}
}

// ---------------------------------------------------------------------------
// Test: ReadLimit is configured (10MB for PNG screenshot support)
// ---------------------------------------------------------------------------

func TestReadLimitSet(t *testing.T) {
	// Send a frame slightly above the default 32KB limit to verify ReadLimit
	// has been raised. The default ReadLimit is 32768 bytes; we send 64KB.
	// If ReadLimit were not set, the read would be rejected.

	mock := &mockProxyServer{
		onConnect: func(stream game.ProxyService_ConnectAgentServer) error {
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

	proxyConn, _ := setupTestGRPC(t, mock)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocketConnect(w, r, proxyConn)
	}))
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv.URL)+"/api/v1/sessions/limit-test/agent/connect", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Raise client-side ReadLimit too so we can read the large echoed response.
	conn.SetReadLimit(10 << 20)

	// Build a frame with 64KB of screenshot data — exceeds default 32KB limit.
	largeData := make([]byte, 64*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	sendFrame := &game.AgentFrame{
		SessionId: "limit-test",
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				Encoding: game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:     largeData,
				WidthPx:  100,
				HeightPx: 100,
			},
		},
	}
	msg, err := protojson.Marshal(sendFrame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	err = conn.Write(ctx, websocket.MessageText, msg)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read the echoed response — if ReadLimit were 32KB, this would fail.
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("read echoed frame (64KB payload): %v — ReadLimit may not be set to 10MB", err)
	}
}
