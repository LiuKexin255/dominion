# Research: agent-v2 对话呈现与游戏链路缺陷修复 + testplan 重构

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-09-03

调研方法说明：dsh 官方 webUI 调研基于 npm 官方包解包实读（`@deepseek-ai/dsh-client-ui-conversation`、`@deepseek-ai/dsh-client-ui-chat` 的 0.1.2-rc.1 与 0.1.1-rc.2 版本、`@deepseek-ai/dsh-api-session-controller@0.1.2-rc.1`、`@deepseek-ai/dsh-client-ui-theme@0.1.1-rc.2`，npm registry 下载 tarball 分析 package.json/README/*.d.ts），源仓库 https://github.com/deepseek-ai/deepseek-harness ；testplan 调研基于 `projects/game/testplan/` 全目录实读。所有结论可由所引材料复核。

---

## D1: 官方 webUI 复用分层（用户指令①核心决策）

**Decision**: 采用**分层复用**——L1 复用 primitives 原子组件（扩展）、L2 复用 ui-theme 的 token CSS 表（新增）、L3 以官方 chat 包行为为对齐基线自研组装（抄设计不抄代码，051 A8 惯例延续）、**L4（完整引入官方 conversation/chat 插件栈）否决**。

**Rationale**（L4 否决的证据链）：

1. **数据面协议不匹配**：官方 UI 包消费 `dsh-api-session-controller` 的 Client 命名空间（session/skills/fileReferences Remote、`SessionEventStream`=Gateway `RemoteJournalStream`、`SessionControlStream`=`RemoteSnapshotStream`；历史为 durable session event 日志分页 + follow 流 + `SessionEventLikeEntry` 记录模型——见该包 README "use this package" 章节）。agent-v2 的服务面是自定义 `/api/v2` NDJSON ChatEvent 流 + 内存态消息 List（051 契约）。直接复用官方 UI 需要在 agent-v2 侧实现整套 session-controller Host 面（或写巨型适配器伪造其 Client 接口），等于重写服务面。
2. **依赖生态不匹配**：`dsh-client-ui-conversation@0.1.1-rc.2` 的 peerDependencies 有 24 个包（dsh-agent、dsh-api-remotes、dsh-client-connection、dsh-client-locale、dsh-client-ui-settings、dsh-client-ui-input-trigger、dsh-commands、dsh-brand、dsh-session-stats、dsh-token-meter、dsh-compaction、dsh-client-runtime、dsh-attachment、dsh-goal、dsh-plan-mode、dsh-tool-todo、dsh-client-ui-layout 等——tarball package.json 实测）。这些概念（goal/plan-mode/todo/attachment/skills）对游戏 agent 域是过度设计（constitution 原则 II）。
3. **版本线特性错位**：0.1.2-rc.1 线有 Turn Process Folding 与 chat 包，但 peer 要求 cordis ^4.0.2（本仓库 4.0.1）且依赖面更大；0.1.1-rc.2 线（与本仓库 dsh 家族精确 pin 对齐）的 conversation 包 README 无 Turn Process Folding（实测 grep 0 命中）且 **chat 包不存在 0.1.1-rc.2 版本**（npm versions 实测：chat 仅 0.0.1 与 0.1.2 线）。不存在"版本对齐且特性齐全"的可用组合。
4. L1/L2/L3 已覆盖本 feature 全部呈现需求：分段折叠=行为对齐（L3）、markdown=primitives `MarkdownText`（L1）、样式缺口=ui-theme token CSS（L2）。

**Alternatives considered**: 完整栈复用（上述否决）；仅手写补 CSS 变量不用官方 token 表（放弃——ui-theme 的 token sheets 是官方"唯一色彩权威"，手补易漏且与组件升级漂移）。

## D2: 与官方 webUI 的不一致清单（用户指令①要求指出）

**Decision**: 逐项列示如下；除特别注明外均按本仓库语义执行，不强行对齐。

| 维度 | 官方 webUI | agent-v2 现状/本 feature | 处置 |
|---|---|---|---|
| Turn/Step 模型 | Turn→Step 两级时间线（`ConversationLocationIndex`） | 服务端历史每 step 一条 HistoryMessage（051 既有） | **一致**，天然对齐（A1 裁定基础） |
| 内容块种类 | `AssistantBlock` = text / reasoning / image / tool-call（`records.d.ts` 实测） | ContentBlock = text / think / toolCall | **一致**（reasoning↔think 同义、tool-call↔toolCall 同义）；image 块 agent-v2 无（GLM 文本模型），无需支持 |
| skill | **不是内容块**：官方 skill 是宿主能力系统（`dsh-skill` 插件、skills Remote、catalog），经 composer slash command 进入 | 无 skill 系统 | 不引入（游戏域无诉求）；用户指令中"skill"确认不存在于 assistant 内容模型 |
| 中断内容 | `'assistant-step'` 节点三态 running/settled/**interrupted**（`chat-nodes.d.ts`） | driver 仅 abort 路径固化 interrupted；ERROR 路径丢弃 | **对齐**：本 feature 修复 ERROR 路径（D5），语义与官方 interrupted 一致 |
| cancel 语义 | `IConversation.cancel()` = "Cancel the in-flight turn **while preserving its pending Queue**"（`service.d.ts` 实测）；排队消息呈现在 QueueDock，不进 transcript | 用户裁定（Clarifications 2026-09-03）：排队消息**落地为历史 user message**（进 transcript、不触发回合） | **不一致，按用户裁定执行**：官方 Queue 是 inbox/steering 体系（edit/remove/steer 操作），agent-v2 排队是简单 FIFO，落地语义更简单直接；在 contracts/agent-api-changes.md 记录该差异 |
| 历史模型 | durable session event 日志（seq、50/200 条分页、prepend、turn-jump） | 内存态消息列表一次性 List | 不对齐（051 A2 内存态裁定延续）；官方 Load earlier/turn 导航能力不在 scope |
| 乐观回显 | `session.beginSubmission` echo（submit 点击当帧显示、rpcId 关联退休） | 无 echo（send 即等待流首事件） | 不引入（超出缺陷修复 scope） |
| composer Stop | 运行中主按钮 Stop，有草稿时切 Queue Send（conversation README） | 本 feature 新增终止按钮 | **行为对齐**（Stop 形态），入口简化为 composer 旁单一按钮 |
| 主题 | ui-theme 插件管理 light/dark/system 偏好 + token sheets | 深色单一主题（自有 theme.css 变量子集） | **token 表复用**（D4），主题切换不引入（保持单一深色） |

## D3: 流式分段感知——ChatEvent 协议扩展 step 字段

**Decision**: `BlockStartEvent`/`BlockDeltaEvent`/`BlockEndEvent` 各增加 `int32 step` 字段（proto3 可选语义，缺省 0=首个 step，向后兼容：旧客户端忽略未知字段）。

**Rationale**: 前端按 step 分段需要 live 流上的 step 边界信号。现状 ChatEvent 事件只带 turn-global `index`（`projects/game/agent_v2/src/history.ts` 的 `remapIndex` 重映射），web 无法感知块属于哪个 step；而服务端 TurnCollector 已跟踪 step（`ActiveTurn.step`，事件 `data` 里的 `turn/step` 字段——`driver.ts` 的 `session.append("assistant/chunk", { turn, step, chunk })`），映射层透传即可，无新状态。

**Alternatives considered**: 新增独立 `step_end` 事件（放弃——块事件带 step 更简单且天然支持乱序容错）；前端按 block 类型启发式推断（放弃——不可靠）。

## D4: 样式缺口修复——引入官方 token CSS 表

**Decision**: web 前端引入 `@deepseek-ai/dsh-client-ui-theme@0.1.1-rc.2` 的 `src/styles/` token CSS（`base.css`、`design-platform.css`、`scrollbar.css`、`gradient-shadow-text.css`、`shiki.css`，按 README 声明的顺序），替代 `theme.css` 中手写的 `--dsw-*` 变量子集；保持深色单一主题（token 表的 dark 值经 `body[data-ds-dark-theme]` 或等效选择器激活，具体以 sheets 实际结构为准）；自有布局样式（`--app-*`）保留。

**Rationale**: ui-theme README 明确 "The token sheets are the sole color authority"——官方 token 表是 primitives 组件（Menu 卡片的 `--dsw-specific-menu`/`--dsw-alias-border-inverted`/`--dsh-shadow-lv3` 等）视觉的权威来源。引入方式为**仅消费 CSS 文件**（该包 exports 含 `./src/*`，vite 可直接 import css），不引入其 cordis runtime（ThemeRuntime 是 light/dark/system 偏好管理，web 用不到）。系统性修复 FR-021/022 且未来 primitives 升级时视觉自动对齐。

**Alternatives considered**: 手动在 theme.css 补缺失变量（放弃——逐组件打补丁，遗漏面大且与官方漂移）；引入 ui-theme 完整插件（放弃——需要 cordis client runtime + settings 生态）。

**版本注意**: dsh 依赖统一 `pnpm-workspace.yaml` catalog 管理（含 `third_party/dsh/core` 底座闭包；rc 线精确版本 0.1.1-rc.2，cordis/schemastery 保持既有 range——见 `survey/deepseek-harness-b1-bazel-packaging.md` §4.2）；ui-theme 0.1.1-rc.2 与 primitives 0.1.1-rc.2 同线。

> **载体重设计（2026-09-04 修正）**: 本条"该包 exports 含 `./src/*`，vite 可直接 import css"的引入方式与 npm tarball 实态不符（0.1.1-rc.2/0.1.2-rc.1 均不含 `src/`；CSS 以字符串内嵌于 `lib/client.js`，该文件为 dsh ModuleLoader 格式不可 import；官方注入路径是 cordis 客户端插件，与"不引入 cordis runtime"冲突）。引入载体改为"从安装包提取 vendor 至 `src/dsh-theme/` + 同步防漂移门禁"，候选验证与终态设计见 [revisions/phase10-theme-css-carrier.md](revisions/phase10-theme-css-carrier.md)。本条其余裁定（token 表复用替代手写子集、深色单一主题、不引入 cordis runtime、catalog 版本治理）不变。

## D5: 失败回合固化——driver ERROR 路径补 interrupted append

**Decision**: `common/js/dsh-plugins/saolei-loop/src/driver.ts` 的 step 循环：LLM 流失败（catch 非 abort 路径）与 finish error 抛 `LlmError` 前，若 assembler 已有部分内容，按 abort 路径同构的方式 append `assistant/message`（`interrupted: true`，仅含已产出块）。工具执行异常同理（executeToolCalls 异常冒泡前，本 step 的 assistant/message 已 append——现有顺序天然如此，验证即可）。

**Rationale**: 服务端 `SessionHistory` 是 session-lifetime 收集（`projects/game/agent_v2/src/history.ts:442-451`：`assistant/message` 到达即 `appendAssistant`，与流是否在线无关）——driver 补 append 后，失败回合的已产出内容自动进入历史与回填。对齐官方 `'assistant-step'` 的 interrupted 三态（D2）。这是"看过即焚"的服务端半；前端半见 D6。

**Alternatives considered**: 宿主层从 chunk 流重建历史（放弃——duplicate 状态源，与 loop 的 session log 冲突）；接受丢失（用户明确报告为缺陷）。

## D6: 前端失败/终止保留——store 终态语义拆分

**Decision**: `store/chat.ts` 归约改造：
1. `TURN_STATUS_ERROR`：不再丢弃 live——已完成的 step 分段保留并入本地历史（live 中无 `blockEnd` 的尾块按 interrupted 呈现），错误提示独立（`error` 字段照旧）。
2. 新增 `TURN_STATUS_CANCELED`（终止终态，D7）：同 ERROR 的保留语义，终态标识为"已终止"（非错误文案）。
3. `TURN_STATUS_ABORTED` 语义收窄为"会话已删除/重物化"（清空逻辑保留，与 051 既有 dispose/update 路径一致），与用户终止彻底区分。

**Rationale**: 前端丢弃逻辑在 `chat.ts:220-223`（ERROR 归约 `live: null`）。保留已呈现内容是 FR-013 的前端半；三态拆分让"失败/终止/销毁"互不混淆。

## D7: 终止 API——`:cancel` 自定义方法

**Decision**: AgentService 新增 `Cancel`（`:cancel`，POST `/api/v2/{session}/agent:cancel`，无请求体语义、返回空/状态）。服务端行为（session.ts）：
1. 在途回合存在 → 终止（经既有 `TurnCollector.abort()` 缝收束，driver 的 cancel 传播取消 LLM 流与在途工具，bridge dispatch 的 signal abort → FAILED "aborted" in-band 结算）；流上发 `turn_end{TURN_STATUS_CANCELED}`。
2. 排队消息落地：排队消息在 enqueue 时已 `appendUser` 进历史（`history.ts` `SessionHistory.appendUser`——"落地"的服务端事实已成立），cancel 时清空待处理队列（不触发回合）、向受影响流发必要的 queue 状态事件；幂等（无在途回合且无队列时 no-op 成功）。
3. 终止后 session 立即可接受新 Send。

**Rationale**: 命令名对齐官方 `cancel()`（D2）；终态用新枚举而非复用 ABORTED（D6）。排队落地语义与现状机制的耦合点（enqueue 即固化）使服务端改动集中在"清队列不触发"。

**Alternatives considered**: 复用 `TURN_STATUS_ABORTED`（放弃——前端既有清空语义冲突）；终止即清空队列（用户裁定否决，落地保留意图）。

## D8: 桌面连接状态——bridge 查询面 + GetAgent 携带 + 前端轮询

**Decision**:
1. `@dominion/dsh-desktop-bridge` 服务接口增加 `isDesktopConnected(sessionName): boolean`（读连接注册表事实）。
2. `GetAgent` 响应（`Agent` 消息）增加 `bool desktop_connected` 字段；经 `/api/v2` 现有路由透出（gateway/proxy 零改动预期——proto 字段扩展自动透传）。
3. 前端：ChatPanel 在进入会话、send 前、turn 结束、以及 10s 定时器轮询 GetAgent 刷新连接状态；状态指示呈现在对话页顶部（已连接/未连接/未知[查询失败降级]）。物化前（agent 不存在）GetAgent 404 → 状态"未知"降级（FR-002 不可静默显示已连接）。

**Rationale**: SC-005 要求 10 秒内反映状态变化——轮询 10s 已满足且实现最简（无推送基础设施）；事件推送（ChatEvent 流上新事件类型）需要流常开，与"空闲时无流"的现状冲突。连接事实以 agent 侧 bridge 注册表为唯一来源（接管语义下最终一致）。

**Alternatives considered**: WS 推送（放弃——超 scope）；desktop 自报（放弃——非 agent 侧事实，不可信）。

## D9: 模型目录配置

**Decision**: `projects/game/agent_v2/cordis.yml` llm-glm `models` 改为两条目：`glm-5.3`（contextWindow 1000000）与 `glm-5.3-flash`（contextWindow 以官方文档核实的默认窗口为准，实现期验证——见开放项）；`projects/game/agent_v2/src/session.ts` 的 `DEFAULT_MODEL` 改为 `GLM_MODEL || 'glm-5.3'`；`GLM_MODEL` env 覆盖机制保留。

**Rationale**: 官方套餐文档（https://docs.bigmodel.cn/cn/coding-plan/overview ，2026-09 实读）确认当前支持 GLM-5.3 与 GLM-5.3-Flash，历史别名自动切换（当前默认 glm-5.2 实际已被静默切至 glm-5.3，物化显示与实际服务不一致）。目录唯一来源是组合配置（051 research D4），改配置即全链路生效（ListModels、物化校验同源）。

**开放项（实现期验证）**: ① glm-5.3-flash 的基础 context window（官方 OpenAI Response 端点下是否需 `[1m]` 后缀形态——以 https://docs.bigmodel.cn/cn/coding-plan/latest-model.md 的调用配置核实）；② flash 为多模态模型（官方文档），llm-glm adapter 仅文本请求不受影响（验证 adapter 不因模型差异变更行为）。

## D10: "desktop 无执行却有游戏状态"排查 playbook（FR-001 执行方案）

**Decision**: 按"证据优先"的分步排查流程执行（不得凭推测改代码），产出执行证据归档：

1. **环境基线核实**：确认 agent-v2 无 `GLM_BASE_URL`/`GLM_LLM_TARGET` 覆盖（`projects/game/deploy.yaml` 无注入——已核实，LLM 为真实端点 `https://open.bigmodel.cn/api/v1`）；确认无 fake-desktop（生产部署清单已核实）。
2. **复现与取证**：真实 desktop 连接指定 session（确认 desktop 侧连接成功日志）→ web 对话发起游戏 → 经 signoz skill 拉取该 trace：agent-v2 侧 `desktop connection attached` 日志是否出现（bridge attach）、工具 dispatch 是否发出（dispatch 日志/FAILED "desktop disconnected"）、turn_end 终态与 error 内容。
3. **候选断点假设**（按现有证据的置信度排序）：
   a. **turn 以 ERROR 结束**（高置信：与"历史回填只剩用户消息"现象互证——D5 的缺口使 ERROR 回合服务端零固化）——ERROR 根因候选：LLM 流 finish error 后 llm-retry 仍失败、工具执行异常冒泡（如 agent-scoped `saoleiGame` 服务未就绪时 fail-loud 抛错）、其他 step 内异常；
   b. **desktop 连接的 session 与对话 session 不一致**（中置信：web 无连接状态可见性时用户无从发现——D8 修复后可立即辨别）；
   c. dispatch 链路断点（低置信：代码核查 dispatch 键两端一致，见 spec Motivation）。
4. **修复与回归**：按定位的断点修复（若为 a，D5 修复后失败回合可见错误内容将直接暴露 ERROR 根因——两缺陷修复互相催化）；SC-001 真实环境端到端复验（desktop 执行记录 + 桌面游戏真实进行 + 棋盘一致）。

**Rationale**: 用户已确认正式环境人工测试（排除环境混淆）；问题 3 与问题 4（历史丢失）的复合指向 turn ERROR。D5/D8 的修复本身会显著缩小排查面（失败内容可见 + 连接状态可见）。

## D11: testplan 重构方案（用户指令②）

**Decision**（详见 [contracts/testplan.md](contracts/testplan.md)）：
1. **deploy 合并**：`deploy_agent_v2_drop.yaml` 并入 `deploy_agent_v2.yaml`——单 deploy 内两个 fake-desktop 实例（`fake-desktop-won`→session `desktop-e2e-won`/scenario won；`fake-desktop-drop`→session `desktop-e2e-drop`/scenario progressive+disconnect fault），两执行器绑定不同 session 互不干扰（探索实证：两份 deploy 服务清单逐项相同，唯一差异是该服务 env）。
2. **suite 归并**：7 suite → **1 suite**（单次部署，cases 按"配置面→对话面→游戏面→桌面面"顺序排列）；用例间以唯一 session/preset 资源名隔离，避免跨 case 污染。
3. **binary 归并**：`agent_v2_game_disconnect_test` 并入 `agent_v2_game_test`（拓扑分离前提消失——两场景各自绑定 won/drop session 的 executor，同一 binary 内分函数）；其余 binary 保持（模块边界清晰）。
4. **执行预算**：部署次数 7→1（省 6 次部署 + 6×60s settle），`guitar run` 默认 10m 预算内完成的概率显著提升（超时不再必然）。

**Rationale**: `style/large_test.md` "按 suite 编排"要求在"较多部署拓扑/较少部署"间平衡——现状是 6 次完全相同的重复部署（反面极端）。guitar 串行执行 suites/cases（`tools/test/guitar/README.md`："suites 按 YAML 中的顺序串行执行"），单 suite 多 case = 一次部署顺序跑多个 binary，无并发干扰面；风险（状态残留）用唯一资源名隔离。测试文件/helper 组织已合规（探索实证：无 spec 编号命名、helper 共享良好），仅 helper 文件过大（`agent_v2_helpers_test.go` 1032 行）作为顺带优化项（按模块拆分，非本次必须）。

**Alternatives considered**: 保留 2 个 suite（配置面/全链路各一，两次部署）——放弃，单 suite 收益最大且隔离手段充分；为 disconnect 保留独立 deploy——放弃，正是用户要求消除的重复部署。

## D12: 前端组件与 store 改造面汇总

**Decision**: 改造集中在既有组件，不引入新框架/路由：
- `store/chat.ts`：step 感知（块事件 step 字段分组为 step 分段，不再整回合合并）、D6 三态、终止后状态；
- `components/ChatView.tsx`：按 step 分段渲染（复用 `AgentBlocks` 改造为分段容器）、turn 完成后折叠（最终答案判定=最后一个含非空 text 且无 tool-call 的 step；折叠区含步骤/工具计数与展开控制——对齐官方 Turn Process Folding 的可见规则，组件形态参考而非移植）、composer 区终止按钮（运行中可见）；
- `components/ReasoningRow.tsx`：展开体 MarkdownText；
- `components/ToolCard.tsx`：result 呈现改预格式化等宽（棋盘文本不再 JSON 字符串字面量化）；
- `components/PresetsView.tsx`：FormMode 驱动的独占视图切换；
- `components/SessionList.tsx`：零改动（Menu portal 行为正确，token CSS 引入后卡片视觉自动修复——D4）；
- `App.tsx`/`ChatPanel`：连接状态轮询与指示、agent 面板默认模型随目录自动生效（零改动预期）。

**Rationale**: 分段/折叠的判定逻辑（最终答案 step 判定、折叠计数）参考官方 chat 包 README "Turn Process Folding" 的规则陈述实现（L3 对齐：流式期间全展开、turn/end 后折叠、无最终答案不折叠、手动展开保持），但数据流与组件为本仓库既有形态。

---

## 开放项汇总（全部为实现期验证点，非设计歧义）

| 项 | 归属 | 验证方式 |
|---|---|---|
| glm-5.3-flash context window 与 `[1m]` 后缀必要性 | D9 | 官方调用配置文档核实 + 实测 |
| ui-theme token sheets 的 dark 激活选择器形态 | D4 | 已落定：`body[data-ds-dark-theme]` 布尔属性存在性选择器（token 定义于 `body` 作用域），见 [revisions/phase10-theme-css-carrier.md](revisions/phase10-theme-css-carrier.md) §1.2 |
| gateway/proxy 对 proto 新字段的透传 | D8 | codegen 后集成验证（预期零改动） |
| guitar 单 suite 多 case 的执行顺序保证 | D11 | guitar 文档/实测（README 已证 suites 串行） |
| 排查断点定位（D10 假设 a/b/c 孰是） | D10 | 真实环境 signoz 取证 |
