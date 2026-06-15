# Contract: Agent Service API

## Scope

This contract defines the new public API surface for the agent service after the SessionAgent/AgentAdapter decoupling. It replaces the previous Create/Get/Delete agent lifecycle model with a connect-centric model.

## Removed Operations

### CreateAgent

- **Previous surface**: `POST /api/v1/sessions/{id}/agent` (ProxyService) and internal `AgentService.CreateAgent` RPC.
- **Status**: REMOVED. Adapter creation is implicit — the adapter is created on-demand when the first text frame specifying a profile is processed.
- **Migration**: Clients no longer call CreateAgent before connecting. The connect step implicitly handles adapter initialization.

### DeleteAgent

- **Previous surface**: `DELETE /api/v1/sessions/{id}/agent` (ProxyService) and internal `AgentService.DeleteAgent` RPC.
- **Status**: REMOVED. Session deletion no longer explicitly cleans up agent service state. Orphaned MemorySaver entries are acceptable within the in-memory process-lifetime scope.
- **Migration**: Clients no longer call DeleteAgent. Deleting the session is sufficient.

## Retained Operations (Modified)

### GetAgent

- **Surface**: `GET /api/v1/sessions/{id}/agent` (ProxyService) — path unchanged.
- **Semantics change**: Returns the current adapter state rather than a created-agent resource.
- **Response fields**:
  - `name`: `sessions/{id}/agent` (unchanged).
  - `session_id`: the session identifier (unchanged).
  - `agent_profile_name`: the currently active profile name, or empty if no adapter is connected.
  - `create_time`: session creation time (SessionAgent lifecycle = session lifecycle).
- **Behavior**: Returns 200 with adapter state if the session exists; returns 404 if the session does not exist.

### ListMessages

- **Surface**: `GET /api/v1/sessions/{id}/messages` (ProxyService) — path changed from `/sessions/{id}/agent/messages`.
- **Semantics**: Unchanged. Returns chronological conversation messages for the session.
- **Parent field**: Now `sessions/{id}` (session-level) instead of `sessions/{id}/agent` (agent-level).
- **Behavior**: Returns messages in chronological order. Returns empty list for sessions with no conversation. Returns 404 if the session does not exist.

### ConnectAgent (WebSocket)

- **Surface**: WebSocket at `/api/v1/sessions/{id}/connect` — path changed from `/sessions/{id}/agent/connect`.
- **Semantics**: Bidirectional stream exchanging AgentFrame units. The connection is per-session (not per-agent).
- **Profile selection**: Each text frame carries an optional `agent_profile_name` field. The first text frame with a profile name triggers adapter creation. Subsequent frames with a different profile name trigger adapter switching.
- **Connection exclusivity**: Only one WebSocket connection per session at any time. A new connection forcibly closes the previous one.
- **Frame behavior**: Unchanged payload types (text, thinking, status, echo, warn, wait). Agent response frames carry `agent_profile_name` to identify the producing profile.

## New Proto Field

### AgentFrame.agent_profile_name

- **Field number**: 21
- **Type**: `string` (optional)
- **Semantics**:
  - USER text frames: specifies which profile should process the message. Triggers adapter creation or switching.
  - AGENT text/thinking frames: identifies which profile produced this response.
  - Control frames (status, echo, wait, warn): not carried.
- **Default**: empty string when not applicable.

## Invariants

- SessionService (CreateSession, ListSessions, GetSession, DeleteSession) remains unchanged.
- PromptService (AgentProfile/Skill CRUD) remains unchanged.
- AgentFrame payload types (text, thinking, status, echo, warn, wait, screenshot, operation, etc.) remain unchanged.
- FrameSender enum remains unchanged.
- Message proto structure remains unchanged.
- Same-session message processing remains serialized (FIFO).
- Conversation history is keyed by session identity and shared across all profiles within the session.
- Session deletion does not explicitly clean up agent service MemorySaver state (acceptable for in-memory scope).

## Gateway Route Changes

| Operation | Old Route | New Route | Status |
|-----------|-----------|-----------|--------|
| CreateAgent | `POST /sessions/{id}/agent` | — | REMOVED |
| GetAgent | `GET /sessions/{id}/agent` | `GET /sessions/{id}/agent` | UNCHANGED path, modified semantics |
| DeleteAgent | `DELETE /sessions/{id}/agent` | — | REMOVED |
| ListMessages | `GET /sessions/{id}/agent/messages` | `GET /sessions/{id}/messages` | PATH CHANGED |
| ConnectAgent (WS) | `WS /sessions/{id}/agent/connect` | `WS /sessions/{id}/connect` | PATH CHANGED |

## Removed Proto Messages

- `CreateAgentRequest` (ProxyService)
- `DeleteAgentRequest` (ProxyService)
- `AgentCreateRequest` (AgentService)
- `AgentDeleteRequest` (AgentService)

## Test Contract

Unit tests must prove:

1. Connect establishes a session-scoped WebSocket without requiring prior agent creation.
2. Text frames with `agent_profile_name` trigger adapter creation and message processing.
3. Text frames with a different `agent_profile_name` trigger adapter switching.
4. Agent response frames carry the correct `agent_profile_name`.
5. A second WebSocket connection to the same session closes the first.
6. GetAgent returns the current adapter state (active profile or empty).
7. ListMessages returns session-scoped chronological messages.
8. Same-session rapid sends preserve FIFO order.

Large tests must prove:

1. The public connect-chat-switch-profile-reconnect-list flow works end-to-end.
2. Conversation history persists across profile switches and reconnections.
3. Connection exclusivity (kick old) works in production deployment.
4. Message order and visible content remain compatible.
