# Feature Specification: dsh Preset Roster Demo — roster 机制验证与 preset 扩展实践

**Feature Branch**: `058-dsh-preset-roster-demo`

**Created**: 2026-09-08

**Status**: Draft

**Input**: User description: "为 roster 验证任务生成 spec，除了验证 roster 是否支持，目标还包括探索 preset 扩展、以及与 service 交互的最佳实践方式。另外，先将上面讨论的内容，保存为中间数据，待验证任务完成后，再根据实践过程，将 roster 验证结果写入 `survey/`"

## Motivation

saolei team 模式调研（`survey/deepseek-harness-team-mode.md`，2026-09-08 四轮决策）已拍板：双 agent 拓扑、preset 分池、角色差异全部由 preset 承载、物化零定制（头部决策 ③⑥⑩⑭）——但全部结论停留在源码级纸面推断，本仓库尚无任何 roster（`@deepseek-ai/dsh-agent-presets`）实证。

同时，agent_v2（`projects/game/agent_v2/`）现状是"无 roster 部署"（B1 直组形态）：组合由进程级 `cordis.yml` 一份固定、persona 经 declaration-merge 塞进 `AgentOptions`、preset 只是 Mongo 里一个 persona 字段——**preset 扩展逻辑错放在 service 层**（`projects/game/agent_v2/src/presets.ts` + `server.ts`），既无 per-session 组合能力，也无清晰的边界划分（2026-09-08 讨论结论，`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md` §1）。

本 feature 是三重目标的**实证（PoC）**，载体为扩展 047 demo（`experimental/dsh/demo`）：

1. **roster 机制验证**：per-session preset 选择、scope 解析链（`agent → preset → global`）、standing mount 共享、热创作发现、generation（文件 stamp 驱动）在 B1 嵌入形态下是否如调研结论成立；
2. **preset 扩展实践（C1 copy-then-patch）**：静态模板（部署数据）+ 动态字段（存储只存 persona 等必要部分）+ copy-then-patch 物化——数据库不存完整组合文件（冗余且大部分内容逻辑上属于模板）；
3. **service ↔ preset 扩展插件的边界与对接最佳实践**：扩展逻辑以 Dominion 插件包承载（不进 service 层），service 经 ctx 服务对接（API 输入注入与输出读取），两者边界划分清楚。

验证完成后，将 roster 验证结果**依据实践过程**写入 `survey/`（本 spec 的 `research.md` 为中间数据与底稿，见 Assumptions）。

## Clarifications

### Session 2026-09-08（roster 验证方案讨论，四轮）

- Q: 验证载体用扩展 047 demo 还是新建 sibling demo？ → A: **扩展 047 demo 本身**（demo 组合由两行扩为挂 roster，proto 扩 preset 面；047 已验证的链路作为承载）。
- Q: preset 扩展的三个候选形态（A 代码拼装 / B 直管最终组合文件 / C 静态模板+动态数据）选哪个？ → A: **C1（copy-then-patch）**——完整文件入库太重，大部分内容逻辑上属于"模板"，存储只存必要动态字段。前置机制事实：dsh 的 `preset.yml` 仅承载展示元数据、`@deepseek-ai/dsh-persona` 仅支持行内 `text`（无 file 输入），故"静态配置 + 动态 sidecar 数据文件"（C2）不存在，动态字段物化必然落组合文件的 persona 行（`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md` §2）。
- Q: 扩展逻辑的代码边界放哪？ → A: **通用 authoring 基座插件**（`common/js/dsh-plugins/` 下，与 llm-glm 同形态）——copy-then-patch 机制、存储 seam、roster 消费是场景无关的；场景差异只在模板内容与动态字段。service 层不触碰 roster API 与文件系统。
- Q: conversation 与 preset 的绑定面？ → A: **显式 CreateConversation RPC**（对齐 agent_v2 UpdateAgent 物化形态：先建会话定 preset，再发消息），不做 lazy 首条消息绑定。
- Q: demo 中动态数据的存储实现？ → A: **内存实现**（插件定义 Store 接口 seam，demo 注入内存实现；重启丢失为 demo 已知限制风格，Mongo 生产化留给 agent_v2 迁移时接同一 seam）。
- Q: 验证 per-preset 工具目录差异所需的 model-facing 工具插件放哪？ → A: **demo 本地 workspace 包**（小工具 + 配套 guidance 同包，preset 行以裸包名引用；demo 内容不进 `common/`）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 显式建会话选择 preset，组合随会话生效 (Priority: P1)

开发者（或大型测试用例）经 API **显式创建会话**并指定 preset（模板或经 API 创作的副本；缺省走 roster 默认），随后在该会话发送消息——agent 的 persona 与模型可见工具目录由所选 preset 决定：不同 preset 的两个会话呈现不同的组合表现，同 preset 的多个会话共享同一份挂载。

**Why this priority**: 这是 roster 机制的核心实证（per-session 选择 + scope 链 + standing mount 共享），是 saolei 双 agent 拓扑（`survey/deepseek-harness-team-mode.md` 头部决策 ③⑥）的前提能力；单此一条即可独立演示"一个进程同时服务多种组合的会话"。

**Independent Test**: 部署 demo 后，创建两个绑定不同 preset 的会话各发一条消息，断言两者的模型可见组合（persona/工具目录）不同且各自与 preset 声明一致；不指定 preset 创建的会话使用默认模板。

**Acceptance Scenarios**:

1. **Given** 模板集已随部署就绪（含/不含工具行的两类模板），**When** 调用方分别创建绑定 preset A（含工具行）与 preset B（不含工具行）的会话并各发消息，**Then** 两个会话的模型可见组合不同：A 的会话可见该工具及其配套守则，B 的会话两者皆无（工具与 guidance 的行级一致性）。
2. **Given** 调用方创建会话时未指定 preset，**When** 会话建立并发送消息，**Then** 该会话使用 roster 配置的默认 preset。
3. **Given** 两个会话绑定同一 preset，**When** 各自完成对话，**Then** 两者共享同一份 preset 挂载（组合注册只存在一份，会话间状态隔离）。
4. **Given** 会话已绑定某 preset，**When** 后续消息继续该会话，**Then** 组合保持创建时所绑定者不变（会话级稳定性）。

---

### User Story 2 - 经 API 创作、更新、删除 preset（C1 闭环）(Priority: P2)

开发者经 preset CRUD API 创作 preset：指定模板、提供动态字段（persona 文本、可选展示名）——系统将模板物化为可写 root 下的 preset 副本（copy-then-patch：persona 写入副本组合文件、展示名写入展示元数据），动态字段存入存储。更新 persona 后，**已存在的会话保持旧组合继续对话，新会话拿到新组合**；删除后，已加入会话不受影响，新会话创建被拒绝。

**Why this priority**: C1 是本 feature 的核心交付模式（saolei 将来 player/planner 分池的创作路径）；其"更新→代际切换"语义（旧会话稳、新会话新）是 roster generation 机制的关键实证。

**Independent Test**: 经 API 创建 preset（选模板、给 persona）→ 新会话绑定该 preset 发消息断言 persona 生效 → 更新 persona → 原会话再发消息断言行为不变、新会话断言新 persona 生效 → 删除 → 原会话仍可对话、再建会话被明确拒绝。

**Acceptance Scenarios**:

1. **Given** 存储（内存）与可写 root 就绪，**When** 调用方经 API 创建 preset（template=X, persona=P1），**Then** 存储记录该动态字段集合，且不存储模板的静态内容（无完整组合文件副本入存储）；经该 preset 创建的新会话生效 persona P1。
2. **Given** preset 已被会话 S 绑定（persona=P1），**When** 调用方经 API 将 persona 更新为 P2，**Then** 会话 S 继续对话时组合保持 P1 不变，而新创建的同名 preset 会话生效 P2。
3. **Given** preset 已被会话 S 绑定，**When** 调用方经 API 删除该 preset，**Then** 会话 S 不受影响（继续可对话），新会话绑定该 preset id 被明确拒绝。
4. **Given** 调用方创建 preset 时使用的 preset id 已被占用，或引用不存在的模板，**When** 调用 API，**Then** 请求被拒绝且不留半物化状态（无残留目录与存储记录）。

---

### User Story 3 - preset 扩展以独立插件形态与 service 对接 (Priority: P3)

其他服务的开发者（agent_v2/saolei 迁移的实际受众）可以观察并复用本 demo 确立的边界实践：preset 扩展逻辑（模板物化、动态字段存储、roster 消费）全部位于独立插件包内，service 层（RPC 处理、会话管理）仅经插件暴露的服务接口完成 API 输入注入（创建会话时传 preset id）与输出读取（查询 preset 资源），**不直接引用 roster API 或文件系统**。

**Why this priority**: 边界划分是本 feature 的架构交付物（修正 agent_v2 现状的"service 层内嵌 preset 逻辑"），是后续迁移的模板；但它是约束性/可审计性目标，以 US1/US2 的功能面为载体，故列 P3。

**Independent Test**: 代码审查审计：demo 的 service 层源码（RPC handler 与会话管理）零 roster 服务引用与零文件系统引用；preset 领域操作全部经插件服务接口完成；该审计作为验收 checklist 项。

**Acceptance Scenarios**:

1. **Given** demo 完整实现，**When** 审计 service 层源码，**Then** 无任何 roster API 直接调用与文件系统读写（对 preset 而言），所有 preset 领域操作经插件服务接口。
2. **Given** demo 完整实现，**When** 审计插件包源码，**Then** 无任何 RPC/传输层概念（proto、gateway 路由）进入插件；插件只依赖框架 ctx 与 roster 服务。

---

### Edge Cases

- **preset 组合文件损坏（broken）**：名册列表以 broken 原因呈现该行而非静默跳过；绑定该 preset 的会话创建 fail-fast（携带发现报告的原因），不产生半组合会话。
- **未创建会话直接发消息**：SendMessage 以明确错误拒绝（提示先创建会话），不隐式创建。
- **物化失败回滚**：API create/update 过程中任一步失败（存储冲突、文件写入失败等），不留半物化状态——目录与存储记录要么都落地要么都不存在。
- **重复创建会话**：同一会话资源再次 CreateConversation——同 preset 幂等（无副作用），异 preset 重建（按新 preset 重新物化；在途消息语义随重建丢弃）。
- **服务重启**：内存存储的动态 preset 资源丢失（与 demo 会话同为内存态，已知限制）；模板为部署数据不受影响；重启后再次使用前需重新创作/创建。
- **未知 preset id**：会话创建与 preset 引用均以明确错误拒绝（列出可用集合）。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: demo agent 的组合清单 MUST 挂载官方 roster 插件行（`@deepseek-ai/dsh-agent-presets`），配置**模板 root（system 信任）+ 可写 root（user 信任，且为第一个 user root）**两个扫描根、显式默认 preset，并禁用机器用户目录根（确定性）；roots 发现语义（热读取）与信任语义（可写/可删仅限第一个 user root）遵循 roster 官方定义（`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md` §4）。
- **FR-002**: 系统 MUST 提供显式会话创建入口（CreateConversation）：创建时绑定 preset id（可选，缺省为 roster 默认）；未创建会话的 SendMessage MUST 以 FAILED_PRECONDITION 明确拒绝（语义对齐 agent_v2 FR-007：无懒创建）。
- **FR-003**: 会话物化 MUST 经框架创建选项的 preset 元数据与未发布 setup 钩子完成挂载（对齐官方宿主的 composeAgent 接线模式），preset id 记入会话头；组合 MUST NOT 进程级固定于会话之外。
- **FR-004**: 系统 MUST 提供 preset 资源 CRUD 面（Create/Get/List/Update/Delete，资源语义对齐 agent_v2 PresetService 的 AIP 风格）：Create 指定模板与动态字段（persona 文本、可选展示名）；Update 仅改动态字段；存储 MUST NOT 保存模板静态内容的副本（数据库只存必要动态字段，2026-09-08 决策）。
- **FR-005**: preset 扩展逻辑 MUST 以独立 Dominion 插件包（`common/js/dsh-plugins/` 下，通用 authoring 基座定位）承载：内部包含存储接口 seam（demo 注入内存实现）、copy-then-patch 物化（经 roster 官方创作路径拷贝模板 + 定点改写副本动态字段）、roster 服务消费（inject 依赖）；该插件 MUST NOT 含传输/RPC 概念。service 层 MUST 仅经插件暴露的 ctx 服务完成对接（输入注入与输出读取），MUST NOT 直接引用 roster API 或读写 preset 文件。
- **FR-006**: 模板 preset（至少两份：含 model-facing 工具行的与不含的）MUST 作为部署数据随 demo agent 分发（置于 system 信任 root）；工具行引用的工具 MUST 由 demo 本地 workspace 插件包提供，且该插件 MUST 将工具与其配套 prompt 守则封装于同一包内（行级选择一致性：不挂该行则 schema 与守则同时缺席）。
- **FR-007**: 验证矩阵（`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md` §7：V1 选择与默认/V2 scope 链与 mount 共享与工具-guidance 一致性/V3 热创作与 generation 与 broken 呈现/V4 C1 闭环与边界审计）MUST 全部有测试承载（单测与/或大型测试），MUST NOT 存在无断言承载的验证点。
- **FR-008**: 大型测试 MUST 扩展既有 demo 测试计划（`experimental/dsh/demo/testplan/interface_test.yaml`）：部署含 roster 组合的 demo 全链路，经公共 HTTP 入口覆盖 US1/US2 全部验收场景；验收以 testplan skill 实际执行完整闭环（部署→用例→清理）且**全部用例通过**为准（`.specify/memory/constitution.md` 原则 VI；禁止以构建检查替代执行）。
- **FR-009**: 新增 dsh 依赖（roster、persona 行插件等）MUST 与现有 dsh 版本线（0.1.1-rc.2）同线精确 pin（dist-tag 不可信，`experimental/dsh/demo/README.md` 已知限制；persona 包以 registry versions 实际核对同线版本号），版本变更以 lockfile 方式整体进行。
- **FR-010**: 验证完成后 MUST 依据实践过程将 roster 验证结果写入 `survey/`（含：roster 机制实证结论与调研差异、C1 扩展实践结论、service↔插件边界最佳实践），`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md` 中的纸面结论以实践结果为准修订。

### Key Entities

- **Preset Template（模板 preset）**: 部署数据（system 信任 root 下的 preset 目录），静态组合内容的唯一载体（工具行、persona 行占位等）；不可经 API 删除。
- **Authored Preset（创作 preset）**: 动态字段集合（preset id、模板引用、persona 文本、可选展示名、时间戳）+ 可写 root 下物化副本的配对；生命周期由 API CRUD 驱动。
- **Conversation（会话）**: 显式创建、绑定 preset id 的对话单元；内存态；组合在创建时确定、存活期稳定。
- **Preset Authoring 服务**: 插件暴露的 ctx 服务——preset 领域操作的唯一对接面（创作/物化/查询/删除）；service 层的消费对象。
- **Roster（名册）**: 官方 roster 服务——root 扫描（热读取）、preset 发现与健康（broken）、创作拷贝、挂载与代际（文件 stamp）的唯一机制所有者。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 大型测试经 testplan skill 完整闭环执行（部署→全部用例→清理），**全部用例通过**（零 failed、零 flaky），其中 preset 场景用例覆盖 US1 全部 4 个与 US2 全部 4 个验收场景。
- **SC-002**: 组合差异可断言：不同 preset 会话的模型可见组合不同、默认 preset 生效、同 preset 会话共享挂载（共享以单测断言组合注册份数，不以内存字节数为准）。
- **SC-003**: C1 闭环可断言：API 创作的 preset 即刻可用于新会话（热发现）；persona 更新后旧会话组合不变、新会话生效新值（generation 切换）；删除后旧会话存活、新会话被拒。
- **SC-004**: 边界可审计：service 层源码零 roster API 引用、零 preset 文件系统操作（审计作为 checklist 项通过）。
- **SC-005**: 验证落档：`survey/` 增补 roster 验证结果文档（基于实践过程，非纸面推断），与本 spec `research.md` 中被实践修订的结论形成对照记录。

## Assumptions

- **dsh 版本线**：0.1.1-rc.2 线同线精确 pin；新增依赖（roster、persona）以 registry versions 核对同线版本；0.x-rc 破坏性变更风险由实验性 demo 接受（`experimental/dsh/demo/README.md` 已知限制延续）。
- **demo 存储为内存实现**：preset 动态字段与 demo 会话同为进程内存态、随重启丢失（demo 已知限制风格，README 声明）；模板为部署数据不丢。Mongo 生产化延后至 agent_v2 迁移时经同一 Store seam 扩展，不在本 feature 范围。
- **中间数据与 survey 写作**：`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md` 承载 2026-09-08 全部讨论结论（agent_v2 现状分析、选项对比、社区参考、机制事实、架构设计、验证矩阵），作为 plan 阶段输入与验证后 survey 写作底稿；survey 写入（FR-010）在验证完成后按实践进行，是本 feature 交付的一部分。
- **实验性质量线**：demo 无 auth/secrets/生产化运维；目标是机制验证与边界实践，不交付生产服务。
- **fake-llm 能力扩展最小化**：如需端到端断言模型可见组合（persona/工具目录回显），优先以 agent 侧单测承载；fake-llm 仅在单测不足以覆盖端到端时增加最小 echo 模板（plan 阶段定）。
- **047 既有行为保持**：本 feature 为增量扩展，不改坏 047 已验收的聊天往返/多轮/并发语义（既有大型测试用例全部保持通过）。
- **`preset.yml` 展示元数据能力受限**：id 与信任级由目录/root 决定不可写；展示名是可选动态字段，缺失时以 preset id 呈现（roster 官方降级行为）。
