package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dominion/projects/game"

	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// statusFrame builds a TeamFrame carrying one status FlowPart.
func statusFrame(status game.StatusSignalStatus) *game.TeamFrame {
	return &game.TeamFrame{
		SessionId:  "test-session",
		TemplateId: "saolei",
		Payload: &game.TeamFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: status}}},
		}}},
	}
}

// ndjsonLine encodes frame as one downlink envelope line.
func ndjsonLine(t *testing.T, frame *game.TeamFrame) []byte {
	t.Helper()
	data, err := protojson.Marshal(frame)
	if err != nil {
		t.Errorf("protojson.Marshal(frame) error: %v", err)
		return nil
	}
	return append(append([]byte(`{"result":`), data...), '}', '\n')
}

// writeAndFlush writes data and flushes so a streaming client observes it
// without waiting for the handler to return.
func writeAndFlush(t *testing.T, w http.ResponseWriter, data []byte) {
	t.Helper()
	if _, err := w.Write(data); err != nil {
		return
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// TestBridgeClient_Connect_StreamURLAndEnvHeader verifies Connect dials the
// :stream endpoint with the env header and without an Accept header.
func TestBridgeClient_Connect_StreamURLAndEnvHeader(t *testing.T) {
	// given: a server that captures the stream request
	var gotPath, gotEnv, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotEnv = r.Header.Get("env")
		gotAccept = r.Header.Get("Accept")
		writeAndFlush(t, w, ndjsonLine(t, statusFrame(game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)))
		<-r.Context().Done()
	}))
	defer srv.Close()

	// when: connecting
	client := NewBridgeClient()
	if err := client.Connect(context.Background(), srv.URL, "saolei", "sess-123", "test-env"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer client.Close()

	// then: path, env header, no Accept
	wantPath := "/api/v2/templates/saolei/sessions/sess-123:stream"
	if gotPath != wantPath {
		t.Errorf("stream path = %q, want %q", gotPath, wantPath)
	}
	if gotEnv != "test-env" {
		t.Errorf("env header = %q, want %q", gotEnv, "test-env")
	}
	if gotAccept != "" {
		t.Errorf("Accept header = %q, want empty", gotAccept)
	}
}

// TestBridgeClient_Connect_StreamOpenError verifies a non-2xx :stream response
// is decoded as a JSON status envelope (contract §1.1) and surfaced as an
// error, leaving the client unconnected.
func TestBridgeClient_Connect_StreamOpenError(t *testing.T) {
	// given: a server responding 404 with a JSON status envelope
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"code":5,"message":"session not found"}`))
	}))
	defer srv.Close()

	// when: connecting
	client := NewBridgeClient()
	err := client.Connect(context.Background(), srv.URL, "saolei", "sess-123", "test-env")

	// then: the envelope message is surfaced and no reader is stored
	if err == nil {
		t.Fatal("Connect() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("error = %q, want containing %q", err.Error(), "session not found")
	}
	if _, recvErr := client.RecvFrame(context.Background()); recvErr == nil {
		t.Error("RecvFrame() expected 'not connected' error, got nil")
	}
}

// TestBridgeClient_WaitFirstStatus verifies the first-frame confirmation: an
// immediate status frame, a delayed status frame, a first frame without a
// status FlowPart, and a timeout each behave as the contract requires
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.2/§5.1).
func TestBridgeClient_WaitFirstStatus(t *testing.T) {
	tests := []struct {
		name       string
		timeout    time.Duration
		writeFirst func(t *testing.T, w http.ResponseWriter)
		wantStatus string
		wantErr    bool
	}{
		{
			name:    "immediate status frame",
			timeout: 2 * time.Second,
			writeFirst: func(t *testing.T, w http.ResponseWriter) {
				writeAndFlush(t, w, ndjsonLine(t, statusFrame(game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)))
			},
			wantStatus: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE.String(),
		},
		{
			name:    "delayed status frame",
			timeout: 2 * time.Second,
			writeFirst: func(t *testing.T, w http.ResponseWriter) {
				time.Sleep(20 * time.Millisecond)
				writeAndFlush(t, w, ndjsonLine(t, statusFrame(game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE)))
			},
			wantStatus: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE.String(),
		},
		{
			name:    "first frame without a status part",
			timeout: 2 * time.Second,
			writeFirst: func(t *testing.T, w http.ResponseWriter) {
				writeAndFlush(t, w, ndjsonLine(t, &game.TeamFrame{
					SessionId:  "test-session",
					TemplateId: "saolei",
					Payload: &game.TeamFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
						{Kind: &game.FlowPart_Wait{Wait: &game.WaitSignal{}}},
					}}},
				}))
			},
			wantStatus: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_UNSPECIFIED.String(),
		},
		{
			name:    "no frame within timeout",
			timeout: 50 * time.Millisecond,
			writeFirst: func(t *testing.T, w http.ResponseWriter) {
				// Send the response head but never a line; keep the stream
				// open until Close cancels it.
				w.WriteHeader(http.StatusOK)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			},
			wantErr: true,
		},
		{
			name:    "error tail before any status",
			timeout: 2 * time.Second,
			writeFirst: func(t *testing.T, w http.ResponseWriter) {
				writeAndFlush(t, w, []byte(`{"error":{"code":9,"message":"no stream"}}`+"\n"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: a streaming server that writes the configured first frame
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.writeFirst(t, w)
				<-r.Context().Done()
			}))
			defer srv.Close()

			client := NewBridgeClient()
			if err := client.Connect(context.Background(), srv.URL, "saolei", "sess-123", "test-env"); err != nil {
				t.Fatalf("Connect() unexpected error: %v", err)
			}
			defer client.Close()

			// when: waiting for the first status frame
			status, err := client.WaitFirstStatus(context.Background(), tt.timeout)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("WaitFirstStatus() expected error, got status %q", status)
				}
				return
			}
			if err != nil {
				t.Fatalf("WaitFirstStatus() unexpected error: %v", err)
			}
			if status != tt.wantStatus {
				t.Errorf("status = %q, want %q", status, tt.wantStatus)
			}
		})
	}
}

// TestBridgeClient_RecvFrame_LineFraming verifies frames are delimited by
// newline, not by transport chunk: several lines in one write are returned one
// at a time, and a line split across writes is reassembled
// (specs/066-desktop-bridge-split/research.md §3).
func TestBridgeClient_RecvFrame_LineFraming(t *testing.T) {
	t.Run("single chunk with multiple lines", func(t *testing.T) {
		// given: two frames written in one chunk
		first := statusFrame(game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
		second := statusFrame(game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeAndFlush(t, w, append(ndjsonLine(t, first), ndjsonLine(t, second)...))
			<-r.Context().Done()
		}))
		defer srv.Close()

		client := NewBridgeClient()
		if err := client.Connect(context.Background(), srv.URL, "saolei", "sess-123", "test-env"); err != nil {
			t.Fatalf("Connect() unexpected error: %v", err)
		}
		defer client.Close()

		// when/then: both frames are returned separately
		assertStatusFrame(t, client, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
		assertStatusFrame(t, client, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE)
	})

	t.Run("single line split across chunks", func(t *testing.T) {
		// given: one frame line written in two chunks
		line := ndjsonLine(t, statusFrame(game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE))
		split := len(line) / 2
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeAndFlush(t, w, line[:split])
			time.Sleep(20 * time.Millisecond)
			writeAndFlush(t, w, line[split:])
			<-r.Context().Done()
		}))
		defer srv.Close()

		client := NewBridgeClient()
		if err := client.Connect(context.Background(), srv.URL, "saolei", "sess-123", "test-env"); err != nil {
			t.Fatalf("Connect() unexpected error: %v", err)
		}
		defer client.Close()

		// when/then: the reassembled frame is returned
		assertStatusFrame(t, client, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
	})
}

// assertStatusFrame receives one frame and asserts it carries the given status.
func assertStatusFrame(t *testing.T, client *BridgeClient, want game.StatusSignalStatus) {
	t.Helper()
	frame, err := client.RecvFrame(context.Background())
	if err != nil {
		t.Fatalf("RecvFrame() unexpected error: %v", err)
	}
	got := frame.GetFlowParts().GetParts()[0].GetStatus().GetStatus()
	if got != want {
		t.Errorf("status = %v, want %v", got, want)
	}
}

// TestBridgeClient_RecvFrame_ErrorTail verifies an {"error": ...} tail line
// terminates the stream with an error carrying the server message.
func TestBridgeClient_RecvFrame_ErrorTail(t *testing.T) {
	// given: a data line followed by an error tail line
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAndFlush(t, w, ndjsonLine(t, statusFrame(game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)))
		writeAndFlush(t, w, []byte(`{"error":{"code":13,"message":"agent restarted"}}`+"\n"))
	}))
	defer srv.Close()

	client := NewBridgeClient()
	if err := client.Connect(context.Background(), srv.URL, "saolei", "sess-123", "test-env"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer client.Close()

	// when: reading the data line then the error tail
	assertStatusFrame(t, client, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE)
	_, err := client.RecvFrame(context.Background())

	// then: the error carries the server message
	if err == nil {
		t.Fatal("RecvFrame() expected stream error, got nil")
	}
	if !strings.Contains(err.Error(), "agent restarted") {
		t.Errorf("error = %q, want containing %q", err.Error(), "agent restarted")
	}
}

// TestBridgeClient_RecvFrame_DiscardUnknown verifies an unknown TeamFrame
// field does not fail decoding (FR-007 forward compatibility).
func TestBridgeClient_RecvFrame_DiscardUnknown(t *testing.T) {
	// given: a result line with an unknown field alongside a known status
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAndFlush(t, w, []byte(`{"result":{"sessionId":"s","futureField":{"nested":true},"flowParts":{"parts":[{"status":{"status":"STATUS_SIGNAL_STATUS_IDLE"}}]}}}`+"\n"))
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := NewBridgeClient()
	if err := client.Connect(context.Background(), srv.URL, "saolei", "sess-123", "test-env"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer client.Close()

	// when/then: the known field survives, the unknown field is discarded
	assertStatusFrame(t, client, game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE)
}

// TestBridgeClient_SendFrame_RequestBody verifies the uplink request: POST to
// the :sendFrame endpoint with an octet-stream body equal to the marshalled
// SendFrameRequest and no Accept header.
func TestBridgeClient_SendFrame_RequestBody(t *testing.T) {
	// given: a server that records the sendFrame request
	var gotPath, gotContentType, gotAccept string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ":stream") {
			w.WriteHeader(http.StatusOK)
			return
		}
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewBridgeClient()
	if err := client.Connect(context.Background(), srv.URL, "saolei", "sess-123", "test-env"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer client.Close()

	frame := &game.UserFrame{
		SessionId: "sess-123",
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_FlowResult{FlowResult: &game.FlowResultPart{ToolId: "tool-1", Status: game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED}}},
		}}},
	}

	// when: sending the frame
	if err := client.SendFrame(context.Background(), frame); err != nil {
		t.Fatalf("SendFrame() unexpected error: %v", err)
	}

	// then: path, content type, no Accept, and exact proto body
	if wantPath := "/api/v2/templates/saolei/sessions/sess-123:sendFrame"; gotPath != wantPath {
		t.Errorf("sendFrame path = %q, want %q", gotPath, wantPath)
	}
	if gotContentType != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/octet-stream")
	}
	if gotAccept != "" {
		t.Errorf("Accept header = %q, want empty", gotAccept)
	}
	wantBody, err := proto.Marshal(&game.SendFrameRequest{Frame: frame})
	if err != nil {
		t.Fatalf("proto.Marshal(SendFrameRequest) error: %v", err)
	}
	if !bytes.Equal(gotBody, wantBody) {
		t.Errorf("sendFrame body = %d bytes, want exactly %d bytes (no encoding inflation)", len(gotBody), len(wantBody))
	}
}

// TestBridgeClient_SendFrame_NearLimitBody verifies a screenshot near the 5 MiB
// application limit travels byte-for-byte (no base64/JSON inflation).
func TestBridgeClient_SendFrame_NearLimitBody(t *testing.T) {
	// given: a server that records the sendFrame body
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ":stream") {
			w.WriteHeader(http.StatusOK)
			return
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewBridgeClient()
	if err := client.Connect(context.Background(), srv.URL, "saolei", "sess-123", "test-env"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer client.Close()

	screenshot := make([]byte, 5*1024*1024)
	for i := range screenshot {
		screenshot[i] = byte(i % 251)
	}
	frame := &game.UserFrame{
		SessionId: "sess-123",
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_FlowResult{FlowResult: &game.FlowResultPart{
				ToolId:     "tool-large",
				Status:     game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
				Screenshot: &game.ImagePart{Encoding: game.ImageEncoding_IMAGE_ENCODING_PNG, Data: screenshot},
			}}},
		}}},
	}

	// when: sending the frame
	if err := client.SendFrame(context.Background(), frame); err != nil {
		t.Fatalf("SendFrame() unexpected error: %v", err)
	}

	// then: the received body equals the marshalled proto payload exactly
	wantBody, err := proto.Marshal(&game.SendFrameRequest{Frame: frame})
	if err != nil {
		t.Fatalf("proto.Marshal(SendFrameRequest) error: %v", err)
	}
	if !bytes.Equal(gotBody, wantBody) {
		t.Errorf("sendFrame body = %d bytes, want exactly %d bytes", len(gotBody), len(wantBody))
	}
}

// TestBridgeClient_SendFrame_ErrorDispatch verifies non-2xx sendFrame responses
// are decoded by Content-Type (contract §1.1): octet-stream bodies decode to a
// gRPC code, other media types carry the body text verbatim.
func TestBridgeClient_SendFrame_ErrorDispatch(t *testing.T) {
	binaryStatus := func(t *testing.T, code codes.Code, message string) []byte {
		t.Helper()
		data, err := proto.Marshal(&statuspb.Status{Code: int32(code), Message: message})
		if err != nil {
			t.Fatalf("proto.Marshal(status.Status) error: %v", err)
		}
		return data
	}

	tests := []struct {
		name        string
		statusCode  int
		contentType string
		body        func(t *testing.T) []byte
		wantCode    codes.Code
		wantMessage string
	}{
		{
			name:        "413 plain text guard",
			statusCode:  http.StatusRequestEntityTooLarge,
			contentType: "text/plain; charset=utf-8",
			body:        func(t *testing.T) []byte { return []byte("request body too large\n") },
			wantCode:    codes.OK,
			wantMessage: "request body too large",
		},
		{
			name:        "400 binary FAILED_PRECONDITION",
			statusCode:  http.StatusBadRequest,
			contentType: "application/octet-stream",
			body: func(t *testing.T) []byte {
				return binaryStatus(t, codes.FailedPrecondition, "desktop stream not active")
			},
			wantCode:    codes.FailedPrecondition,
			wantMessage: "desktop stream not active",
		},
		{
			name:        "404 binary NOT_FOUND",
			statusCode:  http.StatusNotFound,
			contentType: "application/octet-stream",
			body: func(t *testing.T) []byte {
				return binaryStatus(t, codes.NotFound, "no owner for session")
			},
			wantCode:    codes.NotFound,
			wantMessage: "no owner for session",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: a server answering sendFrame with the configured error
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ":stream") {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.statusCode)
				w.Write(tt.body(t))
			}))
			defer srv.Close()

			client := NewBridgeClient()
			if err := client.Connect(context.Background(), srv.URL, "saolei", "sess-123", "test-env"); err != nil {
				t.Fatalf("Connect() unexpected error: %v", err)
			}
			defer client.Close()

			// when: sending a frame
			err := client.SendFrame(context.Background(), &game.UserFrame{SessionId: "sess-123"})

			// then: the structured error carries status, gRPC code, and message
			if err == nil {
				t.Fatal("SendFrame() expected error, got nil")
			}
			var bridgeErr *BridgeError
			if !errors.As(err, &bridgeErr) {
				t.Fatalf("error type = %T, want *BridgeError", err)
			}
			if bridgeErr.StatusCode != tt.statusCode {
				t.Errorf("StatusCode = %d, want %d", bridgeErr.StatusCode, tt.statusCode)
			}
			if bridgeErr.GRPCCode != tt.wantCode {
				t.Errorf("GRPCCode = %v, want %v", bridgeErr.GRPCCode, tt.wantCode)
			}
			if !strings.Contains(bridgeErr.Message, tt.wantMessage) {
				t.Errorf("Message = %q, want containing %q", bridgeErr.Message, tt.wantMessage)
			}
		})
	}
}

// TestBridgeClient_SendFrame_NotConnected verifies SendFrame fails cleanly when
// Connect was never called.
func TestBridgeClient_SendFrame_NotConnected(t *testing.T) {
	// given: a fresh client
	client := NewBridgeClient()

	// when/then: sending returns a not-connected error
	err := client.SendFrame(context.Background(), &game.UserFrame{SessionId: "s"})
	if err == nil {
		t.Fatal("SendFrame() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("error = %q, want containing %q", err.Error(), "not connected")
	}
}

// TestBridgeClient_Close_UnblocksRecv verifies Close cancels the stream and
// unblocks a RecvFrame blocked on a line read.
func TestBridgeClient_Close_UnblocksRecv(t *testing.T) {
	// given: a server that flushes the response head but never writes a frame
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := NewBridgeClient()
	if err := client.Connect(context.Background(), srv.URL, "saolei", "sess-123", "test-env"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}

	recvErr := make(chan error, 1)
	go func() {
		_, err := client.RecvFrame(context.Background())
		recvErr <- err
	}()
	time.Sleep(50 * time.Millisecond)

	// when: closing
	if err := client.Close(); err != nil {
		t.Fatalf("Close() unexpected error: %v", err)
	}

	// then: the blocked read unblocks with an error
	select {
	case err := <-recvErr:
		if err == nil {
			t.Error("RecvFrame() returned nil error after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RecvFrame did not unblock within 2s after Close")
	}
}
