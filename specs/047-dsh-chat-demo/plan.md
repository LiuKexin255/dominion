# Implementation Plan: dsh Chat Demo — grpc-js 服务进程内嵌入 dsh

**Branch**: `047-dsh-chat-demo` | **Date**: 2026-08-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/047-dsh-chat-demo/spec.md`

## Summary

以 PoC 落地 `survey/deepseek-harness-b1-bazel-packaging.md` 的锁定决策：在 `experimental/dsh/demo` 下交付三个服务组成的最小 chat 链路——**grpc-gateway（Go，HTTP 入口）→ dsh agent（grpc-js/TS，B1 进程内 `boot()` 嵌入 dsh）→ fake-llm（Go，OpenAI chat-completions 兼容）**，外加 `third_party/dsh/core` 框架核心底座 target，并以 testplan 大型测试（`guitar run` 全流程、全部用例通过）作为验收。

研究阶段已按 spec FR-007 的证据优先门禁完成实证（详见 [research.md](research.md)）：**官方 `dsh-llm-deepseek` 适配器胜出**——`baseURL` 纯配置可指向 fake-llm（`!!js` 表达式注入运行期 Dominion 服务发现结果）、dummy API key 即可通过校验（免 credentials 插件）、text-only 请求无 Files API 依赖。因此 **fake-llm 仅实现 chat-completions wire（含 SSE 流式），零自研 dsh 插件**；chat 组合清单为**两行**（`agent-spine` + `llm-deepseek`，spine 五个裁剪键关闭工具面）。dsh 服务经 `ctx.agents.create({sessionId})`（宿主自选会话 ID）+ `agent.followup()` + `session/event` 订阅（idle 终止取末条 `assistant/message`）驱动对话。

## Technical Context

**Language/Version**: TypeScript（Node toolchain 24，swc 编译，**服务入口 CJS，与仓库 TS 包一致**；依赖的 ESM dsh 包经 require(esm) 消费，Node 22.12+ 默认启用——[research.md](research.md) D8）；Go（仓库既有 toolchain，gateway 与 fake-llm）；dsh 全家桶精确 pin `0.1.1-rc.2`（dist-tag 不可信，见 `survey/deepseek-harness-b1-plugin-packaging.md` §4.2/§4.3）

**Primary Dependencies**: 
- dsh 框架核心（`third_party/dsh/core` 闭包清单包，≈11 包：`@deepseek-ai/dsh-app-boot` + cordis 家族（cordis / cordis-plugin-loader / include / group / timer）+ `node-addon-require-builtin` + app-boot 的 4 个 dsh peers：home-paths / invariants / system-prompt / launch-environment）
- 官方插件（服务级声明）：`@deepseek-ai/dsh-agent-spine-demo`（agent spine）、`@deepseek-ai/dsh-llm-deepseek`（LLM adapter，route `deepseek-official`）
- grpc 侧：`@grpc/grpc-js` + `@grpc/proto-loader`（catalog）；Go grpc-gateway v2（仓库既有 bazel 依赖）
- 仓库公共包：`@dominion/common-js-otel` / `common-js-logs` / `common-js-grpc-otel` / `common-js-grpc-resolver` / `common-js-resolver`（Dominion 服务发现，FR-011）

**Storage**: N/A（会话为 dsh 进程内内存态，随进程销毁——spec Assumptions；不启用 persistence 插件行）

**Testing**: 服务单测（Go `go_test` / TS `vitest_test`，Constitution IV 每次变更必跑）；大型测试 = testplan skill `guitar run`（部署→用例→清理闭环，全部用例通过，Constitution VI，FR-008）

**Target Platform**: Linux 容器（distroless nodejs24-debian12 运行 TS 服务，glibc 2.36 内实测 native addon——`survey/deepseek-harness-b1-bazel-packaging.md` §7 风险 2）

**Project Type**: 多服务 demo（2× Go 服务 + 1× grpc-js 服务 + 1 个 bazel 依赖底座 + 大型测试）

**Performance Goals**: N/A（PoC；正确性与确定性优先，无吞吐/延迟目标）

**Constraints**: 产物不 bundle（per-file 编译，现状链路本就如此）；dsh 上游包为 ESM-only（服务入口 CJS 经 require(esm) 消费，Node 22.12+ 默认启用——[research.md](research.md) D8，服务根无需 package.json data 文件）；运行时零外部 LLM/网络依赖；fake-llm 寻址必须经 Dominion 服务发现（FR-011，禁止静态地址）

**Scale/Scope**: 3 服务 + 1 底座包 + 1 testplan；~40±10 个 dsh 闭包包（`survey/deepseek-harness-b1-bazel-packaging.md` §5.4.2）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 门禁 | 状态 |
|---|---|---|
| I 引用溯源 | plan/research/contracts 全部携带仓库相对路径或完整 URL | ✅ 通过 |
| II 重构式变更 | demo 全新增量；复用既有构建图（`artifact_pkg_js`/`artifact_pkg_go`/`artifact_image` 原样），`third_party/dsh/core` 按 `survey/deepseek-harness-b1-bazel-packaging.md` §5.1 既有范式（`common/js/otel` 模式）落地，无过度设计 | ✅ 通过 |
| III 接口优先设计 | Phase 1 产出四份契约：chat-api（HTTP+gRPC）、fake-llm-wire（SSE wire）、fake-llm-templates（模板 schema）、dsh-agent-service（组合/寻址/闭包契约），实现前定约 | ✅ 通过 |
| IV 测试颗粒度 | 单测随代码变更执行（不单列 task）；大型测试单独验收 | ✅ 通过 |
| V 编码前阅读文档 | tasks.md 阶段（`/speckit.tasks`）按三分类声明文档清单 | ⏳ 待 tasks 阶段 |
| VI 大型测试验收 | FR-008：testplan skill 实际执行 `guitar run`，全部用例通过；fake-llm 为测试基建随 testplan 部署（README 记录豁免先例参照） | ✅ 通过（设计含验收路径） |

**Post-Phase-1 复核（2026-08-22）**: 设计产物（research/data-model/4×contracts/quickstart）复核通过——原则 I（全部决策带仓库路径或上游 URL）、原则 III（四份契约先于实现定约）、原则 VI（quickstart §3 定义 guitar run 验收闭环）；无新增违规，Complexity Tracking 维持空。**D8 修订（2026-08-22）**：服务模块格式定为 CJS（与仓库 TS 包统一），服务根无需 package.json data 文件修补；require(esm) 依据与备选否决见 [research.md](research.md) D8 修订版（原 ESM 方案及其 `package.root.json` 设计经勘误废弃——`artifact_pkg_js` data_files 无重命名能力）。

## Project Structure

### Documentation (this feature)

```text
specs/047-dsh-chat-demo/
├── plan.md              # This file
├── research.md          # Phase 0 output — 实证决策记录（适配器门禁、驱动面、组合、寻址）
├── data-model.md        # Phase 1 output — 实体与数据形态
├── quickstart.md        # Phase 1 output — 端到端验证指南
├── contracts/           # Phase 1 output
│   ├── chat-api.md           # 对外 HTTP API + gRPC Chat 服务契约
│   ├── fake-llm-wire.md      # fake-llm chat-completions SSE wire 契约
│   ├── fake-llm-templates.md # 模板配置 schema 与匹配语义
│   └── dsh-agent-service.md  # dsh 服务组合清单/寻址/闭包契约
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
third_party/dsh/
└── core/                       # dsh 框架核心底座（闭包清单 workspace 包，零插件）
    ├── package.json            #   精确 pin ≈11 个核心包（catalog 例外，见 survey §4-2）
    ├── version.ts              #   导出底座快照标识（js_runtime_library 的 lib 载荷）
    ├── tsconfig.json
    └── BUILD.bazel             #   npm_link_all_packages + js_runtime_library(runtime_pkg)

experimental/dsh/demo/
├── chat.proto                  # 应用根 proto（Chat 服务 + google.api.http 注解，grpc_chain 惯例）
├── agent/                      # dsh grpc-js 服务（TS，CJS 入口——仓库统一，require(esm) 消费 dsh ESM 包）
│   ├── package.json            #   直接依赖：app-boot/spine/llm-deepseek/agent/llm + grpc + workspace 公共包
│   ├── tsconfig.json
│   ├── .swcrc                  #   module: commonjs（仓库统一）
│   ├── cordis.yml              #   组合清单（两行：agent-spine + llm-deepseek）
│   ├── service.yaml            #   agent 服务部署（grpc:50051, tls）
│   └── src/
│       ├── bootstrap.ts        #   otel init → bootDsh() → 动态 import server → startServer → SIGTERM/SIGINT 退出链
│       ├── dsh.ts              #   bootDsh(): resolver 解析 fake-llm → 注入 env → boot() → Context（fail-loud）
│       ├── session.ts          #   AgentSessions: get-or-create / followup / 事件收集 / dispose
│       ├── server.ts           #   grpc-js server：Chat handler（SendMessage 映射）
│       ├── dsh.test.ts         #   fail-loud 单测（boot 失败 → 诊断日志 + exit(1)）
│       ├── server.test.ts      #   INTERNAL 错误映射单测
│       └── session.test.ts     #   单测
├── gateway/                    # grpc-gateway（Go，grpc_chain/testplan/gateway 样板）
│   ├── main.go
│   ├── BUILD.bazel             #   go_library/go_binary + artifact_pkg_go + artifact_image
│   └── service.yaml            #   kind: stateless, port http:80, tls
├── fake-llm/                   # fake-llm（Go，照搬 projects/game/fake-llm 模式）
│   ├── cmd/main.go
│   ├── service/                #   handler / matcher（多轮最小扩展）/ store / testdata
│   └── BUILD.bazel             #   artifact_pkg_go + artifact_image
└── testplan/
    ├── deploy.yaml             #   三服务 + gateway ingress（PathPrefix /experimental/dsh-demo）
    ├── interface_test.yaml     #   guitar plan（US1/US2 用例）
    ├── chat_test.go            #   go_largetest（US1 用例，target: testplan_test）
    ├── multiturn_test.go       #   go_largetest（US2 用例，追加进既有 suite）
    ├── BUILD.bazel
    └── closure_audit_test.go   #   SC-004 闭包审计用例（tar 内 dsh-* 包溯源断言）

pnpm-workspace.yaml             #   packages 增 third_party/dsh/core 与 experimental/dsh/demo/agent
```

**Structure Decision**: 单应用多服务布局，沿用 `experimental/grpc_chain/` 的"应用根 proto + 每服务子目录 + testplan/ 收纳部署与用例"惯例；dsh 底座独立于 `third_party/dsh/`（锁定决策的定位：跨项目可复用，非 demo 私有）。Go 依赖经 gazelle/`bazel mod tidy` 进既有 MODULE.bazel 体系。

## Complexity Tracking

> 无 Constitution Check 违规需论证。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
