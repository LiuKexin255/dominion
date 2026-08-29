// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { ReasoningRow } from './ReasoningRow.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

// disclosureRow queries the DisclosureRow header element (aria-expanded 载体).
function disclosureRow(): HTMLElement {
  const el = screen
    .getByTestId('reasoning-row')
    .querySelector('[data-disclosure-row]')
  if (el === null) throw new Error('disclosure row not rendered')
  return el as HTMLElement
}

describe('ReasoningRow', () => {
  it('默认折叠，完成态摘要显示首行且全文不可见', () => {
    render(<ReasoningRow text={'思考首行摘要\n后续思考行'} running={false} />)
    expect(screen.getByTestId('reasoning-row').getAttribute('data-state')).toBe('ok')
    expect(disclosureRow().getAttribute('aria-expanded')).toBe('false')
    expect(screen.getByTestId('reasoning-summary').textContent).toBe('思考首行摘要')
    expect(screen.queryByTestId('reasoning-body')).toBeNull()
    // 完成态无跟随标记（running 专属）。
    expect(screen.getByTestId('reasoning-summary').getAttribute('data-follow-end')).toBeNull()
  })

  it('点击行切换展开/折叠，展开渲染完整思考文本', () => {
    render(<ReasoningRow text={'思考首行摘要\n后续思考行'} running={false} />)
    fireEvent.click(disclosureRow())
    expect(disclosureRow().getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByTestId('reasoning-body').textContent).toBe('思考首行摘要\n后续思考行')
    // 展开态不再渲染折叠摘要行（DisclosureRow 契约）。
    expect(screen.queryByTestId('reasoning-summary')).toBeNull()
    fireEvent.click(disclosureRow())
    expect(disclosureRow().getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByTestId('reasoning-body')).toBeNull()
  })

  it('running 态摘要跟随最新行并标记跟随态', () => {
    render(<ReasoningRow text={'已完成的一行\n仍在生成的一行'} running />)
    expect(screen.getByTestId('reasoning-row').getAttribute('data-state')).toBe('running')
    expect(screen.getByTestId('reasoning-summary').textContent).toBe('仍在生成的一行')
    expect(screen.getByTestId('reasoning-summary').getAttribute('data-follow-end')).not.toBeNull()
  })

  it('无思考文本不渲染组件', () => {
    const { container } = render(<ReasoningRow text="" running />)
    expect(container.querySelector('[data-testid="reasoning-row"]')).toBeNull()
  })

  it('仅空白的思考文本等同无文本、不渲染组件', () => {
    const { container } = render(<ReasoningRow text={'  \n '} running={false} />)
    expect(container.querySelector('[data-testid="reasoning-row"]')).toBeNull()
    expect(container.textContent).toBe('')
  })
})
