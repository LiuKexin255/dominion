package game_test

import (
	"strings"
	"testing"
	"time"

	game "dominion/projects/game"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// proto_test.go exercises the content-model proto contract introduced by
// the 023-saolei-mcp-refine refactor: the single Part oneof is split into a
// display channel (MessagePart — text/thinking/image/tool_call/tool_result)
// and a control channel (FlowPart — mouse/keyboard operations + wait/warn/
// status signals). Connect frames are direction-split (spec
// 035-proto-contract-refine): UserFrame is the inbound transport unit,
// TeamFrame the outbound one; each payload is message_parts OR flow_parts.
// See specs/023-saolei-mcp-refine/contracts/content-model-contract.md §1..§6
// and specs/035-proto-contract-refine/contracts/frame-split.md §1..§5.
//
// The old frame types (AgentAckFrame, AgentEchoFrame/AgentTextFrame, ...),
// the AgentFrame envelope, and the FrameSender enum are all REMOVED: the
// generated Go types have no accessors for them. The fact that this file
// compiles is itself the proof those symbols no longer exist.
//
// The 059 team-mode session face (specs/059-agent-v2-team-mode/contracts/
// team-api.md §1) is asserted by the Team/TeamMember/TeamMessage/ChatEvent
// tests below: the former Agent/UpdateAgent/GetAgent/ListAgentMessages
// definitions no longer exist, and the v1 team/prompt declarations are
// removed from this generation unit (spec 059 US1).

func TestTeamFrameMessagePartsTextRoundtrip(t *testing.T) {
	// given: an outbound TeamFrame whose payload is a MessageParts of one
	// TextPart (display channel — agent display content, role AGENT per
	// specs/035-proto-contract-refine/research.md R3)
	given := &game.TeamFrame{
		SessionId:  "sessions/test-text",
		TemplateId: "saolei",
		FrameId:    "frame-text-001",
		CreateTime: &timestamppb.Timestamp{
			Seconds: time.Now().Unix(),
		},
		Agent: "player",
		Role:  game.MessageRole_MESSAGE_ROLE_AGENT,
		Payload: &game.TeamFrame_MessageParts{
			MessageParts: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: "Hello from agent"}}},
				},
			},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// then: verify camelCase JSON naming for the messageParts payload
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"messageParts"`) {
		t.Errorf("JSON output missing messageParts oneof field, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"text"`) {
		t.Errorf("JSON output missing text part discriminator, got: %s", jsonStr)
	}

	// when: unmarshal from protojson
	got := new(game.TeamFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify top-level fields
	if got.GetSessionId() != "sessions/test-text" {
		t.Errorf("sessionId: got %q, want %q", got.GetSessionId(), "sessions/test-text")
	}
	if got.GetTemplateId() != "saolei" {
		t.Errorf("templateId: got %q, want %q", got.GetTemplateId(), "saolei")
	}
	if got.GetFrameId() != "frame-text-001" {
		t.Errorf("frameId: got %q, want %q", got.GetFrameId(), "frame-text-001")
	}
	if got.GetAgent() != "player" {
		t.Errorf("agent: got %q, want %q", got.GetAgent(), "player")
	}
	if got.GetRole() != game.MessageRole_MESSAGE_ROLE_AGENT {
		t.Errorf("role: got %v, want %v", got.GetRole(), game.MessageRole_MESSAGE_ROLE_AGENT)
	}

	// then: verify the MessageParts payload holds the TextPart
	mp := got.GetMessageParts()
	if mp == nil {
		t.Fatal("GetMessageParts() returned nil")
	}
	if len(mp.GetParts()) != 1 {
		t.Fatalf("parts length: got %d, want 1", len(mp.GetParts()))
	}
	text := mp.GetParts()[0].GetText()
	if text == nil {
		t.Fatal("part[0].GetText() returned nil")
	}
	if text.GetContent() != "Hello from agent" {
		t.Errorf("text.content: got %q, want %q", text.GetContent(), "Hello from agent")
	}
}

func TestUserFrameMessagePartsTextRoundtrip(t *testing.T) {
	// given: an inbound UserFrame whose payload is a MessageParts of one
	// TextPart (user message content; no outbound-only envelope fields —
	// specs/035-proto-contract-refine/contracts/frame-split.md §2)
	given := &game.UserFrame{
		SessionId:  "sessions/test-user",
		TemplateId: "saolei",
		Agent:      "player",
		Payload: &game.UserFrame_MessageParts{
			MessageParts: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: "Hello from user"}}},
				},
			},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// then: verify camelCase JSON naming for the messageParts payload
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"messageParts"`) {
		t.Errorf("JSON output missing messageParts oneof field, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"text"`) {
		t.Errorf("JSON output missing text part discriminator, got: %s", jsonStr)
	}
	// UserFrame carries no frame_id/create_time/sender — the server does not
	// consume them on the inbound direction.
	for _, absent := range []string{`"frameId"`, `"createTime"`, `"sender"`} {
		if strings.Contains(jsonStr, absent) {
			t.Errorf("JSON output unexpectedly contains %s (UserFrame excludes outbound-only envelope fields), got: %s", absent, jsonStr)
		}
	}

	// when: unmarshal from protojson
	got := new(game.UserFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify top-level fields
	if got.GetSessionId() != "sessions/test-user" {
		t.Errorf("sessionId: got %q, want %q", got.GetSessionId(), "sessions/test-user")
	}
	if got.GetTemplateId() != "saolei" {
		t.Errorf("templateId: got %q, want %q", got.GetTemplateId(), "saolei")
	}
	if got.GetAgent() != "player" {
		t.Errorf("agent: got %q, want %q", got.GetAgent(), "player")
	}

	// then: verify the MessageParts payload holds the TextPart
	mp := got.GetMessageParts()
	if mp == nil {
		t.Fatal("GetMessageParts() returned nil")
	}
	if len(mp.GetParts()) != 1 {
		t.Fatalf("parts length: got %d, want 1", len(mp.GetParts()))
	}
	text := mp.GetParts()[0].GetText()
	if text == nil {
		t.Fatal("part[0].GetText() returned nil")
	}
	if text.GetContent() != "Hello from user" {
		t.Errorf("text.content: got %q, want %q", text.GetContent(), "Hello from user")
	}
}

func TestMessagePartsMultiPartRoundtrip(t *testing.T) {
	// given: a MessageParts carrying multiple display parts [TextPart, ImagePart]
	// (mirrors a user turn: caption + screenshot)
	given := &game.MessageParts{
		Parts: []*game.MessagePart{
			{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: "what is this?"}}},
			{Kind: &game.MessagePart_Image{Image: &game.ImagePart{
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
	got := new(game.MessageParts)
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

func TestTeamFrameFlowPartsMouseMoveRoundtrip(t *testing.T) {
	// given: an outbound TeamFrame whose flow_parts payload holds a
	// MouseMovePart (control channel — operation request dispatched by the
	// agent's OperationBridge)
	given := &game.TeamFrame{
		SessionId:  "sessions/test-move",
		TemplateId: "saolei",
		FrameId:    "frame-move-001",
		Payload: &game.TeamFrame_FlowParts{
			FlowParts: &game.FlowParts{
				Parts: []*game.FlowPart{
					{Kind: &game.FlowPart_MouseMove{MouseMove: &game.MouseMovePart{
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
	got := new(game.TeamFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the MouseMovePart fields
	move := got.GetFlowParts().GetParts()[0].GetMouseMove()
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

func TestTeamFrameFlowPartsMouseClickRoundtrip(t *testing.T) {
	// given: an outbound TeamFrame whose flow_parts payload holds a
	// MouseClickPart (control channel)
	given := &game.TeamFrame{
		SessionId:  "sessions/test-click",
		TemplateId: "saolei",
		FrameId:    "frame-click-001",
		Payload: &game.TeamFrame_FlowParts{
			FlowParts: &game.FlowParts{
				Parts: []*game.FlowPart{
					{Kind: &game.FlowPart_MouseClick{MouseClick: &game.MouseClickPart{
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
	got := new(game.TeamFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the MouseClickPart fields
	click := got.GetFlowParts().GetParts()[0].GetMouseClick()
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

func TestTeamFrameMessagePartsToolResultRoundtrip(t *testing.T) {
	// given: an outbound TeamFrame whose message_parts payload holds a
	// ToolResultPart with a nested ImagePart screenshot (display channel — the
	// desktop-reported outcome rendered as a conversation entry)
	given := &game.TeamFrame{
		SessionId:  "sessions/test-result",
		TemplateId: "saolei",
		FrameId:    "frame-result-001",
		Role:       game.MessageRole_MESSAGE_ROLE_AGENT,
		Payload: &game.TeamFrame_MessageParts{
			MessageParts: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_ToolResult{ToolResult: &game.ToolResultPart{
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
	got := new(game.TeamFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the ToolResultPart fields incl. nested screenshot
	result := got.GetMessageParts().GetParts()[0].GetToolResult()
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

func TestPartCompletionEnumRoundtrip(t *testing.T) {
	// given: content parts carrying the 044 "interrupted" marker on the wire
	// layer — the PartCompletion enum field (specs/044-llm-stall-recovery-fix/
	// contracts/desktop-rendering-contract.md §3; data-model.md §4.2): an
	// interrupted text part, an interrupted thinking part, and a normal
	// (UNSPECIFIED) text part.
	tests := []struct {
		name     string
		part     *game.MessagePart
		wantJSON string // enum-name string the protojson output must contain; "" = must omit the field
		wantEnum game.PartCompletion
	}{
		{
			name: "interrupted text part",
			part: &game.MessagePart{Kind: &game.MessagePart_Text{Text: &game.TextPart{
				Content:    "cut off mid",
				Completion: game.PartCompletion_PART_COMPLETION_INTERRUPTED,
			}}},
			wantJSON: "PART_COMPLETION_INTERRUPTED",
			wantEnum: game.PartCompletion_PART_COMPLETION_INTERRUPTED,
		},
		{
			name: "interrupted thinking part",
			part: &game.MessagePart{Kind: &game.MessagePart_Thinking{Thinking: &game.ThinkingPart{
				Content:    "cut off mid",
				Completion: game.PartCompletion_PART_COMPLETION_INTERRUPTED,
			}}},
			wantJSON: "PART_COMPLETION_INTERRUPTED",
			wantEnum: game.PartCompletion_PART_COMPLETION_INTERRUPTED,
		},
		{
			name: "normal text part omits the zero-value completion field",
			part: &game.MessagePart{Kind: &game.MessagePart_Text{Text: &game.TextPart{
				Content: "complete reply",
			}}},
			wantEnum: game.PartCompletion_PART_COMPLETION_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when: marshal to protojson, then unmarshal back
			jsonBytes, err := protojson.Marshal(tt.part)
			if err != nil {
				t.Fatalf("protojson.Marshal() error: %v", err)
			}
			jsonStr := string(jsonBytes)
			if tt.wantJSON != "" && !strings.Contains(jsonStr, tt.wantJSON) {
				t.Errorf("JSON output missing completion enum name %q, got: %s", tt.wantJSON, jsonStr)
			}
			if tt.wantJSON == "" && strings.Contains(jsonStr, "completion") {
				t.Errorf("JSON output should omit the zero-value completion field, got: %s", jsonStr)
			}

			got := new(game.MessagePart)
			if err := protojson.Unmarshal(jsonBytes, got); err != nil {
				t.Fatalf("protojson.Unmarshal() error: %v", err)
			}

			// then: the round-tripped part carries the enum value
			var gotCompletion game.PartCompletion
			switch {
			case got.GetText() != nil:
				gotCompletion = got.GetText().GetCompletion()
			case got.GetThinking() != nil:
				gotCompletion = got.GetThinking().GetCompletion()
			}
			if gotCompletion != tt.wantEnum {
				t.Errorf("completion: got %v, want %v", gotCompletion, tt.wantEnum)
			}
		})
	}
}

func TestUserFrameFlowPartsFlowResultRoundtrip(t *testing.T) {
	// given: an inbound UserFrame whose flow_parts payload carries a
	// FlowResultPart (the desktop's operation-execution outcome reported on
	// the control channel — specs/025-desktop-image-state-refine/contracts/
	// flow-result-contract.md)
	given := &game.UserFrame{
		SessionId:  "sessions/test-result",
		TemplateId: "saolei",
		Payload: &game.UserFrame_FlowParts{
			FlowParts: &game.FlowParts{
				Parts: []*game.FlowPart{
					{Kind: &game.FlowPart_FlowResult{FlowResult: &game.FlowResultPart{
						ToolId:  "tool-move-001",
						Status:  game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
						Message: "ok",
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

	// then: verify the flowResult discriminator flattened by protojson
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"flowResult"`) {
		t.Errorf("JSON output missing flowResult part discriminator, got: %s", jsonStr)
	}

	// when: unmarshal from protojson
	got := new(game.UserFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the FlowResultPart fields
	result := got.GetFlowParts().GetParts()[0].GetFlowResult()
	if result == nil {
		t.Fatal("part[0].GetFlowResult() returned nil")
	}
	if result.GetToolId() != "tool-move-001" {
		t.Errorf("toolId: got %q, want %q", result.GetToolId(), "tool-move-001")
	}
	if result.GetStatus() != game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED {
		t.Errorf("status: got %v, want %v", result.GetStatus(), game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED)
	}
}

func TestTeamFrameWaitRoundtrip(t *testing.T) {
	// given: an outbound TeamFrame whose flow_parts payload carries a
	// WaitSignal (control channel — wait is a FlowPart kind per spec 023 C3 /
	// FR-003)
	given := &game.TeamFrame{
		SessionId:  "sessions/test-wait",
		TemplateId: "saolei",
		FrameId:    "frame-wait-001",
		Payload: &game.TeamFrame_FlowParts{
			FlowParts: &game.FlowParts{
				Parts: []*game.FlowPart{
					{Kind: &game.FlowPart_Wait{Wait: &game.WaitSignal{Reason: "turn complete"}}},
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
	got := new(game.TeamFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the wait FlowPart
	wait := got.GetFlowParts().GetParts()[0].GetWait()
	if wait == nil {
		t.Fatal("part[0].GetWait() returned nil")
	}
	if wait.GetReason() != "turn complete" {
		t.Errorf("wait.reason: got %q, want %q", wait.GetReason(), "turn complete")
	}
}

func TestTeamFrameWarnRoundtrip(t *testing.T) {
	// given: an outbound TeamFrame whose flow_parts payload carries a
	// WarnSignal (control channel — warn is a FlowPart kind per spec 023 C3 /
	// FR-003)
	given := &game.TeamFrame{
		SessionId:  "sessions/test-warn",
		TemplateId: "saolei",
		Payload: &game.TeamFrame_FlowParts{
			FlowParts: &game.FlowParts{
				Parts: []*game.FlowPart{
					{Kind: &game.FlowPart_Warn{Warn: &game.WarnSignal{Message: "Stale sequence ignored", Code: "STALE_SEQUENCE"}}},
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
	got := new(game.TeamFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the warn FlowPart
	warn := got.GetFlowParts().GetParts()[0].GetWarn()
	if warn == nil {
		t.Fatal("part[0].GetWarn() returned nil")
	}
	if warn.GetMessage() != "Stale sequence ignored" {
		t.Errorf("warn.message: got %q, want %q", warn.GetMessage(), "Stale sequence ignored")
	}
	if warn.GetCode() != "STALE_SEQUENCE" {
		t.Errorf("warn.code: got %q, want %q", warn.GetCode(), "STALE_SEQUENCE")
	}
}

func TestTeamFrameStatusRoundtrip(t *testing.T) {
	// given: an outbound TeamFrame whose flow_parts payload carries a
	// StatusSignal (control channel — status is a FlowPart kind per spec 023
	// C3 / FR-003)
	given := &game.TeamFrame{
		SessionId:  "sessions/test-status",
		TemplateId: "saolei",
		Payload: &game.TeamFrame_FlowParts{
			FlowParts: &game.FlowParts{
				Parts: []*game.FlowPart{
					{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
				},
			},
		},
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}

	// then: verify the flowParts discriminator flattened by protojson
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"flowParts"`) {
		t.Errorf("JSON output missing flowParts oneof field, got: %s", jsonStr)
	}

	// when: unmarshal from protojson
	got := new(game.TeamFrame)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: verify the status FlowPart
	status := got.GetFlowParts().GetParts()[0].GetStatus()
	if status == nil {
		t.Fatal("part[0].GetStatus() returned nil")
	}
	if status.GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE {
		t.Errorf("status.status: got %q, want %q", status.GetStatus(), game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE)
	}
}

func TestMessagePartKindDiscriminatorFlattening(t *testing.T) {
	// protojson renders a oneof by emitting ONLY the active case's field
	// name (the discriminator) as the JSON key. This table asserts the
	// expected discriminator for every display MessagePart.kind variant
	// (specs/023-saolei-mcp-refine/contracts/content-model-contract.md §2).
	// The 023 split moved the mouse operations out of this oneof into
	// FlowPart (covered by TestFlowPartKindDiscriminatorFlattening).
	tests := []struct {
		name        string
		part        *game.MessagePart
		wantKey     string
		wantMissing []string
	}{
		{
			name:        "text",
			part:        &game.MessagePart{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: "hi"}}},
			wantKey:     `"text"`,
			wantMissing: []string{`"thinking"`, `"image"`, `"toolCall"`, `"toolResult"`},
		},
		{
			name:        "thinking",
			part:        &game.MessagePart{Kind: &game.MessagePart_Thinking{Thinking: &game.ThinkingPart{Content: "hmm"}}},
			wantKey:     `"thinking"`,
			wantMissing: []string{`"text"`, `"image"`, `"toolCall"`, `"toolResult"`},
		},
		{
			name:        "image",
			part:        &game.MessagePart{Kind: &game.MessagePart_Image{Image: &game.ImagePart{Encoding: game.ImageEncoding_IMAGE_ENCODING_PNG}}},
			wantKey:     `"image"`,
			wantMissing: []string{`"text"`, `"thinking"`, `"toolCall"`, `"toolResult"`},
		},
		{
			name:        "tool_result",
			part:        &game.MessagePart{Kind: &game.MessagePart_ToolResult{ToolResult: &game.ToolResultPart{ToolId: "t3", Status: game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED}}},
			wantKey:     `"toolResult"`,
			wantMissing: []string{`"text"`, `"thinking"`, `"image"`, `"toolCall"`},
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
			got := new(game.MessagePart)
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

func TestFlowPartKindDiscriminatorFlattening(t *testing.T) {
	// protojson renders a oneof by emitting ONLY the active case's field
	// name (the discriminator) as the JSON key. This table asserts the
	// expected discriminator for the control FlowPart.kind operation
	// variants (specs/023-saolei-mcp-refine/contracts/content-model-contract.md §2).
	// The mouse operations moved here from the removed Part oneof; signal
	// kinds (wait/warn/status) are covered by the TeamFrame signal
	// roundtrips above.
	tests := []struct {
		name        string
		part        *game.FlowPart
		wantKey     string
		wantMissing []string
	}{
		{
			name:        "mouse_move",
			part:        &game.FlowPart{Kind: &game.FlowPart_MouseMove{MouseMove: &game.MouseMovePart{ToolId: "t1", XPx: 1, YPx: 2}}},
			wantKey:     `"mouseMove"`,
			wantMissing: []string{`"mouseClick"`, `"keyboardPress"`, `"mouseMoveAndClick"`, `"wait"`, `"warn"`, `"status"`},
		},
		{
			name:        "mouse_click",
			part:        &game.FlowPart{Kind: &game.FlowPart_MouseClick{MouseClick: &game.MouseClickPart{ToolId: "t2", Click: game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK}}},
			wantKey:     `"mouseClick"`,
			wantMissing: []string{`"mouseMove"`, `"keyboardPress"`, `"mouseMoveAndClick"`, `"wait"`, `"warn"`, `"status"`},
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
			got := new(game.FlowPart)
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

func TestPresetRoleRoundtrip(t *testing.T) {
	// given: Preset resources of both pools (the 059 role extension,
	// specs/059-agent-v2-team-mode/contracts/preset-api.md §2): role is a
	// scene vocabulary string on the wire (2026-09-10 generic-primitive
	// ruling) and persona stays the user-editable persona carrier.
	tests := []struct {
		name     string
		preset   *game.Preset
		wantRole string
	}{
		{
			name: "player pool preset",
			preset: &game.Preset{
				Name:    "templates/saolei/presets/p1",
				Persona: "你是扫雷 player。",
				Role:    "player",
			},
			wantRole: "player",
		},
		{
			name: "planner pool preset",
			preset: &game.Preset{
				Name:    "templates/saolei/presets/p2",
				Persona: "你是扫雷 planner。",
				Role:    "planner",
			},
			wantRole: "planner",
		},
		{
			name: "scene-agnostic role string passes through",
			preset: &game.Preset{
				Name: "templates/saolei/presets/p3",
				Role: "referee",
			},
			wantRole: "referee",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when: marshal to protojson, then unmarshal back
			jsonBytes, err := protojson.Marshal(tt.preset)
			if err != nil {
				t.Fatalf("protojson.Marshal() error: %v", err)
			}
			jsonStr := string(jsonBytes)
			if !strings.Contains(jsonStr, `"role"`) {
				t.Errorf("JSON output missing role field, got: %s", jsonStr)
			}
			if !strings.Contains(jsonStr, tt.wantRole) {
				t.Errorf("JSON output missing role string %q, got: %s", tt.wantRole, jsonStr)
			}

			got := new(game.Preset)
			if err := protojson.Unmarshal(jsonBytes, got); err != nil {
				t.Fatalf("protojson.Unmarshal() error: %v", err)
			}
			if got.GetRole() != tt.wantRole {
				t.Errorf("role: got %q, want %q", got.GetRole(), tt.wantRole)
			}
			if got.GetPersona() != tt.preset.GetPersona() {
				t.Errorf("persona: got %q, want %q", got.GetPersona(), tt.preset.GetPersona())
			}
		})
	}
}

func TestCreatePresetRequestCarriesRole(t *testing.T) {
	// given: a CreatePresetRequest whose role decides the pool the preset is
	// created into (create 必填、不可变 — preset-api.md §2); the role rides
	// the REQUEST as a scene vocabulary string (AIP-133 user-specified fields
	// on the request message).
	given := &game.CreatePresetRequest{
		Parent:   "templates/saolei",
		PresetId: "p1",
		Preset:   &game.Preset{Persona: "persona"},
		Role:     "planner",
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}
	if !strings.Contains(string(jsonBytes), `"planner"`) {
		t.Errorf("JSON output missing request role string, got: %s", string(jsonBytes))
	}

	// then: unmarshal carries the role back
	got := new(game.CreatePresetRequest)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}
	if got.GetRole() != "planner" {
		t.Errorf("role: got %q, want %q", got.GetRole(), "planner")
	}
	if got.GetPresetId() != "p1" {
		t.Errorf("presetId: got %q, want %q", got.GetPresetId(), "p1")
	}
}

func TestListPresetsRequestRoleFilter(t *testing.T) {
	// given: a ListPresetsRequest with the optional role filter set as a
	// scene vocabulary string
	given := &game.ListPresetsRequest{
		Parent:   "templates/saolei",
		PageSize: 10,
		Role:     "player",
	}

	// when: marshal to protojson, then unmarshal back
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}
	if !strings.Contains(string(jsonBytes), `"player"`) {
		t.Errorf("JSON output missing role filter string, got: %s", string(jsonBytes))
	}

	got := new(game.ListPresetsRequest)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: the filter survives the round trip; the empty value means no
	// filtering and is omitted by protojson
	if got.GetRole() != "player" {
		t.Errorf("role: got %q, want %q", got.GetRole(), "player")
	}
	unfiltered := protojson.Format(new(game.ListPresetsRequest))
	if strings.Contains(unfiltered, "role") {
		t.Errorf("empty request JSON should omit the role filter, got: %s", unfiltered)
	}
}

func TestTeamResourceRoundtrip(t *testing.T) {
	// given: the team singleton with its AIP-156 resource name and the
	// members list — caller-supplied member configurations and materialized
	// snapshots share one shape ({role, preset, model}); role is a scene
	// vocabulary string and system_prompt is output-only
	// (specs/059-agent-v2-team-mode/data-model.md §2)
	given := &game.Team{
		Name:             "templates/saolei/sessions/s1/team",
		DesktopConnected: true,
		CreateTime:       timestamppb.New(time.Unix(1000, 0)),
		UpdateTime:       timestamppb.New(time.Unix(2000, 0)),
		Members: []*game.TeamMember{
			{
				Name:   "templates/saolei/sessions/s1/team/members/player",
				Role:   "player",
				Preset: "templates/saolei/presets/p1",
				Model:  "glm-5.3",
			},
			{
				Name:         "templates/saolei/sessions/s1/team/members/planner",
				Role:         "planner",
				Preset:       "templates/saolei/presets/p2",
				Model:        "glm-5.5",
				SystemPrompt: "你是扫雷 planner。",
			},
		},
	}

	// when: marshal to protojson, then unmarshal back
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}
	jsonStr := string(jsonBytes)
	for _, want := range []string{
		`"members"`,
		`"role"`,
		`"player"`,
		`"planner"`,
		`"preset"`,
		`"model"`,
		`"desktopConnected"`,
		`"systemPrompt"`,
	} {
		if !strings.Contains(jsonStr, want) {
			t.Errorf("JSON output missing %s, got: %s", want, jsonStr)
		}
	}
	// The removed scene-specific scalar fields must not reappear.
	for _, absent := range []string{"playerPreset", "plannerPreset", "playerModel", "plannerModel"} {
		if strings.Contains(jsonStr, absent) {
			t.Errorf("JSON output unexpectedly contains removed field %s, got: %s", absent, jsonStr)
		}
	}

	got := new(game.Team)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: the resource name, the members list, and the runtime state
	// survive the round trip
	if got.GetName() != given.GetName() {
		t.Errorf("name: got %q, want %q", got.GetName(), given.GetName())
	}
	if !got.GetDesktopConnected() {
		t.Error("desktopConnected: got false, want true")
	}
	members := got.GetMembers()
	if len(members) != 2 {
		t.Fatalf("members length: got %d, want 2", len(members))
	}
	if members[0].GetRole() != "player" || members[0].GetPreset() != "templates/saolei/presets/p1" {
		t.Errorf("members[0]: got role=%q preset=%q", members[0].GetRole(), members[0].GetPreset())
	}
	if members[1].GetRole() != "planner" || members[1].GetModel() != "glm-5.5" {
		t.Errorf("members[1]: got role=%q model=%q", members[1].GetRole(), members[1].GetModel())
	}
	if members[1].GetSystemPrompt() != "你是扫雷 planner。" {
		t.Errorf("members[1].systemPrompt: got %q, want the planner prompt", members[1].GetSystemPrompt())
	}
}

func TestTeamMemberInputShape(t *testing.T) {
	// given: a caller-supplied member configuration — role + preset required,
	// model optional, name/system_prompt left to the server (the same message
	// carries both the input and the output shape)
	given := &game.TeamMember{
		Role:   "player",
		Preset: "templates/saolei/presets/p1",
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"role"`) || !strings.Contains(jsonStr, `"preset"`) {
		t.Errorf("member input JSON missing role/preset, got: %s", jsonStr)
	}
	for _, absent := range []string{"name", "systemPrompt", "model"} {
		if strings.Contains(jsonStr, absent) {
			t.Errorf("member input JSON unexpectedly carries unset field %s, got: %s", absent, jsonStr)
		}
	}

	// then: the input round-trips
	got := new(game.TeamMember)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}
	if got.GetRole() != "player" || got.GetPreset() != "templates/saolei/presets/p1" {
		t.Errorf("member: got role=%q preset=%q", got.GetRole(), got.GetPreset())
	}
}

func TestTeamMessageRoundtrip(t *testing.T) {
	// given: the team-level frame payload variants — a user entry and a
	// member entry carrying the merge anchor (data-model.md §2 TeamMessage;
	// the same shape rides the ChatEvent team_message frame); member is a
	// scene string with the reserved "user" value
	tests := []struct {
		name       string
		message    *game.TeamMessage
		wantMember string
		wantText   string
	}{
		{
			name: "user entry",
			message: &game.TeamMessage{
				Member: "user",
				Message: &game.HistoryMessage{
					MessageId: "m1",
					Role:      game.Role_ROLE_USER,
					Blocks:    []*game.ContentBlock{{Kind: &game.ContentBlock_Text{Text: &game.TextBlock{Content: "开始一局"}}}},
				},
				Seq: 1,
			},
			wantMember: "user",
			wantText:   "开始一局",
		},
		{
			name: "member entry",
			message: &game.TeamMessage{
				Member: "planner",
				Message: &game.HistoryMessage{
					MessageId: "m2",
					Role:      game.Role_ROLE_AGENT,
					Blocks:    []*game.ContentBlock{{Kind: &game.ContentBlock_Text{Text: &game.TextBlock{Content: "开局策略"}}}},
				},
				Seq: 2,
			},
			wantMember: "planner",
			wantText:   "开局策略",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when: marshal to protojson, then unmarshal back
			jsonBytes, err := protojson.Marshal(tt.message)
			if err != nil {
				t.Fatalf("protojson.Marshal() error: %v", err)
			}
			if !strings.Contains(string(jsonBytes), `"seq"`) {
				t.Errorf("JSON output missing seq anchor, got: %s", string(jsonBytes))
			}

			got := new(game.TeamMessage)
			if err := protojson.Unmarshal(jsonBytes, got); err != nil {
				t.Fatalf("protojson.Unmarshal() error: %v", err)
			}

			// then: producer, native message, and anchor survive
			if got.GetMember() != tt.wantMember {
				t.Errorf("member: got %q, want %q", got.GetMember(), tt.wantMember)
			}
			if got.GetSeq() != tt.message.GetSeq() {
				t.Errorf("seq: got %d, want %d", got.GetSeq(), tt.message.GetSeq())
			}
			if text := got.GetMessage().GetBlocks()[0].GetText().GetContent(); text != tt.wantText {
				t.Errorf("message text: got %q, want %q", text, tt.wantText)
			}
		})
	}
}

func TestMemberViewMessageRoundtrip(t *testing.T) {
	// given: a member-view entry whose message came from another member's
	// relay (sender="player" in the planner's view; web-views.md §4 renders it
	// as `user: [player]…`)
	given := &game.MemberViewMessage{
		Message: &game.HistoryMessage{
			MessageId: "m3",
			Role:      game.Role_ROLE_USER,
			Blocks:    []*game.ContentBlock{{Kind: &game.ContentBlock_Text{Text: &game.TextBlock{Content: "[player] 已点击 (3,4)"}}}},
		},
		Sender: "player",
	}

	// when: marshal to protojson, then unmarshal back
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}
	if !strings.Contains(string(jsonBytes), `"sender"`) {
		t.Errorf("JSON output missing sender field, got: %s", string(jsonBytes))
	}

	got := new(game.MemberViewMessage)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}

	// then: the sender annotation and the relayed body survive
	if got.GetSender() != "player" {
		t.Errorf("sender: got %q, want %q", got.GetSender(), "player")
	}
	if text := got.GetMessage().GetBlocks()[0].GetText().GetContent(); text != "[player] 已点击 (3,4)" {
		t.Errorf("message text: got %q, want the relayed body", text)
	}
}

func TestChatEventTeamFrames(t *testing.T) {
	// given: the two frame classes of the team stream (contracts/team-api.md
	// §3.2) — a member event frame carrying the outer member label (a scene
	// role string), and a team-level team_message frame carrying the merged
	// entry
	memberFrame := &game.ChatEvent{
		Session: "templates/saolei/sessions/s1",
		TurnId:  "turn-1",
		Member:  "player",
		Payload: &game.ChatEvent_Delta{Delta: &game.BlockDeltaEvent{Index: 0, Text: "hi"}},
	}
	teamFrame := &game.ChatEvent{
		Session: "templates/saolei/sessions/s1",
		Payload: &game.ChatEvent_TeamMessage{TeamMessage: &game.TeamMessage{
			Member: "user",
			Message: &game.HistoryMessage{
				MessageId: "m1",
				Role:      game.Role_ROLE_USER,
				Blocks:    []*game.ContentBlock{{Kind: &game.ContentBlock_Text{Text: &game.TextBlock{Content: "开始"}}}},
			},
			Seq: 1,
		}},
	}

	// when/then: the member frame renders the member label and the delta arm
	memberJSON, err := protojson.Marshal(memberFrame)
	if err != nil {
		t.Fatalf("protojson.Marshal(member frame) error: %v", err)
	}
	if !strings.Contains(string(memberJSON), `"player"`) || !strings.Contains(string(memberJSON), `"delta"`) {
		t.Errorf("member frame JSON missing member/delta, got: %s", string(memberJSON))
	}

	// when/then: the team frame renders the teamMessage arm and no outer
	// member label (team-level frames do not set the outer member field)
	teamJSON, err := protojson.Marshal(teamFrame)
	if err != nil {
		t.Fatalf("protojson.Marshal(team frame) error: %v", err)
	}
	if !strings.Contains(string(teamJSON), `"teamMessage"`) {
		t.Errorf("team frame JSON missing teamMessage arm, got: %s", string(teamJSON))
	}

	// then: both frames round-trip back to their payload arms
	var gotMember game.ChatEvent
	if err := protojson.Unmarshal(memberJSON, &gotMember); err != nil {
		t.Fatalf("protojson.Unmarshal(member frame) error: %v", err)
	}
	if gotMember.GetMember() != "player" || gotMember.GetDelta() == nil {
		t.Errorf("member frame round trip: member=%q delta=%v", gotMember.GetMember(), gotMember.GetDelta())
	}
	var gotTeam game.ChatEvent
	if err := protojson.Unmarshal(teamJSON, &gotTeam); err != nil {
		t.Fatalf("protojson.Unmarshal(team frame) error: %v", err)
	}
	if gotTeam.GetMember() != "" {
		t.Errorf("team frame outer member: got %q, want unset (team-level frame)", gotTeam.GetMember())
	}
	if gotTeam.GetTeamMessage().GetSeq() != 1 {
		t.Errorf("team frame round trip: seq=%d, want 1", gotTeam.GetTeamMessage().GetSeq())
	}
}

func TestUpdateTeamRequestCarriesMembers(t *testing.T) {
	// given: an UpdateTeamRequest whose team body carries the members list
	// (the identity rides the URL path; AIP-134)
	given := &game.UpdateTeamRequest{
		Team: &game.Team{
			Name: "templates/saolei/sessions/s1/team",
			Members: []*game.TeamMember{
				{Role: "player", Preset: "templates/saolei/presets/p1"},
				{Role: "planner", Preset: "templates/saolei/presets/p2"},
			},
		},
		AllowMissing: true,
	}

	// when: marshal to protojson
	jsonBytes, err := protojson.Marshal(given)
	if err != nil {
		t.Fatalf("protojson.Marshal() error: %v", err)
	}
	jsonStr := string(jsonBytes)
	for _, want := range []string{`"team"`, `"members"`, `"player"`, `"planner"`, `"allowMissing"`} {
		if !strings.Contains(jsonStr, want) {
			t.Errorf("JSON output missing %s, got: %s", want, jsonStr)
		}
	}

	// then: the request round-trips
	got := new(game.UpdateTeamRequest)
	if err := protojson.Unmarshal(jsonBytes, got); err != nil {
		t.Fatalf("protojson.Unmarshal() error: %v", err)
	}
	if members := got.GetTeam().GetMembers(); len(members) != 2 || members[1].GetPreset() != "templates/saolei/presets/p2" {
		t.Errorf("team.members: got %+v, want the planner preset", members)
	}
	if !got.GetAllowMissing() {
		t.Error("allowMissing: got false, want true")
	}
}
