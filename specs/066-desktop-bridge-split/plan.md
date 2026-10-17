# Implementation Plan: DesktopBridgeService.Connect 协议拆分——下行服务流 + 上行单发接口

**Branch**: `066-desktop-bridge-split` | **Date**: 2026-09-18 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/066-desktop-bridge-split/spec.md`；用户补充指令："本需求为重构，原有基于 ws + bidi 框架的代码应当移除。框架应当基于新的框架设计，保持干净，不要遗留旧框架内容。"

## Summary

将 `DesktopBridgeService.Connect`（bidi stream，无 HTTP 注解、经 gateway 自研 WS↔gRPC 桥承载）拆分为两个带 HTTP 注解的 RPC：`Stream`（server-streaming，下行，`GET ...:stream`，chunked NDJSON）与 `SendFrame`（unary，上行，`POST ...:sendFrame`，`application/octet-stream` 二进制 proto body）。拆分后全链路均为 grpc-gateway v2.27.6 原生支持形态（server streaming 已有生产先例 `AgentService.Send`），gateway 的自研 WS 桥（约 230 行 + 测试）、proxy 的 bidi relay 与首帧身份重放、共享包 `bind.Binder`/`bind.WithFirstFrame`、desktop/fake-desktop 的 WS 客户端**全部删除**（用户指令：干净重构，不遗留旧框架内容）。连接事实源转移到 `:stream` 连接：打开即推 status 首帧（连接确认）、~10s 应用层心跳、新连接接管 + 晚到断开 compare-and-delete no-op；无活跃流时 `SendFrame` 拒绝 `FAILED_PRECONDITION`。行为面（游戏链路、终局统计、team 消息流、web 前端）零变化；扫雷统计口径已核验为现状满足（无代码变更）。动机与现状对照见 [spec.md](spec.md) Motivation 表，此处不重复。

## Technical Context

**Language/Version**: Go 1.x（gateway/proxy/desktop/fake-desktop/testplan）、TypeScript ESM（agent_v2 + dsh-plugins，grpc-js）

**Primary Dependencies**: grpc-gateway v2.27.6（锁死于 go.mod；server streaming `ForwardResponseStream`、内建 `runtime.ProtoMarshaller`）、grpc-go、@grpc/grpc-js + @grpc/proto-loader（agent_v2）、coder/websocket（**拆分后从桥接链路移除**）、bazel + gazelle

**Storage**: 无新增持久化；owner 亲和既有 Mongo store（proxy）原样复用

**Testing**: `bazel test`（单测：Go + vitest TS）；大型测试 testplan skill（`tools/test/guitar`，`projects/game/testplan/`，fake-desktop + fake-llm 拓扑）

**Target Platform**: Linux 服务（gateway/proxy/agent_v2/fake-desktop）+ Windows/macOS 桌面（desktop，wails）

**Project Type**: 跨服务内部接口重构（微服务链路 + 桌面客户端 + 大型测试）

**Performance Goals**: ≤5MiB 截图上行零编码膨胀（octet-stream 直传）；下行小帧 NDJSON（无截图下行）；空闲链路靠 ~10s 心跳存活

**Constraints**: gRPC hop 8MB 消息上限（既有）；keepalive 配对（teamConn `WithLongLivedClientKeepalive` ↔ proxy `WithLongLivedServerKeepalive`，既有）；gateway `http.Server` 无 WriteTimeout（既有）；ingress 纯路径前缀路由（既有，零改动）

**Scale/Scope**: 单 desktop 客户端/会话；变更面 = agent_v2.proto、gateway、proxy、agent_v2（server.ts + desktop-bridge 插件）、desktop、fake-desktop、testplan、`pkg/bind` 清理

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁 | 原则 | 评估 |
|---|---|---|
| 文档阅读门禁 | V | tasks 阶段强制：每 phase 按三分类列出必读文档（本 plan 已验证调研文档与代码锚点实际内容相符） |
| 实现门禁 | II / III | ✅ II：本 feature 即"架构过度设计收缩"——删除自研 WS 桥，符合重构式变更；用户指令明确"保持干净、不遗留旧框架内容"（对齐原则 VII 终态表述）。✅ III：接口优先——proto 契约（`Stream`/`SendFrame` + 注解 + 错误语义）在 contracts/desktop-bridge-v2.md 先行定义 |
| 编译 + 单测门禁 | IV | 每次代码变更执行 `bazel build` + `bazel test`（相关 target），不单列 task |
| 引用、图表与终态门禁 | I / VII / VIII | 所有文档带仓库相对路径/完整 URL 引用；交付物只含终态（旧协议代码与测试全删，无残留——FR-009/SC-005）；图表统一 Mermaid |
| 大型测试验收门禁 | VI | 拆分全链路切换后 MUST 实际执行 testplan（`guitar run`，部署→测试→清理闭环）且全部用例通过；见 quickstart.md |

**Post-Phase-1 复查**：设计产物（research.md / data-model.md / contracts/desktop-bridge-v2.md / quickstart.md）已生成且含全部引用与 Mermaid 图；无未决 NEEDS CLARIFICATION。✅

## Project Structure

### Documentation (this feature)

```text
specs/066-desktop-bridge-split/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── desktop-bridge-v2.md   # Phase 1 output（协议契约）
└── checklists/
    └── requirements.md  # /speckit.specify output
```

### Source Code (repository root)

```text
projects/game/
├── agent_v2.proto                      # DesktopBridgeService：Connect → Stream + SendFrame
├── game.proto                          # UserFrame/TeamFrame 注释措辞更新（WS → HTTP 流）
├── gateway/cmd/
│   ├── main.go                         # 删 WS 桥；注册 DesktopBridgeServiceHandler + octet-stream marshaler + body-limit 中间件
│   └── main_test.go                    # 删 WS 桥测试；新增 body-limit 中间件测试
├── proxy/
│   ├── handler/bridge.go               # Connect bidi relay → Stream（ServerStreamBinder）+ SendFrame（unary 转发）
│   └── cmd/main.go                     # 装配不变（RegisterDesktopBridgeServiceServer）
├── pkg/bind/
│   ├── binder.go + binder_test.go      # 删除（bidi 泵无生产使用点）
│   ├── first_frame.go + first_frame_test.go  # 删除（首帧重放机制消失）
│   └── server_stream.go + server_stream_test.go  # 保留（Send 在用）
├── agent_v2/src/server.ts              # buildDesktopBridgeHandlers：Stream/SendFrame 适配
├── desktop/
│   ├── internal/api/
│   │   ├── websocket.go                # 删除（WSClient）→ 新增 bridgeclient.go（HTTP 流读取 + unary POST）
│   │   └── client.go                   # 既有 REST 客户端（模式参考，env header 透传同源）
│   └── app.go                          # Connect 探针发送移除（首帧即服务端 status 帧）；sendOperationResult 有界重试
└── fake-desktop/service/session.go     # WS dial/probe/write/read → HTTP 流 + POST；drain pause 机制删除
common/js/dsh-plugins/desktop-bridge/src/
├── wire.ts                             # BidiStream → 可写流面 + 入站 RPC 面；删 deriveSessionName 依赖面
├── bridge.ts                           # attach（首帧推送 + 心跳）+ sendFrame（FAILED_PRECONDITION）+ 删 respondProbe（Phase 6 T015）
└── index.ts                            # 导出面同步
projects/game/testplan/
├── desktop_flow_test.go                # WS 客户端测试 → HTTP 客户端重写（首帧/接管/派发回执/断连/空闲存活/超限归因）
├── agent_v2_helpers_test.go            # 共享 flow 客户端 helper（connect/probe/receipt/read/script）→ HTTP 流 + unary POST 重写
├── agent_v2_game_test.go               # 经共享 helper 的用例断言迁移（含 probe-echo 断言 → 流存活/心跳语义）
├── agent_v2_conversation_test.go       # 经共享 helper 的用例断言迁移（调用点同步）
├── agent_v2_preset_test.go             # 经共享 helper 的用例断言迁移（调用点同步）
├── agent_v2_game_disconnect_test.go    # 行为断言不变（经 fake-desktop）；注释中 probe 措辞更新
└── helpers_test.go                     # 通用 HTTP helper（如有 WS 依赖同步改）
```

**Structure Decision**: 不新增目录/包；变更为既有文件的替换与删除。`pkg/bind` 收缩为单文件包（server_stream.go）。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违规项。本 feature 是净收缩：删除自研 WS 桥（~230 行）、bidi 泵 + 首帧重放（bind 两文件 + 测试）、WSClient（165 行）、fake-desktop WS 逻辑；新增面为两个带注解 RPC、两段转发（复用既有范式）、一个 HTTP 桥客户端与插件接口调整。
