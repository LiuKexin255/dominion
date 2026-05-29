package game_test

import (
	"testing"
	"time"

	game "dominion/projects/game"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAgentFrameOneofRoundtrip(t *testing.T) {
	given := &game.AgentFrame{
		SessionId: "sessions/test-1",
		FrameId:   "frame-001",
		CreateTime: &timestamppb.Timestamp{
			Seconds: 1748534400,
			Nanos:   123456789,
		},
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId:   "cap-001",
				Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:        []byte{0x01, 0x02, 0x03, 0x04},
				WidthPx:     1920,
				HeightPx:    1080,
				ScaleFactor: 1.5,
				WindowTitle: "Test Window",
				CaptureTime: &timestamppb.Timestamp{
					Seconds: 1748534401,
					Nanos:   987654321,
				},
			},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// when: unmarshal from protojson
	got := new(game.AgentFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify top-level fields
	if got.GetSessionId() != "sessions/test-1" {
		t.Errorf("sessionId: got %q, want %q", got.GetSessionId(), "sessions/test-1")
	}
	if got.GetFrameId() != "frame-001" {
		t.Errorf("frameId: got %q, want %q", got.GetFrameId(), "frame-001")
	}
	if got.GetCreateTime().GetSeconds() != 1748534400 {
		t.Errorf("createTime.seconds: got %d, want %d", got.GetCreateTime().GetSeconds(), 1748534400)
	}
	if got.GetCreateTime().GetNanos() != 123456789 {
		t.Errorf("createTime.nanos: got %d, want %d", got.GetCreateTime().GetNanos(), 123456789)
	}

	// then: verify screenshot payload
	screenshot := got.GetScreenshot()
	if screenshot == nil {
		t.Fatal("GetScreenshot() returned nil")
	}
	if screenshot.GetCaptureId() != "cap-001" {
		t.Errorf("captureId: got %q, want %q", screenshot.GetCaptureId(), "cap-001")
	}
	if screenshot.GetEncoding() != game.ImageEncoding_IMAGE_ENCODING_PNG {
		t.Errorf("encoding: got %v, want %v", screenshot.GetEncoding(), game.ImageEncoding_IMAGE_ENCODING_PNG)
	}
	if screenshot.GetWidthPx() != 1920 {
		t.Errorf("widthPx: got %d, want %d", screenshot.GetWidthPx(), 1920)
	}
	if screenshot.GetHeightPx() != 1080 {
		t.Errorf("heightPx: got %d, want %d", screenshot.GetHeightPx(), 1080)
	}
	if screenshot.GetScaleFactor() != 1.5 {
		t.Errorf("scaleFactor: got %f, want %f", screenshot.GetScaleFactor(), 1.5)
	}
	if screenshot.GetWindowTitle() != "Test Window" {
		t.Errorf("windowTitle: got %q, want %q", screenshot.GetWindowTitle(), "Test Window")
	}
	if screenshot.GetCaptureTime().GetSeconds() != 1748534401 {
		t.Errorf("captureTime.seconds: got %d, want %d", screenshot.GetCaptureTime().GetSeconds(), 1748534401)
	}
}

func TestAgentFrameAckRoundtrip(t *testing.T) {
	// given: an AgentFrame with ack payload
	given := &game.AgentFrame{
		SessionId: "sessions/test-2",
		FrameId:   "ack-frame-001",
		CreateTime: &timestamppb.Timestamp{
			Seconds: time.Now().Unix(),
		},
		Payload: &game.AgentFrame_Ack{
			Ack: &game.AgentAckFrame{
				AckFrameId: "frame-001",
				Message:    "received",
			},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// then: verify camelCase JSON naming in output
	jsonStr := string(jsonBytes)
	if !contains(jsonStr, `"ackFrameId"`) {
		t.Errorf("JSON output missing camelCase field ackFrameId, got: %s", jsonStr)
	}
	if !contains(jsonStr, `"frame-001"`) {
		t.Errorf("JSON output missing ack_frame_id value, got: %s", jsonStr)
	}
	if !contains(jsonStr, `"ack"`) {
		t.Errorf("JSON output missing ack oneof field, got: %s", jsonStr)
	}

	// when: unmarshal from protojson
	got := new(game.AgentFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify all fields preserved
	if got.GetSessionId() != "sessions/test-2" {
		t.Errorf("sessionId: got %q, want %q", got.GetSessionId(), "sessions/test-2")
	}
	ack := got.GetAck()
	if ack == nil {
		t.Fatal("GetAck() returned nil")
	}
	if ack.GetAckFrameId() != "frame-001" {
		t.Errorf("ackFrameId: got %q, want %q", ack.GetAckFrameId(), "frame-001")
	}
	if ack.GetMessage() != "received" {
		t.Errorf("message: got %q, want %q", ack.GetMessage(), "received")
	}
}

func TestEmptyCreateSessionRequest(t *testing.T) {
	// when: marshal empty CreateSessionRequest
	jsonBytes, err := protojson.Marshal(new(game.CreateSessionRequest))
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// then: verify output is {}
	if string(jsonBytes) != "{}" {
		t.Errorf("empty CreateSessionRequest: got %s, want {}", string(jsonBytes))
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
