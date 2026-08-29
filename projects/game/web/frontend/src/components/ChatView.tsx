// 对话主区：历史回填 + 实时流合并渲染 + 排队指示 + 发送输入
// （行为基线 desktop ChatView，契约 specs/049-agent-v2-dsh-init/contracts/
// web-frontend.md §3.2）。Agent 消息按块类型分类呈现：THINK → ReasoningRow
// （running 态由 live 流/历史回填区分）、TEXT → MessageText，两者不混排；
// TOOL_CALL 渲染分支由 Phase 5（T027）加入，事件模型已在 store 层归类。
import { useEffect, useRef, useState } from 'react'
import { Button, Input, MessageText } from '@deepseek-ai/dsh-client-ui-primitives'
import type { ContentBlock, HistoryMessage } from '../api/conversation.js'
import type { BlockDraft, LiveTurn, QueuedMsg } from '../store/chat.js'
import { ReasoningRow } from './ReasoningRow.js'

export interface ChatViewProps {
  session: string
  history: HistoryMessage[]
  live: LiveTurn | null
  queue: QueuedMsg[]
  error: string | null
  onSend: (text: string) => void
}

// blockText projects the TEXT content out of either block shape (history
// protojson ContentBlock / live BlockDraft); THINK blocks render through
// ReasoningRow and TOOL_CALL blocks through the Phase 5 (T027) card, so
// neither contributes body text here.
function blockText(b: ContentBlock | BlockDraft): string | undefined {
  if ('type' in b) return b.type === 'TEXT' ? b.text : undefined
  return b.text?.content
}

// blockThink projects the THINK content out of either block shape.
function blockThink(b: ContentBlock | BlockDraft): string | undefined {
  if ('type' in b) return b.type === 'THINK' ? b.text : undefined
  return b.think?.content
}

// AgentBlocks renders one agent message's blocks in order: THINK →
// ReasoningRow、TEXT → MessageText （web-frontend.md §2: 分类呈现不混排）。
// streaming running 只落在 live 回合的尾块上——流式块按序 append 恒为尾块，
// 已终结的 THINK 块（其后还有 TEXT 在流式）因此呈现完成态摘要。
function AgentBlocks({
  blocks,
  running,
}: {
  blocks: (ContentBlock | BlockDraft)[]
  running: boolean
}) {
  return (
    <div className="msg-agent">
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
        const text = blockText(b)
        if (text === undefined || text.trim() === '') return null
        return (
          <div key={i} data-testid="agent-text">
            <MessageText text={text} />
          </div>
        )
      })}
    </div>
  )
}

export function ChatView({
  session,
  history,
  live,
  queue,
  error,
  onSend,
}: ChatViewProps) {
  const [draft, setDraft] = useState('')
  const messagesRef = useRef<HTMLDivElement | null>(null)

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

  return (
    <div className="chat">
      <div className="chat-messages" data-testid="chat-messages" ref={messagesRef}>
        {history.map((m, i) =>
          m.role === 'ROLE_USER' ? (
            <div key={i} className="msg-user">
              {m.blocks.map((b, j) => (
                <span key={j}>{b.text?.content ?? ''}</span>
              ))}
            </div>
          ) : (
            <AgentBlocks key={i} blocks={m.blocks} running={false} />
          ),
        )}
        {live !== null && <AgentBlocks blocks={live.blocks} running />}
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
