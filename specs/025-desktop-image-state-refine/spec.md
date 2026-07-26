# Feature Specification: Desktop Window-Select Flow, Image-Transfer Hardening & Saolei Text-State Recognition

**Feature Branch**: `025-desktop-image-state-refine`

**Created**: 2026-07-26

**Status**: Draft

**Input**: User description: "对 projects/game/ 修改：1) desktop 在 session 对话页面，移除窗口的显式"绑定"。目前实测如果不点击截图按钮，只是选中窗口，则操作后截图会提示失败。这块改为不需要"绑定"，而是截图时使用选中的窗口进行截图。2) 使用 ws 在 desktop 和 agent 之间传递图片可能会导致 frame 过大，需要对这部分进行优化或者换一种更稳妥的方案。3) 为 saolei mcp 返回改为文字格式的游戏状态，使用 projects/game/pkg/saolei-board 实现图片到扫雷游戏状态的识别。mcp 操作前增加校验，根据最新的游戏状态对操作指令进行校验，如果不符合游戏规则则拒绝。"

## Motivation

Three independent problems in the current desktop↔agent↔LLM path for the game, each rooted in a different layer. All three reduce the reliability and correctness of an end-to-end game turn.

**Problem 1 — a spurious two-step "window binding" causes failures.** The desktop session chat page treats "selecting a window" and "binding a window" as two separate actions. The dropdown at `projects/game/desktop/frontend/src/App.svelte:880-885` sets only a frontend value (`selectedWindowHandle`, `App.svelte:125`). The backend's notion of the active window — `App.boundWin` (`projects/game/desktop/app.go:200`) — is set **only** by an explicit `BindWindow()` call, which today happens **only** inside the "Capture Screenshot" button handler `handleCaptureScreenshot()` (`App.svelte:770-788`). If a user merely selects a window from the dropdown (without clicking Capture), `boundWin` stays zero-valued. Consequently every agent operation hits the early failure `if a.boundWin.Handle == 0 { return failed("no window bound") }` (`app.go:1074`), and the post-action screenshot is skipped entirely (`app.go:1129` guard). The reported symptom — "选中窗口后操作，截图提示失败" — is exactly this path. The fix is to collapse the two concepts: the **selected** window is the window used for screenshots and operations; there is no separate binding step.

**Problem 2 — image data over the desktop↔gateway WebSocket leg is oversized-prone.** Today a screenshot is raw PNG bytes placed into `ImagePart.data` (`projects/game/game.proto:348-355`), serialized by `protojson.Marshal` (base64), and sent as a **single WebSocket text frame** (`projects/game/desktop/internal/api/websocket.go:62-79`, `websocket.MessageText`). Base64 inflates the payload by ~33%, and the whole frame — image included — must clear the WebSocket message-size path in one shot. The hard ceiling is `maxScreenshotBytes = 5 MiB` (`app.go:667`), but even below that, a large window screenshot produces a very large single WS text frame that is fragile (proxy/gateway frame caps, allocation spikes) and is duplicated through the turn: the user-turn screenshot (`SendUserTurn`, `app.go:693-758`) and every post-action screenshot (`app.go:1129-1150`). Notably, reliable chunking already exists **only** on the SSE delivery path (`projects/game/desktop/internal/chatstream/chunk.go:19`, `maxFragmentBytes = 48 KiB`); the WS transport has no equivalent. The requirement is to make image delivery between desktop and agent robust and efficient so it never fails on frame size.

**Problem 3 — the model reads the board from a screenshot, and nothing rejects illegal moves.** Since [023 — Saolei MCP Refine](../023-saolei-mcp-refine/spec.md) removed the agent-side grid model and validation (FR-017/FR-018), the four saolei tools are stateless dispatchers: each dispatches its operation and returns a text line **plus the raw screenshot** to the model (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:118-126`), with **no** pre-dispatch validation (`saolei-mcp.ts:286-295`, `cellInputSchema` only constrains `x,y ≥ 0`). This offloads all visual reading and rule-checking to the LLM: it must interpret pixels to know the board, and nothing prevents an illegal move (clicking an already-revealed cell, operating before a game starts, moving after the game ended, coordinates off the board). The deterministic recognition library `@dominion/game-saolei-board` (`projects/game/pkg/saolei-board/README.md`) already turns a Minesweeper screenshot into a structured, human/LLM-readable **text** board via `recognizeBoard` / `SaoleiBoard.init` + `updateFromScreenshot` + `renderBoardText`. Routing recognition through that library — instead of through the model's pixel-reading — lets the MCP return a **text** game state and lets the MCP itself validate each requested move against the latest recognized state, rejecting illegal ones before they ever reach the desktop.

## Relationship

- **Refines [018 — Saolei MCP](../018-saolei-mcp/spec.md)** and **partially revises [023 — Saolei MCP Refine](../023-saolei-mcp-refine/spec.md)**: 023 deliberately made the saolei MCP stateless and removed validation, on the premise that the model would read the returned screenshot. This feature reverses that premise for the **recognition + validation** dimension only: the agent now derives game state **deterministically from the screenshot** (via `@dominion/game-saolei-board`) rather than asking the LLM to read pixels, and validates moves against that recognized state. The desktop-facing operation contract (the proto `FlowPart` kinds: `saolei_init` → `KeyboardPressPart{F2}`; the cell tools → `MouseMoveAndClickPart` at the fixed board geometry with `WINDOW_MESSAGE`) and the retained grid→pixel geometry (`projects/game/agent/src/mcp/saolei/geometry.ts`) are unchanged. The four-tool surface (`saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`) is unchanged.
- **Consumes [`projects/game/pkg/saolei-board`](../../projects/game/pkg/saolei-board/README.md)** as the recognition engine. Its `SaoleiBoard` provides monotonic cross-screenshot state (`init` for a new game; `updateFromScreenshot` for subsequent frames, rejecting dimension changes and non-monotonic cell regressions) and a text renderer. This feature wires that engine into the saolei MCP.
- **Independent of the [023](../023-saolei-mcp-refine/spec.md) content-model split and debug drawer**: the conversation rendering, `MessagePart`/`FlowPart` split, tool-bubble behavior, and debug hold are not in scope here. The saolei tool results still flow through the same `OperationBridge` → `ToolResultPart` path; only **what the saolei tool returns to the model** (text state, not image) and **what it checks before dispatching** change.
- **Touches the desktop transport independently**: Problem 1 (window-select) and Problem 2 (image transfer) are desktop-side and orthogonal to the saolei MCP changes. All three may be planned and delivered as separate phases.
- **Interface (Constitution §III)**: the saolei tool **return contract** changes (image block → text board block; new "rejected as illegal" outcome) and a **validation contract** is introduced (the rules a move must satisfy, derived from the recognized state). The proto `FlowPart` operation contract is unchanged. The exact text-board schema and the precise validation rule set are settled in planning, constrained by the Functional Requirements below.

## Clarifications

Resolved during specification by reasonable defaults (documented in **Assumptions**). No `[NEEDS CLARIFICATION]` markers remain. Decisions settled in clarification (Session 2026-07-26): the **validation strictness** for Problem 3 (strict, per FR-015) and the **operation-result channel separation** via a new `FlowResultPart` (plan input, FR-023..FR-026). The **transport mechanism** for Problem 2 is the one open architecture decision and is resolved in `research.md` (Phase 0); this spec fixes only the *outcome* (FR-007..FR-011).

### Session 2026-07-26

- Q: What happens to the recognized saolei state across session reconnect / re-entry (a game visually in progress but no recognized state in memory)? → A: No special handling needed. The recognized state is held in-memory in the per-session MCP server, **co-located with the LLM checkpoint on the agent service**; on agent restart both are lost together, so there is never a scenario where one survives without the other. Reconnect/re-entry state recovery is therefore **out of scope** — a fresh session simply starts a new game.
- Q: How strict should pre-dispatch validation be for operations on already-revealed/flagged cells? → A: **Strict**. Reject any move whose target-cell state makes it a no-op or impermissible per the rules — left-click on a revealed cell or a flag, flag on a revealed cell, chord on anything but a revealed number (1–8) — in addition to structural violations. Validation judges target-cell compatibility, not predicted outcome (so a chord whose adjacent-flag count does not match is still a **legal, permitted** move and is NOT rejected).
- Q: With the saolei MCP now returning text (not image) to the model, should the desktop conversation bubble for saolei operations still show the captured screenshot to the human user? → A: **Text board only** — the saolei tool-result bubble shows the recognized text board and MUST NOT display the screenshot (FR-022). The screenshot is consumed internally for recognition only. Consistent with 023 (the bubble renders from the tool's LLM result, now text); no desktop-side screenshot mirror is re-introduced.
- Q (plan input): Should the operation-execution result be separated from the display tool result? → A: **Yes — introduce a `FlowResultPart`** as the control-channel counterpart to `ToolResultPart`. Flow/operation results travel as `FlowResultPart` (in `FlowParts`); they are no longer mixed with the display `tool_result` `MessagePart`. The desktop emits `FlowResultPart`; the agent translates it into the display `tool_result` per tool (FR-023..FR-026). This completes 023's conversation/control decoupling and gives saolei a clean control-channel path for the recognition screenshot while the display stays text-only.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The Selected Window Is Used Directly; No Separate Binding (Priority: P1)

On the desktop session chat page, picking a window from the window list is sufficient to make that window the target of every screenshot and every agent operation in the turn. There is no separate "bind"/"capture to activate" step: the user selects a window, sends a message, and the agent's operations execute against that window and return a post-action screenshot. The prior defect — selecting a window without first clicking Capture, then running an operation, yielding a "no window bound" failure and a missing screenshot — no longer occurs. Re-selecting a different window makes the newly selected window the target for subsequent actions.

**Why this priority**: This is the directly reported user-facing defect ("选中窗口后操作，截图提示失败"). It blocks the primary game loop whenever the user follows the natural select-then-chat flow. It is fully desktop-side and independent of the other two stories.

**Independent Test**: Select a window from the list **without** clicking any Capture button; send a message that triggers an operation; confirm the operation executes against the selected window and a post-action screenshot is returned (no "no window bound" error, no missing screenshot).

**Acceptance Scenarios**:

1. **Given** a window is selected in the dropdown and no Capture button has been pressed, **When** an agent operation arrives, **Then** the operation executes against the selected window (not a failure) and a post-action screenshot of that window is captured and returned.
2. **Given** a window is selected, **When** the user (or the agent flow) takes a screenshot, **Then** the screenshot is captured from the currently selected window — there is no separate binding action required beforehand.
3. **Given** a different window is selected mid-session, **When** the next operation or screenshot occurs, **Then** it targets the newly selected window.
4. **Given** no window is selected, **When** an operation or screenshot is attempted, **Then** it fails with a clear, user-facing message indicating no window is selected (graceful, not a crash).

---

### User Story 2 - Image Transfer Between Desktop and Agent Is Robust and Efficient (Priority: P1)

Image data exchanged between the desktop and the agent (the user-turn screenshot and every post-action screenshot) is delivered reliably and never fails because of frame size. Regardless of how large a window screenshot is, a full image round-trip completes without oversized-frame errors, allocation failures, or silent truncation. The mechanism is also efficient — it does not waste disproportionate bandwidth or memory relative to what the agent actually needs the image for.

**Why this priority**: A single oversized frame can break an entire turn, and screenshots flow on every operation, so this is a frequent, turn-breaking reliability hazard on the desktop↔agent path. It is independent of the window-select fix and of the saolei MCP changes.

**Independent Test**: Run a turn against a large (high-resolution / high-DPI) window that produces a screenshot near or above today's 5 MiB ceiling; confirm the screenshot is delivered to the agent and the result returns without any frame-size failure, and that no other turn is disrupted.

**Acceptance Scenarios**:

1. **Given** a screenshot whose encoded size would today produce an oversized WS text frame, **When** it is sent from desktop to agent, **Then** it is delivered completely and the turn continues (no failure, no truncation).
2. **Given** any operation that produces a post-action screenshot, **When** the result returns to the agent, **Then** the image is delivered reliably and the agent can consume it (e.g. for board recognition) without a transport-level error.
3. **Given** a run of many consecutive operations, **When** screenshots flow repeatedly, **Then** none of them fail on frame size and no progressive degradation (memory/bandwidth) is observed.
4. **Given** the delivery mechanism chosen, **When** measured, **Then** the on-the-wire cost of carrying an image is bounded and proportionate to its useful size, not inflated by a fixed large multiple.

---

### User Story 3 - Saolei MCP Returns Text Game State and Validates Moves Against It (Priority: P1)

The saolei MCP tools return the current game board as **text** (recognized deterministically from the screenshot by `@dominion/game-saolei-board`), instead of returning the screenshot image for the model to read. Before dispatching any cell operation, the MCP checks the requested move against the latest recognized game state: if the move violates the game rules (e.g. operating before a game has started, targeting an already-revealed cell, targeting a coordinate outside the board, or moving after the game has ended), the MCP **rejects** it and returns a clear reason — without dispatching it to the desktop. Legal moves dispatch as before, the post-action screenshot is recognized into the new state, and the updated text board is returned. The state is maintained across calls within the same game (initialized on `saolei_init`, updated from each subsequent screenshot), so validation always uses the freshest recognized board.

**Why this priority**: Correctness of the game loop. Deterministic recognition replaces error-prone LLM pixel-reading, and pre-dispatch validation prevents wasted/illegal operations. This is the value the `saolei-board` library was built for (per its README: "把视觉识别从 LLM 卸载到确定性的颜色分析代码，提升操作准确率").

**Independent Test**: Configure a saolei profile; call `saolei_init`, then call `saolei_click` on a legal cell — confirm the result contains a **text** board (no image), and the cell reflects the reveal; then call `saolei_click` again on the **same** (now revealed) cell — confirm it is **rejected** with a reason and is **not** dispatched to the desktop.

**Acceptance Scenarios**:

1. **Given** a saolei session, **When** `saolei_init` is called, **Then** it dispatches the new-game keypress, recognizes the initial board from the returned screenshot, and returns the initial board as **text** (not an image) to the model.
2. **Given** a legal move on the current recognized board, **When** a cell tool is called, **Then** it dispatches the operation, recognizes the updated board from the new screenshot, and returns the updated **text** board.
3. **Given** an illegal move (violates a game rule against the latest recognized state), **When** a cell tool is called, **Then** it is **rejected before dispatch** with a clear reason, and the desktop receives no operation for it.
4. **Given** a cell operation is attempted before any game has been started (no recognized state), **When** it is called, **Then** it is rejected as "no active game" (the model is guided to call `saolei_init` first).
5. **Given** the game has reached a terminal state (won or lost) in the recognized state, **When** a cell operation is called, **Then** it is rejected until a new game is started.
6. **Given** any saolei tool result, **When** the model receives it, **Then** the result carries the board as text and does **not** carry the screenshot image as model-facing content (the image is consumed internally for recognition only).

---

### Edge Cases

- **Selected window disappears (closed / minimized / hidden)**: a screenshot or operation against it fails gracefully with a clear message; the UI does not crash and can recover by selecting another window.
- **Re-selecting a window mid-turn**: subsequent operations/screenshots target the new selection; in-flight operations against the prior window complete or fail cleanly without cross-contamination.
- **Image-transfer degradation / interruption**: if the delivery path is interrupted, the turn fails with a clear, attributable error rather than hanging or silently dropping the screenshot.
- **Recognition of a non-Minesweeper window or unreadable board**: the saolei tool returns an explicit "unable to recognize board" outcome rather than a fabricated state; cell operations are rejected until a valid board is recognized.
- **Recognition uncertainty (`UNKNOWN` cells)**: validation does **not** reject a move solely because some cells are uncertain; only concrete rule violations (provable from the recognized state) cause rejection. (Mirrors `saolei-board`'s lenient handling of `UNKNOWN`, per its README.)
- **State reset / new game**: calling `saolei_init` (or otherwise starting a new game) resets the recognized state; a dimension change or non-monotonic regression between screenshots is handled per `saolei-board`'s `init`/`updateFromScreenshot` contract (re-init required), surfaced as a clear outcome rather than a silent corruption.
- **Agent restart / session re-entry**: the recognized state and the LLM checkpoint are co-located on the agent and lost together, so re-entering a session after an agent restart starts a fresh game (no partial state). Within a live session (no restart), the in-memory state persists across the session's calls as normal.
- **Out-of-bounds coordinate**: a cell coordinate outside the recognized board dimensions is rejected as illegal before dispatch (the model is told the valid range).
- **Validation vs. recognized-state staleness**: because the state is refreshed from the most recent screenshot each turn, validation reflects the latest observed board; if recognition could not update the state, the tool reports that rather than validating against stale data.
- **First move of a game**: after `saolei_init`, the first cell operation on an unrevealed cell is legal and dispatches; validation does not over-constrain the opening.

## Requirements *(mandatory)*

### Functional Requirements

**Window-select flow (Problem 1)**

- **FR-001**: The desktop session chat page MUST treat the window selected in the window list as the active target for screenshots and operations. There MUST be no separate user-facing "bind window" / "activate window" action required before screenshots or operations work.
- **FR-002**: An agent operation MUST execute against the currently selected window and MUST NOT fail with a "no window bound" error when a window is selected, regardless of whether any Capture button was pressed.
- **FR-003**: A screenshot (both user-initiated and post-operation) MUST be captured from the currently selected window without requiring a prior explicit binding step.
- **FR-004**: Selecting a different window MUST make that window the target for all subsequent screenshots and operations.
- **FR-005**: When no window is selected, an operation or screenshot MUST fail gracefully with a clear message; it MUST NOT crash or silently no-op.
- **FR-006**: The internal "bound window" concept (the backend `App.boundWin` field at `projects/game/desktop/app.go:200` and its setters/readers) MUST be removed or replaced so that the selected window is the single source of truth for the operation/screenshot target (Constitution §II — collapse the redundant binding layer, do not patch around it).

**Image-transfer hardening (Problem 2)**

- **FR-007**: Image data exchanged between the desktop and the agent (user-turn screenshot and every post-action screenshot) MUST be delivered reliably; it MUST NOT fail solely because a single serialized frame would be large.
- **FR-008**: The delivery mechanism MUST NOT inflate the image payload beyond the size of the raw PNG bytes (no unconditional base64 encoding in a single text frame), and MUST have a defined maximum frame size (10 MiB) on both ends of the WebSocket leg, so the on-the-wire cost is bounded by the useful image size without fixed overhead multipliers.
- **FR-009**: The hardening MUST cover both directions that carry images on the desktop↔agent path today — the user-turn screenshot inbound and the post-action screenshot returned with the operation result.
- **FR-010**: A failed/oversized image delivery MUST surface as a clear, attributable error (not a hang and not a silent truncation), and MUST NOT destabilize the WebSocket session or other concurrent turns.
- **FR-011**: The change MUST be applied consistently across the desktop↔gateway WebSocket transport (`projects/game/desktop/internal/api/websocket.go` send/receive) and any peer (gateway/agent) that handles the same frames, so both ends agree on the new representation.

**Saolei text-state recognition & validation (Problem 3)**

- **FR-012**: The saolei MCP tools MUST return the current game board as **text** to the model, produced by recognizing the screenshot with `@dominion/game-saolei-board` (`recognizeBoard` / `SaoleiBoard` / `renderBoardText`). The screenshot MUST NOT be returned to the model as model-facing image content; it is consumed internally for recognition only.
- **FR-013**: The agent MUST maintain the recognized saolei board state across calls within the same game: initialize on `saolei_init` (`SaoleiBoard.init`) and update from each subsequent screenshot (`updateFromScreenshot`), so validation always uses the latest recognized state.
- **FR-014**: Before dispatching any cell operation (`saolei_click` / `saolei_flag` / `saolei_chord_click`), the MCP MUST validate the requested move against the latest recognized state. An operation that violates a game rule MUST be **rejected before dispatch** with a clear reason, and the desktop MUST receive no operation for it.
- **FR-015**: Validation is **strict** (decided in clarification, Session 2026-07-26). Before dispatching any cell operation, the MCP MUST reject — with a clear reason and without dispatching — any of the following, derived from the recognized state:
  - (a) a cell operation attempted when no game is active (before `saolei_init`);
  - (b) a coordinate outside the recognized board dimensions;
  - (c) `saolei_click` (left-click) whose target is **not** an unrevealed cell — left-click on a revealed cell (number 0–8) or on a flag (`F`) is rejected; only left-click on an unrevealed (`*`) cell dispatches;
  - (d) `saolei_flag` whose target is a revealed cell (number 0–8) — rejected; flagging an unrevealed (`*`) or already-flagged (`F`) cell is permitted (place / toggle flag);
  - (e) `saolei_chord_click` whose target is anything other than a revealed number (1–8) — rejected; chord is permitted only on a revealed number. A chord whose adjacent-flag count does not match the number is still a **legal, permitted** move (it may reveal nothing) and MUST NOT be rejected — validation judges target-cell compatibility, not predicted outcome;
  - (f) any cell operation after the recognized state is terminal (won or lost) until a new game is started.
  A move MUST NOT be rejected solely because its target cell is `UNKNOWN` (per FR-018); validation is lenient where the recognized state is uncertain.
- **FR-016**: A rejected move MUST be reported to the model as an outcome the model can act on (a clear text reason and, where useful, the current text board and/or the valid coordinate range), guiding the model toward a legal move.
- **FR-017**: When the post-action screenshot cannot be recognized as a saolei board, the tool MUST return an explicit "unable to recognize" outcome and MUST NOT fabricate or persist a false board state; subsequent cell operations MUST be rejected until a valid board is recognized (or a new game is started).
- **FR-018**: Validation MUST be lenient toward recognition uncertainty: a move MUST NOT be rejected solely because some recognized cells are `UNKNOWN`. Only concrete, provable rule violations cause rejection.
- **FR-019**: The desktop-facing operation contract MUST remain unchanged: `saolei_init` dispatches the new-game keypress; the cell tools translate grid `(x,y)` via the retained geometry (`projects/game/agent/src/mcp/saolei/geometry.ts`) and dispatch the same proto `FlowPart` operation as before. Only **what the tool returns to the model** and **what it checks first** change.
- **FR-020**: The four-tool surface (`saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`) MUST remain unchanged; no tool is added or removed for this feature.
- **FR-021**: The built-in saolei skill (`projects/game/agent/src/skill/saolei/SKILL.md`) MUST be updated to tell the model that tool results return a **text** board (with the symbol legend), and that illegal moves are rejected with a reason — replacing any guidance that says the model should read a returned screenshot.
- **FR-022**: The desktop conversation bubble for a saolei tool result MUST show the recognized **text** board only; it MUST NOT display the captured screenshot. The screenshot is consumed internally for recognition (and may be logged), never rendered in the conversation. (Consistent with [023](../023-saolei-mcp-refine/spec.md) C8/FR-010: the bubble renders from the tool's LLM result, which is now text. Native mouse-tool bubbles are unaffected and may still show their screenshot.)

**Operation-result channel separation (FlowResultPart)**

- **FR-023**: The content model MUST introduce a `FlowResultPart` that carries the desktop's outcome of an executed `FlowPart` operation (status + message + screenshot) over the **control channel** (`FlowParts`), as the operation-channel counterpart to the operation-request kinds (mouse/keyboard). It corresponds shape-wise to `ToolResultPart` but MUST NOT be rendered in the conversation — it is a `FlowPart` kind, not a `MessagePart`.
- **FR-024**: The desktop MUST report each operation's outcome as a `FlowResultPart` (in a `flow_parts` frame) and MUST NOT use the display `tool_result` `MessagePart` to carry operation-execution results. The display `tool_result` `MessagePart` is emitted by the agent (from the tool's LLM result) — the desktop no longer emits a `tool_result` `MessagePart` for an operation outcome.
- **FR-025**: `OperationBridge.handleResult` MUST consume `FlowResultPart` (control channel) to resolve the pending dispatch; the bridge's internal `OperationResult` shape is unchanged. The agent's tool layer translates the `FlowResultPart` into the display `tool_result` per tool (text board for saolei per FR-012; text + screenshot for native mouse tools).
- **FR-026**: `FlowResultPart.screenshot` is the control-channel carrier for the post-action screenshot. It MUST NOT appear as model-facing image content except where a tool explicitly includes it in its display `tool_result` (native mouse tools). For saolei it is consumed for recognition only (FR-012/FR-022). This keeps the screenshot in the control channel and the model-facing display text-only for saolei.

### Key Entities *(include if feature involves data)*

- **Selected window (single source of truth)**: the window currently chosen in the desktop session chat page, used directly as the target for screenshots and operations. Replaces the prior separate "bound window" concept.
- **Recognized saolei board state**: the structured, text-renderable game board derived deterministically from a screenshot by `@dominion/game-saolei-board`. Maintained across calls within a game (`init` / `updateFromScreenshot`); the basis for both the text returned to the model and the move validation.
- **Move-validation outcome**: for a requested cell operation, either "legal → dispatch" or "illegal → rejected with reason", decided against the recognized state before any dispatch to the desktop.
- **Saolei tool result (revised return contract)**: text board (and/or rejection reason) returned to the model, **without** a model-facing image block; the screenshot is an internal input to recognition.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of turns, selecting a window (without any Capture/bind action) and then running an agent operation results in the operation executing against that window and a post-action screenshot being returned — zero "no window bound" failures when a window is selected.
- **SC-002**: In 100% of turns against large/high-DPI windows (screenshots at or above the prior 5 MiB ceiling), the image is delivered between desktop and agent without a frame-size failure, and the turn completes.
- **SC-003**: In 100% of saolei tool results, the model receives a **text** board (never a model-facing screenshot image), produced by `@dominion/game-saolei-board`.
- **SC-004**: In 100% of cases, an illegal saolei move (no active game, out-of-bounds coordinate, impermissible action on a revealed cell, or post-terminal move) is rejected **before dispatch**, with a clear reason, and the desktop receives no operation for it.
- **SC-005**: In 100% of legal saolei moves, the operation dispatches as before and the returned text board reflects the updated state.
- **SC-006**: In 100% of saolei turns, zero operations are dispatched against illegal targets — cells that are revealed, flagged, out-of-bounds, or in a terminal game state — as a result of deterministic pre-dispatch validation against the recognized state (SC-004), and the recognized text board accurately reflects the actual game board in the screenshot (verified by the `@dominion/game-saolei-board` golden test suite), eliminating the pixel-reading errors inherent to the stateless model-reads-screenshot baseline.

## Assumptions

- The recognition engine `@dominion/game-saolei-board` is correct and sufficient for the target game (classic Win32 Microsoft Minesweeper, board geometry origin 24/200, cell 32×32px, per its README). Its monotonic cross-screenshot validation (`init`/`updateFromScreenshot`) is the mechanism used to maintain state; this feature does not re-implement recognition.
- The recognized saolei state is held **in-memory in the per-session MCP server**, co-located with the LangChain LLM checkpoint on the agent service. Their lifecycles are unified: on agent restart both are lost together, so there is no mismatch scenario (state lost while checkpoint persists, or vice versa). No persistence/checkpointing of the recognized state and no reconnect-state-recovery logic is required (out of scope). This is why `saolei_init` being destructive (F2 = new game) is not a recovery hazard: a lost session starts a new game rather than trying to rebuild state non-destructively.
- The screenshot still must reach the **agent** so `saolei-board` (a TypeScript library consumed by the agent) can recognize it. Therefore Problem 2 (image-transfer hardening) still applies to the desktop→agent path even though the **model** no longer receives the image. (Moving recognition to the desktop/Go side so no image crosses the WS leg is an alternative architecture, but it is **out of scope** here because the chosen recognition library is TypeScript/agent-side.)
- The set of game-rule validations follows standard Minesweeper rules. The validation **strictness** was decided in clarification (Session 2026-07-26): **strict** — reject any move whose target-cell state makes it a no-op or impermissible per the rules, per the enumerated FR-015(a)–(f). Chord is permitted only on a revealed number (1–8); a chord whose adjacent-flag count does not match is still a legal, permitted move (it may reveal nothing) and is NOT rejected, because validation judges target-cell compatibility rather than predicted outcome.
- The **transport mechanism** for Problem 2 is a **plan-time** architecture decision (binary WS frames with protobuf, compression, cropping/resizing the image to what recognition needs, chunking analogous to the existing SSE path, or a dedicated binary upload channel). This spec fixes only the *outcome* (reliable, bounded, efficient delivery); the chosen mechanism must satisfy FR-007 through FR-011.
- The proto `FlowPart` operation contract (the desktop-facing operations) is unchanged; only the saolei tool **return** contract (text vs. image) and the **validation** contract are new. Whether the text board is carried as a new proto field, a message convention, or an MCP content-block text is a plan-time interface detail (Constitution §III) constrained by FR-012/FR-016.
- The desktop conversation rendering, the `MessagePart`/`FlowPart` content-model split, the tool-bubble behavior, and the debug-mode hold from [023](../023-saolei-mcp-refine/spec.md) are **out of scope** and must remain compatible with the saolei tool-result changes here. The one in-scope consequence for the conversation is FR-022: a saolei tool-result bubble shows the **text** board only (no screenshot), because the bubble renders from the tool's LLM result which is now text; no desktop-side screenshot mirror is re-introduced.
- Reversing 023's "stateless + no validation" decision (FR-017/FR-018 there) for the recognition-and-validation dimension is intended: the agent re-introduces **recognized** state (deterministically derived from screenshots, not manually tracked) and **pre-dispatch** validation. This is a deliberate, scoped reversal, not a regression of the 023 content-model work.
- The agent service remains the large-test SUT (per Constitution principle VI); the desktop client is verified by build + unit + manual per `style/large_test.md`.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Repository-Internal References

- `specs/018-saolei-mcp/spec.md` — the original saolei MCP feature (desktop-facing operation contract retained here).
- `specs/023-saolei-mcp-refine/spec.md` — the stateless refactor whose "no state, no validation" premise (FR-017/FR-018) is **partially reversed** here for the recognition + validation dimension.
- `projects/game/pkg/saolei-board/README.md` — the recognition library consumed by this feature: `recognizeBoard`, `SaoleiBoard.init`/`updateFromScreenshot`, `renderBoardText`, the text-board symbol legend, and the monotonic cross-screenshot validation semantics.
- `projects/game/desktop/frontend/src/App.svelte` — `selectedWindowHandle` (L125), the window dropdown (L880-885), the Capture button (L886-888), and `handleCaptureScreenshot` (L770-788, the only caller of `bindWindow` today).
- `projects/game/desktop/app.go` — `App.boundWin` (L200), `BindWindow` (L1249-1277), `CaptureScreenshot` (L1279-1309, requires `boundWin`), the "no window bound" failure (L1074), the post-action screenshot guard (L1129-1150), `SendUserTurn` image path (L693-758), `maxScreenshotBytes` (L667).
- `projects/game/desktop/app_operation.go` — `runMouseMoveAndClick` (L28) and `runMouseMove` (L94) read `a.boundWin.Handle`.
- `projects/game/desktop/internal/api/websocket.go` — `SendFrame`/`RecvFrame` (L62-106): protojson → base64 → single WS text frame.
- `projects/game/desktop/internal/chatstream/chunk.go` — `maxFragmentBytes` (L19, 48 KiB): existing reliable chunking, SSE-only today.
- `projects/game/game.proto` — `ImagePart` (L348-355), `AgentFrame` (L452-474), the `FlowPart` operation kinds and `ToolResultPart` (the desktop-facing contract, unchanged).
- `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` — `resultFromDispatch` (L118-126, returns text + image today), the four tools (L159-275), `cellInputSchema` (L288-295, no validation today).
- `projects/game/agent/src/mcp/saolei/geometry.ts` — the retained grid→pixel geometry.
- `projects/game/agent/src/operation-bridge.ts` — `handleResult` (L261-285): screenshot → `OperationResult` (the path recognition will consume).

### External

- Minesweeper rules (standard, classic Win32 `winmine.exe`) are the authority for the FR-015 validation set; no single external normative document is newly authoritative, and the rules are common knowledge for the game. The `saolei-board` color references (cited in its README) are background only.
