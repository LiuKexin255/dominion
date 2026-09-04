// @vitest-environment jsdom
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Session } from '../api/sessions.js'
import { SessionList } from './SessionList.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

const S1 = 'templates/saolei/sessions/s1'
const S2 = 'templates/saolei/sessions/s2'

// theme.css 的 CSS 面断言（标题 nowrap、名称渐隐）按文件内容读取：jsdom 不
// 应用外部样式表，且 vitest 默认 css:false 会把 .css 模块（含 ?raw 变体）
// 替换为空串，故走文件系统。路径候选覆盖两种执行环境：bazel runfiles
// （js_test cwd = workspace 根，见 projects/game/web/frontend/BUILD.bazel）
// 与包目录下的 vitest CLI。
function loadThemeCss(): string {
  for (const base of [
    process.cwd(),
    resolve(process.cwd(), 'projects/game/web/frontend'),
  ]) {
    const path = resolve(base, 'src/theme.css')
    if (existsSync(path)) return readFileSync(path, 'utf8')
  }
  throw new Error('theme.css not found relative to cwd')
}

const THEME_CSS = loadThemeCss()

// vendored 官方 token sheets 的 union 内容与 index.html 原文（Menu 卡片视觉
// 断言面：specs/054-agent-v2-bugfixes/contracts/web-ui.md §1/§7 与
// specs/054-agent-v2-bugfixes/revisions/phase10-theme-css-carrier.md §4——
// jsdom 不应用外部样式表、不解析 var()，文件内容断言是本仓库可行断言面）。
function loadDshThemeSheets(): string {
  const dir = resolveFirstDir('src/dsh-theme')
  return readdirSync(dir)
    .filter((f) => f.endsWith('.css') && f !== 'index.css')
    .map((f) => readFileSync(join(dir, f), 'utf8'))
    .join('\n')
}

function loadIndexHtml(): string {
  for (const base of [
    process.cwd(),
    resolve(process.cwd(), 'projects/game/web/frontend'),
  ]) {
    const path = resolve(base, 'index.html')
    if (existsSync(path)) return readFileSync(path, 'utf8')
  }
  throw new Error('index.html not found relative to cwd')
}

function resolveFirstDir(rel: string): string {
  for (const base of [
    process.cwd(),
    resolve(process.cwd(), 'projects/game/web/frontend'),
  ]) {
    const path = resolve(base, rel)
    if (existsSync(path)) return path
  }
  throw new Error(`${rel} not found relative to cwd`)
}

const DSH_SHEETS = loadDshThemeSheets()
const INDEX_HTML = loadIndexHtml()

// Callback props 均以 vi.fn() 注入（style/javascript.md Mock 约定：DI seam，
// 零模块拦截），并对触发路径做正向断言。onDelete 可注入自定义 double（如
// 受控 Promise）以驱动"删除进行中"分支。
function renderList(
  options: {
    sessions?: Session[]
    selected?: string | null
    loading?: boolean
    error?: string | null
    onDelete?: (name: string) => void | Promise<void>
  } = {},
) {
  const callbacks = {
    onSelect: vi.fn(),
    onRefresh: vi.fn(),
    onCreate: vi.fn(),
    onDelete: options.onDelete ?? vi.fn(),
  }
  render(
    <SessionList
      sessions={
        options.sessions ?? [
          { name: S1, createTime: '2026-08-29T08:00:00Z' },
          { name: S2, createTime: '2026-08-28T08:00:00Z' },
        ]
      }
      selected={options.selected ?? null}
      loading={options.loading ?? false}
      error={options.error ?? null}
      onSelect={callbacks.onSelect}
      onRefresh={callbacks.onRefresh}
      onCreate={callbacks.onCreate}
      onDelete={callbacks.onDelete}
    />,
  )
  return callbacks
}

// openMenu 打开指定序号条目的 `···` 菜单。
function openMenu(index: number): void {
  fireEvent.click(screen.getAllByTestId('session-actions')[index]!)
}

// confirmDeleteViaMenu 走完整删除确认步：菜单 → 删除 → 确认删除。
function confirmDeleteViaMenu(index: number): void {
  openMenu(index)
  fireEvent.click(screen.getByRole('menuitem', { name: '删除' }))
  fireEvent.click(screen.getByRole('menuitem', { name: '确认删除' }))
}

describe('SessionList 列表呈现（US4）', () => {
  it('列表渲染名称与格式化后的创建时间（非原始 RFC 3339 串）', () => {
    renderList()

    const items = screen.getAllByTestId('session-item')
    expect(items).toHaveLength(2)
    expect(items[0]?.textContent).toContain('s1')
    expect(items[1]?.textContent).toContain('s2')

    const times = screen.getAllByTestId('session-time')
    expect(times[0]?.textContent).toContain('2026')
    // 格式化生效：不再是原始 ISO 串。
    expect(times[0]?.textContent).not.toBe('2026-08-29T08:00:00Z')
  })

  it('无 createTime 的条目不渲染时间格；空列表呈现空态文案', () => {
    const { rerender } = render(
      <SessionList
        sessions={[{ name: S1 }]}
        selected={null}
        loading={false}
        error={null}
        onSelect={vi.fn()}
        onRefresh={vi.fn()}
        onCreate={vi.fn()}
        onDelete={vi.fn()}
      />,
    )
    expect(screen.getByTestId('session-item').textContent).toBe('s1')
    expect(screen.queryByTestId('session-time')).toBeNull()

    rerender(
      <SessionList
        sessions={[]}
        selected={null}
        loading={false}
        error={null}
        onSelect={vi.fn()}
        onRefresh={vi.fn()}
        onCreate={vi.fn()}
        onDelete={vi.fn()}
      />,
    )
    expect(screen.queryByTestId('session-item')).toBeNull()
    expect(screen.getByText(/暂无 session/)).toBeTruthy()
  })

  it('列表错误呈现于侧栏错误区', () => {
    renderList({ error: '网络不可用' })
    expect(screen.getByText('网络不可用')).toBeTruthy()
  })
})

describe('SessionList 操作回调（US4）', () => {
  it('刷新图标按钮点击触发 onRefresh；loading 中禁用', () => {
    const { onRefresh } = renderList({ selected: S1 })
    fireEvent.click(screen.getByTestId('refresh-sessions'))
    expect(onRefresh).toHaveBeenCalledTimes(1)

    cleanup()
    renderList({ selected: S1, loading: true })
    const refresh = screen.getByTestId('refresh-sessions') as HTMLButtonElement
    expect(refresh.disabled).toBe(true)
  })

  it('新建图标按钮点击触发 onCreate', () => {
    const { onCreate } = renderList()
    fireEvent.click(screen.getByTestId('create-session'))
    expect(onCreate).toHaveBeenCalledTimes(1)
  })
})

// ─── 侧栏四项交互（specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §1） ───

describe('SessionList 侧栏交互（US4）', () => {
  it('FR-001 标题单行：长列表数据下计数正确且声明 nowrap', () => {
    const many: Session[] = Array.from({ length: 6 }, (_, i) => ({
      name: `templates/saolei/sessions/s${i}`,
    }))
    renderList({ sessions: many })

    const title = screen.getByTestId('sidebar-title')
    expect(title.textContent).toBe('Sessions (6)')
    // CSS 面：.sidebar-title 规则声明 white-space: nowrap（渲染断言：标题
    // 元素高度 = 单行行高需真实布局，jsdom 下以规则存在性承载该契约）。
    expect(THEME_CSS).toMatch(/\.sidebar-title\s*\{[^}]*white-space:\s*nowrap/s)
  })

  it('FR-002 图标按钮：aria-label 可达、图标 svg 呈现、点击触发回调', () => {
    const { onRefresh, onCreate } = renderList()

    const refresh = screen.getByTestId('refresh-sessions')
    expect(refresh.tagName).toBe('BUTTON')
    expect(refresh.getAttribute('aria-label')).toBe('刷新')
    expect(refresh.querySelector('svg')).not.toBeNull()
    fireEvent.click(refresh)
    expect(onRefresh).toHaveBeenCalledTimes(1)

    const create = screen.getByTestId('create-session')
    expect(create.tagName).toBe('BUTTON')
    expect(create.getAttribute('aria-label')).toBe('新建会话')
    expect(create.querySelector('svg')).not.toBeNull()
    fireEvent.click(create)
    expect(onCreate).toHaveBeenCalledTimes(1)
  })

  it('FR-002 loading 中新建/刷新图标按钮禁用', () => {
    renderList({ loading: true })
    expect((screen.getByTestId('refresh-sessions') as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByTestId('create-session') as HTMLButtonElement).disabled).toBe(true)
  })

  it('FR-003 ··· 菜单删除不依赖选中态：确认后回调携带该条目资源名', () => {
    const { onDelete } = renderList({ selected: null })

    const actions = screen.getAllByTestId('session-actions')
    expect(actions).toHaveLength(2)
    // 未选中任何条目，删除入口仍然可用。
    confirmDeleteViaMenu(0)
    expect(onDelete).toHaveBeenCalledTimes(1)
    expect(onDelete).toHaveBeenCalledWith(S1)
  })

  it('FR-003 删除确认步：未确认不回调；取消退出确认步', () => {
    const { onDelete } = renderList()
    openMenu(1)
    fireEvent.click(screen.getByRole('menuitem', { name: '删除' }))

    // 进入确认步但未点确认：回调未触发。
    expect(onDelete).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('menuitem', { name: '取消' }))
    expect(onDelete).not.toHaveBeenCalled()
    // 取消后回到删除项（确认步退出，可重新进入）。
    expect(screen.getByRole('menuitem', { name: '删除' })).toBeTruthy()
  })

  it('FR-003 删除进行中：该条目删除项禁用，完成后恢复', async () => {
    let release!: () => void
    const gated = new Promise<void>((resolve) => {
      release = resolve
    })
    const onDelete = vi.fn(() => gated)
    renderList({ onDelete })

    confirmDeleteViaMenu(0)
    expect(onDelete).toHaveBeenCalledWith(S1)

    // 删除进行中：重新打开该条目菜单，删除项禁用（disabled 条件从
    // 「无选中」改为「该条目删除进行中」）。
    openMenu(0)
    expect(
      (screen.getByRole('menuitem', { name: '删除' }) as HTMLButtonElement).disabled,
    ).toBe(true)

    // 完成后菜单保持打开，删除项恢复可用（deleting 清除即时反映在菜单上）。
    await act(async () => {
      release()
      await gated
    })
    expect(
      (screen.getByRole('menuitem', { name: '删除' }) as HTMLButtonElement).disabled,
    ).toBe(false)
    // 其余条目不受影响。
    openMenu(1)
    expect(
      (screen.getByRole('menuitem', { name: '删除' }) as HTMLButtonElement).disabled,
    ).toBe(false)
  })

  it('FR-003 onDelete 同步抛错：deleting 必然清理，条目删除项恢复可用', () => {
    const onDelete = vi.fn(() => {
      throw new Error('sync boom')
    })
    renderList({ onDelete })

    confirmDeleteViaMenu(0)
    expect(onDelete).toHaveBeenCalledWith(S1)

    // 同步抛错在 runDelete 的 async 包裹内被吞，deleting 在同一次 act
    // 批处理内清理完毕——重新打开菜单即恢复可用。
    openMenu(0)
    expect(
      (screen.getByRole('menuitem', { name: '删除' }) as HTMLButtonElement).disabled,
    ).toBe(false)
  })

  it('FR-003 onDelete 拒绝：deleting 必然清理且无 unhandled rejection', async () => {
    let reject!: (err: Error) => void
    const gated = new Promise<void>((_, rej) => {
      reject = rej
    })
    const onDelete = vi.fn(() => gated)
    renderList({ onDelete })

    confirmDeleteViaMenu(0)
    openMenu(0)
    expect(
      (screen.getByRole('menuitem', { name: '删除' }) as HTMLButtonElement).disabled,
    ).toBe(true)

    await act(async () => {
      reject(new Error('async boom'))
      // 拒绝由组件 catch 吞掉；本测试进程若出现 unhandled rejection，
      // vitest 默认判失败——用例通过即为无 unhandled rejection 的正向证明。
      try {
        await gated
      } catch {
        // 预期拒绝，已被组件路径消费。
      }
    })
    expect(
      (screen.getByRole('menuitem', { name: '删除' }) as HTMLButtonElement).disabled,
    ).toBe(false)
  })

  it('FR-004 长名虚化与悬停滚动：悬停切换滚动类，移出复位类与 scrollLeft', () => {
    const longName = `templates/saolei/sessions/${'x'.repeat(120)}`
    renderList({ sessions: [{ name: longName }] })

    const name = screen.getByTestId('session-name')
    expect(name.className).toContain('session-name')
    // CSS 面（默认态）：右侧渐隐遮罩、不换行（规则存在性，jsdom 不做布局）。
    expect(THEME_CSS).toMatch(/\.session-name\s*\{[^}]*mask-image:/s)
    expect(THEME_CSS).toMatch(/\.session-name\s*\{[^}]*white-space:\s*nowrap/s)
    expect(THEME_CSS).toMatch(
      /\.session-name\.scrollable\s*\{[^}]*overflow-x:\s*auto/s,
    )

    // 悬停：条目 mouseenter 切换 scrollable 类（横向滚动查看全名）。
    expect(name.className).not.toContain('scrollable')
    fireEvent.mouseEnter(screen.getByTestId('session-item'))
    expect(name.className).toContain('session-name scrollable')

    // 移出：复位类与 scrollLeft（scrollLeft 由 jsdom 直接驱动以验证复位路径）。
    name.scrollLeft = 60
    expect(name.scrollLeft).toBe(60)
    fireEvent.mouseLeave(screen.getByTestId('session-item'))
    expect(name.className).not.toContain('scrollable')
    expect(name.scrollLeft).toBe(0)
  })
})

describe('SessionList 选中态与切换（US4）', () => {
  it('选中项带 selected 样式，点击条目触发 onSelect 携带资源名', () => {
    const { onSelect } = renderList({ selected: S2 })

    const items = screen.getAllByTestId('session-item')
    expect(items[0]?.className).not.toContain('selected')
    expect(items[1]?.className).toContain('selected')

    fireEvent.click(items[0] as HTMLElement)
    expect(onSelect).toHaveBeenCalledTimes(1)
    expect(onSelect).toHaveBeenCalledWith(S1)
  })
})

// ─── Menu 卡片视觉断言（US8，specs/054-agent-v2-bugfixes/contracts/web-ui.md
// §1/§7，specs/054-agent-v2-bugfixes/revisions/phase10-theme-css-carrier.md §4；
// 组件行为零改动，视觉由 vendored token sheets 承载） ───

describe('SessionList Menu 卡片视觉（US8）', () => {
  it('vendored sheets 定义 Menu 卡片消费的三 token（union 面）', () => {
    expect(DSH_SHEETS).toMatch(/--dsw-specific-menu\s*:/)
    expect(DSH_SHEETS).toMatch(/--dsw-alias-border-inverted\s*:/)
    expect(DSH_SHEETS).toMatch(/--dsw-shadow-lv3\s*:/)
  })

  it('sheets 含 dark 激活选择器，index.html body 携带激活属性', () => {
    expect(DSH_SHEETS).toMatch(/body\[data-ds-dark-theme\]/)
    expect(INDEX_HTML).toMatch(/<body[^>]*\bdata-ds-dark-theme\b/)
  })

  it('theme.css 不再定义任何 --dsw-* 变量（sheets 为唯一权威），保留 --app-* 与深色基色', () => {
    expect(THEME_CSS).not.toMatch(/--dsw-[a-z0-9-]+\s*:/)
    expect(THEME_CSS).toMatch(/color-scheme:\s*dark/)
    expect(THEME_CSS).toMatch(/--app-bg\s*:/)
  })
})
