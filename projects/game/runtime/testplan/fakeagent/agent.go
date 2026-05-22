// Package fakeagent provides a fake Windows agent implementation for
// runtime service-level testing. It connects to the runtime via WebSocket
// using runtimepb proto types and can send media segments and respond to
// control requests.
package fakeagent

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	runtimepb "dominion/projects/game/runtime"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	jsonMarshaler   = protojson.MarshalOptions{}
	jsonUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// Agent is a fake Windows agent that connects to a game runtime via WebSocket.
type Agent struct {
	conn      *websocket.Conn
	sessionID string

	mu             sync.Mutex
	controlHandled map[string]chan *runtimepb.GameControlResult
	done           chan struct{}
}

// Connect establishes a WebSocket connection to the runtime and sends a
// GameHello with role=WINDOWS_AGENT. The wsURL should be a full WebSocket URL
// including the token query parameter.
func Connect(ctx context.Context, wsURL, sessionID string) (*Agent, error) {
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{},
	})
	if err != nil {
		return nil, fmt.Errorf("dial websocket: %w", err)
	}

	a := &Agent{
		conn:           conn,
		sessionID:      sessionID,
		controlHandled: make(map[string]chan *runtimepb.GameControlResult),
		done:           make(chan struct{}),
	}

	// Send hello.
	hello := &runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: a.nextMsgID("hello"),
		Payload: &runtimepb.GameWebSocketEnvelope_Hello{
			Hello: &runtimepb.GameHello{
				Role: runtimepb.GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			},
		},
	}
	if err := a.write(ctx, hello); err != nil {
		conn.Close(websocket.StatusNormalClosure, "hello write failed")
		return nil, fmt.Errorf("write hello: %w", err)
	}

	return a, nil
}

// SendMediaInit sends a fMP4 initialization segment.
func (a *Agent) SendMediaInit(ctx context.Context, streamID, initID, mimeType, codec string, data []byte) error {
	msg := &runtimepb.GameWebSocketEnvelope{
		SessionId: a.sessionID,
		MessageId: a.nextMsgID("media-init"),
		Payload: &runtimepb.GameWebSocketEnvelope_MediaInit{
			MediaInit: &runtimepb.GameMediaInit{
				StreamId: streamID,
				InitId:   initID,
				MimeType: mimeType,
				Codec:    codec,
				Segment:  data,
			},
		},
	}
	return a.write(ctx, msg)
}

// SendMediaSegment sends a fMP4 media segment.
func (a *Agent) SendMediaSegment(ctx context.Context, streamID, initID string, sequence uint64, data []byte, randomAccess bool) error {
	msg := &runtimepb.GameWebSocketEnvelope{
		SessionId: a.sessionID,
		MessageId: a.nextMsgID("media-seg"),
		Payload: &runtimepb.GameWebSocketEnvelope_MediaSegment{
			MediaSegment: &runtimepb.GameMediaSegment{
				StreamId:     streamID,
				InitId:       initID,
				Sequence:     sequence,
				Segment:      data,
				RandomAccess: &randomAccess,
			},
		},
	}
	return a.write(ctx, msg)
}

// SendAck sends a control acknowledgment.
func (a *Agent) SendAck(ctx context.Context, operationID string) error {
	msg := &runtimepb.GameWebSocketEnvelope{
		SessionId: a.sessionID,
		MessageId: a.nextMsgID("ack"),
		Payload: &runtimepb.GameWebSocketEnvelope_ControlAck{
			ControlAck: &runtimepb.GameControlAck{
				OperationId: operationID,
			},
		},
	}
	return a.write(ctx, msg)
}

// SendControlResult sends a control result.
func (a *Agent) SendControlResult(ctx context.Context, operationID string, status runtimepb.GameControlResultStatus, errMsg string) error {
	msg := &runtimepb.GameWebSocketEnvelope{
		SessionId: a.sessionID,
		MessageId: a.nextMsgID("result"),
		Payload: &runtimepb.GameWebSocketEnvelope_ControlResult{
			ControlResult: &runtimepb.GameControlResult{
				OperationId:  operationID,
				Status:       status,
				ErrorMessage: errMsg,
			},
		},
	}
	return a.write(ctx, msg)
}

// ExpectControlRequest reads and returns the next GameControlRequest from the
// WebSocket. Returns an error on timeout or if a non-control-request message is
// received.
func (a *Agent) ExpectControlRequest(ctx context.Context) (*runtimepb.GameControlRequest, error) {
	env, err := a.read(ctx)
	if err != nil {
		return nil, fmt.Errorf("expect control request: %w", err)
	}
	req := env.GetControlRequest()
	if req == nil {
		return nil, fmt.Errorf("expected control request, got %T", env.Payload)
	}
	return req, nil
}

// ReadMessage reads and returns the next GameWebSocketEnvelope.
func (a *Agent) ReadMessage(ctx context.Context) (*runtimepb.GameWebSocketEnvelope, error) {
	return a.read(ctx)
}

// Close closes the WebSocket connection.
func (a *Agent) Close() error {
	return a.conn.Close(websocket.StatusNormalClosure, "test done")
}

// write serializes and sends a proto message on the WebSocket.
func (a *Agent) write(ctx context.Context, msg *runtimepb.GameWebSocketEnvelope) error {
	data, err := jsonMarshaler.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return a.conn.Write(ctx, websocket.MessageText, data)
}

// read reads and deserializes a proto message from the WebSocket.
func (a *Agent) read(ctx context.Context) (*runtimepb.GameWebSocketEnvelope, error) {
	_, data, err := a.conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	env := new(runtimepb.GameWebSocketEnvelope)
	if err := jsonUnmarshaler.Unmarshal(data, env); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return env, nil
}

// nextMsgID generates a unique message ID.
func (a *Agent) nextMsgID(prefix string) string {
	return fmt.Sprintf("test-%s-%d", prefix, time.Now().UnixNano())
}
