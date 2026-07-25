# Contract: Tool Dispatch (decoupled operation channel, native-tool status carriage, stateless saolei)

**Feature**: 023-saolei-mcp-refine
**Date**: 2026-07-25
**Status**: Phase 1 contract — MUST be satisfied by implementation. Settled BEFORE any tool/bridge code change (Constitution Principle III). **Revised 2026-07-25** (research.md D10/D12) to decouple the operation channel from the conversation channel.

This contract pins three things that span the agent's tool layer and the `OperationBridge`:
1. The **operation channel correlation**: `OperationBridge.dispatch` mints its own id (decoupled from the conversation `tool_call.id` — research.md D10).
2. The carriage of the real `ToolResultStatus` through the `ToolMessage` and the `MemorySaver` checkpoint for **native** tools (spec FR-012..FR-015); MCP (saolei) tools read neutral (D12).
3. The stateless saolei tool set — four pure dispatch-and-return tools, no per-session state, no `saolei_update`, no validation (spec FR-016..FR-022).

It is the tool↔bridge↔checkpoint interface. The proto operation Parts themselves are unchanged (they live in `FlowPart` now per [content-model-contract.md](content-model-contract.md), but their fields are identical to spec 018).

## Authority

- Spec: `spec.md` FR-006..FR-022, C6, C8, C9, C11.
- Research: `research.md` D2 (`config.toolCall.id`), D4 (`additional_kwargs.toolResultStatus`), D5 (live emission), D7 (stateless).
- Data model: `data-model.md` §6 (status provenance), §7 (saolei tool set).
- LangChain JS runtime: `config.toolCall.id` — https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/utils.ts ; `AIMessage.tool_calls` / `ToolMessage.tool_call_id` — https://js.langchain.com/docs/how_to/tool_calling/.
- Current code: `projects/game/agent/src/operation-bridge.ts` (`dispatch` 176, `handleResult` 306, `pushResult` 281), `projects/game/agent/src/tools/shared/result-blocks.ts` (`buildResultBlocks` 33), `projects/game/agent/src/tools/mouse_click/mouse-click.ts`, `projects/game/agent/src/tools/mouse_move/mouse-move.ts`, `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`.

## §1. `OperationBridge.dispatch` — operation-channel correlation (decoupled, D10)

> **Revision (2026-07-25, research.md D10):** the original §1 threaded the LangChain `tool_call.id` onto the FlowPart operation (FR-008 coupling). US3 implementation proved MCP tools cannot obtain that id and the MCP adapter cannot carry `additional_kwargs`. The conversation and operation channels are now **decoupled**: `dispatch` mints its own operation id and does NOT take a `toolId` parameter. The conversation-channel grouping uses the LangChain `tool_call.id` independently (D2 revised).

### Signature

```ts
// After (decoupled — research.md D10):
async dispatch(
  part: FlowPart,                 // FlowPart (not Part) per content-model split
  signal?: AbortSignal,
): Promise<OperationResult>
```

### Behaviour

- The bridge **always mints** a UUID for the operation (`randomUUID()`, `operation-bridge.ts:215`) and stamps it onto the operation's `tool_id`. This id is for **operation-channel correlation only** (dispatch↔`handleResult` via the pending map); it is NOT related to the conversation-channel `tool_call.id`.
- `handleResult` correlation is unchanged: the desktop's `ToolResultPart.tool_id` matches the pending dispatch's bridge-minted `tool_id`.
- The 20-min `DISPATCH_TIMEOUT_MS` backstop (022 FR-014) is unchanged.
- The envelope the bridge writes via the sink is a `FlowParts` frame (per the content-model split):
  ```ts
  const envelope: AgentFrame = { payload: "flowParts", flowParts: { parts: [part] } };
  ```

### Why

Decoupling lets MCP tools (saolei) dispatch without needing the LangChain `tool_call.id`, and removes the dead `toolId` coupling surface (Principle II). The conversation grouping (evolving bubble) uses the LangChain `tool_call.id` on the MessageParts, which LangChain wires automatically for both native and MCP tools (D2 revised). The debug Confirm is re-anchored onto an operation-channel drawer (D11), so it also does not need the conversation id.

## §2. Tool function — conversation-channel id (native tools only)

> **Revision (D10):** tools no longer pass an id to `dispatch`. A native tool reads `config.toolCall.id` ONLY to set the `ToolMessage.tool_call_id` (conversation grouping); an MCP tool does not read any id (the adapter sets `tool_call_id` automatically).

A **native** tool (mouse) reads the LangChain `tool_call.id` and uses it on the returned `ToolMessage` (NOT on dispatch):

```ts
// Mouse tool example (mouse-click.ts / mouse-move.ts):
return tool(
  async (args, config): Promise<ToolMessage> => {
    const signal = (config as { signal?: AbortSignal } | undefined)?.signal;
    const toolCallId = (config as { toolCall?: { id?: string } } | undefined)?.toolCall?.id;
    const part: FlowPart = { mouseClick: { click: CLICK_TYPE_TO_PROTO[click_type] } };
    const result = await bridge.dispatch(part, signal);   // no toolId (decoupled, D10)
    return buildToolResultMessage(result, toolCallId, "mouse_click");  // toolCallId → ToolMessage.tool_call_id
  },
  { name: "mouse_click", description: "...", schema: mouseClickSchema, extras: { standalone: true } },
);
```

An **MCP** tool (saolei) does NOT read `config` for an id; it dispatches and returns MCP content blocks. The `@langchain/mcp-adapters` client builds the `ToolMessage` with the correct `tool_call_id` from the originating call, so the conversation grouping works without manual threading.

- `config.toolCall.id` is the LangChain tool-call id (research.md D2 — verified from `langchain-ai/langchainjs` `libs/langchain-core/src/tools/utils.ts`).

## §3. `ToolMessage` — carrying the real status through the checkpoint (native tools)

> **Scope (D12):** real-status carriage applies to **native** tools (mouse). **MCP** tools (saolei) return MCP content blocks; the adapter-built `ToolMessage` has no `additional_kwargs`, so their status is neutral (`TOOL_RESULT_STATUS_UNSPECIFIED`) — FR-014-compliant, fixes the spurious-FAILED bug. See research.md D12.

### Native helper — `buildToolResultMessage`

Replaces `buildResultBlocks` (`result-blocks.ts`) as the tool RETURN value for native tools. Returns a `ToolMessage` (not raw content blocks) carrying the real status in `additional_kwargs`:

```ts
import { ToolMessage } from "@langchain/core/messages";

export function buildToolResultMessage(
  result: OperationResult,
  toolCallId: string | undefined,
  name: string,
): ToolMessage {
  return new ToolMessage({
    content: buildResultBlocks(result),   // unchanged blocks: status text + screenshot image_url + pixel-size annotation
    tool_call_id: toolCallId ?? "",       // LangChain links result↔call (conversation grouping); empty when id unknown
    name,                                 // tool name, for display
    additional_kwargs: {
      toolResultStatus: result.status,    // the REAL ToolResultStatus enum-name string, e.g. "TOOL_RESULT_STATUS_SUCCEEDED"
    },
  });
}
```

- `content` is the existing `MouseContentBlock[]` from `buildResultBlocks` (unchanged) — the model sees the same text + screenshot it sees today.
- `additional_kwargs.toolResultStatus` carries the real status (resolved by `bridge.dispatch` from the desktop's `ToolResultPart.status`). It is serialized through `MemorySaver` alongside `content` (verified by `spike.checkpoint.test.ts`, research.md D4).
- `tool_call_id` links the result to the originating `AIMessage.tool_calls[i].id` (LangChain's ToolNode uses this for the model's tool-result loop AND for the conversation bubble grouping).

### Saolei (MCP) tools — neutral status (D12)

Saolei MCP tool handlers return MCP content blocks (`{ content: [...] }`); the `@langchain/mcp-adapters` client wraps them into a `ToolMessage` **without** `additional_kwargs.toolResultStatus`. Therefore a saolei tool result's status is `TOOL_RESULT_STATUS_UNSPECIFIED` (neutral) on both the live path and in history. This is accepted (research.md D12): it fixes the original spurious-FAILED bug (the text-heuristic `inferToolResultStatus` is gone) and satisfies FR-014. The actual outcome remains visible via the result message text + the returned screenshot. **No `additional_kwargs` injection mechanism is built for MCP tools** (rejected as added complexity — D10/D12).

## §4. `ListMessages` reconstruction (status from `additional_kwargs`)

`projects/game/agent/src/handler.ts` `ListMessages` is rewritten for the new model. For each `BaseMessage` in the checkpoint:

| Message type | Reconstructed `MessagePart`s |
|---|---|
| `HumanMessage` | `text` (from string/array content) + `image` (from image blocks) |
| `AIMessage` | `thinking` (reasoning blocks) + `text` (text blocks) + `image` (image blocks) + **`tool_call`** for each `tool_calls[i]`: `{ tool_id: call.id, name: call.name, args_json: JSON.stringify(call.args ?? {}) }` |
| `ToolMessage` | **`tool_result`**: `{ tool_id: msg.tool_call_id, status: msg.additional_kwargs?.toolResultStatus ?? "TOOL_RESULT_STATUS_UNSPECIFIED", message: <from content text block>, screenshot: <from content image_url block> }` |

Status resolution (FR-013..FR-015):
- Read `additional_kwargs.toolResultStatus` directly.
- If absent → `TOOL_RESULT_STATUS_UNSPECIFIED` (neutral). **Never** infer from text. **Never** default to `FAILED`.
- `inferToolResultStatus` (`handler.ts:776`), `toolCallToPart` (`handler.ts:756`), and `reconstructToolResult` (`handler.ts:795`) are **removed**.

The reconstruction emits one `Message` per `BaseMessage` (as today), with `content: MessageParts { parts: [...] }`.

## §5. `OperationBridge.pushResult` — removed

`pushResult` (`operation-bridge.ts:281`) was added in 021 for `saolei_update`'s display-only forwarding. With `saolei_update` removed (§6) it has **no consumer**. It is **deleted** along with its `FRAME_SENDER_SYSTEM` local constant (used only by `pushResult`). The bridge's public surface becomes `registerSink` / `unregisterSink` / `dispatch` / `handleResult`. `dispatch`/`handleResult` correlation is unchanged.

## §6. Stateless saolei MCP tool set

> **Unblocked (D10/D12):** saolei tools no longer need the LangChain `tool_call.id` (decoupled) nor `additional_kwargs` status (neutral). They dispatch via `bridge.dispatch(part, signal)` (no toolId) and return MCP content blocks; status is neutral (D12).

`projects/game/agent/src/mcp/saolei/saolei-mcp.ts` is rewritten. `createSaoleiMcpServer(bridge: OperationBridge)` (no `initialState` parameter) registers exactly four tools. Each tool dispatches a FlowPart via `bridge.dispatch(part, signal)` (bridge mints the operation id — §1) and returns MCP content blocks (`{ content: [...] }` — a status text block + an image block); the `@langchain/mcp-adapters` client wraps them into a `ToolMessage` whose status is neutral (D12). The saolei tool status text in the content block records the dispatch outcome for the model/user to read; the structured `toolResultStatus` is neutral.

### `saolei_init` — no arguments

- **inputSchema**: `{}` (no `width`/`height` — dropped per C11; they affected only agent-side state).
- **Behaviour**: `bridge.dispatch({ keyboardPress: { key: "KEYBOARD_KEY_F2" } }, signal)` and return the result + screenshot.
- **Returns**: MCP content blocks — a text block `"saolei_init: F2 dispatched (new game)"` + the screenshot image block. (The adapter wraps these into a `ToolMessage` with neutral status — D12.)
- Re-calling re-dispatches F2 (FR-019).

### `saolei_click(x, y)` / `saolei_flag(x, y)` / `saolei_chord_click(x, y)`

- **inputSchema** (each): `{ x: int ≥ 0, y: int ≥ 0 }` (top-left origin `(0,0)`; `x`=column, `y`=row — unchanged convention).
- **Behaviour**: compute `(xPx, yPx) = center(x, y)` via `geometry.ts` (unchanged formula); `bridge.dispatch({ mouseMoveAndClick: { xPx, yPx, click: <ACTION>, method: "MOUSE_INPUT_METHOD_WINDOW_MESSAGE" } }, signal)`; return the result + screenshot.
  - `saolei_click` → `click: "MOUSE_CLICK_ACTION_LEFT_CLICK"`.
  - `saolei_flag` → `click: "MOUSE_CLICK_ACTION_RIGHT_CLICK"`.
  - `saolei_chord_click` → `click: "MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS"`.
- **Returns**: MCP content blocks — a text block `"<tool> dispatched at (x,y)"` + the screenshot image block. (Adapter-wrapped `ToolMessage` carries neutral status — D12.)
- **No pre-dispatch validation** (FR-018). **No alternation** (FR-021): a second operation is accepted immediately; there is no `pendingUpdate`/`lastOp`.

### Removed (vs spec 018)

- `saolei_update` tool registration — deleted.
- `GameState` / `CellStatus` / `LastOp` / `createGameState` (`game-state.ts`) — file deleted.
- All validators (`validation.ts`: `validateUpdate`, `validateClick/Flag/ChordPreDispatch`, `validateClick/Flag/ChordUpdate`, `validateRange`, connectivity helpers) — file deleted.
- `validation.test.ts` — file deleted.
- The `pendingUpdate` / `lastOp` / alternation logic — deleted.
- `createSaoleiMcpServer`'s `initialState` parameter and the returned `state` field — the handle becomes `{ server }` (or just returns the `McpServer`).

### `mcp-host.ts`

`createSaoleiMcpServer(looked.bridge)` (`mcp-host.ts:86`) drops the second argument. `SaoleiMcpHandle` drops the `state` field.

### `SKILL.md` (built-in saolei skill)

`projects/game/agent/src/skill/saolei/SKILL.md` is rewritten (FR-022):
- Describes exactly the four tools and the top-left-origin `(x,y)` convention.
- Instructs the model to read the returned screenshot to track the board (the model infers board bounds from the image, as it already must for unseen cells).
- **No** `saolei_update`, **no** alternation, **no** cell-status reporting contract, **no** validation-rejection guidance.

## §7. Live emission of `tool_call` / `tool_result` (agent turn loop)

The adapter's `generateTurn` (`projects/game/agent/src/llm.ts:399`) yields two new `ContentBlock` variants so the `Connect` handler can emit them as `MessageParts` frames live (spec FR-006 / FR-009):

```ts
export type ContentBlock =
  | { type: "reasoning"; reasoning: string }
  | { type: "text"; text: string }
  | { type: "tool_call"; name: string; args: unknown; toolCallId: string }
  | { type: "tool_result"; toolCallId: string; status: string; message: string; screenshot?: { data: string; widthPx: number; heightPx: number } };
```

- `tool_call`: yielded when a streamed `AIMessage` carries `tool_calls` (one block per call), BEFORE the tool node executes. Source: `AIMessage.tool_calls[i]` (`{ name, args, id }`).
- `tool_result`: yielded when a `ToolMessage` is produced. Source: `ToolMessage.content` blocks (message + screenshot) + `ToolMessage.additional_kwargs.toolResultStatus` + `ToolMessage.tool_call_id`. For native tools `additional_kwargs.toolResultStatus` is the real status; for MCP (saolei) tools it is absent → neutral (`TOOL_RESULT_STATUS_UNSPECIFIED`) (D12).

The `Connect` handler (`handler.ts:379`) emits a `message_parts` frame for each: `{ payload: "messageParts", messageParts: { parts: [{ toolCall: {...} }] } }` or `[{ toolResult: {...} }]`. This makes the live conversation show the same tool calls/results that `ListMessages` reconstructs (single source of truth, FR-009).

> **Implemented mechanism (US2/Phase 3):** `stream.messages` ignores tool-role messages and `stream.toolCalls.output` drops `additional_kwargs`, so `tool_result` is read from the raw v3 `GraphRunStream` ProtocolEvents (`{method:"tools", params:{data:{event:"tool-finished", output:<ToolMessage>}}}`, empirically verified for pinned `@langchain/langgraph` ^1.4.8). See `projects/game/agent/src/llm.ts` `consumeToolResults` / `extractToolOutput`. The conversation-channel `tool_call.id` (from `stream.toolCalls`) groups the bubble; the operation-channel id is separate (D10).

## §8. What does NOT change (scope boundary)

- The four saolei tool *names* and their `(x, y)` grid convention.
- The proto operation Parts and their fields (`MouseMoveAndClickPart.xPx/yPx/click/method`, `KeyboardPressPart.key`, `tool_id`) — they move into `FlowPart` but their shapes are identical to spec 018.
- `OperationBridge.dispatch`/`handleResult` correlation semantics and the 20-min timeout.
- `buildResultBlocks` content (status text + screenshot image_url + pixel-size annotation) — reused verbatim as the `ToolMessage.content`.
- The `@langchain/mcp-adapters` loopback client and the `mcp-host.ts` HTTP routing — unchanged (only the tool registrations inside the server change).
- The 022 debug control-plane method/event names (`SetDebugMode`, `ConfirmToolResult`, `game:debug:result-held`/`result-released`).
- The mouse tools' `extras: { standalone: true }`.

## Out of scope for this contract

- The proto `MessagePart`/`FlowPart`/`ToolCallPart` shapes and field numbers → [content-model-contract.md](content-model-contract.md).
- The evolving-bubble frontend view model → `data-model.md` §5.
- The debug-hold **drawer** (session-top Confirm on the operation channel) → [contracts/debug-drawer-contract.md](debug-drawer-contract.md) (research.md D11).
- The desktop `recvLoop`/`handleInboundOperation` changes (FlowParts execution, result-mirror removal) → `data-model.md` §8/§9 + `quickstart.md`.
