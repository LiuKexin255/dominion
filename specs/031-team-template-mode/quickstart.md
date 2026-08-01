# Quickstart: Team Template Mode 验证指南

**Feature**: `031-team-template-mode` | **Spec**: [`spec.md`](./spec.md) | **Plan**: [`plan.md`](./plan.md)

> 端到端可运行验证场景，证明特性可用。本文为验证/运行指南，**不含**实现代码（实现属 `tasks.md`）。引用契约与数据模型，不重复。宪法原则 VI：服务型应用须经 testplan skill 完整执行大型测试（部署→测试→清理）且全部用例通过。

---

## 0. 前置

- 仓库根：`/mnt/code/dominion`（bazel 入口）。
- 编译/单测：`bazel build //...` / `bazel test //...`（每次代码变更必做，宪法原则 IV）。
- 大型测试：testplan skill（`tools/test/guitar`，`guitar run <plan.yaml>`）；规范见 `style/large_test.md`。
- 测试用 fake-llm（`projects/game/fake-llm/`）驱动 LLM，避免外部依赖（既有惯例）。

---

## 1. 单测层（每次变更，不单列 task）

### 1.1 proto / 资源名解析（`gameconst`）

- 验证 `SessionName`/`SessionID`/`TeamName`/`MessageAgentName`/`TeamProfileName` 解析新层级（[`contracts/api-contract.md`](./contracts/api-contract.md) §5）。
- 验证 `Template` 枚举与路径段一致；旧解析（顶层 `sessions/*`、`prompts/*`）已移除。

### 1.2 prompt 服务（TeamProfile）

- TeamProfile CRUD（oneof `spec.saolei`）；`template` 与 oneof 变体一致性校验；update_mask（含 oneof 成员路径）。

### 1.3 agent（team graph + sink + strategy-store）

- `SaoleiEventSink`：未传 sink 行为不变（FR-020）；传记录型 sink 按时机回调，`onGameEnd.status` 为枚举（[`contracts/saolei-sink-contract.md`](./contracts/saolei-sink-contract.md)）。
- team sink → ephemeral buffer；player 节点后处理（createAgent 返回后）读 buffer → `gameEnded`；条件边路由；planner 触发一次；graph 清 `gameEnded`（[`contracts/team-graph-contract.md`](./contracts/team-graph-contract.md) §2/§4）。
- `StrategyStore`（agent 直连 mongo，初始 `""`）：`get`(无记录→`""`)、`put`→`get` 一致；重启后持久（[`contracts/strategy-store-contract.md`](./contracts/strategy-store-contract.md)）。
- `RefreshTeam`：对两通道发 `REMOVE_ALL_MESSAGES`，策略（mongo store）不变（FR-018）。
- player = createAgent（全 loop）；仅 player 持 saolei 工具；planner 仅 `update_strategy`（FR-010/FR-012）。

### 1.4 desktop frontend（多 tab + profile 特化）

- frame 按 `agent` 归位（D12）；agent 列表来自 `Team.agents`；`accepts_user_input=false` 的 tab 屏蔽输入（[`contracts/desktop-contract.md`](./contracts/desktop-contract.md)）。
- saolei profile 表单仅 player/planner 模型（typed oneof 驱动）。

---

## 2. 大型测试（验收；FR-030；宪法原则 VI）

> 经 testplan skill 完整执行：`guitar run projects/game/testplan/<plan>.yaml`（部署→测试→清理闭环）。**全部用例通过**方为验收；仅 `bazel build` 测试 target **不构成**验收。

### 2.1 新增/改造测试计划

参照既有 `projects/game/testplan/system_test.yaml` 与 `deploy_agent.yaml`（含 session+proxy+agent+prompt+gateway+fake-llm）。新增 saolei team 行为用例（deploy 拓扑复用）。建议用例（具体由 `tasks.md` 落地）：

| 用例 | 覆盖 | 期望 |
|---|---|---|
| **team-connect** | 新层级 Connect（`templates/saolei/sessions/.../connect`）、GetTeam 返回 `agents=[player, planner]` | FR-003/FR-004/D3 |
| **player-exclusive-control** | team turn 中仅 player 调 saolei 工具；planner 不发起桌面操作 | FR-010 |
| **planner-trigger-per-game** | 一局 won/lost 结束 → planner 恰好触发一次（同局不重复） | FR-011/D6 |
| **strategy-shared-persistent** | planner `update_strategy` 写策略 → 下一局 player 作为当前态势读取、planner 作为 system 读取；策略以 session id 隔离 | FR-013/FR-014/FR-015 |
| **refresh-team-clears-short-term** | `RefreshTeam` 后短期消息清空、策略仍可读 | FR-018/D8 |
| **message-partition-by-agent** | ListMessages 按 agent 分区（player/planner 各自流） | FR-005 |
| **team-profile-crud** | TeamProfile（saolei）CRUD；tools/mcp 不可配 | FR-006/FR-027 |
| **sink-decoupled** | （可在 agent 单测兼测）MCP 无 team mode 耦合引用 | FR-019 |

### 2.2 运行步骤（testplan skill）

1. 加载 testplan skill；阅读 `style/large_test.md`。
2. `guitar run projects/game/testplan/<saolei-team-plan>.yaml`（完整部署→测试→清理）。
3. 结果：**all cases passed**。失败/flaky 即验收未过，修复后重跑至全绿。

---

## 3. 端到端手测（desktop；可选补充）

> 非验收必需，但可在大型测试通过后做交互确认。

1. 启动 desktop；顶层选择 `saolei` 模板（本地枚举，无网络请求，FR-024）。
2. 创建会话 → 进入对话；对话区出现 `player`/`planner` 两个 tab（来自 `Team.agents`）。
3. 在 `player` tab 输入"开局" → player 经 saolei MCP 操作桌面；`player` tab 显示其消息。
4. 一局结束 → `planner` tab 出现复盘消息（planner 触发一次）；策略更新。
5. 切到 `planner` tab：输入被屏蔽（仅观察，FR-032）。
6. `RefreshTeam` → `player`/`planner` tab 短期消息清空，但 player 下一局仍按当前策略落子（策略保留）。
7. 进入 saolei profile 页：仅 player/planner 模型可选（FR-029）。

---

## 4. 验收通过标准（Definition of Done）

- `bazel build //...` 与 `bazel test //...`（相关 target）全绿（每代码变更）。
- 大型测试经 testplan skill 完整执行，**全部用例通过**（FR-030/SC-008/宪法原则 VI）。
- 所有契约（api/team-graph/sink/strategy-store/desktop）的"验证要点"逐条满足。
- 引用门禁：代码/文档含可追溯来源（宪法原则 I）。

## 5. 参考

- 契约：[`contracts/`](./contracts/)；数据模型：[`data-model.md`](./data-model.md)；决策：[`research.md`](./research.md)。
- 既有测试：`projects/game/testplan/`（`system_test.yaml`、`deploy_agent.yaml`、`agent_saolei_test.go` 等）。
- 规范：`style/large_test.md`、`style/golang.md`、`style/javascript.md`、`style/api.md`（及其引用的 AIP）。
