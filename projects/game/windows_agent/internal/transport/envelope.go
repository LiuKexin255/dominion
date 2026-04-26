// Package transport provides the WebSocket transport layer for the Windows Agent.
package transport

import (
	"fmt"
	"time"

	gw "dominion/projects/game/gateway"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// protojsonMarshaler encodes proto messages to JSON without unknown fields.
var protojsonMarshaler = protojson.MarshalOptions{}

// protojsonUnmarshaler decodes JSON to proto messages, discarding unknown fields.
var protojsonUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}

// EncodeEnvelope marshals a GameWebSocketEnvelope to JSON bytes.
func EncodeEnvelope(env *gw.GameWebSocketEnvelope) ([]byte, error) {
	data, err := protojsonMarshaler.Marshal(proto.Message(env))
	if err != nil {
		return nil, fmt.Errorf("marshal envelope: %w", err)
	}
	return data, nil
}

// DecodeEnvelope unmarshals JSON bytes to a GameWebSocketEnvelope.
// Unknown fields are discarded to maintain forward compatibility.
func DecodeEnvelope(data []byte) (*gw.GameWebSocketEnvelope, error) {
	env := new(gw.GameWebSocketEnvelope)
	if err := protojsonUnmarshaler.Unmarshal(data, proto.Message(env)); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return env, nil
}

// MessageID generates a unique message ID with the given prefix.
// Format: "{prefix}-{unix_nano}".
func MessageID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
