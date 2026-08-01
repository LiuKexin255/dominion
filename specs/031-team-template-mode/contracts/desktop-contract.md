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

### 2.3 Team create-if-missing（决策 2，FR-033）

- Team 必须经 `CreateTeam` **显式创建**（AIP-133；GetTeam/Connect/ListMessages/RefreshTeam 未创建 → NOT_FOUND，无懒加载）。
- **desktop 流程（发送消息/进入会话时）**：`getTeam(template, sessionID)` → **失败（典型 NOT_FOUND）** → `createTeam(template, sessionID, defaultProfileResourceName)` → 继续（connect/sendUserTurn 前确保 Team 存在）。
- **默认 profile 简化（Phase 6）**：本阶段 desktop 固定使用默认 profile `templates/{template}/profiles/default`（AIP-122 完整资源名）；**profile 选择 UX 推迟到 Phase 7**（ProfileManagement / TeamProfile CRUD）。
- **并发安全**：重复 `CreateTeam` 且 profile 相同 → 幂等返回既有 Team（api-contract §2.2 幂等注），多 tab 竞态下 create 失败可重读 GetTeam 收敛。

## 3. Profile 页面特化（`ProfileManagement`）

- 按**当前模板**的 TeamProfile 渲染特化表单（FR-029）。
- 表单字段由 TeamProfile 的 **typed oneof 变体**驱动（`spec.saolei` → `SaoleiProfile{player_model, planner_model}`），**非通用 key-value 表单**、**非硬编码潜规则**（D1）。
- saolei：仅渲染 `player_model` / `planner_model` 选择；tools/mcp/skill 不可配置（FR-027，模板固定装配）。
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
| （无对应旧绑定） | `CreateTeam(template, sessionID, profile)` | **显式创建 Team（AIP-133，唯一创建点）**；`profile` 为 TeamProfile 完整资源名 `templates/{template}/profiles/{profile}`（AIP-122，template 段 MUST 与 parent 一致，handler 校验）；GetTeam→NotFound 时 frontend 调此绑定（create-if-missing，决策 2） |
| `List/Create/.../Update/DeleteAgentProfile` | `...TeamProfile(template, ...)` | TeamProfile CRUD |
| `List/Create/Get/DeleteSkill` | （移除） | Skill 废弃 |

- `SessionView` 增 `template` 字段；`TeamView`/`TeamAgentView` 新增；`AgentProfileView` → `TeamProfileView`（含 `SaoleiProfileView`）。

## 5. frontend 类型变更（`projects/game/desktop/frontend/src/api.ts`）

- 新增本地 `Template` 常量（`TEMPLATE_SAOLEI = "saolei"`，Template 路径段，与 proto Template 资源 / gameconst 常量一致，FR-024）与 `TEMPLATES` 列表（仅本地常量，无模板列表 API）。
- 新增 `Team`（`{name, sessionId, agents: TeamAgent[], createTime?}`）/`TeamAgent`（`{name, acceptsUserInput}`）接口——对应 Wails `TeamView`/`TeamAgentView`。
- `Session` 增 `template` 字段（Wails `SessionView`）。
- `AgentFrame.agentProfileName` → `agent`（D12）；`Message` 增 `agent` 字段（按 agent 分区，FR-005）。
- `AgentProfile`/`Skill` 接口移除；新增 `TeamProfile`（Wails `TeamProfileView`——typed oneof `spec.saolei` 被 Go view model 拍平为顶层 `playerModel`/`plannerModel`，未设置变体时缺失）/`SaoleiProfile`（`{playerModel, plannerModel}`）/`CreateTeamProfileRequest`/`ListTeamProfilesResponse`。
- 绑定 wrapper 随 §4 调整：`createSession(template)`/`connect(template, sessionID)`/`getTeam(template, sessionID)`/`createTeam(template, sessionID, profile)`/`refreshTeam(template, sessionID)`/`sendUserTurn(template, sessionID, text, screenshot..., agent)`/`listMessages(template, sessionID, agent)`/TeamProfile CRUD/`openChatStream(sessionID, agent)`。

## 6. 验证要点

- 顶层模板切换基于本地常量、无网络请求（FR-024）。
- 对话多 tab，agent 列表来自 `Team.agents`（不硬编码）；frame 按 `agent` 归位（FR-025）。
- planner tab 屏蔽输入（`accepts_user_input=false`，FR-032）。
- saolei profile 页仅 player/planner 模型选择（FR-029），由 typed oneof 驱动。
- 发送消息/进入会话前 Team 存在（GetTeam→NotFound→CreateTeam(默认 profile)，决策 2）。
