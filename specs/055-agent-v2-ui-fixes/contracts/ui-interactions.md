# Contract: web/desktop 交互契约（055）

**Feature**: `specs/055-agent-v2-ui-fixes/spec.md`
**范围**: web 对话交互（`projects/game/web/frontend`）与 desktop 列表刷新（`projects/game/desktop/frontend`）。上游对照基线为 [deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)（dsh-web）；审计叙事与决策依据见 `specs/055-agent-v2-ui-fixes/research.md`。

## §1 消息区滚动与跟随（FR-001~004）

对齐上游 [ChatView.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ChatView.tsx) / [ChatView.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ChatView.module.css)：

1. **滚动范围**：对话消息区（`.chat-messages`）是唯一垂直滚动面；任何输出阶段（思考流式含展开体、正文流式、工具卡片、排队指示、错误/终止横幅）浏览器窗口不得出现垂直滚动条、页面整体不得可滚动。
2. **条件跟随**：消息区滚动位置满足 `scrollHeight - scrollTop - clientHeight <= 24`（`FOLLOW_THRESHOLD = 24`）时视为**贴底**；仅贴底时内容增长触发 `scrollTop = scrollHeight`；非贴底时内容增长不改变滚动位置。
3. **新消息回底**：用户发送新消息（经输入区 submit）无条件将消息区滚至底部并进入贴底态。
4. **回底入口**：非贴底时在消息区右下呈现"回到底部"按钮（chevron-down 图标、34px、sticky 定位、`aria-label`、`data-testid="to-bottom-button"`）；点击回底并恢复跟随；贴底时不渲染。按钮存在性仅由滚动位置决定，与回合状态无关。

**测试口径**：§1.2/1.3/1.4 行为以组件级测试断言（jsdom 中以可赋值的 `scrollTop`/`scrollHeight`/`clientHeight` 驱动）；§1.1 布局约束以部署环境人工验证记录闭合（spec Clarifications 2026-09-05 Q2=A）。

## §2 ReasoningRow 折叠行（FR-001 布局围栏 / FR-009 对齐）

对齐上游 [ReasoningRow.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.tsx) / [ReasoningRow.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.module.css)：

1. 根节点承载 `data-expanded`（展开时存在）与 `data-state`（running/ok）；折叠态（无 `data-expanded`）样式施加 `contain: layout` 且高度固定 24px——高度围栏使折叠盒纵向尺寸与内容解耦，流式增长不影响祖先布局。与上游 [ReasoningRow.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.module.css) 的 `contain: size layout` 为有意差异：本地 `.msg-agent` 是 fit-content 宽度卡片，size containment 会把卡片 intrinsic 宽度归零使 THINK-only 卡片塌缩不可见（上游全宽 block 布局无此依赖）；根因分析见 `specs/055-agent-v2-ui-fixes/revisions/fr001-investigation.md`。
2. 折叠摘要为双层结构（`summary > summaryText`）：running 时 `data-follow-end` 以纯 CSS 右对齐露出最新行末尾（行容器 `overflow: hidden` 裁剪），非 running 显示首行 + ellipsis；**不使用任何程序化 scrollLeft/动画帧机制**。
3. running 扫描动画渐变色为 `color-mix(in srgb, var(--dsw-alias-bg-base) 60%, transparent)`（主题感知），不得硬编码颜色字面量。
4. 展开体渲染维持本地既有语义：MarkdownText（有意偏离 D1，见 §5）。

## §3 状态横幅配色（FR-005/006）

1. `theme.css` 定义并唯一来源化四个 app 级变量：`--app-banner-error-bg/-fg`、`--app-banner-warn-bg/-fg`，值必须解析到 `src/dsh-theme/design-platform.css` 已定义 token（映射表见 `specs/055-agent-v2-ui-fixes/data-model.md` §3）。
2. `.chat-error`、`.presets-error`、`.agent-panel-error` 使用 error 组；`.chat-canceled` 使用 warn 组；文字-背景对比度 ≥ 4.5:1（WCAG AA），由 `src/theme-contrast.test.ts` 以 `?raw` 样式文本解析断言。
3. 错误（红系）与警示（黄系）色系保持可区分；透明底 + 前景色用法（排队提示、引导条、连接状态等）不在本契约映射内，不改动。

## §4 折叠开关与 desktop 刷新（FR-007 / FR-009）

1. **turn-process 开关**：`IconChevronDownOutline14` 图标 + `data-open`（或 `aria-expanded` 对应属性）CSS 旋转，替代文字箭头；标签文案保留本地化口径（"思考过程（N 步骤 · M 次工具调用）"）。
2. **desktop 返回刷新**：`handleBackToSessions` 在切回 sessions 页后调用 `handleRefresh()`；刷新仅写列表/加载/错误状态，不干扰选择与页面状态；失败不清空既有列表。既有 onMount、模板切换刷新语义不变。

## §5 有意偏离清单（权威登记，FR-009）

传承组件与上游交互不一致的唯一合法状态：已对齐（§1~§4），或在本清单登记。**清单外不得存在未声明的交互偏差。**

| # | 偏离 | 上游行为 | 登记理由 |
| --- | --- | --- | --- |
| D1 | 思考展开体 MarkdownText 渲染 | 纯文本 thinkBody | `specs/054-agent-v2-bugfixes/spec.md` FR-010 要求与正文同 markdown 能力 |
| D2 | ToolCard 简化单卡（StateDot/名称/状态/JsonBlock 参数/`<pre>` 结果） | ToolRow 多变体丰富视图 | `specs/054-agent-v2-bugfixes/contracts/web-ui.md` §3 棋盘预格式化契约；上游视图对应我们不存在的工具形态 |
| D3 | 输入区单行 Input + Enter 发送 | contentEditable 富输入 composer | `specs/049-agent-v2-dsh-init/contracts/web-frontend.md` §3.2 自有设计；富输入为独立产品能力，未移植 |
| D4 | 用户消息纯文本气泡 | 附件/文件/重试倒计时呈现 | 产品能力差异（无对应消息类型） |
| D5 | 文案本地化口径（中文标签/状态文案） | i18n key 体系 | attribution 声明的 locale/slot 剥离；交互语义相同 |
| D6 | web 自有功能面（侧栏/会话列表/preset 管理/终止按钮/排队提示/连接状态） | 无上游对应 | 本产品自有功能（051/054 交付） |

**未移植（不在对齐范围，spec A6）**：contentEditable 富输入 composer、附件/文件消息、MessageIconActions、ToolRow 丰富工具视图、模型重试倒计时、TurnNavigator、turn 虚拟列表/分页。
