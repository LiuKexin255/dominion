// 会话对话状态 store：ChatEvent 归约 + useSyncExternalStore 绑定。归约不变式
// 照 specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §4，事件序保证
// 见 specs/049-agent-v2-dsh-init/contracts/conversation-api.md §3（每流恰好一
// 个终结 turn_end、queued 先于 turn_start、delta 按到达序拼接；tool_result 为
// 051 扩展帧，specs/051-agent-v2-dsh-migration/data-model.md §2.4）。块事件按
// step 分段路由（specs/054-agent-v2-bugfixes/data-model.md §5.1：step 为分组
// 维度、index 仍为块序维度），COMPLETED 回合依 step 投影多条历史。store 无框
// 架依赖；React 侧经 useChatState 订阅（react.dev/reference/react/
// useSyncExternalStore——getSnapshot 返回缓存快照，未变化时同一引用）。
import { useSyncExternalStore } from 'react'
import type { ChatEvent, ContentBlock, HistoryMessage, Role } from '../api/conversation.js'

// Role values the store produces when merging live turns into history.
const ROLE_AGENT: Role = 'ROLE_AGENT'

export interface QueuedMsg {
  text: string
  position: number
}

export type BlockDraft =
  | { index: number; type: 'TEXT' | 'THINK'; text: string }
  | {
      index: number
      type: 'TOOL_CALL'
      toolId: string
      name: string
      args: string
      status: string
      result?: string
    }

// StepDraft 是一个模型输出步骤的草稿（specs/054-agent-v2-bugfixes/
// data-model.md §5.1）：块按事件 step 归组；下一 step 的块事件到达即把此前
// step 置 settled（分段边界）。settled 供呈现层区分流式中的活跃分段。
export interface StepDraft {
  step: number
  blocks: BlockDraft[]
  settled: boolean
}

export interface LiveTurn {
  turnId: string
  steps: StepDraft[]
}

export interface ChatState {
  // 标准 List 回填 + 流式回合合并产物（web-frontend.md §4）。
  history: HistoryMessage[]
  live: LiveTurn | null
  queue: QueuedMsg[]
  error: string | null
  // 最近的回合以"已终止"终态收束（turn_end{CANCELED}，用户 :cancel）：
  // 呈现层据此渲染"已终止"标识——独立于 error（非错误文案，
  // specs/054-agent-v2-bugfixes/contracts/web-ui.md §4）。新回合开始
  // （turn_start）或回填重建时清除。
  canceled: boolean
}

const EMPTY_STATE: ChatState = { history: [], live: null, queue: [], error: null, canceled: false }

// liveBlocksToContentBlocks projects the accumulated drafts back to the
// protojson ContentBlock shape so merged turns render through the same path
// as backfilled history (FR-014 一致性).
function liveBlocksToContentBlocks(blocks: BlockDraft[]): ContentBlock[] {
  return blocks.map((b) => {
    if (b.type === 'TOOL_CALL') {
      return {
        toolCall: {
          toolId: b.toolId,
          name: b.name,
          argsJson: b.args,
          status: b.status,
          ...(b.result === undefined ? {} : { result: b.result }),
        },
      }
    }
    if (b.type === 'THINK') return { think: { content: b.text } }
    return { text: { content: b.text } }
  })
}

function blockStartDraft(event: NonNullable<ChatEvent['blockStart']>): BlockDraft {
  if (event.type === 'BLOCK_TYPE_TOOL_CALL') {
    return {
      index: event.index,
      type: 'TOOL_CALL',
      toolId: event.toolId ?? '',
      name: event.name ?? '',
      args: '',
      status: 'TOOL_STATUS_RUNNING',
    }
  }
  return {
    index: event.index,
    type: event.type === 'BLOCK_TYPE_THINK' ? 'THINK' : 'TEXT',
    text: '',
  }
}

// blockEndTerminal overlays the terminal block onto the draft (end 覆盖终态，
// web-frontend.md §4); the block content equals the concatenated deltas
// (conversation-api.md §3 不变式 4).
function blockEndTerminal(
  draft: BlockDraft,
  block: ContentBlock,
): BlockDraft {
  if (block.toolCall) {
    return {
      index: draft.index,
      type: 'TOOL_CALL',
      toolId: block.toolCall.toolId,
      name: block.toolCall.name,
      args: block.toolCall.argsJson,
      status: block.toolCall.status,
      ...(block.toolCall.result === undefined ? {} : { result: block.toolCall.result }),
    }
  }
  if (draft.type === 'TOOL_CALL') return draft
  const text = block.think?.content ?? block.text?.content ?? ''
  return { index: draft.index, type: draft.type, text }
}

// settleDraft overlays one tool_result frame onto a RUNNING tool-call draft;
// non-matching drafts pass through unchanged (data-model.md §2.4: tool_id is
// the join key; only the RUNNING block settles).
function settleDraft(draft: BlockDraft, toolId: string, status: string, result: string): BlockDraft {
  if (draft.type !== 'TOOL_CALL' || draft.toolId !== toolId || draft.status !== 'TOOL_STATUS_RUNNING') {
    return draft
  }
  return { ...draft, status, result }
}

// eventStep 落定事件的分组维度：缺 step（旧服务端/残留流）归组 0，行为退化
// 不崩溃（specs/054-agent-v2-bugfixes/data-model.md §5.1）。
function eventStep(step: number | undefined): number {
  return step ?? 0
}

// appendBlockStart routes one block_start onto its step group：新 step 的首
// 个块事件把此前全部 step 置 settled（分段边界）并开新组；同 step 的后续块
// 直接追加。
function appendBlockStart(steps: StepDraft[], step: number, draft: BlockDraft): StepDraft[] {
  if (steps.some((s) => s.step === step)) {
    return steps.map((s) => (s.step === step ? { ...s, blocks: [...s.blocks, draft] } : s))
  }
  return [...steps.map((s) => (s.settled ? s : { ...s, settled: true })), { step, blocks: [draft], settled: false }]
}

// mapStepBlocks applies one block-序 operation inside the step group the
// event routes to；group 不存在（病态序）时原样返回。
function mapStepBlocks(
  steps: StepDraft[],
  step: number,
  map: (blocks: BlockDraft[]) => BlockDraft[],
): StepDraft[] {
  return steps.map((s) => (s.step === step ? { ...s, blocks: map(s.blocks) } : s))
}

// settleHistoryMessage applies one tool_result frame to a backfilled history
// message: the matching RUNNING tool-call block (by tool_id) reaches its
// terminal status with the rendered result.
function settleHistoryMessage(
  message: HistoryMessage,
  toolId: string,
  status: string,
  result: string,
): HistoryMessage {
  return {
    ...message,
    blocks: message.blocks.map((b) =>
      b.toolCall?.toolId === toolId && b.toolCall.status === 'TOOL_STATUS_RUNNING'
        ? { ...b, toolCall: { ...b.toolCall, status, result } }
        : b,
    ),
  }
}

// stepsToHistory projects live step drafts onto history messages (one per
// step，对齐服务端每 step 一条 assistant/message——specs/054-agent-v2-bugfixes/
// data-model.md §5.1)。COMPLETED 与 ERROR 终态共用：ERROR（interrupted=true）
// 仅尾步消息标记 `interrupted: true`（与回填 List 的 HistoryMessage.interrupted
// 同构——刷新前后折叠判定一致，data-model §1.5；此前 step 不标记）；未完成尾
// 块以已流出内容原样投影（RUNNING 的 tool-call draft 保留 RUNNING，中断终态
// 由呈现层在历史语境推导），刷新回填后经同一呈现路径得到一致形态（FR-013）。
// CANCELED 终态复用 interrupted=true 投影（Phase 6 T014）。
function stepsToHistory(steps: StepDraft[], interrupted: boolean): HistoryMessage[] {
  return steps.map((s, i) => ({
    role: ROLE_AGENT,
    blocks: liveBlocksToContentBlocks(s.blocks),
    ...(interrupted && i === steps.length - 1 ? { interrupted: true } : {}),
  }))
}

// turnId anchors delta/block events to the live turn (server-minted UUID,
// constant across one turn's events — conversation-api.md §1).
function reduceEvent(state: ChatState, event: ChatEvent, queuedText = ''): ChatState {
  if (event.queued) {
    const item: QueuedMsg = { text: queuedText, position: event.queued.position }
    return { ...state, queue: [...state.queue, item] }
  }
  if (event.turnStart) {
    // 本消息 queue 项转为 live（排队指示消除）；队首即下一回合
    // （FR-012 自动按序发送，服务端回合序 = 队列序）。
    const [, ...rest] = state.queue
    return {
      ...state,
      queue: rest,
      live: { turnId: event.turnId ?? '', steps: [] },
      error: null,
      canceled: false,
    }
  }
  if (event.blockStart) {
    if (!state.live) return state
    return {
      ...state,
      live: {
        ...state.live,
        steps: appendBlockStart(state.live.steps, eventStep(event.blockStart.step), blockStartDraft(event.blockStart)),
      },
    }
  }
  if (event.delta) {
    if (!state.live) return state
    const { index, text } = event.delta
    return {
      ...state,
      live: {
        ...state.live,
        steps: mapStepBlocks(state.live.steps, eventStep(event.delta.step), (blocks) =>
          blocks.map((b) => {
            if (b.index !== index) return b
            if (b.type === 'TOOL_CALL') return { ...b, args: b.args + text }
            return { ...b, text: b.text + text }
          }),
        ),
      },
    }
  }
  if (event.blockEnd) {
    if (!state.live) return state
    const { index, block } = event.blockEnd
    return {
      ...state,
      live: {
        ...state.live,
        steps: mapStepBlocks(state.live.steps, eventStep(event.blockEnd.step), (blocks) =>
          blocks.map((b) => (b.index === index ? blockEndTerminal(b, block) : b)),
        ),
      },
    }
  }
  if (event.toolResult) {
    // 工具结果终态化（web-frontend.md §4）：按 tool_id 在 live 全部 step
    // （跨 step，tool_id 为回合内唯一关联键）与 history 中最近的 RUNNING
    // ToolCallBlock 更新终态；找不到（如重启后残留流）忽略。
    const { toolId, status, result } = event.toolResult
    if (state.live) {
      let hit = false
      const steps = state.live.steps.map((s) => {
        let stepHit = false
        const blocks = s.blocks.map((b) => {
          const next = settleDraft(b, toolId, status, result)
          stepHit = stepHit || next !== b
          return next
        })
        hit = hit || stepHit
        return stepHit ? { ...s, blocks } : s
      })
      if (hit) return { ...state, live: { ...state.live, steps } }
    }
    // live 未命中：回退到历史（逆序 = 最近的 AGENT 消息优先），仅存在匹配块
    // 时重建数组。
    for (let i = state.history.length - 1; i >= 0; i -= 1) {
      const m = state.history[i]
      if (
        m.blocks.some(
          (b) => b.toolCall?.toolId === toolId && b.toolCall.status === 'TOOL_STATUS_RUNNING',
        )
      ) {
        const history = state.history.slice()
        history[i] = settleHistoryMessage(m, toolId, status, result)
        return { ...state, history }
      }
    }
    return state
  }
  if (event.turnEnd) {
    switch (event.turnEnd.status) {
      case 'TURN_STATUS_COMPLETED': {
        // steps 依序投影为多条 HistoryMessage（每 step 一条，对齐服务端
        // 每 step 一条 assistant/message——specs/054-agent-v2-bugfixes/
        // data-model.md §5.2）；无块的空回合不投影空气泡。
        if (!state.live) return state
        const merged = stepsToHistory(state.live.steps, false)
        return {
          ...state,
          history: merged.length > 0 ? [...state.history, ...merged] : state.history,
          live: null,
        }
      }
      case 'TURN_STATUS_ERROR': {
        // 失败回合保留已呈现内容（specs/054-agent-v2-bugfixes/data-model.md
        // §5.2）：已呈现 step 并入本地历史（不清空、不原地消失），尾步消息
        // 标记 interrupted（§1.5，中断前缀非终态答案——折叠判定排除），
        // 错误提示独立、不吞内容（FR-013）；无 live 的失败（如首帧即
        // turn_end）只设置错误。
        const error = event.turnEnd.error?.message ?? '对话回合失败'
        if (!state.live) return { ...state, live: null, error }
        const merged = stepsToHistory(state.live.steps, true)
        return {
          ...state,
          history: merged.length > 0 ? [...state.history, ...merged] : state.history,
          live: null,
          error,
        }
      }
      case 'TURN_STATUS_CANCELED': {
        // 用户终止（specs/054-agent-v2-bugfixes/data-model.md §5.2）：保留
        // 语义复用 ERROR（已呈现 step 并入历史、尾步 interrupted 标记），
        // 终态标识为 canceled（"已终止"，非错误文案——web-ui.md §4）。服务
        // 端 :cancel 同时清空了待处理队列：每个排队流都会收到
        // turn_end{CANCELED}（无 live 的归约分支），排队 chip 随之移除；
        // 落地 user 消息已在其 queued 帧时入历史（enqueue 即固化，
        // data-model §3），故此处只清 chip 不动历史。
        if (!state.live) {
          return { ...state, live: null, queue: [], canceled: true }
        }
        const merged = stepsToHistory(state.live.steps, true)
        return {
          ...state,
          history: merged.length > 0 ? [...state.history, ...merged] : state.history,
          live: null,
          queue: [],
          canceled: true,
        }
      }
      case 'TURN_STATUS_ABORTED':
        // 会话已删除：清空（提示与返回列表由 App 层编排，web-frontend.md §4）。
        return EMPTY_STATE
      default:
        return state
    }
  }
  // 未知 oneof 分支：忽略（proto3 forward-compat，conversation-api.md §2）。
  return state
}

// ChatStore is one session's event-driven chat state. React components
// subscribe via useChatState; send() consumes one Send stream and feeds its
// events through the reducer.
export class ChatStore {
  private state: ChatState = EMPTY_STATE
  private listeners: (() => void)[] = []
  // sendTexts holds per-stream user texts in FIFO order: the queued event
  // carries only a position (conversation-api.md §1), the text is known
  // locally at send time; each stream consumes exactly one entry at its first
  // frame (queued or turn_start), see send().
  private sendTexts: string[] = []

  subscribe = (listener: () => void): (() => void) => {
    this.listeners = [...this.listeners, listener]
    return () => {
      this.listeners = this.listeners.filter((l) => l !== listener)
    }
  }

  getSnapshot = (): ChatState => this.state

  private setState(next: ChatState): void {
    this.state = next
    for (const l of this.listeners) l()
  }

  private apply(event: ChatEvent, queuedText?: string): void {
    this.setState(reduceEvent(this.state, event, queuedText))
  }

  applyEvent(event: ChatEvent): void {
    this.apply(event)
  }

  // loadHistory rebuilds the state from a List backfill (FR-014 回填，
  // web-frontend.md §4「刷新/切换会话 → 全量重建」)。
  loadHistory(messages: HistoryMessage[]): void {
    this.setState({ history: messages, live: null, queue: [], error: null, canceled: false })
  }

  // send consumes one Send stream: the user text feeds the queued indicator,
  // every event flows through the reducer. Each stream consumes its text from
  // the FIFO exactly once — at its first frame (queued, or turn_start for a
  // direct unqueued turn) — so a direct turn leaves no stale entry that a
  // later queued chip could pick up; a stream failing before the first frame
  // (request-level failure, conversation-api.md §2) cleans its residual. The
  // user message enters history at the moment the server records it (入队即
  // 记录，conversation-api.md §4 历史侧)——即首个 queued 帧，或无排队时的
  // turn_start；请求级失败则两者都不发生，与服务端状态保持一致。A stream
  // dying without turn_end (transport drop) surfaces as an error so the input
  // recovers (conversation-api.md §2 正常路径流尾即 turn_end).
  async send(text: string, stream: AsyncIterable<ChatEvent>): Promise<void> {
    const userMsg: HistoryMessage = {
      role: 'ROLE_USER',
      blocks: [{ text: { content: text } }],
    }
    let accepted = false
    const acceptUserMessage = (): void => {
      if (accepted) return
      accepted = true
      this.setState({ ...this.state, history: [...this.state.history, userMsg] })
    }
    let consumed = false
    const consumeSendText = (): string => {
      consumed = true
      const [head, ...rest] = this.sendTexts
      this.sendTexts = rest
      return head ?? ''
    }
    this.sendTexts.push(text)
    try {
      for await (const event of stream) {
        if (event.queued) {
          const queuedText = consumeSendText()
          acceptUserMessage()
          this.apply(event, queuedText)
          continue
        }
        if (event.turnStart) {
          consumeSendText()
          acceptUserMessage()
          this.apply(event)
          continue
        }
        this.apply(event)
      }
      // Stream ended normally without ever delivering queued/turn_start (e.g.
      // first frame is turn_end{ERROR} on a session-create failure, or
      // turn_end{ABORTED} on a re-materialization race): the text never left
      // the FIFO — drop it so a later queued chip cannot pick it up.
      if (!consumed) consumeSendText()
    } catch (err) {
      if (!consumed) consumeSendText()
      this.setState({
        ...this.state,
        live: null,
        error: err instanceof Error ? err.message : String(err),
      })
    }
  }
}

export function useChatState(store: ChatStore): ChatState {
  return useSyncExternalStore(store.subscribe, store.getSnapshot)
}
