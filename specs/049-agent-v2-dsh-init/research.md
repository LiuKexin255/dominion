# Research: Game Agent v2 — dsh 迁移 Step 1：session 对话页面与模型接入

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-28（2026-08-29 修订，含第二批架构指令与第三批裁定：服务发现名约束返工 + proxy 容忍） | **Status**: 完成——spec 全部开放点已决策（Session 2026-08-27 澄清 + 2026-08-28 plan 决策 Q1–Q5 + 2026-08-29 用户架构指令（含第二批：测试零 secret 与空 key 容忍；第三批：方向 1 改名 + proxy 容忍，D13））

研究方法：仓库现状核对（`experimental/dsh/demo/`、`projects/game/`、`experimental/js/vite_react_demo/`、`specs/047|048|050`）+ 上游源码实证（deepseek-harness master @ `b150a55` = `dsh-0.1.1-rc.2`，本地检出 `/tmp/opencode/dsh`）+ npm registry 实测（dsh-client-ui-* 三包 0.1.1-rc.2 tarball 解包）+ OpenAI Responses 官方 OpenAPI 规范核实 + GLM 官方文档核实。每条决策附来源。

**用户架构指令（2026-08-28，硬约束）**：所有对 dsh 的扩展都交付为插件；agent_v2 服务本体只包含"服务 built-in dsh 必要"的最小宿主代码。另：chat 内容接口与 flow 控制分离是用户的既定演进方向（D4 的方向依据）。

**用户架构指令（2026-08-29，硬约束，修订 D4/D11/D12）**：(1) agent_v2 为有状态服务，必须经 proxy 路由，不能被 gateway 直接接入；(2) 为 `projects/game/pkg/bind` 扩展 server-streaming 支持；(3) agent_v2 的 proto 与 game 既有 proto 放同一目录，不单独分目录。

**用户架构指令（2026-08-29，硬约束，修订 D4 owner 存储）**：proxy 拆分 mongo collection 时，database/collection 命名知识归属 store 层（`projects/game/proxy/runtime/mongo`）持有；`projects/game/proxy/cmd/main.go` 只做装配，不出现 collection 字符串。

**用户架构指令（2026-08-29 第二批，硬约束，修订 D9）**：(1) 测试不依赖真实 secret——允许 secret 文件不存在（即 LLM 请求的 secret 为空），测试部署不在 deploy 中设置 secret；(2) 增加一个不包括 secret 配置的测试 artifact；不注入 secret——读取不到 key 就在 LLM 请求时跳过设置 key 的流程。

**用户裁定（2026-08-29 第三批，方向 1，修订 D9/D13）**：实测暴露两个缺陷，裁定修复方向——(1) 服务发现名违反 deploy API 约束（`^[a-z][a-z0-9-]{0,19}$` 禁下划线，`agent_v2` endpoints 查询 400）：服务名改 `agent-v2`，目录/proto/包名不动；(2) proxy 对"管理的有状态服务不存在"为致命行为（agent-v2 测试环境不部署 v1 agent → v1 manager 重试耗尽 → proxy crash loop）：修复为容忍语义（见 D13）。

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

## D4 — `/api/v2` 对话 API：gRPC server-streaming per send + proxy 有状态路由（gateway→proxy→agent-v2 两跳）⭐

**Decision**（plan Q5 用户确认 2026-08-28；2026-08-29 用户架构指令修订路由拓扑）：`ConversationService` proto 定义于 app 根目录 `projects/game/agent_v2.proto`（package `projects.game.v2`，归属决策见 D12）：`Send`（server-streaming，`post: /api/v2/{session=templates/*/sessions/*}:send`）、`ListHistory`（`get: …:history`）、`Dispose`（`post: …:dispose`，幂等）。事件模型与 dsh StreamChunk 同构（`queued/turn_start/block_start/delta/block_end/turn_end`）；排队消息的流保持打开（先 `queued{position}`，轮到后 `turn_start`→delta→`turn_end` 才关闭）。

**路由拓扑（2026-08-29 用户指令）**：HTTP 面保留在 gateway（grpc-gateway 注册 `ConversationServiceHandler`，挂既有 `teamConn`——即 proxy conn，与 TeamService 同一连接）；proxy（`kind: stateless`、仅 gRPC）增量注册 `ConversationServiceServer`，按 owner 亲和将 `(template, session)` 定向到 agent-v2 的有状态实例（`kind: stateful`，对齐 v1 agent）；proxy 无 HTTP 面，gateway 仍是唯一 HTTP 出口——这是分工而非违背用户指令。

**owner 亲和设计（复用 v1 路由设施）**：

1. **owner 存储**：复用 proxy 既有 `domain.OwnerStore` 接口与 Mongo 实现（`projects/game/proxy/domain/interfaces.go`、`projects/game/proxy/runtime/mongo/owner_store.go`），构造函数语义化——`NewAgentOwnerStore(client)`（v1 team owner，`game_proxy.agent_owners`）与 `NewAgentV2OwnerStore(client)`（`game_proxy.agent_v2_owners`，与 v1 不同 collection：两个实例池独立，同一 game session 可同时存在 v1 team owner 与 v2 conversation owner，互不干扰）；database/collection 常量私有化于 store 包内，命名知识单一事实源在 store 层——`cmd/main.go` 只做装配、不出现 collection 字符串（2026-08-29 用户指令）。v1 调用点仅构造名变化，存储位置与行为零改动。
2. **分配时机**：首次 `Send` get-or-create（并发竞态 `ErrOwnerAlreadyExists` → 重读胜者，复用 `assignOwner` 模式 `projects/game/proxy/handler/handler.go`）；pick 复用 FNV32a(sessionID) % 实例数 hash 亲和（`projects/game/proxy/runtime/picker/hash_picker.go`）。读路径不分配：`ListHistory` 无 owner → 空响应短路；`Dispose` 无 owner → 幂等 Empty 短路（保持"新会话回填空/已释放即成功"契约）。
3. **实例连接**：proxy 为 agent-v2 装配第二个 `agentclient.Manager`（`dominion-stateful` 按实例序号解析，`specs/006-grpc-js-service-discovery` FR-009/FR-012；30s 刷新 daemon 同 v1）；conn factory 参数化 target（v1 行为零改动）。
4. **owner 生命周期**：dispose 不删除 owner 记录——映射是亲和锚点而非会话状态（与 v1 owner 生命周期一致）；同资源名再 Send 定向同实例、get-or-create 全新会话（FR-015 无残留状态由 agent-v2 侧保证）。

**Rationale**:

1. **用户既定演进方向**：chat 内容接口与 flow 控制分离——对话内容走独立 HTTP 流（用户 2026-08-28 确认），future flow 控制不需要挤在 WebSocket 里。
2. **有状态服务必须经 proxy 亲和路由是本仓库既有惯例**：v1 agent 是 game app 唯一 `kind: stateful` 服务，从不被 gateway 直连——gateway 的 TeamService 一律连 proxy（`gameconst.TeamTarget = "game/proxy:grpc"`，`projects/game/gateway/cmd/main.go` teamConn）；owner 解析与亲和分配全部收在 proxy（`projects/game/proxy/cmd/main.go`）。agent_v2 的会话/队列/历史驻留进程内存（D2/D10），是比 v1 agent 更强的有状态服务，同样不能被 gateway 直连。
3. **直连破坏 session ownership 与路由**：desktop 直连 agent 曾因"绕过 session ownership 与 proxy routing"被否决（`specs/007-dialog-agent/research.md` §Preserve existing session/proxy/gateway/prompt architecture 的 alternatives）；gateway 直连 agent-v2 同理——无 owner 亲和则同一 session 的请求可能落到不同实例，内存会话/历史即被撕裂。
4. **grpc-gateway v2 原生支持 server-streaming**（chunked NDJSON，逐消息 flush；`common/gopkg/grpc/default.go` 的 `GatewayDefault()` 仅 OTel tracing，无流式阻碍）；gateway 侧改动 = 在既有 teamConn 上注册 handler + `/api/v2/` 子树两处增量（既有 `/api/v1/` 分支不动）。
5. **keepalive 链路现成**：gateway→proxy 已配 `WithLongLivedClientKeepalive`（TeamService.Connect 长流先例），proxy server 侧已配 `WithLongLivedServerKeepalive`，proxy→agent-v2 连接沿用 agentclient 的长流 keepalive——Send 排队流的长 Idle 窗口全链路被既有配置覆盖。
6. **流生命周期 = 回合生命周期**：`turn_end` 即流尾，边界天然；排队语义用"流保持打开"直接表达。
7. **前端消费简单**：fetch + ReadableStream + NDJSON 行解析（非 EventSource——EventSource 仅支持 GET，无法承载 POST body）。
8. **测试面友好**：大型测试用普通 HTTP 客户端断言事件序。

**Alternatives considered**: *gateway 直连 agent-v2（gwmux 挂 agentV2Conn）*——被 2026-08-29 用户架构指令否决：有状态服务绕过 proxy 亲和路由（同 `specs/007-dialog-agent/research.md` 否决 desktop 直连的理由）；*WebSocket bidi*（desktop Connect 形态）——gateway 需第二套协议栈、帧协议自定义回合边界、重连/心跳复杂度，而 bidi 收益（跨标签页实时推送）本阶段无需求，否决；*SSE(GET)+unary POST*——需两条 API + 事件订阅状态机，比 per-send 流复杂，否决；*owner store 构造参数化 `NewMongoOwnerStore(client, db, coll)` + 导出默认常量由 main.go 传参*——collection 命名知识泄漏到装配层（v2 collection 以字面量落在 main.go，无编译期保护，与 store 常量构成双事实源），被 2026-08-29 用户指令否决（col 命名归属 store 层，main.go 只做装配）；*类型化选择参数（如 `NewMongoOwnerStore(client, OwnerCollection)` 枚举）*——枚举值本身成为导出 API 面（命名知识以第二形态暴露于包外），且 v1 调用形状同样变化，相比语义化构造函数无额外收益，否决。

**Spec 影响**: FR-013 的对话 API 形态与路由拓扑、FR-010 的 proxy/bind 增量例外；契约见 [contracts/conversation-api.md](contracts/conversation-api.md)；路由实体见 [data-model.md](data-model.md) §2.9。

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

**Decision**（plan Q4 用户确认，2026-08-28）：删除流程由 web 前端编排：先调存量 session 服务删除元数据（`/api/v1`，零改动），成功后调 agent-v2 `:dispose`（幂等：会话不存在 = 已释放，返回 Empty）。Dispose 使该会话所有打开中的 Send 流收到 `turn_end{ABORTED}` 关闭、排队消息作废、历史不可查询（FR-015 立即终止释放）。

**Rationale**:

1. gateway 链式调用需改既有 `/api/v1` delete 行为（违 FR-010 既有行为不变原则），否决。
2. 编排中断（如 dispose 请求丢失）的最坏后果 = 资源滞留至 agent-v2 进程重启——历史本就是内存态（spec Assumptions），可接受；desktop 删除同名 session 的跨客户端场景同理（已知限制，后续 step 可加惰性校验）。
3. 幂等语义避免"删除编排竞态报错"（web 重试安全）。

**Alternatives considered**: *agent-v2 惰性校验 session 存在性*（GetSession per access）——引入对 session 服务的运行期依赖与延迟，收益仅清理边缘泄漏，否决（记录为后续 step 可选项）。

---

## D7 — 大型测试模型端点：扩展 `projects/game/fake-llm` 新增 `POST /v1/responses`（OpenAI Responses wire）

**Decision**（plan Q3 用户确认，2026-08-28）：既有 fake-llm（Go，chat-completions wire）**增量**新增 Responses 端点：同一模板/匹配/延迟设施上，按 OpenAI Responses SSE 事件词汇发流（`response.created` → `response.output_item.added`(reasoning) → `response.reasoning_summary_text.delta`* → `response.output_item.added`(message) → `response.output_text.delta`* → `response.output_item.done` → `response.completed`(usage)）。既有 chat-completions 端点与全部存量用例零改动。

**Rationale**:

1. 复用既有确定性设施：关键词模板 + 兜底（`projects/game/fake-llm/README.md`）、多轮条件（demo fake-llm 的 `history_keywords`/`min_turn` 模式，047 D7）、**可控延迟**（043/044 stall 测试已依赖——队列大型测试 FR-012 需要"回合进行中"的可控窗口）。
2. FR-007 要求端点可配置替换：agent-v2 经 `GLM_LLM_TARGET`（Dominion resolver target）解析 fake 地址注入 `GLM_BASE_URL`（D9），测试部署零外部网络（SC-001）。
3. Responses SSE 词汇以 OpenAI 官方 OpenAPI 规范为准（streaming events 定义：`response.output_text.delta`/`response.reasoning_summary_text.delta`/`response.completed` 等，[openai-openapi responses streaming](https://github.com/openai/openai-openapi)）。

**Alternatives considered**: *新建独立 fake 服务*——复制模板/延迟设施、测试部署多一服务，无隔离收益（fake-llm 本就是测试专用设施，不在 FR-010 保护清单内），否决；*复用 `experimental/dsh/demo/fake-llm`*——属 dsh-demo app（跨 app 部署耦合），且无延迟设施，否决。

**Spec 影响**: FR-011 大型测试确定性；契约见 [contracts/fake-responses-wire.md](contracts/fake-responses-wire.md)。

---

## D8 — web 前端与 web 服务：ui-primitives 组件级复用 + think/tool 自建 + 050 Go 静态服务

**Decision**:

1. **依赖**：`@deepseek-ai/dsh-client-ui-primitives@0.1.1-rc.2` **精确 pin**（`pnpm-workspace.yaml` catalog 引用；dsh 依赖统一 catalog 管理）。实测该包运行时**零 cordis/dsh-invariants import**（0.1.1-rc.2 tarball `lib/index.js` grep 实证；peerDependencies 声明为残留），React 18、11 个 `--dsw-*` CSS token 自建主题即可。
2. **复用组件**：`MessageText`/`MarkdownText`/`CodeBlock`/`JsonBlock`（markdown 渲染）、`DisclosureRow`（think 折叠外壳）、`IconThinkOutline14`/`StateDot`/`Button`/`Input` 等。
3. **自建组件**（参照源码）：`ReasoningRow`——照 `dsh-client-ui-chat` 的 [ReasoningRow.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.tsx)（DisclosureRow + 折叠摘要行 latest/first line + 流式跟随滚动 + `data-state=running`）；`ToolCard`——参照 [ui-tool ToolCallTree.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-tool/src/client/tool/ToolCallTree.tsx) 但剥离 slot 系统，仅名称/参数/状态/结果关联展示（US3）。
4. **web 服务**：`projects/game/web/server/`（Go + `embed.FS` dist + `http.FileServerFS`），逐行照 `experimental/js/vite_react_demo/server/main.go`（050 US4 已实证，样板说明见 [experimental/js/vite_react_demo/README.md](../../experimental/js/vite_react_demo/README.md)）。
5. **前端工程**：`projects/game/web/frontend/`（vite + React + vitest，`vite_build`/`vitest_test` 规则用法照 050 `experimental/js/vite_react_demo/BUILD.bazel`）；workspace 增 `projects/game/web/frontend` 条目；页面结构 = 侧栏 session 列表 + 主区对话（desktop `SessionList/ChatView/ChatMessage` 行为基线：列表/新建/删除/切换、text/think 区分渲染、流式合并、排队指示、tool_call 与 tool_result 按 tool_id 关联合并）。

**Rationale**: spec FR-009 组件级复用决策（2026-08-27 澄清 B 路线）；050 基建已落地（catalog react 栈 + vite_build React 支持 + 静态托管样板，`specs/050-vite-react-bazel/` 当前分支已交付）；BSD-3-Clause 许可允许参照改造（保留必要 attribution，交付时在包 README 注明）。

**Alternatives considered**: *依赖 `dsh-client-ui-conversation`/`dsh-client-ui-tool` 整包*——peer 闭包 25+ dsh client 包（连接层/slot 系统全链），为两个组件引入整个 web 客户端栈，否决（spec 澄清已定"参照自建"）。

---

## D9 — 配置与 secret 面：双 artifact（生产含 secret / 测试零 secret）+ token 三级解析（容忍缺失）+ `GLM_BASE_URL`/`GLM_LLM_TARGET`/`GLM_MODEL` 环境注入

**Decision**（2026-08-29 第二批用户指令修订；原"两级解析 + 缺失 fail-loud"语义被否决，见 Alternatives）：

1. **token**（FR-008）：bootstrap 三级解析——`GLM_API_KEY` 已设（trim 非空）直用；否则读 `$DOMINION_SECRET_DIR/glm-api-token` 文件 → `process.env.GLM_API_KEY`（运行时契约：文件路径 `/mnt/dominion/secret/glm-api-token`，`specs/002-deploy-secret-config/contracts/secret-config.md` §5）；两者皆缺失/空 → **保持未设并 log 一条 warning**（结构化日志，含 env 名与 secret 文件路径、不含任何 key 内容——SC-004）后继续 boot，不 fail-loud。插件侧空 key 请求不携带 Authorization header（[contracts/glm-llm-plugin.md](contracts/glm-llm-plugin.md) §3 义务 6），key 非空照常 `Bearer <key>`；cordis.yml `apiKeyEnv: GLM_API_KEY` 消费（官方 cookbook 推荐的 env 注入模式）。
2. **artifact 双声明（测试零 secret）**：`projects/game/agent_v2/service.yaml` 声明两个 artifacts——生产 `agent-v2`（target `:cmd_image`、tls、`secrets: [glm-api-token]`，`projects/game/deploy.yaml` 绑定 `glm-api-token: {secret: llm-secrets, key: glm-codingplan}` 不变）与测试 `agent-v2-test`（同 target、tls、**无 secrets**；artifact 终态名遵循 D13 命名约束——服务名与 artifact 名不含下划线）。deploy 服务条目经 `artifact.name` 按名选用（[tools/release/deploy/README.md](../../tools/release/deploy/README.md) §服务类型）：编译器的 secret 双向硬校验（artifact 声明的 secret 必须绑定、绑定必须声明）仅对**所选 artifact**生效——无 secret 声明的 artifact 无需任何绑定（`tools/release/deploy/v2/compiler/compiler.go` secret 编译段）。测试 `projects/game/testplan/deploy_agent_v2.yaml` 选用 `agent-v2-test`：零 secret 绑定、零 `GLM_API_KEY` env，测试集群无需为 agent-v2 预置 secret。双 artifact 是纯声明层方案，deploy 工具本身零改动。
3. **端点**（FR-007 可替换）：bootstrap 顺序——若 `GLM_BASE_URL` 已设则直用；否则若 `GLM_LLM_TARGET` 已设（Dominion resolver target，如 `dominion:///game/fake-llm:8080`）→ resolve → `GLM_BASE_URL = http://{endpoint}/v1`；否则默认 `https://open.bigmodel.cn/api/v1`。cordis.yml `baseURL: !!js process.env.GLM_BASE_URL`（047 D2：resolve 必须前移到 boot 前，`!!js` 惰性同步求值）。测试部署仅设 `GLM_LLM_TARGET`（fake 对 Authorization header 容忍——任意值忽略、无 header 亦无碍，[contracts/fake-responses-wire.md](contracts/fake-responses-wire.md) §1）。
4. **模型 id**：`GLM_MODEL`（默认 `glm-5.2`）。

**Rationale**: 现有 agent 的 secret 消费先例（`projects/game/agent/src/server.ts:124` 读 `$DOMINION_SECRET_DIR/provider`）；dsh cookbook 明示 "Secrets are cordis-native: schemastery Config with env fallbacks, fed from cordis.yml via `!!js process.env.MY_KEY`. Never read ad-hoc key files in code"——文件读取收敛在 bootstrap 单点后注入 env，插件自身零文件 IO。生产路径的正确性由真实冒烟（SC-003，生产 deploy.yaml 绑定 + 运维预置 secret）覆盖：真实端点对无 Authorization 请求 401 → 首轮 `turn_end{ERROR}` 明确呈现，不属测试关注面。

**Alternatives considered**:

- *token 缺失 fail-loud（原 D9 语义）*——被 2026-08-29 第二批用户指令否决：测试不应依赖真实 secret，最简单形态即允许 secret 文件不存在（LLM 请求 secret 为空）；fail-loud 迫使测试部署注入 dummy key 或在测试集群预置占位 secret，违背测试独立性（T033 首跑曾被迫在 `llm-secrets` 临时预置占位 `glm-codingplan` key 才能通过部署——该依赖由本决策消除）。
- *测试部署注入 `GLM_API_KEY=dummy-key`（demo 先例 `experimental/dsh/demo/testplan/deploy.yaml`）*——env 可满足 boot，但生产 artifact 的 secret 声明仍触发编译器"声明必须绑定"硬校验，测试部署仍需绑定 `llm-secrets/glm-codingplan`（即仍依赖测试集群预置 key）；用户指令"增加一个测试用 artifact 不包括 secret 配置"从声明层根除该依赖，否决 dummy-key 方案。
- *deploy 工具增加"测试态跳过 secret 校验"开关*——为测试态在工具层开特例，污染通用部署语义；双 artifact 纯声明层方案零工具改动即可达成，否决。
- *插件直接读 secret 文件*——违反 cookbook 约定且把部署细节烧进插件，维持原否决。

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

## D11 — bind 包扩展 server-streaming 泵：泛型 `ServerStreamBinder`（v1 bidi Bind 零行为改动）

**Decision**（2026-08-29 用户指令）：`projects/game/pkg/bind` 新增文件 `server_stream.go`，定义泛型 server-streaming 转发泵。上游流为**已发送形态**：请求的发送与半关闭由调用方的建流调用承载（grpc-go 生成客户端的 server-streaming 方法在 `Send(ctx, in)` 内即完成 `SendMsg(in)` + `CloseSend()`，返回仅暴露 `Recv` 的响应流——`grpc.ServerStreamingClient`，[grpc-go v1.80.0 stream_interfaces.go](https://github.com/grpc/grpc-go/blob/v1.80.0/stream_interfaces.go)），泵只消费响应半流。接口形态：

```go
// ServerSendStream is the downstream half of a server-streaming forward: it
// emits response frames to the original caller (grpc-go's generated
// XxxServer stream interfaces satisfy this structurally).
type ServerSendStream[Resp any] interface {
    Send(*Resp) error
}

// UpstreamStream is the upstream half: the response stream of a
// server-streaming call whose request was already sent and half-closed by
// the stream-open call (grpc's ServerStreamingClient[Resp] satisfies this
// structurally).
type UpstreamStream[Resp any] interface {
    Recv() (*Resp, error)
}

// ServerStreamBinder forwards one server-streaming RPC's response half:
// relay response frames downstream until the upstream ends.
type ServerStreamBinder[Resp any] interface {
    BindServerStream(downstream ServerSendStream[Resp], upstream UpstreamStream[Resp]) error
}

func NewServerStreamBinder[Resp any]() ServerStreamBinder[Resp]
```

语义：调用方先发起上游调用（`client.Send(ctx, req)` 建流，请求发送与半关闭由生成代码承载），再将返回的响应流与下游流交给泵；泵 = `upstream.Recv()` 循环逐帧 `downstream.Send`；`io.EOF`/`context.Canceled` 归一化为 nil（与 v1 `Binder.Bind` 的 report 归一化一致，`projects/game/pkg/bind/binder.go`）；其余错误原样返回（proxy handler 以 gRPC status 语义透传）。下游断开由共享 ctx 联动：downstream ctx 取消 → upstream（同 ctx 建流）Recv 报 Canceled → 干净关闭。proxy 侧实例化 `bind.NewServerStreamBinder[gamev2.ChatEvent]()` 注入 ConversationHandler（DI seam 对齐 v1 TeamHandler 注入 `bind.Binder` 的构造模式）。

**Rationale**:

1. **用户指令**：server stream 支持落位 bind 包（进程内流泵是 bind 的职责面——v1 注释即自述"bidirectional stream binding logic"，扩展单向形态同域）。
2. **与生成代码形态对齐**：protoc-gen-go-grpc v1.5.1（grpc-go 1.80.0）对 server-streaming 方法生成的客户端是 `Send(ctx, in) (grpc.ServerStreamingClient[Resp], error)`——建流调用内完成 `SendMsg(in)` + `CloseSend()`，返回流仅含 `Recv`（[grpc-go v1.80.0 stream_interfaces.go](https://github.com/grpc/grpc-go/blob/v1.80.0/stream_interfaces.go)）。Recv-only 的 `UpstreamStream[Resp]` 使生成流无需任何适配层即结构满足；请求发送是"调用 RPC 方法"的一部分，归调用方（handler）职责——建流失败在调用点映射 UNAVAILABLE（tasks.md T037），泵只负责响应帧中继与收尾归一化。
3. **v1 `Binder.Bind` 接口类型锁死 v1 帧**：`Bind(left UserFrameStream, right TeamFrameStream)` 绑定 `game.UserFrame/TeamFrame`（`projects/game/pkg/bind/binder.go`），无泛型/单向形态；ChatEvent 类型又不属于 v1 帧类型，须以泛型参数化（Go 方法不允许新增类型参数，故为独立泛型类型 + 构造函数，而非在 binder 上加方法）。
4. **零行为改动约束**：v1 两个使用点（proxy 转发 Connect bidi：`projects/game/proxy/handler/handler.go:216-217`；gateway WebSocket 桥：`projects/game/gateway/cmd/main.go`）不动；新实现独立成文件，v1 `binder.go`/`first_frame.go` 零 diff（constitution 原则 II 扩展而非打补丁，存量零回归可验）。
5. **错误归一化复用**：泵语义（EOF/Canceled→nil、首错优先）与 v1 report 行为一致，消费方（handler→gRPC status）获得相同契约。

**Alternatives considered**: *upstream 接口自含请求发送（`Send(*Req)` + `CloseSend()`，泵先发请求再中继）*——与 grpc-go 生成流形态矛盾：生成流建流时已发送并半关闭，接口无类型化 `Send` 方法，每个消费方必须携带 no-op 适配层、接口承诺与事实不符，否决；*proxy handler 内联手写转发循环*——泵语义（错误归一化、下游断开联动）散落各服务无法复用，违背用户"扩展 bind"指令，否决；*把 v1 Binder 泛型化重写*——触碰两个生产使用点换取零行为收益，回归风险不对称，否决。

**Spec 影响**: FR-010 例外 (c)；消费方实现要点见 tasks.md T036/T037。

---

## D12 — proto 归属：app 根目录同目录独立文件 `projects/game/agent_v2.proto`（package `projects.game.v2` 不变）

**Decision**（2026-08-29 用户指令）：proto 文件从 `projects/game/agent_v2/agent_v2.proto` 移至 **`projects/game/agent_v2.proto`**（与 `game.proto` 同目录）；proto 内容零改动——package 保持 `projects.game.v2`、`go_package` 保持 `dominion/projects/game/v2`。Bazel 目标布局：

- Go 消费链：`proto_library`（`agent_v2_proto`）+ `go_proto_library`（`agent_v2_go_proto`，importpath `dominion/projects/game/v2`）+ `go_library` 移入 **`projects/game/BUILD.bazel`**，与 `game_proto`/`game_go_proto` 并列（app 根目录承载该 app 全部 proto 的既有惯例）；deps 仅 `@googleapis//google/api:annotations_go_proto`（linker 冲突约束注释随迁，见 Rationale-4）。
- TS 消费链：`ts_proto_library`（`agent_v2_types`）+ `js_library` **保留在 `projects/game/agent_v2/BUILD.bazel`**，`proto = "//projects/game:agent_v2_proto"` 跨目录引用——逐字复用 agent v1 先例（`projects/game/agent/BUILD.bazel:20-24` 的 `game_types` 引用 `//projects/game:game_proto`）；agent_v2 src 的类型 import 路径（`../agent_v2_types/projects/game/v2/...`）与生成物路径（由 proto package 决定）全部不变。
- agent_v2 运行时：`artifact_pkg_js.runtime_protos` 改指 `//projects/game:agent_v2_proto`；proto 文件按标准导入路径物化于服务根下（`tools/release/deploy/README.md` §runtime_protos），`server.ts` 的 `PROTO_PATH` 改为 `SERVICE_ROOT/projects/game/agent_v2.proto`（demo 先例：`experimental/dsh/demo/agent/src/server.ts:27` 以 canonical 路径加载根目录 proto）。

**Rationale**:

1. **app 根目录 proto 惯例 + 用户指令**：game app 全部服务 proto 集于根 `game.proto`，各服务目录无 proto、一律引用根 proto（`projects/game/BUILD.bazel` 的 `game_proto`；agent v1 跨目录引用即既有惯例）——用户明示"没看到有分两个目录的理由"，agent_v2 子目录 proto 是唯一例外，消除。
2. **版本化 API 的包隔离保留**：`/api/v2` 是独立版本面，`projects.game.v2` 独立 proto package 使 v2 专属类型（ChatEvent/BlockType 等）不进 v1 命名空间——同目录多文件、按版本分包是 proto API 版本化的标准形态；并入 `game.proto`（package `projects.game`）会让 v2 类型共享 v1 无版本命名空间，且每个 v2 变更都触碰 v1 契约文件。
3. **返工最小化**：文件内容与 importpath 不变 → gateway 的 `gamev2 "dominion/projects/game/v2"` import 不变（仅 BUILD label 与 gazelle:resolve 改指 `//projects/game:agent_v2`）；agent_v2 src 类型 import 不变；server.ts 仅改一个路径常量。
4. **linker 约束随迁不变**：`@googleapis//google/api` 的 `annotations_go_proto` 与 `field_behavior_go_proto` 共享同一 Go importpath，不能同时进 deps（"multiple copies of package passed to linker"）——`agent_v2_go_proto` 沿用 annotations_go_proto 单 dep 形态（该约束已实证于 agent_v2 BUILD 的既有注释与构建）。
5. **demo 先例同构**：dsh-demo app 的 proto 在 app 根（`experimental/dsh/demo/chat.proto`），服务目录引用根 proto——独立 app 的同构形态。

**Alternatives considered**: *并入 `game.proto` 单文件（package `projects.game`）*——v2 类型进 v1 命名空间（版本语义损失）、agent_v2 全部 TS 类型 import 路径重写（`projects/game/v2`→`projects/game`）、v2 演进持续触碰 v1 契约文件；同目录独立文件已满足"放到一起"，否决；*留在 `projects/game/agent_v2/` 子目录*——被用户指令否决。

**Spec 影响**: [contracts/conversation-api.md](contracts/conversation-api.md) §1；tasks.md T039（返工）/T016/T017（引用修订）。

---

## D13 — 服务发现名 `agent-v2`（deploy API 命名约束）+ proxy 对"管理服务缺席"的容忍 ⭐

**Decision**（2026-08-29 第三批用户裁定，方向 1）：

1. **服务发现名与服务 artifact 名改用 `agent-v2`**：deploy 服务发现 API 对 app/service 名的约束为 `^[a-z][a-z0-9-]{0,19}$`（`projects/infra/deploy/domain/service_endpoints_name.go:14` `appNamePattern`，经 `NewServiceEndpointsName` 强制校验），**不允许下划线**。原名 `agent_v2` 部署虽成功（deploy 编译器对 k8s 资源名自动清洗为 `agent-v2`），但 endpoints 查询资源名 `deploy/scopes/{scope}/environments/{env}/apps/game/services/agent_v2/endpoints` 被 API 以 400 "invalid resource name" 拒绝——服务发现永不可达（SigNoz 实证：proxy `agentclient-manager-v2` daemon start failed 循环）。终态命名：
   - `projects/game/agent_v2/service.yaml`：服务名 `name: agent-v2`；artifact 名 `agent-v2`（生产）/ `agent-v2-test`（测试）——对齐 v1 先例"服务名 = 生产 artifact 名"（服务 `agent` + artifact `agent`/`agent_test`，`projects/game/agent/service.yaml`），测试后缀统一连字符与服务名形态一致。
   - 联动引用：`projects/game/pkg/gameconst/const.go` `AgentV2Target = "game/agent-v2:grpc"`（`solver.ParseTarget` 对 service 段无字符校验、port 段 `grpc` 为合法 DNS label，`common/gopkg/solver/target.go:100-129`——解析合法）；`projects/game/deploy.yaml` 条目 `name: agent-v2`；`projects/game/testplan/deploy_agent_v2.yaml` 条目 `name: agent-v2-test`（`artifact.name` 按名选用语义，[tools/release/deploy/README.md](../../tools/release/deploy/README.md) §服务类型）；`projects/game/agent_v2/src/bootstrap.ts:41` OTel reporter 服务身份 `"game/agent-v2"`（SigNoz 可见服务名；该文件既有日志字段 `service: "game-agent-v2"` 本已连字符，二者自此一致）。
   - `agent-v2` 通过 appNamePattern（8 字符、小写+连字符）；与 k8s 清洗名一致（重部署零 churn）、与 suite 名 `agent-v2-conversation`、fake 触发词 `agent-v2-*`（`projects/game/testplan/agent_v2_helpers_test.go`）、BIN_NAME `game-agent-v2`（`projects/game/agent_v2/src/dsh.ts`）、npm 包名 `@dominion/game-agent-v2` 同构。
   - **不改动**（用户裁定边界）：目录 `projects/game/agent_v2/`、proto 文件 `projects/game/agent_v2.proto` 及 package `projects.game.v2`/go_package、`/api/v2` 路径、bazel target 名（`agent_v2_proto`/`agent_v2_types` 等）、测试文件名（`agent_v2_conversation_test.go` 等）、Mongo collection `game_proxy.agent_v2_owners`（存储标识符非发现名、无命名约束，改名零行为收益）、env `GLM_*`。

2. **proxy 容忍落位 agentclient manager.refresh（错误分类，候选 i）**：`projects/game/proxy/runtime/agentclient/manager.go` 的 `refresh()` 对 resolver 错误做分类——`errors.Is(err, solver.ErrServiceNotFound)` 时归一为**空实例集**（warn 日志后以空集走既有收尾逻辑：陈旧连接关闭、连接表清空、返回 nil），服务出现后由周期刷新（30s）自然发现。daemon 重启策略与其余错误类别零改动。接口签名级终态：

   ```go
   instances, err := m.resolver.Resolve(ctx, m.target)
   if err != nil {
       if errors.Is(err, solver.ErrServiceNotFound) {
           // 管理的有状态服务在本环境不存在（拓扑状态而非故障）：归一空实例集，
           // 周期刷新在服务出现后自然发现（specs/049-agent-v2-dsh-init/research.md D13）。
           logs.Warn(ctx, "stateful service not found, treating as empty instance set", /* app/service 字段 */)
           instances = nil
       } else {
           return fmt.Errorf("agentclient: resolve failed: %w", err)
       }
   }
   ```

   错误判定链路实证：`common/gopkg/solver/deploy_http_client.go:56-57`（HTTP 404 → 裸返回 `solver.ErrServiceNotFound`）→ `common/gopkg/solver/deploy_stateful_resolver.go:48`（`%w` 包装）→ `manager.go:140`（`%w` 包装）——`errors.Is` 全程可达，无需新增包装约定。注意 400 "invalid resource name" 走 `deploy_http_client.go:59-62` 普通 error 路径（非 sentinel），不在容忍范围——错误的目标名是配置/程序错误，保持致命语义正确。

**Rationale**:

1. **命名约束是 deploy API 硬约束**：改名是最小修复；deploy API 本身不动（用户边界：不动 `projects/infra/deploy`）。
2. **crash 根因与回归面**：`bootstrap.Daemon` 对初始 refresh 失败指数退避重启、默认 `maxRestarts=5` 耗尽后 `DaemonFatal` 全局退出（`common/gopkg/bootstrap/daemon.go:128-145`、`applyRestartPolicy`）。agent-v2 测试部署不含 v1 agent → proxy 的 v1 manager（`game/agent` → 404）耗尽重试 → proxy crash loop（实测 ~30s 周期 5 restarts）。既有 suite 凡部署 proxy 必同时部署 v1 agent，v1 路径从未暴露；但 T037 起 proxy 恒装配 v2 daemon——**全部既有 suite（不含 agent-v2 的部署）都暴露于 v2 路径的同一 crash loop**，容忍修复因此是 SC-005（存量零回归）的必要条件，而非仅测试环境便利。
3. **语义准确性**："管理服务不存在"是拓扑状态而非故障——与 resolver 对"API 成功 + 零实例"的既有容忍（`common/gopkg/solver/deploy_stateful_resolver.go:73-76` warn + nil,nil）同一语义上移一层；空池下 handler 行为已由契约定义（无实例连接 → UNAVAILABLE → 503，[contracts/conversation-api.md](contracts/conversation-api.md) §2.1），无需 handler 改动。
4. **零回归**：`ErrServiceNotFound` 仅在目标服务缺席 deploy API 时出现——凡 v1 agent 与 proxy 同部署的环境（全部既有 suite）v1 manager 永不触发；v2 manager 触发场景恰是当前 crash-loop 待修回归。其余错误类别路径与重启策略不变。

**Alternatives considered**:

- *daemon 错误分类器（`WithDaemonErrorClassifier` 映射 not-found + `WithDaemonMaxRestarts(-1)`，候选 ii）*——`maxRestarts=-1` 对**所有**错误类别解除上限：真实故障（API 5xx/网络分区）从"5 次后 fatal"退化为无限重试，错误类语义合流；classifier 全量替换默认分类器（`common/gopkg/bootstrap/daemon.go` `classify`/`defaultErrorClassifier`），需在 proxy 重复实现 bootstrap 默认规则；且重启语义错配——每次重启 worker.Stop → `manager.Close()` 关闭该池全部缓存连接，并按周期刷 "daemon start failed" ERROR 日志（即 crash-loop 观测特征），对可能持续整个 suite 时长的拓扑状态制造海量噪音；恢复时延（backoff 封顶 30s）相比周期刷新（30s）无优势。否决。
- *容忍落在 resolver（`common/gopkg/solver`）层*——resolver 是共享通用库，not-found → 空集会改变全部消费方语义（对其他消费方 fail-loud 可能正确）；容忍是 proxy 的部署拓扑需求，落在 proxy 私有 runtime（agentclient）影响面最小。否决。
- *deploy API 放宽命名约束*——被用户边界否决（不动 `projects/infra/deploy`）。
- *目录/proto/包名一并改连字符*——被用户裁定否决（目录不动）；这些标识无 API 约束，改名纯 churn。
- *proxy 按"v1 池必须存在、v2 池可缺席"区别对待*——硬编码拓扑假设；两个 manager 共享同一 refresh 代码路径，统一容忍既是最小实现也覆盖"agent-v2 暂时不可发现"（滚动更新窗口）。否决。

**命名区分规则（sweep 边界，防止误伤）**：`agent_v2` → `agent-v2` 仅当 token 是 deploy/discovery 平面标识符——service.yaml `name:` 值、artifact 名（含 deploy yaml `artifact.name` 选用值）、target 字符串服务段（`game/agent-v2:grpc`）、OTel 服务身份串（`game/agent-v2`）——或行文主语为可部署服务/实例/StatefulSet/artifact；以下保持 `agent_v2`：目录与工作区路径（`projects/game/agent_v2/`、`pnpm-workspace.yaml` 条目、lockfile）、文件名（`agent_v2.proto`、`deploy_agent_v2.yaml`、`agent_v2_conversation_test.go`、`agent_v2_helpers_test.go`、fake-llm testdata `agent_v2.yaml`）、bazel target 名与生成类型 import 路径（`agent_v2_proto`/`agent_v2_types`/`../agent_v2_types/...`）、proto package `projects.game.v2`/go_package/`/api/v2`、npm 包名/BIN_NAME、Mongo collection `game_proxy.agent_v2_owners`、env `GLM_*`、指称代码模块/项目的行文与未被返工触碰的代码注释。

**Spec 影响**: spec.md FR-016（新增）、FR-008/SC-005 关联修订；tasks.md T044–T047（返工）。

---

## 决策汇总

| # | 决策 | 状态 |
|---|---|---|
| D1 | GLM Responses 自研插件 `@dominion/dsh-llm-glm` @ `common/js/dsh-plugins/llm-glm` | ✅ 用户确认（Q2） |
| D2 | agent_v2 极简宿主 = demo 样板 + 队列/历史/流式转发托管模块 | ✅ 设计定（用户指令边界） |
| D3 | cordis.yml 两行：agent-spine 五裁剪 + llm-glm（models 显式目录） | ✅ 设计定 |
| D4 | `/api/v2` 对话 API = gRPC server-streaming per send（NDJSON chunked）+ proxy 有状态路由（gateway→proxy→agent-v2，owner 亲和） | ✅ 用户确认（Q5，2026-08-28；2026-08-29 用户指令修订路由拓扑、owner store 构造语义化） |
| D5 | 同主机名路径分流（game.liukexin.com：`/`→web、`/api/*`→gateway），零 CORS | ✅ 用户确认（Q1） |
| D6 | 删除编排 = web 前端 DELETE /api/v1 → POST :dispose（幂等） | ✅ 用户确认（Q4） |
| D7 | fake Responses = 扩展 projects/game/fake-llm 新增 /v1/responses 端点 | ✅ 用户确认（Q3） |
| D8 | 前端组件级复用 ui-primitives@0.1.1-rc.2 + think/tool 参照自建 + 050 Go 静态服务 | ✅ 设计定 |
| D9 | 双 artifact（生产 `agent-v2` 含 secret 绑定不变 / 测试 `agent-v2-test` 零 secret）+ GLM_API_KEY 三级解析（env → secret 文件 → 未设+warning，无 fail-loud；空 key 请求免 Authorization）；GLM_BASE_URL/GLM_LLM_TARGET/GLM_MODEL 注入 | ✅ 用户指令（2026-08-29 第二批） |
| D10 | 队列（每会话 FIFO）+ 历史（会话生命周期双通道收集）+ idle 终止 | ✅ 设计定 |
| D11 | bind 包扩展泛型 `ServerStreamBinder` server-streaming 泵（v1 bidi Bind 零行为改动） | ✅ 用户指令（2026-08-29） |
| D12 | proto 归位 `projects/game/agent_v2.proto`（与 game.proto 同目录，package `projects.game.v2` 不变；Go 目标入根 BUILD、TS types 跨目录引用） | ✅ 用户指令（2026-08-29） |
| D13 | 服务发现名 `agent-v2`（service.yaml `name`/artifact 名/target 串/OTel 服务身份，目录/proto/包名不动）+ proxy agentclient 对 `ErrServiceNotFound` 容忍（空实例集语义，manager.refresh 错误分类） | ✅ 用户裁定（2026-08-29 第三批，方向 1） |

**已知限制（终态记录，非遗留）**：多标签页无实时推送（未发送消息的页面不看到他页回合，可刷新经 history 查询）；desktop 删除同名 session 不触发 agent-v2 释放（跨客户端编排留待后续 step）；对话历史随 agent-v2 重启丢失（spec Assumptions 明示接受）。
