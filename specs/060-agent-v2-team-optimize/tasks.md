# Tasks: Agent v2 team 模式优化（部署配置收敛 / 常量库 / 实时流修复 / 广播净化 / 提示词分层）

**Input**: Design documents from `/specs/060-agent-v2-team-optimize/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: 编译与单测是每个代码任务的一部分（constitution 原则 IV——不单独分配 task，各任务描述含对应单测更新）；大型测试作为验收在最终 phase 单独执行（原则 VI）。

**Organization**: 按 user story 分 phase（spec US1–US7），常量库为多 story 共享基础置于 Foundational。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story（US1–US7）
- 每任务含精确文件路径

## Path Conventions

- Go：`common/gopkg/`、`projects/infra/deploy/`、`tools/release/deploy/`
- TS 插件/服务：`common/js/`、`projects/game/agent_v2/`
- 前端：`projects/game/web/frontend/src/`
- proto/部署/测试计划：`projects/game/`

---

## Phase 1: Foundational（常量库——多 story 共享基础）

**Purpose**: 仓库通用常量库两语言包（spec US3 的库本体前置：US1 的 builder 注入与 agent_v2 消费都依赖它）。

**文档清单**（编码前必读；constitution 原则 V 三分类）：

- **代码规范文档**：`style/golang.md`；[Google Go Style](https://google.github.io/styleguide/go/)（`style/golang.md` 引用入口；其中 [Style Guide](https://google.github.io/styleguide/go/guide) 为规范+权威必读）；`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用基准）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/060-agent-v2-team-optimize/contracts/const-lib.md`；`specs/060-agent-v2-team-optimize/research.md`（R4）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（`style/javascript.md` 引用的包级契约——新建 JS 包必读）

- [X] T001 [P] 新建 Go 常量包 `common/gopkg/constants`：`constants.go` 导出 12 个平台保留环境变量名常量（含新增 `DOMINION_ARTIFACT_DIR`，见 `specs/060-agent-v2-team-optimize/contracts/const-lib.md` §1 表）+ doc 注释写明收录原则；`constants_test.go` 断言常量值；`bazel run //:gazelle common/gopkg/constants` 生成 BUILD.bazel
- [X] T002 [P] 新建 JS 常量包 `common/js/constants`（包名 `@dominion/common-js-constants`）：`src/index.ts` 导出与 Go 侧一一对齐的 12 个常量 + `src/index.test.ts` 对齐断言；按 `specs/048-js-esm-migration/contracts/esm-package-conventions.md` 配置 `package.json`（`"type": "module"`）/`tsconfig.json`；`pnpm-workspace.yaml` 注册包并经 catalog 管理依赖版本；`bazel run //:gazelle common/js/constants` 生成 BUILD.bazel
- [X] T003 常量库工作区接线验证：`bazel mod tidy` + 两包 `bazel test` 绿（新包 workspace 接线收尾，非功能单测）

**Checkpoint**: 常量库就绪，US1（builder/agent_v2 消费）与 US3（采用清扫）可引用。

---

## Phase 2: User Story 1 - 部署配置精简与产物位置保留环境变量 (Priority: P1)

**Goal**: deploy 平台注入 `DOMINION_ARTIFACT_DIR`；agent_v2 模板根改由其派生；部署清单移除 `PRESET_TEMPLATES_ROOT`（`PRESET_WRITABLE_ROOT` 的清单移除归 US2——两 env 分属两 story 的独立收敛，终态见 quickstart V1）。

**Independent Test**: `bazel test`（builder/env 断言、dsh.ts 解析分支）绿；部署拓扑 boot 后 roster 模板解析成功（大型验证在 Phase 9）。

**文档清单**：

- **代码规范文档**：`style/golang.md`；[Google Go Style](https://google.github.io/styleguide/go/)（`style/golang.md` 引用入口；其中 [Style Guide](https://google.github.io/styleguide/go/guide) 为规范+权威必读）；`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用基准）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/060-agent-v2-team-optimize/contracts/deploy-env.md`；`specs/060-agent-v2-team-optimize/contracts/const-lib.md`；`specs/060-agent-v2-team-optimize/research.md`（R1/R3/R11）

- [X] T004 [US1] `projects/infra/deploy/runtime/k8s/builder.go`：stateful 与 stateless 两处保留变量块追加注入 `DOMINION_ARTIFACT_DIR`（值 = `/dominion/{app}/{service}` 产物目录，由 workload 的 app/service 构造；变量名引用 `common/gopkg/constants` 常量；平台值追加在用户 env 之后 last-wins，对齐 `DOMINION_SECRET_DIR` 语义）；`projects/infra/deploy/runtime/k8s/executor.go` 的 `ReservedEnvironmentVariableNames` 清单追加 `DOMINION_ARTIFACT_DIR`（FR-001 保留名校验面）；`projects/infra/deploy/runtime/k8s/builder_test.go` 更新 env 计数/顺序断言并新增用户同名覆盖用例（对齐既有 DOMINION_SECRET_DIR 覆盖用例形态，L1906-1955 同型）、`executor_test.go` 的 `want` 清单同步
- [X] T005 [P] [US1] `tools/release/deploy/README.md`：保留变量清单（§服务环境变量）补入 `DOMINION_ARTIFACT_DIR` + 用途说明（声明产物放置目录；用户 env 不可覆盖）
- [X] T006 [P] [US1] `projects/game/agent_v2/src/dsh.ts`：新增 preset 模板根解析——`PRESET_TEMPLATES_ROOT` 显式覆盖 > `${DOMINION_ARTIFACT_DIR}/preset-templates` 派生 > 皆缺 boot fail-loud（错误信息含两个变量名；boot 前写入组合 env 的既有宿主注入模式）；变量名经 `@dominion/common-js-constants` 引用；`projects/game/agent_v2/src/dsh.test.ts` 新增三分支用例（T004 先行：派生链依赖其注入语义）
- [X] T007 [US1] 移除 `PRESET_TEMPLATES_ROOT` 声明：`projects/game/deploy.yaml`（agent-v2 env 块）与 `projects/game/testplan/deploy_agent_v2.yaml`、`deploy_agent_v2_drop.yaml`、`deploy_agent_v2_memory_down.yaml`（`PRESET_WRITABLE_ROOT` 行保留至 US2 的 T012 原子移除；本任务后 boot 依赖 T004+T006 派生链）；`rg` 断言生产与 testplan 部署清单中 `PRESET_TEMPLATES_ROOT` 零残留（SC-001）
- [X] T008 [US1] `projects/game/agent_v2/README.md`：模型端点/凭据章节之外补产物根派生说明（`DOMINION_ARTIFACT_DIR` 派生 + 显式覆盖 + fail-loud；终态表述，constitution 原则 VII）

**Checkpoint**: 部署清单不再声明模板根；boot 经平台变量派生（`PRESET_WRITABLE_ROOT` 仍在位，US2 收敛）。

---

## Phase 3: User Story 2 - preset 唯一事实源化（使用时派生，不维护文件） (Priority: P1)

**Goal**: 用户 preset CRUD 只写 Mongo store；物化组合使用时派生为纯临时文件经官方 `mountPreset` 挂载；`PRESET_WRITABLE_ROOT` 语义消亡。

**Independent Test**: `bazel test`（preset-authoring 全套单测重写后绿：derive 幂等、compose 派生链、CRUD store-only）；代码检索断言 copy-then-patch 维护路径移除；大型验证在 Phase 9。

**文档清单**：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：
  - [dsh-agent-presets README（0.1.1-rc.2）](https://unpkg.com/@deepseek-ai/dsh-agent-presets@0.1.1-rc.2/README.md)（roster/preset 根与配置面）
  - [dsh-agent-presets `mountPreset` 类型声明（0.1.1-rc.2）](https://unpkg.com/@deepseek-ai/dsh-agent-presets@0.1.1-rc.2/lib/types/mount.d.ts)（根导出 `mountPreset` 与挂载保障——README 未覆盖，T010 消费面）
  - [dsh-agent-presets `AgentPreset` 类型声明（0.1.1-rc.2）](https://unpkg.com/@deepseek-ai/dsh-agent-presets@0.1.1-rc.2/lib/types/preset.d.ts)（合成 `AgentPreset` 对象字段）
- **技术文章/技术参考文档**：`specs/060-agent-v2-team-optimize/contracts/preset-derivation.md`；`specs/060-agent-v2-team-optimize/data-model.md`（§3）；`specs/060-agent-v2-team-optimize/research.md`（R2）；`specs/059-agent-v2-team-mode/contracts/preset-api.md`（基线契约：RPC 面/role 语义与「不变的面」§1/§3；本 feature 修订其 §2 copy-then-patch 条款）

- [X] T009 [US2] 新增 `common/js/dsh-plugins/preset-authoring/src/derive.ts`：`deriveComposition(store 记录, 模板组合)` → 组合行列表（persona 行 `config.text` ← 记录 persona；空 persona 保留模板原文=默认 base）→ `mkdtemp(os.tmpdir())` 写 `agent.cordis.yml` → 返回合成 `AgentPreset {id, trust: 'user', path}`；幂等纯函数；新增 `derive.test.ts`（persona 替换/空值回退/幂等重建/临时文件内容）
- [X] T010 [US2] `common/js/dsh-plugins/preset-authoring/src/index.ts`：`compose(presetId)` 改为 `store.get` → `deriveComposition` → setup 经根导出的 `mountPreset(agentCtx, preset)` 挂载（import 自 `@deepseek-ai/dsh-agent-presets`；fail-fast 语义与 `agentPreset` meta 不变）；`index.test.ts` 更新（compose 不再 resolve/copy 用户 preset；NOT_FOUND/幂等用例）
- [X] T011 [US2] `common/js/dsh-plugins/preset-authoring/src/index.ts` + `src/materialize.ts`：create/update/remove 改 store-only（create 保留 `templateRules` 模板行校验——`validateTemplateRows`/`PERSONA_ROW_NAME` 迁入 derive 模块或保留原位）；移除 copy-then-patch 维护路径（`materializeCopy`/`updateMaterialization`/`removeMaterialization`/`rebuildCopy` 及 roster copy/remove 调用）；`materialize.test.ts` 用例迁移/删除、`index.test.ts`/`store.test.ts` 对齐
- [X] T012 [US2] （与 T011 同批原子交付）`projects/game/agent_v2/cordis.yml` 删除 user root 行（`PRESET_WRITABLE_ROOT` 表达式条目）；`projects/game/deploy.yaml` 与三份 `projects/game/testplan/deploy_agent_v2*.yaml` 移除 `PRESET_WRITABLE_ROOT`（agent-v2 env 块至此仅剩 secret 绑定）；`rg` 断言全部部署清单中 `PRESET_TEMPLATES_ROOT`/`PRESET_WRITABLE_ROOT` 零残留（SC-001）
- [X] T013 [P] [US2] `projects/game/agent_v2/README.md`：preset 持久化章节终态化（store 唯一事实源 + 使用时派生挂载；「已知限制」中可写副本重建语义条目按派生语义改写）
- [X] T035 [US2] （执行期用户指令 2026-09-11：按当前设计同步修改 dsh/demo）experimental/dsh/demo 迁移至 060 preset 派生语义：单测改经 store 记录 compose（模板 id 不再直连）、V2-2 共享挂载断言改 per-agent 派生挂载、default/无 preset 语义对齐 preset 必选（INVALID_ARGUMENT）、cordis.yml 删 user root、testplan deploy.yaml 移除 PRESET_WRITABLE_ROOT、场景断言同步

**Checkpoint**: preset 全生命周期仅依赖 store；磁盘副本缺席不影响任何行为。

---

## Phase 4: User Story 4 - webUI 实时流修复 🎯 (Priority: P1)

**Goal**: 根因修复两个实时缺陷（根因链见 research.md R5/R6，修复与根因一一对应、最小边界）：(a) 成员消费输入时实时呈现（新增 `member_view` 帧）；(b) 工具结果三投影面即时终态化。**回填收敛/并发流去重/断开投影零改动**（用户指令的修复边界）。

**Independent Test**: `bazel test`（session/history/chat store 单测绿，含新增 059 真实帧序用例）；大型验证在 Phase 9（member_view 帧断言）。

**文档清单**：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；`style/api.md`；[AIP-140 Field names](https://google.aip.dev/140)、[AIP-203 Field behavior documentation](https://google.aip.dev/203)（proto 帧字段命名与行为文档——`style/api.md` 引用）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/060-agent-v2-team-optimize/contracts/team-api.md`（§2/§3）；`specs/060-agent-v2-team-optimize/research.md`（R5/R6）；`specs/060-agent-v2-team-optimize/data-model.md`（§1/§4）；`specs/059-agent-v2-team-mode/contracts/team-api.md`（基线契约）；`specs/059-agent-v2-team-mode/contracts/web-views.md`（前端行为基线）

- [X] T014 [P] [US4] `projects/game/agent_v2.proto`：`ChatEvent` oneof 新增 `member_view` 帧（`MemberViewEvent {member, sender, message}`——字段语义见 `specs/060-agent-v2-team-optimize/contracts/team-api.md` §2）；bazel 生成代码刷新（既有 proto 生成 target）
- [X] T015 [US4] `projects/game/agent_v2/src/history.ts`：`appendMemberViewUser` 写入成员视角的同时扇出 `member_view` 帧（`{member, sender, message}`；sender=user 输入或广播发送 role）；`projects/game/agent_v2/src/history.test.ts` 新增消费帧用例（用户输入消费 + 广播注入消费两类；与 ListMemberMessages 投影同构）
- [X] T016 [P] [US4] `projects/game/web/frontend/src/store/chat.ts`：`toolResult` 归约改为**一次归约内跨三投影面幂等 settle**（live 草稿 `settleDraft` + 归并序列 `settleHistoryEntry` + 成员视角 `settleMemberHistory`；删除 live 命中 early-return）；`projects/game/web/frontend/src/store/chat.test.ts`：新增 059 真实帧序用例（`blockStart` 无 toolId → `blockEnd` 带 toolId → `team_message` 固化 → `tool_result` 断言三面终态；重复 `tool_result` 帧幂等）并修正既有 L301 用例的假帧形（blockStart 去掉 toolId、补 blockEnd/teamMessage）
- [X] T017 [US4] `projects/game/web/frontend/src/store/chat.ts` + `src/api/conversation.ts`：`member_view` 帧归约（`memberHistory[member]` 追加 `{message, sender}`；messageId 幂等；不触碰归并序列/live/queue）+ ChatEvent 类型；`chat.test.ts` 归约用例（追加/幂等/广播注入 sender 标注）
- [X] T018 [US4] 回归确认：`projects/game/web/frontend/src/store/chat.test.ts` 既有断开投影/并发流去重/回填用例零回归（本 story 不改动这些路径；若有用例失败即为修复越界，回退对齐 R6 修复边界）

**Checkpoint**: 发送消息后成员视角实时出现该输入；工具完成后状态/结果即时更新（无需刷新）。

---

## Phase 5: User Story 3 - 仓库通用常量库采用清扫 (Priority: P2)

**Goal**: deploy 工具与服务、`projects/game/` 使用方的保留变量字面量切换到常量库（库本体已在 Phase 1 交付；本 phase 完成 FR-005 采用面）。

**Independent Test**: `bazel test`（builder 断言零回归）绿；范围内零裸字面量（检索断言）。

**文档清单**：

- **代码规范文档**：`style/golang.md`；[Google Go Style](https://google.github.io/styleguide/go/)（`style/golang.md` 引用入口；[Style Guide](https://google.github.io/styleguide/go/guide) 规范+权威必读）；`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用基准）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/060-agent-v2-team-optimize/contracts/const-lib.md`；`specs/060-agent-v2-team-optimize/research.md`（R4）

- [X] T019 [P] [US3] `projects/infra/deploy/runtime/k8s/builder.go`：保留变量常量块（L23-70）中 12 个保留变量名（含新增 `DOMINION_ARTIFACT_DIR`）整体替换为 `common/gopkg/constants` 引用（删除本地重复定义；`envLogLevel`/`LOG_LEVEL` 与 volume/path/probe 等非保留常量保留原位；注入行为与 `builder_test.go` 断言零回归）
- [X] T020 [P] [US3] `projects/game/agent_v2`：既有保留变量字面量切换——`src/presets.ts` 的 `DOMINION_ENVIRONMENT`、`src/dsh.ts` 的 `DOMINION_SECRET_DIR` 改经 `@dominion/common-js-constants` 引用（行为零变更）
- [X] T021 [US3] 采用范围检索断言：`rg` 确认 `projects/infra/deploy/runtime/k8s/` 与 `projects/game/agent_v2/src/` 内 12 个保留变量名零裸字面量（注释/文档字符串除外；common 既有包不在范围——Clarifications 2026-09-11 裁定）；YAML 部署清单（如 `projects/infra/deploy/k8s.yaml`、`projects/game/deploy.yaml`）无法引用语言常量、不在本范围

**Checkpoint**: 常量单一事实源在采用范围内成立。

---

## Phase 6: User Story 5 - team 状态呈现（激活成员 + system prompt 主界面入口） (Priority: P2)

**Goal**: GetTeam 暴露单一"当前激活成员"值；web 对话页实时呈现；system prompt 入口提升到主界面。

**Independent Test**: `bazel test`（orchestrator/session/App 组件测试绿）；大型验证在 Phase 9。

**文档清单**：

- **代码规范文档**：`style/api.md`；[AIP-129 Server-Modified Values and Defaults](https://google.aip.dev/129)（output-only 字段规范）；`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/060-agent-v2-team-optimize/contracts/team-api.md`（§1/§5）；`specs/060-agent-v2-team-optimize/research.md`（R7/R10）；`specs/059-agent-v2-team-mode/contracts/web-views.md`

- [X] T022 [P] [US5] `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`：`OrchestratorSnapshot` 外露 `activation`（下一条输入归属成员，即 `current` 字段）；`orchestrator.test.ts` 快照断言更新（物化后 planner / 切换流转 / 取消保持）
- [X] T023 [P] [US5] `projects/game/agent_v2.proto`（`Team` 新增 output-only `active_member`）+ `projects/game/agent_v2/src/session.ts`（`TeamView.activeMember = driving?.role ?? activation`，`toTeamView` 投影）+ `projects/game/agent_v2/src/server.ts`（`teamViewToProto` 映射）+ `session.test.ts` 用例
- [X] T024 [US5] `projects/game/web/frontend/src/api/agent.ts`（Team.activeMember 类型）+ `src/App.tsx`：对话页工具条激活成员徽标——实时推导（任一成员 `turn_start` 帧覆盖最近 GetTeam 值；live 收束回退 GetTeam 值）+ `App.test.tsx` 用例（徽标渲染/推导切换）
- [X] T025 [US5] `projects/game/web/frontend/src/App.tsx` + `src/components/ChatView.tsx`：主界面 system prompt 入口——team 工具条成员清单区每成员可点击，经既有 `getTeamMember` 读取并以只读浮层展示全文（复用 `src/components/TeamSettingsPanel.tsx` 的浮层逻辑；设置面板内入口保留）+ 组件测试用例

**Checkpoint**: 对话页实时可见当前激活成员；主界面直接查看成员 system prompt。

---

## Phase 7: User Story 6 - 广播消息净化（think 移除 + wire 格式去重） (Priority: P2)

**Goal**: 广播内容不含 think；单一 XML 标注形态（头行摘要废止）；team section 同步；夹具/断言联动。

**Independent Test**: `bazel test`（broadcast/section 单测绿）；大型回归在 Phase 9。

**文档清单**：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/060-agent-v2-team-optimize/contracts/team-api.md`（§4）；`specs/060-agent-v2-team-optimize/data-model.md`（§7）；`specs/060-agent-v2-team-optimize/research.md`（R8）；`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`（§1 广播契约基线——本 phase 修订其格式条款）

- [X] T026 [P] [US6] `common/js/dsh-plugins/team/src/broadcast.ts`：`messageBody`/`toolResultBody` 移除 reasoning 分支（仅 text 原样 + 其余块无损 JSON）；`renderBroadcast` 改单一 XML 形态（删头行；工具单元 `context:` 移入包裹体可选行——终态格式见 data-model.md §7）；移除 `summarize`/`boundContextSummary` 引用；`broadcast.test.ts` 终态格式用例（think 缺席/正文唯一/全文保持/context 行）
- [X] T027 [P] [US6] `common/js/dsh-plugins/team/src/section.ts`：广播格式约定按终态格式改写（单一标签对形态；工具单元 context 行说明）+ `team.test.ts`/`section` 相关用例更新
- [X] T028 [US6] 夹具与断言联动：`projects/game/fake-llm/service/testdata/team_planner.yaml`、`team_player.yaml` 锚点核对（`<player-message>`/`<player-tool-call>` 标签锚不变，头行相关关键词如有则清理）；`projects/game/testplan/agent_v2_conversation_test.go` 等处广播文本断言按新格式更新；成员视角 relay 呈现断言（`user: [sender]` 前缀 + 注入原文，正文仅出现一次、不剥离 XML 标签）

**Checkpoint**: 广播注入文本无 think、无重复标注；成员视角 relay 呈现同步净化。

---

## Phase 8: User Story 7 - 提示词分层（玩法进 saolei-loop / 工具守则仅用法 / persona 去重） (Priority: P2)

**Goal**: `saolei:game` section（全员）承载玩法+操作；`saolei:guidance` 仅剩工具用法；persona 去重；夹具关键词同步。

**Independent Test**: `bazel test`（插件单测绿：section 注册/文本、guidance 无玩法关键词）；大型验证在 Phase 9。

**文档清单**：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[dsh-system-prompt 类型声明（0.1.1-rc.2）](https://unpkg.com/@deepseek-ai/dsh-system-prompt@0.1.1-rc.2/lib/types/index.d.ts)（`SystemPrompt.section()`/PromptSection——`saolei:game` section 注册面）、[同包 README（0.1.1-rc.2）](https://unpkg.com/@deepseek-ai/dsh-system-prompt@0.1.1-rc.2/README.md)
- **技术文章/技术参考文档**：`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md`；`specs/060-agent-v2-team-optimize/research.md`（R9）；[Microsoft Minesweeper — Wikipedia](https://en.wikipedia.org/wiki/Microsoft_Minesweeper)；[Minesweeper (video game) — Wikipedia](https://en.wikipedia.org/wiki/Minesweeper_(video_game))（玩法 section 内容依据）

- [X] T029 [US7] `common/js/dsh-plugins/saolei-loop/src/index.ts`：`apply(ctx)` 注册 `saolei:game` section（`ctx.systemPrompt.section({name: 'saolei:game', order: 50, text: SAOLEI_GAME_RULES})`；`SAOLEI_GAME_RULES` 导出——内容按 `specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §1：权威玩法 + 与三工具交集的操作，不含工具调用形态细节）+ 注册/文本单测
- [X] T030 [US7] `common/js/dsh-plugins/saolei/src/index.ts`：`SAOLEI_GUIDANCE` 收缩为纯工具用法（保留符号表/坐标标尺/结果三层结构/校验拒绝语义/示例流/纪律；移除玩法陈述——切分表见 prompt-sections.md §2）+ `index.test.ts`（无玩法关键词断言 + 保留项断言）
- [X] T031 [US7] `projects/game/agent_v2/preset-templates/player/player/agent.cordis.yml`：player persona 瘦身（移除操作清单描述，保留身份/职责/风格——prompt-sections.md §3）；`projects/game/fake-llm/service/testdata/team_player.yaml`/`team_planner.yaml` 的 `system_keywords` 同步（被移文案锚点调整；planner 可加 `saolei:game` 玩法关键词锚点）

**Checkpoint**: planner 获得玩法输入；提示词三层所有权单一。

---

## Phase 9: Polish & 大型测试验收

**Purpose**: 跨 story 收口与全量验收（constitution 原则 VI：实际执行 testplan 部署→测试→清理闭环，全部用例通过）。

**文档清单**：

- **代码规范文档**：`style/large_test.md`；`style/golang.md`（`style/large_test.md` 明文引用：大型测试代码必须遵守其单元测试规范——T032 Go 测试用例编写）；[Google Go Style](https://google.github.io/styleguide/go/)（`style/golang.md` 引用入口）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/060-agent-v2-team-optimize/quickstart.md`（V1–V7 验证场景）；`specs/060-agent-v2-team-optimize/research.md`（R12）

- [X] T032 `projects/game/testplan/`：新增/更新断言——`agent_v2_conversation_test.go`（member_view 帧到达断言：planner 首驱消费用户输入即达；工具 tool_result 帧后即时终态的 wire 序断言）、`agent_v2_game_test.go`（GetTeam `active_member` 阶段流转断言）、`agent_v2_preset_test.go`（CRUD→物化派生链路：无副本路径断言）；`projects/game/testplan/saolei_fixtures_test.go` 夹具一致性
- [X] T033 `projects/game/agent_v2/README.md` 全面终态化：env 推导/preset 派生/实时性（member_view、三面 settle）/激活成员/广播新格式/提示词分层——只表述终态（constitution 原则 VII），移除被取代机制的描述
- [X] T034 大型测试验收：加载 testplan skill 执行 `guitar run projects/game/testplan/system_test.yaml`（三 suite：game-system / game-disconnect / game-memory-down）——完整部署→测试→清理闭环，**全部用例通过**（任何 failed/flaky 修复后重跑直至全绿；仅构建通过不构成验收）；T035 联动：执行 `guitar run experimental/dsh/demo/testplan/interface_test.yaml`（demo 迁移后 wire 面验收，review 2026-09-11 补入，同样全量通过）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Foundational）**: 无前置，立即开始；**阻塞** US1/US3（常量库消费方）
- **Phase 2（US1）**: 依赖 Phase 1（builder/agent_v2 引用常量库）
- **Phase 3（US2）**: 依赖 T006/T007 已合入（cordis.yml/env 收敛在 US1 之后的同一文件面上顺序进行；T012 与 T011 原子）
- **Phase 4（US4）**: **无跨 story 依赖**（文件面独立：proto/history/chat.ts）——可与 Phase 2/3 并行（多开发者场景）或按序执行
- **Phase 5（US3）**: 依赖 Phase 1 + T004（builder 注入先行、常量块替换随后；同文件顺序执行）
- **Phase 6（US5）**: proto 变更依赖 T014 之后合入（同文件 `agent_v2.proto` 顺序编辑）；其余无依赖
- **Phase 7（US6）**: 无跨 story 依赖（建议在 US4 后：同为 team 流/夹具相关文件的顺序编辑）
- **Phase 8（US7）**: 夹具文件与 US6 同文件（`team_*.yaml`）——顺序执行
- **Phase 9（Polish/验收）**: 依赖全部 story 完成

### User Story Dependencies

- US1（P1）→ US2（P1）：部署清单/env 与 cordis.yml 收敛链上顺序执行（T007 → T012 分属两 story 的原子边界）
- US4（P1）：独立（不同文件面），可先行或并行
- US3（P2）：依赖 Phase 1；builder 侧在 US1 之后
- US5/US6/US7（P2）：相互独立；US6→US7 夹具文件顺序；US5 的 proto 任务在 US4 之后

### Within Each User Story

- 单测随实现任务同批完成（constitution 原则 IV：每次变更 `bazel build` + `bazel test` 相关 target）
- 服务/协议任务先于依赖其产物的消费任务（T014→T015 帧生成、T014→T017 帧词汇对齐、T023→T024 类型对齐；T016 无协议依赖）
- 同文件任务串行（chat.ts 的 T016→T017；App.tsx 的 T024→T025；builder.go 的 T004→T019）

### Parallel Opportunities

- Phase 1：T001 ∥ T002（不同语言包目录）
- Phase 2：T005 ∥ T006（README 与 dsh.ts 不同文件）；T004 先行（T006 派生链引用其语义）
- Phase 3：T009 →（T010 → T011 → T012 串行：index.ts/cordis.yml 依赖链）；T013（README）独立可并行
- Phase 4：T014 → T015（服务端）；T016 → T017（chat.ts 串行）；服务端与前端两线可并行（T014/T015 ∥ T016/T017）
- Phase 5：T019 ∥ T020（不同包/文件）→ T021（依赖两者）
- Phase 6：T022 ∥ T023（不同包）→ T024 → T025（App.tsx 同文件串行）；T022/T023 线与 T024/T025 线无依赖，可并行
- Phase 7：T026 ∥ T027（broadcast.ts 与 section.ts 不同文件）→ T028（夹具与断言随格式收敛）
- 多开发者：US4 线 ∥（US1→US2 线）∥（US5/US6/US7 线）

---

## Parallel Example: User Story 4

```bash
# 服务端线与前端线并行（不同文件面）：
Task: T014 "agent_v2.proto member_view 帧" → T015 "history.ts 消费帧扇出"
Task: T016 "chat.ts toolResult 三投影面 settle" → T017 "chat.ts member_view 归约"
# 两线合流后执行 T018 回归确认
```

---

## Implementation Strategy

### MVP First（US4 优先切片）

1. Phase 1（Foundational 常量库）
2. Phase 4（US4 实时修复——用户最痛、文件面独立、可立即交付价值）🎯
3. STOP and VALIDATE：单测 + 手工验证实时性（quickstart V4 场景 1/2）
4. 其余 P1（US1 → US2）→ P2（US3 → US5 → US6 → US7）逐 story 增量交付

### Incremental Delivery

1. 常量库（Phase 1）→ US1 → US2：部署与 preset 基建收敛（quickstart V1–V3 可验）
2. US4：实时正确性（V4）
3. US3 → US5 → US6 → US7：常量采用、可观测性、广播质量、提示词（V3/V7/V6/V5）
4. Phase 9：大型测试全量验收（guitar run 三 suite 全绿 = 验收通过）

---

## Notes

- [P] = 不同文件且无未完成依赖；同文件任务一律串行（编辑冲突）
- 编译+单测是每个代码任务的一部分（不单列 task）；大型测试仅 Phase 9 验收任务
- US4 修复边界纪律（执行期用户指令）：只做 research.md R5/R6 的最小变更；T018 是越界哨兵（既有收敛用例失败 = 修复越界）
- 提交粒度：每任务或逻辑组一 commit；T011+T012 必须同批（原子）
