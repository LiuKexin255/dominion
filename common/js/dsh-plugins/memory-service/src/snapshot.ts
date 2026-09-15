/**
 * The memory snapshot: the planner's long-term memory rendered as one plain
 * text block for the system prompt, plus the prompt-section identity.
 *
 * Rendering semantics follow spec 039 T017: each entry contributes exactly
 * one line and the service-internal `memory_id` is NEVER rendered — the model
 * locates entries by content through the memory tool's `old_text` substring
 * matching. An empty memory renders as the empty string, which the
 * system-prompt registry drops from the prompt entirely (empty sections do
 * not render), so no placeholder logic is needed.
 *
 * Injection policy per
 * specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §2: the
 * entries are ordered by `updateTime` descending (an entry without a
 * timestamp counts as the oldest), ties are broken by `memory_id` ascending
 * so the order never depends on the service's response order, and only the
 * 10 most recently updated entries are injected.
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
 * How many of the most recently updated entries the snapshot injects
 * (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §2
 * item 2). The truncation only limits the prompt injection: the memory tool
 * keeps operating on the full storage.
 */
const SNAPSHOT_ENTRY_LIMIT = 10;

/**
 * Order two entries by recency: `updateTime` descending with `undefined`
 * (no parseable timestamp) last, ties broken by `memory_id` ascending for a
 * deterministic order.
 */
function byRecency(a: MemoryEntry, b: MemoryEntry): number {
  if (a.updateTime !== b.updateTime) {
    if (a.updateTime === undefined) {
      return 1;
    }
    if (b.updateTime === undefined) {
      return -1;
    }
    return b.updateTime - a.updateTime;
  }
  if (a.memory_id === b.memory_id) {
    return 0;
  }
  return a.memory_id < b.memory_id ? -1 : 1;
}

/**
 * Render the snapshot text: `长期记忆：` header plus one line per entry, or
 * the empty string when there are no entries (the section then does not
 * render at all). Entries without a parseable `updateTime` sort as the
 * oldest; the snapshot injects the 10 most recent entries only.
 */
export function renderMemorySnapshot(entries: readonly MemoryEntry[]): string {
  if (entries.length === 0) {
    return "";
  }
  const recent = [...entries].sort(byRecency).slice(0, SNAPSHOT_ENTRY_LIMIT);
  return `长期记忆：\n${recent.map((entry) => entry.content).join("\n")}`;
}
