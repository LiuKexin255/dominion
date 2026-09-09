# Quickstart: Agent v2 Team 模式迁移 — 端到端验证指南

> 验证场景索引（Phase 1）。契约细节见 [contracts/](contracts/)，实体见 [data-model.md](data-model.md)；断言口径对齐 spec US1–US5 与 SC-001–SC-005。大型测试执行规范见 `style/large_test.md`（testplan skill）。

## 0. 前置条件

- 仓库构建入口：`bazel build //...` / `bazel test //...`（单测随每次变更执行，不在此重复）。
- 大型测试：`guitar run projects/game/testplan/deploy_agent_v2.yaml`（部署→测试→清理闭环；全量通过 = 验收）。
- 环境注入：fake-llm / fake-desktop 端点（testplan 部署拓扑内提供，无需真实 LLM/desktop）。
- team 双角色夹具（fake-llm）：player 夹具（识别 player persona、依序调用 saolei 工具至终局、接收策略广播后继续）、planner 夹具（产出开局策略/复盘正文、调用 memory 工具）。

## 1. 验证场景

### V1 — v1 移除干净（US1 / SC-001）

1. 代码检索零残留：`projects/game/agent` 目录、`@dominion/game-agent` 包名、`TeamService`/`PromptService` 定义、v1 专属夹具（`fake-llm/service/testdata/planner*.yaml`）均无命中；`projects/game/prompt` 目录不存在。
2. 保留面仍可用：`/api/v1` sessions CRUD、memory 服务及其 `/api/v1/.../memories` 路由、desktop connect（`/api/v2/.../connect`）行为不变。
3. `bazel build //...` 与 `bazel test //...` 全通过；大型测试既有回归用例全绿。

### V2 — preset 分池与角色锁定（US3 / SC-004）

1. 经 `POST /api/v2/templates/saolei/presets` 创建 player/planner preset 各一（role 必填）；`GET .../presets?role=PLAYER` 过滤正确；`PATCH` 仅可改 persona，改 role 被拒。
2. 物化 team 后（见 V3），`GET /api/v2/.../team/members/player` 的 system_prompt 含扫雷工具守则、不含 memory 痕迹；`.../members/planner` 相反且含记忆快照（SC-004/SC-005 断言面）。
3. 服务重启后 preset 仍在（Mongo 持久化）。

### V3 — team 物化与自动开局（US2 场景 1/2）

1. 未物化 session 直接 `POST ...:send` → `FAILED_PRECONDITION`，web 呈现引导态。
2. `PATCH /api/v2/{team}` 提交（player/planner preset + 可选 model）→ 物化成功返回 Team（members/desktop_connected）。
3. **无任何用户 Send**：数秒内自动出现 planner 开局策略消息（团队视图实时），随后 player 被驱动开始游戏（saolei 工具调用流 + fake-desktop 收到操作）。

### V4 — 完整局至终局 + 复盘 + memory + 续驱（US2 场景 3/4/5，SC-002）

1. player 持续工具调用至终局（won/lost，fake-desktop/board 夹具驱动）。
2. 终局后自动出现 planner 复盘消息；复盘回合含 memory 工具调用 → 经 `/api/v1/.../memories` 断言条目已持久化（立即持久化）。
3. planner 回合结束后**无用户触发**，player 被续驱进入下一轮（结构性续驱；夹具可控制"继续开局"与"不开局"两种行为各验证一次）。
4. 全程团队视图可见双成员事件流（member 标注正确）。

### V5 — 双视图与 system prompt（US4/US5，SC-003/SC-005）

1. `GET /api/v2/.../team/messages`：归并序列含 USER/PLAYER/PLANNER 标注，成员消息为原生输出（含工具调用块），无广播包装形态。
2. `GET .../team/members/player/messages`：`user→user`、player 输出→`agent`、planner 消息→`role=USER, sender=PLANNER`（渲染 `user: [planner]...`）；planner 视角对称。
3. 同一消息跨视图正文一致；web 视图切换器恰 3 个视图（1 团队 + 2 成员）。
4. `GET .../team/members/{x}` 返回完整 system_prompt，双成员内容分化（V2-2 断言口径）。

### V6 — 用户消息：排队、消化优先于切换、取消恢复（US2 场景 6/9，FR-011/FR-017）

1. player 回合中 Send → `queued` 帧；回合结束后由 player 消化。
2. planner 复盘回合结束时存在排队消息 → planner 先消化排队消息、随后才切换续驱 player（夹具构造该时序）。
3. `POST .../team:cancel` → 在途回合 `turn_end{CANCELED}`、续驱暂停（无新回合出现）；再次 Send → 循环恢复；重复 cancel 幂等成功。

### V7 — 刷新与边界（Edge Cases）

1. 物化后再次 `PATCH`（改 preset/model）→ 在途回合终止、历史清空、新生命周期（双视图与新状态一致）、create_time 保留。
2. 物化时 memory 服务不可达（testplan 注入故障）→ UpdateTeam 失败、无半物化（GetTeam NOT_FOUND）、可重试成功。
3. desktop 断连（fake-desktop 停止）→ 工具错误结果可见、进程存活、重连后可继续。

## 2. 执行命令

```bash
# 单测（开发过程随变更执行）
bazel test //projects/game/agent_v2/... //common/js/dsh-plugins/... //projects/game/fake-llm/...

# 大型测试（验收：部署→用例→清理，全部用例通过）
guitar run projects/game/testplan/deploy_agent_v2.yaml
```

大型测试用例与上述 V1–V7 的承载映射在 `/speckit.tasks` 阶段细化到 testplan yaml；边界审计（v1 移除验收）可复用 `specs/058-dsh-preset-roster-demo/checklists/boundaries.md` 的 grep 命令面模板。

## 3. 预期结果（验收口径）

- V1–V7 全部断言通过；大型测试**所有用例全部通过**（failed/flaky 均为不通过，constitution 原则 VI）。
- 交付后 spec SC-001–SC-005 全部可判定为达成。
