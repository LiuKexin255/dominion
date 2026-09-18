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

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

// wireChunk wraps one bare ChatEvent JSON line in the grpc-gateway v2
// streaming envelope the real /api/v2 wire carries ({"result": <ChatEvent>}
// + "\n" — team-api.md §3.2 双帧承载：成员事件帧带 member 标注，team_message
// 为 team 级帧）。
function wireChunk(eventLine: string): string {
  return `{"result":${eventLine}}\n`
}

// memberWire builds one member event frame: member is the producing member's
// role string (scenario vocabulary; USER is the reserved user value,
// team-api.md §3.2).
function memberWire(member: string, turnId: string, event: Record<string, unknown>): string {
  return wireChunk(JSON.stringify({ member: member.toLowerCase(), turnId, ...event }))
}

// teamEventWire builds the team 级 team_message frame (protojson: int64 seq is
// a JSON string; member/message identical to a ListTeamMessages element).
function teamEventWire(
  member: string,
  seq: number,
  content: string,
  role: 'ROLE_USER' | 'ROLE_AGENT' = 'ROLE_AGENT',
): string {
  return wireChunk(
    JSON.stringify({
      teamMessage: {
        member: member.toLowerCase(),
        message: { role, blocks: [{ text: { content } }] },
        seq: String(seq),
      },
    }),
  )
}

// teamView builds one GetTeam/UpdateTeam response projection (team-api.md §1):
// members 输入输出同形（role 为场景词汇字符串），顶层不预选 model（面板默认
// 留空 = body 省略）。
function teamView(name: string, body: Record<string, unknown> = {}): Response {
  return jsonResponse({
    name: `${name}/team`,
    members: [
      {
        name: `${name}/team/members/player`,
        role: 'player',
        preset: 'templates/saolei/presets/p-player',
        model: 'glm-responses/glm-5.2',
      },
      {
        name: `${name}/team/members/planner`,
        role: 'planner',
        preset: 'templates/saolei/presets/p-planner',
      },
    ],
    ...body,
  })
}

// makeFetchMock routes the app's relative-path API calls. The Send route
// streams the team stream's NDJSON events (team-api.md §3) in two phases: the
// remaining frames are only enqueued once the test releases them, so the
// 渐进呈现 assertion is deterministic.
function makeFetchMock() {
  let releaseRest: (() => void) | null = null
  const restReleased = new Promise<void>((resolve) => {
    releaseRest = resolve
  })

  const encoder = new TextEncoder()
  const firstPhase =
    teamEventWire('USER', 1, '你好', 'ROLE_USER') +
    memberWire('PLANNER', 't1', { turnStart: {} }) +
    memberWire('PLANNER', 't1', { blockStart: { index: 0, type: 'BLOCK_TYPE_TEXT' } }) +
    memberWire('PLANNER', 't1', { delta: { index: 0, text: '部' } })
  const restPhase = [
    memberWire('PLANNER', 't1', { delta: { index: 0, text: '分' } }),
    memberWire('PLANNER', 't1', {
      blockEnd: { index: 0, block: { text: { content: '部分' } } },
    }),
    teamEventWire('PLANNER', 2, '部分'),
    memberWire('PLANNER', 't1', { turnEnd: { status: 'TURN_STATUS_COMPLETED' } }),
  ]

  const fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    if (url === '/api/v1/templates/saolei/sessions' && (init?.method ?? 'GET') === 'GET') {
      return jsonResponse({ sessions: [] })
    }
    if (url === '/api/v1/templates/saolei/sessions' && init?.method === 'POST') {
      return jsonResponse({ name: SESSION, createTime: '2026-08-29T00:00:00Z' })
    }
    if (url === `/api/v2/${SESSION}/team` && (init?.method ?? 'GET') === 'GET') {
      return teamView(SESSION)
    }
    if (url === `/api/v2/${SESSION}/team/messages`) {
      return jsonResponse({ messages: [] })
    }
    // 成员视角回填（web-views.md §2）：挂载与回合结束各请求一次，测试面
    // 默认为空（成员视角渲染专项用例在各自 fixture 中给出行数据）。
    if (
      url === `/api/v2/${SESSION}/team/members/player/messages` ||
      url === `/api/v2/${SESSION}/team/members/planner/messages`
    ) {
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

  it('新建 session 后发送消息，回复渐进呈现并合并入团队视图', async () => {
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

    // Release the remaining frames: the full text renders as the member turn
    // completes and merges into the team view.
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

// ─── 多会话隔离与删除编排（web-views.md §2/§6，FR-014） ─────────────────────

const S1 = 'templates/saolei/sessions/s1'
const S2 = 'templates/saolei/sessions/s2'

interface Us4Fixture {
  name: string
  deleteStatus?: number
  // 自定义响应（延迟/失败注入）；未提供时按默认成功响应路由。
  historyResponse?: () => Promise<Response> | Response
  // 成员视角回填注入（member = 'player' | 'planner'；web-views.md §2）；
  // 未提供时返回空集合。
  memberHistoryResponse?: (member: string) => Promise<Response> | Response
  deleteResponse?: () => Promise<Response>
  send?: () => Response
}

// makeUs4FetchMock routes list/delete/team/messages/send per fixture. Delete
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
      if (url === `/api/v2/${f.name}/team/messages`) {
        if (f.historyResponse !== undefined) return f.historyResponse()
        return jsonResponse({ messages: [] })
      }
      // 成员视角回填（web-views.md §2）：默认空；专项用例以 memberHistoryResponse
      // 注入视角序列。
      if (
        url === `/api/v2/${f.name}/team/members/player/messages` ||
        url === `/api/v2/${f.name}/team/members/planner/messages`
      ) {
        const member = url.includes('/members/player/') ? 'player' : 'planner'
        if (f.memberHistoryResponse !== undefined) return f.memberHistoryResponse(member)
        return jsonResponse({ messages: [] })
      }
      if (url === `/api/v2/${f.name}/team` && method === 'GET') {
        return teamView(f.name)
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

// s1FirstPhase / s1Rest build the canonical planner turn over the team stream
// (user message team_message frame + member event frames + consolidation).
function s1FirstPhase(userText: string): string {
  return (
    teamEventWire('USER', 1, userText, 'ROLE_USER') +
    memberWire('PLANNER', 't1', { turnStart: {} }) +
    memberWire('PLANNER', 't1', { blockStart: { index: 0, type: 'BLOCK_TYPE_TEXT' } }) +
    memberWire('PLANNER', 't1', { delta: { index: 0, text: '部' } })
  )
}

function s1Rest(seq: number): string[] {
  return [
    memberWire('PLANNER', 't1', { delta: { index: 0, text: '分' } }),
    memberWire('PLANNER', 't1', {
      blockEnd: { index: 0, block: { text: { content: '部分' } } },
    }),
    teamEventWire('PLANNER', seq, '部分'),
    memberWire('PLANNER', 't1', { turnEnd: { status: 'TURN_STATUS_COMPLETED' } }),
  ]
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
    const s1Send = pausedSend(s1FirstPhase('一'), s1Rest(2))
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

    // s1 的 team 流在后台继续归约直至回合完成（fetch 不中断）。
    s1Send.release()
    await s1Send.done

    // 回到 s1：「部分」已由后台归约合并入团队视图，用户消息「一」仍在；
    // List 仅首次进入请求一次（返回不重置状态、不重复回填）。
    fireEvent.click(screen.getByText('s1'))
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('部分')
    })
    expect(screen.getByText('一')).toBeTruthy()
    const s1HistoryCalls = fetchMock.mock.calls.filter(
      (call) => call[0] === `/api/v2/${S1}/team/messages`,
    )
    expect(s1HistoryCalls).toHaveLength(1)
  })
})

// ─── App 并发 Send 流（V6 排队场景：流 A 存续期间再 Send 建流 B；
// ─── team-api.md §3.4：多流完整扇出、前端按锚去重） ─────────────────────────

describe('App 并发 Send 流', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('两流并存时重复扇出的 delta 不翻倍、终态唯一（修复 M3）', async () => {
    // 流 A：本轮打开的流；流 B：排队路径的首帧 queued + 同一回合的完整重复帧。
    const streamA = pausedSend(s1FirstPhase('一'), s1Rest(3))
    const streamB = pausedSend(
      wireChunk('{"queued":{"position":1}}') +
        teamEventWire('USER', 2, '二', 'ROLE_USER') +
        memberWire('PLANNER', 't1', { turnStart: {} }) +
        memberWire('PLANNER', 't1', { blockStart: { index: 0, type: 'BLOCK_TYPE_TEXT' } }) +
        memberWire('PLANNER', 't1', { delta: { index: 0, text: '部' } }),
      s1Rest(3),
    )
    let sendCalls = 0
    fetchMock = makeUs4FetchMock([
      {
        name: S1,
        send: () => {
          sendCalls += 1
          return sendCalls === 1 ? streamA.response : streamB.response
        },
      },
    ])
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    fireEvent.click(await screen.findByText('s1'))
    const input = await screen.findByTestId('chat-input')
    fireEvent.change(input, { target: { value: '一' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('部')
    })

    // 流 A 存续期间再 Send：建流 B（服务端排队），B 完整重复扇出同一回合。
    fireEvent.change(input, { target: { value: '二' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect(sendCalls).toBe(2)
    })
    await waitFor(() => {
      expect(screen.getByText('二')).toBeTruthy()
    })
    // B 的重复 delta 被块归属去重：live 文本仍为「部」（修复前为「部部」）。
    expect(screen.getAllByTestId('agent-text')).toHaveLength(1)
    expect(screen.getByTestId('agent-text').textContent).toBe('部')

    // 两流各自送达固化与终态：归并条目唯一、正文不翻倍。
    streamA.release()
    streamB.release()
    await Promise.all([streamA.done, streamB.done])
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('部分')
    })
    expect(screen.getAllByTestId('agent-text')).toHaveLength(1)
    expect(screen.getAllByTestId('member-tag')).toHaveLength(1)
    expect(screen.getByText('一')).toBeTruthy()
    expect(screen.getByText('二')).toBeTruthy()
    expect(screen.queryByTestId('chat-error')).toBeNull()
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
    const s1Send = pausedSend(s1FirstPhase('一'), s1Rest(2))
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
      expect(fetchMock.mock.calls.some((call) => call[0] === `/api/v2/${S1}/team/messages`)).toBe(
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
    const gate = await enterS1WithGatedHistory(() =>
      jsonResponse({
        messages: [
          {
            member: 'player',
            message: { role: 'ROLE_AGENT', blocks: [{ text: { content: '旧历史' } }] },
            seq: '1',
          },
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

describe('App 流断开回填对齐（用户裁定 2026-09-10：List 回填 + 下次 Send 重建）', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('流断开：已流出尾步保持可见并呈现错误；List 回填以服务端 seq 序列重新对齐', async () => {
    let historyCalls = 0
    const backfill = {
      messages: [
        {
          member: 'user',
          message: { role: 'ROLE_USER', blocks: [{ text: { content: '一' } }] },
          seq: '1',
        },
        {
          member: 'planner',
          message: { role: 'ROLE_AGENT', blocks: [{ text: { content: '完整回合' } }] },
          seq: '2',
        },
      ],
    }
    // 断线自动回填的响应由 gate 持有：先断言断开瞬态（透明尾步 + 错误），
    // 再放行回填断言 seq 对齐后的权威序列。
    const backfillGate = gatedResponse(() => jsonResponse(backfill))
    const encoder = new TextEncoder()
    fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      const method = init?.method ?? 'GET'
      if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
        return jsonResponse({ sessions: [{ name: S1, createTime: '2026-08-29T00:00:00Z' }] })
      }
      if (url === `/api/v2/${S1}/team` && method === 'GET') {
        return teamView(S1)
      }
      if (url === `/api/v2/${S1}/team/messages` && method === 'GET') {
        historyCalls += 1
        return historyCalls === 1 ? jsonResponse({ messages: [] }) : backfillGate.respond()
      }
      if (url === `/api/v2/${S1}:send` && method === 'POST') {
        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(
              encoder.encode(
                s1FirstPhase('一') + memberWire('PLANNER', 't1', { delta: { index: 0, text: '分' } }),
              ),
            )
            // 已入队帧先被消费，再以传输错误终止（controller.error 立即调用会
            // 丢弃队列，无法构造"断开前已呈现"的瞬态）。
            setTimeout(() => controller.error(new Error('network down')), 0)
          },
        })
        return new Response(stream, { status: 200, headers: { 'Content-Type': 'application/json' } })
      }
      throw new Error(`unexpected fetch: ${url} ${method}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    fireEvent.click(await screen.findByText('s1'))
    const input = await screen.findByTestId('chat-input')
    fireEvent.change(input, { target: { value: '一' } })
    fireEvent.click(screen.getByTestId('send-button'))

    // 断开后内容不丢、错误呈现。
    await waitFor(() => {
      expect(screen.getByTestId('chat-error').textContent).toContain('network down')
    })
    expect(screen.getByText('部分')).toBeTruthy()

    // 自动回填（第二次 List）以服务端权威序列替换本地投影，错误随之收敛。
    await waitFor(() => {
      expect(historyCalls).toBe(2)
    })
    backfillGate.open()
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('完整回合')
    })
    expect(screen.getByText('一')).toBeTruthy()
    expect(screen.queryByTestId('chat-error')).toBeNull()
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

  it('DELETE /api/v1 成功后条目移除并提示返回列表（无 team 释放调用）', async () => {
    await selectS1()

    deleteViaMenu('s1')
    await waitFor(() => {
      expect(screen.queryByTestId('session-item')).toBeNull()
    })

    // 删除编排仅 DELETE /api/v1 元数据——Dispose RPC 已移除，无任何
    // /api/v2 释放跳（team-api.md §1）。
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
// ─── 测试义务 6：请求 + 流终态；ChatPanel 的 onCancel → cancelTeam → 错误
// ─── 呈现/终态归约链路） ────────────────────────────────────────────────────

// makeCancelFetchMock routes the faces the cancel scenarios touch: session
// list, team view, empty backfill, a paused Send stream, and the team :cancel
// custom method whose response the test injects.
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
    if (url === `/api/v2/${S1}/team/messages`) {
      return jsonResponse({ messages: [] })
    }
    if (
      url === `/api/v2/${S1}/team/members/player/messages` ||
      url === `/api/v2/${S1}/team/members/planner/messages`
    ) {
      return jsonResponse({ messages: [] })
    }
    if (url === `/api/v2/${S1}/team` && method === 'GET') {
      return teamView(S1)
    }
    if (url === `/api/v2/${S1}:send` && method === 'POST') {
      return options.send()
    }
    if (url === `/api/v2/${S1}/team:cancel` && method === 'POST') {
      return options.cancelResponse()
    }
    throw new Error(`unexpected fetch: ${url}`)
  })
}

// ─── 桌面连接状态三态与刷新（specs/054-agent-v2-bugfixes/contracts/
// ─── web-ui.md §5/§8-6：ChatPanel 的 GetTeam desktop_connected 投影、
// ─── 进入会话/send 前/turn 结束即时刷新 + 10s 轮询、404/失败降级 unknown） ────

// makeConnFetchMock routes the faces the connection-status scenarios touch:
// session list, per-session empty backfill, per-session GetTeam (the
// connection fact source), and an optional Send stream. GetTeam responses
// are injected per session so each test drives only the state it asserts.
function makeConnFetchMock(
  sessions: string[],
  teamFor: (name: string) => Response,
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
      if (url === `/api/v2/${name}/team/messages`) {
        return jsonResponse({ messages: [] })
      }
      if (
        url === `/api/v2/${name}/team/members/player/messages` ||
        url === `/api/v2/${name}/team/members/planner/messages`
      ) {
        return jsonResponse({ messages: [] })
      }
      if (url === `/api/v2/${name}/team` && method === 'GET') {
        return teamFor(name)
      }
      if (url === `/api/v2/${name}:send` && method === 'POST') {
        return sendFor?.(name) ?? jsonResponse({})
      }
    }
    throw new Error(`unexpected fetch: ${url}`)
  })
}

// teamGetCalls counts the GetTeam reads for one session — the refresh
// trigger assertions below are phrased as deltas over this count (positive
// assertions that the GetTeam route is actually exercised;
// style/javascript.md Mock 约定).
function teamGetCalls(fetchMock: ReturnType<typeof vi.fn>, name: string): number {
  return fetchMock.mock.calls.filter(
    (call) =>
      call[0] === `/api/v2/${name}/team` &&
      ((call[1] as RequestInit | undefined)?.method ?? 'GET') === 'GET',
  ).length
}

describe('App 桌面连接状态三态（web-ui.md §5）', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = makeConnFetchMock([S1], () => teamView(S1))
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
    fetchMock = makeConnFetchMock([S1], () => teamView(S1, { desktopConnected: true }))
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

  it('GetTeam 404（未物化）→ 降级未知，禁止显示为已连接', async () => {
    fetchMock = makeConnFetchMock([S1], () => new Response('not found', { status: 404 }))
    vi.stubGlobal('fetch', fetchMock)
    const status = await enterS1()
    await waitFor(() => {
      expect(status.getAttribute('data-state')).toBe('unknown')
    })
    expect(status.textContent).toBe('桌面连接未知')
    expect(status.textContent).not.toContain('已连接')
  })

  it('GetTeam 请求失败（500）→ 同样降级未知', async () => {
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
    fetchMock = makeConnFetchMock([S1], () => teamView(S1, { desktopConnected: true }))
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('send 前与 turn 结束各即时刷新一次连接状态', async () => {
    const s1Send = pausedSend(s1FirstPhase('你好'), s1Rest(2))
    fetchMock = makeConnFetchMock(
      [S1],
      () => teamView(S1, { desktopConnected: true }),
      () => s1Send.response,
    )
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)
    fireEvent.click(await screen.findByText('s1'))

    // 进入会话：物化探测 + 连接刷新各读一次 GetTeam，随后以增量为断言面。
    await screen.findByTestId('desktop-conn-status')
    await waitFor(() => {
      expect(teamGetCalls(fetchMock, S1)).toBeGreaterThanOrEqual(2)
    })
    const before = teamGetCalls(fetchMock, S1)

    const input = await screen.findByTestId('chat-input')
    fireEvent.change(input, { target: { value: '你好' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect(teamGetCalls(fetchMock, S1)).toBe(before + 1)
    })

    s1Send.release()
    await s1Send.done
    await waitFor(() => {
      expect(teamGetCalls(fetchMock, S1)).toBe(before + 2)
    })
  })

  it('10s 轮询仅在 active 会话触发，后台面板不轮询', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    fetchMock = makeConnFetchMock(
      [S1, S2],
      (name) => teamView(name, { desktopConnected: name === S1 }),
    )
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    // 进入 s1 后切到 s2：s1 面板保持挂载（后台），s2 为唯一 active 面板。
    fireEvent.click(await screen.findByText('s1'))
    await screen.findByTestId('chat-input')
    fireEvent.click(screen.getByText('s2'))
    await screen.findByTestId('chat-input')
    await waitFor(() => {
      expect(teamGetCalls(fetchMock, S2)).toBeGreaterThanOrEqual(2)
    })
    const s1Before = teamGetCalls(fetchMock, S1)
    const s2Before = teamGetCalls(fetchMock, S2)

    await vi.advanceTimersByTimeAsync(10_000)
    await waitFor(() => {
      expect(teamGetCalls(fetchMock, S2)).toBe(s2Before + 1)
    })
    expect(teamGetCalls(fetchMock, S1)).toBe(s1Before)
  })
})

// ─── App 激活成员徽标（specs/060-agent-v2-team-optimize/contracts/
// ─── team-api.md §1/§5：GetTeam activeMember 快照 + turn_start 帧实时推导
// ─── + live 收束回退） ───────────────────────────────────────────────────────

describe('App 激活成员徽标（specs/060-agent-v2-team-optimize/contracts/team-api.md §1/§5）', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = makeConnFetchMock([S1], () => teamView(S1, { activeMember: 'planner' }))
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('物化后呈现 GetTeam 的 activeMember（初始 activation = planner）', async () => {
    render(<App />)
    fireEvent.click(await screen.findByText('s1'))

    await waitFor(() => {
      expect(screen.getByTestId('active-member').getAttribute('data-member')).toBe('planner')
    })
    expect(screen.getByTestId('active-member').textContent).toContain('planner')
    // 正向断言 GetTeam 路由被 exercise（style/javascript.md mock 约定）。
    expect(fetchMock.mock.calls.some((call) => call[0] === `/api/v2/${S1}/team`)).toBe(true)
  })

  it('未物化（GetTeam 404）不呈现激活成员徽标', async () => {
    fetchMock = makeConnFetchMock([S1], () => new Response('not found', { status: 404 }))
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)
    fireEvent.click(await screen.findByText('s1'))

    await screen.findByTestId('team-guide')
    expect(screen.queryByTestId('active-member')).toBeNull()
  })

  it('在途成员 turn_start 帧覆盖 GetTeam 值；live 收束后回退最近 GetTeam 值', async () => {
    const send = pausedSend(
      teamEventWire('USER', 1, '一', 'ROLE_USER') +
        memberWire('PLAYER', 't1', { turnStart: {} }) +
        memberWire('PLAYER', 't1', { blockStart: { index: 0, type: 'BLOCK_TYPE_TEXT' } }) +
        memberWire('PLAYER', 't1', { delta: { index: 0, text: '部' } }),
      [
        memberWire('PLAYER', 't1', {
          blockEnd: { index: 0, block: { text: { content: '部分' } } },
        }),
        teamEventWire('PLAYER', 2, '部分'),
        memberWire('PLAYER', 't1', { turnEnd: { status: 'TURN_STATUS_COMPLETED' } }),
      ],
    )
    fetchMock = makeConnFetchMock(
      [S1],
      () => teamView(S1, { activeMember: 'planner' }),
      () => send.response,
    )
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    fireEvent.click(await screen.findByText('s1'))
    await waitFor(() => {
      expect(screen.getByTestId('active-member').getAttribute('data-member')).toBe('planner')
    })

    // 成员回合在途（PLAYER 的 turn_start 帧）：实时推导覆盖 GetTeam 快照。
    const input = await screen.findByTestId('chat-input')
    fireEvent.change(input, { target: { value: '一' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect(screen.getByTestId('active-member').getAttribute('data-member')).toBe('player')
    })

    // live 全部收束：回退最近 GetTeam 值（该快照仍报 planner）。
    send.release()
    await send.done
    await waitFor(() => {
      expect(screen.getByTestId('active-member').getAttribute('data-member')).toBe('planner')
    })
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
  // default to the member turn's turn_end{CANCELED} terminal frame (终态经流，
  // cancel 请求本身只承载请求级结果——web-ui.md §4).
  async function enterRunningS1(
    cancelResponse: () => Response,
    restFrames: string[] = [
      memberWire('PLAYER', 't1', { turnEnd: { status: 'TURN_STATUS_CANCELED' } }),
    ],
  ): Promise<ReturnType<typeof pausedSend>> {
    const s1Send = pausedSend(
      teamEventWire('USER', 1, '失控回合', 'ROLE_USER') +
        memberWire('PLAYER', 't1', { turnStart: {} }) +
        memberWire('PLAYER', 't1', { blockStart: { index: 0, type: 'BLOCK_TYPE_TEXT' } }) +
        memberWire('PLAYER', 't1', { delta: { index: 0, text: '部' } }),
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

  it('运行中点击终止：POST {session}/team:cancel 请求形状正确，流上 turn_end{CANCELED} 呈现"已终止"终态', async () => {
    const s1Send = await enterRunningS1(() => jsonResponse({}))
    expect(screen.getByTestId('cancel-button')).toBeTruthy()

    fireEvent.click(screen.getByTestId('cancel-button'))

    // cancelTeam 的请求形状（请求仅 name 路径参数，body 空对象）。
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${S1}/team:cancel`,
        expect.objectContaining({
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({}),
        }),
      )
    })

    // 终态经流上 turn_end{CANCELED} 由 store 归约：已产出分段保留入归并
    // 序列、"已终止"标识呈现且不复用错误呈现面。
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
    const s1Send = await enterRunningS1(() => new Response('team not materialized', { status: 400 }), [])

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
// ─── §2：Apply 成功（UpdateTeam 清理重建，team-api.md §1）后经与挂载回填同一
// ─── 的 runBackfill 重建团队视图；测试口径 = 契约 §2.7，收敛矩阵 = data-
// ─── model.md §1.3） ─────────────────────────────────────────────────────────

const PLAYER_PRESET = {
  name: 'templates/saolei/presets/p-player',
  persona: '你是扫雷 player',
  role: 'player',
  createTime: '2026-08-29T00:00:00Z',
  updateTime: '2026-08-29T00:00:00Z',
}
const PLANNER_PRESET = {
  name: 'templates/saolei/presets/p-planner',
  persona: '你是扫雷 planner',
  role: 'planner',
  createTime: '2026-08-29T00:00:00Z',
  updateTime: '2026-08-29T00:00:00Z',
}
const OLD_HISTORY = {
  messages: [
    {
      member: 'player',
      message: { role: 'ROLE_AGENT', blocks: [{ text: { content: '旧历史' } }] },
      seq: '1',
    },
  ],
}

const SEND_FIRST_PHASE = s1FirstPhase('一')
const SEND_COMPLETED_REST = s1Rest(2)
const SEND_ABORTED_REST = [
  memberWire('PLAYER', 't1', { turnEnd: { status: 'TURN_STATUS_ABORTED' } }),
]

// makeApplyFetchMock routes the faces the rebuild-sync scenarios touch: the
// session list, sequenced ListTeamMessages responses（第 N 次 GET 依序取用、
// 末项重复——挂载回填与 Apply 后回填两次命中，契约 §2.7）, GetTeam, panel
// data sources, PATCH (updateTeam), and Send. historyCallCount positively
// tracks the messages GETs — the rebuild-sync trigger assertion
// (style/javascript.md Mock 约定).
function makeApplyFetchMock(fixtures: {
  historyResponses?: (() => Response | Promise<Response>)[]
  // memberHistoryResponse：成员视角回填注入（member + 该成员第 N 次请求），
  // 缺省为空集合（web-views.md §2）。
  memberHistoryResponse?: (member: string, call: number) => Response | Promise<Response>
  teamGet?: () => Response
  patchResponse?: () => Response
  sendResponse?: () => Response
}) {
  let historyCalls = 0
  const memberCalls: Record<string, number> = {}
  const history = fixtures.historyResponses ?? [() => jsonResponse({ messages: [] })]
  const fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    const method = init?.method ?? 'GET'
    if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
      return jsonResponse({ sessions: [{ name: S1, createTime: '2026-08-29T00:00:00Z' }] })
    }
    if (url === `/api/v2/${S1}/team/messages` && method === 'GET') {
      const respond = history[Math.min(historyCalls, history.length - 1)]
      historyCalls += 1
      return respond()
    }
    if (
      url === `/api/v2/${S1}/team/members/player/messages` ||
      url === `/api/v2/${S1}/team/members/planner/messages`
    ) {
      const member = url.includes('/members/player/') ? 'player' : 'planner'
      memberCalls[member] = (memberCalls[member] ?? 0) + 1
      return (
        fixtures.memberHistoryResponse?.(member, memberCalls[member]) ??
        jsonResponse({ messages: [] })
      )
    }
    if (url === `/api/v2/${S1}/team` && method === 'GET') {
      return fixtures.teamGet?.() ?? teamView(S1)
    }
    if (url === '/api/v2/templates/saolei/presets?role=player' && method === 'GET') {
      return jsonResponse({ presets: [PLAYER_PRESET] })
    }
    if (url === '/api/v2/templates/saolei/presets?role=planner' && method === 'GET') {
      return jsonResponse({ presets: [PLANNER_PRESET] })
    }
    if (url === '/api/v2/models' && method === 'GET') {
      return jsonResponse({ models: [{ id: 'glm-responses/glm-5.2' }] })
    }
    if (url === `/api/v2/${S1}/team?allow_missing=true` && method === 'PATCH') {
      return fixtures.patchResponse?.() ?? jsonResponse(teamView(S1))
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
  // 证明 GetTeam 探测与首次回填确实被 exercise）.
  async function enterS1(): Promise<void> {
    render(<App />)
    fireEvent.click(await screen.findByText('s1'))
    await screen.findByTestId('chat-input')
    await waitFor(() => {
      expect(fetchMock.mock.calls.some((call) => call[0] === `/api/v2/${S1}/team/messages`)).toBe(
        true,
      )
      expect(fetchMock.mock.calls.some((call) => call[0] === `/api/v2/${S1}/team`)).toBe(true)
    })
  }

  // waitForPanelReady waits for the settings panel and its data sources with
  // positive fetch assertions（双 preset 池按 role 过滤 + models 同源面）.
  async function waitForPanelReady(): Promise<void> {
    expect(await screen.findByTestId('team-settings-panel')).toBeTruthy()
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v2/templates/saolei/presets?role=player',
        undefined,
      )
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v2/templates/saolei/presets?role=planner',
        undefined,
      )
      expect(fetchMock).toHaveBeenCalledWith('/api/v2/models', undefined)
    })
    await waitFor(() => {
      expect(
        (screen.getByTestId('team-player-preset-select') as HTMLSelectElement).querySelectorAll(
          'option',
        ).length,
      ).toBeGreaterThan(1)
    })
  }

  // applyPreset selects the fixture presets and clicks Apply（双 preset 必选）.
  function applyPreset(): void {
    fireEvent.change(screen.getByTestId('team-player-preset-select'), {
      target: { value: PLAYER_PRESET.name },
    })
    fireEvent.change(screen.getByTestId('team-planner-preset-select'), {
      target: { value: PLANNER_PRESET.name },
    })
    fireEvent.click(screen.getByTestId('team-apply'))
  }

  // assertPatchApplied positively asserts the UpdateTeam PATCH request shape
  // （team-api.md §1/§2：PATCH allow_missing=true，body 为 Team.members 输入
  // 列表）。已物化 panel 从 members 快照预选 player 的生效 model；首次物化
  // （无快照）传 '' = 不携带 model（部署默认）。
  async function assertPatchApplied(playerModel = 'glm-responses/glm-5.2'): Promise<void> {
    const playerMember =
      playerModel === ''
        ? { role: 'player', preset: PLAYER_PRESET.name }
        : { role: 'player', preset: PLAYER_PRESET.name, model: playerModel }
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${S1}/team?allow_missing=true`,
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({
            members: [
              playerMember,
              { role: 'planner', preset: PLANNER_PRESET.name },
            ],
          }),
        }),
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
    fireEvent.click(await screen.findByTestId('team-settings-button'))
    await waitForPanelReady()
    applyPreset()
    await assertPatchApplied()
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

    fireEvent.click(await screen.findByTestId('team-settings-button'))
    await waitForPanelReady()
    applyPreset()
    await assertPatchApplied()

    // Apply 成功触发第二次回填（重建同步动作，契约 §2.2）。
    await waitFor(() => {
      expect(historyCallCount()).toBe(2)
    })
    await waitFor(() => {
      expect(screen.queryByText('旧历史')).toBeNull()
    })
    // onApplied 既有语义零回归：面板关闭、状态已物化；无错误呈现。
    expect(screen.queryByTestId('team-settings-panel')).toBeNull()
    expect(screen.getByTestId('team-status').textContent).toContain('已物化')
    expect(screen.queryByTestId('chat-error')).toBeNull()
  })

  it('Apply 后紧随 send：刷新即清空旧序列，慢回填让位不覆盖新回合', async () => {
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
    expect(screen.getByText('旧历史')).toBeTruthy()

    fireEvent.click(await screen.findByTestId('team-settings-button'))
    await waitForPanelReady()
    applyPreset()
    await assertPatchApplied()
    // Apply 成功即新生命周期：旧序列本地即时清空（team-api.md §5），不等回填
    // （避免旧 seq 锚与新序列冲突）。
    expect(screen.queryByText('旧历史')).toBeNull()
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

    // 空回填落地但让位：live 与用户消息保持。
    backfillGate.open()
    await flush()
    expect(lastAgentText()).toBe('部')
    expect(screen.getByText('一')).toBeTruthy()
    expect(screen.queryByText('旧历史')).toBeNull()

    // 回合照常完成合并，回填内容仍不出现。
    s1Send.release()
    await s1Send.done
    await waitFor(() => {
      expect(lastAgentText()).toBe('部分')
    })
    expect(screen.getByText('一')).toBeTruthy()
    expect(screen.queryByText('旧历史')).toBeNull()
    expect(screen.queryByTestId('chat-error')).toBeNull()
  })

  it('Apply 成功后回填失败（500）：刷新已清空对话、回填错误呈现', async () => {
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

    fireEvent.click(await screen.findByTestId('team-settings-button'))
    await waitForPanelReady()
    applyPreset()
    await assertPatchApplied()

    // 回填失败 → 既有 backfillError 呈现（契约 §2.4）；新生命周期对话面
    // （刷新即时清空）保持为空、不回退旧序列。
    await waitFor(() => {
      expect(screen.getByTestId('chat-error').textContent).toContain('500')
    })
    expect(screen.queryByText('旧历史')).toBeNull()
    expect(screen.queryByTestId('team-settings-panel')).toBeNull()
    expect(screen.getByTestId('team-status').textContent).toContain('已物化')
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

    fireEvent.click(await screen.findByTestId('team-settings-button'))
    await waitForPanelReady()
    applyPreset()

    // 面板错误既有呈现（TeamSettingsPanel apply 的 catch 路径）。
    await waitFor(() => {
      expect(screen.getByTestId('team-settings-error').textContent).toContain('500')
    })
    await flush()
    // 回填触发只在 Apply 成功路径（契约 §2.1）：失败后无第二次回填。
    expect(historyCallCount()).toBe(1)
    expect(screen.getByTestId('team-settings-panel')).toBeTruthy()
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
      teamGet: () => new Response('not materialized', { status: 404 }),
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
    const guide = await screen.findByTestId('team-guide')
    expect(guide.textContent).toContain('该会话尚未物化 team')
    expect(screen.getByTestId('team-status').textContent).toBe('未物化')
    await waitFor(() => {
      expect(historyCallCount()).toBe(1)
    })

    // 引导入口打开面板并 Apply（web-views.md §1 引导流转；无物化快照 →
    // members 输入不含 model = 部署默认）。
    fireEvent.click(screen.getByTestId('team-guide-open'))
    await waitForPanelReady()
    applyPreset()
    await assertPatchApplied('')

    // Apply 成功：面板与引导消退（onApplied 既有语义零回归）。
    await waitFor(() => {
      expect(screen.queryByTestId('team-settings-panel')).toBeNull()
    })
    expect(screen.queryByTestId('team-guide')).toBeNull()
    expect(screen.getByTestId('team-status').textContent).toContain('已物化')

    // Apply 后回填触发且返回 200 空（team 已存在，research D1）→ 对话面为空。
    await waitFor(() => {
      expect(historyCallCount()).toBe(2)
    })
    assertCleanChat()
    expect(screen.queryByTestId('team-settings-error')).toBeNull()
  })

  it('刷新生命周期与在途成员回填竞态：旧生命周期的视角响应落地不复活', async () => {
    // 挂载时的成员视角回填被 gate 持有；Apply（新生命周期）后旧响应才落地：
    // 纪元守卫必须整体丢弃它——旧视角内容不得复活（merge 规则保留响应外
    // 条目的前提是同一生命周期）。
    const staleGate = gatedResponse(() =>
      jsonResponse({
        messages: [
          {
            message: {
              messageId: 'old-lifecycle',
              role: 'ROLE_AGENT',
              blocks: [{ text: { content: '旧生命周期视角内容' } }],
            },
            sender: 'player',
          },
        ],
      }),
    )
    const mock = makeApplyFetchMock({
      memberHistoryResponse: (member, call) => {
        if (member !== 'player') return jsonResponse({ messages: [] })
        // 第 1 次 = 挂载（旧生命周期，gate 持有）；第 2 次 = Apply 后重建。
        return call === 1 ? staleGate.respond() : jsonResponse({ messages: [] })
      },
    })
    fetchMock = mock.fetchMock
    historyCallCount = mock.historyCallCount
    vi.stubGlobal('fetch', fetchMock)

    await enterS1()
    fireEvent.click(await screen.findByTestId('team-settings-button'))
    await waitForPanelReady()
    applyPreset()
    await assertPatchApplied()

    // 旧响应在刷新后才落地：成员视角保持重建后的空序列。
    staleGate.open()
    await flush()
    fireEvent.click(screen.getByTestId('view-player'))
    expect(screen.queryByText('旧生命周期视角内容')).toBeNull()
  })
})

// ─── App 主界面 system prompt 入口（FR-009，
// ─── specs/059-agent-v2-team-mode/contracts/web-views.md §5：工具条成员清单
// ─── 点击 → GetTeamMember 全文只读浮层；设置面板内入口保留不变） ─────────────

describe('App 主界面 system prompt 入口', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  const PLAYER_PROMPT = 'player 完整 system prompt\n第二行'
  const PLANNER_PROMPT = 'planner 完整 system prompt'

  beforeEach(() => {
    fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      const method = init?.method ?? 'GET'
      if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
        return jsonResponse({ sessions: [{ name: S1, createTime: '2026-08-29T00:00:00Z' }] })
      }
      if (url === `/api/v2/${S1}/team` && method === 'GET') {
        return teamView(S1, { activeMember: 'planner' })
      }
      if (url === `/api/v2/${S1}/team/messages`) {
        return jsonResponse({ messages: [] })
      }
      if (
        url === `/api/v2/${S1}/team/members/player/messages` ||
        url === `/api/v2/${S1}/team/members/planner/messages`
      ) {
        return jsonResponse({ messages: [] })
      }
      if (url === `/api/v2/${S1}/team/members/player` && method === 'GET') {
        return jsonResponse({ role: 'player', systemPrompt: PLAYER_PROMPT })
      }
      if (url === `/api/v2/${S1}/team/members/planner` && method === 'GET') {
        return jsonResponse({ role: 'planner', systemPrompt: PLANNER_PROMPT })
      }
      // 设置面板数据面（互斥用例打开面板时消费）。
      if (url === '/api/v2/templates/saolei/presets?role=player' && method === 'GET') {
        return jsonResponse({
          presets: [
            { name: 'templates/saolei/presets/p-player', role: 'player', persona: 'p' },
          ],
        })
      }
      if (url === '/api/v2/templates/saolei/presets?role=planner' && method === 'GET') {
        return jsonResponse({
          presets: [
            { name: 'templates/saolei/presets/p-planner', role: 'planner', persona: 'q' },
          ],
        })
      }
      if (url === '/api/v2/models' && method === 'GET') {
        return jsonResponse({ models: [{ id: 'glm-responses/glm-5.2' }] })
      }
      throw new Error(`unexpected fetch: ${url} ${method}`)
    })
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('工具条成员清单每成员可点击：GetTeamMember 全文经只读浮层呈现，切换成员重新取数', async () => {
    render(<App />)
    fireEvent.click(await screen.findByText('s1'))

    const entries = await screen.findAllByTestId('team-member')
    expect(entries).toHaveLength(2)
    // 成员 chip 原样渲染复合标识（contracts/model-selection.md §4）。
    expect(entries[0]?.textContent).toContain('glm-responses/glm-5.2')
    // 主界面直接可见入口：无需打开设置面板。
    expect(screen.queryByTestId('team-settings-panel')).toBeNull()

    // 点击 player 入口：GET GetTeamMember 取实际装配结果（mock 正向断言），
    // 只读等宽全文 <pre> 呈现。
    fireEvent.click(entries[0] as HTMLElement)
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(`/api/v2/${S1}/team/members/player`, undefined)
    })
    const pre = await screen.findByTestId('system-prompt-text')
    expect(pre.textContent).toBe(PLAYER_PROMPT)
    expect(screen.getByTestId('system-prompt-title').textContent).toContain('player')

    // 切换另一成员：重新取数并更新全文。
    fireEvent.click(entries[1] as HTMLElement)
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(`/api/v2/${S1}/team/members/planner`, undefined)
    })
    await waitFor(() => {
      expect(screen.getByTestId('system-prompt-text').textContent).toBe(PLANNER_PROMPT)
    })

    // 关闭入口收起浮层。
    fireEvent.click(screen.getByTestId('system-prompt-close'))
    expect(screen.queryByTestId('system-prompt')).toBeNull()
  })

  it('工具条入口与设置面板入口互斥：共享单一浮层，后打开者替换先打开者', async () => {
    render(<App />)
    fireEvent.click(await screen.findByText('s1'))

    // 工具条 player 入口打开浮层：DOM 恰一个 system-prompt。
    const entries = await screen.findAllByTestId('team-member')
    fireEvent.click(entries[0] as HTMLElement)
    await waitFor(() => {
      expect(screen.getByTestId('system-prompt').getAttribute('data-role')).toBe('player')
    })
    expect(screen.getAllByTestId('system-prompt')).toHaveLength(1)

    // 打开设置面板并点击面板内 planner 入口：后打开者替换前者，
    // DOM 仍恰一个浮层（无重复 testid/无叠加）。
    fireEvent.click(screen.getByTestId('team-settings-button'))
    await screen.findByTestId('team-settings-panel')
    fireEvent.click(await screen.findByTestId('member-system-prompt-planner'))
    await waitFor(() => {
      expect(screen.getByTestId('system-prompt').getAttribute('data-role')).toBe('planner')
    })
    expect(screen.getAllByTestId('system-prompt')).toHaveLength(1)
    await waitFor(() => {
      expect(screen.getByTestId('system-prompt-text').textContent).toBe(PLANNER_PROMPT)
    })

    // 反向：面板开启期间再点工具条 player 入口，同样只保留一个浮层。
    fireEvent.click(entries[0] as HTMLElement)
    await waitFor(() => {
      expect(screen.getByTestId('system-prompt').getAttribute('data-role')).toBe('player')
    })
    expect(screen.getAllByTestId('system-prompt')).toHaveLength(1)
    await waitFor(() => {
      expect(screen.getByTestId('system-prompt-text').textContent).toBe(PLAYER_PROMPT)
    })
  })
})

// ─── App 双视图切换（T030：恰 3 视图、成员视角按回填渲染、切换不重填） ─────────

describe('App 双视图切换', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('恰 3 个视图；成员视角按回填渲染 user/relay/agent；切换不触发重新回填（各视图历史常驻）', async () => {
    const playerView = {
      messages: [
        {
          message: { role: 'ROLE_USER', blocks: [{ text: { content: '开始一局' } }] },
          sender: 'user',
        },
        {
          message: { role: 'ROLE_AGENT', blocks: [{ text: { content: '落子 a1' } }] },
          sender: 'player',
        },
        {
          message: {
            role: 'ROLE_USER',
            blocks: [
              {
                text: {
                  content: '<planner-message>\n先开左上角\n</planner-message>',
                },
              },
            ],
          },
          sender: 'planner',
        },
      ],
    }
    const plannerView = {
      messages: [
        {
          message: { role: 'ROLE_USER', blocks: [{ text: { content: '开始一局' } }] },
          sender: 'user',
        },
        {
          message: { role: 'ROLE_AGENT', blocks: [{ text: { content: '先开左上角' } }] },
          sender: 'planner',
        },
      ],
    }
    const fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      const method = init?.method ?? 'GET'
      if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
        return jsonResponse({ sessions: [{ name: S1, createTime: '2026-08-29T00:00:00Z' }] })
      }
      if (url === `/api/v2/${S1}/team/messages`) {
        return jsonResponse({ messages: [] })
      }
      if (url === `/api/v2/${S1}/team/members/player/messages`) {
        return jsonResponse(playerView)
      }
      if (url === `/api/v2/${S1}/team/members/planner/messages`) {
        return jsonResponse(plannerView)
      }
      if (url === `/api/v2/${S1}/team` && method === 'GET') {
        return teamView(S1)
      }
      throw new Error(`unexpected fetch: ${url} ${method}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    fireEvent.click(await screen.findByText('s1'))
    const switcher = await screen.findByTestId('chat-view-switch')
    // 恰 3 个视图：团队 | player | planner；缺省团队视图。
    expect(switcher.querySelectorAll('button')).toHaveLength(3)
    expect(screen.getByTestId('view-team').getAttribute('data-active')).toBe('true')

    // player 视角：回填序列按 `user 气泡 / agent 输出 / user: [sender] 标注` 渲染；
    // relay 正文为注入原文（标签对原样、不剥离，正文仅出现一次——
    // specs/060-agent-v2-team-optimize/contracts/team-api.md §4）。
    fireEvent.click(screen.getByTestId('view-player'))
    expect(screen.getByTestId('view-player').getAttribute('data-active')).toBe('true')
    expect(screen.getByTestId('relay-source').textContent).toBe('user: [planner]')
    expect(screen.getByTestId('relay-body').textContent).toBe(
      '<planner-message>\n先开左上角\n</planner-message>',
    )
    expect(screen.getByTestId('agent-text').textContent).toBe('落子 a1')

    // planner 视角：用户消息 + 自己的输出，无 relay 条目。
    fireEvent.click(screen.getByTestId('view-planner'))
    expect(screen.getByTestId('agent-text').textContent).toBe('先开左上角')
    expect(screen.queryByTestId('member-relay')).toBeNull()

    // 切回团队视图：数据面即时恢复且切换不触发任何回填请求——成员视角各恰
    // 一次挂载回填（web-views.md §2：切换为纯前端状态、各视图历史常驻）。
    fireEvent.click(screen.getByTestId('view-team'))
    expect(screen.getByTestId('view-team').getAttribute('data-active')).toBe('true')
    const memberCalls = fetchMock.mock.calls.filter((call) =>
      String(call[0]).includes('/members/'),
    )
    expect(memberCalls).toHaveLength(2)
  })
})
