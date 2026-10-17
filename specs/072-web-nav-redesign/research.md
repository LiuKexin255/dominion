# Research: web 侧栏导航与左栏信息架构重设计调研

**Feature**: specs/072-web-nav-redesign（用户对已交付 web UI 的导航 UX 调整意见调研）
**Date**: 2026-09-29 | **上游意见来源**: 用户对 `specs/071-memory-preset-scope` 交付物的反馈（三项：☰ 入口、左栏随 tab 联动、调研官方与社区设计）

## 1. 现状盘点（代码事实）

### 1.1 布局与导航现状

- 整体布局 `.app` = `.sidebar`（260px 固定）+ `.main`（`projects/game/web/frontend/src/theme.css:61-75`）。
- 侧栏 = `SessionList`（标题 + 刷新/新建图标按钮 + session 条目列表）+ 底部 `.view-switch`（`margin-top: auto` 吸底）：`Sessions | Presets` 两个文本直达按钮 + 「知识」入口按钮（`projects/game/web/frontend/src/App.tsx:602-677`）。
- 「知识」入口按钮文案在二级视图激活时为 `知识 · Wiki` / `知识 · Memory`（`App.tsx:649`：`知识{knowledgeView ? ` · ${VIEW_LABELS[view]}` : ''}`）。`.view-switch` 为 flex 行、侧栏仅 260px，三个按钮并排时该文案**换行为两行**——用户反馈的视觉问题机理。
- 二级浮层为**手写 popover**（`div.knowledge-menu` + `role=menu` + 原生 button `role=menuitem`，App 内自维护外点 mousedown / Esc 关闭 effect，`App.tsx:499-523`），样式 `.knowledge-menu`（`theme.css:203-242`，自底向上 `bottom: calc(100% + 4px)`）。
- 现行契约：`specs/071-memory-preset-scope/contracts/web-nav.md`（两级导航 + 浮层行为表）；基线 `specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §2（侧栏底部 `Sessions | Presets` 切换、单页 state、无 router）。

### 1.2 痛点机理：非 session 视图下点击 session 条目无响应

`openSession(name)` 只做 `setOpened` / `setSelected` / `setNotice`（`App.tsx:532-538`），**不改 `view`**。当用户处于 presets/wiki/memory 视图时，左侧 SessionList 仍然完整渲染，点击条目仅更新隐藏的 `.view-pane` 内部选中态（会话面板常驻挂载、以 CSS `display:none` 隐藏，`App.tsx:681-699`），主区无任何可见变化——形成"死点击"。

### 1.3 各视图自身的信息架构（左栏联动设计需避免两处列表冗余）

| 视图 | 视图内部已有的列表/选择器 | 代码位置 |
|---|---|---|
| PresetsView | role 过滤下拉 + preset 全量列表（行内编辑/删除） | `projects/game/web/frontend/src/components/PresetsView.tsx:278-357` |
| WikiView | 搜索框 + wiki 标题列表（分页）+ 查看全文/编辑表单 | `projects/game/web/frontend/src/components/WikiView.tsx:56-118` |
| MemoryView | preset 选择器（`<select>` 下拉，`data-testid="memory-preset-select"`）+ memory 条目列表（分页） | `projects/game/web/frontend/src/components/MemoryView.tsx:335-351, 423-479` |

### 1.4 会话面板常驻挂载与互斥渲染基线（不可回退的约束）

- 会话面板容器常驻挂载、非当前视图以 CSS 隐藏（卸载会让 ChatPanel 回填 effect 重跑、覆盖在途回合）：`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §2 与 `App.tsx:679-699`、`theme.css:470-479`。
- 主区管理视图互斥渲染（同一时刻至多一个管理视图渲染）：`specs/071-memory-preset-scope/contracts/web-nav.md` §3。

## 2. dsh 官方 webUI 插件调研（上游 deepseek-harness）

本仓库前端依赖 `@deepseek-ai/dsh-client-ui-primitives@0.1.7-rc.2`（`pnpm-workspace.yaml:26`），上游仓库 https://github.com/deepseek-ai/deepseek-harness ，官方 web 客户端位于 `packages/client/`。

### 2.1 官方整体布局：三栏 AppFrame

来源：https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-layout/README.md

- **三栏**：左 sidebar（264–420px 可拖，默认 280px，可折叠为 56px rail）+ 中央 Conversation + 右栏面板轨道（默认 45% 视口、上限 70%）。
- **全局面板（global panels）**：占据主区 `main` keyed slot；`ctx.layout.selectPanel(id)` 选中已注册面板，`null` 回到 Conversation（**不改当前 Session**）。设置页、插件管理等均为全局面板替换主区，而非改变左栏内容。

### 2.2 官方左栏：恒为"会话/工作区导航器"

来源：https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-sidebar/README.md 与 https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-workspace/README.md

- 左栏结构：品牌行 + New Session 动作 + 折叠控制 + workspace/session 浏览（`sidebar.workspaces` seat，由 ui-workspace 填充）+ **底吸 Settings 入口**（`sidebar.settings`）。
- **全局面板入口（`sidebar.panellist`）**：插件向侧栏注册**图标条目**（`id`/`order`/`label`），同一 `id` 寻址主区 `main` slot 的面板组件——即官方用"侧栏图标列表"选择主区面板，**左栏内容不随面板切换而变**。
- **点击 session 条目永远回到会话**：`ctx.uiWorkspace.openSession(target)` "synchronously replaces the owned mainView reference and **returns the main area to Conversation**"——官方不存在"死点击"：任何面板激活时点 session 都会切回 Conversation 并打开该会话。
- **面板激活时 session 行抑制选中态**："Session rows read `usePanelInfo` to suppress their selected appearance while a global panel is active"——避免"高亮的会话与主区内容不一致"的误导。

### 2.3 官方右栏：工具面板轨道（tab 化）

来源：https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-sidebar-right/README.md

文件树/浏览器/终端/文档预览是**右侧**面板 tab（每会话一个 docking surface、tab strip、guide 引导页），不占左栏。即官方把"工作区级工具"放右栏、"会话导航"放左栏、"管理页面"放主区全局面板。

### 2.4 ui-primitives 能力面（本仓库可直接用的组件）

来源：本仓库 `projects/game/web/frontend/node_modules/@deepseek-ai/dsh-client-ui-primitives/lib/types/`（README：https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-primitives/README.md ）

- **无布局类组件**（Sidebar/Nav/Split 等）——primitives 是"controls + icons + renderers"原子包；侧栏布局由应用自持（本仓库 `theme.css` 同款做法与官方一致）。
- `Button`：`variant`（primary/ghost/outline/toolbar）、`size`（md 36px / sm 28px）、`icon?: ReactNode`（leading 16px 图标）；icon-only 按钮无文本即无换行可能（`lib/types/Button.d.ts`）。
- `Menu` / `MenuItemButton`：**分组标签 `{type:'label'}`、分隔线 `{type:'separator'}`、嵌套子菜单**；`side="top"`（向上展开）、`portal`、`selectedId` + `selection='check'`（选中项打勾）或 `'fill'`；开态键盘漫游（↑/↓/Home/End）、Esc/外点经 `onClose` 回调（`lib/types/Menu.d.ts`）。SessionList 已用 `Menu`（portal 模式，`projects/game/web/frontend/src/components/SessionList.tsx:274-301`）——仓库内先例。
- 图标集（`lib/types/icons/index.d.ts` 全量核对）：**没有三横杠（hamburger）图标**。最接近的 `IconFlatListOutlineRegular` 是"左列圆点 + 右列三横线"的清单图标（`lib/index.js` 中 path 为 `M6 3.5h7.5…` 三线 + `M2.6 3.5h.01` 三点），不是纯三横杠。纯三横杠需**内联 SVG**——先例：`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §1 图标按钮条款明确"dsh primitives 图标**或内联 SVG**"；图标按钮可访问名模式为原生 `title` + `aria-label` 单元素承载（`SessionList.tsx:52-80` IconButton）。

## 3. 常用 web 页面与社区常见设计

### 3.1 VS Code：Activity Bar + Side Bar 联动（左栏随导航切换内容的正典）

来源：https://code.visualstudio.com/docs/editing/userinterface 与扩展 UX 指南 https://code.visualstudio.com/api/ux-guidelines/overview

> "The Activity Bar and the Primary Sidebar are tightly coupled. Clicking on a contributed Activity Bar Item (read: View Container) opens up the Primary Sidebar where one or more Views associated with that View Container will be rendered."

即：最左侧图标 rail 选择**域**，旁边的 Side Bar 渲染该域的视图列表——**左栏内容严格跟随 rail 选中项**。这是用户直觉（"左侧栏位内容跟随底部 tab 改变"）的最直接先例。

### 3.2 Discord：双左栏（服务器 rail + 频道列表，第二栏内容跟随第一栏）

来源：https://getstream.io/blog/discord-clone-server-list/ （对 Discord 三栏布局的复刻文档：server list（左窄 rail）→ channel list（当前 server 的频道，随 rail 切换）→ 消息主区）

> "The Discord app uses a three-pane layout … the list of servers … on the left as rounded icons … the list of channels **on the active server** … the list of messages on the currently selected channel."

### 3.3 Slack（iPad 重设计）：tabs 表意图，sidebar 选择任务，content 完成任务

来源：https://slack.design/articles/how-we-redesigned-slack-for-the-ipad/

> "as they move through the app, **tabs allow users to show intent, the sidebar allows them to choose your task at hand, and the content view allows the opportunity to get the task done**."

即导航分层惯例：一级选择器（tab/rail）决定二级列表（sidebar）的内容域，sidebar 条目决定主区内容。

### 3.4 管理后台模板：侧栏导航恒定 + 内容区切换（Ant Design / shadcn/ui）

来源：https://ant.design/components/layout/ （Layout/Sider：侧栏菜单导航，`collapsible`/`collapsedWidth`；"Two-columns layout … mainnav is placed on the left side of the page"）；https://ui.shadcn.com/docs/components/sidebar （SidebarProvider/Sidebar/SidebarInset；`collapsible="icon"` rail 模式；SidebarHeader/Footer/Group 结构）

管理后台惯例：左侧是**恒定的功能菜单**，右侧内容区随菜单切换；侧栏本身不因内容区变化而变。与 dsh 官方"左栏恒为会话导航"同构。

### 3.5 Master-Detail（列表-详情）模式：管理视图的正典

来源：https://developer.microsoft.com/en-us/azure-devops/components/master-detail （master panel 固定宽列表 + detail area；"The collection list needs to always have one active item"）；https://learn.microsoft.com/en-us/windows/apps/develop/ui/controls/list-details （side-by-side 样式：list pane + details pane 同屏）

presets/wiki/memory 三类管理视图天然是 master-detail 结构：左列表选中项驱动右侧详情/编辑。若把 master 列表放左栏，即形成 VS Code 式联动。

### 3.6 图标菜单按钮可达性惯例

来源：WAI-ARIA APG Menu Button Pattern https://www.w3.org/WAI/ARIA/apg/patterns/menu-button/

- 触发按钮 `aria-haspopup="menu"`（或 `true`）；开态 `aria-expanded="true"`、关态 `"false"`（现有实现已满足，`App.tsx:645-646`）。
- 图标-only 按钮必须提供可访问名（本仓库先例：`title` + `aria-label`，`SessionList.tsx:52-80`）。
- 菜单容器 `role=menu`、条目 `role=menuitem`；菜单通常以箭头/横杠图标暗示弹出行为。

## 4. 结论对照

| 维度 | dsh 官方 webUI | VS Code / Discord / Slack | 管理后台（AntD/shadcn） | Master-Detail 正典 |
|---|---|---|---|---|
| 左栏内容 | 恒为会话/工作区导航 | 随一级 rail/tab 切换 | 恒定功能菜单 | master 列表（选中项驱动详情） |
| 非"会话"页面下点会话条目 | **切回 Conversation 并打开**（openSession 语义） | n/a（rail 切换即换栏） | n/a | n/a |
| 管理页面承载 | 主区全局面板（selectPanel） | 主区（编辑器）/Side Bar 视图 | 主区内容区 | 主区 detail pane |
| 选中态抑制 | 全局面板激活时 session 行抑制选中高亮 | rail 当前项高亮 | 菜单当前项高亮 | 列表当前项高亮 |

两点直接可用的结论：

1. **"死点击"在所有参考系中都不是合法状态**：官方 dsh 的解法是"点 session 即回主区会话"；VS Code/Discord 的解法是"左栏与主区同域，不存在跨域点击"。修复方式二选一（见 design.md 方案 A/B），但"点击无响应"必须消除。
2. **左栏是否随 tab 切换存在两种都成立的正典**：官方 dsh + 管理后台 = 左栏恒定；VS Code + Discord + Slack + master-detail = 左栏随域切换。对本应用两者的取舍依据（成本、扩展性、会话可达性）见 `design.md` §3 与分叉点 D1。
