# Contract: AgentService（agent-v2 对话/agent/preset/模型目录 API）

**Feature**: [spec.md](spec.md) FR-005/006/007/008 | **决策**: [research.md](../research.md) D1/D2/D3/D4/D9/D10 | **数据模型**: [data-model.md](../data-model.md) §2.1–2.4/§3

契约即 proto：§1 为 `projects/game/agent_v2.proto` 全文（逐字一致）。面 = **`AgentService`**（会话作用域有状态面）+ **`PresetService`**（无状态配置面）+ **`DesktopBridgeService`**（flow 桥接，见 [desktop-bridge.md](desktop-bridge.md)）。proto package `projects.game.v2` 不变；Go 生成面并入 `dominion/projects/game` 包（protoc-gen-go-aip v0.1.3 跨 Go 包父构造器缺陷的规避裁定，[revisions/directive-2026-09-01.md](../revisions/directive-2026-09-01.md) §2.6）；proto 文件在 app 根（049 D12 惯例）。049 的 [conversation-api.md](../../049-agent-v2-dsh-init/contracts/conversation-api.md) 事件序/映射/两跳错误表除本文显式修改外全部延续。

## 1. Proto 面

`projects/game/agent_v2.proto` 全文（与仓库文件逐字一致；REST 注解、资源注解与 `resource_reference` 齐备——`Agent`/`Preset` 挂 `google.api.resource`，type 沿用 `game.liukexin.com/*` 域，AIP-122/123）：

```proto
syntax = "proto3";

package projects.game.v2;

import "google/api/annotations.proto";
import "google/api/field_behavior.proto";
import "google/api/resource.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/field_mask.proto";
import "google/protobuf/timestamp.proto";
import "projects/game/game.proto";

// go_package: the v2 Go generated surface is merged into the v1 package
// dominion/projects/game — protoc-gen-go-aip v0.1.3 emits the parent
// constructor's receiver type unqualified, which only resolves when the
// parent resources (v1 Template/Session) share the generated file's Go
// package (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md
// §2.6). The proto package stays projects.game.v2: wire, REST, and TS are
// unaffected.
option go_package = "dominion/projects/game";

// AgentService is the session-scoped stateful surface of agent_v2: the
// session's agent singleton (AIP-156 singletons,
// https://google.aip.dev/156), its in-memory message history, and the Send
// turn stream. Agent/queue/game state lives in the serving instance's
// process memory, so every RPC requires proxy owner affinity — the gateway
// routes this service through the proxy, while the stateless configuration
// surface (PresetService, same host and port) is dialed directly
// (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §4).
// Exposed through the game gateway under /api/v2 (AIP-134 create-or-update,
// https://google.aip.dev/134#create-or-update; AIP-132 standard List,
// https://google.aip.dev/132; AIP-136 custom methods,
// https://google.aip.dev/136; resource names per AIP-122,
// https://google.aip.dev/122).
// Contract: specs/051-agent-v2-dsh-migration/contracts/agent-api.md §1/§2.
// Prefix Path: /api/v2
service AgentService {
  // UpdateAgent materializes (or refreshes) the session's agent singleton:
  // create-or-update on an AIP-156 singleton (AIP-134 create-or-update,
  // https://google.aip.dev/134#create-or-update) — there is no Create/Delete
  // RPC. Validation is fail-fast (preset must exist, model must be in the
  // catalog); an existing agent is torn down and rebuilt with the new
  // configuration, so every call yields a clean agent (refresh semantics
  // included).
  rpc UpdateAgent(UpdateAgentRequest) returns (Agent) {
    option (google.api.http) = {
      patch: "/api/v2/{agent.name=templates/*/sessions/*/agent}"
      body: "agent"
    };
  }

  // GetAgent returns the session's materialized agent configuration;
  // NOT_FOUND when the agent is not materialized. AIP-131:
  // https://google.aip.dev/131
  rpc GetAgent(GetAgentRequest) returns (Agent) {
    option (google.api.http) = {
      get: "/api/v2/{name=templates/*/sessions/*/agent}"
    };
  }

  // ListAgentMessages lists the agent's in-memory conversation history for
  // refresh/reconnect backfill. AIP-132 standard List over the agent
  // singleton's message child collection (the parent is the agent resource
  // name); AIP-158: https://google.aip.dev/158
  rpc ListAgentMessages(ListAgentMessagesRequest) returns (ListAgentMessagesResponse) {
    option (google.api.http) = {
      get: "/api/v2/{parent=templates/*/sessions/*/agent}/messages"
    };
  }

  // Send one user message and stream the turn's events until its turn ends
  // (including queued waiting). Custom method on the session resource
  // (AIP-136: https://google.aip.dev/136); the agent must already be
  // materialized — there is no lazy creation.
  rpc Send(SendRequest) returns (stream ChatEvent) {
    option (google.api.http) = {
      post: "/api/v2/{session=templates/*/sessions/*}:send"
      body: "*"
    };
  }
}

// PresetService is the stateless configuration surface of agent_v2: the
// preset collection backing agent materialization and the deployment-level
// model catalog. Preset state lives in Mongo and the catalog is static
// plugin configuration, so any agent_v2 instance can serve every RPC — the
// gateway dials agent_v2 directly for this service (no proxy owner
// affinity). Contract: specs/051-agent-v2-dsh-migration/contracts/agent-api.md.
// Prefix Path: /api/v2
service PresetService {
  // CreatePreset creates a Preset under a template; a duplicate caller-id
  // errors with ALREADY_EXISTS. AIP-133: https://google.aip.dev/133
  rpc CreatePreset(CreatePresetRequest) returns (Preset) {
    option (google.api.http) = {
      post: "/api/v2/{parent=templates/*}/presets"
      body: "preset"
    };
  }

  // ListPresets lists Presets under a template. AIP-132:
  // https://google.aip.dev/132
  rpc ListPresets(ListPresetsRequest) returns (ListPresetsResponse) {
    option (google.api.http) = {
      get: "/api/v2/{parent=templates/*}/presets"
    };
  }

  // GetPreset gets a Preset. AIP-131: https://google.aip.dev/131
  rpc GetPreset(GetPresetRequest) returns (Preset) {
    option (google.api.http) = {
      get: "/api/v2/{name=templates/*/presets/*}"
    };
  }

  // UpdatePreset updates a Preset (mutable field: player_prompt).
  // AIP-134: https://google.aip.dev/134
  rpc UpdatePreset(UpdatePresetRequest) returns (Preset) {
    option (google.api.http) = {
      patch: "/api/v2/{preset.name=templates/*/presets/*}"
      body: "preset"
    };
  }

  // DeletePreset deletes a Preset; it does not affect already-materialized
  // agents (their persona is a materialization-time snapshot).
  // AIP-135: https://google.aip.dev/135
  rpc DeletePreset(DeletePresetRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      delete: "/api/v2/{name=templates/*/presets/*}"
    };
  }

  // ListModels returns the read-only deployment-level model catalog
  // (no parent collection, no CRUD). The catalog source is the llm-glm
  // plugin configuration, shared with UpdateAgent's model validation.
  rpc ListModels(ListModelsRequest) returns (ListModelsResponse) {
    option (google.api.http) = {
      get: "/api/v2/models"
    };
  }
}

// DesktopBridgeService carries the desktop flow control stream for one
// session: the desktop connects through the gateway's WebSocket endpoint and
// the frames are relayed to the agent_v2 instance owning the session.
// Bidirectional streaming has no REST binding (AIP-127,
// https://google.aip.dev/127) — the WS path is carried by a gateway custom
// route, mirroring the v1 TeamService.Connect shape. Frame semantics:
// specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §1.
// Prefix Path: /api/v2/templates/{template}/sessions/{session}/connect
service DesktopBridgeService {
  // Connect establishes the bidirectional flow stream. The first UserFrame
  // (gateway-injected identity, StatusSignal probe) binds the connection to
  // the session; a new connection for the same session takes over and closes
  // the previous one. The client sends UserFrames (probes, operation
  // results); the server sends TeamFrames (status responses, operation
  // requests). Frame types are reused verbatim from projects/game/game.proto.
  rpc Connect(stream projects.game.UserFrame) returns (stream projects.game.TeamFrame);
}

// Agent is the session's agent singleton resource (AIP-156:
// https://google.aip.dev/156): exactly one per session, materialized via
// UpdateAgent (no Create/Delete RPC).
message Agent {
  option (google.api.resource) = {
    type: "game.liukexin.com/Agent"
    pattern: "templates/{template}/sessions/{session}/agent"
    singular: "agent"
    plural: "agents"
  };

  // The agent resource name.
  // Format: templates/{template}/sessions/{session}/agent
  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  // The full preset resource name the agent materializes from; required.
  string preset = 2 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {
      type: "game.liukexin.com/Preset"
    }
  ];
  // The model id; empty = process default (GLM_MODEL || "glm-5.2", same
  // source as the ListModels catalog).
  string model = 3;
  google.protobuf.Timestamp create_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp update_time = 5 [(google.api.field_behavior) = OUTPUT_ONLY];
}

// UpdateAgentRequest materializes or refreshes the agent singleton. The
// resource's name field (Agent.name, pattern
// templates/{template}/sessions/{session}/agent) is the target identity,
// surfaced in the HTTP path via {agent.name=...}. An omitted update_mask
// replaces all mutable fields (preset, model); allow_missing=true makes the
// call create the agent on first use (AIP-134 create-or-update).
message UpdateAgentRequest {
  Agent agent = 1 [(google.api.field_behavior) = REQUIRED];
  google.protobuf.FieldMask update_mask = 2;
  bool allow_missing = 3;
}

// GetAgentRequest gets the session's agent singleton.
message GetAgentRequest {
  // The agent resource name to retrieve.
  // Format: templates/{template}/sessions/{session}/agent
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {
      type: "game.liukexin.com/Agent"
    }
  ];
}

// ListAgentMessagesRequest lists the agent singleton's messages.
message ListAgentMessagesRequest {
  // The parent agent resource name.
  // Format: templates/{template}/sessions/{session}/agent
  string parent = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {
      type: "game.liukexin.com/Agent"
    }
  ];
  int32 page_size = 2;
  string page_token = 3;
}

// ListAgentMessagesResponse is the response for listing agent messages.
message ListAgentMessagesResponse {
  repeated HistoryMessage messages = 1;
  string next_page_token = 2;
}

// SendRequest sends one user message to the session's agent.
message SendRequest {
  // Full game session resource name: templates/{template}/sessions/{session}.
  // The Session type is declared by projects/game/game.proto (the v1 face
  // of the same game API — shared resource type domain, AIP-123:
  // https://google.aip.dev/123).
  string session = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {
      type: "game.liukexin.com/Session"
    }
  ];
  // The user message text for this turn. Non-empty (INVALID_ARGUMENT).
  string text = 2 [(google.api.field_behavior) = REQUIRED];
}

// Preset is the per-template configuration resource an agent materializes
// from. Persistence is the agent_v2 service's own (Mongo, survives
// restarts).
message Preset {
  option (google.api.resource) = {
    type: "game.liukexin.com/Preset"
    pattern: "templates/{template}/presets/{preset}"
    singular: "preset"
    plural: "presets"
  };

  // The preset resource name.
  // Format: templates/{template}/presets/{preset}
  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  // The player prompt; empty = fall back to the default player base at
  // materialization time. MUST NOT carry model fields.
  string player_prompt = 2;
  google.protobuf.Timestamp create_time = 3 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp update_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];
}

// CreatePresetRequest creates a Preset under a template (AIP-133:
// https://google.aip.dev/133; caller-supplied id).
message CreatePresetRequest {
  // The template to create the preset in.
  // Format: templates/{template}
  string parent = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {
      child_type: "game.liukexin.com/Preset"
    }
  ];
  string preset_id = 2 [(google.api.field_behavior) = REQUIRED];
  Preset preset = 3 [(google.api.field_behavior) = REQUIRED];
}

// ListPresetsRequest lists Presets under a template (AIP-132:
// https://google.aip.dev/132; AIP-158: https://google.aip.dev/158).
message ListPresetsRequest {
  // The template to list presets from.
  // Format: templates/{template}
  string parent = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {
      child_type: "game.liukexin.com/Preset"
    }
  ];
  int32 page_size = 2;
  string page_token = 3;
}

// ListPresetsResponse is the response for listing Presets.
message ListPresetsResponse {
  repeated Preset presets = 1;
  string next_page_token = 2;
}

// GetPresetRequest gets a Preset (AIP-131: https://google.aip.dev/131).
message GetPresetRequest {
  // The preset resource name to retrieve.
  // Format: templates/{template}/presets/{preset}
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {
      type: "game.liukexin.com/Preset"
    }
  ];
}

// UpdatePresetRequest updates a Preset (AIP-134:
// https://google.aip.dev/134). The resource's name field (Preset.name,
// pattern templates/{template}/presets/{preset}) is the target identity,
// surfaced in the HTTP path via {preset.name=...}. update_mask supports the
// player_prompt path; an omitted mask replaces all mutable fields.
message UpdatePresetRequest {
  Preset preset = 1 [(google.api.field_behavior) = REQUIRED];
  google.protobuf.FieldMask update_mask = 2;
}

// DeletePresetRequest deletes a Preset (AIP-135: https://google.aip.dev/135).
message DeletePresetRequest {
  // The preset resource name to delete.
  // Format: templates/{template}/presets/{preset}
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {
      type: "game.liukexin.com/Preset"
    }
  ];
}

// Model is one entry of the read-only deployment-level model catalog.
message Model {
  string id = 1;
  int32 context_window = 2;
}

// ListModelsRequest lists the deployment-level model catalog (no parent
// collection).
message ListModelsRequest {}

// ListModelsResponse is the response for listing Models.
message ListModelsResponse {
  repeated Model models = 1;
}

// ─── Streaming events ────────────────────────────────────────────────────────

// ChatEvent is one streamed conversation event of a Send call. The payload
// vocabulary mirrors dsh StreamChunk one-to-one (plus the queued/turn
// lifecycle envelopes); block indexes are globally monotonic within one
// turn (per-step provider indexes are remapped by the collector).
// Mapping table: specs/049-agent-v2-dsh-init/contracts/conversation-api.md §4;
// tool_result extension: specs/051-agent-v2-dsh-migration/research.md D10.
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
    ToolResultEvent tool_result = 16;
  }
}

// The message was enqueued behind a running turn
// (specs/049-agent-v2-dsh-init/spec.md FR-012); position is the 1-based
// queue slot at enqueue time.
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

// ToolResultEvent carries one tool execution's terminal outcome. tool_id is
// the join key back to the originating tool-call block; the web client
// finalizes the matching block on arrival.
message ToolResultEvent {
  // The provider-issued tool call id this result belongs to.
  string tool_id = 1;
  // Terminal tool status: SUCCEEDED or FAILED.
  ToolStatus status = 2;
  // The rendered result text (board text / error text).
  string result = 3;
}

enum TurnStatus {
  TURN_STATUS_UNSPECIFIED = 0;
  TURN_STATUS_COMPLETED = 1;
  TURN_STATUS_ERROR = 2;   // model/transport failure; session stays usable
  TURN_STATUS_ABORTED = 3; // turn aborted mid-flight (re-materialization / shutdown)
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

message HistoryMessage {
  // Server-assigned per-session sequence id (e.g. "m1", "m2").
  string message_id = 1;
  Role role = 2;
  google.protobuf.Timestamp create_time = 3;
  repeated ContentBlock blocks = 4;
}

// ContentBlock is one display content block.
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
  // Human-readable execution result; linked to the call in one block. The
  // terminal status/result are backfilled by tool_id when the matching
  // tool_result arrives (specs/051-agent-v2-dsh-migration/research.md D10).
  string result = 5;
}
```

TS 类型经 `ts_proto_library`（消费 `:agent_v2_proto`）重生成，不受 Go 包归属影响；Go 生成单元为合并后的 `game_go_proto`（grpc + grpc-gateway 双服务 + go_gen_aip 解析器，`projects/game/BUILD.bazel`；directive §2.3/§2.6）——`ParseAgentName`/`ParsePresetName`/`Agent.ParsePreset`/`SendRequest.ParseSession` 等生成于 `dominion/projects/game` 包。

## 2. 方法语义

### 2.1 UpdateAgent（物化/刷新统一入口，FR-006/008）

1. **校验先行**（fail-fast，无半物化）：资源名形状 + template ∈ {saolei}；`preset` 非空且存在（否则 `NOT_FOUND`）；`model` 非空时 ∈ `ListModels` 目录（否则 `INVALID_ARGUMENT`，US2 场景 7）。
2. **物化**：存在旧物化 → 既定终止语义（在途回合 `turn_end{ABORTED}`、排队作废）→ dispose 旧 dsh agent（history/queue/GameRuntime 随之清空）→ `ctx.agents.create({sessionId, agentOptions: {provider: "glm-responses", model, persona}})`；persona = preset 当前 `player_prompt`，空回退 `DEFAULT_PLAYER_BASE`。**无论配置是否变化**都执行上述清理重建（refresh 语义并入）。
3. **幂等**：同配置重复 Update → 同配置干净 agent；返回物化后的 `Agent`（含时间戳）。
4. **模型目录同源**：校验与 web 下拉共用 `ctx.llm.listModels("glm-responses")`（[research.md](../research.md) D4）。

### 2.2 GetAgent

返回当前物化配置；未物化（含重启后 owner 在而 agent 无）→ `NOT_FOUND`。

### 2.3 ListAgentMessages

保序/保分类/回填语义 = 049 FR-014；分页字段为协议合规（内存全量按序，token 为空实现即可——`next_page_token` 恒空、`page_size` 上限 1000 忽略截断）。ToolCallBlock 终态与 `result` 已按 tool_id 回填（[data-model.md](../data-model.md) §2.3）。

### 2.4 Send（显式物化前置，FR-007）

- 请求级校验（流不打开）：资源名形状 / 空文本 → `INVALID_ARGUMENT`。
- **未物化拒绝**：proxy 无 owner → `NOT_FOUND`；owner 在而 agent 未物化 → `FAILED_PRECONDITION`（"agent not materialized; send UpdateAgent first"，grpc-gateway → 400）。**MUST NOT 懒物化**（049 get-or-create 行为废止）。
- 流式行为（NDJSON `{"result": <ChatEvent>}` 每行、排队、turn 序、错误乘事件不乘 HTTP 状态）= 049 §2/§3 全部延续；新增 `tool_result` 帧（多 step 工具回合）与**回合全局 block index**（[data-model.md](../data-model.md) §2.4）。

### 2.5 preset CRUD（PresetService 面，FR-005）

标准 AIP 语义：Create（caller-supplied id、template 校验、重复 `ALREADY_EXISTS`）、Get/List（keyset 分页可选，个人规模默认 100）、Update（FieldMask `player_prompt`）、Delete（无联动）。持久化 Mongo `game_agent_v2.presets`，重启不丢。

### 2.6 ListModels（PresetService 面）

只读目录（`Model{id, context_window}`），源 = 组合配置 `llm-glm models[]` 经 `ctx.llm.listModels` 导出；响应不含任何端点/token 信息（SC-006）。归属 PresetService：目录与 preset 面同为无状态配置面，与 web 物化面板的双下拉消费形态对齐（[research.md](../research.md) D9）。

## 3. 错误表（按路由面分层，049 §2.1 延续 + 变更）

**会话面（AgentService/DesktopBridgeService，gateway → proxy → agent-v2 两跳）**：

| 故障点 | gRPC / HTTP |
|---|---|
| gateway→proxy 不可达 | UNAVAILABLE / 503 |
| owner store（Mongo）故障 | INTERNAL / 500 |
| 无 agent-v2 活实例 | UNAVAILABLE / 503 |
| owner 实例离线 / 流打开失败 | UNAVAILABLE / 503 |
| Send 无 owner | NOT_FOUND / 404 |
| Send 有 owner 未物化 | FAILED_PRECONDITION / 400 |
| UpdateAgent preset 缺失 / model 未知 | NOT_FOUND 404 / INVALID_ARGUMENT 400 |
| agent-v2 其余请求级错误 | 原样透传（049 propagateAgentError 语义） |
| 回合内错误（模型/工具失败） | 乘 `turn_end{ERROR}` 或工具错误结果，HTTP 仍 200 |

**配置面（PresetService，gateway → agent-v2 一跳直连）**：

| 故障点 | gRPC / HTTP |
|---|---|
| gateway→agent-v2 不可达 / 无活实例 | UNAVAILABLE / 503 |
| Mongo 故障（preset 读写） | INTERNAL / 500 |
| preset 重复创建等请求级错误 | agent-v2 原样返回（ALREADY_EXISTS 409 等） |

## 4. 路由拓扑（双面分工）

- **会话面**（`AgentService` + `DesktopBridgeService`）：agent-v2 的 agent/队列/游戏状态在进程内存，仅经 proxy owner 亲和可达（无 http 块，049 D4 的适用边界=会话面）；gateway 在 proxy conn（teamConn）上注册 `AgentService` HTTP 面（`/api/v2/` 子树的 4 条 agent 路径）。proxy 分配语义见 [data-model.md](../data-model.md) §2.9（UpdateAgent = 新分配点；Send 只查不分配）。
- **配置面**（`PresetService`）：preset 状态在 Mongo、模型目录为静态配置，任意 agent-v2 实例可服务——gateway 直连 agent-v2（presetConn，`gameconst.AgentV2Target` 服务发现，gRPC 客户端 LB），不经 proxy（2026-09-01 用户指令，[revisions/directive-2026-09-01.md](../revisions/directive-2026-09-01.md) §3；[research.md](../research.md) D9）。REST 路径与请求形状不变，对 web 零感知。
- 两面的 gwmux handler 路径集不相交（agent 面 4 路径 vs preset/models 面 6 路径）。

## 5. 消费方契约

- **web**：`api/agent.ts` 全方法消费；`Send`/`ListAgentMessages` 替代 049 `:history`；删除编排不再调 dispose（见 [web-frontend.md](web-frontend.md)）。
- **desktop**：不消费 AgentService（对话/物化均不在 desktop 面）。
- **测试**：`agent_v2_conversation_test`（更名后回归）+ `agent_v2_preset_test`（新，US2 全场景断言）。

## 6. 实现期必读（间接引用显式列出）

- 049 契约基线：`specs/049-agent-v2-dsh-init/contracts/conversation-api.md`（事件序 §3、dsh→ChatEvent 映射 §4、两跳错误 §2.1——延续部分）
- AIP 规范（Google, 仓库外）：AIP-131 Get https://google.aip.dev/131 、AIP-132 List https://google.aip.dev/132 、AIP-133 Create https://google.aip.dev/133 、AIP-134 Update https://google.aip.dev/134 、AIP-135 Delete https://google.aip.dev/135 、AIP-136 自定义方法 https://google.aip.dev/136 、AIP-156 单例资源 https://google.aip.dev/156
- 仓库 API 风格：`style/api.md`
- v1 语义参照：`projects/game/game.proto`（Team/TeamProfile 的 AIP 形状先例）、`projects/game/agent/src/handler.ts:101-280`（UpdateTeam/GetTeam/RefreshTeam 的错误映射先例）
