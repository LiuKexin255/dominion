// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
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
  session?: string
}): RenderResult {
  return render(
    <ChatView
      session={props.session ?? SESSION}
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
  props: { history?: HistoryMessage[]; live?: LiveTurn | null; session?: string },
): void {
  result.rerender(
    <ChatView
      session={props.session ?? SESSION}
      history={props.history ?? []}
      live={props.live ?? null}
      queue={[]}
      error={null}
      onSend={noop}
    />,
  )
}

// liveOf wraps one step's block drafts into a single-step live turn（单 step
// 是旧用例的退化形态：无 step 事件的流全部归组 0）。
function liveOf(blocks: BlockDraft[]): LiveTurn {
  return { turnId: 't1', steps: [{ step: 0, blocks, settled: false }] }
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
            step: 0,
            settled: true,
            blocks: [
              { index: 0, type: 'THINK', text: '第一步思考' },
              { index: 1, type: 'TOOL_CALL', toolId: 'call-a', name: 'saolei_init', args: '{}', status: 'TOOL_STATUS_SUCCEEDED', result: 'ok' },
            ],
          },
          {
            step: 1,
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
          { step: 0, settled: true, blocks: [{ index: 0, type: 'THINK', text: '第一段第一行\n第一段第二行' }] },
          { step: 1, settled: false, blocks: [{ index: 1, type: 'THINK', text: '第二段第一行\n第二段最新行' }] },
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
            step: 0,
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

  it('无最终答案的回合（以纯工具调用结束）全可见不折叠', () => {
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
