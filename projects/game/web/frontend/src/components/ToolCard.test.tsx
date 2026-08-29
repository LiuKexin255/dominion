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
    // 参数区与结果区同卡关联，均默认折叠、展开可见内容。
    expect(screen.getByTestId('tool-card-args').querySelector('pre')).toBeNull()
    expect(screen.getByTestId('tool-card-result').textContent).toContain('结果')
    expect(screen.getByTestId('tool-card-result').querySelector('pre')).toBeNull()
    toggleBlock('tool-card-result')
    expect(
      screen.getByTestId('tool-card-result').querySelector('pre')?.textContent,
    ).toContain('"title": "Example Domain"')
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
    // 非 JSON 的错误文本落为字符串字面量关联展示。
    toggleBlock('tool-card-result')
    expect(
      screen.getByTestId('tool-card-result').querySelector('pre')?.textContent,
    ).toContain('command failed with exit code 1')
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
})
