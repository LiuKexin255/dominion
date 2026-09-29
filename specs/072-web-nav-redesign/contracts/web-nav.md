# Contract: 侧栏两级导航（☰ 图标菜单 + 左栏会话直达）

**Feature**: specs/072-web-nav-redesign/spec.md（US1/US2；裁定 Session 2026-09-29：D1=方案 A 官方对齐、D2/D3/D6 按建议、方案 B rejected——`design.md` §8）| **改动面**: `src/App.tsx` 侧栏 `view-switch` 区 + `src/theme.css`
**取代声明**: 本文取代 [`specs/071-memory-preset-scope/contracts/web-nav.md`](../../071-memory-preset-scope/contracts/web-nav.md)（071 版的两级结构与行为语义在本文延续，入口形态/实现载体/选中态呈现/左栏交互按 072 终态为准）。

## 1. 结构（两级）

- **一级（侧栏底部，行数恒定）**：`sessions`、`presets` 两个**文本**直达按钮 + 一个 **☰ 图标菜单入口按钮**。一级不平铺 wiki/memory；入口按钮为图标-only（primitives `Button size="sm"` + 内联三横杠 SVG，无 children——任意侧栏宽度下不换行）。
- **二级（primitives `Menu`，`side="top"` 自底向上展开）**：入口点击弹出，条目经 `MenuEntry` 分组承载：

```ts
// 新类别 = 追加一个 {type:'label'} 段 + 若干 item；入口与侧栏底部行数不变。
[
  { type: 'label', id: 'g-knowledge', text: '知识' },
  { id: 'wiki', label: 'Wiki' },
  { id: 'memory', label: 'Memory' },
]
```

- 菜单为 in-place 模式（非 portal）：`view-switch` 不处滚动容器内，无 overflow 裁剪；定位与卡片样式由 primitives `Menu`（`.root` relative + `.sideTop` 向上展开）提供，侧栏流内仅 Menu 的锚定 wrapper 占位——底部行数不随二级项数量增长。
- **左栏（SessionList）恒为会话导航**：非 session 视图不改变左栏内容（对齐官方 dsh webUI 左栏模式，`specs/072-web-nav-redesign/research.md` §2.2）；presets/wiki/memory 管理面板自带各自列表/选择器。

## 2. 行为

| 交互 | 行为 |
|---|---|
| 点击一级直达按钮（sessions/presets） | 切换主区视图；菜单若开着则关闭 |
| 点击 ☰ 入口 | 开/闭菜单（toggle） |
| 点击菜单内视图项（wiki/memory） | 切换主区视图 + 关闭菜单 |
| 菜单开启时点击菜单外部 / Esc | 仅关闭菜单，**不**改变当前视图（`Menu.onClose` 内建） |
| **点击左栏 session 条目（任意视图下）** | **切回 sessions 视图 + 选中并打开该会话**（`openSession` 置 `view='sessions'`；对齐官方 "`openSession` … returns the main area to Conversation"，`specs/072-web-nav-redesign/research.md` §2.2）——非 session 视图下不存在无可见响应的点击 |

菜单开闭为纯前端瞬态，键盘漫游（↑/↓/Home/End）由 primitives `Menu` 内建；App 不自维护外点/Esc 监听。

## 3. 状态与选中态

- `App` 保留瞬态 `knowledgeMenuOpen: boolean`（菜单开闭）；**不**承载视图状态。
- `view: 'sessions' | 'presets' | 'wiki' | 'memory'` 语义不变。选中态呈现：
  - 直达按钮：当前视图高亮（不变）。
  - ☰ 入口：wiki/memory（或未来二级项）激活时 `variant="primary"` + `data-active="true"`；**不拼接视图名文本**（图标-only，无两行问题）。
  - 菜单内当前二级视图项经 `Menu` 的 `selectedId={knowledgeView ? view : undefined}` 呈现选中标记（非二级视图激活时不设选中项）——"当前在哪个二级视图"在打开菜单时可见。
- 菜单关闭后当前视图与选中态保持，不回退默认视图。
- **SessionList 选中高亮在非 session 视图下保留**（D6 裁定）：点击条目即切回会话视图，高亮是"当前会话"指示，不存在高亮与主区不一致的停留态。
- 主区互斥渲染语义不变；会话面板常驻挂载、CSS 隐藏的既有行为不变（`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §2）——`openSession` 切回 sessions 视图后 `.view-pane` 取消隐藏、既有状态直接呈现。

## 4. 样式与可访问性

- 删除 071 手写浮层样式（`.view-menu`/`.knowledge-menu`/`.knowledge-menu-item`）；卡片、层级、键盘焦点样式由 primitives `Menu.module.css` 提供。`theme.css` 仅保留 `--app-*` 与布局样式（该文件既有职责）。
- 入口按钮可访问名：`title="更多视图"` + `aria-label="更多视图"`（图标-only 必须有可访问名，SessionList `IconButton` 先例）；`aria-haspopup="menu"` + `aria-expanded={knowledgeMenuOpen}`（WAI-ARIA APG menu-button，https://www.w3.org/WAI/ARIA/apg/patterns/menu-button/ ）。
- 内联三横杠 SVG：viewBox `0 0 16 16`、`stroke="currentColor"`、`aria-hidden`（形态对齐 primitives 图标实现，`lib/index.js` IconArtwork 同款 svg 属性）；primitives 图标集无汉堡 glyph（`lib/types/icons/index.d.ts`），内联 SVG 为 `specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §1 允许的载体。
- `data-testid`：入口 `view-more`；菜单条目无 testid 通道（`MenuItem` 数据行），测试经 `getByRole('menuitem', { name })` 定位（`SessionList.test.tsx:127` 先例）；`view-switch`/`view-sessions`/`view-presets` 与各视图容器 testid（`presets-view`/`wiki-view`/`memory-view`）不变。

## 5. 验收锚点

1. vitest：一级 = 2 文本直达 + 1 图标入口；入口有 `aria-label`/`aria-haspopup`/`aria-expanded` 且**无文本子节点**（不换行回归防护）；点入口弹菜单（分组 label「知识」+ wiki/memory 项经 role 查询可达）；点 wiki/memory 切视图且菜单关闭；外点/Esc 仅关闭不改视图；关闭后入口 `data-active` 与当前视图保持。
2. vitest：presets/wiki/memory 视图下点击 `session-item` → `view-sessions` 激活、目标会话面板呈现、原管理视图不渲染（`openSession` 跨视图切换，三个视图各一断言路径）。
3. vitest：四视图互斥渲染、会话面板常驻挂载（切换返回不丢在途状态）、既有 sessions/presets 直达行为零回归。
4. 结构断言：`view-switch` 子元素数恒定（3）；二级项渲染于 Menu 列表内而非侧栏流内。
