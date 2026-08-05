/**
 * Per-agent chat display cap for the desktop session chat view.
 *
 * US4 (specs/037-saolei-team-optimize/spec.md FR-020..FR-025): each agent tab
 * keeps at most this many rendered chat entries; overflow is evicted
 * oldest-first (FIFO). Compression (FR-023) MUST NOT explicitly clear local
 * history — the summary arrives as a new message and stale pre-compression
 * entries roll off naturally via the same FIFO. Kept as a pure module so it
 * can be unit-tested without a Svelte harness (style/javascript.md — testable
 * seams over module-level mocks).
 */
export const MAX_CHAT_ENTRIES_PER_AGENT = 200

/**
 * Trims `entries` to its newest `max` items, dropping the oldest when over
 * cap (data-model.md §7): FIFO eviction of the oldest messages only, unified
 * across live-stream and history-loaded sources (FR-025). At-cap/under-cap
 * arrays are returned unchanged (same reference, no copy).
 */
export function trimFifo<T>(entries: T[], max: number = MAX_CHAT_ENTRIES_PER_AGENT): T[] {
  return entries.length > max ? entries.slice(-max) : entries
}
