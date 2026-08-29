// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App.js'

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

// wireChunk wraps one bare ChatEvent JSON line in the grpc-gateway v2
// streaming envelope the real /api/v2 wire carries ({"result": <ChatEvent>}
// + "\n" — conversation-api.md §2).
function wireChunk(eventLine: string): string {
  return `{"result":${eventLine}}\n`
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
    wireChunk('{"turnId":"t1","turnStart":{}}') +
    wireChunk('{"turnId":"t1","blockStart":{"index":0,"type":"BLOCK_TYPE_TEXT"}}') +
    wireChunk('{"turnId":"t1","delta":{"index":0,"text":"部"}}')
  const restPhase = [
    wireChunk('{"turnId":"t1","delta":{"index":0,"text":"分"}}'),
    wireChunk('{"turnId":"t1","blockEnd":{"index":0,"block":{"text":{"content":"部分"}}}}'),
    wireChunk('{"turnId":"t1","turnEnd":{"status":"TURN_STATUS_COMPLETED"}}'),
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

// ─── US4（T028 删除编排 / T029 多会话隔离） ──────────────────────────────────

const S1 = 'templates/saolei/sessions/s1'
const S2 = 'templates/saolei/sessions/s2'

interface Us4Fixture {
  name: string
  deleteStatus?: number
  disposeStatus?: number
  // 自定义响应（延迟/失败注入）；未提供时按默认成功响应路由。
  historyResponse?: () => Promise<Response>
  deleteResponse?: () => Promise<Response>
  send?: () => Response
}

// makeUs4FetchMock routes list/delete/dispose/history/send per fixture.
// delete/dispose default to success so each test overrides only the branch it
// exercises.
function makeUs4FetchMock(fixtures: Us4Fixture[]) {
  return vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    const method = init?.method ?? 'GET'
    if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
      return jsonResponse({
        sessions: fixtures.map((f) => ({
          name: f.name,
          createTime: '2026-08-29T00:00:00Z',
        })),
      })
    }
    for (const f of fixtures) {
      if (url === `/api/v1/${f.name}` && method === 'DELETE') {
        if (f.deleteResponse !== undefined) return f.deleteResponse()
        const status = f.deleteStatus ?? 200
        return new Response(status === 200 ? '{}' : 'delete failed', { status })
      }
      if (url === `/api/v2/${f.name}:history`) {
        if (f.historyResponse !== undefined) return f.historyResponse()
        return jsonResponse({ messages: [] })
      }
      if (url === `/api/v2/${f.name}:send` && method === 'POST') {
        return f.send?.() ?? jsonResponse({})
      }
      if (url === `/api/v2/${f.name}:dispose` && method === 'POST') {
        const status = f.disposeStatus ?? 200
        return new Response(status === 200 ? '{}' : 'dispose failed', {
          status,
        })
      }
    }
    throw new Error(`unexpected fetch: ${url}`)
  })
}

// gatedResponse holds the fetch response behind `open()` so a test can land
// the response at a chosen point (race/regression windows); each call awaits
// the same gate, which stays open afterwards.
function gatedResponse(make: () => Response): {
  respond: () => Promise<Response>
  open: () => void
} {
  let openGate: () => void = () => {}
  const gate = new Promise<void>((resolve) => {
    openGate = resolve
  })
  return {
    respond: async () => {
      await gate
      return make()
    },
    open: () => openGate(),
  }
}

// pausedSend returns a Send response whose first phase is delivered
// immediately and whose remaining frames wait for release(); `done` resolves
// once the stream is fully consumed, making background-reduction assertions
// deterministic.
function pausedSend(firstPhase: string, restFrames: string[]) {
  const encoder = new TextEncoder()
  let releaseRest: () => void = () => {}
  const restReleased = new Promise<void>((resolve) => {
    releaseRest = resolve
  })
  let markDone: () => void = () => {}
  const done = new Promise<void>((resolve) => {
    markDone = resolve
  })
  const stream = new ReadableStream<Uint8Array>({
    async start(controller) {
      controller.enqueue(encoder.encode(firstPhase))
      await restReleased
      for (const frame of restFrames) controller.enqueue(encoder.encode(frame))
      controller.close()
      markDone()
    },
  })
  return {
    response: new Response(stream, {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }),
    release: () => releaseRest(),
    done,
  }
}

describe('App 多会话隔离（US4/T029）', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = makeUs4FetchMock([])
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('双会话切换互不串扰：发送中切走后流在后台归约，返回保留状态且不重复回填', async () => {
    const s1Send = pausedSend(
      wireChunk('{"turnId":"t1","turnStart":{}}') +
        wireChunk('{"turnId":"t1","blockStart":{"index":0,"type":"BLOCK_TYPE_TEXT"}}') +
        wireChunk('{"turnId":"t1","delta":{"index":0,"text":"部"}}'),
      [
        wireChunk('{"turnId":"t1","delta":{"index":0,"text":"分"}}'),
        wireChunk('{"turnId":"t1","blockEnd":{"index":0,"block":{"text":{"content":"部分"}}}}'),
        wireChunk('{"turnId":"t1","turnEnd":{"status":"TURN_STATUS_COMPLETED"}}'),
      ],
    )
    fetchMock = makeUs4FetchMock([
      { name: S1, send: () => s1Send.response },
      { name: S2 },
    ])
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    // 进入 s1 并发送；首帧 delta「部」先呈现。
    fireEvent.click(await screen.findByText('s1'))
    const input = await screen.findByTestId('chat-input')
    expect(input.getAttribute('aria-label')).toContain('s1')
    fireEvent.change(input, { target: { value: '一' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('部')
    })

    // 切到 s2：s1 内容不在 DOM（未选中的面板不渲染），s2 输入区就绪。
    fireEvent.click(screen.getByText('s2'))
    const s2Input = await screen.findByTestId('chat-input')
    expect(s2Input.getAttribute('aria-label')).toContain('s2')
    expect(screen.queryByTestId('agent-text')).toBeNull()
    expect(screen.queryByText('部')).toBeNull()

    // s1 的 Send 流在后台继续归约直至回合完成（fetch 不中断）。
    s1Send.release()
    await s1Send.done

    // 回到 s1：「部分」已由后台归约合并入历史，用户消息「一」仍在；
    // :history 仅首次进入请求一次（返回不重置状态、不重复回填）。
    fireEvent.click(screen.getByText('s1'))
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('部分')
    })
    expect(screen.getByText('一')).toBeTruthy()
    const s1HistoryCalls = fetchMock.mock.calls.filter(
      (call) => call[0] === `/api/v2/${S1}:history`,
    )
    expect(s1HistoryCalls).toHaveLength(1)
  })
})

describe('App 回填竞态与失败路径（US4 回归）', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = makeUs4FetchMock([])
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // enterS1WithGatedHistory renders the app, enters s1 with the history
  // response held behind the returned gate, sends a message and waits for its
  // first delta to render — the gate is then resolved mid-turn.
  async function enterS1WithGatedHistory(
    make: () => Response,
  ): Promise<{ open: () => void; send: ReturnType<typeof pausedSend> }> {
    const historyGate = gatedResponse(make)
    const s1Send = pausedSend(
      wireChunk('{"turnId":"t1","turnStart":{}}') +
        wireChunk('{"turnId":"t1","blockStart":{"index":0,"type":"BLOCK_TYPE_TEXT"}}') +
        wireChunk('{"turnId":"t1","delta":{"index":0,"text":"部"}}'),
      [
        wireChunk('{"turnId":"t1","delta":{"index":0,"text":"分"}}'),
        wireChunk('{"turnId":"t1","blockEnd":{"index":0,"block":{"text":{"content":"部分"}}}}'),
        wireChunk('{"turnId":"t1","turnEnd":{"status":"TURN_STATUS_COMPLETED"}}'),
      ],
    )
    fetchMock = makeUs4FetchMock([
      {
        name: S1,
        historyResponse: historyGate.respond,
        send: () => s1Send.response,
      },
    ])
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    fireEvent.click(await screen.findByText('s1'))
    await waitFor(() => {
      expect(fetchMock.mock.calls.some((call) => call[0] === `/api/v2/${S1}:history`)).toBe(true)
    })
    const input = await screen.findByTestId('chat-input')
    fireEvent.change(input, { target: { value: '一' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('部')
    })
    return { open: historyGate.open, send: s1Send }
  }

  it('回填让位守卫：history 响应晚于 send 首帧落地时不覆盖在途回合与用户消息', async () => {
    const gate = await enterS1WithGatedHistory(
      () =>
        jsonResponse({
          messages: [
            { role: 'ROLE_AGENT', blocks: [{ text: { content: '旧历史' } }] },
          ],
        }),
    )

    // 回填（含过期历史）在回合进行中落地：整体让位——live 与本地用户消息
    // 保持不变，过期历史不进入渲染（open 后至流耗尽经过多个宏任务，
    // 回填 .then 必已执行，终态断言确定）。
    gate.open()
    expect(screen.getByTestId('agent-text').textContent).toBe('部')
    expect(screen.getByText('一')).toBeTruthy()

    // 回合正常完成并合并；回填内容仍不出现。
    gate.send.release()
    await gate.send.done
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('部分')
    })
    expect(screen.getByText('一')).toBeTruthy()
    expect(screen.queryByText('旧历史')).toBeNull()
  })

  it('回填失败不清状态：history 500 时在途回合与用户消息保持、仅提示错误', async () => {
    const gate = await enterS1WithGatedHistory(() => new Response('history failed', { status: 500 }))

    gate.open()
    // 回归断言（major 修复）：失败路径仅呈现错误，live/用户消息不被清空。
    await waitFor(() => {
      expect(screen.getByTestId('chat-error')).toBeTruthy()
    })
    expect(screen.getByTestId('agent-text').textContent).toBe('部')
    expect(screen.getByText('一')).toBeTruthy()

    // 回合可照常完成合并。
    gate.send.release()
    await gate.send.done
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('部分')
    })
    expect(screen.getByText('一')).toBeTruthy()
  })
})

describe('App 删除编排（US4/T028）', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = makeUs4FetchMock([{ name: S1 }])
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  async function selectS1(): Promise<void> {
    render(<App />)
    fireEvent.click(await screen.findByText('s1'))
    await screen.findByTestId('chat-input')
  }

  it('DELETE /api/v1 成功后 POST :dispose，条目移除并提示返回列表', async () => {
    await selectS1()

    fireEvent.click(screen.getByTestId('delete-session'))
    await waitFor(() => {
      expect(screen.queryByTestId('session-item')).toBeNull()
    })

    // 编排顺序（research.md D6）：先 /api/v1 元数据删除，成功才 /api/v2 释放。
    const calls = fetchMock.mock.calls
    const deleteIdx = calls.findIndex(
      (call) => call[0] === `/api/v1/${S1}` && (call[1] as RequestInit).method === 'DELETE',
    )
    const disposeIdx = calls.findIndex(
      (call) => call[0] === `/api/v2/${S1}:dispose`,
    )
    expect(deleteIdx).toBeGreaterThanOrEqual(0)
    expect(disposeIdx).toBeGreaterThan(deleteIdx)
    expect((calls[disposeIdx]?.[1] as RequestInit).method).toBe('POST')

    // 返回列表页 + 删除提示（web-frontend.md §4）。
    expect(screen.getByTestId('empty-hint').textContent).toContain('会话已删除')
  })

  it('dispose 失败仅记录不阻断：条目仍移除、返回列表（容错分支）', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      fetchMock = makeUs4FetchMock([{ name: S1, disposeStatus: 500 }])
      vi.stubGlobal('fetch', fetchMock)
      await selectS1()

      fireEvent.click(screen.getByTestId('delete-session'))
      await waitFor(() => {
        expect(screen.queryByTestId('session-item')).toBeNull()
      })

      // mock 正向断言：dispose 确实被调用且以失败收场，失败仅记录。
      expect(
        fetchMock.mock.calls.some((call) => call[0] === `/api/v2/${S1}:dispose`),
      ).toBe(true)
      expect(errorSpy).toHaveBeenCalled()
      expect(screen.getByTestId('empty-hint').textContent).toContain('会话已删除')
    } finally {
      errorSpy.mockRestore()
    }
  })

  it('删除 await 窗口内切换到其他会话：删除完成后停留在新会话', async () => {
    const deleteGate = gatedResponse(() => new Response('{}', { status: 200 }))
    fetchMock = makeUs4FetchMock([
      { name: S1, deleteResponse: deleteGate.respond },
      { name: S2 },
    ])
    vi.stubGlobal('fetch', fetchMock)
    await selectS1()

    // await 窗口内导航到 s2。
    fireEvent.click(screen.getByTestId('delete-session'))
    fireEvent.click(screen.getByText('s2'))
    await waitFor(() => {
      expect(screen.getByTestId('chat-input').getAttribute('aria-label')).toContain('s2')
    })

    // 删除完成：s1 从列表移除，但导航不被覆盖——停在 s2，无返回列表提示。
    deleteGate.open()
    await waitFor(() => {
      expect(screen.queryByText('s1')).toBeNull()
    })
    // 完整编排仍执行（DELETE 成功后 dispose）。
    expect(
      fetchMock.mock.calls.some((call) => call[0] === `/api/v2/${S1}:dispose`),
    ).toBe(true)
    expect(screen.getByTestId('chat-input').getAttribute('aria-label')).toContain('s2')
    expect(screen.queryByTestId('empty-hint')).toBeNull()
    expect(screen.queryByText(/会话已删除/)).toBeNull()
  })

  it('DELETE /api/v1 失败：错误呈现、条目保留、不触发 dispose', async () => {
    fetchMock = makeUs4FetchMock([{ name: S1, deleteStatus: 500 }])
    vi.stubGlobal('fetch', fetchMock)
    await selectS1()

    fireEvent.click(screen.getByTestId('delete-session'))
    await waitFor(() => {
      expect(screen.getByTestId('session-error')).toBeTruthy()
    })

    expect(screen.getByTestId('session-item')).toBeTruthy()
    expect(
      fetchMock.mock.calls.some((call) => call[0] === `/api/v2/${S1}:dispose`),
    ).toBe(false)
  })
})
