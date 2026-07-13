# Feature Specification: Agent Loop Graceful Abort on Desktop Disconnect

**Feature Branch**: `017-agent-loop-graceful-abort`

**Created**: 2026-07-13

**Status**: Draft

**Input**: User description: "我希望对 @projects/game/agent/ 服务中，关于 agent loop 关于断开 ws 后的中止方式。移除现有的 ws 断开后的终止方法，改用官方文档中 https://docs.langchain.com/oss/javascript/langchain/overview 终止方式，在 ws 断开后优雅终止 agent loop。"

## User Scenarios & Testing *(mandatory)*

<!--
  The "user" of this feature is the desktop client that drives the agent via
  the bidi gRPC stream, plus the operator who runs the agent service.  The
  feature is a behavior-quality refactor of an existing internal path, so
  user stories focus on observable disconnect behavior, not greenfield UX.
-->

### User Story 1 - Clean Disconnect Mid-Turn (Priority: P1)

A desktop is actively running an agent turn (LLM is reasoning, or a tool call
has just been dispatched) when the desktop application is closed, the network
drops, or the user manually disconnects. The agent service MUST stop that
in-flight turn promptly, without continuing to spend LLM tokens on a stream
no one is listening to, and without producing error frames addressed to a
dead peer. When the desktop reconnects with the same session id, a fresh turn
can be started and the conversation continues from the last persisted
checkpoint.

**Why this priority**: this is the core scenario the feature exists for. The
current throw-based termination leaves LLM streams running until the next
tool boundary and surfaces disconnect as a processing error; the new behavior
must make disconnect a first-class, graceful event.

**Independent Test**: disconnect the desktop while an agent turn is in
progress (LLM mid-response) and observe (a) no further LLM tokens are
consumed for that turn, (b) no error/warn frames are emitted by the agent
service for the dead session, and (c) reconnecting the desktop with the same
session id allows a new turn that resumes from the prior conversation
history.

**Acceptance Scenarios**:

1. **Given** an agent turn is in flight with the LLM streaming a text
   response, **When** the desktop bidi stream disconnects, **Then** the
   agent service stops the in-flight turn within a short, bounded window
   and emits no further frames addressed to the disconnected session.
2. **Given** an agent turn is in flight and a tool operation has been
   dispatched to the desktop, **When** the desktop bidi stream disconnects
   before the tool result returns, **Then** the agent service stops the
   in-flight turn and does not wait for the tool result or surface a tool
   failure to the LLM.
3. **Given** a turn was aborted due to disconnect, **When** the desktop
   reconnects with the same session id and starts a new turn, **Then** the
   new turn begins from the last persisted conversation state (no
   duplicate or lost user message).

---

### User Story 2 - No Regression on Normal Turn Completion (Priority: P2)

When the desktop remains connected for the entire turn, the agent loop MUST
behave exactly as it does today: the turn runs to completion, all reasoning
and text blocks stream to the desktop, the final `wait` frame is sent, and
the per-session mutex is released. The graceful-abort path MUST only engage
on actual disconnect, never on healthy completion.

**Why this priority**: the refactor must not change the happy path. A
regression here would break every working session.

**Independent Test**: run a normal end-to-end turn with a stable desktop
connection and confirm the streamed output, the closing `wait` frame, and
the mutex-release behavior are byte-for-byte identical to today.

**Acceptance Scenarios**:

1. **Given** a stable desktop connection, **When** the user submits a turn
   that completes normally, **Then** all reasoning/text frames are
   delivered, the `wait` frame is sent, and the per-session mutex is
   released.
2. **Given** a stable desktop connection, **When** a tool call completes
   normally and the LLM continues the turn, **Then** tool results flow
   through the bridge exactly as today and the turn runs to completion.

---

### User Story 3 - Idiomatic Termination Mechanism (Priority: P3)

The disconnect-termination mechanism MUST follow the cancellation contract
documented by LangChain for its JavaScript agent runtime, rather than a
custom throw injected into the agent's tool-execution path. Removing the
custom mechanism and aligning with the official contract is itself a
deliverable, independent of the user-visible behavior above: it keeps the
service upgrade-safe against future LangChain releases and removes a
maintenance burden.

**Why this priority**: this is the stated refactor goal and the reason the
feature was requested, but its value is realized through Stories 1 and 2.

**Independent Test**: inspect the agent service source and confirm (a) the
custom disconnect-throw mechanism has been removed, and (b) disconnect now
flows through the official LangChain cancellation entry point documented at
[LangChain JS — Overview](https://docs.langchain.com/oss/javascript/langchain/overview).

**Acceptance Scenarios**:

1. **Given** the agent service source after the change, **When** a reviewer
   searches for the prior custom disconnect-throw hook (the
   tool-execution middleware that throws on missing desktop sink, and the
   `OperationBridge` throw-on-no-sink path), **Then** none of that custom
   termination logic remains.
2. **Given** the agent service source after the change, **When** a reviewer
   traces the disconnect path, **Then** it routes through the LangChain
   cancellation contract documented at
   [LangChain JS — Overview](https://docs.langchain.com/oss/javascript/langchain/overview).

---

### Edge Cases

- **Disconnect exactly at turn boundary**: the desktop disconnects in the
  instant between the final `wait` frame being written and the mutex being
  released. The mutex MUST still be released; no abort logic MUST fire for
  an already-completed turn.
- **Disconnect during adapter bind**: the desktop disconnects while
  `SessionAgent` is binding a new adapter (the bind lock is held). The
  disconnect MUST NOT corrupt the bind state; the in-progress bind is
  allowed to complete or fail on its own, and the next reconnect reuses or
  rebuilds the adapter as today.
- **Reconnect during aborted turn**: the desktop reconnects before the
  aborted turn's stream has fully torn down. The new connection MUST be
  able to register a fresh sink and start a new turn; the still-tearing-down
  aborted turn MUST NOT interfere with the new turn.
- **Desktop sends tool result after disconnect**: a tool result frame
  arrives for a turn that has already been aborted. The service MUST drop
  it safely without reviving the aborted turn or throwing.
- **Disconnect while no turn is in flight**: the desktop disconnects while
  the session is idle. The service MUST remain idle and MUST NOT perform
  any abort work (there is nothing to abort).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The agent service MUST stop any in-flight agent turn promptly
  when the desktop bidi stream disconnects (stream `end` or `error`), for
  every phase of the turn — LLM reasoning, text streaming, and tool
  dispatch.
- **FR-002**: The disconnect-termination mechanism MUST be the official
  LangChain JavaScript cancellation contract documented at
  [LangChain JS — Overview](https://docs.langchain.com/oss/javascript/langchain/overview),
  not a custom throw injected into the agent's tool-execution path.
- **FR-003**: The agent service MUST remove the prior custom
  disconnect-termination mechanism in full — specifically the
  tool-execution middleware that throws when no desktop sink is registered,
  and the throw-on-no-sink path in the operation bridge's dispatch flow.
- **FR-004**: On graceful abort, the agent service MUST NOT emit error or
  warning frames addressed to the disconnected desktop (the stream is gone;
  emitting would be wasted work and noisy logs).
- **FR-005**: On graceful abort, the agent service MUST release the
  per-session turn mutex so the session is ready to accept a new turn from
  a reconnecting desktop.
- **FR-006**: A gracefully aborted turn MUST leave the conversation
  checkpoint in a state from which a subsequent turn (after reconnect)
  resumes correctly — no partial LLM message, no orphaned tool message,
  no lost user input.
- **FR-007**: The normal (non-disconnect) turn path MUST be unchanged:
   the same frames are streamed in the same order, the same `wait` frame
   closes the turn, and the mutex is released identically.
- **FR-008**: The disconnect-termination mechanism MUST engage uniformly
   regardless of which phase of the turn the disconnect occurs in (LLM
   thinking, text streaming, or awaiting a tool result) — no phase may be
   left running after disconnect.
- **FR-009**: Disconnect while no turn is in flight MUST remain a no-op:
   no abort work, no frames emitted, no state change beyond unregistering
   the sink (as today).

### Key Entities *(include if feature involves data)*

- **Agent Turn**: a single conversational pass driven by a user message,
  spanning LLM reasoning, text streaming, and zero or more tool
  dispatches. The entity whose lifecycle this feature governs on
  disconnect.
- **Desktop Bidi Stream**: the bidirectional gRPC stream between the
  agent service and a desktop. Its `end`/`error` events are the trigger
  for graceful abort.
- **Conversation Checkpoint**: the persisted conversation state for a
  session (managed by the existing checkpointer). Must survive an aborted
  turn intact so reconnect resumes cleanly.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: When the desktop disconnects mid-turn, no further LLM tokens
  are billed for that turn within at most a few seconds of the disconnect
  being detected by the agent service.
- **SC-002**: After the change, zero error or warning frames are emitted
  by the agent service that are addressed to a desktop which has already
  disconnected (measurable by log/frame inspection across a disconnect
  test).
- **SC-003**: A desktop that disconnects mid-turn and reconnects with the
  same session id can start a new turn that resumes from the prior
  conversation history with 100% of pre-disconnect user messages and
  completed assistant messages preserved.
- **SC-004**: The normal-turn happy path exhibits no behavioral
  regression: streamed frames, closing `wait` frame, and mutex-release
  are identical to the pre-change service across a representative set of
  turns (text-only, image+text, and tool-using turns).
- **SC-005**: A reviewer can confirm, by source inspection alone, that
  the custom disconnect-throw mechanism is fully removed and the
  disconnect path routes exclusively through the official LangChain
  cancellation contract referenced in FR-002.

## Assumptions

- The desktop bidi stream already emits `end` and `error` events on
  disconnect (verified in the existing `Connect` handler in
  `projects/game/agent/src/handler.ts`); this feature consumes those
  events as the disconnect signal and does not change how disconnect is
  detected, only how the in-flight turn is stopped.
- "Graceful termination" means: stop the in-flight turn as soon as
  disconnect is detected, do not wait for any in-flight tool result, do
  not emit any frames to the dead peer, and release the per-session turn
  mutex. In-flight tool dispatches whose results will never arrive are
  cancelled rather than allowed to time out.
- The LangChain JavaScript runtime referenced by
  [LangChain JS — Overview](https://docs.langchain.com/oss/javascript/langchain/overview)
  exposes a cancellation contract suitable for aborting an in-flight
  `streamEvents` agent run; the exact API surface (option name, signal
  type) is an implementation concern for `plan.md` and will be researched
  there per Constitution §III.
- The existing per-session `SessionAgent` survives stream reconnects and
  is shared across reconnects of the same session id (verified in the
  existing `OperationBridge` documentation); this feature preserves that
  property and does not change session/agent lifetime.
- The existing checkpointer continues to govern what conversation state
  persists across an aborted turn; this feature does not change
  checkpoint semantics, only when a turn stops writing to it.
- Disconnect detection latency (time from physical network drop to the
  gRPC stream emitting `end`/`error`) is governed by the existing gRPC
  transport configuration and is out of scope for this feature; the
  feature only guarantees prompt termination *after* disconnect is
  detected.
- Removing the custom `DesktopDisconnectedError` throw path does not
  remove the `DesktopDisconnectedError` type itself if other callers
  still depend on it; the exact removal scope is settled in `plan.md`.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [LangChain JS — Overview](https://docs.langchain.com/oss/javascript/langchain/overview) — the official LangChain JavaScript documentation explicitly named in the feature request as the source of the new termination contract. Reflects LangChain.js v1.x.
- [Streaming (LangChain.js)](https://docs.langchain.com/oss/javascript/langchain/streaming) — documents `stream()` / `streamEvents()` and the `signal` cancellation option in the runnable config; the contract this feature adopts.
- [Event Streaming v3 (LangChain.js)](https://docs.langchain.com/oss/javascript/langchain/event-streaming) — the typed event projection used by the existing `streamEvents(v3)` invocation this feature modifies.
- [Frontend Join & Rejoin: stop vs disconnect](https://docs.langchain.com/oss/javascript/langchain/frontend/join-rejoin) — official guidance distinguishing cancellation (`stop`) from mere disconnection, informing the graceful-termination intent.
- [LangGraph Interrupts](https://docs.langchain.com/oss/javascript/langgraph/interrupts) — referenced to distinguish HITL `interrupt()` (pause/resume) from `AbortSignal` cancellation (terminate); the feature uses cancellation, not interrupt.
- [ModelAbortError API reference](https://reference.langchain.com/javascript/langchain-core/errors/ModelAbortError) — the framework's typed error for aborted invocations, carrying partial output. `@langchain/core` ≥ 1.2.x.

### Repositories

- [langchain-ai/langchainjs#9900 — full AbortSignal handling across providers](https://github.com/langchain-ai/langchainjs/pull/9900) — merged 2026-01-30; established uniform signal propagation and `ModelAbortError` across all chat-model providers (`@langchain/core` minor bump; provider packages patched).
- [langchain-ai/langchainjs#8671 — AbortSignal in react-agent params](https://github.com/langchain-ai/langchainjs/pull/8671) — merged 2025-08-16; established signal propagation through agent tool execution.

### In-Repository Sources

- `projects/game/agent/src/llm.ts` — current `toolAbortOnDisconnect` middleware and `streamEvents(v3)` invocation (no `signal` passed today); the loop-cancellation mechanism to be replaced.
- `projects/game/agent/src/handler.ts` — `Connect` bidi-stream handler; stream `end`/`error` events that drive disconnect detection.
- `projects/game/agent/src/operation-bridge.ts` — `OperationBridge` sink lifecycle and `DesktopDisconnectedError`; the throw-on-no-sink dispatch path referenced in FR-003.

### Articles & RFCs

- No external articles or RFCs cited. (The WHATWG `AbortController` / `AbortSignal` standard underlies the framework primitive; `plan.md` may add the DOM spec link if deeper grounding is warranted.)
