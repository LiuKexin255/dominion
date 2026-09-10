# Contract: Web UI（team 模型 / 双视图 / system prompt 查看）

> 前端契约（`projects/game/web/frontend/src/`）。数据源 RPC 见 [team-api.md](team-api.md) 与 [preset-api.md](preset-api.md)；FR 对应 FR-013~FR-017。
> 现状组件（App.tsx / ChatView.tsx / ChatStore / AgentSettingsPanel.tsx 等）的既有机制（NDJSON 流消费、useSyncExternalStore、常驻挂载、回填、排队 chip、取消、错误横幅）按 member 维度扩展，本契约定义行为规范而非组件实现。

## 1. team 配置面板（替代 AgentSettingsPanel）

- **未物化引导态**：无 team 时对话页引导完成物化（对齐现状单 agent 引导模式）。
- **物化表单**：player preset 下拉（role=`"player"` 过滤）+ planner preset 下拉（role=`"planner"` 过滤），均必选；player/planner model 下拉各一（可选、留空=部署默认；选项来自 `/api/v2/models`，与校验同源）。Apply = `UpdateTeam`，提交为 `team.members` 两条成员配置（`{role: "player", preset, model?}` 与 `{role: "planner", preset, model?}`）。
- **刷新语义提示**：再次 Apply = 刷新（清空短期记忆、终止在途回合），显式提示（对齐现状）。
- **状态呈现**：物化状态、desktop 连接状态（GetTeam 定期刷新，节奏沿用现状 10s + 关键时机即时刷新）、成员清单（role + preset + model）。

## 2. 视图模型（每个 team 会话）

| 视图 | 数量 | 数据源（回填） | 数据源（实时） |
|---|---|---|---|
| 团队视图 | 1 | `ListTeamMessages` | team 流（Send 建立，[team-api.md](team-api.md) §3）：成员事件帧按 `member` 归并增量渲染 + `team_message` 帧按 `seq` 锚定归并序 |
| 成员视角视图 | 2（player/planner） | `ListMemberMessages` | team 流按 `member` 过滤的成员事件帧（其他成员产出在其被驱动消费前不出现——经回填呈现） |

- **视图切换**：对话页顶部切换器（团队 | player | planner）；切换为纯前端状态，不重新回填（各视图历史常驻）。
- **成员标识**：member/sender 为 wire 字符串值——保留值 `"user"`（用户消息）与成员 role（场景词汇，saolei 下 `"player"`/`"planner"`）；前端直接按字符串值归并、过滤与渲染（无枚举名前缀归一化）。
- **实时事件来源（team 流）**：编排自动驱动（非用户 Send 触发）与用户触发的成员回合事件经同一 **team 流**到达前端——Send 建立的 NDJSON 流持续至 team 静止，覆盖全部成员回合（US2 场景 2/4 与 US4 场景 4 的断言依据）。成员事件帧驱动实时增量渲染；`team_message` 帧提供与 `ListTeamMessages` 同源的 `seq` 锚，实时归并序与回填一致（SC-003）；多流并发与异常断开恢复的消费规则见 [team-api.md](team-api.md) §3.3/§3.4（断开经 List 回填补齐，下次 Send 重建流）。

## 3. 团队视图渲染规范（FR-014）

- 全部消息按 `seq` 时间归并：用户消息（member=`"user"`，现有 user 气泡形态）、各成员原生输出（member=成员 role 字符串）。
- 成员消息取该成员原始输出形态（正文 Markdown / 思考折叠行 / 工具调用卡），**归属到所属成员名下**（成员标签/头像位区分 player 与 planner）；MUST NOT 显示广播包装形态（`[sender] <sender-message>` 标签对是其他成员视角的注入格式，不出现在团队视图）。
- 既有 CompletedTurn 折叠逻辑按成员维度保持（各成员的中间步骤折叠、最终答案呈现）。

## 4. 成员视角视图渲染规范（FR-015）

- 该成员视角消息序列：`role=USER` 且 `sender="user"` → 用户消息气泡；`role=AGENT` → 该成员自己的输出（agent 形态，含工具调用卡）；`role=USER` 且 `sender` 为成员 role → 标注来源的用户消息（渲染为 `user: [sender] 正文`——sender 为另一成员 role 字符串原值时的广播注入原文，saolei 下如 `user: [player] …`）。`message.role`（USER/AGENT）沿用 HistoryMessage 自身枚举不变，`sender` 为 string。
- 自己=agent 的渲染与团队视图中的该成员原生输出**正文一致**（SC-003）。

## 5. system prompt 查看（FR-016）

- 成员清单（配置面板或视图切换器）提供每成员"查看 system prompt"入口 → 展示 `GetTeamMember` 返回的 `system_prompt` 全文（只读、等宽/原文呈现）。
- 内容要求与实例实际生效一致（服务端从装配面取，非另行拼装）；刷新 team 后随新配置更新。

## 6. 交互保持项（FR-017）

- 发送框、排队 chip（team 级）、取消按钮（target=team）、错误横幅、"已终止"标识、贴底跟随/回到底部、多会话常驻挂载与 active 门控——行为规范与现状一致（对象扩展为 team 及其成员）。
- preset 管理界面（PresetsView）：列表增加 role 标识与过滤；新建表单增加 role 必选（单选）；编辑仍仅 persona。
