// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { RenderResult } from '@testing-library/react'
import type { HistoryMessage } from '../api/conversation.js'
import type { LiveTurn } from '../store/chat.js'
import { ChatView } from './ChatView.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

const SESSION = 'templates/saolei/sessions/s1'
const noop = (): void => {}

// ChatView 集成测试：构造 store 层 BlockDraft / protojson ContentBlock 两种块
// 形态直接驱动渲染（不经过 fetch 流），断言 THINK/TEXT 分类呈现与
// ReasoningRow 状态语义（US2 场景，specs/049-agent-v2-dsh-init/spec.md）。
function renderChatView(props: {
  history?: HistoryMessage[]
  live?: LiveTurn | null
}): RenderResult {
  return render(
    <ChatView
      session={SESSION}
      history={props.history ?? []}
      live={props.live ?? null}
      queue={[]}
      error={null}
      onSend={noop}
    />,
  )
}

function rerenderChatView(
  result: RenderResult,
  props: { history?: HistoryMessage[]; live?: LiveTurn | null },
): void {
  result.rerender(
    <ChatView
      session={SESSION}
      history={props.history ?? []}
      live={props.live ?? null}
      queue={[]}
      error={null}
      onSend={noop}
    />,
  )
}

describe('ChatView THINK 渲染分支', () => {
  it('流式回合 THINK 折叠渐进呈现、running 摘要跟随最新行，与正文分类不混排', () => {
    const result = renderChatView({
      live: {
        turnId: 't1',
        blocks: [{ index: 0, type: 'THINK', text: '先拆解问题\n再给出步骤' }],
      },
    })

    // 折叠态 + running：摘要 = 最新行，完整思考文本不可见。
    expect(screen.getByTestId('reasoning-row').getAttribute('data-state')).toBe('running')
    expect(screen.getByTestId('reasoning-summary').textContent).toBe('再给出步骤')
    expect(screen.queryByTestId('reasoning-body')).toBeNull()

    // 流式渐进：delta 追加后摘要跟随最新行。
    rerenderChatView(result, {
      live: {
        turnId: 't1',
        blocks: [{ index: 0, type: 'THINK', text: '先拆解问题\n再给出步骤\n开始作答' }],
      },
    })
    expect(screen.getByTestId('reasoning-summary').textContent).toBe('开始作答')

    // 正文块与思考块分类呈现：正文独立渲染，思考保持折叠区域。
    rerenderChatView(result, {
      live: {
        turnId: 't1',
        blocks: [
          { index: 0, type: 'THINK', text: '先拆解问题\n再给出步骤\n开始作答' },
          { index: 1, type: 'TEXT', text: '答案正文' },
        ],
      },
    })
    expect(screen.getByTestId('agent-text').textContent).toBe('答案正文')
    expect(screen.getByTestId('reasoning-row')).toBeTruthy()
    expect(screen.queryByTestId('reasoning-body')).toBeNull()
  })

  it('多块 live 回合：已终结 THINK 呈现完成态首行摘要，running 仅属流式尾块', () => {
    renderChatView({
      live: {
        turnId: 't1',
        blocks: [
          { index: 0, type: 'THINK', text: '思考第一行\n思考第二行' },
          { index: 1, type: 'TEXT', text: '正在流式的正文' },
        ],
      },
    })
    // THINK 块已终结（其后 TEXT 在流式）→ ok + 首行摘要，而非 running 最新行。
    expect(screen.getByTestId('reasoning-row').getAttribute('data-state')).toBe('ok')
    expect(screen.getByTestId('reasoning-summary').textContent).toBe('思考第一行')
    expect(screen.getByTestId('reasoning-summary').getAttribute('data-follow-end')).toBeNull()
    expect(screen.getByTestId('agent-text').textContent).toBe('正在流式的正文')
  })

  it('完成回合（历史回填）THINK 呈现完成态、摘要为首行，正文保持独立', () => {
    renderChatView({
      history: [
        {
          role: 'ROLE_AGENT',
          blocks: [
            { think: { content: '思考第一行\n思考第二行' } },
            { text: { content: '答案正文' } },
          ],
        },
      ],
    })
    // 历史回填即完成态（running=false）。
    expect(screen.getByTestId('reasoning-row').getAttribute('data-state')).toBe('ok')
    expect(screen.getByTestId('reasoning-summary').textContent).toBe('思考第一行')
    expect(screen.getByTestId('reasoning-summary').getAttribute('data-follow-end')).toBeNull()
    expect(screen.queryByTestId('reasoning-body')).toBeNull()
    expect(screen.getByTestId('agent-text').textContent).toBe('答案正文')
  })

  it('纯 text 回合零 THINK 区域（US2 场景 2）', () => {
    renderChatView({
      history: [{ role: 'ROLE_AGENT', blocks: [{ text: { content: '纯正文回复' } }] }],
    })
    expect(screen.getByTestId('agent-text').textContent).toBe('纯正文回复')
    expect(screen.queryByTestId('reasoning-row')).toBeNull()
  })

  it('流式纯 text 回合同样零 THINK 区域', () => {
    renderChatView({
      live: { turnId: 't1', blocks: [{ index: 0, type: 'TEXT', text: '流式正文' }] },
    })
    expect(screen.getByTestId('agent-text').textContent).toBe('流式正文')
    expect(screen.queryByTestId('reasoning-row')).toBeNull()
  })

  it('流式 THINK 块尚无文本时不渲染空思考区域', () => {
    renderChatView({
      live: { turnId: 't1', blocks: [{ index: 0, type: 'THINK', text: '' }] },
    })
    expect(screen.queryByTestId('reasoning-row')).toBeNull()
    expect(screen.queryByTestId('agent-text')).toBeNull()
  })
})
