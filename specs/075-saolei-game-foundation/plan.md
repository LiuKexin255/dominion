# Implementation Plan: saolei 游戏基础与智能体工具解耦（dsh-saolei / dsh-saolei-tools）

**Branch**: `075-saolei-game-foundation` | **Date**: 2026-10-01 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/075-saolei-game-foundation/spec.md`

## Summary

按"游戏领域归位"重新划分 saolei 体系：**`@dominion/dsh-saolei` 重新定义为 saolei 游戏基础插件**（组合清单**顶级基础行**，`ctx.provide("saolei")` 提供基础服务面：`createGameRuntime(sessionName, desktopBridge)` 纯领域工厂——普通类构造、无 scope 注册副作用——+ `saolei:game` 玩法 section spec），游戏运行时体系（`game/` 四文件约 1,000 行 + 测试）从 saolei-loop **文件级等价迁入**，玩法提示词内容归游戏基础持有；**新建工具插件 `@dominion/dsh-saolei-tools`**（saolei-loop `config.plugins` 子插件，延续 074 能力提供方模式），三工具 + 双形式校验 + `saolei:guidance` 等价迁入，工具经 dsh-saolei 的类型与运行时 API 操作游戏（依赖方向归位：tools → 基础、loop → 基础，消除现状 dsh-saolei 对 saolei-loop 的类型反向依赖）；**saolei-loop 编排正向依赖基础面**（`inject: ["saolei"]`）且**持有游戏实例**（materialize 时经基础面工厂构造、生命周期 = 物化周期——`GameRuntimeService` 的 cordis Service 形态移除，实例为编排字段）——player setup 注册 saoleiTools 单元（三工具 + guidance）+ `saolei:game` + **游戏句柄**（编排持有的实例注册进 player agent ctx `isolate("saoleiGame")`，memory face 同型，工具 `exec.agent.ctx.saoleiGame` 解析零变化），**planner setup 注册 `saolei:game`**（074 遗留修复：planner 系统提示词恢复游戏玩法可见性，唯一预期行为变化）+ memory 单元；编排终局事件直读自身字段（`MemberRuntime.game`/`MountPlayerRuntime` seam/session.ts 透传删除）；announcer 留编排层、`gameStatsText` 改从基础导入。大型测试验收以 **testplan 显式 prompt 断言**为主锚（Clarifications 2026-10-01 Option B：planner prompt 含玩法标题、player 兼含玩法与 guidance、planner 无 guidance），fake-llm 夹具锚点恢复命中为核实项。

技术方案的关键裁定（面命名与内容、注册序、迁移分步与原子切换批次、契约修订清单）见 [research.md](research.md) D1–D9；接口契约见 [contracts/](contracts/)；实体与导出面迁移映射见 [data-model.md](data-model.md)；端到端验证见 [quickstart.md](quickstart.md)。

## Technical Context

**Language/Version**: TypeScript（ESM，`common/js/dsh-plugins/*` 与 `projects/game/agent_v2` 既有栈）；构建/测试入口 Bazel；testplan Go 用例（大型测试断言）。

**Primary Dependencies**: `@deepseek-ai/cordis`（插件运行时：provide/inject、Service、declare module Context 增强）、`@deepseek-ai/dsh-tools`（defineTool）、`@deepseek-ai/dsh-system-prompt`（PromptSection 类型）、`@deepseek-ai/dsh-agent`（Agent/AgentHandle 类型——saolei-loop 编排消费；dsh-saolei 迁移后不再引用，不作其 peer）、`@dominion/dsh-desktop-bridge`（DesktopBridgeService 类型 + 运行时服务，随迁 peer）、`@dominion/game-saolei-board`（棋盘识别库，依赖随迁至 dsh-saolei）、`@dominion/dsh-team`（编排既有编译依赖先例）。

**Storage**: N/A（无存储变化；纯包边界重新划分）。

**Testing**: vitest 单测（bazel `ts_project` + `vitest_test`，随代码变更执行）；大型测试经 testplan skill 执行 `projects/game/testplan/system_test.yaml` 四 suite（game-system / game-disconnect / game-memory-down / game-stall，宪章原则 VI）+ 新增三个显式 system_prompt 断言（FR-018）。

**Target Platform**: Linux server（agent_v2 服务组合；部署拓扑零变化）。

**Project Type**: common 插件库重构（dsh-saolei 原地改造为游戏基础、dsh-saolei-tools 新增、saolei-loop 消费切换）+ agent_v2 组合清单变更 + testplan 断言新增。

**Performance Goals**: 行为零回归（player prompt/工具行为/编排状态机逐字等价）；唯一预期变化 = planner prompt +saolei:game。

**Constraints**: 全部移动等价迁移（工具 schema/文案、guidance/玩法 section 名/order/文本、游戏运行时行为契约零变化）；迁移每步 `bazel build //...` + `bazel test //...` 可通过；切换批次原子合入（双重注册风险）；依赖方向单向（基础不依赖上层）。

**Scale/Scope**: game/ 迁移约 1,000 行 + 测试 1,161 行（含 `GameRuntimeService` Service 形态移除与工厂签名改造）；dsh-saolei 工具部分迁出约 300 行（index.ts 420 行主体）；saolei-loop 消费与所有权切换（orchestrator/index/announcer/session.ts 透传清理 + 测试）；cordis.yml 22 行终态；契约修订八份（060/074/070/051/064/062/065 + 059 路径引用终态化）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 评估 | 状态 |
|---|---|---|
| I. 引用溯源 | plan/research/contracts/data-model/quickstart 引用使用仓库相对路径或完整 URL；等价迁移的比对基准（迁移源文件与既有契约）在 research.md D6/D8 列明 | ✅ |
| II. 重构式变更 | 本特性即重构式：修复依赖方向倒挂（dsh-saolei → saolei-loop 反向类型依赖）与领域/能力混置（游戏本体在编排层、玩法提示词在工具单元）；架构调整与遗留问题修复（planner 玩法可见性）同批交付 | ✅ |
| III. 接口优先设计 | contracts/ 四份契约先于实现定稿：游戏基础面（saolei-foundation）、工具插件单元（saolei-tools-plugin）、编排消费与注册序（team-loop-assembly）、切换与验收（cutover-contracts） | ✅ |
| IV. 测试颗粒度与执行频率 | 单测随各迁移步执行（不单列 task）；大型测试在原子切换 phase 单独分配验收任务（含新增显式断言） | ✅ |
| V. 编码前阅读文档 | tasks.md 按三分类格式为每个 phase 显式列出文档清单（含间接引用文档：style/large_test.md 反查 style/golang.md、esm-package-conventions 及其引用的 048 research §R5、Phase 3 的 074 grpc-runtime §1 行先例、Phase 5 的 074 team-assembly / 060 prompt-sections 契约原文、迁移源文件与测试）；规划时已实际阅读所列文档 | ✅ |
| VI. 服务型应用大型测试验收 | SC-001：四 suite 经 testplan skill 实际执行全用例通过 + FR-018 三个显式 prompt 断言；禁止仅构建检查（quickstart.md 验证步骤） | ✅ |
| VII. 终态表述 | 死引用/过时注释/契约/README 清理是显式交付物（FR-016）；saolei-loop 不再导出游戏类型（单一出口，无兼容期双出口）；被否决方案只在 research.md 必要存档 | ✅ |
| VIII. 文档图表格式 | 本 plan、data-model、contracts 中图表全部 Mermaid | ✅ |

无违规项 → Complexity Tracking 无需填写。

## Project Structure

### Documentation (this feature)

```text
specs/075-saolei-game-foundation/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── saolei-foundation.md
│   ├── saolei-tools-plugin.md
│   ├── team-loop-assembly.md
│   └── cutover-contracts.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
common/js/dsh-plugins/
├── saolei/                # 原地改造：saolei 游戏基础（顶级基础行，ctx.provide("saolei")）
│   └── src/
│       ├── index.ts       # 插件入口（name/inject/apply 面提供）+ 玩法 section 常量 + 导出汇总
│       ├── index.test.ts  # 重写：基础面内容断言（面存在、工厂、gameSection 逐字、无工具）
│       └── game/          # 迁入（自 saolei-loop，文件级等价迁移）
│           ├── runtime.ts + runtime.test.ts
│           ├── board.ts
│           ├── geometry.ts
│           └── text.ts + text.test.ts
├── saolei-tools/          # 新增：智能体工具插件（config.plugins 子插件，provide saoleiTools）
│   ├── package.json / tsconfig.json / .swcrc / BUILD.bazel
│   └── src/
│       ├── index.ts       # 三工具 + 双形式校验 + v1 拒绝字面量 + saolei:guidance（等价迁入）
│       └── index.test.ts  # 自旧 dsh-saolei index.test.ts 迁移改写（单元无 saolei:game 断言等）
└── saolei-loop/           # 消费与所有权切换：编排层（inject "saolei"，删 src/game/，持有游戏实例字段）
    └── src/
        ├── index.ts       # inject "saolei"（装载期 fail-loud）+ apply 双能力面检查（saoleiTools/memoryMount）；移除 game/ 再导出、MountPlayerRuntime 类型与 saoleiGame declare module
        ├── orchestrator.ts # 基础面消费（工厂/section/游戏实例持有）、player/planner setup 注册、终局直读自身字段
        ├── announcer.ts   # gameStatsText 改从 @dominion/dsh-saolei 导入
        └── orchestrator.test.ts / index.test.ts  # harness + 断言更新
projects/game/agent_v2/
├── cordis.yml             # +顶级 saolei 行；saolei-loop config.plugins → [dsh-saolei-tools, dsh-saolei-memory]（22 行）
├── package.json / BUILD.bazel   # +@dominion/dsh-saolei-tools 依赖声明
├── src/dsh.test.ts        # manifest 22 行 + config.plugins 内容断言终态化
└── src/session.ts         # MountPlayerRuntime 透传与 deps 清理（编排类型导入面收缩）
projects/game/testplan/
└── agent_v2_game_test.go  # TestAgentV2TeamGameStatsPromptFaces 增三个显式 system_prompt 断言（FR-018）
```

**Structure Decision**: 沿用仓库既有 `common/js/dsh-plugins/*` cordis 插件包布局（048 esm-package-conventions）；dsh-saolei 原地改造（同包语义变化：工具提供方 → 游戏基础），`game/` 保持子目录结构随迁（文件级等价，最小 diff）；dsh-saolei-tools 新包同型。组装接线在 `projects/game/agent_v2/cordis.yml`（顶级行 + config.plugins）。不引入新顶层目录、不新增存储。

## Complexity Tracking

> 无 Constitution Check 违规，无需填写。
