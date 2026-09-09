// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
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
// + "\n" — conversation-api.md §2, extended by the 051 tool_result frame).
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
    if (url === `/api/v2/${SESSION}/agent/messages`) {
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

// ─── 多会话隔离与删除编排（web-frontend.md §4/§6，FR-007） ──────────────────

const S1 = 'templates/saolei/sessions/s1'
const S2 = 'templates/saolei/sessions/s2'

interface Us4Fixture {
  name: string
  deleteStatus?: number
  // 自定义响应（延迟/失败注入）；未提供时按默认成功响应路由。
  historyResponse?: () => Promise<Response>
  deleteResponse?: () => Promise<Response>
  send?: () => Response
}

// makeUs4FetchMock routes list/delete/messages/send per fixture. Delete
// defaults to success so each test overrides only the branch it exercises.
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
      if (url === `/api/v2/${f.name}/agent/messages`) {
        if (f.historyResponse !== undefined) return f.historyResponse()
        return jsonResponse({ messages: [] })
      }
      if (url === `/api/v2/${f.name}:send` && method === 'POST') {
        return f.send?.() ?? jsonResponse({})
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

describe('App 多会话隔离', () => {
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
    // List 仅首次进入请求一次（返回不重置状态、不重复回填）。
    fireEvent.click(screen.getByText('s1'))
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('部分')
    })
    expect(screen.getByText('一')).toBeTruthy()
    const s1HistoryCalls = fetchMock.mock.calls.filter(
      (call) => call[0] === `/api/v2/${S1}/agent/messages`,
    )
    expect(s1HistoryCalls).toHaveLength(1)
  })
})

describe('App 回填竞态与失败路径', () => {
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
      expect(fetchMock.mock.calls.some((call) => call[0] === `/api/v2/${S1}/agent/messages`)).toBe(
        true,
      )
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

describe('App 删除编排（FR-007：仅元数据删除）', () => {
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

  // deleteViaMenu 走条目 `···` 菜单的删除确认步（FR-003：删除入口在每
  // 条目右侧菜单、不依赖选中态，specs/051-agent-v2-dsh-migration/
  // contracts/web-frontend.md §1）。
  function deleteViaMenu(name: string): void {
    const entry = screen.getByText(name).closest('li')
    expect(entry).toBeTruthy()
    fireEvent.click(within(entry as HTMLElement).getByTestId('session-actions'))
    fireEvent.click(screen.getByRole('menuitem', { name: '删除' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '确认删除' }))
  }

  it('DELETE /api/v1 成功后条目移除并提示返回列表（无 agent 释放调用）', async () => {
    await selectS1()

    deleteViaMenu('s1')
    await waitFor(() => {
      expect(screen.queryByTestId('session-item')).toBeNull()
    })

    // 删除编排仅 DELETE /api/v1 元数据——Dispose RPC 已移除，无任何
    // /api/v2 释放跳（agent-api.md §2，FR-007）。
    const calls = fetchMock.mock.calls
    expect(
      calls.some(
        (call) => call[0] === `/api/v1/${S1}` && (call[1] as RequestInit).method === 'DELETE',
      ),
    ).toBe(true)
    expect(calls.some((call) => String(call[0]).includes(':dispose'))).toBe(false)

    // 返回列表页 + 删除提示（web-frontend.md §4）。
    expect(screen.getByTestId('empty-hint').textContent).toContain('会话已删除')
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
    deleteViaMenu('s1')
    fireEvent.click(screen.getByText('s2'))
    await waitFor(() => {
      expect(screen.getByTestId('chat-input').getAttribute('aria-label')).toContain('s2')
    })

    // 删除完成：s1 从列表移除，但导航不被覆盖——停在 s2，无返回列表提示。
    deleteGate.open()
    await waitFor(() => {
      expect(screen.queryByText('s1')).toBeNull()
    })
    expect(screen.getByTestId('chat-input').getAttribute('aria-label')).toContain('s2')
    expect(screen.queryByTestId('empty-hint')).toBeNull()
    expect(screen.queryByText(/会话已删除/)).toBeNull()
  })

  it('DELETE /api/v1 失败：错误呈现、条目保留', async () => {
    fetchMock = makeUs4FetchMock([{ name: S1, deleteStatus: 500 }])
    vi.stubGlobal('fetch', fetchMock)
    await selectS1()

    deleteViaMenu('s1')
    await waitFor(() => {
      expect(screen.getByTestId('session-error')).toBeTruthy()
    })

    expect(screen.getByTestId('session-item')).toBeTruthy()
  })
})

// ─── App cancel 编排（specs/054-agent-v2-bugfixes/contracts/web-ui.md §8
// ─── 测试义务 6：请求 + 流终态；ChatPanel 的 onCancel → cancelAgent → 错误
// ─── 呈现/终态归约链路） ────────────────────────────────────────────────────

// makeCancelFetchMock routes the faces the cancel scenarios touch: session
// list, empty backfill, a paused Send stream, and the :cancel custom method
// whose response the test injects.
function makeCancelFetchMock(options: {
  send: () => Response
  cancelResponse: () => Response
}) {
  return vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    const method = init?.method ?? 'GET'
    if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
      return jsonResponse({
        sessions: [{ name: S1, createTime: '2026-08-29T00:00:00Z' }],
      })
    }
    if (url === `/api/v2/${S1}/agent/messages`) {
      return jsonResponse({ messages: [] })
    }
    if (url === `/api/v2/${S1}:send` && method === 'POST') {
      return options.send()
    }
    if (url === `/api/v2/${S1}/agent:cancel` && method === 'POST') {
      return options.cancelResponse()
    }
    throw new Error(`unexpected fetch: ${url}`)
  })
}

// ─── 桌面连接状态三态与刷新（specs/054-agent-v2-bugfixes/contracts/
// ─── web-ui.md §5/§8-6：ChatPanel 的 GetAgent desktop_connected 投影、
// ─── 进入会话/send 前/turn 结束即时刷新 + 10s 轮询、404/失败降级 unknown） ────

// makeConnFetchMock routes the faces the connection-status scenarios touch:
// session list, per-session empty backfill, per-session GetAgent (the
// connection fact source), and an optional Send stream. GetAgent responses
// are injected per session so each test drives only the state it asserts.
function makeConnFetchMock(
  sessions: string[],
  agentFor: (name: string) => Response,
  sendFor?: (name: string) => Response,
) {
  return vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    const method = init?.method ?? 'GET'
    if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
      return jsonResponse({
        sessions: sessions.map((name) => ({ name, createTime: '2026-08-29T00:00:00Z' })),
      })
    }
    for (const name of sessions) {
      if (url === `/api/v2/${name}/agent/messages`) {
        return jsonResponse({ messages: [] })
      }
      if (url === `/api/v2/${name}/agent` && method === 'GET') {
        return agentFor(name)
      }
      if (url === `/api/v2/${name}:send` && method === 'POST') {
        return sendFor?.(name) ?? jsonResponse({})
      }
    }
    throw new Error(`unexpected fetch: ${url}`)
  })
}

// agentGetCalls counts the GetAgent reads for one session — the refresh
// trigger assertions below are phrased as deltas over this count (positive
// assertions that the GetAgent route is actually exercised;
// style/javascript.md Mock 约定).
function agentGetCalls(fetchMock: ReturnType<typeof vi.fn>, name: string): number {
  return fetchMock.mock.calls.filter(
    (call) =>
      call[0] === `/api/v2/${name}/agent` &&
      ((call[1] as RequestInit | undefined)?.method ?? 'GET') === 'GET',
  ).length
}

function agentView(name: string, body: Record<string, unknown>): Response {
  return jsonResponse({ name: `${name}/agent`, preset: 'templates/saolei/presets/p1', ...body })
}

describe('App 桌面连接状态三态（web-ui.md §5）', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = makeConnFetchMock([S1], () => agentView(S1, {}))
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  async function enterS1(): Promise<HTMLElement> {
    render(<App />)
    fireEvent.click(await screen.findByText('s1'))
    return await screen.findByTestId('desktop-conn-status')
  }

  it('desktop_connected=true → 已连接（success 态呈现）', async () => {
    fetchMock = makeConnFetchMock([S1], () => agentView(S1, { desktopConnected: true }))
    vi.stubGlobal('fetch', fetchMock)
    const status = await enterS1()
    await waitFor(() => {
      expect(status.getAttribute('data-state')).toBe('connected')
    })
    expect(status.textContent).toBe('桌面已连接')
  })

  it('desktop_connected 缺省（protojson false 不输出）→ 未连接（警示呈现）', async () => {
    const status = await enterS1()
    await waitFor(() => {
      expect(status.getAttribute('data-state')).toBe('disconnected')
    })
    expect(status.textContent).toBe('桌面未连接')
  })

  it('GetAgent 404（未物化）→ 降级未知，禁止显示为已连接', async () => {
    fetchMock = makeConnFetchMock([S1], () => new Response('not found', { status: 404 }))
    vi.stubGlobal('fetch', fetchMock)
    const status = await enterS1()
    await waitFor(() => {
      expect(status.getAttribute('data-state')).toBe('unknown')
    })
    expect(status.textContent).toBe('桌面连接未知')
    expect(status.textContent).not.toContain('已连接')
  })

  it('GetAgent 请求失败（500）→ 同样降级未知', async () => {
    fetchMock = makeConnFetchMock([S1], () => new Response('boom', { status: 500 }))
    vi.stubGlobal('fetch', fetchMock)
    const status = await enterS1()
    await waitFor(() => {
      expect(status.getAttribute('data-state')).toBe('unknown')
    })
    expect(status.textContent).not.toContain('已连接')
  })
})

describe('App 连接状态刷新时机（web-ui.md §5：即时刷新 + 轮询）', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = makeConnFetchMock([S1], () => agentView(S1, { desktopConnected: true }))
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('send 前与 turn 结束各即时刷新一次连接状态', async () => {
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
    fetchMock = makeConnFetchMock(
      [S1],
      () => agentView(S1, { desktopConnected: true }),
      () => s1Send.response,
    )
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)
    fireEvent.click(await screen.findByText('s1'))

    // 进入会话：物化探测 + 连接刷新各读一次 GetAgent，随后以增量为断言面。
    await screen.findByTestId('desktop-conn-status')
    await waitFor(() => {
      expect(agentGetCalls(fetchMock, S1)).toBeGreaterThanOrEqual(2)
    })
    const before = agentGetCalls(fetchMock, S1)

    const input = await screen.findByTestId('chat-input')
    fireEvent.change(input, { target: { value: '你好' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect(agentGetCalls(fetchMock, S1)).toBe(before + 1)
    })

    s1Send.release()
    await s1Send.done
    await waitFor(() => {
      expect(agentGetCalls(fetchMock, S1)).toBe(before + 2)
    })
  })

  it('10s 轮询仅在 active 会话触发，后台面板不轮询', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    fetchMock = makeConnFetchMock(
      [S1, S2],
      (name) => agentView(name, { desktopConnected: name === S1 }),
    )
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    // 进入 s1 后切到 s2：s1 面板保持挂载（后台），s2 为唯一 active 面板。
    fireEvent.click(await screen.findByText('s1'))
    await screen.findByTestId('chat-input')
    fireEvent.click(screen.getByText('s2'))
    await screen.findByTestId('chat-input')
    await waitFor(() => {
      expect(agentGetCalls(fetchMock, S2)).toBeGreaterThanOrEqual(2)
    })
    const s1Before = agentGetCalls(fetchMock, S1)
    const s2Before = agentGetCalls(fetchMock, S2)

    await vi.advanceTimersByTimeAsync(10_000)
    await waitFor(() => {
      expect(agentGetCalls(fetchMock, S2)).toBe(s2Before + 1)
    })
    expect(agentGetCalls(fetchMock, S1)).toBe(s1Before)
  })
})

describe('App cancel 编排（web-ui.md §4/§8-6）', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // enterRunningS1 renders the app, enters s1 and sends a message whose
  // stream stays paused mid-turn — the cancel button is up for the running
  // turn. `restFrames` are the frames the test releases at the end; they
  // default to the turn_end{CANCELED} terminal frame (终态经流，cancel 请求
  // 本身只承载请求级结果——web-ui.md §4).
  async function enterRunningS1(
    cancelResponse: () => Response,
    restFrames: string[] = [wireChunk('{"turnId":"t1","turnEnd":{"status":"TURN_STATUS_CANCELED"}}')],
  ): Promise<ReturnType<typeof pausedSend>> {
    const s1Send = pausedSend(
      wireChunk('{"turnId":"t1","turnStart":{}}') +
        wireChunk('{"turnId":"t1","blockStart":{"index":0,"type":"BLOCK_TYPE_TEXT"}}') +
        wireChunk('{"turnId":"t1","delta":{"index":0,"text":"部"}}'),
      restFrames,
    )
    fetchMock = makeCancelFetchMock({ send: () => s1Send.response, cancelResponse })
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    fireEvent.click(await screen.findByText('s1'))
    const input = await screen.findByTestId('chat-input')
    fireEvent.change(input, { target: { value: '失控回合' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('部')
    })
    return s1Send
  }

  it('运行中点击终止：POST {session}/agent:cancel 请求形状正确，流上 turn_end{CANCELED} 呈现"已终止"终态', async () => {
    const s1Send = await enterRunningS1(() => jsonResponse({}))
    expect(screen.getByTestId('cancel-button')).toBeTruthy()

    fireEvent.click(screen.getByTestId('cancel-button'))

    // cancelAgent 的请求形状（请求仅 name 路径参数，body 空对象）。
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${S1}/agent:cancel`,
        expect.objectContaining({
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({}),
        }),
      )
    })

    // 终态经流上 turn_end{CANCELED} 由 store 归约：已产出分段保留入历史、
    // "已终止"标识呈现且不复用错误呈现面。
    s1Send.release()
    await s1Send.done
    await waitFor(() => {
      expect(screen.getByTestId('turn-canceled').textContent).toBe('已终止')
    })
    expect(screen.queryByTestId('chat-error')).toBeNull()
    expect(screen.getByTestId('agent-text').textContent).toBe('部')
    // 回合已收束：输入立即可用（终止按钮随 live 归空消失）。
    expect(screen.queryByTestId('cancel-button')).toBeNull()
  })

  it('cancel 请求失败不吞：错误经 chat-error 呈现，无"已终止"终态（终态只能来自流）', async () => {
    // 空剩余帧：本用例断言"无 turn_end{CANCELED}"的失败路径，流不在途。
    const s1Send = await enterRunningS1(() => new Response('agent not materialized', { status: 400 }), [])

    fireEvent.click(screen.getByTestId('cancel-button'))

    // 请求级失败（未物化 FAILED_PRECONDITION→400）呈现，不吞（web-ui.md §4）。
    await waitFor(() => {
      expect(screen.getByTestId('chat-error').textContent).toContain('400')
    })
    // 流上无 turn_end{CANCELED}，终态标识不出现；在途回合保持运行中。
    expect(screen.queryByTestId('turn-canceled')).toBeNull()
    expect(screen.getByTestId('agent-text').textContent).toBe('部')
    expect(screen.getByTestId('cancel-button')).toBeTruthy()

    // 收尾 release：流 close 后 store.send 循环正常结束（无 turn_end 不报
    // 错），不留未完成的异步任务。
    s1Send.release()
    await s1Send.done
  })
})

// ─── App 重建同步（specs/057-agent-v2-ui-fixes-2/contracts/ui-interactions.md
// ─── §2：Apply 成功（UpdateAgent 清理重建，specs/051-agent-v2-dsh-migration/
// ─── contracts/agent-api.md §2.1）后经与挂载回填同一的 runBackfill 重建对话
// ─── 视图；测试口径 = 契约 §2.7，收敛矩阵 = data-model.md §1.3） ─────────────

const PRESET_P1 = {
  name: 'templates/saolei/presets/p1',
  persona: '你是扫雷玩家',
  createTime: '2026-08-29T00:00:00Z',
  updateTime: '2026-08-29T00:00:00Z',
}
const AGENT_MATERIALIZED = {
  name: `${S1}/agent`,
  preset: PRESET_P1.name,
  model: 'glm-5.2',
  createTime: '2026-08-29T01:00:00Z',
  updateTime: '2026-08-29T01:00:00Z',
}
const OLD_HISTORY = {
  messages: [{ role: 'ROLE_AGENT', blocks: [{ text: { content: '旧历史' } }] }],
}

const SEND_FIRST_PHASE =
  wireChunk('{"turnId":"t1","turnStart":{}}') +
  wireChunk('{"turnId":"t1","blockStart":{"index":0,"type":"BLOCK_TYPE_TEXT"}}') +
  wireChunk('{"turnId":"t1","delta":{"index":0,"text":"部"}}')
const SEND_COMPLETED_REST = [
  wireChunk('{"turnId":"t1","delta":{"index":0,"text":"分"}}'),
  wireChunk('{"turnId":"t1","blockEnd":{"index":0,"block":{"text":{"content":"部分"}}}}'),
  wireChunk('{"turnId":"t1","turnEnd":{"status":"TURN_STATUS_COMPLETED"}}'),
]
const SEND_ABORTED_REST = [wireChunk('{"turnId":"t1","turnEnd":{"status":"TURN_STATUS_ABORTED"}}')]

// makeApplyFetchMock routes the faces the rebuild-sync scenarios touch: the
// session list, sequenced ListAgentMessages responses（第 N 次 GET 依序取用、
// 末项重复——挂载回填与 Apply 后回填两次命中，契约 §2.7）, GetAgent, panel
// data sources, PATCH (updateAgent), and Send. historyCallCount positively
// tracks the messages GETs — the rebuild-sync trigger assertion
// (style/javascript.md Mock 约定).
function makeApplyFetchMock(fixtures: {
  historyResponses?: (() => Response | Promise<Response>)[]
  agentGet?: () => Response
  patchResponse?: () => Response
  sendResponse?: () => Response
}) {
  let historyCalls = 0
  const history = fixtures.historyResponses ?? [() => jsonResponse({ messages: [] })]
  const fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    const method = init?.method ?? 'GET'
    if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
      return jsonResponse({ sessions: [{ name: S1, createTime: '2026-08-29T00:00:00Z' }] })
    }
    if (url === `/api/v2/${S1}/agent/messages` && method === 'GET') {
      const respond = history[Math.min(historyCalls, history.length - 1)]
      historyCalls += 1
      return respond()
    }
    if (url === `/api/v2/${S1}/agent` && method === 'GET') {
      return fixtures.agentGet?.() ?? agentView(S1, {})
    }
    if (url === '/api/v2/templates/saolei/presets' && method === 'GET') {
      return jsonResponse({ presets: [PRESET_P1] })
    }
    if (url === '/api/v2/models' && method === 'GET') {
      return jsonResponse({ models: [{ id: 'glm-5.2' }] })
    }
    if (url === `/api/v2/${S1}/agent?allow_missing=true` && method === 'PATCH') {
      return fixtures.patchResponse?.() ?? jsonResponse(AGENT_MATERIALIZED)
    }
    if (url === `/api/v2/${S1}:send` && method === 'POST') {
      return fixtures.sendResponse?.() ?? jsonResponse({})
    }
    throw new Error(`unexpected fetch: ${url} ${method}`)
  })
  return { fetchMock, historyCallCount: () => historyCalls }
}

describe('App 重建同步（Apply 成功后对话视图即时同步）', () => {
  let fetchMock: ReturnType<typeof vi.fn>
  let historyCallCount: () => number

  beforeEach(() => {
    const mock = makeApplyFetchMock({})
    fetchMock = mock.fetchMock
    historyCallCount = mock.historyCallCount
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // flush drains two macrotask turns: promise continuations queued by an
  // opened response gate（microtask chain，含 Response body 消费）在超时宏任务
  // 前全部落地，使"回填响应已处理"的断言确定。
  async function flush(): Promise<void> {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
  }

  // lastAgentText reads the last agent-text element's content: ChatView renders
  // history before the live turn, so the last element is the current turn's
  // newest content（历史含旧消息时 agent-text 有多个，getBy 会多匹配抛错）.
  function lastAgentText(): string {
    const texts = screen.getAllByTestId('agent-text')
    return (texts[texts.length - 1] as HTMLElement).textContent ?? ''
  }

  // enterS1 renders the app and enters s1; positive assertions cover the
  // routes the rebuild-sync scenarios rely on（style/javascript.md mock 约定：
  // 证明 GetAgent 探测与首次回填确实被 exercise）.
  async function enterS1(): Promise<void> {
    render(<App />)
    fireEvent.click(await screen.findByText('s1'))
    await screen.findByTestId('chat-input')
    await waitFor(() => {
      expect(fetchMock.mock.calls.some((call) => call[0] === `/api/v2/${S1}/agent/messages`)).toBe(
        true,
      )
      expect(fetchMock.mock.calls.some((call) => call[0] === `/api/v2/${S1}/agent`)).toBe(true)
    })
  }

  // waitForPanelReady waits for the settings panel and its data sources with
  // positive fetch assertions（presets/models 下拉同源面）.
  async function waitForPanelReady(): Promise<void> {
    expect(await screen.findByTestId('agent-settings-panel')).toBeTruthy()
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/v2/templates/saolei/presets', undefined)
      expect(fetchMock).toHaveBeenCalledWith('/api/v2/models', undefined)
    })
    await waitFor(() => {
      expect(
        (screen.getByTestId('agent-preset-select') as HTMLSelectElement).querySelectorAll('option')
          .length,
      ).toBeGreaterThan(1)
    })
  }

  // applyPreset selects the fixture preset and clicks Apply（preset 必选）.
  function applyPreset(): void {
    fireEvent.change(screen.getByTestId('agent-preset-select'), {
      target: { value: PRESET_P1.name },
    })
    fireEvent.click(screen.getByTestId('agent-apply'))
  }

  // assertPatchApplied positively asserts the UpdateAgent PATCH request shape
  // （agent-api.md §2.1：PATCH allow_missing=true，body 仅 {preset, model?}）.
  async function assertPatchApplied(body: Record<string, string>): Promise<void> {
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${S1}/agent?allow_missing=true`,
        expect.objectContaining({ method: 'PATCH', body: JSON.stringify(body) }),
      )
    })
  }

  // assertCleanChat asserts the converged clean chat face（data-model.md
  // §1.3 收敛矩阵终态：history/live/queue/error/canceled 全部复位）.
  function assertCleanChat(): void {
    expect(screen.queryByTestId('agent-text')).toBeNull()
    expect(screen.queryByText('旧历史')).toBeNull()
    expect(screen.queryByText('一')).toBeNull()
    expect(screen.queryByTestId('queue-chip')).toBeNull()
    expect(screen.queryByTestId('chat-error')).toBeNull()
    expect(screen.queryByTestId('turn-canceled')).toBeNull()
  }

  // enterBusyS1 drives s1 into the busy premise shared by the two convergence
  // orders: old history rendered, a turn live mid-stream, then a successful
  // Apply whose rebuild backfill is held in flight by the gate.
  async function enterBusyS1(): Promise<void> {
    await enterS1()
    const input = await screen.findByTestId('chat-input')
    fireEvent.change(input, { target: { value: '一' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${S1}:send`,
        expect.objectContaining({ method: 'POST' }),
      )
    })
    await waitFor(() => {
      expect(lastAgentText()).toBe('部')
    })
    fireEvent.click(await screen.findByTestId('agent-settings-button'))
    await waitForPanelReady()
    applyPreset()
    await assertPatchApplied({ preset: PRESET_P1.name })
    await waitFor(() => {
      expect(historyCallCount()).toBe(2)
    })
  }

  it('已物化会话 Apply 成功：PATCH 200 后再次回填历史，旧消息清空', async () => {
    const mock = makeApplyFetchMock({
      historyResponses: [() => jsonResponse(OLD_HISTORY), () => jsonResponse({ messages: [] })],
    })
    fetchMock = mock.fetchMock
    historyCallCount = mock.historyCallCount
    vi.stubGlobal('fetch', fetchMock)
    await enterS1()
    expect(screen.getByText('旧历史')).toBeTruthy()

    fireEvent.click(await screen.findByTestId('agent-settings-button'))
    await waitForPanelReady()
    applyPreset()
    await assertPatchApplied({ preset: PRESET_P1.name })

    // Apply 成功触发第二次回填（重建同步动作，契约 §2.2）。
    await waitFor(() => {
      expect(historyCallCount()).toBe(2)
    })
    await waitFor(() => {
      expect(screen.queryByText('旧历史')).toBeNull()
    })
    // onApplied 既有语义零回归：面板关闭、状态已物化；无错误呈现。
    expect(screen.queryByTestId('agent-settings-panel')).toBeNull()
    expect(screen.getByTestId('agent-status').textContent).toContain('已物化')
    expect(screen.queryByTestId('chat-error')).toBeNull()
  })

  it('Apply 后紧随 send：慢回填让位，空历史不覆盖新回合', async () => {
    const backfillGate = gatedResponse(() => jsonResponse({ messages: [] }))
    const s1Send = pausedSend(SEND_FIRST_PHASE, SEND_COMPLETED_REST)
    const mock = makeApplyFetchMock({
      historyResponses: [() => jsonResponse(OLD_HISTORY), backfillGate.respond],
      sendResponse: () => s1Send.response,
    })
    fetchMock = mock.fetchMock
    historyCallCount = mock.historyCallCount
    vi.stubGlobal('fetch', fetchMock)
    await enterS1()

    fireEvent.click(await screen.findByTestId('agent-settings-button'))
    await waitForPanelReady()
    applyPreset()
    await assertPatchApplied({ preset: PRESET_P1.name })
    // Apply 触发的回填已发出且在途（gate 关闭）。
    await waitFor(() => {
      expect(historyCallCount()).toBe(2)
    })

    // 紧随发送：send 一经开始，回填整体让位（契约 §2.3）。
    const input = await screen.findByTestId('chat-input')
    fireEvent.change(input, { target: { value: '一' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${S1}:send`,
        expect.objectContaining({ method: 'POST' }),
      )
    })
    await waitFor(() => {
      expect(lastAgentText()).toBe('部')
    })

    // 空历史落地但让位：live、用户消息与既有历史均保持。
    backfillGate.open()
    await flush()
    expect(lastAgentText()).toBe('部')
    expect(screen.getByText('一')).toBeTruthy()
    expect(screen.getByText('旧历史')).toBeTruthy()

    // 回合照常完成合并，回填内容仍不出现。
    s1Send.release()
    await s1Send.done
    await waitFor(() => {
      expect(lastAgentText()).toBe('部分')
    })
    expect(screen.getByText('一')).toBeTruthy()
    expect(screen.getByText('旧历史')).toBeTruthy()
    expect(screen.queryByTestId('chat-error')).toBeNull()
  })

  it('Apply 成功后回填失败（500）：既有回填错误呈现、对话不清空', async () => {
    const mock = makeApplyFetchMock({
      historyResponses: [
        () => jsonResponse(OLD_HISTORY),
        () => new Response('history failed', { status: 500 }),
      ],
    })
    fetchMock = mock.fetchMock
    historyCallCount = mock.historyCallCount
    vi.stubGlobal('fetch', fetchMock)
    await enterS1()

    fireEvent.click(await screen.findByTestId('agent-settings-button'))
    await waitForPanelReady()
    applyPreset()
    await assertPatchApplied({ preset: PRESET_P1.name })

    // 回填失败 → 既有 backfillError 呈现（契约 §2.4），对话不清空。
    await waitFor(() => {
      expect(screen.getByTestId('chat-error').textContent).toContain('500')
    })
    expect(screen.getByText('旧历史')).toBeTruthy()
    expect(screen.queryByTestId('agent-settings-panel')).toBeNull()
    expect(screen.getByTestId('agent-status').textContent).toContain('已物化')
  })

  it('Apply 失败（PATCH 500）：面板错误呈现、不触发回填、对话不清空', async () => {
    const mock = makeApplyFetchMock({
      historyResponses: [() => jsonResponse(OLD_HISTORY)],
      patchResponse: () => new Response('apply failed', { status: 500 }),
    })
    fetchMock = mock.fetchMock
    historyCallCount = mock.historyCallCount
    vi.stubGlobal('fetch', fetchMock)
    await enterS1()

    fireEvent.click(await screen.findByTestId('agent-settings-button'))
    await waitForPanelReady()
    applyPreset()

    // 面板错误既有呈现（AgentSettingsPanel apply 的 catch 路径）。
    await waitFor(() => {
      expect(screen.getByTestId('agent-settings-error').textContent).toContain('500')
    })
    await flush()
    // 回填触发只在 Apply 成功路径（契约 §2.1）：失败后无第二次回填。
    expect(historyCallCount()).toBe(1)
    expect(screen.getByTestId('agent-settings-panel')).toBeTruthy()
    expect(screen.getByText('旧历史')).toBeTruthy()
    expect(screen.queryByTestId('chat-error')).toBeNull()
  })

  it('忙时收敛（ABORTED 先落地、回填后落地）：收敛同一干净终态', async () => {
    const backfillGate = gatedResponse(() => jsonResponse({ messages: [] }))
    const s1Send = pausedSend(SEND_FIRST_PHASE, SEND_ABORTED_REST)
    const mock = makeApplyFetchMock({
      historyResponses: [() => jsonResponse(OLD_HISTORY), backfillGate.respond],
      sendResponse: () => s1Send.response,
    })
    fetchMock = mock.fetchMock
    historyCallCount = mock.historyCallCount
    vi.stubGlobal('fetch', fetchMock)
    await enterBusyS1()

    // 事件一：在途流收 turn_end{ABORTED}，store 归约清空（web-frontend.md §4）。
    s1Send.release()
    await s1Send.done
    await waitFor(() => {
      expect(screen.queryByTestId('agent-text')).toBeNull()
    })
    expect(screen.queryByText('旧历史')).toBeNull()
    expect(screen.queryByText('一')).toBeNull()

    // 事件二：回填 200 空后落地（守卫仍 false）→ 同一干净终态，无复活残留。
    backfillGate.open()
    await flush()
    assertCleanChat()
  })

  it('忙时收敛（回填先落地、ABORTED 后落地）：收敛同一干净终态', async () => {
    const backfillGate = gatedResponse(() => jsonResponse({ messages: [] }))
    const s1Send = pausedSend(SEND_FIRST_PHASE, SEND_ABORTED_REST)
    const mock = makeApplyFetchMock({
      historyResponses: [() => jsonResponse(OLD_HISTORY), backfillGate.respond],
      sendResponse: () => s1Send.response,
    })
    fetchMock = mock.fetchMock
    historyCallCount = mock.historyCallCount
    vi.stubGlobal('fetch', fetchMock)
    await enterBusyS1()

    // 事件一：回填先落地（守卫 false）→ loadHistory([]) 全量清空（含 live）。
    backfillGate.open()
    await waitFor(() => {
      expect(screen.queryByTestId('agent-text')).toBeNull()
    })
    expect(screen.queryByText('旧历史')).toBeNull()
    expect(screen.queryByText('一')).toBeNull()

    // 事件二：ABORTED 落地于空态，归约幂等 → 终态不变。
    s1Send.release()
    await s1Send.done
    await flush()
    assertCleanChat()
  })

  it('首次物化：未物化 Apply 成功 → 回填 200 空、引导消退、对话面为空', async () => {
    const mock = makeApplyFetchMock({
      agentGet: () => new Response('not materialized', { status: 404 }),
      historyResponses: [
        () => new Response('not found', { status: 404 }),
        () => jsonResponse({ messages: [] }),
      ],
    })
    fetchMock = mock.fetchMock
    historyCallCount = mock.historyCallCount
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)
    fireEvent.click(await screen.findByText('s1'))
    const guide = await screen.findByTestId('agent-guide')
    expect(guide.textContent).toContain('该会话尚未设置 agent')
    expect(screen.getByTestId('agent-status').textContent).toBe('未物化')
    await waitFor(() => {
      expect(historyCallCount()).toBe(1)
    })

    // 引导入口打开面板并 Apply（web-frontend.md §3 引导流转）。
    fireEvent.click(screen.getByTestId('agent-guide-open'))
    await waitForPanelReady()
    applyPreset()
    await assertPatchApplied({ preset: PRESET_P1.name })

    // Apply 成功：面板与引导消退（onApplied 既有语义零回归）。
    await waitFor(() => {
      expect(screen.queryByTestId('agent-settings-panel')).toBeNull()
    })
    expect(screen.queryByTestId('agent-guide')).toBeNull()
    expect(screen.getByTestId('agent-status').textContent).toContain('已物化')

    // Apply 后回填触发且返回 200 空（agent 已存在，research D1）→ 对话面为空。
    await waitFor(() => {
      expect(historyCallCount()).toBe(2)
    })
    assertCleanChat()
    expect(screen.queryByTestId('agent-settings-error')).toBeNull()
  })
})
