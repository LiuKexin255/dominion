// @vitest-environment jsdom
// PresetsView 组件测试（specs/054-agent-v2-bugfixes/contracts/web-ui.md §8
// 测试义务 4：视图切换矩阵——进入/保存/取消/失败/外部删除竞态与字段语义；
// CRUD 交互与 API 调用形状的契约基线见 specs/051-agent-v2-dsh-migration/
// contracts/web-frontend.md §6 测试义务 2）；另覆盖 App 层视图切换
// （sessions | presets 单页 state）。Mock 约定照 style/javascript.md：
// vi.fn() double。
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '../App.js'
import { PresetsView } from './PresetsView.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

const PRESET_P1 = {
  name: 'templates/saolei/presets/p1',
  persona: '你是扫雷玩家',
  role: 'player',
  createTime: '2026-08-29T00:00:00Z',
  updateTime: '2026-08-29T01:00:00Z',
}

const PRESET_PLANNER = {
  name: 'templates/saolei/presets/p-planner',
  persona: '你是扫雷 planner',
  role: 'planner',
  createTime: '2026-08-29T00:30:00Z',
  updateTime: '2026-08-29T00:30:00Z',
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

interface PresetsRoute {
  // 初始 preset 集合（GET 响应随 create/delete 变化——mock 内存集合有状态，
  // 与真实 CRUD 往返一致）。
  initial?: typeof PRESET_P1[]
  createStatus?: number
  // updatePreset（PATCH）的响应状态；非 200 时模拟编辑保存失败路径。
  patchStatus?: number
  // 模拟外部删除：置位后 GET 列表不再返回该条目（组件 UI 之外的删除路径），
  // 用于驱动"正在编辑的条目被外部删除"的竞态检测。
  externalDelete?: string
}

// makeFetchMock routes the PresetsView calls: list/create/get/patch/delete on
// /api/v2/templates/saolei/presets（create 的 query 参数随 URL 一并到达，需按
// 前缀匹配）。The collection mutates across calls so post-CRUD refreshes
// observe the created/deleted state.
function makeFetchMock(route: PresetsRoute = {}) {
  const presets: typeof PRESET_P1[] = [...(route.initial ?? [PRESET_P1])]
  return vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    const method = init?.method ?? 'GET'
    if (url.startsWith('/api/v2/templates/saolei/presets') && method === 'GET') {
      // ListPresets 的 role 过滤由服务端承载（preset-api.md §1）：mock 按
      // query 参数过滤内存集合，空 = 不过滤。
      const filter = url.includes('?role=') ? decodeURIComponent(url.split('?role=')[1] ?? '') : ''
      const visible = route.externalDelete !== undefined ? presets.filter((p) => p.name !== route.externalDelete) : presets
      return jsonResponse({ presets: filter === '' ? visible : visible.filter((p) => p.role === filter) })
    }
    if (url.startsWith('/api/v2/templates/saolei/presets?preset_id=') && method === 'POST') {
      if (route.createStatus !== undefined && route.createStatus !== 200) {
        return jsonResponse('preset already exists', route.createStatus)
      }
      const id = decodeURIComponent((url.split('preset_id=')[1] ?? '').split('&')[0] ?? '')
      const role = decodeURIComponent(url.split('&role=')[1] ?? '')
      const created = {
        name: `templates/saolei/presets/${id}`,
        persona: JSON.parse(String(init?.body)).persona as string,
        role,
        createTime: '2026-08-29T02:00:00Z',
        updateTime: '2026-08-29T02:00:00Z',
      }
      presets.push(created)
      return jsonResponse(created)
    }
    // updatePreset 的 PATCH 带 ?update_mask= query（api/agent.js），按前缀
    // 匹配；更新同时刷新 update_time（服务端 OUTPUT_ONLY 字段语义），列表
    // 刷新后据此断言更新后的条目呈现。
    if (url.startsWith('/api/v2/templates/saolei/presets/p1?update_mask=') && method === 'PATCH') {
      const prompt = JSON.parse(String(init?.body)).persona as string
      if (route.patchStatus !== undefined && route.patchStatus !== 200) {
        return jsonResponse('preset not found', route.patchStatus)
      }
      const updated = { ...PRESET_P1, persona: prompt, updateTime: '2026-08-29T03:00:00Z' }
      const idx = presets.findIndex((p) => p.name === PRESET_P1.name)
      if (idx >= 0) presets[idx] = updated
      return jsonResponse(updated)
    }
    if (url === '/api/v2/templates/saolei/presets/p1' && method === 'DELETE') {
      const idx = presets.findIndex((p) => p.name === PRESET_P1.name)
      if (idx >= 0) presets.splice(idx, 1)
      return jsonResponse({})
    }
    throw new Error(`unexpected fetch: ${url} ${method}`)
  })
}

describe('PresetsView', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = makeFetchMock()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('列表呈现 preset 名称与更新时间', async () => {
    render(<PresetsView template="saolei" />)
    expect((await screen.findByTestId('preset-name')).textContent).toBe('p1')
    expect(screen.getByTestId('preset-time')).toBeTruthy()
  })

  it('列表 role 标识：每条目呈现所属池（player/planner）', async () => {
    fetchMock = makeFetchMock({ initial: [PRESET_P1, PRESET_PLANNER] })
    vi.stubGlobal('fetch', fetchMock)
    render(<PresetsView template="saolei" />)

    const badges = await screen.findAllByTestId('preset-role')
    expect(badges.map((b) => b.textContent)).toEqual(['player', 'planner'])
    expect(badges.map((b) => b.getAttribute('data-role'))).toEqual(['player', 'planner'])
  })

  it('role 过滤：选择池经 listPresets role 参数重取列表，全部 = 无参数', async () => {
    fetchMock = makeFetchMock({ initial: [PRESET_P1, PRESET_PLANNER] })
    vi.stubGlobal('fetch', fetchMock)
    render(<PresetsView template="saolei" />)
    expect(await screen.findAllByTestId('preset-name')).toHaveLength(2)

    fireEvent.change(screen.getByTestId('preset-role-filter'), { target: { value: 'player' } })
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/v2/templates/saolei/presets?role=player', undefined)
    })
    await waitFor(() => {
      expect(screen.getAllByTestId('preset-name').map((n) => n.textContent)).toEqual(['p1'])
    })

    fireEvent.change(screen.getByTestId('preset-role-filter'), { target: { value: 'planner' } })
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/v2/templates/saolei/presets?role=planner', undefined)
    })
    await waitFor(() => {
      expect(screen.getAllByTestId('preset-name').map((n) => n.textContent)).toEqual(['p-planner'])
    })

    // 全部 = 不过滤（preset-api.md §1 role 空字符串）。
    fireEvent.change(screen.getByTestId('preset-role-filter'), { target: { value: '' } })
    await waitFor(() => {
      expect(screen.getAllByTestId('preset-name')).toHaveLength(2)
    })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/v2/templates/saolei/presets', undefined)
  })

  it('过滤池下无条目：呈现池专属空态，新建入口预选该池', async () => {
    fetchMock = makeFetchMock({ initial: [PRESET_P1] })
    vi.stubGlobal('fetch', fetchMock)
    render(<PresetsView template="saolei" />)
    await screen.findByTestId('preset-name')

    fireEvent.change(screen.getByTestId('preset-role-filter'), { target: { value: 'planner' } })
    const empty = await screen.findByTestId('presets-empty')
    expect(empty.textContent).toContain('还没有 planner 角色的 preset')

    fireEvent.click(screen.getByTestId('presets-empty-create'))
    expect((screen.getByTestId('preset-role-planner') as HTMLInputElement).checked).toBe(true)
  })

  it('空态呈现引导文案与新建入口', async () => {
    fetchMock = makeFetchMock({ initial: [] })
    vi.stubGlobal('fetch', fetchMock)
    render(<PresetsView template="saolei" />)

    const empty = await screen.findByTestId('presets-empty')
    expect(empty.textContent).toContain('先创建 preset 才能物化 team')

    // 空态新建入口打开表单；独占编辑视图下空态引导不再渲染。
    fireEvent.click(screen.getByTestId('presets-empty-create'))
    expect(screen.getByTestId('preset-form')).toBeTruthy()
    expect(screen.queryByTestId('presets-empty')).toBeNull()
  })

  it('新建：POST 携带 query preset_id/role 与 body persona，成功后刷新列表并呈现 role 标识', async () => {
    fetchMock = makeFetchMock({ initial: [] })
    vi.stubGlobal('fetch', fetchMock)
    render(<PresetsView template="saolei" />)

    fireEvent.click(await screen.findByTestId('presets-empty-create'))
    fireEvent.change(screen.getByTestId('preset-name-input'), { target: { value: 'p2' } })
    fireEvent.click(screen.getByTestId('preset-role-player'))
    fireEvent.change(screen.getByTestId('preset-prompt-input'), { target: { value: '提示词\n第二行' } })
    fireEvent.click(screen.getByTestId('preset-save'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v2/templates/saolei/presets?preset_id=p2&role=player',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ persona: '提示词\n第二行' }),
        }),
      )
    })
    // 保存后列表刷新（第二次 GET），新条目呈现——表单关闭、列表回归、
    // role 标识呈现新建时选择的池。
    await waitFor(() => {
      expect((screen.getByTestId('preset-name') as HTMLElement).textContent).toBe('p2')
    })
    expect(screen.getByTestId('preset-item')).toBeTruthy()
    expect((screen.getByTestId('preset-role') as HTMLElement).textContent).toBe('player')
    expect(screen.queryByTestId('preset-form')).toBeNull()
  })

  it('新建 role 必选：未选 role 时保存禁用且不发起请求', async () => {
    fetchMock = makeFetchMock({ initial: [] })
    vi.stubGlobal('fetch', fetchMock)
    render(<PresetsView template="saolei" />)

    fireEvent.click(await screen.findByTestId('presets-empty-create'))
    fireEvent.change(screen.getByTestId('preset-name-input'), { target: { value: 'p2' } })
    // 名称已填但 role 未选：保存仍禁用、无 POST。
    expect((screen.getByTestId('preset-save') as HTMLButtonElement).disabled).toBe(true)

    // 选中 role 后解除（必选校验）。
    fireEvent.click(screen.getByTestId('preset-role-planner'))
    expect((screen.getByTestId('preset-save') as HTMLButtonElement).disabled).toBe(false)
    expect(
      fetchMock.mock.calls.filter((c) => (c[1] as RequestInit | undefined)?.method === 'POST'),
    ).toHaveLength(0)
  })

  it('新建名称必选：名称为空时保存禁用且不发起请求', async () => {
    fetchMock = makeFetchMock({ initial: [] })
    vi.stubGlobal('fetch', fetchMock)
    render(<PresetsView template="saolei" />)

    fireEvent.click(await screen.findByTestId('presets-empty-create'))
    expect((screen.getByTestId('preset-save') as HTMLButtonElement).disabled).toBe(true)
    expect(fetchMock.mock.calls.filter((c) => (c[1] as RequestInit | undefined)?.method === 'POST')).toHaveLength(0)
  })

  it('编辑：表单预填、名称只读，保存走 PATCH update_mask=persona', async () => {
    render(<PresetsView template="saolei" />)
    fireEvent.click(await screen.findByTestId('preset-edit'))

    // 独占编辑视图：编辑期间列表条目不渲染（无"可见但禁用"残留）。
    expect(screen.getByTestId('preset-form')).toBeTruthy()
    expect(screen.queryByTestId('preset-item')).toBeNull()
    expect(screen.queryByTestId('presets-empty')).toBeNull()
    // 头部按钮守卫：新建禁用（防止静默重置进行中的表单），刷新保持可用
    // （竞态检测的驱动面）。
    expect((screen.getByTestId('create-preset') as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByTestId('refresh-presets') as HTMLButtonElement).disabled).toBe(false)

    const nameInput = screen.getByTestId('preset-name-input') as HTMLInputElement
    expect(nameInput.value).toBe('p1')
    expect(nameInput.disabled).toBe(true)
    expect((screen.getByTestId('preset-prompt-input') as HTMLTextAreaElement).value).toBe('你是扫雷玩家')

    fireEvent.change(screen.getByTestId('preset-prompt-input'), { target: { value: '新的提示词' } })
    fireEvent.click(screen.getByTestId('preset-save'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v2/templates/saolei/presets/p1?update_mask=persona',
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({ persona: '新的提示词' }),
        }),
      )
    })

    // 保存成功路径：表单关闭（save 失败会保留表单并进错误态）、无错误提示，
    // 列表经第二次 GET 刷新且更新后的条目呈现（update_time 反映本次编辑）。
    await waitFor(() => {
      expect(screen.queryByTestId('preset-form')).toBeNull()
    })
    expect(screen.queryByTestId('presets-error')).toBeNull()
    expect(screen.getByTestId('preset-item')).toBeTruthy()
    const listGets = fetchMock.mock.calls.filter(
      (c) => c[0] === '/api/v2/templates/saolei/presets' && ((c[1] as RequestInit | undefined)?.method ?? 'GET') === 'GET',
    )
    expect(listGets.length).toBeGreaterThanOrEqual(2)
    expect((screen.getByTestId('preset-name') as HTMLElement).textContent).toBe('p1')
    expect((screen.getByTestId('preset-time') as HTMLElement).textContent).toBe(
      new Date('2026-08-29T03:00:00Z').toLocaleString(),
    )
  })

  it('编辑仅 persona：无 role 可改（只读 role + 不可变提示），PATCH 载荷无 role', async () => {
    render(<PresetsView template="saolei" />)
    fireEvent.click(await screen.findByTestId('preset-edit'))

    // 编辑面不出现 role 单选项（role 创建后不可变，preset-api.md §2）；
    // role 以只读值呈现并附不可变提示。
    expect(screen.queryByTestId('preset-role-field')).toBeNull()
    expect(screen.queryByTestId('preset-role-player')).toBeNull()
    expect(screen.queryByTestId('preset-role-planner')).toBeNull()
    expect((screen.getByTestId('preset-role-value') as HTMLElement).textContent).toBe('player')
    expect(screen.getByTestId('preset-role-readonly').textContent).toContain('创建后不可改')

    fireEvent.change(screen.getByTestId('preset-prompt-input'), { target: { value: '新的提示词' } })
    fireEvent.click(screen.getByTestId('preset-save'))
    await waitFor(() => {
      // 精确 body 断言：update_mask=persona，payload 仅 persona（无 role）。
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v2/templates/saolei/presets/p1?update_mask=persona',
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({ persona: '新的提示词' }),
        }),
      )
    })
  })

  it('过滤池中新建另一池 preset：保存后过滤切到新条目所属池', async () => {
    fetchMock = makeFetchMock({ initial: [PRESET_P1] })
    vi.stubGlobal('fetch', fetchMock)
    render(<PresetsView template="saolei" />)
    await screen.findByTestId('preset-name')

    fireEvent.change(screen.getByTestId('preset-role-filter'), { target: { value: 'player' } })
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/v2/templates/saolei/presets?role=player', undefined)
    })

    fireEvent.click(screen.getByTestId('create-preset'))
    fireEvent.change(screen.getByTestId('preset-name-input'), { target: { value: 'p3' } })
    fireEvent.click(screen.getByTestId('preset-role-planner'))
    fireEvent.click(screen.getByTestId('preset-save'))

    // roleFilter 变更经 refresh effect 重取 planner 池，新条目可见。
    await waitFor(() => {
      expect((screen.getByTestId('preset-role-filter') as HTMLSelectElement).value).toBe('planner')
    })
    await waitFor(() => {
      expect((screen.getByTestId('preset-name') as HTMLElement).textContent).toBe('p3')
    })
    expect((screen.getByTestId('preset-role') as HTMLElement).textContent).toBe('planner')
  })

  it('删除带确认：确认后 DELETE 资源名；取消不发起请求', async () => {
    render(<PresetsView template="saolei" />)
    fireEvent.click(await screen.findByTestId('preset-delete'))

    // 确认 UI 出现，尚未发起 DELETE。
    expect(screen.getByTestId('preset-delete-confirm')).toBeTruthy()
    expect(fetchMock.mock.calls.filter((c) => (c[1] as RequestInit | undefined)?.method === 'DELETE')).toHaveLength(0)

    // 取消：无请求，条目保留。
    fireEvent.click(screen.getByTestId('preset-delete-cancel'))
    expect(screen.queryByTestId('preset-delete-confirm')).toBeNull()

    // 再删除并确认：DELETE 完整资源名，列表刷新后条目移除。
    fireEvent.click(screen.getByTestId('preset-delete'))
    fireEvent.click(screen.getByTestId('preset-delete-confirm'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v2/templates/saolei/presets/p1',
        expect.objectContaining({ method: 'DELETE' }),
      )
    })
    // 列表刷新后条目移除（mock 集合已删），回到空态引导。
    await waitFor(() => {
      expect(screen.queryByTestId('preset-item')).toBeNull()
    })
    expect(screen.getByTestId('presets-empty')).toBeTruthy()
  })

  it('创建冲突（409）：错误呈现、停留编辑视图且内容不丢；取消返回列表并清除错误', async () => {
    fetchMock = makeFetchMock({ initial: [], createStatus: 409 })
    vi.stubGlobal('fetch', fetchMock)
    render(<PresetsView template="saolei" />)

    fireEvent.click(await screen.findByTestId('presets-empty-create'))
    fireEvent.change(screen.getByTestId('preset-name-input'), { target: { value: 'p1' } })
    fireEvent.click(screen.getByTestId('preset-role-player'))
    fireEvent.change(screen.getByTestId('preset-prompt-input'), { target: { value: '草稿提示词' } })
    fireEvent.click(screen.getByTestId('preset-save'))

    await waitFor(() => {
      expect(screen.getByTestId('presets-error')).toBeTruthy()
    })
    // 失败停留编辑视图：表单保留、已输入内容不丢、role 选择保留，列表/空态
    // 均不渲染。
    expect(screen.getByTestId('preset-form')).toBeTruthy()
    expect((screen.getByTestId('preset-name-input') as HTMLInputElement).value).toBe('p1')
    expect((screen.getByTestId('preset-role-player') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByTestId('preset-prompt-input') as HTMLTextAreaElement).value).toBe('草稿提示词')
    expect(screen.queryByTestId('presets-empty')).toBeNull()

    // 取消返回列表，错误横幅随表单关闭一并清除（错误属于编辑操作上下文，
    // 不残留为列表态错误）。
    fireEvent.click(screen.getByTestId('preset-cancel'))
    expect(screen.queryByTestId('preset-form')).toBeNull()
    expect(screen.queryByTestId('presets-error')).toBeNull()
    expect(screen.getByTestId('presets-empty')).toBeTruthy()
  })

  it('编辑保存失败（PATCH 500）：错误呈现、停留编辑视图且 prompt 草稿不丢', async () => {
    fetchMock = makeFetchMock({ patchStatus: 500 })
    vi.stubGlobal('fetch', fetchMock)
    render(<PresetsView template="saolei" />)

    fireEvent.click(await screen.findByTestId('preset-edit'))
    fireEvent.change(screen.getByTestId('preset-prompt-input'), { target: { value: '改了一半的提示词' } })
    fireEvent.click(screen.getByTestId('preset-save'))

    // 保存请求按 update_mask 发出（正向断言 mock 被 exercise）。
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v2/templates/saolei/presets/p1?update_mask=persona',
        expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ persona: '改了一半的提示词' }) }),
      )
    })
    await waitFor(() => {
      expect(screen.getByTestId('presets-error')).toBeTruthy()
    })
    // 失败停留编辑视图：表单保留、prompt 草稿不丢、名称仍只读。
    expect(screen.getByTestId('preset-form')).toBeTruthy()
    expect((screen.getByTestId('preset-prompt-input') as HTMLTextAreaElement).value).toBe('改了一半的提示词')
    expect((screen.getByTestId('preset-name-input') as HTMLInputElement).disabled).toBe(true)
  })

  it('新建为独占视图：进入后列表不渲染，取消返回列表且内容不变', async () => {
    render(<PresetsView template="saolei" />)
    await screen.findByTestId('preset-name')

    fireEvent.click(screen.getByTestId('create-preset'))
    expect(screen.getByTestId('preset-form')).toBeTruthy()
    expect(screen.queryByTestId('preset-item')).toBeNull()
    expect(screen.queryByTestId('presets-empty')).toBeNull()
    // 新建：名称可输入（051 语义延续）。
    const nameInput = screen.getByTestId('preset-name-input') as HTMLInputElement
    expect(nameInput.disabled).toBe(false)
    fireEvent.change(nameInput, { target: { value: 'draft' } })

    // 取消返回列表：条目原样呈现，草稿不落入列表。
    fireEvent.click(screen.getByTestId('preset-cancel'))
    expect(screen.queryByTestId('preset-form')).toBeNull()
    expect(screen.getByTestId('preset-item')).toBeTruthy()
    expect((screen.getByTestId('preset-name') as HTMLElement).textContent).toBe('p1')
    expect(screen.queryByTestId('presets-error')).toBeNull()
  })

  it('正在编辑的条目被外部删除：刷新列表后自动关闭表单返回列表', async () => {
    const route: PresetsRoute = { patchStatus: 500 }
    fetchMock = makeFetchMock(route)
    vi.stubGlobal('fetch', fetchMock)
    render(<PresetsView template="saolei" />)

    fireEvent.click(await screen.findByTestId('preset-edit'))
    expect(screen.getByTestId('preset-form')).toBeTruthy()
    expect(screen.queryByTestId('preset-item')).toBeNull()

    // 先制造编辑态错误：保存失败，错误横幅呈现且表单停留。
    fireEvent.change(screen.getByTestId('preset-prompt-input'), { target: { value: '改了一半的提示词' } })
    fireEvent.click(screen.getByTestId('preset-save'))
    await waitFor(() => {
      expect(screen.getByTestId('presets-error')).toBeTruthy()
    })
    expect(screen.getByTestId('preset-form')).toBeTruthy()

    // 条目在组件之外被删除（独占视图下无删除入口），用户点击刷新拉取
    // 最新列表——刷新按钮在编辑期间保持可用，是竞态检测的触发面。
    route.externalDelete = PRESET_P1.name
    fireEvent.click(screen.getByTestId('refresh-presets'))

    await waitFor(() => {
      expect(screen.queryByTestId('preset-form')).toBeNull()
    })
    // 错误横幅随表单关闭一并清除（closeForm 统一收口编辑态错误），不残留
    // 为列表态错误。
    expect(screen.queryByTestId('presets-error')).toBeNull()
    // 返回列表视图（集合已空 → 空态引导），不残留已删除条目的编辑态。
    expect(screen.getByTestId('presets-empty')).toBeTruthy()
  })

  it('编辑期间刷新且条目仍在：表单保持打开（竞态误关闭负向路径）', async () => {
    render(<PresetsView template="saolei" />)
    fireEvent.click(await screen.findByTestId('preset-edit'))
    fireEvent.change(screen.getByTestId('preset-prompt-input'), { target: { value: '编辑中的草稿' } })

    // 刷新返回的集合仍含被编辑条目（externalDelete 未启用）→ 不误关闭。
    fireEvent.click(screen.getByTestId('refresh-presets'))
    await waitFor(() => {
      // 刷新完成：加载态退出（此时 presets 更新已提交、竞态 effect 已执行）。
      expect(screen.queryByText('加载中…')).toBeNull()
    })
    expect(screen.getByTestId('preset-form')).toBeTruthy()
    // 编辑草稿不受刷新影响。
    expect((screen.getByTestId('preset-prompt-input') as HTMLTextAreaElement).value).toBe('编辑中的草稿')
  })
})

// ─── App 层视图切换（web-frontend.md §2：侧栏底部 sessions | presets） ───────

describe('App 视图切换', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      const method = init?.method ?? 'GET'
      if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
        return jsonResponse({ sessions: [] })
      }
      if (url === '/api/v2/templates/saolei/presets' && method === 'GET') {
        return jsonResponse({ presets: [PRESET_P1] })
      }
      throw new Error(`unexpected fetch: ${url} ${method}`)
    })
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('底部切换 presets 显示管理视图，切回 sessions 恢复会话空态', async () => {
    render(<App />)

    fireEvent.click(await screen.findByTestId('view-presets'))
    expect(await screen.findByTestId('presets-view')).toBeTruthy()
    expect((await screen.findByTestId('preset-name')).textContent).toBe('p1')
    expect(screen.queryByTestId('empty-hint')).toBeNull()

    fireEvent.click(screen.getByTestId('view-sessions'))
    expect(await screen.findByTestId('empty-hint')).toBeTruthy()
    expect(screen.queryByTestId('presets-view')).toBeNull()
  })

  // wireChunk wraps one bare ChatEvent JSON line in the grpc-gateway v2
  // streaming envelope the real /api/v2 wire carries ({"result": <ChatEvent>}
  // + "\n" — conversation-api.md §2).
  function wireChunk(eventLine: string): string {
    return `{"result":${eventLine}}\n`
  }

  it('切 presets 再切回：会话面板不重挂载——List 仅请求一次且在途回合不丢', async () => {
    const S1 = 'templates/saolei/sessions/s1'
    // 在途回合：首帧「部」先呈现，其余帧等 release（切视图往返后完成）。
    const encoder = new TextEncoder()
    let releaseRest: () => void = () => {}
    const restReleased = new Promise<void>((resolve) => {
      releaseRest = resolve
    })
    let markDone: () => void = () => {}
    const done = new Promise<void>((resolve) => {
      markDone = resolve
    })

    fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
      const method = init?.method ?? 'GET'
      if (url === '/api/v1/templates/saolei/sessions' && method === 'GET') {
        return jsonResponse({ sessions: [{ name: S1, createTime: '2026-08-29T00:00:00Z' }] })
      }
      if (url === '/api/v2/templates/saolei/presets' && method === 'GET') {
        return jsonResponse({ presets: [PRESET_P1] })
      }
      if (url === `/api/v2/${S1}/team`) {
        return jsonResponse({
          name: `${S1}/team`,
          members: [
            { name: `${S1}/team/members/player`, role: 'player', preset: PRESET_P1.name },
            { name: `${S1}/team/members/planner`, role: 'planner', preset: PRESET_P1.name },
          ],
        })
      }
      if (url === `/api/v2/${S1}/team/messages`) {
        return jsonResponse({ messages: [] })
      }
      if (url === `/api/v2/${S1}:send` && method === 'POST') {
        const stream = new ReadableStream<Uint8Array>({
          async start(controller) {
            controller.enqueue(
              encoder.encode(
                wireChunk(
                  '{"teamMessage":{"member":"user","message":{"role":"ROLE_USER","blocks":[{"text":{"content":"一"}}]},"seq":"1"}}',
                ) +
                  wireChunk('{"member":"player","turnId":"t1","turnStart":{}}') +
                  wireChunk('{"member":"player","turnId":"t1","blockStart":{"index":0,"type":"BLOCK_TYPE_TEXT"}}') +
                  wireChunk('{"member":"player","turnId":"t1","delta":{"index":0,"text":"部"}}'),
              ),
            )
            await restReleased
            controller.enqueue(encoder.encode(wireChunk('{"member":"player","turnId":"t1","delta":{"index":0,"text":"分"}}')))
            controller.enqueue(encoder.encode(wireChunk('{"member":"player","turnId":"t1","blockEnd":{"index":0,"block":{"text":{"content":"部分"}}}}')))
            controller.enqueue(encoder.encode(wireChunk('{"teamMessage":{"member":"player","message":{"role":"ROLE_AGENT","blocks":[{"text":{"content":"部分"}}]},"seq":"2"}}')))
            controller.enqueue(encoder.encode(wireChunk('{"member":"player","turnId":"t1","turnEnd":{"status":"TURN_STATUS_COMPLETED"}}')))
            controller.close()
            markDone()
          },
        })
        return new Response(stream, { status: 200, headers: { 'Content-Type': 'application/json' } })
      }
      throw new Error(`unexpected fetch: ${url} ${method}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<App />)
    fireEvent.click(await screen.findByText('s1'))
    fireEvent.change(await screen.findByTestId('chat-input'), { target: { value: '一' } })
    fireEvent.click(screen.getByTestId('send-button'))
    await waitFor(() => {
      expect((screen.getByTestId('agent-text') as HTMLElement).textContent).toBe('部')
    })

    // 切到 presets 再切回：面板未被卸载（卸载会重跑回填 effect 并以空历史
    // 覆盖在途回合，随后帧因 live === null 被丢弃）。
    fireEvent.click(screen.getByTestId('view-presets'))
    expect(await screen.findByTestId('presets-view')).toBeTruthy()
    fireEvent.click(screen.getByTestId('view-sessions'))

    // 回合剩余帧在隐藏期间照常归约，切回后完整呈现——无重挂载的重复 List。
    releaseRest()
    await done
    await waitFor(() => {
      expect((screen.getByTestId('agent-text') as HTMLElement).textContent).toBe('部分')
    })
    expect(screen.getByText('一')).toBeTruthy()
    const listCalls = fetchMock.mock.calls.filter((c) => c[0] === `/api/v2/${S1}/team/messages`)
    expect(listCalls).toHaveLength(1)
  })
})
