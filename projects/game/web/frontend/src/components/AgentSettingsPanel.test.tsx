// @vitest-environment jsdom
// AgentSettingsPanel 组件测试（specs/051-agent-v2-dsh-migration/contracts/
// web-frontend.md §6 测试义务 3）：下拉数据源（presets/models）、preset 必选
// 校验、Apply 请求形状、未物化引导态流转（US2 场景 5，经 App 全流程驱动）。
// Mock 约定照 style/javascript.md：vi.fn() double + 正向断言。
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '../App.js'
import { AgentSettingsPanel } from './AgentSettingsPanel.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

const SESSION = 'templates/saolei/sessions/s1'
const PRESET_P1 = {
  name: 'templates/saolei/presets/p1',
  persona: '你是扫雷玩家',
  createTime: '2026-08-29T00:00:00Z',
  updateTime: '2026-08-29T00:00:00Z',
}
const AGENT_MATERIALIZED = {
  name: `${SESSION}/agent`,
  preset: PRESET_P1.name,
  model: 'glm-5.2',
  createTime: '2026-08-29T01:00:00Z',
  updateTime: '2026-08-29T01:00:00Z',
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('AgentSettingsPanel', () => {
  let fetchMock: ReturnType<typeof vi.fn>
  let onApplied: ReturnType<typeof vi.fn>
  let onClose: ReturnType<typeof vi.fn>

  beforeEach(() => {
    onApplied = vi.fn()
    onClose = vi.fn()
    fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      const method = init?.method ?? 'GET'
      if (url === '/api/v2/templates/saolei/presets' && method === 'GET') {
        return jsonResponse({ presets: [PRESET_P1] })
      }
      if (url === '/api/v2/models' && method === 'GET') {
        return jsonResponse({ models: [{ id: 'glm-5.2', contextWindow: 128000 }] })
      }
      if (url === `/api/v2/${SESSION}/agent?allow_missing=true` && method === 'PATCH') {
        const body = JSON.parse(String(init?.body)) as { preset: string; model?: string }
        return jsonResponse({ ...AGENT_MATERIALIZED, preset: body.preset, model: body.model })
      }
      throw new Error(`unexpected fetch: ${url} ${method}`)
    })
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('下拉数据源：preset 列表 + model 列表含「默认」项', async () => {
    render(
      <AgentSettingsPanel session={SESSION} materialized={null} onApplied={onApplied} onClose={onClose} />,
    )

    // 数据源请求齐发（web-frontend.md §3：两个下拉同属 PresetService 面）。
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/v2/templates/saolei/presets', undefined)
      expect(fetchMock).toHaveBeenCalledWith('/api/v2/models', undefined)
    })

    const presetSelect = await screen.findByTestId('agent-preset-select')
    const modelSelect = screen.getByTestId('agent-model-select')
    const presetText = Array.from(presetSelect.querySelectorAll('option')).map((o) => o.textContent)
    const modelText = Array.from(modelSelect.querySelectorAll('option')).map((o) => o.textContent)
    expect(presetText).toContain('p1')
    expect(modelText).toEqual(['默认', 'glm-5.2'])
  })

  it('preset 必选校验：未选择时提示且不发 PATCH', async () => {
    render(
      <AgentSettingsPanel session={SESSION} materialized={null} onApplied={onApplied} onClose={onClose} />,
    )
    fireEvent.click(await screen.findByTestId('agent-apply'))

    expect((await screen.findByTestId('agent-settings-error')).textContent).toContain('必须选择一个 preset')
    expect(onApplied).not.toHaveBeenCalled()
    expect(fetchMock.mock.calls.filter((c) => (c[1] as RequestInit | undefined)?.method === 'PATCH')).toHaveLength(0)
  })

  it('Apply 请求形状：PATCH allow_missing=true，body 仅 {preset, model}（默认模型省略 model）', async () => {
    render(
      <AgentSettingsPanel session={SESSION} materialized={null} onApplied={onApplied} onClose={onClose} />,
    )

    await screen.findByTestId('agent-preset-select')
    fireEvent.change(screen.getByTestId('agent-preset-select'), { target: { value: PRESET_P1.name } })
    // 默认模型：model 省略（agent-api.md §1 Agent.model 空 = 进程默认）。
    fireEvent.click(screen.getByTestId('agent-apply'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${SESSION}/agent?allow_missing=true`,
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({ preset: PRESET_P1.name }),
        }),
      )
    })
    await waitFor(() => {
      expect(onApplied).toHaveBeenCalledWith(expect.objectContaining({ name: AGENT_MATERIALIZED.name }))
    })

    // 指定模型：body 携带 model。
    fireEvent.change(screen.getByTestId('agent-model-select'), { target: { value: 'glm-5.2' } })
    fireEvent.click(screen.getByTestId('agent-apply'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenLastCalledWith(
        `/api/v2/${SESSION}/agent?allow_missing=true`,
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({ preset: PRESET_P1.name, model: 'glm-5.2' }),
        }),
      )
    })
  })

  it('已物化：预选当前配置并呈现刷新语义提示；未物化无提示', async () => {
    const { rerender } = render(
      <AgentSettingsPanel session={SESSION} materialized={AGENT_MATERIALIZED} onApplied={onApplied} onClose={onClose} />,
    )

    expect(screen.getByTestId('agent-refresh-hint').textContent).toContain('清空短期记忆')
    await waitFor(() => {
      expect((screen.getByTestId('agent-preset-select') as HTMLSelectElement).value).toBe(PRESET_P1.name)
    })
    expect((screen.getByTestId('agent-model-select') as HTMLSelectElement).value).toBe('glm-5.2')

    rerender(
      <AgentSettingsPanel session={SESSION} materialized={null} onApplied={onApplied} onClose={onClose} />,
    )
    expect(screen.queryByTestId('agent-refresh-hint')).toBeNull()
  })

  it('数据源失败呈现错误', async () => {
    fetchMock = vi.fn(async () => new Response('boom', { status: 500 }))
    vi.stubGlobal('fetch', fetchMock)
    render(
      <AgentSettingsPanel session={SESSION} materialized={null} onApplied={onApplied} onClose={onClose} />,
    )
    expect((await screen.findByTestId('agent-settings-error')).textContent).toContain('boom')
  })
})

// ─── 未物化引导态流转（web-frontend.md §3，US2 场景 5） ─────────────────────

function wireChunk(eventLine: string): string {
  return `{"result":${eventLine}}\n`
}

describe('App 未物化引导', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    // 物化状态标记：PATCH（UpdateAgent）成功前 session 未物化；成功后 agent
    // 已存在且历史随清理重建清空——ListAgentMessages 未物化 404、物化后恒
    // 200 空集合（specs/051-agent-v2-dsh-migration/contracts/agent-api.md
    // §2.1/§2.3）。
    let materialized = false
    fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      const method = init?.method ?? 'GET'
      if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
        return jsonResponse({ sessions: [{ name: SESSION, createTime: '2026-08-29T00:00:00Z' }] })
      }
      // 未物化：GetAgent 404（agent-api.md §2.2）。
      if (url === `/api/v2/${SESSION}/agent` && method === 'GET') {
        return new Response('not materialized', { status: 404 })
      }
      // ListAgentMessages 按物化状态分流：未物化 404；物化成功后 agent 已
      // 存在、服务端历史为空（specs/051-agent-v2-dsh-migration/contracts/
      // agent-api.md §2.1/§2.3；specs/057-agent-v2-ui-fixes-2/contracts/
      // ui-interactions.md §2.7）。
      if (url === `/api/v2/${SESSION}/agent/messages` && method === 'GET') {
        return materialized
          ? jsonResponse({ messages: [] })
          : new Response('not found', { status: 404 })
      }
      if (url === '/api/v2/templates/saolei/presets' && method === 'GET') {
        return jsonResponse({ presets: [PRESET_P1] })
      }
      if (url === '/api/v2/models' && method === 'GET') {
        return jsonResponse({ models: [{ id: 'glm-5.2' }] })
      }
      if (url === `/api/v2/${SESSION}/agent?allow_missing=true` && method === 'PATCH') {
        materialized = true
        return jsonResponse(AGENT_MATERIALIZED)
      }
      // 物化后的正常回合。
      if (url === `/api/v2/${SESSION}:send` && method === 'POST') {
        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            const encoder = new TextEncoder()
            controller.enqueue(
              encoder.encode(
                wireChunk('{"turnId":"t1","turnStart":{}}') +
                  wireChunk('{"turnId":"t1","blockStart":{"index":0,"type":"BLOCK_TYPE_TEXT"}}') +
                  wireChunk('{"turnId":"t1","delta":{"index":0,"text":"好的"}}') +
                  wireChunk('{"turnId":"t1","blockEnd":{"index":0,"block":{"text":{"content":"好的"}}}}') +
                  wireChunk('{"turnId":"t1","turnEnd":{"status":"TURN_STATUS_COMPLETED"}}'),
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

  it('未物化 session 进入引导态 → 面板物化 → 引导消失且可发送（US2 场景 5）', async () => {
    render(<App />)

    // 打开 session：GetAgent 404 → 未物化引导态呈现。
    fireEvent.click(await screen.findByText('s1'))
    const guide = await screen.findByTestId('agent-guide')
    expect(guide.textContent).toContain('该会话尚未设置 agent')
    expect(screen.getByTestId('agent-status').textContent).toBe('未物化')

    // 引导入口打开物化面板。
    fireEvent.click(screen.getByTestId('agent-guide-open'))
    expect(await screen.findByTestId('agent-settings-panel')).toBeTruthy()
    expect(screen.queryByTestId('agent-guide')).toBeNull()

    // 选择 preset（必选）与默认模型，Apply = UpdateAgent。
    await waitFor(() => {
      expect((screen.getByTestId('agent-preset-select') as HTMLSelectElement).querySelectorAll('option').length).toBeGreaterThan(1)
    })
    fireEvent.change(screen.getByTestId('agent-preset-select'), { target: { value: PRESET_P1.name } })
    fireEvent.click(screen.getByTestId('agent-apply'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/v2/${SESSION}/agent?allow_missing=true`,
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({ preset: PRESET_P1.name }),
        }),
      )
    })

    // 物化成功：面板关闭、引导消失、状态呈现已物化。
    await waitFor(() => {
      expect(screen.queryByTestId('agent-settings-panel')).toBeNull()
    })
    expect(screen.queryByTestId('agent-guide')).toBeNull()
    expect(screen.getByTestId('agent-status').textContent).toContain('已物化')

    // 物化后可正常发送（回合完成并呈现回复）。
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
  })
})
