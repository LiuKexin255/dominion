# Data Model: Agent v2 team 模式优化

> 实体与状态模型。字段类型为概念类型（proto/TS 形态由实现承载）；行为契约见 [contracts/](contracts/)。
> 本文只描述**变更**；未提及的 059 实体（Team/TeamMember/Preset 资源、团队消息流、编排状态机）语义不变（`specs/059-agent-v2-team-mode/data-model.md`）。

## 1. team 流帧词汇（ChatEvent 扩展）

| 帧 | 层级 | 载荷 | 变更 |
|---|---|---|---|
| `member_view`（新） | team 级（不设外层 `member`） | `{member, sender, message}` | **新增**：一个成员消费了一条输入（用户输入或他成员广播注入）写入其视角的实时通知。`member` = 消费方成员 role；`sender` = 来源（保留值 `"user"` 或广播发送成员 role）；`message` = 该成员视角消息投影（`HistoryMessage`，与 `ListMemberMessages` 元素同构：ROLE_USER + sender 标注）。扇出时机 = 服务端 `appendMemberViewUser` 写入时刻（编排驱动成员、消息注入其 log 的同步点）。幂等锚 = `message.messageId`（服务端分配，跨帧唯一）。 |
| `tool_result` | 成员级 | 不变 | 载荷零变更；**前端消费语义变更**（见 §4）。 |

其余帧（`turn_start`/`block_start`/`delta`/`block_end`/`turn_end`/`team_message`/`queued`）载荷与扇出语义不变。

## 2. Team 视图（GetTeam 输出扩展）

- **`active_member`（新增，output-only，string）**：当前激活成员——单一合并值（spec Clarifications 2026-09-11 裁定）：成员回合在途时 = 该回合驱动成员（任一时刻至多一个）；静止时 = 下一条输入归属成员（activation；物化后初始 `planner`，取消/静止不改变归属）。team 物化后恒非空；未物化时 GetTeam 为 NOT_FOUND（无空值歧义）。
- 来源：编排层 snapshot 的 `driving?.role ?? activation`（`OrchestratorSnapshot` 相应增加 `activation` 外露）。
- web 呈现态：工具条激活成员徽标；实时推导 = 最近 `turn_start` 帧的 member（live 非空期间）→ live 全部收束后回退最近 GetTeam 值（刷新时机沿用现状：进入会话/send 前/10s 轮询/live→空迁移）。

## 3. preset 数据形态（唯一事实源 + 派生视图）

### 3.1 持久实体（不变项强化）

`PresetRecord`（Mongo `game_agent_v2.presets`）：`{id, role, template, persona, displayName, 时间戳}`——字段集不变；**唯一事实源**：CRUD 只写 store。

### 3.2 派生视图（新实体：DerivedComposition，非持久）

```
DerivedComposition :=
  来源: PresetRecord × 模板组合文件（镜像内 agent.cordis.yml，roster 发现）
  内容: 模板组合行列表，其中 persona 行 config.text ← record.persona
        （record.persona 为空 → 保留模板行原文 = 角色默认 base，空值回退语义不变）
  物化形态: 使用时生成的临时组合文件（os.tmpdir() 下 mkdtemp 会话目录，
        仅 agent.cordis.yml；不维护、不清理承诺、删除无副作用、重建幂等）
  消费方: 合成 AgentPreset {id: record.id, trust: 'user', path} → 官方 mountPreset
  生命周期: 一次物化（UpdateTeam 成员创建）一挂载；fiber 随成员 agent 卸载
```

校验规则（不变）：Create 时 `templateRules` 行级校验（读模板文件）；物化时 preset 存在 + role 匹配 + model 在目录（服务端场景校验）。

### 3.3 生命周期（终态）

| 事件 | 终态行为 |
|---|---|
| Create | store 写（模板校验读模板文件） |
| Update(persona/displayName) | store 写 |
| Delete | store 删 |
| compose(id) | store.get → 派生临时文件 → mountPreset |
| Pod 重建后 | compose 恒派生（幂等，无副本概念） |

## 4. 前端 ChatState 消费规则（变更）

- **`tool_result` 帧**：settle 应用面从"live 草稿命中即止（early-return）+ history 回退"改为**一次归约内跨三投影面幂等应用**：live 草稿（`settleDraft`）、归并序列条目（`settleHistoryEntry`）、成员视角条目（`settleMemberHistory`）——各面按 `toolId` + `TOOL_STATUS_RUNNING` 匹配（已终态块不命中 = 天然幂等/多流重复帧去重）。根因与修复边界见 [research.md](research.md) R6。
- **`member_view` 帧（新）**：`memberHistory[member]` 追加 `{message, sender: frame.sender}`；messageId 已存在则忽略（幂等）；不触碰归并序列、live、queue。
- 激活成员推导态：`turn_start` → active = frame.member；live 收束 → 保留最近 GetTeam 值（不新增 store 字段，由呈现层从 live 派生 + GetTeam 快照组合）。

## 5. 平台常量（常量库首批实体）

`ReservedEnvName` 集（Go `common/gopkg/constants` + JS `@dominion/common-js-constants` 同源收录）：`SERVICE_APP`、`DOMINION_ENVIRONMENT`、`POD_NAMESPACE`、`TLS_CERT_FILE`、`TLS_KEY_FILE`、`TLS_CA_FILE`、`TLS_SERVER_NAME`、`S3_ACCESS_KEY`、`S3_SECRET_KEY`、`DOMINION_SECRET_DIR`、`DOMINION_CONFIG_DIR`、**`DOMINION_ARTIFACT_DIR`（本 feature 新增，值 = `/dominion/{app}/{service}` 产物目录）**。收录原则：跨领域、无既有权威来源的仓库级常量（common 既有公共包的自有领域常量不收录，Clarifications 2026-09-11 裁定）。

## 6. 提示词 section 模型（三层所有权）

| 层 | 所有者 | 注册 | order 频段 | 内容边界 |
|---|---|---|---|---|
| 玩法 + 可用操作 | `saolei-loop`（host 行 `saolei:game`） | apply(ctx) boot 时全局注册，全员可见 | 50（persona/team 之后、工具守则之前） | 经典扫雷规则（权威来源）+ 与 saolei 工具能力对齐的操作集合（交集）；不含工具调用形态/结果格式细节 |
| 工具守则 | `saolei`（preset 行 `saolei:guidance`，仅 player） | 行挂载（不变） | 100（不变） | 仅工具用法：符号表/坐标标尺/结果三层结构/校验拒绝语义/示例/纪律；无玩法规则陈述 |
| persona | preset 模板 `@deepseek-ai/dsh-persona` 行 | 行挂载（不变） | 0（不变） | 身份/职责/风格；无玩法与操作描述 |

team section（team 插件）仅承载团队事实 + **广播格式约定（单一 XML 标注形态，R8 终态格式）**。

## 7. 广播 wire 形态（终态）

```
<message 单元>
<{role}-message>
{发言正文原文（仅 text 块，无 think）}
</{role}-message>

<tool 单元>
<{role}-tool-call>
context: {局 id}              ← 可选行（无则整行省略）
tool: {工具名}
args: {完整参数原文}
result: {完整结果原文}
</{role}-tool-call>
```

不变量：1:1 原样（args/result 全文不截断不摘要）；仅发送者标注（标签名）；无 think/reasoning 内容；消费锚（`TeamBroadcastSource.messageId`）与派生重建算法不变。
