// api/agent.js 的 fetch mock 单测：断言每个方法的 URL/method/body 请求形状
// 与响应投影（specs/059-agent-v2-team-mode/contracts/team-api.md 与
// contracts/web-views.md §1 的客户端面；role/member/sender 均为场景词汇字符串
// ——2026-09-10 用户裁定）。Mock 约定照 style/javascript.md：vi.fn()
// test-double + 对被拦截调用做正向断言。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  cancelTeam,
  createPreset,
  deletePreset,
  getPreset,
  getTeam,
  listMemberMessages,
  listModels,
  listPresets,
  listTeamMessages,
  updatePreset,
  updateTeam,
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
            persona: '你是扫雷玩家',
            role: 'player',
            createTime: '2026-08-29T00:00:00Z',
            updateTime: '2026-08-29T01:00:00Z',
          },
        ],
      }),
    )
    const presets = await listPresets('saolei')
    expect(presets).toHaveLength(1)
    expect(presets[0].name).toBe('templates/saolei/presets/p1')
    expect(presets[0].persona).toBe('你是扫雷玩家')
    expect(presets[0].role).toBe('player')
  })

  it('listPresets 携带 role 过滤参数（分池下拉，preset-api.md §1）；空 = 不过滤', async () => {
    fetchMock.mockImplementation(async () => jsonResponse({ presets: [] }))
    await listPresets('saolei', 'player')
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v2/templates/saolei/presets?role=player',
      undefined,
    )

    await listPresets('saolei', 'planner')
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v2/templates/saolei/presets?role=planner',
      undefined,
    )

    // 空字符串 = 不过滤（preset-api.md §1）。
    await listPresets('saolei', '')
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v2/templates/saolei/presets',
      undefined,
    )
  })

  it('createPreset POST，caller-supplied id 与 role 走 query，body 为 Preset 本体', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: 'templates/saolei/presets/p1', persona: 'p' }),
    )
    const created = await createPreset('saolei', 'p1', '你是扫雷玩家', 'player')
    expect(created.name).toBe('templates/saolei/presets/p1')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/presets?preset_id=p1&role=player',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ persona: '你是扫雷玩家' }),
      }),
    )

    // role 省略（T022 表单就位前的兼容调用）：query 仅 preset_id。
    await createPreset('saolei', 'p1', 'p')
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v2/templates/saolei/presets?preset_id=p1',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ persona: 'p' }) }),
    )
  })

  it('getPreset GET 完整资源名', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: 'templates/saolei/presets/p1', persona: 'x' }),
    )
    await getPreset('templates/saolei/presets/p1')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/presets/p1',
      undefined,
    )
  })

  it('updatePreset PATCH 携带 update_mask=persona 与新内容', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: 'templates/saolei/presets/p1', persona: '新的' }),
    )
    await updatePreset('templates/saolei/presets/p1', '新的')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/presets/p1?update_mask=persona',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ persona: '新的' }),
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

  it('getTeam GET session 的 team 单例（成员清单与连接状态投影）', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        name: 'templates/saolei/sessions/s1/team',
        members: [
          {
            name: 'templates/saolei/sessions/s1/team/members/player',
            role: 'player',
            preset: 'templates/saolei/presets/p1',
            model: 'glm-5.2',
          },
          {
            name: 'templates/saolei/sessions/s1/team/members/planner',
            role: 'planner',
            preset: 'templates/saolei/presets/p2',
          },
        ],
        desktopConnected: true,
      }),
    )
    const team = await getTeam('templates/saolei/sessions/s1')
    expect(team.name).toBe('templates/saolei/sessions/s1/team')
    expect(team.members).toHaveLength(2)
    expect(team.members?.[0]?.role).toBe('player')
    expect(team.members?.[1]?.preset).toBe('templates/saolei/presets/p2')
    expect(team.desktopConnected).toBe(true)
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/sessions/s1/team',
      undefined,
    )
  })

  it('updateTeam PATCH allow_missing=true，body 为 Team.members 输入列表（model 空省略）', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        name: 'templates/saolei/sessions/s1/team',
        members: [
          { role: 'player', preset: 'templates/saolei/presets/p1' },
          { role: 'planner', preset: 'templates/saolei/presets/p2' },
        ],
      }),
    )
    await updateTeam('templates/saolei/sessions/s1', [
      { role: 'player', preset: 'templates/saolei/presets/p1' },
      { role: 'planner', preset: 'templates/saolei/presets/p2' },
    ])
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/sessions/s1/team?allow_missing=true',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({
          members: [
            { role: 'player', preset: 'templates/saolei/presets/p1' },
            { role: 'planner', preset: 'templates/saolei/presets/p2' },
          ],
        }),
      }),
    )

    // output-only 字段（name/systemPrompt）与空 model 不进 body。
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: 'templates/saolei/sessions/s1/team', members: [] }),
    )
    await updateTeam('templates/saolei/sessions/s1', [
      {
        name: 'templates/saolei/sessions/s1/team/members/player',
        role: 'player',
        preset: 'templates/saolei/presets/p1',
        model: 'glm-5.2',
        systemPrompt: 'server-filled',
      },
      { role: 'planner', preset: 'templates/saolei/presets/p2', model: '' },
    ])
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v2/templates/saolei/sessions/s1/team?allow_missing=true',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({
          members: [
            { role: 'player', preset: 'templates/saolei/presets/p1', model: 'glm-5.2' },
            { role: 'planner', preset: 'templates/saolei/presets/p2' },
          ],
        }),
      }),
    )
  })

  it('cancelTeam POST {session}/team:cancel，body 空对象；未物化失败抛 ApiError(400)', async () => {
    // AIP-136 自定义方法（team-api.md §4）：请求仅 name 路径参数，body:"*"
    // 下 body 为空对象；幂等 no-op 同样 200。
    fetchMock.mockImplementation(async () => jsonResponse({}))
    await expect(cancelTeam('templates/saolei/sessions/s1')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/sessions/s1/team:cancel',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      }),
    )

    // 未物化 → FAILED_PRECONDITION → 400（与 Send 前置错误同族），错误不吞。
    fetchMock.mockImplementation(async () => new Response('team not materialized', { status: 400 }))
    const err = await cancelTeam('templates/saolei/sessions/s1').then(
      () => null,
      (e: unknown) => e,
    )
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(400)
  })

  it('listTeamMessages GET team/messages 并返回归并序列（member 字符串）', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        messages: [
          {
            member: 'user',
            message: { role: 'ROLE_USER', blocks: [{ text: { content: '开局' } }] },
            seq: '1',
          },
          {
            member: 'player',
            message: { role: 'ROLE_AGENT', blocks: [{ text: { content: '收到' } }] },
            seq: '2',
          },
        ],
      }),
    )
    const messages = await listTeamMessages('templates/saolei/sessions/s1')
    expect(messages).toHaveLength(2)
    expect(messages[0]?.member).toBe('user')
    expect(messages[0]?.message.role).toBe('ROLE_USER')
    expect(messages[1]?.member).toBe('player')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/sessions/s1/team/messages',
      undefined,
    )
  })

  it('listMemberMessages GET 成员视角历史（sender 字符串标注）', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        messages: [
          {
            message: { role: 'ROLE_USER', blocks: [{ text: { content: '开局' } }] },
            sender: 'user',
          },
          {
            message: { role: 'ROLE_USER', blocks: [{ text: { content: '[planner] 策略' } }] },
            sender: 'planner',
          },
        ],
      }),
    )
    const messages = await listMemberMessages('templates/saolei/sessions/s1', 'player')
    expect(messages).toHaveLength(2)
    expect(messages[1]?.sender).toBe('planner')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v2/templates/saolei/sessions/s1/team/members/player/messages',
      undefined,
    )
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

    // getTeam 未物化 → 404（team-api.md §1/§2，调用方据此进入引导态）。
    fetchMock.mockImplementation(async () => new Response('not found', { status: 404 }))
    const get404 = await getTeam('templates/saolei/sessions/s1').then(
      () => null,
      (e: unknown) => e,
    )
    expect(get404).toBeInstanceOf(ApiError)
    expect((get404 as ApiError).status).toBe(404)
  })
})
