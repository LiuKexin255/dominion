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
// TeamFrame the outbound one; each payload is message_parts OR flow_parts;
// Message.content is MessageParts (display only). See
// specs/023-saolei-mcp-refine/contracts/content-model-contract.md §1..§6 and
// specs/035-proto-contract-refine/contracts/frame-split.md §1..§5.
//
// The old frame types (AgentAckFrame, AgentEchoFrame/AgentTextFrame, ...),
// the AgentFrame envelope, and the FrameSender enum are all REMOVED: the
// generated Go types have no accessors for them. The fact that this file
// compiles is itself the proof those symbols no longer exist.

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

func TestMessageContentRoundtrip(t *testing.T) {
	// given: a Message whose content is a MessageParts (display blocks only)
	// and whose role is the MessageRole enum (replaced the FrameSender sender
	// field — FR-020). Message.type is reserved, so the serialized JSON must
	// contain NO `type` field. Control FlowParts can never appear here
	// (spec 023 FR-004).
	given := &game.Message{
		Name:      "sessions/test/agent/messages/msg-001",
		MessageId: "msg-001",
		Role:      game.MessageRole_MESSAGE_ROLE_AGENT,
		Content: &game.MessageParts{
			Parts: []*game.MessagePart{
				{Kind: &game.MessagePart_Thinking{Thinking: &game.ThinkingPart{Content: "Analyzing screenshot..."}}},
				{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: "I will click the button."}}},
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

	// then: verify the MessageRole round-trips
	if got.GetRole() != game.MessageRole_MESSAGE_ROLE_AGENT {
		t.Errorf("role: got %v, want %v", got.GetRole(), game.MessageRole_MESSAGE_ROLE_AGENT)
	}

	// then: verify the MessageParts content survived with both parts in order
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
