# Research: LLM Stream Stall Recovery — Timeout Tuning & Partial Output Persistence

**Feature**: 044-llm-stall-recovery-fix | **Date**: 2026-08-12 | **Spec**: [spec.md](spec.md)

This document resolves the plan-level unknowns for [Feature 044](spec.md), grounded in the survey [`survey/llm-stream-stall-recovery-revision.md`](../../survey/llm-stream-stall-recovery-revision.md) and direct verification of the installed code. Each decision records what was chosen, why, and the alternatives rejected.

The spec's own `### Decisions resolved from the survey` and `### Session 2026-08-12` clarifications already fixed the WHAT (120s default; per-reasoning-model floor with `max(default, floor)`; per-block machine-readable "interrupted" flag; model-visible partial output; WarnSignal ⚠ bubble standardization). This research resolves the HOW at the implementation-design level.

---

## R1. Idle-timeout default value & references correction

**Decision**: `STREAM_IDLE_TIMEOUT_MS` default `30_000` → **`120_000`** (120s) in `projects/game/agent/src/llm.ts:43-44`. The minimum-configurable floor (enforced where the env var is read) rises to **60s**. The doc-comment block (`llm.ts:34-42`) is rewritten to cite the accurate industry anchors and drop the inaccurate "community 15–30s consensus" line.

**Rationale**: 120s is the industry median — LangChain Python `stream_chunk_timeout` ([PR #36949](https://github.com/langchain-ai/langchain/pull/36949)) and OpenClaw's idle window ([PR #93965](https://github.com/openclaw/openclaw/pull/93965)) both default to 120s; Codex is 300s ([issue #23807](https://github.com/openai/codex/issues/23807)); opencode client is disabled ([PR #18264](https://github.com/anomalyco/opencode/pull/18264)). The 043 "15–30s consensus" citation traced to opencode [PR #25575](https://github.com/anomalyco/opencode/pull/25575), which only *propagates* a user-configured value (not a default) — and opencode's own default was moved to disabled because tight chunk timeouts "cause too many small issues across the board." 120s eliminates the reasoning-model false-positive regime (Hermes measured ~65s to first content token for `deepseek-v4-flash`, [hermes#61461](https://github.com/NousResearch/hermes-agent/issues/61461)) while still catching real silent dropouts.

**Alternatives rejected**:
- **180s (Hermes stale-stream default)**: too lenient for non-reasoning real stalls; reasoning models are better served by the per-model floor (R2) than by inflating the global default.
- **Keep 30s, rely on retry**: rejected — retry is out of scope (spec FR-009), and false positives were the dominant production failure.
- **Disable by default (opencode style)**: rejected — we have no client-layer chunk-idle guard ([langchainjs #9088](https://github.com/langchain-ai/langchainjs/issues/9088)), so disabling LangGraph's `idleTimeout` would remove the only chunk-idle defense entirely.

**Verification gate**: unit test `STREAM_IDLE_TIMEOUT_MS === 120_000` when the env var is unset (`llm.test.ts`); existing `graph.test.ts:2684-2689` (asserts `nodes[name].timeout?.idleTimeout === STREAM_IDLE_TIMEOUT_MS`) continues to pass unchanged.

---

## R2. Per-reasoning-model floor

**Decision**: New module `projects/game/agent/src/reasoning-timeouts.ts` exporting:

- `REASONING_IDLE_TIMEOUT_FLOOR: ReadonlyArray<readonly [substring, floorMs]>` — an explicit, auditable allowlist (mirrors Hermes's `_REASONING_STALE_TIMEOUT_FLOORS`, [commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)). Initial set: DeepSeek family (`deepseek-r1`, `deepseek-reasoner`, `deepseek-v4-`) → 600_000ms; OpenAI o-series (`o1-`, `o3-`) → 600_000; `o3-mini-`/`o4-mini-` → 300_000; `claude-opus-` → 240_000. (Exact values frozen in tasks.md T003; DeepSeek 600s is the proven case.)
- `getReasoningIdleTimeoutFloor(modelSpec: string): number | null` — strips the `{provider}/` prefix via the existing `parseModelSpec` (`projects/game/agent/src/model-provider.ts:26-32`), then **longest-substring-first** matching (so `o3-mini-` matches before `o3-`; avoids `o1` matching `olmo-1` — a Hermes-documented pitfall).
- `resolveStreamIdleTimeout(modelSpec?: string): number` — implements [idle-timeout-contract.md §1](contracts/idle-timeout-contract.md#1-resolution-rule) 规则序：**(1) env 显式设置**（`llm.ts` 导出 `STREAM_IDLE_TIMEOUT_EXPLICIT` 标志）→ 返回 `STREAM_IDLE_TIMEOUT_MS` as-is（即使低于 floor — spec FR-003/US2.3）；**(2) 否则** spec 匹配 floor → `max(STREAM_IDLE_TIMEOUT_MS, floor)`；**(3) 否则** `STREAM_IDLE_TIMEOUT_MS`。**注意**：不能写成裸 `max(env_or_default, floor)` —— 那会把显式设置的低值（如 env=90s + DeepSeek 600s floor）抬到 600s，违反 FR-003；env 分支必须先行（契约 §1 的 `operator set env?` 分支）。

**Application point**: `projects/game/agent/src/team/graph.ts:383-389`. `TeamGraphDeps` gains **optional** `playerModelSpec?: string` / `plannerModelSpec?: string` (the bare model names). The builder computes `playerIdleTimeout = resolveStreamIdleTimeout(deps.playerModelSpec)` and `plannerIdleTimeout = resolveStreamIdleTimeout(deps.plannerModelSpec)`, and passes each to the corresponding `addNode({ timeout: { idleTimeout, refreshOn: "auto" } })`. Production wiring (`projects/game/agent/src/server.ts:260,335`) passes the profile's model specs (already available via `prompt-client.ts` `playerModel`/`plannerModel` strings). The deps are **optional** so existing test call sites (`buildTeamGraph({ playerModel, plannerModel, ... })` across `graph.test.ts`, `session-team.test.ts`, `handler.test.ts`, `context-middleware.test.ts`) keep working — they fall back to the default timeout.

**Rationale**: Co-locating the floor table with the timeout constant (`llm.ts` domain) and applying it at the single `addNode` seam keeps the change minimal and testable. Optional deps avoid breaking the ~30 existing `buildTeamGraph` call sites. Longest-first matching is a proven correctness safeguard.

**Alternatives rejected**:
- **Config-file/env-driven floor table**: rejected for v1 — an explicit code table is auditable and matches Hermes; externalizing adds config plumbing disproportionate to the small table. Can be revisited if the table grows.
- **Inflate the global default to 600s for everyone**: rejected — over-lenient for non-reasoning real stalls; the floor targets only reasoning models.
- **Apply the floor inside `model-provider.ts`**: rejected — the provider returns a `ChatModel` instance with no idle-timeout concept; the timeout is a LangGraph node property, applied at `addNode`.
- **`setNodeDefaults`** (apply timeout to ALL nodes): explicitly rejected by 043 (`graph.ts:373-376`) — would extend the timeout to `initInstruction`/`postCompactInstruction`/`compress`, whose event patterns are out of scope (they use the init-turn total timeout, 043 FR-009).

**Verification gate**: `getReasoningIdleTimeoutFloor("openai/deepseek-v4-flash") === 600_000`; `getReasoningIdleTimeoutFloor("gpt-4") === null`; longest-first test (`o3-mini-` → 300_000, not the `o3-` 600_000). `graph.test.ts` asserts the resolved per-node `idleTimeout` reflects the floor when a spec is supplied, and that an explicit env below the floor is honored as-is (FR-003).

---

## R3. Partial-output persistence — implementation layer

**Decision**: Implement persistence in **`runTeamTurn`** (`projects/game/agent/src/session-team.ts:725-912`), the `async *` generator that owns the `graph.streamEvents` loop AND has access to `this.graphHandle.graph.updateState` + `this.sessionId`. Pattern:

1. Maintain `const partialBlocks: TurnBlock[] = []` (type at `turn-loop.ts:91` — `{ agent: string; block: ContentBlock }`).
2. Before each `yield { agent, block }`, push a shallow clone `{ agent, block: { ...block } }` to `partialBlocks`.
3. Wrap the `for await (const event of stream)` loop AND the trailing `await stream.output` (line 911) in `try { … } catch (err) { if (isNodeTimeoutError(err)) { await this.persistPartialOutput(err, partialBlocks); } throw err; }`.
4. `persistPartialOutput` merges the stalled node's blocks (R7) into an AIMessage + ToolMessages (R6), calls `graph.updateState` (R4), then returns so the re-throw propagates to `runLoop` (`turn-loop.ts:346-359`) → `finishError` (warn + wait, retain buffer) — **unchanged 043 behavior**.

**Rationale**: `runTeamTurn` is the only layer that satisfies BOTH requirements: (a) it sees every streamed `TurnBlock` (the deltas the frontend already received), and (b) it holds the graph handle (`this.graphHandle.graph.updateState`) and session id needed to write the checkpoint. The persistence is therefore a transparent addition that re-throws the same error, leaving `turn-loop.ts`'s state machine untouched (Constitution §II — minimal, layered, not a fork).

**Alternatives rejected**:
- **Persist in `turn-loop.ts:runLoop`**: rejected — `runLoop` consumes `TurnBlock`s via `this.runner` but has **no graph handle** (the runner abstraction intentionally decouples the loop from the graph, per 043/031 D10). Pushing graph access into the loop would break that decoupling.
- **Patch LangGraph's `task.writes.splice`**: rejected — forking the framework; the splice is correct for atomic nodes, our case (streaming) needs compensation at the runner layer.
- **A separate `partialOutputs` channel**: rejected (spec Clarifications Q1/§6.4 survey decision) — writing to the existing per-agent channel (`playerMessages`/`plannerMessages`) keeps the `ListMessages` protocol unchanged and makes the partial output model-visible for next-turn continuity.

**Verification gate**: `session-team.test.ts` mock-stall — inject a fake stream that yields N blocks then rejects with a `NodeTimeoutError`; assert `updateState` was called with the merged AIMessage in the correct channel, then the error re-throws; `turn-loop.test.ts` confirms `finishError` still emits warn+wait with retained buffer.

---

## R4. `graph.updateState` after the AbortSignal fired (feasibility spike)

**Open question (spec Assumption / survey §6.4 risk)**: LangGraph's `idleTimeout` calls `timeoutController.abort()` then throws (`dist/pregel/timeout.js:200-211`). Does `graph.updateState({ configurable: { thread_id } }, values)` succeed when invoked from the `catch` block (i.e., after the in-flight `streamEvents` invocation was aborted)?

**Decision**: Treat as a **mandatory spike** in the first implementation task. Expected outcome (to confirm): `updateState` is an independent checkpointer write — it operates on the `MemorySaver` directly, not on the aborted `streamEvents` invocation's AbortController. The abort only cancels the in-flight pregel super-step; a fresh `updateState` call starts a new, independent checkpoint mutation. Therefore it should succeed.

**Spike procedure**: in `session-team.test.ts`, build a graph with a `MemorySaver`, start `streamEvents`, inject a `NodeTimeoutError` (or a real `idleTimeout` with a short timeout + a model that never emits), and in the catch call `graph.updateState(...)` then `graph.getState()` — assert the written values are present. If it fails (e.g., the checkpointer is locked), fall back to writing via a **fresh** `Pregel` interaction or direct checkpointer `.put` — documented as a tasks.md contingency.

**Rationale**: This is the single highest-risk assumption. Resolving it empirically in the first task de-risks the entire partial-output design before the rest is built.

**Verification gate**: the spike test itself is the gate; it MUST pass before downstream partial-output tasks proceed.

---

## R5. "Interrupted" flag — carrying mechanism that survives `ListMessages`

**Spec requirement (FR-005, per the Session 2026-08-12 clarification)**: the flag marks ONLY the content block that was mid-stream at the stall (typically the last block), not the whole partial reply; machine-readable; the desktop renders it (FR-013).

**Constraint**: `ListMessages` reconstruction (`projects/game/agent/src/handler.ts:668-717`) reorganizes an AIMessage's `content` array — it collects ALL `reasoning` blocks, then ALL `text` blocks (joined), then ALL `image` blocks — into `MessagePart`s. A naive per-block flag on a content-block array element would be **lost** during this reorganization (the reconstruction builds fresh parts, dropping unknown fields).

**Decision (two-layer carrier)**: the marker exists in two layers — checkpoint and wire.

- **Checkpoint layer** (agent in-process AIMessage): the interrupted tail content block carries `additional_kwargs: { interrupted: true }` (consistent with how `toolResultStatus` is carried on `additional_kwargs` at `llm.ts:428-435`). This survives the `MemorySaver` serde (confirmed by the R4 spike + T006 tests). It never leaves the agent process.
- **Wire layer** (proto, cross-network): a **formal proto enum field** — `PartCompletion completion` on `TextPart` and `ThinkingPart` (field number 2 each). `handler.ts` ListMessages reconstruction **translates** the checkpoint-layer `additional_kwargs.interrupted` into this proto field on the emitted part. See [data-model.md §4.2](data-model.md#42-wire-layer-carrier--partcompletion-proto-enum-field-cross-network) for the proto definition and [desktop-rendering-contract.md §3](contracts/desktop-rendering-contract.md#3-fr-013--interrupted-indicator-render-after-reconnect) for the wire shape.

**Why the wire layer MUST be a declared proto field (loose JSON proven infeasible)**: an earlier revision of this research proposed an additive `interrupted:true` field the desktop reads "leniently" (tolerating extra JSON). Empirical verification of the network path proved this CANNOT work — every hop strips undeclared fields: (1) `@grpc/proto-loader` serializes against `game.proto` (no `interrupted` field → dropped at serialize); (2) proxy `grpc-go` is strict proto; (3) gateway `grpc-gateway` protojson emits only known fields; (4) the desktop Go client `protojson.UnmarshalOptions{DiscardUnknown: true}` (`client.go:312`) discards unknown fields; (5) `view_model.go` `protoToJSONMap` marshals via strict `protojson.Marshal` (`:222-234`) — only known fields survive. A marker that is not a declared proto field cannot cross the network. Making it a declared field means every hop preserves it naturally. This is a **controlled exception to FR-010** (no proto wire change), authorized by the user for this single field (see plan.md "FR-010 controlled exception").

**Field placement — Option B (TextPart + ThinkingPart, not MessagePart)**: text and thinking are the only part kinds that stream incrementally and can be "mid-stream"; image/tool_call/tool_result are atomic. Placing the field on the two part kinds that need it matches the user's instruction ("为那些需要标记的 part 增加枚举参数"), keeps semantics scoped, and localizes the render to the existing per-variant branches. protojson omits the zero-value `PART_COMPLETION_UNSPECIFIED`, so a normal part serializes without the field (forward-compatible with older clients) — same precedent as `ToolResultStatus`.

**Why per-block is achievable here**: in practice the partial AIMessage has at most `[reasoning…][text…]` and the interrupted one is the **last** block (the one being streamed when the stall hit). Because the reconstruction already separates reasoning from text, marking the emitted text-part (or thinking-part) completion as `INTERRUPTED` maps cleanly to "the text/thinking that was cut off." If both reasoning and text were streaming (rare), the LAST emitted block type is marked.

**Alternatives rejected**:
- **Message-level `response_metadata.stall_interrupted`**: simplest, but marks the WHOLE message — explicitly rejected by the user (Session 2026-08-12: "仅标记中断时那一条，而不是整个 partial").
- **A new `MessagePart` kind** (e.g. `interrupted_marker`): rejected — pollutes the content model and is a larger proto change than a single enum field on two existing messages.
- **Loose-JSON additive field (prior approach)**: rejected — proven infeasible (see above); every network hop strips undeclared fields.
- **Drop per-block, mark whole message**: rejected (contradicts FR-005/SC-003 as clarified).

**Verification gate**: `handler.test.ts` — a checkpointed AIMessage whose last content block carries `additional_kwargs.interrupted` is returned by `ListMessages` with the corresponding `TextPart`/`ThinkingPart` `completion = PART_COMPLETION_INTERRUPTED`; a normal complete AIMessage emits no `completion` (UNSPECIFIED). `proto_test.go` — a `TextPart`/`ThinkingPart` with `INTERRUPTED` round-trips through protojson; the JSON contains `"PART_COMPLETION_INTERRUPTED"`.

---

## R6. Merge strategy — `TurnBlock[]` → checkpoint messages

**Decision**: New helper `mergePartialBlocks(blocks: TurnBlock[]): { aiMessage: AIMessage; toolMessages: ToolMessage[] }` (in `session-team.ts`, alongside the existing inverse `messageToContentBlocks` at `:1170`). Rules (spec FR-004/FR-006):

- **text blocks** → concatenated into one `{ type: "text", text }` content-block (in stream order). If this is the interrupted tail (last block overall), attach `additional_kwargs.interrupted = true` (R5).
- **reasoning blocks** → concatenated into one `{ type: "reasoning", reasoning }` content-block. If the interrupted tail, mark it.
- The AIMessage `content` is the array `[…reasoning?, …text?]` (matching how `messageToContentBlocks`/`ListMessages` expect array content). `tool_calls` on the AIMessage = **only complete tool calls** (see below).
- **tool_result blocks** (`type: "tool_result"`, from `tool-finished` events) → each becomes a standalone `ToolMessage` (with `tool_call_id`, content encoding the message/screenshot, and `additional_kwargs.toolResultStatus`). These represent side effects that already executed on the desktop — RETAINED (spec FR-006).
- **tool_call blocks** (`type: "tool_call"`, from `tool-started` events): a tool_call with a **matching retained tool_result** is complete → keep it on the AIMessage's `tool_calls` (so history shows the call + its result, linked). A tool_call with **no matching tool_result** is mid-flight (the tool was invoked but never returned before the stall) → **DROP** it (spec FR-006: a partial tool_call cannot be dispatched and would corrupt tool history). This mirrors opencode's "cleans up incomplete tool calls before retrying" ([PR #19116](https://github.com/anomalyco/opencode/pull/19116)).

**Rationale**: The rules make the persisted partial output a **valid, replay-safe conversation state**: text/reasoning the user saw is preserved; executed tool side effects are retained; un-executable partial tool calls are dropped. The AIMessage shape round-trips through `ListMessages` identically to a normal AI reply (reconstruction at `handler.ts:668-717` reads exactly this content-array + tool_calls shape).

**Edge cases handled**:
- No text and no reasoning (stall at first byte) → no AIMessage to persist; only complete ToolMessages (if any) are written. If nothing was streamed, `persistPartialOutput` is a no-op (spec US3.4).
- Reasoning-only (no content text) → AIMessage with a single reasoning content-block (marked interrupted).
- Only complete tool calls (no streaming text) → AIMessage with tool_calls + matched ToolMessages.

**Verification gate**: `session-team.test.ts` unit tests for `mergePartialBlocks` covering: text+reasoning, reasoning-only, tool_call with result (kept), tool_call without result (dropped), empty (no-op).

---

## R7. Partitioning by stalled node — `NodeTimeoutError.node`

**Problem**: A single `runTeamTurn` invocation can stream **multiple nodes** (player → conditional edge → planner). When the planner stalls, the accumulated `partialBlocks` contain BOTH the player's (already-checkpointed) output AND the planner's (lost) partial. Persisting ALL blocks would **duplicate** the player's output (the player node completed → its writes were committed to the checkpoint by LangGraph before the planner started; only the stalled node's `task.writes` are spliced).

**Decision**: Read the stalled node name from the error. `NodeTimeoutError` exposes `node: string` and `kind: "run" | "idle"` (`@langchain/langgraph` `dist/errors.d.ts:103-125`, verified). In `persistPartialOutput`:

1. Guard: `isNodeTimeoutError(err) && err.kind === "idle"` (run-timeouts aren't configured here, but the guard is explicit).
2. Read `stalledAgent = err.node` (e.g. `"player"` or `"planner"`).
3. Filter `partialBlocks` to `blocks.filter(b => b.agent === stalledAgent)` — only the stalled node's streamed blocks.
4. Channel = `stalledAgent === "player" ? "playerMessages" : "plannerMessages"` (matches `AGENT_CHANNELS` at `handler.ts:82-83`).
5. `updateState({ configurable: { thread_id: this.sessionId } }, { [channel]: [aiMessage, ...toolMessages] })`.

**Rationale**: This avoids duplicating already-checkpointed prior-node output. Prior completed nodes committed their writes normally; only the stalled node's streamed-but-uncheckpointed output needs compensation. The `.node` field makes the partitioning exact and robust.

**Edge case — subgraph namespaces**: `agentFromNamespace` (`session-team.ts:787`) extracts the agent from the event namespace (createAgent subgraphs). The `err.node` from LangGraph is the **outer** graph node name (`"player"`/`"planner"`, the `addNode` names), which matches the TurnBlock `agent` values yielded for the outer nodes. If the stall occurs inside a createAgent subgraph, `err.node` may carry a namespaced path — `persistPartialOutput` normalizes via the same `agentFromNamespace` helper before filtering. This is verified in the R4 spike.

**Verification gate**: `session-team.test.ts` — a turn that streams player blocks (complete) then planner blocks (stall) results in `updateState` writing ONLY to `plannerMessages` (player's output not duplicated); player-only stall writes only to `playerMessages`.

---

## R8. Desktop WarnSignal rendering standardization + proto reconciliation

**Decision (FR-012)**: The desktop already renders `FlowPart.warn` (WarnSignal) as a conversation ⚠ bubble (`projects/game/desktop/frontend/src/App.svelte:789-802` builds a `warnMessage` entry; `components/ChatView.svelte:271-279` renders `.msg-warn` / `.warn-bubble` with a ⚠ icon). FR-012 **formalizes** this as the standard for all warn sources (no agent change — the agent's `warnFrame()` emission at `turn-loop.ts:522` is unchanged). The proto comment at `projects/game/game.proto:451-453` (spec 023: "FlowPart … never rendered as conversation entries") is **updated** to document `warn` as the rendered exception (warn is a control signal that IS surfaced as a distinct warning bubble; all other FlowPart kinds remain non-rendered).

**Decision (FR-013)**: The desktop history-seed path (which builds chat entries from `ListMessages` on reconnect) renders the interrupted marker (R5) — now carried by the `TextPart.completion`/`ThinkingPart.completion` proto enum field (`PART_COMPLETION_INTERRUPTED`) — as a visual "中断"/truncated indicator on that bubble. The live stall notice continues to use the ⚠ warn bubble (transient, live-only); the interrupted flag is what survives reconnect. Because the marker is a formal proto field, the Go desktop layers (`client.go` `DiscardUnknown` unmarshal, `view_model.go` strict `protojson.Marshal`) preserve it with **no logic change** — known fields are never discarded and are always emitted by strict marshal.

**Key distinction (verified, spec Assumption)**: there is exactly ONE `warn` mechanism — the `WarnSignal` FlowPart (`projects/game/game.proto:505,644-648`). It is **transient**: FlowParts are never persisted to message history (`game.proto:741`, `desktop/frontend/src/api.ts:398`), so the ⚠ bubble is gone after reconnect. This is precisely why the per-block "interrupted" flag (R5) MUST live on the persisted `Message` — it is the only signal that survives reconnection. (`warn()` from `@dominion/common-js-logs` is a separate server-side log function — not to be confused with WarnSignal.)

**Alternatives rejected**:
- **Persist the warn as a synthetic history message**: rejected — violates the content-model contract (FlowParts never in history); would require a new MessagePart kind (proto change).
- **Drop the surviving flag, rely on the transient warn only**: rejected — after reconnect the partial output would look like a normal (short) message with no truncation indication (spec SC-003 requires visibility).

**Verification gate**: desktop unit/component test — a `warn` FlowPart renders a ⚠ bubble; a `ListMessages` entry with an interrupted part renders the interrupted indicator; large test — stall → reconnect → both the partial output and its interrupted marker are visible.

---

## R9. T012 heartbeat false-stall — root-cause investigation (2026-08-14, post-enabler)

**Context**: [large-test-status.md](large-test-status.md) §2 — `TestAgentStallToolExecutionNotFalselyDetected` (043 US3) failed in env `game.ltum8zvw`: a `NodeTimeoutError(idle)` fired at ~60s during a 65s tool wait that the client-side heartbeat (`withIdleHeartbeat`, 10s cadence) was supposed to bridge. Root cause was left unconfirmed ((a) real agent bug vs (b) test-config limitation).

**Signoz evidence (queried 2026-08-14)**: trace `843f54736610e7cee177f6646a75b7cc` spans have **expired** from the trace store (empty result). The log store retains 7 records for that trace: `operation bridge sink registered` at `2026-08-13T07:06:37.185Z` → `connect stream ended` + `operation bridge sink unregistered` at `07:07:42.31Z` (+65.1s), and proxy `connect team: bind failed — rpc error: code = Canceled desc = context canceled` at `07:07:42.30Z` — consistent with a false stall firing at ~60s and tearing down the turn before the 65s desktop reply. Critically, a whole-env query (`service.name='game/agent' AND body CONTAINS timeout|stall|idle|heartbeat`) returns **zero records**: the agent logs neither heartbeat ticks nor stall/timeout events. **(a) vs (b) is NOT discriminable from the stale run.**

**Mechanics audit (`@langchain/langgraph@1.4.8`, `node_modules/@langchain/langgraph/dist/pregel/timeout.js`)**:

- `TimedAttemptScope.touch()` refreshes `lastProgress` **unconditionally** (the `refreshOn` gate applies only to `autoTouch`); `wrapped.heartbeat` calls `touch()` whenever `policy.idleTimeout !== undefined`.
- The idle watchdog is a self-rescheduling check: `checkIdle` computes `remaining = lastProgress + idleMs − now`; on `remaining > 0` it re-arms `setTimeout(checkIdle, remaining)`. A 10s heartbeat cadence against a 60s window **cannot mathematically elapse** — every timer fire sees ≥50s remaining.
- Wiring is intact: `withIdleHeartbeat` wraps BOTH production MCP paths (`projects/game/agent/src/llm.ts:386` saolei, `:426` memory); langchain's ToolNode spreads `...config` into the tool invoke (`node_modules/langchain/dist/agents/nodes/ToolNode.js:229-241`) so `config.heartbeat` reaches the wrapper, and the wrapper drives its own `setInterval` regardless of what the inner MCP call does with the config.

**Conclusion**: static analysis cannot reproduce the failure. The observed signature (fire at ≈ node-start + 60s, i.e. as if `lastProgress` never advanced past the pre-tool auto-touch) matches "heartbeats stopped reaching the scope after the first refresh" — and the 15s-scale pass (where ONE tick at T=10s sufficed) vs the 60s-scale fail (where REPEATED ticks are required) is consistent with an interval that stops after its first tick. But there is no log evidence either way.

**Decision**: three-part resolution, executed together in the resume scope:

1. **Observability first**: add per-tick structured logging to `withIdleHeartbeat` (`@dominion/common-js-logs` `info`, mirroring fake-llm's 046 FR-018 per-chunk log pattern) so any recurrence is directly attributable in signoz: ticks present + false stall → the `touch()`→`checkIdle` path is broken (LangGraph contract issue); ticks absent → the wrapper's timer lifecycle is broken (agent bug).
2. **Short-scale re-run**: with R10's controlled config (5s idle / 2s heartbeat / 12s tool delay — six heartbeat ticks inside the wait), a recurrence fails in ~5s instead of ~60s, making the debug loop fast.
3. **Contingency branch** (only if the false stall recurs with ticks logged): inspect the failing trace via signoz; fix at the identified layer; re-run until green (Constitution VI).

Note: the failing run (2026-08-13 07:07) used the **pre-046** fake-llm (046 landed 2026-08-14 03:40+); 046 preserves legacy template behavior byte-for-byte (046 FR-007/SC-004), so the heartbeat failure predates and is unaffected by 046.

---

## R10. Service-config channel for agent timeouts (045 integration, 2026-08-14)

**Decision**: introduce a **single test-grade config block** consumed by the agent, with per-parameter resolution `env (explicit, clamped) > service config (explicit, as-is) > code default`:

- `projects/game/agent/service.yaml` declares block **`agent_timeouts`**, entry **`timeouts`** (type `yaml`), fields `streamIdleTimeoutMs` / `toolHeartbeatIntervalMs` / `initTurnTimeoutMs` (numbers, ms). Shipped values are **test-grade** (`5000` / `2000` / default) — selected ONLY by the stall deploy; production and standard deploys select nothing and keep code defaults.
- New `projects/game/agent/src/agent-timeouts.ts`: `DEFAULT_AGENT_TIMEOUTS` (120s / 10s / 120s), `loadAgentTimeoutOverrides()` (readConfig in try/catch → `undefined` when the block is not selected or `DOMINION_CONFIG_DIR` is unset — a **deliberate divergence** from 045 US3.3's "error on unselected block": the agent treats the block as an optional override channel, since production must run without it), and pure `resolveAgentTimeouts(env, overrides)` implementing the precedence matrix + the heartbeat<idle fail-fast validation.
- `llm.ts` keeps its exported constant names (`STREAM_IDLE_TIMEOUT_MS`, `STREAM_IDLE_TIMEOUT_EXPLICIT` — now true when **env OR config** supplies the idle value — `INIT_TURN_TIMEOUT_MS`, `TOOL_HEARTBEAT_INTERVAL_MS`) sourced from the resolver, so `reasoning-timeouts.ts` and all downstream imports stay unchanged.
- `deploy_agent_stall.yaml`: drop `GAME_STREAM_IDLE_TIMEOUT_MS` env; add `configs: [agent_timeouts]` to the `agent_test` artifact.

**Semantics rationale**:

- **Config = explicit operator configuration.** Selecting a block is a deliberate, code-reviewed deploy-time act; honoring it as-is (even below a floor, even below 60s) matches FR-003's "explicit operator choice wins" exactly as the env channel does. Consequently the reasoning floor is suppressed whenever the idle timeout comes from env or config — the floor only ever raises the **code default**.
- **The 60s minimum clamp stays env-scoped.** FR-001's clamp is a typo-guard for the raw env channel; config values are committed, auditable declarations (045 FR-008 selection-only), a different trust tier. This is the precise relaxation that unblocks fast large tests.
- **Heartbeat < idle validation**: resolved heartbeat ≥ resolved idle throws at startup (fail-fast with guidance), because the invariant (`llm.ts` `TOOL_HEARTBEAT_INTERVAL_MS` doc) is load-bearing for 043 FR-003.

**Alternatives rejected**:

- **Multi-block cascade** (production block + test block, agent reads test-first): production already has the env channel for ops tuning; the cascade adds resolution complexity for zero present value. Revisit if production adopts config-driven tuning.
- **Testplan-local service.yaml variant**: duplicates the whole service definition per test deploy — anti-pattern; values would drift from the real service.
- **Keeping the env channel and lowering the clamp**: violates FR-001's false-positive-regime guard for raw env input; the config channel isolates test-grade values to a reviewed file instead.

**Verification**: unit — `resolveAgentTimeouts` matrix (env-only clamp, config as-is incl. <60s, env>config precedence, floor suppression via `STREAM_IDLE_TIMEOUT_EXPLICIT`, heartbeat≥idle throws, absent block → defaults); large — the stall suite runs green at the config-driven timings.

---

## R11. SC-005 re-evaluation after 046 (spec-owner decision still pending)

The A4 rationale (dropped US2 floor large-test case) rested on two legs: (1) the explicit deploy env suppresses the floor; (2) "fake-llm has no recoverable-silence template". **Leg (2) is now false** — 046 shipped `think-interrupt-gap` (finite resumable mid-thinking gap, `projects/game/fake-llm/service/testdata/stall_recovery.yaml`; 046 初始 90s，[tasks.md](tasks.md) T019 缩至 15s 以匹配 config 驱动的 5s 窗口). Leg (1) still holds for ANY explicit idle source (env or config, per FR-003/R10), so a floor large-test requires the **default path** (env+config unset → effective `max(120s, floor)`), i.e. a >120s silent gap observed to NOT fire.

Cost re-assessment of option γ: `deploy_agent.yaml` (standard suite) already runs the default topology — **no new deploy needed**; the case rides an existing standard-suite module file with a deepseek profile and a ~150s-gap template (~3min case). γ's earlier rejection ("high cost; essentially tests LangGraph's contract") is therefore substantially weaker, though the >120s wall time remains.

**Status**: α (unit-level substitution + quickstart note) remains the recorded default; γ is drafted as an OPTIONAL task in [plan.md](plan.md) (resume scope), gated on the spec owner's ruling. β (reword SC-005) untouched. See [large-test-status.md](large-test-status.md) §4.

---

## Summary of resolved unknowns

| Unknown | Resolution | Section |
|---|---|---|
| New idle default & references | 120s; accurate anchors; min 60s | R1 |
| Reasoning-floor mechanism | `reasoning-timeouts.ts`, longest-first, `max(default, floor)`, optional graph deps | R2 |
| Partial-output persistence layer | `runTeamTurn` accumulate + catch + `updateState` + re-throw | R3 |
| `updateState` after abort | Mandatory spike (expected: succeeds — independent checkpointer write) | R4 |
| Interrupted-flag carrying | Checkpoint: `additional_kwargs.interrupted` (agent in-process); Wire: `PartCompletion` proto enum field on `TextPart`/`ThinkingPart` (FR-010 controlled exception — loose JSON proven infeasible) | R5 |
| Merge rules | text/reasoning → AIMessage content; tool_result → ToolMessage; unmatched tool_call dropped | R6 |
| Multi-node partitioning | Filter by `NodeTimeoutError.node`; write only the stalled node's channel | R7 |
| WarnSignal rendering + proto | Formalize existing ⚠ bubble; update proto comment; interrupted flag survives reconnect | R8 |
| T012 heartbeat false-stall root cause | Not discriminable from stale telemetry (no heartbeat logs existed); per-tick logging + short-scale re-run is the discriminator; contingency branch defined | R9 |
| Timeout params → service config (045) | Single test-grade block `agent_timeouts`; env > config > default; config as-is (no clamp), floor-suppressing, heartbeat<idle fail-fast | R10 |
| SC-005 post-046 | α stands by default; γ now cheap-ish (default topology + resumable-silence template exist) — spec-owner ruling pending | R11 |

All plan-level unknowns are resolved (R4 is an empirical spike with a clear expected outcome and a documented contingency; R9's (a)/(b) split is resolved *procedurally* — observability + short-scale rerun — rather than from stale data; R11's α/β/γ is a recorded spec-owner decision, defaulting to α). No `NEEDS CLARIFICATION` remains.
