# Quickstart Validation: Agent Checkpoint & Session UI Redesign

## Prerequisites

- Repository dependencies are available through Bazel.
- Gateway, proxy, agent, session, and prompt services can be deployed through the existing game testplan.
- A test agent profile exists or can be created through the desktop/profile API.

## Targeted Build and Test Commands

Run targeted tests first during implementation:

```bash
bazel test //projects/game/agent/...
bazel test //projects/game/desktop/...
bazel test //projects/game/gateway/...
bazel test //projects/game/proxy/...
```

After proto or Go API changes, synchronize generated BUILD metadata:

```bash
bazel run //:gazelle projects/game
```

Final repository verification:

```bash
bazel build //...
bazel test //...
```

## Large-Test Validation

Use the repository `testplan` skill to validate the system through deployable surfaces. The feature should extend the existing game system testplan with cases covering:

1. Create session and profile.
2. Create agent with profile.
3. Enter play and send multiple messages.
4. Leave and re-enter play.
5. List messages and verify prior messages appear in chronological order, each carrying its native `message_id`.
6. Send a follow-up message and verify agent context resumes.
7. Delete agent.
8. Recreate agent for the same session.
9. Verify the new agent's messages are empty and old context does not leak (each new message carries its own native `message_id`, none inherited from the deleted agent).

## Manual Desktop QA

### Scenario 1: Session detail state flow

1. Open desktop client.
2. Create or select a session with no agent.
3. Verify session detail shows profile selector + Create Agent only.
4. Create agent.
5. Verify session detail switches to agent summary + Enter Play.
6. Verify no Connect Agent button exists and no WebSocket connection is opened while staying on detail.

### Scenario 2: Play entry, history, and auto-connect

1. Click Enter Play from an agent-ready session.
2. Verify play page shows connecting state, then chat ready state.
3. Verify sidebar shows agent name, profile name, model, connection status, and View Profile.
4. Send a message and receive agent response.
5. Leave play and return.
6. Verify messages load before new input.
7. Send a follow-up referring to earlier content and verify the agent responds with prior context.

### Scenario 3: Delete and recreate

1. In a session with history, delete the agent.
2. Recreate an agent for the same session.
3. Enter play.
4. Verify history is empty.
5. Send a message asking about prior content and verify the agent has no old context.

## Expected Outcomes

- Session detail never shows profile selector and agent summary together.
- Manual connection controls are absent.
- Connection happens automatically on play entry or send fallback.
- `ListMessages` returns prior messages within 2 seconds for normal test histories.
- Profile model configured at agent creation is the model used during turns.
- Delete/recreate never leaks prior history.
