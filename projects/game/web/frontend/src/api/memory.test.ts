// api/memory.js 的 fetch mock 单测：断言每个方法的 URL/method/body/query 请求
// 形状与响应投影（契约 specs/071-memory-preset-scope/contracts/web-memory-view.md §2、
// specs/071-memory-preset-scope/contracts/memory-service.md §2、data-model.md §4）。
// Mock 约定照 style/javascript.md：vi.fn() test-double + 对被拦截调用做正向
// 断言。
import { createHash } from 'node:crypto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createMemory,
  deleteMemory,
  listMemories,
  memoryIdForContent,
  updateMemory,
} from './memory.js'
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

function emptyResponse(status = 200): Response {
  return new Response('{}', { status })
}

const LIST_BASE = '/api/v1/templates/saolei/presets/p1/memories'
// 资源名（protojson name）不带 /api/v1 前缀；请求路径经 /api/v1 前缀
// （projects/game/game.proto Memory 资源 pattern + gateway 挂载）。
const MEMORY_M1 = 'templates/saolei/presets/p1/memories/m1'
const MEMORY_M1_URL = `/api/v1/${MEMORY_M1}`

describe('memory api 客户端', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      throw new Error(`unexpected fetch: ${url} ${init?.method ?? 'GET'}`)
    })
    vi.stubGlobal('fetch', fetchMock)
  })

  it('listMemories GET preset memory 集合（protojson 投影，空集合 → [] + ""）', async () => {
    fetchMock.mockImplementation(async () => jsonResponse({}))
    await expect(listMemories('saolei', 'p1')).resolves.toEqual({
      memories: [],
      nextPageToken: '',
    })
    expect(fetchMock).toHaveBeenCalledWith(LIST_BASE, undefined)

    fetchMock.mockImplementation(async () =>
      jsonResponse({
        memories: [
          {
            name: MEMORY_M1,
            content: '先确认补给清单',
            createTime: '2026-09-20T00:00:00Z',
            updateTime: '2026-09-20T01:00:00Z',
          },
        ],
        nextPageToken: 'next',
      }),
    )
    const page = await listMemories('saolei', 'p1')
    expect(page.memories).toHaveLength(1)
    expect(page.memories[0]?.name).toBe(MEMORY_M1)
    expect(page.memories[0]?.content).toBe('先确认补给清单')
    expect(page.memories[0]?.updateTime).toBe('2026-09-20T01:00:00Z')
    expect(page.nextPageToken).toBe('next')
  })

  it('listMemories 分页与 order_by 参数（缺省与空 token 不发送）', async () => {
    fetchMock.mockImplementation(async () => jsonResponse({ memories: [] }))

    await listMemories('saolei', 'p1', { pageSize: 20, orderBy: 'update_time desc' })
    expect(fetchMock).toHaveBeenLastCalledWith(
      `${LIST_BASE}?page_size=20&order_by=update_time+desc`,
      undefined,
    )

    await listMemories('saolei', 'p1', { pageSize: 50, pageToken: 'tok-1' })
    expect(fetchMock).toHaveBeenLastCalledWith(
      `${LIST_BASE}?page_size=50&page_token=tok-1`,
      undefined,
    )

    // 空 token = 首页：不发送 page_token（ListMemoriesOptions.pageToken 契约）；
    // 空 orderBy 同样不发送（服务端缺省 memory_id 升序）。
    await listMemories('saolei', 'p1', { pageToken: '', orderBy: '' })
    expect(fetchMock).toHaveBeenLastCalledWith(LIST_BASE, undefined)
  })

  it('createMemory POST：memory_id 走 query 且为内容摘要，body 为 memory 本体', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: MEMORY_M1, content: '先确认补给清单' }),
    )

    const created = await createMemory('saolei', 'p1', '先确认补给清单')
    expect(created.name).toBe(MEMORY_M1)
    expect(created.content).toBe('先确认补给清单')

    // 请求形状：memory_id = sha256(content)[:32]（与工具路径同值域）。
    const wantId = createHash('sha256').update('先确认补给清单').digest('hex').slice(0, 32)
    expect(wantId).toMatch(/^[a-f0-9]{32}$/)
    expect(fetchMock).toHaveBeenCalledWith(
      `${LIST_BASE}?memory_id=${wantId}`,
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        // projects/game/game.proto CreateMemoryRequest binds body:"memory"，
        // 故 HTTP body 即 Memory 消息本体（扁平 {content}）。
        body: JSON.stringify({ content: '先确认补给清单' }),
      }),
    )
  })

  it('updateMemory PATCH：update_mask=content 走 query，body 携带 name/content', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: MEMORY_M1, content: '改后内容' }),
    )

    const updated = await updateMemory(MEMORY_M1, '改后内容')
    expect(updated.content).toBe('改后内容')
    expect(fetchMock).toHaveBeenCalledWith(
      `${MEMORY_M1_URL}?update_mask=content`,
      expect.objectContaining({
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: MEMORY_M1, content: '改后内容' }),
      }),
    )
  })

  it('deleteMemory DELETE 完整资源名，失败抛 ApiError', async () => {
    fetchMock.mockImplementation(async () => emptyResponse())
    await expect(deleteMemory(MEMORY_M1)).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith(
      MEMORY_M1_URL,
      expect.objectContaining({ method: 'DELETE' }),
    )

    fetchMock.mockImplementation(async () => emptyResponse(404))
    await expect(deleteMemory(MEMORY_M1)).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
    })
  })

  it('请求级失败映射为 ApiError（409 ALREADY_EXISTS / 400 INVALID_ARGUMENT）', async () => {
    fetchMock.mockImplementation(async () => new Response('memory already exists', { status: 409 }))
    const conflict = await createMemory('saolei', 'p1', '同内容').then(
      () => null,
      (e: unknown) => e,
    )
    expect(conflict).toBeInstanceOf(ApiError)
    expect((conflict as ApiError).status).toBe(409)
    expect((conflict as ApiError).body).toContain('already exists')

    fetchMock.mockImplementation(async () => new Response('content too long', { status: 400 }))
    const invalid = await updateMemory(MEMORY_M1, '超限内容').then(
      () => null,
      (e: unknown) => e,
    )
    expect(invalid).toBeInstanceOf(ApiError)
    expect((invalid as ApiError).status).toBe(400)
  })
})

// memoryIdForContent 是工具路径 generateMemoryId 的 web 孪生
// （common/js/memory/src/operations.ts），须同算法同值域
// （specs/071-memory-preset-scope/data-model.md §1.1）。断言既用已知向量
// （sha256 标准测试向量），也逐输入对照 node createHash 的 node 路径结果——
// 两侧为独立实现，避免以同实现对照同实现。
describe('memory id 派生（generateMemoryId 孪生）', () => {
  it('memoryIdForContent：sha256(content) 前 32 位十六进制，符合 [a-z0-9_-]+', async () => {
    await expect(memoryIdForContent('')).resolves.toBe('e3b0c44298fc1c149afbf4c8996fb924')
    await expect(memoryIdForContent('abc')).resolves.toBe('ba7816bf8f01cfea414140de5dae2223')
    await expect(memoryIdForContent('扫雷策略')).resolves.toBe('a23788cd59c3638d93b3c8e41bfbf283')
    expect(await memoryIdForContent('先确认补给清单')).toMatch(/^[a-f0-9]{32}$/)
  })

  it('与 node createHash 路径逐输入同值（含空串与多字节内容）', async () => {
    const samples = ['', 'abc', '先确认补给清单', 'multi\nline\ncontent', 'a'.repeat(1024)]
    for (const content of samples) {
      const nodeId = createHash('sha256').update(content).digest('hex').slice(0, 32)
      await expect(memoryIdForContent(content)).resolves.toBe(nodeId)
    }
  })
})
