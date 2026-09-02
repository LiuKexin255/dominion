import { describe, expect, it } from 'vitest'
import type { ChatEvent, HistoryMessage } from '../api/conversation.js'
import { ChatStore } from './chat.js'

function turnTextEvents(turnId: string, deltas: string[]): ChatEvent[] {
  return [
    { turnId, turnStart: {} },
    { turnId, blockStart: { index: 0, type: 'BLOCK_TYPE_TEXT' } },
    ...deltas.map((text): ChatEvent => ({ turnId, delta: { index: 0, text } })),
    {
      turnId,
      blockEnd: { index: 0, block: { text: { content: deltas.join('') } } },
    },
  ]
}

async function* eventsOf(events: ChatEvent[]): AsyncGenerator<ChatEvent> {
  for (const e of events) yield e
}

// streamThenDrop yields the given events and then fails with a transport
// error — a stream that never delivers turn_end (conversation-api.md §2:
// 正常路径流尾即 turn_end).
async function* streamThenDrop(events: ChatEvent[]): AsyncGenerator<ChatEvent> {
  for (const e of events) yield e
  throw new Error('transport dropped')
}

describe('ChatStore reducer', () => {
  it('reduces queued → turn_start → deltas → turn_end{COMPLETED} into a merged history entry', () => {
    const store = new ChatStore()
    for (const e of [
      { queued: { position: 1 } },
      ...turnTextEvents('t1', ['你', '好']),
      {
        turnId: 't1',
        turnEnd: {
          status: 'TURN_STATUS_COMPLETED',
          usage: { inputTokens: '10', outputTokens: '2' },
        },
      } satisfies ChatEvent,
    ]) {
      store.applyEvent(e)
    }

    const s = store.getSnapshot()
    expect(s.queue).toEqual([])
    expect(s.live).toBeNull()
    expect(s.error).toBeNull()
    expect(s.history).toEqual([
      { role: 'ROLE_AGENT', blocks: [{ text: { content: '你好' } }] },
    ])
  })

  it('keeps THINK and TEXT drafts classified and ordered within one turn', () => {
    const store = new ChatStore()
    const events: ChatEvent[] = [
      { turnId: 't1', turnStart: {} },
      { turnId: 't1', blockStart: { index: 0, type: 'BLOCK_TYPE_THINK' } },
      { turnId: 't1', delta: { index: 0, text: '推理' } },
      { turnId: 't1', blockStart: { index: 1, type: 'BLOCK_TYPE_TEXT' } },
      { turnId: 't1', delta: { index: 1, text: '正文' } },
      {
        turnId: 't1',
        blockEnd: { index: 0, block: { think: { content: '推理' } } },
      },
      {
        turnId: 't1',
        blockEnd: { index: 1, block: { text: { content: '正文' } } },
      },
      { turnId: 't1', turnEnd: { status: 'TURN_STATUS_COMPLETED' } },
    ]
    for (const e of events) {
      store.applyEvent(e)
    }

    expect(store.getSnapshot().history).toEqual([
      {
        role: 'ROLE_AGENT',
        blocks: [
          { think: { content: '推理' } },
          { text: { content: '正文' } },
        ],
      },
    ])
  })

  it('reduces a TOOL_CALL block: delta-concatenated args, terminal overlay with RUNNING→SUCCEEDED, merged into history', () => {
    const store = new ChatStore()
    store.applyEvent({ turnId: 't1', turnStart: {} })
    store.applyEvent({
      turnId: 't1',
      blockStart: {
        index: 0,
        type: 'BLOCK_TYPE_TOOL_CALL',
        toolId: 'call-1',
        name: 'bash',
      },
    })

    // block_start opens a RUNNING draft; deltas stream-concatenate args.
    expect(store.getSnapshot().live?.blocks).toEqual([
      {
        index: 0,
        type: 'TOOL_CALL',
        toolId: 'call-1',
        name: 'bash',
        args: '',
        status: 'TOOL_STATUS_RUNNING',
      },
    ])
    store.applyEvent({ turnId: 't1', delta: { index: 0, text: '{"command":"ls' } })
    store.applyEvent({ turnId: 't1', delta: { index: 0, text: ' -la"}' } })
    expect(store.getSnapshot().live?.blocks[0]).toMatchObject({
      args: '{"command":"ls -la"}',
      status: 'TOOL_STATUS_RUNNING',
    })

    // block_end overlays the terminal block: status transition + result.
    store.applyEvent({
      turnId: 't1',
      blockEnd: {
        index: 0,
        block: {
          toolCall: {
            toolId: 'call-1',
            name: 'bash',
            argsJson: '{"command":"ls -la"}',
            status: 'TOOL_STATUS_SUCCEEDED',
            result: 'file-a.txt',
          },
        },
      },
    })
    expect(store.getSnapshot().live?.blocks).toEqual([
      {
        index: 0,
        type: 'TOOL_CALL',
        toolId: 'call-1',
        name: 'bash',
        args: '{"command":"ls -la"}',
        status: 'TOOL_STATUS_SUCCEEDED',
        result: 'file-a.txt',
      },
    ])

    // turn_end{COMPLETED} merges the turn into history as the protojson
    // ContentBlock projection (与回填历史同一渲染路径，FR-014).
    store.applyEvent({ turnId: 't1', turnEnd: { status: 'TURN_STATUS_COMPLETED' } })
    expect(store.getSnapshot().history).toEqual([
      {
        role: 'ROLE_AGENT',
        blocks: [
          {
            toolCall: {
              toolId: 'call-1',
              name: 'bash',
              argsJson: '{"command":"ls -la"}',
              status: 'TOOL_STATUS_SUCCEEDED',
              result: 'file-a.txt',
            },
          },
        ],
      },
    ])
    expect(store.getSnapshot().live).toBeNull()
  })

  it('tool_result settles the live RUNNING block by tool_id (status + result)', () => {
    const store = new ChatStore()
    store.applyEvent({ turnId: 't1', turnStart: {} })
    store.applyEvent({
      turnId: 't1',
      blockStart: {
        index: 0,
        type: 'BLOCK_TYPE_TOOL_CALL',
        toolId: 'call-1',
        name: 'saolei_init',
      },
    })
    store.applyEvent({ turnId: 't1', delta: { index: 0, text: '{}' } })

    store.applyEvent({
      turnId: 't1',
      toolResult: {
        toolId: 'call-1',
        status: 'TOOL_STATUS_SUCCEEDED',
        result: 'new game started\ngame status: playing',
      },
    })

    // RUNNING → SUCCEEDED with the rendered result; args stay intact.
    expect(store.getSnapshot().live?.blocks[0]).toEqual({
      index: 0,
      type: 'TOOL_CALL',
      toolId: 'call-1',
      name: 'saolei_init',
      args: '{}',
      status: 'TOOL_STATUS_SUCCEEDED',
      result: 'new game started\ngame status: playing',
    })
    expect(store.getSnapshot().history).toEqual([])
  })

  it('tool_result FAILED settles the live block with the error text', () => {
    const store = new ChatStore()
    store.applyEvent({ turnId: 't1', turnStart: {} })
    store.applyEvent({
      turnId: 't1',
      blockStart: {
        index: 0,
        type: 'BLOCK_TYPE_TOOL_CALL',
        toolId: 'call-2',
        name: 'saolei_operate',
      },
    })

    store.applyEvent({
      turnId: 't1',
      toolResult: {
        toolId: 'call-2',
        status: 'TOOL_STATUS_FAILED',
        result: 'desktop disconnected',
      },
    })

    expect(store.getSnapshot().live?.blocks[0]).toMatchObject({
      status: 'TOOL_STATUS_FAILED',
      result: 'desktop disconnected',
    })
  })

  it('tool_result falls back to the most recent matching RUNNING history block', () => {
    const store = new ChatStore()
    // A turn ended with its tool call still RUNNING (assistant-message
    // projection, data-model.md §2.3): the block lands in history as RUNNING.
    store.applyEvent({ turnId: 't1', turnStart: {} })
    store.applyEvent({
      turnId: 't1',
      blockStart: {
        index: 0,
        type: 'BLOCK_TYPE_TOOL_CALL',
        toolId: 'call-9',
        name: 'saolei_init',
      },
    })
    store.applyEvent({ turnId: 't1', turnEnd: { status: 'TURN_STATUS_COMPLETED' } })
    expect(
      store.getSnapshot().history[0]?.blocks[0],
    ).toMatchObject({ toolCall: { status: 'TOOL_STATUS_RUNNING' } })

    // A later stream (live is null) carries the missing result frame.
    store.applyEvent({
      turnId: 't2',
      toolResult: {
        toolId: 'call-9',
        status: 'TOOL_STATUS_SUCCEEDED',
        result: 'board text',
      },
    })

    const block = store.getSnapshot().history[0]?.blocks[0]
    expect(block).toMatchObject({
      toolCall: { toolId: 'call-9', status: 'TOOL_STATUS_SUCCEEDED', result: 'board text' },
    })
    expect(store.getSnapshot().live).toBeNull()
  })

  it('tool_result with an unknown tool_id is ignored (forward-compat)', () => {
    const store = new ChatStore()
    store.applyEvent({ turnId: 't1', turnStart: {} })
    store.applyEvent({
      turnId: 't1',
      blockStart: {
        index: 0,
        type: 'BLOCK_TYPE_TOOL_CALL',
        toolId: 'call-1',
        name: 'saolei_init',
      },
    })

    store.applyEvent({
      turnId: 't1',
      toolResult: {
        toolId: 'call-unknown',
        status: 'TOOL_STATUS_SUCCEEDED',
        result: 'stale',
      },
    })

    // No state transition: the running block stays RUNNING and untouched.
    expect(store.getSnapshot().live?.blocks[0]).toEqual({
      index: 0,
      type: 'TOOL_CALL',
      toolId: 'call-1',
      name: 'saolei_init',
      args: '',
      status: 'TOOL_STATUS_RUNNING',
    })
  })

  it('tool_result does not re-settle an already terminal block', () => {
    const store = new ChatStore()
    store.applyEvent({ turnId: 't1', turnStart: {} })
    store.applyEvent({
      turnId: 't1',
      blockStart: {
        index: 0,
        type: 'BLOCK_TYPE_TOOL_CALL',
        toolId: 'call-1',
        name: 'bash',
      },
    })
    store.applyEvent({
      turnId: 't1',
      toolResult: {
        toolId: 'call-1',
        status: 'TOOL_STATUS_SUCCEEDED',
        result: 'first',
      },
    })
    store.applyEvent({
      turnId: 't1',
      toolResult: {
        toolId: 'call-1',
        status: 'TOOL_STATUS_FAILED',
        result: 'second',
      },
    })

    // The first terminal status wins; the late duplicate is ignored.
    expect(store.getSnapshot().live?.blocks[0]).toMatchObject({
      status: 'TOOL_STATUS_SUCCEEDED',
      result: 'first',
    })
  })

  it('turn_end{ERROR} surfaces the error, keeps the session usable', () => {
    const store = new ChatStore()
    store.applyEvent({ turnId: 't1', turnStart: {} })
    store.applyEvent({
      turnId: 't1',
      blockStart: { index: 0, type: 'BLOCK_TYPE_TEXT' },
    })
    store.applyEvent({ turnId: 't1', delta: { index: 0, text: '部分' } })
    store.applyEvent({
      turnId: 't1',
      turnEnd: {
        status: 'TURN_STATUS_ERROR',
        error: { code: 'LLM_UPSTREAM', message: '模型端点不可达' },
      },
    })

    const s = store.getSnapshot()
    expect(s.error).toBe('模型端点不可达')
    expect(s.live).toBeNull()
    expect(s.history).toEqual([])
  })

  it('turn_end{ABORTED} clears the whole session state', () => {
    const store = new ChatStore()
    store.applyEvent({ queued: { position: 2 } })
    store.applyEvent({ turnId: 't1', turnStart: {} })
    store.loadHistory([{ role: 'ROLE_USER', blocks: [{ text: { content: 'hi' } }] }])

    store.applyEvent({ turnId: 't9', turnEnd: { status: 'TURN_STATUS_ABORTED' } })

    expect(store.getSnapshot()).toEqual({
      history: [],
      live: null,
      queue: [],
      error: null,
    })
  })

  it('loadHistory rebuilds the state from a List backfill', () => {
    const store = new ChatStore()
    store.applyEvent({ queued: { position: 1 } })
    store.applyEvent({ turnId: 't1', turnStart: {} })
    const backfill: HistoryMessage[] = [
      { role: 'ROLE_USER', blocks: [{ text: { content: 'M1' } }] },
      { role: 'ROLE_AGENT', blocks: [{ text: { content: 'R1' } }] },
    ]

    store.loadHistory(backfill)

    const s = store.getSnapshot()
    expect(s.history).toEqual(backfill)
    expect(s.live).toBeNull()
    expect(s.queue).toEqual([])
    expect(s.error).toBeNull()
  })

  it('send attaches the local text to the queued indicator and merges the turn on completion', async () => {
    const store = new ChatStore()
    const stream = eventsOf([
      { queued: { position: 1 } },
      ...turnTextEvents('t1', ['回', '复']),
      { turnId: 't1', turnEnd: { status: 'TURN_STATUS_COMPLETED' } },
    ])

    await store.send('第一条', stream)

    // The queue drained on turn_start; the user message was recorded at
    // enqueue time and the completed turn merged after it.
    expect(store.getSnapshot().queue).toEqual([])
    expect(store.getSnapshot().history).toEqual([
      { role: 'ROLE_USER', blocks: [{ text: { content: '第一条' } }] },
      { role: 'ROLE_AGENT', blocks: [{ text: { content: '回复' } }] },
    ])
  })

  it('send does not record the user message when the stream never opens', async () => {
    const store = new ChatStore()

    // Request-level failure (stream not opened, conversation-api.md §2): the
    // server never recorded the message, so history stays untouched.
    await store.send('hi', streamThenDrop([]))

    expect(store.getSnapshot().history).toEqual([])
    expect(store.getSnapshot().error).toBe('transport dropped')
  })

  it('consumes each stream text exactly once: a direct turn must not shift the next queued chip', async () => {
    const store = new ChatStore()
    // Capture every queued-chip text ever rendered (web-frontend.md §4
    // QueuedMsg.text) across both sends.
    const queuedTextsSeen: string[] = []
    store.subscribe(() => {
      for (const q of store.getSnapshot().queue) {
        if (!queuedTextsSeen.includes(q.text)) queuedTextsSeen.push(q.text)
      }
    })

    // Idle session: the stream opens straight with turn_start — no queued
    // frame, so this text must leave the FIFO at the turn_start.
    const directTurn: ChatEvent[] = [
      { turnId: 't1', turnStart: {} },
      { turnId: 't1', blockStart: { index: 0, type: 'BLOCK_TYPE_TEXT' } },
      { turnId: 't1', delta: { index: 0, text: '回复一' } },
      {
        turnId: 't1',
        blockEnd: { index: 0, block: { text: { content: '回复一' } } },
      },
      { turnId: 't1', turnEnd: { status: 'TURN_STATUS_COMPLETED' } },
    ]
    await store.send('第一条', eventsOf(directTurn))

    // Busy session: first frame is queued{1}; the chip must carry THIS send's
    // text, not the unconsumed first one.
    const queuedTurn: ChatEvent[] = [
      { queued: { position: 1 } },
      { turnId: 't2', turnStart: {} },
      { turnId: 't2', turnEnd: { status: 'TURN_STATUS_COMPLETED' } },
    ]
    await store.send('第二条', eventsOf(queuedTurn))

    expect(queuedTextsSeen).toEqual(['第二条'])
  })

  it('a stream failing before its first frame leaves no stale send text', async () => {
    const store = new ChatStore()
    const queuedTextsSeen: string[] = []
    store.subscribe(() => {
      for (const q of store.getSnapshot().queue) {
        if (!queuedTextsSeen.includes(q.text)) queuedTextsSeen.push(q.text)
      }
    })

    // Request-level failure before any frame (conversation-api.md §2): the
    // residual text must be cleaned up, not served to the next queued chip.
    await store.send('失败消息', streamThenDrop([]))
    expect(store.getSnapshot().error).toBe('transport dropped')

    await store.send(
      '后续消息',
      eventsOf([
        { queued: { position: 1 } },
        { turnId: 't1', turnStart: {} },
        { turnId: 't1', turnEnd: { status: 'TURN_STATUS_COMPLETED' } },
      ]),
    )

    expect(queuedTextsSeen).toEqual(['后续消息'])
  })

  it('a stream whose first frame is turn_end{ERROR} leaves no stale send text', async () => {
    const store = new ChatStore()
    const queuedTextsSeen: string[] = []
    store.subscribe(() => {
      for (const q of store.getSnapshot().queue) {
        if (!queuedTextsSeen.includes(q.text)) queuedTextsSeen.push(q.text)
      }
    })

    // Session-create failure: the stream opens straight into turn_end{ERROR}
    // — no queued/turn_start frame ever carried this send's text out of the
    // FIFO (re-materialization race turn_end{ABORTED} first frames share
    // this path).
    await store.send(
      '失败消息',
      eventsOf([
        {
          turnId: 't1',
          turnEnd: {
            status: 'TURN_STATUS_ERROR',
            error: { code: 'SESSION_CREATE', message: '会话创建失败' },
          },
        },
      ]),
    )
    expect(store.getSnapshot().error).toBe('会话创建失败')
    expect(store.getSnapshot().history).toEqual([])

    // The next queued message must carry its own text.
    await store.send(
      '后续消息',
      eventsOf([
        { queued: { position: 1 } },
        { turnId: 't2', turnStart: {} },
        { turnId: 't2', turnEnd: { status: 'TURN_STATUS_COMPLETED' } },
      ]),
    )

    expect(queuedTextsSeen).toEqual(['后续消息'])
  })

  it('send surfaces a stream that dies without turn_end as an error', async () => {
    const store = new ChatStore()

    await store.send('hi', streamThenDrop([]))

    expect(store.getSnapshot().error).toBe('transport dropped')
    expect(store.getSnapshot().live).toBeNull()
  })
})
