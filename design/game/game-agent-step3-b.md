# Agent 玩游戏 step3.b 方案

本文档承接 `ideas/llm_agent_play_game/README.md` 中 step3 的总体目标和 step3.b 的阶段目标，以及 `design/game/game-agent-step3-a.md` 已确定的协议、prompt 服务、profile 创建和手动操作闭环。step3.b 的核心目标是把 agent runtime 切换为 TypeScript grpc-js 服务，并引入 LangChain deepagent 生态的最小单 agent 能力，使 agent 能基于截图、profile 和 SKILLS 输出文本、思考内容与一个 desktop 操作。

## 目标

完成 step3.b 后应达到以下效果：

1. step3 的 TypeScript agent、deepagent、MCP、tools、SKILLS、超时清理和 desktop 对话式交互目标在 step3.a 的协议基础上完整落地。
2. agent 服务由 TypeScript grpc-js 实现，并保持现有 `AgentService` gRPC 协议和 proxy owner 路由链路可用。
3. agent runtime 引入 LangChain deepagent 生态，但只启用本阶段需要的最小单 agent 能力。
4. agent 不包含 subagent 和 long-term memory。
5. agent 能根据 prompt 服务中的 profile、系统提示词和 SKILLS 创建 session-scoped runtime。
6. agent 能接收 desktop 截图，将截图作为模型输入，并输出 thinking、text 或一个 operation。
7. agent 支持 MCP、tools 和 runtime 相关 SKILLS；这些运行时能力由 agent 服务内置或注册，工具无关 SKILLS 仍由 prompt 服务加载。
8. 单次 invoke 默认 10 分钟超时，30 分钟 idle 且无 active invoke 或 pending operation 时自动删除 agent。
9. desktop UI 支持 prompt 管理和对话式 play 页面。
10. 系统级验收优先通过现有 testplan 的新增 suite 完成，真实窗口操作继续保留 Windows 手动验收。

## 非目标

step3.b 不包含以下内容：

1. 不实现 subagent。
2. 不实现 long-term memory。
3. 不实现策略总结和自动更新策略。
4. 不把 LangChain、MCP 或 tool 内部细节暴露给 gateway/proxy。
5. 不实现多 outstanding operation。
6. 不要求 testplan 运行真实 Windows UI 自动化。

## 已确认决策

1. step3.b 不改变 step3 总体目标，只把 step3.a 的可预测 runtime 替换为 TypeScript deepagent runtime。
2. TypeScript agent 是现有 agent service 的替换实现，不改变 gateway、proxy、session 的外部职责。
3. DeepAgent 能力作为 agent runtime 内部实现细节。
4. 如果完整 `createDeepAgent` 默认能力超出阶段目标，可以先使用 LangChain 单 agent 能力并保留 deepagent middleware 扩展点；对外仍称为引入 deepagent 生态。
5. prompt Mongo 只存 profile 和工具无关 SKILLS，不存运行时轨迹。
6. 游戏玩法提示词继续通过 SKILL 输入。
7. 所有鼠标操作坐标仍为截图相对像素坐标。

## 总体架构

step3.b 的服务链路：

```text
desktop
  -> gateway HTTP/WS
  -> proxy
  -> agent-ts grpc-js AgentService
       -> prompt gRPC client
       -> profile / skills loader
       -> mcp/tool registry
       -> minimal deepagent runtime
```

gateway 和 proxy 不感知 agent 是 Go 还是 TypeScript。proxy 仍通过 resolver 获取 stateful agent instance，并通过 gRPC 连接 owner agent。

## 模型设计

step3.b 复用 step3.a 的 proto 模型，不新增面向 gateway/proxy 的 deepagent 专用模型。

### Agent runtime 模型

TypeScript agent 内部建议使用以下概念模型：

1. `AgentInstance`：一个 session 下的 agent runtime。
2. `AgentProfileSnapshot`：创建 agent 时从 prompt 服务读取的 profile 和 skills 快照。
3. `InvokeState`：一次截图输入触发的 agent invoke 状态。
4. `ToolRegistry`：agent 内置 MCP 和 tools 的合法名称与实现集合。
5. `SkillBundle`：从 prompt 服务加载的工具无关 SKILLS。

profile 更新不会自动影响已创建 agent。已创建 agent 使用创建时的 profile snapshot，避免运行时配置漂移。后续需要热更新时再设计 reload 语义。

### DeepAgent 输入输出映射

输入：

1. system prompt 来自 profile。
2. 游戏玩法提示来自 profile 引用的 SKILLS。
3. 截图以 multimodal image 输入。
4. 最近一次 operation result 作为文本上下文输入。

输出：

1. reasoning/thinking 内容映射为 `AgentThinkingFrame`。
2. 普通文本映射为 `AgentTextFrame`。
3. desktop 操作 tool 调用映射为 `AgentOperationFrame`。
4. runtime 错误映射为 `AgentErrorFrame`。

每次 invoke 最多产生一个等待 desktop 执行的 operation。operation 执行结果返回前不启动新的 operation。

## 代码分层

建议目录：

```text
projects/game/agent_ts/
  src/
    main.ts
    grpc/
      agent_service.ts
      proto.ts
    runtime/
      agent_instance.ts
      invoke_state.ts
      manager.ts
    prompt/
      client.ts
      profile_loader.ts
    deepagent/
      factory.ts
      mapper.ts
    tools/
      registry.ts
      desktop_operation.ts
    config.ts
  package.json
  tsconfig.json
  BUILD.bazel
  service.yaml
```

职责：

1. `grpc` 只处理 grpc-js server、stream 和 proto/domain 转换。
2. `runtime` 管理 session scoped agent、invoke 状态、超时和 idle 删除。
3. `prompt` 封装 PromptService gRPC client。
4. `deepagent` 封装 LangChain/deepagent 创建、输入构造和输出解析。
5. `tools` 注册 MCP、tools 和 desktop operation tool。
6. `config` 管理超时、模型和测试覆盖配置。

## TypeScript agent 服务

`agent_ts` 必须实现与现有 `AgentService` 等价的 gRPC 行为：

1. `CreateAgent`：加载 profile 和 skills，校验 MCP 名称，创建 runtime。
2. `DeleteAgent`：删除 runtime；runtime 不存在时返回成功。
3. `GetAgentStatus`：返回 session agent 状态。
4. `Connect`：处理 `AgentFrame` 双向 stream。

兼容性要求：

1. Go proxy 可以通过现有 agent client manager 连接 TypeScript agent。
2. `Connect` 的双向 stream 行为与 Go agent 兼容。
3. `status`、`echo`、`screenshot`、`operation_result` 都应通过 protojson/gRPC 链路正常往返。

## DeepAgent 最小能力

本阶段只启用：

1. 一个 session-scoped agent。
2. system prompt。
3. SKILLS 文本注入或按需加载。
4. 截图 multimodal 输入。
5. desktop operation tool。
6. 可选 MCP tool registry 校验和一个 dummy MCP/tool 调用。

不启用：

1. subagent。
2. long-term memory。
3. 策略总结。
4. 自动文件系统写入，除非该能力是 deepagent 最小运行所必需且不对外暴露。

## Timeout 与生命周期

### Invoke timeout

默认单次 invoke 超时时间为 10 分钟。超时后：

1. 当前 invoke 标记为 timed_out。
2. pending operation 清理。
3. stream 返回 error 或 status frame，供 desktop 展示。
4. agent runtime 保留，等待下一次输入或删除。

### Idle delete

默认 idle 时间为 30 分钟。满足以下条件时删除 agent runtime：

1. 没有 active invoke。
2. 没有 pending operation。
3. 没有新的 input。
4. 超过 idle 时间。

测试环境必须可以配置更短的 timeout 和 idle 时间，避免大型测试等待真实 10 分钟或 30 分钟。

## Desktop UI

step3.b 对 desktop UI 的目标：

1. 增加 prompt 管理入口，支持 profile 和 skill 的基本创建、查看和删除。
2. agent 创建时可以选择 agent profile。
3. play 页面改为对话风格。
4. desktop 作为 user role 仅发送图片，图片默认折叠，点击后展开。
5. agent 消息展示 thinking、text 和 operation。
6. operation 明确展示操作类型、坐标、引用 screenshot、sequence 和执行结果。
7. 用户手动确认后 desktop 执行操作并回传 operation result。

## 测试方案

### 单元测试

TypeScript agent 至少覆盖：

1. profile loader 正确读取 profile 和 skills。
2. disabled profile、missing skill、invalid mcp name 返回确定错误。
3. screenshot 输入能转换为 multimodal message。
4. model/text 输出能转换为 `AgentTextFrame`。
5. reasoning/thinking 输出能转换为 `AgentThinkingFrame`。
6. operation tool 调用能转换为 `AgentOperationFrame`。
7. operation result 能推进 invoke 状态。
8. invoke timeout 使用 fake clock 或短配置触发。
9. idle delete 使用 fake clock 或短配置触发。
10. DeleteAgent 对空 runtime 幂等。

Go 侧至少覆盖：

1. proxy 仍能透传 profile name。
2. gateway 对新增 frame 仍透明转发。
3. prompt service 与 step3.a 行为保持兼容。

Desktop 至少覆盖：

1. operation frame 展示模型。
2. 坐标转换。
3. operation result frame 构造。
4. profile 选择参数传入 CreateAgent。

### Testplan

继续使用现有 `projects/game/testplan/system_test.yaml` 的多 suite 机制，不创建新的 testplan 文件。

建议新增 suite：

```yaml
  - name: step3b
    deploy: //projects/game/testplan/deploy_step3b.yaml
    endpoint:
      http:
        public: https://game.liukexin.com
    cases:
      - //projects/game/testplan:step3b_testplan_test
```

`deploy_step3b.yaml` 使用 prompt 服务，并将 agent artifact 替换为 TypeScript agent service。

step3.b testplan case 建议：

1. `TestTSAgentStatus`：创建 profile、session、agent 后，WS status 返回成功。
2. `TestTSAgentEcho`：保持基础 stream 兼容性。
3. `TestTSAgentScreenshotToText`：发送测试 PNG 后收到 text frame。
4. `TestTSAgentScreenshotToOperation`：发送测试 PNG 后收到 operation frame，坐标在截图范围内。
5. `TestTSAgentOperationResultCompletesInvoke`：回传 operation result 后收到 completed text/status。
6. `TestTSAgentInvalidProfile`：无效 profile 创建失败。
7. `TestTSAgentInvalidMCPName`：非法 MCP 名称创建失败。
8. `TestTSAgentInvokeTimeout`：短超时配置下 invoke 超时返回确定错误。
9. `TestTSAgentIdleDelete`：短 idle 配置下 runtime 被自动删除，DeleteAgent 再调用仍成功。
10. `TestFullStep3bLifecycle`：profile/skill -> create session -> create agent -> connect -> screenshot -> operation -> result -> delete agent -> delete session。

### Windows 手动验收

1. 启动 desktop。
2. 创建 profile 和 skill。
3. 创建 session 并选择 profile 创建 agent。
4. 绑定窗口。
5. 发送截图。
6. agent 返回 thinking/text/operation。
7. 用户确认执行 operation。
8. 窗口收到点击或按键。
9. desktop 回传 operation result 和新截图。
10. agent 继续输出或完成本轮 invoke。

## 实施步骤

1. 建立 TypeScript agent service 的 Bazel、package、service.yaml 和 grpc-js 服务骨架。
2. 实现 AgentService 的 status/echo 兼容行为。
3. 将 test deploy 中 agent artifact 切换为 TypeScript agent，并验证 proxy/gateway 链路。
4. 接入 prompt client，完成 CreateAgent profile 加载和校验。
5. 接入 minimal deepagent runtime，将截图输入转换为模型输入。
6. 实现 thinking/text/operation 输出映射。
7. 实现 invoke timeout 和 idle delete。
8. 重构 desktop prompt 管理和对话式 play 页面。
9. 补充单元测试和 step3.b testplan suite。
10. 执行 Bazel、构建、testplan 和 Windows 手动验收。

## 验收标准

1. TypeScript agent service 可以通过 Bazel 构建为可部署 artifact。
2. `bazel test //projects/game/...` 成功。
3. `bazel build //projects/game/...` 成功。
4. `guitar validate projects/game/testplan/system_test.yaml` 成功。
5. `guitar run projects/game/testplan/system_test.yaml` 中 step2、step3.a、step3.b 相关 suite 均通过。
6. Go proxy 能通过现有 owner routing 连接 TypeScript agent。
7. TS agent 能完成 screenshot -> deepagent/minimal agent -> operation 的链路。
8. profile 和 skills 从 prompt Mongo 服务加载。
9. invalid profile、invalid MCP、invoke timeout、idle delete 都有确定行为。
10. desktop 对话式 play 页面能展示图片、thinking、text、operation 和执行结果。
11. Windows 手动验收通过。

## 风险与注意事项

1. TypeScript gRPC 服务和 Bazel image 是本阶段最大基础设施风险，应先以 status/echo 骨架验证。
2. deepagent 默认能力可能超过本阶段目标，必须保持最小启用。
3. 不要让 prompt profile 更新影响已创建 agent，避免运行时漂移。
4. 不要在 gateway/proxy 中加入 deepagent、MCP 或 tool 特判。
5. 超时配置必须可测试，不能只能使用真实 10 分钟或 30 分钟。
6. 多模态图片输入与模型供应商有关，必须在 runtime 层隔离模型差异。

## 未来规划

1. 增加 subagent。
2. 增加 long-term memory。
3. 增加策略总结和 SKILL 更新建议。
4. 增加自动连续截图和无人值守操作循环。
5. 增加 profile 版本化和 agent reload 语义。
