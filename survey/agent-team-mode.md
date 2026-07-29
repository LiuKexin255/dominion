# 调研：Agent Team Mode（多 Agent 数据同步/交互机制）

> **状态**：调研完成，待立项进入方案设计
> **日期**：2026-07-29
> **目标服务**：`projects/game/agent/`
> **范围**：为 game agent 增加 team mode（player + planner 协作），聚焦 LangGraph StateGraph 进程内的数据同步/交互
> **说明**：本文为调研材料，供后续制定方案（spec / plan）时使用；非 SDD spec 文档。

---

## 1. 背景与目标

当前 game agent 是**单 session、单 agent** 架构。需求是引入 **team mode**：多个 agent 协作完成扫雷游戏——
- **player**：主控 agent，独占桌面控制（鼠标/键盘），执行游戏操作。
- **planner**：侧路规划 agent，**不参与控制**，仅基于游戏状态产出/更新策略，单向推送给 player。

核心问题是：**player 与 planner 之间如何同步与交互数据**。

经多轮澄清，需求收敛为：
1. 信息共享范围聚焦在 **LangGraph StateGraph 进程内**（不跨服务、不用 A2A）。
2. 不需要多个 agent 控制桌面（player 独占）。
3. "策略"作为 **memory 单独存储管理**，可为 team 实例设置 memory id；其余历史 in-cache，可按需清空（仅保留策略）。
4. 游戏状态/行动由 saolei MCP 承接，其同步在**代码层**完成，LLM 不参与。
5. saolei MCP **不与 team mode 耦合**，仅提供可配置的 sink/hook 接口。
6. planner 每局游戏调用一次，**由结构化 hook 信号触发**（不靠字符串匹配 tool 输出）。

---

## 2. 当前架构现状（代码分析）

### 2.1 核心组件

| 组件 | 文件 | 职责 |
|---|---|---|
| `SessionAgentStore` | `projects/game/agent/src/session-agent.ts` | `Map<sessionId, SessionAgent>`，session 间**完全隔离**，无跨 session 通信 |
| `SessionAgent` | 同上 | 每 session 拥有一个 `AgentAdapter`，管理 profile 绑定/刷新；含 `bindLock` 串行化 |
| `AgentAdapter` / `AgentAdapterImpl` | `projects/game/agent/src/llm.ts` | 包装 LangChain `createAgent`（from `langchain` v1.0）；`generateTurn` 流式产出 ContentBlock |
| `OperationBridge` | `projects/game/agent/src/operation-bridge.ts` | session 级桥接：LangChain 工具 ↔ desktop bidi stream；`dispatch(part)` 发操作、`handleResult` 收结果 |
| `Handler` | `projects/game/agent/src/handler.ts` | gRPC `Connect` bidi streaming；每 session 一个 mutex（FIFO 非重入），turn 串行 |
| `MemorySaver` | `@langchain/langgraph` | in-memory checkpointer，`thread_id = sessionId` |

### 2.2 关键现状：gameState 已是 MCP 工具的代码层副产品

`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:596` 维护会话级状态：

```typescript
let recognized: GameState | null = null;  // createSaoleiMcpServer 闭包变量
```

每次 player 调 saolei 工具（click/flag/init），handler 执行 `bridge.dispatch` → desktop 返回 screenshot → `recognize()` 用 `@dominion/game-saolei-board` 识别 → **自动更新 `recognized`**。**player LLM 从不"记录" gameState——它已是工具执行的副作用。** 这正是需求第 4 点（代码层同步、LLM 不参与）的天然基础。

> ⚠️ **代码债务**（team mode 改动时应一并修正）：`projects/game/agent/src/mcp-host.ts:9` 注释仍写 *"the server carries no per-session game state"*（spec 023 旧说法），但 spec 025 后 `recognized` 状态已存在。注释过时。

**gameStatus 第一手计算**：`saolei-mcp.ts:253` 的 `gameStatus(state): "won"|"lost"|"playing"` 已在工具 handler 内计算——这是后续 hook 信号触发的稳定来源（非文本解析）。

### 2.3 预留的扩展点

`projects/game/agent/src/context-middleware.ts` 当前是 identity pass-through（`beforeModel` 原样返回 messages），其注释明确：

> *"In future releases, this middleware will be the interception point for context management, summarization, and message pruning."*

team mode 的"清空非策略历史"正好落地于此——符合宪法原则 II（重构式扩展，不堆叠）。

### 2.4 依赖版本（`pnpm-workspace.yaml` catalog）

```
langchain:                "^1.5.4"
@langchain/core:          "^1.2.3"
@langchain/langgraph:     "^1.4.8"
@langchain/anthropic:     "^1.5.2"
@langchain/openai:        "^1.5.5"
@langchain/mcp-adapters:  "^1.1.3"
```

agent 用的是 **LangChain 1.0 的 `createAgent`**（`import { createAgent } from "langchain"`），不是旧的 `createReactAgent`。

---

## 3. 多 Agent 框架数据同步/交互机制全景

按"同步发生的范围"分两层。核心判断依据（[Redis blog](https://redis.io/blog/mcp-vs-a2a-which-protocol-do-you-need/)）：**问题不是你有几个 agent，而是它们是否跨越你不拥有的边界。**

### 3.1 第一层：进程内（LangGraph StateGraph）—— 本方案所在层

LangGraph 提供 3 类机制，全部基于**共享 graph state（typed state + reducer）**：

#### (1) 共享状态通道（Shared State Channels）— 数据同步的根基
- StateGraph 持有 typed state（JS 用 `StateSchema` + zod / `ReducedValue` / `MessagesValue`）。
- 每个 agent 作为节点：读 state → 执行 → 返回 partial update；state 的每个 key 配 **reducer** 定义合并语义（`messagesStateReducer` 追加、`operator.add` 列表合并、自定义 reducer）。
- **可见性策略**：shared scratchpad（共享 `messages`）vs private scratchpad（独立 state key + 边界转换）。

#### (2) 控制权转移（Handoff）— 串行协作
- 特殊 tool 返回 `Command({ goto, update, graph: Command.PARENT })`，把当前 agent 消息追加到父 state 并路由到目标 agent。
- 拓扑：supervisor（中心化）/ swarm-network（去中心化）。

#### (3) 并行执行（Parallel / Map-Reduce）
- 静态并行：一个节点多 outgoing edges → 同一 superstep 并行。
- 动态并行：条件 edge 返回 `[Send("node", state), ...]`，运行时决定任务数；reducer 合并并发写；汇聚节点自动 barrier。

#### ⚠️ 兼容性约束（对本项目至关重要）
- `@langchain/langgraph-swarm` / `@langchain/langgraph-supervisor` JS 包**仅支持旧 `createReactAgent`，未与 LangChain 1.0 的 `createAgent` 兼容**（[langchain-ai/langgraphjs#1739](https://github.com/langchain-ai/langgraphjs/issues/1739)）。
- 官方推荐：用 **"single agent with middleware"** 或**手写 StateGraph + 自定义 handoff tool**，绕开这两个包。有第三方称其"已不再积极维护"。
- **结论**：team mode 应采用原生 graph 能力，不依赖 swarm/supervisor 包。

### 3.2 第二层：跨进程/跨服务（本方案不涉及，仅备查）

| 机制 | 用途 | 适用场景 |
|---|---|---|
| **LangGraph RemoteGraph** | 远程 deployment 作为本地 subgraph 节点 | 多 LangGraph 部署组合；⚠️ 禁止同 deployment 自调（死锁） |
| **A2A (Agent2Agent) Protocol** | agent 间标准化协作（Agent Card + JSON-RPC + Task 生命周期） | 跨不拥有的边界（不同团队/框架/供应商）；2026 v1.0，Linux Foundation |
| **MCP**（本项目已用于 saolei） | agent → tool/data | agent 接工具，非 agent 间协作 |

**核心洞察**：同代码库、同团队、共享权限的白盒 agent，用框架内编排（LangGraph）即可，无需 A2A。本方案 player/planner 同属本项目白盒 agent → 进程内 StateGraph 足够。

### 3.3 框架宏观对比（佐证选型：LangGraph 正确）

| 维度 | LangGraph | CrewAI | AutoGen / MS Agent Framework 1.0 |
|---|---|---|---|
| 协调模型 | 显式有向图 + typed state | 角色团队 + 任务流 | 会话式消息传递 |
| 原生 checkpointing | ✅ Postgres/Redis/SQLite | ⚠️ 2026.04 才加 | ⚠️ 内存为主 |
| Token 开销 | **9%**（最低） | 18% | 31% |
| 并行 | `Send`/多边 superstep | hierarchical process | MagenticOneGroupChat |
| 可观测性 | LangSmith 原生（最佳） | 弱 | Azure Monitor |

来源：[agent-harness.ai benchmark](https://agent-harness.ai/blog/multi-agent-orchestration-frameworks-benchmark-crewai-vs-langgraph-vs-autogen-performance-cost-and-integration-complexity/)、[turion.ai](https://turion.ai/blog/langgraph-vs-crewai-vs-autogen-comparison-2026/)。本项目已选 LangGraph.js 且为正确的生产级选择。

---

## 4. LangChain / LangGraph 版本与 StateGraph 基础

### 4.1 版本演进（1.0 之后有更新，向后兼容）

LangChain 1.0 于 2025-10-22 GA，承诺**到 2.0 前无 breaking change**。此后按 semver 发布 1.x minor：

| 版本 | 时间 | 关键特性 |
|---|---|---|
| 1.0.0 | 2025-10-22 | `create_agent`、middleware 系统、standard content blocks |
| 1.1.0 | 2026-03（LangGraph） | type-safe streaming/invoke `version="v2"`、`GraphOutput` |
| 1.2.0 | 2026-05-11（LangGraph） | **DeltaChannel (beta)**、per-node timeouts、node-level error handlers、graceful shutdown、streaming v3 |
| 1.2.x | 至 2026-07-28 | 最新 `langgraph==1.2.9`（Python） |

**注意**：
- JS（`@langchain/langgraph`）与 Python（`langgraph`）**版本号独立**，不能跨语言直接比较。本项目 JS 已到 1.4.8。
- 部分 1.2 特性（per-node timeouts、error handlers）**Python-only**，JS 没有。
- DeltaChannel（减少长 thread checkpoint 开销）仍 beta，非本方案必需。

来源：[LangChain changelog](https://docs.langchain.com/oss/python/releases/changelog)、[byteiota 1.2 解读](https://byteiota.com/langgraph-1-2-per-node-timeouts-deltachannel-and-streaming-v3/)、[GitHub releases](https://github.com/langchain-ai/langgraph/releases)。

### 4.2 StateGraph 详解（JS/TS API）

StateGraph 是 LangGraph 核心抽象，`createAgent` 内部即编译成一个 StateGraph（ReactAgent）。五大组成：

- **State**：`StateSchema` 定义 typed state，每个 key 是一个 channel。普通 channel 默认 reducer="覆盖"；`ReducedValue` 自定义合并；`MessagesValue` 内置消息去重追加。
- **Node**：`addNode(name, fn)`，`fn(state) => partial update`，`GraphNode<typeof State>` 类型安全。
- **Edge**：`addEdge(from, to)`（静态；多 outgoing edge → 同 superstep 并行）；`addConditionalEdges(from, routerFn)`（动态路由）。
- **多 schema**：input/output/overall/private，控制可见性（多 agent 隔离基础）。
- **compile**：`.compile({ checkpointer, store })`；checkpointer 做 thread 级持久化，store 做跨 thread 长期记忆。
- **执行模型（Pregel superstep）**：离散超步；同 superstep 内节点并行，reducer 合并并发写；汇聚节点自动 barrier。

来源：[LangGraph.js graph-api](https://docs.langchain.com/oss/javascript/langgraph/graph-api)、[use-graph-api](https://docs.langchain.com/oss/javascript/langgraph/use-graph-api)。

---

## 5. player + planner 模式收敛过程（决策记录）

### 5.1 路径选择

调研了 3 种落地路径，最终选 **路径 A**：

| 路径 | 模式 | 是否真并行 | Coding 量 | 适配性 |
|---|---|---|---|---|
| **A（选定）** | 单图条件触发，串行协作 | ❌ 串行（player 主控，简单可控） | 中 | ✅✅ 进程内，复用现有栈 |
| B | 单图并行侧路（superstep） | ✅ 但 planner 每步跑 | 中高 | ✅ 但语义复杂 |
| C | 异步后台 planner（Deep Agents async subagents） | ✅ 真异步 | 低+中 | ⚠️ 依赖 Agent Protocol server，当前自建 gRPC 无此设施 |

**选 A 的理由**：完全进程内、复用现有 `MemorySaver`/`OperationBridge`/gRPC、能精确实现"player 主控 + planner 按需触发 + 共享 state 同步"。"独立运行"体现为独立节点 + 独立 profile + 独立 LLM 调用（非独立进程）。

> 备选 C（Deep Agents async subagents，[docs](https://docs.langchain.com/oss/python/deepagents/async-subagents)、[blog](https://www.langchain.com/blog/deep-agents-v0-5)）是"任务委派"模型（start/check/update/cancel），非"持续观察自动同步"，且依赖 LangSmith Deployment / 自托管 Agent Protocol server，对本项目过重。记录备查。

### 5.2 关键决策点收敛

| 决策点 | 最终选择 | 依据 |
|---|---|---|
| 信息共享范围 | StateGraph 进程内 | 同团队白盒 agent，无需 A2A |
| 控制权 | player 独占 | planner 不操作桌面 |
| 策略存储 | **Store**（长期 memory，namespace = teamMemoryId） | 独立于 checkpointer，可跨 thread 复用、独立管理/清空 |
| gameState 同步 | saolei MCP **可配置 sink 接口** → 写 Store | MCP 不耦合 team mode；LLM 不参与 |
| planner 触发 | **每局一次，结构化 hook 信号触发** | 见 5.3 |
| 历史管理 | messages in-cache（MemorySaver），可清空；strategy 在 Store 不受影响 | `REMOVE_ALL_MESSAGES` 原语 |

### 5.3 planner 触发机制（重要调整：hook 信号，非字符串匹配）

**初版设想**（已废弃）：graph 条件边解析 player 最近 tool result 的 `game status: won|lost` 文本行。
**问题**：字符串匹配不稳定（依赖文本格式，易因 prompt/输出变动失效）。

**最终方案**：结构化 hook 信号触发。`saolei-mcp.ts` 的 `gameStatus(state)` 函数（:253）已在 handler 内第一手计算出 `"won"|"lost"|"playing"` 枚举。通过 sink 把这个**结构化信号**输出，team mode 据此触发 planner——完全不解析文本。

数据流见第 6 节。

---

## 6. 最终方案草图

### 6.1 架构总览

```
TeamGraph (父 StateGraph, 单 thread = teamMemoryId)
  state: { messages (MessagesValue, 可清空), gameEnded: "won"|"lost"|null, ... }

  START → [player] ──条件边(读 state.gameEnded)──→ [planner] ──→ [player] ...
                    │  gameEnded=null → 回 player    │ gameEnded≠null 触发
                    │                                │ 每局结束复盘
  player 入口:       │                                planner 入口:
   代码层从 Store读   │                                 代码层从 Store读
   strategy 注入ctx  │                                 gameState(sink写入)
                    │                                调 update_strategy 工具
                    │                                → 写 Store strategy
                    ↓                                ↓
          ┌──────────── InMemoryStore (进程级共享) ────────────┐
          │ ["team", teamMemoryId, "strategy"]   ← planner 写  │
          │ ["team", teamMemoryId, "gameState"]  ← sink 写     │
          │ ["team", teamMemoryId, "gameEvent"]  ← sink 写(结束信号)│
          └────────────────────────────────────────────────────┘
                    ↑
                    │ saoleiEventSink (team mode 注册; 通用接口, MCP 不知 team mode)
          [saolei MCP server]  ← createSaoleiMcpServer(bridge, boardApi, sink)
                    ↑ loopback HTTP (player 的 saolei_* 工具调用)

  context-middleware.ts: 需要时发 REMOVE_ALL_MESSAGES 清空 messages
                         （strategy 在 Store, 不受影响）
```

### 6.2 hook 信号触发的精确数据流

1. player LLM 调 saolei 工具（如 `saolei_click`）。
2. `saolei-mcp.ts` handler 执行：`bridge.dispatch` → `recognize()` 更新 `recognized` → 计算 `gameStatus(state)`。
3. **当 status 变为 `won`/`lost`，handler 调 `sink.onGameEnd(state, status)`** —— 结构化信号（status 枚举，第一手，非文本解析）。
4. team mode 注册的 sink 实现：写 Store `["team", id, "gameEvent"] = { status, state, endedAt }`，并更新 `["team", id, "gameState"]`。
5. **player 节点 wrapper**（player 工具调用返回后执行）：从 Store 读最新 `gameEvent` → 若有未处理结束事件，写入 `TeamState.gameEnded = status`。
6. **条件边**读 `state.gameEnded`：非 null → 路由 `planner`；null → 路由 `player`。
7. **planner 节点**：读 Store gameState → LLM 复盘 → 调 `update_strategy` 写 Store strategy → 清除 `gameEvent`/`gameEnded` 标志 → 回 player。

关键：步骤 3 的信号来自 MCP 内部已计算好的 `gameStatus()`，通过 sink 结构化输出，**不依赖 tool result 文本格式**，稳定可靠。

---

## 7. 技术可行性论证

### 7.1 Store 作为 team memory（namespace 隔离）✅

LangGraph Store（与 checkpointer 不同）专为跨 thread 长期记忆设计。JS API（context7 确认）：

```typescript
import { InMemoryStore } from "@langchain/langgraph";
const store = new InMemoryStore();
const graph = builder.compile({ store });

// 节点内通过 runtime.store 访问
const node: GraphNode<typeof State> = async (state, runtime) => {
  const ns = ["team", runtime.context.teamMemoryId, "strategy"];
  await runtime.store.put(ns, "current", { strategy: "..." });
  const memo = await runtime.store.get(ns, "current");
};
// invoke 时: { configurable: { thread_id }, context: { teamMemoryId } }
```

**namespace 即 team memory id**：`["team", teamMemoryId, "strategy"]`，不同 team 隔离。`InMemoryStore` 是普通对象实例，可进程级创建，同时交给 `compile({ store })` 和注入 MCP host 的 lookup（`OperationBridge` 注入模式即现成先例）。MCP、player、planner 共享同一 store 实例。精确读取用 `get(ns, key)`（无需 embedding）。

来源：[LangGraph.js add-memory](https://docs.langchain.com/oss/javascript/langgraph/add-memory)、[stores](https://docs.langchain.com/oss/javascript/langgraph/stores)。

### 7.2 清空非策略历史、保留策略 ✅（原生支持）

LangGraph `messagesStateReducer`（`MessagesValue` 内部）内置全量清空原语（[langgraphjs 源码](https://github.com/langchain-ai/langgraphjs/blob/main/libs/langgraph-core/src/graph/messages_reducer.ts)）：

```typescript
export const REMOVE_ALL_MESSAGES = "__remove_all__";
// reducer 遇到 RemoveMessage({ id: REMOVE_ALL_MESSAGES }) 时，
// 丢弃所有 prior messages，只保留它后面的消息
```

清空用法（[short-term-memory 文档](https://docs.langchain.com/oss/javascript/langchain/short-term-memory)）：

```typescript
import { RemoveMessage } from "@langchain/core/messages";
import { REMOVE_ALL_MESSAGES } from "@langchain/langgraph";
return { messages: [new RemoveMessage({ id: REMOVE_ALL_MESSAGES })] };
```

**与策略解耦的根本原因**（[forum.langchain.com 最佳实践](https://forum.langchain.com/t/langgraph-postgresql-chat-history-and-summarization-best-practice/3521)）：**checkpointer 是 LLM 上下文管理工具，不是消息存储；Store 是独立长期记忆层，不受 checkpointer 压缩/清空影响。**

| 层 | 存什么 | 清空历史时 |
|---|---|---|
| Checkpointer（MemorySaver） | messages | ✅ 被清空（避免干扰） |
| Store（namespace 隔离） | strategy | ❌ 不受影响（保留复用） |

落地于 `context-middleware.ts`（已预留）。

> ⚠️ 官方 `summarizationMiddleware` 有已知 bug（[deepagents#2876](https://github.com/langchain-ai/deepagents/issues/2876)）：只压缩模型可见消息，不清空 checkpoint state，致 checkpoint 无限增长。本方案诉求是"清空"非"摘要"，用自定义 `REMOVE_ALL_MESSAGES` 更贴合；`MemorySaver` 是 in-memory（重启即清），growth 问题可控。

### 7.3 saoleiEventSink 解耦接口 ✅

MCP 只定义"事件形状"，不知道 store/teamMemoryId/strategy。默认无 sink（行为零变化，向后兼容）：

```typescript
// saolei-mcp.ts —— 通用事件 sink 接口（不依赖 team mode 概念）
export interface SaoleiEventSink {
  onGameStart(state: GameState): void | Promise<void>;
  onGameEnd(state: GameState, status: "won" | "lost"): void | Promise<void>;
  onMove(tool: CellTool, x: number, y: number, state: GameState): void | Promise<void>;
}

export function createSaoleiMcpServer(
  bridge: OperationBridge,
  boardApi: SaoleiBoardApi = createDefaultBoardApi(),
  sink?: SaoleiEventSink,   // 可选；默认 undefined（零行为变化）
): McpServer { /* recognize() 后、gameStatus 变化时调 sink?.onXxx() */ }
```

team mode 注册（消费者）：

```typescript
const teamSink: SaoleiEventSink = {
  onMove: async (_t, _x, _y, state) =>
    store.put(["team", teamMemoryId, "gameState"], "current", { state }),
  onGameEnd: async (state, status) =>
    store.put(["team", teamMemoryId, "gameEvent"], "last", { state, status, endedAt: Date.now() }),
};
createSaoleiMcpServer(bridge, undefined, teamSink);
```

`onGameEnd` 承载结构化 `status` 枚举——这就是 hook 信号触发的基础。

### 7.4 hook 信号触发（结构化，非字符串匹配）✅

如 6.2 所述，触发信号来自 `saolei-mcp.ts` 内部第一手的 `gameStatus(state)` 计算，经 sink `onGameEnd(state, status)` 结构化输出，team mode 写入 Store + TeamState.gameEnded，条件边读结构化字段路由。**全程不解析 tool result 文本**，稳定可靠。

---

## 8. 待澄清/待决策项（进入方案设计前需拍板）

1. **teamMemoryId 来源**：复用 session id，还是在 profile/配置里独立声明 team 标识？
   - 影响：是否支持"策略跨 session 复用"或"同 session 切换 team"。

2. **player 读 strategy 的注入形式**：
   - (a) 追加到 system prompt（策略作为"长期指令"）
   - (b) 作为单独 system/human message 前置（策略作为"当前态势"）
   - （排除"给 player 一个 read_strategy 工具"——与"代码层、LLM 不参与同步"相悖）

3. **planner 模型与 profile 结构**：planner 用与 player 相同的 `AgentProfile`，还是扩展支持"一个 team 配置含多 profile（player profile + planner profile）"？
   - 影响：proto `AgentProfile` / `PromptClient` / `ProfileData` 是否要扩展。

4. **planner 的工具集**：planner 至少需要 `update_strategy`（写 Store）。是否还需要只读工具（如读历史策略、读 gameState）？planner 是否复用 saolei 工具（理论上不需要，因 planner 不控制）？

5. **gameEnded 标志的生命周期**：planner 处理后由谁清除（planner wrapper 清 Store gameEvent + 写 TeamState.gameEnded=null）？需明确避免重复触发。

6. **清空历史的时机/策略**：何时触发清空（每局开始？消息数阈值？手动）？清空时是否保留最近 N 条或 system 消息？

---

## 9. 对现有代码的影响预估（供方案设计参考）

| 文件 | 预期变更类型 |
|---|---|
| `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` | 扩展：`createSaoleiMcpServer` 增 `sink?` 参数；recognize 后调 sink；修正过时注释 |
| `projects/game/agent/src/mcp-host.ts` | 扩展：`SessionBridgeLookup` 增 store/sink 传递；修正过时注释 |
| `projects/game/agent/src/llm.ts` | 扩展：支持 team graph 构建（player+planner 节点）；`AdapterFactory` 可能扩展 |
| `projects/game/agent/src/context-middleware.ts` | 实现：`REMOVE_ALL_MESSAGES` 清空逻辑（落地预留扩展点） |
| `projects/game/agent/src/session-agent.ts` | 扩展：team 模式下的 SessionAgent（持有 team graph + store namespace） |
| `projects/game/agent/src/handler.ts` | 适配：team mode 的 turn 编排（可能） |
| `projects/game/agent/src/server.ts` | 进程级 `InMemoryStore` 创建与注入 |
| `projects/game/game.proto` | 可能：`AgentProfile` 扩展（若决策项 3 选多 profile） |
| 新增 `src/team/` 目录 | TeamGraph builder、TeamState schema、planner 节点、update_strategy 工具、teamSink |

> 以上为调研阶段的预估，具体以方案设计（spec/plan）为准。

---

## 10. 参考资料

### 进程内（LangGraph StateGraph）
- LangGraph handoffs（TS）: https://docs.langchain.com/oss/javascript/langchain/multi-agent/handoffs
- multi-agent concepts（JS）: https://github.com/langchain-ai/langgraphjs/blob/main/docs/docs/concepts/multi_agent.md
- Send API / map-reduce: https://docs.langchain.com/oss/python/langgraph/use-graph-api
- graph-api（JS）: https://docs.langchain.com/oss/javascript/langgraph/graph-api
- swarm/supervisor 与 createAgent 不兼容: https://github.com/langchain-ai/langgraphjs/issues/1739
- Scaling LangGraph（并行/subgraph）: https://aipractitioner.substack.com/p/scaling-langgraph-agents-parallelization

### 记忆与状态管理
- LangGraph.js add-memory（store）: https://docs.langchain.com/oss/javascript/langgraph/add-memory
- LangGraph.js stores: https://docs.langchain.com/oss/javascript/langgraph/stores
- short-term memory（trim/delete/summarize）: https://docs.langchain.com/oss/javascript/langchain/short-term-memory
- prebuilt middleware（summarization/contextEditing）: https://docs.langchain.com/oss/javascript/langchain/middleware/built-in
- messagesStateReducer 源码（REMOVE_ALL_MESSAGES）: https://github.com/langchain-ai/langgraphjs/blob/main/libs/langgraph-core/src/graph/messages_reducer.ts
- checkpointer vs Store 最佳实践: https://forum.langchain.com/t/langgraph-postgresql-chat-history-and-summarization-best-practice/3521
- summarizationMiddleware checkpoint 增长 bug: https://github.com/langchain-ai/deepagents/issues/2876

### 版本与框架对比
- LangChain changelog: https://docs.langchain.com/oss/python/releases/changelog
- LangGraph 1.2 解读: https://byteiota.com/langgraph-1-2-per-node-timeouts-deltachannel-and-streaming-v3/
- LangGraph releases: https://github.com/langchain-ai/langgraph/releases
- 框架 benchmark: https://agent-harness.ai/blog/multi-agent-orchestration-frameworks-benchmark-crewai-vs-langgraph-vs-autogen-performance-cost-and-integration-complexity/
- 框架对比: https://turion.ai/blog/langgraph-vs-crewai-vs-autogen-comparison-2026/

### 跨服务协议（备查，本方案不涉及）
- A2A 官网: https://a2a-protocol.org/latest/
- MCP vs A2A（Redis）: https://redis.io/blog/mcp-vs-a2a-which-protocol-do-you-need/
- RemoteGraph: https://docs.langchain.com/langsmith/use-remote-graph

### 异步 subagent（备选路径 C，备查）
- Deep Agents async subagents: https://docs.langchain.com/oss/python/deepagents/async-subagents
- Deep Agents v0.5 blog: https://www.langchain.com/blog/deep-agents-v0-5
- 自托管 Agent Protocol server 示例: https://github.com/langchain-ai/deepagents/blob/main/examples/async-subagent-server/server.py

### 仓库内引用
- `projects/game/agent/src/session-agent.ts`
- `projects/game/agent/src/llm.ts`
- `projects/game/agent/src/operation-bridge.ts`
- `projects/game/agent/src/handler.ts`
- `projects/game/agent/src/context-middleware.ts`
- `projects/game/agent/src/mcp-host.ts`
- `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`
- `projects/game/game.proto`
- `pnpm-workspace.yaml`
