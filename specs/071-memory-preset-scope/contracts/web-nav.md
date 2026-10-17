# Contract: 侧栏两级导航（浮出菜单）

> **[SUPERSEDED — see Feature 072]**: [`specs/072-web-nav-redesign/contracts/web-nav.md`](../../072-web-nav-redesign/contracts/web-nav.md) supersedes 本文——入口由「知识」文本按钮改为 ☰ 图标菜单（primitives `Menu`，分组可扩展）、选中态呈现与左栏 session 条目跨视图交互按 072 终态为准；两级结构与"浮层关闭不改当前视图"语义延续。

**Feature**: specs/071-memory-preset-scope/spec.md（US5/FR-015；Clarifications Session 2026-09-28 Q2 裁定 popover）| **改动面**: `src/App.tsx` 侧栏 `view-switch` 区 + `src/theme.css`

## 1. 结构（两级）

- **一级（侧栏底部，行数恒定）**：`sessions`、`presets` 两个直达视图按钮 + 一个固定的二级菜单入口按钮（入口文案「知识」，plan 定）。一级不再平铺 wiki/memory。
- **二级（浮出菜单 popover）**：入口点击弹出，承载 `wiki`、`memory` 两个视图项。浮层不占侧栏布局空间（绝对定位/浮层），侧栏底部行数不随视图数量增长。

## 2. 行为

| 交互 | 行为 |
|---|---|
| 点击一级直达按钮（sessions/presets） | 切换主区视图；浮层若开着则关闭 |
| 点击二级入口 | 开/闭浮层（toggle） |
| 点击浮层内视图项（wiki/memory） | 切换主区视图 + 关闭浮层 |
| 浮层开启时点击浮层外部 | 仅关闭浮层，**不**改变当前视图 |
| Esc（如实现） | 关闭浮层，不改当前视图 |

## 3. 状态与选中态

- `App` 新增瞬态 `knowledgeMenuOpen: boolean`（浮层开闭）；**不**承载视图状态。
- `view: 'sessions' | 'presets' | 'wiki' | 'memory'`（扩展）；当前视图选中态：直达按钮高亮，或（wiki/memory 时）入口按钮呈现当前选中视图标识。
- 浮层关闭后当前视图与选中态保持，不回退默认视图。
- 主区互斥渲染语义不变（同一时刻至多一个管理视图渲染）；会话面板常驻挂载、CSS 隐藏的既有行为不变（`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md §2`）。

## 4. 样式与可访问性

- `theme.css` 补充浮层定位/层级样式（沿用 dsh-theme 变量与 presets-* 管理视图形态）；`data-testid` 命名延续既有模式（`view-switch`、`view-sessions` 等 + 菜单/入口新 testid）。
- 菜单按钮 `aria-expanded`/`aria-haspopup`（如 primitives 支持）。

## 5. 验收锚点

1. vitest：一级仅 2 直达按钮 + 1 入口；点入口弹浮层；点 wiki/memory 切视图且浮层关闭；点浮层外部仅关闭不改视图；关闭后选中态保持。
2. vitest：四视图互斥渲染（切换后其他管理视图不渲染）；既有 sessions/presets 直达行为零回归。
3. 侧栏底部 DOM 行数在视图增至 >2 个二级项时保持恒定（结构断言：二级项渲染于浮层内而非侧栏流内）。
