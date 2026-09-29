# Tasks: web 侧栏导航重设计（☰ 图标菜单 + 左栏会话直达）

**Input**: Design documents from `/specs/072-web-nav-redesign/`（spec.md、design.md、research.md、contracts/web-nav.md、plan.md）

**Prerequisites**: plan.md、spec.md（US1/US2）、research.md、design.md、contracts/web-nav.md（均已存在；071 旧契约 superseded 指针已落定）

**Tests**: 按宪法原则 IV：**编译 + 单测属于每个实现任务的一部分**（`bazel build` + `bazel test` 相关 target，不单列 task）；本特性纯前端、无服务端变更，无大型测试新增面（宪法 VI 豁免口径见 plan.md Constitution Check 门禁 5，对齐 `specs/069-wiki-template-web/research.md` R8）。

**Organization**: 按 user story 分 phase（US1 ☰ 入口 + Menu 迁移 → US2 openSession 跨视图 → Polish 回归）。两个 US 共改 `App.tsx`/`App.test.tsx`，**严格串行**（US1 先行——入口/菜单结构是 US2 新用例的断言载体；AGENTS.md 同文件编辑串行约束）；`theme.css`（US1 T001）与 `App.tsx` 不同文件、T001先行。管理视图组件与 `SessionList.tsx` 零改动。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story（US1/US2）；Setup/Polish phase 无 story 标签
- 每个任务含精确文件路径

## Path Conventions

仓库根相对路径；web 前端 `projects/game/web/frontend/src/`（与 plan.md Project Structure 一致）；primitives 包内文件经 workspace `node_modules` 实路径引用（`projects/game/web/frontend/node_modules/@deepseek-ai/dsh-client-ui-primitives/`）。

---

## Phase 1: Setup

**无任务**：既有 web 前端增量变更，无新包、无新依赖（内联 SVG 为浏览器原生载体，`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §1 允许）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无

---

## Phase 2: User Story 1 - ☰ 图标菜单入口 + primitives Menu 迁移 (Priority: P1) 🎯

**Goal**: 入口图标化（两行问题根除）+ 菜单迁移 primitives `Menu`（分组可扩展、键盘漫游/外点/Esc 内建、`selectedId` 选中呈现）；手写浮层与自维护 effect 删除。

**Independent Test**: spec.md US1 验收场景 1-6——`App.test.tsx` 入口图标化/菜单分组/切换与关闭行为/结构恒定断言全绿（无服务端依赖）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（含其引用基准 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）
- **官方文档**：[React 官方文档](https://react.dev/learn)（组件/state——外点/Esc effect 删除面的背景）；[Vitest](https://vitest.dev/)、[Testing Library: React Testing Library](https://testing-library.com/docs/react-testing-library/intro/)（用例改造）；[WAI-ARIA APG Menu Button Pattern](https://www.w3.org/WAI/ARIA/apg/patterns/menu-button/)（`aria-haspopup`/`aria-expanded`/菜单语义）；dsh primitives（`@deepseek-ai/dsh-client-ui-primitives@0.1.7-rc.2`）——仓库内实文件：`projects/game/web/frontend/node_modules/@deepseek-ai/dsh-client-ui-primitives/lib/types/Menu.d.ts`（`Menu`/`MenuEntry`/`MenuItem`/`side`/`selectedId`/`selection`/`onClose` 全量 props）、`lib/types/Button.d.ts`（`icon` prop、`size`）、`lib/types/icons/index.d.ts`（图标集全量——无汉堡 glyph 的依据）、`lib/Menu.module.css`（in-place 定位：`.root` relative + `.list` absolute + `.sideTop` 自底向上）、`README.md`（组件目录 Menu/Button 行为说明；上游 https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-primitives/README.md ）
- **技术文章/技术参考文档**：`specs/072-web-nav-redesign/contracts/web-nav.md`（全文——本 phase 的实现契约）、`specs/072-web-nav-redesign/design.md`（§2 摘要/§3.1 入口与菜单设计/§3.3 样式收敛/§4.1 流程图/§6 测试影响面）、`specs/072-web-nav-redesign/spec.md`（US1 验收场景 1-6、FR-001..FR-004、FR-009、FR-010、Edge Cases 之 testid 条目）、`specs/072-web-nav-redesign/research.md`（§1.1 现状与两行问题机理、§2.2 官方模式、§2.4 primitives 能力面与图标缺失结论）；基线与被取代面：`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md`（§1 图标按钮条款——内联 SVG/`title`+`aria-label` 先例；§2 会话面板常驻挂载基线）、`specs/071-memory-preset-scope/contracts/web-nav.md`（行为表与不变语义的延续核对——该文件头部已标 `[SUPERSEDED — see Feature 072]`）；既有代码参考：`projects/game/web/frontend/src/App.tsx`（`menuRef`/外点/Esc effect——L499-523；`selectView`——L527-530；`VIEW_LABELS`——L62-67；入口与手写浮层——L638-675）、`projects/game/web/frontend/src/App.test.tsx`（三个导航用例断言现状——L1948-2083，含 `menu.parentElement.className === 'view-menu'` 结构断言 L1972 与 `textContent` 含 `Wiki`/`Memory` 断言 L1986/L2059——本 task 删除面）、`projects/game/web/frontend/src/components/SessionList.tsx`（L52-80 `IconButton` 的 `title`+`aria-label` 图标按钮模式；L274-301 primitives `Menu` 用法先例——注意其 `portal` 是因列表处 `overflow-y:auto` 容器，本场景 in-place 即可）、`projects/game/web/frontend/src/components/SessionList.test.tsx`（L127 `getByRole('menuitem', { name })` 断言先例）、`projects/game/web/frontend/src/theme.css`（L184-242 待删样式段）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（TS 源码书写规则）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（`vitest_test` shim 退出码契约）

- [X] T001 [P] [US1] 在 `projects/game/web/frontend/src/theme.css` 删除 071 手写浮层样式段（FR-010，终态）：`.view-menu`（L195-198）、`.knowledge-menu`（L203-217）、`.knowledge-menu-item` 及其 `:hover`/`[data-active='true']` 变体（L220-242）与相关注释一并清除——菜单卡片/层级/键盘焦点样式由 primitives `Menu.module.css` 提供；若 `view-switch` flex 行内的 Menu 锚定 wrapper（`.root` inline-flex）需要对齐微调，做最小补充（`--app-*` token 体系内），MUST NOT 保留任何手写浮层/菜单项样式
- [X] T002 [US1] 在 `projects/game/web/frontend/src/App.tsx` 完成入口与菜单迁移并同步改造 `App.test.tsx`（依赖 T001；严格按 `specs/072-web-nav-redesign/contracts/web-nav.md` §1-§4；`App.tsx` 与 `App.test.tsx` 在本 task 内先实现后改断言，一体验收）：
  - **入口按钮**：替换 L640-650 的「知识」文本按钮为图标-only 按钮——`Button size="sm"`、`icon` = 内联三横杠 SVG（App.tsx 内模块级小组件，仅此一处使用不新建文件；viewBox `0 0 16 16`、三等长线、`fill="none"`、`stroke="currentColor"`、`strokeLinecap="round"`、`aria-hidden="true"`，svg 属性形态对齐 primitives 图标实现）、无 children、`title="更多视图"` + `aria-label="更多视图"`、`aria-haspopup="menu"`、`aria-expanded={knowledgeMenuOpen}`、`data-testid="view-more"`（testid 更名，自 `view-knowledge`）、`variant={knowledgeView ? 'primary' : 'ghost'}` + `data-active`（语义同现状，去掉视图名文本拼接）
  - **菜单**：替换 L651-674 手写浮层为 primitives `Menu`——`open={knowledgeMenuOpen}`、`anchor` = 入口按钮、`items` = `MenuEntry[]`（`{ type: 'label', id: 'g-knowledge', text: '知识' }` + `{ id: 'wiki', label: VIEW_LABELS.wiki }` + `{ id: 'memory', label: VIEW_LABELS.memory }`）、`selectedId={knowledgeView ? view : undefined}`、`onSelect={(id) => selectView(id as AppView)}`、`onClose={() => setKnowledgeMenuOpen(false)}`、`side="top"`（in-place，无 `portal`——`view-switch` 不处滚动容器内）
  - **删除面**：`menuRef` 与外点/Esc 关闭 effect（L499-523，Menu `onClose` 内建承载）；`knowledge-menu` 浮层 JSX；头注释中「知识」入口表述更新为 ☰ 图标菜单（引用 `specs/072-web-nav-redesign/contracts/web-nav.md`）
  - **App.test.tsx 改造**（L1948-2083 三个用例，按 contracts/web-nav.md §5 锚点 1/4）：入口断言 `view-knowledge`→`view-more`、新增 `aria-label` 与"无文本子节点"断言（SC-001 不换行防护）、保留 `aria-haspopup`/`aria-expanded`/`data-active` 断言；删除 `entry.textContent` 含 `Wiki`/`Memory` 断言（L1986/L2059）；菜单断言改 `getByRole('menuitem', { name: 'Wiki' / 'Memory' })` + 分组标签「知识」文本查询；`selectedId` 选中呈现断言按 Menu `selection` 实现落地；外点/Esc 关闭断言按 Menu 关闭触发机制模拟（`fireEvent.keyDown(document.body, { key: 'Escape' })` 起步，外点按其监听面模拟——实现时以 primitives Menu 行为为准）；结构断言：`view-switch` 子元素数恒为 3、二级项在 Menu 列表内（锚点 4）；四视图互斥渲染与直达零回归断言语义保留
  - 编译 + 单测：`bazel build` + `bazel test`（web 前端相关 target）通过

**Checkpoint**: 入口图标化 + 分组菜单可用，两行问题根除，既有导航语义零回归（SC-001/SC-002）。

---

## Phase 3: User Story 2 - 左栏 session 条目任意视图可达 (Priority: P2)

**Goal**: `openSession` 跨视图切换（死点击消除）；会话面板常驻挂载/高亮保留等基线零变化。

**Independent Test**: spec.md US2 验收场景 1-5——`App.test.tsx` 三管理视图点击 session 条目切回断言全绿。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）
- **官方文档**：[React 官方文档](https://react.dev/learn)（state 与事件处理）；[Vitest](https://vitest.dev/)、[Testing Library: React Testing Library](https://testing-library.com/docs/react-testing-library/intro/)
- **技术文章/技术参考文档**：`specs/072-web-nav-redesign/contracts/web-nav.md`（§2 行为表"点击左栏 session 条目"行、§3 高亮保留与常驻挂载条款、§5 锚点 2/3）、`specs/072-web-nav-redesign/spec.md`（US2 验收场景 1-5、FR-005/FR-006/FR-008、Edge Cases 之删除会话/空列表条目）、`specs/072-web-nav-redesign/research.md`（§1.2 死点击机理、§2.2 官方 `openSession` 语义出处）、`specs/072-web-nav-redesign/design.md`（§3.2、§4.2 状态图）；基线：`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §2（会话面板常驻挂载/视图切换语义）、`specs/059-agent-v2-team-mode/contracts/web-views.md` §2（常驻挂载不重跑回填的注释出处——切回呈现既有状态的依据）；既有代码参考：`projects/game/web/frontend/src/App.tsx`（`openSession` L532-538、`onCreate` L556-565——复用 `openSession` 统一入口、`onDelete` 编排 L567-597——零改动核对、`ChatPanel` 常驻挂载渲染 L681-699）、`projects/game/web/frontend/src/App.test.tsx`（会话用例现状——新建/删除/切换断言基线，L170 附近及 T002 改造后形态）

- [X] T003 [US2] 在 `projects/game/web/frontend/src/App.tsx` 扩展 `openSession` 并同步新增 `App.test.tsx` 用例（依赖 T002——`App.tsx`/`App.test.tsx` 同文件串行）：`openSession` 内新增 `setView('sessions')`（FR-005；`setOpened`/`setSelected`/`setNotice` 既有逻辑与顺序不变；`onCreate` 经 `openSession` 自动获得同语义——US2 场景 4；`onDelete` 编排零改动）；`App.test.tsx` 新增用例（contracts/web-nav.md §5 锚点 2）：presets、wiki、memory 三个视图下分别点击 `session-item` → `view-sessions` 呈激活（`data-active`）、目标会话面板（`team-toolbar` 等 ChatPanel 面）呈现、原管理视图（`presets-view`/`wiki-view`/`memory-view`）不再渲染；补一条非 session 视图下"新建会话"切回断言；既有会话切换/常驻挂载用例零回归（切换返回不重跑回填——FR-008）；编译 + 单测通过

**Checkpoint**: 死点击消除，任意视图一键直达会话（SC-003）。

---

## Phase 4: Polish & Cross-Cutting Concerns

**Purpose**: 全量回归与最终验收核对。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/072-web-nav-redesign/spec.md`（Success Criteria SC-001..SC-004 逐项核对）、`specs/072-web-nav-redesign/contracts/web-nav.md`（§5 验收锚点 1-4 逐项核对）

- [X] T004 全量回归：`bazel test //projects/game/web/...` 全绿 + `bazel build //projects/game/...` 通过；按 SC-001..SC-004 与 contracts/web-nav.md §5 锚点逐项核对；显式确认零改动面（`SessionList.tsx`/`SessionList.test.tsx`、`PresetsView`/`WikiView`/`MemoryView` 及其测试、`theme.css` 除 T001 删除段外、服务端全部面）未受影响

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**: 无任务，直接开始
- **Phase 2（US1）**: T001 与 T002 起点可并行（不同文件），T002 依赖 T001 完成（断言基于样式收敛后结构；071 T016→T017 同款先例）
- **Phase 3（US2）**: 依赖 Phase 2（T002/T003 同改 `App.tsx`/`App.test.tsx`，严格串行）
- **Phase 4（Polish）**: 依赖 Phase 2 + Phase 3 全部完成

### User Story Dependencies

- **US1（P1）**: 独立（纯前端）；入口/菜单结构是 US2 新用例的断言载体，先行
- **US2（P2）**: 依赖 US1（同文件串行）

### Parallel Opportunities

- T001（theme.css）与 T002 的 `App.tsx` 实现部分起点可并行；T002 内断言改造须在实现后
- 无其他并行面（改动文件高度集中——两文件 + 样式段）
