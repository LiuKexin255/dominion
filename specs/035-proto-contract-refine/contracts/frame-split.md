# Contract: 帧方向拆分 — UserFrame / TeamFrame / MessageRole

**Spec**: [spec.md](../spec.md) FR-009~020 | **Data Model**: [data-model.md](../data-model.md) §1.3~1.8 | **Research**: [research.md](../research.md) R2~R5

---

## 1. RPC 签名变更

```proto
service TeamService {
  // Connect establishes a bidirectional streaming channel for team
  // communication. The client sends UserFrame (user input, operation
  // results, connectivity probes); the server sends TeamFrame (agent
  // display content, control signals, operation requests). No REST binding.
  rpc Connect(stream UserFrame) returns (stream TeamFrame);
}
```

原 `rpc Connect(stream AgentFrame) returns (stream AgentFrame);` 删除。

---

## 2. UserFrame（入参）

### 2.1 消息定义

```proto
message UserFrame {
  string session_id = 1 [(google.api.field_behavior) = REQUIRED];
  string template_id = 2 [(google.api.field_behavior) = REQUIRED];
  string agent = 3;

  oneof payload {
    MessageParts message_parts = 10;
    FlowParts flow_parts = 11;
  }
}
```

### 2.2 字段语义

| field | 必要性 | 语义 |
|-------|--------|------|
| `session_id` | REQUIRED | 会话路由标识。gateway 从 connect URL 路径段注入（覆盖客户端值，`gateway/cmd/main.go:180-181`）。proxy 首帧读取做路由（`proxy/handler/handler.go:167-170`）。 |
| `template_id` | REQUIRED | 模板路由标识。gateway 从 connect URL 路径段注入。proxy 首帧读取做路由。 |
| `agent` | optional | 用户输入的目标 team agent（如 "player"/"planner"）。仅 message_parts（用户消息）场景需要；flow_parts（操作结果/探活）可省略。agent `resolveUserInputAgent(frame.agent)` 用于路由（`handler.ts:420`）。 |
| `message_parts` | oneof | 用户消息内容（TextPart 文本 + ImagePart 图片）。 |
| `flow_parts` | oneof | 控制通道：FlowResultPart（操作执行结果回报）、StatusSignal（连接探活）。 |

### 2.3 不含的字段（原 AgentFrame 有，现已排除）

| 排除字段 | 原因 |
|----------|------|
| `frame_id` | 服务端完全不消费入站 frame_id（`handler.ts` 的 `stream.on("data")` 从不读取）。 |
| `create_time` | 服务端完全不消费入站 create_time。 |
| `sender` | 入站天然来自用户——FrameSender 枚举移除（FR-019），方向已隐含发送方。 |

---

## 3. TeamFrame（出参）

### 3.1 消息定义

```proto
message TeamFrame {
  string session_id = 1 [(google.api.field_behavior) = REQUIRED];
  string template_id = 2 [(google.api.field_behavior) = REQUIRED];
  string frame_id = 3;
  google.protobuf.Timestamp create_time = 4;
  string agent = 5;
  MessageRole role = 6;

  oneof payload {
    MessageParts message_parts = 10;
    FlowParts flow_parts = 11;
  }
}
```

### 3.2 字段语义

| field | 必要性 | 语义 |
|-------|--------|------|
| `session_id` | REQUIRED | 会话路由标识（回传）。所有 TeamFrame 发送点必设。 |
| `template_id` | REQUIRED | 模板路由标识（回传）。所有 TeamFrame 发送点必设。 |
| `frame_id` | optional | 帧唯一标识（UUID）。前端用于去重（`App.svelte:685-686` `renderedMessageIds`）。所有 TeamFrame 发送点必设。 |
| `create_time` | optional | 帧时间戳。前端用于气泡时间显示（`App.svelte:726`）。所有 TeamFrame 发送点必设。 |
| `agent` | optional | 产出帧的 team agent。前端用于分 tab 路由（`App.svelte:552`）。message_parts 帧必设（产出 agent）；flow_parts 控制信号帧可省略（无特定 agent 来源）。 |
| `role` | optional | 消息角色。实时 message_parts 帧→AGENT；SeedFromHistory 重放帧→从 Message.role 拷贝；flowParts 帧→UNSPECIFIED。前端用于气泡对齐（user 右/agent 左）。 |
| `message_parts` | oneof | agent 显示内容（TextPart/ThinkingPart/ImagePart/ToolCallPart/ToolResultPart）。恒为 AGENT 产出。 |
| `flow_parts` | oneof | 控制信号（WaitSignal/WarnSignal/StatusSignal/QueueSignal）+ 操作请求（MouseMovePart/MouseClickPart/KeyboardPressPart/MouseMoveAndClickPart/FlowResultPart）。 |

### 3.3 信封完整性契约（FR-013）

**所有 TeamFrame 发送点 MUST 设置 `session_id`、`template_id`、`frame_id`、`create_time`。**

修复的缺陷：`operation-bridge.ts:239-242` 当前只设 `{payload: "flowParts", flowParts: {...}}`，缺所有信封字段。拆分后统一通过 `buildTeamFrame` 函数构造，从结构上消除不一致。

发送点清单：
| 发送点 | 位置 | payload 类型 |
|--------|------|-------------|
| display frames | `turn-loop.ts:414-466` | message_parts（text/thinking/tool_call/tool_result） |
| wait frame | `turn-loop.ts:469-474` | flow_parts（WaitSignal） |
| queue signal | `turn-loop.ts:485-489` | flow_parts（QueueSignal） |
| warn frame | `turn-loop.ts:492-498` | flow_parts（WarnSignal） |
| status probe reply | `handler.ts:371-390` | flow_parts（StatusSignal） |
| user-input-rejected warn+wait | `handler.ts:427-454` | flow_parts（WarnSignal + WaitSignal） |
| operation dispatch | `operation-bridge.ts:239-242` | flow_parts（mouse/keyboard operation） |

---

## 4. MessageRole 枚举（替代 FrameSender）

### 4.1 定义

```proto
enum MessageRole {
  MESSAGE_ROLE_UNSPECIFIED = 0;
  MESSAGE_ROLE_USER = 1;
  MESSAGE_ROLE_AGENT = 2;
}
```

### 4.2 与原 FrameSender 的映射

| 原 FrameSender | 新 MessageRole | 备注 |
|----------------|----------------|------|
| `FRAME_SENDER_USER` | `MESSAGE_ROLE_USER` | 用户消息（LangChain human） |
| `FRAME_SENDER_AGENT` | `MESSAGE_ROLE_AGENT` | agent 消息（LangChain ai） |
| `FRAME_SENDER_SYSTEM` | `MESSAGE_ROLE_AGENT` | **统一**：tool 消息从 SYSTEM 改为 AGENT（与 live 流 `turn-loop.ts:462` tool_result 帧对齐）。原 SYSTEM 的控制信号语义已由 TeamFrame.flowParts payload 类型隐含。 |
| `FRAME_SENDER_UNSPECIFIED` | `MESSAGE_ROLE_UNSPECIFIED` | 默认值 |

### 4.3 FrameSender 枚举移除

`FrameSender` 枚举（`game.proto:286-292`）完全删除。所有引用替换：
- `AgentFrame.sender` → 不存在（帧拆分后无 sender 字段）。
- `Message.sender` → `Message.role`（类型 MessageRole）。

---

## 5. Message 资源变更

```proto
message Message {
  option (google.api.resource) = { ... };  // pattern 不变

  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  string message_id = 2 [(google.api.field_behavior) = OUTPUT_ONLY];
  MessageRole role = 3;  // 原 FrameSender sender = 3
  string agent = 4;
  google.protobuf.Timestamp create_time = 5;
  MessageParts content = 6;
}
```

`sender` 字段（field 3）类型从 `FrameSender` 改为 `MessageRole`，字段名建议改为 `role`（更语义化，且避免与帧中已删除的 sender 混淆）。field number 保持 3 不变。

---

## 6. 转发层适配

### 6.1 bind 接口拆分

原 `pkg/bind/binder.go:18-24` 的 `AgentFrameStream` 接口拆分为：

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
```

`Bind` 函数签名适配：左端 Recv(UserFrame) → 右端 Send(UserFrame)；右端 Recv(TeamFrame) → 左端 Send(TeamFrame)。类型安全，不会混方向。

### 6.2 gateway 适配

`gateway/cmd/main.go:169-193`：
- `Recv()` 构造 `*game.UserFrame`（替代 `*game.AgentFrame`），注入 `session_id`/`template_id`。
- `Send()` 序列化 `*game.TeamFrame`（替代 `*game.AgentFrame`）。

### 6.3 proxy 适配

`proxy/handler/handler.go:162-170`：首帧读取 `*game.UserFrame` 的 `template_id`/`session_id` 做路由。`first_frame.go` 的预读帧类型改为 `*game.UserFrame`。

---

## 7. desktop 客户端 + 前端适配

### 7.1 Go desktop

| 位置 | 变更 |
|------|------|
| `app.go:561-571`（SendUserTurn） | 构造 `*game.UserFrame`（替代 AgentFrame），不设 frame_id/create_time/sender |
| `app.go:730-743`（operation result） | 构造 `*game.UserFrame` |
| `app.go:1643-1653`（probe） | 构造 `*game.UserFrame` |
| `app.go:617-691`（recvLoop） | 解析 `*game.TeamFrame`（替代 AgentFrame） |
| `app.go:629-638`（错误合成 wait） | 合成 `*game.TeamFrame` |
| `internal/api/websocket.go` | SendFrame/RecvFrame 帧类型适配 |
| `internal/chatstream/stream.go:197-208`（SeedFromHistory） | 重放帧从 AgentFrame 改为 TeamFrame；`role` 从 `msg.GetRole()` 读取 |
| `view_model.go:52-59, 192-208` | MessageViewModel.Sender → Role（`m.GetRole().String()`） |

### 7.2 前端 api.ts

| 位置 | 变更 |
|------|------|
| `api.ts:65-70` | `FrameSender` 枚举替换为 `MessageRole`（USER=1, AGENT=2） |
| `api.ts:363-371` | `AgentFrame` 接口替换为 `UserFrame`（出站）+ `TeamFrame`（入站） |
| `api.ts:378-385` | `Message` 接口 `sender` 字段改为 `role: MessageRole` |

### 7.3 前端 App.svelte

| 位置 | 变更 |
|------|------|
| `App.svelte:520-533` | `senderFromString`/`resolveSender` 改为 `resolveRole`，值域 MessageRole |
| `App.svelte:688` | `resolveSender(frame.sender)` → `frame.role ?? MessageRole.AGENT`（实时帧恒 AGENT） |
| `App.svelte:477` | `resolveSender(m.sender)` → `m.role`（历史消息） |
| `App.svelte:856` | 乐观用户消息 `sender: FrameSender.USER` → `role: MessageRole.USER` |
| `App.svelte:759` | warn 帧 `sender: FrameSender.SYSTEM` → 删除（TeamFrame 无 sender） |

### 7.4 前端 ChatView.svelte / ChatMessage.svelte

| 位置 | 变更 |
|------|------|
| `ChatView.svelte:102-108` | `msg.sender === FrameSender.AGENT/USER` → `msg.role === MessageRole.AGENT/USER` |
| `ChatView.svelte:178-180` | `isAgentSender(sender)` → `isAgentRole(role)` |
| `ChatView.svelte:252, 263` | `item.sender` → `item.role` |
| `ChatMessage.svelte:34` | `sender === FrameSender.USER` → `role === MessageRole.USER` |
| 所有 `sender` prop | 重命名为 `role`，类型 `MessageRole` |
