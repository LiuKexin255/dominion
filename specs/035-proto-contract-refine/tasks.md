# Tasks: Proto 契约修正 — 资源父级字段合规与帧方向拆分

**Input**: Design documents from `/specs/035-proto-contract-refine/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: 单测在代码变更任务中执行（不单列）；大型测试单独分配验收 task。

**Organization**: 任务按 user story 组织。两个 US 均为 P1，US1（资源字段移除）作为 MVP 先行，US2（帧拆分）随后。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2)
- Include exact file paths in descriptions

---

## Phase 1: User Story 1 — 资源冗余字段移除 (Priority: P1) 🎯 MVP

**Goal**: 移除 `Session.template`、`Session.session_id`、`TeamProfile.template` 三个可从资源名称派生的冗余 proto 字段；domain/storage 层保留不动。

**Independent Test**: `bazel build //projects/game/...` + `bazel test //projects/game/session/... //projects/game/prompt/... //projects/game/desktop:...` 通过；Session/TeamProfile CRUD 行为不变。

### 文档清单

- **代码规范文档**: `style/golang.md`、`style/api.md`（及其引用的 [AIP-124 Resource association](https://google.aip.dev/124)、[AIP-133 Standard methods: Create](https://google.aip.dev/133)、[AIP-122 Resource names](https://google.aip.dev/122)）
- **官方文档**: 无
- **技术文章**: 无

### Implementation

- [X] T001 [US1] 修改 `projects/game/game.proto`：删除 `Session` message 的 `template`（field 2）和 `session_id`（field 3）字段，添加 `reserved 2, 3; reserved "template", "session_id";`。参考 `specs/035-proto-contract-refine/data-model.md` §1.1、`specs/035-proto-contract-refine/contracts/resource-fields.md` §1.1。
- [X] T002 [US1] 修改 `projects/game/game.proto`：删除 `TeamProfile` message 的 `template`（field 2）字段，添加 `reserved 2; reserved "template";`。参考 `specs/035-proto-contract-refine/data-model.md` §1.2、`specs/035-proto-contract-refine/contracts/resource-fields.md` §1.2。
- [X] T003 [US1] 运行 `bazel run //:gazelle projects/game` 更新 `projects/game/BUILD.bazel`（proto 变更后 gazelle 同步）。
- [X] T004 [P] [US1] 修改 `projects/game/session/handler/handler.go`：在 `sessionToProto`（约行 158-174）中删除 `Template:` 和 `SessionId:` 字段赋值（保留 `Name:` 和 `CreateTime:`）；`CreateSession` 中的 template 推导逻辑（从 parent 解析）和 domain 落库逻辑不变。同步适配 `projects/game/session/handler/handler_test.go`：移除所有 `GetTemplate()`/`GetSessionId()` 断言（约行 144-152, 190-197, 274-275, 536-540）与 `wantTemplate` 表字段（约行 69）。修改后运行 `bazel test //projects/game/session/...` 验证通过。参考 `specs/035-proto-contract-refine/data-model.md` §4.1。
- [X] T005 [P] [US1] 修改 `projects/game/prompt/handler/handler.go`：(1) `validateTeamProfileBody`（约行 205-220）删除读取 `tp.GetTemplate()` 的 body template 校验逻辑（原行 209-215），oneof spec 一致性校验（`validateSpecConsistency`）的数据源参数改为 `parent`（TemplateName）；(2) `UpdateTeamProfile`（约行 157-162）删除 body template 一致性校验；(3) `teamProfileToProto`（约行 238-261）删除 `Template:` 赋值。同步适配 `projects/game/prompt/handler/handler_test.go`：移除 `GetTemplate()` 断言（约行 118-119, 146-147, 800-801）；将 template mismatch with parent 测试用例改写为 oneof 不一致测试（约行 187-194）；移除 body `Template:` 构造（约行 200, 467, 480, 502, 546, 555）。修改后运行 `bazel test //projects/game/prompt/...` 验证通过。参考 `specs/035-proto-contract-refine/data-model.md` §4.3~4.4、`specs/035-proto-contract-refine/contracts/resource-fields.md` §2.3~2.4。
- [X] T006 [P] [US1] 修改 `projects/game/desktop/view_model.go`：(1) `sessionViewFromProto`（约行 121-131）将 `SessionID` 改为从 `game.ParseSessionName(s.GetName()).SessionID` 派生，删除 `Template:` 赋值；(2) `SessionView` struct（约行 15-20）删除 `Template` 字段；(3) `TeamProfileView` struct（约行 81-89）删除 `Template` 字段；(4) `teamProfileViewFromProto`（约行 244-265）删除 `Template:` 赋值。同步适配 `projects/game/desktop/view_model_test.go`：移除 `Template` 断言（约行 38-39, 295-296）；`sessionId` 断言改为验证从 name 解析的值（约行 51-55）。参考 `specs/035-proto-contract-refine/data-model.md` §4.2、`specs/035-proto-contract-refine/contracts/resource-fields.md` §3.1。
- [X] T007 [P] [US1] 修改 `projects/game/desktop/app.go`：(1) `CreateTeamProfile`（约行 1283-1287）删除 `Template: game.TemplateName{TemplateID: template}.String()` 赋值；(2) `UpdateTeamProfile`（约行 1430-1434）删除 `Template:` 赋值。同步适配 `projects/game/desktop/app_test.go`（移除 `view.Template` 断言约行 2010-2014, 2392-2393；移除请求体 `profile.GetTemplate()` 断言约行 2270-2271；移除 mock 响应中的 `"template"` JSON 字段约行 1991, 2370）与 `projects/game/desktop/internal/api/client_test.go`（移除 Session 响应/断言中的 template 约行 33, 97-98, 127, 140-141, 169, 184, 254-255, 290, 334-335, 754, 767-768；移除 CreateTeamProfile 请求断言 `GetTemplate()` 约行 841-853, 860, 891-892；移除 UpdateTeamProfile patch 中的 `Template` 约行 1133-1151）。修改后运行 `bazel test //projects/game/desktop:...` 验证通过。参考 `specs/035-proto-contract-refine/contracts/resource-fields.md` §3.2。
- [X] T008 [P] [US1] 修改 `projects/game/desktop/frontend/src/api.ts`：(1) `Session` interface（约行 39-44）删除 `template` 字段；(2) `TeamProfile` interface（约行 325-340）删除 `template` 字段。参考 `specs/035-proto-contract-refine/contracts/resource-fields.md` §3.3。

> 测试适配与单测验证已并入各实现任务（T004~T008 内同步适配对应测试文件并运行 `bazel test`），符合宪法原则 IV（编译+单测作为代码变更任务的一部分，不单独分配 task）。

**Checkpoint**: US1 完成。Session/TeamProfile 资源不再包含可从名称派生的冗余字段；CRUD 行为不变。验证：`bazel build //projects/game/... && bazel test //projects/game/session/... //projects/game/prompt/... //projects/game/desktop:...` 编译通过、单测全绿。

---

## Phase 2: User Story 2 — 帧方向拆分 + FrameSender 移除 + MessageRole 引入 (Priority: P1)

**Goal**: 将 `Connect(stream AgentFrame) returns (stream AgentFrame)` 拆分为 `Connect(stream UserFrame) returns (stream TeamFrame)`；移除 `FrameSender` 枚举；为 `Message` 引入 `MessageRole` 枚举；修复 operation-bridge 信封缺陷。

**Independent Test**: `bazel build //projects/game/...` + `bazel test //projects/game/...`（含 agent TS 单测）通过；端到端实时通信全链路行为不变。

### 文档清单

- **代码规范文档**: `style/golang.md`、`style/javascript.md`、`style/api.md`（及其引用的 [AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)、[AIP-126 Enumerations](https://google.aip.dev/126)）
- **官方文档**: 无
- **技术文章**: 无

### Implementation — Proto 契约

- [X] T015 [US2] 修改 `projects/game/game.proto`：(1) 新增 `MessageRole` 枚举（`MESSAGE_ROLE_UNSPECIFIED=0; MESSAGE_ROLE_USER=1; MESSAGE_ROLE_AGENT=2;`）；(2) 新增 `UserFrame` message（session_id/template_id/agent + payload oneof）；(3) 新增 `TeamFrame` message（session_id/template_id/frame_id/create_time/agent/role + payload oneof）；(4) `Message` message 的 `FrameSender sender = 3` 改为 `MessageRole role = 3`；(5) `Connect` RPC 签名改为 `rpc Connect(stream UserFrame) returns (stream TeamFrame);`；(6) 删除 `AgentFrame` message 和 `FrameSender` 枚举。同步适配 `projects/game/proto_test.go`：将所有 `AgentFrame` roundtrip 测试改为 `UserFrame`/`TeamFrame` roundtrip；移除 `FrameSender` 引用（约行 34, 75-76, 580）。参考 `specs/035-proto-contract-refine/data-model.md` §1.3~1.8、`specs/035-proto-contract-refine/contracts/frame-split.md` §1~5。
- [X] T016 [US2] 运行 `bazel run //:gazelle projects/game` 更新 `projects/game/BUILD.bazel`。

### Implementation — Agent 服务端（TypeScript）

- [X] T017 [US2] 修改 `projects/game/agent/src/handler.ts`：(1) 移除本地 `FrameSender` 常量（约行 67-78）；(2) `Connect` handler 的入参类型从 `AgentFrame` 改为 `UserFrame`，`stream.on("data")` 中读取 `frame.sessionId`/`frame.templateId`/`frame.payload`/`frame.agent`（约行 328-506），移除 `frame.sender === USER` 门控（约行 410，入站天然为用户）；(3) `buildFrame` 改为 `buildTeamFrame`，构造 `TeamFrame`（含 role 字段），移除 sender 参数；(4) status probe reply（约行 371-390）和 user-input-rejected warn+wait（约行 427-454）改用 `buildTeamFrame`；(5) `ListMessages` sender 推导（约行 584-589）改为 role：human→`MESSAGE_ROLE_USER`，ai+tool→`MESSAGE_ROLE_AGENT`（统一 tool 为 AGENT）。同步适配 `projects/game/agent/src/handler.test.ts`：移除 `FRAME_SENDER_*` 常量（约行 33-35）；`userContentFrame` 构造改为 `UserFrame`（约行 202-218）；移除 `sender` 字段断言；出站帧断言改为 `TeamFrame`；移除非-user 帧忽略测试（入站天然为用户，约行 551-557）。修改后运行 agent TS 单测验证通过。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §3.3、§4.2。
- [X] T018 [US2] 修改 `projects/game/agent/src/turn-loop.ts`：(1) 移除本地 `FrameSender` 常量（约行 67-78）；(2) `buildFrame` 改为 `buildTeamFrame`，构造 `TeamFrame`（含 role=AGENT for messageParts，UNSPECIFIED for flowParts），移除 sender 参数；(3) 所有 display frame（约行 414-466）使用 `buildTeamFrame`；(4) waitFrame（约行 469-474）、queueSignalFrame（约行 485-489）、warnFrame（约行 492-498）使用 `buildTeamFrame`；(5) `TurnLoopEmit` 类型从 `(frame: AgentFrame)` 改为 `(frame: TeamFrame)`。同步适配 `projects/game/agent/src/turn-loop.test.ts`：emit frames 类型从 `AgentFrame` 改为 `TeamFrame`；信封完整性断言 `f.sessionId/f.templateId` 保留（约行 234）。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §3.3。
- [X] T019 [US2] 修改 `projects/game/agent/src/operation-bridge.ts`：(1) `OperationSink` 类型从 `(frame: AgentFrame)` 改为 `(frame: TeamFrame)`；(2) dispatch（约行 239-242）中的帧构造改为通过 `buildTeamFrame` 函数（传入 sessionId/templateId），设置完整的信封字段（session_id/template_id/frame_id/create_time），修复原只设 payload 的缺陷。同步适配 `projects/game/agent/src/operation-bridge.test.ts`：dispatch 写入的帧断言从 `AgentFrame` 改为 `TeamFrame`；新增信封完整性断言（session_id/template_id/frame_id/create_time 非空），验证原缺陷已修复（约行 323-345）。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §3.3（信封完整性契约 FR-013）。

### Implementation — Gateway（Go）

- [X] T020 [P] [US2] 修改 `projects/game/gateway/cmd/main.go`：(1) `wsStream.Recv()`（约行 169-183）构造 `*game.UserFrame`（替代 `*game.AgentFrame`），注入 `frame.TemplateId`/`frame.SessionId`；(2) `wsStream.Send()`（约行 187-193）序列化 `*game.TeamFrame`（替代 `*game.AgentFrame`）；(3) `handleWebSocketConnect` 中 `teamClient.Connect` 返回的 stream 类型适配。同步适配 `projects/game/gateway/cmd/main_test.go`：帧构造/解析从 `AgentFrame` 改为 `UserFrame`/`TeamFrame`；移除 `FrameSender` 引用（约行 744, 879）。修改后运行 `bazel test //projects/game/gateway/...` 验证通过。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §6.2。

### Implementation — Proxy + Bind（Go）

- [X] T021 [P] [US2] 修改 `projects/game/proxy/handler/handler.go`：(1) `Connect` handler（约行 159-206）首帧读取 `*game.UserFrame` 的 `template_id`/`session_id` 做路由（替代 AgentFrame）；(2) `bind.WithFirstFrame` 传入的帧类型适配。同步适配 `projects/game/proxy/handler/handler_test.go`：mock stream 类型从 `AgentFrameStream` 改为 `UserFrameStream`/`TeamFrameStream`；`makeProxyStream` 首帧类型改为 `*game.UserFrame`（约行 887-902）。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §6.3。
- [X] T022 [P] [US2] 修改 `projects/game/pkg/bind/binder.go`：(1) 将 `AgentFrameStream` 接口拆分为 `UserFrameStream`（`Recv() (*game.UserFrame, error)`）和 `TeamFrameStream`（`Send(*game.TeamFrame) error`）；(2) `Bind` 函数签名适配：左端 Recv(UserFrame)→右端 Send(UserFrame)，右端 Recv(TeamFrame)→左端 Send(TeamFrame)。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §6.1、`specs/035-proto-contract-refine/research.md` R4。
- [X] T023 [P] [US2] 修改 `projects/game/pkg/bind/first_frame.go`：`WithFirstFrame` 的预读帧类型从 `*game.AgentFrame` 改为 `*game.UserFrame`。

### Implementation — Desktop 客户端（Go）

- [X] T024 [P] [US2] 修改 `projects/game/desktop/app.go`：(1) `SendUserTurn`（约行 561-571）构造 `*game.UserFrame`（不设 frame_id/create_time/sender）；(2) operation result（约行 730-743）构造 `*game.UserFrame`；(3) Connect probe（约行 1643-1653）构造 `*game.UserFrame`；(4) `recvLoop`（约行 617-691）解析 `*game.TeamFrame`；(5) 错误合成 wait 帧（约行 629-638）合成 `*game.TeamFrame`；(6) 移除所有 `FrameSender` 引用。同步适配 `projects/game/desktop/app_test.go`：帧构造从 `AgentFrame` 改为 `UserFrame`，帧解析改为 `TeamFrame`；移除 `FrameSender` 引用；`views[0].Sender` 断言改为 `Role`（约行 259-260）。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §7.1。
- [X] T025 [P] [US2] 修改 `projects/game/desktop/internal/api/websocket.go`：`SendFrame` 参数类型改为 `*game.UserFrame`，`RecvFrame` 返回 `*game.TeamFrame`（替代 `*game.AgentFrame`）。同步适配 `projects/game/desktop/internal/api/websocket_test.go`（帧类型从 `AgentFrame` 改为 `UserFrame`/`TeamFrame`；移除 `FrameSender` 引用约行 282, 543）与 `projects/game/desktop/internal/api/client_test.go`（ListMessages 响应断言 `GetSender()` → `GetRole()`，`FrameSender_USER` → `MessageRole_USER` 约行 660-672）。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §7.1。
- [X] T026 [P] [US2] 修改 `projects/game/desktop/internal/chatstream/stream.go`：`SeedFromHistory`（约行 197-208）重放帧从 `*game.AgentFrame` 改为 `*game.TeamFrame`，role 从 `msg.GetRole()` 读取（替代 `msg.GetSender()`）；`ChatEvent.Frame` 类型改为 `*game.TeamFrame`；`Append` 参数类型适配。同步适配 `projects/game/desktop/internal/chatstream/stream_test.go`：`testFrame`/`testMessages` 帧类型改为 `TeamFrame`；`TestSeedFromHistory` 中 msg sender→role、帧 sender→role 断言（约行 37, 56, 782-819）。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §7.1、`specs/035-proto-contract-refine/research.md` R3（SeedFromHistory 适配）。
- [X] T027 [P] [US2] 修改 `projects/game/desktop/internal/chatstream/chunk.go`：`SerializeFrame` 参数类型改为 `*game.TeamFrame`。同步适配 `projects/game/desktop/internal/chatstream/chunk_test.go`（`SerializeFrame` 测试帧类型改为 `*game.TeamFrame`；移除 `FrameSender` 引用约行 65）与 `server_test.go`（`bigTextFrame` 类型改为 `*game.TeamFrame`；移除 `FrameSender` 引用约行 163）。
- [X] T028 [P] [US2] 修改 `projects/game/desktop/view_model.go`：(1) `MessageViewModel` struct（约行 52-59）`Sender` 字段改为 `Role string \`json:"role"\``；(2) `ToMessageViewModels`（约行 192-208）`Sender: m.GetSender().String()` 改为 `Role: m.GetRole().String()`。同步适配 `projects/game/desktop/view_model_test.go`：`MessageViewModel.Sender` → `Role`；`FrameSender_USER/AGENT` → `MessageRole_USER/AGENT`（约行 465, 474, 546, 650, 657）。

### Implementation — 前端（TypeScript / Svelte）

- [X] T029 [P] [US2] 修改 `projects/game/desktop/frontend/src/api.ts`：(1) `FrameSender` 枚举（约行 65-70）替换为 `MessageRole`（`USER=1, AGENT=2`）；(2) `AgentFrame` interface（约行 363-371）拆分为 `UserFrame`（出站：sessionId/templateId/agent/messageParts/flowParts）和 `TeamFrame`（入站：sessionId/templateId/frameId/createTime/agent/role/messageParts/flowParts）；(3) `Message` interface（约行 378-385）`sender` 字段改为 `role: MessageRole`。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §7.2。
- [X] T030 [US2] 修改 `projects/game/desktop/frontend/src/App.svelte`：(1) `resolveSender`/`senderFromString`（约行 520-533）改为 `resolveRole`，值域 `MessageRole`；(2) `handleMessageParts`（约行 688）`resolveSender(frame.sender)` 改为 `frame.role ?? MessageRole.AGENT`（实时帧恒 AGENT）；(3) `loadAgentHistories`（约行 477）`resolveSender(m.sender)` 改为 `m.role`；(4) 乐观用户消息（约行 856）`sender: FrameSender.USER` 改为 `role: MessageRole.USER`；(5) warn 帧（约行 759）删除 `sender: FrameSender.SYSTEM`（TeamFrame 无 sender）；(6) `ChatEntry.sender` 改为 `role: MessageRole`。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §7.3。
- [X] T031 [P] [US2] 修改 `projects/game/desktop/frontend/src/components/ChatView.svelte`：(1) `msg.sender === FrameSender.AGENT/USER`（约行 102-108）改为 `msg.role === MessageRole.AGENT/USER`；(2) `isAgentSender(sender)`（约行 178-180）改为 `isAgentRole(role)`；(3) `item.sender`（约行 252, 263）改为 `item.role`；(4) 所有 `sender` prop 重命名为 `role`，类型 `MessageRole`。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §7.4。
- [X] T032 [P] [US2] 修改 `projects/game/desktop/frontend/src/components/ChatMessage.svelte`：(1) `sender === FrameSender.USER`（约行 34）改为 `role === MessageRole.USER`；(2) `sender` prop 重命名为 `role`，类型 `MessageRole`。参考 `specs/035-proto-contract-refine/contracts/frame-split.md` §7.4。
- [X] T033 [P] [US2] 修改 `projects/game/desktop/frontend/src/chat-stream.ts`：`AgentFrameJson` 类型别名（约行 7-13）改为 `TeamFrameJson`，类型从 `AgentFrame` 改为 `TeamFrame`。

> 测试适配与单测验证已并入各实现任务（T015~T028 内同步适配对应测试文件并运行单测），符合宪法原则 IV（编译+单测作为代码变更任务的一部分，不单独分配 task）。

**Checkpoint**: US2 完成。Connect 流使用方向独立的 UserFrame/TeamFrame；FrameSender 移除；Message 使用 MessageRole；operation-bridge 信封缺陷修复。验证：`bazel build //projects/game/... && bazel test //projects/game/...`（含 Go + TS 单测）编译通过、单测全绿。

---

## Phase 3: 大型测试验收 + Polish

**Purpose**: 大型测试适配与验收；跨 story 收尾。

### 文档清单

- **代码规范文档**: `style/golang.md`、`style/large_test.md`
- **官方文档**: 无
- **技术文章**: 无

### 大型测试适配

- [X] T048 修改 `projects/game/testplan/helpers_test.go`：(1) `buildTextFrame`/`buildUserTurnFrame`/`buildOperationResultFrame`/`buildFlowResultFrame` 构造器改为 `*game.UserFrame`（约行 665-820）；(2) `readWSFrame`/`readWSFrameNoFatal` 解析 `*game.TeamFrame`（约行 642-659）；(3) `collectTextContents` 中 `f.GetSender() != FrameSender_AGENT` 过滤改为 `f.GetRole() == MessageRole_AGENT`（约行 1033-1045）；(4) `senderString` 诊断辅助改为 `roleString`（约行 1209-1222）；(5) `createTeamProfile` helper 移除 body `Template` 字段（约行 291）；(6) `sessionResponse` struct 移除 sessionId（约行 129-133）；(7) `drainWSFrame`（约行 1197）谓词签名 `func(*game.AgentFrame) bool` → `func(*game.TeamFrame) bool`，返回类型同步改为 `*game.TeamFrame`（被 agent_multimodal/operation/saolei 三个套件共用）。
- [X] T048a [P] 修改 `projects/game/testplan/agent_multimodal_test.go`：`drainWSFrame` 谓词 `*game.AgentFrame` → `*game.TeamFrame`（约行 43, 54, 84, 138, 149）；`buildTextFrame` 调用适配。
- [X] T048b [P] 修改 `projects/game/testplan/agent_operation_test.go`：`drainWSFrame` 谓词 `*game.AgentFrame` → `*game.TeamFrame`（约行 173, 253, 263）；`buildTextFrame` 调用适配。
- [X] T048c [P] 修改 `projects/game/testplan/agent_saolei_test.go`：`drainWSFrame` 谓词与 `readWSFrame` 返回值 `*game.AgentFrame` → `*game.TeamFrame`（约行 172, 315, 421, 543, 690, 850, 873）；`buildTextFrame` 调用适配。
- [X] T049 [P] 修改 `projects/game/testplan/session_test.go`：移除 sessionId/template 断言；断言改为验证 `name` 格式正确（约行 24-39）。
- [X] T050 [P] 修改 `projects/game/testplan/saolei_team_test.go`：(1) 移除 TeamProfile 创建时的 body `template` 提交（约行 519-520）；(2) `TestTeamProfileTemplateConsistency` 改为 oneof spec 不一致测试（约行 594-608）；(3) connect 流帧类型适配（UserFrame/TeamFrame）；(4) `GetSender()` 断言改为 `GetRole()`（约行 139, 348, 728）。
- [X] T051 [P] 修改 `projects/game/testplan/agent_checkpoint_test.go`：`msg.GetSender()` 断言改为 `msg.GetRole()`（约行 83, 89）；`buildTextFrame` 调用适配（约行 104, 190）；`textResp.GetSender()` 改为 `GetRole()`（约行 119-121）。
- [X] T052 [P] 修改 `projects/game/testplan/agent_dialog_test.go`：`buildTextFrame` 调用适配（约行 64, 114, 152）；`GetSender() != FrameSender_AGENT` 断言改为 `GetRole() == MessageRole_AGENT`（约行 76, 91, 316, 529）。

### 大型测试验收

- [X] T053 加载 `testplan` skill，阅读 `style/large_test.md`，通过 testplan skill 执行 `projects/game/testplan/system_test.yaml`（`guitar run projects/game/testplan/system_test.yaml`），完成部署→测试→清理闭环。验收标准：所有测试用例全部通过。参考 `specs/035-proto-contract-refine/quickstart.md` §2.2/§3.2。

### Polish

- [X] T054 更新 `projects/game/game.proto` 中涉及 `AgentFrame`/`FrameSender`/`Session.template`/`Session.session_id`/`TeamProfile.template` 的注释，使其反映新的 UserFrame/TeamFrame/MessageRole 契约。参考 `specs/035-proto-contract-refine/spec.md` Edge Cases。

**Checkpoint**: 大型测试全绿；proto 注释与契约一致。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (US1)**: 无外部依赖，可立即开始。proto 变更先行（T001~T003），下游适配随后。
- **Phase 2 (US2)**: 依赖 Phase 1 完成（同一 proto 文件，避免合并冲突）。proto 变更先行（T015~T016），下游适配随后。
- **Phase 3**: 依赖 Phase 1 + Phase 2 全部完成。

### Within Each User Story

- Proto 变更 → gazelle → 服务端 handler → desktop client → 前端（各任务内同步适配对应测试文件，并运行单测验证）
- 单测在代码变更 task 内执行（`bazel build` + `bazel test`），不单列（宪法原则 IV）

### Parallel Opportunities

- Phase 1: T004~T008 可并行（不同文件，互不依赖；各任务内含对应测试适配）
- Phase 2: T020~T028 可并行（gateway/proxy/bind/desktop 各组件独立）
- Phase 2: T029~T033 可并行（前端各组件独立，T030 依赖 T029 的 api.ts 类型定义）
- Phase 3: T048a~T048c 可并行（不同测试文件）
- Phase 3: T049~T052 可并行（不同测试文件）

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. 完成 Phase 1 (US1)：资源字段移除
2. **STOP and VALIDATE**：`bazel test` 单测通过
3. 继续 Phase 2 (US2)：帧拆分

### Incremental Delivery

1. Phase 1 → US1 独立可测（资源 CRUD 行为不变）
2. Phase 2 → US2 独立可测（实时通信行为不变）
3. Phase 3 → 大型测试全链路验收

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- 每个 phase 编译+单测通过后再进入下一 phase
- proto 变更后务必运行 gazelle 更新 BUILD.bazel
- 大型测试验收 MUST 通过 testplan skill 实际执行（`guitar run`），禁止仅以 `bazel build` 替代
