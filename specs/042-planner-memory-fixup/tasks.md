# Tasks: planner 记忆校准实现修复

**Input**: Design documents from `/specs/042-planner-memory-fixup/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: 编译 + 单测属每个代码变更任务的一部分（宪法原则 IV，不单列 task）；大型测试单列为 Phase 4 验收 task（FR-014，宪法原则 VI）。

**文档清单约定**: 仓库内设计文档与既有代码参考（contracts/、quickstart.md、既有源文件、既有 testplan 文件、仓库内 SKILL）本属"spec 相关文件 / 代码库参考"（宪法 V：spec 相关文件无需重复列出即为必读），但为满足"不做引用传递 / 规划即阅读"并精确到 section/行级阅读范围，仍按惯例显式列于各 phase 的「技术文章」分类下（该分类名义上为仓库外资料，此处为展示位置约定）；「代码规范文档」只列仓库内 `style/` 规范及其引用的外部规范，「官方文档」只列仓库外完整 URL。

**Organization**: 按用户故事组织。Phase 1 为 US1（saolei_operate 停止行格式）；Phase 2 为 US2（review 指令实时显示帧）；Phase 3 为 US3（RefreshTeam 后初始指令触发）；Phase 4 为大型测试验收。三个 US 均为 P1、相互独立、可并行实现。

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 归属用户故事（US1/US2/US3）；Polish phase 无 story 标签
- 描述含确切文件路径

## Path Conventions

多服务 web 应用（gRPC 微服务）：TS agent 在 `projects/game/agent/src/`，大型测试在 `projects/game/testplan/`。

---

## Phase 1: User Story 1 — saolei_operate 停止行格式 (Priority: P1) 🎯 MVP

**Goal**: `saolei_operate` 批量操作中途停止时，结果行从 `stopped at op K (reason)`（序号）改为 `stopped at {type}({x},{y}) (reason)`（具体操作参数），与 gameLog 渲染格式一致。

**Independent Test**: `saolei_operate` 传入一个有序操作列表，使某操作触发停止（游戏结束/结构性拒绝），返回的单次结果行含导致停止的操作的 type + 坐标（非序号）；正常完成/跳过的结果行格式不变。

### 文档清单（编码前必读）

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)；[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)；`specs/020-agent-resources-layout/contracts/skill-md-format.md`（SKILL.md frontmatter/body 格式契约——T002 改写须遵循；MUST 保留 frontmatter `name: saolei` + body 含 `# saolei` 与 `saolei_init` 标记，否则 `skill-loader.test.ts` 既有断言破坏）
- **官方文档**: 无（`operateResultText` 为仓库内既有 TS 函数，无第三方 API 变更）
- **技术文章**: `specs/042-planner-memory-fixup/contracts/saolei-operate-stop-format.md` §1-5（签名变更 + 调用点 + 影响面）；既有 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`（`operateResultText` `:660-680`、batch loop `:1058-1130`、`CellOperation` `:157-161`、`OperationType` `:151`）；既有 `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts`（停止行断言位置见 contract §3.2）；既有 `projects/game/agent/src/skill/saolei/SKILL.md`（全文——T002 待改文件，须明确哪些行过时）；既有 `projects/game/agent/src/team/planner.ts:172-174`（gameLog 渲染 `${op.type}(${op.x},${op.y})` 格式——新停止行格式的权威对齐来源）；`specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md` §2（既有停止行示意描述）

**Tasks**:

- [X] T001 [US1] 在 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` 变更 `operateResultText` 函数（`:660-680`）与 `saolei_operate` handler batch loop（`:1058-1130`）：①`operateResultText` 参数 `stoppedAt: number | null` → `stoppedOp: CellOperation | null`；②停止行模板 `stopped at op ${stoppedAt}` → `stopped at ${stoppedOp.type}(${stoppedOp.x},${stoppedOp.y})`（与 `buildReviewInput` 的 gameLog 渲染格式一致）；③batch loop 中 `stoppedAt = i + 1`（两处：game-end break + structural-stop break）→ `stoppedOp = operations[i]`；④`operateResultText(...)` 调用参数 `stoppedAt` → `stoppedOp`；⑤非停止行（`executed N ops` / `skipped S no-op ops`）不变；⑥空 operations 列表的早期返回路径（`:1049-1056`）传 `stoppedOp: null`（无停止）。详见 `contracts/saolei-operate-stop-format.md` §1-2。含单测（`projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts` 中所有 `stopped at op` 断言同步为 `stopped at {type}({x},{y})` 格式——用各测试用例的实际操作参数，约 lines 823, 854, 946, 955, 959, 993, 1499, 1621, 1854, 1877, 1880）。
- [X] T002 [P] [US1] 更新 `projects/game/agent/src/skill/saolei/SKILL.md`（player skill，经 `appendSkillBodyToPrompt(base, ["saolei"])` 注入 player systemPrompt）：将所有 `stopped at op K (reason)` 引用更新为 `stopped at type(x,y) (reason)`（`type` = click/flag/chord，`(x,y)` = 坐标）。**背景**：T001 改变了停止行格式，SKILL.md 中多处描述旧格式（约 lines 60, 66, 133, 171, 180-182, 258, 265, 272, 279）——属架构/工具面变更须同步完成（宪法原则 II）。逐处同步（新格式权威来源：`contracts/saolei-operate-stop-format.md` §1.2 + `saolei-mcp.ts` `operateResultText` 实际产出）：①outcome 行章节（`:60`）：`stopped at op K (reason)` → `stopped at type(x,y) (reason)`；②body shape 章节（`:66`）：`stopped at op K (...)` 引用同步；③Move validation 章节（`:133, 171`）：structural/terminal stop 结果行描述同步；④Reason code 表（`:180-182`）：各 `stopped at op K (...)` 引用同步；⑤Example play flow（`:258, 265, 272`）：示例停止行同步；⑥Summary 段（`:279`）：`stopped at op K (reason)` 引用同步。格式契约 `specs/020-agent-resources-layout/contracts/skill-md-format.md`（frontmatter `name: saolei` === folder name，MUST 保留）。MUST 保留 `skill-loader.test.ts` 既有断言标记（frontmatter `name: saolei`、body 含 `# saolei` 与 `saolei_init`）。非代码变更（Markdown 数据文件，无新单测）；编译验证 `bazel build //projects/game/agent/...`（data_files 解析不变）+ `bazel test //projects/game/agent/src:skill-loader_test`（既有断言 pass）。

**Checkpoint**: US1 单测全绿（`bazel test //projects/game/agent/src/mcp/saolei:saolei-mcp_test`）；停止行含具体操作参数（type + 坐标）；SKILL.md 同步反映新格式（无旧 `stopped at op K` 残留）；player skill 既有断言标记保留。可独立交付为 MVP。

---

## Phase 2: User Story 2 — review 指令实时显示帧 (Priority: P1)

**Goal**: planner.ts review 节点在将 `instruct_player` 暂存指令写入 `playerMessages` 时，发射实时显示帧（复用 instruction-node.ts 的 `ensureMessageId` + `emitChannelFrame` 模式），使 review 指令在 desktop player 页面对话列表实时可见——行为与 init/compact 场景一致。

**Independent Test**: review 节点 instruction 不为 null 时发射 `emitChannelFrame(PRIMARY_AGENT_NAME, instruction, writeBack.id, "MESSAGE_ROLE_USER")` 帧；发射帧的 frameId 与 checkpoint 持久化消息 id 一致（`ensureMessageId` 保证）；instruction 为 null 时不发射帧（既有行为不变）。

### 文档清单（编码前必读）

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)；[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)；`specs/019-js-test-reliability/`（js_test 执行模型 / DI seam）
- **官方文档**: [@langchain/langgraph — use-graph-api](https://docs.langchain.com/oss/javascript/langgraph/use-graph-api)（node 返回值→reducer、configurable）
- **技术文章**: `specs/042-planner-memory-fixup/contracts/review-instruction-display.md` §1-5（问题定位 + 修复方案 + `ensureMessageId` export + 消息对象复用 + 验证要点）；既有 `projects/game/agent/src/team/instruction-node.ts:138-141`（`ensureMessageId` 模块私有函数——T003 export 权威来源；`:299-319` 帧发射模式——planner.ts 复用的权威参考）；既有 `projects/game/agent/src/team/planner.ts:58-67`（当前 import 块——须追加 `PRIMARY_AGENT_NAME` + `ensureMessageId`）、`:321-323`（`emitChannelFrame` 既有读取——复用）、`:389-419`（review 节点 return——T003 重构位置）；`specs/041-realtime-init-push/contracts/realtime-channel-contract.md` §4（frameId == msg.id dedup 机制——`ensureMessageId` 保证一致性）；`specs/039-planner-memory-calibration/contracts/team-graph-contract.md` §4（instruct_player 工具 + R1 外部 buffer 中转 + review 场景帧发射契约）

**Tasks**:

- [X] T003 [US2] ①在 `projects/game/agent/src/team/instruction-node.ts:138-141` 将 `function ensureMessageId(msg: BaseMessage): string` 从模块私有改为 `export function ensureMessageId(msg: BaseMessage): string`（函数实现不变）。②在 `projects/game/agent/src/team/planner.ts` 追加导入：`PRIMARY_AGENT_NAME` from `"../session-team"`（当前 `:67` 仅导入 `ChannelFrameEmitter` 类型，追加常量）、`ensureMessageId` from `"./instruction-node"`。③重构 review 节点 return 逻辑（`:389-419`）：当 `instruction !== null` 时，先创建 `const writeBack = new HumanMessage(instruction)` → 调 `ensureMessageId(writeBack)` 保证 id → 发射 `emitChannelFrame(PRIMARY_AGENT_NAME, instruction, writeBack.id ?? undefined, "MESSAGE_ROLE_USER")`（复用 `:321-323` 既有 `emitChannelFrame` 变量）→ 构建 `const update: Partial<TeamStateValue>` 并设 `update.playerMessages = [writeBack]`（同一消息对象——发射帧与 checkpoint 持久化同 id，dedup 无重复）→ `return update`。当 `instruction === null` 时不发射帧、不写 playerMessages（既有行为不变）。消息顺序（FR-017：紧跟 tool_result 之后）不变——既有 `messagesStateReducer` 追加语义保证。详见 `contracts/review-instruction-display.md` §2。含单测（`projects/game/agent/src/team/graph.test.ts`：断言 review 节点 instruction 不为 null 时发射帧、frameId == writeBack.id、instruction 为 null 时不发射帧）。

**Checkpoint**: US2 单测全绿；review 指令实时显示帧发射与 init/compact 行为一致；`ensureMessageId` 从 instruction-node.ts export。

---

## Phase 3: User Story 3 — RefreshTeam 后初始指令触发 (Priority: P1)

**Goal**: `refreshTeam()` 清空短期消息通道后触发一次无游戏历史初始指令产出（复用 `runInitTurn()` 图执行逻辑），通过提取公共方法 `startInstructionTurn()` 实现——`triggerInitInstruction` 调用它（保留一次性守卫），`refreshTeam` 调用它（无守卫，可重复触发）。

**Independent Test**: `refreshTeam()` 清通道后调用 `startInstructionTurn()`；post-refresh 指令产出期间 `isBusy()` 为 true、`isRunning()` 为 false；连续 refresh 每次均触发指令；指令产出失败降级（不阻断 RefreshTeam）。

### 文档清单（编码前必读）

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)；[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)；`specs/019-js-test-reliability/`（js_test 执行模型 / DI seam）
- **官方文档**: [@langchain/langgraph — use-graph-api](https://docs.langchain.com/oss/javascript/langgraph/use-graph-api)（configurable、条件边）；[@langchain/langgraph — checkpointers](https://docs.langchain.com/oss/javascript/langgraph/checkpointers)（`updateState`/RefreshTeam）
- **技术文章**: `specs/042-planner-memory-fixup/contracts/refresh-instruction-trigger.md` §1-6（当前实现 + 修复方案 + `initTurn` 覆写安全性 + `isBusy`/`isRunning` 守卫 + `runInitTurn` 复用正确性 + 验证要点）；既有 `projects/game/agent/src/session-team.ts:316-318`（`refreshTeam` 当前实现——T004 修改位置）、`:340-349`（`triggerInitInstruction` 一次性守卫——T004 提取公共方法）、`:386-425`（`runInitTurn` 图执行逻辑——复用，不改）、`:557-575`（`isRunning`/`isBusy` 守卫语义——不变）、`:654-664`（`runTeamTurn` 中 `await this.initTurn`——post-refresh 覆写后新 turn await 新 Promise）；既有 `projects/game/agent/src/handler.ts:222-280`（RefreshTeam handler——不变，`isBusy()` 守卫已覆盖 post-refresh 期间拒绝）；既有 `projects/game/agent/src/context-middleware.ts:82-98`（`refreshTeamChannels`——不变）；`specs/039-planner-memory-calibration/contracts/team-graph-contract.md` §6（init instruction 异步触发语义）/§7（RefreshTeam 既有行为——本特性增补"清通道后触发初始指令"）

**Tasks**:

- [X] T004 [US3] 在 `projects/game/agent/src/session-team.ts` 提取公共方法并修改 `refreshTeam`：①新增私有方法 `private startInstructionTurn(): void`——提取自 `triggerInitInstruction` 的公共逻辑：`this.initInFlight = true; this.initTurn = this.runInitTurn().finally(() => { this.initInFlight = false; });`（`runInitTurn` 不变）。②`triggerInitInstruction`（`:340-349`）改为调用 `this.startInstructionTurn()`（保留 `if (this.initTurn) return;` 一次性守卫不变）。③`refreshTeam`（`:316-318`）在 `await refreshTeamChannels(...)` 后追加 `this.startInstructionTurn();`（无守卫——每次 refresh 均触发，FR-013）。④`this.initTurn` 覆写安全（`refreshTeam` 调用时 `isBusy()` 必为 false——handler 守卫保证；覆写后新 `runTeamTurn` await 新 Promise）。⑤`isBusy()`/`isRunning()` 守卫语义不变（`initInFlight` 机制既有：post-refresh 期间 `isBusy()=true` 拒绝二次 refresh/rebuild，`isRunning()=false` 不驱动 typing indicator）。详见 `contracts/refresh-instruction-trigger.md` §2-3。含单测（`projects/game/agent/src/session-team.test.ts`：①更新既有 "refreshTeam clears BOTH channels" 用例（`:319`）验证清通道后触发指令（`isBusy()` 为 true 期间、完成后 `playerMessages` 含新指令）；②新增连续 refresh 每次触发指令用例；③新增 post-refresh 期间 `isRunning()=false` 验证；`projects/game/agent/src/handler.test.ts`：既有 RefreshTeam 用例 pass——`isBusy()` 守卫已覆盖 post-refresh 期间拒绝，handler.ts 不变）。

**Checkpoint**: US3 单测全绿；`refreshTeam` 清通道后触发初始指令；连续 refresh 每次均触发；`isBusy`/`isRunning` 守卫不变；handler.ts 不变。

---

## Phase 4: Polish & 大型测试验收（FR-014，宪法原则 VI）

**Purpose**: 大型测试断言同步 + 全量构建/单测 + 大型测试执行。

### 文档清单（编码前必读）

- **代码规范文档**: `style/large_test.md`（**每个被测系统只维护一份测试计划 YAML**；用例按**模块**拆分非按 spec/场景编号；`go_largetest` rule；`guitar run <plan.yaml>`）；`style/golang.md`（大型测试遵守单测命名/表驱动/given-when-then 规范）；[Google Go Style Guide — Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)
- **官方文档**: 无
- **技术文章**: `specs/042-planner-memory-fixup/quickstart.md` 场景 1/2/3 + 大型测试执行段；既有 `projects/game/testplan/system_test.yaml`（既有 suite，**在此计划加 case，禁新建 YAML**）；既有 `projects/game/testplan/deploy_agent.yaml`（既有 deploy 拓扑，已含 memory 服务条目）；既有 `projects/game/testplan/agent_saolei_test.go`（saolei 模块——停止行断言更新位置见 `contracts/saolei-operate-stop-format.md` §3.3，约 lines 42, 47, 52, 406, 492-493, 511, 577, 598-599, 684, 705-706）；既有 `projects/game/testplan/saolei_team_test.go`（team 模块——停止行引用更新位置见 §3.4，约 lines 1155, 1182, 1324, 1328, 1338）；既有 `projects/game/testplan/helpers_test.go`（helper/fixture 模式——`:1850` 如有停止行引用则同步）；既有 `projects/game/fake-llm/service/testdata/sample_saolei_structural_stop.yaml`（fixture——`:7` 停止行注释同步）；既有 `projects/game/testplan/README.md` §3/§5（fake-llm fixture 与 helpers 常量 lockstep）；testplan SKILL（仓库内 `.opencode/skills/testplan/SKILL.md`，`tools/test/guitar`）

**Tasks**:

- [X] T005 [P] 更新大型测试 Go 断言于 `projects/game/testplan/`：①`agent_saolei_test.go` 中所有 `stopped at op K (reason)` 断言/注释更新为 `stopped at {type}({x},{y}) (reason)`（用各用例的实际操作参数——约 lines 42, 47, 52, 406, 492-493, 511, 577, 598-599, 684, 705-706）；②`saolei_team_test.go` 中 `stopped at op 1 (...)` 引用更新（约 lines 1155, 1182, 1324, 1328, 1338）；③`helpers_test.go` 如有 `stopped at op` 引用则同步（约 line 1850）。新格式权威来源：`contracts/saolei-operate-stop-format.md` §1.2。编译验证 `bazel build //projects/game/testplan/...`。
- [X] T006 [P] 更新 fake-LLM fixture 于 `projects/game/fake-llm/service/testdata/sample_saolei_structural_stop.yaml`：将 `stopped at op 2 (out_of_bounds)` 注释/期望同步为 `stopped at {type}({x},{y}) (out_of_bounds)`（用 fixture 对应的实际操作参数）。同步 `helpers_test.go` 期望常量（lockstep，`testplan/README.md` §5）。确认 `projects/game/fake-llm/service/message_store_test.go` pin 通过。
- [X] T007 Run `bazel build //...` + `bazel test //...`（全量 Go + TS 编译与单测全绿）；按 SC-001/SC-002/SC-003 在单测层验证（停止行含操作参数；review 指令帧发射；refresh 后指令触发）。`rg "stopped at op " projects/game/ specs/` 确认旧格式零残留（仅 `specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md` 示意描述 MAY 保留——与 quickstart.md 影响面检查一致）。
- [X] T008 经 **testplan SKILL** 执行 `guitar run projects/game/testplan/system_test.yaml`（完整部署→测试→清理闭环，**禁止仅 `bazel build` 替代验收**，宪法原则 VI）；**所有测试用例全部通过**（failed/flaky 即未通过，修复重跑至全绿）。覆盖：①saolei_operate 停止行含操作参数（SC-001）；②review 指令在 player 对话列表实时可见（SC-002）；③RefreshTeam 后初始指令产出（SC-003）。详见 `quickstart.md` 场景 1/2/3 大型测试段。

**Checkpoint**: 全量 `bazel build/test` 通过；`guitar run` 全部用例通过（FR-014、SC-004）；特性验收完成。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (US1)**: 无依赖；改动 `saolei-mcp.ts` + `saolei-mcp.test.ts` + `SKILL.md`。**不依赖** Phase 2/3。
- **Phase 2 (US2)**: 无依赖；改动 `instruction-node.ts` + `planner.ts`。**不依赖** Phase 1/3。
- **Phase 3 (US3)**: 无依赖；改动 `session-team.ts`。**不依赖** Phase 1/2。
- **Phase 4 (大型测试)**: 依赖 Phase 1/2/3 全部完成（端到端可测 + 大型测试断言同步）。

### User Story Dependencies

- **US1 (P1)**: 独立。Phase 1 后即可作 MVP 交付/验证。
- **US2 (P1)**: 独立。与 US1/US3 无文件冲突、无逻辑依赖。
- **US3 (P1)**: 独立。与 US1/US2 无文件冲突、无逻辑依赖。

### Parallel Opportunities

- **跨 Phase 并行**：Phase 1/2/3 三个 US 相互独立（不同文件、无逻辑依赖），可由不同 developer 并行实现。
- **Phase 1 内部**：T001（saolei-mcp.ts + test）与 T002（SKILL.md）不同文件、可并行——T002 的新格式以 `contracts/saolei-operate-stop-format.md` §1.2 为权威来源（与 T001 同一权威），无需等待 T001 确认；若实现中发现格式细节偏差再串行对齐。
- **Phase 4 内部**：T005（Go 断言）与 T006（fixture）不同目录、可并行；T007（bazel build+test）在 T005/T006 后；T008（guitar run）在 T007 后。

---

## Parallel Example: Phase 1/2/3 跨 US 并行

```bash
# 三个 US 相互独立（不同文件），可并行：
Task: "T001 [US1] saolei-mcp.ts operateResultText + batch loop + 单测"
Task: "T003 [US2] instruction-node.ts export + planner.ts 帧发射 + 单测"
Task: "T004 [US3] session-team.ts startInstructionTurn + refreshTeam + 单测"
```

## Parallel Example: Phase 4 内部

```bash
# T005/T006 不同目录、可并行：
Task: "T005 更新大型测试 Go 断言（agent_saolei_test.go, saolei_team_test.go, helpers_test.go）"
Task: "T006 更新 fake-LLM fixture（sample_saolei_structural_stop.yaml）"
```

---

## Implementation Strategy

### MVP First（仅 US1）

1. Phase 1 US1（saolei_operate 停止行格式）——独立交付、单测验证（SC-001）。
2. **STOP and VALIDATE**：US1 端到端可在 Phase 4 大型测试中验证。

### Incremental Delivery

1. Phase 1 US1 → 停止行格式修复（SC-001）。
2. Phase 2 US2 → review 指令实时显示（SC-002）。
3. Phase 3 US3 → RefreshTeam 指令触发（SC-003）。
4. Phase 4 大型测试 → 全部用例通过（SC-004，FR-014）。

### Parallel Team Strategy

- Developer A: Phase 1（US1）。
- Developer B: Phase 2（US2）。
- Developer C: Phase 3（US3）。
- Phase 4 在所有功能 phase 完成后统一执行。

---

## Notes

- [P] task = 不同文件、无未完成依赖；同文件多变更须串行（避免并发编辑丢失）。
- [Story] 标签映射到 spec.md 用户故事。
- 每个代码变更 task 内含其编译 + 单测（`bazel build`/`bazel test` 相关 target），不单列（宪法原则 IV）。
- 大型测试**仅在 Phase 4** 经 testplan SKILL 执行（`guitar run`，完整部署→测试→清理），禁止仅 `bazel build` 替代验收（宪法原则 VI）。
- 外部文档已在各 phase 文档清单显式列出；AGENTS.md 与 spec 文件为代码开发必读，不再重复。
- 间接引用已显式展开：`style/javascript.md` → Google TypeScript Style Guide；`style/large_test.md` → testplan SKILL/guitar；`specs/041-realtime-init-push/contracts/realtime-channel-contract.md` §4（frameId dedup 机制）；`specs/020-agent-resources-layout/contracts/skill-md-format.md`（SKILL.md 格式契约）。
- clean break：不考虑 039 既有大型测试断言的"兼容"——直接更新为新格式（本特性为 039 的 bugfix，大型测试断言同步更新是预期变更）。
