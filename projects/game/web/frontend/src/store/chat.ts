// 会话对话状态 store：team 流 ChatEvent 归约 + useSyncExternalStore 绑定。
// 归约契约见 specs/059-agent-v2-team-mode/contracts/team-api.md §3 与
// contracts/web-views.md §2/§3/§4，增量修订见
// specs/060-agent-v2-team-optimize/contracts/team-api.md §2/§3——双帧承载：
// 成员事件帧按 (member, turn_id) 分组增量渲染（block index/step 以成员回合
// 为单位），`team_message` 帧按 seq 作归并序锚（与 ListTeamMessages 同源，
// 保证实时归并序与回填一致）。store 维护双形态：团队归并序列（history，
// web-views.md §3）与每成员视角序列（memberHistory，§4）——成员自身输出在
// 固化时双写；用户输入与跨成员广播注入在被成员消费时经 `member_view` 帧
// 进入其视角（messageId 幂等），ListMemberMessages 回填作权威序列重对齐。
// 并发流重复帧按锚幂等：`team_message` 同 seq / `member_view` 同 messageId
// 忽略，成员帧按 (member, turn_id) + index 幂等（team-api.md §3.4）。store
// 无框架依赖；React 侧经 useChatState 订阅
// （react.dev/reference/react/useSyncExternalStore——getSnapshot 返回缓存
// 快照，未变化时同一引用）。
import { useSyncExternalStore } from 'react'
import type {
  ChatEvent,
  ContentBlock,
  HistoryMessage,
  Role,
  TeamMessage,
} from '../api/conversation.js'
import { seqOf, USER_MEMBER } from '../api/conversation.js'

// Role values the store produces when merging live member turns into history.
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

// StepDraft 是一个成员模型输出步骤的草稿（specs/054-agent-v2-bugfixes/
// data-model.md §5.1）：块按事件 step 归组；下一 step 的块事件到达即把此前
// step 置 settled（分段边界）。settled 供呈现层区分流式中的活跃分段。
export interface StepDraft {
  step: number
  blocks: BlockDraft[]
  settled: boolean
}

// LiveMemberTurn 是一个成员回合的流式草稿（team-api.md §3.2：成员事件帧按
// (member, turn_id) 分组）。fixedSteps = 已由 `team_message` 帧固化入归并
// 序列的前导 step 数——渲染时跳过，避免同一 step 同时以 live 草稿与归并
// 条目重复呈现。owners = 每块（`step:index`）的 delta 归属流 id：并发 team
// 流完整扇出同一回合（team-api.md §3.4），同一 block 的 delta 只能由一个流
// 应用一次——首个处理该 block_start 的流成为 owner，其余流的重复 delta 忽略
// （delta 无帧内标识，块归属是可将重复帧判定为幂等的锚）；归属流终止时释放
// 归属，存活流从当前进度无缝续接，无重复也无缺口。
export interface LiveMemberTurn {
  member: string
  turnId: string
  steps: StepDraft[]
  fixedSteps: number
  owners: Record<string, number>
}

// blockKey 是块草稿的回合内唯一键（step 与 index 均以成员回合为单位）。
function blockKey(step: number, index: number): string {
  return `${step}:${index}`
}

// claimBlock returns the owners record with the block claimed by streamId when
// it is unowned (首次处理该块的流成为 owner)；已归属他流时原样返回。
function claimBlock(
  owners: Record<string, number>,
  key: string,
  streamId: number,
): Record<string, number> {
  return owners[key] === undefined ? { ...owners, [key]: streamId } : owners
}

// TeamMessageEntry 是团队视图归并序列条目（web-views.md §2：team_message
// 帧载荷与 ListTeamMessages 元素同构）。
export interface TeamMessageEntry {
  member: string
  message: HistoryMessage
  seq: number
  // 本地投影占位：回合收束（turn_end）/流断开时 team_message 帧未到达的
  // 尾步按已流出内容先行投影（内容不消失、订阅中途加入的回合可见），后续
  // 同成员首个真实帧按到达序替换它（保持数组位置；seq 为负的本地占位值，
  // 不与服务端 seq 冲突）——真实帧到达后不重复。
  projected?: boolean
  // live 进行中回合的客户端派生标记（specs/064-memory-split-fold-remain/
  // contracts/web-ui.md §3 与 data-model.md §3.1）：team_message 归约时该
  // 成员存在打开的 live 回合 → 固化条目落 true，分组层据此整组流式展开、
  // 不进三分类。回合收束全路径（closeLiveTurn——含全部步已固化的早退路径）
  // 清除；回填重建、投影占位与用户消息条目恒不携带。服务端 HistoryMessage
  // 无回合状态字段，标记来源是 store 观察到的回合生命周期。
  open?: boolean
}

// MemberViewEntry 是成员视角序列条目（web-views.md §4：与 ListMemberMessages
// 的 MemberViewMessage 同构）。message.role 沿用 HistoryMessage 枚举：
// ROLE_AGENT = 该成员自己的输出；ROLE_USER + sender="user" = 用户输入；
// ROLE_USER + sender=成员 role = 广播注入（渲染 `user: [sender] 正文`）。
export interface MemberViewEntry {
  message: HistoryMessage
  sender: string
  // 本地投影占位（语义同 TeamMessageEntry.projected）：该成员回合收束但
  // team_message 帧未到达的尾步先行投影，后续同成员真实条目按 FIFO 原位
  // 替换；ListMemberMessages 回填以服务端序列整体取代时丢弃。
  projected?: boolean
  // live 进行中回合的客户端派生标记（与 TeamMessageEntry.open 同源同语义，
  // specs/064-memory-split-fold-remain/contracts/web-ui.md §3）：该成员视角
  // 的固化条目在成员存在打开的 live 回合时落 true，成员视角分组据此整组
  // 展开；回合收束清除；回填条目与投影占位恒不携带。
  open?: boolean
}

export interface ChatState {
  // 归并序列（seq 锚）：team_message 帧实时追加 + ListTeamMessages 回填；
  // 回合收束但帧未到达的尾步以本地投影占位先行进入（见 TeamMessageEntry）。
  history: TeamMessageEntry[]
  // 每成员视角序列（member 为 wire role 字符串）：ListMemberMessages 回填 +
  // 成员自身输出固化（team_message 帧与该成员团队视图条目同源同值）。
  memberHistory: Record<string, MemberViewEntry[]>
  // 流式成员回合草稿（team 流覆盖的多回合）：按到达序排列，串行驱动下同一
  // 时刻至多一个进行中回合；回合收束（turn_end/流断开）即投影入归并序列
  // 与该成员自身视角序列。
  live: LiveMemberTurn[]
  queue: QueuedMsg[]
  error: string | null
  // 最近的回合以"已终止"终态收束（turn_end{CANCELED}，用户 team 级取消）：
  // 呈现层据此渲染"已终止"标识——独立于 error（非错误文案，
  // specs/054-agent-v2-bugfixes/contracts/web-ui.md §4）。新回合开始
  // （turn_start）或回填重建时清除。
  canceled: boolean
}

const EMPTY_STATE: ChatState = {
  history: [],
  memberHistory: {},
  live: [],
  queue: [],
  error: null,
  canceled: false,
}

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

// blockEndTerminal overlays the terminal block onto the draft (end 覆盖终态,
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
// 直接追加。同 (step, index) 的重复 block_start（并发流扇出）是幂等 no-op，
// 不重置已累积的 delta。
function appendBlockStart(steps: StepDraft[], step: number, draft: BlockDraft): StepDraft[] {
  const group = steps.find((s) => s.step === step)
  if (group !== undefined) {
    if (group.blocks.some((b) => b.index === draft.index)) return steps
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

// settleHistoryEntry applies one tool_result frame to a merged-sequence
// entry: the matching RUNNING tool-call block (by tool_id) reaches its
// terminal status with the rendered result. An entry without a matching
// RUNNING block is returned unchanged (same reference), so re-applying the
// same frame is a no-op — the natural dedup for concurrent-stream duplicate
// frames (team-api.md §3.4).
function settleHistoryEntry(
  entry: TeamMessageEntry,
  toolId: string,
  status: string,
  result: string,
): TeamMessageEntry {
  if (
    !entry.message.blocks.some(
      (b) => b.toolCall?.toolId === toolId && b.toolCall.status === 'TOOL_STATUS_RUNNING',
    )
  ) {
    return entry
  }
  return {
    ...entry,
    message: {
      ...entry.message,
      blocks: entry.message.blocks.map((b) =>
        b.toolCall?.toolId === toolId && b.toolCall.status === 'TOOL_STATUS_RUNNING'
          ? { ...b, toolCall: { ...b.toolCall, status, result } }
          : b,
      ),
    },
  }
}

// settleMemberHistory applies one tool_result frame to the matching RUNNING
// tool-call block inside a member's own view: the consolidated output is the
// same message the merged sequence carries (appendMemberView), so the tool
// settlement is mirrored there (the server mutates the shared message, both
// projections observe it — projects/game/agent_v2/src/history.ts
// settleToolResult).
function settleMemberHistory(
  memberHistory: Record<string, MemberViewEntry[]>,
  member: string,
  toolId: string,
  status: string,
  result: string,
): Record<string, MemberViewEntry[]> {
  const view = memberHistory[member]
  if (view === undefined) return memberHistory
  let changed = false
  const next = view.map((entry) => {
    if (
      !entry.message.blocks.some(
        (b) => b.toolCall?.toolId === toolId && b.toolCall.status === 'TOOL_STATUS_RUNNING',
      )
    ) {
      return entry
    }
    changed = true
    return {
      ...entry,
      message: {
        ...entry.message,
        blocks: entry.message.blocks.map((b) =>
          b.toolCall?.toolId === toolId && b.toolCall.status === 'TOOL_STATUS_RUNNING'
            ? { ...b, toolCall: { ...b.toolCall, status, result } }
            : b,
        ),
      },
    }
  })
  return changed ? { ...memberHistory, [member]: next } : memberHistory
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

// insertBySeq inserts a merged-sequence entry at its seq position (real
// entries stay seq-ordered even when frames arrive across concurrent streams);
// duplicate seq is filtered by the caller (seq is the merge-order anchor).
function insertBySeq(history: TeamMessageEntry[], entry: TeamMessageEntry): TeamMessageEntry[] {
  const index = history.findIndex((e) => e.seq > entry.seq)
  if (index < 0) return [...history, entry]
  return [...history.slice(0, index), entry, ...history.slice(index)]
}

// eventMember resolves the member a member event frame belongs to: the frame's
// own role string, or — for a member-less frame on a stream holding exactly one
// live turn (degraded/legacy frames) — that turn's member. Empty string =
// unset (team-api.md §3.2: member is only set on member event frames).
function eventMember(state: ChatState, event: ChatEvent): string | undefined {
  const member = event.member
  if (member !== undefined && member !== '') return member
  return state.live.length === 1 ? state.live[0]?.member : undefined
}

// findLiveTurn locates the live draft by (member, turn_id) — the grouping key
// of member event frames (team-api.md §3.2).
function findLiveTurn(state: ChatState, member: string | undefined, turnId: string): number {
  if (member === undefined) return -1
  return state.live.findIndex((t) => t.member === member && t.turnId === turnId)
}

// projectTail projects one closed/failed turn's unconsolidated steps into the
// merged sequence as local placeholders (negative seq keeps them distinct from
// server seq anchors; array position is the render order). A later real
// `team_message` frame for the same member replaces the first placeholder in
// arrival order. 投影条目恒不携带 open 标记（投影只产生于回合收束之后，
// specs/064-memory-split-fold-remain/contracts/web-ui.md §3）。
function projectTail(
  history: TeamMessageEntry[],
  member: string,
  messages: HistoryMessage[],
): TeamMessageEntry[] {
  let seq = history.reduce((min, e) => Math.min(min, e.seq), 0)
  return messages.map((message) => {
    seq -= 1
    return { member, message, seq, projected: true }
  })
}

// appendMemberInputView appends one consumed input (a member_view frame) to
// the consuming member's view (web-views.md §4; specs/060-agent-v2-team-
// optimize/contracts/team-api.md §2). The idempotency anchor is messageId — a
// duplicate frame from a concurrent stream is ignored. Only this view is
// touched: the merged sequence, live drafts, and the queue do not carry the
// consumption fact (consumption before the input appears stays unforced).
function appendMemberInputView(
  memberHistory: Record<string, MemberViewEntry[]>,
  member: string,
  message: HistoryMessage,
  sender: string,
): Record<string, MemberViewEntry[]> {
  const view = memberHistory[member] ?? []
  const messageId = message.messageId
  if (
    messageId !== undefined &&
    messageId !== '' &&
    view.some((entry) => entry.message.messageId === messageId)
  ) {
    return memberHistory
  }
  return { ...memberHistory, [member]: [...view, { message, sender }] }
}

// appendMemberView consolidates one member output into that member's own view
// (web-views.md §2/§4: the team_message frame for a member output carries the
// same message object the server writes into both projections — the merged
// sequence and the producer's own view). Concurrent-stream duplicate frames
// are idempotent by messageId; a projected local placeholder is replaced in
// arrival order, mirroring the merged sequence. open 透传该成员是否存在打开
// 的 live 回合（specs/064-memory-split-fold-remain/contracts/web-ui.md §3/§4）；
// projected 替换路径不落标记（占位条目诞生于已收束回合，该成员新回合的
// live 不得使上一回合的迟到帧被误标 open）。
function appendMemberView(
  memberHistory: Record<string, MemberViewEntry[]>,
  member: string,
  message: HistoryMessage,
  open: boolean,
): Record<string, MemberViewEntry[]> {
  const view = memberHistory[member] ?? []
  const messageId = message.messageId
  if (
    messageId !== undefined &&
    messageId !== '' &&
    view.some((e) => e.message.messageId === messageId)
  ) {
    return memberHistory
  }
  const projectedIndex = view.findIndex((e) => e.projected === true)
  const entry: MemberViewEntry = {
    message,
    sender: member,
    ...(open && projectedIndex < 0 ? { open: true } : {}),
  }
  const next =
    projectedIndex >= 0
      ? view.map((e, i) => (i === projectedIndex ? entry : e))
      : [...view, entry]
  return { ...memberHistory, [member]: next }
}

// projectMemberTail projects one closed/failed turn's unconsolidated steps into
// the producer's own view as local placeholders (same semantics as projectTail
// for the merged sequence: a later real team_message frame replaces the first
// placeholder in arrival order; a ListMemberMessages backfill drops them all).
// 投影条目恒不携带 open 标记（同 projectTail）。
function projectMemberTail(
  memberHistory: Record<string, MemberViewEntry[]>,
  member: string,
  messages: HistoryMessage[],
): Record<string, MemberViewEntry[]> {
  const view = memberHistory[member] ?? []
  return {
    ...memberHistory,
    [member]: [
      ...view,
      ...messages.map((message) => ({ message, sender: member, projected: true })),
    ],
  }
}

// clearEntryOpen drops the sparse live open mark from one merged/view entry
// (same reference when unmarked).
function clearEntryOpen<T extends { open?: boolean }>(entry: T): T {
  if (entry.open !== true) return entry
  const next = { ...entry }
  delete next.open
  return next
}

// clearMemberOpen removes the live open mark from every entry of one member
// (merged sequence + that member's own view) on turn close.
function clearMemberOpen(state: ChatState, member: string): ChatState {
  const view = state.memberHistory[member]
  return {
    ...state,
    history: state.history.map((entry) =>
      entry.member === member ? clearEntryOpen(entry) : entry,
    ),
    memberHistory:
      view === undefined
        ? state.memberHistory
        : { ...state.memberHistory, [member]: view.map(clearEntryOpen) },
  }
}

// clearAllMemberOpen is the loadHistory defensive reset: 归并序列重建天然无
// 标记、live 同步复位后标记不再有清除事件来源，成员视角的残留标记必须先
// 清除（specs/064-memory-split-fold-remain/contracts/web-ui.md §3）。
function clearAllMemberOpen(
  memberHistory: Record<string, MemberViewEntry[]>,
): Record<string, MemberViewEntry[]> {
  let changed = false
  const next = Object.fromEntries(
    Object.entries(memberHistory).map(([member, view]) => {
      const cleared = view.map(clearEntryOpen)
      if (cleared.some((entry, i) => entry !== view[i])) changed = true
      return [member, cleared] as const
    }),
  )
  return changed ? next : memberHistory
}

// closeLiveTurn closes a live turn by projecting its unconsolidated tail into
// the merged sequence and the producer's own member view (steps already
// consolidated through team_message frames are already there). Interrupted
// turns mark the tail message so the folding check treats it as a prefix
// (specs/054-agent-v2-bugfixes/data-model.md §5.2). 收束即清除该成员全部条目
// 的 open 标记——清除先于"无尾步可投影"的早退返回，全部步已固化的收束路径
// 同样清除（specs/064-memory-split-fold-remain/contracts/web-ui.md §3）。
function closeLiveTurn(state: ChatState, index: number, interrupted: boolean): ChatState {
  const turn = state.live[index]
  if (turn === undefined) return state
  const live = state.live.filter((_, i) => i !== index)
  const closed = clearMemberOpen(state, turn.member)
  const pending = turn.steps.slice(turn.fixedSteps)
  if (pending.length === 0) return { ...closed, live }
  const messages = stepsToHistory(pending, interrupted)
  return {
    ...closed,
    live,
    history: [...closed.history, ...projectTail(closed.history, turn.member, messages)],
    memberHistory: projectMemberTail(closed.memberHistory, turn.member, messages),
  }
}

// consumeFixedStep advances the first live turn of the member that still has an
// unconsolidated step: a `team_message` frame is the anchor that step N is now
// materialized in the merged sequence, so the live preview drops it.
function consumeFixedStep(live: LiveMemberTurn[], member: string): LiveMemberTurn[] {
  const index = live.findIndex((t) => t.member === member && t.fixedSteps < t.steps.length)
  if (index < 0) return live
  return live.map((t, i) =>
    i === index ? { ...t, fixedSteps: Math.min(t.fixedSteps + 1, t.steps.length) } : t,
  )
}

// reduceEvent is the ChatEvent reducer for one team stream. Member event
// frames route by (member, turn_id); team-level frames (queued / team_message
// / member_view) carry no outer member. streamId identifies the feeding Send
// stream for the delta ownership anchors (并发流去重，team-api.md §3.4).
function reduceEvent(
  state: ChatState,
  event: ChatEvent,
  queuedText = '',
  streamId = 0,
): ChatState {
  if (event.queued) {
    const item: QueuedMsg = { text: queuedText, position: event.queued.position }
    return { ...state, queue: [...state.queue, item] }
  }
  if (event.teamMessage) {
    const frame = event.teamMessage
    const member = frame.member ?? ''
    // Unknown/unset member: forward-compat ignore (empty = unset,
    // team-api.md §3.2).
    if (member === '') return state
    const seq = seqOf(frame.seq)
    // seq 归并锚幂等：并发流重复帧忽略（team-api.md §3.4）。投影占位条目
    // （负 seq）不参与该判定，由下方的替换路径消费。
    if (state.history.some((e) => e.projected !== true && e.seq === seq)) return state
    // live 进行中回合的标记判定（specs/064-memory-split-fold-remain/
    // contracts/web-ui.md §3）：该成员存在打开的 live 回合 → 本帧固化的条目
    // 是进行中回合的已固化前缀，落 open: true 供分组层整组展开。保留值
    // "user" 不是成员回合，恒不标记。
    const open = member !== USER_MEMBER && state.live.some((t) => t.member === member)
    const entry: TeamMessageEntry = { member, message: frame.message, seq }
    const projectedIndex = state.history.findIndex(
      (e) => e.projected === true && e.member === member,
    )
    const history =
      projectedIndex >= 0
        ? // projected 替换路径不落标记：占位条目由 closeLiveTurn 产生、其回合
          // 已收束，迟到帧不得因该成员新回合的 live 被误标 open（契约 §3/§4）。
          state.history.map((e, i) => (i === projectedIndex ? entry : e))
        : insertBySeq(state.history, open ? { ...entry, open: true } : entry)
    // 成员自身输出双写进其视角（服务端 appendMemberOutput 的同一双投影）；
    // 用户消息与跨成员广播注入在被该成员消费时进入其视角——消费锚只有
    // 服务端历史可见，前端不伪造，经 loadMemberHistory 回填
    // （web-views.md §2「经回填呈现」）。
    const memberHistory =
      member === USER_MEMBER
        ? state.memberHistory
        : appendMemberView(state.memberHistory, member, frame.message, open)
    return {
      ...state,
      history,
      memberHistory,
      live: consumeFixedStep(state.live, member),
    }
  }
  if (event.memberView) {
    // 成员消费输入的实时通知（specs/060-agent-v2-team-optimize/contracts/
    // team-api.md §2）：追加进该成员视角；messageId 幂等（并发流重复帧按锚
    // 忽略）。归并序列/live/queue 零改动——消费事实只属于成员视角，未被消费
    // 成员的视角不因用户消息写入（消费前不出现语义保持）。
    const frame = event.memberView
    const member = frame.member ?? ''
    // 保留值 "user" 是归并序列的用户标签、不是成员 role：畸形帧不得创建
    // memberHistory["user"] 视角（与 team_message 分支同型守卫，
    // team-api.md §3.2）。
    if (member === '' || member === USER_MEMBER) return state
    const memberHistory = appendMemberInputView(
      state.memberHistory,
      member,
      frame.message,
      frame.sender ?? '',
    )
    return memberHistory === state.memberHistory ? state : { ...state, memberHistory }
  }
  if (event.turnStart) {
    const member = eventMember(state, event)
    if (member === undefined) return state
    const turnId = event.turnId ?? ''
    // 重复 turn_start（并发流扇出）幂等：不清空已累积的草稿。
    if (findLiveTurn(state, member, turnId) >= 0) return state
    // 本消息 queue 项转为 live（排队指示消除）：队首即下一回合被驱动时消
    // 化（FR-011 排队消息由当前激活成员处理，team 流覆盖其消化回合）。
    const [, ...rest] = state.queue
    return {
      ...state,
      queue: rest,
      live: [...state.live, { member, turnId, steps: [], fixedSteps: 0, owners: {} }],
      error: null,
      canceled: false,
    }
  }
  if (event.blockStart) {
    const member = eventMember(state, event)
    const index = findLiveTurn(state, member, event.turnId ?? '')
    if (index < 0) return state
    const step = eventStep(event.blockStart.step)
    const key = blockKey(step, event.blockStart.index)
    const draft = blockStartDraft(event.blockStart)
    return {
      ...state,
      live: state.live.map((t, i) =>
        i === index
          ? {
              ...t,
              steps: appendBlockStart(t.steps, step, draft),
              owners: claimBlock(t.owners, key, streamId),
            }
          : t,
      ),
    }
  }
  if (event.delta) {
    const member = eventMember(state, event)
    const index = findLiveTurn(state, member, event.turnId ?? '')
    if (index < 0) return state
    const turn = state.live[index]
    if (turn === undefined) return state
    const step = eventStep(event.delta.step)
    const { index: blockIndex, text } = event.delta
    const key = blockKey(step, blockIndex)
    const owner = turn.owners[key]
    // 并发流重复帧按块归属去重：同一 block 的 delta 只由其 owner 流应用一次
    // （team-api.md §3.4；delta 无帧内标识，块归属即增量锚）。
    if (owner !== undefined && owner !== streamId) return state
    return {
      ...state,
      live: state.live.map((t, i) =>
        i === index
          ? {
              ...t,
              steps: mapStepBlocks(t.steps, step, (blocks) =>
                blocks.map((b) => {
                  if (b.index !== blockIndex) return b
                  if (b.type === 'TOOL_CALL') return { ...b, args: b.args + text }
                  return { ...b, text: b.text + text }
                }),
              ),
              owners: claimBlock(t.owners, key, streamId),
            }
          : t,
      ),
    }
  }
  if (event.blockEnd) {
    const member = eventMember(state, event)
    const index = findLiveTurn(state, member, event.turnId ?? '')
    if (index < 0) return state
    const step = eventStep(event.blockEnd.step)
    const { index: blockIndex, block } = event.blockEnd
    const key = blockKey(step, blockIndex)
    return {
      ...state,
      live: state.live.map((t, i) =>
        i === index
          ? {
              ...t,
              steps: mapStepBlocks(t.steps, step, (blocks) =>
                blocks.map((b) => (b.index === blockIndex ? blockEndTerminal(b, block) : b)),
              ),
              owners: claimBlock(t.owners, key, streamId),
            }
          : t,
      ),
    }
  }
  if (event.toolResult) {
    // 工具结果终态化（specs/060-agent-v2-team-optimize/contracts/team-api.md
    // §3）：一次归约内跨三个投影面幂等 settle——live 草稿、归并序列条目、
    // 成员视角条目。各面按 tool_id + TOOL_STATUS_RUNNING 匹配，已终态块不
    // 命中，天然幂等并去重并发流的重复帧（team-api.md §3.4）。059 的渲染枢
    // 轴把已固化 step 的可见副本移到归并序列/成员视角（live 仅留 fixedSteps
    // 草稿），故不在 live 命中后短路——已固化条目同样必须终态化
    // （specs/060-agent-v2-team-optimize/research.md R6）。
    const { toolId, status, result } = event.toolResult
    let changed = false
    const live = state.live.map((turn) => {
      let turnChanged = false
      const steps = turn.steps.map((s) => {
        let stepChanged = false
        const blocks = s.blocks.map((b) => {
          const settled = settleDraft(b, toolId, status, result)
          stepChanged = stepChanged || settled !== b
          return settled
        })
        turnChanged = turnChanged || stepChanged
        return stepChanged ? { ...s, blocks } : s
      })
      if (!turnChanged) return turn
      changed = true
      return { ...turn, steps }
    })
    const history = state.history.map((entry) => {
      const settled = settleHistoryEntry(entry, toolId, status, result)
      changed = changed || settled !== entry
      return settled
    })
    let memberHistory = state.memberHistory
    for (const member of Object.keys(memberHistory)) {
      memberHistory = settleMemberHistory(memberHistory, member, toolId, status, result)
    }
    changed = changed || memberHistory !== state.memberHistory
    return changed ? { ...state, live, history, memberHistory } : state
  }
  if (event.turnEnd) {
    const member = eventMember(state, event)
    const index = findLiveTurn(state, member, event.turnId ?? '')
    switch (event.turnEnd.status) {
      case 'TURN_STATUS_COMPLETED':
        // 已固化的 step 已经由 team_message 帧进入归并序列；未固化尾步投影
        // 为本地占位条目（帧丢失兜底，帧到达后按序替换），回合移除。
        return index < 0 ? state : closeLiveTurn(state, index, false)
      case 'TURN_STATUS_ERROR': {
        // 失败回合保留已呈现内容（specs/054-agent-v2-bugfixes/data-model.md
        // §5.2）：未固化尾步以 interrupted 投影并入归并序列（不清空、不原
        // 地消失），错误提示独立、不吞内容（FR-013）；无 live 的失败（如
        // 首帧即 turn_end）只设置错误。
        const error = event.turnEnd.error?.message ?? '对话回合失败'
        if (index < 0) return { ...state, error }
        return { ...closeLiveTurn(state, index, true), error }
      }
      case 'TURN_STATUS_CANCELED': {
        // 用户终止（specs/054-agent-v2-bugfixes/data-model.md §5.2）：保留
        // 已呈现内容、终态标识为 canceled（"已终止"，非错误文案——web-ui.md
        // §4）。服务端 :cancel 同时作废 team 排队队列：排队 chip 随终态移除
        // （排队 user 消息已在其 team_message{USER} 帧时入归并序列）。
        const next = index < 0 ? state : closeLiveTurn(state, index, true)
        return { ...next, queue: [], canceled: true }
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
// subscribe via useChatState; send() consumes one Send stream (the team stream
// runs until the team goes quiescent) and feeds its events through the
// reducer.
export class ChatStore {
  private state: ChatState = EMPTY_STATE
  private listeners: (() => void)[] = []
  // sendTexts holds per-stream user texts in FIFO order: the queued event
  // carries only a position (team-api.md §3), the text is known locally at
  // send time; each stream consumes exactly one entry at its first frame
  // (queued receipt or stream-established frame), see send().
  private sendTexts: string[] = []
  // nextStreamId mints the per-send stream identity used as the delta block
  // ownership key；activeStreamId is the newest stream: concurrent streams all
  // feed the store (每流完整扇出，team-api.md §3.4), deltas are deduped by
  // block ownership while only the newest stream owns the terminal/error
  // transitions (close/error 不因被取代的旧流而触发).
  private nextStreamId = 0
  private activeStreamId = 0

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

  private apply(event: ChatEvent, queuedText?: string, streamId = 0): void {
    this.setState(reduceEvent(this.state, event, queuedText, streamId))
  }

  // applyEvent 是事件注入面（组件/测试直接喂帧）：所有帧同属一个外部流
  // 身份（streamId 0），块归属退化为单源，行为与单流一致。
  applyEvent(event: ChatEvent): void {
    this.apply(event)
  }

  // loadHistory rebuilds the state from a ListTeamMessages backfill
  // (FR-014 回填，web-views.md §2「刷新/切换会话 → 全量重建」)：条目按 seq
  // 重排（与 team_message 帧同源锚），live/queue/error 复位；流断开后的重新
  // 对齐即经此路径（threads 本地草稿被服务端归并序列取代）。
  loadHistory(messages: TeamMessage[]): void {
    const history: TeamMessageEntry[] = []
    for (const m of messages) {
      const member = m.member ?? ''
      // 空 member = 未设置（team-api.md §3.2）：无法归属的条目忽略。
      if (member === '') continue
      history.push({ member, message: m.message, seq: seqOf(m.seq) })
    }
    history.sort((a, b) => a.seq - b.seq)
    // 团队归并序列重建不触碰成员视角序列：同一生命周期的重对齐（流断开
    // 回填）保留成员视角已回填内容；刷新（新生命周期）由调用方经
    // clearMemberHistory 显式复位后重新回填。live 同步复位使 open 标记不再
    // 有清除事件来源，故此处防御性清除成员视角的残留标记
    // （specs/064-memory-split-fold-remain/contracts/web-ui.md §3）。
    this.setState({
      history,
      memberHistory: clearAllMemberOpen(this.state.memberHistory),
      live: [],
      queue: [],
      error: null,
      canceled: false,
    })
  }

  // loadMemberHistory applies one ListMemberMessages backfill (web-views.md §2):
  // the server sequence is authoritative (consumption order with sender
  // annotations). Local entries absent from the response are kept — an own
  // output consolidated after the request was issued (structural continuation
  // can start before the response lands) must not vanish; projected
  // placeholders are dropped because the response is their authoritative
  // replacement. Server order is preserved (no local seq anchor exists for a
  // member view).
  loadMemberHistory(
    member: string,
    messages: ReadonlyArray<{ message: HistoryMessage; sender?: string }>,
  ): void {
    const server: MemberViewEntry[] = messages.map((m) => ({
      message: m.message,
      sender: m.sender ?? '',
    }))
    const serverIds = new Set(
      server
        .map((e) => e.message.messageId)
        .filter((id): id is string => id !== undefined && id !== ''),
    )
    const local = (this.state.memberHistory[member] ?? []).filter(
      (e) =>
        e.projected !== true &&
        (e.message.messageId === undefined ||
          e.message.messageId === '' ||
          !serverIds.has(e.message.messageId)),
    )
    const next = [...server, ...local]
    this.setState({
      ...this.state,
      memberHistory: { ...this.state.memberHistory, [member]: next },
    })
  }

  // clearMemberHistory resets every member-view sequence (refresh/rebuild
  // lifecycle, team-api.md §1/§5): the caller re-backfills afterwards.
  clearMemberHistory(): void {
    if (Object.keys(this.state.memberHistory).length === 0) return
    this.setState({ ...this.state, memberHistory: {} })
  }

  // releaseStreamOwnership drops one terminated stream's delta block
  // ownership so the surviving streams continue the blocks seamlessly
  // (已应用的前缀保留在草稿里，存活流的下一个 delta 从当前进度续接——无重复
  // 也无缺口).
  private releaseStreamOwnership(streamId: number): void {
    let changed = false
    const live = this.state.live.map((turn) => {
      const owners = Object.fromEntries(
        Object.entries(turn.owners).filter(([, id]) => id !== streamId),
      )
      if (Object.keys(owners).length === Object.keys(turn.owners).length) return turn
      changed = true
      return { ...turn, owners }
    })
    if (changed) this.setState({ ...this.state, live })
  }

  // send consumes one team stream: the user text feeds the queued indicator,
  // every event flows through the reducer. Each stream consumes its text from
  // the FIFO exactly once — at its first frame (queued receipt for a busy
  // member, or the stream-established frame for a direct turn) — so a direct
  // turn leaves no stale entry that a later queued chip could pick up; a
  // stream failing before the first frame (request-level failure, team-api.md
  // §3) cleans its residual. The user message itself enters the merged
  // sequence via its `team_message{member=USER}` frame (enqueue 即固化，
  // team-api.md §3), so no local echo is added here. Concurrent Send streams
  // (V6 排队场景：流 A 存续期间再 Send 建流 B) all keep feeding the store;
  // per-block delta ownership makes the duplicate fan-out exactly-once
  // (team-api.md §3.4). A superseded stream terminating does not close/project
  // the shared drafts and does not surface an error — it only releases its
  // block ownership; the newest stream's termination projects the streamed
  // tail into the merged sequence and surfaces its error; the next backfill
  // realigns with the server.
  async send(text: string, stream: AsyncIterable<ChatEvent>): Promise<void> {
    const streamId = ++this.nextStreamId
    this.activeStreamId = streamId
    let consumed = false
    const consumeSendText = (): string => {
      consumed = true
      const [head, ...rest] = this.sendTexts
      this.sendTexts = rest
      return head ?? ''
    }
    this.sendTexts.push(text)
    const isActive = (): boolean => this.activeStreamId === streamId
    try {
      for await (const event of stream) {
        if (event.queued) {
          const queuedText = consumeSendText()
          this.apply(event, queuedText, streamId)
          continue
        }
        if (!consumed) consumeSendText()
        this.apply(event, undefined, streamId)
      }
      // Stream ended normally (team quiescence, team-api.md §3.1): no in-flight
      // turn can remain by contract, but a stream truncated without an error
      // must not leave drafts rendering as running forever — the newest stream
      // projects them (content stays visible until the next backfill); a
      // superseded stream only releases its block ownership.
      if (!consumed) consumeSendText()
      if (isActive()) this.closePendingTurns(true)
      else this.releaseStreamOwnership(streamId)
    } catch (err) {
      if (!consumed) consumeSendText()
      if (!isActive()) {
        this.releaseStreamOwnership(streamId)
        return
      }
      this.closePendingTurns(true)
      this.setState({
        ...this.state,
        error: err instanceof Error ? err.message : String(err),
      })
    }
  }

  // closePendingTurns projects every still-running live draft into the merged
  // sequence as a local placeholder (interrupted tail semantics), keeping the
  // streamed content visible when the stream ends without the turn_end /
  // team_message pair. The next ListTeamMessages backfill replaces projections
  // with the server's authoritative sequence.
  private closePendingTurns(interrupted: boolean): void {
    let state = this.state
    const live = [...state.live]
    for (const turn of live) {
      const index = state.live.findIndex((t) => t.member === turn.member && t.turnId === turn.turnId)
      if (index >= 0) state = closeLiveTurn(state, index, interrupted)
    }
    if (state !== this.state) this.setState(state)
  }
}

export function useChatState(store: ChatStore): ChatState {
  return useSyncExternalStore(store.subscribe, store.getSnapshot)
}
