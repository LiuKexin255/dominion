// preset 管理视图：列表（name + 更新时间）、新建/编辑表单（名称 +
// player_prompt 多行文本）、删除确认与空态引导（契约
// specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §2）。preset 无
// 内置默认（spec Q2 裁定：使用前需先创建），空态引导用户先创建才能物化
// agent。名称创建后不可改（UpdatePreset 的可变字段仅 player_prompt，
// agent-api.md §1 UpdatePresetRequest）。
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

  const closeForm = useCallback(() => setForm({ kind: 'closed' }), [])

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
      setForm({ kind: 'closed' })
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }, [form, nameDraft, promptDraft, template, refresh])

  const confirmDelete = useCallback(
    async (name: string) => {
      setSaving(true)
      try {
        await deletePreset(name)
        setDeletePending(null)
        if (form.kind === 'edit' && form.preset.name === name) setForm({ kind: 'closed' })
        await refresh()
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        setSaving(false)
      }
    },
    [form, refresh],
  )

  return (
    <div className="presets-view" data-testid="presets-view">
      <div className="presets-header">
        <h2 className="presets-title">Presets</h2>
        <div className="presets-actions">
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

      {form.kind === 'create' && (
        <form
          className="preset-form"
          data-testid="preset-form"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <label className="preset-form-field" htmlFor="preset-name-input">
            <span>名称</span>
            <Input
              id="preset-name-input"
              data-testid="preset-name-input"
              aria-label="preset 名称"
              value={nameDraft}
              onChange={(e) => setNameDraft(e.target.value)}
              placeholder="如 default"
            />
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
            <Button variant="primary" data-testid="preset-save" disabled={saving || nameDraft.trim() === ''} onClick={() => void save()}>
              保存
            </Button>
            <Button data-testid="preset-cancel" disabled={saving} onClick={closeForm}>
              取消
            </Button>
          </div>
        </form>
      )}

      {form.kind === 'edit' && (
        <form
          className="preset-form"
          data-testid="preset-form"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <label className="preset-form-field" htmlFor="preset-name-input-readonly">
            <span>名称（创建后不可改）</span>
            <Input id="preset-name-input-readonly" data-testid="preset-name-input" aria-label="preset 名称" value={nameDraft} disabled readOnly />
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
            <Button variant="primary" data-testid="preset-save" disabled={saving} onClick={() => void save()}>
              保存
            </Button>
            <Button data-testid="preset-cancel" disabled={saving} onClick={closeForm}>
              取消
            </Button>
          </div>
        </form>
      )}

      {!loading && presets.length === 0 && error === null ? (
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
                    <Button data-testid="preset-edit" disabled={form.kind !== 'closed' || saving} onClick={() => openEdit(p)}>
                      编辑
                    </Button>
                    <Button data-testid="preset-delete" disabled={form.kind !== 'closed' || saving} onClick={() => setDeletePending(p.name)}>
                      删除
                    </Button>
                  </>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {loading && <div className="presets-note">加载中…</div>}
    </div>
  )
}
