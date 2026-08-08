# Tasks: Team 单例 AIP-156 一致化

**Input**: Design documents from `/specs/040-team-singleton-conformance/`

**Prerequisites**: [plan.md](plan.md)（required）、[spec.md](spec.md)（required，用户故事）、[research.md](research.md)、[data-model.md](data-model.md)、[contracts/api-contract.md](contracts/api-contract.md)、[contracts/team-rebuild-contract.md](contracts/team-rebuild-contract.md)、[quickstart.md](quickstart.md)

**Tests**: 编译 + 单测属每个代码变更任务的一部分（宪法原则 IV，不单列 task）；大型测试单列为 Phase 4 验收 task（宪法原则 VI）。

**Organization**: 按交付增量组织。Phase 1 为 proto 契约（共享前置）；Phase 2 为 US1+US2 MVP（单例物化 + 幂等/偏离消除——二者共享 `UpdateTeam` 路径，不可分割）；Phase 3 为 US3（profile 变更 graph 重建，建立在 MVP 之上）；Phase 4 为文档同步 + 大型测试验收。

> **US1/US2 耦合说明**：US2（幂等/多标签并发/消除 AIP-133 偏离）没有独立代码路径——`allow_missing` 既是物化机制（US1）也是幂等机制（US2），且 `TeamAlreadyExistsError`/ALREADY_EXISTS 的移除是删除 CreateTeam 的必然结果。故 US1+US2 合为 Phase 2 一个切片交付；US2 的验收由该切片内的 proxy `assignOwner`（不变，竞态收敛）+ agent 幂等/删 ALREADY_EXISTS + desktop 单次 update 共同满足。

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 归属用户故事（US1/US2/US3）；Setup/Foundational/Polish phase 无 story 标签
- 描述含确切文件路径

## Path Conventions

多服务 web 应用（gRPC 微服务）：Go 服务在 `projects/game/<service>/`，TS agent 在 `projects/game/agent/src/`，proto 在 `projects/game/game.proto`，大型测试在 `projects/game/testplan/`。

---

## Phase 1: Foundational（proto 契约 + codegen）

**Purpose**: proto 契约变更是所有用户故事的前置（接口优先，宪法原则 III）。

### 文档清单（编码前必读）

- **代码规范文档**: `style/api.md`（AIP 索引 + API 基础规则——REST 风格 / RPC 用 grpc 协议 / HTTP 用 google apis 注解 / Service 注释含 Prefix Path，须配合下列具体 AIP）；[AIP-156 Singleton resources](https://google.aip.dev/156)；[AIP-134 Standard methods: Update（create-or-update / allow_missing）](https://google.aip.dev/134#create-or-update)；[AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)；[AIP-203 Field behavior documentation](https://google.aip.dev/203)；[AIP-122 Resource names](https://google.aip.dev/122)
- **官方文档**: 无（本 phase 无第三方依赖的官方文档）
- **技术文章**: [Access Approval Settings proto（googleapis）](https://github.com/googleapis/googleapis/blob/master/google/cloud/accessapproval/v1/accessapproval.proto)（跨服务单例 Get+Update 无 Create 的实践范例）
- **仓库内参考文档**（补充显式列出，非上述三分类）：`projects/game/game.proto`（既有 `TeamProfile` 资源 + `UpdateTeamProfileRequest`/`UpdateTeamProfile` RPC 为 Update 模式模板，照搬其 `google.api.http` PATCH + `body` + `method_signature` + `google.api.resource_reference` 注解模式）

**Tasks**:

- [X] T001 修改 `projects/game/game.proto`：(1) 删除 `rpc CreateTeam`（约 `:71-77`）与 `message CreateTeamRequest`（约 `:770-789`）；(2) TeamService 新增 `rpc UpdateTeam(UpdateTeamRequest) returns (Team)`（AIP-134，PATCH `/api/v1/{team.name=templates/*/sessions/*/team}`，`body: "team"`，`method_signature: "team,update_mask"`），置于 TeamService 首位；(3) `message Team`（约 `:229-240`）新增 `string profile = 2 [(google.api.resource_reference) = { type: "game.liukexin.com/TeamProfile" }]`，`agents`→field 3、`create_time`→field 4；(4) 新增 `message UpdateTeamRequest { Team team = 1 [REQUIRED]; google.protobuf.FieldMask update_mask = 2; bool allow_missing = 3; }`（置文件末尾请求区）；(5) 重写 CreateTeam/Team 相关注释为"AIP-156 单例，经 UpdateTeam(allow_missing=true) 物化"——注释 MUST 携带 AIP-156/134 完整 URL（宪法原则 I，照既有注释惯例，如 `game.proto:153`）。字段号与语义详见 [data-model.md](data-model.md) §2-3、[contracts/api-contract.md](contracts/api-contract.md) §2。修改后 `bazel build //projects/game` 验证 codegen 通过。
- [X] T002 运行 `bazel run //:gazelle projects/game` 重新生成 proto codegen 的 `BUILD.bazel` target；`bazel mod tidy`；`bazel build //projects/game` 验证 codegen 产出 `UpdateTeam`（`TeamServiceServer`/`TeamServiceClient` 方法 + grpc-gateway PATCH 路由）、`UpdateTeamRequest`、`Team.profile` 字段。**Checkpoint**：codegen 可用，下游服务可引用 `UpdateTeam`/`UpdateTeamRequest`。

---

## Phase 2: US1 + US2 — 单例物化 + 幂等/偏离消除（MVP）🎯

**Goal**: Team 成为 AIP-156 单例——移除 CreateTeam，全链路改 `UpdateTeam(allow_missing=true)` 物化（US1）；重复/并发物化幂等收敛、消除 TeamAlreadyExistsError/ALREADY_EXISTS 偏离（US2）。

**Independent Test**: 对新会话 `UpdateTeam(allow_missing=true, profile=P)` 物化成功且 `GetTeam` 读回 profile=P；重复同 profile 幂等返回；多标签并发收敛、无 ALREADY_EXISTS。详见 [quickstart.md](quickstart.md) 场景 1/2/4。

### 文档清单（编码前必读）

- **代码规范文档**: `style/api.md` + [AIP-156 Singleton resources](https://google.aip.dev/156)（US1 单例语义）+ [AIP-134 create-or-update](https://google.aip.dev/134#create-or-update) + [AIP-193 Errors](https://google.aip.dev/193)（错误码语义）；`style/golang.md`（proxy/desktop Go）+ 其引用的 [Google Go Style](https://google.github.io/styleguide/go/)（入口索引；[Style Guide](https://google.github.io/styleguide/go/guide) 规范必读、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)）；`style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) + 其内嵌引用的 [vitest module mocking pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（agent TS；涉及模块级 mock 时必读）
- **官方文档**: [grpc-gateway（HTTP transcoding，body 字段绑定 vs query 参数）](https://github.com/grpc-ecosystem/grpc-gateway)；[@grpc/node（grpc-js 服务定义/proto-loader）](https://grpc.io/docs/languages/grpc/node/)
- **技术文章**: 无
- **仓库内参考文档**（补充显式列出，非上述三分类）：[contracts/api-contract.md](contracts/api-contract.md) §2（UpdateTeam 行为矩阵/校验/并发与 owner）

**Tasks**:

- [X] T003 [P] [US1] 修改 `projects/game/proxy/runtime/agentclient/client.go`：`Client` 接口方法 `CreateTeam(ctx, *game.CreateTeamRequest)`→`UpdateTeam(ctx, *game.UpdateTeamRequest) (*game.Team, error)`；`AgentClient` 包装改为转发 `c.client.UpdateTeam(ctx, req)`；删除 CreateTeam 转发。编译时接口检查 `var _ Client = (*AgentClient)(nil)` 须通过。
- [X] T004 [US1] 修改 `projects/game/proxy/handler/handler.go`：(1) `CreateTeam` handler（约 `:63-88`）改为 `UpdateTeam`：解析 `req.GetTeam().GetName()`（`game.ParseTeamName`，替代原 `game.ParseSessionName(req.GetParent())`）；**proxy 为路由层，始终 `assignOwner`**（get-or-create 路由解析，唯一 owner 分配点不变，竞态重读保留，[research.md](research.md) §R10）后转发 `client.UpdateTeam(ctx, req)` + `propagateAgentError`；**proxy 不 inspect `allow_missing`**——allow_missing 是 Team 资源语义，由 agent `SessionTeamStore.update` 处理（缺失+true→物化、缺失+false→NOT_FOUND、既有幂等/重建）。(2) `Connect`/`GetTeam`/`ListMessages`/`RefreshTeam` 的 `lookupOwner`（`:255-266`）**不变**（未物化仍 NOT_FOUND）。同步适配 `projects/game/proxy/handler/handler_test.go`：`TestCreateTeam`（`:296-436`）→`TestUpdateTeam`（mock 加 `updateTeamResult/Err`、断言 `lastUpdateTeamReq`；物化/assignOwner 复用/竞态重读/Unavailable/InvalidArgument/下游错误传播 用例 1:1 保留；错误用例由"invalid parent"改为"invalid team name"；**移除 allow_missing=false+无 owner→NOT_FOUND 用例**（NOT_FOUND 由 agent 层返回，agent 测试覆盖）；**新增 allow_missing 原样透传用例**（断言 `lastUpdateTeamReq.GetAllowMissing()` 与入参一致））。运行 `bazel test //projects/game/proxy/...`。
- [X] T005 [P] [US1] 修改 `projects/game/agent/src/handler.ts`：(1) `CreateTeam` handler（`:93-158`）改为 `UpdateTeam`：解析 `team.name`（既有 `parseTeamName`，`handler.ts:734-744`）+ profile（`parseProfileName`）；template 段一致性校验（FR-008）；**删除 `TeamAlreadyExistsError`→ALREADY_EXISTS 分支**（`:131-157`，偏离消除）；按 `allow_missing` + 缺失/既有分派 store upsert（缺失+allow_missing→创建；**缺失+allow_missing=false→NOT_FOUND**（AIP-134 标准 Update 语义，[data-model.md](data-model.md) §4）；既有+同 profile→幂等；既有+异 profile→**MVP 临时**抛错映射 FAILED_PRECONDITION "profile change rebuild pending (US3)"，US3 替换）；响应 `buildTeamResource` 含 `profile`（FR-004）。(2) `GetTeam`/`Connect`/`ListMessages`/`RefreshTeam` 的 NOT_FOUND 不变量不变（细节文案"call CreateTeam first"改为"provision via UpdateTeam"）。同步适配 `projects/game/agent/src/handler.test.ts`（`:304-444`）：CreateTeam 用例→UpdateTeam（物化成功/**缺失+allow_missing=false→NOT_FOUND**/同 profile 幂等/异 profile 临时 FAILED_PRECONDITION/malformed/template mismatch/下游 gRPC 透传）。运行 `bazel test //projects/game/agent/...`。
- [X] T006 [US1] 修改 `projects/game/agent/src/session-team.ts`：(1) `SessionTeamStore.create`（`:533-568`）改为 `update(sessionId, template, profileName, allowMissing)` 的 upsert：缺失+allowMissing→factory 构建（保留 `pending` 单飞，`:506`）；**缺失+!allowMissing→NOT_FOUND**；既有+同 profileName→返回既有（幂等，FR-002）；既有+异 profileName→**MVP 临时**抛 `Error("profile change rebuild pending")`（T005 映射 FAILED_PRECONDITION，US3 T011 替换为重建）。(2) **删除 `TeamAlreadyExistsError`（`:487-499`）**（FR-007）。(3) `teams` map 记录 `profileName`（GetTeam 读回 profile）。同步适配 `projects/game/agent/src/session-team.test.ts`（`:362-460`）：create 用例→update（缺失创建/**缺失+!allowMissing→NOT_FOUND**/同 profile 幂等/单飞/不隐式创建；异 profile 临时错误用例）。运行 `bazel test //projects/game/agent/...`。
- [X] T007 [P] [US1] 修改 `projects/game/desktop/internal/api/client.go`：`CreateTeam`（`:233-271`，POST）→`UpdateTeam(ctx, template, sessionID, profile string, updateMaskPaths []string, allowMissing bool)`（PATCH + `update_mask`/`allow_missing` query，body 为 `game.Team{Name, Profile}`），模板照既有 `UpdateTeamProfile`（`:476-513`）。同步适配 `projects/game/desktop/internal/api/client_test.go`（`:482-566`）：`TestClient_CreateTeam`→`TestClient_UpdateTeam`（断言 PATCH 方法、path、body 为 Team{name,profile}、query `allow_missing`）。运行 `bazel test //projects/game/desktop:...`。
- [X] T008 [P] [US1] 修改 `projects/game/desktop/app.go`：`App.CreateTeam`（`:1094-1138`）→`App.UpdateTeam(template, sessionID, profile string, updateMaskPaths []string, allowMissing bool)`，构建 `game.Team{Name: SessionName{...}.String()+"/team", Profile: profile}` 调 `client.UpdateTeam`，模板照 `App.UpdateTeamProfile`（`:1423-1478`）。同步适配 `projects/game/desktop/app_test.go`（`:2123-2192`）：`TestCreateTeam_*`→`TestUpdateTeam_*`（断言 PATCH + Team body + allow_missing）。运行 `bazel test //projects/game/desktop:...`。
- [X] T009 [US1] 修改桌面前端：(1) `projects/game/desktop/frontend/src/api.ts:529` `createTeam`→`updateTeam(template, sessionID, profile, updateMaskPaths, allowMissing)`（调 `a.UpdateTeam`）；(2) `projects/game/desktop/frontend/src/App.svelte`：`handleProfileSelected`（`:437-459`）将 `createTeam(...)` 改为 `updateTeam(tpl, sessionId, profileFullName, [], true)`；进入会话的 GetTeam→NOT_FOUND→弹窗两步（`:385-397`）保留（弹窗仍选 profile），但创建调用改为单次 `updateTeam(allowMissing=true)`——移除原 CreateTeam 失败后的 GetTeam 兜底重读（allow_missing 天然收敛，[research.md](research.md) §R6/§R9）。前端类型/调用点全量替换 `createTeam`→`updateTeam`。

**Checkpoint**: Team 单例物化端到端可用（US1）；重复/并发物化幂等收敛、无 ALREADY_EXISTS（US2）；`bazel build //projects/game` + proxy/agent/desktop 单测全通过。**MVP 可独立验证**（[quickstart.md](quickstart.md) 场景 1/2/4，除异 profile 重建外）。**MVP 过渡态（受控）**：既有+异 profile → 临时 FAILED_PRECONDITION（T005/T006 占位；T011/T013 合入后替换为重建；MVP 态不得对外宣称 FR-005 已达成）。

---

## Phase 3: US3 — profile 变更 graph 重建

**Goal**: 已物化 Team 上 `UpdateTeam` 改 profile 时，复用既有 MemorySaver checkpointer 重建 team graph，保留对话/游戏状态；turn in-flight 时拒绝（FR-005/FR-006）。

**Independent Test**: 物化 Team（profile=P1）产生历史后，`UpdateTeam(profile=P2)` 重建，下一 turn 用 P2 且历史消息零丢失；in-flight 重建→FAILED_PRECONDITION。详见 [quickstart.md](quickstart.md) 场景 3。

### 文档清单（编码前必读）

- **代码规范文档**: `style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) + 其内嵌引用的 [vitest module mocking pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（涉及模块级 mock 时必读）
- **官方文档**: [@langchain/langgraph（MemorySaver checkpointer / StateGraph / thread_id 语义）](https://langchain-ai.github.io/langgraphjs/)
- **技术文章**: 无
- **仓库内参考文档**（补充显式列出，非上述三分类）：[contracts/team-rebuild-contract.md](contracts/team-rebuild-contract.md)（重建契约，§7 单测要点）

**Tasks**:

- [X] T010 [P] [US3] 修改 `projects/game/agent/src/team/graph.ts`：`buildTeamGraph(deps)`（`:213-259`）增**可选**入参 `checkpointer?: MemorySaver`（加到 `TeamGraphDeps`，`:150-180`，或独立参数）；缺省→`new MemorySaver()`（首建，`:241` 不变）；提供时→`.compile({ checkpointer })`（`:256`）用注入的既有 checkpointer。`TeamGraphHandle.checkpointer`（`:146`）仍暴露。`TeamState`（`:67-89`）不变。`experimental/golang/aip_codegen` 无需同步更新；如 `team/graph` 有既有单测则适配（首建缺省行为不变）。
- [X] T011 [US3] 修改 `projects/game/agent/src/session-team.ts`：(1) `SessionTeamStore.update`（T006）的"既有+异 profileName"分支由临时抛错改为**重建**（**删除 MVP 临时 `Error("profile change rebuild pending")` 文案——合入前不得残留**）：复用 `pending` 单飞（`:506`）防并发重建；调 factory 重建子路径（T012）传入既有 `team.graphHandle.checkpointer`；成功后替换 `SessionTeam` 的 `graphHandle`（由 `readonly` 改可替换，或加 `rebuildProfile(newHandle)` 方法，`:122`）并更新 `teams` map 的 `profileName`；异常时既有 Team 不变（不留半重建状态）。(2) **in-flight 守卫**：重建前检 `team.isRunning()`（`:230-232`）→FAILED_PRECONDITION（FR-006，复用 RefreshTeam 守卫语义，`handler.ts:231-238`）。同步适配 `session-team.test.ts`：异 profile→重建（断言 `handle.checkpointer` 引用不变、`getTeamState()` 历史计数/内容零丢失、单飞仅一次 build、in-flight→抛错、重建失败→既有不变）。
- [X] T012 [US3] 修改 `projects/game/agent/src/server.ts`：生产 factory（`:252-306`）抽出"仅重建 graph（复用 checkpointer）"子路径——解析新 profile（`promptClient.getTeamProfile`）→新 deps（models/prompts）→`buildTeamGraph(newDeps, existingCheckpointer)`；**复用既有 buffer/bridge/sink/MCP-host**（profile 无关，不重建，[team-rebuild-contract.md](contracts/team-rebuild-contract.md) §3）；返回新 `TeamGraphHandle` 供 T011 替换。重建失败抛错（既有 Team 不变）。
- [X] T013 [US3] 修改 `projects/game/agent/src/handler.ts`：`UpdateTeam` handler（T005）的"既有+异 profile"分支由临时 FAILED_PRECONDITION 改为调 store 重建（T011），in-flight 时由 store 抛错映射 FAILED_PRECONDITION；**删除临时占位文案 "profile change rebuild pending (US3)"**；下游 gRPC 状态透传不变。同步适配 `handler.test.ts`：异 profile→成功重建（响应 profile=P2）+ in-flight→FAILED_PRECONDITION + 重建后下一 turn 用新 model（fake provider 断言）用例。运行 `bazel test //projects/game/agent/...`。

**Checkpoint**: profile 变更重建端到端可用（US3）；`bazel build //projects/game` + agent 单测全通过。三个用户故事全部可独立验证。

---

## Phase 4: Polish & 大型测试验收

**Purpose**: 文档同步（引用溯源，宪法原则 I）+ 大型测试验收（宪法原则 VI）。

### 文档清单（编码前必读）

- **代码规范文档**: `style/large_test.md`（+ 其引用的 `style/golang.md`——大型测试 Go 用例须守 golang 单测规范，+ 后者引用的 [Google Go Style](https://google.github.io/styleguide/go/)（入口索引；[Style Guide](https://google.github.io/styleguide/go/guide) 规范必读、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)））；`style/api.md` + [AIP-156](https://google.aip.dev/156) + [AIP-133](https://google.aip.dev/133) + [AIP-134 create-or-update](https://google.aip.dev/134#create-or-update)（T014/T016 改写 031/039 文档须准确描述 allow_missing 物化/幂等语义）——文档对齐合规描述（T014 改写 031 契约须准确描述被移除的 AIP-133 偏离）
- **官方文档**: 无
- **技术文章**: 无

**Tasks**:

- [X] T014 [P] 同步 `specs/031-team-template-mode/`：(1) `spec.md` FR-033（`:229`）与 FR-003/Team 定义（`:174,234`）改写为"Team 为 AIP-156 单例，经 `UpdateTeam(allow_missing=true)` 物化；profile 可经 Update 变更（重建 graph）"，并标注被 `040` supersede；(2) `contracts/api-contract.md` §2.2（`:36,40,46`）：删 AIP-133 偏离注、删 CreateTeam 行、新增 UpdateTeam（AIP-134/156）；(3) `data-model.md`（`:51,205`）、`plan.md`（`:24,30`）、`tasks.md`（`:185-189`）、`quickstart.md` 中 CreateTeam 引用改为 UpdateTeam。
- [X] T015 [P] 同步 `specs/035-proto-contract-refine/data-model.md:178`：请求消息清单 `CreateTeamRequest`→`UpdateTeamRequest`。
- [X] T016 [P] 同步 `specs/039-planner-memory-calibration/`（**未实现，仅改文档**）：将 initInstruction 触发点由"CreateTeam 构建 graph 后异步"改为"`UpdateTeam(allow_missing=true)` 物化路径（graph 首建）后异步"；profile 变更重建不重跑 init（仅首建触发）。涉及 `spec.md`（`:30,82,91,142`）、`research.md`（`:124,241,249,280`）、`tasks.md`（`:141,158`）、`contracts/team-graph-contract.md`（`:58,150,152,153`）中所有 `CreateTeam`/`SessionTeamStore.create` 引用。
- [X] T017 修改大型测试代码 `projects/game/testplan/`：(1) `helpers_test.go`（`:451,481`）：`game.CreateTeamRequest`→`game.UpdateTeamRequest`，构造 PATCH 请求（body 为 `game.Team{Name,Profile}` + `allow_missing` query）；(2) `saolei_team_test.go`（`:243-250` 及 CreateTeam 相关用例）：原"同 profile→200 / 异 profile→409"改为"同 profile→200 幂等 / 异 profile→200 重建成功"，新增 in-flight 重建→400 FAILED_PRECONDITION 用例。运行 `bazel build //projects/game/testplan:...` 验证测试代码编译通过（测试执行与验收属 T018 guitar 实跑）。
- [X] T018 大型测试验收（宪法原则 VI）：经 testplan skill 执行 `guitar run <plan.yaml>`（部署→测试→清理闭环），覆盖 [quickstart.md](quickstart.md) 场景 1-4（物化/幂等多标签/profile 重建/错误语义）。**验收标准：所有用例全部通过**（任何 failed/flaky 视为未通过，须修复重跑直至全绿）。仅 `bazel build` 测试 target **不构成**验收。

**Checkpoint**: 文档与契约一致（引用溯源）；大型测试实跑全绿（服务型应用验收）。

---

## Dependencies & Execution Order

### Phase 依赖

- **Phase 1（Foundational）**：无依赖，最先。`bazel build` 因新接口报错驱动 Phase 2/3 落地。
- **Phase 2（US1+US2 MVP）**：依赖 Phase 1（proto/codegen）。proxy（T003/T004）、agent（T005/T006）、desktop（T007/T008/T009）三组中：同组内串行（接口→handler/store/client→app→frontend），跨组可并行（不同服务，仅共享 proto）。
- **Phase 3（US3）**：依赖 Phase 2 的 agent store/handler（T005/T006）与 graph.ts（T010）。T010（graph 可选 checkpointer）可与 Phase 2 并行（不同文件、无依赖），但 T011-T013 须在 T006/T010 后。
- **Phase 4（Polish）**：T014/T015/T016 文档可并行（不同 spec 目录）；T017 依赖 Phase 1-3；T018（验收）依赖全部完成。

### 用户故事依赖

- **US1（P1）+ US2（P2）**：合为 Phase 2 交付（共享 `UpdateTeam` 路径，不可分割）。US2 无独立 task——其验收（幂等/并发收敛/无 ALREADY_EXISTS）由 Phase 2 切片内 T004（assignOwner 不变）、T005/T006（幂等+删 TeamAlreadyExistsError）、T009（单次 update 取代 create-if-missing）共同满足。
- **US3（P3）**：依赖 Phase 2（建立在 UpdateTeam handler/store 之上），独立可测（[quickstart.md](quickstart.md) 场景 3）。

### 并行机会

- Phase 1 后，proxy 组（T003→T004）、agent 组（T005→T006）、desktop 组（T007/T008 并行→T009）可三路并行。
- T010（graph.ts）可与 Phase 2 并行（[P]，独立文件）。
- T014/T015/T016 文档三路并行（[P]，不同 spec）。

---

## Implementation Strategy

### MVP First（Phase 1 + Phase 2）

1. 完成 Phase 1（proto + codegen）。
2. 完成 Phase 2（US1+US2：proxy + agent + desktop 物化/幂等）。
3. **STOP 验证**：[quickstart.md](quickstart.md) 场景 1/2/4（物化、幂等多标签、错误语义）单测全绿；异 profile 重建暂为临时 FAILED_PRECONDITION（不可达于桌面 MVP 用法）。
4. MVP 可演示：会话进入→选 profile→单次 UpdateTeam 物化→Connect/发送消息。

### Incremental Delivery

1. Phase 1+2 → MVP（US1+US2）。
2. 加 Phase 3 → US3（profile 重建）。
3. 加 Phase 4 → 文档同步 + 大型测试验收（全合规）。

---

## Notes

- [P] task = 不同文件、无未完成依赖。
- 编译 + 单测是每个代码 task 的一部分（宪法 IV），不单列；大型测试单列 T017/T018（宪法 IV/VI）。
- proto 字段号为 clean break（无线上存量 Team 资源须兼容）；`agents`/`create_time` field 号顺移。
- 间接引用已显式列出：`style/api.md` 为索引，须配合具体 AIP URL（AIP-156/134/127/193/203/122）；`style/javascript.md` 引用外部 [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) 与内嵌 [vitest module mocking pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)；`style/golang.md` 引用外部 [Google Go Style](https://google.github.io/styleguide/go/)（guide/decisions/best-practices）。各 phase 文档清单按需显式列出（宪法原则 V，不做引用传递）。
- US3 重建为最高风险项，须严格按 [contracts/team-rebuild-contract.md](contracts/team-rebuild-contract.md) §7 单测要点验证（checkpointer 引用不变 + 历史零丢失 + in-flight 守卫 + 单飞 + 失败回滚）。
- **已知同步遗漏（039 评审补充修正，C1/C5）**：T016 原始同步范围未含 `specs/039-planner-memory-calibration/quickstart.md`（:22,53 的 CreateTeam 表述）、T014 未含 `specs/031-team-template-mode/contracts/desktop-contract.md`（§2.3/§4/§5/§6 的 CreateTeam 表述）；两处已由后续修正补齐 supersede 标注，本任务描述保持当时执行范围。
