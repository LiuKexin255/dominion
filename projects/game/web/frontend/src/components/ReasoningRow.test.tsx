// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
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

// running 态 DisclosureRow 以 TextShimmer 装饰层重渲染折叠内容（aria-hidden +
// inert，装饰副本在 jsdom 中与真实副本同为 DOM 节点），故真实摘要须排除装饰层
// 定位（specs/073-dsh-0.2-upgrade/contracts/shimmer-adoption.md §2）。
function liveSummary(): HTMLElement {
  const live = screen
    .getAllByTestId('reasoning-summary')
    .find((el) => el.closest('[inert]') === null)
  if (live === undefined) throw new Error('live reasoning-summary not rendered')
  return live
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
    // 思考体走 compact 变体（次级排版：13px/tertiary/紧凑段距），对齐上游
    // ReasoningContent 的 variant="compact"。
    expect(body.querySelector('[data-markdown-variant="compact"]')).not.toBeNull()
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
    expect(liveSummary().textContent).toBe('仍在生成的一行')
    expect(liveSummary().getAttribute('data-follow-end')).not.toBeNull()
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

// 上游对齐结构（specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md §2、
// data-model.md §2）：根节点 data-expanded 围栏钩子 + follow-end 纯 CSS 双层
// 摘要，无任何程序化滚动。
describe('ReasoningRow 对齐结构', () => {
  it('根节点承载 data-expanded：折叠时不存在、展开时存在（F1 围栏钩子）', () => {
    render(<ReasoningRow text={'思考首行摘要\n后续思考行'} running={false} />)
    const root = screen.getByTestId('reasoning-row')
    expect(root.getAttribute('data-expanded')).toBeNull()

    fireEvent.click(disclosureRow())
    expect(root.getAttribute('data-expanded')).toBe('true')

    fireEvent.click(disclosureRow())
    expect(root.getAttribute('data-expanded')).toBeNull()
  })

  it('折叠摘要是双层结构（summary > summaryText）：running 与完成态同构', () => {
    render(<ReasoningRow text={'已完成的一行\n仍在生成的一行'} running />)
    const summary = liveSummary()
    expect(summary.getAttribute('data-follow-end')).not.toBeNull()
    const summaryText = summary.querySelector('.reasoning-summary-text')
    expect(summaryText).not.toBeNull()
    expect(summaryText?.textContent).toBe('仍在生成的一行')

    cleanup()
    render(<ReasoningRow text={'思考首行摘要\n后续思考行'} running={false} />)
    const settled = screen.getByTestId('reasoning-summary')
    expect(settled.getAttribute('data-follow-end')).toBeNull()
    expect(settled.querySelector('.reasoning-summary-text')?.textContent).toBe('思考首行摘要')
  })

  it('流式增长不产生程序化 scrollLeft 写入（follow-end 为纯 CSS 机制，F2）', async () => {
    const result = render(<ReasoningRow text={'第一行'} running />)
    const summary = liveSummary()
    // 以 setter 探针拦截 scrollLeft 写入：旧机制（rAF 节流 3 帧后写
    // scrollLeft）若在，跨帧后必有调用。
    const scrollLeftWrites = vi.fn()
    Object.defineProperty(summary, 'scrollLeft', {
      get: () => 0,
      set: (value: number) => scrollLeftWrites(value),
      configurable: true,
    })

    result.rerender(<ReasoningRow text={'第一行\n第二行\n第三行\n第四行'} running />)
    for (let i = 0; i < 6; i += 1) {
      await new Promise((resolve) => requestAnimationFrame(resolve))
    }
    expect(scrollLeftWrites).not.toHaveBeenCalled()
    expect(summary.scrollLeft).toBe(0)
  })
})

// shimmer 采用（specs/073-dsh-0.2-upgrade/contracts/shimmer-adoption.md §4）：
// 折叠摘要经 TextShimmer 表达进行中共享高光、DisclosureRow running 驱动头部
// 动画；jsdom 无动画帧，断言 DOM 结构与属性语义，不触发动画。
describe('ReasoningRow shimmer 采用', () => {
  // decoration 是 TextShimmer active 时的装饰层（inert/clipped），其真实文字
  // 以 data-shimmer-text 属性承载、经 CSS ::after 呈现——用它证明摘要确实参与
  // 共享高光（真实无障碍内容仍是 .reasoning-summary-text 内的文字节点）。
  function shimmerDecoration(): { root: Element; decoration: Element } | null {
    const root = disclosureRow().querySelector('[data-shimmer]')
    if (root === null) return null
    const decoration = root.querySelector('[inert]')
    if (decoration === null) return null
    return { root, decoration }
  }

  function decorativeTexts(decoration: Element): string[] {
    return Array.from(decoration.querySelectorAll('[data-shimmer-text]')).map(
      (el) => el.getAttribute('data-shimmer-text') ?? '',
    )
  }

  it('running 态折叠摘要以 TextShimmer active 呈现，且「正在生成」无障碍语义在场', () => {
    render(<ReasoningRow text={'已完成的一行\n仍在生成的一行'} running />)
    const summaryText = liveSummary().querySelector('.reasoning-summary-text')
    expect(summaryText).not.toBeNull()
    // TextShimmer 包裹摘要：真实文字由一层 span 承载且内容完整可读。
    expect(summaryText?.querySelector('span')?.textContent).toBe('仍在生成的一行')

    // active 语义：动画根 data-shimmer 在场，装饰层 aria-hidden + inert
    // （装饰性、被裁剪，不改变摘要行布局）。
    const shimmer = shimmerDecoration()
    if (shimmer === null) throw new Error('shimmer decoration not rendered')
    expect(shimmer.root.getAttribute('data-shimmer')).toBe('true')
    expect(shimmer.decoration.getAttribute('aria-hidden')).toBe('true')
    // 摘要文字进入共享高光装饰副本（同一文本经 data-shimmer-text 呈现）。
    expect(decorativeTexts(shimmer.decoration)).toContain('仍在生成的一行')

    // 进行中语义不得仅由动画承载。
    expect(
      screen.getByTestId('reasoning-row').querySelector('.visually-hidden')?.textContent,
    ).toBe('正在生成')
  })

  it('ok 态文字完整呈现且非 active，状态切换不重挂载摘要文本节点', () => {
    const { rerender } = render(
      <ReasoningRow text={'已完成的一行\n仍在生成的一行'} running />,
    )
    const runningTextNode = liveSummary().querySelector(
      '.reasoning-summary-text > span',
    )
    expect(runningTextNode?.textContent).toBe('仍在生成的一行')

    rerender(<ReasoningRow text={'已完成的一行\n仍在生成的一行'} running={false} />)

    // 动画停止：无 active 根、无装饰层。
    expect(disclosureRow().querySelector('[data-shimmer]')).toBeNull()
    expect(disclosureRow().querySelector('[inert]')).toBeNull()
    // 完结态完整呈现首行内容。
    const settledTextNode = liveSummary().querySelector(
      '.reasoning-summary-text > span',
    )
    expect(settledTextNode?.textContent).toBe('已完成的一行')
    // TextShimmer 契约「inactive text keeps the same node」：动画开关不重挂载
    // 真实文字节点。
    expect(settledTextNode).toBe(runningTextNode)
    // 完结态无进行中无障碍语义。
    expect(screen.getByTestId('reasoning-row').querySelector('.visually-hidden')).toBeNull()
  })
})
