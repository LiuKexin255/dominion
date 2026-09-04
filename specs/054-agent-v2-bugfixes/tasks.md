# Tasks: agent-v2 对话呈现与游戏链路缺陷修复 + testplan 重构

**Input**: Design documents from `/specs/054-agent-v2-bugfixes/`（plan.md / spec.md / research.md / data-model.md / contracts/ / quickstart.md）

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Organization**: 按 user story 组织（依赖序：US2→US4 是 store 结构依赖链；US1 的排查验收按用户指令置于 testplan 验收后的手工验收 phase）。编译+单测为每次代码变更的一部分（constitution IV），测试编写并入各实现任务；大型测试验收与手工验收单列。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story
- 所有路径为仓库相对路径

---

## Phase 1: Setup（共享基础设施）

**Purpose**: dsh 依赖统一 catalog 治理（含 `third_party/dsh/core` 底座）+ 引入官方 token 依赖（US8 消费；依赖管理为仓库级操作）

### 文档清单

- **代码规范文档**：`style/javascript.md`（ESM/依赖约定）；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用基准）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/research.md` D4（引入方式与版本决策）、`survey/deepseek-harness-b1-bazel-packaging.md` §4.2（dsh 依赖治理：统一 catalog 管理，含底座闭包）

- [x] T001 dsh 依赖统一 catalog 管理（含 `third_party/dsh/core` 底座，无任何例外）：
  1. `pnpm-workspace.yaml` catalog 新增 22 个条目（rc 线精确版本 0.1.1-rc.2；cordis/schemastery 保持既有 range；cordis-plugin 家族与 node-addon-require-builtin 精确版本）：`@deepseek-ai/dsh-agent`、`dsh-agent-spine-demo`、`dsh-app-boot`、`dsh-home-paths`、`dsh-invariants`、`dsh-launch-environment`、`dsh-llm`、`dsh-llm-deepseek`、`dsh-llm-retry`、`dsh-scope`、`dsh-session`、`dsh-system-prompt`、`dsh-tools`、`dsh-client-ui-primitives`、`dsh-client-ui-theme`（新增，消费在 Phase 10）、`cordis`（`^4.0.1`）、`cordis-plugin-group`（`1.0.1`）、`cordis-plugin-include`（`1.0.6`）、`cordis-plugin-loader`（`1.0.2`）、`cordis-plugin-timer`（`1.1.3`）、`schemastery`（`^3.18.1`）、`node-addon-require-builtin`（`0.1.5`）；
  2. 以下 manifest 的直接依赖版本声明改为 `catalog:`：`third_party/dsh/core/`、`projects/game/web/frontend/`、`projects/game/agent_v2/`、`experimental/dsh/demo/agent/`、`common/js/dsh-plugins/saolei-loop/`、`common/js/dsh-plugins/saolei/`、`common/js/dsh-plugins/llm-glm/`、`common/js/dsh-plugins/desktop-bridge/`；
  3. 经 `bazel run @pnpm -- --dir /mnt/code/dominion install` 更新 lock（禁止手改 `pnpm-lock.yaml`）；核对迁移前后解析版本一致（rc 线 0.1.1-rc.2 / cordis 4.0.1 / cordis-plugin 1.0.x 线 / schemastery 3.18.1 / node-addon 0.1.5）；`third_party/dsh/core/version.ts` 的 DSH_CORE_SNAPSHOT 语义不变；
  4. 审计门禁：全仓 manifest 零直接 `@deepseek-ai/*` 与 `node-addon-require-builtin` 版本声明；`bazel build //third_party/dsh/... //projects/game/... //common/js/dsh-plugins/... //experimental/dsh/...` 通过；`closure_audit_test`（声明面名称集校验）不受影响

**Checkpoint**: 依赖迁移完成、审计通过，`bazel build //projects/game/web/... //third_party/dsh/...` 通过

---

## Phase 2: Foundational（协议扩展，阻塞全部 US 的服务端/前端面）

**Purpose**: `agent_v2.proto` 扩展与 codegen——US2（step）/US4（固化呈现）/US5（cancel）/US1（连接状态）的协议基础

### 文档清单

- **代码规范文档**：`style/api.md`；[AIP-136 Custom methods](https://google.aip.dev/136)（`:cancel` 形态）；[AIP-140 Field names](https://google.aip.dev/140)；`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（server.ts 的 Cancel 编译适配）；`style/golang.md`；[Google Go Style Guide](https://google.github.io/styleguide/go/guide)（proxy 测试替身的 Cancel 编译适配）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md`、`specs/054-agent-v2-bugfixes/data-model.md` §1、`specs/051-agent-v2-dsh-migration/contracts/agent-api.md`（051 基线，方法命名/注释/HTTP 注解延续其形态）

- [x] T002 扩展 `projects/game/agent_v2.proto`：`BlockStartEvent`/`BlockDeltaEvent`/`BlockEndEvent` 各增加 `int32 step` 字段；`TurnStatus` 增加 `TURN_STATUS_CANCELED = 4`；新增 `Cancel` RPC（`:cancel` 自定义方法，POST `{name=templates/*/sessions/*/agent}:cancel`，请求仅 name、响应空对象）及 `CancelRequest`/`CancelResponse` 消息；`Agent` 消息增加 `bool desktop_connected` 字段；Service/Method 注释含 Prefix Path 与语义说明（延续 051 既有注释风格）；codegen 与编译验证：`bazel build //projects/game/agent_v2/... //projects/game/gateway/... //projects/game/proxy/...`（生成类型进 `projects/game/agent_v2/agent_v2_types/`；gateway 零改动——grpc-gateway 注册随 codegen 自动携带 `:cancel` 路由；新增 RPC 另需两处编译适配，行为不变（`:cancel` 端到端仍 Unimplemented，语义落地在 Phase 6）：`projects/game/agent_v2/src/server.ts` 的 `buildAgentHandlers` 补显式 UNIMPLEMENTED `Cancel` 条目（proto-loader-gen-types 的 handler 接口方法为必填属性）、`projects/game/proxy/handler/agent_test.go` 的 `fakeAgentClient` 补 `Cancel` 方法（记录请求/配置错误/默认成功，ListAgentMessages 同构）——依据 `specs/054-agent-v2-bugfixes/revisions/phase2-proxy-cancel.md` §0.2）

**Checkpoint**: 协议扩展就绪且全链编译通过（无行为变更）

---

## Phase 3: User Story 2 - 按步骤分段呈现 (Priority: P1)

**Goal**: 流式按 step 分段依次独立呈现；回合完成后最终答案独立、此前步骤折叠为可展开"思考过程"区；回填与流式形态一致（FR-004/005/006）

**Independent Test**: `bazel test //projects/game/agent_v2/... //projects/game/web/frontend/...`——step 透传、store 分组与多消息投影、分段渲染与折叠/展开组件用例全绿

### 文档清单

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[dsh-client-ui-chat README（npm，含 "Turn Process Folding" 章节）](https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-chat)（折叠规则的行为基线：流式全展开/turn 结束折叠/无最终答案不折叠）；源仓库 [deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/contracts/web-ui.md` §2、`specs/054-agent-v2-bugfixes/data-model.md` §5.1、`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §4（事件模型基线）、`specs/049-agent-v2-dsh-init/contracts/web-frontend.md`（store reducer 不变量基线）

- [x] T003 [P] [US2] `projects/game/agent_v2/src/history.ts`：chunk→ChatEvent 映射透传 step——`BlockStartEvent`/`BlockDeltaEvent`/`BlockEndEvent` 携带 `ActiveTurn` 已跟踪的 step 序号（`data.step`，缺失置 0）；`projects/game/agent_v2/src/history.test.ts` 增加 step 透传与缺省用例
- [x] T004 [P] [US2] `projects/game/web/frontend/src/store/chat.ts`：`LiveTurn` 改造为 `steps: StepDraft[]`（块事件按 `event.step` 路由分组、缺 step 归组 0、下一 step 到达即置前一 step settled）；`turn_end{COMPLETED}` 将 steps 依序投影为**多条** `HistoryMessage`（废除整回合合并）；`turn_end{ERROR/ABORTED}` 行为此阶段暂保持现状（US4 处理）；`store/chat` 测试更新（step 分组/退化/多消息投影/tool_result 跨 step 按 tool_id 关联）
- [x] T005 [US2] `projects/game/web/frontend/src/components/ChatView.tsx`：按 steps 分段依次独立渲染（每 step 一个分段容器，步骤内 think→ReasoningRow、toolCall→ToolCard、text→正文分类分列；空 text 跳过语义保留）；`ChatView` 组件测试（流式分段依次呈现、分类分列、用户消息不变）
- [x] T006 [US2] `projects/game/web/frontend/src/components/ChatView.tsx`：回合完成后的折叠——最终答案判定（最后一个含非空 text 块且无 tool-call 块的 step）独立呈现，此前 steps 折叠为"思考过程"摘要区（步骤/工具计数、点击展开、手动展开页面会话内保持）；无最终答案的回合全可见不折叠（此阶段以 ERROR 外的既有终态覆盖，ERROR/CANCELED 专属呈现随 US4/US5 完善）；回填默认折叠态；组件测试（折叠/展开/计数/回填默认态）

**Checkpoint**: 流式分段与回填一致，既有对话用例零回归

---

## Phase 4: User Story 4 - 失败回合内容保留 (Priority: P1)

**Goal**: 回合无论成功/失败/终止，已产出内容固化进历史并保留呈现，刷新回填可见（FR-012/013/014）

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/... //projects/game/web/frontend/...`——driver interrupted 固化、HistoryMessage.interrupted 记录与 List 透出、store ERROR 保留与尾步标记、回填一致性（含失败回合不折叠）用例全绿

### 文档清单

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；`style/api.md`；[AIP-140 Field names](https://google.aip.dev/140)（T009b 的 HistoryMessage 字段扩展）
- **官方文档**：[dsh-client-ui-chat README（npm）](https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-chat)（Turn Process Folding 规则——"a closed Turn with no final answer keeps all process evidence visible" 与 interrupted 呈现基线）
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md` §2/§6、`specs/054-agent-v2-bugfixes/contracts/web-ui.md` §2.1–2.2、`specs/054-agent-v2-bugfixes/data-model.md` §2/§5.2、`specs/054-agent-v2-bugfixes/research.md` D2（`'assistant-step'` running/settled/interrupted 三态语义——源自包内 `chat-nodes.d.ts`，README 不含该枚举）/D5/D6、`specs/054-agent-v2-bugfixes/revisions/phase4-failed-turn-folding.md`（T009b 补充设计：失败回合折叠裁定与 interrupted 信号传递）

- [x] T007 [P] [US4] `common/js/dsh-plugins/saolei-loop/src/driver.ts`：ERROR 路径 interrupted 固化——LLM 流失败 catch（非 abort）与 finish error 抛 `LlmError` 前，assembler 有部分内容时 append `assistant/message`（`interrupted: true`，仅含已产出块，与既有 abort 路径同构）；`driver` 单测（流失败/finish error 两路径的固化断言、空 assembler 不 append、工具执行异常路径"assistant/message 已先 append"的既有顺序验证——research D5）
- [x] T008 [P] [US4] `projects/game/web/frontend/src/store/chat.ts`：终态保留——`turn_end{ERROR}` 不再丢弃 live：已 settled 的 steps 保留并入本地历史，未完成尾块按 interrupted 呈现，错误提示独立；`turn_end{ABORTED}` 清空语义保持不变；store 测试（ERROR 保留/ABORTED 清空/尾块 interrupted）
- [x] T009 [US4] `projects/game/web/frontend/src/components/ChatView.tsx` + store：回填一致性——无最终答案回合回填后全可见不折叠；历史中 status 仍为 RUNNING 且无 result 的陈旧工具块按中断终态呈现（回填侧推导）；组件测试（注入失败的回填可见性、RUNNING 陈旧块终态）
- [x] T009b [US4] 失败回合折叠缺口修复（依据 `specs/054-agent-v2-bugfixes/revisions/phase4-failed-turn-folding.md` §3）：`projects/game/agent_v2.proto`：`HistoryMessage` 增加 `bool interrupted = 5`（注释含 FR-005 语义；codegen 验证 `bazel build //projects/game/agent_v2/... //projects/game/gateway/... //projects/game/proxy/...`，gateway/proxy 零源码改动）；`projects/game/agent_v2/src/history.ts`：`AssistantMessageEvent.data` 补 `interrupted?: boolean`、`SessionHistory.appendAssistant` 增加 interrupted 参数（仅 true 落字段）、`onSessionEvent` 透传；`projects/game/web/frontend/src/api/conversation.ts`：`HistoryMessage` 补 `interrupted?: boolean`；`projects/game/web/frontend/src/store/chat.ts`：`stepsToHistory` 增加 interrupted 参数——ERROR 投影时仅尾步消息标记 `interrupted: true`（CANCELED 同构，Phase 6 T014 复用）；`projects/game/web/frontend/src/components/ChatView.tsx`：最终答案判定排除 interrupted 消息（`isFinalAnswer` 改收 message）；`common/js/dsh-plugins/saolei-loop/src/driver.ts`：finish-error waterfall 后 abort 窗口的 interrupted 固化补齐（revision §7-2）；契约文档同步（data-model.md §1.5/§2/§5.2、contracts/agent-api-changes.md §6、contracts/web-ui.md §2.1/2.2/§8，文本见 revision §5）；测试：`history.test.ts`（append 记录+TurnCollector 透出）、`chat.test.ts`（尾步标记+更新既有 :490/:514 断言）、`ChatView.test.tsx`（部分文本尾 ERROR 回合不折叠——本地/回填两路径、COMPLETED 折叠零回归）、`driver.test.ts`（waterfall abort 固化）

**Checkpoint**: 注入失败的回合内容"看过不再丢"，成功回合回填零回归

---

## Phase 5: User Story 3 - markdown 渲染 (Priority: P1)

**Goal**: 正文与思考展开体 markdown 渲染，工具棋盘等宽预格式化（FR-008/009/010/011）

**Independent Test**: `bazel test //projects/game/web/frontend/...`——GFM 渲染、流式稳定、棋盘对齐用例全绿

### 文档清单

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[dsh-client-ui-primitives README（npm，含 "Markdown rendering" 章节）](https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-primitives)（MarkdownText 能力边界：GFM+KaTeX、流式增量、安全渲染、不完整片段容错——本任务只消费不重造）
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/contracts/web-ui.md` §3

- [x] T010 [P] [US3] `projects/game/web/frontend/src/components/ChatView.tsx`：agent 正文 `MessageText` 替换为 `MarkdownText`（import 自 `@deepseek-ai/dsh-client-ui-primitives`）；`projects/game/web/frontend/src/components/ReasoningRow.tsx`：展开体同样替换为 `MarkdownText`；组件测试（标题/列表/粗体/行内代码/代码块/表格/链接渲染、无原始符号裸露、流式增量稳定、不完整片段不崩溃、用户消息保持纯文本）
- [x] T011 [P] [US3] `projects/game/web/frontend/src/components/ToolCard.tsx`：结果呈现由 JSON 字符串字面量（`JsonBlock`）改为预格式化等宽文本呈现（`<pre>`/等宽样式，保持棋盘坐标标尺对齐，不 markdown 化）；组件测试（多行棋盘对齐、行列不错位）

**Checkpoint**: markdown 缺陷清零，既有 ToolCard 用例更新通过

---

## Phase 6: User Story 5 - 终止按钮 (Priority: P2)

**Goal**: 对话页终止运行中回合——优雅终止、排队消息落地、会话立即可用（FR-015/016/017）

**Independent Test**: `bazel test //projects/game/agent_v2/... //projects/game/proxy/... //projects/game/web/frontend/...`——cancel 服务端全语义（agent_v2 + proxy 转发）与前端编排用例全绿

### 文档清单

- **代码规范文档**：`style/api.md`；[AIP-136 Custom methods](https://google.aip.dev/136)；`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；`style/golang.md`（T013b proxy Go 转发）；[Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` 引用基准）
- **官方文档**：[dsh-client-ui-conversation README（npm）](https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-conversation)（composer 运行中 Stop 形态参考；**注意**：官方 cancel 保留 pending Queue，本实现按用户裁定为排队落地——差异见 contracts/agent-api-changes.md §2）
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md` §2/§3、`specs/054-agent-v2-bugfixes/contracts/web-ui.md` §4、`specs/054-agent-v2-bugfixes/data-model.md` §1.2/§1.3/§3、`specs/054-agent-v2-bugfixes/research.md` D7、`specs/054-agent-v2-bugfixes/revisions/phase2-proxy-cancel.md`（T013b 设计）、`specs/051-agent-v2-dsh-migration/contracts/agent-api.md` §2/§3（两跳错误表与 NOT_FOUND/FAILED_PRECONDITION 分层基线）

- [x] T012 [US5] `projects/game/agent_v2/src/session.ts`：实现 Cancel——终止在途回合（经 `TurnCollector.abort()` 缝，cancel 传播取消 LLM 流与在途工具；在途流发 `turn_end{TURN_STATUS_CANCELED}`）、排队消息落地（清空待处理队列不触发回合，历史 user 消息保留）、幂等（无回合无队列成功 no-op）、终止后新 Send 立即可用；`session.test.ts` 用例（终止传播[≤5s 内收到 turn_end{CANCELED}，SC-004]/落地/幂等/后续 Send/与 Update 并发）
- [x] T013 [US5] `projects/game/agent_v2/src/server.ts`：注册 Cancel handler（路径解析与未物化错误语义同 Send 前置错误族）；handler 单测
- [x] T013b [P] [US5] `projects/game/proxy/handler/agent.go`：新增 `Cancel` 转发方法——GetAgent 同构（`parseAgentResourceName` → `lookupAgentOwner`（只查不分配）→ `agentV2Conn` → `newAgentClient.Cancel` → `propagateAgentError`），无本地业务语义（cancel 语义在 T012 的 agent_v2），并同步 agent.go 包/类型注释的方法清单；`projects/game/proxy/handler/agent_test.go`：Cancel 用例（正常转发/INVALID_ARGUMENT 表驱动含未知 template/无 owner NOT_FOUND 且不分配/owner 实例不可达 UNAVAILABLE/下游状态原码传播）；按 `specs/054-agent-v2-bugfixes/revisions/phase2-proxy-cancel.md` §5 在 `specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md` §3 补 proxy 路由说明一行；仅依赖 Phase 2，可与 T012/T013 并行
- [x] T014 [US5] `projects/game/web/frontend/src/api/agent.ts`（Agent 单例面，与 GetAgent 客户端同归属）：新增 `cancelAgent(session)` 客户端（POST `{session}/agent:cancel`）；`projects/game/web/frontend/src/components/ChatView.tsx`：composer 区终止按钮（仅 live 运行中可见/可用、重复点击防抖、请求失败错误呈现）；`store/chat.ts` + `App.tsx`：`TURN_STATUS_CANCELED` 终态呈现（保留语义复用 US4、终态标识"已终止"非错误文案）、取消后队列 chip 移除、落地 user 消息呈现；组件测试（可见性/点击编排/CANCELED 终态/排队落地/空闲不可触发）

**Checkpoint**: 终止全链可用（服务端语义 + 前端编排）

---

## Phase 7: User Story 1（能力半）- 桌面连接状态可见 (Priority: P1)

**Goal**: 桌面连接状态查询与对话页呈现（FR-002；FR-001 排查修复在 Phase 12 手工验收闭环）

**Independent Test**: `bazel test //common/js/dsh-plugins/desktop-bridge/... //projects/game/agent_v2/... //projects/game/web/frontend/...`——查询面/GetAgent/前端三态用例全绿

### 文档清单

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md` §4、`specs/054-agent-v2-bugfixes/contracts/web-ui.md` §5、`specs/054-agent-v2-bugfixes/data-model.md` §1.4/§5.3、`specs/054-agent-v2-bugfixes/research.md` D8

- [x] T015 [P] [US1] `common/js/dsh-plugins/desktop-bridge/src/index.ts`（及 `bridge.ts`）：服务接口新增 `isDesktopConnected(sessionName): boolean`（直读连接注册表）；插件单测（有/无连接、接管后状态）
- [x] T016 [P] [US1] `projects/game/agent_v2/src/server.ts`：GetAgent 响应填充 `desktop_connected`（经宿主持有的 bridge 服务实例）；单测（物化会话两态、未物化 404 路径不变）
- [x] T017 [US1] `projects/game/web/frontend/src/api/agent.ts`：GetAgent 响应类型扩展 `desktopConnected`；`projects/game/web/frontend/src/App.tsx`（ChatPanel 编排）：连接状态三态（connected/disconnected/unknown）——进入会话、send 前、turn 结束即时刷新 + 10s 轮询，GetAgent 404/失败降级 unknown；`ChatPanel` 头部（对话页顶部，契约 `specs/054-agent-v2-bugfixes/contracts/web-ui.md` §5）：状态指示呈现（文案+状态色）；组件测试（三态、轮询触发面、降级不显示已连接）

**Checkpoint**: 连接状态全链可见（排查 fr-001 的可观测性前提就绪）

---

## Phase 8: User Story 6 - 模型目录 (Priority: P2)

**Goal**: 目录覆盖 GLM Coding Plan 当前全部支持模型、默认为有效模型（FR-018）

**Independent Test**: `bazel test //projects/game/agent_v2/...`——目录/默认值/校验用例全绿

### 文档清单

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[GLM Coding Plan 套餐概览](https://docs.bigmodel.cn/cn/coding-plan/overview)（支持模型集合与自动切换规则）；[GLM Coding Plan 最新模型与调用配置](https://docs.bigmodel.cn/cn/coding-plan/latest-model.md)（`glm-5.3-flash` context window 与 1M 形态——实现前核实）
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md` §5、`specs/054-agent-v2-bugfixes/data-model.md` §4、`specs/054-agent-v2-bugfixes/research.md` D9（开放项验证点）

- [ ] T018 [US6] `projects/game/agent_v2/cordis.yml`：llm-glm `models` 改为 `glm-5.3`（contextWindow 1000000）与 `glm-5.3-flash`（contextWindow 以 latest-model.md 核实值为准，[1m] 后缀形态按文档判定）双条目，移除 `glm-5.2`；`projects/game/agent_v2/src/session.ts`：`DEFAULT_MODEL` 改 `GLM_MODEL || 'glm-5.3'`；相关单测/既有 models 目录用例更新（双模型、默认值、未知 id 拒绝零回归）

**Checkpoint**: 物化下拉双模型可选且校验同源

---

## Phase 9: User Story 7 - preset 独占编辑视图 (Priority: P2)

**Goal**: preset 新建/编辑为独占视图，无"可见但禁用"列表残留（FR-019/020）

**Independent Test**: `bazel test //projects/game/web/frontend/...`——视图切换矩阵用例全绿

### 文档清单

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/contracts/web-ui.md` §6

- [ ] T019 [US7] `projects/game/web/frontend/src/components/PresetsView.tsx`：FormMode 驱动独占视图切换（create/edit 期间列表不渲染；保存成功/取消返回列表；失败停留+内容不丢+错误呈现；编辑名称只读/新建可输入延续；正在编辑条目被删返回列表）；组件测试更新（进入/保存/取消/失败/竞态矩阵）

**Checkpoint**: preset 编辑中间态缺陷清零

---

## Phase 10: User Story 8 - token CSS 与菜单视觉 (Priority: P2)

**Goal**: 引入官方 token 表系统性修复组件视觉（FR-021/022/023）

**Independent Test**: `bazel test //projects/game/web/frontend/...`——Menu 卡片视觉断言与既有组件用例全绿；人工浏览器目验菜单弹出形态

### 文档清单

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[dsh-client-ui-theme README（npm）](https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-theme)（token sheets 清单与引入顺序：base→design-platform→scrollbar→gradient-shadow-text→shiki；dark 激活选择器形态——引入时以包内 `src/styles/*.css` 实读为准）
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/contracts/web-ui.md` §1/§7、`specs/054-agent-v2-bugfixes/research.md` D4

- [ ] T020 [US8] `projects/game/web/frontend/src/main.tsx` + `projects/game/web/frontend/src/theme.css`：按官方顺序 import `@deepseek-ai/dsh-client-ui-theme/src/styles/` 五个 CSS（vite 直接 import），以 sheets 实际选择器激活深色 token（`body[data-ds-dark-theme]` 或等效），**删除** `theme.css` 中手写的 `--dsw-*` 变量子集（token sheets 为唯一权威；`--app-*` 布局样式保留）；新增 `SessionList` Menu 卡片视觉断言（容器 token/计算样式存在）；核查既有使用组件（Button/Input/StateDot/ReasoningRow/ToolCard 等）无视觉回归；组件测试更新

**Checkpoint**: `···` 菜单以完整卡片视觉弹出，同类隐患系统性消除

---

## Phase 11: testplan 重构（用户指令②）

**Goal**: deploy 合并 + suite 归并，部署次数 7→1，执行时间显著下降（FR-024 载体重构）

**Independent Test**: 部署配置/YAML/target 无残留引用（`rg deploy_agent_v2_drop` 零命中）；`bazel build //projects/game/testplan/...` 通过（实际执行验证在 Phase 12）

### 文档清单

- **代码规范文档**：`style/large_test.md`（模块/suite 编排与反模式）、`style/golang.md`（表驱动/given-when-then/命名）；[Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` 引用基准）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/contracts/testplan.md`、`specs/054-agent-v2-bugfixes/research.md` D11、`specs/054-agent-v2-bugfixes/data-model.md` §6、`tools/test/guitar/README.md`（suite/case 串行执行语义出处，contracts/testplan.md §2 引证）

- [ ] T021 `projects/game/testplan/deploy_agent_v2.yaml`：既有 fake-desktop 实例更名 `fake-desktop-won`（env 不变）并新增 `fake-desktop-drop` 实例（同 artifact，env：`FAKE_DESKTOP_SESSION=desktop-e2e-drop`、`FAKE_DESKTOP_SCENARIO=progressive`、`FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS=3`）；删除 `projects/game/testplan/deploy_agent_v2_drop.yaml`；全仓引用核查无残留
- [ ] T022 `projects/game/testplan/system_test.yaml`：7 suite 归并为 1 suite `game-system`（cases 顺序：testplan_test → memory_test → web_test → agent_v2_conversation_test → agent_v2_preset_test → agent_v2_game_test → desktop_flow_test；suite/case description 按模块职能重述，移除对已删 deploy 与独立 disconnect suite 的引用）
- [ ] T023 `projects/game/testplan/agent_v2_game_disconnect_test.go` 用例并入 `projects/game/testplan/agent_v2_game_test.go`（测试函数迁移、绑定 `desktop-e2e-drop` session 不变）；删除 disconnect 文件；`projects/game/testplan/BUILD.bazel` 移除 `agent_v2_game_disconnect_test` target（`agent_v2_game_test` size 复核；`testplan_test` 保持 gazelle 默认名）+ gazelle 校验
- [ ] T024 `projects/game/testplan/agent_v2_conversation_test.go` 新增/更新用例（表驱动、given/when/then）：NDJSON 块事件 step 断言与回填每 step 一条、注入 LLM 失败的 ERROR 回合已产出内容回填可见（断言尾步 `HistoryMessage.interrupted=true` 透出）、`:cancel` 全语义（终止/CANCELED 终态/排队落地/幂等/后续 Send 可用）、GetAgent `desktop_connected`（有/无连接）；`projects/game/testplan/agent_v2_preset_test.go`：模型目录断言更新（glm-5.3/glm-5.3-flash/默认值/未知 id 拒绝）；helper 按需补充（复用 `agent_v2_helpers_test.go`，不复制）
- [ ] T025 [P] `projects/game/testplan/README.md`：执行预算与说明更新（单 suite 单部署、超时参数按实测校准）

**Checkpoint**: 编排重构完成、无残留引用、testplan targets 编译通过

---

## Phase 12: 最终验收（testplan 全量 + 手工验收 US1 + 反馈排查闭环）

**Purpose**: constitution VI 大型测试验收 + 用户指令的手工验收（正式部署配合验证 + 反馈排查）

### 文档清单

- **代码规范文档**：`style/large_test.md`（执行规范："FOR Agent: 使用 testplan SKILL 来执行大型测试"）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/054-agent-v2-bugfixes/quickstart.md`（§2 testplan 执行、§3 真实环境端到端步骤、§4 SC 对照）、`specs/054-agent-v2-bugfixes/research.md` D10（排查 playbook：signoz 取证路径与候选断点）、`specs/054-agent-v2-bugfixes/contracts/testplan.md` §5（验收锚点）

- [ ] T026 全量编译+单测终态门禁（区别于各任务内嵌的增量门禁）：`bazel build //...` 与 `bazel test //...` 全绿（含全部新单测/组件用例与既有零回归）
- [ ] T027 经 testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`：完成部署→测试→清理闭环，**全部用例通过**（任何 failed/flaky 修复后重跑直至全绿；不以 build 替代执行——constitution VI）；记录重构后执行时长（对照预算）
- [ ] T028 [US1] 手工验收（与用户配合，正式部署）：将修复部署至正式环境（`projects/game/deploy.yaml` 拓扑，真实 GLM 端点）；引导用户按 `specs/054-agent-v2-bugfixes/quickstart.md` §3 执行——desktop 连接正确 session 并绑定扫雷窗口、web 确认连接状态指示、物化 agent（模型目录双模型）、发起"开始一局扫雷"；**与用户核对验收点**：desktop 真实收到并执行操作、桌面扫雷游戏真实开始与推进、web 棋盘与桌面一致、分段/折叠/markdown/终止/刷新一致性、断开 desktop 后错误呈现；记录执行证据（desktop 执行记录/对话截图/trace id）
- [ ] T029 [US1] 反馈排查循环（如 T028 任一验收点不通过）：按 `specs/054-agent-v2-bugfixes/research.md` D10 playbook 用 signoz skill 拉取用户操作对应的 trace/log（`desktop connection attached`、dispatch 结果、turn_end 终态与 error），定位断点并修复（禁止凭推测改代码），修复后与用户复验该验收点；循环直至 §3 全部验收点通过
- [ ] T030 [US1] 验收证据与记录归档：按 `specs/054-agent-v2-bugfixes/quickstart.md` §4 SC 对照表逐项填写验证结果（SC-001 执行证据、SC-002~007 对应用例/记录），归档至 feature 目录（如 `specs/054-agent-v2-bugfixes/revisions/acceptance-2026-09.md`，含 trace id 与时长数据）

**Checkpoint**: 全部 SC 达成，手工验收通过（用户确认）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: 无依赖，立即开始
- **Phase 2 (Foundational)**: 依赖 Phase 1（无关，可并行启动 proto 编辑，但编译验证需依赖就绪）；**阻塞 Phase 3–8 的协议面**
- **Phase 3 (US2)**: 依赖 Phase 2——store steps 结构是 US4/US5 store 改动的**前置**（严格先行）
- **Phase 4 (US4)**: 依赖 Phase 3（T008 复用 T004 的 steps 结构）；T007（driver）仅依赖 Phase 2，可与 T003/T004 并行；T009b 为 review 补充任务（proto 仅涉 `HistoryMessage` 字段扩展，不重开已闭合的 Phase 2），依赖 T007–T009 顺序执行（driver/history/store/ChatView 同文件串行）
- **Phase 5 (US3)**: 依赖 Phase 3（T010 与 T005/T006 同文件 ChatView，串行避免冲突）
- **Phase 6 (US5)**: 依赖 Phase 2 + Phase 4（CANCELED 保留语义复用 US4 的终态保留）
- **Phase 7 (US1)**: 依赖 Phase 2；与 Phase 3–6 可并行（不同文件）
- **Phase 8/9/10 (US6/US7/US8)**: 各自仅依赖 Phase 1/2，相互可并行
- **Phase 11 (testplan)**: 依赖 Phase 2–8 全部完成（用例断言新行为）
- **Phase 12 (最终验收)**: 依赖全部 phase

### User Story Dependencies

- **US2 → US4 → US5**（store 结构链：steps → 终态保留 → CANCELED）
- **US1 能力半（Phase 7）独立**；US1 验收半（FR-001 排查修复）在 Phase 12 与用户配合闭环
- **US3/US7/US8/US6 相互独立**

### Parallel Opportunities

- Phase 2 完成后：Phase 3 的 T003/T004 并行；Phase 4 的 T007 与 Phase 3 并行；Phase 7 全部与 Phase 3–6 并行；Phase 8/9/10 三者并行
- Phase 5 内 T010/T011 并行（不同组件文件）；Phase 6 内 T013b（proxy Cancel 转发，仅依赖 Phase 2）可与 T012/T013 并行
- Phase 11 内 T025 与 T021–T024 并行

---

## Implementation Strategy

### MVP First（P1 全量：US2+US4+US3+US1 能力半）

1. Phase 1–2（依赖+协议）→ 2. Phase 3–5（呈现三连：分段/保留/markdown）→ 3. Phase 7（连接状态）→ 4. 停点验证：P1 缺陷全部可独立演示
   - 注：US1 的完整验收（真实链路排查修复）按用户指令在 Phase 12 testplan 验收后与用户手工闭环；P1 能力半就绪即可进入后续 phase

### Incremental Delivery

1. Setup+Foundational → 2. +US2/US4/US3/US1（P1 呈现与可观测）→ 3. +US5/US6/US7/US8（P2）→ 4. +testplan 重构 → 5. 最终验收（自动 + 手工）

---

## Notes

- 编译+单测（`bazel build`/`bazel test` 相关 target）为每个实现任务的交付门禁，未单列 task（constitution IV）
- T028–T030 为**协作任务**：需用户在正式部署环境配合操作（desktop 连接/绑定/游戏观察），执行者负责部署、引导、取证与修复循环
- 大型测试一律经 testplan skill 执行（`style/large_test.md`）；排查一律经 signoz skill 取证（`AGENTS.md`）
- 对同一文件的编辑串行进行（ChatView.tsx 跨 phase 复用，严格按 phase 顺序）
