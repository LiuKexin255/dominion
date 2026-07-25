# Tasks: Conversation Content-Model Refactor & Saolei MCP Simplification

**Input**: Design documents from `/specs/023-saolei-mcp-refine/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/content-model-contract.md, contracts/tool-dispatch-contract.md, contracts/debug-drawer-contract.md, quickstart.md.

**Tests**: 单元测试 (`*.test.ts` / `*_test.go`) 是每个代码任务的一部分（宪章原则 IV — 编译 `bazel build` + 单测 `bazel test` 在每次代码变更时执行，**不单独分配 task**）。下方每个代码 task 的描述都包含"更新相邻测试"的要求。大型测试（服务验收）单独分配为 Phase 6 的验收 task（宪章原则 VI）。

**Organization**: 按 spec.md 的用户故事组织（US1/US2/US3 均 P1，US4 P2）。US1 是架构主轴（proto 内容模型拆分 + 对话渲染），是 MVP；其 proto 变更是 clean break（spec C2）。

**2026-07-25 修订背景**：US3 实现时证实 MCP 工具架构无法满足原 FR-008 耦合（MCP handler 拿不到 `config.toolCall.id`，adapter 不能带 `additional_kwargs`）。方案修订为**解耦对话/操作通道**（research.md D10）、**Debug Confirm 改 session 顶部抽屉**（D11）、**saolei(MCP) 状态 neutral**（D12）。Phase 1–3 已按原（耦合）设计实现并提交；本 tasks.md 标注其完成状态，新工作从 Phase 4 起。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、依赖已完成）
- **[Story]**: 用户故事归属（US1/US2/US3/US4）
- 所有 task 描述包含确切文件路径

## Path Conventions

多工程变更，根于 `projects/game/`：
- proto: `projects/game/game.proto`
- agent (TypeScript): `projects/game/agent/src/...`
- desktop (Go + Svelte): `projects/game/desktop/...`，前端 `projects/game/desktop/frontend/src/...`
- 大型测试: `projects/game/testplan/...`

---

## 全局约定（宪章原则 I / IV / V）

- **引用溯源**：代码注释引用 specs/契约时写明相对路径（如 `specs/023-saolei-mcp-refine/contracts/tool-dispatch-contract.md §1`）。
- **编译+单测门禁**：每个代码 task 完成后 MUST 运行 `bazel build //projects/game/agent/... //projects/game/desktop/...` 与 `bazel test //projects/game/agent/... //projects/game/desktop/...`（相关 target），作为该 task 的一部分。
- **proto 改动后**：运行 `bazel run //:gazelle projects/game/agent projects/game/desktop` 重新生成 `BUILD.bazel` 与 `game_types`（TS）/ Go proto 类型。（本修订不改 proto，无需 regen。）
- **本 feature 的 spec/plan/research/data-model/contracts 为必读**（宪章原则 V 注：无需在下方各 phase 重复列出）。

---

## Phase 1: Setup（LangChain 假设验证）— ✅ 已完成

**Commit**: `9975b52`。两个 LangChain 假设（research.md D2 `config.toolCall.id`、D4 `additional_kwargs` round-trip）已用 vitest spike 证实，主轴可建立其上。

- [X] T001 在 `projects/game/agent/src/spike.toolcall.test.ts` 验证 `config.toolCall.id` = `AIMessage.tool_calls[0].id`。
- [X] T002 在 `projects/game/agent/src/spike.checkpoint.test.ts` 验证 `additional_kwargs.toolResultStatus` + image block 经 `MemorySaver` round-trip 存活。

---

## Phase 2: User Story 1 — 对话仅渲染 LLM 消息；操作/控制信号为独立不渲染通道 (Priority: P1) ✅ 已完成（主体）

**Commit**: `c5a6d61`。proto 内容模型拆分（MessagePart/FlowPart/ToolCallPart）+ 对话渲染主轴 + live≡history 已实现并单测通过。

> **修订注**：本 phase 按原（耦合）设计实现——mouse 工具向 `dispatch` 传 `toolCallId`、`ChatView` 在 tool 气泡上渲染 Confirm（`heldToolIds`/`onConfirm`）。这些耦合面在 Phase 5（解耦 + 抽屉）中移除/替换。proto 拆分、tool_call/tool_result 气泡（按 `tool_call.id` 分组）、live/history 渲染**保留不变**。

- [X] T003 重构 `projects/game/game.proto`：拆分 `Part`→`MessagePart`/`FlowPart`，新增 `ToolCallPart`/`MessageParts`/`FlowParts`，`AgentFrame.payload`→`{message_parts, flow_parts}`，`Message.content`→`MessageParts`。
- [X] T004 `bazel run //:gazelle` 重新生成 TS/Go proto 类型。
- [X] T005–T017 agent bridge/llm/handler/tools、desktop app.go/view_model.go、前端 api.ts/App.svelte/ChatView.svelte 迁移到新模型（详见 commit）。

---

## Phase 3: User Story 2 — 工具结果在 history 中显示真实状态（无伪 "failed"）(Priority: P1) ✅ 已完成（native 工具）

**Commit**: `ece1569`。native(mouse) 工具真实状态经 `ToolMessage.additional_kwargs.toolResultStatus` 穿透 `MemorySaver`，`ListMessages` 直读；live 路径（`consumeToolResults`）从 raw stream events 读真实 status；文本推断已移除。

> **修订注（D12）**：真实状态穿透仅 native 工具。saolei(MCP) 工具状态 = neutral（UNSPECIFIED），在 Phase 4（US3）落地。

- [X] T018 `result-blocks.ts` 新增 `buildToolResultMessage`（返回带 `additional_kwargs.toolResultStatus` 的 ToolMessage）。
- [X] T019/T020 mouse 工具返回 `buildToolResultMessage`；`llm.ts` live 路径调整读真实 status。

---

## Phase 4: User Story 3 — Saolei MCP 无状态化（无格子状态、无校验、无 saolei_update）(Priority: P1) 🎯 已解阻塞

**Goal**: saolei MCP 仅暴露 4 个无状态工具（`saolei_init`/`saolei_click`/`saolei_flag`/`saolei_chord_click`），纯 dispatch-and-return；移除 `saolei_update`、`GameState`、validation、operate-then-update 交替；内置 skill 同步更新。saolei 工具状态 = neutral（D12）。

**Independent Test**: 配置 saolei profile；跑一轮 `saolei_init` → `saolei_click` → 再 `saolei_click`（中间无 update）；确认仅 4 工具、第二次 click 被接受、`saolei_update` 不存在、saolei 结果状态 neutral（非 FAILED）。

**Depends on**: US1（`FlowPart` 类型）已满足。**不依赖** dispatch 签名解耦（saolei 保持当前 `dispatch(part)` 调用，signal 传递在 Phase 5 统一）。US3 已因 D10/D12 解阻塞——saolei 不需要 `tool_call.id`，状态走 neutral。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：`style/javascript.md`（§测试：vitest_test 宏、Mock/DI 约定、禁止跨包 vi.mock、验证 mock 生效）。
- **官方文档**：
  - Google TypeScript Style Guide（通用 TS 编码规范基准）— https://google.github.io/styleguide/tsguide.html
  - MCP TS SDK `McpServer.registerTool` / `RequestHandlerExtra`（tool handler 参数、`extra.signal` 访问、result content blocks）— https://github.com/modelcontextprotocol/typescript-sdk （`@modelcontextprotocol/sdk` ^1.29.0）
  - LangChain JS tool calling（`ToolMessage` 结构）— https://js.langchain.com/docs/how_to/tool_calling/
- **技术文章**：无。
- **前序 feature 契约（本 phase 实现需要阅读其内容）**：
  - `specs/018-saolei-mcp/contracts/proto-operation-contract.md`（操作 Part 字段，desktop-facing 契约不变——`MouseMoveAndClickPart{WINDOW_MESSAGE}`/`KeyboardPressPart{F2}`）

### 任务

- [X] T021 [US3] 重写 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`：`createSaoleiMcpServer(bridge)`（**删除** `initialState` 参数与返回的 `state`）仅注册 4 个无状态工具——
  - `saolei_init`（无参，`bridge.dispatch({ keyboardPress: { key: "KEYBOARD_KEY_F2" } })`，保持当前 `dispatch(part)` 调用形式，**不**传 toolId；**signal 传递延迟到 T028（Phase 5）**——T026 改 dispatch 签名后 saolei 才能 clean 地 `dispatch(part, signal)`，避免本 phase 用 `dispatch(part, undefined, signal)` 过渡形式，此 double-touch 为有意安排）；
  - `saolei_click`/`saolei_flag`/`saolei_chord_click`（`{x,y}`，经 `geometry.center` 算像素后 `bridge.dispatch({ mouseMoveAndClick: { xPx, yPx, click: <ACTION>, method: "MOUSE_INPUT_METHOD_WINDOW_MESSAGE" } })`——click→LEFT_CLICK、flag→RIGHT_CLICK、chord_click→LEFT_RIGHT_PRESS）；
  - 每个工具返回 **MCP content blocks**（一个状态文本 block + 一个截图 image block）——**不**构造 `ToolMessage`、**不**设 `additional_kwargs`（adapter 包装的 ToolMessage 状态为 neutral，D12）；状态文本 block 记录 dispatch outcome 供 model/user 阅读；
  - **移除** `saolei_update`、`pendingUpdate`/`lastOp` 交替、所有 validation 调用、`UPDATE_STATUS_ENUM`、`STATUS_FAILED`/`pushResult` 相关引用。
  - 同步重写 `saolei-mcp.test.ts`：断言仅 4 工具、`saolei_init` 无 width/height、连续两次 `saolei_click` 均成功 dispatch、各工具 dispatch 的 proto 操作 Part 字段不变、**返回的 MCP content 不含 `additional_kwargs`（neutral）**。依据 `contracts/tool-dispatch-contract.md` §6、`data-model.md` §7、`research.md` D7/D12。
- [X] T022 [P] [US3] 删除 `projects/game/agent/src/mcp/saolei/game-state.ts`、`projects/game/agent/src/mcp/saolei/validation.ts`、`projects/game/agent/src/mcp/saolei/validation.test.ts`（无消费者）；运行 `bazel run //:gazelle projects/game/agent` 清理 `BUILD.bazel`。
- [X] T023 [P] [US3] 更新 `projects/game/agent/src/mcp-host.ts`：`createSaoleiMcpServer(looked.bridge)`（line 86 附近）去掉第二参数；`SaoleiMcpHandle` 去掉 `state` 字段（仅 `{ server }` 或直接返回 `McpServer`）；同步更新 `mcp-host.test.ts`。
- [X] T024 [US3] 更新 `projects/game/agent/src/operation-bridge.ts`：**删除** `pushResult` 方法（line 281 附近）与其专属 `FRAME_SENDER_SYSTEM` 常量（consumer-less，`saolei_update` 已由 T021 移除）；确认 `dispatch`/`handleResult`/`registerSink`/`unregisterSink` 契约完好。同步更新 `operation-bridge.test.ts` 移除 `pushResult` 用例。依据 `contracts/tool-dispatch-contract.md` §5、`research.md` D7。
- [X] T025 [P] [US3] 重写 `projects/game/agent/src/skill/saolei/SKILL.md`：描述 4 个工具与 top-left `(x,y)` 约定、阅读返回截图追踪棋盘；**移除** `saolei_update`、交替、cell-status 上报契约、validation 拒绝指引。依据 spec FR-022。

**Checkpoint**: saolei MCP 4 工具无状态、可连续调用；`saolei_update`/state/validation 已删；`pushResult` 已删；US3 独立测试通过（含 neutral 状态）。

---

## Phase 5: User Story 4 + 解耦返工 — 解耦对话/操作通道 + Debug Confirm 抽屉 (Priority: P2 + 发现性 D10 返工)

**Goal**: (a) **解耦返工（D10）**：`OperationBridge.dispatch` 移除 `toolId` 参数（bridge 始终自铸 UUID），mouse/saolei 工具不再向 dispatch 传 toolId（对话 `tool_call.id` 仅用于 `ToolMessage.tool_call_id` 分组，与操作通道无关）。(b) **Debug 抽屉（D11，US4）**：Confirm 控制从 tool 气泡移到 session 对话框顶部的**抽屉式提示**，显示操作请求内容（FlowPart 操作）+ 确认按钮，完全在操作通道上（解耦对话）。两者**捆绑在同一 phase** 以避免中间状态（agent 解耦后 FlowPart.tool_id 不再等于 tool_call.id，旧 Confirm-on-bubble 会失效；抽屉在同一 phase 上线以恢复可用的 debug 体验）。

**Independent Test**: 开 debug；跑一轮含 tool 操作；session 顶部出现抽屉显示操作请求（如"移动并点击 (136, 344) · 左键 · 窗口消息"）+ Confirm 按钮；agent 不前进直到确认（或 15min auto-continue）；确认后抽屉条目消失、tool 气泡更新为结果；hold 期间日志可见执行细节、截图不显示。解耦验证：连续两个 mouse 操作的 dispatched FlowPart.tool_id 为不同 bridge UUID（≠ tool_call.id），而各自 ToolMessage.tool_call_id = 其 config.toolCall.id。

**Depends on**: US1（tool 气泡 + tool_id）、US3（`pushResult` 已删、saolei 已无状态）。

> **优先级说明**：US4（P2）在 US3（P1）之后，因解耦返工（D10）是发现性的 foundational 清理，且必须与抽屉（US4）捆绑以维持可用 debug 体验。saolei 在本 phase 顺带补传 `signal`。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：
  - `style/golang.md`（desktop Go 规范：指针/数组/注释/单测表驱动）
  - `style/javascript.md`（§测试：vitest_test 宏、Mock/DI 约定；无通用 TS 编码规范）
- **官方文档**：
  - Google TypeScript Style Guide（前端 .svelte/.ts 通用编码规范基准）— https://google.github.io/styleguide/tsguide.html
  - Svelte 5 runes（`$state`/`$props`/`$derived`/`$effect`）— 参考仓库内既有用法 `projects/game/desktop/frontend/src/App.svelte`、`projects/game/desktop/frontend/src/components/ChatView.svelte`（已阅读）
  - Wails v2 runtime events（`runtime.EventsOn`/`EventsEmit`）— 参考仓库内既有用法 `projects/game/desktop/frontend/src/main.ts`
- **技术文章**：无。
- **前序 feature 契约（本 phase 实现需要阅读其内容）**：
  - `specs/022-desktop-debug-mode/contracts/debug-control-plane.md`（`SetDebugMode`/`ConfirmToolResult`/`game:debug:result-held`/`result-released` 方法与事件名，本 feature 不变；payload 扩展见 023 drawer 契约）

### 解耦返工（D10）

- [X] T026 [US4] 更新 `projects/game/agent/src/operation-bridge.ts`：`dispatch` 签名移除 `toolId` 参数——由 `dispatch(part: FlowPart, toolId?: string, signal?: AbortSignal)` 改为 `dispatch(part: FlowPart, signal?: AbortSignal)`；bridge **始终** `randomUUID()` 自铸 operation id（移除 `toolId ?? ` 条件，直接 `randomUUID()`）。`handleResult` 关联语义不变。同步更新 `operation-bridge.test.ts`（移除 toolId 透传断言；断言 dispatch 自铸 UUID）。依据 `contracts/tool-dispatch-contract.md` §1、`research.md` D10。
- [X] T027 [P] [US4] 更新 `projects/game/agent/src/tools/mouse_move/mouse-move.ts` 与 `projects/game/agent/src/tools/mouse_click/mouse-click.ts`：`bridge.dispatch(part, toolCallId, signal)` 改为 `bridge.dispatch(part, signal)`（**不再**向 dispatch 传 toolCallId）；**保留**读取 `(config as { toolCall?: { id?: string } })?.toolCall?.id` 用于 `buildToolResultMessage(result, toolCallId, name)` 的 `ToolMessage.tool_call_id`（对话分组用，不变）。同步更新相邻测试（断言 dispatch 不收 toolId；ToolMessage.tool_call_id 仍 = config.toolCall.id）。
- [X] T028 [P] [US4] 更新 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`：各工具 `bridge.dispatch(part)` 改为 `bridge.dispatch(part, extra.signal)`（从 MCP `RequestHandlerExtra.signal` 取 signal 传入；T026 后签名已为 2 参）。同步更新 `saolei-mcp.test.ts`。

### Debug 抽屉（D11，US4）

- [X] T029 [US4] 更新 `projects/game/desktop/app.go` 的 `handleInboundOperation` debug 分支（line 756 附近）：在 hold 前 emit `game:debug:result-held`，payload **扩展**为 `{ toolId, operation: { kind, summary, details } }`——`toolId` 为操作通道 bridge 自铸 id（`ToolResultPart.tool_id`）；`operation` 由 `recvLoop` 解码的 `FlowPart` 构建（`kind`=FlowPart 变体名 snake_case；`summary`=人类可读单行描述如"移动并点击 (136, 344) · 左键 · 窗口消息"/"按键 F2"；`details`={xPx,yPx,click,method,key} 原始字段）。`SetDebugMode`/`ConfirmToolResult`/`result-released`（含 reason）签名与行为不变。`holdAndRelease`+`SendFrame` 顺序不变。依据 `contracts/debug-drawer-contract.md` §2/§4。
- [X] T030 [US4] 更新 `projects/game/desktop/frontend/src/api.ts`：新增 `HeldOperation` 接口（`{ toolId; kind; summary; details }`）；扩展 `game:debug:result-held` 事件 payload 类型为 `{ toolId; operation: {...} }`（保留 `game:debug:result-released` 不变）；保留 `confirmToolResult`/`setDebugMode` wrapper。
- [X] T031 [US4] 更新 `projects/game/desktop/frontend/src/App.svelte`：将 `heldToolIds: Set<string>` 替换为 `heldOperations: HeldOperation[]`（$state）；`game:debug:result-held` 监听器改为按 payload 追加 `{ toolId, kind, summary, details }`（保序）；`game:debug:result-released` 监听器改为按 `toolId` 移除条目；`handleConfirm` 改为 `confirmToolResult(toolId)`（不变）。在 `.chat-main` 内 `.chat-top-bar` 与 `<ChatView>` 之间渲染 `<OperationConfirmDrawer>`（`heldOperations` + `onConfirm`）；**移除**传给 `ChatView` 的 `heldToolIds`/`onConfirm` props。依据 `contracts/debug-drawer-contract.md` §3。
- [X] T032 [P] [US4] 新建 `projects/game/desktop/frontend/src/components/OperationConfirmDrawer.svelte`：抽屉式面板（pin 在 session chat 顶部，视觉上与下方对话记录分离）；按 `heldOperations` arrival order 每条渲染一行（`summary` 文本 + Confirm 按钮，`onclick`→`onConfirm(entry.toolId)`）；多个同时 hold 垂直堆叠；`heldOperations` 为空时不渲染。纯操作通道，不引用任何对话 `tool_call.id`。依据 `contracts/debug-drawer-contract.md` §3.2。
- [X] T033 [US4] 更新 `projects/game/desktop/frontend/src/components/ChatView.svelte`：**移除** `heldToolIds`/`onConfirm` props 与 Confirm-on-bubble 渲染（022 §3 规则）；对话转录仅渲染 `MessagePart`（tool_call/tool_result 气泡按对话通道 `tool_call.id` 分组），无 held-state 分支。
- [X] T034 [US4] 验证 FR-026（15min `debugHoldTimeout` auto-continue）/FR-027（debug off 立即回传，无 hold/无抽屉）/**FR-011（hold 期间操作 + succeeded/failed outcome 在日志可达——非截图）**；如未被前序改动触碰则仅确认行为（022 DEBUG logging 已大量覆盖，确认 FR-011 reachability）；确认 20min `DISPATCH_TIMEOUT_MS` backstop 不变。

**Checkpoint**: 对话/操作通道解耦（dispatch 无 toolId，FlowPart.tool_id 为 bridge UUID ≠ tool_call.id）；Debug Confirm 在 session 顶部抽屉（操作通道），ChatView 无 held-state；US4 独立测试通过（抽屉显示操作请求、agent 阻塞至确认、确认后气泡更新、日志可见、debug off 立即回传）。

---

## Phase 6: Polish & 大型测试验收（宪章原则 VI）

**Purpose**: 跨故事的大型测试更新与执行验收。agent 是服务型应用，大型测试 MUST 实际执行（`guitar run`）且全部用例通过，仅构建检查不构成验收。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：`style/large_test.md`（大型测试组织：按模块拆分、复用既有测试计划 YAML、`guitar run` 执行、`pkg/testtool` 读环境变量）；`style/golang.md`（`*_test.go` 表驱动/given-when-then）。
- **官方文档**：无（`guitar` / `testplan` SKILL 提供执行指引）。
- **技术文章**：无。
- **SKILL**：执行大型测试前加载 `testplan` SKILL（`.opencode/skills/testplan/SKILL.md`）。

### 任务

- [ ] T035 [P] 更新 `projects/game/testplan/agent_saolei_test.go`：改为 4 工具无状态流（init→click→click 连续，无 update），断言 dispatch 的操作 Part 字段不变、saolei 结果状态 neutral（D12，非 FAILED）、操作 FlowPart 不在 `Message.content`。依据 `quickstart.md` Scenario 5。
- [ ] T036 [P] 更新 `projects/game/testplan/agent_operation_test.go`：改为断言 tool_call/tool_result MessageParts 与 mouse 工具携带的真实状态（解耦后 FlowPart.tool_id 为 bridge UUID，≠ tool_call.id）。依据 `quickstart.md` Scenario 7。
- [ ] T037 [P] 更新 `projects/game/testplan/agent_checkpoint_test.go`：改为 leave/re-enter 后 `ListMessages` 真实状态保持（mouse 成功仍 succeeded、失败仍 failed、saolei neutral）——quickstart Scenario 6。依据 `quickstart.md` Scenario 6、`data-model.md` §6。
- [ ] T038 [P] 更新 `projects/game/testplan/agent_dialog_test.go`：改为断言 tool_call 渲染 name+args、操作 FlowPart 不在 `Message.content`。复用 `helpers_test.go` 既有构造/断言，不复制 helper。依据 `quickstart.md` Scenario 7、`style/large_test.md`。
- [ ] T039 更新 `projects/game/testplan/system_test.yaml`：确认 `agent-saolei`/`agent-operation`/`checkpoint-resume`/`agent-dialog` suite 引用更新后的 case binary（通常无需新增 suite；若 deploy 拓扑无根本变化不得新建独立 YAML）。
- [ ] T040 通过 `testplan` SKILL 执行大型测试验收：`guitar run projects/game/testplan/system_test.yaml`，完成部署→测试→清理闭环；**所有用例 MUST 全部通过**（failed/flaky 即验收未通过，修复后重跑至全绿）。仅 `bazel build` 测试 target 不构成验收（宪章原则 VI v1.3.0）。

**Checkpoint**: 大型测试全部通过；feature 验收完成。

---

## Dependencies & Execution Order

### Phase 依赖

- **Phase 1 (Setup)**: ✅ 完成（commit `9975b52`）。
- **Phase 2 (US1, MVP 主轴)**: ✅ 完成（commit `c5a6d61`）。
- **Phase 3 (US2)**: ✅ 完成（commit `ece1569`，native 工具）。
- **Phase 4 (US3, P1)**: 依赖 US1（已满足）；**已解阻塞**。saolei 不依赖 dispatch 签名解耦。
- **Phase 5 (US4 + 解耦返工)**: 依赖 US1（tool 气泡）+ US3（`pushResult` 已删、saolei 已无状态）。解耦返工与抽屉**捆绑**避免 debug 中间断裂。
- **Phase 6 (验收)**: 依赖 US3 + US4 完成。

### 为何 US4 在 US3 之后（优先级说明）

US3（saolei，P1）不依赖解耦签名变更（saolei 保持 `dispatch(part)`），可独立先行。解耦返工（D10）是发现性的 foundational 清理，必须与抽屉（US4）捆绑（agent 解耦后旧 Confirm-on-bubble 失效，抽屉同期上线恢复可用 debug）。故顺序为 US3 → (解耦+US4)。

### Within US3 任务依赖

- T021 (saolei 重写) → T024 (pushResult 删除须在 saolei_update 移除后)。
- T022 ∥ T023 ∥ T025（删文件/改 host/改 skill 互不依赖）。

### Within 解耦+US4 任务依赖

- T026 (dispatch 签名) → T027 (mouse) → T028 (saolei signal)（同签名链）。
- T029 (app.go payload) → T030 (api.ts 类型) → T031 (App.svelte) → T032 (drawer 组件) → T033 (ChatView 移除)。
- T034 (验证) 最后。

### Parallel Opportunities

- US3: T022 ∥ T023 ∥ T025；T021→T024。
- 解耦+US4: T027 ∥ T028（mouse/saolei 不同文件，均依赖 T026）；T032（新组件）可与 T030 并行准备。
- Phase 6: T035 ∥ T036 ∥ T037 ∥ T038（不同测试文件）。

---

## Implementation Strategy

### 当前状态（修订后）

1. ✅ Phase 1–3 已完成（spike + US1 内容模型 + US2 native 真实状态）。
2. Phase 4 (US3): saolei 无状态化（解阻塞）。
3. Phase 5 (US4 + 解耦): 抽屉 + 移除耦合面。
4. Phase 6: 大型测试全绿验收。

### Incremental Delivery

1. US3 → saolei 无状态化（item 1）。
2. + 解耦+US4 → 对话/操作解耦 + debug 抽屉。
3. Phase 6 → 大型测试全绿验收。

---

## Notes

- [P] = 不同文件、依赖已完成。
- [Story] 标签映射到 spec.md 用户故事。
- 每个代码 task 包含其编译 (`bazel build`) + 单测 (`bazel test`) + 相邻 `*.test.ts`/`*_test.go` 更新（宪章原则 IV）。
- 大型测试验收 MUST 实际 `guitar run`（宪章原则 VI v1.3.0），仅构建不构成验收。
- 每个 phase 编码前 MUST 完整阅读该 phase "文档清单" 全部文档（宪章原则 V）。
- 本修订**不改 proto**（content-model-contract 不变）；仅 tool_id 语义（两个独立 id，D10）与 debug/drawer 表面（D11）变化。
