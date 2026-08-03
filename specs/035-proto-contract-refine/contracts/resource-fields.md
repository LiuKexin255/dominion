# Contract: 资源字段移除 — Session / TeamProfile

**Spec**: [spec.md](../spec.md) FR-001~008 | **Data Model**: [data-model.md](../data-model.md) §1.1~1.2

---

## 1. 移除的字段

### 1.1 Session

| field | 原定义 | 移除后 |
|-------|--------|--------|
| `template` (field 2) | OUTPUT_ONLY, `resource_reference` → Template | 删除；template 信息仅由 `name` pattern `templates/{template}/sessions/{session}` 的路径段承载 |
| `session_id` (field 3) | OUTPUT_ONLY | 删除；session ID 仅由 `name` 的路径段承载 |

proto 字段编号 reserved（clean break hygiene）：
```proto
reserved 2, 3;
reserved "template", "session_id";
```

### 1.2 TeamProfile

| field | 原定义 | 移除后 |
|-------|--------|--------|
| `template` (field 2) | REQUIRED, `resource_reference` → Template | 删除；template 信息仅由 `name` pattern `templates/{template}/profiles/{profile}` 的路径段承载 |

proto 字段编号 reserved：
```proto
reserved 2;
reserved "template";
```

---

## 2. 行为变更

### 2.1 CreateSession

**请求**：`CreateSessionRequest.parent`（URL 路径段）是 template 的唯一权威来源。`CreateSessionRequest.session_id`（请求字段，非资源字段）保留不变——它是 AIP-133 的 `{resource}_id`。

**响应**：返回的 `Session` 资源不再包含 `template` 和 `session_id` 字段。客户端需要 session ID 时从 `Session.name` 解析。

### 2.2 GetSession / ListSessions

响应中的 `Session` 不再包含 `template` 和 `session_id`。

### 2.3 CreateTeamProfile

**请求**：`CreateTeamProfileRequest.parent`（URL 路径段）是 template 的唯一权威来源。客户端**不应**在 `TeamProfile` 资源体中设置 `template` 字段（字段已删除）。

**校验变更**：
- 原 `validateTeamProfileBody` 读取 `tp.GetTemplate()` 做 double-check（`prompt/handler/handler.go:209-215`）→ **删除**。
- oneof spec 一致性校验（`validateSpecConsistency`）的数据源从 `bodyTpl`（资源体 template）改为 `parent`（请求 parent 路径段）。

### 2.4 UpdateTeamProfile

**请求**：客户端不再需要在资源体中设置 `template`。原 `UpdateTeamProfile` handler（`handler.go:157-162`）中 body template 一致性校验 → **删除**。template 由资源名称路径段推导。

### 2.5 GetTeamProfile / ListTeamProfiles / DeleteTeamProfile

响应中的 `TeamProfile` 不再包含 `template`。

---

## 3. 客户端适配

### 3.1 Go desktop — sessionViewFromProto

`desktop/view_model.go:121-131` 的 `sessionViewFromProto`：
- 删除 `Template: s.GetTemplate()` 赋值。
- `SessionID` 改为从 `game.ParseSessionName(s.GetName()).SessionID` 派生（复用 `teamViewFromProto` 模式，`view_model.go:162-166`）。
- `SessionView.Template` 字段删除。
- Wails JSON 的 `sessionId` 字段形状不变 → 前端零改动。

### 3.2 Go desktop — TeamProfile 构造

`desktop/app.go:1283-1287`（CreateTeamProfile）和 `app.go:1430-1434`（UpdateTeamProfile）：
- 删除 `Template: game.TemplateName{TemplateID: template}.String()` 赋值。
- `TeamProfileView.Template` 字段删除。

### 3.3 前端 api.ts

- `Session` 接口（`api.ts:39-44`）：删除 `template` 字段；`sessionId` 保留（由 Go view-model 派生，JSON 形状不变）。
- `TeamProfile` 接口（`api.ts:325-340`）：删除 `template` 字段。

### 3.4 前端组件

- `App.svelte`：不使用 `session.template`（已确认），不使用 `profile.template`（已确认）。无需改动。
- `SessionList.svelte`：使用 `session.sessionId`（由 view-model 派生，不变）。无需改动。
- `ProfileManagement.svelte`：`template` 是组件 prop（来自顶层状态），非 `profile.template`。无需改动。

---

## 4. Domain / Storage 层（不变）

`domain.Session.Template`、`domain.Session.SessionID`、`domain.TeamProfile.Template` 及对应的 Mongo BSON 字段**保留不动**。它们是内部存储身份/过滤键，不受 proto 契约约束。

参考：`session/domain/model.go:10-18`、`session/runtime/mongo/model.go:19-23`、`prompt/domain/model.go:11-35`、`prompt/runtime/mongo/model.go:28-35`。
