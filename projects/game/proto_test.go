package game_test

import (
	"testing"
	"time"

	game "dominion/projects/game"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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

func TestAgentFrameTextRoundtrip(t *testing.T) {
	// given: an AgentFrame with text payload including invoke_id and sequence
	given := &game.AgentFrame{
		SessionId: "sessions/test-text",
		FrameId:   "frame-text-001",
		InvokeId:  "invoke-001",
		Sequence:  1,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: "Hello from agent"},
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
	if got.GetSessionId() != "sessions/test-text" {
		t.Errorf("sessionId: got %q, want %q", got.GetSessionId(), "sessions/test-text")
	}
	if got.GetFrameId() != "frame-text-001" {
		t.Errorf("frameId: got %q, want %q", got.GetFrameId(), "frame-text-001")
	}
	if got.GetInvokeId() != "invoke-001" {
		t.Errorf("invokeId: got %q, want %q", got.GetInvokeId(), "invoke-001")
	}
	if got.GetSequence() != 1 {
		t.Errorf("sequence: got %d, want %d", got.GetSequence(), 1)
	}

	// then: verify text payload
	text := got.GetText()
	if text == nil {
		t.Fatal("GetText() returned nil")
	}
	if text.GetContent() != "Hello from agent" {
		t.Errorf("text.content: got %q, want %q", text.GetContent(), "Hello from agent")
	}
}

func TestAgentFrameThinkingRoundtrip(t *testing.T) {
	// given: an AgentFrame with thinking payload
	given := &game.AgentFrame{
		SessionId: "sessions/test-thinking",
		FrameId:   "frame-think-001",
		Payload: &game.AgentFrame_Thinking{
			Thinking: &game.AgentThinkingFrame{Content: "Analyzing screenshot..."},
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
	if got.GetSessionId() != "sessions/test-thinking" {
		t.Errorf("sessionId: got %q, want %q", got.GetSessionId(), "sessions/test-thinking")
	}

	// then: verify thinking payload
	thinking := got.GetThinking()
	if thinking == nil {
		t.Fatal("GetThinking() returned nil")
	}
	if thinking.GetContent() != "Analyzing screenshot..." {
		t.Errorf("thinking.content: got %q, want %q", thinking.GetContent(), "Analyzing screenshot...")
	}
}

func TestAgentFrameOperationRoundtrip(t *testing.T) {
	// given: an AgentFrame with mouse operation payload
	given := &game.AgentFrame{
		SessionId: "sessions/test-op",
		InvokeId:  "invoke-001",
		Sequence:  2,
		Payload: &game.AgentFrame_Operation{
			Operation: &game.AgentOperationFrame{
				OperationId: "op-001",
				Operation: &game.AgentOperationFrame_Mouse{
					Mouse: &game.AgentMouseOperation{
						Action: game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_CLICK,
						XPx:    400,
						YPx:    300,
					},
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

	// then: verify operation payload
	op := got.GetOperation()
	if op == nil {
		t.Fatal("GetOperation() returned nil")
	}
	if op.GetOperationId() != "op-001" {
		t.Errorf("operationId: got %q, want %q", op.GetOperationId(), "op-001")
	}

	// then: verify mouse operation
	mouse := op.GetMouse()
	if mouse == nil {
		t.Fatal("GetMouse() returned nil")
	}
	if mouse.GetAction() != game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_CLICK {
		t.Errorf("action: got %v, want %v", mouse.GetAction(), game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_CLICK)
	}
	if mouse.GetXPx() != 400 {
		t.Errorf("xPx: got %d, want %d", mouse.GetXPx(), 400)
	}
	if mouse.GetYPx() != 300 {
		t.Errorf("yPx: got %d, want %d", mouse.GetYPx(), 300)
	}
}

func TestAgentFrameWarnRoundtrip(t *testing.T) {
	// given: an AgentFrame with warn payload
	given := &game.AgentFrame{
		SessionId: "sessions/test-warn",
		Payload: &game.AgentFrame_Warn{
			Warn: &game.AgentWarnFrame{Message: "Stale sequence ignored", Code: "STALE_SEQUENCE"},
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

	// then: verify warn payload
	warn := got.GetWarn()
	if warn == nil {
		t.Fatal("GetWarn() returned nil")
	}
	if warn.GetMessage() != "Stale sequence ignored" {
		t.Errorf("warn.message: got %q, want %q", warn.GetMessage(), "Stale sequence ignored")
	}
	if warn.GetCode() != "STALE_SEQUENCE" {
		t.Errorf("warn.code: got %q, want %q", warn.GetCode(), "STALE_SEQUENCE")
	}
}

func TestAgentImageFrameRoundtrip(t *testing.T) {
	// given: an AgentImageFrame
	given := &game.AgentImageFrame{
		Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
		Data:        []byte{0xAA, 0xBB, 0xCC},
		WidthPx:     1920,
		HeightPx:    1080,
		ScaleFactor: 1.0,
		WindowTitle: "Test Game",
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// when: unmarshal from protojson
	got := new(game.AgentImageFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify all fields preserved
	if got.GetEncoding() != game.ImageEncoding_IMAGE_ENCODING_PNG {
		t.Errorf("encoding: got %v, want %v", got.GetEncoding(), game.ImageEncoding_IMAGE_ENCODING_PNG)
	}
	if got.GetWidthPx() != 1920 {
		t.Errorf("widthPx: got %d, want %d", got.GetWidthPx(), 1920)
	}
	if got.GetHeightPx() != 1080 {
		t.Errorf("heightPx: got %d, want %d", got.GetHeightPx(), 1080)
	}
	if got.GetScaleFactor() != 1.0 {
		t.Errorf("scaleFactor: got %f, want %f", got.GetScaleFactor(), 1.0)
	}
	if got.GetWindowTitle() != "Test Game" {
		t.Errorf("windowTitle: got %q, want %q", got.GetWindowTitle(), "Test Game")
	}
}

func TestAgentProfileRoundtrip(t *testing.T) {
	// given: an AgentProfile with all fields populated
	given := &game.AgentProfile{
		Name:         "agentProfiles/default",
		Model:        "gpt-4",
		SystemPrompt: "You are a helpful game agent.",
		SkillNames:   []string{"gameplay-basics", "navigation"},
		McpNames:     []string{"screenshot-tool"},
		Enabled:      true,
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// when: unmarshal from protojson
	got := new(game.AgentProfile)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify all fields preserved
	if got.GetName() != "agentProfiles/default" {
		t.Errorf("name: got %q, want %q", got.GetName(), "agentProfiles/default")
	}
	if got.GetModel() != "gpt-4" {
		t.Errorf("model: got %q, want %q", got.GetModel(), "gpt-4")
	}
	if got.GetSystemPrompt() != "You are a helpful game agent." {
		t.Errorf("systemPrompt: got %q, want %q", got.GetSystemPrompt(), "You are a helpful game agent.")
	}
	if len(got.GetSkillNames()) != 2 || got.GetSkillNames()[0] != "gameplay-basics" || got.GetSkillNames()[1] != "navigation" {
		t.Errorf("skillNames: got %v, want %v", got.GetSkillNames(), []string{"gameplay-basics", "navigation"})
	}
	if len(got.GetMcpNames()) != 1 || got.GetMcpNames()[0] != "screenshot-tool" {
		t.Errorf("mcpNames: got %v, want %v", got.GetMcpNames(), []string{"screenshot-tool"})
	}
	if got.GetEnabled() != true {
		t.Errorf("enabled: got %v, want %v", got.GetEnabled(), true)
	}
}

func TestSkillRoundtrip(t *testing.T) {
	// given: a Skill with all fields populated
	given := &game.Skill{
		Name:    "skills/navigation",
		Content: "Navigate efficiently through the game world.",
		Enabled: true,
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// when: unmarshal from protojson
	got := new(game.Skill)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify all fields preserved
	if got.GetName() != "skills/navigation" {
		t.Errorf("name: got %q, want %q", got.GetName(), "skills/navigation")
	}
	if got.GetContent() != "Navigate efficiently through the game world." {
		t.Errorf("content: got %q, want %q", got.GetContent(), "Navigate efficiently through the game world.")
	}
	if got.GetEnabled() != true {
		t.Errorf("enabled: got %v, want %v", got.GetEnabled(), true)
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
