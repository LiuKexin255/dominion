# Contract: Desktop 模板控制面 + 多标签页 + Profile 特化

**Feature**: `031-team-template-mode` | **Spec**: [`spec.md`](../spec.md) | **Research**: D3/D12

> desktop（Wails + Svelte）改造：模板作为顶层控制面（本地常量），session 对话页多标签页（按 team agent），frame 按 agent 归位，profile 页按模板特化（typed oneof）。**通用优先、typed、禁硬编码 agent 名/禁潜规则**（directive ②）。

---

## 1. 模板控制面（顶层）

- desktop 持**本地 Template 常量**（`saolei`，与 proto Template 资源 / gameconst 常量一致，FR-024）。
- 顶层切换模板基于本地常量，**不发起模板列表 API 请求**（FR-024）。
- 不同模板：**大部分页面通用**，**少数页面特性化**（FR-026）。

## 2. 多标签页对话（`ChatView` / `App.svelte`）

- 进入会话后，对话区按 team 内 agent 分多个 tab，每 tab 对应一个 agent（FR-025）。
- **agent 列表来自后端** `Team.agents`（typed `TeamAgent[]`，GetTeam 返回，D3），**desktop 不硬编码 agent 名**（通用渲染）。
- frame 按 `AgentFrame.agent`（D12，取代 `agent_profile_name`）归入对应 agent 的 tab。
- 现有按 `agentProfileName` 合并消息的逻辑（`App.svelte` `handleMessageParts`）改为按 `agent` 归位 + 合并。
- **SSE 流**：Go chatstream Registry 为 per-session 单流（每次 Open 轮换 token 踢掉旧订阅者，`desktop/internal/chatstream/stream.go`），故 frontend **每 session 只开一条流**（`openChatStream(sessionID, 首个 agent)`，seed = 该 agent 的消息分区）；其余 agent 的历史经 `listMessages(template, sessionID, agent)` 按 tab 拉取；live frame 按 `agent` 归位。seed replay 的 frame 不带 `agent` 字段（Go `SeedFromHistory` 省略），按 seed agent 归位，并按 `frameId` 与 listMessages 历史去重。

### 2.1 输入路由与屏蔽（FR-031/FR-032）

- 用户输入路由给**当前选中且 `accepts_user_input=true`** 的 agent（saolei: player）。
- `accepts_user_input=false` 的 agent（saolei: planner）其 tab **屏蔽输入**（仅观察视图）。
- 屏蔽依据 `TeamAgent.accepts_user_input`（typed bool，从后端读，非硬编码）。

### 2.2 未知 agent 名称（edge case）

- 若 frame 的 `agent` 不在 `Team.agents` 内：**归入默认 tab（`Team.agents` 首个 agent）并打 warn 日志**（丢弃会丢数据；归入默认 tab 保证可观测、不崩溃；约束：契约字段存在）。

### 2.3 Team create-if-missing（决策 2，FR-033）**[SUPERSEDED — see Feature 040]**

- Team 必须经 `CreateTeam` **显式创建**（AIP-133；GetTeam/Connect/ListMessages/RefreshTeam 未创建 → NOT_FOUND，无懒加载）。**[SUPERSEDED — see Feature 040]**: [`specs/040-team-singleton-conformance/`](../../040-team-singleton-conformance/) supersedes 本条——Team 为 [AIP-156](https://google.aip.dev/156) 单例，MUST NOT 暴露 `CreateTeam`，MUST 经 `UpdateTeam(allow_missing=true)` 物化（040 FR-001/FR-002）；"未创建 → NOT_FOUND"不变量保留。
- **desktop 流程（发送消息/进入会话时）**：`getTeam(template, sessionID)` → **失败（典型 NOT_FOUND）** → `createTeam(template, sessionID, defaultProfileResourceName)` → 继续（connect/sendUserTurn 前确保 Team 存在）。**[SUPERSEDED — see Feature 040]**: desktop 流程改为 GetTeam→NOT_FOUND→**单次** `updateTeam(template, sessionID, profile, [], true)`（040 T007-T009 已实现），移除 create 失败后的 GetTeam 兜底重读（`allow_missing` 天然收敛）。
- **默认 profile 简化（Phase 6）**：本阶段 desktop 固定使用默认 profile `templates/{template}/profiles/default`（AIP-122 完整资源名）；**profile 选择 UX 推迟到 Phase 7**（ProfileManagement / TeamProfile CRUD）。**[SUPERSEDED — see Feature 040]**: 040 后 desktop 经 profile 弹窗（`handleProfileSelected`）显式选择 profile 并经单次 `updateTeam(allowMissing=true)` 物化（040 T009），无固定默认 profile。
- **并发安全**：重复 `CreateTeam` 且 profile 相同 → 幂等返回既有 Team（api-contract §2.2 幂等注），多 tab 竞态下 create 失败可重读 GetTeam 收敛。**[SUPERSEDED — see Feature 040]**: 幂等由 `UpdateTeam(allow_missing=true)` 天然保证（040 FR-002），原"异 profile → ALREADY_EXISTS"的 AIP-133 偏离已移除（040 FR-007）。

## 3. Profile 页面特化（`ProfileManagement`）

- 按**当前模板**的 TeamProfile 渲染特化表单（FR-029）。
- 表单字段由 TeamProfile 的 **typed oneof 变体**驱动（`spec.saolei` → `SaoleiProfile{player_model, planner_model, player_prompt, planner_prompt}`），**非通用 key-value 表单**、**非硬编码潜规则**（D1）。
- saolei：渲染 `player_model` / `planner_model` 选择 + `player_prompt` / `planner_prompt` 输入（textarea，空值表示使用模板默认 base，FR-034）；tools/mcp/skill 不可配置（FR-027，模板固定装配）。
- CRUD 经新 TeamProfile 绑定（取代 AgentProfile 绑定，`app.go` `List/Create/Get/Delete/UpdateAgentProfile` → `...TeamProfile`）。

## 4. Wails 绑定变更（`projects/game/desktop/app.go`）

| 旧绑定 | 新绑定 | 说明 |
|---|---|---|
| `CreateSession()` | `CreateSession(template)` | 指定模板（Template 资源名） |
| `ConnectAgent(sessionID)` | `Connect(template, sessionID)` | FR-004 端点 |
| `RefreshAgent(sessionID)` | `RefreshTeam(template, sessionID)` | FR-008 |
| `SendUserTurn(..., agentProfileName)` | `SendUserTurn(..., agent)` | D12；agent 名称 |
| `ListMessages(sessionID)` | `ListMessages(template, sessionID, agent)` | 按 agent 分区（FR-005） |
| `GetAgent(sessionID)` | `GetTeam(template, sessionID)` | 返回 Team（含 agents） |
| （无对应旧绑定） | `CreateTeam(template, sessionID, profile)` | **显式创建 Team（AIP-133，唯一创建点）**；`profile` 为 TeamProfile 完整资源名 `templates/{template}/profiles/{profile}`（AIP-122，template 段 MUST 与 parent 一致，handler 校验）；GetTeam→NotFound 时 frontend 调此绑定（create-if-missing，决策 2）**[SUPERSEDED — see Feature 040]**: 绑定已改为 `UpdateTeam(template, sessionID, profile, updateMaskPaths, allowMissing)`（PATCH + `allow_missing` query，AIP-134/156 物化；040 T007/T008 已实现） |
| `List/Create/.../Update/DeleteAgentProfile` | `...TeamProfile(template, ...)` | TeamProfile CRUD |
| `List/Create/Get/DeleteSkill` | （移除） | Skill 废弃 |

- `SessionView` 增 `template` 字段；`TeamView`/`TeamAgentView` 新增；`AgentProfileView` → `TeamProfileView`（含 `SaoleiProfileView`）。

## 5. frontend 类型变更（`projects/game/desktop/frontend/src/api.ts`）

- 新增本地 `Template` 常量（`TEMPLATE_SAOLEI = "saolei"`，Template 路径段，与 proto Template 资源 / gameconst 常量一致，FR-024）与 `TEMPLATES` 列表（仅本地常量，无模板列表 API）。
- 新增 `Team`（`{name, sessionId, agents: TeamAgent[], createTime?}`）/`TeamAgent`（`{name, acceptsUserInput}`）接口——对应 Wails `TeamView`/`TeamAgentView`。
- `Session` 增 `template` 字段（Wails `SessionView`）。
- `AgentFrame.agentProfileName` → `agent`（D12）；`Message` 增 `agent` 字段（按 agent 分区，FR-005）。
- `AgentProfile`/`Skill` 接口移除；新增 `TeamProfile`（Wails `TeamProfileView`——typed oneof `spec.saolei` 被 Go view model 拍平为顶层 `playerModel`/`plannerModel`/`playerPrompt`/`plannerPrompt`，未设置变体时缺失）/`SaoleiProfile`（`{playerModel, plannerModel, playerPrompt, plannerPrompt}`）/`CreateTeamProfileRequest`/`ListTeamProfilesResponse`。
- 绑定 wrapper 随 §4 调整：`createSession(template)`/`connect(template, sessionID)`/`getTeam(template, sessionID)`/`updateTeam(template, sessionID, profile, updateMaskPaths, allowMissing)`（**[SUPERSEDED — see Feature 040]**：原 `createTeam(template, sessionID, profile)` 已移除，040 T007-T009）/`refreshTeam(template, sessionID)`/`sendUserTurn(template, sessionID, text, screenshot..., agent)`/`listMessages(template, sessionID, agent)`/TeamProfile CRUD/`openChatStream(sessionID, agent)`。

## 6. 验证要点

- 顶层模板切换基于本地常量、无网络请求（FR-024）。
- 对话多 tab，agent 列表来自 `Team.agents`（不硬编码）；frame 按 `agent` 归位（FR-025）。
- planner tab 屏蔽输入（`accepts_user_input=false`，FR-032）。
- saolei profile 页渲染 player/planner 模型选择 + base 提示词输入（FR-029/FR-034），由 typed oneof 驱动。
- 发送消息/进入会话前 Team 存在（GetTeam→NotFound→CreateTeam(默认 profile)，决策 2）。**[SUPERSEDED — see Feature 040]**: 改为 GetTeam→NotFound→`updateTeam(allowMissing=true)`（040 T009 已实现）。
