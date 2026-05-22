// Package transport provides the WebSocket transport layer for the Windows Agent.
package transport

import (
	"fmt"
	"time"

	runtimepb "dominion/projects/game/runtime"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	protojsonMarshaler   = protojson.MarshalOptions{}
	protojsonUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// EncodeEnvelope marshals a GameWebSocketEnvelope to JSON bytes.
func EncodeEnvelope(env *runtimepb.GameWebSocketEnvelope) ([]byte, error) {
	data, err := protojsonMarshaler.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal envelope: %w", err)
	}
	return data, nil
}

// DecodeEnvelope unmarshals JSON bytes to a GameWebSocketEnvelope.
// Unknown fields are discarded to maintain forward compatibility.
func DecodeEnvelope(data []byte) (*runtimepb.GameWebSocketEnvelope, error) {
	env := new(runtimepb.GameWebSocketEnvelope)
	if err := protojsonUnmarshaler.Unmarshal(data, env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return env, nil
}

// MessageID generates a unique message ID with the given prefix.
// Format: "{prefix}-{unix_nano}".
func MessageID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
