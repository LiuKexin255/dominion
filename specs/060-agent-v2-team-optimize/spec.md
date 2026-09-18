# Feature Specification: Agent v2 team 模式优化（部署配置收敛 / 常量库 / 实时流修复 / 广播净化 / 提示词分层）

**Feature Branch**: `060-agent-v2-team-optimize`

**Created**: 2026-09-11

**Status**: Draft

**Input**: User description: "`specs/059-agent-v2-team-mode/` 很成功，测试验证大部分流程均正常。现在有个优化项：1. `projects/game/deploy.yaml` agent-v2 当中的 env 配置。(1) 非必要不增加配置，如果是默认设置可以省略。(2) PRESET_TEMPLATES_ROOT 的前半段 /dominion/game/agent-v2 是由打包工具决定的，写在这里是不稳定的。我记得 deploy 部署时会注入环境变量声明产物放置位置（如果没有则添加一个）。(3) PRESET_WRITABLE_ROOT 设计上就是临时文件，为什么不使用更通用的 /tmp 目录。(4) 最终的 preset 是否需要以文件的方式放到目录中，因为这还需要维护这个文件与 mongo 中的数据保持一致。能否在真正使用时再拼装这个 preset（最好是在内存中直接使用，如果必须以文件的方式，那就纯当做临时文件，用完删除/废弃，不维护）。这样是不是成本更低一些。2. 在 common 目录下增加一个 const lib（包括 golang 和 js），用来收集那些仓库通用的常量。这次就先将 deploy 保留环境变量放进去。这样其他的代码在使用时可以通过这个 lib 使用（关联上面的问题）。这次修改常量引用应用范围 deploy 工具和服务、以及 `projects/game/` 下使用这些常量的服务。3. webUI 现在实时更新消息有几个 bug：(1) 启动时 planner 页面输出会丢失用户输入；(2) tools 完成后没有及时更新结果和状态。这两个问题在刷新页面以及完全静止后可以通过 history 修正（我观察页面猜测 agent 在完成后会通过 history 进行一次刷新）。我推测是 stream 流丢失了一些数据。4. webUI 没看到按钮可以查看 agent 的系统提示词。5. team 广播消息移除 think 内容。另外在提示词上有几个优化：1. 将 saolei 游戏和可用操作的提示词移动到 saolei_loop 当中，也就是在 saolei_loop 声明游戏玩法和可用操作（操作对齐 saolei 插件中实现的工具，取两者的交集），这样 player 和 planner 的提示词都能包括这部分。顺便查询下比较权威的玩法说明（例如 wiki，saolei 游戏目标是旧版经典扫雷，win98 上那种），作为提示词。2. 而 saolei 插件给工具配套的提示词，则仅包括工具如何使用（不再包括游戏玩法和操作对游戏的影响）。" 补充 1：webUI 上 agent 对话内容的"思考过程（N 步骤 · M 次工具调用）"似乎只在 agent 正常停止后出现，agent 运行中没有——确认是否符合 dsh-web 的设计预期。补充 2：message wire 格式有重复内容（广播头行摘要复述了正文开头），要么以 `[agent-name]` 开头、要么用 XML 形式 `<{agent-name}-message></...>`，二者选其一即可。补充 3：team 信息增加说明当前激活的 agent 是哪个。

## Motivation

`specs/059-agent-v2-team-mode/` 交付了 agent v2 的 team 模式（player + planner 双 agent、roster preset、双视图 web UI、team 流）。测试验证大部分流程正常后，本 feature 收敛一批优化项与缺陷：部署配置的稳定性与维护成本、仓库常量的单一事实源、webUI 实时流的正确性、team 可观测性与广播消息质量、提示词的所有权分层。

目标与现状的差距（本 feature 要完成的工作）：

| 维度 | 现状（059 后） | 目标 |
|---|---|---|
| agent-v2 env 配置 | deploy.yaml 显式声明 `PRESET_TEMPLATES_ROOT`（含打包工具决定的不稳定前缀 `/dominion/game/agent-v2`）与 `PRESET_WRITABLE_ROOT`（专有容器路径） | 平台注入产物位置保留变量（现无则新增）；preset 路径在服务内推导；部署清单非必要配置全部省略 |
| preset 数据形态 | Mongo store 记录 + 可写目录中的组合文件副本，两份需保持一致（copy-then-patch 双写维护） | store 为唯一事实源；成员物化所需的组合在使用时从 store 记录派生（内存优先；必须文件时为纯临时产物，不维护） |
| 仓库通用常量 | deploy 保留环境变量名散落定义于 deploy 工具（`projects/infra/deploy/runtime/k8s/builder.go` 常量）、各 common 包与服务中的字面量 | common 下新增常量库（Go + JS），首批收录保留环境变量名；deploy 工具/服务与 `projects/game/` 使用方统一引用 |
| webUI 实时流 | 成员视角视图实时流不呈现用户输入（仅回填修正）；工具完成后结果/状态不及时更新（刷新或静止后经 history 修正） | 用户输入在被成员消费时实时呈现；工具结果与终态在完成时实时呈现 |
| team 可观测性 | GetTeam 不暴露当前激活成员（编排层持有 `activation`/`drivingMember` 但不外露）；system prompt 查看入口藏在"设置 team"面板内 | team 查询面暴露当前激活成员；对话主界面直接可见激活成员与成员 system prompt 入口 |
| 广播消息质量 | 广播正文含 think（reasoning）内容；wire 格式为"头行摘要 + 标签对包裹正文"，头行摘要复述正文开头造成重复 | 广播不含 think；单一标注形态（发送者标注不重复正文） |
| 提示词所有权 | 玩法与操作说明混在 saolei 工具守则（仅 player 可见）与 preset persona 中；planner 无游戏规则输入 | 玩法 + 可用操作由 saolei-loop 提供（全员可见，依据权威玩法说明）；工具守则仅剩工具用法；persona 不重复玩法 |

## Clarifications

> 本节记录已裁定/已验证事项的结论来源；终态规范编码于 FR 与 Assumptions。

### 补充 1 裁定：运行中不出现"思考过程"折叠符合上游设计（无需修改）

上游 dsh-web 的 Turn process folding 设计（https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/archived/feature/2026-08-14-web-turn-process-folding.md ）明确："a Turn remains fully expanded while it is open"（回合打开期间保持完全展开）、"an open Turn never folds"、折叠控件"remains hidden until the closed Turn has a final answer and complete history"（回合关闭且存在最终答案后才显示）。当前 web 实现（`projects/game/web/frontend/src/components/ChatView.tsx` CompletedTurn/live 分路径）与该设计一致：**运行中保持展开、回合结束后才出现"思考过程（N 步骤 · M 次工具调用）"折叠控件是预期行为**，本 feature 不修改，仅在此记录确认结论。

### 补充 2 裁定：广播 wire 格式择一（去重）

用户裁定：发送者标注与正文不得重复——要么 `[角色]` 头行形态、要么 `<角色-message>` 标签对形态，二者选其一。当前实现（`common/js/dsh-plugins/team/src/broadcast.ts` renderBroadcast）的头行 `[角色] 摘要` 中"摘要"取自正文首行，属复述冗余。具体择哪一形态（及工具调用的对应形态）由 plan 阶段决定；team section 的广播格式约定（`common/js/dsh-plugins/team/src/section.ts`）与依赖广播标记的 fake-llm 夹具/大型测试断言（如 `projects/game/fake-llm/service/testdata/team_planner.yaml` 的 `<player-message>`/`<player-tool-call>` 锚点）MUST 同步更新。

### 用户偏好顺序：preset 拼装机制

用户对 preset 使用时拼装的偏好：**最好在内存中直接使用；如果必须以文件的方式，那就纯当做临时文件（用完删除/废弃，不维护）**。机制选择（内存行挂载 vs 临时文件）由 plan 阶段调研官方 roster 挂载面后决定，spec 只约束终态行为（FR-003）。

### Session 2026-09-11（clarify 裁定）

- Q: 常量库引用切换的范围是否包含 common 既有包内部的同名常量定义（如 resolver/otel/config/mongo 等包各自定义的 `DOMINION_ENVIRONMENT` 字面量）？ → A：不包含。边界原则：**该需求的目的是常量一致性与避免冗余，并非机械式地收集常量**——common 下已是公共库的包自身即可作为其领域常量的统一权威来源，不重复收进新常量库也不改为引用新库；新常量库只收**跨领域、尚无既有权威来源**的仓库级常量（本次即 deploy 平台保留环境变量名）。切换范围维持 deploy 工具与服务 + `projects/game/` 下使用方（FR-005）。
- Q: team 查询面的"当前激活成员"以单一合并值暴露，还是将在途驱动成员与下一条输入归属成员分开暴露？ → A（用户裁定）：**单一合并值**——成员回合在途时为该回合的驱动成员（任一时刻至多一个），静止时为下一条输入归属成员（activation；物化后初始为 planner）；两概念在串行驱动下行为不分离，一个值即完整回答"现在是谁在处理"，不过度拆分 API 契约（FR-008）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 部署配置精简与产物位置保留环境变量 (Priority: P1)

部署平台向每个 artifact 服务注入一个**声明产物放置位置的保留环境变量**（当前不存在——现有保留变量只有 SERVICE_APP/DOMINION_ENVIRONMENT/POD_NAMESPACE/TLS_*/S3_*/DOMINION_SECRET_DIR/DOMINION_CONFIG_DIR，见 `projects/infra/deploy/runtime/k8s/builder.go` 与 `tools/release/deploy/README.md` §环境变量保留清单），值即打包工具放置产物的目录（`/dominion/{app}/{service}`，`tools/release/deploy/README.md` §镜像布局）。agent-v2 的 preset 模板根改为从该变量推导（默认值在服务代码内，本地/测试可显式覆盖），`projects/game/deploy.yaml` 与 `projects/game/testplan/deploy_agent_v2*.yaml` 中的 `PRESET_TEMPLATES_ROOT`/`PRESET_WRITABLE_ROOT` 全部移除——非必要不配置。

**Why this priority**: 用户要求 1 的直接交付物；消除部署清单中由打包工具决定的不稳定路径（打包布局一旦变化部署清单即失效），并以"非必要不配置"降低部署面维护成本。

**Independent Test**: 全仓构建与测试通过；部署（生产与 testplan 拓扑）后 agent-v2 正常 boot、roster 模板根解析成功（模板 preset 可列出/物化）；检索部署清单确认 preset 路径 env 零残留；新增保留变量出现在容器 env 中且用户显式同名配置被平台覆盖（保留名语义）。

**Acceptance Scenarios**:

1. **Given** 任意 artifact 服务部署，**When** 容器启动，**Then** 平台注入产物位置保留环境变量，值为该服务产物的实际放置目录；该变量名在保留变量清单（deploy 文档与校验）中，用户 env 同名声明被平台值覆盖（对齐既有保留名语义）。
2. **Given** agent-v2 部署（生产与 testplan 拓扑）且部署清单未声明任何 preset 路径 env，**When** 服务 boot，**Then** roster 的模板根（player/planner 两池）从产物位置变量推导解析成功，preset 列表/物化行为与现状一致。
3. **Given** 本地/测试环境无产物位置变量，**When** 运行 agent_v2 测试，**Then** 显式覆盖变量（既有宿主注入模式）或测试内推导默认使 roster 解析可用（现有单测/大型测试全量通过）。
4. **Given** `projects/game/deploy.yaml`，**When** 变更完成，**Then** agent-v2 的 env 块除 secret 绑定外无任何 preset 相关条目（默认设置省略）。

---

### User Story 2 - preset 唯一事实源化：使用时派生，不维护文件 (Priority: P1)

用户创作的 preset 不再以"目录中被维护的文件"形态存在：preset 的 CRUD（创建/编辑/删除）只写 store（Mongo）；成员物化所需的 preset 组合在**真正使用时**从 store 记录派生——优先在内存中直接组装生效；若机制上必须文件，则文件为纯临时产物（使用时生成于系统临时目录，不维护与 store 的一致性）。部署清单与容器路径约定中不再存在需感知的 preset 可写根（`PRESET_WRITABLE_ROOT` 语义消亡；临时文件落系统临时目录 /tmp 一类通用位置）。

**Why this priority**: 用户要求 1(3)(4) 的直接交付物；消除"文件副本 ↔ Mongo store"双写一致性的维护成本（当前 copy-then-patch 双写路径见 `common/js/dsh-plugins/preset-authoring/src/materialize.ts`），并消除专有可写路径。

**Independent Test**: preset CRUD 后删除/清空任何磁盘副本（或重启 Pod），物化（UpdateTeam）依然成功且成员 system prompt 的 persona 与 store 记录一致；全流程（创建→物化→对话→编辑→再物化）大型测试通过；代码检索确认不存在"为一致性而维护副本"的路径。

**Acceptance Scenarios**:

1. **Given** 用户经 web/API 创建/编辑 preset，**When** 操作成功，**Then** 仅 store 记录变更，不存在需要与 store 对账的持久组合文件。
2. **Given** store 中存在 preset 记录但磁盘副本缺席（如 Pod 重建后），**When** 物化引用该 preset 的 team，**Then** 物化成功，成员生效的 persona 与插件组合与 store 记录一致（派生是使用时的纯函数，幂等可重放）。
3. **Given** 派生机制需要落文件（如官方 roster 挂载面要求），**When** 物化发生，**Then** 文件生成于系统临时目录、用完即可废弃，任何时刻删除该文件都不影响 store 数据与后续物化正确性。
4. **Given** 部署清单与容器规格，**When** 变更完成，**Then** 不存在 preset 专用可写路径声明（临时产物走系统临时目录语义）。
5. **Given** 模板 preset（player/planner 池模板，镜像内数据），**When** 物化，**Then** 模板仍作为部署产物分发（打包数据而非运行时维护文件），行级校验（templateRules）语义不变。

---

### User Story 3 - 仓库通用常量库（Go + JS） (Priority: P2)

在 common 目录下新增一个常量库（Go 包 + JS 包，对齐 `common/gopkg/*` 与 `common/js/*` 的既有包形态），收集仓库通用常量；首批收录 deploy 平台保留环境变量名（含本次新增的产物位置变量）。本次将引用方切换到常量库的范围：deploy 工具与服务（保留变量的注入实现与校验）、`projects/game/` 下使用这些常量的服务（如 agent_v2 的 `DOMINION_ENVIRONMENT`/`DOMINION_SECRET_DIR` 消费点）。

**Why this priority**: 用户要求 2 的直接交付物；常量名跨 deploy 工具、基础设施服务与业务服务散落重复定义（如 DOMINION_ENVIRONMENT 同时出现于 `projects/infra/deploy/runtime/k8s/builder.go`、`common/gopkg/*`、`common/js/resolver`、`projects/game/agent_v2/src/presets.ts` 等），单一事实源消除拼写漂移风险，并使新增保留变量的采用成本最小。

**Independent Test**: 常量库存在且含全部保留变量名；指定范围内字面量定义被替换为常量引用（代码检索断言范围内零散落字面量）；全仓构建与测试通过。

**Acceptance Scenarios**:

1. **Given** deploy 工具与服务的保留变量注入实现，**When** 切换完成，**Then** 变量名引用自常量库（Go 侧），注入行为不变（既有 env 数量/顺序断言测试不回归）。
2. **Given** `projects/game/` 下消费保留变量的服务代码，**When** 切换完成，**Then** 对应字面量改为经 JS/Go 常量库引用，行为不变。
3. **Given** 后续新增保留变量（本次的产物位置变量），**When** 常量库收录，**Then** 各使用方一次引用即可获得（本次即以该变量为首个新增实践）。

---

### User Story 4 - webUI 实时流修复：用户输入实时可见与工具结果即时更新 (Priority: P1) 🎯 MVP

修复 team 流实时呈现的两个缺陷：(a) 发送消息启动工作流时，正在输出的成员（如 planner）的视角视图不呈现触发本轮的用户输入——用户输入只经刷新/回合完全静止后的成员视角回填（ListMemberMessages）修正，实时流中"丢失"；(b) 工具调用完成后其结果与终态状态不及时更新——同样只经刷新/静止后的 history 修正。修复后：用户输入在被成员消费驱动时实时出现在该成员视角视图；工具结果与终态在工具完成时随流事件即时呈现（无需等待回合结束或刷新）。刷新与静止后的回填修正行为（现状收敛路径）保持不变。

**Why this priority**: 用户要求 3 的直接交付物；实时流正确性是对话可用性的核心——两缺陷都要求用户刷新页面才能看到真实状态，违背流式体验预期。用户推测为流数据丢失，根因定位（服务端帧缺失/时序/前端归约）在 plan 阶段完成，spec 约束可观测终态行为。

**Independent Test**: 大型测试（fake-llm + fake-desktop）断言流帧序列与前端归约：成员视角在成员回合开始消费用户消息的同时呈现该输入；每次工具结果帧到达后前端对应工具块即时到达终态（含结果文本）；既有回填/断开重连/多流去重回归用例全量通过。

**Acceptance Scenarios**:

1. **Given** team 已物化静止，**When** 用户发送第一条消息触发 planner 回合，**Then** planner 视角视图实时依次呈现：该用户输入（user 气泡）→ planner 流式输出；无需等待回合结束。
2. **Given** 成员回合进行中其他成员的视角视图，**When** 该回合产出，**Then** 其他成员视角仍不实时显示该回合产出（消费前不出现，既有语义不变），仅团队视图与产出者自身视角实时呈现。
3. **Given** player 回合中一次工具调用完成，**When** 工具结果事件到达，**Then** 团队视图与 player 视角的该工具块立即由执行中转为终态（成功/失败）并显示结果内容；后续步骤继续流式。
4. **Given** 流断开或并发多流，**When** 修复后，**Then** 既有收敛语义（断开投影、List 回填补齐、按锚去重）不回归。
5. **Given** 回合结束/静止，**When** 成员视角回填执行（现状路径），**Then** 回填结果与实时呈现一致（实时修复不引入实时与回填的分叉）。

---

### User Story 5 - team 状态呈现：当前激活成员与 system prompt 主界面入口 (Priority: P2)

team 信息增加说明**当前激活的 agent**：team 查询面（GetTeam）暴露**单一"当前激活成员"值**——成员回合在途时为该回合的驱动成员（任一时刻至多一个），静止时为下一条输入归属的成员（activation，物化后初始为 planner）；web 对话页实时可见当前激活成员（流式期间以成员事件帧推导，静止时以查询面为准）。同时，成员 system prompt 的查看入口从"设置 team"面板（`projects/game/web/frontend/src/components/TeamSettingsPanel.tsx`）提升到对话主界面直接可见（每个成员一个入口，内容仍为 GetTeamMember 的 output-only `system_prompt` 全文，语义不变）。

**Why this priority**: 补充需求 3 与用户要求 4 的直接交付物；激活成员是理解 team 编排行为（"现在谁在处理"）的关键观测面，编排层已持有该状态（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 的 activation/drivingMember）仅未外露；system prompt 入口已实现但不可发现（藏在设置面板内）。

**Independent Test**: 物化后查询 team 显示激活成员为 planner；用户首驱后流转至 player 期间 UI 实时显示 player 激活；取消/静止后显示当前 activation。对话主界面（不打开设置面板）可查看两个成员的 system prompt 全文，内容与实例生效提示词一致（既有 FR-016 语义）。

**Acceptance Scenarios**:

1. **Given** team 物化完成静止，**When** 用户查看 team 状态，**Then** 当前激活成员显示为 planner（初始 activation，等待首条用户消息）。
2. **Given** 多局循环进行中（player 回合在途），**When** 用户查看对话页，**Then** 激活成员实时显示为 player；终局后 planner 复盘期间显示为 planner（与编排切换同步，无需刷新）。
3. **Given** 用户执行取消后 team 静止，**When** 查看状态，**Then** 显示当前激活成员（静止时即下一条输入归属成员，取消不改变该归属语义）。
4. **Given** team 已物化，**When** 用户在对话主界面（不打开设置面板）点击某成员的 system prompt 入口，**Then** 该成员当前生效的完整 system prompt 全文可见，刷新 team 后内容随新配置更新。
5. **Given** team 未物化，**When** 查看对话页，**Then** 无激活成员呈现（未物化引导态不受影响）。

---

### User Story 6 - 广播消息净化：移除 think 与 wire 格式去重 (Priority: P2)

成员间 1:1 广播的消息内容不再包含思考（think/reasoning）内容——广播单元只承载正文发言与工具调用/结果全文（当前 `common/js/dsh-plugins/team/src/broadcast.ts` 的 messageBody/toolResultBody 把 reasoning 块一并拼入正文）。同时广播 wire 格式收敛为单一标注形态：发送者标注不重复正文——`[角色]` 头行或 `<角色-message>` 标签对二者择一（Clarifications 补充 2），消除当前"头行摘要复述正文开头"的重复；team section 的广播格式约定同步更新。

**Why this priority**: 用户要求 5 与补充 2 的直接交付物；think 内容对接收方成员是无协作价值的 token 负担（正文与工具事实才是群聊信息面），格式重复同样浪费 token 且干扰阅读。

**Independent Test**: 构造含 think + 正文 + 工具调用的成员输出，断言广播注入文本：不含任何 reasoning 内容、正文恰好出现一次（无头行复述）、工具调用的参数与结果全文保留（1:1 原样语义不回归）；接收方成员驱动的输入历史符合新格式；fake-llm 夹具与大型测试断言按新格式更新后全量通过。

**Acceptance Scenarios**:

1. **Given** 成员输出含 think 块与正文，**When** 输出广播至其他成员，**Then** 其他成员收到的注入消息只含正文（think 内容完全缺席），正文无重复。
2. **Given** 成员产出工具调用与结果，**When** 广播，**Then** 工具调用的完整参数与结果全文原样到达（不截断、不摘要——1:1 原样语义保持），标注形态与发言形态一致（同一择一规则）。
3. **Given** team section（全员共享的团队级提示词），**When** 格式变更，**Then** 广播格式约定描述与新 wire 格式一致（成员被明确告知如何解读广播）。
4. **Given** 成员视角视图中的广播注入条目（`user: [sender] 正文`），**When** 呈现，**Then** 正文无重复内容（wire 格式去重后成员视角显示同步净化）。

---

### User Story 7 - 提示词分层：玩法进 saolei-loop，工具守则仅剩用法 (Priority: P2)

游戏玩法与可用操作说明的所有权移至 saolei-loop 插件：saolei-loop 以 prompt section 声明扫雷玩法（目标、棋盘、数字含义、输赢判定）与可用操作（操作集合对齐 saolei 插件实际实现的工具能力，取两者交集），该 section 对全部成员生效——player 与 planner 的 system prompt 都包含玩法与操作说明（当前玩法说明混在仅 player 可见的 `saolei:guidance` 与 preset persona 中，planner 无游戏规则输入）。玩法说明内容以权威来源为依据（旧版经典扫雷 / Win98 时代的 Microsoft Minesweeper：https://en.wikipedia.org/wiki/Microsoft_Minesweeper 与 https://en.wikipedia.org/wiki/Minesweeper_(video_game) ）。相应收缩：saolei 插件的工具守则仅保留工具使用方法（调用方式、参数形态、结果文本格式、校验与拒绝语义、错误处理），不再包含游戏玩法与操作对游戏影响的说明；preset 模板 persona 不再重复玩法/操作描述（身份与职责为主）。

**Why this priority**: 用户提示词优化 1/2 的直接交付物；提示词所有权单一（玩法归 loop、用法归工具行、身份归 persona）：planner 获得规则输入才能制定可执行策略与准确复盘，player 不因 persona 与工具守则重复陈述而浪费 token。

**Independent Test**: 物化后断言两个成员的 system prompt：均含玩法与操作 section（内容与 saolei 工具能力对齐、与权威玩法一致）；`saolei:guidance` section 仅含工具用法（无玩法陈述）；persona 无玩法/操作重复。fake-llm 的 system_keywords 匹配面与大型测试对话夹具同步更新后全量通过。

**Acceptance Scenarios**:

1. **Given** team 物化完成，**When** 查看 planner 成员的 system prompt，**Then** 含玩法与可用操作说明（此前 planner 无此输入），内容与 saolei 工具实现的操作能力一致（无未实现的操作、无遗漏已实现操作）。
2. **Given** 同一 team，**When** 查看 player 成员的 system prompt，**Then** 含同一玩法与操作 section（同源同文），且 `saolei:guidance` 部分仅陈述工具用法（如何调用、结果格式、拒绝语义），不含玩法规则陈述。
3. **Given** 玩法说明文本，**When** 审阅，**Then** 规则表述与权威来源一致（经典扫雷：揭示非雷全部格子获胜、数字为相邻雷数、空格级联展开、右键标旗、双击(chord)展开、踩雷即负）。
4. **Given** preset 模板的 persona，**When** 审阅，**Then** 只含身份与职责陈述（第一人称角色声明），不含玩法/操作描述（无重复定义）。
5. **Given** 提示词变更后的完整对话链路，**When** 大型测试执行（fake-llm 双角色夹具按新提示词匹配），**Then** 多局闭环全量用例通过（夹具关键词随提示词分层同步调整）。

---

### Edge Cases

- **打包布局变化**：产物放置目录由平台注入而非部署清单硬编码，镜像布局调整不再要求逐服务改部署清单（本 feature 的动机之一）；产物位置变量缺失时服务侧 fail-loud（boot 期明确报错，对齐 roster 根解析失败的既有 fail-loud 语义）。
- **store 与派生的一致性**：派生是使用时从 store 记录的一次性读取，preset 编辑后已物化成员不受影响（既有"物化内容固化"语义不变），再次物化取新值。
- **临时文件清理**：若派生机制落临时文件，其为纯临时产物——生命周期由进程/容器承载（系统临时目录随容器销毁，不承诺进程内清理），任何时刻删除不影响 store 数据与后续物化正确性（重复物化幂等重建）。
- **保留变量与用户 env 冲突**：新产物位置变量按既有保留名语义处理（用户同名声明被平台值覆盖，注入顺序对齐现状——平台保留变量追加在用户 env 之后）。
- **流断开与实时修复的交互**：实时呈现修复不改变断开收敛路径（本地投影 + List 回填对齐）；断开后重连页面按回填呈现（含用户输入与工具终态），与实时路径一致。
- **广播格式变更的兼容**：wire 格式与 think 移除是服务端渲染变更，无存量数据迁移（team 为进程内存态，重启即新格式）；依赖广播锚点文本的夹具/断言同批更新，不存在新旧格式并存窗口。
- **激活成员呈现的静止语义**：静止时显示 activation（下一条输入归属成员），非"最后一个回合的成员"；编排失败/暂停（取消）后 activation 保持、呈现不回退为未知。
- **玩法提示词与工具描述漂移**：操作说明与 saolei 工具能力取交集的要求是持续约束（提示词分层后玩法 section 与工具守则各自演化时不得重新引入对方内容）。
- **常量库范围控制**：本次仅切换 deploy 工具/服务与 `projects/game/` 使用方；common 既有公共包不改为引用新库——它们自身是其领域常量的权威来源，机械式收集/改引反而制造冗余（Clarifications 2026-09-11 裁定的目的表述：一致性、避免冗余，非机械收集）。

## Requirements *(mandatory)*

### Functional Requirements

**部署配置与产物路径**

- **FR-001**: deploy 平台 MUST 为每个 artifact 服务注入声明产物放置位置的保留环境变量（当前不存在，本 feature 新增）：值为该服务产物在容器内的实际放置目录（`/dominion/{app}/{service}` 布局）；该变量名 MUST 纳入平台保留变量清单（deploy 文档与校验保留名列表），用户 env 同名声明被平台值覆盖，注入语义对齐既有保留变量。
- **FR-002**: agent-v2 的 preset 模板根 MUST 默认由 FR-001 的产物位置变量推导（默认推导在服务代码内完成），本地/测试形态 MAY 以显式环境变量覆盖（既有宿主注入模式）；`projects/game/deploy.yaml` 与 `projects/game/testplan/` 下部署清单 MUST NOT 声明 preset 模板根或可写根路径（默认设置省略）；产物位置变量缺失且无显式覆盖时 boot MUST fail-loud。
- **FR-003**: 用户创作 preset 的组合文件副本 MUST NOT 作为被维护的持久产物存在：preset CRUD MUST 只写 store（Mongo，唯一事实源）；成员物化消费的 preset 组合 MUST 在使用时从 store 记录派生——优先在内存中直接组装生效；若挂载机制必须文件，则文件 MUST 为纯临时产物（使用时生成、落系统临时目录、不维护与 store 的一致性、删除后可幂等重建）。池模板 preset（镜像内数据）保持部署产物分发形态与行级校验（templateRules）语义不变。

**仓库通用常量库**

- **FR-004**: common 目录下 MUST 新增仓库通用常量库，同时提供 Go 包与 JS 包（对齐 `common/gopkg/*`、`common/js/*` 既有包形态与命名约定）。收录原则（2026-09-11 用户裁定）：目的是**常量一致性与避免冗余，并非机械式收集常量**——仅收录跨领域、尚无既有权威来源的仓库级常量；已是公共库的包自身是其领域常量的权威来源（如 common 下其他公共库自有的常量定义），MUST NOT 重复收录。首批收录 deploy 平台保留环境变量名全集（含 FR-001 新增变量）。
- **FR-005**: deploy 工具与服务（保留变量注入实现与校验）以及 `projects/game/` 下消费这些常量的服务 MUST 改为经常量库引用对应常量名（消除指定范围内的散落字面量定义）；common 既有公共包内部的同类定义本次不改（它们自身即是领域常量的权威来源，Clarifications 2026-09-11 裁定——避免为一致而制造冗余引用）。

**webUI 实时流修复**

- **FR-006**: 成员视角视图 MUST 在该成员消费用户输入时实时呈现该输入（与成员回合的流式输出同流到达，无需等待回合结束/刷新/回填）；其他成员在被驱动消费前的视角视图 MUST NOT 实时出现该输入（既有"消费前不出现"语义保持）。
- **FR-007**: 工具调用的终态（成功/失败）与结果内容 MUST 在工具完成时随流事件实时呈现（执行中的工具块在结果事件到达时即时终态化）；流帧不得缺失导致前端只能依赖回合结束/刷新/回填修正工具状态。修复 MUST NOT 改变既有收敛语义（断开投影、List 回填、并发流按锚去重）。

**team 状态与可观测性**

- **FR-008**: team 查询面 MUST 暴露单一"当前激活成员"值（2026-09-11 用户裁定，不拆分两个概念）：成员回合在途时为该回合的驱动成员（任一时刻至多一个），静止时为下一条输入的归属成员（activation；物化后初始为 planner，取消/静止不改变归属）；web 对话页 MUST 呈现当前激活成员，流式期间与编排切换实时同步（无需刷新）。
- **FR-009**: web 对话主界面 MUST 为每个成员提供查看其当前生效完整 system prompt 的直接入口（不要求打开 team 设置面板）；内容与实例实际使用的系统提示词一致（既有 GetTeamMember output-only 语义复用）。

**广播消息净化**

- **FR-010**: 成员间广播的消息内容 MUST NOT 包含思考（reasoning/think）块内容；广播单元 MUST 仅承载正文发言与工具调用（完整参数与结果全文，1:1 原样语义保持）。
- **FR-011**: 广播 wire 格式 MUST 采用单一标注形态——发送者标注不重复正文：`[角色]` 头行（无摘要复述）与 `<角色-message>` 标签对二者择一（工具调用单元遵循同规则的对应形态，具体择一由 plan 决定并在全链路统一）；team section 的广播格式约定 MUST 与最终格式一致；依赖广播文本锚点的测试夹具与断言 MUST 同批更新。

**提示词分层**

- **FR-012**: saolei-loop 插件 MUST 提供"游戏玩法 + 可用操作"的 prompt section，对 team 全部成员生效（player 与 planner 的 system prompt 均包含）；玩法说明 MUST 以权威来源为依据（经典扫雷：https://en.wikipedia.org/wiki/Microsoft_Minesweeper 、https://en.wikipedia.org/wiki/Minesweeper_(video_game) ）；可用操作说明 MUST 与 saolei 插件实现的工具能力对齐（交集：不声明未实现的操作，不遗漏已实现操作）。
- **FR-013**: saolei 插件的工具守则（`saolei:guidance`）MUST 仅包含工具使用方法（调用方式、参数形态与互斥规则、结果文本三层结构、符号/坐标读法、校验与拒绝语义、错误处理）；MUST NOT 包含游戏玩法规则与操作对游戏影响的说明。
- **FR-014**: preset 模板 persona MUST NOT 包含玩法与可用操作描述（仅身份、职责与风格；玩法归 FR-012 的 loop section——提示词所有权单一）。

### Key Entities

- **产物位置保留环境变量**（新）：deploy 平台注入的声明 artifact 放置目录的保留变量；服务侧推导一切打包内数据路径（如 preset 模板根）的锚点。
- **常量库（const lib）**：common 下 Go + JS 双语言包，**跨领域、无既有权威来源**的仓库级常量单一事实源（目的：一致性与避免冗余，非机械收集）；首批内容为平台保留环境变量名；已是公共库的包保持其领域常量的权威来源地位。
- **preset store 记录**（唯一事实源）：用户创作 preset 的全部持久事实（role、persona、模板来源等）；物化组合是它在使用时的派生视图（内存组装或临时文件），不再是需要维护的对账对象。
- **广播 wire 格式**：成员间 1:1 转发消息的注入文本形态（单一标注 + 原样正文/工具事实，无 think）；team section 中向全员声明的解读约定与之同源。
- **编排激活状态**：编排层持有的"当前激活成员"判定事实——回合在途的驱动成员（driving，任一时刻至多一个）与静止时的下一条输入归属成员（activation），对外呈现为单一合并值（2026-09-11 裁定）；team 查询面与 web 呈现的观测对象。
- **玩法提示词 section**（saolei-loop 所有）：游戏玩法与可用操作说明（权威来源 + 工具能力交集），全员生效；与工具守则（用法）、persona（身份）构成三层所有权边界。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 生产与 testplan 部署清单中 preset 路径环境变量零残留（代码检索断言）；部署后 agent-v2 boot 成功、模板 preset 可列出与物化；产物位置保留变量存在于全部 artifact 服务容器 env 中且被列入保留名清单。
- **SC-002**: preset 全生命周期（创建/编辑/删除/物化/重启后物化）仅依赖 store：磁盘副本缺席（Pod 重建/手动清理）不影响任何行为；物化成员的 system prompt persona 与 store 记录一致（大型测试断言）。
- **SC-003**: 常量库存在（Go + JS）且收录全部保留变量名；deploy 工具/服务与 `projects/game/` 使用方范围内零保留变量名字面量散落定义（代码检索断言）；全仓构建与测试通过。
- **SC-004**: 实时流行为（大型测试 + 手工验证）：用户发送消息后，被驱动成员的视角视图即时呈现该输入；每次工具完成后其状态与结果即时更新（不等待回合结束/刷新）；既有回填、断开收敛、多流去重回归用例全量通过。「实时/即时」的验收口径 = 对应帧到达后前端即呈现（以流帧序与前端归约断言验证），不设绝对时限。
- **SC-005**: 广播注入文本（大型测试断言）：不含任何 think 内容；正文恰好出现一次（无头行复述）；工具调用参数与结果全文保留；fake-llm 夹具与大型测试按新格式全量通过。
- **SC-006**: 提示词分层（system prompt 断言）：player 与 planner 均含玩法与操作 section（内容与权威玩法一致、与工具能力对齐）；`saolei:guidance` 无玩法陈述；persona 无玩法/操作重复；多局闭环大型测试全量通过。
- **SC-007**: team 状态呈现：查询面暴露激活成员；web 对话页实时显示当前激活成员（物化后 planner、player 回合期 player、复盘期 planner）；主界面可直接查看成员 system prompt 全文。

## Assumptions

- **上游折叠设计确认结论**（Clarifications 补充 1）：运行中不出现"思考过程"折叠、回合结束后折叠，均为上游 dsh-web 设计预期（https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/archived/feature/2026-08-14-web-turn-process-folding.md ），本 feature 不修改该行为。
- **roster 模板保留文件形态**：池模板（`projects/game/agent_v2/preset-templates/`）是镜像内打包数据（部署产物），不是运行时维护文件，保留现状；FR-003 仅消亡"用户创作副本的可写根维护"。
- **机制细节留给 plan**：新保留变量的命名、注入点（k8s builder）与文档面更新；preset 派生的具体机制（内存行挂载 vs 临时文件）——两者均不改变 FR 的终态行为；广播格式二选一的具体选择。
- **webUI 缺陷根因在 plan 阶段定位**：spec 只约束可观测终态行为（FR-006/FR-007）；根因候选包括服务端帧缺失/时序（`projects/game/agent_v2/src/history.ts` 的 tool_result/用户消息消费帧）与前端归约路径（`projects/game/web/frontend/src/store/chat.ts`）。
- **测试基建联动**：fake-llm 夹具（`projects/game/fake-llm/service/testdata/team_*.yaml` 的广播锚点与 system_keywords）、testplan 断言随广播格式与提示词变更同批更新；大型测试继续以 fake LLM + fake desktop 支撑确定性验证（constitution 原则 VI）。
- **deploy env 为纯字符串**（无变量插值，`tools/release/deploy/pkg/schema/deploy.schema.json`）：路径推导发生在服务代码内（FR-002），部署清单不出现派生表达式。
- **范围边界**：本 feature 不改变 team 编排语义（交替激活/续驱/取消）、preset 分池与角色锁定、memory 机制、compact 排除等 059 既有裁定；common 既有包内部常量定义的收敛不在本次范围。
