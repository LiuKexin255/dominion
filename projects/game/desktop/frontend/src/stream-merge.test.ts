import { describe, expect, it } from 'vitest'
import { MessageRole } from './api'
import { appendToEntry, findMergeTarget } from './stream-merge'
import type { StreamEntry, StreamPart } from './stream-merge'

// US2 bubble-continuity tests (specs/038-queue-input-mid-turn/spec.md
// FR-005/FR-006, data-model.md §5): the streaming merge must find its target
// past interleaved USER entries so a queued user message never splits the
// agent's bubble. Pure module — no Svelte harness needed
// (style/javascript.md §测试).

function agentEntry(agent: string, mergeKind: 'text' | 'thinking', parts: StreamPart[]): StreamEntry {
  return { role: MessageRole.AGENT, agent, mergeKind, parts }
}

function userEntry(): StreamEntry {
  return { role: MessageRole.USER, parts: [] }
}

// A warn control-signal bubble: role AGENT but no agent/mergeKind/parts
// (App.svelte renders it from warnMessage only — breaks the streaming chain).
function warnEntry(): StreamEntry {
  return { role: MessageRole.AGENT, parts: [] }
}

describe('findMergeTarget', () => {
  it('merges a text chunk into the trailing same-agent text entry (no USER interleaved)', () => {
    const list = [agentEntry('player', 'text', [{ text: { content: 'hello' } }])]
    expect(findMergeTarget(list, 'player', 'text')).toBe(0)
  })

  it('merges a thinking chunk past a USER entry into the earlier agent thinking entry — bubble NOT split', () => {
    const list = [
      agentEntry('player', 'thinking', [{ thinking: { content: 'step 1' } }]),
      userEntry(),
    ]
    expect(findMergeTarget(list, 'player', 'thinking')).toBe(0)
  })

  it('skips multiple consecutive USER entries and still finds the agent entry', () => {
    const list = [
      agentEntry('player', 'text', [{ text: { content: 'a' } }]),
      userEntry(),
      userEntry(),
      userEntry(),
    ]
    expect(findMergeTarget(list, 'player', 'text')).toBe(0)
  })

  it('breaks on a non-USER entry that does not match (warn bubble) — new entry', () => {
    const list = [
      agentEntry('player', 'thinking', [{ thinking: { content: 'x' } }]),
      userEntry(),
      warnEntry(),
    ]
    expect(findMergeTarget(list, 'player', 'thinking')).toBeNull()
  })

  it('breaks on an AGENT entry with a different mergeKind even when the agent matches', () => {
    const list: StreamEntry[] = [
      agentEntry('player', 'thinking', [{ thinking: { content: 'x' } }]),
      { role: MessageRole.AGENT, agent: 'player', mergeKind: 'mixed', parts: [{ text: { content: 'warn' } }] },
    ]
    expect(findMergeTarget(list, 'player', 'thinking')).toBeNull()
  })

  it('returns null when no entry matches the agent', () => {
    const list = [agentEntry('player', 'text', [{ text: { content: 'a' } }])]
    expect(findMergeTarget(list, 'other-agent', 'text')).toBeNull()
  })

  it('skips an agent entry with empty parts and still finds the earlier match', () => {
    const list = [
      agentEntry('player', 'thinking', [{ thinking: { content: 'a' } }]),
      agentEntry('player', 'thinking', []),
    ]
    expect(findMergeTarget(list, 'player', 'thinking')).toBe(0)
  })

  it('returns null for an empty list', () => {
    expect(findMergeTarget([], 'player', 'text')).toBeNull()
  })
})

describe('appendToEntry', () => {
  it('concatenates text content onto the trailing text part and returns a new array', () => {
    const list = [agentEntry('player', 'text', [{ text: { content: 'hello' } }])]
    const next = appendToEntry(list, 0, [{ text: { content: ' world' } }], 'text')
    expect(next).not.toBe(list)
    expect(next[0].parts?.[0].text?.content).toBe('hello world')
    expect(list[0].parts?.[0].text?.content).toBe('hello')
  })

  it('concatenates thinking content across multiple incoming parts', () => {
    const list = [agentEntry('player', 'thinking', [{ thinking: { content: 'a' } }])]
    const next = appendToEntry(list, 0, [{ thinking: { content: 'b' } }, { thinking: { content: 'c' } }], 'thinking')
    expect(next[0].parts?.[0].thinking?.content).toBe('abc')
    expect(list[0].parts?.[0].thinking?.content).toBe('a')
  })

  it('does not mutate the original entry or part objects', () => {
    const part: StreamPart = { text: { content: 'x' } }
    const entry = agentEntry('player', 'text', [part])
    const list = [entry]
    const next = appendToEntry(list, 0, [{ text: { content: 'y' } }], 'text')
    expect(next[0]).not.toBe(entry)
    expect(next[0].parts?.[0]).not.toBe(part)
    expect(next[0].parts?.[0].text?.content).toBe('xy')
    expect(entry.parts?.[0]).toBe(part)
    expect(part.text?.content).toBe('x')
  })
})
