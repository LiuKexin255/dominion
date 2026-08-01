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

### 2.1 输入路由与屏蔽（FR-031/FR-032）

- 用户输入路由给**当前选中且 `accepts_user_input=true`** 的 agent（saolei: player）。
- `accepts_user_input=false` 的 agent（saolei: planner）其 tab **屏蔽输入**（仅观察视图）。
- 屏蔽依据 `TeamAgent.accepts_user_input`（typed bool，从后端读，非硬编码）。

### 2.2 未知 agent 名称（edge case）

- 若 frame 的 `agent` 不在 `Team.agents` 内：归位策略（丢弃/归入默认 tab）由 `tasks.md` 定（约束：契约字段存在；不崩溃）。

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

- 新增本地 `Template` 常量（与 proto Template 资源一致）、`Team`/`TeamAgent` 接口。
- `AgentFrame.agentProfileName` → `agent`（D12）。
- `AgentProfile`/`Skill` 接口移除；新增 `TeamProfile`（含 oneof `spec`）/`SaoleiProfile`。
- 绑定 wrapper 随 §4 调整。

## 6. 验证要点

- 顶层模板切换基于本地常量、无网络请求（FR-024）。
- 对话多 tab，agent 列表来自 `Team.agents`（不硬编码）；frame 按 `agent` 归位（FR-025）。
- planner tab 屏蔽输入（`accepts_user_input=false`，FR-032）。
- saolei profile 页仅 player/planner 模型选择（FR-029），由 typed oneof 驱动。
