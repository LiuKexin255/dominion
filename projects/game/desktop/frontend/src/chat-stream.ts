// Chat push stream helpers — SSE (Server-Sent Events) delivery for chat dialog
// messages. Pure, testable-by-inspection functions for chunk reassembly and
// reconnect-safe dedup, plus an EventSource factory that wires chat/chunk
// handlers. Replaces the framework host→webview `game:frame` event channel so
// delivery survives desktop-window focus changes (spec 016).

import type { AgentFrame } from './api'

// AgentFrameJson is the JSON wire form of AgentFrame (camelCase, flattened
// oneof, base64 bytes) — the exact shape `frameToMap`/`protoToJSONMap` already
// produce. Kept as a distinct alias so the SSE/reassembly layer stays decoupled
// from value imports; it is structurally identical to AgentFrame.
export type AgentFrameJson = AgentFrame

// One piece of a chunked large event (spec 016 §4.2). The whole group shares a
// single logical event id; the SSE `id:` line is emitted ONLY on the final
// piece, so a drop before it leaves Last-Event-ID un-advanced and reconnect
// replays the whole group.
export interface ChunkEnvelope {
  groupId: string
  index: number
  total: number
  fragment: string
}

// Tracks one in-progress chunk group during reassembly.
export interface ChunkState {
  total: number
  fragments: (string | null)[]
  received: number
}

// Reconnect-safe dedup tracker. `seenId` holds SSE event ids already applied
// (C11); `seenGroup` holds completed chunk-group ids. Both are consulted on the
// final chunk of a group so a partially-replayed group cannot duplicate.
export interface Deduper {
  readonly seenId: Set<string>
  readonly seenGroup: Set<string>
  // C11 chat-event dedup. Returns true when the event id is new and the frame
  // should be applied; marks it seen either way.
  checkChat(eventId: string): boolean
  // C11 chunk-group dedup (final chunk only). Returns true when neither the
  // event id nor the group id has been applied; marks both seen either way.
  checkGroup(eventId: string, groupId: string): boolean
  // Reset all dedup state (fresh session entry).
  reset(): void
}

export interface ChatEventHandlers {
  onFrame: (frame: AgentFrameJson) => void
  onOpen?: () => void
  onError?: (err: Event) => void
}

// reassembleChunk buffers one chunk fragment by groupId. When the final piece
// arrives (received === total) it concatenates fragments in index order, parses
// the JSON, and returns { complete }. Returns undefined while the group is
// still partial. The entry is evicted from `state` once complete or on a parse
// failure so a corrupt group does not leak. Duplicate fragments (index already
// filled) are ignored, making reassembly idempotent under reconnect replay.
export function reassembleChunk(
  state: Map<string, ChunkState>,
  chunk: ChunkEnvelope,
): { complete?: AgentFrameJson } | undefined {
  if (chunk.total < 1 || chunk.index < 0 || chunk.index >= chunk.total) return undefined
  let entry = state.get(chunk.groupId)
  if (!entry) {
    entry = {
      total: chunk.total,
      fragments: new Array(chunk.total).fill(null),
      received: 0,
    }
    state.set(chunk.groupId, entry)
  }
  if (entry.fragments[chunk.index] != null) {
    // Duplicate fragment (reconnect replay) — ignore.
    return undefined
  }
  entry.fragments[chunk.index] = chunk.fragment
  entry.received++
  if (entry.received < entry.total) return undefined
  state.delete(chunk.groupId)
  const full = entry.fragments.join('')
  try {
    return { complete: JSON.parse(full) as AgentFrameJson }
  } catch {
    // Corrupt group — already evicted; nothing to deliver.
    return undefined
  }
}

// makeDeduper creates a reconnect-safe dedup tracker.
export function makeDeduper(): Deduper {
  const seenId = new Set<string>()
  const seenGroup = new Set<string>()
  return {
    seenId,
    seenGroup,
    checkChat(eventId: string): boolean {
      if (seenId.has(eventId)) return false
      seenId.add(eventId)
      return true
    },
    checkGroup(eventId: string, groupId: string): boolean {
      if (seenId.has(eventId) || seenGroup.has(groupId)) return false
      seenId.add(eventId)
      seenGroup.add(groupId)
      return true
    },
    reset(): void {
      seenId.clear()
      seenGroup.clear()
    },
  }
}

// openChatEventSource creates an EventSource against the chat push endpoint
// with session+token as query params (EventSource cannot set headers — R7), and
// wires `chat`/`chunk` handlers with reconnect-safe dedup (C11) and chunk
// reassembly. Non-final chunks (index < total-1) never touch the id dedup:
// their lastEventId equals the PREVIOUS event's id and must be ignored (R8/F9).
// The token is never logged anywhere.
export function openChatEventSource(
  endpoint: string,
  sessionID: string,
  token: string,
  chunkState: Map<string, ChunkState>,
  deduper: Deduper,
  handlers: ChatEventHandlers,
): EventSource {
  const url =
    endpoint +
    '?session=' + encodeURIComponent(sessionID) +
    '&token=' + encodeURIComponent(token)
  const es = new EventSource(url)

  es.addEventListener('open', () => {
    handlers.onOpen?.()
  })

  es.addEventListener('chat', (event: MessageEvent) => {
    // C11: discard already-applied event ids; otherwise mark and deliver.
    if (!deduper.checkChat(event.lastEventId)) return
    let frame: AgentFrameJson
    try {
      frame = JSON.parse(event.data) as AgentFrameJson
    } catch {
      return
    }
    handlers.onFrame(frame)
  })

  es.addEventListener('chunk', (event: MessageEvent) => {
    let chunk: ChunkEnvelope
    try {
      chunk = JSON.parse(event.data) as ChunkEnvelope
    } catch {
      return
    }
    const isFinal = chunk.index === chunk.total - 1
    if (!isFinal) {
      // R8/F9: non-final chunk — do NOT consult the id dedup. Its lastEventId
      // is the previous event's id, not this group's. Buffer only.
      reassembleChunk(chunkState, chunk)
      return
    }
    // Final chunk: complete reassembly, then C11 dedup on the group.
    const result = reassembleChunk(chunkState, chunk)
    if (!result?.complete) return
    if (!deduper.checkGroup(event.lastEventId, chunk.groupId)) return
    handlers.onFrame(result.complete)
  })

  es.addEventListener('error', (err: Event) => {
    handlers.onError?.(err)
  })

  return es
}
