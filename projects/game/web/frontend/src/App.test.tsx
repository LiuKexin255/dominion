// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

const SESSION = 'templates/saolei/sessions/s1'

// ─── fetch mock（style/javascript.md Mock 约定：vi.fn() test-double；测试对
// ─── 被拦截调用做正向断言证明 mock 确实生效） ─────────────────────────────────

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

// makeFetchMock routes the app's relative-path API calls. The Send route
// streams the turn's NDJSON events (conversation-api.md §2) in two phases:
// the remaining frames are only enqueued once the test releases them, so the
// 渐进呈现 assertion is deterministic.
function makeFetchMock() {
  let releaseRest: (() => void) | null = null
  const restReleased = new Promise<void>((resolve) => {
    releaseRest = resolve
  })

  const encoder = new TextEncoder()
  const firstPhase =
    '{"turnId":"t1","turnStart":{}}\n' +
    '{"turnId":"t1","blockStart":{"index":0,"type":"BLOCK_TYPE_TEXT"}}\n' +
    '{"turnId":"t1","delta":{"index":0,"text":"部"}}\n'
  const restPhase = [
    '{"turnId":"t1","delta":{"index":0,"text":"分"}}\n',
    '{"turnId":"t1","blockEnd":{"index":0,"block":{"text":{"content":"部分"}}}}\n',
    '{"turnId":"t1","turnEnd":{"status":"TURN_STATUS_COMPLETED"}}\n',
  ]

  const fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    if (url === '/api/v1/templates/saolei/sessions' && (init?.method ?? 'GET') === 'GET') {
      return jsonResponse({ sessions: [] })
    }
    if (url === '/api/v1/templates/saolei/sessions' && init?.method === 'POST') {
      return jsonResponse({ name: SESSION, createTime: '2026-08-29T00:00:00Z' })
    }
    if (url === `/api/v2/${SESSION}:history`) {
      return jsonResponse({ messages: [] })
    }
    if (url === `/api/v2/${SESSION}:send` && init?.method === 'POST') {
      const stream = new ReadableStream<Uint8Array>({
        async start(controller) {
          controller.enqueue(encoder.encode(firstPhase))
          await restReleased
          for (const frame of restPhase) controller.enqueue(encoder.encode(frame))
          controller.close()
        },
      })
      return new Response(stream, {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    throw new Error(`unexpected fetch: ${url}`)
  })

  return { fetchMock, releaseRest: () => releaseRest?.() }
}

describe('App 对话闭环', () => {
  let releaseRest: () => void
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    const mock = makeFetchMock()
    fetchMock = mock.fetchMock
    releaseRest = mock.releaseRest
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('新建 session 后发送消息，回复渐进呈现并合并入历史', async () => {
    render(<App />)

    // New session from the sidebar; the app auto-selects it.
    fireEvent.click(await screen.findByTestId('create-session'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v1/templates/saolei/sessions',
        expect.objectContaining({ method: 'POST' }),
      )
    })

    // Type a message and send it.
    const input = await screen.findByTestId('chat-input')
    fireEvent.change(input, { target: { value: '你好' } })
    fireEvent.click(screen.getByTestId('send-button'))

    // The Send call carried the typed text（mock 正向断言）.
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${SESSION}:send`,
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ text: '你好' }),
        }),
      )
    })

    // 渐进呈现：仅首帧 delta 已渲染，回合尚未结束。
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('部')
    })

    // Release the remaining frames: the full text renders as the turn
    // completes and merges into history.
    releaseRest()
    await waitFor(() => {
      const texts = screen
        .getAllByTestId('agent-text')
        .map((el) => el.textContent)
      expect(texts).toContain('部分')
    })
    expect(screen.getByText('你好')).toBeTruthy()
    expect(screen.queryByTestId('queue-chip')).toBeNull()
  })
})
