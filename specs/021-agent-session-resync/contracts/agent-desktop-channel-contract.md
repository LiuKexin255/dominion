# Contract: Agent ↔ Desktop Channel Behaviors

**Feature**: `021-agent-session-resync` | **Date**: 2026-07-24

This contract specifies the **behaviors added over the existing agent↔desktop bidirectional session channel** (`AgentFrame` oneof). No proto schema is added (research.md D8); these are protocol/semantic contracts over `StatusSignal` and `ToolResultPart`.

Reference messages (`projects/game/game.proto`):
- `StatusSignal { StatusSignalStatus status }`, `StatusSignalStatus ∈ {UNSPECIFIED, ACTIVE, IDLE}`
- `ToolResultPart { tool_id, status, message, screenshot }`, `ToolResultStatus ∈ {UNSPECIFIED, SUCCEEDED, FAILED}`
- `WarnSignal { message, code }`, `WaitSignal { reason }`

---

## 1. Status ping-pong (request/response)

**Direction:** desktop-initiated request → agent response, over the same bidi stream.

### Request (desktop → agent)

| Field | Value |
|---|---|
| `AgentFrame.payload` | `status` |
| `StatusSignal.status` | the desktop's signal (existing connect probe sends `ACTIVE`; semantics of the outbound value are not consumed by the agent today) |

**When sent:** on session (re-)entry, as the existing connect-time application-level probe (Go `ConnectAgent`). No periodic heartbeat is required by this feature.

### Response (agent → desktop)

| Field | Value |
|---|---|
| `AgentFrame.payload` | `status` |
| `AgentFrame.sender` | `SYSTEM` |
| `StatusSignal.status` | derived per [data-model.md §1](../data-model.md): `ACTIVE` if a turn is in-flight (`isMutexHeld`); else `IDLE` if an adapter is bound; else `UNSPECIFIED` |

**Guarantees:**
- The agent MUST respond to every inbound `status` frame (it already does; the value now reflects real working state).
- The "in-flight" source is the shared per-session turn mutex, not a per-stream flag.

### Desktop reconciliation (Go + frontend)

- Go `ConnectAgent` MUST capture the response frame's `StatusSignalStatus` and return it to the frontend (today the response is discarded).
- The frontend MUST, on session entry, reconcile the typing indicator against the returned status: `ACTIVE` ⇒ indicator on (`processing = true`); `IDLE`/`UNSPECIFIED` ⇒ indicator off (`processing = false`, `playState = 'chat_ready'`).
- `resetPlayPageState` MUST defensively reset `processing = false` on entry (the probe then refines it).

**Non-functional:** the probe retains its existing 10s timeout; on timeout/no-response the existing connection-error handling applies (the desktop does not assume idle indefinitely).

---

## 2. Display-only tool result (agent → desktop)

**Direction:** agent → desktop, one-way (no correlation, no awaited response).

### Frame

| Field | Value |
|---|---|
| `AgentFrame.payload` | `content` |
| `AgentFrame.sender` | `SYSTEM` |
| `AgentFrame.frameId` | fresh UUID |
| `AgentFrame.agentProfileName` | the bound profile |
| `PartBlock.parts[0]` | `tool_result` |
| `ToolResultPart.toolId` | display-only id (not correlated to any `dispatch`) |
| `ToolResultPart.status` | `SUCCEEDED` (acceptance) \| `FAILED` (validation rejection) — [research.md D5](../research.md) |
| `ToolResultPart.message` | self-descriptive: `<tool>: <outcome/reason>` |
| `ToolResultPart.screenshot` | absent |

**When sent:** immediately after an agent-internal tool resolves, for tools that produce no desktop operation (today: `saolei_update`). Sent via `OperationBridge.pushResult` (writes to the current sink; no-op if no sink; creates no `pending` entry).

**Desktop handling (unchanged):** the desktop `recvLoop` appends the frame to the chat stream and renders it as a result card (`ChatView` `toolResult` kind). It MUST NOT execute any input action (the part is not an operation kind). The desktop MUST NOT echo a `ToolResultPart` back for it.

**Status semantics note:** the saolei tools never return an MCP-level error (018 D8). The `FAILED` status is a display-only affordance the agent applies when forwarding a logically-rejected update; it is not an MCP error and does not change the model's received result.

**Best-effort delivery:** if the connection is broken at the moment of forwarding, the live frame may be lost; it remains part of the agent's persisted turn history and is restored on the next history replay (the frame is emitted on the stream like any agent content).
