# Research: Game Agent v2 — dsh 迁移 Step 1：session 对话页面与模型接入

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-28 | **Status**: 完成——spec 全部开放点已决策（Session 2026-08-27 澄清 + 2026-08-28 plan 决策 Q1–Q5）

研究方法：仓库现状核对（`experimental/dsh/demo/`、`projects/game/`、`experimental/js/vite_react_demo/`、`specs/047|048|050`）+ 上游源码实证（deepseek-harness master @ `b150a55` = `dsh-0.1.1-rc.2`，本地检出 `/tmp/opencode/dsh`）+ npm registry 实测（dsh-client-ui-* 三包 0.1.1-rc.2 tarball 解包）+ OpenAI Responses 官方 OpenAPI 规范核实 + GLM 官方文档核实。每条决策附来源。

**用户架构指令（2026-08-28，硬约束）**：所有对 dsh 的扩展都交付为插件；agent_v2 服务本体只包含"服务 built-in dsh 必要"的最小宿主代码。另：chat 内容接口与 flow 控制分离是用户的既定演进方向（D4 的方向依据）。

---

## D1 — GLM Responses 接入：自研 dsh 插件 `@dominion/dsh-llm-glm`，置于 `common/js/dsh-plugins/llm-glm` ⭐

**Decision**: 新建 workspace 插件包 `@dominion/dsh-llm-glm`（目录 `common/js/dsh-plugins/llm-glm/`，`common/js/**` 已在 pnpm-workspace glob 内，零 workspace 配置改动），按官方 cookbook 的插件形态实现：

```ts
class GlmResponsesAdapter extends LlmAdapter {
  async *stream(options: GenerateOptions): AsyncIterable<StreamChunk> { … }
}
export const name = 'llm-glm'
export const inject = ['llm']
export const Config /* schemastery: baseURL, apiKeyEnv, models[] */
export function apply(ctx: Context, config: Config) {
  ctx.llm.registerAdapter(['glm-responses'], new GlmResponsesAdapter(config))
}
```

位置归属（plan Q2 用户决策）：GLM 适配是与具体服务无关的基础能力，放公共目录而非 agent_v2 子目录；该目录同时为后续 dsh 插件（工具等）建立归属地。

**Rationale**:

1. **用户指令**：dsh 扩展必须是插件；agent_v2 只含宿主代码。
2. **官方插件契约完全开放**：`LlmAdapter` 唯一抽象方法是 `stream(options): AsyncIterable<StreamChunk>`；`providerInfo/resolveModel/listModels/prepareCall` 均可选（[packages/llm/llm/src/index.ts 实测 d.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm/src/index.ts)，本地物化 `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm/lib/types/index.d.ts`）；cookbook 给出最小插件模板与注册语义（[docs/cookbook/adding-an-llm-adapter.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md)）。
3. **官方仅有 chat-completions 适配器**（`dsh-llm-deepseek`，wire 为 `${baseURL}/chat/completions`），无 Responses 适配器（`specs/047-dsh-chat-demo/research.md` D1 回退路径记录）。
4. **GLM 端点为 OpenAI Responses 协议**：Base URL `https://open.bigmodel.cn/api/v1`（[GLM codingplan 接入文档](https://docs.bigmodel.cn/cn/coding-plan/tool/others) §编程端点）。
5. **think 是一等公民**：`StreamChunk` 原生含 `reasoning-delta`、`ContentBlockMap` 含 `ReasoningBlock`（types.d.ts 实测）——GLM 推理输出可无损映射。
6. **依赖形态对齐官方适配器**：deps 仅 `eventsource-parser`（SSE 解析）+ `@deepseek-ai/schemastery`（Config schema）；peers 仅 `@deepseek-ai/dsh-llm` + `@deepseek-ai/cordis`（对照 `dsh-llm-deepseek` package.json：deps 2 项、peers 12 项——我们裁剪到最小集，因不使用 credentials/settings/timeout/attachment 等宿主设施）。

**Alternatives considered**:

- *复用 `dsh-llm-deepseek` + chat-completions 端点*（`https://open.bigmodel.cn/api/coding/paas/v4`）：被 spec FR-007 否决（用户指定 Responses 协议端点）；且 chat-completions 路径的 think 以 `reasoning_content` 字段承载，语义等价但违背用户决策。
- *agent_v2 内联适配器代码（不经插件）*：被用户指令否决（dsh 扩展必须是插件）。

**Spec 影响**: FR-007 落地为插件包；契约见 [contracts/glm-llm-plugin.md](contracts/glm-llm-plugin.md)。

---

## D2 — agent_v2 极简宿主：demo 样板骨架 + 队列/历史/流式转发三个托管模块

**Decision**: `projects/game/agent_v2/` 为原生 ESM TS 服务（`specs/048-js-esm-migration/contracts/esm-package-conventions.md`：`"type":"module"` + nodenext + `.js` 相对导入后缀 + swc es6/preserveImportMeta），结构对齐 `experimental/dsh/demo/agent/`（`AGENTS.md` Bazel/Gazelle 命令）：

- `bootstrap.ts`/`dsh.ts`：OTel init → boot 前 endpoint/secret 注入（见 D9）→ fail-loud 组合启动 → gRPC server → SIGTERM/SIGINT 优雅退出链（stop server → dispose 会话 → dispose root fiber → flush OTel → exit 0）。
- `session.ts`：get-or-create 会话注册表（`Map<资源名, SessionEntry>` + single-flight creations 防抖，demo session.ts 同款模式）+ **每会话 FIFO 队列** + **dispose 立即释放**（FR-015）。
- `history.ts`：**会话生命周期**（非流生命周期）挂接的事件收集器——`assistant/chunk` 驱动流式转发，`assistant/message` 追加终局块到内存历史（FR-014）。
- `server.ts`：grpc-js + proto-loader（TLS 机会加载，demo server.ts 同款），实现 ConversationService 三 RPC（D4）。

**Rationale**: demo 样板已被 047 大型测试实证（`experimental/dsh/demo/agent/src/` 全套 + testplan 通过）；宿主逻辑（会话映射/队列/历史/服务面）不是 dsh 扩展，属宿主本体（用户指令的边界：扩展=插件，托管=宿主）。会话映射沿用"宿主自选 SessionId = game session 资源名"直映射（047 D5：`ctx.agents.create({sessionId: …})`，branded string 宿主自选是上游显式设计）。

**Alternatives considered**: *把队列/历史也做成 dsh 插件*——它们是 agent_v2 的服务面职责（gRPC handler 的状态机），不是对 dsh 能力面的扩展；做成插件会把服务状态藏进组合树、增加复杂度无收益，否决。

---

## D3 — 组合清单 `cordis.yml`：两行（agent-spine 全裁剪 + llm-glm）

**Decision**: agent_v2 的 cordis.yml 仅两行：

1. `agent-spine`（`@deepseek-ai/dsh-agent-spine-demo`）：`persona`（game 对话助手人设）、`workspaceContext: false`（唯一必填键）、`includeRuntimeContext: false`、`includeHarnessIdentity: false`、`skills: {enabled: false}`、`toolBash: false`、`toolJobs: false`（demo `experimental/dsh/demo/agent/cordis.yml` 同款五裁剪键，零工具=FR-006）。
2. `llm-glm`（`@dominion/dsh-llm-glm`）：`apiKeyEnv: GLM_API_KEY`、`baseURL: !!js process.env.GLM_BASE_URL`、`models: [{id: !!js process.env.GLM_MODEL || 'glm-5.2', contextWindow: 1000000}]`（模型 id 可配置默认 glm-5.2=FR-007；contextWindow 对齐 GLM 文档 glm-5.2=1000000，[接入文档](https://docs.bigmodel.cn/cn/coding-plan/tool/others) §配置示例）。

**Rationale**: 047 D4 实证结论直接迁移：spine 为 executor-less/UI-less 骨架，工具面关闭后无需 sandbox/terminal/fs/persistence 行；`sdk-jsonrpc-server` 行必须省略（B1 宿主进程主权）；`models[]` 显式目录避免适配器把自定义 model id 误解析（`resolveModel` 语义）。

**Alternatives considered**: *加 persistence 行*——内存历史本阶段够用（spec Assumptions），否决（dsh persistence 插件留待后续 step）。

---

## D4 — `/api/v2` 对话 API：gRPC server-streaming per send（`POST :send` → NDJSON chunked 流）⭐

**Decision**（plan Q5 用户确认，2026-08-28）：新 proto `projects/game/agent_v2/agent_v2.proto`（`package projects.game.v2`）定义 `ConversationService`：`Send`（server-streaming，`post: /api/v2/{session=templates/*/sessions/*}:send`）、`ListHistory`（`get: …:history`）、`Dispose`（`post: …:dispose`，幂等）。事件模型与 dsh StreamChunk 同构（`queued/turn_start/block_start/delta/block_end/turn_end`）。排队消息的流保持打开（先 `queued{position}`，轮到后 `turn_start`→delta→`turn_end` 才关闭）。

**Rationale**:

1. **用户既定演进方向**：chat 内容接口与 flow 控制分离——对话内容走独立 HTTP 流是正确方向（用户 2026-08-28 确认），future flow 控制不需要挤在 WebSocket 里。
2. **grpc-gateway v2 原生支持 server-streaming**（chunked NDJSON，逐消息 flush；`common/gopkg/grpc/default.go` 的 `GatewayDefault()` 仅 OTel tracing，无流式阻碍）；gateway 侧改动 = 注册 handler + `/api/v2/` 子树两处增量（`projects/game/gateway/cmd/main.go` 现有 `/api/v1/` 分支不动）。
3. **流生命周期 = 回合生命周期**：`turn_end` 即流尾，边界天然；排队语义用"流保持打开"直接表达。
4. **前端消费简单**：fetch + ReadableStream + NDJSON 行解析（非 EventSource——EventSource 仅支持 GET，无法承载 POST body）。
5. **测试面友好**：大型测试用普通 HTTP 客户端断言事件序。

**Alternatives considered**: *WebSocket bidi*（desktop Connect 形态）——gateway 需第二套协议栈、帧协议自定义回合边界、重连/心跳复杂度，而 bidi 收益（跨标签页实时推送）本阶段无需求，否决；*SSE(GET)+unary POST*——需两条 API + 事件订阅状态机，比 per-send 流复杂，否决。

**Spec 影响**: FR-013 的对话 API 形态；契约见 [contracts/conversation-api.md](contracts/conversation-api.md)。

---

## D5 — web↔gateway 同源：同主机名路径分流（`game.liukexin.com`：`/`→web，`/api/*`→gateway）

**Decision**（plan Q1 用户确认，2026-08-28）：game deploy.yaml 中 web 服务声明 `hostnames: [game.liukexin.com]` + `matches: [{backend: http, path: PathPrefix /}]`；gateway 的 http 块新增 `value: /api/v2/` match（`/api/v1/` 原样）。前端全部用**相对路径**调 API（`/api/v1/...`、`/api/v2/...`），零 CORS。

**Rationale**:

1. k8s 层为 Gateway API HTTPRoute 挂共享 `traefik-gateway`（`projects/infra/deploy/k8s.yaml:260` 实证渲染形态）；Gateway API 语义下**同主机名多 HTTPRoute 规则合并、PathPrefix 最长匹配**——`/api/v1/`、`/api/v2/` 精确压过 `/`，静态页面与 API 各达其后端。
2. 满足 FR-013 的本质（web 自身 HTTP 直接 serve 页面；API 统一经 gateway）同时消除 CORS 中间件（对既有 `/api/v1` 加响应头会触碰 FR-010 的"不改既有行为"红线）。
3. 独立主机名方案的唯一收益（页面/API 独立域名）本阶段无需求。

**Alternatives considered**: *独立主机名 + gateway CORS*——严格读 FR-010 有争议且流式 CORS 预检复杂，否决。

**Spec 影响**: FR-013 落地形态；SC-005 不受影响（gateway 二进制仅新增路由注册）。

---

## D6 — session 删除生命周期：web 前端编排 `DELETE /api/v1` → `POST /api/v2 :dispose`（幂等）

**Decision**（plan Q4 用户确认，2026-08-28）：删除流程由 web 前端编排：先调存量 session 服务删除元数据（`/api/v1`，零改动），成功后调 agent_v2 `:dispose`（幂等：会话不存在 = 已释放，返回 Empty）。Dispose 使该会话所有打开中的 Send 流收到 `turn_end{ABORTED}` 关闭、排队消息作废、历史不可查询（FR-015 立即终止释放）。

**Rationale**:

1. gateway 链式调用需改既有 `/api/v1` delete 行为（违 FR-010 唯一例外原则），否决。
2. 编排中断（如 dispose 请求丢失）的最坏后果 = 资源滞留至 agent_v2 进程重启——历史本就是内存态（spec Assumptions），可接受；desktop 删除同名 session 的跨客户端场景同理（已知限制，后续 step 可加惰性校验）。
3. 幂等语义避免"删除编排竞态报错"（web 重试安全）。

**Alternatives considered**: *agent_v2 惰性校验 session 存在性*（GetSession per access）——引入对 session 服务的运行期依赖与延迟，收益仅清理边缘泄漏，否决（记录为后续 step 可选项）。

---

## D7 — 大型测试模型端点：扩展 `projects/game/fake-llm` 新增 `POST /v1/responses`（OpenAI Responses wire）

**Decision**（plan Q3 用户确认，2026-08-28）：既有 fake-llm（Go，chat-completions wire）**增量**新增 Responses 端点：同一模板/匹配/延迟设施上，按 OpenAI Responses SSE 事件词汇发流（`response.created` → `response.output_item.added`(reasoning) → `response.reasoning_summary_text.delta`* → `response.output_item.added`(message) → `response.output_text.delta`* → `response.output_item.done` → `response.completed`(usage)）。既有 chat-completions 端点与全部存量用例零改动。

**Rationale**:

1. 复用既有确定性设施：关键词模板 + 兜底（`projects/game/fake-llm/README.md`）、多轮条件（demo fake-llm 的 `history_keywords`/`min_turn` 模式，047 D7）、**可控延迟**（043/044 stall 测试已依赖——队列大型测试 FR-012 需要"回合进行中"的可控窗口）。
2. FR-007 要求端点可配置替换：agent_v2 经 `GLM_LLM_TARGET`（Dominion resolver target）解析 fake 地址注入 `GLM_BASE_URL`（D9），测试部署零外部网络（SC-001）。
3. Responses SSE 词汇以 OpenAI 官方 OpenAPI 规范为准（streaming events 定义：`response.output_text.delta`/`response.reasoning_summary_text.delta`/`response.completed` 等，[openai-openapi responses streaming](https://github.com/openai/openai-openapi)）。

**Alternatives considered**: *新建独立 fake 服务*——复制模板/延迟设施、测试部署多一服务，无隔离收益（fake-llm 本就是测试专用设施，不在 FR-010 保护清单内），否决；*复用 `experimental/dsh/demo/fake-llm`*——属 dsh-demo app（跨 app 部署耦合），且无延迟设施，否决。

**Spec 影响**: FR-011 大型测试确定性；契约见 [contracts/fake-responses-wire.md](contracts/fake-responses-wire.md)。

---

## D8 — web 前端与 web 服务：ui-primitives 组件级复用 + think/tool 自建 + 050 Go 静态服务

**Decision**:

1. **依赖**：`@deepseek-ai/dsh-client-ui-primitives@0.1.1-rc.2` **精确 pin**（package.json 直接版本——dsh 家族锁定决策的 catalog 例外，对齐 `third_party/dsh/core` 与 047 D6 先例）。实测该包运行时**零 cordis/dsh-invariants import**（0.1.1-rc.2 tarball `lib/index.js` grep 实证；peerDependencies 声明为残留），React 18、11 个 `--dsw-*` CSS token 自建主题即可。
2. **复用组件**：`MessageText`/`MarkdownText`/`CodeBlock`/`JsonBlock`（markdown 渲染）、`DisclosureRow`（think 折叠外壳）、`IconThinkOutline14`/`StateDot`/`Button`/`Input` 等。
3. **自建组件**（参照源码）：`ReasoningRow`——照 `dsh-client-ui-chat` 的 [ReasoningRow.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.tsx)（DisclosureRow + 折叠摘要行 latest/first line + 流式跟随滚动 + `data-state=running`）；`ToolCard`——参照 [ui-tool ToolCallTree.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-tool/src/client/tool/ToolCallTree.tsx) 但剥离 slot 系统，仅名称/参数/状态/结果关联展示（US3）。
4. **web 服务**：`projects/game/web/server/`（Go + `embed.FS` dist + `http.FileServerFS`），逐行照 `experimental/js/vite_react_demo/server/main.go`（050 US4 已实证，样板说明见 [experimental/js/vite_react_demo/README.md](../../experimental/js/vite_react_demo/README.md)）。
5. **前端工程**：`projects/game/web/frontend/`（vite + React + vitest，`vite_build`/`vitest_test` 规则用法照 050 `experimental/js/vite_react_demo/BUILD.bazel`）；workspace 增 `projects/game/web/frontend` 条目；页面结构 = 侧栏 session 列表 + 主区对话（desktop `SessionList/ChatView/ChatMessage` 行为基线：列表/新建/删除/切换、text/think 区分渲染、流式合并、排队指示、tool_call 与 tool_result 按 tool_id 关联合并）。

**Rationale**: spec FR-009 组件级复用决策（2026-08-27 澄清 B 路线）；050 基建已落地（catalog react 栈 + vite_build React 支持 + 静态托管样板，`specs/050-vite-react-bazel/` 当前分支已交付）；BSD-3-Clause 许可允许参照改造（保留必要 attribution，交付时在包 README 注明）。

**Alternatives considered**: *依赖 `dsh-client-ui-conversation`/`dsh-client-ui-tool` 整包*——peer 闭包 25+ dsh client 包（连接层/slot 系统全链），为两个组件引入整个 web 客户端栈，否决（spec 澄清已定"参照自建"）。

---

## D9 — 配置与 secret 面：`glm-api-token` 文件 secret + `GLM_BASE_URL`/`GLM_LLM_TARGET`/`GLM_MODEL` 环境注入

**Decision**:

1. **token**（FR-008）：agent_v2 `service.yaml` artifact 声明 `secrets: [glm-api-token]`；`deploy.yaml` 绑定 `glm-api-token: {secret: llm-secrets, key: glm-codingplan}`（k8s secret key 由运维预置，README 记录）；bootstrap 读取 `$DOMINION_SECRET_DIR/glm-api-token` 文件内容 → `process.env.GLM_API_KEY`（cordis.yml `apiKeyEnv: GLM_API_KEY` 消费，官方 cookbook 推荐的 env 注入模式）；缺失/空 → fail-loud 退出且错误信息不含 token 内容（SC-004；`specs/002-deploy-secret-config/contracts/secret-config.md` §5 运行时契约：文件路径 `/mnt/dominion/secret/glm-api-token`）。
2. **端点**（FR-007 可替换）：bootstrap 顺序——若 `GLM_BASE_URL` 已设则直用；否则若 `GLM_LLM_TARGET` 已设（Dominion resolver target，如 `dominion:///game/fake-llm:8080`）→ resolve → `GLM_BASE_URL = http://{endpoint}/v1`；否则默认 `https://open.bigmodel.cn/api/v1`。cordis.yml `baseURL: !!js process.env.GLM_BASE_URL`（047 D2：resolve 必须前移到 boot 前，`!!js` 惰性同步求值）。测试部署设 `GLM_LLM_TARGET` + `GLM_API_KEY=dummy-key`（fake 容忍 authorization 头，demo test deploy.yaml 同款）。
3. **模型 id**：`GLM_MODEL`（默认 `glm-5.2`）。

**Rationale**: 现有 agent 的 secret 消费先例（`projects/game/agent/src/server.ts:124` 读 `$DOMINION_SECRET_DIR/provider`）；dsh cookbook 明示 "Secrets are cordis-native: schemastery Config with env fallbacks, fed from cordis.yml via `!!js process.env.MY_KEY`. Never read ad-hoc key files in code"——文件读取收敛在 bootstrap 单点后注入 env，插件自身零文件 IO。

**Alternatives considered**: *插件直接读 secret 文件*——违反 cookbook 约定且把部署细节烧进插件，否决。

---

## D10 — 队列、历史与流式转发的实现模式（宿主侧状态机）

**Decision**:

1. **每会话 TurnRunner + FIFO 队列**：会话内串行（回合一个接一个）、会话间并行（demo `session.ts` 的 chain 模式扩展）；`Send` 到达时忙 → 入队并立即向该流发 `queued{position}`，流保持打开；回合结束自动取队首续跑（FR-012 最小行为，不迁移 030/038 的 observe-only 等扩展）。
2. **双通道收集**：`session/event` 订阅挂在会话条目上（会话生命周期）——`assistant/chunk` 的 chunk 载荷（`packages/core/agent-loop/src/agent.ts:350` 实证：`{turn, step, chunk}`，chunk 即原始 StreamChunk）驱动流式转发；`assistant/message`（终局块 + usage）追加内存历史。流断开只停转发、不停收集（FR-014 刷新回填一致性）。
3. **回合终止**：`agent/status → idle`（demo D3 事件收集 + idle 终止模式）；`agent/error`/`turn/end` 错误原因映射 `turn_end{ERROR}`。
4. **历史模型**：`HistoryMessage{role, create_time, blocks[]}`，用户消息在入队时记录、agent 回复在 `assistant/message` 时记录终局块（text/think/tool-call 分类保序，FR-014）。

**Rationale**: demo session.ts 全套模式已被 047 实证；chunk/message 双事件语义上游有测试锚定（`agent.ts:350-401`）；"会话生命周期收集器"是刷新一致性的结构保证。

**Alternatives considered**: *仅靠 `assistant/message` 终局（非流式）*——违反 FR-004 流式要求；*仅流式不记历史*——违反 FR-014；均否决。

---

## 决策汇总

| # | 决策 | 状态 |
|---|---|---|
| D1 | GLM Responses 自研插件 `@dominion/dsh-llm-glm` @ `common/js/dsh-plugins/llm-glm` | ✅ 用户确认（Q2） |
| D2 | agent_v2 极简宿主 = demo 样板 + 队列/历史/流式转发托管模块 | ✅ 设计定（用户指令边界） |
| D3 | cordis.yml 两行：agent-spine 五裁剪 + llm-glm（models 显式目录） | ✅ 设计定 |
| D4 | `/api/v2` 对话 API = gRPC server-streaming per send（NDJSON chunked） | ✅ 用户确认（Q5，chat/flow 分离方向） |
| D5 | 同主机名路径分流（game.liukexin.com：`/`→web、`/api/*`→gateway），零 CORS | ✅ 用户确认（Q1） |
| D6 | 删除编排 = web 前端 DELETE /api/v1 → POST :dispose（幂等） | ✅ 用户确认（Q4） |
| D7 | fake Responses = 扩展 projects/game/fake-llm 新增 /v1/responses 端点 | ✅ 用户确认（Q3） |
| D8 | 前端组件级复用 ui-primitives@0.1.1-rc.2 + think/tool 参照自建 + 050 Go 静态服务 | ✅ 设计定 |
| D9 | secret=glm-api-token 文件→env；GLM_BASE_URL/GLM_LLM_TARGET/GLM_MODEL 注入 | ✅ 设计定 |
| D10 | 队列（每会话 FIFO）+ 历史（会话生命周期双通道收集）+ idle 终止 | ✅ 设计定 |

**已知限制（终态记录，非遗留）**：多标签页无实时推送（未发送消息的页面不看到他页回合，可刷新经 history 查询）；desktop 删除同名 session 不触发 agent_v2 释放（跨客户端编排留待后续 step）；对话历史随 agent_v2 重启丢失（spec Assumptions 明示接受）。
