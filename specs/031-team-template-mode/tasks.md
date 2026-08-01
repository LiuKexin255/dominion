# Tasks: Team Template Mode (StateGraph 升级)

**Input**: Design documents from `/specs/031-team-template-mode/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: 单测（编译+`bazel test`）属每代码变更 task 内（宪法原则 IV，不单列）；大型测试单列验收 task（Phase 8，宪法原则 VI）。

**Organization**: 按 user story 分 phase；**每个 phase 遵循"先删除旧设计、再实现新设计"原则（需求方 directive）**——同一次变更中先移除旧代码、再编写新代码，避免开发过程中兼容不需要的代码。

**Delete-first 原则说明**：proto 重写后各服务 target 在其 phase 内修复前不会编译通过（大型重构预期行为）；每个 task 内先删旧码再写新码，task 结束时该 task 涉及的 bazel target 应可通过编译+单测。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g. US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 依赖准备——agent 新增 TS mongo 驱动依赖（StrategyStore 持久化）。

### 文档清单

- **代码规范文档**：`style/javascript.md`（引用 https://google.github.io/styleguide/tsguide.html ）；`AGENTS.md`（TS/JS 依赖管理：catalog 统一管理 + `pnpm up` + gazelle + `bazel mod tidy`）
- **官方文档**：无
- **技术文章**：无

### Tasks

- [ ] T001 Add `mongodb` TS driver to `pnpm-workspace.yaml` catalog (root `pnpm-workspace.yaml` catalog section); run `bazel run @pnpm -- --dir /mnt/code/dominion up` to update lockfile
- [ ] T002 Add `mongodb` dependency to `projects/game/agent/package.json` dependencies (reference catalog entry from T001); run `bazel run //:gazelle projects/game/agent` to update `projects/game/agent/BUILD.bazel`; verify `bazel build projects/game/agent:lib`

---

## Phase 2: Foundational (Proto & gameconst Rewrite)

**Purpose**: 重写 proto 资源层级契约与资源名解析——所有 user story 的契约地基（宪法原则 III 接口优先）。clean break：移除 Agent/AgentProfile/Skill/ProxyService/AgentService/RefreshAgent，新增 Template 资源消息/TeamService/Team/TeamAgent/TeamProfile/SaoleiProfile。

**⚠️ CRITICAL**: 本 phase 完成后 proto 编译通过，但下游 Go 服务/TS agent target 将引用已移除的类型而编译失败——后续 Phase 3-5 逐步修复。这是大型重构的预期行为。

### 文档清单

- **代码规范文档**：`style/api.md`（引用以下 AIP 作为 API 设计规范基准——均须阅读）：
  - [AIP-121 Resource-oriented design](https://google.aip.dev/121)
  - [AIP-122 Resource names](https://google.aip.dev/122)
  - [AIP-123 Resource types](https://google.aip.dev/123)
  - [AIP-126 Enumerations](https://google.aip.dev/126)（FrameSender 等 enum；Template 设计修订后为资源消息，见 T003 注 1）
  - [AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)
  - [AIP-131 Standard methods: Get](https://google.aip.dev/131)
  - [AIP-132 Standard methods: List](https://google.aip.dev/132)
  - [AIP-133 Standard methods: Create](https://google.aip.dev/133)
  - [AIP-134 Standard methods: Update](https://google.aip.dev/134)
  - [AIP-135 Standard methods: Delete](https://google.aip.dev/135)
  - [AIP-136 Custom methods](https://google.aip.dev/136)（`:refresh` custom method）
  - [AIP-149 Unset field values](https://google.aip.dev/149)（oneof 语义）
  - [AIP-161 Field masks](https://google.aip.dev/161)（UpdateTeamProfile update_mask）
- **官方文档**：无
- **技术文章**：`specs/031-team-template-mode/contracts/api-contract.md`（资源层级 + 服务 + RPC + 关键消息语义）；`specs/031-team-template-mode/data-model.md` §1（API 资源实体字段）

### Tasks

- [ ] T003 Rewrite `projects/game/game.proto` — **先删除后新增**（clean break，参照 `contracts/api-contract.md`）：
  - **删除**：`ProxyService`、`AgentService`、`Agent`（message）、`AgentProfile`（message + 全部 Request/Response）、`Skill`（message + 全部 Request/Response）、`RefreshAgentRequest`、`GetAgentRequest`；`Session` pattern 改为 `templates/{template}/sessions/{session}`；`Message` pattern 改为含 agents 分区；`AgentFrame` field 7 `agent_profile_name` 改名 `agent`。
  - **新增**：`message Template`（`google.api.resource` 注解：type `game.liukexin.com/Template`，pattern `templates/{template}`，singular/plural；无任何 RPC，FR-001——见注 1）；`message TeamAgent { string name=1; bool accepts_user_input=2; }`；`message Team`（pattern `templates/{template}/sessions/{session}/team`，含 `repeated TeamAgent agents`）；`TeamService`（`GetTeam`/`Connect`/`ListMessages`/`RefreshTeam`，取代 ProxyService+AgentService）；`message TeamProfile`（pattern `templates/{template}/profiles/{profile}`，含 `string template`（REQUIRED + `resource_reference` type `game.liukexin.com/Template`）+ `oneof spec { SaoleiProfile saolei=10; }`）；`message SaoleiProfile { string player_model=1; string planner_model=2; }`；PromptService 改为 TeamProfile CRUD（CreateTeamProfile/GetTeamProfile/ListTeamProfiles/UpdateTeamProfile/DeleteTeamProfile）。
  - **保留不变**：`MessageParts`/`FlowParts`/`MessagePart`/`FlowPart` 及其子消息（TextPart/ThinkingPart/ImagePart 等）、`FrameSender` enum、其他 enums。`Message` 资源仅改 pattern + 加 `agent` field。
  - **SessionService RPCs**：pattern 从 `sessions/*` 改为 `templates/*/sessions/*`（CreateSession parent=`templates/{template}`）。
  - 每个 Service/Method 保留注释（`style/api.md` 要求 Service 注释含 Prefix Path）。
  - 验证 `bazel build projects/game:game_proto`。

> **注 1（设计修订）**：Template 实现为资源消息（`message Template`，无任何 RPC）而非 proto enum；`Session.template`（OUTPUT_ONLY）/`TeamProfile.template`（REQUIRED）为 `string` + `resource_reference`（值 = 模板资源名 `templates/{template}`）；具体模板值在 gameconst 常量（`SaoleiTemplate`）；资源名解析由 `protoc-gen-go-aip` codegen 生成（`ParseTemplateName` 等），见 `contracts/api-contract.md` §3.1/§5。
- [ ] T004 Rewrite `projects/game/pkg/gameconst/const.go` — **先删除后新增**（参照 `contracts/api-contract.md` §5）：
  - **删除**：`SessionNamePrefix`、`AgentProfileNamePrefix`、`SkillNamePrefix`、`PromptsParent`；`AgentName`/`AgentSessionID`/`AgentProfileName`/`AgentProfileID`/`SkillName`/`SkillID`；对应 error vars（`ErrInvalidAgentProfileName`/`ErrInvalidSkillName`/`ErrInvalidAgentName`）。
  - **新增**：Template 常量与校验——`var SaoleiTemplate = game.TemplateName{TemplateID: "saolei"}`（具体模板值，非 proto enum）、`ValidateTemplateName(name game.TemplateName) error`、`IsKnownTemplateID(segment string) bool`、`ErrInvalidTemplate`。保留 gRPC target 常量（`SessionTarget`/`ProxyTarget`→rename 为 `TeamTarget`/`AgentTarget`/`PromptTarget` 等，按实际 proto service 名调整）、log field 常量。
  - **注 2（设计修订）**：资源名解析不再手写于 gameconst——由 `protoc-gen-go-aip` codegen 生成（`ParseTemplateName`/`ParseSessionName`/`ParseTeamName`/`ParseTeamProfileName`/`ParseMessageName` 及 `ParseName()`/`ParseTemplate()`/`Parent()`），见 `contracts/api-contract.md` §5。
  - 验证 `bazel build projects/game/pkg/gameconst` + `bazel test projects/game/pkg/gameconst`（更新或新增 const_test.go 验证 Template 常量与校验）。

**Checkpoint**: proto + gameconst 就绪。下游各服务 target 待 Phase 3-5 修复。

---

## Phase 3: User Story 1 - Template 与 Team 资源层级重构 (Priority: P1) 🎯 MVP

**Goal**: Go 服务层适配新 proto 资源层级——session/proxy(→TeamService)/prompt 服务全部适配 templates/{template}/... 路径；prompt 服务移除 AgentProfile/Skill、实现 TeamProfile CRUD。

**Independent Test**: proto 契约与各服务 API 路由验证（spec US1 AC1-AC6）：Session 路径为 `templates/{template}/sessions/{session}`；Team 资源取代 Agent；TeamProfile 取代 AgentProfile/Skill；无顶层 sessions 路径。

### 文档清单

- **代码规范文档**：`style/golang.md`（Go 代码风格）；`style/api.md`（引用 AIP 列表同 Phase 2）；`style/mongo.md`（BSON 对象定义规范）
- **官方文档**：无
- **技术文章**：`specs/031-team-template-mode/contracts/api-contract.md` §2/§5（服务 RPC + 资源名解析）；`specs/031-team-template-mode/data-model.md` §1.2-§1.5（Session/Team/Message/TeamProfile 实体）

### Tasks

- [ ] T005 [P] [US1] Rewrite session service for template-scoped paths in `projects/game/session/handler/handler.go` + `projects/game/session/handler/handler_test.go` — **先删除后新增**：
  - `CreateSession`：request parent 从 `sessions` 改为 `templates/{template}`；提取 template 段；proto `Session` 增加 `template` 字段回填；`gameconst.SessionName(template, id)` 替换旧 `SessionName(id)`。
  - `GetSession`/`DeleteSession`：`gameconst.SessionID(name)` 返回 `(template, sessionID)`；校验 template 合法。
  - `ListSessions`：parent 从 `sessions` 改为 `templates/{template}`。
  - `cmd/main.go`：wiring 不变（proxy client → team client 后续 Phase 5 处理 agent 端，此处 proxy→team 由 T006 覆盖）。
  - domain/model.go `Session` struct 加 `Template string` 字段；mongo model.go 加对应 BSON field。
  - 验证 `bazel build projects/game/session:grpc` + `bazel test projects/game/session/...`。
- [ ] T006 [P] [US1] Rewrite proxy service → TeamService in `projects/game/proxy/handler/handler.go` + `handler_test.go` + `runtime/agentclient/client.go` + `cmd/main.go` — **先删除后新增**：
  - **删除** handler 中 `GetAgent`/`ListMessages`(old)/`ConnectAgent`/`RefreshAgent` 实现。
  - **新增** `GetTeam`（GET `templates/*/sessions/*/team`，解析 `gameconst.TeamSessionID`，lookup owner，调下游 `client.GetTeam`）；`Connect`（bidi stream，端点 `templates/{template}/sessions/{session}/connect`，assign owner 逻辑复用）；`ListMessages`（GET `.../agents/*/messages`，按 agent 分区，解析 `gameconst.MessageAgentParse`）；`RefreshTeam`（POST `.../team:refresh`）。
  - `agentclient/client.go`：`Client` interface 改为 `GetTeam`/`Connect`(TeamService_ConnectClient)/`ListMessages`/`RefreshTeam`；`AgentClient` wrapper 改为 `game.TeamServiceClient`。
  - `agentclient/manager.go`：gRPC target 改为 `game/team:grpc`（或按 proto service 名）。
  - 验证 `bazel build projects/game/proxy/...` + `bazel test projects/game/proxy/...`。
- [ ] T007 [P] [US1] Rewrite prompt service — delete AgentProfile/Skill, implement TeamProfile CRUD in `projects/game/prompt/domain/model.go` + `domain/repository.go` + `domain/errors.go` + `handler/handler.go` + `handler/handler_test.go` + `runtime/mongo/model.go` + `runtime/mongo/repository.go` + `runtime/mongo/repository_test.go` + `cmd/main.go` — **先删除后新增**：
  - **删除**：`AgentProfile` struct、`Skill` struct、`AgentProfileRepository`/`SkillRepository` interfaces；handler 中全部 AgentProfile/Skill RPC 实现（CreateAgentProfile/GetAgentProfile/ListAgentProfiles/UpdateAgentProfile/DeleteAgentProfile/CreateSkill/GetSkill/ListSkills/DeleteSkill）；mongo `agentProfileDocument`/`skillDocument`/对应 filter；`agent_profiles`/`skills` collection 操作。
  - **新增**：`TeamProfile` domain struct（`TeamProfileName`/`Template`/`SaoleiPlayerModel`/`SaoleiPlannerModel`/`CreateTime`/`UpdateTime`）；`TeamProfileRepository` interface（Create/Get/List/Update/Delete）；handler TeamProfile CRUD（`CreateTeamProfile`/`GetTeamProfile`/`ListTeamProfiles`/`UpdateTeamProfile`/`DeleteTeamProfile`）——校验 `template` 与 oneof 变体一致（FR 禁潜规则）；UpdateTeamProfile 支持 update_mask（含 oneof 成员路径 `saolei.player_model`/`saolei.planner_model`）；mongo `teamProfileDocument`（`team_profiles` 集合，unique index on `team_profile_name`）；repository 实现。
  - `cmd/main.go`：`NewRepository` 改 collection 为 `team_profiles`；wiring 简化（仅 TeamProfileRepository）。
  - 验证 `bazel build projects/game/prompt:grpc` + `bazel test projects/game/prompt/...`。

**Checkpoint**: 三个 Go 服务适配新 proto。Agent TS + desktop 待后续 phase。

---

## Phase 4: User Story 3 - saolei MCP 旁路事件 sink (Priority: P1)

**Goal**: saolei MCP 提供可选旁路事件 sink 注册接口，仅定义事件形状不耦合 team mode；默认无 sink 行为零变化。

**Independent Test**: MCP 不注册 sink 时与升级前行为一致（FR-020）；注册记录型 sink 后按时机回调，`onGameEnd.status` 为枚举（FR-022）；接口不引用 team/strategy/store（FR-019）。

### 文档清单

- **代码规范文档**：`style/javascript.md`（引用 https://google.github.io/styleguide/tsguide.html ）
- **官方文档**：`@modelcontextprotocol/sdk` MCP server API（GitHub https://github.com/modelcontextprotocol/typescript-sdk ）
- **技术文章**：`specs/031-team-template-mode/contracts/saolei-sink-contract.md`（接口签名 + 注册点 + 调用时机 + 错误隔离）

### Tasks

- [ ] T008 [US3] Add `SaoleiEventSink` interface and `sink?` parameter to `createSaoleiMcpServer` in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` + update `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts` — **先改签名后接线**：
  - 定义 `export interface SaoleiEventSink { onGameStart(state: GameState): void|Promise<void>; onMove(tool: CellTool, x: number, y: number, state: GameState): void|Promise<void>; onGameEnd(state: GameState, status: "won"|"lost"): void|Promise<void>; }`（参照 `contracts/saolei-sink-contract.md` §1；**不引用 team/strategy/store/teamMemoryId**）。
  - `createSaoleiMcpServer(bridge, boardApi?, sink?)` 增第三可选参数 `sink?: SaoleiEventSink`（默认 `undefined`）。
  - 在 saolei MCP handler 内接线：`saolei_init` recognize 成功后调 `sink?.onGameStart(state)`；`saolei_click`/`saolei_flag` recognize 成功后调 `sink?.onMove(tool, x, y, state)`；`gameStatus(state)`（`:253`）变为 won/lost 时调 `sink?.onGameEnd(state, status)`。
  - **错误隔离**：sink 回调用 try/catch 包裹 + 日志，异常不影响工具返回值（`contracts/saolei-sink-contract.md` §5）。
  - 单测：未传 sink 时全部工具行为不变（对照现有测试基线）；传记录型 sink 后 onGameStart/onMove/onGameEnd 按时机回调且 `status` 为 `"won"|"lost"` 枚举。
  - 验证 `bazel test projects/game/agent:lib_test`（含 saolei-mcp.test.ts）。
- [ ] T009 [US3] Update `projects/game/agent/src/mcp-host.ts` + `mcp-host.test.ts` — extend `SessionBridgeLookup` to carry team sink:
  - `SessionBridgeLookup` 类型扩展：返回值增加 `sink?: SaoleiEventSink`（由 SessionTeam 提供，绑定 ephemeral buffer；Phase 5 SessionTeam 实现后注入）。
  - 修正 `mcp-host.ts:9` 过时注释（spec 025 后 `recognized` 状态已存在）。
  - `createSaoleiMcpServer` 调用处传递 `lookup(sessionId).sink`（如有）。
  - 单测更新。
  - 验证 `bazel test projects/game/agent:lib_test`。

**Checkpoint**: MCP sink 接口就绪，team 侧消费者（teamSink）待 Phase 5 实现。

---

## Phase 5: User Story 2 - saolei 模板的 player+planner 协作 (Priority: P1)

**Goal**: agent 核心从单 agent 重构为 saolei team graph（player + planner），含 strategy store、ephemeral buffer、条件边路由、RefreshTeam 清空。**先删除旧单 agent 代码（session-agent/llm adapter），再实现新 team graph 代码**。

**Independent Test**: team 运行一局扫雷时仅 player 操作、planner 不参与；局结束 planner 恰好触发一次并写策略；策略跨局共享；RefreshTeam 清短期保留策略（FR-009..FR-018）。

### 文档清单

- **代码规范文档**：`style/javascript.md`（引用 https://google.github.io/styleguide/tsguide.html ）
- **官方文档**（均须阅读）：
  - LangGraph StateGraph + Annotation.Root：https://langchain-ai.github.io/langgraphjs/ （`Annotation.Root`、`StateGraph`、`MemorySaver`、`messagesStateReducer`、`REMOVE_ALL_MESSAGES`）
  - LangGraph `createAgent`：https://v5.langchain.com/docs/ (LangChain 1.x `createAgent` API、middleware hooks `beforeModel`/`wrapToolCall`/`afterAgent`)
  - MongoDB Node.js Driver：https://www.mongodb.com/docs/drivers/node/current/ （connect、collection、upsert、index）
- **技术文章**（均须阅读）：
  - `experimental/ts/team_graph_spike/FINDINGS.md`（spike 实测结论：Annotation.Root 必须、createAgent 不带 checkpointer、TS2883 坑、middleware 钩子面、gameEnded 终值坑）
  - `experimental/ts/team_graph_spike/src/team-graph.ts`（spike 验证可行的最小 team graph 骨架——**实现范本**）
  - `specs/031-team-template-mode/contracts/team-graph-contract.md`（state/nodes/edges/flow/RefreshTeam 契约）
  - `specs/031-team-template-mode/contracts/strategy-store-contract.md`（StrategyStore 接口 + mongo 文档形状 + 初始值 `""`）
  - `specs/031-team-template-mode/research.md` D5/D6/D7/D8/D10/D14（per-agent 通道、planner 触发、ephemeral buffer、RefreshTeam、基础设施重构、API 假设实测确认）

### Tasks

> **实现顺序**：先创建新文件（strategy-store → team/* → session-team → middleware → prompt-client → handler → server），最后删除旧文件（session-agent/llm adapter path）。每步编译+单测。

- [ ] T010 [P] [US2] Create `projects/game/agent/src/strategy-store.ts` — `StrategyStore` interface (`get(sessionId): Promise<string>` 无记录返回 `""`；`put(sessionId, content): Promise<void>`) + `MongoStrategyStore` impl（直连 mongo，`strategies` 集合，`session_id` unique index，upsert 语义；连接配置经 secrets 类同 prompt 服务连法）。含 fake impl（内存 Map，用于测试）。单测：get(无记录)→`""`、put→get 一致、session 隔离。参照 `contracts/strategy-store-contract.md`。
- [ ] T011 [P] [US2] Create `projects/game/agent/src/team/state.ts` — `TeamState` via **`Annotation.Root`**（**不要用 `new StateSchema`+zod**——spike D14 注意事项 1）：`playerMessages`/`plannerMessages` = `Annotation<BaseMessage[]>({ reducer: messagesStateReducer, default: () => [] })`；`gameEnded` = `Annotation<"won"|"lost"|null>({ reducer: overwrite, default: () => null })`。**TS2883 坑**（D14 注意事项 2）：`TeamState` 保持模块私有、仅导出 `typeof TeamState.State`。参照 `contracts/team-graph-contract.md` §1 + spike `team-graph.ts`。
- [ ] T012 [US2] Create `projects/game/agent/src/team/update-strategy.ts` — `update_strategy` tool（仅 planner 持有，写 `StrategyStore.put(sessionId, content)`；schema `{ content: z.string() }`）。参照 `contracts/team-graph-contract.md` §2.4。
- [ ] T013 [US2] Create `projects/game/agent/src/team/team-sink.ts` — `teamSink` 实现（`SaoleiEventSink` consumer）：`onGameStart` 可选重置 buffer；`onMove` 更新 `buffer.gameState`；`onGameEnd` 写 `buffer.gameEvent = {state, status, endedAt, consumed:false}` + 更新 `buffer.gameState`。ephemeral buffer 类型定义（per-session 进程内普通对象）。参照 `contracts/saolei-sink-contract.md` §4 + `contracts/team-graph-contract.md` §3。
- [ ] T014 [US2] Create `projects/game/agent/src/team/player.ts` — player 节点：**`createAgent`（不带自身 checkpointer，内部 agent loop 跑到 LLM 自停）**；工具 = saolei MCP tools（仅 player）；进入时读 `StrategyStore.get(sessionId)` 作为"当前态势"注入 prompt（FR-015；player 无读取工具）；**后处理**（createAgent 返回后执行一次）：读 ephemeral buffer `gameEvent`，若未 consumed 则写 `TeamState.gameEnded = status`（D6 步骤 4）。流式输出 `ContentBlock` → `AgentFrame`（`agent="player"`）。`accepts_user_input=true`。参照 `contracts/team-graph-contract.md` §2.1 + spike `team-graph.ts`。
- [ ] T015 [US2] Create `projects/game/agent/src/team/planner.ts` — planner 节点：system = [复盘指令] + [当前策略 `StrategyStore.get`（初始 `""`，FR-014）]；复盘输入 = ephemeral buffer `gameState`；工具 = 仅 `update_strategy`（FR-012）；`update_strategy` 重试在节点内部处理（D6/需求方 #6）；节点返回后 graph 清 `gameEnded=null`（无条件）。`accepts_user_input=false`。参照 `contracts/team-graph-contract.md` §2.2。
- [ ] T016 [US2] Create `projects/game/agent/src/team/graph.ts` — `TeamGraph` builder：`StateGraph(TeamState)` → addNode("player", playerNode) / addNode("planner", plannerNode) → addEdge(START, "player") → addConditionalEdges("player", route(读 `state.gameEnded`): gameEnded≠null→"planner"；null→END) → addEdge("planner", 清 gameEnded=null, "player") → compile({ checkpointer: new MemorySaver() })。模板定义导出 `TeamAgent[]` 描述（`[{name:"player",accepts_user_input:true},{name:"planner",accepts_user_input:false}]`）。参照 `contracts/team-graph-contract.md` §2.3 + spike `team-graph.ts`。**测试断言坑**（D14 注意事项 3）：断言"局结束"检查 `plannerMessages` 非空而非终态 `gameEnded`。
- [ ] T017 [US2] Create `projects/game/agent/src/session-team.ts` — `SessionTeam`（取代 `SessionAgent`）：持有 team graph 实例 + per-session ephemeral buffer + `StrategyStore` 引用 + `OperationBridge`（player 独占）。`SessionTeamStore`（取代 `SessionAgentStore`）：map sessionId → SessionTeam。turn 提交 = 一次 graph invoke（`gameEnded` 在 turn 内由条件边处理，不破坏 TurnLoop 单飞）。复用 TurnLoop 单飞/队列语义（D10）。参照 `contracts/team-graph-contract.md` §6。
- [ ] T018 [US2] Rewrite `projects/game/agent/src/context-middleware.ts` — RefreshTeam 清空短期记忆：`beforeModel` 钩子返回 `{ messages: [new RemoveMessage({ id: REMOVE_ALL_MESSAGES })] }` 对 playerMessages 与 plannerMessages 各清一次（per-channel 独立，spike D14 A1 实测确认）。策略（StrategyStore）与 gameEnded 不在清空范围（FR-018）。参照 `contracts/team-graph-contract.md` §5 + spike D14 A4。
- [ ] T019 [US2] Rewrite `projects/game/agent/src/prompt-client.ts` — **先删除后新增**：删除 `getProfile(profileName)`（GetAgentProfile）；新增 `getTeamProfile(template, profileName)` → 调 `GetTeamProfile` RPC，返回 `{ playerModel, plannerModel }`（从 `oneof spec.saolei` 解析）。PROMPT_SERVICE_TARGET 不变。参照 `contracts/api-contract.md` §2.3 + `contracts/strategy-store-contract.md`（prompt 服务不参与 strategy）。更新 `prompt-client.test.ts`。
- [ ] T020 [US2] Rewrite `projects/game/agent/src/handler.ts` — **先删除后新增**：删除 `GetAgent`/`RefreshAgent` handler 实现；新增 `GetTeam`（返回 Team + agents 描述，D3）；`Connect`（bidi stream，用户输入帧路由给"接受用户输入"的 agent FR-032；frame `AgentFrame.agent` 标识来源 agent）；`ListMessages`（按 agent 分区，从 checkpoint state 按 agent 通道重建历史）；`RefreshTeam`（触发 context-middleware 清空 + invalidate team graph state）。参照 `contracts/api-contract.md` §2.2 + `contracts/team-graph-contract.md` §6。更新 `handler.test.ts`。
- [ ] T021 [US2] Rewrite `projects/game/agent/src/server.ts` — **先删除后新增** wiring：删除 SessionAgentStore + AdapterFactory path；新增 SessionTeamStore + StrategyStore(mongo client) + team graph builder + MemorySaver。mongo 客户端初始化（连接当前 mongo 实例，`strategies` 集合）。MCP host lookup 注入 team sink（T009 扩展点）。`model-provider.ts` 适配 TeamProfile（player/planner 各自 ChatModel）。参照 `contracts/team-graph-contract.md` §6 + D10/D11。
- [ ] T022 [US2] Delete old single-agent code: `projects/game/agent/src/session-agent.ts` + `session-agent.test.ts`（全删，由 session-team.ts 取代）；rewrite `projects/game/agent/src/llm.ts` — **删除** `AdapterFactory`/`AgentAdapter`/`AgentAdapterImpl`（单 agent adapter 路径），**保留或迁移**仍被 team graph/handler 引用的共享类型（`ContentBlock`/`TurnContent`/`TurnContentPart`/`buildTools`/`MOUSE_TOOL_NAMES`，按实际引用情况决定迁移到 team/ 或保留）；更新 `llm.test.ts`（删除 adapter 测试，保留共享类型测试）。`profile-guard.ts` 适配 team 上下文（或删除如不再需要）。验证 `bazel build projects/game/agent:lib` + `bazel test projects/game/agent:lib_test`。

**Checkpoint**: agent 从单 agent 完全重构为 team graph。player 独占操作 + planner 每局触发 + 策略持久 + RefreshTeam 清空。

---

## Phase 6: User Story 4 - Desktop 多 Agent 标签页与模板控制面 (Priority: P2)

**Goal**: desktop 以模板为顶层控制面（枚举常量），对话区按 team agent 分 tab，frame 按 agent 归位，planner tab 屏蔽输入。

**Independent Test**: 顶层切换模板基于枚举无网络请求（FR-024）；对话多 tab 来自 `Team.agents`（不硬编码）；frame 按 `agent` 归位（FR-025）；planner tab 屏蔽输入（FR-032）。

### 文档清单

- **代码规范文档**：`style/golang.md`（Go 代码风格，app.go）；`style/javascript.md`（引用 https://google.github.io/styleguide/tsguide.html ，frontend）
- **官方文档**：无
- **技术文章**：`specs/031-team-template-mode/contracts/desktop-contract.md`（模板控制面 + 多 tab + 输入路由 + Wails 绑定变更 + frontend 类型变更）；`specs/031-team-template-mode/data-model.md` §1.3/§1.6（Team/TeamAgent + AgentFrame 字段）

### Tasks

- [ ] T023 [P] [US4] Rewrite `projects/game/desktop/view_model.go` — **先删除后新增**：删除 `AgentView`/`AgentProfileView`/`CreateAgentProfileView`/`ListAgentProfilesView`；新增 `TeamView`（Name, SessionID, Agents: []TeamAgentView）、`TeamAgentView`（Name, AcceptsUserInput）、`TeamProfileView`（Name, Template, PlayerModel, PlannerModel）、`CreateTeamProfileView`、`ListTeamProfilesView`。`SessionView` 加 `Template` 字段。更新 `view_model_test.go`。
- [ ] T024 [P] [US4] Rewrite `projects/game/desktop/frontend/src/api.ts` — **先删除后新增** types + bindings：删除 `AgentProfile`/`Skill` interfaces + 对应 binding wrappers（createAgentProfile/getAgentProfile/listAgentProfiles/updateAgentProfile/deleteAgentProfile/createSkill/getSkill/listSkills/deleteSkill）；新增 `Template` enum（`TEMPLATE_SAOLEI`）、`Team`/`TeamAgent` interfaces（`{name, accepts_user_input}`）、`TeamProfile`/`SaoleiProfile` interfaces；`AgentFrame.agentProfileName` → `agent`（D12）；binding wrappers 改为 `createSession(template)`/`connect(template, sessionID)`/`refreshTeam(template, sessionID)`/`sendUserTurn(template, sessionID, text, screenshot..., agent)`/`listMessages(template, sessionID, agent)`/`getTeam(template, sessionID)`/teamProfile CRUD。`WailsApp` interface 同步更新。
- [ ] T025 [US4] Rewrite `projects/game/desktop/app.go` — **先删除后新增** Wails bindings（参照 `contracts/desktop-contract.md` §4）：删除 `CreateSession()`(no-arg)/`ConnectAgent`/`RefreshAgent`/`GetAgent`/`SendUserTurn(...,agentProfileName)`/`ListMessages(sessionID)`/AgentProfile CRUD/Skill CRUD bindings；新增 `CreateSession(template)`/`Connect(template, sessionID)`/`RefreshTeam(template, sessionID)`/`GetTeam(template, sessionID)`/`SendUserTurn(template, sessionID, text, screenshot..., agent)`/`ListMessages(template, sessionID, agent)`/TeamProfile CRUD bindings。`recvLoop`/`handleInboundOperation` 适配新 frame 结构（`agent` 替代 `agentProfileName`）。更新 `app_test.go`。
- [ ] T026 [US4] Rewrite `projects/game/desktop/frontend/src/App.svelte` — **先删除后新增**：顶层模板控制面（本地枚举常量 `TEMPLATE_SAOLEI`，无网络请求 FR-024）；`handleMessageParts` 按 `frame.agent`（替代 `agentProfileName`）归位 + 合并；会话对话区多 tab（agent 列表来自 `getTeam().agents`，不硬编码 FR-025）；输入路由给当前选中且 `accepts_user_input=true` 的 agent（FR-032）。`page` state 加模板维度。
- [ ] T027 [US4] Rewrite `projects/game/desktop/frontend/src/components/AgentSidebar.svelte` → `TeamSidebar.svelte` — 显示 Team + agents 列表（typed `TeamAgent[]`）、连接状态、刷新按钮（`RefreshTeam`）；agent 输入能力标注（`accepts_user_input`）；planner agent 的 tab 屏蔽输入（FR-032）。删除旧 profile selector（profile 管理移至 US5 ProfileManagement）。

**Checkpoint**: desktop 适配 team 模型。多 tab + 模板控制面 + 输入路由就绪。

---

## Phase 7: User Story 5 - 模板特化的 TeamProfile 配置 (Priority: P2)

**Goal**: saolei 的 TeamProfile 仅含 player/planner 模型选择；desktop profile 页对该模板特化渲染；tools/mcp 由模板固定装配不可经 profile 配置。

**Independent Test**: prompt 服务管理 `templates/saolei/profiles/{profile}`；TeamProfile 仅含 player/planner 模型（FR-027）；tools/mcp 模板固定装配（FR-028）；desktop profile 页特化渲染（FR-029）。

### 文档清单

- **代码规范文档**：`style/javascript.md`（引用 https://google.github.io/styleguide/tsguide.html ）
- **官方文档**：无
- **技术文章**：`specs/031-team-template-mode/contracts/desktop-contract.md` §3（Profile 页面特化）；`specs/031-team-template-mode/data-model.md` §1.5（TeamProfile/SaoleiProfile 字段）

### Tasks

- [ ] T028 [US5] Rewrite `projects/game/desktop/frontend/src/components/ProfileManagement.svelte` — **先删除后新增**：删除通用 AgentProfile 表单（model/systemPrompt/skillNames/mcpNames/toolNames/enabled 字段）；按当前模板的 TeamProfile typed oneof 渲染特化表单——saolei：仅 `player_model` / `planner_model` 选择（FR-029），无 tools/mcp/skill 字段（FR-027/FR-028，模板固定装配）。CRUD 经新 TeamProfile bindings（T024）。

**Checkpoint**: TeamProfile 配置面完整。saolei profile 仅模型选择，tools/mcp 模板装配。

---

## Phase 8: Polish & Large Test (FR-030 宪法原则 VI)

**Purpose**: 大型测试创建与执行验收。

### 文档清单

- **代码规范文档**：`style/large_test.md`（测试计划结构 + guitar 执行）；`style/golang.md`（Go 测试代码风格）
- **官方文档**：无
- **技术文章**：`specs/031-team-template-mode/quickstart.md` §2（大型测试用例清单 + 运行步骤）；`projects/game/testplan/README.md`（既有测试计划文档）

### Tasks

- [ ] T029 Create saolei team large test cases in `projects/game/testplan/` — 参照 `quickstart.md` §2.1 用例表：新增/改造 Go test 文件覆盖 team-connect（FR-003/FR-004）、player-exclusive-control（FR-010）、planner-trigger-per-game（FR-011/D6）、strategy-shared-persistent（FR-013/FR-014/FR-015）、refresh-team-clears-short-term（FR-018/D8）、message-partition-by-agent（FR-005）、team-profile-crud（FR-006/FR-027）。改造 `helpers_test.go` 适配新资源路径（`templates/saolei/sessions/...`）。更新 `system_test.yaml`（新增 saolei-team suite）+ `deploy_agent.yaml`（拓扑复用，确保 agent_test image 含新 team graph 代码）。新增 fake-llm testdata（planner update_strategy 响应）到 `projects/game/fake-llm/service/testdata/`。声明 `go_largetest` target 在 `projects/game/testplan/BUILD.bazel`。验证 `bazel build projects/game/testplan:...`。
- [ ] T030 Execute large test via testplan skill — 加载 testplan skill；阅读 `style/large_test.md`；执行 `guitar run projects/game/testplan/<saolei-team-plan>.yaml`（完整部署→测试→清理闭环）。**验收标准：所有测试用例全部通过（all cases passed）**。存在任何 failed/flaky 即验收未过，修复后重跑至全绿（宪法原则 VI）。
- [ ] T031 Full repository build + test verification — 执行 `bazel build //...` + `bazel test //...` 确保全仓库编译与单测通过（所有 phase 完成后的最终验证）。修复任何遗留编译错误或测试失败。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately.
- **Foundational (Phase 2)**: Depends on Phase 1（agent mongo 依赖就绪后方可 wiring；proto 本身不依赖 mongo）。
- **US1 (Phase 3)**: Depends on Phase 2（proto+gameconst 就绪后方可适配 Go 服务）。
- **US3 (Phase 4)**: Depends on Phase 2（proto 类型）。**不依赖** Phase 3/5（MCP sink 接口独立）。
- **US2 (Phase 5)**: Depends on Phase 2（proto）+ Phase 3 T007（prompt TeamProfile CRUD，agent prompt-client 依赖）+ Phase 4（MCP sink，teamSink 消费）。
- **US4 (Phase 6)**: Depends on Phase 2（proto types）+ Phase 5（team 行为就绪，desktop 展示 team agents）。
- **US5 (Phase 7)**: Depends on Phase 6（desktop 模板控制面就绪，profile 页在其内）。
- **Large Test (Phase 8)**: Depends on Phase 3-7 全部完成。

### User Story Dependencies

```
Phase 1 (Setup)
    ↓
Phase 2 (Proto+gameconst) ← foundational, blocks all
    ↓
Phase 3 (US1 Go services) ──────┐
    ↓                            │
Phase 4 (US3 MCP sink) ──────→ Phase 5 (US2 Team graph)
                                 ↓
                           Phase 6 (US4 Desktop)
                                 ↓
                           Phase 7 (US5 Profile)
                                 ↓
                           Phase 8 (Large Test)
```

### Within Each User Story

- 先删除旧码、再实现新码（需求方 directive）
- proto → services → agent → desktop 依次适配
- 编译 + 单测属每 task 内（宪法原则 IV）
- task 结束时该 task 涉及 bazel target 通过编译+单测

### Parallel Opportunities

- Phase 1: T001/T002 sequential（T002 依赖 T001 catalog entry）
- Phase 2: T003/T004 可并行（proto 与 gameconst 独立文件；但 gameconst 引用 proto 生成的类型——建议 T003 先完成再 T004）
- Phase 3: T005/T006/T007 **可并行**（三个 Go 服务不同目录、互不依赖）
- Phase 4: T008→T009 sequential（mcp-host 依赖 saolei-mcp 的 sink 类型）
- Phase 5: T010/T011 **可并行**（strategy-store 与 team/state.ts 独立）；T012-T016 sequential（依赖链：update-strategy→team-sink→player→planner→graph）；T017-T022 sequential（session-team→middleware→prompt-client→handler→server→delete-old）
- Phase 6: T023/T024 **可并行**（view_model.go 与 api.ts 独立）；T025→T026→T027 sequential（app.go→App.svelte→TeamSidebar）
- Phase 7: T028 sequential（单文件）
- Phase 8: T029→T030→T031 sequential

---

## Implementation Strategy

### MVP First (US1 + US3 + US2 = all P1)

1. Complete Phase 1: Setup (mongo TS driver)
2. Complete Phase 2: Foundational (proto + gameconst rewrite)
3. Complete Phase 3: US1 (Go services adapt to new proto)
4. Complete Phase 4: US3 (MCP sink interface)
5. Complete Phase 5: US2 (Team graph — core behavior)
6. **STOP and VALIDATE**: Large test (Phase 8 T029-T030) for P1 stories
7. Deploy/demo if ready — core team behavior proven

### Incremental Delivery

1. Setup + Foundational → Contract ready
2. US1 → Go services adapted → Test independently
3. US3 → MCP sink → Test independently (no-sink compat + with-sink callbacks)
4. US2 → Team graph → Test end-to-end (P1 MVP!)
5. US4 → Desktop multi-tab → Test UI
6. US5 → Profile specialization → Test config
7. Large Test → Full acceptance

### Key Implementation Notes (from spike D14)

1. **`TeamState` 必须用 `Annotation.Root`**（非 zod StateSchema）——zod schema 不满足 `SerializableSchema`
2. **TS2883 坑**：`TeamState` 保持模块私有，仅导出 `typeof TeamState.State`
3. **createAgent 不带自身 checkpointer**；消息持久化由外层 `MemorySaver` 统一承载
4. **`gameEnded` 终值 null**（planner 跑完即清）；断言"局结束"检查 `plannerMessages` 非空
5. **middleware 钩子**：RefreshTeam 落地于 `beforeModel`（返回 `REMOVE_ALL_MESSAGES`）
6. **实现范本**：`experimental/ts/team_graph_spike/src/team-graph.ts`

---

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks
- [Story] label maps task to specific user story for traceability
- Delete-first 原则：每个变更先移除旧码再写新码（需求方 directive）
- 编译 + 单测 = 每 task 必做（宪法原则 IV），不单列 task
- 大型测试 = Phase 8 验收（宪法原则 VI），必须经 testplan skill 完整执行
- 每次代码变更后执行 `bazel run //:gazelle` 更新 BUILD.bazel
- TS 代码格式化：`bazel run //:go -- fmt [变更文件]`（Go）；vitest test 经 `:lib_test` target
