import { describe, expect, it } from 'vitest'
import { MAX_CHAT_ENTRIES_PER_AGENT, trimFifo } from './chat-fifo'

// US4 FIFO display cap tests (specs/037-saolei-team-optimize/spec.md
// FR-020..FR-025). The module is pure — no Svelte harness needed
// (style/javascript.md — reliable DI/test-double seams over vi.mock).

describe('trimFifo', () => {
  it('removes the oldest entries when over cap, keeping the newest N (FIFO, FR-021)', () => {
    const entries = Array.from({ length: MAX_CHAT_ENTRIES_PER_AGENT + 5 }, (_, i) => `m${i}`)
    const trimmed = trimFifo(entries)
    expect(trimmed).toHaveLength(MAX_CHAT_ENTRIES_PER_AGENT)
    expect(trimmed[0]).toBe('m5')
    expect(trimmed[trimmed.length - 1]).toBe(`m${MAX_CHAT_ENTRIES_PER_AGENT + 4}`)
  })

  it('leaves an at-cap array unchanged (same reference, FR-021)', () => {
    const entries = Array.from({ length: MAX_CHAT_ENTRIES_PER_AGENT }, (_, i) => `m${i}`)
    const trimmed = trimFifo(entries)
    expect(trimmed).toBe(entries)
    expect(trimmed).toHaveLength(MAX_CHAT_ENTRIES_PER_AGENT)
  })

  it('leaves an under-cap array unchanged (same reference, FR-021)', () => {
    const entries = ['a', 'b', 'c']
    const trimmed = trimFifo(entries)
    expect(trimmed).toBe(entries)
    expect(trimmed).toEqual(['a', 'b', 'c'])
  })

  it('honours an explicit custom max at the call site', () => {
    const trimmed = trimFifo(['a', 'b', 'c', 'd'], 2)
    expect(trimmed).toEqual(['c', 'd'])
  })

  it('counts each agent tab independently — trimming one array leaves the other intact (FR-022)', () => {
    const player = Array.from({ length: MAX_CHAT_ENTRIES_PER_AGENT + 1 }, (_, i) => `player-${i}`)
    const planner = Array.from({ length: 10 }, (_, i) => `planner-${i}`)
    const trimmedPlayer = trimFifo(player)
    const trimmedPlanner = trimFifo(planner)
    expect(trimmedPlayer).toHaveLength(MAX_CHAT_ENTRIES_PER_AGENT)
    expect(trimmedPlanner).toBe(planner)
    expect(trimmedPlanner).toHaveLength(10)
    expect(trimmedPlanner[0]).toBe('planner-0')
  })

  it('truncates an over-cap history load to the newest cap entries (FR-024)', () => {
    const history = Array.from({ length: MAX_CHAT_ENTRIES_PER_AGENT + 30 }, (_, i) => `h-${i}`)
    const loaded = trimFifo([...history])
    expect(loaded).toHaveLength(MAX_CHAT_ENTRIES_PER_AGENT)
    expect(loaded[0]).toBe('h-30')
    expect(loaded[loaded.length - 1]).toBe(`h-${MAX_CHAT_ENTRIES_PER_AGENT + 29}`)
  })

  it('applies one FIFO to live and history messages mixed in arrival order (FR-025)', () => {
    // Live stream fills the tab to cap; a history append then overflows it.
    // The unified FIFO evicts the oldest entries regardless of source.
    const live = Array.from({ length: MAX_CHAT_ENTRIES_PER_AGENT }, (_, i) => `live-${i}`)
    const merged = [...live, 'hist-1', 'hist-2', 'hist-3']
    const trimmed = trimFifo(merged)
    expect(trimmed).toHaveLength(MAX_CHAT_ENTRIES_PER_AGENT)
    expect(trimmed[0]).toBe('live-3')
    expect(trimmed[trimmed.length - 1]).toBe('hist-3')
  })
})
