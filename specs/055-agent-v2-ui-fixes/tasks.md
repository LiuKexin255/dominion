# Tasks: 055 agent-v2 界面可用性修复 + dsh-web 交互对齐

**Input**: Design documents from `/specs/055-agent-v2-ui-fixes/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/ui-interactions.md, quickstart.md

**Tests**: spec FR-008 明确要求组件级测试（跟随滚动行为、对比度断言、desktop 刷新断言），故各 story phase 含测试任务；编译 + 既有单测回归（`bazel test`）是每次代码变更的一部分，不单列 task（宪法 IV）。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3, US4)
- Include exact file paths in descriptions

## Path Conventions

- web 前端：`projects/game/web/frontend/src/`
- desktop 前端：`projects/game/desktop/frontend/src/`
- 排查/复查记录：`specs/055-agent-v2-ui-fixes/revisions/`

## 文档清单（宪法 V：每个 phase 开始前 MUST 完整阅读；AGENTS.md 与本 feature spec 文件为必读，不在此重复）

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

无任务——本 feature 在既有代码库内交付（纯前端交互/样式 + desktop 列表加载时机），无项目初始化需求；唯一依赖变更（desktop jsdom devDep）归属 US3 phase（T009）。

---

## Phase 2: User Story 1 - 输出期间滚动行为正确（Priority: P1）🎯 MVP

**Goal**: 消息区成为唯一垂直滚动面（含思考流式两态）；贴底跟随条件化（阈值 24px）；非贴底呈现"回到底部"浮动入口；发新消息回底。

**Independent Test**: `bazel test //projects/game/web/frontend:lib_test`（跟随/回底入口/新消息回底/ReasoningRow 对齐结构断言）+ 真实浏览器环境三场景（思考折叠/思考展开/正文输出）无窗口滚动条（T006 排查记录）。

### 文档清单

- **代码规范文档**：
  - `style/javascript.md`（ESM 书写规则、测试/mock 约定、vitest_test 宏）
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：
  - [React useEffect Reference](https://react.dev/reference/react/useEffect)（scroll 事件订阅与清理模式）
  - [dsh-client-ui-primitives README](https://cdn.jsdelivr.net/npm/@deepseek-ai/dsh-client-ui-primitives@0.1.1-rc.2/README.md)（DisclosureRow/IconChevronDownOutline14 等 API）
  - [上游 ChatView.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ChatView.tsx)（FOLLOW_THRESHOLD/atBottom/toBottom 参照实现）
  - [上游 ChatView.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ChatView.module.css)（toBottom 按钮样式形态）
  - [上游 ReasoningRow.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.tsx)（data-expanded/双层摘要结构）
  - [上游 ReasoningRow.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.module.css)（contain 围栏/follow-end 纯 CSS/动画）
- **技术文章/技术参考文档**：
  - `specs/055-agent-v2-ui-fixes/research.md` §1.2（F1/F2/F4）、§2.2（跟随设计）、§2.3（FR-001 排查协议）
  - `specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md` §1（滚动与跟随契约）、§2（ReasoningRow 对齐契约）
  - `specs/055-agent-v2-ui-fixes/data-model.md` §1（跟随状态机）、§2（ReasoningRow 状态）

### Tests for User Story 1 ⚠️

- [X] T001 [P] [US1] 扩展 `projects/game/web/frontend/src/components/ChatView.test.tsx`：新增条件跟随断言（贴底内容增长保持贴底 / 非贴底位置保持 / 回底后恢复跟随 / 发新消息回底）与回底入口断言（非贴底渲染 `data-testid="to-bottom-button"`、点击回底并恢复跟随、贴底不渲染）——jsdom 以可赋值 `scrollTop`/`scrollHeight`/`clientHeight` 驱动；先跑确认失败
- [X] T002 [P] [US1] 扩展 `projects/game/web/frontend/src/components/ReasoningRow.test.tsx`：`data-expanded` 存在性断言、follow-end 双层结构（summary > summaryText）断言、无程序化 scrollLeft 断言——先跑确认失败
- [X] T003 [P] [US1] 新增 `projects/game/web/frontend/src/theme-fence.test.ts`：以 `?raw` 导入 `theme.css` 断言围栏规则存在（`.reasoning-row:not([data-expanded])` 含 `contain: size layout` 与 `height: 24px`）——先跑确认失败

### Implementation for User Story 1

- [X] T004 [P] [US1] `projects/game/web/frontend/src/components/ChatView.tsx`：实现条件跟随状态机（data-model.md §1）——scroll 监听维护 atBottom（`scrollHeight - scrollTop - clientHeight <= 24`）、内容增长 effect 改为仅贴底时 `scrollTop = scrollHeight`、submit 无条件回底；替换现有无条件贴底 effect
- [X] T005 [P] [US1] `projects/game/web/frontend/src/components/ReasoningRow.tsx` + `projects/game/web/frontend/src/theme.css`：F1 折叠态布局围栏（根节点 `data-expanded`；`.reasoning-row:not([data-expanded]) { contain: size layout; height: 24px; }`）；F2 follow-end 改上游纯 CSS 双层结构（`summary > summaryText`、`data-follow-end` 右对齐、行容器 `overflow: hidden` 裁剪），**删除** `useThrottledVisualUpdate`/`summaryRef`/`scheduleSummaryScroll` 机制
- [X] T006 [US1] `projects/game/web/frontend/src/components/ChatView.tsx` + `projects/game/web/frontend/src/theme.css`：非贴底渲染"回到底部"浮动入口（`IconChevronDownOutline14`、sticky 右下 34px、`aria-label`、`data-testid="to-bottom-button"`，样式参照上游 ChatView.module.css `.toBottomSlot/.toBottom` 适配本地布局），点击回底并恢复跟随（依赖 T004、T005 完成——ChatView.tsx 与 theme.css 串行）
- [X] T007 [US1] FR-001 排查与修复验证（research.md §2.3 协议）：真实浏览器环境（本地 vite dev 或 testplan 部署）复现思考流式**折叠/展开两态 + 正文输出**三场景；执行隔离实验 A（禁用 sweep 动画/`prefers-reduced-motion`）与 B（核查部署构建中行容器 `overflow` 裁剪实际生效）；断点结论与修复记录写入 `specs/055-agent-v2-ui-fixes/revisions/fr001-investigation.md`——若 F1/F2 落地后仍复现，按实际断点继续修复直至三场景均无窗口滚动条

**Checkpoint**: US1 独立可测——`bazel test //projects/game/web/frontend:lib_test` 全绿；T007 三场景人工验证通过并有排查记录。

---

## Phase 3: User Story 2 - 错误与警示状态文字可读（Priority: P1）

**Goal**: 四类状态横幅（`.chat-error`/`.chat-canceled`/`.presets-error`/`.agent-panel-error`）文字-背景对比度 ≥ 4.5:1，红/黄色系仍可区分。

**Independent Test**: `bazel test //projects/game/web/frontend:lib_test`（新增对比度断言全绿）+ 人工查看错误横幅/"已终止"标识文字可读。

### 文档清单

- **代码规范文档**：
  - `style/javascript.md`
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：
  - 无（本 phase 无第三方组件 API 使用）
- **技术文章/技术参考文档**：
  - `specs/055-agent-v2-ui-fixes/research.md` §2.1（R1 token 映射与被否备选）
  - `specs/055-agent-v2-ui-fixes/data-model.md` §3（横幅变量映射表）
  - `specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md` §3（横幅配色契约）
  - [WCAG 2.2 Understanding Contrast (Minimum)](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html)（对比度口径与计算公式）

### Tests for User Story 2 ⚠️

- [X] T008 [US2] 新增 `projects/game/web/frontend/src/theme-contrast.test.ts`：按文件内容读取 `projects/game/web/frontend/src/theme.css` 与 `src/dsh-theme/design-platform.css`（vitest `css:false` 下 `?raw` 为空串，仓库既有 readFileSync 惯例），解析四类横幅规则的 `background`/`color` 变量引用链至具体色值，按 WCAG 相对亮度公式计算对比度并断言 ≥ 4.5:1——先跑确认失败

### Implementation for User Story 2

- [X] T009 [US2] `projects/game/web/frontend/src/theme.css`：新增 `--app-banner-error-bg/-fg`、`--app-banner-warn-bg/-fg` 四个 app 级变量（映射值见 data-model.md §3：red-900/red-100、warn-tertiary/warn-primary），`.chat-error`/`.presets-error`/`.agent-panel-error` 改用 error 组、`.chat-canceled` 改用 warn 组（依赖 T008 完成后使其转绿）

**Checkpoint**: US2 独立可测——对比度断言全绿；红/黄色系视觉可区分。

---

## Phase 4: User Story 3 - desktop session 列表进入即刷新（Priority: P2）

**Goal**: desktop 从 session 详情返回列表时自动刷新；既有启动/模板切换刷新语义零回归；失败不清空列表。

**Independent Test**: `bazel test //projects/game/desktop/frontend:lib_test`（返回刷新断言）——与 web 侧任务无文件交集，可与 US1/US2 并行。

### 文档清单

- **代码规范文档**：
  - `style/javascript.md`（**mock 约定重点**：vi.mock 禁止跨包外部依赖；包内相对模块 mock 须对每个 mock 做正向调用断言）
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：
  - [Svelte svelte 包 API（mount/unmount/flushSync）](https://svelte.dev/docs/svelte/svelte)
  - [Vitest vi.mock API](https://vitest.dev/api/vi.html)
- **技术文章/技术参考文档**：
  - `specs/055-agent-v2-ui-fixes/research.md` §2.4（R3 测试基建决策与被否备选）
  - `specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md` §4.2（返回刷新契约与不变量）

### Tests for User Story 3 ⚠️

- [X] T010 [P] [US3] `projects/game/desktop/frontend/package.json` devDependencies 增加 `"jsdom": "catalog:"`（根 `pnpm-workspace.yaml` catalog 已有 `^26.0.0`），执行 `bazel run @pnpm -- --dir /mnt/code/dominion/projects/game/desktop/frontend up`、`bazel run //:gazelle projects/game/desktop/frontend`、`bazel mod tidy` 更新依赖与 BUILD
- [X] T011 [P] [US3] 新增 `projects/game/desktop/frontend/src/App.test.ts`：`// @vitest-environment jsdom` pragma；`vi.mock('./api')`（包内相对模块）并 `mount(App)` + `flushSync` 驱动——断言 (a) 点击 Back 后 `listSessions` 再次调用、(b) 刷新失败（mock reject）时错误呈现且既有列表不清空、(c) 每个 mock 均有正向调用断言（style/javascript.md 规则）；`window.runtime` 缺省安全（jsdom 无 Wails 注入）——依赖 T010 的 jsdom devDep 就位，先跑确认失败

### Implementation for User Story 3

- [X] T012 [US3] `projects/game/desktop/frontend/src/App.svelte`：`handleBackToSessions()` 在 `page = 'sessions'` 后追加 `void handleRefresh()`（契约 §4.2：刷新仅写 `sessions`/`loading`/`error`，不触碰 `selectedSession`/`page`）——完成后 T010 断言转绿

**Checkpoint**: US3 独立可测——desktop `lib_test` 全绿；既有 api.test.ts 零回归。

---

## Phase 5: User Story 4 - 传承组件交互逻辑与 dsh-web 对齐（Priority: P2）

**Goal**: 审计修复项全部落地（F3 动画颜色、F5 折叠开关 chevron）；偏离清单（contracts §5）与代码行为逐项一致，清单外无未声明偏差。

**Independent Test**: `bazel test //projects/game/web/frontend:lib_test` 全绿 + 偏离清单复查记录（T014）逐项核对成立。

### 文档清单

- **代码规范文档**：
  - `style/javascript.md`
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：
  - [上游 TurnProcessNodeView.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/TurnProcessNodeView.tsx)（chevron + data-open 结构）
  - [上游 TurnProcessNodeView.module.css](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/TurnProcessNodeView.module.css)（chevron `rotate(-90deg)`→`rotate(0)` 旋转形态）
  - [dsh-client-ui-primitives README](https://cdn.jsdelivr.net/npm/@deepseek-ai/dsh-client-ui-primitives@0.1.1-rc.2/README.md)（IconChevronDownOutline14）
- **技术文章/技术参考文档**：
  - `specs/055-agent-v2-ui-fixes/research.md` §1.2（F3/F5）、§1.3（D1-D6 偏离清单）
  - `specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md` §4.1（折叠开关契约）、§5（权威偏离清单）

### Implementation for User Story 4

- [X] T013 [US4] `projects/game/web/frontend/src/theme.css` + `projects/game/web/frontend/src/components/ChatView.tsx`：F3 扫描动画渐变改 `color-mix(in srgb, var(--dsw-alias-bg-base) 60%, transparent)`（删除硬编码 rgba）；F5 `.turn-process-toggle` 文字箭头 ▸/▾ 换 `IconChevronDownOutline14` + `data-open` 旋转样式（参照上游 TurnProcessNodeView.module.css 形态，标签文案保留本地口径 D5）
- [X] T014 [US4] 偏离清单闭合复查（SC-005）：对照 `specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md` §5 逐项核对代码实际行为与清单声明一致、清单外无未声明的交互偏差；复查记录写入 `specs/055-agent-v2-ui-fixes/revisions/parity-audit-review.md`（发现出入时修契约或修代码，以终态一致为准）

**Checkpoint**: US4 交付后四项 story 全部完成；审计闭环（修复 + 清单 + 复查记录）。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 跨 story 的验收与回归

### 文档清单

- **代码规范文档**：
  - `style/large_test.md`（testplan 执行规范——T016 必读）
- **官方文档**：
  - 无
- **技术文章/技术参考文档**：
  - `specs/055-agent-v2-ui-fixes/quickstart.md`（§2 自动化命令、§3 人工场景、§4 预期结果）

- [X] T015 全量人工验证记录：按 `specs/055-agent-v2-ui-fixes/quickstart.md` §3 执行五场景（窗口无滚动两态 / 跟随不劫持与回底 / 横幅可读 / desktop 刷新 / 对齐复查）并留存记录（部署环境；FR-001 布局验收以本记录闭合——spec Clarifications 2026-09-05 Q2=A）
- [X] T016 大型测试回归：通过 testplan skill 执行 054 既有测试计划 `projects/game/testplan/system_test.yaml`（含 agent_v2 两 suite 拓扑，`guitar run` 完成部署→测试→清理闭环），全部用例通过（宪法 VI gate 5 + spec A5 回归面承诺；本 feature 无新增大型测试面）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**: 无任务，直接进入 story phases。
- **US1（Phase 2）**: 起点；MVP。
- **US2（Phase 3）**: 依赖 US1 完成（`theme.css` 文件交集，避免并发编辑——AGENTS.md 同文件串行约定）。
- **US3（Phase 4）**: 无 web 侧依赖，**可与 US1/US2 并行**（不同项目目录）。
- **US4（Phase 5）**: 依赖 US1/US2 完成（`theme.css`/`ChatView.tsx` 文件交集）。
- **Polish（Phase 6）**: 依赖全部 story 完成。

### User Story Dependencies

- **US1 (P1)**: 无依赖——MVP，最先交付。
- **US2 (P1)**: 文件交集依赖 US1；逻辑上独立可测。
- **US3 (P2)**: 完全独立（desktop 项目），可并行。
- **US4 (P2)**: 依赖 US1/US2 的文件变更完成；审计闭环是收尾面。

### Within Each User Story

- 测试任务先行（先失败后实现，TDD 式推进 FR-008 要求的断言）。
- 同文件任务串行（T005 → T006 的 theme.css；US3 依赖链 T010 → T011 → T012：devDep → 测试 → 实现）。

### Parallel Opportunities

- US1 内：T001 ∥ T002 ∥ T003（三个不同测试文件）；T004 ∥ T005（ChatView.tsx vs ReasoningRow.tsx + theme.css）。
- 跨 story：US3（T010/T011）与 US1/US2 全程并行。
- T008 与 US1 的 T007 无文件交集，可在 US1 收尾时并行启动（对比度测试独立文件）。

---

## Parallel Example: User Story 1

```text
# 三个测试任务并行（不同文件）：
Task: T001 ChatView.test.tsx 条件跟随与回底入口断言
Task: T002 ReasoningRow.test.tsx 对齐结构断言
Task: T003 theme-fence.test.ts 围栏规则断言

# 两个实现任务并行（不同文件）：
Task: T004 ChatView.tsx 条件跟随状态机
Task: T005 ReasoningRow.tsx + theme.css 围栏与 follow-end 纯 CSS 化
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. 完成 Phase 2（US1）：T001-T003 失败测试 → T004-T006 实现 → `bazel test //projects/game/web/frontend:lib_test` 全绿 → T007 排查记录闭环。
2. **STOP and VALIDATE**: 真实浏览器三场景验证（折叠/展开/正文无窗口滚动 + 跟随交互正确）。

### Incremental Delivery

1. US1 → 验证（滚动行为 MVP）
2. US2 → 验证（横幅可读，含对比度断言）
3. US3（可与 1-2 并行）→ 验证（desktop 刷新）
4. US4 → 验证（对齐闭环 + 偏离清单复查）
5. Phase 6 人工全场景记录 + testplan 回归 → feature 验收（quickstart §4 预期结果全部达成）

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- 同一文件的编辑串行进行（AGENTS.md 并发编辑约定）
- 编译 + 既有单测回归（`bazel test //...` 或相关 target）随每次代码变更执行，不单列 task（宪法 IV）
- T007/T014/T015 的记录文档写入 `specs/055-agent-v2-ui-fixes/revisions/`，作为验收证据（宪法 I/VI）
