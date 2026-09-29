# Implementation Plan: web 侧栏导航重设计（☰ 图标菜单 + 左栏会话直达）

**Branch**: `072-web-nav-redesign` | **Date**: 2026-09-29 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/072-web-nav-redesign/spec.md`；调研 [research.md](research.md)、设计 [design.md](design.md)（分叉裁定 Session 2026-09-29：D1=官方对齐、D2 保留文本、D3 迁移 primitives Menu、D6 高亮保留；方案 B rejected）；终态契约 [contracts/web-nav.md](contracts/web-nav.md)。本 plan 为对已交付导航面（`specs/071-memory-preset-scope` US5 创立的两级导航 + `specs/051-agent-v2-dsh-migration` §2 会话面板基线）的增量修订。

## Summary

两点交付（均为纯前端，无服务端变更）：

1. **☰ 图标菜单入口（US1）**：侧栏底部「知识」文本入口替换为图标-only 按钮（内联三横杠 SVG——primitives 图标集无汉堡 glyph，[research.md](research.md) §2.4；`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §1 允许内联载体），根除「知识 · Wiki」两行问题；手写浮层（`div.knowledge-menu` + 外点/Esc effect）整体替换为 primitives `Menu`（in-place + `side="top"`，`MenuEntry` 分组：`{type:'label'}`「知识」组 + wiki/memory 项，`selectedId` 呈现当前二级视图，键盘漫游/外点/Esc 内建）——分组结构即"以后不只有知识这一类"的扩展形态，新类别只加 label 段，侧栏底部行数恒定；`theme.css` 删除手写浮层样式。
2. **左栏 session 条目任意视图可达（US2）**：`openSession` 语义对齐官方 dsh webUI（任何视图下点击 session 条目 → 切回 sessions 视图并打开该会话），消解"死点击"；会话面板常驻挂载/主区互斥渲染/SessionList 选中高亮保留等基线零变化；管理视图与 SessionList 组件零改动。

## Technical Context

**Language/Version**: TypeScript（React 18，vite 构建，`projects/game/web/frontend`）。

**Primary Dependencies**: `@deepseek-ai/dsh-client-ui-primitives@0.1.7-rc.2`（`pnpm-workspace.yaml:26` catalog）——本计划使用其 `Button`（`icon` prop，`lib/types/Button.d.ts`）、`Menu`/`MenuEntry`（分组标签/分隔线/`side`/`selectedId`/`onClose`，`lib/types/Menu.d.ts`；in-place 定位见 `lib/Menu.module.css` `.root` relative + `.sideTop`）；图标集无汉堡 glyph（`lib/types/icons/index.d.ts`）→ 内联 SVG（svg 属性形态对齐包内 IconArtwork 实现：viewBox `0 0 16 16`、`stroke="currentColor"`、`aria-hidden`）。无新增依赖。

**Storage**: 无（纯前端导航；无服务端/存储面）。

**Testing**: vitest + React Testing Library（组件测试，`App.test.tsx` 既有 fetch mock/断言形态延续）；菜单条目断言经 `getByRole('menuitem', { name })`（`MenuItem` 数据行无 testid 通道；`projects/game/web/frontend/src/components/SessionList.test.tsx:127` 先例）；`bazel test` 为执行入口。

**Target Platform**: web 静态托管（gateway 同源）；无路由库，单页 state 切换（现状）。

**Project Type**: web 前端增量修订（`projects/game/web/frontend`）。

**Performance Goals**: 无新增性能面（菜单渲染为条件挂载，与 071 手写浮层同阶）。

**Constraints**: 会话面板常驻挂载与主区互斥渲染不变量（[contracts/web-nav.md](contracts/web-nav.md) §3）；侧栏底部行数恒定；无新依赖；`App.tsx` 同文件任务串行（AGENTS.md 同文件编辑串行约束）。

**Scale/Scope**: 改动面 = `App.tsx`（入口/菜单/openSession）+ `theme.css`（删浮层样式）+ `App.test.tsx`（断言改造 + 新用例）；SessionList/管理视图组件及测试零改动。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁（宪法原则） | 状态 | 说明 |
|---|---|---|
| 1. 文档阅读门禁（V） | ✅ 计划于 tasks.md 落实 | 每 phase 三分类文档清单（style/javascript.md 及其引用的 Google TypeScript Style、React/Vitest/RTL/primitives 官方文档 URL、072 design/research/契约 + 基线契约），规划时已逐一阅读确认含实际内容 |
| 2. 实现门禁（II/III） | ✅ | 接口先行：[contracts/web-nav.md](contracts/web-nav.md)（终态契约，取代 071 版）先于实现产出；重构式：手写浮层 + 自维护关闭监听被 primitives `Menu` 整体替换（删除而非叠加，无兼容层）；`openSession` 为行为语义修订（对齐官方），非补丁 |
| 3. 编译 + 单测门禁（IV） | ✅ | 每次 `bazel build`/`bazel test`（相关 target）为开发任务一部分，不单列 task |
| 4. 引用、图表与终态门禁（I/VII/VIII） | ✅ | 产出带仓库相对路径/URL 引用；终态表述（071 旧契约加 SUPERSEDED 指针而非双契约并存；rejected 方案仅 design.md §8 简记）；图表 Mermaid（design.md §4、spec.md US1） |
| 5. 大型测试验收门禁（VI） | ✅ 豁免（纯前端） | 无服务端/REST/gRPC 面变更；UI 逻辑由 vitest 组件测试覆盖，豁免口径对齐 `specs/069-wiki-template-web/research.md` R8（web 无浏览器执行面）；前端为静态资产，既有 testplan suite 行为零影响 |

**Phase 1 设计后复核**：契约/设计/调研/spec 已产出且分叉全部裁定（无 NEEDS CLARIFICATION）；改动文件与测试影响面已收敛（见下）。**通过**。

## Project Structure

### Documentation (this feature)

```text
specs/072-web-nav-redesign/
├── plan.md              # This file
├── research.md          # 调研（官方 webUI + 社区模式 + primitives 能力面）
├── design.md            # 设计（终态 + 裁定记录 + rejected alternatives）
├── spec.md              # 用户故事/需求/成功标准
├── contracts/
│   └── web-nav.md       # 终态契约（取代 specs/071-memory-preset-scope/contracts/web-nav.md）
└── tasks.md             # Phase 2 output（本 plan 同批产出）
```

### Source Code (repository root)

```text
projects/game/web/frontend/src/
├── App.tsx              # ☰ 入口 + primitives Menu 迁移（删手写浮层/effect）；
│                        #   openSession 跨视图切换（US2）
├── App.test.tsx         # 导航用例断言改造 + 跨视图新用例
└── theme.css            # 删除 .view-menu/.knowledge-menu/.knowledge-menu-item 手写浮层样式
```

**Structure Decision**: 仓库既有 web 前端内增量修订；`SessionList.tsx`、三个管理视图组件（`PresetsView`/`WikiView`/`MemoryView`）及其测试零改动。071 旧契约头部的 `[SUPERSEDED — see Feature 072]` 指针已随本 plan 落定（031→040 supersede 先例格式）。

## Complexity Tracking

> 无宪法门禁违例，无需记录。
