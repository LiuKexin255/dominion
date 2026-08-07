# Contract: MemoryService（grpc-go，planner 长期记忆）

**Feature**: `039-planner-memory-calibration` | **Spec**: [`spec.md`](../spec.md) | **Research**: D1/D2/D9

> 新建 grpc-go `MemoryService`，承载 planner 长期记忆条目（Memory 资源 CRUD）。AIP 风格（`style/api.md`，https://google.aip.dev）。存储：MongoDB **独立数据库 `game_memory`**（`style/mongo.md`，MUST NOT 与 agent/prompt 库混用）。服务结构仿 `projects/game/prompt/`（cmd/handler/domain/runtime/mongo）。

---

## 1. 资源（proto，新增于 `projects/game/game.proto`）

### 1.1 Memory 资源消息

```proto
message Memory {
  option (google.api.resource) = {
    type: "game.liukexin.com/Memory"
    pattern: "templates/{template}/sessions/{session}/memories/{memory}"
    singular: "memory"
    plural: "memories"
  };
  string name = 1 [(Identify)];
  string memory_id = 2 [OutputOnly];
  string content = 3;
  google.protobuf.Timestamp create_time = 4 [OutputOnly];
  google.protobuf.Timestamp update_time = 5 [OutputOnly];
}
```

- pattern `templates/{template}/sessions/{session}/memories/{memory}`（FR-012，集合段复数 `memories`）。`{memory}` = LLM 提供的 `memory_id`（FR-008）。
- `google.api.resource` 注解驱动 `protoc-gen-go-aip` codegen（`ParseMemoryName`/`MemoryName`/parent 解析，同 031 §5，无需手写）。

### 1.2 字段语义

| 字段 | 行为 | 说明 |
|---|---|---|
| `name` | IDENTIFIER | 完整资源名；OUTPUT 派生 |
| `memory_id` | OUTPUT_ONLY | `{memory}` 段；CreateMemory 请求的 `memory_id` 派生 |
| `content` | REQUIRED（Create/Update） | 记忆内容文本 |
| `create_time`/`update_time` | OUTPUT_ONLY | 服务端管理 |

---

## 2. 服务与 RPC（`MemoryService`）

```proto
// MemoryService 承载 planner 长期记忆（spec 039 FR-006）。
// Prefix Path: /api/v1/templates/{template}/sessions/{session}/memories
service MemoryService {
  rpc CreateMemory(CreateMemoryRequest) returns (Memory) {
    option (google.api.http) = {
      post: "/api/v1/{parent=templates/*/sessions/*}/memories"
      body: "*"
    };
  }
  rpc UpdateMemory(UpdateMemoryRequest) returns (Memory) {
    option (google.api.http) = {
      patch: "/api/v1/{memory.name=templates/*/sessions/*/memories/*}"
      body: "memory"
    };
  }
  rpc DeleteMemory(DeleteMemoryRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      delete: "/api/v1/{name=templates/*/sessions/*/memories/*}"
    };
  }
  rpc ListMemories(ListMemoriesRequest) returns (ListMemoriesResponse) {
    option (google.api.http) = {
      get: "/api/v1/{parent=templates/*/sessions/*}/memories"
    };
  }
}
```

| RPC | 方法/路径 | 说明 |
|---|---|---|
| `CreateMemory` | POST `/api/v1/{parent=templates/*/sessions/*}/memories`（body `"*"`） | AIP-133；请求 `{ parent, memory_id, content }`；`memory_id` 已存在 → `ALREADY_EXISTS`（FR-008 冲突拒绝） |
| `UpdateMemory` | PATCH `/api/v1/{memory.name=...}`（body `memory`） | AIP-134；请求 `{ memory: {name, content} }`；不存在 → `NOT_FOUND` |
| `DeleteMemory` | DELETE `/api/v1/{name=...}` | AIP-135；不存在 → `NOT_FOUND` |
| `ListMemories` | GET `/api/v1/{parent=templates/*/sessions/*}/memories` | AIP-132 + AIP-158 分页（`page_size`/`page_token`/`next_page_token`）；返回该 session 一页 memory（agent 烘焙冻结快照用，非 LLM 工具） |

> `ListMemories` **MUST 支持分页**（AIP-158，`style/api.md` 引用 https://google.aip.dev/158）：请求 `page_size`（默认/上限见下）+ `page_token`，响应 `next_page_token`。冻结快照烘焙时按页遍历至 `next_page_token` 为空（planner 长期记忆为有界集合，实际多为一页，但契约须符合 AIP-132/158）。分页字段名、默认 page_size、上限对齐 prompt 服务 `ListTeamProfiles`（`projects/game/prompt/domain/model.go` `DefaultListTeamProfilesPageSize=100`/`MaxListTeamProfilesPageSize=1000`）。
>
> **记忆条目上限（决策，落实 spec 边案例"由 plan 决定"）**：v1 **不设硬上限值**——"有界集合"约束由冻结快照单页烘焙的实际规模保证（上限即分页常量 100/1000 隐含边界）；达上限报错让 LLM 自行 consolidate 的策略**缓行**（后续优化，不阻塞本特性）。服务端对记忆数量不做额外限制。

### 请求消息（语义）

```proto
message CreateMemoryRequest {
  string parent = 1 [(Required)];          // session 资源名 templates/{t}/sessions/{s}
  string memory_id = 2 [(Required)];       // LLM 提供（FR-008）；字符集 [a-z0-9_-]+（plan 落实）
  string content = 3 [(Required)];
}
message UpdateMemoryRequest {
  Memory memory = 1 [(Required)];          // name + content
}
message DeleteMemoryRequest {
  string name = 1 [(Required)];
}
message ListMemoriesRequest {
  string parent = 1 [(Required)];          // session 资源名
  int32 page_size = 2;                     // AIP-158
  string page_token = 3;                   // AIP-158
}
message ListMemoriesResponse {
  repeated Memory memories = 1;
  string next_page_token = 2;              // AIP-158
}
```

---

## 3. mongo 文档形状（`memories` 集合，`game_memory` 库）

```text
{
  _id: ObjectId,                       // 自动生成，不覆盖（style/mongo.md）
  template: string,                    // {template} 段
  session_id: string,                  // {session} 段
  memory_id: string,                   // {memory} 段（LLM 提供）
  content: string,
  create_time: ISODate,
  update_time: ISODate
}
```

- 唯一索引：`(template, session_id, memory_id)`（复合，唯一）。
- 集合归属 `game_memory` 数据库（memory 服务专属，`style/mongo.md`）。
- 对象字段不用 `bson.M`，定义具体 model（`style/mongo.md`）。

---

## 4. 服务实现（仿 `projects/game/prompt/`）

```text
projects/game/memory/
├── cmd/main.go            // mongo.NewClient("game/mongo") + db "game_memory" + RegisterMemoryServiceServer
├── handler/handler.go     // MemoryService handler（gRPC framework 逻辑）
├── domain/
│   ├── model.go           // Memory 领域模型（对象语义→指针，style/golang.md）
│   ├── repository.go      // MemoryRepository 接口（Create/Update/Delete/List）
│   └── errors.go          // ErrAlreadyExists/ErrNotFound
├── runtime/mongo/
│   ├── repository.go      // mongo 仓储（仿 prompt/runtime/mongo/repository.go）
│   └── model.go           // memoryDocument（bson）
└── service.yaml           // 部署描述
```

- `mongo.NewClient("game/mongo")`（与 prompt 服务同款，`dominion/common/gopkg/mongo`）。
- `NewRepository(client, "game_memory")`（独立 db，仿 prompt 的 `NewRepository(client, "game_prompt")`）。
- codegen：`protoc-gen-go-aip` 生成 `ParseMemoryName` 等（资源名解析全由 codegen）。

---

## 5. 部署与发现

- `projects/game/deploy.yaml`：新增 `memory` 服务条目（artifact `//projects/game/memory/service.yaml`，与 prompt 平级）。
- `projects/game/pkg/gameconst/const.go`：新增 `MemoryTarget = "game/memory:grpc"`（仿 `PromptTarget`）。
- agent TS client 目标：`dominion:///game/memory:50051`（`memory-client.ts`，仿 `prompt-client.ts`）。

---

## 6. 错误码

| 场景 | gRPC code | 说明 |
|---|---|---|
| `CreateMemory` memory_id 已存在 | `ALREADY_EXISTS`（AIP-133） | FR-008 冲突拒绝 |
| `UpdateMemory`/`DeleteMemory` 不存在 | `NOT_FOUND`（AIP-131） | |
| `memory_id` 字符集非法 | `INVALID_ARGUMENT`（AIP-193） | plan 落实字符集校验 |

---

## 7. 验证要点

- Memory 资源 pattern 为 `templates/{template}/sessions/{session}/memories/{memory}`；codegen 生成 `ParseMemoryName`。
- CreateMemory memory_id 重复 → ALREADY_EXISTS；Update/Delete 不存在 → NOT_FOUND。
- 数据库 `game_memory` 独立于 agent/prompt 的库（`style/mongo.md` 审查）。
- 服务结构仿 prompt（cmd/handler/domain/runtime/mongo），用 `bootstrap`/`grpc`/`mongo`/`otel`。
- ListMemories 返回 session 全部 memory（供冻结快照烘焙）。
