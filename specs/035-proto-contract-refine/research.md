# Phase 0: Research — Proto 契约修正

**Date**: 2026-08-03

本文件记录 `/speckit.plan` Phase 0 的调研结论。所有决策基于 spec.md（[链接](spec.md)）中已确认的需求与用户 clarify 决策。

---

## R1: Session.template / Session.session_id / TeamProfile.template 移除方案

### Decision

移除 `game.proto` 中三个 proto 字段，保留 domain/storage 层字段不动：

| 字段 | proto 定义 | 移除方式 | domain 影响 |
|------|-----------|----------|-------------|
| `Session.template` (field 2) | OUTPUT_ONLY, resource_reference→Template | 删除 proto 字段 | 无（domain.Session.Template 保留，是存储过滤键） |
| `Session.session_id` (field 3) | OUTPUT_ONLY | 删除 proto 字段 | 无（domain.Session.SessionID 保留，是存储主键/分页键） |
| `TeamProfile.template` (field 2) | REQUIRED, resource_reference→Template | 删除 proto 字段 + 删除 body 校验逻辑 | 无（domain.TeamProfile.Template 保留，是存储过滤键） |

### Rationale

- **proto vs domain 分离**：proto 是对外契约（WHAT），domain/storage 是内部存储模型（HOW）。AIP-124 约束的是 proto 资源消息，不约束内部存储。`session/handler/handler.go:158-174` 的 `sessionToProto` 是 proto 边界的唯一转换点——只需在此停止设置这三个字段，domain 层完全不动。
- **session_id 派生**：`Session.name` pattern 是 `templates/{template}/sessions/{session}`，`ParseSessionName(name).SessionID` 直接得到 session_id。desktop `view_model.go:149-173` 的 `teamViewFromProto` 已证明此模式可行（`ParseTeamName(t.GetName()).SessionID`）。`sessionViewFromProto` 复用同一模式后，Wails JSON 的 `sessionId` 字段形状不变，前端零改动。
- **TeamProfile.template 校验逻辑**：`validateTeamProfileBody`（`prompt/handler/handler.go:205-220`）目前读 `tp.GetTemplate()` 做 double-check。移除后：CreateTeamProfile 的 template 完全由 `req.GetParent()`（URL 路径段）推导；oneof spec 一致性校验的数据源改为 parent 路径段。UpdateTeamProfile（行 157-162）的 body template 校验删除。
- **clean break**：用户明确"必要的 breaking 变更不用考虑兼容问题"。proto 字段删除不需要 `reserved` 编号（这是对外契约修正，不是内部演进），但为 hygiene 可 `reserved` 字段编号。

### Alternatives considered

1. **保留 Session.session_id**：区分 template（父级冗余，违反 AIP-124）与 session_id（资源自身 ID，AIP 未明确禁止）。→ 用户在 clarify 中选择"纳入范围，一并移除"，遵循同一"资源体不携带可从名称派生的字段"原则。

---

## R2: UserFrame / TeamFrame 字段设计

### Decision

将 `AgentFrame` 拆分为两个方向独立的消息：

#### UserFrame（入参：桌面客户端→服务端）

```proto
message UserFrame {
  string session_id = 1 [(google.api.field_behavior) = REQUIRED];
  string template_id = 2 [(google.api.field_behavior) = REQUIRED];
  string agent = 3;  // 目标 team agent（player/planner），用于用户输入路由

  oneof payload {
    MessageParts message_parts = 10;  // 用户消息内容（文本/图片）
    FlowParts flow_parts = 11;        // 操作结果回报、连接探活（status）
  }
}
```

**不含的字段**（原 AgentFrame 有但服务端忽略）：
- ~~`frame_id`~~：服务端完全不消费入站 frame_id。
- ~~`create_time`~~：服务端完全不消费入站 create_time。
- ~~`sender`~~：入站天然来自用户（FrameSender 移除，FR-019）。

#### TeamFrame（出参：服务端→桌面客户端）

```proto
message TeamFrame {
  string session_id = 1 [(google.api.field_behavior) = REQUIRED];
  string template_id = 2 [(google.api.field_behavior) = REQUIRED];
  string frame_id = 3;  // 帧去重标识（前端消费）
  google.protobuf.Timestamp create_time = 4;  // 时间戳（前端消费）
  string agent = 5;  // 来源 team agent（前端分 tab 路由）

  oneof payload {
    MessageParts message_parts = 10;  // agent 显示内容（恒为 AGENT 产出）
    FlowParts flow_parts = 11;        // 控制信号（wait/warn/status/queue）+ 操作请求（mouse/keyboard）
  }
}
```

**不含的字段**：
- ~~`sender`~~：TeamFrame 的 messageParts 恒为 agent、flowParts 恒为系统控制/操作——方向 + payload 类型已隐含发送方（FR-019）。

### Rationale

基于 spec 中的 AgentFrame 双向字段使用矩阵（spec.md User Story 2）：

- **入站忽略项**：`handler.ts:328-506` 的 `stream.on("data")` 从不读入站帧的 `frame_id`/`create_time`。`sender===USER` 门控（`handler.ts:410`）在方向拆分后变为天然隐含——入站帧天然来自用户，不再需要枚举区分。
- **出站必填项**：前端 `App.svelte:685-686`（frameId 去重）、`App.svelte:726`（createTime 时间戳）、`App.svelte:552`（agent 分 tab 路由）都需要这些字段。
- **operation-bridge 信封修复**：`operation-bridge.ts:239-242` 当前只设 `{payload: "flowParts", flowParts: {...}}`，缺所有信封字段。拆分后 TeamFrame 的构造必须由统一的 `buildTeamFrame` 函数完成，bridge dispatch 也走此函数，从结构上消除缺陷。
- **field number 空间**：UserFrame 和 TeamFrame 是独立消息，各有自己的 field number 空间。原 AgentFrame 的 `reserved` 编号（4/5 invoke_id/sequence、10/20/21/22 旧 payload）不需跨消息保留。

### Alternatives considered

1. **保留 AgentFrame 作为 TeamFrame 的父类型（用嵌套/组合）**：增加间接层，违反"降低复杂度"的目标。→ 直接定义两个独立 message。

2. **UserFrame 不带 agent 字段**（用户输入总是给 player）：虽然 saolei 模板下用户输入主要给 player，但 spec FR-032 要求"user-input frames are routed to the team agent that accepts user input"——`handler.ts:420` 的 `resolveUserInputAgent(frame.agent)` 已支持多 agent 路由。保留 agent 字段使契约可扩展。

3. **UserFrame 不带 session_id/template_id（由 gateway 注入即可）**：gateway（`main.go:180-181`）确实会覆盖这两个字段。但 proxy（`handler.go:167-170`）从首帧读取它们做路由——若 UserFrame 无这两个字段，proxy 首帧路由将无字段可读。因此保留。

---

## R3: FrameSender 移除 + MessageRole 引入

### Decision

1. **完全移除** `FrameSender` 枚举（`game.proto:286-292`）。
2. **为 Message 引入专用 `MessageRole` 枚举**：

```proto
enum MessageRole {
  MESSAGE_ROLE_UNSPECIFIED = 0;
  MESSAGE_ROLE_USER = 1;    // human 消息
  MESSAGE_ROLE_AGENT = 2;   // ai + tool 消息（统一为 AGENT）
}
```

3. **Message.sender 字段重命名+改类型**：`FrameSender sender = 3` → `MessageRole role = 3`（或保留字段名 `sender` 但类型改为 MessageRole——用户未指定命名，plan 阶段建议用 `role` 更语义化）。

### Rationale

- **FrameSender 仅被两处使用**：`AgentFrame.sender`（实时帧，拆分后移除）和 `Message.sender`（历史消息，保留但改类型）。
- **Message.sender 不可替代**：agent `handler.ts:584-589` 在 ListMessages 时从 LangChain 消息类型推导 sender（human→USER, ai→AGENT, tool→SYSTEM），客户端无法自行推导。前端用它做气泡对齐（user 右对齐、agent 左对齐）和 pending 标记。
- **tool 消息角色统一**：现有 history 中 tool 消息为 `FRAME_SENDER_SYSTEM`（`handler.ts:589`），但 live 流 tool_result 帧为 `FRAME_SENDER_AGENT`（`turn-loop.ts:462`）——不一致。MessageRole 只保留 USER/AGENT 两值，tool 消息归为 AGENT，与 live 行为对齐。
- **前端 tool 消息渲染不依赖 sender**：调查确认 `ChatView.svelte` 的 tool-bubble 渲染不按 sender 区分，因此 SYSTEM→AGENT 统一不影响渲染。
- **SYSTEM 角色**：原 FrameSender.SYSTEM 在 MessageRole 中不需要——history 的 system 消息在 `handler.ts:577-579` 已被 skip（不返回），live 流控制信号已由 TeamFrame 的 payload 类型（flowParts）隐含。

### 关于 SeedFromHistory 的适配

`desktop/internal/chatstream/stream.go:197-208` 的 `SeedFromHistory` 将历史 Message 转成帧形状重放给前端（SSE）。当前代码直拷 `msg.GetSender()` 到帧的 Sender 字段。帧拆分后：

- 重放帧类型变为 TeamFrame（出站方向）。
- TeamFrame 无 sender 字段，但有 MessageRole 信息需要传递给前端做气泡对齐。
- **方案**：SeedFromHistory 重放的 TeamFrame 中，user 消息和 agent 消息都通过 messageParts 承载。前端需要区分用户气泡 vs agent 气泡——但 TeamFrame 没有 role/sender 字段。
- **解决**：SeedFromHistory 重放时，在 TeamFrame 中嵌入 role 信息。最自然的方式：为 TeamFrame 增加一个 `MessageRole role` 字段（仅 messageParts payload 有意义，flowParts 时为 UNSPECIFIED）。但这引入了与"方向隐含发送方"的轻微张力。

> **Plan 决策**：TeamFrame 增加 `MessageRole role = 6` 字段。理由：history 重放场景需要区分 user/agent 消息气泡，而实时 TeamFrame 的 messageParts 恒为 agent（role=AGENT），flowParts 的 role 为 UNSPECIFIED。这比"前端记住历史 user 消息 id 后自行判断"更简洁。real-time 路径（`handleMessageParts`）中前端当前依赖 `resolveSender(frame.sender)`，拆分后改为读 `frame.role`——实时帧恒为 AGENT，history seed 帧携带原始 role。

### Alternatives considered

1. **为 Message 引入 string 类型的 sender（而非枚举）**：弱类型，前端需自行解析字符串。→ 枚举更安全。

2. **不统一 tool 角色（保留 SYSTEM）**：留下预存不一致。→ 统一为 AGENT，与 live 对齐。

3. **TeamFrame 不加 role 字段，前端靠本地 echo 记住 user 消息**：重连后历史加载时前端无法区分哪些是 user 消息（`loadAgentHistories` 从服务端拉取）。→ role 字段是必要的。

---

## R4: 转发层（gateway/proxy/bind）适配

### Decision

gateway、proxy、bind 层适配方向独立的帧类型，保持透明转发语义：

- **gateway**（`main.go:169-193`）：`Recv()` 构造 `*game.UserFrame`（替代 `*game.AgentFrame`），注入 `session_id`/`template_id`；`Send()` 序列化 `*game.TeamFrame`。
- **proxy**（`handler.go:162-170`）：首帧读取 `UserFrame` 的 `template_id`/`session_id` 做路由。
- **bind**（`binder.go:18-24`）：`AgentFrameStream` 接口拆分为 `UserFrameStream`（`Recv() (*game.UserFrame, error)`）和 `TeamFrameStream`（`Send(*game.TeamFrame) error`），或使用泛型/两个独立接口。`Bind` 函数签名适配。
- **first_frame.go**：`WithFirstFrame` 的预读帧类型从 `*game.AgentFrame` 改为 `*game.UserFrame`。

### Rationale

gRPC bidi stream 的入参和出参类型已由 proto 定义确定。Go gRPC 生成的 `TeamService_ConnectServer` 接口有 `Recv() (*game.UserFrame, error)` 和 `Send(*game.TeamFrame) error`——类型天然不同。bind 层当前用单一 `AgentFrameStream` 接口（因为入参出参都是 AgentFrame），拆分后需两个接口。

### bind 层接口设计

```go
// 入站方向（客户端→服务端）
type UserFrameStream interface {
    Recv() (*game.UserFrame, error)
    Context() context.Context
}

// 出站方向（服务端→客户端）
type TeamFrameStream interface {
    Send(*game.TeamFrame) error
    Context() context.Context
}

// Bind 双向转发：左端 Recv(UserFrame) → 右端 Send(UserFrame)
//                     右端 Recv(TeamFrame) → 左端 Send(TeamFrame)
```

`Bind` 的四个 goroutine 中，左→右传 UserFrame，右→左传 TeamFrame——类型安全，不会混方向。

---

## R5: desktop 客户端 + 前端适配

### Decision

- **Go desktop**（`app.go`, `view_model.go`, `internal/api/`, `internal/chatstream/`）：所有构造 `*game.AgentFrame` 的地方改为 `*game.UserFrame`（出站），所有解析入站帧的地方改为 `*game.TeamFrame`。
- **SeedFromHistory**（`chatstream/stream.go:197-208`）：重放帧从 `AgentFrame` 改为 `TeamFrame`，role 从 `msg.GetRole()` 读取（替代 `msg.GetSender()`）。
- **前端 api.ts**：`AgentFrame` 接口拆分为 `UserFrame`/`TeamFrame`；`FrameSender` 枚举替换为 `MessageRole`（USER/AGENT）。
- **前端 App.svelte**：`resolveSender`/`senderFromString` 改为读 `frame.role`（TeamFrame）或 `message.role`（Message），值域从 `FrameSender.*` 变为 `MessageRole.*`。气泡对齐逻辑（user 右、agent 左）不变。
- **前端 ChatView.svelte / ChatMessage.svelte**：`sender` prop 类型从 `FrameSender` 改为 `MessageRole`。

### Rationale

- **前端渲染语义不变**：messageParts 恒为 agent 气泡（实时），history seed 帧的 role 决定 user/agent 气泡。flowParts 不渲染为对话条目（现有行为不变）。
- **乐观用户消息**（`App.svelte:856` `sender: FrameSender.USER`）：当前前端发送用户消息时本地插入一条乐观气泡。拆分后乐观气泡的 role 仍为 `MESSAGE_ROLE_USER`。

---

## R6: 测试适配清单

### Decision

所有测试从 AgentFrame/FrameSender 适配为 UserFrame/TeamFrame/MessageRole：

**Go 单测**：
- `proto_test.go`：AgentFrame roundtrip 测试改为 UserFrame/TeamFrame roundtrip。
- `proxy/handler/handler_test.go`：mock stream 类型适配。
- `gateway/cmd/main_test.go`：帧构造/解析适配。
- `desktop/view_model_test.go`：MessageViewModel.Sender → Role。
- `desktop/app_test.go`、`desktop/internal/api/websocket_test.go`、`desktop/internal/chatstream/*_test.go`：帧类型适配。
- `session/handler/handler_test.go`：移除 template/session_id 断言。
- `prompt/handler/handler_test.go`：移除 TeamProfile.template body 校验测试。

**TS 单测**：
- `handler.test.ts`、`turn-loop.test.ts`、`operation-bridge.test.ts`：帧类型适配 + 信封完整性断言。

**大型测试**（`testplan/`）：
- `helpers_test.go`：`buildTextFrame`/`buildUserTurnFrame`/`buildOperationResultFrame` 等构造器改为 UserFrame；`readWSFrame` 解析 TeamFrame。
- `session_test.go`：移除 sessionId/template 断言，断言改为从 name 解析。
- `saolei_team_test.go`：移除 TeamProfile.template body 提交；connect 流帧类型适配。
- `agent_checkpoint_test.go`、`agent_dialog_test.go`：sender 断言改为 role。
