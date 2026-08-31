# Contract: AgentService（agent-v2 对话/agent/preset/模型目录 API）

**Feature**: [spec.md](spec.md) FR-005/006/007/008 | **决策**: [research.md](../research.md) D1/D2/D3/D4/D9/D10 | **数据模型**: [data-model.md](../data-model.md) §2.1–2.4/§3

`projects/game/agent_v2.proto` 重塑：原 `ConversationService` 更名 **`AgentService`** 并规范化（AIP）；新增 **`DesktopBridgeService`**（见 [desktop-bridge.md](desktop-bridge.md)）。package `projects.game.v2`、`go_package = "dominion/projects/game/v2"` 不变；proto 文件仍在 app 根（049 D12 惯例）。049 的 [conversation-api.md](../../049-agent-v2-dsh-init/contracts/conversation-api.md) 事件序/映射/两跳错误表除本文显式修改外全部延续。

## 1. Proto 面

```proto
syntax = "proto3";
package projects.game.v2;
// imports: annotations, field_behavior, empty, timestamp,
//          projects/game/game.proto（仅 DesktopBridgeService 帧复用）

service AgentService {
  // AIP-156 单例 + AIP-134 create-or-update（allow_missing=true 物化；无 Create/Delete）
  rpc UpdateAgent(UpdateAgentRequest) returns (Agent) {
    patch: "/api/v2/{agent.name=templates/*/sessions/*/agent}" body: "agent";
  }
  rpc GetAgent(GetAgentRequest) returns (Agent) {
    get: "/api/v2/{name=templates/*/sessions/*/agent}";
  }
  // AIP-132 标准 List（消息为 agent 单例子资源）
  rpc ListAgentMessages(ListAgentMessagesRequest) returns (ListAgentMessagesResponse) {
    get: "/api/v2/{parent=templates/*/sessions/*/agent}/messages";
  }
  // AIP-136 自定义方法（server-streaming；MUST NOT 懒物化）
  rpc Send(SendRequest) returns (stream ChatEvent) {
    post: "/api/v2/{session=templates/*/sessions/*}:send" body: "*";
  }
  // preset 标准 CRUD（AIP-133/131/132/134/135）
  rpc CreatePreset(CreatePresetRequest) returns (Preset) {
    post: "/api/v2/{parent=templates/*}/presets" body: "preset";
  }
  rpc ListPresets(ListPresetsRequest) returns (ListPresetsResponse) {
    get: "/api/v2/{parent=templates/*}/presets";
  }
  rpc GetPreset(GetPresetRequest) returns (Preset) {
    get: "/api/v2/{name=templates/*/presets/*}";
  }
  rpc UpdatePreset(UpdatePresetRequest) returns (Preset) {
    patch: "/api/v2/{preset.name=templates/*/presets/*}" body: "preset";
  }
  rpc DeletePreset(DeletePresetRequest) returns (google.protobuf.Empty) {
    delete: "/api/v2/{name=templates/*/presets/*}";
  }
  // 只读模型目录（部署级，无父集合）
  rpc ListModels(ListModelsRequest) returns (ListModelsResponse) {
    get: "/api/v2/models";
  }
}

message Agent {
  string name = 1;              // templates/{template}/sessions/{session}/agent
  string preset = 2 [(google.api.field_behavior) = REQUIRED];  // 完整 preset 资源名
  string model = 3;             // 空 = 进程默认（GLM_MODEL || "glm-5.2"）
  google.protobuf.Timestamp create_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp update_time = 5 [(google.api.field_behavior) = OUTPUT_ONLY];
}
message UpdateAgentRequest {
  Agent agent = 1 [(google.api.field_behavior) = REQUIRED];
  google.protobuf.FieldMask update_mask = 2;  // 省略 = 全量替换可变字段（preset、model）
  bool allow_missing = 3;                     // web 恒发 true；服务端按 create-or-update 语义实现
}
message GetAgentRequest { string name = 1 [(google.api.field_behavior) = REQUIRED]; }
message ListAgentMessagesRequest {
  string parent = 1 [(google.api.field_behavior) = REQUIRED];
  int32 page_size = 2; string page_token = 3;
}
message ListAgentMessagesResponse {
  repeated HistoryMessage messages = 1; string next_page_token = 2;
}
message SendRequest {
  string session = 1 [(google.api.field_behavior) = REQUIRED];  // 完整 session 资源名
  string text = 2 [(google.api.field_behavior) = REQUIRED];     // 非空
}
message Preset {
  string name = 1;              // templates/{template}/presets/{preset}
  string player_prompt = 2;     // 空 = 物化回退 DEFAULT_PLAYER_BASE
  google.protobuf.Timestamp create_time = 3 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp update_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];
}
message CreatePresetRequest {
  string parent = 1 [(google.api.field_behavior) = REQUIRED];
  string preset_id = 2 [(google.api.field_behavior) = REQUIRED];
  Preset preset = 3 [(google.api.field_behavior) = REQUIRED];
}
message UpdatePresetRequest { Preset preset = 1; google.protobuf.FieldMask update_mask = 2; }
// GetPresetRequest/ListPresetsRequest/DeletePresetRequest：name/parent + 标准分页
message Model { string id = 1; int32 context_window = 2; }
message ListModelsRequest {}
message ListModelsResponse { repeated Model models = 1; }

// ChatEvent 扩展（049 事件集延续 + 新 oneof 分支）
message ToolResultEvent {
  string tool_id = 1;                            // 与 block_start/tool call 的 join 键
  ToolStatus status = 2;                         // SUCCEEDED | FAILED
  string result = 3;                             // 渲染结果文本（棋盘/错误文本）
}
message ChatEvent { /* 049 字段不变 */ ToolResultEvent tool_result = 16; }
```

TS 类型经 `ts_proto_library` 重生成；Go 侧 `agent_v2_go_proto`（grpc + grpc-gateway + go_gen_aip）重生成——`projects/game/BUILD.bazel:51-93` 既有 target 演进。

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

### 2.5 preset CRUD（FR-005）

标准 AIP 语义：Create（caller-supplied id、template 校验、重复 `ALREADY_EXISTS`）、Get/List（keyset 分页可选，个人规模默认 100）、Update（FieldMask `player_prompt`）、Delete（无联动）。持久化 Mongo `game_agent_v2.presets`，重启不丢。

### 2.6 ListModels

只读目录（`Model{id, context_window}`），源 = 组合配置 `llm-glm models[]` 经 `ctx.llm.listModels` 导出；响应不含任何端点/token 信息（SC-006）。

## 3. 两跳错误表（gateway → proxy → agent-v2，049 §2.1 延续 + 变更）

| 故障点 | gRPC / HTTP |
|---|---|
| gateway→proxy 不可达 | UNAVAILABLE / 503 |
| owner store（Mongo）故障 | INTERNAL / 500 |
| 无 agent-v2 活实例（任意 RPC） | UNAVAILABLE / 503 |
| owner 实例离线 / 流打开失败 | UNAVAILABLE / 503 |
| Send 无 owner | NOT_FOUND / 404 |
| Send 有 owner 未物化 | FAILED_PRECONDITION / 400 |
| UpdateAgent preset 缺失 / model 未知 | NOT_FOUND 404 / INVALID_ARGUMENT 400 |
| agent-v2 其余请求级错误 | 原样透传（049 propagateAgentError 语义） |
| 回合内错误（模型/工具失败） | 乘 `turn_end{ERROR}` 或工具错误结果，HTTP 仍 200 |

## 4. 路由拓扑（托管面不变）

agent-v2 stateful、仅经 proxy owner 亲和可达（无 http 块，049 D4）；gateway 在既有 proxy conn 上注册 `AgentService` HTTP 面（`/api/v2/` 子树）。proxy 侧分配语义变更见 [data-model.md](../data-model.md) §2.9（UpdateAgent = 新分配点；Send 只查不分配；preset/models 无亲和）。

## 5. 消费方契约

- **web**：`api/agent.ts` 全方法消费；`Send`/`ListAgentMessages` 替代 049 `:history`；删除编排不再调 dispose（见 [web-frontend.md](web-frontend.md)）。
- **desktop**：不消费 AgentService（对话/物化均不在 desktop 面）。
- **测试**：`agent_v2_conversation_test`（更名后回归）+ `agent_v2_preset_test`（新，US2 全场景断言）。

## 6. 实现期必读（间接引用显式列出）

- 049 契约基线：`specs/049-agent-v2-dsh-init/contracts/conversation-api.md`（事件序 §3、dsh→ChatEvent 映射 §4、两跳错误 §2.1——延续部分）
- AIP 规范（Google, 仓库外）：AIP-131 Get https://google.aip.dev/131 、AIP-132 List https://google.aip.dev/132 、AIP-133 Create https://google.aip.dev/133 、AIP-134 Update https://google.aip.dev/134 、AIP-135 Delete https://google.aip.dev/135 、AIP-136 自定义方法 https://google.aip.dev/136 、AIP-156 单例资源 https://google.aip.dev/156
- 仓库 API 风格：`style/api.md`
- v1 语义参照：`projects/game/game.proto`（Team/TeamProfile 的 AIP 形状先例）、`projects/game/agent/src/handler.ts:101-280`（UpdateTeam/GetTeam/RefreshTeam 的错误映射先例）
