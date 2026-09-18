// preset 管理视图：列表（name + 更新时间）、新建/编辑独占表单（编辑期间
// 列表不渲染，specs/054-agent-v2-bugfixes/contracts/web-ui.md §6）、删除
// 确认与空态引导（specs/051-agent-v2-dsh-migration/contracts/
// web-frontend.md §2）。preset 无内置默认（spec Q2 裁定：使用前需先创建），
// 空态引导用户先创建才能物化 agent。名称创建后不可改（UpdatePreset 的
// 可变字段仅 player_prompt，specs/051-agent-v2-dsh-migration/contracts/
// agent-api.md §1 UpdatePresetRequest）；正在编辑的条目被外部删除时经列表
// 刷新自动关闭表单返回列表（051 自动关闭语义在独占视图下的延续，
// web-ui.md §6 竞态行）。
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

type FormMode = { kind: 'closed' } | { kind: 'create' } | { kind: 'edit'; preset: Preset }

export function PresetsView({ template }: PresetsViewProps) {
  const [presets, setPresets] = useState<Preset[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [form, setForm] = useState<FormMode>({ kind: 'closed' })
  const [nameDraft, setNameDraft] = useState('')
  const [promptDraft, setPromptDraft] = useState('')
  // 待确认删除的 preset 资源名（inline 确认，二步提交）。
  const [deletePending, setDeletePending] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      setPresets(await listPresets(template))
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [template])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const openCreate = useCallback(() => {
    setForm({ kind: 'create' })
    setNameDraft('')
    setPromptDraft('')
    // 收起悬置中的删除确认，避免确认 UI 与表单同时呈现。
    setDeletePending(null)
    setError(null)
  }, [])

  const openEdit = useCallback((preset: Preset) => {
    setForm({ kind: 'edit', preset })
    setNameDraft(presetTitle(preset.name))
    setPromptDraft(preset.playerPrompt ?? '')
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
    setSaving(true)
    try {
      if (form.kind === 'create') {
        await createPreset(template, presetId, promptDraft)
      } else if (form.kind === 'edit') {
        await updatePreset(form.preset.name, promptDraft)
      }
      closeForm()
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }, [form, nameDraft, promptDraft, template, refresh, closeForm])

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
      <p className="presets-hint">preset 是 agent 物化时引用的玩家提示词配置（不含模型）。</p>
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
          <label className="preset-form-field" htmlFor="preset-prompt-input">
            <span>player_prompt（空 = 物化时回退默认提示词）</span>
            <textarea
              id="preset-prompt-input"
              className="preset-prompt-input"
              data-testid="preset-prompt-input"
              aria-label="player_prompt"
              rows={8}
              value={promptDraft}
              onChange={(e) => setPromptDraft(e.target.value)}
            />
          </label>
          <div className="preset-form-actions">
            <Button
              variant="primary"
              data-testid="preset-save"
              disabled={saving || (form.kind === 'create' && nameDraft.trim() === '')}
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

      {form.kind === 'closed' &&
        (!loading && presets.length === 0 && error === null ? (
          <div className="presets-empty" data-testid="presets-empty">
            还没有 preset。先创建 preset 才能物化 agent——在会话页的「设置 agent」里需要选择一个 preset。
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
                  <span className="preset-item-name" data-testid="preset-name">
                    {presetTitle(p.name)}
                  </span>
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
        ))}
      {loading && <div className="presets-note">加载中…</div>}
    </div>
  )
}
