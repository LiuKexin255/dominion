# Tasks: agent_v2 host 层场景化重构支持主子模型

**Input**: Design documents from `/specs/080-host-main-sub-model/`

**Prerequisites**: plan.md（required）、spec.md（required）、research.md、data-model.md、contracts/、quickstart.md

**Tests**: 单测不单列任务——按宪法 IV（`.specify/memory/constitution.md`），编译 + 单测（`bazel build`/`bazel test` 相关 target）是每次代码变更的一部分，随各实现任务执行并在任务描述中注明；大型测试验收单列任务（宪法 VI）。

**Organization**: 按 user story 组织（spec.md US1/US2/US3），每个 story 独立可测、可作增量交付。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 归属 user story（US1/US2/US3）
- 描述含精确文件路径

## Path Conventions

本 feature 无新建工程根；路径为仓库相对路径：host 代码 `projects/game/agent_v2/src/`、新插件包 `common/js/dsh-plugins/sts2/loop/`、测试物料 `projects/game/testplan/` 与 `projects/game/fake-llm/service/`。操作命令（gazelle/pnpm/go/bazel）见 `AGENTS.md`。

**每个 phase 的文档清单**（宪法 V 三分类；AGENTS.md 与 spec/plan 属必读不重复列出；080 contracts 与外部/间接引用文档按 phase 显式列出并标注章节）：

---

## Phase 1: Setup（新包骨架与 workspace 接线）

**Purpose**: `@dominion/dsh-sts2-loop` 可编译空包就位并被 agent_v2 依赖闭包接收。

**文档清单**：

- **代码规范文档**：`style/javascript.md`（ESM 包书写/编译规则）；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（style/javascript.md 引用基准）、[TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)、[SWC Modules 配置](https://swc.rs/docs/configuration/modules)、[rules_swc tsconfig 锁步](https://github.com/aspect-build/rules_swc/blob/main/docs/tsconfig.md)（style/javascript.md §模块系统 引用的规范链接——T001 的 tsconfig/.swcrc 判据）
- **官方文档**：[pnpm catalogs](https://pnpm.io/catalogs)（workspace 依赖版本统一 catalog 机制）
- **技术文章/技术参考文档**：`specs/048-js-esm-migration/contracts/esm-package-conventions.md` §1/§2/§4（包声明/tsconfig-.swcrc 锁步/打包与运行——新包判据）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（`vitest_test` 退出码/空套件契约——T001 的 `lib_test` target 判定）；`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §1/§2/§4（专用分组布局、包名映射、打包通道与 node_modules 忽略不变量——T001/T002 的 target 与 `.bazelignore` 判据）；`common/js/dsh-plugins/saolei/loop/package.json`（catalog/workspace 混合声明先例——T001 对齐对象）与 `common/js/dsh-plugins/saolei/loop/tsconfig.json`、`common/js/dsh-plugins/saolei/loop/.swcrc`（tsconfig/.swcrc 锁步先例——T001 对齐对象）；`common/js/dsh-plugins/sts2/game/package.json`（同分组包形态先例）与 `common/js/dsh-plugins/sts2/game/tsconfig.json`、`common/js/dsh-plugins/sts2/game/.swcrc`（同分组 config 先例）；`common/js/dsh-plugins/sts2/game/BUILD.bazel`（`:pkg`/`:runtime_pkg` target 形态先例）+ `common/js/dsh-plugins/saolei/loop/BUILD.bazel`（`runtime_deps` 声明先例）；`projects/game/agent_v2/package.json`（T002 的 workspace 依赖声明形态参照）与 `projects/game/agent_v2/BUILD.bazel`（`server_pkg.runtime_deps` 修改点、T012 的 `server_lib` link dep 参照）

- [X] T001 [P] 创建 `@dominion/dsh-sts2-loop` 包骨架于 `common/js/dsh-plugins/sts2/loop/`：`package.json`（name/private/`"type": "module"`/exports 双条件指向 `./src/index.js`；peerDependencies = `@deepseek-ai/cordis`、`@deepseek-ai/dsh-agent`、`@deepseek-ai/dsh-session`、`@deepseek-ai/dsh-llm` 走 catalog + `@dominion/dsh-team`（仅类型）走 `workspace:*`，对齐 `common/js/dsh-plugins/saolei/loop/package.json` 的 catalog/workspace 混合形态；devDependencies = `@types/node`/`typescript`/`vitest` catalog）、`tsconfig.json` + `.swcrc`（按 esm-package-conventions §2 锁步）、`src/index.ts`（空导出占位）、`README.md`（定位：最小编排包，引用 `specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md`）；`bazel run //:gazelle common/js/dsh-plugins/sts2/loop` 生成 BUILD.bazel，再按 `common/js/dsh-plugins/sts2/game/BUILD.bazel` 先例补 gazelle 不生成的两个 target：`js_library(:pkg)`（srcs 含 `package.json`，裸包名解析的 manifest 前提）与 `js_runtime_library(:runtime_pkg)`（`package_name = "@dominion/dsh-sts2-loop"`）；并按 `style/javascript.md` vitest_test 宏规则补 `lib_test` target（data = ts glob + 镜像 node_modules deps）；根 `.bazelignore` 增 `common/js/dsh-plugins/sts2/loop/node_modules`（`MODULE.bazel` 的 `verify_node_modules_ignored` 强校验，见 `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §4.3）；核对 `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §1/§2 已含 `sts2/loop/` → `@dominion/dsh-sts2-loop`（偏差回改）；`bazel build`/`bazel test` 通过
- [X] T002 workspace 接线：`projects/game/agent_v2/package.json` 声明 `@dominion/dsh-sts2-loop` workspace 依赖（声明形式参照该文件中 `@dominion/dsh-sts2` 既有行）；`bazel run @pnpm -- --dir "$(git rev-parse --show-toplevel)/projects/game/agent_v2" up`（`--dir` 用仓库根派生，勿硬编码 worktree 路径）；`projects/game/agent_v2/BUILD.bazel` 的 `server_pkg.runtime_deps` 增 `//common/js/dsh-plugins/sts2/loop:runtime_pkg`（workspace 包进部署闭包的唯一通道，`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §4.1；T012 引入 import 后须重跑 gazelle 补 `server_lib` deps）；`bazel run //:gazelle projects/game/agent_v2`；`bazel mod tidy`；`bazel build //projects/game/agent_v2/...` 验证依赖闭包

**Checkpoint**: 新包独立构建测试通过、agent_v2 闭包含新包（quickstart V1 前半）。

---

## Phase 2: Foundational（场景中立编排 seam 接口）

**Purpose**: 全部 user story 共同消费的 seam 类型模块先行（宪法 III 接口优先）。

**⚠️ CRITICAL**: 未完成本 phase 不得开始任何 user story。

**文档清单**：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)、[TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)（ESM 导出/类型再导出判据）
- **官方文档**：[dsh-agent 公开面（`AgentHandle` 等类型）](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/packages/core/agent/src/index.ts)、[dsh-session 会话事件类型（`SessionEvent`）](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/packages/core/session/src/index.ts)（版本对齐 0.2.0-rc.2；`third_party/dsh/core` 只是依赖闭包/版本快照包，不含上游源码；类型以 pinned GitHub tag 与安装后的 workspace `node_modules` 为准）
- **技术文章/技术参考文档**：`projects/game/agent_v2/src/session.ts`（`LOOP_ERROR_TEXT`/`loopErrorText` 迁移源与 `TeamSessionsDeps` 现状）、`projects/game/agent_v2/src/dsh.ts`（`DshContext` 类型来源）、`projects/game/agent_v2/src/scenes.ts`（`SceneSpec` 现状与 `SceneRegistry` 的模板键来源）；`specs/080-host-main-sub-model/contracts/scene-registry.md` §1/§2（seam 方法表、工厂 deps 类型与类型逐字段）；`specs/080-host-main-sub-model/data-model.md` §2（SceneOrchestration/SceneSnapshot/SceneFailure 字段语义）；`common/js/dsh-plugins/team/src/team.ts`（`TeamMemberEvent`:154 与 `TeamEventStream`:169——seam `teamEvents()` 返回类型与信封定义的直接来源）；`common/js/dsh-plugins/saolei/loop/src/orchestrator.ts`（`TeamOrchestrator` 公开面与 `OrchestratorStateError`——被适配对象与状态错误语义对照源）；`common/js/dsh-plugins/saolei/loop/src/member.ts`（`LoopErrorCode` 六码——`SceneLoopErrorCode` 字面量来源）；`specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md` §2（`lastError.origin` 双源语义——`SceneFailure.origin` 消费依据）；`specs/080-host-main-sub-model/quickstart.md` §V3（场景中立硬/软门禁命令与判定口径——T003/T006 的门禁基准）

- [X] T003 创建 seam 接口模块 `projects/game/agent_v2/src/orchestration.ts`：定义 `SceneOrchestration`（materialize/members/member/teamEvents/snapshot/submit/cancel/whenQuiescent/dispose）、`SceneMaterializeInput`/`SceneMemberConfig`（`{role, preset, provider?, model?}`）、`SceneMember`（`{role, kind: "agent"|"announce-only"}`）、`SceneSnapshot`（含 `materialized`/`active`/`activation`/`paused`/`queued`/`failed`/`lastError`，**不含 phase**）、`SceneFailure`（`{message, member, code, origin: "member-turn"|"orchestration", detail}`）、`SceneLoopErrorCode`（六码联合 `"TRANSIENT"|"QUOTA"|"CREDENTIAL"|"CONFIG"|"INPUT_TOO_LARGE"|"INTERNAL"`）、`SceneRegistry`（`Readonly<Record<string, SceneSpec>>` 注入类型，`SceneSpec` 以 `import type { SceneSpec } from "./scenes.js"` 引用——类型擦除，不构成运行时环；`scene-*.ts` 仍禁止 import `scenes.ts`）、`SceneRegistrationDeps`（`compose`/`agents`/`logger`/`provider`/`team` 结构化 seam 子集）与 `SceneStateError`（`UNMATERIALIZED`/`ALREADY_MATERIALIZED`/`DISPOSED`）——逐字段对照 `specs/080-host-main-sub-model/contracts/scene-registry.md` §1/§2；同时定义 `SceneLogger`（`error(message, context: Readonly<Record<string, unknown>>)` 等场景无关窄面）等注入类型；**本任务不删除 `session.ts` 对 `@dominion/dsh-saolei-loop` 的 import、不改 `TeamSessionsDeps` 字段类型**——这两步属于 T006（编排构造切换后统一改型/删除），以保证 Phase 2 结束时 `bazel build`/`bazel test` 仍可绿；将 `LOOP_ERROR_TEXT`/`loopErrorText` 从 `projects/game/agent_v2/src/session.ts` 迁入本模块、key 类型从 saolei-loop `LoopErrorCode` 改锚 `SceneLoopErrorCode`（六码字面量与用户文案**逐字节不变**），session.ts 改为 re-export 保持既有导入方不变；`bazel build` + `bazel test //projects/game/agent_v2/...` 零回归

**Checkpoint**: seam 类型可用，user story 实现可并行开始。

---

## Phase 3: User Story 1 - host 场景注册：通用路径场景中立，saolei 经注册接入零变化 (Priority: P1) 🎯 MVP

**Goal**: host 通用路径（session/history）消费场景注册（编排工厂 + 成员投影），saolei 全部行为逐字节不变（spec FR-001~FR-005、FR-008 的单测面）。

**Independent Test**: `bazel test //projects/game/agent_v2/...` 全绿（既有断言除 079 的 sts2 未接线断言外零修改通过——错误文案逐字节、校验时序、名册/投影行为）；quickstart V3 固定命令集预检通过（硬门禁零命中含注释、软门禁仅注释/规范链接命中；可执行代码中的场景词汇仅存于 `scenes.ts`/`scene-saolei.ts`，`orchestration.ts` 零场景词汇）。

**文档清单**：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)、[TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)、[Vitest Mocking Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（T004-T006 单测的 mock 约定）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/080-host-main-sub-model/contracts/scene-registry.md` §1/§2/§3/§4/§5（注册形态、seam 方法表与输入/快照类型、事件分发、职责边界、saolei 适配器职责）；`specs/080-host-main-sub-model/data-model.md` §1-§3（SceneSpec 字段语义、seam 实体、成员运行时与动态投影——类型签名以 scene-registry 为规范源）；`common/js/dsh-plugins/team/src/team.ts`（`TeamMemberEvent` 信封 `{role, memberId, channel, event}`——T006 分发的输入类型）；`common/js/dsh-plugins/saolei/loop/src/orchestrator.ts`（`TeamMaterializeOptions`/`announcer`/成员访问公开面——T005 被适配对象）；`common/js/dsh-plugins/saolei/loop/src/member.ts`（`LoopErrorCode`/成员结果语义——T005 错误映射）；`specs/079-sts2-template-scaffold/contracts/scene-dispatch.md` §3-§4（fail-loud 机制与 saolei 零变化可验面）；`specs/059-agent-v2-team-mode/contracts/team-api.md` §2（物化/刷新契约——同构保持基准）；`specs/077-announcer-member-unification/contracts/member-event-stream.md` §5（成员事件单路消费）；`specs/077-announcer-member-unification/contracts/host-member-parity.md` §2（announce-only 成员的 kind 派生/成员面行为——T005/T006 保持基准）；`specs/071-memory-preset-scope/contracts/dsh-plugins.md` §4（memoryScope 派生——移入 saolei 适配器的语义）；`specs/063-llm-reliability-opencode-go/contracts/model-selection.md` §3（复合标识拆分传递）；`specs/080-host-main-sub-model/quickstart.md` §V3（场景中立硬/软门禁命令与判定口径——T006 的门禁基准）

- [X] T004 [P] [US1] `projects/game/agent_v2/src/history.ts` 角色类型场景中立化：删除 `AgentMemberRole`/`AGENT_MEMBER_ROLES`/`isAgentMemberRole` 与 `MemberRole`（含 `session.ts:75` 的 re-export），`MemberCollector`/`TeamHistory`（`views` 改 `Map<string, MemberViewEntry[]>` 惰性创建，`appendMemberViewUser`/`settleToolResult`/`listMemberMessages`）及导出类型的 role 签名全部改 `string`；`TeamHistory.appendMemberOutput(role, content, interrupted = false, recordView = true)`（`recordView` 为第四参，避免与既有 `interrupted` 位置冲突；T007 的动态投影传 `false` 以不构建成员视角）；`TeamHistory.appendMemberViewUser(role, event, recordView = true)`；`MemberCollector` 构造函数增加 `recordView?: boolean`（默认 `true`，透传到上述两方法，`false` 时抑制 `views` 写入与 `member_view` 帧）；同步 `projects/game/agent_v2/src/history.test.ts`（既有断言全绿，不改语义；补 `Map` 惰性视图与 `recordView=false` 用例）；`session.ts` 中 `isAgentMemberRole`/`AgentMemberRole`/`MemberRole` 消费点随本任务改 string（与 T006 的文件冲突以本任务先行为准——只做类型替换，不做结构变更）
- [X] T005 [P] [US1] 创建 `projects/game/agent_v2/src/scene-saolei.ts` 并演进 `projects/game/agent_v2/src/scenes.ts`：`SceneSpec` 新增可选字段 `createOrchestration?: (ctx: DshContext, deps: SceneRegistrationDeps) => SceneOrchestration` 与必填字段 `faceMembers: readonly string[]`（face 可见成员名清单，仅用于成员名预检/roster 错误文案，**不含任何能力/行为声明**；saolei = `["player","planner","announcer"]`），不变式 `orchestratable === true ⇔ 工厂存在`（`orchestratable` 为唯一接线门，工厂不承担第二重门语义；`scenes.test.ts` 断言）；`scene-saolei.ts` 导出 `createSaoleiOrchestration(ctx, deps, spec)`，由 `scenes.ts` 以闭包绑定自身条目数据（**禁止 `scene-saolei.ts` import `scenes.ts`，避免模块循环**）；saolei 适配器实现 seam——`materialize` 把中立输入映射为 `TeamMaterializeOptions`（`goal` = 传入 `spec.teamGoal` 原文、`memoryScope = {template, preset: planner 成员 preset id}`（§4 派生不变）、`player`/`planner` 槽位与 provider/model 路由）、`members()` 返回三成员投影（player/planner agent + announcer announce-only）、**announcer 不变式内化**（物化后 `announcer === undefined` → 整体回滚并抛错）、`member`/`teamEvents`/`snapshot`（丢弃 `phase` 字段）/`submit`/`cancel`/`whenQuiescent`/`dispose` 直通 `TeamOrchestrator`；适配器包装 host logger 以保留自己的 `phase` 日志字段（不进入通用 seam）；saolei 条目挂工厂（构造 deps 含 `team` seam）；未接线的 sts2 条目补 `faceMembers: []`（保证 T005 可编译；T012 再改为 `["main"]`）；`scenes.test.ts` 既有 saolei/sts2 固定对象断言同步加 `faceMembers`（sts2 暂为 `[]`）；新增 `scene-saolei` 单测（goal/memoryScope 传参断言、announcer 不变式、成员投影序、`faceMembers` 值）
- [X] T006 [US1] `projects/game/agent_v2/src/session.ts` 通用路径泛化：物化改为经**注入场景注册**（`TeamSessionsDeps.scenes: SceneRegistry`，默认模块级 `SCENES`）的 `createOrchestration` 工厂构造编排（seam deps 经 `TeamSessionsDeps` 透传，场景注册模块自取所需 seam）；session 内全部场景解析——接线门（等价 `wiredScene`）、roster 校验/成员角色查询、未接线文案——MUST 经该注入 registry，禁止绕过注入值直读模块 `SCENES`（供 US2 合成场景测试）；`TeamEntry.members` 由编排 `members()` 投影派生（agent → handle+collector、announce-only → 归并播报投影，kind 判别消费不变）；成员视图所需 `preset`/`model` 由已验证的 `SceneMaterializeInput.members` 按 role 关联（announce-only = 空串），不再从 saolei 形参数拼装；成员名校验与 roster 错误文案改由注入 registry 的 `faceMembers` 派生（含 announcer；`isMemberRole`/`memberRoster` 不再直读模块 `SCENES` 或 `ANNOUNCER_MEMBER_ROLE`）；删除 `orchestratedRoles`（恰双成员断言）、announcer 硬编码成员与自检、`memoryScope`/`goal` 计算与 saolei 形 materialize 参数拼装、`createOrchestrator()` 直构；`TeamSessionsDeps` 的 seam 字段改用 `orchestration.ts` 的结构化类型，`session.ts` 不再 import `@dominion/dsh-saolei-loop`（含头注释）；`mapOrchestratorError` 从 `instanceof OrchestratorStateError` 改为按 `code` 结构化映射（`UNMATERIALIZED`/`ALREADY_MATERIALIZED`/`DISPOSED` → `FAILED_PRECONDITION`）；`projects/game/agent_v2/src/session.test.ts` 除 079 的 sts2 未接线断言（由 T012 改写）外既有断言**零修改**全绿（错误文案逐字节、校验时序、流收束）+ 补投影派生用例 + **合成单成员/无 announcer 场景（注入 `deps.scenes` + stub 编排）的物化/驱动/刷新用例（US1 AC3）**；`projects/game/agent_v2/src/scenes.test.ts` 同步工厂与 `faceMembers` 字段断言

**Checkpoint**: US1 独立完成——saolei 单测面零回归、host 通用路径场景中立（V3 预检）。

---

## Phase 4: User Story 2 - 主子会话的 face 语义映射 (Priority: P2)

**Goal**: 单成员（主子）roster 的物化/刷新/名册语义成立 + 名册外角色（未来子代理）的事件标注投影机制落地（spec FR-004/FR-006/FR-007 的单测面）。

**Independent Test**: 经 `TeamSessionsDeps.scenes` 注入合成单成员场景（stub 编排 double）+ 合成信封的单测全部通过——无需 sts2 接线即可验证 face 语义（对照 `specs/080-host-main-sub-model/contracts/face-main-sub.md`）。

**文档清单**：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)、[TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)、[Vitest Mocking Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（T007/T008 单测的 mock 约定）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/080-host-main-sub-model/contracts/face-main-sub.md` §1-§5（单成员物化/名册固定表/active_member 语义/错误码与文案/proto 零变更声明）；`specs/080-host-main-sub-model/contracts/scene-registry.md` §3-§4（事件分发与动态投影规则——投影键为 `(role, memberId)`；两层 fail-fast 与错误码）；`specs/080-host-main-sub-model/data-model.md` §3（成员运行时/动态投影实体）；`specs/060-agent-v2-team-optimize/contracts/team-api.md` §1（active_member 单一合并值语义）；`specs/077-announcer-member-unification/contracts/host-member-parity.md` §2（成员 kind 派生与成员面 NOT_FOUND 行为）；`specs/059-agent-v2-team-mode/contracts/team-api.md` §2（刷新语义同构基准）；`common/js/dsh-plugins/preset-authoring/src/derive.ts`（空 persona 保留池模板 prefix/锚串、非空 persona 覆盖——T008 合成 preset 的语义依据）；`projects/game/agent_v2.proto`（场景无关 team 原语——T008 零变更核对面）

- [X] T007 [US2] 名册外角色动态投影：`projects/game/agent_v2/src/session.ts` 成员事件分发改按 `specs/080-host-main-sub-model/contracts/scene-registry.md` §3——信封 role ∈ 投影按 kind 派发（现状行为不变）；role ∉ 投影时按 **`(role, memberId)` 键**惰性创建动态 collector（首个信封到达时；同 role 的不同 memberId 各自独立，互不吞事件）产出成员标注帧与归并序列条目（member 标签 = 信封 role），**不进名册、无成员资源、无成员视角**；动态 collector 由会话条目持有并以 `MemberCollector` 的 **viewless 模式**（`recordView=false`，由 T004 在 `history.ts` 提供）复用既有事件处理：只写归并序列、产出成员标注帧，不写 `TeamHistory.views`、不发 `member_view` 帧（对齐 `data-model.md` §3 “不构建成员视角”）；条目 teardown 销毁、刷新（重建）不跨代保留；`session.test.ts` 新增合成信封用例（未知 role → 帧 + 归并条目、同 role 两 memberId 各自成条、`GetTeamMember`/`ListMemberMessages` 对该角色 NOT_FOUND、刷新后清零），`history.test.ts` 增 viewless 模式不写视图的断言
- [X] T008 [US2] face 语义单测（对照 `specs/080-host-main-sub-model/contracts/face-main-sub.md` §1-§4）：在 `projects/game/agent_v2/src/session.test.ts` 经 `deps.scenes` 注入合成单成员场景（`roster: ["single"]`/`presetRoles: ["single"]`/`rolePoolTemplate: {single: "single"}`/`faceMembers: ["single"]`、stub 编排 double 实现 seam 各方法）验证——单成员 UpdateTeam 物化（名册恰 1、无服务端附加成员）、roster 校验错误路径（双成员/错 role → `INVALID_ARGUMENT`）、`active_member`（在途 = 驱动成员、静止 = activation、cancel 不变）、刷新同构（在途 `turn_end{ABORTED}`、未消费输入作废、`create_time` 保留、幂等）；确认 `projects/game/agent_v2.proto` 全程零变更

**Checkpoint**: US1 + US2 均独立可用——face 语义全部单测闭环。

---

## Phase 5: User Story 3 - sts2 最小接线：主子形态的端到端验收载体 (Priority: P3)

**Goal**: `@dominion/dsh-sts2-loop` 最小编排实现、sts2 场景开放（roster/preset 词汇 `main`）、组合清单接线、fake-llm fixture 与 `game-sts2` suite 就位（spec FR-009；FR-010 负向边界不动）。

**Independent Test**: 包单测全绿；sts2 物化/对话路径 host 单测全绿；`game-sts2` suite 用例见 Phase 6 大型测试验收执行。

**文档清单**：

- **代码规范文档**：`style/javascript.md`、[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)、[TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)、[Vitest Mocking Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（T009-T013）；`style/golang.md` 与 [Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)（T014-T016 的 Go 用例规范，含单元测试节表驱动/given-when-then）；`style/large_test.md`（T016 的 suite/target/反模式约定）
- **官方文档**：[dsh-agent 公开面（`CreateAgentOptions`/`AgentHandle`）](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/packages/core/agent/src/index.ts)、[dsh-agent-loop 驱动原语（`inject`/`followup`/`steer`/`whenIdle`）](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/packages/core/agent-loop/src/agent.ts)、[dsh-session 会话事件类型（`SessionEvent` 等）](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/packages/core/session/src/index.ts)、[dsh-llm 公共面（LLM 错误/模型类型）](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/packages/llm/llm/src/index.ts)（版本对齐 0.2.0-rc.2；`third_party/dsh/core` 只是依赖闭包/版本快照包，不含上游源码；类型以 pinned GitHub tag 与安装后的 workspace `node_modules` 为准）
- **技术文章/技术参考文档**：`specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md`（T009-T011 全部行为）；`specs/080-host-main-sub-model/contracts/scene-registry.md` §1/§2/§6（T011 seam 实现、T012 场景注册工厂与接线不变式）；`specs/080-host-main-sub-model/contracts/face-main-sub.md` §1/§4（T012 单成员物化/刷新语义与错误码）；`specs/080-host-main-sub-model/data-model.md` §1、§4（SceneSpec sts2 条目、状态转移图）；`survey/sts2-agent-model.md` §1-§2（主子模型裁定与 dsh 原语证据）；`specs/079-sts2-template-scaffold/contracts/scene-dispatch.md` §1/§3（T012 改写的 sts2 fail-loud 断言基准；T016 的 sts2 数据面接受面）；`common/js/dsh-plugins/saolei/loop/src/agent-member.ts`（驱动原语先例源码——T009）；`common/js/dsh-plugins/saolei/loop/src/orchestrator.ts`（物化/回滚先例——T011）；`common/js/dsh-plugins/team/src/team.ts`（`TeamMemberEvent`:154 信封与 `TeamEventStream`:169——T010/T011 的类型来源）；`common/js/dsh-plugins/preset-authoring/src/derive.ts`（persona patch 语义：**非空 persona 覆盖池模板 prefix**、空 persona 保留模板 base——T016 的 sts2 preset 必须空 persona 的依据）；`specs/070-dsh-upgrade/contracts/preset-model-mapping.md` §3 与 `specs/070-dsh-upgrade/contracts/composition-manifest.md`（T013：templates 快照契约与清单行同步义务）；`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §1/§2/§3（T013 惰性行形态与目录/包名映射）；`specs/074-dsh-grpc-runtime/contracts/preset-templates.md` §1/§2（templates 快照 plugins 边界判据——T013）；`specs/063-llm-reliability-opencode-go/contracts/model-selection.md` §3（复合标识校验同构）；`projects/game/testplan/README.md`（T014-T016：suite 惯例与 fake-llm lockstep）；`projects/game/testplan/deploy_agent_v2.yaml`、`projects/game/testplan/deploy_agent_v2_memory_down.yaml`（T016 精简拓扑的相对基线与「无 memory 可启动」证据）；`projects/game/fake-llm/README.md`（T014：注入设施与 testdata/embed 约定）；`specs/046-fake-llm-think-chunking/contracts/template-config.md`（T014：模板 schema/字段校验）；`specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §2/§3（T014：`system_keywords`/多轮条件与匹配优先级）；`specs/012-fake-llm-service/spec.md` FR-006/FR-007（T014：keywords 子串命中与 name 字典序 tie-break）；`specs/059-agent-v2-team-mode/contracts/team-api.md` §2/§3（T016：单成员物化/刷新与 Send 流帧语义同构基准）；`specs/060-agent-v2-team-optimize/contracts/team-api.md` §1（T016：active_member 语义，场景 activation 见 080 face 契约 §3）；`specs/080-host-main-sub-model/contracts/testplan-game-sts2.md`（T014-T016 全部）

- [X] T009 [P] [US3] `common/js/dsh-plugins/sts2/loop/src/drive.ts`：单 agent 驱动原语（对照 `contracts/sts2-loop-minimal.md` §4）——静止装配（`inject` 前缀 + 末条 `followup` + `agent.whenIdle()` 收束）、在途 `steer` 入队（pending 计数）与 turn 结束重放持有输入集、cancel 冻结与投递恢复、引擎错误六码映射（对齐 saolei `mapEngineCode` 分类集）；`drive.test.ts`（DI doubles，`style/javascript.md` Mock 约定：构造器参数注入 + `vi.fn()`，禁止模块拦截）
- [X] T010 [P] [US3] `common/js/dsh-plugins/sts2/loop/src/events.ts`：main agent 事件信封——订阅 agent 的 session 事件与 live 面（`agent/assistant-stream`/`agent/status`/`agent/error`），装入 `TeamMemberEvent` 信封 `{role: "main", memberId: <agent session id>, channel, event}`（类型导入 `@dominion/dsh-team`，无运行时依赖）；`events.test.ts`
- [X] T011 [US3] `common/js/dsh-plugins/sts2/loop/src/orchestrator.ts`：`Sts2Orchestrator` 组装（对照 `contracts/sts2-loop-minimal.md` §3/§6）——物化 `compose(preset)` → `ctx.agents.create`（sessionId `${session}/main`、绑定 preset 声明与 model 路由）→ `preset.setup(agentCtx)`，失败整体回滚保持未物化；`members()`/`member()`/`teamEvents()`/`snapshot()`（`activation` 恒 `"main"`、无 phase、`queued` = steered 计数）/`submit`/`cancel`/`whenQuiescent`/`dispose`；`orchestrator.test.ts`（物化/回滚/静止-在途-收束/排队重放/冻结恢复/快照；状态错误用包内类，`code` 与 host `SceneStateError` 同构——包不 import host）；`src/index.ts` 公开面（`Sts2Orchestrator`/`Sts2OrchestratorDeps`/插件行 `name: "sts2-loop"`、`inject: []`、空 `apply`）+ README 终态化
- [X] T012 [US3] sts2 场景开放：`projects/game/agent_v2/src/scenes.ts` sts2 条目改为 `roster: ["main"]`/`presetRoles: ["main"]`/`rolePoolTemplate: {main: "main"}`/`faceMembers: ["main"]`/`teamGoal: ""`/`orchestratable: true` + 工厂；新建 `projects/game/agent_v2/src/scene-sts2.ts`（工厂直构 `Sts2Orchestrator`，无额外派生），并 `bazel run //:gazelle projects/game/agent_v2` 补 `server_lib` 对 `@dominion/dsh-sts2-loop` 的 link dep；`scenes.test.ts` 更新（079 的 sts2 未接线断言改为已接线语义——`wiredScene("sts2")` 通过；接线门/错误文案机制保留，以 `wiredScene("unknown-scene") === undefined` 与 `sceneNotWiredMessage("unknown-scene")` 单测覆盖，并断言 `orchestratable === true ⇔ createOrchestration 存在`、`faceMembers: ["main"]`）；`session.test.ts` 增 sts2 物化路径（真编排 + doubles：单成员 roster 校验、错误码、刷新），并**改写 079 的 3 条 sts2 未接线断言**（materialize 拒绝 / 双成员角色错误 / 未物化 `GetTeam`）为已接线语义；`server.test.ts` 增 sts2 preset 面开放断言（role `main` 创建/List role 过滤，079 §3 的 sts2 拒绝断言同步改写）
- [X] T013 [US3] `projects/game/agent_v2/cordis.yml`：新增 `- { id: sts2-loop, name: '@dominion/dsh-sts2-loop' }` 惰性行（空 config，注释锚定 080 契约；紧跟现有 `sts2` 行之后，对齐 079 布局 §3 的领域行邻近/24 行终态）；`preset-authoring` 行 Config `templates` 快照新增 `main` 池模板（name/description 对齐 player/planner 行；`plugins` 仅 persona 子行，`config.prefix` = `"你是 sts2 主 agent，负责与用户对话并统筹单局推进：所有行动以场景编排派发的房间上下文为准，不臆造未提供的游戏状态；回答简洁、明确。"`——必须含锚串"sts2 主 agent"（fake-llm `system_keywords` 依赖））；`projects/game/agent_v2/src/dsh.test.ts` 断言与文案同步（组合行；`templates` 快照键与逐行定义改为 `player`/`planner`/`main`，新增 `MAIN_TEMPLATE` 常量与 main persona 锚串断言；文件头注释、`it` 标题与行内注释的 "23-row"/"23 行" 一并改 24——宪法 VII 终态）；同步 `specs/070-dsh-upgrade/contracts/composition-manifest.md` §2（行集合 23→24 行、新增 sts2-loop 行与 main 池模板的断言契约）并核对 `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §3 的 24 行终态（含 sts2-loop 惰性行；偏差回改）
- [X] T014 [P] [US3] fake-llm fixture 与期望常量：新建 `projects/game/fake-llm/service/testdata/sts2_main.yaml`（**新增 1 条消息模板**，总数 41→42；条目名 `agent-v2-sts2-ping`；`responses_only: true` + `system_keywords: ["sts2 主 agent"]` + 触发词 `sts2-ping`——已核对不与既有触发词（含 `hello`）构成子串包含）；`projects/game/fake-llm/service/BUILD.bazel` 的 `service.embedsrcs` 新增 `testdata/sts2_main.yaml`（`//go:embed testdata/*` 只嵌入声明文件，漏列则 fixture 不进二进制）；`projects/game/fake-llm/service/message_store_test.go` 嵌入断言同步——重钉总数（41→42）、name 排序列表与受影响的位置索引断言；新建 `projects/game/testplan/sts2_conversation_test.go` 骨架（package + 包级常量：触发词 `sts2-ping` 与期望回包文本；本任务不写用例）——T016 在同一文件补四用例
- [X] T015 [P] [US3] testplan helper 参数化：`projects/game/testplan/agent_v2_helpers_test.go`——`agentV2SessionName`/`ensureAgentV2Session`/preset 创建族（`postAgentV2PresetCreate`/`createAgentV2PresetWithRole`/`listAgentV2PresetsByRole`）增加 `templateID` 参数，全部 saolei 既有调用点显式传 `saoleiTemplateID`（编译期全量更新、零语义变化）；新增 `updateSts2MainTeam` 薄包装（内部调 `updateAgentV2TeamMembers` 传单成员 `main`）；既有 saolei 测试文件随参数化编译通过
- [X] T016 [US3] `game-sts2` suite：新建 `projects/game/testplan/deploy_sts2.yaml`（六服务精简拓扑：mongo/session/fake-llm/proxy/agent-v2-test（`GLM_LLM_TARGET: dominion:///game/fake-llm:8080` 及 opencode 同型 env）/gateway（`game.liukexin.com` + `/api/v1/`、`/api/v2/` 路由）——对照 `contracts/testplan-game-sts2.md` §1）；`projects/game/testplan/system_test.yaml` suites 末尾挂 `game-sts2`，并同步顶部 suite 描述（四 suite → 五 suite）；在 T014 建立的 `projects/game/testplan/sts2_conversation_test.go` 中补四用例（`TestSts2MainMaterializeAndRoster`/`TestSts2MainConversationTurn`/`TestSts2MainQuiescenceAndRefresh`/`TestSts2NonRosterMemberNotFound`——G/W/T 对照契约 §3；用例 3 覆盖静止、刷新与**取消**三窗口：`:cancel` → `turn_end{CANCELED}` → 冻结 → 再 Send 恢复；sts2 preset 创建一律传**空 persona**（保留 main 池模板 persona 锚串，见 `common/js/dsh-plugins/preset-authoring/src/derive.ts`）；复用既有 helper，`style/golang.md` given/when/then 结构）；`projects/game/testplan/README.md` §2 suite 表、首段拓扑说明与 §5 运行说明（四→五 suite；示例 `--timeout` 改为 20m）同步新增 `game-sts2`，§6 第 3 条（常量约定）补一句"suite 专用期望常量可与该 suite 用例文件同文件（sts2 = `sts2_conversation_test.go`）"；`BUILD.bazel` 新增 `go_largetest` target `sts2_conversation_test`（srcs 含 `helpers_test.go`/`agent_v2_helpers_test.go`/`sts2_conversation_test.go`）；`bazel build //projects/game/testplan:sts2_conversation_test` 通过

**Checkpoint**: 全部实现就位——包/ host /物料构建单测全绿，待 Phase 6 大型测试验收。

---

## Phase 6: Polish & 验收（跨 story 收尾）

**Purpose**: 大型测试完整验收（宪法 VI）、文档终态化、结构检查与交付审计。

**文档清单**：

- **代码规范文档**：`style/large_test.md`（验收执行与 testplan skill 约定）；`style/golang.md`（T018/T020 若需修正 Go 用例或断言时的规范）；`style/javascript.md` 与 [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)、[TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)、[Vitest Mocking Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（T019 若需回改 TS 实现时的规范）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/080-host-main-sub-model/quickstart.md` V1-V5 全项（V1 构建/单测、V2 关键验证点、V3 结构硬/软门禁、V4 大型测试验收、V5 手动冒烟）；`specs/080-host-main-sub-model/contracts/scene-registry.md` §1-§7（T017/T019：场景注册/seam/投影/职责边界的终态对账基准）；`specs/080-host-main-sub-model/contracts/face-main-sub.md` §1-§5（T017/T019：face 语义终态）；`specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md` §1-§7（T017/T019：最小编排包终态）；`specs/080-host-main-sub-model/contracts/testplan-game-sts2.md` §6（验收标准）；`specs/079-sts2-template-scaffold/contracts/scene-dispatch.md` §1-§5、`specs/079-sts2-template-scaffold/data-model.md` §2/§3/§5/§6、`specs/079-sts2-template-scaffold/quickstart.md` V1-V7（V1/V5 24 行断言、V2 布局、V3 数据面、V4 场景操作成功语义、V6 五 suites、V7 结构检查白名单）、`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §1-§4、`specs/079-sts2-template-scaffold/spec.md` 顶部 080 后续说明（T020 的同步对象与 T019 的一致性核对基准）；`specs/059-agent-v2-team-mode/contracts/team-api.md` §2/§3、`specs/060-agent-v2-team-optimize/contracts/team-api.md` §1、`specs/060-agent-v2-team-optimize/data-model.md` §2、`specs/060-agent-v2-team-optimize/research.md` R7（T020 的场景化补注与 T019 对账基准）；`projects/game/agent_v2/README.md`（T017 待更新对象的现状基线）；`projects/game/testplan/README.md`（T016 更新后的 suite 清单与常量约定基线）

- [X] T017 [P] README 终态化：更新 `projects/game/agent_v2/README.md`——host 多场景/场景注册（`scenes.ts` 行为级演进）/中立编排 seam/动态角色投影/sts2 最小接线各节按终态表述（宪法 VII：只述当前状态，引用 080 契约路径；支撑 SC-002 的文档面）；核对 `common/js/dsh-plugins/sts2/loop/README.md` 与实现一致
- [X] T018 大型测试完整验收（宪法 VI，spec FR-008/FR-009、SC-001/SC-003）：经 testplan skill 执行 `guitar run projects/game/testplan/system_test.yaml --timeout=20m`（五 suite 全链路预算；若仍超时则按 suite 单独执行，每 suite 独立 run id + 部署→测试→清理闭环）——既有四 suite（game-system/game-disconnect/game-memory-down/game-stall）全部用例通过（saolei 零回归）+ `game-sts2` 四用例全部通过；任一 failed/flaky 即验收未通过，修复后重跑至全绿（禁止以 `bazel build` 替代执行）
- [X] T019 终验走查与交付审计：执行 `specs/080-host-main-sub-model/quickstart.md` V1-V5 全项——V3 使用其固定命令集（硬门禁零命中含注释；软门禁仅注释/`specs/` 规范链接命中并逐条记录行号），可执行代码中的场景词汇仅存于 `scenes.ts`/`scene-saolei.ts`/`scene-sts2.ts`，`orchestration.ts` 必须零场景词汇（SC-002 达成）；git diff 范围审查——`projects/game/web/`、desktop 前端、Go 服务（session/memory/proxy/gateway）零改动确认（spec Assumptions 与 079 契约 §1 面不变），并按 FR-010 负向边界断言：`common/js/dsh-plugins/sts2/game/` 无改动、无 fake-sts2 服务/desktop relay 新增；4 份 080 契约逐条与实现行为对账并记录 SC-004 验收结论；`scene-sts2.ts`/`Sts2Orchestrator` 代码审计无 `desktopBridge` 引用（sts2 无 desktop flow 语义，FR-010）；核对 T020 同步后的 079 契约/数据模型/quickstart、`specs/059-agent-v2-team-mode/contracts/team-api.md` §2/§3、`specs/060-agent-v2-team-optimize/contracts/team-api.md` §1、`specs/060-agent-v2-team-optimize/data-model.md` §2、`specs/060-agent-v2-team-optimize/research.md` R7 不与 080 行为矛盾（发现偏差回改实现或修订文档并重跑 T018）
- [X] T020 [P] 079/059/060 文档终态核对与补正（plan 跨 feature 同步约束；SC-004 下游输入）：核对 `specs/079-sts2-template-scaffold/spec.md` 顶部 080 后续说明、`contracts/scene-dispatch.md` §1/§2/§3/§4（sts2 `UpdateTeam`/preset CRUD 开放、roster/preset 词汇 `main`、编排 `Sts2Orchestrator`；fail-loud 仅保留未知模板/未接线场景机制；场景词汇落点含 `scene-saolei.ts`/`scene-sts2.ts`）、`data-model.md` §2/§3/§5/§6（sts2 行已接线/成功语义、组合清单 24 行）、`quickstart.md` V1-V7（V1/V5 24 行断言、V2 `sts2/loop` 布局、V3 preset 词汇 `main`、V4 sts2 场景操作成功语义、V6 五 suites、V7 结构检查白名单与 080 V3 取代预期）与 `contracts/dsh-plugins-layout.md` §1-§4（`sts2/loop` 布局、包名映射、24 行惰性行、打包不变量）与实现行为一致；偏差回改文档或实现（文档已在规划期同步，本任务为终态核对）；同时补注 `specs/059-agent-v2-team-mode/contracts/team-api.md` §2（`KNOWN_TEMPLATES` 白名单、场景层校验归属、初始 activation 场景化）与 `specs/060-agent-v2-team-optimize/contracts/team-api.md` §1、`specs/060-agent-v2-team-optimize/data-model.md` §2、`specs/060-agent-v2-team-optimize/research.md` R7（activation 场景化补注）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**: 无依赖，立即开始；T001 与 T002 串行（T002 消费 T001 产物）
- **Phase 2（Foundational）**: 无硬依赖（T003 只新增类型模块，不 import 插件包）；可与 Phase 1 并行启动——**阻塞全部 user story**
- **Phase 3（US1）**: 依赖 T003；T004 ∥ T005，T006 依赖 T005（及 T004 的类型先行）
- **Phase 4（US2）**: 依赖 T006（session 泛化 + 注入 `scenes` registry）；T007 → T008 串行
- **Phase 5（US3）**: 依赖 T006（泛化路径）与 T003；T009 ∥ T010 ∥ T014 ∥ T015；T011 依赖 T009/T010；T012 依赖 T011；T013 依赖 T011（行引用包）；T016 依赖 T012/T013/T014/T015
- **Phase 6**: 依赖 Phase 3-5 全部完成；T017 ∥ T018 前置准备 ∥ T020；T019 最后

### User Story Dependencies

- **US1（P1）**: Foundational 后即可开始，无 story 间依赖——**MVP**
- **US2（P2）**: 依赖 US1 的 session 泛化与 `deps.scenes` seam（独立可测：合成场景/stub 编排，不需 US3）
- **US3（P3）**: 依赖 US1（泛化路径）；与 US2 无实现依赖但端到端语义由 US2 契约锚定（face-main-sub.md）——建议顺序执行

### Parallel Opportunities

- Phase 1：T001 内部无并行点（单包骨架）
- Phase 3：T004（history.ts）∥ T005（scene-saolei.ts/scenes.ts）——不同文件
- Phase 5：T009（drive.ts）∥ T010（events.ts）∥ T014（fake-llm fixture + 期望常量骨架）∥ T015（helper 参数化）——四个不同目录/文件（T014 与 T016 同文件但串行）
- Phase 6：T017（README）∥ T018 前置准备 ∥ T020（079 文档同步）

---

## Parallel Example: User Story 3

```bash
# 四个无依赖任务并行推进（不同文件/目录）：
Task: "T009 drive.ts + drive.test.ts（common/js/dsh-plugins/sts2/loop/src/）"
Task: "T010 events.ts + events.test.ts（common/js/dsh-plugins/sts2/loop/src/）"
Task: "T014 fake-llm fixture + 期望常量骨架（projects/game/fake-llm/service/testdata/、service/BUILD.bazel、testplan/sts2_conversation_test.go 常量段）"
Task: "T015 helper 参数化（projects/game/testplan/agent_v2_helpers_test.go）"
# 汇合后串行：T011（orchestrator 组装）→ T012（场景开放）→ T013（cordis）→ T016（suite）
```

---

## Implementation Strategy

### MVP First（User Story 1 Only）

1. Phase 1 + Phase 2（骨架 + seam 接口）
2. Phase 3（US1：场景注册 + saolei 适配 + host 泛化，单测零回归）
3. **STOP and VALIDATE**：`bazel test //projects/game/agent_v2/...` 全绿 + quickstart V3 固定命令集预检——此时已可交付"host 场景中立 + saolei 零变化"的结构价值

### Incremental Delivery

1. Setup + Foundational → seam 就位
2. US1 → 单测零回归验证（MVP）
3. US2 → face 语义单测闭环（合成场景）
4. US3 → sts2 最小接线 + 物料就位
5. Phase 6 → 大型测试全量验收（saolei 四 suite 零回归 + game-sts2 全绿）+ 文档终态（含 079 契约/quickstart 同步）

---

## Notes

- [P] = 不同文件且无未完成依赖；同文件任务串行（AGENTS.md：同文件编辑串行）
- 单测随任务执行（宪法 IV），不单列；大型测试验收 = T018（宪法 VI，不可用构建替代）
- 每个任务完成后 `bazel build` + 相关 `bazel test` 必须通过再进入下一任务；按逻辑组提交
- T003 只新增 seam 类型/迁移 `LOOP_ERROR_TEXT`，**不得**提前删除 `session.ts` 的 saolei-loop import 或改 `TeamSessionsDeps` 字段类型——这两步在 T006 完成，保证 Phase 2 checkpoint 可绿
- T004 的 history 改动（`MemberRole`/`views`/`recordView`）是 T007 viewless 动态投影的前提；T004 必须先于 T007 完成
- saolei 零变化是硬约束：T005/T006 中除 079 的 sts2 未接线断言（由 T012 改写）外，既有测试断言**零修改**必须全绿（若必须改断言即视为行为变化，停止并回规）
- T012 会改写 079 的 sts2 fail-loud 断言（`session.test.ts`/`server.test.ts`/`scenes.test.ts` 三处，预期内——面开放，错误断言改为成功语义）；除此之外不得修改任何既有断言
- 部署闭包硬前提（`dsh-plugins-layout.md` §4）：新包的 `.bazelignore` 条目在 T001、`server_pkg.runtime_deps` 在 T002，缺一即运行时裸名解析失败或构建被 `verify_node_modules_ignored` 拒绝
- fake-llm 触发词新增后必须全 suite 回归（testdata 全进程共享，子串冲突风险——quickstart 回归提示）；`sts2_main.yaml` 新增 1 条后 `message_store_test.go` 总数断言为 42，触发词 `sts2-ping` 已避开 `hello` 等既有关键字
- T014/T016 同文件串行：T014 先建 `sts2_conversation_test.go` 常量骨架，T016 再补用例（T014 的 `[P]` 仅指与 T009/T010/T015 并行）
