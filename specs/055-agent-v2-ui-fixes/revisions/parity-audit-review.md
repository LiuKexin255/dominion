# 对齐复查记录：契约 §1~§5 与代码终态逐项核对（T014 / SC-005）

**Feature**: `specs/055-agent-v2-ui-fixes/spec.md`（US4 / FR-009 / SC-005）
**日期**: 2026-09-05
**方法**: 以 `specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md`（权威偏离清单载体）逐条款对照代码实际行为；每条给出代码位置与一致/偏离结论。组件级断言引用 `projects/game/web/frontend/src` 下测试文件；布局约束引用部署环境复验记录 `specs/055-agent-v2-ui-fixes/revisions/fr001-investigation.md`。上游对照基线 [deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)。

**结论**: §1~§4 全部条款与代码终态一致；§5 D1~D6 六项偏离逐项核实与清单声明一致；清单外发现 1 项观察点，均判定为非交互语义偏差（见"清单外观察"），无需登记。**清单外无未声明的交互偏差，SC-005 满足。**

## §1 消息区滚动与跟随（FR-001~004）

| 条款 | 代码位置 | 结论 |
| --- | --- | --- |
| §1.1 滚动范围（消息区唯一垂直滚动面） | `projects/game/web/frontend/src/theme.css`：`.chat-messages`（`overflow-y: auto`）+ `html, body, #root { height: 100%; margin: 0 }` + `.app { height: 100% }` | 一致（布局面：文档无滚动容器；行为面：部署复验三场景无窗口滚动条，见 fr001-investigation.md 复验记录） |
| §1.2 条件跟随（阈值 24，仅贴底跟随） | `projects/game/web/frontend/src/components/ChatView.tsx`：`FOLLOW_THRESHOLD = 24`、scroll 监听维护 `atBottom`、内容增长 `useLayoutEffect` 仅 `atBottom` 时 `scrollTop = scrollHeight` | 一致（断言：`ChatView.test.tsx` "条件跟随滚动" describe，含 24px 阈值边界用例） |
| §1.3 新消息回底 | `ChatView.tsx` `submit()` → `toBottom()`（无条件滚底并置贴底） | 一致（断言："非贴底时发新消息：无条件回底并恢复跟随"用例） |
| §1.4 回底入口（非贴底呈现、点击回底、贴底不渲染、与回合状态无关） | `ChatView.tsx` `{!atBottom && <div className="to-bottom-slot">…<IconChevronDownOutline14 />…</div>}`（`aria-label="回到底部"`、`data-testid="to-bottom-button"`）；`theme.css` `.to-bottom-slot`（sticky bottom 16px）/`.to-bottom`（34×34） | 一致（断言：aria-label、点击回底恢复跟随、终态横幅出现仍呈现） |
| §1.5 布局包含（滚动容器 `position: relative`） | `theme.css` `.chat-messages { position: relative }` | 一致（断言：`theme-fence.test.ts` "消息区布局包含"用例；行为面：展开思考无窗口滚动条复验通过） |

## §2 ReasoningRow 折叠行（FR-001 围栏 / FR-009 对齐）

| 条款 | 代码位置 | 结论 |
| --- | --- | --- |
| §2.1 `data-expanded` / `data-state` / 折叠围栏 `contain: layout` + 24px（与上游 `contain: size layout` 的有意差异已登记） | `projects/game/web/frontend/src/components/ReasoningRow.tsx`（`data-expanded={expanded \|\| undefined}`、`data-state`）+ `theme.css` `.reasoning-row:not([data-expanded])` | 一致（断言：`ReasoningRow.test.tsx` "对齐结构" + `theme-fence.test.ts` 围栏用例；差异理由见 fr001-investigation.md 断点 0） |
| §2.2 双层摘要 `summary > summaryText`、`data-follow-end` 纯 CSS 右对齐、无程序化 scrollLeft | `ReasoningRow.tsx`（`reasoning-summary` > `reasoning-summary-text`）+ `theme.css` `.reasoning-summary[data-follow-end]` 系列 | 一致（断言：双层结构用例 + setter 探针跨 rAF 帧断言无 scrollLeft 写入） |
| §2.3 sweep 渐变 `color-mix(in srgb, var(--dsw-alias-bg-base) 60%, transparent)`，不得硬编码颜色字面量 | `theme.css` `.reasoning-row[data-state='running'] .reasoning-row-line::after` | 一致（断言：`theme-fence.test.ts` 含 `color-mix` 正断言与 `rgba(15, 18, 22` 负断言；`--dsw-alias-bg-base` 存在性由 `src/dsh-theme.test.ts` 的消费覆盖测试兜底） |
| §2.4 展开体 MarkdownText（D1） | `ReasoningRow.tsx` `<MarkdownText text={text} streaming={running} />` | 一致（D1 登记，见 §5） |

## §3 状态横幅配色（FR-005/006）

| 条款 | 代码位置 | 结论 |
| --- | --- | --- |
| §3.1 四个 app 级变量唯一来源化、解析到 vendored token | `theme.css` `:root` 块 `--app-banner-error-bg: var(--dsw-static-red-900)`、`--app-banner-error-fg: var(--dsw-static-red-100)`、`--app-banner-warn-bg: var(--dsw-alias-state-warn-tertiary)`、`--app-banner-warn-fg: var(--dsw-alias-state-warn-primary)` | 一致（映射表 `specs/055-agent-v2-ui-fixes/data-model.md` §3 与代码同值；US2 交付时即以 red-100 为终态，理由见 `specs/055-agent-v2-ui-fixes/research.md` §2.1——alias error-primary 4.36:1 不达 FR-005 下限） |
| §3.2 error 组三类横幅 + warn 组终止标识、对比度 ≥ 4.5:1 | `theme.css` `.chat-error` / `.presets-error` / `.agent-panel-error`（error 组）、`.chat-canceled`（warn 组）；`projects/game/web/frontend/src/theme-contrast.test.ts`（`MIN_CONTRAST = 4.5`，变量引用链解析至色值计算） | 一致 |
| §3.3 红/黄色系可区分、透明底用法不改动 | 同上 + `theme-contrast.test.ts` 色系区分断言；`.queue-chip` / `.agent-guide` / `.desktop-conn` 等未触碰 | 一致 |

## §4.1 turn-process 折叠开关（FR-009）

| 条款 | 代码位置 | 结论 |
| --- | --- | --- |
| `IconChevronDownOutline14` + `data-open` CSS 旋转（折叠 -90deg → 展开 0）、标签文案本地口径 | `ChatView.tsx` `CompletedTurn` toggle 按钮（`data-open={expanded \|\| undefined}`、`<IconChevronDownOutline14 className="turn-process-chevron" />`，文字箭头已移除；onClick 显式 `focus()`，对齐上游）；`theme.css` `.turn-process-toggle[data-open] .turn-process-chevron`（`rotate(-90deg)` → `rotate(0)`，100ms 过渡，prefers-reduced-motion 降级）；文案"思考过程（N 步骤 · M 次工具调用）" | 一致（断言：chevron svg 存在、`data-open` 随展开态切换、无 ▾/▸ 字符；`ChatView.test.tsx` "折叠开关 chevron 化"用例；其余既有折叠断言全部保持通过） |

§4.2（desktop 返回刷新）属 US3 交付面（commit 6e8015f），不在本复查执行范围；`//projects/game/desktop/frontend:lib_test` 全绿佐证无回归。

## §5 有意偏离清单逐项核实（D1~D6）

| # | 清单声明 | 代码核实 | 结论 |
| --- | --- | --- | --- |
| D1 | 思考展开体 MarkdownText 渲染 | `ReasoningRow.tsx` 展开体 `MarkdownText`（streaming 透传） | 一致 |
| D2 | ToolCard 简化单卡（StateDot/名称/状态/JsonBlock 参数/`<pre>` 结果） | `projects/game/web/frontend/src/components/ToolCard.tsx`：单卡四区（header：StateDot + 名称 + 状态 + toolId；参数 JsonBlock；结果 `<pre class="tool-card-result-pre">`） | 一致 |
| D3 | 输入区单行 Input + Enter 发送 | `ChatView.tsx` composer：`<Input className="chat-input" onKeyDown={Enter → submit}>` | 一致 |
| D4 | 用户消息纯文本气泡 | `ChatView.tsx` `.msg-user` 内 `<span>{b.text?.content ?? ''}</span>`，无 markdown/附件渲染 | 一致 |
| D5 | 文案本地化口径 | "思考过程（N 步骤 · M 次工具调用）"、"思考过程"、"正在生成"、"终止"、"已终止"、"排队中 #N"、"回到底部" | 一致 |
| D6 | web 自有功能面（侧栏/会话列表/preset 管理/终止按钮/排队提示/连接状态） | `SessionList.tsx` / `PresetsView.tsx` / `AgentSettingsPanel.tsx` / `ChatView.tsx` 终止与排队呈现——均无上游对应物 | 一致 |

## 清单外观察（判定为非交互语义偏差，不登记 §5）

1. **`reasoning-separator` 颜色 token 差异**：本地 `--dsw-alias-label-tertiary` vs 上游 `--dsw-alias-label-caption`。FR-009 约束范围为**交互逻辑**；颜色 token 属视觉层，不构成交互偏差。

除上述一项外，对照 §1/§2/§4.1 与上游 [ChatView.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ChatView.tsx)、[ReasoningRow.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.tsx)、[TurnProcessNodeView.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/TurnProcessNodeView.tsx) 的交互逻辑，未发现其他清单外未声明偏差。
