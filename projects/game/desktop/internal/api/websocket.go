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

// WSClient is a WebSocket client for the game gateway agent connect endpoint.
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

	// Build full connect URL: wss://host/api/v1/sessions/{id}/agent/connect
	fullURL := fmt.Sprintf("%s/api/v1/sessions/%s/agent/connect", strings.TrimSuffix(wsURL, "/"), sessionID)

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
func (w *WSClient) SendFrame(frame *game.AgentFrame) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return fmt.Errorf("send frame: not connected")
	}

	data, err := protojson.Marshal(frame)
	if err != nil {
		return fmt.Errorf("send frame: %w", err)
	}

	if err := w.conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		return fmt.Errorf("send frame: %w", err)
	}
	return nil
}

// RecvFrame receives a protojson-encoded AgentFrame from the WebSocket.
// Unknown fields are discarded for forward compatibility.
func (w *WSClient) RecvFrame() (*game.AgentFrame, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return nil, fmt.Errorf("receive frame: not connected")
	}

	_, data, err := w.conn.Read(context.Background())
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
func (w *WSClient) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return nil
	}

	// Close with normal status
	err := w.conn.Close(websocket.StatusNormalClosure, "")
	w.conn = nil
	if err != nil {
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
