package transport

import (
	"strings"
	"testing"

	gw "dominion/projects/game/gateway"

	"google.golang.org/protobuf/proto"
)

func TestEncodeEnvelope(t *testing.T) {
	// given
	env := &gw.GameWebSocketEnvelope{
		SessionId: "session-123",
		MessageId: "msg-001",
		Payload: &gw.GameWebSocketEnvelope_Hello{
			Hello: &gw.GameHello{
				Role: gw.GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			},
		},
	}

	// when
	data, err := EncodeEnvelope(env)

	// then
	if err != nil {
		t.Fatalf("EncodeEnvelope unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("EncodeEnvelope returned empty data")
	}
	if !strings.Contains(string(data), `"sessionId"`) {
		t.Fatalf("EncodeEnvelope result missing sessionId field: %s", data)
	}
	if !strings.Contains(string(data), `"hello"`) {
		t.Fatalf("EncodeEnvelope result missing hello payload: %s", data)
	}
}

func TestDecodeEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    *gw.GameWebSocketEnvelope
		wantErr bool
	}{
		{
			name: "valid hello envelope",
			json: `{"sessionId":"s-1","messageId":"m-1","hello":{"role":1}}`,
			want: &gw.GameWebSocketEnvelope{
				SessionId: "s-1",
				MessageId: "m-1",
				Payload: &gw.GameWebSocketEnvelope_Hello{
					Hello: &gw.GameHello{
						Role: gw.GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
					},
				},
			},
		},
		{
			name: "valid ping envelope",
			json: `{"sessionId":"s-2","messageId":"m-2","ping":{"nonce":"abc"}}`,
			want: &gw.GameWebSocketEnvelope{
				SessionId: "s-2",
				MessageId: "m-2",
				Payload: &gw.GameWebSocketEnvelope_Ping{
					Ping: &gw.GamePing{Nonce: "abc"},
				},
			},
		},
		{
			name:    "invalid json",
			json:    `{not json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got, err := DecodeEnvelope([]byte(tt.json))

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DecodeEnvelope(%q) expected error, got nil", tt.json)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeEnvelope(%q) unexpected error: %v", tt.json, err)
			}
			if got.SessionId != tt.want.SessionId {
				t.Fatalf("SessionId: got %q, want %q", got.SessionId, tt.want.SessionId)
			}
			if got.MessageId != tt.want.MessageId {
				t.Fatalf("MessageId: got %q, want %q", got.MessageId, tt.want.MessageId)
			}
			if !proto.Equal(got, tt.want) {
				t.Fatalf("envelope mismatch:\ngot:  %v\nwant: %v", got, tt.want)
			}
		})
	}
}

func TestEncodeDecodeRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		env  *gw.GameWebSocketEnvelope
	}{
		{
			name: "media_init v2 roundtrip",
			env: &gw.GameWebSocketEnvelope{
				SessionId: "session-rt",
				MessageId: "msg-rt-init",
				Payload: &gw.GameWebSocketEnvelope_MediaInit{
					MediaInit: &gw.GameMediaInit{
						StreamId: "stream-1",
						InitId:   "init-abc123",
						MimeType: MimeTypeMP4,
						Codec:    CodecH264AVC,
						Segment:  []byte{0x00, 0x01, 0x02},
					},
				},
			},
		},
		{
			name: "media_segment v2 roundtrip",
			env: &gw.GameWebSocketEnvelope{
				SessionId: "session-rt",
				MessageId: "msg-rt-seg",
				Payload: &gw.GameWebSocketEnvelope_MediaSegment{
					MediaSegment: &gw.GameMediaSegment{
						StreamId:      "stream-1",
						InitId:        "init-abc123",
						Sequence:      42,
						Segment:       []byte{0xAA, 0xBB, 0xCC},
						RandomAccess:  boolPtr(true),
						DurationMs:    33,
						Discontinuity: false,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			data, err := EncodeEnvelope(tt.env)
			if err != nil {
				t.Fatalf("EncodeEnvelope unexpected error: %v", err)
			}
			decoded, err := DecodeEnvelope(data)
			if err != nil {
				t.Fatalf("DecodeEnvelope unexpected error: %v", err)
			}

			// then
			if !proto.Equal(tt.env, decoded) {
				t.Fatalf("roundtrip mismatch:\noriginal: %v\ndecoded: %v", tt.env, decoded)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

func TestMessageID(t *testing.T) {
	// given
	prefix := "hello"

	// when
	id1 := MessageID(prefix)
	id2 := MessageID(prefix)

	// then
	if !strings.HasPrefix(id1, prefix+"-") {
		t.Fatalf("MessageID(%q) = %q, want prefix %q", prefix, id1, prefix+"-")
	}
	if id1 == id2 {
		t.Fatalf("MessageID called twice returned same value: %s", id1)
	}
}

func TestDecodeEnvelopeDiscardUnknown(t *testing.T) {
	// given: JSON with an extra field "futureField" not in the proto schema
	jsonData := `{"sessionId":"s-d","messageId":"m-d","hello":{"role":1},"futureField":"unknown"}`

	// when
	env, err := DecodeEnvelope([]byte(jsonData))

	// then
	if err != nil {
		t.Fatalf("DecodeEnvelope with unknown fields unexpected error: %v", err)
	}
	if env.SessionId != "s-d" {
		t.Fatalf("SessionId: got %q, want %q", env.SessionId, "s-d")
	}
}
