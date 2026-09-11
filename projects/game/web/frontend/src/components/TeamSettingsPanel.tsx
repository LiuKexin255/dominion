// team 物化面板：player/planner preset 下拉（按 role 字符串过滤，均必选）+
// 双 model 下拉（listModels + "默认"项）+ Apply = UpdateTeam（契约
// specs/059-agent-v2-team-mode/contracts/web-views.md §1）。model 空 = 部署
// 默认（team-api.md §2 members[].model 空 = 部署默认）；已物化会话再次
// Apply 即刷新（终止在途回合、清空短期记忆并按新配置重建，team-api.md §1），
// 面板对该语义显式提示。成员清单提供每成员"查看 system prompt"入口：
// GetTeamMember 返回该实例当前生效的完整装配结果，只读等宽全文呈现，刷新
// team（新物化快照）后重新打开即取新值（web-views.md §5）。
import { useCallback, useEffect, useState } from 'react'
import { Button } from '@deepseek-ai/dsh-client-ui-primitives'
import {
  getTeam,
  listModels,
  listPresets,
  updateTeam,
} from '../api/agent.js'
import type { Model, Preset, Team, TeamMember } from '../api/agent.js'
import { ApiError } from '../api/conversation.js'
import { SystemPromptOverlay, useSystemPrompt } from './SystemPromptOverlay.js'
import type { SystemPromptController } from './SystemPromptOverlay.js'

interface TeamSettingsPanelProps {
  session: string
  // 当前物化配置；null = 未物化（首次物化）。
  materialized: Team | null
  // Apply 成功回调：调用方刷新 teamStatus 并关闭面板。
  onApplied: (team: Team) => void
  onClose: () => void
  // 可选：调用方共享的 system prompt 控制器（App 对话页把同一实例给工具条
  // 成员清单与本面板成员清单，结构上保证任一时刻至多一个浮层；浮层由调用方
  // 渲染）。缺省时面板自持实例并在面板内渲染（独立使用/组件测试）。
  systemPrompt?: SystemPromptController
}

// templateOf projects the session resource name to its template segment
// (templates/{template}/sessions/{id} → {template})——preset 目录按模板隔离，
// 物化校验要求 preset 与 team 同模板（team-api.md §2）。
function templateOf(session: string): string {
  return session.split('/')[1] ?? ''
}

// memberOf projects the materialized member snapshot by role（members 输入
// 输出同形，data-model.md §2）。
function memberOf(team: Team | null, role: string): TeamMember | undefined {
  return team?.members?.find((m) => m.role === role)
}

// presetTitle projects a preset resource name to its display id.
function presetTitle(name: string | undefined): string {
  if (name === undefined || name === '') return '—'
  return name.split('/').pop() ?? name
}

export function TeamSettingsPanel({
  session,
  materialized,
  onApplied,
  onClose,
  systemPrompt,
}: TeamSettingsPanelProps) {
  const template = templateOf(session)
  const [playerPresets, setPlayerPresets] = useState<Preset[]>([])
  const [plannerPresets, setPlannerPresets] = useState<Preset[]>([])
  const [models, setModels] = useState<Model[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [playerPreset, setPlayerPreset] = useState(memberOf(materialized, 'player')?.preset ?? '')
  const [plannerPreset, setPlannerPreset] = useState(memberOf(materialized, 'planner')?.preset ?? '')
  // '' = "默认"项（部署默认模型，body 省略该字段）。
  const [playerModel, setPlayerModel] = useState(memberOf(materialized, 'player')?.model ?? '')
  const [plannerModel, setPlannerModel] = useState(memberOf(materialized, 'planner')?.model ?? '')
  const [applying, setApplying] = useState(false)
  // system prompt 查看
  // （specs/059-agent-v2-team-mode/contracts/web-views.md §5）：调用方共享
  // 控制器时（App 集成）入口驱动同一实例，浮层由调用方单一渲染；否则面板
  // 自持实例并在面板内渲染（独立使用/组件测试）。刷新 team（新物化快照
  // updateTime 变化）或切换会话时收起，重新打开入口即取新值。
  const ownSystemPrompt = useSystemPrompt(session, materialized?.updateTime)
  const prompt = systemPrompt ?? ownSystemPrompt

  useEffect(() => {
    let cancelled = false
    // 三个下拉同属配置面（gateway 直连，web-views.md §1），数据源与
    // UpdateTeam 的提交校验同源（team-api.md §2）：preset 按角色池过滤
    // （preset-api.md §1 role 过滤参数为场景词汇字符串）。
    void Promise.all([
      listPresets(template, 'player'),
      listPresets(template, 'planner'),
      listModels(),
    ])
      .then(([players, planners, modelList]) => {
        if (cancelled) return
        setPlayerPresets(players)
        setPlannerPresets(planners)
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
    // 两个 preset 均必选（web-views.md §1）：未选择时仅提示，不发请求。
    if (playerPreset === '') {
      setError('必须选择一个 player preset')
      return
    }
    if (plannerPreset === '') {
      setError('必须选择一个 planner preset')
      return
    }
    setApplying(true)
    try {
      // Apply 提交 team.members 两条成员配置（web-views.md §1，data-model.md
      // §2：输入输出同形；model 空省略 = 部署默认）。
      const members: TeamMember[] = [
        { role: 'player', preset: playerPreset, ...(playerModel !== '' ? { model: playerModel } : {}) },
        { role: 'planner', preset: plannerPreset, ...(plannerModel !== '' ? { model: plannerModel } : {}) },
      ]
      const team = await updateTeam(session, members)
      onApplied(team)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setApplying(false)
    }
  }, [playerPreset, plannerPreset, playerModel, plannerModel, session, onApplied])

  const presetOptions = (presets: Preset[], selected: string) => (
    <>
      <option value="">{loading ? '加载中…' : '请选择 preset'}</option>
      {presets.map((p) => (
        <option key={p.name} value={p.name}>
          {p.name.split('/').pop() ?? p.name}
        </option>
      ))}
      {/* 保持已物化配置的预选：物化后池内 preset 可能被删除（已物化成员不
          受影响，team-api.md §2 删除无 fan-out），此时补一个占位选项使当前
          配置可见且不会静默改为空值。 */}
      {selected !== '' && !presets.some((p) => p.name === selected) && (
        <option value={selected}>{selected.split('/').pop() ?? selected}</option>
      )}
    </>
  )

  return (
    <div className="team-settings-panel" data-testid="team-settings-panel">
      <div className="team-panel-header">
        <span>{materialized === null ? '设置 team（未物化）' : '设置 team（已物化，再次应用即刷新）'}</span>
        <Button data-testid="team-panel-close" onClick={onClose}>
          关闭
        </Button>
      </div>
      {materialized !== null && (
        <p className="team-refresh-hint" data-testid="team-refresh-hint">
          该会话已有 team：再次应用将终止在途回合、清空短期记忆并按新配置重建（刷新语义）。
        </p>
      )}
      {materialized !== null && (
        // 成员清单（web-views.md §1 状态呈现 + §5 查看入口）：每成员一项，
        // 提供只读 system prompt 全文入口。
        <div className="team-panel-members" data-testid="team-panel-members">
          <span className="team-panel-members-title">成员清单</span>
          {(materialized.members ?? []).map((member) => (
            <div
              key={member.role}
              className="team-panel-member"
              data-testid="team-panel-member"
              data-role={member.role}
            >
              <span className="team-panel-member-info">
                {member.role} · {presetTitle(member.preset)} ·{' '}
                {member.model !== undefined && member.model !== ''
                  ? member.model
                  : '默认模型'}
              </span>
              <Button
                data-testid={`member-system-prompt-${member.role}`}
                onClick={() => void prompt.open(member.role)}
              >
                查看 system prompt
              </Button>
            </div>
          ))}
        </div>
      )}
      {systemPrompt === undefined && <SystemPromptOverlay controller={ownSystemPrompt} />}
      <label className="team-panel-field" htmlFor="team-player-preset-select">
        <span>player preset（必选）</span>
        <select
          id="team-player-preset-select"
          data-testid="team-player-preset-select"
          value={playerPreset}
          onChange={(e) => setPlayerPreset(e.target.value)}
        >
          {presetOptions(playerPresets, playerPreset)}
        </select>
      </label>
      <label className="team-panel-field" htmlFor="team-planner-preset-select">
        <span>planner preset（必选）</span>
        <select
          id="team-planner-preset-select"
          data-testid="team-planner-preset-select"
          value={plannerPreset}
          onChange={(e) => setPlannerPreset(e.target.value)}
        >
          {presetOptions(plannerPresets, plannerPreset)}
        </select>
      </label>
      <label className="team-panel-field" htmlFor="team-player-model-select">
        <span>player model（留空 = 部署默认）</span>
        <select
          id="team-player-model-select"
          data-testid="team-player-model-select"
          value={playerModel}
          onChange={(e) => setPlayerModel(e.target.value)}
        >
          <option value="">默认</option>
          {models.map((m) => (
            <option key={m.id} value={m.id}>
              {m.id}
            </option>
          ))}
        </select>
      </label>
      <label className="team-panel-field" htmlFor="team-planner-model-select">
        <span>planner model（留空 = 部署默认）</span>
        <select
          id="team-planner-model-select"
          data-testid="team-planner-model-select"
          value={plannerModel}
          onChange={(e) => setPlannerModel(e.target.value)}
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
        <div className="team-panel-error" data-testid="team-settings-error" role="alert">
          {error}
        </div>
      )}
      <div className="team-panel-actions">
        <Button
          variant="primary"
          data-testid="team-apply"
          disabled={applying || loading}
          onClick={() => void apply()}
        >
          应用
        </Button>
      </div>
    </div>
  )
}

// isUnmaterializedError reports whether a GetTeam failure means "the team
// singleton is absent" (404 NOT_FOUND, team-api.md §1/§2)——anything else is a
// transport/other failure and must not trigger the guide.
export function isUnmaterializedError(err: unknown): boolean {
  return err instanceof ApiError && err.status === 404
}

// probeTeam fetches the session's team singleton and classifies the
// materialization status ('materialized' | 'unmaterialized' | 'unknown').
export async function probeTeam(
  session: string,
): Promise<{ status: 'materialized' | 'unmaterialized' | 'unknown'; team: Team | null }> {
  try {
    return { status: 'materialized', team: await getTeam(session) }
  } catch (err) {
    if (isUnmaterializedError(err)) return { status: 'unmaterialized', team: null }
    return { status: 'unknown', team: null }
  }
}
