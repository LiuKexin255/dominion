# Tasks: Conversation Content-Model Refactor & Saolei MCP Simplification

**Input**: Design documents from `/specs/023-saolei-mcp-refine/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/content-model-contract.md, contracts/tool-dispatch-contract.md, quickstart.md.

**Tests**: 单元测试 (`*.test.ts` / `*_test.go`) 是每个代码任务的一部分（宪章原则 IV — 编译 `bazel build` + 单测 `bazel test` 在每次代码变更时执行，**不单独分配 task**）。下方每个代码 task 的描述都包含"更新相邻测试"的要求。大型测试（服务验收）单独分配为 Phase 6 的验收 task（宪章原则 VI）。

**Organization**: 按 spec.md 的用户故事组织（US1/US2/US3 均 P1，US4 P2）。US1 是架构主轴（proto 内容模型拆分 + 对话渲染），是 MVP；其 proto 变更是 clean break（spec C2），因此 US1 的 checkpoint 是新模型上首个编译+单测通过的构建。

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

- **引用溯源**：代码注释引用 specs/契约时写明相对路径（如 `specs/023-saolei-mcp-refine/contracts/content-model-contract.md §3`）。
- **编译+单测门禁**：每个代码 task 完成后 MUST 运行 `bazel build //projects/game/agent/... //projects/game/desktop/...` 与 `bazel test //projects/game/agent/... //projects/game/desktop/...`（相关 target），作为该 task 的一部分。
- **proto 改动后**：运行 `bazel run //:gazelle projects/game/agent projects/game/desktop` 重新生成 `BUILD.bazel` 与 `game_types`（TS）/ Go proto 类型。
- **本 feature 的 spec/plan/research/data-model/contracts 为必读**（宪章原则 V 注：无需在下方各 phase 重复列出）。

---

## Phase 1: Setup（LangChain 假设验证）

**Purpose**: 在动工主轴前，验证 research.md D2（`config.toolCall.id`）与 D4（`MemorySaver` 透传 `additional_kwargs`）两个设计所依赖的 LangChain 运行时行为。若假设不成立，须回到 plan/research 修订设计再继续。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：`style/javascript.md`（§测试 — DI / `vitest_test` 宏 / `vi.fn()` test-double seam；禁止跨包 `vi.mock`）。
- **官方文档**：
  - LangChain JS 工具 `RunnableConfig.toolCall.id` 类型守卫 — https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/utils.ts
  - LangChain JS tool calling（`AIMessage.tool_calls` / `ToolMessage.tool_call_id` 结构）— https://js.langchain.com/docs/how_to/tool_calling/
  - LangChain JS tools 概念（`config` 在 tool 函数中的访问）— https://js.langchain.com/docs/concepts/tools/
  - LangChain JS persistence（`MemorySaver` checkpoint over `BaseMessage`s）— https://js.langchain.com/docs/concepts/persistence/
- **技术文章**：无。

- [ ] T001 在 `projects/game/agent/src/spike.toolcall.test.ts` 新建 vitest spike：用 `fakeModel().respondWithTools([{name,args}])` 构造一个 `createAgent`，invoke 后断言被调用的 tool 函数的 `config.toolCall.id` 等于 `AIMessage.tool_calls[0].id`（验证 research.md D2）。若该字段在 `@langchain/langgraph` ^1.4.8 / `langchain` ^1.5.3 下不存在或形状不同，停止并修订设计。
- [ ] T002 在 `projects/game/agent/src/spike.checkpoint.test.ts` 新建 vitest spike：构造一个带 `additional_kwargs: { toolResultStatus: "TOOL_RESULT_STATUS_SUCCEEDED" }` 与一个 `image_url` content block 的 `ToolMessage`，经 `MemorySaver` 支撑的 `createAgent` 跑一轮后 `getState`，断言 `additional_kwargs.toolResultStatus` 与 image block 均存活（验证 research.md D4）。若不存活，停止并修订 status 承载方案。

**Checkpoint**: 两个 LangChain 假设均已用 spike 证实；主轴实现可建立其上。

---

## Phase 2: User Story 1 — 对话仅渲染 LLM 消息；操作/控制信号为独立不渲染通道 (Priority: P1) 🎯 MVP 主轴

**Goal**: 拆分 proto 内容模型（`MessagePart`/`FlowPart` + 新 `ToolCallPart`），让对话从 LLM 消息这单一真实源渲染；操作 Part 与 wait/warn/status 不再作为对话条目；live 与 history 渲染一致；tool_call 与 tool_result 合并为单个按 `tool_id` 演化的气泡。

**Independent Test**: 跑一轮含 tool 调用的对话：每个 tool call 渲染一个气泡（工具名 + 参数），该气泡随后原地更新为结果（状态 + 截图）；无操作 Part 或控制信号出现在对话中；离开并重进 session，气泡一致呈现。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：
  - `style/javascript.md`（**仅含 §测试**：js_test 执行模型、`vitest_test` 宏、Mock/DI 约定、`require()` for instrumented packages；**无通用 TS 编码规范**——通用 TS 风格见下 Google TypeScript Style Guide）
  - `style/golang.md`（desktop Go 规范：指针/数组/注释/单测表驱动）
  - `style/api.md` 及其引用的 AIP-126（枚举）https://google.aip.dev/126 、AIP-140（字段名）https://google.aip.dev/140 、AIP-180（向后兼容，本 feature 按 C2 豁免）https://google.aip.dev/180
- **官方文档**：
  - Google TypeScript Style Guide（本仓库 js/ts 通用编码规范基准，`style/javascript.md` §引用 指向此处，按宪章原则 V 显式列出）— https://google.github.io/styleguide/tsguide.html
  - LangChain JS `streamEvents` v3 事件 schema（`on_chat_model_end`/`on_tool_end`、`StreamEvent.data`）— https://api.js.langchain.com/classes/langchain_core_runnables.Runnable.html
  - LangChain JS tool calling（`AIMessage.tool_calls` / `ToolMessage`）— https://js.langchain.com/docs/how_to/tool_calling/
  - LangChain JS tools 概念（`config` 访问）— https://js.langchain.com/docs/concepts/tools/
  - Go `protojson` 序列化（camelCase、oneof 扁平化、bytes 为 base64）— 参考仓库内既有用法 `projects/game/desktop/view_model.go:115` `protoToJSONMap`（已阅读）
  - Svelte 5 runes（`$state`/`$props`/`$effect`）— 参考仓库内既有用法 `projects/game/desktop/frontend/src/App.svelte`、`projects/game/desktop/frontend/src/components/ChatView.svelte`（已阅读）
- **技术文章**：无。
- **前序 feature 契约（本 phase 实现需要阅读其内容）**：
  - `specs/018-saolei-mcp/contracts/proto-operation-contract.md`（操作 Part 字段：`MouseMoveAndClickPart`/`KeyboardPressPart`/`MouseInputMethod` 等，本 feature 保留不变）

### Proto 内容模型拆分

- [ ] T003 [US1] 按 `specs/023-saolei-mcp-refine/contracts/content-model-contract.md` §1–§6 重构 `projects/game/game.proto`：删除 `Part`/`PartBlock`；新增 `ToolCallPart{tool_id,name,args_json}`、`MessagePart`(oneof text/thinking/image/tool_call/tool_result)、`FlowPart`(oneof mouse_move/mouse_click/keyboard_press/mouse_move_and_click/wait/warn/status)、`MessageParts`/`FlowParts`；`AgentFrame.payload` 改为 `oneof{ message_parts=11; flow_parts=12; }` 并 `reserved 10,20,21,22`；`Message.content` 改为 `MessageParts`；`WaitSignal`/`WarnSignal`/`StatusSignal` 消息形状不变但归入 `FlowPart.kind`。枚举（`ToolResultStatus`/`MouseClickAction`/`MouseInputMethod`/`KeyboardKey`/`StatusSignalStatus` 等）保持不变。
- [ ] T004 [US1] 运行 `bazel run //:gazelle projects/game/agent projects/game/desktop` 重新生成 TS `game_types` 与 Go proto 类型，确认 `MessagePart`/`FlowPart`/`ToolCallPart`/`MessageParts`/`FlowParts` 已生成、旧 `Part`/`PartBlock` 已移除。（此后构建会因消费方仍引用旧类型而失败，后续 task 逐文件恢复编译。）

### Agent — bridge（tool_id 线程穿透）

- [ ] T005 [P] [US1] 更新 `projects/game/agent/src/operation-bridge.ts`：`dispatch(part: Part, signal?)` 改为 `dispatch(part: FlowPart, toolId?: string, signal?: AbortSignal)`；当传入 `toolId` 时将其 stamp 到操作的 `tool_id`（取代当前 `randomUUID()` 于 line 208），未传时回退 `randomUUID()`；发送的 envelope 改为 `{ payload: "flowParts", flowParts: { parts: [part] } }`；`pushResult` 的 envelope 改为 `{ payload: "messageParts", messageParts: { parts: [{ toolResult }] } }`（**保留** `pushResult`，US3 再删除）；其余 `registerSink`/`unregisterSink`/`handleResult` 与 20 分钟 `DISPATCH_TIMEOUT_MS` 不变。同步更新 `operation-bridge.test.ts`。依据 `contracts/tool-dispatch-contract.md` §1。

### Agent — llm（live tool_call/tool_result 产出）

- [ ] T006 [P] [US1] 更新 `projects/game/agent/src/llm.ts`：扩展 `ContentBlock` 联合类型新增 `{ type: "tool_call"; name; args; toolCallId }` 与 `{ type: "tool_result"; toolCallId; status; message; screenshot? }`；在 `streamFromAgent` 中，当流式 `AIMessage` 携带 `tool_calls` 时按 call 产出 `tool_call` block（取 `call.id`/`call.name`/`call.args`），当出现 `ToolMessage` 时产出 `tool_result` block（status 取自 `additional_kwargs.toolResultStatus`，缺省 `TOOL_RESULT_STATUS_UNSPECIFIED`；message+screenshot 取自 content blocks）。保留现有 text/reasoning 流式逻辑。同步更新 `llm.test.ts`。依据 `contracts/tool-dispatch-contract.md` §7、`research.md` D5。

### Agent — handler（Connect 产出 + ListMessages 重建）

- [ ] T007 [US1] 更新 `projects/game/agent/src/handler.ts` 的 `Connect`：将 text/thinking/image/tool_call/tool_result block 以 `message_parts` frame 发送（`payload: "messageParts"`），将操作 Part 与 wait/warn/status 以 `flow_parts` frame 发送（`payload: "flowParts"`）；桌面回传的 `ToolResultPart` 仍路由到 `bridge.handleResult`；移除 `PAYLOAD_ONEOF_KEYS` 中旧的 `content` 判别。同步更新 `handler.test.ts` Connect 相关用例。依据 `data-model.md` §4。
- [ ] T008 [US1] 更新 `projects/game/agent/src/handler.ts` 的 `ListMessages`：从 `BaseMessage` 重建 `MessageParts`——`AIMessage.tool_calls[i]` → `tool_call` part（`{ tool_id: call.id, name: call.name, args_json: JSON.stringify(call.args ?? {}) }`）；`ToolMessage` → `tool_result` part（`tool_id: msg.tool_call_id`、status 取 `msg.additional_kwargs?.toolResultStatus ?? "TOOL_RESULT_STATUS_UNSPECIFIED"`、message/screenshot 取自 content blocks）；**删除** `toolCallToPart`、`reconstructToolResult`、`inferToolResultStatus` 与 `PIXEL_SIZE_PATTERN` 文本推断路径。同步更新 `handler.test.ts` ListMessages 用例（含 tool_calls fixture 于 line 1642+）。依据 `contracts/tool-dispatch-contract.md` §4、`data-model.md` §6。

### Agent — 工具（tool_id + FlowPart dispatch）

- [ ] T009 [P] [US1] 更新 `projects/game/agent/src/tools/mouse_move/mouse-move.ts`：读取 `(config as { toolCall?: { id?: string } })?.toolCall?.id`，调用 `bridge.dispatch({ mouseMove: { xPx, yPx } }, toolCallId, signal)`（`Part`→`FlowPart` 类型）；返回值暂仍为 `buildResultBlocks(result)`（US2 再改为 ToolMessage）。同步更新相邻测试。依据 `contracts/tool-dispatch-contract.md` §2。
- [ ] T010 [P] [US1] 更新 `projects/game/agent/src/tools/mouse_click/mouse-click.ts`：同 T009（`mouseClick` + `click_type`→action 映射不变）。
- [ ] T011 [P] [US1] 更新 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`：**仅做类型适配**——`Part`→`FlowPart` 类型标注、`bridge.dispatch(part)`→`bridge.dispatch(flowPart)`；**保留**当前有状态逻辑（5 工具、validation、`saolei_update`、`pushResult` 调用），US4 阶段（US3）再做语义重写。同步更新 `saolei-mcp.test.ts` 中因类型变更失败的断言。

### Desktop — Go（recvLoop 分支 + 结果镜像移除）

- [ ] T012 [US1] 更新 `projects/game/desktop/app.go` 的 `recvLoop`（line 650）：按新 `payload` 分支——`message_parts` → `chatStreams.Append`（前端渲染）；`flow_parts` → 遍历 `FlowPart`：操作类（`mouse_move`/`mouse_click`/`keyboard_press`/`mouse_move_and_click`）经 `handleInboundOperation` 执行且**不** Append 到 chatStreams（FR-005），信号类（`wait`/`warn`/`status`）Append 到 chatStreams 供前端反应（wait 清 typing、warn 警告、status no-op）。依据 `data-model.md` §9、`research.md` D9。
- [ ] T013 [US1] 更新 `projects/game/desktop/app.go` 的 `handleInboundOperation`（line 728）：**移除**两处 `chatStreams.Append(sessionID, resultFrame)`（debug 分支 line 759 与非 debug 分支 line 783）——结果仅经 `ws.SendFrame` 回传 agent（FR-010：截图来自 LLM tool result，非桌面镜像）；debug 分支保留 `holdAndRelease` + `SendFrame` 顺序。依据 `research.md` D8、`data-model.md` §8。
- [ ] T014 [P] [US1] 更新 `projects/game/desktop/view_model.go`：`MessageViewModel.Content` 现承载 `MessageParts`（protojson 序列化机制不变，确认 `protoToJSONMap` 对新 oneof 扁平化正确——`messagePart` 的 `toolCall`/`toolResult` 变体 camelCase 输出）。

### Desktop — 前端（类型 + 渲染）

- [ ] T015 [US1] 更新 `projects/game/desktop/frontend/src/api.ts`：拆分 `Part`→`MessagePart`/`FlowPart` 接口；新增 `ToolCallPart { toolId; name; argsJson }`；新增 `messagePartKind()` 与 `flowPartKind()`；`AgentFrame.payload` 形状改为 `messageParts?`/`flowParts?`；`Message.content` 类型改为 `MessageParts`；移除旧 `partKind` 中 `mouseMove`/`mouseClick`（操作不再在 MessagePart 中）。
- [ ] T016 [US1] 更新 `projects/game/desktop/frontend/src/App.svelte`：`handleAgentFrame` 按 `messageParts`/`flowParts` 分支——`messageParts` 走 `handleMessageParts`（原 `handleContentPayload` 改名/调整）；`flowParts` 中 `wait`→`processing=false`、`warn`→警告条目、`status`→no-op、操作类忽略；`heldToolIds` 继续透传 `ChatView`。
- [ ] T017 [US1] 更新 `projects/game/desktop/frontend/src/components/ChatView.svelte`：仅渲染 `MessagePart`；新增 **tool 气泡按 `tool_id` 演化**逻辑——`tool_call` 创建气泡（显示 name + `argsJson`），同 `tool_id` 的 `tool_result` **原地合并**到该气泡（状态 + message + 截图），不新增条目；移除 `mouseMove`/`mouseClick` 操作卡片分支；Confirm 控制保留在气泡上（US4 调整其锚定）。依据 `data-model.md` §5。

**Checkpoint**: 新模型编译+单测通过；US1 独立测试通过（tool call 单气泡并原地更新；无操作/信号出现在对话；live 与重进后一致）。

---

## Phase 3: User Story 2 — 工具结果在 history 中显示真实状态（无伪 "failed"）(Priority: P1)

**Goal**: 真实 `ToolResultStatus` 经 `ToolMessage.additional_kwargs.toolResultStatus` 穿透 `MemorySaver` checkpoint，`ListMessages` 直读真实状态；移除文本推断；缺省为 neutral 而非 FAILED。

**Independent Test**: 跑一轮含 ≥2 个成功 + 1 个真实失败的工具操作；离开并重进；成功仍读 succeeded、失败仍读 failed；状态不可用时显示 neutral。

**Depends on**: US1（ListMessages 已在读 `additional_kwargs.toolResultStatus`，缺省 UNSPECIFIED；本 phase 让 mouse 工具真正填入真实值）。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：`style/javascript.md`（**仅含 §测试**：`vitest_test` 宏、Mock/DI 约定；无通用 TS 编码规范）。
- **官方文档**：
  - Google TypeScript Style Guide（通用 TS 编码规范基准）— https://google.github.io/styleguide/tsguide.html
  - LangChain JS tool calling（`ToolMessage` 结构与 `additional_kwargs`）— https://js.langchain.com/docs/how_to/tool_calling/
  - LangChain JS persistence（`MemorySaver`）— https://js.langchain.com/docs/concepts/persistence/
- **技术文章**：无。

- [ ] T018 [US2] 更新 `projects/game/agent/src/tools/shared/result-blocks.ts`：新增 `buildToolResultMessage(result: OperationResult, toolCallId: string | undefined, name: string): ToolMessage`——返回 `new ToolMessage({ content: buildResultBlocks(result), tool_call_id: toolCallId ?? "", name, additional_kwargs: { toolResultStatus: result.status } })`；保留 `buildResultBlocks` 作为 content 构造器。新增/更新 `result-blocks` 的相邻测试，覆盖 SUCCEEDED/FAILED 与 `tool_call_id` 透传。依据 `contracts/tool-dispatch-contract.md` §3、`research.md` D4（含 T002 spike 已验证的 round-trip）。
- [ ] T019 [P] [US2] 更新 `projects/game/agent/src/tools/mouse_move/mouse-move.ts`：返回值由 `buildResultBlocks(result)` 改为 `buildToolResultMessage(result, toolCallId, "mouse_move")`（依赖 T018）。
- [ ] T020 [P] [US2] 更新 `projects/game/agent/src/tools/mouse_click/mouse-click.ts`：同 T019（`"mouse_click"`）。

**Checkpoint**: mouse 工具结果在 history 中显示真实状态；US2 独立测试通过（成功/失败/缺省 neutral，无文本推断）。

---

## Phase 4: User Story 3 — Saolei MCP 无状态化（无格子状态、无校验、无 saolei_update）(Priority: P1)

**Goal**: saolei MCP 仅暴露 4 个无状态工具（`saolei_init`/`saolei_click`/`saolei_flag`/`saolei_chord_click`），纯 dispatch-and-return；移除 `saolei_update`、`GameState`、validation、operate-then-update 交替；内置 skill 同步更新。

**Independent Test**: 配置 saolei profile；跑一轮 `saolei_init` → `saolei_click` → 再 `saolei_click`（中间无 update）；确认仅 4 工具、第二次 click 被接受、`saolei_update` 不存在。

**Depends on**: US1（`FlowPart` 类型 + `bridge.dispatch(flowPart, toolId)`）、US2（`buildToolResultMessage` 用于状态承载）。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：`style/javascript.md`（**仅含 §测试**：`vitest_test` 宏、Mock/DI 约定；无通用 TS 编码规范）。
- **官方文档**：
  - Google TypeScript Style Guide（通用 TS 编码规范基准）— https://google.github.io/styleguide/tsguide.html
  - MCP TS SDK `McpServer.registerTool` — https://github.com/modelcontextprotocol/typescript-sdk （`@modelcontextprotocol/sdk` ^1.29.0）
  - LangChain JS tool calling（`ToolMessage` 用于 saolei 结果承载）— https://js.langchain.com/docs/how_to/tool_calling/
- **技术文章**：无。
- **前序 feature 契约（本 phase 实现需要阅读其内容）**：
  - `specs/018-saolei-mcp/contracts/proto-operation-contract.md`（操作 Part 字段，desktop-facing 契约不变）

- [ ] T021 [US3] 重写 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`：`createSaoleiMcpServer(bridge)`（删除 `initialState` 参数与返回的 `state`）仅注册 4 个无状态工具——`saolei_init`（无参，dispatch `KeyboardPressPart{F2}`）；`saolei_click`/`saolei_flag`/`saolei_chord_click`（`{x,y}`，经 `geometry.center` 算像素后 dispatch `MouseMoveAndClickPart{WINDOW_MESSAGE, 对应 click action}`）；每个工具返回 `buildToolResultMessage(result, toolCallId, name)`（依赖 US2 T018）；**移除** `saolei_update`、`pendingUpdate`/`lastOp` 交替、所有 validation 调用、`UPDATE_STATUS_ENUM`、`STATUS_FAILED`/`pushResult` 相关引用。同步重写 `saolei-mcp.test.ts`：断言仅 4 工具、`saolei_init` 无 width/height、连续两次 `saolei_click` 均成功 dispatch、各工具 dispatch 的 proto 操作 Part 字段不变。依据 `contracts/tool-dispatch-contract.md` §6、`data-model.md` §7。
- [ ] T022 [P] [US3] 删除 `projects/game/agent/src/mcp/saolei/game-state.ts`、`projects/game/agent/src/mcp/saolei/validation.ts`、`projects/game/agent/src/mcp/saolei/validation.test.ts`（无消费者）；运行 `bazel run //:gazelle projects/game/agent` 清理 `BUILD.bazel`。
- [ ] T023 [P] [US3] 更新 `projects/game/agent/src/mcp-host.ts`：`createSaoleiMcpServer(looked.bridge)`（line 86）去掉第二参数；`SaoleiMcpHandle` 去掉 `state` 字段（仅 `{ server }` 或直接返回 `McpServer`）；同步更新 `mcp-host.test.ts`。
- [ ] T024 [US3] 更新 `projects/game/agent/src/operation-bridge.ts`：**删除** `pushResult` 方法（line 281）与其专属 `FRAME_SENDER_SYSTEM` 常量（consumer-less，`saolei_update` 已移除）；确认 `dispatch`/`handleResult`/`registerSink`/`unregisterSink` 契约完好。同步更新 `operation-bridge.test.ts` 移除 `pushResult` 用例。依据 `contracts/tool-dispatch-contract.md` §5、`research.md` D7。
- [ ] T025 [P] [US3] 重写 `projects/game/agent/src/skill/saolei/SKILL.md`：描述 4 个工具与 top-left `(x,y)` 约定、阅读返回截图追踪棋盘；**移除** `saolei_update`、交替、cell-status 上报契约、validation 拒绝指引。依据 spec FR-022。

**Checkpoint**: saolei MCP 4 工具无状态、可连续调用；`saolei_update`/state/validation 已删；US3 独立测试通过。

---

## Phase 5: User Story 4 — Debug 模式 hold 重新锚定到新模型 (Priority: P2)

**Goal**: debug 开启时，desktop 仍在执行后/回传 agent 前 hold 结果；Confirm 控制出现在该 tool_call 的对话气泡上（经 `tool_id` 关联）；hold 期间气泡仅显示 tool_call（无截图），执行细节在日志可见；release 后气泡更新为结果。auto-continue 15min 与 agent 20min backstop 不变。

**Independent Test**: 开 debug；跑一轮含 tool 操作；Confirm 出现在该 tool_call 气泡上、agent 不前进直到确认；确认后气泡更新为结果；hold 期间日志可见执行细节。

**Depends on**: US1（tool_call 气泡 + `tool_id` 线程穿透；US1 T013 已移除结果镜像）。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：`style/golang.md`（desktop Go 规范）；`style/javascript.md`（**仅含 §测试**；无通用 TS 编码规范）。
- **官方文档**：Google TypeScript Style Guide（前端 .svelte/.ts 通用编码规范基准）— https://google.github.io/styleguide/tsguide.html ；其余沿用 022 既有 Wails/Svelte 模式，参考仓库内已阅读文件 `projects/game/desktop/app.go`、`projects/game/desktop/frontend/src/App.svelte`。
- **技术文章**：无。
- **前序 feature 契约（本 phase 实现需要阅读其内容）**：
  - `specs/022-desktop-debug-mode/contracts/debug-control-plane.md`（`SetDebugMode`/`ConfirmToolResult`/`game:debug:result-held`/`result-released` 方法与事件名，本 feature 不变）

- [ ] T026 [US4] 更新 `projects/game/desktop/app.go` 的 `handleInboundOperation` debug 分支（line 756）：确认 US1 T013 后该分支为 `holdAndRelease(toolID); SendFrame`（无 chatStreams 镜像）；`holdAndRelease` 仍 emit `game:debug:result-held { toolId }`（tool_id 来自 FlowPart 操作，即 LangChain tool_call.id，与 tool_call 气泡一致——FR-024）；如需调整日志使执行 outcome（操作+状态，非截图）在 hold 期间可达（FR-011），补充 `logger.Debug` 条目。
- [ ] T027 [US4] 更新 `projects/game/desktop/frontend/src/components/ChatView.svelte` 与 `projects/game/desktop/frontend/src/App.svelte`：Confirm 控制锚定到 **tool_call 气泡**（`heldToolIds.has(bubble.tool_id)` 时在 `[ call ]` 气泡上渲染 Confirm，不再依赖 tool_result 气泡）；hold 期间气泡仅显示 tool_call（截图在 `tool_result` 到达后才出现——自然成立，因结果未 release 前 agent 不产 tool_result）。依据 `data-model.md` §5 Debug Confirm anchoring、spec C12。
- [ ] T028 [US4] 验证 FR-027（debug 关闭时结果立即回传 agent，无 hold/无 Confirm）与 FR-026（15min auto-continue / 20min backstop 不变）；如 `debugHoldTimeout`/`DISPATCH_TIMEOUT_MS` 未被前序 phase 改动则无需变更，仅确认行为。

**Checkpoint**: debug hold 锚定到 tool_call 气泡；US4 独立测试通过（Confirm 在 tool_call 气泡、agent 阻塞至确认、确认后更新、日志可见、debug off 立即回传）。

---

## Phase 6: Polish & 大型测试验收（宪章原则 VI）

**Purpose**: 跨故事的大型测试更新与执行验收。agent 是服务型应用，大型测试 MUST 实际执行（`guitar run`）且全部用例通过，仅构建检查不构成验收。

### 文档清单（本 phase 编码前必读，宪章原则 V）

- **代码规范文档**：`style/large_test.md`（大型测试组织：按模块拆分、复用既有测试计划 YAML、`guitar run` 执行、`pkg/testtool` 读环境变量）；`style/golang.md`（`*_test.go` 表驱动/given-when-then）。
- **官方文档**：无（`guitar` / `testplan` SKILL 提供执行指引）。
- **技术文章**：无。
- **SKILL**：执行大型测试前加载 `testplan` SKILL（`.opencode/skills/testplan/SKILL.md`）。

- [ ] T029 [P] 更新 `projects/game/testplan/` 下既有大型测试（按模块组织，不为本 feature 新建 YAML——`style/large_test.md`）：
  - `agent_saolei_test.go`：改为 4 工具无状态流（init→click→click 连续，无 update），断言 dispatch 的操作 Part 字段不变、结果状态穿透；
  - `agent_operation_test.go`：改为断言 tool_call/tool_result MessageParts 与携带的真实状态；
  - `agent_checkpoint_test.go`：改为 leave/re-enter 后 `ListMessages` 真实状态保持（成功仍 succeeded、失败仍 failed、缺省 neutral）；
  - `agent_dialog_test.go`：改为断言 tool_call 渲染 name+args、操作 FlowPart 不在 `Message.content`；
  - 复用 `helpers_test.go` 既有构造/断言，不复制 helper。依据 `quickstart.md` Scenario 5–7、`style/large_test.md`。
- [ ] T030 更新 `projects/game/testplan/system_test.yaml`：确认 `agent-saolei`/`agent-operation`/`checkpoint-resume`/`agent-dialog` suite 引用更新后的 case binary（通常无需新增 suite；若 deploy 拓扑无根本变化不得新建独立 YAML）。
- [ ] T031 通过 `testplan` SKILL 执行大型测试验收：`guitar run projects/game/testplan/system_test.yaml`，完成部署→测试→清理闭环；**所有用例 MUST 全部通过**（failed/flaky 即验收未通过，修复后重跑至全绿）。仅 `bazel build` 测试 target 不构成验收（宪章原则 VI v1.3.0）。

**Checkpoint**: 大型测试全部通过；feature 验收完成。

---

## Dependencies & Execution Order

### Phase 依赖

- **Phase 1 (Setup)**: 无依赖，立即开始。两个 spike 证实 LangChain 假设；若失败须修订设计。
- **Phase 2 (US1, MVP 主轴)**: 依赖 Phase 1。proto 变更是 clean break，US1 的 checkpoint 是新模型首个编译+单测通过的构建；**阻塞** US2/US3/US4。
- **Phase 3 (US2)**: 依赖 US1（`ListMessages` 已读 `additional_kwargs.toolResultStatus`；mouse 工具已在 US1 用 `toolId` dispatch）。
- **Phase 4 (US3)**: 依赖 US1（`FlowPart` 类型）+ US2（`buildToolResultMessage` 用于 saolei 结果承载）。
- **Phase 5 (US4)**: 依赖 US1（tool_call 气泡 + `tool_id` 线程；US1 T013 已移除桌面结果镜像）。**可与 US2/US3 并行**（不同文件：US4 改 desktop app.go/前端，US2/US3 改 agent）。
- **Phase 6 (Polish/验收)**: 依赖 US1–US4 完成。

### 为何没有单独的 "Foundational" phase

proto 内容模型拆分（`contracts/content-model-contract.md`）是 clean break（spec C2）：`Part`/`PartBlock` 被删除，所有消费方必须同时迁移才能恢复编译。因此 proto 变更与全部消费方迁移不可分（单独 proto phase 无法通过编译门禁），统一归入 US1（主轴）。US1 即 foundational。

### Saolei-mcp.ts 的两次触碰（已知，可接受）

US1 T011 对 `saolei-mcp.ts` 做最小类型适配（`Part`→`FlowPart`，保留有状态逻辑），US3 T021 做语义重写（无状态化）。这是按优先级（US1 先于 US3）带来的少量重做，仅限类型标注层面，可接受。若希望避免，可将 US3 提前到 US1 之前（US3 在旧 proto 上即可独立完成单元验收），但会改变 MVP 交付顺序。

### Within US1 任务依赖

- T003 (proto) → T004 (regen) → 所有消费方 task。
- T005 (bridge) ∥ T006 (llm)（不同文件，均依赖 T004）。
- T007/T008 (handler) 依赖 T005/T006。
- T009/T010/T011 (tools) 依赖 T005；三者不同文件可并行。
- T012 → T013（同文件 `app.go`）。
- T014 (view_model.go) ∥ agent tasks。
- T015 (api.ts) → T016 (App.svelte) → T017 (ChatView.svelte)。

### Parallel Opportunities

- Phase 1: T001 ∥ T002（不同 spike 文件）。
- US1: T005 ∥ T006；T009 ∥ T010 ∥ T011；T014 ∥ (agent tasks)。
- US2: T019 ∥ T020（两个 mouse 工具）。
- US3: T022 ∥ T023 ∥ T025（删文件/改 host/改 skill 互不依赖）；T021→T024（pushResult 删除须在 saolei_update 移除后）。
- 跨故事：US4 可与 US2/US3 并行（US4 改 desktop，US2/US3 改 agent）。

---

## Parallel Example: US1

```text
# proto + regen 先行（串行）：
T003 (game.proto) → T004 (gazelle regen)

# 然后 agent 侧并行：
T005 (operation-bridge.ts)  ∥  T006 (llm.ts)
T009 (mouse-move.ts)  ∥  T010 (mouse-click.ts)  ∥  T011 (saolei-mcp.ts 适配)   # 均依赖 T005

# handler 依赖 bridge+llm：
T007 (handler Connect)  →  T008 (handler ListMessages)

# desktop/前端：
T012 (recvLoop) → T013 (handleInboundOperation)
T014 (view_model.go)                      # 与 agent 侧并行
T015 (api.ts) → T016 (App.svelte) → T017 (ChatView.svelte)
```

---

## Implementation Strategy

### MVP First (US1 + US2)

1. Phase 1: Setup spikes（验证 LangChain 假设）。
2. Phase 2: US1（proto 拆分 + 对话渲染主轴）——首个新模型编译+单测通过构建。
3. Phase 3: US2（真实状态穿透）——修复 item 3（history 伪 failed）。
4. **STOP and VALIDATE**：US1+US2 覆盖用户 item 2（tool 展示）+ item 3（状态 bug）；手动 + 大型测试验证。

### Incremental Delivery

1. Setup + US1 → 新模型主轴可用。
2. + US2 → history 状态正确（item 3 修复）。
3. + US3 → saolei 无状态化（item 1）。
4. + US4 → debug hold 适配新模型。
5. Phase 6 → 大型测试全绿验收。

### 关于 US3 提前的可选优化

US3（saolei 无状态化）在旧 proto 上即可独立完成单元验收，不依赖内容模型拆分。若希望最小化 `saolei-mcp.ts` 重做，可将 US3 提前到 US1 之前执行（US3 → US1 → US2 → US4），代价是 item 1 先于 item 2/3 交付。默认按优先级（US1 先）执行。

---

## Notes

- [P] = 不同文件、依赖已完成。
- [Story] 标签映射到 spec.md 用户故事。
- 每个代码 task 包含其编译 (`bazel build`) + 单测 (`bazel test`) + 相邻 `*.test.ts`/`*_test.go` 更新（宪章原则 IV）。
- proto 改动后 MUST `bazel run //:gazelle` 重新生成 `BUILD.bazel` 与类型。
- 大型测试验收 MUST 实际 `guitar run`（宪章原则 VI v1.3.0），仅构建不构成验收。
- 每个 phase 编码前 MUST 完整阅读该 phase "文档清单" 全部文档（宪章原则 V）。
