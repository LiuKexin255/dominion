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
- [ ] T019 大型测试执行验收：经 testplan SKILL 执行 `guitar run projects/game/testplan/system_test.yaml`（完整部署→测试→清理闭环），全部用例通过；任何 failed/flaky 修复后重跑至全绿
- [ ] T020 按 `specs/065-agent-v2-team-refine/quickstart.md` 走查验证：§2 单测命令全绿 + §3 大型测试断言项逐条对照 + §4 手动观察路径（可选）

**Checkpoint（验证门禁）**: testplan 全部用例通过（all cases passed）；quickstart 校验项逐条对照通过。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Foundational）**: 无前置——直接开始；**阻塞 Phase 2/3/5**（team 接口泛化是 US1/US2/US4 的前提）；**不阻塞 Phase 4**（memory-service 独立，可并行）
- **Phase 2（US1）**: 依赖 Phase 1；**阻塞 Phase 3**（US2 断言针对 US1 的触发实现）与 Phase 6（大型测试验收覆盖 US1/US2）
- **Phase 3（US2）**: 依赖 Phase 2
- **Phase 4（US3）**: 无代码依赖、不消费 Phase 1 产物——可与 Phase 2/3/5 并行
- **Phase 5（US4）**: 依赖 Phase 1（section.ts 属 team 插件；roster 断言与 US1 注册面解耦——纯函数直测）
- **Phase 6**: 依赖 Phase 2/3/4/5 全部完成（T015/T016/T018 可与前置 phase 部分重叠编写；T017 依赖 T015/T016 完成后收口；T019 执行验收须全量就绪）

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

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- 每个 phase 的"文档清单"为该 phase 必读集合（constitution 原则 V 三分类格式）；编码前完整阅读
- proto / Go 生产服务 / web 前端 / preset 模板零改动——不在任务面内
- Commit after each task or logical group; stop at any checkpoint to validate independently
