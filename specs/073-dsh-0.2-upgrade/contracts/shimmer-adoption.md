# Contract: ReasoningRow shimmer 采用（UX 契约）

**Feature**: specs/073-dsh-0.2-upgrade/spec.md（FR-013、US3 场景 3/4、Edge Cases「shimmer 动画的无障碍与降级」）
**依据**: specs/073-dsh-0.2-upgrade/research.md D5；组件现状 `projects/game/web/frontend/src/components/ReasoningRow.tsx`

## 1. 采用点（唯一组件）

`src/components/ReasoningRow.tsx`——ChatView 流式回合 THINK 尾块的渲染组件（`ChatView.tsx` AgentStep 以 `running={running && i === blocks.length - 1}` 驱动）。

### 1.1 折叠摘要 shimmer

- 现状：`.reasoning-summary-text` 渲染 `summary`（running 时 `latestLine(text)`，否则 `firstLine(text)`）。
- 采用：摘要文本以 `<TextShimmer active={running}>` 包裹（0.2.0 TextShimmer：`children` 为纯文字、`active` 控制共享高光动画、inactive 文字保持同一节点可选中）。
- 不变量：`data-follow-end`（running 态 CSS 跟尾露出最新行末尾）机制保留——滚动语义与动画语义正交；TextShimmer 实例不包裹图标与分隔符（`.reasoning-separator` 留在外层，对齐组件契约 "Keep icons outside"）。

### 1.2 DisclosureRow 头部动画

- 采用：向 DisclosureRow 传 `running={running}`（0.1.7 已有 prop、此前未使用；0.2.0 语义为"animate the complete header"，CSS 随包演进）。

## 2. 状态契约

| 状态 | 折叠摘要 | DisclosureRow 头部 | 无障碍 |
|---|---|---|---|
| `running` | TextShimmer `active`（高光动画，`--dsw-alias-label-shimmer`） | `running` 动画 | `visually-hidden`「正在生成」**保留**（进行中语义不得仅由动画承载） |
| `ok`（完结/历史） | TextShimmer 非 active，文字节点保留、可选中 | 无动画 | — |

- 状态切换不重挂载摘要文本节点（"inactive text keeps the same node"）。
- 布局稳定性：动画装饰层（decoration）为 inert/clipped，不改变摘要行布局；回合完结后内容与完结前一致。

## 3. 明确不采用（defer，research.md D7）

- TEXT 尾块（MarkdownText 流式正文）不加 shimmer——markdown 内容不符合 TextShimmer 契约，且 `streaming` 已表达流式语义。
- MenuGroup / Tooltip `openOnClick` / turn-trigger / switch / 文档选区 token——无承载面，不引入任何承载面改动（spec US3 场景 4）。

## 4. 测试断言（`src/components/ReasoningRow.test.tsx` 增补）

1. running 态：渲染 TextShimmer 消费 `active` 语义（jsdom 无动画帧，断言 DOM 结构/属性，不断言动画帧）；visually-hidden「正在生成」在场。
2. ok 态（历史/完结）：文字内容完整呈现且非 active；文本节点在状态切换间保留（不重挂载）。
3. 既有用例全部保持（摘要行选择逻辑 latestLine/firstLine、展开体 MarkdownText streaming、空文本不渲染）——采用不得破坏既有断言语义。
