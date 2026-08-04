# Contract: Desktop 消息气泡对齐修复

**Feature**: `036-team-mode-bugfix` | **Spec**: [`spec.md`](../spec.md) | **Research**: D4

> Issue 3 前端修复契约。描述 ChatView.svelte 中用户消息气泡右对齐的 CSS 变更。

---

## 1. 问题

`ChatView.svelte:274-276` 对非 agent 文本/thinking 消息外包了一层 `.msg-row`（无 `msg-user` 对齐类），ChatMessage 内部又渲染自己的 `.msg-row.msg-user`。

```svelte
<!-- 当前（有 bug） -->
<div class="msg-row" class:msg-pending={item.pending}>
  <ChatMessage part={item.part} role={item.role} timestamp={item.timestamp} />
</div>
```

- `.chat-thread` 是 flex column 容器，`.msg-row` 是其直接子项。
- 外层 `.msg-row` 的 `display: flex; justify-content: flex-start`（CSS 默认对齐，无 `.msg-user` 修正）。
- 内层 ChatMessage 的根 `.msg-row.msg-user`（`justify-content: flex-end`）只是外层 flex 容器的内容宽度子项 → `flex-end` 无效。

## 2. 修复

将外层 `.msg-row` wrapper 改为不设置 flex 布局的 wrapper，让 ChatMessage 自身的 `.msg-row.msg-user` 成为 `.chat-thread` 的直接 flex 子项（效果上）：

```svelte
<!-- 修复后 -->
<div class="msg-pending-wrapper" class:msg-pending={item.pending}>
  <ChatMessage part={item.part} role={item.role} timestamp={item.timestamp} />
</div>
```

`.msg-pending-wrapper` 的 CSS：

```css
/* 不设置 display: flex，让 ChatMessage 的 .msg-row 直接控制对齐 */
.msg-pending-wrapper {
  padding: 2px 12px;  /* 与原 .msg-row 的 padding 一致 */
}
```

`.msg-pending` 的 `opacity: 0.65` 样式保留（已定义于 ChatView `<style>`，`class:msg-pending` 引用）。

## 3. 影响范围

仅影响 `ChatView.svelte` 中 `kind === 'text' || kind === 'thinking'` 分支（ChatMessage 渲染路径）。

以下路径**不受影响**：
- Agent markdown 文本：使用 ChatView 自身的 `.msg-row.msg-agent`（`justify-content: flex-start`，已正确左对齐）。
- 图片消息：使用 `.msg-row.msg-image` + `msg-image-user`（已正确处理左右对齐）。
- 工具消息：使用 `.msg-row.msg-operation`（已正确左对齐）。
- 警告消息：使用 `.msg-row.msg-warn`（已正确左对齐）。

## 4. 验证

- 用户文本消息气泡靠右对齐（`.msg-row.msg-user` 的 `justify-content: flex-end` 生效）。
- Pending（排队中）用户消息仍保留 `opacity: 0.65` 视觉效果。
- Agent 消息靠左对齐不变。
