package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	game "dominion/projects/game"

	"dominion/common/gopkg/bootstrap"
	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

// probeTimeout bounds the wait for the first TeamFrame after the probe — the
// same application-layer probe window the real desktop applies
// (projects/game/desktop/app.go App.Connect, 10s; contracts/desktop-bridge.md
// §1).
const probeTimeout = 10 * time.Second

// reconnectDelay is the fixed pause between connection attempts. A test
// facility favors determinism over adaptive backoff.
const reconnectDelay = 2 * time.Second

// disconnectDrainPause is the grace period between the injected teardown
// decision and the actual close: the final receipts must drain through the
// gateway→proxy relay before the stream tears down, otherwise the bridge's
// disconnect settlement races the receipts and flips the last operation
// into a synthetic FAILED receipt (US1 scenario 5 wants the NEXT dispatch
// to fail, not the one whose receipt already left).
const disconnectDrainPause = time.Second

// readLimit mirrors the gateway's 10 MiB WebSocket read limit so image-bearing
// frames do not tear down the session (projects/game/gateway/cmd/main.go).
const readLimit = 10 << 20

// Config wires the executor to one gateway session.
type Config struct {
	// GatewayURL is the gateway base HTTP URL (http://host:port); it is
	// converted to its ws:// form for dialing.
	GatewayURL string
	// Template and Session identify the flow session to bind; the gateway
	// injects them into the first frame, overwriting the client values.
	Template string
	Session  string
	// Scenario selects the deterministic board scenario.
	Scenario string
	// Fault carries the failure injections.
	Fault Fault
}

// Run dials the gateway /api/v2 connect entry and serves the session until
// ctx is cancelled. A dropped connection is re-dialed after reconnectDelay
// (US1 scenario 5: a mid-game desktop disconnect must recover so the game can
// continue).
func Run(ctx context.Context, cfg Config, log *slog.Logger) error {
	scenario, err := loadScenario(cfg.Scenario)
	if err != nil {
		return err
	}
	wsURL, err := convertToWS(cfg.GatewayURL)
	if err != nil {
		return fmt.Errorf("gateway url: %w", err)
	}
	connectURL := fmt.Sprintf("%s/api/v2/templates/%s/sessions/%s/connect",
		strings.TrimSuffix(wsURL, "/"), url.PathEscape(cfg.Template), url.PathEscape(cfg.Session))

	for {
		if err := serveOnce(ctx, connectURL, cfg, scenario, log); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Error("desktop flow connection ended", slog.String("url", connectURL), slog.String("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reconnectDelay):
		}
	}
}

// serveOnce runs one connection lifetime: dial, probe, read loop.
func serveOnce(ctx context.Context, connectURL string, cfg Config, scenario *Scenario, log *slog.Logger) error {
	executor := NewExecutor(scenario, cfg.Fault)

	dialCtx, cancelDial := context.WithTimeout(ctx, probeTimeout)
	defer cancelDial()
	conn, _, err := websocket.Dial(dialCtx, connectURL, &websocket.DialOptions{
		HTTPHeader: http.Header{},
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	conn.SetReadLimit(readLimit)
	defer conn.CloseNow()

	// First frame = identity + probe (contracts/desktop-bridge.md §1): the
	// gateway binds the connection from the URL, the bridge answers with a
	// status frame.
	probe := &game.UserFrame{
		SessionId:  cfg.Session,
		TemplateId: cfg.Template,
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{{
			Kind: &game.FlowPart_Status{Status: &game.StatusSignal{
				Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE,
			}},
		}}}},
	}
	if err := writeFrame(ctx, conn, probe); err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	probeCtx, cancelProbe := context.WithTimeout(ctx, probeTimeout)
	defer cancelProbe()
	first, err := readFrame(probeCtx, conn)
	if err != nil {
		return fmt.Errorf("probe reply: %w", err)
	}
	log.Info("desktop flow connected",
		slog.String("session", cfg.Session),
		slog.String("scenario", scenario.Name),
		slog.Int("reply_parts", len(first.GetFlowParts().GetParts())))

	for {
		frame, err := readFrame(ctx, conn)
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
			reply := &game.UserFrame{
				SessionId:  cfg.Session,
				TemplateId: cfg.Template,
				Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{{
					Kind: &game.FlowPart_FlowResult{FlowResult: receipt},
				}}}},
			}
			if err := writeFrame(ctx, conn, reply); err != nil {
				return fmt.Errorf("receipt: %w", err)
			}
		}
		// The disconnect injection closes the connection after the
		// configured receipts are out (US1 scenario 5 mid-game drop). The
		// drain pause lets those receipts cross the relay first — CloseNow
		// would drop them mid-flight, and an immediate close races the
		// bridge's disconnect settlement.
		if executor.Disconnected() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(disconnectDrainPause):
			}
			_ = conn.Close(websocket.StatusGoingAway, "fault injection: disconnecting after operation receipts")
			return errors.New("fault injection: disconnecting after operation receipts")
		}
	}
}

// writeFrame sends one binary-proto UserFrame.
func writeFrame(ctx context.Context, conn *websocket.Conn, frame *game.UserFrame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// readFrame receives one binary-proto TeamFrame.
func readFrame(ctx context.Context, conn *websocket.Conn) (*game.TeamFrame, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	frame := new(game.TeamFrame)
	if err := proto.Unmarshal(data, frame); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return frame, nil
}

// convertToWS converts an HTTP URL to a WebSocket URL (shared shape with the
// desktop client, projects/game/desktop/internal/api/websocket.go).
func convertToWS(httpURL string) (string, error) {
	u, err := url.Parse(httpURL)
	if err != nil {
		return "", err
	}
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s", scheme, u.Host), nil
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
