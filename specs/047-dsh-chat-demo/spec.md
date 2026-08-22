# Feature Specification: dsh Chat Demo — grpc-js 服务进程内嵌入 dsh

**Feature Branch**: `047-dsh-chat-demo`

**Created**: 2026-08-22

**Status**: Draft

**Input**: User description: "为 grpc-js 服务嵌入 dsh 开发一个 demo，前置调研 `survey/deepseek-harness-b1-bazel-packaging.md`。
1. 一个 grpc-gateway，一个嵌入了 dsh 的 grpc-js 服务，一个实现了 openai response 或 completions（response 优先）接口的 fake-llm 服务（参考 `projects/game/fake-llm/`），放到 `experimental/dsh/demo` 目录下。
2. 实现 third_party/dsh 依赖 target，本次只实现核心框架，框架内不包含任何插件。
3. 最终目标是实现一个 chat agent，可以通过 gateway 访问 dsh 服务。验收方法是通过 testplan 执行一个大型测试。
4. 如果需要的话，为 fake-llm 开发一个自研插件；除此以外 chat 其他的依赖则使用官方插件。"

## Motivation

dsh（DeepSeek Harness，`@deepseek-ai/*` npm 包族）的 B1 集成模式调研已完成并锁定关键决策（`survey/deepseek-harness-b1-bazel-packaging.md` 状态栏，2026-08-22）：dsh 依赖 target 仅含**框架核心**（≈11 包，与 plan/research D6 的枚举一致），插件（含官方）全部按需声明；底座以"闭包清单 workspace 包"形态进入现有 bazel/pnpm 构建图（`survey/deepseek-harness-b1-bazel-packaging.md` §5.1/§5.4.1）。但这一切至今停留在纸面——本仓库尚无任何服务真正嵌入 dsh 运行。

本 feature 是该调研的**实证（PoC）**：用现有构建链路（`artifact_pkg_js`/`artifact_image`、Go grpc-gateway、testplan 大型测试，参照 `experimental/grpc_chain/` 与 `projects/game/agent/` 的既有样板）搭一条完整的最小 chat 链路——**grpc-gateway（HTTP 入口）→ 嵌入 dsh 的 grpc-js 服务（B1：进程内 `boot()` 组合插件树）→ fake-llm（脚本化模型端点）**——验证：

1. `third_party/dsh` 依赖 target 的"框架核心 + 插件按需"形态真正可用（锁定决策的第一次落地）；
2. "最小 chat = agent spine 行 + LLM adapter 行 + 零服务面"的组合量度成立（`survey/deepseek-harness-b1-bazel-packaging.md` §5.4.2）——业务进程直接驱动框架的 agent 驱动面（`ctx.agents`），无需额外服务面插件；
3. 确定性模型行为（fake-llm）足以支撑无外部依赖的端到端大型测试验收（Constitution VI：`testplan` skill 全流程 `guitar run`，全部用例通过）。

## Clarifications

### Session 2026-08-22

- Q: fake-llm 的对外接口与 LLM 适配策略应锁定哪个方向：直接锁定 OpenAI Responses API + 自研适配插件，还是在 plan 阶段优先实证"官方 DeepSeek 适配器能否通过配置 baseURL 指向自有 chat-completions 端点"以争取零自研适配器？ → A: **B——证据优先**：plan 阶段先实证官方 `dsh-llm-deepseek` 适配器能否以 baseURL 配置指向 fake-llm 的 chat-completions 接口；实证成功则零自研适配器（fake-llm 仅实现 chat-completions），失败则回退 Responses API + 自研适配插件。
- Q: 新的 fake-llm 服务用什么语言实现？ → A: **Go**——照搬 `projects/game/fake-llm/` 的模板匹配与打包模式（关键词匹配、确定性兜底、内嵌 testdata、`artifact_pkg_go` 链路），仅新增所需 wire 与多轮感知匹配。
- Q: 自研 LLM 适配插件对 fake-llm 的寻址方式应采用哪种？ → A: **Dominion 服务发现**（`@dominion/common-js-resolver` + `dominion:///` target，复用 `projects/game/agent/src/resolver-provider.ts` 先例）；并**放大到两条适配路径**：官方 `dsh-llm-deepseek` 适配器同样必须经 Dominion 寻址取得 fake-llm endpoint（运行期解析注入，而非静态地址）——该约束同时是 FR-007 实证门禁的必要条件（见 Q1）。
- Q: fake-llm 的多轮感知匹配机制应采用哪种？ → A: **最小扩展**：模板可选声明"历史关键词/轮次序号"附加匹配条件，未声明则退化为现有关键词匹配（与 game fake-llm 行为一致）；fake-llm 定位是测试基建，稳定、可支撑测试即可，不做复杂对话模拟。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 通过网关完成一次确定性聊天往返 (Priority: P1)

开发者（或大型测试用例）向 demo 的**唯一公共 HTTP 入口**发送一条聊天消息（携带会话标识与消息内容），消息依次经过网关 → dsh 服务（驱动框架内的 agent 会话与主循环）→ LLM 适配 → fake-llm（脚本化模板匹配），最终一条**确定性的脚本化回复**沿原路返回给调用方。整个链路无任何外部 LLM/网络依赖。

**Why this priority**: 这是本 feature 的 MVP 与最终目标（"可以通过 gateway 访问 dsh 服务"）的最小完整切片：单此一条用户故事即可独立演示 B1 嵌入的端到端价值，其余故事都是它的延伸。

**Independent Test**: 部署三个服务后，向公共 HTTP 入口发送命中某脚本模板的消息，断言收到的回复与该模板的脚本化回复完全一致（确定性断言，可重复执行）。

**Acceptance Scenarios**:

1. **Given** 三个服务（网关、dsh 服务、fake-llm）已部署，**When** 调用方向公共 HTTP 入口发送一条命中模板 A 的聊天消息，**Then** 收到的回复内容与模板 A 的脚本化回复一致。
2. **Given** 同一部署实例，**When** 再次发送相同消息，**Then** 收到相同回复（确定性、可重复）。
3. **Given** 调用方发送一条不命中任何模板的消息，**When** 消息走完整个链路，**Then** 收到 fake-llm 的确定性兜底回复（而非挂起、崩溃或随机内容）。

---

### User Story 2 - 多轮会话连续性 (Priority: P2)

调用方在同一会话标识下连续发送多条消息，后续轮次复用同一 agent 会话——fake-llm 的脚本化回复可以**感知到先前轮次的内容**（例如第二轮回复按规则引用第一轮内容），证明上下文在会话内正确传递；不同会话标识之间的消息互不干扰。

**Why this priority**: chat agent 区别于单轮问答的本质是会话连续性；这也是验证 dsh 官方 spine 插件（agent 服务 + 会话管理 + 主循环）被正确组合与驱动的信号。

**Independent Test**: 在同一会话标识下发送两轮消息，断言第二轮回复依赖第一轮内容（脚本模板按上下文变化）；再用第二个会话标识重复第一轮消息，断言其回复与第一个会话的第一轮相同（会话隔离）。

**Acceptance Scenarios**:

1. **Given** 会话 S 已完成第一轮对话（消息 M1 → 回复 R1），**When** 在会话 S 内发送第二轮消息 M2（其脚本化回复规则依赖 M1/R1 的在场），**Then** 收到的回复符合"已见 M1/R1"的脚本分支，而非首轮分支。
2. **Given** 会话 S 已有两轮对话，**When** 调用方以新会话标识 S' 发送 M1，**Then** S' 收到的回复与会话 S 的首轮回复相同（S 的上下文未泄漏进 S'）。
3. **Given** 两个会话 S1、S2 交错发送消息，**When** 各自完成一轮，**Then** 两个会话各自收到符合自身上下文的正确回复（并发隔离）。

---

### User Story 3 - dsh 依赖底座可复用且仅含框架核心 (Priority: P3)

其他服务的开发者可以引用 `third_party/dsh` 下的依赖 target 在自己的 grpc-js（或任意 Node/TS）服务中嵌入 dsh：该底座 target **只物化框架核心闭包**（boot 与 cordis 家族运行时，不含任何插件包）；任何插件能力（agent spine、LLM 适配器等）都必须由消费方服务显式声明。demo 的 dsh 服务本身就是第一个这样构建的消费者——它是该底座可用性的活证据。

**Why this priority**: 底座是本 feature 的平台性交付物，价值超出单个 demo（后续真实服务按同一模式嵌入）；但它以 US1 的链路作为验收载体，故列为 P3。

**Independent Test**: 对 demo dsh 服务的物化依赖闭包做审计：来自 `third_party/dsh` 底座 target 的包集合中**零插件包**；服务运行时闭包 = 底座核心 ∪ 服务显式声明的插件（官方 spine + LLM 适配 + 各自 peer 闭包），无第三来源。

**Acceptance Scenarios**:

1. **Given** 底座 target 单独被引用（无任何插件声明），**When** 审计其物化包集合，**Then** 集合内只有框架核心包，不含任何 `@deepseek-ai/dsh-*` 插件包。
2. **Given** demo dsh 服务声明了底座 + 所需插件，**When** 审计其完整运行时闭包，**Then** 每个插件包均可追溯到服务侧的显式声明（官方插件或自研插件），版本与底座同线。
3. **Given** 开发者试图启用一个未声明（不在物化集合内）的插件，**When** 服务启动，**Then** 启动失败并给出明确诊断（解析失败即失败，绝不静默降级——见 FR-009）。

---

### Edge Cases

- **fake-llm 不可达**：模型调用失败时，本轮对话以明确错误返回（HTTP 错误响应），dsh 服务进程存活且后续轮次可恢复——错误在链路中清晰传播，不被吞掉或挂起。
- **畸形/非法请求**：网关收到缺少必要字段或非法载荷的请求时返回明确的错误状态码，链路任何一层不得崩溃。
- **组合配置错误**（缺插件、peer 不满足、YAML 非法等）：dsh 服务启动即失败（fail-loud），携带可定位的诊断信息；不存在"半启动"状态。
- **超长会话**：纯 chat 组合无上下文压缩能力（`survey/deepseek-harness-b1-bazel-packaging.md` §5.4.2），长会话上下文单调增长——demo 范围内接受此限制并记录，不做压缩。
- **并发会话压力**：多个会话并发消息不产生串扰或死锁（US2 验收场景 3 覆盖）。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 提供唯一公共 HTTP 聊天入口（由网关对外暴露），接受会话标识 + 消息内容，返回本轮 agent 回复；v1 为非流式请求/响应语义。
- **FR-002**: 系统 MUST 支持多轮会话：同一会话标识的后续消息复用同一 agent 会话并可见先前轮次；不同会话标识彼此隔离。
- **FR-003**: dsh 服务 MUST 以进程内嵌入（B1 模式，`survey/deepseek-harness-integration-modes.md` §3.1）方式运行 dsh：启动时依据声明式组合清单组装插件树，通过框架的 agent 驱动面（`ctx.agents`：创建/续会话/追加消息）驱动对话；收到终止信号时优雅释放（dispose）后退出。
- **FR-004**: 仓库 MUST 提供 `third_party/dsh` 下的 dsh 依赖 target（框架核心底座）：为消费服务物化框架核心闭包；底座 MUST NOT 包含任何插件包——所有插件（官方与自研）由消费方显式声明（对应用户需求 2 与锁定决策"精确混合"，`survey/deepseek-harness-b1-bazel-packaging.md` §5.4.1）。
- **FR-005**: chat agent 的能力组合 MUST 优先使用官方 dsh 插件（agent spine 等，`survey/deepseek-harness-b1-bazel-packaging.md` §5.4.2 行①）；自研插件仅允许用于官方插件集无法覆盖的缺口（预期唯一缺口：fake-llm 的 LLM 适配，见 FR-006/FR-007）。
- **FR-006**: 系统 MUST 提供 fake-llm 服务（**Go 语言新实现**，位于 `experimental/dsh/demo`，照搬 `projects/game/fake-llm/` 的模板匹配模式）：对外提供与其 LLM 适配路径匹配的 OpenAI 兼容接口（chat-completions 或 Responses API，选型由 FR-007 的适配决策门禁决定），按请求内容匹配脚本化模板返回确定性回复——模板支持可选的多轮附加匹配条件（历史关键词/轮次序号，未声明则退化为现有关键词匹配）；未命中模板时返回确定性兜底回复；运行时零外部网络/真实 LLM 依赖。
- **FR-007**: LLM 适配路径 MUST 由 plan 阶段的**适配器实证门禁**决定（2026-08-22 澄清，证据优先）：①首选实证官方 `dsh-llm-deepseek` 适配器能否对接 fake-llm 的 chat-completions 接口——实证成功的必要条件包括：(a) endpoint 可由**运行期 Dominion 服务发现**解析注入（FR-011，不使用静态地址），(b) 无需真实 DeepSeek credentials 即可工作；成功则**零自研适配插件**，fake-llm 仅实现 chat-completions；②实证失败时回退：fake-llm 实现 OpenAI **Responses API**，系统提供自研 LLM 适配插件（dsh 插件形态：声明式组合清单中的一行）桥接框架 LLM 接缝到 fake-llm（寻址同样遵循 FR-011）。无论哪条路径，除 LLM 适配外 chat 其余依赖全部使用官方插件。（前置调研事实：官方仅有 chat-completions + Files API wire 的 DeepSeek 适配器，无 OpenAI 兼容/Responses 适配器——`survey/deepseek-harness-b1-bazel-packaging.md` §5.4.2 行②。）
- **FR-008**: 系统 MUST 附带一个大型测试（testplan）：部署全部三个服务，经公共 HTTP 入口验证聊天往返与多轮会话（覆盖 US1/US2 验收场景），并完成清理；验收标准为通过仓库标准大型测试工作流（testplan skill，`guitar run`）实际执行且**全部用例通过**（`.specify/memory/constitution.md` 原则 VI）。
- **FR-009**: dsh 服务启动 MUST fail-loud：组合清单启用行的解析失败、peer 缺失或激活失败时，进程以携带诊断的错误退出，绝不静默跳过或降级运行（`survey/deepseek-harness-b1-plugin-packaging.md` §2.1 步骤 8 的 fail-loud 审计语义）。
- **FR-010**: 三个服务（网关、dsh 服务、fake-llm）与大型测试均 MUST 位于 `experimental/dsh/demo` 目录下；dsh 依赖底座位于 `third_party/dsh`。
- **FR-011**: dsh 服务对 fake-llm 的模型端点寻址 MUST 走 Dominion 服务发现（2026-08-22 澄清）：组合清单/适配配置中声明 Dominion target（如 `dominion:///dsh-demo/fake-llm:8080`），运行期经 `@dominion/common-js-resolver` 模式解析为 endpoint（复用 `projects/game/agent/src/resolver-provider.ts` 先例）；该约束对官方适配器与自研适配插件两条路径**同等适用**，禁止静态地址。

### Key Entities

- **Conversation（会话）**: 由会话标识唯一确定，包含有序的对话轮次；生命周期为 demo 部署实例的存续期（无持久化）。
- **Chat Turn（对话轮次）**: 一轮"用户消息 → agent 回复"；回复由 agent 会话上下文与匹配的模型模板共同决定。
- **Model Response Template（模型响应模板）**: fake-llm 的脚本化回复定义——匹配规则（基于请求内容的关键词匹配 + **可选的多轮附加条件**：历史关键词/轮次序号，2026-08-22 澄清的"最小扩展"）+ 确定性回复内容；未命中时的兜底模板。fake-llm 定位为测试基建：稳定、可支撑测试即可，不做复杂对话模拟。
- **dsh Composition Manifest（组合清单）**: dsh 服务启动时消费的声明式插件组装清单（每行 = 一个启用的插件 + 其配置），是"启用面"的唯一事实源。
- **dsh Core Baseline（dsh 框架核心底座）**: `third_party/dsh` 下版本化（同 rc 线精确 pin）的框架核心包集合，是消费服务"解析面"的最小公共层。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 大型测试中，所有脚本化聊天往返断言 100% 通过——经公共 HTTP 入口发送的消息返回与模板逐字一致的确定性回复。
- **SC-002**: 多轮连续性可验证：同一会话的第二轮回复走"已见首轮上下文"的脚本分支，而新会话发送相同消息回到首轮分支（两种分支在测试中均被触发并断言）。
- **SC-003**: 大型测试通过标准工作流实际执行完整闭环（部署 → 全部用例 → 清理），全部用例通过（零 failed、零 flaky）；执行期间零外部 LLM/网络依赖（fake-llm 模板完全内嵌）。
- **SC-004**: 依赖闭包审计通过：`third_party/dsh` 底座 target 物化集合 = 框架核心包（零插件包）；demo dsh 服务运行时闭包中的每个插件包均可追溯到服务的显式声明。
- **SC-005**: 两个并发会话交错对话时，各自回复始终符合自身会话上下文（零串扰）。

## Assumptions

- **dsh 版本线**：dsh 全家桶按 0.1.1-rc 线**同线精确 pin**（dist-tag 不可信，`survey/deepseek-harness-b1-plugin-packaging.md` §4.2/§4.3）；0.x-rc 的破坏性变更风险被实验性 demo 接受，升级以 lockfile PR 方式整体进行。
- **仓库既有约定复用**：网关沿用仓库 Go grpc-gateway 样板（`experimental/grpc_chain/testplan/gateway/`，HTTP:80、proto `google.api.http` 注解路由、服务发现走 `app/service:grpc`）；grpc-js 服务沿用 `experimental/grpc_chain/mid/` 与 `projects/game/agent/` 样板（proto 位于应用根、`ts_proto_library`、`artifact_pkg_js`/`artifact_image`、`service.yaml`/`deploy.yaml`/testplan 格式）；构建图扩展遵循 `AGENTS.md` 的 pnpm/bazel 流程。
- **fake-llm 为新实现**：在 `experimental/dsh/demo` 下以 **Go** 新建（照搬 `projects/game/fake-llm/` 的关键词模板匹配、确定性兜底与内嵌 testdata 模式）；`projects/game/fake-llm/` 本身不在本 feature 范围内、不被修改。接口选型由 FR-007 的适配器实证门禁决定（证据优先：官方适配器 baseURL 复用成功 → 仅 chat-completions；失败 → Responses API + 自研适配插件），原始输入的"response 优先"由该门禁的回退路径承接。
- **非流式 v1**：聊天入口为非流式请求/响应；SSE/流式输出不在本 feature 范围。
- **会话持久化与压缩不在范围**：会话为内存态、随进程销毁；无上下文压缩（`survey/deepseek-harness-b1-bazel-packaging.md` §5.4.2 已记录为纯 chat 组合的固有限制，demo 接受）。
- **实验性质量线**：demo 无需 auth/secrets/生产化运维；goal 是验证嵌入模式与构建链路，而非交付生产服务。
- **jsonrpc 服务面插件不启用**：B1 的结构性优势是业务进程直接驱动 `ctx.agents`，无需标准 wire 服务面（`survey/deepseek-harness-b1-bazel-packaging.md` §5.4.2 行③）；dsh 服务对网关暴露自有 gRPC 契约即可。
- **验收环境**：大型测试经 testplan skill 在仓库标准环境中执行（`.specify/memory/constitution.md` 原则 VI）；fake-llm 作为 demo 依赖服务随 testplan 一同部署（其自身按测试基建定位，单独大型测试可豁免，参照 `projects/game/fake-llm/README.md` 的豁免先例）。
