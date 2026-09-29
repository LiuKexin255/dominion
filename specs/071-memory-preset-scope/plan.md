# Implementation Plan: memory 作用域从 session 迁移至 preset、web memory 管理与侧栏二级导航

**Branch**: `071-memory-preset-scope` | **Date**: 2026-09-28 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/071-memory-preset-scope/spec.md`；clarify 阶段四项裁定（web 创建 id 内容摘要派生、二级菜单浮出形态、列表默认 update_time 降序、集合不设容量上限）；本 plan 为对已交付特性（039/064/065/068/069 memory 面、058/059/060 preset 面、069 web wiki 面）的增量修订，作用域迁移口径对齐 `specs/069-wiki-template-web/plan.md`（wiki session→template 先例）。

## Summary

四部分交付：

1. **memory 作用域迁移**（服务端 + 工具面）：Memory 资源 pattern 由 `templates/{template}/sessions/{session}/memories/{memory}` 迁移为 `templates/{template}/presets/{preset}/memories/{memory}`——proto 注解与注释、handler 父解析（`ParsePresetName`）、domain 作用域键（`SessionID`→`PresetID`）、mongo 文档 `preset_id` 字段与两个 preset 维度索引（启动时创建）；旧索引与旧文档的清除为新服务首启前的一次性运维清理（不进服务代码，`wiki_repository.go` 既有启动清理代码同批移除）；JS `PlannerMemoryScope` 收敛为 `{template, preset}`、client 寻址与 operations 签名随迁；四 RPC 语义（校验、order_by、page token 上下文、分页）零变化，同一 planner preset 的记忆跨 session 持久共享。
2. **物化装配迁移**：`agent_v2` `doMaterialize` 的 `memoryScope` 改从 planner member 的 preset 资源名构建（`presetId()` 已有解析）；快照加载（10 条近因、冻结）、memory 工具写路径、终局回顾落库全部落到物化来源 preset 的集合。
3. **web memory 管理视图 + 侧栏二级导航**：`api/memory.ts` REST 客户端（preset 级路由、默认 `order_by=update_time desc`、创建 id 前端 `sha256(content)[:32]` 派生——与工具路径同值域）+ `MemoryView` 组件（preset 选择器 + 分页列表/查看/创建/编辑/二步删除，PresetsView/WikiView 模式）+ `App` 侧栏两级导航（一级 sessions/presets + 固定入口按钮（文案定为**「知识」**，见 `contracts/web-nav.md`）弹出浮层承载 wiki/memory，侧栏底部行数恒定）。
4. **场景无关文案清理**：`WikiView` 两处场景化文案（空态 planner、扫雷示例占位）修正为中性措辞；`MemoryView` 全部文案中性化；组件测试做场景词扫描断言。

技术决策见 [research.md](research.md)（R1–R10）；契约四份 + data-model/quickstart 已产出（Phase 1）。

## Technical Context

**Language/Version**: Go（bazel 构建，gRPC + grpc-gateway，protoc-gen-go-aip 资源名 codegen）；TypeScript（pnpm workspace，cordis/dsh 插件体系 + vite/React 前端）。

**Primary Dependencies**: protoc-gen-go-aip（`//:go_gen_aip`，preset 级父解析先例：Preset 之于 template，`projects/game/BUILD.bazel` 单 codegen 单元两 proto 同包）；`@deepseek-ai/dsh-client-ui-primitives`（Button/Input，原生 `<textarea>` 先例见 PresetsView persona）；WebCrypto `crypto.subtle`（web 端摘要 id 派生，与 node `createHash` 同算法同值域）。

**Storage**: mongo 7.0 社区版（`game_memory.memories`：文档 `session_id`→`preset_id`、索引 unique `(template, preset_id, memory_id)` + `(template, preset_id, update_time desc, memory_id asc)`；旧索引与旧文档的清除为新服务首启前的一次性运维清理（服务启动仅创建新索引）；`game_agent_v2.presets` 零变更）。

**Testing**: `bazel test`（Go 单测：fake collection；JS 单测：vi.fn DI doubles）；vitest（web 组件 + api 客户端）；testplan skill（`guitar run`，gateway HTTP 大型测试，宪法 VI）。

**Target Platform**: Linux 容器服务（memory 服务二进制内变更；agent_v2 宿主 JS 服务；web 静态托管 + gateway `/api/v1`）。

**Project Type**: web-service（gRPC + REST gateway）增量修订 + agent 插件库 + web 前端视图。

**Performance Goals**: 无硬性延迟验收阈值；快照为一次 ListMemories 单页（≤10 条）预取，沿用现状；web 视图请求为常规 REST 往返。

**Constraints**: 自托管 mongo 7.0 社区版；memory content ≤ 1024 rune（不变）；memory_id 匹配 `[a-z0-9_-]+`（不变）；服务端不校验 parent preset 存在性（跨服务零耦合，与 session 口径一致）；工具结果永不为抛出的错误（文本结果约定）；web 无新增 CORS 面（同源相对路径）；侧栏底部行数恒定（浮出菜单不占侧栏空间）。

**Scale/Scope**: template 封闭集当前仅 `saolei`；单 preset memory 集合不设条数上限（spec FR-018，容量治理另行立项）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁（宪法原则） | 状态 | 说明 |
|---|---|---|
| 1. 文档阅读门禁（V） | ✅ 计划于 tasks.md 落实 | 每 phase 三分类文档清单（style/、官方文档 URL、research/contracts/data-model），规划时逐一阅读；本 plan 引用的仓库内契约（`specs/039-planner-memory-calibration/contracts/memory-service-contract.md`、`specs/068-wiki-round-pause/contracts/memory-page-token-context.md`、`specs/069-wiki-template-web/contracts/memory-content-limit.md`、`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`、`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md`）已在 tasks.md 显式列出 |
| 2. 实现门禁（II/III） | ✅ | 接口先行：`contracts/memory-service.md`、`contracts/dsh-plugins.md`、`contracts/web-memory-view.md`、`contracts/web-nav.md` + data-model 先于实现产出；重构式：作用域迁移是对 memory 资源模型的架构级修订（非补丁），proto/存储/工具面/物化装配/测试同批收敛到 preset 终态，无过渡态、无兼容层 |
| 3. 编译 + 单测门禁（IV） | ✅ | 每次 `bazel build`/`bazel test`（相关 target）为开发任务一部分，不单列 task |
| 4. 引用、图表与终态门禁（I/VII/VIII） | ✅ | 产出带仓库相对路径/URL 引用；clean break 无演进残留（旧索引/旧字段/旧路由/旧注释清除，契约只述终态）；图表 Mermaid |
| 5. 大型测试验收门禁（VI） | ✅ | memory REST 面与跨 session 共享回路走 testplan 实际执行（部署→测试→清理闭环、全用例通过）；web 视图无浏览器执行面，UI 逻辑由 vitest 组件测试覆盖、REST 闭环由 testplan 覆盖（`specs/069-wiki-template-web/research.md` R8 同款裁断） |

**Phase 1 设计后复核**：四份契约 + data-model + quickstart 已产出且接口设计先于实现（门禁 2）；无未决 NEEDS CLARIFICATION（spec 四项 clarify 裁定已回落到契约）。**通过**。

## Project Structure

### Documentation (this feature)

```text
specs/071-memory-preset-scope/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── memory-service.md      # MemoryService preset 级服务契约（REST/gRPC）
│   ├── dsh-plugins.md         # JS 作用域模型与插件契约（scope/client/guidance）
│   ├── web-memory-view.md     # web memory 管理视图契约
│   └── web-nav.md             # 侧栏两级导航（浮出菜单）契约
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                    # Memory 资源 pattern/注释/HTTP 注解迁移（MemoryService 面）
├── memory/
│   ├── handler/handler.go        # parent 解析 ParseSessionName→ParsePresetName（+测试）
│   ├── domain/model.go           # Memory.SessionID→PresetID（+测试）
│   └── runtime/mongo/            # 文档字段/索引 preset 维度（repository.go、model.go，+测试；移除 legacy 启动清理代码——含 wiki_repository.go）
├── gateway/cmd/main_test.go      # /api/v1 路由断言路径更新（注册代码零改动）
├── agent_v2/src/session.ts       # memoryScope 从 planner preset 构建（+测试）
└── web/frontend/src/
    ├── api/memory.ts             # 新增：preset 级 REST 客户端（+测试）
    ├── api/agent.ts              # 复用 listPresets（零改动）
    ├── components/MemoryView.tsx # 新增：memory 管理视图（+测试）
    ├── components/WikiView.tsx   # 两处场景化文案修正（+测试断言）
    ├── components/App.tsx        # 侧栏两级导航改造（+测试）
    └── theme.css                 # 导航浮层/视图样式补充
common/js/dsh-plugins/
├── memory-service/src/           # client/service/operations 签名随迁（+测试）
├── memory/src/guidance.ts        # 分工段跨局持久措辞修订（+测试）
└── saolei-loop/src/orchestrator.ts # PlannerMemoryScope 形态随迁（+测试）
projects/game/testplan/
├── memory_test.go                # HTTP 用例 preset 级路由改造
└── agent_v2_preset_test.go       # 跨 session 共享新用例 + 既有 memory 用例随迁
```

**Structure Decision**: 仓库既有多模块布局内增量修订（Go 服务 + JS 插件 workspace + web 前端），无新增顶层结构；上表为实际改动文件清单。

## Complexity Tracking

> 无宪法门禁违例，无需记录。
