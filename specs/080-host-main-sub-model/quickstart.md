# Quickstart: agent_v2 host 层场景化重构支持主子模型 — 验证指南

> Feature: [spec.md](spec.md) | Plan: [plan.md](plan.md) | 契约：[contracts/scene-registry.md](contracts/scene-registry.md)、[contracts/face-main-sub.md](contracts/face-main-sub.md)、[contracts/sts2-loop-minimal.md](contracts/sts2-loop-minimal.md)、[contracts/testplan-game-sts2.md](contracts/testplan-game-sts2.md)
> 本文档是验证/运行指南（实现细节归 tasks 与实现阶段）。Success Criteria 对应：SC-001（V4）、SC-002（V2/V3）、SC-003（V4）、SC-004（契约文档本身）。

## 前置

- 仓库构建/测试入口 bazel；JS 工具经 `bazel run //:go`/`bazel run @pnpm` 类通道（`AGENTS.md`）。
- 大型测试经 testplan skill（`tools/test/guitar`）执行；执行前阅读 `style/large_test.md`。
- 本地无需真实 LLM/desktop：全部验证走 fake-llm（零 secret 测试 artifact `agent-v2-test`）。

## V1: 构建与单测（每次代码变更，宪法 IV）

```bash
# 全仓构建 + 相关单测（agent_v2、新包、fake-llm）
bazel build //...
bazel test //projects/game/agent_v2/... //common/js/dsh-plugins/sts2/... \
  //projects/game/fake-llm/...
# testplan Go 用例编译：go_largetest 带 manual tag，通配不会展开，须显式构建
bazel build //projects/game/testplan:sts2_conversation_test
```

**通过标准**：构建零错误；单测全绿（含 `dsh.test.ts` 组合清单断言——`sts2-loop` 行与 main 池模板快照逐行断言，[sts2-loop-minimal.md](contracts/sts2-loop-minimal.md) §1）。

## V2: 关键单测验证点（速查）

| 验证点 | 位置 | 断言要点 |
|---|---|---|
| saolei 零变化 | `projects/game/agent_v2/src/session.test.ts`/`server.test.ts`/`history.test.ts` | 既有断言全量保留通过（错误文案逐字节、校验时序、名册/投影行为） |
| seam 中立与成员投影 | `session.test.ts`（泛化后） | 成员运行时由 `members()` 投影派生；成员名/roster 文案由 `faceMembers` 派生；无 announcer 场景物化无不变式违例 |
| 名册外角色动态投影 | `session.test.ts`（合成信封用例）/`history.test.ts`（viewless 模式） | 未知 role 信封 → 成员标注帧 + 归并序列条目；`GetTeamMember`/`ListMemberMessages` 对其 NOT_FOUND；刷新后不跨代；viewless 不写成员视角（[scene-registry.md](contracts/scene-registry.md) §3） |
| sts2 单成员物化 | `session.test.ts`（场景表 sts2 开放后） | roster `["main"]` 校验、079 fail-loud 收缩（UpdateTeam/preset 面开放）、刷新语义（[face-main-sub.md](contracts/face-main-sub.md) §1） |
| 最小编排 | `common/js/dsh-plugins/sts2/loop/src/*.test.ts` | 物化/回滚、静止-在途-收束、steer 排队重放、cancel 冻结恢复、六码映射、事件信封形状（[sts2-loop-minimal.md](contracts/sts2-loop-minimal.md) §7） |
| fake-llm lockstep | `projects/game/fake-llm/service/message_store_test.go` | `sts2_main.yaml` 嵌入断言同步（[testplan-game-sts2.md](contracts/testplan-game-sts2.md) §4） |

## V3: 场景中立结构检查（SC-002，固定命令集）

对 host 通用路径执行（079 SC-005 检查面的扩展）。**判定口径**：硬门禁零命中；软门禁只允许注释/规范链接命中（逐条核对，出现在 import/类型引用/表达式/字面量即失败）。

```bash
# 硬门禁（零容忍，含注释）：
rg -n 'memoryScope|teamGoal|TEAM_GOAL|TeamOrchestrator|@dominion/dsh-saolei-loop|orchestratedRoles|members\.length\s*[!=]==\s*2|恰 ?双成员' \
  projects/game/agent_v2/src/session.ts projects/game/agent_v2/src/history.ts \
  projects/game/agent_v2/src/server.ts projects/game/agent_v2/src/orchestration.ts
# 软门禁（注释/规范链接白名单；代码面零命中）：
rg -n '(^|[^A-Za-z0-9_])(announcer|player|planner)([^A-Za-z0-9_]|$)' \
  projects/game/agent_v2/src/session.ts projects/game/agent_v2/src/history.ts \
  projects/game/agent_v2/src/server.ts projects/game/agent_v2/src/orchestration.ts
```

**通过标准**：硬门禁零命中（含"恰双成员 roster 断言"残留面；覆盖 `session.ts`/`history.ts`/`server.ts`/`orchestration.ts`）；软门禁的每个命中行均为注释或 `specs/` 规范链接（PR 记录核对行号），无任何可执行代码命中。场景词汇只允许存在于 `scenes.ts`/`scene-saolei.ts`/`scene-sts2.ts`；`orchestration.ts` 作为通用 seam 模块必须零场景词汇（[scene-registry.md](contracts/scene-registry.md) §4 禁入清单）。

> `specs/079-sts2-template-scaffold/quickstart.md` V7 曾允许 `session.ts` 命中 `@dominion/dsh-saolei-loop` 导入路径；本 feature 后该白名单失效，以本节硬门禁（零命中）为准。

## V4: 大型测试验收（宪法 VI；SC-001/SC-003）

```bash
# 经 testplan skill 执行（完整部署→测试→清理闭环）：
guitar run projects/game/testplan/system_test.yaml --timeout=20m
```

**通过标准**：

1. 既有四 suite（game-system / game-disconnect / game-memory-down / game-stall）全部用例通过——saolei 零回归（SC-001）。
2. 新 `game-sts2` suite 四用例（物化名册 / 对话回合 / 静止-刷新-取消 / 非名册 NOT_FOUND，[testplan-game-sts2.md](contracts/testplan-game-sts2.md) §3）全部通过——主子形态端到端（SC-003）。
3. 任一 failed/flaky = 验收未通过，修复后重跑至全绿。
4. 该预算覆盖五 suite；若整体超时，按 suite 单独执行（每 suite 独立 run id + 部署→测试→清理闭环），不得跳过任何 suite。

## V5: 手动冒烟（可选）

部署 `deploy_sts2.yaml` 拓扑后，经 gateway 手动验证 face 语义（对照 [face-main-sub.md](contracts/face-main-sub.md)）：

1. `POST /api/v1/templates/sts2/sessions?session_id=...`（session 创建）；
2. `POST /api/v2/templates/sts2/presets?preset_id=...&role=main`（**persona 留空**——空 persona 保留 main 池模板 prefix base 与锚串"sts2 主 agent"；非空 persona 会覆盖 prefix 使 fake-llm `system_keywords` 失配）；
3. `PATCH /api/v2/{session}/team?allow_missing=true`，body `{"members":[{"role":"main","preset":"...","model":"glm-responses/glm-5.3"}]}`；
4. `POST /api/v2/{session}:send`（NDJSON），发送 `sts2-ping`——观察恰一个 `main` turn 与 fixture 文本回包、流在静止点结束；
5. `GET /api/v2/{session}/team`——名册恰 `main`；`GET .../team/members/announcer` → 404。

## 回归提示

- 组合清单/`templates` 快照变化必须同步 `dsh.test.ts` 断言与 `specs/070-dsh-upgrade/contracts/composition-manifest.md`（Edge Cases）。
- fake-llm testdata 为全进程共享：`sts2_main.yaml` 触发词与 saolei 触发词的子串冲突会在 saolei suite 复现（`projects/game/testplan/README.md` 事故记录）；新增触发词跑一遍 V4 全 suite 确认。
- `agent_v2_helpers_test.go` 参数化后，saolei 既有调用点必须显式传 `saoleiTemplateID`（编译期强制），禁止保留隐式默认值路径。
