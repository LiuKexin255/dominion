# Agent 玩游戏 step3.a 优化方案

本文档记录对 `design/game/game-agent-step3-a.md` 当前实现方案的优化结论，用于指导后续协议、分层和测试调整。

## 目标

完成优化后应达到以下效果：

1. `AgentFrame` 协议只有一个权威序号来源，避免 envelope 与 payload 重复保存 `sequence`。
2. `proxy` 保持纯转发与 owner 路由职责，不保存、不默认、不解释 agent profile。
3. `prompt` 的 domain/runtime/handler 分层与 session 服务一致，资源名只在 handler 边界生成。
4. `agent` 创建流程显式处理 profile，runtime 创建参数清晰，不把 prompt 服务隐式混入 invoke runtime。
5. desktop 与 agent 的手动游玩闭环符合“每次操作后 desktop 发送下一张截图”的模型。

## 优化项

### 1. 收敛 `sequence` 到 `AgentFrame`

当前 `AgentFrame` 已包含：

```proto
string invoke_id = 4;
int64 sequence = 5;
```

因此具体 payload 不应再保存独立 `sequence` 字段，包括：

1. `AgentScreenshotFrame.sequence`
2. `AgentOperationFrame.sequence`
3. `AgentOperationResultFrame.sequence`

优化后以 `AgentFrame.sequence` 作为唯一权威序号。handler 将序号从 frame envelope 传入 domain，runtime 输出 frame 时也只设置 envelope 序号。

### 2. proxy 不保存、不默认 profile

`proxy` 只负责：

1. 根据 session 查找或创建 owner。
2. 将 `CreateAgent` 请求转发给选中的 agent owner。
3. 持久化 owner 路由所需信息。

`agent_profile_name` 不影响 owner 查找，因此不应存入 `AgentOwner` 或 proxy Mongo 文档。`proxy` 也不应把空 profile 设置为 `default`。

空 profile、默认 profile、profile 是否存在、是否 enabled，均应由 agent/prompt 侧处理。

### 3. prompt runtime/mongo 直接实现 repo 接口

`prompt/cmd/main.go` 中的 `agentProfileAdapter` 和 `skillAdapter` 应移除。

推荐让 `prompt/runtime/mongo.Repository` 直接实现：

1. `domain.AgentProfileRepository`
2. `domain.SkillRepository`

这与 session/proxy 的 runtime/mongo 模式一致，main.go 只负责依赖组装。

### 4. prompt domain 不保存 API resource name

`AgentProfile` 和 `Skill` 的 domain 模型不应保存 proto/API resource name。

推荐 domain 模型只保存业务标识与业务字段：

```go
type AgentProfile struct {
    AgentProfileName string
    Model            string
    SystemPrompt     string
    SkillNames       []string
    MCPNames         []string
    Enabled          bool
    CreateTime       time.Time
    UpdateTime       time.Time
}

type Skill struct {
    SkillName  string
    Content    string
    Enabled    bool
    CreateTime time.Time
    UpdateTime time.Time
}
```

`name` 只在 handler 输出 proto 时合成。Mongo 文档同理不保存 `name`，避免 `name` 与业务 ID 漂移。

资源名解析和拼装应放入 `projects/game/pkg/gameconst`，参考 session 的 `SessionName()` / `SessionID()` 模式，新增：

1. `AgentProfileName(profileID string) string`
2. `AgentProfileID(name string) (string, error)`
3. `SkillName(skillID string) string`
4. `SkillID(name string) (string, error)`

### 5. 修正 prompt Mongo BSON 写法

`prompt/runtime/mongo` 中 `bson.D` 建议使用显式 `Key` / `Value` 字段，避免 LSP 告警：

```go
bson.D{{Key: fieldAgentProfileName, Value: 1}}
```

对象字段仍应使用具体 BSON 模型，不使用 `bson.M` 表示对象结构。

### 6. agent runtime/client 分层调整

`agent/runtime` 根目录不应直接放实现文件。推荐按 runtime 依赖或能力拆分子包，例如：

1. `agent/runtime/invoke`：最小 step3.a invoke runtime。
2. `agent/runtime/promptclient`：封装 prompt gRPC client 到 agent domain 接口。

`agent/cmd/main.go` 中的 `promptClientAdapter` 应移动到 `runtime/promptclient`，main.go 只创建连接和组装对象。

### 7. agent 创建参数显式化

agent 服务不应在 runtime 内部把空 `profileName` 默认成 `default`。

推荐流程：

1. handler 收到 `AgentCreateRequest`。
2. 若未提供 `agent_profile_name`，返回 InvalidArgument。
3. agent 创建服务或工厂调用 prompt client 拉取 profile 与 skills。
4. 将 profile 解析结果转换成明确的 runtime 参数。
5. 使用显式参数创建 invoke runtime/session。

示例参数形态：

```go
type InvokeRuntimeConfig struct {
    ProfileName  string
    Model        string
    SystemPrompt string
    Skills       []SkillConfig
    MCPNames     []string
}
```

这样可以避免把 prompt 服务调用与 `InvokeRuntime` 状态机耦合在一起。

### 8. 调整截图/操作闭环模型

当前 step3.a 文档写入了独立 `operation_result`，但优化方向应改为：desktop 每次执行操作后发送下一张 screenshot，下一张 screenshot 即代表操作后的新观察结果。

推荐协议模型：

1. desktop 发送 `screenshot`。
2. agent 返回 `text`、`thinking` 或 `operation`。
3. desktop 展示并执行 operation。
4. desktop 执行后发送下一张 `screenshot`。
5. agent 根据新 screenshot 判断继续、等待或结束。

不再需要独立 `OperationResult` 作为主流程输入。如果需要表达“继续等待”或“主动请求当前截图”，可增加空操作或专门的 request-screenshot payload。

推荐优先新增专门 payload，而不是让 `AgentOperationFrame` 空 oneof：

```proto
message AgentWaitFrame {
  string reason = 1;
}

message AgentScreenshotRequestFrame {
  string reason = 1;
}
```

如为保持 step3.a 最小实现，也可临时使用空操作，但需要在 proto 注释中明确其语义，避免与“operation 只表达一个动作”的约束冲突。

## 调整顺序

1. 先更新 `game.proto`：移除 payload 级 `sequence`，修订 operation/result/screenshot 模型。
2. 更新 agent domain/runtime/handler，统一使用 `AgentFrame.sequence`。
3. 更新 desktop 发送流程：操作后发送下一张 screenshot，而不是发送 `operation_result`。
4. 清理 proxy 的 `AgentProfileName` 存储和默认值逻辑。
5. 清理 prompt domain、mongo、handler 的 resource name 生成位置。
6. 移动 agent prompt client adapter 与 runtime 实现到 runtime 子包。
7. 更新 proto/unit/system test 与 testplan。

## 验收标准

1. `AgentFrame.sequence` 是唯一序号来源，所有相关测试不再依赖 payload sequence。
2. 未提供 `agent_profile_name` 创建 agent 时返回明确错误。
3. proxy owner 记录只包含 owner 路由所需字段。
4. prompt Mongo 文档不保存 `name`，handler response 仍能返回正确 resource name。
5. desktop 执行 operation 后通过下一张 screenshot 推动状态机继续。
6. `bazel test //projects/game/...` 通过。
7. step3.a testplan 更新为新的截图/操作闭环并可执行。
