package game_test

import (
	"strings"
	"testing"
	"time"

	game "dominion/projects/game"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// proto_test.go exercises the Part-model proto contract introduced by the
// 015-desktop-agent-refinement refactor.
//
// The old frame types (AgentAckFrame, AgentEchoFrame/AgentTextFrame,
// AgentThinkingFrame, AgentOperationFrame, AgentMouseAction,
// AgentImageFrame) and the AgentFrame.invoke_id / AgentFrame.sequence and
// Message.type metadata are all REMOVED: the fields are `reserved` in
// game.proto, so the generated Go types have no accessors for them. The
// fact that this file compiles is itself the proof those symbols no
// longer exist. AgentFrame now carries exactly one payload drawn from a
// single oneof — a PartBlock of content OR one control signal
// (wait/warn/status). Message.content is the same PartBlock shape.

func TestAgentFrameContentTextRoundtrip(t *testing.T) {
	// given: an AgentFrame whose payload is a PartBlock of one TextPart
	given := &game.AgentFrame{
		SessionId: "sessions/test-text",
		FrameId:   "frame-text-001",
		Sender:    game.FrameSender_FRAME_SENDER_AGENT,
		CreateTime: &timestamppb.Timestamp{
			Seconds: time.Now().Unix(),
		},
		Payload: &game.AgentFrame_Content{
			Content: &game.PartBlock{
				Parts: []*game.Part{
					{Kind: &game.Part_Text{Text: &game.TextPart{Content: "Hello from agent"}}},
				},
			},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// then: verify camelCase JSON naming for the content payload
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"content"`) {
		t.Errorf("JSON output missing content oneof field, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"text"`) {
		t.Errorf("JSON output missing text part discriminator, got: %s", jsonStr)
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
	if got.GetSender() != game.FrameSender_FRAME_SENDER_AGENT {
		t.Errorf("sender: got %v, want %v", got.GetSender(), game.FrameSender_FRAME_SENDER_AGENT)
	}

	// then: verify the content PartBlock holds the TextPart
	content := got.GetContent()
	if content == nil {
		t.Fatal("GetContent() returned nil")
	}
	if len(content.GetParts()) != 1 {
		t.Fatalf("parts length: got %d, want 1", len(content.GetParts()))
	}
	text := content.GetParts()[0].GetText()
	if text == nil {
		t.Fatal("part[0].GetText() returned nil")
	}
	if text.GetContent() != "Hello from agent" {
		t.Errorf("text.content: got %q, want %q", text.GetContent(), "Hello from agent")
	}
}

func TestPartBlockMultiPartRoundtrip(t *testing.T) {
	// given: a PartBlock carrying multiple parts [TextPart, ImagePart]
	// (mirrors a user turn: caption + screenshot)
	given := &game.PartBlock{
		Parts: []*game.Part{
			{Kind: &game.Part_Text{Text: &game.TextPart{Content: "what is this?"}}},
			{Kind: &game.Part_Image{Image: &game.ImagePart{
				Encoding: game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:     []byte{0x89, 0x50, 0x4e, 0x47},
				WidthPx:  1920,
				HeightPx: 1080,
			}}},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// when: unmarshal from protojson
	got := new(game.PartBlock)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify both parts survived in order with correct discriminators
	parts := got.GetParts()
	if len(parts) != 2 {
		t.Fatalf("parts length: got %d, want 2", len(parts))
	}
	if parts[0].GetText() == nil {
		t.Error("part[0] is not a TextPart")
	} else if parts[0].GetText().GetContent() != "what is this?" {
		t.Errorf("part[0].text.content: got %q, want %q", parts[0].GetText().GetContent(), "what is this?")
	}
	img := parts[1].GetImage()
	if img == nil {
		t.Fatal("part[1] is not an ImagePart")
	}
	if img.GetEncoding() != game.ImageEncoding_IMAGE_ENCODING_PNG {
		t.Errorf("part[1].image.encoding: got %v, want %v", img.GetEncoding(), game.ImageEncoding_IMAGE_ENCODING_PNG)
	}
	if img.GetWidthPx() != 1920 {
		t.Errorf("part[1].image.widthPx: got %d, want %d", img.GetWidthPx(), 1920)
	}
	if img.GetHeightPx() != 1080 {
		t.Errorf("part[1].image.heightPx: got %d, want %d", img.GetHeightPx(), 1080)
	}
}

func TestAgentFrameContentMouseMoveRoundtrip(t *testing.T) {
	// given: an AgentFrame whose content PartBlock holds a MouseMovePart
	given := &game.AgentFrame{
		SessionId: "sessions/test-move",
		FrameId:   "frame-move-001",
		Payload: &game.AgentFrame_Content{
			Content: &game.PartBlock{
				Parts: []*game.Part{
					{Kind: &game.Part_MouseMove{MouseMove: &game.MouseMovePart{
						ToolId: "tool-move-001",
						XPx:    400,
						YPx:    300,
					}}},
				},
			},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// then: verify the mouseMove discriminator flattened by protojson
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"mouseMove"`) {
		t.Errorf("JSON output missing mouseMove part discriminator, got: %s", jsonStr)
	}

	// when: unmarshal from protojson
	got := new(game.AgentFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the MouseMovePart fields
	move := got.GetContent().GetParts()[0].GetMouseMove()
	if move == nil {
		t.Fatal("part[0].GetMouseMove() returned nil")
	}
	if move.GetToolId() != "tool-move-001" {
		t.Errorf("toolId: got %q, want %q", move.GetToolId(), "tool-move-001")
	}
	if move.GetXPx() != 400 {
		t.Errorf("xPx: got %d, want %d", move.GetXPx(), 400)
	}
	if move.GetYPx() != 300 {
		t.Errorf("yPx: got %d, want %d", move.GetYPx(), 300)
	}
}

func TestAgentFrameContentMouseClickRoundtrip(t *testing.T) {
	// given: an AgentFrame whose content PartBlock holds a MouseClickPart
	given := &game.AgentFrame{
		SessionId: "sessions/test-click",
		FrameId:   "frame-click-001",
		Payload: &game.AgentFrame_Content{
			Content: &game.PartBlock{
				Parts: []*game.Part{
					{Kind: &game.Part_MouseClick{MouseClick: &game.MouseClickPart{
						ToolId: "tool-click-001",
						Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
					}}},
				},
			},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// then: verify the mouseClick discriminator and the enum serialized as a
	// STRING name (protojson default for enums), proving oneof flattening.
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"mouseClick"`) {
		t.Errorf("JSON output missing mouseClick part discriminator, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, "MOUSE_CLICK_ACTION_LEFT_CLICK") {
		t.Errorf("JSON output missing click enum string name, got: %s", jsonStr)
	}

	// when: unmarshal from protojson
	got := new(game.AgentFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the MouseClickPart fields
	click := got.GetContent().GetParts()[0].GetMouseClick()
	if click == nil {
		t.Fatal("part[0].GetMouseClick() returned nil")
	}
	if click.GetToolId() != "tool-click-001" {
		t.Errorf("toolId: got %q, want %q", click.GetToolId(), "tool-click-001")
	}
	if click.GetClick() != game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK {
		t.Errorf("click: got %v, want %v", click.GetClick(), game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK)
	}
}

func TestAgentFrameContentToolResultRoundtrip(t *testing.T) {
	// given: an AgentFrame whose content PartBlock holds a ToolResultPart
	// with a nested ImagePart screenshot (the desktop-reported outcome of
	// a mouse operation)
	given := &game.AgentFrame{
		SessionId: "sessions/test-result",
		FrameId:   "frame-result-001",
		Payload: &game.AgentFrame_Content{
			Content: &game.PartBlock{
				Parts: []*game.Part{
					{Kind: &game.Part_ToolResult{ToolResult: &game.ToolResultPart{
						ToolId:  "tool-move-001",
						Status:  game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
						Message: "cursor moved",
						Screenshot: &game.ImagePart{
							Encoding: game.ImageEncoding_IMAGE_ENCODING_PNG,
							Data:     []byte{0xAA, 0xBB, 0xCC},
							WidthPx:  1920,
							HeightPx: 1080,
						},
					}}},
				},
			},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// then: verify the toolResult discriminator flattened by protojson
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"toolResult"`) {
		t.Errorf("JSON output missing toolResult part discriminator, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, "TOOL_RESULT_STATUS_SUCCEEDED") {
		t.Errorf("JSON output missing status enum string name, got: %s", jsonStr)
	}

	// when: unmarshal from protojson
	got := new(game.AgentFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the ToolResultPart fields incl. nested screenshot
	result := got.GetContent().GetParts()[0].GetToolResult()
	if result == nil {
		t.Fatal("part[0].GetToolResult() returned nil")
	}
	if result.GetToolId() != "tool-move-001" {
		t.Errorf("toolId: got %q, want %q", result.GetToolId(), "tool-move-001")
	}
	if result.GetStatus() != game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED {
		t.Errorf("status: got %v, want %v", result.GetStatus(), game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED)
	}
	if result.GetMessage() != "cursor moved" {
		t.Errorf("message: got %q, want %q", result.GetMessage(), "cursor moved")
	}
	shot := result.GetScreenshot()
	if shot == nil {
		t.Fatal("screenshot is nil")
	}
	if shot.GetWidthPx() != 1920 || shot.GetHeightPx() != 1080 {
		t.Errorf("screenshot dims: got %dx%d, want 1920x1080", shot.GetWidthPx(), shot.GetHeightPx())
	}
}

func TestAgentFrameWaitRoundtrip(t *testing.T) {
	// given: an AgentFrame with a wait control-signal payload
	given := &game.AgentFrame{
		SessionId: "sessions/test-wait",
		FrameId:   "frame-wait-001",
		Payload: &game.AgentFrame_Wait{
			Wait: &game.WaitSignal{Reason: "turn complete"},
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

	// then: verify the wait payload
	wait := got.GetWait()
	if wait == nil {
		t.Fatal("GetWait() returned nil")
	}
	if wait.GetReason() != "turn complete" {
		t.Errorf("wait.reason: got %q, want %q", wait.GetReason(), "turn complete")
	}
}

func TestAgentFrameWarnRoundtrip(t *testing.T) {
	// given: an AgentFrame with a warn control-signal payload
	given := &game.AgentFrame{
		SessionId: "sessions/test-warn",
		Payload: &game.AgentFrame_Warn{
			Warn: &game.WarnSignal{Message: "Stale sequence ignored", Code: "STALE_SEQUENCE"},
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

	// then: verify the warn payload
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

func TestAgentFrameStatusRoundtrip(t *testing.T) {
	// given: an AgentFrame with a status control-signal payload
	given := &game.AgentFrame{
		SessionId: "sessions/test-status",
		Payload: &game.AgentFrame_Status{
			Status: &game.StatusSignal{Status: "ready"},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// then: verify the status discriminator flattened by protojson
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"status"`) {
		t.Errorf("JSON output missing status oneof field, got: %s", jsonStr)
	}

	// when: unmarshal from protojson
	got := new(game.AgentFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the status payload
	status := got.GetStatus()
	if status == nil {
		t.Fatal("GetStatus() returned nil")
	}
	if status.GetStatus() != "ready" {
		t.Errorf("status.status: got %q, want %q", status.GetStatus(), "ready")
	}
}

func TestPartKindDiscriminatorFlattening(t *testing.T) {
	// protojson renders a oneof by emitting ONLY the active case's field
	// name (the discriminator) as the JSON key. This table asserts the
	// expected discriminator for every Part.kind variant.
	tests := []struct {
		name        string
		part        *game.Part
		wantKey     string
		wantMissing []string
	}{
		{
			name:        "text",
			part:        &game.Part{Kind: &game.Part_Text{Text: &game.TextPart{Content: "hi"}}},
			wantKey:     `"text"`,
			wantMissing: []string{`"thinking"`, `"image"`, `"mouseMove"`, `"mouseClick"`, `"toolResult"`},
		},
		{
			name:        "thinking",
			part:        &game.Part{Kind: &game.Part_Thinking{Thinking: &game.ThinkingPart{Content: "hmm"}}},
			wantKey:     `"thinking"`,
			wantMissing: []string{`"text"`, `"image"`, `"mouseMove"`, `"mouseClick"`, `"toolResult"`},
		},
		{
			name: "image",
			part: &game.Part{Kind: &game.Part_Image{Image: &game.ImagePart{Encoding: game.ImageEncoding_IMAGE_ENCODING_PNG}}},
			wantKey: `"image"`,
			wantMissing: []string{`"text"`, `"thinking"`, `"mouseMove"`, `"mouseClick"`, `"toolResult"`},
		},
		{
			name: "mouse_move",
			part: &game.Part{Kind: &game.Part_MouseMove{MouseMove: &game.MouseMovePart{ToolId: "t1", XPx: 1, YPx: 2}}},
			wantKey: `"mouseMove"`,
			wantMissing: []string{`"text"`, `"thinking"`, `"image"`, `"mouseClick"`, `"toolResult"`},
		},
		{
			name: "mouse_click",
			part: &game.Part{Kind: &game.Part_MouseClick{MouseClick: &game.MouseClickPart{ToolId: "t2", Click: game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK}}},
			wantKey: `"mouseClick"`,
			wantMissing: []string{`"text"`, `"thinking"`, `"image"`, `"mouseMove"`, `"toolResult"`},
		},
		{
			name: "tool_result",
			part: &game.Part{Kind: &game.Part_ToolResult{ToolResult: &game.ToolResultPart{ToolId: "t3", Status: game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED}}},
			wantKey: `"toolResult"`,
			wantMissing: []string{`"text"`, `"thinking"`, `"image"`, `"mouseMove"`, `"mouseClick"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes, err := protojson.Marshal(tt.part)
			if err != nil {
				t.Fatalf("protojson.Marshal() error: %v", err)
			}
			jsonStr := string(jsonBytes)
			if !strings.Contains(jsonStr, tt.wantKey) {
				t.Errorf("JSON missing discriminator %s, got: %s", tt.wantKey, jsonStr)
			}
			for _, absent := range tt.wantMissing {
				if strings.Contains(jsonStr, absent) {
					t.Errorf("JSON unexpectedly contains sibling discriminator %s, got: %s", absent, jsonStr)
				}
			}

			// round-trip back
			got := new(game.Part)
			if err := protojson.Unmarshal(jsonBytes, got); err != nil {
				t.Fatalf("protojson.Unmarshal() error: %v", err)
			}
			jsonBytes2, err := protojson.Marshal(got)
			if err != nil {
				t.Fatalf("re-marshal error: %v", err)
			}
			if string(jsonBytes2) != jsonStr {
				t.Errorf("round-trip not stable: got %s, want %s", string(jsonBytes2), jsonStr)
			}
		})
	}
}

func TestMessageContentRoundtrip(t *testing.T) {
	// given: a Message whose content is a PartBlock (NOT the old `type` +
	// text/image_data/operation oneof). Message.type is reserved, so the
	// serialized JSON must contain NO `type` field.
	given := &game.Message{
		Name:      "sessions/test/agent/messages/msg-001",
		MessageId: "msg-001",
		Sender:    game.FrameSender_FRAME_SENDER_AGENT,
		Content: &game.PartBlock{
			Parts: []*game.Part{
				{Kind: &game.Part_Thinking{Thinking: &game.ThinkingPart{Content: "Analyzing screenshot..."}}},
				{Kind: &game.Part_Text{Text: &game.TextPart{Content: "I will click the button."}}},
			},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// then: the JSON must NOT carry a `type` field (reserved & removed)
	jsonStr := string(jsonBytes)
	if strings.Contains(jsonStr, `"type"`) {
		t.Errorf("Message JSON unexpectedly contains reserved `type` field, got: %s", jsonStr)
	}
	// then: the JSON must NOT carry the old content oneof keys
	for _, old := range []string{`"imageData"`, `"operation"`, `"operationResult"`} {
		if strings.Contains(jsonStr, old) {
			t.Errorf("Message JSON unexpectedly contains old content oneof key %s, got: %s", old, jsonStr)
		}
	}

	// when: unmarshal from protojson
	got := new(game.Message)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the PartBlock content survived with both parts in order
	content := got.GetContent()
	if content == nil {
		t.Fatal("GetContent() returned nil")
	}
	parts := content.GetParts()
	if len(parts) != 2 {
		t.Fatalf("parts length: got %d, want 2", len(parts))
	}
	if parts[0].GetThinking() == nil {
		t.Error("part[0] is not a ThinkingPart")
	} else if parts[0].GetThinking().GetContent() != "Analyzing screenshot..." {
		t.Errorf("part[0].thinking.content: got %q, want %q", parts[0].GetThinking().GetContent(), "Analyzing screenshot...")
	}
	if parts[1].GetText() == nil {
		t.Error("part[1] is not a TextPart")
	} else if parts[1].GetText().GetContent() != "I will click the button." {
		t.Errorf("part[1].text.content: got %q, want %q", parts[1].GetText().GetContent(), "I will click the button.")
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

func TestAgentProfileRoundtrip(t *testing.T) {
	// given: an AgentProfile with all fields populated
	given := &game.AgentProfile{
		Name:        "agentProfiles/default",
		Model:       "gpt-4",
		SystemPrompt: "You are a helpful game agent.",
		SkillNames:  []string{"gameplay-basics", "navigation"},
		McpNames:    []string{"screenshot-tool"},
		Enabled:     true,
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
