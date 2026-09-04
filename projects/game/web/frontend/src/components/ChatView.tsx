// 对话主区：历史回填 + 实时流合并渲染 + 排队指示 + 发送输入
// （行为基线 desktop ChatView，契约 specs/049-agent-v2-dsh-init/contracts/
// web-frontend.md §3.2）。agent 输出按模型输出步骤分段呈现（specs/
// 054-agent-v2-bugfixes/contracts/web-ui.md §2.2）：历史一条消息即一个 step、
// live 回合每个 step 一个分段容器，依次独立呈现；步骤内 THINK →
// ReasoningRow、TEXT → MarkdownText、TOOL_CALL → ToolCard 分类分列不混排。
import { useEffect, useRef, useState } from 'react'
import { Button, Input, MarkdownText } from '@deepseek-ai/dsh-client-ui-primitives'
import type { ContentBlock, HistoryMessage } from '../api/conversation.js'
import type { BlockDraft, LiveTurn, QueuedMsg } from '../store/chat.js'
import { ReasoningRow } from './ReasoningRow.js'
import { ToolCard, type ToolCardStatus } from './ToolCard.js'

export interface ChatViewProps {
  session: string
  history: HistoryMessage[]
  live: LiveTurn | null
  queue: QueuedMsg[]
  error: string | null
  // 最近回合是否以"已终止"终态收束（store 归约 turn_end{CANCELED}，
  // specs/054-agent-v2-bugfixes/contracts/web-ui.md §4）。
  canceled: boolean
  onSend: (text: string) => void
  // 终止在途回合（POST {session}/agent:cancel 编排，App.tsx ChatPanel）；
  // promise 落定后解除防抖，请求失败由编排层呈现错误。
  onCancel: () => Promise<void>
}

// blockText projects the TEXT content out of either block shape (history
// protojson ContentBlock / live BlockDraft); THINK blocks render through
// ReasoningRow and TOOL_CALL blocks through ToolCard, so neither contributes
// body text here.
function blockText(b: ContentBlock | BlockDraft): string | undefined {
  if ('type' in b) return b.type === 'TEXT' ? b.text : undefined
  return b.text?.content
}

// blockThink projects the THINK content out of either block shape.
function blockThink(b: ContentBlock | BlockDraft): string | undefined {
  if ('type' in b) return b.type === 'THINK' ? b.text : undefined
  return b.think?.content
}

// protojson ToolStatus 枚举名（projects/game/agent_v2.proto ToolStatus）→
// ToolCard 状态；proto3 forward-compat：未知枚举值视作执行中
// （conversation-api.md §2 未知 oneof/枚举消费端忽略的同一容错方向）。
// historical 语境（历史回填/已 settled 分段）：status 仍为 RUNNING 且无
// result 的陈旧工具块按中断终态呈现（回合已异常结束、结果不再会到达——
// specs/054-agent-v2-bugfixes/data-model.md §2 回填侧推导，Edge Cases"无
// 结果的工具块不得呈现为永久运行中"）；流式活跃分段中的 RUNNING 仍为执行中。
function toolCardStatus(status: string, result: string | undefined, historical: boolean): ToolCardStatus {
  if (historical && status === 'TOOL_STATUS_RUNNING' && result === undefined) {
    return 'INTERRUPTED'
  }
  switch (status) {
    case 'TOOL_STATUS_SUCCEEDED':
      return 'SUCCEEDED'
    case 'TOOL_STATUS_FAILED':
      return 'FAILED'
    default:
      return 'RUNNING'
  }
}

interface ToolCallView {
  toolId: string
  name: string
  argsJson: string
  status: ToolCardStatus
  result?: string
}

// blockToolCall projects the TOOL_CALL content out of either block shape
// (BlockDraft 存 protojson 枚举名形式的 status，见 store/chat.ts
// blockStartDraft/blockEndTerminal；ContentBlock 为 protojson 投影本体).
// historical 透传给 toolCardStatus（历史语境的陈旧 RUNNING 推导中断态）。
function blockToolCall(b: ContentBlock | BlockDraft, historical: boolean): ToolCallView | undefined {
  if ('type' in b) {
    if (b.type !== 'TOOL_CALL') return undefined
    return {
      toolId: b.toolId,
      name: b.name,
      argsJson: b.args,
      status: toolCardStatus(b.status, b.result, historical),
      ...(b.result === undefined ? {} : { result: b.result }),
    }
  }
  if (b.toolCall === undefined) return undefined
  return {
    toolId: b.toolCall.toolId,
    name: b.toolCall.name,
    argsJson: b.toolCall.argsJson,
    status: toolCardStatus(b.toolCall.status, b.toolCall.result, historical),
    ...(b.toolCall.result === undefined ? {} : { result: b.toolCall.result }),
  }
}

// AgentStep renders one step's blocks in order: THINK → ReasoningRow、
// TEXT → MarkdownText、TOOL_CALL → ToolCard （web-frontend.md §2: 分类呈现
// 不混排）。streaming running 只落在流式回合最后一段的尾块上——流式块按序
// append 恒为尾块，已终结的 THINK 块（其后还有 TEXT 在流式）因此呈现完成态
// 摘要。非 running 语境（历史回填/已 settled 分段）中陈旧 RUNNING 工具块
// 推导中断终态（specs/054-agent-v2-bugfixes/data-model.md §2）。
//
// TEXT 块经 MarkdownText 渲染（specs/054-agent-v2-bugfixes/contracts/
// web-ui.md §3：GFM 正文、流式增量解析、不完整片段不崩溃）。streaming prop
// 传给仍在其流式回合活跃分段尾块上的 TEXT（与 THINK 的 running 判定同一
// 模式）：MarkdownText streaming 时按尾块增量重解析、已完结块冻结缓存
// （包 README "Markdown rendering"，@deepseek-ai/dsh-client-ui-primitives
// https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-primitives ），
// settle 后全量重解析落定。codeLabels 不传（无代码复制文案本地化需求，
// 包内默认标签即可，省去引用稳定性维护）。
function AgentStep({
  blocks,
  running,
}: {
  blocks: (ContentBlock | BlockDraft)[]
  running: boolean
}) {
  return (
    <div className="msg-agent" data-testid="agent-step">
      {blocks.map((b, i) => {
        const think = blockThink(b)
        if (think !== undefined) {
          return (
            <ReasoningRow
              key={i}
              text={think}
              running={running && i === blocks.length - 1}
            />
          )
        }
        const tool = blockToolCall(b, !running)
        if (tool !== undefined) {
          return <ToolCard key={i} {...tool} />
        }
        const text = blockText(b)
        if (text === undefined || text.trim() === '') return null
        return (
          <div key={i} data-testid="agent-text">
            <MarkdownText text={text} streaming={running && i === blocks.length - 1} />
          </div>
        )
      })}
    </div>
  )
}

// isFinalAnswer 判定一个 step 是否为回合的最终答案：含非空 text 块且无
// tool-call 块，且非 interrupted（specs/054-agent-v2-bugfixes/contracts/
// web-ui.md §2.2 折叠规则；interrupted 消息是中断前缀、非终态答案——
// specs/054-agent-v2-bugfixes/revisions/phase4-failed-turn-folding.md §1，
// A1 官方 'assistant-step' interrupted 三态基线）。
function isFinalAnswer(message: HistoryMessage): boolean {
  return (
    !message.interrupted &&
    !message.blocks.some((b) => b.toolCall !== undefined) &&
    message.blocks.some((b) => (b.text?.content ?? '').trim() !== '')
  )
}

// CompletedTurn renders one finished turn (a run of consecutive agent history
// messages, one per step): the final answer step stays独立呈现，此前 steps
// 默认折叠进"思考过程"摘要区（步骤/工具计数，点击展开；手动展开在页面会话
// 内保持，由 ChatView 的展开状态承载）；无最终答案的回合（失败/终止/纯工具
// 结束）保持全部过程内容可见不折叠。
function CompletedTurn({
  messages,
  expanded,
  onToggle,
}: {
  messages: HistoryMessage[]
  expanded: boolean
  onToggle: () => void
}) {
  let finalIndex = -1
  for (let i = messages.length - 1; i >= 0; i -= 1) {
    const message = messages[i]
    if (message !== undefined && isFinalAnswer(message)) {
      finalIndex = i
      break
    }
  }
  // 无最终答案、或最终答案即首个 step：无过程可收（同单 step 回合），全部
  // 直接呈现、不渲染折叠控件。
  if (finalIndex <= 0) {
    return (
      <>
        {messages.map((m, i) => (
          <AgentStep key={i} blocks={m.blocks} running={false} />
        ))}
      </>
    )
  }
  // 防御性边界：最终答案之后的 step 在正常驱动下不存在（最终答案取最后一
  // 个匹配），若出现则一并直接呈现，不静默丢弃。
  const process = messages.slice(0, finalIndex)
  const trailing = messages.slice(finalIndex + 1)
  const toolCount = process.reduce(
    (n, m) => n + m.blocks.filter((b) => b.toolCall !== undefined).length,
    0,
  )
  return (
    <>
      <button
        type="button"
        className="turn-process-toggle"
        data-testid="turn-process-toggle"
        aria-expanded={expanded}
        onClick={onToggle}
      >
        思考过程（{process.length} 步骤 · {toolCount} 次工具调用）
        {expanded ? ' ▾' : ' ▸'}
      </button>
      {expanded && (
        <div className="turn-process" data-testid="turn-process">
          {process.map((m, i) => (
            <AgentStep key={i} blocks={m.blocks} running={false} />
          ))}
        </div>
      )}
      <AgentStep blocks={messages[finalIndex]?.blocks ?? []} running={false} />
      {trailing.map((m, i) => (
        <AgentStep key={finalIndex + 1 + i} blocks={m.blocks} running={false} />
      ))}
    </>
  )
}

export function ChatView({
  session,
  history,
  live,
  queue,
  error,
  canceled,
  onSend,
  onCancel,
}: ChatViewProps) {
  const [draft, setDraft] = useState('')
  // 手动展开的完成回合（组首消息的 history index 为键；历史只追加，index
  // 稳定）。页面会话内保持，无持久化（官方差异声明：web-ui.md §2.3）；切换
  // 会话（loadHistory 重建 history）后旧 index 键指向另一回合，重置展开
  // 状态使回填回到默认折叠。
  const [expandedTurns, setExpandedTurns] = useState<ReadonlySet<number>>(new Set())
  // 终止请求在途标记：运行中重复点击防抖（web-ui.md §4）——在途期间按钮
  // 禁用并忽略后续点击，promise 落定即解除。
  const [cancelPending, setCancelPending] = useState(false)
  const messagesRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    setExpandedTurns(new Set())
  }, [session])

  // 流式跟随滚动：内容增长（历史、实时块、排队指示）即贴底。
  useEffect(() => {
    const el = messagesRef.current
    if (el !== null) el.scrollTop = el.scrollHeight
  }, [history, live, queue, error])

  const submit = () => {
    const text = draft.trim()
    if (text === '') return
    onSend(text)
    setDraft('')
  }

  // 终止入口仅 live 回合运行中呈现（web-ui.md §4：空闲不呈现触发面）；点击
  // 后等待流上 turn_end{CANCELED} 由 store 归约承载，按钮在请求在途期间
  // 禁用（防抖）。
  const cancel = () => {
    if (cancelPending) return
    setCancelPending(true)
    void onCancel().finally(() => setCancelPending(false))
  }

  const toggleTurn = (start: number): void => {
    setExpandedTurns((prev) => {
      const next = new Set(prev)
      if (next.has(start)) {
        next.delete(start)
      } else {
        next.add(start)
      }
      return next
    })
  }

  return (
    <div className="chat">
      <div className="chat-messages" data-testid="chat-messages" ref={messagesRef}>
        {history.map((m, i) => {
          if (m.role === 'ROLE_USER') {
            return (
              <div key={i} className="msg-user">
                {m.blocks.map((b, j) => (
                  <span key={j}>{b.text?.content ?? ''}</span>
                ))}
              </div>
            )
          }
          // 连续 ROLE_AGENT 消息构成一个已完成回合（服务端每 step 一条），
          // 由组首渲染整组并应用折叠；组内其余消息跳过。
          if (i > 0 && history[i - 1]?.role === 'ROLE_AGENT') return null
          let end = i
          while (end < history.length && history[end]?.role === 'ROLE_AGENT') end += 1
          const messages = history.slice(i, end)
          return (
            <CompletedTurn
              key={i}
              messages={messages}
              expanded={expandedTurns.has(i)}
              onToggle={() => toggleTurn(i)}
            />
          )
        })}
        {/* 流式回合各 step 分段依次独立呈现，全部展开（官方折叠规则：Turn
         * 打开期间过程行保持展开——web-ui.md §2.2）；running 只属最后一段。 */}
        {live !== null &&
          live.steps.map((step, i) => (
            <AgentStep key={step.step} blocks={step.blocks} running={i === live.steps.length - 1} />
          ))}
        {queue.map((q, i) => (
          <div key={i} className="queue-chip" data-testid="queue-chip">
            排队中 #{q.position}
          </div>
        ))}
        {error !== null && (
          <div className="chat-error" data-testid="chat-error" role="alert">
            {error}
          </div>
        )}
        {canceled && (
          // "已终止"终态标识：独立于错误文案（web-ui.md §4）。
          <div className="chat-canceled" data-testid="turn-canceled">
            已终止
          </div>
        )}
      </div>
      <div className="chat-composer">
        <Input
          className="chat-input"
          data-testid="chat-input"
          aria-label={`发送消息到 ${session}`}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') submit()
          }}
        />
        {live !== null && (
          <Button
            data-testid="cancel-button"
            disabled={cancelPending}
            onClick={cancel}
          >
            终止
          </Button>
        )}
        <Button
          variant="primary"
          data-testid="send-button"
          onClick={submit}
        >
          发送
        </Button>
      </div>
    </div>
  )
}
