// ChatStore team 流归约单测（specs/059-agent-v2-team-mode/contracts/
// team-api.md §3 与 contracts/web-views.md §2）：成员事件帧按 (member,
// turn_id) 分组增量渲染、`team_message` 帧 seq 归并锚（同源去重）、排队
// chip、多回合流收敛、流断开后 List 回填对齐、取消/失败终态。Mock 约定照
// style/javascript.md：事件流经 AsyncGenerator 直接注入（无模块拦截）。
import { describe, expect, it } from 'vitest'
import type {
  BlockType,
  ChatEvent,
  HistoryMessage,
  TeamMessage,
  TurnStatus,
} from '../api/conversation.js'
import { ChatStore } from './chat.js'

// 成员 role 为 wire 字符串（场景词汇小写；保留值 "user"=用户消息，
// team-api.md §3.2）；测试用例以大写别名书写、此处归一为 wire 值。
function wireMember(member: string): string {
  return member.toLowerCase()
}

function startTurn(member: string, turnId: string): ChatEvent {
  return { member: wireMember(member), turnId, turnStart: {} }
}

function blockStart(
  member: string,
  turnId: string,
  index: number,
  step: number,
  type: BlockType = 'BLOCK_TYPE_TEXT',
): ChatEvent {
  return { member: wireMember(member), turnId, blockStart: { index, type, step } }
}

function delta(member: string, turnId: string, index: number, text: string, step: number): ChatEvent {
  return { member: wireMember(member), turnId, delta: { index, text, step } }
}

function blockEnd(member: string, turnId: string, index: number, content: string, step: number): ChatEvent {
  return {
    member: wireMember(member),
    turnId,
    blockEnd: { index, block: { text: { content } }, step },
  }
}

function endTurn(member: string, turnId: string, status: TurnStatus = 'TURN_STATUS_COMPLETED'): ChatEvent {
  return { member: wireMember(member), turnId, turnEnd: { status } }
}

function endTurnWithError(member: string, turnId: string, code: string, message: string): ChatEvent {
  return {
    member: wireMember(member),
    turnId,
    turnEnd: { status: 'TURN_STATUS_ERROR', error: { code, message } },
  }
}

function teamMessageEvent(
  member: string,
  seq: number,
  content: string,
  role: 'ROLE_USER' | 'ROLE_AGENT' = 'ROLE_AGENT',
): ChatEvent {
  const message: HistoryMessage =
    role === 'ROLE_USER'
      ? { role, blocks: [{ text: { content } }] }
      : { role, blocks: [{ text: { content } }] }
  // protojson int64：seq 序列化为 JSON 字符串（store 以 seqOf 归一化）。
  return { teamMessage: { member: wireMember(member), message, seq: String(seq) } }
}

// oneTextStep emits the canonical block_start/deltas/block_end sequence for a
// single TEXT step of one member turn.
function oneTextStep(member: string, turnId: string, step: number, text: string): ChatEvent[] {
  return [
    blockStart(member, turnId, step - 1, step),
    delta(member, turnId, step - 1, text, step),
    blockEnd(member, turnId, step - 1, text, step),
  ]
}

async function* eventsOf(events: ChatEvent[]): AsyncGenerator<ChatEvent> {
  for (const e of events) yield e
}

// streamThenDrop yields the given events and then fails with a transport
// error — a team stream that never reaches quiescence (team-api.md §3.4:
// 断开由客户端经 List 回填补齐).
async function* streamThenDrop(events: ChatEvent[]): AsyncGenerator<ChatEvent> {
  for (const e of events) yield e
  throw new Error('transport dropped')
}

const LAST_SEQ = (store: ChatStore): number => {
  const history = store.getSnapshot().history
  return history.length === 0 ? 0 : (history[history.length - 1]?.seq ?? 0)
}

describe('ChatStore team 流归约', () => {
  it('归并 queued → 用户 team_message → planner 回合（成员帧）→ turn_end 为 seq 序历史', () => {
    const store = new ChatStore()
    for (const e of [
      { queued: { position: 1 } },
      teamMessageEvent('USER', 1, '你好', 'ROLE_USER'),
      startTurn('PLANNER', 't1'),
      ...oneTextStep('PLANNER', 't1', 1, '开局策略'),
      teamMessageEvent('PLANNER', 2, '开局策略'),
      endTurn('PLANNER', 't1'),
    ]) {
      store.applyEvent(e)
    }

    const s = store.getSnapshot()
    expect(s.queue).toEqual([])
    expect(s.live).toEqual([])
    expect(s.error).toBeNull()
    expect(s.history).toEqual([
      { member: 'user', message: { role: 'ROLE_USER', blocks: [{ text: { content: '你好' } }] }, seq: 1 },
      {
        member: 'planner',
        message: { role: 'ROLE_AGENT', blocks: [{ text: { content: '开局策略' } }] },
        seq: 2,
      },
    ])
  })

  it('成员事件帧按 (member, turn_id) 分组：两个成员回合的 index/step 空间互不串扰', () => {
    const store = new ChatStore()
    // 串行驱动下回合依次发生；事件交错送达时仍按 (member, turnId) 归组。
    for (const e of [
      startTurn('PLANNER', 't1'),
      blockStart('PLANNER', 't1', 0, 1),
      startTurn('PLAYER', 't2'),
      blockStart('PLAYER', 't2', 0, 1),
      delta('PLANNER', 't1', 0, '策略', 1),
      delta('PLAYER', 't2', 0, '落子', 1),
    ]) {
      store.applyEvent(e)
    }

    const s = store.getSnapshot()
    expect(s.live).toHaveLength(2)
    expect(s.live[0]).toMatchObject({ member: 'planner', turnId: 't1' })
    expect(s.live[0]?.steps[0]?.blocks[0]).toMatchObject({ type: 'TEXT', text: '策略' })
    expect(s.live[1]).toMatchObject({ member: 'player', turnId: 't2' })
    expect(s.live[1]?.steps[0]?.blocks[0]).toMatchObject({ type: 'TEXT', text: '落子' })
  })

  it('team_message 帧按 seq 锚插入（乱序到达仍保持归并序），同 seq 重复帧忽略', () => {
    const store = new ChatStore()
    store.applyEvent(teamMessageEvent('USER', 2, '第二条', 'ROLE_USER'))
    store.applyEvent(teamMessageEvent('USER', 1, '第一条', 'ROLE_USER'))
    store.applyEvent(teamMessageEvent('USER', 3, '第三条', 'ROLE_USER'))
    // 并发流重复扇出：同 seq 忽略（team-api.md §3.4 帧应用幂等）。
    store.applyEvent(teamMessageEvent('USER', 2, '第二条', 'ROLE_USER'))

    expect(store.getSnapshot().history.map((e) => e.seq)).toEqual([1, 2, 3])
    expect(store.getSnapshot().history.map((e) => e.message.blocks[0]?.text?.content)).toEqual([
      '第一条',
      '第二条',
      '第三条',
    ])
  })

  it('team_message 固化锚推进 live 前导 step：固化后不再重复渲染；endLiveTurn 全部固化即移除', () => {
    const store = new ChatStore()
    for (const e of [
      startTurn('PLAYER', 't1'),
      ...oneTextStep('PLAYER', 't1', 1, '第一步'),
      ...oneTextStep('PLAYER', 't1', 2, '第二步'),
      // 第一步在回合内先固化（team-api.md §3.2：回合内逐步落定）。
      teamMessageEvent('PLAYER', 1, '第一步'),
    ]) {
      store.applyEvent(e)
    }
    expect(store.getSnapshot().live[0]?.fixedSteps).toBe(1)
    expect(store.getSnapshot().history).toHaveLength(1)

    // 第二步固化后回合收束：全部 step 已固化 → live 条目移除（内容在归并序列）。
    store.applyEvent(teamMessageEvent('PLAYER', 2, '第二步'))
    store.applyEvent(endTurn('PLAYER', 't1'))
    expect(store.getSnapshot().live).toEqual([])
    expect(store.getSnapshot().history.map((e) => e.seq)).toEqual([1, 2])
  })

  it('turn_end 先于 team_message 到达：已流出尾步投影为本地占位，帧到达后原位替换（丢帧/乱序兜底）', () => {
    const store = new ChatStore()
    for (const e of [
      startTurn('PLANNER', 't1'),
      ...oneTextStep('PLANNER', 't1', 1, '复盘'),
      endTurn('PLANNER', 't1'),
    ]) {
      store.applyEvent(e)
    }

    // 帧未到达：内容不消失，投影为占位条目（负 seq，不参与服务端 seq 去重）。
    const projected = store.getSnapshot().history
    expect(store.getSnapshot().live).toEqual([])
    expect(projected).toHaveLength(1)
    expect(projected[0]?.projected).toBe(true)
    expect(projected[0]?.message.blocks[0]?.text?.content).toBe('复盘')

    // 锚帧到达：原位替换为服务端条目，不产生重复。
    store.applyEvent(teamMessageEvent('PLANNER', 7, '复盘'))
    const history = store.getSnapshot().history
    expect(history).toHaveLength(1)
    expect(history[0]?.projected).toBeUndefined()
    expect(history[0]?.seq).toBe(7)
    expect(history[0]?.message.blocks[0]?.text?.content).toBe('复盘')
  })

  it('queued 帧携带本流文本生成 chip，turn_start 消费队首', () => {
    const store = new ChatStore()
    store.applyEvent({ queued: { position: 1 } })
    expect(store.getSnapshot().queue).toEqual([{ text: '', position: 1 }])

    store.applyEvent(startTurn('PLANNER', 't1'))
    expect(store.getSnapshot().queue).toEqual([])
    expect(store.getSnapshot().live).toHaveLength(1)
  })

  it('多回合持续流收敛：planner 开局 → player 游戏 → planner 复盘各自固化，live 全空', () => {
    const store = new ChatStore()
    for (const e of [
      teamMessageEvent('USER', 1, '开始', 'ROLE_USER'),
      startTurn('PLANNER', 't1'),
      ...oneTextStep('PLANNER', 't1', 1, '策略'),
      teamMessageEvent('PLANNER', 2, '策略'),
      endTurn('PLANNER', 't1'),
      startTurn('PLAYER', 't2'),
      ...oneTextStep('PLAYER', 't2', 1, '开始游戏'),
      teamMessageEvent('PLAYER', 3, '开始游戏'),
      endTurn('PLAYER', 't2'),
      startTurn('PLANNER', 't3'),
      ...oneTextStep('PLANNER', 't3', 1, '复盘'),
      teamMessageEvent('PLANNER', 4, '复盘'),
      endTurn('PLANNER', 't3'),
    ]) {
      store.applyEvent(e)
    }

    const s = store.getSnapshot()
    expect(s.live).toEqual([])
    expect(s.history.map((e) => [e.member, e.seq])).toEqual([
      ['user', 1],
      ['planner', 2],
      ['player', 3],
      ['planner', 4],
    ])
  })

  it('流断开：已流出尾步保持可见且错误呈现；List 回填按 seq 重排替换本地草稿（断开对齐）', () => {
    const store = new ChatStore()
    store.applyEvent(teamMessageEvent('USER', 1, '开始', 'ROLE_USER'))

    // 在途流断开：不抛失已呈现内容。
    store.applyEvent(startTurn('PLAYER', 't2'))
    store.applyEvent(blockStart('PLAYER', 't2', 0, 1))
    store.applyEvent(delta('PLAYER', 't2', 0, '半截输出', 1))
    store.applyEvent(
      endTurnWithError('PLAYER', 't2', 'TRANSPORT', '流断开'),
    )

    expect(store.getSnapshot().error).toBe('流断开')
    // 尾步投影为 interrupted 占位条目，内容不丢。
    expect(store.getSnapshot().live).toEqual([])
    expect(store.getSnapshot().history).toHaveLength(2)
    expect(store.getSnapshot().history[1]).toMatchObject({
      member: 'player',
      projected: true,
      message: { interrupted: true },
    })

    // 回填（ListTeamMessages）：服务端归并序列重建，本地草稿清空。
    const backfill: TeamMessage[] = [
      { member: wireMember('USER'), message: { role: 'ROLE_USER', blocks: [{ text: { content: '开始' } }] }, seq: 1 },
      {
        member: wireMember('PLANNER'),
        message: { role: 'ROLE_AGENT', blocks: [{ text: { content: '策略' } }] },
        seq: 3,
      },
      {
        member: wireMember('PLAYER'),
        message: { role: 'ROLE_AGENT', blocks: [{ text: { content: '落子' } }] },
        seq: 2,
      },
    ]
    store.loadHistory(backfill)

    const s = store.getSnapshot()
    expect(s.live).toEqual([])
    expect(s.error).toBeNull()
    expect(s.history.map((e) => e.seq)).toEqual([1, 2, 3])
    expect(s.history.map((e) => e.member)).toEqual(['user', 'player', 'planner'])
  })

  it('tool_result 按 tool_id 跨成员回合终态化 live 草稿，并回退到归并序列', () => {
    const store = new ChatStore()
    store.applyEvent(startTurn('PLAYER', 't1'))
    store.applyEvent({
      member: wireMember('PLAYER'),
      turnId: 't1',
      blockStart: { index: 0, type: 'BLOCK_TYPE_TOOL_CALL', toolId: 'call-1', name: 'saolei_init' },
    })
    store.applyEvent({
      member: wireMember('PLAYER'),
      turnId: 't1',
      toolResult: { toolId: 'call-1', status: 'TOOL_STATUS_SUCCEEDED', result: 'board' },
    })

    expect(store.getSnapshot().live[0]?.steps[0]?.blocks[0]).toMatchObject({
      type: 'TOOL_CALL',
      status: 'TOOL_STATUS_SUCCEEDED',
      result: 'board',
    })
  })

  it('turn_end{CANCELED} 清空 team 排队 chip 并置"已终止"；下个 turn_start 清除', () => {
    const store = new ChatStore()
    store.applyEvent({ queued: { position: 1 } })
    store.applyEvent(startTurn('PLANNER', 't1'))
    store.applyEvent(teamMessageEvent('USER', 1, '排队消息', 'ROLE_USER'))
    store.applyEvent({ queued: { position: 2 } })
    store.applyEvent(endTurn('PLANNER', 't1', 'TURN_STATUS_CANCELED'))

    const s = store.getSnapshot()
    expect(s.canceled).toBe(true)
    expect(s.queue).toEqual([])
    // 排队 user 消息已在 team_message 帧固化入归并序列。
    expect(s.history.map((e) => e.member)).toEqual(['user'])

    store.applyEvent(startTurn('PLAYER', 't2'))
    expect(store.getSnapshot().canceled).toBe(false)
  })

  it('turn_end{ERROR} 保留已流出尾步（interrupted 投影）且错误独立；无 live 时只设置错误', () => {
    const store = new ChatStore()
    for (const e of [
      startTurn('PLANNER', 't1'),
      ...oneTextStep('PLANNER', 't1', 1, '已完成'),
      blockStart('PLANNER', 't1', 1, 2),
      delta('PLANNER', 't1', 1, '部分输出', 2),
      endTurnWithError('PLANNER', 't1', 'LLM', '流中断'),
    ]) {
      store.applyEvent(e)
    }
    const s = store.getSnapshot()
    expect(s.error).toBe('流中断')
    // 无 team_message 帧时，两 step 均投影入归并序列：前段保持 settled
    // 形态、尾步标记 interrupted（与回填 List 同构——FR-013）。
    expect(s.live).toEqual([])
    expect(s.history).toHaveLength(2)
    expect(s.history[0]?.projected).toBe(true)
    expect(s.history[0]?.message).toEqual({
      role: 'ROLE_AGENT',
      blocks: [{ text: { content: '已完成' } }],
    })
    expect(s.history[1]?.projected).toBe(true)
    expect(s.history[1]?.message).toEqual({
      role: 'ROLE_AGENT',
      blocks: [{ text: { content: '部分输出' } }],
      interrupted: true,
    })

    // 无 live 的错误（首帧即 turn_end{ERROR}）：只设置错误。
    store.loadHistory([])
    store.applyEvent(endTurnWithError('PLANNER', 't9', 'X', '创建失败'))
    expect(store.getSnapshot().error).toBe('创建失败')
    expect(store.getSnapshot().history).toEqual([])
  })

  it('turn_end{ABORTED} 清空全部状态（会话删除）', () => {
    const store = new ChatStore()
    store.applyEvent({ queued: { position: 2 } })
    store.applyEvent(teamMessageEvent('USER', 1, 'hi', 'ROLE_USER'))
    store.applyEvent(startTurn('PLANNER', 't1'))

    store.applyEvent({ member: wireMember('PLANNER'), turnId: 't1', turnEnd: { status: 'TURN_STATUS_ABORTED' } })

    expect(store.getSnapshot()).toEqual({
      history: [],
      live: [],
      queue: [],
      error: null,
      canceled: false,
    })
  })

  it('同流内重复扇出按锚幂等：重复 turn_start/block_start 不重建草稿，重复 team_message 帧不重复条目', () => {
    const store = new ChatStore()
    // applyEvent 的帧同属一个外部流身份（streamId 0）：模拟同一流的帧重放。
    // 结构锚（turn_start/block_start/team_message seq）幂等；同一流内重复的
    // delta 无帧内标识、按流各应用一次（跨流去重由块归属保障，见并发流用例）。
    for (const e of [
      startTurn('PLANNER', 't1'),
      startTurn('PLANNER', 't1'),
      blockStart('PLANNER', 't1', 0, 1),
      blockStart('PLANNER', 't1', 0, 1),
      delta('PLANNER', 't1', 0, '策略', 1),
      delta('PLANNER', 't1', 0, '策略', 1),
      teamMessageEvent('PLANNER', 1, '策略'),
      teamMessageEvent('PLANNER', 1, '策略'),
      endTurn('PLANNER', 't1'),
    ]) {
      store.applyEvent(e)
    }

    expect(store.getSnapshot().live).toEqual([])
    expect(store.getSnapshot().history).toHaveLength(1)
    expect(store.getSnapshot().history[0]?.message.blocks).toHaveLength(1)
  })

  it('projected 占位按成员 FIFO 被真实帧替换（多 step 尾步）', () => {
    const store = new ChatStore()
    for (const e of [
      startTurn('PLAYER', 't1'),
      ...oneTextStep('PLAYER', 't1', 1, '第一步'),
      ...oneTextStep('PLAYER', 't1', 2, '第二步'),
      endTurn('PLAYER', 't1', 'TURN_STATUS_ERROR'),
    ]) {
      store.applyEvent(e)
    }
    expect(store.getSnapshot().history.map((e) => e.projected)).toEqual([true, true])

    store.applyEvent(teamMessageEvent('PLAYER', 5, '第一步'))
    store.applyEvent(teamMessageEvent('PLAYER', 6, '第二步'))
    const history = store.getSnapshot().history
    expect(history).toHaveLength(2)
    expect(history.map((e) => e.projected)).toEqual([undefined, undefined])
    expect(history.map((e) => e.seq)).toEqual([5, 6])
  })

  it('loadHistory 按 seq 重排并忽略空 member 条目', () => {
    const store = new ChatStore()
    store.loadHistory([
      { member: 'planner', message: { role: 'ROLE_AGENT', blocks: [{ text: { content: 'P' } }] }, seq: 2 },
      { member: '', message: { role: 'ROLE_AGENT', blocks: [] }, seq: 9 },
      { member: 'user', message: { role: 'ROLE_USER', blocks: [{ text: { content: 'U' } }] }, seq: 1 },
    ])

    const s = store.getSnapshot()
    expect(s.history.map((e) => e.seq)).toEqual([1, 2])
    expect(s.live).toEqual([])
    expect(s.queue).toEqual([])
    expect(s.error).toBeNull()
  })

  it('send 每个流的文本恰好消费一次：直接回合不残留、排队 chip 携带本条文本', async () => {
    const store = new ChatStore()
    const queuedTextsSeen: string[] = []
    store.subscribe(() => {
      for (const q of store.getSnapshot().queue) {
        if (!queuedTextsSeen.includes(q.text)) queuedTextsSeen.push(q.text)
      }
    })

    // 忙碌成员：首帧 queued{1}，chip 必须携带本条文本。
    await store.send(
      '第一条',
      eventsOf([
        { queued: { position: 1 } },
        teamMessageEvent('USER', 1, '第一条', 'ROLE_USER'),
        startTurn('PLANNER', 't1'),
        ...oneTextStep('PLANNER', 't1', 1, '回复一'),
        teamMessageEvent('PLANNER', 2, '回复一'),
        endTurn('PLANNER', 't1'),
      ]),
    )
    expect(queuedTextsSeen).toEqual(['第一条'])

    // 直接回合：首帧即 team_message{USER}，本条文本被消费。
    await store.send(
      '第二条',
      eventsOf([
        teamMessageEvent('USER', 3, '第二条', 'ROLE_USER'),
        startTurn('PLAYER', 't2'),
        ...oneTextStep('PLAYER', 't2', 1, '回复二'),
        teamMessageEvent('PLAYER', 4, '回复二'),
        endTurn('PLAYER', 't2'),
      ]),
    )

    expect(queuedTextsSeen).toEqual(['第一条'])
    expect(store.getSnapshot().history.map((e) => e.message.blocks[0]?.text?.content)).toEqual([
      '第一条',
      '回复一',
      '第二条',
      '回复二',
    ])
  })

  it('send 在首帧前失败不残留文本，后续排队 chip 使用自身文本', async () => {
    const store = new ChatStore()
    const queuedTextsSeen: string[] = []
    store.subscribe(() => {
      for (const q of store.getSnapshot().queue) {
        if (!queuedTextsSeen.includes(q.text)) queuedTextsSeen.push(q.text)
      }
    })

    await store.send('失败消息', streamThenDrop([]))
    expect(store.getSnapshot().error).toBe('transport dropped')

    await store.send(
      '后续消息',
      eventsOf([
        { queued: { position: 1 } },
        teamMessageEvent('USER', 1, '后续消息', 'ROLE_USER'),
        startTurn('PLANNER', 't1'),
        endTurn('PLANNER', 't1'),
      ]),
    )

    expect(queuedTextsSeen).toEqual(['后续消息'])
  })

  it('send 正常读到流尾（team 静止）：全部回合已固化收束、不误报错误', async () => {
    const store = new ChatStore()
    await store.send(
      'hi',
      eventsOf([
        teamMessageEvent('USER', 1, 'hi', 'ROLE_USER'),
        startTurn('PLANNER', 't1'),
        ...oneTextStep('PLANNER', 't1', 1, '静止前最后一段'),
        teamMessageEvent('PLANNER', 2, '静止前最后一段'),
        endTurn('PLANNER', 't1'),
      ]),
    )

    const s = store.getSnapshot()
    expect(s.error).toBeNull()
    expect(s.live).toEqual([])
    expect(s.history).toHaveLength(2)
    expect(LAST_SEQ(store)).toBe(2)
  })

  it('无 member 标注的块事件降级归属唯一在途回合（旧帧容错）', () => {
    const store = new ChatStore()
    store.applyEvent(startTurn('PLAYER', 't1'))
    for (const e of [
      { turnId: 't1', blockStart: { index: 0, type: 'BLOCK_TYPE_TEXT' } },
      { turnId: 't1', delta: { index: 0, text: '无标注' } },
      { turnId: 't1', blockEnd: { index: 0, block: { text: { content: '无标注' } } } },
    ] as ChatEvent[]) {
      store.applyEvent(e)
    }

    expect(store.getSnapshot().live[0]?.member).toBe('player')
    expect(store.getSnapshot().live[0]?.steps[0]?.blocks[0]).toMatchObject({
      type: 'TEXT',
      text: '无标注',
    })
  })

  // ─── 并发 team 流（V6 排队场景：流 A 存续期间再 Send 建流 B；
  // ─── team-api.md §3.4：多流完整扇出、前端按锚去重） ─────────────────────────

  // StreamFeeder 手动驱动一个打开的 Send 流：测试推帧/结束/失败，store 的
  // send() 循环按序消费。
  class StreamFeeder {
    private queue: ChatEvent[] = []
    private failure: Error | null = null
    private closed = false
    private wake: (() => void) | null = null

    push(event: ChatEvent): void {
      this.queue.push(event)
      this.wake?.()
      this.wake = null
    }

    end(): void {
      this.closed = true
      this.wake?.()
      this.wake = null
    }

    fail(err: Error): void {
      this.failure = err
      this.wake?.()
      this.wake = null
    }

    async *iterate(): AsyncGenerator<ChatEvent> {
      for (;;) {
        if (this.queue.length > 0) {
          yield this.queue.shift() as ChatEvent
          continue
        }
        if (this.failure !== null) throw this.failure
        if (this.closed) return
        await new Promise<void>((resolve) => {
          this.wake = resolve
        })
      }
    }
  }

  // flush 让 store 两条 send 循环的全部待处理微任务落地（推帧后断言确定）。
  async function flush(): Promise<void> {
    await new Promise((resolve) => setTimeout(resolve, 0))
  }

  function liveText(store: ChatStore): string | undefined {
    const block = store.getSnapshot().live[0]?.steps[0]?.blocks[0]
    return block !== undefined && block.type !== 'TOOL_CALL' ? block.text : undefined
  }

  it('并发流 delta 按块归属去重：流 A 存续期间建流 B，重复扇出不翻倍且终态唯一', async () => {
    const store = new ChatStore()
    const a = new StreamFeeder()
    const b = new StreamFeeder()
    const sendA = store.send('一', a.iterate())

    // 流 A 建立并收到首段流式帧。
    a.push(teamMessageEvent('USER', 1, '一', 'ROLE_USER'))
    a.push(startTurn('PLANNER', 't1'))
    a.push(blockStart('PLANNER', 't1', 0, 1))
    a.push(delta('PLANNER', 't1', 0, '你', 1))
    await flush()
    expect(liveText(store)).toBe('你')

    // 流 B（排队路径：用户再 Send）建立，与 A 完整重复扇出同一回合的帧。
    const sendB = store.send('二', b.iterate())
    b.push({ queued: { position: 1 } })
    b.push(teamMessageEvent('USER', 2, '二', 'ROLE_USER'))
    b.push(startTurn('PLANNER', 't1'))
    b.push(blockStart('PLANNER', 't1', 0, 1))
    b.push(delta('PLANNER', 't1', 0, '你', 1))
    await flush()

    // A 是该 block 的 owner（首个处理 block_start 的流）：B 的重复 delta
    // 不追加——修复前此处文本翻倍为「你你」。
    expect(liveText(store)).toBe('你')

    // 两流交错扇出同一后续 delta：仍只应用一次。
    a.push(delta('PLANNER', 't1', 0, '好', 1))
    await flush()
    b.push(delta('PLANNER', 't1', 0, '好', 1))
    await flush()
    expect(liveText(store)).toBe('你好')

    // 收尾：两流各自提交同一固化帧与终态（seq 锚与块 end 覆盖幂等）。
    b.push(blockEnd('PLANNER', 't1', 0, '你好', 1))
    b.push(teamMessageEvent('PLANNER', 3, '你好'))
    b.push(endTurn('PLANNER', 't1'))
    a.push(blockEnd('PLANNER', 't1', 0, '你好', 1))
    a.push(teamMessageEvent('PLANNER', 3, '你好'))
    a.push(endTurn('PLANNER', 't1'))
    a.end()
    b.end()
    await Promise.all([sendA, sendB])

    const s = store.getSnapshot()
    expect(s.live).toEqual([])
    expect(s.error).toBeNull()
    // 归并历史唯一：两条用户消息（enqueue 即固化，team-api.md §3）+ planner
    // 条目；正文不因双流翻倍。
    expect(s.history.map((e) => [e.member, e.seq])).toEqual([
      ['user', 1],
      ['user', 2],
      ['planner', 3],
    ])
    expect(s.history[2]?.message.blocks[0]?.text?.content).toBe('你好')
  })

  it('并发流：被取代的流断开不兜底投影、不报错，存活流无缝续接且固化唯一', async () => {
    const store = new ChatStore()
    const a = new StreamFeeder()
    const sendA = store.send('一', a.iterate())
    a.push(teamMessageEvent('USER', 1, '一', 'ROLE_USER'))
    a.push(startTurn('PLANNER', 't1'))
    a.push(blockStart('PLANNER', 't1', 0, 1))
    a.push(delta('PLANNER', 't1', 0, '部', 1))
    await flush()
    expect(liveText(store)).toBe('部')

    // 流 B 建立（A 被取代）；A 随即断开。
    const b = new StreamFeeder()
    const sendB = store.send('二', b.iterate())
    a.fail(new Error('stream A dropped'))
    await flush()

    // 被取代的流：不触发兜底投影（不产生重复占位条目）、不呈现错误，只释放
    // 它拥有的块归属。
    expect(store.getSnapshot().error).toBeNull()
    expect(store.getSnapshot().live).toHaveLength(1)

    // 存活流从当前进度续接：后续 delta 不重复已应用前缀、也无缺口。
    b.push(delta('PLANNER', 't1', 0, '分', 1))
    b.push(blockEnd('PLANNER', 't1', 0, '部分', 1))
    b.push(teamMessageEvent('PLANNER', 2, '部分'))
    b.push(endTurn('PLANNER', 't1'))
    b.end()
    await Promise.all([sendA, sendB])

    const s = store.getSnapshot()
    expect(s.live).toEqual([])
    expect(s.error).toBeNull()
    // 固化条目唯一（A 未兜底投影，B 的真实固化帧不重复应用）。
    expect(s.history.map((e) => [e.member, e.seq])).toEqual([
      ['user', 1],
      ['planner', 2],
    ])
    expect(s.history[1]?.message.blocks[0]?.text?.content).toBe('部分')
  })
})
