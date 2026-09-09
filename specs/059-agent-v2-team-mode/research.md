# Research: Agent v2 Team 模式迁移

> **状态**：Phase 0 完成。全部技术决策已定（无 NEEDS CLARIFICATION 残留）；决策依据以四份前期调研为一手来源，本文记录**采纳结论 + 理由 + 备选**，并承载 plan 阶段的新决策（R2/R3/R7/R8 等）。
> **一手调研**：`survey/deepseek-harness-team-mode.md`（下称 team-mode）、`survey/deepseek-harness-memory-plugin.md`（下称 memory-plugin）、`survey/deepseek-harness-roster-verification.md`（下称 roster-verification）、`survey/deepseek-harness-agent-loop-prereq.md`（下称 prereq）。

---

## R1 总体架构：双顶层 agent + team 层群聊模型（采纳调研基线）

- **Decision**：player/planner 为两个对等的顶层 agent（各持独立 session log、persona、system prompt）；群聊抽象为独立 **team 插件**（`common/js/dsh-plugins/team/`，只投递不驱动）；**saolei-loop 升为 team loop 编排层**（驱动权唯一归属），agent 驱动回归官方 `dsh-agent-loop` 行。
- **Rationale**：team-mode §6/§9 已拍板（五轮用户确认，2026-09-08）：双 agent 与模型更契合；官方 loop 依赖面对扫雷场景几乎无冗余；saolei-loop 不再 `setFactory`，prereq §4.7 的 8 项自研驱动重写清单（turn/step 状态机、abort 检查点、中断流落日志、wake latch、四决策点 waterfall、driver containment、工厂所有权、resume 冷恢复）随官方行回归而全部消解，prereq §4.6 的自研 invariant 伴生不再需要（`team-broadcast` 是 `MessageSourceMap` 的 merge 扩展，不触发 request-reconstruction 校验，team-mode §9.3）。
- **Alternatives**：①单 agent 双角色（2026-08-28 决策，被 team-mode 头部决策 ③ 取代）；②planner 作为 subagent（排除——child 输入由 parent 委派请求决定，压缩失真且目标被 player 左右，team-mode §3.4）；③保留自研 agent loop 层（saolei-loop 现状，`setFactory` 替换官方行）——被拓扑翻转淘汰：驱动不再有 saolei 特化需求（team-mode §9.3 对照表），保留即承担 0.x-rc 逐版对照的复制维护成本。
- **引用**：team-mode §4/§6/§9.3；prereq §2/§3/§4。

## R2 角色身份注入归属：preset persona 承载（plan 阶段新决策，2026-09-09 用户委托裁定）

- **Decision**：agent 的角色身份由 **preset 的 persona 文本**承载（第一人称："你是扫雷 player：…职责…"）；team 插件的 team section 只承载**团队级事实**（团队目标、成员名册、广播格式约定，均为第三人称、内容同源统一渲染给全体成员）。防重复定义的边界规则：
  - persona MUST NOT 包含团队级事实（团队目标、成员名册、广播格式约定）；
  - team section MUST NOT 包含成员第一人称身份指向（"你是 X"）；
  - 名册中每成员只写**一句话级职责摘要**（供其他成员理解队友，索引角色），详细职责与人格在各自 persona（详情）。
- **Rationale**：① **所有权单一**（dsh "every fact in the prompt has exactly one owner"，`survey/deepseek-harness-preset.md` §6.4）：身份是 per-member 内容，per-member 内容的第一归属是 preset（池即角色）；team 级内容归 team section——按"内容的受众范围"切分是最干净的所有权边界。② **persona 空值回退完整**：persona 留空回退默认 base（既有语义），默认 base 天然自带身份声明（现状 `DEFAULT_PLAYER_BASE` 同型）；若身份放 team section，默认 base 与 team section 将重复声明身份。③ **team section 保持同源渲染**：身份放 team section 需按成员差异化渲染（每成员看到"你是不同的 X"），破坏"内容同源统一渲染"的简洁性（team-mode §4.5）。④ **调研基线形态**：team-mode §5.3 的 persona 示例即以"你是扫雷 player"开头，名册是第三人称事实。⑤ 编排是结构性的（谁被驱动由 saolei-loop 决定，不依赖 prompt 中的身份声明），身份在 persona 中可被用户编辑是特性而非风险。
- **Alternatives**：team 插件承接身份注入（per-member 差异化渲染 team section，内容由上层编排插件提供）——机制可行但引入 per-member 渲染变体、与默认 base 回退语义冲突、且身份与人格分离后 persona 撰写别扭（提示词惯例以身份开头）。被否决。
- **引用**：team-mode §4.5/§5.3；`survey/deepseek-harness-preset.md` §6.4；本决策由用户于 plan 阶段提出选项并委托裁定（"两种方式都行，但要明确你选择的方案，避免重复定义"）。

## R3 preset 体系：roster 机制落地 + Mongo Store + 分池模板（采纳实证路径）

- **Decision**：agent_v2 组合清单挂官方 `dsh-agent-presets` roster 行（`roots: [player 池, planner 池]` 两个目录、不设 default——preset 必选）；preset 创作/编辑面复用 058 的 `@dominion/dsh-preset-authoring` 插件（copy-then-patch：模板 preset 拷贝后仅 patch persona 行），其 `PresetStore` seam 增加 **Mongo 实现**（沿用 `game_agent_v2.presets` 集合演进，记录增加 role 与既有时间戳），经 `Config.storage` 切换；物化接线改为 `compose()` 形态（resolve 提前 + setup 内 mount，`meta.agentPreset` 落 session header）。**两个池的模板 preset 分别内置角色工具插件行**：player 模板含 `@dominion/dsh-saolei` 行，planner 模板含 `@dominion/dsh-memory` 行——行级选择保证工具与配套 guidance 同生共死（roster-verification V2-3 实证）。现有 `AgentOptions.persona` declaration-merge 注入路径（`common/js/dsh-plugins/saolei-loop/src/index.ts:67-71`、`projects/game/agent_v2/src/session.ts:296`）废弃。
- **Rationale**：roster-verification §2 已实证 B1 直组形态下 roster 全部机制点（分池 roots、standing mount、generation、行级一致、copy-then-patch、compose 接线，V1–V4 全通过）；§4.2 给出 agent_v2 迁移路径要点（闭包三面原子变更、roster 行 + authoring 插件复用、compose 接线、Mongo Store seam 同型）。memory-plugin 决策 ⑥ 的路径 A 前提（roster 可行性）由 058 消解。
- **Alternatives**：扩展现状 Mongo 单字段 `PresetRecord`（加 role + 保持 `player_prompt`）——无法承载工具插件行（无 per-session 组合差异、无行级一致语义、无 generation/authoring），与"preset 选择插件行"的调研决策 ⑭ 冲突。被否决。
- **实现注意**（来自实证，tasks 阶段必须遵守）：① 组合三面（package.json ⟷ cordis.yml ⟷ tar 物化）闭包审计要求**原子变更**（roster-verification §5 对照 3、`experimental/dsh/demo/testplan/closure_audit_test.go`）；② workspace 包经 `runtime_deps` 进打包闭包（`npm_deps` 对 workspace 包是 no-op，§5 对照 2）；③ 物化副本丢失语义——部署无用户卷通道（§5 对照 1），接受"store 为 source of truth、副本可从 store 记录重建"；④ roster 已知限制：superseded generation 不回收、root 扫描无 watch——demo 规模无感知（§7 风险 2）。
- **引用**：roster-verification §2/§3/§4.2/§5；`experimental/dsh/demo/agent/cordis.yml`、`experimental/dsh/demo/agent/src/session.ts`（`doCreate()` compose 接线样板）、`common/js/dsh-plugins/preset-authoring/src/`（复用本体）；team-mode §3.2/§3.6。

## R4 memory 插件：两功能面 + 存储沿用（采纳 memory-plugin 决策 ①–⑧）

- **Decision**：新增 `@dominion/dsh-memory` 插件（`common/js/dsh-plugins/memory/`），两个功能面：① **memory 单工具**（add/replace/remove + `operations[]` 批量原子、`old_text` 子串定位、无 read 动作、失败也是文本结果不中断对话——v1 `projects/game/agent/src/mcp/memory/memory-mcp.ts` 的 schema/语义原样迁移，description 按快照新语义改写）；② **system prompt 快照 section**（函数式 `text: (context) => 快照缓存.get(context.scope) ?? ""`，order 200+，空自动不渲染）。挂载 = planner preset 的 memory 插件行（工具 + section）+ host 层服务面（`load(scopeKey, (template, session))` 预取 + gRPC client 访问 memory 服务）；预取由 saolei-loop 物化 planner 的 setup 显式调用；**首读失败 = setup throw = 物化整体回滚**（fail-loud）。存储沿用 memory Go 服务（`dominion:///game/memory:50051`，client 迁移自 v1 `projects/game/agent/src/memory-client.ts`），scope 键 (template, session)。不注册 guidance section（单工具无跨调用协调需求，memory-plugin §4.2）。
- **Rationale**：memory-plugin 决策 ①②③④⑤⑥⑦⑧ 全量（功能对齐 v1、两功能面、快照生命周期固定、广播可见性已接受、存储沿用、路径 A、scope 键、fail-loud）；工具与 guidance 一致性由行级选择天然保证（无 guidance 即无一致性问题）。
- **Alternatives**：路径 B（loop attach，agent-scoped 注册）——记录为机制等价回退（memory-plugin §5.3）；进程内嵌入存储/MCP 形态——已排除（memory-plugin §7）。
- **引用**：memory-plugin 全文（§3–§9）；v1 迁移样板 `projects/game/agent/src/mcp/memory/memory-mcp.ts`、`projects/game/agent/src/memory-client.ts`。

## R5 team 插件：群聊原语契约（采纳 team-mode §4.4/§4.5/§5.3 设计基线）

- **Decision**：`@dominion/dsh-team` 提供 `ctx.team` 服务，场景无关：
  - `register({ goal, members: [{ agent, role, summary }] })`——注册即 team section 注入（经各成员 `agent.ctx` 注册，order 1–49 频段：persona(0) 之后、工具守则(100–199) 之前；内容由注册参数渲染：目标 + 名册（每成员一句话职责摘要，R2 边界）+ 广播格式约定）+ 成员输出订阅生效；
  - 成员输出收集：订阅成员 `session/event`，收集 `assistant/message`（发言）与 `tool/call`+`tool/result`（按 callId 配对为一条广播单元）；
  - 1:1 原样广播语义：工具调用完整 args 与 result 全文（wire 序列化差异不算；不摘要不聚合）；只有发送者标注、无收件人定向；广播格式（`[sender] 摘要` + 标签对包裹）在 drain 构造时渲染；
  - buffer：per-member **待消费引用列表**（有序 messageId/callId 锚点，**不复制消息内容**）——广播动作 = 将锚点追加进除发送者外每个成员的待消费列表（不写 agent log，决策 ⑦）；team 不维护全局消息副本队列（若实现存在中转列表，锚点进入全部接收方列表后即移除，不滞留已完成广播的消息）；
  - `drain(member)`：team 内部按锚点从成员 session log（既有读取面）读取实际内容 → 构造注入就绪的广播 UserMessage 返回——**索引与读取是 team 内部逻辑，不对外暴露索引**（编排层拿到即注入就绪的消息，不接触 agent log 与索引细节）；
  - 派生重建（决策 ⑮）：待消费列表可在重建时由「sender log 产出 − receiver 消费锚点（receiver log 中 `source.kind === 'team-broadcast'` 的 `messageId` 集合）」推导；重建即一致、exactly-once 天然成立；顺序：串行驱动下按驱动轮次归并（锚点插入序即事件到达序）；
  - `MessageSourceMap` merge 扩展 `team-broadcast`：`{ kind, role, senderSessionId, messageId, context?, form: 'relay' }`（`context` 泛化关联键，saolei 侧填局 id，team 不理解其语义）。
- **Rationale**：team-mode 决策 ④⑤⑦⑧⑩⑬⑮ + §5.3 四层格式设计 + §4.4a 派生重建算法；投递结构参照 `dsh-subagent` continuation manager（持有 AgentHandle + 投递规则，平级拓扑）。
- **Alternatives**：buffer 独立持久化 + 对账（被 ⑮ 取代——独立事实源必然引入一致性检查义务）；广播直投 agent inbox/广播驱动（被 ⑦ 取代——唤醒张力上移到 loop 驱动策略）；buffer 存广播条目**内容副本**（每接收方一份格式化副本，2026-09-09 plan 阶段用户裁定否决——副本浪费内存且引入副本与 log 的一致性维护面，引用模型下消息内容唯一存在于成员 log，drain 时经既有 session 读取面按锚点现读）。
- **引用**：team-mode §4.4/§4.4a/§4.5/§5.3/§8；[@deepseek-ai/dsh-subagent README（0.1.1-rc.2，unpkg 官方包内容镜像）](https://unpkg.com/@deepseek-ai/dsh-subagent@0.1.1-rc.2/README.md)（结构参照；`listChildren` 直读 live session store 的先例）。

## R6 saolei-loop team loop：交替激活编排状态机（含 clarify session 语义）

- **Decision**：`common/js/dsh-plugins/saolei-loop` 重构为编排层（移除 `driver.ts` 的自研 turn/step 状态机与 `setFactory` 工厂认领）：
  - **物化编排**：`UpdateTeam` → 校验（preset 存在且池匹配角色、model 在目录）→ 逐成员 `ctx.agents.create({ sessionId, agentOptions: {provider, model}, meta: { agentPreset }, setup })`，setup 链 = roster mount（compose 返回）+ **player 专属**：agent-scoped 注册 `saoleiGame`（GameRuntime 从现 factory 迁来，绑定 sessionName/desktopBridge）+ **planner 专属**：`await ctx.plannerMemory.load(...)` 预取快照（fail-loud）→ `ctx.team.register({goal, members})` → 自动开始工作流（驱动 planner 产出开局策略）。两角色 create 调用同构（差异全在 preset 内容，物化零定制）。
  - **交替激活状态机**：任一时刻至多一个成员激活；阶段流转 = planner(开局/复盘) ⇄ player(游戏/轮次)；驱动 = `drain(成员 buffer)` → 构造 UserMessage → `followup()` 注入 → 等待回合结束（`agent/status` idle）。
  - **续驱规则**（spec clarify 裁定）：planner 回合结束后**结构性续驱** player 进入下一轮（是否开局由 LLM 决定）；player 游戏结束（gameEnded，来自其持有的游戏事件流）→ 驱动 planner 复盘。
  - **排队消息优先于切换**：当前激活成员回合结束时若有排队用户消息，先驱动当前成员消化，再执行编排切换。
  - **取消**：终止在途回合 + 暂停续驱；用户再次 Send 恢复（消息进团队流，由当前激活成员处理）。
  - **游戏事件流持有**：GameRuntime（棋盘/规则/操作/胜负判定）留在 saolei 侧（`common/js/dsh-plugins/saolei/src/game/` 迁移归属不变），经 agent-scoped `saoleiGame` 服务暴露；游戏事实与 team 消息流解耦（决策 ⑨）。
- **Rationale**：team-mode §9.3/§9.4（物化与运行流程）；spec Clarifications（2026-09-09 三项裁定）；GameRuntime 归属沿用 051 决策（游戏状态在 saolei 插件侧）。
- **Alternatives**：v1 的 LangGraph 状态机迁移（用户明确"不要过度迁移旧版本方案"——LangChain 驱动模型不适用 dsh，且 v1 的 configurable 标志/内部构建消息机制被"群聊历史直接驱动"取代）。
- **引用**：team-mode §4.2/§9.4；spec FR-009/FR-010/FR-011/FR-017；`common/js/dsh-plugins/saolei-loop/src/`（现状代码）、`common/js/dsh-plugins/saolei/src/game/runtime.ts`（GameRuntime）。

## R7 组合清单变更与 session persistence 取舍（plan 阶段决策）

- **Decision**：agent_v2 `cordis.yml` 变更——**新增**：`dsh-agent-loop`（官方驱动回归）、`dsh-agent-presets`（roster，config 见 R3）、`@dominion/dsh-team`、memory host 行（`ctx.plannerMemory` 服务面）；**保留**：llm-glm、desktop-bridge、saolei（工具行，同时进 player 模板 preset）、saolei-loop（语义变为 team loop）、llm-retry、invariants 三件套；**不引入** `dsh-session-persistence`：agent 为进程内存态（重启后重新物化，既有语义），team buffer 重建从 live log 派生即可；team 注册事实由物化编排重建（重启 = 重新物化 = 重新 register），无需持久化。
- **Rationale**：team-mode §9.1 层 1 基线含 persistence 行，但其存在理由是 buffer 派生重建的跨进程读取面（§4.4a）——本 feature 维持"进程内存态、重启重物化"的既有部署语义（spec Assumptions），persistence 无承载需求；引入反而新增存储面与 resume 语义（saolei-loop 现状 resume 即 fail-loud）。组合三面闭包审计要求本次行变更与 package.json/tar 物化原子交付（R3 实现注意①）。
- **Alternatives**：引入 persistence（为未来断点继续预留）——本 feature 无需求方，YAGNI；留待需要跨进程 team 语义的后续 feature。
- **引用**：team-mode §9.1/§4.4a；`projects/game/agent_v2/cordis.yml`（现状）；`projects/game/agent_v2/README.md`（内存态已知限制）。

## R8 对外 API：agent 单例 → team 单例 + member 子资源（plan 阶段决策）

- **Decision**：`agent_v2.proto` 的 `AgentService` 演进为 team 模型（契约全文见 [contracts/team-api.md](contracts/team-api.md)）：
  - `Agent` 单例资源 → **`Team` 单例资源**（`templates/{template}/sessions/{session}/team`，AIP-156）：`UpdateTeam`（物化/刷新：player_preset/planner_preset 必填 + 各自 model 可选）、`GetTeam`（成员清单 + desktop_connected + 物化状态）；
  - **`TeamMember` 子资源**：`GetTeamMember` 返回含 output-only `system_prompt`（FR-016 查看）；`ListMemberMessages`（成员视角历史，消息带 `sender`/`source_kind` 标注支撑"他人=标注来源的 user"渲染）；
  - **`ListTeamMessages`**：团队视图历史（归并序列，每条标注产出成员；user 消息与成员原生输出）；
  - `Send` 路径不变（`POST /api/v2/{session}:send`，NDJSON 流），**ChatEvent 增加 `member` 字段**（PLAYER/PLANNER）标注产出成员，`queued`/`turn_start`/`turn_end` 等回合级帧标注当前激活成员；
  - `Cancel` 语义按 FR-017 扩展（target 改为 team）；
  - **PresetService**：`Preset` 增加 `role`（PLAYER/PLANNER，create 必填不可变），`ListPresets` 支持 role 过滤（契约见 [contracts/preset-api.md](contracts/preset-api.md)）；
  - `DesktopBridgeService` 不变（session 单位连接、player 独占使用，FR-012）。
- **Rationale**：对齐 AIP-156 单例 + 子资源惯例与既有 `/api/v2` 路由分层（会话面经 proxy、配置面直连）；事件带成员标识是团队视图实时归并渲染的最小扩展；沿用 NDJSON 流（现状消费方式零破坏性概念迁移）。
- **Alternatives**：保留 Agent 资源名仅扩字段（名不符实且 AIP 资源模型混乱）；多 agent 泛化集合（`agents[]` 任意数量）——spec 拍板恰 2 成员、角色固定，泛化是过度设计（原则 II）。
- **引用**：`projects/game/agent_v2.proto`（现状）；[contracts/team-api.md](contracts/team-api.md)、[contracts/preset-api.md](contracts/preset-api.md)；https://google.aip.dev/156。

## R9 web UI：team 模型 + 双视图 + system prompt 查看（plan 阶段决策）

- **Decision**：web 前端（`projects/game/web/frontend/src/`）演进：① AgentSettingsPanel → TeamSettingsPanel（双 preset 下拉按池过滤 + 双 model + Apply=UpdateTeam）；② 对话区视图切换（1 团队视图 + 2 成员视角视图）：团队视图消费 `ListTeamMessages` + member 标注的实时事件归并；成员视角视图消费 `ListMemberMessages` + 该成员的实时事件；③ 成员详情入口查看 system prompt（GetTeamMember）。既有 ChatStore/流式渲染/排队/取消/回填机制按 member 维度扩展。契约见 [contracts/web-views.md](contracts/web-views.md)。
- **Rationale**：FR-013~FR-017；团队视图"原生输出"要求事件与历史按成员归并（非广播包装形态）——数据源是各成员原生事件流；成员视角视图的数据源是该成员 history（含广播注入的 user 消息 + sender 标注）。
- **Alternatives**：团队视图复用广播包装消息渲染（违反 FR-014"不显示转发/包装形态"）。
- **引用**：spec FR-013~FR-017、US4/US5；`projects/game/web/frontend/src/`（现状组件）。

## R10 v1 移除清单（实证盘点，含保留项）

- **Decision**：移除——① `projects/game/agent/` 整目录（v1 服务）；② `pnpm-workspace.yaml` 的 `projects/game/agent` 行 + `pnpm-lock.yaml` importer 段；③ `game.proto` 的 `TeamService`、`PromptService` 及 v1 专属消息（`Team`/`TeamAgent`/`SaoleiProfile`/`UpdateTeamRequest`/`RefreshTeamRequest`/`TeamProfile` 等）；④ `projects/game/prompt/` 整目录（v1 专属配置服务，未部署）；⑤ fake-llm 的 v1 planner 夹具（`projects/game/fake-llm/service/testdata/planner.yaml`、`planner_tools.yaml`——注释引用 v1 源码且关键词匹配 v1 复盘前缀）；⑥ 过期注释（如 `projects/game/agent/service.yaml` 已随目录移除）。
- **保留**（v2 复用或调研拍板）：`SessionService` 与 Session 资源（`/api/v1` 会话管理在用）；`UserFrame`/`TeamFrame`（`DesktopBridgeService.Connect` 复用，`agent_v2.proto:166-174`）；`MemoryService` + memory Go 服务 + gateway `/api/v1` memory 路由（memory-plugin 决策 ⑤）；fake-desktop 的几何公式注释（`projects/game/fake-desktop/service/executor.go:10`——是 fake-desktop 行为实现依据而非 v1 引用，其指向的公式本体随 v1 目录移除后需将公式内联或改写为自包含注释）；`gameconst` 的 `TeamTarget` 命名（承载 v2 proxy 目标，可顺手改名但非必须）。
- **Rationale**：探索报告实证——v1 已退出生产部署与测试拓扑（deploy/testplan 无 `game/agent`），残留仅为代码/协议/夹具/注释；移除边界与 spec FR-001~FR-003、Assumptions 一致。
- **引用**：`projects/game/deploy.yaml:13-62`（生产清单无 v1）；`projects/game/gateway/cmd/main.go`（路由现状）；`projects/game/game.proto`（service 归属盘点）。

## R11 驱动语义细则（spec clarify session 裁定的工程化表述）

- **Decision**（并入 R6 状态机实现）：① 用户消息进团队消息流广播全员，由**当前激活成员**处理；② @标记仅为内容表达，系统不解析；③ 排队消息在当前回合结束后由当前激活成员消化，**消化优先于编排切换**；④ 取消 = 终止在途回合 + 暂停续驱 + 排队消息落地不触发新驱动，再次 Send 恢复；⑤ planner 无指令工具（instruct_player 不迁移），成员间通信经团队消息流（FR-008）。
- **Rationale**：用户在 plan 前 clarify 阶段的补充裁定（2026-09-09，spec Clarifications 第 3 条）；交替激活对齐 v1 驱动形态（player/planner 交替，`projects/game/agent/src/team/graph.ts` 状态机同构），但实现为 dsh 编排层（不迁移 LangGraph）。
- **引用**：spec Clarifications、FR-008/FR-011/FR-017。

## R12 测试策略

- **Decision**：
  - **单测**（每次变更必过，`bazel test`）：team 插件（buffer 派生重建/消费锚点/广播格式/team section 渲染/注册清理）、saolei-loop 编排状态机（交替激活/续驱/排队优先/取消暂停/物化编排含 fail-loud）、memory 插件（工具 schema/批量原子/子串定位/快照 section 时机/fail-loud）、preset authoring Mongo Store、agent_v2 gRPC 面（team 资源校验/错误映射）、web store reducer（member 归并）。
  - **大型测试**（验收，`guitar run` 全量通过）：fake-llm 新增 team 双角色夹具——player 夹具（按 system_keywords 识别 persona、依序调用 saolei 工具直至终局、接收策略广播）、planner 夹具（产出开局策略/复盘正文、调用 memory 工具）；用例覆盖：team 物化与自动开局、完整局至终局、复盘+memory 持久化（经 memory 服务断言）、结构性续驱第二局、排队消息消化优先于切换、取消暂停与恢复、双视图历史断言（ListTeamMessages/ListMemberMessages）、system prompt 内容断言（player 无 memory 痕迹/planner 有）、preset 分池 CRUD 与角色锁定、既有对话能力回归。
- **Rationale**：constitution 原则 IV/VI（单测小颗粒高频、大型测试全量通过验收）；fake-llm 机制（scenario 夹具 + system_keywords）已被 046/047/051/058 实证支撑确定性端到端。
- **引用**：`projects/game/fake-llm/`（机制）；`experimental/dsh/demo/testplan/`（testplan 形态样板）；`specs/058-dsh-preset-roster-demo/checklists/boundaries.md`（边界审计模板可复用于移除验收）。

---

## 风险与缓解（汇总）

1. **0.1.1-rc.2 破坏性变更**：全家桶精确 pin（既有决策）；本 feature 新增依赖官方 `dsh-agent-loop`/`dsh-agent-presets` 行，升级需逐 rc 对照（prereq §7.2 风险 1 同源）。
2. **roster 生产化迁移面**：authoring 插件从 demo 形态服务 agent_v2（Store Mongo 化、组合接线、闭包三面原子变更）——058 已实证路径，按 §4.2 要点执行。
3. **广播压力面**（team-mode §9.5 无先例项 2）：planner 单局 buffer 条目数 = player 工具调用数，drain 一次注入的消息量需在大型测试实测（官方 `agent/pre-step` 批量语义支持）。
4. **buffer 派生重建边界**（无先例项 3）：串行驱动下按驱动轮次归并（team-mode §4.4a）；重建/自愈/不重复由 team 插件单测覆盖。
5. **planner 上下文增长**（compact 排除）：接受为已知限制（spec FR-018/Edge Cases）；token 代价已由调研决策 ⑬ 承担。
6. **组合闭包审计**：新增行/依赖必须 package.json ⟷ cordis.yml ⟷ tar 三面原子交付（roster-verification §5 对照 3）。

## 对 tasks 阶段的输入

- 实现顺序建议：v1 移除（独立清理 slice）→ team 插件 + saolei-loop 重构（核心）→ memory 插件 + preset 体系（roster 迁移）→ agent_v2 API/宿主 → web UI → 大型测试收口。
- 每个 phase 的文档清单（constitution 原则 V 三分类）由 `/speckit.tasks` 生成；本文与 [contracts/](contracts/)、[data-model.md](data-model.md) 是必读输入。
