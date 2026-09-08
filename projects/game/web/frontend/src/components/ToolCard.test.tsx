// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { ToolCard } from './ToolCard.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

// toggleBlock expands the JsonBlock inside a wrapper (label 可见 + 展开断言).
function toggleBlock(testId: string): void {
  const wrapper = screen.getByTestId(testId)
  const button = wrapper.querySelector('button')
  if (button === null) throw new Error(`json block toggle not found in ${testId}`)
  fireEvent.click(button)
}

describe('ToolCard', () => {
  it('RUNNING 态：名称/状态呈现，参数默认折叠、展开可见 JSON（US3 场景 1）', () => {
    render(
      <ToolCard
        toolId="call-1"
        name="bash"
        argsJson='{"command":"ls -la"}'
        status="RUNNING"
      />,
    )
    expect(screen.getByTestId('tool-card').getAttribute('data-tool-id')).toBe('call-1')
    expect(screen.getByTestId('tool-card-name').textContent).toBe('bash')
    expect(screen.getByTestId('tool-card-state').textContent).toBe('运行中')
    // StateDot ongoing（蓝色运行环，svg data-state）。
    expect(
      screen.getByTestId('tool-card').querySelector('[data-state="ongoing"]'),
    ).not.toBeNull()
    // 参数默认折叠：label 可见、JSON body 不可见。
    expect(screen.getByTestId('tool-card-args').textContent).toContain('参数')
    expect(screen.getByTestId('tool-card-args').querySelector('pre')).toBeNull()
    toggleBlock('tool-card-args')
    expect(
      screen.getByTestId('tool-card-args').querySelector('pre')?.textContent,
    ).toContain('"command": "ls -la"')
    // RUNNING 无结果区。
    expect(screen.queryByTestId('tool-card-result')).toBeNull()
  })

  it('SUCCEEDED 态：结果与调用关联展示于同一卡片（US3 场景 2）', () => {
    render(
      <ToolCard
        toolId="call-2"
        name="browser"
        argsJson='{"url":"https://example.com"}'
        status="SUCCEEDED"
        result='{"title":"Example Domain"}'
      />,
    )
    expect(
      screen.getByTestId('tool-card').getAttribute('data-tool-id'),
    ).toBe('call-2')
    expect(screen.getByTestId('tool-card').getAttribute('data-status')).toBe('SUCCEEDED')
    expect(screen.getByTestId('tool-card-state').textContent).toBe('已完成')
    expect(
      screen.getByTestId('tool-card').querySelector('[data-state="done"]'),
    ).not.toBeNull()
    // 参数区默认折叠、展开可见内容；结果区预格式化等宽直接呈现（web-ui.md
    // §3）：原文原样（非 JSON.stringify 字面量形态）、无需展开。
    expect(screen.getByTestId('tool-card-args').querySelector('pre')).toBeNull()
    toggleBlock('tool-card-args')
    expect(
      screen.getByTestId('tool-card-args').querySelector('pre')?.textContent,
    ).toContain('"url": "https://example.com"')
    const resultPre = screen.getByTestId('tool-card-result-pre')
    expect(resultPre.textContent).toBe('{"title":"Example Domain"}')
    expect(resultPre.querySelector('button')).toBeNull()
  })

  it('FAILED 态：失败状态标识与错误结果关联（US3 场景 2 失败分支）', () => {
    render(
      <ToolCard
        toolId="call-3"
        name="bash"
        argsJson='{"command":"exit 1"}'
        status="FAILED"
        result="command failed with exit code 1"
      />,
    )
    expect(
      screen.getByTestId('tool-card').getAttribute('data-tool-id'),
    ).toBe('call-3')
    expect(screen.getByTestId('tool-card').getAttribute('data-status')).toBe('FAILED')
    expect(screen.getByTestId('tool-card-state').textContent).toBe('失败')
    expect(
      screen.getByTestId('tool-card').querySelector('[data-state="error"]'),
    ).not.toBeNull()
    // 非 JSON 的错误文本以原文预格式化呈现（无引号/转义的字符串字面量形态）。
    expect(screen.getByTestId('tool-card-result-pre').textContent).toBe(
      'command failed with exit code 1',
    )
  })

  it('不完整 JSON 参数（流式中途）按字符串字面量容错展示', () => {
    render(
      <ToolCard toolId="call-4" name="bash" argsJson='{"command":"ls' status="RUNNING" />,
    )
    toggleBlock('tool-card-args')
    // 字符串字面量经 JsonBlock 的 JSON.stringify 展示：引号转义形态。
    expect(
      screen.getByTestId('tool-card-args').querySelector('pre')?.textContent,
    ).toContain('{\\"command\\":\\"ls')
  })

  it('多行棋盘结果等宽预格式化：换行与空格逐字符保留、行列不错位（web-ui.md §3）', () => {
    // 文本棋盘含坐标标尺：每行空格数决定列对齐，任一空白丢失即错位。
    const board = [
      '  0 1 2 3',
      '0 · 1 · ?',
      '1 1 1 · ?',
      '2 0 0 1 ?',
    ].join('\n')
    render(
      <ToolCard
        toolId="call-5"
        name="saolei_view"
        argsJson='{}'
        status="SUCCEEDED"
        result={board}
      />,
    )
    const pre = screen.getByTestId('tool-card-result-pre')
    // 换行与行首空格逐字符保留（white-space: pre 语义；等宽下同列字符
    // 上下对齐，行列不错位）。
    expect(pre.textContent).toBe(board)
    const lines = pre.textContent?.split('\n') ?? []
    expect(lines).toHaveLength(4)
    expect(lines.every((line) => line.length === lines[0]?.length)).toBe(true)
    // pre 元素 UA 样式（jsdom 内建 UA stylesheet）：空白保持 + 等宽族。
    const style = window.getComputedStyle(pre)
    expect(style.whiteSpace).toBe('pre')
    expect(style.fontFamily).toContain('monospace')
    // 结果不 markdown 化、不经 JSON 处理：无 markdown/JSON 容器痕迹。
    expect(pre.closest('.md-code-block')).toBeNull()
    expect(pre.querySelector('code')).toBeNull()
  })
})
