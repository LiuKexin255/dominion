# Data Model: Desktop Window-Select Flow, Image-Transfer Hardening & Saolei Text-State Recognition

**Feature**: [spec.md](./spec.md) | **Date**: 2026-07-26

This feature's data changes are: (1) one proto addition (`FlowResultPart` + a `FlowPart` oneof kind), (2) an in-memory recognized-state entity on the agent, and (3) a desktop-side "selected window" entity that replaces the removed `boundWin`. The `AgentFrame` envelope, the `MessagePart`/`FlowPart` split from 023, and the desktop-facing operation messages (`MouseMoveAndClickPart`, `KeyboardPressPart`, `ImagePart`, `ToolResultStatus`) are unchanged.

---

## 1. Proto — `FlowResultPart` (new) and `FlowPart` oneof extension

Source: `projects/game/game.proto`. Field numbers chosen to not collide with existing ones.

### 1.1 New message `FlowResultPart`

Carries the desktop's outcome of an executed `FlowPart` operation, reported over the **control channel** (`FlowParts`). Shape mirrors `ToolResultPart` (`game.proto:403-408`) but it is a `FlowPart` kind, never rendered in the conversation. See [contracts/flow-result-contract.md](./contracts/flow-result-contract.md).

```protobuf
// FlowResultPart carries the desktop's outcome of an executed FlowPart
// operation (mouse/keyboard), reported back over the CONTROL channel
// (FlowParts). It is the operation-channel counterpart to the operation
// request kinds and corresponds shape-wise to ToolResultPart — but it is a
// FlowPart kind, NEVER rendered in the conversation (spec 025 FR-023/FR-024).
// The agent consumes it (OperationBridge.handleResult) and translates it into
// the display tool_result MessagePart per tool (text board for saolei;
// text + screenshot for mouse tools).
message FlowResultPart {
  // Operation-channel id (bridge-minted by OperationBridge.dispatch), matching
  // the originating operation FlowPart's tool_id. Used only for dispatch↔result
  // correlation; unrelated to the conversation tool_call.id (023 C13).
  string tool_id = 1;
  // Operation outcome. Reuses ToolResultStatus (UNSPECIFIED/SUCCEEDED/FAILED).
  ToolResultStatus status = 2;
  // Human/agent-readable outcome detail (e.g. "ok" or an error message).
  string message = 3;
  // Post-action screenshot (control-channel carrier). Consumed by the agent
  // for recognition (saolei) or copied into a mouse tool's display tool_result.
  ImagePart screenshot = 4;
}
```

**Field semantics**:
- `tool_id` — correlates to the request `FlowPart`'s `tool_id` (minted by `OperationBridge.dispatch`, `projects/game/agent/src/operation-bridge.ts:208-209`).
- `status` — reuses `ToolResultStatus` (`game.proto`, enum with `TOOL_RESULT_STATUS_UNSPECIFIED/SUCCEEDED/FAILED`). The operation's real outcome; the agent derives the display `tool_result` status from it.
- `message` — same role as `ToolResultPart.message` today (e.g. `"ok"`, `"no window selected"`, `"... (screenshot capture failed: ...)"`).
- `screenshot` — the post-action screenshot, same shape as today's `ToolResultPart.screenshot` (`game.proto:407`). `ScaleFactor`/`WindowTitle` are read from the resolved **selected window** (D3) at capture time.

### 1.2 `FlowPart` oneof — add `flow_result`

`projects/game/game.proto:312-322`. Add one new case (number 8):

```protobuf
message FlowPart {
  oneof kind {
    MouseMovePart         mouse_move           = 1;
    MouseClickPart        mouse_click          = 2;
    KeyboardPressPart     keyboard_press       = 3;
    MouseMoveAndClickPart mouse_move_and_click = 4;
    WaitSignal            wait                 = 5;
    WarnSignal            warn                 = 6;
    StatusSignal          status               = 7;
    FlowResultPart        flow_result          = 8;  // NEW (spec 025 FR-023)
  }
}
```

**Unchanged**: `MessagePart` (incl. its `tool_result` case, `game.proto:296-304`), `MessageParts`/`FlowParts`, `AgentFrame.payload` oneof, `ImagePart`, the operation request messages, and the signal messages.

**Naming note**: the `FlowPart` oneof field for `StatusSignal` is named `status` (= 7); `FlowResultPart` has its own field named `status` (type `ToolResultStatus`) *inside* the `FlowResultPart` message. These do not collide (different scopes).

---

## 2. Entity — Recognized saolei board state (agent, in-memory)

| Attribute | Value |
|---|---|
| Location | In-memory, inside the per-session saolei MCP server closure (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`; server created per-session by `projects/game/agent/src/mcp-host.ts`). |
| Type | A `SaoleiBoard` instance from `@dominion/game-saolei-board` (`projects/game/pkg/saolei-board/README.md` → "核心库用法"). |
| Lifecycle | Co-located with the LangChain checkpoint on the agent; both lost together on agent restart. No persistence, no reconnect recovery (spec Clarification Q1). |
| Input | `FlowResultPart.screenshot` (base64 PNG) → decoded to `Buffer` → `SaoleiBoard.init` / `updateFromScreenshot`. |
| Output | `renderBoardText(state)` → the text board returned to the model (D6). |

**State machine** (per session):

```
                 saolei_init (F2 dispatch + init screenshot)
   [none] ───────────────────────────────────────────────► [active]
     ▲                                                         │
     │                                          updateFromScreenshot on each legal cell op
     │                                                         │
     │                                            terminal recognized (lost)
     │                                                         │
     │  saolei_init again (F2 dispatch + init)                ▼
   [active] ◄───────────────────────────────────────────── [terminal]
                                                              │
                                    any cell op → REJECT (FR-015f) until re-init
```

**Transitions**:
- `none → active`: `saolei_init` dispatches F2, captures screenshot, `SaoleiBoard.init(screenshot)` succeeds, returns initial text board.
- `active → active`: a legal cell op dispatches, captures screenshot, `updateFromScreenshot(screenshot)` succeeds (monotonic), returns updated text board.
- `active → terminal`: `updateFromScreenshot` recognizes a terminal (**lost**) board — a `HIT_MINE` (`X`, the triggered mine) or `MINE` (`M`, an end-game revealed mine) is present. Returns text board; subsequent cell ops rejected (FR-015f) until `saolei_init`. Terminal detection is **loss-only**: a win is not reliably detectable from the cell grid (classic Minesweeper auto-flags every mine as `F` on a win, indistinguishable from a player-placed flag, and exposes no distinct per-cell marker), so `game_over` covers the post-loss case where the model would otherwise keep clicking a finished board.
- `* → error (unable to recognize)`: `init`/`updateFromScreenshot` throws (`BoardStateIncompatibleError` / `BoardDimensionMismatchError`, or the image is not a saolei board). The tool returns "unable to recognize" (FR-017); the state is marked invalid; cell ops rejected until a successful `saolei_init`.

**Validation source**: the current recognized `state` (cell grid + dimensions) is the input to pre-dispatch validation (D5). `UNKNOWN` cells are treated leniently (FR-018): a move targeting an `UNKNOWN` cell is **not** rejected on state grounds.

---

## 3. Entity — Selected window (desktop, single source of truth)

| Attribute | Value |
|---|---|
| Location | Frontend reactive state `selectedWindowHandle` (`projects/game/desktop/frontend/src/App.svelte:125`); resolved to a `capture.WindowRef` (`projects/game/desktop/internal/capture/window.go:14-21`) at use time on the backend. |
| Replaces | `App.boundWin` (`projects/game/desktop/app.go:200`) and its `BindWindow`/`CaptureScreenshot` two-step (removed — D3). |
| Resolution | Backend resolves `WindowRef` from the selected handle via `capture.ListWindows` lookup (as `BindWindow` does today, `app.go:1249-1277`) at the point an operation or screenshot runs. |

**Attributes consumed** (from the resolved `WindowRef`):
- `Handle` (uintptr) — passed to operation executors (`projects/game/desktop/app_operation.go:28,94`) and `capture.CaptureWindow`.
- `ScaleFactor`, `WindowTitle` — read into `ImagePart`/`FlowResultPart.screenshot` at capture time (replacing `app.go:719-720,1146-1147`).
- `WidthPx`, `HeightPx` — from the captured image.

**Validity rules**:
- No window selected ⇒ operation/screenshot fails gracefully with a clear message (FR-005); never a crash, never a silent no-op.
- Selected window disappeared (closed/minimized/hidden) ⇒ `capture.CaptureWindow` rejects it (existing validation, `projects/game/desktop/internal/capture/capture.go`); surfaced as a clear failure.

**State transitions**: selecting a window sets the handle; selecting a different window replaces it (the next op/screenshot targets the new selection — FR-004). There is no "bound"/"unbound" transition; the selected handle is the sole state.

---

## 4. Unchanged contracts (explicit non-changes)

To bound scope and prevent accidental breakage, these are **explicitly unchanged** by this feature:

- `AgentFrame` envelope and its `payload` oneof (`message_parts` / `flow_parts`) — `game.proto:452-474`.
- The display `MessagePart` kinds, including `tool_result` (`game.proto:296-304`) — still emitted by the **agent** for display (mouse tools: text + screenshot; saolei: text only). The desktop no longer emits `tool_result` `MessagePart` for operation outcomes (FR-024), but the message kind itself is unchanged.
- The operation-request `FlowPart` kinds (`mouse_move`, `mouse_click`, `keyboard_press`, `mouse_move_and_click`) and their messages — the desktop-facing contract (FR-019/FR-020).
- `ImagePart` (`game.proto:348-355`) and `ToolResultStatus`.
- The grid→pixel geometry in `projects/game/agent/src/mcp/saolei/geometry.ts` (client space) and `saolei-board`'s screenshot-space geometry — both retained, not mixed (D4).
