# Research: Conversation Content-Model Refactor & Saolei MCP Simplification

**Feature**: 023-saolei-mcp-refine | **Date**: 2026-07-25 | **Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md)

This document resolves the plan-time unknowns left open by the spec (the spec's Clarifications C1..C12 settle the requirements; the items below settle the *technical* decisions needed before contracts and code). Each decision carries its rationale and the alternatives rejected.

The spec's stated plan-time details to resolve are: the exact proto field numbers, the `ToolCallPart.args` representation (spec §Relationship & §Assumptions), and confirmation that no proto-level persistence exists (spec §Assumptions). Research decisions D1..D9 cover these plus the mechanics the design depends on (LangChain `tool_call.id` propagation, status survival through `MemorySaver`, the debug-hold re-anchor surface).

---

## D1 — Content-model split: two disjoint proto categories replacing the single `Part` oneof

**Decision**: Replace the single `Part` oneof (and `PartBlock`) with two disjoint message categories, each a repeated list, carried by a renamed `AgentFrame.payload` oneof:

- **`MessagePart`** (display only — what the conversation renders and what `Message` carries): `text`, `thinking`, `image`, **`tool_call`** (new), `tool_result`.
- **`FlowPart`** (control only — drives desktop execution and turn control; never rendered in the conversation): `mouse_move`, `mouse_click`, `keyboard_press`, `mouse_move_and_click`, and the control signals `wait` / `warn` / `status` (moved out of the `AgentFrame.payload` oneof into `FlowPart` kinds per spec C3 / FR-003).
- `AgentFrame.payload` becomes `oneof { MessageParts message_parts; FlowParts flow_parts; }` where `MessageParts`/`FlowParts` are each `repeated` their part kind.
- `Message.content` (was `PartBlock`) becomes `MessageParts`.
- `PartBlock` and `Part` are removed.

This is exactly spec FR-001..FR-004 / C3. The exact field numbers are pinned in [contracts/content-model-contract.md](contracts/content-model-contract.md).

**Rationale**: The spec's confirmed root cause (spec §Motivation, verified in code — see "Root cause confirmation" below) is that live and history render tool calls/results from *two different sources* that diverge. Unifying both on the LLM messages requires the conversation surface to carry the model's *semantic* tool invocation (`tool_call`), while the *physical* operation (mouse/keyboard) and turn-control signals must move off the conversation surface entirely. Two disjoint categories make "render only MessageParts" (FR-005) structural rather than a filter convention, so a FlowPart can never accidentally render as a chat entry.

**Alternatives rejected**:
- *Keep one `Part` oneof, add `tool_call`, filter FlowParts at render time.* Rejected: the live/history divergence is precisely because the conversation and the operation channel share a type today; keeping them shared perpetuates the class of bug. The spec (C3) explicitly chooses the split.
- *Move wait/warn/status out of the frame payload into a separate top-level field instead of FlowPart kinds.* Rejected: the spec (C3) explicitly says the signals become `FlowPart` kinds; a separate field would reintroduce a three-way payload distinction.

**Root cause confirmation (read from the code, not assumed)**:
- Live: `projects/game/desktop/frontend/src/api.ts:136` `partKind()` recognizes only `text`/`thinking`/`image`/`mouseMove`/`mouseClick`/`toolResult` — **not** `keyboard_press`/`mouse_move_and_click`, so saolei operation Parts never render live as input. The model's `tool_call` (name+args) is never streamed at all — only the physical operation Part the bridge emits (`projects/game/agent/src/operation-bridge.ts:238`).
- History: `projects/game/agent/src/handler.ts:756` `toolCallToPart` only knows `mouse_move`/`mouse_click` → saolei `tool_calls` produce **no** input part. Status is guessed by `inferToolResultStatus` (`handler.ts:776`) → `FAILED` unless the text contains "ok"/"succeeded". The real `ToolResultStatus` (resolved in `operation-bridge.ts:306` `handleResult`) is never carried into the `ToolMessage` — `projects/game/agent/src/tools/shared/result-blocks.ts:33` `buildResultBlocks` writes only `result.message` + screenshot. So on re-entry the status is lost and guessed. This matches spec §Motivation verbatim.

---

## D2 — `ToolCallPart` and the `tool_id` source (LangChain `tool_call.id`)

**Decision**: A new `ToolCallPart { string tool_id = 1; string name = 2; string args_json = 3; }` carries the model's tool invocation as display content. The `tool_id` is sourced from the **LangChain `tool_call.id`** — the same id LangChain assigns to each entry of `AIMessage.tool_calls` and echoes back on the matching `ToolMessage.tool_call_id`. The `tool_call` MessagePart, the `FlowPart` operation the tool dispatches, and the `tool_result` MessagePart all carry this one id (spec FR-008 / C6).

**How the id reaches the dispatch path (confirmed from source)**: a LangChain `tool()` handler receives the `RunnableConfig` as its second argument, and the tool-call id is at **`config.toolCall.id`**. This is verified by the LangChain JS runtime:
- `langchain-ai/langchainjs` `libs/langchain-core/src/tools/utils.ts` — the type guard `config is { toolCall: { id?: string } }` reading `config.toolCall.id` (https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/utils.ts).
- `n8n-io/n8n` `packages/@n8n/ai-workflow-builder.ee/src/tools/helpers/response.ts:12` — `const toolCallId = config.toolCall?.id as string;` used to build `new ToolMessage({ tool_call_id: toolCallId, ... })`.

So each tool (mouse + saolei) reads `config.toolCall?.id` and passes it to `OperationBridge.dispatch(part, toolId, signal)`; the bridge stamps that id onto the `FlowPart` operation (instead of minting its own UUID via `randomUUID()` at `operation-bridge.ts:208`). The matching `ToolMessage` produced by the agent's turn loop already carries `tool_call_id` (LangChain wires it automatically), so `ListMessages` reconstruction emits the `tool_result` MessagePart with the same id — no manual threading needed on the result side.

**Rationale**: One id, sourced from the only place that knows it at model-invocation time (the LangChain runtime), threads call→operation→result. This is the minimal change that satisfies FR-008 and makes the evolving-bubble grouping (FR-007) and the debug Confirm association (FR-023) work off one key.

**Alternatives rejected**:
- *Let the bridge keep minting its own UUID and separately map it to the tool_call id.* Rejected: adds a mapping table and a reconciliation edge case (live tool_call id vs bridge id); the spec (C6) explicitly says the bridge's self-minted id is "reconciled here" by sourcing from the tool_call id.
- *Derive the id from the `on_tool_start` streamEvent's `run_id`.* Rejected: `run_id` is the *tool execution's* run id, not the `tool_call.id`; they are different identifiers, and mapping them is brittle.

**Fallback when `config.toolCall.id` is absent**: in non-agent (direct-invoke) test paths the id may be missing. `dispatch` falls back to minting a UUID when `toolId` is `undefined`, preserving today's behaviour for any caller that does not pass one. (The production agent path always supplies it.)

---

## D3 — `ToolCallPart.args` representation

**Decision**: `string args_json` — a single JSON-encoded string carrying the model's tool-call arguments verbatim (`JSON.stringify(tool_call.args)`).

**Rationale**:
- The arguments originate as `Record<string, unknown>` on `AIMessage.tool_calls[i].args` (LangChain). A JSON string faithfully round-trips any shape (scalars, nested objects) without type coercion.
- The field is **display-only** (FR-002: "sufficient to display the call's input"); the frontend pretty-prints it (e.g. `JSON.parse` → formatted `<pre>`). No downstream code consumes it as structured data.
- Simplest proto shape; AIP-140 (field names) compliant; avoids proto `map` ordering non-determinism and the stringification loss `map<string,string>` would impose on non-string values.

**Reconstruction (history)**: `ListMessages` builds the `ToolCallPart` from `AIMessage.tool_calls[i]` → `{ tool_id: call.id, name: call.name, args_json: JSON.stringify(call.args ?? {}) }`.

**Alternatives rejected**:
- `map<string,string> args` — lossy (values must be stringified; numbers/bools/nested lose type; keys with non-string semantics get coerced).
- `repeated ToolCallArg { string name; string value_json; }` — more UI-structured, but over-engineered for a display-only field and adds a message + per-arg allocation. Reconsider only if the UI later needs per-arg table rendering.

---

## D4 — Carrying the real `ToolResultStatus` through the checkpoint

**Decision**: The tool constructs and returns a **`ToolMessage`** directly (instead of returning raw content blocks), carrying:
- `content`: the existing content-block array (`buildResultBlocks` output — status text + screenshot `image_url` + pixel-size annotation, unchanged);
- `tool_call_id`: `config.toolCall.id` (so LangChain links result↔call);
- `name`: the tool name (so history can display it without re-deriving);
- `additional_kwargs.toolResultStatus`: the **real** `ToolResultStatus` enum-name string (`"TOOL_RESULT_STATUS_SUCCEEDED"` / `"_FAILED"`), taken from `OperationResult.status` resolved by `bridge.dispatch`.

`ListMessages` reconstruction reads the status from `msg.additional_kwargs?.toolResultStatus`, defaulting to `TOOL_RESULT_STATUS_UNSPECIFIED` when absent — **never** `FAILED`. The text-heuristic `inferToolResultStatus` (`handler.ts:776`) is **removed** (FR-015).

**Rationale**:
- `additional_kwargs` is the standard `BaseMessage` extensibility channel and is serialized by `MemorySaver`'s JSON serde alongside `content` (complex `content` arrays already survive the round-trip today — see `handler.ts:541` reading checkpointed content blocks). Carrying the status there keeps it out of the model-visible text (FR-015: no text inference) and out of the screenshot.
- Returning a `ToolMessage` directly is supported by the LangChain JS `ToolNode`: when a tool's invocation returns a `ToolMessage`, the node passes it through (rather than re-wrapping the content). This is the cleanest way to set `additional_kwargs` from inside the tool.
- The tool already holds the real status (`OperationResult.status` from `bridge.dispatch`'s resolved promise — `operation-bridge.ts:323`), so no new resolution point is needed.

**Verification required at implementation** (called out as an explicit task): a unit test that round-trips a `ToolMessage` (with `additional_kwargs.toolResultStatus` + an `image_url` content block) through `MemorySaver` and asserts both the status and the screenshot survive `getState`. This guards against any future serde regression and confirms the assumption concretely.

**Alternatives rejected**:
- *Encode the status as a structured content block (e.g. `{ type: "tool_result_status", status }`) that `reconstructToolResult` parses.* Rejected: redundant with `additional_kwargs`, and the model would see an unfamiliar block in its tool result.
- *Infer from the result text with a richer heuristic.* Rejected — spec FR-015 explicitly forbids text inference as the source of truth.
- *Use `ToolMessage.artifact`.* Rejected: `artifact` serde through `MemorySaver` is less battle-tested than `additional_kwargs`; `additional_kwargs` is the lower-risk choice.
- *Carry status only on the proto `ToolResultPart` (transport) and not in the checkpoint.* Rejected — spec FR-012/013 requires the status to survive the checkpoint so history reads the real outcome.

---

## D5 — Live emission of `tool_call` and `tool_result` MessageParts

**Decision**: The adapter's `generateTurn` (`projects/game/agent/src/llm.ts:399`) is extended to yield two new `ContentBlock` variants — `{ type: "tool_call"; name; args; toolCallId }` and `{ type: "tool_result"; toolCallId; status; message; screenshot? }` — by inspecting the streamed messages:
- An `AIMessage` carrying `tool_calls` → yield one `tool_call` block per call (name, args, `tool_call.id`). This fires when the model finishes emitting the tool-call (before the ToolNode executes it), so the `tool_call` bubble appears before the operation runs.
- A `ToolMessage` → yield a `tool_result` block from its content blocks (message + screenshot) and `additional_kwargs.toolResultStatus`.

The `Connect` handler (`handler.ts:379` turn loop) emits these as `MessageParts` content frames (a frame carrying a single `tool_call` or `tool_result` MessagePart). This makes the live conversation show the same tool calls/results that history replays (FR-006 / FR-009).

**Mechanism (confirmed from docs)**: the agent already uses `agent.streamEvents(..., { version: "v3" })` and iterates `stream.messages` (`llm.ts:455`, `llm.ts:468`). LangGraph's `streamEvents` v3 surfaces every message produced by the graph, including the `AIMessage` (with `tool_calls`) and the subsequent `ToolMessage` produced by the tool node. The exact accessor for detecting "the streamed AI message now carries tool_calls" is verified against the pinned `@langchain/langgraph` ^1.4.8 / `langchain` ^1.5.3 at implementation time (the repo's `spike.test.ts:298` already documents `AIMessageChunk.tool_calls` / `tool_call_chunks`).

**Rationale**: emitting live from the same `BaseMessage` stream that the checkpoint stores guarantees live≡history (FR-009) for free — both are projections of the identical source.

**Alternatives rejected**:
- *Switch from `stream.messages` to raw `streamEvents` event-by-event (`on_chat_model_end`, `on_tool_start`, `on_tool_end`).* Rejected as the primary path: it is a larger rewrite of the working text/reasoning streaming loop. Kept as a fallback if `stream.messages` cannot expose `tool_calls` reliably at the right time. (The `on_tool_start` event's `data.input` + `name` is a documented source for the call args/name — https://api.js.langchain.com/classes/langchain_core_runnables.Runnable.html — but its id linkage is via `run_id`, not `tool_call.id`, which D2 already rejects.)

---

## D6 — Single evolving bubble (tool_call + tool_result grouped by `tool_id`)

**Decision**: The desktop frontend (`ChatView.svelte`) groups a `tool_call` MessagePart and the later `tool_result` MessagePart that shares its `tool_id` into **one** conversation bubble (spec FR-007 / C5). The bubble first renders the call (tool name + `args_json`); when the matching `tool_result` arrives (same `tool_id`) the **same** bubble is updated in place to also show the result (status + message + screenshot) — no new entry is appended. This applies identically to live frames and replayed history.

**Implementation shape**: the frontend keeps a per-`tool_id` view model for tool bubbles. A `tool_call` part creates/updates the entry; a `tool_result` part (matched by `tool_id`) merges the result into the existing entry. If a `tool_result` arrives with no preceding `tool_call` (e.g. a very old history edge), the entry is created from the result alone (graceful degradation, spec Edge Case "History written before this feature" is out of scope per C2, but the render path still degrades safely).

**Rationale**: one bubble per call is the UX the spec mandates (C5) and the surface the debug Confirm control anchors onto (D8).

**Alternatives rejected**: two separate bubbles (call, then result). Rejected — spec C5 explicitly chooses one evolving bubble; two bubbles re-fragments the conversation the split was meant to unify.

---

## D7 — Stateless saolei MCP (remove state, validation, `saolei_update`)

**Decision**: The saolei MCP is rewritten to expose exactly four stateless tools — `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click` — each a pure dispatch-and-return over the existing `OperationBridge`. Removed:
- `projects/game/agent/src/mcp/saolei/game-state.ts` (the per-session `GameState` grid model — spec C11 "格子状态记录").
- `projects/game/agent/src/mcp/saolei/validation.ts` (the rule validators — spec C11 "格子状态校验").
- `projects/game/agent/src/mcp/saolei/validation.test.ts`.
- The `saolei_update` tool registration and the operate-then-update alternation (`pendingUpdate`/`lastOp`) (spec FR-016..FR-018).

Retained unchanged: `projects/game/agent/src/mcp/saolei/geometry.ts` (the fixed grid→pixel formula — FR-020 / Assumption). `saolei_init` drops its `width`/`height` arguments (they affected only agent-side state — C11) and becomes a bare F2 dispatch; the other three keep their `(x, y)` schema and dispatch the same proto operation Parts as before (`MouseMoveAndClickPart`, `WINDOW_MESSAGE`, the respective click action) — the **desktop-facing contract is unchanged** (FR-020). `createSaoleiMcpServer(bridge)` drops its `initialState` parameter. The built-in skill `src/skill/saolei/SKILL.md` is rewritten to describe the four tools, the top-left-origin `(x,y)` convention, and reading the returned screenshot — with no `saolei_update`, no alternation, and no validation-rejection guidance (FR-022).

**`pushResult` disposition** (spec Edge Case): `OperationBridge.pushResult` (`operation-bridge.ts:281`) was added for `saolei_update`'s display-only forwarding (021). After `saolei_update` is removed it has **no consumer**. It is **removed** (not retained as dormant surface) — Principle II (don't leave dead surface). The bridge's `dispatch`/`handleResult` contract stays intact; only the one-way display forwarder goes.

**Rationale**: the desktop reliably returns a screenshot the model can read; the agent-side grid model duplicated that state and the validation layer rejected operations the model could otherwise learn from. Removing them is net simplification (spec §Motivation item 3, explicit team decision).

**Alternatives rejected**: keep `pushResult` as a general mechanism. Rejected — no consumer, and leaving it invites future misuse; YAGNI.

---

## D8 — Debug-mode hold re-anchored onto the `tool_call` bubble

**Decision**: The 022 debug hold stays at the **same point** — the desktop holds after executing the operation and before returning the result to the agent (`projects/game/desktop/app.go:728` `handleInboundOperation`, between `executeAgentOperation` and `ws.SendFrame`) (spec C10 / FR-023). What changes is the **association surface**:
- The Confirm control moves from the (now-gone) desktop-mirrored result bubble onto the **`tool_call`** conversation bubble, matched by `tool_id` (FR-024). The desktop emits `game:debug:result-held { toolId }` / `game:debug:result-released { toolId, reason }` exactly as in 022 (contract `specs/022-desktop-debug-mode/contracts/debug-control-plane.md` §2); the frontend shows Confirm on the tool_call bubble whose `tool_id` is in `heldToolIds`.
- During the hold the bubble shows **only** the `tool_call` (name + args) — the screenshot is **not** displayed during the hold (spec C12 / FR-010). The screenshot becomes visible in the `tool_result` once the hold releases and the agent produces the tool's LLM result.
- The execution outcome (operation performed + succeeded/failed) is reachable in the **log** during the hold (spec C7 / FR-011) — already emitted by the 022 DEBUG logging in `app.go` (`recvLoop` / `executeAgentOperation`); the screenshot is intentionally NOT surfaced in the log (C7).

**Critical配套 change — the desktop stops mirroring the result to the chat stream** (spec Edge Case "Mouse-tool result source change", C8): `handleInboundOperation` currently appends the `ToolResultPart` frame to `chatStreams` (both in the debug branch at `app.go:759` and the non-debug branch at `app.go:783`). That mirror is **removed**. The result the desktop computes is sent back to the agent over the WS only (resolving `bridge.dispatch`); the conversation sees the result later, as a `tool_result` MessagePart the agent emits from the tool's LLM result (FR-010). The net display is equivalent; the source shifts from a desktop mirror to an agent emission.

**Why this is consistent**: by D2 the `tool_id` on the held FlowPart operation equals the `tool_call` MessagePart's `tool_id`, so the held-result event associates with the correct bubble. The control path (execute→hold→release→return) and the render path (tool_call bubble → later tool_result update) proceed independently and associate via `tool_id` only — their relative ordering is not relied upon (spec FR-024).

**Alternatives rejected**:
- *Keep mirroring the result to the chat stream during the hold (show the screenshot before the agent sees it).* Rejected — spec C12 explicitly excludes showing the screenshot during the hold, and C8 says the screenshot comes from the LLM tool result. The desktop mirror would diverge from the agent-emitted result.
- *Move the hold into the agent.* Rejected — spec C10 / 022 Q1 settled the hold as desktop-side, at the pre-return boundary; the agent is merely waiting.

---

## D9 — Desktop `recvLoop` and frontend rendering under the split

**Decision**: The desktop `recvLoop` (`app.go:650`) branches on the new `AgentFrame.payload` oneof:
- **`message_parts`** → append the frame to `chatStreams` (so the frontend SSE renders it) — covers text/thinking/image/`tool_call`/`tool_result`. (Same append-as-today for the display channel.)
- **`flow_parts`** → for each FlowPart: execute operation kinds (`mouse_move`/`mouse_click`/`keyboard_press`/`mouse_move_and_click`) via `handleInboundOperation` and **do not** append operation frames to `chatStreams` (FR-005: operations are never conversation entries); for signal kinds (`wait`/`warn`/`status`) append the frame to `chatStreams` so the frontend can react (clear typing on `wait`, show warning on `warn`, no-op on `status`), but they are NOT rendered as conversation bubbles.

The frontend (`App.svelte` `handleAgentFrame`, `ChatView.svelte`) renders **only** `MessagePart`s; `FlowPart` signals drive `processing`/warning state without producing a chat bubble. `ChatView`'s tool-bubble grouping (D6) and Confirm anchoring (D8) operate on `MessagePart`s only.

**On `Message.content` (history)**: `view_model.go:94` `ToMessageViewModels` serializes the new `MessageParts` via protojson (camelCase, flattened oneofs, base64 images) — unchanged mechanism, new shape. The frontend `Message.content` is typed as `MessageParts`.

**Rationale**: keeping the "append display frames to chatStreams" model minimizes chatstream changes; the split's render decision moves to the frontend (`render only MessageParts`), which is where FR-005 is naturally enforced.

**Alternatives rejected**: append FlowParts to chatStreams and have the frontend ignore them. Rejected for *operation* FlowParts — they carry pixel coordinates and would pollute the chat log needlessly; not appending them is cleaner. (Signal FlowParts ARE appended because the frontend must react to `wait`.)

---

## Summary of plan-time unknowns (resolved)

| Spec-open item | Resolved by |
|---|---|
| Proto field numbers | [contracts/content-model-contract.md](contracts/content-model-contract.md) |
| `ToolCallPart.args` representation | D3 (`string args_json`) |
| Confirm no proto-level persistence | Confirmed: persistence is `MemorySaver` over `BaseMessage`s (`handler.ts:500` `adapter.getState`); proto `Message`/`AgentFrame` are transport/reconstruction only. The proto change is a clean break (C2) at the reconstruction layer. |
| `tool_id` source & threading | D2 (LangChain `config.toolCall.id`) |
| Real status through checkpoint | D4 (`ToolMessage.additional_kwargs.toolResultStatus`) |
| `pushResult` disposition | D7 (removed — consumer-less) |

## References

### Repository-Internal
- `specs/018-saolei-mcp/spec.md`, `specs/018-saolei-mcp/contracts/mcp-tool-contract.md`, `specs/018-saolei-mcp/contracts/proto-operation-contract.md` — the feature refined here.
- `specs/021-agent-session-resync/spec.md` — origin of `pushResult` (now removed by D7).
- `specs/022-desktop-debug-mode/spec.md`, `specs/022-desktop-debug-mode/contracts/debug-control-plane.md` — the hold/Confirm behaviour re-anchored by D8.
- `projects/game/game.proto` — current `Part`/`PartBlock`/`AgentFrame.payload`/`Message.content` (being split).
- `projects/game/agent/src/handler.ts`, `projects/game/agent/src/operation-bridge.ts`, `projects/game/agent/src/llm.ts`, `projects/game/agent/src/tools/shared/result-blocks.ts`, `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`, `projects/game/agent/src/mcp/saolei/game-state.ts`, `projects/game/agent/src/mcp/saolei/validation.ts`, `projects/game/agent/src/mcp/saolei/geometry.ts`, `projects/game/agent/src/skill/saolei/SKILL.md` — agent surfaces touched.
- `projects/game/desktop/app.go`, `projects/game/desktop/view_model.go`, `projects/game/desktop/frontend/src/api.ts`, `projects/game/desktop/frontend/src/App.svelte`, `projects/game/desktop/frontend/src/components/ChatView.svelte` — desktop surfaces touched.
- `projects/game/agent/src/spike.test.ts` — documents `AIMessageChunk.tool_calls` / `contentBlocks` shape (basis for D5).

### External
- LangChain JS `tool()` `RunnableConfig.toolCall.id` — https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/utils.ts (type guard reading `config.toolCall.id`); consumer example https://github.com/n8n-io/n8n/blob/master/packages/%40n8n/ai-workflow-builder.ee/src/tools/helpers/response.ts.
- LangChain JS `streamEvents` v3 event schema (`on_tool_start` / `on_chat_model_end` / `on_tool_end`, `StreamEvent.data`) — https://api.js.langchain.com/classes/langchain_core_runnables.Runnable.html and https://js.langchain.com/docs/concepts/tools/ (config access within a tool).
- LangChain JS `AIMessage.tool_calls` / `ToolMessage.tool_call_id` shape — https://js.langchain.com/docs/how_to/tool_calling/.
- LangChain JS `MemorySaver` checkpoint over `BaseMessage`s — https://js.langchain.com/docs/concepts/persistence/ (background; the repo's `handler.ts:500` is the concrete in-repo usage).
