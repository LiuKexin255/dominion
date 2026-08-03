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

## D2 — `ToolCallPart` and the `tool_id` source (LangChain `tool_call.id`) — **REVISED by D10**

> **Revision (2026-07-25, see D10):** The original D2 required ONE shared `tool_id` across the `tool_call` MessagePart, the `FlowPart` operation, and the `tool_result` MessagePart (spec FR-008 / C6). Implementation (US3 spike) proved this is **not achievable for MCP tools**: an MCP tool handler (`McpServer.registerTool` callback) receives `RequestHandlerExtra`, NOT the LangChain `RunnableConfig`, so it cannot read `config.toolCall.id`; and `@langchain/mcp-adapters` does not forward `config.toolCall.id` to the MCP server nor build the result `ToolMessage` with `additional_kwargs`. D10 resolves this by **decoupling** the conversation channel from the operation channel — they no longer share an id. The text below is retained for the parts that still hold (the `tool_call.id` source, native-tool access); the cross-channel threading claim is superseded.

**Decision (as revised by D10)**: A new `ToolCallPart { string tool_id = 1; string name = 2; string args_json = 3; }` carries the model's tool invocation as display content. Its `tool_id` is the **LangChain `tool_call.id`** — used to group the `tool_call` MessagePart with the later `tool_result` MessagePart **within the conversation channel only** (the evolving bubble, D6). The `FlowPart` operation the tool dispatches carries a **separate, bridge-minted `tool_id`** for dispatch↔result correlation on the operation channel (D10). The two ids are NOT required to match.

**How the conversation-channel id is obtained (confirmed from source)**: a LangChain `tool()` handler receives the `RunnableConfig` as its second argument, and the tool-call id is at **`config.toolCall.id`**. This is verified by the LangChain JS runtime:
- `langchain-ai/langchainjs` `libs/langchain-core/src/tools/utils.ts` — the type guard `config is { toolCall: { id?: string } }` reading `config.toolCall.id` (https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/utils.ts).
- `n8n-io/n8n` `packages/@n8n/ai-workflow-builder.ee/src/tools/helpers/response.ts:12` — `const toolCallId = config.toolCall?.id as string;` used to build `new ToolMessage({ tool_call_id: toolCallId, ... })`.

So a **native** tool (mouse) reads `config.toolCall?.id` and uses it as the `ToolMessage.tool_call_id` (LangChain links result↔call automatically), so `ListMessages` reconstruction emits the `tool_result` MessagePart with the same id — grouping the evolving bubble (D6). The native tool does NOT pass this id to `dispatch` (D10 decouples the operation channel). An **MCP** tool (saolei) cannot read `config.toolCall.id` (its handler gets `RequestHandlerExtra`), but it does not need to: the MCP adapter-built `ToolMessage` already carries the correct `tool_call_id` (the ToolNode sets it from the originating call), so the conversation-channel grouping works for MCP tools too.

**Rationale**: The conversation-channel id, sourced from the only place that knows it at model-invocation time (the LangChain runtime), groups the evolving bubble (FR-007). It is NOT threaded onto the operation (D10).

**Alternatives rejected**:
- *Thread one id across tool_call ↔ FlowPart ↔ tool_result (original FR-008).* Rejected after the US3 spike proved MCP tools cannot obtain `config.toolCall.id` and the MCP adapter cannot carry `additional_kwargs` — see D10 for the full root-cause and the decoupling decision.
- *Derive the id from the `on_tool_start` streamEvent's `run_id`.* Rejected: `run_id` is the *tool execution's* run id, not the `tool_call.id`; they are different identifiers, and mapping them is brittle.

**Fallback when `config.toolCall.id` is absent**: in non-agent (direct-invoke) test paths the id may be missing. The native tool falls back to an empty `tool_call_id` on the `ToolMessage` (LangChain's convention). The operation channel is unaffected (the bridge always mints its own id — D10).

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

**Rationale**: one bubble per call is the UX the spec mandates (C5). (The debug Confirm surface that originally anchored onto this bubble is moved to a session-top drawer — D11.)

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

## D8 — Debug-mode hold re-anchored — **SUPERSEDED by D11 (drawer)**

> **Revision (2026-07-25, see D11):** The original D8 placed the Confirm control on the `tool_call` conversation bubble, associated via the shared `tool_id` (then required by FR-008). The US3 spike (D10) removed the shared `tool_id`, so the bubble can no longer associate with the held operation. D11 re-anchors the Confirm onto a **session-top drawer** on the operation channel — fully decoupled from the conversation. The hold POINT (desktop holds after execute, before returning to the agent) and the配套 chat-stream mirror removal described here are RETAINED; only the Confirm surface changes.

**Decision (as superseded by D11)**: The 022 debug hold stays at the **same point** — the desktop holds after executing the operation and before returning the result to the agent (`projects/game/desktop/app.go` `handleInboundOperation`, between `executeAgentOperation` and `ws.SendFrame`) (spec C10 / FR-023). The Confirm control is NO LONGER on the conversation bubble; per D11 it is a session-top drawer keyed by the **operation-channel** `tool_id` (bridge-minted). During the hold the execution outcome is reachable in the **log** (spec C7 / FR-011).

**Critical配套 change — the desktop stops mirroring the result to the chat stream** (spec Edge Case "Mouse-tool result source change", C8): `handleInboundOperation` currently appends the `ToolResultPart` frame to `chatStreams` (both in the debug branch at `app.go:759` and the non-debug branch at `app.go:783`). That mirror is **removed**. The result the desktop computes is sent back to the agent over the WS only (resolving `bridge.dispatch`); the conversation sees the result later, as a `tool_result` MessagePart the agent emits from the tool's LLM result (FR-010). The net display is equivalent; the source shifts from a desktop mirror to an agent emission.

**Why this is consistent**: ~~by D2 the `tool_id` on the held FlowPart operation equals the `tool_call` MessagePart's `tool_id`~~ (SUPERSEDED — D10 decouples the two ids). The drawer associates with the held operation via the **operation-channel** `tool_id` (D11); the conversation render path (tool_call bubble → later tool_result update) proceeds independently and is NOT relied upon for the Confirm association.

**Alternatives rejected**:
- *Keep mirroring the result to the chat stream during the hold (show the screenshot before the agent sees it).* Rejected — spec C12 explicitly excludes showing the screenshot during the hold, and C8 says the screenshot comes from the LLM tool result. The desktop mirror would diverge from the agent-emitted result.
- *Move the hold into the agent.* Rejected — spec C10 / 022 Q1 settled the hold as desktop-side, at the pre-return boundary; the agent is merely waiting.

---

## D10 — Decouple the conversation channel from the operation channel (core revision)

**Decision**: The conversation display and the operation execution are **two fully independent channels** that no longer share an id:

- **Conversation channel (display)** — derived solely from the LLM message stream. `tool_call` and `tool_result` MessageParts are grouped by the LangChain `tool_call.id` into one evolving bubble (D6). This works identically for native tools (mouse) and MCP tools (saolei), because both produce an `AIMessage.tool_calls` entry and a `ToolMessage` whose `tool_call_id` LangChain sets from the originating call — no manual threading needed.
- **Operation channel (control)** — `OperationBridge.dispatch` mints its own UUID (`operation-bridge.ts:215` `toolId ?? randomUUID()`, now **always** minted), stamps it on the `FlowPart` operation, and correlates the desktop's `ToolResultPart` reply via that UUID (`handleResult`). This correlation is internal to the bridge↔desktop operation channel.

The two channels are NOT linked by a shared id. Concretely:
- `OperationBridge.dispatch` signature drops the `toolId` parameter: `dispatch(part: FlowPart, signal?: AbortSignal): Promise<OperationResult>`. The bridge always mints the operation id internally.
- Native (mouse) tools read `config.toolCall.id` ONLY to set `ToolMessage.tool_call_id` (conversation grouping); they do NOT pass it to `dispatch`.
- MCP (saolei) tools do NOT read any id; the adapter-built `ToolMessage` carries the correct `tool_call_id` automatically.

**Root cause that forced this revision (read from source during US3 implementation)**:
- MCP `McpServer.registerTool` callback signature is `(args, extra: RequestHandlerExtra)` (`@modelcontextprotocol/sdk` ^1.29.0 `dist/esm/server/mcp.d.ts`); `RequestHandlerExtra` has `signal/authInfo/sessionId/_meta/...` but **no** `toolCall`, **no** LangChain `RunnableConfig`.
- `@langchain/mcp-adapters` ^1.1.3 `_callTool` (`dist/tools.js:351-420`) builds `requestOptions` from `config` using only `timeout/signal/onprogress`; it **never reads nor forwards** `config.toolCall.id` to the MCP `callTool` request.
- Therefore an MCP tool handler cannot learn the LangChain `tool_call.id`, so the original FR-008 (one id across tool_call↔FlowPart↔tool_result) is **unimplementable for MCP tools** without inventing a fragile side-channel (custom transport `_meta` injection + zod passthrough + adapter hooks). Decoupling removes the requirement entirely.

**Rationale**: The original coupling existed only to (a) group the evolving bubble and (b) anchor the debug Confirm on the bubble. (a) is satisfied by the conversation-channel `tool_call.id` alone (LangChain wires it for both native and MCP tools). (b) is moved to an operation-channel drawer (D11), which does not need the conversation id. Decoupling also lets saolei tools work as-is (MCP, no id access) — unblocking US3 — and removes the now-dead `toolId` parameter from the bridge (Principle II: don't leave coupling surface that nothing requires).

**Spec impact**: FR-008 is relaxed — the `tool_call` MessagePart and the `tool_result` MessagePart MUST still share the LangChain `tool_call.id` (bubble grouping); the `FlowPart` operation uses an independent bridge-minted id. FR-024 (control path ↔ render path associate via `tool_id`) is **superseded by D11**: the Confirm associates with the held operation via the operation-channel id, not with the conversation bubble. See the spec Clarification additions (C13/C14) recorded in `spec.md`.

**Alternatives rejected**:
- *Convert saolei tools from MCP to native LangChain `tool()` (developer's option A).* Rejected: larger change (removes `McpServer`/`mcp-host.ts`/loopback), and the user's direction explicitly keeps the MCP ("mcp 就不需要获取到 tool_id"). Decoupling achieves the goal without converting.
- *Inject `tool_call.id` into MCP via a custom transport + adapter `afterToolCall` hook (developer's option B).* Rejected: long fragile chain depending on adapter/SDK internals; contradicts the user's "移除掉那些不需要的功能和设计".
- *Relax FR-008 but keep the `dispatch` `toolId` param dormant.* Rejected: leaving unused coupling surface violates Principle II; the param is removed.

---

## D11 — Debug Confirm re-anchored onto a session-top drawer (operation channel)

**Decision**: With the conversation decoupled (D10), the debug Confirm control moves OFF the conversation bubble onto a **drawer fixed at the top of the session chat view**. The drawer is driven entirely by the **operation channel**:

- When the desktop holds an operation result (debug mode, after execute / before return — D8 hold point), it emits `game:debug:result-held` carrying the **operation request content** + the operation-channel `toolId` (the bridge-minted id on the `FlowPart`/`ToolResultPart`).
- The frontend renders a drawer above the conversation showing a human-readable description of the held operation (e.g. "移动并点击 (136, 344) · 左键 · 窗口消息" / "按键 F2") plus a **Confirm** button.
- Clicking Confirm calls `ConfirmToolResult(toolId)` (022 method, unchanged), which releases that held result to the agent.
- `game:debug:result-released { toolId, reason }` (022 event, unchanged names) dismisses the drawer entry.
- The conversation (`ChatView`) is NOT involved: it no longer receives `heldToolIds`/`onConfirm`. Multiple simultaneous holds render multiple drawer entries (one per held operation `toolId`).

**Why a drawer (not the bubble)**: the held artifact is an *operation* (a physical mouse/keyboard command the desktop executed), not a *conversation* event. Showing it on a tool bubble conflated the semantic tool-call (what the model asked) with the physical operation (what the desktop ran), and — per D10 — the two no longer share an id to associate them. A session-top drawer presents the operation request on its own terms, in arrival order, decoupled from conversation rendering. This matches the user's direction ("抽屉式的提示…操作请求的内容以及确认按钮…完全解耦tools的对话展示与操作命令的执行").

**Operation content payload**: the `game:debug:result-held` event payload is extended (vs 022's `{ toolId }`) to include a rendered operation descriptor so the drawer needs no proto knowledge:
```jsonc
{
  "toolId": "<bridge-minted operation id>",
  "operation": {
    "kind": "mouse_move" | "mouse_click" | "keyboard_press" | "mouse_move_and_click",
    "summary": "<human-readable, e.g. '移动并点击 (136, 344) · 左键 · 窗口消息'>",
    "details": { /* xPx, yPx, click, method, key — the raw FlowPart fields */ }
  }
}
```
The desktop Go backend builds `summary`/`details` from the `FlowPart` it received (it already decodes the oneof). `ConfirmToolResult(toolID)` and the `result-released` event are unchanged from 022 (same `toolId` = operation-channel id).

**What is removed**: the frontend `heldToolIds: Set<string>` state and the `ChatView` Confirm-on-bubble rendering (022 contract §3) are removed. `ChatView` no longer takes `heldToolIds`/`onConfirm` props. The 022 `game:debug:result-held`/`result-released` event NAMES and `SetDebugMode`/`ConfirmToolResult` method names are UNCHANGED (only the `result-held` payload gains the `operation` field — additive).

**Rationale**: decoupling the hold UI from the conversation UI means (a) saolei operations (whose FlowPart `tool_id` ≠ any tool_call id) get a Confirm surface, (b) the conversation renderer becomes simpler (no held-state branching), (c) the drawer is the natural place for "an operation is awaiting your approval".

**Alternatives rejected**:
- *Keep Confirm on the conversation bubble, matched by tool_call.id.* Rejected — D10 removed the shared id; MCP operations have no tool_call.id to match. Also the user explicitly asked for the drawer.
- *Show the screenshot in the drawer during the hold.* Rejected — spec C12 excludes showing the screenshot before the result returns to the agent; the drawer shows the operation request (text), not the captured screenshot.

---

## D12 — Saolei (MCP) tool-result status is neutral; real status only for native (mouse) tools

**Decision**: Under decoupling (D10), the real `ToolResultStatus` is carried into the checkpointed `ToolMessage.additional_kwargs.toolResultStatus` ONLY for **native** tools (mouse), via `buildToolResultMessage` (D4, US2). **MCP** tools (saolei) return MCP content blocks; the `@langchain/mcp-adapters` client builds the `ToolMessage` WITHOUT `additional_kwargs`, so the saolei tool-result status is `TOOL_RESULT_STATUS_UNSPECIFIED` (neutral) — both live and in history.

This is **FR-014-compliant** ("a historical tool result whose real status is unavailable MUST be shown with an unspecified/neutral status; MUST NOT be defaulted to FAILED") and it **fixes the original bug** (spec item 3: saolei results showed spurious `FAILED` because `inferToolResultStatus` guessed FAILED from text). With the text-heuristic removed (US1/US2), saolei results read neutral — never FAILED. The actual outcome remains visible to the model and the user via the result message text (the saolei tool includes a status line in its content blocks) and the returned screenshot; only the structured status *badge* is neutral.

**Rationale**: carrying the real status for MCP tools would require either converting saolei to native tools (rejected, D10) or an adapter `afterToolCall` hook + status-encoding protocol (rejected as added complexity contradicting "移除掉那些不需要的功能和设计"). Neutral status is the simplest design that satisfies the actual user complaint (no spurious FAILED) and FR-014. Real status remains a property of native tools, where it is cheap and already implemented (US2).

**Spec impact**: US2 acceptance is refined — the "real status survives the checkpoint" guarantee (FR-012/FR-013) applies to native (mouse) tools; for MCP (saolei) tools the guarantee is "neutral, never FAILED" (FR-014). Recorded as spec Clarification C15. The US2 independent test is run with mouse tools (which carry real status); a separate saolei-history assertion verifies neutral (not FAILED).

**Alternatives rejected**:
- *Make saolei carry real status via an adapter hook.* Rejected — complexity; see D10 alternatives.
- *Infer saolei status from text.* Rejected — FR-015 forbids text inference.

---

## D9 — Desktop `recvLoop` and frontend rendering under the split

**Decision**: The desktop `recvLoop` (`app.go:650`) branches on the new `AgentFrame.payload` oneof:
- **`message_parts`** → append the frame to `chatStreams` (so the frontend SSE renders it) — covers text/thinking/image/`tool_call`/`tool_result`. (Same append-as-today for the display channel.)
- **`flow_parts`** → for each FlowPart: execute operation kinds (`mouse_move`/`mouse_click`/`keyboard_press`/`mouse_move_and_click`) via `handleInboundOperation` and **do not** append operation frames to `chatStreams` (FR-005: operations are never conversation entries); for signal kinds (`wait`/`warn`/`status`) append the frame to `chatStreams` so the frontend can react (clear typing on `wait`, show warning on `warn`, no-op on `status`), but they are NOT rendered as conversation bubbles.

The frontend (`App.svelte` `handleAgentFrame`, `ChatView.svelte`) renders **only** `MessagePart`s; `FlowPart` signals drive `processing`/warning state without producing a chat bubble. `ChatView`'s tool-bubble grouping (D6) operates on `MessagePart`s only. The debug Confirm is a session-top drawer driven by the operation channel (D11), NOT a bubble surface.

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
| `tool_id` source & threading | D2 (conversation channel: LangChain `tool_call.id`); **D10** (operation channel decoupled, bridge-minted id). Original cross-channel threading (FR-008) relaxed — see D10. |
| Real status through checkpoint | D4 (`ToolMessage.additional_kwargs.toolResultStatus`) for native tools; **D12** (saolei/MCP tools → neutral, FR-014). |
| `pushResult` disposition | D7 (removed — consumer-less) |
| MCP tools cannot access `tool_call.id` / carry `additional_kwargs` (US3 blockage) | **D10** (decouple channels) + **D12** (saolei neutral status) |
| Debug Confirm surface after decoupling | **D11** (session-top drawer on the operation channel) |

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
- MCP TS SDK `McpServer.registerTool` / `RequestHandlerExtra` (D10 root-cause) — https://github.com/modelcontextprotocol/typescript-sdk (`@modelcontextprotocol/sdk` ^1.29.0).
- `@langchain/mcp-adapters` MCP→ToolMessage wrapping (D10 root-cause: `_callTool` does not forward `config.toolCall.id`; builds `ToolMessage` without `additional_kwargs`) — https://github.com/langchain-ai/langchainjs/tree/main/libs/mcp-adapters (`@langchain/mcp-adapters` ^1.1.3).
