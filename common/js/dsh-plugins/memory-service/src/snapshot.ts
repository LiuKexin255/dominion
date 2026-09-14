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
 * Render the snapshot text: `长期记忆：` header plus one line per entry, or
 * the empty string when there are no entries (the section then does not
 * render at all).
 */
export function renderMemorySnapshot(entries: readonly MemoryEntry[]): string {
  if (entries.length === 0) {
    return "";
  }
  return `长期记忆：\n${entries.map((entry) => entry.content).join("\n")}`;
}
