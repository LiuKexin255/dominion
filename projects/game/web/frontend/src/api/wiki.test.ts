// api/wiki.js 的 fetch mock 单测：断言每个方法的 URL/method/body/query 请求
// 形状与响应投影（契约 specs/069-wiki-template-web/contracts/web-wiki-view.md
// §2、contracts/wiki-service.md §2、data-model.md §4）。Mock 约定照
// style/javascript.md：vi.fn() test-double + 对被拦截调用做正向断言。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createWiki,
  deleteWiki,
  getWiki,
  listWikis,
  normalizeWikiTitle,
  searchWikis,
  updateWiki,
  wikiIdForTitle,
} from './wiki.js'
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

const WIKI_W1 = 'templates/saolei/wikis/w1'

describe('wiki api 客户端', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      throw new Error(`unexpected fetch: ${url} ${init?.method ?? 'GET'}`)
    })
    vi.stubGlobal('fetch', fetchMock)
  })

  it('listWikis GET 模板 wiki 集合（protojson 投影，空集合 → [] + ""）', async () => {
    fetchMock.mockImplementation(async () => jsonResponse({}))
    await expect(listWikis('saolei')).resolves.toEqual({ wikis: [], nextPageToken: '' })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/templates/saolei/wikis', undefined)

    fetchMock.mockImplementation(async () =>
      jsonResponse({
        wikis: [
          {
            name: WIKI_W1,
            title: '开局-左上角',
            content: '先点角落',
            createTime: '2026-09-20T00:00:00Z',
            updateTime: '2026-09-20T01:00:00Z',
          },
        ],
        nextPageToken: 'next',
      }),
    )
    const page = await listWikis('saolei')
    expect(page.wikis).toHaveLength(1)
    expect(page.wikis[0]?.name).toBe(WIKI_W1)
    expect(page.wikis[0]?.content).toBe('先点角落')
    expect(page.wikis[0]?.updateTime).toBe('2026-09-20T01:00:00Z')
    expect(page.nextPageToken).toBe('next')
  })

  it('listWikis 分页参数 page_size/page_token（缺省与空 token 不发送）', async () => {
    fetchMock.mockImplementation(async () => jsonResponse({ wikis: [] }))
    await listWikis('saolei', { pageSize: 20 })
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v1/templates/saolei/wikis?page_size=20',
      undefined,
    )

    await listWikis('saolei', { pageSize: 50, pageToken: 'tok-1' })
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v1/templates/saolei/wikis?page_size=50&page_token=tok-1',
      undefined,
    )

    // 空 token = 首页：不发送 page_token（ListWikisOptions.pageToken 契约）。
    await listWikis('saolei', { pageToken: '' })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/templates/saolei/wikis', undefined)
  })

  it('searchWikis GET :search，query 与分页参数，返回轻量条目（无 content）', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        wikis: [{ name: WIKI_W1, title: '开局-左上角' }],
        nextPageToken: 'more',
      }),
    )
    const page = await searchWikis('saolei', '开局', { pageSize: 20 })
    expect(page.wikis).toEqual([{ name: WIKI_W1, title: '开局-左上角' }])
    expect(page.nextPageToken).toBe('more')
    // 轻量条目无 content 字段（contracts/wiki-service.md §2/§4）。
    expect('content' in page.wikis[0]!).toBe(false)
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/templates/saolei/wikis:search?query=%E5%BC%80%E5%B1%80&page_size=20',
      undefined,
    )

    await searchWikis('saolei', 'alpha', { pageToken: 'tok-2' })
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v1/templates/saolei/wikis:search?query=alpha&page_token=tok-2',
      undefined,
    )
  })

  it('createWiki POST：wiki_id 走 query，body 为 wiki 本体，返回完整资源', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: WIKI_W1, title: 'Alpha', content: '正文' }),
    )
    const created = await createWiki('saolei', '8ed3f6ad685b959ead7022518e1af76c', 'Alpha', '正文')
    expect(created.name).toBe(WIKI_W1)
    expect(created.content).toBe('正文')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/templates/saolei/wikis?wiki_id=8ed3f6ad685b959ead7022518e1af76c',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: 'Alpha', content: '正文' }),
      }),
    )
  })

  it('getWiki GET 完整资源名', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: WIKI_W1, title: 'Alpha', content: '正文' }),
    )
    const wiki = await getWiki(WIKI_W1)
    expect(wiki.title).toBe('Alpha')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/templates/saolei/wikis/w1', undefined)
  })

  it('updateWiki PATCH：update_mask 按实际修改字段构造（title/content 子集）', async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ name: WIKI_W1, title: 'Alpha', content: '正文' }),
    )

    await updateWiki(WIKI_W1, { title: 'Alpha', content: '正文' })
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v1/templates/saolei/wikis/w1?update_mask=title,content',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ title: 'Alpha', content: '正文' }),
      }),
    )

    await updateWiki(WIKI_W1, { content: '仅正文' })
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v1/templates/saolei/wikis/w1?update_mask=content',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ content: '仅正文' }),
      }),
    )

    await updateWiki(WIKI_W1, { title: '仅标题' })
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v1/templates/saolei/wikis/w1?update_mask=title',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ title: '仅标题' }),
      }),
    )
  })

  it('deleteWiki DELETE 完整资源名，失败抛 ApiError', async () => {
    fetchMock.mockImplementation(async () => emptyResponse())
    await expect(deleteWiki(WIKI_W1)).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/templates/saolei/wikis/w1',
      expect.objectContaining({ method: 'DELETE' }),
    )

    fetchMock.mockImplementation(async () => emptyResponse(404))
    await expect(deleteWiki(WIKI_W1)).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
    })
  })

  it('请求级失败映射为 ApiError（409 ALREADY_EXISTS / 404 NOT_FOUND）', async () => {
    fetchMock.mockImplementation(async () => new Response('wiki already exists', { status: 409 }))
    const conflict = await createWiki('saolei', 'id', 'Alpha', '正文').then(
      () => null,
      (e: unknown) => e,
    )
    expect(conflict).toBeInstanceOf(ApiError)
    expect((conflict as ApiError).status).toBe(409)
    expect((conflict as ApiError).body).toContain('already exists')

    fetchMock.mockImplementation(async () => new Response('not found', { status: 404 }))
    const missing = await getWiki(WIKI_W1).then(
      () => null,
      (e: unknown) => e,
    )
    expect(missing).toBeInstanceOf(ApiError)
    expect((missing as ApiError).status).toBe(404)
  })
})

// normalizeWikiTitle / wikiIdForTitle 是服务端 NormalizeTitle 与 id 派生规则的
// JS 孪生，须与 common/js/dsh-plugins/memory-service/src/wiki-operations.ts 同
// 结果（specs/069-wiki-template-web/data-model.md §4）。断言用硬编码的 sha256
// 前缀（'abc' 的 SHA-256 为已知向量），避免以同实现对照同实现。
describe('wiki id 派生（NormalizeTitle 孪生）', () => {
  it('normalizeWikiTitle：NFKC + 小写（不做 trim）', () => {
    expect(normalizeWikiTitle('ABC')).toBe('abc')
    expect(normalizeWikiTitle('ＡＢＣ')).toBe('abc')
    expect(normalizeWikiTitle(' Ａlpha ')).toBe(' alpha ')
    expect(normalizeWikiTitle('扫雷策略')).toBe('扫雷策略')
  })

  it('wikiIdForTitle：sha256(title_norm) 前 32 位十六进制，全角/大小写同像同 id', async () => {
    const fullwidth = await wikiIdForTitle('ＡＢＣ')
    expect(fullwidth).toBe('ba7816bf8f01cfea414140de5dae2223')
    expect(fullwidth).toMatch(/^[a-f0-9]{32}$/)
    // 'ABC' / 'abc' / 全角 'ＡＢＣ' 规范化同像 → 同 id。
    await expect(wikiIdForTitle('ABC')).resolves.toBe(fullwidth)
    await expect(wikiIdForTitle('abc')).resolves.toBe(fullwidth)
    await expect(wikiIdForTitle('Alpha')).resolves.toBe('8ed3f6ad685b959ead7022518e1af76c')
    await expect(wikiIdForTitle('扫雷策略')).resolves.toBe('a23788cd59c3638d93b3c8e41bfbf283')
  })
})
