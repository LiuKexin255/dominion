# Contract: Chat API（对外 HTTP + gRPC Chat 服务）

**Feature**: [spec.md](spec.md) FR-001/FR-002 | **Date**: 2026-08-22 | **修订**: 2026-08-22 按 AIP-136 自定义方法模式重定义资源名路由（合规 `style/api.md`）

demo 的唯一公共入口契约：调用方 → gateway（HTTP/JSON）→ agent 服务（gRPC）。版本策略：demo 无版本化承诺（experimental，破坏性变更不通知）。

## 1. HTTP API（gateway 对外）

### POST /experimental/dsh-demo/conversations/{conversation}:sendMessage

会话资源上的自定义方法（[AIP-136](https://google.aip.dev/136) resource-based custom method；HTTP/JSON 经 [AIP-127](https://google.aip.dev/127) transcoding）。

**Request**（URI 变量 + JSON body，`body: "*"`）:

- URI: `{conversation}` = 会话资源 ID（即 Conversation 实体的 id，如 `conv-1`）；
- Body:

```json
{ "message": "hello there" }
```

| 字段 | 位置 | 类型 | 必填 | 校验失败 |
|---|---|---|---|---|
| name（资源名 `conversations/{id}`） | URI 变量拼装 | string | 是（id 非空，`conversations/*` 匹配） | 400 |
| message | body | string | 是（非空） | 400 |

**Response 200**:

```json
{ "name": "conversations/conv-1", "reply": "Hello! How can I help you today?" }
```

| 字段 | 类型 | 语义 |
|---|---|---|
| name | string | 回显会话资源名 |
| reply | string | 本轮 agent 回复（确定性模板回复，[fake-llm-templates.md](fake-llm-templates.md)）；无 assistant 消息时为空串 |

**语义**: 非流式、同步请求/响应（spec FR-001）；同一会话的多次调用构成多轮会话（FR-002），reply 可能依赖历史（多轮模板条件）。

### 错误码

| HTTP | gRPC | 场景 |
|---|---|---|
| 400 | INVALID_ARGUMENT | 字段缺失/为空、资源名不匹配 `conversations/*`（畸形请求 Edge Case） |
| 500 | INTERNAL | agent 内部错误（模型调用失败等——fake-llm 不可达时本轮以错误返回，进程存活，Edge Case） |
| 503 | UNAVAILABLE | agent 服务未就绪/不可达 |

错误体携带可读 message（经 gateway 透传 gRPC status message，[AIP-193](https://google.aip.dev/193)）。

## 2. gRPC 服务契约（gateway → agent）

proto：`experimental/dsh/demo/chat.proto`（`package experimental.dsh.demo`；`option go_package = "dominion/experimental/dsh/demo"`）。

```protobuf
// Chat service serves the dsh chat agent demo.
// Prefix Path: /experimental/dsh-demo
service Chat {
  // Sends one user message to a conversation and returns the assistant reply.
  rpc SendMessage(SendMessageRequest) returns (SendMessageResponse) {
    option (google.api.http) = {
      post: "/experimental/dsh-demo/{name=conversations/*}:sendMessage"
      body: "*"
    };
  }
}
message SendMessageRequest {
  // Conversation resource name, e.g. "conversations/conv-1".
  string name = 1;
  // The user message text for this turn.
  string message = 2;
}
message SendMessageResponse {
  // Conversation resource name (echo).
  string name = 1;
  // The assistant reply for this turn.
  string reply = 2;
}
```

- 传输：agent 监听 `0.0.0.0:50051`（grpc 端口，TLS opportunistic——仓库 grpc-js 服务惯例：`/etc/tls/tls.crt|key` 在场则 TLS，否则 insecure）；gateway 经 `solver.URI("dsh-demo/agent:grpc")` 拨号。
- 字段语义与 HTTP 完全同构（gateway 纯转译，无自有逻辑）；`name` 由 URI 变量 `{name=conversations/*}` 自动填充（[AIP-127](https://google.aip.dev/127)）。
- 资源名到会话标识的映射：服务端从 `name` 提取 `conversations/` 后缀作为 Conversation id（直映射 dsh SessionId，见 [dsh-agent-service.md](dsh-agent-service.md) §3）。

## 3. 时序与超时

- 单轮 = 一次 SendMessage 往返（内部含完整 dsh 回合：followup → LLM 调用 → idle 终止，[research.md](../research.md) D3）。
- fake-llm 即时回复（无 chunk 间 delay 配置），正常回合亚秒级；demo 不设自定义超时（gateway/HTTP 默认链路超时兜底）。

## 4. 验收锚点（映射 spec 验收场景）

| 场景 | 本契约断言 |
|---|---|
| US1-1/2 | 命中模板 → reply 与模板 text 逐字一致；重复请求同 reply |
| US1-3 | 不命中任何模板 → reply 为兜底模板内容 |
| US2-1 | 同会话第二轮（history_keywords 满足）→ 多轮分支模板的 text |
| US2-2 | 新会话同消息 → 首轮分支模板的 text |
| US2-3 | 两会话交错 → 各自正确分支 |
| Edge | 空字段/坏资源名 → 400；fake-llm 不可达 → 500 且进程存活 |
