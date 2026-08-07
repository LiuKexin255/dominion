# Data Model: Team 单例 AIP-156 一致化

**Feature**: `040-team-singleton-conformance` | **Spec**: [spec.md](spec.md) | **Contract**: [contracts/api-contract.md](contracts/api-contract.md)

> proto 资源/消息层面变更（AIP 风格）。字段号为实现时 protoc 流程确定并保留 reserved 卫生；此处给语义与关键字段。本特性 supersede [`specs/031-team-template-mode/data-model.md`](../031-team-template-mode/data-model.md) §Team 生命周期。

---

## 1. 资源层级（pattern，不变）

| 资源 | pattern | 说明 |
|---|---|---|
| **Session** | `templates/{template}/sessions/{session}` | 父，属 SessionService（跨服务父，不变） |
| **Team**（单例） | `templates/{template}/sessions/{session}/team` | 单数静态段（AIP-156 合规，[research.md](research.md) §R4）；属 TeamService |
| **Message** | `templates/{template}/sessions/{session}/team/agents/{agent}/messages/{message}` | 不变 |
| **TeamProfile** | `templates/{template}/profiles/{profile}` | 被 Team.profile 引用，属 PromptService（不变） |

---

## 2. Team 消息（变更：新增 `profile` 字段）

```proto
message Team {
  option (google.api.resource) = {
    type: "game.liukexin.com/Team"
    pattern: "templates/{template}/sessions/{session}/team"   // 单数静态段，不变（research.md §R4）
    plural: "teams"                                            // 声明保留（AIP-156 要求）；pattern 不用
    singular: "team"
  };

  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  // 新增：Team 当前所基于的 TeamProfile 全名（可变更，FR-004）。
  // 物化时由 UpdateTeam.team.profile 设置；变更经 UpdateTeam 触发 graph 重建（FR-005）。
  string profile = 2 [(google.api.resource_reference) = {
    type: "game.liukexin.com/TeamProfile"
  }];
  repeated TeamAgent agents = 3 [(google.api.field_behavior) = OUTPUT_ONLY]; // 模板 schema 派生，不变
  google.protobuf.Timestamp create_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];
}
```

**变更说明**：
- 新增 `profile`（field 2）：Team 当前所基于的 TeamProfile 资源全名。原 `CreateTeamRequest.profile` 的语义迁移至此。
- `agents` field 号 2→3；`create_time` 3→4（clean break，本特性为契约重构，无线上存量 Team 资源需兼容）。
- `TeamAgent`（`projects/game/game.proto:216-222`）不变。

---

## 3. UpdateTeamRequest 消息（新增）

```proto
message UpdateTeamRequest {
  // 目标 Team。name 定位单例（templates/{template}/sessions/{session}/team）；
  // profile 为可变体（设置/变更所基于的 TeamProfile）。
  Team team = 1 [(google.api.field_behavior) = REQUIRED];
  google.protobuf.FieldMask update_mask = 2;
  // 不存在则创建——单例物化点（AIP-134 create-or-update，research.md §R3）。
  bool allow_missing = 3;
}
```

**字段语义**：
- `team.name`：定位目标单例（AIP-134 Update 以资源 name 为标识）。
- `team.profile`：物化或变更时携带的 TeamProfile 全名。
- `update_mask`：AIP-134 标准；可变字段仅 `profile`。`allow_missing=true` 物化时 mask 被忽略（全字段应用）。
- `allow_missing`：true 时 upsert（缺失创建/存在更新）；false 时标准 Update（缺失 → NOT_FOUND，AIP-134）。

**移除**：`CreateTeamRequest`（原 `game.proto:770-789`）整体删除。

---

## 4. Team 生命周期状态（变更）

```text
[未物化] --UpdateTeam(allow_missing=true, profile=P)--> [已物化·profile=P]
                                                              |
                                              UpdateTeam(profile=P') (重建)
                                                              v
                                                        [已物化·profile=P']
```

| 状态 | GetTeam | Connect/ListMessages/RefreshTeam | UpdateTeam |
|---|---|---|---|
| **未物化** | NOT_FOUND（FR-003） | NOT_FOUND | `allow_missing=true` → 物化；`allow_missing=false` → NOT_FOUND |
| **已物化·profile=P** | 返回 Team（profile=P） | 正常 | 同 profile → 幂等返回；异 profile → 重建（FR-005） |

**不变量**：
- `GetTeam`/`Connect`/`ListMessages`/`RefreshTeam` **无懒加载物化**（FR-003，保留 031 既定不变量）。
- 物化仅在 `UpdateTeam` 路径发生（唯一物化点，取代原"唯一创建点"语义）。

---

## 5. owner 分配模型（不变）

- TeamService（proxy）侧 owner 分配（`projects/game/proxy/runtime/mongo/owner_store.go`）**不变**：复合键 `(template_id, session_id)`；get-or-create + duplicate-key → `ErrOwnerAlreadyExists` → 重读胜者。
- `assignOwner`（`proxy/handler/handler.go:274-348`）从 CreateTeam 搬入 UpdateTeam，**始终被 UpdateTeam 调用**（get-or-create 路由解析，唯一 owner 分配点）；proxy **不解读 `allow_missing`**（allow_missing 是 Team 资源语义，由 agent 处理物化/NOT_FOUND——proxy 为路由层，仅负责 owner 路由）。`assignOwner`/`lookupOwner` 逻辑本身**不变**。
- `lookupOwner`（Connect/GetTeam/ListMessages/RefreshTeam）不变。

---

## 6. team graph checkpointer（重建复用，[research.md](research.md) §R7）

- `TeamGraphHandle.checkpointer: MemorySaver`（`projects/game/agent/src/team/graph.ts:146`）public 暴露。
- 首建：`buildTeamGraph` 内部 `new MemorySaver()`（`graph.ts:241`）。
- 重建（profile 变更）：注入既有 checkpointer（`buildTeamGraph` 增可选 `checkpointer` 入参）。同一模块私有 `TeamState` schema + 同一 `thread_id=sessionId` → 通道正确重建（详见 [contracts/team-rebuild-contract.md](contracts/team-rebuild-contract.md)）。

---

## 7. 验证要点

- Team 无 CreateTeam RPC（AIP-156 合规，FR-001）；UpdateTeam 为唯一物化点。
- `Team.profile` 为可变字段，GetTeam/UpdateTeam 响应携带（FR-004）。
- profile 模板段与 team.name 模板段一致性由 handler 校验（FR-008，禁潜规则）。
- Team 配置路径无 ALREADY_EXISTS（FR-007，偏离消除）。
