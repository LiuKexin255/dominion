// memory 管理视图：先选 preset，再管理该 preset 的 memory 集合
// （分页浏览、创建、编辑、inline 二步删除）——契约
// specs/071-memory-preset-scope/contracts/web-memory-view.md §1/§3-4。组织与
// 交互语汇沿用既有管理视图：PresetsView 的列表/表单形态、
// specs/069-wiki-template-web/contracts/web-wiki-view.md §3 的分页与独占表单、
// specs/054-agent-v2-bugfixes/contracts/web-ui.md §6（独占创建/编辑表单——
// 编辑期间列表不渲染、inline 二步删除、loading/error/空态与错误呈现）。视图为
// 纯前端 state（选中 preset、分页 token 链、表单模式、删除确认），与
// sessions/presets/wiki 视图互斥渲染。
//
// 全部用户可见文案场景无关（spec.md FR-016）：消费方以「agent」指代；preset
// 只呈现资源名末段 id，template 段与 role 均不进入 UI。条目 id 由
// api/memory.ts 按内容摘要派生（sha256(content)[:32]，与 memory 工具写入路径
// 同值域），用户不可见不可改。
import { useCallback, useEffect, useRef, useState } from 'react'
import { Button } from '@deepseek-ai/dsh-client-ui-primitives'
import { listPresets } from '../api/agent.js'
import type { Preset } from '../api/agent.js'
import { ApiError } from '../api/conversation.js'
import { createMemory, deleteMemory, listMemories, updateMemory } from '../api/memory.js'
import type { Memory } from '../api/memory.js'

interface MemoryViewProps {
  template: string
}

// 每页条数（契约 web-memory-view.md §3 视图默认 pageSize=20；服务端默认 100、
// 上限 1000——projects/game/game.proto ListMemoriesRequest.page_size）。
const MEMORY_PAGE_SIZE = 20

// 视图默认排序：最近更新优先，与 planner 快照注入的近因口径一致
// （specs/071-memory-preset-scope/spec.md Clarifications Session 2026-09-28）。
const MEMORY_ORDER_BY = 'update_time desc'

// 单条 content 上限（服务端 domain.MaxMemoryContentRunes 同值：
// specs/069-wiki-template-web/contracts/memory-content-limit.md §1）。
const MEMORY_CONTENT_MAX_RUNES = 1024

type FormMode = { kind: 'closed' } | { kind: 'create' } | { kind: 'edit'; memory: Memory }

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// presentError maps a request-level failure to the user-facing sentence
// (契约 web-memory-view.md §2 错误呈现): 409 = 同内容条目已存在（id 为内容摘要，
// 同内容必然同 id）、404 = 条目已不存在、400 = 参数错误（前端预检的兜底，如超限）；
// 其余（网络/5xx）沿用 ApiError.message。
function presentError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 409) return '该内容已存在（相同内容的条目不会重复创建）。'
    if (err.status === 404) return '条目不存在或已被删除。'
    if (err.status === 400) return `content 不合法：${serverMessage(err.body)}`
  }
  return errorMessage(err)
}

// serverMessage extracts the gRPC `message` from the grpc-gateway error body
// ({"code":3,"message":"...","details":[]}) so the UI shows the domain
// sentence (e.g. the rune-limit text) instead of the raw JSON envelope; a
// non-JSON body is presented as-is.
function serverMessage(body: string): string {
  try {
    const parsed = JSON.parse(body) as { message?: unknown }
    if (typeof parsed.message === 'string' && parsed.message !== '') return parsed.message
  } catch {
    // Not a JSON envelope: fall through to the raw body.
  }
  return body
}

// formatTime renders the protojson update_time (RFC 3339 string) in the
// browser locale; an unparseable value falls back to the raw string.
function formatTime(updateTime: string): string {
  const date = new Date(updateTime)
  return Number.isNaN(date.getTime()) ? updateTime : date.toLocaleString()
}

// presetIdOf projects a preset resource name to its display id
// (templates/{template}/presets/{preset} → {preset})：template 段不进入 UI
// （管理视图场景无关，FR-016）。
function presetIdOf(name: string): string {
  return name.split('/').pop() ?? name
}

// countRunes counts Unicode code points — the rune measure the service bound
// uses (utf8.RuneCountInString); a UTF-16 .length would double-count surrogate
// pairs (specs/069-wiki-template-web/contracts/memory-content-limit.md §1；
// 工具路径同款度量见 common/js/dsh-plugins/memory-service/src/operations.ts
// countRunes)。
function countRunes(value: string): number {
  return [...value].length
}

export function MemoryView({ template }: MemoryViewProps) {
  const [presets, setPresets] = useState<Preset[]>([])
  const [presetsLoading, setPresetsLoading] = useState(true)
  const [presetsError, setPresetsError] = useState<string | null>(null)
  // 选中的 preset id（资源名 templates/{template}/presets/{preset} 的末段；
  // null = 未选择：呈现选择引导，不发集合请求——data-model.md §4 presetKey）。
  const [presetKey, setPresetKey] = useState<string | null>(null)

  const [items, setItems] = useState<Memory[]>([])
  const [loading, setLoading] = useState(false)
  // 写入/删除（非列表请求）进行中：禁用操作入口，避免并发提交。
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // AIP-158 游标分页：pageTokens 是从首页到当前页的 token 链（首项 ''），
  // 末项为当前页 token；nextToken 是当前页的续页 token（'' = 末页）。
  const [pageTokens, setPageTokens] = useState<string[]>([''])
  const [nextToken, setNextToken] = useState('')
  // 列表刷新计数器：同一 preset/page 上重复刷新也要重取。
  const [reloadKey, setReloadKey] = useState(0)
  const [form, setForm] = useState<FormMode>({ kind: 'closed' })
  const [contentDraft, setContentDraft] = useState('')
  // 待确认删除的 memory 资源名（inline 二步提交）。
  const [deletePending, setDeletePending] = useState<string | null>(null)

  const currentToken = pageTokens[pageTokens.length - 1] ?? ''
  const onFirstPage = pageTokens.length <= 1
  const hasNextPage = nextToken !== ''

  // preset 选择器数据 = 当前 template 的全部 preset（全角色、不按 role 过滤——
  // 契约 §1；场景无关，spec FR-016）。
  useEffect(() => {
    let cancelled = false
    void (async () => {
      setPresetsLoading(true)
      try {
        const list = await listPresets(template)
        if (cancelled) return
        setPresets(list)
        setPresetsError(null)
      } catch (err) {
        if (cancelled) return
        setPresetsError(errorMessage(err))
      } finally {
        if (!cancelled) setPresetsLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [template])

  // 集合取数纪元（request sequence）：每次发起取数都递增。响应落地时若纪元已
  // 变，说明已有更新的请求在途，本次响应视为过期并整体丢弃——旧 preset 的晚到
  // 响应不得把旧集合写进新选择的视图，跨集合零泄漏
  // （specs/071-memory-preset-scope/spec.md FR-008 与 SC-002）。React 的 Fetching
  // data 模式以 effect cleanup 忽略过期响应
  // （https://react.dev/learn/synchronizing-with-effects#fetching-data ），此处以
  // 请求纪元表达同一语义：effect 触发的取数与写入后的显式重取共用该守卫，两者
  // 都只在仍属当前选择时落地；显式重取在 preset 未变时纪元恒匹配，不会被吞掉。
  const refreshEpoch = useRef(0)

  // 当前选中 preset 的同步镜像：写操作（创建/编辑/删除）的 await 窗口内可能发生
  // preset 切换，其完成后的重取闭包已过期，镜像用于判定该次重取是否仍属于当前
  // 选择（state 闭包值在异步续体里读不到最新）。
  const presetRef = useRef<string | null>(null)

  // bumpInflight 使一切在途集合请求过期并返回新的纪元号（供本次取数捕获）。
  const bumpInflight = useCallback((): number => {
    refreshEpoch.current += 1
    return refreshEpoch.current
  }, [])

  const refresh = useCallback(async () => {
    const preset = presetKey
    // 发起时即已过期（写操作 await 窗口内切换了 preset）：本次重取属于旧集合，
    // 当前选择的 effect 已自行发起对应请求——不再取数，也不改动状态。
    if (presetRef.current !== preset) return
    const epoch = bumpInflight()
    if (preset === null) {
      setItems([])
      setNextToken('')
      setLoading(false)
      return
    }
    setLoading(true)
    try {
      const page = await listMemories(template, preset, {
        pageSize: MEMORY_PAGE_SIZE,
        pageToken: currentToken,
        orderBy: MEMORY_ORDER_BY,
      })
      // 双重过期判定：更新的请求已发起（epoch），或当前选择已不再是本次 preset
      // （presetRef）。
      if (epoch !== refreshEpoch.current || presetRef.current !== preset) return
      setItems(page.memories)
      setNextToken(page.nextPageToken)
      setError(null)
    } catch (err) {
      if (epoch !== refreshEpoch.current || presetRef.current !== preset) return
      setError(errorMessage(err))
    } finally {
      // 过期请求不回收加载态：当前选择的请求负责收尾（避免竞态下清掉在途请求的
      // 加载指示）。
      if (epoch === refreshEpoch.current) setLoading(false)
    }
  }, [bumpInflight, template, presetKey, currentToken, reloadKey])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // 切换 preset：重置集合视图状态——不保留旧集合的页 token，表单与删除确认随
  // 切换关闭（契约 §3）。reloadKey 递增使同一 token 也重新取数。在途请求即时过期
  // （bumpInflight 与 presetRef 均为同步写入）：切换发生在事件处理内，早于任何
  // 微任务，故旧集合的晚到响应不可能落在新选择之下（FR-008）。
  const selectPreset = useCallback(
    (value: string) => {
      const next = value === '' ? null : value
      bumpInflight()
      presetRef.current = next
      setPresetKey(next)
      setItems([])
      setPageTokens([''])
      setNextToken('')
      setForm({ kind: 'closed' })
      setDeletePending(null)
      setError(null)
      setReloadKey((key) => key + 1)
    },
    [bumpInflight],
  )

  const goNext = useCallback(() => {
    if (nextToken === '') return
    setPageTokens((prev) => [...prev, nextToken])
    setDeletePending(null)
  }, [nextToken])

  const goPrev = useCallback(() => {
    setPageTokens((prev) => (prev.length > 1 ? prev.slice(0, -1) : prev))
    setDeletePending(null)
  }, [])

  const closeForm = useCallback(() => {
    setForm({ kind: 'closed' })
    setError(null)
  }, [])

  const openCreate = useCallback(() => {
    setForm({ kind: 'create' })
    setContentDraft('')
    setDeletePending(null)
    setError(null)
  }, [])

  const openEdit = useCallback((memory: Memory) => {
    setForm({ kind: 'edit', memory })
    setContentDraft(memory.content)
    setDeletePending(null)
    setError(null)
  }, [])

  const save = useCallback(async () => {
    if (presetKey === null || form.kind === 'closed') return
    // 空内容不提交（与 memory 工具写入路径的 empty-content 拒绝同口径，
    // common/js/dsh-plugins/memory-service/src/operations.ts applyAdd）。
    const content = contentDraft.trim()
    if (content === '') return
    // 前端预检：以服务端同款 rune 度量先行拒绝超限，避免无谓往返；服务端 400
    // 仍为权威兜底（契约 §3）。
    const runes = countRunes(content)
    if (runes > MEMORY_CONTENT_MAX_RUNES) {
      setError(`content 超过 ${MEMORY_CONTENT_MAX_RUNES} 字符上限（当前 ${runes}）。`)
      return
    }
    setBusy(true)
    try {
      if (form.kind === 'create') {
        await createMemory(template, presetKey, content)
      } else {
        await updateMemory(form.memory.name, content)
      }
      setForm({ kind: 'closed' })
      await refresh()
    } catch (err) {
      // 编辑目标被外部并发删除：呈现 404 并返回列表（054 §6 自动关闭语义）。
      if (form.kind === 'edit' && err instanceof ApiError && err.status === 404) {
        setForm({ kind: 'closed' })
        await refresh()
      }
      setError(presentError(err))
    } finally {
      setBusy(false)
    }
  }, [presetKey, form, contentDraft, template, refresh])

  const confirmDelete = useCallback(
    async (name: string) => {
      setBusy(true)
      let failure: unknown = null
      try {
        await deleteMemory(name)
      } catch (err) {
        failure = err
      }
      setDeletePending(null)
      setBusy(false)
      // 成功或并发删除（404）都以服务端列表为准重取；错误在刷新后呈现，
      // 避免被 refresh 的成功路径清除。
      await refresh()
      if (failure !== null) setError(presentError(failure))
    },
    [refresh],
  )

  return (
    <div className="presets-view" data-testid="memory-view">
      <div className="presets-header">
        <h2 className="presets-title">Memory</h2>
        <div className="presets-actions">
          <Button
            data-testid="refresh-memories"
            disabled={presetKey === null || presetsLoading || loading || busy}
            onClick={() => setReloadKey((key) => key + 1)}
          >
            刷新
          </Button>
          <Button
            variant="primary"
            data-testid="create-memory"
            disabled={presetKey === null || loading || busy || form.kind !== 'closed'}
            onClick={openCreate}
          >
            新建 memory
          </Button>
        </div>
      </div>
      <p className="presets-hint">
        先选择 preset，再维护它的 memory 集合；同一 preset 在不同会话中的 agent 共享同一份记忆。
      </p>
      <div className="presets-filter">
        <label htmlFor="memory-preset-select">preset</label>
        <select
          id="memory-preset-select"
          data-testid="memory-preset-select"
          value={presetKey ?? ''}
          disabled={presetsLoading || busy}
          onChange={(e) => selectPreset(e.target.value)}
        >
          <option value="">{presets.length === 0 ? '（当前没有 preset）' : '请选择 preset'}</option>
          {presets.map((preset) => (
            <option key={preset.name} value={presetIdOf(preset.name)}>
              {presetIdOf(preset.name)}
            </option>
          ))}
        </select>
      </div>
      {presetsError !== null && (
        <div className="presets-error" data-testid="memory-presets-error" role="alert">
          {presetsError}
        </div>
      )}
      {error !== null && (
        <div className="presets-error" data-testid="memory-error" role="alert">
          {error}
        </div>
      )}

      {presetKey === null && (
        <div className="presets-empty" data-testid="memory-select-guide">
          选择一个 preset，查看和维护它的 memory 集合。
        </div>
      )}

      {presetKey !== null && form.kind !== 'closed' && (
        <form
          className="preset-form"
          data-testid="memory-form"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <label className="preset-form-field" htmlFor="memory-content-input">
            <span>内容（≤ {MEMORY_CONTENT_MAX_RUNES} 字符）</span>
            <textarea
              id="memory-content-input"
              className="preset-prompt-input"
              data-testid="memory-content-input"
              aria-label="memory 内容"
              rows={6}
              value={contentDraft}
              onChange={(e) => setContentDraft(e.target.value)}
              placeholder="一条对后续会话仍然有用的要点"
            />
          </label>
          <div className="preset-form-actions">
            <Button
              variant="primary"
              data-testid="memory-save"
              disabled={busy || contentDraft.trim() === ''}
              onClick={() => void save()}
            >
              保存
            </Button>
            <Button data-testid="memory-cancel" disabled={busy} onClick={closeForm}>
              取消
            </Button>
          </div>
        </form>
      )}

      {presetKey !== null && form.kind === 'closed' && (
        <>
          {!loading && items.length === 0 && error === null ? (
            <div className="presets-empty" data-testid="memory-empty">
              该 preset 还没有 memory 条目。新建一条把要点沉淀下来，agent 也能检索到。
              <div className="presets-empty-actions">
                <Button
                  variant="primary"
                  data-testid="memory-empty-create"
                  onClick={openCreate}
                >
                  新建 memory
                </Button>
              </div>
            </div>
          ) : (
            <ul className="preset-items">
              {items.map((item) => (
                <li key={item.name} className="preset-item" data-testid="memory-item">
                  <div className="preset-item-main">
                    {/* 条目无 title 字段：列表行直接呈现全文（List 返回全量资源，
                        memory 服务无单条 Get 面——契约 §3「查看」）。 */}
                    <span className="preset-item-content" data-testid="memory-content">
                      {item.content}
                    </span>
                    {item.updateTime !== undefined && (
                      <span className="preset-item-time" data-testid="memory-time">
                        {formatTime(item.updateTime)}
                      </span>
                    )}
                  </div>
                  <div className="preset-item-actions">
                    {deletePending === item.name ? (
                      <>
                        <span className="preset-delete-confirm">确认删除？</span>
                        <Button
                          variant="primary"
                          data-testid="memory-delete-confirm"
                          disabled={busy}
                          onClick={() => void confirmDelete(item.name)}
                        >
                          确认
                        </Button>
                        <Button
                          data-testid="memory-delete-cancel"
                          disabled={busy}
                          onClick={() => setDeletePending(null)}
                        >
                          取消
                        </Button>
                      </>
                    ) : (
                      <>
                        <Button
                          data-testid="memory-edit"
                          disabled={busy}
                          onClick={() => openEdit(item)}
                        >
                          编辑
                        </Button>
                        <Button
                          data-testid="memory-delete"
                          disabled={busy}
                          onClick={() => setDeletePending(item.name)}
                        >
                          删除
                        </Button>
                      </>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}

          {!loading && (!onFirstPage || hasNextPage) && (
            <div className="presets-actions" data-testid="memory-pager">
              <span className="presets-note" data-testid="memory-page">
                第 {pageTokens.length} 页
              </span>
              <Button
                data-testid="memory-prev"
                disabled={loading || busy || onFirstPage}
                onClick={goPrev}
              >
                上一页
              </Button>
              <Button
                data-testid="memory-next"
                disabled={loading || busy || !hasNextPage}
                onClick={goNext}
              >
                下一页
              </Button>
            </div>
          )}
        </>
      )}
      {loading && <div className="presets-note">加载中…</div>}
    </div>
  )
}
