// /api/v2 配置面与 team 面 API 客户端（PresetService + AgentService team 模型，
// 契约 specs/059-agent-v2-team-mode/contracts/team-api.md 与
// contracts/preset-api.md）。会话面 proto 为场景无关 team 原语（2026-09-10
// 用户裁定）：role/member/sender 均为字符串——保留值 "user" 标注用户消息、
// 成员 role 为场景词汇（saolei 下 "player"/"planner"）；Team 物化输入为
// members 列表。路由分工（team-api.md §7）：preset CRUD 与 ListModels 由
// gateway 直连 agent-v2；team 单例（AIP-156）经 proxy 两跳。全部类型为
// protojson 投影：camelCase 字段名、Timestamp 为 RFC 3339 字符串；错误复用
// api/conversation.js 的 ApiError/requestJson（grpc-gateway gRPC→HTTP 映射）。

import type { HistoryMessage, TeamMessage } from './conversation.js'
import { ApiError, requestJson } from './conversation.js'

// Preset per agent_v2.proto Preset（protojson 投影）；role 为场景词汇字符串
// （saolei 下 "player"/"planner"）——create 必填不可变、决定所属池与绑定的
// 工具插件行（preset-api.md §2）；persona 空 = 物化时回退该角色默认 base。
export interface Preset {
  name: string
  persona?: string
  role?: string
  createTime?: string
  updateTime?: string
}

export interface ListPresetsResponse {
  presets?: Preset[]
  nextPageToken?: string
}

// Model per agent_v2.proto Model：只读目录项，无端点/token 信息。
export interface Model {
  id: string
  contextWindow?: number
}

export interface ListModelsResponse {
  models?: Model[]
}

// TeamMember is one team member carrier: the caller-supplied materialization
// configuration and the runtime snapshot share one shape (data-model.md §2).
// Input carries role (non-empty scenario vocabulary) + preset + optional model
// (empty = deployment default); name/system_prompt are server-filled on
// output (system_prompt only through GetTeamMember).
export interface TeamMember {
  name?: string
  role: string
  preset: string
  model?: string
  systemPrompt?: string
}

// Team is the session's team singleton resource (AIP-156): exactly one per
// session, materialized/refreshed via UpdateTeam (no Create/Delete RPC;
// contracts/team-api.md §1/§2). members is the materialization input and the
// member-state output (same shape); desktopConnected 为该 session 的桌面桥接
// 连接事实（player 独占使用，proto3 缺省 false 经 protojson 不输出——缺字段
// = 未连接）。
export interface Team {
  name: string
  members?: TeamMember[]
  desktopConnected?: boolean
  createTime?: string
  updateTime?: string
}

export interface ListTeamMessagesResponse {
  messages?: TeamMessage[]
  nextPageToken?: string
}

// MemberViewMessage is one member-view history entry (ListMemberMessages):
// sender marks the injected source ("user" = user input, a member role string
// = team broadcast relay, raw wire value) — rendered as `user: [sender]…` for
// other members (contracts/team-api.md §5).
export interface MemberViewMessage {
  message: HistoryMessage
  sender?: string
}

export interface ListMemberMessagesResponse {
  messages?: MemberViewMessage[]
  nextPageToken?: string
}

// ─── PresetService（gateway 直连面） ─────────────────────────────────────────

// listPresets optionally filters by role pool（preset-api.md §1：role 过滤
// 参数为场景词汇字符串，空 = 不过滤）——物化面板的两个下拉分别取
// "player"/"planner" 池，数据源与 UpdateTeam 的提交校验同源。
export async function listPresets(template: string, role?: string): Promise<Preset[]> {
  const base = `/api/v2/templates/${template}/presets`
  const query = role !== undefined && role !== '' ? `?role=${encodeURIComponent(role)}` : ''
  const res = await requestJson<ListPresetsResponse>(`${base}${query}`)
  return res.presets ?? []
}

// createPreset 以 caller-supplied id 建资源（AIP-133）；id 与请求级 role 走
// query 参数（body:"preset" 绑定下非 body 字段映射为 query），body 即 Preset
// 本体。role 为场景词汇字符串（preset 分池与 copy-then-patch 拷贝源），
// create 必填且创建后不可变（preset-api.md §2）。
export async function createPreset(
  template: string,
  presetId: string,
  persona: string,
  role: string,
): Promise<Preset> {
  return requestJson<Preset>(
    `/api/v2/templates/${template}/presets?preset_id=${encodeURIComponent(presetId)}&role=${encodeURIComponent(role)}`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ persona }),
    },
  )
}

export async function getPreset(name: string): Promise<Preset> {
  return requestJson<Preset>(`/api/v2/${name}`)
}

// updatePreset 仅可变字段 persona（AIP-134）；显式 update_mask 经
// query 传递（body:"preset" 绑定下 mask 无法进 body），服务端校验 mask 路径。
export async function updatePreset(
  name: string,
  persona: string,
): Promise<Preset> {
  return requestJson<Preset>(`/api/v2/${name}?update_mask=persona`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ persona }),
  })
}

export async function deletePreset(name: string): Promise<void> {
  const res = await fetch(`/api/v2/${name}`, { method: 'DELETE' })
  if (!res.ok) throw new ApiError(res.status, await res.text())
}

// listModels 返回部署级只读模型目录（与 UpdateTeam 校验同源，
// team-api.md §2）。
export async function listModels(): Promise<Model[]> {
  const res = await requestJson<ListModelsResponse>('/api/v2/models')
  return res.models ?? []
}

// ─── AgentService team 面（proxy 两跳） ──────────────────────────────────────

// getTeam 未物化时得到 404（team-api.md §1/§2）——调用方以 ApiError.status
// 区分引导态。
export async function getTeam(session: string): Promise<Team> {
  return requestJson<Team>(`/api/v2/${session}/team`)
}

// updateTeam 物化/刷新 team 单例（AIP-134 create-or-update），提交
// team.members 成员配置列表。HTTP body 即 Team 资源本体（gateway 注解
// body:"team"）：只序列化输入侧字段 {role, preset, model?}，name 等
// output-only 字段不进 body——grpc-gateway 对 PATCH 从 Body 的 Team 派生
// update_mask（runtime FieldMaskFromRequestBody，仅当 mask 为空时；repeated
// 字段停在 "members"），name 进 body 会派生出服务端拒绝的 "name" mask 路径
// （team-api.md §1/§2）。model 空 = 部署默认，body 省略该字段。
export async function updateTeam(session: string, members: TeamMember[]): Promise<Team> {
  const body = {
    members: members.map((member) =>
      member.model !== undefined && member.model !== ''
        ? { role: member.role, preset: member.preset, model: member.model }
        : { role: member.role, preset: member.preset },
    ),
  }
  return requestJson<Team>(`/api/v2/${session}/team?allow_missing=true`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

// cancelTeam 终止 team 在途回合（无论哪个成员被驱动）并暂停编排自动续驱
// （AIP-136 自定义方法，team-api.md §4）。请求仅 name 路径参数（body:"*"
// 注解下 body 为空对象）；幂等——无在途回合时为 no-op 成功；未物化 →
// 400/FAILED_PRECONDITION（与 Send 前置错误同族）。终态经流上
// turn_end{CANCELED} 呈现，本调用只承载请求级结果。
export async function cancelTeam(session: string): Promise<void> {
  await requestJson(`/api/v2/${session}/team:cancel`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
  })
}

// listTeamMessages 回填团队视图归并序列（AIP-132，team-api.md §5）；seq 与
// team 流的 team_message 帧同源，保证实时归并序与回填一致。
export async function listTeamMessages(session: string): Promise<TeamMessage[]> {
  const res = await requestJson<ListTeamMessagesResponse>(
    `/api/v2/${session}/team/messages`,
  )
  return res.messages ?? []
}

// listMemberMessages 回填成员视角历史（AIP-132，team-api.md §5）：sender
// 为字符串来源标注，前端对成员来源渲染 `user: [sender]…`。
export async function listMemberMessages(
  session: string,
  member: string,
): Promise<MemberViewMessage[]> {
  const res = await requestJson<ListMemberMessagesResponse>(
    `/api/v2/${session}/team/members/${member}/messages`,
  )
  return res.messages ?? []
}
