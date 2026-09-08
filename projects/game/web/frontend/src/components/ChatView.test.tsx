// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { RenderResult } from '@testing-library/react'
import type { ChatEvent, ContentBlock, HistoryMessage } from '../api/conversation.js'
import type { BlockDraft, LiveTurn } from '../store/chat.js'
import { ChatStore } from '../store/chat.js'
import { ChatView } from './ChatView.js'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

const SESSION = 'templates/saolei/sessions/s1'
const noop = (): void => {}
const noopCancel = async (): Promise<void> => {}

// ChatView 集成测试：构造 store 层 BlockDraft / protojson ContentBlock 两种块
// 形态直接驱动渲染（不经过 fetch 流），断言 THINK/TEXT/TOOL_CALL 三分类保序
// 呈现与 ReasoningRow / ToolCard 状态语义（US2/US3 场景，
// specs/049-agent-v2-dsh-init/spec.md）。
function renderChatView(props: {
  history?: HistoryMessage[]
  live?: LiveTurn | null
  session?: string
  canceled?: boolean
  onCancel?: () => Promise<void>
  onSend?: (text: string) => void
}): RenderResult {
  return render(
    <ChatView
      session={props.session ?? SESSION}
      history={props.history ?? []}
      live={props.live ?? null}
      queue={[]}
      error={null}
      canceled={props.canceled ?? false}
      onSend={props.onSend ?? noop}
      onCancel={props.onCancel ?? noopCancel}
    />,
  )
}

function rerenderChatView(
  result: RenderResult,
  props: {
    history?: HistoryMessage[]
    live?: LiveTurn | null
    session?: string
    canceled?: boolean
    onSend?: (text: string) => void
  },
): void {
  result.rerender(
    <ChatView
      session={props.session ?? SESSION}
      history={props.history ?? []}
      live={props.live ?? null}
      queue={[]}
      error={null}
      canceled={props.canceled ?? false}
      onSend={props.onSend ?? noop}
      onCancel={noopCancel}
    />,
  )
}

// liveOf wraps one step's block drafts into a single-step live turn. The
// server step loop numbers steps from 1 (monotonic within a turn); 0 is only
// the missing-field sentinel for stepless events
// (specs/054-agent-v2-bugfixes/revisions/phase11-step-numbering.md §1).
function liveOf(blocks: BlockDraft[]): LiveTurn {
  return { turnId: 't1', steps: [{ step: 1, blocks, settled: false }] }
}

describe('ChatView THINK 渲染分支', () => {
  it('流式回合 THINK 折叠渐进呈现、running 摘要跟随最新行，与正文分类不混排', () => {
    const result = renderChatView({
      live: liveOf([{ index: 0, type: 'THINK', text: '先拆解问题\n再给出步骤' }]),
    })

    // 折叠态 + running：摘要 = 最新行，完整思考文本不可见。
    expect(screen.getByTestId('reasoning-row').getAttribute('data-state')).toBe('running')
    expect(screen.getByTestId('reasoning-summary').textContent).toBe('再给出步骤')
    expect(screen.queryByTestId('reasoning-body')).toBeNull()

    // 流式渐进：delta 追加后摘要跟随最新行。
    rerenderChatView(result, {
      live: liveOf([{ index: 0, type: 'THINK', text: '先拆解问题\n再给出步骤\n开始作答' }]),
    })
    expect(screen.getByTestId('reasoning-summary').textContent).toBe('开始作答')

    // 正文块与思考块分类呈现：正文独立渲染，思考保持折叠区域。
    rerenderChatView(result, {
      live: liveOf([
        { index: 0, type: 'THINK', text: '先拆解问题\n再给出步骤\n开始作答' },
        { index: 1, type: 'TEXT', text: '答案正文' },
      ]),
    })
    expect(screen.getByTestId('agent-text').textContent).toBe('答案正文')
    expect(screen.getByTestId('reasoning-row')).toBeTruthy()
    expect(screen.queryByTestId('reasoning-body')).toBeNull()
  })

  it('多块 live 回合：已终结 THINK 呈现完成态首行摘要，running 仅属流式尾块', () => {
    renderChatView({
      live: liveOf([
        { index: 0, type: 'THINK', text: '思考第一行\n思考第二行' },
        { index: 1, type: 'TEXT', text: '正在流式的正文' },
      ]),
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
      live: liveOf([{ index: 0, type: 'TEXT', text: '流式正文' }]),
    })
    expect(screen.getByTestId('agent-text').textContent).toBe('流式正文')
    expect(screen.queryByTestId('reasoning-row')).toBeNull()
  })

  it('流式 THINK 块尚无文本时不渲染空思考区域', () => {
    renderChatView({
      live: liveOf([{ index: 0, type: 'THINK', text: '' }]),
    })
    expect(screen.queryByTestId('reasoning-row')).toBeNull()
    expect(screen.queryByTestId('agent-text')).toBeNull()
  })
})

// blockKinds reads the rendered block-type sequence across every agent step
// container in message order (按 DOM 序断言保序渲染；多 step 分段/多历史消息
// 时聚合各容器子元素).
function blockKinds(): (string | null)[] {
  const agents = screen
    .getByTestId('chat-messages')
    .querySelectorAll('.msg-agent')
  if (agents.length === 0) throw new Error('agent message not rendered')
  return Array.from(agents).flatMap((agent) =>
    Array.from(agent.children).map((el) => el.getAttribute('data-testid')),
  )
}

describe('ChatView TOOL_CALL 渲染分支', () => {
  it('live 流式 TOOL_CALL 块以 RUNNING 态 ToolCard 呈现名称与参数', () => {
    renderChatView({
      live: liveOf([
        { index: 0, type: 'TOOL_CALL', toolId: 'call-a', name: 'bash', args: '{"command":"ls"', status: 'TOOL_STATUS_RUNNING' },
      ]),
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
    renderChatView({ live: liveOf(liveBlocks) })
    expect(blockKinds()).toEqual(['reasoning-row', 'tool-card', 'agent-text'])
    expect(screen.getByTestId('tool-card').getAttribute('data-tool-id')).toBe('call-a')
  })
})

describe('ChatView step 分段呈现（specs/054-agent-v2-bugfixes/contracts/web-ui.md §2.2）', () => {
  it('流式多 step 回合按分段依次独立呈现（每 step 一个容器，互不合并）', () => {
    renderChatView({
      live: {
        turnId: 't1',
        steps: [
          {
            step: 1,
            settled: true,
            blocks: [
              { index: 0, type: 'THINK', text: '第一步思考' },
              { index: 1, type: 'TOOL_CALL', toolId: 'call-a', name: 'saolei_init', args: '{}', status: 'TOOL_STATUS_SUCCEEDED', result: 'ok' },
            ],
          },
          {
            step: 2,
            settled: false,
            blocks: [
              { index: 2, type: 'THINK', text: '第二步思考' },
              { index: 3, type: 'TEXT', text: '棋盘已初始化' },
            ],
          },
        ],
      },
    })

    // 每 step 一个独立分段容器，依序呈现。
    const steps = screen.getAllByTestId('agent-step')
    expect(steps).toHaveLength(2)
    // 第一段：思考 + 工具卡片；第二段：思考 + 正文——分类分列在各自容器内。
    expect(Array.from(steps[0].children).map((el) => el.getAttribute('data-testid'))).toEqual([
      'reasoning-row',
      'tool-card',
    ])
    expect(Array.from(steps[1].children).map((el) => el.getAttribute('data-testid'))).toEqual([
      'reasoning-row',
      'agent-text',
    ])
    expect(screen.getByTestId('agent-text').textContent).toBe('棋盘已初始化')
  })

  it('流式 running 只属最后一段：前段 THINK 呈现完成态、尾段 THINK 跟随最新行', () => {
    renderChatView({
      live: {
        turnId: 't1',
        steps: [
          { step: 1, settled: true, blocks: [{ index: 0, type: 'THINK', text: '第一段第一行\n第一段第二行' }] },
          { step: 2, settled: false, blocks: [{ index: 1, type: 'THINK', text: '第二段第一行\n第二段最新行' }] },
        ],
      },
    })

    const rows = screen.getAllByTestId('reasoning-row')
    expect(rows[0]?.getAttribute('data-state')).toBe('ok')
    expect(screen.getAllByTestId('reasoning-summary')[0]?.textContent).toBe('第一段第一行')
    expect(rows[1]?.getAttribute('data-state')).toBe('running')
    expect(screen.getAllByTestId('reasoning-summary')[1]?.textContent).toBe('第二段最新行')
  })

  it('历史回填多 step 回合默认折叠，展开后每条 agent 消息即一个 step 分段', () => {
    renderChatView({
      history: [
        { role: 'ROLE_USER', blocks: [{ text: { content: '开始一局扫雷' } }] },
        {
          role: 'ROLE_AGENT',
          blocks: [
            { think: { content: '初始化' } },
            { toolCall: { toolId: 'call-a', name: 'saolei_init', argsJson: '{}', status: 'TOOL_STATUS_SUCCEEDED', result: 'ok' } },
          ],
        },
        { role: 'ROLE_AGENT', blocks: [{ text: { content: '已开局，雷数为 10' } }] },
      ],
    })

    // 用户消息保持独立气泡（US2 场景 6）；回填默认折叠态：过程不可见，
    // 仅最终答案分段可见。
    expect(screen.getByTestId('chat-messages').querySelector('.msg-user')?.textContent).toBe('开始一局扫雷')
    expect(screen.queryByTestId('turn-process')).toBeNull()
    expect(screen.queryByTestId('reasoning-row')).toBeNull()
    expect(screen.queryByTestId('tool-card')).toBeNull()
    expect(screen.getAllByTestId('agent-step')).toHaveLength(1)
    expect(screen.getByTestId('agent-text').textContent).toBe('已开局，雷数为 10')

    // 手动展开：每条 agent 消息即一个 step 分段，依次独立呈现。
    fireEvent.click(screen.getByTestId('turn-process-toggle'))
    const steps = screen.getAllByTestId('agent-step')
    expect(steps).toHaveLength(2)
    expect(Array.from(steps[0].children).map((el) => el.getAttribute('data-testid'))).toEqual([
      'reasoning-row',
      'tool-card',
    ])
    expect(Array.from(steps[1].children).map((el) => el.getAttribute('data-testid'))).toEqual([
      'agent-text',
    ])
  })

  it('步骤内空 text 块跳过、不产生空气泡（既有语义延续）', () => {
    renderChatView({
      live: {
        turnId: 't1',
        steps: [
          {
            step: 1,
            settled: false,
            blocks: [
              { index: 0, type: 'TEXT', text: '' },
              { index: 1, type: 'TEXT', text: '有内容的正文' },
            ],
          },
        ],
      },
    })
    expect(screen.getAllByTestId('agent-text')).toHaveLength(1)
    expect(screen.getByTestId('agent-text').textContent).toBe('有内容的正文')
  })
})

describe('ChatView 回合完成后的折叠（specs/054-agent-v2-bugfixes/contracts/web-ui.md §2.2）', () => {
  // 多 step 完成回合：step0 思考+工具、step1 中间正文、step2 最终答案。
  const COMPLETED_TURN: HistoryMessage[] = [
    {
      role: 'ROLE_AGENT',
      blocks: [
        { think: { content: '先初始化棋盘' } },
        { toolCall: { toolId: 'call-a', name: 'saolei_init', argsJson: '{}', status: 'TOOL_STATUS_SUCCEEDED', result: 'ok' } },
      ],
    },
    { role: 'ROLE_AGENT', blocks: [{ text: { content: '中间播报：开局完成' } }] },
    { role: 'ROLE_AGENT', blocks: [{ text: { content: '棋盘已就绪，请下令。' } }] },
  ]

  it('完成回合默认折叠：摘要含步骤/工具计数，过程不可见，最终答案独立可见', () => {
    renderChatView({ history: COMPLETED_TURN })

    const toggle = screen.getByTestId('turn-process-toggle')
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(toggle.textContent).toContain('2 步骤')
    expect(toggle.textContent).toContain('1 次工具调用')
    // 折叠态：过程证据（思考/工具/中间正文）不在 DOM。
    expect(screen.queryByTestId('turn-process')).toBeNull()
    expect(screen.queryByTestId('reasoning-row')).toBeNull()
    expect(screen.queryByTestId('tool-card')).toBeNull()
    expect(screen.getAllByTestId('agent-text')).toHaveLength(1)
    // 最终答案 = 最后一个含非空 text 且无 tool-call 的 step。
    expect(screen.getByTestId('agent-text').textContent).toBe('棋盘已就绪，请下令。')
  })

  it('点击展开呈现全部过程，再次点击收起', () => {
    renderChatView({ history: COMPLETED_TURN })

    fireEvent.click(screen.getByTestId('turn-process-toggle'))
    expect(screen.getByTestId('turn-process-toggle').getAttribute('aria-expanded')).toBe('true')
    // 展开区挂 turn-process 样式类（flex column + 间距，theme.css）。
    expect(screen.getByTestId('turn-process').className).toBe('turn-process')
    // 过程区依序呈现两个 step 分段：思考+工具、中间正文。
    const processSteps = screen.getByTestId('turn-process').querySelectorAll('.msg-agent')
    expect(processSteps).toHaveLength(2)
    expect(screen.getByTestId('reasoning-row')).not.toBeNull()
    expect(screen.getByTestId('tool-card')).not.toBeNull()
    expect(screen.getAllByTestId('agent-text').map((el) => el.textContent)).toEqual([
      '中间播报：开局完成',
      '棋盘已就绪，请下令。',
    ])

    fireEvent.click(screen.getByTestId('turn-process-toggle'))
    expect(screen.queryByTestId('turn-process')).toBeNull()
    expect(screen.queryByTestId('reasoning-row')).toBeNull()
    expect(screen.getByTestId('agent-text').textContent).toBe('棋盘已就绪，请下令。')
  })

  it('折叠开关 chevron 化：图标 svg 存在、data-open 随展开态切换（F5，对齐上游 TurnProcessNodeView，FR-009）', () => {
    // 上游形态：按钮 data-open={open || undefined} + IconChevronDownOutline14
    // （https://github.com/deepseek-ai/deepseek-harness/blob/master/
    // packages/client/ui-chat/src/client/chat/TurnProcessNodeView.tsx ）。
    renderChatView({ history: COMPLETED_TURN })
    const toggle = screen.getByTestId('turn-process-toggle')
    // 折叠态：chevron 图标存在、data-open 不存在；文字箭头不再使用。
    // 断言 class 落点而非裸 svg：className 由包 IconChevronDownOutline14
    // 透传，jsdom 可断言；CSS transform 旋转无法在 jsdom 断言，class 落点
    // 是可测的最近端点。
    expect(toggle.querySelector('svg.turn-process-chevron')).not.toBeNull()
    expect(toggle.getAttribute('data-open')).toBeNull()
    expect(toggle.textContent).not.toContain('▾')
    expect(toggle.textContent).not.toContain('▸')

    // 展开：data-open 出现（CSS 据此旋转 chevron -90deg → 0）；收起后消失。
    fireEvent.click(toggle)
    expect(toggle.getAttribute('data-open')).toBe('true')
    fireEvent.click(toggle)
    expect(toggle.getAttribute('data-open')).toBeNull()
  })

  it('无最终答案的回合（以纯工具调用结束）全可见不折叠，陈旧 RUNNING 工具块呈现中断终态', () => {
    renderChatView({
      history: [
        {
          role: 'ROLE_AGENT',
          blocks: [
            { think: { content: '查一下' } },
            { toolCall: { toolId: 'call-a', name: 'saolei_init', argsJson: '{}', status: 'TOOL_STATUS_SUCCEEDED', result: 'ok' } },
          ],
        },
        {
          role: 'ROLE_AGENT',
          blocks: [{ toolCall: { toolId: 'call-b', name: 'saolei_operate', argsJson: '{}', status: 'TOOL_STATUS_RUNNING' } }],
        },
      ],
    })

    expect(screen.queryByTestId('turn-process-toggle')).toBeNull()
    // 全部过程证据保持可见（官方规则：无最终答案的 closed Turn 保留全部
    // 过程内容）。
    expect(screen.getAllByTestId('agent-step')).toHaveLength(2)
    expect(screen.getByTestId('reasoning-row')).not.toBeNull()
    expect(screen.getAllByTestId('tool-card')).toHaveLength(2)
    // 回合已结束而结果不再会到达：历史中 RUNNING 且无 result 的陈旧工具块
    // 按中断终态呈现，而非"永久运行中"（specs/054-agent-v2-bugfixes/
    // data-model.md §2 回填侧推导；Edge Cases 裁定）。
    const stale = screen
      .getAllByTestId('tool-card')
      .find((card) => card.getAttribute('data-tool-id') === 'call-b')
    expect(stale?.getAttribute('data-status')).toBe('INTERRUPTED')
    expect(
      screen
        .getAllByTestId('tool-card-state')
        .map((el) => el.textContent),
    ).toContain('已中断')
    expect(stale?.querySelector('[data-state="warning"]')).not.toBeNull()
  })

  it('注入失败的回合回填后全部过程内容可见、失败前已产出的 RUNNING 工具块按中断终态呈现', () => {
    // US4 场景（specs/054-agent-v2-bugfixes/spec.md US4 场景 1）：流式呈现
    // 过的内容（思考、工具调用、部分正文）经回填再次可见，无最终答案不折叠；
    // 服务端工具异常路径固化历史中 RUNNING 无 result 的工具块按中断呈现。
    renderChatView({
      history: [
        { role: 'ROLE_USER', blocks: [{ text: { content: '开始一局扫雷' } }] },
        {
          role: 'ROLE_AGENT',
          blocks: [
            { think: { content: '先初始化棋盘' } },
            { toolCall: { toolId: 'call-a', name: 'saolei_init', argsJson: '{}', status: 'TOOL_STATUS_SUCCEEDED', result: 'ok' } },
          ],
        },
        {
          role: 'ROLE_AGENT',
          blocks: [
            {
              toolCall: { toolId: 'call-b', name: 'saolei_operate', argsJson: '{"type":"click"}', status: 'TOOL_STATUS_RUNNING' },
            },
            { text: { content: '正要点击第一格' } },
          ],
        },
      ],
    })

    // 无最终答案（尾步含工具块）：不折叠、全部过程可见。
    expect(screen.queryByTestId('turn-process-toggle')).toBeNull()
    expect(screen.getByTestId('chat-messages').querySelector('.msg-user')?.textContent).toBe('开始一局扫雷')
    expect(screen.getAllByTestId('agent-step')).toHaveLength(2)
    expect(screen.getByTestId('reasoning-row')).not.toBeNull()
    expect(screen.getAllByTestId('agent-text').map((el) => el.textContent)).toEqual(['正要点击第一格'])
    // 已得结果的工具保持完成态；未回结果的陈旧工具块为中断终态。
    const cards = screen.getAllByTestId('tool-card')
    expect(cards).toHaveLength(2)
    expect(cards[0]?.getAttribute('data-status')).toBe('SUCCEEDED')
    expect(cards[1]?.getAttribute('data-status')).toBe('INTERRUPTED')
    expect(screen.getAllByTestId('tool-card-state').map((el) => el.textContent)).toEqual([
      '已完成',
      '已中断',
    ])
  })

  it('live 流式活跃分段中的 RUNNING 工具块仍呈现执行中（中断推导仅限历史语境）', () => {
    renderChatView({
      live: {
        turnId: 't1',
        steps: [
          { step: 1, settled: false, blocks: [{ index: 0, type: 'TOOL_CALL', toolId: 'call-a', name: 'bash', args: '{}', status: 'TOOL_STATUS_RUNNING' }] },
        ],
      },
    })
    const card = screen.getByTestId('tool-card')
    expect(card.getAttribute('data-status')).toBe('RUNNING')
    expect(screen.getByTestId('tool-card-state').textContent).toBe('运行中')
    expect(card.querySelector('[data-state="ongoing"]')).not.toBeNull()
  })

  it('单 step 纯正文回合无折叠控件（无过程可收）', () => {
    renderChatView({
      history: [{ role: 'ROLE_AGENT', blocks: [{ text: { content: '纯正文回复' } }] }],
    })
    expect(screen.queryByTestId('turn-process-toggle')).toBeNull()
    expect(screen.getByTestId('agent-text').textContent).toBe('纯正文回复')
  })

  it('手动展开在页面会话内保持（rerender 追加新内容后仍展开）', () => {
    const result = renderChatView({ history: COMPLETED_TURN })
    fireEvent.click(screen.getByTestId('turn-process-toggle'))

    // 新一轮 user 消息 + agent 消息追加（历史只追加，旧回合组 index 不变）。
    rerenderChatView(result, {
      history: [
        ...COMPLETED_TURN,
        { role: 'ROLE_USER', blocks: [{ text: { content: '继续' } }] },
        { role: 'ROLE_AGENT', blocks: [{ text: { content: '新回合回复' } }] },
      ],
    })

    // 旧回合保持手动展开态；新回合（单 step）无折叠控件。
    expect(screen.getByTestId('turn-process-toggle').getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByTestId('turn-process')).not.toBeNull()
    const texts = screen.getAllByTestId('agent-text').map((el) => el.textContent)
    expect(texts).toContain('中间播报：开局完成')
    expect(texts).toContain('新回合回复')
  })

  it('最终答案之后的 step 一并直接呈现、不静默丢弃（防御性边界）', () => {
    // 最终答案（step1，正文）之后跟一个纯工具 step——正常驱动下不存在
    // （最终答案取最后一个匹配），出现时内容必须保留。
    renderChatView({
      history: [
        {
          role: 'ROLE_AGENT',
          blocks: [
            { think: { content: '先初始化' } },
            { toolCall: { toolId: 'call-a', name: 'saolei_init', argsJson: '{}', status: 'TOOL_STATUS_SUCCEEDED', result: 'ok' } },
          ],
        },
        { role: 'ROLE_AGENT', blocks: [{ text: { content: '最终答案' } }] },
        {
          role: 'ROLE_AGENT',
          blocks: [{ toolCall: { toolId: 'call-b', name: 'saolei_operate', argsJson: '{}', status: 'TOOL_STATUS_SUCCEEDED', result: 'done' } }],
        },
      ],
    })

    // 最终答案是 step1：过程区只含 step0（计数不含尾段），尾段 step2 直接
    // 呈现。
    expect(screen.getByTestId('turn-process-toggle').textContent).toContain('1 步骤')
    expect(screen.getByTestId('turn-process-toggle').textContent).toContain('1 次工具调用')
    expect(screen.getAllByTestId('agent-step')).toHaveLength(2)
    expect(screen.getByTestId('agent-text').textContent).toBe('最终答案')
    expect(screen.getByTestId('tool-card').getAttribute('data-tool-id')).toBe('call-b')
  })

  it('最终答案即首个 step 且回合多 step 时不渲染折叠控件、全部直接呈现', () => {
    // finalIndex === 0：过程区为空，不出现"思考过程（0 步骤 · 0 次工具
    // 调用）"的怪异摘要，全部 step 直接呈现不折叠。
    renderChatView({
      history: [
        { role: 'ROLE_AGENT', blocks: [{ text: { content: '首步即答案' } }] },
        {
          role: 'ROLE_AGENT',
          blocks: [{ toolCall: { toolId: 'call-a', name: 'saolei_operate', argsJson: '{}', status: 'TOOL_STATUS_SUCCEEDED', result: 'ok' } }],
        },
        { role: 'ROLE_AGENT', blocks: [{ think: { content: '事后思考' } }] },
      ],
    })

    expect(screen.queryByTestId('turn-process-toggle')).toBeNull()
    expect(screen.queryByTestId('turn-process')).toBeNull()
    expect(screen.getAllByTestId('agent-step')).toHaveLength(3)
    expect(screen.getByTestId('agent-text').textContent).toBe('首步即答案')
    expect(screen.getByTestId('tool-card')).not.toBeNull()
    expect(screen.getByTestId('reasoning-row')).not.toBeNull()
  })

  it('切换会话后展开状态重置，新会话回合回到默认折叠态', () => {
    const result = renderChatView({ history: COMPLETED_TURN, session: 'templates/saolei/sessions/a' })
    fireEvent.click(screen.getByTestId('turn-process-toggle'))
    expect(screen.getByTestId('turn-process-toggle').getAttribute('aria-expanded')).toBe('true')

    // 切换到另一会话（loadHistory 重建 history）：旧 index 键不得错误展开
    // 新会话的回合。
    rerenderChatView(result, {
      history: COMPLETED_TURN,
      session: 'templates/saolei/sessions/b',
    })

    expect(screen.getByTestId('turn-process-toggle').getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByTestId('turn-process')).toBeNull()
  })
})

describe('失败回合不折叠（specs/054-agent-v2-bugfixes/revisions/phase4-failed-turn-folding.md）', () => {
  // 部分正文尾步（含非空 text、无 tool-call）在内容形态上与最终答案无法区
  // 分：HistoryMessage.interrupted 是两路径共同的判定信号（data-model §1.5）
  // ——本地（store ERROR 投影尾步标记）与回填（服务端 List 透出）都必须不折
  // 叠（FR-005/US2 场景 5）。
  const FAILED_TURN_BACKFILL: HistoryMessage[] = [
    {
      role: 'ROLE_AGENT',
      blocks: [
        { think: { content: '先初始化棋盘' } },
        { toolCall: { toolId: 'call-a', name: 'saolei_init', argsJson: '{}', status: 'TOOL_STATUS_SUCCEEDED', result: 'ok' } },
      ],
    },
    { role: 'ROLE_AGENT', blocks: [{ text: { content: '正要播报开局' } }], interrupted: true },
  ]

  function expectFailedTurnUnfolded(): void {
    expect(screen.queryByTestId('turn-process-toggle')).toBeNull()
    expect(screen.getAllByTestId('agent-step')).toHaveLength(2)
    expect(screen.getByTestId('reasoning-row')).not.toBeNull()
    expect(screen.getAllByTestId('tool-card')).toHaveLength(1)
    expect(screen.getByTestId('agent-text').textContent).toBe('正要播报开局')
  }

  it('回填路径：interrupted 尾步（部分正文）排除出最终答案，回合全可见不折叠', () => {
    renderChatView({ history: FAILED_TURN_BACKFILL })
    expectFailedTurnUnfolded()
  })

  it('本地路径：store ERROR 投影的尾步标记与回填渲染形态一致（FR-013）', () => {
    const store = new ChatStore()
    const events: ChatEvent[] = [
      { turnId: 't1', turnStart: {} },
      { turnId: 't1', blockStart: { index: 0, type: 'BLOCK_TYPE_THINK', step: 1 } },
      { turnId: 't1', delta: { index: 0, text: '先初始化棋盘', step: 1 } },
      {
        turnId: 't1',
        blockEnd: { index: 0, block: { think: { content: '先初始化棋盘' } }, step: 1 },
      },
      {
        turnId: 't1',
        blockStart: { index: 1, type: 'BLOCK_TYPE_TOOL_CALL', toolId: 'call-a', name: 'saolei_init', step: 1 },
      },
      { turnId: 't1', delta: { index: 1, text: '{}', step: 1 } },
      {
        turnId: 't1',
        blockEnd: {
          index: 1,
          block: { toolCall: { toolId: 'call-a', name: 'saolei_init', argsJson: '{}', status: 'TOOL_STATUS_SUCCEEDED', result: 'ok' } },
          step: 1,
        },
      },
      { turnId: 't1', blockStart: { index: 2, type: 'BLOCK_TYPE_TEXT', step: 2 } },
      { turnId: 't1', delta: { index: 2, text: '正要播报开局', step: 2 } },
      {
        turnId: 't1',
        turnEnd: { status: 'TURN_STATUS_ERROR', error: { code: 'LLM_UPSTREAM', message: '流中断' } },
      },
    ]
    for (const e of events) {
      store.applyEvent(e)
    }
    const s = store.getSnapshot()
    expect(s.error).toBe('流中断')
    expect(s.live).toBeNull()

    // 本地投影渲染：与回填同构的 interrupted 尾步标记驱动同一折叠判定。
    render(
      <ChatView
        session={SESSION}
        history={s.history}
        live={s.live}
        queue={s.queue}
        error={s.error}
        canceled={false}
        onSend={noop}
        onCancel={noopCancel}
      />,
    )
    expectFailedTurnUnfolded()

    // 同构造以回填形态（store 投影产物即 List 消息形态）重渲染：形态一致。
    cleanup()
    renderChatView({ history: s.history })
    expectFailedTurnUnfolded()
  })
})

describe('ChatView markdown 渲染（specs/054-agent-v2-bugfixes/contracts/web-ui.md §3）', () => {
  it('GFM 元素渲染为格式化内容：标题/列表/粗体/行内代码/代码块/表格/链接，无原始符号裸露', () => {
    renderChatView({
      history: [
        {
          role: 'ROLE_AGENT',
          blocks: [
            {
              text: {
                content: [
                  '## 扫雷开局',
                  '',
                  '- 白棋在角落',
                  '- 黑棋在中路',
                  '',
                  '**注意**避开 `雷区` 标记',
                  '',
                  '[规则说明](https://example.com/rules)',
                  '',
                  '| 行 | 列 |',
                  '|---|---|',
                  '| 1 | 2 |',
                  '',
                  '```text',
                  '0 1 2',
                  '```',
                ].join('\n'),
              },
            },
          ],
        },
      ],
    })
    const text = screen.getByTestId('agent-text')
    expect(text.querySelector('h2')?.textContent).toBe('扫雷开局')
    expect(text.querySelectorAll('li')).toHaveLength(2)
    expect(text.querySelector('strong')?.textContent).toBe('注意')
    expect(text.querySelector('p code')?.textContent).toBe('雷区')
    expect(text.querySelector('a')?.getAttribute('href')).toBe('https://example.com/rules')
    expect(text.querySelector('a')?.getAttribute('rel')).toContain('noopener')
    expect(text.querySelector('table')?.querySelector('td')?.textContent).toBe('1')
    expect(text.querySelector('pre code')?.textContent).toContain('0 1 2')
    // 原始 markdown 符号不裸露（FR-008）。
    for (const raw of ['##', '**', '`', '|---|', '```']) {
      expect(text.textContent).not.toContain(raw)
    }
  })

  it('流式增量稳定：正文块持续追加渲染正确，已冻结首块跨 chunk 保持同一元素（FR-009）', () => {
    const result = renderChatView({
      live: liveOf([{ index: 0, type: 'TEXT', text: '# 开局' }]),
    })
    rerenderChatView(result, {
      live: liveOf([{ index: 0, type: 'TEXT', text: '# 开局\n\n第一段播报' }]),
    })
    rerenderChatView(result, {
      live: liveOf([{ index: 0, type: 'TEXT', text: '# 开局\n\n第一段播报\n\n第二段播报' }]),
    })
    // 增量到达第三块后首块进入冻结区：再次增量，首块元素保持同一节点
    // （MarkdownText 冻结块缓存跨 chunk reconcile、不重挂载——包 README
    // "Markdown rendering"，https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-primitives ）。
    const heading = screen.getByTestId('agent-text').querySelector('h1')
    expect(heading?.textContent).toBe('开局')
    rerenderChatView(result, {
      live: liveOf([
        { index: 0, type: 'TEXT', text: '# 开局\n\n第一段播报\n\n第二段播报\n\n第三段播报' },
      ]),
    })
    expect(screen.getByTestId('agent-text').querySelector('h1')).toBe(heading)
    expect(screen.getByTestId('agent-text').querySelectorAll('p')).toHaveLength(3)
  })

  it('不完整 markdown 片段（流式未闭合代码块）容错呈现不崩溃', () => {
    expect(() =>
      renderChatView({
        live: liveOf([{ index: 0, type: 'TEXT', text: '结果如下\n\n```js\nconsole.log("x' }]),
      }),
    ).not.toThrow()
    const text = screen.getByTestId('agent-text')
    expect(text.querySelector('code')?.textContent).toContain('console.log("x')
  })

  it('用户消息保持纯文本：markdown 符号原样保留、不渲染为格式化元素（web-ui.md §3）', () => {
    renderChatView({
      history: [
        {
          role: 'ROLE_USER',
          blocks: [{ text: { content: '**这不是粗体** 与 `这不是代码`' } }],
        },
      ],
    })
    const user = screen.getByTestId('chat-messages').querySelector('.msg-user')
    expect(user?.textContent).toBe('**这不是粗体** 与 `这不是代码`')
    expect(user?.querySelector('strong')).toBeNull()
    expect(user?.querySelector('code')).toBeNull()
  })
})

describe('ChatView 终止按钮（specs/054-agent-v2-bugfixes/contracts/web-ui.md §4）', () => {
  it('仅 live 回合运行中可见：空闲不呈现触发面（不可触发、无副作用）', () => {
    // 空闲：无终止按钮。
    const result = renderChatView({ history: [{ role: 'ROLE_USER', blocks: [{ text: { content: 'hi' } }] }] })
    expect(screen.queryByTestId('cancel-button')).toBeNull()

    // 运行中（live 非空）：终止按钮出现在 composer 区（发送按钮旁）。
    rerenderChatView(result, { live: liveOf([{ index: 0, type: 'TEXT', text: '流式中' }]) })
    expect(screen.getByTestId('cancel-button')).not.toBeNull()
    expect(screen.getByTestId('cancel-button').textContent).toBe('终止')
    expect(screen.getByTestId('send-button')).not.toBeNull()

    // 回合结束（live 归空）：触发面消失。
    rerenderChatView(result, { live: null })
    expect(screen.queryByTestId('cancel-button')).toBeNull()
  })

  it('点击编排：一次点击触发一次 onCancel，请求在途期间重复点击防抖', async () => {
    let release!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    const onCancel = vi.fn(() => gate)
    renderChatView({ live: liveOf([{ index: 0, type: 'TEXT', text: '失控回合' }]), onCancel })

    fireEvent.click(screen.getByTestId('cancel-button'))
    fireEvent.click(screen.getByTestId('cancel-button'))
    expect(onCancel).toHaveBeenCalledTimes(1)
    // 在途期间按钮禁用（防抖的呈现面）。
    expect((screen.getByTestId('cancel-button') as HTMLButtonElement).disabled).toBe(true)

    release()
    await waitFor(() =>
      expect((screen.getByTestId('cancel-button') as HTMLButtonElement).disabled).toBe(false),
    )
    // 请求落定后可再次触发（新一次终止请求）。
    fireEvent.click(screen.getByTestId('cancel-button'))
    expect(onCancel).toHaveBeenCalledTimes(2)
  })

  it('onCancel 请求失败不吞：编排层错误经 error prop 呈现（终态标识独立于错误文案）', async () => {
    // 请求失败呈现由 App.tsx ChatPanel 编排（cancelAgent catch → error），
    // 组件面断言：error 与 canceled 同屏时各自独立呈现——"已终止"非错误
    // 文案。
    renderChatView({
      live: liveOf([{ index: 0, type: 'TEXT', text: '部分产出' }]),
      canceled: true,
      onCancel: async () => {
        throw new Error('503 unavailable')
      },
    })
    fireEvent.click(screen.getByTestId('cancel-button'))
    // promise rejection 由编排层承载，组件不因 rejection 崩溃；等待防抖解除。
    await waitFor(() =>
      expect((screen.getByTestId('cancel-button') as HTMLButtonElement).disabled).toBe(false),
    )
  })

  it('CANCELED 终态呈现"已终止"标识：非错误文案、独立呈现', () => {
    renderChatView({ canceled: true })
    const marker = screen.getByTestId('turn-canceled')
    expect(marker.textContent).toBe('已终止')
    // 不复用错误呈现面（role=alert 的 chat-error）。
    expect(screen.queryByTestId('chat-error')).toBeNull()
    expect(marker.getAttribute('role')).toBeNull()
  })

  it('取消后排队 chip 移除、落地 user 消息以历史形态呈现（store 驱动）', async () => {
    // store 走真实归约：排队流 queued 帧（chip + 落地 user 消息）→
    // turn_end{CANCELED}（chip 移除），渲染面断言 web-ui.md §4 排队落地。
    const store = new ChatStore()
    async function* canceledQueuedStream(): AsyncGenerator<ChatEvent> {
      yield { queued: { position: 1 } }
      yield { turnId: 't2', turnEnd: { status: 'TURN_STATUS_CANCELED' } }
    }
    const renderWithStore = (): void => {
      const s = store.getSnapshot()
      cleanup()
      render(
        <ChatView
          session={SESSION}
          history={s.history}
          live={s.live}
          queue={s.queue}
          error={s.error}
          canceled={s.canceled}
          onSend={noop}
          onCancel={noopCancel}
        />,
      )
    }
    renderWithStore()
    expect(screen.queryByTestId('queue-chip')).toBeNull()

    await store.send('排队消息', canceledQueuedStream())
    renderWithStore()
    expect(screen.queryByTestId('queue-chip')).toBeNull()
    // 排队消息以历史 user 消息形态出现在对话流（落地）。
    expect(screen.getByTestId('chat-messages').querySelector('.msg-user')?.textContent).toBe('排队消息')
    expect(screen.getByTestId('turn-canceled')).not.toBeNull()
  })
})

// 条件跟随与回底入口（FR-002~004，specs/055-agent-v2-ui-fixes/contracts/
// ui-interactions.md §1、data-model.md §1 跟随状态机）。jsdom 无布局引擎：
// 以实例属性覆写 .chat-messages 的 scrollTop/scrollHeight/clientHeight 为
// 可赋值驱动（覆写遮蔽 Element.prototype 上的只读实现，程序化赋值即生效）。
interface ScrollMockState {
  scrollTop: number
  scrollHeight: number
  clientHeight: number
}

function mockScrollable(): {
  el: HTMLElement
  set: (next: Partial<ScrollMockState>) => void
} {
  const el = screen.getByTestId('chat-messages')
  const state: ScrollMockState = { scrollTop: 0, scrollHeight: 0, clientHeight: 0 }
  Object.defineProperty(el, 'scrollTop', {
    get: () => state.scrollTop,
    set: (value: number) => {
      state.scrollTop = value
    },
    configurable: true,
  })
  Object.defineProperty(el, 'scrollHeight', { get: () => state.scrollHeight, configurable: true })
  Object.defineProperty(el, 'clientHeight', { get: () => state.clientHeight, configurable: true })
  return { el, set: (next) => Object.assign(state, next) }
}

describe('ChatView 条件跟随滚动（specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md §1）', () => {
  it('贴底时内容增长保持贴底，贴底期间不渲染回底按钮（FR-002/FR-004）', () => {
    const result = renderChatView({ live: liveOf([{ index: 0, type: 'TEXT', text: '初始正文' }]) })
    const { el, set } = mockScrollable()
    set({ scrollTop: 600, scrollHeight: 1000, clientHeight: 400 })
    fireEvent.scroll(el)
    expect(screen.queryByTestId('to-bottom-button')).toBeNull()

    // 贴底 + 内容增长：视图写入新的 scrollHeight（跟随生效）。
    set({ scrollHeight: 1200 })
    rerenderChatView(result, {
      live: liveOf([{ index: 0, type: 'TEXT', text: '初始正文\n\n第一段增量' }]),
    })
    expect(el.scrollTop).toBe(1200)
    expect(screen.queryByTestId('to-bottom-button')).toBeNull()
  })

  it('思考流式（THINK 块）同样受条件跟随约束：贴底跟随、非贴底位置保持（US1 独立测试要求思考/正文两阶段分别断言）', () => {
    const result = renderChatView({ live: liveOf([{ index: 0, type: 'THINK', text: '先拆解问题' }]) })
    const { el, set } = mockScrollable()
    set({ scrollTop: 600, scrollHeight: 1000, clientHeight: 400 })
    fireEvent.scroll(el)
    expect(screen.getByTestId('reasoning-row').getAttribute('data-state')).toBe('running')

    // 贴底 + 思考流式增长：跟随贴底（折叠摘要行的增长也是内容增长）。
    set({ scrollHeight: 1200 })
    rerenderChatView(result, {
      live: liveOf([{ index: 0, type: 'THINK', text: '先拆解问题\n再给出步骤' }]),
    })
    expect(el.scrollTop).toBe(1200)

    // 用户上滚后再增长：阅读位置保持、不被拉回。
    set({ scrollTop: 500 })
    fireEvent.scroll(el)
    set({ scrollHeight: 1500 })
    rerenderChatView(result, {
      live: liveOf([{ index: 0, type: 'THINK', text: '先拆解问题\n再给出步骤\n开始作答' }]),
    })
    expect(el.scrollTop).toBe(500)
  })

  it('非贴底时位置保持不被拉回，回底按钮呈现（FR-002/FR-004）', () => {
    const result = renderChatView({ live: liveOf([{ index: 0, type: 'TEXT', text: '初始正文' }]) })
    const { el, set } = mockScrollable()
    set({ scrollTop: 600, scrollHeight: 1000, clientHeight: 400 })
    fireEvent.scroll(el)

    // 用户上滚离开底部：后续输出不改变阅读位置；按钮 aria-label 对齐
    // 契约 §1.4（specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md）。
    set({ scrollTop: 200 })
    fireEvent.scroll(el)
    expect(screen.getByTestId('to-bottom-button').getAttribute('aria-label')).toBe('回到底部')
    set({ scrollHeight: 1500 })
    rerenderChatView(result, {
      live: liveOf([{ index: 0, type: 'TEXT', text: '初始正文\n\n大量新增输出内容' }]),
    })
    expect(el.scrollTop).toBe(200)
    expect(screen.getByTestId('to-bottom-button')).not.toBeNull()
  })

  it('点击回底按钮：视角回底、按钮消失、跟随恢复（FR-004）', () => {
    const result = renderChatView({ live: liveOf([{ index: 0, type: 'TEXT', text: '初始正文' }]) })
    const { el, set } = mockScrollable()
    set({ scrollTop: 200, scrollHeight: 1000, clientHeight: 400 })
    fireEvent.scroll(el)

    fireEvent.click(screen.getByTestId('to-bottom-button'))
    expect(el.scrollTop).toBe(1000)
    expect(screen.queryByTestId('to-bottom-button')).toBeNull()

    // 回底后跟随恢复：内容增长重新贴底。
    set({ scrollHeight: 1300 })
    rerenderChatView(result, {
      live: liveOf([{ index: 0, type: 'TEXT', text: '初始正文\n\n回底后的增量' }]),
    })
    expect(el.scrollTop).toBe(1300)
  })

  it('非贴底时发新消息：无条件回底并恢复跟随（FR-003）', () => {
    const onSend = vi.fn()
    const result = renderChatView({
      history: [{ role: 'ROLE_USER', blocks: [{ text: { content: '第一条' } }] }],
      onSend,
    })
    const { el, set } = mockScrollable()
    set({ scrollTop: 100, scrollHeight: 1000, clientHeight: 400 })
    fireEvent.scroll(el)
    expect(screen.getByTestId('to-bottom-button')).not.toBeNull()

    fireEvent.change(screen.getByTestId('chat-input'), { target: { value: '新消息' } })
    fireEvent.click(screen.getByTestId('send-button'))
    expect(onSend).toHaveBeenCalledWith('新消息')

    // 父层将 user 消息落入 history 后重渲染：视角已在底部、按钮消失。
    rerenderChatView(result, {
      history: [
        { role: 'ROLE_USER', blocks: [{ text: { content: '第一条' } }] },
        { role: 'ROLE_USER', blocks: [{ text: { content: '新消息' } }] },
      ],
      onSend,
    })
    expect(el.scrollTop).toBe(1000)
    expect(screen.queryByTestId('to-bottom-button')).toBeNull()
    expect(screen.getByTestId('chat-messages').textContent).toContain('新消息')
  })

  it('贴底判定阈值为 24px：距底 ≤ 24 视为贴底、> 24 离底（R7，上游 FOLLOW_THRESHOLD）', () => {
    const r1 = renderChatView({ live: liveOf([{ index: 0, type: 'TEXT', text: '正文' }]) })
    const m1 = mockScrollable()
    m1.set({ scrollTop: 976, scrollHeight: 1400, clientHeight: 400 })
    fireEvent.scroll(m1.el)
    expect(screen.queryByTestId('to-bottom-button')).toBeNull()
    rerenderChatView(r1, { live: liveOf([{ index: 0, type: 'TEXT', text: '正文\n\n增量' }]) })
    expect(m1.el.scrollTop).toBe(1400)

    cleanup()
    renderChatView({ live: liveOf([{ index: 0, type: 'TEXT', text: '正文' }]) })
    const m2 = mockScrollable()
    m2.set({ scrollTop: 975, scrollHeight: 1400, clientHeight: 400 })
    fireEvent.scroll(m2.el)
    expect(screen.getByTestId('to-bottom-button')).not.toBeNull()
  })

  it('回合结束时用户已上滚：终态横幅出现不强行拉底（Edge Cases"回合结束时的视角"）', () => {
    const result = renderChatView({ live: liveOf([{ index: 0, type: 'TEXT', text: '流式正文' }]) })
    const { el, set } = mockScrollable()
    set({ scrollTop: 300, scrollHeight: 1000, clientHeight: 400 })
    fireEvent.scroll(el)

    // 回合以错误终态收束（live 归空、error 呈现）：阅读位置保持原位。
    result.rerender(
      <ChatView
        session={SESSION}
        history={[]}
        live={null}
        queue={[]}
        error="流中断"
        canceled={false}
        onSend={noop}
        onCancel={noopCancel}
      />,
    )
    expect(screen.getByTestId('chat-error').textContent).toBe('流中断')
    expect(el.scrollTop).toBe(300)
    // 回底按钮存在性仅由滚动位置决定，与回合状态无关（specs/
    // 055-agent-v2-ui-fixes/spec.md Edge Cases"回底入口的存在条件"）：
    // 回合已结束（live 归空、error 呈现）且非贴底时仍然呈现。
    expect(screen.getByTestId('to-bottom-button')).not.toBeNull()
  })
})
