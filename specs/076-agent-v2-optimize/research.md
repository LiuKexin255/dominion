# Research: agent_v2 优化——wiki 注入收敛、team 成员流程标准化与 announcer 成员化

**Feature**: [spec.md](spec.md) | **调研基线**: `survey/saolei-member-switch-flow.md`（设计裁定 1–24，本文件不重复其论证，仅记录 plan 级决策与补充验证）

**用户裁定（2026-10-14）**：D1 flow 技术栈 = 自研 minimal runtime；D2 落地批次 = 三批递进 US2→US3→US1。

## D1: flow 实现技术栈——自研 minimal runtime

**Decision**: flow 层（活动定义 + `next(facts)` 纯谓词 + last-outcome 分流）与执行层（指令消费 + 成员驱动）均为手写 TypeScript，零新依赖。

**Rationale**:

1. **语义已完全规定且含定制概念**：`start(activity)` 单一指令、resting 第四终态、failed/aborted 统一冻结 + deliver 恢复链——这些是 survey 裁定 12/20/21 的定制语义，通用状态机需要绕行建模（见 Alternatives）。
2. **图极小**：5 活动 / 8 边 / 单流程，自研运行时约数百行，无复用压力（survey §5.7：Conductor 的 definition/placement 分离在当前规模不采用）。
3. **仓库风格契合**：dsh 插件族（llm-glm、grpc-runtime、team、saolei-*）全部自研 bespoke——引入通用框架与既有取向相反（宪法原则 II：过度设计 MUST 简化）。
4. **底座已存在**：dsh-agent 0.2.0-rc.2 原生提供成员适配器所需的全部机械（D3），自研层只剩薄决策/分流逻辑。

**Alternatives considered**:

- **XState v5**（`setup`/`createMachine` 强类型、活动 = state + `fromPromise` invoke（`onDone`/`onError`）、边 = guard；官方文档 https://stately.ai/docs）：语义成熟、可视化、actor 模型。否决理由——(a) invoke 出口只有 done/error 两态，resting（无可执行输入的静止）与 aborted（取消打断、非故障）需为每活动增设影子状态绕行；(b) 冻结-恢复链（冻结期间唯一驱动源是 deliver、唤醒后按 last-outcome 分流）与事件驱动转移模型错位；(c) 新依赖 + 新范式引入单一 cordis 插件，团队接触面陡增；(d) 5 活动的规模下框架收益无法摊薄。留作演进位：若未来流程数量/复杂度显著增长（survey §5.7 Conductor 先例），可再评估。
- **轻量库**（robot3 等）：生态与文档弱于 XState，无差异化收益，不评估。

## D2: 落地批次——三批递进 US2→US3→US1

**Decision**: 批1 = US2（wiki 注入移除 + memory 库分层 + unary 策略去冗〔D10〕）；批2 = US3（announcer 更名 + 名册放宽）；批3 = US1（flow 标准化重构）。

**Rationale**:

1. **FR-206 顺序约束**：名册放宽必须先行或同批于 flow 重构——批2 先行解除批3 的呈现耦合，flow 重构落地时 active_member 真实短暂归属 announcer 已被名册面承接。
2. **风险递增排序**：批1 最小且完全独立（无契约变更）；批2 中等（对外呈现变更但无消息形状变更，见 D6）；批3 最大（行为保持重构）。每批独立过完整验收门禁（编译+单测+大型测试），review 粒度友好，中断可恢复。
3. **行为保持基准最干净**：批3 开工时批1/批2 已稳定，`orchestrator.test.ts` 基准只受 FR-113 两点显式调整影响，无其他批次噪声。

**Alternatives considered**: US1 先行（否决——首批即最大风险项，且名册未放宽期间依赖"同步窗口不可观察"过渡态）；全部同批（否决——review 与回归范围过大，中途不可交付）。

## D3: 成员适配器机械底座——dsh-agent 0.2.0-rc.2 原生 API（已验证）

**Decision**: LLM 成员适配器直接使用 dsh-agent 既有 API，不自建等待/投递机械。

**验证事实**（`common/js/dsh-plugins/saolei-loop/node_modules/@deepseek-ai/dsh-agent/lib/types/runtime-types.d.ts`，dsh-agent 0.2.0-rc.2）：

| API | 语义 | 适配器用途 |
|---|---|---|
| `agent.steer(message)` | "Submit steering for the nearest step. An **idle driver starts a turn**; a running driver consumes it at its **next step boundary**" | `deliver` 的在途分支（`specs/061-team-queue-steer/research.md` R1/R3 语义即此原生行为） |
| `agent.whenIdle(): Promise<void>` | "Resolve after the current whole-agent activity reaches quiescence... follows replacement work" | `drive()` 闭环的完成等待（取代编排器手写 idleWaiters，`orchestrator.ts:1249-1262` 下沉后废弃） |
| `agent.inbox` | `nextTurn`/`nextStep` 只读列表 + `clear/append/prepend/replace/remove` | 完成定义自包含（pending 判定）与 landed 收编 |
| `agent.cancel(cause, {keepInbox})` | 清 pending（除非 keepInbox）并 abort 在途；无活动时 no-op | cancel 的成员侧动作（landed 收编经 inbox 读取） |
| `agent.inject / followup / send` | next-step 不唤醒 / next-turn 唤醒 / 通用入口 | 静止装配驱动（drain+message 的注入形态） |

**Rationale**: 061 的 inbox 语义已源码级核实（`specs/061-team-queue-steer/spec.md` Clarifications：双列表/三入口/claim 批语义/cancel durable 清空）；本 feature 的 deliver 分流即对其 API 的直接消费，适配器内不再有自建队列。

**Alternatives considered**: 保留编排器手写 idleWaiters 机械（否决——裁定 16 要求完成判断收敛成员适配器，原生 `whenIdle` 更薄且免维护）。

## D4: memory 库两层导出面

**Decision**:

1. **功能单元层**：`buildMemoryUnit({scope, memoryClient, callUnary})`（memory 工具 + `memory:snapshot` + `ready=prefetch`——**无 guidance section**，用户裁定 2026-10-14：memory 工具只有一个，单工具的使用说明已由 `MEMORY_TOOL_DESCRIPTION` 完整承载〔快照近因 top-10 与全量写语义，`common/js/memory/src/tool.ts:40-49`〕）与 `buildWikiUnit({template, wikiClient, callUnary})`（三 wiki 工具 + `wiki:guidance` + `ready` 空操作）两个独立工厂；face/store 随之拆分——`MemoryStore` 只持 memoryClient（face key `memory`），`WikiStore` 只持 wikiClient（face key `wiki`，wiki 工具执行解析改经 `exec.agent.ctx.wiki`，memory 工具仍经 `exec.agent.ctx.memory`）；策略函数 seam 见 D10。
2. **组合层**：组合工厂将两单元合并 + 注入组合层自有的**跨存储分工 section**（`memory:guidance`(110)，现状 division 文本归组合层所有）——等价锚点：组合产物 = 现状 `buildMemoryCapabilities` 输出（工具集 ×4、section `memory:guidance`(110)/`wiki:guidance`(120)/`memory:snapshot`(200) 与 order 逐项一致）。

**Rationale**: 单元独立性要求工具解析自洽（spec FR-002）——现状 wiki 方法挂在 memory face 上（`common/js/memory/src/face.ts` 的 `wikiSearch/wikiGet/wikiWrite`）必须拆开。memory 单元无 guidance 的依据（已验证）：`MEMORY_TOOL_DESCRIPTION` 已完整承载单工具使用说明与快照语义，跨局持久意图由 persona 文本承载（`projects/game/agent_v2/cordis.yml` planner persona"把值得跨局保留的观察写入长期记忆"）——guidance section 无独有信息可承载；wiki 单元的 `wiki:guidance` 描述三工具协作工作流（search→get→write），单工具 description 无法承载，故保留。分工文本（memory vs wiki 边界）只在两存储并存时有意义，故归组合层。wiki 的 scope 只有 template 半边（069：一模板一 wiki 集），故 `buildWikiUnit` 收 template 而非完整 MemoryScope。

**Alternatives considered**: memory-only guidance 文本（本 feature 早先方案，用户裁定否决——单工具无独有 guidance 信息，徒增一个 section）；保留合并 face + wiki 工具继续经 memory face 解析（否决——wiki 单元独立挂载时仍需 memory face 在场，违背独立性）；memory 单元带可选 wiki 参数（否决——单元工厂参数化组合语义，两层边界模糊）。

**边界澄清**：wiki 工具的执行解析路径变更（face key）属库内结构重组，不属"wiki 能力改动"——工具 schema/文案/语义/基建全部不变（spec FR-004/FR-005）；`@dominion/common-js-memory` 的唯一库外消费方是 saolei-memory 适配器（已验证），拆分无其他波及面。

## D5: wiki 注入移除面

**Decision**: `@dominion/dsh-saolei-memory` 适配器只构造 MemoryService client（WikiService client 构造删除），`memoryMount.build(scope)` 改为产出 memory 单元。

**验证事实**：

- 注入链唯一：适配器 → `memoryMount` provide → 编排器 `registerMemory`（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts:921-940`，仅 planner 挂载）——无其他 wiki 注入路径（player preset persona 与 planner persona 文本均无 wiki 引用，`projects/game/agent_v2/cordis.yml` preset-authoring 行）。
- 类型 seam 解耦：saolei-loop 的 `MemoryCapabilities` 是结构类型（faces value 为 `unknown`，`orchestrator.ts:245-259`），库内导出面重组不破坏编排器编译面；`MemoryCapabilitiesDeps` 类型（含 wikiClient）仅库与适配器使用。
- 基建面不动：`projects/game/game.proto` WikiService、memory 服务 wiki 实现（`projects/game/memory/`）、gateway `/api/v1` wiki 路由、web `WikiView.tsx`、`projects/game/testplan/wiki_test.go`（挂 `system_test.yaml`）全部零改动。

## D6: announcer 成员化与更名面

**Decision 与验证事实**：

1. **proto 消息形状零变更**：`Team.members`/`active_member`/`TeamMember` 均为既有字段（string role/repeated），announcer 进入名册是**宿主行为**变更（`toTeamView` 增加第三项）；proto 注释（"player/planner members"、"members/player or members/planner"）需同步修订（`projects/game/agent_v2.proto:27-75/284-320`）。
2. **导出更名清单**（`common/js/dsh-plugins/saolei-loop/src/announcer.ts`）：`SAOLEI_MEMBER_ROLE`("saolei") → `ANNOUNCER_MEMBER_ROLE`("announcer")、`SAOLEI_MEMBER_SUMMARY` → `ANNOUNCER_MEMBER_SUMMARY`（summary 文本随之改写为 announcer 语义）、`SaoleiSystemMember` 类 → `AnnouncerMember`、source id 前缀 `${session}/saolei` → `${session}/announcer`；消费点：`projects/game/agent_v2/src/session.ts`（订阅投影 `appendAnnouncement(SAOLEI_MEMBER_ROLE, ...)` :563）、`history.ts`、065 契约、agent_v2 README、测试断言。
3. **宿主放宽点**：`validateSaoleiMembers`（`session.ts:880-894`，请求侧两成员校验**保持**）、`toTeamView`（:1009-1021，名册数组增加 announcer 项：role="announcer"、preset/model/systemPrompt 空串）、`MemberRole`/`TeamRole` 类型纳入 announcer、`GetTeamMember` 路径服务 announcer（output-only system_prompt 空值）。
4. **web 渲染点**（已验证存在名册渲染）：`projects/game/web/frontend/src/App.tsx:451`（`team?.members?.map` 名册展示）与 `TeamSettingsPanel.tsx:173`（物化面板成员列表；preset 下拉 :70-74 仍按 player/planner 过滤，不受影响）；激活指示 `App.tsx:381/440` 为字符串直渲染（wire 角色串透传，065 §4 "web 前端零改动"结论的沿用面）。

**Rationale**: 呈现统一（裁定 23）的最小落地 = 宿主视图 + 注释 + web 兼容渲染；消息形状不变使 gateway/proto 编译面零波及。

**Alternatives considered**: proto 增加成员类型字段区分 LLM/非 LLM 成员（否决——`TeamMember` 现有字段已可表达空 preset/model 形态，加字段是无信息量扩张）。

## D7: 错误码宿主呈现 seam

**Decision**: loop 层在快照/失败事实中透传 code（六码）；宿主（session 层）维护 code → 用户动作文案映射并在既有失败帧/快照面携带；web 沿既有错误渲染路径显示文本（`App.tsx` errorMessage 路径），不新增独立错误组件。流收束的 origin 二分（member-turn 干净 / orchestration INTERNAL）保持（`specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md` §2）。

**Rationale**: spec FR-107 只约束"宿主按码产出文案、loop 不产出"；web 既有渲染已覆盖文本显示，最小改动即闭环。

## D8: 061 supersession 处置

**Decision**: 061 spec 头部增补 supersession 注记：其静止半边（编排 FIFO / R2）由本 feature FR-105 取代，在途半边（steer → inbox，R1/R3）经成员域 `deliver` 落地（语义等价引用其 `contracts/orchestrator-input.md` §1 在途分支与 `contracts/web-queue-ui.md` 的 chip 生命周期——web 面契约**整体沿用**，仅消费信号的产生路径从编排 FIFO 变为成员 inbox）。061 的 plan/tasks 不再执行。

**Rationale**: survey 裁定 17；061 的 web 契约与 mid-turn 语义经审核仍然正确，重写无收益。

## D9: 行为保持验收基准

**Decision**: `common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts`（9 个 describe ≈40 用例：transitions/announcement/queue-priority/cancel-dispose/failure/rollback/natural-stop 等）为批3 的行为保持基准，两点显式调整（spec FR-113）：(a) 多消息时序按 061 SC 方向（mid-turn 进入）重写 queue-priority 相关断言；(b) cancel 打断 review 在途的现状竞态（记录被错误消费，`orchestrator.ts:1042-1049`）按 aborted 冻结语义修正断言。

**验证事实**: survey §9 已给出 S1–S10 → 目标结构的 10/10 映射表；`orchestrator.test.ts` describe 结构与场景一一对应（survey §9 测试锚点）。

## D10: unary 调用策略去冗——随 client 注入（用户裁定 2026-10-14，并入批1）

**Decision**: memory 库不再自持 unary 调用策略副本。单元/组合工厂的组装 seam 在 client 之外同时收一个**结构类型的策略函数**（与 `ctx.grpc.callUnary` 结构兼容），saolei-memory 组装时把 `ctx.grpc.callUnary` 传入；`common/js/memory/src/store.ts` 的私有策略副本（`UnaryInvoke`/`callUnary`/`probeChannel`，:357-431——`callUnary` :377-403、`probeChannel` :416-431）删除。

**Rationale（调研事实）**:

1. **库从未自持 client**——client 实例一直由 saolei-memory 经 `ctx.grpc.client<T>()` 构造后注入（`MemoryClient`/`WikiClient` 构造函数，`store.ts:443/:591`）；自持的是每次调用套用的**调用策略**（waitForReady + 10s deadline + 通道探测 + 失败日志，约 75 行，与 grpc-runtime `unary.ts` 几乎逐行相同）。
2. **`ctx.grpc.client<T>()` 返回裸 grpc-js client**（grpc-runtime `client.ts:192-199`：`new ServiceClient(..., {channelOverride})` 直接 `as T`，无策略包装）——裸调用无 waitForReady/deadline/探测/日志，策略必须随调用发生。
3. 074 契约 §3 称"两处各持一份是依赖方向的必然"——**该论断被夸大**：重复是服务面形态选择（策略挂 ctx 方法 + client 返回裸对象）的结果；策略函数可以像 client 一样作为结构类型注入，依赖方向规则（纯库不 import 插件包）不破。
4. **`ctx.grpc.callUnary` 生产代码零消费**（grep 验证：仅 grpc-runtime 自身测试调用）——真正在跑的策略是库内副本，ctx 面是死面，该形态从未被实际使用承重。
5. 策略唯一化到 grpc-runtime（`common/js/dsh-plugins/grpc-runtime/src/unary.test.ts` 已覆盖策略行为）；库测试改为注入替身（与 client 替身同型，`store.ts` 头注释的测试哲学不变）。

**Alternatives considered**: A 插件 `client()` 返回方法级策略包装对象（否决——需动态区分 RPC 方法与 Client API 面〔`getChannel`/`close` 等〕，包装脆弱，且改变仓库唯一 proto-object cast 点的既有语义）；C 维持现状（否决——与用户设计判断冲突，且 ctx 面零消费证明其未被承重）。

**契约修订**: `specs/074-dsh-grpc-runtime/contracts/memory-library.md` §3 的"两处各持一份是依赖方向的必然，非待收敛的重复"表述撤销，改为策略注入 seam 描述（策略实现唯一归属 grpc-runtime，库经组装 seam 消费）。
