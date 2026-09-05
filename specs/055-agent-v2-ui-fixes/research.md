# Research: 055 agent-v2 界面可用性修复 + dsh-web 交互对齐

**Feature**: `specs/055-agent-v2-ui-fixes/spec.md`
**日期**: 2026-09-05
**方法**: 逐文件对照本地 web 前端（`projects/game/web/frontend/src/`）与 dsh-web 上游（[deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)）的传承组件源码与样式；四项用户缺陷的根因核实（读取本地源码 + 上游 token 表）。

---

## 1. dsh-web 交互对齐审计（spec FR-009 / US4）

### 1.1 审计范围界定（lineage 判定）

**纳入审计（改造传承组件）**——依据 `projects/game/web/frontend/README.md` Attribution 与组件源码注释中的上游来源链接：

| 本地组件/样式 | 上游来源 |
| --- | --- |
| `src/components/ChatView.tsx`（滚动交互部分） | [packages/client/ui-chat/src/client/chat/ChatView.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ChatView.tsx) + [ChatView.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ChatView.module.css) |
| `src/components/ReasoningRow.tsx` + `src/theme.css` 的 `.reasoning-row*` 规则 | [ReasoningRow.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.tsx) + [ReasoningRow.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.module.css) |
| 折叠开关视觉（`.turn-process-toggle`） | [TurnProcessNodeView.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/TurnProcessNodeView.tsx)（chevron 图标 + data-open 旋转） |
| MarkdownText / DisclosureRow / JsonBlock / StateDot / Input 等基元 | 直接复用 npm 包 `@deepseek-ai/dsh-client-ui-primitives`（无改造，天然对齐） |

**不纳入对齐（未移植的上游产品能力）**——属产品能力差异，非交互偏差（spec A6）：

- contentEditable 富输入 composer（[ComposerContentEditable](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/client/ui-conversation/src/client/input/editor)，含附件、@/斜杠菜单、编辑范围等）；
- 用户消息附件/文件呈现、MessageIconActions、模型重试倒计时（[MessageItem.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/MessageItem.tsx)）；
- ToolRow 丰富工具视图（terminal/diff/read/search/web 卡，[GenericToolCard.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-tool/src/client/tool/toolviews/GenericToolCard.tsx)）；
- TurnNavigator（回合导航侧栏）、turn 虚拟列表/分页加载等。

### 1.2 审计结论：修复项（F 系列，spec FR-009 "修复"）

#### F1 思考折叠行缺失布局围栏（问题 4 折叠态候选修复，对齐义务独立成立）

- **上游**: `.root:not([data-expanded]) { contain: size layout; height: calc(24px + var(--dsh-content-font-delta, 0px)); }`（ReasoningRow.module.css）；TSX 根节点带 `data-expanded={expanded || undefined}`。
- **本地**: 无 `data-expanded` 属性、无 `contain`、无固定高度（`projects/game/web/frontend/src/theme.css` 的 `.reasoning-row`）。流式期间思考行的内容测量可影响祖先布局——文档级滚动条（问题 4）的已知候选根因；最终以部署环境复现验证（spec A3）。
- **修复**: ReasoningRow.tsx 根节点加 `data-expanded`；theme.css 加 `.reasoning-row:not([data-expanded]) { contain: size layout; height: 24px; }`（本地无 content-font-delta 变量体系，直接 24px，与 DisclosureRow `.row` 高度一致）。

#### F2 跟随摘要（follow-end）机制偏离：程序化 scrollLeft → 上游纯 CSS

- **上游**: 摘要为双层结构 `.summary > .summaryText`；运行态 `.summary[data-follow-end] { display: flex; justify-content: flex-end }` + `.summaryText { flex: 0 0 auto; width: max-content; min-width: 100%; overflow: visible; text-overflow: clip }`——文本右对齐自然露出最新行末尾，由 `.row` 的 `overflow: hidden` 裁剪。无任何 JS 滚动。
- **本地**: 单层 `.reasoning-summary` + `overflow: hidden` + rAF 节流的 `element.scrollLeft = scrollWidth - clientHeight`（`useThrottledVisualUpdate`，改造自上游另一个文件）。自造机制，且与 F1 的围栏缺失叠加。
- **修复**: 采用上游双层结构与纯 CSS follow-end；删除 `useThrottledVisualUpdate`/`summaryRef`/`scheduleSummaryScroll` 机制（减代码，Constitution II 简化）。

#### F3 扫描动画颜色硬编码（浅色主题值用于深色主题）

- **上游**: `background: linear-gradient(90deg, transparent 0%, color-mix(in srgb, var(--dsw-alias-bg-base) 60%, transparent) 55%, transparent 100%)`——主题感知。
- **本地**: 硬编码 `rgba(15, 18, 22, 0.6)`（深色叠加，本为浅色主题设计）——深色主题下 sweep 接近不可见。
- **修复**: 对齐上游 `color-mix(... var(--dsw-alias-bg-base) ...)` 写法。

#### F4 对话滚动交互整体偏离（问题 2 的根因，US1 主体）

- **上游**: `FOLLOW_THRESHOLD = 24`；滚动监听维护 at-bottom 态，仅"贴底 + 流末梢签名变化"时 `el.scrollTop = el.scrollHeight`；`!atBottom` 时渲染 34×36px 的 chevron 回底按钮（sticky 槽、右下、aria-label）；用户自己的新提交（appendedUser/appendedSubmission）无条件回底。
- **本地**: `[history, live, queue, error]` 依赖的 effect 无条件贴底；无回底按钮。
- **修复**: 见 §2.2 设计（条件跟随 + 回底入口 + 新消息回底）。

#### F5 折叠开关视觉：文字箭头 → 上游 chevron 图标

- **上游**: `IconChevronDownOutline14` + CSS `[data-open]` 旋转；标签为计数文案（"N 次工具调用 · N 条消息"）。
- **本地**: `.turn-process-toggle` 用文字 `▾/▸`；标签"思考过程（N 步骤 · M 次工具调用）"。
- **修复**: 换用 `IconChevronDownOutline14` + `data-open` 旋转样式；标签文案保留本地化口径（交互相同，属 D5）。

### 1.3 审计结论：有意偏离项（D 系列，登记清单，保留不改）

| # | 偏离 | 上游行为 | 保留理由 |
| --- | --- | --- | --- |
| D1 | 思考展开体以 MarkdownText 渲染 | `thinkBody` 纯文本（pre-wrap） | `specs/054-agent-v2-bugfixes/` FR-010 明确要求思考体与正文同 markdown 能力（GFM、流式增量） |
| D2 | ToolCard 简化单卡（StateDot + 名称 + 状态 + JsonBlock 参数 + `<pre>` 结果） | ToolRow 变体图标 + terminal/diff/read/search/web 卡 | 049/054 契约定型（`specs/054-agent-v2-bugfixes/contracts/web-ui.md` §3：扫雷棋盘预格式化等宽呈现是验收要求）；上游丰富视图对应我们不存在工具形态 |
| D3 | 输入区单行 Input + Enter 发送 | contentEditable 富输入（多行/附件/命令菜单） | 049/051 自有设计（`specs/049-agent-v2-dsh-init/contracts/web-frontend.md` §3.2），未移植上游 composer；对齐=新增产品能力，非缺陷 |
| D4 | 用户消息纯文本气泡 | 附件/文件/重试倒计时等丰富呈现 | 产品能力差异（后端无这些消息类型） |
| D5 | 文案本地化口径（"思考过程（N 步骤 · M 次工具调用）"等中文文案） | i18n key 体系 | 上游 locale/slot 体系被剥离（attribution 已声明）；交互语义相同 |
| D6 | web 侧栏/会话列表/preset 管理/终止按钮/排队提示等自有功能 | 无对应上游 | 本产品自有功能（051/054 交付），无对齐对象 |

---

## 2. 四项缺陷的修复设计

### 2.1 状态横幅对比度（问题 1 / FR-005）

**根因**（已核实）: dark 主题下 `--dsw-alias-state-error-primary` 与 `--dsw-alias-state-error-secondary` 同为 red-400 `#f25a5a`；warn primary `#f59e0b` / secondary `#f7ad31` 近似同色（`projects/game/web/frontend/src/dsh-theme/design-platform.css`）。`secondary` 在该体系中的语义是"亮色前景变体"而非"填充底色"；深色主题下合法的填充底色是深色调 token（warn-tertiary=amber-900 `#27241f`、success-tertiary=green-900）——**error 无 tertiary 别名**（token 表未定义）。

**决策 R1**: 在 `theme.css` 定义 app 级横幅配色变量（单一来源，全部解析到官方静态 token，不散落硬编码）：

| 用途 | 背景 | 文字 | WCAG 对比度（计算值） |
| --- | --- | --- | --- |
| 错误横幅（`.chat-error`/`.presets-error`/`.agent-panel-error`） | `var(--dsw-static-red-900)` `#570c0c` | `var(--dsw-alias-state-error-primary)` `#f25a5a` | ≈5.0:1 ✓ |
| 警示横幅（`.chat-canceled`） | `var(--dsw-alias-state-warn-tertiary)` `#27241f` | `var(--dsw-alias-state-warn-primary)` `#f59e0b` | ≈6.9:1 ✓ |

- 红黄两色系区分保持（FR-006）；`.queue-chip`/`.agent-guide`/`.desktop-conn` 等透明底 + 前景色用法本就可读，不动。
- **备选被否**: 亮底深字（red-100 底）——与深色主题整体风格冲突；直接改 vendored token 表——上游文件不应本地语义化改写（`src/dsh-theme/README.md` 升级流程会被破坏）。

### 2.2 条件跟随滚动 + 回底入口（问题 2 / FR-002~004，对齐上游 F4）

**设计**（ChatView 内实现，jsdom 可测）：

- 以 `scroll` 事件维护 at-bottom 布尔态：`scrollHeight - scrollTop - clientHeight <= 24`（上游 `FOLLOW_THRESHOLD = 24`）视为贴底。
- 内容增长的贴底 effect 仅在 at-bottom 时执行 `scrollTop = scrollHeight`；发新消息（submit）无条件回底并置 at-bottom。
- `!atBottom` 时在消息区内渲染"回到底部"按钮（`IconChevronDownOutline14`，sticky 定位右下、34px、aria-label="回到底部"、`data-testid="to-bottom-button"`），点击回底并恢复跟随；贴底时不渲染（FR-004，Edge Case"回底入口的存在条件"：与回合状态无关）。
- 展开态切换（turn-process/ReasoningRow 展开）不主动滚动（阅读位置保持，Edge Case"回合结束时的视角"）。
- **备选被否**: CSS `overflow-anchor` 蹦床方案——浏览器实现差异大，不可组件级断言；保留 rAF 节流的 scrollLeft——被 F2 纯 CSS 方案取代。

### 2.3 思考行布局约束（问题 4 / FR-001）

**用户观察补充（2026-09-05）：折叠态与展开态均出现全页面滚动条**——这排除了"折叠态围栏缺失是唯一根因"的假设（展开态上游同样无围栏），根因必须从**两态共有而正文输出所无**的因素中定位。

**候选清单（按优先级）**：

1. **running 扫描动画 `::after`**（首要候选）：`.reasoning-row[data-state='running'] .reasoning-row-line::after` 挂在行容器上，**折叠/展开两态行容器均存在**；正文输出（MarkdownText）没有该元素——与"仅思考输出出现、正文正常"的观察完全吻合。本地实现与上游有两处差异：keyframes `left: -300px → 100%` + `width: 300px` 的越界定位依赖 `.row`/`.reasoning-row-line` 的 `overflow: hidden` 裁剪（模块 CSS + 本地覆盖双保险，但**部署构建中的实际裁剪状态需验证**——若 CSS 加载顺序/模块类名哈希导致 `overflow` 未生效，动画元素即越出滚动容器裁剪链）；且 `inset-block`/`left` 百分比动画逐帧触发布局。
2. **折叠态围栏缺失**（F1）：解释折叠态，但单独无法解释展开态。
3. **follow-end 程序化 scrollLeft**（F2 对象）：仅折叠态存在（摘要只在折叠时渲染），无法独立解释展开态，但与候选 1 叠加。

**排查协议（实现阶段执行，FR-001"不得凭推测改代码"的落地步骤）**：

1. 部署环境复现两态滚动条后，DevTools 检查 `document.documentElement.scrollHeight > clientHeight` 的溢出来源（Elements → 逐层定位越界元素，或 Performance→Layout）。
2. **隔离实验 A**：DevTools 中禁用 `reasoning-row-sweep` 动画（或系统开启 `prefers-reduced-motion`——本地样式已含 `animation: none` 降级分支）→ 滚动条是否消失。
3. **隔离实验 B**：检查部署构建中 `.reasoning-row-line`/DisclosureRow `.row` 的 computed `overflow` 与 `position` 是否实际生效（CSS module 类名哈希 + 加载顺序）。
4. 按 1-3 的实际断点修复（候选 1 成立则修动画的裁剪/实现方式，候选 2/3 由 F1/F2 覆盖）；修复后两态 + 正文输出三场景全部复查。
5. 验收 = 部署环境人工验证记录（2026-09-05 澄清 Q2=A；quickstart §3 场景 1 已含两态验证步骤）。

**修复方向**：F1/F2/F3 全部落地（对齐义务与 FR-009 不依赖根因结论）；候选 1 若被证实，动画实现收敛为上游写法（`color-mix` 渐变 + 依赖行容器裁剪的同一结构），必要时以等效不含越界定位的实现替代——具体以隔离实验结论为准。

### 2.4 desktop 列表刷新（问题 3 / FR-007）

- `projects/game/desktop/frontend/src/App.svelte` 的 `handleBackToSessions()`：在 `page = 'sessions'` 后追加 `void handleRefresh()`（既有错误语义/加载态天然复用：失败 set error 且不清空 sessions）。
- 刷新与选择竞态：`handleRefresh` 仅写 `sessions`/`loading`/`error` 状态，不触碰 `selectedSession`/`page`，天然满足 Edge Case"刷新进行中选择不受干扰"。
- **测试基建**: desktop 前端有 vitest（`vitest_test`，`projects/game/desktop/frontend/BUILD.bazel`）但无 jsdom。决策 R3: 添加 `jsdom` devDep（catalog 已有 `^26.0.0`），用 Svelte 5 命令式 `mount(App)` + `flushSync` + 原生 DOM 事件 + `vi.mock('./api')` 断言"点击 Back → listSessions 再次调用"（不引入 @testing-library/svelte，最小依赖面）。
- **备选被否**: 抽取纯函数测逻辑——刷新编排本就是一行调用，抽函数为测试而测试；@testing-library/svelte——增加依赖无必要。

### 2.5 对比度断言的组件级测试（FR-008）

- jsdom 的 `getComputedStyle` 不解析 CSS 自定义属性链，**决策 R2**: 测试以 `import ... from 'theme.css?raw'` / `'design-platform.css?raw'`（vite 原生支持）读取两份样式文本，解析 `.chat-error` 等规则的 `background`/`color` 变量引用 → 从 token 表解析变量值 → 计算 WCAG 相对亮度对比度，断言 ≥ 4.5:1。断言对象是**真实交付样式**而非平行常量。
- 新增 `src/theme-contrast.test.ts`（web），套件覆盖四类横幅规则。

---

## 3. Decisions 总览

| # | 决策 | 依据 |
| --- | --- | --- |
| R1 | 横幅配色：app 级变量映射官方静态 token（red-900/amber-900 底 + primary 前景） | §2.1；保持 token 单一来源与升级流程 |
| R2 | 对比度测试以 `?raw` CSS 解析 + WCAG 公式断言 | jsdom 限制；断言真实样式 |
| R3 | desktop 测试加 jsdom devDep，Svelte 5 mount + 原生事件 | 最小依赖；FR-008 组件级断言 |
| R4 | ReasoningRow follow-end 采用上游纯 CSS 双层结构，删除本地 rAF/scrollLeft 机制 | F2 审计；Constitution II 简化 |
| R5 | 回底按钮样式对齐上游形态（sticky 右下 34px chevron），具体间距适配本地布局 | F4；上游 ChatView.module.css `.toBottomSlot/.toBottom` |
| R6 | 折叠开关换 chevron 图标 + data-open 旋转；标签文案保留本地口径 | F5 + D5 |
| R7 | 滚动跟随阈值取上游 `FOLLOW_THRESHOLD = 24` | 上游既定值，无需自创 |

## 4. 开放问题

无——三项 plan 前澄清已闭合（回底入口/验收手段/刷新时机），审计范围与偏离清单已定型（§1）。布局缺陷根因排查义务（FR-001）在交付前的部署环境验证中闭环（§2.3）。
