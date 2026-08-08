---
name: memory
description: Guides the planner on how and when to use the memory tool (action/content/old_text/operations) to maintain its own long-term review memory, and on the frozen-snapshot model. Use this skill when the session profile has the memory MCP enabled and the planner must record, update, or remove a long-term memory entry.
compatibility: opencode
metadata:
  audience: dominion
  scope: memory-mcp
---

# memory

This skill guides the planner (the reviewing agent) in maintaining its own **long-term review memory** through the single `memory` MCP tool. The memory tool is hermes-style: one tool, actions `add` / `replace` / `remove`, located by short `old_text` substrings — there is **no `memory_id` argument** and **no separate memory_add/update/remove tools**. The memory lives in the memory service behind the agent and is presented to you as a **frozen snapshot** (see "Frozen snapshot model" below).

Authority: `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md` (tool surface + old_text semantics), `specs/039-planner-memory-calibration/contracts/memory-skill-contract.md` (this skill), hermes memory tool guidance (https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py).

## Tool usage (HOW)

A single `memory` tool with two mutually exclusive forms:

- **Single operation** — `action` (`add` | `replace` | `remove`) plus:
  - `add(content)`: record a **new** entry. The agent generates the internal id for you — you never provide or see one.
  - `replace(old_text, content)`: locate an existing entry with a **short unique substring** `old_text` and replace its text with `content`.
  - `remove(old_text)`: locate an existing entry with a short substring and delete it.
- **Batch** — `operations: [{action, content?, old_text?}, ...]`: apply several operations in ONE call, all-or-nothing (a failing op aborts the whole batch).

You do NOT need the full entry text or any id to update/delete: a short substring is enough.

### old_text matching and self-correction

`old_text` matches entries by **case-sensitive substring containment**. Matching is deliberately strict, and the tool helps you recover:

- **0 hits** → the tool returns an error listing the **current entries**; pick a substring that actually appears in the entry you meant.
- **Multiple DISTINCT entries hit** → the tool returns an error with the matched previews; provide a **more specific substring**.
- Entries with identical content count once (the first one is acted on).

### Tool results

Every call returns one text result. Success: `memory added` / `memory replaced` / `memory removed` / `memory: applied N operation(s)`. Failures are returned as **text** (never a tool crash): read the returned current entries and retry with a corrected call — often in the same turn.

## When to record (WHEN)

Record **after a review**, proactively, the insights that accumulate across games. The planner's review memory is where your cross-game calibration lives. Prioritize:

1. **Repeated player error patterns** — e.g. "player 在边角频繁误标地雷" (the same class of mistake across games).
2. **Strategies/techniques proven effective** — e.g. "开局先点中心区域效率更高".
3. **Your own review-methodology evolution** — e.g. "评估 player 应优先看操作次数与正确标记比".

Priority order: repeated patterns > validated strategies > methodology.

## What to skip (SKIP)

Do NOT store:

- **Single-game one-off mistakes** — an isolated error that does not form a pattern.
- **Board/game transients** — specific coordinates or states (e.g. "(3,5) 是地雷"): game-specific, no cross-game value.
- **Easily re-derivable facts** — general minesweeper rules.
- **Instruction content** — instructions sent to the player live in `playerMessages`, NOT in planner long-term memory. The two channels have distinct jobs: memory = your evolving calibration; playerMessages = recent per-game guidance.

## Frozen snapshot model (important)

Your system prompt's long-term memory is a **frozen snapshot**: baked once and kept unchanged across many reviews.

- When you `add`/`replace`/`remove`, the change is **persisted immediately**, but it will **NOT appear in your system prompt right away** — the snapshot refreshes only at the **compression boundary** (every 5 games).
- Therefore: **do not re-add an insight just because it is not in the current snapshot** — it is already saved.
- Tool results always reflect the **live** state (success/failure/current entries), so use them as feedback for what you just did.

## Writing style

Each entry should be **compact and information-dense**, reusable across games — not a running log. Prefer concrete, actionable calibration ("player 倾向于在数字格附近过度标记，应在复盘指令中强调先 deduce 再 flag") over vague notes. When entries grow stale or overlap, consolidate them with `replace`/`remove` (or one atomic batch) rather than accumulating clutter.
