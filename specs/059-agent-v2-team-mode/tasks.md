# Tasks: Agent v2 Team 模式迁移（player + planner 双 agent）

**Input**: Design documents from `/specs/059-agent-v2-team-mode/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: 大型测试用例任务按 spec（constitution 原则 VI）包含在各 story phase；单测不单列任务（原则 IV：编译+单测是每次代码变更的一部分）。

**Organization**: Tasks grouped by user story。P1 内部顺序依依赖调整为 US2 → US3 → US1（US1 的 v1 移除依赖 US3 完成迁移——memory 插件的实现以 v1 源码为迁移样板，删除须在其后）；US4/US5 为 P2。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1–US5 mapping to spec.md user stories

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 新插件包骨架、依赖与模板 preset 数据，使 Phase 2 组合清单演进可一次性原子完成。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/javascript.md`
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的基准）
- **官方文档**：
  - [@deepseek-ai/dsh-agent-presets README（0.1.1-rc.2，unpkg 官方包内容镜像）](https://unpkg.com/@deepseek-ai/dsh-agent-presets@0.1.1-rc.2/README.md)（roster：preset 目录结构、`agent.cordis.yml`/`preset.yml` 数据格式、roots 配置——URL 阅读不受 T001 安装时序影响）
- **技术文章/技术参考文档**：
  - `specs/059-agent-v2-team-mode/research.md`（R3/R5/R7）
  - `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`（§5 组合清单终态）
  - `survey/deepseek-harness-roster-verification.md`（§2 组合清单与模板数据形态）
  - `specs/048-js-esm-migration/contracts/esm-package-conventions.md`（`style/javascript.md` 引用的仓库内 ESM 包级约定契约——新包骨架必读；按三分类格式归入本类）
  - `experimental/dsh/demo/agent/presets-templates/demo-standard/agent.cordis.yml` 与 `experimental/dsh/demo/agent/presets-templates/demo-standard/preset.yml`（058 模板 preset 数据样板；`demo-tools/` 子目录下同名两文件为第二样板）

### Tasks

- [X] T001 在 `pnpm-workspace.yaml` catalog 中新增本 feature 依赖（`@deepseek-ai/dsh-agent-presets`、`@deepseek-ai/dsh-agent-loop`，0.1.1-rc.2 同线精确 pin），执行 `pnpm up` 更新锁文件；不修改 `projects/game/agent` 相关条目（US1 范围）
- [X] T002 [P] 创建 `common/js/dsh-plugins/team/` 包骨架：`package.json`（`@dominion/dsh-team`，ESM 契约）、`tsconfig.json` + `.swcrc` 锁步、`src/index.ts` 导出最小 Service 插件（可被组合加载的空 `apply`）、`BUILD.bazel`（gazelle 生成 + `vitest_test` 宏）
- [X] T003 [P] 创建 `common/js/dsh-plugins/memory/` 包骨架（`@dominion/dsh-memory`，同 T002 形态；含 host 服务面与工具行的双导出入口占位）
- [X] T004 [P] 创建模板 preset 数据目录 `projects/game/agent_v2/preset-templates/`：`player/`（persona 行占位——含"你是扫雷 player…"第一人称身份开头锚行，锚行前缀为跨 phase 稳定契约，其余默认 base 内容由 T021 完善；+ `@dominion/dsh-saolei` 工具插件行）与 `planner/`（persona 行占位——同型锚行"你是扫雷 planner…"；+ `@dominion/dsh-memory` 插件行）各含 `agent.cordis.yml` + `preset.yml`（形态对照 `experimental/dsh/demo/agent/presets-templates/demo-standard/agent.cordis.yml` 与同目录 `preset.yml`）

**Checkpoint / 验证门禁**: `bazel build //common/js/dsh-plugins/... //projects/game/agent_v2/...` 通过；两个新包 `bazel test`（空套件）通过。

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: dsh 组合枢轴——官方 `dsh-agent-loop` 回归、roster 物化路径落地、saolei-loop 从 agent loop 枢轴为 team loop 骨架。本 phase 完成后既有**单 agent** 能力经新路径全量回归，是全部 story 的前提。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/javascript.md`
  - `style/api.md` 及其引用的 [AIP-133 Standard methods: Create](https://google.aip.dev/133)、[AIP-134 Standard methods: Update](https://google.aip.dev/134)（preset CRUD 扩展）
  - `style/mongo.md`（T006 Mongo Store 实现的文档模型——写 Mongo 代码前必读）
  - `style/large_test.md`（回归门禁执行）及其引用的 `style/golang.md`
- **官方文档**：
  - [@deepseek-ai/dsh-agent-presets README（0.1.1-rc.2，unpkg 官方包内容镜像）](https://unpkg.com/@deepseek-ai/dsh-agent-presets@0.1.1-rc.2/README.md)（standing mount/join、roots 有序与信任、compose/generation）
- **技术文章/技术参考文档**：
  - `specs/059-agent-v2-team-mode/research.md`（R3/R7 及"实现注意"四条：三面原子、runtime_deps 通道、副本可重建、roster 已知限制）
  - `specs/059-agent-v2-team-mode/contracts/preset-api.md`、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`（§5）
  - `survey/deepseek-harness-roster-verification.md`（§2 实测结论、§4.2 迁移路径要点、§5 被实践修订的对照）
  - `survey/deepseek-harness-team-mode.md`（§2.3 preset 挂载模型、§9.3 saolei-loop 层级转变）
  - `experimental/dsh/demo/agent/cordis.yml` 与 `experimental/dsh/demo/agent/src/session.ts`（058 实证的组合行与 `doCreate()` compose 接线样板）

### Tasks

- [X] T005 演进 `projects/game/agent_v2/cordis.yml` 组合清单（三面原子：cordis.yml ⟷ `projects/game/agent_v2/package.json` ⟷ tar 物化）：新增 `dsh-agent-loop`（Config.agents[] 留空）、`dsh-agent-presets`（roots = 1 个可写 user root + player/planner 两个模板 system root，不设 default）、`@dominion/dsh-team` 行、`@dominion/dsh-memory` host 行、`@dominion/dsh-preset-authoring` 行；`@dominion/dsh-saolei` 行从 host 层移除（改经 player 模板 preset 挂载，T004）；`projects/game/agent_v2/BUILD.bazel` 按 `runtime_deps`（workspace 包）/`npm_deps`（registry 包）通道补依赖（对照 `experimental/dsh/demo/agent/BUILD.bazel`）
- [X] T006 演进 `common/js/dsh-plugins/preset-authoring/src/`（Store seam 增加 Mongo 实现：`game_agent_v2.presets`、文档含 role/persona/时间戳，索引与凭据解析迁移自 `projects/game/agent_v2/src/presets.ts`；`Config.storage` 切换）+ `projects/game/agent_v2.proto`（`Preset` 增加 `role`（string——场景词汇，saolei 下 "player"/"planner"；create 必填不可变）、`CreatePresetRequest.role`、`ListPresetsRequest.role` 过滤（空=不过滤））+ `projects/game/agent_v2/src/server.ts` presets RPC 面切换到 authoring 插件（`ctx.presetAuthoring`，废弃直接读 `presets.ts` PresetRecord；`projects/game/agent_v2/src/presets.ts` 相应收缩为 Mongo 连接/凭据供 authoring Store 复用）
- [X] T007 重构 `common/js/dsh-plugins/saolei-loop/src/` 为 team loop 骨架：删除 `driver.ts`（自研 turn/step 状态机）与 `ctx.agents.setFactory` 工厂认领及 `AgentOptions.persona` declaration-merge（`src/index.ts:67-71`、`:295`、`:412-416`）；物化编排改为 `ctx.agents.create`（官方 factory）+ `ctx.presetAuthoring.compose()` 返回的 setup 内 mount + `GameRuntime` 以 agent-scoped `saoleiGame` 注册（注册点从 factory 迁至物化 setup，`src/game/runtime.ts` 归属不变）；`projects/game/agent_v2/src/session.ts` 物化调用点同步（现有单 agent UpdateAgent API 形态保持，内部换新路径）
- [X] T008 回归收口：`projects/game/fake-llm/service/testdata/agent_v2.yaml`、`agent_v2_saolei.yaml` 夹具适配 preset 行 persona（system_keywords 锚定模板 persona 身份开头锚行——T004 建立、T021 完善时保持不变的跨 phase 稳定前缀）；执行 `guitar run projects/game/testplan/system_test.yaml`（其 suite 1 引用 `deploy_agent_v2.yaml` 拓扑）确认既有单 agent 用例经官方 loop + roster 路径全量通过

**Checkpoint / 验证门禁**: `bazel build //... && bazel test //...` 通过；T008 大型测试全绿（架构枢轴不破坏既有能力）。

---

## Phase 3: User Story 2 - 在 web 上与 team 协作完成多局扫雷游戏 (Priority: P1) 🎯 MVP

**Goal**: team 物化（双成员）、群聊消息流、交替激活编排（用户首驱/续驱/排队优先/取消暂停）、team API 与实时流、web team 化最小面（配置面板 + 归并流渲染）。

**Independent Test**: 部署后物化 team（fake LLM + fake desktop），断言：物化后静止等待（无成员被驱动）；用户首条消息触发 planner 开局策略 → player 被驱动游戏至终局 → 自动复盘 → 结构性续驱第二局；除首条消息外无用户触发消息；排队/取消语义正确（quickstart V3/V4/V6）。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/javascript.md`
  - `style/api.md` 及其引用的 [AIP-156 Singleton resources](https://google.aip.dev/156)、[AIP-131 Standard methods: Get](https://google.aip.dev/131)、[AIP-136 Custom methods](https://google.aip.dev/136)、[AIP-193 Errors](https://google.aip.dev/193)
  - `style/golang.md`（gateway 路由调整）
  - `style/large_test.md` 及其引用的 `style/golang.md`（测试用例编写）
- **官方文档**：
  - [@deepseek-ai/dsh-agent README（0.1.1-rc.2，unpkg 官方包内容镜像）](https://unpkg.com/@deepseek-ai/dsh-agent@0.1.1-rc.2/README.md)（AgentHandle/followup、registry 生命周期、`agent/status`——编排驱动的消费面）
  - [@deepseek-ai/dsh-subagent README（0.1.1-rc.2，unpkg 官方包内容镜像）](https://unpkg.com/@deepseek-ai/dsh-subagent@0.1.1-rc.2/README.md)（continuation manager 与 settlement notice——team 插件投递结构参照；该包未安装于本地 node_modules，故以官方包内容 URL 为准）
- **技术文章/技术参考文档**：
  - `specs/059-agent-v2-team-mode/research.md`（R5/R6/R8/R11）
  - `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`（§1 team 插件、§2 saolei-loop 编排）
  - `specs/059-agent-v2-team-mode/contracts/team-api.md`、`specs/059-agent-v2-team-mode/data-model.md`（§1/§3/§5）
  - `specs/059-agent-v2-team-mode/contracts/web-views.md`（§1–§3——T017 web team 化最小面：配置面板/归并渲染/未物化引导）
  - `survey/deepseek-harness-team-mode.md`（§4.4/§4.4a buffer 引用模型、§5.3 广播格式、§9.4 运行流程）
  - `survey/deepseek-harness-agent-loop-prereq.md`（§2 官方 loop 结构、§4 异常处理——编排等待/idle 语义依据）
  - `specs/059-agent-v2-team-mode/quickstart.md`（V3/V4/V6 场景）

### Tasks

- [X] T009 [P] [US2] 实现 `common/js/dsh-plugins/team/src/` 核心一：`ctx.team.register({goal, members})`（幂等；经成员 `agent.ctx` 注册 team section——order 1–49、内容=goal+名册（每成员 `[role] summary` 第三人称一句话）+ 广播格式约定、不含第一人称身份）；成员 `session/event` 订阅收集（`assistant/message` 发言；`tool/call`+`tool/result` 按 callId 配对）；`MessageSourceMap` merge 扩展 `team-broadcast`（`form:'relay'`、role/senderSessionId/messageId/context）；dispose 时 scope 自动清理（含单测）
- [X] T010 [P] [US2] 实现 `common/js/dsh-plugins/team/src/` 核心二：**引用投递**（产出事件 → messageId/callId 锚点按到达序追加进除发送者外各成员待消费列表；不复制内容；无全局消息副本队列，中转条目在进入全部接收方列表后即移除）；`drain(member)`（team 内部按锚点经 session 读取面取实际内容 → 渲染广播格式：`[sender] 摘要` 头行 + 标签对包裹的**原样正文**——发言原文、工具 args 与 result 全文，不截断不聚合（FR-008）→ 返回注入就绪 UserMessage；索引不对外暴露）；派生重建（待消费 = sender log 产出 − receiver 消费锚点集合；exactly-once、丢失自愈）（含单测）
- [X] T011 [P] [US2] 新增 fake-llm team 双角色夹具 `projects/game/fake-llm/service/testdata/`：`team_planner.yaml`（system_keywords 识别 planner persona；产出开局策略与复盘正文；含 memory 工具调用步骤——本 phase 可不含，US3 启用）、`team_player.yaml`（识别 player persona；依脚本调用 saolei 工具至终局；接收策略广播后续驱）
- [X] T012 [US2] 演进 `projects/game/agent_v2.proto` 会话面为 team 模型（对照 `specs/059-agent-v2-team-mode/contracts/team-api.md` 全量一次到位；**泛化约束（2026-09-10 用户裁定）：会话面与 saolei 场景解耦——无 role 枚举、无场景特化字段**）：`Team`（单例资源；物化输入 = `members` 列表——每成员 `{role, preset, model?}`，不设 player_preset/planner_preset/player_model/planner_model 类字段；输出 members 与输入同形 + desktop_connected）、`TeamMember`（role/preset/model 与 Team.members 同形 + output-only `system_prompt`）、`UpdateTeam`/`GetTeam`/`GetTeamMember`/`ListTeamMessages`/`ListMemberMessages`、`Send` 请求面不变（流语义为 team 流，team-api.md §3.1）、`Cancel` target 改 team、`ChatEvent` 增加 `member` 字段与 team 级 `team_message` 帧（`{member, message, seq}`，与 ListTeamMessages 元素同构、seq 同源同值——team-api.md §3.2）、移除 `Agent`/`UpdateAgent`/`GetAgent`/`ListAgentMessages`；`TeamRole`/`PresetRole` 枚举不引入——`TeamMember.role`/`ChatEvent.member`/`TeamMessage.member`/`MemberViewMessage.sender`/`Preset.role`/`CreatePresetRequest.role`/`ListPresetsRequest.role` 全部 string（约定值：成员 role=场景词汇 "player"/"planner"；用户消息标注保留值 "user"；空字符串=未设置）；场景约束（members 恰 2、role 集合恰 {"player","planner"}、preset.role 与成员 role 字符串相等、model 在目录）由 agent_v2 服务端校验承载（team-api.md §2）；受影响消费方（`src/server.ts` 校验/`src/session.ts` 物化/`src/history.ts` 成员标注/web `parseMember` 与 preset 过滤值/夹具与 testplan 断言）按契约同步适配；更新 `projects/game/proto_test.go` 与生成类型
- [X] T013 [US2] 实现 `common/js/dsh-plugins/saolei-loop/src/` 编排状态机（消费 `ctx.team`）：交替激活（任一时刻至多一成员被驱动）；物化后静止等待（初始激活 = planner，不自动驱动任何成员），用户首条消息驱动 planner 产出开局策略；一切驱动以群聊消息为输入（drain 未消费团队消息 + 排队用户消息），无合成驱动消息、无输入保持静止在当前激活成员；planner 回合结束→排队消息先由 planner 消化（消化优先于切换）→结构性续驱 player；player 侧 gameEnded（GameRuntime 游戏事件流）→驱动 planner 复盘；取消=终止在途回合+暂停续驱；用户消息经注入 seam 由当前激活成员处理（planner memory load 以 DI seam 注入，US3 接真实实现）（含单测：状态机转移/排队优先/取消暂停/恢复）
- [X] T014 [US2] 重构 `projects/game/agent_v2/src/session.ts` 为 team 注册表：per-session team 物化/刷新（fail-fast 校验 preset 存在且 role 匹配、model 在目录；**物化任一步失败——含成员 setup 抛错（planner memory 预取 fail-loud，T013 seam / T021 真实实现）——清理已建成员并整体回滚：不残留半物化 team（GetTeam NOT_FOUND）、可重试**；刷新=终止在途回合+排队作废+清空记忆+重建；create_time 保留）、team 级 FIFO 排队、cancel 编排、与 saolei-loop 编排服务的接缝（含单测：物化中途失败整体回滚无半物化）
- [X] T015 [US2] 演进 `projects/game/agent_v2/src/history.ts`：双成员事件收集（`session/event`/`agent/status` 按成员标注）、ChatEvent `member` 归并、团队归并序列（seq 单调分配——`team_message` 帧载荷与 ListTeamMessages 元素同源同值）与成员视角历史的内存投影（List 面数据源，US4 启用 RPC）
- [X] T016 [US2] 重构 `projects/game/agent_v2/src/server.ts` RPC 面：`UpdateTeam`/`GetTeam`/`GetTeamMember`/`Send`（team 流：持续至 team 静止、成员事件帧 + `team_message` 帧双承载、扇出至全部活跃流且 `queued` 帧仅回执本流、断开不终止编排——team-api.md §3）/`Cancel`（team 语义）+ 错误映射（`INVALID_ARGUMENT`/`FAILED_PRECONDITION`/`NOT_FOUND`，cause 链）；`projects/game/gateway/cmd/main.go` 路由同步（`/api/v2` team 面，经 proxy；移除旧 agent 面注册）
- [X] T017 [P] [US2] web team 化最小面 `projects/game/web/frontend/src/`：`AgentSettingsPanel.tsx` → `TeamSettingsPanel.tsx`（player/planner preset 下拉按 role 过滤 + 双 model 下拉 + Apply=UpdateTeam + 刷新语义提示）；`api/agent.ts` → team API 客户端；对话页消费 team 流（成员事件帧按 member 归并增量渲染 + `team_message` 帧 seq 锚归并——team-api.md §3.2；团队视图雏形：成员标签区分 player/planner）；未物化引导态对齐
- [X] T018 [US2] 大型测试：在 `projects/game/testplan/deploy_agent_v2.yaml` 既有计划中按模块归位新增用例（team 物化、静止等待与用户首驱（物化后无 Send 不出现任何驱动） / 完整局至终局 / 复盘与续驱第二局 / 排队消化优先 / 取消暂停与恢复 / 刷新 team——在途回合终止+排队作废+记忆清空+重建+create_time 保留（US2 场景 7）/ desktop 断连与重连恢复——工具错误结果可见、进程存活（US2 场景 8）/ 未物化拒绝），对照 `projects/game/testplan/` 既有 Go 测试文件组织（helper 复用 `helpers_test.go`，资源名全称包装）

**Checkpoint / 验证门禁**: `bazel build/test` 通过；`guitar run projects/game/testplan/system_test.yaml` 全量通过（含既有回归）——US2 独立可验收。

---

## Phase 4: User Story 3 - preset 分池管理与角色工具锁定 (Priority: P1)

**Goal**: memory 插件完整落地（工具+快照+host 服务面）、planner 侧接线、web preset 管理 role 化、角色锁定的端到端断言。

**Independent Test**: 两池 preset CRUD（role 过滤/不可变）；物化后 player 恰有 saolei 工具组守则、planner 恰有 memory 守则与记忆快照（fake-llm system_keywords 断言）；memory 修改经 `/api/v1/.../memories` 可查证（quickstart V2 + US2 场景 5）。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/javascript.md`
  - `style/api.md` 及其引用的 [AIP-133](https://google.aip.dev/133)、[AIP-134](https://google.aip.dev/134)
  - `style/large_test.md` 及其引用的 `style/golang.md`
- **官方文档**：
  - [@deepseek-ai/dsh-system-prompt 类型声明（0.1.1-rc.2，unpkg 官方包内容镜像）](https://unpkg.com/@deepseek-ai/dsh-system-prompt@0.1.1-rc.2/lib/types/index.d.ts)（PromptSection.text 函数式形态——快照 section 依据）与其[同包 README（unpkg）](https://unpkg.com/@deepseek-ai/dsh-system-prompt@0.1.1-rc.2/README.md)
  - [@deepseek-ai/dsh-tools README（0.1.1-rc.2，unpkg 官方包内容镜像）](https://unpkg.com/@deepseek-ai/dsh-tools@0.1.1-rc.2/README.md)（defineTool schema DSL 用法）与其 [lib/types/schema.d.ts（unpkg）](https://unpkg.com/@deepseek-ai/dsh-tools@0.1.1-rc.2/lib/types/schema.d.ts)（ValueSchemaSpec/ParameterSchemaSpec——memory 工具参数 schema 与约束关键词（const/enum/required）依据）
- **技术文章/技术参考文档**：
  - `specs/059-agent-v2-team-mode/research.md`（R2 角色身份边界/R4 memory 决策）
  - `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`（§3 memory、§4 saolei 工具行）
  - `specs/059-agent-v2-team-mode/contracts/preset-api.md`
  - `survey/deepseek-harness-memory-plugin.md`（决策 ①–⑧ 与 v1 迁移锚点表 §2.1）
  - `projects/game/agent/src/mcp/memory/memory-mcp.ts`、`projects/game/agent/src/memory-client.ts`（v1 迁移样板源码——**本 phase 结束前保持可用**，US1 随后移除）
  - `common/js/dsh-plugins/saolei/src/index.ts`（工具+guidance 同 apply 注册的结构参照）
  - `specs/059-agent-v2-team-mode/quickstart.md`（V2 场景）

### Tasks

- [X] T019 [P] [US3] 实现 `common/js/dsh-plugins/memory/src/` host 服务面：`ctx.plannerMemory`（`load(agentCtx, {template, session})`——经 gRPC client（迁移自 `projects/game/agent/src/memory-client.ts`，`dominion:///game/memory:50051`）读 memory 服务 → 渲染纯文本快照（每行一条、不含 id）→ 写快照缓存并绑定 agent scope；失败 throw=fail-loud；写路径 add/replace/remove/批量原子经同 client 落库立即持久化）（含单测：vi.fn() double 的 client seam）
- [X] T020 [P] [US3] 实现 `common/js/dsh-plugins/memory/src/` preset 行功能面：memory 单工具（参数 schema 对照 v1 `memory-mcp.ts:427-471`——action/content/old_text 单操作 XOR operations[] 批量互斥、批量原子 preflight、old_text 子串定位 0/多命中返回条目文本、失败即文本结果不抛错、无 read 动作；description 改写为"快照固定于 agent 启动"语义）+ 快照 section（函数式 `text:(context)=>缓存.get(context.scope)??""`、order 200+、空不渲染）（含单测）
- [X] T021 [US3] 接线：`common/js/dsh-plugins/saolei-loop/src/` 物化编排的 planner setup 调用真实 `ctx.plannerMemory.load`（替换 T013 的 DI seam；**load 失败 throw 经 T014 物化编排整体回滚路径处理，无半物化**）；完善 `projects/game/agent_v2/preset-templates/` 两模板 persona 默认 base（第一人称角色身份开头，R2 边界：persona 不含团队级事实；**不得改动 T004 建立的身份开头锚行前缀——T008/T011 夹具 system_keywords 已锚定该行**）与 authoring 模板校验（player 模板恰含 saolei 行、planner 模板恰含 memory 行，违约 INVALID_ARGUMENT）
- [X] T022 [P] [US3] web preset 管理扩展 `projects/game/web/frontend/src/components/PresetsView.tsx` 与 `api/agent.ts`：列表 role 标识与过滤、新建表单 role 必选单选、编辑仍仅 persona
- [X] T023 [US3] 大型测试：`projects/game/testplan/` preset 模块与 memory 断言用例（分池 CRUD 与 role 校验拒绝 / 热创作零重启生效 / 服务重启后 preset 持久化（quickstart V2-3）/ persona 空值回退该角色默认 base（US3 场景 3）/ 角色锁定——fake-llm system_keywords 断言 player system prompt 含 saolei 守则且无 memory 痕迹、planner 相反且含快照 / 复盘中 memory 调用经 `/api/v1/.../memories` 断言持久化 / 物化时 memory 服务不可达 fail-loud 回滚）

**Checkpoint / 验证门禁**: `bazel build/test` 通过；大型测试全量通过（含 US2 用例回归）。

---

## Phase 5: User Story 1 - 完全移除 agent v1，交付纯净的 v2 基线 (Priority: P1)

**Goal**: v1 服务、协议、配套服务、夹具与过期引用全部移除；保留面（SessionService/UserFrame/TeamFrame/memory 服务）不受影响。

**Independent Test**: 代码检索零残留 + 全仓构建测试通过 + 大型测试回归全绿（quickstart V1）。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/golang.md`（Go 服务/常量清理）
  - `style/api.md`（proto 移除涉及）
- **官方文档**：无
- **技术文章/技术参考文档**：
  - `specs/059-agent-v2-team-mode/research.md`（R10 移除清单与保留项）
  - `specs/059-agent-v2-team-mode/quickstart.md`（V1 场景）
  - `specs/058-dsh-preset-roster-demo/checklists/boundaries.md`（边界审计 grep 命令面模板）

### Tasks

- [X] T024 [US1] 移除 `projects/game/agent/` 整目录（含 BUILD.bazel/service.yaml/node_modules 清理）及 `pnpm-workspace.yaml:9` 的 workspace 条目与 `pnpm-lock.yaml` 对应 importer 段（`pnpm up` 收敛）；`bazel run //:gazelle` 清理生成 target
- [X] T025 [P] [US1] 移除 `projects/game/game.proto` 中 v1 专属定义（`TeamService`、`PromptService` 及 `Team`/`TeamAgent`/`SaoleiProfile`/`UpdateTeamRequest`/`RefreshTeamRequest`/`TeamProfile` 等仅 v1 使用的消息；保留 `SessionService`/`Session`/`UserFrame`/`TeamFrame`/`MemoryService`）；更新 `projects/game/proto_test.go` 与全部生成类型消费方（gateway/agent_v2/desktop/session 等编译面）
- [X] T026 [P] [US1] 移除 `projects/game/prompt/` 整目录（v1 专属配置服务）及其 BUILD/部署引用核查
- [X] T027 [P] [US1] 清理 fake-llm v1 专属夹具 `projects/game/fake-llm/service/testdata/`（`planner.yaml`、`planner_tools.yaml` 及其余仅引用 v1 源码路径/语义的 v1 时期夹具——以 grep `projects/game/agent` 引用与 v1 复盘关键词核查为准逐一判定），保留 v2/team 夹具
- [X] T028 [US1] 过期引用清理：`projects/game/pkg/gameconst/const.go` 的 `TeamTarget` 更名（现为 v2 proxy 目标，名实对齐，如 `AgentV2Target`）及注释；`projects/game/fake-desktop/service/executor.go:10` 等几何公式注释自包含化（公式本体随 v1 删除，内联公式内容并标注来源语义）；全仓 grep `projects/game/agent`、`@dominion/game-agent`、`TeamService`、`PromptService` 断言零残留（specs/survey 历史文档除外）

**Checkpoint / 验证门禁**: 检索零残留；`bazel build //... && bazel test //...` 通过；`guitar run projects/game/testplan/system_test.yaml` 全绿。

---

## Phase 6: User Story 4 - team 对话双视图 (Priority: P2)

**Goal**: 团队视图（1 个，原生输出归并）+ 成员视角视图（2 个，"自己=agent、他人=标注来源 user"）的 API 与 UI。

**Independent Test**: 一局含用户消息与双成员产出的对话后，ListTeamMessages 归并正确、ListMemberMessages 视角正确、同一消息跨视图正文一致；web 恰 3 个视图切换（quickstart V5）。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/javascript.md`
  - `style/api.md` 及其引用的 [AIP-132 Standard methods: List](https://google.aip.dev/132)
  - `style/large_test.md` 及其引用的 `style/golang.md`
- **官方文档**：无
- **技术文章/技术参考文档**：
  - `specs/059-agent-v2-team-mode/research.md`（R9）
  - `specs/059-agent-v2-team-mode/contracts/team-api.md`（§5 历史读取）、`specs/059-agent-v2-team-mode/contracts/web-views.md`（§2/§3/§4）
  - `specs/059-agent-v2-team-mode/data-model.md`（TeamMessage/MemberViewMessage）
  - `specs/059-agent-v2-team-mode/quickstart.md`（V5 场景）

### Tasks

- [ ] T029 [US4] 实现 `projects/game/agent_v2/src/` List 面：`ListTeamMessages`（归并序列 + seq 单调 + member 标注）、`ListMemberMessages`（成员视角 + sender 标注），数据源为 T015 历史投影；`server.ts` RPC 接线与分页兼容位
- [ ] T030 [US4] web 双视图 `projects/game/web/frontend/src/`：视图切换器（团队 | player | planner，各视图历史常驻不重填）；团队视图（原生输出归并、成员标签、不显示广播包装形态、CompletedTurn 折叠按成员维度）；成员视角视图（`user: [sender]` 标注渲染）；`store/chat.ts` 扩展 member 归并与双 store 形态
- [ ] T031 [US4] 大型测试：视图数据断言用例（两类 List 面内容与标注 / 同一消息跨视图正文一致 / 刷新后历史按新生命周期重建）

**Checkpoint / 验证门禁**: `bazel build/test` 通过；大型测试全量通过。

---

## Phase 7: User Story 5 - 查看 team 成员的 system prompt (Priority: P2)

**Goal**: 每成员实例的完整 system prompt 可查看且与实际生效一致。

**Independent Test**: 双成员 system prompt 完整可读、因角色分化（player 无 memory 痕迹/planner 有）、刷新后随新配置更新（quickstart V5-4/V2-2）。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/javascript.md`
  - `style/large_test.md` 及其引用的 `style/golang.md`
- **官方文档**：
  - [@deepseek-ai/dsh-system-prompt 类型声明（0.1.1-rc.2，unpkg 官方包内容镜像）](https://unpkg.com/@deepseek-ai/dsh-system-prompt@0.1.1-rc.2/lib/types/index.d.ts)（SystemPrompt.assemble 与 `system-prompt/assemble` 事件面——从装配面取实际内容而非另行拼装）
- **技术文章/技术参考文档**：
  - `specs/059-agent-v2-team-mode/research.md`（R2/R9）
  - `specs/059-agent-v2-team-mode/contracts/team-api.md`（GetTeamMember）、`specs/059-agent-v2-team-mode/contracts/web-views.md`（§5）
  - `specs/059-agent-v2-team-mode/data-model.md`（SystemPrompt 所有权表）
  - `specs/059-agent-v2-team-mode/quickstart.md`（V5-4）

### Tasks

- [ ] T032 [US5] 实现 `projects/game/agent_v2/src/` 的 `GetTeamMember` `system_prompt` 字段：从各成员实例的 system prompt 装配面取实际内容（与发给模型一致，非另行拼装）；接入 saolei-loop 物化编排的成员句柄读取面
- [ ] T033 [P] [US5] web system prompt 查看入口 `projects/game/web/frontend/src/`：成员清单处入口 + 只读全文呈现（等宽），刷新 team 后内容随新配置更新
- [ ] T034 [US5] 大型测试：system prompt 断言用例（完整可读 / player-planner 分化 / 编辑 persona 刷新后更新）

**Checkpoint / 验证门禁**: `bazel build/test` 通过；大型测试全量通过。

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: 文档终态化与全量验收收口。

### 文档清单（本 phase 必读）

- **代码规范文档**：
  - `style/large_test.md`（全量验收执行规范）
- **官方文档**：无
- **技术文章/技术参考文档**：
  - `specs/059-agent-v2-team-mode/quickstart.md`（V1–V7 全量）
  - `specs/059-agent-v2-team-mode/spec.md`（SC-001–SC-005 验收口径）

### Tasks

- [ ] T035 更新 `projects/game/agent_v2/README.md` 为 team 模型终态（三服务、team 物化/刷新语义、preset 分池、双视图、已知限制——compact 排除与 planner 上下文增长、内存态重启重物化）；核对 `projects/game/deploy.yaml` 服务清单无 v1 残留引用
- [ ] T036 执行 quickstart V1–V7 全量验证：`guitar run projects/game/testplan/system_test.yaml` 完整部署→测试→清理闭环，全部用例通过（无 failed/flaky）；对照 spec SC-001–SC-005 逐条判定达成

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: 无依赖，立即开始。
- **Phase 2 (Foundational)**: 依赖 Phase 1（组合行需要包骨架与模板数据可加载）；**阻塞全部 user story**。
- **Phase 3 (US2 MVP)**: 依赖 Phase 2（官方 loop 物化路径与 roster）。
- **Phase 4 (US3)**: 依赖 Phase 3（team 物化编排存在，memory 以 DI seam 先行；v1 源码仍可用作迁移样板）。
- **Phase 5 (US1)**: 依赖 Phase 4（memory 迁移完成后 v1 目录方可删除）。
- **Phase 6 (US4)**: 依赖 Phase 3（消息流与 member 标注）。
- **Phase 7 (US5)**: 依赖 Phase 3（成员实例与物化编排）；与 Phase 6 可并行。
- **Phase 8 (Polish)**: 依赖全部 story 完成。

### User Story Dependencies（P1 内序 = US2 → US3 → US1，依依赖非优先级差异）

- **US2**: 无 story 间依赖（foundational 后即可开始）——MVP。
- **US3**: 依赖 US2 的物化编排（DI seam 接线点）；memory 实现本身独立可并行开发。
- **US1**: 依赖 US3（v1 memory 源码为迁移样板，删除须在其后）；其余移除项独立。
- **US4/US5**: 依赖 US2；互相独立。

### Parallel Opportunities

- Phase 1: T002 ∥ T003 ∥ T004（不同目录）；T001 先行（锁文件串行）。
- Phase 2: T006 完成前 T005 的组合行中 authoring/presets 行需 T006 代码就位——**T005 与 T006 实质原子交付**（同一变更）；T007 依赖 T005/T006。
- Phase 3: T009 ∥ T010（team 插件两段，文件可分）∥ T011（夹具）；T014–T016 串行（同文件簇）；T017 [P] 可与后端任务并行。
- Phase 4: T019 ∥ T020 ∥ T022（不同文件）；T021 依赖 T019/T020。
- Phase 5: T025 ∥ T026 ∥ T027（不同文件）；T024 与 T028 随后。
- Phase 6/7: 两个 phase 可由不同执行者并行。

---

## Implementation Strategy

### MVP First (US2)

1. Phase 1 + Phase 2（架构枢轴，既有能力回归门禁）
2. Phase 3 (US2) → **STOP and VALIDATE**: `guitar run` 全绿即 MVP（team 多局闭环可演示）
3. Phase 4 (US3) → Phase 5 (US1 清理) → 依次交付

### Incremental Delivery

每个 phase 以 `bazel build/test` + 阶段大型测试为门禁；任一 checkpoint 后可中断/恢复（任务粒度按文件与契约条款切分，恢复时以未勾选任务为起点）。

---

## Notes

- 单测（vitest）随每个实现任务交付，遵守 `style/javascript.md` Mock 约定（DI/vi.fn()，禁止新增模块级 vi.mock）。
- 大型测试用例按被测模块归位到既有 `deploy_agent_v2.yaml` 计划（禁止新建平行测试计划，`style/large_test.md` 反模式 1/4）。
- 组合清单与依赖变更必须三面原子（package.json ⟷ cordis.yml ⟷ tar 物化，闭包审计依据 `survey/deepseek-harness-roster-verification.md` §5 对照 3）。
