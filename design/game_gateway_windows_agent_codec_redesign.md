# Game Gateway 与 Windows Agent WebSocket 编解码重设计方案

## 背景

`projects/game/gateway` 与 `projects/game/windows_agent` 之间通过 WebSocket 交换游戏控制、媒体流、心跳和错误消息。当前两端已经统一使用 `gateway.proto` 中的 `GameWebSocketEnvelope` 作为 Go 类型，并通过 `protojson` 编解码，但实现过程中出现了多处协议语义漂移：

* `gateway.proto` 注释描述的是 `type + payload` 的 JSON envelope，但实际 `protojson` 线格式是 flattened oneof。
* gateway 与 windows_agent 分别手写了 operation kind、mouse button、control result 等转换逻辑，默认值行为不一致。
* 部分非法或未知值会被 fallback 成可执行动作，例如 gateway 将未知 control operation kind 映射成 `mouse_click`。
* `ControlResult` 的 `TIMED_OUT` 状态在 proto 到 domain 的转换中丢失。
* windows_agent 媒体 MIME 字符串存在多个定义。
* routing 语义依赖 `TargetConnID == ""` 与 sender role 的组合推断，注释与实际行为不一致。

本方案目标不是为现有错误继续补充 fallback，而是重新明确 gateway、windows_agent、web 三端共同遵守的协议边界，并让双方通过同一套 codec、validator 和常量协作。

## 目标

本方案交付后应达成以下效果：

* WebSocket 线协议有唯一、准确、可测试的定义。
* gateway、windows_agent、web 三端同步迁移到新的 control request 结构，不保留旧 `kind + mouse` 业务兼容路径。
* gateway 与 windows_agent 不再各自维护容易漂移的枚举转换和 MIME 常量。
* 非法业务消息在协议边界显式拒绝或返回明确错误，不被 fallback 成其他合法动作。
* 继续保留 `DiscardUnknown: true`，保证 protobuf 字段扩展时具备向后兼容能力。
* gateway 的业务层只处理已验证过的协议消息，不再依赖 lossy 的 proto -> domain -> proto 往返来做纯转发。

## 非目标

* 不将 WebSocket proto 拆到新的 Go/proto package。
* 不支持旧版 `GameControlRequest.kind + mouse` 作为运行时 fallback。
* 不实现自定义 `{ "type": "...", "payload": {...} }` JSON envelope。
* 不通过猜测 ffmpeg 输出 profile 来决定 MIME 字符串。
* 不在本方案中重新设计 session service、token、WebSocket 连接 URL 或 Wails UI。

## 已决策项

| 决策 | 结论 |
|---|---|
| WebSocket proto 位置 | 保留在 `projects/game/gateway/gateway.proto`，不拆到新包。 |
| Control request 结构 | 直接 breaking change，改为最终形态 `oneof action`，三端同步更新。 |
| 旧 control request 兼容 | 不保留旧 `kind + mouse` 业务兼容逻辑。 |
| MIME 来源 | 使用 windows_agent transport 常量明确指定，不根据媒体内容猜测；gateway 只透传和缓存。 |
| `DiscardUnknown` | 保留 `protojson.UnmarshalOptions{DiscardUnknown: true}`，用于新增字段向后兼容。 |
| 协议校验错误 | gateway 发送 `GameError` 后关闭 WS；windows_agent 有 `operation_id` 时回 `ControlResult FAILED`，否则发送 `GameError`。 |

## 线协议模型

继续使用 `GameWebSocketEnvelope` 的 `protojson` 表达。实际 JSON 为 protobuf JSON 的 flattened oneof 形态，字段名为 lowerCamelCase：

```json
{
  "sessionId": "session-1",
  "messageId": "msg-1",
  "controlRequest": {
    "operationId": "op-1",
    "mouseClick": {
      "button": "GAME_MOUSE_BUTTON_LEFT",
      "x": 10,
      "y": 20
    }
  }
}
```

`GameWebSocketEnvelope` 不包含独立 `type` 字段，也不包含 `payload` wrapper。活动的 oneof 字段名就是消息类型。

### Envelope 约束

所有业务消息都必须满足：

* `session_id` 非空。
* `message_id` 非空。
* `payload` oneof 必须设置且只能设置一个。
* envelope 中的 `session_id` 必须等于 URL path 中的 session ID。
* WebSocket 建立后第一条业务消息必须是 `hello`。

### 角色与方向约束

gateway 按连接角色校验消息方向：

| 发送方 | 允许发送 |
|---|---|
| windows_agent | `media_init`, `media_segment`, `control_ack`, `control_result`, `pong`, `error` |
| web | `control_request`, `ping` |
| gateway -> windows_agent | `control_request`, `ping`, `error` |
| gateway -> web | `media_init`, `media_segment`, `control_ack`, `control_result`, `pong`, `error` |

不符合方向的 payload 是协议错误。

## Proto 模型调整

### `GameControlRequest`

将当前 `kind + mouse` 的扁平结构改为 action oneof：

```proto
message GameControlRequest {
  string operation_id = 1 [(google.api.field_behavior) = REQUIRED];

  bool flash_snapshot = 2 [(google.api.field_behavior) = OPTIONAL];

  oneof action {
    GameMouseClick mouse_click = 3;
    GameMouseDoubleClick mouse_double_click = 4;
    GameMouseDrag mouse_drag = 5;
    GameMouseHover mouse_hover = 6;
    GameMouseHold mouse_hold = 7;
  }
}

message GameMouseClick {
  GameMouseButton button = 1 [(google.api.field_behavior) = REQUIRED];
  int32 x = 2 [(google.api.field_behavior) = REQUIRED];
  int32 y = 3 [(google.api.field_behavior) = REQUIRED];
}

message GameMouseDoubleClick {
  GameMouseButton button = 1 [(google.api.field_behavior) = REQUIRED];
  int32 x = 2 [(google.api.field_behavior) = REQUIRED];
  int32 y = 3 [(google.api.field_behavior) = REQUIRED];
}

message GameMouseDrag {
  GameMouseButton button = 1 [(google.api.field_behavior) = REQUIRED];
  int32 from_x = 2 [(google.api.field_behavior) = REQUIRED];
  int32 from_y = 3 [(google.api.field_behavior) = REQUIRED];
  int32 to_x = 4 [(google.api.field_behavior) = REQUIRED];
  int32 to_y = 5 [(google.api.field_behavior) = REQUIRED];
  int32 duration_ms = 6 [(google.api.field_behavior) = OPTIONAL];
}

message GameMouseHover {
  int32 x = 1 [(google.api.field_behavior) = REQUIRED];
  int32 y = 2 [(google.api.field_behavior) = REQUIRED];
}

message GameMouseHold {
  GameMouseButton button = 1 [(google.api.field_behavior) = REQUIRED];
  int32 x = 2 [(google.api.field_behavior) = REQUIRED];
  int32 y = 3 [(google.api.field_behavior) = REQUIRED];
  int32 duration_ms = 4 [(google.api.field_behavior) = REQUIRED];
}
```

`GameMouseAction`、`GameControlRequest.kind`、`GameControlRequest.mouse` 删除。所有测试和客户端同步更新，不做旧字段 runtime fallback。

### `GameOperation`

`GameOperation.kind` 可继续使用 `GameControlOperationKind`，作为 read model 中 inflight operation 的摘要字段。它不再作为 WebSocket control request 的解码依据。

### `GameControlResult`

保持 `status` enum 作为唯一状态来源：

```proto
message GameControlResult {
  string operation_id = 1 [(google.api.field_behavior) = REQUIRED];
  GameControlResultStatus status = 2 [(google.api.field_behavior) = REQUIRED];
  string error_message = 3 [(google.api.field_behavior) = OUTPUT_ONLY];
}
```

gateway 内部不再用 `Success + TimedOut` 两个 bool 表达结果状态，避免组合态丢失。

## 代码分层

### gateway package 内新增协议辅助文件

由于 WebSocket proto 不拆包，协议辅助逻辑放在 `projects/game/gateway` package 内，供 gateway 自身、windows_agent、fakeagent 和测试复用。

建议新增：

* `projects/game/gateway/ws_codec.go`
  * `EncodeWebSocketEnvelope(env *GameWebSocketEnvelope) ([]byte, error)`
  * `DecodeWebSocketEnvelope(data []byte) (*GameWebSocketEnvelope, error)`
  * 内部继续使用 `protojson.MarshalOptions{}` 和 `protojson.UnmarshalOptions{DiscardUnknown: true}`。
* `projects/game/gateway/ws_validate.go`
  * `ValidateWebSocketEnvelope(env *GameWebSocketEnvelope) error`
  * `ValidateHello(env *GameWebSocketEnvelope) error`
  * `ValidateRolePayload(role GameClientRole, env *GameWebSocketEnvelope) error`
  * `ValidateControlRequest(req *GameControlRequest) error`
* `projects/game/gateway/ws_constants.go`
  * 协议错误 code、最大 hold duration 对齐说明等 gateway 侧协议常量；不定义或判断具体媒体 codec。
* `projects/game/gateway/ws_action.go`
  * action oneof 与 internal domain/read-model 摘要之间的转换。

这样 windows_agent 可直接 import `dominion/projects/game/gateway` 复用 codec/validator，不引入 import cycle。

### gateway WebSocket handler

`projects/game/gateway/ws.go` 调整为：

1. `readEnvelope` 调用 `DecodeWebSocketEnvelope`。
2. hello 阶段调用 `ValidateHello`。
3. 普通消息阶段先调用：
   * `ValidateWebSocketEnvelope`
   * session ID match 校验
   * `ValidateRolePayload`
4. 消息交给 service 前已经是合法协议消息。
5. 删除本文件中重复的 operation/button fallback 转换。

### gateway service/domain

当前 `domain.Message` 是 `GameWebSocketEnvelope` 的镜像，且纯转发路径会发生 proto -> domain -> proto 往返。重构后建议：

* media、ack、result 等纯协议消息尽量保持 typed proto payload，不再复制成等价 domain payload 后再复制回来。
* control executor 内部只保存业务必须字段：operation id、action kind 摘要、flash_snapshot、requester conn id、create time。
* `ControlResultPayload` 改为保存 `GameControlResultStatus`，或直接使用 `*GameControlResult`。

如需保留 `domain.Message` 作为第一阶段过渡，也必须做到：

* 不再有 unknown kind -> mouse_click。
* `TIMED_OUT` 不丢失。
* 每个 proto payload 往返测试覆盖，证明 proto -> domain -> proto 不丢字段。

### windows_agent transport

`projects/game/windows_agent/internal/transport/envelope.go` 不再维护自己的 protojson options，改为委托 gateway package：

* `EncodeEnvelope` 调用 `gateway.EncodeWebSocketEnvelope`。
* `DecodeEnvelope` 调用 `gateway.DecodeWebSocketEnvelope`。

`sender.go` 保持构造 typed proto envelope，control result 使用 typed proto status，media MIME 使用 `projects/game/windows_agent/internal/transport/constants.go` 中的显式常量。

### windows_agent runtime/input

`projects/game/windows_agent/internal/runtime/control_flow.go` 不再做重复 enum fallback。处理流程为：

1. 收到 `GameControlRequest`。
2. 调用 `gateway.ValidateControlRequest`。
3. 发送 `ControlAck`。
4. 将 action oneof 转为 `input.Command`。
5. 执行 input helper。
6. 回 `ControlResult`：成功为 `SUCCEEDED`，失败为 `FAILED` 并带 `error_message`。

`projects/game/windows_agent/internal/input/command.go` 保持 helper IPC 模型，但新增从 action oneof 到 `Command` 的转换入口，避免先转成 gateway domain payload。

## 关键细节

### `DiscardUnknown: true` 的边界

保留 `DiscardUnknown: true` 的目的只是允许新字段被旧端忽略，不能用于接受旧业务结构。

因此：

* 新 `GameControlRequest` 必须设置 action oneof。
* 旧客户端只发送 `kind/mouse` 时，新生成代码中这些字段已不存在，会被 unknown discard，随后 `ValidateControlRequest` 因 action 缺失失败。
* 这符合“三端同步 breaking change”的决策。

### MIME 常量

MIME 使用 windows_agent transport 中的显式常量，不猜测媒体内容。建议：

* 在 `projects/game/windows_agent/internal/transport/constants.go` 中保留唯一生产常量，例如 `MimeTypeMP4`。
* windows_agent 发送 `media_init` 时只使用该常量。
* gateway 只透传、缓存和向 web 广播 `mime_type`，不判断具体 codec 字符串。
* fakeagent、gateway testplan、interface test 如需模拟真实 agent，应使用与生产常量相同的显式测试常量；测试不应让 gateway 依赖 windows_agent 的 internal package。
* ffmpeg 参数必须与该常量表达的 H.264 profile/level 对齐；如果未来改变 ffmpeg profile，必须同步改常量和测试。

本方案不在运行时解析 `avcC` 来推断 MIME。

### 协议错误行为

gateway：

* hello 缺失、session mismatch、非法 payload direction、非法 control action：发送 `GameError{code: "protocol_error"}` 后关闭 WS。
* service/business 错误可继续使用现有 `gateway_error` 或细化错误 code。

windows_agent：

* 收到非法 control request 且能取得 `operation_id`：返回 `ControlResult{status: FAILED, error_message: ...}`。
* 收到无法归属 operation 的非法消息：发送 `GameError{code: "protocol_error"}`。

### Routing 语义

`domain.RoutedMessage.TargetConnID == ""` 当前同时表达“发给 agent”和“广播给 web”，实际含义依赖 sender role。建议改为显式 destination：

```go
type RouteTargetKind int

const (
  RouteTargetAgent RouteTargetKind = iota + 1
  RouteTargetWebBroadcast
  RouteTargetConn
)

type RoutedMessage struct {
  TargetKind RouteTargetKind
  TargetConnID string
  Message *GameWebSocketEnvelope
}
```

这样 service 层明确说明消息目标，WebSocket handler 不再根据 sender role 猜测空 target 的含义。

## 测试计划

### 单元测试

新增或更新：

* `projects/game/gateway/ws_codec_test.go`
  * 覆盖所有 envelope 类型的 encode/decode。
  * 固定 protojson flattened oneof golden JSON。
  * 验证 unknown field 在 `DiscardUnknown: true` 下可忽略。
* `projects/game/gateway/ws_validate_test.go`
  * 缺 session/message/payload 失败。
  * role 与 payload direction 不匹配失败。
  * control request 未设置 action 失败。
  * hold duration <= 0 或 > 30s 失败。
  * unspecified button 失败。
* `projects/game/windows_agent/internal/transport/envelope_test.go`
  * 验证 agent 使用 gateway codec，wire bytes 与 gateway golden 兼容。
* `projects/game/windows_agent/internal/input/command_test.go`
  * 每种 action oneof 转换为 helper command。
  * 非法 action/button/duration 返回错误。

### 集成测试

更新：

* `projects/game/gateway/ws_test.go`
* `projects/game/gateway/testplan/fakeagent`
* `projects/game/gateway/testplan/interface_test.go`
* `projects/game/testplan/session_gateway_test.go`
* windows_agent transport/runtime tests

重点验证：

* web 发送新版 action oneof，gateway 转发给 agent。
* windows_agent 执行后回 ack/result，gateway 准确路由回 requester web conn。
* 旧 `kind/mouse` JSON 被 `DiscardUnknown` 忽略后因 action 缺失被拒绝，而不是 fallback。
* agent 使用唯一生产 MIME 常量发送 media init，gateway 只透传该值。

### Bazel 验证

实现完成后执行：

```bash
bazel run @rules_go//go -- fmt <changed-go-files>
bazel run //:gazelle projects/game/gateway projects/game/windows_agent
bazel test //projects/game/gateway/...
bazel test //projects/game/windows_agent/...
bazel test //projects/game/testplan/...
bazel build //projects/game/gateway/...
bazel build //projects/game/windows_agent/...
```

如果 proto 或 BUILD 依赖发生变化，再执行：

```bash
bazel mod tidy
```

## 迁移步骤

1. 修改 `gateway.proto`：
   * 更新 WebSocket envelope 注释为实际 protojson flattened oneof。
   * 将 `GameControlRequest` 改为 action oneof。
   * 新增 action message。
   * 删除 `GameMouseAction` 以及 request 中的 `kind/mouse` 字段。
2. 运行 gazelle 更新 BUILD。
3. 在 gateway package 新增 codec、validator、常量和 action helper。
4. 重构 gateway ws/service/control，使其消费已验证协议消息。
5. 重构 windows_agent transport 复用 gateway codec。
6. 重构 windows_agent control/input，使其直接处理 action oneof。
7. 更新 fakeagent、web 端、gateway testplan 和系统 testplan。
8. 运行单测、build 与大型测试计划。

## 验收标准

* `gateway.proto` 注释与实际 protojson 线格式一致。
* 三端都只生成/接受 action oneof 形态的 `GameControlRequest`。
* 旧 `kind/mouse` 消息不会被 fallback 成 mouse click。
* `ControlResultStatus_TIMED_OUT` 不丢失。
* media init 的 MIME 来自 windows_agent transport 的唯一生产常量，gateway 不再维护冲突常量。
* gateway 与 windows_agent 的 envelope 编解码测试使用同一 codec 并通过 golden 兼容测试。
* `bazel test //projects/game/gateway/...` 通过。
* `bazel test //projects/game/windows_agent/...` 通过。
* 相关 game testplan 通过，或明确记录非本变更导致的部署/环境问题。

## 未来规划

如果后续 WebSocket 协议被更多组件复用，可再将 WebSocket proto 和 codec 从 `projects/game/gateway` 拆到中立 package。当前根据已决策项不进行拆包。

如果未来需要 gateway 与 windows_agent 独立滚动发布，应在 `hello` 中增加 protocol version/capability 协商，而不是重新引入旧字段 fallback。
