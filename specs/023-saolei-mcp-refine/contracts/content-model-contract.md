# Contract: Proto Content Model (MessagePart / FlowPart split)

**Feature**: 023-saolei-mcp-refine
**Date**: 2026-07-25
**Status**: Phase 1 contract — MUST be satisfied by implementation. Settled BEFORE any proto/code change (Constitution Principle III).

This contract pins the exact `projects/game/game.proto` changes that split the conversation content model into a display channel (`MessagePart`) and a control channel (`FlowPart`), and introduce `ToolCallPart`. It is the agent↔desktop proto interface. Per spec Clarification C2 this is a **clean break** — old sessions/checkpoints need not remain readable at the proto reconstruction layer; no compatibility shim.

## Authority

- Spec: `spec.md` FR-001..FR-005, C3, C4.
- Research: `research.md` D1 (split), D2 (`tool_id`), D3 (`args_json`).
- Data model: `data-model.md` §1..§3.
- Current proto: `projects/game/game.proto` (`Part` oneof at lines 278-289; `PartBlock` 271-275; `AgentFrame.payload` 412-417; `Message.content` 438; `WaitSignal`/`WarnSignal`/`StatusSignal` 367/372/390; operation Parts `MouseMovePart` 313 / `MouseClickPart` 322 / `KeyboardPressPart` 334 / `MouseMoveAndClickPart` 345 / `ToolResultPart` 357).
- Constitution Principle III (Interface-First Design); `style/api.md` (AIP-126 enums, AIP-140 field names, AIP-180 backward-compat notes — waived here per C2).

## Naming & numbering conventions

- Proto3 native enums; type name `PascalCase`; values `SCREAMING_SNAKE_CASE` prefixed with the enum name (matches existing `MouseClickAction`, `ToolResultStatus`, etc.).
- First value `{ENUM}_UNSPECIFIED = 0` (proto3 zero-default).
- Each new message's oneof starts its field numbers at 1.
- Removed fields are `reserved` to prevent accidental reuse (clean break makes reservation hygiene, not wire compat).

## §1. New message — `ToolCallPart`

```protobuf
// ToolCallPart carries the model's tool invocation as display content. It is
// the semantic counterpart of the physical FlowPart operation a tool dispatches:
// the conversation shows the tool name + arguments (this part), while the
// desktop executes the operation (a FlowPart). tool_id links the call to its
// subsequent tool_result MessagePart and to the FlowPart operation it dispatches
// (spec 023 FR-002/FR-008). args_json is the model's arguments verbatim
// (JSON.stringify(tool_call.args)); display-only.
message ToolCallPart {
  string tool_id   = 1;
  string name      = 2;
  string args_json = 3;
}
```

- `tool_id`: the LangChain `tool_call.id` (research.md D2). Same value as the dispatched FlowPart's `tool_id` and the matching `ToolResultPart.tool_id`.
- `args_json`: `JSON.stringify` of the model's `tool_call.args`. Examples: `saolei_click(x=3,y=4)` → `"{\"x\":3,\"y\":4}"`; `mouse_move(x_px=120,y_px=200)` → `"{\"x_px\":120,\"y_px\":200}"`.

## §2. New messages — `MessagePart` and `FlowPart` (the two categories)

```protobuf
// MessagePart is one display-only content block. The conversation renders only
// MessageParts; they are the sole content of Message (history) and the
// message_parts payload of AgentFrame (live). A tool call and its result are
// both MessageParts (tool_call, tool_result) linked by tool_id.
message MessagePart {
  oneof kind {
    TextPart       text        = 1;
    ThinkingPart   thinking    = 2;
    ImagePart      image       = 3;
    ToolCallPart   tool_call   = 4;   // NEW (spec 023 FR-002)
    ToolResultPart tool_result = 5;
  }
}

// FlowPart is one control-only block. It drives desktop execution (mouse/
// keyboard operations) and turn control (wait/warn/status); it is NEVER rendered
// as a conversation entry (spec 023 FR-005). The operation kinds reuse the
// existing Part messages verbatim (MouseMovePart, MouseClickPart,
// KeyboardPressPart, MouseMoveAndClickPart — fields unchanged from spec 018).
// The signal kinds (wait/warn/status) reuse the existing signal messages.
message FlowPart {
  oneof kind {
    MouseMovePart         mouse_move         = 1;
    MouseClickPart        mouse_click        = 2;
    KeyboardPressPart     keyboard_press     = 3;
    MouseMoveAndClickPart mouse_move_and_click = 4;
    WaitSignal            wait               = 5;
    WarnSignal            warn               = 6;
    StatusSignal          status             = 7;
  }
}
```

- `TextPart`, `ThinkingPart`, `ImagePart`, `ToolResultPart` — unchanged messages (moved into `MessagePart.kind`).
- `MouseMovePart`, `MouseClickPart`, `KeyboardPressPart`, `MouseMoveAndClickPart` — unchanged messages (moved into `FlowPart.kind`). Each already carries `tool_id` (stamped by `OperationBridge.dispatch`).
- `WaitSignal`, `WarnSignal`, `StatusSignal` — unchanged messages (moved into `FlowPart.kind`).

## §3. New wrapper messages — `MessageParts` and `FlowParts`

```protobuf
// MessageParts is the display payload: an ordered list of MessagePart. Carried
// by AgentFrame.message_parts (live) and Message.content (history) — identical
// shape, so live and history render identically (spec 023 FR-009).
message MessageParts {
  repeated MessagePart parts = 1;
}

// FlowParts is the control payload: an ordered list of FlowPart. Carried by
// AgentFrame.flow_parts. Never appears in Message.content.
message FlowParts {
  repeated FlowPart parts = 1;
}
```

## §4. `AgentFrame.payload` restructure

```protobuf
message AgentFrame {
  string session_id = 1 [(google.api.field_behavior) = REQUIRED];
  string frame_id = 2;
  google.protobuf.Timestamp create_time = 3;
  reserved 4, 5;
  reserved "invoke_id", "sequence";
  FrameSender sender = 6;
  string agent_profile_name = 7;

  // Exactly one payload per frame: a batch of display blocks OR a batch of
  // control blocks. The prior content/wait/warn/status cases are removed
  // (clean break, spec 023 C2): wait/warn/status became FlowPart kinds.
  oneof payload {
    MessageParts message_parts = 11;
    FlowParts    flow_parts    = 12;
  }
  reserved 10, 20, 21, 22;
  reserved "content", "wait", "warn", "status";
}
```

- Field numbers 11/12 are fresh (10 was `PartBlock content`; 20/21/22 were `wait`/`warn`/`status`). Reserved for hygiene.
- `payload` discriminator strings under protojson: `"messageParts"` / `"flowParts"` (camelCase). The agent's `Connect` handler builds outbound frames with the explicit `payload: "messageParts"` / `"flowParts"` discriminator (same pattern as today's `payload: "content"` at `handler.ts:158`).

## §5. `Message.content` restructure

```protobuf
message Message {
  // ... name, message_id, sender, create_time unchanged
  MessageParts content = 5;   // was PartBlock; display blocks only
  reserved 6, 7, 8, 9;
  reserved "type", "image_data", "operation", "operation_result";
}
```

- Field 5 changes type from `PartBlock` to `MessageParts` (clean break, C2). Same field number is fine since old data is not expected to render (C2).
- Control blocks never appear here (FR-004).

## §6. Removed messages

```protobuf
// PartBlock and Part are removed — replaced by MessageParts/FlowParts and
// MessagePart/FlowPart respectively. Their field numbers are not reserved
// (they were top-level messages, not fields); the generated TS/Go types are
// regenerated cleanly.
```

`PartBlock` (lines 271-275) and `Part` (278-289) are deleted. Code that referenced `Part`/`PartBlock` (`handler.ts`, `operation-bridge.ts`, `desktop/app.go`, `desktop/frontend/src/api.ts`, `desktop/view_model.go`) is migrated to `MessagePart`/`FlowPart`/`MessageParts`/`FlowParts`.

## §7. Enums (unchanged)

`FrameSender`, `ImageEncoding`, `MouseClickAction`, `ToolResultStatus`, `MouseInputMethod`, `KeyboardKey`, `StatusSignalStatus` — **unchanged**. `ToolResultStatus` keeps `UNSPECIFIED`/`SUCCEEDED`/`FAILED` (the `UNSPECIFIED` value is now meaningful: it is the neutral status a historical tool_result shows when the real status is unavailable — FR-014).

## §8. protojson rendering (frontend)

Under protojson (used by the desktop's `view_model.go:115 protoToJSONMap` and the frontend's `api.ts` types):
- `AgentFrame.payload` flattens to `messageParts: { parts: [...] }` or `flowParts: { parts: [...] }`.
- `MessagePart.kind` flattens so exactly one of `text`/`thinking`/`image`/`toolCall`/`toolResult` is set (camelCase).
- `FlowPart.kind` flattens so exactly one of `mouseMove`/`mouseClick`/`keyboardPress`/`mouseMoveAndClick`/`wait`/`warn`/`status` is set.
- `ToolCallPart` → `{ toolId, name, argsJson }`.
- The frontend's `messagePartKind(part)` returns the active variant name (`'text' | 'thinking' | 'image' | 'toolCall' | 'toolResult'`); `flowPartKind(part)` returns the FlowPart variant. The conversation renderer (`ChatView.svelte`) switches on `messagePartKind` only and ignores FlowParts.

## §9. Code-generation impact

- Agent (TS): after the proto change, run `bazel run //:gazelle projects/game/agent`; `ts_proto_library("game_types")` regenerates `game_types/projects/game/{MessagePart,FlowPart,MessageParts,FlowParts,ToolCallPart,AgentFrame,Message,...}.ts`. The old `Part.ts`/`PartBlock.ts` disappear.
- Desktop (Go): run `bazel run //:gazelle projects/game/desktop`; the Go proto regenerates the new message types under `dominion/projects/game`.
- Frontend `api.ts`: hand-written types are updated to mirror the new shapes (the file is hand-maintained, not generated).

## §10. Backward compatibility (waived per C2)

Spec Clarification C2 declares a clean break: old sessions/checkpoints are not migrated and need not remain readable at the proto reconstruction layer. The LangChain `MemorySaver` checkpoint (the actual persistence — `BaseMessage`s) is unaffected by the proto change. Therefore:
- No proto-level migration is built.
- Old `Message`/`AgentFrame` wire data (if any cached) is not expected to render correctly; this is accepted.
- The `reserved` statements are hygiene only, not wire-compat guarantees.

## Out of scope for this contract

- Tool↔bridge `tool_id` threading, the ToolMessage status carriage, and the stateless saolei tool set → `contracts/tool-dispatch-contract.md`.
- The evolving-bubble frontend view model → `data-model.md` §5.
- Fixed board geometry → `projects/game/agent/src/mcp/saolei/geometry.ts` (unchanged).
