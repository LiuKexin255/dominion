# Implementation Plan: agent-v2 对话呈现与游戏链路缺陷修复 + testplan 重构

**Branch**: `054-agent-v2-bugfixes` | **Date**: 2026-09-03 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/054-agent-v2-bugfixes/spec.md`；用户追加指令（2026-09-03）：①再次调研 dsh 官方 webUI 对 agent turn 的展示与处理，优先复用/参考官方 webUI，不一致处直接指出；②按最新 `style/large_test.md` 的模块与 suite 编排重构 testplan 测试用例，将使用相同部署拓扑的模块测试用例合并到同一 suite，减少测试执行时间。

## Summary

修复 051 交付的 8 个缺陷/需求 + 重构 game testplan 编排，核心策略为**官方 webUI 复用分层**（调研结论见 [research.md](research.md) D1/D2）：

1. **对话呈现改造**（US2/3/4）：前端按官方 `dsh-client-ui-chat` 的 Turn Process Folding 模型重构——流式按 step 分段（协议扩展：ChatEvent 块事件携带 step 序号）、完成后最终答案独立+此前步骤折叠为"思考过程"区；正文/思考改用 primitives 的 `MarkdownText`（流式增量 GFM 渲染）；saolei-loop driver 的 ERROR 路径补 interrupted 固化 + 前端失败回合保留已呈现分段，消除"看过即焚"。
2. **终止能力**（US5）：AgentService 新增 `:cancel` 自定义方法（对齐官方 `IConversation.cancel()` 命令名，语义按用户裁定：排队消息落地为历史 user message，区别于官方的保留 Queue——差异已在 research D2 指出）；TurnStatus 新增 `TURN_STATUS_CANCELED` 终态；对话页 composer 区终止按钮。
3. **链路真实性与可观测**（US1）：以正式环境执行证据排查"desktop 无执行"断点（排查 playbook 见 research D10）；desktop-bridge 暴露连接状态查询，GetAgent 响应携带 `desktop_connected`，对话页呈现连接状态指示。
4. **配置面**（US6/7/8）：模型目录改为 GLM Coding Plan 实际支持模型（glm-5.3、glm-5.3-flash）；PresetsView 改独占编辑视图；引入官方 `dsh-client-ui-theme` 的 token CSS 表（`--dsw-*` 唯一色彩权威）系统性修复 Menu 等组件的视觉变量缺口。
5. **testplan 重构**（用户指令②）：deploy 合并（两个 fake-desktop 实例进一份 deploy，7 次部署 → 1 次）+ suite 归并 + 相应 binary/helper 调整，契约见 [contracts/testplan.md](contracts/testplan.md)。

## Technical Context

**Language/Version**: TypeScript（ESM，web 前端 React 18 + vite + vitest；saolei-loop/desktop-bridge/agent_v2 宿主）、Go（gateway/proxy/testplan）、proto3（`projects/game/agent_v2.proto` 扩展）

**Primary Dependencies**:
- 既有 dsh 家族 0.1.1-rc.2 精确 pin（A8 延续）：cordis 4.0.1、dsh-agent、dsh-session 等；依赖治理：dsh 依赖统一 `pnpm-workspace.yaml` catalog 管理（含 `third_party/dsh/core` 底座；rc 线精确版本、cordis/schemastery 保持 range）
- 官方 UI 复用（同版本线 0.1.1-rc.2）：`@deepseek-ai/dsh-client-ui-primitives`（`MarkdownText` 等原子，已在用）、`@deepseek-ai/dsh-client-ui-theme@0.1.1-rc.2` 的官方 token CSS 表（以源码形态 vendored 于 frontend `src/dsh-theme/`，不引入该 npm 依赖与其 cordis runtime——research D4 / revisions/phase10-theme-css-carrier.md）
- 官方行为参考（不引入代码）：`@deepseek-ai/dsh-client-ui-chat@0.1.2-rc.1` 与 `@deepseek-ai/dsh-client-ui-conversation@0.1.2-rc.1`（Turn Process Folding、`'assistant-step'` 三态、`IConversation.cancel()`——research D1/D2）
- workspace 包：`@dominion/dsh-saolei-loop`（driver ERROR 固化）、`@dominion/dsh-desktop-bridge`（连接状态查询）
- MongoDB（preset 持久化，不动）

**Storage**: 无新增存储；preset Mongo 延续；agent/历史/游戏状态维持内存态（051 A2 延续）

**Testing**: vitest（web 组件/store + TS 插件单测，每次变更必带）；Go test（gateway/proxy 如有面变更）；bazel build/test；大型测试经 testplan skill（`guitar run projects/game/testplan/system_test.yaml`）——重构后 1 suite 全量通过（constitution 原则 VI：实际执行部署→测试→清理闭环）

**Target Platform**: Linux 容器（agent-v2/gateway/proxy/web/testplan）+ Windows desktop（真实执行端，人工验证）

**Project Type**: 多服务 web 系统（SDD/speckit）

**Performance Goals**: 终止后回合停止 ≤5 秒（SC-004，组件/模块测试断言）；testplan 总执行时间显著下降（部署次数 7→1，见 contracts/testplan.md 预算）

**Constraints**: dsh 家族维持 0.1.1-rc.2 精确 pin（官方 UI 完整栈不可用性见 research D1——数据面协议与依赖生态不匹配，peer cordis ^4.0.2 与本仓库 4.0.1 冲突且 0.1.1-rc.2 线无 chat 包/无折叠特性）；token 零泄漏延续；testplan 零外部网络依赖（fake LLM + fake desktop）

**Scale/Scope**: 单部署个人工具规模（延续 051）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md`（v1.4.0）逐原则核查：

| 原则 | 判定 | 说明 |
|---|---|---|
| I. 引用溯源 | ✅ | research/data-model/contracts 全部引用带仓库相对路径或完整 URL（官方包 npm/GitHub 引证齐备）；实现产物注释沿用惯例 |
| II. 重构式变更 | ✅ | 呈现层按官方模型重构 store/组件分层（非在旧合并逻辑上打补丁）；历史固化为服务端固化语义修正（driver 统一 interrupted 语义）；testplan 为编排结构重构而非叠加；同时**拒绝**官方 UI 完整栈的过度设计（research D1 L4 否决——24+ peer 依赖生态对游戏域过度） |
| III. 接口优先设计 | ✅ | Phase 1 交付 [contracts/agent-api-changes.md](contracts/agent-api-changes.md)（/api/v2 协议变更：step 字段/:cancel/连接状态）、[contracts/web-ui.md](contracts/web-ui.md)（前端 UI 契约与官方对齐基线）、[contracts/testplan.md](contracts/testplan.md)（测试编排契约）；实现前先定契约 |
| IV. 测试颗粒度 | ✅ | 编译+单测为每次变更的一部分（不单列）；大型测试验收单列（含 testplan 重构本身的执行验证） |
| V. 编码前阅读文档 | ✅ | tasks.md 阶段按三分类格式声明每 phase 文档清单；本 plan 的 research/contracts 均基于实读的官方包源码/README（npm 包解包验证），无凭印象引用 |
| VI. 服务型应用大型测试验收 | ✅ | 修复后经 testplan skill 实际执行重构后的 `system_test.yaml`（完整部署→测试→清理闭环），全部用例通过为验收；不以 bazel build 替代 |
| VII. 终态表述 | ✅ | 交付物只表述终态（testplan 重构移除旧 deploy/suite 结构不留残留；spec/plan 中被否决的官方栈方案仅作为决策记录保留于 research） |

无未辩护违反项 → **Complexity Tracking 无需填写**。

## Project Structure

### Documentation (this feature)

```text
specs/054-agent-v2-bugfixes/
├── plan.md                      # This file
├── research.md                  # Phase 0 — 决策 D1–D12（官方 webUI 复用分层/差异清单/排查 playbook/testplan 重构依据）
├── data-model.md                # Phase 1 — ChatEvent step 扩展/TurnStatus CANCELED/固化语义/连接状态/模型目录
├── quickstart.md                # Phase 1 — 验证指引（组件/单测/真实环境端到端/testplan 执行）
├── contracts/
│   ├── agent-api-changes.md     # /api/v2 协议变更（块事件 step 字段、:cancel、GetAgent 连接状态、模型目录）
│   ├── web-ui.md                # web 前端契约（分段折叠/markdown/终止按钮/连接状态/preset 独占视图/token CSS 引入）
│   └── testplan.md              # testplan 重构契约（deploy 合并/suite 归并/binary 与 helper 调整）
└── tasks.md                     # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/
├── agent_v2.proto               # 扩展：BlockStart/Delta/End +step、TurnStatus +TURN_STATUS_CANCELED、Cancel rpc、Agent +desktop_connected
├── agent_v2/
│   ├── cordis.yml               # llm-glm models: glm-5.3 + glm-5.3-flash（目录唯一来源）
│   └── src/
│       ├── session.ts           # :cancel 实现（终止在途回合+排队落地）；DEFAULT_MODEL 改 glm-5.3
│       ├── server.ts            # Cancel handler 注册；GetAgent 响应携带连接状态
│       └── history.ts           # 块事件携带 step 序号（chunk→ChatEvent 映射扩展）
├── web/frontend/src/
│   ├── App.tsx / theme.css / dsh-theme/  # 引入官方 token CSS（vendored sheets，App.tsx import），自有布局样式保留
│   ├── store/chat.ts           # step 感知分段（不合并整回合）、ERROR/CANCELED 保留、ABORTED 语义拆分
│   ├── components/ChatView.tsx  # 分段渲染+完成后折叠（Turn Process Folding）、终止按钮、连接状态指示
│   ├── components/ReasoningRow.tsx / ToolCard.tsx  # MarkdownText、棋盘等宽呈现
│   └── components/PresetsView.tsx  # 独占编辑视图
├── testplan/
│   ├── system_test.yaml         # 7 suite → 1 suite（一次部署，cases 顺序执行）
│   ├── deploy_agent_v2.yaml     # +第二个 fake-desktop 实例（drop 场景）
│   └── deploy_agent_v2_drop.yaml  # 删除（拓扑并入主 deploy）
└── (gateway：/api/v2 透传零改动；proxy：Cancel 转发见 revisions/phase2-proxy-cancel.md)

common/js/dsh-plugins/
├── saolei-loop/src/driver.ts    # ERROR 路径 interrupted 固化（对齐 abort 路径与官方 interrupted 语义）
└── desktop-bridge/src/          # 连接状态查询面（hasConnection）
```

**Structure Decision**: 沿用既有布局，无新顶层目录；前端主题引入方式（依赖包 vs 提取 CSS 文件）在 research D4 定界、tasks 阶段按 pnpm catalog 规范落地。

---

## 下游执行建议（供 /speckit.tasks 参考）

设计产物（research/data-model/contracts）已消除全部 NEEDS CLARIFICATION。建议 phase 划分与验证门禁（tasks.md 可按 user story 重组，以下为实现依赖顺序）：

1. **Proto 扩展 + codegen**：agent_v2.proto（step 字段/CANCELED/Cancel/desktop_connected）→ Go/TS codegen + 编译门禁（无行为变更，纯扩展）。
2. **saolei-loop 固化修复**（US4 服务端半）：driver ERROR 路径 interrupted append + 单测（失败回合历史固化）。
3. **agent_v2 宿主**：history.ts 块事件 step 映射、session.ts :cancel（排队落地）、server.ts 连接状态、模型目录配置——vitest + 既有用例零回归。
4. **web 前端主体**：token CSS 引入 → store 分段/CANCELED → ChatView 折叠/markdown/终止/连接状态 → PresetsView 独占视图（vitest 组件级全覆盖）。
5. **真实环境链路排查与修复**（US1）：按 research D10 playbook 执行（signoz tracing 定位断点），产出执行证据；此 phase 需要真实 desktop + 正式环境配合。
6. **testplan 重构**：deploy 合并 + suite 归并 + binary/helper 调整；`guitar run system_test.yaml` 全量通过（部署→测试→清理闭环，全部用例 green）。
7. **最终验收**（单列 task）：组件/单测全绿 + testplan 全量执行记录 + 真实环境端到端证据（SC-001）归档。

每 phase 的必读文档清单（三分类格式）由 tasks.md 显式声明；官方包参考文档（`dsh-client-ui-chat` README 的 Turn Process Folding 章节、primitives README 的 MarkdownText 章节、ui-theme README 的 token sheets 章节）与 `style/large_test.md` 为关键间接引用，tasks.md 必须显式列出。
