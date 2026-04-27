package transport

import (
	"context"
	"fmt"

	gw "dominion/projects/game/gateway"
	"dominion/projects/game/gateway/domain"
)

// SendHello sends the hello message with the agent role after connecting.
func (c *Client) SendHello(ctx context.Context, sessionID string) error {
	c.mu.Lock()
	c.sessionID = sessionID
	c.mu.Unlock()

	return c.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("hello"),
		Payload: &gw.GameWebSocketEnvelope_Hello{
			Hello: &gw.GameHello{
				Role: AgentRole,
			},
		},
	})
}

// SendMediaInit sends the fMP4 initialisation segment to the gateway.
func (c *Client) SendMediaInit(ctx context.Context, sessionID, mimeType string, segment []byte) error {
	if len(segment) > domain.MaxSegmentSize {
		return fmt.Errorf("media_init segment %d bytes exceeds %d limit", len(segment), domain.MaxSegmentSize)
	}

	return c.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("media-init"),
		Payload: &gw.GameWebSocketEnvelope_MediaInit{
			MediaInit: &gw.GameMediaInit{
				MimeType: mimeType,
				Segment:  segment,
			},
		},
	})
}

// SendMediaSegment sends one fMP4 media segment to the gateway.
func (c *Client) SendMediaSegment(ctx context.Context, sessionID, segmentID string, segment []byte, keyFrame bool) error {
	if len(segment) > domain.MaxSegmentSize {
		return fmt.Errorf("media_segment %d bytes exceeds %d limit", len(segment), domain.MaxSegmentSize)
	}

	return c.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("media-seg"),
		Payload: &gw.GameWebSocketEnvelope_MediaSegment{
			MediaSegment: &gw.GameMediaSegment{
				SegmentId: segmentID,
				Segment:   segment,
				KeyFrame:  keyFrame,
			},
		},
	})
}

// SendControlAck acknowledges receipt of a control request.
func (c *Client) SendControlAck(ctx context.Context, sessionID, operationID string) error {
	return c.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("ack"),
		Payload: &gw.GameWebSocketEnvelope_ControlAck{
			ControlAck: &gw.GameControlAck{
				OperationId: operationID,
			},
		},
	})
}

// SendControlResult sends the outcome of a control operation.
func (c *Client) SendControlResult(ctx context.Context, sessionID, operationID string, status gw.GameControlResultStatus) error {
	return c.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("result"),
		Payload: &gw.GameWebSocketEnvelope_ControlResult{
			ControlResult: &gw.GameControlResult{
				OperationId: operationID,
				Status:      status,
			},
		},
	})
}

// SendPong replies to a ping from the gateway.
func (c *Client) SendPong(ctx context.Context, sessionID, nonce string) error {
	return c.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("pong"),
		Payload: &gw.GameWebSocketEnvelope_Pong{
			Pong: &gw.GamePong{Nonce: nonce},
		},
	})
}

// SendError reports an error to the gateway.
func (c *Client) SendError(ctx context.Context, sessionID, code, message string) error {
	return c.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("error"),
		Payload: &gw.GameWebSocketEnvelope_Error{
			Error: &gw.GameError{
				Code:    code,
				Message: message,
			},
		},
	})
}
