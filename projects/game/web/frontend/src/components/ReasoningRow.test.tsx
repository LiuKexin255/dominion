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

  it('展开体以 markdown 渲染：粗体/行内代码/列表为格式化元素，无原始符号裸露（web-ui.md §3）', () => {
    render(
      <ReasoningRow
        text={'判断棋盘状态：**优先角落**，用 `saolei_operate` 工具\n- 先看坐标\n- 再点击'}
        running={false}
      />,
    )
    fireEvent.click(disclosureRow())
    const body = screen.getByTestId('reasoning-body')
    expect(body.querySelector('strong')?.textContent).toBe('优先角落')
    expect(body.querySelector('code')?.textContent).toBe('saolei_operate')
    expect(body.querySelectorAll('li')).toHaveLength(2)
    // 原始 markdown 符号不残留（FR-008/FR-010 同能力）。
    expect(body.textContent).not.toContain('**')
    expect(body.textContent).not.toContain('`')
  })

  it('running 态摘要跟随最新行并标记跟随态', () => {
    render(<ReasoningRow text={'已完成的一行\n仍在生成的一行'} running />)
    expect(screen.getByTestId('reasoning-row').getAttribute('data-state')).toBe('running')
    expect(screen.getByTestId('reasoning-summary').textContent).toBe('仍在生成的一行')
    expect(screen.getByTestId('reasoning-summary').getAttribute('data-follow-end')).not.toBeNull()
  })

  it('running 态展开体流式渲染（streaming 透传）：不完整 markdown 片段不崩溃', () => {
    render(<ReasoningRow text={'分析中\n\n```js\n未闭合的代码块'} running />)
    fireEvent.click(disclosureRow())
    const body = screen.getByTestId('reasoning-body')
    // 未闭合 fence 按纯文本代码块容错呈现，文本不丢、渲染不抛错。
    expect(body.textContent).toContain('未闭合的代码块')
    expect(body.querySelector('code')?.textContent).toContain('未闭合的代码块')
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
