# Implementation Plan: memory 插件拆分 + 终局回合折叠 + remain 语义澄清

**Branch**: `064-memory-split-fold-remain` | **Date**: 2026-09-14 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/064-memory-split-fold-remain/spec.md`

## Summary

三项独立修正：① memory 插件双面拆分——`@dominion/dsh-memory`（现一个包双挂载面：host 主入口 + `./preset-row` 工具行）拆为 `@dominion/dsh-memory-service`（host 基建，域核心 + `ctx.plannerMemory`）与 `@dominion/dsh-memory`（纯工具面，继承主名），preset 维度规则只引用工具插件（用户裁定，Clarifications 2026-09-14）；② webUI 完成回合折叠规则扩展——"终局收束回合"（无最终答案步且无 interrupted 标记）以末步为锚折叠，与 planner 回合同型；③ `saolei_remain` 三处表述澄清（工具 description / 玩法规则 prompt / 结果体 legend 行）——主语义 = 每数字格周围剩余未标记雷数，显式排除旗数误读并锚定坐标读法。行为零回归是拆分与折叠的共同边界。

## Technical Context

**Language/Version**: TypeScript（ESM，Node ≥ 仓库 NodeNext 配置）；YAML（cordis 组合清单）。

**Primary Dependencies**: `@deepseek-ai/cordis` 0.1.1-rc.2 全家桶（dsh-tools/dsh-agent/dsh-scope/dsh-system-prompt）、grpc-js + proto-loader（memory 服务 client，随拆分归 memory-service 包）；pnpm workspace（新包 `workspace:*`）；bazel + gazelle（`js_runtime_library` runtime_pkg 通道）。

**Storage**: 无新增存储；memory 服务（`dominion:///game/memory:50051`）连接语义不变。

**Testing**: vitest（两个插件包 + web frontend 组件测试，`tools/dev/js/vitest_test.bzl`）；bazel `bazel build //...` / `bazel test //...`；大型测试 guitar（testplan skill，`projects/game/testplan/`）。

**Target Platform**: Linux 容器（agent_v2 服务 + web frontend）。

**Project Type**: web-service（dsh 插件族 + React 前端）。

**Performance Goals**: N/A（行为等价重构 + 文本/呈现变更；无性能面变化）。

**Constraints**: 组合三面原子变更（package.json ⟷ cordis.yml ⟷ 镜像物化，`specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md` §4）；`specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md` 终局收束机制零改动；`plannerMemory` 服务名与 memory 工具契约稳定。

**Scale/Scope**: 新增 1 个 workspace 包；涉改约 11 个源文件 + 10 个测试文件 + 8 处 package/BUILD/组合配置 + 4 份 README/契约文档（精确清单见 tasks.md 各任务）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁（原则） | 状态 | 说明 |
|---|---|---|
| 文档阅读门禁（V） | ✅ | tasks.md 各 phase 按三分类显式列出文档清单，契约/规范引用的间接文档（064 contracts 三份、059 §2/§3/§5、054 data-model §1.5/§5.2 与 revisions、051 data-model §2.5、060 prompt-sections、062 收束契约等）已显式列出 |
| 实现门禁（II 重构式 / III 接口优先） | ✅ | 拆分即重构式变更（边界不符合新认知 → 重构而非补丁）；契约先行：contracts/ 三份文档先于实现定稿 |
| 编译 + 单测门禁（IV） | ✅ | 每切面变更随 `bazel build` + `bazel test`（quickstart 场景 1–3），不单列 task |
| 引用、图表与终态门禁（I / VII / VIII） | ✅ | 代码注释重指向 064 契约；无 `./preset-row` 残留引用（终态）；data-model 图表为 Mermaid |
| 大型测试验收门禁（VI） | ✅ | 收尾 phase 经 guitar 全量执行 agent_v2 系 testplan（部署→测试→清理闭环、全用例通过） |

Phase 1 复核：设计产物（contracts/ + data-model.md + quickstart.md）与宪法无冲突；无 Complexity Tracking 违规项。

## Project Structure

### Documentation (this feature)

```text
specs/064-memory-split-fold-remain/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── dsh-plugins.md       # memory 插件拆分终态契约（修订 059 §3/§5）
│   ├── web-ui.md            # 完成回合折叠规则修订（修订 054 §2.2）
│   └── saolei-plugins.md    # remain 结果体与表述修订（修订 051 §2.2 remain 条目）
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
common/js/dsh-plugins/
├── memory-service/          # 新增：@dominion/dsh-memory-service（host 基建）
│   ├── BUILD.bazel          # gazelle 生成 + runtime_pkg target（npm_deps: grpc-js/proto-loader）
│   ├── package.json
│   └── src/
│       ├── client.ts        # 迁自 memory（MemoryClient + MemoryStore/MemoryEntry）
│       ├── operations.ts    # 迁自 memory（操作语义核心）
│       ├── snapshot.ts      # 迁自 memory（渲染 + section 常量）
│       ├── service.ts       # 迁自 memory（PlannerMemoryService）
│       ├── index.ts         # host 行（provide ctx.plannerMemory；re-export 域核心）
│       └── *.test.ts        # client/operations/service 单测迁移
└── memory/                  # 收缩：@dominion/dsh-memory（纯工具面，继承主名）
    ├── BUILD.bazel          # runtime_deps + memory-service:runtime_pkg；去 grpc 面
    ├── package.json         # 依赖 + @dominion/dsh-memory-service: workspace:*；exports 仅 "."
    └── src/
        ├── tool.ts          # 留驻（跨包导入 service 包类型/常量）
        ├── index.ts         # 原 preset-row.ts 升为主入口（agent 行）
        └── *.test.ts        # tool/index 单测

projects/game/agent_v2/
├── BUILD.bazel              # runtime_deps + memory-service:runtime_pkg
├── package.json             # dependencies + @dominion/dsh-memory-service
├── cordis.yml               # host 行改名；templateRules 终值
├── preset-templates/planner/planner/agent.cordis.yml   # 行名 → @dominion/dsh-memory
├── README.md                # 模板守则描述更新
└── src/dsh.test.ts          # 组合面断言更新

common/js/dsh-plugins/saolei-loop/src/
├── game/text.ts             # remainText 增 legend 行
├── game/runtime.test.ts     # remain 断言补 legend
└── index.ts                 # SAOLEI_GAME_RULES remain/全局计数表述

common/js/dsh-plugins/saolei/
├── src/index.ts             # saolei_remain description + SAOLEI_GUIDANCE 工具条目
├── src/index.test.ts        # 措辞断言
└── README.md

projects/game/web/frontend/src/components/
├── ChatView.tsx             # CompletedTurn 三分类折叠规则
└── ChatView.test.tsx        # 既有 fixture 修正（:504/:542 补 interrupted）+ 新用例
```

**Structure Decision**: 仓库既有布局内变更——插件族目录（`common/js/dsh-plugins/`）新增 memory-service 包、收缩 memory 包；服务组合面（`projects/game/agent_v2/`）与前端组件（`projects/game/web/frontend/`）原位修改。无新顶层结构。

## Complexity Tracking

> 无宪法违规需论证——拆分是用户裁定的边界修正（原则 II 重构式变更的正面执行），不引入新模式/新抽象层。
