// preset 管理视图：role 分池列表（role 标识 + 全部/player/planner 过滤）、
// 新建/编辑独占表单（编辑期间列表不渲染，
// specs/054-agent-v2-bugfixes/contracts/web-ui.md §6）、删除确认与空态引导
// （specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §2）。preset
// 无内置默认（spec Q2 裁定：使用前需先创建），空态引导用户先创建才能物化
// team。role 创建时必选、创建后不可改（决定所属池与绑定的工具插件行）；
// 名称同样创建后不可改（UpdatePreset 的可变字段仅 persona，
// specs/059-agent-v2-team-mode/contracts/preset-api.md §1/§2）；正在编辑的
// 条目被外部删除时经列表刷新自动关闭表单返回列表（051 自动关闭语义在独占
// 视图下的延续，web-ui.md §6 竞态行）。
import { useCallback, useEffect, useState } from 'react'
import { Button, Input } from '@deepseek-ai/dsh-client-ui-primitives'
import {
  createPreset,
  deletePreset,
  listPresets,
  updatePreset,
} from '../api/agent.js'
import type { Preset } from '../api/agent.js'

interface PresetsViewProps {
  template: string
}

// formatTime renders the protojson update_time (RFC 3339 string) in the
// browser locale; an unparseable value falls back to the raw string.
function formatTime(updateTime: string): string {
  const date = new Date(updateTime)
  return Number.isNaN(date.getTime()) ? updateTime : date.toLocaleString()
}

// presetTitle projects the resource name to its display id segment
// (templates/{template}/presets/{id} → {id}).
function presetTitle(name: string): string {
  return name.split('/').pop() ?? name
}

// role 单选与过滤的池集合（场景词汇字符串，preset-api.md §2：saolei 下
// "player"/"planner"）。前端直接按 wire 字符串消费，无枚举归一化。
const ROLE_OPTIONS = ['player', 'planner'] as const

// roleLabel renders the wire role of a listed preset; empty = 未设置
// （proto3 缺省——role 进入 create 要求后仅在存量记录上出现）。
function roleLabel(role: string | undefined): string {
  return role === undefined || role === '' ? '未设置' : role
}

type FormMode = { kind: 'closed' } | { kind: 'create' } | { kind: 'edit'; preset: Preset }

export function PresetsView({ template }: PresetsViewProps) {
  const [presets, setPresets] = useState<Preset[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [form, setForm] = useState<FormMode>({ kind: 'closed' })
  const [nameDraft, setNameDraft] = useState('')
  const [promptDraft, setPromptDraft] = useState('')
  // role 草稿仅创建表单使用（必选）；'' = 未选。
  const [roleDraft, setRoleDraft] = useState('')
  // role 过滤（preset-api.md §1 ListPresets role 参数）；'' = 全部（不过滤）。
  const [roleFilter, setRoleFilter] = useState('')
  // 待确认删除的 preset 资源名（inline 确认，二步提交）。
  const [deletePending, setDeletePending] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      setPresets(await listPresets(template, roleFilter))
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [template, roleFilter])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const openCreate = useCallback(() => {
    setForm({ kind: 'create' })
    setNameDraft('')
    setPromptDraft('')
    // 过滤在某一池时预选该池（空则保持未选，强制用户显式选择 role）。
    setRoleDraft(roleFilter)
    // 收起悬置中的删除确认，避免确认 UI 与表单同时呈现。
    setDeletePending(null)
    setError(null)
  }, [roleFilter])

  const openEdit = useCallback((preset: Preset) => {
    setForm({ kind: 'edit', preset })
    setNameDraft(presetTitle(preset.name))
    setPromptDraft(preset.persona ?? '')
    setDeletePending(null)
    setError(null)
  }, [])

  // 关闭表单返回列表：同步清除错误横幅——错误属于编辑操作上下文（保存
  // 失败等），返回列表后残留会误导为列表自身出错；列表加载错误由 refresh
  // 自行设置。
  const closeForm = useCallback(() => {
    setForm({ kind: 'closed' })
    setError(null)
  }, [])

  const save = useCallback(async () => {
    const presetId = nameDraft.trim()
    if (presetId === '') return
    // role 同属创建必填（preset-api.md §2）——保存按钮同步禁用，此处双保险。
    if (form.kind === 'create' && roleDraft === '') return
    setSaving(true)
    try {
      if (form.kind === 'create') {
        await createPreset(template, presetId, promptDraft, roleDraft)
        closeForm()
        // 新条目不属于当前过滤池时切到该池（roleFilter 变更经 refresh
        // effect 重取列表）；否则直接刷新。不切换的话新条目会被当前过滤
        // 隐藏，看起来像创建失败。
        if (roleFilter !== '' && roleFilter !== roleDraft) {
          setRoleFilter(roleDraft)
        } else {
          await refresh()
        }
      } else if (form.kind === 'edit') {
        await updatePreset(form.preset.name, promptDraft)
        closeForm()
        await refresh()
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }, [form, nameDraft, promptDraft, roleDraft, roleFilter, template, refresh, closeForm])

  const confirmDelete = useCallback(
    async (name: string) => {
      setSaving(true)
      try {
        await deletePreset(name)
        setDeletePending(null)
        await refresh()
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        setSaving(false)
      }
    },
    [refresh],
  )

  // 独占编辑期间被编辑条目被外部删除（列表刷新后缺失）→ 自动关闭表单
  // 返回列表，不残留已删除条目的编辑态。
  useEffect(() => {
    if (form.kind === 'edit' && !presets.some((p) => p.name === form.preset.name)) {
      closeForm()
    }
  }, [form, presets, closeForm])

  return (
    <div className="presets-view" data-testid="presets-view">
      <div className="presets-header">
        <h2 className="presets-title">Presets</h2>
        <div className="presets-actions">
          {/* 刷新在编辑期间保持可用——独占视图下它是竞态检测（被编辑条目被
              外部删除后自动关闭表单）的驱动面，不禁用；新建按钮编辑期间
              禁用，避免静默重置进行中的表单。 */}
          <Button data-testid="refresh-presets" disabled={loading} onClick={() => void refresh()}>
            刷新
          </Button>
          <Button variant="primary" data-testid="create-preset" disabled={loading || form.kind !== 'closed'} onClick={openCreate}>
            新建 preset
          </Button>
        </div>
      </div>
      <p className="presets-hint">
        preset 按 role 分池（player/planner），内容为角色 persona（不含模型）；role 决定绑定的工具插件组，创建后不可改。
      </p>
      {error !== null && (
        <div className="presets-error" data-testid="presets-error" role="alert">
          {error}
        </div>
      )}

      {form.kind !== 'closed' && (
        <form
          className="preset-form"
          data-testid="preset-form"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <label className="preset-form-field" htmlFor="preset-name-input">
            <span>{form.kind === 'create' ? '名称' : '名称（创建后不可改）'}</span>
            {form.kind === 'create' ? (
              <Input
                id="preset-name-input"
                data-testid="preset-name-input"
                aria-label="preset 名称"
                value={nameDraft}
                onChange={(e) => setNameDraft(e.target.value)}
                placeholder="如 default"
              />
            ) : (
              <Input id="preset-name-input" data-testid="preset-name-input" aria-label="preset 名称" value={nameDraft} disabled readOnly />
            )}
          </label>
          {form.kind === 'create' ? (
            <div
              className="preset-form-field"
              data-testid="preset-role-field"
              role="radiogroup"
              aria-label="role"
            >
              <span>role（必选，创建后不可改）</span>
              <div className="preset-role-options">
                {ROLE_OPTIONS.map((role) => (
                  <label key={role} className="preset-role-option">
                    <input
                      type="radio"
                      name="preset-role"
                      value={role}
                      data-testid={`preset-role-${role}`}
                      checked={roleDraft === role}
                      onChange={() => setRoleDraft(role)}
                    />
                    {role}
                  </label>
                ))}
              </div>
            </div>
          ) : (
            <div className="preset-form-field" data-testid="preset-role-readonly">
              <span>role（创建后不可改——决定绑定的工具插件组）</span>
              <span className="preset-role-value" data-testid="preset-role-value">
                {roleLabel(form.preset.role)}
              </span>
            </div>
          )}
          <label className="preset-form-field" htmlFor="preset-prompt-input">
            <span>persona（空 = 物化时回退该角色默认 base）</span>
            <textarea
              id="preset-prompt-input"
              className="preset-prompt-input"
              data-testid="preset-prompt-input"
              aria-label="persona"
              rows={8}
              value={promptDraft}
              onChange={(e) => setPromptDraft(e.target.value)}
            />
          </label>
          <div className="preset-form-actions">
            <Button
              variant="primary"
              data-testid="preset-save"
              disabled={
                saving ||
                (form.kind === 'create' && (nameDraft.trim() === '' || roleDraft === ''))
              }
              onClick={() => void save()}
            >
              保存
            </Button>
            <Button data-testid="preset-cancel" disabled={saving} onClick={closeForm}>
              取消
            </Button>
          </div>
        </form>
      )}

      {form.kind === 'closed' && (
        <>
          {/* role 过滤（preset-api.md §1：ListPresets role 参数——空 = 全部
              = 不过滤）；过滤变更经 refresh effect 重取列表。 */}
          <div className="presets-filter">
            <label htmlFor="preset-role-filter">role 过滤</label>
            <select
              id="preset-role-filter"
              data-testid="preset-role-filter"
              value={roleFilter}
              onChange={(e) => setRoleFilter(e.target.value)}
            >
              <option value="">全部</option>
              {ROLE_OPTIONS.map((role) => (
                <option key={role} value={role}>
                  {role}
                </option>
              ))}
            </select>
          </div>
          {!loading && presets.length === 0 && error === null ? (
            <div className="presets-empty" data-testid="presets-empty">
              {roleFilter === ''
                ? '还没有 preset。先创建 preset 才能物化 team——在会话页的「设置 team」里需要选择 player/planner 的 preset。'
                : `还没有 ${roleFilter} 角色的 preset。`}
              <div className="presets-empty-actions">
                <Button variant="primary" data-testid="presets-empty-create" onClick={openCreate}>
                  新建 preset
                </Button>
              </div>
            </div>
          ) : (
            <ul className="preset-items">
              {presets.map((p) => (
                <li key={p.name} className="preset-item" data-testid="preset-item">
                  <div className="preset-item-main">
                    <div className="preset-item-title">
                      <span className="preset-item-name" data-testid="preset-name">
                        {presetTitle(p.name)}
                      </span>
                      <span
                        className="preset-role-badge"
                        data-testid="preset-role"
                        data-role={roleLabel(p.role)}
                      >
                        {roleLabel(p.role)}
                      </span>
                    </div>
                    {p.updateTime !== undefined && (
                      <span className="preset-item-time" data-testid="preset-time">
                        {formatTime(p.updateTime)}
                      </span>
                    )}
                  </div>
                  <div className="preset-item-actions">
                    {deletePending === p.name ? (
                      <>
                        <span className="preset-delete-confirm">确认删除？</span>
                        <Button
                          variant="primary"
                          data-testid="preset-delete-confirm"
                          disabled={saving}
                          onClick={() => void confirmDelete(p.name)}
                        >
                          确认
                        </Button>
                        <Button data-testid="preset-delete-cancel" disabled={saving} onClick={() => setDeletePending(null)}>
                          取消
                        </Button>
                      </>
                    ) : (
                      <>
                        <Button data-testid="preset-edit" disabled={saving} onClick={() => openEdit(p)}>
                          编辑
                        </Button>
                        <Button data-testid="preset-delete" disabled={saving} onClick={() => setDeletePending(p.name)}>
                          删除
                        </Button>
                      </>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      {loading && <div className="presets-note">加载中…</div>}
    </div>
  )
}
