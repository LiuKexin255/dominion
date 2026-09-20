// @vitest-environment jsdom
// WikiView 组件测试（契约 specs/069-wiki-template-web/contracts/web-wiki-view.md
// §3-4 与 §5 验收锚点：搜索/空关键词 List、创建成功入列、重名错误呈现、编辑
// 保存、二步删除、空态/错误态、查看全文、404 并发删除）。Mock 约定照
// style/javascript.md：vi.fn() test-double + 对被拦截调用做正向断言。
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { WikiView } from './WikiView.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

// 资源名（protojson name）与 REST URL 前缀：name 不带 /api/v1，请求路径则经
// /api/v1 前缀（projects/game/game.proto Wiki 资源 pattern）。
const RESOURCE_BASE = 'templates/saolei/wikis'
const LIST_BASE = `/api/v1/${RESOURCE_BASE}`

interface Entry {
  name: string
  title: string
  content: string
  updateTime: string
}

const ENTRY_W1: Entry = {
  name: `${RESOURCE_BASE}/w1`,
  title: '开局-左上角',
  content: '先点角落',
  updateTime: '2026-09-20T01:00:00Z',
}

const ENTRY_W2: Entry = {
  name: `${RESOURCE_BASE}/w2`,
  title: '终局-猜雷',
  content: '留一格',
  updateTime: '2026-09-20T02:00:00Z',
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function norm(value: string): string {
  return value.normalize('NFKC').toLowerCase()
}

interface WikiRoute {
  initial?: Entry[]
  // 模拟外部删除：置位后列表/单条 GET/PATCH 都不再命中该条目。
  externalDelete?: string
  createStatus?: number
  patchStatus?: number
  deleteStatus?: number
  listStatus?: number
  // 第一页返回的续页 token（分页用例）与第二页条目。
  listNextToken?: string
  page2?: Entry[]
}

// makeFetchMock routes the WikiView calls over an in-memory collection so post-
// CRUD refreshes observe the created/updated/deleted state.
function makeFetchMock(route: WikiRoute = {}) {
  // 逐条浅拷贝：PATCH/创建会就地改写条目，避免模块级 fixture 被跨用例污染。
  const entries: Entry[] = (route.initial ?? [ENTRY_W1]).map((entry) => ({ ...entry }))
  const visible = (): Entry[] =>
    route.externalDelete !== undefined
      ? entries.filter((entry) => entry.name !== route.externalDelete)
      : entries

  return vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    const method = init?.method ?? 'GET'

    if (url.includes(':search')) {
      const query = new URL(url, 'https://test.local').searchParams.get('query') ?? ''
      const hits = visible()
        .filter((entry) => norm(entry.title).includes(norm(query)))
        .map((entry) => ({ name: entry.name, title: entry.title }))
      return jsonResponse({ wikis: hits })
    }

    if (url.startsWith(`${LIST_BASE}?wiki_id=`) && method === 'POST') {
      if (route.createStatus !== undefined && route.createStatus !== 200) {
        return new Response('wiki already exists', { status: route.createStatus })
      }
      const id = decodeURIComponent((url.split('wiki_id=')[1] ?? '').split('&')[0] ?? '')
      const body = JSON.parse(String(init?.body)) as { title: string; content: string }
      const created: Entry = {
        name: `${RESOURCE_BASE}/${id}`,
        title: body.title,
        content: body.content,
        updateTime: '2026-09-20T03:00:00Z',
      }
      entries.push(created)
      return jsonResponse(created)
    }

    if (url.startsWith(`${LIST_BASE}/`) && url.includes('?update_mask=') && method === 'PATCH') {
      if (route.patchStatus !== undefined && route.patchStatus !== 200) {
        return new Response('wiki not found', { status: route.patchStatus })
      }
      const name = `${RESOURCE_BASE}${url.slice(LIST_BASE.length).split('?')[0]}`
      const target = visible().find((entry) => entry.name === name)
      if (target === undefined) return new Response('not found', { status: 404 })
      const body = JSON.parse(String(init?.body)) as { title?: string; content?: string }
      if (body.title !== undefined) target.title = body.title
      if (body.content !== undefined) target.content = body.content
      target.updateTime = '2026-09-20T04:00:00Z'
      return jsonResponse(target)
    }

    if (url.startsWith(`${LIST_BASE}/`)) {
      const full = `${RESOURCE_BASE}${url.slice(LIST_BASE.length).split('?')[0]}`
      if (method === 'GET') {
        const target = visible().find((entry) => entry.name === full)
        if (target === undefined) return new Response('not found', { status: 404 })
        return jsonResponse(target)
      }
      if (method === 'DELETE') {
        const idx = entries.findIndex((entry) => entry.name === full)
        if (route.deleteStatus !== undefined && route.deleteStatus !== 200) {
          if (idx >= 0) entries.splice(idx, 1)
          return new Response('not found', { status: route.deleteStatus })
        }
        if (idx >= 0) entries.splice(idx, 1)
        return jsonResponse({})
      }
    }

    if (url.startsWith(LIST_BASE) && method === 'GET') {
      if (route.listStatus !== undefined && route.listStatus !== 200) {
        return new Response('list failed', { status: route.listStatus })
      }
      const onPage2 = url.includes('page_token=p2')
      const wikis = onPage2 ? (route.page2 ?? []) : visible()
      return jsonResponse({ wikis, nextPageToken: onPage2 ? '' : (route.listNextToken ?? '') })
    }

    throw new Error(`unexpected fetch: ${url} ${method}`)
  })
}

describe('WikiView', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = makeFetchMock()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('空关键词：走 ListWikis 浏览列表，呈现 title 与 update_time', async () => {
    render(<WikiView template="saolei" />)

    expect((await screen.findByTestId('wiki-title')).textContent).toBe('开局-左上角')
    expect(screen.getByTestId('wiki-time').textContent).toBe(
      new Date(ENTRY_W1.updateTime).toLocaleString(),
    )
    // 空搜索 = List（带 page_size，无 token 与 query）。
    expect(fetchMock).toHaveBeenCalledWith(`${LIST_BASE}?page_size=20`, undefined)
  })

  it('非空关键词：走 SearchWikis，命中列表无 update_time；清空再提交回到 List', async () => {
    fetchMock = makeFetchMock({ initial: [ENTRY_W1, ENTRY_W2] })
    vi.stubGlobal('fetch', fetchMock)
    render(<WikiView template="saolei" />)
    await screen.findAllByTestId('wiki-title')

    fireEvent.change(screen.getByTestId('wiki-search-input'), { target: { value: '终局' } })
    fireEvent.click(screen.getByTestId('wiki-search-submit'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `${LIST_BASE}:search?query=%E7%BB%88%E5%B1%80&page_size=20`,
        undefined,
      )
    })
    expect((await screen.findByTestId('wiki-title')).textContent).toBe('终局-猜雷')
    // 搜索条目为轻量摘要（无 update_time）。
    expect(screen.queryByTestId('wiki-time')).toBeNull()

    // 清空关键词再提交：回到 ListWikis（浏览全部）。
    fireEvent.change(screen.getByTestId('wiki-search-input'), { target: { value: '' } })
    fireEvent.click(screen.getByTestId('wiki-search-submit'))
    await waitFor(() => {
      expect(screen.getAllByTestId('wiki-title')).toHaveLength(2)
    })
    expect(fetchMock).toHaveBeenLastCalledWith(`${LIST_BASE}?page_size=20`, undefined)
  })

  it('搜索结果条目点击：经 GetWiki 展示完整 content', async () => {
    fetchMock = makeFetchMock({ initial: [ENTRY_W1, ENTRY_W2] })
    vi.stubGlobal('fetch', fetchMock)
    render(<WikiView template="saolei" />)
    await screen.findAllByTestId('wiki-title')

    fireEvent.change(screen.getByTestId('wiki-search-input'), { target: { value: '终局' } })
    fireEvent.click(screen.getByTestId('wiki-search-submit'))
    fireEvent.click(await screen.findByTestId('wiki-title'))

    expect(await screen.findByTestId('wiki-detail-title')).toBeTruthy()
    expect(screen.getByTestId('wiki-detail-content').textContent).toBe('留一格')
    expect(fetchMock).toHaveBeenCalledWith(`${LIST_BASE}/w2`, undefined)

    // 返回列表恢复搜索视图。
    fireEvent.click(screen.getByTestId('wiki-detail-close'))
    expect(screen.getByTestId('wiki-search-input')).toBeTruthy()
  })

  it('创建成功：POST 携带 wiki_id 与 body，保存后条目入列', async () => {
    render(<WikiView template="saolei" />)
    fireEvent.click(await screen.findByTestId('create-wiki'))
    expect(screen.getByTestId('wiki-form')).toBeTruthy()
    // 独占表单：编辑期间列表不渲染。
    expect(screen.queryByTestId('wiki-item')).toBeNull()

    fireEvent.change(screen.getByTestId('wiki-title-input'), { target: { value: '新条目' } })
    fireEvent.change(screen.getByTestId('wiki-content-input'), {
      target: { value: '正文内容\n第二行' },
    })
    fireEvent.click(screen.getByTestId('wiki-save'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringMatching(new RegExp(`^${LIST_BASE}\\?wiki_id=[a-f0-9]{32}$`)),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ title: '新条目', content: '正文内容\n第二行' }),
        }),
      )
    })
    // 保存后表单关闭、列表刷新并呈现新条目。
    await waitFor(() => {
      expect(screen.queryByTestId('wiki-form')).toBeNull()
    })
    await waitFor(() => {
      expect(screen.getAllByTestId('wiki-title').map((el) => el.textContent)).toContain('新条目')
    })
  })

  it('创建重名（409）：错误呈现、停留编辑视图且内容不丢', async () => {
    fetchMock = makeFetchMock({ initial: [], createStatus: 409 })
    vi.stubGlobal('fetch', fetchMock)
    render(<WikiView template="saolei" />)

    fireEvent.click(await screen.findByTestId('wiki-empty-create'))
    fireEvent.change(screen.getByTestId('wiki-title-input'), { target: { value: '重复标题' } })
    fireEvent.change(screen.getByTestId('wiki-content-input'), { target: { value: '草稿正文' } })
    fireEvent.click(screen.getByTestId('wiki-save'))

    await waitFor(() => {
      expect(screen.getByTestId('wiki-error')).toBeTruthy()
    })
    // 正向断言 POST 已发出（mock 被 exercise），失败停留表单、草稿保留。
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining(`${LIST_BASE}?wiki_id=`),
      expect.objectContaining({ method: 'POST' }),
    )
    expect(screen.getByTestId('wiki-form')).toBeTruthy()
    expect((screen.getByTestId('wiki-title-input') as HTMLInputElement).value).toBe('重复标题')
    expect((screen.getByTestId('wiki-content-input') as HTMLTextAreaElement).value).toBe('草稿正文')

    // 取消返回列表并清除错误（错误属于编辑上下文）。
    fireEvent.click(screen.getByTestId('wiki-cancel'))
    expect(screen.queryByTestId('wiki-form')).toBeNull()
    expect(screen.queryByTestId('wiki-error')).toBeNull()
  })

  it('编辑保存：预填全文，仅改动 content 时 PATCH update_mask=content', async () => {
    render(<WikiView template="saolei" />)
    fireEvent.click(await screen.findByTestId('wiki-edit'))

    // 编辑入口经 GetWiki 取完整资源后进入独占表单。
    await waitFor(() => {
      expect((screen.getByTestId('wiki-title-input') as HTMLInputElement).value).toBe('开局-左上角')
    })
    expect((screen.getByTestId('wiki-content-input') as HTMLTextAreaElement).value).toBe(
      '先点角落',
    )
    expect(screen.queryByTestId('wiki-item')).toBeNull()

    fireEvent.change(screen.getByTestId('wiki-content-input'), { target: { value: '改后正文' } })
    fireEvent.click(screen.getByTestId('wiki-save'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `${LIST_BASE}/w1?update_mask=content`,
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({ content: '改后正文' }),
        }),
      )
    })
    await waitFor(() => {
      expect(screen.queryByTestId('wiki-form')).toBeNull()
    })
    // 列表已刷新；查看详情反映新正文。
    fireEvent.click(await screen.findByTestId('wiki-title'))
    await waitFor(() => {
      expect(screen.getByTestId('wiki-detail-content').textContent).toBe('改后正文')
    })
  })

  it('编辑标题：PATCH update_mask=title（mask 只含实际改动字段）', async () => {
    render(<WikiView template="saolei" />)
    fireEvent.click(await screen.findByTestId('wiki-edit'))
    await waitFor(() => {
      expect((screen.getByTestId('wiki-title-input') as HTMLInputElement).value).toBe('开局-左上角')
    })

    fireEvent.change(screen.getByTestId('wiki-title-input'), { target: { value: '新标题' } })
    fireEvent.click(screen.getByTestId('wiki-save'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `${LIST_BASE}/w1?update_mask=title`,
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({ title: '新标题' }),
        }),
      )
    })
  })

  it('正在编辑的条目被外部删除：提交 404 呈现并返回列表（不吞错误）', async () => {
    const route: WikiRoute = {}
    fetchMock = makeFetchMock(route)
    vi.stubGlobal('fetch', fetchMock)
    render(<WikiView template="saolei" />)
    fireEvent.click(await screen.findByTestId('wiki-edit'))
    await waitFor(() => {
      expect(screen.getByTestId('wiki-form')).toBeTruthy()
    })

    // 条目在组件之外被删除：PATCH 返回 404。
    route.externalDelete = ENTRY_W1.name
    fireEvent.change(screen.getByTestId('wiki-content-input'), { target: { value: '改不动了' } })
    fireEvent.click(screen.getByTestId('wiki-save'))

    await waitFor(() => {
      expect(screen.queryByTestId('wiki-form')).toBeNull()
    })
    expect(screen.getByTestId('wiki-error').textContent).toContain('404')
    // 列表已无该条目（服务端权威结果）；错误呈现时不渲染"还没有条目"空态，
    // 避免把加载/写入失败误报为空库。
    expect(screen.queryByTestId('wiki-item')).toBeNull()
  })

  it('删除：inline 二步确认，取消不发请求，确认后 DELETE 并移除条目', async () => {
    render(<WikiView template="saolei" />)
    fireEvent.click(await screen.findByTestId('wiki-delete'))

    // 待确认态：尚未发起 DELETE。
    expect(screen.getByTestId('wiki-delete-confirm')).toBeTruthy()
    expect(
      fetchMock.mock.calls.filter((call) => (call[1] as RequestInit | undefined)?.method === 'DELETE'),
    ).toHaveLength(0)

    fireEvent.click(screen.getByTestId('wiki-delete-cancel'))
    expect(screen.queryByTestId('wiki-delete-confirm')).toBeNull()

    fireEvent.click(screen.getByTestId('wiki-delete'))
    fireEvent.click(screen.getByTestId('wiki-delete-confirm'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `${LIST_BASE}/w1`,
        expect.objectContaining({ method: 'DELETE' }),
      )
    })
    await waitFor(() => {
      expect(screen.queryByTestId('wiki-item')).toBeNull()
    })
    expect(screen.getByTestId('wiki-empty')).toBeTruthy()
  })

  it('删除并发 404：错误呈现且条目从列表消除（刷新以服务端为准）', async () => {
    fetchMock = makeFetchMock({ initial: [ENTRY_W1], deleteStatus: 404 })
    vi.stubGlobal('fetch', fetchMock)
    render(<WikiView template="saolei" />)

    fireEvent.click(await screen.findByTestId('wiki-delete'))
    fireEvent.click(screen.getByTestId('wiki-delete-confirm'))
    await waitFor(() => {
      expect(screen.getByTestId('wiki-error').textContent).toContain('404')
    })
    expect(screen.queryByTestId('wiki-item')).toBeNull()
  })

  it('空态：无条目呈现引导与新建入口', async () => {
    fetchMock = makeFetchMock({ initial: [] })
    vi.stubGlobal('fetch', fetchMock)
    render(<WikiView template="saolei" />)

    const empty = await screen.findByTestId('wiki-empty')
    expect(empty.textContent).toContain('还没有 wiki 条目')
    fireEvent.click(screen.getByTestId('wiki-empty-create'))
    expect(screen.getByTestId('wiki-form')).toBeTruthy()
    expect(screen.queryByTestId('wiki-empty')).toBeNull()
  })

  it('错误态：列表请求失败经 ApiError.message 呈现，不静默', async () => {
    fetchMock = makeFetchMock({ listStatus: 500 })
    vi.stubGlobal('fetch', fetchMock)
    render(<WikiView template="saolei" />)

    await waitFor(() => {
      expect(screen.getByTestId('wiki-error').textContent).toContain('500')
    })
    expect(fetchMock).toHaveBeenCalledWith(`${LIST_BASE}?page_size=20`, undefined)
  })

  it('分页：下一页携带 page_token，上一页回到首页', async () => {
    fetchMock = makeFetchMock({ initial: [ENTRY_W1], listNextToken: 'p2', page2: [ENTRY_W2] })
    vi.stubGlobal('fetch', fetchMock)
    render(<WikiView template="saolei" />)

    expect((await screen.findByTestId('wiki-title')).textContent).toBe('开局-左上角')
    expect((screen.getByTestId('wiki-prev') as HTMLButtonElement).disabled).toBe(true)

    fireEvent.click(screen.getByTestId('wiki-next'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(`${LIST_BASE}?page_size=20&page_token=p2`, undefined)
    })
    expect((await screen.findByTestId('wiki-title')).textContent).toBe('终局-猜雷')
    expect(screen.getByTestId('wiki-page').textContent).toContain('第 2 页')

    fireEvent.click(screen.getByTestId('wiki-prev'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenLastCalledWith(`${LIST_BASE}?page_size=20`, undefined)
    })
    expect((await screen.findByTestId('wiki-title')).textContent).toBe('开局-左上角')
  })
})
