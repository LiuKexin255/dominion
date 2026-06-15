# Contract: Agent Service Compatibility

## Scope

This contract defines the public behavior that must remain unchanged while the agent service replaces its deepagent foundation with service-owned LangChain execution.

## Public API Invariants

No proto, HTTP route, gateway/proxy mapping, desktop binding, or frontend API change is planned.

### Create Agent

- Existing surface: create one agent under a session from an agent profile.
- Required invariant: response still contains the same `Agent` resource fields, including `name`, `session_id`, `agent_profile_name`, and `create_time`.
- Required invariant: the created agent copies profile behavior at creation time.
- Required invariant: missing or unknown profile errors remain compatible with current behavior.

### Get Agent

- Existing surface: retrieve the active session agent.
- Required invariant: existing agents return the same `Agent` shape.
- Required invariant: missing agents return not-found behavior compatible with current clients.

### Delete Agent

- Existing surface: delete the active session agent.
- Required invariant: deletion clears active metadata and conversation continuity data.
- Required invariant: delete/recreate for the same session starts with no prior conversation influence.
- Required invariant: idempotent missing-agent behavior remains compatible with current implementation.

### Connect / ConnectAgent Stream

- Existing surface: bidirectional stream exchanging `AgentFrame` units.
- Required invariant: user text frames produce agent `thinking` and/or `text` frames followed by a system `wait` frame.
- Required invariant: provider or model errors produce compatible `warn` behavior and keep the stream usable according to existing handler expectations.
- Required invariant: status probes, echo/deprecated payload handling, frame sequencing, `invoke_id`, `frame_id`, and sender semantics remain compatible.
- Required invariant: rapid same-session messages are processed in FIFO order without concurrent model execution for that session.

### List Messages

- Existing surface: list public `Message` resources for `sessions/{session_id}/agent`.
- Required invariant: messages are returned in chronological order for normal conversation sizes.
- Required invariant: public message names and `message_id` behavior remain compatible with existing clients.
- Required invariant: visible user/agent text is preserved; `wait` frames and transient stream controls are not returned as messages.
- Required invariant: missing agent behavior remains compatible with current clients.

## Compatibility Baseline

The migration must preserve:

- Public APIs and resource shapes.
- Desktop/operator create, enter-play, chat, leave, return, history, resume, delete, recreate flows.
- Message history and resume semantics.
- Profile model selection and profile snapshot behavior.
- Current configured profile tool/skill behavior when observable through existing public surfaces.

The migration does not need to preserve:

- Deepagent-only planning abstractions.
- Deepagent virtual filesystem internals.
- Deepagent subagent/task internals.
- Any hidden default that is not observable through public service, desktop, or operator workflows.

## Acceptance Mapping

| Invariant | Primary validation |
|-----------|--------------------|
| Agent lifecycle unchanged | TypeScript `handler.test.ts`; large-test `agent_dialog_test.go` |
| Stream frames unchanged | TypeScript `handler.test.ts`; large-test `agent_dialog_test.go` |
| Resume and history unchanged | TypeScript adapter/handler tests; large-test `agent_checkpoint_test.go` |
| Delete/recreate isolation | TypeScript handler tests; large-test `agent_checkpoint_test.go` |
| Model/profile behavior | TypeScript adapter tests; large-test `agent_checkpoint_test.go` |
| No public proto/API change | Source diff review of `projects/game/game.proto`, gateway, proxy, desktop API files |
