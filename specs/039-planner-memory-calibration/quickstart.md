# Quickstart: Planner 长期记忆与校准指令（端到端验证）

**Feature**: `039-planner-memory-calibration` | **Spec**: [`spec.md`](./spec.md) | **Plan**: [`plan.md`](./plan.md)

> 端到端验证指南（非实现）。聚焦证明特性可工作的可运行场景；实现细节在 `tasks.md`。契约/数据模型细节见 [`contracts/`](./contracts/) 与 [`data-model.md`](./data-model.md)。大型测试规范见 `style/large_test.md`，经 testplan skill（`tools/test/guitar`）执行。

---

## 前置条件

- 仓库可 `bazel build //...` / `bazel test //...`（Go + TS）。
- 大型测试环境：`game/mongo` infra + session/proxy/prompt/agent/gateway + **新增 memory 服务**（`projects/game/deploy.yaml` 加 `memory` 条目）。
- 一个 saolei TeamProfile（player/planner 模型 + base 提示词，031 FR-027/FR-034）已创建。
- LLM provider 可达（agent `llm-secrets`）。

---

## 场景 1：saolei_operate 批量落子（FR-001/FR-002，US1）

**验证**：player 工具面仅 `saolei_operate`；批量按序执行、单次返回；失败细分（无害空操作跳过、结构性/游戏结束停止）。

1. 部署完整拓扑（含 memory 服务），`UpdateTeam(allow_missing=true)` 物化 team（AIP-134/156，040 supersede 后无 CreateTeam）+ Connect。
2. player 调 `saolei_init` 开局，再调 `saolei_operate([{type:"click",x:0,y:0},{type:"flag",x:1,y:1},...])`。
3. **期望**：一次调用一次返回（结果行 + 游戏状态行 + 最终棋盘）；操作按序生效。
4. 构造无害空操作（如在已揭示数字格 click）：**期望**该 op 被跳过、批量继续、单次结果标注跳过。
5. 构造结构性拒绝（越界）：**期望**在该 op 停止、剩余不执行、结果反映停止原因。
6. 同步检查 planner 复盘输入（gameLog）：以 `saolei_operate`（含全部 operations）为单位记录；planner 工具描述含 `saolei_operate` + click/flag/chord。

> 契约：[`contracts/saolei-operate-contract.md`](./contracts/saolei-operate-contract.md)。

---

## 场景 2：planner 长期记忆 + memory 服务（FR-006..012，US2）

**验证**：memory 服务独立数据库持久化；planner 持 memory_add/update/remove（含 memory_id）；冻结快照注入（每条 `memory_id: 内容`）；mcp 经 agent 转发不直连。

1. memory 服务部署后，确认其连 `game_memory` 库（独立于 agent/prompt 库，`style/mongo.md`）。
2. team 初始化 → planner 经 `memory_add(memory_id="m1", content="...")` 写入；查 memory 服务 `memories` 集合确认持久化。
3. 重启 memory 服务进程 → `memory_update`/`memory_remove` 仍可定位 `m1`（持久）。
4. `memory_add(memory_id="m1", ...)` 重复 → 返回 ALREADY_EXISTS 错误文本（add 与 update 区分）。
5. 触发压缩（第 5 局结束）→ 确认 planner 系统提示词的冻结记忆快照刷新（含最新 memory）；快照每条以 `memory_id: 内容` 呈现。
6. 审查连接拓扑：memory mcp server → memory-client（agent）→ memory 服务；mcp 不直连 memory 服务（SC-006）。

> 契约：[`contracts/memory-service-contract.md`](./contracts/memory-service-contract.md)、[`contracts/memory-mcp-contract.md`](./contracts/memory-mcp-contract.md)。

---

## 场景 3：planner→player 校准指令 + 废弃 StrategyStore（FR-013..019，US3）

**验证**：player 不再读策略；两场景指令（review prompt"必要时才调用"同 turn / init-compact prompt 引导无历史、不激活 player）。

1. **代码审查**：`StrategyStore`/`update_strategy`/player"当前态势"注入全部移除（SC-005，无残留引用）。
2. **team 初始化**：`UpdateTeam(allow_missing=true)` 物化返回（initInstruction 异步触发，仅 graph 首建；profile 变更重建不重跑 init，040 FR-005）→ 确认 planner 经 prompt 引导产出初始指令（无游戏历史，LLM 决定是否调用）→ 进 `pendingInstruction` 槽；首次 user message → player 激活时指令随同注入 playerMessages（不产生仅因指令的独立 player 激活；异步期间 user message 排在指令之后）。
3. **正常游戏结束**：player tool_calling → tool_result（游戏结束）→ planner 复盘（对 player 不可见，在 plannerMessages）→ planner 按"必要时才调用"**可选** `instruct_player` → 指令 HumanMessage 进 playerMessages（顺序 `tool_result → 指令 → player output`）→ graph 路由回 player 继续；planner 不发指令时 player 亦继续。
4. **触发压缩（第 5 局）**：review → compress（冻结快照刷新）→ postCompactInstruction（prompt 引导无历史指令，LLM 决定）→ `pendingInstruction` → turn 结束（player 停下）→ 下次激活注入（与 037"压缩后自动停下"一致）。
5. 消息顺序断言（FR-017）：playerMessages 可见序列为 `tool_calling → tool_result → planner 指令 → player message output`。

> 契约：[`contracts/team-graph-contract.md`](./contracts/team-graph-contract.md)。

---

## 大型测试执行（FR-018，宪法原则 VI）

- 测试计划：`projects/game/testplan/system_test.yaml`（新增 `planner-memory` suite，复用既有部署拓扑 `deploy_agent.yaml`——已含 memory 服务条目；**不新建 YAML**，覆盖上述场景 1/2/3，tasks T036）。
- 执行（须经 testplan skill 完整部署→测试→清理闭环，禁止仅 `bazel build` 替代验收）：

  ```bash
  # 经 testplan skill：guitar run <plan.yaml>
  ```

- **验收标准**：所有测试用例全部通过（all cases passed；failed/flaky = 未通过，须修复重跑）。

---

## 代码层级验证（编译 + 单测，每次变更）

```bash
bazel build //...        # Go memory 服务 + TS agent + proto codegen
bazel test //...         # Go 单测（memory 仓储/handler）+ TS js_test（mcp/graph/snapshot）
```

- 重点关注：memory 服务仓储（独立 db、唯一索引、ALREADY_EXISTS/NOT_FOUND）、saolei_operate（失败细分）、冻结快照（refresh/toSystemMessage）、instruct_player（playerMessages 追加）、StrategyStore 移除后无残留引用。
