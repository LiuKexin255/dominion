// 应用装配：侧栏 session 列表 + 主区对话（web-frontend.md §2 单页双区）。
// 无路由库——视图切换由选中 session 状态驱动；每个选中会话一个 ChatStore
// （key 重挂载隔离），进入会话先经 :history 回填（FR-014）。
import { useCallback, useEffect, useRef, useState } from 'react'
import './theme.css'
import { listHistory, sendStream } from './api/conversation.js'
import { createSession, listSessions } from './api/sessions.js'
import type { Session } from './api/sessions.js'
import { ChatView } from './components/ChatView.js'
import { SessionList } from './components/SessionList.js'
import { ChatStore, useChatState } from './store/chat.js'

// 本阶段新建 session 固定 saolei template（spec 澄清，存量 session 服务零改动；
// 常量口径对齐 desktop 前端 api.ts 的 TEMPLATES）。
const TEMPLATE_SAOLEI = 'saolei'

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// ChatPanel hosts one selected session's store: enter → :history backfill,
// send → Send stream consumed by the store.
function ChatPanel({ session }: { session: string }) {
  const storeRef = useRef<ChatStore | null>(null)
  if (storeRef.current === null) storeRef.current = new ChatStore()
  const store = storeRef.current
  const state = useChatState(store)
  const [backfillError, setBackfillError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    listHistory(session)
      .then((messages) => {
        if (!cancelled) {
          setBackfillError(null)
          store.loadHistory(messages)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setBackfillError(errorMessage(err))
          store.loadHistory([])
        }
      })
    return () => {
      cancelled = true
    }
  }, [session, store])

  const onSend = useCallback(
    (text: string) => {
      void store.send(text, sendStream(session, text))
    },
    [store, session],
  )

  return (
    <ChatView
      session={session}
      history={state.history}
      live={state.live}
      queue={state.queue}
      error={state.error ?? backfillError}
      onSend={onSend}
    />
  )
}

export function App() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)
  const [listError, setListError] = useState<string | null>(null)
  const [selected, setSelected] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      setSessions(await listSessions(TEMPLATE_SAOLEI))
      setListError(null)
    } catch (err) {
      setListError(errorMessage(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const onCreate = useCallback(async () => {
    try {
      const created = await createSession(TEMPLATE_SAOLEI)
      setSessions((prev) => [...prev, created])
      setSelected(created.name)
      setListError(null)
    } catch (err) {
      setListError(errorMessage(err))
    }
  }, [])

  return (
    <div className="app">
      <aside className="sidebar">
        <SessionList
          sessions={sessions}
          selected={selected}
          loading={loading}
          error={listError}
          onSelect={setSelected}
          onCreate={() => {
            void onCreate()
          }}
        />
      </aside>
      <main className="main">
        {selected === null ? (
          <div className="empty-hint">选择或新建一个 session 开始对话</div>
        ) : (
          <ChatPanel key={selected} session={selected} />
        )}
      </main>
    </div>
  )
}
