package transport

import (
	"context"
	"log"

	gw "dominion/projects/game/gateway"

	"fmt"
)

// InboundMessage represents a decoded downstream WebSocket message.
// Exactly one of the typed fields is non-nil, matching the envelope payload.
type InboundMessage struct {
	Envelope       *gw.GameWebSocketEnvelope
	ControlRequest *gw.GameControlRequest
	Ping           *gw.GamePing
	Error          *gw.GameError
}

// ReadLoop starts a goroutine that reads WebSocket messages and dispatches
// them as InboundMessage values on the returned channel. The channel is closed
// when the read loop exits (on error or context cancellation).
func (c *Client) ReadLoop(ctx context.Context) (<-chan InboundMessage, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	ch := make(chan InboundMessage, 16)

	go func() {
		defer close(ch)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("transport: read error: %v", err)
				return
			}

			env, err := DecodeEnvelope(data)
			if err != nil {
				log.Printf("transport: decode error: %v", err)
				continue
			}

			msg := InboundMessage{Envelope: env}
			switch p := env.Payload.(type) {
			case *gw.GameWebSocketEnvelope_ControlRequest:
				msg.ControlRequest = p.ControlRequest
			case *gw.GameWebSocketEnvelope_Ping:
				msg.Ping = p.Ping
			case *gw.GameWebSocketEnvelope_Error:
				msg.Error = p.Error
			default:
				log.Printf("transport: ignoring message type %T", env.Payload)
				continue
			}

			select {
			case ch <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}
