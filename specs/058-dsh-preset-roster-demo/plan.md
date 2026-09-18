# Implementation Plan: 058 dsh preset roster demo

**Branch**: `058-dsh-preset-roster-demo` | **Date**: 2026-09-08 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/058-dsh-preset-roster-demo/spec.md`

## Summary

在 047 demo（`experimental/dsh/demo`）上验证 dsh roster（`@deepseek-ai/dsh-agent-presets`）机制与 C1 preset 扩展模式：demo agent 组合**从 spine 打包改造为 agent_v2 同型直组**（R12），挂 roster + 自研通用 authoring 插件（`@dominion/dsh-preset-authoring`，copy-then-patch 物化 + Store seam 内存实现）；API 面扩 CreateConversation（显式会话绑定 preset）与 PresetService CRUD；fake-llm 增 `system_keywords` 匹配条件承载端到端组合断言。验证矩阵 V1-V4 全覆盖（单测 + 大型测试），验证完成后按实践写 survey/（FR-010）。

## Technical Context

**Language/Version**: TypeScript 6 / Node ESM（grpc-js 服务）；Go（fake-llm 扩展）；Starlark（bazel）

**Primary Dependencies**:
- 新增（catalog，均 0.1.1-rc.2 同线，R1/R12）：`@deepseek-ai/dsh-agent-presets`、`@deepseek-ai/dsh-persona`、`@deepseek-ai/dsh-agent-loop`
- 新增（workspace）：`@dominion/dsh-preset-authoring`（`common/js/dsh-plugins/preset-authoring`）、`@dominion/dsh-demo-echo`（`experimental/dsh/demo/agent-plugins/demo-echo`）
- 复用：`js-yaml`（catalog 已有，插件包物化用）

**Storage**: preset 动态字段 = 插件内 Store seam 的内存实现（重启丢失，demo 已知限制）；物化文件 = writable root emptyDir；模板 = 镜像内数据目录

**Testing**: vitest（单测，`bazel test`）；大型测试 `guitar run experimental/dsh/demo/testplan/interface_test.yaml`（testplan skill，全部用例通过为验收）

**Target Platform**: Linux 容器（k8s 部署，demo 拓扑不变：gateway → agent → fake-llm）

**Project Type**: 实验性验证服务（PoC）+ 平台插件包

**Performance Goals**: 无新增性能目标（roster 官方实测：preset 挂载 ~38-135ms/次，demo 场景无感）

**Constraints**: dsh 0.1.1-rc.2 线精确 pin（FR-009）；service 层零 roster/fs 引用（FR-005）；047 既有大型测试用例全部保持通过

**Scale/Scope**: demo 规模（单实例）；模板 2 份；测试用例新增 ~8-10 个大型场景

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 检查 | 状态 |
|---|---|---|
| I 引用溯源 | 本 plan 与 research/contracts 全部结论带仓库相对路径或完整 URL 引用 | ✅ |
| II 重构式变更 | R12 基座对齐（spine → 直组）与 roster 能力作为同一变更交付，非补丁堆叠 | ✅ |
| III 接口优先 | Phase 1 产出五份契约（chat-api / composition-manifest / preset-authoring-plugin / demo-echo-plugin / fake-llm-system-keywords），实现前定契约 | ✅ |
| IV 测试颗粒度 | 每 task 内 `bazel build`+`bazel test`；大型测试单独验收 task（FR-008） | ✅ |
| V 编码前阅读 | tasks 阶段按三分类（代码规范/官方文档/技术参考）逐 phase 列文档清单 | ✅（tasks 命令落实） |
| VI 大型测试验收 | `guitar run` 完整闭环 + 全部用例通过，禁止构建检查替代（FR-008） | ✅ |
| VII 终态表述 | 交付文档只表述终态；discussion-2026-09-08.md 是讨论存档（非交付迭代记录），survey 写作时以实践修订 | ✅ |

无违规；无需 Complexity Tracking。

## Project Structure

### Documentation (this feature)

```text
specs/058-dsh-preset-roster-demo/
├── plan.md                        # 本文件
├── research.md                    # Phase 0 决策（R1-R12）
├── discussion-2026-09-08.md       # 讨论存档（中间数据，survey 写作底稿）
├── data-model.md                  # Phase 1 实体模型
├── quickstart.md                  # Phase 1 验证指南
├── contracts/
│   ├── chat-api.md                # HTTP/gRPC 契约（Chat.CreateConversation + PresetService）
│   ├── composition-manifest.md    # 组合清单直组改造 + roster roots/env 契约
│   ├── preset-authoring-plugin.md # 插件契约（服务接口/Store seam/物化算法/模板约定）
│   ├── demo-echo-plugin.md        # demo 工具插件契约
│   └── fake-llm-system-keywords.md # fake-llm 匹配扩展契约（R6）
└── tasks.md                       # Phase 2 (/speckit.tasks 产物，已生成)
```

### Source Code (repository root)

```text
common/js/dsh-plugins/preset-authoring/          # 新：通用 authoring 基座插件
├── package.json                                  #   name: @dominion/dsh-preset-authoring
├── BUILD.bazel / tsconfig.json
└── src/
    ├── index.ts                                  #   插件四导出 + ctx.presetAuthoring 服务注册
    ├── store.ts                                  #   PresetStore seam + 内存实现
    ├── materialize.ts                            #   copy-then-patch（roster copy + js-yaml round-trip + 回滚）
    └── *.test.ts                                 #   单测（mock roster ctx，style/javascript.md Mock convention）

experimental/dsh/demo/
├── agent-plugins/demo-echo/                      # 新：demo 工具插件（workspace 包）
│   ├── package.json                              #   name: @dominion/dsh-demo-echo
│   ├── BUILD.bazel / tsconfig.json
│   └── src/index.ts (+test)                      #   demo_echo 工具 + guidance 同 apply()
├── agent/
│   ├── presets-templates/                        # 新：模板 preset 部署数据（artifact_pkg_js data）
│   │   ├── demo-standard/{agent.cordis.yml, preset.yml}
│   │   └── demo-tools/{agent.cordis.yml, preset.yml}
│   ├── cordis.yml                                # 改：spine → 直组 + agent-presets/preset-authoring 行
│   ├── package.json / BUILD.bazel                # 改：依赖增删（R12）、npm_deps、模板 data_files
│   ├── chat.proto → ../chat.proto                # 改：CreateConversation + PresetService（gateway 重生成）
│   └── src/
│       ├── session.ts                            # 改：显式会话（compose 消费 + FAILED_PRECONDITION）
│       ├── server.ts / bootstrap.ts              # 改：新 RPC handlers + env 注入
│       └── *.test.ts                             # 改/增：会话与 handler 单测
├── fake-llm/                                     # 改：system_keywords 匹配条件 + 新模板
│   └── service/testdata/*.yaml
├── gateway/                                      # 改：proto 重生成（grpc-gateway 注解路由）
└── testplan/
    ├── interface_test.yaml                       # 改：preset 场景用例（US1/US2 全验收场景）
    └── (closure_audit_test 随 package.json 重算)

pnpm-workspace.yaml                               # 改：catalog 三项 + packages 增 agent-plugins/*
```

**Structure Decision**: 双新包（通用基座进 `common/js/dsh-plugins/`、demo 工具进 demo 本地 `agent-plugins/`——D3/D7 决策）；demo 既有三服务拓扑与目录不变，proto 仍在 `experimental/dsh/demo/chat.proto`。

## Complexity Tracking

> 无 Constitution Check 违规，无需填写。
