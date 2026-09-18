// api/agent.js 的 fetch mock 单测：断言每个方法的 URL/method/body 请求形状
// 与响应投影（specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §6
// 测试义务 2/3 的客户端面）。Mock 约定照 style/javascript.md：vi.fn()
// test-double + 对被拦截调用做正向断言。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  cancelAgent,
  createPreset,
  deletePreset,
  getAgent,
  getPreset,
  listModels,
  listPresets,
  updateAgent,
  updatePreset,
} from './agent.js'
import { ApiError } from './conversation.js'

afterEach(() => {
  vi.unstubAllGlobals()
})

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

// route 方法级 DELETE 等无 body 响应。
function emptyResponse(status = 200): Response {
  return new Response('{}', { status })
}

describe('agent api 客户端', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      throw new Error(`unexpected fetch: ${url} ${init?.method ?? 'GET'}`)
    })
    vi.stubGlobal('fetch', fetchMock)
  })

  it('listPresets GET 模板 preset 集合并返回数组（空集合 → []）', async () => {
    fetchMock.mockImplementation(async () => jsonResponse({ presets: [] }))
    await expect(listPresets('saolei')).resolves.toEqual([])
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/presets',
      undefined,
    )

    fetchMock.mockImplementation(async () =>
      jsonResponse({
        presets: [
          {
            name: 'templates/saolei/presets/p1',
            playerPrompt: '你是扫雷玩家',
            createTime: '2026-08-29T00:00:00Z',
            updateTime: '2026-08-29T01:00:00Z',
          },
        ],
      }),
    )
    const presets = await listPresets('saolei')
    expect(presets).toHaveLength(1)
    expect(presets[0].name).toBe('templates/saolei/presets/p1')
    expect(presets[0].playerPrompt).toBe('你是扫雷玩家')
  })

  it('createPreset POST，caller-supplied id 走 query，body 为 Preset 本体', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: 'templates/saolei/presets/p1', playerPrompt: 'p' }),
    )
    const created = await createPreset('saolei', 'p1', '你是扫雷玩家')
    expect(created.name).toBe('templates/saolei/presets/p1')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/presets?preset_id=p1',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ playerPrompt: '你是扫雷玩家' }),
      }),
    )
  })

  it('getPreset GET 完整资源名', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: 'templates/saolei/presets/p1', playerPrompt: 'x' }),
    )
    await getPreset('templates/saolei/presets/p1')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/presets/p1',
      undefined,
    )
  })

  it('updatePreset PATCH 携带 update_mask=player_prompt 与新内容', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: 'templates/saolei/presets/p1', playerPrompt: '新的' }),
    )
    await updatePreset('templates/saolei/presets/p1', '新的')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/presets/p1?update_mask=player_prompt',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ playerPrompt: '新的' }),
      }),
    )
  })

  it('deletePreset DELETE 完整资源名，失败抛 ApiError', async () => {
    fetchMock.mockImplementation(async () => emptyResponse())
    await expect(deletePreset('templates/saolei/presets/p1')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/presets/p1',
      expect.objectContaining({ method: 'DELETE' }),
    )

    fetchMock.mockImplementation(async () => emptyResponse(404))
    await expect(deletePreset('templates/saolei/presets/none')).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
    })
  })

  it('listModels GET 部署级目录', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ models: [{ id: 'glm-5.2', contextWindow: 128000 }] }),
    )
    const models = await listModels()
    expect(models).toEqual([{ id: 'glm-5.2', contextWindow: 128000 }])
    expect(fetchMock).toHaveBeenCalledWith('/api/v2/models', undefined)
  })

  it('getAgent GET session 的 agent 单例', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        name: 'templates/saolei/sessions/s1/agent',
        preset: 'templates/saolei/presets/p1',
        model: 'glm-5.2',
      }),
    )
    const agent = await getAgent('templates/saolei/sessions/s1')
    expect(agent.name).toBe('templates/saolei/sessions/s1/agent')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/sessions/s1/agent',
      undefined,
    )
  })

  it('updateAgent PATCH allow_missing=true，body 仅可变字段且 model 空时省略', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: 'templates/saolei/sessions/s1/agent', preset: 'templates/saolei/presets/p1' }),
    )
    await updateAgent('templates/saolei/sessions/s1', 'templates/saolei/presets/p1', '')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/sessions/s1/agent?allow_missing=true',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ preset: 'templates/saolei/presets/p1' }),
      }),
    )

    fetchMock.mockImplementation(async () =>
      jsonResponse({
        name: 'templates/saolei/sessions/s1/agent',
        preset: 'templates/saolei/presets/p1',
        model: 'glm-5.2',
      }),
    )
    await updateAgent('templates/saolei/sessions/s1', 'templates/saolei/presets/p1', 'glm-5.2')
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v2/templates/saolei/sessions/s1/agent?allow_missing=true',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({
          preset: 'templates/saolei/presets/p1',
          model: 'glm-5.2',
        }),
      }),
    )
  })

  it('cancelAgent POST {session}/agent:cancel，body 空对象；未物化失败抛 ApiError(400)', async () => {
    // AIP-136 自定义方法（specs/054-agent-v2-bugfixes/contracts/
    // agent-api-changes.md §3）：请求仅 name 路径参数，body:"*" 下 body 为
    // 空对象；幂等 no-op 同样 200。
    fetchMock.mockImplementation(async () => jsonResponse({}))
    await expect(cancelAgent('templates/saolei/sessions/s1')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/sessions/s1/agent:cancel',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      }),
    )

    // 未物化 → FAILED_PRECONDITION → 400（与 Send 前置错误同族），错误不吞。
    fetchMock.mockImplementation(async () => new Response('agent not materialized', { status: 400 }))
    const err = await cancelAgent('templates/saolei/sessions/s1').then(
      () => null,
      (e: unknown) => e,
    )
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(400)
  })

  it('请求级失败映射为 ApiError（携带 HTTP status 与 body）', async () => {
    fetchMock.mockImplementation(async () =>
      new Response('preset already exists', { status: 409 }),
    )
    const err = await createPreset('saolei', 'p1', 'x').then(
      () => null,
      (e: unknown) => e,
    )
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(409)
    expect((err as ApiError).body).toContain('already exists')

    // getAgent 未物化 → 404（agent-api.md §2.2，调用方据此进入引导态）。
    fetchMock.mockImplementation(async () => new Response('not found', { status: 404 }))
    const get404 = await getAgent('templates/saolei/sessions/s1').then(
      () => null,
      (e: unknown) => e,
    )
    expect(get404).toBeInstanceOf(ApiError)
    expect((get404 as ApiError).status).toBe(404)
  })
})
