/**
 * Streaming merge helpers for the desktop chat view — agent bubble
 * continuity (US2, specs/038-queue-input-mid-turn/spec.md FR-005/FR-006).
 *
 * `App.svelte:handleMessageParts` folds consecutive agent text/thinking
 * chunks into a single bubble. When a queued user message (optimistic
 * insertion) interleaves between chunks, the merge target is found by
 * scanning backwards past USER entries (specs/038-queue-input-mid-turn/
 * data-model.md §5; research.md D3). Pure module — no Svelte imports — so it
 * is unit-testable without a Svelte harness (style/javascript.md §测试).
 */
import { MessageRole } from './api'
import { trimFifo } from './chat-fifo'

/**
 * One display part within a StreamEntry — the text/thinking subset of
 * api.ts MessagePart consumed by the streaming merge.
 */
export interface StreamPart {
  text?: { content: string }
  thinking?: { content: string }
}

/**
 * Structural subset of App.svelte's ChatEntry consumed by the streaming
 * merge. ChatEntry is a superset (messageId/timestamp/warnMessage and
 * image/toolCall/toolResult parts), which makes ChatEntry[] directly
 * assignable here; entries without a merge candidate (warn bubbles) simply
 * never match.
 */
export interface StreamEntry {
  role: MessageRole
  agent?: string
  mergeKind?: 'text' | 'thinking' | 'mixed'
  parts?: StreamPart[]
}

/**
 * Finds the index of the entry a streaming text/thinking chunk should merge
 * into: the last AGENT entry with matching `agent` and `mergeKind` reachable
 * from the end of `list`, skipping interleaved USER entries
 * (specs/038-queue-input-mid-turn/data-model.md §5). Scanning stops — and
 * null is returned — at the first non-USER entry that does not match (e.g. a
 * warn bubble breaks the streaming chain, research.md D3). An AGENT entry
 * that matches but carries no parts yet is skipped so an earlier populated
 * entry can still be found.
 */
export function findMergeTarget<T extends StreamEntry>(
  list: T[],
  agent: string,
  kind: 'text' | 'thinking',
): number | null {
  for (let i = list.length - 1; i >= 0; i--) {
    const entry = list[i]
    if (entry.role === MessageRole.AGENT && entry.agent === agent && entry.mergeKind === kind) {
      if (entry.parts && entry.parts.length > 0) return i
      continue
    }
    if (entry.role !== MessageRole.USER) return null
  }
  return null
}

/**
 * Returns a NEW array with `incomingParts` content concatenated onto the
 * trailing text/thinking part of the entry at `index` (`kind` selects the
 * part variant), then trimmed by the per-agent FIFO cap. The input array,
 * its entries and its parts are not mutated.
 */
export function appendToEntry<T extends StreamEntry>(
  list: T[],
  index: number,
  incomingParts: StreamPart[],
  kind: 'text' | 'thinking',
): T[] {
  const next = [...list]
  const target = { ...next[index], parts: [...(next[index].parts ?? [])] }
  const trailing = { ...target.parts[target.parts.length - 1] }
  const joined = incomingParts
    .map(p => (kind === 'text' ? p.text?.content : p.thinking?.content) ?? '')
    .join('')
  if (kind === 'text' && trailing.text) {
    trailing.text = { content: trailing.text.content + joined }
  } else if (kind === 'thinking' && trailing.thinking) {
    trailing.thinking = { content: trailing.thinking.content + joined }
  }
  target.parts[target.parts.length - 1] = trailing
  next[index] = target
  return trimFifo(next)
}
