# Contract: Desktop UI State Flow

## Session Detail Page

### State: Checking Agent

**Trigger**: Operator selects a session.

**UI**:
- Show session identity and loading state.
- Do not show profile selector until no-agent is confirmed.
- Do not open WebSocket connection.

### State: Setup Required

**Trigger**: Selected session has no agent.

**UI**:
- Show profile selector and Create Agent button.
- Hide agent summary.
- Hide connection controls.
- Enter Play is unavailable until agent creation succeeds.

### State: Agent Ready

**Trigger**: Selected session has an agent.

**UI**:
- Show agent summary: name, profile name, creation time.
- Show Enter Play.
- Hide profile selector.
- Hide manual connect control.
- Do not open WebSocket connection.

### State: Error

**Trigger**: Agent metadata/profile load fails.

**UI**:
- Show actionable error and retry.
- Preserve Back to Sessions.

## Play Page

### State: Connecting

**Trigger**: Operator clicks Enter Play.

**UI**:
- Show chat shell and sidebar with agent metadata.
- Show connection progress.
- Disable or queue sends until connection succeeds.

### State: Loading History

**Trigger**: Connection established or play page initialized.

**UI**:
- Show loading indicator in chat thread.
- Retrieve agent messages through the `ListMessages` API.

### State: Chat Ready

**Trigger**: History loaded or empty history confirmed.

**UI**:
- Render chronological history entries.
- Enable input.
- Sidebar shows agent name, profile name, model, connection status, and View Profile.

### State: Processing

**Trigger**: Operator sends a message.

**UI**:
- Append user message immediately.
- Show processing/typing state until wait frame.
- Preserve queue indicator if a send is waiting for connection or turn completion.

### State: Connection Error

**Trigger**: Play-entry connection fails or send fallback cannot connect.

**UI**:
- Show visible error with retry.
- Keep sidebar metadata visible.
- Do not return to setup unless agent metadata is missing.

### State: Agent Lost

**Trigger**: Agent metadata is missing on re-entry (process restart cleared in-memory state, or the agent was externally deleted). A `GetAgent` not-found while the operator believed an agent existed.

**UI**:
- Show an explicit "Agent unavailable — process may have restarted" message.
- Offer "Recreate agent" returning to the Session Detail `setup required` state, preserving session identity.
- Do NOT silently drop into `setup required` — the transition must be operator-visible because prior conversation context is gone.
- Preserve Back to Sessions.

### WebSocket Lifecycle

- On play exit (back to session detail or sessions list), the WebSocket connection closes.
- Re-entering play re-establishes the connection as part of the `connecting` state.
- Agent metadata and checkpoint state persist regardless of WebSocket state — closing the WS does not lose conversation context.
- No automatic reconnection: if the connection drops during play, the UI shows `connection error` with operator-initiated retry only.

## Sidebar Profile Details

The View Profile control displays read-only profile details: profile name, model, enabled status, system prompt, skill names, and MCP names. Editing profiles remains outside this feature.
