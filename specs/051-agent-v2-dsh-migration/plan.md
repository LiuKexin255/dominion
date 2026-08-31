# Implementation Plan: Game Agent v2 — dsh 迁移 Step 2：游戏 agent 迁移与 desktop 退化

**Branch**: `051-agent-v2-dsh-migration` | **Date**: 2026-08-31 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/051-agent-v2-dsh-migration/spec.md`

## Summary

在 049 交付的 agent-v2（dsh 嵌入 + GLM Responses + 零工具对话）与 web（session 管理 + 对话页）之上，完成 dsh 迁移第二步：

1. **saolei-loop 替换官方 agent-loop**：agent-v2 组合清单从 spine 单行改为直组核心件（官方 spine 硬挂载 AgentLoop 且 `AgentRegistry.setFactory` 禁止二次注册——spec Motivation 实证约束），自研 loop 插件提供 AgentFactory，持有每 session 游戏状态/历史并经桌面桥接下发操作（调研 `survey/deepseek-harness-agent-loop-prereq.md` §4.7 继承清单）。
2. **三个新 dsh 插件**：`desktop-bridge`（desktop 双向 flow 控制流，gateway `/api/v2` WS → proxy 定向 → 插件 gRPC 面）、`saolei-loop`（loop + 游戏状态）、`saolei`（init/operate/remain 三工具 + 配套 prompt section，替代 v1 saolei MCP + SKILL.md）。
3. **preset 资源与 agent 单例 API**（Q2 裁定）：prompt 服务 TeamProfile 编辑迁移为 agent-v2 的 preset 标准 CRUD（Mongo 持久化）+ web 编辑界面；agent 为 session 单例资源，经 Update（allow_missing）显式物化（preset 必填、模型可选并校验），refresh 用例并入 Update；Send 不再懒物化；Dispose 移除；ListHistory 改标准 List。
4. **desktop 退化为 flow 控制终端**：移除 session 管理/preset 编辑/对话界面（含 chatstream SSE 子系统），保留连接（改连 `/api/v2` flow 流）/绑定/执行/确认抽屉，session 选择改为只读。
5. **web 侧栏四项交互优化**（标题单行/图标按钮/`···` 删除菜单/长名虚化 + 悬停滚动）+ preset 管理视图 + agent 物化面板 + 工具调用块结果呈现（`tool_result` 事件扩展）。
6. **v1 链路处置**（Q1 裁定）：v1 agent 与 prompt 服务自部署移除，gateway `/api/v1` team/prompt 路由与 WS 入口、proxy TeamService 转发面、相关 testplan suites 下线；memory 服务与路由不动。
7. **大型测试**（FR-020）：fake LLM + 新增 fake desktop 执行器，经 testplan skill（`guitar run`）实际执行完整部署→测试→清理闭环且全部用例通过。

## Technical Context

**Language/Version**: TypeScript（ESM，agent_v2 + dsh 插件 + web 前端 React 18）、Go（gateway/proxy/desktop/testplan/fake-llm/fake-desktop）、proto3（`projects/game/agent_v2.proto` 重塑 + `projects/game/game.proto` 帧类型复用）

**Primary Dependencies**:
- dsh 家族 0.1.1-rc.2 精确 pin（A8）：`dsh-agent`（Agent 接口/Inbox/registry）、`dsh-session`、`dsh-llm`、`dsh-system-prompt`、`dsh-tools`、`dsh-scope`、`dsh-invariants`、`dsh-llm-retry`、`cordis-plugin-timer`、`dsh-app-boot`
- workspace 包：`@dominion/dsh-llm-glm`（扩展 listModels + 工具序列化）、新增 `@dominion/dsh-desktop-bridge`、`@dominion/dsh-saolei-loop`、`@dominion/dsh-saolei`
- `@dominion/game-saolei-board`（棋盘识别，FR-015 复用）
- `mongodb`（catalog `^7.5.0`，preset 持久化）
- grpc-js + proto-loader（agent_v2 服务面）、grpc-gateway v2 + coder/websocket（gateway）、React 18 + vite + vitest + @testing-library/react（web）

**Storage**: MongoDB——preset 数据（新 db `game_agent_v2`、collection `presets`，重启不丢，FR-005）；agent/对话历史/游戏状态全部为 agent-v2 进程内存态（A2）；session 元数据（存量 `game_session`）与 memory（存量 `game_memory`）不动

**Testing**: vitest（TS 单测，每插件/每模块随交付）；Go test；bazel build/test；大型测试经 testplan skill（`guitar run <plan.yaml>`，`tools/test/guitar`），fake LLM（既有 `/v1/responses`）+ 新增 fake desktop 执行器（A9）

**Target Platform**: Linux 容器（agent-v2/gateway/proxy/web/fake-*）+ Windows desktop（wails v2，操作执行端）

**Project Type**: 多服务 web/desktop 系统（SDD/speckit）

**Performance Goals**: 无新增硬性指标；延续 049 语义（流式回合、会话间不互阻）

**Constraints**: dsh 0.x-rc 无稳定性承诺（全家族精确 pin，A8）；agent-v2 仅经 proxy owner 亲和可达（无 http 块，049 D4）；token 零泄漏（SC-006）；零外部网络依赖的大型测试

**Scale/Scope**: 单部署（game.liukexin.com）；session/preset 数量为个人工具规模；agent-v2 stateful 可多实例（owner 亲和路由）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md`（v1.4.0）逐原则核查：

| 原则 | 判定 | 说明 |
|---|---|---|
| I. 引用溯源 | ✅ | 本 plan 与 research/data-model/contracts 全部引用带仓库相对路径或完整 URL；实现产物注释沿用 049 惯例 |
| II. 重构式变更 | ✅ | v1→v2 迁移以**替换**方式进行：组合清单直组重排、ConversationService 更名 AgentService 并规范化（非打补丁）；v1 面整体下线而非共存堆叠 |
| III. 接口优先设计 | ✅ | Phase 1 交付 [contracts/agent-api.md](contracts/agent-api.md)（AgentService proto + 语义）、[contracts/desktop-bridge.md](contracts/desktop-bridge.md)（flow 桥接面）、[contracts/saolei-plugins.md](contracts/saolei-plugins.md)（插件服务接口）、[contracts/web-frontend.md](contracts/web-frontend.md)；实现前先定契约 |
| IV. 测试颗粒度 | ✅ | 编译+单测为每次变更的一部分（不单列 task）；大型测试单列为验收 task（FR-020） |
| V. 编码前阅读文档 | ✅ | tasks.md（Phase 2）按三分类格式声明每 phase 文档清单；本 plan 的 research/contracts 已实读引用源 |
| VI. 服务型应用大型测试验收 | ✅ | FR-020：经 testplan skill 实际执行 `guitar run`（部署→测试→清理闭环），全部用例通过为验收；不以 bazel build 替代 |
| VII. 终态表述 | ✅ | 交付物只表述终态；v1 代码保留仓库但部署/文档不残留半迁移状态描述 |

无未辩护违反项 → **Complexity Tracking 无需填写**。

## Project Structure

### Documentation (this feature)

```text
specs/051-agent-v2-dsh-migration/
├── plan.md              # This file
├── research.md          # Phase 0 output — 决策 D1–D16
├── data-model.md        # Phase 1 output — 实体/状态机/校验
├── quickstart.md        # Phase 1 output — 验证指引
├── contracts/
│   ├── agent-api.md         # AgentService（preset/agent/messages/send/models）
│   ├── desktop-bridge.md    # DesktopBridgeService + gateway WS + proxy 转发
│   ├── saolei-plugins.md    # 三插件包契约 + 组合清单 + loop/游戏契约
│   └── web-frontend.md      # web UI 契约（侧栏/preset/物化/工具结果）
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/
├── agent_v2.proto              # 重塑：ConversationService → AgentService + 新 DesktopBridgeService
├── agent_v2/
│   ├── cordis.yml              # 组合清单：spine 单行 → 直组核心件 + 三自研插件
│   ├── package.json            # + dsh 核心件 peers、mongodb、@dominion/game-saolei-board 传递
│   └── src/
│       ├── server.ts           # 注册 AgentService + DesktopBridgeService（桥接插件提供 handlers）
│       ├── session.ts          # AgentSessions → 物化管理（Update 物化/刷新；Send 前置校验）
│       ├── history.ts          # TurnCollector：tool/call+tool/result 映射、回合全局 index
│       ├── presets.ts          # (新) preset Mongo 存取 + 校验
│       └── bootstrap.ts        # mongo 连接 + graceful shutdown 顺序
├── pkg/saolei-board/           # 不变（识别库复用，FR-015）
├── gateway/cmd/main.go         # 移除 team/prompt 处理器与 v1 WS；新增 /api/v2 WS connect 入口
├── proxy/
│   ├── cmd/main.go             # 移除 TeamHandler/agent 侧 manager；新增 AgentHandler + BridgeHandler
│   └── handler/                # conversation.go → agent.go（preset 无亲和转发；Update 分配 owner）
├── desktop/                    # 移除会话管理/Profile/对话 UI + chatstream；连接改 /api/v2；session 只读选择
├── web/frontend/src/           # 侧栏四项优化；preset 管理视图；agent 物化面板；api/store 扩展
├── fake-llm/service/testdata/  # agent_v2 游戏模板（saolei 工具调用链）
├── fake-desktop/               # (新) 确定性桌面执行器测试设施（A9）
├── deploy.yaml                 # 移除 prompt + v1 agent
└── testplan/
    ├── system_test.yaml        # 修剪 v1 suites；新增 v2 game/preset/bridge suites
    └── deploy_agent_v2.yaml    # + mongo + memory + fake-desktop

common/js/dsh-plugins/
├── llm-glm/                    # 扩展：listModels() + function_call/output 序列化
├── desktop-bridge/             # (新) @dominion/dsh-desktop-bridge
├── saolei-loop/                # (新) @dominion/dsh-saolei-loop
└── saolei/                     # (新) @dominion/dsh-saolei
```

**Structure Decision**: 沿用 049 既有布局——dsh 插件为 `common/js/dsh-plugins/` workspace 包（`common/js/**` glob 已覆盖，049 glm 插件先例）；agent-v2 承载 gRPC 服务面与宿主逻辑；桌面/网关/代理改动在其既有目录内完成。无新顶层目录（fake-desktop 与 fake-llm 同级）。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违反项。

---

## 下游执行建议（供 /speckit.tasks 参考）

设计产物（research/data-model/contracts）已消除全部 NEEDS CLARIFICATION；建议 phase 划分与验证门禁（注：以下编号为设计产物交付顺序建议，与 tasks.md 的 Phase 编号不同源——tasks.md 按 user story 重新组织 phase）：

1. **Proto 重塑 + codegen**：agent_v2.proto（AgentService/DesktopBridgeService/Agent/Preset/Model/ToolResultEvent）→ Go/TS codegen + 编译门禁。
2. **dsh 插件三件 + glm 扩展**：desktop-bridge → saolei-loop → saolei（依赖顺序）；glm listModels + 工具序列化；每包 vitest。
3. **agent-v2 宿主**：组合清单直组、物化管理（Update/Send 前置）、preset Mongo 存储、TurnCollector 工具事件映射；vitest + 049 用例零回归（更名后）。
4. **proxy + gateway**：AgentHandler（preset 无亲和/Update 分配 owner）、BridgeHandler（get-or-create 亲和）、`/api/v2` WS 入口、v1 面移除；Go 单测（root-mux 路由用例更新）。
5. **web**：侧栏四项 + preset 视图 + 物化面板 + tool_result 渲染；组件测试（vitest）。
6. **desktop 退化**：移除清单见 [contracts/web-frontend.md](contracts/web-frontend.md) §5 与 research D12；连接 URL 改 `/api/v2`；Go 测试修剪。
7. **部署与测试设施**：deploy.yaml 修剪、fake-desktop、fake-llm 模板、testplan suites 重组。
8. **大型测试验收**（单列 task）：`guitar run projects/game/testplan/system_test.yaml` 全量通过（部署→测试→清理闭环，全部用例 green）。

每 phase 的必读文档清单（三分类格式）由 tasks.md 显式声明；间接引用（如 dsh 官方 README、AIP 条目、v1 SKILL.md/源码契约源）按 constitution 原则 V 由 tasks.md 各 phase 清单一次性显式列出（各 contract 文末"实现期必读"为核对源，不替代 tasks.md 的显式声明）。
