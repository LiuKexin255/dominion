# Feature Specification: Tool Bubble Rendering & Saolei Coordinate Accuracy

**Feature Branch**: `024-tool-render-coord-fix`

**Created**: 2026-07-26

**Status**: Draft

**Input**: User description: "@specs/023-saolei-mcp-refine/ 其中几个变更进行优化和修复 1. session 对话框中新的 tool 内容没有气泡，只有文字例如 saolei_init / {} / ✗ failed saolei_init: F2 dispatched (new game)，并且执行成功也展示 failed。tool message 来源已经从 llm message 流中获取，failed/success 判断逻辑是否也应该同步修改 2. saolei 操作坐标转换是否有问题，desktop 接收到的操作是像素位置(168, 344)，格子式 (4,4) ，但实际点击的格子是 (4,8)，y 向下移动了 100+px。检查是否叠加了其他鼠标位移的逻辑，应当去掉。"

## Motivation & Root Cause

Two defects surfaced after implementing [023 — Conversation Content-Model Refactor & Saolei MCP Simplification](../023-saolei-mcp-refine/spec.md). Investigation (reading the code) traced each to a concrete root cause; both are correctness/UX regressions of the 023 desktop surface, not new requirements.

### Defect 1 — The tool bubble shows no styling and reads "failed" even on success

The session conversation renders a tool call as bare text — the tool name, its `{}` args, and a `✗ failed <message>` line — with no bubble box, and a successful saolei operation still reads "failed". The tool-result message itself is correct (it comes from the LLM message stream, e.g. `"saolei_init: F2 dispatched (new game)"`); only the *container styling* and the *status judgment* are wrong.

Two independent root causes, both confined to the desktop tool-bubble renderer (`projects/game/desktop/frontend/src/components/ChatView.svelte`):

| # | Symptom | Root cause (confirmed by reading the code) |
|---|---|---|
| 1a | "no bubble, only text" | The 023 refactor renamed the tool-bubble template to new class names (`.tool-bubble`, `.tool-head`, `.tool-name`, `.tool-args`, `.tool-result`, `.tool-pending`, `.tool-resolved-success`/`-failure`) but the component `<style>` block was **not** updated — it still holds the pre-refactor definitions (`.op-card`, `.op-result-card`, `.op-result-success`/`-failure`, …) that the new template no longer references. A repo-wide search for any `.tool-*` selector in `ChatView.svelte` finds **none**, so the tool call renders as unstyled text. |
| 1b | "even success shows failed" | Saolei (MCP) tool results carry the **neutral** status `TOOL_RESULT_STATUS_UNSPECIFIED` (proto enum value 0) per 023 C15 / research.md D12, and the agent emits exactly that (live: `projects/game/agent/src/llm.ts` `consumeToolResults` defaults to `STATUS_UNSPECIFIED`; history: `projects/game/agent/src/handler.ts` `readToolResultStatus` defaults to `STATUS_UNSPECIFIED`). **protojson omits fields equal to their default value**, so the `status` field is dropped on the wire and the frontend observes `status: undefined`. The renderer's success check (`isToolResultSucceeded`) returns false for an absent status, and its status-text ternary falls through to `"failed"`. Native (mouse) tools carry `SUCCEEDED`/`FAILED` (non-zero), which protojson always emits, so they are unaffected — the bug is specific to neutral-status results. |

Per the user's note ("tool message 来源已经从 llm message 流中获取，failed/success 判断逻辑是否也应该同步修改"), the fix is to make the status judgment read from the tool-result status carried from the LLM stream — where saolei is neutral — rather than treating an absent field as failure. This completes 023's item-3 fix (023 FR-014: a result whose real status is unavailable MUST be neutral, **never FAILED**), which the protojson-omission gap had left half-done for the live desktop surface.

### Defect 2 — A saolei cell click lands on the wrong cell (systematic downward drift)

A `saolei_click` at grid `(4, 4)` dispatches pixel `(168, 344)` but the click lands several rows too low (the operator reported ~cell `(4, 8)`). The user hypothesises compounded mouse-displacement logic; the code shows the execution path is clean, and the real cause is a coordinate-space mismatch:

- The grid→pixel geometry (`projects/game/agent/src/mcp/saolei/geometry.ts`: `BOARD_ORIGIN_X_PX = 24`, `BOARD_ORIGIN_Y_PX = 200`, `CELL_SIZE_PX = 32`, `center(x,y)`) was supplied in [018 research.md D6](../018-saolei-mcp/research.md) / [018 data-model.md §5](../018-saolei-mcp/data-model.md) and labelled "window-client coordinates". `BOARD_ORIGIN_Y_PX = 200` corresponds to the board's top edge measured from the **full window's** top — the coordinate space of the screenshot, since `projects/game/desktop/internal/capture/capture.go` `CaptureWindow` captures the entire window (bounds from DWM extended-frame bounds / `GetWindowRect`, which include the non-client chrome: title bar, menu bar, borders).
- A `WINDOW_MESSAGE` click, however, posts `WM_*` messages whose `lParam` are **client** coordinates (origin at the client-area top-left, which **excludes** the non-client chrome) — see `projects/game/desktop/app_operation.go` `runMouseMoveAndClick` (WINDOW_MESSAGE branch) → `projects/game/desktop/internal/operation/window_message_windows.go` `ExecuteWindowMessageClick` → `makeLPARAM`. Confirmed by reading: the WINDOW_MESSAGE path applies **no** `ScreenshotToScreenCoords`, **no** `SetCursorPos`/`MoveCursor`, **no** foreground-move — it packs the agent-supplied coordinate verbatim into `lParam`.
- Therefore the agent sends client-y `344` intending "4 rows below the board top", but in the client coordinate space that point is lower than in the screenshot/full-window space, because the non-client chrome (title + menu + borders, operator-measured **96 px**) is absent from the client origin. Net: every click drifts down by the chrome height (96 px ≈ 3 rows), so `(4,4)` lands several rows too low (the operator reported ~row 8).

The fix is to make the grid→pixel translation match the click's coordinate space (client) so clicks land on the intended cell — a **refactor over a patch** (Constitution §II): reconcile the coordinate space, do not layer an opposing offset to "cancel" the drift.

## Relationship

- Fixes defects found while running [023 — Conversation Content-Model Refactor & Saolei MCP Simplification](../023-saolei-mcp-refine/spec.md): completes 023 FR-014 (neutral status, never FAILED) on the live desktop surface, and makes 023's evolving tool bubble (FR-007) actually visible.
- The saolei tool set (023 US3 / FR-016..FR-022) and the four-tool stateless contract are **unchanged** — only the grid→pixel calibration and the conversation renderer change.
- The desktop-facing operation contract from [018 — Saolei MCP](../018-saolei-mcp/spec.md) (`MouseMoveAndClickPart` with `WINDOW_MESSAGE`, client-coord `lParam`) is **unchanged** — the desktop already posts the exact coordinate it receives; the fix corrects the coordinate the agent computes, not the posting.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Tool Calls Render as Styled Bubbles with Accurate Status (Priority: P1)

In the desktop session conversation, every tool call renders as one visually distinct **bubble** — a bordered box showing the tool name, its input arguments, and (when the result arrives) the outcome. A tool result whose status is neutral/unavailable (the saolei/MCP case) shows a **neutral** state — never "failed"; a result that genuinely succeeded shows "succeeded", and one that genuinely failed shows "failed". The status is taken from the tool-result status carried from the LLM message stream (neutral for saolei per 023 C15), so a successful saolei operation no longer reads as a failure. Live turns and history replay render the bubbles identically.

**Why this priority**: This is the most visible defect — the conversation shows broken tool calls (unstyled text + spurious "failed") on every saolei turn, which both looks broken and misleads the operator about whether the operation worked. It is the direct continuation of 023's item-3 correctness fix.

**Independent Test**: Run a saolei turn that calls `saolei_init` then `saolei_click`; confirm each tool call appears as a bordered bubble (name + args), the init shows a neutral/succeeded outcome (not "failed") when F2 dispatched, and the click bubble shows the result; leave and re-enter the session and confirm the same bubbles appear identically.

**Acceptance Scenarios**:

1. **Given** a turn in which the model calls a tool, **When** the call is emitted, **Then** the conversation shows a **bordered bubble** containing the tool name and its input arguments (not unstyled plain text).
2. **Given** a tool-call bubble, **When** the tool's result arrives, **Then** the same bubble updates in place to show the result (outcome + message + screenshot) — no new conversation entry is appended.
3. **Given** a saolei (neutral-status) tool result, **When** it is rendered (live or from history), **Then** it shows a **neutral** state — it MUST NOT read "failed".
4. **Given** a tool result whose real status is `SUCCEEDED`, **When** it is rendered, **Then** it reads "succeeded"; **Given** one whose real status is `FAILED`, **Then** it reads "failed" (the fix does not mask real failures and does not regress native-tool display).
5. **Given** a turn observed live and then replayed from history, **When** the history is rendered, **Then** the tool bubbles are identical between the live view and the replayed view (same content, same status, same styling).

---

### User Story 2 - A Saolei Cell Click Lands on the Intended Cell (Priority: P1)

A `saolei_click` / `saolei_flag` / `saolei_chord_click` at grid `(x, y)` clicks the **actual** cell `(x, y)` of the bound Minesweeper window — no systematic row/column drift. A click intended for `(4, 4)` lands on cell `(4, 4)`, not `(4, 8)`. The returned screenshot reflects the intended cell's state.

**Why this priority**: Without this, saolei is unusable — every cell operation hits the wrong cell, so the model's view of the board (read from screenshots) never matches its actions and it cannot play. It is independent of US1 and can be fixed/verified on its own.

**Independent Test**: Bind a Minesweeper window; call `saolei_init` then `saolei_click(4, 4)`; confirm from the returned screenshot (or the game's visible state) that cell `(4, 4)` — not `(4, 8)` — was revealed/affected.

**Acceptance Scenarios**:

1. **Given** a bound Minesweeper window and a fresh game, **When** `saolei_click(x, y)` is called for an in-bounds cell, **Then** the click lands on cell `(x, y)` (verified by the returned screenshot showing that cell's state change).
2. **Given** the bound window, **When** `saolei_flag` / `saolei_chord_click` are called at `(x, y)`, **Then** they likewise land on cell `(x, y)` — the fix applies uniformly to all three cell-operation tools.
3. **Given** a sequence of clicks at varying `(x, y)`, **When** they are dispatched, **Then** every click lands on its intended cell with no systematic downward offset (the non-client-chrome drift is compensated).
4. **Given** the click coordinate the agent computes, **When** the desktop posts it via `WINDOW_MESSAGE`, **Then** the desktop posts exactly that coordinate with no additional cursor displacement or screen-offset compounded on top (the current clean WINDOW_MESSAGE posting behavior is preserved — no regression).

---

### Edge Cases

- **Tool result with no status field on the wire**: because protojson omits default-value (enum-zero) fields, a neutral result may arrive with `status` absent entirely. The renderer MUST treat an absent status identically to an explicit `UNSPECIFIED` — neutral, never "failed".
- **Tool call whose result never arrives** (e.g. turn aborted): the bubble stays in the "called, not yet resolved" state (existing "running…" behavior) — unaffected.
- **Native (mouse) tool results**: they carry `SUCCEEDED`/`FAILED` (non-zero), which protojson always emits, so their display is unchanged. The status fix MUST NOT alter native-tool display.
- **Out-of-bounds grid coordinate** (023 accepted tradeoff): with validation removed, a coordinate outside the board still dispatches to whatever pixel the geometry yields; this feature does not re-introduce validation. The geometry fix is about *calibration*, not bounds-checking.
- **Different Minesweeper window layouts**: the geometry targets the standard / modern Microsoft Minesweeper the operator binds (the layout 018 D6 targeted). If a different minesweeper implementation is bound, the board origin may differ; the plan records the calibration target and the fix keeps the constants in one place (`geometry.ts`) so they can be re-tuned without touching the tool contracts.
- **Coordinate-space consistency for screenshots vs clicks**: the model reads the board from screenshots (full-window space) but acts via grid coordinates, so a screenshot-space-vs-client-space difference does not affect play *as long as clicks land correctly*. Reconciling the two spaces (so a future pixel-accurate screenshot↔grid mapping holds) is a plan-time consideration, not a play-blocking requirement.

## Requirements *(mandatory)*

### Functional Requirements

**Tool bubble rendering (US1)**

- **FR-001**: A tool call and its result MUST render in the conversation as a single, visually distinct **bubble** — a bordered container showing the tool name and its input arguments, and (when resolved) the result — not as unstyled/plain text.
- **FR-002**: A tool result whose status is neutral/unspecified — whether expressed explicitly as `UNSPECIFIED` or **absent on the wire** (protojson default-value omission) — MUST render as a **neutral** state; it MUST NOT render as "failed".
- **FR-003**: The tool-result status shown in the conversation MUST be taken from the status carried from the LLM message stream (neutral for MCP/saolei tools per 023 C15/D12; `SUCCEEDED`/`FAILED` for native mouse tools). The status judgment MUST NOT infer failure from an absent field.
- **FR-004**: A tool result whose real status is `SUCCEEDED` MUST render as succeeded; one whose real status is `FAILED` MUST render as failed. The fix MUST NOT mask genuine failures or regress native-tool status display.
- **FR-005**: A tool call and its result MUST render as one evolving bubble that updates in place when the result arrives (consistent with 023 FR-007); live turns and history replay MUST render the tool bubbles identically.

**Saolei coordinate accuracy (US2)**

- **FR-006**: A `saolei_click` / `saolei_flag` / `saolei_chord_click` at grid `(x, y)` MUST land the click on cell `(x, y)` of the bound Minesweeper window — no systematic row or column offset. (A click intended for `(4, 4)` MUST land on `(4, 4)`, not `(4, 8)`.)
- **FR-007**: The grid→pixel translation used for `WINDOW_MESSAGE` clicks MUST be expressed in the **client** coordinate space — the space of the `WM_*` `lParam` the desktop posts — so the geometry matches the click's coordinate space. The screenshot→client compensation MUST be applied in the agent (the operation originator that dispatches the mouse op), not the desktop; it MUST NOT apply a screenshot / full-window (non-client-chrome-inclusive) origin to client-space coordinates.
- **FR-008**: The desktop MUST post the click at exactly the grid→pixel coordinate the agent supplies; the `WINDOW_MESSAGE` path MUST apply no additional cursor displacement, screen-offset, or compounded move on top of the agent-supplied coordinate (current behavior preserved — no regression).
- **FR-009**: The coordinate fix MUST apply uniformly to `saolei_click`, `saolei_flag`, and `saolei_chord_click` (all three share the grid→pixel geometry).

### Key Entities

- **Tool bubble (evolving)**: one conversation bubble per tool call, keyed by the conversation-channel `tool_call.id`, showing the call (name + args) first and updating in place with the result (status + message + screenshot). Defect 1 is purely about this surface rendering correctly.
- **Tool-result status (carried, neutral-by-default)**: the outcome status carried from the LLM message stream. Native tools carry `SUCCEEDED`/`FAILED`; MCP (saolei) tools carry neutral (`UNSPECIFIED`). On the wire (protojson) a neutral status may be omitted; the renderer must treat absent-as-neutral.
- **Grid→client-pixel geometry**: the fixed formula translating grid `(x, y)` to the pixel sent in a `WINDOW_MESSAGE` click. It MUST be calibrated in the client coordinate space so clicks hit the intended cell.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of turns, each tool call renders a visually distinct bordered bubble (tool name + arguments + result); no tool call appears as unstyled plain text.
- **SC-002**: In 100% of cases, a neutral/unspecified tool result (including one whose `status` is absent on the wire) renders as neutral — never as "failed"; native `SUCCEEDED`/`FAILED` results render correctly. Net: zero spurious "failed" tool results in the conversation.
- **SC-003**: In 100% of in-bounds saolei cell operations, the click lands on the intended cell `(x, y)` (verified by the returned screenshot); there is no systematic row/column offset.
- **SC-004**: Live turns and history replay render tool bubbles identically (content, status, styling).

## Assumptions

- The tool-result message source is already the LLM message stream (the user confirmed this), and the agent already carries neutral status for saolei (023 C15/D12) and `SUCCEEDED`/`FAILED` for native tools (023 Phase 3). Defect 1 is confined to the desktop renderer's styling and status interpretation; no agent-side status change is required.
- protojson serialization (used by the desktop view model / chat stream) omits proto fields equal to their default value, so a neutral (`UNSPECIFIED`, enum 0) `status` is absent on the wire; the renderer must be robust to an absent status. (If a future change enables `EmitDefaults`, an explicit `UNSPECIFIED` must still render as neutral — the requirement holds either way.)
- The grid→pixel geometry constants live in one place (`projects/game/agent/src/mcp/saolei/geometry.ts`); re-calibrating them does not change the four saolei tools' contracts or the desktop-facing `WINDOW_MESSAGE` operation Part.
- The desktop `WINDOW_MESSAGE` click path already posts the exact client coordinate the agent supplies (verified by reading `app_operation.go` + `window_message_windows.go`); Defect 2 is fixed at the geometry/coordinate-space level, not by altering the posting logic.
- The model acts via grid coordinates `(x, y)` and does not require pixel-accurate screenshot↔grid mapping to play; reconciling screenshot space and client space for future pixel-accurate reads is a plan-time consideration, not a play-blocking requirement.
- The bound Minesweeper window is the standard / modern Microsoft Minesweeper the operator targets; the board-origin constants are window-layout-specific and may need re-tuning if a different implementation is bound.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Repository-Internal References

- `specs/023-saolei-mcp-refine/spec.md` — the feature whose implementation surfaced both defects (FR-007 evolving bubble; FR-014 neutral-never-FAILED; C15/D12 saolei neutral status).
- `specs/023-saolei-mcp-refine/research.md`, `specs/023-saolei-mcp-refine/data-model.md` — 023 design context (D10/D12 decoupling + neutral status; data-model §5/§6 bubble + status).
- `specs/018-saolei-mcp/research.md` D6, `specs/018-saolei-mcp/data-model.md` §5 — origin of the grid→pixel geometry (`BOARD_ORIGIN_X_PX=24`, `BOARD_ORIGIN_Y_PX=200`, `CELL_SIZE_PX=32`), labelled "window-client coordinates".
- `projects/game/desktop/frontend/src/components/ChatView.svelte` — the tool-bubble renderer: template uses `.tool-bubble`/`.tool-head`/`.tool-name`/`.tool-args`/`.tool-result`/`.tool-pending`/`.tool-resolved-*` classes; `<style>` lacks those rules (Defect 1a); `isToolResultSucceeded` + the status-text ternary treat absent status as "failed" (Defect 1b).
- `projects/game/desktop/frontend/src/api.ts` — `ToolResultPart.status` (protojson enum-name string / numeric), `ToolResultStatus` enum, `messagePartKind`.
- `projects/game/agent/src/mcp/saolei/geometry.ts` — `BOARD_ORIGIN_*` constants + `center(x,y)` (Defect 2 geometry).
- `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` — the four stateless tools dispatch via `center(x,y)` into `MouseMoveAndClickPart`/`KeyboardPressPart`.
- `projects/game/desktop/app_operation.go` — `runMouseMoveAndClick` WINDOW_MESSAGE branch posts the exact client coordinate (no offset / no cursor move).
- `projects/game/desktop/internal/operation/window_message_windows.go`, `projects/game/desktop/internal/operation/window_message_logic.go` — `ExecuteWindowMessageClick` / `makeLPARAM` pack client coords into `WM_*` `lParam`.
- `projects/game/desktop/internal/capture/capture.go`, `projects/game/desktop/internal/capture/window.go` — `CaptureWindow` captures the **full window** (DWM extended-frame bounds / `GetWindowRect`), i.e. the screenshot/full-window coordinate space.
- `projects/game/agent/src/handler.ts` — live tool_result emission (~line 457) and `ListMessages` reconstruction + `readToolResultStatus` (default `STATUS_UNSPECIFIED`, ~line 844).
- `projects/game/agent/src/llm.ts` — `consumeToolResults` (status defaults to `STATUS_UNSPECIFIED`, ~line 635).

### External

- proto3 JSON mapping — default-value (enum-zero) fields are omitted on the wire: https://protobuf.dev/programming-guides/json/ (relevant to Defect 1b: a neutral `UNSPECIFIED` status is absent in protojson).
- Win32 `WM_LBUTTONDOWN` / `WM_RBUTTONDOWN` — `lParam` carries **client** coordinates: https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-lbuttondown
- `DwmGetWindowAttribute` (`DWMWA_EXTENDED_FRAME_BOUNDS`) vs `GetClientRect` — full-window bounds (incl. non-client chrome) vs client area: https://learn.microsoft.com/en-us/windows/win32/api/dwmapi/nf-dwmapi-dwmgetwindowattribute
