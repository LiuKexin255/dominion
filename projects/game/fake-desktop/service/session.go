package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	game "dominion/projects/game"

	"dominion/common/gopkg/bootstrap"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// firstFrameTimeout bounds the wait for the downlink stream's first frame —
// the server's connection-confirmation status frame whose write also flushes
// the response headers. It is the same application-layer window the real
// desktop applies
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.2/§5.1).
const firstFrameTimeout = 10 * time.Second

// reconnectDelay is the fixed pause between connection attempts. A test
// facility favors determinism over adaptive backoff.
const reconnectDelay = 2 * time.Second

// flowHTTPClient is the shared HTTP client for the bridge endpoints. Its
// response-header timeout implements firstFrameTimeout for the dial (the
// stream's headers arrive with its first frame, never before), and bounds the
// unary receipt posts the same way.
var flowHTTPClient = newFlowHTTPClient()

func newFlowHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = firstFrameTimeout
	return &http.Client{Transport: transport}
}

// Config wires the executor to one gateway session.
type Config struct {
	// GatewayURL is the gateway base HTTP URL (http://host:port).
	GatewayURL string
	// Template and Session identify the flow session to bind; the downlink
	// stream addresses the session by its resource path.
	Template string
	Session  string
	// Scenario selects the deterministic board scenario.
	Scenario string
	// Fault carries the failure injections.
	Fault Fault
}

// Run opens the gateway /api/v2 session downlink stream and serves the
// session until ctx is cancelled. A dropped connection is re-opened after
// reconnectDelay (US1 scenario 5: a mid-game desktop disconnect must recover
// so the game can continue).
func Run(ctx context.Context, cfg Config, log *slog.Logger) error {
	scenario, err := loadScenario(cfg.Scenario)
	if err != nil {
		return err
	}
	baseURL := fmt.Sprintf("%s/api/v2/templates/%s/sessions/%s",
		strings.TrimSuffix(cfg.GatewayURL, "/"), url.PathEscape(cfg.Template), url.PathEscape(cfg.Session))
	streamURL := baseURL + ":stream"
	sendURL := baseURL + ":sendFrame"

	for {
		if err := serveOnce(ctx, streamURL, sendURL, cfg, scenario, log); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Error("desktop flow connection ended", slog.String("url", streamURL), slog.String("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reconnectDelay):
		}
	}
}

// serveOnce runs one stream lifetime: open the downlink stream, consume the
// first status frame, then read operations and post their receipts until the
// stream ends or the fault injection fires.
func serveOnce(ctx context.Context, streamURL, sendURL string, cfg Config, scenario *Scenario, log *slog.Logger) error {
	executor := NewExecutor(scenario, cfg.Fault)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	resp, err := flowHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("connect: %s", streamOpenError(resp))
	}

	reader := bufio.NewReader(resp.Body)
	first, err := readTeamFrame(reader)
	if err != nil {
		return fmt.Errorf("first frame: %w", err)
	}
	log.Info("desktop flow connected",
		slog.String("session", cfg.Session),
		slog.String("scenario", scenario.Name),
		slog.Int("reply_parts", len(first.GetFlowParts().GetParts())))

	for {
		frame, err := readTeamFrame(reader)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read: %w", err)
		}
		for _, part := range frame.GetFlowParts().GetParts() {
			receipt := executor.Execute(part)
			if receipt == nil {
				continue
			}
			if err := postReceipt(ctx, sendURL, receipt); err != nil {
				return fmt.Errorf("receipt: %w", err)
			}
		}
		// The disconnect injection tears the stream down once the configured
		// receipt count is reached (US1 scenario 5 mid-game drop). The unary
		// response proves the receipt already reached agent_v2: closing the
		// body here cannot overtake an in-flight receipt
		// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §5.5).
		if executor.Disconnected() {
			return errors.New("fault injection: disconnecting after operation receipts")
		}
	}
}

// readTeamFrame reads one downlink line and returns the TeamFrame it carries.
// Lines are read with a newline delimiter, so a message split across transport
// chunks is reassembled and several messages coalesced into one chunk are
// returned one at a time
// (specs/066-desktop-bridge-split/research.md §3).
func readTeamFrame(reader *bufio.Reader) (*game.TeamFrame, error) {
	for {
		line, readErr := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			if readErr != nil {
				return nil, readErr
			}
			continue
		}
		return decodeTeamFrame(trimmed)
	}
}

// decodeTeamFrame decodes one NDJSON envelope line: {"result": <TeamFrame>} or
// the {"error": ...} stream-termination tail
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §1).
func decodeTeamFrame(line []byte) (*game.TeamFrame, error) {
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return nil, fmt.Errorf("decode stream line: %w", err)
	}
	if envelope.Error != nil {
		if envelope.Error.Message != "" {
			return nil, fmt.Errorf("stream terminated: %s", envelope.Error.Message)
		}
		return nil, errors.New("stream terminated")
	}
	if len(envelope.Result) == 0 {
		return nil, errors.New("decode stream line: missing result frame")
	}

	frame := new(game.TeamFrame)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(envelope.Result, frame); err != nil {
		return nil, fmt.Errorf("decode stream frame: %w", err)
	}
	return frame, nil
}

// postReceipt submits one operation receipt through the uplink single-shot
// endpoint (POST :sendFrame, octet-stream;
// specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §1/§5.3).
func postReceipt(ctx context.Context, sendURL string, receipt *game.FlowResultPart) error {
	frame := &game.UserFrame{
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{{
			Kind: &game.FlowPart_FlowResult{FlowResult: receipt},
		}}}},
	}
	payload, err := proto.Marshal(&game.SendFrameRequest{Frame: frame})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := flowHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("post status=%d, body=%s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}

// streamOpenError describes a pre-stream rejection: the :stream GET has no
// request Content-Type, so the gateway reports it as a JSON status envelope
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §1.1).
func streamOpenError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	return fmt.Errorf("stream open status=%d, body=%s", resp.StatusCode, message)
}

// component adapts Run to the bootstrap lifecycle: Start launches the
// connection loop in a goroutine; Stop cancels it.
type component struct {
	cfg  Config
	log  *slog.Logger
	done chan error
}

// Component returns the bootstrap Component serving the desktop flow session.
func Component(cfg Config, log *slog.Logger) bootstrap.Component {
	return &component{cfg: cfg, log: log, done: make(chan error, 1)}
}

// Name returns the component name.
func (c *component) Name() string { return "fake-desktop-flow" }

// Stage returns the server lifecycle stage.
func (c *component) Stage() bootstrap.Stage { return bootstrap.StageServer }

// Start launches the reconnecting connection loop.
func (c *component) Start(ctx context.Context) error {
	go func() {
		err := Run(ctx, c.cfg, c.log)
		if err != nil && ctx.Err() == nil {
			c.done <- err
			return
		}
		c.done <- nil
	}()
	return nil
}

// Stop is a no-op: the loop exits when its context is cancelled by the
// bootstrap shutdown.
func (c *component) Stop(_ context.Context) error { return nil }

// Done reports an unexpected loop exit.
func (c *component) Done() <-chan error { return c.done }
