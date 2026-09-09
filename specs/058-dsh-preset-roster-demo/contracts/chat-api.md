# Contract: Chat API（会话面与 preset 配置面）

> HTTP/gRPC 契约：`experimental/dsh/demo/chat.proto`（`google.api.http` 注解经 gateway 透出）。
> 资源语义对齐 agent_v2（`specs/051-agent-v2-dsh-migration/contracts/agent-api.md`）；前缀路径 `/experimental/dsh-demo` 不变。
> 错误码映射：gRPC status（AIP-193 语义：`INVALID_ARGUMENT`/`ALREADY_EXISTS`=已存在资源/`NOT_FOUND`/`FAILED_PRECONDITION`）。

## 1. Chat 服务（会话面）

### 1.1 CreateConversation（新增，FR-002/R4）

```proto
rpc CreateConversation(CreateConversationRequest) returns (Conversation) {
  option (google.api.http) = { post: "/experimental/dsh-demo/conversations" body: "*" };
}
```

| 字段 | 类型 | 语义 |
|---|---|---|
| request.conversation_id | string | 资源 id（AIP-133 user-specified id）；非法字符/保留字 → `INVALID_ARGUMENT` |
| request.preset | string | 可选；模板 id 或创作副本 id（R5）；缺省 = roster `default`（V1-2） |

响应 `Conversation`：`name`（`conversations/{id}`）、`preset`（**resolved** id）、`create_time`。

**幂等/重建**（R4）：同 id 同 preset → 幂等返回现有视图；同 id 异 preset → dispose 旧 agent 后按新 preset 重建（在途 round 失败返回）；引用不可 resolve 的 preset → `INVALID_ARGUMENT`（错误透传 roster resolve 的可用集合信息）。

### 1.2 SendMessage（不变，语义收紧）

未 CreateConversation 的会话 → `FAILED_PRECONDITION`（"conversation not created; call CreateConversation first"，FR-002；047 的首条消息 lazy 创建语义移除）。

## 2. PresetService（新增，FR-004；无状态配置面）

```proto
service PresetService {
  rpc CreatePreset(CreatePresetRequest) returns (Preset);
  rpc GetPreset(GetPresetRequest) returns (Preset);
  rpc ListPresets(ListPresetsRequest) returns (ListPresetsResponse);
  rpc UpdatePreset(UpdatePresetRequest) returns (Preset);
  rpc DeletePreset(DeletePresetRequest) returns (Empty);
}
```

HTTP 注解（AIP 风格，与 agent_v2 PresetService 同型）：

| RPC | HTTP | 路径 |
|---|---|---|
| CreatePreset | POST | `/experimental/dsh-demo/presets`（body: `"*"`；`preset_id` 经 body 携带——grpc-gateway 对 `body: "*"` 不解析 query string，AIP-127） |
| GetPreset | GET | `/experimental/dsh-demo/{name=presets/*}` |
| ListPresets | GET | `/experimental/dsh-demo/presets`（demo 规模：无分页参数，全量返回） |
| UpdatePreset | PATCH | `/experimental/dsh-demo/{name=presets/*}`（body 携带 `update_mask`，AIP-134） |
| DeletePreset | DELETE | `/experimental/dsh-demo/{name=presets/*}` |

**Preset 资源**：`name`（`presets/{id}`）、`template`、`persona`、`display_name`、`create_time`、`update_time`。

| RPC | 字段约束 | 错误 |
|---|---|---|
| CreatePreset | `preset_id` 必填（`[a-z0-9][a-z0-9-]*`）；`template` 必填（必须可 resolve）；`persona` 必填非空；`display_name` 可选 | 重复 id → `ALREADY_EXISTS`；未知模板/非法 id → `INVALID_ARGUMENT` |
| UpdatePreset | `update_mask` 限 `persona`/`display_name` | 未知 id → `NOT_FOUND`；空 mask → `INVALID_ARGUMENT` |
| DeletePreset | — | 未知 id → `NOT_FOUND`；**模板不可删**（roster `remove()` 拒绝 system trust，透传其错误 → `FAILED_PRECONDITION`） |

**语义**（data-model §2）：Create/Update/Delete 触发物化（副本文件），Store 与目录同生同灭；Update persona → 新会话新组合、旧会话不变（V3-2）；Delete → 已加入会话不受影响（V4-3）。

## 3. Gateway 路由归属

- 会话面（Chat）：既有链路（gateway → agent）不变。
- 配置面（PresetService）：demo 单实例、无 proxy 亲和需求——gateway 直连 agent（与 047 拓扑一致；agent_v2 的两跳拆分是 stateful 亲和需求，demo 无此需求，不引入）。
