# Quickstart: Conversation Content-Model Refactor & Saolei MCP Simplification

**Feature**: 023-saolei-mcp-refine | **Date**: 2026-07-25 | **Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md)

This is a validation/run guide — it documents the runnable scenarios that prove the feature works end-to-end. It references the contracts and data model for shapes rather than duplicating them. Implementation details belong in `tasks.md`.

## Prerequisites

- Repo builds clean: `bazel build //...` (agent TS + desktop Go + proto regen).
- Agent unit tests pass: `bazel test //projects/game/agent/...`.
- Desktop unit tests pass: `bazel test //projects/game/desktop/...`.
- The `game_types` TS proto types and the Go proto types are regenerated (`bazel run //:gazelle projects/game/agent projects/game/desktop` after the proto change).

## Scenario map

| # | Scope | What it proves | Spec ref |
|---|---|---|---|
| 1 | unit (agent) | `tool_id` threading: tool reads `config.toolCall.id` → dispatch stamps it on the FlowPart; fallback to UUID when absent. | FR-008, D2 |
| 2 | unit (agent) | Real status survives `MemorySaver`: `ToolMessage.additional_kwargs.toolResultStatus` round-trips; `ListMessages` reads it; no text inference; absent → UNSPECIFIED. | FR-012..FR-015, D4 |
| 3 | unit (agent) | Stateless saolei MCP: exactly 4 tools; back-to-back `saolei_click` accepted (no `saolei_update`, no alternation); `saolei_init` has no `width`/`height`. | FR-016..FR-022, D7 |
| 4 | unit (agent) | Live `tool_call`/`tool_result` emission: `generateTurn` yields the new blocks from `AIMessage.tool_calls` / `ToolMessage`. | FR-006, D5 |
| 5 | large (agent service) | End-to-end saolei flow over the deployed agent: init→click→click (back-to-back); the dispatched operation Parts are the unchanged 018 proto operations; the result status is carried through. | FR-020/FR-021, US3 |
| 6 | large (agent service) | History status correctness: run a turn with a succeeded + a failed tool op, leave/re-enter, `ListMessages` returns the same statuses (no spurious "failed"). | FR-012/FR-013, US2 |
| 7 | large (agent service) | Conversation renders only LLM messages: a tool turn yields a `tool_call` + `tool_result` (MessageParts); operations (FlowParts) are not in `Message.content`. | FR-005/FR-006/FR-009, US1 |

> Large-test organisation: per `style/large_test.md`, scenarios 5–7 are added as **cases/suites in the existing** `projects/game/testplan/system_test.yaml` (suites `agent-saolei`, `agent-operation`, `checkpoint-resume`, `agent-dialog`), NOT as new test-plan YAMLs. The existing `agent_saolei_test.go`, `agent_operation_test.go`, `agent_checkpoint_test.go`, `agent_dialog_test.go` are updated to the new content model. Acceptance = `guitar run projects/game/testplan/system_test.yaml` with **all cases passing** (Constitution §VI).

---

## Scenario 1 — `tool_id` threading (unit)

**File**: `projects/game/agent/src/operation-bridge.test.ts` (+ `tools/mouse_click/mouse-click.test.ts` / `mouse_move/mouse-move.test.ts`).

1. **Given** a fake `OperationBridge` whose `dispatch` captures the `FlowPart` it receives, and a `RunnableConfig` carrying `{ toolCall: { id: "call_abc" } }`.
2. **When** `createMouseClickTool(bridge)` is invoked with that config.
3. **Then**:
   - The dispatched `FlowPart.mouseMoveAndClick.tool_id` (or `mouseClick.tool_id`) equals `"call_abc"` (not a fresh UUID).
   - The returned `ToolMessage.tool_call_id` equals `"call_abc"`.
4. **Given** a config with **no** `toolCall.id`.
5. **When** dispatched.
6. **Then** the `tool_id` is a non-empty UUID (fallback path), and dispatch still resolves.

Reference: [contracts/tool-dispatch-contract.md](contracts/tool-dispatch-contract.md) §1..§2.

---

## Scenario 2 — Real status through `MemorySaver` (unit)

**File**: `projects/game/agent/src/tools/shared/result-blocks.test.ts` + `projects/game/agent/src/handler.test.ts`.

1. **Given** an `OperationResult { status: "TOOL_RESULT_STATUS_SUCCEEDED", message: "ok", screenshot: {...} }`.
2. **When** `buildToolResultMessage(result, "call_1", "saolei_click")` is called and the resulting `ToolMessage` is round-tripped through a `MemorySaver` checkpoint (`adapter.getState`).
3. **Then** the restored `ToolMessage.additional_kwargs.toolResultStatus === "TOOL_RESULT_STATUS_SUCCEEDED"`, and the `content` image block survived (screenshot reachable).
4. **Given** the round-tripped `ToolMessage`, **when** `ListMessages` reconstructs it → the `tool_result` MessagePart has `status = TOOL_RESULT_STATUS_SUCCEEDED`.
5. **Given** a `ToolMessage` with **no** `additional_kwargs.toolResultStatus` → reconstructed `tool_result` status is `TOOL_RESULT_STATUS_UNSPECIFIED` (neutral, **not** `FAILED`).
6. **Given** a result message text that does **not** contain "ok"/"succeeded" but whose real status is `SUCCEEDED` → reconstructed status is still `SUCCEEDED` (proves no text inference; `inferToolResultStatus` is gone).

Reference: [contracts/tool-dispatch-contract.md](contracts/tool-dispatch-contract.md) §3..§4; [data-model.md](data-model.md) §6.

---

## Scenario 3 — Stateless saolei MCP (unit)

**File**: `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts`.

1. **Given** `createSaoleiMcpServer(fakeBridge)`.
2. **When** the server's tool list is enumerated.
3. **Then** exactly four tools are exposed: `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`; `saolei_update` is absent.
4. **Given** `saolei_init`'s inputSchema → it has **no** `width`/`height` properties.
5. **When** `saolei_click(3,4)` is called twice in succession (no intervening step).
6. **Then** both calls dispatch a `MouseMoveAndClickPart{ LEFT_CLICK, WINDOW_MESSAGE }` at `center(3,4) = (136, 344)` and return a result — the second is **not** rejected ("must update first" is gone).
7. **Then** `game-state.ts` / `validation.ts` files no longer exist (`bazel build` succeeds without them).

Reference: [contracts/tool-dispatch-contract.md](contracts/tool-dispatch-contract.md) §6; [data-model.md](data-model.md) §7.

---

## Scenario 4 — Live `tool_call` / `tool_result` emission (unit)

**File**: `projects/game/agent/src/llm.test.ts`.

1. **Given** a fake agent whose `streamEvents` yields an `AIMessage` with `tool_calls: [{ name: "saolei_click", args: { x: 3, y: 4 }, id: "call_1" }]`, followed by a `ToolMessage` with `tool_call_id: "call_1"`, `additional_kwargs.toolResultStatus: "TOOL_RESULT_STATUS_SUCCEEDED"`, content `[text "ok", image_url]`.
2. **When** `adapter.generateTurn(...)` is iterated.
3. **Then** the yielded blocks include `{ type: "tool_call", name: "saolei_click", args: {x:3,y:4}, toolCallId: "call_1" }` and later `{ type: "tool_result", toolCallId: "call_1", status: "TOOL_RESULT_STATUS_SUCCEEDED", message: "ok", screenshot: {...} }`.
4. **Then** the `tool_call` block is yielded **before** the `tool_result` block.

Reference: [contracts/tool-dispatch-contract.md](contracts/tool-dispatch-contract.md) §7; [research.md](research.md) D5.

---

## Scenario 5 — End-to-end saolei flow (large, agent service)

**Plan**: `projects/game/testplan/system_test.yaml` suite `agent-saolei` → `projects/game/testplan/agent_saolei_test.go` (updated).

**Acceptance gate**: `guitar run projects/game/testplan/system_test.yaml` (suites `agent-saolei`), all cases pass.

1. **Deploy** the agent SUT via the testplan (suite `agent-saolei` deploy config).
2. **Given** a saolei-enabled profile and a fake-LLM fixture driving `saolei_init` → `saolei_click(3,4)` → `saolei_click(5,6)` (back-to-back, no update).
3. **When** the test (playing desktop) reads the dispatched operation Parts over the WS:
   - `saolei_init` dispatches a `KeyboardPressPart{ F2 }` (FlowPart).
   - Each `saolei_click` dispatches a `MouseMoveAndClickPart{ LEFT_CLICK, WINDOW_MESSAGE }` at the cell centre (136,344) and (184,344).
4. **Then** the test echoes each back as a `ToolResultPart{ SUCCEEDED }`; the agent continues (no "must update first" rejection).
5. **Then** `ListMessages` for the session returns `Message`s whose `content.parts` include `tool_call` parts (name + `argsJson`) and `tool_result` parts (status `SUCCEEDED`, screenshot reachable) — and **no** operation FlowParts in `Message.content`.

Reference: spec US3 / FR-020; [data-model.md](data-model.md) §4/§7.

---

## Scenario 6 — History status correctness (large, agent service)

**Plan**: `projects/game/testplan/system_test.yaml` suite `checkpoint-resume` → `projects/game/testplan/agent_checkpoint_test.go` (updated).

**Acceptance gate**: `guitar run projects/game/testplan/system_test.yaml` (suite `checkpoint-resume`), all cases pass.

1. **Deploy** the agent SUT (suite `checkpoint-resume`).
2. **Given** a turn with at least two successful tool operations and one genuine failure (the fake-LLM fixture drives tool_calls; the test-acting-desktop replies SUCCEEDED for two and FAILED for one).
3. **When** the turn completes, then the WS disconnects and reconnects (leave/re-enter).
4. **Then** `ListMessages` returns the historical `tool_result` parts with the **same** statuses: the two successes read `SUCCEEDED`, the failure reads `FAILED`.
5. **Then** no historical `tool_result` reads `FAILED` unless its real status was `FAILED` (no spurious "failed").
6. **Given** a tool result whose real status is unavailable (no `additional_kwargs.toolResultStatus`) → it reads `UNSPECIFIED` (neutral), not `FAILED`.

Reference: spec US2 / FR-012..FR-015; [data-model.md](data-model.md) §6.

---

## Scenario 7 — Conversation renders only LLM messages (large, agent service)

**Plan**: `projects/game/testplan/system_test.yaml` suites `agent-dialog` + `agent-operation` → `projects/game/testplan/agent_dialog_test.go` + `agent_operation_test.go` (updated).

**Acceptance gate**: `guitar run projects/game/testplan/system_test.yaml` (suites `agent-dialog`, `agent-operation`), all cases pass.

1. **Deploy** the agent SUT.
2. **Given** a turn in which the model calls a tool (fake-LLM fixture emits a `tool_call`, the test-acting-desktop executes the operation Part and replies).
3. **When** the turn's frames are observed over the WS:
   - A `message_parts` frame carrying a `tool_call` MessagePart (name + `argsJson`) is emitted (live).
   - A `flow_parts` frame carrying the operation is emitted (control).
   - Later, a `message_parts` frame carrying the `tool_result` MessagePart (status + screenshot) is emitted.
4. **Then** `ListMessages` returns `Message`s whose `content.parts` contain `tool_call` and `tool_result` MessageParts only — **no** `mouseMove`/`mouseClick`/`keyboardPress`/`mouseMoveAndClick` parts in `Message.content` (operations are control-only).
5. **Then** the live `tool_call`/`tool_result` frames and the history-reconstructed ones carry identical content for the same turn (single source of truth).

Reference: spec US1 / FR-005/FR-006/FR-009; [data-model.md](data-model.md) §4.

---

## Manual / desktop verification (build + manual per `style/large_test.md`)

The desktop (Go + Svelte client) is verified by `bazel build` + `bazel test` (unit) + manual run, not by a large test (it is a client, not a deployed service). Manual checks against a running agent:

- **Evolving bubble**: run a tool turn; the tool call renders one bubble (name + args); the same bubble updates in place with the result when it arrives (no new entry appended). Leave/re-enter → identical bubbles (live≡history).
- **No operation entries**: mouse/keyboard operations and wait/warn signals never appear as conversation bubbles.
- **Debug hold (US4)**: toggle Debug on; run a tool turn; the Confirm button appears on the **tool_call** bubble (not a result bubble); the agent does not advance until confirmed (or 15-min auto-continue); after confirm the bubble updates with the result; the execution detail is visible in the log during the hold; the screenshot is **not** shown during the hold (only after release).
- **Status fidelity**: a succeeded tool op still reads succeeded after re-entering; a genuinely failed one still reads failed.

Reference: spec US4 / FR-023..FR-027; [data-model.md](data-model.md) §5.
