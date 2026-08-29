// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { RenderResult } from '@testing-library/react'
import type { ContentBlock, HistoryMessage } from '../api/conversation.js'
import type { BlockDraft, LiveTurn } from '../store/chat.js'
import { ChatView } from './ChatView.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

const SESSION = 'templates/saolei/sessions/s1'
const noop = (): void => {}

// ChatView 集成测试：构造 store 层 BlockDraft / protojson ContentBlock 两种块
// 形态直接驱动渲染（不经过 fetch 流），断言 THINK/TEXT/TOOL_CALL 三分类保序
// 呈现与 ReasoningRow / ToolCard 状态语义（US2/US3 场景，
// specs/049-agent-v2-dsh-init/spec.md）。
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

// blockKinds reads the rendered block-type sequence of the single agent
// message in the message area (按 DOM 序断言保序渲染).
function blockKinds(): (string | null)[] {
  const agent = screen
    .getByTestId('chat-messages')
    .querySelector('.msg-agent')
  if (agent === null) throw new Error('agent message not rendered')
  return Array.from(agent.children).map((el) => el.getAttribute('data-testid'))
}

describe('ChatView TOOL_CALL 渲染分支', () => {
  it('live 流式 TOOL_CALL 块以 RUNNING 态 ToolCard 呈现名称与参数', () => {
    renderChatView({
      live: {
        turnId: 't1',
        blocks: [
          { index: 0, type: 'TOOL_CALL', toolId: 'call-a', name: 'bash', args: '{"command":"ls"', status: 'TOOL_STATUS_RUNNING' },
        ],
      },
    })
    const card = screen.getByTestId('tool-card')
    expect(card.getAttribute('data-tool-id')).toBe('call-a')
    expect(card.getAttribute('data-status')).toBe('RUNNING')
    expect(screen.getByTestId('tool-card-name').textContent).toBe('bash')
    expect(screen.getByTestId('tool-card-state').textContent).toBe('运行中')
    expect(card.querySelector('[data-state="ongoing"]')).not.toBeNull()
  })

  it('历史回填 tool_call 块按 tool_id 关联展示、结果同卡（SUCCEEDED）', () => {
    renderChatView({
      history: [
        {
          role: 'ROLE_AGENT',
          blocks: [
            {
              toolCall: {
                toolId: 'call-a',
                name: 'bash',
                argsJson: '{"command":"ls"}',
                status: 'TOOL_STATUS_SUCCEEDED',
                result: 'file-a.txt',
              },
            },
          ],
        },
      ],
    })
    const card = screen.getByTestId('tool-card')
    expect(card.getAttribute('data-tool-id')).toBe('call-a')
    expect(card.getAttribute('data-status')).toBe('SUCCEEDED')
    expect(screen.getByTestId('tool-card-state').textContent).toBe('已完成')
    expect(screen.getByTestId('tool-card-result')).not.toBeNull()
  })

  it('混合回合（正文+思考+多次工具调用）三类内容按序可区分呈现（US3 场景 3）', () => {
    const blocks: ContentBlock[] = [
      { think: { content: '需要先列出目录' } },
      { text: { content: '我来查看目录。' } },
      {
        toolCall: {
          toolId: 'call-a',
          name: 'bash',
          argsJson: '{"command":"ls"}',
          status: 'TOOL_STATUS_SUCCEEDED',
          result: 'file-a.txt',
        },
      },
      {
        toolCall: {
          toolId: 'call-b',
          name: 'bash',
          argsJson: '{"command":"pwd"}',
          status: 'TOOL_STATUS_FAILED',
          result: 'exit code 1',
        },
      },
      { text: { content: '目录里有 file-a.txt。' } },
    ]
    renderChatView({
      history: [
        { role: 'ROLE_USER', blocks: [{ text: { content: '看看当前目录' } }] },
        { role: 'ROLE_AGENT', blocks },
      ],
    })
    // 按发生顺序：思考 → 正文 → 工具 ×2 → 正文，三类形态可区分。
    expect(blockKinds()).toEqual([
      'reasoning-row',
      'agent-text',
      'tool-card',
      'tool-card',
      'agent-text',
    ])
    const cards = screen.getAllByTestId('tool-card')
    expect(cards[0]?.getAttribute('data-tool-id')).toBe('call-a')
    expect(cards[0]?.getAttribute('data-status')).toBe('SUCCEEDED')
    expect(cards[1]?.getAttribute('data-tool-id')).toBe('call-b')
    expect(cards[1]?.getAttribute('data-status')).toBe('FAILED')
    expect(screen.getAllByTestId('agent-text').map((el) => el.textContent)).toEqual([
      '我来查看目录。',
      '目录里有 file-a.txt。',
    ])
  })

  it('live 混合回合同样保序：思考 → 工具（RUNNING）→ 正文', () => {
    const liveBlocks: BlockDraft[] = [
      { index: 0, type: 'THINK', text: '先想一下' },
      { index: 1, type: 'TOOL_CALL', toolId: 'call-a', name: 'bash', args: '{"command":"ls"}', status: 'TOOL_STATUS_RUNNING' },
      { index: 2, type: 'TEXT', text: '正在执行' },
    ]
    renderChatView({ live: { turnId: 't1', blocks: liveBlocks } })
    expect(blockKinds()).toEqual(['reasoning-row', 'tool-card', 'agent-text'])
    expect(screen.getByTestId('tool-card').getAttribute('data-tool-id')).toBe('call-a')
  })
})
