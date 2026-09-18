package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	game "dominion/projects/game"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// agentSide is the httptest double of the gateway's desktop-bridge endpoints.
// It serves the downlink NDJSON stream (the connection-confirmation status
// frame, then the scripted operation frames) and records every uplink receipt
// POST (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §1).
type agentSide struct {
	mu       sync.Mutex
	requests []*game.SendFrameRequest
	receipts []*game.FlowResultPart

	receiptCh chan *game.FlowResultPart
}

// serveStream writes the downlink frames and waits for each operation's
// receipt before dispatching the next one, then holds the stream open until
// the client goes away — the long-lived server stream the executor reads.
func (a *agentSide) serveStream(w http.ResponseWriter, r *http.Request, ops []*game.FlowPart) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	writeTeamEnvelope(w, &game.TeamFrame{Payload: &game.TeamFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{{
		Kind: &game.FlowPart_Status{Status: &game.StatusSignal{
			Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE,
		}},
	}}}}})
	flusher.Flush()

	for _, op := range ops {
		writeTeamEnvelope(w, &game.TeamFrame{Payload: &game.TeamFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{op}}}})
		flusher.Flush()
		select {
		case <-a.receiptCh:
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
			return
		}
	}
	<-r.Context().Done()
}

// serveSendFrame decodes one octet-stream SendFrameRequest and records it.
func (a *agentSide) serveSendFrame(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	request := new(game.SendFrameRequest)
	if err := proto.Unmarshal(body, request); err != nil {
		http.Error(w, "unmarshal request", http.StatusBadRequest)
		return
	}
	parts := request.GetFrame().GetFlowParts().GetParts()
	if len(parts) != 1 || parts[0].GetFlowResult() == nil {
		http.Error(w, "request carries no flow result", http.StatusBadRequest)
		return
	}
	receipt := parts[0].GetFlowResult()

	a.mu.Lock()
	a.requests = append(a.requests, request)
	a.receipts = append(a.receipts, receipt)
	a.mu.Unlock()

	a.receiptCh <- receipt
	w.WriteHeader(http.StatusOK)
}

// requestsOf/receiptsOf snapshot the recorded posts.
func (a *agentSide) requestsOf() []*game.SendFrameRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*game.SendFrameRequest(nil), a.requests...)
}

func (a *agentSide) receiptsOf() []*game.FlowResultPart {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*game.FlowResultPart(nil), a.receipts...)
}

// agentSideSessionPath is the session resource path the double asserts: the
// test cases serve Config{Template: "saolei", Session: "s1"}, so the desktop
// identity must travel the URL path exactly (contract §2.1 —
// specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md).
const (
	agentSideTemplate  = "saolei"
	agentSideSession   = "s1"
	agentSideStreamURL = "/api/v2/templates/saolei/sessions/s1:stream"
	agentSideSendURL   = "/api/v2/templates/saolei/sessions/s1:sendFrame"
)

// serveAgentSide starts the agent-side double for the scripted operations.
func serveAgentSide(t *testing.T, ops []*game.FlowPart) (*httptest.Server, *agentSide) {
	t.Helper()
	side := &agentSide{receiptCh: make(chan *game.FlowResultPart, len(ops)+1)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == agentSideStreamURL:
			side.serveStream(w, r, ops)
		case r.Method == http.MethodPost && r.URL.Path == agentSideSendURL:
			side.serveSendFrame(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, side
}

// writeTeamEnvelope writes one grpc-gateway server-stream envelope line.
func writeTeamEnvelope(w io.Writer, frame *game.TeamFrame) {
	payload, err := protojson.Marshal(frame)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "{\"result\":%s}\n", payload)
}

func TestServeOnceStreamsOperationsAndPostsReceipts(t *testing.T) {
	ops := []*game.FlowPart{
		keyPress("op-f2", game.KeyboardKey_KEYBOARD_KEY_F2),
		cellCenter("op-click", game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK, 3, 4),
	}
	srv, side := serveAgentSide(t, ops)

	scenario, err := loadScenario(scenarioProgressive)
	if err != nil {
		t.Fatalf("loadScenario: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	baseURL := srv.URL + "/api/v2/templates/" + agentSideTemplate + "/sessions/" + agentSideSession
	done := make(chan error, 1)
	go func() {
		done <- serveOnce(ctx, baseURL+":stream", baseURL+":sendFrame", Config{
			Template: agentSideTemplate,
			Session:  agentSideSession,
		}, scenario, slog.Default())
	}()

	// The double's first frame is the status connection confirmation; the
	// scripted exchange completing proves serveOnce consumed it and read on
	// to the operations. The handler holds the stream open after the last
	// receipt, so the poll below wins over an early exit.
	deadline := time.After(5 * time.Second)
receiptsLoop:
	for {
		if got := side.receiptsOf(); len(got) == 2 {
			break
		}
		select {
		case err := <-done:
			if len(side.receiptsOf()) >= 2 {
				break receiptsLoop
			}
			t.Fatalf("serveOnce exited before both receipts: %v (receipts=%d)", err, len(side.receiptsOf()))
		case <-deadline:
			t.Fatalf("timed out waiting for receipts, got %d", len(side.receiptsOf()))
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	receipts := side.receiptsOf()
	if receipts[0].GetMessage() != "F2 pressed, new game started" {
		t.Errorf("receipt[0] message = %q", receipts[0].GetMessage())
	}
	assertScreenshot(t, receipts[0], mustFixture(t, "saolei_1.png"))
	if receipts[1].GetMessage() != "click(3,4) executed" {
		t.Errorf("receipt[1] message = %q", receipts[1].GetMessage())
	}
	assertScreenshot(t, receipts[1], mustFixture(t, "saolei_3.png"))
	if receipts[1].GetToolId() != "op-click" {
		t.Errorf("receipt[1] tool_id = %q, want op-click", receipts[1].GetToolId())
	}

	// The uplink body is the SendFrameRequest proto carrying the receipt
	// frame and nothing else (identity travels the URL path).
	requests := side.requestsOf()
	if len(requests) != 2 {
		t.Fatalf("recorded posts = %d, want 2", len(requests))
	}
	part := requests[1].GetFrame().GetFlowParts().GetParts()[0]
	if part.GetFlowResult() == nil || part.GetFlowResult().GetToolId() != "op-click" {
		t.Errorf("posted frame part = %+v, want the op-click FlowResult", part)
	}
}

func TestServeOnceDisconnectInjection(t *testing.T) {
	ops := []*game.FlowPart{
		cellCenter("op-1", game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK, 0, 0),
	}
	srv, side := serveAgentSide(t, ops)

	scenario, err := loadScenario(scenarioWon)
	if err != nil {
		t.Fatalf("loadScenario: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	baseURL := srv.URL + "/api/v2/templates/" + agentSideTemplate + "/sessions/" + agentSideSession
	done := make(chan error, 1)
	go func() {
		done <- serveOnce(ctx, baseURL+":stream", baseURL+":sendFrame", Config{
			Template: agentSideTemplate,
			Session:  agentSideSession,
			Fault:    Fault{DisconnectAfterOps: 1},
		}, scenario, slog.Default())
	}()

	// The injection tears the stream down after the single receipt, so
	// serveOnce returns (the fault sentinel error) on its own.
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("serveOnce returned nil after the disconnect injection, want the fault error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the injected disconnect")
	}
	if got := side.receiptsOf(); len(got) != 1 {
		t.Fatalf("receipts = %d, want 1", len(got))
	}
}

func TestDecodeTeamFrame(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantStatus game.StatusSignalStatus
		wantErr    string
	}{
		{
			name:       "status result frame",
			line:       `{"result":{"flowParts":{"parts":[{"status":{"status":"STATUS_SIGNAL_STATUS_ACTIVE"}}]}}}`,
			wantStatus: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE,
		},
		{
			name:    "stream termination tail",
			line:    `{"error":{"code":13,"message":"stream terminated"}}`,
			wantErr: "stream terminated",
		},
		{
			name:    "missing result envelope",
			line:    `{}`,
			wantErr: "missing result frame",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := decodeTeamFrame([]byte(tt.line))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("decodeTeamFrame(%s) error = %v, want %q", tt.line, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeTeamFrame(%s): %v", tt.line, err)
			}
			signal := frame.GetFlowParts().GetParts()[0].GetStatus()
			if signal.GetStatus() != tt.wantStatus {
				t.Errorf("status = %v, want %v", signal.GetStatus(), tt.wantStatus)
			}
		})
	}
}

// mustFixture reads one embedded fixture, failing the test on error.
func mustFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := fixture(name)
	if err != nil {
		t.Fatalf("fixture(%s): %v", name, err)
	}
	return data
}
