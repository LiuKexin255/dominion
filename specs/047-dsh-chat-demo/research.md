# Research: dsh Chat Demo

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-22 | **Status**: 完成——spec 全部 NEEDS CLARIFICATION/实证门禁已解决

研究方法：两项上游源码实证（dsh 仓库 master @ `b150a55` = npm `next` = `dsh-0.1.1-rc.2`）+ 仓库样板核对（`experimental/grpc_chain/`、`projects/game/`、`common/js/otel/`）。每条决策附来源。

---

## D1 — FR-007 适配器实证门禁：官方 `dsh-llm-deepseek` 胜出，零自研插件 ⭐

**Decision**: 采用官方适配器路径；fake-llm 仅实现 OpenAI chat-completions wire（**必须支持 SSE 流式**）；不开发任何自研 dsh 插件。

**Rationale（实证证据，满足 spec FR-007 两个必要条件）**:

1. **`baseURL` 纯配置可指**（满足必要条件 a 的前提）：baseURL 解析优先级为 `config.baseURL ?? env($DEEPSEEK_BASE_URL) ?? 'https://api.deepseek.com'`，请求时拼接 `${baseURL}/chat/completions`。cordis.yml 行内支持 `!!js` 表达式（官方示例同款模式）——可注入运行期环境变量（[packages/llm/llm-deepseek/src/index.ts:357-361](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm-deepseek/src/index.ts)、[src/adapter.ts:607](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm-deepseek/src/adapter.ts)、[examples/jsonrpc-agent/minimal.cordis.yml:11-18](https://github.com/deepseek-ai/deepseek-harness/blob/master/examples/jsonrpc-agent/minimal.cordis.yml)）。
2. **免真实 credentials**（必要条件 b）：`inject = ['llm']`（不含 credentials）；无 credentials 插件时直接读 `apiKeyEnv` 指名的环境变量；key 校验仅"非空 + HTTP header 安全"，dummy 值即可（[index.ts:75-76, 411-432](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm-deepseek/src/index.ts)、[packages/llm/llm/src/index.ts:138-158](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm/src/index.ts)）。
3. **无 Files API 硬依赖**：`POST /files` 仅图片请求触发；text-only 不触碰（[adapter.ts:534](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm-deepseek/src/adapter.ts)）。
4. **SSE 强制**：请求恒为 `stream: true, stream_options: {include_usage: true}`、`accept: text/event-stream`，响应须为 OpenAI 风格 SSE（delta → usage → `data: [DONE]`）——这是 fake-llm 的硬性 wire 义务（[serialize.ts:356-370](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm-deepseek/src/serialize.ts)、[adapter.ts:523](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm-deepseek/src/adapter.ts)）。
5. fake-llm 须**容忍**额外 header（`authorization: Bearer <dummy>`、`x-deepseek-harness-*`、attribution User-Agent），无须校验（[adapter.ts:513-530](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm-deepseek/src/adapter.ts)）。

**Alternatives considered**:
- *Responses API + 自研适配插件*（spec FR-007 回退路径）：被证据否决——官方路径两个必要条件全满足，自研无增量价值。回退路径保留为记录：自研适配器需实现 `LlmAdapter`（唯一抽象方法 `stream(options): AsyncIterable<StreamChunk>`，`ctx.llm.registerAdapter(['route'], adapter)` 注册；[packages/llm/llm/src/index.ts:187-259, 365](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm/src/index.ts)）。
- *自研 wire*：否决——README 明示支持 "OpenAI-compatible gateway requests"。

**Spec 影响**: FR-006 fake-llm 接口锁定为 chat-completions（SSE）；FR-007 条件②（自研插件回退）不触发。用户原始输入"response 优先"由澄清 Q1（证据优先程序）承接，证据结论为 chat-completions。

---

## D2 — Dominion 寻址注入（FR-011）：boot 前 resolve → env → `!!js` baseURL

**Decision**: `bootstrap.ts` 中 `boot()` **之前**完成 Dominion 服务发现：`createResolver().resolve("dominion:///dsh-demo/fake-llm:8080")` → 取首 endpoint → `process.env.FAKE_LLM_BASE_URL = "http://<host:port>/v1"`；cordis.yml 的 llm-deepseek 行写 `baseURL: !!js process.env.FAKE_LLM_BASE_URL`。同时设 `apiKeyEnv: FAKE_LLM_API_KEY`（dummy 值，随 deploy env 注入）。

**Rationale**: `!!js` 表达式为惰性同步求值，无法承载 resolver 的异步 fetch——故解析必须前移到 bootstrap、经环境变量传递（官方 examples 同款 env 注入模式）。寻址语义满足 FR-011：endpoint 由运行期服务发现产生，非构建期静态地址。resolver 需要的 env（`SERVICE_APP`/`DOMINION_ENVIRONMENT`）由部署系统注入——与 game agent 在大型测试中的既有机制完全一致（`projects/game/agent/src/resolver-provider.ts:5-15`、`common/js/resolver/src/resolver.ts:38-52`）。

**Alternatives considered**: *`DEEPSEEK_BASE_URL` 环境变量直通*（上游支持）——同样可行，但 `apiKeyEnv`/`baseURL` 配置显式出现在 cordis.yml 中契约更清晰（组合清单自文档化）；*boot `prepare` 钩子内 resolve*——引入时序耦合（prepare 在 Loader 挂载后、配置树求值前，可裁但非必要复杂度）。cordis.yml 路线选定，DEEPSEEK_BASE_URL 记为等价替代。

---

## D3 — 回复观察模式：事件收集 + idle 终止（官方 SDK 模式）

**Decision**: gRPC handler 采用官方 TS SDK client 的在进程内等价模式：订阅 `session/event`（按 sessionId 路由）→ `agent.followup(createUserMessage(...))` → 以 `agent/status` → `idle`（或 `agent.whenIdle()`）为回合终止信号 → 逆序取该会话**最后一条** `assistant/message` 事件的 text blocks 拼接为回复。

**Rationale**: `followup()` 返回 void（无 promise/句柄）——这是上游显式设计（"returns no handle"，[docs/subsystems/core.md:155](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/subsystems/core.md)）；官方 jsonrpc server 与 SDK client 均为"事件流 + idle 终止"模式，wire 不变量"最后一条通知是 session.status idle"有官方测试锚定（[packages/sdk/server/src/server.ts:71-77, 132-143](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/server/src/server.ts)、[packages/sdk/client/src/api.ts:146-194, 236-245](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/client/src/api.ts)、[examples/jsonrpc-agent/tests/sdk.snapshot.ts:427-430](https://github.com/deepseek-ai/deepseek-harness/blob/master/examples/jsonrpc-agent/tests/sdk.snapshot.ts)）。消息构造：`createUserMessage({ content: [{type:'text', text}], source: {kind:'user'} })`（[packages/llm/llm/src/message.ts:192](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm/src/message.ts)）。

**Alternatives considered**: *仅 `await agent.whenIdle()`*——更短，但与 followup 的唤醒存在理论竞态（状态翻转前观测即返回），事件收集模式确定性更强；作为实现简化手段保留，tasks 阶段二选一皆可接受（契约只要求"回合完成的判定可依赖"）。

---

## D4 — 组合清单：两行 + spine 五裁剪键；不启用 jsonrpc-server 与 persistence 行

**Decision**: `cordis.yml` 仅两行：

1. `agent-spine`（`@deepseek-ai/dsh-agent-spine-demo`）：`includeHarnessIdentity: false`、`includeRuntimeContext: false`、`persona: <demo 人设>`、`workspaceContext: false`（**唯一必填键**）、`skills: {enabled: false}`、`toolBash: false`、`toolJobs: false`；
2. `llm-deepseek`（`@deepseek-ai/dsh-llm-deepseek`）：`apiKeyEnv: FAKE_LLM_API_KEY`、`baseURL: !!js process.env.FAKE_LLM_BASE_URL`、`models: [{id: fake-chat-v1, contextWindow: <定值>}]`（显式单条 text-only 目录，替换默认 DeepSeek 目录）。

**Rationale**: spine 为 "executor-less/UI-less agent spine"——工具面关闭后无需 sandbox/terminal/fs/persistence 行（官方两例组合同款裁剪，[packages/examples/agent-spine-demo/src/index.ts:92-129, 85-87](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/examples/agent-spine-demo/src/index.ts)；[examples/jsonrpc-agent/minimal.cordis.yml](https://github.com/deepseek-ai/deepseek-harness/blob/master/examples/jsonrpc-agent/minimal.cordis.yml)）；`sdk-jsonrpc-server` 行必须省略——它是 stdio 服务面，B1 宿主自有进程主权（且会接管 shutdown）；persistence 行仅跨进程 resume 需要，demo 会话内存态（spec Assumptions）。`models[]` 显式目录可避免适配器把 fake model id 当 DeepSeek 模型解析（`resolveModel` 语义）。

**Alternatives considered**: *挂 `sessions`(jsonl persistence) 行*——为 demo 引入文件系统布局依赖，收益仅断电续会话，否决；*启用工具行（bash/editor）*——超出纯 chat 目标（spec FR-005 边界），否决。

---

## D5 — 会话映射与并发：宿主自选 SessionId + get-or-create + 交错一等公民

**Decision**: gRPC 请求的 `session_id` 直接作为 dsh `SessionId`（branded string，宿主自选是上游显式设计）；handler 维护 get-or-create（`ctx.agents.get(id)` 命中复用，未命中 `ctx.agents.create({sessionId, agentOptions: {provider: 'deepseek-official', model: 'fake-chat-v1'}})`）；同名 create 已注册会抛错，get-or-create 是官方 server 同款模式；关闭时逐 agent `handle.dispose()` 后 `ctx.fiber.dispose()`。

**Rationale**: [packages/core/agent/src/index.ts:74-82, 405, 424, 482](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/core/agent/src/index.ts)、[packages/sdk/server/src/server.ts:203-216, 155-181](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/server/src/server.ts)。并发（US2-场景3）：registry 为 `Map<SessionId, AgentEntry>`、每 agent 独立 fiber，交错会话一等公民；官方 server 以 `sessionCreations` Map 去重并发同 id 创建——demo 复刻该防抖。`provider: 'deepseek-official'` 是官方适配器注册的路由名（[packages/llm/llm/src/index.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm/src/index.ts)）。

**Alternatives considered**: *服务端自合成 sessionId（哈希）*——无收益且破坏可观测性，否决。

---

## D6 — `third_party/dsh/core` 底座：≈11 包闭包清单 workspace 包

**Decision**: 照 `common/js/otel` 范式（`common/js/otel/BUILD.bazel`）实现零插件底座：package.json 精确 pin（catalog 例外，survey §4-2）+ `js_runtime_library(name="runtime_pkg", package_name="@dominion/dsh-core", lib=":version_lib", npm_deps=[全部核心 link targets])`。核心包清单（0.1.1-rc.2 线）：

`@deepseek-ai/dsh-app-boot`、`@deepseek-ai/cordis`、`@deepseek-ai/cordis-plugin-loader`、`@deepseek-ai/cordis-plugin-include`、`@deepseek-ai/cordis-plugin-group`、`@deepseek-ai/cordis-plugin-timer`、`node-addon-require-builtin`（native addon 前提）、`@deepseek-ai/dsh-home-paths`、`@deepseek-ai/dsh-invariants`、`@deepseek-ai/dsh-system-prompt`、`@deepseek-ai/dsh-launch-environment`。

**Rationale**: `survey/deepseek-harness-b1-bazel-packaging.md` §5.4.2 框架核心定义 + §5.1 封装范式（`js_runtime_library` + 服务 `runtime_deps` 单点引用，`_collect_runtime_closure` BFS 穿透）；link target 只按 package.json 直接依赖生成（同文 §3.3 实证），故全量枚举是完整性的结构保证。服务侧（`experimental/dsh/demo/agent`）package.json 另行声明 spine/llm-deepseek/app-boot/agent/llm 等直接依赖，其 BUILD `npm_deps` 枚举对应 link targets——插件（含 spine 的 26 peer 闭包、adapter 的 12 peer 闭包）经传递物化进服务闭包。

**Alternatives considered**: *全量 ~100 包 baseline*——被锁定决策否决（survey §5.4.1）；*peer 裁剪清单*——纯优化项，PoC 不做（survey §5.1"枚举全量 vs 传递裁剪"）。

**残余实证项（tasks 阶段验证，非设计阻塞）**: ① 包名/版本以实现时 `dsh-app-boot@0.1.1-rc.2` 的 peers 实测为准（上游 rc 漂移可能微调清单）；② native addon 在 distroless nodejs24-debian12 内 `requireBuiltin` 加载实测（survey §7 风险 2）；③ 同名包版本唯一性校验（survey §7 风险 7）→ 由 SC-004 闭包审计用例承接。

---

## D7 — fake-llm（Go）：chat-completions wire + SSE 恒支持 + 多轮最小扩展匹配

**Decision**: 照搬 `projects/game/fake-llm/` 结构（cmd/service、`go:embed` testdata、关键词模板、确定性兜底），差异面：

1. **wire**：`POST /v1/chat/completions`（OpenAI Chat Completions 形状）+ `GET /health`；`stream:true`（dsh 路径必经）与 `stream:false`（测试便利）都支持；SSE 帧：role delta → `content` delta（可多帧）→ finish chunk（携带 usage）→ `data: [DONE]`。
2. **header 容忍**：忽略 `authorization`/`x-deepseek-harness-*`/attribution UA（D1-5）。
3. **多轮最小扩展**（澄清 Q4）：模板可选附加 `history_keywords`（须出现在**除最后一条 user 消息外**的历史消息中）与 `min_turn`（请求中 user 消息数下限）；匹配优先级：条件模板（多轮条件全满足）> 纯关键词模板 > 兜底模板；未声明多轮条件的模板行为与 game fake-llm 一致。
4. **model 忽略**：请求 `model` 字段不参与匹配（fake 目录由 dsh 侧配置对齐，D4）。

**Rationale**: game fake-llm 模板模式已被 044-046 大型测试验证（`projects/game/fake-llm/README.md`）；SSE/usage/[DONE] 义务来自 D1-4；多轮扩展是 US2 验收的最小充分机制（第二轮消息可与首轮相同，纯靠历史条件分支）。

**Alternatives considered**: *全对话遍历匹配* / *脚本状态机*——澄清 Q4 明示不需要复杂化，否决。

---

## D8 — TS 服务入口 CJS 化（require(esm) 消费 dsh ESM 包），零 data 文件修补

> 2026-08-22 修订：原 "ESM 入口 + package.root.json data 文件" 方案废弃（勘误见 Alternatives 第三条）。

**Decision**: agent 服务以 CJS 编译——`.swcrc` `module.type: "commonjs"`、tsconfig `module: "commonjs"` 且不设 `moduleResolution` 字段（与仓库既有 TS 包统一，样板 `common/js/otel/tsconfig.json`、`experimental/grpc_chain/mid/tsconfig.json`）；bootstrap 传 `bareModuleBaseUrl = pathToFileURL(__filename).href`（入口 `import { pathToFileURL } from "node:url"`）；**服务根无需任何 package.json data 文件**——产物 tar 内无服务根 package.json 时 `.js` 默认按 CJS 解析（`survey/deepseek-harness-b1-bazel-packaging.md` §3.2），恰为 CJS 入口所需。

**Rationale**:

1. **CJS 宿主可直接 require ESM dsh 包**：`require(esm)` 自 Node 22.12 起默认启用（22.7 实验引入）、v25.4 stable（[Node.js docs: Loading ECMAScript modules using require()](https://nodejs.org/api/modules.html#loading-ecmascript-modules-using-require)；演进与同步性原理见 [Joyee Cheung: require(esm) in Node.js](https://joyeecheung.github.io/blog/2024/03/18/require-esm-in-node-js/)——ESM 仅在模块图含 top-level await 时才异步，同步图可被 require）。
2. **0.1.1-rc.2 五个静态依赖包全部同步无 TLA**（app-boot/agent/llm/agent-spine-demo/llm-deepseek，2026-08-22 本地 node 实证 require 成功）——require(esm) 的唯一硬约束不触发；dsh 包自身仍按 ESM 解析（物化 node_modules 内各包 package.json 带 `"type": "module"`）。
3. **rules_ts typecheck 沙箱与源树/产物判定一致**：typecheck 沙箱内无 package.json，ESM 方向需 `module: esnext` + `moduleResolution: bundler` 的 workaround 且引入仓库第二种 tsconfig 配置风格；CJS 方向下沙箱、源树、产物 tar 三者的模块格式判定一致。

**Alternatives considered**:

- *ESM + 源 package.json 作 data_files*（可行备选）：Node ≥22.7 的 syntax detection 下甚至可不放任何文件（`.js` 按语法探测为 ESM），但 Node 官方建议显式 `type` 声明（syntax detection 有解析开销，同 Joyee Cheung 博文）；且与仓库统一 CJS 格式不一致，放弃。
- *`.mjs` 后缀*：swc/ts_project 输出重命名路径不在现有链路习惯内，否决（不变）。
- ~~*package.root.json data 文件*~~（**勘误废弃**）：`artifact_pkg_js` 的 data_files 物化按原名拷贝、无重命名能力（`tools/release/defs.bzl` Phase 5 data_files，L473-481），"`package.root.json` 输出为服务根 `package.json`" 的原设计不成立，废弃。

**附注（供下游免查）**: swc `module.type` 的 ESM 值为 `"es6"`（当前 `experimental/dsh/demo/agent/.swcrc` 实况即此值）、CJS 值为 `"commonjs"`（[swc configuration reference](https://swc.rs/docs/configuration/swcrc)）。

---

## D9 — 网关与对外 API：grpc_chain 样板 + `/experimental/dsh-demo` 前缀

**Decision**: gateway 为 Go grpc-gateway v2 二进制（`experimental/grpc_chain/testplan/gateway/main.go` 样板：`solver.URI("dsh-demo/agent:grpc")` 拨号、`runtime.NewServeMux(pgrpc.GatewayDefault()...)`、`phttp.Handler`、bootstrap 组件注册）；proto `experimental/dsh/demo/chat.proto`（`package experimental.dsh.demo`、`google.api.http` 注解 `post: "/experimental/dsh-demo/chat"` body `*`）；Go 代码经 `go_proto_library` + grpc-gateway/AIP 编译器生成（`experimental/golang/grpc_hello_world/BUILD.bazel` 样板）；ingress `PathPrefix: /experimental/dsh-demo`、hostname `apitest.liukexin.com`（grpc_chain 大型测试同款）。

**Rationale**: 仓库既有 gateway 惯例全量复用（explore 报告 §1）；experimental 命名空间路径前缀与 grpc_chain 一致。app 命名 `dsh-demo`，服务 `gateway`/`agent`/`fake-llm`（resolver target `dominion:///dsh-demo/fake-llm:8080`，spec FR-011 示例一致）。

---

## D10 — 勘误与上游 API 事实（供 tasks 阶段直接消费）

1. **`boot()` 实际签名无 anchor 参数**：`boot(binName, absoluteConfigPath, patches?, prepare?, bareModuleBaseUrl?): Promise<Context>`（[packages/boot/app-boot/src/index.ts:757](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/boot/app-boot/src/index.ts)）——survey 中"第 5 参 anchor"实为 `bareModuleBaseUrl`。官方 runner 样板：[packages/examples/jsonrpc-demo/src/runner.ts:38-49](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/examples/jsonrpc-demo/src/runner.ts)。
2. **配置路径无内建 fallback**：宿主自决（`$DSH_CORDIS_CONFIG` env > argv 惯例可参照）；demo 用产物内静态 `cordis.yml`（`data_files` 携带，survey §2.2 原材料②）。
3. **`ctx.agents` 完整 API 面**（research 实测引用见 D3/D5）：`create/resume/get/list/roots/register/withInitiator/...`；`Agent`: `followup/steer/inject/cancel/whenIdle/status/session/inbox`。
4. **回合事件生命周期**：`followup → agent/status running → turn/start → assistant/chunk* → assistant/message → turn/end → agent/status idle`（[docs/agent-lifecycle.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/agent-lifecycle.md)）——D3 的收集/终止依据。
5. **上游稳定性预期**：developer preview，"THERE WILL BE COMPATIBILITY-BREAKING CHANGES"（[README](https://github.com/deepseek-ai/deepseek-harness#developer-preview)）——demo 锁 0.1.1-rc.2 精确 pin，升级走 lockfile PR（spec Assumptions）。

---

## 决策汇总

| # | 决策 | 状态 |
|---|---|---|
| D1 | 官方 llm-deepseek 胜出，fake-llm = chat-completions + SSE，零自研插件 | ✅ 实证锁定 |
| D2 | boot 前 Dominion resolve → env → `!!js` baseURL 注入 | ✅ 设计定 |
| D3 | 回复 = session/event 收集 + idle 终止 + 末条 assistant/message | ✅ 设计定 |
| D4 | cordis.yml 两行 + spine 五裁剪键；无 jsonrpc-server/persistence 行 | ✅ 设计定 |
| D5 | gRPC session_id ↔ dsh SessionId 直映射 + get-or-create + 并发防抖 | ✅ 设计定 |
| D6 | 底座 ≈11 包闭包清单 workspace 包（otel 范式） | ✅ 设计定（清单以实测 pin 为准） |
| D7 | fake-llm Go：SSE 恒支持 + header 容忍 + 多轮最小扩展匹配 | ✅ 设计定 |
| D8 | TS 入口 CJS（仓库统一）+ require(esm) 消费 dsh ESM 包；无 package.json data 文件 | ✅ 修订定（2026-08-22，原 ESM 方案废弃） |
| D9 | grpc_chain gateway 样板 + `/experimental/dsh-demo` 前缀 + app `dsh-demo` | ✅ 设计定 |
| D10 | 上游 API 勘误与事实清单 | ✅ 记录 |
