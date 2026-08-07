# Contract: API（TeamService proto 契约）

**Feature**: `040-team-singleton-conformance` | **Spec**: [spec.md](spec.md) | **Research**: R1/R3/R6/R10

> AIP 风格（https://google.aip.dev）gRPC API 契约。本特性 supersede [`specs/031-team-template-mode/contracts/api-contract.md`](../../031-team-template-mode/contracts/api-contract.md) §2.2 的 `CreateTeam`：移除 `CreateTeam`，新增 `UpdateTeam`（[AIP-134 create-or-update](https://google.aip.dev/134#create-or-update)）；Team 成为 [AIP-156](https://google.aip.dev/156) 合规单例。`GetTeam`/`Connect`/`ListMessages`/`RefreshTeam` **不变**（含其 NOT_FOUND 不变量）。

---

## 1. TeamService（`/api/v1/templates/{template}/sessions/{session}/...`）

| RPC | 方法/路径 | 说明 |
|---|---|---|
| `UpdateTeam` | PATCH `/api/v1/{team.name=templates/*/sessions/*/team}`（body `team`） | **取代 CreateTeam**。唯一物化点（AIP-134 + AIP-156，FR-001/FR-002）。`allow_missing=true` 缺失则物化、存在则更新（profile 变更触发 graph 重建，FR-005）。详见 §2 |
| `GetTeam` | GET `/api/v1/{name=templates/*/sessions/*/team}` | AIP-131（不变）。未物化 → NOT_FOUND（FR-003）。响应含 `profile`（FR-004） |
| `Connect` | bidi stream（无 REST） | 不变。未物化 → NOT_FOUND（stream error 通道） |
| `ListMessages` | GET `/api/v1/{parent=templates/*/sessions/*/team/agents/*}/messages` | AIP-132（不变）。未物化 → NOT_FOUND |
| `RefreshTeam` | POST `/api/v1/{name=templates/*/sessions/*/team}:refresh` | AIP-136（不变）。未物化 → NOT_FOUND |

---

## 2. UpdateTeam（核心变更）

### 2.1 RPC 定义

```proto
service TeamService {
  // UpdateTeam is the singleton's materialization + mutation point (AIP-134
  // create-or-update + AIP-156 singleton; replaces the former CreateTeam).
  // allow_missing=true materializes the per-session Team on first call;
  // repeated calls are idempotent (same profile → existing Team returned).
  // A different profile rebuilds the team graph (state preserved, see
  // team-rebuild-contract.md). The Team is a true AIP-156 singleton: there is
  // no Create RPC.
  rpc UpdateTeam(UpdateTeamRequest) returns (Team) {
    option (google.api.http) = {
      patch: "/api/v1/{team.name=templates/*/sessions/*/team}"
      body: "team"
    };
    option (google.api.method_signature) = "team,update_mask";
  }
  // GetTeam / Connect / ListMessages / RefreshTeam — 不变（见 §1）
}
```

### 2.2 请求消息

```proto
message UpdateTeamRequest {
  Team team = 1 [(google.api.field_behavior) = REQUIRED];     // name 定位单例；profile 为可变体
  google.protobuf.FieldMask update_mask = 2;
  bool allow_missing = 3;                                      // 单例物化点
}
```

- REST 绑定：`PATCH /api/v1/templates/{template}/sessions/{session}/team`，body 为 `team`（含 `name` + `profile`）；`update_mask`、`allow_missing` 作为 query 参数（grpc-gateway 将 body 外字段提至 query）。

### 2.3 行为矩阵

| 既有 Team？ | allow_missing | team.profile vs 既有 | 结果 |
|---|---|---|---|
| 否 | true | P | 物化（创建，profile=P）；响应 Team(profile=P) |
| 否 | false | * | NOT_FOUND（AIP-134 标准 Update） |
| 是 | * | = 既有 P | 幂等返回既有 Team（无变更，FR-002） |
| 是 | * | ≠ 既有 P | 重建 graph（FR-005，详见 rebuild 契约）；响应 Team(profile=P') |
| 是 | * | 未设置/空 | INVALID_ARGUMENT（profile 必填） |

### 2.4 校验

- `team.name` 解析失败 → INVALID_ARGUMENT。
- profile 模板段 ≠ `team.name` 模板段 → INVALID_ARGUMENT（FR-008，禁潜规则）。
- profile 引用不存在的 TeamProfile → NOT_FOUND（透传 PromptService，FR-009）。
- turn in-flight 时 profile 变更 → FAILED_PRECONDITION（FR-006，复用 `isRunning()` 守卫）。
- 重建失败（如新模型不可用）→ 既有 Team 保持不变，返回错误（不留半重建状态）。

### 2.5 并发与 owner

- proxy 侧 `assignOwner`（`projects/game/proxy/handler/handler.go:274-348`）从 CreateTeam 搬入 UpdateTeam，**唯一 owner 分配点**不变：get-or-create + `ErrOwnerAlreadyExists` 竞态重读胜者（[research.md](../research.md) §R10）。`UpdateTeam` **始终调用 `assignOwner`**（路由解析），**不 inspect `allow_missing`**——proxy 是路由层，allow_missing 是 Team 资源语义，由 agent `SessionTeamStore.update`（`projects/game/agent/src/session-team.ts`）处理（缺失+true→物化、缺失+false→NOT_FOUND、既有+同 profile→幂等、既有+异 profile→重建）。§2.3 行为矩阵描述 proxy+agent 合并行为：`allow_missing=false`+未物化的 NOT_FOUND 由 agent 层返回，proxy 透传（[AIP-134 create-or-update](https://google.aip.dev/134#create-or-update)）。
- 多个并发 `UpdateTeam(allow_missing=true)` 针对同一未物化会话：owner 分配收敛于胜者；agent 侧 `SessionTeamStore` 单飞（`pending` map）保证 graph 仅构建一次。
- **Team 配置路径不外泄 ALREADY_EXISTS**（FR-007，[research.md](../research.md) §R6 偏离消除）。

---

## 3. 移除清单（clean break）

- `rpc CreateTeam(CreateTeamRequest) returns (Team)`（原 `game.proto:71-77`）。
- `message CreateTeamRequest`（原 `game.proto:770-789`）。
- `TeamAlreadyExistsError`（agent `session-team.ts:487-499`）及其 → ALREADY_EXISTS 映射（agent `handler.ts:131-157`）。
- 031 契约 §2.2 的"幂等 create 注（AIP-133 偏离）"（`specs/031-team-template-mode/contracts/api-contract.md:46`）。
- desktop 的 create-if-missing 两步（GetTeam→dialog→createTeam → 改为单次 update，见 [../research.md](../research.md) R9）。

---

## 4. 保留不变

- `GetTeam`/`Connect`/`ListMessages`/`RefreshTeam` 的语义、HTTP/gRPC 绑定、NOT_FOUND 不变量。
- owner store（Mongo）、`assignOwner`/`lookupOwner` 逻辑、picker。
- `Message`/`TeamProfile`/`SaoleiProfile`/Part 模型（spec 023/025/030/035 既定）。
- Team 资源 pattern `templates/{template}/sessions/{session}/team`（单数静态段，[research.md](../research.md) §R4）。

---

## 5. 验证要点

- TeamService 无 CreateTeam RPC；UpdateTeam 为唯一物化点（FR-001）。
- `allow_missing=true` 物化 + 幂等（FR-002）；同 profile 幂等、异 profile 重建（FR-005）。
- GetTeam 未物化 NOT_FOUND（FR-003）；响应含 profile（FR-004）。
- 配置路径无 ALREADY_EXISTS（FR-007）；profile 模板段一致性校验（FR-008）。
