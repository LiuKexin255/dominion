# Implementation Plan: Agent v2 Team 模式迁移（player + planner 双 agent）

**Branch**: `059-agent-v2-team-mode` | **Date**: 2026-09-09 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/059-agent-v2-team-mode/spec.md`

## Summary

把 agent v1 的 team 模式迁移到 agent v2：session 组织从单 agent 单例升级为 team（player + planner 双顶层 agent，群聊消息模型）；saolei-loop 从 agent loop 层升为 team loop 编排层（agent 驱动回归官方 `dsh-agent-loop` 行）；preset 按 role 分池并经 roster 机制绑定角色工具插件行（player→saolei 工具组、planner→memory 工具组+记忆快照）；web UI 提供 1 个团队视图 + 每成员视角视图 + 成员 system prompt 查看；同时完全移除 agent v1 的代码与引用。compact 明确排除。

技术路线以四份前期调研为基线：`survey/deepseek-harness-team-mode.md`（§6/§9 架构与堆叠基线）、`survey/deepseek-harness-memory-plugin.md`（决策 ①–⑧）、`survey/deepseek-harness-roster-verification.md`（roster 实证 + §4.2 迁移路径）、`survey/deepseek-harness-agent-loop-prereq.md`（官方 loop 机制）。全部技术决策的依据与备选见 [research.md](research.md)。

## Technical Context

**Language/Version**: TypeScript（Node ESM，agent_v2 + dsh 插件）+ Go（gateway/session 等既有服务）+ React 18（web 前端）；Bazel 构建。

**Primary Dependencies**: `@deepseek-ai/dsh` 全家桶 0.1.1-rc.2 精确 pin（本 feature 新增组合行：`dsh-agent-loop`、`dsh-agent-presets`）；自研插件族 `@dominion/dsh-{llm-glm,desktop-bridge,saolei,saolei-loop}` + 新增 `@dominion/dsh-team`、`@dominion/dsh-memory`；grpc-js；Mongo（preset 存储）；web 沿用 `@deepseek-ai/dsh-client-ui-primitives`。

**Storage**: Mongo `game_agent_v2.presets`（演进为 roster authoring 的 Store 实现，含 role）；memory 数据沿用既有 memory 服务（Mongo `game_memory`，零变更）。

**Testing**: vitest（单测）+ `bazel test`；大型测试经 testplan skill（`guitar run`，fake-llm + fake-desktop）。

**Target Platform**: Linux 服务（deploy 平台部署 agent-v2/gateway/web 等）。

**Project Type**: web-service（gRPC + REST gateway）+ web UI。

**Performance Goals**: 沿用现状（单实例 stateful + proxy owner 亲和）；无新增性能目标。

**Constraints**: dsh 0.1.1-rc.2 破坏性变更风险由精确 pin 承担（既有决策）；组合清单三面（package.json ⟷ cordis.yml ⟷ tar 物化）闭包审计要求原子变更（`survey/deepseek-harness-roster-verification.md` §5 对照 3）。

**Scale/Scope**: 单实例低并发（既有语义）；每 session 一个 team、恰 2 成员。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

对照 `.specify/memory/constitution.md`（v1.4.0）七原则与五门禁，本 plan 的合规承载：

| 原则/门禁 | 承载 |
|---|---|
| I 引用溯源 | research/contracts/data-model 全部结论附仓库内相对路径或仓库外 URL 引用 |
| II 重构式变更 | v1 移除与 team 架构演进作为同一变更交付；saolei-loop 层级转变（agent loop→team loop）是架构收缩而非堆叠补丁——自研 driver 的 8 项重写清单（prereq §4.7）随官方 agent-loop 回归而消解 |
| III 接口优先设计 | Phase 1 产出 [contracts/](contracts/)（team-api、preset-api、dsh-plugins、web-views）先于 tasks/实现 |
| IV 测试颗粒度 | 编译+单测归属各实现变更（不单列 task）；大型测试单独验收（quickstart.md + testplan） |
| V 编码前阅读文档 | 由后续 `/speckit.tasks` 在 tasks.md 中按三分类清单声明 |
| VI 大型测试验收 | quickstart.md 定义验证场景；验收必须实际 `guitar run` 全量通过 |
| VII 终态表述 | v1 完全移除不留迭代痕迹；被取代方案仅在 research.md 记录必要理由 |

无违规项 → Complexity Tracking 不适用。

## Project Structure

### Documentation (this feature)

```text
specs/059-agent-v2-team-mode/
├── plan.md              # This file
├── research.md          # Phase 0 output — 全部技术决策与依据
├── data-model.md        # Phase 1 output — 实体与资源模型
├── quickstart.md        # Phase 1 output — 端到端验证指南
├── contracts/           # Phase 1 output
│   ├── team-api.md      # AgentService 演进为 team 模型的对外契约
│   ├── preset-api.md    # PresetService role 扩展契约
│   ├── dsh-plugins.md   # 自研插件内部契约（team/saolei-loop/memory/saolei）
│   └── web-views.md     # web 双视图与 system prompt 查看契约
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/
├── agent_v2/                    # 宿主服务演进：team 物化编排消费 ctx.team；gRPC 面按 contracts/team-api.md 重构
│   ├── cordis.yml               # 组合清单变更（R7）：+dsh-agent-loop/+dsh-agent-presets/+memory host 行
│   └── src/                     # server/session/history/presets 按 team 模型重构
├── gateway/cmd/main.go          # 路由按 team API 契约调整（/api/v2）
├── agent_v2.proto               # team 模型协议演进（Team/TeamMember/事件扩展）
├── game.proto                   # 移除 TeamService/PromptService 及 v1 专属消息（保留 Session/UserFrame/TeamFrame/MemoryService）
├── prompt/                      # 整目录移除（v1 专属配置服务）
├── agent/                       # 整目录移除（v1 服务）
└── web/frontend/src/            # team 模型 UI：配置面板/团队视图/成员视角视图/system prompt 查看

common/js/dsh-plugins/
├── team/                        # 新增：群聊原语插件（register/drain/buffer 派生重建/team section）
├── memory/                      # 新增：planner memory 插件（工具行 + host 服务面）+ Mongo-free gRPC client
├── saolei-loop/                 # 重构：agent loop 层 → team loop 编排层（driver.ts 移除，GameRuntime 迁至物化 setup）
├── saolei/                      # 工具插件行（preset 层挂载，结构基本不变）
└── preset-authoring/            # 复用（058 产出）：Store seam 增 Mongo 实现，服务 agent_v2

projects/game/fake-llm/          # v1 planner 夹具移除，新增 team 双角色夹具
testplan（projects/game/testplan/deploy_agent_v2.yaml 及用例）  # team 模式大型测试
```

**Structure Decision**: 沿用仓库既有分区（agent_v2 宿主 + common/js/dsh-plugins 插件族 + web 前端 + gateway），新增 `team`、`memory` 两个插件包；`saolei-loop` 原地重构为 team loop（不做新目录，保持组合行引用稳定）。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违规项，不适用。
