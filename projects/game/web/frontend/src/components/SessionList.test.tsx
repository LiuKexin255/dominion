// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Session } from '../api/sessions.js'
import { SessionList } from './SessionList.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

const S1 = 'templates/saolei/sessions/s1'
const S2 = 'templates/saolei/sessions/s2'

// Callback props 均以 vi.fn() 注入（style/javascript.md Mock 约定：DI seam，
// 零模块拦截），并对触发路径做正向断言。
function renderList(
  options: {
    sessions?: Session[]
    selected?: string | null
    loading?: boolean
    error?: string | null
  } = {},
) {
  const callbacks = {
    onSelect: vi.fn(),
    onRefresh: vi.fn(),
    onCreate: vi.fn(),
    onDelete: vi.fn(),
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
  it('Refresh 点击触发 onRefresh；loading 中按钮禁用', () => {
    const { onRefresh } = renderList({ selected: S1 })
    fireEvent.click(screen.getByTestId('refresh-sessions'))
    expect(onRefresh).toHaveBeenCalledTimes(1)

    cleanup()
    renderList({ selected: S1, loading: true })
    const refresh = screen.getByTestId('refresh-sessions') as HTMLButtonElement
    expect(refresh.disabled).toBe(true)
  })

  it('Create 点击触发 onCreate', () => {
    const { onCreate } = renderList()
    fireEvent.click(screen.getByTestId('create-session'))
    expect(onCreate).toHaveBeenCalledTimes(1)
  })

  it('Delete 作用于选中项：未选中时禁用，选中后点击携带其资源名', () => {
    const { onDelete } = renderList({ selected: null })
    const del = screen.getByTestId('delete-session') as HTMLButtonElement
    expect(del.disabled).toBe(true)
    fireEvent.click(del)
    expect(onDelete).not.toHaveBeenCalled()

    cleanup()
    const callbacks = renderList({ selected: S1 })
    const enabled = screen.getByTestId('delete-session') as HTMLButtonElement
    expect(enabled.disabled).toBe(false)
    fireEvent.click(enabled)
    expect(callbacks.onDelete).toHaveBeenCalledTimes(1)
    expect(callbacks.onDelete).toHaveBeenCalledWith(S1)
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
