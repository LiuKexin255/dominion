// 应用装配：侧栏（session 列表 + 视图切换）+ 主区（对话 / preset 管理）。
// 无路由库——会话视图由选中 session 状态驱动，presets 视图为单页 state 切换
// （specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §2）。每
// session 的 ChatStore 以资源名为键存于 App 级 Map：切换/返回列表不丢各自
// 进度（发送中的 Send 流由 store 持有、在后台继续归约，fetch 不中断——
// FR-012/FR-014 前端侧）。会话面板在两种视图下都常驻挂载：视图切换仅以
// CSS 隐藏会话面板（unmount 会让 ChatPanel 的回填 effect 重跑，回填响应
// 落地时覆盖在途回合——web-frontend.md §4），未选中会话的面板保持挂载仅
// 不渲染，再次进入直接呈现既有状态。
import { useCallback, useEffect, useRef, useState } from 'react'
import './theme.css'
import { Button } from '@deepseek-ai/dsh-client-ui-primitives'
import { ApiError, listHistory, sendStream } from './api/conversation.js'
import type { ChatEvent } from './api/conversation.js'
import { cancelAgent, getAgent } from './api/agent.js'
import type { Agent } from './api/agent.js'
import { createSession, deleteSession, listSessions } from './api/sessions.js'
import type { Session } from './api/sessions.js'
import {
  AgentSettingsPanel,
  isUnmaterializedError,
  probeAgent,
} from './components/AgentSettingsPanel.js'
import { ChatView } from './components/ChatView.js'
import { PresetsView } from './components/PresetsView.js'
import { SessionList } from './components/SessionList.js'
import { ChatStore, useChatState } from './store/chat.js'

// 本阶段新建 session 固定 saolei template（spec 澄清，存量 session 服务零改动；
// 常量口径对齐 desktop 前端 api.ts 的 TEMPLATES）。
const TEMPLATE_SAOLEI = 'saolei'

// 侧栏底部视图切换的两个视图（web-frontend.md §2：sessions | presets）。
type AppView = 'sessions' | 'presets'

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// agent 单例的物化状态（data-model.md §2.10）：GetAgent 404 / ListAgentMessages
// 404 / Send 前置错误（FAILED_PRECONDITION→400、NOT_FOUND→404）驱动
// unmaterialized；探测/请求级失败为 unknown（不引导）。
type AgentStatus = 'unknown' | 'unmaterialized' | 'materialized'

// 桌面连接状态三态（specs/054-agent-v2-bugfixes/data-model.md §5.3，契约
// specs/054-agent-v2-bugfixes/contracts/web-ui.md §5）：connected/
// disconnected 由 GetAgent desktop_connected 投影；unknown 为降级态
// （agent 未物化 404 或查询失败）——禁止显示为已连接。
type DesktopConn = 'connected' | 'disconnected' | 'unknown'

// ChatPanel hosts one session's store-backed chat view. The store outlives the
// panel's active state (owned by App's per-session map), so an in-flight turn
// keeps reducing while this panel renders nothing. The ListAgentMessages
// backfill runs
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
  const [agentStatus, setAgentStatus] = useState<AgentStatus>('unknown')
  const [agent, setAgent] = useState<Agent | null>(null)
  // 连接态独立承载 probe/轮询结果（不复用 agent state：onApplied 的物化
  // 响应不带连接字段，复用会被覆盖回 unknown）。
  const [desktopConn, setDesktopConn] = useState<DesktopConn>('unknown')
  const [panelOpen, setPanelOpen] = useState(false)
  // 终止请求失败呈现（不吞，specs/054-agent-v2-bugfixes/contracts/web-ui.md
  // §4）；请求成功不设错误——终态经流上 turn_end{CANCELED} 由 store 归约。
  const [cancelError, setCancelError] = useState<string | null>(null)
  // 回填发起后本面板是否有 send 开始：send 与回填竞态时整体让位于 send
  // （判据说明见下方 loadHistory 调用处注释）。
  const sentSinceBackfill = useRef(false)

  // 连接状态即时刷新（specs/054-agent-v2-bugfixes/contracts/web-ui.md §5）：
  // GetAgent 200 → desktop_connected 投影 connected/disconnected；404（未
  // 物化）与其他请求级失败一律降级 unknown——不虚构连接事实。
  const refreshDesktopConn = useCallback(async () => {
    try {
      const view = await getAgent(session)
      setDesktopConn(view.desktopConnected === true ? 'connected' : 'disconnected')
    } catch {
      setDesktopConn('unknown')
    }
  }, [session])

  useEffect(() => {
    sentSinceBackfill.current = false
    let cancelled = false
    // 物化状态探测（web-frontend.md §3）：GetAgent 404 → 未物化引导态；
    // 其余失败仅置 unknown，不影响对话。
    void probeAgent(session).then((probe) => {
      if (cancelled) return
      setAgentStatus(probe.status)
      setAgent(probe.agent)
    })
    // 进入会话即时刷新连接状态（web-ui.md §5）。
    void refreshDesktopConn()
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
        if (cancelled) return
        // 未物化（含无 owner）session 的 ListAgentMessages 是 404
        // （agent-api.md §2.2/§2.3）——这是"尚无历史"而非错误：置空历史并
        // 进入未物化引导态。
        if (isUnmaterializedError(err)) {
          setAgentStatus('unmaterialized')
          setAgent(null)
          if (!sentSinceBackfill.current) store.loadHistory([])
          return
        }
        // 仅提示不清状态：回填失败时在途回合与本地消息保持不变，可刷新
        // 重试回填（FR-014 前端侧）。
        setBackfillError(errorMessage(err))
      })
    return () => {
      cancelled = true
    }
  }, [session, store, refreshDesktopConn])

  const onSend = useCallback(
    (text: string) => {
      sentSinceBackfill.current = true
      setCancelError(null)
      // send 前即时刷新连接状态（web-ui.md §5；不阻塞发送，指示仅是观测面）。
      void refreshDesktopConn()
      // Send 前置拒绝（未物化 FAILED_PRECONDITION→400 / 无 owner
      // NOT_FOUND→404，agent-api.md §2.4）驱动引导态：流失败后探测 agent
      // 单例，仅 404 确认未物化（400 的其他来源如空文本不引导）。
      async function* guidedSend(): AsyncGenerator<ChatEvent> {
        try {
          yield* sendStream(session, text)
        } catch (err) {
          if (err instanceof ApiError && (err.status === 400 || err.status === 404)) {
            const probe = await probeAgent(session)
            if (probe.status === 'unmaterialized') {
              setAgentStatus('unmaterialized')
              setAgent(null)
            }
          }
          throw err
        }
      }
      void store.send(text, guidedSend())
    },
    [store, session, refreshDesktopConn],
  )

  const onCancel = useCallback(async () => {
    setCancelError(null)
    try {
      await cancelAgent(session)
    } catch (err) {
      setCancelError(errorMessage(err))
    }
  }, [session])

  const onApplied = useCallback(
    (materialized: Agent) => {
      setAgent(materialized)
      setAgentStatus('materialized')
      setPanelOpen(false)
    },
    [],
  )

  // turn 结束即时刷新（web-ui.md §5）：全部回合终态（COMPLETED/ERROR/
  // CANCELED/流传输失败）在 store 归约中均把 live 归 null，以 live→null
  // 迁移为触发面即可覆盖 canceled 等全部终态。已知边界：若 turnStart+
  // turnEnd 在同一渲染批次内同步归约（React 18 自动批处理），本 effect 只
  // 观察到最终 null 态、漏掉该回合的一次即时刷新——实际流式场景事件跨
  // chunk 到达不会合并批次，且 send 前刷新与 10s 轮询兜底，该边界可接受。
  const hadLive = useRef(false)
  useEffect(() => {
    if (state.live !== null) {
      hadLive.current = true
      return
    }
    if (!hadLive.current) return
    hadLive.current = false
    void refreshDesktopConn()
  }, [state.live, refreshDesktopConn])

  // 10s 轮询（SC-005：连接/断开/接管后 ≤10s 反映）。ChatPanel 对所有已
  // 打开会话常驻挂载（active 仅控制渲染），轮询必须以 active 门控——否则
  // 每个打开过的会话都永久轮询。
  useEffect(() => {
    if (!active) return
    const id = setInterval(() => {
      void refreshDesktopConn()
    }, 10_000)
    return () => clearInterval(id)
  }, [active, refreshDesktopConn])

  if (!active) return null
  return (
    <div className="chat">
      <div className="agent-toolbar">
        <span
          className="desktop-conn"
          data-testid="desktop-conn-status"
          data-state={desktopConn}
        >
          {desktopConn === 'connected'
            ? '桌面已连接'
            : desktopConn === 'disconnected'
              ? '桌面未连接'
              : '桌面连接未知'}
        </span>
        <span className="agent-status" data-testid="agent-status">
          {agentStatus === 'materialized'
            ? `已物化${agent?.model ? ` · ${agent.model}` : ' · 默认模型'}`
            : agentStatus === 'unmaterialized'
              ? '未物化'
              : ''}
        </span>
        <Button data-testid="agent-settings-button" onClick={() => setPanelOpen((o) => !o)}>
          设置 agent
        </Button>
      </div>
      {agentStatus === 'unmaterialized' && !panelOpen && (
        <div className="agent-guide" data-testid="agent-guide">
          <span>该会话尚未设置 agent——选择 preset（必选）与模型完成物化后即可对话。</span>
          <Button
            variant="primary"
            data-testid="agent-guide-open"
            onClick={() => setPanelOpen(true)}
          >
            设置 agent
          </Button>
        </div>
      )}
      {panelOpen && (
        <AgentSettingsPanel
          session={session}
          materialized={agent}
          onApplied={onApplied}
          onClose={() => setPanelOpen(false)}
        />
      )}
      <ChatView
        session={session}
        history={state.history}
        live={state.live}
        queue={state.queue}
        error={state.error ?? cancelError ?? backfillError}
        canceled={state.canceled}
        onSend={onSend}
        onCancel={onCancel}
      />
    </div>
  )
}

export function App() {
  const [view, setView] = useState<AppView>('sessions')
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
        // 删除编排：仅 /api/v1 元数据删除——session 删除不联动 agent 清理
        // （specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2，
        // FR-007 Dispose 移除）。
        await deleteSession(name)
      } catch (err) {
        setListError(errorMessage(err))
        setLoading(false)
        return
      }
      // 同资源名的新建命中残留 agent 为已接受限制（spec Edge Cases）：
      // 丢弃本地 store 与面板。
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
          // 直传 async 编排（返回的 Promise 供 SessionList 驱动"该条目
          // 删除进行中"的条目级禁用）；编排体本身不变。
          onDelete={onDelete}
        />
        <div className="view-switch" data-testid="view-switch">
          <Button
            variant={view === 'sessions' ? 'primary' : 'ghost'}
            data-testid="view-sessions"
            onClick={() => setView('sessions')}
          >
            Sessions
          </Button>
          <Button
            variant={view === 'presets' ? 'primary' : 'ghost'}
            data-testid="view-presets"
            onClick={() => setView('presets')}
          >
            Presets
          </Button>
        </div>
      </aside>
      <main className="main">
        {/* 会话面板容器常驻挂载（面板不因视图切换卸载，回填 effect 不重跑）；
            空态提示属纯展示内容，仅在本视图是当前视图时渲染。 */}
        <div className={view === 'sessions' ? 'view-pane' : 'view-pane hidden'}>
          {view === 'sessions' && selected === null ? (
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
        </div>
        {view === 'presets' ? <PresetsView template={TEMPLATE_SAOLEI} /> : null}
      </main>
    </div>
  )
}
