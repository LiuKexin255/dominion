# 调研：DeepSeek Harness（dsh）agent 框架

> **状态**：调研完成，尚未做出任何采用/重构决策
> **日期**：2026-08-14
> **目标服务**：`projects/game/agent/`（仅作现状对照记录）
> **范围**：dsh 框架架构（Cordis 底座、plugin/profile/bundle 机制、WebUI 与框架关系、TUI 插件安装机制、嵌入/驱动方式、subagent 机制），以及 dsh 与 pi（earendil-works）的框架对比
> **说明**：本文为纯调研材料，记录框架事实与本地代码现状差异；不含采用决策、迁移方案或未来方向设计。

---

## 1. 背景与调研问题

本次调研围绕以下问题展开：

1. dsh 的 WebUI 与 harness 框架的关系是什么？是否绑定？社区 TUI 插件（dsh-cc-tui）如何与框架和 WebUI 共存？
2. TUI 插件安装在哪里？安装在 dsh 应用上，还是以 profile 为单位安装、按 profile 启动？
3. dsh 提供哪些嵌入/驱动方式？（作为 `projects/game/agent/` 的 agent 框架的可行性输入信息之一）
4. dsh 与 pi（https://github.com/earendil-works/pi）框架有什么区别？

调研信息源：

- dsh 仓库：https://github.com/deepseek-ai/deepseek-harness（MIT，~89.6k stars，developer preview）
  - [README](https://github.com/deepseek-ai/deepseek-harness/blob/master/README.md)
  - [docs/architecture.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md)
  - [docs/development.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/development.md)
  - [packages/boot/app-boot/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/boot/app-boot/README.md)
  - [apps/cli/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/apps/cli/README.md)
  - [packages/bundle/base/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/bundle/base/README.md)
  - [packages/bundle/headless/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/bundle/headless/README.md)
  - [docs/user/guide/index.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/index.md)（Web UI 指南）
  - [docs/user/guide/python-sdk.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/python-sdk.md)
  - [examples/jsonrpc-agent/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/examples/jsonrpc-agent/README.md)
  - [docs/subsystems/subagent.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/subsystems/subagent.md)
- dsh-TUI 插件：https://github.com/ccch1mneyyy/dsh-TUI（MIT，~804 stars，npm 包名 `dsh-cc-tui`）
  - [README](https://github.com/ccch1mneyyy/dsh-TUI/blob/main/README.md)
  - [docs/getting-started.md](https://github.com/ccch1mneyyy/dsh-TUI/blob/main/docs/getting-started.md)
- pi 仓库：https://github.com/earendil-works/pi（MIT，~90.2k stars）
  - [README](https://github.com/earendil-works/pi/blob/main/README.md)
  - [packages/coding-agent/README.md](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/README.md)
  - [packages/agent/README.md](https://github.com/earendil-works/pi/blob/main/packages/agent/README.md)
  - [packages/ai/README.md](https://github.com/earendil-works/pi/blob/main/packages/ai/README.md)
- Cordis 框架：https://github.com/cordiverse/cordis （设计论文 [*A Programming Paradigm for Spatiotemporal Composability*](https://github.com/cordiverse/paper)）

---

## 2. dsh 总体架构

### 2.1 定位与底座

dsh 是 DeepSeek 开源的 agent harness，底座是 Cordis 插件框架（服务 + 类型化事件 + 可撤销 effect 注入共享 context）。核心理念是 **"Everything is a Plugin"**：模型适配器、工具注册表、会话日志、乃至 agent loop 本身全部是插件，没有特权核心——扩展方式是在其他插件旁边挂载自己的插件，注册是 effect，插件卸载时自动回滚（[architecture.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md)）。

**成熟度状态**：developer preview，README 明示 "**THERE WILL BE COMPATIBILITY-BREAKING CHANGES**"；仓库 12,000+ commits，迭代极快。

### 2.2 Profile 与 Bundle 组合机制

两个核心组织概念（[app-boot README](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/boot/app-boot/README.md)）：

| 概念 | 定义 |
|---|---|
| **bundle** | 一个 npm 包，manifest 声明 `dsh: { bundle: { patch: "./cordis.patch.yml" } }`，携带一组 Cordis 配置行 + 它们挂载的代码，是 Cordis 配置行及其代码的分发格式 |
| **profile** | `$DSH_HOME/profiles/<name>/`（默认 `~/.dsh/profiles/<name>/`）下的目录：`package.json`（`dsh.profile` manifest 声明**有序 bundles 列表** + out-of-tree 插件依赖）+ 用户自己的 `cordis.patch.yml` |

组合顺序（对空 entry list 逐层 patch）：

```
各 bundle 按 dsh.profile.bundles 列表顺序
  → profile 的 cordis.patch.yml
  → home 级 $DSH_HOME/cordis.patch.yml
  → --patch overlay
```

关键语义：

- patch 按 id 目标行**整段替换**该行的 config（不深合并）；覆盖时需复述保留的字段。
- bundle 名先从 dsh 安装内解析（`@deepseek-ai/dsh-base`、`dsh-web-app`、`dsh-headless`），再从 profile 自己的 `node_modules`（pnpm 安装 out-of-tree 插件）。
- `healProfilesModuleFallback` 维护 `$DSH_HOME/profiles/node_modules` 扁平 symlink 目录，使裸包名可通过 Node 普通父级查找解析。
- `web` 与 `headless` 是随产品发布的 profile 模板，首次使用自动初始化；其他 profile 必须通过 `dsh plugin` 创建。
- 用户 patch 层支持 HMR：`watchUserPatches` 监听 patch 文件变更并事务性重组合。
- `dsh --profile <name> --dump-config` 可离线查看实际启动的组合树。

### 2.3 核心包与 ctx 键

| Package | Owns | `ctx` key |
|---|---|---|
| `core/session` | append-only `SessionEvent` 日志与内存 store | `ctx.sessions` |
| `core/system-prompt` | prompt section 与 tool schema 组装 | `ctx.systemPrompt` |
| `core/tools` | scoped 工具注册表与受护栏的执行管道 | `ctx.tools` |
| `core/agent` | `Agent` 接口、live registry、`agent/*` 事件 | `ctx.agents` |
| `core/agent-loop` | 默认驱动（实现该接口） | `ctx.agentLoop` |
| `llm/llm` | 消息与流式词汇表 + adapter seam | `ctx.llm` |

注意 `core/agent` 与 `core/agent-loop` 是**接口与实现分离**：`Agent` 是接口，`agent-loop` 是默认驱动——即 loop 本身可被插件替换。

### 2.4 事件体系与 Turn 流程

事件是主要扩展点，分三个域：

- **Session 事件**：durable 事实 append 进日志并经 `session/event` 广播（用于必须 survive reload 的事实）。
- **Agent 事件**（`agent/*`）：携带 live `Agent`（inbox、step、status、request、validation、continuation），用于观察/拦截在途工作。
- **Capability 事件**：把策略与适配器挂到 seam（`fs/*`、`tools/*`、`telemetry/*`），无需 import loop。

Turn 流程（一个 step = 一次模型请求 + 其工具调用；一个 turn = 零或多个 step）：

```
turn/start
  claim next-step input + 一条排队消息
  组装 prompt sections + tool schemas
  -> agent/pre-step          reject | enter(messages)
  step/start
  derive model history from the log
  agent/request -> llm/stream -> assistant/chunk* -> assistant/message
  tool/call* -> tools/pre-execute -> tools/execute -> tools/post-execute -> tool/result*
  step/end
  -> agent/turn-stopping
turn/end
```

其中 `agent/pre-step`、`agent/request`、`llm/stream`、三个 `tools/*` 是 waterfall（listener 必须调 `next()` 委托）；`agent/turn-stopping` 串行无 `next()`。

**输入通过单一 inbox 到达 driver**：某些消息立即唤醒；injected context 在 inbox 中等待下一条消息。`agent/pre-step` 决定模型看到什么（listener 可改写 claimed 消息或直接 reject）。

### 2.5 会话日志（Session Log）

会话日志是模型所见上下文的**真源**：

- `deriveMessages()` 从日志投影 model history；raw `assistant/chunk` 事件保留 replay 与 UI 保真度。
- Fork、resume、transcripts、telemetry、persistence 全部从该流派生。
- **不变量 "Model-visible means logged"**：任何到达模型请求的内容必须可从日志重建（有运行时断言）；新增 model-visible 输入需要新的 session event（扩展 `SessionEventMap` 并从日志渲染）。

### 2.6 Capability Seams（能力接缝）

**seam** = 可交换能力，含三个角色：**Service Definition**（声明接口）、**Service Provider**（实现）、**Consumer**（使用，常见为 model-facing tool）。一个包可兼任角色，但单一角色不构成 seam；新增能力意味着三者都设计。

典型例子：filesystem 与 subprocess provider 共享一个 execution world——指向远程 sandbox 即可把 Bash、PTY、LSP 一起迁移，无需 provider 分叉。

架构文档的 "Where new behavior goes" 表给出全部扩展点映射（新增 model provider → `ctx.llm`；新增 model-facing 工具 → `ctx.tools`；新增 shell → `ctx.shell` backend；拦截请求/工具/turn → `agent/*`/`tools/*` 事件；新增 UI → 驱动 `ctx.agents` 并从 `session/event` 渲染；等等）。

### 2.7 Subagent 机制

（[docs/subsystems/subagent.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/subsystems/subagent.md)）

subagent seam 让 agent 把工作委派给子 agent。与其他 capability seam 的区别：**多个 provider 实现可在一个 context 中共存**，按名注册（`ctx.subagents`）；bash 只允许一个 executor。

关键机制：

- **start-time capability 检查**：provider 在静态 descriptor 上声明能力（outputSchema/depthLimit/toolFilter/persona），service 在 start 前检查，缺失则 loud reject（`UNSUPPORTED_CAPABILITY`），绝不 accepted-then-ignored。
- **one-shot vs continuable**：one-shot 是 `SubagentRun`（发布后消费者 await `result` 并 dispose）；**continuable** 是 durable child Session + 至多一个进程本地 Activation，continuation manager 拥有激活准入、直系父授权、live 所有权图、cold resume、child-first 释放——Agent loop 拥有全部 turn 排序与执行。
- **Agent inbox 是唯一队列**：每条 continuation message 是一个 `Agent.followup()` FIFO turn；follow-up 不能重定向进行中的 turn。
- 提供者：`spawn-in-process`、`fork`、`acp`、`codex`、`claude-code`、`dsh-sdk`。
- **委派深度**是 durable `SessionHeader.delegationDepth` + merge-extensible `AgentOptions.subagentDepth`；**fork seeding** 使用 `CreateAgentOptions.seed`（父日志的 balanced completed-turn prefix）。

注意：这是**父子委派模型**（delegation），不是图执行模型——无 LangGraph 式的条件边/checkpointer 等价物。

### 2.8 dsh-base bundle 内容

`dsh-base` 是每个 profile 的第一层 bundle：模型适配器、共享 agent-default-model 选择、工具、持久化、策略（sandbox/approval）、设置/凭证、telemetry、host 级 subagent providers。patch 文件内按平台门控两套 shell 栈（POSIX bash / win32 pwsh）。Codex 与 Claude Code provider 默认 dormant 加载。

该 bundle 深度面向 **coding workspace 场景**（fs/shell/sandbox/approval/str_replace_editor 等）。

---

## 3. WebUI 与框架的关系（调研问题 1）

**WebUI 不是框架的绑定部分，而是叠在基础层上的一个可替换 bundle。** 层次：

```
dsh-base  (每个 profile 的第一层)
   ↑
   ├── + dsh-web-app   → "web" profile      (浏览器应用；dsh web = dsh --profile web 的别名，默认 :3080)
   ├── + dsh-headless  → "headless" profile (一次性 runner，无 server，跑完打印最终答案退出)
   └── + dsh-cc-tui    → "cc-tui" profile   (社区 TUI 插件，见 §4)
```

事实依据：

- `dsh web` 只是 `dsh --profile web` 的别名（[apps/cli/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/apps/cli/README.md)）。
- headless bundle README 明确说明它 "mounts no Host, HTTP server, Web runtime, or browser plugin"——即**去掉 WebUI 后框架照常工作**，UI 与内核彻底解耦。
- Web UI 指南（[docs/user/guide/index.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/index.md)）描述的流程是：启动 server → Settings→Models 配置 key → 选择 workspace（session composer 在未选 workspace 前不可用）→ 开会话发任务；审批由活动权限策略下的 Web UI 弹出。

**TUI 插件与框架和 WebUI 的"共存"方式：不共存于同一进程，而是各占一个 profile。**

- TUI 安装创建的 profile 里 bundle 层**只有 dsh-base，没有 dsh-web-app**；TUI 作为 out-of-tree 插件，用自己的 `cordis.patch.yml` patch 层插入 TUI 前端、Agent preset 名册、SQLite 会话持久化、工作状态行等。
- 两者共享**同一份服务接口与数据**：TUI 只做交互与呈现，订阅 `session/event` 事件流做投影渲染；模型调用、工具执行、fork/resume、compaction、持久化全部由 dsh-base 服务拥有。dsh-TUI 工作状态行消费的 `activity/status` 事件来自与 Web UI 共用的 `dsh-working-activity` 数据源；0.3.7 起 `/resume` 改用与 `dsh web` 共享的 JSONL 会话库。
- dsh-TUI 的运行链路自述：`dsh profile -> dsh-base -> dsh-TUI Cordis patch -> Agent preset + DSH services -> session/event -> Channel projection -> React components -> ported Ink/Yoga renderer -> terminal`。
- "Everything is a Plugin" 的直接体现：WebUI、TUI、headless 是同一内核的三种可互换 surface，互不感知，按 profile 选择启动哪一个。

---

## 4. TUI 插件安装机制（调研问题 2）

TUI 插件**不是装在"dsh 应用本体"上，而是以 profile 为单位安装**。`dsh plugin --profile cc-tui add dsh-cc-tui` 的实际动作（[dsh-TUI getting-started](https://github.com/ccch1mneyyy/dsh-TUI/blob/main/docs/getting-started.md)）：

1. 在 `$DSH_HOME/profiles/cc-tui/` 初始化 profile（未设置 `DSH_HOME` 时默认 `~/.dsh`）；
2. profile 第一层 bundle 设为 `@deepseek-ai/dsh-base`；
3. **在 profile 目录内**通过 pnpm 安装 `dsh-cc-tui`；
4. 读取包内 `dsh.bundle.patch` 元数据，将 `cordis.patch.yml` 追加为组合层。

启动：`dsh --profile cc-tui`，组合顺序为 `dsh-base → dsh-cc-tui patch → 用户 profile patch`。用户自定义覆盖写在 `$DSH_HOME/profiles/cc-tui/cordis.patch.yml`。

要点：

- 同一机器上 `web`、`headless`、`cc-tui` 乃至任意多个 profile 并存；插件隔离在各自 profile 目录内，互不影响。
- 更新用 `dsh plugin --profile cc-tui add dsh-cc-tui@latest`（不带 `@latest` 时 pnpm 按 profile package.json 已记录的范围就地解析）。
- 用户覆盖层 `cordis.patch.yml` 在更新中原样保留。
- `config` 块是**整段替换不是逐字段深合并**。
- 前置依赖：Node `^22.19 || >=24`、官方 dsh CLI、pnpm **10+**（pnpm 9 的传递依赖提升行为不同会导致启动即退，见该仓库 issue #60）、TTY、`DEEPSEEK_API_KEY`。
- dsh-TUI 不实现独立沙箱，使用当前 DSH profile 的文件/Shell/sandbox/approval 策略；因不消费审批流，DSH 的 `/permission` 未适配。

---

## 5. dsh 的嵌入/驱动方式（调研问题 3 输入）

官方支持的接入形态（均为事实记录，按与自有服务进程的关系排列）：

| 方式 | 形态 | 说明 |
|---|---|---|
| `dsh --profile <name>` | 独立 CLI 进程 | 按 profile 组合启动；`headless` 变体接受一个任务参数，跑完输出最终答案退出（exit code 由最后一个 `turn/end` 是否完成决定），不开放监听端口 |
| **Python SDK** | 子进程 runtime | `pip install deepseek-harness-sdk`；`DeepSeekHarness(provider, model, cwd, session_root, cordis=...)` 上下文管理器 + `harness.run(task, session_id=...)`；自带 bundled runtime，目标机器无需系统 Node.js；同一 harness + 同一 session id 复用会话持有的持久 Bash 进程 |
| **ACP** | stdio JSON-RPC server | Agent Client Protocol 自动化 server，暴露全新 agent 会话，支持 session/permission/cancellation（examples/acp-agent） |
| **`--mode` SDK（Node）** | Node 库 | dsh 侧无直接等价物，见下行 `boot()` |
| **Node `boot()` 自组** | 库级组装 | `@deepseek-ai/dsh-app-boot` 导出 `boot()`/`loadProfile()`/`initProfile()`/`composeEntries()`/`watchUserPatches()` 等；官方 `dsh` CLI 与 ACP demo bin 即是"thin self-executing composition over these helpers"；外部调用者需提供 Loader 可选 native helper（`node-addon-require-builtin`）或保证插件可被普通 Node 解析 |
| **JSON-RPC agent 组合** | 配置裁剪示例 | examples/jsonrpc-agent：无人值守组合，故意不加载任何 terminal UI/console logger/approval UI/user-questions 工具（stdout 属于 SDK 协议、turn 由 SDK 驱动）；模型面工具固定为 bash(前台)/read/write/edit/subagent/todo_write；证明"裁剪组合"是官方支持用法 |

jsonrpc-agent 的 minimal 变体组合性质（记录供理解裁剪粒度）：系统提示词经 `DSH_SYSTEM_PROMPT` 注入；不挂载 context-compaction 插件；组合 local PTY、bare `fs-local` backend、`danger-full-access` 策略、未压缩 JSONL 持久化；该策略下 Bash 与绝对路径编辑可修改 runtime 进程可见的任何路径，官方文档建议仅在一次性 checkout 或容器内运行。

插件在自定义组合下的可安装性（事实记录）：

- profile 路线：`dsh plugin --profile <name> add <pkg>` 对自定义 profile 完全可用。
- `boot()` 自组路线：app-boot 导出完整 profile 机制原语，可在自有服务进程内复用整套 "profile + out-of-tree 插件 + 用户 patch 层 + HMR" 能力。
- UI 类插件消费 dsh-base 的服务面（`session/event`、`ctx.agents`、approval 流等）：组合若保留 dsh-base（或等价服务行），此类插件可用；大幅裁剪则部分功能退化（dsh-TUI 不消费审批流即为先例）。

---

## 6. 与 `projects/game/agent/` 现状对照（事实性差异记录）

本地现状（调研时点）：

- 技术栈：LangChain/LangGraph（`projects/game/agent/package.json`：`@langchain/langgraph`、`@langchain/anthropic`、`@langchain/openai`、`@langchain/mcp-adapters`、`@modelcontextprotocol/sdk` 等）。
- 架构：TEAM 架构（specs/031）——player/planner 双 agent 的 LangGraph team graph（条件边处理 `gameEnded`、`MemorySaver` checkpointer 以 `thread_id = sessionId` 保对话连续性）；`projects/game/agent/src/turn-loop.ts` 实现 spec 030 的 single-flight + FIFO 队列语义（in-flight turn 不吸收外部缓冲消息；turn 完成时全部排队消息合并为一个聚合 `HumanMessage` 作为下一 turn）。
- 服务形态：长驻多会话 gRPC 服务（`projects/game/agent/src/server.ts`：TeamService bidi 流，`SessionTeamStore` 每会话一图；`projects/game/agent/src/bootstrap.ts` 为容器入口，OTel 先行初始化）。
- 工具：saolei/memory 以 MCP server 形态自建（`projects/game/agent/src/mcp-host.ts`）。

逐维度差异：

| 维度 | `projects/game/agent/` 现状 | dsh 提供 |
|---|---|---|
| 框架底座 | LangChain/LangGraph 图执行 | Cordis 插件树 + 自研 agent loop（step/turn + append-only session log），与 LangChain 生态无关 |
| 多 agent 协作 | LangGraph team graph（条件边、checkpointer、`thread_id` 续跑） | subagent seam（父子委派：continuable child session + FIFO followup + report 回传）；无图执行等价物 |
| 排队输入 | spec 030：single-flight + FIFO；in-flight turn 不吸收新消息 | 单一 agent inbox（claim/排队模型；`agent/pre-step` 决定模型所见）；pi 侧另有 steering/follow-up 双队列对照（见 §7） |
| 服务形态 | 长驻多会话 gRPC 服务 | 无现成长驻多会话 server 形态：web=单用户本地应用、headless=一次性进程、ACP=stdio JSON-RPC、Python SDK=子进程 runtime；多会话并发需自行设计 |
| 工具接入 | MCP server（saolei/memory）经 `@langchain/mcp-adapters` 接入 | `ctx.tools` 注册制；自带 MCP client（工具以 `mcp__<server>__<tool>` 命名挂载第三方 MCP server） |
| 安全/策略 | 服务跑在容器内（`projects/game/agent/service.yaml`），无进程内审批 | base bundle 内建 fs/shell sandbox + approval 流 + 权限策略（`danger-full-access`/`workspace-write`/`read-only`）；无人值守场景需显式配置策略或裁掉 approval（dsh-TUI 即不消费审批流的先例） |
| 依赖管理 | 本仓库 TS 依赖统一在 `pnpm-workspace.yaml` catalog + Bazel 构建 | dsh 生态按 profile 目录 pnpm 安装；Node `^22.19 || >=24`；`boot()` 路线依赖 Loader 可选 native helper |

---

## 7. dsh 与 pi 对比（调研问题 4）

### 7.1 一句话定位

- **dsh**：插件框架优先的 harness——底座是 Cordis 组合框架，整个产品（含 agent loop、模型适配、会话日志）都是配置组合出来的插件，产品本身是该框架的一个参考组装。
- **pi**：库优先的最小化工具箱——一组可独立使用的 npm 库（`pi-ai` 统一 LLM API / `pi-agent-core` agent 运行时 / `pi-tui`），加一个"故意做得极小"的 coding agent CLI，一切高级能力靠 TypeScript 扩展模块回调挂上去。

两者 star 数相当（均 ~90k），但组合哲学处于两个极端。

### 7.2 对比表

| 维度 | dsh | pi |
|---|---|---|
| **扩展模型** | Cordis 插件：服务/类型化事件/可撤销 effect 注入共享 context；注册即 effect，卸载自动回滚；配置树组合（bundle → profile → patch 层） | TypeScript 扩展模块：`export default (pi: ExtensionAPI) => { pi.registerTool(...); pi.on("tool_call", ...) }`，应用级回调，非组合框架 |
| **agent loop 地位** | loop 本身是插件（`core/agent-loop` 是 `Agent` 接口的默认驱动，可被替换） | loop 是库核心（`pi-agent-core` 的 `Agent`/`agentLoop`），扩展只能经 hook（`beforeToolCall`/`afterToolCall`/`shouldStopAfterTurn`）干预，不能替换 |
| **内置能力** | 电池齐全：sandbox/approval/权限策略、MCP client、subagent seam（多 provider 共存）、会话持久化、compaction、telemetry | 刻意极简：**无 MCP、无 subagent、无权限弹窗、无 plan mode、无内置 todo、无后台 bash**（官方 Philosophy 一节明示） |
| **安全模型** | 内建 fs/shell 沙箱 + 审批流 + 权限策略插件 | 无内建权限系统，以进程权限运行；官方建议容器化隔离（Gondolin micro-VM / Docker / OpenShell 三种模式） |
| **LLM 接入** | `ctx.llm` adapter seam；DeepSeek 为主 + OpenAI 兼容端点 | `pi-ai` 独立统一多 provider LLM 库（30+ provider、统一流式事件、成本追踪、跨 provider 中途切换、constrained sampling） |
| **会话模型** | append-only `SessionEvent` 日志（"model-visible means logged" 不变量），fork/resume/compaction 全部从日志投影 | JSONL **树状**会话文件（`id`/`parentId`），原地分支 `/tree`、`/fork`、`/clone` |
| **排队输入** | 单一 agent inbox（claim 模型） | steering（工具执行间隙插入）/ follow-up（全部工作结束后投递）双队列，`one-at-a-time`/`all` 两种投递模式 |
| **嵌入方式** | Python SDK（bundled runtime）、Node `boot()` 自组、ACP（stdio JSON-RPC）、`dsh --profile` | SDK（`createAgentSession` + 可替换 runtime）、RPC 模式（stdin/stdout JSONL）、`-p`/`--mode json` |
| **包分发** | npm 插件装进 profile（`dsh plugin --profile x add pkg`），按 profile 隔离 | Pi Packages（npm 或 git，`pi install npm:...`/`git:...`），装到 `~/.pi/agent/` 或项目本地 `.pi/` |
| **治理/成熟度** | DeepSeek 官方维护，developer preview，明示破坏性变更，12k+ commits | 小团队精维护（新贡献者 issue/PR 默认自动关闭），依赖全 pinned + `min-release-age=2` + `--ignore-scripts` 供应链加固，5.6k commits |

### 7.3 三个本质架构分歧

1. **组合发生在"配置"还是"代码"层。** dsh 的运行形态是启动时由 patch 层组合出的插件树，`cordis.patch.yml` 是一等公民——换模型适配器、裁剪工具集、换 UI 都是改配置行（jsonrpc-agent 示例即纯配置裁剪产物）。pi 的组合发生在使用者代码里：`new Agent({ tools, convertToLlm, transformContext, ... })`，import 什么库、传什么回调，产品就是什么——没有运行时配置组合层。

2. **"可替换的边界"画在哪里。** dsh 把可替换边界画得极深：loop、session log、模型适配全部可换，代价是任何东西都要理解 Cordis 的 context/service/event 模型。pi 把边界画得极浅但极稳：loop 核心不可换（只能 hook），换来的是 `pi-agent-core`/`pi-ai` 可脱离 coding agent 完全独立使用（可只用 `pi-ai` 做纯 LLM API 层）。dsh 的对应物是底层 Cordis 框架，而非 dsh 包本身。

3. **电池齐全 vs 有意裸配。** pi 明确反对 MCP（"用带 README 的 CLI 工具代替"）与权限弹窗（"跑容器里"）。dsh 相反：MCP client、subagent、sandbox、approval 全在 base bundle 内。对以 MCP server 形态提供能力的服务（如本地 saolei/memory MCP），dsh 原生消费 MCP；pi 路线需把 MCP client 做成扩展或改写为直接工具注册。

### 7.4 生态交叉事实

dsh-TUI（社区 TUI 插件）的上下文进度条与 TPS 仪表算法分别参考 pi 生态的 pi-nano-context 与 pi-tps-meter 实现；其工作状态行消费的 `dsh-working-activity` 事件与 dsh Web UI 同源。两个生态在 UI 组件层面已有互相借鉴。

---

## 8. 风险与限制记录（调研发现，非决策）

dsh 侧：

1. **成熟度**：developer preview，官方明示将有兼容性破坏变更；patch 行配置随版本演进可能要求 restate（整段替换语义）。
2. **profile/patch 语义**：id-targeted patch 不深合并，覆盖需复述全部保留字段；bundle 更新后自定义组合存在维护面。
3. **Node/工具链**：Node `^22.19 || >=24`；pnpm ≥10（TUI 场景）；`boot()` 自组需 Loader 可选 native helper（`node-addon-require-builtin`）或保证插件可被普通 Node 解析。
4. **无人值守审批**：approval 流在无人值守场景需显式配置策略或裁掉，否则可能等待审批卡住会话。
5. **base bundle 面向 coding workspace**：fs/shell/sandbox/approval/str_replace_editor 对非 coding agent 是冗余行，裁剪组合本身是一项组合维护工作。

pi 侧：

1. **核心 hook 面窄**：loop 不可替换，深度定制最终可能需要 fork 内核。
2. **无内建 MCP client**：MCP 能力需扩展实现或改写工具注册方式。
3. **无内建权限系统**：依赖容器化/沙箱外部隔离。

---

## 9. 引用来源汇总

仓库外（全部为官方文档/仓库）：

- https://github.com/deepseek-ai/deepseek-harness （README / docs/architecture.md / docs/development.md / packages/boot/app-boot/README.md / apps/cli/README.md / packages/bundle/base/README.md / packages/bundle/headless/README.md / docs/user/guide/index.md / docs/user/guide/python-sdk.md / examples/README.md / examples/jsonrpc-agent/README.md / docs/subsystems/subagent.md）
- https://github.com/ccch1mneyyy/dsh-TUI （README / docs/getting-started.md）
- https://github.com/earendil-works/pi （README / packages/coding-agent/README.md / packages/agent/README.md / packages/ai/README.md）
- https://github.com/cordiverse/cordis 、https://github.com/cordiverse/paper （Cordis 框架及其设计论文）

仓库内（现状对照引用）：

- `projects/game/agent/package.json`
- `projects/game/agent/src/bootstrap.ts`
- `projects/game/agent/src/server.ts`
- `projects/game/agent/src/turn-loop.ts`
- `projects/game/agent/src/mcp-host.ts`
- `projects/game/agent/service.yaml`
- `specs/030-queued-chat-input/`、`specs/031-team-template-mode/`（turn-loop.ts 注释中引用的 spec 来源）
