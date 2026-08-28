# Contract: /api/v2 对话 API（ConversationService）

**Feature**: [spec.md](spec.md) FR-013/FR-004/FR-012/FR-014/FR-015 | **决策**: [research.md](../research.md) D4/D6

## 1. 接口定义（proto：`projects/game/agent_v2/agent_v2.proto`，package `projects.game.v2`）

```protobuf
syntax = "proto3";
package projects.game.v2;

import "google/api/annotations.proto";
import "google/api/field_behavior.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/timestamp.proto";

option go_package = "dominion/projects/game/v2";

// ConversationService is the agent_v2 conversation surface, exposed through
// the game gateway under /api/v2 (AIP-136 custom methods; the session field
// carries the full game session resource name per AIP-122).
service ConversationService {
  // Send one user message and stream the turn's events until its turn ends
  // (including queued waiting, FR-012).
  rpc Send(SendRequest) returns (stream ChatEvent) {
    option (google.api.http) = {
      post: "/api/v2/{session=templates/*/sessions/*}:send"
      body: "*"
    };
  }
  // ListHistory returns the session's in-memory conversation history for
  // refresh/reconnect backfill (FR-014).
  rpc ListHistory(ListHistoryRequest) returns (ListHistoryResponse) {
    option (google.api.http) = {
      get: "/api/v2/{session=templates/*/sessions/*}:history"
    };
  }
  // Dispose releases the session's dsh resources immediately: in-flight turn
  // aborted, queued messages dropped, history no longer queryable (FR-015).
  // Idempotent: an absent session is treated as already released.
  rpc Dispose(DisposeRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      post: "/api/v2/{session=templates/*/sessions/*}:dispose"
      body: "*"
    };
  }
}

message SendRequest {
  // Full game session resource name: templates/{template}/sessions/{session}.
  string session = 1 [(google.api.field_behavior) = REQUIRED];
  // The user message text for this turn. Non-empty (INVALID_ARGUMENT).
  string text = 2 [(google.api.field_behavior) = REQUIRED];
}

message ListHistoryRequest {
  string session = 1 [(google.api.field_behavior) = REQUIRED];
}

message DisposeRequest {
  string session = 1 [(google.api.field_behavior) = REQUIRED];
}

// ─── Streaming events ────────────────────────────────────────────────────────

// ChatEvent is one streamed conversation event of a Send call. The payload
// vocabulary mirrors dsh StreamChunk one-to-one (plus the queued/turn
// lifecycle envelopes); block indexes follow first-seen stream order.
message ChatEvent {
  string session = 1 [(google.api.field_behavior) = REQUIRED];
  // Server-minted turn identity (UUID); constant across one turn's events.
  string turn_id = 2;
  oneof payload {
    QueuedEvent queued = 10;
    TurnStartEvent turn_start = 11;
    BlockStartEvent block_start = 12;
    BlockDeltaEvent delta = 13;
    BlockEndEvent block_end = 14;
    TurnEndEvent turn_end = 15;
  }
}

// The message was enqueued behind a running turn (FR-012); position is the
// 1-based queue slot at enqueue time.
message QueuedEvent { int32 position = 1; }

message TurnStartEvent {}

// BlockType selects the delta semantics of a content block.
enum BlockType {
  BLOCK_TYPE_UNSPECIFIED = 0;
  BLOCK_TYPE_TEXT = 1;
  BLOCK_TYPE_THINK = 2;
  BLOCK_TYPE_TOOL_CALL = 3;
}

message BlockStartEvent {
  int32 index = 1;
  BlockType type = 2;
  // TOOL_CALL only: the provider-issued call id and tool name.
  string tool_id = 3;
  string name = 4;
}

// One incremental text fragment of the block at `index`; semantics derive
// from the owning BlockStartEvent.type (TEXT body / THINK reasoning /
// TOOL_CALL raw JSON argument fragment).
message BlockDeltaEvent {
  int32 index = 1;
  string text = 2;
}

message BlockEndEvent {
  int32 index = 1;
  ContentBlock block = 2;
}

enum TurnStatus {
  TURN_STATUS_UNSPECIFIED = 0;
  TURN_STATUS_COMPLETED = 1;
  TURN_STATUS_ERROR = 2;    // model/transport failure; session stays usable
  TURN_STATUS_ABORTED = 3; // session disposed mid-turn / queue dropped
}

message TurnError {
  string code = 1;
  string message = 2;
}

message TurnUsage {
  int64 input_tokens = 1;
  int64 output_tokens = 2;
  int64 reasoning_tokens = 3;
}

message TurnEndEvent {
  TurnStatus status = 1;
  TurnError error = 2;
  TurnUsage usage = 3;
}

// ─── History ─────────────────────────────────────────────────────────────────

enum Role {
  ROLE_UNSPECIFIED = 0;
  ROLE_USER = 1;
  ROLE_AGENT = 2;
}

message ListHistoryResponse {
  repeated HistoryMessage messages = 1;
}

message HistoryMessage {
  // Server-assigned per-session sequence id (e.g. "m1", "m2").
  string message_id = 1;
  Role role = 2;
  google.protobuf.Timestamp create_time = 3;
  repeated ContentBlock blocks = 4;
}

// ContentBlock is one display content block (behavior baseline: game.proto
// MessagePart semantics — text/think classified, tool call & result linked).
message ContentBlock {
  oneof kind {
    TextBlock text = 1;
    ThinkBlock think = 2;
    ToolCallBlock tool_call = 3;
  }
}

message TextBlock { string content = 1; }

message ThinkBlock { string content = 1; }

enum ToolStatus {
  TOOL_STATUS_UNSPECIFIED = 0;
  TOOL_STATUS_RUNNING = 1;
  TOOL_STATUS_SUCCEEDED = 2;
  TOOL_STATUS_FAILED = 3;
}

message ToolCallBlock {
  string tool_id = 1;
  string name = 2;
  // Raw JSON arguments string, as produced by the model.
  string args_json = 3;
  ToolStatus status = 4;
  // Human-readable execution result; linked to the call in one block (US3).
  string result = 5;
}
```

Go 侧代码生成沿 `projects/game/BUILD.bazel` 的 `go_proto_library` 形态（go_grpc_v2 + go_proto + grpc-gateway + go_gen_aip 编译器，importpath `dominion/projects/game/v2`）；TS 侧沿 demo 的 `ts_proto_library` + proto-loader 运行时加载（`experimental/dsh/demo/agent/BUILD.bazel`、`src/server.ts`）。

## 2. REST/流式绑定与错误映射

| RPC | HTTP | 成功 | 请求级失败（流不开启） |
|---|---|---|---|
| Send | `POST /api/v2/templates/{t}/sessions/{s}:send`，body `{"text": "..."}` | 200 + `application/json` NDJSON chunked，逐事件 flush | 空文本/资源名非法 → 400 INVALID_ARGUMENT；网关到 agent_v2 不可达 → 502/14 |
| ListHistory | `GET ...:history` | 200 JSON（`ListHistoryResponse`） | 资源名非法 → 400 |
| Dispose | `POST ...:dispose`，body `{}` | 200 `{}`（幂等：不存在亦成功） | 资源名非法 → 400 |

- **NDJSON 帧**：每个 `ChatEvent` 序列化为单行 JSON + `\n`（grpc-gateway v2 默认流式 marshaler 行为）。事件内未知 oneof 分支必须被消费端忽略（proto3 forward-compat）。
- **回合内错误走事件**（`turn_end{ERROR}`，HTTP 仍 200）：模型端点不可达/超时/流中断——进程存活、会话可恢复（spec Edge Cases）。
- **同源**：经 game.liukexin.com 路径分流（[research.md](../research.md) D5），前端相对路径调用，零 CORS。

## 3. 事件序不变式（消费端可依赖的顺序保证）

1. 每个流**恰好一个**终结事件（`turn_end`），且是最后一帧。
2. `turn_start` 先于本回合全部 `block_*`/`delta` 事件；`queued`（如出现）先于 `turn_start`。
3. `block_start{index}` 恰出现一次/块；其 `delta` 与 `block_end` 以同 `index` 归属且不与他块交错（对齐 cookbook 的 index 分配义务）。
4. `delta.text` 按到达序拼接 = 块内容；`block_end.block` 为该块终态（与拼接结果一致）。
5. 无思考回合不出现 THINK 块（US2 场景 2）；无工具回合不出现 TOOL_CALL 块（FR-006）。
6. `usage`（如提供）随 `turn_end` 携带（对齐 cookbook "usage before finish and nothing after"——映射为终局事件载荷）。
7. **排队**：忙时到达的 Send 首帧为 `queued{position}`；此后流静默直至该消息回合 `turn_start`（FR-012 自动按序发送）。
8. **处置**：Dispose 后该会话在途流各收一帧 `turn_end{ABORTED}` 后关闭；排队消息的流同样收 `turn_end{ABORTED}`（作废）。

## 4. dsh 事件 → ChatEvent 映射（agent_v2 转发规则）

| dsh 事件（`session/event` 载荷） | ChatEvent | 说明 |
|---|---|---|
| ——（Send 到达且会话忙） | `queued{position}` | 宿主队列语义 |
| ——（回合开始） | `turn_start` | followup 提交、订阅就绪后 |
| chunk `block-start{text}` / `block-start{reasoning}` / `block-start{tool-call}` | `block_start{type: TEXT\|THINK\|TOOL_CALL, tool_id?, name?}` | type 直映射；index 透传 |
| chunk `text-delta` / `reasoning-delta` / `tool-call-delta` | `delta{index, text}` | 三类 delta 统一为 text 载荷，语义由块类型决定 |
| chunk `block-end` | `block_end{index, block}` | ContentBlock 映射：`text→TextBlock`、`reasoning→ThinkBlock`、`tool-call→ToolCallBlock{status: RUNNING}`（本阶段无终态来源） |
| chunk `usage` | 并入 `turn_end.usage` | 不单独发帧 |
| `agent/status → idle` | `turn_end{COMPLETED, usage}` | 回合终止信号（047 D3） |
| `agent/error` / `turn/end{error}` | `turn_end{ERROR, error{code,message}}` | 错误信息不得包含 token 内容（SC-004） |
| Dispose/进程退出中断 | `turn_end{ABORTED}` | 宿主补发 |

历史侧：`assistant/message`（终局块）→ `HistoryMessage{role: AGENT, blocks}` 追加；用户消息入队时 → `HistoryMessage{role: USER, blocks:[TextBlock]}` 追加（[data-model.md](../data-model.md) §2.5）。

## 5. 刷新回填与多标签页（FR-014 边界）

- **回填**：进入/刷新对话页 → `GET :history` 全量回填（存活期间）；流式与回填一致性由"事件终态 ⊕ 历史 = assistant/message"结构保证（[data-model.md](../data-model.md) §3-2）。
- **中途断开**：浏览器断开只停转发不停服务端回合；刷新后 history 呈现已产出前缀，随回合推进增长直至完整。
- **多标签页**（已知限制，记录于 research.md）：未发送消息的标签页不收实时事件；可刷新经 history 查询看到进展。
- **历史丢失**：agent_v2 重启后 history 为空（spec Assumptions：内存态）；session 列表（/api/v1）不受影响。

## 6. 消费端样例（前端流读取）

```ts
// NDJSON 行流读取（fetch + ReadableStream；EventSource 不适用——POST body）
async function* sendStream(session: string, text: string): AsyncIterable<ChatEvent> {
  const res = await fetch(`/api/v2/${session}:send`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text }),
  });
  if (!res.ok || !res.body) throw new ApiError(res.status, await res.text());
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let nl: number;
    while ((nl = buf.indexOf("\n")) >= 0) {
      yield JSON.parse(buf.slice(0, nl)) as ChatEvent;
      buf = buf.slice(nl + 1);
    }
  }
}
```

## 7. 验收锚点（对应 spec 场景）

| 场景 | 断言 |
|---|---|
| US1-1 | Send → 200，`turn_start`→…→`turn_end{COMPLETED}` 事件序完整，正文渐进到达（多帧 delta） |
| US1-2 | 同会话第二轮回复体现首轮上下文（fake 多轮条件/真实模型均可验证） |
| US1-4 / FR-012 | 回合进行中 Send → 首帧 `queued{≥1}` → 前序 `turn_end` 后自动 `turn_start` |
| US2 | THINK 块与 TEXT 块分类可区分获取；无思考回合零 THINK 块 |
| US4-3 / FR-015 | 删除→dispose：在途流收 `turn_end{ABORTED}`；同资源名新建为全新会话 |
| FR-014 | 回填内容与流式终态一致 |
| Edge-模型故障 | `turn_end{ERROR}`，进程存活，后续轮次成功 |
