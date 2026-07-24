# Feature Specification: Desktop Conversation Debug Mode

**Feature Branch**: `022-desktop-debug-mode`

**Created**: 2026-07-24

**Status**: Draft

**Input**: User description: "为 desktop session 对话页面新增 debug 模式，此模式下 1) 输出 debug 日志 2）tool result 返回需要用户手动确认再返回。debug 模式切换通过一个 ui 上一个 switch 进行，手动确认则是在 tool 的对话气泡中增加一个"确认"按钮，点击后继续。按钮点击后消失，手动确认仅用作调试，行为和结果无需记录状态，不影响流程。"

## Motivation

The desktop conversation page (`projects/game/desktop/frontend/src/App.svelte` chat layout, rendered via `projects/game/desktop/frontend/src/components/ChatView.svelte`) currently executes each tool operation the agent requests and immediately returns the result to the agent, with no way for a developer to stop and inspect a tool result at the instant before it goes back to the model. The log panel (`projects/game/desktop/frontend/src/components/LogPanel.svelte`, fed by `projects/game/desktop/frontend/src/logger.ts`) is always visible but emits every entry unconditionally and has no verbose "debug" level.

This feature adds a developer-facing **debug mode** to the conversation page, toggled by a single UI switch, that (1) surfaces debug-level log output and (2) **holds each tool result on the desktop side** — after the desktop has executed the tool and computed the result, but **before returning it to the agent** — until the developer manually confirms via a "Confirm" button on the tool-result bubble. During the hold the agent is merely in its normal "waiting for a tool result" state; the pause is a desktop-side timing intervention and is **transparent**: once confirmed, the returned result and the agent's subsequent behavior are identical to a run with no pause. It is explicitly a diagnostic aid — no confirmation state is persisted, and the pause does not alter what a tool returns or what the agent decides.

## Relationship

- Builds on the conversation page and tool-execution path established by [015 — Desktop Agent Interaction Refinement](../015-desktop-agent-refinement/spec.md) and the SSE chat delivery channel of [016 — Desktop SSE Chat Message Delivery](../016-desktop-sse-chat-push/spec.md).
- The exact desktop hook point is `projects/game/desktop/app.go` `handleInboundOperation` (lines 611–651): today it computes the result (`executeAgentOperation`, line 612), sends it to the agent over the WebSocket (`ws.SendFrame`, line 638), then mirrors it to the frontend chat stream (`chatStreams.Append`, line 649). In debug mode this feature reorders that sequence to: compute → mirror to frontend as "pending confirmation" → wait for the user's Confirm (max 15 min, else auto-continue) → send to agent.
- This feature also touches the **agent service**: the tool-result dispatch timeout `DISPATCH_TIMEOUT_MS` (`projects/game/agent/src/operation-bridge.ts:35`, currently 5 s) is raised to **20 minutes** as a safety-net backstop, so the desktop's 15-minute auto-continue always fires before the agent would time out. This is a one-constant change; it does not alter the agent's execution logic.
- The agent-side correlation that this pause rides on is `projects/game/agent/src/operation-bridge.ts` `dispatch()`/`handleResult()`, keyed by the `tool_id` UUID echoed in the `ToolResultPart`.

## Clarifications

### Session 2026-07-24

- **Q1 (resolved)** — What does "tool result 返回需要用户手动确认再返回" mean, and does it involve the agent? → The pause happens on the **desktop side**, **after the desktop executes the tool and computes the result, but before returning that result to the agent**. The agent is merely waiting for the result (its normal state); the pause operation does **not** involve the agent's execution logic. "不影响流程" means the pause is transparent — pausing then confirming yields the same tool result, agent decisions, and persisted state as not pausing; the pause only inserts a wait. Encoded in FR-006–FR-012. (The earlier draft's "frontend cosmetic / frontend pacing" interpretations are rejected.)
- **Q2 (resolved)** — The agent-side `dispatch()` had a 5 s result-wait timeout (`projects/game/agent/src/operation-bridge.ts:35`), which would break transparency for any confirmation slower than 5 s. → Decision: the **agent service raises the dispatch timeout to 20 minutes as a backstop**, and the **desktop caps the manual-confirmation wait at 15 minutes, auto-continuing (auto-releasing the result to the agent) if the user does not confirm in time**. Because 20 min > 15 min, the desktop auto-continue always fires before the agent backstop, so under normal debug usage the agent never times out. Encoded in FR-013 (desktop 15-min auto-continue) and FR-014 (agent 20-min backstop).
- Q: When debug mode is ON, where should DEBUG-level log entries originate? → A: **Both** the frontend (`projects/game/desktop/frontend/src/logger.ts`) and the desktop Go backend (`projects/game/desktop/internal/applog/logger.go`, forwarded to the panel via the existing `game:log` channel). The log panel is the unified view, and the highest-value debug signal spans both layers (inbound frames on the frontend, tool execution/results on the Go backend). Encoded in FR-004.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Debug Mode Toggle Surfaces Verbose Logs (Priority: P1)

A developer investigating a conversation wants more visibility than the default log panel offers. They flip a clearly labeled "Debug" switch in the conversation page toolbar; from that moment the log panel begins showing debug-level entries (e.g., inbound frames, tool-result details) that are otherwise suppressed. Flipping the switch back off stops further debug output. The switch is the single, discoverable entry point to the diagnostic surface.

**Why this priority**: Verbose logging is the foundational capability of "debug mode" and is independently valuable even without the tool-result hold. It is the cheapest slice to deliver and the one a developer reaches for first when something looks wrong.

**Independent Test**: Open a session, toggle Debug on, trigger or observe an agent turn, and confirm debug-level entries appear in the log panel that were absent before the toggle. Toggle Debug off and confirm no new debug-level entries appear.

**Acceptance Scenarios**:

1. **Given** the conversation page with debug mode OFF, **When** an agent turn produces frames and tool results, **Then** the log panel shows only the default (non-debug) log entries — no debug-level entries appear.
2. **Given** debug mode is OFF, **When** the user toggles the Debug switch ON, **Then** the switch visibly reflects the ON state and the log panel begins receiving debug-level entries for subsequent activity.
3. **Given** debug mode is ON, **When** the user toggles the Debug switch OFF, **Then** the switch reflects the OFF state and no further debug-level entries are produced (entries already shown remain until manually cleared).
4. **Given** debug mode is ON, **When** the user leaves and re-enters the conversation page (or the session), **Then** debug mode resets to OFF by default — the toggle is not persisted.

---

### User Story 2 - Tool Results Are Held for Manual Confirmation Before Returning to the Agent (Priority: P2)

With debug mode ON, a developer wants to inspect each tool result at the moment the desktop has produced it, before it goes back to the model. After the desktop executes a tool and computes the result, the conversation shows that tool-result bubble with a "Confirm" button and **holds the result** — it is not yet sent to the agent. The developer inspects the result (status, message, screenshot) and clicks Confirm; only then does the desktop return the result to the agent, the agent resumes from its wait, and the Confirm button disappears. If the developer does not confirm within 15 minutes, the desktop auto-continues (returns the result and dismisses the button) so neither the turn nor the agent hangs. The agent experiences nothing more than a longer-than-usual wait for the result; the result content and everything that follows are identical to a run with no pause.

**Why this priority**: This is the distinctive behavior of debug mode and the reason a developer reaches for the toggle beyond mere logging. It depends on debug mode existing (US1) and on the desktop result-return path, so it follows logging in priority.

**Independent Test**: Toggle Debug ON, run an agent turn that performs at least one tool operation (e.g., a mouse click), and confirm: (a) the tool-result bubble appears with a "Confirm" button and the agent does **not** proceed (no subsequent model output) until the button is clicked; (b) after clicking Confirm the button disappears and the agent resumes; (c) the final conversation is identical to the same turn run with Debug OFF. Toggle Debug OFF and confirm tool results return immediately with no Confirm button and no hold.

**Acceptance Scenarios**:

1. **Given** debug mode is ON and the desktop has executed a tool and computed its result, **When** the result would normally be returned to the agent, **Then** the desktop instead holds it and surfaces the tool-result bubble with a "Confirm" control, and the agent remains waiting (does not advance to the next step).
2. **Given** a tool result is being held pending confirmation, **When** the user clicks its "Confirm" control, **Then** the desktop returns that result to the agent, the agent resumes, and the "Confirm" control is removed from that bubble.
3. **Given** debug mode is OFF, **When** the desktop executes a tool, **Then** the result is returned to the agent immediately with no Confirm control shown and no hold — identical to today.
4. **Given** a tool result is held pending confirmation and the user does not click Confirm, **When** 15 minutes elapse, **Then** the desktop automatically returns the result to the agent and dismisses the Confirm control (auto-continue), so the turn and the agent are never left waiting indefinitely.
5. **Given** a turn is run once with debug mode ON (with confirmations) and once with debug mode OFF, **When** both turns complete, **Then** the tool results, agent decisions, and persisted conversation state are identical between the two — the pause was transparent.

---

### Edge Cases

- **Slow confirmation (within 15 min)**: the developer takes several minutes to inspect a result before confirming. The desktop holds the result and the agent waits throughout; on confirm the result returns normally and transparency holds. The agent's 20-minute backstop never fires because 15 min < 20 min.
- **No confirmation within 15 min**: the desktop auto-continues — returns the held result to the agent and dismisses the Confirm control. The turn proceeds as if confirmed; transparency holds (the same result is returned, just later).
- **Multiple tool results produced in one inbound frame**: if a single inbound frame contains several tool-request parts, each is executed and held independently; the developer confirms each in turn (or each auto-continues after its own 15 min). They are not collapsed into one confirmation.
- **Display-only tool results (agent-pushed)**: some `ToolResultPart` frames are pushed by the agent purely for display (the agent-side `pushResult` path) and are not results the desktop is about to return. These MUST NOT receive a Confirm control — only tool results the desktop is actively holding before return get the Confirm control.
- **Debug toggled OFF mid-hold**: if debug mode is toggled OFF while a tool result is held pending confirmation, the held result is returned to the agent immediately (the hold is released) so the turn is not stuck; the Confirm control is dismissed.
- **Leaving the session with a held result**: leaving the session must not leave the agent waiting; any held result is released (returned or the turn is torn down per the existing disconnect behavior in feature 017). The 15-minute auto-continue and 20-minute backstop ensure no turn hangs even if the desktop process itself stalls.
- **History replay while debug mode is ON**: historical tool results replayed on session entry are already-final (not being held for return), so they do NOT receive a Confirm control — only live tool results the desktop is about to return are held.
- **Failed tool results**: a tool result with FAILED status is held for confirmation just like a successful one, so the developer can inspect failures before they reach the model.
- **Non-debug tool-result latency**: raising the agent dispatch timeout to 20 minutes globally means a genuinely hung desktop (in any mode) is now detected after up to 20 minutes instead of 5 seconds. This is the accepted tradeoff of the backstop; in normal (non-debug) operation results return within milliseconds so the longer timeout is dormant.

## Requirements *(mandatory)*

### Functional Requirements

**Debug Mode Toggle**

- **FR-001**: The conversation page MUST provide a single, clearly labeled switch control in the conversation toolbar that toggles debug mode between ON and OFF.
- **FR-002**: Debug mode MUST default to OFF and MUST NOT be persisted: leaving and re-entering the conversation page (or the session) resets it to OFF. The toggle holds no durable state.

**Debug Log Output**

- **FR-003**: When debug mode is OFF, debug-level log entries MUST NOT be emitted or displayed in the log panel; the panel shows only the entries it shows today.
- **FR-004**: When debug mode is ON, the system MUST output debug-level log entries to the existing log panel from **both** the frontend (`projects/game/desktop/frontend/src/logger.ts`) and the desktop Go backend (`projects/game/desktop/internal/applog/logger.go`, forwarded via the existing `game:log` channel) — including at minimum diagnostic detail about inbound conversation frames (frontend) and tool execution/results (Go backend) as they are processed.
- **FR-005**: Toggling debug mode OFF MUST stop further debug-level log output. Entries already displayed before the toggle MAY remain visible until the user manually clears the log panel (existing "Clear Logs" affordance).

**Tool Result Hold-for-Confirmation**

- **FR-006**: When debug mode is ON, after the desktop executes a tool and computes its result, the desktop MUST hold that result and MUST NOT return it to the agent until the user manually confirms (or the 15-minute auto-continue fires). During the hold the agent remains in its normal waiting-for-result state; the agent's execution logic is not modified.
- **FR-007**: The hold MUST be transparent: once the user confirms (or the desktop auto-continues), the result returned to the agent and everything that follows (agent decisions, persisted conversation state) MUST be identical to a run performed with no hold.
- **FR-008**: While a tool result is held, the conversation view MUST surface that tool-result bubble with a "Confirm" control so the user can inspect the result (status, message, screenshot) and trigger its return.
- **FR-009**: Clicking the "Confirm" control on a held tool result MUST (a) cause the desktop to return that result to the agent, and (b) remove the "Confirm" control from that bubble.
- **FR-010**: The hold and confirmation MUST be debug-only and stateless: the confirmation MUST NOT be persisted, MUST NOT alter the tool result content, and MUST NOT change the agent's decisions or the persisted conversation — only the timing of the result's return.
- **FR-011**: When debug mode is OFF, the desktop MUST return each tool result to the agent immediately upon computation, exactly as it does today — no "Confirm" control is shown and nothing is held.
- **FR-012**: Only tool results the desktop is actively holding before return MUST show the "Confirm" control. Display-only tool results pushed by the agent, and historical tool results replayed on session entry, MUST NOT show a "Confirm" control.

**Hold and Timeout Bounds**

- **FR-013**: The desktop MUST cap the manual-confirmation wait at **15 minutes** per held tool result. If the user does not confirm within 15 minutes, the desktop MUST automatically release the held result to the agent (auto-continue) and dismiss the "Confirm" control, so the turn is never left waiting indefinitely.
- **FR-014**: The agent service MUST raise the tool-result dispatch timeout (`DISPATCH_TIMEOUT_MS`, `projects/game/agent/src/operation-bridge.ts:35`) from 5 seconds to **20 minutes** as a safety-net backstop, so the desktop's 15-minute auto-continue always fires before the agent would time out. This change is global (the timeout is not mode-aware); in normal non-debug operation results return within milliseconds, so the longer timeout remains dormant.

**Scope Boundary**

- **FR-015**: Debug mode MUST affect ONLY: (a) the desktop app's debug-level logging — frontend (`logger.ts`) and Go backend (`internal/applog/logger.go`) DEBUG entries gated by debug mode (FR-004), flowing to the panel over the **unchanged** `game:log` forwarding mechanism; (b) the desktop-side tool-result hold with its 15-minute auto-continue (FR-006–FR-013); and (c) the agent-side dispatch-timeout constant (raised to 20 min per FR-014). All other pages (session list, agent profiles), the SSE chat delivery channel, the `game:log` forwarding mechanism itself, and the agent service's execution logic MUST remain fully functional and unchanged.

### Key Entities *(include if feature involves data)*

- **Debug Mode (transient)**: A non-persisted, page-scoped boolean state owned by the conversation page, toggled by a single UI switch. When ON it enables debug-level log output and the tool-result hold-for-confirmation; when OFF the conversation page and the desktop result-return path behave exactly as today. It resets to OFF on page/session exit and is never stored.
- **Held Tool Result (transient)**: A tool result the desktop has computed but is deliberately not yet returning to the agent while debug mode is ON. It exists only in memory on the desktop, is surfaced in the conversation as a bubble with a "Confirm" control, and is released (returned to the agent) on user confirmation, on the 15-minute auto-continue, or when debug mode is turned off / the session is left. It carries no durable representation and is not transmitted until released.
- **Confirm Control (transient)**: The per-held-tool-result UI affordance ("Confirm" button) on a tool-result bubble. Present only while that result is held in debug mode; removed once the result is returned to the agent (by click, auto-continue, or hold release).
- **Dispatch Timeout Backstop (config)**: The agent-side maximum wait for a tool result, raised to 20 minutes. A non-mode-aware safety threshold ensuring the desktop's 15-minute auto-continue always precedes any agent-side timeout.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With debug mode OFF, the log panel shows zero debug-level entries during an agent turn; with debug mode ON, debug-level entries appear for 100% of subsequent frames/tool results until the mode is toggled off.
- **SC-002**: The debug toggle is discoverable from the conversation toolbar in a single action, and 100% of testers can enable debug mode without instructions.
- **SC-003**: With debug mode ON, for 100% of tool results the desktop computes, the result is held (the agent does not advance) and a "Confirm" control appears; clicking it returns the result to the agent and removes the control, in every case.
- **SC-004**: With debug mode ON, if the user does not confirm a held result within 15 minutes, the desktop auto-continues (returns the result, dismisses the control) in 100% of cases, and the agent's 20-minute backstop never fires during normal debug usage.
- **SC-005**: A turn run with debug mode ON (with confirmations) and the same turn run with debug mode OFF produce identical tool results, agent decisions, and persisted conversation state.
- **SC-006**: Session list, profile management, the SSE chat channel, the `game:log` log-forwarding mechanism, and the agent service's execution logic remain 100% functional and unchanged; the only agent-side change is the dispatch-timeout constant value.

## Assumptions

- Debug mode is a **developer-facing diagnostic**. Its hold is implemented at the desktop's result-return boundary in `projects/game/desktop/app.go` `handleInboundOperation` (between result computation at line 612 and the `ws.SendFrame` return at line 638). In debug mode the result is mirrored to the frontend (rendered with a "Confirm" control) and the return is deferred until confirmation or the 15-minute auto-continue. The exact signaling between the frontend "Confirm" click and the Go backend, the 15-minute timer mechanism, and how the held result is marked as pending in the chat stream, are plan-phase implementation concerns constrained by FR-006–FR-013.
- The **agent service change is limited to raising `DISPATCH_TIMEOUT_MS`** (`projects/game/agent/src/operation-bridge.ts:35`) from 5 s to 20 min. It does not touch the agent's execution logic, correlation (`tool_id`), or cancellation contract. Because 20 min > 15 min, the desktop auto-continue always fires first under debug usage.
- Raising the dispatch timeout globally means a genuinely hung desktop (in any mode) is detected after up to 20 min rather than 5 s; this is the accepted tradeoff of the backstop and is documented in Edge Cases. In normal non-debug operation, results return within milliseconds so the longer timeout is dormant.
- "输出 debug 日志" is interpreted as: debug mode gates the emission/display of a verbose **DEBUG log level** in the existing log panel (`projects/game/desktop/frontend/src/components/LogPanel.svelte`), sourced from **both** the frontend logger (`projects/game/desktop/frontend/src/logger.ts`) and the desktop Go backend logger (`projects/game/desktop/internal/applog/logger.go`, forwarded via the existing `game:log` channel — the unified panel already aggregates both). Neither layer has a debug-level concept today; debug mode introduces and gates DEBUG-level entries in both. What exactly constitutes a DEBUG entry in each layer is a plan-phase detail, constrained by FR-004.
- The debug switch lives in the conversation toolbar (`projects/game/desktop/frontend/src/App.svelte` chat-top-bar, alongside the session label / window selector / capture button) — the natural, discoverable location for a per-conversation diagnostic control.
- Debug mode defaults to OFF and is not persisted; it resets on leaving/re-entering the conversation page or session (per FR-002), matching "仅用作调试，无需记录状态".
- The agent-tool correlation rides the existing `tool_id` UUID echoed in the `ToolResultPart` (`projects/game/agent/src/operation-bridge.ts`); this feature does not change that correlation, only the timing of when the desktop sends the result frame.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Repository-Internal

- `projects/game/desktop/app.go` — `handleInboundOperation` (lines 611–651) the exact result-return boundary where the hold hooks in; `recvLoop` (lines 546–603) the inbound tool-operation loop; `executeAgentOperation` (line 612+) the tool executor.
- `projects/game/desktop/frontend/src/App.svelte` — conversation page layout and the chat-top-bar toolbar where the debug switch will live.
- `projects/game/desktop/frontend/src/components/ChatView.svelte` — message thread; tool-result rendering branch (lines 197–221) where the "Confirm" control will be added for held results.
- `projects/game/desktop/frontend/src/components/LogPanel.svelte` and `projects/game/desktop/frontend/src/logger.ts` — existing log panel and frontend logging utility to be extended with a DEBUG level gated by debug mode.
- `projects/game/desktop/internal/applog/logger.go` — desktop Go backend logger (forwarded to the panel via the existing `game:log` channel) to be extended with a DEBUG level gated by debug mode (FR-004).
- `projects/game/desktop/frontend/src/api.ts` — `Part` / `ToolResultPart` / `partKind()` (lines 108–144) describing tool-result data.
- `projects/game/agent/src/operation-bridge.ts` — agent-side `dispatch()`/`handleResult()` and the `DISPATCH_TIMEOUT_MS` constant (line 35) to be raised from 5 s to 20 min per FR-014.
- [015 — Desktop Agent Interaction Refinement](../015-desktop-agent-refinement/spec.md), [016 — Desktop SSE Chat Message Delivery](../016-desktop-sse-chat-push/spec.md), [017 — Agent Loop Graceful Abort](../017-agent-loop-graceful-abort/spec.md) — the conversation-page, chat-delivery, and disconnect-handling foundation this feature builds on.
- `.specify/memory/constitution.md` — Citation Provenance (§I) and Interface-First Design (§III) govern this specification.

### External

- No external specifications are authoritative for this feature. Framework/library documentation (Svelte, Wails) will be consulted in `plan.md` per Constitution §III. The LangChain cancellation/timeout contract referenced in [017](../017-agent-loop-graceful-abort/spec.md) remains background for the dispatch-timeout change.
