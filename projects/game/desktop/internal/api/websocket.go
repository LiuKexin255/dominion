package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	game "dominion/projects/game"

	"dominion/projects/game/desktop/internal/trace"
	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

// WSClient is a WebSocket client for the game gateway team connect endpoint.
type WSClient struct {
	mu        sync.Mutex
	conn      *websocket.Conn
	template  string
	sessionID string
}

// Connect establishes a WebSocket connection to the gateway's team connect
// endpoint. gatewayURL is the HTTP URL (e.g., "https://game.liukexin.com").
// The URL is converted from https:// to wss:// (or http:// to ws://).
// The connect path follows the gateway convention
// /api/v1/templates/{template}/sessions/{sessionID}/connect (spec
// 031-team-template-mode contracts/api-contract.md §2.2, FR-004).
func (w *WSClient) Connect(ctx context.Context, gatewayURL, template, sessionID, env string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Convert HTTP URL to WebSocket URL
	wsURL, err := convertToWS(gatewayURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Build full connect URL: wss://host/api/v1/templates/{template}/sessions/{id}/connect
	fullURL := fmt.Sprintf("%s/api/v1/templates/%s/sessions/%s/connect",
		strings.TrimSuffix(wsURL, "/"), url.PathEscape(template), url.PathEscape(sessionID))

	// Set up headers
	header := http.Header{}
	if env != "" {
		header.Set("env", env)
	}

	// Dial the WebSocket
	conn, _, err := websocket.Dial(ctx, fullURL, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: trace.NewHTTPTransport()},
		HTTPHeader: header,
	})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Raise the read limit from the coder/websocket default (32 KiB) to 10 MiB
	// so image-bearing frames (screenshots) do not trigger ErrMessageTooBig and
	// tear down the session. Matches the gateway's 10 MiB limit
	// (projects/game/gateway/cmd/main.go:216). See
	// specs/025-desktop-image-state-refine/contracts/image-transport-contract.md §3.
	conn.SetReadLimit(10 << 20)

	w.conn = conn
	w.template = template
	w.sessionID = sessionID
	return nil
}

// SendFrame sends a binary-protobuf-encoded AgentFrame over the WebSocket.
func (w *WSClient) SendFrame(ctx context.Context, frame *game.AgentFrame) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return fmt.Errorf("send frame: not connected")
	}

	data, err := proto.Marshal(frame)
	if err != nil {
		return fmt.Errorf("send frame: %w", err)
	}

	if err := w.conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		return fmt.Errorf("send frame: %w", err)
	}
	return nil
}

// RecvFrame receives a binary-protobuf-encoded AgentFrame from the WebSocket.
// proto.Unmarshal preserves unknown fields per the proto spec, maintaining the
// forward-compatibility that protojson's DiscardUnknown previously provided
// (specs/025-desktop-image-state-refine/contracts/image-transport-contract.md §2).
//
// The connection is snapshotted under w.mu and Read is called WITHOUT the
// lock held: conn.Read blocks for the lifetime of the turn, so holding w.mu
// across it would deadlock Close (R5).
func (w *WSClient) RecvFrame(ctx context.Context) (*game.AgentFrame, error) {
	w.mu.Lock()
	conn := w.conn
	w.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("receive frame: not connected")
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("receive frame: %w", err)
	}

	frame := new(game.AgentFrame)
	if err := proto.Unmarshal(data, frame); err != nil {
		return nil, fmt.Errorf("receive frame: %w", err)
	}
	return frame, nil
}

// Close closes the WebSocket connection.
// It is safe to call Close multiple times or when not connected.
//
// The connection is grabbed and nil'd under w.mu, then closed OUTSIDE the
// lock so an in-flight RecvFrame does not deadlock on w.mu (R5).
//
// CloseNow (not Close with a status) is used deliberately: coder/websocket's
// Close(code, reason) performs a close handshake whose waitCloseHandshake
// acquires the same read-lock an in-flight RecvFrame holds, blocking up to
// the 5s handshake timeout. CloseNow tears the underlying connection down
// immediately, unblocking RecvFrame's Read without any handshake wait — the
// semantics CloseAgent needs to promptly tear the socket down.
func (w *WSClient) Close() error {
	w.mu.Lock()
	conn := w.conn
	w.conn = nil
	w.mu.Unlock()

	if conn == nil {
		return nil
	}

	if err := conn.CloseNow(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

// convertToWS converts an HTTP URL to a WebSocket URL.
// "https://host" → "wss://host", "http://host" → "ws://host"
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
