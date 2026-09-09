// /api/v2 配置面 API 客户端（PresetService + AgentService 单例面，契约
// specs/051-agent-v2-dsh-migration/contracts/agent-api.md §1/§2；web 消费面
// specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §2/§3）。
// 路由分工（agent-api.md §4）：preset CRUD 与 ListModels 由 gateway 直连
// agent-v2；Agent 单例（AIP-156）经 proxy 两跳。全部类型为 protojson 投影：
// camelCase 字段名、Timestamp 为 RFC 3339 字符串；错误复用
// api/conversation.js 的 ApiError/requestJson（grpc-gateway gRPC→HTTP 映射）。

import { ApiError, requestJson } from './conversation.js'

// Preset per agent_v2.proto Preset（protojson 投影）；persona 空 =
// 物化时回退默认 base（data-model.md §2.1）。
export interface Preset {
  name: string
  persona?: string
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

// Agent 是 session 的 agent 单例资源（AIP-156）；model 空 = 进程默认。
// desktopConnected 为该 session 的桌面桥接连接事实（agent 侧注册表直读，
// specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md §4）——proto3
// 缺省 false 经 protojson 不输出，可选语义正好（缺字段 = 未连接）。
export interface Agent {
  name: string
  preset: string
  model?: string
  createTime?: string
  updateTime?: string
  desktopConnected?: boolean
}

// ─── PresetService（gateway 直连面） ─────────────────────────────────────────

export async function listPresets(template: string): Promise<Preset[]> {
  const res = await requestJson<ListPresetsResponse>(
    `/api/v2/templates/${template}/presets`,
  )
  return res.presets ?? []
}

// createPreset 以 caller-supplied id 建资源（AIP-133）；id 走 query 参数
// （body:"preset" 绑定），body 即 Preset 本体。
export async function createPreset(
  template: string,
  presetId: string,
  persona: string,
): Promise<Preset> {
  return requestJson<Preset>(
    `/api/v2/templates/${template}/presets?preset_id=${encodeURIComponent(presetId)}`,
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

// listModels 返回部署级只读模型目录（与 UpdateAgent 校验同源，
// agent-api.md §2.6）。
export async function listModels(): Promise<Model[]> {
  const res = await requestJson<ListModelsResponse>('/api/v2/models')
  return res.models ?? []
}

// ─── AgentService 单例面（proxy 两跳） ───────────────────────────────────────

// getAgent 未物化时得到 404（agent-api.md §2.2）——调用方以 ApiError.status
// 区分引导态。
export async function getAgent(session: string): Promise<Agent> {
  return requestJson<Agent>(`/api/v2/${session}/agent`)
}

// updateAgent 物化/刷新 agent 单例（AIP-134 create-or-update）。body 不携带
// name：grpc-gateway 对 PATCH 从 body 字段派生 update_mask（runtime
// FieldMaskFromRequestBody，仅当 mask 为空时），name 进 body 会派生出服务端
// 拒绝的 "name" mask 路径——身份由 URL 路径变量承载（与 testplan
// agent_v2_helpers_test.go updateAgentV2Agent 同一规则）。model 空 = 进程
// 默认，body 省略该字段。
export async function updateAgent(
  session: string,
  preset: string,
  model: string,
): Promise<Agent> {
  return requestJson<Agent>(
    `/api/v2/${session}/agent?allow_missing=true`,
    {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(model === '' ? { preset } : { preset, model }),
    },
  )
}

// cancelAgent 终止 session 在途回合并落地排队消息（AIP-136 自定义方法，
// specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md §3）。请求仅
// name 路径参数（body:"*" 注解下 body 为空对象）；幂等——无在途回合且无
// 队列时成功 no-op；未物化 → 400/FAILED_PRECONDITION（与 Send 前置错误
// 同族）。终态经流上 turn_end{CANCELED} 呈现，本调用只承载请求级结果。
export async function cancelAgent(session: string): Promise<void> {
  await requestJson(`/api/v2/${session}/agent:cancel`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
  })
}
