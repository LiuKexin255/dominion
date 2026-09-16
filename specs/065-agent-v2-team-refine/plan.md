# Implementation Plan: agent_v2 team 优化——扫雷系统终局播报、记忆快照近因注入与广播格式提示词澄清

**Branch**: `065-agent-v2-team-refine` | **Date**: 2026-09-15 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/065-agent-v2-team-refine/spec.md`

## Summary

三块优化：(1) saolei team 新增"扫雷系统"系统角色（role `saolei`，非 LLM、非物化成员），在终局交接路径（player 回合收束 → planner 复盘前）播报本局统计消息（结果 + 单个操作总数 + click/flag/chord 分项数），全体真实成员消费，排队消化优先序与"被跳过局不补发"语义继承自既有 `nextStep()` 分支结构；(2) planner 记忆快照按条目更新时间倒排、仅注入最近 10 条——排序由 memory 服务 `ListMemories` 的**通用排序机制**承担（AIP-132 `{field} [desc]` 语法 + 字段白名单映射 + 唯一键收尾 + 通用游标 + 单路径仓储，2026-09-16 两次用户裁定），JS 客户端以 `page_size=10 + order_by=update_time desc` 单页装载；(3) team section 提示词补两处——广播标签格式仅输入侧呈现（成员自身输出不自我包装）+ roster 含 saolei 行。

技术方案（详见 [research.md](research.md)）：**team 插件定义成员消息源接口（`TeamMemberSource`，依赖倒置）**——成员（agent 或非 agent）实现该接口提供消息（`events` 为共享事件词汇表的 log），team 的派生/渲染/消费闭包全复用、不感知成员种类；agent 经 `agentMemberSource` 适配器接入（现有语义零变化）。扫雷系统成员（`SaoleiSystemMember`，saolei-loop 实现）持**内存 log**（`assistant/message` 形态事件，随物化清零——与 agent 成员 log 实际行为对齐），以常规 announce-only 成员注册（roster 自然渲染）；统计触发落在 orchestrator `nextStep()` 终局分支（`announce` 先于 `drain(planner)`，`statsSentFor` 记录级 guard 保证 exactly-once）；宿主经 `orchestrator.announcer` 订阅其产出追加 merge/`team_message`（与 MemberCollector 订阅 agent 事件同构的投影路径）。

## Technical Context

**Language/Version**: TypeScript（ESM，strict；048-js-esm-migration 终态）；Go（memory 服务排序增量 + 测试计划侧断言）

**Primary Dependencies**: `@deepseek-ai/cordis` / `@deepseek-ai/dsh-agent` / `@deepseek-ai/dsh-llm` / `@deepseek-ai/dsh-session`（0.1.1-rc.2 线）；`@dominion/dsh-team`、`@dominion/dsh-saolei-loop`、`@dominion/dsh-memory-service`（本仓库 common/js/dsh-plugins/）；`@grpc/grpc-js` + `@grpc/proto-loader`（memory 客户端）

**Storage**: 进程内存（team 编排态/成员产出单元/统计计数）；Mongo（preset、memory 服务存储——经服务，不直连）

**Testing**: vitest 单测（bazel test //common/js/dsh-plugins/... //projects/game/agent_v2/...）；大型测试 testplan skill（`guitar run projects/game/testplan/system_test.yaml`，fake-llm + fake-desktop）

**Target Platform**: Linux 服务（k8s 部署，服务发现名 `agent-v2`）

**Project Type**: web-service（gRPC team agent 服务 + dsh 进程内组合）

**Performance Goals**: 无新增性能目标（成员播报为每局一条的同步内存操作；快照截断缩减 planner prompt token）

**Constraints**: 现有契约保持——proto 会话面零改动（`TeamMessage.member` 本就是 string）；`GetTeam.members`/`active_member` 不含系统角色；059 FR-009/010 的驱动输入不变量在"系统产生的团队消息"意义上保持（spec Assumptions）

**Scale/Scope**: 单实例内存态 team（owner 亲和路由）；记忆条目典型 <100、注入 ≤10

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁 | 状态 | 说明 |
|---|---|---|
| 1. 文档阅读门禁（原则 V） | ✅ 计划期已履行 | research/contracts 引用均已实际阅读核实（见 research.md 源码锚点）；tasks 阶段文档清单由 /speckit.tasks 落实 |
| 2. 实现门禁（原则 II/III） | ✅ | 接口先行：`contracts/team-member-source.md`、`contracts/game-stats-broadcast.md`、`contracts/memory-snapshot-recency.md` 先于实现定义契约；重构式：team 的成员抽象泛化为消息源接口（依赖倒置——成员实现接口提供消息），扫雷系统成员是接口的非 agent 实现者，team 无"系统广播"特设概念，`reconcile` 读权威分析见 research.md D1 |
| 3. 编译 + 单测门禁（原则 IV） | ✅ 计划内 | 每次代码变更伴随 `bazel build` + `bazel test`（相关 target），不单列 task |
| 4. 引用、图表与终态门禁（原则 I/VII/VIII） | ✅ | 全部引用带仓库相对路径或完整 URL；图表统一 Mermaid；文档只表述终态 |
| 5. 大型测试验收门禁（原则 VI） | ✅ 计划内 | 验收含 testplan skill 实际执行（deploy→test→cleanup 闭环）且全部用例通过，见 quickstart.md §3 |

Phase 1 设计后复核：无新增违规——三份契约覆盖全部新接口面；无过度设计（成员消息源接口是"非 agent 成员经既有派生/消费机制提供消息"的最小抽象，扫雷成员实现收敛在 saolei-loop 内）。

## Project Structure

### Documentation (this feature)

```text
specs/065-agent-v2-team-refine/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── team-member-source.md
│   ├── game-stats-broadcast.md
│   └── memory-snapshot-recency.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
common/js/dsh-plugins/
├── team/src/
│   ├── team.ts          # TeamMemberSource 接口 + agentMemberSource 适配器 + 注册/drain/relay/reconcile 读 source（announce-only 能力位）
│   ├── broadcast.ts     # sender 键语义放宽（成员 id，类型 string）
│   ├── section.ts       # roster（含 saolei 行）+ 广播标签仅输入侧表述
│   └── index.ts         # 导出面：TeamMemberSource / agentMemberSource / 类型形状更新
├── saolei-loop/src/
│   ├── announcer.ts     # （新）SaoleiSystemMember：内存 log + announce() + TeamMemberSource 实现（announce-only）
│   ├── orchestrator.ts  # 终局分支 announce（先于 drain(planner)）+ statsSentFor guard + 注册第三成员 + announcer 访问器
│   ├── game/runtime.ts  # 每局 per-type 操作计数（init 清零、ok 递增）
│   ├── game/board.ts    # GameStats 扩展 operationsByType + computeGameStats 签名扩展
│   ├── game/text.ts     # 统计消息文本模板 gameStatsText
│   └── index.ts         # 导出面：SaoleiSystemMember / SAOLEI_MEMBER_SUMMARY / gameStatsText
└── memory-service/src/
    ├── client.ts        # listMemories 增可选 {orderBy, pageSize}（pageSize 给定单页即止）；MemoryEntry = {memory_id, content}
    ├── snapshot.ts      # renderMemorySnapshot 纯透传渲染 + SNAPSHOT_ORDER_BY/SNAPSHOT_ENTRY_LIMIT 注入策略常量
    └── service.ts       # load 以 {orderBy, pageSize: 10} 单页装载最近 10 条

projects/game/
├── game.proto           # ListMemoriesRequest 增 string order_by = 4（AIP-132 通用语法 + 白名单 + 唯一键收尾 + token 键匹配注释）
└── memory/
    ├── domain/          # sort.go（新）：MemorySortTerm + MemorySortFieldSpec 白名单映射表 + ParseMemoryOrderBy；
    │                    # pagination.go：通用游标 codec（字段序列 + 类型化键值）；ErrInvalidPageToken + 仓储签名 sort []MemorySortTerm
    ├── handler/handler.go  # order_by → domain.ParseMemoryOrderBy；解析错误/ErrInvalidPageToken → INVALID_ARGUMENT（无排序知识）
    └── runtime/mongo/repository.go  # 单路径 ListMemories：通用 sort spec + 键匹配 + OR 阶梯 + limit+1 + 游标构造；
                                     # 启动建复合索引（既有唯一索引不变）

projects/game/agent_v2/src/
├── session.ts           # doMaterialize 订阅 orchestrator.announcer 产出 → appendAnnouncement（teardown 退订）
└── history.ts           # TeamHistory.appendAnnouncement（ROLE_AGENT + appendMerge member 放宽为 string）

projects/game/testplan/
├── agent_v2_game_test.go         # 终局统计播报断言（内容/条数/消费面）
├── agent_v2_conversation_test.go # 排队跳局场景断言（按现有用例归属扩展）
└── memory_test.go + helpers_test.go  # 有序列表端到端断言（通用语法正路径/排序/复合游标续页/非法 order_by 400）
```

**Structure Decision**: 复用既有三插件 + 宿主布局（team / saolei-loop / memory-service / agent_v2 / testplan），无新目录；`node_modules` 符号链接与 `BUILD.bazel` 由 gazelle 维护（AGENTS.md 流程）。

## Complexity Tracking

> Constitution Check 无违规，无需条目。
