# Tasks: team 排队消息 step 边界进入与 turn 语义表述修正

**Input**: Design documents from `/specs/061-team-queue-steer/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: 单测/组件测不单列任务（constitution 原则 IV：编译+单测是每次代码变更的一部分，随各任务执行）；大型测试独立验收任务（原则 VI：实际 `guitar run` 部署→测试→清理闭环、全量通过）。

**Organization**: Tasks grouped by user story。**Setup/Foundational phase 不适用**——本 feature 零新增依赖/包/proto/组合行（plan.md：全部原地修改），直接进入 story phase。P1 顺序 US1 → US2（US2 是 US1 机制的不变量验证）；US3/US4 为 P2（US4 为纯文档修正，可在代码 phase 后随时并行插入）。**执行顺序前置**：`specs/062-team-game-end-handoff/` 先行落地——交互语义（终局收束 × 在途 steer：pending steered 消息使终局 turn 同 turn 延展消费、复盘交接在后）见 spec.md Edge Cases、research.md R3a、contracts/orchestrator-input.md §6，测试面为 quickstart V6（T012）。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1–US4 mapping to spec.md user stories

---

## Phase 1: US1 - 排队消息在下一个 step 随工具结果进入 (Priority: P1) 🎯 MVP

**Goal**: turn 在途时 submit 立即 `steer`（dsh 原生 next-step claim 承载 FR-001）；`snapshot.queued` 语义扩展；消费闭环与静止判定兜底。

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei-loop //projects/game/agent_v2`——orchestrator 单测断言在途 submit → steer 调用、静止路径零回归、计数/闭环/静止兜底；session 单测断言 `queued{position}` 覆盖两路径。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/javascript.md`
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的基准）
- **官方文档**：
  - [@deepseek-ai/dsh-agent README（0.1.1-rc.2，unpkg 官方包内容镜像）](https://unpkg.com/@deepseek-ai/dsh-agent@0.1.1-rc.2/README.md)（`steer`/`inject`/`agent.inbox`/claim 批语义/`cancel`/`agent/status` running 覆盖 drain interval——编排输入面的全部机制依据）
- **技术文章/技术参考文档**：
  - `specs/061-team-queue-steer/spec.md`（FR-001、Clarifications 全部裁定）
  - `specs/061-team-queue-steer/research.md`（R1–R4）
  - `specs/061-team-queue-steer/contracts/orchestrator-input.md`（§1 submit 行为、§2 计数、§3 静止、§5 不变量）
  - `specs/061-team-queue-steer/data-model.md`（§1 生命周期、§2 编排状态、§3 静止判定）
  - `survey/deepseek-harness-turn-step-semantics.md`（§3 dsh turn/step/inbox 语义基准）

### Tasks

- [ ] T001 [US1] `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`：`submit` 增加在途分支（`drivingMember !== null` → `createUserMessage` 后 `member.agent.steer(message)`，消息不入编排 FIFO；静止分支保持现状 FIFO 路径）；`snapshot.queued` 扩展为 `queue.length + steeredPending.size`（新增 `steeredPending: Set<MessageId>`，steer 时 add）；文件头注释（:28-36）收敛——turn 本义表述（删 "turn ended and no further turn is triggered" 触发源式解释，改为 turn 结束即无新模型调用 + 待消化输入）并补 steer 投递路径说明（FR-006 的 orchestrator 落点随本任务原子完成）。`orchestrator.test.ts` 新用例（fake agent double）：在途 submit 断言 `steer` 被调且 FIFO 不收、静止 submit 零回归、`snapshot.queued` 两路径、`SubmitResult.position` = FIFO+pending+1
- [ ] T002 [US1] `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`：steeredPending 消费闭环——经成员事件面订阅 `user/message`（`source.kind === "user"`，与 `MemberCollector` 同源的 `session/event` 面）按 `messageId` 移出；drive 收束（idle 等待返回）清空集合；`whenQuiescent()` 增加兜底——静止判定附加"被驱动成员 `agent.inbox.hasPending === false`"检查（R4 竞态兜底）。`orchestrator.test.ts` 新用例：闭环按 messageId 移出、drive 收束清空、inbox pending 时静止判定不收敛（fake double 呈现 pending）
- [ ] T003 [US1] `projects/game/agent_v2/src/session.ts` + `session.test.ts`：确认 `watchQuiescence` 经扩展后的 snapshot/whenQuiescent **零源码改动**即满足静止语义（`snapshot.queued` 已含 steeredPending——仅注释同步如需）；`session.test.ts` 补用例：成员 turn 在途时 Send 的 `queued{position}` = FIFO + steered-unclaimed + 1（经 `TeamSessionsDeps` double 驱动两路径）

**Checkpoint / 验证门禁**: `bazel build //common/js/dsh-plugins/saolei-loop //projects/game/agent_v2 && bazel test //common/js/dsh-plugins/saolei-loop //projects/game/agent_v2` 通过；在途/静止两路径单测全绿。

---

## Phase 2: US2 - turn 结束回退与切换节点保持不变 (Priority: P1)

**Goal**: 不变量验证——回退由 dsh steer-wake 自愈承担（R3：编排层零新逻辑），切换优先级与"消化优先于切换"经 drain interval 自然保持。**预期零生产代码改动**（若用例失败按 R3 修正实现而非打补丁）。

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei-loop`——自愈/切换优先级/无额外消化三类不变量用例全绿。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/javascript.md`
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的基准）
- **官方文档**：
  - [@deepseek-ai/dsh-agent README（0.1.1-rc.2）](https://unpkg.com/@deepseek-ai/dsh-agent@0.1.1-rc.2/README.md)（`steer` 的 idle-开-turn 语义、`agent/status` running 覆盖 consecutive turns——自愈论证依据）
- **技术文章/技术参考文档**：
  - `specs/061-team-queue-steer/spec.md`（FR-002/FR-003、US2 验收场景、Edge Cases"终局收束与在途 steer 并存"）
  - `specs/061-team-queue-steer/research.md`（R3——含四格竞态窗口表〔stopping 检查点前后两窗口〕、R3a——062 交互）
  - `specs/061-team-queue-steer/contracts/orchestrator-input.md`（§5 不变量、§6 终局收束交互）
  - `specs/061-team-queue-steer/data-model.md`（§1 回退自洽不变量）
  - `specs/062-team-game-end-handoff/spec.md`（Session 2026-09-12 裁定二——"消化优先于复盘"的既有优先级基准）
  - `specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md`（§3 消费语义与本 feature 的交互委托条款）

### Tasks

- [ ] T004 [US2] `common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts` 补不变量用例（fake agent double 呈现 dsh 行为，生产代码零改动预期）：① turn 收尾窗口（idle 事件延迟发出）steer 的消息**不**触发编排层消化 drive（`nextStep` 断言无额外 drive——自愈由 dsh 承担）；② `nextStep` 优先级序回归（排队 > pendingReview > planning/reviewing→player 切换 > gameEnded 复盘 > player 续驱）；③ steered 消息致 running 延伸时 `drive()` 的 idle 等待覆盖至延伸结束（waitForIdle 跨连续 running 区间）

**Checkpoint / 验证门禁**: `bazel test //common/js/dsh-plugins/saolei-loop` 通过；生产代码 diff 为零（或仅 R3 论证内的必要修正）。

---

## Phase 3: US3 - 排队指示与取消语义（landed 解耦层 + 前端消费消除）(Priority: P2)

**Goal**: Cancel 收编未消费消息入 `landed`（Send 零定制）；统一 flush 规则（驱动装配/steer 前，来源无关）；前端 chip 消除改挂 `member_view{sender:"user"}`。

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei-loop //projects/game/web/...`——landed 收编/累积/flush/不触发驱动单测；chip 消费消除（含重复文本）组件/store 测试。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/javascript.md`
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：
  - [@deepseek-ai/dsh-agent README（0.1.1-rc.2）](https://unpkg.com/@deepseek-ai/dsh-agent@0.1.1-rc.2/README.md)（`inject` 非唤醒语义、`cancel` 默认清 inbox/`keepInbox`、inbox 可变更面）
- **技术文章/技术参考文档**：
  - `specs/061-team-queue-steer/spec.md`（FR-004、Session 2026-09-11 三条裁定）
  - `specs/061-team-queue-steer/research.md`（R5——含可达性论证与解耦原则、R6）
  - `specs/061-team-queue-steer/contracts/orchestrator-input.md`（§1 flushLanded 统一规则、§4 cancel）
  - `specs/061-team-queue-steer/contracts/web-queue-ui.md`（§1 生命周期表、§2 文本首匹配、§3 断言面）
  - `specs/061-team-queue-steer/data-model.md`（§1 landed 状态、§2 状态表、§4 前端排队态）

### Tasks

- [ ] T005 [US3] `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`：新增 `landed: UserMessage[]` 与 `flushLanded()`（取空返回）；`cancel()` 收编——`landed.push(...queue.splice(0), ...steeredPending 对应消息对象)`（`agent.cancel({kind:"user"})` 默认清 inbox，landed 已在编排层不受影响）；统一 flush 规则——`nextStep` 驱动装配 `messages = [...relays, ...flushLanded(), 新消息]`、submit 在途分支 steer 前先 `for m of flushLanded(): member.agent.inject(m)`（非唤醒）；landed 不进 `nextStep` 驱动判定、不计入 `snapshot.queued`；消费闭环与 steeredPending 共用（messageId）。`orchestrator.test.ts` 新用例：cancel 收编（FIFO+steered 两来源）、多次取消累积、landed 单独存在时 pump 静止（不空转）、静止/在途两路径 flush、flush 后经 user/message 闭环移出
- [ ] T006 [P] [US3] `projects/game/web/frontend/src/store/chat.ts`：chip 消除改挂 `member_view`——reduce 收到 `member_view` 且 `sender === "user"` 时按首个 text 块内容对 `queue` 文本首匹配移除；`turn_start` 分支的队首出队逻辑（:548-550 及注释）退役删除；`turn_end` 终态全清保持（:691 附近现状）。`chat.test.ts`：改写既有 "turn_start 消费队首" 用例（:247）为 member_view 语义；新增——mid-turn 消费消除（不等 turn_end）、重复文本逐条消除无 double-remove、`sender !== "user"` 不触碰 queue、终态全清回归

**Checkpoint / 验证门禁**: `bazel build //common/js/dsh-plugins/saolei-loop //projects/game/web/... && bazel test //common/js/dsh-plugins/saolei-loop //projects/game/web/...` 通过。

---

## Phase 4: US4 - turn 冗余表述修正（spec 与注释）(Priority: P2)

**Goal**: FR-006/FR-007 全量落地——059 家族切换锚点收敛 + "team turn 持续流"命名 + 049/059 supersession 注记（research.md R8 的 11 条落点〔含 059 `research.md:80/:91` 命名点〕；orchestrator 头注释已随 T001 完成）。

**Independent Test**: SC-004 文本检索断言（T010 的 rg 命令清单）零残留、注记齐备、原文未被重写。

### 文档清单（本 phase 必读）

- **代码规范文档**：无（纯文档修正）
- **官方文档**：无
- **技术文章/技术参考文档**：
  - `specs/061-team-queue-steer/spec.md`（FR-006/FR-007、SC-004、Session 2026-09-11 裁定）
  - `specs/061-team-queue-steer/research.md`（R8——11 条落点清单与逐处修正内容）
  - `specs/061-team-queue-steer/contracts/orchestrator-input.md`（§1/§2/§4——T008 对齐的终态契约文本）
  - `specs/038-queue-input-mid-turn/spec.md`（FR-002/Revision——supersession 注记形态的既有实践参照，T009）
  - `survey/deepseek-harness-turn-step-semantics.md`（术语基准——收敛式表述的依据）
  - 修正目标文件（任务描述逐处指明行号）：`specs/059-agent-v2-team-mode/`（spec.md / data-model.md / research.md / contracts/dsh-plugins.md / contracts/team-api.md）与 `specs/049-agent-v2-dsh-init/`（spec.md / research.md）

### Tasks

- [ ] T007 [P] [US4] `specs/059-agent-v2-team-mode/spec.md`（:47 切换节点 Clarification ② 删除"两种新 turn 触发源"枚举、收敛为"turn 已结束（不再有新的模型调用）且无待消化排队消息"；:49 "team turn 持续流"改称"team 持续流（至 team 静止）"）+ `data-model.md`（:156 planner→player 切换锚点同收敛）+ `research.md`（:59 R6 续驱规则、:110 决策⑦ 同收敛；:80 流生命周期设计行与 :91 Alternatives 行的命名同处理——改称或同行加注"非 dsh turn"澄清）——原文其余语义不动
- [ ] T008 [P] [US4] `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`（:66 结构性续驱条目同收敛；§2 驱动契约更新——:65 `followup()` 简写补 inject 折叠机制说明，新增 steer 投递路径/steeredPending/landed flush/静止判定 inbox 兜底，对齐 `specs/061-team-queue-steer/contracts/orchestrator-input.md` 终态）+ `contracts/team-api.md`（:35 §3.1 "team turn 持续流"命名同 T007）
- [ ] T009 [US4] supersession 注记三处（均不重写原文，注记形态对照 038 为 030 FR-013 加注的既有实践；与 T007 同改 059 `spec.md`——在 T007 后执行）：`specs/049-agent-v2-dsh-init/spec.md`（:153 FR-012 附注——mid-turn 注入是 038 核心行为而非"observe-only 等扩展"，team 形态排队语义以 `specs/061-team-queue-steer/spec.md` 为准）+ `specs/049-agent-v2-dsh-init/research.md`（:199 决策行附注同义）+ `specs/059-agent-v2-team-mode/spec.md`（:181 FR-011 附注——"回合结束后…消化"子句由 061 FR-001/FR-002 supersede，其余子句保持）
- [ ] T010 [US4] SC-004 验证：执行文本检索断言并记录结果——`rg -n '工具调用引发的后续 turn' specs/059-agent-v2-team-mode/ common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 零命中；`rg -n 'team turn' specs/059-agent-v2-team-mode/` 零命中（若采加注形态，每条命中同行 MUST 含"非 dsh turn"澄清语）；049 两处 + 059 FR-011 注记存在（`rg -n 'superses|061-team-queue-steer' specs/049-agent-v2-dsh-init/ specs/059-agent-v2-team-mode/spec.md`）；061 家族术语一致。依赖 T001、T007–T009

**Checkpoint / 验证门禁**: T010 全部断言通过；`git diff` 审阅确认仅注记/表述变更、无原文重写。

---

## Phase 5: 大型测试验收（Final — constitution 原则 VI）

**Goal**: quickstart V1–V4 + V6 场景经 testplan 实际执行全量通过（部署→测试→清理闭环）；V5（文档检索）已由 T010 承载。

**Independent Test**: `guitar run` 全绿——V1 mid-turn 进入、V2 回退/切换、V3 取消/落地、V4 既有全量回归、V6 终局收束 × 在途 steer（062 交互）。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/large_test.md`（testplan/guitar 编排与用例结构规范）
  - `style/golang.md`（`style/large_test.md:53` 明文引用：大型测试代码必须遵守其单元测试规范——T012 Go 用例编写）
  - [Google Go Style](https://google.github.io/styleguide/go/)（入口索引）；其中 [Style Guide](https://google.github.io/styleguide/go/guide)（规范+权威，必读）与 [Style Decisions](https://google.github.io/styleguide/go/decisions)（规范）为 `style/golang.md:309-315` 引用的外部基准
- **官方文档**：无（fake-llm/testplan 为自研基建）
- **技术文章/技术参考文档**：
  - `specs/061-team-queue-steer/quickstart.md`（V1–V6 场景与断言面）
  - `specs/061-team-queue-steer/research.md`（R7——fake-llm 匹配面论证、R3a——062 交互与 V6 测试义务）
  - `specs/047-dsh-chat-demo/contracts/fake-llm-templates.md`（模板 schema、`keywords`/`history_keywords` 匹配语义——testdata 作者面契约）
  - `specs/062-team-game-end-handoff/spec.md` 与 `specs/062-team-game-end-handoff/quickstart.md`（先行落地的终局收束语义与夹具基线——终局链规则已成为"零执行"断言面；V6 的交互前提）
  - `projects/game/fake-llm/service/testdata/team_player.yaml`（既有 team player 模板样板——T011 就地扩展目标，经 062 注释更新后形态）
  - `projects/game/testplan/system_test.yaml`（suite 编排现状——新用例挂载点）
  - `projects/game/testplan/agent_v2_game_test.go`（既有 team 用例与 helper 复用面——T012 修改目标；按模块组织，`style/large_test.md`）
  - `.opencode/skills/testplan/SKILL.md`（testplan 执行流程——T012 运行前加载，`style/large_test.md` 末"FOR Agent"指向）

### Tasks

- [ ] T011 [P] `projects/game/fake-llm/service/testdata/team_player.yaml`（就地扩展模板组，不新建文件）：新增 mid-turn 场景模板——steer 探针模板（`keywords:[steer-probe]` → 回应正文，承载 V1 step 级分叉；**响应 MUST 为纯文本**〔无工具调用〕——V6 中延展步靠它收束）、收尾纯文本模板（V2 尾段场景）、落地引用模板（`keywords` 命中新消息 + `history_keywords` 命中落地消息 → V3 双引用断言）；既有多步工具循环模板复用为 step 1 产 tool call。与 062 夹具基线共存：终局链规则（`agent-v2-saolei-operate-won/lost` 等）保持 062 注释后的形态（零执行断言面），本任务不触碰；夹具关键词与既有 persona 锚行约定不冲突（对齐 `specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §3 匹配优先级）
- [ ] T012 依赖 T011：`projects/game/testplan/agent_v2_game_test.go`（team 用例所在 binary）新增 V1–V3 与 V6 用例 + `projects/game/testplan/system_test.yaml` 挂载（对齐既有 suite 结构）：V1——多步工具回合期间发送 steer-probe 消息，断言 `member_view{sender:"user"}` 帧序（消费时点）、fake-llm 下一 step 响应命中探针模板、ListMemberMessages 回填中输入位于两 step 输出之间、归并序列位置 = 发送时刻；V2——尾段到达 → 同成员自愈新回合消费 → 消化完成后才切换（事件序断言）；V3——消费一条 + 排队一条 → Cancel → 再次 Send，断言新回合 LLM 请求历史含落地+新消息（fake 响应双引用）、Cancel 与 Send 之间无成员回合；V6（062 交互）——player 终局 step 工具执行期间发送 steer 消息，断言 turn 延展一步（消息与终局工具结果同批进入、探针模板纯文本响应后收束）、延展步后无更多模型输出、player 延展 turn 的 turn_end 先于 planner 复盘 turn_start、复盘输入含终局工具单元。执行 `guitar run projects/game/testplan/system_test.yaml`（部署→测试→清理闭环）V1–V4 + V6 全量通过（V4 = 既有 team 用例零回归）

**Checkpoint / 验证门禁**: `guitar run` 全绿（全部用例 passed，无 failed/flaky——不满足则修复后重跑至全绿）；清理闭环完成。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (US1)**：无前置（无 setup/foundational）——立即开始
- **Phase 2 (US2)**：依赖 T001/T002（同一机制的验证）
- **Phase 3 (US3)**：T005 依赖 T001/T002（同文件 `orchestrator.ts`，串行）；T006 与 T005 并行（不同文件）
- **Phase 4 (US4)**：T007 与 T008 并行（不同文件）；T009 与 T007 同改 059 `spec.md`——串行（T007 → T009）；建议在 T005 后执行（T008 的契约更新描述终态机制含 landed flush）；T010 依赖 T001、T007–T009
- **Phase 5**：依赖 T001–T006（行为终态）与 T011（夹具）；T012 依赖 T011

### Parallel Opportunities

- T003 在 T001/T002 之后串行（T003 断言依赖 T001 的 snapshot 语义与 T002 的闭环；已去除 [P]）
- T005 ∥ T006（`orchestrator.ts` vs `store/chat.ts`）
- T007 ∥ T008（不同文件）；T009 与 T007 同改 059 `spec.md`——串行（T007 → T009）
- T011 可与 Phase 3/4 并行（纯夹具数据）

### MVP Scope

US1（+US2 验证）——mid-turn 进入语义独立成立；US3/US4/Phase 5 增量交付。

## Notes

- 同文件任务串行执行（`orchestrator.ts`：T001 → T002 → T005）；对同一文件的编辑避免并发（AGENTS.md）。
- 每个代码任务完成时执行对应 target 的 `bazel build` + `bazel test`（原则 IV，不单列）。
- 文档修正（T007–T009）只增补注记/收敛表述，不重写原文（原则 VII：终态表述；spec FR-006/007 明确）。
