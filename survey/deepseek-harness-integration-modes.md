# 调研：DeepSeek Harness（dsh）集成模式与 A 模式扩展性

> **状态**：调研完成，尚未做出任何采用/重构决策
> **日期**：2026-08-15
> **前置调研**：`survey/deepseek-harness-framework.md`（§5 嵌入/驱动方式）、`survey/deepseek-harness-preset.md`（preset 机制与 "agent" 概念解构）
> **范围**：TS SDK 的事实修正与 Python 优先的刻意不对称、agent 框架集成拓扑分类（A/B1/B2）与社区倾向证据、dsh A 模式深挖（插件扩展面对 MCP 的增量、多 agent 实例能力、host 表面插件化证据、服务化缺口与判定）
> **说明**：本文为纯调研材料，记录框架事实与分析结论；不含采用决策、迁移方案或未来方向设计。本文 §2 修正前置调研 §5 表格中一处过时记录。

---

## 1. 背景与调研问题

前置调研（framework §5）记录了 dsh 的官方接入形态，留下三个后续问题，本次调研围绕它们展开：

1. dsh 是否只提供 Python SDK？为什么没有 TS 的？还是 TS 不需要 SDK？（进程内嵌入是否需要 SDK）
2. agent 框架的集成拓扑有哪几种——是 "harness 作为 host、插件作为 channel 与其他服务通信"（A），还是 "进程包装一个 agent harness"（B）？社区倾向哪种？
3. A 模式下，dsh 的插件能否提供超出 MCP 的交互能力？dsh host 能否启动多 agent 实例？能否通过插件让 host/service 拥有我们需要的能力（服务化）？

信息源见 §7。

---

## 2. SDK 事实修正：TS SDK 存在，Python 优先是刻意不对称（调研问题 1）

### 2.1 事实

**dsh 有 TS SDK**。[packages/sdk/](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/README.md) 是完整 TS SDK 组，[设计文档（2026-07-27）](https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/feature/2026-07-27-typescript-sdk-and-sdk-subagent-backend.md)自称 "the TypeScript twin of `python/sdk`"：

| TS 包 | 对应 Python 侧 | 职责 |
|---|---|---|
| `@deepseek-ai/dsh-sdk-protocol` | （Python 内嵌） | wire 协议具名类型 + 传输类；server 漂移编译报错 |
| `@deepseek-ai/dsh-sdk-client` | `deepseek-harness-sdk` | `DeepSeekHarness`/`HarnessSession`：spawn + JSON-RPC + 类型化错误 |
| `@deepseek-ai/dsh-sdk-jsonrpc-server` | `deepseek-harness-runtime-bin`（runtime 侧） | stdio JSON-RPC 服务端 |

历史事实（设计文档 Problem 一节）：JSON-RPC server 落地时**唯一客户端是 Python SDK**；TS SDK 为内部需求（仓库测试、自动化、subagent backend）后来补齐——"只有 Python SDK" 的印象来自这段窗口期。

### 2.2 Python 优先的根源：两条刻意不对称

**① Python 用户没有 Node，TS 用户"定义上就有 Node"。** 设计文档 Alternatives 原话："Python's carrier resolution exists to **ship wheels to users without Node**. A TypeScript consumer **definitionally has Node**...inventing a distribution story with no consumer violates the **require-current-need rule**."——Python SDK 必须携带 bundled runtime（[python/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/python/README.md)：client 经 stdio newline-delimited JSON-RPC 驱动 bundled runtime）；TS SDK 刻意不做 bundled-runtime 解析，launch 必须显式 `command`/`args`。

**② TS 有 in-process 主路线。** 同为 TS 集成，SDK 是次要选项：

```
TS 集成路线（两条）                       Python 集成路线（一条）
──────────────────────────              ─────────────────────
① in-process：import dsh-app-boot，      ① 子进程：Python SDK
   boot()/loadProfile()/composeEntries()    （bundled runtime + JSON-RPC）
   组合插件树（dsh CLI 即这些 helper 上
   的 "thin self-executing composition"）
② out-of-process：TS SDK client
   （进程隔离、测试自动化、subagent backend）
```

**SDK 存在的唯一理由是桥接进程边界**（spawn + stdio JSON-RPC）。进程内嵌入不需要 SDK——直接消费 `ctx.agents`/`session/event`/`ctx.sessions` 服务面即可；SDK 与否只是部署拓扑的推论，不是独立决策。

### 2.3 两 SDK 间刻意保留的不对称（设计文档 "deliberate asymmetries"）

| 维度 | TS SDK | Python SDK |
|---|---|---|
| runtime 解析 | 显式 `command`/`args` | 自动启动匹配的 bundled runtime |
| `env` | 替换（调用方持有凭据策略） | 合并 |
| `TurnResult` | 携带结构化 `reason` | 只暴露 `status` |
| teardown | 自持 stdin-EOF → SIGTERM → SIGKILL 阶梯（客户端在 harness context 之外） | 依赖 context |

### 2.4 有意思的副产品

TS SDK 的第一个消费者是 dsh 自己：`@deepseek-ai/dsh-subagent-dsh-sdk`——subagent backend，**子进程是完整 harness runtime**（自己的 config/持久化/工具），父子用 SDK 协议通信（harness 驱动 harness，递归组合）。印证 dsh 的路线：内部需求先行、分发故事延后（require-current-need）。

---

## 3. 集成拓扑分类与社区倾向（调研问题 2）

### 3.1 三种拓扑（"谁拥有进程"的分野）

```
A.  harness 为宿主：dsh/agent 框架拥有进程，业务能力以插件/MCP server 接入
B1. 库内嵌：业务进程拥有一切，框架是依赖（LangChain SDK 形态）
B2. 子进程/守护进程包装：业务进程拥有入口，harness 作为子进程或 sidecar 运行
```

### 3.2 社区实况证据表

| 框架/产品 | 模式 | 证据 |
|---|---|---|
| LangChain/LangGraph、OpenAI Agents SDK、CrewAI、AutoGen、Semantic Kernel、Google ADK、Vercel AI、Mastra | **B1 库** | 框架是 pip/npm 依赖装进业务服务；不存在"部署一个 LangChain 进程再把业务服务插进去"的用法 |
| **Claude Agent SDK** | **B2 子进程** | [官方 hosting 文档](https://code.claude.com/docs/en/agent-sdk/hosting)："When your code calls `query()`, the SDK spawns a separate `claude` CLI process and talks to it over stdio"；"One agent session maps to one subprocess"；TS/Python SDK 均捆绑原生二进制、semver 随包 |
| **Codex SDK** | **B2 子进程** | [官方 SDK 文档](https://developers.openai.com/codex/sdk) + [sdk/typescript README](https://github.com/openai/codex/blob/main/sdk/typescript/README.md)：TS SDK wrap `codex` CLI、spawn 后经 stdin/stdout 交换 JSONL；Python SDK 控制 app-server over JSON-RPC、pinned runtime |
| **opencode** | **B2 守护进程**（v2 补 B1） | [server 文档](https://opencode.ai/docs/server/)：client/server 分离、`opencode serve` HTTP server、SDK 由 OpenAPI spec 生成；v2 [`@opencode-ai/sdk-next`](https://opencode.ai/v2/docs/build/sdk) 提供 Effect 原生进程内托管 |
| **dsh** | **B2 + B1 双轨** | Python/TS SDK（子进程）；`boot()` 进程内组装（B1）——Claude/Codex 均不提供的路线 |
| Dify / Coze / n8n / Flowise | **A 平台宿主** | 平台拥有进程，能力以插件/节点/工具接入 |
| Claude Code / Codex 桌面用法 | **A 产品宿主** | CLI/TUI 拥有进程，MCP server/hooks 是扩展通道 |

第三方出现了统一三家子进程协议的适配层（[go-agent-agnostic](https://pkg.go.dev/github.com/j3ssie/go-agent-agnostic/sdk/opencode)：Claude=JSON-lines over stdio、Codex=JSON-RPC v2 over stdio、OpenCode=REST+SSE），佐证 B2 已是稳定品类。

### 3.3 发现

**① 数量上 B1 是绝对主流——但按品类看分层清晰。** "agent 是我服务的一个 feature"时，社区默认框架当库装进自己进程（LangChain 一族全部生态；`projects/game/agent/` 现状即此形态）。而 **harness 级产品**（Claude Code、Codex、opencode、dsh）几乎都不把 loop 作为库提供——它们的标准嵌入方式是 B2。

**② Anthropic 把 B2 的 rationale 说透**（[hosting 指南](https://code.claude.com/docs/en/agent-sdk/hosting) + [社区深评](https://o-mega.ai/articles/claude-agent-sdk-the-2026-deep-dive)）：崩溃隔离（"The agent does not lose its work because the application process died"——loop 崩溃不带走 agent 状态，transcript 在磁盘、session id 持久）；发布节奏解耦（pinned 二进制，SDK semver 即升级路径）；信任边界（子进程独占 shell/cwd/磁盘状态）；语言匹配（消费者是 Python 时 harness 是 Node——与 dsh 做 bundled runtime 的理由相同）。

**③ 最有分量：Anthropic 官方 hosting 架构 = 我们的服务形态。** 指南原文："Your application handles client requests on that port and calls the SDK internally; **the subprocess itself does not listen on the network**"——应用持有网络端口、SDK 在进程内调用、子进程不监听网络、session 按一致性哈希钉在容器（长会话容器池）。即"**长驻多会话服务包装子进程 harness**"是被官方背书的路径模板。配套件齐备：`session_store`/`SessionStore` 适配器（S3/Redis/Postgres 参考实现）解决跨主机 resume；多租户四件套（per-tenant `CLAUDE_CONFIG_DIR`、per-tenant cwd、env 控制、出口代理规则）。

**④ OpenAI 官方示范模式的组合用法**（[Codex SDK 文档](https://developers.openai.com/codex/sdk)）："If Codex is one specialist inside a broader orchestrated workflow, **run Codex CLI as an MCP server and orchestrate it with the Agents SDK**"——编排器是自己的代码（B1 库），harness 作为专家叶子以 A 形通道（MCP server 或 [`codexTool`](https://openai.github.io/openai-agents-js/guides/tools/)）挂上。即：**A 模式在实战中以"叶子"身份存在，不以"宿主"身份存在**——没人把业务服务做成插件塞进 Codex，而是把 Codex 做成一个工具挂在自己的编排器上。

**⑤ A 模式的真正主战场**是低代码平台（Dify/n8n/Coze：平台宿主+插件市场）和个人工具场景。在"后端服务构建 agent 能力"赛道，A 是倒置所有权的少数派路线。

**⑥ MCP 是分野的中立面**：能力一旦以 MCP server 暴露，A/B 之争降级为"谁当 host"的部署决策。本仓库 saolei/memory 已是 MCP server——该投资在任何 host（含 dsh、Claude Code、opencode）下保值。

### 3.4 结论

**社区倾向"你的进程包装 harness"（B）：B1 是存量主流（库），B2 是 harness 级产品 2026 年的增量标准（子进程/守护进程）；"harness 当宿主、业务做插件"（A）留给平台型产品与工具型场景。** 对我们的长驻多会话 gRPC 形态：现状 B1（LangGraph 库）；若未来要 harness 级能力，B2 是标准姿势（Anthropic hosting 模板可直接套用）。

---

## 4. A 模式深挖（调研问题 3）

### 4.1 插件交互能力 vs MCP：完整的扩展面清单

依据 [docs/architecture.md 的 "Where new behavior goes" 全表](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md) 与包结构，按能力方向整理：

| 方向 | 扩展面 | 说明 | MCP 能否覆盖 |
|---|---|---|---|
| 模型面（dsh 调你的能力） | `ctx.tools` 原生工具（defineTool DSL） | schema 进 prompt 装配、走受护栏执行管道 | 可（但经子进程协议，非进程内） |
| 模型面（你注入上下文） | `agent.inject()` | 落入下一次 admitted request——外部事件驱动的上下文注入 | ❌ |
| 人面 | `ctx.commands` | 人工命令，**不经模型 turn** 直接分发 | ❌ |
| 人面 | approval/interaction seam + ask-user 工具 | 审批流、向用户提问的完整协议（`packages/interaction`） | ❌ |
| 驱动面（你驱动 dsh） | `ctx.agents` create/resume + followup | 建 agent、续会话、给在途 agent 追加消息 | 部分（MCP 无会话语义） |
| 观察面（dsh 推给你） | `session/event`、`agent/*`、`tools/*`、遥测 | 类型化事件流，全部可订阅 | ❌（MCP 是请求-响应，无事件流） |
| UI 面 | `ConversationNodeDefinition` + keyed renderer | Web Client 聊天节点自定义 | ❌ |
| 能力底座面 | `ctx.fs`/`ctx.shell`/`ctx.terminals`/`ctx.sandbox` provider | **换 execution world**：fs+subprocess 指向远程沙箱，Bash/PTY/LSP 整体迁移 | ❌（seam 与 MCP 的本质差） |
| 定时面 | schedule 域（`packages/schedule`） | durable、session-local 定时 follow-up | ❌ |

官方示例回答"插件能加多完整的能力域"：

- **[web-schedule](https://github.com/deepseek-ai/deepseek-harness/tree/master/examples/web-schedule)**：一个 overlay 加进整个"定时提醒"域——3 个新工具（schedule_create/list/delete）+ durable 状态 + Web UI + 冷会话恢复语义，`dsh web --patch examples/web-schedule/cordis.yml` 即启用。"通过插件给 host 加能力"的官方最小完整样板。
- **[web-cordis](https://github.com/deepseek-ai/deepseek-harness/tree/master/examples/web-cordis)**：agent 检查并修改自己运行中的插件树（`packages/extensions` 提供运行时自改）。

**关键增量在"双向 + 有状态 + 事件"**：MCP 是无状态请求-响应的子进程协议；插件注册的是进程内服务 + 类型化事件 + 可撤销 effect。对游戏场景最相关的三个 MCP 给不了的能力：`agent.inject()`（游戏状态变化主动推进 agent）、事件流（agent 行为实时推给游戏服务）、execution world 替换（fs/shell 整体指向游戏状态存储）。

### 4.2 多 agent 实例：机制"是"，产品级"单用户"

机制上明确支持，四层证据：

1. `ctx.agents` 是 **live registry + create/resume factory seam**（architecture.md core 包表）；
2. preset 机制设计目标即 "one process can run several differently composed agents at once"（preset 调研 §2.2）；
3. in-process subagent provider 同进程生成子 agent（父子委派）；
4. **跨会话查询**：`dsh-host-apiproxy` 向浏览器应答 `listChildren`/`followup` 等跨会话查询（preset 设计文档）——web host 本来就同时持有多个活 agent。

容量（官方实测）：每活 agent 0.17（minimal）~1.31MB（standard），严格线性，dispose 几乎全量回收。**两个产品级缺口**：

- **idle eviction 未实现**：host 不 dispose agent，每个 touch 过的 session 保留 ~1.3MB（设计文档明示 TODO 归 host 所有）——长驻服务的核心缺口；
- **信任模型是单用户本地**：preset 特权 RPC loopback-pinned、无鉴权、无租户隔离。**多 agent 实例 ≠ 多用户服务**——前者机制在，后者产品完全没有。

### 4.3 host 表面皆插件：最强证据

[packages/host](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/host/README.md) 与 [packages/api](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/api/README.md) 的包结构——host 的每个对外表面都是插件行：

| host 表面 | 实现者 | 性质 |
|---|---|---|
| HTTP 服务器 | `dsh-host-webserver`（`ctx.webServer`，"HTTP route carrier"） | 插件行 |
| API 网关 | `dsh-host-apiproxy` + `api/gateway`（Typert RPC dispatcher） | 插件行 |
| ACP server（stdio JSON-RPC） | `packages/acp` | 插件行 |
| SDK JSON-RPC server | `dsh-sdk-jsonrpc-server` | 插件行 |
| 工作区选择器 | `ctx.directoryPicker` seam + native/browse/auto 三个可换 provider | seam + 插件行 |

含义：**"给 dsh 加一个 gRPC 服务表面"不是黑魔法，就是写一个像 ACP server 那样的 host-plane 插件**——挂进组合、消费 `ctx.agents`/`ctx.sessions`、serve 自有前端协议。且 [Typert 机制](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/api/README.md)允许插件贡献新 RPC domain（goal 域先例："the Gateway serves the goal domain as Remote endpoints whose receiver comes from a generated descriptor"）——新 domain 不需要改网关本体。

A 模式下我们的形态映射（调研性推演，非决策）：

```
dsh host 进程（web 或 boot() 自组组合）
  ├── 自写 gRPC surface 插件（像 acp 那样；消费 ctx.agents/session/event）
  ├── 游戏能力：ctx.tools 原生工具 或 mcp-client 行（saolei/memory 可直接复用）
  ├── 游戏状态 → agent：agent.inject() + 事件驱动
  ├── 审批：无人值守策略 或 转发到自有前端的 approval provider
  └── preset：player/planner 各一份组合清单
```

### 4.4 判定

| 维度 | 判定 |
|---|---|
| 插件扩展性 > MCP | ✅ 成立，且差距是结构性的（服务+事件+effect vs 请求-响应） |
| 多 agent 实例 | ✅ 进程内多活 agent + 跨会话查询机制完备 |
| 插件给 host 加能力 | ✅ host 表面本身即插件，ACP/SDK-server/web-schedule 三个先例 |
| **服务化就绪度** | ❌ 单用户信任模型（无鉴权/租户）、idle eviction TODO、未见官方 hosting/服务化指南（对比：Anthropic 为 B2 模式提供完整 [hosting 文档](https://code.claude.com/docs/en/agent-sdk/hosting)） |
| 成熟度 | ❌ developer preview、高速漂移、破坏性变更明示；A 模式等于把内部 API 当依赖面 |

一句话：**A 模式在 dsh 上的机制成立度是本次调研过所有框架中最高的——这正是 "Everything is a Plugin" 的兑现处；但走 A 模式等于"以 dsh 为应用框架做二次开发"，鉴权、多租户、生命周期管理等服务必需件全部自建，且踩在 preview 期的内部 API 上。B2 模式下这些问题不存在——这也解释了为什么连 DeepSeek 自己的所有官方表面都长在插件机制上，却没有一个 "dsh as a service" 的形态。**

---

## 5. 对 `projects/game/agent/` 的调研性含义（非决策）

1. **拓扑选项完整化**：B1（现状，LangGraph 库内嵌）；B2（SDK/ACP 子进程，Anthropic hosting 模板）；A（dsh host + 自写 gRPC surface 插件）。§4.4 判定 A 的机制成立度最高但服务化缺口最大，B2 是 harness 级能力的标准姿势但 dsh 侧无现成长驻多会话表面（SDK runtime 单连接单调用方；ACP 单会话）——若走 B2 需按"N 会话 = N 个 runtime 子进程"设计（参考 Anthropic "one session maps to one subprocess" 与容器池路由）。
2. **MCP 投资保值**（§3.3⑥）：saolei/memory 在 A/B2/现状三条路线下均可复用。
3. **进程内嵌入不需要 TS SDK**（§2.2②）：SDK 只桥接进程边界；`boot()` in-process 路线直接消费服务面。若评估 B2，jsonrpc-agent 的裁剪组合（framework 调研 §5）是子进程内容物的官方样板。

---

## 6. 风险与限制记录（调研发现，非决策）

1. **A 模式服务化自建清单**：鉴权/多租户（loopback-pinned 特权 RPC 需改造或隔离）、idle agent eviction（~1.3MB/touched session）、进程生命周期管理；均踩在 preview 期内部 API 上。
2. **B2 形态缺口**：dsh 无官方长驻多会话 serving 表面与 hosting 指南；并发模型需自行设计。
3. **SDK wire 无 cancel 方法**（2026-07-27 设计文档 Consequences）：客户端超时后服务端 turn 继续跑到进程 teardown——无人值守场景需注意。
4. **工具链错配**（延续 framework 调研 §8）：Node `^22.19 || >=24`、profile 目录 pnpm 安装，与本仓库 Bazel + catalog 模式存在集成摩擦。

---

## 7. 引用来源汇总

仓库外（官方文档/仓库）：

- https://github.com/deepseek-ai/deepseek-harness
  - packages/sdk/README.md（TS SDK 组：protocol/client/server）
  - .agents/notes/implemented/feature/2026-07-27-typescript-sdk-and-sdk-subagent-backend.md（TS SDK 决策：不对称 rationale、subagent-dsh-sdk）
  - python/README.md（Python SDK：bundled runtime、stdio JSON-RPC）
  - docs/architecture.md（"Where new behavior goes" 扩展点全表）
  - packages/api/README.md（remotes/gateway、Typert、依赖方向）
  - packages/host/README.md（webserver/apiproxy/picker seam 皆插件）
  - examples/web-schedule、examples/web-cordis（插件加完整能力域的样板）
- Claude Agent SDK：
  - https://code.claude.com/docs/en/agent-sdk/hosting（子进程架构、N 会话 N 子进程、SessionStore、多租户、容器路由）
  - https://code.claude.com/docs/en/agent-sdk/agent-loop（loop 语义）
- Codex SDK：
  - https://developers.openai.com/codex/sdk（官方定位、组合姿势："Codex as MCP server + Agents SDK 编排"）
  - https://github.com/openai/codex/blob/main/sdk/typescript/README.md（spawn CLI + stdin/stdout JSONL）
- opencode：
  - https://opencode.ai/docs/server/（client/server 分离、OpenAPI）
  - https://opencode.ai/docs/sdk/（JS/TS client）
  - https://opencode.ai/v2/docs/build/sdk（sdk-next：Effect 原生进程内托管）
  - https://cefboud.com/posts/coding-agents-internals-opencode-deepdive/（Hono/Bun、Stainless codegen、SSE 内部机制）
- https://openai.github.io/openai-agents-js/guides/tools/（实验性 codexTool）
- https://pkg.go.dev/github.com/j3ssie/go-agent-agnostic/sdk/opencode（三家子进程协议统一适配层）
- https://o-mega.ai/articles/claude-agent-sdk-the-2026-deep-dive（B2 崩溃隔离 rationale 的社区深评）

仓库内：

- `survey/deepseek-harness-framework.md`（前置调研：§5 嵌入方式、§8 风险）
- `survey/deepseek-harness-preset.md`（前置调研：preset 机制、agent 概念解构）
- `projects/game/agent/`（现状对照：长驻多会话 gRPC 服务、saolei/memory MCP）
