# Feature Specification: Agent Session Resync & Adapter Simplification

**Feature Branch**: `021-agent-session-resync`

**Created**: 2026-07-24

**Status**: Draft

**Input**: User description: "对于 specs/018-saolei-mcp/ 实现进行一些优化：1) saolei_update 没有展示在 desktop 上，对于这种不需要 desktop 处理的 tool，desktop 只展示不处理即可，可能的改动是 desktop 需要接收服务端发来的 ToolResultPart。2) desktop session 退出再重进后，所有 mcp tools 都显示为 fail。3) desktop session 退出再重进后，一直显示 'agent is typing'，agent 状态没有正常同步；为 agent 服务增加'状态 ping-pong'，客户端发送 statusSignal 报告自己状态，agent 服务收到后也发送 StatusSignal 给客户端，使 session 重新进入后能正确获取 agent 状态。4) 移除 agent 服务的自动切换 agent adapter 功能，session agent 的 profile 和 adapter 的重建都由 Refresh 操作负责，重构这部分以简化。"

## Clarifications

### Session 2026-07-24

- C1 (scope of tool-result display): The "display internal tool results" change (saolei_update) is scoped to **tool calls that the agent resolves entirely server-side and that therefore produce no operation Part for the desktop to execute** — today only `saolei_update`. Tools that *do* dispatch an operation to the desktop (`saolei_init`/`saolei_click`/`saolei_flag`/`saolei_chord_click`) already surface on the desktop via the existing operation result mirror, and their existing display path is unchanged. The agent forwards a `ToolResultPart` (with a self-descriptive message) for the agent-internal tool so the desktop renders it as a result card without executing anything.
- C2 (status values reported by ping-pong): The agent's status response reuses the existing `StatusSignalStatus` enum. `IDLE` = no turn in-flight (ready for input); `ACTIVE` = a turn is currently in-flight (the agent is working). On session re-entry the desktop sends a `StatusSignal` and reconciles its "typing" indicator against the response: an `IDLE`/not-working response clears the indicator; an `ACTIVE`/working response keeps it. This supersedes the previous behavior where the desktop's status probe response was received but ignored.
- C3 (forwarded tool-result status semantics): Q: For forwarded agent-internal tool results (e.g. `saolei_update`), how is the `ToolResultPart` status set when the tool's logical outcome is a validation rejection? → A: Reflect the logical outcome — SUCCEEDED when the update is accepted, FAILED when validation rejects it (reason carried in the message). This is clearer for the operator than a success marker beside rejection text, and the agent already knows the outcome at forwarding time. (The saolei tools themselves never return an MCP-level error per the 018 decision D8; the FAILED status here is a display-only affordance the agent applies when forwarding, not an MCP error.)
- C4 (profile-name guard on turn entry): Q: After removing the auto-switch, what happens to a turn whose profile name differs from the bound adapter's? → A: The turn is rejected (blocked from entering the agent) with a warning naming the mismatch; the agent emits the warning together with the turn-completion signal so the desktop returns to ready, the session agent does not panic, and subsequent turns are accepted normally. The guard does not block the initial build or a post-Refresh rebuild (no bound adapter ⇒ accept). This replaces the earlier "ignore the mismatch and run against the current adapter" behavior, because silently running a turn against the wrong profile (wrong model/tools/MCPs) is more confusing and riskier than a clear rejection.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Agent Status Re-syncs on Session Re-entry (Priority: P1)

An operator starts a turn in a desktop session and then leaves the session (backs out to the session list) while the agent is still working. When the operator re-enters that session, the desktop asks the agent for its current status. If the agent is no longer working, the "Agent is typing…" indicator clears instead of staying stuck forever; if the agent is still working, the indicator correctly reflects that.

**Why this priority**: The stuck "typing" indicator is the most visible reconnect defect — it makes a re-entered session appear frozen even when the agent is idle and ready. It blocks any further usable interaction until the operator reloads.

**Independent Test**: Start a turn, leave the session mid-turn, re-enter, and verify the typing indicator matches the agent's real working/idle state (cleared when idle).

**Acceptance Scenarios**:

1. **Given** a turn is in-flight on the agent, **When** the operator leaves and re-enters the session, **Then** the desktop queries the agent status and the "typing" indicator reflects whether the agent is currently working (clears if idle, shows if working).
2. **Given** the agent is idle (no turn in-flight), **When** the operator re-enters a session that previously showed "Agent is typing…", **Then** the typing indicator is cleared and the operator can send a new message.
3. **Given** a working agent, **When** the operator re-enters and the agent later completes its turn, **Then** the typing indicator clears when the turn ends.

---

### User Story 2 - Saolei MCP Tools Dispatch Reliably After Reconnect (Priority: P1)

After an operator leaves a desktop session and re-enters it (or after the desktop connection drops and is re-established), the saolei MCP tools the model calls during a new turn must dispatch to the desktop and succeed. They must not be reported as failed purely because of the disconnect/reconnect sequence.

**Why this priority**: A reconnect that silently breaks every saolei tool call makes the saolei profile unusable after any session navigation — equal in severity to the typing-indicator defect because it breaks the core gameplay loop.

**Independent Test**: Open a saolei-enabled session, leave and re-enter, start a turn, and verify `saolei_init`/`saolei_click`/etc. dispatch to the desktop and return succeeded (not failed) results.

**Acceptance Scenarios**:

1. **Given** a desktop session that has been left and re-entered, **When** the model calls a saolei operation tool during a new turn, **Then** the operation is dispatched to the desktop over the current connection and returns a valid result.
2. **Given** a desktop connection that dropped and was re-established, **When** the next turn runs, **Then** no leftover state from the closed connection causes tool dispatches to fail.
3. **Given** a normal (never-disconnected) session, **When** saolei tools are called, **Then** behavior is unchanged from before this feature (no regression).

---

### User Story 3 - Agent-Internal Tool Results Are Visible on the Desktop (Priority: P2)

When the model calls `saolei_update` (an agent-side tool that does not produce a desktop operation), the desktop displays the tool's result so the operator can see what happened — exactly like the results of dispatched operations, but without the desktop executing anything.

**Why this priority**: This is a visibility/usability gap rather than a broken interaction. The core loop works; the operator simply cannot observe the update step. It is sequenced after the reconnect defects.

**Independent Test**: Run a turn that includes a `saolei_update` call and confirm a result card for it appears on the desktop; confirm the desktop performs no input action for it.

**Acceptance Scenarios**:

1. **Given** the model has called `saolei_update`, **When** the tool completes, **Then** the desktop displays a result entry reflecting its outcome (succeeded/failed and a descriptive message).
2. **Given** a `saolei_update` result forwarded to the desktop, **Then** the desktop performs no mouse/keyboard input action for it (display-only, never executed).
3. **Given** the saolei operation tools (`init`/`click`/`flag`/`chord_click`), **When** they complete, **Then** their existing desktop display path is unchanged.

---

### User Story 4 - Adapter Lifecycle Simplified to Refresh-Only With Profile Guard (Priority: P2)

The agent no longer automatically rebuilds its adapter when it notices a different agent profile on an incoming turn. Instead, once a session is bound to a profile, a turn carrying a *different* profile name is actively rejected (blocked from entering the agent) with a warning, so the operator is told to Refresh rather than silently running a turn against the wrong profile. Changing or refreshing the session's profile/adapter is the sole responsibility of the explicit Refresh operation, which simplifies the agent's session lifecycle and removes an implicit, hard-to-reason-about switching path. The rejection is non-fatal: it never crashes the session agent and never blocks subsequent messages.

**Why this priority**: This is a maintainability/simplification change with no user-visible behavior change when the desktop already calls Refresh after profile changes. It removes accidental complexity rather than fixing a defect, so it is the lowest priority.

**Independent Test**: Send turns under one profile, send a turn with a different profile name (no Refresh) and confirm it is rejected with a warning, then change the profile via Refresh and confirm the adapter rebuilds only through Refresh.

**Acceptance Scenarios**:

1. **Given** a session bound to a profile, **When** an incoming turn carries a different profile name but no Refresh has occurred, **Then** the turn is rejected (blocked from entering the agent), the adapter is NOT rebuilt, and the operator receives a warning naming the mismatch (expected vs. received profile).
2. **Given** a rejected (profile-mismatched) turn, **Then** the session agent does NOT panic and a subsequent turn (with the matching profile name, or after Refresh) is accepted normally — the rejection does not block later messages.
3. **Given** the operator changes the profile and triggers Refresh, **When** the next turn runs, **Then** the adapter is rebuilt with the new profile's configuration (model, prompt, tools, MCPs, skills).
4. **Given** a session with no adapter yet (first turn, or just after Refresh invalidated the adapter), **When** a turn arrives carrying a profile name, **Then** it is accepted and the adapter is built for that profile (the guard does not block the initial/rebuild build).

---

### Edge Cases

- What happens when the status ping-pong response arrives after the operator has already started a new turn? The desktop reconciles to the most recent known state: a turn the operator just sent keeps the typing indicator on regardless of a late IDLE response; it clears when that turn's completion signal arrives.
- What happens if the agent status probe never gets a response (agent unreachable)? The existing connection-error handling applies; the desktop does not assume idle indefinitely — it surfaces the connection problem rather than masking it.
- What happens when the desktop reconnects mid-turn while the agent is still working? The agent aborts the in-flight turn on the closed stream (existing behavior); on reconnect the desktop learns (via status ping-pong) that no turn is in-flight and clears the typing indicator.
- What happens when an agent-internal tool result is forwarded to the desktop but the connection is broken at that moment? It is best-effort: the result may be absent from the live stream, but it remains part of the persisted history and is restored on the next history replay.
- What happens when `saolei_update` is rejected by validation? The forwarded result is displayed as FAILED with the rejection reason in the message (C3); the desktop performs no input action. This display-only FAILED status does not correspond to an MCP error (the tool still returns normally to the model per 018 decision D8).
- What happens when a turn carries a different profile name but the desktop never calls Refresh? The turn is rejected with a warning (FR-012a), the desktop returns to ready, and the operator must Refresh to switch profiles. The session agent stays healthy and accepts subsequent matching turns.
- What happens to a session whose adapter was invalidated by Refresh but the operator sends a turn before the rebuild finishes? The adapter builds lazily on that turn (existing serialized-bind behavior); the turn waits for the build to complete rather than failing.

## Requirements *(mandatory)*

### Functional Requirements

**Agent status re-sync (ping-pong)**

- **FR-001**: The agent MUST expose a status request/response over the session channel: when the desktop sends a `StatusSignal`, the agent MUST respond with a `StatusSignal` carrying the agent's actual working state for that session.
- **FR-002**: The agent's status response MUST distinguish working from idle: `ACTIVE` when a turn is currently in-flight for the session, and `IDLE` when no turn is in-flight (ready for input). When the session has no bound adapter, the response MUST indicate it is not ready.
- **FR-003**: On (re-)entering a session, the desktop MUST query the agent's status via the ping-pong and reconcile its "agent is typing" indicator to the response — clearing it when the agent reports idle/not-working, and keeping it when the agent reports working.
- **FR-004**: The desktop MUST NOT leave the "agent is typing" indicator stuck after re-entering a session whose prior turn is no longer in-flight. Re-entering a session MUST reset the indicator to a value consistent with the agent's current status (not the stale state from before leaving).

**Reliable dispatch after reconnect**

- **FR-005**: After a desktop session exits and re-enters (or the connection drops and is re-established), saolei operation tools called during a new turn MUST dispatch to the desktop over the current connection and return valid results — they MUST NOT fail solely because of the disconnect/reconnect sequence.
- **FR-006**: The agent-to-desktop dispatch path MUST be tied to the live connection for the current turn; cleanup triggered by a closing connection MUST NOT invalidate the dispatch path of a different, still-live connection/turn (no cross-connection interference).
- **FR-007**: Existing dispatch behavior on a never-disconnected session MUST remain unchanged (no regression for the normal path).

**Display of agent-internal tool results**

- **FR-008**: For a tool call that the agent resolves entirely server-side and that produces no desktop operation (e.g., `saolei_update`), the agent MUST forward a `ToolResultPart` to the desktop so the operator can observe the outcome.
- **FR-009**: A forwarded agent-internal `ToolResultPart` MUST carry a self-descriptive message identifying the tool and its result, so the operator can understand it without additional context. Its status MUST reflect the tool's logical outcome — SUCCEEDED when the update is accepted, FAILED when validation rejects it (the rejection reason carried in the message).
- **FR-010**: The desktop MUST render a forwarded agent-internal `ToolResultPart` as a display-only result (same surface used for operation results), and MUST NOT execute any input action for it.
- **FR-011**: Tools that DO dispatch an operation to the desktop MUST keep their existing display path unchanged; this feature MUST NOT duplicate their results or alter their rendering.

**Adapter lifecycle simplification**

- **FR-012**: The agent MUST NOT implicitly rebuild the session adapter when an incoming turn carries a different agent profile name than the currently bound one. The per-turn profile name MUST NOT, by itself, trigger an adapter rebuild.
- **FR-012a**: When a session already has a bound adapter and an incoming turn carries a profile name that differs from the bound one, the agent MUST reject that turn (block it from entering the agent) and emit a warning that names the mismatch (the bound profile vs. the received profile). The rejection MUST be non-fatal: it MUST NOT crash/panic the session agent, MUST NOT leak the per-session turn mutex, and MUST NOT prevent a subsequent turn (matching profile, or arriving after Refresh) from being accepted normally.
- **FR-012b**: A rejected (profile-mismatched) turn MUST still return the desktop to a ready state — the agent MUST emit the warning together with the turn-completion signal the desktop uses to clear its "typing" indicator, so the operator can immediately send the next message.
- **FR-012c**: The profile guard MUST NOT block the initial build or a post-Refresh rebuild: when the session has no bound adapter, a turn carrying any profile name MUST be accepted and the adapter built for that profile.
- **FR-013**: The session adapter MUST be rebuilt ONLY via the explicit Refresh operation (profile/adapter refresh). The initial adapter for a session MUST still be built lazily on the first turn.
- **FR-014**: After the operator changes the agent profile and triggers Refresh, the next turn MUST run against a freshly built adapter reflecting the new profile's model, system prompt, tools, MCPs, and built-in skills.

### Key Entities *(include if feature involves data)*

- **Agent working state (per session)**: whether a turn is currently in-flight (working) or the agent is ready for input (idle) — the value the status ping-pong reports and that reconciles the desktop's typing indicator.
- **Agent-internal tool result**: the outcome of a tool resolved entirely server-side (no desktop operation), forwarded to the desktop as a display-only `ToolResultPart` so the operator can observe it.
- **Session adapter lifecycle**: the single path (Refresh) by which a session's agent adapter is (re)built after the initial build; the per-turn profile name no longer participates in switching adapters.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of tested reconnect cases, re-entering a session whose agent is idle clears the "Agent is typing…" indicator (no stuck typing indicator remains).
- **SC-002**: In 100% of tested reconnect cases, saolei operation tools called during a new turn after reconnect dispatch to the desktop and return succeeded results (zero spurious "failed" results attributable to the reconnect).
- **SC-003**: In 100% of tested turns that include `saolei_update`, the desktop displays a result entry for the update and performs no input action for it.
- **SC-004**: In 100% of profile-change tests, the adapter rebuilds exclusively via Refresh; a different profile name on a turn frame without a preceding Refresh is rejected with a warning (not silently run, not rebuilt), and the rejection leaves the session able to accept subsequent turns.
- **SC-005**: No regression in the normal (never-disconnected) path: tool dispatch, tool-result display, and turn completion behave exactly as before for sessions that never disconnect.

## Assumptions

- The agent and desktop already exchange `AgentFrame` messages over a bidirectional session channel, and the proto already defines `StatusSignal`/`StatusSignalStatus` (`UNSPECIFIED`/`ACTIVE`/`IDLE`) and `ToolResultPart`. These optimizations primarily change *behavior* over the existing message set; they are expected to require little or no proto change, with exact proto deltas confirmed at plan time.
- The agent can determine its own working state per session via its existing per-session turn serialization (a turn is in-flight exactly while the session's turn mutex/active-turn tracker is held). Reusing this as the source of truth for `ACTIVE` vs `IDLE` is assumed; the precise accessor is a plan-time decision.
- The "agent-internal tool result" display is scoped to tools that resolve server-side with no desktop operation (today: `saolei_update`). A general mechanism for surfacing arbitrary LLM tool calls (names + arguments) to the desktop is out of scope; only the result is forwarded, with a self-descriptive message.
- The desktop already calls Refresh after an agent-profile update (per the 018 implementation), so removing the implicit per-turn adapter switch does not change observable behavior in the normal flow. If any code path relied on the implicit switch, it must be migrated to Refresh at implementation time.
- The reconnect defects (#2/#3) are reproduced against the current 018 implementation; their precise root causes (e.g., connection-scoped sink lifecycle races, stale per-turn state) are to be confirmed during plan/research rather than assumed here.
- These changes target the existing `projects/game/{agent (TypeScript), desktop (Go + Svelte), game.proto}` tree; no new project or external dependency is introduced.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Repository-Internal References

- `specs/018-saolei-mcp/spec.md` — the feature being optimized (saolei MCP tools, OperationBridge, profile/skill/adapter wiring).
- `specs/018-saolei-mcp/plan.md` — implementation plan and architecture for 018 (adapter factory, MCP host, status signals).
- `projects/game/game.proto` — `AgentFrame` oneof (`content`/`wait`/`warn`/`status`), `StatusSignal`/`StatusSignalStatus`, `ToolResultPart`, `RefreshAgentRequest`.
- `projects/game/agent/src/handler.ts` — `Connect` streaming handler: status-probe response (the basis of the ping-pong), turn mutex, bridge-sink registration/cleanup.
- `projects/game/agent/src/session-agent.ts` — `SessionAgent.getOrCreateAdapter` (the per-turn profile-switch auto-rebuild to be removed), `invalidateAdapter`, `getAdapterState`.
- `projects/game/agent/src/operation-bridge.ts` — `registerSink`/`unregisterSink`/`dispatch` (the dispatch path whose reconnect reliability is being fixed).
- `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` — the `saolei_update` handler (agent-internal tool whose result is to be surfaced).
- `projects/game/agent/src/llm.ts` — `AdapterFactory` and `AgentAdapterImpl.create` (adapter build path kept; per-turn switch removed).
- `projects/game/desktop/app.go` — desktop `recvLoop`/`handleInboundOperation` (operation execution + result mirror) and connect-time status probe.
- `projects/game/desktop/frontend/src/App.svelte` — `processing` typing-indicator state, `handleSelectSession`/`resetPlayPageState` (reconnect state), `handleAgentFrame` (status frame currently ignored).
- `projects/game/desktop/frontend/src/components/ChatView.svelte` — `toolResult` rendering (the display surface reused for agent-internal results).
- `projects/game/desktop/frontend/src/api.ts` — `Part`/`ToolResultPart`/`partKind` frontend model.
