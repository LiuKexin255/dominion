# Implementation Plan: Team Template Mode (StateGraph 升级)

**Branch**: `031-team-template-mode` | **Date**: 2026-07-29 | **Spec**: [`spec.md`](./spec.md)

**Input**: Feature specification from `/specs/031-team-template-mode/spec.md`

## Summary

将 game agent 从"单 session 单 agent"架构升级为"模板（Team 模板）+ LangGraph StateGraph 多 agent team"架构（survey: `survey/agent-team-mode.md`）。核心变更：

1. **API 资源层级重构（clean break）**：引入 Template 顶层路径段（固定枚举，仅 `saolei`），Session/Team/Connect/Message/TeamProfile 全部改挂到 `templates/{template}/...` 下；废弃并移除 `Agent`、`AgentProfile`、`Skill` 资源及其 RPC；`RefreshAgent`→`RefreshTeam`。
2. **saolei 模板 team graph**：StateGraph 含 `player`（独占桌面控制 + saolei MCP）与 `planner`（每局结束触发一次复盘，经 `update_strategy` 写策略）。策略为长期记忆（**持久化到 MongoDB，经当前 mongo 服务**），以 session id 为键；其余 message 为短期记忆（内存 checkpointer），仅由 `RefreshTeam` 清空。
3. **saolei MCP 旁路 sink**：MCP 提供结构化事件 sink 注册接口（不耦合 team mode），模板侧注册 sink 将游戏状态/结束事件写入进程内 state，驱动 planner 触发。
4. **desktop 多标签页 + 模板控制面**：对话区按 team 内 agent 分 tab；agent 增加"是否接受用户输入"属性（saolei 中 planner 屏蔽输入）；profile 页面按模板特化（typed oneof，非 blob）。

**重构方向**（需求方 directive 1）：本次为大规模重构，以**移除旧设计代码、添加新代码**为大方向，不做兼容补丁。

**通用 vs 特化设计原则**（需求方 directive 2）：proto 与 desktop 设计优先通用；模板特化用 **typed `oneof`/枚举**表达，**禁止用 `bytes`/`string`/blob 等"非格式化"方式实现"通用"，禁止为通用引入"潜规则"**（隐式约定）。

## Technical Context

**Language/Version**: Go 1.x（session/proxy/prompt/desktop 后端）；TypeScript（agent，grpc-js）；Svelte（desktop frontend）。

**Primary Dependencies**:
- LangChain 1.x / `@langchain/langgraph` ^1.4.8 / `@langchain/core` ^1.2.3（agent team graph、StateGraph、checkpointer）—— 详见 `survey/agent-team-mode.md` §4.1 与 `pnpm-workspace.yaml` catalog。
- `@modelcontextprotocol/sdk`（saolei MCP server）、`@langchain/mcp-adapters`（MCP client tools）。
- TS mongo 驱动（agent 策略持久化，新增依赖；版本统一于 `pnpm-workspace.yaml` catalog）。
- Go: `go.mongodb.org/mongo-driver`（prompt 服务 TeamProfile 仓储）、`google.golang.org/grpc` + `protoc`（AIP 风格 API）。
- Wails v2（desktop 桌面壳）。

**Storage**:
- **MongoDB**（当前 mongo 实例）：
  - **TeamProfile**（模板特化配置）——由 prompt 服务管理，复用 `projects/game/prompt/runtime/mongo/` 既有 Go 仓储（新增 `team_profiles` 集合）。
  - **Strategy**（长期策略记忆，以 session id 为键）——由 **agent 服务自身**持久化（mongo-backed memory store，修订 #5）；agent 新增 mongo 客户端，直连同一 mongo 实例（新增 `strategies` 集合）。**不经 prompt 服务**。
- **内存 checkpointer**（`MemorySaver`）：短期消息记忆（per-agent channel），`RefreshTeam` 清空。
- **进程内 ephemeral state buffer**（per session）：sink 写入的游戏状态/结束事件，供 team graph 节点读取（非持久、非"记忆"）。

**Testing**: `bazel test //...`（Go 单测 + TS `js_test`）；大型测试经 testplan skill（`tools/test/guitar`，`projects/game/testplan/`），宪法原则 VI 强制。

**Target Platform**: Linux 服务（session/proxy/prompt/agent）+ Windows desktop（Wails）。

**Project Type**: 多服务 web/desktop 应用（gRPC 微服务 + 桌面客户端）。

**Performance Goals**: 无新增硬性性能目标；策略读写为低频（player 每 turn 读一次、planner 每局写一次），agent 直连 mongo 可接受。复用现有 turn 基础设施的单飞/队列语义。

**Constraints**: clean break（不考虑历史数据兼容，需求方确认）；proto 必须通用+typed（无 blob/无潜规则）；LangChain 1.0 `createAgent` 与 swarm/supervisor 包不兼容（survey §3.1⚠️），team graph 用原生 StateGraph + 自定义条件边。

**Spike 验证（已完成）**：LangGraph ^1.4.8 关键 API 假设已由 `experimental/ts/team_graph_spike/`（testplan + fake-llm + vitest）实测**全部确认**（`research.md` D14）：`REMOVE_ALL_MESSAGES` per-channel 清空、createAgent 作外层图节点（不带自身 checkpointer，消息由外层 `MemorySaver` 持有）、单 TeamState+per-agent 通道序列化、middleware 钩子面（`beforeModel` 落地 RefreshTeam）。**实现须用 `Annotation.Root`（非 zod StateSchema）**，并注意 TS2883 导出坑（详见 D14 注意事项 + `contracts/team-graph-contract.md` §1）。

**Scale/Scope**: 单运维者研究型应用（扫雷 agent）；当前固定单模板 `saolei`，但设计须为多模板扩展留位（typed oneof）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md`（v1.3.0）核心原则与开发流程门禁评估本特性：

| 原则/门禁 | 评估 | 结论 |
|---|---|---|
| **I. 引用溯源** | 所有契约/数据模型引用仓库内相对路径（如 `projects/game/game.proto`）与外部 URL（AIP/LangGraph docs）。 | ✅ 设计文档须含来源指针 |
| **II. 重构式变更** | 需求方明确 directive 1"移除旧代码、添加新代码"。本方案移除 Agent/AgentProfile/Skill/RefreshAgent 旧设计，新增 Template/Team/TeamProfile/team graph/sink 新设计，同步进行（非补丁）。 | ✅ 合规（本特性即重构） |
| **III. 接口优先设计** | Phase 1 先定 proto API 契约（`contracts/api-contract.md`）、team graph 契约、sink 契约、strategy store 契约，再实现。 | ✅ 合规 |
| **IV. 测试颗粒度** | 编译+单测属各代码变更 task 内（不单列）；大型测试单列验收 task。 | ✅ 合规 |
| **V. 编码前阅读文档** | `tasks.md` 须为每 phase 声明三分类文档清单（代码规范/官方文档/技术文章），含间接引用。 | ✅ 由 `/speckit.tasks` 落实 |
| **VI. 大型测试验收** | 服务型应用，FR-030 强制大型测试，须经 testplan skill 完整执行（部署→测试→清理），全部用例通过。 | ✅ 合规（见 `quickstart.md`） |

**门禁执行顺序**：文档阅读 → 实现（重构式+接口优先）→ 编译+单测 → 引用 → 大型测试验收。

**无违规需豁免**（Complexity Tracking 留空）。

## Project Structure

### Documentation (this feature)

```text
specs/031-team-template-mode/
├── plan.md                      # 本文件
├── research.md                  # Phase 0：设计决策与调研
├── data-model.md                # Phase 1：实体与状态模型
├── quickstart.md                # Phase 1：端到端验证指南
├── contracts/                   # Phase 1：接口契约
│   ├── api-contract.md          # proto 资源层级 + 服务 + RPC
│   ├── team-graph-contract.md   # saolei team StateGraph（节点/边/状态/策略流）
│   ├── saolei-sink-contract.md  # saolei MCP 旁路事件 sink 接口
│   ├── strategy-store-contract.md # 策略长期记忆持久化（mongo）+ agent 访问
│   └── desktop-contract.md      # desktop 模板控制面 + 多标签页 + profile 特化
└── tasks.md                     # Phase 2（/speckit.tasks 生成，非本命令）
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                         # 【重写】资源层级、服务、消息（见 contracts/api-contract.md）
├── pkg/gameconst/const.go             # 【重写】资源名解析（templates/{template}/...）
├── session/                           # 【改】SessionService：session 挂到 templates/{template}/下
├── proxy/                             # 【改】ProxyService→Team 视图：GetTeam/Connect/ListMessages/RefreshTeam
├── prompt/                            # 【改】PromptService：管理 TeamProfile（oneof）；移除 AgentProfile/Skill（不涉 Strategy）
│   ├── domain/model.go                #   TeamProfile 领域模型
│   └── runtime/mongo/repository.go    #   mongo 仓储（team_profiles 集合）
├── agent/                             # 【重写核心】单 agent → saolei team graph
│   ├── src/
│   │   ├── server.ts                  #   进程级 wiring（team graph + StrategyStore + mongo 客户端）
│   │   ├── team/                      #   【新增】saolei 模板：TeamGraph builder、TeamState、player（createAgent 全 loop）/planner 节点、update_strategy、teamSink
│   │   ├── strategy-store.ts          #   【新增】StrategyStore 接口 + MongoStrategyStore（agent 直连 mongo 持久化策略）
│   │   ├── session-team.ts            #   【新增】SessionTeam（取代 SessionAgent，持有 team graph + 状态 buffer）
│   │   ├── mcp/saolei/saolei-mcp.ts   #   【改】createSaoleiMcpServer 增 sink? 参数；recognize 后调 sink；修过时注释
│   │   ├── mcp-host.ts                #   【改】lookup 传递 sink/state buffer；修过时注释
│   │   ├── context-middleware.ts      #   【改】REMOVE_ALL_MESSAGES 清空（FR-018 RefreshTeam）
│   │   ├── prompt-client.ts           #   【改】TeamProfile RPC client
│   │   └── handler.ts                 #   【改】Connect/GetTeam/ListMessages/RefreshTeam（agent 端）
│   └── (移除 llm.ts 单 agent AdapterFactory 路径 → team graph)
├── desktop/
│   ├── app.go                         # 【改】Wails 绑定：CreateSession(template)/Connect/RefreshTeam/SendUserTurn(agent)/ListMessages(agent)/TeamProfile CRUD
│   └── frontend/src/
│       ├── api.ts                     # 【改】类型 + 绑定（Template 枚举、Team/TeamAgent、agent 字段）
│       ├── App.svelte                 # 【改】模板控制面 + 多 tab 路由
│       └── components/                # 【改/新增】ChatView 多 tab、ProfileManagement 特化、AgentSidebar→TeamSidebar
└── testplan/                          # 【新增/改】saolei team 端到端大型测试计划
```

**Structure Decision**: 复用现有多服务拓扑（session/proxy/prompt/agent/desktop），不新增服务进程。agent 内新增 `src/team/` 目录承载 saolei 模板（TeamGraph/节点/sink），以"模板"为组织边界为未来多模板扩展留位。**职责分离**：prompt 服务管理 TeamProfile（静态配置，复用既有 Go mongo 仓储）；agent 服务自身持久化 Strategy（运行时记忆，新增 TS mongo 客户端，直连同一 mongo 实例）——strategy 不经 prompt 服务（修订 #5）。

## Complexity Tracking

> 无 Constitution Check 违规需豁免，本表留空。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
