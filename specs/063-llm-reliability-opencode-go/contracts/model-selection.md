# Contract: 模型选择面（provider/model-id 复合标识）

**Feature**: [spec.md](../spec.md) FR-014, FR-018 | **决策**: [research.md](../research.md) D11 | **裁定**: spec Clarifications Session 2026-09-12（Option C）

**proto 基线**: `projects/game/agent_v2.proto` **零字段变更**——`Model.id`（`:548-551`）与 `TeamMember.model`（`:246-248`）承载复合标识字符串。

## 1. 标识语法与解析

- 形态：`${provider}/${model-id}`，例：`glm-responses/glm-5.3`、`opencode-go/kimi-k3`。
- 解析（服务侧**单一解析点** `projects/game/agent_v2/src/session.ts`）：按首个 `/` 切分 `(provider, model)`；无 `/`、provider 空段、model 空段 → `INVALID_ARGUMENT`（`TeamSessionError`，信息提示复合形态并指向 ListModels）。
- 默认值：空（UpdateTeam 省略 model 字段）= 部署默认 `glm-responses/${env.GLM_MODEL || "glm-5.3"}`；`GLM_MODEL` env 语义不变（裸 id + 隐含 glm provider）。

## 2. ListModels（PresetService，`GET /api/v2/models`）

- handler 从单 provider 查询改为联合：`ctx.llm.listProviders()` 遍历 → 每路由 `listModels(provider)` + `resolveModelInfo(provider, id)` 取 contextWindow。
- 条目 `Model.id = "${provider}/${model.id}"`，`context_window` 同现状投影（`projects/game/agent_v2/src/server.ts:943-980` 改造）。
- 顺序：provider 注册序 × 各自目录序（deterministic）。
- 联合目录仅含**已注册 adapter** 的 provider route（本部署即 `glm-responses` + `opencode-go`）；dormant/未注册路由不出现（dsh `listProviders()` 语义）。
- 错误语义：某 provider 目录查询失败 → RPC 失败（fail-loud，不静默截断目录）。

## 3. UpdateTeam / 校验 / 物化

- `TeamMember.model` 值域 = 复合标识或空（空 = 默认）。
- `validateModel(composite)`：切分 → 对**所选 provider** 的目录做**强校验**（沿用现状 `session.ts:890-902` 的部署目录校验；advisory 解析仅存在于插件 `resolveModel` 层，不改变选择面语义），错误信息含复合形态提示。
- 物化链：`doMaterialize` 切分两成员 model → `orchestrator.materialize({player: {preset, provider, model}, planner: {...}})`（`TeamMemberOptions.provider` 新增字段）→ `createMember` 构造 `agentOptions: {provider: member.provider ?? deps.provider ?? TEAM_PROVIDER, model}`（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts:586-591` 改造；`agentOptions.model` 恒为**裸 id**，system prompt `{{model}}` 渲染不受影响）。
- `deps.provider`（TeamSessions 注入 seam）保留为回退维度；生产路径 provider 全部来自复合标识切分。
- 输出侧（GetTeam/GetTeamMember/成员视图）：`model` 字段回填**复合标识**（物化输入的切分结果重组，或存储的复合原文；两者一致——单一解析点保证）。

## 4. web UI（选择与呈现）

- `TeamSettingsPanel`：下拉 option 值 = 复合标识原样（flatten，用户裁定）；"默认"空值语义不变（省略 model 字段）；provider/model 二级级联为纯前端可选演进，非本范围。
- `App.tsx` 成员 chip：复合标识原样渲染。
- UI 不感知 provider 插件实现（无 endpoint/token 概念）。
- 类型层（`api/agent.ts` 的 `Model.id`/`TeamMember.model`）值域注释更新为复合标识。

## 5. 兼容与迁移

- proto/gateway/proxy 零改动（字符串值域变化）。
- 旧裸 id 输入（若调用方未升级）：`INVALID_ARGUMENT`，错误信息给出复合形态示例——fail-loud，不静默猜测 provider。
- 既有单测/大型测试断言更新：目录 fixtures 改复合形态；`agent_v2_preset_test.go` 目录断言改为联合目录（2 provider × 各自条目）。

## 6. 测试义务

1. **session 单测**：复合切分（正常/无斜杠/空段）、按 provider 校验命中与拒绝、默认值重组、物化输入传递 `(provider, model)`、`agentOptions.model` 恒裸 id（system prompt `{{model}}` 渲染不变——复合标识只存在于选择面，不进入 agentOptions）。
2. **server 单测**：ListModels 联合目录（mock listProviders 多路由）、顺序确定性、单 provider 目录失败 → RPC 失败；UpdateTeam 复合/裸 id/空值三分支。
3. **orchestrator 单测**：`TeamMemberOptions.provider` 透传至 agentOptions（member.provider 优先级链）。
4. **web 单测**：下拉 option 复合值、默认空值提交省略字段、chip 渲染。
5. **大型测试**：`agent_v2_preset_test.go` 联合目录与 UpdateTeam 分支断言；`opencode-go/<model>` 多轮会话（含工具调用）在 `agent_v2_conversation_test.go`（SC-003，quickstart 场景 5；按模块归位，见 `style/large_test.md`）。
