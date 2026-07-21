# Contract: Proto Operation Extensions

**Feature**: 018-saolei-mcp
**Date**: 2026-07-20
**Status**: Phase 1 contract — MUST be satisfied by implementation. Settled BEFORE any proto/code change (Constitution Principle III).

This contract pins the exact extensions to `projects/game/game.proto` that carry saolei operations from agent to desktop. `AgentFrame` and `Part` remain **tool-agnostic** (FR-004): they declare generic input operations, never saolei semantics.

## Authority

- Spec: `spec.md` FR-004, FR-004a..FR-004d, FR-006..FR-009.
- Research: `research.md` D4, D5, D7.
- Constitution Principle III (Interface-First Design): proto changes are settled before implementation.
- Current proto: `projects/game/game.proto:201-234` (enums), `253-309` (Part + part messages), `346-365` (AgentFrame).

## Naming & numbering conventions (verified from existing proto)

- Proto3 native enums; type name `PascalCase`; values `SCREAMING_SNAKE_CASE` prefixed with the enum name.
- First value `{ENUM}_UNSPECIFIED = 0` (proto3 zero-default requirement).
- Part oneof field numbers are sequential; next free slots after `tool_result = 6` are `7` and `8`.
- Existing enums for reference: `MouseClickAction` (`game.proto:219-226`), `ToolResultStatus` (`230-234`).

## Extensions

### 1. New enum — `MouseInputMethod`

```protobuf
enum MouseInputMethod {
  MOUSE_INPUT_METHOD_UNSPECIFIED = 0;
  MOUSE_INPUT_METHOD_SIMULATED = 1;
  MOUSE_INPUT_METHOD_WINDOW_MESSAGE = 2;
}
```

Semantics:
- `SIMULATED` — the existing behavior: the desktop moves the OS cursor (`SetCursorPos`) and injects button events via `SendInput`. Coordinates are **screen-absolute** (the desktop converts screenshot-relative → screen via window bounds). This is the default for the existing `mouse_move`/`mouse_click` tools.
- `WINDOW_MESSAGE` — the desktop posts `WM_*` button messages to the bound window's HWND **without moving the cursor**. Coordinates are **window-client** (packed into `lParam`), no screen-offset addition.
- `UNSPECIFIED` — the desktop treats it as `SIMULATED` (backward compatibility for old Parts that omit the field).

### 2. New part — `KeyboardPressPart`

```protobuf
message KeyboardPressPart {
  string tool_id = 1;
  KeyboardKey key = 2;
}
```

```protobuf
enum KeyboardKey {
  KEYBOARD_KEY_UNSPECIFIED = 0;
  KEYBOARD_KEY_F2 = 1;
}
```

- Carries a single key identifier. `KEYBOARD_KEY_F2` is used by `saolei_init` to start a new minesweeper game (FR-006).
- `tool_id` follows the existing convention (stamped by `OperationBridge.dispatch`, `projects/game/agent/src/operation-bridge.ts:155-156`).
- The enum starts with only F2; additional keys are added as future tools need them (extensible).
- Desktop execution: posts the key to the bound HWND (e.g., `WM_KEYDOWN`/`WM_KEYUP` with virtual key code `VK_F2 = 0x71`), or `SendInput` with `KEYBDINPUT`. The concrete Win32 mechanism is the desktop's responsibility (FR-004: "具体的鼠标与键盘操作的实现方式有 desktop 负责"). Window-message posting is preferred (no foreground-focus requirement, no cursor side effects).

### 3. New part — `MouseMoveAndClickPart`

```protobuf
message MouseMoveAndClickPart {
  string tool_id = 1;
  int32 x_px = 2;
  int32 y_px = 3;
  MouseClickAction click = 4;
  MouseInputMethod method = 5;
}
```

- Combines move + click into one atomic operation (research.md D5). Used by the saolei cell operations.
- `x_px`/`y_px` are **window-client coordinates** when `method = WINDOW_MESSAGE` (computed by the saolei MCP via the fixed formula, `data-model.md` §5). When `method = SIMULATED`, they are screenshot-relative screen-target coords (not used by saolei, but the field keeps the part general).
- `click` reuses the existing `MouseClickAction` enum (`game.proto:219-226`): `LEFT_CLICK`, `LEFT_DOUBLE_CLICK`, `RIGHT_CLICK`, `RIGHT_DOUBLE_CLICK`, `LEFT_RIGHT_PRESS`.
- `method` selects the desktop execution path.

### 4. Add `MouseInputMethod method` to the existing mouse parts

```protobuf
message MouseMovePart {     // existing: game.proto:287-291
  string tool_id = 1;
  int32 x_px = 2;
  int32 y_px = 3;
  MouseInputMethod method = 4;   // NEW
}

message MouseClickPart {    // existing: game.proto:294-298
  string tool_id = 1;
  MouseClickAction click = 2;
  MouseInputMethod method = 3;   // NEW
}
```

- The new field defaults to `MOUSE_INPUT_METHOD_UNSPECIFIED` (= 0), which the desktop treats as `SIMULATED`. Existing `mouse_move`/`mouse_click` tool calls omit the field → behavior unchanged (FR-004c).

### 5. Extend `Part.kind` oneof

```protobuf
message Part {              // existing: game.proto:253-263
  oneof kind {
    TextPart       text              = 1;
    ThinkingPart   thinking          = 2;
    ImagePart      image             = 3;
    MouseMovePart  mouse_move        = 4;
    MouseClickPart mouse_click       = 5;
    ToolResultPart tool_result       = 6;
    KeyboardPressPart     keyboard_press       = 7;   // NEW
    MouseMoveAndClickPart mouse_move_and_click = 8;   // NEW
  }
}
```

## Desktop `MouseClickAction` → `WM_*` mapping (WINDOW_MESSAGE)

The desktop posts these sequences to the bound HWND with the window-client coordinate packed into `lParam` (low-order = x, high-order = y) (FR-004d):

| `MouseClickAction` | `WM_*` sequence |
|---|---|
| `LEFT_CLICK` | `WM_LBUTTONDOWN` → `WM_LBUTTONUP` |
| `LEFT_DOUBLE_CLICK` | `WM_LBUTTONDOWN` → `WM_LBUTTONUP` → `WM_LBUTTONDOWN` → `WM_LBUTTONUP` (+ `WM_LBUTTONDBLCLK`) |
| `RIGHT_CLICK` | `WM_RBUTTONDOWN` → `WM_RBUTTONUP` |
| `RIGHT_DOUBLE_CLICK` | `WM_RBUTTONDOWN` → `WM_RBUTTONUP` → `WM_RBUTTONDOWN` → `WM_RBUTTONUP` (+ `WM_RBUTTONDBLCLK`) |
| `LEFT_RIGHT_PRESS` | `WM_LBUTTONDOWN` → `WM_RBUTTONDOWN` → `WM_RBUTTONUP` → `WM_LBUTTONUP` (chord) |

The exact message interleaving for `LEFT_RIGHT_PRESS` is the desktop's responsibility; the contract requires only: both buttons' down/up are posted to the target coordinate without OS cursor movement.

## Backward compatibility

1. The new `method` field on `MouseMovePart`/`MouseClickPart` defaults to `UNSPECIFIED` → treated as `SIMULATED` → existing mouse tools behave identically.
2. The `Part.kind` additions are purely additive. The desktop's existing `GetMouseMove()`/`GetMouseClick()` accessors ignore unknown oneof members.
3. TS types regenerate automatically via `ts_proto_library("game_types")` (`projects/game/agent/BUILD.bazel:11-15`). Go types regenerate via the existing `dominion/projects/game` proto import used by the desktop.
4. No reserved fields are consumed; no existing field numbers change.

## Code-generation impact

- Agent (TS): after the proto change, run `bazel run //:gazelle projects/game/agent` (BUILD glob is automatic for `src/**/*.ts`); `ts_proto_library` regenerates `game_types/projects/game/{Part,KeyboardPressPart,MouseMoveAndClickPart,MouseInputMethod,KeyboardKey,MouseMovePart,MouseClickPart}.ts`.
- Desktop (Go): run `bazel run //:gazelle projects/game/desktop`; the Go proto regenerates the new message/enum types under the `dominion/projects/game` package.

## Out of scope for this contract

- The saolei MCP tool schemas → `contracts/mcp-tool-contract.md`.
- The fixed board geometry formula → `data-model.md` §5.
- Validation rules → `data-model.md` §8.
- Multi-key chords or modifier-key support (single-key `KeyboardPressPart` only for F2).
