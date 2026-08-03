# Contract: API（proto 资源层级 + 服务 + RPC）

**Feature**: `031-team-template-mode` | **Spec**: [`spec.md`](../spec.md) | **Research**: D1/D2/D3/D12

> AIP 风格（https://google.aip.dev）gRPC API 契约。clean break：移除 Agent/AgentProfile/Skill/RefreshAgent，新增 Template（资源消息）/Team/TeamProfile/RefreshTeam。**通用优先、typed oneof/枚举、禁 blob/禁潜规则**（directive ②）。本契约给出语义与关键字段；字段号由实现时 protoc 流程确定并保留 reserved 卫生。

---

## 1. 资源层级（资源 pattern）

| 资源 | pattern | 说明 |
|---|---|---|
| **Session** | `templates/{template}/sessions/{session}` | 挂到模板下（FR-002）；移除顶层 `sessions/*` |
| **Team** | `templates/{template}/sessions/{session}/team` | 取代 Agent（FR-003） |
| **Message** | `templates/{template}/sessions/{session}/team/agents/{agent}/messages/{message}` | 按 team 内 agent 分区（FR-005）；`{agent}`=agent 名称 |
| **TeamProfile** | `templates/{template}/profiles/{profile}` | 取代 AgentProfile（FR-006）；prompt 服务管理 |
| **Template** | `templates/{template}` | 资源消息（`message Template`），无 CRUD/List RPC（FR-001）；具体值由 gameconst 常量表示 |

`{template}` ∈ Template 资源名（gameconst 常量，当前仅 `saolei`）。`{agent}` ∈ 模板 graph schema 已知 agent 名称（saolei: `player`/`planner`）。

---

## 2. 服务与 RPC

### 2.1 SessionService（`/api/v1/templates/{template}/sessions`）

| RPC | 方法/路径 | 说明 |
|---|---|---|
| `CreateSession` | POST `/api/v1/{parent=templates/*}/sessions` | parent=`templates/{template}`；AIP-133 |
| `ListSessions` | GET `/api/v1/{parent=templates/*}/sessions` | AIP-132 |
| `GetSession` | GET `/api/v1/{name=templates/*/sessions/*}` | AIP-131 |
| `DeleteSession` | DELETE `/api/v1/{name=templates/*/sessions/*}` | AIP-135 |

### 2.2 TeamService（取代 ProxyService；`templates/{template}/sessions/{session}/...`）

> 原 `ProxyService`/`AgentService`（重复）合并为 TeamService。**Team 由 `CreateTeam` 显式创建（设计决策：新增 CreateTeam RPC，Agent 移除懒加载模式）**——不再随 Connect 隐式存在。`GetTeam`/`Connect`/`ListMessages`/`RefreshTeam` 均要求 Team 已创建（未创建 → NOT_FOUND）；profile 由 CreateTeamRequest 显式传入，不再使用固定默认 profile。

| RPC | 方法/路径 | 说明 |
|---|---|---|
| `CreateTeam` | POST `/api/v1/{parent=templates/*/sessions/*}/team`（body `"*"`） | **AIP-133 显式创建（唯一创建点）**：请求携带 parent Session + profile（TeamProfile 完整资源名 `templates/{template}/profiles/{profile}`，AIP-122——template 段 MUST 与 parent 一致，handler 校验，禁潜规则）；响应为 Team 资源。代理侧为唯一 owner 分配点；重复 create：**profile 相同 → 幂等返回既有 Team；profile 不同 → ALREADY_EXISTS（details 携带既有 profile）**（per-session 单例，desktop create-if-missing 流程可安全重试；仅 profile 相同时才不返回 AIP-133 严格 ALREADY_EXISTS，理由：单例资源 + 桌面竞态场景，见下注） |
| `GetTeam` | GET `/api/v1/{name=templates/*/sessions/*/team}` | 返回 Team（含 `agents` 描述，D3）；未创建 → NOT_FOUND |
| `Connect` | bidi stream（无 REST） | 端点 `templates/{template}/sessions/{session}/connect`（FR-004）；stream `AgentFrame`。**不分配 owner/不创建 Team**——未创建 → NOT_FOUND（agent 端经 stream error 通道下发该状态）。**帧路由对**：frame 携带 `template_id`+`session_id`（均裸段；gateway 从 connect URL 路径注入两字段，覆盖客户端值）；proxy 据此对直接构造 Session 资源名（`game.SessionName{TemplateID, SessionID}`，不再解析全名） |
| `ListMessages` | GET `/api/v1/{parent=templates/*/sessions/*/team/agents/*}/messages` | 按 agent 分区（FR-005）；未创建 → NOT_FOUND |
| `RefreshTeam` | POST `/api/v1/{name=templates/*/sessions/*/team}:refresh` | 取代 RefreshAgent（FR-008）；清空短期记忆（FR-018）；未创建 → NOT_FOUND |

> **幂等 create 注（相对 AIP-133 的偏离，用户细化决策）**：AIP-133（https://google.aip.dev/133）要求重复创建返回 `ALREADY_EXISTS`。本契约将 Team 视为 per-session 单例（资源 id 为字面量 `team`），且 desktop 的"发送消息时 NotFound 则 CreateTeam"流程天然存在并发重试（多标签页竞态）——**重复 create 且 profile 相同**时返回既有 Team（幂等）而非 ALREADY_EXISTS，避免竞态下桌面收到伪错误；**重复 create 且 profile 不同**时返回 ALREADY_EXISTS（details 携带既有 profile），因该重入并非幂等重试而是配置不一致。profile 比较在 agent 层（`SessionTeamStore.create`，map 记录每 session 创建时所用 profile）；proxy 层 owner 分配独立幂等（`assignOwner` 在 `ErrOwnerAlreadyExists` 并发竞态下重读既有 owner 而非报错）。该偏离在实现与测试中显式记录。

`Connect` 用户输入帧路由给"接受用户输入"的 agent（FR-032；saolei 中 player）。frame `AgentFrame.agent` 标识来源 agent。

### 2.3 PromptService（`templates/{template}/profiles`；仅 TeamProfile 静态配置）

> 移除 AgentProfile/Skill RPC；管理 TeamProfile CRUD。**Strategy 不经 prompt 服务**（strategy 由 agent 服务自身持久化到 mongo，见 [`strategy-store-contract.md`](./strategy-store-contract.md)）。

**TeamProfile（公开 REST）**:

| RPC | 方法/路径 | 说明 |
|---|---|---|
| `CreateTeamProfile` | POST `/api/v1/{parent=templates/*}/profiles` | AIP-133；校验 `template` 与 oneof 变体一致 |
| `GetTeamProfile` | GET `/api/v1/{name=templates/*/profiles/*}` | AIP-131 |
| `ListTeamProfiles` | GET `/api/v1/{parent=templates/*}/profiles` | AIP-132 |
| `UpdateTeamProfile` | PATCH `/api/v1/{team_profile.name=templates/*/profiles/*}` | AIP-134；update_mask（含 oneof 成员路径，实现需谨慎） |
| `DeleteTeamProfile` | DELETE `/api/v1/{name=templates/*/profiles/*}` | AIP-135 |

---

## 3. 关键消息（语义）

### 3.1 `Template`（资源消息，无 RPC）

```proto
message Template {
  option (google.api.resource) = {
    type: "game.liukexin.com/Template"
    pattern: "templates/{template}"
    singular: "template"
    plural: "templates"
  };
  string name = 1 [(Identify)];
}
```

无任何 RPC（FR-001）。存在目的：(1) `Session.template`/`TeamProfile.template` 的 typed 引用目标；(2) 驱动 `protoc-gen-go-aip` codegen 生成 `ParseTemplateName`/`TemplateName`，资源名解析全由 codegen 承担（无手写 parent 解析）。具体模板值（当前仅 `saolei`）以 gameconst 常量表示（`SaoleiTemplate = game.TemplateName{TemplateID: "saolei"}`），非 proto enum、非裸 string。

### 3.2 `Session`

```proto
message Session {
  option (google.api.resource) = { type: "game.liukexin.com/Session"
    pattern: "templates/{template}/sessions/{session}" ... };
  string name = 1 [(Identify)];
  string template = 2 [OutputOnly, (google.api.resource_reference) = { type: "game.liukexin.com/Template" }];
      // 值 = 模板资源名（如 "templates/saolei"）；OUTPUT_ONLY，创建时由父路径派生
  string session_id = 3 [OutputOnly];
  google.protobuf.Timestamp create_time = 4 [OutputOnly];
}
```

### 3.3 `Team` / `TeamAgent`

```proto
message TeamAgent {
  string name = 1;                // player | planner
  bool accepts_user_input = 2;    // FR-031
}
message Team {
  option (google.api.resource) = { pattern: "templates/{template}/sessions/{session}/team" ... };
  string name = 1;
  repeated TeamAgent agents = 2;  // 来自模板 graph schema（D3）
  google.protobuf.Timestamp create_time = 3;
}
```

### 3.4 `Message`

```proto
message Message {
  option (google.api.resource) = {
    pattern: "templates/{template}/sessions/{session}/team/agents/{agent}/messages/{message}" ... };
  string name = 1;
  string message_id = 2;
  FrameSender sender = 3;
  string agent = 4;               // 所属 team 内 agent 名称（与路径 {agent} 一致）
  google.protobuf.Timestamp create_time = 5;
  MessageParts content = 6;       // display only，与 live frame 同形（不变）
}
```
> `MessageParts`/`FlowPart`/各 Part 消息（spec 023/025/030 既定）**不变**，仅 `Message` 资源 pattern 与 `agent` 字段变更。

### 3.5 `TeamProfile` / `SaoleiProfile`（oneof 特化）

```proto
message TeamProfile {
  option (google.api.resource) = { pattern: "templates/{template}/profiles/{profile}" ... };
  string name = 1;
  string template = 2 [Required, (google.api.resource_reference) = { type: "game.liukexin.com/Template" }];
      // 值 = 模板资源名；客户端提供，handler 校验与 parent 及 oneof 变体一致（禁潜规则）
  google.protobuf.Timestamp create_time = 3;
  google.protobuf.Timestamp update_time = 4;
  oneof spec {
    SaoleiProfile saolei = 10;    // typed 模板特化（D1）
  }
}
message SaoleiProfile {
  string player_model = 1;
  string planner_model = 2;
  string player_prompt = 3;   // 可选；空字符串=未设置=回退模板默认 base（FR-034，见 [`spec.md`](../spec.md)）
  string planner_prompt = 4;  // 可选；空字符串=未设置=回退模板默认 base（FR-034，见 [`spec.md`](../spec.md)）
}
```

### 3.6 `AgentFrame`（字段变更，D12）

```proto
message AgentFrame {
  string session_id = 1 [Required];   // 裸 session id（{session} 段）
  string frame_id = 2;
  google.protobuf.Timestamp create_time = 3;
  FrameSender sender = 6;
  string agent = 7;               // 取代 agent_profile_name（field 7 重命名）
  string template_id = 8 [Required];   // 裸 template id（{template} 段）；与 session_id 构成路由对——gateway 从 connect URL 注入，proxy 据此构造 Session 资源名（§2.2）
  oneof payload { MessageParts message_parts = 11; FlowParts flow_parts = 12; }
}
```

---

## 4. 移除清单（clean break）

- 服务：`ProxyService`、`AgentService`（→ 合并为 `TeamService`）。
- 资源/RPC：`Agent`、`AgentProfile`（含 Create/Get/List/Update/Delete）、`Skill`（含 Create/Get/List/Delete）、`RefreshAgent`、`ConnectAgent`（→ `Connect`）。
- 字段：`Agent.agent_profile_name`、`AgentFrame.agent_profile_name`、`prompts/` 单例命名空间（`gameconst.PromptsParent`）。
- `enum Template`（设计修订移除）：具体模板值改由 gameconst 常量（`game.TemplateName` 资源对象）表示，非 proto enum。
- 保留：MCP 配套内置 skill（`projects/game/agent/src/skill/`，FR-007）；`MessageParts`/`FlowPart` 等 Part 模型（spec 023/025/030）。

## 5. 资源名解析（codegen 驱动）

- 资源名解析全部由 `protoc-gen-go-aip` codegen 生成（`dominion/projects/game` 包）：`ParseTemplateName`/`ParseSessionName`/`ParseTeamName`/`ParseTeamProfileName`/`ParseMessageName`，及各消息的 `ParseName()`/`ParseTemplate()`/`Parent()`——不再手写 parent 解析。
- `gameconst` 不再含手写资源名解析器，仅保留：gRPC target 常量（`SessionTarget`/`TeamTarget`/`AgentTarget`/`PromptTarget`）、log field 常量、Template 常量与校验（`SaoleiTemplate`/`ValidateTemplateName`/`IsKnownTemplateID`/`ErrInvalidTemplate`）。
- 移除 `AgentProfileName`/`SkillName`/`PromptsParent` 相关。

## 6. 验证要点

- Template 无 CRUD/List RPC；模板引用为 Template 资源（`string` + `resource_reference`，codegen 解析）；具体值由 gameconst 常量表示（非裸 string、非 proto enum）。
- TeamProfile `template` 字段与 oneof 变体一致性由 handler 校验（禁潜规则）。
- `Team.agents` typed 暴露，desktop 通用渲染（不硬编码 agent 名）。
- Strategy 由 agent 服务自身持久化（mongo），不经 prompt 服务、无公开 REST（见 `strategy-store-contract.md`）。
