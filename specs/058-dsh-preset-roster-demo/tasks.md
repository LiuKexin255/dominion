# Tasks: 058 dsh preset roster demo

**Input**: Design documents from `/specs/058-dsh-preset-roster-demo/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md, discussion-2026-09-08.md

**Organization**: 按 user story 分 phase（US1=显式会话选 preset / US2=preset CRUD 闭环 / US3=边界审计）；每 phase 附**文档清单**（Constitution 原则 V 三分类：代码规范文档 / 官方文档 / 技术文章·技术参考文档）。

**Tests**: 单测为实现任务的组成部分（Constitution 原则 IV：每次变更 `bazel build` + `bazel test` 并入任务，不单列）；大型测试单独验收任务（原则 VI）。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: US1/US2/US3；Setup/Foundational/Polish 无标签

---

## Phase 1: Setup（workspace 与依赖脚手架）

**文档清单**：
- **代码规范文档**：`style/javascript.md`（ESM 包契约：type: module/tsconfig/.swcrc 锁步）及其引用的外部规范：[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（js/ts 规范基准）、[TypeScript 模块参考（nodenext/.js 扩展名）](https://www.typescriptlang.org/docs/handbook/modules/reference.html)、[swc 模块配置](https://swc.rs/docs/configuration/modules)、[rules_swc tsconfig 锁步](https://github.com/aspect-build/rules_swc/blob/main/docs/tsconfig.md)
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md`（§5 workspace/catalog 增项）、`specs/058-dsh-preset-roster-demo/research.md`（R1 版本线、R8 宿主依赖面）

- [ ] T001 [P] `pnpm-workspace.yaml`：`packages` 增 `experimental/dsh/demo/agent-plugins/*`；`catalog` 增 `@deepseek-ai/dsh-agent-loop: 0.1.1-rc.2`、`@deepseek-ai/dsh-agent-presets: 0.1.1-rc.2`、`@deepseek-ai/dsh-persona: 0.1.1-rc.2`
- [ ] T002 [P] 新包骨架：`common/js/dsh-plugins/preset-authoring/{package.json,tsconfig.json,.swcrc}`（name `@dominion/dsh-preset-authoring`；deps：`@deepseek-ai/cordis`、`@deepseek-ai/schemastery`、`@deepseek-ai/dsh-agent-presets`（类型面）、`js-yaml`，均 catalog）与 `experimental/dsh/demo/agent-plugins/demo-echo/{package.json,tsconfig.json,.swcrc}`（name `@dominion/dsh-demo-echo`；deps：`@deepseek-ai/cordis`、`@deepseek-ai/schemastery`）
- [ ] T003 `experimental/dsh/demo/agent/package.json` 依赖增删（research.md R8/R12：删 `dsh-agent-spine-demo`；增 `dsh-agent-loop`/`dsh-session`/`dsh-tools`/`dsh-agent-presets`/`dsh-persona` catalog 项与 `@dominion/dsh-demo-echo`/`@dominion/dsh-preset-authoring` workspace 项）→ `bazel run @pnpm -- --dir /mnt/code/dominion/experimental/dsh/demo/agent up` → 目标目录 `bazel run //:gazelle` → `bazel mod tidy`

---

## Phase 2: Foundational（插件包 / 模板数据 / proto / 组合改造——阻塞全部 story）

**文档清单**：
- **代码规范文档**：`style/javascript.md`（Mock 约定：vi.fn() DI seam、禁止模块拦截；vitest_test data 规则）及其引用的外部规范：[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)、[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（Mock 约定依据）；`style/api.md`（T009 proto/HTTP 注解）及其引用的 [AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)、[AIP-122 Resource names](https://google.aip.dev/122)
- **官方文档**：roster 服务面与语义——`node_modules/.pnpm/@deepseek-ai+dsh-agent-presets@0.1.1-rc.2_5200ead8959daeaefdf3dd69ba905368/node_modules/@deepseek-ai/dsh-agent-presets/README.md`（Service/Authoring/Config/包解析各节）；persona 行 Config——[dsh-persona README](https://cdn.jsdelivr.net/npm/@deepseek-ai/dsh-persona@0.1.1-rc.2/README.md)（Config/scope-only/模型可见性）
- **技术文章/技术参考文档**：`specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md`、`specs/058-dsh-preset-roster-demo/contracts/demo-echo-plugin.md`、`specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md`、`specs/058-dsh-preset-roster-demo/contracts/chat-api.md`、`specs/047-dsh-chat-demo/contracts/dsh-agent-service.md`（§4 闭包契约：npm_deps 分层）、`projects/game/agent_v2/cordis.yml`（直组形态参照）、`common/js/dsh-plugins/llm-glm/src/index.ts`（插件四导出形态参照）、`specs/058-dsh-preset-roster-demo/research.md`（R3 patch、R10 接口、R12 直组）

- [ ] T004 [P] demo-echo 插件：`experimental/dsh/demo/agent-plugins/demo-echo/src/index.ts`（`demo_echo` 工具 + guidance section 同 `apply()` 注册，inject `["tools","systemPrompt"]`）+ `src/index.test.ts`（mock registry 断言两者成对注册）
- [ ] T005 [P] preset-authoring Store：`common/js/dsh-plugins/preset-authoring/src/store.ts`（`PresetStore` 接口 + 内存实现 + 稳定错误码 ALREADY_EXISTS/NOT_FOUND）+ `src/store.test.ts`
- [ ] T006 preset-authoring 物化：`common/js/dsh-plugins/preset-authoring/src/materialize.ts`（roster `copy(template, id, displayName)` → js-yaml round-trip patch persona 行 `config.text`（原子写）→ 失败回滚目录；update 的 persona/preset.yml 分流）+ `src/materialize.test.ts`（mock roster/fs seam：patch 正确性、回滚无残留、模板约定违约报错）
- [ ] T007 preset-authoring 服务装配：`common/js/dsh-plugins/preset-authoring/src/index.ts`（四导出 + `ctx.presetAuthoring` 注册：`compose/create/get/list/update/remove`，contracts §2 接口）+ `src/index.test.ts`（compose 返回形状、错误码映射；V3-3：fs seam 写坏副本组合文件 → resolve/list 报 broken、compose fail-fast 不产生半组合会话）
- [ ] T008 [P] 模板 preset 数据：`experimental/dsh/demo/agent/presets-templates/demo-standard/{agent.cordis.yml,preset.yml}` 与 `demo-tools/{agent.cordis.yml,preset.yml}`（contracts/composition-manifest.md §3；遵守模板约定：无注释/无 !!js/唯一 persona 行）
- [ ] T009 [P] proto 扩展：`experimental/dsh/demo/chat.proto` 增 `Chat.CreateConversation` 与 `PresetService` 五 RPC（含 `google.api.http` 注解，contracts/chat-api.md）→ 重生成 `ts_proto_library` 类型与 gateway 产物（`experimental/dsh/demo/gateway/`）
- [ ] T010 [P] 组合改造：`experimental/dsh/demo/agent/cordis.yml` 按 contracts/composition-manifest.md §1 重写（spine → 直组 11 行；roster roots 经 `PRESET_TEMPLATES_ROOT`/`PRESET_WRITABLE_ROOT` env）
- [ ] T011 BUILD 与部署面：`experimental/dsh/demo/agent/BUILD.bazel`（npm_deps 增删 + `data_files` 增 `presets-templates` + vitest data 镜像同步）；`experimental/dsh/demo/testplan/deploy.yaml` 增 emptyDir 卷与两 env 注入；`experimental/dsh/demo/testplan/closure_audit_test.go` expected 集随 package.json 重算

---

## Phase 3: User Story 1 — 显式建会话选择 preset（Priority: P1）🎯 MVP

**Goal**: 会话经 CreateConversation 显式创建并绑定 preset；组合随 preset 生效（default/差异/共享）。

**Independent Test**: 部署后创建绑定 `demo-tools` 与 `demo-standard` 的两个会话各发消息——`system_keywords` 断言 persona 与 `demo_echo` guidance 差异；不传 preset 走 default；未创建会话 SendMessage 报 FAILED_PRECONDITION。

**文档清单**：
- **代码规范文档**：`style/javascript.md` 及其引用的 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；`style/golang.md`（表驱动/given-when-then/命名）；`style/api.md` 及其引用的 [AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)、[AIP-122 Resource names](https://google.aip.dev/122)（CreateConversation 注解与资源名核对）；`style/large_test.md`（T017 大型测试用例：模块维度/helper 复用）
- **官方文档**：`node_modules/.pnpm/@deepseek-ai+dsh-agent@0.1.1-rc.2_c1537a8836b04097f168b024f1e38d85/node_modules/@deepseek-ai/dsh-agent/lib/types/index.d.ts`（`CreateAgentOptions.setup`/`meta.agentPreset`/`AgentSetup` 类型）
- **技术文章/技术参考文档**：`specs/058-dsh-preset-roster-demo/contracts/chat-api.md`（§1）、`specs/058-dsh-preset-roster-demo/contracts/fake-llm-system-keywords.md`、`specs/058-dsh-preset-roster-demo/data-model.md`（§3 Conversation 状态转移）、`specs/047-dsh-chat-demo/contracts/fake-llm-templates.md`（匹配语义母本）、`node_modules/.pnpm/@deepseek-ai+dsh-host-apiproxy@0.1.1-rc.2_7a1c54e2b954eca6f88bad802758761b/node_modules/@deepseek-ai/dsh-host-apiproxy/lib/index.js:1717-1765`（`composeAgent`/`assertPresetUnchanged` 官方接线参照）、`specs/058-dsh-preset-roster-demo/research.md`（R2/R4/R6）

- [ ] T012 [US1] `experimental/dsh/demo/agent/src/session.ts` 显式会话改造：`AgentSessions` 增 conversation 注册表（preset 绑定），`create()` 改为消费 `ctx.get("presetAuthoring").compose(presetId)`（`meta: {cwd, agentPreset}` + `setup`）；同 preset 幂等 / 异 preset dispose 重建（R4）；`send()` 对未创建会话抛 FAILED_PRECONDITION 域错误
- [ ] T013 [US1] `experimental/dsh/demo/agent/src/server.ts` 增 `CreateConversation` handler（校验/错误映射/资源视图）+ `server.test.ts`（域错误 → gRPC status 映射、preset 校验失败透传）
- [ ] T014 [P] [US1] fake-llm 扩展：`experimental/dsh/demo/fake-llm/service/message_types.go`（模板结构体增 `SystemKeywords []string`，yaml `system_keywords`）+ `matcher.go`（`S` 集合计算 + every-hit 并入条件模板判定，未声明零行为变化）+ `matcher_test.go`/`handler_test.go` 表驱动用例
- [ ] T015 [P] [US1] fake-llm testdata：`experimental/dsh/demo/fake-llm/service/testdata/` 新增 preset 场景模板组（contracts/fake-llm-system-keywords.md §3：persona 差异/guidance 在场与缺席）
- [ ] T016 [US1] 集成单测：`experimental/dsh/demo/agent/src/composition.test.ts`——boot 真直组组合（temp roots + 两模板），断言 V1-1 单测面（两 preset 会话 persona/system prompt 差异 + deployment persona 遮蔽，mock LLM 适配或 llm 请求面断言）、V1-3（header `agentPreset` 落对）、V2-1（preset 行工具仅成员可见）、V2-2（同 preset 两会话共享一份注册）
- [ ] T017 [US1] 大型测试会话绑定用例：`experimental/dsh/demo/testplan/preset_test.go`（模块=preset 会话绑定面：V1-1/V1-2/V2-3 端到端、US1-AS3 同 preset 两会话行为一致、US1-AS4 会话级稳定性（后续消息组合保持绑定不变）、幂等/重建、FAILED_PRECONDITION；遵守 `style/large_test.md` 模块维度与 helper 复用 `helpers_test.go`）+ `interface_test.yaml` 挂载 case

**Checkpoint**: MVP 完成——US1 全部验收场景可独立演示（quickstart §3 场景 1-3、7-8）

---

## Phase 4: User Story 2 — preset CRUD 闭环（Priority: P2）

**Goal**: 经 API 创作/更新/删除 preset；generation 切换（旧会话稳、新会话新）；删除后旧会话存活。

**Independent Test**: CreatePreset → 新会话生效 → UpdatePreset persona → 原会话不变/新会话新值 → Delete → 原会话可对话/新会话被拒（quickstart §3 场景 4-6）。

**文档清单**：
- **代码规范文档**：`style/javascript.md` 及其引用的 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；`style/golang.md`、`style/large_test.md`（T019 大型测试 Go 用例）；`style/api.md` 及其引用的 [AIP-133 Standard methods: Create](https://google.aip.dev/133)、[AIP-134 Standard methods: Update](https://google.aip.dev/134)、[AIP-135 Standard methods: Delete](https://google.aip.dev/135)、[AIP-193 Errors](https://google.aip.dev/193)
- **官方文档**：`node_modules/.pnpm/@deepseek-ai+dsh-agent-presets@0.1.1-rc.2_5200ead8959daeaefdf3dd69ba905368/node_modules/@deepseek-ai/dsh-agent-presets/README.md`（`copy`/`remove`/发现热读取/stamp generation 各节——handler 语义透传依据）
- **技术文章/技术参考文档**：`specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md`（§4 物化算法/§6 错误码）、`specs/058-dsh-preset-roster-demo/contracts/chat-api.md`（§2 PresetService）、`specs/058-dsh-preset-roster-demo/data-model.md`（§2 AuthoredPreset 状态转移）、`projects/game/agent_v2/src/presets.ts`（Store 错误码与 keyset 先例参照）、`specs/058-dsh-preset-roster-demo/research.md`（R3/R5）

- [ ] T018 [US2] `experimental/dsh/demo/agent/src/server.ts` 增 `PresetService` 五 handlers（校验：id 语法/template 可 resolve/persona 非空/update_mask 限定；错误映射 contracts §6）+ `server.test.ts`（handler 面单测：域错误 → status、字段校验、模板拒绝删除透传）
- [ ] T019 [US2] 大型测试 CRUD/generation 用例：`experimental/dsh/demo/testplan/preset_test.go` 追加（模块=preset 资源面：V3-1 热创作、V3-2 generation 切换、V4-1 闭环、V4-3 删除语义、US2-AS4 重复 id→ALREADY_EXISTS / 未知模板→INVALID_ARGUMENT / 拒绝且无半物化残留；V3-3 broken 场景经 preset 创建后损坏文件的测试步骤承载）

**Checkpoint**: US1 + US2 均独立可用；C1 闭环全链路（store↔文件↔roster）经端到端验证

---

## Phase 5: User Story 3 — 边界审计（Priority: P3）

**Goal**: service 层零 roster/fs 引用、插件零 RPC 概念——架构边界可审计。

**Independent Test**: 代码审计（checklist 项）：`experimental/dsh/demo/agent/src/server.ts`、`session.ts` 无 `agentPresets`/`node:fs` 引用；`common/js/dsh-plugins/preset-authoring/src/*` 无 proto/gRPC 概念。

**文档清单**：
- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/058-dsh-preset-roster-demo/contracts/preset-authoring-plugin.md`（§2 边界声明）

- [ ] T020 [US3] 执行边界审计并在 `specs/058-dsh-preset-roster-demo/checklists/` 记录结论：grep 审计 service 层（`rg "agentPresets|node:fs" experimental/dsh/demo/agent/src/`）与插件层（RPC/proto 概念）；发现越界则回改对应文件后复审

**Checkpoint**: V4-2 通过——边界交付物完成（对 agent_v2 迁移的模板价值）

---

## Phase 6: Polish & 验收（跨 story 收尾）

**文档清单**：
- **代码规范文档**：`style/large_test.md`（测试计划/用例组织/反模式）、`style/golang.md`（large_test 引用的 Go 单测规范——间接引用显式列出）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/058-dsh-preset-roster-demo/quickstart.md`（§2/§3 验证命令与场景）、`experimental/dsh/demo/README.md`（现状描述，更新对象）、`specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md`（§1/§2 组合清单与 roots/env——T021 描述对象）、`specs/058-dsh-preset-roster-demo/contracts/chat-api.md`（§1/§2 API 面——T021 描述对象）、`specs/058-dsh-preset-roster-demo/discussion-2026-09-08.md`（§9 survey 写作指引）、`specs/058-dsh-preset-roster-demo/research.md`（R1-R12 实践对照——T023 survey 底稿）、`survey/deepseek-harness-team-mode.md`（§2.3/§3——survey 实证对照对象）

- [ ] T021 [P] 更新 `experimental/dsh/demo/README.md`：组合清单（直组+roster+authoring 行）、PresetService/CreateConversation 面、roots/emptyDir/env 说明、"preset 资源与物化文件为进程态、重启丢失"已知限制（终态表述，原则 VII）
- [ ] T022 大型测试验收闭环（Constitution 原则 VI）：经 testplan skill 执行 `guitar run experimental/dsh/demo/testplan/interface_test.yaml`——部署→全部用例（047 既有回归 + preset 新用例）→清理；**全部通过**（零 failed/flaky）为准，失败则修复后重跑；同步 `bazel test //experimental/dsh/demo/testplan:closure_audit_test`
- [ ] T023 survey 落档（FR-010）：按 `discussion-2026-09-08.md` §9 写入 `survey/deepseek-harness-roster-verification.md`（roster 机制实证对照、C1 实践摩擦、边界最佳实践与 agent_v2 迁移建议；被实践修订的纸面结论对照记录）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1**：无依赖，立即开始（T001 ∥ T002 → T003）
- **Phase 2**：依赖 Phase 1（T004 ∥ T005 → T006 → T007；T008 ∥ T009 ∥ T010 ∥ T011 依赖 T004/T007 完成）
- **Phase 3 (US1)**：依赖 Phase 2 完成（T012 → T013；T014 ∥ T015 → T016 → T017）
- **Phase 4 (US2)**：依赖 Phase 2 完成、与 US1 的独立文件任务可并行；**T018 编辑 `server.ts`/`server.test.ts`，与 US1 的 T013 同文件，须在 T013 合入后串行**；T019 依赖 T017（同文件 preset_test.go 追加）
- **Phase 5 (US3)**：依赖 US1+US2 代码定形（审计对象）
- **Phase 6**：依赖全部 story 完成；T021 ∥ T022 前置 → T023

### Parallel Opportunities

```text
Phase 1:  T001 ∥ T002
Phase 2:  T004 ∥ T005；T008 ∥ T009 ∥ T010 ∥ T011
Phase 3:  T014 ∥ T015
多人力:   US1 与 US2 在 Phase 2 后可由两人并行（US2 的 T018 编辑 server.ts 与 US1 的 T013 同文件、需等其合入；T019 需等 T017 合入 preset_test.go）
```

---

## Implementation Strategy

### MVP First（US1 only）

1. Phase 1 → Phase 2 → Phase 3（US1）
2. **STOP and VALIDATE**：quickstart §3 场景 1-3/7-8 + `bazel test //experimental/dsh/demo/... //common/js/dsh-plugins/...`
3. MVP 即演示"一进程多组合会话"的 roster 核心实证

### Incremental Delivery

1. US1（MVP）→ US2（C1 闭环 + generation）→ US3（边界审计）→ Phase 6（README/大型测试全绿/survey）
2. 每个Checkpoint 独立可验证；047 既有用例始终作为回归门禁

---

## Notes

- 每个实现任务自带 `bazel build` + `bazel test`（相关 target）——Constitution 原则 IV，不单列任务
- 大型测试执行（T022）是唯一验收任务——原则 VI：`guitar run` 完整闭环 + 全部用例通过，构建检查不替代
- 同文件任务串行（T006→T007、T012→T013、T017→T019）；`preset_test.go` 追加时遵守模块维度组织（`style/large_test.md` 反模式 1/2）
- 单测 mock 一律 DI/vi.fn() seam（`style/javascript.md` Mock 约定）；集成单测（T016）用真组合 + temp 目录，不访问外部依赖
