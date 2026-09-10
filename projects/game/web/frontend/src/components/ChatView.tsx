// 对话主区：团队视图（history/stream 归并渲染 + 成员标签）+ 成员视角视图
// （memberHistory 回填/自身输出固化 + live 按 member 过滤）+ 排队指示 + 发送
// 输入（行为基线 desktop ChatView，契约 specs/059-agent-v2-team-mode/
// contracts/web-views.md §2/§3/§4）。team 归并序列条目按 seq 序呈现（USER
// 气泡 / 成员原生输出带 player/planner 标签，不显示广播包装形态）；成员
// 视角序列按消费面呈现（ROLE_AGENT=自己的输出（agent 形态）；ROLE_USER +
// sender="user"=用户气泡；ROLE_USER + sender=成员 role=标注来源的用户消息
// `user: [sender] 正文`）。成员输出按模型输出步骤分段呈现
// （specs/054-agent-v2-bugfixes/contracts/web-ui.md §2.2）：历史一条消息即
// 一个 step、live 回合每个 step 一个分段容器，依次独立呈现；步骤内 THINK →
// ReasoningRow、TEXT → MarkdownText、TOOL_CALL → ToolCard 分类分列不混排。
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import {
  Button,
  IconChevronDownOutline14,
  Input,
  MarkdownText,
} from '@deepseek-ai/dsh-client-ui-primitives'
import type { ContentBlock, HistoryMessage } from '../api/conversation.js'
import { USER_MEMBER } from '../api/conversation.js'
import type {
  BlockDraft,
  LiveMemberTurn,
  MemberViewEntry,
  QueuedMsg,
  TeamMessageEntry,
} from '../store/chat.js'
import { ReasoningRow } from './ReasoningRow.js'
import { ToolCard, type ToolCardStatus } from './ToolCard.js'

// TEAM_VIEW 是团队视图的 view 值；其余取值即成员 role 字符串（场景词汇，
// saolei 下 "player"/"planner"），直接按 wire 字符串消费（web-views.md §2）。
export const TEAM_VIEW = 'team'

// 贴底判定阈值（px）：距底 ≤ 阈值视为贴底并跟随（R7，取上游既定值 24，
// 上游来源与设计见 specs/055-agent-v2-ui-fixes/research.md §2.2）。
const FOLLOW_THRESHOLD = 24

export interface ChatViewProps {
  session: string
  // 活动视图：TEAM_VIEW（团队视图）或成员 role 字符串（成员视角视图）。
  // 缺省团队视图（切换为纯前端状态，由调用方持有；web-views.md §2）。
  view?: string
  // 团队视图归并序列（seq 锚；web-views.md §2）：team_message 帧 + List 回填。
  history: TeamMessageEntry[]
  // 每成员视角序列（web-views.md §2/§4）：ListMemberMessages 回填 + 自身
  // 输出固化；按 memberHistory[view] 取当前成员视角。
  memberHistory?: Record<string, MemberViewEntry[]>
  // team 流覆盖的流式成员回合（按 (member, turnId) 分组；多回合持续流）。
  live: LiveMemberTurn[]
  queue: QueuedMsg[]
  error: string | null
  // 最近回合是否以"已终止"终态收束（store 归约 turn_end{CANCELED}，
  // specs/054-agent-v2-bugfixes/contracts/web-ui.md §4）。
  canceled: boolean
  onSend: (text: string) => void
  // 终止 team 在途回合（POST {session}/team:cancel 编排，App.tsx ChatPanel）；
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

// MemberTag 标注成员归属（web-views.md §3：成员消息归属到所属成员名下，
// 团队视图用成员标签区分 player/planner；MUST NOT 显示广播包装形态）。
// member 为 wire role 字符串，直接渲染（无枚举名前缀归一化）。
function MemberTag({ member }: { member: string }) {
  return (
    <span className="member-tag" data-testid="member-tag" data-member={member}>
      {member}
    </span>
  )
}

// MemberTurn renders one member's finished turn group (consecutive
// same-member step messages) with its member label; folding stays per member
// (web-views.md §3). A user entry or another member breaks the group.
function MemberTurn({
  member,
  messages,
  expanded,
  onToggle,
}: {
  member: string
  messages: HistoryMessage[]
  expanded: boolean
  onToggle: () => void
}) {
  return (
    <div className="member-turn" data-testid="member-turn" data-member={member}>
      <MemberTag member={member} />
      <CompletedTurn messages={messages} expanded={expanded} onToggle={onToggle} />
    </div>
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
        data-open={expanded || undefined}
        aria-expanded={expanded}
        onClick={(event) => {
          // 显式聚焦加固（对齐上游 TurnProcessNodeView.tsx onClick）：Safari
          // 等 click 不聚焦 button 的浏览器保证焦点落回开关。
          // https://github.com/deepseek-ai/deepseek-harness/blob/master/
          // packages/client/ui-chat/src/client/chat/TurnProcessNodeView.tsx
          event.currentTarget.focus()
          onToggle()
        }}
      >
        思考过程（{process.length} 步骤 · {toolCount} 次工具调用）
        <IconChevronDownOutline14 className="turn-process-chevron" />
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

// messageText joins a history message's text blocks (成员视角的广播注入条目
// 为纯文本——服务端 appendMemberViewUser 只投影 text 块，
// projects/game/agent_v2/src/history.ts)。
function messageText(message: HistoryMessage): string {
  return message.blocks.map((b) => b.text?.content ?? '').join('')
}

// UserBubble 是用户消息气泡（团队视图的用户输入与成员视角的用户输入共用
// 形态；web-views.md §3/§4：纯文本、不 markdown 化）。
function UserBubble({ message }: { message: HistoryMessage }) {
  return (
    <div className="msg-user">
      {message.blocks.map((b, j) => (
        <span key={j}>{b.text?.content ?? ''}</span>
      ))}
    </div>
  )
}

// TeamMessages renders the merged team sequence（web-views.md §3）：全部消息
// 按 seq 归并；成员原生输出归属成员名下（成员标签），连续同成员 AGENT 条目
// 构成该成员的一个已完成回合并由 CompletedTurn 折叠（按成员维度）。广播
// 包装形态（`[sender] <sender-message>` 标签对）属于成员视角的注入格式，
// 不进入本视图——数据源即原生输出（ListTeamMessages/team_message 帧）。
function TeamMessages({
  history,
  live,
  expanded,
  onToggle,
}: {
  history: TeamMessageEntry[]
  live: LiveMemberTurn[]
  expanded: ReadonlySet<string>
  onToggle: (key: string) => void
}) {
  return (
    <>
      {history.map((entry, i) => {
        const { member, message } = entry
        if (member === USER_MEMBER) {
          return <UserBubble key={i} message={message} />
        }
        // 连续同成员 AGENT 条目构成该成员的一个已完成回合（服务端每 step
        // 一条），由组首渲染整组并应用折叠（web-views.md §3：折叠按成员
        // 维度）；组内其余条目跳过；USER 或另一成员条目断开分组。
        if (
          i > 0 &&
          history[i - 1]?.member === member &&
          history[i - 1]?.message.role === 'ROLE_AGENT'
        ) {
          return null
        }
        let end = i
        while (
          end < history.length &&
          history[end]?.member === member &&
          history[end]?.message.role === 'ROLE_AGENT'
        ) {
          end += 1
        }
        const messages = history.slice(i, end).map((e) => e.message)
        const key = `${TEAM_VIEW}:${i}`
        return (
          <MemberTurn
            key={i}
            member={member}
            messages={messages}
            expanded={expanded.has(key)}
            onToggle={() => onToggle(key)}
          />
        )
      })}
      {/* 流式成员回合各 step 分段依次独立呈现（全部展开——官方折叠规则：
       * Turn 打开期间过程行保持展开，web-ui.md §2.2）；running 只属该回合
       * 最后一段；已由 team_message 帧固化的前导 step 已进入归并序列，
       * 跳过以免重复呈现。 */}
      {live.map((turn) => {
        const steps = turn.steps.slice(turn.fixedSteps)
        if (steps.length === 0) return null
        return (
          <div
            key={`${turn.member}:${turn.turnId}`}
            className="member-turn"
            data-testid="member-turn"
            data-member={turn.member}
          >
            <MemberTag member={turn.member} />
            {steps.map((step, i) => (
              <AgentStep
                key={step.step}
                blocks={step.blocks}
                running={i === steps.length - 1}
              />
            ))}
          </div>
        )
      })}
    </>
  )
}

// MemberMessages renders one member's view sequence（web-views.md §4）：
// ROLE_AGENT = 该成员自己的输出（agent 形态，折叠按自身回合分组）；
// ROLE_USER + sender="user" = 用户输入气泡；ROLE_USER + sender=成员 role =
// 标注来源的用户消息（`user: [sender] 正文`——正文为该成员实际消费的注入
// 原文，含其转发的工具调用与正文）。live 为按 member 过滤后的流式回合
// （其他成员的流式事件不进入本视角——其他成员产出在其被驱动消费前不出现，
// 经回填以消费面呈现）。
function MemberMessages({
  member,
  entries,
  live,
  expanded,
  onToggle,
}: {
  member: string
  entries: MemberViewEntry[]
  live: LiveMemberTurn[]
  expanded: ReadonlySet<string>
  onToggle: (key: string) => void
}) {
  return (
    <>
      {entries.map((entry, i) => {
        const { message, sender } = entry
        if (message.role === 'ROLE_AGENT') {
          // 连续 AGENT 条目构成该成员的一个已完成回合（服务端每 step 一条），
          // 由组首渲染整组并应用折叠。
          if (i > 0 && entries[i - 1]?.message.role === 'ROLE_AGENT') return null
          let end = i
          while (
            end < entries.length &&
            entries[end]?.message.role === 'ROLE_AGENT'
          ) {
            end += 1
          }
          const messages = entries.slice(i, end).map((e) => e.message)
          const key = `${member}:${i}`
          return (
            <div
              key={i}
              className="member-turn"
              data-testid="member-turn"
              data-member={member}
            >
              <CompletedTurn
                messages={messages}
                expanded={expanded.has(key)}
                onToggle={() => onToggle(key)}
              />
            </div>
          )
        }
        // 空 sender = 未设置（proto3 缺省；防御性按用户输入呈现）。sender 为
        // 另一成员 role 时是广播注入（sender 即 role 字符串原值）。
        if (sender === undefined || sender === '' || sender === USER_MEMBER) {
          return <UserBubble key={i} message={message} />
        }
        return (
          <div
            key={i}
            className="msg-user msg-relay"
            data-testid="member-relay"
            data-sender={sender}
          >
            <span className="relay-source" data-testid="relay-source">
              user: [{sender}]
            </span>
            <span className="relay-body" data-testid="relay-body">
              {messageText(message)}
            </span>
          </div>
        )
      })}
      {live.map((turn) => {
        const steps = turn.steps.slice(turn.fixedSteps)
        if (steps.length === 0) return null
        return (
          <div
            key={`${turn.member}:${turn.turnId}`}
            className="member-turn"
            data-testid="member-turn"
            data-member={turn.member}
          >
            {steps.map((step, i) => (
              <AgentStep
                key={step.step}
                blocks={step.blocks}
                running={i === steps.length - 1}
              />
            ))}
          </div>
        )
      })}
    </>
  )
}

export function ChatView({
  session,
  view = TEAM_VIEW,
  history,
  memberHistory = {},
  live,
  queue,
  error,
  canceled,
  onSend,
  onCancel,
}: ChatViewProps) {
  const [draft, setDraft] = useState('')
  // 手动展开的完成回合（键 = `${view}:${组首 index}`；历史只追加，index
  // 稳定）。页面会话内保持，无持久化（官方差异声明：web-ui.md §2.3）；切换
  // 会话（loadHistory 重建 history）后旧 index 键指向另一回合，重置展开
  // 状态使回填回到默认折叠。视图前缀隔离团队/成员序列的同 index 碰撞。
  const [expandedTurns, setExpandedTurns] = useState<ReadonlySet<string>>(new Set())
  // 终止请求在途标记：运行中重复点击防抖（web-ui.md §4）——在途期间按钮
  // 禁用并忽略后续点击，promise 落定即解除。
  const [cancelPending, setCancelPending] = useState(false)
  const messagesRef = useRef<HTMLDivElement | null>(null)
  // 贴底跟随状态机（specs/055-agent-v2-ui-fixes/data-model.md §1）：仅贴底
  // 时内容增长跟随；ref 镜像供 scroll 监听（mount 一次）比对去重。
  const [atBottom, setAtBottom] = useState(true)
  const atBottomRef = useRef(true)

  useEffect(() => {
    setExpandedTurns(new Set())
    // 切换会话回到底部视角（specs/055-agent-v2-ui-fixes/spec.md Edge Cases
    // "多会话切换"；既有回填回底行为零回归）。
    atBottomRef.current = true
    setAtBottom(true)
  }, [session])

  // scroll 监听维护 atBottom：距底 ≤ FOLLOW_THRESHOLD 视为贴底（R7，上游
  // FOLLOW_THRESHOLD = 24，specs/055-agent-v2-ui-fixes/data-model.md §1）。
  useEffect(() => {
    const el = messagesRef.current
    if (el === null) return undefined
    const onScroll = (): void => {
      const next = el.scrollHeight - el.scrollTop - el.clientHeight <= FOLLOW_THRESHOLD
      if (next === atBottomRef.current) return
      atBottomRef.current = next
      setAtBottom(next)
    }
    el.addEventListener('scroll', onScroll)
    return () => el.removeEventListener('scroll', onScroll)
  }, [])

  // 内容增长的贴底 effect：仅贴底时跟随；非贴底（跟随停止）不改变阅读
  // 位置——含回合终态横幅出现时（Edge Cases"回合结束时的视角"不强行拉底）。
  // useLayoutEffect 使滚动写入在浏览器 paint 前完成（视觉同步：流式高频
  // chunk 下避免"内容已增长、视图晚一帧回底"的闪烁；对齐上游同型视觉
  // effect，React 文档 https://react.dev/reference/react/useLayoutEffect ）。
  useLayoutEffect(() => {
    const el = messagesRef.current
    if (el === null || !atBottom) return
    el.scrollTop = el.scrollHeight
  }, [view, history, memberHistory, live, queue, error, atBottom])

  // 无条件回底入口：发新消息（FR-003）与"回到底部"按钮共用——回底即恢复
  // 跟随（specs/055-agent-v2-ui-fixes/data-model.md §1）。
  const toBottom = (): void => {
    const el = messagesRef.current
    if (el !== null) el.scrollTop = el.scrollHeight
    atBottomRef.current = true
    setAtBottom(true)
  }

  const submit = () => {
    const text = draft.trim()
    if (text === '') return
    onSend(text)
    setDraft('')
    toBottom()
  }

  // 终止入口仅 live 回合运行中呈现（web-ui.md §4：空闲不呈现触发面）；点击
  // 后等待流上 turn_end{CANCELED} 由 store 归约承载，按钮在请求在途期间
  // 禁用（防抖）。
  const cancel = () => {
    if (cancelPending) return
    setCancelPending(true)
    // onCancel 的请求级失败由编排层（App.tsx ChatPanel）呈现；组件侧仅在
    // 落定后解除防抖，不让 rejection 变成未处理拒绝。
    void onCancel().then(
      () => setCancelPending(false),
      () => setCancelPending(false),
    )
  }

  const toggleTurn = (key: string): void => {
    setExpandedTurns((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }

  // 活动视图的数据面：团队视图直接消费归并序列与全部 live 回合；成员视角
  // 消费该成员的视角序列与按 member 过滤的 live 回合（其他成员的流式产出
  // 不进入本视角，web-views.md §2/§4）。
  const memberView = view !== TEAM_VIEW ? view : null
  const memberEntries = memberView !== null ? (memberHistory[memberView] ?? []) : []
  const viewLive = memberView !== null ? live.filter((t) => t.member === memberView) : live

  return (
    <div className="chat">
      <div className="chat-messages" data-testid="chat-messages" ref={messagesRef}>
        {memberView === null ? (
          <TeamMessages
            history={history}
            live={viewLive}
            expanded={expandedTurns}
            onToggle={toggleTurn}
          />
        ) : (
          <MemberMessages
            member={memberView}
            entries={memberEntries}
            live={viewLive}
            expanded={expandedTurns}
            onToggle={toggleTurn}
          />
        )}
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
        {/* 回到底部浮动入口：渲染条件 = !atBottom，与回合状态无关（FR-004、
         * specs/055-agent-v2-ui-fixes/data-model.md §1 不变量）；sticky 槽
         * 挂在消息区（唯一滚动面）内随视口浮动，点击回底并恢复跟随。 */}
        {!atBottom && (
          <div className="to-bottom-slot">
            <button
              type="button"
              className="to-bottom"
              aria-label="回到底部"
              data-testid="to-bottom-button"
              onClick={toBottom}
            >
              <IconChevronDownOutline14 />
            </button>
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
        {live.length > 0 && (
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
