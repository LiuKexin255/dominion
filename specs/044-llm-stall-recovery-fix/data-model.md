# Data Model: LLM Stream Stall Recovery — Timeout Tuning & Partial Output Persistence

**Feature**: 044-llm-stall-recovery-fix | **Date**: 2026-08-12 | **Spec**: [spec.md](spec.md)

This document defines the entities, data shapes, and state transitions introduced or modified by [Feature 044](spec.md). It complements [Feature 043's data model](../043-llm-stream-stall-recovery/data-model.md) (which is unchanged) and the contracts in [contracts/](contracts/).

---

## 1. Effective Stream Idle Timeout (computed)

The chunk-idle timeout applied to a model-holding node (`player`/`planner`), resolved at graph-build time.

**Resolution rule** (spec FR-001/FR-003, contracts/idle-timeout-contract.md §1):

```
STREAM_IDLE_TIMEOUT_MS = Number(process.env.GAME_STREAM_IDLE_TIMEOUT_MS) >= 60_000
    ? Number(process.env.GAME_STREAM_IDLE_TIMEOUT_MS) : 120_000   // was 30_000; values < 60_000 clamped
effectiveIdleTimeout(modelSpec?) =
    operator set env?                  → the env value (honored as-is, even below a floor)
    : modelSpec matches a floor?       → max(STREAM_IDLE_TIMEOUT_MS, floor)
    : otherwise                        → STREAM_IDLE_TIMEOUT_MS
```

| Field | Type | Source | Notes |
|---|---|---|---|
| `STREAM_IDLE_TIMEOUT_MS` | `number` (ms) | `projects/game/agent/src/llm.ts:56-58` | Default **120_000**; env `GAME_STREAM_IDLE_TIMEOUT_MS` overrides. Min configurable **60_000** (enforced where read). |
| `effectiveIdleTimeout` | `number` (ms) | computed via `resolveStreamIdleTimeout(modelSpec)` in `reasoning-timeouts.ts` | Applied per-node at `team/graph.ts:383-389` `addNode({ timeout: { idleTimeout }})`. |

**Invariants**:
- The floor **only ever raises** the effective timeout above the default; it never lowers it.
- An explicit operator env var is the source of `STREAM_IDLE_TIMEOUT_MS`, so it always wins (even below a floor) — the floor never overrides an explicit operator choice.
- The effective timeout applies **only** to `player`/`planner` nodes (NOT `initInstruction`/`postCompactInstruction`/`compress` — unchanged from 043, `graph.ts:373-376`).

**实现注记**：`resolveStreamIdleTimeout` 需感知 env 是否显式设置（`llm.ts` 导出 `STREAM_IDLE_TIMEOUT_EXPLICIT` 标志）——裸 `max(env_or_default, floor)` 会把显式设置的低值（如 env=90s + DeepSeek 600s floor）抬到 floor，违反 FR-003（见 research.md R2 与 tasks.md T003）。

---

## 2. Reasoning-Model Floor (allowlist entity)

An explicit, auditable mapping of reasoning-model substrings to a minimum idle-timeout floor.

**Shape** (`projects/game/agent/src/reasoning-timeouts.ts`):

```typescript
REASONING_IDLE_TIMEOUT_FLOOR: ReadonlyArray<readonly [substring: string, floorMs: number]>
```

| Substring (matched against bare model name) | Floor (ms) | Basis |
|---|---|---|
| `deepseek-r1` | 600_000 | Hermes measured ~65s first-content-token for `deepseek-v4-flash` ([hermes#61461](https://github.com/NousResearch/hermes-agent/issues/61461)); 600s is the safety margin Hermes uses for the DeepSeek family ([commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)). |
| `deepseek-reasoner` | 600_000 | same |
| `deepseek-v4-` | 600_000 | same — the production model |
| `o1-` | 600_000 | OpenAI o-series reasoning |
| `o3-` | 600_000 | same |
| `o3-mini-` | 300_000 | lighter reasoning（与 `o4-mini-` 同档；longest-first 使其先于 `o3-` 匹配） |
| `o4-mini-` | 300_000 | lighter reasoning |
| `claude-opus-` | 240_000 | Claude thinking models |

*(Initial set — 值与 tasks.md T003 冻结值一致（本表与 T003 互为一致；新增模型 = 两处同步追加 + 单测）。Non-matching models → no floor, default applies.)*

**Matching semantics** (`getReasoningIdleTimeoutFloor(modelSpec)`):
1. Strip `{provider}/` prefix via `parseModelSpec` (`model-provider.ts:26-32`).
2. Lowercase the bare name.
3. **Longest-substring-first** match (sort entries by `substring.length` descending) — so `o3-mini-` is tested before `o3-`, preventing `o1` from matching `olmo-1` (a Hermes-documented pitfall).
4. Return the first match's `floorMs`, or `null` if none match.

**Lifecycle**: static code table. Adding a reasoning model = append a row + unit test. (Externalizing to config is a deferred option — see research.md R2.)

---

## 3. Partial Agent Output (persisted on stall)

The already-streamed output of the stalled node, reconstructed from accumulated `TurnBlock`s and written to the checkpoint so it survives reconnection.

### 3.1 Source — accumulated `TurnBlock`s

`TurnBlock` (`projects/game/agent/src/turn-loop.ts:91`): `{ agent: string; block: ContentBlock }`. `ContentBlock` (`llm.ts:72-91`): a discriminated union of `reasoning` / `text` / `tool_call` / `tool_result`.

During a turn, `runTeamTurn` (`session-team.ts:725-912`) yields these as deltas. For 044, each yielded block is **also** pushed (shallow-cloned) into a `partialBlocks: TurnBlock[]` accumulator.

### 3.2 Merge — `TurnBlock[]` → checkpoint messages

`mergePartialBlocks(blocks: TurnBlock[]): { aiMessage: AIMessage; toolMessages: ToolMessage[] }` (new helper in `session-team.ts`). Rules (spec FR-004/FR-006, contracts/partial-output-contract.md §3):

| Input block(s) | Output | Rule |
|---|---|---|
| `text` blocks (one or more) | AIMessage `content[]` entry `{ type: "text", text: <concatenated> }` | Concatenate in stream order. If this is the interrupted tail (last block overall), attach `additional_kwargs: { interrupted: true }` (§4). |
| `reasoning` blocks | AIMessage `content[]` entry `{ type: "reasoning", reasoning: <concatenated> }` | Same; mark interrupted if it is the tail. |
| `tool_call` **with** a retained `tool_result` | AIMessage `tool_calls[]` entry `{ id, name, args }` | Complete call → retained (history shows call + result, linked by `tool_id`). |
| `tool_call` **without** a `tool_result` | (dropped) | Mid-flight partial call — cannot dispatch, would corrupt tool history (spec FR-006). |
| `tool_result` blocks | standalone `ToolMessage` each | Side effect already executed on the desktop → retained. Carries `tool_call_id`, content (message/screenshot), `additional_kwargs.toolResultStatus`. |

**Resulting AIMessage shape** (round-trips through `ListMessages` identically to a normal AI reply — reconstruction at `handler.ts:668-717`):
- `content`: array of `{type:"reasoning"}` and/or `{type:"text"}` blocks (the interrupted tail block carries the flag).
- `tool_calls`: only complete calls.
- a fresh `id` (so `messagesStateReducer` appends without colliding).

### 3.3 Partitioning — only the stalled node

Filtered by `NodeTimeoutError.node` (research.md R7): only blocks where `block.agent === stalledAgent` are merged. Written to `stalledAgent === "player" ? "playerMessages" : "plannerMessages"` (matches `AGENT_CHANNELS`, `handler.ts:82-83`). Prior completed nodes' output is already checkpointed — not duplicated.

### 3.4 State transition (stall → checkpoint consistent)

| Step | Frontend (live) | Checkpoint (`playerMessages`/`plannerMessages`) |
|---|---|---|
| user message | sent | `[HumanMessage]` (normal) |
| agent streams partial output | sees deltas (live TeamFrames) | (not yet — createAgent internal accumulate) |
| **idleTimeout fires** | stream breaks; ⚠ warn bubble; `wait` | `task.writes.splice` discards stalled node's writes |
| **`persistPartialOutput`** (NEW) | — | `updateState` appends `[AIMessage(partial, interrupted), ...ToolMessages]` to the stalled node's channel |
| user "继续游戏" (buffer retained, 043 FR-006) | queued | buffer retained |
| next turn drains | "继续游戏" as input | model sees `[Human, AIMessage(partial, interrupted), Human("继续游戏")]` → continuity (spec Clarifications Q1) |
| **reconnect → `ListMessages`** | sees partial output (with interrupted marker) + continuation | partial output present — **not lost** (the bug fix) |

---

## 4. "Interrupted" Flag (per-content-block marker)

A machine-readable marker on the **specific content block** that was mid-stream when the stall occurred (typically the last block of the partial). Survives reconnection (unlike the transient ⚠ warn bubble).

**Carrier** (research.md R5): `additional_kwargs.interrupted = true` on the AIMessage content-block array element:

```typescript
// example interrupted tail text block
{ type: "text", text: "…cut off mid-sentence", additional_kwargs: { interrupted: true } }
```

**Propagation through `ListMessages`**: the reconstruction (`handler.ts:668-717`) is extended minimally — when emitting the `MessagePart` for a content block that carries `additional_kwargs.interrupted`, it sets an `interrupted: true` marker on the emitted part (text → `{ text: { content, interrupted } }`, reasoning → `{ thinking: { content, interrupted } }`). A normal complete AIMessage has no such marker.

**Semantics**:
- Marks ONLY the interrupted block — earlier completed blocks in the same partial are unmarked (spec FR-005, Session 2026-08-12 clarification).
- The desktop renders a visual "中断"/truncated indicator on the flagged part (FR-013).
- Distinct from the transient ⚠ `warn` bubble (FR-012), which is live-only.

*(Exact field name finalized in tasks.md; `additional_kwargs` is consistent with how `toolResultStatus` is already carried — `llm.ts:428-435`.)*

---

## 5. Desktop rendering state (additive)

No new persisted entities on the desktop. Two rendering behaviors are formalized:

| Behavior | Trigger | Rendering | Source |
|---|---|---|---|
| ⚠ warn bubble (FR-012) | `FlowPart.warn` (WarnSignal) on the live Connect stream | `.msg-warn` / `.warn-bubble` with ⚠ icon (existing) | `App.svelte:789-802`, `ChatView.svelte:271-279` |
| interrupted indicator (FR-013) | a `MessagePart` from `ListMessages` carrying the interrupted marker | a visual truncated/中断 indicator on that bubble (new, additive) | `ChatView.svelte` render branch + history-seed path in `App.svelte` |

The ⚠ warn bubble is **transient** (FlowParts never persist to history — `game.proto:741`); the interrupted indicator **survives reconnect** (it rides on the persisted `Message`).

---

## 6. Unchanged entities (from 043 / earlier features)

These are referenced but NOT modified by 044 (spec FR-010):
- `MemorySaver` checkpointer, `playerMessages`/`plannerMessages` channels, `messagesStateReducer` — unchanged.
- `TurnLoop` buffer + `finishError`/`finishAbort` terminals — unchanged.
- `withIdleHeartbeat` tool wrapper (`llm.ts:302-322`), `TOOL_HEARTBEAT_INTERVAL_MS` — unchanged.
- `INIT_TURN_TIMEOUT_MS` / `runInitTurn` total timeout — unchanged.
- `NodeTimeoutError` re-throw in `player.ts`/`planner.ts` — unchanged.
- `FlowPart` / `WarnSignal` / `MessagePart` proto messages — unchanged (only the `game.proto:451-453` **comment** is reconciled, FR-012).
