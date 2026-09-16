# Tasks: agent-v2-team-refine

**Input**: Design documents from `/specs/065-agent-v2-team-refine/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: spec 的 Success Criteria 显式要求单测（SC-003/SC-004）与大型测试（SC-001/SC-002/SC-005）——测试任务按需求纳入。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g. US1, US2, US3)
- Include exact file paths in descriptions

## 通用约定（每个代码任务随行，不单列 task）

- 每次代码变更随行 `bazel build` + `bazel test`（相关 target，constitution 原则 IV）；新增/删除源文件后在对应目录执行 `bazel run //:gazelle <dir>` 更新 BUILD.bazel（AGENTS.md 流程）。
- 对同一文件的编辑串行进行（AGENTS.md）；代码注释遵守 constitution 原则 I/VII（引用带路径/URL、只表述终态）。
- AGENTS.md 与本 feature 的 spec.md 为必读，不在各 phase 文档清单中重复列出。

---

## Phase 1: Foundational — team 成员消息源接口（阻塞 US1/US4；US3 不受阻塞）

**Purpose**: team 插件的成员抽象泛化为消息源接口（依赖倒置），agent 成员经适配器接入（语义零变化），announce-only 能力位就绪。无独立 Setup 需求——既有服务/依赖零新增，直接进入基础层。

**文档清单**：

- 代码规范文档：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- 官方文档：[dsh-session README](https://unpkg.com/@deepseek-ai/dsh-session@0.1.1-rc.2/README.md)（SessionEvent 词汇表、`session.events` 快照语义——派生读源的输入形态）；[dsh-agent README](https://unpkg.com/@deepseek-ai/dsh-agent@0.1.1-rc.2/README.md)（`AgentHandle = {agent, dispose}` 与 agent 事件面——`agentMemberSource` 适配器映射依据；`common/js/dsh-plugins/team/src/team.ts` 头部既有引用）
- 技术文章/技术参考文档：`specs/065-agent-v2-team-refine/contracts/team-member-source.md`；`specs/065-agent-v2-team-refine/data-model.md` §1.1；`specs/065-agent-v2-team-refine/research.md`（R0/D1）；`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §1（team 基线契约）；`specs/060-agent-v2-team-optimize/contracts/team-api.md` §4（广播 wire 形态基线）

**Tasks**:

- [X] T001 在 `common/js/dsh-plugins/team/src/team.ts` 定义 `TeamMemberSource` 接口（`id`/`events`/`subscribe?`/`sectionTarget?`/`consumes?`）与 `agentMemberSource(handle)` 适配器；`TeamMemberRegistration.agent: AgentHandle` 泛化为 `source: TeamMemberSource`；注册（section 经 `sectionTarget`）、事件订阅（经 `subscribe`，适配器内过滤本成员）、`drain`（按 source id 解析）、relay/reconcile 改读 `source.events`；announce-only（`consumes: false`）不建 pending、不被 relay、`drain` throw fail-loud；在 `common/js/dsh-plugins/team/src/index.ts` re-export `TeamMemberSource` 与 `agentMemberSource`（包公共导出面——saolei-loop 经 `@dominion/dsh-team` 包根导入）
- [X] T002 在 `common/js/dsh-plugins/team/src/broadcast.ts` 将 sender 键语义放宽为"sender 成员 id"（`PendingUnit.senderSessionId` 与 `TeamBroadcastSource.senderSessionId` 类型放宽为 string，字段名保留；`deriveUnits`/`renderBroadcast`/`consumedAnchors`/`buildBroadcastMessage` 逻辑零改动）
- [X] T003 更新 team 插件单测：`common/js/dsh-plugins/team/src/team.test.ts` 与 `common/js/dsh-plugins/team/src/broadcast.test.ts`——既有用例改经 `agentMemberSource` 适配器（行为等价回归），新增 announce-only（不建 pending/不被 relay/drain throw/roster 含其行）与非 agent source 派生同权（`assistant/message` 形态事件 → 发言单元 → `<{role}-message>` 渲染 → 注入 → 消费闭包）断言（接口迁移不可分割的测试适配，随 T001/T002 交付——非独立测试执行任务，constitution 原则 IV）
- [X] T004 同步下游 team seam fake 至 source 形态：`common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts` 的 fake team（register 接受 source 成员、drain 按成员 id）与 `projects/game/agent_v2/src/session.test.ts` 的 team seam fake；`bazel test //common/js/dsh-plugins/team/... //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/...` 全绿（接口迁移不可分割的测试适配/回归收口，非独立测试执行任务，constitution 原则 IV）

**Checkpoint（验证门禁）**: team/saolei-loop/agent_v2 三包 build+test 全绿；team 插件无"系统广播"特设概念（仅接口泛化 + 能力位）。

---

## Phase 2: User Story 1 — 扫雷系统成员终局播报 (Priority: P1) 🎯 MVP

**Goal**: 运行时每局分项计数 + 扫雷系统成员（内存 log）+ orchestrator 终局交接触发（announce 先于 drain、`statsSentFor` guard）+ 宿主订阅落地（merge/team_message 帧）。

**Independent Test**: 单测链路——runtime 分项计数口径与 `gameStatsText` 模板；announcer log/source 能力位；orchestrator 交接播报（复盘输入集末位、同记录不重发）；宿主 `appendAnnouncement` 落 merge + 帧、订阅随物化建立/teardown 退订。

**文档清单**：

- 代码规范文档：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- 官方文档：[dsh-session README](https://unpkg.com/@deepseek-ai/dsh-session@0.1.1-rc.2/README.md)（announcer log 条目的 `assistant/message` 事件形态依据）；[dsh-llm message types](https://unpkg.com/@deepseek-ai/dsh-llm@0.1.1-rc.2/lib/types/message.d.ts)（`createAssistantMessage`/`MessageId`/`AssistantMessage.source` 构造形态）
- 技术文章/技术参考文档：`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md`（§1–§5，含交接流程图）；`specs/065-agent-v2-team-refine/contracts/team-member-source.md` §1/§2（`TeamMemberSource` 接口、announce-only 能力位与派生同权语义——T007 的 `SaoleiSystemMember` 是其非 agent 实现；game-stats-broadcast.md §3 明确转指该文件）；`specs/062-team-game-end-handoff/spec.md`（交接优先序零改动基线——game-stats-broadcast.md 依赖契约）；`specs/065-agent-v2-team-refine/data-model.md` §1.2–§1.4/§2/§3；`specs/065-agent-v2-team-refine/research.md`（D2/D3/D6/D9）

**Tasks**:

- [X] T005 [P] [US1] 在 `common/js/dsh-plugins/saolei-loop/src/game/board.ts` 为 `GameStats` 增 `operationsByType: Record<OperationType, number>` 并扩展 `computeGameStats` 签名接收该表；在 `common/js/dsh-plugins/saolei-loop/src/game/runtime.ts` 维护每局分项计数（`init` 清零、`executeOperation` kind==="ok" 时按 `op.type` 递增，分项和恒等 `operationCount`）；扩展 `common/js/dsh-plugins/saolei-loop/src/game/runtime.test.ts`（批量多操作、SKIP/STOP/派发失败不计、init 清零、随终局记录携带）
- [X] T006 [P] [US1] 在 `common/js/dsh-plugins/saolei-loop/src/game/text.ts` 新增纯函数 `gameStatsText(record)`（`data-model.md` §3 模板：结果行 + 总数/分项行）；新增 `common/js/dsh-plugins/saolei-loop/src/game/text.test.ts`（won/lost、计数字段、确定性）
- [X] T007 [P] [US1] 新增 `common/js/dsh-plugins/saolei-loop/src/announcer.ts`：`SaoleiSystemMember`（内存 log 追加 `assistant/message` 形态事件——`createAssistantMessage` 构造（入参传合成 provider/model，构造后 `source = {kind: "model", provider, model}`；`turn`/`step` 固定占位）、唯一 MessageId、单 text block、time/seq 单调；`announce(text)` 非空校验 fail-loud 并同步通知订阅者；source：`id = ${session}/saolei`、`consumes = false`、无 `sectionTarget`、实现 `subscribe`）+ `SAOLEI_MEMBER_SUMMARY` 常量；新增 `common/js/dsh-plugins/saolei-loop/src/announcer.test.ts`
- [X] T008 [US1] 在 `common/js/dsh-plugins/saolei-loop/src/index.ts` 导出 `gameStatsText`/`SaoleiSystemMember`/`SAOLEI_MEMBER_SUMMARY`，并运行 `bazel run //:gazelle common/js/dsh-plugins/saolei-loop` 更新新文件的 BUILD；在 `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`：物化注册第三成员（saolei source + role `"saolei"` + summary 常量，player/planner 经 `agentMemberSource`）；`nextStep()` 终局分支先 `announce(gameStatsText(event))` 再 `drain(planner)`，`statsSentFor` 记录恒等 guard（物化时重置）；新增 `announcer` 访问器（物化后有值）；扩展 `common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts`：交接播报居复盘输入集末位、同记录不重发、dispose 后不播报、init-only 局（无终局记录）不播报、存在未复盘终局记录时复盘保证启动
- [X] T009 [P] [US1] 在 `projects/game/agent_v2/src/history.ts` 新增 `TeamHistory.appendAnnouncement(role, text)`（`ROLE_AGENT` + 单 text block + `appendMerge(member=role)`，`appendMerge` member 参数放宽为 string）并扩展 `projects/game/agent_v2/src/history.test.ts`
- [X] T010 [US1] 在 `projects/game/agent_v2/src/session.ts` 的 `doMaterialize` 中经 `orchestrator.announcer` 订阅其产出事件并逐条调用 `history.appendAnnouncement`（teardown 随 entry 退订）；扩展 `projects/game/agent_v2/src/session.test.ts`（订阅建立/退订、merge 入列 + `team_message` 帧扇出）

**Checkpoint（验证门禁）**: saolei-loop + agent_v2 全部单测绿；US1 独立可测（fake team 注入 source 成员即可走通播报 → drain → 复盘输入链）。

---

## Phase 3: User Story 2 — 排队优先序不被播报干扰 (Priority: P1)

**Goal**: 以单测固化 FR-003 不变量：排队消化优先、跳局不补发、顺延不丢弃、重试不重发、取消恢复顺延。

**Independent Test**: orchestrator 单测五场景全绿（排队优先/跳局不补发/顺延不丢/重试不重发/取消恢复顺延；无需大型测试即可独立验证不变量）。

**文档清单**：

- 代码规范文档：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- 官方文档：无
- 技术文章/技术参考文档：`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §3（交接流程图：skip 分支与顺延语义）；`specs/062-team-game-end-handoff/spec.md`（排队消化优先序/交接分支零改动基线——回归断言依据）；`specs/065-agent-v2-team-refine/research.md` D2（跳局零特判推导）

**Tasks**:

- [X] T011 [US2] 扩展 `common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts` 排队优先序用例：①终局后队列非空 → player 先消化、不播报；②消化驱动 player 开新局且终局记录被覆盖 → 被跳过局无播报、新局交接恰播报新局；③消化后原局记录未被覆盖 → 原局照常播报（顺延）；④复盘驱动失败重试（pendingReview 持有输入集）不重播报；⑤终局后 cancel 暂停、再 send 恢复且原终局记录未被覆盖 → 统计随交接顺延播报（edge case：取消与交接竞态）；若断言暴露实现偏差，修复 `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 直至全绿

**Checkpoint（验证门禁）**: 五场景单测全绿；既有排队/取消/刷新用例回归通过。

---

## Phase 4: User Story 3 — 记忆快照近因注入 (Priority: P2)

**Goal**: JS 客户端捕获 `update_time` 归一化 epoch ms；快照渲染按更新时间倒排、截取最近 10 条、并列确定。

> 排序承担者已由 Phase 7 改为 memory 服务端（2026-09-16 用户裁定）：本 phase 交付的 `updateTime` 捕获与客户端排序/截取是 Phase 7 的改造对象，终态契约以 [contracts/memory-snapshot-recency.md](contracts/memory-snapshot-recency.md) 为准。

**Independent Test**: memory-service 单测（>10 条截断降序 / 不足全量 / 空不渲染 / 并列 memory_id 升序 / `memory_id` 不渲染 / load 冻结回归）。

**文档清单**：

- 代码规范文档：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- 官方文档：[@grpc/proto-loader README](https://github.com/grpc/grpc-node/blob/master/packages/proto-loader/README.md)（`longs: String` 选项下 Timestamp 的表示形态）
- 技术文章/技术参考文档：`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`；`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md` §2（快照冻结时机/写路径零改动基线——memory-snapshot-recency.md 基线契约）；`specs/065-agent-v2-team-refine/data-model.md` §1.5；`specs/065-agent-v2-team-refine/research.md` D4

**Tasks**:

- [X] T012 [P] [US3] 在 `common/js/dsh-plugins/memory-service/src/client.ts` 为 `MemoryEntry` 增 `updateTime?: number` 并在 `listMemories` 解析归一化（`{seconds: string, nanos: number}` → `Math.round(Number(seconds)*1000 + nanos/1e6)`，缺失/不可解析保持 undefined）；扩展 `common/js/dsh-plugins/memory-service/src/client.test.ts`
- [X] T013 [US3] 在 `common/js/dsh-plugins/memory-service/src/snapshot.ts` 的 `renderMemorySnapshot` 实现倒排 + 截取（`updateTime` 降序、undefined 最旧、并列 `memory_id` 升序、前 10 条、空集空串）；扩展 `common/js/dsh-plugins/memory-service/src/service.test.ts`（排序/截断/并列/冻结回归，`memory_id` 不渲染）

**Checkpoint（验证门禁）**: `bazel test //common/js/dsh-plugins/memory-service/...` 全绿；写路径与冻结时机零变化（既有用例回归）。

---

## Phase 5: User Story 4 — team 提示词澄清 (Priority: P2)

**Goal**: team section 补"仅输入侧"表述；roster 随成员注册自然含 saolei 行。

**Independent Test**: `renderTeamSection` 纯函数单测（三成员列表输入 → 含 saolei roster 行与"自身输出不使用广播标签"表述）。

**文档清单**：

- 代码规范文档：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- 官方文档：无
- 技术文章/技术参考文档：`specs/065-agent-v2-team-refine/contracts/team-member-source.md` §3–§4（§3 三成员注册与 roster 规范行；§4 终态措辞）；`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §1（team section/群聊原语基线——team-member-source.md 基线契约之一）；`specs/060-agent-v2-team-optimize/contracts/team-api.md` §4（措辞与 wire 形态同源约定）

**Tasks**:

- [X] T014 [US4] 在 `common/js/dsh-plugins/team/src/section.ts` 的 `renderTeamSection` 广播格式说明段末尾追加"仅输入侧"表述行（措辞按 `contracts/team-member-source.md` §4 终态文本）；扩展 `common/js/dsh-plugins/team/src/team.test.ts` 的 section 断言（新行存在；三成员 roster 渲染含 saolei 行）

**Checkpoint（验证门禁）**: team 单测绿；宿主 `projects/game/agent_v2/src/system-prompt.test.ts` 回归通过（system prompt 组装面零破坏）。

---

## Phase 6: 大型测试验收 & Polish

**Purpose**: 大型测试断言扩展与执行验收（constitution 原则 VI：实际执行 deploy→test→cleanup 闭环、全部用例通过）+ 文档增量。

**文档清单**：

- 代码规范文档：`style/large_test.md`；`style/golang.md`（§单元测试——大型测试必须遵守其命名/表驱动/given-when-then/禁止塞断言规范）；其引用基准 [Google Go Style](https://google.github.io/styleguide/go/)（入口索引）与 [Style Guide](https://google.github.io/styleguide/go/guide)（规范+权威，必读）
- 官方文档：无（guitar/testplan 经 SKILL 执行）
- 技术文章/技术参考文档：`specs/065-agent-v2-team-refine/quickstart.md`；`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §5（验收面）；`.opencode/skills/testplan/SKILL.md`（T019 的 guitar 执行流程与强约束）；`.opencode/skills/signoz/SKILL.md`（仅失败排障路径——logs/traces 查询）；`projects/game/testplan/system_test.yaml`（实际计划：suite/case 权威定义）；`tools/test/guitar/README.md`（guitar CLI 参考）；`projects/game/testplan/README.md`（suite-拓扑对照与夹具消费方式）；`projects/game/agent_v2/README.md`（服务目录 README——testplan SKILL「必须先读」项；T018 的编辑对象）；`projects/game/fake-llm/README.md`（keyword 匹配与场景模板机制）

**Tasks**:

- [X] T015 [P] 扩展 `projects/game/testplan/agent_v2_game_test.go`（游戏面模块）：多局 won/lost 链路断言——每局交接后归并序列恰一条 `member="saolei"` 统计消息（正文对照 `data-model.md` §3 模板；数值与该局 fake-desktop 实际成功派发序列一致，含一次批量多操作对照局）、planner 复盘输入含播报消息（fake-llm review 规则 keywords 命中模板关键行）、player 成员视图含 `user: [saolei]` 注入条目、`GetTeam.members`/`active_member` 不含 saolei、system prompt 含 roster saolei 行与"仅输入侧"表述、planner 快照 ≤10 条且倒序（>10 条记忆夹具会话对照）；共享构造/断言复用 `projects/game/testplan/agent_v2_helpers_test.go` 既有 helper（通用 HTTP/session helper 见 `helpers_test.go`）；如需新增共享 helper，统一落入 `agent_v2_helpers_test.go` 并由 T015 先行添加、T016 复用（同文件编辑串行）
- [X] T016 [P] 扩展 `projects/game/testplan/agent_v2_conversation_test.go`（对话面模块）：排队跳局场景——终局 player 回合收束时注入排队用户消息 → player 消化并开新局 → 被跳过局无统计消息、新局交接恰一条新局统计；对照用例（终局后无排队）统计即时播报
- [X] T017 fake-llm 夹具更新（依赖 T015/T016 完成）：若 T015/T016 断言无法命中既有场景模板/keyword 机制（机制见 `projects/game/fake-llm/README.md`），则更新 `projects/game/fake-llm/service/` 相应模板/规则；否则不改动。无论何种结论，均将"夹具是否改动"记入 T019 汇报；任何改动不破坏既有场景
- [X] T018 [P] 更新 `projects/game/agent_v2/README.md`：team 组合清单（成员消息源接口与扫雷系统 announce-only 成员）、广播与提示词分层（roster 含 saolei 行、"仅输入侧"表述）、planner memory（快照近因注入 ≤10 条）、大型测试断言面增量
- [X] T019 大型测试执行验收：经 testplan SKILL 执行 `guitar run projects/game/testplan/system_test.yaml`（完整部署→测试→清理闭环），全部用例通过；任何 failed/flaky 修复后重跑至全绿
- [X] T020 按 `specs/065-agent-v2-team-refine/quickstart.md` 走查验证：§2 单测命令全绿 + §3 大型测试断言项逐条对照 + §4 手动观察路径（可选）

**Checkpoint（验证门禁）**: testplan 全部用例通过（all cases passed）；quickstart 校验项逐条对照通过。

---

## Phase 7: 修改——memory List 服务端排序（2026-09-16 用户裁定）

> 排序实现已由 Phase 8 通用化（2026-09-16 第二次用户裁定）：本 phase 交付的 Go 三层 per-order 定制实现（`ListMemoriesOrder` 两值枚举、handler 两个字面量等值匹配、`listMemoriesByMemoryIDAsc` raw 游标与 `listMemoriesByUpdateTimeDesc` 定制复合游标两套分叉）是 Phase 8 的改造对象，T021–T028 勾选保留为历史（同 Phase 4 先例）；JS 面（T025/T026）与 testplan helper 的 `orderBy` 参数（T027 一部分）零改动延续；T029/T030 的验收执行并入 Phase 8（T035/T036）统一收口。终态契约以 [contracts/memory-snapshot-recency.md](contracts/memory-snapshot-recency.md) 为准。

**Purpose**: 快照的"按 `update_time` 取最近 10 条"改由 memory 服务 `ListMemories` 的排序能力承担（`order_by`，AIP-132）：客户端不再全量拉取后自排序，改为 `page_size=10 + order_by=update_time desc` 单页装载。缺省（不传 `order_by`）保持 `memory_id` 升序 + raw 游标——写路径与既有分页消费面零破坏。契约：`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`（本 phase 的接口权威）。前置：Phase 4 已交付的 JS 实现是本 phase 的改造对象（T012/T013 勾选保留为历史，不回改）。

**文档清单**：

- 代码规范文档：`style/api.md`；[AIP-132 Standard methods: List](https://google.aip.dev/132)（Ordering 节——`order_by` 形态、`desc` 后缀、空白不敏感）；[AIP-158 Pagination](https://google.aip.dev/158)（page token opacity、续页参数 "must match"）；[AIP-193 Errors](https://google.aip.dev/193)（INVALID_ARGUMENT 语义）；`style/golang.md`（含单元测试规范：表驱动/given-when-then/命名）；[Google Go Style 入口](https://google.github.io/styleguide/go/) 与 [Style Guide](https://google.github.io/styleguide/go/guide)；`style/mongo.md`（库表/对象定义）；`style/javascript.md`（ESM 书写规则 + vitest DI seam 测试约定）；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)；`style/large_test.md`（T027/T029 测试组织与反模式）
- 官方文档：[go.mongodb.org/mongo-driver/mongo/options（pkg.go.dev）](https://pkg.go.dev/go.mongodb.org/mongo-driver/mongo/options)（`FindOptions.SetSort`/`SetLimit` 与 `IndexModel` API 权威参考）
- 技术文章/技术参考文档：`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`（§1–§4 接口与验收面权威）；`specs/065-agent-v2-team-refine/data-model.md` §1.5；`specs/065-agent-v2-team-refine/research.md`（R0 session 先例条目、D4）；`specs/039-planner-memory-calibration/contracts/memory-service-contract.md` §2（ListMemories RPC 契约基线）；`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md` §2（快照冻结/写路径零改动基线）；session 服务复合游标先例（只读参考）——`projects/game/session/domain/pagination.go`、`projects/game/session/domain/pagination_test.go`、`projects/game/session/runtime/mongo/repository.go`（`NewSessionRepository` 建索引 + `List` 的 `$or` 游标过滤）、`projects/game/session/handler/handler.go`（`ListSessions` 的 token 解码与 INVALID_ARGUMENT 映射）；T029 执行参考——`.opencode/skills/testplan/SKILL.md`、`tools/test/guitar/README.md`、`projects/game/testplan/README.md`、`projects/game/testplan/system_test.yaml`

**Tasks**:

- [X] T021 [P] [US3] 在 `projects/game/game.proto` 的 `ListMemoriesRequest` 增 `string order_by = 4`，注释按契约 §1：AIP-132 引用、支持值（空 = 缺省 `memory_id` 升序；`update_time desc`，空白不敏感）、`update_time` 并列以 `memory_id` 升序打破（确定全序）、非法值 INVALID_ARGUMENT、page_token 顺序作用域（续页参数须一致，两模式 token 形态不互通）；`bazel build //projects/game` 确认 `game_go_proto` 再生成（生成物由规则产出、不入库）
- [X] T022 [P] [US3] 在 `projects/game/memory/domain/model.go` 增 `ListMemoriesOrder` 类型与两常量（`ListMemoriesOrderMemoryIDAsc` 零值 = 缺省序、`ListMemoriesOrderUpdateTimeDesc`）；新增 `projects/game/memory/domain/pagination.go`：`MemoryPageCursor{UpdateTime time.Time, MemoryID string}` + `EncodeMemoryPageToken`/`DecodeMemoryPageToken`（base64url NoPadding JSON、`update_time` UTC RFC3339Nano；空 token/坏 base64/坏 JSON/缺字段/坏时间 → error——形态镜像 `projects/game/session/domain/pagination.go`）；`errors.go` 增 `ErrInvalidPageToken`；新增 `projects/game/memory/domain/pagination_test.go`（表驱动 round-trip 与坏 token 各形态，镜像 session 的 `pagination_test.go`）；`bazel run //:gazelle projects/game/memory` 更新 BUILD（本 task 不改仓储接口，独立可编译）
- [X] T023 [US3] 在 `projects/game/memory/domain/repository.go` 的 `ListMemories` 签名增 `order ListMemoriesOrder`；`projects/game/memory/runtime/mongo/repository.go`：ordered 分支（`pageToken != ""` 时经 `DecodeMemoryPageToken` 解码、失败返回 `domain.ErrInvalidPageToken`；过滤 `$or: [{update_time: {$lt: T}}, {update_time: T, memory_id: {$gt: M}}]`；`SetSort(bson.D{{update_time, -1}, {memory_id, 1}})`；limit+1；`next_page_token` 由页末条目经 `EncodeMemoryPageToken` 编码）；`NewRepository` 增建非唯一复合索引 `{template: 1, session_id: 1, update_time: -1, memory_id: 1}`（镜像 session 仓储启动建索引）；缺省分支行为原样；`projects/game/memory/handler/handler.go` 的调用点暂传缺省序、`handler_test.go` 的 fake 签名机械适配（编译闭环随行，原则 IV）；扩展 `repository_test.go`：fake `Find` 支持有序过滤（`$or` 求值）与双键排序，新增 ordered 用例（`update_time` 降序、同毫秒并列 `memory_id` 升序、limit+1 续页跨页全量一次、坏 token → `ErrInvalidPageToken`）与缺省模式回归
- [X] T024 [US3] 在 `projects/game/memory/handler/handler.go` 的 `ListMemories` 增 `order_by` 解析：空白切分归一（`strings.Fields` 连接）后与 `update_time desc` 等值比较 → ordered 序，空串 → 缺省序，其他值 → `INVALID_ARGUMENT`（错误信息列受支持值）；`toStatusError` 增 `domain.ErrInvalidPageToken → INVALID_ARGUMENT`；扩展 `handler_test.go`：合法/非法 `order_by` 表驱动（透传 order、未知字段/升序/多字段 → InvalidArgument）、仓储 `ErrInvalidPageToken` 映射
- [X] T025 [P] [US3] 在 `common/js/dsh-plugins/memory-service/src/client.ts`：`MemoryStore.listMemories` 增可选参数 `options?: { orderBy?: string; pageSize?: number }`；`MemoryClient.listMemories` 实现——`pageSize` 给定时单页即止（一次请求即返回，不续翻），请求 wire 携带 `pageSize`/`orderBy`（undefined 字段不发送）；`MemoryEntry` 收缩为 `{memory_id, content}`（删除 `updateTime` 字段、`normalizeUpdateTime`、`ListedMemory.updateTime`——排序知识收敛服务端）；扩展 `client.test.ts`：单页语义（给定 pageSize 时不续翻 + 请求参数断言）、全页累积回归、删除 Timestamp 归一化用例
- [X] T026 [US3] 在 `common/js/dsh-plugins/memory-service/src/snapshot.ts`：`renderMemorySnapshot` 改纯透传渲染（删除 `byRecency` 排序与截取；保留 `长期记忆：` 头、逐条一行、空集空串、`memory_id` 不渲染语义），新增导出 `SNAPSHOT_ORDER_BY = "update_time desc"`、保留并导出 `SNAPSHOT_ENTRY_LIMIT = 10`（注入策略常量单点所有）；在 `service.ts` 的 `load` 改为 `client.listMemories(scope.template, scope.session, {orderBy: SNAPSHOT_ORDER_BY, pageSize: SNAPSHOT_ENTRY_LIMIT})`；更新 `service.test.ts`："load (snapshot recency)" 块改为断言装载调用形态（`toHaveBeenCalledWith` 含 options）与快照按返回序透传渲染（截断由装载查询的 `pageSize` 承担、渲染不做二次截断——store 返回面即快照内容），写路径/fail-loud/冻结回归保持
- [X] T027 [US3] 在 `projects/game/testplan/helpers_test.go` 的 `listMemories` helper 增 `orderBy string` 参数（空串 = 不附带查询参数；`memory_test.go` 3 个既有调用点机械适配）；扩展 `projects/game/testplan/memory_test.go`：经网关 `?order_by=update_time%20desc` 的有序断言——PATCH 更新某条目后该条目浮至首位（`update_time` 降序）、同毫秒并列组内 `memory_id` 升序、`page_size=2` 复合游标续页全量一次且序保持、非法 `order_by`（如 `foo` 与 `update_time`）→ 400 INVALID_ARGUMENT；既有缺省模式分页断言（`memory_id` 升序）回归
- [X] T028 [P] [US3] 更新 `projects/game/agent_v2/README.md` 的 planner memory 行：快照近因注入表述为"memory 服务 `ListMemories.order_by` 服务端排序、`page_size=10` 单页装载最近 10 条"（终态描述，替换客户端排序表述），大型测试断言面同步
- [ ] T029 大型测试执行验收：经 testplan SKILL 执行 `guitar run projects/game/testplan/system_test.yaml`（完整部署→测试→清理闭环），全部用例通过；重点核对 `TestMemoryServiceHttpCrudAndPagination`（新增 ordered 断言 + 缺省回归）与 `TestAgentV2TeamGameStatsPromptFaces`（12 条夹具快照断言零改动通过——服务端排序结果与断言一致）；任何 failed/flaky 修复后重跑至全绿
- [ ] T030 按 `specs/065-agent-v2-team-refine/quickstart.md` 走查验证：§2 单测命令全绿（含 `//projects/game/memory/...`）+ §3 大型测试断言项 1–5 逐条对照 + §4 手动观察路径（可选）

**Checkpoint（验证门禁）**: `bazel test //projects/game/memory/... //common/js/dsh-plugins/memory-service/...` 全绿；缺省模式零破坏（既有 handler/仓储/testplan 断言回归通过）；testplan 实际执行全绿（all cases passed）；quickstart 校验项逐条对照通过。

---

## Phase 8: 修改——通用 order_by 映射与通用游标（2026-09-16 第二次用户裁定）

> 通用机制的 Go 实现已由 Phase 9 按 Go 习惯与分层重铸（2026-09-16 第三、四、五次用户裁定）：本 phase 交付的 Go 三层实现（`MemoryPageCursor`/`memoryCursorJSON` 双结构体游标 + `*string` 字段与转换层、`[]MemorySortTerm`/`[]MemorySortFieldSpec` 值语义容器、仓储内白名单/键匹配校验、通用 Unique 扫描收尾、`memorySeekFilter` 三平行数组前序条件）是 Phase 9 的改造对象，T031–T034 勾选保留为历史（同 Phase 7 先例）；testplan 断言面（T033）与 README（T034）内容行为不变、零改动延续；T035/T036 的验收执行并入 Phase 9（T040/T041）统一收口。终态契约以 [contracts/memory-snapshot-recency.md](contracts/memory-snapshot-recency.md) 为准。

**Purpose**: ListMemories 排序从 per-order 定制实现改为**通用机制**：`order_by` 按 AIP-132 `{field} [desc]` 通用语法解析 + **API 字段 → Mongo 字段白名单映射表**（单一事实源）+ **唯一键收尾 tie-breaker**（排序键未以唯一字段收尾自动追加 `memory_id asc`；缺省经同一规则推导）+ **通用游标**（token 编码页末条目全部排序键值，不编码方向；键序列与请求排序键不匹配拒绝）+ **单路径仓储**（sort spec/键匹配/OR 阶梯/limit+1/编码一套流程，无 per-order 方法）。验收标准之一："新增一个可排序字段 = 白名单一行 + accessor 一行 + 一个复合索引"。JS 快照消费面零改动（`update_time desc` 字符串 + `page_size=10` 单页不变）；FR-007 语义零变化。契约（本 phase 接口权威）：`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`（已按通用机制终态重写）。前置：Phase 7 已交付的 Go 三层实现是本 phase 的改造对象。

**文档清单**：

- 代码规范文档：`style/api.md`（AIP 索引）；[AIP-132 Standard methods: List](https://google.aip.dev/132)（Ordering 节——逗号分隔字段列表、`desc` 后缀、空白不敏感、升序省略后缀）；[AIP-158 Pagination](https://google.aip.dev/158)（token opacity、"all other parameters must match"、base64 混淆惯例）；[AIP-193 Errors](https://google.aip.dev/193)（INVALID_ARGUMENT 语义与错误信息要求）；`style/golang.md`（含单元测试规范：表驱动/given-when-then/命名）；[Google Go Style 入口](https://google.github.io/styleguide/go/) 与 [Style Guide](https://google.github.io/styleguide/go/guide)；`style/mongo.md`（库表/对象定义——`_id` 不覆盖、具体模型不用 bson.M）；`style/large_test.md`（T033/T035/T036 测试组织与反模式）
- 官方文档：[go.mongodb.org/mongo-driver/mongo/options（pkg.go.dev）](https://pkg.go.dev/go.mongodb.org/mongo-driver/mongo/options)（`FindOptions.SetSort`/`SetLimit` 与 `IndexModel` API 权威参考）；[MongoDB: Use Indexes to Sort Query Results](https://www.mongodb.com/docs/v7.0/tutorial/sort-results-with-indexes/)（复合索引方向匹配（同序/逆序）、sort 键序 = 索引键序、`$or` 各支可分别走索引——索引义务论证）；[MongoDB ESR Guideline](https://www.mongodb.com/docs/manual/tutorial/equality-sort-range-guideline/)（等值前缀在前、排序键随后的索引布局）
- 技术文章/技术参考文档：`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`（§1–§4 通用机制接口与验收面权威）；`specs/065-agent-v2-team-refine/data-model.md` §1.5（实体形态）；`specs/065-agent-v2-team-refine/research.md`（R0 排序通用化社区调研、D4 通用机制决策与被否决反模式）；`specs/039-planner-memory-calibration/contracts/memory-service-contract.md` §2（ListMemories RPC 契约基线）；`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md` §2（快照冻结/写路径零改动基线）；session 服务单排序先例（只读参考，不追溯改造）——`projects/game/session/domain/pagination.go`、`projects/game/session/runtime/mongo/repository.go`（`List` 的 `$or` 游标过滤与启动建索引）；[use-the-index-luke: Paging Through Results（seek method）](https://use-the-index-luke.com/sql/partial-results/fetch-next-page)（确定排序/唯一列收尾/OR 阶梯展开/方向翻转比较）；[mongo-keyset-pagination（GitHub）](https://github.com/Guy-Meridor/mongo-keyset-pagination)（"sort must define a total order, end it with a unique field"、索引对齐）；[Cursor-based Pagination on Multiple Fields with MongoDB（Brian Pfretzschner）](https://brianp.de/posts/2024/mongodb-cursor-pagination-multiple-fields/)（Mongo 复合游标 OR 阶梯标准形与索引注意）；T035 执行参考——`.opencode/skills/testplan/SKILL.md`、`tools/test/guitar/README.md`、`projects/game/testplan/README.md`、`projects/game/testplan/system_test.yaml`

**Tasks**:

- [X] T031 [P] [US3] 在 `projects/game/game.proto` 将 `ListMemoriesRequest.order_by` 注释重写为通用机制终态（AIP-132 `{field} [desc]` 逗号分隔语法、空白不敏感、白名单 `memory_id`/`update_time`、唯一键收尾——`memory_id` 仅可处于末位、缺省经收尾规则推导为 `memory_id` 升序、`next_page_token` 编码最终排序键值序列且键不匹配拒绝、非法值 INVALID_ARGUMENT）；`bazel build //projects/game` 确认 `game_go_proto` 再生成（生成物由规则产出、不入库）。新增 `projects/game/memory/domain/sort.go`：`MemorySortTerm{Field string, Descending bool}`、`MemorySortValueKind`（string/time）、`MemorySortFieldSpec{Field, MongoField string; Kind MemorySortValueKind; Unique bool}`、白名单映射表（有序、单一事实源：memory_id 唯一 string / update_time 非唯一 time）与 `MemorySortFieldSpecs()`、`ParseMemoryOrderBy(orderBy string) ([]MemorySortTerm, error)`——语法解析（逗号切分、项内空白归一、后缀恰为小写 `desc` 否则非法）、白名单校验（未知字段/子字段路径拒绝）、重复字段拒绝、唯一键收尾（未以 `Unique` 字段收尾追加 `memory_id asc`；追加冲突即 `memory_id` 非末位拒绝）、错误信息列受支持字段与语法；新增 `projects/game/memory/domain/sort_test.go`（表驱动覆盖契约 §4 排序解析面全清单）；`bazel run //:gazelle projects/game/memory` 更新新文件 BUILD（本 task 不改既有签名，独立可编译）
- [X] T032 [US3] Go 服务链单路径重造（编译闭环原子变更——domain 签名变更牵连 mongo/handler，不可按层拆分）：① domain：`projects/game/memory/domain/model.go` 删除 `ListMemoriesOrder` 类型与常量；`repository.go` 的 `ListMemories` 签名 `order ListMemoriesOrder` → `sort []MemorySortTerm`（注释改单路径语义）；`pagination.go` 重写为通用 codec——`MemoryPageCursor{Fields []string, Values []any}`（string/time.Time）、`EncodeMemoryPageToken`/`DecodeMemoryPageToken`（base64url NoPadding JSON 数组 `[{field, value}]`、时间 UTC RFC3339Nano、按白名单 Kind 解码与校验值类型、未知 field/坏形态报错）；重写 `pagination_test.go`（多键 round-trip 含纳秒精度 + 坏 token 表驱动）。② runtime/mongo：`repository.go` 删除 `listMemoriesByMemoryIDAsc`/`listMemoriesByUpdateTimeDesc`，`ListMemories` 单路径——sort spec（白名单 MongoField × 方向）、`pageToken` 解码 + 字段序列与最终排序键匹配校验（不匹配 wrap `ErrInvalidPageToken`，即 AIP-158 跨序重放拒绝）、通用 OR 阶梯过滤（前缀相等 + 当前键 `$gt` 升/`$lt` 降）、limit+1、页满按最终键从页末条目提取键值编码 next token；`model.go` 增 `memoryDocument` 的 `sortValue(field string)` accessor（memory_id/update_time）；启动建索引不变；重写 `repository_test.go`——fake `Find` 的 `$or` 求值通用化（任意前缀相等 + 方向感知比较）与排序通用化（按 sort spec 任意键序），用例按契约 §4 仓储面（缺省 `[memory_id asc]` 回归含翻页全量一次、`update_time desc` 降序 + 同毫秒并列 `memory_id` 升序 + tie 组跨页边界全序保持、`memory_id desc` 方向翻转、显式 `update_time desc, memory_id` 等价对照、跨序重放与键数不匹配拒绝、坏 token 拒绝）。③ handler：`projects/game/memory/handler/handler.go` 删除 `listMemoriesOrderValues`/`parseListMemoriesOrder`，`ListMemories` 改调 `domain.ParseMemoryOrderBy`（错误 → INVALID_ARGUMENT），`ErrInvalidPageToken` 映射保留；重写 `handler_test.go` order_by 用例（fake repo 记录最终排序键并 deep-equal 断言；合法/非法表驱动按契约 §4）。随行 `bazel build` + `bazel test //projects/game/memory/...` 全绿（原则 IV）；`bazel run //:gazelle projects/game/memory`
- [X] T033 [P] [US3] 扩展 `projects/game/testplan/memory_test.go` 的 `TestMemoryServiceHttpListOrderBy`（依赖 T032 服务行为）：语义翻转修正——`update_time`（裸字段升序）与 `update_time desc, memory_id`（多字段显式 tie-breaker）从非法 400 断言改为合法正路径断言（各自序正确，且与 `update_time desc` 的单页序一致对照）；非法 `order_by` 集合改为 `foo`（未知字段）、`update_time asc`（非法后缀）、`content`（未开放字段）→ 400 INVALID_ARGUMENT；PATCH 浮首/并列序/`page_size=2` 复合游标续页/缺省模式分页回归断言保持；`helpers_test.go` 无改动
- [X] T034 [P] [US3] 更新 `projects/game/agent_v2/README.md` 的 planner memory 行：order_by 表述通用化（AIP-132 `{field} [desc]` 语法、白名单 `memory_id`/`update_time`、唯一键收尾 tie-breaker、通用游标），快照装载 `update_time desc` + `page_size=10` 单页表述不变，大型测试断言面同步
- [ ] T035 大型测试执行验收（依赖 T032/T033/T034）：经 testplan SKILL 执行 `guitar run projects/game/testplan/system_test.yaml`（完整部署→测试→清理闭环），全部用例通过；重点核对 `TestMemoryServiceHttpListOrderBy`（通用语法正路径 + 新非法集合 + 缺省回归）、`TestMemoryServiceHttpCrudAndPagination`（缺省分页回归——token 形态变化不影响客户端透传）、`TestAgentV2TeamGameStatsPromptFaces`（12 条夹具快照断言零改动通过——服务端排序结果与断言一致）；任何 failed/flaky 修复后重跑至全绿
- [ ] T036 按 `specs/065-agent-v2-team-refine/quickstart.md` 走查验证：§2 单测命令全绿（含 `//projects/game/memory/...` 与 `//common/js/dsh-plugins/memory-service/...`——JS 面零改动回归）+ §3 大型测试断言项 1–5 逐条对照 + §4 手动观察路径（可选）

**Checkpoint（验证门禁）**: `bazel test //projects/game/memory/... //common/js/dsh-plugins/memory-service/...` 全绿；缺省模式零破坏（排序结果与翻页语义同前）；`memory` 三层无 per-order 方法/字面量匹配/分叉游标（机制通用性可由 sort.go 白名单 + 单路径 ListMemories 验证）；"新增一个可排序字段 = 白名单一行 + accessor 一行 + 一个复合索引"成立；testplan 实际执行全绿（all cases passed）；quickstart 校验项逐条对照通过。

---

## Phase 9: 修改——Go 习惯与分层重铸（2026-09-16 第三、四、五次用户裁定）

> 第五次用户裁定（2026-09-16）针对本 phase 未提交的实现提出五点修正：①`ParseMemoryOrderBy` 填补逻辑缺陷（中间位 `memory_id` 重复追加）与两处 Go 习惯问题（if 包裹主逻辑、`seen`+`validated` 双中间结构）；②cursor 未按指针规范传 `*domain.MemoryPageCursor`；③`CursorValue` 被逐子句重复调用（Θ(n²) 次转换）；④`memorySeekClauses` 每子句从零重建前序条件（应为增量派生）；⑤值转换分层不对称（token→typed 在 domain 方法、doc→wire 格式化在 repo——成套逻辑 split 两层）。Phase 8/9 代码均未提交、无历史包袱：T037–T039 按第五轮终态重写并取消勾选（重做后由 developer 勾回），T040/T041 验收收口不变。终态契约以 [contracts/memory-snapshot-recency.md](contracts/memory-snapshot-recency.md) 为准（§1 item 9 为对象关系与值转换责任 classDiagram）。

**Purpose**: Phase 8 通用机制的 Go 实现**以 Go 语言习惯与仓库指针规范重铸**，同时完成分层重排、查询构造通用化与游标类型演进化（`order_by` 对外语义面唯一变化：曾因"`memory_id` 非末位"被拒绝的键列表转为合法；排序结果、翻页语义、JS/testplan 消费面不变；token 对外 opaque、全消费面只透传，内部 JSON 形态变化零破坏）：①**struct 包装游标 + 指针签名**——`MemoryPageCursor struct{Entries []*MemoryCursorEntry \`json:"entries"\`}`、`MemoryCursorEntry{Field, Value string}`（json tag 直接标注）即 wire 形态（JSON 对象 `{"entries":[...]}`），删除 `memoryCursorJSON` 中间结构体与 `*string` 字段及 encode/decode 转换层；跨函数边界一律 `*MemoryPageCursor`（codec 与仓储签名，`style/golang.md` 函数参数与返回值优先指针），**nil = 首页唯一判据**（handler 空 token 传 nil、仓储 `cursor != nil` 才 seek）；②**指针规范对齐 `style/golang.md`**——结构体容器一律 `[]*T`、查找返回 `*T`、`ParseMemoryOrderBy` 返回 `[]*MemorySortTerm`；③**分层重排**——排序参数全部校验（语法/白名单/重复/游标键匹配/时间可解析+转换）收敛 domain 层（`ParseMemoryOrderBy` + `DecodeMemoryPageToken(token, sort)`），仓储收到已验证输入只做直译（不回头调用 domain 查表校验），handler 只做字符串透传与错误映射；④**seek 条件通用 OR 阶梯（无长度分支，O(n) 增量构造）**——`memorySeekClauses(sort, cursor)` 对任意键数以单一循环统一生成，**等值前缀逐键演进**（第 i 子句由第 i-1 子句派生：尾项退化为等值 + 追加新比较项——不从零重建），统一赋 `filter["$or"]`（单键 = 一个子句的 `$or`，语义等价于裸比较——不做"并入 filter"特例），删除 `memorySeekFilter(specs, sort, values)` 三平行数组前序累积；⑤**删除 `Unique` 标志**——指定 tie-breaker 字段（`memory_id`）直判收尾，`appendMemorySortTieBreaker` 通用扫描式收尾删除；`MemorySortTerm` 自包含 `MongoField`/kind 使派生 specs 数组消失；⑥**解析终态（第五轮）**——空白 `order_by` 早返回固定默认值 `[]*MemorySortTerm{memory_id asc}`（主流程保持顶层）；两趟处理（"先校验后产出"的字面形态——第一趟纯校验+入 map、无外部可见产出）：校验趟逐项校验（语法/白名单/重复）并将构造的 `spec.term(descending)` 存入 `validated := make(map[string]*MemorySortTerm)`——**map 为唯一已验证存储**（field → term 本体；任一项失败即 `(nil, error)` 无部分产出——map 为局部状态不泄漏），排序趟**再过一遍 items** 按请求原序从 map 取 term 组装切片（map 迭代序随机，原序由重放输入恢复——`items` 是输入产物非派生结构，无平行数组回归）；**填补判据 = 请求排序字段完全不含 `memory_id`**（map 存在性查找；任意位置含即不追加——唯一键参与任意键位已全序），删除"`memory_id` 非末位"拒绝；⑦**值转换单次化 + 责任收敛（第五轮）**——`MemoryCursorEntry` 携包内私有 `typed any`（json 不可见）+ `Typed() any`；decode 经 `CursorValue` 每 entry 一次完成校验与转换；`MemorySortTerm.CursorEntry(value any) *MemoryCursorEntry` 为对称的 typed→entry 构造器（时间 UTC RFC3339Nano 格式化唯一所在）；仓储零转换（`sortValue(field) any` 返回 typed、seek 读 `Typed()` 原样入 bson、next-token 经 `CursorEntry` 构造）；⑧**对象关系图**——契约 §1 item 9 Mermaid classDiagram + 值转换责任表（各对象创建/持有/调用方向、nil 语义）。契约（本 phase 接口权威）：`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`（§1/§4 已按第五轮终态重写）。前置：Phase 8 已交付的 Go 三层实现是本 phase 的改造对象。

**文档清单**：

- 代码规范文档：`style/golang.md`（指针节——容器元素 `[]*T`/函数签名优先指针/元组语义例外、函数节——已校验入参不重复校验、数组节——空数组返回 nil/初始化不加长度参数、注释/单元测试各节）；[Google Go Style Guide](https://google.github.io/styleguide/go/guide)（Clarity/Simplicity/Least mechanism——"unnecessary levels of abstraction"、"complex when its purpose should be simple 是重访实现的信号"）；[Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)（Pass values——`*string` 反例、Nil slices、In-band errors、Useful test failures）；[Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)（In-Band Errors、Pass Values、Receiver Type、Receiver Names）；[Effective Go](https://go.dev/doc/effective_go)（Introduction——"C++/Java 程序直译成 Go 难以满意"；Multiple return values——出参模拟的反面锚点；Composite literals；[If 节](https://go.dev/doc/effective_go#if)——早返回/成功路径沿页面下行（第五轮：空白 `order_by` 早返回的锚点）；[Maps 节](https://go.dev/doc/effective_go#maps)——comma-ok 存在性测试（第五轮：`validated map[string]*MemorySortTerm` 的重复检查/填补判据锚点））；[Go maps in action（Go blog）](https://go.dev/blog/maps)（two-value 存在性测试与 "Iteration order ... is not specified"——第五轮 map 习惯用法 + 排序趟再过 items 保序的官方博客锚点）；`style/mongo.md`（`_id` 不覆盖；具体模型不用 `bson.M`——存储文档模型约束，查询过滤动态构造沿用 session 先例 `bson.M` 形态）
- 官方文档：[go.mongodb.org/mongo-driver/mongo/options（pkg.go.dev）](https://pkg.go.dev/go.mongodb.org/mongo-driver/mongo/options)（`FindOptions.SetSort`/`SetLimit` 与 `IndexModel` API——仓储查询构造沿用，零新增 API 面）；[AIP-132 Standard methods: List](https://google.aip.dev/132)（Ordering 语义——本轮零变化的行为基准）；[AIP-158 Pagination](https://google.aip.dev/158)（token opacity/"must match"——同上）；[AIP-193 Errors](https://google.aip.dev/193)（INVALID_ARGUMENT 语义——同上）
- 技术文章/技术参考文档：`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`（§1–§4 终态接口与验收面权威——含 §1 item 9 classDiagram 与值转换责任表）；`specs/065-agent-v2-team-refine/research.md`（R0 Go 语言习惯调研条目（含第五轮 map/早返回/指针 nil 补充）+ D4 五轮裁定与被否决形态（含第五轮五点否决））；`specs/065-agent-v2-team-refine/data-model.md` §1.5（终态实体形态）；session 服务单排序先例（只读参考——nil-cursor 首页（`cursor *domain.ListPageCursor` 指针先例）与 limit+1 形态；其字面两子句 `$or` 是通用阶梯在固定双键服务中的手写特例，只看语义不照抄形态）——`projects/game/session/domain/pagination.go`、`projects/game/session/runtime/mongo/repository.go`（`List` 的续页过滤）；`specs/039-planner-memory-calibration/contracts/memory-service-contract.md` §2（ListMemories RPC 契约基线）；T040 执行参考——`.opencode/skills/testplan/SKILL.md`、`tools/test/guitar/README.md`、`projects/game/testplan/README.md`、`projects/game/testplan/system_test.yaml`

**Tasks**:

- [X] T037 [P] [US3] 在 `projects/game/game.proto` 将 `ListMemoriesRequest.order_by` 注释的 tie-breaker 段修订为第五轮终态：'a key list that does not end in "memory_id" (the designated unique tie-breaker) gets "memory_id asc" appended, so "memory_id" may only appear as the final term' → 'a key list that contains no "memory_id" (the designated unique tie-breaker) gets "memory_id asc" appended — a key list that already includes "memory_id" (at any position) is a total order because "memory_id" is unique, so nothing is appended'，并删除非法值清单中的 'non-final "memory_id"'；其余注释（AIP-132 语法、白名单、token 键匹配、INVALID_ARGUMENT）不变；`bazel build //projects/game` 确认 `game_go_proto` 再生成（注释级变更，wire 零变化）
- [X] T038 [US3] Go 三层按 Go 习惯与分层重铸（编译闭环原子变更——`MemorySortTerm`/`MemoryPageCursor`/仓储签名变更牵连 domain/mongo/handler 与全部测试，不可按层拆分；`order_by` 对外语义面唯一变化：曾因"`memory_id` 非末位"被拒绝的键列表转为合法；排序结果与翻页语义不变，token 对外 opaque、全消费面只透传——内部 JSON 形态变化零破坏）：① domain/sort.go：保留导出常量 `MemorySortFieldMemoryID`/`MemorySortFieldUpdateTime`；`MemorySortValueKind` 及其常量、`MemorySortFieldSpec`（行结构收缩为 `{Field, MongoField string; kind}`，**删除 `Unique` 标志**）、白名单表、查找函数全部改为包内私有（`memorySortValueKind`/`memorySortValueString`/`memorySortValueTime`/`memorySortFieldSpec`/`memorySortFieldSpecs []*memorySortFieldSpec`/`memorySortFieldSpecByField(field string) (*memorySortFieldSpec, bool)`——指针返回；kind 常量以 `memorySortValueString` 为零值——iota 首位，测试手构 term（零 kind）即 string 语义）；`MemorySortTerm` 重铸为自包含键元素 `{Field, MongoField string; Descending bool}` + 包内私有 `kind` 字段 + **一对对称转换方法**：`CursorValue(value string) (any, error)`（wire → typed：string kind 原样返回、time kind `time.Parse(time.RFC3339Nano)`）与 `CursorEntry(value any) *MemoryCursorEntry`（typed → entry：string kind 原样、time kind `value.(time.Time).UTC().Format(time.RFC3339Nano)` 格式化——**typed→wire 的唯一所在**；kind 与 value 动态类型不匹配 panic fail-fast——编程期不变量违约，同 `memoryDocument.sortValue` 未知字段先例，无 error 路径）；`ParseMemoryOrderBy(orderBy string) ([]*MemorySortTerm, error)` 终态：**空白（空串/纯空白）早返回** `[]*MemorySortTerm{memorySortTieBreaker.term(false)}`（每次调用新构造字面量，不共享包级单例——返回切片可被调用方持有），主流程保持在顶层；非空输入**两趟**处理（"先校验后产出"的字面形态——第一趟纯校验+入 map、无外部可见产出，语义保持）：**第一趟（校验趟）**逐项校验（语法 → 白名单 → 重复）并将构造的 `spec.term(len(fields) == 2)` 存入 `validated := make(map[string]*MemorySortTerm)`——map 是**唯一已验证存储**（field → `*MemorySortTerm` 本体直接存放，无 spec→term 二次构造），任一项失败即 `(nil, error)`（无部分产出——map 为函数内局部状态，不随错误泄漏）；**第二趟（排序趟）再过一遍 items**——map 迭代序随机，请求原序由重放输入恢复（`items` 是 `strings.Split` 的输入产物而非派生结构——与第三轮否决的跨函数签名随行派生 specs 平行数组性质不同，无平行数组回归）：逐项 `terms = append(terms, validated[strings.Fields(item)[0]])`（第一趟已保证在 map 中，直接取用）；`if _, ok := validated[memorySortTieBreaker.Field]; !ok` 才 append 填补 tie-breaker term（不在 items 中，置于输出末位）；**删除 `memory_id` 非末位拒绝**、`seen` map、按序旁路 terms 维护与 `memoryOrderByTerm` 中间结构及 `MemorySortFieldSpecs`/`MemorySortFieldSpecByField`/`appendMemorySortTieBreaker`（骨架伪代码见契约 §1 item 5）。② domain/pagination.go：`MemoryCursorEntry{Field string \`json:"field"\`, Value string \`json:"value"\`}` + 包内私有 `typed any` 字段（`encoding/json` 对未导出字段不可见——wire 形态仍为 `{field, value}` 全 string）+ 导出方法 `Typed() any`（返回 decode/构造时一次转换的 typed 值）；`type MemoryPageCursor struct { Entries []*MemoryCursorEntry \`json:"entries"\` }`（struct 包装的单一 wire 形态；未来向 token 加参数 = struct 加字段，签名不变）；`EncodeMemoryPageToken(cursor *MemoryPageCursor) string` 全函数（**指针入参**；nil 或 `len(cursor.Entries) == 0` 返回空串；base64url NoPadding JSON 对象 `{"entries":[...]}`）；`DecodeMemoryPageToken(token string, sort []*MemorySortTerm) (*MemoryPageCursor, error)`（**返回指针，成功恒非 nil**；空 token/坏 base64/坏 JSON（含顶层非对象——unmarshal 入 struct 即拒）/`entries` 缺失/非数组/为空/entry 为 null/entry `Field` 或 `Value` 为空串拒绝；字段序列与 `sort` 逐位匹配校验——长度/字段名不符即拒，涵盖未知 field 与跨序重放；逐 entry 经 `sort[i].CursorValue(entry.Value)` **一次完成校验与转换**——typed 存入 `entry.typed`，仓储 thereafter 不再调用 `CursorValue`；全部错误 wrap `ErrInvalidPageToken`）；删除 `memoryCursorJSON`、旧 `MemoryPageCursor{Fields, Values}`、`encodeMemorySortValue`/`decodeMemorySortValue`。③ domain/repository.go：`ListMemories(ctx, template, session string, sort []*MemorySortTerm, cursor *MemoryPageCursor, pageSize int) ([]*Memory, string, error)`——注释声明 sort/cursor 为 domain 已验证输入（`ParseMemoryOrderBy`/`DecodeMemoryPageToken` 产物）、**nil = 首页唯一判据**、非 nil cursor 携非空且 typed 就绪 Entries（Decode 产物入参契约——手构空 Entries 为违约输入，fail-fast 同 `sortValue` 未知字段先例）、仓储不做重复校验。④ runtime/mongo/model.go：`sortValue(field string) any` **返回 typed 值**（`memory_id` → `d.MemoryID`（string）；`update_time` → `d.UpdateTime`（`time.Time`）——**零格式化**，wire 格式化移入 domain `CursorEntry`；未知字段 panic 不变）。⑤ runtime/mongo/repository.go：`ListMemories` 直译流程——scope filter `{template, session_id}` + `cursor != nil` 时 `filter["$or"] = memorySeekClauses(sort, cursor)`：**通用 OR 阶梯、O(n) 增量构造、无 error 返回**（构造路径零转换）——`prefix := make(bson.M)` 等值前缀逐键演进，每轮 `clause := 浅拷贝(prefix)`（独立 `bson.M`——`maps.Copy` 或等价循环；entry 值均为 string/`time.Time` 不可变标量，浅拷贝足够）+ 追加 `clause[term.MongoField] = bson.M{memoryCmpOp(term.Descending): cursor.Entries[i].Typed()}`，`clauses = append(clauses, clause)` 后 `prefix[term.MongoField] = cursor.Entries[i].Typed()`（尾项退化为等值汇入前缀——下一子句派生基，每键 O(1) 摊还；**不从零重建前序条件、不调用 `CursorValue`**；无"并入 filter"特例、不按 `len(sort)` 分支）；`SetSort(memorySortDocument(sort))`（`MongoField` × 方向直译）+ `SetLimit(pageSize+1)`；页满 `memoryNextPageToken(doc, sort) string`——`cursor.Entries` 逐键经 `term.CursorEntry(doc.sortValue(term.Field))` 构造 + `domain.EncodeMemoryPageToken(&cursor)`；删除 `memorySortSpec`/`matchMemoryCursor`/旧 `memorySeekFilter`/旧 `memoryNextPageToken`。⑥ handler/handler.go：`ListMemories` 接线——`domain.ParseMemoryOrderBy(req.GetOrderBy())` 错误 → INVALID_ARGUMENT；`var cursor *domain.MemoryPageCursor`（**空 token 时保持 nil 传仓储**），token 非空时 `cursor, err = domain.DecodeMemoryPageToken(token, sort)` 错误 → `toStatusError`（`ErrInvalidPageToken` → INVALID_ARGUMENT）；仓储调用传 `(sort, cursor, pageSize)`。⑦ 重写四个测试文件：`domain/sort_test.go`（既有表驱动用例语义保留、`want` 改 `[]*MemorySortTerm`——仅比较导出字段 `Field`/`MongoField`/`Descending`；**新增中间位合法用例**：`"memory_id, update_time desc"` → `[memory_id asc, update_time desc]`、`"memory_id desc, update_time"` → `[memory_id desc, update_time asc]`（原样不追加）；**删除"非末位拒绝"用例**；`""` 与纯空白（`"  "`/`"\t"`）→ `[memory_id asc]`（早返回路径）；重复字段含 `"memory_id, memory_id"`——map 重复检查）；`domain/pagination_test.go`（round-trip 以 `Encode→Decode(token, sort)` 闭环含纳秒与混键；**decode 产物 `entry.Typed()` 为 typed 值**——时间 entry 返回解析后 `time.Time`、字符串 entry 原样（转换单次发生的行为面）；JSON 形态 pin——顶层对象 `{"entries":[{field, value}, ...]}`、`typed` 不出现在序列化输出；坏 token 表驱动（空/坏 base64/坏 JSON/顶层非对象（如顶层数组）/entries 缺失/非数组/空/元素 null/缺 field/缺 value/长度不匹配/字段名不符含未知 field/跨序重放/时间不可解析）；等价拼写互通（`update_time desc` 与 `update_time desc, memory_id` 的 token 互解）；指针语义（`Decode` 成功返回非 nil `*MemoryPageCursor`；`Encode(nil)` 与空 Entries cursor → 空 token））；`runtime/mongo/repository_test.go`（**nil cursor = 首页**——fake `Find` 记录 filter 断言无 `$or`；`memorySeekClauses` 直测任意键数：1/2 键对照全路径行为，3 键以手构 `sort`（`MemorySortTerm` 导出字段，零 kind = string）+ entries 经 `term.CursorEntry(...)` 构造（wire-only 字面量手构 entry 的 `Typed()` 为 nil——违约输入，不经此路径），断言恰 3 子句、前缀相等与方向比较逐位正确——pin 无长度分支与增量构造（回归为 `switch len(sort)` 或每子句从零重建即失败）；**单次转换行为面**——fake `Find` 断言 filter 子句中的时间值为 `time.Time` 实例（typed 直达 bson，仓储无 string→time 转换路径；`memorySeekClauses` 无 error 返回为结构证据）；**删除防御性转换错误用例**（`TestMemoryListDefensiveCursorValueError`——仓储无转换即无该路径，坏时间由 domain decode 拒绝已在 codec 用例覆盖）；fake `Find` 求值任意子句数 `$or`（前缀相等 + 方向感知比较）+ 任意键序排序；既有行为面全保留：缺省回归 + 翻页全量（seek 经一个子句的 `$or`）、`update_time desc` 并列 `memory_id` 升序 + tie 跨页边界全序、`memory_id desc` 方向翻转、显式两键等价对照）；`handler/handler_test.go`（fake repo 记录 `sort` 与 `cursor` 指针（deep-equal 断言 `Entries` 的 `{Field, Value}`——`typed` 为私有）、对 `MemorySortTerm` 仅断言导出字段 `Field`/`MongoField`/`Descending`；合法/非法 `order_by` 表驱动——**中间位形态（`memory_id, update_time desc`）→ 仓储收到原样最终键无追加**、**空 token → 仓储收到 nil cursor**、坏 token/跨序 token → InvalidArgument、合法 token → 仓储收到已解码 cursor）。随行 `bazel build` + `bazel test //projects/game/memory/...` 全绿（原则 IV）；`bazel run //:gazelle projects/game/memory`
- [X] T039 [P] [US3] 核对 `projects/game/agent_v2/README.md` 的 planner memory 行与 Phase 9 第五轮终态的一致性（tie-breaker"完全不含 memory_id 才追加"表述与中间位合法、domain 校验+转换/仓储 typed 直译分层、nil 游标首页是否需要体现）；零改动或仅措辞微调，结论（是否改动及理由）记入 phase 汇报
- [X] T040 大型测试执行验收（依赖 T037/T038/T039；并入 Phase 8 的 T035 收口）：经 testplan SKILL 执行 `guitar run projects/game/testplan/system_test.yaml`（完整部署→测试→清理闭环），全部用例通过；重点核对 `TestMemoryServiceHttpListOrderBy`（通用语法正路径 + 非法集合 + 缺省回归——排序结果与翻页语义不变的端到端证据）、`TestMemoryServiceHttpCrudAndPagination`（缺省分页回归——token opaque、客户端透传零破坏）、`TestAgentV2TeamGameStatsPromptFaces`（12 条夹具快照断言零改动通过——服务端排序结果与断言一致）；任何 failed/flaky 修复后重跑至全绿
- [X] T041 按 `specs/065-agent-v2-team-refine/quickstart.md` 走查验证（并入 Phase 8 的 T036 收口）：§2 单测命令全绿（含 `//projects/game/memory/...` 与 `//common/js/dsh-plugins/memory-service/...`——JS 面零改动回归）+ §3 大型测试断言项 1–5 逐条对照 + §4 手动观察路径（可选）

**Checkpoint（验证门禁）**: `bazel test //projects/game/memory/... //common/js/dsh-plugins/memory-service/...` 全绿；`order_by` 缺省排序与翻页行为同前（token 对外 opaque、全消费面透传零破坏；对外语义面唯一变化 = 中间位 `memory_id` 键列表转为合法）；Go 三层无 `*string` 字段、无双结构体游标转换层、无值语义结构体容器（`style/golang.md` 指针节逐条对照——游标全链 `*MemoryPageCursor`、**nil = 首页唯一判据**，无 `len(Entries)` 判首页路径）；`ParseMemoryOrderBy` 主流程顶层（空白早返回固定默认值）、`validated := make(map[string]*MemorySortTerm)` 为唯一已验证存储（兼重复检查/`memory_id` 存在性查找/term 存放与取用，排序趟再过 items 恢复请求原序——无 `seen`、无 bool map、无中间结构）、**无 `memory_id` 位置拒绝**（填补判据 = map 完全不含才追加——中间位用例在）；**仓储零 string↔typed 转换**（`sortValue(field) any` typed 返回、seek 读 `entry.Typed()` 原样入 bson、next-token 经 `term.CursorEntry` 构造——runtime/mongo 无 `CursorValue` 调用点）、`memorySeekClauses` 无 error 返回且为增量前缀构造（等值前缀逐键演进，无每子句从零重建——fake 断言 filter 时间值为 `time.Time` 实例、防御性转换用例已删除）；seek 构造无长度分支（单一循环覆盖任意键数、单键 = 单子句 `$or`，3 键直测用例在）；契约 §1 item 9 classDiagram 与值转换责任表已核对（对象创建/持有/调用方向、每条转换归属层、nil 语义与实现一致）；"新增一个可排序字段 = 白名单一行 + accessor 一行 + 一个复合索引"仍成立（seek 构造零改动）；testplan 实际执行全绿（all cases passed）；quickstart 校验项逐条对照通过。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Foundational）**: 无前置——直接开始；**阻塞 Phase 2/3/5**（team 接口泛化是 US1/US2/US4 的前提）；**不阻塞 Phase 4**（memory-service 独立，可并行）
- **Phase 2（US1）**: 依赖 Phase 1；**阻塞 Phase 3**（US2 断言针对 US1 的触发实现）与 Phase 6（大型测试验收覆盖 US1/US2）
- **Phase 3（US2）**: 依赖 Phase 2
- **Phase 4（US3）**: 无代码依赖、不消费 Phase 1 产物——可与 Phase 2/3/5 并行
- **Phase 5（US4）**: 依赖 Phase 1（section.ts 属 team 插件；roster 断言与 US1 注册面解耦——纯函数直测）
- **Phase 6**: 依赖 Phase 2/3/4/5 全部完成（T015/T016/T018 可与前置 phase 部分重叠编写；T017 依赖 T015/T016 完成后收口；T019 执行验收须全量就绪）
- **Phase 7（US3 修改）**: 依赖 Phase 4 已交付（JS 面改造对象）与 Phase 6 的大型测试基线；T021/T022 可并行先行（互不依赖文件），T023 依赖 T021+T022（proto 再生成 + domain 排序面），T024 依赖 T023（接口与调用点）；T025 依赖 T021（proto-loader 读源 proto 的 wire 字段），T026 依赖 T025；T027 依赖 T024（端到端经服务行为）；T028 随时可做；T029/T030 收口（须 T021–T028 全部完成）——**T029/T030 的验收执行并入 Phase 8（T035/T036）统一收口**（Phase 8 重造 Go 链后一次执行，避免双重验收）
- **Phase 8（US3 修改·通用化）**: 依赖 Phase 7 已交付（Go 三层改造对象）；T031（proto 注释 + domain sort.go）与 T034（README）不同文件可并行先行；T032 依赖 T031（`MemorySortTerm` 类型与 proto 契约），是编译闭环原子 task（domain 签名变更牵连 mongo/handler，不可按层拆分）；T033 依赖 T032（断言语义随服务行为翻转）；T035 依赖 T032+T033+T034（全量就绪后执行）；T036 收口（依赖 T035）——**T035/T036 的验收执行并入 Phase 9（T040/T041）统一收口**（Phase 9 重铸 Go 链后一次执行，避免双重验收）
- **Phase 9（US3 修改·Go 习惯与分层重铸）**: 依赖 Phase 8 已交付（Go 三层通用机制实现为改造对象）；T037（proto 注释修订）与 T039（README 核对）不同文件可与 T038 并行先行；T038 是编译闭环原子 task（`MemorySortTerm`/`MemoryPageCursor`/仓储签名变更牵连 domain/mongo/handler 与全部测试，不可按层拆分——第五轮修订后 T037–T039 待重做）；T040 依赖 T037+T038+T039（全量就绪后执行）；T041 收口（依赖 T040）

### User Story Dependencies

- **US1 (P1)**: Phase 1 后即可开始——MVP 核心
- **US2 (P1)**: 依赖 US1 的触发实现（同一编排面，测试驱动固化不变量）
- **US3 (P2)**: 独立（memory-service 包，无跨包依赖）
- **US4 (P2)**: 依赖 Phase 1；与 US1 无代码依赖（section 措辞 vs 编排触发）

### Within Each User Story

- 数据/纯函数先行（board/text/client/snapshot），再机制（runtime/announcer/orchestrator），再集成（session 订阅）
- 每完成一个 task 即 build+test（原则 IV），不积攒

### Parallel Opportunities

- Phase 1 内：T001/T002 串行（类型耦合），T003 依赖 T001/T002，T004 收口
- Phase 2 内：T005/T006/T007 三者不同文件可并行（导出面收口在 T008）；T009 与 T005–T008 可并行；T008→T010 串行（访问器依赖）
- Phase 4 与 Phase 2/3/5 全程可并行（不同包）
- Phase 6 内：T015/T016/T018 可并行（新增共享 helper 统一写入 `agent_v2_helpers_test.go`，由 T015 先行）；T017 依赖 T015/T016，串行收口
- Phase 7 内：T021（proto）/T022（domain）/T028（README）三者不同文件可并行；T023→T024 串行（Go 服务链）；T025→T026 串行（JS 链，与 Go 链可并行）；T027 依赖 T024 后编写；T029/T030 并入 Phase 8 收口
- Phase 8 内：T031（proto 注释 + sort.go）与 T034（README）不同文件可并行；T032 串行核心（Go 链编译闭环原子 task）；T033 依赖 T032；T035→T036 串行收口（并入 Phase 9）
- Phase 9 内：T037（proto 注释修订）与 T039（README 核对）不同文件可并行；T038 串行核心（Go 链编译闭环原子 task）；T040→T041 串行收口

---

## Parallel Example: User Story 1

```bash
# 三个纯数据/纯函数任务并行：
Task: "T005 [P] [US1] board.ts+runtime.ts 分项计数与终局携带"
Task: "T006 [P] [US1] text.ts gameStatsText 模板与测试"
Task: "T007 [P] [US1] announcer.ts SaoleiSystemMember 与测试"

# 宿主投影任务可与上述并行：
Task: "T009 [P] [US1] history.ts appendAnnouncement 与测试"

# 收口串行：
Task: "T008 [US1] index.ts 导出收口 + orchestrator.ts 触发/注册/访问器" → "T010 [US1] session.ts 订阅落地"
```

---

## Implementation Strategy

### MVP First（Phase 1 + Phase 2 = US1）

1. 完成 Phase 1：team 成员消息源接口（agent 语义零变化回归）
2. 完成 Phase 2：扫雷系统终局播报全链（计数 → 播报 → 复盘输入 → 团队视图）
3. **STOP and VALIDATE**：US1 单测链独立验证（不需要大型测试即可证明播报机制正确）

### Incremental Delivery

1. Phase 1 → 基础就绪（接口泛化，行为零变化）
2. + Phase 2 → US1 交付（MVP：终局统计播报）
3. + Phase 3 → US2 交付（优先序不变量固化）
4. + Phase 4 → US3 交付（快照近因，可随时插入）
5. + Phase 5 → US4 交付（提示词澄清）
6. + Phase 6 → 大型测试验收 + 文档终态
7. + Phase 7 → US3 修改交付（memory List 服务端排序 + 客户端单页装载与收缩）
8. + Phase 8 → US3 修改交付·通用化（通用 order_by 映射与通用游标）
9. + Phase 9 → US3 修改交付·Go 习惯与分层重铸（domain 全量校验+转换单次化/仓储 typed 直译/struct 包装指针游标（nil = 首页）/解析空白早返回与 map 填补判据/通用 OR 阶梯 seek（无长度分支、O(n) 增量构造），重新大型测试验收）

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- 每个 phase 的"文档清单"为该 phase 必读集合（constitution 原则 V 三分类格式）；编码前完整阅读
- web 前端 / preset 模板零改动——不在任务面内；proto 与 memory Go 服务经 Phase 7 进入任务面（ListMemories `order_by`，2026-09-16 第一次用户裁定），经 Phase 8 通用化（通用语法/白名单/收尾/通用游标/单路径仓储，2026-09-16 第二次用户裁定），经 Phase 9 以 Go 习惯与分层重铸（domain 全量校验、仓储已验证输入直译、struct 包装游标、指定 tie-breaker、通用 OR 阶梯 seek（无长度分支），2026-09-16 第三、四次用户裁定；第五次用户裁定修正解析填补判据（完全不含 `memory_id` 才追加、中间位合法、空白早返回 + map 判据）、游标全链指针化（nil = 首页）、值转换单次化与责任收敛 domain（entry 携 typed、term 对称转换方法、仓储零转换）、seek O(n) 增量构造、契约补对象关系 classDiagram——排序结果与翻页语义零变化，token 对外 opaque、全消费面透传零破坏）
- Commit after each task or logical group; stop at any checkpoint to validate independently
