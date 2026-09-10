// @vitest-environment jsdom
// TeamSettingsPanel 组件测试（specs/059-agent-v2-team-mode/contracts/
// web-views.md §1/§6）：下拉数据源（按 role 字符串过滤的双 preset + models）、
// preset 必选校验、Apply 请求形状（UpdateTeam members 输入列表）、刷新语义
// 提示、未物化引导态流转（经 App 全流程驱动）与物化后首驱提示。
// Mock 约定照 style/javascript.md：vi.fn() double + 正向断言。
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '../App.js'
import { TeamSettingsPanel } from './TeamSettingsPanel.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

const SESSION = 'templates/saolei/sessions/s1'
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
const TEAM_MATERIALIZED = {
  name: `${SESSION}/team`,
  members: [
    {
      name: `${SESSION}/team/members/player`,
      role: 'player',
      preset: PLAYER_PRESET.name,
      model: 'glm-5.2',
    },
    {
      name: `${SESSION}/team/members/planner`,
      role: 'planner',
      preset: PLANNER_PRESET.name,
      model: 'glm-5.1',
    },
  ],
  createTime: '2026-08-29T01:00:00Z',
  updateTime: '2026-08-29T01:00:00Z',
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('TeamSettingsPanel', () => {
  let fetchMock: ReturnType<typeof vi.fn>
  let onApplied: ReturnType<typeof vi.fn>
  let onClose: ReturnType<typeof vi.fn>

  beforeEach(() => {
    onApplied = vi.fn()
    onClose = vi.fn()
    fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      const method = init?.method ?? 'GET'
      if (url === '/api/v2/templates/saolei/presets?role=player' && method === 'GET') {
        return jsonResponse({ presets: [PLAYER_PRESET] })
      }
      if (url === '/api/v2/templates/saolei/presets?role=planner' && method === 'GET') {
        return jsonResponse({ presets: [PLANNER_PRESET] })
      }
      if (url === '/api/v2/models' && method === 'GET') {
        return jsonResponse({
          models: [{ id: 'glm-5.2', contextWindow: 128000 }, { id: 'glm-5.1' }],
        })
      }
      if (url === `/api/v2/${SESSION}/team?allow_missing=true` && method === 'PATCH') {
        const body = JSON.parse(String(init?.body)) as { members: unknown[] }
        return jsonResponse({ ...TEAM_MATERIALIZED, members: body.members })
      }
      throw new Error(`unexpected fetch: ${url} ${method}`)
    })
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('下拉数据源：双 preset 池按 role 字符串过滤 + model 列表含「默认」项', async () => {
    render(
      <TeamSettingsPanel session={SESSION} materialized={null} onApplied={onApplied} onClose={onClose} />,
    )

    // 数据源请求齐发（web-views.md §1：三个下拉同属配置面；preset 按角色池
    // 过滤——preset-api.md §1 role 过滤参数为字符串）。
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

    const playerSelect = await screen.findByTestId('team-player-preset-select')
    const plannerSelect = screen.getByTestId('team-planner-preset-select')
    expect(
      Array.from(playerSelect.querySelectorAll('option')).map((o) => o.textContent),
    ).toContain('p-player')
    expect(
      Array.from(playerSelect.querySelectorAll('option')).map((o) => o.textContent),
    ).not.toContain('p-planner')
    expect(
      Array.from(plannerSelect.querySelectorAll('option')).map((o) => o.textContent),
    ).toContain('p-planner')
    expect(
      Array.from(plannerSelect.querySelectorAll('option')).map((o) => o.textContent),
    ).not.toContain('p-player')

    const playerModelSelect = screen.getByTestId('team-player-model-select')
    const plannerModelSelect = screen.getByTestId('team-planner-model-select')
    expect(Array.from(playerModelSelect.querySelectorAll('option')).map((o) => o.textContent)).toEqual([
      '默认',
      'glm-5.2',
      'glm-5.1',
    ])
    expect(
      Array.from(plannerModelSelect.querySelectorAll('option')).map((o) => o.textContent),
    ).toEqual(['默认', 'glm-5.2', 'glm-5.1'])
  })

  it('双 preset 必选校验：逐个提示且不发 PATCH', async () => {
    render(
      <TeamSettingsPanel session={SESSION} materialized={null} onApplied={onApplied} onClose={onClose} />,
    )
    fireEvent.click(await screen.findByTestId('team-apply'))
    expect((await screen.findByTestId('team-settings-error')).textContent).toContain(
      '必须选择一个 player preset',
    )

    fireEvent.change(screen.getByTestId('team-player-preset-select'), {
      target: { value: PLAYER_PRESET.name },
    })
    fireEvent.click(screen.getByTestId('team-apply'))
    expect((await screen.findByTestId('team-settings-error')).textContent).toContain(
      '必须选择一个 planner preset',
    )

    expect(onApplied).not.toHaveBeenCalled()
    expect(
      fetchMock.mock.calls.filter((c) => (c[1] as RequestInit | undefined)?.method === 'PATCH'),
    ).toHaveLength(0)
  })

  it('Apply 请求形状：PATCH allow_missing=true，body 为 members 输入列表（默认模型省略）', async () => {
    render(
      <TeamSettingsPanel session={SESSION} materialized={null} onApplied={onApplied} onClose={onClose} />,
    )

    await screen.findByTestId('team-player-preset-select')
    fireEvent.change(screen.getByTestId('team-player-preset-select'), {
      target: { value: PLAYER_PRESET.name },
    })
    fireEvent.change(screen.getByTestId('team-planner-preset-select'), {
      target: { value: PLANNER_PRESET.name },
    })
    // 默认模型：model 省略（team-api.md §2 members[].model 空 = 部署默认）。
    fireEvent.click(screen.getByTestId('team-apply'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${SESSION}/team?allow_missing=true`,
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({
            members: [
              { role: 'player', preset: PLAYER_PRESET.name },
              { role: 'planner', preset: PLANNER_PRESET.name },
            ],
          }),
        }),
      )
    })
    await waitFor(() => {
      expect(onApplied).toHaveBeenCalledWith(expect.objectContaining({ name: TEAM_MATERIALIZED.name }))
    })

    // 指定模型：两条成员配置各携带 model。
    fireEvent.change(screen.getByTestId('team-player-model-select'), {
      target: { value: 'glm-5.2' },
    })
    fireEvent.change(screen.getByTestId('team-planner-model-select'), {
      target: { value: 'glm-5.1' },
    })
    fireEvent.click(screen.getByTestId('team-apply'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenLastCalledWith(
        `/api/v2/${SESSION}/team?allow_missing=true`,
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({
            members: [
              { role: 'player', preset: PLAYER_PRESET.name, model: 'glm-5.2' },
              { role: 'planner', preset: PLANNER_PRESET.name, model: 'glm-5.1' },
            ],
          }),
        }),
      )
    })
  })

  it('已物化：从 members 快照预选当前配置并呈现刷新语义提示；未物化无提示', async () => {
    const { rerender } = render(
      <TeamSettingsPanel
        session={SESSION}
        materialized={TEAM_MATERIALIZED}
        onApplied={onApplied}
        onClose={onClose}
      />,
    )

    expect(screen.getByTestId('team-refresh-hint').textContent).toContain('终止在途回合')
    expect(screen.getByTestId('team-refresh-hint').textContent).toContain('清空短期记忆')
    await waitFor(() => {
      expect((screen.getByTestId('team-player-preset-select') as HTMLSelectElement).value).toBe(
        PLAYER_PRESET.name,
      )
    })
    expect((screen.getByTestId('team-planner-preset-select') as HTMLSelectElement).value).toBe(
      PLANNER_PRESET.name,
    )
    expect((screen.getByTestId('team-player-model-select') as HTMLSelectElement).value).toBe('glm-5.2')
    expect((screen.getByTestId('team-planner-model-select') as HTMLSelectElement).value).toBe('glm-5.1')

    rerender(
      <TeamSettingsPanel session={SESSION} materialized={null} onApplied={onApplied} onClose={onClose} />,
    )
    expect(screen.queryByTestId('team-refresh-hint')).toBeNull()
  })

  it('数据源失败呈现错误', async () => {
    fetchMock = vi.fn(async () => new Response('boom', { status: 500 }))
    vi.stubGlobal('fetch', fetchMock)
    render(
      <TeamSettingsPanel session={SESSION} materialized={null} onApplied={onApplied} onClose={onClose} />,
    )
    expect((await screen.findByTestId('team-settings-error')).textContent).toContain('boom')
  })
})

// ─── 未物化引导态流转与首驱提示（web-views.md §1） ───────────────────────────

function wireChunk(eventLine: string): string {
  return `{"result":${eventLine}}\n`
}

function memberWire(member: string, turnId: string, event: Record<string, unknown>): string {
  return wireChunk(JSON.stringify({ member, turnId, ...event }))
}

function teamEventWire(member: string, seq: number, content: string, role: string): string {
  return wireChunk(
    JSON.stringify({
      teamMessage: {
        member,
        message: { role, blocks: [{ text: { content } }] },
        seq: String(seq),
      },
    }),
  )
}

describe('App 未物化引导与 team 物化', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    // 物化状态标记：PATCH（UpdateTeam）成功前 session 未物化；成功后 team
    // 已存在且归并序列随清理重建为空——ListTeamMessages 未物化 404、物化后
    // 恒 200 空集合（team-api.md §1/§5）。
    let materialized = false
    fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      const method = init?.method ?? 'GET'
      if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
        return jsonResponse({ sessions: [{ name: SESSION, createTime: '2026-08-29T00:00:00Z' }] })
      }
      // 未物化：GetTeam 404（team-api.md §1）。
      if (url === `/api/v2/${SESSION}/team` && method === 'GET') {
        return materialized
          ? jsonResponse(TEAM_MATERIALIZED)
          : new Response('not materialized', { status: 404 })
      }
      // ListTeamMessages 按物化状态分流：未物化 404；物化成功后 team 已
      // 存在、服务端归并序列为空（team-api.md §1/§5）。
      if (url === `/api/v2/${SESSION}/team/messages` && method === 'GET') {
        return materialized
          ? jsonResponse({ messages: [] })
          : new Response('not found', { status: 404 })
      }
      if (url === '/api/v2/templates/saolei/presets?role=player' && method === 'GET') {
        return jsonResponse({ presets: [PLAYER_PRESET] })
      }
      if (url === '/api/v2/templates/saolei/presets?role=planner' && method === 'GET') {
        return jsonResponse({ presets: [PLANNER_PRESET] })
      }
      if (url === '/api/v2/models' && method === 'GET') {
        return jsonResponse({ models: [{ id: 'glm-5.2' }] })
      }
      if (url === `/api/v2/${SESSION}/team?allow_missing=true` && method === 'PATCH') {
        materialized = true
        return jsonResponse(TEAM_MATERIALIZED)
      }
      // 物化后的 team 流：用户消息 team_message 帧 + planner 回合成员事件帧
      // 与固化帧（team-api.md §3.2 双帧承载；member 为角色字符串）。
      if (url === `/api/v2/${SESSION}:send` && method === 'POST') {
        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            const encoder = new TextEncoder()
            controller.enqueue(
              encoder.encode(
                teamEventWire('user', 1, '你好', 'ROLE_USER') +
                  memberWire('planner', 't1', { turnStart: {} }) +
                  memberWire('planner', 't1', {
                    blockStart: { index: 0, type: 'BLOCK_TYPE_TEXT' },
                  }) +
                  memberWire('planner', 't1', { delta: { index: 0, text: '好的' } }) +
                  memberWire('planner', 't1', {
                    blockEnd: { index: 0, block: { text: { content: '好的' } } },
                  }) +
                  teamEventWire('planner', 2, '好的', 'ROLE_AGENT') +
                  memberWire('planner', 't1', {
                    turnEnd: { status: 'TURN_STATUS_COMPLETED' },
                  }),
              ),
            )
            controller.close()
          },
        })
        return new Response(stream, { status: 200, headers: { 'Content-Type': 'application/json' } })
      }
      throw new Error(`unexpected fetch: ${url} ${method}`)
    })
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('未物化 session 进入引导态 → 物化 → 首驱提示呈现且可发送（US2 场景 1/2）', async () => {
    render(<App />)

    // 打开 session：GetTeam 404 → 未物化引导态呈现。
    fireEvent.click(await screen.findByText('s1'))
    const guide = await screen.findByTestId('team-guide')
    expect(guide.textContent).toContain('该会话尚未物化 team')
    expect(screen.getByTestId('team-status').textContent).toBe('未物化')

    // 引导入口打开物化面板。
    fireEvent.click(screen.getByTestId('team-guide-open'))
    expect(await screen.findByTestId('team-settings-panel')).toBeTruthy()
    expect(screen.queryByTestId('team-guide')).toBeNull()

    // 选择双 preset（均必选），Apply = UpdateTeam。
    await waitFor(() => {
      expect(
        (screen.getByTestId('team-player-preset-select') as HTMLSelectElement).querySelectorAll('option')
          .length,
      ).toBeGreaterThan(1)
    })
    fireEvent.change(screen.getByTestId('team-player-preset-select'), {
      target: { value: PLAYER_PRESET.name },
    })
    fireEvent.change(screen.getByTestId('team-planner-preset-select'), {
      target: { value: PLANNER_PRESET.name },
    })
    fireEvent.click(screen.getByTestId('team-apply'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${SESSION}/team?allow_missing=true`,
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({
            members: [
              { role: 'player', preset: PLAYER_PRESET.name },
              { role: 'planner', preset: PLANNER_PRESET.name },
            ],
          }),
        }),
      )
    })

    // 物化成功：面板关闭、引导消失、状态呈现已物化、成员清单呈现。
    await waitFor(() => {
      expect(screen.queryByTestId('team-settings-panel')).toBeNull()
    })
    expect(screen.queryByTestId('team-guide')).toBeNull()
    expect(screen.getByTestId('team-status').textContent).toContain('已物化')
    await waitFor(() => {
      expect(screen.getAllByTestId('team-member')).toHaveLength(2)
    })
    expect(screen.getByTestId('team-members').textContent).toContain('player')
    expect(screen.getByTestId('team-members').textContent).toContain('planner')

    // 用户首驱裁定：物化后静止等待提示呈现；发送第一条消息触发 planner。
    expect(screen.getByTestId('team-ready-guide').textContent).toContain('发送第一条消息')
    fireEvent.change(screen.getByTestId('chat-input'), { target: { value: '你好' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${SESSION}:send`,
        expect.objectContaining({ method: 'POST' }),
      )
    })
    await waitFor(() => {
      expect(screen.getByTestId('agent-text').textContent).toBe('好的')
    })
    // 成员标签区分为 planner；用户消息经 team_message 帧进入团队视图。
    expect(screen.getAllByTestId('member-tag')[0]?.getAttribute('data-member')).toBe('planner')
    expect(screen.getByText('你好')).toBeTruthy()
    // 首次驱动已发生：首驱提示消退（对话非空）。
    await waitFor(() => {
      expect(screen.queryByTestId('team-ready-guide')).toBeNull()
    })
  })
})
