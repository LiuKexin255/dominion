# Agent 玩游戏 step3.a 方案

本文档承接 `ideas/llm_agent_play_game/README.md` 中 step3 的总体目标和 step3.a 的阶段目标，以及 `design/game/game-agent-step2.md`、`design/game/game-agent-step2-review-convergence.md`、`design/game/desktop-screenshot-refactor.md` 已确定的截图、连接和测试约束。step3.a 的核心目标是先稳定 agent 与 desktop 之间的手动游玩协议，并新增 mongo backed prompt 服务，使后续 step3.b 接入 TypeScript deepagent runtime 时不需要再次改变基础协议。

## 目标

完成 step3.a 后应达到以下效果：

1. step3 的 deepagent、MCP、tools、SKILLS、prompt 管理、手动操作和 desktop 对话式交互目标被拆解为可先验收的协议与控制面能力。
2. `AgentFrame` 能表达一次手动游玩闭环：desktop 发送截图，agent 返回文本、思考内容或一个操作指令，desktop 执行后返回操作结果和下一张截图。
3. agent 输出的鼠标坐标只使用截图相对像素坐标；desktop 持有窗口绑定和截图元数据，并负责转换到本地窗口或屏幕坐标。
4. agent 与 desktop 的交互带有 `invoke_id` 和递增 `sequence`，旧序号、错误 invoke 或错误状态下的 frame 不会触发新的操作。
5. 新增 `prompt` 服务，使用 mongo 存储 agent profile 和工具无关 SKILLS。
6. agent 创建支持 `agent_profile_name`，能够基于 profile 校验 SKILLS 和 MCP 名称后创建 agent 实例。
7. 本阶段可以使用可预测的最小 agent runtime 生成操作指令，不要求接入真实 deepagent 推理。
8. 系统级验收优先通过现有 `projects/game/testplan` 编排完成，并在同一个 testplan 文件中新增 step3.a suite。

## 非目标

step3.a 不包含以下内容：

1. 不把 agent 服务切换为 TypeScript。
2. 不接入真实 LangChain deepagent 模型推理。
3. 不实现 subagent、long-term memory 或策略总结。
4. 不实现自动连续截图或无人值守操作循环；本阶段仍是手动游玩闭环。
5. 不要求在 testplan 中执行真实 Windows 鼠标或键盘操作；真实窗口操作保留为 Windows 手动验收。
6. 不把截图、操作轨迹或 agent 运行时记忆保存到 prompt 服务。

## 已确认决策

1. step3.a 保留 step3 的总体目标，但先稳定 wire protocol、prompt 控制面和状态机，再在 step3.b 接入 deepagent runtime。
2. prompt 服务使用 mongo 存储。
3. profile 模型不包含 `agent_prompt`；游戏玩法相关提示词通过 SKILL 输入。
4. gateway 和 proxy 继续只转发 `AgentFrame`，不理解 deepagent、MCP、tools 或 SKILLS。
5. 鼠标操作坐标使用截图相对像素坐标，不使用屏幕绝对坐标。
6. 每个 session 初版只允许一个 active invoke，且同一时间只允许一个待处理 desktop 操作。
7. DeleteAgent 删除空 agent 视为成功，避免无意义噪音。

## 总体架构

step3.a 沿用 step2 的服务链路，并新增 prompt 服务：

```text
desktop Wails
  ├─ HTTP/JSON unary
  │   -> gateway grpc-gateway
  │   -> session / proxy / prompt gRPC
  └─ WebSocket AgentFrame protojson
      -> gateway
      -> proxy ConnectAgent gRPC stream
      -> owner agent Connect gRPC stream

agent
  -> prompt gRPC client
  -> validate profile / skills / mcp names
  -> deterministic step3.a runtime
```

职责边界：

1. `prompt` 只管理 profile 和工具无关 SKILLS。
2. `proxy` 继续负责 owner 选择、owner 持久化和 stream 路由。
3. `agent` 负责 profile 加载、runtime 状态、invoke 状态机和操作序号校验。
4. `desktop` 负责截图、操作展示、用户确认执行、本地坐标转换和结果回传。

## 模型设计

### PromptService

建议在 `projects/game/game.proto` 中新增 `PromptService`，由 gateway 注册 grpc-gateway HTTP handler。

资源：

1. `AgentProfile`：描述 agent 创建所需的模型、系统提示词、SKILLS 和 MCP 名称。
2. `Skill`：工具无关的 SKILL 文件内容。

建议 HTTP 路径：

```text
POST /api/v1/prompts/agentProfiles
GET  /api/v1/prompts/agentProfiles/{agent_profile_name}
GET  /api/v1/prompts/agentProfiles
DELETE /api/v1/prompts/agentProfiles/{agent_profile_name}

POST /api/v1/prompts/skills
GET  /api/v1/prompts/skills/{skill_name}
GET  /api/v1/prompts/skills
DELETE /api/v1/prompts/skills/{skill_name}
```

如果短期只需要支持 agent 创建，`Create` 与 `Get` 可以先落地，`List` 和 `Delete` 可作为同阶段补充测试目标。

### AgentProfile

profile 不保存游戏玩法提示词的独立 `agent_prompt` 字段。玩法相关内容通过 `skill_names` 引用 SKILL。

建议字段：

```proto
message AgentProfile {
  string name = 1;
  string agent_profile_name = 2;
  string model = 3;
  string system_prompt = 4;
  repeated string skill_names = 5;
  repeated string mcp_names = 6;
  bool enabled = 7;
  google.protobuf.Timestamp create_time = 8;
  google.protobuf.Timestamp update_time = 9;
}
```

### Skill

建议字段：

```proto
message Skill {
  string name = 1;
  string skill_name = 2;
  string content = 3;
  bool enabled = 4;
  google.protobuf.Timestamp create_time = 5;
  google.protobuf.Timestamp update_time = 6;
}
```

### CreateAgent

`ProxyService.CreateAgent` 的外部请求和 `AgentService.CreateAgent` 的 owner runtime 请求都需要携带 `agent_profile_name`。

规则：

1. `agent_profile_name` 为空时使用 `default`。
2. profile 不存在时创建失败。
3. profile disabled 时创建失败。
4. profile 中引用的 skill 不存在或 disabled 时创建失败。
5. profile 中的 MCP 名称不在 agent 内置 registry 中时创建失败。
6. proxy 不解析 profile 内容，只负责透传 profile name。

### AgentFrame 扩展

现有 `status`、`echo`、`screenshot`、`ack` 保持兼容。新增 oneof payload 建议包括：

1. `text`：agent 面向 desktop 展示的文本输出。
2. `thinking`：agent 思考或中间过程展示内容。
3. `operation`：agent 请求 desktop 执行的一个操作。
4. `operation_result`：desktop 对一个操作的执行结果。
5. `warn` 或 `error`：协议状态、序号或执行失败信息。

frame envelope 建议新增：

```proto
string invoke_id = 4;
int64 sequence = 5;
```

`frame_id` 继续作为单帧排查标识，`sequence` 用于业务顺序约束。

### Screenshot

`AgentScreenshotFrame` 建议新增：

```proto
string screenshot_id = 11;
int64 sequence = 12;
```

语义：

1. `screenshot_id` 标识这张截图，后续 operation 必须引用它。
2. `width_px` / `height_px` 是 agent 坐标边界。
3. agent 输出的坐标必须满足 `0 <= x_px < width_px` 与 `0 <= y_px < height_px`。

### Operation

每个 `AgentOperationFrame` 只表达一个动作。

建议字段：

```proto
message AgentOperationFrame {
  string operation_id = 1;
  string screenshot_id = 2;
  int64 sequence = 3;
  oneof operation {
    AgentMouseOperation mouse = 10;
    AgentKeyboardOperation keyboard = 11;
  }
}
```

支持操作：

1. 鼠标左键单击。
2. 鼠标右键单击。
3. 鼠标左键双击。
4. 鼠标右键双击。
5. 按键操作。

鼠标操作使用截图相对像素坐标：

```proto
message AgentMouseOperation {
  AgentMouseButton button = 1;
  AgentMouseClickType click_type = 2;
  int32 x_px = 3;
  int32 y_px = 4;
}
```

### OperationResult

desktop 执行或拒绝 operation 后回传结果。

建议字段：

```proto
message AgentOperationResultFrame {
  string operation_id = 1;
  int64 sequence = 2;
  AgentOperationResultStatus status = 3;
  string message = 4;
}
```

`status` 至少包括：

1. `ACCEPTED`。
2. `EXECUTED`。
3. `REJECTED`。
4. `FAILED`。
5. `TIMED_OUT`。

## 状态机设计

### Invoke 状态

每个 session 初版只允许一个 active invoke：

```text
idle
  -> invoking
  -> waiting_for_operation_result
  -> invoking
  -> completed / failed / timed_out
```

规则：

1. desktop 发送截图可以启动一个 invoke。
2. agent 发送 operation 后进入 `waiting_for_operation_result`。
3. 在等待 operation result 时，新的 screenshot 必须匹配当前 invoke 和期望 sequence，否则返回 warn 或忽略。
4. operation result 到达后 agent 可以继续输出 text、thinking、operation 或完成 invoke。
5. 本阶段不支持并行 operation。

### Sequence 规则

建议使用每个 invoke 内单调递增 sequence：

1. desktop 发起 screenshot，sequence 为当前输入序号。
2. agent 输出 operation，sequence 必须递增。
3. desktop 输出 operation_result，sequence 必须递增。
4. 小于或等于已处理 sequence 的 frame 视为旧 frame，记录 warn 并忽略。
5. 大于期望 sequence 的 frame 视为乱序，记录 warn 并拒绝。
6. invoke_id 不匹配时不执行操作。

## 代码分层

### prompt

建议目录：

```text
projects/game/prompt/
  cmd/
    main.go
  domain/
    model.go
    repository.go
  handler/
    handler.go
  runtime/
    mongo/
      repository.go
      model.go
  service.yaml
```

Mongo 约束：

1. database 使用 `game_prompt`。
2. collections 使用 `agent_profiles` 和 `skills`。
3. 不使用 `_id` 覆盖业务字段。
4. bson 中对象字段使用明确 struct，不使用无结构 `bson.M` 表达业务模型。
5. `agent_profile_name` 和 `skill_name` 建唯一索引。

### agent

step3.a 可以继续使用 Go agent，但将 runtime 从简单截图 ack 扩展为 invoke runtime。

建议 domain 接口增加：

```text
Create(sessionID, profileName)
Delete(sessionID)
Status(sessionID)
ReceiveScreenshot(sessionID, input)
ReceiveOperationResult(sessionID, result)
```

agent handler 只负责 proto/domain 转换和 gRPC 错误映射，状态机和序号校验放在 runtime/service 层。

### proxy

proxy 只需要在 `CreateAgent` 中透传 `agent_profile_name`。`ConnectAgent` 继续对新增 oneof payload 透明转发。

### gateway

gateway 需要新增 prompt gRPC client 并注册 PromptService grpc-gateway handler。WebSocket 逻辑不解析新增业务 payload。

### desktop

step3.a 的 desktop 改动以协议闭环为主：

1. TypeScript API 类型增加 text、thinking、operation、operation_result、warn/error。
2. Play 页面展示 agent operation。
3. 用户手动确认执行 operation。
4. Go 后端提供坐标转换和本地操作执行接口。
5. 执行后回传 operation_result，并按用户操作截图回传下一张图片。

## 测试方案

### 单元测试

至少覆盖：

1. `AgentFrame` 新 oneof payload protojson roundtrip。
2. `AgentScreenshotFrame` 的 `screenshot_id` 和 `sequence` roundtrip。
3. operation 坐标边界校验。
4. invoke 状态机 happy path。
5. 重复 sequence、跳跃 sequence、错误 invoke_id。
6. `CreateAgent` 透传 `agent_profile_name`。
7. prompt mongo repository 的 create/get/list/delete。
8. profile disabled、skill missing、mcp name invalid 的错误映射。
9. DeleteAgent 空 agent 幂等。
10. desktop 坐标转换函数：截图相对像素到屏幕或窗口坐标。

### Testplan

不需要创建新的 testplan 文件。现有 `projects/game/testplan/system_test.yaml` 可以新增多个 suite。

建议新增 suite：

```yaml
suites:
  - name: step2-regression
    deploy: //projects/game/testplan/deploy.yaml
    endpoint:
      http:
        public: https://game.liukexin.com
    cases:
      - //projects/game/testplan:testplan_test

  - name: step3a
    deploy: //projects/game/testplan/deploy_step3a.yaml
    endpoint:
      http:
        public: https://game.liukexin.com
    cases:
      - //projects/game/testplan:step3a_testplan_test
```

`deploy_step3a.yaml` 在现有部署基础上增加 prompt 服务。

step3.a testplan case 建议：

1. `TestPromptProfileCreateGet`。
2. `TestPromptSkillCreateGet`。
3. `TestCreateAgentWithDefaultProfile`。
4. `TestCreateAgentWithNamedProfile`。
5. `TestCreateAgentMissingProfile`。
6. `TestScreenshotToOperation`。
7. `TestOperationResultCompletesInvoke`。
8. `TestRejectStaleSequence`。
9. `TestRejectWrongInvokeID`。
10. `TestDeleteAgentIdempotent`。
11. `TestFullStep3aLifecycle`。

### Windows 手动验收

由于真实窗口操作依赖 Windows 桌面环境，本阶段保留手动验收：

1. 启动 desktop。
2. 创建 profile 和 skill。
3. 创建 session 与 agent。
4. 绑定普通窗口。
5. 发送截图。
6. desktop 收到 agent operation 并展示。
7. 用户确认执行。
8. 窗口收到对应鼠标或键盘操作。
9. desktop 回传 operation result 和下一张截图。

## 实施步骤

1. 更新 `game.proto`：新增 PromptService、profile/skill、operation/text/thinking/result/warn/error frame、CreateAgent profile 字段。
2. 实现 prompt 服务及 mongo repository。
3. 更新 gateway 注册 prompt 服务。
4. 更新 proxy CreateAgent 透传 profile name。
5. 更新 agent runtime，支持 profile 加载、invoke 状态机和 mock operation 输出。
6. 更新 desktop API 类型和 Play 页面，支持展示与回传 operation/result。
7. 补充单元测试。
8. 在现有 testplan 中新增 step3.a suite 和部署配置。
9. 执行 Bazel 测试、构建和 testplan 验收。

## 验收标准

1. `bazel test //projects/game/...` 成功。
2. `bazel build //projects/game/...` 成功。
3. `guitar validate projects/game/testplan/system_test.yaml` 成功。
4. `guitar run projects/game/testplan/system_test.yaml` 中 step2 regression suite 和 step3.a suite 均通过。
5. prompt profile/skill 可通过 HTTP 创建和读取，并由 Mongo 持久化。
6. CreateAgent 能使用 profile 创建 agent，缺失或禁用 profile 会返回确定错误。
7. WebSocket 能完成 screenshot -> operation -> operation_result 的黑盒链路。
8. stale sequence 和 wrong invoke_id 不会触发操作。
9. DeleteAgent 对空 agent 幂等。
10. Windows 手动验收中坐标转换和真实操作符合截图相对像素语义。

## 风险与注意事项

1. proto 是破坏性变更，desktop、gateway、proxy、agent、testplan 必须同步更新。
2. sequence 规则必须在 runtime 层集中实现，避免 handler 和 desktop 分散判断导致漂移。
3. prompt 服务不应存储运行时轨迹，否则会和后续 agent memory 边界混淆。
4. 真实 Windows 操作不能强制进入 Linux CI/testplan。
5. 初版不支持多个 outstanding operation，避免幂等和取消语义复杂化。

## 未来规划

1. step3.b 将 agent 服务切换为 TypeScript grpc-js 服务，并接入最小 deepagent runtime。
2. 后续阶段可增加自动连续截图、策略总结和长期记忆，但这些不属于 step3.a。
