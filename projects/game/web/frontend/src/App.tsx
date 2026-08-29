// 应用装配：侧栏 session 列表 + 主区对话（web-frontend.md §2 单页双区）。
// 无路由库——视图切换由选中 session 状态驱动。每 session 的 ChatStore 以资源名
// 为键存于 App 级 Map：切换/返回列表不丢各自进度（发送中的 Send 流由 store
// 持有、在后台继续归约，fetch 不中断——FR-012/FR-014 前端侧），未选中会话的
// 面板保持挂载仅不渲染，再次进入直接呈现既有状态。
import { useCallback, useEffect, useRef, useState } from 'react'
import './theme.css'
import { disposeSession, listHistory, sendStream } from './api/conversation.js'
import { createSession, deleteSession, listSessions } from './api/sessions.js'
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

// ChatPanel hosts one session's store-backed chat view. The store outlives the
// panel's active state (owned by App's per-session map), so an in-flight turn
// keeps reducing while this panel renders nothing. The :history backfill runs
// once on first entry (FR-014) and must not overwrite a turn that started
// before the backfill response landed.
function ChatPanel({
  session,
  store,
  active,
}: {
  session: string
  store: ChatStore
  active: boolean
}) {
  const state = useChatState(store)
  const [backfillError, setBackfillError] = useState<string | null>(null)
  // 回填发起后本面板是否有 send 开始：send 与回填竞态时整体让位于 send
  // （判据说明见下方 loadHistory 调用处注释）。
  const sentSinceBackfill = useRef(false)

  useEffect(() => {
    sentSinceBackfill.current = false
    let cancelled = false
    listHistory(session)
      .then((messages) => {
        if (cancelled) return
        setBackfillError(null)
        // 让位判据 = 自回填发起后是否有 send 开始，而非响应落地时刻的
        // live/queue 快照——快照判据留有两个竞态窗口：(a) 回合已完成
        // （慢网络下过期历史覆盖已合并入历史的回合）；(b) 服务端已记录
        // 用户消息但客户端首帧未达（回填含该消息，随后首帧
        // acceptUserMessage 再追加 = 重复消息）。send 一经开始，回填整体
        // 让位（FR-012/FR-014 前端侧，切换/返回不丢各自进度）。
        if (!sentSinceBackfill.current) store.loadHistory(messages)
      })
      .catch((err: unknown) => {
        // 仅提示不清状态：回填失败时在途回合与本地消息保持不变，可刷新
        // 重试回填（FR-014 前端侧）。
        if (!cancelled) setBackfillError(errorMessage(err))
      })
    return () => {
      cancelled = true
    }
  }, [session, store])

  const onSend = useCallback(
    (text: string) => {
      sentSinceBackfill.current = true
      void store.send(text, sendStream(session, text))
    },
    [store, session],
  )

  if (!active) return null
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
  // 已打开会话的渲染清单（ChatPanel 常驻挂载、按 active 决定是否渲染）；
  // store 实例注册表见 storesRef（Map 变更不经 React 状态，渲染源以本清单为准）。
  const [opened, setOpened] = useState<string[]>([])
  const [notice, setNotice] = useState<string | null>(null)
  const storesRef = useRef<Map<string, ChatStore> | null>(null)
  if (storesRef.current === null) storesRef.current = new Map()
  // 选中态镜像：删除编排的 await 窗口内用户可能切换会话，回调闭包里的
  // selected 已陈旧，清理决策读取完成时刻的镜像而非闭包值。
  const selectedRef = useRef<string | null>(null)
  useEffect(() => {
    selectedRef.current = selected
  }, [selected])

  const openSession = useCallback((name: string) => {
    const stores = storesRef.current
    if (stores !== null && !stores.has(name)) stores.set(name, new ChatStore())
    setOpened((prev) => (prev.includes(name) ? prev : [...prev, name]))
    setSelected(name)
    setNotice(null)
  }, [])

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
      setListError(null)
      openSession(created.name)
    } catch (err) {
      setListError(errorMessage(err))
    }
  }, [openSession])

  const onDelete = useCallback(
    async (name: string) => {
      setLoading(true)
      try {
        // 删除编排第一跳：/api/v1 元数据删除（specs/049-agent-v2-dsh-init/
        // contracts/web-frontend.md §5，D6——成功才进入第二跳）。
        await deleteSession(name)
      } catch (err) {
        setListError(errorMessage(err))
        setLoading(false)
        return
      }
      try {
        await disposeSession(name)
      } catch (err) {
        // dispose 失败仅记录不阻断：会话资源随 agent_v2 重启释放
        // （specs/049-agent-v2-dsh-init/research.md D6）。
        console.error(`dispose ${name} failed (ignored):`, errorMessage(err))
      }
      // 同资源名的新建是全新会话（FR-015）：丢弃本地 store 与面板。
      storesRef.current?.delete(name)
      setSessions((prev) => prev.filter((s) => s.name !== name))
      setOpened((prev) => prev.filter((n) => n !== name))
      // 仅当被删会话仍是完成时刻的当前视图时返回列表并提示（await 窗口内
      // 用户可能已切换到其他会话）。
      if (selectedRef.current === name) {
        setSelected(null)
        // turn_end{ABORTED} 由 store 清空（web-frontend.md §4）；提示与
        // 返回列表仅由删除流程编排——发起删除的标签页必然收到 ABORTED，
        // 其他标签页受多标签页已知限制约束（specs/049-agent-v2-dsh-init/
        // research.md 已知限制节）。
        setNotice('会话已删除，已返回列表')
      }
      setLoading(false)
    },
    [],
  )

  return (
    <div className="app">
      <aside className="sidebar">
        <SessionList
          sessions={sessions}
          selected={selected}
          loading={loading}
          error={listError}
          onSelect={openSession}
          onRefresh={() => {
            void refresh()
          }}
          onCreate={() => {
            void onCreate()
          }}
          onDelete={(name) => {
            void onDelete(name)
          }}
        />
      </aside>
      <main className="main">
        {selected === null ? (
          <div className="empty-hint" data-testid="empty-hint">
            {notice ?? '选择或新建一个 session 开始对话'}
          </div>
        ) : null}
        {opened.map((name) => {
          const store = storesRef.current?.get(name)
          if (store === undefined) return null
          return (
            <ChatPanel
              key={name}
              session={name}
              store={store}
              active={name === selected}
            />
          )
        })}
      </main>
    </div>
  )
}
