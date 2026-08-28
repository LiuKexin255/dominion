# Feature Specification: Game Agent v2 — dsh 迁移 Step 1：session 对话页面与模型接入

**Feature Branch**: `049-agent-v2-dsh-init`

**Created**: 2026-08-27

**Status**: Draft

**Input**: User description: "为 @projects/game/agent/ 迁移至 dsh 框架的step 1。本阶段目标完全 session 对话页面迁移并接入模型，可以完成对话：
1. 为新的 agent_v2和 web (网页服务)项目进行初始化，并且迁移 session 管理和session（如果未特殊说明，session 都是指 game 领域内的session）页面
2. web 包含 session 管理页面和 session 对话页面。session 对话页面支持 text、think 以及 tools，其实现和页面风格参考 dsh-web（最好能复用 dsh-web 的前端代码或组件）。
3. 注意不要过度迁移当前 desktop 和 agent 的功能，agent_v2 和 web 只包含现阶段的所需的功能。
4. 模型接入 GLM codingplan https://docs.bigmodel.cn/cn/coding-plan/tool/others，采用 openai response 协议，token 跟 agent 一样，采用 secret 提供。"

## Motivation

`projects/game/agent/` 是基于 LangChain 自组装的 agent（TeamService + 内存 checkpoint + 桌面操作桥），对话入口为 desktop（Wails）应用。dsh（DeepSeek Harness）迁移已具备全部前置基础：`third_party/dsh/core` 框架核心底座与 B1 进程内嵌入模式已经由 `specs/047-dsh-chat-demo` 实证落地（样板：`experimental/dsh/demo/agent/`，含 boot/组合清单/`ctx.agents` 驱动/官方 LLM 适配器 baseURL 复用等结论，见 `specs/047-dsh-chat-demo/research.md` D1–D10）。

本 feature 是 game agent 迁移至 dsh 框架的**第一步**：以两个新项目 `projects/game/agent_v2`（嵌入 dsh 的 agent 服务）与 `projects/game/web`（网页服务）为载体，把**session 管理 + session 对话页面**从 desktop 迁移到网页，并首次接入真实模型——GLM codingplan（OpenAI Responses 协议端点 `https://open.bigmodel.cn/api/v1`，见 https://docs.bigmodel.cn/cn/coding-plan/tool/others ），达成"在浏览器里对 game session 完成一次流式对话（text/think 端到端可见，tools 渲染能力就绪——本阶段 agent 零工具，见 Clarifications）"。

范围纪律（用户需求 3）：本阶段**只做**对话所需的最小集合；team 模式、desktop 操作桥（鼠标/截图）、saolei/memory MCP、prompt/memory 服务联动等**一律不迁移**；现有 `projects/game/agent`、`projects/game/desktop`、`projects/game/proxy` 等服务保持原样、继续可用。

页面迁移的**行为基线**来自 desktop 前端（`projects/game/desktop/frontend/src/components/` 的 SessionList/ChatView/ChatMessage：列表/新建/删除/切换、text/think/tool 渲染、流式合并），**风格与实现基线**来自 dsh Web UI（https://github.com/deepseek-ai/deepseek-harness ，版本线 0.1.1-rc.2，与 `third_party/dsh/core` 同线）。

## Clarifications

### Session 2026-08-27

- Q: web 与 agent_v2 的架构路线（dsh-web 复用级别）？ → A: **B——双服务 + 组件级复用**：agent_v2 为嵌入 dsh 的 gRPC agent 服务（样板 `experimental/dsh/demo/agent/`）；web 为独立网页服务，前端复用 `@deepseek-ai/dsh-client-ui-primitives` 纯 React 组件，think 折叠与工具卡片交互参照 dsh-web 源码自建；session 管理沿用现有 game session 服务。
- Q: agent_v2 本阶段的工具范围？ → A: **零工具**：本阶段不启用任何工具（组合清单裁剪全部工具面）；对话页仍实现 tools 渲染能力（数据模型与组件），其端到端验证推迟到后续第一个工具实现的 step 再进行。
- Q: 同一 session 回合进行中用户再次发送消息如何处理？ → A: **排队**：新消息入队并显示排队指示，当前回合结束后自动按序发送（对齐 desktop 队列行为基线 `specs/030-queued-chat-input`、`specs/038-queue-input-mid-turn`）。
- Q: web 服务对外（浏览器）的入口采用哪种暴露形态？ → A: **页面独立暴露 + API 统一经 gateway**：前端页面由 web 服务自身 HTTP 监听直接 serve；API 全部经存量 gateway 访问——session 管理复用既有 `/api/v1` REST 路由（零改动），agent_v2 对话 API 由 gateway **新增路由**暴露并绑定 **`/api/v2` 前缀**（与旧接口区分）；此为 FR-010 存量不动原则的唯一例外（仅新增、不改既有路由）。
- Q: 本阶段新建 session 使用什么 template？ → A: **复用 `saolei`**：存量 session 服务零改动；网页对话 session 与桌面扫雷 session 共用同一 template 的 session 空间。
- Q: 对话页刷新后是否需要回填会话已有对话历史？ → A: **需要**：agent_v2 维护每会话内存对话记录并暴露历史查询 API；刷新/重连后对话页完整回填（agent_v2 存活期间，对齐 desktop 行为基线）。
- Q: session 正在对话中被删除时如何处理进行中回合与会话资源？ → A: **立即终止释放**：删除成功后 agent_v2 立即释放该会话的 dsh 资源（dispose），进行中回合终止、未发送的排队消息作废。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 在网页上新建 session 并完成一次模型对话 (Priority: P1)

用户用浏览器打开 web 服务的页面，在 session 管理页面新建一个 game session 并进入对话页，输入一条消息发送；消息经 gateway 新增的 `/api/v2` 对话 API（FR-013）发送给 agent_v2（dsh 驱动的 agent 会话），agent 调用 GLM codingplan 模型生成回复；回复正文（text）以流式方式渐进出现在对话页中。同一 session 内继续发送第二条消息，模型回复能感知先前轮次内容（多轮连续性）。

**Why this priority**: 这是本阶段的 MVP 与最终目标（"完全 session 对话页面迁移并接入模型，可以完成对话"）的最小完整切片：单此一条用户故事即可独立演示"网页 + dsh agent + 真实模型"的端到端价值。

**Independent Test**: 部署新服务后，浏览器（或直接调用 gateway 的 `/api/v2` 对话 API）新建 session、发送消息，断言在 60 秒内收到模型回复且回复以流式渐进呈现；再发第二条消息断言回复依赖首轮上下文。

**Acceptance Scenarios**:

1. **Given** 新服务已部署且模型 token 已配置，**When** 用户在 session 管理页新建 session 并进入对话页发送消息 M1，**Then** 对话页渐进式（流式）呈现模型回复 R1，回复完成后本轮对话结束、可继续输入。
2. **Given** session S 已完成 M1→R1 一轮，**When** 用户在同一 session 内发送 M2，**Then** 模型回复体现对 M1/R1 上下文的感知（多轮连续性）。
3. **Given** 用户在另一个新建 session S' 中发送与 M1 相同的消息，**Then** S' 的对话与 S 互不干扰（会话隔离）。
4. **Given** session S 的当前回合仍在进行，**When** 用户再次发送消息，**Then** 该消息入队并呈现排队指示，当前回合结束后自动按序发送（FR-012）。

---

### User Story 2 - 思考过程（think）可见且与正文区分 (Priority: P1)

当模型（GLM 为推理模型）在生成回复过程中产出思考内容时，对话页将 think 内容以与正文（text）可区分的方式呈现：默认折叠、可展开查看，流式期间有渐进更新的呈现；正文与思考不混排在同一段落里。

**Why this priority**: think 展示是用户明确要求的对话页三项能力之一（text、think、tools），也是推理模型体验的核心；GLM 模型天然产出推理内容，缺失该能力则对话页迁移不完整。

**Independent Test**: 在部署实例上触发一次包含思考内容的模型回复，断言页面/接口层可以分别获取 text 内容与 think 内容，且 think 以折叠/可展开形态与正文区分展示。

**Acceptance Scenarios**:

1. **Given** 模型回复包含思考内容，**When** 回复流式生成，**Then** think 内容渐进呈现于可折叠区域，正文（text）单独呈现，两者可区分。
2. **Given** 某轮模型回复不含思考内容，**When** 该轮渲染，**Then** 页面正常呈现正文，不出现空的思考区域。

---

### User Story 3 - 对话页具备工具调用（tools）渲染能力 (Priority: P2)

对话页具备工具调用的渲染能力：给定工具调用内容（名称、输入参数、执行状态、结果），页面将其作为独立于正文的块呈现，tool_call 与 tool_result 关联展示——行为对齐 desktop ChatView 的工具气泡。本阶段 agent_v2 零工具（FR-006），该能力以构造数据在页面/接口层验证；端到端（真实工具调用）验证推迟到后续第一个工具实现的 step。

**Why this priority**: tools 是用户明确要求的三项对话页能力之一；本阶段仅交付渲染能力（数据模型与组件），端到端真实链路随工具接入在后续 step 验证。

**Independent Test**: 以构造的工具调用数据驱动对话页（或其渲染组件），断言名称/参数/状态/结果可区分、与正文区分且关联呈现。

**Acceptance Scenarios**:

1. **Given** 一组构造的对话内容包含工具调用（名称/参数/执行中状态），**When** 页面渲染该内容，**Then** 工具调用块独立呈现调用中状态（名称/参数）。
2. **Given** 构造内容中该工具调用带有执行结果（成功/失败与输出），**When** 渲染，**Then** 结果与调用关联展示于同一工具块。
3. **Given** 构造的一轮对话包含"正文 + 思考 + 多次工具调用"，**When** 该轮渲染，**Then** 三类内容各自以可区分的形态按发生顺序呈现。

---

### User Story 4 - session 管理页面完整能力 (Priority: P2)

用户在 session 管理页面可以查看 game session 列表（含创建时间）、新建 session、删除 session、点击进入/切换到某个 session 的对话页；对齐 desktop SessionList 的行为基线。

**Why this priority**: "web 包含 session 管理页面和 session 对话页面"的用户要求；创建/进入的最小能力已并入 US1，本故事补齐列表与删除的完整管理能力。

**Independent Test**: 通过页面（或 web 服务 API）执行 新建 → 列表可见 → 进入对话 → 返回列表 → 删除 的完整闭环，断言每步状态正确。

**Acceptance Scenarios**:

1. **Given** 用户处于 session 管理页，**When** 新建 session，**Then** 列表出现新条目并可进入其对话页。
2. **Given** 列表中存在 session S，**When** 删除 S，**Then** S 从列表消失且其对话页不再可达。
3. **Given** session S 的对话回合正在进行中（或有排队消息），**When** 删除 S，**Then** 进行中回合终止、排队消息作废、agent_v2 释放该会话资源，S 从列表消失（FR-015）。
4. **Given** 存在多个 session 且其一正在对话中，**When** 用户切换到另一 session，**Then** 对话页展示对应 session 的内容，互不串扰。

---

### Edge Cases

- **模型端点不可达/超时**：本轮对话以明确错误在页面呈现，agent_v2 与 web 服务进程存活，后续轮次可恢复。
- **token 无效或缺失**：在启动或首次调用时明确报错（fail-loud），不静默降级；错误信息不泄露 token 内容。
- **页面刷新/重连**：对话页刷新后可重新进入会话并看到该会话已有的对话内容，回填内容与此前流式呈现一致（agent_v2 存活期间，FR-014）。
- **agent_v2 重启**：session 列表仍完整（session 元数据持久于现有 session 服务）；对话历史为内存态、随进程丢失——本阶段接受该限制（与现有 agent 的内存 checkpoint 行为一致，见 Assumptions）。
- **超长会话**：上下文随轮次单调增长，本阶段不做压缩（与 `specs/047-dsh-chat-demo` 的纯 chat 组合限制一致）。
- **并发多 session 对话**：多个 session 并发收发消息互不串扰、不阻塞。
- **回合进行中的新输入**：同一 session 回合进行中收到的新用户消息入队（不丢失、不拒绝），页面呈现排队指示；当前回合结束后自动按序发送（FR-012）。
- **对话中删除 session**：删除成功后进行中回合终止、排队消息作废、会话资源立即释放（FR-015）；删除后以相同资源名新建的 session 为全新会话。
- **非法输入**：空消息等非法请求得到明确提示，服务不崩溃。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 初始化两个新项目：`projects/game/agent_v2`（agent 服务）与 `projects/game/web`（网页服务），遵循仓库既有服务交付形态（bazel 构建、`service.yaml`/`deploy.yaml` 部署声明、服务发现寻址），并纳入 game 域部署。
- **FR-002**: agent_v2 MUST 以 dsh 进程内嵌入（B1 模式，`survey/deepseek-harness-b1-bazel-packaging.md` §5.4）方式运行：启动时按声明式组合清单（cordis.yml）组装插件树、fail-loud（解析/peer 失败即携带诊断退出）、收到终止信号优雅释放后退出——对齐 `experimental/dsh/demo/agent/` 已实证的样板。
- **FR-003**: session 管理 MUST 沿用 game 域 session 模型（`templates/{template}/sessions/{session}`），管理数据面基于现有 game session 服务（`projects/game/session/`，Mongo 持久化）复用而非重建。
- **FR-004**: session 对话页 MUST 流式渲染模型输出：正文（text）渐进呈现；思考内容（think）MUST 与正文区分展示（默认折叠、可展开），流式期间渐进更新。
- **FR-005**: session 对话页 MUST 具备工具调用渲染能力：工具名称、输入参数、执行状态与结果，作为独立于正文的内容块，且工具调用与其结果关联展示；本阶段 agent_v2 零工具（见 FR-006），该能力以构造数据在页面/接口层验证，端到端验收安排见 FR-006。
- **FR-006**: agent_v2 本阶段 MUST NOT 启用任何工具（组合清单裁剪全部工具面）：工具接入及对话页 tools 的端到端验证推迟到后续第一个工具实现的 step（2026-08-27 澄清）。
- **FR-007**: agent_v2 MUST 通过 GLM codingplan 接入模型，采用 OpenAI **Responses** 协议（Base URL `https://open.bigmodel.cn/api/v1`，https://docs.bigmodel.cn/cn/coding-plan/tool/others ）；模型 id 可配置（默认 `glm-5.2`）；以 dsh LLM 适配插件形态接入（官方仅有 chat-completions 适配器，Responses 需自研适配插件——`specs/047-dsh-chat-demo/research.md` D1 回退路径），模型端点地址 MUST 可经配置替换（供确定性测试以 fake 端点替换真实端点）。
- **FR-008**: 模型 API token MUST 与现有 agent 一致经仓库 secret 机制提供：`service.yaml` artifact 声明逻辑 secret 名 + `deploy.yaml` 绑定 k8s secret + 运行期经 `DOMINION_SECRET_DIR` 文件读取（`specs/002-deploy-secret-config/` 契约）；token MUST NOT 出现在代码、配置明文或镜像中。
- **FR-009**: web 对话页的实现与页面风格 MUST 参考 dsh-web（https://github.com/deepseek-ai/deepseek-harness ），复用级别为**组件级复用**（2026-08-27 澄清）：前端复用 `@deepseek-ai/dsh-client-ui-primitives`（零 Cordis 依赖的纯 React 组件库，0.1.1-rc.2 同线）的 markdown/代码块等渲染组件；think 折叠、工具卡片等交互组件参照 dsh-web 源码（`dsh-client-ui-conversation`/`dsh-client-ui-tool`）自建。
- **FR-010**: 范围边界：agent_v2 与 web MUST NOT 迁移 team 模式、desktop 操作桥（鼠标/截图）、saolei/memory MCP、prompt/memory 服务联动等本阶段不需要的能力；现有 `projects/game/agent`、`projects/game/desktop`、`projects/game/proxy` 等存量服务及其链路 MUST 保持不变。唯一例外（2026-08-27 澄清）：存量 gateway 允许**新增** `/api/v2` 前缀的对话路由（agent_v2 对话 API 的暴露通道，见 FR-013），MUST NOT 修改其既有 `/api/v1` 路由与行为。
- **FR-011**: 系统 MUST 附带大型测试（testplan）：部署新服务（模型端点以确定性 fake 替换、零外部网络依赖），经验收入口验证 session 管理闭环（US4）、端到端对话（US1/US2 的 text/think 流式可区分获取、多轮连续性）与刷新后历史回填一致性（FR-014），完成清理；tools 渲染能力（US3）以构造数据在页面/接口层经组件单测验证（FR-005，随编译+单测门禁执行，本阶段 agent_v2 零工具、无真实工具块流经系统）；验收标准为经 testplan skill（`guitar run`）实际执行完整部署→测试→清理闭环且**全部用例通过**（`.specify/memory/constitution.md` 原则 VI）。
- **FR-012**: 同一 session 内回合进行中收到的新用户消息 MUST 排队：对话页呈现排队状态（入队消息与数量可见），当前回合结束后按序自动发送入队消息（2026-08-27 澄清，行为对齐 desktop 队列基线 `specs/030-queued-chat-input`、`specs/038-queue-input-mid-turn`，仅迁移其最小行为、不迁移 observe-only 等扩展）；不同 session 之间互不排队、互不阻塞。
- **FR-013**: 对外暴露形态 MUST 为"页面独立 + API 经 gateway"（2026-08-27 澄清）：web 服务以自身 HTTP 监听直接 serve 前端页面；浏览器侧 API 统一访问存量 gateway——session 管理复用既有 `/api/v1` 路由，agent_v2 对话 API（流式）经 gateway **新增路由**暴露、绑定 **`/api/v2` 前缀**（gateway 对 agent_v2 经服务发现寻址；路由增量例外边界见 FR-010）。
- **FR-014**: agent_v2 MUST 为每个会话维护内存态对话记录并暴露历史查询 API（2026-08-27 澄清，对齐 desktop `ListMessages` 行为基线）：记录以内容块（text/think/tool call/tool result）形式保序、保分类；对话页刷新/重连后经该 API **完整回填**会话已有对话内容（agent_v2 存活期间）；记录随 agent_v2 进程重启丢失（内存态，见 Assumptions），且回填内容与此前流式呈现的内容一致。
- **FR-015**: session 删除的生命周期 MUST 为"立即终止释放"（2026-08-27 澄清）：session 元数据删除成功后，agent_v2 MUST 立即释放该会话占用的 dsh 资源（dispose）——进行中的回合终止、未发送的排队消息作废、该会话的内存对话记录不再可查询；随后对同一 session 资源名的新建得到全新会话（无残留状态）。

### Key Entities

- **Game Session**: game 域会话，资源名 `templates/{template}/sessions/{session}`，元数据（创建时间等）由现有 session 服务持久化；本阶段新建 session 固定使用 `saolei` template（2026-08-27 澄清，存量 session 服务零改动），网页对话 session 与桌面扫雷 session 共用该 template 的 session 空间（列表互通）；一个 game session 映射 agent_v2 内一个 dsh agent 会话（映射方式对齐 demo 样板的 get-or-create 模式）。
- **Chat Turn（对话轮次）**: 一轮"用户消息 → agent 回复"；回复由若干内容块构成。
- **Content Block（内容块）**: 对话内容的分类单元——text（正文）、think（思考）、tool call（工具调用）、tool result（工具结果）；行为基线对齐 `projects/game/game.proto` 的 MessagePart 语义与 desktop ChatView 的渲染规则。
- **Tool Call（工具调用）**: 名称、输入参数、执行状态、执行结果。
- **dsh Composition Manifest（组合清单）**: agent_v2 启动时消费的声明式插件组装清单（每行 = 启用的插件 + 配置），是启用面的唯一事实源（`experimental/dsh/demo/agent/cordis.yml` 样板）。
- **Model Endpoint（模型端点）**: GLM codingplan 的 OpenAI Responses 协议端点（`https://open.bigmodel.cn/api/v1`）+ 经 secret 提供的 API token + 可配置模型 id；测试态可替换为确定性 fake 端点。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 大型测试全部通过：session 管理闭环与端到端对话（含多轮连续性）的所有用例 100% 通过（零 failed、零 flaky），且执行期间零外部网络依赖。
- **SC-002**: text 与 think 在端到端对话中可区分获取且以流式渐进方式呈现；tools 渲染能力以构造数据在页面/接口层验证可用（端到端验证随后续第一个工具接入进行）。
- **SC-003**: 端到端对话可完成：从新建 session 到收到模型回复的完整路径在部署环境中可稳定走通（大型测试外，真实 GLM 端点的冒烟步骤在交付文档中记录并可手工复验）。
- **SC-004**: token 零泄漏：全部交付物（代码、构建产物、部署声明、文档示例）中不存在明文模型 token；token 仅经 secret 机制注入运行时。
- **SC-005**: 存量零回归：现有 agent/desktop/proxy 等存量服务的既有测试与部署不受本变更影响；存量 gateway 的既有路由（`/api/v1`）行为与测试不变（仅允许新增 `/api/v2` 路由）。

## Assumptions

- **dsh 版本线**：全家族按 0.1.1-rc.2 同线精确 pin（对齐 `third_party/dsh/core` 与 `specs/047-dsh-chat-demo` 的锁定决策；dist-tag 不可信）。
- **GLM Responses 适配为自研 dsh 插件**：官方 `dsh-llm-deepseek` 适配器仅支持 chat-completions wire（`specs/047-dsh-chat-demo/research.md` D1），用户指定 Responses 协议，故需自研 LLM 适配插件（实现 dsh LLM 适配缝，注册路由）。
- **大型测试的确定性**：真实 GLM 端点不进大型测试（外部网络/成本/非确定性）；FR-007 的可替换端点配置使测试以 Responses 协议的确定性 fake 服务替代（扩展 `projects/game/fake-llm` 新增 Responses 端点，见 `specs/049-agent-v2-dsh-init/research.md` D7）；真实端点接入以手工冒烟验证并记录在交付文档。
- **对话历史为内存态**：agent_v2 内对话历史随进程重启丢失（与现有 agent 内存 checkpoint 行为一致），存活期间经内存记录 + 历史查询 API 支撑刷新回填（FR-014）；session 列表因复用 session 服务而持久。dsh persistence 插件接入留待后续 step。
- **template 复用 saolei**：本阶段新建 session 固定使用 `saolei`（2026-08-27 澄清），存量 session 服务的 template 校验零改动；网页与桌面的 session 列表互通（同一 template 空间）。引入独立对话 template 留待后续 step 评估。
- **web 服务内网暴露、无鉴权**：与 game 现有网关一致（内网环境）；暴露形态已定为"web 直接 serve 页面 + API 经 gateway（session 复用 `/api/v1`、对话走新增 `/api/v2`）"（FR-013）。
- **流式呈现为本阶段要求**：对齐 dsh-web 与 desktop 的既有体验；仅非流式回复不构成"完全迁移"。
- **样板复用**：agent_v2 的 dsh 嵌入骨架（boot 前 endpoint 注入、fail-loud、优雅退出、get-or-create 会话注册表、回合事件收集）对齐 `experimental/dsh/demo/agent/` 实证模式。
- **存量不动**：现有 agent/desktop/proxy/session/prompt/memory/gateway 服务与 desktop 发布链路本阶段不下线、不修改既有行为；唯一例外为存量 gateway 新增 `/api/v2` 对话路由（FR-010/FR-013）；新服务与存量并存部署。
