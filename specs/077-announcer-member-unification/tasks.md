# Tasks: 077 成员统一性补缺（announcer 同权 + team 统一消息流 + 在途广播快路）

**Input**: Design documents from `/specs/077-announcer-member-unification/`

**Prerequisites**: `specs/077-announcer-member-unification/plan.md`（技术上下文/结构）、`specs/077-announcer-member-unification/spec.md`（US1 P1/US2 P2/US3 P3 与 FR/SC）、`specs/077-announcer-member-unification/research.md`（D1–D9）、`specs/077-announcer-member-unification/data-model.md`、`specs/077-announcer-member-unification/contracts/`（member-event-stream / inflight-broadcast-fastpath / host-member-parity）、`specs/077-announcer-member-unification/quickstart.md`（均已存在）

**通用约定**（全部 phase 适用）：

- 编译 + 单测随**每次代码变更**执行（宪法 IV，不单列 task）：`bazel build` + `bazel test` 相关 target（各 phase 门禁列出）。
- 无新依赖、无 package.json/tsconfig 变更；新测试用例写入既有 `*.test.ts`（各包 `vitest_test` 的 `src/**/*.ts` glob 已覆盖，预期无 BUILD 变更；如 gazelle 需刷新在对应目录执行 `bazel run //:gazelle <dir>`）。
- 代码书写遵循各 phase 文档清单「代码规范文档」桶所列规范（本 feature 三包均为 `style/javascript.md` 及其引用的 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）。`style/javascript.md` 引用的**模块格式类**外部文档（TypeScript modules reference / Node ESM / swc / rules_ts）本 feature 各 phase 均不列出——本 feature 无 package.json/tsconfig/构建配置变更，任务不涉及模块解析与 loader 形态（宪法 V 引用传递的显式豁免）。
- 注释只表述终态与"为什么"（宪法 VII），引用带仓库相对路径或完整 URL。

---

## Phase 1: Setup

**Goal**: 无（存量仓库内部重构，无项目初始化任务——无新包/依赖/构建配置）。

**文档清单**（编码前必读；宪法 V 三分类）：

- **代码规范文档**：无（本 phase 无编码任务）
- **官方文档**：无
- **技术文章/技术参考文档**：无（各实现 phase 自带文档清单）

---

## Phase 2: User Story 1 - team 统一成员消息流与宿主投影收敛 (Priority: P1) 🎯 MVP

**Goal**: team 插件成为宿主消费的成员消息流权威（`TeamMemberSource.live` + `TeamHandle.events()`），orchestrator 零形状传递，宿主三路订阅收敛为单路——投影对外行为零变化。

**Independent Test**: `specs/077-announcer-member-unification/quickstart.md` §2 team/saolei-loop/agent_v2 三包 `bazel test` 全绿 + 门禁 grep（无逐成员根事件订阅/无 announcer 独立订阅）。

### 文档清单（编码前必读；宪法 V 三分类）

- **代码规范文档**：`style/javascript.md`（ESM/测试书写规则——相对导入带 `.js`、`export type` 显式、DI/`vi.fn()` seam、`vi.mock` 拦截必须正向断言）；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：[dsh-agent 0.2.0-rc.2 README](https://unpkg.com/@deepseek-ai/dsh-agent@0.2.0-rc.2/README.md)（agent 事件词汇——live 面订阅的 `agent/assistant-stream|agent/status|agent/error` 载荷与 `followup/steer/inject` 语义；本地同源副本 `common/js/dsh-plugins/team/node_modules/@deepseek-ai/dsh-agent/README.md`）；[Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（`style/javascript.md` 的 DI/`vi.fn()` 优先、禁新增 `vi.mock` 模块拦截约定之依据——本 phase 单测书写）
- **技术文章/技术参考文档**：`specs/077-announcer-member-unification/research.md` D1–D3/D8；`specs/077-announcer-member-unification/contracts/member-event-stream.md`（全部——接口/传输/传递/消费/验收面）；`specs/077-announcer-member-unification/data-model.md` §1/§3.3；`specs/077-announcer-member-unification/quickstart.md` §2/§3（本 phase 的验证清单与门禁）；`survey/saolei-hook-scripts.md` §2.4（K4 目标方向）；`specs/065-agent-v2-team-refine/contracts/team-member-source.md`（修订基线——扩展点所在）；`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §1（修订基线——服务面行为规范）

### Implementation

- [X] T001 [US1] team 插件统一流基础：`common/js/dsh-plugins/team/src/team.ts` 定义 `TeamLiveEvent` 联合、`TeamMemberEvent` 信封（role/memberId/channel: "log"|"live"/event）、`TeamEventStream`、`TeamMemberSource.live` 可选面、`TeamHandle.events()`；`agentMemberSource` 实现 live 面（订阅根事件 `agent/assistant-stream|agent/status|agent/error`，按 agent 身份过滤并剥离身份字段——成员归属由信封承载）；既有 `source.subscribe` 订阅改为**同源双消费者扇出**（relay 处理 + 流订阅者通知，同一事件同一序）；成员移除/`disposeTeam` 后流不再有事件。扩展 `common/js/dsh-plugins/team/src/team.test.ts`：信封标注正确、log 通道与 relay 同事件同序、无 live 面成员（announce-only double）只产生 log 通道事件、dispose 后无事件（契约 §6 验收面）
- [X] T002 [US1] orchestrator 零形状读取面：`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 新增只读方法 `teamEvents(): TeamEventStream`——直接返回 `this.teamRegistration.events()`（无场景词汇、无自有形状）；未物化/disposed 调用沿用 `OrchestratorStateError` 既有语义。扩展 `common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts`：传递断言（返回 team 流对象）+ 状态错误
- [X] T003 [P] [US1] ⚛ 原子变更组 A（与 T004 同组：两任务的代码与测试改动完成后统一跑门禁，期间不提交）MemberCollector 输入入口化：`projects/game/agent_v2/src/history.ts`——构造器取消四类根事件订阅（`ctx.on("session/event"|"agent/assistant-stream"|"agent/status"|"agent/error")`），改为公开信封分发入口 `applyEvent(envelope: TeamMemberEvent): void`（log 通道 → 既有 `onSessionEvent` 处理、live 通道 → 既有 `onAssistantStream`/`onStatus`/`onError` 处理；形态见 `specs/077-announcer-member-unification/data-model.md` §3.3 与 `specs/077-announcer-member-unification/contracts/member-event-stream.md` §5）；turn 簿记/流式块/工具 settle/idle 收束/`markOutcome` 记账机械不变；`dispose` 形态保持。同步切换 `projects/game/agent_v2/src/history.test.ts` 驱动面（`new MemberCollector(...)` 及其经 mock ctx 根事件驱动的 `emit` 改为经 `applyEvent` 入口驱动），**全部既有断言不变**
- [X] T004 [US1] ⚛ 原子变更组 A（与 T003 同组，见上）宿主单路消费：`projects/game/agent_v2/src/session.ts`——`doMaterialize` 经 `orchestrator.teamEvents()` 单路订阅，按信封 `role` 分发（agent 成员 → `collector.applyEvent(envelope)`；announcer 的 log 通道 `assistant/message` → 既有 `history.appendAnnouncement(ANNOUNCER_MEMBER_ROLE, text)`）；`TeamEntry` 增 `streamOff` 承接退订职责（`announceOff` 独立订阅删除）；`teardownEntry` 单路退订。`projects/game/agent_v2/src/session.test.ts` 驱动面切换：`emit` helper 改为经统一流注入事件（真实 `Team` 服务或成员 source double），**全部既有行为断言不变**（FR-003 行为等价基准）

**Checkpoint / 验证门禁**：`bazel test //common/js/dsh-plugins/team/... //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/...` 全绿（门禁 3 以**原子变更组 A**为单位满足——T003+T004 完成后统一校验）；`grep -rn 'ctx.on("session/event"\|ctx.on("agent/' projects/game/agent_v2/src/` 无匹配（逐成员根事件订阅消失，含测试替身）；`grep -n "announceOff" projects/game/agent_v2/src/session.ts` 无匹配。

---

## Phase 3: User Story 2 - announcer 宿主统一 member 化 (Priority: P2)

**Goal**: `TeamEntry` 统一持有三成员（MemberRuntime 判别联合），名册/成员读取/成员视图/teardown 按成员属性派生——特例代码消失，076 呈现契约回归保持。

**Independent Test**: `specs/077-announcer-member-unification/quickstart.md` §3 特例符号 grep 零残留 + 076 announcer 契约验收面（`specs/076-agent-v2-optimize/contracts/announcer-member.md` §6）单测全绿。

### 文档清单（编码前必读；宪法 V 三分类）

- **代码规范文档**：`style/javascript.md`（ESM/测试书写规则）；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：无第三方 API 语义依赖；测试书写依据 [Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（`style/javascript.md` 的 DI/`vi.fn()` 优先、禁新增 `vi.mock` 模块拦截约定之依据）
- **技术文章/技术参考文档**：`specs/077-announcer-member-unification/research.md` D7–D8；`specs/077-announcer-member-unification/contracts/host-member-parity.md`（全部——派生规则表/回归基准/消失清单）；`specs/077-announcer-member-unification/data-model.md` §3；`specs/077-announcer-member-unification/contracts/member-event-stream.md` §5（宿主消费形态——US1 衔接）；`specs/077-announcer-member-unification/quickstart.md` §2/§3（本 phase 的验证清单与门禁）；`specs/076-agent-v2-optimize/contracts/announcer-member.md`（回归基准，§3/§6）；`specs/076-agent-v2-optimize/contracts/flow-orchestration.md` §7（宿主联动面——修订基线）

### Implementation

- [X] T005 [US2] 统一成员持有：`projects/game/agent_v2/src/session.ts`——`MemberRuntime` 改判别联合 `{ kind: "agent"; role; preset; model; handle; collector } | { kind: "announce-only"; role }`（`specs/077-announcer-member-unification/data-model.md` §3.1）；`TeamEntry.members` 容纳三成员；`createMemberRuntime` 与物化装配适配（announcer 项无 runtime 构造）；`teardownEntry` 按统一成员列表释放，在途成员 `markOutcome` 经成员 kind 过滤（取代 `activeAgentMember` 角色串判定）
- [X] T006 [US2] 视图属性派生：`projects/game/agent_v2/src/session.ts`——`toTeamView` 遍历统一成员集合派生名册（announce-only ⇒ 空 preset/model 项；**删除 `announcerMemberView` 函数与三成员拼装特判**）；`getTeamMember` 按 kind 分派（announce-only ⇒ 同步空形态视图、无 assembly/重试；**删除 `member === ANNOUNCER_MEMBER_ROLE` 提前 return 分支**；agent 路径不变）；`listMemberMessages` 按 kind 判定 NOT_FOUND（取代 player/planner 角色硬编码）；**删除 `isAgentMemberRole`/`activeAgentMember` 辅助**（`isMemberRole` 保留仅作 wire 资源名合法性校验，不得用于成员类别分派）
- [X] T007 [US2] 回归与消失断言：`projects/game/agent_v2/src/session.test.ts`——076 announcer 契约验收面回归全绿（三成员名册、`GetTeamMember("announcer")` 空形态、请求侧校验拒绝 caller-supplied announcer、刷新后旧生命周期播报不进新投影）；新增特例消失断言（roster 由统一集合派生、teardown 单路）

**Checkpoint / 验证门禁**：`bazel test //projects/game/agent_v2/...` 全绿；[quickstart.md](quickstart.md) §3 两条 grep 零匹配（`announcerMemberView|announceOff`、`isAgentMemberRole|activeAgentMember`）。

---

## Phase 4: User Story 3 - 在途广播快路 (Priority: P3)

**Goal**: 插入式产出（message 单元）经与 drain 相同的构造路径 + source 投递面送达当前在途成员下一 step；锚点闭包 exactly-once、发言级顺序一致、驱动零影响（hook 落地前生产行为惰性，单测承载——spec Clarifications 2026-10-16）。

**Independent Test**: `specs/077-announcer-member-unification/quickstart.md` §2 快路行为（SC-003 四项）+ 顺序一致性 + 驱动零影响断言全绿（team/saolei-loop 单测）。

### 文档清单（编码前必读；宪法 V 三分类）

- **代码规范文档**：`style/javascript.md`（ESM/测试书写规则）；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：[dsh-agent 0.2.0-rc.2 README](https://unpkg.com/@deepseek-ai/dsh-agent@0.2.0-rc.2/README.md)（`inject()` 不唤醒、"lands in the next admitted step"、`cancel` 清 inbox、`whenIdle` 语义）；[dsh-agent-loop 0.2.0-rc.2 README](https://unpkg.com/@deepseek-ai/dsh-agent-loop@0.2.0-rc.2/README.md)（step/turn 认领机——"At a turn boundary it opens the durable turn, then atomically claims pending next-step input plus one queued prompt; between steps it claims only next-step input"；本地同源副本 `projects/game/agent_v2/node_modules/@deepseek-ai/dsh-agent-loop/README.md`）；[Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（`style/javascript.md` 的 DI/`vi.fn()` 优先、禁新增 `vi.mock` 模块拦截约定之依据——本 phase 单测书写）
- **技术文章/技术参考文档**：`specs/077-announcer-member-unification/research.md` D4–D6；`specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md`（全部——触发定义/机械/exactly-once/顺序/零影响/边缘/验收面）；`specs/077-announcer-member-unification/data-model.md` §1.1/§2（投递面字段所在接口 + 生命周期状态图）；`specs/077-announcer-member-unification/quickstart.md` §2（本 phase 的验证清单）；`survey/saolei-hook-scripts.md` §2.2.4/§2.2.5（裁定语义）；`specs/065-agent-v2-team-refine/contracts/team-member-source.md` §1（接口扩展点——`deliverInFlight` 字段所在）/§2（消费闭包基线）；`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §1 第 7 条（「team 只投递不驱动」的投递面归属）；`specs/061-team-queue-steer/contracts/orchestrator-input.md`（在途投递/inbox 边界——steer 共存语义）

### Implementation

- [X] T008 [US3] team 快路机械：`common/js/dsh-plugins/team/src/team.ts`——`TeamMemberSource` 增可选 `deliverInFlight?: (message: UserMessage) => void`（与 `consumes` 同级的结构性属性）；`relay()` 对 **message 单元**（`kind === "message"`）：pending 锚点追加照常（基线不动）→ 经 `buildMessage`（与 drain 完全相同构造路径）构建广播消息 → 对每个带投递面的 consuming 接收方调用（`try/catch` 捕获 + warn 日志回落，不向 announce 路径上抛）；**tool 单元不触发**。扩展 `common/js/dsh-plugins/team/src/team.test.ts`：①在途投递收到的消息与 drain 构造逐字段等价（同锚点/`team-broadcast` source/渲染文本）；②快路认领后 `drain` 前置 reconcile 剔除 pending 副本（恰好一次）；③静止接收方（投递面 no-op double）pending 照常 drain；④投递面 throw → relay 不上抛、pending 不受扰（warn 断言）；⑤tool 单元不调用投递面；⑥终局统计播报场景（announcer 生产、无在途接收方）——各接收方投递面策略 no-op、relay 不上抛、pending 照常 drain（现状行为不变；对应 spec SC-003 第④项）
- [X] T009 [US3] 成员侧策略与供给：`common/js/dsh-plugins/saolei-loop/src/agent-member.ts`——新增投递面策略方法 `deliverInFlight(message: UserMessage): void`（`busy` → `this.agent.inject(message)`；静止 → no-op；异常捕获 + warn 回落，见 `specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md` §2.1）；`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` `createMember` 组合供给（注册的 source 对象携带 `deliverInFlight` 闭包委托 `AgentMember.deliverInFlight`，announcer 注册项不带——结构性缺席）。扩展 `common/js/dsh-plugins/saolei-loop/src/agent-member.test.ts` 与 `orchestrator.test.ts`：inject 不唤醒断言（无新 turn、`whenSettled`/idle 语义不变）、`steered`/`pendingInput` 计数不含快路消息、静止 no-op、注册面供给/缺席断言
- [X] T010 [US3] 顺序一致性断言：`common/js/dsh-plugins/team/src/team.test.ts`——跨成员发言级顺序一致（快路接收方 log 的共享消息相对序 = 其他成员 drain 序 = 生产序）；工具单元级偏差仅出现在单 step 窗口内（`specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md` §4 披露形态的断言化）

**Checkpoint / 验证门禁**：`bazel test //common/js/dsh-plugins/team/... //common/js/dsh-plugins/saolei-loop/...` 全绿。

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: 快路同锚点双投收口（research.md D10：cancel 双向自愈 + turn 失败残留清扫 + 场景 6 断言）、契约终态修订、README 同步、最终验收（宪法 VI 大型测试门禁）。

### 文档清单（编码前必读；宪法 V 三分类）

- **代码规范文档**：`style/javascript.md`（T014–T016 编码任务——ESM/测试书写规则：相对导入带 `.js`、`export type` 显式、DI/`vi.fn()` seam）；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）；`style/large_test.md`（T013 testplan 执行约定；"FOR Agent: 使用 testplan SKILL"）
- **官方文档**：[dsh-agent 0.2.0-rc.2 README](https://unpkg.com/@deepseek-ai/dsh-agent@0.2.0-rc.2/README.md)（T014–T015——`cancel` 默认清 inbox、结构面 `Inbox` 的 `nextStep`/`nextTurn`/`remove`、`inject` 落 next admitted step；本地同源副本 `common/js/dsh-plugins/team/node_modules/@deepseek-ai/dsh-agent/README.md`）；[dsh-agent-loop 0.2.0-rc.2 README](https://unpkg.com/@deepseek-ai/dsh-agent-loop@0.2.0-rc.2/README.md)（T015——turn 开启"atomically claims pending next-step input plus one queued prompt"、失败 turn 不清 inbox 仅 cancel 清 pending work；本地同源副本 `projects/game/agent_v2/node_modules/@deepseek-ai/dsh-agent-loop/README.md`）；[Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（`style/javascript.md` 的 DI/`vi.fn()` 优先、禁新增 `vi.mock` 模块拦截约定之依据——T014–T016 单测书写）
- **技术文章/技术参考文档**：`specs/077-announcer-member-unification/research.md` D10（T014–T016 裁定：两窗口缺陷机械核实、单一可靠性载体、被否决备选）；`specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md`（§2.1 inbox 卫生/§3 单一载体/§6 边缘路径/§7 验收面——T014–T016 的契约终态）；`specs/077-announcer-member-unification/data-model.md` §2（状态转移与不变量）；`specs/061-team-queue-steer/contracts/orchestrator-input.md` §1/§4（steer 残留随 turn 边界认领、landed flush 统一规则——用户消息面不变、T014/T015 清扫范围边界）；`specs/076-agent-v2-optimize/contracts/flow-orchestration.md` §6（cancel ② landed 范围收窄的修订基线，T011）；`specs/077-announcer-member-unification/quickstart.md`（全部——验证清单）；`projects/game/testplan/system_test.yaml`（T013 执行对象——四套件与部署拓扑）；`specs/077-announcer-member-unification/spec.md`（FR-013/SC-003/SC-006/SC-007）；本 feature 三份契约（修订语义来源）——`specs/077-announcer-member-unification/contracts/member-event-stream.md`、`specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md`、`specs/077-announcer-member-unification/contracts/host-member-parity.md`；被修订契约原文——`specs/065-agent-v2-team-refine/contracts/team-member-source.md`、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`、`specs/076-agent-v2-optimize/contracts/flow-orchestration.md`、`specs/076-agent-v2-optimize/contracts/announcer-member.md`

### Implementation

- [X] T011 [P] 契约终态修订（宪法 VII：直接修订原文，不留增量痕迹；修订处引用 `specs/077-announcer-member-unification/spec.md` 与本 feature `contracts/`）：①`specs/065-agent-v2-team-refine/contracts/team-member-source.md`——§1 接口增 `live`/`deliverInFlight` 可选面、§2 传输语义增统一流同源扇出与快路投递规则；②`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §1——服务面增 `TeamHandle.events()`、行为规范第 3 条增投递时机（快路叠加 pending）、**第 7 条「team 只投递不驱动」增投递面归属说明**（team 只调用成员供给的 `deliverInFlight` 闭包，不直接调用 `agent.inject`——inject 由成员适配器执行，该条不变量保持成立）；③`specs/076-agent-v2-optimize/contracts/flow-orchestration.md`——§3 item 4 两类输入分层注记（广播投递时机：拉动式 drain + 快路提前投递）、§6 cancel ②「未消费输入落地为消息历史」范围收窄为**用户消息**（`team-broadcast` inbox 残留不落地——pending 基线承担其可靠性，`specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md` §3/§6）与 §7 末行「announcer 订阅投影」改为终态表述（宿主成员事件消费经 team 统一流；**loop 侧 `orchestrator.announcer` 读取面保留**——仍供成员读取与测试）；④`specs/076-agent-v2-optimize/contracts/announcer-member.md` §3 item 3 实现侧表述按成员 kind 属性派生更新（必做：现表述的「放宽硬编码 `player`/`planner` roster guard」由判别联合派生取代）
- [X] T012 [P] `projects/game/agent_v2/README.md` 更新：成员投影单路消费（team 统一流）、announcer 同权属性派生、快路机制与生产行为惰性说明（引用 `specs/077-announcer-member-unification/`）
- [X] T014 [US3] cancel 双向自愈收口（W1，research.md D10①；契约 §6/§7）：`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` `landUnclaimed` 只收编 `source.kind === "user"` 的未认领 inbox 消息——`team-broadcast` 残留不进 landed（放行给随后 `agent.cancel` 默认清 inbox），锚点未消费由 pending 基线自愈重投（landed 语义收窄为用户未消费输入，076 §6 ② 表述由 T011 同步）。扩展 `common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts`（US3 场景 5 / spec SC-003 第⑤项）：在途成员经投递面注入广播副本（`deliverInFlight`，复用既有 `relay(...)` 构造）+ fake team queue 预置同锚点 pending 副本 → `cancel()` 断言 `dropped` 与 landed 不含广播副本 → 恢复 `submit` 后该锚点在 followup/injection 批中**恰出现一次**（drain 副本、位于用户消息之前）
- [X] T015 [US3] turn 失败残留清扫（W2，research.md D10②；契约 §2.1/§7）：`common/js/dsh-plugins/saolei-loop/src/agent-member.ts` `startTurn` 守卫通过后、装配批注入前，将 `agent.inbox`（`nextStep`/`nextTurn`）中 `source.kind === "team-broadcast"` 的未认领消息经 `inbox.remove` 清除（未认领 ⇒ 锚点未消费 ⇒ pending 派生重投恰一份；用户消息残留不清扫——061 §1 义务 2 语义）。`common/js/dsh-plugins/saolei-loop/src/agent-member.test.ts` fake agent 增 inbox double（`inject` 落 `nextStep`、`remove` 按 message id 移除）；新增断言：drive 在途 → `deliverInFlight` 注入 → `failTurn`（残留存活 inbox）→ 恢复 drive（drain 含同锚点副本）→ 残留被清除、新批恰一份；同窗口用户消息（steer）残留不被清除。`common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts` `createFakeMember` 的 inbox double 同步补 `remove`（既有测试 drive 批中的 relay 副本留存 fake inbox，清扫调用需要该结构面；移除不影响 injections/followups 断言面）
- [X] T016 [US3] [P] 同 step 窗口多条断言（US3 场景 6 / spec SC-003 第⑥项；契约 §7⑦）：`common/js/dsh-plugins/team/src/team.test.ts`——同一 step 窗口两条播报连续生产：带投递面的接收方按生产序 FIFO 收到两条（`source.messageId` 各异、与 drain 构造逐字段等价）；其一被认领（接收方 log 出现该锚点的 `user/message` 注入）后 drain/reconcile 剔除该锚点、其余锚点 pending 独立存活——各锚点独立闭包、无重排与重复
- [X] T013 最终验收：按 `specs/077-announcer-member-unification/quickstart.md` 执行——§2 三包 `bazel build`/`bazel test` 全绿（含 T014–T016 新增断言）；§3 特例消失 grep 零匹配；§4 经 **testplan SKILL** 实际执行 `guitar run projects/game/testplan/system_test.yaml` 全套件（部署→测试→清理闭环），**全部用例通过**（宪法 VI：仅构建检查不构成验收；任何 failed/flaky 即验收未通过）

**Checkpoint / 验证门禁**：T014–T016 完成后 `bazel test //common/js/dsh-plugins/team/... //common/js/dsh-plugins/saolei-loop/...` 全绿（新增：cancel 双向自愈恰一份、失败残留清扫恰一份且用户残留不动、同 step 窗口 FIFO/独立闭包）；T011–T012 文档修订落盘（引用可追溯）；T013 全绿后 feature 达到 spec SC-001–SC-007。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**: 无任务。
- **Phase 2（US1, P1）**: 起点；T001 → T002（T002 与 T003 不同文件可并行）→ **T003+T004 原子变更组 A**（T004 依赖 T002；组内两任务的代码与测试改动完成后统一跑门禁，不单任务提交——`history.test.ts`/`session.test.ts` 的驱动面切换分属两任务，缺一即单测不通过）。
- **Phase 3（US2, P2）**: 依赖 Phase 2 完成（`streamOff`/统一流消费落地后，entry 形态重整才不冲突——session.ts 同文件串行）。
- **Phase 4（US3, P3）**: 依赖 T001（source 类型扩展）；与 Phase 3 无文件冲突（team/loop 侧），但按优先级在 Phase 3 后执行；T008 → T009 → T010。
- **Phase 5（Polish）**: 依赖 Phase 2–4 全部完成（契约修订反映终态；大型测试为全量验收）。Phase 5 内部：T014 → T015 串行（均落 `orchestrator.test.ts`，T015 依赖 T014 后的 fake inbox 终态）；T011/T012/T016 与 T014–T015 无文件冲突可并行；T013 最后（最终验收含 T014–T016 新增单测）。

### User Story Dependencies

- **US1 (P1)**: 无依赖——MVP。
- **US2 (P2)**: 依赖 US1 的 session.ts 消费面收敛（同文件串行；语义上 announcer 订阅特例由统一流消解后才可删旁路）。
- **US3 (P3)**: 依赖 US1 的 T001（`TeamMemberSource` 类型）；与 US2 无依赖。

### Parallel Opportunities

- T002（saolei-loop/orchestrator.ts）与 T003（agent_v2/history.ts）可并行（不同包不同文件，仅共同依赖 T001 类型；T003 的代码改动以**原子变更组 A**交付，见 Phase Dependencies）。
- T011 与 T012 可并行（不同文件）；T016（team.test.ts）与 Phase 5 其余任务无文件冲突可并行。
- T014 与 T015 串行（同落 `orchestrator.test.ts`）。
- 三包单测互不阻塞：team / saolei-loop / agent_v2 的 `bazel test` 可独立执行。

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. 完成 Phase 2（US1）→ 单独验证（Checkpoint 门禁 + 三包单测）——分层缺口②闭合、announcer 订阅特例消解，已是可交付增量。
2. 若需在此停止：US2/US3 未落地时 K4 缺口①其余特例与快路仍存在——可安全中断（US1 行为等价，无对外变化）。

### Incremental Delivery

1. US1 → 验证（MVP：统一流 + 投影收敛）
2. US2 → 验证（announcer 同权终态 + 076 回归）
3. US3 → 验证（快路机械 + 单测四项/顺序/零影响）
4. Phase 5 → 契约/README 终态 + 大型测试全量验收

---

## Notes

- [P] 标记 = 不同文件且无未完成依赖。
- **原子变更组 A**（T003+T004）不可拆分提交：门禁 3（编译 + 单测）以组为单位满足（宪法 IV）。
- 同一文件的任务（session.ts 的 T004/T005/T006）串行执行，避免并发编辑丢失更新。
- 快路单测中"在途"状态以 double/真实 AgentMember 构造，不依赖 hook 功能（生产行为惰性为已接受状态，真实端到端快路场景由后续 hook feature 携带）。
