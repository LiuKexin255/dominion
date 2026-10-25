# Tasks: sts2 场景模板脚手架与 dsh-plugins 通用/专用分层

**Input**: Design documents from `/specs/079-sts2-template-scaffold/`

**Prerequisites**: `specs/079-sts2-template-scaffold/` 下的 `plan.md`（required）、`spec.md`（required for user stories）、`research.md`、`data-model.md`、`contracts/`

**Tests**: 单测/构建按宪法原则 IV 内嵌于各代码任务（任务描述含"同步测试并通过相关 target 的 `bazel build`/`bazel test`"要求，不单列 task）；大型测试按宪法原则 VI 单列 task（阶段①末 + 终验收）。

**Organization**: 阶段次序遵循 plan 阶段用户裁定 **归组 → 注册+泛化 → 骨架**（`specs/079-sts2-template-scaffold/research.md` D8），故执行序为 US3 → US1 → US2 → US4；每个 phase 为独立可验证、可中断恢复的增量（各 phase 末全绿）。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- 仓库根相对路径（如 `common/js/dsh-plugins/`、`projects/game/agent_v2/`）；绝对路径仅用于命令参数（pnpm `--dir` 要求，AGENTS.md）。

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

无需任何任务：本 feature 全部在仓库既有结构内变更（无新项目/框架初始化、无新依赖框架、无存储迁移）。各 phase 开始前按其「文档清单」完成阅读（宪法原则 V 门禁）。

**文档清单**（本 phase 无任务，显式标注为空）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无

---

## Phase 2: User Story 3 - dsh-plugins 通用/专用分层（执行序①，spec P3）

**Goal**: 5 个 saolei 专用插件归组至 `common/js/dsh-plugins/saolei/{game,loop,tools,memory,hooks}`（基础包落位 `saolei/game`），通用插件保留顶层；包名/公开 API/依赖方向不变，行为零变化。

**Independent Test**: 全仓构建与单测全绿；目录布局符合 `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §1/§2 的 **saolei 组部分**（顶层 6 个通用插件 + `saolei/{game,loop,tools,memory,hooks}`；`sts2/` 分组由 Phase 5 创建，完整 §1/§2 由 T019 终验）；既有大型测试全部用例通过（零回归证据）。

**文档清单**（本 phase 开始前必读）：

- **代码规范文档**：
  - `style/javascript.md`（ESM 书写规则与 `vitest_test` 宏 data 规则）
  - [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
  - `style/golang.md`（T007 修改 `projects/game/testplan/agent_v2_helpers_test.go`、`projects/game/fake-desktop/service/executor.go` 的路径注释；T008 执行 Go 大型测试）
  - [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` 引用的规范基石）
  - [Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)（`style/golang.md` 声明的 normative 风格决策）
  - `style/large_test.md`（T008 大型测试的组织与 `guitar` 执行规范）
- **官方文档**：
  - [pnpm settings — workspace packages glob 语义](https://pnpm.io/settings)（确认 `common/js/**` 递归匹配分组子包）
  - [pnpm update — 依赖/lockfile 更新命令（`pnpm up` 为其别名）](https://pnpm.io/cli/update)
  - [aspect-build/rules_js — pnpm 锁文件与 node_modules 忽略校验](https://github.com/aspect-build/rules_js/blob/main/docs/pnpm.md)（`npm_translate_lock` / `verify_node_modules_ignored`，`.bazelignore` 强校验背景）
- **技术文章/技术参考文档**：
  - `specs/048-js-esm-migration/contracts/esm-package-conventions.md` §2（`style/javascript.md` 引用的包级契约：tsconfig/`.swcrc` 锁步、`paths` 指向 `<dep>/src/index.js` 规则——迁移 tsconfig 必读）
  - `specs/079-sts2-template-scaffold/research.md` D5（布局与同步清单）、D8/D9
  - `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §1/§2/§4
  - `specs/079-sts2-template-scaffold/data-model.md` §4
  - `specs/079-sts2-template-scaffold/quickstart.md` V1/V2/V6
  - `projects/game/testplan/README.md`（T008 执行的测试计划 `system_test.yaml`、四套部署拓扑与运行方式）

**Implementation**:

- [X] T001 [US3] 以 `git mv` 归组迁移（保历史，禁复制重建）：基础包两跳下移 `common/js/dsh-plugins/saolei` → `common/js/dsh-plugins/saolei-game` → 建 `common/js/dsh-plugins/saolei/` 后再 `git mv` 至 `common/js/dsh-plugins/saolei/game`；随后 `git mv` 四包：`saolei-loop→saolei/loop`、`saolei-tools→saolei/tools`、`saolei-memory→saolei/memory`、`saolei-hooks→saolei/hooks`。全部包名（`@dominion/dsh-saolei*`）与 `package.json` 内容不变
- [X] T002 [US3] 更新根 `.bazelignore`：删除 5 条旧 `common/js/dsh-plugins/saolei*/node_modules` 条目与残留的 `common/js/dsh-plugins/saolei-interrupt/node_modules`（目录已不存在），新增 `common/js/dsh-plugins/saolei/game/node_modules`、`.../saolei/loop/node_modules`、`.../saolei/tools/node_modules`、`.../saolei/memory/node_modules`、`.../saolei/hooks/node_modules`。`MODULE.bazel:132` 配置了 `verify_node_modules_ignored = "//:.bazelignore"`（rules_js 文档将该属性标注为 Bazel 7.x 语义）；若当前 Bazel 版本不再触发该检查，以 `bazel build //...` 无 node_modules 相关告警为准完成验收
- [X] T003 [US3] 更新 `projects/game/agent_v2/BUILD.bazel`：`server_pkg.runtime_deps` 中 5 个 saolei 包 label 改为 `//common/js/dsh-plugins/saolei/{game,loop,tools,memory,hooks}:runtime_pkg`（workspace 包保持 runtime_deps 通道，禁入 npm_deps）
- [X] T004 [US3] 更新 `common/js/dsh-plugins/saolei/loop/BUILD.bazel`：对基础包的 runtime 依赖由 `//common/js/dsh-plugins/saolei:runtime_pkg` 改为 `//common/js/dsh-plugins/saolei/game:runtime_pkg`（team 依赖不变）
- [X] T005 [P] [US3] 重算 6 处 tsconfig `paths` 相对层级：5 个 saolei 包自身 `tsconfig.json`（组内互引、`@dominion/game-saolei-board` 映射，以及 `saolei-memory` 对 `@dominion/common-js-memory` 的映射）与 `projects/game/agent_v2/tsconfig.json`（saolei-loop 条目），映射仍指向 `<dep>/src/index.js`（`specs/048-js-esm-migration/contracts/esm-package-conventions.md` §2）
- [X] T006 [US3] 重建 workspace 链接与 lockfile：`bazel run @pnpm -- --dir <仓库根绝对路径> up`（lockfile importer 键与 `link:` 路径随迁移重算；禁止手改 `pnpm-lock.yaml`），随后 `bazel build //...` 确认全绿
- [X] T007 [P] [US3] 迁移后全仓路径终态化（宪法原则 VII）：先记录迁移前的旧路径清单（`git grep -n -e 'dsh-plugins/saolei-' -e 'dsh-plugins/saolei/' -- common projects experimental`，排除 `node_modules`），迁移后以 `grep -rnP 'dsh-plugins/saolei(?!/(game|loop|tools|memory|hooks))' common projects experimental --exclude-dir=node_modules` 核对（该负向断言排除 T002-T004 已写入的终态路径，避免把 `saolei/game` 二次改写）；按映射逐处替换——`saolei`→`saolei/game`、`saolei-loop`→`saolei/loop`、`saolei-tools`→`saolei/tools`、`saolei-memory`→`saolei/memory`、`saolei-hooks`→`saolei/hooks`；**禁止**对 `dsh-plugins/saolei` 做统一前缀替换（会把 `saolei-tools` 错写成 `saolei/game-tools`）。覆盖：5 个迁移包 README 首行与交叉引用（`saolei-memory` 无 README，不新增）、各包 `src/` 与 `BUILD.bazel` 注释（如 `saolei/game/BUILD.bazel`、`saolei/tools/BUILD.bazel`、`saolei/hooks/BUILD.bazel`、`saolei/loop/src/orchestrator.ts:1335`、`saolei/hooks/src/{index.test.ts,tool.test.ts,handler.ts,tool.ts}`），以及非迁移包中引用旧路径的注释/文档（`common/js/memory/src/face.ts`、`common/js/dsh-plugins/llm-glm/src/serialize.test.ts`、`common/js/dsh-plugins/llm-opencode-go/src/serialize.test.ts`、`common/js/dsh-plugins/desktop-bridge/src/index.test.ts`、`projects/game/pkg/saolei-board/src/core/{types,geometry}.ts`、`projects/game/pkg/saolei-board/README.md`、`projects/game/agent_v2/README.md`、`projects/game/testplan/agent_v2_helpers_test.go`、`projects/game/fake-desktop/service/{executor.go,executor_test.go}`、`experimental/js/team_graph_spike/src/team-graph.ts`）；`specs/`、`survey/` 历史文档不改写
- [X] T008 [US3] 大型测试零回归验证（宪法原则 VI）：经 testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`（完成部署→测试→清理闭环），全部用例通过（failed/flaky 均视为未通过，须修复重跑）

**Checkpoint**: 目录分层落定、行为零变化有端到端证据。**门禁**：T008 全绿后方可进入 Phase 3。

---

## Phase 3: User Story 1 - sts2 成为系统可识别的场景模板（执行序②a，spec P1）🎯 MVP

**Goal**: Go 侧注册 `sts2`（`gameconst` 白名单）+ desktop/web 本地常量，数据面（session/wiki，以及 proxy 的模板校验）对 `templates/sts2/...` 提供与 saolei 同构的接受；memory 按 `(template, preset)` 作用域结构接受。agent_v2 的 TS 镜像 `KNOWN_TEMPLATES` 与场景表在 Phase 4 同批落地，避免注册先行时 preset 资源名仍拼 saolei 常量造成跨模板写入。

**Independent Test**: `specs/079-sts2-template-scaffold/quickstart.md` V3——经 gateway 对 `templates/sts2/sessions` 全生命周期 CRUD 成功且与 saolei 分区隔离；desktop 模板下拉出现 sts2（T010 组件测试断言）；未知模板仍被拒绝。

**文档清单**（本 phase 开始前必读）：

- **代码规范文档**：
  - `style/golang.md`（Go 常量/注释/表驱动单测规范）
  - [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` 引用的规范基石）
  - [Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)（`style/golang.md` 声明为 normative 的具体风格决策）
  - `style/javascript.md`（desktop/web 前端 TS 改动规范）
  - [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
  - `style/api.md`（REST/gRPC 资源与错误语义规范；AIP-122 为本文引用的具体条目）
- **官方文档**：
  - [AIP-122 Resource Names](https://google.aip.dev/122)（`templates/{template}` 资源名语义；031 契约引用）
- **技术文章/技术参考文档**：
  - `specs/031-team-template-mode/spec.md`（FR-001 Template 资源模型、FR-024 desktop 本地常量约束——T009/T010 的注释与行为锚点）
  - `specs/079-sts2-template-scaffold/research.md` D1（注册面）、D7（前端常量）
  - `specs/079-sts2-template-scaffold/contracts/scene-dispatch.md` §1（接受面矩阵）、§5（前端常量契约）
  - `specs/079-sts2-template-scaffold/data-model.md` §1
  - `specs/079-sts2-template-scaffold/quickstart.md` V3

**Implementation**:

- [X] T009 [P] [US1] Go 注册：`projects/game/pkg/gameconst/const.go` 新增 `var Sts2Template = game.TemplateName{TemplateID: "sts2"}` 并将 `"sts2"` 加入 `knownTemplateIDs`（注释对齐 SaoleiTemplate 的 031 FR-001 说明）；同步 `projects/game/pkg/gameconst/const_test.go`：按 `TestSaoleiTemplate` 同型新增 `TestSts2Template`、`TestValidateTemplateName`/`TestIsKnownTemplateID` 表中补 `sts2` 用例；若 `projects/game/proto_test.go` 的资源名用例需要 sts2 变体一并补充；proxy 的模板校验（`projects/game/proxy/handler/agent.go:341,360,375`）与 memory 的 `(template, preset)` 作用域（`projects/game/memory/handler/handler_test.go`，资源名 `templates/sts2/presets/{preset}/memories`，仅结构接受、不校验 preset 存在性）各增一条 sts2 通过用例，session/wiki handler 既有用例随白名单扩容回归；通过 `bazel test //projects/game/...`。agent_v2 的 TS 镜像 `KNOWN_TEMPLATES` 属场景表改造，见 T013
- [X] T010 [P] [US1] desktop 常量：`projects/game/desktop/frontend/src/api.ts` 新增 `export const TEMPLATE_STS2 = 'sts2'` 并将 `TEMPLATES` 改为 `[TEMPLATE_SAOLEI, TEMPLATE_STS2]`，注释由 "Current known templates: saolei only" 终态化为 saolei 与 sts2（引用 `specs/079-sts2-template-scaffold/contracts/scene-dispatch.md` §5；维持 FR-024 本地常量、禁止模板列表请求）；`App.svelte` 模板下拉随 `TEMPLATES` 自动呈现，无需结构改动；同步更新 `projects/game/desktop/frontend/src/App.test.ts` 的 `./api` mock（补 `TEMPLATE_STS2: 'sts2'`，`TEMPLATES` 改为 `['saolei','sts2']`）并断言 `[data-testid="template-select"]` 含两个选项、切换至 sts2 后 `listSessions` 以 `'sts2'` 模板调用，`projects/game/desktop/frontend/src/api.test.ts` 增 `TEMPLATES` 精确断言；通过前端既有 build/test target
- [X] T011 [P] [US1] web 常量集合化：`projects/game/web/frontend/src/App.tsx` 将单常量 `TEMPLATE_SAOLEI` 重构为"已知模板集合 + 当前场景选择常量"（集合含 `'saolei'`/`'sts2'`，选择常量值仍 `'saolei'`），`App.tsx` 5 处使用点（:574/:589/:722-724）改引选择常量，行为不变；`projects/game/web/frontend/src/scene-words.ts` 场景词扫描表增 `'sts2'` 与 `'杀戮尖塔'`；通过 web 前端既有 build/test target

**Checkpoint**: sts2 的 session/wiki 数据面全生命周期可用、memory 结构接受（`specs/079-sts2-template-scaffold/quickstart.md` V3）。**门禁**：T009-T011 全部通过后进入 Phase 4。

---

## Phase 4: User Story 2 - 场景差异按 template 泛化（执行序②b，spec P2）

**Goal**: agent_v2 的 saolei 场景假设（preset 词表/池映射/资源父名、roster 校验、任务目标文案）收敛为场景表 `scenes.ts`；saolei 行为零变化；sts2 场景化操作 fail-loud。

**Independent Test**: `specs/079-sts2-template-scaffold/quickstart.md` V4（sts2 的 UpdateTeam/CreatePreset/ListPresets 等 `INVALID_ARGUMENT` 拒绝）+ V7（`server.ts`/`session.ts` 无 saolei 场景字面量残留）+ 既有 saolei 单测全量回归通过（端到端大型测试零回归由 FR-010/T020 终验收覆盖）。

**文档清单**（本 phase 开始前必读）：

- **代码规范文档**：
  - `style/javascript.md`（源码书写规则、测试执行模型与 mock 约定）
  - [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
  - `style/api.md`（gRPC 资源与错误语义规范；AIP-122/AIP-193 为其引用的具体条目）
- **官方文档**：
  - [AIP-122 Resource Names](https://google.aip.dev/122)（模板资源名与解析语义）
  - [AIP-193 Errors](https://google.aip.dev/193)（fail-loud 的 `INVALID_ARGUMENT` 语义）
- **技术文章/技术参考文档**：
  - `specs/079-sts2-template-scaffold/research.md` D2（场景表形态）、D4（fail-loud 语义）
  - `specs/079-sts2-template-scaffold/contracts/scene-dispatch.md`（全文——分派契约与错误文案）
  - `specs/079-sts2-template-scaffold/data-model.md` §1/§2/§3/§6
  - `specs/079-sts2-template-scaffold/quickstart.md` V4/V7
  - `specs/059-agent-v2-team-mode/contracts/preset-api.md` §2（角色词表与创建后不可变）
  - `specs/059-agent-v2-team-mode/contracts/team-api.md` §2/§6（team 资源校验与错误语义）
  - `specs/060-agent-v2-team-optimize/contracts/preset-derivation.md` §2（角色 → 池模板映射）
  - `specs/070-dsh-upgrade/contracts/preset-model-mapping.md` §3（`templates` 快照契约）

**Implementation**:

- [X] T012 [US2] 新建场景表 `projects/game/agent_v2/src/scenes.ts`：`SceneSpec` 接口（字段 `roster: readonly string[]`、`presetRoles: readonly string[]`、`rolePoolTemplate: Readonly<Record<string, string>>`、`orchestratable: boolean`、`teamGoal: string`）与只读表 `SCENES`，两条目取值严格按 `specs/079-sts2-template-scaffold/data-model.md` §2：saolei = `roster: ["player","planner"]`、`presetRoles: ["player","planner"]`、`rolePoolTemplate: {player:"player", planner:"planner"}`、`orchestratable: true`、`teamGoal` 为 `session.ts:91` 现文案原文；sts2 = `roster: []`、`presetRoles: []`、`rolePoolTemplate: {}`、`orchestratable: false`、`teamGoal: ""`；含场景表内容单测（新建 `scenes.test.ts`）
- [X] T013 [P] [US2] `projects/game/agent_v2/src/server.ts` 泛化：`KNOWN_TEMPLATES` 加入 `"sts2"`（注释保持 gameconst mirror 锚定；与场景表同批落地，避免注册先行时 preset 资源名仍拼 saolei 常量造成跨模板写入）；`PresetRole`/`isSceneRole`/`ROLE_TEMPLATE`/`SAOLEI_TEMPLATE`（现 :242-260）改为按 template 查 `SCENES` 校验与映射——Create/List 取 `parseTemplateParent(request.parent)` 的 template，Get/Update/Delete 取 `parsePresetResource(request.name)` 的 template；角色校验与错误文案由 `SCENES[template].presetRoles` 构造（`role is required and must be a known ${template} scene role (${quotedRoles}), got "..."`、List 的 role filter 同型），saolei 产出字符串与现状逐字节一致；`presetRoles` 为空时在入口按 `specs/079-sts2-template-scaffold/contracts/scene-dispatch.md` §3 文案拒绝（`template "{template}" has no preset vocabulary (scene not wired)`，role 参数无论是否合法均同错、不进入存储）；preset 资源名构造（:264 `templates/${SAOLEI_TEMPLATE}/presets/...`）改用该 template 段（saolei 产出字符串不变）；将 :566-567 的旧场景注释改为按 `SCENES[template]` 校验的终态表述；同步 `projects/game/agent_v2/src/server.test.ts`：增 sts2 的 Create/List/Get/Update/Delete 五个 RPC fail-loud 用例（parent 与资源名两条 template 来源）与 saolei 请求回归用例；通过 `bazel test //projects/game/agent_v2/...`
- [X] T014 [P] [US2] `projects/game/agent_v2/src/session.ts` 泛化：materialize 入口在 `validateMemberStructure`（:556）**之前**按 `parsedSession.template` 查 `orchestratable`，`false` 时抛 `TeamSessionError`（code `INVALID_ARGUMENT`，文案 `scene "sts2" is not wired for team orchestration yet`）——保证任意 roster（含空 members）同错、不进入任何校验/存储；`validateSaoleiMembers`（:999-1013）改为 `validateSceneRoster(members, template)`（并同步 :1019 引用旧函数名的注释），成员数与角色集合由 `SCENES[template].roster` 供给，错误文案由 `template` + `roster` 构造（saolei 产出 `saolei team materialization requires exactly 2 members (player and planner), got N` / `saolei team member roles must be exactly {"player", "planner"}, got [...]`，逐字节不变），函数内无 `"player"`/`"planner"` 字面量；后续角色取值（`memberFor` :560-561、`validateMemberPreset` :571-572、`orchestrator.member` :637-638、`createMemberRuntime` :651-652）改由 `roster` 供给（saolei 顺序不变；消费点按 `AgentMemberRole`/`MemberRole` 收窄，必要时补一个类型守卫；编排尾段仅在 `orchestratable=true` 时可达）；`isMemberRole`（:1110）的 `player`/`planner` 判定改查场景表 roster 并集（必要时接收 template 参数，由调用点从 session 资源名解析），`announcer` 改引已导入的 `ANNOUNCER_MEMBER_ROLE`；成员视图错误文案中的 roster 描述（`getTeamMember` :434 的 `player/planner/announcer`、`listMemberMessages` :511 的 `player/planner`）同样由 `SCENES[template].roster` 加 `ANNOUNCER_MEMBER_ROLE` 构造（saolei 产出字符串逐字节不变）；`TEAM_GOAL` 常量（:91）改由场景表 `teamGoal` 供给（saolei 值不变）；同步 `projects/game/agent_v2/src/session.test.ts`：增 sts2 materialize 拒绝用例（空 members 与任意 roster 均同错）；通过 `bazel test //projects/game/agent_v2/...`

**Checkpoint**: `specs/079-sts2-template-scaffold/contracts/scene-dispatch.md` §4 泛化不变量成立（SC-005 grep 验证 + saolei 全量单测回归）。**门禁**：T012-T014 通过后进入 Phase 5。

---

## Phase 5: User Story 4 - sts2 插件包骨架就位（执行序③，spec P4）

**Goal**: 创建 `common/js/dsh-plugins/sts2/game/` 骨架包 `@dominion/dsh-sts2`（惰性空插件），接线 agent_v2 依赖/打包通道，组合清单挂载惰性行（22→23 行）。

**Independent Test**: `specs/079-sts2-template-scaffold/quickstart.md` V5——骨架包独立构建/测试通过；agent_v2 部署产物含 `node_modules/@dominion/dsh-sts2`；`dsh.test.ts` 23 行断言通过；服务经大型测试部署正常启动（惰性行加载验证，由 T020 覆盖）。

**文档清单**（本 phase 开始前必读）：

- **代码规范文档**：
  - `style/javascript.md`（`vitest_test` 宏 data 规则、相对导入 `.js` 扩展名规则）
  - [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：
  - [DeepSeek Harness — Cordis primer（插件形态：`name` / `inject` / `apply`）](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cordis-primer.md)
  - [aspect-build/rules_ts — ts_project 使用说明（README）](https://github.com/aspect-build/rules_ts/blob/main/README.md)
  - [SWC — modules 配置](https://swc.rs/docs/configuration/modules)（`style/javascript.md` 引用的 `.swcrc` 模块配置基准）
  - [aspect-build/rules_swc — tsconfig 集成](https://github.com/aspect-build/rules_swc/blob/main/docs/tsconfig.md)（`style/javascript.md` 引用的 tsconfig/.swcrc 锁步约束）
  - [pnpm settings（workspace 包声明）](https://pnpm.io/settings)
- **技术文章/技术参考文档**：
  - `specs/048-js-esm-migration/contracts/esm-package-conventions.md` §2/§5（新包 `package.json`/`tsconfig.json`/`.swcrc` 锁步契约与测试数据规则——骨架包必须满足的包级规范）
  - `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（`vitest_test` shim 退出码契约：失败 fail-closed、空套件 vacuous pass；`style/javascript.md` §测试 与 esm-package-conventions §5 共同引用）
  - `specs/079-sts2-template-scaffold/research.md` D3（惰性行裁定）、D6（骨架包形态）
  - `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §3/§4/§5
  - `specs/079-sts2-template-scaffold/data-model.md` §5
  - `specs/075-saolei-game-foundation/contracts/saolei-foundation.md` §1（游戏基础插件形态先例——骨架包镜像的行形态）
  - `specs/070-dsh-upgrade/contracts/composition-manifest.md`（需修订为 23 行终态的契约原文）
  - `specs/079-sts2-template-scaffold/quickstart.md` V5
  - `survey/sts2-game-control.md` §5、`survey/sts2-mod-poc.md` §5（README 演进入口引用内容）

**Implementation**:

- [X] T015 [US4] 创建骨架包 `common/js/dsh-plugins/sts2/game/`：`package.json`（`"name": "@dominion/dsh-sts2"`、`"private": true`、`"type": "module"`、main/types/exports 指向 `./src/index.js`，peerDependencies 仅 `"@deepseek-ai/cordis": "catalog:"`，devDependencies 对齐既有包 catalog 条目）；`tsconfig.json` + `.swcrc`（按 `specs/048-js-esm-migration/contracts/esm-package-conventions.md` §2 锁步：nodenext/ES2020、es6+preserveImportMeta）；`src/index.ts` 导出惰性插件——`export const name = "sts2"; export const inject: string[] = []; export function apply(_ctx: Context): void {}`（插件导出名与清单行 `id` 一致；无 provide、无工具、无 section）；`src/index.test.ts` 断言插件形态（name 字段值、inject 为空数组、apply 为可调用函数）；`README.md` 记录包定位（sts2 场景基础占位、空实现现状）与演进入口（引用 `survey/sts2-game-control.md` §5、`survey/sts2-mod-poc.md` §5）
- [X] T016 [US4] 创建 `common/js/dsh-plugins/sts2/game/BUILD.bazel`：镜像 `common/js/dsh-plugins/saolei/game/BUILD.bazel` 结构——`npm_link_all_packages(name="node_modules")` + `ts_config` + `ts_project(:lib)`（deps 仅 `:node_modules/@deepseek-ai/cordis`、`:node_modules/@types/node`）+ `js_library(:pkg)`（srcs 含 `package.json`——裸名解析需 manifest）+ `js_runtime_library(:runtime_pkg)`（`package_name = "@dominion/dsh-sts2"`，无 npm_deps/runtime_deps）+ `vitest_test(:lib_test)`（data 为 `.ts` 源 glob 镜像 `:lib` 的 node_modules deps，规则见 `style/javascript.md`）；根 `.bazelignore` 增 `common/js/dsh-plugins/sts2/game/node_modules`；通过 `bazel build`/`bazel test //common/js/dsh-plugins/sts2/...`
- [X] T017 [US4] agent_v2 依赖接线：`projects/game/agent_v2/package.json` 增 `"@dominion/dsh-sts2": "workspace:*"`（dependencies，字母序归位）；`projects/game/agent_v2/BUILD.bazel` `server_pkg.runtime_deps` 增 `//common/js/dsh-plugins/sts2/game:runtime_pkg`（禁入 npm_deps）；`bazel run @pnpm -- --dir <仓库根绝对路径> up` 重生成 lockfile 后 `bazel build //projects/game/agent_v2/...` 验证部署闭包含骨架包
- [X] T018 [US4] 组合清单挂载与契约同步：`projects/game/agent_v2/cordis.yml` 在 `saolei` 领域行后新增 `- { id: sts2, name: '@dominion/dsh-sts2' }`（附注释：脚手架惰性占位行、空 apply、场景 feature 填充实现，引用 `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §3）；`projects/game/agent_v2/src/dsh.test.ts` 组合清单断言 22→23 行（id 与 name 双列表在 `saolei` 行后插入 `sts2`/`@dominion/dsh-sts2`），**同时**把测试标题（现 "direct-composes the 22-row plugin set…"）、文件头注释（现 :27 "22-row set…"）与 :816 注释中的 "22" 改为 23（宪法原则 VII 终态表述）；修订 `specs/070-dsh-upgrade/contracts/composition-manifest.md` §2 为 23 行终态——行集合说明（现 :14 "终态 22 行"）与断言契约段（现 :48 "以终态 22 行精确断言"）两处均须同步；通过 `bazel test //projects/game/agent_v2/...`

**Checkpoint**: 骨架包全通道就位（打包/部署/裸名解析/挂载）。**门禁**：T015-T018 通过后进入终验收。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 终验收与跨故事一致性

**文档清单**（本 phase 开始前必读）：

- **代码规范文档**：
  - `style/large_test.md`（大型测试组织与 guitar 执行规范）
  - `style/golang.md`（T020 出现失败用例需修复 Go 测试代码时的规范）
  - [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` 引用的规范基石）
  - [Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)（`style/golang.md` 声明的 normative 风格决策）
- **官方文档**：
  - 无
- **技术文章/技术参考文档**：
  - `specs/079-sts2-template-scaffold/quickstart.md`（V1-V7 全量验证场景）
  - `specs/079-sts2-template-scaffold/spec.md`（Success Criteria SC-001~SC-005 对照）
  - `projects/game/testplan/README.md`（测试计划与部署拓扑说明）

- [X] T019 执行 `specs/079-sts2-template-scaffold/quickstart.md` 全量验证 V1-V7（构建/布局/数据面/fail-loud/清单与产物/结构 grep），逐项结果记录于实施 PR/MR 描述（V1-V7 对照表）；另做负向边界核查（FR-009）：确认未新增 sts2 工具面、desktop 桥 relay / 新 `FlowPart` kind、fake-sts2 服务、fake-llm sts2 fixture、sts2 preset 池模板与 testplan sts2 用例
- [X] T020 终验收大型测试（宪法原则 VI）：经 testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`（完成部署→测试→清理闭环），**全部用例通过**；存在 failed/flaky 即验收未通过，修复后重跑直至全绿（禁止以 `bazel build` 编译测试 target 替代实际执行）。T008 为阶段①（纯目录归组）的零回归证据，本条为 feature 终验收，两者均不可省略

**Checkpoint**: spec SC-001~SC-005 全部满足，feature 验收完成。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无任务。
- **US3 (Phase 2)**: 执行序①（用户裁定 D8：纯机械移动先落地、单独回归）——阻塞后续全部 phase（目录布局是后续接线 label 的前提）。
- **US1 (Phase 3)**: 依赖 Phase 2 完成（终态布局）。
- **US2 (Phase 4)**: 依赖 Phase 3（TS 泛化以 Go/常量注册先就位为前提，保证中间态语义干净：注册前 TS 闸门拒绝、泛化后 fail-loud 拒绝）。
- **US4 (Phase 5)**: 依赖 Phase 2（分组目录）与 Phase 4（清单断言一次性到位）。
- **Polish (Phase 6)**: 依赖全部 story 完成。

### User Story Dependencies

- **US3 (P3)**: 执行序第一——无依赖。
- **US1 (P1)**: 依赖 US3（label/布局终态）；Go 侧注册（T009）与前端常量（T010/T011）彼此独立；agent_v2 的 TS 镜像 `KNOWN_TEMPLATES` 随 US2 场景表同批落地（T013）。
- **US2 (P2)**: 依赖 US1（Go 注册与前端常量先行）；T012 → T013/T014（场景表是消费方改造的输入；TS 镜像与 preset 泛化在同一任务内一次性到位）。
- **US4 (P4)**: 依赖 US3+US2；T015 → T016 → T017 → T018（建包 → BUILD → 接线 → 清单，链式）。

### Parallel Opportunities

- T001 完成后，T005 可与 T002/T003/T004 并行（不同文件）。
- T009/T010/T011 完全并行（Go / desktop / web 三端不同文件）。
- T013 与 T014 在 T012 完成后可并行（`server.ts` 与 `session.ts` 不同文件）。
- T007（文档路径终态化）可与 T006 并行。

---

## Parallel Example: User Story 1

```bash
# 三端注册并行（不同文件、无依赖）：
Task: "T009 Go 注册 gameconst + const_test.go"
Task: "T010 desktop api.ts TEMPLATE_STS2"
Task: "T011 web App.tsx 常量集合化 + scene-words.ts"
```

---

## Implementation Strategy

### MVP First (US3 + US1)

1. 完成 Phase 2（归组 + 大型测试零回归）——布局与分层先落定。
2. 完成 Phase 3（Go 注册 + 前端常量）——sts2 的 session/wiki 数据面可用（SC-001 的 session/wiki 部分达成；memory 为 `(template, preset)` 结构接受，agent_v2 场景门禁随 Phase 4 落地）。
3. **STOP and VALIDATE**: 执行 `specs/079-sts2-template-scaffold/quickstart.md` V3 与当前阶段适用的 V1/V2 子项（V1 全仓构建/单测；V2 仅 saolei 组布局）；V1 的 23 行断言与 V2 的 `sts2/game/` 检查属阶段③完成后的终态项，此时不可执行。此阶段已可演示"Go 服务面认识 sts2"。

### Incremental Delivery

1. 归组（US3）→ 数据面注册（US1）→ 场景泛化（US2，fail-loud 语义就位）→ 骨架包（US4，23 行清单）→ 终验收（Phase 6）。
2. 每个 phase 末全绿，可中断恢复（中断后从下一 phase 继续，无需回溯）。

### Parallel Team Strategy

- Phase 3 内三端任务可分派三人并行；Phase 4 内 T013/T014 可双人并行（T012 先行合入）。

---

## Notes

- [P] tasks = different files, no dependencies
- 每 phase 开始前必须完成该 phase「文档清单」全部文档的完整阅读（宪法原则 V；含清单中间接引用的外部文档，均已显式列出）
- 构建与单测是代码任务的一部分（宪法原则 IV）：每个代码任务完成时即通过相关 target 的 `bazel build`/`bazel test`，不等待 phase 末
- JS 包的 BUILD.bazel 为手写 target（根 gazelle 仅注册 proto/go/python 语言，不生成 JS target）；修改 `package.json` 后用 `bazel run @pnpm -- --dir <仓库根绝对路径> up` 更新，禁止手改 `pnpm-lock.yaml`（AGENTS.md）
- 大型测试一律经 testplan skill 执行（`style/large_test.md`：`guitar run <plan.yaml>` 部署→测试→清理闭环）；全部用例通过才算验收
- Commit after each task or logical group; 每 phase 末为天然 review/中断边界
