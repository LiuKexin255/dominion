# Feature Specification: LLM Stream Stall Recovery — Timeout Tuning & Partial Output Persistence

**Feature Branch**: `044-llm-stall-recovery-fix`

**Created**: 2026-08-12

**Status**: Draft

**Input**: User description: "为 `survey/llm-stream-stall-recovery-revision.md` 当中提到的两个问题修复制定 spec" — fix the two production problems observed after [Feature 043](../043-llm-stream-stall-recovery/spec.md) shipped: (1) stall detection fires far too frequently on reasoning-heavy scenarios, and (2) already-streamed agent output is permanently lost from the checkpoint when a stall terminates a turn.

## Motivation

[Feature 043 — LLM Stream Stall Recovery](../043-llm-stream-stall-recovery/spec.md) shipped on 2026-08-11 with a chunk-idle watchdog defaulting to **30 seconds** (`STREAM_IDLE_TIMEOUT_MS`, `projects/game/agent/src/llm.ts:43-44`). Two severe production problems emerged, fully analyzed in the survey [`survey/llm-stream-stall-recovery-revision.md`](../../survey/llm-stream-stall-recovery-revision.md):

### Problem 1 — Stall detection fires far too frequently (false positives)

In production session `a7cb3d62f0269fa88410093380f79def` (env `game.prod`, template `saolei`, model `deepseek-v4-flash`), **two stalls occurred within 7 minutes** (player stall at 13:20:23 with `elapsed=160232ms`, planner stall at 13:21:43 with `elapsed=30004ms`), making a single minesweeper game nearly uncompletable. Critically, the **same model + gateway does not interrupt this aggressively in the opencode client** — proving our 30s threshold, not the upstream provider, is the dominant trigger.

Cross-framework research (survey §4, §5.1–§5.3) shows our 30s default is the **most aggressive in the industry** — 4–10× tighter than every comparable framework:

| Framework | chunk-idle default | reasoning model handling |
|---|---|---|
| LangChain Python (`stream_chunk_timeout`, [PR #36949](https://github.com/langchain-ai/langchain/pull/36949)) | **120s** | none |
| OpenClaw ([PR #93965](https://github.com/openclaw/openclaw/pull/93965)) | **120s** idle + 300s first-event | none |
| Hermes Agent ([commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)) | **180s** | **600s floor (allowlist)** |
| OpenAI Codex CLI ([issue #23807](https://github.com/openai/codex/issues/23807)) | **300s** | none |
| opencode client ([PR #18264](https://github.com/anomalyco/opencode/pull/18264)) | **disabled** by default | none |
| **dominion (Feature 043)** | **30s** ⚠️ | none |

Two root causes compound:

1. **Over-aggressive default.** Feature 043's `spec.md:20` cited opencode [PR #25575](https://github.com/anomalyco/opencode/pull/25575) as evidence of a "15–30 second community consensus." That citation is inaccurate (survey §5.2): PR #25575 only *propagates* a user-configured value, and opencode's own default was moved to **disabled** by [PR #18264](https://github.com/anomalyco/opencode/pull/18264) because "this is causing too many small issues across the board." The 30s figure was a PR example value, never a consensus.
2. **No accommodation for reasoning models.** `deepseek-v4-flash` is a reasoning model ([OpenCode Go docs](https://opencode.ai/docs/zh-cn/go/)) that consumes `reasoning_content` for an extended period before emitting the first `content` token. Hermes measured **~65s to first content token** for this exact model+gateway ([hermes issue #61461](https://github.com/NousResearch/hermes-agent/issues/61461)) — a normal reasoning pass *alone* exceeds 30s. When the model enters a deep-thinking phase it emits no chunks at all (not even keepalive pings), so the idle timer elapses and aborts a legitimately-progressing turn. Hermes solved exactly this with a per-reasoning-model stale-timeout floor ([commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa), defaulting DeepSeek to 600s).

### Problem 2 — Already-streamed agent output is permanently lost after a stall

When a stall terminates a turn, the agent output that was *already streamed to the frontend* vanishes from the session on reconnect. User-observed sequence:

```
开始游戏 → agent output1 (streamed) → stall → user "继续游戏" → agent output2 → abort
```

After re-entering the session, `ListMessages` returns `开始游戏 / 继续游戏 / agent output2` — **output1 is gone forever** (survey §5.5).

Root cause: LangGraph's `idleTimeout` assumes nodes are atomic — on expiry it runs `task.writes.splice(0, task.writes.length)` (`@langchain/langgraph` `dist/pregel/timeout.js:200-211`), **discarding all buffered writes** before raising `NodeTimeoutError`. But streaming breaks atomicity: partial output was already yielded to the frontend through the live Connect stream, yet never reached the checkpoint. `ListMessages` reads exclusively from the checkpoint (`projects/game/agent/src/handler.ts:619` → `team.getTeamState()` → `graph.getState().values.playerMessages`), so the streamed-but-uncheckpointed output is irrecoverable. Feature 043's FR-006 only retains the *user input buffer*; it never considered *agent output already streamed* — a genuine blind spot in 043's acceptance scenarios.

### Goal of this revision

This spec is a **focused follow-up to Feature 043**, not a rewrite. It preserves 043's stall-detection architecture (LangGraph `TimeoutPolicy.idleTimeout` + tool-execution heartbeat) and corrects the two production problems: (a) calibrate the idle threshold to industry norms and add reasoning-model tolerance, and (b) persist already-streamed partial output on stall so the checkpoint and the live stream stay consistent.

## Clarifications

### Decisions resolved from the survey

The survey (`survey/llm-stream-stall-recovery-revision.md` §9) posed six open questions. The following resolutions are adopted as informed defaults (rationale recorded so they can be revisited in `/speckit.plan`); none require blocking user input because each has a clearly-justified, research-backed answer:

- **New spec vs. 043 revision** → **New spec 044.** Feature 043 is fully delivered and serves as the historical record of the original mechanism. A focused follow-up spec keeps each change auditable and avoids mutating a shipped spec's FRs in place. (Constitution §II — Refactoring Over Patching: the correction is delivered as a coherent new change, not a patch overlaid on 043's prose.)
- **New default idle value** → **120s.** Aligns with LangChain Python ([PR #36949](https://github.com/langchain-ai/langchain/pull/36949)) and OpenClaw's idle window ([PR #93965](https://github.com/openclaw/openclaw/pull/93965)) — the industry median. 180s (Hermes) was rejected as too lenient for non-reasoning real stalls; reasoning models are instead handled by the floor (next decision). The minimum configurable value rises from 15s to **60s** to prevent reintroducing the false-positive regime.
- **Reasoning-model floor** → **Adopt the Hermes-style per-model floor.** Known reasoning models receive a longer idle-tolerance floor (DeepSeek family → 600s, matching Hermes's measured safety margin for the ~65s first-token latency). The floor **only ever raises the default**; an explicit operator configuration always wins, even if lower (`max(default, floor)` semantics, per [hermes `reasoning_timeouts.py`](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)). This is the layer that specifically fixes `deepseek-v4-flash`.
- **Partial-output marking mechanism** → left to `/speckit.plan` (an implementation detail). The spec requires only that persisted partial output be *distinguishable* from a complete reply; the exact metadata key is a plan-level decision.
- **Automatic retry / fallback** → **explicitly out of scope** (FR-007), deferred to a future spec. The survey (§6.3) recommends it ship independently because replay-safety (distinguishing model-idle from side-effecting tool execution) is a distinct, higher-complexity concern.
- **Self-written `stream_chunk_timeout` in the model provider** → **out of scope.** LangChain JS still lacks a client-layer chunk-idle guard ([langchainjs #9088](https://github.com/langchain-ai/langchainjs/issues/9088)); we continue to rely on LangGraph's `idleTimeout` as the sole chunk-idle defense, now correctly calibrated.

### Session 2026-08-12

- Q: When partial output is persisted after a stall, should it become part of the model's conversation context on the next turn (model-visible, enabling continuity) or be user-visible-only (excluded from the model's input)? → A: **Model-visible** — persist into the per-agent message channel so the model sees its own truncated reply on the next turn and can continue from it (continuity); `ListMessages` naturally returns it. Chosen for simplicity (no separate display-only channel or input filter) and because it lets "继续游戏" resume the interrupted thought; confusion risk is mitigated by the interrupted-block marker (FR-005) and by dropping partial tool calls (FR-006).
- Q: How should the "incomplete" marker on persisted partial output be carried, given the `warn` (WarnSignal) bubble is a transient FlowPart that does not survive reconnection? → A: **Per-block machine-readable metadata flag (Option A).** The flag marks ONLY the specific content block (text or thinking) that was mid-stream at the moment of stall — typically the last block of the partial; earlier fully-streamed blocks in the same turn are NOT marked. The desktop renders an "interrupted" indicator on the flagged block. Real-time notice still uses the ⚠ `warn` bubble. Additionally: **standardize the desktop's WarnSignal rendering** — render every `warn` as a conversation ⚠ bubble (the current idleTimeout-style rendering), reconciling [Feature 023](../023-saolei-mcp-refine/spec.md)'s "FlowParts never rendered as conversation entries" statement (warn is the documented exception).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Idle Detection Aligns With Industry Norms (Priority: P1)

The agent's stall-detection window reflects the real-world balance between catching a genuinely stalled stream and not interrupting a model that is legitimately working. The default idle period is raised to an industry-median value (120 seconds), so brief network jitter, normal inter-chunk pauses, and ordinary model "thinking" between tokens no longer abort a healthy turn. A real silent-stream dropout is still detected — just without the false-positive storm the 30s default caused.

**Why this priority**: This is the stop-the-bleeding fix. The 30s default is the single largest source of false stalls across *all* models (not only reasoning ones). Raising it to 120s immediately removes the bulk of the interruptions users experience, and it is the lowest-risk, highest-leverage change in this spec.

**Independent Test**: Configure a non-reasoning model with normal inter-chunk latency. Run a turn that includes pauses between tokens (but no true stall) longer than 30s yet shorter than 120s. Verify no stall fires and the turn completes normally. Separately, simulate a true silent dropout (no events) and verify the stall is still detected within ~120s + margin.

**Acceptance Scenarios**:

1. **Given** an agent turn delivering content with normal inter-chunk pauses, **When** a pause longer than the old 30s threshold (but shorter than the new default) occurs, **Then** no stall is detected and the turn continues to completion.
2. **Given** a genuinely stalled stream (connection alive, zero events), **When** the new default idle period elapses (~120s), **Then** the stall IS detected and the agent recovers (per [Feature 043](../043-llm-stream-stall-recovery/spec.md) FR-004/FR-005) — real stalls are still caught.
3. **Given** an operator who sets the idle period explicitly via the configuration variable, **When** the value is at least the new minimum (60s), **Then** the operator's value is honored; values below the minimum are rejected/clamped.

---

### User Story 2 - Reasoning Models Get Extended Thinking Tolerance (Priority: P1)

Models that produce extended reasoning before emitting content (e.g., `deepseek-v4-flash`, which can take ~65s to produce the first content token while it consumes `reasoning_content`) no longer trigger false stalls during their legitimate thinking phase. The agent applies a longer idle-timeout floor to known reasoning models, so a deep-thinking pass that emits no chunks for an extended period is recognized as progress rather than a stall.

**Why this priority**: This is the layer that specifically fixes the production `saolei` + `deepseek-v4-flash` outage — the template where stalls were catastrophic. Without it, reasoning models remain unusable even after the default is raised, because their normal thinking time can exceed any reasonable non-reasoning default.

**Independent Test**: Configure a reasoning model whose thinking phase emits no streaming events for a period longer than the default idle period but shorter than its floor (e.g., ~90s of silent reasoning for a model with a 600s floor). Verify no stall fires and the turn eventually produces content. Separately, verify a *real* stall on the same model (no events beyond the floor) is still detected.

**Acceptance Scenarios**:

1. **Given** an agent turn using a known reasoning model in its thinking phase (no streaming events), **When** the silent period exceeds the default idle period but remains below the model's floor, **Then** no stall is detected — the thinking phase is treated as legitimate progress.
2. **Given** a reasoning model whose floor applies, **When** no explicit operator configuration is set, **Then** the effective idle period is the higher of the default and the model's floor (the floor never lowers the default for non-matching models).
3. **Given** an operator who explicitly configures the idle period for a reasoning model, **When** that explicit value is applied, **Then** the operator's value wins as-is — the floor does not override an explicit operator choice (even if the operator chose a value below the floor).
4. **Given** a model that is NOT in the reasoning-model set, **When** a turn runs, **Then** only the default idle period applies — unrecognized models are never given an inflated timeout.

---

### User Story 3 - Streamed Output Survives a Stall and Reconnection (Priority: P1)

When a stall terminates a turn mid-reply, the portion of the agent's output that was *already streamed to the frontend* is persisted to the checkpoint so it survives a session reconnection. After re-entering the session, the user can see that partial reply in the message history (marked as incomplete), instead of it silently vanishing. The live stream and the persisted checkpoint stay consistent: what the user saw during the turn is what they see after reconnect.

**Why this priority**: This is a data-integrity fix. Silent, unrecoverable loss of agent output is a correctness defect — the user watched the agent reply, then lost it forever. It is co-equal with the stall-frequency problem because even a perfectly-calibrated timeout will occasionally fire on a real stall, and every such event must not destroy already-produced work.

**Independent Test**: Start an agent turn, let the agent stream a partial reply, then simulate a stall. Verify the turn terminates (per [Feature 043](../043-llm-stream-stall-recovery/spec.md)). Reconnect / re-enter the session and call `ListMessages`: verify the partial reply is present in the message history and is marked as incomplete (distinguishable from a complete reply).

**Acceptance Scenarios**:

1. **Given** an agent turn that has already streamed partial output (text and/or reasoning) to the frontend, **When** a stall is detected and the turn terminates, **Then** the already-streamed partial output is persisted to the checkpoint.
2. **Given** a stall-terminated turn with persisted partial output, **When** the session is re-entered and the message history is read (e.g., via `ListMessages`), **Then** the partial output is present — it does not silently vanish.
3. **Given** a persisted partial output whose last streamed block was truncated by the stall, **When** the user views the message history after reconnection, **Then** only that interrupted block is visibly marked (an "interrupted" indicator) so the user can tell where the reply was cut off; earlier completed blocks in the same turn are not marked.
4. **Given** a stall that occurs before any output was streamed (e.g., stall at first byte), **When** the turn terminates, **Then** there is no partial output to persist and the message history simply reflects the user's input — no empty/truncated artifact is fabricated.
5. **Given** a turn where one or more tool results had fully completed and been dispatched before the stall, **When** partial output is persisted, **Then** those complete tool results are retained (they represent side effects that already happened); any incomplete/partial tool call that was mid-flight is NOT retained as a dispatchable call (it cannot be executed and would corrupt tool history).
6. **Given** a stall-terminated turn whose partial output was persisted, **When** the next turn begins (e.g., the user sends "继续游戏"), **Then** the model's input context includes the persisted partial reply (its last block flagged interrupted) so the model can continue from where it was interrupted — recovery is continuous, not a clean-slate restart.

---

### Edge Cases

- **Reasoning model not yet in the floor set**: a newly-introduced reasoning model not present in the floor list receives only the default (120s) and may false-stall during deep thinking until the list is updated. This is an accepted, documented maintenance obligation (see Assumptions) — the floor set is intentionally explicit and auditable, not heuristic.
- **Explicit config below a reasoning floor**: if an operator deliberately sets a value below a model's floor, the explicit value is honored as-is (the floor only raises the *default*, never overrides an explicit operator decision). This preserves operator control and matches Hermes semantics.
- **Partial output is reasoning-only (no content text)**: if the model streamed only `reasoning_content` and no final `content` before stalling, the reasoning block is persisted and carries the "interrupted" flag (it was the mid-stream block) — the user can see the model's partial thinking, marked as cut off.
- **Stall during the tool-execution phase vs. the model-streaming phase**: the tool-execution heartbeat (043 FR-003) keeps the idle timer alive during tool dispatch, so a stall during tool execution does not occur; partial-output persistence only triggers when the *model-streaming* phase stalls. A complete tool result already dispatched before a model-phase stall is retained (side-effect safety); a partial tool call mid-dispatch is dropped.
- **Repeated stalls in one session**: each stall persists its own partial output as a separate message whose last block is flagged interrupted. The message history accumulates `[user] → [AI, last block interrupted] → [user "继续"] → [complete AI]`, accurately reflecting what happened.
- **Partial output followed by a later complete reply**: the incomplete partial remains in the model's conversation history and is visible to the model on the next turn, so the next reply can continue from it; the interrupted block keeps its "interrupted" flag, and the user sees both the truncated partial (with the cut-off point marked) and the continuation.
- **`updateState` invoked after the model call's AbortSignal fired**: persisting partial output on stall requires writing to the checkpoint after the stall aborted the in-flight call. The write is an independent checkpoint operation, not part of the aborted model invocation, so it is expected to succeed; this assumption must be verified in `/speckit.plan` (survey §6.4 risk note).
- **Interaction with 043's queued-message retention**: this feature is additive to 043 FR-006/FR-007. The user input buffer is still retained and auto-drained on the next turn; the partial *agent* output is now *also* retained in the message history. The two retentions are independent (one is queued user input, the other is already-streamed agent output) and do not conflict.

## Requirements *(mandatory)*

### Functional Requirements

**Idle Timeout Calibration (Problem 1 — default)**

- **FR-001**: The default stream-stall idle period MUST be raised from 30 seconds to **120 seconds**, aligning with the industry-median chunk-idle defaults of LangChain Python ([PR #36949](https://github.com/langchain-ai/langchain/pull/36949)) and OpenClaw ([PR #93965](https://github.com/openclaw/openclaw/pull/93965)). The minimum configurable value MUST be raised from 15 seconds to **60 seconds** to prevent reintroducing the false-positive regime. The existing configuration variable (`GAME_STREAM_IDLE_TIMEOUT_MS`) MUST continue to override the default. *(Revises [Feature 043](../043-llm-stream-stall-recovery/spec.md) FR-001.)*

**Reasoning-Model Tolerance (Problem 1 — reasoning)**

- **FR-002**: The agent MUST apply a longer idle-timeout floor to known reasoning models (models that emit extended reasoning/thinking content before final output, e.g., the DeepSeek family, OpenAI o-series, Claude thinking models). The floor for each reasoning model MUST be large enough to accommodate its measured thinking latency (e.g., DeepSeek family floor ≥ 600s, based on Hermes's measured ~65s first-content-token latency plus safety margin, [commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)).
- **FR-003**: The reasoning-model floor MUST only ever raise the effective idle period above the default; it MUST NOT lower it. The effective idle period for a turn MUST be: the operator's explicit configuration when one is set (honored as-is, even if below the floor); otherwise `max(default, model_floor)` when the model matches the reasoning set; otherwise the default. Non-matching models MUST receive only the default.

**Partial Output Persistence (Problem 2)**

- **FR-004**: When a turn terminates due to a detected stream stall (per [Feature 043](../043-llm-stream-stall-recovery/spec.md) FR-004), the agent's already-streamed partial output (text and/or reasoning blocks emitted to the frontend before the stall) MUST be persisted to the checkpoint so that it is returned by subsequent message-history reads (e.g., `ListMessages`). This compensates for LangGraph's `idleTimeout` discarding buffered writes on abort (`task.writes.splice`, `@langchain/langgraph` `dist/pregel/timeout.js`).
- **FR-005**: The persisted partial output MUST carry an "interrupted" indicator on the specific content block (text or thinking) that was mid-stream at the moment the stall occurred — typically the last block of the partial. Earlier fully-streamed blocks within the same turn MUST NOT be marked (only the truncated block is flagged). The indicator MUST be machine-readable metadata (exact key is a plan-level decision) so the desktop can render a visual "interrupted" marker on that block. The real-time stall notice still uses the existing ⚠ `warn` bubble (FR-012); the per-block flag is what survives reconnection.
- **FR-006**: When persisting partial output, fully-completed tool results that were already dispatched before the stall MUST be retained (they represent side effects that already occurred); incomplete/partial tool calls that were mid-flight MUST NOT be retained as dispatchable calls (they cannot be executed and would corrupt tool history).
- **FR-007**: Partial-output persistence MUST apply to whichever node stalled (the player node and the planner node both stream), writing to that node's per-agent message channel. The persisted partial output MUST be part of the model's conversation history on the next turn (i.e., it lands in the same channel the model reads, such as `playerMessages`), so the model can continue from its truncated reply rather than starting fresh; `ListMessages` naturally returns it for the correct agent partition.

**Configuration**

- **FR-008**: The default idle period (FR-001) and the reasoning-model floor set (FR-002) MUST be configurable. The default MUST work sensibly without explicit configuration; the reasoning-floor set MUST be explicit and auditable (so operators can see which models receive extended tolerance). An operator's explicit idle-period configuration MUST always take precedence over both the default and the floor (per FR-003).

**Scope Boundaries**

- **FR-009**: This feature MUST NOT introduce automatic retry or fallback of a stalled turn. Retry/replay-safety is explicitly out of scope and deferred to a future spec (survey §6.3) — it requires distinguishing model-idle from side-effecting tool execution, a distinct and higher-complexity concern. *(Reaffirms [Feature 043](../043-llm-stream-stall-recovery/spec.md) FR-013.)*
- **FR-010**: This feature MUST NOT change [Feature 043](../043-llm-stream-stall-recovery/spec.md)'s other agent-side behaviors: the tool-execution heartbeat (043 FR-003), the queued-message buffer retention on stall (043 FR-006/FR-007), the `warn` + `wait` recovery signal emission (043 FR-005/FR-008), or the init-turn timeout (043 FR-009/FR-010). It only (a) recalibrates the idle threshold, (b) adds the reasoning floor, and (c) adds partial-output persistence on stall. (FR-012 standardizes the *desktop's rendering* of the already-emitted `warn` and reconciles its contract comment; it does not change the agent's `warn` emission.)
- **FR-011**: This feature MUST NOT alter the existing abort semantics (user-initiated abort clears the buffer per [Feature 030](../030-queued-chat-input/spec.md) FR-011; connection-drop abort per [Feature 026](../026-agent-abort-crash-fix/spec.md)). Partial-output persistence applies ONLY to stall-induced termination, not to abort-induced termination (an abort is an intentional halt; partial output need not be retained on abort).

**Desktop Rendering & Contract**

- **FR-012**: The desktop MUST render `WarnSignal` (FlowPart.warn) consistently as a conversation warning bubble (the current idleTimeout-style ⚠ bubble, `projects/game/desktop/frontend/src/App.svelte:789-802` + `components/ChatView.svelte:271-279`) for all warn sources. This standardizes the stall-recovery notice and reconciles [Feature 023](../023-saolei-mcp-refine/spec.md)'s "FlowParts never rendered as conversation entries" contract statement (`projects/game/game.proto:451-453`) — `warn` is the one control signal that IS surfaced in the conversation as a distinct warning bubble; all other FlowPart kinds remain non-rendered. The proto comment should be updated in the implementation to document `warn` as the rendered exception.
- **FR-013**: The desktop MUST render the per-block "interrupted" indicator (FR-005) on persisted partial output after session reconnection, so the truncation is visible in history — not only during the live stall via the ⚠ `warn` bubble (which is transient and does not survive reconnection).

### Key Entities *(include if feature involves data)*

- **Reasoning-Model Floor**: a per-model lower bound on the stream-stall idle period, applied to models known to emit extended reasoning before content. It only ever raises the effective idle period above the default; an explicit operator configuration always wins (FR-003). Conceptually mirrors Hermes's `_REASONING_STALE_TIMEOUT_FLOORS` ([commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)).
- **Partial Agent Output**: the portion of an agent's reply (text and/or reasoning, plus any fully-completed tool results) that was already streamed to the frontend before a stall terminated the turn. Under 043 it was lost on stall (LangGraph discards buffered writes); under this feature it is persisted to the checkpoint and marked incomplete (FR-004–FR-006). It is persisted into the per-agent message channel, making it part of the model's conversation history on the next turn (FR-007) — the model sees its own truncated reply and can continue from it. The content block that was mid-stream at the stall carries an "interrupted" flag (FR-005); earlier completed blocks in the turn are unmarked. Distinct from the queued *user input* buffer retained by 043 FR-006, and distinct from the transient ⚠ `warn` bubble (a FlowPart that does not survive reconnection).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The stream-stall false-positive rate on reasoning-heavy scenarios drops to near zero — a reasoning model's legitimate thinking phase (silent for longer than the old 30s threshold but within its floor) triggers zero false stalls in 100% of measured turns.
- **SC-002**: In 100% of stall-terminated turns where partial agent output had been streamed, that partial output is present in the message history after session reconnection — zero silently-lost replies.
- **SC-003**: In 100% of stall-terminated turns, the specific content block that was mid-stream at the stall (typically the last block of the partial) is visibly marked as interrupted after reconnection, so the user can tell where the reply was truncated.
- **SC-004**: In 100% of turns using non-reasoning models, a genuine silent-stream dropout (zero events) is still detected within the new default idle period + a small margin (~120s + margin) — real stalls remain caught.
- **SC-005**: The production `saolei` template completes a full game session without a stall-induced interruption caused by the reasoning model's normal thinking time, in the large-test validation.
- **SC-006**: Existing [Feature 043](../043-llm-stream-stall-recovery/spec.md) behaviors (tool-heartbeat no-false-stall, queued-buffer retention, `warn`+`wait` recovery, abort semantics) exhibit zero regressions.

## Assumptions

- This is a **focused follow-up to Feature 043**, delivered as new spec 044 rather than an in-place rewrite of 043's shipped FRs (see Clarifications). Feature 043's architecture (LangGraph `TimeoutPolicy.idleTimeout` at `projects/game/agent/src/team/graph.ts:383-389` + client-side tool heartbeat) is retained unchanged in mechanism; only the threshold value, the reasoning floor, and the partial-output persistence are added.
- The **120s default** is chosen as the industry median (LangChain Python 120s, OpenClaw 120s idle). It trades a longer real-stall detection latency (30s → 120s) for a dramatic reduction in false positives — the correct tradeoff because (a) reasoning-model false positives were the dominant production failure, and (b) true stalls are eventually caught regardless. If faster true-stall detection is later desired, it should come from bounded retry (future spec), not from re-tightening the threshold.
- The **reasoning-model floor set** is maintained as an explicit, auditable list (mirroring Hermes's allowlist pattern). Adding a new reasoning model requires updating the list — this is an accepted, documented maintenance obligation, preferable to either "inflate the default for everyone" or "leave reasoning models broken." The exact maintenance vehicle (hardcoded table vs. config-driven) is a `/speckit.plan` decision.
- An operator's **explicit idle-period configuration always wins**, even when below a model's floor. This intentionally preserves operator control (e.g., an operator may deliberately want aggressive detection for a specific deployment) and matches Hermes's `max(default, floor)` semantics where explicit config is never lowered.
- Persisting partial output requires writing to the checkpoint **after** the stall aborted the in-flight model call. The checkpoint write is an independent operation (not part of the aborted invocation) and is expected to succeed; this must be confirmed during planning (survey §6.4 risk). Memory cost of accumulating one turn's deltas is negligible (well under typical message sizes).
- **Automatic retry/fallback is out of scope** (FR-009), deferred to a future spec. The survey (§6.3) recommends it ship separately because replay-safety — distinguishing a model-idle stall (replay-safe) from a stall during/after side-effecting tool execution (not replay-safe) — is a distinct, higher-complexity concern best designed and validated on its own.
- LangChain JS still lacks a client-layer chunk-idle guard ([langchainjs #9088](https://github.com/langchain-ai/langchainjs/issues/9088)). We continue to rely on LangGraph's `idleTimeout` as the sole chunk-idle defense, now correctly calibrated; adding a self-written `stream_chunk_timeout` in `projects/game/agent/src/model-provider.ts` is a possible long-term hardening but is explicitly out of scope here.
- Production evidence and cross-framework citations are drawn from the survey [`survey/llm-stream-stall-recovery-revision.md`](../../survey/llm-stream-stall-recovery-revision.md) (session `a7cb3d62f0269fa88410093380f79def`, trace `0362ac8cb2a8089011f92dcb539c756a`, env `game.prod`).
- **`warn` (WarnSignal) is a transient FlowPart, not message history.** There is exactly one `warn` mechanism — the `WarnSignal` FlowPart (`projects/game/game.proto:505,644-648`), emitted by the stall-recovery terminal (`projects/game/agent/src/turn-loop.ts:522` `warnFrame()`). The desktop already renders it as a conversation ⚠ bubble (`App.svelte:789-802`, `ChatView.svelte:271-279`); FlowParts are never persisted to message history (`game.proto:741`), so the ⚠ bubble is gone after reconnection. This is why the per-block "interrupted" flag (FR-005) MUST live on the persisted `Message` itself — it is the only signal that survives reconnection. FR-012 formalizes the ⚠-bubble rendering and reconciles [Feature 023](../023-saolei-mcp-refine/spec.md)'s "FlowParts never rendered as conversation entries" comment (the proto comment update is a follow-up task). Note: `warn()` from `@dominion/common-js-logs` is a separate server-side log function (goes to signoz, not the user) — not to be confused with the `WarnSignal`.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Survey & Prior Specification (in-repository)

- `survey/llm-stream-stall-recovery-revision.md` — the research document this spec is based on: production incident analysis, cross-framework timeout survey, partial-output-loss root cause, and revision proposals. All external citations below originate from this survey's evidence index (§8).
- [Feature 043 — LLM Stream Stall Recovery](../043-llm-stream-stall-recovery/spec.md) — the shipped mechanism this spec revises (idle-timeout default, reasoning-floor addition, partial-output persistence). Its FR-001 (default 30s), FR-006/FR-007 (queued-buffer retention), FR-013 (no retry) are directly referenced by this spec's FRs.

### Official Documentation

- [OpenCode Go (中文)](https://opencode.ai/docs/zh-cn/go/) — confirms `deepseek-v4-flash` is served via `/chat/completions` and is a reasoning model (emits `reasoning_content` before `content`), the basis for FR-002.
- [LangGraph — `TimeoutPolicy` (node-level `idleTimeout`)](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/pregel/utils/timeout.ts) — the built-in per-node idle-timeout mechanism (used unchanged from 043); its `task.writes.splice` behavior on expiry is the root cause of partial-output loss (Problem 2).
- [LangChain — `stream_chunk_timeout` reference](https://reference.langchain.com/python/langchain-openai/chat_models/base/BaseChatOpenAI/stream_chunk_timeout) — documents the parsed-chunk-interval timeout concept (120s default in the Python SDK), the industry anchor for FR-001's new default.

### Repositories (cross-framework evidence)

- [langchain-ai/langchain — PR #36949: prevent silent streaming hangs in ChatOpenAI](https://github.com/langchain-ai/langchain/pull/36949) — LangChain Python's 120s `stream_chunk_timeout` default; the industry-median anchor for FR-001.
- [langchain-ai/langchainjs — issue #9088: Streaming inactivity timeout](https://github.com/langchain-ai/langchainjs/issues/9088) — confirms LangChain JS (dominion's runtime) lacks a client-layer chunk-idle guard, so LangGraph's `idleTimeout` remains the sole defense (Assumptions).
- [NousResearch/hermes-agent — commit 27c486e: apply per-reasoning-model stale-timeout floor](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa) — the per-reasoning-model floor pattern (`max(default, floor)`, explicit config never lowered) directly informing FR-002/FR-003.
- [NousResearch/hermes-agent — issue #61461: opencode-go + deepseek-v4-flash still hangs](https://github.com/NousResearch/hermes-agent/issues/61461) — measured **~65s to first content token** for `deepseek-v4-flash`, the empirical basis for the 600s DeepSeek floor (FR-002).
- [openclaw/openclaw — PR #93965: fix(opencode-go) streaming completes when provider ends responses](https://github.com/openclaw/openclaw/pull/93965) — OpenClaw's 120s idle + 300s first-event layered timeout; co-anchor for FR-001's 120s default.
- [anomalyco/opencode — PR #18264: disable chunk timeout by default](https://github.com/anomalyco/opencode/pull/18264) — the reverse evidence that 043's "15–30s community consensus" citation was inaccurate; opencode itself moved the default to disabled because tight chunk timeouts "cause too many small issues across the board."
- [openai/codex — issue #23807: stream_idle_timeout defaults to 300s](https://github.com/openai/codex/issues/23807) — Codex CLI's 300s default, the lenient end of the industry range, confirming 30s is an outlier.

### In-Repository Sources

- `projects/game/agent/src/llm.ts:43-44` — `STREAM_IDLE_TIMEOUT_MS = Number(process.env.GAME_STREAM_IDLE_TIMEOUT_MS) || 30_000`; the constant whose default FR-001 revises to 120s.
- `projects/game/agent/src/team/graph.ts:383-389` — player/planner `addNode({ timeout: { idleTimeout: STREAM_IDLE_TIMEOUT_MS, refreshOn: "auto" } })`; the application point where the reasoning-model floor (FR-002/FR-003) takes effect.
- `projects/game/agent/src/handler.ts:581,619` — `ListMessages` reads `team.getTeamState()` → checkpoint state; the read path that must return persisted partial output (FR-004 verification).
- `projects/game/agent/src/session-team.ts:317` — `getTeamState()` (the checkpoint read backing `ListMessages`); `runTeamTurn` (the streaming loop, survey §8.3) is the partial-output accumulation/persistence site for FR-004–FR-007.
- `projects/game/agent/src/turn-loop.ts:413-424` — `finishError` (retain buffer, emit `warn` + `wait`); the stall-recovery terminal that partial-output persistence cooperates with (re-throw after persist, per survey §6.4).

### Articles & RFCs

- No external articles or RFCs cited beyond the repository-linked issues and PRs above.

### Related Specifications

- [Feature 043 — LLM Stream Stall Recovery](../043-llm-stream-stall-recovery/spec.md) — the parent mechanism; this spec revises its FR-001 (default), reaffirms FR-013 (no retry), and adds the reasoning floor + partial-output persistence as new behavior layered on 043's architecture.
- [Feature 030 — Queued Chat Input During Agent Run](../030-queued-chat-input/spec.md) — defines the queued *user input* buffer retention (043 FR-006). This spec's partial-output persistence is the *agent output* counterpart; the two retentions are independent and additive (Edge Cases).
- [Feature 026 — Agent Abort Crash Fix](../026-agent-abort-crash-fix/spec.md) — defines connection-drop abort. FR-011 confirms partial-output persistence does NOT apply to abort-induced termination.
