# Quickstart: Queued Input Mid-Turn Injection & Bubble Continuity

**Feature**: `specs/038-queue-input-mid-turn` | **Date**: 2026-08-06

This guide documents the validation scenarios that prove the feature works end-to-end. Implementation details belong in `tasks.md`.

## Prerequisites

- The agent service and desktop app build successfully (`bazel build //...`).
- A saolei session with a Team created (player + planner agents).
- The desktop chat view connected to the session (SSE stream open).

## Scenario 1: Mid-Turn Injection at Tool-Result Boundary (FR-001, US1)

**Validates**: A message queued while the agent is executing a tool is delivered to the agent's next reasoning step, not delayed until the full turn completes.

**Steps**:
1. Send a user message that triggers the player to make a tool call (e.g., "开始游戏" / start a new game).
2. While the player is executing the tool (the tool-bubble shows "running…"), submit a second user message (e.g., "小心地雷").
3. Observe the chat: the second message appears as pending (queued indicator shows "1 message queued").
4. Wait for the tool result to return.

**Expected outcome**:
- The pending indicator decrements to 0 **immediately** when the tool result is processed (before the full turn completes).
- The player's next reasoning step addresses the second message — the agent acts on "小心地雷" in the same turn, without waiting for the `wait` signal.
- The QueueSignal depth sequence is: submit→1, drain→0 (mid-turn), then eventually `wait`.

**How to verify via logs/traces**: filter for `QueueSignal` frames in the agent log; the depth should go 1→0 mid-turn (before `wait`), not 1→0 at turn-end.

## Scenario 2: Agent Bubble Continuity (FR-005/FR-006, US2)

**Validates**: Submitting a user message during the agent's streaming thinking/text output does NOT split the agent's bubble.

**Steps**:
1. Trigger a player turn that produces streaming thinking output.
2. While the thinking bubble is actively streaming (content growing), submit a queued user message.
3. Continue observing the thinking output.

**Expected outcome**:
- The thinking content remains ONE continuous bubble — subsequent thinking chunks merge into the same bubble that started before the user message.
- The user's queued message appears below the thinking bubble as a pending item.
- The visual order is: `[thinking bubble — continuous]` then `[user message — pending]` below it.

**How to verify**: count the thinking bubbles in the chat thread. There must be exactly ONE thinking bubble for the continuous thinking stream, regardless of the interleaved user message.

## Scenario 3: Fallback — No Tool Calls (FR-004, US3)

**Validates**: When the agent's turn completes without calling any tools, queued messages are delivered at the turn-completion boundary (existing spec 030 behavior).

**Steps**:
1. Send a user message that the player answers with pure reasoning (no tool call).
2. While the player is streaming its response, submit a second user message.
3. Wait for the turn to complete (`wait` signal).

**Expected outcome**:
- The second message stays pending throughout the turn (no mid-turn drain — there is no tool-result boundary).
- On turn completion, a new turn starts automatically using the queued message as input (existing spec 030 FR-006 behavior).
- The QueueSignal depth sequence is: submit→1, turn-end drain→0, then the next turn starts.

## Scenario 4: Abort Discards Remaining Queue (FR-009)

**Validates**: An explicit abort discards unconsumed queued messages.

**Steps**:
1. Start a player turn with tool calls.
2. Queue two messages during the turn.
3. Before both are consumed, trigger an abort (stop control).

**Expected outcome**:
- Remaining unconsumed queued messages are discarded (QueueSignal depth → 0 via abort path).
- No auto-continued turn starts from the discarded messages.
- Messages already injected (consumed by a previous `drainQueue` call) are not rolled back.

## Scenario 5: Unit Test — `drainQueue()` and Merge Logic

**Validates**: The `TurnLoop.drainQueue()` method and the frontend merge logic work correctly in isolation.

**Agent unit test** (`turn-loop.test.ts`):
- Construct a TurnLoop with a recording emit sink and a fake runner.
- Call `submit(content1)` to start a turn; call `submit(content2)` to buffer.
- Verify `drainQueue()` returns combined content and emits `QueueSignal(0)`.
- Verify `drainQueue()` on an empty buffer returns `null` (no emission).
- Verify the turn-end drain still works after a `drainQueue` call (no double-drain).

**Frontend** (manual or component test):
- Simulate: agent thinking chunk → user message → agent thinking chunk.
- Verify the two thinking chunks merge into one bubble (backward-scan merge).

## Large Test (testplan)

The feature MUST be validated end-to-end via the testplan skill (`guitar run <plan.yaml>`), per Constitution §VI. The large test covers Scenario 1 (mid-turn injection) using a fake-model agent that makes a tool call, allowing a queued message to be submitted during the tool execution window, and verifying the agent's next model call receives the queued message.

See `style/large_test.md` for testplan authoring conventions.
