// 应用装配：侧栏（session 列表 + 视图切换）+ 主区（对话 / preset 管理）。
// 无路由库——会话视图由选中 session 状态驱动，presets 视图为单页 state 切换
// （specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §2）。每
// session 的 ChatStore 以资源名为键存于 App 级 Map：切换/返回列表不丢各自
// 进度（发送中的 team 流由 store 持有、在后台继续归约，fetch 不中断——
// FR-014/FR-017 前端侧）。会话面板在两种视图下都常驻挂载：视图切换仅以
// CSS 隐藏会话面板（unmount 会让 ChatPanel 的回填 effect 重跑，回填响应
// 落地时覆盖在途回合——web-views.md §2），未选中会话的面板保持挂载仅
// 不渲染，再次进入直接呈现既有状态。
import { useCallback, useEffect, useRef, useState } from 'react'
import './dsh-theme/index.css'
import './theme.css'
import { Button } from '@deepseek-ai/dsh-client-ui-primitives'
import { ApiError, sendStream } from './api/conversation.js'
import type { ChatEvent } from './api/conversation.js'
import {
  cancelTeam,
  getTeam,
  listMemberMessages,
  listTeamMessages,
} from './api/agent.js'
import type { Team } from './api/agent.js'
import { createSession, deleteSession, listSessions } from './api/sessions.js'
import type { Session } from './api/sessions.js'
import {
  isUnmaterializedError,
  probeTeam,
  TeamSettingsPanel,
} from './components/TeamSettingsPanel.js'
import { ChatView, TEAM_VIEW } from './components/ChatView.js'
import { PresetsView } from './components/PresetsView.js'
import { SessionList } from './components/SessionList.js'
import {
  SystemPromptOverlay,
  useSystemPrompt,
} from './components/SystemPromptOverlay.js'
import { ChatStore, useChatState } from './store/chat.js'

// 本阶段新建 session 固定 saolei template（spec 澄清，存量 session 服务零改动；
// 常量口径对齐 desktop 前端 api.ts 的 TEMPLATES）。
const TEMPLATE_SAOLEI = 'saolei'

// 成员视角视图的成员集合（场景词汇字符串；web-views.md §2：成员视角视图
// 数量 = 成员数，saolei 恰 player/planner——切换器恰 3 视图：团队 + 2 成员）。
const MEMBER_VIEWS = ['player', 'planner'] as const

// 侧栏底部视图切换的两个视图（web-frontend.md §2：sessions | presets）。
type AppView = 'sessions' | 'presets'

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// team 单例的物化状态（team-api.md §1/§2）：GetTeam 404 / ListTeamMessages
// 404 / Send 前置错误（FAILED_PRECONDITION→400、NOT_FOUND→404）驱动
// unmaterialized；探测/请求级失败为 unknown（不引导）。
type TeamStatus = 'unknown' | 'unmaterialized' | 'materialized'

// 桌面连接状态三态（specs/054-agent-v2-bugfixes/data-model.md §5.3，契约
// specs/054-agent-v2-bugfixes/contracts/web-ui.md §5）：connected/
// disconnected 由 GetTeam desktop_connected 投影（player 独占使用）；
// unknown 为降级态（team 未物化 404 或查询失败）——禁止显示为已连接。
type DesktopConn = 'connected' | 'disconnected' | 'unknown'

// presetTitle projects a preset resource name to its display id.
function presetTitle(name: string | undefined): string {
  if (name === undefined || name === '') return '—'
  return name.split('/').pop() ?? name
}

// ChatPanel hosts one session's store-backed team view. The store outlives the
// panel's active state (owned by App's per-session map), so an in-flight team
// stream keeps reducing while this panel renders nothing. The ListTeamMessages
// backfill (runBackfill) runs on first entry (FR-014) and after a successful
// team re-apply (refresh rebuilds the conversation from an empty merged
// sequence, team-api.md §1), and must not overwrite a turn that started before
// the backfill response landed.
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
  const [teamStatus, setTeamStatus] = useState<TeamStatus>('unknown')
  const [team, setTeam] = useState<Team | null>(null)
  // 连接态独立承载查询结果（不复用 team state，避免探测失败时虚构连接）。
  const [desktopConn, setDesktopConn] = useState<DesktopConn>('unknown')
  const [panelOpen, setPanelOpen] = useState(false)
  // 主界面 system prompt 入口（FR-009，
  // specs/059-agent-v2-team-mode/contracts/web-views.md §5）：工具条成员清单的
  // 点击入口经既有 GetTeamMember 读取全文，浮层逻辑与设置面板共用
  // （SystemPromptOverlay 的单一控制器）：工具条成员清单与设置面板成员清单
  // 共用同一实例，任一时刻至多一个浮层；设置面板内入口保留不变。
  const systemPrompt = useSystemPrompt(session, team?.updateTime)
  // 对话页顶部视图（web-views.md §2：团队 | player | planner）：纯前端状态，
  // 切换不重新回填（各视图历史常驻于 store）；按 session 常驻于本面板，
  // 会话切换/返回时保持所选视图。
  const [chatView, setChatView] = useState<string>(TEAM_VIEW)
  // 终止请求失败呈现（不吞，specs/054-agent-v2-bugfixes/contracts/web-ui.md
  // §4）；请求成功不设错误——终态经流上 turn_end{CANCELED} 由 store 归约。
  const [cancelError, setCancelError] = useState<string | null>(null)
  // 回填发起后本面板是否有 send 开始：send 与回填竞态时整体让位于 send
  // （判据说明见 runBackfill 内守卫处注释）。
  const sentSinceBackfill = useRef(false)
  // 成员视角回填的生命周期纪元：Apply 刷新（新生命周期）时递增；在途响应
  // 落地时纪元不匹配即丢弃——旧生命周期的视角序列不得在 clear 之后复活
  // （merge 规则保留响应外条目的前提是同一生命周期）。
  const memberEpoch = useRef(0)

  // team 状态与连接状态即时刷新（web-views.md §1：GetTeam 定期刷新，节奏沿用
  // 现状 10s + 关键时机即时刷新）：GetTeam 200 → 成员清单与 desktop_connected
  // 投影；404（未物化）与其他请求级失败一律降级 unknown——不虚构连接事实。
  const refreshTeam = useCallback(async () => {
    try {
      const view = await getTeam(session)
      setTeam(view)
      setDesktopConn(view.desktopConnected === true ? 'connected' : 'disconnected')
    } catch {
      setDesktopConn('unknown')
    }
  }, [session])

  // runBackfill 是挂载回填与重建同步（Apply 成功）共用的唯一回填路径
  // （specs/057-agent-v2-ui-fixes-2/contracts/ui-interactions.md §2）：发起即
  // 复位让位守卫（新 epoch），ListTeamMessages 落地且守卫仍 false 时经
  // store.loadHistory 全量重建（seq 锚对齐，web-views.md §2）——两处调用同一
  // 函数使挂载与应用路径行为永不分叉。isCancelled 供挂载 effect 在会话切换/
  // 卸载时丢弃在途响应（清理语义）；Apply 路径无清理面，缺省不取消。
  const runBackfill = useCallback(
    async (isCancelled: () => boolean = () => false) => {
      sentSinceBackfill.current = false
      try {
        const messages = await listTeamMessages(session)
        if (isCancelled()) return
        setBackfillError(null)
        // 让位判据 = 自回填发起后是否有 send 开始，而非响应落地时刻的
        // live/queue 快照——快照判据留有两个竞态窗口：(a) 回合已完成
        // （慢网络下过期历史覆盖已合并入历史的回合）；(b) 服务端已记录
        // 用户消息但客户端首帧未达（回填含该消息，随后 team_message 帧到达
        // 再追加 = 重复）。send 一经开始，回填整体让位（web-views.md §2，
        // FR-014/FR-017 前端侧，切换/返回不丢各自进度）。
        if (!sentSinceBackfill.current) store.loadHistory(messages)
      } catch (err) {
        if (isCancelled()) return
        // 未物化（含无 owner）session 的 ListTeamMessages 是 404
        // （team-api.md §1/§2）——这是"尚无历史"而非错误：置空历史并进入
        // 未物化引导态。
        if (isUnmaterializedError(err)) {
          setTeamStatus('unmaterialized')
          setTeam(null)
          if (!sentSinceBackfill.current) store.loadHistory([])
          return
        }
        // 仅提示不清状态：回填失败时在途回合与本地消息保持不变，可刷新
        // 重试回填（web-views.md §2）。
        setBackfillError(errorMessage(err))
      }
    },
    [session, store],
  )

  // runMemberBackfill 同步两个成员视角视图（web-views.md §2）：用户输入与
  // 跨成员广播注入在被成员消费时进入其视角，消费事实只有服务端历史可见
  // （前端无消费锚，不伪造），故在挂载、回合结束（消费面已固化）与 Apply
  // 重建后经 ListMemberMessages 回填。store 侧按 messageId 合并（响应期间
  // 新固化的自身输出保留，投影占位以服务端序列为准被取代）。未物化 404
  // 是"尚无视角历史"而非错误。
  const runMemberBackfill = useCallback(
    async (isCancelled: () => boolean = () => false) => {
      const epoch = memberEpoch.current
      const results = await Promise.all(
        MEMBER_VIEWS.map(async (member) => {
          try {
            return { member, messages: await listMemberMessages(session, member) }
          } catch (err) {
            return { member, messages: null, error: err }
          }
        }),
      )
      // 纪元不匹配 = 响应属于旧生命周期（期间发生了 Apply 刷新）：整体丢弃。
      if (isCancelled() || epoch !== memberEpoch.current) return
      for (const result of results) {
        if (result.messages !== null) {
          store.loadMemberHistory(result.member, result.messages)
          continue
        }
        if (!isUnmaterializedError(result.error)) setBackfillError(errorMessage(result.error))
      }
    },
    [session, store],
  )

  useEffect(() => {
    let cancelled = false
    // 物化状态探测（web-views.md §1）：GetTeam 404 → 未物化引导态；其余
    // 失败仅置 unknown，不影响对话。
    void probeTeam(session).then((probe) => {
      if (cancelled) return
      setTeamStatus(probe.status)
      setTeam(probe.team)
    })
    // 进入会话即时刷新连接状态与成员清单（web-views.md §1）与两成员视角
    // 历史（web-views.md §2 回填）。
    void refreshTeam()
    void runBackfill(() => cancelled)
    void runMemberBackfill(() => cancelled)
    return () => {
      cancelled = true
    }
  }, [session, store, refreshTeam, runBackfill, runMemberBackfill])

  const onSend = useCallback(
    (text: string) => {
      sentSinceBackfill.current = true
      setCancelError(null)
      // send 前即时刷新状态（web-views.md §1；不阻塞发送，指示仅是观测面）。
      void refreshTeam()
      // Send 前置拒绝（未物化 FAILED_PRECONDITION→400 / 无 owner
      // NOT_FOUND→404，team-api.md §3/§6）驱动引导态：流失败后探测 team
      // 单例，仅 404 确认未物化（400 的其他来源如空文本不引导）。
      async function* guidedSend(): AsyncGenerator<ChatEvent> {
        try {
          yield* sendStream(session, text)
        } catch (err) {
          if (err instanceof ApiError && (err.status === 400 || err.status === 404)) {
            const probe = await probeTeam(session)
            if (probe.status === 'unmaterialized') {
              setTeamStatus('unmaterialized')
              setTeam(null)
            }
          }
          throw err
        }
      }
      void store.send(text, guidedSend()).then(() => {
        // 流断开的收敛（用户裁定 2026-09-10：流断开后经 List 回填 + 下次 Send
        // 重建）：store 已把已流出尾步投影入归并序列并置错误，此处经
        // ListTeamMessages 重新对齐服务端权威序列（seq 锚），下次 Send 建立
        // 新流。正常读到 team 静止结束（无错误）不触发。
        if (store.getSnapshot().error !== null) void runBackfill()
      })
    },
    [store, session, refreshTeam, runBackfill],
  )

  const onCancel = useCallback(async () => {
    setCancelError(null)
    try {
      await cancelTeam(session)
    } catch (err) {
      setCancelError(errorMessage(err))
    }
  }, [session])

  const onApplied = useCallback(
    (materialized: Team) => {
      setTeam(materialized)
      setTeamStatus('materialized')
      setDesktopConn(
        materialized.desktopConnected === true ? 'connected' : 'disconnected',
      )
      setPanelOpen(false)
      // 刷新为新生命周期（team-api.md §5：刷新 team 后为新生命周期，服务端
      // 归并序列与成员视角历史已随重建清空）：本地归并序列与成员视角序列
      // 即时复位，避免旧生命周期的 seq/消息锚污染新序列；随后经同一回填
      // 路径取服务端当前序列（重建同步，
      // specs/057-agent-v2-ui-fixes-2/contracts/ui-interactions.md §2；
      // 成员视角重建同步见 web-views.md §2）。纪元先递增：在途的旧生命周期
      // 成员回填响应按纪元丢弃。
      memberEpoch.current += 1
      store.loadHistory([])
      store.clearMemberHistory()
      void runBackfill()
      void runMemberBackfill()
    },
    [runBackfill, runMemberBackfill, store],
  )

  // 回合结束即时刷新（web-views.md §1）：全部成员回合终态（COMPLETED/ERROR/
  // CANCELED/流传输失败）在 store 归约中均把 live 收束（移除或投影入归并
  // 序列），以 live 非空→空的迁移为触发面即可覆盖 canceled 等全部终态。
  // 已知边界：若 turnStart+turnEnd 在同一渲染批次内同步归约（React 18 自动
  // 批处理），本 effect 只观察到最终空态、漏掉该回合的一次即时刷新——实际
  // 流式场景事件跨 chunk 到达不会合并批次，且 send 前刷新与 10s 轮询兜底，
  // 该边界可接受。
  const hadLive = useRef(false)
  useEffect(() => {
    if (state.live.length > 0) {
      hadLive.current = true
      return
    }
    if (!hadLive.current) return
    hadLive.current = false
    void refreshTeam()
    // 回合结束是成员视角消费面的固化点（回合开始时成员消费了用户消息/
    // 广播注入，回合内自身输出逐步固化）：即时回填两成员视角
    // （web-views.md §2「经回填呈现」）。
    void runMemberBackfill()
  }, [state.live, refreshTeam, runMemberBackfill])

  // 10s 轮询（web-views.md §1：desktop 连接状态 10s + 关键时机即时）。ChatPanel
  // 对所有已打开会话常驻挂载（active 仅控制渲染），轮询必须以 active 门控——
  // 否则每个打开过的会话都永久轮询。
  useEffect(() => {
    if (!active) return
    const id = setInterval(() => {
      void refreshTeam()
    }, 10_000)
    return () => clearInterval(id)
  }, [active, refreshTeam])

  // 激活成员（FR-008，specs/060-agent-v2-team-optimize/spec.md）：任一成员
  // 回合在途时呈现该回合成员（turn_start 帧到达即推导，覆盖最近 GetTeam 快照
  // 值）；live 全部收束后回退最近 GetTeam 值（回合终态触发的即时刷新使该快照
  // 随编排收敛更新）。不新增 store 字段——呈现层从 live 派生 + GetTeam 快照
  // 组合（specs/060-agent-v2-team-optimize/research.md R7）。
  const liveMember = state.live[state.live.length - 1]?.member
  const activeMember = liveMember ?? team?.activeMember

  if (!active) return null
  return (
    <div className="chat">
      <div className="team-toolbar">
        {/* 视图切换器（web-views.md §2：团队 | player | planner，恰 3 视图）：
            纯前端状态切换、各视图历史常驻不重填——切换只更换呈现的数据面，
            不触发任何回填请求。 */}
        <div className="chat-view-switch" data-testid="chat-view-switch">
          <Button
            variant={chatView === TEAM_VIEW ? 'primary' : 'ghost'}
            data-testid="view-team"
            data-active={chatView === TEAM_VIEW || undefined}
            onClick={() => setChatView(TEAM_VIEW)}
          >
            团队
          </Button>
          {MEMBER_VIEWS.map((member) => (
            <Button
              key={member}
              variant={chatView === member ? 'primary' : 'ghost'}
              data-testid={`view-${member}`}
              data-active={chatView === member || undefined}
              onClick={() => setChatView(member)}
            >
              {member}
            </Button>
          ))}
        </div>
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
        <span className="team-status" data-testid="team-status">
          {teamStatus === 'materialized'
            ? '已物化'
            : teamStatus === 'unmaterialized'
              ? '未物化'
              : ''}
        </span>
        {/* 激活成员徽标（specs/060-agent-v2-team-optimize/contracts/team-api.md
            §1/§5）：物化后呈现当前激活成员（在途驱动成员，静止为下一条输入
            归属成员）；未物化不呈现。 */}
        {teamStatus === 'materialized' &&
          activeMember !== undefined &&
          activeMember !== '' && (
            <span
              className="active-member"
              data-testid="active-member"
              data-member={activeMember}
            >
              当前激活：{activeMember}
            </span>
          )}
        {/* 成员清单（specs/059-agent-v2-team-mode/contracts/web-views.md §1
            状态呈现：role + preset + model；role 为 wire 字符串，直接渲染）。
            每成员即 system prompt 查看入口（FR-009，
            specs/060-agent-v2-team-optimize/spec.md：主界面直接可见，无需打开
            设置面板）。 */}
        <span className="team-members" data-testid="team-members">
          {team?.members?.map((member, i) => (
            <button
              key={member.name ?? i}
              type="button"
              className="team-member"
              data-testid="team-member"
              data-role={member.role}
              onClick={() => void systemPrompt.open(member.role)}
            >
              {member.role} · {presetTitle(member.preset)} ·{' '}
              {member.model !== undefined && member.model !== '' ? member.model : '默认模型'}
            </button>
          ))}
        </span>
        <Button data-testid="team-settings-button" onClick={() => setPanelOpen((o) => !o)}>
          设置 team
        </Button>
      </div>
      <SystemPromptOverlay controller={systemPrompt} className="system-prompt-overlay" />
      {teamStatus === 'unmaterialized' && !panelOpen && (
        <div className="team-guide" data-testid="team-guide">
          <span>
            该会话尚未物化 team——选择 player 与 planner 的 preset（各自必选）与模型完成物化后即可对话。
          </span>
          <Button
            variant="primary"
            data-testid="team-guide-open"
            onClick={() => setPanelOpen(true)}
          >
            设置 team
          </Button>
        </div>
      )}
      {teamStatus === 'materialized' && !panelOpen && state.history.length === 0 && state.live.length === 0 && (
        // 用户首驱裁定（spec Clarifications 2026-09-10）：物化后 team 静止
        // 等待，开始游戏的首次驱动由用户第一条消息触发（由 planner 处理）。
        <div className="team-ready-guide" data-testid="team-ready-guide">
          team 已物化并静止等待——发送第一条消息开始工作流（由 planner 处理）。
        </div>
      )}
      {panelOpen && (
        <TeamSettingsPanel
          session={session}
          materialized={team}
          systemPrompt={systemPrompt}
          onApplied={onApplied}
          onClose={() => setPanelOpen(false)}
        />
      )}
      <ChatView
        session={session}
        view={chatView}
        history={state.history}
        memberHistory={state.memberHistory}
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
        // 删除编排：仅 /api/v1 元数据删除——session 删除不联动 team 清理
        // （team 为进程内存态，重启/删除后回到未物化引导态；team-api.md §1）。
        await deleteSession(name)
      } catch (err) {
        setListError(errorMessage(err))
        setLoading(false)
        return
      }
      // 同资源名的新建命中残留 team 为已接受限制（spec Edge Cases）：
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
