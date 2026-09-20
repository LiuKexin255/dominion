# Implementation Plan: wiki 提升为 template 级知识库、memory 单条长度上限与 web wiki 管理

**Branch**: `069-wiki-template-web` | **Date**: 2026-09-20 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/069-wiki-template-web/spec.md`；specify/clarify 阶段四项裁定（memory 上限 1024 rune、存量 clean break、浏览排序保持 List 语义、上限指引预先说明）；本 plan 为对已交付特性 `specs/068-wiki-round-pause/` 的增量修订。

## Summary

三部分交付：

1. **wiki 作用域提升**（服务端 + 工具面）：Wiki 资源 pattern 由 `templates/{template}/sessions/{session}/wikis/{wiki}` 提升为 `templates/{template}/wikis/{wiki}`——proto 注解、handler 父解析（`ParseTemplateName`）、domain 作用域键、mongo 文档去 `session_id` 与三个 template 维度索引、启动幂等 clean break（清旧索引 + 清旧文档）；JS `WikiStore`/wiki 操作核/`PlannerMemoryService` 转发全部收敛为 template 单参；六 RPC 语义与三个工具的模型面行为零变化，知识对同 template 全部 session 共享。
2. **memory 单条上限**：服务端 `MaxMemoryContentRunes = 1024` 写入校验（Create/Update → INVALID_ARGUMENT）+ JS 预检（单条与批量 preflight，超限领域文本含上限与 `wiki_write` 引导、全批不写）+ `memory:guidance` 分工段预先说明（约数表述）与 wiki 跨会话持久措辞。
3. **web wiki 管理视图**：`api/wiki.ts` REST 客户端（template 级路由、PATCH mask query 传递、`sha256(title_norm)[:32]` id 派生）+ `WikiView` 组件（搜索/分页列表/独占编辑表单/二步删除，PresetsView 模式）+ `App` 第三视图切换（固定 saolei）。

技术决策见 [research.md](research.md)（R1–R8）；契约四份 + data-model/quickstart 已产出（Phase 1）。

## Technical Context

**Language/Version**: Go（bazel 构建，gRPC + grpc-gateway，protoc-gen-go-aip 资源名 codegen）；TypeScript（pnpm workspace，cordis/dsh 插件体系 + vite/React 前端）。

**Primary Dependencies**: protoc-gen-go-aip（`com_github_protoc_contrib_protoc_gen_go_aip`，template 级子资源先例：session 之于 template、Preset 之于 template）；`@deepseek-ai/dsh-client-ui-primitives`（Button/Input/Modal 等，原生 `<textarea>` 先例见 PresetsView persona）；`@grpc/grpc-js` + `@grpc/proto-loader`（JS gRPC client）。

**Storage**: mongo 7.0 社区版（`game_memory.wikis`：文档去 `session_id`、索引 unique `(template, wiki_id)` / unique `(template, title_norm)` / multikey `(template, title_grams)`、启动幂等清理旧索引与旧文档；`memories` 集合零 schema 变更）。

**Testing**: `bazel test`（Go 单测：fake collection；JS 单测：vi.fn DI doubles）；vitest（web 组件 + api 客户端）；testplan skill（`guitar run`，gateway HTTP 大型测试，宪法 VI）。

**Target Platform**: Linux 容器服务（memory 服务二进制内变更；agent_v2 宿主 JS 服务；web 静态托管 + gateway `/api/v1`）。

**Project Type**: web-service（gRPC + REST gateway）增量修订 + agent 插件库 + web 前端视图。

**Performance Goals**: 无硬性延迟验收阈值；检索以 bigram `$all` 索引前置过滤约束候选集（个人规模 10²–10⁴ 条），web 视图请求为常规 REST 往返。

**Constraints**: 自托管 mongo 7.0 社区版；title ≤ 128 rune / content ≤ 64KB（wiki，不变）；memory content ≤ 1024 rune（新增）；工具结果永不为抛出的错误（文本结果约定）；web 无新增 CORS 面（同源相对路径）。

**Scale/Scope**: template 封闭集当前仅 `saolei`；单 template wiki 集合无上限（知识库定位，无快照注入、零上下文成本）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁（宪法原则） | 状态 | 说明 |
|---|---|---|
| 1. 文档阅读门禁（V） | ✅ 计划于 tasks.md 落实 | 每 phase 三分类文档清单（style/、官方文档 URL、survey+research+contracts），规划时逐一阅读 |
| 2. 实现门禁（II/III） | ✅ | 接口先行：`contracts/wiki-service.md`、`contracts/memory-content-limit.md`、`contracts/dsh-plugins.md`、`contracts/web-wiki-view.md` + data-model 已先于实现产出；重构式：作用域提升是对 068 资源模型的架构级修订（非补丁），proto/存储/工具面/测试同批收敛到 template 终态，无过渡态 |
| 3. 编译 + 单测门禁（IV） | ✅ | 每次 `bazel build`/`bazel test`（相关 target）为开发任务一部分，不单列 task |
| 4. 引用、图表与终态门禁（I/VII/VIII） | ✅ | 产出带仓库相对路径/URL 引用；clean break 无演进残留（旧索引/旧字段/旧路由清除，契约只述终态）；图表 Mermaid |
| 5. 大型测试验收门禁（VI） | ✅ | wiki/memory REST 面走 testplan 实际执行（部署→测试→清理闭环、全用例通过）；web 视图无浏览器执行面，UI 逻辑由 vitest 组件测试覆盖、REST 闭环由 wiki_test.go 覆盖（裁断记录于 research.md R8） |

**Phase 1 设计后复核**：四份契约 + data-model + quickstart 已产出且接口设计先于实现（门禁 2）；无未决 NEEDS CLARIFICATION（spec 四项裁定已回落）。**通过**。

## Project Structure

### Documentation (this feature)

```text
specs/069-wiki-template-web/
├── plan.md                        # 本文件
├── research.md                    # Phase 0 输出（R1–R8 决策）
├── data-model.md                  # Phase 1 输出（资源/存储/工具签名/web 投影）
├── quickstart.md                  # Phase 1 输出（验证指南）
├── checklists/
│   └── requirements.md            # spec 质量检查清单
├── contracts/
│   ├── wiki-service.md            # template 级 WikiService 契约（取代 068 同名）
│   ├── memory-content-limit.md    # memory content 1024 rune 上限契约
│   ├── dsh-plugins.md             # memory 插件修订（作用域收敛/预检/指引措辞）
│   └── web-wiki-view.md           # web wiki 管理视图契约
└── tasks.md                       # Phase 2 输出（由 /speckit.tasks 生成）
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                        # Wiki pattern/parent/HTTP 注解 → template 级；Memory content 上限注释
├── BUILD.bazel                       # gazelle 再生成（proto codegen 消息内变更）
├── memory/
│   ├── handler/wiki_handler.go       # 父解析 ParseSessionName → ParseTemplateName（六 RPC）
│   ├── handler/handler.go            # CreateMemory/UpdateMemory 调 ValidateMemoryContent
│   ├── domain/wiki.go                # Wiki 结构去 SessionID；作用域注释终态化
│   ├── domain/model.go               # MaxMemoryContentRunes + ValidateMemoryContent
│   ├── domain/wiki_repository.go     # 仓储接口签名去 session 维度
│   └── runtime/mongo/wiki_repository.go  # 三个 template 维度索引 + 启动幂等清理（旧索引/旧文档）
├── gateway/cmd/main.go               # 零改动（注册不变，路由随 proto 注解变更）
├── web/frontend/src/
│   ├── api/wiki.ts                   # 新增：template 级 wiki REST 客户端
│   ├── components/WikiView.tsx       # 新增：wiki 管理视图（PresetsView 模式）
│   └── App.tsx                       # AppView 增加 'wiki' + 侧栏第三切换 + 主区渲染
└── testplan/
    ├── wiki_test.go                  # 修订：template 级路由全面更新
    └── memory_test.go                # 增例：content > 1024 rune → 400
common/js/dsh-plugins/
├── memory-service/src/
│   ├── wiki-client.ts                # WikiStore/name 构造解析去 session 参数
│   ├── wiki-operations.ts            # wikiSearch/Get/Write 核去 session 参数
│   ├── operations.ts                 # memory 预检：MEMORY_CONTENT_MAX_RUNES + 领域文本
│   └── service.ts                    # wiki 方法仅转发 scope.template
└── memory/src/
    └── guidance.ts                   # 分工段：上限句 + wiki 跨会话持久措辞
```

（各 `*_test.go`/`*.test.ts(x)` 随实现面同步修订/新增，未在上图展开。）

**Structure Decision**: 全部落入既有目录与分层（AIP 服务分层 handler/domain/runtime、插件两包结构、web frontend api/components 划分），不新增顶层结构；gateway 与 agent_v2 编排零改动。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违例需要论证：三部分均为既有基础设施上的收敛或增量——作用域提升是收缩性变更（减一个作用域维度），memory 上限是一处校验 + 一处预检，web 视图复用 PresetsView 全部交互语汇；最大新增面是 WikiView 单组件与 wiki.ts 单客户端。
