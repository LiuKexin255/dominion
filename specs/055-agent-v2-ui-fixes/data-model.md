# Data Model: 055 agent-v2 界面可用性修复 + dsh-web 交互对齐

**Feature**: `specs/055-agent-v2-ui-fixes/spec.md`

本 feature 不改变任何持久化数据形状与服务契约（纯前端交互/样式 + desktop 列表加载时机）。本文档固化**行为状态机与样式变量映射**——它们是本次变更的"数据"。

## 1. 消息区跟随滚动（Follow Scroll）状态机

实体：`atBottom: boolean`（ChatView 内部状态，随组件生命周期，不持久化）。

| 当前态 | 事件 | 迁移动作 | 次态 |
| --- | --- | --- | --- |
| any | scroll（用户或程序滚动） | 重算 `scrollHeight - scrollTop - clientHeight <= 24`（阈值 R7，上游 `FOLLOW_THRESHOLD = 24`） | 贴底→true / 离底→false |
| true | 内容增长（history/live/queue/error 变更） | `scrollTop = scrollHeight`（贴底跟随） | true |
| false | 内容增长 | 不滚动（阅读位置保持） | false |
| any | 用户发送新消息（submit） | `scrollTop = scrollHeight` 并置贴底 | true |
| false | 点击"回到底部"按钮 | `scrollTop = scrollHeight` 并置贴底 | true |
| true | —— | "回到底部"按钮不渲染 | true |

不变量：

- 回底按钮的渲染条件 = `!atBottom`，与回合状态（live 运行中/结束）无关（spec Edge Cases"回底入口的存在条件"）。
- 所有滚动只作用于消息区容器；文档/窗口不产生滚动（FR-001，由布局围栏保证，人工验证闭合）。

## 2. ReasoningRow 折叠行状态

实体：`expanded: boolean`（本地 UI 态）× `running: boolean`（流式尾块判定，来自 store）。

- 根节点 `data-expanded={expanded || undefined}`、`data-state={running ? 'running' : 'ok'}`（对齐上游）。
- 样式契约：`.reasoning-row:not([data-expanded])` 施加 `contain: size layout; height: 24px`——折叠态盒尺寸与内容解耦（上游对齐义务）；展开态随 thinkBody 自然增长。FR-001 的根因（用户观察：折叠/展开两态均出现窗口滚动条）以部署环境复现排查为准（research.md §2.3 排查协议），围栏是候选修复之一而非预设结论。
- 折叠摘要：双层 `summary > summaryText`；`running` 时 `data-follow-end` → 右对齐露出最新行末尾（纯 CSS，由行容器 `overflow: hidden` 裁剪）；非 running 显示首行 + ellipsis。
- `running` 时扫描动画 `::after` 渐变色 = `color-mix(in srgb, var(--dsw-alias-bg-base) 60%, transparent)`（主题感知，对齐上游）。

## 3. 状态横幅配色映射（theme.css app 级变量，R1）

| 变量 | 解析 | 用于 |
| --- | --- | --- |
| `--app-banner-error-bg` | `var(--dsw-static-red-900)` `#570c0c` | `.chat-error` / `.presets-error` / `.agent-panel-error` 背景 |
| `--app-banner-error-fg` | `var(--dsw-alias-state-error-primary)` `#f25a5a` | 同上文字（对比度 ≈5.0:1） |
| `--app-banner-warn-bg` | `var(--dsw-alias-state-warn-tertiary)` `#27241f` | `.chat-canceled` 背景 |
| `--app-banner-warn-fg` | `var(--dsw-alias-state-warn-primary)` `#f59e0b` | 同上文字（对比度 ≈6.9:1） |

约束：值必须解析到 vendored token 表（`src/dsh-theme/design-platform.css`）中已定义的 token；对比度下限 4.5:1 由 `src/theme-contrast.test.ts` 以 `?raw` CSS 解析断言（R2）。透明底 + 前景色用法（`.queue-chip`/`.agent-guide`/`.desktop-conn` 等）不在此映射内、不改动。

## 4. desktop 列表刷新状态（App.svelte）

既有状态不变（`sessions`/`loading`/`error`/`page`/`selectedSession`），仅新增一条迁移：

| 当前态 | 事件 | 迁移动作 | 次态 |
| --- | --- | --- | --- |
| page='session' | 用户点击"← Back to Sessions"（handleBackToSessions） | 断开连接（既有）→ `page = 'sessions'` → **追加 `void handleRefresh()`** | page='sessions'，列表异步刷新中（loading=true → 落地/失败按既有语义） |

不变量：`handleRefresh` 只写 `sessions`/`loading`/`error`，不触碰 `selectedSession`/`page`（刷新落地不干扰用户后续操作）；刷新失败不清空已呈现列表（既有 catch 语义）。
