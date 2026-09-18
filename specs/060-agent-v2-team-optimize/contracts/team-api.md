# Contract: Team API 增量修订（060）

> 本文件是 `specs/059-agent-v2-team-mode/contracts/team-api.md` 的**增量修订**：仅列出变更条款，未提及的条款（RPC 面、UpdateTeam 校验、流生命周期、Cancel、历史读取、错误语义、不变的面）按 059 原契约执行。
> 决策依据：[research.md](../research.md) R5/R6/R7/R8。

## 1. GetTeam 输出扩展（FR-008）

`Team` 消息新增 output-only 字段 **`active_member`**（string）：

- 值 = 当前激活成员（单一合并值，spec Clarifications 2026-09-11 裁定）：成员回合在途时为该回合的驱动成员；静止时为下一条输入的归属成员（activation）。team 物化后恒非空（物化成功即 `planner`）。
- REST 投影：`GET /api/v2/{name=templates/*/sessions/*/team}` 响应新增 `activeMember`（protojson camelCase）。
- 编排失败/取消暂停不置空：取消后静止时值为当前 activation（下一条 Send 的处理者）。

## 2. ChatEvent 帧词汇扩展（FR-006）

team 流新增 team 级帧 **`member_view`**（与 `team_message`/`queued` 同级，不设外层 `member` 字段）：

```
MemberViewEvent {
  member  string   // 消费该输入的成员 role（写入其视角的成员）
  sender  string   // 来源标注：保留值 "user" = 用户输入；成员 role = 广播注入的发送方
  message HistoryMessage  // 该成员视角的消息投影（ROLE_USER），与 ListMemberMessages 元素同构
}
```

- **扇出时机**：服务端将一条输入写入某成员视角的同一时刻（编排驱动该成员、消息注入其 log 的同步点）——即该成员**消费**该输入的实时事实，早于/伴随其回合的 `turn_start` 后续事件。
- **幂等锚**：`message.messageId`（服务端分配）；客户端重复帧按锚忽略。
- **语义边界**：仅承载消费事实（用户输入 + 广播注入两类，即 059 §5 ListMemberMessages 的 ROLE_USER 条目）；成员自身输出仍经 `team_message` 帧双写（不变）；前端不得据此向**未消费**成员的视角写入（消费前不出现语义保持）。
- **List 面对齐**：`ListMemberMessages` 返回序列与 `member_view` 帧序列同源同值（同一视角投影），回填与实时一致。

## 3. 工具结果实时性（FR-007，前端消费契约）

- 服务端帧序列不变（`block_start`〔tool-call 无 toolId〕→ `tool-call-delta` → `block_end`〔toolId/name/args 首次完整浮现〕→ `team_message`〔step 固化〕→ `tool_result`〔toolId + 终态 + result〕；时序依据官方 loop：assistant/message 先于 tool/result，[research.md](../research.md) R6）。
- **fixation 状态时序**：`team_message` 帧与 `ListTeamMessages` 共享同一 entry 对象（`projects/game/agent_v2/src/history.ts` 的 `appendMerge`/`settleToolResult`），其 toolCall 块状态取决于帧序列化时刻——通常为 `RUNNING`；同步失败工具（插件进程内拒绝，无桌面/网络往返）的 `tool/result` settle 先于该帧序列化时，帧携带已 settle 的终态，且该终态 MUST 与该工具的 `tool_result` 帧一致。帧序不变量（`team_message` 先于 `tool_result`）不变。
- **客户端义务**：`tool_result` 帧必须在一次归约内对**全部三个投影面**幂等终态化——live 草稿、归并序列条目、成员视角条目（按 toolId 匹配 RUNNING 块）——不得在任一面命中后跳过其余面。
- 既有收敛语义（断开投影、List 回填、并发流按锚去重）不变。

## 4. 广播 wire 格式修订（FR-010/FR-011，修订 059 FR-008 的"头行摘要"条款）

成员间 1:1 广播的注入文本采用**单一 XML 标注形态**（头行取消；059 spec FR-008"头行可含简短摘要标签"条款废止，2026-09-11 用户裁定"二者择一"）：

```
发言单元:
<{role}-message>
{发言正文原文——仅 text 块，不含 reasoning/think}
</{role}-message>

工具单元:
<{role}-tool-call>
context: {局 id}          // 可选行：广播方提供的上下文键（saolei 局 id），无则整行省略
tool: {工具名}
args: {完整参数原文}
result: {完整结果原文——仅 text 块}
</{role}-tool-call>
```

- **think 移除**：正文与结果只承载 text 块内容；reasoning/think 块不出现在广播注入文本中（成员自身的团队视图/成员视角**原生输出**呈现不受影响——think 展示是原生输出语义）。
- **不变量**：1:1 原样（args/result 全文、不截断、不摘要）；仅发送者标注（标签名派生自 role，`tagName` 规范化规则不变）；消费锚（`TeamBroadcastSource.messageId`）与派生重建算法不变。
- **team section 同步**：广播格式约定按上述终态表述（`common/js/dsh-plugins/team/src/section.ts`）。
- **消费方联动**：fake-llm 夹具锚点（`<player-message>`/`<player-tool-call>` 标签保持不变）、testplan 广播断言随格式同批生效；无迁移（team 为进程内存态，重启即新格式）。
- **成员视角 relay 呈现**：`user: [sender]` 前缀保留，正文 = 注入原文（含 `<{role}-message>`/`<{role}-tool-call>` 标签对，不剥离）；头行不出现、正文仅呈现一次。

## 5. web 呈现契约（FR-008/FR-009）

- **激活成员**：对话页工具条呈现当前激活成员（徽标/文案）；实时性 = 成员 `turn_start` 帧即时覆盖 + live 收束后以 GetTeam 值兜底（刷新时机沿用现状：进入会话/发送前/10s 轮询/回合静止）。
- **system prompt 入口**：对话主界面（team 工具条成员清单区）为每成员提供查看入口，读取 `GetTeamMember` output-only `system_prompt` 全文（只读浮层，行为同既有设置面板内入口；设置面板内入口保留）。
