// 会话对话状态 store：ChatEvent 归约 + useSyncExternalStore 绑定。归约不变式
// 照 specs/049-agent-v2-dsh-init/contracts/web-frontend.md §4，事件序保证见
// specs/049-agent-v2-dsh-init/contracts/conversation-api.md §3（每流恰好一个
// 终结 turn_end、queued 先于 turn_start、delta 按到达序拼接）。store 无框架
// 依赖；React 侧经 useChatState 订阅（react.dev/reference/react/
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

export interface LiveTurn {
  turnId: string
  blocks: BlockDraft[]
}

export interface ChatState {
  // :history 回填 + 流式回合合并产物（web-frontend.md §4）。
  history: HistoryMessage[]
  live: LiveTurn | null
  queue: QueuedMsg[]
  error: string | null
}

const EMPTY_STATE: ChatState = { history: [], live: null, queue: [], error: null }

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
      live: { turnId: event.turnId ?? '', blocks: [] },
      error: null,
    }
  }
  if (event.blockStart) {
    if (!state.live) return state
    return {
      ...state,
      live: { ...state.live, blocks: [...state.live.blocks, blockStartDraft(event.blockStart)] },
    }
  }
  if (event.delta) {
    if (!state.live) return state
    const blocks = state.live.blocks.map((b) => {
      if (b.index !== event.delta!.index) return b
      if (b.type === 'TOOL_CALL') return { ...b, args: b.args + event.delta!.text }
      return { ...b, text: b.text + event.delta!.text }
    })
    return { ...state, live: { ...state.live, blocks } }
  }
  if (event.blockEnd) {
    if (!state.live) return state
    const blocks = state.live.blocks.map((b) =>
      b.index === event.blockEnd!.index ? blockEndTerminal(b, event.blockEnd!.block) : b,
    )
    return { ...state, live: { ...state.live, blocks } }
  }
  if (event.turnEnd) {
    switch (event.turnEnd.status) {
      case 'TURN_STATUS_COMPLETED': {
        const merged: HistoryMessage =
          state.live && state.live.blocks.length > 0
            ? { role: ROLE_AGENT, blocks: liveBlocksToContentBlocks(state.live.blocks) }
            : { role: ROLE_AGENT, blocks: [] }
        return {
          ...state,
          history: state.live ? [...state.history, merged] : state.history,
          live: null,
        }
      }
      case 'TURN_STATUS_ERROR':
        // 本轮失败：明确提示，会话不崩、输入可重试（web-frontend.md §4）；
        // 失败回合的部分内容不并入历史，刷新后以服务端 :history 为准。
        return { ...state, live: null, error: event.turnEnd.error?.message ?? '对话回合失败' }
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

  // loadHistory rebuilds the state from a :history backfill (FR-014 回填，
  // web-frontend.md §4「刷新/切换会话 → 全量重建」)。
  loadHistory(messages: HistoryMessage[]): void {
    this.setState({ history: messages, live: null, queue: [], error: null })
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
      // turn_end{ABORTED} on a dispose race): the text never left the FIFO —
      // drop it so a later queued chip cannot pick it up.
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
