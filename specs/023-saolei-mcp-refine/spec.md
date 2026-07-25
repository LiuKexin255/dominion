# Feature Specification: Conversation Content-Model Refactor & Saolei MCP Simplification

**Feature Branch**: `023-saolei-mcp-refine`

**Created**: 2026-07-25

**Status**: Draft

**Input**: User description: "对于 specs/018-saolei-mcp/ saolei mcp：1) 移除 mcp 中格子状态校验、格子状态记录与 saolei_update 工具；2) desktop session 对话页面，tools 气泡展示输入参数；3) specs/022-desktop-debug-mode/ 当中提到的 mcp tool 在 session 重新进入后全部状态为 fail（之前成功的 tool 状态也是 fail）仍没有修复，再次进入 session 后全部 tool 都是 fail。" — evolved in clarification into a content-model refactor (conversation-vs-control split) that subsumes items 2 and 3, plus the saolei stateless simplification (item 1).

## Motivation

Three problems surfaced after [018 — Saolei MCP](../018-saolei-mcp/spec.md), [021 — Agent Session Resync](../021-agent-session-resync/spec.md), and [022 — Desktop Debug Mode](../022-desktop-debug-mode/spec.md). Investigation showed items 2 and 3 share a single architectural root cause, so the team chose a **refactor over a patch** (Constitution §II): unify the desktop conversation to render from one source of truth — the LLM messages — and split operation/control signals out of the conversation entirely.

**Root cause (confirmed by reading the code).** The desktop conversation renders tool calls and tool results from **two different sources** that diverge:

| | tool call (input) | tool result (outcome) |
|---|---|---|
| **Live** | the operation Part the desktop receives (`MouseMoveAndClickPart` / `KeyboardPressPart` — pixel coordinates only; no tool name, no arguments). The agent never streams the model's tool_call. | the desktop's own execution result, mirrored to the chat stream (real status + screenshot). |
| **History (re-enter)** | reconstructed from the checkpointed `AIMessage.tool_calls` by `toolCallToPart` — which only knows `mouse_move`/`mouse_click`, so saolei tool calls produce **no** input part at all. | reconstructed from the checkpointed `ToolMessage` by `reconstructToolResult`, with status **guessed** by `inferToolResultStatus` (returns `SUCCEEDED` only if the text contains "ok"/"succeeded", else `FAILED`). |

The real `ToolResultStatus` is known to the bridge at dispatch time but is **never carried into the `ToolMessage`** — `projects/game/agent/src/tools/shared/result-blocks.ts` `buildResultBlocks` writes only `result.message` (a string) + the screenshot. So on re-entry the status is lost and guessed; every saolei result message lacks "ok"/"succeeded" and flips to `FAILED`. 021's live-dispatch fix (stream-scoped sink) did not touch this path, which is why the symptom persists.

This single divergence explains all of item 3 (history shows spurious `failed`), most of item 2 (saolei operation parts are not even rendered live — `partKind()` does not recognize `keyboard_press`/`mouse_move_and_click`), and the live/history inconsistency. The fix is to make **both** live and history draw from the LLM messages, and to make operation/control signals a separate, non-displayed channel.

The third item (saolei stateless) is independent simplification: the agent-side grid-state model, validation layer, operate-then-update alternation, and `saolei_update` tool (018 FR-005/FR-010/FR-011/FR-013..FR-019) are removed; the four surviving tools become stateless dispatchers.

## Relationship

- Refines [018 — Saolei MCP](../018-saolei-mcp/spec.md): the four surviving tools keep their desktop-dispatch behavior and proto operation Parts; removed are the agent-side state/validation layer and the `saolei_update` tool.
- Builds on [021 — Agent Session Resync](../021-agent-session-resync/spec.md): the `OperationBridge.pushResult` display-only forwarder (added for `saolei_update`) becomes unused after US3; its disposition is a plan-time cleanup (it has no other consumer).
- Adapts [022 — Desktop Debug Mode](../022-desktop-debug-mode/spec.md): the tool-result hold and Confirm control are re-anchored onto the new content model (US4) — the hold stays at the same point (before the desktop returns the result to the agent), the Confirm button stays on the conversation bubble, and the control path and the conversation-render path associate via `tool_id`.
- Interface (Constitution §III) is settled here at the message-structure level (see Clarifications C1..C12 and the Key Entities). The exact proto field numbers and the `ToolCallPart.args` representation are plan-time details constrained by the requirements.

## Clarifications

### Session 2026-07-25

- **C1 (merge / scope)**: The content-model refactor is the spine of this single feature; the tool-display behavior (items 2+3 root fix) and the saolei stateless simplification (item 1) all ride on it. One feature, one plan, one set of phases. Not split into a prerequisite feature.
- **C2 (no backward compatibility)**: The proto changes are a **clean break**. Old sessions / checkpoints / frames are not migrated and need not remain readable. No compatibility shim is built.
- **C3 (content-model split — the spine)**: The single `Part` oneof is split into two disjoint categories:
  - **`MessagePart`** (display only — rendered in the conversation, persisted-equivalent in history): `text`, `thinking`, `image`, **`tool_call`** (new), `tool_result`.
  - **`FlowPart`** (control only — drives desktop execution and turn control; never rendered in the conversation): `mouse_move`, `mouse_click`, `keyboard_press`, `mouse_move_and_click`, and the control signals `wait` / `warn` / `status` (moved out of the `AgentFrame.payload` oneof into `FlowPart` kinds).
  - `PartBlock` is replaced by `MessageParts { repeated MessagePart }` (display) and `FlowParts { repeated FlowPart }` (control). `AgentFrame.payload` becomes `oneof { MessageParts message_parts; FlowParts flow_parts; }`. `Message.content` (was `PartBlock`) becomes `MessageParts`.
- **C4 (new ToolCallPart)**: A new `ToolCallPart` carries the model's tool invocation as displayed content: the tool `name`, its input `args`, and a `tool_id` that links the call to its subsequent `tool_result` MessagePart and to the `FlowPart` operation it dispatches. This makes the conversation show the **semantic** tool input (name + arguments) — resolving the prior semantic-vs-physical display question without a special-case hack.
- **C5 (single evolving bubble)**: A tool call and its result render as **one** conversation bubble, not two. The bubble first appears showing the `tool_call` (name + args); when the matching `tool_result` arrives (same `tool_id`) the **same** bubble is updated to also show the result (status + message + screenshot) — no new entry is appended. This applies identically to live frames and replayed history.
- **C6 (tool_id threading)**: The `tool_call` MessagePart, the `FlowPart` operation the tool dispatches, and the `tool_result` MessagePart MUST share one `tool_id`, sourced from the LangChain tool_call id, so that (a) the conversation can group a call and its result into one evolving bubble, and (b) the debug-mode Confirm control on the bubble associates with the held operation result. (Today the bridge mints its own `tool_id` at dispatch, disconnected from the tool_call id; this is reconciled here.)
- **C7 (tool execution results go to the log, not the conversation)**: Because operation Parts are no longer rendered, the desktop's execution-side outcome (what operation ran and its succeeded/failed outcome) is surfaced in the **log panel** rather than the conversation. This is largely already emitted by the 022 DEBUG logging in `projects/game/desktop/app.go` (`recvLoop` / `executeAgentOperation`); the requirement is that the execution outcome is reachable in the log, not the conversation. The **screenshot is explicitly out of scope for the log** — surfacing images in the log panel is a larger change that is not needed now; the screenshot is shown only in the conversation tool-result bubble (per C8), not duplicated into the log.
- **C8 (screenshots come from the LLM tool result)**: The screenshot shown in a tool-result bubble is the one the tool includes in its result to the model (the `ToolMessage` image), for both live and history. The desktop still captures the screenshot as part of executing the operation; it reaches the conversation via the tool's LLM result (the agent emits the `tool_result` MessagePart after the tool returns), not via a desktop-side mirror. (The mouse tools already include the screenshot in their LLM result via `buildResultBlocks`; the saolei tools do via `resultFromDispatch`.)
- **C9 (tool-result status is the real outcome, not guessed)**: The real `ToolResultStatus` (resolved by the bridge when the desktop returns) MUST be carried into the tool's LLM-result representation and through the checkpoint, so `ListMessages` reconstructs the status directly — the text-heuristic `inferToolResultStatus` (current `projects/game/agent/src/handler.ts`, returns `FAILED` unless the message contains "ok"/"succeeded") MUST NOT remain the source of truth. A result whose real status is unavailable MUST be shown as unspecified/neutral, never defaulted to `FAILED`.
- **C10 (debug hold re-anchor)**: The 022 hold stays anchored at the same point — the desktop holds after executing the operation and **before returning the result to the agent** (the control / `FlowPart` path). The Confirm control stays on the conversation bubble. The control path and the conversation-render path proceed independently and are associated via `tool_id` (C6); their relative ordering is not relied upon. During the hold the execution outcome is visible in the log (C7); the bubble shows the `tool_call` and updates with the `tool_result` only after the hold releases and the agent produces the tool's LLM result.
- **C11 (saolei stateless scope)**: "格子状态记录" = the per-session grid model (`GameState` in `projects/game/agent/src/mcp/saolei/game-state.ts`). "格子状态校验" = the rule validators (`projects/game/agent/src/mcp/saolei/validation.ts`). "saolei_update 工具" = the `saolei_update` MCP tool registration. All removed together. The grid→pixel geometry (`projects/game/agent/src/mcp/saolei/geometry.ts`) is retained — the four surviving tools still translate grid `(x,y)` to the fixed board pixel layout. With no state, `saolei_init`'s `width`/`height` arguments have no effect and are dropped — `saolei_init` becomes a bare F2 new-game dispatch (the model infers board bounds from the returned screenshot, as it already must for unseen cells).
- **C12 (no screenshot during a debug hold)**: During a debug hold the conversation bubble shows only the `tool_call` (name + args); the captured screenshot is **not** displayed during the hold. The screenshot becomes visible only in the `tool_result` once it arrives — i.e. after the hold releases and the agent produces the LLM tool result (per C8 / FR-010). Inspecting the screenshot before the result is returned to the agent is explicitly out of scope (the demand is low); the sole requirement is that the screenshot is reachable in the tool result.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The Conversation Renders Only LLM Messages; Operation/Control Signals Are a Separate Non-Displayed Channel (Priority: P1)

The desktop conversation page shows the model's messages: text, thinking, images, **tool calls** (with their name and input arguments), and **tool results**. The mouse/keyboard operation Parts and the wait/warn/status control signals are no longer rendered in the conversation — they drive desktop execution and turn control separately. Live tool calls and the same calls replayed from history render identically, because both are derived from the LLM messages (the `AIMessage` tool_calls and the following `ToolMessage`). A tool call and its result appear as one evolving bubble: the call shows first, and the same bubble updates with the result when it arrives.

**Why this priority**: This is the architectural spine that fixes the live/history divergence (the root cause of items 2 and 3) and that everything else hangs on. Without it the status fix has no consistent surface to land on.

**Independent Test**: Run a turn with tool calls; confirm each tool call renders one bubble showing the tool name + arguments, and that bubble later updates to show the result (status + screenshot); confirm no operation Part or control signal appears as a conversation entry. Leave and re-enter the session and confirm the same bubbles appear identically.

**Acceptance Scenarios**:

1. **Given** a turn in which the model calls a tool, **When** the call is emitted, **Then** the conversation shows a bubble displaying the tool name and its input arguments.
2. **Given** a tool-call bubble, **When** the tool's result arrives, **Then** the **same** bubble is updated to show the result (status, message, screenshot) — no new conversation entry is appended for the result.
3. **Given** a turn, **When** operation Parts (mouse/keyboard) and control signals (wait/warn/status) flow, **Then** none of them appear as conversation entries; they are handled as control only.
4. **Given** a turn observed live and then replayed from history, **When** the history is rendered, **Then** the tool-call/result bubbles are identical between the live view and the replayed view (same content, same status, same screenshots).
5. **Given** a non-saolei (mouse-tool) session, **When** a turn runs, **Then** mouse tool calls render as tool-call bubbles with their arguments and update with their results (the prior `mouseMove`/`mouseClick` operation bubbles are gone).

---

### User Story 2 - Tool Results Show Their Real Status in History (No Spurious "Failed") (Priority: P1)

After leaving and re-entering a session, every historical tool result shows the **same** success/failure status it had during the live turn. A tool operation that succeeded live still reads as succeeded in history; one that genuinely failed still reads as failed. The status is the actual outcome the desktop returned — never a guess from the result text, and never a default-to-failed. A result whose real status is not available is shown as neutral, not failed.

**Why this priority**: This is the correctness defect that corrupts the entire history view after any session navigation (item 3). It is the most visible bug and is independent of the saolei simplification.

**Independent Test**: Run a turn with at least two successful tool operations and one genuine failure; leave and re-enter; confirm the successes still show succeeded and the failure still shows failed.

**Acceptance Scenarios**:

1. **Given** a turn whose tool operations succeeded live, **When** the session is re-entered, **Then** those results show as succeeded in history — identical to their live status.
2. **Given** a turn in which a tool operation genuinely failed, **When** the session is re-entered, **Then** that result still shows as failed (the fix does not mask real failures).
3. **Given** a tool result whose real status is unavailable, **When** it is rendered, **Then** it shows an unspecified/neutral status — never `FAILED` by default.
4. **Given** the result text does not contain "ok"/"succeeded", **When** the status is determined, **Then** it is taken from the carried real status, not inferred from the text.

---

### User Story 3 - Saolei MCP Becomes Stateless: No Grid State, No Validation, No saolei_update (Priority: P1)

The saolei MCP exposes exactly four tools — `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click` — each dispatching its operation to the desktop and returning the result. There is no agent-side grid-state model, no operation/update alternation, no validation, and no `saolei_update` tool. Tools are callable back-to-back with no intervening step. The built-in saolei skill is updated to match.

**Why this priority**: Explicit team decision (item 1). Removes accidental complexity now that the desktop reliably returns a screenshot the model can read.

**Independent Test**: Configure a saolei profile; run a turn calling `saolei_init` then `saolei_click` then `saolei_click` again (no update between); confirm only the four tools are exposed, the second click is accepted, and `saolei_update` does not exist.

**Acceptance Scenarios**:

1. **Given** a saolei-enabled session, **When** an MCP client lists tools, **Then** exactly four are exposed (`saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`); `saolei_update` is absent.
2. **Given** a saolei session, **When** the model calls `saolei_click` twice in succession, **Then** both dispatch and return results — no "must update first" rejection.
3. **Given** any saolei cell operation, **When** it is called, **Then** it dispatches the same proto operation Part as before (`saolei_init` → `KeyboardPressPart{F2}`; the others → `MouseMoveAndClickPart` at the cell center with the same click action and `WINDOW_MESSAGE`) and returns the desktop's result + screenshot — the desktop-facing contract is unchanged.
4. **Given** any operation 018 previously rejected on state grounds, **When** it is called, **Then** it is no longer rejected by the agent — it dispatches and the real game outcome is reflected in the returned screenshot.
5. **Given** a saolei-enabled session, **When** the prompt is assembled, **Then** the injected saolei skill describes the four tools and contains no `saolei_update`, no alternation, and no validation-rejection guidance.

---

### User Story 4 - Debug-Mode Hold Is Re-Anchored onto the New Model (Priority: P2)

With debug mode on, the desktop still holds each tool operation's result **after execution and before returning it to the agent**, and the Confirm control still appears on the conversation bubble for that tool call. The control path (execute → hold → release → return) and the conversation-render path (tool_call bubble → later tool_result update) proceed independently and are associated via the shared `tool_id`. During the hold, the execution outcome is visible in the log; the bubble shows the tool call and updates with the result after the hold releases. Auto-continue (15 min) and the agent backstop (20 min) still apply.

**Why this priority**: This adapts the existing 022 behavior to the refactored content model. It depends on US1 (the new model and `tool_id` threading); it is the lowest-priority story but is required so the debug experience is not broken by the refactor.

**Independent Test**: Toggle debug on; run a turn with a tool operation; confirm the Confirm button appears on that tool-call bubble and the agent does not advance until confirmed (or auto-continue); after confirm the bubble updates with the result; the execution detail is visible in the log during the hold.

**Acceptance Scenarios**:

1. **Given** debug mode on and a tool operation just executed, **When** the result would be returned to the agent, **Then** the desktop holds it and the Confirm control appears on that tool-call's conversation bubble; the agent does not advance.
2. **Given** a held result and the Confirm control on the bubble, **When** the user clicks Confirm, **Then** the result is returned to the agent, the agent resumes, the control is removed, and the bubble updates with the result.
3. **Given** a held result the user does not confirm, **When** 15 minutes elapse, **Then** the desktop auto-continues (returns the result, dismisses the control).
4. **Given** debug mode on, **When** an operation is held, **Then** the execution outcome is visible in the log panel during the hold.
5. **Given** debug mode off, **When** a tool operation runs, **Then** the result returns immediately with no hold and no Confirm control — identical to a non-debug run.

---

### Edge Cases

- **One bubble, late or missing result**: if a `tool_result` never arrives for a `tool_call` (e.g. the turn was aborted), the bubble remains in the "called, not yet resolved" state showing the tool call; it is not left in an inconsistent half-updated state.
- **Multiple tool calls in one turn**: each call gets its own evolving bubble, updated independently by `tool_id`; results do not collide across calls.
- **A tool call with no corresponding FlowPart operation** (an agent-internal tool, e.g. any future tool that resolves server-side): it still renders a tool-call bubble that updates with its result; it simply has no operation Part executed on the desktop.
- **History written before this feature**: not supported (Clarification C2 — clean break, no backward compatibility). Old sessions are not expected to render correctly under the new model.
- **Stateless tools and an out-of-bounds coordinate**: with validation removed, a model coordinate outside the board dispatches to whatever pixel the fixed formula yields; the desktop executes it and the returned screenshot is the model's feedback. Accepted tradeoff of removing agent-side validation.
- **`pushResult` after `saolei_update` is removed**: it currently has no other consumer. Whether it is retained as a general mechanism or removed is a plan-time cleanup; if removed, the bridge's dispatch/handleResult contract must remain intact.
- **Debug toggled off mid-hold** / **leaving the session with a held result**: the held result is released (returned or torn down per existing 017/022 behavior); the bubble is not left with a dangling Confirm control.
- **Mouse-tool result source change**: mouse tool results previously shown via the desktop's execution-result mirror are now shown via the tool's LLM result (which already includes the screenshot via `buildResultBlock`). The net display is equivalent; the source shifts from a desktop mirror to an agent emission.
- **No screenshot during a debug hold**: during a debug hold the bubble shows only the `tool_call`; the captured screenshot is not displayed until the `tool_result` arrives after the hold releases (C12). The screenshot is reachable in the tool result (FR-010).

## Requirements *(mandatory)*

### Functional Requirements

**Content-model split & unified conversation rendering**

- **FR-001**: The content model MUST split `Part` into two disjoint categories: a display `MessagePart` (text / thinking / image / tool_call / tool_result) and a control `FlowPart` (mouse_move / mouse_click / keyboard_press / mouse_move_and_click / wait / warn / status). A given content block belongs to exactly one category.
- **FR-002**: A new `ToolCallPart` MUST carry the model's tool invocation as display content: the tool `name`, its input `args`, and a `tool_id`.
- **FR-003**: `AgentFrame.payload` MUST be `oneof { MessageParts message_parts; FlowParts flow_parts; }`, where `MessageParts` / `FlowParts` are each a `repeated` list of their respective part kind. The prior standalone `content` / `wait` / `warn` / `status` payload cases are replaced (the signals become `FlowPart` kinds).
- **FR-004**: `Message.content` MUST be `MessageParts` (the display blocks); control/flow parts MUST NOT appear in a `Message`.
- **FR-005**: The conversation page MUST render only `MessageParts`; `FlowParts` (operation Parts and wait/warn/status signals) MUST NOT be rendered as conversation entries — they are consumed for desktop execution and turn control only.
- **FR-006**: The agent MUST emit a `tool_call` MessagePart (carrying name + args + tool_id) when the model invokes a tool, and a `tool_result` MessagePart (same tool_id) when the tool's LLM result is produced — so the live conversation shows the same tool calls/results that history replays.
- **FR-007**: A tool call and its result MUST render as a single conversation bubble. The bubble first shows the `tool_call` (name + args); when the matching `tool_result` arrives (same `tool_id`) the SAME bubble MUST be updated to show the result (status + message + screenshot) rather than appending a new entry.
- **FR-008**: The `tool_id` MUST be consistent across the `tool_call` MessagePart, the `FlowPart` operation the tool dispatches, and the `tool_result` MessagePart (sourced from the LangChain tool_call id), so bubble grouping and the debug Confirm association work.
- **FR-009**: Live tool-call/result bubbles and replayed-history tool-call/result bubbles MUST render identically for the same turn (single source of truth: the LLM messages).
- **FR-010**: The screenshot shown in a tool-result bubble MUST be the screenshot the tool includes in its LLM result (the `ToolMessage` image), for both live and history.

**Tool execution results go to the log**

- **FR-011**: The desktop's tool-execution outcome (the operation performed and its succeeded/failed outcome — **not** the screenshot) MUST be reachable in the log panel; it MUST NOT be rendered as a conversation entry. The screenshot is shown only in the conversation tool-result bubble (FR-010) and is intentionally NOT surfaced in the log. (The 022 DEBUG logging already emits most of this execution detail; the requirement is reachability of the operation + status in the log, not the conversation.)

**Tool-result status correctness (the history fix)**

- **FR-012**: The real `ToolResultStatus` resolved when the desktop returns a result MUST be carried into the tool's LLM-result representation and through the checkpoint, so that history reconstruction reads the actual status.
- **FR-013**: `ListMessages` MUST reconstruct each historical tool result with the status actually returned at the time of the operation — succeeded live ⇒ succeeded in history; failed live ⇒ failed in history.
- **FR-014**: A historical tool result whose real status is unavailable MUST be shown with an unspecified/neutral status; it MUST NOT be defaulted to `FAILED`.
- **FR-015**: No tool result's status MAY be inferred from its message text. The text-heuristic inference (current `inferToolResultStatus`) MUST NOT remain the source of truth; if any fallback remains, it MUST default to unspecified/neutral, never `FAILED`.

**Stateless saolei MCP**

- **FR-016**: The saolei MCP MUST expose exactly four tools — `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`. The `saolei_update` tool MUST be removed.
- **FR-017**: The agent MUST NOT maintain a per-session saolei grid-state model (no grid of cell statuses, no `pendingUpdate`, no `lastOp`, no `initialized` flag). The four tools carry no mutable session state between calls.
- **FR-018**: The agent MUST NOT validate saolei operations against game-state rules. There MUST be no pre-dispatch rejection on state grounds and no operation→update alternation.
- **FR-019**: `saolei_init` MUST dispatch the F2 new-game keypress and return the result + screenshot; it MUST NOT take grid-dimension arguments that affect agent-side state. Re-calling it re-dispatches F2.
- **FR-020**: `saolei_click` / `saolei_flag` / `saolei_chord_click` MUST each translate grid `(x,y)` via the unchanged fixed board geometry and dispatch the same proto operation Part as before (`MouseMoveAndClickPart`, `WINDOW_MESSAGE`, the respective click action), then return the desktop's result + screenshot. The desktop-facing contract is unchanged.
- **FR-021**: The four tools MUST be callable back-to-back with no intervening step; a second operation MUST NOT be rejected for lack of an update.
- **FR-022**: The built-in saolei skill (`projects/game/agent/src/skill/saolei/SKILL.md`) MUST describe exactly the four tools, the top-left-origin `(x,y)` convention, and reading the returned screenshot to track the board. It MUST NOT reference `saolei_update`, the alternation, the cell-status reporting contract, or validation rejection.

**Debug-mode hold re-anchor**

- **FR-023**: With debug mode on, the desktop MUST hold a tool operation's result after execution and before returning it to the agent (the control / `FlowPart` path) — the same hold point as 022 — and surface the Confirm control on that tool-call's conversation bubble.
- **FR-024**: The control path (execute → hold → release → return) and the conversation-render path (tool_call bubble → tool_result update) MUST associate via the shared `tool_id` (FR-008); their relative ordering MUST NOT be relied upon.
- **FR-025**: Clicking Confirm MUST release the held result to the agent, dismiss the control, and (once the agent produces the tool result) update the bubble with the result.
- **FR-026**: If the user does not confirm within 15 minutes, the desktop MUST auto-continue (release the result, dismiss the control); the agent's 20-minute dispatch backstop remains in place.
- **FR-027**: With debug mode off, tool results MUST return to the agent immediately with no hold and no Confirm control — identical to a non-debug run.

### Key Entities *(include if feature involves data)*

- **MessagePart vs FlowPart (content categories)**: the two disjoint content categories. `MessagePart` is what the conversation renders and what `Message` carries (text / thinking / image / tool_call / tool_result). `FlowPart` is the control channel (mouse/keyboard operations + wait/warn/status), consumed for desktop execution and turn control, never rendered in the conversation.
- **ToolCallPart (new)**: the display representation of a model tool invocation — tool name, input arguments, and a `tool_id` linking it to its `tool_result` and to the `FlowPart` operation it dispatches.
- **Tool-result status (carried)**: the actual `ToolResultStatus` of an operation, carried from the desktop result through the tool's LLM result into the checkpoint, so history reconstruction reflects the real outcome.
- **Evolving tool bubble**: one conversation bubble per tool call, identified by `tool_id`, that shows the call first and is updated in place with the result when it arrives.
- **Saolei tool set (stateless)**: the four stateless tools, each a pure dispatch-and-return over the existing `OperationBridge`, with no per-session mutable game state.
- **Debug hold (re-anchored)**: the desktop's pre-return hold of an operation result (control path), with its Confirm control on the conversation bubble, associated to the held result via `tool_id`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of turns, each tool call renders a single bubble showing the tool name + arguments and later updates in place with its result; operation Parts and wait/warn/status signals never appear as conversation entries.
- **SC-002**: In 100% of turns, the live tool-call/result bubbles and the same turn replayed from history render identically (content, status, screenshots).
- **SC-003**: In 100% of reconnect cases, a tool operation that succeeded live shows as succeeded after re-entering the session; one that genuinely failed shows as failed; an unknown status shows as neutral — zero spurious "failed" results from history replay.
- **SC-004**: In 100% of saolei sessions, exactly four tools are exposed, back-to-back operations are accepted with no "must update first" rejection, and `saolei_update` is absent.
- **SC-005**: In 100% of debug-mode holds, the Confirm control appears on the correct tool-call bubble (associated via `tool_id`), the agent does not advance until confirmed or auto-continued, and the execution outcome is visible in the log during the hold.
- **SC-006**: With debug mode off, tool dispatch, tool-result return, and conversation rendering behave exactly as in a non-debug run (no hold, no Confirm control).

## Assumptions

- The four surviving saolei tools retain their desktop-facing operation contract from 018 (`KeyboardPressPart{F2}` for init; `MouseMoveAndClickPart` at the fixed board center with `WINDOW_MESSAGE` for the cell operations). The grid→pixel geometry (`projects/game/agent/src/mcp/saolei/geometry.ts`) is retained.
- History persistence is the LangChain checkpoint of `BaseMessage`s (MemorySaver); the proto `Message`/`AgentFrame` are transport/reconstruction types, not the persistence format. `ListMessages` reconstructs proto `MessageParts` from the `BaseMessage`s. Per Clarification C2 (no backward compatibility) the proto change is a clean break; old sessions are not expected to render under the new model. (Final confirmation that no proto-level persistence exists is a plan-time check.)
- The agent can surface tool-call events from the LangGraph stream (the `AIMessage` tool_calls are available before tool execution), so emitting a `tool_call` MessagePart live — consistent with history reconstruction from `AIMessage.tool_calls` — is feasible.
- The mouse tools already include the post-action screenshot in their LLM result via `buildResultBlock` (`projects/game/agent/src/tools/shared/result-blocks.ts`); the saolei tools do via `resultFromDispatch`. So FR-010 (screenshots from the LLM tool result) is already satisfied at the source for both tool families; the work is to render that source consistently and stop the desktop-side mirror-for-display.
- The exact representation of `ToolCallPart.args` (structured map / arg list / JSON) is a plan-time interface detail; the requirement is that it carries the tool name and arguments sufficient to display the call's input.
- The 022 DEBUG logging in `projects/game/desktop/app.go` already emits tool-execution/result detail; FR-011 requires reachability in the log, which is largely already met.
- The desktop conversation page and the agent service are the surfaces touched; no new project or external dependency is introduced. The agent (gRPC service) remains the large-test SUT; the desktop (client) is verified by build + unit + manual per `style/large_test.md`.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Repository-Internal References

- `specs/018-saolei-mcp/spec.md`, `specs/018-saolei-mcp/contracts/mcp-tool-contract.md` — the feature being refined (five tools → four; state/validation/update removed).
- `specs/021-agent-session-resync/spec.md` — prior reconnect work; `pushResult` (now saolei-unused) disposition is a plan-time cleanup.
- `specs/022-desktop-debug-mode/spec.md`, `specs/022-desktop-debug-mode/contracts/debug-control-plane.md` — the hold/Confirm behavior re-anchored by US4.
- `projects/game/game.proto` — `Part` oneof, `PartBlock`, `AgentFrame.payload` oneof (`content`/`wait`/`warn`/`status`), `Message.content`, `KeyboardPressPart`, `MouseMoveAndClickPart`, `ToolResultPart`/`ToolResultStatus`, `WaitSignal`/`WarnSignal`/`StatusSignal` — the content model being split.
- `projects/game/agent/src/handler.ts` — `Connect` (frame emission: text/reasoning blocks at lines ~385–409; the bridge sink registration at ~360); `ListMessages` + `inferToolResultStatus` (~line 776) + `reconstructToolResult` + `toolCallToPart` (the history reconstruction path fixed by US1/US2).
- `projects/game/agent/src/operation-bridge.ts` — `dispatch` (writes the operation Part as a content frame at ~239; mints its own `tool_id` — to be reconciled with the tool_call id per FR-008), `handleResult` (where the real status is resolved), `pushResult`.
- `projects/game/agent/src/tools/shared/result-blocks.ts` — `buildResultBlocks` (writes only `result.message` + screenshot into the `ToolMessage`; the real status is not carried — the US2 gap).
- `projects/game/agent/src/tools/mouse_click/mouse-click.ts`, `projects/game/agent/src/tools/mouse_move/mouse-move.ts` — mouse tools (dispatch + `buildResultBlocks`).
- `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`, `game-state.ts`, `validation.ts`, `geometry.ts` — the saolei MCP (state/validation/update removed by US3; geometry retained).
- `projects/game/agent/src/skill/saolei/SKILL.md` — the built-in skill (updated by US3).
- `projects/game/desktop/app.go` — `recvLoop` (~650; auto-executes operation Parts and mirrors content to the chat stream), `handleInboundOperation` (~728; the hold point), `executeAgentOperation` (~869).
- `projects/game/desktop/frontend/src/components/ChatView.svelte` — part rendering (handles `mouseMove`/`mouseClick`/`toolResult` today; to render `MessagePart`s only and group tool_call+result into one evolving bubble).
- `projects/game/desktop/frontend/src/api.ts` — `Part`/`partKind()` (do not recognize the saolei part kinds today; to be split into `MessagePart`/`FlowPart`).

### External

- No external specifications are newly authoritative for this feature. Minesweeper rules cited by 018 are background only and are not re-cited here, since US3 removes the agent-side rule validation that depended on them. LangGraph streaming / `AIMessage.tool_calls` mechanics are consulted in `plan.md` per Constitution §III.
