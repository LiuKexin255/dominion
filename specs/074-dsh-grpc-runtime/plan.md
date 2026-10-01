# Implementation Plan: gRPC client 基建插件、memory 能力化与 preset 模板简化

**Branch**: `074-dsh-grpc-runtime` | **Date**: 2026-09-30 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/074-dsh-grpc-runtime/spec.md`

## Summary

按基建/能力/组装三层模型重构 agent_v2 的 gRPC client 通路与 preset 模板机制：新增组合级 **grpc-runtime 基建插件**（`ctx.grpc` 服务面：类型化 client 构造 + unary 调用辅助 + channel/packageDefinition 缓存共享 + canonical protoPath 解析，吸收现状 memory-service 连接基建（client.ts 473 行））；**memory-service 整体移除**，`@dominion/dsh-memory` **退化为公共库 `@dominion/common-js-memory`**（迁址 `common/js/memory/`：原子能力单元工厂 + 原语，无插件形态）；成员能力组装从 preset 模板插件行迁入 **team 层成员物化 setup**——**依赖声明 × 提供方挂载 × 编排消费**（cordis 原生 DI）：saolei-loop Config 扁平能力依赖列表（`ctx.plugin` 挂载 + `ctx.provide` 提供实现；`@dominion/dsh-saolei` 改造为 saoleiTools 静态单元提供方，薄 adapter `@dominion/dsh-saolei-memory` 以 config 携带 target/canonical protoPath 提供 memoryMount 工厂），编排代码按角色消费能力单元注册到成员上下文（`build(scope)` 显式传参 → faces → tools → sections → await ready，fail-loud 预取失败即回滚）；**templateRules 移除**、`templates.{}.plugins` 语义收敛为即插即用自包含插件列表、registry 瘦身保留。

技术方案的关键裁定（含用户组装机制四轮收敛、adapter 与库化、canonical protoPath 等决策）见 [research.md](research.md) D1–D14；接口契约见 [contracts/](contracts/)；实体与状态迁移见 [data-model.md](data-model.md)；端到端验证见 [quickstart.md](quickstart.md)。

## Technical Context

**Language/Version**: TypeScript（ESM，`projects/game/agent_v2` 与 `common/js/dsh-plugins/*` 既有栈）；构建/测试入口 Bazel。

**Primary Dependencies**: `@deepseek-ai/cordis`（插件运行时）、deepseek-harness 插件面（`@deepseek-ai/dsh-tools` / `dsh-system-prompt` / `dsh-agent` / `dsh-agent-loop`）、`@deepseek-ai/dsh-agent-preset-registry`（官方 `ctx.agentPresets` 面——preset-authoring 消费；saolei-loop 角色挂载不依赖它，见 research.md D4/D13）、`@grpc/grpc-js` 1.14.4、`@grpc/proto-loader`、`@dominion/common-js-grpc-resolver`（dominion URI scheme，幂等注册）、`@dominion/common-js-grpc-otel`（载入顺序约束：先于 grpc-js 模块加载）、`ts_proto_library`（proto-loader-gen-types 类型生成，`//projects/game/agent_v2:agent_v2_types`，作为结构化调用面的可选兼容性校验参照）。

**Storage**: N/A（无新增存储；preset Mongo store 语义不变）。

**Testing**: vitest 单测（bazel `ts_project` + `vitest_test`，随代码变更执行）；大型测试经 testplan skill 执行 `projects/game/testplan/system_test.yaml` 四 suite（game-system / game-disconnect / game-memory-down / game-stall，宪章原则 VI：全用例通过为验收）。

**Target Platform**: Linux server（agent_v2 服务组合；部署拓扑不变——memory 仍是独立部署服务 `projects/game/memory/service.yaml`，本特性纯 client 侧）。

**Project Type**: common 插件库（grpc-runtime 新增、memory/saolei 重构、preset-authoring/saolei-loop 修改）+ agent_v2 组合变更。

**Performance Goals**: 行为零回归（既有回路延迟/吞吐不变化）；连接共享——同 target 的 MemoryService/WikiService client 共享单一 channel（现状两条连接）。

**Constraints**: grpc-otel 载入顺序（基建不得模块顶层副作用初始化）；组合级生命周期（channel 随组合 teardown）；迁移每步 `bazel build //...` + `bazel test //...` 可通过；能力包领域无关（无场景词汇、无部署布局知识）。

**Scale/Scope**: memory-service 2,080 行（8 文件 + 5 测试文件）迁移/移除；dsh-memory 576 行重构；preset-authoring templateRules 机制移除；saolei-loop 组装 seam 改造；agent_v2 组合清单与 session.ts 接线。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 评估 | 状态 |
|---|---|---|
| I. 引用溯源 | plan/research/contracts/data-model/quickstart 全部引用使用仓库相对路径或完整 URL；代码迁移的 why 注释清单已列（research.md D3） | ✅ |
| II. 重构式变更 | 本特性即重构式：消除每域一行样板（架构不满足需求→重构）、移除 templateRules 层序倒置（过度设计→简化）；架构调整与功能变更同批交付 | ✅ |
| III. 接口优先设计 | contracts/ 四份契约先于实现定稿：grpc-runtime 服务面、能力包导出面、组装 seam、preset 模板语义 | ✅ |
| IV. 测试颗粒度与执行频率 | 单测随各迁移步执行（不单列 task）；大型测试在 cutover 后作验收（tasks.md 单列） | ✅ |
| V. 编码前阅读文档 | tasks.md 按三分类格式为每个 phase 显式列出文档清单（含间接引用文档，无引用传递——Phase 6 由 `style/large_test.md` 反查出 `style/golang.md`、Phase 3 列出组合清单管辖契约 `specs/070-dsh-upgrade/contracts/composition-manifest.md` 与 `common/js/grpc/otel/src/index.test.ts`、Phase 4 列出快照冻结依据 `survey/planner-memory-and-agent-communication.md` 与迁移源测试、Phase 5 以 research.md D13 第 3 条 + `projects/game/agent_v2/cordis.yml` 现状注释为裸包名解析依据；所列地址直达内容，无索引页；无相关内容显式标注"无"） | ✅ |
| VI. 服务型应用大型测试验收 | SC-001：四 suite 经 testplan skill 实际执行全用例通过；禁止仅构建检查（quickstart.md 验证步骤） | ✅ |
| VII. 终态表述 | 死代码/过时 fixture/契约/README 清理是显式交付物（spec US5、research.md D8/D11 P4）；被否决方案只在 research.md 必要存档 | ✅ |
| VIII. 文档图表格式 | 本 plan、data-model、contracts 中图表全部 Mermaid | ✅ |

无违规项 → Complexity Tracking 无需填写。

## Project Structure

### Documentation (this feature)

```text
specs/074-dsh-grpc-runtime/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── grpc-runtime.md
│   ├── memory-library.md
│   ├── team-assembly.md
│   └── preset-templates.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
common/js/
├── memory/                # 新增（迁址）：memory 公共库（@dominion/common-js-memory，原 dsh-memory 退化）
│   └── src/{index.ts, capabilities.ts, store.ts, face.ts, tool.ts, wiki-tools.ts, operations.ts,
│            wiki-operations.ts, snapshot.ts, guidance.ts, errors.ts} + *.test.ts
└── dsh-plugins/
    ├── grpc-runtime/      # 新增：组合级 gRPC client 基建插件（@dominion/dsh-grpc-runtime）
    │   └── src/{index.ts, channel.ts, client.ts, unary.ts, ...} + *.test.ts
    ├── saolei-memory/     # 新增：薄 adapter（@dominion/dsh-saolei-memory）——config: target/protoPath
    │   └── src/{index.ts} + *.test.ts
    ├── memory/            # 删除：原 @dominion/dsh-memory（退化为公共库迁址 common/js/memory/）
    ├── memory-service/    # 删除：整包（含测试与 BUILD）
    ├── saolei/            # 轻度改造：静态单元提供方（ctx.provide("saoleiTools")；工具/文案零变化）
    ├── saolei-loop/       # 修改：config.plugins 扁平依赖列表 + ctx.plugin 装配 + 编排消费序 + loadPlannerMemory 移除
    └── preset-authoring/  # 修改：templateRules 机制移除（Config/校验/类型/测试）
projects/game/agent_v2/
├── cordis.yml             # 组合清单终态：+grpc-runtime 行；-memory-service 行；saolei-loop config 增扁平能力依赖列表；
│                          # 模板工具行与 templateRules 移除（contracts/preset-templates.md §5）
└── src/session.ts         # loadPlannerMemoryBinding 移除（无新增组装代码；memoryScope 流不变）
projects/game/agent_v2/src/dsh.test.ts  # 组合/装配断言更新（终态形态）
```

**Structure Decision**: 沿用仓库既有 common 插件布局；基建插件与能力提供方插件均为 `common/js/dsh-plugins/*` 下的 cordis 插件包（grpc-runtime 以组合行挂载；saolei/saolei-memory 作为 saolei-loop 能力依赖列表的子插件挂载，不再以模板行/宿主行挂载），memory 退化为 `common/js/memory/` 公共库。组装接线在 `projects/game/agent_v2/cordis.yml`（部署数据）与 saolei-loop 代码；`session.ts` 仅移除 `loadPlannerMemoryBinding`、无新增组装代码。不引入新顶层目录。

## Complexity Tracking

> 无 Constitution Check 违规，无需填写。
