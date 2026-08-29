# Implementation Plan: Game Agent v2 — dsh 迁移 Step 1：session 对话页面与模型接入

**Branch**: `049-agent-v2-dsh-init` | **Date**: 2026-08-28 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/049-agent-v2-dsh-init/spec.md`

## Summary

以两个新项目 `projects/game/agent_v2`（嵌入 dsh 的 gRPC agent 服务，B1 模式，样板 `experimental/dsh/demo/agent/`；**有状态服务 `kind: stateful`**）与 `projects/game/web`（React 网页服务，前端组件级复用 dsh-web）为载体，把 session 管理 + session 对话页面从 desktop 迁移到网页，并经自研 dsh LLM 适配插件接入 GLM codingplan（OpenAI Responses 协议端点 `https://open.bigmodel.cn/api/v1`），达成"浏览器里对 game session 完成一次流式对话（text/think 端到端可见，tools 渲染能力就绪、本阶段零工具）"。

架构硬约束（用户指令，2026-08-28）：**所有对 dsh 的扩展（GLM Responses 适配器等）都以插件形态交付；agent_v2 服务本体只保留"嵌入并服务 built-in dsh"所需的最小宿主代码**（bootstrap、组合清单、gRPC 服务面、会话注册表/队列/历史等托管逻辑）。

架构硬约束（用户指令，2026-08-29）：**agent_v2 为有状态服务，经存量 proxy 亲和路由接入、不被 gateway 直连**（gateway→proxy→agent_v2 两跳，proxy 按 owner 映射定向实例，gateway 仍是唯一 HTTP 出口）；**proxy 转发 server-streaming 流的泵以 bind 包扩展交付**（v1 双向 Bind 零行为改动）；**agent_v2 proto 与 game 既有 proto 同目录**（app 根目录）；**proxy owner store 的 database/collection 命名知识归 store 层**（语义化构造 `NewAgentOwnerStore`/`NewAgentV2OwnerStore`，常量私有化于 `runtime/mongo` 包内，`cmd/main.go` 只做装配不出现 collection 字符串）。

技术路线（编号对应 [research.md](research.md) 决策号）：

- **D1 插件形态**：GLM Responses 适配器为独立 workspace 插件包（cordis 插件：`name`/`inject: ['llm']`/schemastery `Config`/`apply` 注册 `ctx.llm.registerAdapter`），实现 `LlmAdapter.stream()`（唯一抽象方法），按官方 cookbook（`docs/cookbook/adding-an-llm-adapter.md`）协议义务发 StreamChunk（reasoning-delta 一等支持）。
- **D2 宿主极简**：agent_v2 = demo 样板骨架（boot 前 resolve/env 注入 + fail-loud + 优雅退出 + get-or-create 会话注册表）+ 每会话 FIFO 队列 + 内存历史收集（`assistant/chunk` 流式 + `assistant/message` 终局）+ gRPC server-streaming 服务面。
- **D4 对话 API 与有状态路由**：proto `projects.game.v2` 位于 app 根 `projects/game/agent_v2.proto`（`/api/v2` 前缀，AIP-136 custom methods `:send`(server-streaming)/`:history`/`:dispose`）；HTTP 出口在 gateway（grpc-gateway 新增路由绑定既有 `/api/v1` 零改动，handler 挂既有 proxy conn）；proxy 增量注册 ConversationService 转发面（owner 亲和：Mongo `game_proxy.agent_v2_owners` + hash picker + agent_v2 专属 agentclient manager）。
- **D11 bind 扩展**：`projects/game/pkg/bind` 新增泛型 `ServerStreamBinder[Resp]` server-streaming 泵（Recv-only 已发送形态上游流，proxy 转发 Send 流的载体；v1 bidi Bind 与其两个使用点零改动）。
- **D8 前端**：vite + React（050 基建），复用 `@deepseek-ai/dsh-client-ui-primitives@0.1.1-rc.2`（运行时零 cordis 依赖，实测），think 折叠/工具卡参照 `dsh-client-ui-chat` 的 `ReasoningRow` 与 `ui-tool` 源码自建；Go 静态服务托管 dist（050 `vite_react_demo/server` 样板）。
- **D7 大型测试**：fake Responses 端点替换真实端点（FR-007 可配置 baseURL），经 testplan skill 实际执行部署→测试→清理闭环，全部用例通过为验收。

## Technical Context

**Language/Version**: TypeScript（ESM，Node ≥24 运行时；tsconfig `module: nodenext`，`specs/048-js-esm-migration/contracts/esm-package-conventions.md`）+ Go 1.x（gateway/web 静态服务）+ React 18（前端）

**Primary Dependencies**:
- dsh 家族 0.1.1-rc.2 精确 pin（对齐 `third_party/dsh/core` 与 047 锁定决策）：`@deepseek-ai/dsh-app-boot`、`@deepseek-ai/dsh-agent`、`@deepseek-ai/dsh-agent-spine-demo`、`@deepseek-ai/dsh-llm`、`@deepseek-ai/dsh-client-ui-primitives`（前端）
- 自研插件：`@dominion/dsh-llm-glm-responses`（workspace 包）
- `@grpc/grpc-js` + `@grpc/proto-loader`（agent_v2 gRPC 面）
- grpc-gateway v2（gateway 新增 `/api/v2` 路由，handler 挂既有 proxy conn）；grpc-go + Mongo driver（proxy ConversationService 转发面增量）；vite/vitest（前端，catalog）

**Storage**: 会话元数据 = 现有 game session 服务（Mongo，零改动）；对话历史 = agent_v2 进程内存（FR-014，随进程重启丢失，spec Assumptions 接受）；agent_v2 owner 路由映射 = proxy 侧 Mongo `game_proxy.agent_v2_owners`（复用 v1 OwnerStore 设施，与 `agent_owners` 隔离，research.md D4）

**Testing**: vitest（agent_v2/插件/前端组件单测）+ go_unittest（gateway）+ testplan 大型测试（`guitar run`，constitution 原则 VI）

**Target Platform**: linux/amd64 容器（bazel 构建 + `service.yaml`/`deploy.yaml` 声明 + 服务发现寻址）

**Project Type**: 双新服务（有状态 gRPC agent 服务 + 网页静态服务）+ 1 个 dsh 插件包 + 存量 gateway/proxy/`pkg/bind` 增量（路由注册、ConversationService 转发面、server-streaming 泵）

**Performance Goals**: 无特殊指标；流式首字节与渐进呈现为功能要求（FR-004），非性能 SLA

**Constraints**: token 零泄漏（SC-004，secret 机制 `specs/002-deploy-secret-config/`）；存量服务与 `/api/v1` 零回归（SC-005/FR-010）；大型测试零外部网络依赖（SC-001）

**Scale/Scope**: 单人内网使用；session 数十级；无鉴权（内网，spec Assumptions）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 状态 | 说明 |
|---|---|---|
| I. 引用溯源 | ✅ | 所有代码/文档引用带仓库相对路径或完整 URL（本计划与 research/contracts 均执行） |
| II. 重构式变更 | ✅ | 新需求以**新项目**承载（agent_v2/web/插件包），不为旧 agent 打补丁；存量保持原样是明确的设计决策而非回避 |
| III. 接口优先 | ✅ | Phase 1 产出 `contracts/`：对话 API 契约（proto/REST/stream 事件序）、GLM 插件契约（cordis 行配置/StreamChunk 义务）、fake Responses wire 契约、前端组件契约 |
| IV. 测试颗粒度 | ✅ | 编译+单测为每 phase 内嵌门禁（`bazel build`/`bazel test`，不单列 task）；大型测试单列验收 phase（FR-011） |
| V. 编码前阅读文档 | ✅ | tasks 阶段每 phase 三分类文档清单（本 plan 已实测阅读全部引用文档） |
| VI. 服务型应用大型测试验收 | ✅ | Phase 7 大型测试经 testplan skill 实际执行 `guitar run`（部署→测试→清理闭环），全部用例通过为验收；不以构建检查替代 |
| VII. 终态表述 | ✅ | 交付物只表述终态；被否决方案仅在 research.md 记录决策依据 |

**违规项**: 无。

## Project Structure

### Documentation (this feature)

```text
specs/049-agent-v2-dsh-init/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── conversation-api.md      # /api/v2 对话 API 契约（proto/REST/流事件序）
│   ├── glm-llm-plugin.md       # GLM Responses dsh 插件契约（行配置/适配义务）
│   ├── fake-responses-wire.md   # fake Responses 端点 wire 契约
│   └── web-frontend.md          # 前端组件与状态契约
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
common/js/dsh-plugins/
└── llm-glm/                          # @dominion/dsh-llm-glm（workspace 插件包，plan Q2 决策：
│                                     #   GLM 适配为与服务无关的基础能力；common/js/** glob 已覆盖）
│   ├── src/
│   │   ├── index.ts                  # cordis 插件（name='llm-glm'/inject=['llm']/Config/apply → registerAdapter）
│   │   ├── adapter.ts                # GlmResponsesAdapter extends LlmAdapter（stream()）
│   │   ├── serialize.ts              # GenerateOptions.messages → Responses input 序列化
│   │   ├── wire.ts                   # Responses SSE 事件类型 + 解析（eventsource-parser）
│   │   └── adapter.test.ts / serialize.test.ts / wire.test.ts
│   └── package.json / tsconfig.json / .swcrc / BUILD.bazel

projects/game/
├── agent_v2.proto                    # ConversationService 契约（package projects.game.v2；与 game.proto 同目录，
│                                     #   research.md D12 用户决策）
├── BUILD.bazel                       # 增量：agent_v2_proto（proto_library）+ agent_v2_go_proto（go_proto_library，
│                                     #   importpath dominion/projects/game/v2）+ agent_v2（go_library），与 game_proto 并列
├── pkg/
│   ├── gameconst/const.go            # 增量：AgentV2Target = "game/agent_v2:grpc"（消费方 = proxy 有状态解析）
│   └── bind/
│       ├── binder.go / first_frame.go        # 不动（v1 bidi，两个既有使用点）
│       ├── server_stream.go                  # 增量：泛型 ServerStreamBinder server-streaming 泵（research.md D11）
│       └── server_stream_test.go
├── proxy/
│   ├── cmd/main.go                   # 增量：agent_v2 owner store/manager/daemon 装配 + ConversationService 注册
│   ├── handler/
│   │   ├── handler.go                # 不动（v1 TeamService）
│   │   ├── handler_test.go           # 不动
│   │   ├── conversation.go           # 增量：ConversationHandler（Send 流转发/ListHistory/Dispose 定向 + owner 亲和）
│   │   └── conversation_test.go
│   └── runtime/
│       ├── agentclient/manager.go    # 修订：conn factory 与 daemon name 参数化（v1 行为零改动）
│       ├── mongo/owner_store.go      # 修订：语义化构造 NewAgentOwnerStore/NewAgentV2OwnerStore（db/col 常量
│       │                             #   包内私有，col 命名知识归 store 层——research.md D4）
│       └── picker/hash_picker.go     # 不动（复用）
├── agent_v2/                         # agent_v2 服务（dsh 宿主，最小集——用户指令；kind: stateful）
│   ├── cordis.yml                    # 组合清单：agent-spine（五裁剪）+ llm-glm 两行（data-model.md §2.6）
│   ├── src/
│   │   ├── bootstrap.ts / dsh.ts     # OTel→secret/env 注入→boot(fail-loud)→server→优雅退出（demo 样板）
│   │   ├── server.ts                 # gRPC 面（Send 流/ListHistory/Dispose，TLS 机会加载；proto 按标准导入路径
│   │   │                             #   加载 SERVICE_ROOT/projects/game/agent_v2.proto）
│   │   ├── session.ts                # 会话注册表 + 每会话 FIFO 队列 + dispose（get-or-create 模式）
│   │   ├── history.ts                # 内存对话记录（chunk 流式转发 + message 终局，会话生命周期收集器）
│   │   └── *.test.ts
│   └── package.json / tsconfig.json / .swcrc / BUILD.bazel（ts_proto_library 引用 //projects/game:agent_v2_proto，
│                                     #   agent v1 game_types 跨目录先例）/ service.yaml（kind: stateful、secrets: [glm-api-token]）
├── web/                              # web 服务（FR-013：页面独立 serve + API 经 gateway）
│   ├── frontend/                     # vite + React（workspace 包，050 模式；ui-primitives@0.1.1-rc.2 精确 pin）
│   │   ├── src/
│   │   │   ├── api/                  # /api/v1 session CRUD + /api/v2 NDJSON 流客户端 + 删除编排（D6）
│   │   │   ├── components/           # SessionList / ChatView / ReasoningRow(自建) / ToolCard(自建)
│   │   │   ├── store/                # 会话状态 + 流事件 reducer（useSyncExternalStore）
│   │   │   └── *.test.tsx            # US3 构造数据组件测试 + reducer/NDJSON 测试
│   │   ├── index.html / vite.config.ts / package.json / BUILD.bazel / theme.css
│   └── server/                       # Go 静态服务（embed dist，照 experimental/js/vite_react_demo/server）
│       └── main.go / assets / BUILD.bazel / service.yaml
├── gateway/cmd/main.go               # 增量：ConversationServiceHandler 注册挂既有 teamConn（经 proxy）+ /api/v2/ 子树
│                                     #       （既有 /api/v1 不动；无 agent_v2 直连 conn）
├── fake-llm/                         # 增量：新增 POST /v1/responses 端点（Responses wire，复用模板/延迟设施）+ testdata
├── deploy.yaml                       # 增量：agent_v2（kind: stateful 由 service.yaml 声明；secret 绑定
│                                     #   glm-api-token→llm-secrets/glm-codingplan）+ web（host game.liukexin.com `/`）
│                                     #   + gateway matches 增 /api/v2/（同主机名路径分流，research.md D5）
└── testplan/
    ├── deploy_agent_v2.yaml               # 大型测试部署（fake-llm 替换真实端点，GLM_LLM_TARGET 注入；
    │                                     #   services 含 proxy——/api/v2 两跳路由依赖）
    ├── agent_v2_conversation_test.go      # 大型测试用例（模块=agent_v2 对话面，T031）
    ├── web_test.go                        # 大型测试用例（模块=web 服务，T031）
    └── system_test.yaml                   # 增量：新增 suite（引用 deploy 与用例 target；不新建独立计划，style/large_test.md §测试计划数量）
```

**Structure Decision**: 三类交付物分离——**插件包**（dsh 扩展，`common/js/dsh-plugins/`，为后续工具插件建立公共归属地，plan Q2 用户决策）、**agent_v2 宿主**（仅"服务 built-in dsh"所需代码，用户指令；有状态服务经 proxy 路由，用户 2026-08-29 指令）、**web**（前端 workspace 包 + Go 静态服务，050 已验证形态）。存量服务结构零改动（gateway/proxy/`pkg/bind`/fake-llm/gameconst 仅增量，v1 行为零改动）。proto 收敛于 app 根目录（用户 2026-08-29 指令，research.md D12）。对话内容接口与 flow 控制分离（用户既定方向，research.md D4）。

## Complexity Tracking

> 无 Constitution 违规需豁免。新增 3 个包（插件/agent_v2/web）各自对应 spec FR-001/FR-002/FR-007/FR-009 的显式交付物要求，非过度设计。

## Phase 1 后 Constitution 复核（设计产物）

| 产物 | 合规点 |
|---|---|
| research.md | 原则 I（每条决策附仓库路径/URL 实证）；原则 VII（被否决方案仅记录决策依据；"已知限制"为终态记录非迭代痕迹） |
| data-model.md | 原则 II（存量零改动+新项目承载的边界论证）；实体与 FR 映射完整（FR-004/005/006/012/014/015 可溯） |
| contracts/ 四份 | 原则 III（接口先行：proto/REST/流事件序/错误码、插件导出与协议义务、fake wire、前端组件与 reducer 不变式——实现前锁定契约）；原则 I（全部引用可溯源） |
| quickstart.md | 原则 VI（大型测试=实际 `guitar run` 执行+全部通过，构建检查不替代）；原则 IV（单测为变更门禁非独立 task） |

**门禁结论**: 通过，无未决项。
