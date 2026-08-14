import { describe, expect, it } from 'vitest'
import { PartCompletion, partInterrupted } from './api'
import type { MessagePart } from './api'

// Pure-function tests for the 044 "interrupted" marker reader
// (specs/044-llm-stall-recovery-fix/spec.md FR-005/FR-013; data-model.md §4.2).
// The desktop lib_test globs src/**/*.ts only (no Svelte component mount), so
// the unit-testable surface is the partInterrupted helper — the pure module
// convention of stream-merge.test.ts / chat-fifo.test.ts
// (style/javascript.md §测试; desktop-rendering-contract.md §4).

describe('partInterrupted', () => {
  it('returns true for a text part carrying PART_COMPLETION_INTERRUPTED (protojson enum-name string)', () => {
    const part: MessagePart = {
      text: { content: 'cut off mid', completion: 'PART_COMPLETION_INTERRUPTED' },
    }
    expect(partInterrupted(part)).toBe(true)
  })

  it('returns true for a thinking part carrying PART_COMPLETION_INTERRUPTED', () => {
    const part: MessagePart = {
      thinking: { content: 'cut off mid', completion: 'PART_COMPLETION_INTERRUPTED' },
    }
    expect(partInterrupted(part)).toBe(true)
  })

  it('returns true for the numeric enum form (protojson "emit enums as integers")', () => {
    const part: MessagePart = {
      text: { content: 'cut off mid', completion: PartCompletion.INTERRUPTED },
    }
    expect(partInterrupted(part)).toBe(true)
  })

  it('returns false for a normal text part (field absent — the protojson zero-value omission)', () => {
    const part: MessagePart = { text: { content: 'complete reply' } }
    expect(partInterrupted(part)).toBe(false)
  })

  it('returns false for a normal thinking part (field absent)', () => {
    const part: MessagePart = { thinking: { content: 'complete thinking' } }
    expect(partInterrupted(part)).toBe(false)
  })

  it('returns false for an explicit UNSPECIFIED completion (string and numeric forms)', () => {
    expect(
      partInterrupted({ text: { content: 'x', completion: 'PART_COMPLETION_UNSPECIFIED' } }),
    ).toBe(false)
    expect(
      partInterrupted({ thinking: { content: 'x', completion: PartCompletion.UNSPECIFIED } }),
    ).toBe(false)
  })

  it('returns false for an empty part (no active text/thinking variant)', () => {
    expect(partInterrupted({})).toBe(false)
  })
})
