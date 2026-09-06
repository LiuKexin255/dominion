# Tasks: 057 agent-v2 web/desktop 界面修复二期

**Input**: Design documents from `/specs/057-agent-v2-ui-fixes-2/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/ui-interactions.md, quickstart.md

**Tests**: spec FR-004 明确要求组件级测试（重建同步、长名悬停揭示、desktop 手动刷新断言），故各 story phase 含测试任务（先失败后实现）；编译 + 既有单测回归（`bazel test`）是每次代码变更的一部分，不单列 task（宪法 IV）。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- web 前端：`projects/game/web/frontend/src/`
- desktop 前端：`projects/game/desktop/frontend/src/`
- 验证/复查记录：`specs/057-agent-v2-ui-fixes-2/revisions/`

## 文档清单（宪法 V：每个 phase 开始前 MUST 完整阅读；AGENTS.md 与本 feature spec 文件为必读，不在此重复）

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

无任务——本 feature 在既有代码库内交付（纯前端行为修复，无项目初始化、无依赖变更、无 BUILD/manifest 变更，research.md §3）。

### 文档清单

- **代码规范文档**：无（本 phase 无任务、无代码变更）
- **官方文档**：无
- **技术文章/技术参考文档**：无

---

## Phase 2: User Story 1 - agent 重建后对话视图即时同步（Priority: P1）🎯 MVP

**Goal**: 应用成功（UpdateAgent）后当前会话对话视图立即同步为重建后状态：挂载回填提取为共享 `runBackfill`（epoch 守卫复位），`onApplied` 追加调用；空闲/忙时/首次物化三路径收敛同一干净终态（data-model.md §1.3 收敛矩阵）。

**Independent Test**: `bazel test //projects/game/web/frontend:lib_test`（新增重建同步断言 + 既有回填/让位断言零回归）。

### 文档清单

- **代码规范文档**：
  - `style/javascript.md`（ESM 书写规则、测试/mock 约定：fetch 级 `vi.fn()` test-double 与正向调用断言——App.test.tsx 既有模式）
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的规范基准）
- **官方文档**：
  - [React useCallback Reference](https://react.dev/reference/react/useCallback)（`runBackfill` 提取为 callback 的 deps 语义）
  - [React useEffect Reference](https://react.dev/reference/react/useEffect)（挂载 effect 改为复用 `runBackfill` 的清理/依赖语义）
- **技术文章/技术参考文档**：
  - `specs/057-agent-v2-ui-fixes-2/research.md` §1.1（根因事实链）、§2 D1（决策与被否备选）
  - `specs/057-agent-v2-ui-fixes-2/data-model.md` §1（回填 epoch、runBackfill 调用点、收敛矩阵）
  - `specs/057-agent-v2-ui-fixes-2/contracts/ui-interactions.md` §2（重建同步契约 + 测试口径）

### Tests for User Story 1 ⚠️

- [ ] T001 [P] [US1] 扩展 `projects/game/web/frontend/src/App.test.tsx`（沿用既有 fetch mock 按路由分发模式）：新增重建同步断言——(a) 已物化会话 Apply 成功（PATCH `/api/v2/.../agent` 200）后再次 GET `.../agent/messages`（回填触发）且对话区旧消息清空；(b) 回填慢返回与紧随 send 的让位（send 后空历史不覆盖新回合）；(c) 回填请求失败（500）→ 既有回填错误呈现、对话不清空；(d) Apply 失败（PATCH 4xx/5xx）→ 不触发第二次回填、面板错误既有呈现；(e) 忙时收敛：在途流 `turn_end{ABORTED}`（store 归约清空）与回填落地任意序 → 与空闲路径收敛同一干净终态（无重复/冲突中间态，data-model §1.3 收敛矩阵）；(f) 首次物化（未物化→物化）Apply 成功 → 回填 200 空、引导态消退、对话面为空；对每个 mock 路由做正向调用断言（`style/javascript.md` 规则）——先跑确认失败

### Implementation for User Story 1

- [ ] T002 [US1] `projects/game/web/frontend/src/App.tsx`：ChatPanel 内提取 `runBackfill`（`useCallback`，deps `[session, store]`）——置 `sentSinceBackfill.current = false`（epoch 复位）→ `listHistory(session)` → 成功且守卫仍 false 时 `store.loadHistory(messages)`、`setBackfillError(null)`；404 走既有未物化分支、其余失败 `setBackfillError`（逻辑自现挂载 effect 原样迁移，挂载 effect 改调 `runBackfill` 保持 `cancelled` 清理语义与零行为回归）；`onApplied` 在既有 setAgent/setAgentStatus/setPanelOpen 之外追加 `void runBackfill()`（契约 §2；依赖 T001 完成后使其转绿）

**Checkpoint**: US1 独立可测——`bazel test //projects/game/web/frontend:lib_test` 全绿（含既有挂载回填/切换会话/发送让位断言零回归）。

---

## Phase 3: User Story 2 - web session 长名悬停可读全名（Priority: P2）

**Goal**: 超宽名称条目悬停自动滚动揭示全名：延迟 250ms 启动、每 30ms 步进 `scrollLeft += 3px`、单程到尾 hold、移出复位 0；短名与 `prefers-reduced-motion: reduce` 不启动；既有渐隐/类切换/复位断言零回归（契约 §1 参数基线）。

**Independent Test**: `bazel test //projects/game/web/frontend:lib_test`（marquee 断言 + 既有 hover 断言零回归）；真实浏览器滚动可达性在 Phase 5 人工验证闭合。

### 文档清单

- **代码规范文档**：
  - `style/javascript.md`（ESM 书写规则、测试/mock 约定）
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：
  - [Vitest vi.useFakeTimers](https://vitest.dev/api/vi.html#vi-usefaketimers)（fake timers 推进 250ms 延迟与 30ms 步进的断言方式）
  - [MDN Window.matchMedia](https://developer.mozilla.org/en-US/docs/Web/API/Window/matchMedia)（reduced-motion 判定 API；jsdom 未实现 `matchMedia`，测试须 stub——见任务描述）
- **技术文章/技术参考文档**：
  - [上游 attachment-rail.client.spec.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-attachment/tests/attachment-rail.client.spec.tsx)（`scrollWidth`/`clientWidth`/`scrollLeft` defineProperty stub 的上游测试模式参照）
  - `specs/057-agent-v2-ui-fixes-2/research.md` §1.2（滚动不可达根因与外部事实链接）、§2 D2~D4（驱动机制/端点行为/参数与降级决策及被否备选）
  - `specs/057-agent-v2-ui-fixes-2/data-model.md` §2（marquee 状态机 idle→armed→scrolling→held 与复位）
  - `specs/057-agent-v2-ui-fixes-2/contracts/ui-interactions.md` §1（触发/节奏/复位/约束契约 + 测试口径）

### Tests for User Story 2 ⚠️

- [ ] T003 [P] [US2] 扩展 `projects/game/web/frontend/src/components/SessionList.test.tsx`：stub `window.matchMedia`（jsdom 未实现；默认非 reduce、可切 reduce 两种返回）+ 对 `.session-name` 元素以 `defineProperty` stub 滚动几何（`scrollWidth`/`clientWidth`/`scrollLeft`，模式参照上游 attachment-rail 测试）+ `vi.useFakeTimers()`——断言 (a) 悬停后未到 250ms 无滚动，`advanceTimersByTime(250 + N*30)` 后 `scrollLeft` 按 3px/步递增至 max 且到 max 后不再增长（hold）；(b) mouseleave 后再 advance 无变化且 `scrollLeft = 0`；(c) 短名（stub `scrollWidth <= clientWidth`）悬停无任何 `scrollLeft` 变化；(d) reduce 悬停不滚动；(e) 既有 scrollable 类切换/移出复位/THEME_CSS 遮罩断言保持通过；(f) `.session-name` span 携带完整名称 `title` 属性（可达性静态途径，契约 §1）——先跑确认失败

### Implementation for User Story 2

- [ ] T004 [US2] `projects/game/web/frontend/src/components/SessionList.tsx`：实现 marquee 机制（data-model.md §2 状态机）——mouseenter（既有 hovered 切换入口）时若名称元素 `scrollWidth > clientWidth` 且 `!matchMedia('(prefers-reduced-motion: reduce)').matches` 则 250ms 延迟后启动 30ms 间隔步进 `scrollLeft += 3px` 至 max 停止；新悬停/移出/组件卸载清除既有定时器（悬停互斥），mouseleave 复用既有 `scrollLeft = 0` 复位点；元素定位沿用 `e.currentTarget.querySelector('.session-name')` 既有模式；`.session-name` span 补充原生 `title={sessionTitle(s.name)}` 静态阅读途径（契约 §1 决策，沿用本组件 IconButton 既有原生 title 模式）；`projects/game/web/frontend/src/theme.css` 预期零改动（`.session-name.scrollable` 既有承载）——依赖 T003 完成后使其转绿

**Checkpoint**: US2 独立可测——`bazel test //projects/game/web/frontend:lib_test` 全绿（US1 断言不回归；两 story 文件无交集）。

---

## Phase 4: User Story 3 - desktop sessions 列表页手动刷新（Priority: P3）

**Goal**: sessions-toolbar 右侧新增 `Refresh` 按钮（`data-testid="refresh-sessions"`、`disabled={loading}` 幂等），点击复用既有 `handleRefresh`（仅写 sessions/loading/error 三态）；既有三个自动刷新时机零回归（契约 §3）。

**Independent Test**: `bazel test //projects/game/desktop/frontend:lib_test`（刷新按钮断言 + 既有刷新断言零回归）——desktop 项目与 web 无文件交集，可全程并行。

### 文档清单

- **代码规范文档**：
  - `style/javascript.md`（**mock 约定重点**：包内相对模块 `vi.mock('./api')` 允许但须对每个 mock 做正向调用断言）
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：
  - [Svelte svelte 包 API（mount/unmount/flushSync）](https://svelte.dev/docs/svelte/svelte)（App.test.ts 既有 mount 驱动模式）
  - [Vitest vi.mock API](https://vitest.dev/api/vi.html)（`vi.mock('./api')` 语义）
- **技术文章/技术参考文档**：
  - `specs/057-agent-v2-ui-fixes-2/research.md` §2 D5（按钮位置/形态/幂等决策与被否备选）
  - `specs/057-agent-v2-ui-fixes-2/data-model.md` §3（handleRefresh 状态写集复用表）
  - `specs/057-agent-v2-ui-fixes-2/contracts/ui-interactions.md` §3（入口/行为/呈现契约、051 退化清单修订登记、测试口径）

### Tests for User Story 3 ⚠️

- [ ] T005 [P] [US3] 扩展 `projects/game/desktop/frontend/src/App.test.ts`（"App sessions refresh" 既有 describe 块同套模式）：新增断言——(a) sessions 页渲染 `data-testid="refresh-sessions"` 按钮，点击后 `listSessions` 再次调用且列表更新；(b) 刷新失败（mock reject）错误呈现且既有列表不清空；(c) 请求在途时按钮 `disabled`（可控 promise 驱动）；(d) 每个 mock 正向调用断言（`style/javascript.md` 规则）——先跑确认失败

### Implementation for User Story 3

- [ ] T006 [US3] `projects/game/desktop/frontend/src/App.svelte`：sessions-toolbar 内模板标识 span 之后添加 `<button class="btn btn-small" data-testid="refresh-sessions" onclick={handleRefresh} disabled={loading}>Refresh</button>`（契约 §3：不改 handleRefresh、不改 SessionList.svelte；`space-between` 布局使按钮自然落右侧）——依赖 T005 完成后使其转绿

**Checkpoint**: US3 独立可测——`bazel test //projects/game/desktop/frontend:lib_test` 全绿（含 055 返回刷新/api.test.ts 零回归）。

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: 跨 story 的验收与回归

### 文档清单

- **代码规范文档**：
  - `style/large_test.md`（testplan 执行规范——T008 必读：`guitar run` 闭环、既有计划复用原则）
- **官方文档**：
  - 无
- **技术文章/技术参考文档**：
  - `specs/057-agent-v2-ui-fixes-2/quickstart.md`（§2 自动化命令、§3 人工场景、§5 预期结果汇总）

- [ ] T007 人工验证记录：按 `specs/057-agent-v2-ui-fixes-2/quickstart.md` §3 在部署环境执行三组场景（§3.1 悬停自动滚动真实浏览器行为 / §3.2 重建同步即时清空与忙时收敛 / §3.3 desktop 手动刷新含失败路径），结果记录写入 `specs/057-agent-v2-ui-fixes-2/revisions/manual-verification.md`（spec A3/FR-004：滚动可达性与重建体验以本记录闭合）
- [ ] T008 大型测试回归：通过 testplan skill 执行既有测试计划 `projects/game/testplan/system_test.yaml`（`guitar run` 完成部署→测试→清理闭环），全部用例通过（宪法 VI gate 5 + spec A3 回归面承诺；本 feature 无服务行为变更、不新增测试计划 YAML——`style/large_test.md` 既有计划复用原则）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**: 无任务，直接进入 story phases。
- **US1（Phase 2）**: 起点；MVP。
- **US2（Phase 3）**: 与 US1 并行（文件无交集：App.tsx/App.test.tsx vs SessionList.tsx/SessionList.test.tsx）。
- **US3（Phase 4）**: 与 US1/US2 全程并行（desktop 项目目录独立）。
- **Polish（Phase 5）**: 依赖全部 story 完成。

### User Story Dependencies

- **US1 (P1)**: 无依赖——MVP，最先交付。
- **US2 (P2)**: 无依赖（web 内不同文件），可与 US1 并行。
- **US3 (P3)**: 完全独立（desktop 项目），可并行。

### Within Each User Story

- 测试任务先行（先失败后实现：T001→T002、T003→T004、T005→T006——测试与实现同文件相邻变更，串行执行）。
- 编译 + 既有单测回归随每次实现变更执行（宪法 IV，不单列）。

### Parallel Opportunities

- 跨 story：US1（T001/T002）∥ US2（T003/T004）∥ US3（T005/T006）——三组文件零交集，可三线并行。
- 同 story 内测试与实现为依赖关系（实现使失败测试转绿），不并行。

---

## Parallel Example: 跨 story 三线并行

```text
# 三条独立线（不同文件、不同项目）：
Task: T001 App.test.tsx 重建同步断言 → T002 App.tsx runBackfill + onApplied
Task: T003 SessionList.test.tsx marquee 断言 → T004 SessionList.tsx marquee 机制
Task: T005 App.test.ts 刷新按钮断言 → T006 App.svelte Refresh 按钮
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. 完成 Phase 2（US1）：T001 失败测试 → T002 实现 → `bazel test //projects/game/web/frontend:lib_test` 全绿。
2. **STOP and VALIDATE**: 部署环境按 quickstart §3.2 验证重建后即时清空（可不刷新页面）。

### Incremental Delivery

1. US1 → 验证（重建同步 MVP）
2. US2 → 验证（长名悬停可读全名）
3. US3（可与 1-2 并行）→ 验证（desktop 手动刷新）
4. Phase 5 人工全场景记录（T007）+ testplan 回归（T008）→ feature 验收（quickstart §5 预期结果全部达成）

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- 同一文件的编辑串行进行（AGENTS.md 并发编辑约定）；测试→实现按依赖顺序执行
- 编译 + 既有单测回归（`bazel test` 相关 target）随每次代码变更执行，不单列 task（宪法 IV）
- T007/T008 的记录/执行证据归档于 `specs/057-agent-v2-ui-fixes-2/revisions/` 与 testplan 执行记录（宪法 I/VI）
