# Contract: 组合终态、迁移分步与验收（cutover）

**Feature**: [spec.md](../spec.md) FR-014–FR-018 | **决策**: [research.md](../research.md) D5–D9 | **数据模型**: [data-model.md](../data-model.md) §5

组合清单终态、原子切换批次、契约修订清单、测试与验收的权威契约。

## 1. 迁移分步与门禁（P1–P4）

research.md D5 表为权威（P1 基础双面期 → P2 工具包 → P3 原子切换 → P4 终态文档——基础先行为类型依赖所决定）。要点：

- **P1 基础双面期**：dsh-saolei 增量改造（game/ 副本迁入含形态改造 + 游戏类型单一出口导出 + 基础面 + 玩法 section + 依赖增补），**保留旧 saoleiTools provide**；saolei-loop/cordis.yml 零改动；saolei-loop 本地 game/ 不动（两份并存，行为用本地版）。
- **P2 工具包并行**：新建 `dsh-saolei-tools`（无消费方；类型从 `@dominion/dsh-saolei` 导入——P1 已就绪；组合行为零变化）。
- **P3 原子切换（单一合入单元，不得部分合入）**：① saolei-loop 消费与所有权切换（inject/面/编排持有游戏实例/注册序/句柄注册/终局直读/announcer import/删 `src/game/`/类型切换/`MountPlayerRuntime` seam 与 session.ts 透传删除）；② dsh-saolei 移除 saoleiTools 双面与工具残留、移除 dsh-saolei-loop 依赖；③ cordis.yml 终态（+顶级 saolei 行、config.plugins 换单、22 行）；④ agent_v2 package.json/BUILD +dsh-saolei-tools；⑤ dsh.test.ts 断言终态化（与 070 契约 §2 锁步）；⑥ testplan 三显式断言；⑦ 四 suite 大型测试执行。**中断恢复判据**：`bazel build //... && bazel test //...` 全绿 + 组合清单处于旧态（无顶级 saolei 行、config.plugins 含 dsh-saolei）或新态（无混合态）。
- **P4 终态文档**：契约修订八份（含 059 路径引用终态化）+ README/注释终态化 + 引用清零复查（纯文档，不重跑四 suite）。

## 2. 组合清单终态（projects/game/agent_v2/cordis.yml）

```yaml
# 顶级行（新增，置于 @dominion/* 行分区、grpc-runtime 基建行同级语境）：
- { id: saolei, name: '@dominion/dsh-saolei' }
# saolei-loop 行（config 变更）：
- id: saolei-loop
  name: '@dominion/dsh-saolei-loop'
  config:
    plugins:
      - name: '@dominion/dsh-saolei-tools'
      - name: '@dominion/dsh-saolei-memory'
        config:
          target: 'dominion:///game/memory:50051'
          protoPath: 'projects/game/game.proto'
```

- 行集合 **21 → 22 行**；其余行零变化；`templates.{}.plugins`（persona-only）零变化。
- 头部注释终态化：saolei-loop 能力依赖行注释改述（`@dominion/dsh-saolei-tools` 提供 saoleiTools、`@dominion/dsh-saolei-memory` 提供 memoryMount）；顶级 saolei 行注释（游戏基础服务，`ctx.saolei` 面）。
- **契约/代码锁步**：`projects/game/agent_v2/src/dsh.test.ts` 的 manifest 断言（22 行集合 + saolei-loop config.plugins 内容 + 顶级 saolei 行无 config）与 `specs/070-dsh-upgrade/contracts/composition-manifest.md` §2 同批修订（074 T005 先例）。

## 3. 契约修订清单（P4）

research.md D7 表为权威（060/074/070/051/064/062/065/059 八份——含 059 的路径引用终态化）。原则：**归属与通路表述改终态；行为条款逐字保留**。

## 4. 测试验收

### 4.1 单测（P1/P2/P3 各步门禁）

- 重组映射：research.md D6 表为权威（game 测试随迁等价；旧 dsh-saolei index.test 主体迁 tools 包改写；dsh-saolei 新面测试；orchestrator/index 测试更新）。
- mock 约定：`style/javascript.md`（DI / `vi.fn`，禁止新增 `vi.mock` 模块拦截）；harness 注入 stub 基础面（`ctx.provide("saolei", {...})`）。

### 4.2 大型测试（P3 ⑦，宪章 VI）

- 经 testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml` 四 suite（game-system / game-disconnect / game-memory-down / game-stall），全部用例通过（all cases passed；failed/flaky = 验收未通过）。
- **新增三显式断言**（FR-018；落点 `projects/game/testplan/agent_v2_game_test.go` `TestAgentV2TeamGameStatsPromptFaces`，断言只增不改既有用例语义）：
  1. planner `GetSystemPrompt()` 含 `## 扫雷玩法与可用操作`（saolei:game 标题）；
  2. player prompt 兼含玩法标题与 `## saolei (Minesweeper tools)`（guidance 标题）；
  3. planner prompt 不含 guidance 标题。
- **夹具核实项**（非验收依据）：`projects/game/fake-llm/service/testdata/team_planner.yaml` 零改动；`team-planner-opening` 的 `扫雷玩法` system_keyword 恢复命中。执行记录中核实。
- Go 断言规范：`style/golang.md`（经 `style/large_test.md` 反查）。

## 5. 静态终态复查（P4）

```bash
# 依赖方向归位（SC-003）：dsh-saolei 内对上层的引用清零
grep -rn "dsh-saolei-loop\|dsh-saolei-tools\|dsh-saolei-memory" common/js/dsh-plugins/saolei/src/ | grep -v test
# 旧导出路径引用清零（SC-004）：saolei-loop 游戏导出消费清零
grep -rn "createAgentGameRuntime\|gameStatsText\|GameEventRecord\|ToolOutcome\|OperateInput" \
  common/js/ projects/game/ --include="*.ts" | grep "dsh-saolei-loop"
# 旧符号清零（SC-004，T019 ④）：迁移中消失的符号在代码引用（import/类型位置/标识符使用）中清零
# 冻结文案豁免（按行排除，依据见下方判定说明）：saolei-tools resolveRuntime 的 fail-loud 文案
# 逐字含 "(the materialization setup registers it on agent.ctx via createAgentGameRuntime)"
grep -rnE "createAgentGameRuntime|GameRuntimeService|MountPlayerRuntime" \
  common/ projects/ --include="*.ts" \
  | grep -vF "(the materialization setup registers it on agent.ctx via createAgentGameRuntime)"
# 目录终态（SC-004）：旧实现消失
ls common/js/dsh-plugins/saolei-loop/src/game/   # 应不存在
```

预期：四组检索零命中（specs/ 内本特性文档除外）；`common/js/dsh-plugins/saolei-tools/` 与 `common/js/dsh-plugins/saolei/src/game/` 存在。

**冻结文案例外判定（T019 ④，2026-10-01 裁定）**：`@dominion/dsh-saolei-tools` `resolveRuntime` 的"无 saoleiGame 服务"fail-loud 文案（`common/js/dsh-plugins/saolei-tools/src/index.ts:127`）逐字含 `via createAgentGameRuntime` 字样——该文案是 T007 自旧 dsh-saolei saoleiTools 面**逐字不变迁入的冻结运行时行为值**（模型可见错误输出，Phase 4 已验收合入），其中符号名属字符串字面量引述、不构成 import/类型/标识符使用，故豁免于"代码引用清零"，机械判据即上述 `grep -vF` 行排除（该字样在 common/ projects/ 的 *.ts 中终态仅此一行——旧 dsh-saolei 双面副本 `saolei/src/index.ts:180` 随 T012 删除）。保留不改的依据：① 改写即重开 T007 已 review 的等价迁移冻结，并使 Phase 5 原子批次（T009–T016，不含 saolei-tools src 编辑）扩面；② `specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md` §5 冻结的是该 fail-loud 的**边界行为**（planner 误调 → 模型可见错误、不收束），文案保留即零影响（064 §2 三拒绝文本冻结对象是 AMBIGUOUS/MISSING/INCOMPLETE 文案，与本文案无关）；③ 宪章 VII 的清零对象是死引用与过时表述（注释/契约/README 等交付物），冻结诊断文案内的符号名引述不在其列。若未来需改写该括注（终态注册路径已改编排 isolate 句柄注册），属模型可见行为面变更，须重开 T007 冻结与 062 §5 评审，不随本特性批次。另：第二组命令以 `| grep "dsh-saolei-loop"` 收窄匹配行，天然不命中该冻结文案行，无需豁免。
