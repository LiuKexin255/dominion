# Implementation Plan: Planner 长期记忆与校准指令

**Branch**: `039-planner-memory-calibration` | **Date**: 2026-08-07（**Amended 2026-08-08**：memory 工具→hermes 式单工具；planner 注入→纯内容；saolei_operate→双形态；新增 memory skill） | **Spec**: [`spec.md`](./spec.md)

**Input**: Feature specification from `/specs/039-planner-memory-calibration/spec.md`

## Summary

对 `specs/031-team-template-mode/` 已落地的 team 架构做四方面改进（破坏性重构，clean break）：

1. **saolei 批量落子**：合并 `saolei_click`/`saolei_flag`/`saolei_chord_click` 为单个 `saolei_operate`（hermes 式双形态参数：普通参数 `type`/`x`/`y` 单次 **或** `operations` 数组批量；保序执行、单次返回；失败按原因细分：无害空操作跳过继续，游戏结束/结构性错误停止）。`saolei_init`/`saolei_remain` 不变。planner 游戏历史以 `saolei_operate` 为单位记录。
2. **planner 长期记忆 + memory 服务**：新建 grpc-go `MemoryService`（独立数据库 `game_memory`，同 mongo 实例，遵循 `style/mongo.md`），承载 planner 长期记忆条目（资源 `templates/{template}/sessions/{session}/memories/{memory}`，存储 API memory_id 式不变）。agent 上新建 memory mcp server，向 planner 暴露**单一 hermes 风格 `memory` 工具**（`action`/`content`/`old_text`/`operations`，无 `memory_id`/无 `target`，Session 2026-08-08），由 agent 将 hermes 式调用转换为 memory 服务的 memory_id 式 RPC（`add`→agent 生成 id+Create；`replace`/`remove`→listMemories+`old_text` 子串定位；0/多命中报错）后经 memory-client 转发（mcp 不直连）。planner 长期记忆以**冻结快照**注入 planner 系统提示词（**纯内容**，无 `memory_id`），压缩边界刷新（调研 D2/D4/D5）。配套 **memory skill**（planner 专属，参考 hermes 引导）注入 planner 系统提示词（FR-020）。
3. **校准指令 + 废弃共享 StrategyStore**：移除 `StrategyStore`/`update_strategy`/player"当前态势"注入。planner 经**指令发送工具**向 player 投递策略指令（HumanMessage 进 `playerMessages`，调研 D6；跨通道写入经外部 buffer 中转 R1）。两场景（均 LLM 决定是否调用工具，无强制检验 R4）：**正常复盘**（携带游戏历史、prompt"必要时才调用"、可选、同 turn 立即注入、player 继续）；**team 初始化/压缩后**（无游戏历史、prompt 要求给指令、不触发 player invoke；初始化**异步**产出 R2、压缩后 turn 结束与下次激活一同注入，与 037"压缩后自动停下"一致）。
4. **大型测试**：覆盖批量操作、memory 持久化与冻结快照、两场景指令投递。

调研依据：`survey/planner-memory-and-agent-communication.md`（hermes 冻结快照/记忆工具/压缩刷新、openclaw HumanMessage、LangGraph 通道）。

**重构方向**（宪法原则 II）：移除 StrategyStore 共享记忆设计，新增 memory 服务 + memory mcp + 指令工具 + 两场景节点，同步进行（非补丁）。

## Technical Context

**Language/Version**: Go 1.x（新建 memory 服务）；TypeScript（agent，grpc-js）；既有 Svelte desktop 不受本特性影响（指令走既有 frame 通道）。

**Primary Dependencies**:
- Go: `go.mongodb.org/mongo-driver`（memory 仓储）、`google.golang.org/grpc` + `protoc`/`protoc-gen-go-aip`（AIP 风格 MemoryService）、`dominion/common/gopkg/{bootstrap,grpc,mongo,otel}`（复用 prompt 服务同款脚手架，`projects/game/prompt/cmd/main.go`）。
- TS: `@langchain/langgraph` ^1.4.8 / `@langchain/core` ^1.2.3（team graph 节点/通道/冻结快照）、`@modelcontextprotocol/sdk`（memory mcp server）、`@grpc/grpc-js` + `@grpc/proto-loader`（memory-client，复用 `prompt-client.ts` 模式）。版本统一于 `pnpm-workspace.yaml` catalog。

**Storage**:
- **MongoDB**（当前 `game/mongo` 实例，`deploy.yaml` infra mongodb）：
  - **Memory**（planner 长期记忆）——由新建 memory 服务管理，**独立数据库 `game_memory`**（`style/mongo.md`：每服务独立数据库，MUST NOT 与 agent 的 `game_prompt` 或 prompt 的库混用，FR-006）。集合 `memories`，唯一索引 `(template, session, memory_id)`。**条目上限决策**：v1 不设硬上限（冻结快照单页烘焙假设，达上限 consolidate 策略缓行——落实 spec 边案例"由 plan 决定"，详见 `contracts/memory-service-contract.md` §ListMemories 注）。
  - 移除：`strategies` 集合（agent 的 `game_prompt` 库）——StrategyStore 废弃（FR-013）。
- **内存 checkpointer**（`MemorySaver`）：短期消息（playerMessages/plannerMessages），不变。
- **进程内 ephemeral buffer**：游戏状态/结束事件 + 指令 pending 槽（压缩/初始化场景的待注入指令）。

**Testing**: `bazel test //...`（Go 单测 + TS `js_test`）；大型测试经 testplan skill（`tools/test/guitar`，`projects/game/testplan/`），宪法原则 VI 强制。

**Target Platform**: Linux 服务（新增 memory 服务 + 既有 session/proxy/prompt/agent/gateway）。

**Project Type**: 多服务 web 应用（gRPC 微服务）。

**Performance Goals**: 无新增硬性性能目标；memory 读写低频（压缩边界读快照、planner 复盘时增删改），memory 服务直连 mongo 可接受。planner 冻结快照避免每次激活重读（D5）。

**Constraints**: clean break（不考虑 `strategies` 数据迁移，FR-013）；proto 通用+typed（AIP 风格，无 blob/无潜规则）；memory 服务独立数据库（`style/mongo.md`）；memory mcp 经 agent 转发、不直连 memory 服务（FR-007）；LangGraph 1.x createAgent 无自身 checkpointer（外层 MemorySaver 持有，031 spike D14 既有约束）。

**Scale/Scope**: 单运维者研究型应用（扫雷 agent）；单模板 `saolei`，设计为多模板扩展留位。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md`（v1.3.0）核心原则与开发流程门禁评估本特性：

| 原则/门禁 | 评估 | 结论 |
|---|---|---|
| **I. 引用溯源** | 所有契约/数据模型引用仓库内相对路径（`projects/game/game.proto`、`projects/game/prompt/...`）与外部 URL（AIP/LangGraph/hermes docs）。 | ✅ 设计文档须含来源指针 |
| **II. 重构式变更** | 移除 StrategyStore 共享记忆旧设计，新增 memory 服务 + memory mcp + 指令工具 + 两场景节点 + saolei_operate，同步交付（非补丁）。 | ✅ 合规（本特性即重构） |
| **III. 接口优先设计** | Phase 1 先定 MemoryService proto 契约（`contracts/memory-service-contract.md`）、memory mcp 契约、team-graph 更新契约、saolei-operate 契约，再实现。 | ✅ 合规 |
| **IV. 测试颗粒度** | 编译+单测属各代码变更 task 内（不单列）；大型测试单列验收 task（FR-018）。 | ✅ 合规 |
| **V. 编码前阅读文档** | `tasks.md` 须为每 phase 声明三分类文档清单（代码规范/官方文档/技术文章），含间接引用。 | ✅ 由 `/speckit.tasks` 落实 |
| **VI. 大型测试验收** | 服务型应用，FR-018 强制大型测试，须经 testplan skill 完整执行（部署→测试→清理），全部用例通过。 | ✅ 合规（见 `quickstart.md`） |

**门禁执行顺序**：文档阅读 → 实现（重构式+接口优先）→ 编译+单测 → 引用 → 大型测试验收。

**无违规需豁免**（Complexity Tracking 留空）。

## Project Structure

### Documentation (this feature)

```text
specs/039-planner-memory-calibration/
├── plan.md                          # 本文件
├── research.md                      # Phase 0：设计决策与调研（D1–D12）
├── data-model.md                    # Phase 1：实体与状态模型
├── quickstart.md                    # Phase 1：端到端验证指南
├── contracts/                       # Phase 1：接口契约
│   ├── memory-service-contract.md   # MemoryService（grpc-go）proto + RPC + mongo（存储 API 不变）
│   ├── memory-mcp-contract.md       # agent memory mcp server（单一 hermes 式 memory 工具/agent 转换/0-multi 命中/path 闭包/转发）
│   ├── memory-skill-contract.md     # memory skill（planner 专属，FR-020，参考 hermes 引导）
│   ├── team-graph-contract.md       # team graph 更新（两场景节点/指令工具/冻结快照纯内容/移除 strategy）
│   └── saolei-operate-contract.md   # saolei_operate（双形态落子工具）
└── tasks.md                         # Phase 2（/speckit.tasks 生成，非本命令）
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                         # 【新增】Memory 资源消息 + MemoryService（Create/Update/Delete/ListMemory）—— 存储 API 不变（Session 2026-08-08）
├── pkg/gameconst/const.go             # 【改】新增 MemoryService target 常量
├── memory/                            # 【新增】grpc-go memory 服务（仿 prompt 结构）
│   ├── cmd/main.go                    #   启动（mongo.NewClient("game/mongo") + db "game_memory"）
│   ├── handler/handler.go             #   MemoryService handler 实现
│   ├── domain/{model.go,repository.go}#   Memory 领域模型 + 仓储接口
│   ├── runtime/mongo/repository.go    #   mongo 仓储（memories 集合，独立 db）
│   └── service.yaml                   #   部署描述
├── deploy.yaml                        # 【改】新增 memory 服务条目
├── agent/src/
│   ├── memory-client.ts               # 【新增】gRPC client（dominion:///game/memory:50051，仿 prompt-client.ts；listMemories 供快照烘焙 + memory 工具 old_text 定位）
│   ├── mcp/memory/memory-mcp.ts       # 【新增】memory mcp server——单一 hermes 式 `memory` 工具（action/content/old_text/operations），agent 转换为 memory_id 式 RPC（add→生成 id；replace/remove→old_text 子串定位，0/多命中报错），path 闭包注入 template/session
│   ├── mcp-host.ts                    # 【改】每 mcp 独立 path（template-scoped：/internal/mcp/{template}/{session}/{saolei|memory}），按 (template,session,kind) 懒创建 McpServer；saolei path 同步迁移
│   ├── skill-loader.ts                # 【改】BUILTIN_SKILL_NAMES 注册 "memory"（FR-020）
│   ├── skill/memory/SKILL.md          # 【新增】memory skill（planner 专属，参考 hermes 引导，FR-020）
│   ├── team/
│   │   ├── graph.ts                   # 【改】两场景节点拆分（review 节点 + init/compact 节点）+ 路由
│   │   ├── planner.ts                 # 【改】移除 strategy 注入；持 memory 工具 + 指令发送工具；冻结记忆快照（纯内容）注入；appendSkillBodyToPrompt(base, ["memory"]) 装配 memory skill
│   │   ├── player.ts                  # 【改】移除 strategy"当前态势"注入；消费 pending 指令（init/compact）
│   │   ├── instruction-tool.ts        # 【新增】指令发送工具（写 HumanMessage 到 playerMessages）
│   │   ├── memory-snapshot.ts         # 【新增】冻结快照缓存（纯内容 toSystemMessage；压缩/init 边界刷新，调研 D5）
│   │   └── state.ts                   # 【改】移除 strategy 相关；新增 pending instruction 槽（init/compact）
│   ├── strategy-store.ts              # 【删除】StrategyStore 接口 + MongoStrategyStore（FR-013）
│   ├── server.ts                      # 【改】移除 StrategyStore/mongo strategy wiring（含 040 重建闭包 rebuilder 两处 buildTeamGraph 调用点）；接 memory-client + memory mcp（首建 factory 与重建闭包均注入 memory 装配）
│   ├── session-team.ts               # 【改】team 初始化异步触发 init 节点（R2，仅 graph 首建；prompt 引导、LLM 决定是否调用 instruct_player，无强制检验——R4）
│   └── mcp/saolei/saolei-mcp.ts       # 【改】saolei_click/flag/chord_click → saolei_operate（双形态参数）；gameLog 以 operate 为单位
└── testplan/                          # 【新增/改】planner-memory 端到端大型测试计划
```

**Structure Decision**: 复用现有多服务拓扑，**新增一个 memory 服务**（grpc-go，仿 `projects/game/prompt/` 结构）。memory 服务独立数据库 `game_memory`（`style/mongo.md`），存储 API（memory_id 式资源）不变。memory mcp server 位于 agent，向 planner 暴露**单一 hermes 风格 `memory` 工具**（agent 转换为 memory_id 式 RPC），与 saolei mcp 各自独立 path（template-scoped：`/internal/mcp/{template}/{session}/{saolei|memory}`，R3），经新增 `memory-client.ts`（仿 `prompt-client.ts`）转发到 memory 服务——mcp 不直连 memory 服务（FR-007）。planner 系统提示词注入 **memory skill**（FR-020，`skill-loader.ts` 注册 + `appendSkillBodyToPrompt`）。team graph 拆分两场景节点（FR-019）。

## Complexity Tracking

> 无 Constitution Check 违规需豁免，本表留空。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
