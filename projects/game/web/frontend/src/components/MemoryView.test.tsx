// @vitest-environment jsdom
// MemoryView 组件测试（契约 specs/071-memory-preset-scope/contracts/
// web-memory-view.md §3-4 与 §5 验收锚点 1/2：preset 选择、列表默认序断言
// （update_time desc）、创建（摘要 id 派生、409 提示）、编辑（mask 传递）、
// 二步删除、空态/错误态、切换 preset 重置、场景词扫描）。Mock 约定照
// style/javascript.md：vi.fn() test-double + 对被拦截调用做正向断言。
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryView } from './MemoryView.js'
import { findSceneWordsIn } from '../scene-words.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

// 资源名（protojson name）与 REST URL 前缀：name 不带 /api/v1，请求路径则经
// /api/v1 前缀（projects/game/game.proto Memory 资源 pattern）。
const RESOURCE_BASE = 'templates/saolei/presets'
const P1 = 'p1'
const P2 = 'p2'
const P1_NAME = `${RESOURCE_BASE}/${P1}`
const P2_NAME = `${RESOURCE_BASE}/${P2}`
const LIST_BASE = `/api/v1/${RESOURCE_BASE}`

interface Entry {
  name: string
  content: string
  updateTime: string
}

function entry(preset: string, id: string, content: string, updateTime: string): Entry {
  return { name: `${RESOURCE_BASE}/${preset}/memories/${id}`, content, updateTime }
}

const E1 = entry(P1, 'm1', '先确认补给清单', '2026-09-20T02:00:00Z')
const E2 = entry(P1, 'm2', '遇到分歧先记录再推进', '2026-09-20T01:00:00Z')
const E3 = entry(P2, 'm1', '换一局先对齐目标', '2026-09-21T02:00:00Z')

// preset 列表 fixture：全角色（player/planner 均在池中，选择器不过滤——
// 契约 §1）。role 为资源数据、不作为文案渲染（FR-016）。
const PRESETS = [
  { name: P1_NAME, role: 'player' },
  { name: P2_NAME, role: 'planner' },
]

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

// flush drains two macrotask turns, so the promise continuations of a released
// (late) response have fully landed before the assertion.
async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await new Promise((resolve) => setTimeout(resolve, 0))
}

interface MemoryRoute {
  // 每 preset 的初始集合（缺省 p1 两条，按 update_time 降序）。
  collections?: Record<string, Entry[]>
  presets?: { name: string; role: string }[]
  presetsStatus?: number
  listStatus?: number
  // createStatus：非 200 时注入 grpc-gateway 错误信封（409 同内容重复 /
  // 400 参数错误由用例选择）。
  createStatus?: number
  patchStatus?: number
  deleteStatus?: number
  // 第一页返回的续页 token 与第二页条目（分页用例）。
  listNextToken?: string
  page2?: Record<string, Entry[]>
  // 自定义列表响应（竞态用例：按 preset 挂起/放行响应）；未提供时按内存集合
  // 返回第一页。
  listResponse?: (preset: string) => Promise<Response> | Response
}

// makeFetchMock routes the MemoryView calls over in-memory collections so post-
// CRUD refreshes observe the created/updated/deleted state.
function makeFetchMock(route: MemoryRoute = {}) {
  const collections: Record<string, Entry[]> = {}
  for (const [preset, list] of Object.entries(route.collections ?? { [P1]: [E1, E2] })) {
    collections[preset] = list.map((e) => ({ ...e }))
  }
  const presets = route.presets ?? PRESETS

  return vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    const method = init?.method ?? 'GET'

    if (url === '/api/v2/templates/saolei/presets' && method === 'GET') {
      if (route.presetsStatus !== undefined && route.presetsStatus !== 200) {
        return new Response('presets failed', { status: route.presetsStatus })
      }
      return jsonResponse({ presets })
    }

    const parsed = new URL(url, 'https://test.local')
    const item = /^\/api\/v1\/templates\/saolei\/presets\/([^/]+)\/memories\/([^/]+)$/.exec(
      parsed.pathname,
    )
    if (item !== null) {
      const [, preset = '', id = ''] = item
      const full = `${RESOURCE_BASE}/${preset}/memories/${id}`
      const list = collections[preset] ?? []
      if (method === 'PATCH' && parsed.searchParams.get('update_mask') === 'content') {
        if (route.patchStatus !== undefined && route.patchStatus !== 200) {
          return new Response('{"code":5,"message":"memory not found"}', {
            status: route.patchStatus,
          })
        }
        const target = list.find((e) => e.name === full)
        if (target === undefined) return new Response('not found', { status: 404 })
        const body = JSON.parse(String(init?.body)) as { content: string }
        target.content = body.content
        target.updateTime = '2026-09-20T04:00:00Z'
        return jsonResponse(target)
      }
      if (method === 'DELETE') {
        if (route.deleteStatus !== undefined && route.deleteStatus !== 200) {
          return new Response('{"code":5,"message":"memory not found"}', {
            status: route.deleteStatus,
          })
        }
        const idx = list.findIndex((e) => e.name === full)
        if (idx >= 0) list.splice(idx, 1)
        return jsonResponse({})
      }
    }

    const listMatch = /^\/api\/v1\/templates\/saolei\/presets\/([^/]+)\/memories$/.exec(
      parsed.pathname,
    )
    if (listMatch !== null) {
      const preset = listMatch[1] ?? ''
      if (method === 'POST') {
        if (route.createStatus !== undefined && route.createStatus !== 200) {
          const envelope =
            route.createStatus === 409
              ? '{"code":6,"message":"memory already exists"}'
              : '{"code":3,"message":"memory content exceeds 1024 characters (got 1025)"}'
          return new Response(envelope, { status: route.createStatus })
        }
        const id = parsed.searchParams.get('memory_id') ?? ''
        const body = JSON.parse(String(init?.body)) as { content: string }
        const created: Entry = {
          name: `${RESOURCE_BASE}/${preset}/memories/${id}`,
          content: body.content,
          updateTime: '2026-09-20T03:00:00Z',
        }
        collections[preset] = [...(collections[preset] ?? []), created]
        return jsonResponse(created)
      }
      if (method === 'GET') {
        if (route.listStatus !== undefined && route.listStatus !== 200) {
          return new Response('list failed', { status: route.listStatus })
        }
        if (route.listResponse !== undefined) return route.listResponse(preset)
        const onPage2 = parsed.searchParams.get('page_token') === 'p2'
        const memories = onPage2 ? (route.page2?.[preset] ?? []) : (collections[preset] ?? [])
        return jsonResponse({ memories, nextPageToken: onPage2 ? '' : (route.listNextToken ?? '') })
      }
    }

    throw new Error(`unexpected fetch: ${url} ${method}`)
  })
}

describe('MemoryView', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = makeFetchMock()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // selectPreset chooses the preset and waits for its collection to render.
  async function selectPreset(value: string): Promise<void> {
    await screen.findByTestId('memory-select-guide')
    fireEvent.change(screen.getByTestId('memory-preset-select'), { target: { value } })
  }

  it('未选择 preset：呈现选择引导、选择器列出全角色 preset id，不发集合请求', async () => {
    render(<MemoryView template="saolei" />)

    expect(await screen.findByTestId('memory-select-guide')).toBeTruthy()
    expect(screen.queryByTestId('memory-item')).toBeNull()
    // preset 选择器：全角色列表，选项文案仅为资源名末段 id（template 段与
    // role 不进入 UI）。
    expect(fetchMock).toHaveBeenCalledWith('/api/v2/templates/saolei/presets', undefined)
    const options = Array.from(
      (screen.getByTestId('memory-preset-select') as HTMLSelectElement).options,
    ).map((option) => option.textContent)
    expect(options).toEqual(['请选择 preset', P1, P2])
    // 未选择前不请求任何集合（正向断言 preset 路由被 exercise）。
    expect(fetchMock.mock.calls.some((call) => String(call[0]).includes('/memories'))).toBe(false)
  })

  it('选择 preset：以 update_time 降序分页请求，行呈现 content 与 updateTime', async () => {
    render(<MemoryView template="saolei" />)
    await selectPreset(P1)

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `${LIST_BASE}/${P1}/memories?page_size=20&order_by=update_time+desc`,
        undefined,
      )
    })
    // 列表序 = 服务端返回序（fixture 为 update_time 降序，最近优先）。
    await waitFor(() => {
      expect(screen.getAllByTestId('memory-content').map((el) => el.textContent)).toEqual([
        E1.content,
        E2.content,
      ])
    })
    expect(screen.getAllByTestId('memory-time').map((el) => el.textContent)).toEqual([
      new Date(E1.updateTime).toLocaleString(),
      new Date(E2.updateTime).toLocaleString(),
    ])
  })

  it('创建成功：POST 携带内容摘要 id 与扁平 memory body，保存后条目入列', async () => {
    render(<MemoryView template="saolei" />)
    await selectPreset(P1)
    await screen.findAllByTestId('memory-item')

    fireEvent.click(screen.getByTestId('create-memory'))
    // 独占表单：编辑期间列表不渲染。
    expect(screen.getByTestId('memory-form')).toBeTruthy()
    expect(screen.queryByTestId('memory-item')).toBeNull()

    fireEvent.change(screen.getByTestId('memory-content-input'), {
      target: { value: '新要点' },
    })
    fireEvent.click(screen.getByTestId('memory-save'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringMatching(
          new RegExp(`^${LIST_BASE}/${P1}/memories\\?memory_id=[a-f0-9]{32}$`),
        ),
        expect.objectContaining({
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          // body 即 Memory 消息本体（projects/game/game.proto body:"memory"）。
          body: JSON.stringify({ content: '新要点' }),
        }),
      )
    })
    await waitFor(() => {
      expect(screen.queryByTestId('memory-form')).toBeNull()
    })
    await waitFor(() => {
      expect(screen.getAllByTestId('memory-content').map((el) => el.textContent)).toContain(
        '新要点',
      )
    })
  })

  it('重复内容 409：呈现"已存在"提示、停留表单且草稿不丢', async () => {
    fetchMock = makeFetchMock({ collections: { [P1]: [] }, createStatus: 409 })
    vi.stubGlobal('fetch', fetchMock)
    render(<MemoryView template="saolei" />)
    await selectPreset(P1)

    fireEvent.click(await screen.findByTestId('memory-empty-create'))
    fireEvent.change(screen.getByTestId('memory-content-input'), {
      target: { value: '重复内容' },
    })
    fireEvent.click(screen.getByTestId('memory-save'))

    await waitFor(() => {
      expect(screen.getByTestId('memory-error').textContent).toContain('已存在')
    })
    // 正向断言 POST 已发出（mock 被 exercise），失败停留表单、草稿保留。
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining(`/memories?memory_id=`),
      expect.objectContaining({ method: 'POST' }),
    )
    expect(screen.getByTestId('memory-form')).toBeTruthy()
    expect((screen.getByTestId('memory-content-input') as HTMLTextAreaElement).value).toBe(
      '重复内容',
    )

    // 取消返回列表并清除错误（错误属于编辑操作上下文，不残留为列表态错误）。
    fireEvent.click(screen.getByTestId('memory-cancel'))
    expect(screen.queryByTestId('memory-form')).toBeNull()
    expect(screen.queryByTestId('memory-error')).toBeNull()
  })

  it('400 兜底：服务端参数错误（超限）经信封 message 呈现', async () => {
    fetchMock = makeFetchMock({ collections: { [P1]: [] }, createStatus: 400 })
    vi.stubGlobal('fetch', fetchMock)
    render(<MemoryView template="saolei" />)
    await selectPreset(P1)

    fireEvent.click(await screen.findByTestId('memory-empty-create'))
    fireEvent.change(screen.getByTestId('memory-content-input'), {
      target: { value: '长度在预检内但被服务端拒绝' },
    })
    fireEvent.click(screen.getByTestId('memory-save'))

    await waitFor(() => {
      expect(screen.getByTestId('memory-error').textContent).toContain('content 不合法')
    })
    // 呈现的是信封内的领域消息，而非原始 JSON 信封。
    expect(screen.getByTestId('memory-error').textContent).toContain('1024 characters')
    expect(screen.getByTestId('memory-error').textContent).not.toContain('"code"')
  })

  it('超限预检以 rune 度量：超过 1024 rune 不发请求；UTF-16 长度超出但 rune 在内可通过', async () => {
    render(<MemoryView template="saolei" />)
    await selectPreset(P1)
    await screen.findAllByTestId('memory-item')

    // 1025 个中文 rune：预检拒绝，无 POST（正向断言计数为零）。
    fireEvent.click(screen.getByTestId('create-memory'))
    fireEvent.change(screen.getByTestId('memory-content-input'), {
      target: { value: '中'.repeat(1025) },
    })
    fireEvent.click(screen.getByTestId('memory-save'))
    await waitFor(() => {
      expect(screen.getByTestId('memory-error').textContent).toContain('1024')
    })
    expect(
      fetchMock.mock.calls.filter(
        (call) => (call[1] as RequestInit | undefined)?.method === 'POST',
      ),
    ).toHaveLength(0)

    // 513 个星面字符 = 513 rune（UTF-16 length 1026）：rune 度量下在内，放行。
    fireEvent.change(screen.getByTestId('memory-content-input'), {
      target: { value: '𝌆'.repeat(513) },
    })
    fireEvent.click(screen.getByTestId('memory-save'))
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.filter(
          (call) => (call[1] as RequestInit | undefined)?.method === 'POST',
        ),
      ).toHaveLength(1)
    })
  })

  it('编辑保存：独占表单预填全文，PATCH update_mask=content，列表反映新值', async () => {
    render(<MemoryView template="saolei" />)
    await selectPreset(P1)
    await screen.findAllByTestId('memory-item')

    fireEvent.click(screen.getAllByTestId('memory-edit')[0] as HTMLElement)
    expect(screen.getByTestId('memory-form')).toBeTruthy()
    expect(screen.queryByTestId('memory-item')).toBeNull()
    expect((screen.getByTestId('memory-content-input') as HTMLTextAreaElement).value).toBe(E1.content)

    fireEvent.change(screen.getByTestId('memory-content-input'), {
      target: { value: '改后内容' },
    })
    fireEvent.click(screen.getByTestId('memory-save'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `${LIST_BASE}/${P1}/memories/m1?update_mask=content`,
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({ name: E1.name, content: '改后内容' }),
        }),
      )
    })
    await waitFor(() => {
      expect(screen.queryByTestId('memory-form')).toBeNull()
    })
    await waitFor(() => {
      expect(screen.getAllByTestId('memory-content').map((el) => el.textContent)).toContain(
        '改后内容',
      )
    })
  })

  it('编辑目标被外部删除：PATCH 404 呈现并返回列表', async () => {
    const route: MemoryRoute = {}
    fetchMock = makeFetchMock(route)
    vi.stubGlobal('fetch', fetchMock)
    render(<MemoryView template="saolei" />)
    await selectPreset(P1)
    await screen.findAllByTestId('memory-item')

    fireEvent.click(screen.getAllByTestId('memory-edit')[0] as HTMLElement)
    route.patchStatus = 404
    fireEvent.change(screen.getByTestId('memory-content-input'), { target: { value: '改不动了' } })
    fireEvent.click(screen.getByTestId('memory-save'))

    await waitFor(() => {
      expect(screen.queryByTestId('memory-form')).toBeNull()
    })
    expect(screen.getByTestId('memory-error').textContent).toContain('条目不存在')
  })

  it('删除：inline 二步确认，取消不发请求，确认后 DELETE 并移除条目', async () => {
    render(<MemoryView template="saolei" />)
    await selectPreset(P1)
    await screen.findAllByTestId('memory-item')

    fireEvent.click(screen.getAllByTestId('memory-delete')[1] as HTMLElement)
    // 待确认态：尚未发起 DELETE。
    expect(screen.getByTestId('memory-delete-confirm')).toBeTruthy()
    expect(
      fetchMock.mock.calls.filter(
        (call) => (call[1] as RequestInit | undefined)?.method === 'DELETE',
      ),
    ).toHaveLength(0)

    fireEvent.click(screen.getByTestId('memory-delete-cancel'))
    expect(screen.queryByTestId('memory-delete-confirm')).toBeNull()

    fireEvent.click(screen.getAllByTestId('memory-delete')[1] as HTMLElement)
    fireEvent.click(screen.getByTestId('memory-delete-confirm'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `${LIST_BASE}/${P1}/memories/m2`,
        expect.objectContaining({ method: 'DELETE' }),
      )
    })
    await waitFor(() => {
      expect(screen.getAllByTestId('memory-content').map((el) => el.textContent)).toEqual([
        E1.content,
      ])
    })
  })

  it('空态：集合为空呈现中性引导与创建入口', async () => {
    fetchMock = makeFetchMock({ collections: { [P1]: [] } })
    vi.stubGlobal('fetch', fetchMock)
    const { container } = render(<MemoryView template="saolei" />)
    await selectPreset(P1)

    const empty = await screen.findByTestId('memory-empty')
    expect(empty.textContent).toContain('还没有 memory 条目')
    // 消费方以中性词指代（FR-016）。
    expect(empty.textContent).toContain('agent')
    // 空态文案（含表单占位/无障碍标签面）场景无关。
    expect(findSceneWordsIn(container)).toEqual([])

    fireEvent.click(screen.getByTestId('memory-empty-create'))
    expect(screen.getByTestId('memory-form')).toBeTruthy()
    expect(screen.queryByTestId('memory-empty')).toBeNull()
  })

  it('错误态：preset 列表与集合列表失败分别呈现，不静默', async () => {
    fetchMock = makeFetchMock({ presetsStatus: 500 })
    vi.stubGlobal('fetch', fetchMock)
    render(<MemoryView template="saolei" />)
    await waitFor(() => {
      expect(screen.getByTestId('memory-presets-error').textContent).toContain('500')
    })

    cleanup()
    fetchMock = makeFetchMock({ listStatus: 500 })
    vi.stubGlobal('fetch', fetchMock)
    render(<MemoryView template="saolei" />)
    await selectPreset(P1)
    await waitFor(() => {
      expect(screen.getByTestId('memory-error').textContent).toContain('500')
    })
    // 失败时不误报空集合。
    expect(screen.queryByTestId('memory-empty')).toBeNull()
  })

  it('分页：下一页携带 page_token，上一页回首页', async () => {
    fetchMock = makeFetchMock({
      collections: { [P1]: [E1] },
      listNextToken: 'p2',
      page2: { [P1]: [E2] },
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<MemoryView template="saolei" />)
    await selectPreset(P1)

    expect((await screen.findByTestId('memory-content')).textContent).toBe(E1.content)
    expect((screen.getByTestId('memory-prev') as HTMLButtonElement).disabled).toBe(true)

    fireEvent.click(screen.getByTestId('memory-next'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        `${LIST_BASE}/${P1}/memories?page_size=20&page_token=p2&order_by=update_time+desc`,
        undefined,
      )
    })
    expect((await screen.findByTestId('memory-content')).textContent).toBe(E2.content)
    expect(screen.getByTestId('memory-page').textContent).toContain('第 2 页')

    fireEvent.click(screen.getByTestId('memory-prev'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenLastCalledWith(
        `${LIST_BASE}/${P1}/memories?page_size=20&order_by=update_time+desc`,
        undefined,
      )
    })
    expect((await screen.findByTestId('memory-content')).textContent).toBe(E1.content)
  })

  it('切换 preset：重置列表与分页（不保留旧集合页 token），表单随切换关闭', async () => {
    fetchMock = makeFetchMock({
      collections: { [P1]: [E1], [P2]: [E3] },
      listNextToken: 'p2',
      page2: { [P1]: [E1] },
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<MemoryView template="saolei" />)
    await selectPreset(P1)
    await screen.findByTestId('memory-content')

    // 翻到第 2 页，再打开创建表单。
    fireEvent.click(screen.getByTestId('memory-next'))
    await waitFor(() => {
      expect(screen.getByTestId('memory-page').textContent).toContain('第 2 页')
    })
    fireEvent.click(screen.getByTestId('create-memory'))
    expect(screen.getByTestId('memory-form')).toBeTruthy()

    // 切换 preset：以新集合首页重新取数（无 page_token），表单关闭。
    fireEvent.change(screen.getByTestId('memory-preset-select'), { target: { value: P2 } })
    await waitFor(() => {
      expect(fetchMock).toHaveBeenLastCalledWith(
        `${LIST_BASE}/${P2}/memories?page_size=20&order_by=update_time+desc`,
        undefined,
      )
    })
    expect(screen.queryByTestId('memory-form')).toBeNull()
    expect((await screen.findByTestId('memory-content')).textContent).toBe(E3.content)
    expect(screen.getByTestId('memory-page').textContent).toContain('第 1 页')
  })

  it('竞态：切换 preset 后旧集合的晚到响应不落地（FR-008 零泄漏）', async () => {
    // P1 的列表响应由 gate 持有：先切到 P2 并让其响应完成，再放行 P1 的响应。
    let releaseP1: () => void = () => {}
    const p1Released = new Promise<void>((resolve) => {
      releaseP1 = resolve
    })
    fetchMock = makeFetchMock({
      collections: { [P1]: [E1], [P2]: [E3] },
      listResponse: async (preset) => {
        if (preset === P1) {
          await p1Released
          return jsonResponse({ memories: [E1] })
        }
        return jsonResponse({ memories: [E3] })
      },
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<MemoryView template="saolei" />)

    await selectPreset(P1)
    // P1 请求已发出且响应挂起（正向断言 mock 被 exercise）。
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some((call) => String(call[0]).includes(`/${P1}/memories`)),
      ).toBe(true)
    })

    // 切到 P2：P2 的响应先落地，列表呈现 P2 数据。
    fireEvent.change(screen.getByTestId('memory-preset-select'), { target: { value: P2 } })
    expect((await screen.findByTestId('memory-content')).textContent).toBe(E3.content)

    // 放行 P1 的晚到响应：纪元与当前选择均不匹配 → 整体丢弃，列表仍为 P2 数据。
    releaseP1()
    await flush()
    expect((screen.getByTestId('memory-preset-select') as HTMLSelectElement).value).toBe(P2)
    expect(screen.getAllByTestId('memory-content').map((el) => el.textContent)).toEqual([E3.content])
    expect(screen.queryByText(E1.content)).toBeNull()
  })

  it('场景词扫描：引导/列表/表单/错误态渲染输出均无场景词', async () => {
    fetchMock = makeFetchMock({ collections: { [P1]: [E1] }, createStatus: 409 })
    vi.stubGlobal('fetch', fetchMock)
    const { container } = render(<MemoryView template="saolei" />)
    const scan = (): void => {
      expect(findSceneWordsIn(container)).toEqual([])
    }

    await screen.findByTestId('memory-select-guide')
    scan()

    await selectPreset(P1)
    await screen.findByTestId('memory-item')
    scan()

    fireEvent.click(screen.getByTestId('create-memory'))
    fireEvent.change(screen.getByTestId('memory-content-input'), { target: { value: '重复内容' } })
    fireEvent.click(screen.getByTestId('memory-save'))
    await waitFor(() => {
      expect(screen.getByTestId('memory-error')).toBeTruthy()
    })
    scan()

    // fixture 的 preset role（player/planner）只作为资源数据传递，不进入渲染
    // 输出——扫描覆盖选择器选项文案。
    fireEvent.click(screen.getByTestId('memory-cancel'))
    expect(screen.getByTestId('memory-preset-select').textContent).not.toContain('player')
    scan()
  })
})
