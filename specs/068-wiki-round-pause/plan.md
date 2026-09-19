# Implementation Plan: planner wiki 知识库与 saolei-loop 每五轮自然停

**Branch**: `068-wiki-round-pause` | **Date**: 2026-09-19 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/068-wiki-round-pause/spec.md`；plan 阶段用户补充：资源标识形态对齐（移除 `Memory.memory_id`，id 由 name 承载）与标识分层原则（agent 面 title / 后端面 name）。

## Summary

三部分交付：

1. **wiki 服务端**（`projects/game/`）：`WikiService`（Create/Get/Update/Delete/List/Search）+ `Wiki` 资源（name-only 标识，FR-017）+ mongo `game_memory.wikis` 集合（title 规范化唯一 + bigram multikey 检索索引）+ gateway REST 注册 + testplan 大型测试；同时 clean break 移除 `Memory.memory_id`（proto/handler/JS client/testplan 四面同步）。
2. **wiki 工具面**（`common/js/dsh-plugins/`）：memory 插件行新增三个 wiki 工具（`wiki_search` 仅 query / `wiki_get` 批量 / `wiki_write` 单条 XOR 批量 upsert）；memory-service 包扩展 `WikiStore` seam 与 wiki 操作核；memory 工具描述通用化 + top-10 快照说明；两个英文 prompt section（memory-vs-wiki 分工差异、wiki 整体用法）。
3. **自然停**（`saolei-loop`）：orchestrator 完成轮计数，每第 5 个完成轮的 planner turn（含排队消息消化）结束后停止自动续驱 player，activation 切至 player、计数清零，用户消息恢复。

技术路线沿用 `survey/game-wiki-knowledge-base.md` 的既定决策（n-gram 预过滤 + `$all`、List 无 order_by、Search 查询限定 offset 分页、redis 快照缓存可降级），plan 阶段新增决策见 [research.md](research.md)（R1–R8）。

## Technical Context

**Language/Version**: Go（bazel 构建，gRPC + grpc-gateway）；TypeScript（pnpm workspace，cordis/dsh 插件体系）。

**Primary Dependencies**: protoc-gen-go-aip（资源名解析代码生成）；`golang.org/x/text`（NFKC 规范化，go.mod 已有）；`@deepseek-ai/dsh-tools`（defineTool）；`@grpc/grpc-js` + `@grpc/proto-loader`（JS gRPC client）；redis 公共库（`common/gopkg/redis`，067 交付，仅 Search 缓存可选使用）。

**Storage**: mongo 7.0 社区版（`game_memory` 库新增 `wikis` 集合；无 Atlas Search——检索走应用侧 n-gram，见调研 §4）。

**Testing**: `bazel test`（Go 单测：fake collection；JS 单测：vi.fn DI doubles）；testplan skill（`guitar run`，gateway HTTP 大型测试，宪法 VI）。

**Target Platform**: Linux 容器服务（memory 服务同二进制扩展；agent_v2 宿主 JS 服务）。

**Project Type**: web-service（gRPC + REST gateway）+ agent 插件库。

**Performance Goals**: Search 候选集以单会话为上界（10²–10³ 条），`$all` 索引预过滤 + Go 侧验证评分，单次检索毫秒级；List 单页默认 20、上限 100（content 体积由分页限制）。

**Constraints**: 自托管 mongo 7.0 社区版（无 `$search`）；title ≤ 128 rune、content ≤ 64KB（INVALID_ARGUMENT）；工具结果永不为抛出的错误（文本结果约定）。

**Scale/Scope**: 单 planner 写者/会话；wiki 条目无快照注入（上下文零成本），检索按需。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁（宪法原则） | 状态 | 说明 |
|---|---|---|
| 1. 文档阅读门禁（V） | ✅ 计划于 tasks.md 落实 | 每 phase 三分类文档清单（style/、官方文档 URL、survey+research+contracts），规划时逐一阅读 |
| 2. 实现门禁（II/III） | ✅ | 接口先行：`contracts/wiki-service.md`、`contracts/memory-resource-identity.md`、`contracts/memory-page-token-context.md`、`contracts/dsh-plugins.md`、`contracts/saolei-loop-round-stop.md` 先于实现；重构式：memory_id 移除与 064 契约 §2 item 4 修订、065 契约 §1 item 4 token 一致性修订随功能同批交付，不留过渡态 |
| 3. 编译 + 单测门禁（IV） | ✅ | 每次 `bazel build`/`bazel test`（相关 target）为开发任务一部分，不单列 task |
| 4. 引用、图表与终态门禁（I/VII/VIII） | ✅ | 所有产出带仓库相对路径/URL 引用；需求正文与 Clarifications 均只表述终态决策（不保留被否决方案的演进过程）；图表统一 Mermaid |
| 5. 大型测试验收门禁（VI） | ✅ | wiki 服务端走 testplan 实际执行（部署→测试→清理闭环、全用例通过）；自然停以编排层单测验收的裁断与理由记录于 research.md §7，豁免说明落于 `projects/game/agent_v2/README.md` §大型测试（宪法 VI 豁免记录） |

**Phase 1 设计后复核**：四份契约 + data-model 已产出，接口设计先于实现（门禁 2）；无未决 NEEDS CLARIFICATION。**通过**。

## Project Structure

### Documentation (this feature)

```text
specs/068-wiki-round-pause/
├── plan.md                        # 本文件
├── research.md                    # Phase 0 输出（R1–R8 决策）
├── data-model.md                  # Phase 1 输出（资源/存储/工具 schema/状态机）
├── quickstart.md                  # Phase 1 输出（验证指南）
├── checklists/
│   └── requirements.md            # spec 质量检查清单
├── contracts/
│   ├── wiki-service.md            # WikiService RPC 契约（含标识形态、检索、分页）
│   ├── memory-resource-identity.md# Memory.memory_id 移除（clean break 改造清单）
│   ├── memory-page-token-context.md# ListMemories token 显式 order_by 上下文（修订 065 §1 item 4）
│   ├── dsh-plugins.md             # memory 插件修订（三工具/prompt/服务面，修订 064）
│   └── saolei-loop-round-stop.md  # 自然停状态机契约
└── tasks.md                       # Phase 2 输出（/speckit.tasks，本命令不创建）
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                        # WikiService + Wiki + 各请求消息；Memory.memory_id 移除（reserved）
├── BUILD.bazel                       # gazelle 生成（proto codegen 单元内新增消息）
├── memory/
│   ├── cmd/main.go                   # WikiService server 注册（同 target，同 mongo db）
│   ├── handler/{handler,handler_test}.go        # wiki handler（CRUD/List/Search）+ memory handler 响应构造修订
│   ├── domain/                       # wiki model/校验/规范化（NormalizeTitle/TitleBigrams）/token/评分
│   └── runtime/mongo/                # wikis 仓储（$all 预过滤）+ 启动建索引（3 个）
├── gateway/cmd/main.go               # WikiService grpc-gateway 注册（gameconst.MemoryTarget）
├── agent_v2/                         # preset/编排零改动（research §4：plannerMemory 面扩展）
└── testplan/
    ├── wiki_test.go                  # 新增：gateway HTTP 端到端（仿 memory_test.go）
    └── memory_test.go                # 修订：memory_id 断言 → name 断言
common/js/dsh-plugins/
├── memory/src/                       # 三 wiki 工具定义 + prompt section 常量 + memory 描述修订
├── memory-service/src/               # WikiStore/client + wiki 操作核（upsert/批量原子/精确解析）+ 服务面扩展 + client name 解析
└── saolei-loop/src/orchestrator.ts   # 完成轮计数 + 停点判定（+ 单测）
```

**Structure Decision**: 全部落入既有目录与分层（AIP 服务分层 handler/domain/runtime、插件两包结构、编排器单文件），不新增顶层结构；`projects/game/testplan/` 增量一个测试文件。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违例需要论证：新能力全部复用既有基础设施（mongo/gRPC/gateway/插件模式/编排状态机），最大新增面是 WikiService 单条服务线与三份契约；memory_id 移除是收缩性变更（减字段、减解码分支）。
