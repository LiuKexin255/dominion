// @vitest-environment jsdom
import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.svelte'
import type { Session } from './api'

// Component-level assertions for the desktop back-navigation refresh contract
// (specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md §4.2): returning
// to the sessions page re-lists sessions, a failed refresh surfaces the
// existing error semantics without clearing the rendered list, and the
// refresh writes only sessions/loading/error — never selectedSession/page
// (specs/055-agent-v2-ui-fixes/data-model.md §4). jsdom has no Wails runtime,
// so window.runtime stays undefined and onMount's event subscription is
// skipped (safe-by-default path).

const mocks = vi.hoisted(() => ({
  listSessions: vi.fn(),
  connect: vi.fn(),
  closeAgent: vi.fn(),
  listWindows: vi.fn(),
  getSelectedWindow: vi.fn(),
}))

// In-package relative-module mock (style/javascript.md §Mock 约定): every
// mock a test exercises carries a positive call assertion in the test body.
// The remaining ./api runtime bindings are plain stubs — App.svelte's named
// ESM imports require every binding to exist, and no test drives them.
vi.mock('./api', () => ({
  TEMPLATE_SAOLEI: 'saolei',
  TEMPLATES: ['saolei'],
  listSessions: mocks.listSessions,
  connect: mocks.connect,
  closeAgent: mocks.closeAgent,
  listWindows: mocks.listWindows,
  getSelectedWindow: mocks.getSelectedWindow,
  setConfig: () => Promise.resolve(undefined),
  setSelectedWindow: () => Promise.resolve(undefined),
  setDebugMode: () => Promise.resolve(undefined),
  confirmToolResult: () => Promise.resolve(undefined),
}))

const SESSIONS: Session[] = [
  { name: 'sessions/a', sessionId: 'a', createTime: '2026-09-05T00:00:00Z' },
  { name: 'sessions/b', sessionId: 'b', createTime: '2026-09-05T00:00:01Z' },
]

let instance: object | undefined

// Polls an assertion after synchronously flushing Svelte's pending state
// updates, so state written by async handlers becomes observable in the DOM
// without real timers.
async function waitForFlush(assertion: () => void): Promise<void> {
  await vi.waitFor(() => {
    flushSync()
    assertion()
  })
}

function required(selector: string): HTMLElement {
  const el = document.querySelector(selector)
  if (!el) throw new Error(`element not found: ${selector}`)
  return el as HTMLElement
}

beforeEach(() => {
  vi.clearAllMocks()
  document.body.innerHTML = ''
  mocks.listSessions.mockResolvedValue({ sessions: SESSIONS, nextPageToken: '' })
  mocks.connect.mockResolvedValue('ws://test')
  mocks.closeAgent.mockResolvedValue(undefined)
  mocks.listWindows.mockResolvedValue([])
  mocks.getSelectedWindow.mockResolvedValue(0)
})

afterEach(() => {
  if (instance) {
    unmount(instance)
    instance = undefined
  }
})

describe('App sessions refresh', () => {
  it('re-lists sessions when navigating back from a session', async () => {
    instance = mount(App, { target: document.body })

    // Mount-time load is the pre-existing refresh semantic
    // (specs/055-agent-v2-ui-fixes/data-model.md §4 onMount row)
    await waitForFlush(() => {
      expect(mocks.listSessions).toHaveBeenCalledTimes(1)
      expect(document.querySelectorAll('.session-row')).toHaveLength(2)
    })

    // Enter a session: the row click drives the read-only selection flow
    required('.session-row').click()
    await waitForFlush(() => {
      expect(required('[data-testid="back-btn"]')).toBeTruthy()
    })
    expect(mocks.connect).toHaveBeenCalledTimes(1)

    // Back navigation MUST refresh the list (FR-007): a second listSessions
    // call lands while the previously rendered rows are re-rendered
    required('[data-testid="back-btn"]').click()
    await waitForFlush(() => {
      expect(mocks.listSessions).toHaveBeenCalledTimes(2)
      expect(document.querySelectorAll('.session-row')).toHaveLength(2)
    })

    // Positive assertions for every remaining exercised mock
    // (style/javascript.md 规则：验证 mock 确实生效)
    expect(mocks.closeAgent).toHaveBeenCalledTimes(1)
    expect(mocks.listWindows).toHaveBeenCalledTimes(1)
    expect(mocks.getSelectedWindow).toHaveBeenCalledTimes(1)
  })

  it('keeps the existing list and renders the error when the back-refresh fails', async () => {
    instance = mount(App, { target: document.body })
    await waitForFlush(() => {
      expect(document.querySelectorAll('.session-row')).toHaveLength(2)
    })

    mocks.listSessions.mockRejectedValueOnce(new Error('list exploded'))

    required('.session-row').click()
    await waitForFlush(() => {
      expect(required('[data-testid="back-btn"]')).toBeTruthy()
    })
    required('[data-testid="back-btn"]').click()

    // Failed refresh: the existing error semantics surface — the banner text
    // plus the error log entry (spec FR-007 "既有错误语义呈现（错误提示 +
    // 日志）"). SessionList's pre-existing render contract swaps rows for the
    // error branch, so list non-clearance is asserted at the data layer
    // below.
    await waitForFlush(() => {
      expect(document.querySelector('.session-error')?.textContent).toContain('list exploded')
      expect(document.querySelector('.log-error')?.textContent).toContain('Refresh failed')
    })
    expect(mocks.listSessions).toHaveBeenCalledTimes(2)

    // The failed refresh must not clear the held sessions
    // (specs/055-agent-v2-ui-fixes/data-model.md §4 既有 catch 语义). The
    // Apply Config handler clears `error`, which switches SessionList out of
    // its error branch: if the catch had emptied `sessions`, the empty state
    // would render instead of the two held rows.
    required('.btn-primary').click()
    await waitForFlush(() => {
      expect(document.querySelectorAll('.session-row')).toHaveLength(2)
    })
  })
})
