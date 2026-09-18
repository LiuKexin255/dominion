# Implementation Plan: LLM 请求发送可靠性修复与 opencode-go 模型接入

**Branch**: `063-llm-reliability-opencode-go` | **Date**: 2026-09-12 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/063-llm-reliability-opencode-go/spec.md`

## Summary

三块工作共享一个根因主题——agent-v2 的 LLM 请求通路缺少官方适配器已有的失败处理设计：

1. **llm-glm 失败分类与重试对齐**（US1/FR-001..008）：`GLM_*` 私有错误码迁移到 dsh 共享失败码分类学（`TRANSPORT`/`RATE_LIMIT`/`SERVER`/`AUTH`/`QUOTA`/`EMPTY_RESPONSE`/`TIMEOUT` 等），使组合中已有的 `llm-retry` 插件真正生效；补齐 `Retry-After` 尊重、错误体仅分类不回显、空补全分类、adapter 级流停滞看护（默认 300s，SSE comment 作 pulse）、消费方停止的传输清理。
2. **编排器 turn 结果观察与成员保持**（US2/FR-009..012）：saolei-loop 编排器经成员 ctx 订阅 `agent/error`（失败 turn 的活跃边界事件，先于 idle）感知 turn 失败；失败时复用既有 `fail()`/`paused` 语义保持激活成员，产出含稳定失败码的结构化 error 日志，下一次 Send 重驱同一成员。
3. **opencode-go 插件与模型选择面**（US3/FR-013..018）：新 workspace 包 `@dominion/dsh-llm-opencode-go`（provider route `opencode-go`，OpenAI Chat Completions wire，官方 deepseek 适配器为设计先例），token/失败处理/看护与 llm-glm 同套义务；模型选择面升级为 `provider/model-id` 复合标识（proto 零字段变更：`Model.id`/`TeamMember.model` 承载复合标识，服务侧单一解析点 split），ListModels 变为已注册 provider 联合目录，web 下拉扁平呈现。

配套：fake-llm 增设有状态 transient 故障注入（单次 HTTP 失败/空补全后恢复）、Responses wire 停滞投影、chat wire HTTP 状态注入；testplan 部署 env 增加 `OPENCODE_LLM_TARGET` 与合成 `OPENCODE_API_KEY`（非真实 secret，覆盖条件携带/零泄漏断言），并新增 `deploy_agent_v2_stall.yaml`（SC-004a 独立 2s 看护拓扑，避免主拓扑 3s/4s 正常 chunk 间隔误触）；部署 secret 增加 `opencode-api-token`；agent_v2 侧 `package.json` workspace 依赖与 `BUILD.bazel` `runtime_deps` 增列新插件（composed 行的运行时闭包），fake-llm 新增 chat-wire `opencode_go.yaml`（SC-003 planner 开场；工具链复用既有 chat fixtures）。

## Technical Context

**Language/Version**: TypeScript（ESM workspace，Node 22 线）/ Go（fake-llm 服务与大型测试）

**Primary Dependencies**: `@deepseek-ai/dsh-llm` `0.1.1-rc.2` 精确 pin（含 `LlmError`/`RetryPolicySchema`/`isQuotaExceededError`/`isContextWindowExceededError`/`assertUsableApiKey`/`attributionHeaders`）、`@deepseek-ai/dsh-llm-retry` `0.1.1-rc.2`（已组合，无需新增）、`@deepseek-ai/dsh-timeout`（**新增** llm-glm 与新插件依赖：`idleWatchdog`/`timeoutOf`/`MAX_TIMER_DELAY_MS`）、`eventsource-parser`、`@deepseek-ai/cordis`；web 前端 React；fake-llm Go `net/http`。

**Storage**: N/A（模型目录为静态配置；会话状态沿用 dsh session/Mongo 既有设施）

**Testing**: vitest（JS 单测，随包交付）；Go 单测（fake-llm service，`bazel test //projects/game/fake-llm/...`）；Go 大型测试（`projects/game/testplan/`，经 testplan skill `guitar run` 执行，`style/large_test.md`）

**Target Platform**: Linux 服务（agent_v2 容器）+ web 浏览器

**Project Type**: web-service（dsh cordis 插件库 + gRPC/REST 服务 + web 前端）

**Performance Goals**: 重试退避有界（默认 5 次、500ms→10s、抖动 0.1，对齐 dsh 默认）；流停滞看护默认 300s（可配置）

**Constraints**: token 零泄漏（本 feature SC-003，`specs/049-agent-v2-dsh-init/spec.md` SC-004 延续）；错误呈现兼容既有 `turn_end{ERROR}` 语义；取消语义零改动（spec FR-005）

**Scale/Scope**: 2 个 LLM 插件路由共存、1 个服务、1 个 web 面板、1 个测试设施扩展

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 评估 | 结论 |
|---|---|---|
| I 引用溯源 | 全部研究/契约文档带仓库相对路径或完整 URL 引用（research.md 各决策附源码行号） | PASS |
| II 重构式变更 | 失败处理以"对齐官方适配器设计"重构 llm-glm 适配器与编排器评估锚点，非叠加补丁；模型选择面单点解析重构三处 provider 硬编码（`session.ts:62`/`orchestrator.ts:77`/插件 route） | PASS |
| III 接口优先 | 5 份契约先行：[llm-failure-taxonomy.md](contracts/llm-failure-taxonomy.md)、[opencode-go-plugin.md](contracts/opencode-go-plugin.md)、[model-selection.md](contracts/model-selection.md)、[orchestrator-turn-outcome.md](contracts/orchestrator-turn-outcome.md)、[fake-llm-fault-injection.md](contracts/fake-llm-fault-injection.md) | PASS |
| IV 测试颗粒度 | 编译+单测内嵌于各开发 task；大型测试独立验收 task（`guitar run`） | PASS |
| V 编码前阅读文档 | tasks.md 按三分类显式列出，且间接引用外链已展开（dsh 上游 URL + 本地物化精确 pin 路径、AIP、Google TS/Go 规范、`style/javascript.md` 外链（含 `specs/019` shim 契约、`specs/048` ESM 契约）、fake-llm README 引用的 `specs/046` 模板 schema 与 streaming-sequence 契约、cookbook）；Phase 6 代码规范含 Google Go 三件套 | PASS |
| VI 服务型应用大型测试验收 | quickstart.md 定义验收场景（对应 SC-001..005，其中 SC-004 分 a/b），经 testplan skill 完整部署→测试→清理闭环，全部通过为验收标准 | PASS |
| VII 终态表述 | 产物只表述终态；`GLM_*` 旧码迁移后在交付物中不残留 | PASS |
| VIII 文档图表格式 | 全部图表 Mermaid 内嵌（data-model.md 状态转移、orchestrator-turn-outcome.md 时序） | PASS |

Phase 1 后复审：设计未引入新违规（无新增包层次外的结构；`@deepseek-ai/dsh-timeout` 为官方家族既有包，属对齐官方设计所需最小依赖）。PASS。

## Project Structure

### Documentation (this feature)

```text
specs/063-llm-reliability-opencode-go/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── llm-failure-taxonomy.md
│   ├── opencode-go-plugin.md
│   ├── model-selection.md
│   ├── orchestrator-turn-outcome.md
│   └── fake-llm-fault-injection.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
common/js/dsh-plugins/
├── llm-glm/                      # 失败码迁移、retry-after、看护、空补全、传输清理
│   ├── src/{adapter,wire,serialize,index}.ts(+tests)
│   └── {BUILD.bazel,package.json}  # dsh-timeout 依赖接线（手工维护，gazelle 不覆盖 JS target）
└── llm-opencode-go/               # 新插件包 @dominion/dsh-llm-opencode-go（Chat Completions wire）
    └── src/{adapter,wire,serialize,index}.ts(+tests)
common/js/dsh-plugins/saolei-loop/
└── src/orchestrator.ts(+test)     # agent/error 订阅、drive 结果、成员保持、结构化日志
projects/game/agent_v2/
├── agent_v2.proto                 # 不变（Model.id/TeamMember.model 承载复合标识）
├── cordis.yml                     # llm-opencode-go 行 + GLM/OPENCODE 看护超时 env
├── package.json                   # dependencies += @dominion/dsh-llm-opencode-go（workspace:*）
├── BUILD.bazel                    # server_pkg runtime_deps += llm-opencode-go:runtime_pkg
├── src/{session,server,dsh}.ts(+tests)  # 复合标识解析、联合目录、OPENCODE_* bootstrap
├── service.yaml                   # secrets += opencode-api-token
└── README.md                      # env/目录终态化
projects/game/deploy.yaml          # opencode token secret 绑定
projects/game/web/frontend/src/
├── api/agent.ts                   # Model/TeamMember 类型注释更新（值域为复合标识）
├── components/TeamSettingsPanel.tsx(+test)  # 扁平复合标识下拉
└── App.tsx(+test)                 # 成员 chip 渲染复合标识
projects/game/fake-llm/
└── service/{message_types,responses,handler}.go(+tests)  # transient 注入、responses stall、chat 状态注入
└── service/testdata/*.yaml        # 注入 fixtures
projects/game/testplan/
├── deploy_agent_v2*.yaml          # OPENCODE_LLM_TARGET；stall 变体携带 2s 看护 env
└── agent_v2_*_test.go             # SC-001..005 验收用例（stall 独立 test 目标）
```

**Structure Decision**: 复用既有 dsh 插件 workspace 布局（`common/js/dsh-plugins/<plugin>/`，与 llm-glm/team/saolei-loop 同构）；新插件独立包而非并入 llm-glm（provider route 独立、依赖面独立、llm-glm README 依赖 pin 决策同样适用）。服务/前端/测试设施均在既有目录内变更，无新顶层结构。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违规。新增 `@deepseek-ai/dsh-timeout` 依赖的必要性：流停滞看护的官方实现载体（`idleWatchdog` 处理 abort 语义/`MAX_TIMER_DELAY_MS` 边界），自研等价物复杂度更高且偏离官方先例（research.md D5）。
