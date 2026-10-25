# Implementation Plan: agent_v2 host 层场景化重构支持主子模型

**Branch**: `080-host-main-sub-model` | **Date**: 2026-10-25 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/080-host-main-sub-model/spec.md`

## Summary

把 agent_v2 host 从"saolei team 场景的专用 host"改造为**多场景 host**：079 的数据级 `SceneSpec`（`projects/game/agent_v2/src/scenes.ts`）演进为行为级**场景注册**（roster 校验、编排工厂、成员投影；场景能力由注册模块内部承载），host 通用路径（session/history/server）消费**场景中立编排 seam**，saolei 全部行为经其场景注册接入后逐字节不变（FR-008）。同时以**最小 sts2 接线**作为主子形态（单主 agent、非 team、无 announcer、无 memory）的端到端验收载体：新建 `@dominion/dsh-sts2-loop` 包提供官方 agent-loop 直驱主 agent 的最小编排，sts2 场景开放（roster=`["main"]` + preset 词汇 `main`），testplan 新增 `game-sts2` suite（FR-009）。face 语义映射（`UpdateTeam` 单成员物化、名册固定、子代理经成员标注可见，FR-006/FR-007）与场景注册契约（FR-001~FR-005）落为 contracts 文档，作为后续 sts2-loop feature（survey §5 S3/S4/S6）与 web feature（S5）的接口输入。

技术要点（决策详见 [research.md](research.md)）：

- 编排 seam 中立接口定义在 host（`projects/game/agent_v2/src/orchestration.ts` 新模块），saolei 经 host 侧薄适配器包装 `TeamOrchestrator`（`@dominion/dsh-saolei-loop`），sts2-loop 新包直接实现同构公开面——依赖方向 host → 插件包，不为两实现新建共享契约包。
- 成员集合（含 announcer）改由编排的**成员投影**派生（seam `members()`），announcer 不变式与 memoryScope/goal 派生移入 saolei 场景注册内部；`SceneSpec.faceMembers` 仅提供 face 成员名/错误文案面（不含能力声明）；统一成员事件流信封沿用 `TeamMemberEvent`（`common/js/dsh-plugins/team/src/team.ts:154`，role 为开放串），名册外角色以 viewless collector 动态投影（只写归并/标注帧，不写成员视角；FR-007 的机制面）。
- `game-sts2` suite 用精简部署 `deploy_sts2.yaml`（mongo + session + fake-llm + proxy + agent-v2-test + gateway），fake-llm fixture 以 `system_keywords` 锚定 sts2 main persona 身份行。

## Technical Context

**Language/Version**: TypeScript（Node ESM，strict）；大型测试二进制 Go（既有 testplan 惯例）。

**Primary Dependencies**: dsh 0.2.0-rc.2 线（升级依据 [specs/073-dsh-0.2-upgrade/plan.md](../073-dsh-0.2-upgrade/plan.md)；组合**行集合**终态 24 行——`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §3、`specs/079-sts2-template-scaffold/data-model.md` §5；`specs/070-dsh-upgrade/contracts/composition-manifest.md` §2 当前 23 行由 T013 同步更新为 24，§1 的版本文字以 073 为准，不在本 feature 修订）：`@deepseek-ai/dsh-agent`/`dsh-agent-loop`/`dsh-session`/`dsh-llm`/`dsh-preset-registry` 等；`third_party/dsh/core` 为同线依赖闭包/版本快照包（非源码）；自研插件 `@dominion/dsh-saolei-loop`（适配对象）、新建 `@dominion/dsh-sts2-loop`；`@dominion/dsh-team`（仅事件信封类型）；pnpm catalog 统一版本（`pnpm-workspace.yaml`）。

**Storage**: Mongo `game_agent_v2.presets`（preset-authoring store，不变；新增 main 池模板为 cordis.yml `templates` 快照条目，非存储变更）。

**Testing**: `bazel test`（JS 单测随仓库 rules_js 通道；Go 大型测试经 testplan skill 执行，`style/large_test.md`）。

**Target Platform**: Linux 服务（agent-v2 `kind: stateful`，gRPC 50051 + gateway `/api/v2`）。

**Project Type**: web-service（gRPC 服务）+ dsh 插件包（workspace 私有包）。

**Performance Goals**: 无新增目标（host 内重构；既有会话面性能不变）。

**Constraints**: saolei 行为零变化（错误文案逐字节、校验时序、编排、prompt sections、大型测试全绿）；proto（`projects/game/agent_v2.proto`）零变更；组合清单行变化必须同步 `dsh.test.ts` 断言与 `specs/070-dsh-upgrade/contracts/composition-manifest.md`（行集合/断言契约）；新增 `sts2/loop` 包必须满足 `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §1/§2（目录树与包名映射）与 §4（`.bazelignore` node_modules 条目 + `js_runtime_library:runtime_pkg` 进 `server_pkg.runtime_deps`）；sts2 开放必须同步 `specs/079-sts2-template-scaffold/{contracts/scene-dispatch.md,contracts/dsh-plugins-layout.md,data-model.md,quickstart.md}` 的 sts2 终态、`projects/game/testplan/{README.md,system_test.yaml}` 的 suite 清单，以及 `specs/059-agent-v2-team-mode/contracts/team-api.md` §2 / `specs/060-agent-v2-team-optimize/{contracts/team-api.md §1,data-model.md §2,research.md R7}` 的场景化补注（activation 由场景决定）。

**Scale/Scope**: 2 个可编排场景（saolei team、sts2 主子最小）；进程内存会话（owner 亲和路由不变）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | 门禁（`.specify/memory/constitution.md`） | 状态 | 说明 |
|---|---|---|---|
| 1 | 文档阅读门禁（原则 V） | ✅ | tasks 阶段按 phase 声明三分类文档清单（含间接引用文档与章节锚点）；本 plan 已列出全部契约与规范锚点 |
| 2 | 实现门禁（原则 II 重构式 / III 接口优先） | ✅ | 本 feature 本身即重构式变更（survey A3）；接口先行——[contracts/scene-registry.md](contracts/scene-registry.md)（seam 契约）与 [contracts/face-main-sub.md](contracts/face-main-sub.md)（face 语义）先于实现定稿 |
| 3 | 编译 + 单测门禁（原则 IV） | ✅ | 每代码变更随 `bazel build` + `bazel test`（相关 target），tasks 不单列 |
| 4 | 引用、图表与终态门禁（原则 I / VII / VIII） | ✅ | 全部文档仓库内相对路径 / 仓库外完整 URL 引用；图表统一 Mermaid；文档只表述终态（被否决案仅在 research.md 决策记录中保留必要理由） |
| 5 | 大型测试验收门禁（原则 VI） | ✅ | FR-008 既有四 suite + FR-009 新增 `game-sts2` suite，经 testplan skill 完整执行部署→测试→清理闭环，全部用例通过为验收；[contracts/testplan-game-sts2.md](contracts/testplan-game-sts2.md) 定稿用例面 |

无门禁违例 → Complexity Tracking 留空。

## Project Structure

### Documentation (this feature)

```text
specs/080-host-main-sub-model/
├── plan.md                        # 本文件
├── research.md                    # Phase 0：决策 D1-D8
├── data-model.md                  # Phase 1：实体与状态转移
├── quickstart.md                  # Phase 1：验证指南（含 SC-002 grep 检查面）
├── contracts/
│   ├── scene-registry.md          # 场景注册契约（seam 中立接口、成员投影、事件信封）
│   ├── face-main-sub.md           # 主子 face 语义映射（S2：单成员物化、名册固定）
│   ├── sts2-loop-minimal.md       # @dominion/dsh-sts2-loop 最小编排包契约
│   └── testplan-game-sts2.md      # game-sts2 suite（部署/fixture/用例）
└── tasks.md                       # Phase 2 输出（/speckit.tasks）
```

### Source Code (repository root)

```text
common/js/dsh-plugins/sts2/
├── game/                          # @dominion/dsh-sts2 骨架（保持不变，FR-010）
└── loop/                          # @dominion/dsh-sts2-loop（本 feature 新建）
    ├── package.json / tsconfig.json / BUILD.bazel（gazelle 生成）
    ├── README.md
    └── src/
        ├── orchestrator.ts        # Sts2Orchestrator：物化/驱动/事件/快照/收束
        ├── drive.ts               # 单 agent 驱动原语（inject+followup/whenIdle/steer）
        ├── events.ts              # main agent 事件 → TeamMemberEvent 信封（role="main"）
        ├── orchestrator.test.ts / drive.test.ts / events.test.ts
        └── index.ts               # 导出面 + 插件行 sts2-loop（本期空能力 apply）

projects/game/agent_v2/
├── cordis.yml                     # 新增 sts2-loop 行；preset-authoring templates 快照新增 main 池模板
├── package.json                   # 声明 @dominion/dsh-sts2-loop workspace 依赖
└── src/
    ├── orchestration.ts           # 场景中立编排 seam 接口（SceneOrchestration 等）
    ├── scenes.ts                  # SceneSpec 演进：编排工厂 + faceMembers + sts2 场景开放
    ├── scene-saolei.ts            # saolei 场景注册（TeamOrchestrator 适配器 + announcer/memoryScope/goal 内化）
    ├── scene-saolei.test.ts       # saolei 适配器单测（goal/memoryScope/announcer/faceMembers）
    ├── scene-sts2.ts              # sts2 场景注册（Sts2Orchestrator 工厂）
    ├── session.ts                 # 通用路径泛化：成员投影派生、动态角色投影、移除双成员/announcer 硬编码
    ├── history.ts                 # AgentMemberRole/MemberRole → string；views Map + recordView（viewless 动态投影）
    ├── dsh.test.ts                # 组合清单断言更新（sts2-loop 行）
    └── session.test.ts / history.test.ts / scenes.test.ts / server.test.ts  # 单测随变更更新

projects/game/testplan/
├── system_test.yaml               # suites 末尾新增 game-sts2；顶部 suite 描述同步
├── deploy_sts2.yaml               # 精简拓扑（mongo/session/fake-llm/proxy/agent-v2-test/gateway）
├── sts2_conversation_test.go      # game-sts2 用例（新 binary；T014 建常量骨架，T016 补用例）
├── BUILD.bazel                    # 新 go_largetest target
├── README.md                      # suite 清单新增 game-sts2（五 suite）
└── agent_v2_helpers_test.go       # templateID 参数化（saolei 既有路径零语义变化）

projects/game/fake-llm/service/
├── BUILD.bazel                    # service embedsrcs 新增 sts2_main.yaml
├── testdata/sts2_main.yaml        # sts2 main persona 锚定 fixture（system_keywords + 唯一触发词）
└── message_store_test.go          # 嵌入断言同步（lockstep）
```

**Structure Decision**: 沿用 079 建立的 `dsh-plugins/{scene}/{game,loop,...}` 专用分组布局（本 feature 只新增 `sts2/loop/`）；host 侧场景注册留在 `projects/game/agent_v2/src/`（079 先例：SceneSpec 在 host）；大型测试与 fixture 按既有 testplan/fake-llm 组织（`style/large_test.md`）。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违例，留空。
