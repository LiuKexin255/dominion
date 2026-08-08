# Tasks: Planner 长期记忆与校准指令

**Input**: Design documents from `/specs/039-planner-memory-calibration/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: 编译 + 单测属每个代码变更任务的一部分（宪法原则 IV，不单列 task）；大型测试单列为 Phase 7 验收 task（FR-018，宪法原则 VI）。

**Organization**: 按交付增量组织。Phase 1 为共享 proto/部署脚手架；Phase 2 为 US1（MVP，独立）；Phase 3-5 为 US2（memory Go 服务 + agent 记忆数据面 + 装配，分三段以求 review 友好）；Phase 6 为 US3（校准指令 + StrategyStore 移除 + 两场景节点）；Phase 7 为大型测试验收。

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 归属用户故事（US1/US2/US3）；Setup/Polish phase 无 story 标签
- 描述含确切文件路径

## Path Conventions

多服务 web 应用（gRPC 微服务）：Go 服务在 `projects/game/<service>/`，TS agent 在 `projects/game/agent/src/`，proto 在 `projects/game/game.proto`，大型测试在 `projects/game/testplan/`。

---

## Phase 1: Setup（共享 proto + 常量 + 部署脚手架）

**Purpose**: 新增 Memory 资源 + MemoryService proto 契约（codegen 驱动 Go/TS 两端）、target 常量、部署条目。本 phase 是 US2/US3 的共享前置。

### 文档清单（编码前必读）

- **代码规范文档**: `style/api.md`（AIP 索引，标准方法/字段行为规范的入口）；[AIP-122 Resource names](https://google.aip.dev/122)；[AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)；[AIP-133 Standard methods: Create](https://google.aip.dev/133)（CreateMemory：请求**内嵌资源** `Memory memory`、`body: "memory"`、`method_signature: "parent,memory,memory_id"`）；[AIP-134 Standard methods: Update](https://google.aip.dev/134)（UpdateMemory：`body: "memory"`、`update_mask` 字段、不存在→`NOT_FOUND`）；[AIP-135 Standard methods: Delete](https://google.aip.dev/135)（DeleteMemory：`delete` 无 body、不存在→`NOT_FOUND`）；[AIP-132 Standard methods: List](https://google.aip.dev/132)（ListMemories：`get`、`parent`、分页字段）；[AIP-158 Pagination](https://google.aip.dev/158)（`page_size`/`page_token`/`next_page_token`）；[AIP-193 Errors](https://google.aip.dev/193)（`ALREADY_EXISTS`/`NOT_FOUND`/`INVALID_ARGUMENT` 语义）；[AIP-203 Field behavior documentation](https://google.aip.dev/203)（`IDENTIFIER`/`REQUIRED`/`OUTPUT_ONLY` 注解语义）
- **官方文档**: 无（以仓库内 `projects/game/game.proto` 既有 `TeamProfile` 资源 + `PromptService`/`CreateTeamProfileRequest` 为实现模板，照搬其 `google.api.resource` / `google.api.http` / `method_signature` / `field_behavior` 注解模式）
- **技术文章**: `specs/039-planner-memory-calibration/contracts/memory-service-contract.md` §1（Memory 资源消息）、§2（MemoryService + RPC + 请求消息）、§6（错误码）

**Tasks**:

- [X] T001 Add `Memory` resource message + `MemoryService` service + 请求/响应消息（Create/Update/Delete/List + 对应 Request/Response）到 `projects/game/game.proto`（`Memory` 置于 `TeamProfile` 之后；`MemoryService` 置于 `PromptService` 之后；请求消息置于文件末尾请求区）。注解照搬既有 `TeamProfile`/`PromptService` 模式：`google.api.resource` pattern `templates/{template}/sessions/{session}/memories/{memory}`、singular/plural、`name` IDENTIFIER、`memory_id`/`content`、OUTPUT_ONLY timestamps；RPC 的 `google.api.http` + `method_signature`；ListMemories 含 `page_size`/`page_token`/`next_page_token`（AIP-158）。详见 `contracts/memory-service-contract.md` §1-2。
- [X] T002 [P] Add `MemoryTarget = "game/memory:grpc"` 常量到 `projects/game/pkg/gameconst/const.go` 的 target 常量块（紧随 `PromptTarget`，同 `"game/{service}:grpc"` 格式）。
- [X] T003 [P] Add memory 服务 artifact 条目到 `projects/game/deploy.yaml`（prod，`{path: //projects/game/memory/service.yaml, name: memory}`，置于 prompt 与 agent 之间）与 `projects/game/testplan/deploy_agent.yaml`（test，同 artifact；该 deploy 的 mongo 为 `persistence: {enabled: false}`）。
- [X] T004 Run `bazel run //:gazelle projects/game` 重新生成 proto codegen 的 `BUILD.bazel` target；`bazel mod tidy`；`bazel build //projects/game` 验证 codegen 产出 `RegisterMemoryServiceServer`、`UnimplementedMemoryServiceServer`、`ParseMemoryName`/`MemoryName` 等。

**Checkpoint**: `bazel build //projects/game` 通过，MemoryService codegen 可用。

---

## Phase 2: User Story 1 — saolei_operate 双形态落子（Priority: P1）🎯 MVP

**Goal**: 合并 `saolei_click`/`saolei_flag`/`saolei_chord_click` 为 `saolei_operate`（hermes 式双形态参数：普通参数单次 / `operations` 数组批量；保序执行、单次返回、失败按原因细分）；sink `onMove`→`onOperate`；gameLog 以 `saolei_operate` 为单位；planner 工具描述同步。

**Independent Test**: `saolei_operate` 双形态（普通参数单次 / `operations` 数组）均可用且等价（单次 = 长度 1 的 operations），按序执行只返回一次；`saolei_init`/`saolei_remain` 不变；无害空操作跳过继续、结构性/游戏结束停止；gameLog 以 `saolei_operate`（含全部操作）为单位；planner 工具描述含 `saolei_operate` + click/flag/chord。

### 文档清单（编码前必读）

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)；`specs/019-js-test-reliability/`（js_test 执行模型 / DI seam，详见其 `contracts/`）
- **官方文档**: [@modelcontextprotocol/typescript SDK](https://github.com/modelcontextprotocol/typescript-sdk)（`McpServer.registerTool` 既有用法，仓库内 saolei-mcp.ts 已 embodiment）；langchain 自定义工具 https://js.langchain.com/docs/how_to/custom_tools/
- **技术文章**: `specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md` §1-5；既有 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`（`validateMove`/`MoveRejection`/`createSaoleiMcpServer`/`registerCellTool` 模板）；`projects/game/agent/src/team/team-sink.ts`（gameLog 记录）；`projects/game/agent/src/team/planner.ts`（`buildReviewInput`/`buildToolDescriptionSection`/`GAME_VISIBLE_PLAYER_TOOLS`）

**Tasks**:

- [ ] T005 [US1] 在 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` 重构落子工具：抽出按单 op 处理的内部函数（复用 `validateMove`+dispatch）；删除 `saolei_click`/`saolei_flag`/`saolei_chord_click` 三个 `registerCellTool` 注册，新增 `saolei_operate`（**双形态参数**，Session 2026-08-08：普通参数 `type`/`x`/`y` 单次 **或** `operations: CellOperation[]` 批量；`CellOperation = {type: "click"|"flag"|"chord", x, y}`，枚举非裸 string；入口归一化为 operations 列表后统一处理）；定义常量集合 `HARMLESS_NOOP_REASONS`（`cell_already_revealed`/`cell_is_flagged`/`cannot_flag_revealed`/`chord_requires_number`/`chord_no_unrevealed_neighbor`）与 `STRUCTURAL_REASONS`（`out_of_bounds`/`no_active_game`）；按序执行、失败细分（无害空操作 SKIP 继续 / 结构性 STOP / 游戏结束 STOP）、单一 MCP 文本结果（结果行 + 游戏状态行 + 棋盘）；`saolei_init`/`saolei_remain` 不变。详见 `contracts/saolei-operate-contract.md` §1-3。含单测（双形态等价、批量顺序、三类失败、空/非法组合）。
- [ ] T006 [US1] 扩展 sink 接口：在 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` 将 `SaoleiEventSink.onMove(tool,x,y,state)` 替换为 `onOperate(operations: CellOperation[], finalState: GameState, stats?: GameStats)`（一次 `saolei_operate` 触发一次，携带全部 operations）；更新 `projects/game/agent/src/team/team-sink.ts` 的 `createTeamSink` 使 gameLog 每条记录为 `{tool:"saolei_operate", operations, status, state}`（一条 = 一次批量调用，FR-004，不再每 op 一条）。详见 `contracts/saolei-operate-contract.md` §4。含单测。
- [ ] T007 [US1] 更新 `projects/game/agent/src/team/planner.ts`：`GAME_VISIBLE_PLAYER_TOOLS` 改为 `["saolei_init","saolei_operate"]`；`buildToolDescriptionSection` 描述 `saolei_operate` 及其 click/flag/chord 操作类型（FR-005）；`buildReviewInput` 渲染 gameLog 每条为 `saolei_operate(operations) → status` + 棋盘。详见 `contracts/saolei-operate-contract.md` §4。含单测。
- [ ] T008 [US1] Run `bazel run //:gazelle`（更新 agent BUILD）、`bazel test //projects/game/agent/...`（saolei-mcp / team-sink / planner 相关 `*_test` target 全绿）；按 SC-001/SC-007 在单测层验证。

**Checkpoint**: US1 单测全绿；落子工具面仅 `saolei_operate`；gameLog/工具描述一致。可独立交付为 MVP。

---

## Phase 3: User Story 2（服务段）— memory Go gRPC 服务

**Goal**: 新建 `projects/game/memory/` grpc-go 服务（仿 `projects/game/prompt/`），独立数据库 `game_memory`，承载 Memory 资源 CRUD + 分页 List。

**Independent Test**: MemoryService 持久化记忆（独立 db、唯一索引、跨重启可读）；Create memory_id 重复 → ALREADY_EXISTS；Update/Delete 不存在 → NOT_FOUND；ListMemories 分页（AIP-158）。

### 文档清单（编码前必读）

- **代码规范文档**: `style/golang.md`；[Google Go Style Guide — Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)；`style/mongo.md`（独立数据库、`_id` 不覆盖、对象字段定义具体 model 不用 `bson.M`）；`style/api.md`；[AIP-122](https://google.aip.dev/122)、[AIP-127](https://google.aip.dev/127)、[AIP-131 Get](https://google.aip.dev/131)、[AIP-132 List](https://google.aip.dev/132)、[AIP-133 Create](https://google.aip.dev/133)、[AIP-134 Update](https://google.aip.dev/134)、[AIP-135 Delete](https://google.aip.dev/135)、[AIP-158 Pagination](https://google.aip.dev/158)、[AIP-193 Errors](https://google.aip.dev/193)
- **官方文档**: [go.mongodb.org/mongo-driver](https://pkg.go.dev/go.mongodb.org/mongo-driver)（mongo/options、bson）；[google.golang.org/grpc](https://pkg.go.dev/google.golang.org/grpc)（codes/status）；`protoc-gen-go-aip`（仓库内既有用法见 `projects/game/BUILD.bazel` 的 `:go_gen_aip` compiler，codegen 生成 `ParseMemoryName` 等）
- **技术文章**: `specs/039-planner-memory-calibration/contracts/memory-service-contract.md` §1-7（资源/RPC/mongo 文档/服务结构/部署/错误码/验证）；既有 `projects/game/prompt/` 全套（`cmd/main.go`、`handler/handler.go`+`handler_test.go`、`domain/{model,repository,errors}.go`、`runtime/mongo/{model,repository}.go`+`repository_test.go`、`service.yaml`、各 `BUILD.bazel`）为 file-for-file 模板

**Tasks**:

- [ ] T009 [US2] Create `projects/game/memory/domain/{model.go,repository.go,errors.go`：`Memory` 领域模型（对象语义→指针，`style/golang.md`）、`MemoryRepository` 接口（`CreateMemory`/`UpdateMemory`/`DeleteMemory`/`ListMemories(ctx, template, session string, pageSize int, pageToken string)`）、`ErrAlreadyExists`/`ErrNotFound`、`DefaultListMemoriesPageSize=100`/`MaxListMemoriesPageSize=1000` 常量。照搬 `prompt/domain` 结构。详见 `contracts/memory-service-contract.md` §3-4。
- [ ] T010 [P] [US2] Create `projects/game/memory/runtime/mongo/{model.go,repository.go}`：`memoryDocument`（bson 具体模型，字段 `template`/`session_id`/`memory_id`/`content`/`create_time`/`update_time`，`_id` 不覆盖，`style/mongo.md`）；`NewRepository(client, "game_memory")` 创建 `memories` 集合并建唯一索引 `(template, session_id, memory_id)`（仿 prompt `NewRepository` 内联建索引）；CRUD：duplicate-key→`ErrAlreadyExists`、`ErrNoDocuments`/Matched0/Deleted0→`ErrNotFound`；List 分页（pageToken 游标 + `limit=pageSize+1` 算 `nextPageToken`）。仿 `prompt/runtime/mongo`。含 `repository_test.go`（Create/Get/Update/Delete/List/重复/不存在，DI seam 仿 prompt）。
- [ ] T011 [P] [US2] Create `projects/game/memory/handler/handler.go`：`Handler` 内嵌 `game.UnimplementedMemoryServiceServer`；`Create/Update/Delete/List` 方法；name 解析用 codegen（`game.ParseMemoryName`/`req.ParseName()`）；`memory_id` 字符集校验 `[a-z0-9_-]+`（非法→`codes.InvalidArgument`，AIP-193）；`toStatusError`（`ErrAlreadyExists`→`codes.AlreadyExists`，`ErrNotFound`→`codes.NotFound`，else `codes.Internal`）；proto↔domain 转换；时间戳在 handler 设置。仿 `prompt/handler`。含 `handler_test.go`。
- [ ] T012 [US2] Create `projects/game/memory/cmd/main.go`（boot：`mongo.NewClient("game/mongo")` + db `"game_memory"` + `game.RegisterMemoryServiceServer` + `bootstrap`/`grpc`/`mongo`/`otel` + `reflection.Register`，照搬 `prompt/cmd/main.go` 顺序）与 `projects/game/memory/service.yaml`（`name: memory`、`app: game`、`kind: stateless`、port 50051、`artifacts: [{name: memory, target: :cmd_image, tls: true}]`，照搬 `prompt/service.yaml`）。
- [ ] T013 [US2] Create `projects/game/memory/` 各 `BUILD.bazel`（`memory/BUILD.bazel` 的 `artifact_pkg_go`+`artifact_image`、`cmd/`、`domain/`、`handler/`（含 `go_unittest`）、`runtime/mongo/`（含 `go_unittest`），照搬 prompt 对应 `BUILD.bazel`）；Run `bazel run //:gazelle projects/game/memory`、`bazel test //projects/game/memory/...`、`bazel build //projects/game/memory:cmd_image`。

**Checkpoint**: `bazel test //projects/game/memory/...` 全绿；memory 服务镜像可构建；独立 db `game_memory`、唯一索引、AIP 错误码、分页 List。

---

## Phase 4: User Story 2（agent 数据面）— memory client + memory mcp + mcp-host path 重构 + 冻结快照

**Goal**: agent 侧记忆数据面构件——`memory-client.ts`（gRPC）、`memory-mcp.ts`（planner 记忆工具）、mcp-host 按 (template,session,kind) 每路独立 template-scoped path（R3，含 saolei path 迁移）、`memory-snapshot.ts`（冻结快照）。均为可独立单测的构建块。

**Independent Test**: memory-client 转发到 memory 服务；memory mcp **单一 hermes 式 `memory` 工具**（`action`/`content`/`old_text`/`operations`，无 `memory_id`）、agent 转换（add 生成 id、replace/remove 经 listMemories + `old_text` 子串定位、0/多命中报错）、path 闭包注入 template/session、错误文本反馈（非异常）；mcp-host 按 template-scoped 多 path 路由两路 mcp；冻结快照 refresh/toSystemMessage（**纯内容**，无 `memory_id`）；skill-loader 注册 "memory" + memory SKILL.md 可加载。

### 文档清单（编码前必读）

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)；`specs/019-js-test-reliability/`（DI seam、`vitest_test` 宏、`data` 规则）
- **官方文档**: [@modelcontextprotocol/typescript SDK](https://github.com/modelcontextprotocol/typescript-sdk)（`McpServer` 工厂 + `registerTool`）；`@grpc/grpc-js` + `@grpc/proto-loader`（仓库内 `prompt-client.ts` 为模板：`registerDominionResolver`、keepalive/round_robin/TLS channel options）；[@langchain/langgraph — configurable / node return / messagesStateReducer](https://docs.langchain.com/oss/javascript/langgraph/use-graph-api)
- **技术文章**: `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md` §1-6；`specs/039-planner-memory-calibration/contracts/memory-skill-contract.md` §1-3（T015b）；`specs/039-planner-memory-calibration/contracts/team-graph-contract.md` §3（冻结快照）；[hermes `tools/memory_tool.py`](https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py)（`memory` 工具 schema 与 `old_text` 子串匹配语义的参考来源，memory-mcp-contract §1.1 与 memory-skill-contract §3 均引用其 HOW/WHEN/SKIP 引导，T015 实现 old_text 匹配、T015b 编写 skill body 措辞须参考）；`survey/planner-memory-and-agent-communication.md` §3/§4/§5（D2 冻结快照、D4 压缩刷新、D5 方案 b）；既有 `projects/game/agent/src/prompt-client.ts`（memory-client 模板）、`projects/game/agent/src/mcp-host.ts`（path/session 既有做法）、`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`（`createSaoleiMcpServer` 闭包工厂模式）、`projects/game/agent/src/skill-loader.ts`（skill 注入机制，T015b 注册 "memory"）、`projects/game/agent/src/skill/saolei/SKILL.md`（skill body 模板）、`specs/020-agent-resources-layout/contracts/skill-md-format.md`（SKILL.md 格式契约，T015b）

**Tasks**:

- [ ] T014 [P] [US2] Create `projects/game/agent/src/memory-client.ts`：`MEMORY_SERVICE_TARGET = "dominion:///game/memory:50051"`；`MemoryClient` 类（`createMemory(template, session, memoryId, content)`/`updateMemory`/`deleteMemory`/`listMemories(template, session): Promise<{memory_id, content}[]>`）；`registerDominionResolver` + proto-loader（`PROTO_PATH`/`PROTO_OPTIONS` 复用 prompt-client 常量）+ `KEEPALIVE_OPTIONS`/`ROUND_ROBIN_SERVICE_CONFIG`/`buildClientCredentials`/`buildChannelOptions`；构造器 DI seam（可选注入 `grpc.Client`，无 `vi.mock`）；`warmup`/`close`。照搬 `prompt-client.ts` 结构。详见 `contracts/memory-mcp-contract.md` §3。含单测（注入 fake client）。
- [ ] T015 [P] [US2] Create `projects/game/agent/src/mcp/memory/memory-mcp.ts`：`createMemoryMcpServer(memoryClient: MemoryClient, template: string, session: string): McpServer` 工厂（闭包绑定 template/session，FR-012）；注册**单一** `memory` 工具（hermes 式参数 `action`∈{add/replace/remove}/`content`/`old_text`/`operations`，**无 `memory_id`/无 `target`**，Session 2026-08-08）；agent 侧转换：`add`→`generateMemoryId(content)` + `createMemory`；`replace`/`remove`→`listMemories` + `matchBySubstring(entries, old_text)`（0 命中/多不同命中→错误文本含当前条目，全相同→作用首条）+ `updateMemory`/`deleteMemory`；`operations`→批量原子（v1 可选，单 op 已满足核心）；工具参数不含 template/session；错误→文本 result（非异常，031 C15 neutral status）；改存储**不刷新冻结快照**。详见 `contracts/memory-mcp-contract.md` §1-2。含单测（注入 fake memoryClient，断言转换调用 + 0/多命中错误文本）。
- [ ] T015b [P] [US2] Create `projects/game/agent/src/skill/memory/SKILL.md`（memory skill，FR-020；遵循 `specs/020-agent-resources-layout/contracts/skill-md-format.md` frontmatter `name: memory`+body）；内容覆盖 `contracts/memory-skill-contract.md` §3（工具用法/何时记/跳过什么/冻结快照模型/写作风格）。更新 `projects/game/agent/src/skill-loader.ts`：`BUILTIN_SKILL_NAMES` 加 `"memory"`（当前仅 `"saolei"`）。更新 agent 的 `artifact_pkg_js` `data_files` 含 `src/skill/memory/SKILL.md`（同 saolei skill 既有 data_files 模式）。详见 `contracts/memory-skill-contract.md` §1-2。含单测（`loadSkillBody("memory")` 返回非空 body；frontmatter 合法）。
- [ ] T016 [US2] 重构 `projects/game/agent/src/mcp-host.ts`：每 mcp 独立 template-scoped path（`/internal/mcp/:template/:session/saolei` 与 `/internal/mcp/:template/:session/memory`，R3）；`getOrCreateSession` 按 `(template, session, kind)` 懒创建对应 `McpServer`（saolei→`createSaoleiMcpServer`，memory→`createMemoryMcpServer`）；扩展 `SessionBridgeLookup` 同时提供 saolei `{bridge, sink}` 与 memory `{memoryClient, template, session}`；路由 handler 按 path 段分发；既有 saolei 接线（`buildSaoleiMcpTools` 连接 URL）同步迁移到新 template-scoped path（clean break）。详见 `contracts/memory-mcp-contract.md` §4。含单测（两路 path 路由 + 懒创建）。
- [ ] T017 [P] [US2] Create `projects/game/agent/src/team/memory-snapshot.ts`：`FrozenMemorySnapshot` 类（`entries: {memory_id, content}[]`、`bakedAt`；`async refresh(memoryClient, template, session)` 经 `listMemories` 重读并按页遍历至 `next_page_token` 空；`toSystemMessage(): BaseMessage` → `new SystemMessage({id: "planner-memory-snapshot", content: "长期记忆：\n<内容> ..."})`，**纯内容**呈现（无 `memory_id` 前缀，FR-011/Session 2026-08-08）；`memory_id` 仅存于 entries 供内部定位，不进 LLM 文本）。详见 `contracts/team-graph-contract.md` §3、survey D2/D5。含单测（refresh 分页遍历 + toSystemMessage 纯内容格式）。
- [ ] T018 [US2] Run `bazel run //:gazelle`、`bazel test //projects/game/agent/...`（memory-client / memory-mcp / mcp-host / memory-snapshot 相关 `*_test` 全绿）。

**Checkpoint**: 记忆数据面构建块单测全绿；mcp-host 双路 template-scoped path 可路由；memory mcp 经 client 转发不直连。

---

## Phase 5: User Story 2（agent 装配）— planner 记忆工具 + 冻结快照注入 + graph/compress + server wiring

**Goal**: 将记忆数据面接入 team graph——planner 持 memory 工具 + 冻结快照（input SystemMessage，替换原 strategy 读取）、`TeamState` 加 `pendingInstruction` 通道、compress 边界刷新快照、server.ts 装配 MemoryClient + 每 session memory mcp。

**Independent Test**: planner 持且仅持**单一 `memory` 工具**（hermes 式，无 `memory_id`；无 update_strategy，update_strategy 在 Phase 6 删）；冻结快照作为 input SystemMessage 注入（**纯内容**，无 `memory_id`）、不烘焙进 systemPrompt；planner 系统提示词含 memory skill body（player 不含）；review 不刷新、compress 刷新；memory 工具改存储不刷新快照。

### 文档清单（编码前必读）

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)；`specs/019-js-test-reliability/`
- **官方文档**: [@langchain/langgraph — use-graph-api](https://docs.langchain.com/oss/javascript/langgraph/use-graph-api)（node 返回值→reducer、configurable、`MessagesValue`/`messagesStateReducer`）；[@langchain/langgraph — checkpointers](https://docs.langchain.com/oss/javascript/langgraph/checkpointers)（`graph.updateState`，RefreshTeam 用）；[@langchain/langgraph — add-memory](https://docs.langchain.com/oss/javascript/langgraph/add-memory)（`messagesStateReducer`、id 过滤模式）
- **技术文章**: `specs/039-planner-memory-calibration/contracts/team-graph-contract.md` §1（TeamState + pendingInstruction）、§2.2（review 节点）、§2.4（compress 刷新）、§3（冻结快照纯内容注入）、§5（策略/记忆流）、§7（RefreshTeam）；`specs/039-planner-memory-calibration/contracts/memory-skill-contract.md` §2（T020 planner skill 装配）；`survey/planner-memory-and-agent-communication.md` §4.2（retain-vs-rebuild 可选优化）；既有 `projects/game/agent/src/team/{graph,planner,compress,state,team-sink}.ts`、`projects/game/agent/src/server.ts`（`buildSaoleiMcpTools` 模式用于 `buildMemoryMcpTools`）、`projects/game/agent/src/skill-loader.ts`（`appendSkillBodyToPrompt` 既有用法，T020 复用）

**Tasks**:

- [ ] T019 [US2] 更新 `projects/game/agent/src/team/state.ts` 与 `projects/game/agent/src/team/graph.ts`：`TeamStateValue`/`TeamState` 新增 `pendingInstruction: Annotation<string|null>({reducer: overwrite, default: () => null})`（D10）；`TeamGraphDeps` 以 `memoryClient: MemoryClient` + `frozenSnapshot: FrozenMemorySnapshot` 替换 `strategyStore`（本 phase 暂保留 strategyStore 仅供 update_strategy 写路径，Phase 6 删——确保中间态可编译）。详见 `contracts/team-graph-contract.md` §1。
- [ ] T020 [US2] 更新 `projects/game/agent/src/team/planner.ts`（review 节点）：`PlannerNodeDeps` 以 `memoryClient`+`frozenSnapshot`+memory mcp tools 替换 strategy 读取；input = `[frozenSnapshot.toSystemMessage(), ...plannerMessages, reviewInput]`（**不再** `buildStrategyMessage`）；memory 工具经 mcp client 取得（planner createAgent tools = **单一 `memory` 工具** + 既有 review 用工具；instruct_player 在 Phase 6 加）；**systemPrompt 装配 memory skill**：`appendSkillBodyToPrompt(base, ["memory"]) + buildToolDescriptionSection(...)`（FR-020，与 player 的 `appendSkillBodyToPrompt(base, ["saolei"])` 对称；当前 planner 不调 appendSkillBodyToPrompt，此处补）；写回时过滤 `planner-memory-snapshot` id（不进 plannerMessages 短期通道）。**冻结快照不在 review 刷新**（FR-010）。详见 `contracts/team-graph-contract.md` §2.2/§3、`contracts/memory-skill-contract.md` §2。含单测（断言 planner systemPrompt 含 memory skill body + SKILL_PROMPT_SEPARATOR）。
- [ ] T021 [P] [US2] 更新 `projects/game/agent/src/team/compress.ts`：压缩后（review 之后、END 之前）触发 `frozenSnapshot.refresh(memoryClient, template, session)`（重读 ListMemories → 重新烘焙，调研 D4）。详见 `contracts/team-graph-contract.md` §2.4。含单测。
- [ ] T022 [US2] 更新 `projects/game/agent/src/server.ts`：构造 `new MemoryClient()` + `warmup()`；在 `SessionTeamStore` factory 内为每 session 建 `FrozenMemorySnapshot`（首次 `refresh`）+ memory mcp（经 `buildMemoryMcpTools(...)` 仿 `buildSaoleiMcpTools`，client 连 mcp-host memory path）+ 将 planner memory 工具/snapshot 传入 `buildTeamGraph`；扩展 `sessionBridges`/`SessionBridgeLookup` 提供 memory 装配（供 mcp-host）。**040 重建闭包（`SessionTeamStore` 第二构造参数 rebuilder，`server.ts:327-365` 的 `buildTeamGraph` 第二调用点 `:352`）MUST 同步装配**：memoryClient/frozenSnapshot/memory 工具对首建（`:288`）与重建两处调用点一致注入——profile 变更重建后的 graph 其 planner 须同样持有记忆工具与冻结快照（buffer/bridge/sink/playerTools/mcp 复用既有 session 装配，仿 `server.ts:343-351` 复用模式）；重建闭包的 strategyStore 接线在 T030 移除。详见 `contracts/team-graph-contract.md` §5、`contracts/memory-mcp-contract.md` §4。含单测（重建后 planner 仍持 memory 工具/冻结快照）。
- [ ] T023 [US2] Run `bazel run //:gazelle`、`bazel test //projects/game/agent/...`（graph/planner/compress/server 相关 `*_test` 全绿）；按 SC-002/SC-003/SC-006/SC-009 在单测层验证（独立 db 由 Go 服务保证；冻结快照冻结语义与纯内容注入格式；mcp 经 agent 转发不直连；memory skill 注入 planner 且 player 不含）。

**Checkpoint**: planner 记忆工具 + memory skill + 冻结快照注入可用；`pendingInstruction` 通道就位；compress 边界刷新；server 装配 memory 全链路。SC-002/SC-003/SC-006/SC-009 单测层达成。

---

## Phase 6: User Story 3 — 校准指令 + StrategyStore 移除 + 两场景节点 + 异步 init

**Goal**: 新建 `instruct_player` 指令工具（外部 buffer 中转 R1）+ init/compact 两场景节点（prompt 引导、无游戏历史、不触发 player invoke、写 pendingInstruction）；player 入口消费 pendingInstruction；`UpdateTeam(allow_missing=true)` 物化（graph 首建）异步触发 initInstruction（R2，typing-state 同步 + user message 排序；profile 变更重建不重跑 init——原 CreateTeam 触发点被 040 supersede）；彻底移除 StrategyStore/update_strategy/player 当前态势注入。

**Independent Test**: player 不再读策略；planner 持 instruct_player；正常复盘 prompt"必要时才调用"（可选、同 turn 紧跟 tool_result 注入、player 继续）；init/compact prompt 引导无历史产出（LLM 决定）、不触发 player invoke（压缩后 turn 结束、init 异步随首次激活注入、期间 user message 排指令后）；`StrategyStore`/`update_strategy`/当前态势注入全部移除无残留引用。

### 文档清单（编码前必读）

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)；`specs/019-js-test-reliability/`
- **官方文档**: [@langchain/langgraph — use-graph-api](https://docs.langchain.com/oss/javascript/langgraph/use-graph-api)（node 返回值写通道、configurable 暂存、条件边）；[@langchain/langgraph — checkpointers](https://docs.langchain.com/oss/javascript/langgraph/checkpointers)（`updateState`/RefreshTeam）
- **技术文章**: `specs/039-planner-memory-calibration/contracts/team-graph-contract.md` §2.1（player 消费 pending）、§2.3（init/compact 节点）、§2.5（条件边）、§4（instruct_player 外部 buffer R1）、§6（异步 init）、§7（RefreshTeam 清 pending）、§8（验证要点）；`survey/planner-memory-and-agent-communication.md` §5/§6（D6 HumanMessage / 外部 buffer 中转，同 037 `emitChannelFrame`）；既有 `projects/game/agent/src/team/{player,planner,graph,state,compress}.ts`、`projects/game/agent/src/session-team.ts`（`SessionTeamStore.update` 异步 hook、`submit` TurnLoop 队列）、`projects/game/agent/src/server.ts`、`projects/game/agent/src/strategy-store.ts` + `projects/game/agent/src/team/update-strategy.ts`（待删）

**Tasks**:

- [ ] T024 [US3] Create `projects/game/agent/src/team/instruction-tool.ts`：`instruct_player` 工具（`schema: {content: z.string()}`），经 `configurable` 提供的外部 buffer 暂存（`stageInstruction(content)`，R1——同 037 `emitChannelFrame` 模式），工具返回 `{ok:true}`（不直写外层通道）。仅供节点在 `createAgent.invoke` 返回后读暂存写通道。详见 `contracts/team-graph-contract.md` §4。含单测。
- [ ] T025 [US3] Create `projects/game/agent/src/team/instruction-node.ts`：`createInstructionNode`（参数区分 scenario `init`|`compact`）共享节点函数——planner 仅依冻结快照、经 prompt **要求**给 player 指令（无 gameLog，LLM 决定是否调用 `instruct_player`，R4 无强制检验）；节点在 invoke 返回后读外部 buffer 暂存，有则写 `TeamState.pendingInstruction`（不触发 player invoke）；prompt 措辞与 review（"必要时才调用"）区分。详见 `contracts/team-graph-contract.md` §2.3。含单测。
- [ ] T026 [US3] 更新 `projects/game/agent/src/team/graph.ts`：加 `initInstruction`（`START → initInstruction → player`）与 `postCompactInstruction`（`compress → postCompactInstruction → END`）节点与边（用 T025 节点函数）；既有 `routeAfterPlanner` 条件边（`graph.ts:199`）不变（非压缩→player）；condition 边编排保证 `review → compress（清通道+刷新快照）→ postCompactInstruction → END`（R5）。详见 `contracts/team-graph-contract.md` §2。
- [ ] T027 [US3] 更新 `projects/game/agent/src/team/planner.ts`（review 节点）：加 `instruct_player` 工具到 planner createAgent tools；review prompt 措辞"必要时才调用"（可选）；节点在 invoke 返回后读外部 buffer 暂存，有则由节点返回值写 `{playerMessages:[new HumanMessage(content)]}`（紧跟游戏结束 tool_result，FR-017 顺序）；无则不产生指令、graph 仍路由回 player。详见 `contracts/team-graph-contract.md` §2.2/§4。含单测。
- [ ] T028 [P] [US3] 更新 `projects/game/agent/src/team/player.ts`：移除 `buildStrategyMessage`/`STRATEGY_MESSAGE_ID`/`strategyStore.get`（FR-013，player 不再读策略）；入口读 `state.pendingInstruction`，非空则作为 `HumanMessage` 注入 `playerMessages` 后返回 `{pendingInstruction: null}` 清空（FR-015/FR-016"与下次激活一同注入"）。详见 `contracts/team-graph-contract.md` §2.1。含单测。
- [ ] T029 [US3] 更新 `projects/game/agent/src/session-team.ts`：`SessionTeamStore.update`（040：`UpdateTeam(allow_missing=true)` 物化路径——原 `SessionTeamStore.create` 被 [`specs/040-team-singleton-conformance/`](../../040-team-singleton-conformance/) supersede）在 team graph **首建**后**异步**触发一次 `initInstruction`（R2，不等 LLM、`UpdateTeam` 物化即返回；**profile 变更重建（040 FR-005）不重跑 initInstruction，仅首建触发**）；协调与 desktop Connect 的 typing-state 时序，且异步产出期间到达的 user message 经 TurnLoop 队列排在 pendingInstruction 之后（player 首次激活先注入 pending 指令）；`RefreshTeam` 经 `graph.updateState` 同时清 `pendingInstruction`（避免过期残留，contract §7）。详见 `contracts/team-graph-contract.md` §6/§7。含单测。
- [ ] T030 [US3] 更新 `projects/game/agent/src/server.ts`：移除 `MongoStrategyStore` 构造 + `ensureIndexes`（FR-013）；移除 team deps 中 strategyStore 接线——**两处 `buildTeamGraph` 调用点均须移除**：首建 factory（`:288`）与 040 重建闭包 rebuilder（`:352`），否则 T031 删除 `strategy-store.ts` 后重建闭包残留引用致编译失败；为 planner 装配指令暂存 buffer（经 `configurable` 注入 `stageInstruction`）；接 `instruct_player` 工具到 planner。
- [ ] T031 [US3] 删除 `projects/game/agent/src/strategy-store.ts` 与 `projects/game/agent/src/team/update-strategy.ts`；清除全部 import 残留（player/planner/graph/server）；移除 `STRATEGIES_COLLECTION` 引用；Run `bazel run //:gazelle` 更新 BUILD（移除已删文件 target）。
- [ ] T032 [US3] Run `bazel build //projects/game/agent/...` + `bazel test //projects/game/agent/...`（team 全套 `*_test` 全绿）；按 SC-004/SC-005 验证（player 无策略读取、指令两场景、StrategyStore 无残留引用——全仓 `rg` 确认 `StrategyStore`/`update_strategy`/`buildStrategyMessage` 零命中）。

**Checkpoint**: US3 单测全绿；指令工具 + 两场景节点 + 异步 init + player pending 消费就位；StrategyStore 全量移除无残留（SC-004/SC-005）。

---

## Phase 7: Polish & 大型测试验收（FR-018，宪法原则 VI）

**Purpose**: 大型测试（经 testplan skill 完整执行）+ 全量构建/单测 + 文档同步。

### 文档清单（编码前必读）

- **代码规范文档**: `style/large_test.md`（**每个被测系统只维护一份测试计划 YAML**；用例按**模块**拆分非按 spec/场景编号；`go_largetest` rule；`guitar run <plan.yaml>`）；`style/golang.md`（大型测试遵守单测命名/表驱动/given-when-then 规范）；[Google Go Style Guide — Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)
- **官方文档**: 无
- **技术文章**: `specs/039-planner-memory-calibration/quickstart.md` 场景 1/2/3 + 大型测试执行段；既有 `projects/game/testplan/system_test.yaml`（既有 9 suite，**在此计划加 suite/case，禁新建 YAML**）、`deploy_agent.yaml`、`saolei_team_test.go`、`helpers_test.go`（既有 helper/fixture 模式，复用 `createTeamProfile`/`setupTeamSession`/`sendUserTurn`/`drainUntilWait` 等）；`projects/game/fake-llm/service/testdata/`（fixture 契约）+ `projects/game/testplan/README.md` §3/§5（fake-llm fixture 与 helpers 常量 lockstep）；testplan SKILL（仓库内 `.opencode/skills/testplan/SKILL.md`，`tools/test/guitar`）；`pkg/testtool`（仓库内 `common/gopkg/testtool`，`MustEndpoint`/`MustEnv`）；`go_largetest` rule（仓库内 `tools/dev/go:defs.bzl`）

**Tasks**:

- [ ] T033 [P] 编写大型测试用例（Go，`*_test.go` 按**被测模块**拆分，复用 `helpers_test.go`）于 `projects/game/testplan/`：① saolei_operate **双形态**（普通参数单次 / `operations` 数组）与失败细分（归入既有 `agent_saolei_test.go` 或 saolei 模块文件）；② memory 持久化 + 冻结快照（纯内容）+ `memory` 工具 agent 转换（`old_text` 0/多命中报错）+ memory skill 注入 planner（player 不含）+ 独立 db + 分页（新建 `memory_test.go` 模块文件）；③ 校准指令两场景 + 消息顺序 + StrategyStore 移除（归入既有 `saolei_team_test.go` team 模块）。用例通过 HTTP/WS 公共入口（`testtool.MustEndpoint("http","public")`）+ 设置/打印 `trace_id`（`tracecontext`）。详见 `quickstart.md` 场景 1/2/3、`style/large_test.md`。
- [ ] T034 [P] Add/update fake-llm fixtures 于 `projects/game/fake-llm/service/testdata/`（脚本化**单一 `memory` 工具**（`action`/`content`/`old_text`/`operations`）+ `instruct_player` 的 tool_call 响应、两场景 prompt 引导产出），并同步 `helpers_test.go` 期望常量（lockstep，`testplan/README.md` §5）；确认 `projects/game/fake-llm/service/message_store_test.go` pin 通过。
- [ ] T035 更新 `projects/game/testplan/BUILD.bazel`：声明新 `go_largetest` target（按 `style/large_test.md` gazelle 默认名规则——同目录至少一个用 `{package_name}_test`，避免重复生成 `go_unittest`）；`srcs` 含 `helpers_test.go` + 对应模块 `*_test.go` + `saolei_fixtures_test.go`（按需）+ `embedsrcs` testdata。
- [ ] T036 更新 `projects/game/testplan/system_test.yaml`：新增 `suite`（如 `planner-memory`）引用新 `go_largetest` case（**不新建 YAML**；如沿用既有 deploy 拓扑则复用 `deploy_agent.yaml`，该 deploy 已含 memory 服务条目——Phase 1 T003 已加）。
- [ ] T037 Run `bazel build //...` + `bazel test //...`（全量 Go + TS 编译与单测全绿）；经 **testplan SKILL** 执行 `guitar run projects/game/testplan/system_test.yaml`（完整部署→测试→清理闭环，**禁止仅 `bazel build` 替代验收**，宪法原则 VI）；**所有用例全部通过**（failed/flaky 即未通过，修复重跑至全绿）。

**Checkpoint**: 全量 `bazel build/test` 通过；`guitar run` 全部用例通过（FR-018、SC-008）；特性验收完成。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: 无依赖；产出 proto codegen + 常量 + 部署条目。**BLOCKS** US2/US3。
- **Phase 2 (US1, MVP)**: 仅依赖 TS 规范；**不依赖** Phase 1 proto（saolei_operate 与 memory 解耦）。可与 Phase 3 并行。
- **Phase 3 (US2 服务)**: 依赖 Phase 1（proto codegen）。可与 Phase 2 并行。
- **Phase 4 (US2 agent 数据面)**: 依赖 Phase 1（proto）+ Phase 3（memory 服务，集成验证用）。
- **Phase 5 (US2 agent 装配)**: 依赖 Phase 4（client/mcp/snapshot 构建块）。
- **Phase 6 (US3)**: 依赖 Phase 5（planner 已持 memory 工具 + 冻结快照；StrategyStore 中间态待清）。
- **Phase 7 (大型测试)**: 依赖 Phase 2/3/5/6 全部完成（端到端可测）。

### User Story Dependencies

- **US1 (P1)**: 独立，Phase 2 后即可作 MVP 交付/验证。
- **US2 (P1)**: 跨 Phase 3+4+5（Go 服务 → agent 数据面 → 装配）；依赖 Phase 1 proto；与 US1 解耦。
- **US3 (P1)**: 依赖 US2 agent 装配（Phase 5，planner 记忆能力作为指令认知来源 + StrategyStore 中间态）；Phase 6 完成移除 StrategyStore。

### Within Each User Story

- 模型/接口 → 仓储/handler → 服务装配（Go）；构建块 → 装配（agent）。
- 编译 + 单测随每个代码变更 task 执行（宪法原则 IV）。
- 大型测试在 Phase 7 统一执行（宪法原则 VI）。

### Parallel Opportunities

- Phase 1: T002/T003 与 T001 不同文件可并行（T001 改 proto，T002/T003 改 const/deploy）；T004 在 T001-T003 后。
- Phase 2: 内部顺序（saolei-mcp → team-sink → planner），与 Phase 3 整体并行。
- Phase 3: T010（runtime/mongo）与 T011（handler）不同目录、均仅依赖 T009（domain），可并行。
- Phase 4: T014（memory-client）先行；T015（memory-mcp）、T017（snapshot）均仅依赖 T014，可并行；T015b（skill + loader）独立于 memory-client，可与 T015/T017 并行；T016（mcp-host）依赖 T015。
- Phase 6: T024（instruction-tool）先行；T028（player）与 T025/T027 可并行（不同文件）。
- Phase 7: T033（用例）与 T034（fixture）可并行（不同目录）。

---

## Parallel Example: Phase 3（US2 服务）

```bash
# domain 先行
Task: "T009 domain model/repository/errors in projects/game/memory/domain/"
# 以下两者不同目录、均仅依赖 domain，可并行：
Task: "T010 runtime/mongo repository + repository_test in projects/game/memory/runtime/mongo/"
Task: "T011 handler + handler_test in projects/game/memory/handler/"
```

## Parallel Example: Phase 4（US2 agent 数据面）

```bash
# memory-client 先行
Task: "T014 memory-client.ts"
# 以下均仅依赖 memory-client（或独立），可并行：
Task: "T015 mcp/memory/memory-mcp.ts"
Task: "T017 team/memory-snapshot.ts"
Task: "T015b skill/memory/SKILL.md + skill-loader.ts 注册（独立于 memory-client，可与 T015/T017 并行）"
```

---

## Implementation Strategy

### MVP First（仅 US1）

1. Phase 1 Setup（proto/const/deploy）。
2. Phase 2 US1（saolei_operate）——独立交付、单测验证（SC-001/SC-007）。
3. **STOP and VALIDATE**：US1 端到端可在 Phase 7 大型测试中验证（或临时手测）。

### Incremental Delivery

1. Phase 1 Setup → proto codegen 就位。
2. Phase 2 US1 → 批量落子 MVP。
3. Phase 3 US2 服务 → memory Go 服务可独立 build/test（SC-002 服务段）。
4. Phase 4 US2 agent 数据面 → 构建块单测绿。
5. Phase 5 US2 agent 装配 → planner 记忆工具 + memory skill + 冻结快照可用（SC-002/SC-003/SC-006/SC-009）。
6. Phase 6 US3 → 指令循环 + StrategyStore 移除（SC-004/SC-005）。
7. Phase 7 大型测试 → 全部用例通过（SC-008，FR-018）。

### Parallel Team Strategy

- Developer A: Phase 2（US1）。
- Developer B: Phase 3（US2 服务，依赖 Phase 1 完成后启动）。
- Phase 4/5/6 串行（agent team 层耦合 planner.ts/graph.ts，避免并发编辑同文件）。
- Phase 7 在所有功能 phase 完成后统一执行。

---

## Notes

- [P] task = 不同文件、无未完成依赖；同文件多变更须串行（避免并发编辑丢失）。
- [Story] 标签映射到 spec.md 用户故事；Setup/Polish phase 无 story 标签。
- 每个代码变更 task 内含其编译 + 单测（`bazel build`/`bazel test` 相关 target），不单列。
- 大型测试**仅在 Phase 7** 经 testplan SKILL 执行（`guitar run`，完整部署→测试→清理），禁止仅 `bazel build` 替代验收（宪法原则 VI）。
- 外部 AIP/LangGraph/SDK 文档已在各 phase 文档清单显式列出（含 `style/api.md` 这类索引文件所引用的具体 AIP）；AGENTS.md 与 spec 文件为代码开发必读，不再重复。
- clean break：不考虑 `strategies` 集合数据迁移（FR-013，Assumptions）；开发/测试环境重建。
- 间接引用已显式展开：`style/api.md`（索引）→ 具体各 AIP（Phase 1 标准方法 AIP-133/134/135/132/158/193、字段行为 AIP-203 均已展开）；`style/golang.md`/`style/javascript.md` → Google 风格指南；`style/large_test.md` → testplan SKILL/guitar；survey → hermes/openclaw 结论（survey 已为权威综合，不需再溯外部 hermes/openclaw 仓库）；**契约直接引用的** hermes `memory_tool.py`（memory-mcp-contract §1.1、memory-skill-contract §3）已在 Phase 4 显式列出。
