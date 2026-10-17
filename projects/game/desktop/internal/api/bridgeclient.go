package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"dominion/projects/game"
	"dominion/projects/game/desktop/internal/trace"

	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// envHeader is the request header carrying the deployment environment. It is
// forwarded verbatim through gwmux to the target services
// (specs/066-desktop-bridge-split/research.md §1 — env header 透传).
const envHeader = "env"

// BridgeClient is the desktop-bridge v2 HTTP client: the session downlink
// stream (chunked NDJSON over GET) plus the uplink single-shot frame
// submission (octet-stream over POST). Endpoint and wire format:
// specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §1; client
// behavior: specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §5.
type BridgeClient struct {
	mu   sync.Mutex
	http *http.Client
	env  string

	sendURL string
	reader  *bufio.Reader
	body    io.ReadCloser
	cancel  context.CancelFunc
}

// NewBridgeClient creates a BridgeClient whose HTTP transport propagates W3C
// trace context (the same transport the REST Client uses).
func NewBridgeClient() *BridgeClient {
	return &BridgeClient{http: &http.Client{Transport: trace.NewHTTPTransport()}}
}

// Connect opens the session's downlink stream and stores its body reader and
// request cancel function. The URL is
// {gatewayURL}/api/v2/templates/{template}/sessions/{sessionID}:stream; env,
// when non-empty, is sent as the env header. No Accept header is set: a GET
// with no request body leaves grpc-gateway's outbound marshaler on the wildcard
// JSON codec, so a pre-stream rejection is a JSON status envelope
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §1.1).
//
// Canceling ctx (or calling Close) aborts the stream and unblocks an in-flight
// RecvFrame.
func (b *BridgeClient) Connect(ctx context.Context, gatewayURL, template, sessionID, env string) error {
	baseURL := fmt.Sprintf("%s/api/v2/templates/%s/sessions/%s",
		strings.TrimSuffix(gatewayURL, "/"), url.PathEscape(template), url.PathEscape(sessionID))

	reqCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, baseURL+":stream", nil)
	if err != nil {
		cancel()
		return fmt.Errorf("connect: %w", err)
	}
	if env != "" {
		req.Header.Set(envHeader, env)
	}

	resp, err := b.http.Do(req)
	if err != nil {
		cancel()
		return fmt.Errorf("connect: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer cancel()
		return fmt.Errorf("connect: %w", readStreamOpenError(resp))
	}

	b.mu.Lock()
	b.env = env
	b.sendURL = baseURL + ":sendFrame"
	b.reader = bufio.NewReader(resp.Body)
	b.body = resp.Body
	b.cancel = cancel
	b.mu.Unlock()
	return nil
}

// WaitFirstStatus reads the first downlink line within timeout and returns the
// status FlowPart's enum name (e.g. "STATUS_SIGNAL_STATUS_ACTIVE") — the
// connection confirmation
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.2/§5.1). A
// timeout or read/parse failure closes the stream and returns an error. A first
// frame carrying no status FlowPart yields the UNSPECIFIED enum name, preserving
// the frontend display semantics
// (specs/021-agent-session-resync/contracts/agent-desktop-channel-contract.md §1).
func (b *BridgeClient) WaitFirstStatus(ctx context.Context, timeout time.Duration) (string, error) {
	type result struct {
		frame *game.TeamFrame
		err   error
	}
	resultCh := make(chan result, 1)
	go func() {
		frame, err := b.RecvFrame(ctx)
		resultCh <- result{frame: frame, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case res := <-resultCh:
		if res.err != nil {
			b.Close()
			return "", fmt.Errorf("wait first status: %w", res.err)
		}
		return statusName(res.frame), nil
	case <-timer.C:
		b.Close()
		<-resultCh
		return "", fmt.Errorf("wait first status: no status frame within %s", timeout)
	case <-ctx.Done():
		b.Close()
		<-resultCh
		return "", fmt.Errorf("wait first status: %w", ctx.Err())
	}
}

// RecvFrame reads one downlink line and returns the TeamFrame it carries. Lines
// are read with a newline delimiter, so a message split across transport chunks
// is reassembled and several messages coalesced into one chunk are returned one
// at a time (specs/066-desktop-bridge-split/research.md §3). A {"result": ...}
// line is decoded with DiscardUnknown for forward compatibility (FR-007); an
// {"error": ...} tail line ends the stream with an error.
func (b *BridgeClient) RecvFrame(ctx context.Context) (*game.TeamFrame, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("receive frame: %w", err)
	}

	b.mu.Lock()
	reader := b.reader
	b.mu.Unlock()
	if reader == nil {
		return nil, fmt.Errorf("receive frame: not connected")
	}

	for {
		line, readErr := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			if readErr != nil {
				return nil, fmt.Errorf("receive frame: %w", readErr)
			}
			continue
		}
		return parseFrameLine(trimmed)
	}
}

// SendFrame submits one uplink frame as an independent request to
// {gatewayURL}/api/v2/templates/{template}/sessions/{sessionID}:sendFrame with
// an application/octet-stream body. No Accept header is set, so grpc-gateway's
// outbound marshaler stays on the request's codec and every error body is a
// binary google.rpc.Status; the error is decoded by response Content-Type
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §1.1/§5.3).
func (b *BridgeClient) SendFrame(ctx context.Context, frame *game.UserFrame) error {
	b.mu.Lock()
	sendURL := b.sendURL
	env := b.env
	b.mu.Unlock()

	if sendURL == "" {
		return fmt.Errorf("send frame: not connected")
	}

	payload, err := proto.Marshal(&game.SendFrameRequest{Frame: frame})
	if err != nil {
		return fmt.Errorf("send frame: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("send frame: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if env != "" {
		req.Header.Set(envHeader, env)
	}

	resp, err := b.http.Do(req)
	if err != nil {
		return fmt.Errorf("send frame: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("send frame: %w", decodeSendFrameError(resp))
	}
	return nil
}

// Close cancels the stream's request context and closes the response body,
// unblocking an in-flight RecvFrame line read. It also clears the stored
// sendFrame URL and env so a subsequent SendFrame fails fast instead of posting
// to the torn-down session. It is safe to call multiple times or when not
// connected.
func (b *BridgeClient) Close() error {
	b.mu.Lock()
	cancel := b.cancel
	body := b.body
	b.reader = nil
	b.body = nil
	b.cancel = nil
	b.sendURL = ""
	b.env = ""
	b.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if body != nil {
		if err := body.Close(); err != nil {
			return fmt.Errorf("close: %w", err)
		}
	}
	return nil
}

// BridgeError describes a failed bridge request. StatusCode is the HTTP
// response status. GRPCCode is decoded from the binary google.rpc.Status error
// body (contract §1.1) and is codes.OK when the body was not that form (for
// example the 413 guard's text/plain body). Message is the server error text.
type BridgeError struct {
	StatusCode int
	GRPCCode   codes.Code
	Message    string
}

// Error implements error.
func (e *BridgeError) Error() string {
	if e == nil {
		return ""
	}
	if e.GRPCCode != codes.OK {
		return fmt.Sprintf("http %d (%s): %s", e.StatusCode, e.GRPCCode, e.Message)
	}
	return fmt.Sprintf("http %d: %s", e.StatusCode, e.Message)
}

// streamErrorMessage is the {"error": <google.rpc.Status>} tail line of the
// downlink NDJSON stream; only message is consumed
// (specs/066-desktop-bridge-split/data-model.md §1.4).
type streamErrorMessage struct {
	Message string `json:"message"`
}

// parseFrameLine decodes one NDJSON envelope line: {"result": <TeamFrame>} or
// the {"error": ...} stream-termination tail.
func parseFrameLine(line []byte) (*game.TeamFrame, error) {
	var envelope struct {
		Result json.RawMessage     `json:"result"`
		Error  *streamErrorMessage `json:"error"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return nil, fmt.Errorf("decode stream line: %w", err)
	}
	if envelope.Error != nil {
		message := envelope.Error.Message
		if message == "" {
			message = "stream terminated"
		}
		return nil, fmt.Errorf("stream terminated: %s", message)
	}
	if len(envelope.Result) == 0 {
		return nil, fmt.Errorf("decode stream line: missing result frame")
	}

	frame := new(game.TeamFrame)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(envelope.Result, frame); err != nil {
		return nil, fmt.Errorf("decode stream frame: %w", err)
	}
	return frame, nil
}

// statusName returns the status FlowPart's enum name, or the UNSPECIFIED name
// when the frame carries no status FlowPart.
func statusName(frame *game.TeamFrame) string {
	flowParts := frame.GetFlowParts()
	for _, part := range flowParts.GetParts() {
		if s := part.GetStatus(); s != nil {
			return s.GetStatus().String()
		}
	}
	return game.StatusSignalStatus_STATUS_SIGNAL_STATUS_UNSPECIFIED.String()
}

// readStreamOpenError decodes a pre-stream failure response. The :stream GET
// has no request Content-Type, so grpc-gateway's outbound marshaler is the
// wildcard JSON codec and the body is a JSON status envelope
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §1.1).
func readStreamOpenError(resp *http.Response) error {
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return &BridgeError{StatusCode: resp.StatusCode, Message: resp.Status}
	}

	message := ""
	if len(body) > 0 {
		parsed := new(statuspb.Status)
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, parsed); err == nil {
			message = parsed.GetMessage()
		}
		if message == "" {
			message = strings.TrimSpace(string(body))
		}
	}
	if message == "" {
		message = resp.Status
	}
	return &BridgeError{StatusCode: resp.StatusCode, Message: message}
}

// decodeSendFrameError decodes a sendFrame error response by its Content-Type
// media type (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md
// §1.1/§5.3): application/octet-stream is a binary google.rpc.Status from which
// the gRPC code and message are recovered; any other type (the 413 guard's
// text/plain body) carries the message verbatim.
func decodeSendFrameError(resp *http.Response) error {
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return &BridgeError{StatusCode: resp.StatusCode, Message: resp.Status}
	}

	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType == "application/octet-stream" {
		parsed := new(statuspb.Status)
		if err := proto.Unmarshal(body, parsed); err == nil {
			st := status.FromProto(parsed)
			return &BridgeError{StatusCode: resp.StatusCode, GRPCCode: st.Code(), Message: st.Message()}
		}
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	return &BridgeError{StatusCode: resp.StatusCode, Message: message}
}
