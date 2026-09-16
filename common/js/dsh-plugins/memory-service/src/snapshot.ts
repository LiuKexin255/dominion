/**
 * The memory snapshot: the planner's long-term memory rendered as one plain
 * text block for the system prompt, plus the prompt-section identity and the
 * injection-policy constants the loader queries with.
 *
 * Rendering semantics follow spec 039 T017: each entry contributes exactly
 * one line in the order the loader received it, and the service-internal
 * `memory_id` is NEVER rendered — the model locates entries by content
 * through the memory tool's `old_text` substring matching. An empty memory
 * renders as the empty string, which the system-prompt registry drops from
 * the prompt entirely (empty sections do not render), so no placeholder
 * logic is needed.
 *
 * Sorting and truncation are the memory service's job
 * (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §2):
 * `service.ts` loads one page with `SNAPSHOT_ORDER_BY` and
 * `SNAPSHOT_ENTRY_LIMIT`, so the received page already is the most recently
 * updated ≤10 entries in order — the renderer is a pure pass-through and
 * never sorts, truncates, or normalizes.
 *
 * Section identity/order per
 * specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §2 item 2: the
 * function form is evaluated on every assembly with that assembly's
 * `AssembleContext`, whose `scope` is the agent, and the order sits after the
 * tool-guidance band (100–199) as the trailing data layer; the snapshot
 * itself is fixed for the agent's lifetime (filled by the materialization
 * setup before the first assembly).
 */

import type { MemoryEntry } from "./client.js";

/** Section name; unique per agent scope (standing mount, one per planner preset). */
export const MEMORY_SNAPSHOT_SECTION_NAME = "memory:snapshot";

/** Order band 200+: after team facts (1–49) and tool guidance (100–199). */
export const MEMORY_SNAPSHOT_SECTION_ORDER = 200;

/**
 * The AIP-132 `order_by` value the snapshot loader passes to `ListMemories`:
 * update_time descending with memory_id ascending as the tie-break — a
 * deterministic total order, newest first
 * (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1/§2).
 */
export const SNAPSHOT_ORDER_BY = "update_time desc";

/**
 * The page size the snapshot loader passes to `ListMemories`, so the injected
 * snapshot holds the most recently updated entries at most. The limit only
 * bounds the prompt injection: the memory tool keeps operating on the full
 * storage (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md
 * §2).
 */
export const SNAPSHOT_ENTRY_LIMIT = 10;

/**
 * Render the snapshot text: `长期记忆：` header plus one line per entry in the
 * given (service-sorted) order, or the empty string when there are no entries
 * (the section then does not render at all). Pure pass-through — no sorting,
 * truncation, or normalization happens here.
 */
export function renderMemorySnapshot(entries: readonly MemoryEntry[]): string {
  if (entries.length === 0) {
    return "";
  }
  return `长期记忆：\n${entries.map((entry) => entry.content).join("\n")}`;
}
