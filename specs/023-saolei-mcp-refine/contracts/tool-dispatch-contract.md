# Contract: Tool Dispatch (tool_id threading, status carriage, stateless saolei)

**Feature**: 023-saolei-mcp-refine
**Date**: 2026-07-25
**Status**: Phase 1 contract — MUST be satisfied by implementation. Settled BEFORE any tool/bridge code change (Constitution Principle III).

This contract pins three things that span the agent's tool layer and the `OperationBridge`:
1. The `tool_id` threading from the LangChain `tool_call.id` through dispatch into the `FlowPart` operation (spec FR-008).
2. The carriage of the real `ToolResultStatus` through the `ToolMessage` and the `MemorySaver` checkpoint (spec FR-012..FR-015).
3. The stateless saolei tool set — four pure dispatch-and-return tools, no per-session state, no `saolei_update`, no validation (spec FR-016..FR-022).

It is the tool↔bridge↔checkpoint interface. The proto operation Parts themselves are unchanged (they live in `FlowPart` now per [content-model-contract.md](content-model-contract.md), but their fields are identical to spec 018).

## Authority

- Spec: `spec.md` FR-006..FR-022, C6, C8, C9, C11.
- Research: `research.md` D2 (`config.toolCall.id`), D4 (`additional_kwargs.toolResultStatus`), D5 (live emission), D7 (stateless).
- Data model: `data-model.md` §6 (status provenance), §7 (saolei tool set).
- LangChain JS runtime: `config.toolCall.id` — https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/utils.ts ; `AIMessage.tool_calls` / `ToolMessage.tool_call_id` — https://js.langchain.com/docs/how_to/tool_calling/.
- Current code: `projects/game/agent/src/operation-bridge.ts` (`dispatch` 176, `handleResult` 306, `pushResult` 281), `projects/game/agent/src/tools/shared/result-blocks.ts` (`buildResultBlocks` 33), `projects/game/agent/src/tools/mouse_click/mouse-click.ts`, `projects/game/agent/src/tools/mouse_move/mouse-move.ts`, `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`.

## §1. `OperationBridge.dispatch` — `tool_id` threading

### Signature change

```ts
// Before (operation-bridge.ts:176):
async dispatch(part: Part, signal?: AbortSignal): Promise<OperationResult>

// After:
async dispatch(
  part: FlowPart,                 // FlowPart (not Part) per content-model split
  toolId?: string,                // NEW: the LangChain tool_call.id (research.md D2)
  signal?: AbortSignal,
): Promise<OperationResult>
```

### Behaviour

- When `toolId` is provided: the bridge stamps `toolId` onto the operation's `tool_id` field (the same way it today stamps `randomUUID()` at `operation-bridge.ts:208-209`), instead of minting a new UUID. This makes the dispatched `FlowPart.tool_id` equal the `tool_call.id`.
- When `toolId` is omitted (e.g. a non-agent direct-invoke test path): the bridge mints a UUID as today (fallback preserves existing behaviour for callers that do not pass one).
- `handleResult` correlation is unchanged: the desktop's `ToolResultPart.tool_id` matches the pending dispatch's `tool_id` (now the `tool_call.id` when supplied).
- The 20-min `DISPATCH_TIMEOUT_MS` backstop (022 FR-014) is unchanged.
- The envelope the bridge writes via the sink becomes a `FlowParts` frame (per the content-model split):
  ```ts
  const envelope: AgentFrame = { payload: "flowParts", flowParts: { parts: [part] } };
  ```

### Why

One id — the LangChain `tool_call.id` — threads `tool_call` MessagePart ↔ `FlowPart` operation ↔ `tool_result` MessagePart. The evolving-bubble grouping (FR-007) and the debug Confirm anchoring (FR-023) both key on it.

## §2. Tool function — reading the `tool_call.id`

Each tool (mouse + saolei) reads the id from the LangChain `RunnableConfig` and passes it to `dispatch`:

```ts
// Mouse tool example (mouse-click.ts / mouse-move.ts):
return tool(
  async (args, config): Promise<ToolMessage> => {
    const signal = (config as { signal?: AbortSignal } | undefined)?.signal;
    const toolCallId = (config as { toolCall?: { id?: string } } | undefined)?.toolCall?.id;
    const part: FlowPart = { mouseClick: { click: CLICK_TYPE_TO_PROTO[click_type] } };
    const result = await bridge.dispatch(part, toolCallId, signal);
    return buildToolResultMessage(result, toolCallId, "mouse_click");
  },
  { name: "mouse_click", description: "...", schema: mouseClickSchema, extras: { standalone: true } },
);
```

- `config.toolCall.id` is the LangChain tool-call id (research.md D2 — verified from `langchain-ai/langchainjs` `libs/langchain-core/src/tools/utils.ts` and `n8n-io/n8n`).
- The tool passes `toolCallId` (possibly `undefined`) to `dispatch`; `dispatch` falls back to a minted UUID when undefined.

## §3. `ToolMessage` — carrying the real status through the checkpoint

### New helper — `buildToolResultMessage`

Replaces `buildResultBlocks` (`result-blocks.ts`). Returns a `ToolMessage` (not raw content blocks) carrying the real status in `additional_kwargs`:

```ts
import { ToolMessage } from "@langchain/core/messages";

export function buildToolResultMessage(
  result: OperationResult,
  toolCallId: string | undefined,
  name: string,
): ToolMessage {
  return new ToolMessage({
    content: buildResultBlocks(result),   // unchanged blocks: status text + screenshot image_url + pixel-size annotation
    tool_call_id: toolCallId ?? "",       // LangChain links result↔call (empty when id unknown)
    name,                                 // tool name, for display
    additional_kwargs: {
      toolResultStatus: result.status,    // the REAL ToolResultStatus enum-name string, e.g. "TOOL_RESULT_STATUS_SUCCEEDED"
    },
  });
}
```

- `content` is the existing `MouseContentBlock[]` from `buildResultBlocks` (unchanged) — the model sees the same text + screenshot it sees today.
- `additional_kwargs.toolResultStatus` carries the real status (resolved by `bridge.dispatch` from the desktop's `ToolResultPart.status`). It is serialized through `MemorySaver` alongside `content`.
- `tool_call_id` links the result to the originating `AIMessage.tool_calls[i].id` (LangChain's ToolNode uses this for the model's tool-result loop).

### Saolei tools — equivalent helper

The saolei tools' `resultFromDispatch` (`saolei-mcp.ts:159`) is changed the same way: it returns a `ToolMessage` (via a saolei-specific builder or by reusing `buildToolResultMessage`) carrying the dispatch text + screenshot + `additional_kwargs.toolResultStatus`. The MCP tool handler returns the `ToolMessage` content to the MCP client (the `@langchain/mcp-adapters` loopback); the agent's ToolNode receives it.

> Note on MCP tool returns: an MCP tool handler returns MCP content blocks (`{ content: [...] }`), and the `@langchain/mcp-adapters` client wraps them into a `ToolMessage`. To carry `additional_kwargs.toolResultStatus` for saolei tools, the handler encodes the status alongside the content (the adapter-built `ToolMessage` must end up with `additional_kwargs.toolResultStatus`). The exact mechanism (handler returns a content block the adapter forwards as additional_kwargs, or the agent post-processes the ToolMessage) is an implementation detail constrained by: **the checkpointed ToolMessage for a saolei tool carries `additional_kwargs.toolResultStatus` equal to the dispatch outcome.** Implementation MUST verify this end-to-end with a round-trip test (research.md D4 verification).

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

`projects/game/agent/src/mcp/saolei/saolei-mcp.ts` is rewritten. `createSaoleiMcpServer(bridge: OperationBridge)` (no `initialState` parameter) registers exactly four tools:

### `saolei_init` — no arguments

- **inputSchema**: `{}` (no `width`/`height` — dropped per C11; they affected only agent-side state).
- **Behaviour**: `bridge.dispatch({ keyboardPress: { key: "KEYBOARD_KEY_F2" } })` and return the result + screenshot.
- **Returns**: `ToolMessage` (via the saolei result builder, §3) with text `"saolei_init: F2 dispatched (new game)"` + screenshot + `toolResultStatus` from the dispatch.
- Re-calling re-dispatches F2 (FR-019).

### `saolei_click(x, y)` / `saolei_flag(x, y)` / `saolei_chord_click(x, y)`

- **inputSchema** (each): `{ x: int ≥ 0, y: int ≥ 0 }` (top-left origin `(0,0)`; `x`=column, `y`=row — unchanged convention).
- **Behaviour**: compute `(xPx, yPx) = center(x, y)` via `geometry.ts` (unchanged formula); `bridge.dispatch({ mouseMoveAndClick: { xPx, yPx, click: <ACTION>, method: "MOUSE_INPUT_METHOD_WINDOW_MESSAGE" } })`; return the result + screenshot.
  - `saolei_click` → `click: "MOUSE_CLICK_ACTION_LEFT_CLICK"`.
  - `saolei_flag` → `click: "MOUSE_CLICK_ACTION_RIGHT_CLICK"`.
  - `saolei_chord_click` → `click: "MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS"`.
- **Returns**: `ToolMessage` with text `"<tool> dispatched at (x,y)"` + screenshot + `toolResultStatus`.
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
- `tool_result`: yielded when a `ToolMessage` is produced. Source: `ToolMessage.content` blocks (message + screenshot) + `ToolMessage.additional_kwargs.toolResultStatus` + `ToolMessage.tool_call_id`.

The `Connect` handler (`handler.ts:379`) emits a `message_parts` frame for each: `{ payload: "messageParts", messageParts: { parts: [{ toolCall: {...} }] } }` or `[{ toolResult: {...} }]`. This makes the live conversation show the same tool calls/results that `ListMessages` reconstructs (single source of truth, FR-009).

> The exact accessor for detecting "the streamed AI message now carries tool_calls" and "a ToolMessage was produced" is verified against the pinned `@langchain/langgraph` ^1.4.8 / `langchain` ^1.5.3 at implementation time (the repo's `spike.test.ts` already documents `AIMessageChunk.tool_calls`). If `stream.messages` does not expose them at the right timing, the fallback is to switch to raw `streamEvents` (`on_chat_model_end` for tool_calls, `on_tool_end` for results) — research.md D5.

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
- The evolving-bubble frontend view model and Confirm anchoring → `data-model.md` §5.
- The desktop `recvLoop`/`handleInboundOperation` changes (FlowParts execution, result-mirror removal) → `data-model.md` §8/§9 + `quickstart.md`.
