# Feature Specification: Step3.b Agent Runtime

**Feature Branch**: `003-step3b-agent-runtime`

**Created**: 2026-06-03

**Status**: Draft

**Input**: User description: "阅读 @ideas/llm_agent_play_game/README.md ，为 step3.b 设定新的需求。"

## Clarifications

### Session 2026-06-03

- Q: Should step3.b use screenshot-only continuation after desktop executes an operation? → A: Use screenshot-only continuation: after an operation, desktop sends the next screenshot as the operation result observation.
- Q: When should OpenCode Go credentials be validated? → A: Validate OpenCode Go credentials during `CreateAgent`; creation fails if the selected model needs missing, unreadable, invalid, or unauthorized credentials.
- Q: How should DeepAgent progressive events be delivered? → A: Stream DeepAgent events in real time as AgentFrames over the existing Connect stream.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 真实 agent 根据截图持续游玩 (Priority: P1)

玩家在 desktop 中进入某个 session 的 play 页面后，可以把当前游戏截图发送给 agent；agent 基于 profile、系统提示词、SKILLS 和可用工具进行最小推理，并通过现有 Connect 流实时返回文本、思考过程、工具进展和单步操作指令。

**Why this priority**: step3.a 已经稳定了截图和操作协议；step3.b 的核心价值是把可预测的最小实现替换成可实际推理的 agent runtime，让手动游玩闭环具备真实决策能力。

**Independent Test**: 使用一个已配置的 agent profile 创建 agent，连接 play 通道并发送一张 PNG 截图；无需真实窗口自动化即可在 invoke 完成前观察到至少一个实时文本、思考或工具进展帧，并返回一个与该截图关联的合法操作指令。

**Acceptance Scenarios**:

1. **Given** session 中已创建使用有效 profile 的 agent，且 desktop 已连接 agent 通道，**When** desktop 发送包含截图尺寸、编码和截图标识的截图帧，**Then** agent 通过同一 Connect 流实时返回同一 session 下的推理进展、文本输出和最多一个待执行操作。
2. **Given** agent 返回鼠标操作，**When** desktop 展示该操作，**Then** 操作坐标使用截图相对像素坐标，desktop 可以继续按 step3.a 规则转换成本地窗口或屏幕坐标。
3. **Given** desktop 执行操作后再次发送新截图，**When** agent 接收该截图，**Then** agent 将该截图作为上一次操作结果后的唯一新观察输入，并继续下一轮推理。

---

### User Story 2 - 通过 profile 选择模型、提示词、SKILLS 和提供商 (Priority: P1)

Agent 配置者可以在 prompt 服务中维护 agent profile 和工具无关 SKILLS，并在创建 agent 时选择 profile；agent 创建时加载 profile 中的系统提示词、模型选择、MCP 名称和 SKILL 引用，校验运行时能力可用后再启动 agent。

**Why this priority**: 没有 profile 驱动的配置加载，agent 行为无法被 prompt 服务管理，也无法在不同游戏、模型和 provider 之间稳定切换。

**Independent Test**: 创建包含模型、系统提示词、MCP 名称和 SKILL 名称的 profile，再用该 profile 创建 agent；创建成功后，agent 的首轮推理表现出 profile 中提示词和 SKILL 内容的约束，且缺失或禁用资源会阻止 agent 创建。

**Acceptance Scenarios**:

1. **Given** profile 引用了启用的工具无关 SKILL，**When** 使用该 profile 创建 agent 并发送截图，**Then** agent 在推理时可使用该 SKILL 提供的游戏玩法指导。
2. **Given** profile 引用了 agent runtime 不支持的 MCP 名称，**When** 创建 agent，**Then** 创建请求失败并明确指出不支持的 MCP 名称。
3. **Given** profile 选择 OpenCode Go 模型且所需 provider 密钥不存在、不可读、无效或未授权，**When** 创建 agent，**Then** 创建请求失败并显式报告 provider 凭据错误，且不会创建不可 invoke 的 agent。

---

### User Story 3 - 保持现有系统链路不变地替换 agent runtime (Priority: P1)

系统维护者可以把 agent 服务 runtime 切换到新的实现，同时保持 gateway、proxy、session、prompt 服务的外部资源路径和 step3.a 已定义的 AgentFrame 通信语义不变。

**Why this priority**: step3.b 需要升级 agent 内部能力，但不能破坏已完成的 session/agent 路由、WebSocket 转发、prompt 管理和 desktop 通信闭环。

**Independent Test**: 复用 step3.a 的 session 创建、agent 创建、agent 连接、截图发送、操作返回、下一张截图继续、序号校验和删除幂等测试；在 runtime 替换后这些测试仍通过，并额外验证最小推理闭环。

**Acceptance Scenarios**:

1. **Given** gateway、proxy、session 和 prompt 服务按 step3.a 方式部署，**When** agent 服务替换为 step3.b runtime，**Then** desktop 仍通过同一 HTTP 和 WebSocket 入口完成 session、agent 和 play 操作。
2. **Given** agent 通道收到 step3.a 已定义的截图、状态、echo 或操作后的下一张截图帧，**When** 新 runtime 处理这些帧，**Then** 响应帧仍遵循已有 AgentFrame 类型和递增序号约束。
3. **Given** 已存在的 DeleteAgent 调用删除一个不存在或已清理的 agent，**When** 调用完成，**Then** 请求成功且不产生错误噪音。

---

### User Story 4 - Desktop 以对话方式管理 prompt 和 play (Priority: P2)

玩家和配置者可以在 desktop 中管理 prompt/profile/SKILLS，并在 play 页面以对话式时间线查看 desktop 截图输入、agent thinking、文本输出、操作指令和本地操作执行状态。

**Why this priority**: 新 runtime 会产生渐进式推理事件；如果 desktop 仍是 step3.a 的测试页面，用户无法清楚理解 agent 当前依据、下一步操作和配置来源。

**Independent Test**: 打开 desktop，创建或查看 profile/SKILL，进入 play 页面发送截图；时间线中 desktop 图片默认折叠展示，agent thinking 和文本可读，操作指令可执行并能显示本地执行状态。

**Acceptance Scenarios**:

1. **Given** 用户在 desktop 打开 prompt 管理页面，**When** 创建或查看 profile 和 SKILL，**Then** 页面展示 profile 的模型、系统提示词、SKILL 名称和 MCP 名称。
2. **Given** play 页面收到 desktop 截图帧，**When** 截图进入对话时间线，**Then** 该消息默认折叠且可由用户展开查看。
3. **Given** play 页面收到 agent thinking、文本和操作帧，**When** 用户查看时间线，**Then** 不同类型内容被清晰区分，操作帧包含可执行动作和关联截图信息。
4. **Given** agent invoke 仍在进行，**When** agent 产生 thinking、文本或工具进展，**Then** desktop 在同一次连接中实时追加对应对话项，而不是等到 invoke 完成后批量展示。

---

### User Story 5 - Agent 生命周期具备超时和空闲清理 (Priority: P2)

系统维护者可以依赖统一的 invoke 超时和 idle 清理规则，避免长期运行的 agent 占用资源，同时不删除仍在推理、等待 desktop 发送下一张截图或刚接收新输入的 agent。

**Why this priority**: deepagent 推理可能持续较久；没有明确生命周期规则会导致资源泄漏或中断仍有效的手动游玩会话。

**Independent Test**: 创建 agent 后分别模拟长时间 invoke、空闲超过阈值、正在等待操作后的下一张截图以及显式删除；系统按预期超时、保留或删除 agent。

**Acceptance Scenarios**:

1. **Given** 某次 invoke 持续超过默认 10 分钟，**When** 超时规则触发，**Then** 本次 invoke 失败并向调用方返回可见的超时状态或警告。
2. **Given** agent 超过 30 分钟没有新输入、没有正在执行的 invoke、也没有等待下一张截图的待处理操作，**When** idle 清理运行，**Then** agent 被自动删除。
3. **Given** agent 正在执行 invoke 或等待 desktop 在操作后发送下一张截图，**When** idle 清理运行，**Then** agent 不会因普通空闲时间而被删除。

### Edge Cases

- If the selected profile does not exist, is disabled, or references disabled SKILLS, agent creation fails with a user-visible configuration error.
- If a tool-independent SKILL is updated in prompt service after an agent is created, the next agent creation or profile refresh must use the latest enabled content; already-running invoke behavior is not required to change mid-invoke.
- If the provider credential secret required by the selected OpenCode Go model is missing, empty, unreadable, invalid, unauthorized, or bound to the wrong key, CreateAgent fails with a provider credential error before the agent is reported usable.
- If OpenCode Go usage limits or authentication failures occur during invoke, the error is surfaced to the session instead of silently switching to another provider.
- If the agent emits an operation whose coordinates are outside the screenshot dimensions, desktop must reject or display it as invalid instead of executing it.
- If desktop executes or rejects an operation, the next screenshot is the only protocol input that communicates the resulting observed state back to the agent; no separate operation-result frame is required for the main flow.
- If desktop sends an out-of-order screenshot or reuses a consumed screenshot sequence, both sides continue to enforce the step3.a monotonic sequence rules and return a warning rather than advancing state.
- If the WebSocket disconnects during an invoke, the agent must not lose the ability to be explicitly deleted, and any later reconnect must surface a consistent current status.
- If no real Windows window operation is available in the test environment, the automated testplan still validates protocol behavior and leaves real window execution for Windows manual acceptance.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The agent service MUST replace the step3.a deterministic runtime with a runtime that performs at least one real model-driven reasoning pass for each accepted screenshot input.
- **FR-002**: The runtime MUST be limited in this phase to a single primary agent and MUST NOT require subagents or long-term memory for the step3.b acceptance path.
- **FR-003**: The runtime MUST continue to accept and emit the existing AgentFrame payload categories needed by step3.a: screenshot input, text output, thinking output, operation output, warning/status information, wait or screenshot-request information, and screenshot-driven continuation.
- **FR-004**: The runtime MUST stream progressive user-visible events during an invoke as AgentFrames over the existing Connect stream, so desktop can display thinking, text, tool activity, operation progress, or warning information before the session advances to the next screenshot.
- **FR-005**: The runtime MUST support MCP/tool capabilities that allow the agent to request exactly one desktop operation at a time from the supported operation set: left/right single click, left/right double click, and keyboard key action.
- **FR-006**: Agent operation coordinates MUST use screenshot-relative pixel coordinates, and desktop remains responsible for converting them to local window or screen coordinates.
- **FR-007**: Agent creation MUST require an `agent_profile_name` and load the corresponding enabled profile from prompt service before the agent becomes usable.
- **FR-008**: Agent creation MUST load all enabled tool-independent SKILLS referenced by the selected profile and reject creation when a required referenced SKILL is missing or disabled.
- **FR-009**: Agent creation MUST validate that every MCP name referenced by the selected profile is supported by the agent runtime.
- **FR-010**: Runtime-owned MCP, tools, and runtime-related SKILLS MUST be owned by the agent service, while tool-independent SKILLS MUST remain managed by prompt service.
- **FR-011**: A profile MUST identify the model/provider selection used by the agent, including support for the default model provider path and OpenCode Go model references.
- **FR-012**: OpenCode Go support MUST use model references in the `opencode-go/<model-id>` form and MUST treat unsupported or malformed model references as configuration errors.
- **FR-013**: Provider credentials required by the selected model MUST be supplied through the deployment secret mechanism and MUST NOT be hard-coded in profile, prompt, desktop, or agent request content.
- **FR-014**: Missing, empty, unreadable, invalid, or unauthorized OpenCode Go provider credentials MUST make CreateAgent fail with an explicit user-visible error when the selected profile requires OpenCode Go, and the system MUST NOT silently fall back to another provider.
- **FR-015**: A single invoke MUST have a default maximum duration of 10 minutes; exceeding it MUST terminate that invoke and report a timeout state or warning.
- **FR-016**: An agent MUST be automatically deleted after 30 minutes with no new input, no active invoke, and no pending operation awaiting the next screenshot observation.
- **FR-017**: Automatic idle deletion MUST NOT delete an agent that is actively invoking or waiting for desktop to send the next screenshot after an operation.
- **FR-018**: DeleteAgent MUST remain idempotent; deleting a missing or already-cleaned agent succeeds without returning an error.
- **FR-019**: Replacing the agent runtime MUST preserve the existing gateway, proxy, session, prompt, HTTP resource paths, WebSocket play entrypoint, and AgentFrame Connect stream used by desktop.
- **FR-020**: Desktop MUST provide prompt/profile/SKILL management surfaces sufficient to create, list, inspect, and delete the resources needed to configure a step3.b agent.
- **FR-021**: Desktop play UI MUST use a conversation-style timeline where desktop image messages are collapsed by default and agent thinking, text, operations, warnings, and local operation execution status are visually distinguishable.
- **FR-022**: Automated acceptance MUST use testplan to cover the service chain, profile loading, SKILL loading, provider credential errors, minimal model-driven invoke, operation emission, timeout handling, idle deletion, sequence warnings, and DeleteAgent idempotency.
- **FR-023**: Real Windows window binding, coordinate conversion, and native operation execution MUST remain manually acceptable when not available in the automated testplan environment.

### Key Entities *(include if feature involves data)*

- **Agent Runtime**: The per-session execution environment that combines model selection, system prompt, runtime tools, loaded SKILLS, current invoke state, and lifecycle timers.
- **Agent Profile**: A prompt-service managed configuration selected at agent creation; includes model/provider choice, system prompt, tool-independent SKILL names, MCP names, and enabled state.
- **Tool-Independent SKILL**: Prompt-service managed guidance content used for game rules or gameplay strategy and loaded dynamically through profile references.
- **Runtime Tool/SKILL**: Agent-service owned capability or instruction bundle tied to MCP/tools and desktop operation output.
- **Provider Credential**: Secret-backed credential material required by the selected model provider, including OpenCode Go API key material.
- **Invoke Cycle**: One reasoning pass initiated by a screenshot input, streaming progressive AgentFrame events in real time and producing at most one pending desktop operation; after the operation, the next screenshot is the continuation input.
- **Desktop Operation**: A mouse or keyboard action requested by the agent and executed by desktop only after validation and user workflow permits it.
- **Play Conversation Item**: A user-visible timeline item representing desktop screenshot input, agent thinking, text, operation, warning, or local operation execution status.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of valid profile-backed agent creations can complete a screenshot-to-agent-output cycle through the existing desktop/gateway/proxy/session path without changing public resource paths.
- **SC-002**: 100% of automated step3.b testplan runs verify that a screenshot can produce at least one real-time model-driven progress frame and a valid operation frame associated with the originating screenshot.
- **SC-003**: 100% of profile references to missing, disabled, or unsupported SKILLS/MCP names fail before the agent is reported usable.
- **SC-004**: 100% of OpenCode Go model selections without readable, valid, and authorized credentials fail during CreateAgent with an explicit provider credential error and do not create an unusable agent or use another provider.
- **SC-005**: 100% of invokes exceeding the 10-minute default limit end with an observable timeout status or warning.
- **SC-006**: 100% of agents idle for more than 30 minutes with no active invoke and no pending operation awaiting a next screenshot are deleted automatically, while active or pending-operation agents are retained.
- **SC-007**: 100% of desktop play sessions display streamed agent thinking/text/tool-progress/operation/warning items and local operation execution status as distinct conversation items, and desktop screenshot messages are collapsed by default.
- **SC-008**: Existing step3.a protocol acceptance cases for screenshot transfer, screenshot-only continuation after operation execution, sequence warnings, and DeleteAgent idempotency continue to pass after the runtime replacement.
- **SC-009**: A tester can configure a profile, create an agent, send a screenshot, inspect agent reasoning output, execute or reject the proposed operation, and send the next screenshot in under 5 minutes on a prepared environment.

## Assumptions

- Step3.a prompt service, AgentFrame schema, Connect stream, session/proxy/gateway routing, screenshot encoding, operation protocol, screenshot-only continuation, and monotonic sequence constraints are the baseline and remain in scope as compatibility requirements.
- The implementation plan must honor the README direction that step3.b switches the agent service runtime to a TypeScript grpc-js service using the minimal LangChain DeepAgents capability set, while this specification describes the user-visible behavior and acceptance boundaries.
- The phrase "default deepagent provider" means the existing model-provider path supported by the selected deepagent ecosystem, while OpenCode Go is an additional explicitly supported provider path.
- OpenCode Go credential material is supplied through the repository's deploy secret configuration and exposed to the agent service at runtime; the exact file or environment binding is a planning detail, but validation occurs during CreateAgent for profiles that select OpenCode Go.
- Step3.b requires real model-driven inference but only the minimal single-agent path; subagents, long-term memory, autonomous repeated screenshot capture, and self-updating strategy memory remain outside this phase.
- Prompt/profile/SKILL CRUD capabilities from step3.a are available as the starting point, but desktop may need UI changes to make them usable for configuring step3.b.
- Automated acceptance can validate native window-operation protocol behavior without requiring a real Windows desktop; actual native mouse/keyboard execution remains a Windows manual acceptance item.
