# Feature Specification: agent_v2 优化——wiki 注入收敛、team 成员流程标准化与 announcer 成员化

**Feature Branch**: `076-agent-v2-optimize`

**Created**: 2026-10-13

**Status**: Draft

**Input**: User description: "对 @projects/game/agent_v2/ 进行优化：1. 从 @dominion/dsh-saolei-memory 中移除 wiki 功能注入，只使用 memory。实践发现 wiki 效果并不好，agent 只写入但不读取。只移除 wiki 工具和配套 prompt 的注入，wiki 能力、基建和 memory 服务不改动。另外 @common/js/memory/ 也分为两层，一层是功能单元层，包括 memory 和 wiki，确保每一个功能单元都可以独立使用；第二层是组合层，用于多个功能单元组合使用。2. 将 team member 流程与成员切换标准化，前置调研 @survey/saolei-member-switch-flow.md。3. 将负责播报的 saolei member 改名，并检查是否按照 member 契约实现，不做特殊处理。如果没有不符的地方则跳过。"

## 背景与调研基线

本 feature 包含三项相互独立可交付、但共享 agent_v2/team 代码面的优化工作：

1. **wiki 注入收敛**：现状 `@dominion/dsh-saolei-memory`（`common/js/dsh-plugins/saolei-memory`）构造 memory+wiki 两个 gRPC client，经 `@dominion/common-js-memory` 的 `buildMemoryCapabilities`（`common/js/memory/src/capabilities.ts`）向 planner 成员整单元挂载 memory 工具 + 三个 wiki 工具与 `memory:guidance`/`wiki:guidance`/`memory:snapshot` 三个 prompt section。实践结论：wiki 只写不读，注入无效。
2. **成员流程与切换标准化**：切换决策集中于 `TeamOrchestrator.nextStep()`（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`）的四个分支，判定/状态赋值/副作用/驱动条件混杂。调研文档 `survey/saolei-member-switch-flow.md` 已完成全部设计裁定（裁定 1–24，技术栈无关），本 spec 以其为设计基线；其 §12 待讨论项中，"announcer 成员化落地"（§12.2）为本 spec 承接项，其余（技术栈/落地映射/宿主联动细节）留 plan 阶段。
3. **announcer 更名与成员契约对齐**：播报成员 role 名 `saolei` 存在场景歧义；且 proto/宿主面对其刻意排除（`validateSaoleiMembers` 两成员校验、`toTeamView` 硬编码双成员名册、`TeamRole` 类型不含），与"member 一视同仁"（调研裁定 6/23）不符。

关系处置：`specs/061-team-queue-steer/`（已规划未实践）的在途投递语义（steer → 成员 inbox）经本 feature 的成员域投递落地，其静止路径方案（编排 FIFO）被调研裁定 17 取代——061 spec 需标注 supersession（FR-114）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - team 成员流程与切换标准化（flow 流程控制）(Priority: P1)

作为 saolei team loop 的维护者，我要成员切换成为**基本操作**：引入新切换场景时只需声明"何时切换到哪"（活动与边的定义），不再重复描述排队消化、静止条件、失败保持、exactly-once 守卫等执行细节。同时，team 的最终用户获得两项可感知改进：在途回合中发送的消息在下一个 step 边界进入成员上下文（不再等待整个回合结束）；成员失败时按错误类别获得"该做什么"的呈现指导。

**Why this priority**: 这是本 feature 的架构核心目标——现状 `nextStep()` 的四分支混杂决策与执行，每次场景扩展都要重复描述大量细节（调研 §2.6 结构性观察）；标准化兑现后新场景 = 一段活动/边定义（+新成员类型时一份接口实现），运行时零改动。它同时承载用户可感知的行为改进（mid-turn 投递、错误呈现），是三项工作中价值与工作量最大的。

**Independent Test**: 可独立于 US2/US3 开发与验证——以 `common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts` 全量用例为行为保持验收基准（两点基准调整见 FR-113），辅以 flow 结构/冻结/投递/错误契约的新增单测与 agent_v2 大型测试回归。

**Acceptance Scenarios**:

1. **Given** 物化后的 team 处于初始 planner 激活，**When** 用户发送首条消息，**Then** planner 消化该消息开 turn，settle 后流程经边判定进入 player 活动（对应现状 S1/S4）。
2. **Given** player 回合在途（多步工具执行中），**When** 用户发送新消息，**Then** 该消息经成员投递在下一个 step 边界随工具结果进入成员上下文（061 方向），而非等待整个 turn 收束（多消息时序语义变化）。
3. **Given** player settle 且存在未复盘终局记录，**When** 泵唤醒流程决策，**Then** 依次进入播报活动与复盘活动（两跳），播报 exactly-once、复盘输入集以播报消息收尾（对应现状 S6，行为保持）。
4. **Given** 复盘活动执行失败（穷尽成员自愈后上报 failed），**When** 用户发送新消息，**Then** 先由冻结成员消化该消息（恢复驱动），后由流程重启复盘活动重放持有输入集（恢复链两次驱动；对应现状 S3/S8，行为保持）。
5. **Given** 任一活动执行中用户取消，**When** 在途回合被打断，**Then** 活动以 aborted 冻结（不写失败事实、流程事实不消费），未消费输入落地为消息历史，再次投递即恢复；已冻结时再次取消为 no-op（对应现状 S9，竞态修正见 FR-108）。
6. **Given** 复盘 settle 且已完成轮数达到自然停阈值，**When** 边判定求值，**Then** 进入 rest 静止活动（激活权归 player、无驱动、计数清零、player 未消费广播保留），后续任何驱动 settle 后按新终局记录研判出边（对应现状 S5/S7，行为保持）。
7. **Given** 一个新切换场景需求（如调研 §7.3 扩展位：新人机交互成员的活动），**When** 维护者落地它，**Then** 只新增活动/边定义（+新成员类型的接口实现），flow 运行时与执行层零改动（标准化的兑现，验证性演示见 SC-007）。

---

### User Story 2 - wiki 注入移除与 memory 库分层 (Priority: P2)

作为能力库与场景组装的维护者，我要 saolei 场景只注入 memory 能力（wiki 工具与配套 prompt 不再进入 planner），且 `@dominion/common-js-memory` 分为**功能单元层**（memory 单元、wiki 单元，各自可独立使用）与**组合层**（多单元组合使用），并消除库内自持的 unary 调用策略副本（策略唯一归属 grpc-runtime）——移除无效注入的同时，wiki 能力、基建与 memory 服务保持完好，组合复用的能力不失。

**Why this priority**: 独立、小颗粒、即时价值：planner 的 system prompt 与工具面去除无效 wiki 注入（只写不读的干扰），库结构按单元/组合分层支撑后续任意组合消费。优先级低于 US1（非架构核心），高于 US3（库层结构是后续能力演进的地基）。

**Independent Test**: 可独立于 US1/US3 开发与验证——saolei-memory 适配器测试（不再构造 wiki client、不再注入 wiki 工具/section）、memory 库单元层/组合层测试（各自独立挂载、组合输出等价）、unary 策略副本消除与注入 seam（FR-006）、wiki 基建回归（gateway wiki 大型测试与 web WikiView 零改动全绿）。

**Acceptance Scenarios**:

1. **Given** saolei team 物化 planner 成员，**When** 成员 setup 完成，**Then** 其工具清单只含 memory 工具（无 wiki_search/wiki_get/wiki_write），system prompt 的能力 section 只含 `memory:snapshot`（无任何 guidance section）。
2. **Given** memory 公共库，**When** 消费方只构造/挂载 memory 单元（或只构造/挂载 wiki 单元），**Then** 该单元的 faces/tools/sections/ready 全部可用，工具执行不因另一单元缺席而失败。
3. **Given** 组合层将 memory 单元与 wiki 单元组合，**When** 整体挂载到成员，**Then** 组装产物（工具集、section 名与 order、能力语义）与现状组合输出等价（组合面兼容锚点）。
4. **Given** wiki 服务、gateway wiki 管理路由与 web WikiView（基建面），**When** 本 feature 交付后运行 wiki 大型测试与 web 回归，**Then** 全部通过（wiki 能力与基建零改动）。

---

### User Story 3 - 播报成员更名 announcer 与成员契约对齐 (Priority: P3)

作为 web 最终用户与 team 框架维护者，负责终局播报的成员以准确名称 **announcer** 出现（消除与"扫雷"场景名的歧义），并作为正式成员被框架**统一且完整**地呈现：进入成员名册、可作为激活成员呈现、以与 LLM 成员一致的方式被访问——不存在"非 LLM 成员排除"的特殊处理。

**Why this priority**: 命名准确性与成员模型一致性（调研裁定 23：统一逻辑成本更低，呈现特例被推翻）；有对外 proto 契约变更，需与 US1 的顺序协调（名册放宽先行或同批，FR-206），故列为 P3 但**不可跳过顺序约束**。前置检查结论：现状存在不符项（proto/宿主排除特判），不满足"跳过"条件。

**Independent Test**: 可独立开发（但与 US1 的落地顺序受 FR-206 约束）——proto/宿主单测（三成员名册、announcer 成员读取、active_member 可为 announcer）、web 前端测试（名册/激活渲染 announcer）、大型测试中名册与播报断言更新。

**Acceptance Scenarios**:

1. **Given** 已物化的 team，**When** 调用 GetTeam，**Then** 名册返回三个成员（player/planner/announcer），announcer 项无 preset/model 值（非 LLM 成员的名册形态）。
2. **Given** 播报活动执行（激活权短暂归属 announcer），**When** 宿主呈现激活成员，**Then** active_member 可为 announcer，无呈现投影特例（呈现可达性受同步执行窗口影响，属成员自身特性，不作框架保证）。
3. **Given** web 前端，**When** 渲染团队名册与激活指示，**Then** announcer 以无 preset/model 形态展示，播报消息的成员标签为 announcer。
4. **Given** 成员读取面，**When** 以成员资源名访问 announcer，**Then** 与 LLM 成员一致的访问方式可用（其 output-only system_prompt 为空值）。
5. **Given** 调用方物化/刷新 team，**When** 提交 UpdateTeam，**Then** 请求侧仍只配置两个 LLM 成员（player/planner，preset 必填校验不变），announcer 由服务端进入名册、不可由调用方配置。

---

### Edge Cases

- **冻结与投递交互**：冻结期间 flow 不发射指令，唯一驱动源是用户投递（deliver 即恢复）；冻结被唤醒后按 last-outcome 分流（settled/resting → 边判定；failed/aborted → 重启当前活动），无泵空转。
- **cancel 打断复盘在途**（现状已知竞态）：在途 drive 以 aborted 返回、流程冻结、终局记录不被消费——现状"idle 返回后记录被错误消费"的行为按此修正（FR-113 基准调整之二，显式验收）。
- **dispose 在 drive 在途时**：在途回合 aborted 不转交；成员释放、退订、注册撤销 allSettled 并行执行，部分失败不阻塞其余；dispose 后 submit 同步报错保持。
- **rest 期间新终局记录到达**：rest 出边在"驱动 settle 后"研判（任何驱动完成即重新研判，判定依据是事实不是投递动作本身）。
- **跳局不补发**：guard 对最新终局记录求值（对象恒等），被新局覆盖的旧记录播报/复盘条件永不再满足——`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §3 item 4 语义保持。
- **静止时多条消息连发**：首条装配开 turn，后续经在途投递于 step 边界/turn 自愈进入（时序语义按 061 方向，非编排 FIFO 逐条消化）。
- **单元/组合 guidance 边界**：memory 单元不携带 guidance section（单工具说明由工具 description 承载）；wiki 单元保留跨工具工作流 guidance；组合挂载时分工文本（memory vs wiki）在场——任何挂载形态的 prompt 都不引用不在场工具。
- **announce 激活窗口**：announce-only 成员同步执行、激活窗口微任务级，呈现是否可观察是成员自身特性与框架无关（可接受，不作为长期排除理由）。
- **配置类错误（CONFIG）**：恢复要求重新物化且流程位置不保留（refresh 语义），与冻结恢复链区分。

## Requirements *(mandatory)*

### Functional Requirements

**A. wiki 注入移除与 memory 库分层（US2）**

- **FR-001**: `@dominion/dsh-saolei-memory` 适配器 MUST 只注入 memory 功能：不再构造 WikiService client、不再向成员注册任何 wiki 工具或 wiki prompt section；物化后的 planner 能力面零 wiki 痕迹。
- **FR-002**: `@dominion/common-js-memory` MUST 分为两层——**功能单元层**：memory 单元与 wiki 单元，每个单元可独立构造、独立挂载（faces/tools/sections/ready 原子四件套形态保持）、独立测试，单元间互不依赖对方在场（含工具执行的自洽解析）；**组合层**：将多个功能单元组合为单一可整体挂载的能力面。
- **FR-003**: memory 单元 MUST 不携带 guidance section——单工具的使用说明已由工具自身 description 完整承载（含快照近因与全量写语义）；跨存储分工 guidance（memory vs wiki 边界描述）归属组合层，仅在组合挂载时注入。
- **FR-004**: 领域语义与冻结契约 MUST 保持：memory 操作语义、wiki 语义（校验/分页/单条上限/两阶段 upsert）不变；组合层（memory+wiki）的组装产物与现状 `buildMemoryCapabilities` 输出等价（工具集、section 名与 order 逐项一致）。
- **FR-005**: wiki 能力、基建与 memory 服务 MUST 零改动且功能完好：game proto 的 WikiService、memory 服务 wiki 实现、gateway wiki 管理路由、web WikiView 与既有 wiki 大型测试全部不变并通过。
- **FR-006**: unary 调用策略 MUST 去重并唯一归属 grpc-runtime：`@dominion/common-js-memory` 不再自持调用策略副本（`common/js/memory/src/store.ts` 的 `UnaryInvoke`/`callUnary`/`probeChannel` 删除），策略以结构类型函数（与 `ctx.grpc.callUnary` 结构兼容）随 client 一起经组装 seam 注入；`specs/074-dsh-grpc-runtime/contracts/memory-library.md` §3 相应修订，grpc-runtime 插件代码零改动（策略实现唯一归属 `common/js/dsh-plugins/grpc-runtime/src/unary.ts`）。

**B. team 成员流程与切换标准化（US1；设计语义依据 survey 裁定 1–24，技术栈无关）**

- **FR-101**: 成员切换决策 MUST 升级为 flow 流程控制：F-Moore 活动节点图为唯一创作结构（节点 = 活动 + 动作，边 = 纯条件 guard；成员流转图为派生视图，不单独创作）；流程位置（当前活动）取代 `phase`+`current` 冗余状态对；流程事实（reviewed/statsSentFor/rounds）由活动钩子写入、边判定只读。
- **FR-102**: flow 层与执行层 MUST 经指令解耦：唯一指令词汇 `start(activity)`（与图节点一一对应）；成员切换 = 进入不同成员的活动，无独立"切换"指令；执行层不读 flow 内部状态、只消费指令与回报终态；flow 层不触碰执行细节（成员驱动、消费锚点、执行完整性）。
- **FR-103**: 成员 MUST 以统一接口被编排：`drive(input?)`（闭环调用——发起→执行→完成判定→返回终态；不 throw 契约：内部失败转译 failed 返回）与 `deliver(message)`（在途 → steer 入成员 inbox 随执行区间消费；静止 → 装配驱动开 turn）；loop 层不感知成员实现类型，LLM agent 成员与 announce-only 成员同权。
- **FR-104**: LLM agent 成员的活动完成判断 MUST 收敛于单一成员适配器实现：agent 信号订阅与 idle 等待机械从编排器下沉至适配器内部；完成 = 本次驱动引发的整个执行区间收束（agent 静止：turn 已结束且无待消化输入）；引擎错误码 → loop 错误码映射、投递分流（steer/装配）与失败后的持有输入重放均在适配器内；编排器与 flow 定义零接触 agent loop 信号。
- **FR-105**: 用户消息投递 MUST 完全成员域化：编排 FIFO 与 queue gate 移除；用户消息经 `deliver` 即时处理（在途 steer 于下一 step 边界进入、turn 收束自愈；静止装配 drain+message 开 turn），完成定义自包含（inbox 空 + 无在途回合）；团队广播（relay）保持驱动装配时 drain 拉动装配（exactly-once 锚点边界不变）；编排层永不合成驱动文本（`specs/059-agent-v2-team-mode/spec.md` FR-009/FR-010 保持）。
- **FR-106**: 活动终态 MUST 为四态 settled/resting/failed/aborted，由 `drive()` 闭环返回；failed/aborted 产生**冻结**（不消费流程事实、不边判定、位置保持），冻结语义由 last-outcome 单字段承载；冻结期间投递即恢复（恢复链两次驱动：投递恢复驱动 → 流程重启当前活动重放持有输入集；"投递先于重试"由执行顺序结构保持）；执行层不再有 paused 概念。
- **FR-107**: 成员上报失败 MUST 遵循错误契约 `failed { code, detail }`：code ∈ {TRANSIENT, QUOTA, CREDENTIAL, CONFIG, INPUT_TOO_LARGE, INTERNAL}，每码绑定恢复三维度（同输入重试可行性/恢复主体/呈现指导）；成员穷尽自愈（引擎 L0 重试瀑布对上层不可见）后最小上报；宿主按码产出用户动作文案——文案取 `survey/saolei-member-switch-flow.md` §8.1 码表的"呈现指导"列，loop 层不产出文案，六码各有非空文案（可作单测判据）；六码的现状执行行为统一为冻结，契约即时价值在呈现指导与诚实分类对齐。
- **FR-108**: cancel MUST 达成目标语义：终止在途回合（已生成内容保留历史）、未消费输入落地为消息历史（不静默丢弃）、流程位置冻结（事实不消费）、等待投递恢复；幂等；已冻结时为 no-op；不产生失败呈现（用户意图非故障）；被打断的在途活动以 aborted 返回且不写失败事实——现状 cancel 打断复盘在途时记录被错误消费的竞态据此修正。
- **FR-109**: dispose/refresh MUST 彻底释放进入终态：在途回合 aborted 不转交、释放成员句柄/退订信号/撤销团队注册（部分失败不阻塞，首个失败上报）、流程状态全清（flow 无生命周期：纯状态对象随引用消失，refresh 不保留流程事实）；幂等；物化原子性（全就绪或全回滚）保持。
- **FR-110**: 终局统计播报 MUST 作为独立活动进入 flow（播报成员的切换纳入流程决策面，非前置副作用）；全部 LLM 活动的输入构造统一为 drain；播报与复盘的 exactly-once 守卫（对象恒等判据）与跳局不补发语义保持（`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §3）。
- **FR-111**: 五轮自然停 MUST 为 rest 独立活动节点（静止是活动语义：激活权归属、无执行）；轮计数由复盘活动的 onSettle 推进、入 rest 时清零、player 未消费广播保留不消费；rest 出边 guard 为"驱动 settle 后"按终局事实研判。
- **FR-112**: 宿主联动 MUST 适配新结构并保持对外语义：快照 phase 由活动派生、静止判定扩展（成员 inbox pending + 冻结短路取代 paused）、active_member 呈现对接激活权落地（在途回合成员优先，静止取激活权）、流收束按失败 origin 二分保持（member-turn 干净收束 / orchestration INTERNAL，`specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md` §2）。
- **FR-113**: 重构 MUST 以行为保持为验收基准：`orchestrator.test.ts` 全量用例在目标结构下通过，仅两点基准调整——多消息时序语义按 061 spec 的 SC 方向调整（`specs/061-team-queue-steer/spec.md`）；cancel 打断复盘在途的竞态按 aborted 冻结语义修正（非逐字保持）；现状 10 个切换/保持场景（`survey/saolei-member-switch-flow.md` §2.3 S1–S10）在目标结构的映射（survey §9）全部保持。
- **FR-114**: `specs/061-team-queue-steer/` MUST 做关系处置：其静止路径方案（编排 FIFO/queue gate，`specs/061-team-queue-steer/research.md` R2）被本 feature 取代，在途投递语义（steer → inbox，`specs/061-team-queue-steer/research.md` R1/R3）经成员域 deliver 落地——061 spec 增补 supersession 标注与去向说明，其未实施任务不再单独执行。

**C. announcer 更名与成员契约对齐（US3；survey 裁定 23 与 §12.2 承接）**

- **FR-201**: 播报成员的 role 名 MUST 从 `saolei` 全量更名为 `announcer`：role 常量值、成员 id 前缀（`${session}/saolei` → `${session}/announcer`）、名册 summary、默认成员摘要、导出名、测试断言、契约与 README 文档中的呈现（会话状态为进程内存态，role 更名无持久化迁移；团队消息序列中播报消息的成员标签呈现见 FR-204）。
- **FR-202**: 框架对激活成员的呈现 MUST 统一且完整（无呈现投影特例）：announcer 进入 GetTeam 成员名册（三成员）、active_member 可为 announcer、成员读取面以与 LLM 成员一致的方式服务 announcer（名册项无 preset/model、output-only system_prompt 为空值——非 LLM 成员的名册形态差异本身允许）。
- **FR-203**: 宿主 MUST 移除播报成员排除特判（FR-202 呈现面的实现侧）：名册构造与成员查找面放宽——团队名册项不再硬编码双 LLM 成员、`GetTeamMember` 的成员查找不再仅接受 player/planner、成员角色类型（宿主 `MemberRole`、编排 `TeamRole`）纳入 announcer。请求侧校验保持严格：`validateSaoleiMembers` 仍要求 UpdateTeam 恰含 player/planner 两 LLM 成员配置、preset 必填——announcer 由服务端进入名册、不可由调用方配置。裁定口径：`survey/saolei-member-switch-flow.md` §12.2 将 `validateSaoleiMembers` 表述为"两成员校验放宽"，本 feature 将该放宽点收窄（仅名册构造与成员查找面，请求侧校验不变）。
- **FR-204**: web 前端 MUST 渲染 announcer：团队名册与激活成员指示含 announcer（无 preset/model 展示形态），团队消息序列中播报消息的成员标签为 announcer。
- **FR-205**: 受影响契约文档 MUST 修订：065 契约（`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §3 item 4/§4）中"proto 面零改动/GetTeam 不含 saolei"条款按统一呈现修订；059/060 team 契约中 active_member/名册语义做增量勘误；其余引用 saolei role 名的契约/spec 同步更名。
- **FR-206**: announcer 名册放宽与 flow 重构 MUST 满足顺序约束：名册放宽先行或与 flow 重构同批落地（重构后激活权真实短暂归属 announcer，名册未放宽期间不得作为长期状态）。

### Key Entities *(include if feature involves data)*

- **功能单元（capability unit）**: memory 单元 / wiki 单元——faces/tools/sections/ready 原子四件套；单元间互不依赖。
- **组合层（composition layer）**: 将多个功能单元组合为单一整体能力面的库层（含跨单元分工 guidance）。
- **unary 策略（UnaryPolicy）**: 结构类型的调用策略函数（waitForReady + 10s deadline + 通道探测 + 失败日志）——实现唯一归属 grpc-runtime（`common/js/dsh-plugins/grpc-runtime/src/unary.ts`），随 client 经组装 seam 注入能力库。
- **活动（Activity）**: F-Moore 图节点——id、member 绑定、可选 onSettle（流程事实写入）、next(facts)（纯谓词出边判定）。
- **事实（Facts）**: 边判定的只读输入——terminal（最新终局记录）/ reviewed（已复盘记录）/ rounds（已完成轮计数）。
- **指令（Command）**: `start(activity)`——flow 层唯一输出，激活权归属与活动执行的语义载体。
- **成员接口（Member）**: `drive(input?)` / `deliver(message)`——team loop 层的执行单元抽象。
- **成员适配器**: 成员类型的单一实现单元——LLM agent 成员适配器（完成判断、错误码映射、投递分流、持有输入重放）与 announce-only 成员（同步执行）。
- **活动终态（outcome）**: settled / resting / failed / aborted——`drive()` 闭环返回；failed 携带 `{code, detail}`。
- **冻结（frozen）与 last-outcome**: 流程位置保持与唤醒后动作分流的载体；失败事实（lastError）归 flow 层。
- **错误契约码**: TRANSIENT / QUOTA / CREDENTIAL / CONFIG / INPUT_TOO_LARGE / INTERNAL——每码绑定恢复三维度。
- **landed**: 取消后落地为消息历史的未消费输入（不静默丢弃）。
- **announcer 成员**: 原 role `saolei`；announce-only（consumes=false）、无 preset/model 的名册形态。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 现状 10 个切换/保持场景（survey §2.3 S1–S10）在目标结构下映射全部保持；`orchestrator.test.ts` 全量用例通过（基准调整仅限 FR-113 两点）。
- **SC-002**: 物化后 planner 的完整 system prompt 与工具清单零 wiki 痕迹（无 wiki 工具、无 wiki 相关 section 与文本引用）。
- **SC-003**: memory 单元与 wiki 单元各自独立挂载/独立测试通过；组合层组合两者的组装产物与现状组合输出等价（工具集、section 名/order 逐项一致）。
- **SC-004**: wiki 基建全量回归通过：gateway wiki 大型测试（`projects/game/testplan/`，经 testplan skill 实际执行）与 web WikiView 相关测试全绿，且基建代码零改动。
- **SC-005**: 用户在成员回合在途发送的消息于下一个 step 边界进入成员上下文（多消息端到端场景验证，不再等待整个回合收束）。验收判据：成员回合在途连续发送 2 条用户消息，两条均随下一 step 的工具结果进入该 step 的模型输入（独立 user 消息块），且成员视角在消费时点实时呈现（无需等待整个回合收束）。
- **SC-006**: GetTeam 名册返回三成员（player/planner/announcer）；播报活动窗口内 active_member 呈现 announcer 可达；web 名册与激活指示渲染 announcer。
- **SC-007**: 扩展模型兑现验证：以测试形态新增一个示例切换场景——取 `survey/saolei-member-switch-flow.md` §7.3 的「工具/服务成员」扩展位（成员 `drive` = 调用外部服务），flow 运行时与执行层零改动即被编排。
- **SC-008**: agent_v2 既有大型测试（`projects/game/testplan/system_test.yaml` 全部套件：game-system（含 conversation/game/wiki/web 各 module）、game-stall、game-memory-down、game-disconnect，均经 testplan skill 实际执行部署→测试→清理闭环）全部用例通过。
- **SC-009**: 库内无 unary 调用策略副本（`common/js/memory/src/store.ts` 的 `UnaryInvoke`/`callUnary`/`probeChannel` 零残留）；策略经组装 seam 注入且与 `ctx.grpc.callUnary` 结构兼容——单测以替身注入验证委托与策略行为，`bazel test //common/js/memory/...` 通过（策略行为由 `common/js/dsh-plugins/grpc-runtime/src/unary.test.ts` 覆盖）。

## Assumptions

- **调研基线权威性**：`survey/saolei-member-switch-flow.md` 的设计裁定（1–24）为本 feature US1 的设计语义基线；其 §12 待讨论项中"announcer 成员化落地"由本 spec 承接为需求（FR-201–FR-206），"flow 技术栈"（自研 minimal runtime vs 状态机库）为 plan 首个决策点，"落地映射/宿主联动面适配"细节归 plan。
- **wiki 注入移除的边界**：仅移除注入面（wiki 工具 + 配套 prompt 的成员注入与 saolei-memory 适配器内的 wiki client 构造）；wiki 能力（库内 wiki 工具/操作语义）、基建（proto WikiService、memory 服务实现、gateway 路由、web WikiView）与 memory 服务零改动。wiki 单元在功能单元层保留完整能力（独立可用 + 可组合）。
- **unary 策略注入边界**（用户裁定 2026-10-14，research.md D10）：仅删除 memory 库内自持的 unary 调用策略副本并改经组装 seam 注入；grpc-runtime 插件代码与 `ctx.grpc` 服务面语义零改动（策略实现唯一归属 grpc-runtime）。
- **guidance 归属**（用户裁定 2026-10-14）：memory 单元不携带 guidance section——单工具的使用说明由工具自身 description 完整承载（`MEMORY_TOOL_DESCRIPTION` 已含快照近因 top-10 与全量写语义，`common/js/memory/src/tool.ts:40-49`；跨局持久意图由 planner persona 文本承载）；现状"memory vs wiki 分工"文本归组合层，仅组合挂载时注入。wiki 单元保留 `wiki:guidance`（三工具协作工作流，单工具 description 无法承载）。
- **组合层消费方**：当前无 memory+wiki 组合的现网消费方（saolei 场景移除后），组合层以等价性测试（FR-004/SC-003）为验收形态保留组合能力。
- **061 关系**：061 的静止半边（编排 FIFO）被取代、在途半边经本 feature 成员域投递落地；061 spec 标注 supersession 后其目录作为语义沿革记录保留。
- **announcer 更名无迁移**：team/会话状态为进程内存态（stateful 服务 + owner 亲和），role 更名不涉及持久化数据迁移；大型测试断言中的 `saolei` 成员标签随更名更新。
- **呈现可达性**：announcer 激活窗口为微任务级，active_member 呈现可达性不作框架保证（成员自身特性，用户已裁定可接受）。
- **验收方式**：按宪法 VI，服务型变更的大型测试验收经 testplan skill 实际执行（部署→测试→清理闭环），全部用例通过为验收标准；编译+单测随每次代码变更执行（宪法 IV）。
