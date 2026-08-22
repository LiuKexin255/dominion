# 调研：DeepSeek Harness（dsh）preset 机制与系统提示词装配

> **状态**：调研完成，尚未做出任何采用/重构决策
> **日期**：2026-08-15
> **前置调研**：`survey/deepseek-harness-framework.md`（框架总体架构：Cordis 底座、bundle/profile/patch 机制、WebUI 关系、嵌入方式、与 pi 对比）
> **范围**：dsh 的 per-session agent preset 机制（定义、host/agent 两平面拆分、官方四个 preset 对比、与插件开发的协作方式）、系统提示词装配机制（persona 与 prompt section 的区别、装配管线、所有权原则）、与其他 agent 框架（opencode/Codex/Claude Code 一族）prompt 注入方式的对比、静态单作者场景下的收敛分析、preset 的价值边界与社区实践、"agent" 概念解构（槽位 × preset × 外置记忆）、与本仓库 `.opencode/agents/` 自定义 agent 的实例对照
> **说明**：本文为纯调研材料，记录框架事实与分析结论；不含采用决策、迁移方案或未来方向设计。§9 为追加调研（含社区检索），与前文同一调研会话完成。

---

## 1. 背景与调研问题

前置调研（`survey/deepseek-harness-framework.md` §5）记录了 jsonrpc-agent 的 minimal 变体组合，引出了"极简等模式是否也是插件"的后续问题。本次调研围绕以下问题链展开：

1. dsh 的"极简"等模式是插件吗？如果不是，是什么？
2. preset 与 profile 有什么区别？同一 profile 下不同 preset 有什么区别？
3. 官方四个 preset（minimal/standard/code/cordis）各自的内容差异是什么？
4. 自研插件（tools、prompt、persona、MCP）如何与 preset 协作——哪些能力能进哪个 preset？
5. prompt section 与 persona 有什么区别？多个 section 如何组织进系统提示词？与"支持 append prompt"有何区别？
6. 什么决定 prompt section 的内容？与 opencode/codex 将 tools/MCP 拼进系统提示词的方式有何区别？
7. 若不考虑动态加载/卸载（工具、MCP、skill 全部固定且每个描述只有一份），dsh 的 prompt 注入与其他 agent 框架的最终提示词还有区别吗？
8. 对组合固定的具体 agent，这套 prompt 注入机制是否还有帮助？
9. preset 对"一个 agent × 4 个 preset"的价值是什么？社区如何使用？dsh 设计 preset 想如何产生效果？（若 agent 绑定固定 preset 是否退化）
10. 本仓库 opencode 形态的 5 个自定义 agent（`.opencode/agents/`）对应 5 个 preset 吗？"dsh 只是把 agent 改名成 preset" 论是否成立？

信息源（全部为 dsh 官方仓库文档与源文件，仓库外 URL）：

- preset 机制：
  - [packages/preset/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/README.md)
  - [设计文档：per-session agent-presets（2026-08-03）](https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/architecture/2026-08-03-per-session-agent-presets.md)
- 四个官方 preset 源文件：
  - [apps/cli/config/agent-presets/minimal/agent.cordis.yml](https://github.com/deepseek-ai/deepseek-harness/blob/master/apps/cli/config/agent-presets/minimal/agent.cordis.yml)
  - [apps/cli/config/agent-presets/standard/agent.cordis.yml](https://github.com/deepseek-ai/deepseek-harness/blob/master/apps/cli/config/agent-presets/standard/agent.cordis.yml)
  - [apps/cli/config/agent-presets/code/agent.cordis.yml](https://github.com/deepseek-ai/deepseek-harness/blob/master/apps/cli/config/agent-presets/code/agent.cordis.yml)
  - [apps/cli/config/agent-presets/cordis/agent.cordis.yml](https://github.com/deepseek-ai/deepseek-harness/blob/master/apps/cli/config/agent-presets/cordis/agent.cordis.yml)
- 系统提示词装配：
  - [packages/core/system-prompt/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/core/system-prompt/README.md)
  - [packages/preset/persona/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/persona/README.md)
  - [设计文档：prompt-variables and tool-guidance ownership（2026-07-05）](https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/architecture/2026-07-05-prompt-variables-and-tool-guidance-ownership.md)
- MCP 接入：
  - [packages/mcp/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/mcp/README.md)
  - [examples/mcp-memory/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/examples/mcp-memory/README.md)
- standalone 组合（前置调研已涉及，本次复核）：
  - [examples/jsonrpc-agent/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/examples/jsonrpc-agent/README.md)
  - [examples/jsonrpc-agent/minimal.cordis.yml](https://github.com/deepseek-ai/deepseek-harness/blob/master/examples/jsonrpc-agent/minimal.cordis.yml)

---

## 2. "极简等模式"是什么：不是插件，是插件组合清单

**结论：dsh 的"模式"（minimal/standard/code/cordis）不是插件，而是一份声明式插件组合清单（`cordis.yml`）。** 没有特权代码路径、没有 mode 开关。`minimal` 与 `standard` 的区别仅是清单上写了哪些插件行、每行的 config 是什么。"极简"的实现手段是**不写行**（absence），加极少数插件自己的 config 开关（如 persona 的 `includeRuntimeContext: false`）。

该结论存在**两层机制**，需区分：

### 2.1 层 1：进程级 standalone 组合（SDK 形态）

`examples/jsonrpc-agent/minimal.cordis.yml` 是 Web `minimal` preset 的 "complete standalone counterpart"（README 原文）——不基于 dsh-base patch，从零直接列出 12 个插件行：

| 插件行 | 作用 |
|---|---|
| `sdk-jsonrpc-server` | JSON-RPC runtime（供 Python SDK 驱动） |
| `llm-deepseek` | 模型适配（`DSH_MODEL`/`DSH_CONTEXT_WINDOW`） |
| `sandbox` + `sandbox-policy` | 本地沙箱，`danger-full-access` |
| `subprocess` / `pty` / `terminal-bash` | 进程与终端栈 |
| `fs-local` | 裸本地文件系统 |
| `agent-spine-demo` | 极简关键：`includeRuntimeContext: false`、`skills.enabled: false`、`toolBash/toolJobs: false`、persona 由 `DSH_SYSTEM_PROMPT` 注入 |
| `persistent-bash` / `str-replace-editor` | 仅有的两个 model-facing 工具 |
| `sessions` | JSONL 持久化（`compression: none`） |

### 2.2 层 2：会话级组合（agent preset）——本次调研的核心

（[packages/preset/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/README.md)、[设计文档 2026-08-03](https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/architecture/2026-08-03-per-session-agent-presets.md)）

- **preset = 一个目录 + 一个 `agent.cordis.yml`**。agent factory 的 `setup(agentCtx)` 把它作为 Cordis `include` 子树挂到该 agent 的 scope context；preset 内所有注册落在该 agent 自己的层，随 agent 卸载自动 unwind。`dsh-tools`/`dsh-system-prompt` 本来就按调用方 scope 归档注册，因此**不需要给 loader 加新层级**。
- **设计动机**：此前组合由启动时的 `cordis.yml` 对整个进程固定；想让 benchmark-minimal agent 与 full coding agent 并存必须跑两个进程；旧 workaround（`apps/cli/config/minimal.cordis.yml`，`--config` overlay 禁用工具行）会同时改掉所有 session。preset 机制让一个进程同时服务多种组合的会话。
- 官方预置 4 个 preset，位于 `apps/cli/config/agent-presets/`：`code`、`cordis`、`minimal`、`standard`。目录列表即名册（文档刻意不复述名单防漂移）。
- 用户自建 preset 位于 `${DSH_HOME:-$HOME/.dsh}/.agent-presets/<id>/`；shipped preset 拒绝写/删，创作路径是"duplicate, then edit"。

### 2.3 两平面拆分（host plane vs agent plane）

| 平面 | 实例数 | 内容 |
|---|---|---|
| **Host** | 每进程 1 个 | 注册表本身（`tools`/`systemPrompt`/`agents`/`agent-loop`/`sessions`）、跨 session 设施（persistence、query、projections、storage、settings、credentials、telemetry）、subagent providers 及其共享后端、web host、**模型路由** |
| **Agent** | 每 session 1 个 | 该 agent 贡献到注册表的内容：工具插件、persona 与 prompt sections、compaction policy |

拆分判据是"**什么必须共享**"而非"什么看起来 agent 相关"：模型路由刻意留在 host——`installAgentLlmTarget` 是 per-agent 的 provider/model seam，但挂在 preset 内的 LLM adapter 永远不会被 host 平面的 `agent-loop` 解析。

反例（设计文档记录的失败教训）：把 `subagents` registry 挪进 preset 会导致 `dsh web` 起不来——`dsh-host-apiproxy` 是 host 行，注入了只有 session 才提供的服务，永远等待。

### 2.4 关键语义与护栏

- **preset id 是 durable 事实**：记录在 `SessionHeader.agentPreset` + `agent-preset/selected` 事件（blank 切换时）；`resolveSessionPreset` 解析两者之对，**resume 重建历史产生时的组合**而非今天的默认。
- **切换只允许 blank session**：一旦 turn 跑过，切换返回 `agent-preset-locked`（历史是在那个 preset 的工具集下产生的，换工具会搁浅已记录的 tool calls）。blank 切换是 unmount-then-mount，新挂载失败回滚旧 preset。
- **preset 不得发布 root service realm**：进程全局服务会在第二个 session 挂同一 preset 时碰撞；mount 时显式 reject，且包不变量在每个服务通知上复查。服务行必须包在 `cordis:group` 的 entry-local `isolate` realm 里。
- **创作是特权 RPC**：roster 的 `read`/`write`/`remove` 是 loopback-pinned（读组合=侦察、写组合=任意能力注入）；`list`/`select` 普通。
- **性能**（官方实测）：minimal 每活 agent ~0.17MB / 挂载 ~38ms；standard/cordis ~1.31MB / ~135ms；严格线性，dispose 几乎全量回收。**已知 TODO**：web host 的 idle agent eviction 未实现，每个 touch 过的 session 保留 ~1.3MB。
- **包名解析**：preset 内的裸包名（bare specifier）从 host 组合的 base 解析（用户 home 下的 preset 向上走不到 harness 的 node_modules）；相对路径随 preset 目录走。**第三方插件包必须先装到 profile/harness 能 resolve 的地方**（如 profile 的 `node_modules`）。

---

## 3. profile 与 preset 辨析（调研问题 2）

两层组合单位**正交**：

| | **profile**（进程级） | **preset**（会话级） |
|---|---|---|
| 回答的问题 | "这个**进程**长什么样？" | "这个**会话的 agent** 有什么能力？" |
| 物理形态 | `$DSH_HOME/profiles/<name>/`：package.json（有序 bundles）+ cordis.patch.yml | 一个目录 + 一个 `agent.cordis.yml` |
| 官方例子 | `web`、`headless`、`cc-tui` | `minimal`、`standard`、`code`、`cordis` |
| 决定内容 | 表面（surface）：WebUI / TUI / 一次性 runner；host 平面设施 | 工具集、persona、prompt sections、compaction 等 agent 平面贡献 |
| 切换粒度 | 换 = 换个进程启动 | 同进程内不同会话可同时用不同 preset |

**同一 profile 下不同 preset 的差异只体现在每个会话里模型拿到什么**（系统提示词 + 工具目录）；UI、模型路由、持久化、凭证是进程共享的，完全不变。

记忆口诀：**profile 选"壳"（哪个 UI/入口），preset 选"脑"（agent 被组合成什么样）**。

两个 preset 确实"像 profile"的场景（事实记录）：

1. **SDK 形态**：`examples/jsonrpc-agent/minimal.cordis.yml` 是完整进程级组合，经 Python SDK 的 `cordis=` 参数直传——角色上接近"给 SDK 进程用的 profile"，但机制上是 config 直传，不经 profile/bundle/patch 那套。
2. **历史形态**：preset 机制落地（2026-08-03）前，minimal 是进程级 `--config` overlay，一次改所有会话。

---

## 4. 官方四个 preset 对比（调研问题 3）

四个 preset 是一条**能力递增链**：

```
minimal（做减法）
standard（基准全量）
code    = standard + 1 行（tool-presentation: mode code）
cordis  = standard + 2 行（tool-cordis + 随包技能），persona 加长
```

| | **minimal** | **standard** | **code** | **cordis** |
|---|---|---|---|---|
| 定位 | 评测/benchmark 基线 | 日常全功能编码 agent | standard 的 Code Mode 版 | "agent 创作 agent"版 |
| 系统提示词 | 一句固定 persona，`complete: true` 封锁一切其他注入 | persona（`{{model}}`/`{{cwd}}` 模板）+ 各工具守则 | 同 standard | 同 standard + persona 内含大段"两平面组合编写教程" |
| 工具数 | 2（`persistent-bash`、`str_replace_editor`） | ~20 | ~20 | ~20 + 自改工具 |
| 相对 standard 的差异 | 去掉几乎所有东西 | — | 多一行 `tool-presentation` | 多 `tool-cordis` + `editing-cordis-compositions` skill（随 preset 目录携带） |
| 上下文压缩 | 无 | 有 | 有 | 有 |
| 文件访问 | 裸 fs-local（entry-local realm，绝对路径随便写），仅容器/一次性 checkout 内安全 | host 沙箱化 fs + 权限策略 | 同 standard | 同 standard |
| 内存/agent（官方实测） | ~0.17 MB | ~1.31 MB | ≈ standard | ≈ standard |

各 preset 关键差异点：

- **minimal**：persona 即完整系统提示词（`complete: true`）；PTY 与 fs 各在 entry-local `isolate` realm（`terminals`/`fs`）；无 compaction、无 skills、无 todo、无 subagent/workflow、无 plan mode、无 ask-user、无 web 工具。
- **standard**：参照系。delegation 组内 codex/claude-code 两行默认 `disabled: true`（复制本 preset 去掉 disabled 才暴露）。文件内每条注释都在示范"这个服务为什么必须在 host 平面"（jobs 注册表、goals 服务、subagents 注册表、tokenMeter 等）。
- **code**：只改"模型调用工具的方式"——工具从一次调用一个动作变为模型写 TypeScript 程序面向生成 SDK 由 `run_code` 一次执行（5 轮往返合成 1 轮）。registry 留 host 平面，preset 拥有的是该 agent 的 presentation。
- **cordis**：`tool-cordis` 可检查活插件树、挂载/卸载插件（`cordis_mount` 在运行时执行模型写的 JavaScript）。文件头明示 **TRUST: 信任边界而非沙箱——该 preset 上的会话等同于 shell 访问**。存在目的：让一个人对 agent 说"帮我写另一个 agent"。

---

## 5. 自研插件与 preset 的协作（调研问题 4）

**核心模型：preset 不是插件的"安装目标"，挂载层才是。** 插件与 preset 的协作只有两条路径，选择哪条决定影响范围：

```
挂载层                        可见范围
──────────────────────────────────────────────
host 层                       该进程所有会话（全部 preset）
（profile patch / bundle）       工具目录 = 全局层 ∪ preset 层（merge）
preset 层                     只有选了这个 preset 的会话
（agent.cordis.yml 里的行）
```

### 5.1 四类扩展的实际行为

| 扩展类型 | 挂 host 层 | 写进 preset 副本 |
|---|---|---|
| **工具插件** | 所有 preset 会话可见，**包括 minimal**（merged catalog 含全局注册） | 仅该 preset |
| **prompt section** | minimal 下被丢弃（persona `complete: true` 封锁一切后续注入）；standard/code/cordis 下正常生效 | 同左；在 minimal 副本里写 prompt 行无效，除非去掉 `complete: true` |
| **persona** | 修改 `system-prompt` 行的部署级 config | preset 里的 persona 行按 scope 遮蔽部署默认（四个官方 preset 均如此） |
| **MCP server** | patch 加一行 `dsh-mcp-client`（mcp-memory 示例路线），`mcp__<server>__<tool>` 全局可见 | 同一行的 config 写进 preset 副本，仅该 preset 可见 |

MCP 行模板（[examples/mcp-memory/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/examples/mcp-memory/README.md)）：

```yaml
- id: my-server
  name: '@deepseek-ai/dsh-mcp-client'
  config:
    serverName: my-memory
    transport: stdio        # 或 streamable-http + url/headers
    command: my-memory-mcp
    args: []
    env: {}
```

`dsh-mcp-client` 将外部 server 的工具注册到 `ctx.tools`（[packages/mcp/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/mcp/README.md)）。stdio 桥接刻意剥除凭据类环境变量与 `DSH_*` 变量；不自动重连（child 崩溃需 restart/HMR）。

### 5.2 实操配方

- **只给全功能 agent 加能力（不动 minimal）**：复制 `standard` → 副本加行 → 存 `$DSH_HOME/.agent-presets/<id>/`。
- **给所有模式加（含 minimal）**：profile patch（`$DSH_HOME/profiles/<name>/cordis.patch.yml`）或 `dsh plugin --profile <name> add <pkg>`。注意会污染 minimal 的评测纯净性（工具漏进目录；prompt 部分被 `complete: true` 挡掉）。
- **绝对纯净的 benchmark minimal**：走 jsonrpc-agent standalone 组合路线，整份进程级清单自己控制。

### 5.3 三个易踩的坑

1. **服务依赖决定工具在哪个 preset 下"活着"**：工具 inject 的服务在哪层决定解析结果。minimal 的裸 `fs-local` 与 PTY 在 entry-local isolate realm 内，realm 外不可见——host 层挂的 fs 类工具在 minimal 会话下解析到 host 的沙箱化 fs，不是 minimal 编辑器用的裸 fs。想共享 minimal 的裸 fs，必须把行写进它 `filesystem` group 内部。
2. **code preset 下工具自动获得代码形态**：presentation 行覆盖整个 merged catalog——host 层挂的工具自动出现在 `run_code` 的生成 SDK 里。
3. **分发约束**：preset 内裸包名从 host 组合解析（见 §2.4）；插件包必须先装到可 resolve 处，preset 行里才能写包名。

---

## 6. 系统提示词装配机制（调研问题 5、6）

### 6.1 persona 与 prompt section 的区别

**persona 本身就是一个 prompt section，但占用一个保留命名插槽**：

| | **persona** | **普通 prompt section** |
|---|---|---|
| 注册名 | `deployment:persona`（固定） | 任意 `name`（如 `tool:bash`） |
| 数量 | 每 agent 恰好一个（同层重名 throw） | 任意多个 |
| order | 固定 `0` | 自带 `order` 字段 |
| 语义 | "这个 agent 是谁"（identity） | "该知道/遵守什么"（事实与守则） |
| 归属 | `dsh-system-prompt` 的 config 持有部署级默认；`dsh-persona` 包存在的唯一目的是让 preset 能按 scope 遮蔽它 | 谁拥有该事实谁注册 |

[`dsh-persona`](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/persona/README.md) 自述：preset 若无此行，能改 agent 的工具、永远改不了 agent 的身份。该行 **scope-only**：在非 agent scope 挂载会与 registry 自己的 `deployment:persona` 注册碰撞、fail loud。config：`text`（模板）、`complete`（默认 false，true 则成为唯一系统提示词 section）、`includeRuntimeContext`（默认 true）。

### 6.2 装配管线

（[dsh-system-prompt README](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/core/system-prompt/README.md)）`dsh-system-prompt` 是 **registry + 装配管线**，每 step 装配一次：

```
收集：全局层 section ∪ 该 agent scope 层 section
        （同名 section：scope 层遮蔽全局层——遮蔽，不是追加）
  ↓
排序：按 order 升序，固定频段：
        -100   harness identity（"You are an AI agent powered by DeepSeek Harness."）
          0    persona（唯一插槽）
       100-199 各工具包的跨调用守则（tool:bash / tool:read / ...）
  ↓
waterfall：system-prompt/assemble 事件，监听者可协作式改写装配结果
  ↓
封口：若存在一个 effective 的 complete: true section
        → 它成为唯一 section，其余全部丢弃
        （多于一个 complete → reject，fail loud）
  ↓
渲染：严格 {{variable}} 插值（未注册变量 throw），空 section 丢弃，空行连接
```

API 面：`ctx.systemPrompt.section()` / `context()`（runtime context）/ `suppressRuntimeContext()` / `tools()`（tool schema provider）/ `variable()` / `assemble(context)`。`assemble(context)` 按 caller 合并全局层 + `context.scope` 的层。

**独立通道**：runtime context（沙箱策略快照、approval 状态等）**不进系统提示词**——渲染为带来源标注的 **user-role 快照**进消息历史。`includeRuntimeContext: false` 关闭的是这条通道，不禁用底层服务。

四个官方 preset 在管线中的样子：minimal = persona `complete: true` 封口；standard/code = persona 占 order 0 + 各工具守则 100–199 + plan mode 规则 section（进 plan mode 时出现）；cordis = standard 唯一 prompt 差异是 persona 的 text 更长（两平面教程写在 persona 里，因为那是身份的一部分）。

### 6.3 与"支持 append prompt"的区别

四样 append 表达不了的性质：

1. **遮蔽（shadowing）**：preset 挂 `dsh-persona` 行的结果是替换该 agent 的 persona，而非出现两个 persona 打架。类比词法作用域：内层同名声明遮蔽外层。append 模型下"覆盖"只能靠约定。
2. **显式排序频段，与加载顺序无关**："registration order is a plugin-load artifact"——排序规范化掉加载偶然性。直接收益：**KV cache 前缀稳定**（persona 挂载于首个请求前、文本不再变；两个不同 preset 的 agent 各自前缀稳定、互不失效）。
3. **`complete: true` 封口算子**：append 只能加，表达不了"除此之外谁都不许加"。minimal 的评测纯净性（prompt 零噪音、跨部署可复现）靠此语义。
4. **全程 fail loud**：同层重名 throw、非有限 order throw、`{{未知变量}}` throw、多个 complete reject。

### 6.4 所有权原则：什么决定 section 装什么

（[设计文档 2026-07-05](https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/architecture/2026-07-05-prompt-variables-and-tool-guidance-ownership.md)）设计决策一句话：**"every fact in the prompt has exactly one owner"**：

| 提示词里的事实 | 唯一所有者 | 载体 |
|---|---|---|
| "你是谁"（角色/行为） | 部署 / preset 作者 | persona section（order 0） |
| 产品身份 | `dsh-system-prompt` | `harness:identity`（order -100） |
| 工具的**单次调用语义**（何时用、参数含义） | 工具包 | tool schema 的 `description`（走 API tools 字段，**不进系统提示词**） |
| 工具的**跨调用习惯**（如"优先用 fs 工具而非 shell 里 cat"） | 工具包 | 该工具包的 prompt section（order 100–199） |
| 模型名 / 工作目录等运行时事实 | agent loop（事实持有者） | `{{model}}`/`{{cwd}}` 变量，严格插值 |

改造动机（Problem 一节记录的病灶，全部是**静态创作问题**而非动态加载问题）：bash/subagent/todo 守则手写在 coding-agent 和 ACP 两份 persona 里已漂移（ACP 那份被删节）；装卸工具要手工同步每个部署的 persona；persona 渲染在工具守则之后（顺序颠倒）；fork 工具复用 spawn 的描述与实际语义不符。

已知不对称（README 明示）：**按 agent 限制工具 ≠ 移除其 guidance**——"a tool restriction does not remove independently registered guidance"。section 跟随插件生命周期与 scope，不跟随每个 agent 的可见性。

---

## 7. 与其他 agent 框架 prompt 注入的对比（调研问题 6、7）

### 7.1 机制对比

典型 agent CLI（Claude Code、Codex、opencode 一族）是**核心持有的单体模板**：核心写死大模板，工具守则、MCP server 指令、环境信息由核心在固定拼接点塞入——核心是一切 prompt 文本的事实所有者，扩展只能往核心预留槽递字符串。

dsh 把所有权打散到事实持有者，单体模板给不了的性质：

1. **生命周期耦合**：注册是 effect，卸载工具插件则其守则 section 自动回滚；append 模型里移除工具后守则文本残留误导模型。
2. **反漂移**（single source of truth）：守则跟工具包走，一个包一份，随包发布/升级。
3. **描述与守则的职责切分**：单次调用语义放 schema description，跨调用习惯才进 section——MCP 工具天然只需 server 自己的 description，框架不替它编 prompt 文本。minimal 封掉 sections 后依然能用工具：工具的"怎么调"在 schema 里。
4. **严格失败**：`{{modle}}` typo 直接 throw。

### 7.2 静态单作者场景下的收敛分析（调研问题 7、8）

若不考虑动态加载（工具/MCP/skill 固定、每个描述只有一份、单作者）：

**最终提示词收敛到同一形状**，模型在 wire 层面分不出框架：

```
[system, order -100] harness/product identity
[system, order 0]    persona（含 {{model}}/{{cwd}} 插值）
[system, order 100+] 各工具跨调用守则
[API tools 字段]     bash / edit / read / mcp__x__y / skill ...
[user-role 快照]     运行时状态（沙箱策略、approval 等）
```

消融的差异（静态单作者下无意义）：排序频段、生命周期耦合、反漂移、按 agent 装配（别家对应物是权限/配置管理）、waterfall、遮蔽。

仍存的**产物级**差异（是"放哪"不是"有什么"）：

| 差异 | dsh | 典型其他 agent |
|---|---|---|
| 单次调用语义的位置 | 只放 tool schema description，不进 prompt 文本 | Claude Code 一族把大量操作手册写进系统提示词文本 |
| 动态事实的位置 | user-role 带来源快照（各家已趋同：Claude Code `<system-reminder>`、Codex environment_context） | 同类做法存在 |
| 前缀稳定性 | 排序频段 + persona 不变 → 结构保证 | 手工维护通常也稳定，但无结构保证 |

### 7.3 本质归纳

> **dsh 的 prompt 注入是"组合协议"（merge semantics，类比 CSS cascade / 分层配置合并）；opencode/codex 的是"创作约定"（核心持有模板，扩展往预留槽递字符串）。**

协议的复杂度按**独立创作方数量 N 计价**：N=1 时协议和约定产出同一个字符串，协议纯属额外复杂度；N>1（部署方 × preset 作者 × 工具包 × per-agent 定制）时协议才兑换价值。

机制价值 ≈ 必须共存的组合变体数 × 组合变更频率 × 独立作者数。dsh 是 harness（平台），N 大且不可控，所以需要协议。

**时间线佐证**（preset 不是这套机制的动机）：所有权原则落地 2026-07-05，preset 机制落地 2026-08-03（一个月后）。因果关系是反的：因为每个事实有唯一 owner、注册按 scope 归档，preset 才"顺便"获得"复制一份清单换一个 agent 人格"的能力。

---

## 8. 对 `projects/game/agent/` 的调研性含义（非决策）

延续前置调研 §6 的现状对照（LangGraph team graph、长驻多会话 gRPC 服务、MCP 自建工具），本次调研的推断性结论：

1. **我们是 N≈1 场景**（单团队、静态组合、player/planner 双 agent）：三个变化轴要么为空（无沙箱/审批状态类 runtime context 需求已由消息历史覆盖），要么任何模板机制/普通代码即可覆盖（工具随版本迭代——MCP 协议本身已把 description 所有权免费给我们；双 agent 静态组合 = 代码里两个 prompt 组装函数）。
2. **即使未来整体借鉴 dsh，理由也不会是 prompt 注入机制**——更可能的候选是会话日志不变量（"model-visible means logged"）、subagent seam、或 profile 组合分发。prompt 注入机制解决问题的前提条件（多方独立创作、组合可变）在我们这里不成立。
3. **可免费拿走的创作纪律**（不依赖任何框架）：
   - 单次调用语义放 tool description、跨调用习惯才进 prompt 文本（MCP 协议自带前者）；
   - 模型名等事实用变量引用 + 严格校验，不手写进 prompt（改配置后 prompt 不说谎）；
   - 运行时状态走消息历史（user-role 快照）而非 system prompt；
   - persona 只写身份，不写操作手册。

---

## 9. preset 的价值边界与 "agent" 概念解构（追加调研，调研问题 9、10）

### 9.1 价值单位：不是 "一个 agent × 4 个 preset"，而是 "1 进程 × N 并发会话 × 会话级选择"

三个正交的轴（信息源见 §11）：

**轴 1：同进程多会话复用。** 官方 CLI reference（[apps/cli/reference/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/apps/cli/reference/README.md)）对 minimal preset 的表述："Select 极简模式 when creating a Web session; every other prompt section and model-facing plugin remains absent from that agent **while the shared browser, workspace, persistence, sandbox, and permission host stays in place**"。[官网](https://www.deepseek.com/harness/)将 4 个 preset 定位为"四种运行模式"，**建会话时选择**：日常标准、跑分极简、批量工具编排 PTC、创作创造模式。一个 `dsh web` 进程内，session A 跑基准评测、session B 干活、session C 在创作 preset——host 设施只装一份。这也是 preset 机制的设计动机（§2.2）。

**轴 2：TUI 反例——dsh 自己承认"绑定固定组合 = 不用 preset"。** [packages/bundle/web-app/cordis.patch.yml](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/bundle/web-app/cordis.patch.yml) 注释原文："The base keeps them for the TUI, which is **single-session and composes its agent process-wide**; the Web surface disables them here and lets each session mount a preset instead."——TUI 单会话、组合进程级固定，因此 base bundle 为 TUI 保留 agent 平面行，Web surface 才禁用它们改用 per-session preset。**固定组合场景的官方处理就是退回进程级组合（profile 机制）**——"agent 绑定固定 preset 则退化" 的判断与框架作者自己的决策一致。

**轴 3：创作与分发单元（社区实践分三层）。**

| 层 | 实践 | 代表 |
|---|---|---|
| 选用 | 建会话时按任务挑模式（跑分/日常/创作） | [runoob 教程](https://www.runoob.com/deepseek-harness/deepseek-harness-start.html)、[官网](https://www.deepseek.com/harness/) |
| 多套 agent | 用 **profile**（非 preset）维护用途隔离的多套 agent（review 只读审查 / writer 文档 / sandbox 受限执行） | [jishuzhan 上手实录](https://jishuzhan.net/article/2088234397949378561) |
| 创作/迁移 | 创造模式自举（agent 写 preset 落 `~/.dsh/.agent-presets/`）；生态桥接（Claude Code/Codex/OpenCode/Pi 配置与 skills 迁移插件、pi2dsh 将 Pi package 转 DSH bundle） | [Discussion #272](https://github.com/deepseek-ai/deepseek-harness/discussions/272)、[Discussion #505](https://github.com/deepseek-ai/deepseek-harness/discussions/505) |

[docs/config-catalog.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/config-catalog.md)：preset 有 `system`/`user` 两级 trust——"a user preset was authored locally, **by a person or by an agent**, and therefore carries the same trust as shell access"。**选择 preset = 给会话授予一组能力包**（权限语义，非仅功能开关）。

**四个官方 preset 是 4 份参考实现，而非"4 种可切换模式"**：standard = 产品默认交付物；minimal = 评测基线（可复现性载体）；code = presentation 实验田；cordis = 自举工具（"复制我，然后改"是官方创作路径）。设计意图是一条**创作飞轮**：用 standard 干活 → 想定制进创造模式 → 描述需求让 agent 生成 preset → 新 preset 进 roster 下个会话可选 → 可分发。preset 让"新 agent 类型"的边际成本降到"复制目录改几行 YAML"；官方自身的新模式（如 code）也走同一条路交付——无特权分支，这是 "Everything is a Plugin" 在组织层面的含义。

### 9.2 创作飞轮的隐含前提：执行体无身份（"agent" 概念解构）

飞轮各环节显式放回 agent 后："[某 agent] 在 standard 下干活 → [某 agent] 在 cordis 下创作 → [某 agent]（新会话）穿上新 preset 使用"。若各环节是**有固定身份的不同 agent**（各自人格/记忆/工具集），则每个 agent 绑定固定组合，preset 退化为"每个 agent 的配置文件"，会话级选择失去意义——即"给飞轮每个环节安排专门 agent 则退化"。

dsh 的解法不是"保证同一个 agent 实例"，而是**把身份从执行体中抽走**。三个概念与我们（LangGraph/opencode）语境错位：

| 我们语境的概念 | dsh 语境的对应物 |
|---|---|
| agent 定义（角色/工具集/人格，如 player/planner、`.opencode/agents/*.md`） | **preset**（数据文件，可复制/分发/git） |
| agent 实例（长驻对象，如每会话一图的 team graph） | **Agent handle**（会话生灭，无 identity 字段） |
| agent 记忆/连续性 | **全部外置**：session log（会话内 + resume）、fork seed（跨 agent 继承）、skills（程序性知识）、memory MCP（陈述性知识） |

证据：`Agent` 接口上没有 identity/persona 字段（persona 是 system-prompt 组合贡献的一个 section）；`SessionHeader` 记录 `agentPreset` id 而非 agent 身份；resume 重建"历史产生时的组合"。**dsh 的 Agent 对象只是"能跑组合的槽位"**——"换 agent"在 dsh 里是新开会话 + 换一件衣服（衣服是数据文件），而非换一个服务对象。

**归纳（本次调研的核心语义结论）**：dsh 的关键语义操作是把 **"agent 定义"外置为 "preset"**，从而让 **"agent" 一词专指运行时实例（槽位）**。与"无状态服务 + 状态外置"的经典架构决策同构。三项（槽位 × preset × 外置记忆）是一体设计——只借其中一项（例如只想要 per-session 组合、不想放弃强身份 agent）拿不到完整收益。

### 9.3 与 opencode agent 定义的对照（本仓库 `.opencode/agents/` 实例分析）

本仓库现有 5 个 opencode 自定义 agent：`.opencode/agents/{checker,designer,developer,executor,reviewer}.md`。字段级映射：

| opencode agent 字段 | dsh 去处 | 备注 |
|---|---|---|
| `description` | preset id + roster | 等价 |
| 正文 markdown（persona + 操作规程） | persona 行 + prompt section 行 | 内容等价 |
| `tools: {edit: false, bash: false}` | 不写对应工具行（absence） | **语义差别**：opencode 是核心持有工具后的"拒绝"开关（核心仍是事实所有者）；dsh 是组合层"缺席"（无 schema、无守则、无执行路径）。对"reviewer 不许改代码"这类需求两者效果相同，差异只在"换实现"时兑现（如挂只读 fs provider） |
| `model` / `temperature` / `reasoningEffort` | **不进 preset**——模型路由刻意留在 host 平面（`installAgentLlmTarget` 是 per-agent seam） | **迁移摩擦点**：本仓库 5 个 agent 用了 3 种模型配置，需走 host 侧路由配置，不能随 preset 文件走 |
| `mode: primary / subagent` | **不是 preset 概念**——对应 subagent seam（`tool-subagent` 行 + provider/persona 参数） | **映射断裂点** |

概念拆分：**opencode 的一个 "agent" = dsh 的 preset（组合）+ subagent seam（委派）两个正交机制**。本仓库 executor 派发 developer/designer 的 SDD 流水线，真正对应的 dsh 能力是 subagent seam 那一半（persona 参数、fork 继承、start-time capability 检查），而非 preset 这一半——若未来评估迁移，对照物是 [docs/subsystems/subagent.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/subsystems/subagent.md)（前置调研 §2.7 已记录）。

实际发现——**本仓库已存在 dsh 设计文档所治的"漂移病"**：同一条"文档阅读纪律"在 4 个 agent 文件中以 4 种措辞出现：

- `.opencode/agents/developer.md`："阅读**所有**要求的文件……禁止未阅读文档变修改代码"+ 检查门控；
- `.opencode/agents/executor.md`："**禁止**进行删减和总结 应当确保 subagent 在执行前按照要求阅读**所有**需要的文件（包括间接引用）"；
- `.opencode/agents/reviewer.md`："**必须**先阅读完整文档……主动阅读那些未被显式声明的间接参考引用"；
- `.opencode/agents/designer.md`："规划即阅读……必须实际阅读列表中每个文档"。

即 [2026-07-05 设计文档](https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/architecture/2026-07-05-prompt-variables-and-tool-guidance-ownership.md) Problem 一节"两份 persona 漂移"的 N=4 版。opencode 格式无 prompt include 机制；**不迁移也可做的对策**：将纪律收敛到单一权威来源（本仓库 Constitution 原则 V 已是），agent 文件只写"遵守原则 V + 链接"，改一处全局生效。

**判定**：N=5、单作者、低频变更——迁移到 preset 属于过度投资（与 §7.3 的 N 计价结论一致）；prompt section 复用是唯一具体的结构性收益，但收益的是"5 个定义间的共享"，可在仓库层面以文档收敛方式获得。

---

## 10. 风险与限制记录（调研发现，非决策）

1. **成熟度**：preset 机制 2026-08-03 才落地（本调研时点前 12 天），developer preview 期、明示破坏性变更；设计文档自述多项踩坑修正（EntryTree.write 会截断 preset 文件、fiber uid 碰撞、host 注入图破坏等），机制仍在快速演进。
2. **web host 内存保留**：idle agent eviction 未实现，每个 touch 过的 session 保留 ~1.3MB（preset 组合后 vs 之前 ~0.2MB）；长驻多会话服务形态需关注（与我们的 gRPC 长驻服务形态相关）。
3. **minimal 的裸 fs + danger-full-access**：仅一次性 checkout 或容器内安全；官方文档明示。
4. **工具 restrict 不移除 guidance**：所有权模型的已知不对称，per-agent 工具裁剪场景需注意。
5. **MCP client 不自动重连**：child 崩溃需 restart/HMR，工具注册残留至 plugin disposal 或成功 re-sync。

---

## 11. 引用来源汇总

仓库外（官方文档/仓库）：

- https://github.com/deepseek-ai/deepseek-harness
  - packages/preset/README.md（preset 组定义：agent-presets、persona）
  - .agents/notes/implemented/architecture/2026-08-03-per-session-agent-presets.md（preset 设计文档）
  - .agents/notes/implemented/architecture/2026-07-05-prompt-variables-and-tool-guidance-ownership.md（所有权原则设计文档）
  - apps/cli/config/agent-presets/{minimal,standard,code,cordis}/agent.cordis.yml（四个官方 preset 源文件）
  - packages/core/system-prompt/README.md（装配 registry API）
  - packages/preset/persona/README.md（persona 行）
  - packages/mcp/README.md、examples/mcp-memory/README.md（MCP client 接入）
  - examples/jsonrpc-agent/README.md、examples/jsonrpc-agent/minimal.cordis.yml（standalone 组合）
  - packages/README.md（包层级与 ctx key 总表）
  - apps/cli/reference/README.md（CLI reference：`DSH_TOOLS_MODE`、minimal preset 的会话级选择表述）
  - packages/bundle/web-app/cordis.patch.yml（TUI process-wide 组合反例、preset roster 挂载行）
  - docs/config-catalog.md（`@deepseek-ai/dsh-agent-presets` Config：roots、`system`/`user` trust）

仓库外（官网与社区实践，追加调研）：

- https://www.deepseek.com/harness/（官网：四种运行模式定位与建会话时选择的表述）
- https://github.com/deepseek-ai/deepseek-harness/discussions/272（生态桥接插件：claude/codex/opencode/pi bridge、pi2dsh）
- https://github.com/deepseek-ai/deepseek-harness/discussions/505（dsh-codex-provider 社区插件发布）
- https://www.runoob.com/deepseek-harness/deepseek-harness-start.html（社区教程：四模式选择）
- https://jishuzhan.net/article/2088234397949378561（社区实践：多 profile 用途隔离——review/writer/sandbox）

仓库内：

- `survey/deepseek-harness-framework.md`（前置调研：框架总体架构）
- `projects/game/agent/package.json`、`projects/game/agent/src/`（现状对照，引自前置调研）
- `.opencode/agents/developer.md`、`.opencode/agents/executor.md`、`.opencode/agents/reviewer.md`、`.opencode/agents/designer.md`、`.opencode/agents/checker.md`（§9.3 实例对照对象）
- `.specify/memory/constitution.md`（原则 V：文档纪律的单一权威来源，§9.3 对策引用）
