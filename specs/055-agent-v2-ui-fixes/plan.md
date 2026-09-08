# Implementation Plan: 055 agent-v2 界面可用性修复 + dsh-web 交互对齐

**Branch**: `055-agent-v2-ui-fixes` | **Date**: 2026-09-05 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/055-agent-v2-ui-fixes/spec.md`

## Summary

修复 054 交付后用户报告的 4 个界面可用性缺陷（web 状态横幅文字与背景同色不可读、输出期间滚动被无条件贴底劫持、desktop 返回 session 列表不刷新、思考流式期间整个窗口出现滚动条——用户补充：折叠/展开两态均出现，根因以部署环境复现排查为准，排查协议与候选清单见 research.md §2.3），并按 2026-09-05 plan 阶段追加指令完成 dsh-web 交互对齐审计：传承组件（对话滚动、思考折叠行、折叠开关视觉）除登记的有意偏离（D1~D6）外全部对齐上游——思考行补回上游折叠态 `contain: size layout` 布局围栏、follow-end 摘要改上游纯 CSS 双层结构（删除本地 rAF/scrollLeft 机制）、扫描动画颜色主题感知化、滚动跟随改为条件跟随（`FOLLOW_THRESHOLD = 24`）并新增"回到底部"浮动入口、折叠开关换 chevron 图标旋转。状态横幅以 app 级变量映射官方深色调 token（red-900/amber-900 底 + primary 前景，对比度 ≥ 4.5:1）。布局约束以部署环境人工验证记录闭合（不引入浏览器 E2E）。审计结论与决策依据见 [research.md](./research.md)，交互契约与偏离清单见 [contracts/ui-interactions.md](./contracts/ui-interactions.md)。

## Technical Context

**Language/Version**: TypeScript（web: React 18 + vite；desktop: Svelte 5 + vite）

**Primary Dependencies**: `@deepseek-ai/dsh-client-ui-primitives`（DisclosureRow/MarkdownText/JsonBlock/StateDot/IconChevronDownOutline14 等，catalog 统一管理）；desktop 测试新增 devDep `jsdom`（catalog 已有 `^26.0.0`，research R3）

**Storage**: 无持久化变更（纯前端交互/样式 + desktop 列表加载时机；数据形状不变，见 data-model.md）

**Testing**: vitest（`bazel test //projects/game/web/frontend:lib_test`、`//projects/game/desktop/frontend:lib_test`）；对比度断言经 `?raw` CSS 解析（research R2）；desktop 用 Svelte 5 命令式 mount + 原生事件 + `vi.mock('./api')`（research R3）；布局约束人工验证（Clarifications Q2=A）

**Target Platform**: web（浏览器，深色单一主题）+ desktop（wails 桌面端）

**Project Type**: 既有 web/desktop 前端缺陷修复（无新服务、无契约变更）

**Performance Goals**: 滚动跟随不得引入逐 chunk 布局抖动（纯 scroll 事件 + 条件贴底，无 rAF 机制）；删除 ReasoningRow 的 rAF 节流机制（净减）

**Constraints**: 054 交付行为零回归（分段/折叠/markdown/终止/内容保留）；样式值必须解析到 vendored token 表（`src/dsh-theme/`，不散落硬编码）

**Scale/Scope**: 变更面——web：`ChatView.tsx`、`ReasoningRow.tsx`、`theme.css`、对应测试 + 新增 `theme-contrast.test.ts`；desktop：`App.svelte`、新增 `App.test.ts`、`package.json`（jsdom devDep）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 检查 | 结论 |
| --- | --- | --- |
| I. 引用溯源 | spec/research/contracts 均带仓库相对路径或完整 URL（上游文件链接、054/049 契约引用） | ✅ |
| II. 重构式变更 | follow-end 由"自造 rAF+scrollLeft 机制"收敛为上游纯 CSS 结构（净删除代码）；状态横幅以 app 级变量单一来源化，非逐处打补丁 | ✅ |
| III. 接口优先设计 | 无服务接口变更；UI 交互契约先行（contracts/ui-interactions.md，含测试口径） | ✅ |
| IV. 测试颗粒度 | 编译+单测随代码变更执行；无大型测试面变更（A5：054 suites 回归） | ✅ |
| V. 编码前阅读文档 | tasks 阶段按 phase 声明文档清单（三分类格式），本 plan 已核验引用文档实际内容 | ✅（tasks 落实） |
| VI. 大型测试验收 | 前端 UI 缺陷修复、不改服务行为；A5 声明以 054 既有 suites 为回归面 | ✅ |
| VII. 终态表述 | 交付物只表述终态；偏离清单为"当前状态对照"非演进记录 | ✅ |

Phase 1 后复查：设计产物（data-model/contracts/quickstart）与上述判定无冲突，通过。

## Project Structure

### Documentation (this feature)

```text
specs/055-agent-v2-ui-fixes/
├── plan.md              # 本文件
├── research.md          # Phase 0：审计清单（F1-F5/D1-D6）+ 决策 R1-R7
├── data-model.md        # Phase 1：行为状态机 + 横幅变量映射
├── quickstart.md        # Phase 1：验证指南（自动化 + 人工场景）
├── contracts/
│   └── ui-interactions.md  # Phase 1：交互契约 + 有意偏离清单（权威登记）
└── tasks.md             # Phase 2 输出（/speckit.tasks 生成）
```

### Source Code (repository root)

```text
projects/game/web/frontend/
├── src/
│   ├── components/
│   │   ├── ChatView.tsx          # 条件跟随 + 回底入口（F4，FR-002~004）
│   │   ├── ChatView.test.tsx     # 跟随/回底/回底入口行为断言
│   │   ├── ReasoningRow.tsx      # data-expanded、删 rAF/scrollLeft 机制（F1/F2）
│   │   └── ReasoningRow.test.tsx # 对齐结构断言
│   ├── theme.css                 # 围栏/follow-end/sweep 色（F1-F3）、横幅变量（R1）、
│   │                             # 回底按钮与折叠开关样式（R5/R6）
│   └── theme-contrast.test.ts    # 新增：?raw CSS 对比度断言（R2）
└── (vite/bazel 既有布局不变)

projects/game/desktop/frontend/
├── src/
│   ├── App.svelte                # handleBackToSessions 追加 handleRefresh（FR-007）
│   └── App.test.ts               # 新增：jsdom + Svelte mount 返回刷新断言（R3）
└── package.json                  # devDependencies 增加 jsdom（catalog）
```

**Structure Decision**: 全部为既有目录内变更，无新模块/包；desktop 新增测试文件与其既有 vitest 基建（`lib_test`）同套。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违规，无需论证。
