package transport

import (
	"context"
	"fmt"
	"sync"

	gw "dominion/projects/game/gateway"
	"dominion/projects/game/gateway/domain"

	"github.com/coder/websocket"
)

// readLimit is the maximum size for a single WebSocket message.
// Generous limit: 2× max segment size + 4 KiB overhead for envelope fields.
var readLimit = int64(domain.MaxSegmentSize)*2 + 4096

// Client manages a WebSocket connection to the game gateway.
type Client struct {
	conn      *websocket.Conn
	sessionID string
	mu        sync.Mutex
	closed    bool
}

// NewClient creates a new WebSocket client.
func NewClient() *Client {
	return new(Client)
}

// Connect dials the WebSocket at connectURL and stores the connection.
func (c *Client) Connect(ctx context.Context, connectURL string) error {
	conn, _, err := websocket.Dial(ctx, connectURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	conn.SetReadLimit(readLimit)

	c.mu.Lock()
	c.conn = conn
	c.closed = false
	c.mu.Unlock()

	return nil
}

// Close sends a normal closure frame and closes the underlying connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.conn == nil {
		return nil
	}
	c.closed = true

	return c.conn.Close(websocket.StatusNormalClosure, "windows-agent done")
}

// IsConnected returns whether the client has an active WebSocket connection.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil && !c.closed
}

// writeEnvelope serialises the envelope to JSON and writes it as a text frame.
func (c *Client) writeEnvelope(ctx context.Context, env *gw.GameWebSocketEnvelope) error {
	data, err := EncodeEnvelope(env)
	if err != nil {
		return fmt.Errorf("encode envelope: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, data)
}
