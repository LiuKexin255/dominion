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
	"google.golang.org/protobuf/encoding/protojson"
)

// WSClient is a WebSocket client for the game gateway session connect endpoint.
type WSClient struct {
	mu        sync.Mutex
	conn      *websocket.Conn
	sessionID string
}

// Connect establishes a WebSocket connection to the gateway's agent connect endpoint.
// gatewayURL is the HTTP URL (e.g., "https://game.liukexin.com").
// The URL is converted from https:// to wss:// (or http:// to ws://).
func (w *WSClient) Connect(ctx context.Context, gatewayURL, sessionID, env string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Convert HTTP URL to WebSocket URL
	wsURL, err := convertToWS(gatewayURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Build full connect URL: wss://host/api/v1/sessions/{id}/connect
	fullURL := fmt.Sprintf("%s/api/v1/sessions/%s/connect", strings.TrimSuffix(wsURL, "/"), sessionID)

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

	w.conn = conn
	w.sessionID = sessionID
	return nil
}

// SendFrame sends a protojson-encoded AgentFrame over the WebSocket.
func (w *WSClient) SendFrame(ctx context.Context, frame *game.AgentFrame) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return fmt.Errorf("send frame: not connected")
	}

	data, err := protojson.Marshal(frame)
	if err != nil {
		return fmt.Errorf("send frame: %w", err)
	}

	if err := w.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("send frame: %w", err)
	}
	return nil
}

// RecvFrame receives a protojson-encoded AgentFrame from the WebSocket.
// Unknown fields are discarded for forward compatibility.
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
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, frame); err != nil {
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
