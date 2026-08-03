# Contract: FlowResultPart (operation-result channel separation)

**Feature**: [spec.md](../spec.md) (FR-023..FR-026) | **Research**: [research.md](../research.md) D2 | **Data model**: [data-model.md](../data-model.md) §1

This contract specifies the new `FlowResultPart` proto message, its placement in the control channel, the migration from the current display-`tool_result` conflation, and the per-tool translation into the display `tool_result`.

## 1. Problem (why)

Today the desktop wraps its operation-execution result as a **display** `MessagePart{tool_result}` in a `message_parts` frame (`projects/game/desktop/app.go:892-898`), but that frame is consumed **only** by `OperationBridge.handleResult` (`projects/game/agent/src/operation-bridge.ts:261`) as a *control* message — it is never rendered. This is a semantic abuse: a display-channel message carrying a control-channel response, conflating the two channels that 023 (C13) set out to decouple.

## 2. Proto

Add a new message and a new `FlowPart` oneof kind in `projects/game/game.proto` (exact field numbers and the oneof extension are in [data-model.md](../data-model.md) §1):

- `message FlowResultPart { string tool_id = 1; ToolResultStatus status = 2; string message = 3; ImagePart screenshot = 4; }`
- `FlowPart.kind` gains `FlowResultPart flow_result = 8;`

`status` reuses `ToolResultStatus` (no new enum). `screenshot` is an `ImagePart` (same shape as today's `ToolResultPart.screenshot`). Nothing else in the proto changes (`AgentFrame`, `MessagePart`, the operation-request kinds, `ImagePart`, signals — all unchanged).

## 3. Channel semantics

| Channel | Payload | `tool_id` correlation | Rendered? |
|---|---|---|---|
| **Control** (`FlowParts`) | operation requests (mouse/keyboard) **and** `FlowResultPart` responses | bridge-minted id (`OperationBridge.dispatch`, `operation-bridge.ts:208`) | **No** — consumed for execution + dispatch resolution |
| **Conversation** (`MessageParts`) | `tool_call`, `tool_result` | LangChain `tool_call.id` (023 C13) | **Yes** — rendered in the conversation |

The `FlowResultPart.tool_id` matches the originating request `FlowPart.tool_id` (operation channel only). It has **no** relation to any conversation `tool_call.id` — the two channels remain fully decoupled (023 C13).

## 4. Migration (desktop → control channel)

Before (conflation):

```
desktop executeAgentOperation → *ToolResultPart
desktop handleInboundOperation → AgentFrame{message_parts:[{tool_result: result}]}  // display frame, control use
```

After (separation):

```
desktop executeAgentOperation → *FlowResultPart                      // same fields, new type
desktop handleInboundOperation → AgentFrame{flow_parts:[{flow_result: result}]}  // control frame
```

Concrete changes:
- `projects/game/desktop/app.go` `executeAgentOperation` returns `*game.FlowResultPart` (fields identical to today's `*game.ToolResultPart`: `ToolId`, `Status`, `Message`, `Screenshot`). The "no window selected" guard (FR-005, replacing `app.go:1074`) and the post-action screenshot block (`app.go:1129-1150`) populate the same fields.
- `projects/game/desktop/app.go` `handleInboundOperation` builds a `flow_parts` frame with a `FlowPart{flow_result}` (replacing the `message_parts`/`tool_result` frame at `app.go:892-898`). The debug-hold logic (`holdAndRelease`, `app.go:906`) is unchanged — it sits between execute and send.
- The desktop **no longer emits** a `tool_result` `MessagePart` for any operation outcome (FR-024).

## 5. Migration (agent → consumes control, emits display)

- `projects/game/agent/src/operation-bridge.ts` `handleResult(result: FlowResultPart)` — signature changes from `ToolResultPart` to `FlowResultPart`. The internal `OperationResult` interface (`status`, `message`, `screenshot?`) is unchanged; only the input type changes.
- `projects/game/agent/src/handler.ts` — the inbound frame router: `flow_parts` frames carrying a `flow_result` kind are routed to `bridge.handleResult` (today the router pulls `tool_result` out of `message_parts`; it now pulls `flow_result` out of `flow_parts`).
- The display `tool_result` `MessagePart` is emitted by the **agent** from each tool's LLM result (unchanged from 023 C8 for mouse tools; saolei now emits a text-only `tool_result` per [saolei-mcp-contract.md](./saolei-mcp-contract.md)).

## 6. Per-tool translation (FlowResultPart → display tool_result)

| Tool family | Reads from `FlowResultPart` | Emits display `tool_result` |
|---|---|---|
| Native mouse (`mouse_click`, `mouse_move`) | `status`, `message`, `screenshot` | text + screenshot (per 023 C8/FR-010); status carried via `additional_kwargs.toolResultStatus` (023 C15) |
| Saolei MCP | `screenshot` (→ recognition) | **text board only**, no screenshot (FR-012/FR-022) |

For saolei, `FlowResultPart.screenshot` is consumed by `@dominion/game-saolei-board` for recognition and **never** copied into the display `tool_result` (FR-026).

## 7. Forward compatibility

`proto.Unmarshal` preserves unknown fields per the proto spec, so the switch does not regress the `DiscardUnknown: true` forward-compatibility behavior that `protojson` provided on the WS leg today (`projects/game/gateway/cmd/main.go:157`). Older clients that send a display `tool_result` for an operation result are not supported (clean break, consistent with 023 C2 — this feature is not expected to interoperate with pre-025 desktop builds).

## 8. Test anchors

- Desktop unit: `executeAgentOperation` returns `*FlowResultPart`; `handleInboundOperation` sends a `flow_parts` frame whose single part is `flow_result` (and no `message_parts`/`tool_result` is emitted).
- Agent unit: `OperationBridge.handleResult` resolves a pending dispatch from a `FlowResultPart`; the inbound router sends `flow_result` (from `flow_parts`) to `handleResult` and does not treat it as a display `tool_result`.
- Large test: an end-to-end turn dispatches a mouse/saolei operation and the agent receives the `FlowResultPart` and emits the correct display `tool_result` ([quickstart.md](../quickstart.md)).
