# Contract: /api/v2 协议变更（agent-api-changes）

**Feature**: [spec.md](spec.md) FR-004/014/015/016/017/018 | **Research**: [research.md](../research.md) D3/D7/D8/D9 | **Data**: [data-model.md](../data-model.md) §1–§4

051 契约 [contracts/agent-api.md](../../051-agent-v2-dsh-migration/contracts/agent-api.md)（AgentService/preset/agent/messages/send/models）为基线，本文只定义**变更面**；未提及方法与语义（preset CRUD、Update 物化、List 标准 List、Send NDJSON 形态、排队、模型目录同源校验）零变化。

## 1. 块事件 step 扩展（FR-004）

| 事件 | 变更 | 语义 |
|---|---|---|
| `blockStart` / `delta` / `blockEnd` | +`int32 step` | turn 内模型输出步骤序号（1 起单调——服务端 step 循环序号透传；0 仅作字段缺失哨兵）；同 step 块共享；step 变化=新分段 |
| `toolResult` | 不变 | 按 tool_id 关联（跨 step 全局匹配） |
| `turnStart` / `turnEnd` / `queued` | 不变 | — |

- NDJSON 流形态与事件序不变（051 agent-api.md §4 延续）；`index`（turn-global 块序）保留，与 `step`（分组）正交。
- 服务端实现：TurnCollector 已跟踪 `ActiveTurn.step`（chunk 事件 `data.step`），映射层透传。
- 兼容：旧客户端忽略未知字段；事件缺 step → 消费端归组 step=0（退化行为）。
- step 起始值裁定见 [revisions/phase11-step-numbering.md](../revisions/phase11-step-numbering.md)。

## 2. TurnStatus 扩展与终态语义（FR-015/016）

`TURN_STATUS_CANCELED`（新枚举值）：用户经 `:cancel` 终止。终态三元组语义：

| 终态 | 内容保留 | 触发方 |
|---|---|---|
| COMPLETED | 全部分段（终态折叠为前端行为） | loop 正常 |
| ERROR | 已产出分段（含 interrupted 尾步） | loop 失败 |
| CANCELED | 已产出分段（含 interrupted 尾步） | 用户 :cancel |
| ABORTED | —（流终止，前端清空） | 会话销毁/Update 重物化（051 既有） |

**与官方差异声明**（research D2）：官方 `IConversation.cancel()` 保留 pending Queue；本契约按用户裁定为**排队消息落地为历史 user message**（enqueue 时已 `appendUser`，cancel 清空待处理队列不触发回合）。消费端不得假设官方 Queue 语义。

## 3. Cancel 自定义方法（FR-015/016/017）

```text
POST /api/v2/{name=templates/*/sessions/*/agent}:cancel
→ 200 {}（空对象）
```

| 行为 | 契约 |
|---|---|
| 前置 | agent 已物化（未物化 → 与 Send 同语义的明确错误） |
| 在途回合 | 终止：模型流与在途工具传播取消；在途桌面操作经既有 abort 语义结算（FAILED "aborted"，不悬挂）；流上发 `turn_end{CANCELED}` |
| 排队消息 | 落地：待处理队列清空、不触发回合；历史 user 消息保留（enqueue 已固化）；每个排队流收到 `turn_end{CANCELED}` 终帧并关闭——cancel 原子清空队列无可重报 position，排队流以既有 turn_end 词汇确定性收束（无新事件类型） |
| 幂等 | 无在途回合且无队列 → 成功 no-op |
| 后置 | session 立即可接受新 Send（无冷却/锁定） |
| 并发 | 与 Update 重物化并发 → 后到者胜出前的在途回合按既有终态语义收束，不产生半清理状态 |

错误码：路径非法/未物化 → 400/FAILED_PRECONDITION（与 Send 前置错误同族，051 FR-007 语义）。
- 路由：会话面两跳（gateway→proxy→agent_v2，051 §4 拓扑）——gateway 的 grpc-gateway 注册随 codegen 自动携带 `:cancel` 路由（零代码改动）；proxy `AgentHandler` 以 owner 亲和显式转发（GetAgent 同构，无本地语义），设计见 [revisions/phase2-proxy-cancel.md](../revisions/phase2-proxy-cancel.md)。

## 4. GetAgent 连接状态（FR-002）

`Agent` 响应新增 `bool desktop_connected`：

| 场景 | 值 |
|---|---|
| 该 session 桥接连接注册表有活跃连接 | true |
| 无连接（含已断开/未连接） | false |
| agent 未物化 | GetAgent 404（前端降级"未知"，不虚构 false 之外的语义） |

- 事实来源：`@dominion/dsh-desktop-bridge` 新查询面 `isDesktopConnected(sessionName)`（连接注册表直读；接管窗口内最终一致）。
- 刷新策略为消费端职责（前端轮询 10s + 关键时刻），服务端不推送。
- gateway/proxy 预期零改动（proto 字段扩展经既有透传自动生效；codegen 后集成验证——research 开放项）。

## 5. 模型目录（FR-018）

- `ListModels` 响应：`glm-5.3`、`glm-5.3-flash`（来源 `cordis.yml` llm-glm `models`，移除 `glm-5.2` 别名条目）。
- 默认模型：`glm-5.3`（`DEFAULT_MODEL = GLM_MODEL || 'glm-5.3'`；env 覆盖机制保留）。
- 物化校验/下拉同源机制零改动（051 FR-006）；`glm-5.3-flash` 的 contextWindow 配置以官方调用文档核实为准（research D9 开放项）。

## 6. 服务端历史固化（FR-012，实现面契约）

- `@dominion/dsh-saolei-loop` driver：LLM 流失败/finish error 抛错前，assembler 有部分内容则 append `assistant/message`（`interrupted: true`）——与既有 abort 路径同构；finish error 经 `agent/request-error` waterfall 后 abort 的窗口同样固化（retry 已不可执行，[revisions/phase4-failed-turn-folding.md](../revisions/phase4-failed-turn-folding.md) §7-2）。
- `SessionHistory.appendAssistant` 记录事件 data 的 `interrupted`；List 响应以 `HistoryMessage.interrupted` 透出（proto 字段扩展，[data-model.md](../data-model.md) §1.5）：消费端据此判定失败/终止回合无最终答案（FR-005 不折叠）。字段扩展经 gateway/proxy 既有透传自动生效（§4 同类）。
- 验收锚点：注入 LLM 流失败的回合，其已流式内容经 List 回填可见且尾步 `interrupted=true`（tool-call 块的中断终态由消费端按消息终态推导）。

## 7. 义务与验收锚点

1. 协议扩展向后兼容（旧客户端未知字段忽略、缺 step 退化）经双端单测断言。
2. `:cancel` 全语义（终止传播/落地/幂等/立即可用/并发）经 agent_v2 vitest 断言；e2e 经 testplan case 覆盖（见 [testplan.md](testplan.md) §4）。
3. `desktop_connected` 与 bridge 注册表事实一致（含接管/断开路径）经单测断言。
4. 模型目录/默认值/校验同源经既有 preset/models 用例更新断言。
