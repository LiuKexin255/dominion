# Research: 078 saolei JS hook（引擎选型 + 工具形态 + 实现决策）

**Feature**: [spec.md](spec.md) | **Date**: 2026-10-17 | **输入**: 用户 plan 阶段指令（2026-10-17）①调研热门 coding agent 的 JS 脚本实现方式（引擎 + 工具形态）；②③④spec 修订（无持久化 describe/空输出不播报/零 section/游戏开始结束不触发——已并入 spec Clarifications）

**基线**: `survey/saolei-hook-scripts.md`（2026-10-16 十轮裁定）+ `specs/077-announcer-member-unification/`（K4 + 在途广播快路已交付）。

---

## 1. 热门 coding agent 的 JS 脚本实现方式盘点（用户指令①）

调研时点 2026-10-17。按"谁写的 JS + 在哪执行 + 资源控制 + 工具面形态"四维整理：

### 1.1 各 agent 实现方式

| Agent | 模型写 JS 的场景 | 执行方式 | 资源控制 | 工具面形态 |
|---|---|---|---|---|
| **Claude Code**（Anthropic） | ① analysis tool（claude.ai）：初代浏览器锁定 Web Worker + console.log 拦截，现役服务端沙箱（[simonw 2024 分析](https://simonwillison.net/2024/Oct/24/claude-analysis-tool/)、[Anthropic code execution](https://platform.claude.com/docs/en/agents-and-tools/tool-use/code-execution-tool)）；② **Mods**（2026-10 v2.1.287+）：JS/TS 函数随 plugin 分发，"run inside the Claude Code process **without a sandbox**"（[VILA-Lab 系统分析](https://github.com/VILA-Lab/Dive-into-Claude-Code)，2026-10 复核）；③ **Dynamic workflows**（2026 v2.1.154+）：模型写 JS 编排脚本，后台 runtime 扇出子代理，中间态存脚本变量（同上）；④ hooks：command/http/mcp_tool handler + 每 handler 超时，PostToolUse 不可阻塞、stderr 可反馈（[hooks 文档](https://code.claude.com/docs/en/hooks)、[hooks-guide](https://code.claude.com/docs/en/hooks-guide)） | 第一方代码进程内直跑或服务端沙箱；shell 走 OS 级 sandbox（bubblewrap/seatbelt，[engineering 博文](https://www.anthropic.com/engineering/claude-code-sandboxing)） | hooks 每 handler timeout；shell sandbox 文件系统/网络隔离 | 工具单职责（Write/Edit/Read 分立）；hook 配置非工具（settings 文件） |
| **OpenClaw**（开源个人助理 agent，2026 爆发） | **Code Mode**：模型生成 JS cell，经 `exec`/`wait` 工具编排 | **默认 Node executor = `node:vm` + worker thread**（"Trusted execution on your Gateway host"；"Node's `node:vm` is not a security boundary. The worker keeps guest computation off the Gateway event loop"）；QuickJS executor = "Hardened guest isolation"（QuickJS-WASI in worker）opt-in（[Code Mode executors](https://docs.openclaw.ai/tools/code-mode/executors)） | `timeoutMs`（host terminate 超时 worker，覆盖同步死循环与 promise 续体）、`memoryLimitBytes`（Node = best-effort V8 worker 堆预算，非安全边界）、output limits、pending-call limits、module guards + 小 globals 集 | 同一 `IsolateDriver` 接口可换引擎；工具桥 `external_*` 函数 |
| **OpenCode** | plugins/custom tools：开发者（非模型）写 TS/JS 文件，Bun 运行时进程内直跑，**无沙箱**（[plugins 文档](https://opencode.ai/docs/plugins/)、[custom tools](https://opencode.ai/docs/custom-tools/)）；模型不写 JS（shell 代理执行） | 第一方插件进程内执行；shell 隔离靠外部 plugin（Daytona/microsandbox/`@anthropic-ai/sandbox-runtime`，OS 级） | 依赖宿主权限系统（permission rules） | `tool()` helper：**一工具一函数**（zod schema + description + execute），工具可覆盖内建 |
| **Gemini CLI**（Google） | extensions = MCP servers + context 文件 + 自定义命令（[官方博客 2025-10](https://blog.google/innovation-and-ai/technology/developers-tools/gemini-cli-extensions/)）；模型不写进程内 JS | shell 沙箱 = docker/podman/seatbelt/runsc/lxc OS 级（[sandboxing 文档](https://geminicli.com/docs/cli/sandbox/)） | OS 级文件系统/网络隔离 + sandbox expansion 审批 | MCP 工具（schema 由 MCP server 定义） |
| **Codex CLI**（OpenAI） | 模型代码经容器执行；2026 版收紧信任与网络策略（[VILA-Lab 对比分析](https://github.com/VILA-Lab/Dive-into-Claude-Code)） | 容器级 | 容器隔离 + 审批 | 单职责工具 |

### 1.2 引擎/传输层方案（供 agent 集成的库）

| 方案 | 形态 | 关键事实 |
|---|---|---|
| **TanStack Code Mode isolates**（[文档](https://tanstack.com/ai/latest/docs/code-mode/code-mode-isolates)） | 驱动抽象：isolated-vm（V8 isolate）/ QuickJS-WASM / QuickJS-Bun / Cloudflare Workers / Daytona / E2B，统一 `IsolateDriver` 接口 | 统一配置面：memoryLimit、timeout、maxStackSize、maxToolCalls、console 捕获；QuickJS-WASM 零原生依赖但解释执行慢 10-50x（对工具编排型脚本无感） |
| **Mastra Code Mode transports**（[QuickJsCodeModeTransport](https://mastra.ai/reference/tools/quickjs-transport)） | Stdio / IsolatedVm / QuickJS 三传输 | QuickJS：进程内 WASM、无 fs/network/process 访问、每次 run 新 runtime、JSON 字符串边界（宿主对象引用不泄漏）、timeout 覆盖同步死循环、run 后 dispose |
| **@sebastianwessel/quickjs**（[AI 生成代码用例](https://sebastianwessel.github.io/quickjs/use-cases/ai-generated-code.html)） | QuickJS-WASM 封装 | fetch/fs 可选注入；"检查危险操作 + 最大执行时间 + 受限 API"清单 |
| **vm2** | node:vm 包装 | **已废弃**（多次沙箱逃逸 CVE，[simonw 2026 研究](https://simonwillison.net/2026/Mar/22/javascript-sandboxing-research/)）——任何方案不再考虑 |
| **simonw 沙箱对比研究**（2026-03，[repo](https://github.com/simonw/research/tree/main/javascript-sandboxing-research)） | isolated-vm / vm2 / quickjs-emscripten / QuickJS-NG / ShadowRealm / Deno Workers 全谱系 | 结论谱系：`node:vm` 单用适合半信任代码；对抗性代码才需要 isolated-vm/QuickJS/容器；isolated-vm 原生编译是 npm 安装失败头号来源 |
| **ado-code-mode-mcp ADR-002**（[ADR](https://github.com/thijs-hakkenberg/ado-code-mode-mcp/blob/main/docs/adr/002-node-vm-sandbox.md)） | `node:vm` + 防御纵深 | 与本 feature 同威胁模型的先例：LLM 写代码、宿主执行；选 node:vm（零依赖/亚毫秒启动/原生 async）；防御纵深 = banned-pattern 校验 + 冻结 globals + `codeGeneration:{strings:false}` + timeout + 最小 globals（`ado`/`console`/`JSON`/`Math`/`Date`/`sleep`）；"threat model 是防止 LLM 意外副作用，非对抗逃逸" |

### 1.3 谱系综合（对 D1 的含义）

1. **威胁模型分层是共识**：第一方（模型/开发者）代码 → 进程内轻隔离（node:vm / 无沙箱直跑）+ 资源有界；不可信第三方代码 → WASM 引擎（QuickJS）/ OS 级 / 容器。OpenClaw 把这条线做成了显式产品语义（"Trusted host execution" vs "Hardened guest isolation"）。
2. **"事件触发 + 超时保护 + 失败不阻塞主流程 + 结果反馈"四要素**（survey §2.3.1 Claude Code hooks 共识）在各实现中均成立：OpenClaw 用 worker terminate 兜底同步死循环，ado 用 vm timeout，Mastra/QuickJS 用 interrupt handler。
3. **无任何热门 agent 提供"模型经工具注册常驻 JS hook"的先例**——本 feature 形态是新的，但其两个组成件（模型写 JS 的执行、工具管理面）都有成熟参照。
4. **工具面形态**：主流 agent 偏好**一工具一职责**（OpenCode `tool()` helper、Claude Code Write/Edit/Read 分立）；多动作 enum 工具在这些 agent 中罕见，但本仓库 memory 工具（`common/js/memory/src/tool.ts`）即单工具 + `action` enum（add/replace/remove）+ execute 内 XOR 校验 + 文本拒绝的成熟先例，且其根因记录（"a schema-level oneOf would turn the model's recoverable mistake into an INVALID_ARGS failure"）同样适用于 hook。

---

## 2. 决策记录

### D1 JS 执行引擎 = `node:vm`（同步函数体 + 注册期编译校验 + 执行期 timeout + 防御纵深）

**Decision**: `node:vm`（`vm.Script` 预编译**调用形态** `(function(board) { … })(board)` + `vm.createContext` + `runInContext({ timeout })` + `codeGeneration: { strings: false, wasm: false }` + 无宿主 API 注入（上下文仅数据参数 `board` 快照）+ 注册期 banned-pattern 校验）。内联调用形态使 `timeout` 覆盖 hook 函数体（若宿主在 vm 之外调用"返回的函数"，死循环不会被中断，故不采用该形态）。同步执行于事件回调内，逐 hook 超时预算（D4）。不引入 worker_threads、不引入新依赖。

**Rationale**:

1. **威胁模型匹配**（spec Assumptions：第一方 planner LLM，防失控资源非对抗逃逸）：§1.3 谱系中该情境的三个独立先例全部收敛于 node:vm——OpenClaw 默认 Node executor（node:vm，"trusted host execution"）、ado ADR-002（同威胁模型逐条论证）、Claude Code Mods（第一方 JS 甚至无沙箱直跑——本方案严格更保守）。survey §2.3.2 建议与社区结论一致（[simonw 2026](https://simonwillison.net/2026/Mar/22/javascript-sandboxing-research/)：node:vm 适合半信任代码）。
2. **脚本形态匹配**：hook 为同步纯计算（无 async、无工具桥接、无 I/O）——QuickJS 的主要短板（asyncify 桥接复杂/性能税，ado ADR 与 TanStack 文档均记录）与主要长处（对抗隔离）都不适用于本场景；node:vm 的"timeout 可中断同步 JS 执行"（[Node vm 文档](https://nodejs.org/api/vm.html)）恰好覆盖死循环防护。
3. **仓库约束匹配**：零新依赖（bazel 闭包无 wasm 资产/原生编译摩擦——isolated-vm 的 node-gyp 与 quickjs 的 ~2MB wasm 是 §1.2 记录的主要落地成本）；ESM/bazel 现状零改动。
4. **内存无硬上限的处置**：node:vm 无内存硬限（OpenClaw 文档明确"not a security boundary... best-effort"）。缓解：① 输入有界（`GameState` JSON，棋盘 ≤ 数百格）；② 逐 hook timeout 界定 CPU 时间；③ 输出截断（D4）界定结果尺寸；④ 注册期脚本长度上限（D4）界定代码尺寸。残余风险（蓄意构造大数组常量）在第一方威胁模型下接受（ado ADR 同口径）。
5. **已知限制披露**：vm timeout 的中断检查发生在 JS 回边/函数入口，**灾变性正则回溯可能不被中断**（V8 regexp 引擎内执行）。缓解：banned-pattern 不覆盖（误伤正常脚本）；预算 + 第一方作者假设兜底；若实践中出现，演进位为 worker_threads 变体（terminate 硬杀）。

**Alternatives considered**:

- **quickjs-emscripten（WASM）**：隔离最强、内存/栈硬限；但 wasm 资产进 bazel 闭包的打包验证成本 + 解释执行开销 + 无 async 需求下的收益为零——记录为**演进位**（hook 面向外部用户开放时升级，survey §2.3.2 同结论）。
- **worker_threads + node:vm**（OpenClaw Node executor 形态）：事件循环零阻塞 + terminate 硬杀（可中断正则死循环）；但生命周期/消息协议复杂度对 ≤500ms 预算的微脚本属过度设计，且 hook 执行本就发生在 operate 工具调用的 await 路径内（短暂阻塞可接受）——记录为 timeout 中断失效时的演进位。
- **`dsh-ptc-runtime-node`**（家族 seam）：契约完备但每次 run 进程生成（数十 ms）× 每次 operate × N hooks 过重；bindings 面向工具调用（survey §2.3.2 已评估）。
- **vm2**：已废弃（CVE），排除。

### D2 工具形态 = 单 action 工具 `saolei_hook` 维持（survey D5 用户裁定确认）

**Decision**: 维持 survey D5 裁定——单工具 `saolei_hook`，`action: set|remove|list` enum；schema 根字段全部 optional，动作互斥在 execute 内校验并以普通文本结果拒绝（memory 工具同型，`common/js/memory/src/tool.ts` 根因记录适用）。

**Rationale**: ① 本仓库 memory 先例即此形态且经多个 feature 验证；② 用户指令③"全部说明经 describe 承载"在单工具下内聚（一份 describe 覆盖三动作与环境说明，不会出现工具面说明分裂）；③ fake-llm keyword 模板驱动的确定性测试对单工具名更简单。调研（§1.3 第 4 点）确认主流 agent 偏好一工具一职责——该形态作为被否决备选记录：`saolei_hook_set/remove/list` 三工具语义更显式，但与本仓库先例和 describe 内聚要求冲突，且三份 schema/describe 维护成本高于一个 action enum。

### D3 同名 set = 覆盖脚本内容，注册序位置保持

**Decision**: set 已存在名称 → 覆盖脚本源码（重新编译校验），注册表中该项**保持原注册序位置**（更新语义，非移动到队尾）。

**Rationale**: "set = 注册/更新"是 planner 直觉语义（spec US2 故事文本即"注册/更新脚本"）；位置保持使播报顺序在脚本演化下稳定（player 看到的 hook 输出顺序不因更新而重排），降低噪音。**Alternatives**: 移动到队尾（变更可观察顺序，无收益）；拒绝重名强迫先 remove（多一次调用纯摩擦）。

### D4 资源上限常量

**Decision**（代码内具名常量，`common/js/dsh-plugins/saolei-hooks/src/limits.ts`，可后续调整不需契约变更）:

| 常量 | 值 | 依据 |
|---|---|---|
| `MAX_HOOKS` | 8 | 每次 operate × N hooks 条播报（上下文增长代价，spec Assumptions）；8 足够覆盖"确定性安全格 + 局面统计 + 启发式告警"类策略组合 |
| `MAX_SCRIPT_CHARS` | 16384（16KB） | 函数体式策略脚本的实际尺寸上限远低于此（survey §2.3.3 形态）；足够容纳百行级分析代码 |
| `MAX_OUTPUT_CHARS` | 4096（4KB） | 播报是广播消息（进全部成员上下文），4KB ≈ 1K token，与 saolei 工具结果文本同量级 |
| `EXECUTION_TIMEOUT_MS` | 500 | 30×30 棋盘的确定性扫描在 V8 下微秒~毫秒级；500ms 是 3 个数量级的安全余量，且单 operate 批量内 N hooks 串行最坏 4s，仍远小于桌面派发往返时延 |

超限处置：数量/脚本长度超限 → set 时文本拒绝（FR-003）；输出超限 → 截断播报并附截断标识；超时 → 中断 + 超时播报（FR-013）。总预算上界 = `MAX_HOOKS × EXECUTION_TIMEOUT_MS` = 4s（同步路径的已接受上界——spec FR-011"不挂死 operate 调用"的验收口径）。

### D5 事件 seam API = `GameRuntimeDeps` 可选回调

**Decision**: 游戏基础事件以 `GameRuntimeDeps` 可选字段提供（如 `onBoardChange?: (entry: GameLogEntry) => void`），`createGameRuntime` 构造参数随之扩展（可选第三参）；`operate` 内在触发条件满足处同步调用。不用 EventEmitter/订阅面。

**Rationale**: ① `GameRuntime` 是纯实现类非 cordis Service，全部协作者经 deps 注入是既有 DI 风格（`GameRuntimeDeps` 定义于 `common/js/dsh-plugins/saolei/src/game/runtime.ts`）；② 订阅方恰一个（hooks 引擎，编排接入），多订阅分发的 EventEmitter 抽象无需求支撑（宪法 II：避免过度设计）；③ 可选字段 = seam 缺省无副作用（spec US4 场景 4）。**Alternatives**: 公开订阅面（过度设计）；工具层包装（已否决，survey K1）。

### D6 播报文本格式

**Decision**（前缀约定，announcer 播报正文）：正文以 `[hook <name>] ` 前缀归因（多 hook 逐条播报、注册序）；空字符串输出不播报（零条目）。各情形（正常输出/输出截断/运行期错误/超时/输出不可序列化）的终态文本表以 [contracts/hook-announcement.md](contracts/hook-announcement.md) §2 为唯一权威来源——本决策只定前缀与"空字符串不播报"两条语义，不在多处复述格式表。超时文本按 `EXECUTION_TIMEOUT_MS` 常量渲染。

**Rationale**: 前缀使 web/成员侧可按名称归因（多 hook 播报流可读性）；错误/超时格式与 saolei 工具的 `rejected:`/错误文本风格一致；截断标识使截断可检测（测试断言面）。

### D7 沙箱 globals 与防御纵深

**Decision**:

1. **无宿主 API 注入**：上下文自带 ECMAScript 内建（Object/Array/JSON/Math/RegExp 等可用），**无任何宿主 API**（无 require/process/console/fetch/timer——fresh context 不含 Node globals，无需逐一剔除）；唯一注入绑定是数据参数 `board`（`entry.state` 的 JSON 快照，以 `createContext({ board })` 供内联调用形态 `(function(board) { … })(board)` 取用），除该数据绑定外不注入任何全局。
2. `codeGeneration: { strings: false, wasm: false }`：禁 eval/Function 动态代码生成。
3. 注册期 banned-pattern 校验（best-effort，ado ADR 同型）：源码含 `require(`、`import `、`process.`、`globalThis.`、`global.`、`fetch(`、`child_process`、`eval(`、`Function(` → set 文本拒绝（终态清单以 [data-model.md](data-model.md) §2.4 为准）。
4. 每次执行 new context（无跨执行共享状态，spec US3 场景 5）。

**Rationale**: fresh vm context 的内建集即最小可用计算面（纯棋盘分析所需全部在内建集内）；参数注入优于全局注入（无全局名污染、脚本形态即纯函数体）。banned-pattern 防的是"作者误以为有宿主 API"的第一类意外（写 `console.log` 调试——注意：不 ban `console`，context 无 console 则运行期 ReferenceError → 播报反馈，同样达成引导；ban 的是危险面）。

### D8 hooks 能力面形态与编排装配

**Decision**: 插件 `@dominion/dsh-saolei-hooks`（`common/js/dsh-plugins/saolei-hooks/`，`name = "saolei-hooks"`，`inject: []` 纯 provider）在 plugin load 时 `ctx.provide("saoleiHooks", mount)`；provide 面是 **team 级工厂**（[data-model.md](data-model.md) §3.1 `SaoleiHooksMount`）：`mount.buildHooksUnit({ announce }) → SaoleiHooksUnit`——`announce: (text: string) => void` 是编排传入的播报 seam（绑定 orchestrator 持有的 announcer），返回的 unit 携带 `tools`（planner 侧工具面，内含共享注册表 + 沙箱执行器的写读入口）、`sections`（恒为空数组——零 prompt section）与 `onBoardChange`（事件处理器，读注册表执行播报）。

**Rationale**: ① registry 必须双角色共享（planner 写、player 侧事件读）——build 一次产出闭包持有同一 registry 的两个面；② announce 经参数注入使插件保持纯 provider（无 cordis 服务依赖、零 saolei-loop 耦合——spec FR-016）；③ saolei-loop `apply` 对 hooks 行做**条件 fail-loud**：`config.plugins` 中存在 `@dominion/dsh-saolei-hooks` 行而装载后 `ctx.get("saoleiHooks")` 缺失 → 组合装载失败；行缺席 → hooks 为可选能力缺席，orchestrator 读取面得 undefined、零 hook 装配（spec FR-015/US5 场景 5）。

**Alternatives**: 两个独立 provide（registry 与 handler 分面）——装配方需要自行保证同一 registry 实例，增加出错面；unit 内直接注入 announcer 引用——需要插件依赖 loop 的类型，破坏零耦合。

### D9 describe 文案设计原则

**Decision**: describe 采用本仓库 memory 工具的文案纪律（"a generic, scenario-free statement of the tool's own function"，`specs/068-wiki-round-pause/spec.md` FR-012 先例）+ 用户指令②③的内容要求：三动作用法、board 字段形状、函数体/return 契约、**空输出不播报（低噪音引导）**、**不持久化（team 刷新清空，跨局保留自行保存——不提 memory、不提任何特定存储工具）**、同步纯函数、无宿主 API、超时/输出上限、触发语义（有棋盘变动的批量操作后执行播报；**游戏开始与结束不触发**）、注册序执行。全部经 describe 承载，无 prompt section（spec FR-006/SC-008）。

**Rationale**: 用户指令（2026-10-17）②③；单工具形态下 describe 是唯一说明面（D2）。

---

## 3. 对 spec 的回写

用户指令②③④已直接修订 spec（Clarifications Session 2026-10-17 追加四条；FR-001/003/006/007/008、US1/US2/US4/US5 场景、Edge Cases、SC-001/SC-008、Assumptions 同步更新）。D1–D9 为 plan 阶段裁定，落地于 [data-model.md](data-model.md)、[contracts/](contracts/)；其中 D4 上限值/D6 前缀在 spec Assumptions"plan 阶段裁定项"清单内，本 research 即其裁定记录。

## 4. 参考（仓库外，除 §1 已列）

- Node.js vm API（timeout 中断语义、非安全边界声明）: https://nodejs.org/api/vm.html
- OpenClaw Code Mode executors（node:vm+worker 默认 / QuickJS 加固、timeoutMs=worker terminate、memoryLimitBytes 语义）: https://docs.openclaw.ai/tools/code-mode/executors
- TanStack Code Mode isolates（驱动矩阵、memoryLimit/timeout/maxToolCalls 统一面）: https://tanstack.com/ai/latest/docs/code-mode/code-mode-isolates
- Mastra QuickJsCodeModeTransport（进程内 QuickJS、JSON 边界、timeout 覆盖同步死循环）: https://mastra.ai/reference/tools/quickjs-transport
- simonw JavaScript sandboxing research 2026（vm2 废弃、node:vm 半信任适用、isolated-vm/quickjs 定位）: https://simonwillison.net/2026/Mar/22/javascript-sandboxing-research/ 、https://github.com/simonw/research/tree/main/javascript-sandboxing-research
- ado-code-mode-mcp ADR-002（同威胁模型 node:vm 选型与防御纵深）: https://github.com/thijs-hakkenberg/ado-code-mode-mcp/blob/main/docs/adr/002-node-vm-sandbox.md
- VILA-Lab/Dive-into-Claude-Code（Mods 无沙箱进程内 JS / Dynamic workflows / Codex 对比，2026-10 复核）: https://github.com/VILA-Lab/Dive-into-Claude-Code
- Claude Code hooks（timeout/不可阻塞/stderr 反馈四要素）: https://code.claude.com/docs/en/hooks 、https://code.claude.com/docs/en/hooks-guide
- Anthropic sandboxing engineering（OS 级 shell 隔离——对照项）: https://www.anthropic.com/engineering/claude-code-sandboxing
- OpenCode plugins/custom tools（一工具一职责、Bun 进程内无沙箱）: https://opencode.ai/docs/plugins/ 、https://opencode.ai/docs/custom-tools/
- Gemini CLI extensions/sandboxing（MCP 形态、OS 级沙箱）: https://blog.google/innovation-and-ai/technology/developers-tools/gemini-cli-extensions/ 、https://geminicli.com/docs/cli/sandbox/
- @sebastianwessel/quickjs AI 生成代码用例: https://sebastianwessel.github.io/quickjs/use-cases/ai-generated-code.html
