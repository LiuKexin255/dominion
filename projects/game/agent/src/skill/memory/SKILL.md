---
name: memory
description: Guides an agent on how and when to use the memory tool (action/content/old_text/operations) to maintain its own long-term memory, and on the frozen-snapshot model. Use this skill when the memory MCP is enabled and the agent must record, update, or remove a long-term memory entry.
compatibility: opencode
metadata:
  audience: dominion
  scope: memory-mcp
---

# memory

This skill teaches you how and when to use the `memory` MCP tool to maintain your own **long-term memory**. The tool is hermes-style: a single tool with actions `add` / `replace` / `remove`, entries located by short `old_text` substrings — there is **no `memory_id` argument** and **no separate memory_add/update/remove tools**. Memory is persisted in the memory service and injected into every future turn, so keep entries **compact and high-signal**. It reaches your system prompt as a **frozen snapshot** (see "Frozen snapshot model" below).

Authority: `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md` (tool surface + old_text semantics), `specs/039-planner-memory-calibration/contracts/memory-skill-contract.md` (this skill), hermes memory tool guidance (https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py).

## Tool usage (HOW)

A single `memory` tool with two mutually exclusive forms:

- **Single operation** — `action` (`add` | `replace` | `remove`) plus:
  - `add(content)`: record a **new** entry. The agent generates the internal id for you — you never provide or see one.
  - `replace(old_text, content)`: locate an existing entry with a **short unique substring** `old_text` and replace its text with `content`.
  - `remove(old_text)`: locate an existing entry with a short substring and delete it.
- **Batch** — `operations: [{action, content?, old_text?}, ...]`: apply several operations in ONE call, all-or-nothing (a failing op aborts the whole batch). Prefer the batch when you make multiple changes — a single call can remove/replace stale entries to make room AND add new ones; the batch is atomic, so don't repeat it.

You do NOT need the full entry text or any id to update/delete: a short substring is enough. Use the bare `action`/`content`/`old_text` fields only for a single lone change.

### old_text matching and self-correction

`old_text` matches entries by **case-sensitive substring containment**. Matching is deliberately strict, and the tool helps you recover:

- **0 hits** → the tool returns an error listing the **current entries**; pick a substring that actually appears in the entry you meant.
- **Multiple DISTINCT entries hit** → the tool returns an error with the matched previews; provide a **more specific substring**.
- Entries with identical content count once (the first one is acted on).

### Tool results

Every call returns one text result. Success: `memory added` / `memory replaced` / `memory removed` / `memory: applied N operation(s)`. Failures are returned as **text** (never a tool crash): read the returned current entries and retry with a corrected call — often in the same turn.

## When to record (WHEN)

Save **proactively** the insights that accumulate across sessions — the durable calibration you would otherwise have to re-derive from scratch. Prioritize:

1. **Repeated error patterns** in the agent you oversee — the same class of mistake recurring across sessions.
2. **Strategies/techniques proven effective** — validated guidance worth reusing.
3. **Evolution of your own review methodology** — how you evaluate the agent you oversee and what to look for.

The best memory stops the agent you oversee repeating past mistakes — and stops you from re-evaluating the same ground every session.

## What to skip (SKIP)

Do NOT store:

- **One-off mistakes** — an isolated error that does not form a pattern.
- **Session-specific transients** — states that hold only for one session and carry no cross-session value (specific positions, ephemeral states, one-off events).
- **Easily re-derivable facts** — general rules you can re-derive instantly.
- **Task progress / completed-work logs** — what happened, step by step.
- **Instruction content** — guidance you send to the agent you oversee lives in its conversation channel, NOT in long-term memory. The two channels have distinct jobs: memory = your evolving calibration; the conversation = recent guidance.

## Frozen snapshot model (important)

Your system prompt's long-term memory is a **frozen snapshot**: baked once and kept unchanged across many sessions.

- When you `add`/`replace`/`remove`, the change is **persisted immediately**, but it will **NOT appear in your system prompt right away** — the snapshot refreshes only at the **compression boundary**.
- Therefore: **do not re-add an insight just because it is not in the current snapshot** — it is already saved.
- Tool results always reflect the **live** state (success/failure/current entries), so use them as feedback for what you just did.

## Writing style

Each entry should be **compact and information-dense**, reusable across sessions — not a running log. Prefer concrete, actionable notes over vague ones. When entries grow stale or overlap, consolidate them with `replace`/`remove` (or one atomic batch) rather than accumulating clutter.
