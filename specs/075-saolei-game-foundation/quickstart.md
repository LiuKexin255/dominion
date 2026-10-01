# Quickstart: saolei 游戏基础与智能体工具解耦 验证指南

**Feature**: [spec.md](spec.md) | 日期: 2026-10-01

本文是端到端验证指南：证明特性按 [spec.md](spec.md) Success Criteria 生效。实现细节见 [tasks.md](tasks.md)（Phase 2 产出）；契约引用见 [contracts/](contracts/)。迁移分步（P1–P4）门禁见 [research.md](research.md) D5。

## 前置条件

- Bazel 构建环境可用（`bazel` / `bazel run @pnpm` / `bazel run //:gazelle` 等命令约定见 `AGENTS.md`）。
- 大型测试经 testplan skill 执行（skill `testplan`；规范 `style/large_test.md`）。
- 迁移分步完成后做全量验收；P1/P2/P3 各步的中间门禁见 research.md D5 表。

## 1. 构建与单测（宪章原则 IV：每次变更执行）

```bash
bazel build //...
bazel test //common/js/dsh-plugins/saolei/... \
           //common/js/dsh-plugins/saolei-tools/... \
           //common/js/dsh-plugins/saolei-loop/... \
           //projects/game/agent_v2/...
```

**预期结果**：

- 全部通过（bazel 退出码 0）。
- 单测覆盖（SC-004/SC-006 与 FR-017）：
  - **dsh-saolei**：game 迁移等价用例全绿（原 saolei-loop `runtime.test`/`text.test` 行为断言零改动随迁；构造形态适配 `new GameRuntime(deps)`）；基础面测试（`ctx.provide("saolei")` 面存在、`createGameRuntime` 工厂纯度（仅收 deps、无 scope 副作用）、`gameSection` 名/order 50/text 逐字 = `SAOLEI_GAME_RULES`、无工具导出、无全局注册副作用）。
  - **dsh-saolei-tools**：saoleiTools 单元测试（三工具 schema 逐字、`saolei:guidance` 逐字、**单元 sections 仅 guidance（无 saolei:game）**、双形式拒绝文本逐字、缺运行时 fail-loud 文案、concludesTurn/isError 语义、无全局注册）。
  - **saolei-loop**：编排测试（harness stub 基础面含 fake `createGameRuntime`；materialize 构造并持有实例断言；player 分支 = saoleiTools 单元 + gameSection + 句柄注册（isolate 注册实例 === 编排持有实例）、注册序；**planner 分支 = gameSection + memory 原序、无工具无 guidance 无句柄**；materialize 失败/dispose 的实例引用丢弃）；apply 双能力面 fail-loud（既有）与 inject 语义；`MountPlayerRuntime` seam 用例清零（含 session.test）。
  - **agent_v2**：`dsh.test.ts` manifest 22 行断言 + saolei-loop config.plugins 内容断言。

## 2. 静态终态验证（SC-003 / SC-004 / SC-005）

```bash
# 依赖方向归位：dsh-saolei 内对上层的引用清零
grep -rn "dsh-saolei-loop\|dsh-saolei-tools\|dsh-saolei-memory" \
  common/js/dsh-plugins/saolei/src/ --include="*.ts" | grep -v ".test.ts"
# 旧导出路径引用清零：游戏类型/工厂不再从 saolei-loop 消费
# （agent_v2 对 saolei-loop 的非游戏类型 import——TeamOrchestrator 等——是正常依赖，故按游戏符号过滤）
grep -rnE "GameEventRecord|OperateInput|ToolOutcome|SaoleiGame|GameRuntimeDeps|gameStatsText|createAgentGameRuntime" \
  common/js/dsh-plugins/saolei-tools/ projects/game/agent_v2/src/ --include="*.ts" | grep "dsh-saolei-loop"
grep -rn "gameStatsText\|createAgentGameRuntime" \
  common/js/dsh-plugins/saolei-loop/src/ --include="*.ts" | grep "import"
# 目录终态
ls common/js/dsh-plugins/saolei-loop/src/ | grep game   # 应无输出
ls common/js/dsh-plugins/saolei-tools/src/              # 应存在
ls common/js/dsh-plugins/saolei/src/game/               # 应存在
```

**预期结果**：检索零命中（游戏类型消费经 `@dominion/dsh-saolei`）；目录形态符合终态。

组合清单终态核对（对照 [contracts/cutover-contracts.md](contracts/cutover-contracts.md) §2）：含顶级 `saolei` 行（无 config）；saolei-loop `config.plugins` = `[@dominion/dsh-saolei-tools, @dominion/dsh-saolei-memory(+连接 config)]`；清单 22 行；`templates.{}.plugins` 仍为 persona-only。

## 3. 大型测试验收（SC-001 / SC-002；宪章 VI：实际执行、全用例通过）

经 testplan skill 执行四 suite（`projects/game/testplan/system_test.yaml`），完成部署→测试→清理闭环：

```text
guitar run projects/game/testplan/system_test.yaml
```

**预期结果**（与改造前对照：除 planner prompt 恢复玩法外零回归）：

- **game-system**（8 cases）：preset 闭环、对话面、game 回路、memory/wiki 存储 API 全过——含 `TestAgentV2TeamGameStatsPromptFaces` 的**三个新增显式断言**（FR-018）：
  1. planner system_prompt 含 `## 扫雷玩法与可用操作`（SC-002 主锚）；
  2. player prompt 兼含玩法标题与 `## saolei (Minesweeper tools)`（SC-006 player 面等价）；
  3. planner prompt 不含 guidance 标题（角色边界）。
- **game-disconnect / game-stall**：player 回路与流停滞路径零回归（组装时序变化对 player 零影响）。
- **game-memory-down**：planner 物化期 fail-loud 预取/回滚在真实故障拓扑下成立（planner setup 增加玩法注册不改变回滚边界）。
- 全部用例 passed（failed/flaky = 验收未通过，修复重跑）。
- **夹具核实项**（非验收依据）：`projects/game/fake-llm/service/testdata/team_planner.yaml` 零改动，`team-planner-opening` 的 `扫雷玩法` system_keyword 恢复命中（planner 开局回合由该模板应答——执行记录核实；若出现 planner 模板匹配异常，以 §3 显式断言的 GetSystemPrompt 实况为准排查，见 research.md D8 背景风险记录）。

## 4. 行为面抽查（可选人工验证）

1. player 成员对话：工具面仅 saolei 三工具；system prompt 含 persona / team:roster / saolei:game / saolei:guidance，无 memory 痕迹（与改造前逐字一致）。
2. planner 成员物化后查看 system prompt：含 saolei:game（玩法）+ memory 三 section，无 saolei 工具与 guidance（074 遗留修复的直观确认——spec M11）。
3. 游戏回路：saolei_init → operate → 终局 → 播报 → planner 复盘（gameStatsText 播报文案与改造前一致）。
