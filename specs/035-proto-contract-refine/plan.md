# Implementation Plan: Proto 契约修正 — 资源父级字段合规与帧方向拆分

**Branch**: `035-proto-contract-refine` | **Date**: 2026-08-03 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/035-proto-contract-refine/spec.md`

## Summary

本需求修正 `game.proto` 中两组契约合规性问题：

1. **资源冗余字段移除**（FR-001~008）：`Session.template`（OUTPUT_ONLY 父级冗余）、`Session.session_id`（OUTPUT_ONLY 资源自身 ID 冗余）、`TeamProfile.template`（REQUIRED 冗余 "double-check" 校验）违反 [AIP-124](https://google.aip.dev/124)——规范父级应由资源名称 pattern 表达，不应在资源体内冗余。移除这三个字段后，template 由 URL 路径段权威推导，session_id 由 desktop view-model 从 `ParseSessionName(name)` 派生（复用 `Team` 资源已有模式）。domain/storage 层字段保留（它们是存储身份/过滤键，不暴露到 proto）。

2. **帧方向拆分 + FrameSender 移除 + MessageRole 引入**（FR-009~020）：`TeamService.Connect(stream AgentFrame) returns (stream AgentFrame)` 拆分为 `Connect(stream UserFrame) returns (stream TeamFrame)`，消除"一个对象适配两个方向"的复杂度（frame_id/create_time 入站被忽略、operation-bridge 不设信封字段等缺陷）。`FrameSender` 枚举随之从帧中移除（方向已隐含发送方）。`Message.sender`（持久化历史消息）有独立的不可替代用途，引入专用 `MessageRole` 枚举替代，同时修复 tool 消息 SYSTEM→AGENT 的预存不一致。

> **用户明确指示**：必要的 breaking 变更不用考虑兼容问题——按 clean break 设计。

## Technical Context

**Language/Version**: Go 1.x（服务端/desktop）、TypeScript（agent 服务端、desktop 前端）

**Primary Dependencies**: gRPC + protobuf + google.api 注解（`google.api.resource`、`google.api.field_behavior`）；Wails（Go↔JS desktop 桥）；Svelte 5（前端 UI）；LangGraph（agent 会话状态/turn loop）

**Storage**: MongoDB（Session/TeamProfile 持久化，domain 字段保留）；LangGraph MemorySaver（agent 会话 checkpoint）

**Testing**: Go `testing`（单测，`bazel test`）；大型测试 `testplan/`（`guitar run <plan.yaml>`，含 WebSocket connect 流、CRUD、ListMessages 端到端验证）；Vitest（TS 单测）

**Target Platform**: Linux server（agent/gateway/proxy）+ Windows desktop（Wails app）

**Project Type**: 多组件微服务 + 桌面应用（gRPC 服务间通信 + WebSocket 客户端通信）

**Constraints**: 无需向后兼容（用户明确 clean break）

**Scale/Scope**: proto 1 个文件 + Go ~15 文件 + TS ~15 文件 + contracts 文档（仅本 feature spec 内增量更新，历史 spec 文档不动）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 状态 | 说明 |
|------|------|------|
| **I. 引用溯源** | ✅ 通过 | 设计文档引用 AIP 规范（[AIP-124](https://google.aip.dev/124)、[AIP-133](https://google.aip.dev/133)）、代码路径（`projects/game/game.proto:200`）均含完整来源指针。 |
| **II. 重构式变更** | ✅ 通过 | 本需求本质是简化过度设计的契约（冗余字段、混合帧），收缩 scope 而非堆叠。operation-bridge 信封缺陷在重构中同步修复。domain/storage 层保留不动，仅动 proto 边界——符合"简化架构"原则。 |
| **III. 接口优先设计** | ✅ 通过 | Phase 1 产出完整契约设计（data-model.md + contracts/），含 UserFrame/TeamFrame/MessageRole schema、字段语义、framing contract——先定契约再实现。 |
| **IV. 测试颗粒度** | ✅ 通过 | 单测在代码变更 phase 内执行（不单列）；大型测试（`testplan/`）作为独立验收 phase。 |
| **V. 编码前阅读文档** | ✅ 通过 | tasks.md（Phase 2 产出）将为每个 phase 声明文档清单（含 style/ 规范、AIP 外部文档）。 |
| **VI. 大型测试验收** | ✅ 通过 | 验收 phase 通过 testplan skill 执行完整 deploy→test→cleanup 闭环。 |

## Project Structure

### Documentation (this feature)

```text
specs/035-proto-contract-refine/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── resource-fields.md   # Session/TeamProfile 字段移除契约
│   └── frame-split.md       # UserFrame/TeamFrame/MessageRole 拆分契约
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                          # 契约源头（Session/TeamProfile/AgentFrame/FrameSender/Message）
├── session/                            # Session 服务端
│   ├── handler/handler.go              # sessionToProto（移除 template/session_id 赋值）
│   ├── domain/model.go                 # domain.Session（保留 Template/SessionID 存储字段）
│   └── runtime/mongo/                  # Mongo 层（保留，不动）
├── prompt/                             # TeamProfile 服务端（PromptService）
│   ├── handler/handler.go              # validateTeamProfileBody（移除 body template 校验）
│   ├── domain/model.go                 # domain.TeamProfile（保留 Template 存储字段）
│   └── runtime/mongo/                  # Mongo 层（保留，不动）
├── agent/                              # Agent 服务端（TypeScript）
│   └── src/
│       ├── handler.ts                  # Connect 服务端、ListMessages sender 推导
│       ├── turn-loop.ts                # buildFrame（出站帧构造）
│       ├── operation-bridge.ts         # dispatch（信封缺陷修复）
│       └── handler.test.ts / turn-loop.test.ts / operation-bridge.test.ts
├── gateway/cmd/main.go                 # WebSocket↔gRPC 转换（注入路由对）
├── proxy/handler/handler.go            # 首帧路由
├── pkg/bind/                           # 透明转发（AgentFrameStream 接口）
├── desktop/                            # Go desktop 客户端
│   ├── app.go                          # SendUserTurn/recvLoop/probe（帧构造/解析）
│   ├── view_model.go                   # sessionViewFromProto（ParseSessionName 派生）
│   ├── internal/api/                   # REST client + WebSocket（Connect stream）
│   ├── internal/chatstream/            # SeedFromHistory（Message→帧形状）
│   └── frontend/src/                   # Svelte 前端
│       ├── api.ts                      # 接口/枚举定义
│       ├── App.svelte                  # 帧消费/渲染
│       └── components/                 # ChatView/ChatMessage/SessionList
├── proto_test.go                       # AgentFrame roundtrip 测试
└── testplan/                           # 大型测试
    ├── system_test.yaml
    ├── helpers_test.go
    ├── session_test.go
    ├── saolei_team_test.go
    ├── saolei_fixtures_test.go
    ├── agent_checkpoint_test.go
    ├── agent_dialog_test.go
    ├── agent_multimodal_test.go
    ├── agent_operation_test.go
    └── agent_saolei_test.go
```

**Structure Decision**: 沿用现有多组件布局，本次为纯契约修正，不新增目录/模块。proto 是契约唯一源头，所有代码从 proto 生成或手工适配。

## Complexity Tracking

无 Constitution Check 违规。本需求是简化（移除冗余字段、拆分混合对象），不引入新复杂度。
