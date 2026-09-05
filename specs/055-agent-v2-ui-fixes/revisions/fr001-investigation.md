# FR-001 排查记录：思考输出期间 step 卡片空白（T007）

**Feature**: `specs/055-agent-v2-ui-fixes/spec.md`（FR-001 / US1 / T007 排查回路）
**日期**: 2026-09-05（部署验证反馈）
**结论**: 已定位并修复；待部署环境复验（见"待验证项"）。

## 现象

部署环境用户实测（session_id: `7556a80de1f851381d98ec5c4ec356cb`，2026-09-05 部署验证反馈）：

1. 思考过程输出期间，step 卡片完全空白无任何内容。
2. 刷新页面后（历史回填）该 step 仍空白。
3. 思考完成后 message 正文正常输出。

## 根因

`contain: size` containment 与本地 fit-content 卡片布局不相容：

- `.msg-agent`（step 卡片容器，`projects/game/web/frontend/src/theme.css` 的 `.msg-agent` 规则）是 `align-self: flex-start; max-width: 80%` 的 **fit-content 宽度**卡片——卡片宽度由子元素 intrinsic 宽度决定（本地自有卡片设计，无上游对应）。
- T005 落地的折叠围栏 `.reasoning-row:not([data-expanded]) { contain: size layout; height: 24px; }` 中，`contain: size` 使 `.reasoning-row` 按"无内容"计算 intrinsic 尺寸 → 对卡片宽度的贡献为 **0**。
- 思考输出期间的 step 只含 THINK 块（ReasoningRow 是唯一子元素）→ 卡片 fit-content 宽度塌缩为 0（仅剩 padding）→ 整卡不可见即"空白"。TEXT 块到达后其内容宽度撑起卡片 → 正文正常。刷新后历史首个 step 仍只含 THINK → 依旧空白。三个症状全部由此解释。
- 上游同款 `contain: size layout`（[ReasoningRow.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.module.css)）安全的原因：dsh-web 消息流是全宽 block 布局，宽度不依赖子元素 intrinsic 尺寸；本地 fit-content 气泡卡片与 `size` containment 不相容。

## 修复

- `projects/game/web/frontend/src/theme.css`：`.reasoning-row:not([data-expanded])` 的 `contain: size layout` → `contain: layout`，保留 `height: 24px` 垂直围栏（FR-001 防纵向布局逃逸由固定 height 保证；`contain: layout` 提供独立格式化上下文）。
- 断言同步：`projects/game/web/frontend/src/theme-fence.test.ts` 围栏断言改为 `contain: layout` + `height: 24px`。
- 契约/文档终态同步：`specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md` §2.1、`specs/055-agent-v2-ui-fixes/data-model.md` §2、`specs/055-agent-v2-ui-fixes/research.md` §1.2（F1 修复行）。
- DOM 结构与组件逻辑不变（ReasoningRow.tsx / ChatView.tsx 无改动）；组件级测试断言全部有效。
- 验证：`bazel test //projects/game/web/frontend:lib_test` 全绿。

## 待验证项（部署环境，待用户复验）

1. 本缺陷修复确认：思考输出期间 step 卡片可见（折叠摘要行呈现），刷新后历史 THINK-only step 同样可见。
2. FR-001 三场景窗口滚动条检查（spec Clarifications 2026-09-05 Q2=A，人工验证记录闭合）：思考折叠态 / 思考展开态 / 正文输出，全程窗口无垂直滚动条、页面整体不可滚动。
3. 跟随交互：贴底跟随、上滚不被拉回、非贴底"回到底部"按钮呈现与点击回底、发新消息回底。
4. `specs/055-agent-v2-ui-fixes/research.md` §2.3 隔离实验 A（禁用 sweep 动画）/ B（核查部署构建裁剪生效）仅在窗口滚动条仍复现时执行。
