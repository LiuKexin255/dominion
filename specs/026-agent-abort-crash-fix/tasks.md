# Tasks: Fix Agent Service Crash on Desktop Disconnect

**Input**: Design documents from `/specs/026-agent-abort-crash-fix/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/stream-abort-contract.md, quickstart.md.

**Tests**: 单元测试 (`*.test.ts`) 是每个代码任务的一部分（宪章原则 IV — 编译 `bazel build` + 单测 `bazel test` 在每次代码变更时执行，**不单独分配 task**）。大型测试（服务验收）单独分配为验收 task（宪章原则 VI）。

**Organization**: 按 spec.md 的用户故事组织（US1/US2 均 P1）。US1 是进程不崩溃的地板保证，US2 是 017 优雅中断行为对等的天花板。两者共享同一组代码变更（`safeWrite` + 全局 handler），实现上不可分割，因此合并在同一 phase 中实现，按故事标注区分验证视角。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、依赖已完成）
- **[Story]**: 用户故事归属（US1/US2）
- 所有 task 描述包含确切文件路径

## Path Conventions

单工程变更，根于 `projects/game/agent/src/`：
- agent (TypeScript): `projects/game/agent/src/...`
- 大型测试: `projects/game/testplan/...`

---

## 全局约定（宪章原则 I / IV / V）

- **引用溯源**：代码注释引用 specs/契约时写明相对路径（如 `specs/026-agent-abort-crash-fix/contracts/stream-abort-contract.md §1`）。
- **编译+单测门禁**：每个代码 task 完成后 MUST 运行 `bazel build //projects/game/agent/...` 与 `bazel test //projects/game/agent/...`，作为该 task 的一部分。
- **行号说明**：T002/T003/T004/T005 中引用的行号基于当前代码快照，实际实现时如行号因文件编辑偏移，应以内容匹配为准（grep `stream.write(` 在 `handler.ts` 中定位所有调用点）。
- **本 feature 的 spec/plan/research/data-model/contracts 为必读**（宪章原则 V 注：无需在下方各 phase 重复列出）。

---

## Phase 1: User Story 1+2 — 修复崩溃 + 恢复优雅中断行为 (Priority: P1)

**Goal**: (a) 在 `handler.ts` 中新增 `safeWrite` 辅助函数并替换所有 `stream.write()` 调用，使写入已关闭 stream 的异常被捕获而非逃逸为 unhandled rejection。(b) 在 `bootstrap.ts` 中注册全局 `unhandledRejection` 处理器作为纵深防御。(c) 恢复 017 的全部优雅中断可观测行为（不向 dead peer 发出 frames、释放 mutex、可重连、正常 turn 路径不变）。

**Independent Test**: 启动 agent 服务，建立 session 并开始 turn；在 turn 进行中断开 desktop bidi stream；确认 agent 服务进程不退出（PID 不变）、其他 session 不受影响、被断开 session 的 turn 已停止且不消耗 LLM token、重连后可从 checkpoint 继续。

**Depends on**: 无前置依赖（直接修复现有代码）。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：
  - `style/javascript.md`（§测试：vitest_test 宏、Mock/DI 约定、禁止跨包 vi.mock、验证 mock 生效；§引用：注释引用外部文件需写明路径）
- **官方文档**：
  - Node.js `process` 事件 `'unhandledRejection'` — https://nodejs.org/api/process.html#event-unhandledrejection （默认 `--unhandled-rejections=throw` 行为、handler 注册语义）
  - Node.js `EventEmitter.captureRejections` — https://nodejs.org/api/events.html#capture-rejections-of-promises （async listener 返回 rejected Promise 默认变成 unhandled rejection 的机制）
- **技术文章**：无。

### 任务

- [ ] T001 [US1] 在 `projects/game/agent/src/handler.ts` 中新增私有辅助函数 `safeWrite`：签名 `(stream: grpc.ServerWritableStream<AgentFrame, AgentFrame>, frame: AgentFrame, sessionId: string): void`；用 try-catch 包裹 `stream.write(frame)`，catch 块中 `warn("stream write failed (peer disconnected?)", { sessionId, error: String(err) })` 并吞掉异常（不重新抛出）。函数**MUST NOT throw**（契约 §1 不变量）。依据 `specs/026-agent-abort-crash-fix/contracts/stream-abort-contract.md` §1、`data-model.md` §1。

- [ ] T002 [US1] 在 `projects/game/agent/src/handler.ts` 的 `Connect` handler 中，将以下 **catch 块内** 的 `stream.write()` 调用（崩溃向量 — research.md §D）替换为 `safeWrite(stream, frame, sessionId)`：
  - 行 527: `stream.write(warnFrame)` → `safeWrite(stream, warnFrame, sessionId)`（catch 块 else 分支 warn frame）
  - 行 537: `stream.write(waitFrame)` → `safeWrite(stream, waitFrame, sessionId)`（catch 块 else 分支 wait frame）

- [ ] T003 [US1] 在 `projects/game/agent/src/handler.ts` 的 `Connect` handler 中，将以下 **try 块内** `for await` 循环及循环后** 的 `stream.write()` 调用替换为 `safeWrite(stream, frame, sessionId)`：
  - 行 434: `stream.write(thinkFrame)` → `safeWrite(stream, thinkFrame, sessionId)`
  - 行 446: `stream.write(textFrame)` → `safeWrite(stream, textFrame, sessionId)`
  - 行 466: `stream.write(toolCallFrame)` → `safeWrite(stream, toolCallFrame, sessionId)`
  - 行 490: `stream.write(toolResultFrame)` → `safeWrite(stream, toolResultFrame, sessionId)`
  - 行 509: `stream.write(waitFrame)` → `safeWrite(stream, waitFrame, sessionId)`（post-loop wait frame，`if (controller.signal.aborted)` else 分支内）

- [ ] T004 [US1] 在 `projects/game/agent/src/handler.ts` 的 `Connect` handler 中，将以下 **try 块外** data callback 内的 `stream.write()` 调用替换为 `safeWrite(stream, frame, sessionId)`：
  - 行 268: `stream.write(statusFrame)` → `safeWrite(stream, statusFrame, sessionId)`（status 响应）
  - 行 348: `stream.write(warnFrame)` → `safeWrite(stream, warnFrame, sessionId)`（profile mismatch warn）
  - 行 357: `stream.write(waitFrame)` → `safeWrite(stream, waitFrame, sessionId)`（profile mismatch wait）
  - 行 372: `stream.write(warnFrame)` → `safeWrite(stream, warnFrame, sessionId)`（no profile warn）
  - 行 386: `stream.write(waitFrame)` → `safeWrite(stream, waitFrame, sessionId)`（no profile wait）

- [ ] T005 [US1] 在 `projects/game/agent/src/handler.ts` 的 sink callback（行 394-396，`registerSink` 注册的回调）中，将 `stream.write(contentEnvelope)` 替换为 `safeWrite(stream, contentEnvelope, sessionId)`。依据 `contracts/stream-abort-contract.md` §1 "Usage rule" 最后一段（sink callback 也应被保护，虽然 `cleanupSinks` 在 stream end/error 时 unregister 了 sink，但窗口仍然存在）。

- [ ] T006 [P] [US1] 在 `projects/game/agent/src/bootstrap.ts` 中注册全局 `unhandledRejection` 处理器：在 `main()` 函数内、`installReporter(...)` 之后、`startServer()` 之前，添加 `process.on("unhandledRejection", (reason) => { error("unhandled promise rejection", { reason: String(reason) }); })`。**MUST NOT** 调用 `process.exit()`（契约 §2 不变量）。依据 `contracts/stream-abort-contract.md` §2、`data-model.md` §2、`research.md` §E D4。

- [ ] T007 [US2] 在 `projects/game/agent/src/handler.test.ts` 中新增单元测试，覆盖以下场景（依据 `quickstart.md` Scenarios 1-3，以及 spec.md FR-004/FR-005/FR-007）：
  - **"safeWrite catches write error on closed stream"**（quickstart Scenario 1）：mock stream 的 `write()` 抛出 `new Error("ERR_STREAM_DESTROYED")`，调用 `safeWrite`，断言不抛出异常、有 `warn` 日志产出。
  - **"catch-block write does not crash on closed stream"**（quickstart Scenario 2, spec FR-001/FR-002）：模拟 turn 进行中 `generateTurn` 抛出非 abort 错误、mock stream `write()` 每次抛出，断言 data callback 完成而不崩溃、无 unhandled rejection。**附加断言**：`finally` 块已执行（`activeTurns` 中不包含该 session、`releaseMutex` 已被调用）—— 验证 FR-004（mutex 释放）和 FR-005（finally 块不受 catch 块异常影响）的隐含保证。
  - **"disconnect during turn emits no frames to dead peer"**（quickstart Scenario 3, spec FR-003 / 017 FR-004）：触发 `stream.on("end")` → `abortAllTurns()`，断言 abort 后无 `stream.write()` 调用（catch 块进入 `if (controller.signal.aborted)` 分支，只记录 info）。
  - **"checkpoint consistency after mid-turn abort"**（spec FR-005 / 017 FR-006）：模拟 turn 中途被 abort，验证 `ListMessages` 返回的对话状态一致 —— 无部分 LLM 消息、无孤立 tool 消息、用户输入不丢失。若此验证在单元层面难以完整模拟（需要 checkpointer），至少验证 catch 块 safeWrite 路径不干扰 `finally` 块中的资源清理逻辑（`activeTurns.delete` / `releaseMutex`）。
  - **"idle session disconnect produces no side effects"**（spec FR-007 / 017 FR-009）：在无 in-flight turn 时触发 disconnect（`stream.on("end")` → `abortAllTurns()`），断言无 `stream.write()` 调用、无 abort 工作执行（`controller.signal.aborted` 保持 `false`，因为无 active controller）、无状态变更。
  同步更新既有 disconnect 相关测试以适配 `safeWrite` 调用路径（如有 mock 断言基于 `stream.write` 的直接调用）。

  **Edge case 覆盖说明**（spec.md Edge Cases L66-74）：上述场景中，(a) "catch-block write does not crash" 覆盖"多个并发流消费者处于不同阶段"的 abort 行为；(b) "idle session disconnect" 覆盖"Abort 时无 turn 在进行中"；(c) "disconnect during turn emits no frames" 覆盖"Abort 恰好发生在 stream.output 已 resolve 之后"（该场景下 catch 块走 `if (controller.signal.aborted)` 分支）。"重连发生在流尚未完全拆除时"与"工具结果返回后断开"的验证依赖大型测试（T009 checkpoint-resume suite）。

**Checkpoint**: `safeWrite` 已替换所有 `stream.write()` 调用；全局 handler 已注册；单元测试通过；编译+单测全绿（`bazel build //projects/game/agent/...` + `bazel test //projects/game/agent/...`）。**SC-005 审计**：实现者完成本 phase 全部 task 后，检查 `handler.ts` 中不再存在未保护的 `stream.write()` 裸调用（grep `stream.write(` 结果应全部被 `safeWrite` 包裹或具备显式的 try-catch 保护），且无其他 dangling Promise（如未被 await 的 async 调用）。

---

## Phase 2: 大型测试验收（宪章原则 VI）

**Purpose**: agent 是服务型应用，大型测试 MUST 实际执行（`guitar run`）且全部用例通过，仅构建检查不构成验收。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：`style/large_test.md`（测试组织：按模块拆分、复用既有测试计划 YAML、`guitar run` 执行、`pkg/testtool` 读环境变量；§反模式）。
- **官方文档**：无（`guitar` / `testplan` SKILL 提供执行指引）。
- **技术文章**：无。
- **SKILL**：执行大型测试前加载 `testplan` SKILL（`.opencode/skills/testplan/SKILL.md`）。

### 任务

- [ ] T008a 检查 `projects/game/testplan/system_test.yaml` 中 `checkpoint-resume` suite，确认是否已覆盖"desktop 断开后 agent 服务存活"场景（依据 `quickstart.md` Scenario 4）。记录检查结果（覆盖或未覆盖），供后续维护参考。

- [ ] T008b [US1] 在 `projects/game/testplan/agent_checkpoint_test.go` 中新增专项测试 case：连接 desktop、开始 turn、中途断开 bidi stream、断言 agent 服务进程仍存活（可对同一服务实例发起第二次 RPC）、断言日志中无 `unhandled promise rejection`。依据 `style/large_test.md` §测试组织（按模块组织、复用既有 helper）。**本 task 为 spec SC-001/SC-002 的直接验证，MUST 执行**（不依赖 T008a 的检查结果 —— 即使现有 suite 名义上覆盖，新增专项 case 不属于冗余，它针对"进程存活"这一最关键验收标准提供独立验证）。

- [ ] T009 通过 `testplan` SKILL 执行大型测试验收：`guitar run projects/game/testplan/system_test.yaml`，完成部署→测试→清理闭环；**所有用例 MUST 全部通过**（failed/flaky 即验收未通过，修复后重跑至全绿）。仅 `bazel build` 测试 target 不构成验收（宪章原则 VI v1.3.0）。依据 `quickstart.md` Scenarios 4-5。

**Checkpoint**: 大型测试全部通过；feature 验收完成。

---

## Dependencies & Execution Order

### Phase 依赖

- **Phase 1 (US1+US2 修复)**: 无前置依赖。
  - T001 (safeWrite helper) → T002/T003/T004/T005 (替换调用点，均依赖 T001 定义 helper)。
  - T006 (bootstrap.ts global handler) 与 T001-T005 不同文件，可并行（标记 [P]）。
  - T007 (单元测试) 依赖 T001-T005 完成（测试 safeWrite 行为）。
- **Phase 2 (大型测试)**: 依赖 Phase 1 全部完成。
  - T008a/T008b 可在 Phase 1 完成后立即开始（不依赖 T009）。
  - T009 依赖 T008b 完成（新 case 加入 suite 后再执行验收）。

### Within Phase 1 任务依赖

- T001 → {T002, T003, T004, T005}（safeWrite 必须先定义）。
- T006 ∥ T001-T005（不同文件 `bootstrap.ts` vs `handler.ts`）。
- T007 → 依赖 T001-T006 全部完成。

### Parallel Opportunities

- T006 可与 T001-T005 并行（不同文件）。
- T002/T003/T004/T005 在 T001 完成后可并行修改不同代码段（但同一文件 `handler.ts`，建议串行避免合并冲突）。
- Phase 2: T008a 和 T008b 可并行（检查现有 suite vs 编写新 case 互不依赖）。

---

## Implementation Strategy

### 当前状态

1. 023 的并发流消费模式（`mergeIterables` + `consumeToolResults`）是正确的 023 设计，**不变更**（research.md D5）。
2. 崩溃根因是 `handler.ts` catch 块中未保护的 `stream.write()` 在 stream 关闭时抛出同步异常（research.md §D）。
3. `bootstrap.ts` 缺少全局 `unhandledRejection` 处理器，使 rejection 导致进程退出。

### Incremental Delivery

1. Phase 1 → `safeWrite` + 全局 handler + 单测全绿（崩溃向量已关闭，017 行为已恢复）。
2. Phase 2 → T008a/T008b 完善大型测试用例 + T009 执行验收全绿。

---

## Notes

- [P] = 不同文件、依赖已完成。
- [Story] 标签映射到 spec.md 用户故事。
- US1（进程不崩溃）和 US2（017 行为对等）共享同一组代码变更；US1 标注实现 task，US2 标注验证 task。
- 每个代码 task 包含其编译 (`bazel build`) + 单测 (`bazel test`) + 相邻 `*.test.ts` 更新（宪章原则 IV）。
- 大型测试验收 MUST 实际 `guitar run`（宪章原则 VI v1.3.0），仅构建不构成验收。
- 每个 phase 编码前 MUST 完整阅读该 phase "文档清单" 全部文档（宪章原则 V）。
- `llm.ts` 的 `mergeIterables` / `consumeToolResults` / `streamFromAgent` **不变更**（research.md D5：bug 在错误处理周边，不在流消费架构）。
