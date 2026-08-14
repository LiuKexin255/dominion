# Implementation Plan: planner 记忆校准实现修复

**Branch**: `042-planner-memory-fixup` | **Date**: 2026-08-10 | **Spec**: [`spec.md`](./spec.md)

**Input**: Feature specification from `/specs/042-planner-memory-fixup/spec.md`

## Summary

对 [`specs/039-planner-memory-calibration/`](../039-planner-memory-calibration/) 已落地实现的三个缺陷修复：

1. **saolei_operate 停止行格式**：`operateResultText` 的停止行从 `stopped at op K (reason)`（序号）改为 `stopped at {type}({x},{y}) (reason)`（具体操作参数），与 gameLog 中 `saolei_operate(click(4,4), flag(5,5))` 的渲染格式一致。影响面含 SKILL.md、单测、大型测试断言（均为格式字符串同步更新）。
2. **review 指令实时显示帧**：planner.ts review 节点在将 `instruct_player` 暂存指令写入 `playerMessages` 时，缺少实时显示帧发射（instruction-node.ts 的 init/compact 场景有，review 场景没有）。修复为复用 instruction-node.ts 同一 `emitChannelFrame` 模式——创建 HumanMessage → `ensureMessageId` → 发射 `PRIMARY_AGENT_NAME` + `MESSAGE_ROLE_USER` 帧（需将 instruction-node.ts 的 `ensureMessageId` 从模块私有改为 export 供 planner.ts 导入，函数实现不变）。
3. **RefreshTeam 后初始指令产出**：`refreshTeam()` 清空通道后触发一次无游戏历史初始指令（与 team 初始化的 init instruction 行为一致），复用 `runInitTurn()` 图执行逻辑。提取公共 `startInstructionTurn()` 方法（去除 `triggerInitInstruction` 的一次性守卫，使 refresh 可重复触发）。

**重构方向**（宪法原则 II）：三处修复均与既有架构对齐——issue 1 使停止行格式与 gameLog 渲染格式一致（而非新增第二种格式）；issue 2 使 review 场景与 init/compact 场景的帧发射行为一致（而非新增第三种帧机制）；issue 3 使 RefreshTeam 与 team init 的指令触发行为一致（提取公共方法而非复制逻辑）。

## Technical Context

**Language/Version**: TypeScript（agent），既有代码无新语言。

**Primary Dependencies**: `@langchain/langgraph` ^1.4.8 / `@langchain/core` ^1.2.3（team graph 节点/通道/configurable/`messagesStateReducer`）；`@modelcontextprotocol/sdk`（saolei mcp 工具）。版本统一于 `pnpm-workspace.yaml` catalog。无新依赖。

**Storage**: 既有 MongoDB（memory 服务，独立 db `game_memory`）+ 内存 checkpointer（`MemorySaver`）+ 进程内 ephemeral buffer——均不变。

**Testing**: `bazel test //...`（TS `js_test`）；大型测试经 testplan skill（`tools/test/guitar`，`projects/game/testplan/`），宪法原则 VI 强制。

**Target Platform**: Linux 服务（既有 agent/gateway/desktop 拓扑不变）。

**Project Type**: 多服务 web 应用（gRPC 微服务），本特性仅涉及 agent TS 代码 + 大型测试。

**Performance Goals**: 无新增（三处修复不引入新 I/O 或计算路径）。

**Constraints**: 
- 不改变 039 核心架构与契约（memory 服务、冻结快照、两场景节点、saolei_operate 双形态）。
- review 指令帧发射复用既有 `emitChannelFrame` / `ChannelFrameEmitter` 机制（041 已验证）。
- RefreshTeam 指令产出复用既有 `runInitTurn` 图执行逻辑与 `initInstruction` 节点。
- 停止行格式变更须同步 SKILL.md（player skill）、单测断言、大型测试断言、fake-LLM fixture（格式字符串一致性，宪法原则 II 架构/工具面变更同步）。

**Scale/Scope**: 单运维者研究型应用；改动面为 4 个 TS 文件（saolei-mcp.ts、planner.ts、session-team.ts、instruction-node.ts）+ 1 个 SKILL.md + 测试/fixture 断言更新。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md`（v1.3.0）核心原则与开发流程门禁评估本特性：

| 原则/门禁 | 评估 | 结论 |
|---|---|---|
| **I. 引用溯源** | 所有契约/代码引用仓库内相对路径（`projects/game/agent/src/...`）与 039 契约链接。停止行格式变更的权威来源为 `saolei-mcp.ts` `operateResultText`。 | ✅ 设计文档须含来源指针 |
| **II. 重构式变更** | 三处修复均与既有架构对齐（格式一致化 / 帧发射对齐 / 方法提取），非补丁堆叠。issue 1 停止行格式与 gameLog 一致；issue 2 review 与 init 帧发射一致；issue 3 提取 `startInstructionTurn()` 公共方法。 | ✅ 合规（本特性即对齐式重构） |
| **III. 接口优先设计** | Phase 1 先定三份契约（停止行格式 / review 帧发射 / refresh 指令触发），再实现。既有接口（`operateResultText` 签名、`emitChannelFrame` 签名、`refreshTeam` 行为）的变更在契约中明确。 | ✅ 合规 |
| **IV. 测试颗粒度** | 编译+单测属各代码变更 task 内（不单列）；大型测试单列验收 task（FR-014）。 | ✅ 合规 |
| **V. 编码前阅读文档** | `tasks.md` 须为每 phase 声明三分类文档清单（代码规范/官方文档/技术文章），含间接引用。 | ✅ 由 `/speckit.tasks` 落实 |
| **VI. 大型测试验收** | 服务型应用，FR-014 强制大型测试，须经 testplan skill 完整执行（部署→测试→清理），全部用例通过。 | ✅ 合规（见 `quickstart.md`） |

**门禁执行顺序**：文档阅读 → 实现（对齐式+接口优先）→ 编译+单测 → 引用 → 大型测试验收。

**无违规需豁免**（Complexity Tracking 留空）。

## Project Structure

### Documentation (this feature)

```text
specs/042-planner-memory-fixup/
├── plan.md                               # 本文件
├── research.md                           # Phase 0：设计决策（D1–D3）
├── data-model.md                         # Phase 1：状态/行为变更模型（最小——无新实体）
├── quickstart.md                         # Phase 1：端到端验证指南
├── contracts/                            # Phase 1：接口契约
│   ├── saolei-operate-stop-format.md     # 停止行格式变更 + 影响面（SKILL.md/测试/fixture）
│   ├── review-instruction-display.md     # review 指令实时显示帧（复用 instruction-node 模式）
│   └── refresh-instruction-trigger.md    # RefreshTeam 后初始指令触发（复用 runInitTurn + 提取公共方法）
└── tasks.md                              # Phase 2（/speckit.tasks 生成，非本命令）
```

### Source Code (repository root)

```text
projects/game/
├── agent/src/
│   ├── mcp/saolei/saolei-mcp.ts          # 【改】operateResultText 停止行：序号 → 操作参数
│   ├── team/
│   │   ├── planner.ts                    # 【改】review 节点：加指令写回的 emitChannelFrame 帧
│   │   └── instruction-node.ts           # 【改】ensureMessageId 从模块私有改为 export（函数实现不变）；帧发射模式权威来源（planner.ts 复用）
│   └── session-team.ts                   # 【改】refreshTeam 后触发指令；提取 startInstructionTurn 公共方法
├── agent/src/skill/saolei/SKILL.md       # 【改】停止行格式描述同步（stopped at op K → stopped at type(x,y)）
├── agent/src/mcp/saolei/saolei-mcp.test.ts  # 【改】停止行断言同步
├── testplan/
│   ├── agent_saolei_test.go              # 【改】停止行断言同步
│   ├── saolei_team_test.go               # 【改】停止行断言同步
│   └── helpers_test.go                   # 【改】停止行引用同步（如有）
└── fake-llm/service/testdata/
    └── sample_saolei_structural_stop.yaml # 【改】停止行注释/期望同步（如有）
```

**Structure Decision**: 无新增文件、无新增目录、无新增服务。三处修复均为既有文件的定向变更：
- **saolei-mcp.ts**：`operateResultText` 函数签名变更（`stoppedAt: number | null` → `stoppedOp: CellOperation | null`）+ 调用点传参变更（`stoppedAt = i + 1` → `stoppedOp = operations[i]`）。
- **planner.ts**：review 节点返回前，当 instruction 不为 null 时，发射 `emitChannelFrame(PRIMARY_AGENT_NAME, instruction, ensureMessageId(writeBack), "MESSAGE_ROLE_USER")` 帧（复用 instruction-node.ts 的 `ensureMessageId` 模式）。
- **instruction-node.ts**：`ensureMessageId` 从模块私有改为 export（函数实现不变），供 planner.ts 导入复用。
- **session-team.ts**：提取 `startInstructionTurn()` 私有方法（`runInitTurn` + `initInFlight` 管理），`triggerInitInstruction` 调用它（保留一次性守卫），`refreshTeam` 清空通道后也调用它（无守卫，可重复触发）。
- **SKILL.md + 测试**：格式字符串断言/描述同步更新（`stopped at op K` → `stopped at type(x,y)`）。

## Complexity Tracking

> 无 Constitution Check 违规需豁免，本表留空。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
