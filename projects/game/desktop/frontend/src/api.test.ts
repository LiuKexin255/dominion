import { describe, expect, it } from 'vitest'
import { flowPartKind } from './api'
import type { FlowPart } from './api'

// Pure-function tests for the FlowPart variant reader. The desktop lib_test
// globs src/**/*.ts only (no Svelte component mount), so the unit-testable
// surface is the pure module convention of api.ts
// (style/javascript.md §测试).

describe('flowPartKind', () => {
  it('returns the operation variant for a mouse click part', () => {
    const part: FlowPart = { mouseClick: { toolId: 'click-1', click: 'MOUSE_CLICK_ACTION_LEFT_CLICK' } }
    expect(flowPartKind(part)).toBe('mouseClick')
  })

  it('returns the operation variant for a keyboard press part', () => {
    const part: FlowPart = { keyboardPress: { toolId: 'kb-1', key: 'KEYBOARD_KEY_F2' } }
    expect(flowPartKind(part)).toBe('keyboardPress')
  })

  it('returns the operation variant for a mouse move part', () => {
    const part: FlowPart = { mouseMove: { toolId: 'mv-1', xPx: 10, yPx: 20 } }
    expect(flowPartKind(part)).toBe('mouseMove')
  })

  it('returns the operation variant for a mouse move-and-click part', () => {
    const part: FlowPart = { mouseMoveAndClick: { toolId: 'mc-1', xPx: 10, yPx: 20, click: 'MOUSE_CLICK_ACTION_LEFT_CLICK' } }
    expect(flowPartKind(part)).toBe('mouseMoveAndClick')
  })

  it('returns the signal variants (wait/warn/status/queue)', () => {
    expect(flowPartKind({ wait: {} })).toBe('wait')
    expect(flowPartKind({ warn: { message: 'boom' } })).toBe('warn')
    expect(flowPartKind({ status: { status: 'STATUS_SIGNAL_STATUS_IDLE' } })).toBe('status')
    expect(flowPartKind({ queue: { queuedCount: 2 } })).toBe('queue')
  })

  it('returns undefined for an empty part (no active variant)', () => {
    expect(flowPartKind({})).toBeUndefined()
  })
})
