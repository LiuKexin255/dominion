// agent 物化面板：preset 下拉（必选）+ model 下拉（listModels + "默认"项）+
// Apply = UpdateAgent（契约 specs/051-agent-v2-dsh-migration/contracts/
// web-frontend.md §3）。model 空 = 进程默认（agent-api.md §1 Agent.model）；
// 已物化会话再次 Apply 即刷新（清空短期记忆 + 重读 preset，US2 场景 3/4），
// 面板对该语义显式提示。
import { useCallback, useEffect, useState } from 'react'
import { Button } from '@deepseek-ai/dsh-client-ui-primitives'
import {
  getAgent,
  listModels,
  listPresets,
  updateAgent,
} from '../api/agent.js'
import type { Agent, Model, Preset } from '../api/agent.js'
import { ApiError } from '../api/conversation.js'

interface AgentSettingsPanelProps {
  session: string
  // 当前物化配置；null = 未物化（首次物化）。
  materialized: Agent | null
  // Apply 成功回调：调用方刷新 agentStatus 并关闭面板。
  onApplied: (agent: Agent) => void
  onClose: () => void
}

// templateOf projects the session resource name to its template segment
// (templates/{template}/sessions/{id} → {template})——preset 目录按模板隔离，
// 物化校验要求 preset 与 agent 同模板（agent-api.md §2.1）。
function templateOf(session: string): string {
  return session.split('/')[1] ?? ''
}

export function AgentSettingsPanel({
  session,
  materialized,
  onApplied,
  onClose,
}: AgentSettingsPanelProps) {
  const template = templateOf(session)
  const [presets, setPresets] = useState<Preset[]>([])
  const [models, setModels] = useState<Model[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [presetName, setPresetName] = useState(materialized?.preset ?? '')
  // '' = "默认"项（进程默认模型）。
  const [modelName, setModelName] = useState(materialized?.model ?? '')
  const [applying, setApplying] = useState(false)

  useEffect(() => {
    let cancelled = false
    // 两个下拉同属 PresetService 面（gateway 直连，web-frontend.md §3），
    // 数据源与 UpdateAgent 的提交校验同源（agent-api.md §2.1/§2.6）。
    void Promise.all([listPresets(template), listModels()])
      .then(([presetList, modelList]) => {
        if (cancelled) return
        setPresets(presetList)
        setModels(modelList)
        setLoading(false)
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : String(err))
        setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [template])

  const apply = useCallback(async () => {
    // preset 必选（web-frontend.md §3）：未选择时仅提示，不发请求。
    if (presetName === '') {
      setError('必须选择一个 preset')
      return
    }
    setApplying(true)
    try {
      const agent = await updateAgent(session, presetName, modelName)
      onApplied(agent)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setApplying(false)
    }
  }, [presetName, modelName, session, onApplied])

  return (
    <div className="agent-settings-panel" data-testid="agent-settings-panel">
      <div className="agent-panel-header">
        <span>{materialized === null ? '设置 agent（未物化）' : '设置 agent（已物化，再次应用即刷新）'}</span>
        <Button data-testid="agent-panel-close" onClick={onClose}>
          关闭
        </Button>
      </div>
      {materialized !== null && (
        <p className="agent-refresh-hint" data-testid="agent-refresh-hint">
          该会话已有 agent：再次应用将清空短期记忆并重读 preset 当前内容（刷新语义）。
        </p>
      )}
      <label className="agent-panel-field" htmlFor="agent-preset-select">
        <span>preset（必选）</span>
        <select
          id="agent-preset-select"
          data-testid="agent-preset-select"
          value={presetName}
          onChange={(e) => setPresetName(e.target.value)}
        >
          <option value="">{loading ? '加载中…' : '请选择 preset'}</option>
          {presets.map((p) => (
            <option key={p.name} value={p.name}>
              {p.name.split('/').pop() ?? p.name}
            </option>
          ))}
        </select>
      </label>
      <label className="agent-panel-field" htmlFor="agent-model-select">
        <span>model（留空 = 进程默认）</span>
        <select
          id="agent-model-select"
          data-testid="agent-model-select"
          value={modelName}
          onChange={(e) => setModelName(e.target.value)}
        >
          <option value="">默认</option>
          {models.map((m) => (
            <option key={m.id} value={m.id}>
              {m.id}
            </option>
          ))}
        </select>
      </label>
      {error !== null && (
        <div className="agent-panel-error" data-testid="agent-settings-error" role="alert">
          {error}
        </div>
      )}
      <div className="agent-panel-actions">
        <Button
          variant="primary"
          data-testid="agent-apply"
          disabled={applying || loading}
          onClick={() => void apply()}
        >
          应用
        </Button>
      </div>
    </div>
  )
}

// materialization guide helpers shared with the ChatPanel integration:
// isUnmaterializedError reports whether a GetAgent failure means "the agent
// singleton is absent" (404 NOT_FOUND, agent-api.md §2.2)——anything else is a
// transport/other failure and must not trigger the guide.
export function isUnmaterializedError(err: unknown): boolean {
  return err instanceof ApiError && err.status === 404
}

// probeAgent fetches the session's agent singleton and classifies the
// materialization status ('materialized' | 'unmaterialized' | 'unknown').
export async function probeAgent(
  session: string,
): Promise<{ status: 'materialized' | 'unmaterialized' | 'unknown'; agent: Agent | null }> {
  try {
    return { status: 'materialized', agent: await getAgent(session) }
  } catch (err) {
    if (isUnmaterializedError(err)) return { status: 'unmaterialized', agent: null }
    return { status: 'unknown', agent: null }
  }
}
