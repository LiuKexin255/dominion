# 调研：dsh memory 插件迁移（planner 长期记忆）

> **状态**：调研完成。**已确认决策（2026-09-08，两轮）**：
> ① **功能对齐 v1**——memory 大体功能对齐当前 agent v1（039 落地）的 memory 功能（工具面、冻结快照注入、立即持久化语义，§3）。
> ② **插件提供两个功能面**——memory 工具（单工具，**不需要配套 prompt**，§4）；system prompt 注入 memory 内容（**注入时机越晚越好，agent 启动时注入当前内容最佳**，§5）。
> ③ **快照固定 + 修改经调用历史呈现**——注入 planner system prompt 的 memory 内容在 agent 生命周期内固定（不刷新）；memory 的修改过程通过 tool 调用历史（planner log 的 tool/call + tool/result）呈现（§5.2/§6.1）。
> ④ **team 广播下 player 可见，接受**——memory tool 调用过程会经 team 广播被 player 看到，先这样；只需 player 能区分该 tool 不是自己调用的（sender 标注已满足，§6.2）。
> 第二轮（同日，待定项全部裁定）：⑤ **存储沿用 memory 服务**（gRPC client 迁移自 v1，§7）。⑥ **插件挂载取路径 A（preset 行 + host 服务面）**——roster 可行性由用户另行验证，作为外部前提（§5.3/§8 风险 5；路径 B 为机制等价回退）。⑦ **scope 键沿用 (template, session)**，对齐 memory 服务资源模型（§3/§9）。⑧ **物化首读 fail loud**——setup throw → 物化回滚不发布（§8 风险 2）。**至此全部待定项已决策，可进入 spec/plan 阶段（路径 A 的 roster 前提待用户验证结论）。**
> **日期**：2026-09-08
> **前置调研**：`survey/deepseek-harness-team-mode.md`（双 agent 拓扑与 team 插件——本调研即其决策 ⑩ 延后的"外置 memory 单独调研"；§9.1 层 3 的"planner memory 工具插件行"占位由本文填充）、`survey/planner-memory-and-agent-communication.md`（v1 memory 的设计依据：hermes 冻结快照、hot-path 单工具、D1–D6 决策）、`survey/deepseek-harness-preset.md`（system prompt 装配管线与所有权原则）、`survey/deepseek-harness-agent-loop-prereq.md`（官方 agent-loop 机制）
> **范围**：memory 从 v1（LangChain，`projects/game/agent/`）迁移到 dsh（agent_v2 生态）的机制调研——功能对齐映射、memory 工具的 dsh-tools 形态、system prompt 注入的时机/冻结/实现路径、team 广播下的可见性分析、存储形态选项、风险项。**不含** memory 插件 spec 的最终方案设计。

---

## 1. 背景与调研问题

team-mode 调研拍板双 agent 拓扑（player/planner 双顶层 agent + team 层）后，planner 的 memory 工具插件行是其堆叠基线（`survey/deepseek-harness-team-mode.md` §9.1 层 3）中唯一"延后调研"的占位。051 落地时明确"memory 服务本 feature 不动（保留部署与 gateway 路由；处置留给后续长期存储迁移 feature）"（`specs/051-agent-v2-dsh-migration/spec.md` Q1/A1）——本调研即该 feature 的前导调研。

调研问题（对应用户给定输入）：

1. v1 memory 功能全景是什么？v2 需要对齐哪些、有意差异哪些？（§3）
2. memory 单工具在 dsh-tools 下的形态？不迁移配套 guidance（memory skill）在 dsh 所有权模型下是否合法？（§4）
3. system prompt 注入的实现路径：注入时机为何能落在"agent 启动"？冻结语义如何保证？section 注册在哪层？（§5）
4. 修改过程经 tool 调用历史呈现的机制依据？player 经 team 广播看到 memory tool 调用的区分机制与风险？（§6）
5. 存储形态的选项与判定？（§7）
6. 其他风险项？（§8）

信息源：

- v1 源码：`projects/game/agent/src/mcp/memory/memory-mcp.ts`（486 行，工具定义与 hermes→id 转换）、`projects/game/agent/src/team/memory-snapshot.ts`（冻结快照）、`projects/game/agent/src/skill/memory/SKILL.md`（配套 guidance）、`projects/game/agent/src/memory-client.ts`（gRPC client）、`projects/game/game.proto`（MemoryService）
- dsh 物化源码（0.1.1-rc.2 线）：`node_modules/.pnpm/@deepseek-ai+dsh-system-prompt@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-system-prompt/`（类型 + README）、`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent/lib/types/index.d.ts`（AgentSetup/CreateAgentOptions）、`node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_ecf90c249e2d9dbb5492af64a9f4bd5d/node_modules/@deepseek-ai/dsh-agent-loop/lib/index.js`（装配调用点）、`node_modules/.pnpm/@deepseek-ai+dsh-agent-presets@0.1.1-rc.2_5200ead8959daeaefdf3dd69ba905368/node_modules/@deepseek-ai/dsh-agent-presets/README.md`（standing mount）
- agent_v2 生态现状：`common/js/dsh-plugins/saolei/src/index.ts`（工具插件先例）、`common/js/dsh-plugins/saolei-loop/src/index.ts`（物化与 agent-scoped 注册先例）、`projects/game/agent_v2/README.md`
- 设计依据：`survey/planner-memory-and-agent-communication.md`（hermes 冻结快照哲学、压缩刷新边界、issue #17251 贬低坑）

---

## 2. 机制事实基础（源码级）

### 2.1 v1 memory 功能全景（对齐基准）

（`projects/game/agent/src/mcp/memory/memory-mcp.ts`、`projects/game/agent/src/team/memory-snapshot.ts`、`projects/game/game.proto`，039 spec 落地）

| 维度 | v1 现状 | 源码锚点 |
|---|---|---|
| 工具面 | **单工具 `memory`**（hermes 风格）：`action`（add/replace/remove）/`content`/`old_text`（大小写敏感子串定位）三字段单操作形式，或 `operations[]` 批量形式（原子，全成功才提交）；**无 memory_id、无 read 动作、无独立读写工具** | `memory-mcp.ts:427-471`（schema）、`:101-132`（matchBySubstring） |
| 工具结果 | 单文本块；成功 `memory added/replaced/removed`、`memory: applied N operation(s)`；**失败也是文本**（0 命中/多命中返回当前全部条目或预览，从不抛异常） | `memory-mcp.ts:472-482` |
| id 转换 | 工具内部把 hermes 风格调用量子转为服务端 id-based RPC：`add` → `sha256(content) 前 32 hex` 为 memory_id + 查重；replace/remove 先 list 全量再子串定位 | `memory-mcp.ts:367-400`、`:82-84` |
| 注入形态 | `FrozenMemorySnapshot` → 一条 SystemMessage（`id = "planner-memory-snapshot"`，纯 content 每行一条，**不含 id**），作为**每次 invoke 的 input 首条**注入；回写通道按 id 过滤防污染短期历史 | `memory-snapshot.ts:42-94`、`projects/game/agent/src/team/planner.ts:351-359`、`:422-424` |
| 注入/刷新时机 | team 初始化首次烘焙 + **每 5 局压缩边界刷新**（hermes 压缩刷新模式）；写操作立即持久化但不刷新快照；刷新失败保留上一快照不阻断 | `projects/game/agent/src/server.ts:257-258`、`projects/game/agent/src/team/compress.ts:277`、`memory-snapshot.ts:71-80` |
| 配套 guidance | memory SKILL.md（何时记录/跳过、old_text 用法、冻结快照模型）append 进 planner base prompt | `projects/game/agent/src/skill/memory/SKILL.md`、`planner.ts:295-299` |
| 存储 | 独立 memory Go 服务（gRPC `:50051`）+ MongoDB `game_memory.memories`，唯一索引 (template, session_id, memory_id)；AIP 风格 RPC（Create/Update/Delete/List，update_mask 仅 content）；gateway 经 `/api/v1/.../memories` 暴露 CRUD | `projects/game/game.proto:184-220`、`:363-376`、`projects/game/memory/runtime/mongo/model.go:24-32` |
| scope 键 | `(template, session)`（业务 session），资源名 `templates/{template}/sessions/{session}/memories/{memory}` | `game.proto:366` |

设计依据（`survey/planner-memory-and-agent-communication.md` 已拍板的 D1/D2/D4/D5）：hot-path 单工具（D1）、冻结快照不随写刷新（D2）、刷新边界定在压缩（D4，**v2 有意改变，见 §5.2**）、不烘焙进 createAgent 的 systemPrompt 而用冻结缓存注入（D5，**v2 的机制约束已消解，见 §5.1**）。

### 2.2 dsh system prompt 装配：section API 与装配时机

（`node_modules/.pnpm/@deepseek-ai+dsh-system-prompt@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-system-prompt/lib/types/index.d.ts`）

- **`PromptSection.text` 支持两种形态**：`string | ((context: AssembleContext) => string)`（index.d.ts:61）——静态文本或**每次装配求值的函数**（注意：**同步函数**，不能 await）。
- **装配时机：每 step 一次**——官方 agent-loop 的 `preStep()` 内 `systemPrompt.assemble(assembleContextFor(this, signal))`（`dsh-agent-loop/lib/index.js:497`）；类型自述 "Registry service for the prompt inputs assembled before each model step"。静态文本 section 生命周期内每 step 求值结果相同 → 请求前缀稳定。
- **装配管线**：全局层 ∪ caller scope 层（同名 scope 遮蔽全局）→ order 升序 → `system-prompt/assemble` waterfall → `complete: true` 封口 → 严格 `{{variable}}` 插值 + **空 section 丢弃**（`survey/deepseek-harness-preset.md` §6.2）。空 section 自动丢弃对 memory 有直接收益：无记忆时 section text 为空即不渲染，无需占位逻辑。
- **KV cache 效应**（`dsh-agent-presets` README "KV Cache effect" 节原文）："Prefix-stable for the life of an agent: a composition is installed once, before the agent is published and therefore before its first request, and is never re-read while the agent runs."——注册先于首次请求、生命周期内不变是框架级承诺。
- **`AssembleContext.scope`**：函数式 section 求值时拿到调用方 scope key（merge-extensible，官方 variable 求值用其扩展字段 `context.agent`，`dsh-agent-loop/lib/index.js:1024-1026`）——standing mount 下共享注册的 section 函数可按 scope 查 per-agent 状态。

### 2.3 物化 setup：先于首次装配的异步窗口

（`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent/lib/types/index.d.ts:57,100-117`）

- `AgentSetup = (agentCtx) => AgentSetupCommit | Promise<...> | void`——**setup 支持 async**。
- 官方契约原文："Everything registered through `agentCtx` (scoped tools, prompt sections/variables, `restrict()`, listeners, awaited child plugins) exists before `session/created`, `agent/created`, `agent/session-start`, **and the first prompt assembly**."
- **推论（本调研最重要的时序事实）**：在物化 setup 内异步读 memory → 注册 section/缓存快照，agent 发布后的首次装配必然读到就绪快照——**预取无竞态**，"agent 启动时注入当前内容"（决策 ②）有精确的机制落点。且 "Drive the agent only after creation resolves"——物化 resolve 后 loop 才驱动，不存在早于 setup 完成的请求。

### 2.4 standing mount 与 per-agent 状态（preset 行形态的前提）

（`node_modules/.pnpm/@deepseek-ai+dsh-agent-presets@0.1.1-rc.2_5200ead8959daeaefdf3dd69ba905368/node_modules/@deepseek-ai/dsh-agent-presets/README.md`）

- standing mount："The mount's tools, prompt sections, and projection units exist exactly once and cover every joined agent — **its plugins key their state by Session/Agent, so sessions stay apart inside one shared instance**"——工具/section 注册每进程一份（preset 层），**per-agent 状态隔离是插件自己的责任**（按 Session/Agent 键控）。
- preset 行插件**没有 agent-join 钩子**：apply 只执行一次（standing），README 未提供 join 回调；mount 的 scope-filtered listeners 虽只收到 parented 下来的 agent 的事件，但事件时序（agent/created 在 publish 后 emit）晚于 setup，**不能作为预取时机**（异步竞态：loop 可能在预取完成前驱动）。
- **推论**：preset 行形态下，快照预取必须由 loop 的物化 setup 显式触发（调用插件提供的服务），不能依赖插件自治感知 join。
- mount rejects："A row that published a service into the root realm"——preset 行内的服务必须 entry-local `isolate` realm 或上移 host 组合；memory 插件若提供 host 层服务面（预取/存储访问 API），该服务行应在 host 层而非 preset 行。

### 2.5 agent_v2 现状与 team-mode 基线的差距（memory 插件的落地环境）

| 维度 | agent_v2 现状（051 落地） | team-mode 基线（§9 堆叠） | 对 memory 插件的影响 |
|---|---|---|---|
| 拓扑 | player 单角色；自研 saolei-loop（agent loop 层，`setFactory`） | player/planner 双顶层 agent；官方 `dsh-agent-loop` 行保留，saolei-loop 升为 team loop | memory 的消费方 planner 尚不存在；本调研按基线设计 |
| preset | Mongo `game_agent_v2.presets`（提示词文本）+ PresetService CRUD；persona 经 `agent.ctx.systemPrompt.section` agent-scoped 注册（`common/js/dsh-plugins/saolei-loop/src/index.ts:412-416`） | `dsh-agent-presets` roster 双 root 池；preset = 目录 + 插件行 | 现状无 preset 插件行机制——memory 插件挂载形态依赖基线落地（§5.3） |
| 组件先例 | saolei 工具插件（工具 + guidance section，`common/js/dsh-plugins/saolei/src/index.ts`）；agent-scoped 服务（`saoleiGame`，物化时注册） | 同左 + team 插件 + memory 插件 | saolei 插件是 memory 插件的直接结构参照 |
| memory | 无（051 明确排除） | planner memory 工具插件行（本文填充） | memory Go 服务与 gateway `/api/v1` memory 路由**仍在产线**（051 A1/FR-019：保留部署，处置留给本 feature 线） |

---

## 3. 问题 1：功能对齐映射（v1 → v2）

决策 ①"大体功能对齐 v1"的逐项判定：

| v1 功能 | v2 对齐判定 | 说明 |
|---|---|---|
| 单工具 `memory`（add/replace/remove + 批量，old_text 子串定位，无 id 无 read） | ✅ 对齐 | 工具名、schema 字段、匹配语义、结果文本原样迁移；载体从 MCP McpServer 换为 dsh-tools `defineTool`（§4） |
| 工具结果语义（失败也是文本 + 返回条目辅助重试） | ✅ 对齐 | execute 内 catch 返回文本结果（dsh-tools 的 throw 路径会变成 model-visible error——刻意不用，保持 v1 的"错误即普通结果"语义，`memory-mcp.ts:478-481` 注释引用 031 C15 neutral status） |
| 立即持久化（写操作即刻落库） | ✅ 对齐 | 工具 execute 同步走存储写入（§7） |
| 冻结快照注入 LLM | ✅ 对齐（形态升级） | 从 invoke input SystemMessage 升级为 system prompt section（§5.1）——v1 用 input 注入的动机是"避免重建 createAgent 实例"（`survey/planner-memory-and-agent-communication.md` D5 方案 b），dsh 的 section 注册与 agent 实例解耦，该约束不存在 |
| 快照刷新边界 = 每 5 局压缩 | ⚠️ **有意差异（决策 ③）** | v2 改为 agent 生命周期固定（物化时读一次）；差异分析见 §5.2 |
| memory SKILL.md guidance | ⚠️ **有意差异（决策 ②）** | 不迁移（§4.2）；工具 description 中的两处 skill/压缩边界引用需改写为 v2 语义 |
| 存储独立服务 + Mongo + gateway CRUD | ✅ 对齐（决策 ⑤） | 沿用 memory 服务（§7）；051 A1 已保留部署与路由，零改动 |
| scope 键 (template, session) | ✅ 对齐（决策 ⑦） | 沿用，对齐 memory 服务资源模型与 gateway 寻址 |

---

## 4. 问题 2a：memory 工具的 dsh 形态（无配套 prompt）

### 4.1 工具注册（结构参照 saolei 插件）

`common/js/dsh-plugins/saolei/src/index.ts` 是直接模板：`defineTool({name, description, parameters, output, execute})` + `ctx.tools.register()`。memory 工具映射：

- **name**：`memory`（保持 v1 名）。
- **parameters**：v1 的 zod schema（`memory-mcp.ts:441-470`）转为 dsh-tools schema DSL（`type/enum/array/object/required` 全部在 dsh-tools 0.1.1-rc.2 支持的 keyword 子集内——saolei 插件注释记录 CONSTRAINT_KEYWORDS 含 type/oneOf/properties/required/additionalProperties/items/enum/const；memory 参数全是 string/enum/array/object，**无 numeric bound 需求**，不触发"minimum 被 registration 拒绝"类约束缺口）。
- **output**：对齐 saolei 的 `{result: string}` + render（text block）。
- **execute**：闭包/agent-scoped 解析存储访问（§5.3）；双形式互斥校验对齐 v1 `applyMemoryCall` 的原子批量语义（preflight 对工作副本校验，`memory-mcp.ts:367-400`）。
- **description 改写**（唯一需要内容调整的部分）：v1 description 末句 "Changes persist immediately but the frozen snapshot refreshes only at the compression boundary. See the memory skill for when to record and what to skip."（`memory-mcp.ts:438-440`）——两处过时：压缩边界（v2 无压缩刷新，改为 agent 启动固定，决策 ③）与 skill 引用（v2 无 skill，决策 ②）。改为 v2 语义的一句话（如 "Changes persist immediately; the snapshot in your system prompt is fixed at agent start."）。

### 4.2 不迁移 guidance 的合法性（dsh 所有权模型下）

dsh 所有权原则（`survey/deepseek-harness-preset.md` §6.4"every fact in the prompt has exactly one owner"）中，工具的**跨调用习惯**（guidance section）是工具包的可选贡献而非义务——minimal preset 封掉全部 sections 后工具照常可用（"工具的'怎么调'在 schema 里"，同文 §7.1 第 3 点）。单次调用语义归 schema description。memory 是单工具、无跨调用协调需求（对比 saolei 三工具 + 棋盘格式约定），**不注册 guidance section 合法且无一致性风险**。

需要注意的责任转移：v1 skill 中"何时记录/何时跳过"的指导（`SKILL.md` "When to record (WHEN)" 节）不迁移后，该信息的去处是 **planner preset 的 persona 文本**（用户编辑面）——是否写、写什么由 preset 作者决定，插件不强制。v1 的 planner base prompt 本就有"把值得跨局保留的观察写入长期记忆（调用 memory 工具）"一句（`projects/game/agent/src/team/planner.ts:103-109`），v2 对应内容自然落在 planner persona。这是产品内容决策，记录不阻塞。

另：不注册 guidance 恰好规避了 team-mode 调研 §3.6 的 restriction 不对称坑——memory 插件只有工具没有 section，任何层级的工具可见性控制都不存在"guidance 残留"问题。

---

## 5. 问题 2b：system prompt 注入——时机、冻结与实现路径

### 5.1 注入时机为何能落在"agent 启动"（决策 ② 的机制落点）

"越晚越好，能在 agent 启动注入当前 memory 内容最好"的机制解读：memory 可能被上一进程或外部 CRUD（gateway `/api/v1`）修改过，读取时机越接近首次使用，快照越"新鲜"；最晚可行点是不能晚于首次装配。dsh 的精确落点即**物化 setup**（§2.3）：setup 内 `await` 读存储 → 注册 section/缓存 → agent 发布 → 首次装配（每 step 装配，§2.2）读到就绪快照。早于物化的任何时机（进程启动、preset 挂载）都更"旧"；晚于物化（per-request live 读）违反冻结决策（③）。**物化 setup 是唯一同时满足"最晚"与"冻结"的时机**。

### 5.2 冻结语义与 v1 刷新边界的差异（决策 ③ 的语义分析）

| | v1 | v2（决策 ③） | hermes 对照 |
|---|---|---|---|
| 快照生命 | 每 5 局压缩边界刷新（`compress.ts:277`） | agent 生命周期内固定，不刷新 | 会话开始冻结 + 压缩时刷新（`survey/planner-memory-and-agent-communication.md` §4.1） |
| 修改的可见通道 | tool result 文本（确认写成功）+ 下次刷新后 system prompt | **tool/call + tool/result 落 planner log**（决策 ③：修改过程经调用历史呈现）+ 下次物化后 system prompt | 同左（tool responses 显示 live state） |

- **可接受性依据**：planner 在 log 中能看到自己每次 add/replace/remove 的调用与结果（dsh "Model-visible means logged"——工具调用必落日志），模型可据此推断当前记忆状态与快照的差异；system prompt 快照提供基线，调用历史提供增量。这与 hermes frozen snapshot 的补偿机制同型。
- **风险边界（§8 风险 3）**：planner 侧若启用 compaction（team-mode 风险 7：planner 上下文膨胀由 compaction 承担），memory tool 的调用历史**也会被压缩**——补偿通道降级为压缩摘要。hermes 的对应坑是压缩摘要把 memory 贬低为 "background reference, NOT active instructions" 导致 agent 忽略记忆（[issue #17251](https://github.com/nousresearch/hermes-agent/issues/17251)）。缓解方向（spec 阶段）：planner compaction 摘要提示词明确 memory 快照是 active context；或接受摘要级保真。记录为已知风险，不改变决策。
- **外部 CRUD 的一致性语义**：gateway memory CRUD 修改后，运行中 planner 的 system prompt 不变（冻结）；下次物化（进程重启/planner 重建）才生效。读侧最终一致，需在 spec 明示。

### 5.3 两条实现路径（section 注册层级的选择——已确认路径 A，决策 ⑥）

机制上都成立，差异在"memory 能力由 preset 文件还是 loop 接线承载"：

**路径 A：preset 行形态（对齐 team-mode 决策 ⑥/⑭ 字面）**

planner preset 的 memory 插件行 apply 时注册：memory 工具（`ctx.tools.register`，落 preset 层——player 的解析链 agent→preset→global 不含 planner preset，天然看不到）+ 函数式 section（`text: (context) => 快照缓存.get(context.scope) ?? ""`，§2.2 的 AssembleContext.scope）。per-agent 快照缓存由插件按 Session/Agent 键控（§2.4 standing mount 语义），**预取由 loop 物化 setup 显式触发**（调用 memory 插件的 host 服务面 `load(scopeKey, businessKey)`——preset 行插件无 join 钩子，§2.4 推论；host 服务行在 host 层避免 root realm reject）。

- 优点：planner preset 文件体现 memory 能力（"preset 选择插件"，决策 ⑭）；多 planner preset 可组合不同 memory 配置。
- 代价：插件双行（host 服务行 + preset 行）或一等包两导出；loop 与 memory 插件间多一条 load 调用契约；依赖 team-mode 基线的 roster 落地。

**路径 B：loop attach 形态（对齐 agent_v2 现状先例）**

memory 插件为 host 行（提供 `ctx.plannerMemory` 服务）；loop 物化 planner 的 setup 内 `await ctx.plannerMemory.attach(agentCtx, {template, session})`；attach 内**全部经 agentCtx（agent scope）注册**：异步读快照 → `agentCtx.systemPrompt.section({... 静态文本快照})` + `agentCtx.tools.register(memory 工具)` + agent-scoped 存储句柄（工具 exec 经 `exec.agent.ctx.get(...)` 解析，saolei `saoleiGame` 同构，`common/js/dsh-plugins/saolei-loop/src/index.ts:412-421` 双先例：persona section 与 agent-scoped 服务都是此形态）。agent dispose → scope unwind → 全部回收。

- 优点：单一挂载行；时序简单（setup 内一站式）；**现状（无 roster）即可落地**；agent-scoped 注册的工具可见性绝对隔离（不在 player 解析链）。
- 代价：**偏离决策 ⑥/⑭ 字面**——"planner 有 memory 工具"的事实由 loop 代码（attach 调用）而非 preset 文件承载；planner preset 只剩 persona 行。注：决策 ⑭ 的原始动机是工具与 guidance 的一致性（§3.6）——memory 无 guidance，该动机下无风险；偏差是纯粹的"能力声明位置"问题。

**判定（2026-09-08 用户确认，决策 ⑥）**：**取路径 A**（preset 行 + host 服务面）——对齐 team-mode 决策 ⑥/⑭ 字面，planner preset 文件体现 memory 能力；预取契约即上文所述"loop 物化 setup 显式调用 host 服务面 `load(scopeKey, businessKey)`"（businessKey = (template, session)，决策 ⑦）。**roster 可行性**（SDK 直组 + preset roster 组合，team-mode §9.5 无先例项 1）**由用户另行验证**，作为路径 A 的外部前提（§8 风险 5）。路径 B（loop attach）记录为回退形态：若 roster 验证不通过，B 机制等价、可复制现状先例先行；两路径的存储访问/快照逻辑共用，切换成本低（工具与 section 注册逻辑从 preset 行移入 attach）。

### 5.4 order 频段与前缀稳定性

- 生命周期内前缀稳定由机制保证（§2.2 KV cache 效应），与 memory section 位置无关。
- 位置建议：order **200+**（工具守则 100–199 之后、靠近 system prompt 尾部）。理由：memory 是"数据"不是"规则"（对比 persona/team section/工具守则都是行为指令）；对齐 hermes 的分层实践（memory 快照属 VOLATILE 层放尾部，`survey/planner-memory-and-agent-communication.md` §4.3）；未来若任何 section 走函数式动态求值，尾部位置使前面稳定 section 的前缀最大化。具体值 spec 定。

---

## 6. 问题 3+4：修改可见性与 team 广播下的 player 视角

### 6.1 修改过程经 tool 调用历史呈现（决策 ③ 的机制依据）

dsh 中工具调用必落日志：`tool/call` + `tool/result` 是 session 事件（"Model-visible means logged"，`survey/deepseek-harness-agent-loop-prereq.md` §2.2/§4.6）。planner 复盘中调用 `memory` 工具 → 调用与结果落 planner log → `deriveMessages()` 投影进后续每次请求。模型对记忆状态的完整视图 = system prompt 快照（基线）+ log 中的调用历史（增量）。无额外机制需要。

### 6.2 player 看到 memory 广播：区分机制与已接受风险（决策 ④）

team 收集成员输出含 `tool/call`/`tool/result`（team-mode 决策 ⑧：工具调用与结果归属 agent 输出），planner 的 memory tool 调用会 1:1 原样广播进 player buffer（决策 ⑬），格式同 team-mode §5.3 第 4 层：`[planner] 工具调用 memory …` + `<planner-tool-call>tool/args/result</planner-tool-call>`。

- **区分机制已满足**：首行 `[planner]` sender 标注 + 正文标签对包裹——player 收到的是"planner 的输出广播"这一群聊语义，team section（名册与格式声明）进一步声明"面向你的工具调用标注来自其他成员"。player 不需要、也不会把它当作自己的调用。决策 ④ 的要求（能区分非自己调用）由既有广播格式满足，**memory 侧零额外机制**。
- **已接受风险（记录）**：
  1. **信息暴露**：memory 内容（planner 对 player 行为的跨局观察校准，可能含负面评价）经广播对 player 可见——v1 中不存在（planner 通道私有）。潜在行为影响（player 看到负面记录后过度补偿）属实验变量，用户已接受。
  2. **幻觉调用风险**：player 看到广播中的 `memory` 工具名但自己的工具目录没有该工具（挂载层隔离）——模型若模仿调用，dsh 工具管线返回 model-visible 失败（不存在的工具），无静默错误。风险低。
  3. **token 膨胀**：v1 工具失败时返回全部条目（`memory-mcp.ts` matchBySubstring 失败路径）——广播原样（决策 ⑬）进 player buffer。代价已由 team-mode 决策 ⑬ 总体接受，memory 的增量是"失败重试场景下的条目列表文本"，量级小。

反向可见性（planner 看 player 的 saolei-* 调用）是 team 设计本意，不属 memory 范畴。

---

## 7. 存储形态（已确认：沿用 memory 服务，决策 ⑤）

| 选项 | 形态 | 判定 | 说明 |
|---|---|---|---|
| **A（已确认）** | **沿用 memory Go 服务**：agent_v2 进程内 gRPC client（`dominion:///game/memory:50051`，v1 `projects/game/agent/src/memory-client.ts` 的迁移样板）→ 现有服务 + Mongo `game_memory` | ✅ 已确认 | 051 A1 已保留服务与 gateway `/api/v1` memory 路由在产线（`specs/051-agent-v2-dsh-migration/spec.md` FR-019）；gateway CRUD、memory 大型测试套件（已迁 `deploy_agent_v2.yaml`）零改动；跨进程一致、多实例安全（数据在共享 Mongo）；hermes→id 转换逻辑在 agent 侧原样迁移。代价：工具链路多一跳进程外 gRPC（v1 同构，无新增风险） |
| B | 进程内嵌入：agent_v2 直连 Mongo 或本地文件 | ❌ 不推荐 | 直连 Mongo = TS 重写存储逻辑（与 Go 服务双实现漂移，违反 single source）；本地文件与 stateful owner 亲和冲突（实例漂移丢数据），且 memory 的存在意义是跨 session 长期——不可接受 |
| C | MCP server 形态（dsh-mcp-client 挂载） | ❌ 排除 | 需给 memory 服务新加 MCP 面（stdio/HTTP）；dsh-tools 原生注册更直接（工具名、schema、结果渲染全可控，无需 `mcp__` 前缀）；v1 的 MCP 包装是 LangChain 生态适配物，dsh 下无对应需求 |

选项 A 下插件对存储的全部依赖收敛为一个 client 接口（List/Create/Update/Delete 四个 RPC + 资源名构造），存储实现（Go/Mongo）对插件透明——未来若要收缩存储形态，只换 client 实现。

---

## 8. 综合风险项

1. **冻结快照 + compaction 的补偿通道退化**（§5.2）：planner log 压缩后，memory 修改历史降级为摘要；hermes issue #17251 记录了摘要贬低 memory 的坑。缓解留 spec（compaction 摘要提示词 or 接受）。**不改决策，记录边界**。
2. **首读失败 = 物化失败（已确认，决策 ⑧）**：v1 刷新失败"保留上一快照、不阻断"（`memory-snapshot.ts:71-80`）的韧性语义不迁移；v2 物化首读失败 → setup throw → 官方契约 "A setup throw/rejection, commit throw, or owner disposal rolls the scope back without publishing either id"（`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent/lib/types/index.d.ts:109-111`）——物化整体回滚，无半物化状态，上游重试物化即重试读取。对齐 dsh fail-loud 哲学与 `mount rejects` 风格（§2.4）。
3. **standing mount 的 per-agent 状态是插件责任**（§2.4）：路径 A 下快照缓存键控（Session/Agent）、预取时序（setup 触发）都是插件契约，无框架保障——需单测覆盖"双 planner agent 快照隔离"与"预取完成前不可装配"。
4. **同步 section 求值函数约束**：`(context) => string` 不能 await（§2.2）——任何异步 IO 必须发生在装配之外（setup 预取）。这是路径 A 的硬约束，spec 需固化该时序依赖（首次装配必然晚于 setup 是框架契约，§2.3，风险在实现误用而非机制）。
5. **roster 前提（已确认路径 A 的外部依赖，决策 ⑥）**：路径 A 依赖 team-mode 基线的 roster 落地（agent_v2 现状无 preset 插件行机制，§2.5）；"SDK 直组 + preset roster 组合"属 team-mode §9.5 无先例项 1 的探索项——**roster 可行性由用户另行验证**，验证结论作为 memory feature spec 的前置输入。回退形态：路径 B（loop attach，机制等价、现状可落地，§5.3）。memory feature 的 spec 需声明与 team-mode feature 的先后关系。
6. **0.1.1-rc.2 快照风险**（沿用前例）：`dsh-system-prompt`/`dsh-tools` API 无稳定性承诺（developer preview）；精确 pin 既有决策覆盖。
7. **工具名 `memory` 的通用性**：preset 层注册 + player preset 不含 → 当前无冲突；host 层未来若引入其他 memory 类工具（如官方 examples/mcp-memory）需注意不同时挂载或改名。
8. **外部 CRUD 与冻结的一致性**（§5.2）：gateway 编辑 memory 对运行中 planner 不生效——spec 需明示"下次物化生效"的最终一致语义，避免使用侧困惑。

---

## 9. 结论与决策记录

**已确认决策（头部 ①–⑧）落地为设计基线**：

- memory 插件两功能面：单工具 `memory`（v1 schema/语义原样迁移，无 guidance section）+ system prompt 快照 section（物化 setup 读一次、生命周期固定、order 200+ 建议、空自动不渲染）。
- 修改可见性走 tool 调用历史（零额外机制）；player 侧区分靠既有 team 广播 sender 标注（零额外机制）。
- 存储：沿用 memory 服务，gRPC client 迁移自 v1 `memory-client.ts`（决策 ⑤，§7）。
- 挂载：路径 A——planner preset 的 memory 插件行（工具 + 函数式快照 section）+ host 层服务面（预取/存储访问）；loop 物化 setup 调用 `load(scopeKey, (template, session))` 预取（决策 ⑥，§5.3）。
- scope 键：(template, session)，对齐 memory 服务资源模型 `templates/{template}/sessions/{session}/memories/{memory}` 与 gateway 寻址；由 loop 物化 planner 时传入（决策 ⑦）。
- 首读失败：fail loud——setup throw → 物化回滚，无半物化状态（决策 ⑧，§8 风险 2）。

**待定项已全部决策（2026-09-08 两轮，头部 ①–⑧）；剩余外部前提一项**：roster 可行性验证（用户另行进行，§8 风险 5）——验证通过则路径 A 定案，不通过则回退路径 B（机制等价）。

**对后续 spec 的输入**：工具 description 改写点（§4.1）、compaction 摘要与 memory 的交互（§8 风险 1）、per-agent 隔离与预取时序的测试要求（§8 风险 3/4）、与 team-mode feature 的顺序声明及 roster 验证结论的回填（§8 风险 5）。

---

## 10. 引用来源汇总

仓库内（v1 源码与 agent_v2 生态）：

- `projects/game/agent/src/mcp/memory/memory-mcp.ts`（v1 工具定义/schema/hermes→id 转换/文本结果语义）
- `projects/game/agent/src/team/memory-snapshot.ts`（v1 冻结快照与刷新失败语义）
- `projects/game/agent/src/team/planner.ts`（v1 快照注入点与 base prompt）
- `projects/game/agent/src/team/compress.ts`（v1 压缩刷新边界）
- `projects/game/agent/src/memory-client.ts`（v1 gRPC client，迁移样板）
- `projects/game/agent/src/skill/memory/SKILL.md`（v1 guidance，不迁移对象）
- `projects/game/game.proto`（MemoryService/Memory 资源模型）
- `projects/game/memory/`（memory Go 服务：handler/domain/mongo）
- `common/js/dsh-plugins/saolei/src/index.ts`（工具插件结构先例：defineTool/schema 约束/output 形态/guidance section）
- `common/js/dsh-plugins/saolei-loop/src/index.ts`（agent-scoped persona section 与服务注册先例）
- `projects/game/agent_v2/README.md`（agent_v2 现状：组合清单/物化/preset 存储）
- `specs/039-planner-memory-calibration/`（v1 memory 的 spec 契约）
- `specs/051-agent-v2-dsh-migration/spec.md`（Q1/A1/FR-019：memory 服务保留决策）

仓库内（dsh 物化源码，0.1.1-rc.2 线）：

- `node_modules/.pnpm/@deepseek-ai+dsh-system-prompt@0.1.1-rc.2_@deepseek-ai+cordis@4.0.1_@deepseek-ai+dsh-in_8b3c61fdc6e580f4203fa0711b2dab74/node_modules/@deepseek-ai/dsh-system-prompt/lib/types/index.d.ts`（PromptSection.text 双形态、section()/assemble()、空 section 丢弃由 renderPrompt 承担）
- 同包 `README.md`（装配管线与 API 面）
- `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent/lib/types/index.d.ts`（AgentSetup async 契约、setup 先于首次装配）
- `node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_ecf90c249e2d9dbb5492af64a9f4bd5d/node_modules/@deepseek-ai/dsh-agent-loop/lib/index.js`（preStep 每 step 装配）
- `node_modules/.pnpm/@deepseek-ai+dsh-agent-presets@0.1.1-rc.2_5200ead8959daeaefdf3dd69ba905368/node_modules/@deepseek-ai/dsh-agent-presets/README.md`（standing mount/per-agent 状态/KV cache 效应/mount rejects）

仓库内（前置调研）：

- `survey/deepseek-harness-team-mode.md`（双 agent 拓扑、team 广播格式、堆叠基线 §9）
- `survey/planner-memory-and-agent-communication.md`（v1 设计依据 D1–D6、hermes 冻结快照与压缩刷新）
- `survey/deepseek-harness-preset.md`（装配管线 §6.2、所有权原则 §6.4）
- `survey/deepseek-harness-agent-loop-prereq.md`（官方 agent-loop 机制、工具管线）

仓库外：

- https://hermes-agent.nousresearch.com/docs/user-guide/features/memory（hermes frozen snapshot 模式）
- https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py（hermes 单工具 add/replace/remove 语义）
- https://github.com/nousresearch/hermes-agent/issues/17251（压缩摘要贬低 memory 的坑）
- https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/core/system-prompt/README.md（官方 system-prompt 文档，与本地物化版本同线）
