package transport

import (
	"context"
	"fmt"

	runtimepb "dominion/projects/game/runtime"
	"dominion/projects/game/runtime/domain"
)

// SendHello sends the hello message with the agent role after connecting.
func (c *Client) SendHello(ctx context.Context, sessionID string) error {
	c.mu.Lock()
	c.sessionID = sessionID
	c.mu.Unlock()

	return c.writeEnvelope(ctx, &runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("hello"),
		Payload: &runtimepb.GameWebSocketEnvelope_Hello{
			Hello: &runtimepb.GameHello{
				Role: AgentRole,
			},
		},
	})
}

// SendMediaInit sends the fMP4 initialisation segment to the gateway.
func (c *Client) SendMediaInit(ctx context.Context, sessionID, streamID, initID, mimeType, codec string, segment []byte) error {
	if len(segment) > domain.MaxSegmentSize {
		return fmt.Errorf("media_init segment %d bytes exceeds %d limit", len(segment), domain.MaxSegmentSize)
	}

	return c.writeEnvelope(ctx, &runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("media-init"),
		Payload: &runtimepb.GameWebSocketEnvelope_MediaInit{
			MediaInit: &runtimepb.GameMediaInit{
				StreamId: streamID,
				InitId:   initID,
				MimeType: mimeType,
				Codec:    codec,
				Segment:  segment,
			},
		},
	})
}

// SendMediaSegment sends one fMP4 media segment to the gateway.
func (c *Client) SendMediaSegment(ctx context.Context, sessionID, streamID, initID string, sequence uint64, segment []byte, randomAccess *bool, durationMS int32, discontinuity bool) error {
	if len(segment) > domain.MaxSegmentSize {
		return fmt.Errorf("media_segment %d bytes exceeds %d limit", len(segment), domain.MaxSegmentSize)
	}

	return c.writeEnvelope(ctx, &runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("media-seg"),
		Payload: &runtimepb.GameWebSocketEnvelope_MediaSegment{
			MediaSegment: &runtimepb.GameMediaSegment{
				StreamId:      streamID,
				InitId:        initID,
				Sequence:      sequence,
				Segment:       segment,
				RandomAccess:  randomAccess,
				DurationMs:    durationMS,
				Discontinuity: discontinuity,
			},
		},
	})
}

// SendControlAck acknowledges receipt of a control request.
func (c *Client) SendControlAck(ctx context.Context, sessionID, operationID string) error {
	return c.writeEnvelope(ctx, &runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("ack"),
		Payload: &runtimepb.GameWebSocketEnvelope_ControlAck{
			ControlAck: &runtimepb.GameControlAck{
				OperationId: operationID,
			},
		},
	})
}

// SendControlResult sends the outcome of a control operation.
func (c *Client) SendControlResult(ctx context.Context, sessionID, operationID string, status runtimepb.GameControlResultStatus) error {
	return c.writeEnvelope(ctx, &runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("result"),
		Payload: &runtimepb.GameWebSocketEnvelope_ControlResult{
			ControlResult: &runtimepb.GameControlResult{
				OperationId: operationID,
				Status:      status,
			},
		},
	})
}

// SendPong replies to a ping from the gateway.
func (c *Client) SendPong(ctx context.Context, sessionID, nonce string) error {
	return c.writeEnvelope(ctx, &runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("pong"),
		Payload: &runtimepb.GameWebSocketEnvelope_Pong{
			Pong: &runtimepb.GamePong{Nonce: nonce},
		},
	})
}

// SendError reports an error to the gateway.
func (c *Client) SendError(ctx context.Context, sessionID, code, message string) error {
	return c.writeEnvelope(ctx, &runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: MessageID("error"),
		Payload: &runtimepb.GameWebSocketEnvelope_Error{
			Error: &runtimepb.GameError{
				Code:    code,
				Message: message,
			},
		},
	})
}
