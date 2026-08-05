# Implementation Plan: saolei Team 模板优化

**Branch**: `037-saolei-team-optimize` | **Date**: 2026-08-05 | **Spec**: [`spec.md`](./spec.md)

**Input**: Feature specification from `specs/037-saolei-team-optimize/spec.md`

## Summary

对 031-team-template-mode 已实现的 saolei team 模板进行五项优化与缺陷修复：(1) 修复 planner 游戏历史消息实时不可见的 bug（streamEvents 不产出 createAgent 输入 HumanMessage）；(2) 每 5 局触发 player/planner 通道全量压缩为一条摘要 AIMessage，压缩后 player 停下等待用户输入；(3) planner 系统提示词注入 player 工具描述（静态文本）；(4) desktop 每 agent tab 消息 FIFO 上限；(5) saolei MCP game end 事件增加游戏统计数据（操作次数/正确标记地雷数/每雷平均操作数）。技术方案采用 LangGraph 原生模式（新增压缩节点 + RemoveMessage + 条件路由），复用 031 的 ephemeral buffer / sink / DI 测试基础设施。

## Technical Context

**Language/Version**: TypeScript 5.x（agent）、Svelte 5（desktop frontend）

**Primary Dependencies**: `@langchain/langgraph` ^1.4.8、`langchain`（createAgent）、`@langchain/core`（messages）、`@dominion/game-saolei-board`（CellStatus/GameState/MineCounter/isWin）、`@modelcontextprotocol/sdk`（MCP server）

**Storage**: LangGraph `MemorySaver`（per-session checkpoint，in-process）；`StrategyStore`（mongo 生产 / fake 测试）；ephemeral buffer（per-session，in-process 对象）

**Testing**: vitest（经 Bazel `js_test` / `vitest_test` 宏执行），DI 模式（`vi.fn()` test-double，不使用 `vi.mock`）；大型测试经 testplan skill（`tools/test/guitar`）执行

**Target Platform**: Linux server（agent）、Windows desktop（Wails + Svelte frontend）

**Project Type**: multi-project monorepo（agent = web-service/team-graph，desktop = desktop-app）

**Performance Goals**: 压缩每 5 局触发一次（低频），单次压缩 = 2 个 LLM 调用（player + planner 各一条摘要），不引入性能瓶颈

**Constraints**: 压缩失败 = 直接 abort 连接（FR-013，致命错误不降级）；压缩语义 = 全量替换（非滑动窗口）；统计数据由 MCP 第一手计算（不解析 tool result 文本）

**Scale/Scope**: 单 operator 研究/调试应用；5 个用户故事、34 个 FR、8 个 SC；变更涉及 ~8 个源文件 + ~3 个测试文件

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

### 原则 I — 引用溯源 ✅

所有设计文档（spec/plan/research/data-model/contracts）均使用仓库内相对路径或完整 URL 引用来源。代码变更将引用 spec FR 编号 + 仓库内文件路径。

### 原则 II — 重构式变更 ✅

本特性是对 031 已实现架构的**优化与扩展**，非推翻重来：
- **扩展点**：TeamState 新增 `gameCounter` 字段（自然扩展，不改变既有字段语义）；graph 新增 compress 节点（条件路由插入，不改既有节点行为）；SaoleiEventSink.onGameEnd 新增可选 `stats` 参数（向后兼容）。
- **简化判断**：压缩采用"全量替换"而非复杂的 head+tail/token-budget 方案——在"每 5 局"的领域触发语义下，全量替换最简洁（research.md D1 对比了 opencode/Claude Code/Deep Agents 的复杂方案后确认本方案不需要它们的基础设施）。
- 无过度设计：不引入 token 计数器、不引入 running summary、不引入工具结果修剪——这些在领域触发 + 全量替换语义下不需要。

### 原则 III — 接口优先设计 ✅

接口契约在 Phase 1 设计中已明确（`contracts/compression-contract.md` + `contracts/game-stats-contract.md`）：
- **压缩节点接口**：`CompressNodeDeps`、节点签名、返回值 schema、graph 路由变更。
- **Sink 接口扩展**：`GameStats` 类型、`onGameEnd` 新增可选参数、`GameEventRecord` 扩展。
- **帧发射接口**：`emitFrame` 回调类型、`TeamGraphDeps` 扩展。
- 向后兼容性已分析（所有扩展均为可选参数或新增字段）。

### 原则 IV — 测试颗粒度与执行频率 ✅

编译 + 单测在每个开发 phase 内执行（不单列 task）。大型测试作为功能验收单独分配 task（FR-034）。测试复用 031/036 的 fake-model + fake-tool DI 基础设施。

### 原则 V — 编码前阅读文档 ✅

`tasks.md`（后续 `/speckit.tasks` 生成）将为每个 phase 显式声明需阅读的文档清单（三分类格式：代码规范文档 / 官方文档 / 技术文章）。

### 原则 VI — 服务型应用大型测试验收 ✅

大型测试覆盖关键服务行为（实时可见性、压缩触发、工具描述注入、游戏统计），经 testplan skill 完整执行（部署→测试→清理闭环），所有用例必须全部通过。`quickstart.md` 场景 6 定义了大型测试计划。

### Post-Phase 1 Re-check ✅

Phase 1 设计（data-model.md + contracts/）完成后，重新校验：
- 引用溯源：所有设计引用 031 契约、spec FR、代码路径——✅
- 重构式变更：扩展而非补丁，全量替换简化——✅
- 接口优先：压缩/sink/帧发射接口已定义——✅
- 无 gate 违规。

## Project Structure

### Documentation (this feature)

```text
specs/037-saolei-team-optimize/
├── plan.md                          # This file
├── spec.md                          # Feature spec (/speckit.specify)
├── checklists/
│   └── requirements.md              # Quality checklist
├── research.md                      # Phase 0: research findings
├── data-model.md                    # Phase 1: entity definitions
├── quickstart.md                    # Phase 1: validation guide
├── contracts/
│   ├── compression-contract.md      # Phase 1: compress node + graph routing
│   └── game-stats-contract.md       # Phase 1: sink extension + stats computation
└── tasks.md                         # Phase 2 (/speckit.tasks - NOT yet created)
```

### Source Code (repository root)

```text
projects/game/
├── agent/src/
│   ├── team/
│   │   ├── graph.ts                 # TeamState schema + buildTeamGraph + routing
│   │   ├── state.ts                 # TeamStateValue (add gameCounter)
│   │   ├── player.ts                # player node (no change for core; deps expansion)
│   │   ├── planner.ts               # planner node (gameCounter++, reviewInput frame, tool desc, stats)
│   │   ├── compress.ts              # NEW: compress node
│   │   ├── team-sink.ts             # createTeamSink (onGameEnd stats, GameEventRecord)
│   │   ├── update-strategy.ts       # (no change)
│   │   ├── graph.test.ts            # integration tests (compress, counter, frame)
│   │   └── compress.test.ts         # NEW: compress node unit tests
│   ├── mcp/saolei/
│   │   ├── saolei-mcp.ts            # MCP server (initState, operationCount, computeGameStats, sink.onGameEnd)
│   │   └── saolei-mcp.test.ts       # MCP tests (stats computation)
│   ├── session-team.ts              # SessionTeam (wire emitFrame into TeamGraphDeps)
│   ├── context-middleware.ts        # refreshTeamChannels (add gameCounter reset)
│   ├── model-provider.ts            # (no change; D2 documents session-ID gap)
│   └── server.ts                    # factory wiring (pass template + emitFrame to buildTeamGraph)
├── desktop/frontend/src/
│   ├── App.svelte                   # chatMessages FIFO (trimFifo at all write points)
│   └── components/
│       └── ChatView.svelte          # (no change; renders from chatMessages[selectedAgent])
└── pkg/saolei-board/src/core/
    ├── types.ts                     # (no change; CellStatus/MineCounter already defined)
    ├── counter.ts                   # (no change; decodeMineCounter already exists)
    └── win.ts                       # (no change; isWin already exists)
```

**Structure Decision**: 本特性变更分布在 agent 的 team/ 子目录（核心变更：新增 compress.ts，修改 graph/state/planner/team-sink）、agent 的 mcp/saolei/ 子目录（统计计算）、agent 的 session-team/context-middleware/server（wiring）、desktop frontend（FIFO）。无新增顶层项目或包——所有变更在既有 monorepo 结构内。

## Complexity Tracking

> 无 Constitution Check 违规。本节为空。
