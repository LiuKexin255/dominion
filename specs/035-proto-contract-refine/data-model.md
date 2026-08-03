# Data Model: Proto 契约修正

**Spec**: [spec.md](spec.md) | **Research**: [research.md](research.md)

---

## 1. 变更后 proto 消息定义

### 1.1 Session（移除 template + session_id）

```proto
message Session {
  option (google.api.resource) = {
    type: "game.liukexin.com/Session"
    pattern: "templates/{template}/sessions/{session}"
    plural: "sessions"
    singular: "session"
  };

  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  // template 和 session_id 均由 name 路径段承载（AIP-124）。
  // field 2 (template)、field 3 (session_id) removed (clean break).
  reserved 2, 3;
  reserved "template", "session_id";

  google.protobuf.Timestamp create_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];
}
```

**移除的字段**：
| 原 field | 原类型 | 原行为 | 移除后数据来源 |
|----------|--------|--------|----------------|
| `template` (2) | string, OUTPUT_ONLY, resource_reference→Template | handler 从 parent 派生 | name 路径段 `templates/{template}/...` |
| `session_id` (3) | string, OUTPUT_ONLY | handler 从 domain.SessionID 设置 | name 路径段 `.../sessions/{session}` |

### 1.2 TeamProfile（移除 template）

```proto
message TeamProfile {
  option (google.api.resource) = {
    type: "game.liukexin.com/TeamProfile"
    pattern: "templates/{template}/profiles/{profile}"
    plural: "teamProfiles"
    singular: "teamProfile"
  };

  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  // template 由 name 路径段承载（AIP-124）；oneof spec 一致性校验数据源
  // 改为 name 路径段（原为 body template 字段）。
  // field 2 (template) removed (clean break).
  reserved 2;
  reserved "template";

  google.protobuf.Timestamp create_time = 3 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp update_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];

  oneof spec {
    SaoleiProfile saolei = 10;
  }
}
```

**移除的字段**：
| 原 field | 原类型 | 原行为 | 移除后数据来源 |
|----------|--------|--------|----------------|
| `template` (2) | string, REQUIRED, resource_reference→Template | 客户端设置 + handler double-check 校验 | name 路径段 `templates/{template}/...` |

### 1.3 UserFrame（新增，替代 AgentFrame 入参）

```proto
message UserFrame {
  string session_id = 1 [(google.api.field_behavior) = REQUIRED];
  string template_id = 2 [(google.api.field_behavior) = REQUIRED];
  string agent = 3;

  oneof payload {
    MessageParts message_parts = 10;
    FlowParts flow_parts = 11;
  }
}
```

| field | 语义 | 设置方 | 消费方 |
|-------|------|--------|--------|
| `session_id` | 会话路由标识 | gateway 从 URL 注入（覆盖客户端值） | proxy 首帧路由；agent 会话查找 |
| `template_id` | 模板路由标识 | gateway 从 URL 注入 | proxy 首帧路由；agent 模板分发 |
| `agent` | 用户输入的目标 team agent | desktop（用户消息场景） | agent `resolveUserInputAgent`（handler.ts:420） |
| `message_parts` | 用户消息内容（文本/图片） | desktop `SendUserTurn` | agent turn loop |
| `flow_parts` | 操作结果回报（flow_result）、连接探活（status） | desktop `handleInboundOperation`/probe | agent handler / operation-bridge |

### 1.4 TeamFrame（新增，替代 AgentFrame 出参）

```proto
message TeamFrame {
  string session_id = 1 [(google.api.field_behavior) = REQUIRED];
  string template_id = 2 [(google.api.field_behavior) = REQUIRED];
  string frame_id = 3;
  google.protobuf.Timestamp create_time = 4;
  string agent = 5;
  MessageRole role = 6;

  oneof payload {
    MessageParts message_parts = 10;
    FlowParts flow_parts = 11;
  }
}
```

| field | 语义 | 设置方 | 消费方 |
|-------|------|--------|--------|
| `session_id` | 会话路由标识（回传） | agent buildTeamFrame | desktop（日志） |
| `template_id` | 模板路由标识（回传） | agent buildTeamFrame | desktop（透传） |
| `frame_id` | 帧唯一标识（去重） | agent buildTeamFrame（randomUUID） | 前端 `renderedMessageIds` 去重（App.svelte:685-686） |
| `create_time` | 帧时间戳 | agent buildTeamFrame（now） | 前端气泡时间戳（App.svelte:726） |
| `agent` | 产出帧的 team agent | agent buildTeamFrame | 前端分 tab 路由（App.svelte:552） |
| `role` | 消息角色（user/agent） | agent buildTeamFrame（实时恒 AGENT）；SeedFromHistory（从 Message.role 拷贝） | 前端气泡对齐（user 右 / agent 左） |
| `message_parts` | agent 显示内容 | agent turn-loop displayFrame | 前端渲染对话气泡 |
| `flow_parts` | 控制信号（wait/warn/status/queue）+ 操作请求（mouse/keyboard/flow_result） | agent handler/turn-loop/operation-bridge | desktop 信号处理 / 操作执行 |

### 1.5 MessageRole（新增枚举，替代 FrameSender）

```proto
enum MessageRole {
  MESSAGE_ROLE_UNSPECIFIED = 0;
  MESSAGE_ROLE_USER = 1;
  MESSAGE_ROLE_AGENT = 2;
}
```

| 值 | 语义 | 来源（ListMessages 推导） | 来源（live/seed） |
|----|------|--------------------------|-------------------|
| `UNSPECIFIED` | 默认值 | — | flowParts payload 时 |
| `USER` | 用户消息 | LangChain `human` 消息（handler.ts:585） | SeedFromHistory user 消息 |
| `AGENT` | agent 消息（含 tool） | LangChain `ai` + `tool` 消息（handler.ts:587-588，tool 统一为 AGENT） | 实时 messageParts 恒 AGENT；SeedFromHistory agent 消息 |

### 1.6 Message（sender → role）

```proto
message Message {
  option (google.api.resource) = { ... };  // pattern 不变

  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  string message_id = 2 [(google.api.field_behavior) = OUTPUT_ONLY];
  // FrameSender sender = 3 → MessageRole role = 3
  MessageRole role = 3;
  string agent = 4;
  google.protobuf.Timestamp create_time = 5;
  MessageParts content = 6;
}
```

### 1.7 移除的消息/枚举

| 消息/枚举 | 原定义位置 | 移除原因 |
|-----------|-----------|----------|
| `AgentFrame` | game.proto:589-621 | 拆分为 UserFrame + TeamFrame |
| `FrameSender` | game.proto:286-292 | 帧方向拆分后冗余；Message 改用 MessageRole |

### 1.8 Connect RPC 签名变更

```proto
rpc Connect(stream UserFrame) returns (stream TeamFrame);
```

无 HTTP binding（bidirectional streaming RPCs are not natively supported over HTTP/1.1，[AIP-127](https://google.aip.dev/127)）。

---

## 2. 不变的消息

以下消息**不修改**（本次需求不涉及）：

- `MessageParts` / `FlowParts` / `MessagePart` / `FlowPart` 及其所有子消息（TextPart, ThinkingPart, ImagePart, ToolCallPart, ToolResultPart, MouseMovePart, MouseClickPart, KeyboardPressPart, MouseMoveAndClickPart, FlowResultPart, WaitSignal, WarnSignal, StatusSignal, QueueSignal）
- 所有枚举（ImageEncoding, MouseClickAction, ToolResultStatus, MouseInputMethod, KeyboardKey, StatusSignalStatus, ServiceKind 等）
- `Team`, `TeamAgent`, `SaoleiProfile`, `Template`
- `CreateSessionRequest`, `ListSessionsRequest`, `GetSessionRequest`, `DeleteSessionRequest`
- `CreateTeamProfileRequest`, `ListTeamProfilesRequest`, `GetTeamProfileRequest`, `UpdateTeamProfileRequest`, `DeleteTeamProfileRequest`
- `CreateTeamRequest`, `GetTeamRequest`, `RefreshTeamRequest`
- `ListMessagesRequest`, `ListMessagesResponse`
- deploy.proto 全部（经调查确认无冗余字段）

---

## 3. Domain / Storage 层（不变）

以下 domain/storage 模型**保留原字段不动**（它们是内部存储模型，不受 proto 契约约束）：

### 3.1 session domain

```go
// projects/game/session/domain/model.go — 不变
type Session struct {
    Template   string  // 存储过滤键，保留
    SessionID  string  // 存储主键/分页键，保留
    CreateTime time.Time
}
```

### 3.2 session mongo

```go
// projects/game/session/runtime/mongo/model.go — 不变
type sessionDocument struct {
    Template   string    `bson:"template"`
    SessionID  string    `bson:"session_id"`
    CreateTime time.Time `bson:"create_time"`
}
```

### 3.3 prompt domain

```go
// projects/game/prompt/domain/model.go — 不变
type TeamProfile struct {
    TeamProfileName string
    Template        string  // 存储过滤键，保留
    SaoleiPlayerModel string
    // ...
}
```

---

## 4. 转换层变更（proto ↔ domain）

### 4.1 sessionToProto（移除字段赋值）

```go
// projects/game/session/handler/handler.go — 修改后
func sessionToProto(s *domain.Session) *game.Session {
    if s == nil {
        return nil
    }
    p := &game.Session{
        Name: game.SessionName{TemplateID: s.Template, SessionID: s.SessionID}.String(),
        // Template 和 SessionId 赋值删除
    }
    if !s.CreateTime.IsZero() {
        p.CreateTime = timestamppb.New(s.CreateTime)
    }
    return p
}
```

### 4.2 sessionViewFromProto（ParseSessionName 派生）

```go
// projects/game/desktop/view_model.go — 修改后
func sessionViewFromProto(s *game.Session) *SessionView {
    if s == nil {
        return nil
    }
    sessionID := ""
    if name, err := game.ParseSessionName(s.GetName()); err == nil {
        sessionID = name.SessionID
    }
    return &SessionView{
        Name:       s.GetName(),
        SessionID:  sessionID,  // 从 name 解析（替代 s.GetSessionId()）
        // Template 字段删除
        CreateTime: timestampString(s.GetCreateTime()),
    }
}
```

### 4.3 validateTeamProfileBody（移除 body template 校验）

```go
// projects/game/prompt/handler/handler.go — 修改后
func validateTeamProfileBody(tp *game.TeamProfile, parent game.TemplateName) error {
    if tp == nil {
        return status.Error(codes.InvalidArgument, "team_profile is required")
    }
    // body template 校验逻辑（原行 209-215）删除
    // oneof spec 一致性校验保留，数据源改为 parent
    if err := validateSpecConsistency(parent, tp.GetSaolei() != nil); err != nil {
        return err
    }
    return nil
}
```

### 4.4 teamProfileToProto（移除 template 赋值）

```go
// projects/game/prompt/handler/handler.go — 修改后
// Template: game.TemplateName{...}.String() 赋值行删除
```
