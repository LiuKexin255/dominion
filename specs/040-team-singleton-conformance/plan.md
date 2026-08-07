# Implementation Plan: Team 单例 AIP-156 一致化

**Branch**: `040-team-singleton-conformance` | **Date**: 2026-08-07 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from [`specs/040-team-singleton-conformance/spec.md`](spec.md)

## Summary

将 Team 资源由"带非标准 CreateTeam 的伪单例"重构为符合 [AIP-156](https://google.aip.dev/156) 的真单例：移除 `CreateTeam`，改经 `UpdateTeam(allow_missing=true)` 物化（[AIP-134 create-or-update](https://google.aip.dev/134#create-or-update) 的 upsert 语义）。`profile` 落到 `Team` 资源体并支持变更——变更触发 team graph 重建（复用既有 MemorySaver checkpointer 保留对话/游戏状态，turn in-flight 时拒绝）。这同时消除 `specs/031-team-template-mode/contracts/api-contract.md` 中对 AIP-133 的偏离（"不同 profile → ALREADY_EXISTS"）。跨服务父子（Session 属 SessionService，Team 属 TeamService）下，单例由自身服务在首次 Update 时物化，父服务不参与创建（实践范例：[Access Approval Settings](https://github.com/googleapis/googleapis/blob/master/google/cloud/accessapproval/v1/accessapproval.proto)，Get+Update 无 Create）。

## Technical Context

**Language/Version**: Go（proxy `projects/game/proxy`、desktop `projects/game/desktop`）；TypeScript（agent `projects/game/agent/src`）；Protocol Buffers v3（契约 `projects/game/game.proto`）。

**Primary Dependencies**: gRPC + grpc-gateway + `protoc-gen-go-aip` v0.1.3（Go 资源名 codegen，[BUILD.bazel:58-63](../../../BUILD.bazel) `go_gen_aip`）；`@grpc/grpc-js` + `@grpc/proto-loader`（agent 运行时服务定义）；`@langchain/langgraph`（agent team graph，含 `MemorySaver` checkpointer）；Wails（desktop 桌面壳）；MongoDB（proxy owner store）。

**Storage**: MongoDB（proxy owner store `projects/game/proxy/runtime/mongo/owner_store.go`——本特性**不变**，get-or-create + 竞态重读语义保留）；进程内 `MemorySaver`（agent team graph checkpointer——profile 变更重建时**复用**，按 `thread_id=sessionId` 保留 playerMessages/plannerMessages/gameEnded/gameCounter）。

**Testing**: bazel（Go `go test` + TS vitest，编译+单测随每次变更，宪法原则 IV）；testplan/guitar（大型测试，宪法原则 VI，`projects/game/testplan/`）。

**Target Platform**: Linux（gRPC 微服务 gateway/proxy/agent/prompt）；Windows（Wails desktop）。

**Project Type**: web-service（gRPC 微服务）+ desktop-app。

**Performance Goals**: 无新增性能目标；保留既有不变量——物化/重建**不阻塞**等 LLM（关联 `specs/039-planner-memory-calibration/research.md` §R2：initInstruction 异步；本特性仅物化 graph 骨架，不等 LLM）。

**Constraints**: gRPC unary RPC deadline（物化须在 RPC 内完成，graph 编译耗时可控）；profile 变更重建须在 turn in-flight 时拒绝（FAILED_PRECONDITION，复用 `RefreshTeam` 的 `isRunning()` 守卫 `handler.ts:231-238`）。

**Scale/Scope**: 单一契约变更，跨 proto + proxy + agent + desktop + testplan + specs 文档；无新服务、无新依赖。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md`（v1.3.0）六条原则评估：

| 原则 | 评估 | 状态 |
|---|---|---|
| **I. 引用溯源** | 所有契约/代码变更须引用 AIP-156/134、Access Approval 范例、supersede 链（031 FR-033）。proto 注释与 spec 已含链接；tasks 须沿用。 | PASS（须在 tasks 强制） |
| **II. 重构式变更** | 本特性即重构——移除 CreateTeam、把 profile 从 request 挪到资源体、store.create→upsert+rebuild。非打补丁：消除 AIP-156 违规与 AIP-133 偏离的根因。 | PASS |
| **III. 接口优先设计** | proto 契约（`contracts/api-contract.md`）在实现前先定：UpdateTeam 的 HTTP/gRPC 绑定、请求/响应 schema、错误码。Phase 1 产出契约后 Phase 2 才实现。 | PASS |
| **IV. 测试颗粒度** | 编译+单测随各服务变更（proxy/agent/desktop，不单列 task）；大型测试（testplan）作为验收 task（tasks.md Phase 4，T018）。 | PASS |
| **V. 编码前阅读文档** | tasks 每 phase 须声明文档清单（三分类）。本 plan 已列关键文档；tasks 阶段细化。 | PASS（须在 tasks 落实） |
| **VI. 服务型应用大型测试** | UpdateTeam 端到端 + profile 变更重建 + 多标签并发幂等 须经 testplan guitar 实跑全通过。 | PASS（tasks.md Phase 4，T018 验收） |

无违规 → Complexity Tracking 留空。

## Project Structure

### Documentation (this feature)

```text
specs/040-team-singleton-conformance/
├── plan.md                       # 本文件
├── spec.md                       # /speckit.specify 产出
├── research.md                   # Phase 0 产出（调研决策）
├── data-model.md                 # Phase 1 产出（资源/消息/状态）
├── quickstart.md                 # Phase 1 产出（验收脚本）
├── contracts/
│   ├── api-contract.md           # Phase 1 产出（TeamService RPC 契约）
│   └── team-rebuild-contract.md  # Phase 1 产出（graph 重建契约）
├── checklists/
│   └── requirements.md           # /speckit.specify 产出
└── tasks.md                      # /speckit.tasks 产出（Phase 2，本命令不创建）
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                         # 契约：移除 CreateTeam/CreateTeamRequest，新增 UpdateTeam/UpdateTeamRequest，Team 加 profile 字段
├── proxy/                             # Go — TeamHandler（assignOwner 不变，入口 RPC 改名）
│   ├── handler/handler.go             # CreateTeam→UpdateTeam
│   ├── runtime/agentclient/client.go  # 接口 CreateTeam→UpdateTeam
│   └── handler/handler_test.go        # TestCreateTeam→TestUpdateTeam
├── agent/src/                         # TS — handler + store（含重建路径）
│   ├── handler.ts                     # CreateTeam→UpdateTeam（删 ALREADY_EXISTS）
│   ├── session-team.ts                # store.create→upsert + rebuild
│   ├── server.ts                      # factory 抽出 rebuild 子路径（复用 checkpointer）
│   ├── team/graph.ts                  # buildTeamGraph 支持注入既有 checkpointer
│   ├── handler.test.ts
│   └── session-team.test.ts
├── desktop/                           # Go + Svelte — client/app/frontend
│   ├── internal/api/client.go         # CreateTeam(POST)→UpdateTeam(PATCH+allow_missing)
│   ├── app.go                         # App.CreateTeam→App.UpdateTeam
│   ├── frontend/src/api.ts            # createTeam→updateTeam(allowMissing)
│   ├── frontend/src/App.svelte        # GetTeam→dialog→create 两步合并为 update(allow_missing)
│   ├── app_test.go
│   └── internal/api/client_test.go
└── testplan/                          # 大型测试
    ├── helpers_test.go                # CreateTeamRequest→UpdateTeamRequest
    └── saolei_team_test.go            # 200/409 断言改为 update 幂等/重建
```

**Structure Decision**: 复用既有四服务拓扑（gateway/proxy/agent/prompt/desktop），无新增服务/目录。改动落在既有 proto 与各服务既有文件，符合原则 II（重构而非新增层）。agent 侧 graph 重建复用 `projects/game/agent/src/team/graph.ts` 既有 `buildTeamGraph`（增可选 checkpointer 注入），不新增模块。

## Complexity Tracking

> 无 Constitution Check 违规，本节留空。
